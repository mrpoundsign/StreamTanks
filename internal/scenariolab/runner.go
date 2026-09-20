package scenariolab

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"streamtanks/internal/app"
)

// memoryEventSink implements app.CollisionSink in-memory for scenario testing.
// In idiomatic Go, this sink satisfies the interface declared by app.Engine without touching SQLite or WebSockets.
type memoryEventSink struct {
	impacts []ImpactRecord
	kills   []KillRecord
}

func (m *memoryEventSink) OnCrater(cx, cy, radius float64, shotID string) {
	m.impacts = append(m.impacts, ImpactRecord{
		ID:      shotID,
		X:       cx,
		Y:       cy,
		Radius:  radius,
		HitType: "terrain",
	})
}

func (m *memoryEventSink) OnKill(victim, killer string, cx, cy float64, isBot bool) {
	reason := "blast"
	if killer == "" {
		reason = "abyss"
	}
	m.kills = append(m.kills, KillRecord{
		Killer: killer,
		Victim: victim,
		Reason: reason,
	})
}

func (m *memoryEventSink) OnSpark(x, y float64) {}

// RunScenarioSimulation converts the scenario to an app.Engine and simulates to steady state.
func RunScenarioSimulation(s *Scenario, dtScale float64) *SimulationResult {
	if dtScale <= 0 {
		dtScale = 1.0
	}

	terrain := make([]float64, len(s.Terrain))
	copy(terrain, s.Terrain)

	players := make(map[string]*app.Player, len(s.Players))
	for k, v := range s.Players {
		players[k] = &app.Player{
			Name:            v.Name,
			X:               v.X,
			Y:               v.Y,
			Angle:           v.Angle,
			Power:           v.Power,
			ActionType:      v.ActionType,
			IsBot:           v.IsBot,
			IsShielded:      v.IsShielded,
			IsDead:          v.IsDead,
			SpeedMultiplier: 1.0,
		}
	}

	engine := app.NewEngine(terrain, players, s.Rules.BouncyWalls, s.Rules.TerrainClimb, s.Rules.MoveDistance, s.Rules.BotPoints)

	// Spawning initial projectiles & movement
	for name, p := range engine.Players {
		if p.IsDead {
			continue
		}
		switch p.ActionType {
		case "FIRE":
			rad := float64(p.Angle) * math.Pi / 180.0
			powerClamped := math.Min(math.Max(float64(p.Power), 1.0), 100.0)
			powerScaled := powerClamped / 5.0
			vx := math.Cos(rad) * powerScaled
			vy := -math.Sin(rad) * powerScaled
			shotID := fmt.Sprintf("sim_%s", name)
			muzzleDist := 25.0
			spawnX := p.X + math.Cos(rad)*muzzleDist
			spawnY := p.Y - 10.0 - math.Sin(rad)*muzzleDist

			engine.Projectiles = append(engine.Projectiles, app.Projectile{
				ID:    shotID,
				X:     spawnX,
				Y:     spawnY,
				VX:    vx,
				VY:    vy,
				Owner: name,
			})
		case "LEFT":
			p.MoveTarget = p.X - float64(s.Rules.MoveDistance)
			p.Moving = true
			p.SpeedMultiplier = 1.0
			p.HasBounced = false
		case "RIGHT":
			p.MoveTarget = p.X + float64(s.Rules.MoveDistance)
			p.Moving = true
			p.SpeedMultiplier = 1.0
			p.HasBounced = false
		case "SHIELD":
			p.Moving = false
			p.IsShielded = true
		}
	}

	sink := &memoryEventSink{
		impacts: make([]ImpactRecord, 0),
		kills:   make([]KillRecord, 0),
	}
	step := 0
	maxSteps := 2000

	for step < maxSteps {
		step++
		isDone := engine.Step(dtScale, sink)
		if isDone {
			break
		}
	}

	finalPlayers := make(map[string]FinalPlayerState, len(engine.Players))
	aliveCount := 0
	winner := ""
	for name, p := range engine.Players {
		finalPlayers[name] = FinalPlayerState{
			X:      p.X,
			Y:      p.Y,
			IsDead: p.IsDead,
		}
		if !p.IsDead {
			aliveCount++
			winner = name
		}
	}
	if aliveCount > 1 {
		winner = "DRAW"
	} else if aliveCount == 0 {
		winner = "NONE"
	}

	kills := sink.kills
	if kills == nil {
		kills = make([]KillRecord, 0)
	}
	impacts := sink.impacts
	if impacts == nil {
		impacts = make([]ImpactRecord, 0)
	}

	return &SimulationResult{
		Kills:        kills,
		Impacts:      impacts,
		FinalTerrain: engine.Terrain,
		FinalPlayers: finalPlayers,
		Winner:       winner,
		TotalSteps:   step,
	}
}

// CompareSimulationResults evaluates whether server and overlay simulations match or diverge.
func CompareSimulationResults(serverRes, clientRes *SimulationResult) *DiffReport {
	report := &DiffReport{
		ServerResult:  serverRes,
		OverlayResult: clientRes,
	}

	var mismatches []string

	// 1. Compare Kills
	serverKills := make(map[string]bool)
	for _, k := range serverRes.Kills {
		serverKills[k.Victim] = true
	}
	clientKills := make(map[string]bool)
	for _, k := range clientRes.Kills {
		clientKills[k.Victim] = true
	}

	for v := range serverKills {
		if !clientKills[v] {
			mismatches = append(mismatches, fmt.Sprintf("Missed Kill: Server killed '%s', but Overlay did not", v))
		}
	}
	for v := range clientKills {
		if !serverKills[v] {
			mismatches = append(mismatches, fmt.Sprintf("Ghost Kill: Overlay killed '%s', but Server did not", v))
		}
	}

	// 2. Compare Terrain
	maxTerrainDiff := 0.0
	terrainDiffCount := 0
	limit := min(len(clientRes.FinalTerrain), len(serverRes.FinalTerrain))
	for i := range limit {
		d := math.Abs(serverRes.FinalTerrain[i] - clientRes.FinalTerrain[i])
		if d > 0.15 {
			terrainDiffCount++
			if d > maxTerrainDiff {
				maxTerrainDiff = d
			}
		}
	}

	// 3. Compare Tank Final Positions
	maxPosDiff := 0.0
	for name, sP := range serverRes.FinalPlayers {
		cP, exists := clientRes.FinalPlayers[name]
		if !exists {
			mismatches = append(mismatches, fmt.Sprintf("Player '%s' missing from Overlay results", name))
			continue
		}
		if sP.IsDead != cP.IsDead {
			mismatches = append(mismatches, fmt.Sprintf("Player '%s' alive status mismatch (Server: dead=%v, Overlay: dead=%v)", name, sP.IsDead, cP.IsDead))
		}
		d := math.Hypot(sP.X-cP.X, sP.Y-cP.Y)
		if d > maxPosDiff {
			maxPosDiff = d
		}
	}

	report.KillMismatches = mismatches
	report.MaxTerrainDiff = maxTerrainDiff
	report.TerrainDiffCount = terrainDiffCount
	report.MaxPositionDiff = maxPosDiff

	hasDisc := len(mismatches) > 0 || terrainDiffCount > 5 || maxPosDiff > 2.0
	report.HasDiscrepancy = hasDisc

	if hasDisc {
		var reasons []string
		if len(mismatches) > 0 {
			reasons = append(reasons, fmt.Sprintf("%d kill/death mismatches", len(mismatches)))
		}
		if terrainDiffCount > 0 {
			reasons = append(reasons, fmt.Sprintf("terrain drift (max %.1fpx across %d columns)", maxTerrainDiff, terrainDiffCount))
		}
		if maxPosDiff > 2.0 {
			reasons = append(reasons, fmt.Sprintf("tank position drift (max %.1fpx)", maxPosDiff))
		}
		report.Summary = strings.Join(reasons, ", ")
	} else {
		report.Summary = "PERFECT MATCH: 0 discrepancies"
	}

	return report
}

// SaveScenario saves a scenario and its associated diff report to disk as JSON.
func SaveScenario(dir string, s *Scenario, diff *DiffReport) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("mismatch_%s.json", s.ID)
	fullPath := filepath.Join(dir, filename)

	payload := struct {
		Scenario   *Scenario   `json:"scenario"`
		DiffReport *DiffReport `json:"diffReport,omitempty"`
	}{
		Scenario:   s,
		DiffReport: diff,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", err
	}
	return fullPath, nil
}

// LoadScenario reads a scenario JSON file from disk.
func LoadScenario(filePath string) (*Scenario, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		for _, b := range getBuiltInScenarios() {
			if b.ID == filePath {
				return b, nil
			}
		}
		return nil, err
	}

	var wrapper struct {
		Scenario *Scenario `json:"scenario"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Scenario != nil {
		return wrapper.Scenario, nil
	}

	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
