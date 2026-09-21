package main

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

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
func (h *Hub) RegisterViewer(channel string, ws *websocket.Conn) {
	cleanChan := strings.ToLower(channel)
	h.mu.Lock()
	if _, exists := h.viewers[cleanChan]; !exists {
		h.viewers[cleanChan] = make(map[*websocket.Conn]bool)
	}
	h.viewers[cleanChan][ws] = true
	cached := h.latestState[cleanChan]
	h.mu.Unlock()

	if cached != nil {
		if err := websocket.JSON.Send(ws, cached); err != nil {
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
	// Copy connections so we don't hold the lock while sending over the network
	conns := make([]*websocket.Conn, 0, len(channelViewers))
	for ws := range channelViewers {
		conns = append(conns, ws)
	}
	h.mu.Unlock()

	for _, ws := range conns {
		if err := websocket.JSON.Send(ws, payload); err != nil {
			log.Printf("Failed to broadcast to viewer on channel %s: %v", cleanChan, err)
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
					var m any
					if err := websocket.JSON.Receive(ws, &m); err != nil {
						errChan <- err
						return
					}
					msgChan <- m
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
