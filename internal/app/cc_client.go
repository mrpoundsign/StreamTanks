package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

var (
	ccMu     sync.Mutex
	ccCancel context.CancelFunc

	ccConnMu        sync.Mutex
	ccConn          *websocket.Conn
	lastViewerState ViewerState
)

func setCCConn(conn *websocket.Conn) {
	ccConnMu.Lock()
	defer ccConnMu.Unlock()
	ccConn = conn
	if conn == nil {
		lastViewerState = ViewerState{}
	}
}

// BroadcastViewerState sends a compact snapshot of the game state to the C&C relay.
// Identical consecutive states are deduplicated to prevent unnecessary network traffic.
func BroadcastViewerState() {
	ccConnMu.Lock()
	conn := ccConn
	if conn == nil {
		ccConnMu.Unlock()
		return
	}

	gameState.mu.Lock()
	alivePlayers := make([]string, 0, len(gameState.Players))
	for name, p := range gameState.Players {
		if !p.IsDead {
			alivePlayers = append(alivePlayers, strings.ToLower(name))
		}
	}
	sort.Strings(alivePlayers)

	vs := ViewerState{
		Phase:          gameState.Phase,
		TimerRemaining: gameState.TimerRemaining,
		RoundID:        gameState.RoundID,
		Winner:         gameState.Winner,
		PlayersCount:   len(alivePlayers),
		Players:        alivePlayers,
		ProtractorX:    gameState.ProtractorX,
		ProtractorY:    gameState.ProtractorY,
	}
	gameState.mu.Unlock()

	if vs.Phase == lastViewerState.Phase &&
		vs.TimerRemaining == lastViewerState.TimerRemaining &&
		vs.RoundID == lastViewerState.RoundID &&
		vs.Winner == lastViewerState.Winner &&
		vs.PlayersCount == lastViewerState.PlayersCount &&
		vs.ProtractorX == lastViewerState.ProtractorX &&
		vs.ProtractorY == lastViewerState.ProtractorY &&
		slices.Equal(vs.Players, lastViewerState.Players) {
		ccConnMu.Unlock()
		return
	}
	lastViewerState = vs
	ccConnMu.Unlock()

	msg := WSMessage{
		Type:    "GAME_STATE",
		Payload: vs,
	}

	if err := websocket.JSON.Send(conn, msg); err != nil {
		log.Printf("[C&C] Failed to send viewer state update: %v", err)
	}
}

// StartCCClientManager initiates or restarts the background C&C relay connection.
func StartCCClientManager(channel string) {
	ccMu.Lock()
	defer ccMu.Unlock()

	if ccCancel != nil {
		ccCancel()
		ccCancel = nil
	}

	gameState.mu.Lock()
	enabled := gameState.CCEnabled
	baseURL := gameState.CCServerURL
	gameState.mu.Unlock()

	if !enabled || strings.TrimSpace(channel) == "" {
		gameState.mu.Lock()
		gameState.CCStatus = "disconnected"
		gameState.ClaimCode = ""
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		return
	}

	if baseURL == "" {
		baseURL = "wss://st-cc.poundsigndesign.com"
	}

	ctx, cancel := context.WithCancel(context.Background())
	ccCancel = cancel

	go runCCClientLoop(ctx, baseURL, strings.ToLower(strings.TrimSpace(channel)))
}

// StopCCClient terminates the active C&C relay connection.
func StopCCClient() {
	ccMu.Lock()
	defer ccMu.Unlock()

	if ccCancel != nil {
		ccCancel()
		ccCancel = nil
	}

	setCCConn(nil)

	gameState.mu.Lock()
	gameState.CCStatus = "disconnected"
	gameState.ClaimCode = ""
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)
}

// ResetCCHostToken deletes the stored C&C host token and restarts the client manager to issue a new claim.
func ResetCCHostToken(channel string) {
	deleteSetting("cc_host_token")
	log.Println("[C&C] Host token reset requested. Re-initiating claim flow...")
	StartCCClientManager(channel)
}

func runCCClientLoop(ctx context.Context, baseURL, channel string) {
	log.Printf("[C&C] Starting C&C client connection manager for channel: %s", channel)

	for {
		select {
		case <-ctx.Done():
			log.Println("[C&C] Connection manager stopped.")
			return
		default:
		}

		err := runCCClient(ctx, baseURL, channel)
		if err != nil {
			log.Printf("[C&C] Client disconnected: %v. Reconnecting in 5 seconds...", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func runCCClient(ctx context.Context, baseURL, channel string) error {
	token := getSetting("cc_host_token")
	connectURL := fmt.Sprintf("%s/ws/host?channel=%s", baseURL, url.QueryEscape(channel))
	if token != "" {
		connectURL += "&token=" + url.QueryEscape(token)
	}

	gameState.mu.Lock()
	gameState.CCStatus = "connecting"
	gameState.ClaimCode = ""
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)

	origin := "http://localhost/"
	ws, err := websocket.Dial(connectURL, "", origin)
	if err != nil {
		gameState.mu.Lock()
		gameState.CCStatus = "disconnected"
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		return fmt.Errorf("dial failed: %w", err)
	}

	defer func() {
		setCCConn(nil)
		_ = ws.Close()

		gameState.mu.Lock()
		gameState.CCStatus = "disconnected"
		gameState.ClaimCode = ""
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
	}()

	// Ping keepalive loop
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := websocket.JSON.Send(ws, map[string]string{"type": "PING"}); err != nil {
					return
				}
			}
		}
	}()

	// Dedicated reader goroutine
	msgChan := make(chan []byte)
	errChan := make(chan error, 1)

	go func() {
		for {
			var raw json.RawMessage
			if err := websocket.JSON.Receive(ws, &raw); err != nil {
				errChan <- err
				return
			}
			msgChan <- raw
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil

		case readErr := <-errChan:
			return fmt.Errorf("read failed: %w", readErr)

		case data := <-msgChan:
			var env struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}

			switch env.Type {
			case "AUTH_CHALLENGE":
				var challenge struct {
					Channel   string `json:"channel"`
					Code      string `json:"code"`
					Command   string `json:"command"`
					ExpiresIn int    `json:"expires_in"`
				}
				if err := json.Unmarshal(env.Payload, &challenge); err == nil {
					log.Printf("\n"+
						"========================================================================\n"+
						"[C&C] CHANNEL CLAIM REQUIRED FOR: %s\n"+
						"[C&C] To authorize this host, type this into your Twitch chat:\n"+
						"      %%claim %s\n"+
						"========================================================================\n", challenge.Channel, challenge.Code)

					gameState.mu.Lock()
					gameState.CCStatus = "pending_claim"
					gameState.ClaimCode = challenge.Code
					gameState.mu.Unlock()
					broadcast(msgStateUpdate, &gameState)
				}

			case "AUTH_SUCCESS":
				var success struct {
					Channel string `json:"channel"`
					Token   string `json:"token"`
				}
				if err := json.Unmarshal(env.Payload, &success); err == nil {
					if success.Token != "" {
						saveSetting("cc_host_token", success.Token)
						log.Println("[C&C] Channel authorization confirmed! Token securely saved to database.")
					} else {
						log.Println("[C&C] Channel authorization confirmed via existing token.")
					}

					gameState.mu.Lock()
					gameState.CCStatus = "connected"
					gameState.ClaimCode = ""
					gameState.mu.Unlock()

					setCCConn(ws)
					broadcast(msgStateUpdate, &gameState)
					BroadcastViewerState()
				}

			case "AUTH_ERROR":
				var errMsg string
				_ = json.Unmarshal(env.Payload, &errMsg)
				log.Printf("[C&C] Authentication error: %s", errMsg)

				deleteSetting("cc_host_token")

				gameState.mu.Lock()
				gameState.CCStatus = "disconnected"
				gameState.ClaimCode = ""
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)

			case "EXTENSION_COMMAND":
				var extCmd struct {
					User    string `json:"user"`
					Command any    `json:"command"`
				}
				if err := json.Unmarshal(env.Payload, &extCmd); err != nil {
					continue
				}

				cmdBytes, err := json.Marshal(extCmd.Command)
				if err != nil {
					continue
				}

				var wsMsg WSMessage
				if err := json.Unmarshal(cmdBytes, &wsMsg); err != nil {
					continue
				}

				if wsMsg.Type == msgChatCommand {
					payloadBytes, err := json.Marshal(wsMsg.Payload)
					if err == nil {
						var cmdStr string
						if err := json.Unmarshal(payloadBytes, &cmdStr); err == nil {
							log.Printf("[C&C] Command from %s: %s", extCmd.User, cmdStr)
							processCommand(extCmd.User, cmdStr, nil, nil)
						}
					}
				}
			}
		}
	}
}
