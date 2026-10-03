package main

import (
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

func sendHostMsg(ws *websocket.Conn, msg *streamtankspbv1.HostServerMessage) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return websocket.Message.Send(ws, data)
}

// Hub manages active host connections and routes viewer messages to them.
type Hub struct {
	mu          sync.RWMutex
	hosts       map[string]*websocket.Conn
	viewers     map[string]map[*websocket.Conn]bool
	latestState map[string][]byte
}

func NewHub() *Hub {
	return &Hub{
		hosts:       make(map[string]*websocket.Conn),
		viewers:     make(map[string]map[*websocket.Conn]bool),
		latestState: make(map[string][]byte),
	}
}

// GetActiveHosts returns a list of all channels currently actively hosted
func (h *Hub) GetActiveHosts() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	activeHosts := make([]string, 0, len(h.hosts))
	for channel := range h.hosts {
		activeHosts = append(activeHosts, channel)
	}
	return activeHosts
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
func (h *Hub) RegisterViewer(channel string, ws *websocket.Conn) {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	if _, exists := h.viewers[cleanChan]; !exists {
		h.viewers[cleanChan] = make(map[*websocket.Conn]bool)
	}
	h.viewers[cleanChan][ws] = true
	cached := h.latestState[cleanChan]
	h.mu.Unlock()

	if len(cached) > 0 {
		if err := websocket.Message.Send(ws, cached); err != nil {
			log.Printf("Failed to send initial cached state to viewer on %s: %v", cleanChan, err)
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

// BroadcastToViewers sends a binary Protobuf ViewerServerMessage to all viewers listening to a channel and caches the latest payload.
func (h *Hub) BroadcastToViewers(channel string, payloadBytes []byte) {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	h.latestState[cleanChan] = payloadBytes
	channelViewers, exists := h.viewers[cleanChan]
	if !exists {
		h.mu.Unlock()
		return
	}
	conns := make([]*websocket.Conn, 0, len(channelViewers))
	for ws := range channelViewers {
		conns = append(conns, ws)
	}
	h.mu.Unlock()

	for _, ws := range conns {
		if err := websocket.Message.Send(ws, payloadBytes); err != nil {
			log.Printf("Failed to broadcast proto to viewer on channel %s: %v", cleanChan, err)
		}
	}
}

// RouteMessage forwards a binary Protobuf HostServerMessage payload to the specific channel's host.
func (h *Hub) RouteMessage(channel string, payload []byte) error {
	cleanChan := strings.ToLower(channel)
	h.mu.RLock()
	ws, exists := h.hosts[cleanChan]
	h.mu.RUnlock()

	if !exists {
		return errors.New("host not connected for channel")
	}

	return websocket.Message.Send(ws, payload)
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
			msgChan := make(chan []byte)
			errChan := make(chan error, 1)
			go func() {
				for {
					var raw []byte
					if err := websocket.Message.Receive(ws, &raw); err != nil {
						errChan <- err
						return
					}
					msgChan <- raw
				}
			}()

			if err != nil {
				reqChannel := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("channel")))
				if reqChannel == "" || claimMgr == nil {
					log.Printf("Host authentication rejected: %v", err)
					_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
						Payload: &streamtankspbv1.HostServerMessage_Error{
							Error: &streamtankspbv1.HostAuthError{Message: err.Error()},
						},
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
					_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
						Payload: &streamtankspbv1.HostServerMessage_Error{
							Error: &streamtankspbv1.HostAuthError{Message: "channel is actively hosted"},
						},
					})
					_ = ws.Close()

					if existingHost != nil {
						_ = sendHostMsg(existingHost, &streamtankspbv1.HostServerMessage{
							Payload: &streamtankspbv1.HostServerMessage_Warning{
								Warning: &streamtankspbv1.HostWarning{
									Event:   "unauthorized_claim_attempt",
									Channel: reqChannel,
									Message: "An unauthenticated connection attempted to claim this channel, but was blocked because this host is actively connected.",
								},
							},
						})
					}
					return
				}

				code, approvedChan, cancel := claimMgr.CreateChallenge(reqChannel)
				defer cancel()

				log.Printf("Host challenge issued for channel %s (code: %s)", reqChannel, code)
				if sendErr := sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
					Payload: &streamtankspbv1.HostServerMessage_Challenge{
						Challenge: &streamtankspbv1.HostAuthChallenge{
							Channel:   reqChannel,
							Code:      code,
							Command:   "%claim " + code,
							ExpiresIn: 300,
						},
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
						_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
							Payload: &streamtankspbv1.HostServerMessage_Success{
								Success: &streamtankspbv1.HostAuthSuccess{
									Channel: channel,
									Token:   newToken,
								},
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
						_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
							Payload: &streamtankspbv1.HostServerMessage_Error{
								Error: &streamtankspbv1.HostAuthError{Message: "Claim timed out. Reconnect to retry."},
							},
						})
						_ = ws.Close()
						return

					case raw := <-msgChan:
						// Handle keepalive ping from host while waiting for claim
						var clientMsg streamtankspbv1.HostClientMessage
						if err := proto.Unmarshal(raw, &clientMsg); err == nil {
							if clientMsg.GetPing() != nil {
								_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
									Payload: &streamtankspbv1.HostServerMessage_Pong{
										Pong: &streamtankspbv1.PongMessage{Timestamp: clientMsg.GetPing().Timestamp},
									},
								})
							}
						}
					}
				}
			} else {
				// Immediately authenticated via valid token
				_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
					Payload: &streamtankspbv1.HostServerMessage_Success{
						Success: &streamtankspbv1.HostAuthSuccess{
							Channel: channel,
						},
					},
				})
			}

			if err := h.RegisterHost(channel, ws); err != nil {
				log.Printf("Host registration failed for %s: %v", channel, err)
				_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
					Payload: &streamtankspbv1.HostServerMessage_Error{
						Error: &streamtankspbv1.HostAuthError{Message: err.Error()},
					},
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
				case rawMsg := <-msgChan:
					var clientMsg streamtankspbv1.HostClientMessage
					if err := proto.Unmarshal(rawMsg, &clientMsg); err != nil {
						log.Printf("Failed to unmarshal HostClientMessage from %s: %v", channel, err)
						continue
					}

					switch payload := clientMsg.Payload.(type) {
					case *streamtankspbv1.HostClientMessage_Ping:
						// Handle ping at session layer; NEVER cache in latestState or broadcast to viewers
						_ = sendHostMsg(ws, &streamtankspbv1.HostServerMessage{
							Payload: &streamtankspbv1.HostServerMessage_Pong{
								Pong: &streamtankspbv1.PongMessage{Timestamp: payload.Ping.Timestamp},
							},
						})
					case *streamtankspbv1.HostClientMessage_Pong:
						// Host pong received, no-op
					case *streamtankspbv1.HostClientMessage_State:
						if payload.State != nil {
							srvMsg := &streamtankspbv1.ViewerServerMessage{
								Payload: &streamtankspbv1.ViewerServerMessage_State{
									State: payload.State,
								},
							}
							if srvBytes, err := proto.Marshal(srvMsg); err == nil {
								h.BroadcastToViewers(channel, srvBytes)
							}
						}
					}
				}
			}
		},
	}
}
