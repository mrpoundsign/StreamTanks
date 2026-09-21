package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestHubRegistration(t *testing.T) {
	hub := NewHub()

	// Create a dummy connection
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {}))
	defer server.Close()

	// Dial the dummy server to get a valid *websocket.Conn
	ws, err := websocket.Dial("ws://"+server.Listener.Addr().String(), "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to create mock websocket: %v", err)
	}
	defer func() { _ = ws.Close() }()

	channel := "testchannel"

	// Test 1: Register successfully
	if err := hub.RegisterHost(channel, ws); err != nil {
		t.Errorf("Expected nil error on first registration, got: %v", err)
	}

	// Test 2: Replace existing host connection gracefully with new connection
	ws2, err := websocket.Dial("ws://"+server.Listener.Addr().String(), "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to create second mock websocket: %v", err)
	}
	defer func() { _ = ws2.Close() }()

	if err := hub.RegisterHost(channel, ws2); err != nil {
		t.Errorf("Expected nil error on host replacement registration, got: %v", err)
	}

	hub.mu.RLock()
	currentHost := hub.hosts[channel]
	hub.mu.RUnlock()
	if currentHost != ws2 {
		t.Errorf("Expected active host to be ws2, got: %v", currentHost)
	}

	// Test 3: Unregister old ws (should NOT remove replaced active host ws2)
	hub.UnregisterHost(channel, ws)
	hub.mu.RLock()
	currentHostAfterOldUnregister := hub.hosts[channel]
	hub.mu.RUnlock()
	if currentHostAfterOldUnregister != ws2 {
		t.Errorf("Expected active host to remain ws2 after unregistering old ws, got: %v", currentHostAfterOldUnregister)
	}

	// Test 4: Unregister active ws2
	hub.UnregisterHost(channel, ws2)
	hub.mu.RLock()
	remainingHost := hub.hosts[channel]
	hub.mu.RUnlock()
	if remainingHost != nil {
		t.Errorf("Expected nil host after unregistering active host, got: %v", remainingHost)
	}
}

func TestHandleHostRejectWhenActivelyHosted(t *testing.T) {
	hub := NewHub()
	auth := NewHMACAuthenticator([]byte("testsecretkey1234567890123456789"))
	claimMgr := NewClaimManager()

	server := httptest.NewServer(hub.HandleHost(auth, claimMgr))
	defer server.Close()

	channel := "mrpoundsign"
	token := auth.GenerateToken(channel)

	// 1. Host 1 connects with valid HMAC token
	wsURL1 := "ws://" + server.Listener.Addr().String() + "/ws/host?channel=" + channel + "&token=" + token
	ws1, err := websocket.Dial(wsURL1, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Host 1 failed to connect: %v", err)
	}
	defer func() { _ = ws1.Close() }()

	var resp1 map[string]any
	if err := websocket.JSON.Receive(ws1, &resp1); err != nil {
		t.Fatalf("Failed to receive auth response for host 1: %v", err)
	}
	if resp1["type"] != "AUTH_SUCCESS" {
		t.Fatalf("Expected AUTH_SUCCESS for host 1, got: %v", resp1)
	}

	// Allow goroutine to complete RegisterHost
	time.Sleep(50 * time.Millisecond)

	// Verify host 1 is registered in hub
	hub.mu.RLock()
	_, isHosted := hub.hosts[channel]
	hub.mu.RUnlock()
	if !isHosted {
		t.Fatal("Expected channel to be actively hosted in hub")
	}

	// 2. Host 2 connects without token for the actively hosted channel
	wsURL2 := "ws://" + server.Listener.Addr().String() + "/ws/host?channel=" + channel
	ws2, err := websocket.Dial(wsURL2, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Host 2 failed to connect: %v", err)
	}
	defer func() { _ = ws2.Close() }()

	var resp2 map[string]any
	if err := websocket.JSON.Receive(ws2, &resp2); err != nil {
		t.Fatalf("Failed to receive auth response for host 2: %v", err)
	}
	if resp2["type"] != "AUTH_ERROR" {
		t.Fatalf("Expected AUTH_ERROR for unauthenticated request on active channel, got: %v", resp2)
	}
	if resp2["payload"] != "channel is actively hosted" {
		t.Fatalf("Expected payload 'channel is actively hosted', got: %v", resp2["payload"])
	}

	// 3. Verify Host 1 receives HOST_WARNING notifying of the rejected attempt
	var warningMsg map[string]any
	if err := websocket.JSON.Receive(ws1, &warningMsg); err != nil {
		t.Fatalf("Failed to receive HOST_WARNING on host 1: %v", err)
	}
	if warningMsg["type"] != "HOST_WARNING" {
		t.Fatalf("Expected HOST_WARNING on host 1, got: %v", warningMsg)
	}
}

func TestTrustFirstAuthenticator(t *testing.T) {
	auth := &TrustFirstAuthenticator{}

	// Test 1: Valid channel
	req, _ := http.NewRequest("GET", "/ws/host?channel=mrpoundsign", nil)
	channel, err := auth.Authenticate(req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if channel != "mrpoundsign" {
		t.Errorf("Expected mrpoundsign, got %s", channel)
	}

	// Test 2: Missing channel
	req2, _ := http.NewRequest("GET", "/ws/host", nil)
	_, err2 := auth.Authenticate(req2)
	if err2 == nil {
		t.Error("Expected error for missing channel parameter")
	}
}

func TestHubStateCachingAndViewerSync(t *testing.T) {
	hub := NewHub()
	channel := "mrpoundsign"

	// Broadcast payload before any viewer connects
	statePayload := map[string]any{
		"type": "GAME_STATE",
		"payload": map[string]any{
			"phase":           "INPUT",
			"timer_remaining": float64(15),
			"round_id":        float64(2),
		},
	}
	hub.BroadcastToViewers(channel, statePayload)

	// Verify cached state in hub
	hub.mu.RLock()
	cached := hub.latestState[channel]
	hub.mu.RUnlock()
	if cached == nil {
		t.Fatalf("Expected state to be cached for channel %s, but got nil", channel)
	}

	// Create a mock server that receives initial message from RegisterViewer
	msgChan := make(chan map[string]any, 1)
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var received map[string]any
		if err := websocket.JSON.Receive(ws, &received); err == nil {
			msgChan <- received
		}
	}))
	defer server.Close()

	viewerWs, err := websocket.Dial("ws://"+server.Listener.Addr().String(), "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to create mock viewer websocket: %v", err)
	}
	defer func() { _ = viewerWs.Close() }()

	// RegisterViewer should immediately deliver cached state
	hub.RegisterViewer(channel, viewerWs)

	select {
	case msg := <-msgChan:
		if msg["type"] != "GAME_STATE" {
			t.Errorf("Expected message type 'GAME_STATE', got '%v'", msg["type"])
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Timed out waiting for initial cached state delivery to new viewer")
	}
}
