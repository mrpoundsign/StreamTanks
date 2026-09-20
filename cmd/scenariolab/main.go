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
	addrFlag   = flag.String("addr", ":8105", "HTTP listen address for visual Scenario Lab UI")
	fuzzFlag   = flag.Int("fuzz", 0, "Run N randomized scenarios in terminal differential fuzzing mode (0 to run web server)")
	outFlag    = flag.String("out", "scenarios", "Directory to save mismatch scenario JSON files")
	replayFlag = flag.String("replay", "", "Path to a scenario JSON file to run and inspect in terminal")
	dirFlag    = flag.String("dir", "./lab-web/public", "Directory containing lab frontend assets")
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

	fmt.Printf("\n>>> Running Replay for Scenario: %s (ID: %s)\n", sc.Name, sc.ID)
	serverRes := scenariolab.RunScenarioSimulation(sc, 1.0)
	fmt.Printf("Server Result: %d steps, %d impacts, %d kills, Winner: %s\n",
		serverRes.TotalSteps, len(serverRes.Impacts), len(serverRes.Kills), serverRes.Winner)
	for _, k := range serverRes.Kills {
		fmt.Printf("  - Kill: %s destroyed %s at (%.1f, %.1f) via %s\n", k.Killer, k.Victim, k.X, k.Y, k.Reason)
	}

	// Run Overlay Simulation
	clientResults, err := runOverlaySimHeadless([]*scenariolab.Scenario{sc})
	if err != nil {
		log.Printf("Warning: Could not run overlay simulation: %v", err)
		return
	}
	if len(clientResults) > 0 {
		diff := scenariolab.CompareSimulationResults(serverRes, &clientResults[0])
		fmt.Printf("\n>>> Comparison Summary: %s\n", diff.Summary)
		if diff.HasDiscrepancy {
			for _, m := range diff.KillMismatches {
				fmt.Printf("  [MISMATCH] %s\n", m)
			}
			if diff.TerrainDiffCount > 0 {
				fmt.Printf("  [TERRAIN DIFF] %d columns differed (max: %.1fpx)\n", diff.TerrainDiffCount, diff.MaxTerrainDiff)
			}
			if diff.MaxPositionDiff > 1.0 {
				fmt.Printf("  [POSITION DIFF] Max tank position delta: %.1fpx\n", diff.MaxPositionDiff)
			}
		}
	}
}

func runFuzzCLI(count int, outDir string) {
	_ = os.MkdirAll(outDir, 0o755)

	fmt.Printf("\n==================================================\n")
	fmt.Printf(" STREAMTANKS DIFFERENTIAL SCENARIO FUZZER\n")
	fmt.Printf(" Iterations: %d | Output Directory: %s\n", count, outDir)
	fmt.Printf("==================================================\n\n")

	_ = ensureSimulationBundle()

	batchSize := 50
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
				if saveErr == nil {
					fmt.Printf("[MISMATCH #%d] Scenario %s -> %s (Saved: %s)\n",
						discrepancies, scenarios[i].ID, diff.Summary, filepath.Base(savedPath))
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
	if _, err := os.Stat(distFile); err == nil {
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
