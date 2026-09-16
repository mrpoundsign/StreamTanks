package app

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"golang.org/x/net/websocket"
)

// ExtensionCommand represents the envelope received from the C&C Server.
type ExtensionCommand struct {
	Type    string `json:"type"`
	Payload struct {
		User    string      `json:"user"`
		Command interface{} `json:"command"`
	} `json:"payload"`
}

func startCCClient(baseURL, channel string) {
	connectURL := fmt.Sprintf("%s/ws/host?channel=%s", baseURL, channel)
	log.Printf("Starting C&C client connection to: %s", connectURL)

	for {
		err := runCCClient(connectURL)
		if err != nil {
			log.Printf("C&C client disconnected: %v. Reconnecting in 5 seconds...", err)
		} else {
			log.Printf("C&C client disconnected normally. Reconnecting in 5 seconds...")
		}
		time.Sleep(5 * time.Second)
	}
}

func runCCClient(connectURL string) error {
	origin := "http://localhost/" // Origin is required by websocket.Dial, but ignored by our C&C server
	ws, err := websocket.Dial(connectURL, "", origin)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer func() {
		clientsMu.Lock()
		delete(activeClients, ws)
		clientsMu.Unlock()
		_ = ws.Close()
	}()

	clientsMu.Lock()
	activeClients[ws] = true
	clientsMu.Unlock()

	log.Println("C&C client successfully connected!")

	// Keep-alive heartbeat to prevent proxy idle timeouts
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := websocket.JSON.Send(ws, map[string]string{"type": "PING"}); err != nil {
				return // Stop goroutine if connection closed
			}
		}
	}()

	for {
		var env ExtensionCommand
		if err := websocket.JSON.Receive(ws, &env); err != nil {
			return fmt.Errorf("read failed: %w", err)
		}

		if env.Type == "EXTENSION_COMMAND" {
			// Extract the raw command payload
			cmdBytes, err := json.Marshal(env.Payload.Command)
			if err != nil {
				log.Printf("Failed to marshal C&C command: %v", err)
				continue
			}

			// We expect the command from the frontend to be something like:
			// { "type": "CHAT_COMMAND", "payload": "%fire 45 60" }
			var wsMsg WSMessage
			if err := json.Unmarshal(cmdBytes, &wsMsg); err != nil {
				log.Printf("Failed to unmarshal C&C command to WSMessage: %v", err)
				continue
			}

			if wsMsg.Type == msgChatCommand {
				payloadBytes, err := json.Marshal(wsMsg.Payload)
				if err == nil {
					var cmdStr string
					if err := json.Unmarshal(payloadBytes, &cmdStr); err == nil {
						log.Printf("[C&C] Command from %s: %s", env.Payload.User, cmdStr)
						// Pass to the game engine as if it was a Twitch chat message
						processCommand(env.Payload.User, cmdStr, nil, nil)
					}
				}
			}
		}
	}
}
