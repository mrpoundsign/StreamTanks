package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
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

	token, err := jwt.ParseWithClaims(tokenString, &ViewerClaims{}, func(token *jwt.Token) (any, error) {
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
		if claims.ChannelID == "" {
			return nil, errors.New("token missing channel_id claim")
		}
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
			req := ws.Request()

			var authData []byte
			if err := websocket.Message.Receive(ws, &authData); err != nil {
				log.Printf("Viewer failed initial auth payload: %v", err)
				return
			}

			var authPb streamtankspbv1.ViewerAuthMessage
			if err := proto.Unmarshal(authData, &authPb); err != nil || authPb.Jwt == "" {
				log.Printf("Viewer auth token missing or invalid protobuf payload: %v", err)
				return
			}
			jwtToken := authPb.Jwt

			claims, err := ViewerAuth(jwtToken, twitchSecret)
			if err != nil {
				log.Printf("Viewer JWT validation failed: %v", err)
				return
			}

			// Resolve viewer username ONLY if UserID is present (identity granted)
			var viewerUsername string
			twitchUserID := claims.UserID
			if twitchUserID != "" {
				if twitchClient != nil {
					if name, err := twitchClient.GetUsername(twitchUserID); err == nil {
						viewerUsername = name
					} else {
						log.Printf("Failed to resolve username for Twitch UserID %s: %v", twitchUserID, err)
					}
				} else {
					viewerUsername = twitchUserID
				}
			}

			channelID := claims.ChannelID
			if channelID == "" || channelID == "*" {
				if req != nil {
					channelID = strings.TrimSpace(req.URL.Query().Get("channel"))
				}
			}
			if channelID == "" {
				log.Printf("Viewer connection rejected: no channel specified in token or query param")
				return
			}

			if isNumeric(channelID) {
				if twitchClient != nil {
					if name, err := twitchClient.GetUsername(channelID); err == nil {
						channelID = name
					} else {
						log.Printf("Failed to resolve channel username for %s: %v", channelID, err)
					}
				}
			} else {
				channelID = strings.ToLower(channelID)
			}

			log.Printf("Viewer (twitch_id: %s, opaque: %s, name: %s, role: %s) connected for channel %s", twitchUserID, claims.OpaqueUserID, viewerUsername, claims.Role, channelID)

			ctxMsg := &streamtankspbv1.ViewerServerMessage{
				Payload: &streamtankspbv1.ViewerServerMessage_Context{
					Context: &streamtankspbv1.ViewerContext{
						Username:     viewerUsername,
						ChannelId:    channelID,
						OpaqueUserId: claims.OpaqueUserID,
						TwitchUserId: twitchUserID,
						Role:         claims.Role,
					},
				},
			}
			if ctxBytes, err := proto.Marshal(ctxMsg); err == nil {
				_ = websocket.Message.Send(ws, ctxBytes)
			}

			hub.RegisterViewer(channelID, ws)
			defer hub.UnregisterViewer(channelID, ws)

			// Loop to receive binary Protobuf actions and route them to the host
			for {
				var data []byte
				if err := websocket.Message.Receive(ws, &data); err != nil {
					log.Printf("Viewer %s disconnected from channel %s", viewerUsername, channelID)
					break
				}

				var actionMsg streamtankspbv1.ViewerActionMessage
				if err := proto.Unmarshal(data, &actionMsg); err != nil || actionMsg.Action == nil {
					continue
				}

				if actionMsg.GetPing() != nil {
					pongMsg := &streamtankspbv1.ViewerServerMessage{
						Payload: &streamtankspbv1.ViewerServerMessage_Pong{
							Pong: &streamtankspbv1.PongMessage{Timestamp: actionMsg.GetPing().Timestamp},
						},
					}
					if pongBytes, err := proto.Marshal(pongMsg); err == nil {
						_ = websocket.Message.Send(ws, pongBytes)
					}
					continue
				}

				if actionMsg.GetPong() != nil {
					continue
				}

				if viewerUsername == "" {
					log.Printf("[Security] Rejected command from unlinked viewer (opaque: %s): identity share required", claims.OpaqueUserID)
					continue
				}

				var cmdStr string
				switch act := actionMsg.Action.(type) {
				case *streamtankspbv1.ViewerActionMessage_Fire:
					cmdStr = fmt.Sprintf("%%fire %g %g", act.Fire.Angle, act.Fire.Power)
				case *streamtankspbv1.ViewerActionMessage_Move:
					switch act.Move.Direction {
					case streamtankspbv1.MoveAction_DIRECTION_LEFT:
						cmdStr = "%left"
					case streamtankspbv1.MoveAction_DIRECTION_RIGHT:
						cmdStr = "%right"
					}
				case *streamtankspbv1.ViewerActionMessage_Shield:
					cmdStr = "%shield"
				case *streamtankspbv1.ViewerActionMessage_Join:
					if act.Join.Emote != "" {
						cmdStr = "%join " + act.Join.Emote
					} else {
						cmdStr = "%join"
					}
				case *streamtankspbv1.ViewerActionMessage_Leave:
					cmdStr = "%leave"
				case *streamtankspbv1.ViewerActionMessage_StartMatch:
					cmdStr = "%startgame"
				}

				if cmdStr != "" {
					hostCmdMsg := &streamtankspbv1.HostServerMessage{
						Payload: &streamtankspbv1.HostServerMessage_Command{
							Command: &streamtankspbv1.HostCommand{
								User:         viewerUsername,
								TwitchUserId: twitchUserID,
								Command:      cmdStr,
								Action:       &actionMsg,
							},
						},
					}
					if hostCmdBytes, err := proto.Marshal(hostCmdMsg); err == nil {
						_ = hub.RouteMessage(channelID, hostCmdBytes)
					}
				}
			}
		},
	}
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
