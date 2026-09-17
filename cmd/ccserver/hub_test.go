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

	channel := "testchannel"

	// Test 1: Register successfully
	if err := hub.RegisterHost(channel, ws); err != nil {
		t.Errorf("Expected nil error on first registration, got: %v", err)
	}

	// Test 2: Reject duplicate
	if err := hub.RegisterHost(channel, ws); err == nil {
		t.Error("Expected error on duplicate registration, got nil")
	}

	// Test 3: Unregister
	hub.UnregisterHost(channel, ws)

	// Test 4: Register again after unregister
	if err := hub.RegisterHost(channel, ws); err != nil {
		t.Errorf("Expected nil error after unregistering, got: %v", err)
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
	statePayload := map[string]interface{}{
		"type": "GAME_STATE",
		"payload": map[string]interface{}{
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
	msgChan := make(chan map[string]interface{}, 1)
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var received map[string]interface{}
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
