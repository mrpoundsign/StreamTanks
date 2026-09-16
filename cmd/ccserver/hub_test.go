package main

import (
	"net/http/httptest"
	"testing"
	"golang.org/x/net/websocket"
	"net/http"
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
