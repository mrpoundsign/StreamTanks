package scenariolab

import (
	"os"
	"testing"
)

func TestGenerateRandomScenario(t *testing.T) {
	s1 := GenerateRandomScenario(42)
	if s1 == nil {
		t.Fatal("expected non-nil scenario")
	}
	if len(s1.Terrain) != terrainWidth {
		t.Errorf("expected terrain length %d, got %d", terrainWidth, len(s1.Terrain))
	}
	if len(s1.Players) < 2 || len(s1.Players) > 6 {
		t.Errorf("expected between 2 and 6 players, got %d", len(s1.Players))
	}
}

func TestRunScenarioSimulation(t *testing.T) {
	s := GenerateRandomScenario(100)
	res := RunScenarioSimulation(s, 1.0)
	if res == nil {
		t.Fatal("expected simulation result")
	}
	if res.TotalSteps == 0 {
		t.Error("expected non-zero steps executed")
	}
	if len(res.FinalTerrain) != terrainWidth {
		t.Errorf("expected final terrain length %d, got %d", terrainWidth, len(res.FinalTerrain))
	}
	if len(res.FinalPlayers) != len(s.Players) {
		t.Errorf("expected %d final players, got %d", len(s.Players), len(res.FinalPlayers))
	}
}

func TestCompareSimulationResults_Identical(t *testing.T) {
	s := GenerateRandomScenario(200)
	res1 := RunScenarioSimulation(s, 1.0)
	res2 := RunScenarioSimulation(s, 1.0)

	report := CompareSimulationResults(res1, res2)
	if report.HasDiscrepancy {
		t.Errorf("expected identical runs to have no discrepancy, got: %s", report.Summary)
	}
	if report.MaxTerrainDiff != 0 {
		t.Errorf("expected 0 terrain diff, got %f", report.MaxTerrainDiff)
	}
	if len(report.KillMismatches) != 0 {
		t.Errorf("expected 0 kill mismatches, got %v", report.KillMismatches)
	}
}

func TestSaveAndLoadScenario(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "scenariotest-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	s := GenerateRandomScenario(300)
	savedPath, err := SaveScenario(tempDir, s, nil)
	if err != nil {
		t.Fatalf("failed to save scenario: %v", err)
	}

	loaded, err := LoadScenario(savedPath)
	if err != nil {
		t.Fatalf("failed to load scenario: %v", err)
	}

	if loaded.ID != s.ID {
		t.Errorf("expected scenario ID %s, got %s", s.ID, loaded.ID)
	}
	if len(loaded.Players) != len(s.Players) {
		t.Errorf("expected %d players, got %d", len(s.Players), len(loaded.Players))
	}
	if len(loaded.Terrain) != len(s.Terrain) {
		t.Errorf("expected %d terrain points, got %d", len(s.Terrain), len(loaded.Terrain))
	}
}
