package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCodeResponse represents the payload from POST /api/auth/device/code
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DevicePollResponse represents the payload from GET /api/auth/device/poll
type DevicePollResponse struct {
	Status    string `json:"status"`
	Token     string `json:"token,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	ExpiresIn int64  `json:"expires_in,omitempty"`
	Error     string `json:"error,omitempty"`
}

// CCClient handles HTTP communications with the StreamTanks C&C relay server
type CCClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewCCClient(baseURL string) *CCClient {
	cleanURL := strings.TrimRight(baseURL, "/")
	if cleanURL == "" {
		cleanURL = "https://st-cc.poundsigndesign.com"
	}
	return &CCClient{
		BaseURL: cleanURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RequestDeviceCode asks the C&C server for a new pairing code
func (c *CCClient) RequestDeviceCode() (*DeviceCodeResponse, error) {
	reqURL := c.BaseURL + "/api/auth/device/code"
	resp, err := c.HTTPClient.Post(reqURL, "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to request device code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var codeResp DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&codeResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &codeResp, nil
}

// PollDeviceAuth checks if the pairing code has been authorized
func (c *CCClient) PollDeviceAuth(deviceCode string) (*DevicePollResponse, error) {
	reqURL := fmt.Sprintf("%s/api/auth/device/poll?device_code=%s", c.BaseURL, url.QueryEscape(deviceCode))
	resp, err := c.HTTPClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("poll request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var pollResp DevicePollResponse
	if err := json.NewDecoder(resp.Body).Decode(&pollResp); err != nil {
		return nil, fmt.Errorf("failed to decode poll response: %w", err)
	}

	if resp.StatusCode == http.StatusAccepted {
		pollResp.Status = "authorization_pending"
		return &pollResp, nil
	}

	if resp.StatusCode == http.StatusOK && pollResp.Status == "approved" {
		return &pollResp, nil
	}

	if pollResp.Error != "" {
		return nil, errors.New(pollResp.Error)
	}

	return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
}

// GetActiveHosts fetches the list of active host channels
func (c *CCClient) GetActiveHosts() ([]string, error) {
	reqURL := c.BaseURL + "/api/hosts"
	resp, err := c.HTTPClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch active hosts: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var hosts []string
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		return nil, fmt.Errorf("failed to decode hosts: %w", err)
	}

	return hosts, nil
}
