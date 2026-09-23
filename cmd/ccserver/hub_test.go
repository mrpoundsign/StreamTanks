package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
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

func TestHubProtobufViewerSync(t *testing.T) {
	hub := NewHub()
	channel := "testproto"

	vsProto := &streamtankspbv1.ViewerState{
		Phase:          "INPUT",
		TimerRemaining: 15,
		RoundId:        10,
		Terrain:        []int32{100, 200, 300},
	}
	serverMsg := &streamtankspbv1.ViewerServerMessage{
		Payload: &streamtankspbv1.ViewerServerMessage_State{
			State: vsProto,
		},
	}
	protoBytes, err := proto.Marshal(serverMsg)
	if err != nil {
		t.Fatalf("failed to marshal proto: %v", err)
	}

	protoChan := make(chan []byte, 1)
	protoServer := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var raw []byte
		if err := websocket.Message.Receive(ws, &raw); err == nil {
			protoChan <- raw
		}
	}))
	defer protoServer.Close()

	viewerWs, err := websocket.Dial("ws://"+protoServer.Listener.Addr().String(), "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial proto mock server: %v", err)
	}
	defer func() { _ = viewerWs.Close() }()

	// Register proto viewer
	hub.RegisterViewer(channel, viewerWs, true)

	// Broadcast proto bytes
	hub.BroadcastToViewers(channel, protoBytes)

	select {
	case receivedBytes := <-protoChan:
		var decodedMsg streamtankspbv1.ViewerServerMessage
		if err := proto.Unmarshal(receivedBytes, &decodedMsg); err != nil {
			t.Fatalf("failed to unmarshal received proto bytes: %v", err)
		}
		if decodedMsg.GetState() == nil || decodedMsg.GetState().Phase != "INPUT" {
			t.Errorf("unexpected decoded state: %+v", decodedMsg.GetState())
		}
		if len(decodedMsg.GetState().Terrain) != 3 {
			t.Errorf("expected 3 terrain points, got %d", len(decodedMsg.GetState().Terrain))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for proto broadcast")
	}
}

func TestHubLegacyJSONServerToProtoViewer(t *testing.T) {
	hub := NewHub()
	channel := "testlegacy"

	// Legacy server JSON payload
	legacyJSONPayload := map[string]any{
		"type": "GAME_STATE",
		"payload": map[string]any{
			"phase":           "INPUT",
			"timer_remaining": float64(12),
			"round_id":        float64(5),
			"players_count":   float64(2),
			"players":         []any{"alice", "bob"},
			"can_start":       false,
			"can_join":        true,
		},
	}

	protoChan := make(chan []byte, 1)
	protoServer := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var raw []byte
		if err := websocket.Message.Receive(ws, &raw); err == nil {
			protoChan <- raw
		}
	}))
	defer protoServer.Close()

	viewerWs, err := websocket.Dial("ws://"+protoServer.Listener.Addr().String(), "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial proto mock server: %v", err)
	}
	defer func() { _ = viewerWs.Close() }()

	// Modern extension registers as proto viewer
	hub.RegisterViewer(channel, viewerWs, true)

	// Old server broadcasts JSON
	hub.BroadcastToViewers(channel, legacyJSONPayload)

	select {
	case receivedBytes := <-protoChan:
		var decodedMsg streamtankspbv1.ViewerServerMessage
		if err := proto.Unmarshal(receivedBytes, &decodedMsg); err != nil {
			t.Fatalf("failed to unmarshal converted proto bytes: %v", err)
		}
		st := decodedMsg.GetState()
		if st == nil {
			t.Fatalf("expected State payload in ViewerServerMessage, got nil")
		}
		if st.Phase != "INPUT" || st.TimerRemaining != 12 || st.RoundId != 5 || st.PlayersCount != 2 {
			t.Errorf("unexpected converted ViewerState: %+v", st)
		}
		if len(st.Players) != 2 || st.Players[0] != "alice" || st.Players[1] != "bob" {
			t.Errorf("unexpected converted players: %+v", st.Players)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for converted proto broadcast")
	}
}

func TestEndToEnd_LegacyHostAndProtoViewer(t *testing.T) {
	hub := NewHub()
	channel := "legacychan"
	rawSecret := []byte("secret12345678901234567890123456")
	b64Secret := base64.StdEncoding.EncodeToString(rawSecret)

	mux := http.NewServeMux()
	mux.Handle("/ws/host", hub.HandleHost(&TrustFirstAuthenticator{}, nil))
	mux.Handle("/ws/viewer", HandleViewer(hub, b64Secret, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Connect Legacy Host (No &format=proto query, sends/receives standard JSON)
	hostWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/host?channel="+channel, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial host: %v", err)
	}
	defer func() { _ = hostWS.Close() }()

	// Read AUTH_SUCCESS from host WS
	var hostAuthSuccess map[string]any
	if err := websocket.JSON.Receive(hostWS, &hostAuthSuccess); err != nil {
		t.Fatalf("Failed to receive host auth response: %v", err)
	}
	if hostAuthSuccess["type"] != "AUTH_SUCCESS" {
		t.Fatalf("Expected AUTH_SUCCESS for host, got: %+v", hostAuthSuccess)
	}

	// 2. Connect Modern Viewer (With &format=proto, sends/receives Protobuf binary frames)
	viewerWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/viewer?format=proto", "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial viewer: %v", err)
	}
	defer func() { _ = viewerWS.Close() }()

	// Generate valid viewer token
	claims := &ViewerClaims{
		OpaqueUserID: "U999",
		UserID:       "viewer1",
		ChannelID:    channel,
		Role:         "viewer",
	}
	jwtObj := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := jwtObj.SignedString(rawSecret)
	if err != nil {
		t.Fatalf("Failed to sign viewer JWT: %v", err)
	}

	authMsg := &streamtankspbv1.ViewerAuthMessage{Jwt: tokenStr}
	authBytes, _ := proto.Marshal(authMsg)
	if err := websocket.Message.Send(viewerWS, authBytes); err != nil {
		t.Fatalf("Failed to send viewer auth proto: %v", err)
	}

	// Viewer receives Context message
	var ctxRaw []byte
	if err := websocket.Message.Receive(viewerWS, &ctxRaw); err != nil {
		t.Fatalf("Failed to receive viewer context: %v", err)
	}
	var ctxMsg streamtankspbv1.ViewerServerMessage
	if err := proto.Unmarshal(ctxRaw, &ctxMsg); err != nil || ctxMsg.GetContext() == nil {
		t.Fatalf("Expected Context ViewerServerMessage, got error: %v or nil payload", err)
	}
	if ctxMsg.GetContext().TwitchUserId != "viewer1" || ctxMsg.GetContext().ChannelId != channel {
		t.Errorf("Unexpected context received: %+v", ctxMsg.GetContext())
	}

	// 3. Legacy Host broadcasts JSON state (using standard websocket.JSON.Send)
	legacyState := map[string]any{
		"type": "GAME_STATE",
		"payload": map[string]any{
			"phase":           "INPUT",
			"timer_remaining": float64(15),
			"round_id":        float64(42),
			"players_count":   float64(1),
			"players":         []any{"alice"},
			"can_start":       false,
			"can_join":        true,
		},
	}
	if err := websocket.JSON.Send(hostWS, legacyState); err != nil {
		t.Fatalf("Failed to send legacy state from host: %v", err)
	}

	// Viewer receives State message converted to Protobuf
	var stateRaw []byte
	if err := websocket.Message.Receive(viewerWS, &stateRaw); err != nil {
		t.Fatalf("Failed to receive converted state on viewer: %v", err)
	}
	var stateMsg streamtankspbv1.ViewerServerMessage
	if err := proto.Unmarshal(stateRaw, &stateMsg); err != nil || stateMsg.GetState() == nil {
		t.Fatalf("Expected State ViewerServerMessage on viewer, got error: %v or nil", err)
	}
	vs := stateMsg.GetState()
	if vs.Phase != "INPUT" || vs.TimerRemaining != 15 || vs.RoundId != 42 {
		t.Errorf("Unexpected converted ViewerState: %+v", vs)
	}

	// 4. Viewer sends Protobuf Action message (e.g. Fire angle=45, power=60)
	actionMsg := &streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Fire{
			Fire: &streamtankspbv1.FireAction{
				Angle: 45,
				Power: 60,
			},
		},
	}
	actionBytes, _ := proto.Marshal(actionMsg)
	if err := websocket.Message.Send(viewerWS, actionBytes); err != nil {
		t.Fatalf("Failed to send action proto from viewer: %v", err)
	}

	// Host receives canonical JSON EXTENSION_COMMAND envelope
	var hostCmd map[string]any
	if err := websocket.JSON.Receive(hostWS, &hostCmd); err != nil {
		t.Fatalf("Failed to receive action on legacy host: %v", err)
	}
	if hostCmd["type"] != "EXTENSION_COMMAND" {
		t.Fatalf("Expected type EXTENSION_COMMAND on host, got: %v", hostCmd["type"])
	}
	pMap, ok := hostCmd["payload"].(map[string]any)
	if !ok {
		t.Fatalf("Expected payload map on host message, got: %v", hostCmd["payload"])
	}
	cmdMap, ok := pMap["command"].(map[string]any)
	if !ok {
		t.Fatalf("Expected command map on host message, got: %v", pMap["command"])
	}
	if cmdMap["type"] != "CHAT_COMMAND" || cmdMap["payload"] != "%fire 45 60" {
		t.Errorf("Unexpected routed command on host: %+v", cmdMap)
	}
}

func TestEndToEnd_ProtoHostAndLegacyViewer(t *testing.T) {
	hub := NewHub()
	channel := "protochan"
	rawSecret := []byte("secret12345678901234567890123456")
	b64Secret := base64.StdEncoding.EncodeToString(rawSecret)

	mux := http.NewServeMux()
	mux.Handle("/ws/host", hub.HandleHost(&TrustFirstAuthenticator{}, nil))
	mux.Handle("/ws/viewer", HandleViewer(hub, b64Secret, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Connect Modern Host (with &format=proto, sends binary Protobuf frames)
	hostWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/host?channel="+channel+"&format=proto", "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial host: %v", err)
	}
	defer func() { _ = hostWS.Close() }()

	// Read AUTH_SUCCESS from host WS
	var hostAuthSuccess map[string]any
	if err := websocket.JSON.Receive(hostWS, &hostAuthSuccess); err != nil {
		t.Fatalf("Failed to receive host auth response: %v", err)
	}
	if hostAuthSuccess["type"] != "AUTH_SUCCESS" {
		t.Fatalf("Expected AUTH_SUCCESS for host, got: %+v", hostAuthSuccess)
	}

	// 2. Connect Legacy Viewer (without &format=proto, sends/receives standard JSON)
	viewerWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/viewer", "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial viewer: %v", err)
	}
	defer func() { _ = viewerWS.Close() }()

	// Generate valid viewer token
	claims := &ViewerClaims{
		OpaqueUserID: "U888",
		UserID:       "legacyviewer",
		ChannelID:    channel,
		Role:         "viewer",
	}
	jwtObj := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := jwtObj.SignedString(rawSecret)
	if err != nil {
		t.Fatalf("Failed to sign viewer JWT: %v", err)
	}

	// Send JSON auth
	if err := websocket.JSON.Send(viewerWS, map[string]string{"jwt": tokenStr}); err != nil {
		t.Fatalf("Failed to send legacy viewer auth: %v", err)
	}

	// Viewer receives VIEWER_INFO JSON
	var viewerInfo map[string]any
	if err := websocket.JSON.Receive(viewerWS, &viewerInfo); err != nil {
		t.Fatalf("Failed to receive viewer info: %v", err)
	}
	if viewerInfo["type"] != "VIEWER_INFO" {
		t.Fatalf("Expected VIEWER_INFO for legacy viewer, got: %+v", viewerInfo)
	}

	// 3. Modern Host broadcasts Protobuf binary state
	vsProto := &streamtankspbv1.ViewerState{
		Phase:          "INPUT",
		TimerRemaining: 20,
		RoundId:        99,
		Players:        []string{"bob"},
		Terrain:        []int32{150, 250},
	}
	serverMsg := &streamtankspbv1.ViewerServerMessage{
		Payload: &streamtankspbv1.ViewerServerMessage_State{
			State: vsProto,
		},
	}
	protoBytes, err := proto.Marshal(serverMsg)
	if err != nil {
		t.Fatalf("Failed to marshal proto state: %v", err)
	}
	if err := websocket.Message.Send(hostWS, protoBytes); err != nil {
		t.Fatalf("Failed to send binary proto state from host: %v", err)
	}

	// Legacy Viewer receives converted JSON state
	var viewerStateMsg map[string]any
	if err := websocket.JSON.Receive(viewerWS, &viewerStateMsg); err != nil {
		t.Fatalf("Failed to receive json state on legacy viewer: %v", err)
	}
	if viewerStateMsg["type"] != "GAME_STATE" {
		t.Fatalf("Expected type GAME_STATE on legacy viewer, got: %v", viewerStateMsg["type"])
	}
	pMap, ok := viewerStateMsg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("Expected payload map on viewer state, got: %v", viewerStateMsg["payload"])
	}
	if pMap["phase"] != "INPUT" || pMap["timer_remaining"] != float64(20) && pMap["timerRemaining"] != float64(20) {
		t.Errorf("Unexpected converted state on legacy viewer: %+v", pMap)
	}

	// 4. Legacy Viewer sends JSON chat command
	legacyCmd := map[string]any{
		"type":    "CHAT_COMMAND",
		"payload": "%left",
	}
	if err := websocket.JSON.Send(viewerWS, legacyCmd); err != nil {
		t.Fatalf("Failed to send legacy chat command: %v", err)
	}

	// Host receives canonical JSON EXTENSION_COMMAND envelope
	var hostCmd map[string]any
	if err := websocket.JSON.Receive(hostWS, &hostCmd); err != nil {
		t.Fatalf("Failed to receive command on host: %v", err)
	}
	if hostCmd["type"] != "EXTENSION_COMMAND" {
		t.Fatalf("Expected type EXTENSION_COMMAND on host, got: %v", hostCmd["type"])
	}
	hostPayload, ok := hostCmd["payload"].(map[string]any)
	if !ok {
		t.Fatalf("Expected payload map on host message, got: %v", hostCmd["payload"])
	}
	cmdPayload, ok := hostPayload["command"].(map[string]any)
	if !ok {
		t.Fatalf("Expected command map on host message, got: %v", hostPayload["command"])
	}
	if cmdPayload["type"] != "CHAT_COMMAND" || cmdPayload["payload"] != "%left" {
		t.Errorf("Unexpected routed command on host: %+v", cmdPayload)
	}
}
