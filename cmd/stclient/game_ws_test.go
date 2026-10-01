package main

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
)

func TestGameClient_LifecycleAndActions(t *testing.T) {
	var (
		receivedAuth     streamtankspbv1.ViewerAuthMessage
		receivedActions  []*streamtankspbv1.ViewerActionMessage
		actionsMu        sync.Mutex
		authReceivedChan = make(chan struct{}, 1)
	)

	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		// Read auth message
		var authData []byte
		if err := websocket.Message.Receive(ws, &authData); err != nil {
			t.Errorf("server: failed to read auth message: %v", err)
			return
		}
		if err := proto.Unmarshal(authData, &receivedAuth); err != nil {
			t.Errorf("server: failed to unmarshal auth: %v", err)
			return
		}
		authReceivedChan <- struct{}{}

		// Send initial State and Context
		stateMsg := &streamtankspbv1.ViewerServerMessage{
			Payload: &streamtankspbv1.ViewerServerMessage_State{
				State: &streamtankspbv1.ViewerState{
					Phase:          "INPUT",
					TimerRemaining: 15,
					Terrain:        []int32{500, 505, 510},
					Tanks: []*streamtankspbv1.TankState{
						{
							Id:       "player1",
							Username: "testuser",
							X:        200,
							Y:        500,
							Health:   100,
						},
					},
					Players: []string{"testuser"},
				},
			},
		}
		stateBytes, _ := proto.Marshal(stateMsg)
		_ = websocket.Message.Send(ws, stateBytes)

		// Read actions from client
		for {
			var actionData []byte
			if err := websocket.Message.Receive(ws, &actionData); err != nil {
				return
			}
			var actionMsg streamtankspbv1.ViewerActionMessage
			if err := proto.Unmarshal(actionData, &actionMsg); err == nil {
				actionsMu.Lock()
				receivedActions = append(receivedActions, &actionMsg)
				actionsMu.Unlock()
			}
		}
	}))
	defer server.Close()

	client := NewGameClient(server.URL, "testchannel", "jwt_token_12345")

	var stateReceived *streamtankspbv1.ViewerState
	stateChan := make(chan struct{}, 1)

	client.OnStateUpdate = func(state *streamtankspbv1.ViewerState) {
		stateReceived = state
		select {
		case stateChan <- struct{}{}:
		default:
		}
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer client.Close()

	// Wait for auth handshake
	select {
	case <-authReceivedChan:
		if receivedAuth.Jwt != "jwt_token_12345" {
			t.Errorf("expected JWT 'jwt_token_12345', got %q", receivedAuth.Jwt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive auth")
	}

	// Wait for state update
	select {
	case <-stateChan:
		if stateReceived.Phase != "INPUT" {
			t.Errorf("expected phase INPUT, got %q", stateReceived.Phase)
		}
		if len(stateReceived.Tanks) != 1 || stateReceived.Tanks[0].Username != "testuser" {
			t.Errorf("unexpected tank state: %+v", stateReceived.Tanks)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client to receive state")
	}

	// Dispatch Actions
	if err := client.SendFire(45, 75); err != nil {
		t.Fatalf("SendFire failed: %v", err)
	}
	if err := client.SendMove(streamtankspbv1.MoveAction_DIRECTION_LEFT); err != nil {
		t.Fatalf("SendMove failed: %v", err)
	}
	if err := client.SendJoin("Kappa"); err != nil {
		t.Fatalf("SendJoin failed: %v", err)
	}
	if err := client.SendShield(); err != nil {
		t.Fatalf("SendShield failed: %v", err)
	}
	if err := client.SendLeave(); err != nil {
		t.Fatalf("SendLeave failed: %v", err)
	}
	if err := client.SendStartMatch(); err != nil {
		t.Fatalf("SendStartMatch failed: %v", err)
	}

	// Verify all 6 actions received on server
	time.Sleep(300 * time.Millisecond)

	actionsMu.Lock()
	defer actionsMu.Unlock()

	if len(receivedActions) != 6 {
		t.Fatalf("expected 6 actions, got %d", len(receivedActions))
	}

	fireAct := receivedActions[0].GetFire()
	if fireAct == nil || fireAct.Angle != 45 || fireAct.Power != 75 {
		t.Errorf("unexpected fire action: %+v", fireAct)
	}

	moveAct := receivedActions[1].GetMove()
	if moveAct == nil || moveAct.Direction != streamtankspbv1.MoveAction_DIRECTION_LEFT {
		t.Errorf("unexpected move action: %+v", moveAct)
	}

	joinAct := receivedActions[2].GetJoin()
	if joinAct == nil || joinAct.Emote != "Kappa" {
		t.Errorf("unexpected join action: %+v", joinAct)
	}

	if receivedActions[3].GetShield() == nil {
		t.Errorf("expected shield action")
	}

	if receivedActions[4].GetLeave() == nil {
		t.Errorf("expected leave action")
	}

	if receivedActions[5].GetStartMatch() == nil {
		t.Errorf("expected start match action")
	}
}

func TestGameClient_InvalidURL(t *testing.T) {
	client := NewGameClient("::invalid-url", "chan", "token")
	if err := client.Connect(); err == nil {
		t.Error("expected error with invalid URL")
	}
}

func TestGameClient_NotConnectedError(t *testing.T) {
	client := NewGameClient("http://localhost:1234", "chan", "token")
	if err := client.SendFire(10, 20); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got %v", err)
	}
}
