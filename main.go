package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
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
	Name      string `json:"name"`
	Emote     string `json:"emote"`
	EmoteURL  string `json:"emoteUrl"`
	LastAngle int    `json:"lastAngle"`
	LastPower int    `json:"lastPower"`
	Fired     bool   `json:"fired"`
	Angle     int    `json:"angle"`
	Power     int    `json:"power"`
}

type GameState struct {
	mu            sync.Mutex
	Phase         string             `json:"phase"`
	Players       map[string]*Player `json:"players"`
	InputDuration int                `json:"inputDuration"` // in seconds
	Leaderboard   map[string]int     `json:"leaderboard"`
	ActiveClients map[*websocket.Conn]bool `json:"-"`
}

var gameState = GameState{
	Phase:         PhaseIdle,
	Players:       make(map[string]*Player),
	InputDuration: 15,
	Leaderboard:   make(map[string]int),
	ActiveClients: make(map[*websocket.Conn]bool),
}

// WSMessage is the generic message sent over websocket
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
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
			// Remove player
			// Payload is expected to be a string (player name)
			payloadBytes, _ := json.Marshal(msg.Payload)
			var name string
			json.Unmarshal(payloadBytes, &name)
			
			gameState.mu.Lock()
			delete(gameState.Players, name)
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
				saveLeaderboard()
			}
			gameState.mu.Unlock()
			broadcast("STATE_UPDATE", gameState)

			// Wait for celebration to end, then return to IDLE and reset terrain
			go func() {
				time.Sleep(5 * time.Second)
				gameState.mu.Lock()
				gameState.Phase = PhaseIdle
				gameState.mu.Unlock()
				broadcast("STATE_UPDATE", gameState)
				broadcast("RESET_TERRAIN", nil)
			}()
		}
	}
}

func saveLeaderboard() {
	data, err := json.Marshal(gameState.Leaderboard)
	if err == nil {
		os.WriteFile("leaderboard.json", data, 0644)
	}
}

func startInputPhase() {
	gameState.mu.Lock()
	gameState.Phase = PhaseInput
	// Reset fired status for all players
	for _, p := range gameState.Players {
		p.Fired = false
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
		if !p.Fired {
			p.Angle = p.LastAngle
			p.Power = p.LastPower
			p.Fired = true
		}
	}
	gameState.mu.Unlock()

	// Send state update which tells frontend to execute the shots
	broadcast("STATE_UPDATE", gameState)
	broadcast("EXECUTE_FIRE", nil)
}

func main() {
	channelName := flag.String("channel", "mrpou", "Twitch channel to join")
	flag.Parse()

	// Load leaderboard
	if data, err := os.ReadFile("leaderboard.json"); err == nil {
		json.Unmarshal(data, &gameState.Leaderboard)
	}

	// Setup Twitch Client
	client := twitch.NewAnonymousClient()

	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		msg := strings.TrimSpace(message.Message)
		username := message.User.Name

		gameState.mu.Lock()
		defer gameState.mu.Unlock()

		// Idle roaming: Any active chatter is tracked, optionally with emote
		if _, exists := gameState.Players[username]; !exists {
			gameState.Players[username] = &Player{
				Name:      username,
				Emote:     "Kappa", // Default emote
				EmoteURL:  "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0",
				LastAngle: 45,
				LastPower: 50,
			}
			// Let frontend know a new player is roaming
			go broadcast("STATE_UPDATE", gameState)
		}

		parts := strings.Split(msg, " ")
		cmd := strings.ToLower(parts[0])

		if cmd == "!join" && len(parts) > 1 {
			player := gameState.Players[username]
			player.Emote = parts[1]
			if len(message.Emotes) > 0 {
				player.EmoteURL = fmt.Sprintf("https://static-cdn.jtvnw.net/emoticons/v2/%s/default/dark/2.0", message.Emotes[0].ID)
			}
			go broadcast("STATE_UPDATE", gameState)
		}

		if cmd == "!startgame" && gameState.Phase == PhaseIdle {
			// Streamer/mod starting game
			go startInputPhase()
		}

		if cmd == "!fire" && (gameState.Phase == PhaseInput || gameState.Phase == PhaseIdle) {
			if gameState.Phase == PhaseIdle {
				// Auto-start the game if someone fires while idle
				gameState.Phase = PhaseInput
				for _, p := range gameState.Players {
					p.Fired = false
				}
				go func() {
					time.Sleep(time.Duration(gameState.InputDuration) * time.Second)
					executeActionPhase()
				}()
			}

			if len(parts) >= 3 {
				var angle, power int
				fmt.Sscanf(parts[1], "%d", &angle)
				fmt.Sscanf(parts[2], "%d", &power)
				
				player := gameState.Players[username]
				player.Angle = angle
				player.Power = power
				player.LastAngle = angle
				player.LastPower = power
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
