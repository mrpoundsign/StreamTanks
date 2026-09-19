package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"streamtanks/web"

	"github.com/gempir/go-twitch-irc/v4"
	"golang.org/x/net/websocket"
)

func resetGameStateForTest() {
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	gameState.Phase = phaseIdle
	gameState.RoundID = 0
	gameState.MatchKills = nil
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
	gameState.TerrainMin = 20
	gameState.TerrainMax = 75
	gameState.Terrain = generateTerrain(20, 75)
	gameState.StartPerm = "broadcaster"
	gameState.ConfigPerm = "broadcaster"
	gameState.MinPlayers = 5
	gameState.BotFill = true
	gameState.BotPoints = 1
	gameState.BotList = []string{"TargetBot", "RustyTank", "IronClad", "CyberDrone", "MechaUnit"}
	gameState.ProtractorX = 250
	gameState.ProtractorY = 270

	cancelAutoRoundTimer()
	cancelFastForward()

	if inputCancel != nil {
		close(inputCancel)
		inputCancel = nil
	}
	if minWaitTimer != nil {
		minWaitTimer.Stop()
		minWaitTimer = nil
	}
	fastForwardScheduled = false
	prevRoundHadCommands = false
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
		Joined:    true,
	}
	gameState.Players["TargetBot"] = &Player{
		Name:      "TargetBot",
		IsBot:     true,
		Emote:     "PogChamp",
		EmoteURL:  "https://static-cdn.jtvnw.net/emoticons/v2/305954156/default/dark/2.0",
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
	conns := make([]*websocket.Conn, 0, numClients)
	for range numClients {
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
	for i := range 20 {
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
	processCommand("Admin", "!terrain 15 45", nil)

	// Reset in-memory gameState
	resetGameStateForTest()
	gameState.mu.Lock()
	if gameState.Prefix != "%" || gameState.PhysicsSpeed != 0.5 || gameState.InputDuration != 2 || gameState.AutoRound != 0 || !gameState.IdleMessage || gameState.BouncyWalls || gameState.TerrainMin != 20 || gameState.TerrainMax != 75 {
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
	if gameState.TerrainMin != 15 || gameState.TerrainMax != 45 {
		t.Errorf("expected loaded TerrainMin=15, TerrainMax=45; got %d, %d", gameState.TerrainMin, gameState.TerrainMax)
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
	subFS, err := web.FS()
	if err != nil {
		t.Fatalf("failed to open embedded subFS: %v", err)
	}

	requiredFiles := []string{
		"index.html",
		"game.js",
		"style.css",
		"admin/index.html",
		"admin/admin.css",
		"admin/admin.js",
	}
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

func TestAdminDashboardEndpoint(t *testing.T) {
	// Test FileServer handling of /admin and /admin/
	subFS, err := web.FS()
	if err != nil {
		t.Fatalf("failed to open embedded subFS: %v", err)
	}
	fileHandler := http.FileServer(http.FS(subFS))

	// 1. Test /admin/ directly returns HTTP 200 and contains admin dashboard title
	req := httptest.NewRequest("GET", "/admin/", nil)
	w := httptest.NewRecorder()
	fileHandler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200 for /admin/, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Commander Admin Console") {
		t.Errorf("expected /admin/ body to contain 'Commander Admin Console', got: %s", body)
	}

	// 2. Test /admin redirects to /admin/ (HTTP 301 Moved Permanently)
	reqRedirect := httptest.NewRequest("GET", "/admin", nil)
	wRedirect := httptest.NewRecorder()
	fileHandler.ServeHTTP(wRedirect, reqRedirect)

	if wRedirect.Code != 301 {
		t.Fatalf("expected HTTP 301 redirect for /admin, got %d", wRedirect.Code)
	}
}

func TestInactivePlayerRandomDirection(t *testing.T) {
	leftCount := 0
	rightCount := 0
	fireCount := 0

	for range 150 {
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
		if p1.ActionType != actionLeft && p1.ActionType != actionRight && p1.ActionType != actionFire {
			t.Errorf("expected ActionType to be LEFT, RIGHT, or FIRE, got %s", p1.ActionType)
		}
		switch p1.ActionType {
		case actionLeft:
			leftCount++
		case actionRight:
			rightCount++
		case actionFire:
			fireCount++
			if p1.Angle < 20 || p1.Angle > 150 {
				t.Errorf("expected uncommanded fire angle in [20, 150], got %d", p1.Angle)
			}
			if p1.Power < 40 || p1.Power > 80 {
				t.Errorf("expected uncommanded fire power in [40, 80], got %d", p1.Power)
			}
		}
		gameState.mu.Unlock()
	}

	if leftCount == 0 || rightCount == 0 || fireCount == 0 {
		t.Errorf("expected LEFT, RIGHT, and FIRE to all be selected across trials, got left=%d, right=%d, fire=%d", leftCount, rightCount, fireCount)
	}
}

func TestInactivePlayerNotWaitedOn(t *testing.T) {
	resetGameStateForTest()

	gameState.mu.Lock()
	gameState.RoundID = 1
	// Alice was active in Round 1
	gameState.Players["Alice"] = &Player{Name: "Alice", LastActiveRound: 1, Fired: false, Joined: true}
	// Bob was idle in Round 1 (LastActiveRound = 0)
	gameState.Players["Bob"] = &Player{Name: "Bob", LastActiveRound: 0, Fired: false, Joined: true}
	gameState.mu.Unlock()

	startInputPhase()

	// Alice fires
	processCommand("Alice", "%fire 45 50", nil)

	// Since Bob was inactive last round, we do not wait for the full round time on Bob.
	// Alice firing truncates the timer to minDuration (clamped to InputDuration=2s in test) + 500ms fast-forward.
	time.Sleep(3000 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		t.Errorf("expected phaseAction after sole active player Alice fired and window elapsed, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
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
	terrain := generateTerrain(20, 75)
	if len(terrain) != defaultTerrainWidth {
		t.Fatalf("expected terrain length %d, got %d", defaultTerrainWidth, len(terrain))
	}

	// In canvas space:
	// maxPct (75%) corresponds to minY (highest point toward top of screen)
	// minPct (20%) corresponds to maxY (lowest point toward bottom of screen)
	minY := float64(defaultTerrainHeight) * (1.0 - 0.75)
	maxY := float64(defaultTerrainHeight) * (1.0 - 0.20)

	for i, y := range terrain {
		if y < minY || y > maxY {
			t.Errorf("terrain at index %d out of expected vertical bounds [%f, %f]: %f", i, minY, maxY, y)
		}
		if i > 0 {
			diff := math.Abs(terrain[i] - terrain[i-1])
			if diff > 2.5 {
				t.Errorf("slope too steep at index %d: diff=%f", i, diff)
			}
		}
	}

	// Test custom constrained bounds (e.g. bottom 10% to 30% of screen)
	constrained := generateTerrain(10, 30)
	cMinY := float64(defaultTerrainHeight) * (1.0 - 0.30)
	cMaxY := float64(defaultTerrainHeight) * (1.0 - 0.10)
	for i, y := range constrained {
		if y < cMinY || y > cMaxY {
			t.Errorf("constrained terrain at index %d out of bounds [%f, %f]: %f", i, cMinY, cMaxY, y)
		}
	}
}

func TestGetTerrainHeight(t *testing.T) {
	terrain := generateTerrain(20, 75)

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

func TestTerrainCommand(t *testing.T) {
	resetGameStateForTest()

	// Default values
	gameState.mu.Lock()
	if gameState.TerrainMin != 20 || gameState.TerrainMax != 75 {
		t.Errorf("expected default TerrainMin=20, TerrainMax=75; got %d, %d", gameState.TerrainMin, gameState.TerrainMax)
	}
	gameState.mu.Unlock()

	// %terrain 30 70
	processCommand("Admin", "%terrain 30 70", nil)
	gameState.mu.Lock()
	if gameState.TerrainMin != 30 || gameState.TerrainMax != 70 {
		t.Errorf("expected TerrainMin=30, TerrainMax=70; got %d, %d", gameState.TerrainMin, gameState.TerrainMax)
	}
	gameState.mu.Unlock()

	// %terrain with percent signs: %terrain 40% 75%
	processCommand("Admin", "%terrain 40% 75%", nil)
	gameState.mu.Lock()
	if gameState.TerrainMin != 40 || gameState.TerrainMax != 75 {
		t.Errorf("expected TerrainMin=40, TerrainMax=75; got %d, %d", gameState.TerrainMin, gameState.TerrainMax)
	}
	gameState.mu.Unlock()

	// Invalid range: min > max-10 should be rejected
	processCommand("Admin", "%terrain 80 30", nil)
	gameState.mu.Lock()
	if gameState.TerrainMin != 40 || gameState.TerrainMax != 75 {
		t.Errorf("expected unchanged bounds after invalid command; got %d, %d", gameState.TerrainMin, gameState.TerrainMax)
	}
	gameState.mu.Unlock()

	// %terrain reset
	processCommand("Admin", "%terrain reset", nil)
	gameState.mu.Lock()
	if gameState.TerrainMin != 20 || gameState.TerrainMax != 75 {
		t.Errorf("expected reset to 20 and 75; got %d, %d", gameState.TerrainMin, gameState.TerrainMax)
	}
	gameState.mu.Unlock()
}

func TestCraterDeduplication(t *testing.T) {
	clearAppliedCraters()

	shotID := "1_Player1"

	appliedCratersMu.Lock()
	if appliedCraters[shotID] {
		t.Fatalf("expected crater %s to not be applied yet", shotID)
	}
	appliedCraters[shotID] = true
	appliedCratersMu.Unlock()

	// Second check: should report already applied
	appliedCratersMu.Lock()
	isApplied := appliedCraters[shotID]
	appliedCratersMu.Unlock()

	if !isApplied {
		t.Errorf("expected crater %s to be marked as applied", shotID)
	}

	// Clearing craters
	clearAppliedCraters()
	appliedCratersMu.Lock()
	afterClear := appliedCraters[shotID]
	appliedCratersMu.Unlock()

	if afterClear {
		t.Errorf("expected crater %s to be cleared after clearAppliedCraters", shotID)
	}
}

func TestHasPermission(t *testing.T) {
	broadcaster := &twitch.User{Name: "Streamer", IsBroadcaster: true}
	mod := &twitch.User{Name: "ModUser", IsMod: true}
	vip := &twitch.User{Name: "VipUser", IsVip: true}
	sub := &twitch.User{Name: "SubUser", Badges: map[string]int{"subscriber": 1}}
	viewer := &twitch.User{Name: "RegularViewer"}

	// Local / nil user always allowed
	if !hasPermission(nil, "broadcaster") {
		t.Errorf("expected nil user to have broadcaster permission")
	}

	// Required: broadcaster
	if !hasPermission(broadcaster, "broadcaster") {
		t.Errorf("expected broadcaster to have broadcaster perm")
	}
	if hasPermission(mod, "broadcaster") {
		t.Errorf("expected mod to NOT have broadcaster perm")
	}
	if hasPermission(viewer, "broadcaster") {
		t.Errorf("expected viewer to NOT have broadcaster perm")
	}

	// Required: mod
	if !hasPermission(broadcaster, "mod") || !hasPermission(mod, "mod") {
		t.Errorf("expected broadcaster and mod to have mod perm")
	}
	if hasPermission(vip, "mod") || hasPermission(viewer, "mod") {
		t.Errorf("expected vip and viewer to NOT have mod perm")
	}

	// Required: vip
	if !hasPermission(broadcaster, "vip") || !hasPermission(mod, "vip") || !hasPermission(vip, "vip") {
		t.Errorf("expected broadcaster, mod, and vip to have vip perm")
	}
	if hasPermission(sub, "vip") || hasPermission(viewer, "vip") {
		t.Errorf("expected sub and viewer to NOT have vip perm")
	}

	// Required: sub
	if !hasPermission(sub, "sub") || !hasPermission(vip, "sub") || !hasPermission(mod, "sub") || !hasPermission(broadcaster, "sub") {
		t.Errorf("expected sub, vip, mod, and broadcaster to have sub perm")
	}
	if hasPermission(viewer, "sub") {
		t.Errorf("expected viewer to NOT have sub perm")
	}

	// Required: all
	if !hasPermission(viewer, "all") {
		t.Errorf("expected viewer to have all perm")
	}
}

func TestPermissionCommands(t *testing.T) {
	resetGameStateForTest()

	broadcaster := &twitch.User{Name: "Streamer", IsBroadcaster: true}
	mod := &twitch.User{Name: "ModUser", IsMod: true}
	viewer := &twitch.User{Name: "RegularViewer"}

	// 1. Default permissions: broadcaster only
	gameState.mu.Lock()
	if gameState.StartPerm != "broadcaster" || gameState.ConfigPerm != "broadcaster" {
		t.Errorf("expected default permissions to be broadcaster, got start=%s, config=%s", gameState.StartPerm, gameState.ConfigPerm)
	}
	gameState.mu.Unlock()

	// 2. Regular viewer cannot %startgame (even if joined)
	processCommand("RegularViewer", "%join Kappa", nil, viewer)
	processCommand("RegularViewer", "%startgame", nil, viewer)
	gameState.mu.Lock()
	if gameState.Phase != phaseIdle {
		t.Errorf("expected phase to remain IDLE when regular viewer executes %%startgame, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// 3. Regular viewer cannot change settings
	processCommand("RegularViewer", "%roundtime 45", nil, viewer)
	gameState.mu.Lock()
	if gameState.InputDuration == 45 {
		t.Errorf("expected InputDuration to remain unchanged when regular viewer executes %%roundtime")
	}
	gameState.mu.Unlock()

	// 4. Regular viewer cannot change permissions
	processCommand("RegularViewer", "%startperm all", nil, viewer)
	gameState.mu.Lock()
	if gameState.StartPerm == "all" {
		t.Errorf("expected StartPerm to remain unchanged when regular viewer executes %%startperm")
	}
	gameState.mu.Unlock()

	// 5. Mod cannot change permissions
	processCommand("ModUser", "%configperm all", nil, mod)
	gameState.mu.Lock()
	if gameState.ConfigPerm == "all" {
		t.Errorf("expected ConfigPerm to remain unchanged when mod executes %%configperm")
	}
	gameState.mu.Unlock()

	// 6. Broadcaster can configure permissions
	processCommand("Streamer", "%configperm mod", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.ConfigPerm != "mod" {
		t.Errorf("expected ConfigPerm to be 'mod', got %s", gameState.ConfigPerm)
	}
	gameState.mu.Unlock()

	// Now mod can change settings
	processCommand("ModUser", "%roundtime 35", nil, mod)
	gameState.mu.Lock()
	if gameState.InputDuration != 35 {
		t.Errorf("expected InputDuration=35 after mod command, got %d", gameState.InputDuration)
	}
	gameState.mu.Unlock()

	// 7. Broadcaster configures %startperm via unified %perm command
	processCommand("Streamer", "%perm start all", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.StartPerm != "all" {
		t.Errorf("expected StartPerm to be 'all', got %s", gameState.StartPerm)
	}
	gameState.mu.Unlock()

	// Now regular viewer can start game
	processCommand("RegularViewer", "%startgame", nil, viewer)
	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		t.Errorf("expected phase to be INPUT after viewer %%startgame with startPerm=all, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestFirePowerAndAngleClamping(t *testing.T) {
	resetGameStateForTest()

	processCommand("Alice", "%join Kappa", nil)
	processCommand("Alice", "%startgame", nil)

	// Fire with power > 100 and angle > 180
	processCommand("Alice", "%fire 250 999", nil)
	gameState.mu.Lock()
	alice := gameState.Players["Alice"]
	if alice.Power != 100 {
		t.Errorf("expected power clamped to 100, got %d", alice.Power)
	}
	if alice.Angle != 180 {
		t.Errorf("expected angle clamped to 180, got %d", alice.Angle)
	}
	gameState.mu.Unlock()

	// Reset to input phase for second test
	gameState.mu.Lock()
	gameState.Phase = phaseInput
	alice.Fired = false
	gameState.mu.Unlock()

	// Fire with power < 1 and angle < 0
	processCommand("Alice", "%fire -45 -50", nil)
	gameState.mu.Lock()
	if alice.Power != 1 {
		t.Errorf("expected power clamped to 1, got %d", alice.Power)
	}
	if alice.Angle != 0 {
		t.Errorf("expected angle clamped to 0, got %d", alice.Angle)
	}
	gameState.mu.Unlock()
}

func TestScoringPerKill(t *testing.T) {
	resetGameStateForTest()

	server := httptest.NewServer(websocket.Handler(handleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Setup players
	processCommand("Alice", "%join Kappa", nil)
	processCommand("Bob", "%join LUL", nil)

	// 1. Alice kills Bob
	gameState.mu.Lock()
	bob := gameState.Players["Bob"]
	checkTankCollisions(bob.X, bob.Y, 50, "Alice")
	gameState.mu.Unlock()

	gameState.mu.Lock()
	if !gameState.Players["Bob"].IsDead {
		t.Errorf("expected Bob to be dead")
	}
	if gameState.Leaderboard["Alice"] != 1 {
		t.Errorf("expected Alice score to be 1, got %d", gameState.Leaderboard["Alice"])
	}
	gameState.mu.Unlock()

	// 2. Environmental death (abyss): Charlie dies with no killer
	processCommand("Charlie", "%join PogChamp", nil)

	gameState.mu.Lock()
	gameState.Phase = phaseAction
	charlie := gameState.Players["Charlie"]
	charlie.Y = 2000.0
	gameState.Terrain[int(charlie.X)] = 2000.0 // Blast a hole so he falls
	gameState.mu.Unlock()

	// call updatePhysicsStep which should kill Charlie and end the game
	gameState.mu.Lock()
	if updatePhysicsStep(1.0) {
		checkGameOverAndTransition() // this unlocks mu!
	} else {
		gameState.mu.Unlock()
	}

	gameState.mu.Lock()
	if !gameState.Players["Charlie"].IsDead {
		t.Errorf("expected Charlie to be dead")
	}
	if gameState.Leaderboard["Charlie"] != 0 {
		t.Errorf("expected Charlie score to be 0, got %d", gameState.Leaderboard["Charlie"])
	}

	// Game over should be triggered by updatePhysicsStep because only Alice is left
	// Since Alice is the winner, she gets +5 points
	if gameState.Leaderboard["Alice"] != 6 {
		t.Errorf("expected Alice score to be 6 (1 kill + 5 win bonus) on game over, got %d", gameState.Leaderboard["Alice"])
	}
	if gameState.Phase != phaseCelebration {
		t.Errorf("expected Phase to be CELEBRATION, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestInputPhase_TenSecondMinimumWhenNoCommandsInPrevRound(t *testing.T) {
	resetGameStateForTest()

	// 1. Join two players
	processCommand("Alice", "%join Kappa", nil)
	processCommand("Bob", "%join LUL", nil)

	gameState.mu.Lock()
	gameState.InputDuration = 20
	gameState.mu.Unlock()

	// Start game -> Round 1 begins (prevRoundHadCommands is false)
	processCommand("Alice", "%startgame", nil)
	time.Sleep(50 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput, got %s", gameState.Phase)
	}
	if prevRoundHadCommands {
		gameState.mu.Unlock()
		t.Fatalf("expected prevRoundHadCommands to be false in round 1")
	}
	gameState.mu.Unlock()

	// 2. Alice fires at T=0. Because this is round 1 and Bob hasn't fired,
	// it should NOT fast forward immediately.
	processCommand("Alice", "%fire 45 50", nil)
	time.Sleep(200 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput to remain active in round 1 until all humans fire, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// 3. Now Bob fires too. All alive players have fired, so it should fast forward immediately!
	processCommand("Bob", "%left", nil)
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseAction after all alive players fired, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// 4. Test timer expiry with short InputDuration (simulating minDuration window reaching end)
	resetGameStateForTest()
	processCommand("Alice", "%join Kappa", nil)
	processCommand("Bob", "%join LUL", nil)
	gameState.mu.Lock()
	gameState.InputDuration = 1 // minDuration will be clamped to 1s
	gameState.mu.Unlock()

	processCommand("Alice", "%startgame", nil)
	time.Sleep(50 * time.Millisecond)

	// Alice fires; Bob does not
	processCommand("Alice", "%fire 45 50", nil)

	// After 200ms, should still be in INPUT phase
	time.Sleep(200 * time.Millisecond)
	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput at 200ms, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// After 1.5s total (> 1s minDuration + 500ms sleep), should have transitioned to ACTION phase
	time.Sleep(1500 * time.Millisecond)
	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseAction after minDuration elapsed, got %s", gameState.Phase)
	}
	// Verify Bob received an automated uncommanded action
	bob := gameState.Players["Bob"]
	if bob.ActionType != actionLeft && bob.ActionType != actionRight && bob.ActionType != actionFire {
		gameState.mu.Unlock()
		t.Errorf("expected Bob to receive a random uncommanded action, got %q", bob.ActionType)
	} else {
		gameState.mu.Unlock()
	}
}

func TestClearLeaderboard(t *testing.T) {
	resetGameStateForTest()
	_ = initDB(":memory:")
	defer closeDB()

	// Populate leaderboard in memory and DB
	incrementWin("Alice")
	incrementWin("Alice")
	incrementWin("Bob")

	gameState.mu.Lock()
	gameState.Leaderboard["Alice"] = 2
	gameState.Leaderboard["Bob"] = 1
	gameState.ConfigPerm = "broadcaster"
	gameState.mu.Unlock()

	regularUser := &twitch.User{Name: "Charlie", Badges: map[string]int{}}
	modUser := &twitch.User{Name: "ModUser", IsMod: true}
	broadcasterUser := &twitch.User{Name: "Streamer", IsBroadcaster: true}

	// 1. Regular user cannot clear leaderboard when ConfigPerm is broadcaster
	processCommand("Charlie", "%clearleaderboard", nil, regularUser)
	gameState.mu.Lock()
	if len(gameState.Leaderboard) != 2 {
		gameState.mu.Unlock()
		t.Fatalf("expected leaderboard not to be cleared by regular user, got len %d", len(gameState.Leaderboard))
	}
	gameState.mu.Unlock()

	// 2. Mod user cannot clear leaderboard when ConfigPerm is broadcaster
	processCommand("ModUser", "%clearleaderboard", nil, modUser)
	gameState.mu.Lock()
	if len(gameState.Leaderboard) != 2 {
		gameState.mu.Unlock()
		t.Fatalf("expected leaderboard not to be cleared by mod when ConfigPerm=broadcaster, got len %d", len(gameState.Leaderboard))
	}
	gameState.mu.Unlock()

	// 3. Broadcaster clears leaderboard
	processCommand("Streamer", "%clearleaderboard", nil, broadcasterUser)
	gameState.mu.Lock()
	if len(gameState.Leaderboard) != 0 {
		gameState.mu.Unlock()
		t.Fatalf("expected leaderboard to be empty after %%clearleaderboard, got %v", gameState.Leaderboard)
	}
	gameState.mu.Unlock()

	// Verify SQLite database was also cleared
	loadLeaderboard()
	gameState.mu.Lock()
	if len(gameState.Leaderboard) != 0 {
		gameState.mu.Unlock()
		t.Fatalf("expected SQLite leaderboard table to be empty after %%clearleaderboard, got %v", gameState.Leaderboard)
	}
	gameState.mu.Unlock()

	// 4. Test alias %resetleaderboard works when ConfigPerm allows it
	incrementWin("David")
	gameState.mu.Lock()
	gameState.Leaderboard["David"] = 1
	gameState.ConfigPerm = "mod"
	gameState.mu.Unlock()

	processCommand("ModUser", "%resetleaderboard", nil, modUser)
	gameState.mu.Lock()
	if len(gameState.Leaderboard) != 0 {
		gameState.mu.Unlock()
		t.Fatalf("expected leaderboard to be empty after %%resetleaderboard, got %v", gameState.Leaderboard)
	}
	gameState.mu.Unlock()

	// Verify SQLite database was also cleared
	loadLeaderboard()
	gameState.mu.Lock()
	if len(gameState.Leaderboard) != 0 {
		gameState.mu.Unlock()
		t.Fatalf("expected SQLite leaderboard table to be empty after %%resetleaderboard, got %v", gameState.Leaderboard)
	}
	gameState.mu.Unlock()
}

func TestDeletePlayer(t *testing.T) {
	resetGameStateForTest()
	_ = initDB(":memory:")
	defer closeDB()

	// Populate leaderboard
	incrementWin("Alice")
	incrementWin("Alice")
	incrementWin("Bob")
	incrementWin("Charlie")

	gameState.mu.Lock()
	gameState.Leaderboard["Alice"] = 2
	gameState.Leaderboard["Bob"] = 1
	gameState.Leaderboard["Charlie"] = 1
	gameState.ConfigPerm = "broadcaster"
	gameState.mu.Unlock()

	regularUser := &twitch.User{Name: "David", Badges: map[string]int{}}
	modUser := &twitch.User{Name: "ModUser", IsMod: true}
	broadcasterUser := &twitch.User{Name: "Streamer", IsBroadcaster: true}

	// 1. Regular user cannot delete player
	processCommand("David", "%deleteplayer Alice", nil, regularUser)
	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["Alice"]; !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice not to be deleted by regular user")
	}
	gameState.mu.Unlock()

	// 2. Mod user cannot delete player when ConfigPerm is broadcaster
	processCommand("ModUser", "%deleteplayer Alice", nil, modUser)
	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["Alice"]; !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice not to be deleted by mod when ConfigPerm is broadcaster")
	}
	gameState.mu.Unlock()

	// 3. Broadcaster deletes player case-insensitively ("alice" removes "Alice")
	processCommand("Streamer", "%deleteplayer alice", nil, broadcasterUser)
	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["Alice"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to be removed by broadcaster case-insensitively")
	}
	if len(gameState.Leaderboard) != 2 {
		gameState.mu.Unlock()
		t.Fatalf("expected 2 players remaining, got %d", len(gameState.Leaderboard))
	}
	gameState.mu.Unlock()

	// Verify SQLite database
	loadLeaderboard()
	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["Alice"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to be removed from SQLite database")
	}
	gameState.mu.Unlock()

	// 4. Test leading @ prefix and alias %removeplayer when ConfigPerm is mod
	gameState.mu.Lock()
	gameState.ConfigPerm = "mod"
	gameState.mu.Unlock()

	processCommand("ModUser", "%removeplayer @Bob", nil, modUser)
	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["Bob"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Bob to be removed via %%removeplayer with @ prefix")
	}
	if len(gameState.Leaderboard) != 1 || gameState.Leaderboard["Charlie"] != 1 {
		gameState.mu.Unlock()
		t.Fatalf("expected only Charlie remaining on leaderboard, got %v", gameState.Leaderboard)
	}
	gameState.mu.Unlock()

	// Verify SQLite database
	loadLeaderboard()
	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["Bob"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Bob to be removed from SQLite database")
	}
	if gameState.Leaderboard["Charlie"] != 1 {
		gameState.mu.Unlock()
		t.Fatalf("expected Charlie to remain in SQLite database, got %v", gameState.Leaderboard)
	}
	gameState.mu.Unlock()
}

func TestBotFillSystem(t *testing.T) {
	resetGameStateForTest()

	// 1. Zero humans: startInputPhase must abort and remain in phaseIdle
	startInputPhase()
	gameState.mu.Lock()
	if gameState.Phase != phaseIdle {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseIdle when 0 humans are present, got %s", gameState.Phase)
	}
	if len(gameState.Players) != 0 {
		gameState.mu.Unlock()
		t.Fatalf("expected 0 players, got %d", len(gameState.Players))
	}
	gameState.mu.Unlock()

	// 2. Add 1 human player and verify bot fill up to MinPlayers (5)
	processCommand("Alice", "%join", nil)
	startInputPhase()

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput after starting with human player, got %s", gameState.Phase)
	}
	if len(gameState.Players) != 5 {
		gameState.mu.Unlock()
		t.Fatalf("expected 5 players filled, got %d", len(gameState.Players))
	}

	// Verify Alice is human and bots have IsBot=true and Fired=true
	alice := gameState.Players["Alice"]
	if alice == nil || alice.IsBot {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to be human (!IsBot)")
	}

	botCount := 0
	for name, p := range gameState.Players {
		if p.IsBot {
			botCount++
			if !p.Fired {
				gameState.mu.Unlock()
				t.Errorf("expected bot %s to have Fired=true", name)
			}
		}
	}
	if botCount != 4 {
		gameState.mu.Unlock()
		t.Fatalf("expected 4 bots filled, got %d", botCount)
	}
	gameState.mu.Unlock()

	// 3. Test bot fill with MinPlayers > len(BotList) to verify nameless bots
	resetGameStateForTest()
	gameState.mu.Lock()
	gameState.MinPlayers = 7
	gameState.mu.Unlock()

	processCommand("Bob", "%join", nil)
	startInputPhase()

	gameState.mu.Lock()
	if len(gameState.Players) != 7 {
		gameState.mu.Unlock()
		t.Fatalf("expected 7 players filled, got %d", len(gameState.Players))
	}

	namelessCount := 0
	for k, p := range gameState.Players {
		if p.IsBot && (p.Name == "" || strings.HasPrefix(k, "_bot_")) {
			namelessCount++
		}
	}
	// 1 human + 5 named bots = 6; 7th must be nameless
	if namelessCount != 1 {
		gameState.mu.Unlock()
		t.Fatalf("expected 1 nameless bot, got %d", namelessCount)
	}
	gameState.mu.Unlock()
}

func TestBotCullAndReplacement(t *testing.T) {
	resetGameStateForTest()

	// Start game with 1 human + 4 bots (MinPlayers = 5)
	processCommand("Alice", "%join", nil)
	startInputPhase()

	gameState.mu.Lock()
	if len(gameState.Players) != 5 {
		gameState.mu.Unlock()
		t.Fatalf("expected 5 players, got %d", len(gameState.Players))
	}

	// Record bots present before Bob joins
	botsBefore := make(map[string]*Player)
	for k, p := range gameState.Players {
		if p.IsBot {
			botsBefore[k] = p
		}
	}
	gameState.mu.Unlock()

	// Bob joins: should cull a named bot and inherit its position
	processCommand("Bob", "%join", nil)

	gameState.mu.Lock()
	if len(gameState.Players) != 5 {
		gameState.mu.Unlock()
		t.Fatalf("expected 5 players preserved after replacement, got %d", len(gameState.Players))
	}

	var culledBot *Player
	var culledKey string
	for k, p := range botsBefore {
		if _, exists := gameState.Players[k]; !exists {
			culledBot = p
			culledKey = k
			break
		}
	}
	if culledBot == nil {
		gameState.mu.Unlock()
		t.Fatalf("expected a bot to be culled")
	}
	if culledBot.Name == "" {
		gameState.mu.Unlock()
		t.Fatalf("expected a named bot to be culled first, but culled was nameless key %s", culledKey)
	}

	bob := gameState.Players["Bob"]
	if bob == nil || bob.IsBot {
		gameState.mu.Unlock()
		t.Fatalf("expected Bob to exist as a human player")
	}
	if bob.X != culledBot.X || bob.Y != culledBot.Y {
		gameState.mu.Unlock()
		t.Fatalf("expected Bob to inherit bot position (%.2f, %.2f), got (%.2f, %.2f)", culledBot.X, culledBot.Y, bob.X, bob.Y)
	}

	// Real players are never culled: Alice and Bob must both exist
	if _, exists := gameState.Players["Alice"]; !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to not be culled")
	}
	gameState.mu.Unlock()
}

func TestBotScoringAndLeaderboardExclusion(t *testing.T) {
	resetGameStateForTest()
	_ = initDB(":memory:")
	defer closeDB()

	// Setup: human Alice, bot TargetBot, configured BotPoints = 3
	gameState.mu.Lock()
	gameState.BotPoints = 3
	gameState.Players["Alice"] = &Player{Name: "Alice", IsBot: false}
	gameState.Players["TargetBot"] = &Player{Name: "TargetBot", IsBot: true}
	gameState.mu.Unlock()

	// 1. Alice kills TargetBot -> Alice earns BotPoints (3)
	gameState.mu.Lock()
	targetBot := gameState.Players["TargetBot"]
	checkTankCollisions(targetBot.X, targetBot.Y, 50.0, "Alice")
	gameState.mu.Unlock()

	gameState.mu.Lock()
	if gameState.Leaderboard["Alice"] != 3 {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to have 3 points from bot kill, got %d", gameState.Leaderboard["Alice"])
	}
	if _, exists := gameState.Leaderboard["TargetBot"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected TargetBot not to appear on leaderboard")
	}
	gameState.mu.Unlock()

	// 2. TargetBot kills Alice -> TargetBot must NOT earn points
	gameState.mu.Lock()
	alice := gameState.Players["Alice"]
	checkTankCollisions(alice.X, alice.Y, 50.0, "TargetBot")
	gameState.mu.Unlock()

	// 3. Game over with bot winner -> TargetBot must NOT earn points
	// Simulation game over via updatePhysicsStep
	gameState.mu.Lock()
	gameState.Phase = phaseAction
	gameState.Projectiles = []Projectile{}
	gameState.Explosions = []Explosion{}
	gameState.mu.Unlock()

	gameState.mu.Lock()
	if updatePhysicsStep(1.0) {
		checkGameOverAndTransition()
	} else {
		gameState.mu.Unlock()
	}

	gameState.mu.Lock()
	if _, exists := gameState.Leaderboard["TargetBot"]; exists {
		t.Fatalf("expected TargetBot never to appear on leaderboard after winning, got %v", gameState.Leaderboard)
	}
	// Alice score should still be 3
	if gameState.Leaderboard["Alice"] != 3 {
		t.Fatalf("expected Alice score to remain 3, got %v", gameState.Leaderboard["Alice"])
	}
	gameState.mu.Unlock()
}

func TestBotConfigurationCommands(t *testing.T) {
	resetGameStateForTest()
	_ = initDB(":memory:")
	defer closeDB()

	broadcaster := &twitch.User{Name: "Streamer", IsBroadcaster: true}

	// 1. %minplayers
	processCommand("Streamer", "%minplayers 8", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.MinPlayers != 8 {
		gameState.mu.Unlock()
		t.Fatalf("expected MinPlayers to be 8, got %d", gameState.MinPlayers)
	}
	gameState.mu.Unlock()

	// Verify persistence in SQLite
	loadSettings()
	gameState.mu.Lock()
	if gameState.MinPlayers != 8 {
		gameState.mu.Unlock()
		t.Fatalf("expected MinPlayers=8 to persist in SQLite, got %d", gameState.MinPlayers)
	}
	gameState.mu.Unlock()

	// 2. %botfill
	processCommand("Streamer", "%botfill off", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.BotFill != false {
		gameState.mu.Unlock()
		t.Fatalf("expected BotFill to be false, got %v", gameState.BotFill)
	}
	gameState.mu.Unlock()

	loadSettings()
	gameState.mu.Lock()
	if gameState.BotFill != false {
		gameState.mu.Unlock()
		t.Fatalf("expected BotFill=false to persist in SQLite")
	}
	gameState.mu.Unlock()

	// 3. %botpoints
	processCommand("Streamer", "%botpoints 5", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.BotPoints != 5 {
		gameState.mu.Unlock()
		t.Fatalf("expected BotPoints to be 5, got %d", gameState.BotPoints)
	}
	gameState.mu.Unlock()

	loadSettings()
	gameState.mu.Lock()
	if gameState.BotPoints != 5 {
		gameState.mu.Unlock()
		t.Fatalf("expected BotPoints=5 to persist in SQLite")
	}
	gameState.mu.Unlock()

	// 4. %botlist add / remove
	processCommand("Streamer", "%botlist add EliteSniper", nil, broadcaster)
	gameState.mu.Lock()
	found := slices.Contains(gameState.BotList, "EliteSniper")
	if !found {
		gameState.mu.Unlock()
		t.Fatalf("expected EliteSniper to be added to BotList")
	}
	gameState.mu.Unlock()

	processCommand("Streamer", "%botlist remove EliteSniper", nil, broadcaster)
	gameState.mu.Lock()
	for _, b := range gameState.BotList {
		if b == "EliteSniper" {
			gameState.mu.Unlock()
			t.Fatalf("expected EliteSniper to be removed from BotList")
		}
	}
	gameState.mu.Unlock()
}

func TestAutoRoundCustomMinutes(t *testing.T) {
	resetGameStateForTest()
	_ = initDB(":memory:")
	defer closeDB()

	broadcaster := &twitch.User{Name: "Streamer", IsBroadcaster: true}

	processCommand("Streamer", "%autoround 12", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.AutoRound != 12 {
		gameState.mu.Unlock()
		t.Fatalf("expected AutoRound to be 12, got %d", gameState.AutoRound)
	}
	gameState.mu.Unlock()

	// Verify persistence in SQLite
	loadSettings()
	gameState.mu.Lock()
	if gameState.AutoRound != 12 {
		gameState.mu.Unlock()
		t.Fatalf("expected AutoRound=12 to persist in SQLite")
	}
	gameState.mu.Unlock()
}

func TestBotGame_InactivePlayerRound2InactivityTimer(t *testing.T) {
	resetGameStateForTest()

	// 1 human joins against bots
	processCommand("Alice", "%join Kappa", nil)

	gameState.mu.Lock()
	gameState.InputDuration = 1 // Use 1s so test executes rapidly
	gameState.mu.Unlock()

	// Start game -> Round 1
	processCommand("Alice", "%startgame", nil)
	time.Sleep(50 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput in Round 1, got %s", gameState.Phase)
	}
	if prevRoundHadCommands {
		gameState.mu.Unlock()
		t.Fatalf("expected prevRoundHadCommands to be false in round 1")
	}
	gameState.mu.Unlock()

	// Alice does NOT enter a command in Round 1.
	// Wait for Round 1 to complete and execute actions:
	time.Sleep(1600 * time.Millisecond)

	// Simulate start of Round 2
	startInputPhase()
	time.Sleep(50 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.RoundID != 2 {
		gameState.mu.Unlock()
		t.Fatalf("expected RoundID=2, got %d", gameState.RoundID)
	}
	// Alice was inactive in Round 1, bots shouldn't mark prevRoundHadCommands = true!
	if prevRoundHadCommands {
		gameState.mu.Unlock()
		t.Fatalf("expected prevRoundHadCommands to be false because human Alice was inactive in round 1")
	}
	// In Round 2 with bots and inactive human, TimerRemaining should be clamped to 1s
	if gameState.TimerRemaining != 1 {
		gameState.mu.Unlock()
		t.Fatalf("expected TimerRemaining to be 1, got %d", gameState.TimerRemaining)
	}
	// At 200ms, should still be in INPUT phase during window
	gameState.mu.Unlock()
	time.Sleep(200 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput during inactivity window at 250ms, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// After 1s duration + 500ms delay, should have transitioned to phaseAction
	time.Sleep(1500 * time.Millisecond)
	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseAction after inactivity window elapsed, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestBotGame_InactivePlayerFiresDuringWindow(t *testing.T) {
	resetGameStateForTest()

	// 1 human joins against bots
	processCommand("Alice", "%join Kappa", nil)

	gameState.mu.Lock()
	gameState.InputDuration = 10
	gameState.RoundID = 1
	// Alice was inactive in Round 1 (LastActiveRound = 0)
	alice := gameState.Players["Alice"]
	alice.LastActiveRound = 0
	gameState.mu.Unlock()

	// Start Round 2
	startInputPhase()
	time.Sleep(50 * time.Millisecond)

	gameState.mu.Lock()
	if prevRoundHadCommands {
		gameState.mu.Unlock()
		t.Fatalf("expected prevRoundHadCommands to be false")
	}
	gameState.mu.Unlock()

	// Alice wakes up and fires during the window
	processCommand("Alice", "%fire 45 60", nil)

	// All alive humans (Alice) have fired, should fast-forward immediately within 700ms
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseAction after Alice fired, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestRound2_ActiveHumanFromRound1TriggersTenSecondWindow(t *testing.T) {
	resetGameStateForTest()

	// 2 humans join with bots: Human A and Human B
	processCommand("Alice", "%join Kappa", nil)
	processCommand("Bob", "%join LUL", nil)

	gameState.mu.Lock()
	gameState.InputDuration = 30
	gameState.RoundID = 1
	// Human A was idle in Round 1 (LastActiveRound = 0), Human B was active (LastActiveRound = 1)
	gameState.Players["Alice"].LastActiveRound = 0
	gameState.Players["Bob"].LastActiveRound = 1
	gameState.mu.Unlock()

	// Start Round 2
	startInputPhase()
	time.Sleep(50 * time.Millisecond)

	gameState.mu.Lock()
	if !prevRoundHadCommands {
		gameState.mu.Unlock()
		t.Fatalf("expected prevRoundHadCommands to be true because Bob was active in round 1")
	}
	if gameState.TimerRemaining != 30 {
		gameState.mu.Unlock()
		t.Fatalf("expected initial TimerRemaining to be 30, got %d", gameState.TimerRemaining)
	}
	gameState.mu.Unlock()

	// Simulate 1.5s elapsed, then Human B (the only active human from round 1) enters a command
	time.Sleep(1500 * time.Millisecond)
	processCommand("Bob", "%fire 45 60", nil)

	gameState.mu.Lock()
	// All active humans from last round have now commanded!
	// Elapsed is ~1.5s, so remaining to reach 10s should be ~8 or 9 seconds
	if gameState.TimerRemaining < 8 || gameState.TimerRemaining > 9 {
		t.Errorf("expected TimerRemaining to be 8 or 9 seconds after Bob fires, got %d", gameState.TimerRemaining)
	}
	if !fastForwardScheduled {
		t.Errorf("expected fastForwardScheduled to be true")
	}
	if gameState.Phase != phaseInput {
		t.Errorf("expected phaseInput still active during the 10s minimum window, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// If Alice also fires now, all alive humans have fired -> immediate fast-forward!
	processCommand("Alice", "%left", nil)
	time.Sleep(700 * time.Millisecond)

	gameState.mu.Lock()
	if gameState.Phase != phaseAction {
		t.Fatalf("expected immediate phaseAction after all humans fired, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()
}

func TestMatchKillsRecap(t *testing.T) {
	resetGameStateForTest()

	// Human Alice and bot TargetBot
	processCommand("Alice", "%join Kappa", nil)
	processCommand("TargetBot", "%join", nil)

	gameState.mu.Lock()
	gameState.Players["Alice"].X = 100
	gameState.Players["Alice"].Y = 500
	gameState.Players["Alice"].Angle = 45
	gameState.Players["Alice"].Power = 60
	gameState.Players["Alice"].IsBot = false

	gameState.Players["TargetBot"].X = 200
	gameState.Players["TargetBot"].Y = 500
	gameState.Players["TargetBot"].IsBot = true

	gameState.Players["AbyssBot"] = &Player{
		Name:  "AbyssBot",
		IsBot: true,
		X:     300,
		Y:     float64(defaultTerrainHeight) + 10,
	}
	gameState.Terrain[300] = 2000.0 // Hole in terrain so tank falls into abyss

	gameState.RoundID = 1
	gameState.Phase = phaseAction

	// 1. Check direct tank collision: Alice kills TargetBot
	checkTankCollisions(200, 500, 50.0, "Alice")

	if len(gameState.MatchKills) != 1 {
		t.Fatalf("expected 1 kill event, got %d", len(gameState.MatchKills))
	}
	k1 := gameState.MatchKills[0]
	if k1.Killer != "Alice" || k1.KillerIsBot != false {
		t.Errorf("expected killer Alice (human), got %s (isBot=%v)", k1.Killer, k1.KillerIsBot)
	}
	if k1.Victim != "TargetBot" || k1.VictimIsBot != true {
		t.Errorf("expected victim TargetBot (bot), got %s (isBot=%v)", k1.Victim, k1.VictimIsBot)
	}
	if k1.Angle != 45 || k1.Power != 60 {
		t.Errorf("expected angle 45, power 60, got angle %d, power %d", k1.Angle, k1.Power)
	}

	// 2. Check abyss fall: AbyssBot falls off screen
	updatePhysicsStep(1.0)
	if len(gameState.MatchKills) != 2 {
		t.Fatalf("expected 2 kill events after abyss fall, got %d", len(gameState.MatchKills))
	}
	k2 := gameState.MatchKills[1]
	if k2.Killer != "" {
		t.Errorf("expected empty killer for abyss death, got %s", k2.Killer)
	}
	if k2.Victim != "AbyssBot" || k2.VictimIsBot != true {
		t.Errorf("expected victim AbyssBot (bot), got %s (isBot=%v)", k2.Victim, k2.VictimIsBot)
	}
	gameState.mu.Unlock()

	// 3. Verify WebSocket broadcast carries MatchKills in stateCopy
	server := httptest.NewServer(websocket.Handler(handleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var initMsg struct {
		Type    string    `json:"type"`
		Payload GameState `json:"payload"`
	}
	if err := websocket.JSON.Receive(conn, &initMsg); err != nil {
		t.Fatalf("failed to receive initial state update: %v", err)
	}
	if len(initMsg.Payload.MatchKills) != 2 {
		t.Errorf("expected 2 MatchKills in broadcast GameState payload, got %d", len(initMsg.Payload.MatchKills))
	}

	// 4. Resetting match state clears MatchKills
	gameState.mu.Lock()
	gameState.Phase = phaseCelebration
	gameState.mu.Unlock()
	resetMatchState()

	gameState.mu.Lock()
	if len(gameState.MatchKills) != 0 {
		t.Errorf("expected MatchKills to be cleared after resetMatchState, got %d", len(gameState.MatchKills))
	}
	gameState.mu.Unlock()
}

func TestCCCommandsAndStorage(t *testing.T) {
	testDBPath := filepath.Join(t.TempDir(), "test_cc.db")
	if err := initDB(testDBPath); err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	defer closeDB()

	// Broadcaster user for permission checks
	adminUser := &twitch.User{
		Name:   "AdminUser",
		Badges: map[string]int{"broadcaster": 1},
	}

	// 1. Initial defaults
	gameState.mu.Lock()
	gameState.CCEnabled = false
	gameState.CCServerURL = "wss://st-cc.poundsigndesign.com"
	gameState.mu.Unlock()

	// 2. Test %cc on
	processCommand("AdminUser", "%cc on", nil, adminUser)
	gameState.mu.Lock()
	if !gameState.CCEnabled {
		t.Errorf("expected CCEnabled to be true after %%cc on")
	}
	gameState.mu.Unlock()
	if getSetting("cc_enabled") != "1" {
		t.Errorf("expected cc_enabled setting to be '1', got '%s'", getSetting("cc_enabled"))
	}

	// 3. Test %cc url
	testURL := "wss://custom-cc.example.com"
	processCommand("AdminUser", "%cc url "+testURL, nil, adminUser)
	gameState.mu.Lock()
	if gameState.CCServerURL != testURL {
		t.Errorf("expected CCServerURL to be '%s', got '%s'", testURL, gameState.CCServerURL)
	}
	gameState.mu.Unlock()
	if getSetting("cc_url") != testURL {
		t.Errorf("expected cc_url setting to be '%s', got '%s'", testURL, getSetting("cc_url"))
	}

	// 4. Test %config cc off
	processCommand("AdminUser", "%config cc off", nil, adminUser)
	gameState.mu.Lock()
	if gameState.CCEnabled {
		t.Errorf("expected CCEnabled to be false after %%config cc off")
	}
	gameState.mu.Unlock()
	if getSetting("cc_enabled") != "0" {
		t.Errorf("expected cc_enabled setting to be '0', got '%s'", getSetting("cc_enabled"))
	}

	// 5. Test saving and retrieving token
	saveSetting("cc_host_token", "sample.token.12345")
	if token := getSetting("cc_host_token"); token != "sample.token.12345" {
		t.Errorf("expected saved token 'sample.token.12345', got '%s'", token)
	}

	// 6. Test loadSettings reloading persisted C&C configuration
	saveSetting("cc_enabled", "1")
	saveSetting("cc_url", "wss://reloaded-cc.example.com")
	loadSettings()

	gameState.mu.Lock()
	if !gameState.CCEnabled {
		t.Errorf("expected CCEnabled to be true after loadSettings")
	}
	if gameState.CCServerURL != "wss://reloaded-cc.example.com" {
		t.Errorf("expected CCServerURL 'wss://reloaded-cc.example.com', got '%s'", gameState.CCServerURL)
	}
	gameState.mu.Unlock()

	// 7. Test %cc reset deletes token
	saveSetting("cc_host_token", "sample.token.to.reset")
	processCommand("AdminUser", "%cc reset", nil, adminUser)
	if token := getSetting("cc_host_token"); token != "" {
		t.Errorf("expected cc_host_token to be cleared after %%cc reset, got '%s'", token)
	}

	// Clean up
	StopCCClient()
}

func TestBroadcastViewerState(t *testing.T) {
	resetGameStateForTest()

	msgChan := make(chan WSMessage, 10)
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		for {
			var m WSMessage
			if err := websocket.JSON.Receive(ws, &m); err == nil {
				msgChan <- m
			} else {
				return
			}
		}
	}))
	defer server.Close()

	clientWS, err := websocket.Dial("ws://"+server.Listener.Addr().String(), "", "http://localhost/")
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}
	defer func() { _ = clientWS.Close() }()

	setCCConn(clientWS)
	defer setCCConn(nil)

	// 1. Initial broadcast sends state
	gameState.mu.Lock()
	gameState.Phase = phaseInput
	gameState.RoundID = 1
	gameState.TimerRemaining = 15
	gameState.mu.Unlock()

	BroadcastViewerState()

	select {
	case msg := <-msgChan:
		if msg.Type != "GAME_STATE" {
			t.Fatalf("expected message type GAME_STATE, got %s", msg.Type)
		}
		payloadBytes, _ := json.Marshal(msg.Payload)
		var vs ViewerState
		if err := json.Unmarshal(payloadBytes, &vs); err != nil {
			t.Fatalf("failed to unmarshal ViewerState: %v", err)
		}
		if vs.Phase != phaseInput || vs.RoundID != 1 || vs.TimerRemaining != 15 {
			t.Errorf("unexpected ViewerState: %+v", vs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ViewerState")
	}

	// 2. Duplicate broadcast should be deduplicated (no message sent)
	BroadcastViewerState()
	select {
	case msg := <-msgChan:
		t.Fatalf("expected duplicate state to be suppressed, but received: %+v", msg)
	case <-time.After(200 * time.Millisecond):
		// Expected: no duplicate message
	}

	// 3. Changed timer should trigger new update
	gameState.mu.Lock()
	gameState.TimerRemaining = 14
	gameState.Players = map[string]*Player{
		"Alice": {Name: "Alice", IsDead: false},
		"Bob":   {Name: "Bob", IsDead: true},
	}
	gameState.mu.Unlock()

	BroadcastViewerState()
	select {
	case msg := <-msgChan:
		payloadBytes, _ := json.Marshal(msg.Payload)
		var vs ViewerState
		_ = json.Unmarshal(payloadBytes, &vs)
		if vs.TimerRemaining != 14 {
			t.Errorf("expected updated TimerRemaining 14, got %d", vs.TimerRemaining)
		}
		if len(vs.Players) != 1 || vs.Players[0] != "alice" {
			t.Errorf("expected alive players [alice], got %+v", vs.Players)
		}
		if vs.PlayersCount != 1 {
			t.Errorf("expected players count 1, got %d", vs.PlayersCount)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for updated ViewerState")
	}

	// 4. Adding a new alive player triggers an update
	gameState.mu.Lock()
	gameState.Players["Charlie"] = &Player{Name: "Charlie", IsDead: false}
	gameState.mu.Unlock()

	BroadcastViewerState()
	select {
	case msg := <-msgChan:
		payloadBytes, _ := json.Marshal(msg.Payload)
		var vs ViewerState
		_ = json.Unmarshal(payloadBytes, &vs)
		if len(vs.Players) != 2 || vs.Players[0] != "alice" || vs.Players[1] != "charlie" {
			t.Errorf("expected alive players [alice, charlie], got %+v", vs.Players)
		}
		if vs.PlayersCount != 2 {
			t.Errorf("expected players count 2, got %d", vs.PlayersCount)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for player roster ViewerState")
	}
}

func TestProtractorCommand(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_protractor.db")
	if err := initDB(dbPath); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer closeDB()

	resetGameStateForTest()

	// 1. Check default
	gameState.mu.Lock()
	if gameState.ProtractorX != 250 || gameState.ProtractorY != 270 {
		t.Fatalf("expected defaults 250, 270, got %d, %d", gameState.ProtractorX, gameState.ProtractorY)
	}
	gameState.mu.Unlock()

	// 2. Run command via processCommand with terrainMax=50 (maxY = 1080 * 0.5 = 540)
	processCommand("Admin", "%terrain 20 50", nil, nil)
	processCommand("Admin", "%protractor 1500 500", nil, nil)

	gameState.mu.Lock()
	if gameState.ProtractorX != 1500 || gameState.ProtractorY != 500 {
		t.Fatalf("expected 1500, 500 after command, got %d, %d", gameState.ProtractorX, gameState.ProtractorY)
	}
	gameState.mu.Unlock()

	// 3. Verify saved to DB
	if xVal := getSetting("protractor_x"); xVal != "1500" {
		t.Fatalf("expected db protractor_x to be 1500, got %s", xVal)
	}
	if yVal := getSetting("protractor_y"); yVal != "500" {
		t.Fatalf("expected db protractor_y to be 500, got %s", yVal)
	}

	// 4. Test maxY clamping: TerrainMax=75 -> maxY = 1080 * 0.25 = 270
	processCommand("Admin", "%terrain 20 75", nil, nil)
	gameState.mu.Lock()
	if gameState.ProtractorY != 270 {
		t.Fatalf("expected protractorY to be clamped to 270 after terrain change, got %d", gameState.ProtractorY)
	}
	gameState.mu.Unlock()

	// Trying to set Y above 270 clamps it to 270
	processCommand("Admin", "%protractor 1000 600", nil, nil)
	gameState.mu.Lock()
	if gameState.ProtractorX != 1000 || gameState.ProtractorY != 270 {
		t.Fatalf("expected 1000, 270 due to maxY clamping, got %d, %d", gameState.ProtractorX, gameState.ProtractorY)
	}
	gameState.mu.Unlock()

	// 5. Test nosave
	processCommand("Admin", "%protractor 300 200 nosave", nil, nil)
	gameState.mu.Lock()
	if gameState.ProtractorX != 300 || gameState.ProtractorY != 200 {
		t.Fatalf("expected 300, 200 after nosave, got %d, %d", gameState.ProtractorX, gameState.ProtractorY)
	}
	gameState.mu.Unlock()
	if xVal := getSetting("protractor_x"); xVal != "1000" {
		t.Fatalf("expected db protractor_x to remain 1000 after nosave, got %s", xVal)
	}

	// 6. Test reset
	processCommand("Admin", "%protractor reset", nil, nil)
	gameState.mu.Lock()
	if gameState.ProtractorX != 250 || gameState.ProtractorY != 270 {
		t.Fatalf("expected 250, 270 after reset (clamped to maxY 270), got %d, %d", gameState.ProtractorX, gameState.ProtractorY)
	}
	// 7. Test JSON marshaling of GameState
	data, err := json.Marshal(&gameState)
	gameState.mu.Unlock()
	if err != nil {
		t.Fatalf("failed to marshal gameState: %v", err)
	}
	var rawMap map[string]any
	if err := json.Unmarshal(data, &rawMap); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if rawMap["protractorX"] != float64(250) || rawMap["protractorY"] != float64(270) {
		t.Fatalf("expected JSON keys protractorX=250, protractorY=270, got protractorX=%v, protractorY=%v", rawMap["protractorX"], rawMap["protractorY"])
	}

	// 8. Test broadcastExcept deep copy of GameState preserves ProtractorX and ProtractorY
	broadcast(msgStateUpdate, &gameState)
}

func TestChatterRoamingAndExplicitJoin(t *testing.T) {
	resetGameStateForTest()

	// 1. Regular chatter sends non-command message during IDLE
	processCommand("Chatter1", "hello everyone in chat!", nil, nil)
	gameState.mu.Lock()
	p1, exists := gameState.Players["Chatter1"]
	if !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Chatter1 to exist in gameState.Players during IDLE")
	}
	if p1.Joined {
		gameState.mu.Unlock()
		t.Fatalf("expected Chatter1 to have Joined=false as ambient roamer, got Joined=true")
	}
	gameState.mu.Unlock()

	// 2. Chatter explicitly types %join
	processCommand("Chatter1", "%join PogChamp", nil, nil)
	gameState.mu.Lock()
	p1 = gameState.Players["Chatter1"]
	if !p1.Joined {
		gameState.mu.Unlock()
		t.Fatalf("expected Chatter1 to have Joined=true after %%join")
	}
	if p1.Emote != "PogChamp" {
		gameState.mu.Unlock()
		t.Fatalf("expected emote PogChamp, got %s", p1.Emote)
	}
	gameState.mu.Unlock()

	// 2b. If NO players are joined, %startgame must be ignored
	resetGameStateForTest()
	processCommand("OnlyRoamer", "just roaming", nil, nil)
	broadcaster := &twitch.User{Name: "Admin", Badges: map[string]int{"broadcaster": 1}}
	processCommand("Admin", "%startgame", nil, broadcaster)
	gameState.mu.Lock()
	if gameState.Phase != phaseIdle {
		gameState.mu.Unlock()
		t.Fatalf("expected Phase to remain PhaseIdle when %%startgame is called with 0 joined players, got %s", gameState.Phase)
	}
	gameState.mu.Unlock()

	// 3. Now Chatter1 joins, Roamer2 does not join
	processCommand("Chatter1", "%join PogChamp", nil, nil)
	processCommand("Roamer2", "just watching stream :)", nil, nil)

	// Broadcaster starts game
	processCommand("Admin", "%startgame", nil, broadcaster)

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected PhaseInput after startgame, got %s", gameState.Phase)
	}
	// Roamer2 must be dropped from active match
	if _, exists := gameState.Players["Roamer2"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected unjoined Roamer2 to be dropped from active match")
	}
	// Chatter1 must remain
	if _, exists := gameState.Players["Chatter1"]; !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected joined Chatter1 to participate in match")
	}
	// Admin did NOT %join, so Admin must NOT be in the match
	if _, exists := gameState.Players["Admin"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Admin NOT to be joined or in the match without typing %%join")
	}
	gameState.mu.Unlock()

	// 4. Chatter sends a non-command message during active match: must NOT spawn
	processCommand("LateChatter", "hey guys what game is this", nil, nil)
	gameState.mu.Lock()
	if _, exists := gameState.Players["LateChatter"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected LateChatter NOT to spawn during active match without %%join")
	}
	gameState.mu.Unlock()
}

func TestLeaveCommand(t *testing.T) {
	resetGameStateForTest()

	// 1. Leave in IDLE
	processCommand("Alice", "%join Kappa", nil, nil)
	gameState.mu.Lock()
	if _, exists := gameState.Players["Alice"]; !exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to exist")
	}
	gameState.mu.Unlock()

	processCommand("Alice", "%leave", nil, nil)
	gameState.mu.Lock()
	if _, exists := gameState.Players["Alice"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Alice to be removed from players after %%leave in IDLE")
	}
	gameState.mu.Unlock()

	// 2. Leave during INPUT phase
	processCommand("Alice", "%join Kappa", nil, nil)
	processCommand("Bob", "%join LUL", nil, nil)
	broadcaster := &twitch.User{Name: "Admin", Badges: map[string]int{"broadcaster": 1}}
	processCommand("Admin", "%startgame", nil, broadcaster)

	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		t.Fatalf("expected phaseInput")
	}
	gameState.mu.Unlock()

	// Bob ragequits with %leave
	processCommand("Bob", "%leave", nil, nil)
	gameState.mu.Lock()
	if _, exists := gameState.Players["Bob"]; exists {
		gameState.mu.Unlock()
		t.Fatalf("expected Bob to be removed from match after %%leave in INPUT")
	}
	gameState.mu.Unlock()
}

func TestMatchRollover_DropInactiveHumans(t *testing.T) {
	resetGameStateForTest()

	gameState.mu.Lock()
	gameState.Phase = phaseCelebration
	// Alice commanded during match
	gameState.Players["Alice"] = &Player{
		Name:            "Alice",
		Joined:          true,
		CommandsInMatch: 2,
		X:               100,
		Y:               300,
	}
	// Bob was AFK during match (0 commands)
	gameState.Players["Bob"] = &Player{
		Name:            "Bob",
		Joined:          true,
		CommandsInMatch: 0,
		X:               200,
		Y:               300,
	}
	// Bot
	gameState.Players["TargetBot"] = &Player{
		Name:  "TargetBot",
		IsBot: true,
	}
	gameState.mu.Unlock()

	resetMatchState()

	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	if gameState.Phase != phaseIdle {
		t.Fatalf("expected phaseIdle after resetMatchState, got %s", gameState.Phase)
	}
	// Bob entered 0 commands, must be dropped
	if _, exists := gameState.Players["Bob"]; exists {
		t.Fatalf("expected inactive Bob to be dropped from next match")
	}
	// Alice entered commands, must be retained and reset CommandsInMatch
	alice, exists := gameState.Players["Alice"]
	if !exists {
		t.Fatalf("expected active Alice to be retained for next match")
	}
	if !alice.Joined {
		t.Fatalf("expected Alice to remain Joined=true for next match")
	}
	if alice.CommandsInMatch != 0 {
		t.Fatalf("expected Alice CommandsInMatch to be reset to 0, got %d", alice.CommandsInMatch)
	}
	// Bot TargetBot should be cleaned up (debug=false)
	if _, exists := gameState.Players["TargetBot"]; exists {
		t.Fatalf("expected bot TargetBot to be cleaned up from match")
	}
}

