package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
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
	fpsFlag       = flag.Int("fps", 0, "Client overlay FPS to simulate (0 = cycle across 144, 120, 60; >0 = fixed FPS)")
	workersFlag   = flag.Int("workers", runtime.GOMAXPROCS(0), "Number of persistent Node.js worker processes in the pool (default: GOMAXPROCS)")
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

	physicsSpeed := sc.Rules.PhysicsSpeed
	if physicsSpeed <= 0 {
		physicsSpeed = 0.5
	}
	serverDtScale := 1.0 * physicsSpeed

	clientFps := 144
	if *fpsFlag > 0 {
		clientFps = *fpsFlag
	}
	clientDtScale := (60.0 / float64(clientFps)) * physicsSpeed

	// Run Overlay Simulation (Headless Node) to get the client reference
	serverRes := scenariolab.RunScenarioSimulation(sc, serverDtScale)
	scaledImpacts := make([]scenariolab.ImpactRecord, len(serverRes.Impacts))
	for j, imp := range serverRes.Impacts {
		scaledImpacts[j] = imp
		scaledImpacts[j].Step = int(math.Round(float64(imp.Step) * float64(clientFps) / 60.0))
	}

	runnerScript := "./scripts/headless_sim.mjs"
	pool, err := scenariolab.NewNodeWorkerPool(1, runnerScript)
	if err != nil {
		log.Fatalf("Failed to initialize worker pool: %v", err)
	}
	defer pool.Close()

	clientResults, err := pool.RunBatch([]scenariolab.OverlaySimBatchItem{
		{
			Scenario:      sc,
			ServerImpacts: scaledImpacts,
			ClientFPS:     clientFps,
			ClientDtScale: clientDtScale,
		},
	})
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

	runIteration := func(runIdx int) bool {
		totalRuns++
		currentServerRes := scenariolab.RunScenarioSimulation(sc, serverDtScale)

		if baselineServerRes == nil {
			baselineServerRes = currentServerRes
		} else {
			internalDiff := scenariolab.CompareSimulationResults(baselineServerRes, currentServerRes)
			if internalDiff.HasDiscrepancy {
				internalDivergedRuns++
			}
		}

		diff := scenariolab.CompareSimulationResults(currentServerRes, clientRes)
		if diff.HasDiscrepancy {
			divergedRuns++
			if firstMismatch == nil {
				firstMismatch = diff
				firstMismatchRun = runIdx
			}
			return false
		}
		return true
	}

	if *untilFailFlag {
		maxScanLimit := 10000
		scanPassed := 0
		foundFailure := false

		fmt.Printf("Scanning for divergence (up to %d iterations)...\n", maxScanLimit)
		for i := 1; i <= maxScanLimit; i++ {
			passed := runIteration(i)
			if passed {
				scanPassed++
			} else {
				foundFailure = true
				fmt.Printf("-> Failure detected on iteration #%d! Sampling remaining %d runs to compute failure rate...\n", i, targetRuns-1)
				break
			}
		}

		if foundFailure {
			for i := totalRuns + 1; i <= totalRuns+targetRuns-1; i++ {
				runIteration(i)
			}
		} else {
			fmt.Printf("-> No divergence detected in %d consecutive runs.\n", maxScanLimit)
		}
	} else {
		for i := 1; i <= targetRuns; i++ {
			runIteration(i)
		}
	}

	failurePct := float64(divergedRuns) / float64(totalRuns) * 100.0
	internalFailurePct := float64(internalDivergedRuns) / float64(totalRuns) * 100.0

	fmt.Printf("\n======================================================================\n")
	fmt.Printf(" AUDIT RESULTS (%d Iterations Tested)\n", totalRuns)
	fmt.Printf("----------------------------------------------------------------------\n")

	if divergedRuns > 0 || internalDivergedRuns > 0 {
		fmt.Printf("  [FLAKINESS DETECTED]\n")
		fmt.Printf("  - Parity Failure Rate:      %.1f%% (%d/%d runs diverged from overlay)\n", failurePct, divergedRuns, totalRuns)
		fmt.Printf("  - Internal Non-Determinism: %.1f%% (%d/%d runs diverged from Run #1)\n", internalFailurePct, internalDivergedRuns, totalRuns)
		fmt.Printf("  - Status: SIMULATION IS NON-DETERMINISTIC ACROSS RUNS\n\n")

		if firstMismatch != nil {
			fmt.Printf("  First Divergence Observed on Run #%d:\n", firstMismatchRun)
			fmt.Printf("  - Summary: %s\n", firstMismatch.Summary)
			for _, m := range firstMismatch.KillMismatches {
				fmt.Printf("    * %s\n", m)
			}
			if firstMismatch.TerrainDiffCount > 0 {
				fmt.Printf("  [TERRAIN DIFF] %d columns diverged (max delta: %.1fpx)\n",
					firstMismatch.TerrainDiffCount, firstMismatch.MaxTerrainDiff)
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

	numWorkers := *workersFlag
	if numWorkers <= 0 {
		numWorkers = runtime.GOMAXPROCS(0)
	}
	if numWorkers <= 0 {
		numWorkers = 1
	}

	fmt.Printf("\n==================================================\n")
	fmt.Printf(" STREAMTANKS DIFFERENTIAL SCENARIO FUZZER\n")
	fmt.Printf(" Iterations: %d | Workers: %d | Output Directory: %s\n", count, numWorkers, outDir)
	fmt.Printf("==================================================\n\n")

	_ = ensureSimulationBundle()

	runnerScript := "./scripts/headless_sim.mjs"
	pool, err := scenariolab.NewNodeWorkerPool(numWorkers, runnerScript)
	if err != nil {
		log.Fatalf("Failed to initialize worker pool: %v", err)
	}
	defer pool.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Printf("\nInterrupted. Shutting down %d workers...\n", pool.Size())
		pool.Close()
		os.Exit(130)
	}()

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

	fpsList := []int{144, 120, 60}
	if *fpsFlag > 0 {
		fpsList = []int{*fpsFlag}
	}

	for b := range totalBatches {
		curBatchSize := batchSize
		if (b+1)*batchSize > count {
			curBatchSize = count - b*batchSize
		}

		scenarios := make([]*scenariolab.Scenario, curBatchSize)
		serverResults := make([]*scenariolab.SimulationResult, curBatchSize)
		items := make([]scenariolab.OverlaySimBatchItem, curBatchSize)

		var genWg sync.WaitGroup
		chunkSize := (curBatchSize + numWorkers - 1) / numWorkers
		for w := range numWorkers {
			start := w * chunkSize
			if start >= curBatchSize {
				break
			}
			end := min(start+chunkSize, curBatchSize)
			genWg.Add(1)
			go func(start, end int) {
				defer genWg.Done()
				for i := start; i < end; i++ {
					seed := time.Now().UnixNano() + int64(b*batchSize+i)*7919
					sc := scenariolab.GenerateRandomScenario(seed)
					scenarios[i] = sc

					physicsSpeed := sc.Rules.PhysicsSpeed
					if physicsSpeed <= 0 {
						physicsSpeed = 0.5
					}
					serverDtScale := 1.0 * physicsSpeed
					serverResults[i] = scenariolab.RunScenarioSimulation(sc, serverDtScale)

					clientFps := fpsList[(b*batchSize+i)%len(fpsList)]
					clientDtScale := (60.0 / float64(clientFps)) * physicsSpeed

					scaledImpacts := make([]scenariolab.ImpactRecord, len(serverResults[i].Impacts))
					for j, imp := range serverResults[i].Impacts {
						scaledImpacts[j] = imp
						scaledImpacts[j].Step = int(math.Round(float64(imp.Step) * float64(clientFps) / 60.0))
					}

					items[i] = scenariolab.OverlaySimBatchItem{
						Scenario:      sc,
						ServerImpacts: scaledImpacts,
						ClientFPS:     clientFps,
						ClientDtScale: clientDtScale,
					}
				}
			}(start, end)
		}
		genWg.Wait()

		clientResults, err := pool.RunBatchParallel(items)
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
