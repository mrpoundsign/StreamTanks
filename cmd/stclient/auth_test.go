package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceAuthFlow_Approval(t *testing.T) {
	var pollCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth/device/code" {
			_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
				DeviceCode:              "dev_test",
				UserCode:                "ST-9999",
				VerificationURIComplete: "http://test/link?code=ST-9999",
				ExpiresIn:               10,
				Interval:                1,
			})
			return
		}
		if r.URL.Path == "/api/auth/device/poll" {
			cnt := pollCount.Add(1)
			if cnt < 2 {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "authorization_pending"})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(DevicePollResponse{
				Status:   "approved",
				Token:    "jwt_flow_token",
				UserID:   "user_111",
				Username: "cool_user",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewCCClient(server.URL)
	flow := NewDeviceAuthFlow(client)

	codeChan := make(chan string, 1)
	approvedChan := make(chan *DevicePollResponse, 1)
	errChan := make(chan error, 1)

	flow.Start(
		func(resp *DeviceCodeResponse) {
			codeChan <- resp.UserCode
		},
		func(resp *DevicePollResponse) {
			approvedChan <- resp
		},
		func(err error) {
			errChan <- err
		},
	)

	select {
	case code := <-codeChan:
		if code != "ST-9999" {
			t.Errorf("expected ST-9999, got %s", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for code callback")
	}

	select {
	case approved := <-approvedChan:
		if approved.Username != "cool_user" || approved.Token != "jwt_flow_token" {
			t.Errorf("unexpected approved response: %+v", approved)
		}
	case err := <-errChan:
		t.Fatalf("unexpected error callback: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for approval")
	}
}

func TestDeviceAuthFlow_Cancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth/device/code" {
			_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
				DeviceCode: "dev_cancel",
				UserCode:   "ST-0000",
				ExpiresIn:  10,
				Interval:   1,
			})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "authorization_pending"})
	}))
	defer server.Close()

	client := NewCCClient(server.URL)
	flow := NewDeviceAuthFlow(client)

	codeChan := make(chan bool, 1)
	flow.Start(
		func(resp *DeviceCodeResponse) {
			codeChan <- true
		},
		nil,
		nil,
	)

	<-codeChan
	flow.Cancel()
	// No panic or hangs on cancel
}
