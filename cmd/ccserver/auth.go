package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HostAuthenticator defines how a streamer's local instance proves they own a channel.
// Using an interface allows us to easily swap out the authentication strategy
// (e.g., from TrustFirst to SharedSecret to TwitchOAuth) without rewriting the Hub.
type HostAuthenticator interface {
	// Authenticate inspects the incoming HTTP upgrade request and returns
	// the authenticated channel ID, or an error if authentication fails.
	Authenticate(r *http.Request) (string, error)
	GenerateToken(channel string) string
	ValidateToken(channel, token string) bool
	InvalidateOlderTokens(channel string, timestamp int64)
}

// TrustFirstAuthenticator implements HostAuthenticator by blindly trusting
// the channel name provided in the query string (e.g., ?channel=mrpoundsign).
// It relies on the Hub to reject the connection if the channel is already claimed.
type TrustFirstAuthenticator struct{}

func (a *TrustFirstAuthenticator) Authenticate(r *http.Request) (string, error) {
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		return "", errors.New("missing channel parameter")
	}
	return strings.ToLower(channel), nil
}

func (a *TrustFirstAuthenticator) GenerateToken(channel string) string {
	return "trusted"
}

func (a *TrustFirstAuthenticator) ValidateToken(channel, token string) bool {
	return true
}

func (a *TrustFirstAuthenticator) InvalidateOlderTokens(channel string, timestamp int64) {}

// HMACAuthenticator verifies cryptographically signed host tokens using a shared secret.
type HMACAuthenticator struct {
	secret             []byte
	mu                 sync.RWMutex
	minValidTimestamps map[string]int64
}

func NewHMACAuthenticator(secret []byte) *HMACAuthenticator {
	return &HMACAuthenticator{
		secret:             secret,
		minValidTimestamps: make(map[string]int64),
	}
}

// InvalidateOlderTokens marks any tokens created before the given timestamp for a channel as invalid.
func (a *HMACAuthenticator) InvalidateOlderTokens(channel string, timestamp int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cleanChannel := strings.ToLower(strings.TrimSpace(channel))
	a.minValidTimestamps[cleanChannel] = timestamp
}

// GenerateToken produces a token formatted as: <channel>.<timestamp>.<hmac-hex>
func (a *HMACAuthenticator) GenerateToken(channel string) string {
	cleanChannel := strings.ToLower(strings.TrimSpace(channel))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	message := cleanChannel + "." + ts
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s.%s.%s", cleanChannel, ts, sig)
}

// ValidateToken checks if the provided token matches the channel and has a valid HMAC signature.
func (a *HMACAuthenticator) ValidateToken(channel, token string) bool {
	cleanChannel := strings.ToLower(strings.TrimSpace(channel))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	tokenChannel := strings.ToLower(strings.TrimSpace(parts[0]))
	ts := parts[1]
	providedSig := parts[2]

	if cleanChannel != tokenChannel {
		return false
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}

	a.mu.RLock()
	minTS := a.minValidTimestamps[cleanChannel]
	a.mu.RUnlock()

	if tsInt < minTS {
		return false
	}

	message := cleanChannel + "." + ts
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(message))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(providedSig), []byte(expectedSig))
}

func (a *HMACAuthenticator) Authenticate(r *http.Request) (string, error) {
	channel := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("channel")))
	if channel == "" {
		return "", errors.New("missing channel parameter")
	}

	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
			token = strings.TrimSpace(after)
		}
	}

	if token == "" {
		return "", errors.New("missing authentication token")
	}

	if !a.ValidateToken(channel, token) {
		return "", errors.New("invalid authentication token")
	}

	return channel, nil
}
