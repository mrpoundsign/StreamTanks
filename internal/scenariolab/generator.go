package scenariolab

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

const (
	terrainWidth  = 1920
	terrainHeight = 1080
	actionFire    = "FIRE"
	actionLeft    = "LEFT"
	actionRight   = "RIGHT"
	actionShield  = "SHIELD"
)

// GenerateRandomScenario creates a deterministic scenario from a seed with realistic terrain and 2-6 tanks.
func GenerateRandomScenario(seed int64) *Scenario {
	r := rand.New(rand.NewPCG(uint64(seed), uint64(seed^0x5DEECE66D)))

	terrain := make([]float64, terrainWidth)
	minPct := 20 + r.IntN(20) // 20-39%
	maxPct := 65 + r.IntN(20) // 65-84%
	minY := float64(terrainHeight) * (1.0 - float64(maxPct)/100.0)
	maxY := float64(terrainHeight) * (1.0 - float64(minPct)/100.0)
	midY := (minY + maxY) / 2.0
	halfSpan := (maxY - minY) / 4.0

	y := midY + (r.Float64()*halfSpan*2.0 - halfSpan)
	slope := 0.0
	terrain[0] = math.Round(y*10) / 10

	for x := 1; x < terrainWidth; x++ {
		slope += (r.Float64() - 0.5) * 0.15
		if slope > 2.0 {
			slope = 2.0
		}
		if slope < -2.0 {
			slope = -2.0
		}
		y += slope
		if y < minY+40.0 {
			slope += 0.08
		}
		if y > maxY-40.0 {
			slope -= 0.08
		}
		if y < minY {
			y = minY
			if slope < 0 {
				slope = 0
			}
		} else if y > maxY {
			y = maxY
			if slope > 0 {
				slope = 0
			}
		}
		terrain[x] = math.Round(y*10) / 10
	}

	numPlayers := 2 + r.IntN(5) // 2 to 6 players
	players := make(map[string]ScenarioPlayer, numPlayers)
	actions := []string{actionFire, actionFire, actionFire, actionLeft, actionRight, actionShield}

	for i := range numPlayers {
		name := fmt.Sprintf("Tank_%d", i+1)
		span := float64(terrainWidth-200) / float64(numPlayers)
		posX := 100.0 + float64(i)*span + r.Float64()*(span-50.0)
		if posX < 50.0 {
			posX = 50.0
		}
		if posX > float64(terrainWidth)-50.0 {
			posX = float64(terrainWidth) - 50.0
		}
		posY := getTerrainHeight(terrain, posX)

		act := actions[r.IntN(len(actions))]
		angle := 20 + r.IntN(140)
		power := 25 + r.IntN(75)

		players[name] = ScenarioPlayer{
			Name:       name,
			X:          posX,
			Y:          posY,
			Angle:      angle,
			Power:      power,
			ActionType: act,
			IsBot:      i > 0,
			IsShielded: act == actionShield,
		}
	}

	return &Scenario{
		ID:   fmt.Sprintf("rnd_%d_%d", seed, time.Now().Unix()),
		Name: fmt.Sprintf("Random Scenario (Seed %d)", seed),
		Seed: seed,
		Rules: ScenarioRules{
			BouncyWalls:  r.IntN(2) == 1,
			TerrainClimb: r.IntN(2) == 1,
			MoveDistance: 100,
			PhysicsSpeed: 0.5,
			BotPoints:    1,
		},
		Terrain: terrain,
		Players: players,
	}
}

// Built-in Edge-Case Scenarios
func getBuiltInScenarios() []*Scenario {
	flatTerrain := make([]float64, terrainWidth)
	for i := range flatTerrain {
		flatTerrain[i] = 650.0
	}

	craterTerrain := make([]float64, terrainWidth)
	for i := range craterTerrain {
		craterTerrain[i] = 600.0
	}
	applyCrater(craterTerrain, 500, 600, 80)

	return []*Scenario{
		{
			ID:          "builtin_near_miss_boundary",
			Name:        "Near Miss Boundary (Blast 70px Threshold)",
			Description: "Shell explodes exactly 69px vs 71px from adjacent tank. Tests blast radius boundary edge condition.",
			Rules: ScenarioRules{
				BouncyWalls:  false,
				TerrainClimb: false,
				MoveDistance: 100,
				PhysicsSpeed: 0.5,
				BotPoints:    1,
			},
			Terrain: flatTerrain,
			Players: map[string]ScenarioPlayer{
				"Shooter": {
					Name:       "Shooter",
					X:          200,
					Y:          650,
					Angle:      45,
					Power:      50,
					ActionType: actionFire,
				},
				"TargetNear": {
					Name:       "TargetNear",
					X:          650,
					Y:          650,
					ActionType: actionShield,
					IsShielded: false,
				},
			},
		},
		{
			ID:          "builtin_direct_hit",
			Name:        "Direct Hull Hit",
			Description: "Shell collides directly with tank hull within 20px direct hit radius.",
			Rules: ScenarioRules{
				BouncyWalls:  false,
				TerrainClimb: false,
				MoveDistance: 100,
				PhysicsSpeed: 0.5,
				BotPoints:    1,
			},
			Terrain: flatTerrain,
			Players: map[string]ScenarioPlayer{
				"Artillery": {
					Name:       "Artillery",
					X:          300,
					Y:          650,
					Angle:      55,
					Power:      60,
					ActionType: actionFire,
				},
				"Target": {
					Name:       "Target",
					X:          850,
					Y:          650,
					ActionType: "",
				},
			},
		},
		{
			ID:          "builtin_shield_deflection",
			Name:        "Active Shield Absorption",
			Description: "Defender activates shield (%shield). Shell collides with 45px shield bubble before terrain impact.",
			Rules: ScenarioRules{
				BouncyWalls:  false,
				TerrainClimb: false,
				MoveDistance: 100,
				PhysicsSpeed: 0.5,
				BotPoints:    1,
			},
			Terrain: flatTerrain,
			Players: map[string]ScenarioPlayer{
				"Attacker": {
					Name:       "Attacker",
					X:          300,
					Y:          650,
					Angle:      45,
					Power:      55,
					ActionType: actionFire,
				},
				"Guardian": {
					Name:       "Guardian",
					X:          700,
					Y:          650,
					ActionType: actionShield,
					IsShielded: true,
				},
			},
		},
		{
			ID:          "builtin_wall_bounce_ricochet",
			Name:        "Wall Bounce Ricochet (+10% Boost)",
			Description: "Bouncy walls enabled. Shell ricochets off right wall and impacts tank on rebound.",
			Rules: ScenarioRules{
				BouncyWalls:  true,
				TerrainClimb: false,
				MoveDistance: 100,
				PhysicsSpeed: 0.5,
				BotPoints:    1,
			},
			Terrain: flatTerrain,
			Players: map[string]ScenarioPlayer{
				"TrickShot": {
					Name:       "TrickShot",
					X:          1700,
					Y:          650,
					Angle:      30,
					Power:      85,
					ActionType: actionFire,
				},
				"Victim": {
					Name:       "Victim",
					X:          1500,
					Y:          650,
					ActionType: "",
				},
			},
		},
		{
			ID:          "builtin_steep_crater_climb",
			Name:        "Steep Crater Rim Climb (Blocked vs Allowed)",
			Description: "Tanks move left/right into a pre-existing 80px crater wall. Tests slope climb restriction.",
			Rules: ScenarioRules{
				BouncyWalls:  false,
				TerrainClimb: false,
				MoveDistance: 150,
				PhysicsSpeed: 0.5,
				BotPoints:    1,
			},
			Terrain: craterTerrain,
			Players: map[string]ScenarioPlayer{
				"Climber": {
					Name:       "Climber",
					X:          500,
					Y:          680,
					ActionType: actionRight,
				},
			},
		},
	}
}

func getTerrainHeight(terrain []float64, x float64) float64 {
	if len(terrain) == 0 {
		return float64(terrainHeight) / 2.0
	}
	idx := int(math.Floor(x))
	if idx < 0 {
		idx = 0
	} else if idx >= len(terrain) {
		idx = len(terrain) - 1
	}
	return terrain[idx]
}

func applyCrater(terrain []float64, cx, cy, radius float64) {
	if len(terrain) == 0 || radius <= 0 {
		return
	}
	startX := max(0, int(math.Floor(cx-radius)))
	endX := min(len(terrain), int(math.Ceil(cx+radius)))
	r2 := radius * radius
	lowestY := cy + radius

	for x := startX; x < endX; x++ {
		if terrain[x] >= lowestY {
			continue
		}
		dx := float64(x) - cx
		dx2 := dx * dx
		if dx2 > r2 {
			continue
		}
		dy := math.Sqrt(r2 - dx2)
		circleBottomY := cy + dy
		if terrain[x] < circleBottomY {
			terrain[x] = math.Round(circleBottomY*10) / 10
		}
	}
}
