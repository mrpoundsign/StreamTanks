package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
)

type wsFrame struct {
	payloadType byte
	data        []byte
}

var frameCodec = websocket.Codec{
	Marshal: func(v any) ([]byte, byte, error) {
		switch data := v.(type) {
		case string:
			return []byte(data), websocket.TextFrame, nil
		case []byte:
			return data, websocket.BinaryFrame, nil
		default:
			return nil, websocket.UnknownFrame, errors.New("unsupported payload type")
		}
	},
	Unmarshal: func(data []byte, payloadType byte, v any) error {
		if f, ok := v.(*wsFrame); ok {
			f.payloadType = payloadType
			f.data = data
			return nil
		}
		return errors.New("unsupported target type for frameCodec")
	},
}

// Hub manages active host connections and routes viewer messages to them.
type Hub struct {
	mu          sync.RWMutex
	hosts       map[string]*websocket.Conn
	viewers     map[string]map[*websocket.Conn]bool
	latestState map[string]any
}

func NewHub() *Hub {
	return &Hub{
		hosts:       make(map[string]*websocket.Conn),
		viewers:     make(map[string]map[*websocket.Conn]bool),
		latestState: make(map[string]any),
	}
}

// RegisterHost registers the host connection for a channel.
// If an existing host connection exists (e.g. from an abrupt disconnect or reconnect),
// it is gracefully closed and replaced by the newly authenticated host connection.
func (h *Hub) RegisterHost(channel string, ws *websocket.Conn) error {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	defer h.mu.Unlock()

	if existingWs, exists := h.hosts[cleanChan]; exists {
		if existingWs != ws {
			log.Printf("[Host Auth] Authenticated host reconnected for channel '%s'; replacing existing session", cleanChan)
			_ = existingWs.Close()
		}
	}

	h.hosts[cleanChan] = ws
	log.Printf("[Host Auth] Host registered successfully for channel: %s", cleanChan)
	return nil
}

// UnregisterHost removes the host connection for a channel.
func (h *Hub) UnregisterHost(channel string, ws *websocket.Conn) {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	defer h.mu.Unlock()

	if existingWs, exists := h.hosts[cleanChan]; exists && existingWs == ws {
		delete(h.hosts, cleanChan)
		delete(h.latestState, cleanChan)
		log.Printf("Host unregistered for channel: %s", cleanChan)
	}
}

// RegisterViewer registers a viewer connection for a channel.
// If cached state exists for the channel, it is immediately sent to the new viewer.
// jsonStateToProtoBytes converts a legacy JSON state update into a Protobuf ViewerServerMessage.
func jsonStateToProtoBytes(payload any) []byte {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	msgType, _ := m["type"].(string)
	if msgType != "GAME_STATE" {
		return nil
	}
	pMap, ok := m["payload"].(map[string]any)
	if !ok {
		return nil
	}

	getStringSlice := func(key string) []string {
		arr, ok := pMap[key].([]any)
		if !ok {
			if strArr, ok := pMap[key].([]string); ok {
				return strArr
			}
			return nil
		}
		res := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				res = append(res, s)
			}
		}
		return res
	}

	getInt := func(key string) int32 {
		if v, ok := pMap[key].(float64); ok {
			return int32(v)
		}
		if v, ok := pMap[key].(int); ok {
			return int32(v)
		}
		return 0
	}

	getBool := func(key string) bool {
		if v, ok := pMap[key].(bool); ok {
			return v
		}
		return false
	}

	getString := func(key string) string {
		if v, ok := pMap[key].(string); ok {
			return v
		}
		return ""
	}

	vsProto := &streamtankspbv1.ViewerState{
		Phase:             getString("phase"),
		TimerRemaining:    getInt("timer_remaining"),
		RoundId:           int64(getInt("round_id")),
		Winner:            getString("winner"),
		PlayersCount:      getInt("players_count"),
		Players:           getStringSlice("players"),
		ProtractorX:       getInt("protractor_x"),
		ProtractorY:       getInt("protractor_y"),
		CanStart:          getBool("can_start"),
		CanJoin:           getBool("can_join"),
		JoinedPlayers:     getStringSlice("joined_players"),
		LeavingPlayers:    getStringSlice("leaving_players"),
		ShieldUsedPlayers: getStringSlice("shield_used_players"),
		ShieldedPlayers:   getStringSlice("shielded_players"),
	}

	serverMsg := &streamtankspbv1.ViewerServerMessage{
		Payload: &streamtankspbv1.ViewerServerMessage_State{
			State: vsProto,
		},
	}
	bytes, err := proto.Marshal(serverMsg)
	if err != nil {
		return nil
	}
	return bytes
}

// RegisterViewer registers a viewer connection for a channel.
// If cached state exists for the channel, it is immediately sent to the new viewer.
func (h *Hub) RegisterViewer(channel string, ws *websocket.Conn, isProto ...bool) {
	protoClient := len(isProto) > 0 && isProto[0]
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	if _, exists := h.viewers[cleanChan]; !exists {
		h.viewers[cleanChan] = make(map[*websocket.Conn]bool)
	}
	h.viewers[cleanChan][ws] = protoClient
	cached := h.latestState[cleanChan]
	h.mu.Unlock()

	if cached != nil {
		if protoClient {
			if cachedBytes, ok := cached.([]byte); ok {
				_ = websocket.Message.Send(ws, cachedBytes)
			} else {
				// Convert legacy JSON state to Protobuf bytes for modern viewer
				if pBytes := jsonStateToProtoBytes(cached); pBytes != nil {
					_ = websocket.Message.Send(ws, pBytes)
				} else {
					_ = websocket.JSON.Send(ws, cached)
				}
			}
		} else {
			if cachedBytes, ok := cached.([]byte); ok {
				var sMsg streamtankspbv1.ViewerServerMessage
				if err := proto.Unmarshal(cachedBytes, &sMsg); err == nil && sMsg.GetState() != nil {
					_ = websocket.JSON.Send(ws, map[string]any{
						"type":    "GAME_STATE",
						"payload": sMsg.GetState(),
					})
				}
			} else {
				if err := websocket.JSON.Send(ws, cached); err != nil {
					log.Printf("Failed to send initial cached state to viewer on %s: %v", cleanChan, err)
				}
			}
		}
	}
}

// UnregisterViewer removes a viewer connection.
func (h *Hub) UnregisterViewer(channel string, ws *websocket.Conn) {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.viewers[cleanChan]; exists {
		delete(h.viewers[cleanChan], ws)
		if len(h.viewers[cleanChan]) == 0 {
			delete(h.viewers, cleanChan)
		}
	}
}

// BroadcastToViewers sends a message to all viewers listening to a channel and caches the latest payload.
func (h *Hub) BroadcastToViewers(channel string, payload any) {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	h.latestState[cleanChan] = payload
	channelViewers, exists := h.viewers[cleanChan]
	if !exists {
		h.mu.Unlock()
		return
	}
	type target struct {
		ws      *websocket.Conn
		isProto bool
	}
	targets := make([]target, 0, len(channelViewers))
	for ws, isProto := range channelViewers {
		targets = append(targets, target{ws: ws, isProto: isProto})
	}
	h.mu.Unlock()

	payloadBytes, isPayloadBytes := payload.([]byte)
	var cachedJSON any
	var cachedProtoBytes []byte

	for _, t := range targets {
		if t.isProto {
			if isPayloadBytes {
				if err := websocket.Message.Send(t.ws, payloadBytes); err != nil {
					log.Printf("Failed to broadcast proto to viewer on channel %s: %v", cleanChan, err)
				}
			} else {
				// Convert legacy JSON state to Protobuf bytes for proto viewer
				if cachedProtoBytes == nil {
					cachedProtoBytes = jsonStateToProtoBytes(payload)
				}
				if cachedProtoBytes != nil {
					if err := websocket.Message.Send(t.ws, cachedProtoBytes); err != nil {
						log.Printf("Failed to broadcast converted proto to viewer on channel %s: %v", cleanChan, err)
					}
				} else {
					if err := websocket.JSON.Send(t.ws, payload); err != nil {
						log.Printf("Failed to broadcast json fallback to proto viewer on channel %s: %v", cleanChan, err)
					}
				}
			}
		} else {
			if isPayloadBytes {
				if cachedJSON == nil {
					var sMsg streamtankspbv1.ViewerServerMessage
					if err := proto.Unmarshal(payloadBytes, &sMsg); err == nil && sMsg.GetState() != nil {
						cachedJSON = map[string]any{
							"type":    "GAME_STATE",
							"payload": sMsg.GetState(),
						}
					}
				}
				if cachedJSON != nil {
					if err := websocket.JSON.Send(t.ws, cachedJSON); err != nil {
						log.Printf("Failed to broadcast json to viewer on channel %s: %v", cleanChan, err)
					}
				}
			} else {
				if err := websocket.JSON.Send(t.ws, payload); err != nil {
					log.Printf("Failed to broadcast to viewer on channel %s: %v", cleanChan, err)
				}
			}
		}
	}
}

// RouteMessage forwards a JSON payload to the specific channel's host.
func (h *Hub) RouteMessage(channel string, payload any) error {
	cleanChan := strings.ToLower(channel)
	h.mu.RLock()
	ws, exists := h.hosts[cleanChan]
	h.mu.RUnlock()

	if !exists {
		return errors.New("host not connected for channel")
	}

	// We just forward the payload exactly as received from the viewer.
	// Since we are using golang.org/x/net/websocket, we use JSON.Send.
	err := websocket.JSON.Send(ws, payload)
	if err != nil {
		log.Printf("Error sending message to host on channel %s: %v", cleanChan, err)
		return err
	}

	return nil
}

// HandleHost is the WebSocket handler for incoming streamer (host) connections.
func (h *Hub) HandleHost(auth HostAuthenticator, claimMgr *ClaimManager) websocket.Server {
	return websocket.Server{
		Handshake: func(config *websocket.Config, req *http.Request) error {
			// Accept any origin
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			req := ws.Request()
			channel, err := auth.Authenticate(req)

			// Single dedicated reader goroutine for this WebSocket connection
			msgChan := make(chan any)
			errChan := make(chan error, 1)
			go func() {
				for {
					var frame wsFrame
					if err := frameCodec.Receive(ws, &frame); err != nil {
						errChan <- err
						return
					}
					if frame.payloadType == websocket.BinaryFrame {
						msgChan <- frame.data
					} else {
						var m any
						if err := json.Unmarshal(frame.data, &m); err == nil {
							msgChan <- m
						} else {
							msgChan <- frame.data
						}
					}
				}
			}()

			if err != nil {
				reqChannel := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("channel")))
				if reqChannel == "" || claimMgr == nil {
					log.Printf("Host authentication rejected: %v", err)
					_ = websocket.JSON.Send(ws, map[string]any{
						"type":    "AUTH_ERROR",
						"payload": err.Error(),
					})
					_ = ws.Close()
					return
				}

				h.mu.RLock()
				existingHost, isActivelyHosted := h.hosts[reqChannel]
				h.mu.RUnlock()

				if isActivelyHosted {
					remoteAddr := req.RemoteAddr
					log.Printf("[Host Auth] Challenge rejected for channel '%s' (remote: %s): channel is already actively hosted", reqChannel, remoteAddr)
					_ = websocket.JSON.Send(ws, map[string]any{
						"type":    "AUTH_ERROR",
						"payload": "channel is actively hosted",
					})
					_ = ws.Close()

					if existingHost != nil {
						_ = websocket.JSON.Send(existingHost, map[string]any{
							"type": "HOST_WARNING",
							"payload": map[string]any{
								"event":   "unauthorized_claim_attempt",
								"channel": reqChannel,
								"message": "An unauthenticated connection attempted to claim this channel, but was blocked because this host is actively connected.",
							},
						})
					}
					return
				}

				code, approvedChan, cancel := claimMgr.CreateChallenge(reqChannel)
				defer cancel()

				log.Printf("Host challenge issued for channel %s (code: %s)", reqChannel, code)
				if sendErr := websocket.JSON.Send(ws, map[string]any{
					"type": "AUTH_CHALLENGE",
					"payload": map[string]any{
						"channel":    reqChannel,
						"code":       code,
						"command":    "%claim " + code,
						"expires_in": 300,
					},
				}); sendErr != nil {
					log.Printf("Failed to send challenge to host: %v", sendErr)
					_ = ws.Close()
					return
				}

				authenticated := false
				for !authenticated {
					select {
					case <-approvedChan:
						channel = reqChannel
						auth.InvalidateOlderTokens(channel, time.Now().Unix())
						newToken := auth.GenerateToken(channel)
						_ = websocket.JSON.Send(ws, map[string]any{
							"type": "AUTH_SUCCESS",
							"payload": map[string]any{
								"channel": channel,
								"token":   newToken,
							},
						})
						log.Printf("Host %s authenticated via in-chat claim successfully!", channel)
						authenticated = true

					case readErr := <-errChan:
						log.Printf("Host disconnected during claim challenge for %s: %v", reqChannel, readErr)
						_ = ws.Close()
						return

					case <-time.After(5 * time.Minute):
						log.Printf("Host challenge timed out for channel %s", reqChannel)
						_ = websocket.JSON.Send(ws, map[string]any{
							"type":    "AUTH_ERROR",
							"payload": "Claim timed out. Reconnect to retry.",
						})
						_ = ws.Close()
						return

					case msg := <-msgChan:
						// Handle PING or keep-alive from host while waiting for claim
						_ = msg
					}
				}
			} else {
				// Immediately authenticated via valid token
				_ = websocket.JSON.Send(ws, map[string]any{
					"type": "AUTH_SUCCESS",
					"payload": map[string]any{
						"channel": channel,
					},
				})
			}

			if err := h.RegisterHost(channel, ws); err != nil {
				log.Printf("Host registration failed for %s: %v", channel, err)
				_ = websocket.JSON.Send(ws, map[string]any{
					"type":    "AUTH_ERROR",
					"payload": err.Error(),
				})
				_ = ws.Close()
				return
			}

			defer func() {
				h.UnregisterHost(channel, ws)
				_ = ws.Close()
			}()

			log.Printf("Host active for channel: %s", channel)

			for {
				select {
				case readErr := <-errChan:
					log.Printf("Host disconnected for channel %s: %v", channel, readErr)
					return
				case msg := <-msgChan:
					h.BroadcastToViewers(channel, msg)
				}
			}
		},
	}
}
