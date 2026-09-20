package scenariolab

import (
	"os"
	"path/filepath"
	"testing"
)

func findHeadlessScript() string {
	candidates := []string{
		"../../scripts/headless_sim.mjs",
		"./scripts/headless_sim.mjs",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
			return c
		}
	}
	return "../../scripts/headless_sim.mjs"
}

func TestNodeWorkerPool_LifecycleAndParallelRun(t *testing.T) {
	scriptPath := findHeadlessScript()
	if _, err := os.Stat(scriptPath); err != nil {
		t.Skipf("headless_sim.mjs not found at %s, skipping", scriptPath)
	}

	pool, err := NewNodeWorkerPool(2, scriptPath)
	if err != nil {
		t.Fatalf("failed to create worker pool: %v", err)
	}
	defer pool.Close()

	if pool.Size() != 2 {
		t.Errorf("expected pool size 2, got %d", pool.Size())
	}

	// Test ResetAll
	if err := pool.ResetAll(); err != nil {
		t.Fatalf("ResetAll failed: %v", err)
	}

	// Generate sample scenario items
	sc1 := GenerateRandomScenario(1001)
	sc2 := GenerateRandomScenario(1002)
	sc3 := GenerateRandomScenario(1003)
	sc4 := GenerateRandomScenario(1004)

	items := []OverlaySimBatchItem{
		{Scenario: sc1, ClientFPS: 60, ClientDtScale: 0.5},
		{Scenario: sc2, ClientFPS: 144, ClientDtScale: 0.5 * (60.0 / 144.0)},
		{Scenario: sc3, ClientFPS: 120, ClientDtScale: 0.5 * (60.0 / 120.0)},
		{Scenario: sc4, ClientFPS: 60, ClientDtScale: 0.5},
	}

	// Test RunBatchParallel across the pool
	results, err := pool.RunBatchParallel(items)
	if err != nil {
		t.Fatalf("RunBatchParallel failed: %v", err)
	}

	if len(results) != len(items) {
		t.Fatalf("expected %d results, got %d", len(items), len(results))
	}

	for i, res := range results {
		if len(res.FinalTerrain) == 0 {
			t.Errorf("item %d has empty final terrain", i)
		}
		if len(res.FinalPlayers) == 0 {
			t.Errorf("item %d has empty final players", i)
		}
	}
}
