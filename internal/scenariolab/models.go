package scenariolab

// Scenario defines the initial state, terrain, and player actions of a test scenario.
type Scenario struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Seed        int64                     `json:"seed,omitempty"`
	Rules       ScenarioRules             `json:"rules"`
	Terrain     []float64                 `json:"terrain"`
	Players     map[string]ScenarioPlayer `json:"players"`
}

// ScenarioRules encapsulates match settings.
type ScenarioRules struct {
	BouncyWalls  bool    `json:"bouncyWalls"`
	TerrainClimb bool    `json:"terrainClimb"`
	MoveDistance int     `json:"moveDistance"`
	PhysicsSpeed float64 `json:"physicsSpeed"`
	BotPoints    int     `json:"botPoints"`
}

// ScenarioPlayer represents an individual tank's state and action in a scenario.
type ScenarioPlayer struct {
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Angle      int     `json:"angle"`
	Power      int     `json:"power"`
	ActionType string  `json:"actionType"` // "FIRE", "LEFT", "RIGHT", "SHIELD"
	IsBot      bool    `json:"isBot"`
	IsShielded bool    `json:"isShielded"`
	IsDead     bool    `json:"isDead"`
}

// SimulationResult holds the complete result of running a scenario.
type SimulationResult struct {
	Kills        []KillRecord                `json:"kills"`
	Impacts      []ImpactRecord              `json:"impacts"`
	FinalTerrain []float64                   `json:"finalTerrain"`
	FinalPlayers map[string]FinalPlayerState `json:"finalPlayers"`
	Winner       string                      `json:"winner"`
	TotalSteps   int                         `json:"totalSteps"`
}

// ImpactRecord records a projectile impact.
type ImpactRecord struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Radius  float64 `json:"radius"`
	Owner   string  `json:"owner"`
	HitType string  `json:"hitType"` // "terrain", "tank", "shield"
}

// KillRecord records a tank elimination.
type KillRecord struct {
	Killer string  `json:"killer"`
	Victim string  `json:"victim"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Reason string  `json:"reason"` // "blast", "abyss"
}

// FinalPlayerState records end-of-round coordinates and alive status.
type FinalPlayerState struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	IsDead bool    `json:"isDead"`
}

// DiffReport documents any divergence between server and overlay simulations.
type DiffReport struct {
	ScenarioID       string            `json:"scenarioId"`
	HasDiscrepancy   bool              `json:"hasDiscrepancy"`
	KillMismatches   []string          `json:"killMismatches,omitempty"`
	MaxTerrainDiff   float64           `json:"maxTerrainDiff"`
	TerrainDiffCount int               `json:"terrainDiffCount"`
	MaxPositionDiff  float64           `json:"maxPositionDiff"`
	ServerResult     *SimulationResult `json:"serverResult"`
	OverlayResult    *SimulationResult `json:"overlayResult"`
	Summary          string            `json:"summary"`
}
