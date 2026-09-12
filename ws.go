package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

var (
	clientsMu     sync.RWMutex
	activeClients = make(map[*websocket.Conn]bool)
)

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
			PhysicsSpeed:  gameState.PhysicsSpeed,
			ShowConfig:    gameState.ShowConfig,
			AutoRound:     gameState.AutoRound,
			IdleMessage:   gameState.IdleMessage,
			BouncyWalls:   gameState.BouncyWalls,
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
	broadcast(msgStateUpdate, &gameState)

	// Listen for messages from frontend
	for {
		var msg WSMessage
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			log.Println("WebSocket disconnected")
			break
		}

		switch msg.Type {
		case msgActionComplete:
			startInputPhase()
		case msgPlayerDied:
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var deadPlayer string
				if err := json.Unmarshal(payloadBytes, &deadPlayer); err == nil {
					gameState.mu.Lock()
					if p, exists := gameState.Players[deadPlayer]; exists {
						p.IsDead = true
					}
					gameState.mu.Unlock()
					broadcast(msgStateUpdate, &gameState)
				}
			}
		case msgGameOver:
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var winner string
				if err := json.Unmarshal(payloadBytes, &winner); err == nil {
					gameState.mu.Lock()
					gameState.Phase = phaseCelebration
					if winner != "" {
						gameState.Leaderboard[winner]++
						incrementWin(winner)
					}
					gameState.mu.Unlock()
					broadcast(msgStateUpdate, &gameState)

					// Safety fallback: if no CELEBRATION_COMPLETE arrives within 12s, reset cleanly
					go func() {
						time.Sleep(12 * time.Second)
						gameState.mu.Lock()
						if gameState.Phase == phaseCelebration {
							gameState.Phase = phaseIdle
							for _, p := range gameState.Players {
								p.IsDead = false
								p.Fired = false
								p.ActionType = ""
							}
							gameState.mu.Unlock()
							broadcast(msgStateUpdate, &gameState)
							broadcast(msgResetTerrain, nil)
							triggerAutoRound()
						} else {
							gameState.mu.Unlock()
						}
					}()
				}
			}
		case msgCelebrationComplete:
			gameState.mu.Lock()
			if gameState.Phase == phaseCelebration {
				gameState.Phase = phaseIdle
				// Revive all players for the next game
				for _, p := range gameState.Players {
					p.IsDead = false
					p.Fired = false
					p.ActionType = ""
				}
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)
				broadcast(msgResetTerrain, nil)
				triggerAutoRound()
			} else {
				gameState.mu.Unlock()
			}
		case msgChatCommand, msgDebugCommand:
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
