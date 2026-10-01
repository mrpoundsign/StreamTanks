package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DeviceSession tracks a pending or approved device authorization session
type DeviceSession struct {
	DeviceCode string    `json:"device_code"`
	UserCode   string    `json:"user_code"`
	Approved   bool      `json:"approved"`
	Token      string    `json:"token,omitempty"`
	UserID     string    `json:"user_id,omitempty"`
	Username   string    `json:"username,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// DeviceAuthManager manages in-memory device authorization pairing sessions
type DeviceAuthManager struct {
	mu       sync.RWMutex
	byDevice map[string]*DeviceSession
	byUser   map[string]*DeviceSession
}

func NewDeviceAuthManager() *DeviceAuthManager {
	return &DeviceAuthManager{
		byDevice: make(map[string]*DeviceSession),
		byUser:   make(map[string]*DeviceSession),
	}
}

// generateRandomHex generates a cryptographically random hex string
func generateRandomHex(byteCount int) string {
	b := make([]byte, byteCount)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// generateUserCode generates a friendly 6-char code: ST-XXXX (e.g. ST-7K2M)
func generateUserCode() string {
	const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // exclude ambiguous chars 0, 1, I, O
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	var sb strings.Builder
	sb.WriteString("ST-")
	for _, val := range b {
		sb.WriteByte(charset[int(val)%len(charset)])
	}
	return sb.String()
}

// cleanupExpired removes expired sessions (caller must hold lock)
func (m *DeviceAuthManager) cleanupExpiredLocked(now time.Time) {
	for dev, sess := range m.byDevice {
		if now.After(sess.ExpiresAt) {
			delete(m.byDevice, dev)
			delete(m.byUser, sess.UserCode)
		}
	}
}

// CreateSession generates a new device pairing session
func (m *DeviceAuthManager) CreateSession(ttl time.Duration) (*DeviceSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.cleanupExpiredLocked(now)

	deviceCode := "st_dev_" + generateRandomHex(16)
	userCode := generateUserCode()

	sess := &DeviceSession{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ExpiresAt:  now.Add(ttl),
	}

	m.byDevice[deviceCode] = sess
	m.byUser[strings.ToUpper(userCode)] = sess

	return sess, nil
}

// GetSessionByDevice retrieves a session by its device code
func (m *DeviceAuthManager) GetSessionByDevice(deviceCode string) (*DeviceSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.byDevice[deviceCode]
	if !ok {
		return nil, errors.New("device session not found")
	}

	if time.Now().After(sess.ExpiresAt) {
		return nil, errors.New("device session expired")
	}

	return sess, nil
}

// ApproveSession marks a device session as approved with the authenticated user credentials
func (m *DeviceAuthManager) ApproveSession(userCode, token, userID, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanCode := strings.ToUpper(strings.TrimSpace(userCode))
	sess, ok := m.byUser[cleanCode]
	if !ok {
		return errors.New("invalid or expired user code")
	}

	if time.Now().After(sess.ExpiresAt) {
		delete(m.byDevice, sess.DeviceCode)
		delete(m.byUser, cleanCode)
		return errors.New("user code expired")
	}

	sess.Approved = true
	sess.Token = token
	sess.UserID = userID
	sess.Username = username

	return nil
}

// HandleDeviceCode returns an HTTP handler for POST /api/auth/device/code
func HandleDeviceCode(mgr *DeviceAuthManager, externalBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		ttl := 10 * time.Minute
		sess, err := mgr.CreateSession(ttl)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to create session"})
			return
		}

		baseURL := strings.TrimRight(externalBaseURL, "/")
		if baseURL == "" {
			scheme := "https"
			if r.TLS == nil && (strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1")) {
				scheme = "http"
			}
			baseURL = scheme + "://" + r.Host
		}
		verificationURI := baseURL + "/link"
		verificationURIComplete := fmt.Sprintf("%s/link?code=%s", baseURL, sess.UserCode)

		resp := map[string]any{
			"device_code":               sess.DeviceCode,
			"user_code":                 sess.UserCode,
			"verification_uri":          verificationURI,
			"verification_uri_complete": verificationURIComplete,
			"expires_in":                int(ttl.Seconds()),
			"interval":                  2,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// HandleDevicePoll returns an HTTP handler for GET /api/auth/device/poll
func HandleDevicePoll(mgr *DeviceAuthManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		deviceCode := strings.TrimSpace(r.URL.Query().Get("device_code"))
		if deviceCode == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"error":  "device_code parameter is required",
			})
			return
		}

		sess, err := mgr.GetSessionByDevice(deviceCode)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "expired_token",
				"error":  err.Error(),
			})
			return
		}

		if !sess.Approved {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "authorization_pending",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "approved",
			"token":      sess.Token,
			"user_id":    sess.UserID,
			"username":   sess.Username,
			"expires_in": 86400,
		})
	}
}

// DeviceApproveRequest represents the body for POST /api/auth/device/approve
type DeviceApproveRequest struct {
	UserCode    string `json:"user_code"`
	AccessToken string `json:"access_token"`
}

// HandleDeviceApprove returns an HTTP handler for POST /api/auth/device/approve
func HandleDeviceApprove(mgr *DeviceAuthManager, secretBytes []byte, twitchClient *TwitchAPIClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var req DeviceApproveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}

		if strings.TrimSpace(req.UserCode) == "" || strings.TrimSpace(req.AccessToken) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "user_code and access_token are required"})
			return
		}

		// Validate Twitch token
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
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid Twitch access token"})
			return
		}

		// Generate StreamTanks JWT
		token, err := GenerateViewerToken(val.UserID, val.Login, secretBytes, 24*time.Hour)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to sign session token"})
			return
		}

		if err := mgr.ApproveSession(req.UserCode, token, val.UserID, val.Login); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "success",
			"username": val.Login,
		})
	}
}

// HandleLinkPage serves the browser linking UI on /link
func HandleLinkPage(clientID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
		
		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>StreamTanks — Link Device</title>
  <style>
    body {
      background-color: #0b0c10;
      color: #c5c6c7;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 100vh;
      margin: 0;
    }
    .card {
      background: #1f2833;
      border: 1px solid #45a29e;
      border-radius: 12px;
      padding: 32px;
      max-width: 420px;
      width: 90%%;
      text-align: center;
      box-shadow: 0 8px 32px rgba(0, 255, 204, 0.15);
    }
    h1 {
      color: #66fcf1;
      font-size: 24px;
      margin-top: 0;
      text-transform: uppercase;
      letter-spacing: 2px;
    }
    .code-badge {
      display: inline-block;
      background: #0b0c10;
      border: 2px dashed #66fcf1;
      border-radius: 8px;
      padding: 12px 24px;
      font-size: 28px;
      font-weight: bold;
      color: #66fcf1;
      letter-spacing: 4px;
      margin: 20px 0;
    }
    .btn {
      background: #9146ff;
      color: white;
      border: none;
      border-radius: 6px;
      padding: 14px 28px;
      font-size: 16px;
      font-weight: bold;
      cursor: pointer;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 10px;
      width: 100%%;
      text-decoration: none;
      box-sizing: border-box;
      transition: background 0.2s;
    }
    .btn:hover {
      background: #772ce8;
    }
    .status {
      margin-top: 18px;
      font-size: 14px;
      color: #c5c6c7;
    }
    .success {
      color: #00ffcc;
      font-weight: bold;
      font-size: 18px;
    }
    .error {
      color: #ff4757;
    }
  </style>
</head>
<body>
  <div class="card" id="card">
    <h1>StreamTanks</h1>
    <p>Pairing your client to your Twitch account</p>
    <div class="code-badge" id="codeDisplay">%s</div>
    
    <div id="actionArea">
      <button class="btn" id="loginBtn" onclick="startTwitchAuth()">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M2.149 0l-1.612 4.119v16.8h5.331v3.081h3.185l3.106-3.081h4.405l6.886-6.886v-14.033h-21.3zm18.368 13.081l-3.563 3.563h-4.832l-2.73 2.73v-2.73h-3.993v-13.882h15.118v10.319zm-3.805-6.524v6.048h-2.149v-6.048h2.149zm-5.74 0v6.048h-2.149v-6.048h2.149z"/></svg>
        Authorize with Twitch
      </button>
    </div>
    <div class="status" id="statusText"></div>
  </div>

  <script>
    const clientId = %q;
    const userCode = %q;

    // Check if redirect contains access_token in URL hash
    if (window.location.hash) {
      const params = new URLSearchParams(window.location.hash.substring(1));
      const accessToken = params.get('access_token');
      const stateCode = params.get('state') || userCode;

      if (accessToken && stateCode) {
        document.getElementById('actionArea').innerHTML = '<p>Verifying credentials with StreamTanks...</p>';
        fetch('/api/auth/device/approve', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ user_code: stateCode, access_token: accessToken })
        })
        .then(res => res.json())
        .then(data => {
          if (data.status === 'success') {
            document.getElementById('actionArea').innerHTML = '<p class="success">Device Paired Successfully!</p><p>You are logged in as <strong>' + data.username + '</strong>.<br>You may now close this tab and return to StreamTanks.</p>';
          } else {
            document.getElementById('actionArea').innerHTML = '<p class="error">Authorization failed: ' + (data.error || 'unknown error') + '</p>';
          }
        })
        .catch(err => {
          document.getElementById('actionArea').innerHTML = '<p class="error">Failed to complete pairing: ' + err + '</p>';
        });
      }
    }

    function startTwitchAuth() {
      if (!clientId) {
        alert('Server TWITCH_CLIENT_ID is not configured. Contact broadcaster.');
        return;
      }
      const redirectUri = window.location.origin + '/link';
      const authUrl = 'https://id.twitch.tv/oauth2/authorize?client_id=' + encodeURIComponent(clientId) +
        '&redirect_uri=' + encodeURIComponent(redirectUri) +
        '&response_type=token' +
        '&state=' + encodeURIComponent(userCode);
      window.location.href = authUrl;
    }
  </script>
</body>
</html>`, code, clientID, code)

		_, _ = w.Write([]byte(html))
	}
}
