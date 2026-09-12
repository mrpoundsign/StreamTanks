package main

import (
	"database/sql"
	"fmt"
	"io/fs"
	"math"
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

	gameState.Phase = phaseIdle
	gameState.Players = make(map[string]*Player)
	gameState.InputDuration = 2
	gameState.MoveDistance = 100
	gameState.Leaderboard = make(map[string]int)
	gameState.Debug = false
	gameState.Prefix = "%"
	gameState.PhysicsSpeed = 0.5
	gameState.ShowConfig = false
	gameState.AutoRound = 0
	gameState.IdleMessage = true
	gameState.BouncyWalls = false
	gameState.Terrain = generateTerrain()

	cancelAutoRoundTimer()

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
	if gameState.Phase != phaseInput {
		t.Errorf("expected phaseInput, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// Send fire command
	processCommand("Alice", "%fire 60 75", nil)

	gameState.mu.Lock()
	alice = gameState.Players["Alice"]
	if !alice.Fired || alice.ActionType != actionFire || alice.Angle != 60 || alice.Power != 75 {
		t.Errorf("unexpected Alice state after fire: %+v", alice)
	}
	gameState.mu.Unlock()

	// Send movement command for Bob
	processCommand("Bob", "%left", nil)

	gameState.mu.Lock()
	bob = gameState.Players["Bob"]
	if !bob.Fired || bob.ActionType != actionLeft {
		t.Errorf("unexpected Bob state after move: %+v", bob)
	}
	gameState.mu.Unlock()

	// All players fired, so executeActionPhase should trigger within 1s
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		t.Errorf("expected phaseAction after all players fired, got %s", gameState.Phase)
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
	if gameState.Phase != phaseInput {
		t.Errorf("expected phaseInput while Player1 hasn't fired yet, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// Player1 fires
	processCommand("Player1", "%fire 45 60", nil)

	// Action phase should trigger promptly
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		t.Errorf("expected phaseAction after both fired, got %s", gameState.Phase)
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
			broadcast(msgStateUpdate, &gameState)
			broadcast(msgPlayerLocked, name)
		}(playerID)
	}

	opWg.Wait()
	close(stopChan)
	wg.Wait()
}

func TestPrefixConfiguration(t *testing.T) {
	resetGameStateForTest()

	// Default prefix is %
	processCommand("Admin", "%prefix !", nil)
	gameState.mu.Lock()
	if gameState.Prefix != "!" {
		t.Errorf("expected Prefix to be !, got %s", gameState.Prefix)
	}
	gameState.mu.Unlock()

	// Test multi-character prefix
	processCommand("Admin", "!prefix tank!", nil)
	gameState.mu.Lock()
	if gameState.Prefix != "tank!" {
		t.Errorf("expected Prefix to be tank!, got %s", gameState.Prefix)
	}
	gameState.mu.Unlock()

	// Now commands should work with tank!
	processCommand("Alice", "tank!join Kappa", nil)
	gameState.mu.Lock()
	alice, exists := gameState.Players["Alice"]
	if !exists || alice.Emote != "Kappa" {
		t.Errorf("expected Alice to join with new prefix, got exists=%v", exists)
	}
	gameState.mu.Unlock()
}

func TestSpeedConfiguration(t *testing.T) {
	resetGameStateForTest()

	// Verify default is 0.5
	gameState.mu.Lock()
	if gameState.PhysicsSpeed != 0.5 {
		t.Errorf("expected default PhysicsSpeed to be 0.5, got %f", gameState.PhysicsSpeed)
	}
	gameState.mu.Unlock()

	// Test %speed command
	processCommand("Admin", "%speed 1.0", nil)
	gameState.mu.Lock()
	if gameState.PhysicsSpeed != 1.0 {
		t.Errorf("expected PhysicsSpeed 1.0, got %f", gameState.PhysicsSpeed)
	}
	gameState.mu.Unlock()

	// Test %physicsspeed alias
	processCommand("Admin", "%physicsspeed 0.25", nil)
	gameState.mu.Lock()
	if gameState.PhysicsSpeed != 0.25 {
		t.Errorf("expected PhysicsSpeed 0.25, got %f", gameState.PhysicsSpeed)
	}
	gameState.mu.Unlock()

	// Test clamping below minimum (0.1)
	processCommand("Admin", "%speed 0.01", nil)
	gameState.mu.Lock()
	if gameState.PhysicsSpeed != 0.1 {
		t.Errorf("expected PhysicsSpeed clamped to 0.1, got %f", gameState.PhysicsSpeed)
	}
	gameState.mu.Unlock()

	// Test clamping above maximum (3.0)
	processCommand("Admin", "%speed 99.0", nil)
	gameState.mu.Lock()
	if gameState.PhysicsSpeed != 3.0 {
		t.Errorf("expected PhysicsSpeed clamped to 3.0, got %f", gameState.PhysicsSpeed)
	}
	gameState.mu.Unlock()
}

func TestConfigCommand(t *testing.T) {
	resetGameStateForTest()

	// Default ShowConfig should be false
	gameState.mu.Lock()
	if gameState.ShowConfig {
		t.Errorf("expected ShowConfig to default to false")
	}
	gameState.mu.Unlock()

	// %config toggles ShowConfig on
	processCommand("Admin", "%config", nil)
	gameState.mu.Lock()
	if !gameState.ShowConfig {
		t.Errorf("expected ShowConfig to be true after %%config toggle")
	}
	gameState.mu.Unlock()

	// Repeating %config toggles ShowConfig off
	processCommand("Admin", "%config", nil)
	gameState.mu.Lock()
	if gameState.ShowConfig {
		t.Errorf("expected ShowConfig to be false after second %%config toggle")
	}
	gameState.mu.Unlock()

	// %config on explicitly turns it on
	processCommand("Admin", "%config on", nil)
	gameState.mu.Lock()
	if !gameState.ShowConfig {
		t.Errorf("expected ShowConfig to be true after %%config on")
	}
	gameState.mu.Unlock()

	// %config off explicitly turns it off
	processCommand("Admin", "%config off", nil)
	gameState.mu.Lock()
	if gameState.ShowConfig {
		t.Errorf("expected ShowConfig to be false after %%config off")
	}
	gameState.mu.Unlock()

	// %settings alias works identically
	processCommand("Admin", "%settings", nil)
	gameState.mu.Lock()
	if !gameState.ShowConfig {
		t.Errorf("expected ShowConfig to be true after %%settings toggle")
	}
	gameState.mu.Unlock()

	processCommand("Admin", "%settings hide", nil)
	gameState.mu.Lock()
	if gameState.ShowConfig {
		t.Errorf("expected ShowConfig to be false after %%settings hide")
	}
	gameState.mu.Unlock()

	// Verify ShowConfig is copied when broadcasting STATE_UPDATE
	processCommand("Admin", "%config on", nil)
	ts := httptest.NewServer(websocket.Handler(handleWebSocket))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	wsConn, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer func() { _ = wsConn.Close() }()

	var initMsg struct {
		Type    string    `json:"type"`
		Payload GameState `json:"payload"`
	}
	if err := websocket.JSON.Receive(wsConn, &initMsg); err != nil {
		t.Fatalf("failed to receive initial STATE_UPDATE: %v", err)
	}
	if !initMsg.Payload.ShowConfig {
		t.Errorf("expected ShowConfig to be true in broadcasted STATE_UPDATE payload, got false")
	}
}

func TestCommandtimeConfiguration(t *testing.T) {
	resetGameStateForTest()

	// Default InputDuration in test helper is 2
	gameState.mu.Lock()
	if gameState.InputDuration != 2 {
		t.Errorf("expected InputDuration 2, got %d", gameState.InputDuration)
	}
	gameState.mu.Unlock()

	// Test %commandtime 30
	processCommand("Admin", "%commandtime 30", nil)
	gameState.mu.Lock()
	if gameState.InputDuration != 30 {
		t.Errorf("expected InputDuration 30, got %d", gameState.InputDuration)
	}
	gameState.mu.Unlock()

	// Test clamping below minimum (5)
	processCommand("Admin", "%commandtime 2", nil)
	gameState.mu.Lock()
	if gameState.InputDuration != 5 {
		t.Errorf("expected InputDuration clamped to 5, got %d", gameState.InputDuration)
	}
	gameState.mu.Unlock()

	// Test clamping above maximum (120)
	processCommand("Admin", "%commandtime 500", nil)
	gameState.mu.Lock()
	if gameState.InputDuration != 120 {
		t.Errorf("expected InputDuration clamped to 120, got %d", gameState.InputDuration)
	}
	gameState.mu.Unlock()
}

func TestSettingsPersistence(t *testing.T) {
	resetGameStateForTest()

	// Create an in-memory SQLite database
	testDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	defer func() { _ = testDB.Close() }()

	_, err = testDB.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("failed to create test settings table: %v", err)
	}

	oldDB := db
	db = testDB
	defer func() { db = oldDB }()

	// Save settings via processCommand
	processCommand("Admin", "%prefix !", nil)
	processCommand("Admin", "!speed 0.8", nil)
	processCommand("Admin", "!commandtime 25", nil)
	processCommand("Admin", "!autoround -1", nil)
	processCommand("Admin", "!idlemessage off", nil)
	processCommand("Admin", "!bouncywalls on", nil)

	// Reset in-memory gameState
	resetGameStateForTest()
	gameState.mu.Lock()
	if gameState.Prefix != "%" || gameState.PhysicsSpeed != 0.5 || gameState.InputDuration != 2 || gameState.AutoRound != 0 || !gameState.IdleMessage || gameState.BouncyWalls {
		gameState.mu.Unlock()
		t.Fatalf("expected reset state")
	}
	gameState.mu.Unlock()

	// Load settings from DB
	loadSettings()

	gameState.mu.Lock()
	if gameState.Prefix != "!" {
		t.Errorf("expected loaded Prefix '!', got %s", gameState.Prefix)
	}
	if gameState.PhysicsSpeed != 0.8 {
		t.Errorf("expected loaded PhysicsSpeed 0.8, got %f", gameState.PhysicsSpeed)
	}
	if gameState.InputDuration != 25 {
		t.Errorf("expected loaded InputDuration 25, got %d", gameState.InputDuration)
	}
	if gameState.AutoRound != -1 {
		t.Errorf("expected loaded AutoRound -1, got %d", gameState.AutoRound)
	}
	if gameState.IdleMessage {
		t.Errorf("expected loaded IdleMessage to be false")
	}
	if !gameState.BouncyWalls {
		t.Errorf("expected loaded BouncyWalls to be true")
	}
	gameState.mu.Unlock()
}

func TestAutoRoundConfiguration(t *testing.T) {
	resetGameStateForTest()

	// Default AutoRound is 0 (off)
	gameState.mu.Lock()
	if gameState.AutoRound != 0 {
		t.Errorf("expected default AutoRound 0, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()

	// %autoround -1 (immediate)
	processCommand("Admin", "%autoround -1", nil)
	gameState.mu.Lock()
	if gameState.AutoRound != -1 {
		t.Errorf("expected AutoRound -1, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()

	// %autoround immediate keyword
	processCommand("Admin", "%autoround immediate", nil)
	gameState.mu.Lock()
	if gameState.AutoRound != -1 {
		t.Errorf("expected AutoRound -1, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()

	// %autoround 5 (5 minutes)
	processCommand("Admin", "%autoround 5", nil)
	gameState.mu.Lock()
	if gameState.AutoRound != 5 {
		t.Errorf("expected AutoRound 5, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()

	// %autoround 0 / off disables
	processCommand("Admin", "%autoround off", nil)
	gameState.mu.Lock()
	if gameState.AutoRound != 0 {
		t.Errorf("expected AutoRound 0, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()

	// Clamping max 60 minutes
	processCommand("Admin", "%autoround 120", nil)
	gameState.mu.Lock()
	if gameState.AutoRound != 60 {
		t.Errorf("expected AutoRound clamped to 60, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()
}

func TestIdleMessageConfiguration(t *testing.T) {
	resetGameStateForTest()

	// Default is true
	gameState.mu.Lock()
	if !gameState.IdleMessage {
		t.Errorf("expected default IdleMessage to be true")
	}
	gameState.mu.Unlock()

	// %idlemessage off
	processCommand("Admin", "%idlemessage off", nil)
	gameState.mu.Lock()
	if gameState.IdleMessage {
		t.Errorf("expected IdleMessage false after %%idlemessage off")
	}
	gameState.mu.Unlock()

	// %idlemessage on
	processCommand("Admin", "%idlemessage on", nil)
	gameState.mu.Lock()
	if !gameState.IdleMessage {
		t.Errorf("expected IdleMessage true after %%idlemessage on")
	}
	gameState.mu.Unlock()

	// %idlemessage toggle
	processCommand("Admin", "%idlemessage", nil)
	gameState.mu.Lock()
	if gameState.IdleMessage {
		t.Errorf("expected IdleMessage false after %%idlemessage toggle")
	}
	gameState.mu.Unlock()

	// Second toggle
	processCommand("Admin", "%idlemessage", nil)
	gameState.mu.Lock()
	if !gameState.IdleMessage {
		t.Errorf("expected IdleMessage true after second %%idlemessage toggle")
	}
	gameState.mu.Unlock()
}

func TestEmbeddedPublicAssets(t *testing.T) {
	// Verify embedded public files can be read
	subFS, err := fs.Sub(embeddedPublic, "public")
	if err != nil {
		t.Fatalf("failed to open embedded subFS: %v", err)
	}

	requiredFiles := []string{"index.html", "game.js", "style.css"}
	for _, fname := range requiredFiles {
		content, err := fs.ReadFile(subFS, fname)
		if err != nil {
			t.Errorf("expected embedded asset %s to be readable, got error: %v", fname, err)
		}
		if len(content) == 0 {
			t.Errorf("expected embedded asset %s to not be empty", fname)
		}
	}
}

func TestInactivePlayerRandomDirection(t *testing.T) {
	leftCount := 0
	rightCount := 0

	for trial := 0; trial < 100; trial++ {
		resetGameStateForTest()

		gameState.mu.Lock()
		gameState.Phase = phaseInput
		gameState.Players["P1"] = &Player{Name: "P1", Fired: false, ActionType: ""}
		gameState.mu.Unlock()

		executeActionPhase()

		gameState.mu.Lock()
		p1 := gameState.Players["P1"]
		if !p1.Fired {
			t.Errorf("expected P1 to be marked fired after action phase")
		}
		if p1.ActionType != actionLeft && p1.ActionType != actionRight {
			t.Errorf("expected ActionType to be LEFT or RIGHT, got %s", p1.ActionType)
		}
		switch p1.ActionType {
		case actionLeft:
			leftCount++
		case actionRight:
			rightCount++
		}
		gameState.mu.Unlock()
	}

	if leftCount == 0 || rightCount == 0 {
		t.Errorf("expected both LEFT and RIGHT to be selected across 100 trials, got left=%d, right=%d", leftCount, rightCount)
	}
}

func TestBouncyWallsConfiguration(t *testing.T) {
	resetGameStateForTest()

	// Default is false
	gameState.mu.Lock()
	if gameState.BouncyWalls {
		t.Errorf("expected default BouncyWalls to be false")
	}
	gameState.mu.Unlock()

	// %bouncywalls on
	processCommand("Admin", "%bouncywalls on", nil)
	gameState.mu.Lock()
	if !gameState.BouncyWalls {
		t.Errorf("expected BouncyWalls true after %%bouncywalls on")
	}
	gameState.mu.Unlock()

	// %bouncywalls off
	processCommand("Admin", "%bouncywalls off", nil)
	gameState.mu.Lock()
	if gameState.BouncyWalls {
		t.Errorf("expected BouncyWalls false after %%bouncywalls off")
	}
	gameState.mu.Unlock()

	// %bouncy toggle
	processCommand("Admin", "%bouncy", nil)
	gameState.mu.Lock()
	if !gameState.BouncyWalls {
		t.Errorf("expected BouncyWalls true after %%bouncy toggle")
	}
	gameState.mu.Unlock()

	// %bouncy toggle off
	processCommand("Admin", "%bouncy", nil)
	gameState.mu.Lock()
	if gameState.BouncyWalls {
		t.Errorf("expected BouncyWalls false after second %%bouncy toggle")
	}
	gameState.mu.Unlock()
}

func TestGenerateTerrain(t *testing.T) {
	terrain := generateTerrain()
	if len(terrain) != defaultTerrainWidth {
		t.Fatalf("expected terrain length %d, got %d", defaultTerrainWidth, len(terrain))
	}

	for i, y := range terrain {
		if y < 100 || y > float64(defaultTerrainHeight)-50 {
			t.Errorf("terrain at index %d out of expected vertical bounds: %f", i, y)
		}
		if i > 0 {
			diff := math.Abs(terrain[i] - terrain[i-1])
			if diff > 2.5 {
				t.Errorf("slope too steep at index %d: diff=%f", i, diff)
			}
		}
	}
}

func TestGetTerrainHeight(t *testing.T) {
	terrain := generateTerrain()

	// Normal lookup
	h := getTerrainHeight(terrain, 500)
	if h != terrain[500] {
		t.Errorf("expected %f, got %f", terrain[500], h)
	}

	// Boundary clamping: x < 0
	hNeg := getTerrainHeight(terrain, -100)
	if hNeg != terrain[0] {
		t.Errorf("expected %f for negative x, got %f", terrain[0], hNeg)
	}

	// Boundary clamping: x >= width
	hOver := getTerrainHeight(terrain, 5000)
	if hOver != terrain[defaultTerrainWidth-1] {
		t.Errorf("expected %f for overflow x, got %f", terrain[defaultTerrainWidth-1], hOver)
	}
}

func TestApplyCrater(t *testing.T) {
	terrain := make([]float64, defaultTerrainWidth)
	for i := range terrain {
		terrain[i] = 500.0 // flat line at Y=500
	}

	cx := 400.0
	cy := 500.0
	radius := 40.0

	applyCrater(terrain, cx, cy, radius)

	// Center should be lowered to cy + radius = 540
	centerH := getTerrainHeight(terrain, cx)
	if centerH != 540.0 {
		t.Errorf("expected crater center depth 540.0, got %f", centerH)
	}

	// Outside crater radius should remain unaffected at 500
	outsideH := getTerrainHeight(terrain, cx+radius+10)
	if outsideH != 500.0 {
		t.Errorf("expected outside crater height 500.0, got %f", outsideH)
	}

	// Idempotency: applying the same crater again should result in the exact same heightmap
	applyCrater(terrain, cx, cy, radius)
	if getTerrainHeight(terrain, cx) != 540.0 {
		t.Errorf("crater height changed upon reapplying: got %f", getTerrainHeight(terrain, cx))
	}
}

func TestTerrainStateSync(t *testing.T) {
	resetGameStateForTest()

	gameState.mu.Lock()
	if len(gameState.Terrain) != defaultTerrainWidth {
		t.Fatalf("expected gameState.Terrain length %d, got %d", defaultTerrainWidth, len(gameState.Terrain))
	}
	gameState.mu.Unlock()

	// Player joins -> assigned X and Y
	processCommand("Alice", "%join Kappa", nil)

	gameState.mu.Lock()
	alice, exists := gameState.Players["Alice"]
	if !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to exist")
	}
	if alice.X <= 0 || alice.Y <= 0 {
		t.Errorf("expected valid Alice coordinates, got X=%f, Y=%f", alice.X, alice.Y)
	}
	expectedY := getTerrainHeight(gameState.Terrain, alice.X)
	if alice.Y != expectedY {
		t.Errorf("expected Alice Y=%f, got %f", expectedY, alice.Y)
	}
	gameState.mu.Unlock()

	// Test resetMatchState
	gameState.mu.Lock()
	gameState.Phase = phaseCelebration
	gameState.mu.Unlock()

	resetMatchState()

	gameState.mu.Lock()
	if gameState.Phase != phaseIdle {
		t.Errorf("expected phaseIdle after resetMatchState, got %s", gameState.Phase)
	}
	if len(gameState.Terrain) != defaultTerrainWidth {
		t.Errorf("expected regenerated terrain, got length %d", len(gameState.Terrain))
	}
	gameState.mu.Unlock()
}





