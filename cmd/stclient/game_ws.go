package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
)

// GameClient manages the binary Protobuf WebSocket connection to the C&C server for a specific channel
type GameClient struct {
	BaseURL  string
	Channel  string
	JWT      string
	ws       *websocket.Conn
	mu       sync.Mutex
	closed   bool
	stopChan chan struct{}

	OnStateUpdate   func(*streamtankspbv1.ViewerState)
	OnContextUpdate func(*streamtankspbv1.ViewerContext)
	OnDisconnect    func(error)
}

func NewGameClient(baseURL, channel, jwt string) *GameClient {
	return &GameClient{
		BaseURL:  baseURL,
		Channel:  strings.ToLower(strings.TrimSpace(channel)),
		JWT:      jwt,
		stopChan: make(chan struct{}),
	}
}

// Connect dials the C&C server and performs the binary Protobuf auth handshake
func (gc *GameClient) Connect() error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	parsed, err := url.Parse(gc.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base url: %w", err)
	}

	wsScheme := "wss"
	if parsed.Scheme == "http" {
		wsScheme = "ws"
	}

	wsURL := fmt.Sprintf("%s://%s/ws/viewer?channel=%s&format=proto", wsScheme, parsed.Host, url.QueryEscape(gc.Channel))
	originURL := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	log.Printf("[GameClient] Connecting to %s", wsURL)
	ws, err := websocket.Dial(wsURL, "", originURL)
	if err != nil {
		log.Printf("[GameClient] Connection failed: %v", err)
		return fmt.Errorf("failed to connect to %s: %w", wsURL, err)
	}
	gc.ws = ws

	// Send ViewerAuthMessage (Protobuf Binary)
	authMsg := &streamtankspbv1.ViewerAuthMessage{
		Jwt: gc.JWT,
	}
	authBytes, err := proto.Marshal(authMsg)
	if err != nil {
		_ = ws.Close()
		log.Printf("[GameClient] Failed to marshal auth message: %v", err)
		return fmt.Errorf("failed to marshal auth message: %w", err)
	}

	if err := websocket.Message.Send(ws, authBytes); err != nil {
		_ = ws.Close()
		log.Printf("[GameClient] Failed to send auth payload: %v", err)
		return fmt.Errorf("failed to send auth payload: %w", err)
	}
	log.Printf("[GameClient] Connected and authenticated successfully (token len: %d)", len(gc.JWT))

	// Start reading loop in background goroutine
	go gc.readPump()

	return nil
}

// readPump continuously reads incoming binary Protobuf messages from C&C
func (gc *GameClient) readPump() {
	var disconnectErr error
	defer func() {
		gc.mu.Lock()
		if gc.ws != nil {
			_ = gc.ws.Close()
			gc.ws = nil
		}
		gc.mu.Unlock()

		log.Printf("[GameClient] Disconnected from %s: %v", gc.Channel, disconnectErr)
		if gc.OnDisconnect != nil {
			gc.OnDisconnect(disconnectErr)
		}
	}()

	for {
		select {
		case <-gc.stopChan:
			return
		default:
		}

		var data []byte
		if err := websocket.Message.Receive(gc.ws, &data); err != nil {
			disconnectErr = err
			return
		}

		// Handle text PING heartbeat from C&C relay
		if strings.Contains(string(data), `"type":"PING"`) {
			_ = websocket.Message.Send(gc.ws, []byte(`{"type":"PONG"}`))
			continue
		}

		var srvMsg streamtankspbv1.ViewerServerMessage
		if err := proto.Unmarshal(data, &srvMsg); err != nil {
			preview := string(data)
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			log.Printf("[GameClient] Failed to unmarshal incoming message (len=%d, preview=%q): %v", len(data), preview, err)
			continue
		}

		switch p := srvMsg.Payload.(type) {
		case *streamtankspbv1.ViewerServerMessage_Context:
			log.Printf("[GameClient] Received ViewerContext: username=%q channel=%q", p.Context.Username, p.Context.ChannelId)
			if gc.OnContextUpdate != nil {
				gc.OnContextUpdate(p.Context)
			}
		case *streamtankspbv1.ViewerServerMessage_State:
			log.Printf("[GameClient] Received ViewerState: phase=%s timer=%ds players=%d tanks=%d",
				p.State.Phase, p.State.TimerRemaining, len(p.State.Players), len(p.State.Tanks))
			if gc.OnStateUpdate != nil {
				gc.OnStateUpdate(p.State)
			}
		default:
			log.Printf("[GameClient] Received unknown payload type: %T", srvMsg.Payload)
		}
	}
}

// sendAction encodes and transmits a ViewerActionMessage
func (gc *GameClient) sendAction(msg *streamtankspbv1.ViewerActionMessage) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	if gc.ws == nil {
		return errors.New("not connected")
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}

	return websocket.Message.Send(gc.ws, data)
}

// SendFire aims and fires a cannon shot
func (gc *GameClient) SendFire(angle, power float32) error {
	return gc.sendAction(&streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Fire{
			Fire: &streamtankspbv1.FireAction{
				Angle: angle,
				Power: power,
			},
		},
	})
}

// SendMove moves the tank left or right
func (gc *GameClient) SendMove(dir streamtankspbv1.MoveAction_Direction) error {
	return gc.sendAction(&streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Move{
			Move: &streamtankspbv1.MoveAction{
				Direction: dir,
			},
		},
	})
}

// SendJoin requests to join the battlefield with an optional emote
func (gc *GameClient) SendJoin(emote string) error {
	return gc.sendAction(&streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Join{
			Join: &streamtankspbv1.JoinAction{
				Emote: emote,
			},
		},
	})
}

// SendShield activates the tank energy shield
func (gc *GameClient) SendShield() error {
	return gc.sendAction(&streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Shield{
			Shield: &streamtankspbv1.ShieldAction{},
		},
	})
}

// SendLeave leaves the active match
func (gc *GameClient) SendLeave() error {
	return gc.sendAction(&streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_Leave{
			Leave: &streamtankspbv1.LeaveAction{},
		},
	})
}

// SendStartMatch starts a match from the IDLE phase
func (gc *GameClient) SendStartMatch() error {
	return gc.sendAction(&streamtankspbv1.ViewerActionMessage{
		Action: &streamtankspbv1.ViewerActionMessage_StartMatch{
			StartMatch: &streamtankspbv1.StartMatchAction{},
		},
	})
}

// Close gracefully closes the WebSocket connection
func (gc *GameClient) Close() {
	gc.mu.Lock()
	if gc.closed {
		gc.mu.Unlock()
		return
	}
	gc.closed = true
	close(gc.stopChan)
	if gc.ws != nil {
		_ = gc.ws.Close()
		gc.ws = nil
	}
	gc.mu.Unlock()
}
