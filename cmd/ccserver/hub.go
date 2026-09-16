package main

import (
	"errors"
	"log"
	"net/http"
	"sync"

	"golang.org/x/net/websocket"
)

// Hub manages active host connections and routes viewer messages to them.
type Hub struct {
	mu      sync.RWMutex
	hosts   map[string]*websocket.Conn
	viewers map[string]map[*websocket.Conn]bool
}

func NewHub() *Hub {
	return &Hub{
		hosts:   make(map[string]*websocket.Conn),
		viewers: make(map[string]map[*websocket.Conn]bool),
	}
}

// RegisterHost attempts to claim the specified channel for a host connection.
// Returns an error if the channel is already actively claimed.
func (h *Hub) RegisterHost(channel string, ws *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.hosts[channel]; exists {
		return errors.New("channel already claimed")
	}

	h.hosts[channel] = ws
	log.Printf("Host registered for channel: %s", channel)
	return nil
}

// UnregisterHost removes the host connection for a channel.
func (h *Hub) UnregisterHost(channel string, ws *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existingWs, exists := h.hosts[channel]; exists && existingWs == ws {
		delete(h.hosts, channel)
		log.Printf("Host unregistered for channel: %s", channel)
	}
}

// RegisterViewer registers a viewer connection for a channel.
func (h *Hub) RegisterViewer(channel string, ws *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.viewers[channel]; !exists {
		h.viewers[channel] = make(map[*websocket.Conn]bool)
	}
	h.viewers[channel][ws] = true
}

// UnregisterViewer removes a viewer connection.
func (h *Hub) UnregisterViewer(channel string, ws *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.viewers[channel]; exists {
		delete(h.viewers[channel], ws)
		if len(h.viewers[channel]) == 0 {
			delete(h.viewers, channel)
		}
	}
}

// BroadcastToViewers sends a message to all viewers listening to a channel.
func (h *Hub) BroadcastToViewers(channel string, payload interface{}) {
	h.mu.RLock()
	channelViewers, exists := h.viewers[channel]
	if !exists {
		h.mu.RUnlock()
		return
	}
	// Copy connections so we don't hold the lock while sending over the network
	conns := make([]*websocket.Conn, 0, len(channelViewers))
	for ws := range channelViewers {
		conns = append(conns, ws)
	}
	h.mu.RUnlock()

	for _, ws := range conns {
		if err := websocket.JSON.Send(ws, payload); err != nil {
			log.Printf("Failed to broadcast to viewer on channel %s: %v", channel, err)
		}
	}
}

// RouteMessage forwards a JSON payload to the specific channel's host.
func (h *Hub) RouteMessage(channel string, payload interface{}) error {
	h.mu.RLock()
	ws, exists := h.hosts[channel]
	h.mu.RUnlock()

	if !exists {
		return errors.New("host not connected for channel")
	}

	// We just forward the payload exactly as received from the viewer.
	// Since we are using golang.org/x/net/websocket, we use JSON.Send.
	err := websocket.JSON.Send(ws, payload)
	if err != nil {
		log.Printf("Error sending message to host on channel %s: %v", channel, err)
		return err
	}

	return nil
}

// HandleHost is the WebSocket handler for incoming streamer (host) connections.
func (h *Hub) HandleHost(auth HostAuthenticator) websocket.Server {
	return websocket.Server{
		Handshake: func(config *websocket.Config, req *http.Request) error {
			// Accept any origin
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			req := ws.Request()
		channel, err := auth.Authenticate(req)
		if err != nil {
			log.Printf("Host authentication failed: %v", err)
			return
		}

		if err := h.RegisterHost(channel, ws); err != nil {
			log.Printf("Host registration failed for %s: %v", channel, err)
			return
		}

		defer func() {
			h.UnregisterHost(channel, ws)
			_ = ws.Close()
		}()

		// Keep the connection alive and read incoming messages.
		// The host sends GAME_STATE updates here which we broadcast to all viewers.
		for {
			var msg interface{}
			if err := websocket.JSON.Receive(ws, &msg); err != nil {
				log.Printf("Host disconnected for channel %s", channel)
				break
			}
			
			// Broadcast the message (e.g. GAME_STATE) to all viewers of this channel
			h.BroadcastToViewers(channel, msg)
		}
	},
	}
}
