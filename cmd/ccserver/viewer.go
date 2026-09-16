package main

import (
	"encoding/base64"
	"errors"
	"log"


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
func HandleViewer(hub *Hub, twitchSecret string) websocket.Handler {
	return func(ws *websocket.Conn) {
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
		}
		channelID := claims.ChannelID

		log.Printf("Viewer %s connected for channel %s", viewerID, channelID)

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

			if err := hub.RouteMessage(channelID, envelope); err != nil {
				// The host is not connected or failed to receive.
				// We could send an error back to the viewer, but silently dropping is fine for now.
			}
		}
	}
}
