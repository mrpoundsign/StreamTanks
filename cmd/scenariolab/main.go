package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"streamtanks/internal/scenariolab"
)

var (
	addrFlag      = flag.String("addr", ":8105", "HTTP listen address for visual Scenario Lab UI")
	fuzzFlag      = flag.Int("fuzz", 0, "Run N randomized scenarios in terminal differential fuzzing mode (0 to run web server)")
	batchFlag     = flag.Int("batch", 500, "Batch size of scenarios per headless simulation chunk")
	outFlag       = flag.String("out", "scenarios", "Directory to save mismatch scenario JSON files")
	replayFlag    = flag.String("replay", "", "Path to a scenario JSON file to run and inspect in terminal")
	runsFlag      = flag.Int("runs", 100, "Number of replay simulation runs to detect flakiness / measure failure rate")
	untilFailFlag = flag.Bool("until-fail", false, "Run replay iterations until a failure/divergence is detected")
	dirFlag       = flag.String("dir", "./lab-web/public", "Directory containing lab frontend assets")
)

func main() {
	flag.Parse()

	if *replayFlag != "" {
		runReplayCLI(*replayFlag)
		return
	}

	if *fuzzFlag > 0 {
		runFuzzCLI(*fuzzFlag, *outFlag)
		return
	}

	// Default: Run Scenario Lab Web Server
	if err := scenariolab.StartScenarioLab(*addrFlag, *outFlag, *dirFlag); err != nil {
		log.Fatalf("Scenario Lab server error: %v", err)
	}
}

func runReplayCLI(filePath string) {
	sc, err := scenariolab.LoadScenario(filePath)
	if err != nil {
		log.Fatalf("Failed to load scenario %q: %v", filePath, err)
	}

	_ = ensureSimulationBundle()

	// Run Overlay Simulation (Headless Node) to get the client reference
	clientResults, err := runOverlaySimHeadless([]*scenariolab.Scenario{sc})
	if err != nil {
		log.Fatalf("Error running overlay simulation: %v", err)
	}
	if len(clientResults) == 0 {
		log.Fatalf("Overlay simulation returned 0 results")
	}
	clientRes := &clientResults[0]

	targetRuns := *runsFlag
	if targetRuns <= 0 {
		targetRuns = 100
	}

	fmt.Printf("\n======================================================================\n")
	fmt.Printf(" STREAMTANKS SCENARIO REPLAY & DETERMINISM AUDIT\n")
	fmt.Printf(" File:      %s\n", filePath)
	fmt.Printf(" Scenario:  %s (ID: %s)\n", sc.Name, sc.ID)
	if *untilFailFlag {
		fmt.Printf(" Mode:      Scan until failure (then sample %d runs)\n", targetRuns)
	} else {
		fmt.Printf(" Mode:      Audit across %d iterations\n", targetRuns)
	}
	fmt.Printf("======================================================================\n\n")

	fmt.Printf("Client (Overlay): %d steps, %d impacts, %d kills, Winner: %s\n",
		clientRes.TotalSteps, len(clientRes.Impacts), len(clientRes.Kills), clientRes.Winner)

	var firstMismatch *scenariolab.DiffReport
	var firstMismatchRun int
	var baselineServerRes *scenariolab.SimulationResult

	divergedRuns := 0
	internalDivergedRuns := 0
	totalRuns := 0

	if *untilFailFlag {
		fmt.Print("Scanning for divergence... ")
		found := false
		maxScan := 10000
		for scanRun := 1; scanRun <= maxScan; scanRun++ {
			sRes := scenariolab.RunScenarioSimulation(sc, 1.0)
			if scanRun == 1 {
				baselineServerRes = sRes
			}
			diff := scenariolab.CompareSimulationResults(sRes, clientRes)
			internalDiff := scenariolab.CompareSimulationResults(sRes, baselineServerRes)

			if diff.HasDiscrepancy || internalDiff.HasDiscrepancy {
				found = true
				firstMismatchRun = scanRun
				if diff.HasDiscrepancy {
					firstMismatch = diff
				} else {
					firstMismatch = internalDiff
				}
				fmt.Printf("FOUND on run #%d!\nSampling %d runs to calculate failure rate...\n\n", scanRun, targetRuns)
				break
			}
		}
		if !found {
			fmt.Printf("No divergence detected after %d iterations.\n", maxScan)
			fmt.Printf("\n>>> Replay Audit Summary: PERFECT MATCH: 100.0%% Deterministic (0/%d failures)\n", maxScan)
			fmt.Printf("======================================================================\n")
			return
		}
	}

	for i := 1; i <= targetRuns; i++ {
		totalRuns++
		sRes := scenariolab.RunScenarioSimulation(sc, 1.0)
		if i == 1 && baselineServerRes == nil {
			baselineServerRes = sRes
			fmt.Printf("Server (Run #1):   %d steps, %d impacts, %d kills, Winner: %s\n",
				sRes.TotalSteps, len(sRes.Impacts), len(sRes.Kills), sRes.Winner)
		}

		diff := scenariolab.CompareSimulationResults(sRes, clientRes)
		internalDiff := scenariolab.CompareSimulationResults(sRes, baselineServerRes)

		if diff.HasDiscrepancy {
			divergedRuns++
			if firstMismatch == nil {
				firstMismatch = diff
				firstMismatchRun = i
			}
		}
		if internalDiff.HasDiscrepancy {
			internalDivergedRuns++
			if firstMismatch == nil {
				firstMismatch = internalDiff
				firstMismatchRun = i
			}
		}
	}

	failureRate := float64(divergedRuns) / float64(totalRuns) * 100.0
	internalRate := float64(internalDivergedRuns) / float64(totalRuns) * 100.0

	fmt.Printf("\n>>> Replay Audit Summary (%d runs):\n", totalRuns)
	if divergedRuns > 0 || internalDivergedRuns > 0 {
		fmt.Printf("  [NON-DETERMINISM / FLAKINESS DETECTED]\n")
		fmt.Printf("  - Parity Failure Rate:      %.1f%% (%d/%d runs diverged from overlay)\n", failureRate, divergedRuns, totalRuns)
		fmt.Printf("  - Internal Non-Determinism: %.1f%% (%d/%d runs diverged from Run #1)\n", internalRate, internalDivergedRuns, totalRuns)
		if firstMismatch != nil {
			fmt.Printf("\n>>> First Divergence Details (Observed on Run #%d):\n", firstMismatchRun)
			fmt.Printf("  Summary: %s\n", firstMismatch.Summary)
			for _, m := range firstMismatch.KillMismatches {
				fmt.Printf("  [MISMATCH] %s\n", m)
			}
			if firstMismatch.TerrainDiffCount > 0 {
				fmt.Printf("  [TERRAIN DIFF] %d columns differed (max: %.1fpx)\n", firstMismatch.TerrainDiffCount, firstMismatch.MaxTerrainDiff)
			}
			if firstMismatch.MaxPositionDiff > 1.0 {
				fmt.Printf("  [POSITION DIFF] Max tank position delta: %.1fpx\n", firstMismatch.MaxPositionDiff)
			}
		}
	} else {
		fmt.Printf("  [PERFECT MATCH: 100.0%% DETERMINISTIC]\n")
		fmt.Printf("  - Parity Failure Rate:      0.0%% (0/%d runs diverged from overlay)\n", totalRuns)
		fmt.Printf("  - Internal Non-Determinism: 0.0%% (0/%d runs diverged from Run #1)\n", totalRuns)
		fmt.Printf("  - Status: All %d runs produced identical bit-for-bit results matching overlay.\n", totalRuns)
	}
	fmt.Printf("======================================================================\n")
}

func runFuzzCLI(count int, outDir string) {
	_ = os.MkdirAll(outDir, 0o755)

	fmt.Printf("\n==================================================\n")
	fmt.Printf(" STREAMTANKS DIFFERENTIAL SCENARIO FUZZER\n")
	fmt.Printf(" Iterations: %d | Output Directory: %s\n", count, outDir)
	fmt.Printf("==================================================\n\n")

	_ = ensureSimulationBundle()

	batchSize := *batchFlag
	if batchSize <= 0 {
		batchSize = 500
	}
	if batchSize > count {
		batchSize = count
	}
	totalBatches := (count + batchSize - 1) / batchSize

	passed := 0
	discrepancies := 0
	startTime := time.Now()

	for b := range totalBatches {
		curBatchSize := batchSize
		if (b+1)*batchSize > count {
			curBatchSize = count - b*batchSize
		}

		scenarios := make([]*scenariolab.Scenario, curBatchSize)
		serverResults := make([]*scenariolab.SimulationResult, curBatchSize)

		for i := range curBatchSize {
			seed := time.Now().UnixNano() + int64(b*batchSize+i)*7919
			sc := scenariolab.GenerateRandomScenario(seed)
			scenarios[i] = sc
			serverResults[i] = scenariolab.RunScenarioSimulation(sc, 1.0)
		}

		clientResults, err := runOverlaySimHeadless(scenarios)
		if err != nil {
			log.Fatalf("Error running overlay headless simulation batch: %v", err)
		}

		for i := range curBatchSize {
			diff := scenariolab.CompareSimulationResults(serverResults[i], &clientResults[i])
			if diff.HasDiscrepancy {
				discrepancies++
				savedPath, saveErr := scenariolab.SaveScenario(outDir, scenarios[i], diff)
				if saveErr == nil && discrepancies <= 20 {
					fmt.Printf("\n[MISMATCH #%d] Scenario %s -> %s (Saved: %s)\n",
						discrepancies, scenarios[i].ID, diff.Summary, filepath.Base(savedPath))
				} else if discrepancies == 21 {
					fmt.Printf("\n[MISMATCH] Additional mismatches will be saved to %s without flooding terminal...\n", outDir)
				}
			} else {
				passed++
			}
		}

		progress := float64(b*batchSize+curBatchSize) / float64(count) * 100.0
		fmt.Printf("Progress: %d/%d (%.1f%%) | Passed: %d | Mismatches: %d\r",
			b*batchSize+curBatchSize, count, progress, passed, discrepancies)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\n\n==================================================\n")
	fmt.Printf(" FUZZING SUMMARY\n")
	fmt.Printf(" Total Scenarios: %d\n", count)
	fmt.Printf(" Perfect Matches: %d (%.1f%%)\n", passed, float64(passed)/float64(count)*100.0)
	fmt.Printf(" Discrepancies:   %d (%.1f%%)\n", discrepancies, float64(discrepancies)/float64(count)*100.0)
	fmt.Printf(" Elapsed Time:    %s (%.1f scenarios/sec)\n", elapsed, float64(count)/elapsed.Seconds())
	fmt.Printf(" Saved Mismatches: %s/\n", outDir)
	fmt.Printf("==================================================\n")
}

func ensureSimulationBundle() error {
	distFile := "./web/dist/simulation.mjs"
	srcFile := "./web/src/simulation.ts"
	distStat, distErr := os.Stat(distFile)
	srcStat, srcErr := os.Stat(srcFile)
	if distErr == nil && srcErr == nil && distStat.ModTime().After(srcStat.ModTime()) {
		return nil
	}
	_ = os.MkdirAll("./web/dist", 0o755)
	cmd := exec.Command("npx", "esbuild", "web/src/simulation.ts", "--bundle", "--format=esm", "--outfile="+distFile)
	return cmd.Run()
}

func runOverlaySimHeadless(scenarios []*scenariolab.Scenario) ([]scenariolab.SimulationResult, error) {
	runnerScript := "./scripts/headless_sim.mjs"
	if _, err := os.Stat(runnerScript); err != nil {
		return nil, fmt.Errorf("headless runner script not found: %s", runnerScript)
	}

	inputData, err := json.Marshal(scenarios)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("node", runnerScript)
	cmd.Stdin = bytes.NewReader(inputData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("node simulation failed: %v, stderr: %s", err, stderr.String())
	}

	type clientItem struct {
		ScenarioID   string                                `json:"scenarioId"`
		Kills        []scenariolab.KillRecord              `json:"kills"`
		Impacts      []scenariolab.ImpactRecord            `json:"impacts"`
		FinalTerrain []float64                             `json:"finalTerrain"`
		FinalPlayers map[string]scenariolab.FinalPlayerState `json:"finalPlayers"`
		TotalSteps   int                                   `json:"totalSteps"`
	}

	var rawItems []clientItem
	if err := json.Unmarshal(stdout.Bytes(), &rawItems); err != nil {
		return nil, fmt.Errorf("failed to parse node simulation output: %v", err)
	}

	results := make([]scenariolab.SimulationResult, len(rawItems))
	for i, item := range rawItems {
		results[i] = scenariolab.SimulationResult{
			Kills:        item.Kills,
			Impacts:      item.Impacts,
			FinalTerrain: item.FinalTerrain,
			FinalPlayers: item.FinalPlayers,
			TotalSteps:   item.TotalSteps,
		}
	}
	return results, nil
}
