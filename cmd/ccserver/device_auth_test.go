package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeviceAuthManager_Lifecycle(t *testing.T) {
	mgr := NewDeviceAuthManager()

	// 1. Create session
	sess, err := mgr.CreateSession(time.Minute)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if !strings.HasPrefix(sess.DeviceCode, "st_dev_") {
		t.Errorf("unexpected device code format: %s", sess.DeviceCode)
	}
	if !strings.HasPrefix(sess.UserCode, "ST-") {
		t.Errorf("unexpected user code format: %s", sess.UserCode)
	}
	if sess.Approved {
		t.Error("expected new session to not be approved")
	}

	// 2. Fetch by device code
	devSess, err := mgr.GetSessionByDevice(sess.DeviceCode)
	if err != nil {
		t.Fatalf("failed to get session by device: %v", err)
	}
	if devSess.UserCode != sess.UserCode {
		t.Errorf("expected user code %s, got %s", sess.UserCode, devSess.UserCode)
	}

	// 3. Approve session
	err = mgr.ApproveSession(sess.UserCode, "jwt_token_123", "twitch_user_1", "streamtanker")
	if err != nil {
		t.Fatalf("failed to approve session: %v", err)
	}

	// 4. Verify approved state
	devSess2, err := mgr.GetSessionByDevice(sess.DeviceCode)
	if err != nil {
		t.Fatalf("failed to get approved session: %v", err)
	}
	if !devSess2.Approved {
		t.Error("expected session to be approved")
	}
	if devSess2.Token != "jwt_token_123" || devSess2.Username != "streamtanker" {
		t.Errorf("unexpected approved session content: %+v", devSess2)
	}

	// 5. Test expiration
	expMgr := NewDeviceAuthManager()
	expSess, _ := expMgr.CreateSession(-time.Second) // already expired
	_, err = expMgr.GetSessionByDevice(expSess.DeviceCode)
	if err == nil {
		t.Error("expected error for expired session, got nil")
	}
}

func TestDeviceAuth_Endpoints(t *testing.T) {
	mgr := NewDeviceAuthManager()
	secret := []byte("secretforhttptestdeviceauth1234567")

	// 1. POST /api/auth/device/code
	reqCode := httptest.NewRequest(http.MethodPost, "/api/auth/device/code", nil)
	wCode := httptest.NewRecorder()
	HandleDeviceCode(mgr, "http://localhost:8080")(wCode, reqCode)

	if wCode.Code != http.StatusOK {
		t.Fatalf("expected 200 for code generation, got %d", wCode.Code)
	}

	var codeResp struct {
		DeviceCode             string `json:"device_code"`
		UserCode               string `json:"user_code"`
		VerificationURI        string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
	}
	if err := json.NewDecoder(wCode.Body).Decode(&codeResp); err != nil {
		t.Fatalf("failed to decode code response: %v", err)
	}

	if codeResp.DeviceCode == "" || codeResp.UserCode == "" {
		t.Fatalf("expected non-empty codes, got: %+v", codeResp)
	}
	if !strings.Contains(codeResp.VerificationURIComplete, codeResp.UserCode) {
		t.Errorf("expected complete URI to contain user code: %s", codeResp.VerificationURIComplete)
	}

	// 2. GET /api/auth/device/poll (pending)
	reqPoll1 := httptest.NewRequest(http.MethodGet, "/api/auth/device/poll?device_code="+codeResp.DeviceCode, nil)
	wPoll1 := httptest.NewRecorder()
	HandleDevicePoll(mgr)(wPoll1, reqPoll1)

	if wPoll1.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for pending poll, got %d", wPoll1.Code)
	}
	var pollResp1 map[string]string
	_ = json.NewDecoder(wPoll1.Body).Decode(&pollResp1)
	if pollResp1["status"] != "authorization_pending" {
		t.Errorf("expected authorization_pending, got: %+v", pollResp1)
	}

	// 3. Mock approve directly
	token, _ := GenerateViewerToken("user_999", "luckyviewer", secret, time.Hour)
	err := mgr.ApproveSession(codeResp.UserCode, token, "user_999", "luckyviewer")
	if err != nil {
		t.Fatalf("failed to approve session: %v", err)
	}

	// 4. GET /api/auth/device/poll (approved)
	reqPoll2 := httptest.NewRequest(http.MethodGet, "/api/auth/device/poll?device_code="+codeResp.DeviceCode, nil)
	wPoll2 := httptest.NewRecorder()
	HandleDevicePoll(mgr)(wPoll2, reqPoll2)

	if wPoll2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for approved poll, got %d", wPoll2.Code)
	}
	var pollResp2 map[string]any
	_ = json.NewDecoder(wPoll2.Body).Decode(&pollResp2)
	if pollResp2["status"] != "approved" {
		t.Errorf("expected status approved, got: %+v", pollResp2)
	}
	if pollResp2["username"] != "luckyviewer" || pollResp2["token"] != token {
		t.Errorf("unexpected approved poll body: %+v", pollResp2)
	}

	// 5. Test Link Page rendering
	reqLink := httptest.NewRequest(http.MethodGet, "/link?code="+codeResp.UserCode, nil)
	wLink := httptest.NewRecorder()
	HandleLinkPage("test_client_id_123")(wLink, reqLink)

	if wLink.Code != http.StatusOK {
		t.Errorf("expected 200 for link page, got %d", wLink.Code)
	}
	bodyStr := wLink.Body.String()
	if !strings.Contains(bodyStr, codeResp.UserCode) {
		t.Errorf("expected link page to contain user code %s", codeResp.UserCode)
	}
	if !strings.Contains(bodyStr, "test_client_id_123") {
		t.Errorf("expected link page to contain client id")
	}
}

func TestDeviceApprove_Endpoint(t *testing.T) {
	mgr := NewDeviceAuthManager()
	secret := []byte("secretforapproveendpoint123456789")

	sess, _ := mgr.CreateSession(time.Minute)

	// Mock Twitch validation server
	mockTwitch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "OAuth good_token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"client_id": "twitch_client",
				"login": "coolpilot",
				"scopes": [],
				"user_id": "555123",
				"expires_in": 3600
			}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status": 401, "message": "invalid"}`))
	}))
	defer mockTwitch.Close()

	// Handler with mock validator
	testApproveHandler := func(w http.ResponseWriter, r *http.Request) {
		var req DeviceApproveRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		val, err := ValidateTwitchToken(req.AccessToken, mockTwitch.URL, nil)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid Twitch access token"})
			return
		}
		token, _ := GenerateViewerToken(val.UserID, val.Login, secret, time.Hour)
		_ = mgr.ApproveSession(req.UserCode, token, val.UserID, val.Login)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "username": val.Login})
	}

	// 1. Invalid token
	bodyBad := `{"user_code": "` + sess.UserCode + `", "access_token": "bad_token"}`
	reqBad := httptest.NewRequest(http.MethodPost, "/api/auth/device/approve", bytes.NewBufferString(bodyBad))
	wBad := httptest.NewRecorder()
	testApproveHandler(wBad, reqBad)
	if wBad.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad token, got %d", wBad.Code)
	}

	// 2. Good token
	bodyGood := `{"user_code": "` + sess.UserCode + `", "access_token": "good_token"}`
	reqGood := httptest.NewRequest(http.MethodPost, "/api/auth/device/approve", bytes.NewBufferString(bodyGood))
	wGood := httptest.NewRecorder()
	testApproveHandler(wGood, reqGood)
	if wGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for good token, got %d", wGood.Code)
	}

	// Verify session approved
	approvedSess, err := mgr.GetSessionByDevice(sess.DeviceCode)
	if err != nil || !approvedSess.Approved || approvedSess.Username != "coolpilot" {
		t.Fatalf("expected approved session for coolpilot, got %+v", approvedSess)
	}
}
