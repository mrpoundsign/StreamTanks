package app

import "sync"

// Game phases
const (
	phaseIdle        = "IDLE"
	phaseInput       = "INPUT"
	phaseAction      = "ACTION"
	phaseCelebration = "CELEBRATION"
)

// WebSocket message types
const (
	msgStateUpdate         = "STATE_UPDATE"
	msgExecuteActions      = "EXECUTE_ACTIONS"
	msgPlayerLocked        = "PLAYER_LOCKED"
	msgResetTerrain        = "RESET_TERRAIN"
	msgActionComplete      = "ACTION_COMPLETE"
	msgPlayerDied          = "PLAYER_DIED"
	msgGameOver            = "GAME_OVER"
	msgCelebrationComplete = "CELEBRATION_COMPLETE"
	msgChatCommand         = "CHAT_COMMAND"
	msgDebugCommand        = "DEBUG_COMMAND"
	msgTerrainCrater       = "TERRAIN_CRATER"
)

// Player actions
const (
	actionFire  = "FIRE"
	actionLeft  = "LEFT"
	actionRight = "RIGHT"
)

// Player models an individual artillery tank in the game
type Player struct {
	Name       string  `json:"name"`
	Emote      string  `json:"emote"`
	EmoteURL   string  `json:"emoteUrl"`
	LastAngle  int     `json:"lastAngle"`
	LastPower  int     `json:"lastPower"`
	ActionType string  `json:"actionType"`
	Fired      bool    `json:"fired"`
	Angle      int     `json:"angle"`
	Power      int     `json:"power"`
	IsDead          bool    `json:"isDead"`
	IsBot           bool    `json:"isBot"`
	X               float64 `json:"x"`
	Y               float64 `json:"y"`
	MoveTarget      float64 `json:"moveTarget,omitempty"`
	Moving          bool    `json:"moving,omitempty"`
	SpeedMultiplier float64 `json:"speedMultiplier,omitempty"`
	HasBounced      bool    `json:"hasBounced,omitempty"`
	LastActiveRound int     `json:"lastActiveRound"`
}

// Projectile represents a tank shell in flight
type Projectile struct {
	ID       string  `json:"id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	Owner    string  `json:"owner"`
	EmoteURL string  `json:"emoteUrl"`
	Bounces  int     `json:"bounces,omitempty"`
}

// Explosion represents a visual blast or spark
type Explosion struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Radius    float64 `json:"radius"`
	MaxRadius float64 `json:"maxRadius"`
	Alpha     float64 `json:"alpha"`
	IsSpark   bool    `json:"isSpark"`
}

// GameState holds all synchronized state for active players, phases, and settings
type GameState struct {
	mu            sync.Mutex
	Phase         string             `json:"phase"`
	Players       map[string]*Player `json:"players"`
	InputDuration int                `json:"inputDuration"` // in seconds
	MoveDistance  int                `json:"moveDistance"`
	Leaderboard   map[string]int     `json:"leaderboard"`
	Debug         bool               `json:"debug"`
	Prefix        string             `json:"prefix"`
	PhysicsSpeed  float64            `json:"physicsSpeed"`
	ShowConfig    bool               `json:"showConfig"`
	AutoRound     int                `json:"autoRound"` // -1: immediate, >0: minutes, 0: off
	IdleMessage   bool               `json:"idleMessage"`
	BouncyWalls   bool               `json:"bouncyWalls"`
	Terrain       []float64          `json:"terrain,omitempty"`
	TerrainMin    int                `json:"terrainMin"` // minimum screen height percentage (e.g. 25%)
	TerrainMax    int                `json:"terrainMax"` // maximum screen height percentage (e.g. 80%)
	RoundID       int                `json:"roundId"`
	StartPerm     string             `json:"startPerm"`  // broadcaster, mod, vip, sub, all
	ConfigPerm    string             `json:"configPerm"` // broadcaster, mod, vip, sub, all
	MinPlayers    int                `json:"minPlayers"` // minimum player count target for bot fill (default: 5)
	BotFill       bool               `json:"botFill"`    // whether to fill empty slots up to MinPlayers with bots
	BotPoints     int                `json:"botPoints"`  // points awarded when human kills a bot (default: 1)
	BotList       []string           `json:"botList"`    // list of named bots to spawn before nameless bots
	CCEnabled     bool               `json:"ccEnabled"`
	CCServerURL   string             `json:"ccServerUrl"`
	CCStatus      string             `json:"ccStatus"` // "disconnected", "connected", "pending_claim", "connecting"
	ClaimCode     string             `json:"claimCode,omitempty"`
	Winner         string             `json:"winner"`         // winner of the current match ("Alice", "AI", or "")
	TimerRemaining int                `json:"timerRemaining,omitempty"` // remaining input seconds when clock is truncated
	MatchKills     []KillEvent        `json:"matchKills,omitempty"`
	Projectiles    []Projectile       `json:"projectiles"`
	Explosions     []Explosion        `json:"explosions"`
}

var defaultBotList = []string{"TargetBot", "RustyTank", "IronClad", "CyberDrone", "MechaUnit"}

// KillEvent records elimination details for post-game recap and future replays (#37)
type KillEvent struct {
	Killer      string  `json:"killer,omitempty"`
	KillerIsBot bool    `json:"killerIsBot,omitempty"`
	Victim      string  `json:"victim"`
	VictimIsBot bool    `json:"victimIsBot"`
	Angle       int     `json:"angle,omitempty"`
	Power       int     `json:"power,omitempty"`
	ImpactX     float64 `json:"impactX,omitempty"`
	ImpactY     float64 `json:"impactY,omitempty"`
	RoundID     int     `json:"roundId,omitempty"`
	Timestamp   int64   `json:"timestamp,omitempty"`
}

// CraterPayload carries crater coordinates and radius for terrain deformation
type CraterPayload struct {
	ID     string  `json:"id,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
}

// PlayerDiedPayload conveys death event information including the killer for point attribution
type PlayerDiedPayload struct {
	Victim      string `json:"victim"`
	VictimIsBot bool   `json:"victimIsBot,omitempty"`
	Killer      string `json:"killer,omitempty"`
	KillerIsBot bool   `json:"killerIsBot,omitempty"`
}

// WSMessage is the generic envelope sent over WebSocket
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// defaultEmotes provides standard Twitch emotes for players without a custom emote
var defaultEmotes = []struct {
	Name string
	URL  string
}{
	{"Kappa", "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0"},
	{"LUL", "https://static-cdn.jtvnw.net/emoticons/v2/425618/default/dark/2.0"},
	{"PogChamp", "https://static-cdn.jtvnw.net/emoticons/v2/305954156/default/dark/2.0"},
	{"GlitchCat", "https://static-cdn.jtvnw.net/emoticons/v2/112290/default/dark/2.0"},
}
