package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TwitchAPIClient handles fetching display names from the Twitch Helix API
type TwitchAPIClient struct {
	clientID     string
	clientSecret string

	mu          sync.RWMutex
	accessToken string
	expiresAt   time.Time

	// cache of numeric user ID to Twitch login username
	usernameCache map[string]string
	cacheMu       sync.RWMutex
}

// NewTwitchAPIClient initializes the client
func NewTwitchAPIClient(clientID, clientSecret string) *TwitchAPIClient {
	return &TwitchAPIClient{
		clientID:      clientID,
		clientSecret:  clientSecret,
		usernameCache: make(map[string]string),
	}
}

// ensureToken checks if the token is valid, and refreshes it if needed
func (c *TwitchAPIClient) ensureToken() error {
	c.mu.RLock()
	// Add 5 minute buffer before expiry
	if c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-5*time.Minute)) {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check again in case another goroutine refreshed it
	if c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-5*time.Minute)) {
		return nil
	}

	log.Println("Fetching new Twitch API Access Token...")

	data := url.Values{}
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to get token, status: %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	c.accessToken = result.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	log.Printf("Twitch API token fetched successfully, expires in %d seconds", result.ExpiresIn)
	
	return nil
}

// GetUsername looks up the user login for a given numeric ID
func (c *TwitchAPIClient) GetUsername(userID string) (string, error) {
	if c.clientID == "" || c.clientSecret == "" {
		return "", errors.New("twitch API credentials not configured")
	}

	// Check cache
	c.cacheMu.RLock()
	name, ok := c.usernameCache[userID]
	c.cacheMu.RUnlock()
	if ok {
		return name, nil
	}

	// Ensure token is valid
	if err := c.ensureToken(); err != nil {
		return "", fmt.Errorf("failed to ensure token: %w", err)
	}

	reqURL := "https://api.twitch.tv/helix/users?id=" + userID
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}

	c.mu.RLock()
	req.Header.Add("Authorization", "Bearer "+c.accessToken)
	req.Header.Add("Client-Id", c.clientID)
	c.mu.RUnlock()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("helix users API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data []struct {
			Login string `json:"login"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("no user found for ID %s", userID)
	}

	username := result.Data[0].Login

	// Update cache
	c.cacheMu.Lock()
	c.usernameCache[userID] = username
	c.cacheMu.Unlock()

	return username, nil
}

// TwitchTokenValidation represents the response payload from Twitch's token validation endpoint.
type TwitchTokenValidation struct {
	ClientID  string   `json:"client_id"`
	Login     string   `json:"login"`
	Scopes    []string `json:"scopes"`
	UserID    string   `json:"user_id"`
	ExpiresIn int      `json:"expires_in"`
}

// ValidateTwitchToken validates a Twitch User Access Token against the Twitch OAuth validation endpoint.
func ValidateTwitchToken(accessToken string, validateURL string, client *http.Client) (*TwitchTokenValidation, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("access token cannot be empty")
	}

	if validateURL == "" {
		validateURL = "https://id.twitch.tv/oauth2/validate"
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	req, err := http.NewRequest("GET", validateURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Twitch validation endpoint: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read validation response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("twitch token validation failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var val TwitchTokenValidation
	if err := json.Unmarshal(bodyBytes, &val); err != nil {
		return nil, fmt.Errorf("failed to parse validation response: %w", err)
	}

	if val.UserID == "" {
		return nil, errors.New("validation response missing user_id")
	}

	return &val, nil
}

// SetCachedUsername manually stores a user ID to username mapping in the cache.
func (c *TwitchAPIClient) SetCachedUsername(userID, username string) {
	if c == nil {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.usernameCache[userID] = username
}

// ValidateUserToken validates an access token and updates the internal username cache on success.
func (c *TwitchAPIClient) ValidateUserToken(accessToken string) (*TwitchTokenValidation, error) {
	val, err := ValidateTwitchToken(accessToken, "", nil)
	if err != nil {
		return nil, err
	}
	if c != nil && val.UserID != "" && val.Login != "" {
		c.SetCachedUsername(val.UserID, val.Login)
	}
	return val, nil
}

