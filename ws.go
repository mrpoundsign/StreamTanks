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

	appliedCratersMu sync.Mutex
	appliedCraters   = make(map[string]bool)
)

func clearAppliedCraters() {
	appliedCratersMu.Lock()
	defer appliedCratersMu.Unlock()
	appliedCraters = make(map[string]bool)
}

func broadcast(msgType string, payload interface{}) {
	broadcastExcept(nil, msgType, payload)
}

func broadcastExcept(exceptConn *websocket.Conn, msgType string, payload interface{}) {
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
		terrainCopy := make([]float64, len(gameState.Terrain))
		copy(terrainCopy, gameState.Terrain)
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
			Terrain:       terrainCopy,
			TerrainMin:    gameState.TerrainMin,
			TerrainMax:    gameState.TerrainMax,
			RoundID:       gameState.RoundID,
			StartPerm:     gameState.StartPerm,
			ConfigPerm:    gameState.ConfigPerm,
			MinPlayers:    gameState.MinPlayers,
			BotFill:       gameState.BotFill,
			BotPoints:     gameState.BotPoints,
			BotList:       gameState.BotList,
		}
		gameState.mu.Unlock()
		payloadCopy = stateCopy
	}

	msg := WSMessage{Type: msgType, Payload: payloadCopy}

	clientsMu.RLock()
	conns := make([]*websocket.Conn, 0, len(activeClients))
	for conn := range activeClients {
		if conn != exceptConn {
			conns = append(conns, conn)
		}
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
			cancelActionFallback()
			gameState.mu.Lock()
			if gameState.Phase == phaseAction {
				gameState.mu.Unlock()
				startInputPhase()
			} else {
				gameState.mu.Unlock()
			}
		case msgPlayerDied:
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var death PlayerDiedPayload
				if err := json.Unmarshal(payloadBytes, &death); err == nil && death.Victim != "" {
					gameState.mu.Lock()
					if p, exists := gameState.Players[death.Victim]; exists && !p.IsDead {
						p.IsDead = true
						if death.Killer != "" && death.Killer != death.Victim {
							killerPlayer := gameState.Players[death.Killer]
							victimPlayer := gameState.Players[death.Victim]
							// Bots never receive points or appear on the leaderboard
							if killerPlayer != nil && !killerPlayer.IsBot {
								pts := 1
								if victimPlayer != nil && victimPlayer.IsBot {
									pts = gameState.BotPoints
								}
								if pts > 0 {
									gameState.Leaderboard[death.Killer] += pts
									addScore(death.Killer, pts)
								}
							}
						}
						gameState.mu.Unlock()
						broadcast(msgStateUpdate, &gameState)
					} else {
						gameState.mu.Unlock()
					}
				} else {
					var victim string
					if err := json.Unmarshal(payloadBytes, &victim); err == nil && victim != "" {
						gameState.mu.Lock()
						if p, exists := gameState.Players[victim]; exists && !p.IsDead {
							p.IsDead = true
							gameState.mu.Unlock()
							broadcast(msgStateUpdate, &gameState)
						} else {
							gameState.mu.Unlock()
						}
					}
				}
			}
		case msgGameOver:
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var winner string
				if err := json.Unmarshal(payloadBytes, &winner); err == nil {
					gameState.mu.Lock()
					if gameState.Phase == phaseCelebration {
						gameState.mu.Unlock()
						break
					}
					cancelActionFallback()
					gameState.Phase = phaseCelebration
					gameState.Winner = winner
					if winner != "" && winner != "AI" {
						if p, exists := gameState.Players[winner]; exists && !p.IsBot {
							gameState.Leaderboard[winner] += 5
							addScore(winner, 5)
						}
					}
					gameState.mu.Unlock()
					broadcast(msgStateUpdate, &gameState)

					// Safety fallback: if no CELEBRATION_COMPLETE arrives within 12s, reset cleanly
					go func() {
						time.Sleep(12 * time.Second)
						resetMatchState()
					}()
				}
			}
		case msgCelebrationComplete:
			gameState.mu.Lock()
			if gameState.Phase == phaseCelebration {
				gameState.mu.Unlock()
				resetMatchState()
			} else {
				gameState.mu.Unlock()
			}
		case msgTerrainCrater:
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var crater CraterPayload
				if err := json.Unmarshal(payloadBytes, &crater); err == nil {
					if crater.ID != "" {
						appliedCratersMu.Lock()
						if appliedCraters[crater.ID] {
							appliedCratersMu.Unlock()
							break
						}
						appliedCraters[crater.ID] = true
						appliedCratersMu.Unlock()
					}
					gameState.mu.Lock()
					applyCrater(gameState.Terrain, crater.X, crater.Y, crater.Radius)
					gameState.mu.Unlock()
					broadcastExcept(ws, msgTerrainCrater, crater)
				}
			}
		case msgChatCommand, msgDebugCommand:
			payloadBytes, err := json.Marshal(msg.Payload)
			if err == nil {
				var cmdStr string
				if err := json.Unmarshal(payloadBytes, &cmdStr); err == nil {
					processCommand(getDebugUsername(), cmdStr, nil, nil)
				}
			}
		}
	}
}
