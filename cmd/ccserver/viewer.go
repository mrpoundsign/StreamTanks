package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

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
			isProto := req != nil && req.URL.Query().Get("format") == "proto"

			var authFrame wsFrame
			if err := frameCodec.Receive(ws, &authFrame); err != nil {
				log.Printf("Viewer failed initial auth payload: %v", err)
				return
			}

			var jwtToken string
			if authFrame.payloadType == websocket.BinaryFrame || isProto {
				var authPb streamtankspbv1.ViewerAuthMessage
				if err := proto.Unmarshal(authFrame.data, &authPb); err == nil && authPb.Jwt != "" {
					jwtToken = authPb.Jwt
				}
			}
			if jwtToken == "" {
				var authMsg struct {
					JWT string `json:"jwt"`
				}
				if err := json.Unmarshal(authFrame.data, &authMsg); err == nil {
					jwtToken = authMsg.JWT
				}
			}

			if jwtToken == "" {
				log.Printf("Viewer auth token missing or invalid payload")
				return
			}

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
			if twitchClient != nil {
				if name, err := twitchClient.GetUsername(channelID); err == nil {
					channelID = name
				} else {
					log.Printf("Failed to resolve channel username for %s: %v", channelID, err)
				}
			}

			log.Printf("Viewer (twitch_id: %s, opaque: %s, name: %s, proto: %v) connected for channel %s", twitchUserID, claims.OpaqueUserID, viewerUsername, isProto, channelID)

			if isProto {
				ctxMsg := &streamtankspbv1.ViewerServerMessage{
					Payload: &streamtankspbv1.ViewerServerMessage_Context{
						Context: &streamtankspbv1.ViewerContext{
							Username:     viewerUsername,
							ChannelId:    channelID,
							OpaqueUserId: claims.OpaqueUserID,
							TwitchUserId: twitchUserID,
						},
					},
				}
				if ctxBytes, err := proto.Marshal(ctxMsg); err == nil {
					_ = websocket.Message.Send(ws, ctxBytes)
				}
			} else {
				// Send viewer identity and channel context to viewer client
				_ = websocket.JSON.Send(ws, map[string]any{
					"type": "VIEWER_INFO",
					"payload": map[string]any{
						"user":      viewerUsername,
						"twitch_id": twitchUserID,
						"channel":   channelID,
					},
				})
			}

			hub.RegisterViewer(channelID, ws, isProto)
			defer hub.UnregisterViewer(channelID, ws)

			// Loop to receive commands and route them to the host
			for {
				var frame wsFrame
				if err := frameCodec.Receive(ws, &frame); err != nil {
					log.Printf("Viewer %s disconnected from channel %s", viewerUsername, channelID)
					break
				}

				if viewerUsername == "" {
					log.Printf("[Security] Rejected command from unlinked viewer (opaque: %s): identity share required", claims.OpaqueUserID)
					if !isProto {
						_ = websocket.JSON.Send(ws, map[string]any{
							"type":    "AUTH_REQUIRED",
							"payload": "Twitch identity link required to participate in StreamTanks",
						})
					}
					continue
				}

				var cmdStr string
				var rawCmdPayload any

				if frame.payloadType == websocket.BinaryFrame || isProto {
					var actionMsg streamtankspbv1.ViewerActionMessage
					if err := proto.Unmarshal(frame.data, &actionMsg); err == nil && actionMsg.Action != nil {
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
					}
				}
				if cmdStr == "" {
					_ = json.Unmarshal(frame.data, &rawCmdPayload)
				}

				var envelope map[string]any
				if cmdStr != "" {
					envelope = map[string]any{
						"type": "EXTENSION_COMMAND",
						"payload": map[string]any{
							"user":      viewerUsername,
							"twitch_id": twitchUserID,
							"command": map[string]any{
								"type":    "CHAT_COMMAND",
								"payload": cmdStr,
							},
						},
					}
				} else if rawCmdPayload != nil {
					envelope = map[string]any{
						"type": "EXTENSION_COMMAND",
						"payload": map[string]any{
							"user":      viewerUsername,
							"twitch_id": twitchUserID,
							"command":   rawCmdPayload,
						},
					}
				}

				if envelope != nil {
					_ = hub.RouteMessage(channelID, envelope)
				}
			}
		},
	}
}
