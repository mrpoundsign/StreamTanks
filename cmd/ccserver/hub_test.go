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

func receiveHostServerMsg(t *testing.T, ws *websocket.Conn) *streamtankspbv1.HostServerMessage {
	t.Helper()
	var data []byte
	if err := websocket.Message.Receive(ws, &data); err != nil {
		t.Fatalf("Failed to receive host server message: %v", err)
	}
	var msg streamtankspbv1.HostServerMessage
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Failed to unmarshal HostServerMessage: %v", err)
	}
	return &msg
}

func sendHostClientMsg(t *testing.T, ws *websocket.Conn, msg *streamtankspbv1.HostClientMessage) {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal HostClientMessage: %v", err)
	}
	if err := websocket.Message.Send(ws, data); err != nil {
		t.Fatalf("Failed to send HostClientMessage: %v", err)
	}
}

func receiveViewerServerMsg(t *testing.T, ws *websocket.Conn) *streamtankspbv1.ViewerServerMessage {
	t.Helper()
	var data []byte
	if err := websocket.Message.Receive(ws, &data); err != nil {
		t.Fatalf("Failed to receive viewer server message: %v", err)
	}
	var msg streamtankspbv1.ViewerServerMessage
	if err := proto.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Failed to unmarshal ViewerServerMessage: %v", err)
	}
	return &msg
}

func sendViewerActionMsg(t *testing.T, ws *websocket.Conn, msg *streamtankspbv1.ViewerActionMessage) {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal ViewerActionMessage: %v", err)
	}
	if err := websocket.Message.Send(ws, data); err != nil {
		t.Fatalf("Failed to send ViewerActionMessage: %v", err)
	}
}

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

	resp1 := receiveHostServerMsg(t, ws1)
	if resp1.GetSuccess() == nil {
		t.Fatalf("Expected Success for host 1, got: %+v", resp1)
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

	resp2 := receiveHostServerMsg(t, ws2)
	if resp2.GetError() == nil {
		t.Fatalf("Expected Error for unauthenticated request on active channel, got: %+v", resp2)
	}
	if resp2.GetError().Message != "channel is actively hosted" {
		t.Fatalf("Expected error message 'channel is actively hosted', got: %v", resp2.GetError().Message)
	}

	// 3. Verify Host 1 receives HOST_WARNING notifying of the rejected attempt
	warningMsg := receiveHostServerMsg(t, ws1)
	if warningMsg.GetWarning() == nil {
		t.Fatalf("Expected Warning on host 1, got: %+v", warningMsg)
	}
	if warningMsg.GetWarning().Event != "unauthorized_claim_attempt" {
		t.Errorf("Expected event unauthorized_claim_attempt, got: %s", warningMsg.GetWarning().Event)
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

	vsProto := &streamtankspbv1.ViewerState{
		Phase:          "INPUT",
		TimerRemaining: 15,
		RoundId:        2,
	}
	serverMsg := &streamtankspbv1.ViewerServerMessage{
		Payload: &streamtankspbv1.ViewerServerMessage_State{
			State: vsProto,
		},
	}
	protoBytes, err := proto.Marshal(serverMsg)
	if err != nil {
		t.Fatalf("Failed to marshal state proto: %v", err)
	}

	// Broadcast payload before any viewer connects
	hub.BroadcastToViewers(channel, protoBytes)

	// Verify cached state in hub
	hub.mu.RLock()
	cached := hub.latestState[channel]
	hub.mu.RUnlock()
	if cached == nil {
		t.Fatalf("Expected state to be cached for channel %s, but got nil", channel)
	}

	// Create a mock viewer server that receives initial message from RegisterViewer
	msgChan := make(chan []byte, 1)
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var received []byte
		if err := websocket.Message.Receive(ws, &received); err == nil {
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
	case data := <-msgChan:
		var decoded streamtankspbv1.ViewerServerMessage
		if err := proto.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal delivered state: %v", err)
		}
		if decoded.GetState() == nil || decoded.GetState().Phase != "INPUT" {
			t.Errorf("Unexpected cached state delivered: %+v", decoded.GetState())
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Timed out waiting for initial cached state delivery to new viewer")
	}
}

func TestEndToEnd_HostAndViewerProtobuf(t *testing.T) {
	hub := NewHub()
	channel := "testprotochan"
	rawSecret := []byte("secret12345678901234567890123456")
	b64Secret := base64.StdEncoding.EncodeToString(rawSecret)

	mux := http.NewServeMux()
	mux.Handle("/ws/host", hub.HandleHost(&TrustFirstAuthenticator{}, nil))
	mux.Handle("/ws/viewer", HandleViewer(hub, b64Secret, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Connect Host
	hostWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/host?channel="+channel, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial host: %v", err)
	}
	defer func() { _ = hostWS.Close() }()

	hostAuthSuccess := receiveHostServerMsg(t, hostWS)
	if hostAuthSuccess.GetSuccess() == nil || hostAuthSuccess.GetSuccess().Channel != channel {
		t.Fatalf("Expected Success for host, got: %+v", hostAuthSuccess)
	}

	// 2. Connect Viewer
	viewerWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/viewer", "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial viewer: %v", err)
	}
	defer func() { _ = viewerWS.Close() }()

	// Generate viewer JWT
	claims := &ViewerClaims{
		OpaqueUserID: "U999",
		UserID:       "alice",
		ChannelID:    channel,
		Role:         "viewer",
	}
	jwtObj := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := jwtObj.SignedString(rawSecret)
	if err != nil {
		t.Fatalf("Failed to sign viewer JWT: %v", err)
	}

	// Send ViewerAuthMessage
	authMsg := &streamtankspbv1.ViewerAuthMessage{Jwt: tokenStr}
	authBytes, _ := proto.Marshal(authMsg)
	if err := websocket.Message.Send(viewerWS, authBytes); err != nil {
		t.Fatalf("Failed to send viewer auth proto: %v", err)
	}

	// Viewer receives Context message
	ctxMsg := receiveViewerServerMsg(t, viewerWS)
	if ctxMsg.GetContext() == nil || ctxMsg.GetContext().TwitchUserId != "alice" {
		t.Fatalf("Expected Context message with TwitchUserId 'alice', got: %+v", ctxMsg)
	}

	// 3. Host broadcasts state using HostClientMessage
	sendHostClientMsg(t, hostWS, &streamtankspbv1.HostClientMessage{
		Payload: &streamtankspbv1.HostClientMessage_State{
			State: &streamtankspbv1.ViewerState{
				Phase:          "INPUT",
				TimerRemaining: 15,
				RoundId:        42,
				PlayersCount:   1,
				Players:        []string{"alice"},
				CanStart:       false,
				CanJoin:        true,
			},
		},
	})

	// Viewer receives State message
	stateMsg := receiveViewerServerMsg(t, viewerWS)
	vs := stateMsg.GetState()
	if vs == nil || vs.Phase != "INPUT" || vs.TimerRemaining != 15 || vs.RoundId != 42 {
		t.Fatalf("Expected ViewerState on viewer, got: %+v", vs)
	}

	// 4. Viewer sends Protobuf Action message (Fire angle=45, power=60)
	sendViewerActionMsg(t, viewerWS, &streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Fire{
			Fire: &streamtankspbv1.FireAction{
				Angle: 45,
				Power: 60,
			},
		},
	})

	// Host receives HostCommand message
	hostCmd := receiveHostServerMsg(t, hostWS)
	if hostCmd.GetCommand() == nil {
		t.Fatalf("Expected Command on host, got: %+v", hostCmd)
	}
	cmd := hostCmd.GetCommand()
	if cmd.User != "alice" || cmd.Command != "%fire 45 60" {
		t.Errorf("Unexpected Command on host: %+v", cmd)
	}
}

func TestHostHeartbeatDoesNotBroadcastToViewers(t *testing.T) {
	hub := NewHub()
	channel := "pingchan"
	rawSecret := []byte("secret12345678901234567890123456")
	b64Secret := base64.StdEncoding.EncodeToString(rawSecret)

	mux := http.NewServeMux()
	mux.Handle("/ws/host", hub.HandleHost(&TrustFirstAuthenticator{}, nil))
	mux.Handle("/ws/viewer", HandleViewer(hub, b64Secret, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Host connects
	hostWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/host?channel="+channel, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial host: %v", err)
	}
	defer func() { _ = hostWS.Close() }()
	_ = receiveHostServerMsg(t, hostWS) // Read Success

	// 2. Host broadcasts initial game state
	sendHostClientMsg(t, hostWS, &streamtankspbv1.HostClientMessage{
		Payload: &streamtankspbv1.HostClientMessage_State{
			State: &streamtankspbv1.ViewerState{
				Phase:          "INPUT",
				TimerRemaining: 20,
				RoundId:        1,
			},
		},
	})

	// Allow broadcast to cache
	time.Sleep(50 * time.Millisecond)

	// 3. Host sends keepalive ping
	pingTimestamp := time.Now().UnixMilli()
	sendHostClientMsg(t, hostWS, &streamtankspbv1.HostClientMessage{
		Payload: &streamtankspbv1.HostClientMessage_Ping{
			Ping: &streamtankspbv1.PingMessage{
				Timestamp: pingTimestamp,
			},
		},
	})

	// 4. Host should receive Pong back directly
	hostPong := receiveHostServerMsg(t, hostWS)
	if hostPong.GetPong() == nil || hostPong.GetPong().Timestamp != pingTimestamp {
		t.Fatalf("Expected Pong with timestamp %d, got: %+v", pingTimestamp, hostPong)
	}

	// 5. Connect new Viewer and verify it receives the GAME STATE, NOT the PING!
	viewerWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/viewer", "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial viewer: %v", err)
	}
	defer func() { _ = viewerWS.Close() }()

	claims := &ViewerClaims{
		OpaqueUserID: "U111",
		UserID:       "bob",
		ChannelID:    channel,
		Role:         "viewer",
	}
	jwtObj := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := jwtObj.SignedString(rawSecret)

	authMsg := &streamtankspbv1.ViewerAuthMessage{Jwt: tokenStr}
	authBytes, _ := proto.Marshal(authMsg)
	_ = websocket.Message.Send(viewerWS, authBytes)

	// Viewer receives Context
	_ = receiveViewerServerMsg(t, viewerWS)

	// Viewer receives State (must NOT be a ping or corrupted data)
	stateMsg := receiveViewerServerMsg(t, viewerWS)
	if stateMsg.GetState() == nil || stateMsg.GetState().Phase != "INPUT" || stateMsg.GetState().TimerRemaining != 20 {
		t.Fatalf("Expected valid cached ViewerState delivered to viewer, got: %+v", stateMsg)
	}
}

func TestViewerHeartbeat(t *testing.T) {
	hub := NewHub()
	channel := "viewerpingchan"
	rawSecret := []byte("secret12345678901234567890123456")
	b64Secret := base64.StdEncoding.EncodeToString(rawSecret)

	mux := http.NewServeMux()
	mux.Handle("/ws/viewer", HandleViewer(hub, b64Secret, nil))

	server := httptest.NewServer(mux)
	defer server.Close()

	viewerWS, err := websocket.Dial("ws://"+server.Listener.Addr().String()+"/ws/viewer", "", "http://localhost/")
	if err != nil {
		t.Fatalf("Failed to dial viewer: %v", err)
	}
	defer func() { _ = viewerWS.Close() }()

	claims := &ViewerClaims{
		OpaqueUserID: "U222",
		UserID:       "charlie",
		ChannelID:    channel,
		Role:         "viewer",
	}
	jwtObj := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := jwtObj.SignedString(rawSecret)

	authMsg := &streamtankspbv1.ViewerAuthMessage{Jwt: tokenStr}
	authBytes, _ := proto.Marshal(authMsg)
	_ = websocket.Message.Send(viewerWS, authBytes)

	// Receive Context
	_ = receiveViewerServerMsg(t, viewerWS)

	// Send Viewer Ping
	pingTime := time.Now().UnixMilli()
	sendViewerActionMsg(t, viewerWS, &streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Ping{
			Ping: &streamtankspbv1.PingMessage{Timestamp: pingTime},
		},
	})

	// Receive Viewer Pong
	pongMsg := receiveViewerServerMsg(t, viewerWS)
	if pongMsg.GetPong() == nil || pongMsg.GetPong().Timestamp != pingTime {
		t.Fatalf("Expected Pong with timestamp %d, got: %+v", pingTime, pongMsg)
	}
}
