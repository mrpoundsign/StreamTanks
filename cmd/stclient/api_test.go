package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCCClient_RequestDeviceCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/device/code" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
			DeviceCode:              "dev_123",
			UserCode:                "ST-7777",
			VerificationURI:         "http://test/link",
			VerificationURIComplete: "http://test/link?code=ST-7777",
			ExpiresIn:               600,
			Interval:                2,
		})
	}))
	defer server.Close()

	client := NewCCClient(server.URL)
	resp, err := client.RequestDeviceCode()
	if err != nil {
		t.Fatalf("RequestDeviceCode failed: %v", err)
	}

	if resp.DeviceCode != "dev_123" || resp.UserCode != "ST-7777" {
		t.Errorf("unexpected code response: %+v", resp)
	}
}

func TestCCClient_PollDeviceAuth(t *testing.T) {
	approved := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/device/poll" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if !approved {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "authorization_pending",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(DevicePollResponse{
			Status:    "approved",
			Token:     "test_jwt_token",
			UserID:    "999",
			Username:  "streamer_bob",
			ExpiresIn: 86400,
		})
	}))
	defer server.Close()

	client := NewCCClient(server.URL)

	// 1. Pending poll
	poll1, err := client.PollDeviceAuth("dev_123")
	if err != nil {
		t.Fatalf("PollDeviceAuth pending failed: %v", err)
	}
	if poll1.Status != "authorization_pending" {
		t.Errorf("expected authorization_pending, got: %s", poll1.Status)
	}

	// 2. Approved poll
	approved = true
	poll2, err := client.PollDeviceAuth("dev_123")
	if err != nil {
		t.Fatalf("PollDeviceAuth approved failed: %v", err)
	}
	if poll2.Status != "approved" || poll2.Username != "streamer_bob" || poll2.Token != "test_jwt_token" {
		t.Errorf("unexpected approved poll response: %+v", poll2)
	}
}

func TestCCClient_GetActiveHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"host1", "host2"})
	}))
	defer server.Close()

	client := NewCCClient(server.URL)
	hosts, err := client.GetActiveHosts()
	if err != nil {
		t.Fatalf("GetActiveHosts failed: %v", err)
	}
	if len(hosts) != 2 || hosts[0] != "host1" || hosts[1] != "host2" {
		t.Errorf("unexpected hosts: %+v", hosts)
	}
}
