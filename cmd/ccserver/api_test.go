package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
)

func TestHandleGetHosts(t *testing.T) {
	hub := NewHub()

	// 1. Initial state: empty list
	req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	w := httptest.NewRecorder()
	HandleGetHosts(hub)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %s", origin)
	}

	var hosts []string
	if err := json.NewDecoder(w.Body).Decode(&hosts); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(hosts))
	}

	// 2. Add active hosts directly to hub
	hub.hosts["chan1"] = nil
	hub.hosts["chan2"] = nil

	req2 := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	w2 := httptest.NewRecorder()
	HandleGetHosts(hub)(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w2.Code)
	}

	var hosts2 []string
	if err := json.NewDecoder(w2.Body).Decode(&hosts2); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(hosts2) != 2 {
		t.Errorf("expected 2 hosts, got %d", len(hosts2))
	}

	// 3. Test OPTIONS preflight
	reqOpt := httptest.NewRequest(http.MethodOptions, "/api/hosts", nil)
	wOpt := httptest.NewRecorder()
	HandleGetHosts(hub)(wOpt, reqOpt)
	if wOpt.Code != http.StatusOK {
		t.Errorf("expected 200 for OPTIONS, got %d", wOpt.Code)
	}

	// 4. Test method not allowed
	reqPost := httptest.NewRequest(http.MethodPost, "/api/hosts", nil)
	wPost := httptest.NewRecorder()
	HandleGetHosts(hub)(wPost, reqPost)
	if wPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", wPost.Code)
	}
}

func TestValidateTwitchToken(t *testing.T) {
	// Mock Twitch OAuth validation server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "OAuth valid_token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status": 401, "message": "invalid access token"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"client_id": "test_client",
			"login": "testuser",
			"scopes": ["user:read:email"],
			"user_id": "123456",
			"expires_in": 3600
		}`))
	}))
	defer server.Close()

	// 1. Empty token
	_, err := ValidateTwitchToken("", server.URL, nil)
	if err == nil {
		t.Error("expected error for empty token, got nil")
	}

	// 2. Invalid token
	_, err = ValidateTwitchToken("bad_token", server.URL, nil)
	if err == nil {
		t.Error("expected error for bad token, got nil")
	}

	// 3. Valid token
	val, err := ValidateTwitchToken("valid_token", server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error for valid token: %v", err)
	}
	if val.UserID != "123456" || val.Login != "testuser" {
		t.Errorf("unexpected validation result: %+v", val)
	}
}

func TestHandleClientAuth(t *testing.T) {
	// Mock Twitch OAuth validation server
	mockTwitch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "OAuth valid_twitch_token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"client_id": "twitch_client_1",
				"login": "coolviewer",
				"scopes": [],
				"user_id": "987654321",
				"expires_in": 7200
			}`))
			return
		}

		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status": 401, "message": "invalid access token"}`))
	}))
	defer mockTwitch.Close()

	secret := []byte("supersecretbase64key1234567890123456")
	b64Secret := base64.StdEncoding.EncodeToString(secret)

	// Custom validator pointing to mockTwitch
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req ClientAuthRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.AccessToken == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_token is required"})
			return
		}

		val, err := ValidateTwitchToken(req.AccessToken, mockTwitch.URL, nil)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired Twitch access token"})
			return
		}

		token, err := GenerateViewerToken(val.UserID, val.Login, secret, 24*time.Hour)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ClientAuthResponse{
			Token:     token,
			UserID:    val.UserID,
			Username:  val.Login,
			ExpiresIn: 86400,
		})
	}

	// 1. Missing token
	reqMissing := httptest.NewRequest(http.MethodPost, "/api/auth/client", bytes.NewBufferString(`{}`))
	wMissing := httptest.NewRecorder()
	testHandler(wMissing, reqMissing)
	if wMissing.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing token, got %d", wMissing.Code)
	}

	// 2. Invalid Twitch token
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/auth/client", bytes.NewBufferString(`{"access_token": "expired_token"}`))
	wInvalid := httptest.NewRecorder()
	testHandler(wInvalid, reqInvalid)
	if wInvalid.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", wInvalid.Code)
	}

	// 3. Valid Twitch token -> minted StreamTanks JWT
	reqValid := httptest.NewRequest(http.MethodPost, "/api/auth/client", bytes.NewBufferString(`{"access_token": "valid_twitch_token"}`))
	wValid := httptest.NewRecorder()
	testHandler(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid token, got %d", wValid.Code)
	}

	var authResp ClientAuthResponse
	if err := json.NewDecoder(wValid.Body).Decode(&authResp); err != nil {
		t.Fatalf("failed to decode auth response: %v", err)
	}

	if authResp.UserID != "987654321" || authResp.Username != "coolviewer" {
		t.Errorf("unexpected user info in response: %+v", authResp)
	}
	if authResp.Token == "" {
		t.Fatal("expected non-empty token")
	}

	// 4. Verify minted token with ViewerAuth
	claims, err := ViewerAuth(authResp.Token, b64Secret)
	if err != nil {
		t.Fatalf("ViewerAuth failed to validate minted token: %v", err)
	}
	if claims.UserID != "987654321" {
		t.Errorf("expected user_id 987654321 in claims, got %s", claims.UserID)
	}
	if claims.ChannelID != "*" {
		t.Errorf("expected channel_id * in claims, got %s", claims.ChannelID)
	}
	if claims.Role != "viewer" {
		t.Errorf("expected role viewer in claims, got %s", claims.Role)
	}
}

func TestGenerateViewerToken(t *testing.T) {
	secret := []byte("randomsecretkeyforstreamtanksjwt123")
	b64Secret := base64.StdEncoding.EncodeToString(secret)

	tokenStr, err := GenerateViewerToken("424242", "tankcommander", secret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateViewerToken failed: %v", err)
	}

	claims, err := ViewerAuth(tokenStr, b64Secret)
	if err != nil {
		t.Fatalf("ViewerAuth failed: %v", err)
	}

	if claims.UserID != "424242" {
		t.Errorf("expected user_id 424242, got %s", claims.UserID)
	}
	if claims.OpaqueUserID != "U424242" {
		t.Errorf("expected opaque_user_id U424242, got %s", claims.OpaqueUserID)
	}
	if claims.ChannelID != "*" {
		t.Errorf("expected channel_id *, got %s", claims.ChannelID)
	}
}

func TestUniversalClient_ViewerConnect(t *testing.T) {
	hub := NewHub()
	secret := []byte("testsecretforuniversalclientviewer123")
	b64Secret := base64.StdEncoding.EncodeToString(secret)

	// Create a test server with HandleViewer
	server := httptest.NewServer(HandleViewer(hub, b64Secret, nil))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?channel=streamtanks&format=proto"

	// Connect as Universal Client
	ws, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("Failed to dial viewer WebSocket: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Generate wildcard token
	tokenStr, err := GenerateViewerToken("777888", "playerone", secret, time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate viewer token: %v", err)
	}

	// Send ViewerAuthMessage (Protobuf)
	authPb := &streamtankspbv1.ViewerAuthMessage{
		Jwt: tokenStr,
	}
	authBytes, err := proto.Marshal(authPb)
	if err != nil {
		t.Fatalf("Failed to marshal auth message: %v", err)
	}
	if err := websocket.Message.Send(ws, authBytes); err != nil {
		t.Fatalf("Failed to send viewer auth: %v", err)
	}

	// Receive ViewerServerMessage_Context
	var ctxBytes []byte
	if err := websocket.Message.Receive(ws, &ctxBytes); err != nil {
		t.Fatalf("Failed to receive context response: %v", err)
	}

	var srvMsg streamtankspbv1.ViewerServerMessage
	if err := proto.Unmarshal(ctxBytes, &srvMsg); err != nil {
		t.Fatalf("Failed to unmarshal server message: %v", err)
	}

	ctxPayload := srvMsg.GetContext()
	if ctxPayload == nil {
		t.Fatalf("Expected context message, got: %+v", &srvMsg)
	}

	if ctxPayload.ChannelId != "streamtanks" {
		t.Errorf("expected channel_id streamtanks, got %s", ctxPayload.ChannelId)
	}
	if ctxPayload.TwitchUserId != "777888" {
		t.Errorf("expected twitch_user_id 777888, got %s", ctxPayload.TwitchUserId)
	}

	// Verify hub registered the viewer under channel "streamtanks" (poll for async registration)
	var viewersCount int
	for start := time.Now(); time.Since(start) < 500*time.Millisecond; time.Sleep(5 * time.Millisecond) {
		hub.mu.RLock()
		viewersCount = len(hub.viewers["streamtanks"])
		hub.mu.RUnlock()
		if viewersCount == 1 {
			break
		}
	}

	if viewersCount != 1 {
		t.Errorf("expected 1 registered viewer for streamtanks, got %d", viewersCount)
	}
}

