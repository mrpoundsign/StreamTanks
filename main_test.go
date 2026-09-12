package main

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func resetGameStateForTest() {
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	gameState.Phase = PhaseIdle
	gameState.Players = make(map[string]*Player)
	gameState.InputDuration = 2
	gameState.MoveDistance = 100
	gameState.Leaderboard = make(map[string]int)
	gameState.Debug = false

	if inputCancel != nil {
		close(inputCancel)
		inputCancel = nil
	}
}

func TestProcessCommand_JoinAndFire(t *testing.T) {
	resetGameStateForTest()

	// Test join command with %
	processCommand("Alice", "%join Kappa", nil)

	gameState.mu.Lock()
	alice, exists := gameState.Players["Alice"]
	if !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to exist in players")
	}
	if alice.Emote != "Kappa" {
		t.Errorf("expected emote Kappa, got %s", alice.Emote)
	}
	gameState.mu.Unlock()

	// Test join with !
	processCommand("Bob", "!join LUL", nil)
	gameState.mu.Lock()
	bob, exists := gameState.Players["Bob"]
	if !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Bob to exist in players")
	}
	if bob.Emote != "LUL" {
		t.Errorf("expected emote LUL, got %s", bob.Emote)
	}
	gameState.mu.Unlock()

	// Start game
	processCommand("Alice", "%startgame", nil)

	// Wait briefly for input phase to activate
	time.Sleep(50 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != PhaseInput {
		t.Errorf("expected PhaseInput, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// Send fire command
	processCommand("Alice", "%fire 60 75", nil)

	gameState.mu.Lock()
	alice = gameState.Players["Alice"]
	if !alice.Fired || alice.ActionType != "FIRE" || alice.Angle != 60 || alice.Power != 75 {
		t.Errorf("unexpected Alice state after fire: %+v", alice)
	}
	gameState.mu.Unlock()

	// Send movement command for Bob
	processCommand("Bob", "%left", nil)

	gameState.mu.Lock()
	bob = gameState.Players["Bob"]
	if !bob.Fired || bob.ActionType != "LEFT" {
		t.Errorf("unexpected Bob state after move: %+v", bob)
	}
	gameState.mu.Unlock()

	// All players fired, so executeActionPhase should trigger within 1s
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != PhaseAction {
		t.Errorf("expected PhaseAction after all players fired, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestDebugBotLifecycle_NoDeadlock(t *testing.T) {
	resetGameStateForTest()

	gameState.mu.Lock()
	gameState.Debug = true
	gameState.Players["Player1"] = &Player{
		Name:      "Player1",
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

	// Start game
	startInputPhase()

	// Wait 1.2s for TargetBot to auto-ready
	time.Sleep(1200 * time.Millisecond)

	gameState.mu.Lock()
	bot := gameState.Players["TargetBot"]
	if !bot.Fired {
		t.Errorf("expected TargetBot to have fired in debug mode")
	}
	if gameState.Phase != PhaseInput {
		t.Errorf("expected PhaseInput while Player1 hasn't fired yet, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// Player1 fires
	processCommand("Player1", "%fire 45 60", nil)

	// Action phase should trigger promptly
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != PhaseAction {
		t.Errorf("expected PhaseAction after both fired, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestConcurrentBroadcastAndStateAccess(t *testing.T) {
	resetGameStateForTest()

	// Spin up a test HTTP server with handleWebSocket
	server := httptest.NewServer(websocket.Handler(handleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect simulated clients
	const numClients = 5
	var conns []*websocket.Conn
	for i := 0; i < numClients; i++ {
		conn, err := websocket.Dial(wsURL, "", server.URL)
		if err != nil {
			t.Fatalf("failed to dial websocket: %v", err)
		}
		defer func(c *websocket.Conn) { _ = c.Close() }(conn)
		conns = append(conns, conn)
	}

	// Read messages in goroutines to prevent blocking
	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	for _, conn := range conns {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					var msg WSMessage
					_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
					_ = websocket.JSON.Receive(c, &msg)
				}
			}
		}(conn)
	}

	// Concurrently broadcast and process commands
	var opWg sync.WaitGroup
	for i := 0; i < 20; i++ {
		opWg.Add(1)
		playerID := fmt.Sprintf("User%d", i)
		go func(name string) {
			defer opWg.Done()
			processCommand(name, "%join Kappa", nil)
			broadcast("STATE_UPDATE", &gameState)
			broadcast("PLAYER_LOCKED", name)
		}(playerID)
	}

	opWg.Wait()
	close(stopChan)
	wg.Wait()
}
