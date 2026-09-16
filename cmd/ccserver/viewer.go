package main

import (
	"encoding/base64"
	"errors"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/websocket"
)

// ViewerClaims defines the expected payload from a Twitch Extension JWT.
type ViewerClaims struct {
	OpaqueUserID string `json:"opaque_user_id"`
	UserID       string `json:"user_id"`
	ChannelID    string `json:"channel_id"`
	Role         string `json:"role"`
	jwt.RegisteredClaims
}

// ViewerAuth parses and validates the Twitch JWT using the provided base64 secret.
func ViewerAuth(tokenString string, b64Secret string) (*ViewerClaims, error) {
	secret, err := base64.StdEncoding.DecodeString(b64Secret)
	if err != nil {
		return nil, errors.New("invalid base64 secret configuration")
	}

	token, err := jwt.ParseWithClaims(tokenString, &ViewerClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Twitch uses HMAC SHA-256 for signing extension JWTs
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*ViewerClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token claims")
}

// HandleViewer is the WebSocket handler for incoming viewer connections.
func HandleViewer(hub *Hub, twitchSecret string, twitchClient *TwitchAPIClient) websocket.Server {
	return websocket.Server{
		Handshake: func(config *websocket.Config, req *http.Request) error {
			// Accept any origin
			return nil
		},
		Handler: func(ws *websocket.Conn) {
		// The viewer frontend connects and sends an initial auth payload
		// For example: {"jwt": "eyJhbG..."}
		
		var authMsg struct {
			JWT string `json:"jwt"`
		}

		if err := websocket.JSON.Receive(ws, &authMsg); err != nil {
			log.Printf("Viewer failed initial auth payload: %v", err)
			return
		}

		claims, err := ViewerAuth(authMsg.JWT, twitchSecret)
		if err != nil {
			log.Printf("Viewer JWT validation failed: %v", err)
			return
		}

		// Use UserID if they granted identity, otherwise OpaqueUserID
		viewerID := claims.UserID
		if viewerID == "" {
			viewerID = claims.OpaqueUserID
		} else if twitchClient != nil {
			// If we have a numeric UserID and the API is configured, resolve it!
			if name, err := twitchClient.GetUsername(viewerID); err == nil {
				viewerID = name
			} else {
				log.Printf("Failed to resolve username for %s: %v", viewerID, err)
			}
		}
		channelID := claims.ChannelID
		if twitchClient != nil {
			if name, err := twitchClient.GetUsername(channelID); err == nil {
				channelID = name
			} else {
				log.Printf("Failed to resolve channel username for %s: %v", channelID, err)
			}
		}

		log.Printf("Viewer %s connected for channel %s", viewerID, channelID)

		hub.RegisterViewer(channelID, ws)
		defer hub.UnregisterViewer(channelID, ws)

		// Loop to receive commands and route them to the host
		for {
			var cmdPayload interface{}
			if err := websocket.JSON.Receive(ws, &cmdPayload); err != nil {
				log.Printf("Viewer %s disconnected from channel %s", viewerID, channelID)
				break
			}

			// We wrap the raw command in an envelope that identifies the user so the Host knows who fired
			// The original game expects a WSMessage where Payload is the actual action (e.g. "FIRE 45 80")
			// but we need to inject the viewer ID.
			// 
			// Let's pass an envelope to the Host:
			// { "type": "EXTENSION_COMMAND", "payload": { "user": "...", "command": ... } }

			envelope := map[string]interface{}{
				"type": "EXTENSION_COMMAND",
				"payload": map[string]interface{}{
					"user": viewerID,
					"command": cmdPayload,
				},
			}

			_ = hub.RouteMessage(channelID, envelope)
			// If err != nil, the host is not connected or failed to receive.
			// We silently drop it for now.
		}
	},
	}
}
