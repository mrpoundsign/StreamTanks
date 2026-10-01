package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ClientAuthRequest represents the JSON request body for POST /api/auth/client
type ClientAuthRequest struct {
	AccessToken string `json:"access_token"`
}

// ClientAuthResponse represents the JSON response body for POST /api/auth/client
type ClientAuthResponse struct {
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	ExpiresIn int64  `json:"expires_in"`
}

// setCORSHeaders sets permissive CORS headers for API endpoints
func setCORSHeaders(w http.ResponseWriter, methods string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", methods)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// HandleGetHosts returns an HTTP handler for GET /api/hosts
func HandleGetHosts(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "method not allowed",
			})
			return
		}

		hosts := hub.GetActiveHosts()
		if hosts == nil {
			hosts = []string{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hosts)
	}
}

// HandleClientAuth returns an HTTP handler for POST /api/auth/client
func HandleClientAuth(secretBytes []byte, twitchClient *TwitchAPIClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "method not allowed",
			})
			return
		}

		var req ClientAuthRequest
		// Attempt to parse JSON body
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Body could be empty or invalid JSON; check Authorization header fallback
			req.AccessToken = ""
		}

		// Fallback to Authorization: Bearer <token> or OAuth <token> if body token is empty
		if strings.TrimSpace(req.AccessToken) == "" {
			authHeader := r.Header.Get("Authorization")
			if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
				req.AccessToken = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(authHeader, "OAuth "); ok {
				req.AccessToken = strings.TrimSpace(after)
			}
		}

		if strings.TrimSpace(req.AccessToken) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "access_token is required",
			})
			return
		}

		// Validate token with Twitch
		var val *TwitchTokenValidation
		var err error
		if twitchClient != nil {
			val, err = twitchClient.ValidateUserToken(req.AccessToken)
		} else {
			val, err = ValidateTwitchToken(req.AccessToken, "", nil)
		}

		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid or expired Twitch access token",
			})
			return
		}

		// Generate StreamTanks JWT valid for 24 hours
		duration := 24 * time.Hour
		token, err := GenerateViewerToken(val.UserID, val.Login, secretBytes, duration)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "failed to generate authentication token",
			})
			return
		}

		resp := ClientAuthResponse{
			Token:     token,
			UserID:    val.UserID,
			Username:  val.Login,
			ExpiresIn: int64(duration.Seconds()),
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
