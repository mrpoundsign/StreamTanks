package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
	"golang.org/x/net/websocket"
	_ "modernc.org/sqlite"
)

// Game phases
const (
	PhaseIdle        = "IDLE"
	PhaseInput       = "INPUT"
	PhaseAction      = "ACTION"
	PhaseCelebration = "CELEBRATION"
)

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

type GameState struct {
	mu            sync.Mutex
	Phase         string             `json:"phase"`
	Players       map[string]*Player `json:"players"`
	InputDuration int                `json:"inputDuration"` // in seconds
	MoveDistance  int                `json:"moveDistance"`
	Leaderboard   map[string]int     `json:"leaderboard"`
	Debug         bool               `json:"debug"`
	Prefix        string             `json:"prefix"`
}

var gameState = GameState{
	Phase:         PhaseIdle,
	Players:       make(map[string]*Player),
	InputDuration: 20,
	MoveDistance:  100,
	Leaderboard:   make(map[string]int),
	Prefix:        "%",
}

var (
	clientsMu     sync.RWMutex
	activeClients = make(map[*websocket.Conn]bool)
)

var db *sql.DB

var defaultEmotes = []struct {
	Name string
	URL  string
}{
	{"Kappa", "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0"},
	{"LUL", "https://static-cdn.jtvnw.net/emoticons/v2/425618/default/dark/2.0"},
	{"PogChamp", "https://static-cdn.jtvnw.net/emoticons/v2/88/default/dark/2.0"},
	{"GlitchCat", "https://static-cdn.jtvnw.net/emoticons/v2/112290/default/dark/2.0"},
}

// WSMessage is the generic message sent over websocket
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

func loadLeaderboard() {
	if db == nil {
		return
	}
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	rows, err := db.Query(`SELECT username, wins FROM leaderboard`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			var wins int
			if err := rows.Scan(&name, &wins); err == nil {
				gameState.Leaderboard[name] = wins
			}
		}
	}
}

func incrementWin(username string) {
	if db == nil {
		return
	}
	_, err := db.Exec(`INSERT INTO leaderboard (username, wins) VALUES (?, 1) ON CONFLICT(username) DO UPDATE SET wins = wins + 1`, username)
	if err != nil {
		log.Println("DB error:", err)
	}
}

func broadcast(msgType string, payload interface{}) {
	payloadCopy := payload

	if payload == &gameState {
		gameState.mu.Lock()
		playersCopy := make(map[string]*Player, len(gameState.Players))
		for k, v := range gameState.Players {
			pCopy := *v
			playersCopy[k] = &pCopy
		}
		lbCopy := make(map[string]int, len(gameState.Leaderboard))
		for k, v := range gameState.Leaderboard {
			lbCopy[k] = v
		}
		stateCopy := &GameState{
			Phase:         gameState.Phase,
			Players:       playersCopy,
			InputDuration: gameState.InputDuration,
			MoveDistance:  gameState.MoveDistance,
			Leaderboard:   lbCopy,
			Debug:         gameState.Debug,
			Prefix:        gameState.Prefix,
		}
		gameState.mu.Unlock()
		payloadCopy = stateCopy
	}

	msg := WSMessage{Type: msgType, Payload: payloadCopy}

	clientsMu.RLock()
	conns := make([]*websocket.Conn, 0, len(activeClients))
	for conn := range activeClients {
		conns = append(conns, conn)
	}
	clientsMu.RUnlock()

	for _, conn := range conns {
		err := websocket.JSON.Send(conn, msg)
		if err != nil {
			log.Printf("Error sending to client: %v", err)
			clientsMu.Lock()
			delete(activeClients, conn)
			clientsMu.Unlock()
			_ = conn.Close()
		}
	}
}

var (
	channelFlag = flag.String("channel", "", "Twitch channel to join (optional)")
	listenAddr  = flag.String("addr", ":8080", "HTTP listen address")
	debugMode   = flag.Bool("debug", false, "Enable debug mode with a test bot for single-player testing")
)

func getDebugUsername() string {
	if channelFlag != nil && *channelFlag != "" {
		return *channelFlag
	}
	return "Player1"
}

func handleWebSocket(ws *websocket.Conn) {
	clientsMu.Lock()
	activeClients[ws] = true
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(activeClients, ws)
		clientsMu.Unlock()
		_ = ws.Close()
	}()

	log.Println("New WebSocket client connected (Overlay)")

	// Send initial state
	broadcast("STATE_UPDATE", &gameState)

	// Listen for messages from frontend
	for {
		var msg WSMessage
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			log.Println("WebSocket disconnected")
			break
		}

		switch msg.Type {
		case "ACTION_COMPLETE":
			startInputPhase()
		case "PLAYER_DIED":
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var deadPlayer string
				if err := json.Unmarshal(payloadBytes, &deadPlayer); err == nil {
					gameState.mu.Lock()
					if p, exists := gameState.Players[deadPlayer]; exists {
						p.IsDead = true
					}
					gameState.mu.Unlock()
					broadcast("STATE_UPDATE", &gameState)
				}
			}
		case "GAME_OVER":
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var winner string
				if err := json.Unmarshal(payloadBytes, &winner); err == nil {
					gameState.mu.Lock()
					gameState.Phase = PhaseCelebration
					if winner != "" {
						gameState.Leaderboard[winner]++
						incrementWin(winner)
					}
					gameState.mu.Unlock()
					broadcast("STATE_UPDATE", &gameState)

					// Safety fallback: if no CELEBRATION_COMPLETE arrives within 12s, reset cleanly
					go func() {
						time.Sleep(12 * time.Second)
						gameState.mu.Lock()
						if gameState.Phase == PhaseCelebration {
							gameState.Phase = PhaseIdle
							for _, p := range gameState.Players {
								p.IsDead = false
								p.Fired = false
								p.ActionType = ""
							}
							gameState.mu.Unlock()
							broadcast("STATE_UPDATE", &gameState)
							broadcast("RESET_TERRAIN", nil)
						} else {
							gameState.mu.Unlock()
						}
					}()
				}
			}
		case "CELEBRATION_COMPLETE":
			gameState.mu.Lock()
			if gameState.Phase == PhaseCelebration {
				gameState.Phase = PhaseIdle
				// Revive all players for the next game
				for _, p := range gameState.Players {
					p.IsDead = false
					p.Fired = false
					p.ActionType = ""
				}
				gameState.mu.Unlock()
				broadcast("STATE_UPDATE", &gameState)
				broadcast("RESET_TERRAIN", nil)
			} else {
				gameState.mu.Unlock()
			}
		case "CHAT_COMMAND":
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var cmdStr string
				if err := json.Unmarshal(payloadBytes, &cmdStr); err == nil {
					processCommand(getDebugUsername(), cmdStr, nil)
				}
			}
		}
	}
}

var inputCancel chan struct{}

func startInputPhase() {
	gameState.mu.Lock()
	gameState.Phase = PhaseInput
	// Reset fired status and action for all players
	for _, p := range gameState.Players {
		p.Fired = false
		p.ActionType = ""
	}
	if inputCancel != nil {
		close(inputCancel)
	}
	inputCancel = make(chan struct{})
	cancelChan := inputCancel

	// In debug mode, auto-ready the TargetBot after 1s so single player can test
	if gameState.Debug {
		if bot, exists := gameState.Players["TargetBot"]; exists && !bot.IsDead {
			go func() {
				time.Sleep(1 * time.Second)
				gameState.mu.Lock()
				if gameState.Phase == PhaseInput && !bot.IsDead {
					bot.Fired = true
					bot.ActionType = "LEFT"
					checkAllPlayersFired()
					gameState.mu.Unlock()
					broadcast("STATE_UPDATE", &gameState)
				} else {
					gameState.mu.Unlock()
				}
			}()
		}
	}
	gameState.mu.Unlock()

	broadcast("STATE_UPDATE", &gameState)

	// Start timer for input phase
	go func() {
		select {
		case <-time.After(time.Duration(gameState.InputDuration) * time.Second):
			executeActionPhase()
		case <-cancelChan:
			return
		}
	}()
}

func checkAllPlayersFired() {
	// Assumes gameState.mu is held
	if gameState.Phase != PhaseInput || inputCancel == nil {
		return
	}
	alivePlayers := 0
	for _, p := range gameState.Players {
		if !p.IsDead {
			alivePlayers++
			if !p.Fired {
				return
			}
		}
	}
	if alivePlayers > 0 {
		close(inputCancel)
		inputCancel = nil
		go func() {
			time.Sleep(500 * time.Millisecond)
			executeActionPhase()
		}()
	}
}

func executeActionPhase() {
	gameState.mu.Lock()
	if gameState.Phase != PhaseInput {
		gameState.mu.Unlock()
		return
	}
	if inputCancel != nil {
		close(inputCancel)
		inputCancel = nil
	}
	gameState.Phase = PhaseAction

	// Apply last known values for those who didn't fire
	for _, p := range gameState.Players {
		if p.IsDead {
			continue
		}
		if !p.Fired || p.ActionType == "" {
			if time.Now().UnixNano()%2 == 0 {
				p.ActionType = "LEFT"
			} else {
				p.ActionType = "RIGHT"
			}
			p.Fired = true
		}
	}
	gameState.mu.Unlock()

	// Send state update which tells frontend to execute the shots/moves
	broadcast("STATE_UPDATE", &gameState)
	broadcast("EXECUTE_ACTIONS", nil)
}

func processCommand(username string, msg string, emotes []*twitch.Emote) {
	gameState.mu.Lock()

	// Ensure player exists in state
	if _, exists := gameState.Players[username]; !exists {
		randIdx := time.Now().UnixNano() % int64(len(defaultEmotes))
		if randIdx < 0 {
			randIdx = -randIdx
		}
		defEmote := defaultEmotes[randIdx]
		gameState.Players[username] = &Player{
			Name:      username,
			Emote:     defEmote.Name,
			EmoteURL:  defEmote.URL,
			LastAngle: 45,
			LastPower: 50,
		}
		gameState.mu.Unlock()
		broadcast("STATE_UPDATE", &gameState)
		gameState.mu.Lock()
	}

	currPrefix := gameState.Prefix
	if currPrefix == "" {
		currPrefix = "%"
	}

	trimmedMsg := strings.TrimSpace(msg)
	var cmdStr string
	switch {
	case strings.HasPrefix(trimmedMsg, currPrefix):
		cmdStr = strings.TrimPrefix(trimmedMsg, currPrefix)
	case strings.HasPrefix(trimmedMsg, "%"):
		cmdStr = strings.TrimPrefix(trimmedMsg, "%")
	case strings.HasPrefix(trimmedMsg, "!"):
		cmdStr = strings.TrimPrefix(trimmedMsg, "!")
	default:
		gameState.mu.Unlock()
		return
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		gameState.mu.Unlock()
		return
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "prefix":
		if len(parts) > 1 {
			newPrefix := parts[1]
			gameState.Prefix = newPrefix
			gameState.mu.Unlock()
			broadcast("STATE_UPDATE", &gameState)
			return
		}

	case "join":
		player := gameState.Players[username]
		if len(parts) > 1 {
			player.Emote = parts[1]
			if len(emotes) > 0 {
				player.EmoteURL = fmt.Sprintf("https://static-cdn.jtvnw.net/emoticons/v2/%s/default/dark/2.0", emotes[0].ID)
			} else {
				for _, de := range defaultEmotes {
					if strings.EqualFold(de.Name, parts[1]) {
						player.EmoteURL = de.URL
						break
					}
				}
			}
		}
		if player.EmoteURL == "" {
			randIdx := time.Now().UnixNano() % int64(len(defaultEmotes))
			if randIdx < 0 {
				randIdx = -randIdx
			}
			player.Emote = defaultEmotes[randIdx].Name
			player.EmoteURL = defaultEmotes[randIdx].URL
		}
		gameState.mu.Unlock()
		broadcast("STATE_UPDATE", &gameState)
		return

	case "startgame":
		if gameState.Phase == PhaseIdle {
			gameState.mu.Unlock()
			startInputPhase()
			return
		}

	case "fire", "left", "right":
		if gameState.Phase == PhaseInput {
			player := gameState.Players[username]
			if cmd == "fire" {
				if len(parts) >= 3 {
					var angle, power int
					_, _ = fmt.Sscanf(parts[1], "%d", &angle)
					_, _ = fmt.Sscanf(parts[2], "%d", &power)

					player.Angle = angle
					player.Power = power
					player.LastAngle = angle
					player.LastPower = power
				} else {
					player.Angle = player.LastAngle
					player.Power = player.LastPower
				}
				player.ActionType = "FIRE"
				player.Fired = true
			} else {
				player.ActionType = strings.ToUpper(cmd)
				player.Fired = true
			}

			checkAllPlayersFired()
			gameState.mu.Unlock()
			broadcast("PLAYER_LOCKED", username)
			broadcast("STATE_UPDATE", &gameState)
			return
		}
	}

	gameState.mu.Unlock()
}

func main() {
	flag.Parse()

	var err error
	db, err = sql.Open("sqlite", "streamtanks.db")
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS leaderboard (username TEXT PRIMARY KEY, wins INTEGER)`)
	if err != nil {
		_ = db.Close()
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Load leaderboard
	loadLeaderboard()

	if *debugMode {
		localPlayer := getDebugUsername()
		gameState.mu.Lock()
		gameState.Debug = true
		gameState.Players[localPlayer] = &Player{
			Name:      localPlayer,
			Emote:     "Kappa",
			EmoteURL:  "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0",
			LastAngle: 45,
			LastPower: 50,
		}
		gameState.Players["TargetBot"] = &Player{
			Name:      "TargetBot",
			Emote:     "PogChamp",
			EmoteURL:  "https://static-cdn.jtvnw.net/emoticons/v2/88/default/dark/2.0",
			LastAngle: 135,
			LastPower: 50,
		}
		gameState.mu.Unlock()
		log.Printf("Debug mode enabled: spawned %s and TargetBot", localPlayer)
	}

	// Setup Twitch Client only if channel flag is provided
	if *channelFlag != "" {
		client := twitch.NewAnonymousClient()
		client.OnPrivateMessage(func(message twitch.PrivateMessage) {
			processCommand(message.User.Name, message.Message, message.Emotes)
		})
		client.Join(*channelFlag)
		go func() {
			log.Printf("Connecting to Twitch channel: %s", *channelFlag)
			err := client.Connect()
			if err != nil {
				log.Fatalf("Twitch client error: %v", err)
			}
		}()
	} else {
		log.Println("No Twitch channel specified; running in local overlay mode (Twitch chat disabled)")
	}

	// Setup WebSocket and HTTP server with no-cache headers for overlay assets
	http.Handle("/ws", websocket.Handler(handleWebSocket))
	fs := http.FileServer(http.Dir("./public"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fs.ServeHTTP(w, r)
	})

	log.Printf("Server starting on %s", *listenAddr)
	if err := http.ListenAndServe(*listenAddr, nil); err != nil {
		log.Println("Server stopped:", err)
	}
}
