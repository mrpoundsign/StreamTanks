package main

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
)

// Player actions
const (
	actionFire  = "FIRE"
	actionLeft  = "LEFT"
	actionRight = "RIGHT"
)

// Player models an individual artillery tank in the game
type Player struct {
	Name       string `json:"name"`
	Emote      string `json:"emote"`
	EmoteURL   string `json:"emoteUrl"`
	LastAngle  int    `json:"lastAngle"`
	LastPower  int    `json:"lastPower"`
	ActionType string `json:"actionType"`
	Fired      bool   `json:"fired"`
	Angle      int    `json:"angle"`
	Power      int    `json:"power"`
	IsDead     bool   `json:"isDead"`
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
	{"PogChamp", "https://static-cdn.jtvnw.net/emoticons/v2/88/default/dark/2.0"},
	{"GlitchCat", "https://static-cdn.jtvnw.net/emoticons/v2/112290/default/dark/2.0"},
}
