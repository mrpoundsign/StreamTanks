package app

import (
	"encoding/json"
	"log"
	"maps"
	"slices"
	"strings"
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

func broadcast(msgType string, payload any) {
	broadcastExcept(nil, msgType, payload)
	if msgType == msgStateUpdate {
		BroadcastViewerState()
	}
}

func broadcastExcept(exceptConn *websocket.Conn, msgType string, payload any) {
	payloadCopy := payload

	if payload == &gameState {
		gameState.mu.Lock()
		playersCopy := make(map[string]*Player, len(gameState.Players))
		for k, v := range gameState.Players {
			pCopy := *v
			playersCopy[k] = &pCopy
		}
		lbCopy := make(map[string]int, len(gameState.Leaderboard))
		maps.Copy(lbCopy, gameState.Leaderboard)
		terrainCopy := make([]float64, len(gameState.Terrain))
		copy(terrainCopy, gameState.Terrain)
		projCopy := make([]Projectile, len(gameState.Projectiles))
		copy(projCopy, gameState.Projectiles)
		expCopy := make([]Explosion, len(gameState.Explosions))
		copy(expCopy, gameState.Explosions)
		matchKillsCopy := make([]KillEvent, len(gameState.MatchKills))
		copy(matchKillsCopy, gameState.MatchKills)
		hasAliveBot := false
		joinedList := make([]string, 0, len(gameState.Players))
		for _, p := range gameState.Players {
			if !p.IsBot && p.Joined {
				joinedList = append(joinedList, strings.ToLower(p.Name))
			}
			if p.IsBot && !p.IsDead {
				hasAliveBot = true
			}
		}
		slices.Sort(joinedList)
		canStart := gameState.Phase == phaseIdle && len(joinedList) > 0
		canJoin := gameState.Phase == phaseIdle || (gameState.Phase == phaseInput && hasAliveBot)

		stateCopy := &GameState{
			Phase:          gameState.Phase,
			Players:        playersCopy,
			Channel:        gameState.Channel,
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
			CCEnabled:      gameState.CCEnabled,
			CCServerURL:    gameState.CCServerURL,
			CCStatus:       gameState.CCStatus,
			ClaimCode:      gameState.ClaimCode,
			Winner:         gameState.Winner,
			TimerRemaining: gameState.TimerRemaining,
			MatchKills:     matchKillsCopy,
			Projectiles:    projCopy,
			Explosions:     expCopy,
			ProtractorX:    gameState.ProtractorX,
			ProtractorY:    gameState.ProtractorY,
			CanStart:       canStart,
			CanJoin:        canJoin,
			JoinedPlayers:  joinedList,
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

	clientType := "Overlay"
	isExtension := false
	if req := ws.Request(); req != nil {
		if req.URL.Query().Get("client") == "admin" || strings.Contains(req.Header.Get("Referer"), "/admin") {
			clientType = "Admin Console"
		} else if req.URL.Query().Get("client") == "extension" || strings.Contains(req.Header.Get("Referer"), "/extension") {
			clientType = "Extension"
			isExtension = true
		}
	}

	defer func() {
		clientsMu.Lock()
		delete(activeClients, ws)
		clientsMu.Unlock()
		_ = ws.Close()
		log.Printf("%s WebSocket disconnected\n", clientType)
	}()

	log.Printf("New WebSocket client connected (%s)\n", clientType)

	// For extension clients, send VIEWER_INFO immediately so the test UI acts as the streamer
	if isExtension {
		_ = websocket.JSON.Send(ws, map[string]any{
			"type": "VIEWER_INFO",
			"payload": map[string]any{
				"user":      getDebugUsername(),
				"twitch_id": "local_dev",
				"channel":   getDebugUsername(),
				"role":      "broadcaster",
			},
		})
	}

	// Send initial state
	broadcast(msgStateUpdate, &gameState)

	// Listen for messages from frontend
	for {
		var msg WSMessage
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
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
