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
	_ "modernc.org/sqlite"
	"golang.org/x/net/websocket"
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
	ActiveClients map[*websocket.Conn]bool `json:"-"`
}

var gameState = GameState{
	Phase:         PhaseIdle,
	Players:       make(map[string]*Player),
	InputDuration: 20,
	MoveDistance:  100,
	Leaderboard:   make(map[string]int),
	ActiveClients: make(map[*websocket.Conn]bool),
}

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
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	rows, err := db.Query(`SELECT username, wins FROM leaderboard`)
	if err == nil {
		defer rows.Close()
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
	_, err := db.Exec(`INSERT INTO leaderboard (username, wins) VALUES (?, 1) ON CONFLICT(username) DO UPDATE SET wins = wins + 1`, username)
	if err != nil {
		log.Println("DB error:", err)
	}
}

func broadcast(msgType string, payload interface{}) {
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	msg := WSMessage{Type: msgType, Payload: payload}
	for conn := range gameState.ActiveClients {
		err := websocket.JSON.Send(conn, msg)
		if err != nil {
			log.Printf("Error sending to client: %v", err)
			conn.Close()
			delete(gameState.ActiveClients, conn)
		}
	}
}

func handleWebSocket(ws *websocket.Conn) {
	gameState.mu.Lock()
	gameState.ActiveClients[ws] = true
	gameState.mu.Unlock()

	log.Println("New WebSocket client connected (Overlay)")

	// Send initial state
	broadcast("STATE_UPDATE", gameState)

	// Listen for messages from frontend (e.g. ActionPhase complete)
	for {
		var msg WSMessage
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			log.Println("WebSocket disconnected")
			gameState.mu.Lock()
			delete(gameState.ActiveClients, ws)
			gameState.mu.Unlock()
			break
		}

		if msg.Type == "ACTION_COMPLETE" {
			// Start new input phase
			startInputPhase()
		} else if msg.Type == "PLAYER_DIED" {
			// A player was destroyed, remove from active players
			payloadBytes, _ := json.Marshal(msg.Payload)
			var deadPlayer string
			json.Unmarshal(payloadBytes, &deadPlayer)

			gameState.mu.Lock()
			if p, exists := gameState.Players[deadPlayer]; exists {
				p.IsDead = true
			}
			gameState.mu.Unlock()
			broadcast("STATE_UPDATE", gameState)
		} else if msg.Type == "GAME_OVER" {
			// Payload is winner name
			payloadBytes, _ := json.Marshal(msg.Payload)
			var winner string
			json.Unmarshal(payloadBytes, &winner)
			
			gameState.mu.Lock()
			gameState.Phase = PhaseCelebration
			if winner != "" {
				gameState.Leaderboard[winner]++
				incrementWin(winner)
			}
			gameState.mu.Unlock()
			broadcast("STATE_UPDATE", gameState)

			// Wait for celebration to end, then return to IDLE and reset terrain
			go func() {
				time.Sleep(5 * time.Second)
				gameState.mu.Lock()
				gameState.Phase = PhaseIdle
				// Revive all players for the next game
				for _, p := range gameState.Players {
					p.IsDead = false
					p.Fired = false
					p.ActionType = ""
				}
				gameState.mu.Unlock()
				broadcast("STATE_UPDATE", gameState)
				broadcast("RESET_TERRAIN", nil)
			}()
		}
	}
}

func startInputPhase() {
	gameState.mu.Lock()
	gameState.Phase = PhaseInput
	// Reset fired status and action for all players
	for _, p := range gameState.Players {
		p.Fired = false
		p.ActionType = ""
	}
	gameState.mu.Unlock()

	broadcast("STATE_UPDATE", gameState)

	// Start timer for input phase
	go func() {
		time.Sleep(time.Duration(gameState.InputDuration) * time.Second)
		executeActionPhase()
	}()
}

func executeActionPhase() {
	gameState.mu.Lock()
	if gameState.Phase != PhaseInput {
		gameState.mu.Unlock()
		return
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
	broadcast("STATE_UPDATE", gameState)
	broadcast("EXECUTE_ACTIONS", nil)
}

func main() {
	channelName := flag.String("channel", "mrpou", "Twitch channel to join")
	flag.Parse()

	var err error
	db, err = sql.Open("sqlite", "streamtanks.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS leaderboard (username TEXT PRIMARY KEY, wins INTEGER)`)
	if err != nil {
		log.Fatal(err)
	}

	// Load leaderboard
	loadLeaderboard()

	// Setup Twitch Client
	client := twitch.NewAnonymousClient()

	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		msg := strings.TrimSpace(message.Message)
		username := message.User.Name

		gameState.mu.Lock()
		defer gameState.mu.Unlock()

		// Idle roaming: Any active chatter is tracked, optionally with emote
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
			// Let frontend know a new player is roaming
			go broadcast("STATE_UPDATE", gameState)
		}

		parts := strings.Split(msg, " ")
		cmd := strings.ToLower(parts[0])

		if cmd == "!join" {
			player := gameState.Players[username]
			if len(parts) > 1 {
				player.Emote = parts[1]
				if len(message.Emotes) > 0 {
					player.EmoteURL = fmt.Sprintf("https://static-cdn.jtvnw.net/emoticons/v2/%s/default/dark/2.0", message.Emotes[0].ID)
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
			go broadcast("STATE_UPDATE", gameState)
		}

		if cmd == "!startgame" && gameState.Phase == PhaseIdle {
			// Streamer/mod starting game
			go startInputPhase()
		}

		if (cmd == "!fire" || cmd == "!left" || cmd == "!right") && gameState.Phase == PhaseInput {
			player := gameState.Players[username]
			if cmd == "!fire" {
				if len(parts) >= 3 {
					var angle, power int
					fmt.Sscanf(parts[1], "%d", &angle)
					fmt.Sscanf(parts[2], "%d", &power)
					
					player.Angle = angle
					player.Power = power
					player.LastAngle = angle
					player.LastPower = power
				} else {
					// Use last known config
					player.Angle = player.LastAngle
					player.Power = player.LastPower
				}
				
				player.ActionType = "FIRE"
				player.Fired = true
				
				go broadcast("PLAYER_LOCKED", username)
				go broadcast("STATE_UPDATE", gameState)
			} else {
				// Movement commands
				player.ActionType = strings.ToUpper(cmd[1:])
				player.Fired = true
				
				go broadcast("PLAYER_LOCKED", username)
				go broadcast("STATE_UPDATE", gameState)
			}
		}
	})

	client.Join(*channelName)

	go func() {
		log.Printf("Connecting to Twitch channel: %s", *channelName)
		err := client.Connect()
		if err != nil {
			log.Fatalf("Twitch client error: %v", err)
		}
	}()

	// Setup HTTP server
	http.Handle("/ws", websocket.Handler(handleWebSocket))
	http.Handle("/", http.FileServer(http.Dir("./public")))

	port := ":8080"
	log.Printf("Server starting on http://localhost%s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
