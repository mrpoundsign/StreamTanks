package app

import (
	"encoding/json"
	"log"
	"sync"

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
		projCopy := make([]Projectile, len(gameState.Projectiles))
		copy(projCopy, gameState.Projectiles)
		expCopy := make([]Explosion, len(gameState.Explosions))
		copy(expCopy, gameState.Explosions)
		stateCopy := &GameState{
			Phase:          gameState.Phase,
			Players:        playersCopy,
			InputDuration:  gameState.InputDuration,
			MoveDistance:   gameState.MoveDistance,
			Leaderboard:    lbCopy,
			Debug:          gameState.Debug,
			Prefix:         gameState.Prefix,
			PhysicsSpeed:   gameState.PhysicsSpeed,
			ShowConfig:     gameState.ShowConfig,
			AutoRound:      gameState.AutoRound,
			IdleMessage:    gameState.IdleMessage,
			BouncyWalls:    gameState.BouncyWalls,
			Terrain:        terrainCopy,
			TerrainMin:     gameState.TerrainMin,
			TerrainMax:     gameState.TerrainMax,
			RoundID:        gameState.RoundID,
			StartPerm:      gameState.StartPerm,
			ConfigPerm:     gameState.ConfigPerm,
			MinPlayers:     gameState.MinPlayers,
			BotFill:        gameState.BotFill,
			BotPoints:      gameState.BotPoints,
			BotList:        gameState.BotList,
			Winner:         gameState.Winner,
			TimerRemaining: gameState.TimerRemaining,
			Projectiles:    projCopy,
			Explosions:     expCopy,
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
