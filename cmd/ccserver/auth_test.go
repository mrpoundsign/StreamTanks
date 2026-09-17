package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestHMACAuthenticator(t *testing.T) {
	secret := []byte("super-secret-test-key-1234567890")
	auth := NewHMACAuthenticator(secret)

	channel := "mrpoundsign"
	token := auth.GenerateToken(channel)

	// 1. Valid token validation
	if !auth.ValidateToken(channel, token) {
		t.Fatalf("Expected token to be valid for channel %s, but validation failed", channel)
	}

	// 2. Case insensitive channel validation
	if !auth.ValidateToken("MrPoundSign", token) {
		t.Fatalf("Expected token to be valid case-insensitively")
	}

	// 3. Different channel should fail
	if auth.ValidateToken("otherchannel", token) {
		t.Fatalf("Expected token to fail for different channel")
	}

	// 4. Tampered token signature should fail
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		tamperedSig := parts[2] + "x"
		tamperedToken := parts[0] + "." + parts[1] + "." + tamperedSig
		if auth.ValidateToken(channel, tamperedToken) {
			t.Fatalf("Expected tampered token to fail validation")
		}
	}

	// 5. Wrong secret should fail
	wrongAuth := NewHMACAuthenticator([]byte("different-secret-key"))
	if wrongAuth.ValidateToken(channel, token) {
		t.Fatalf("Expected token to fail under different secret key")
	}

	// 6. Authenticate HTTP request with query param token
	req, _ := http.NewRequest("GET", "/ws/host?channel=mrpoundsign&token="+token, nil)
	authChan, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("Expected Authenticate to succeed with query param, got error: %v", err)
	}
	if authChan != "mrpoundsign" {
		t.Fatalf("Expected authenticated channel 'mrpoundsign', got '%s'", authChan)
	}

	// 7. Authenticate HTTP request with Authorization Bearer header
	reqHeader, _ := http.NewRequest("GET", "/ws/host?channel=mrpoundsign", nil)
	reqHeader.Header.Set("Authorization", "Bearer "+token)
	authChanHeader, err := auth.Authenticate(reqHeader)
	if err != nil {
		t.Fatalf("Expected Authenticate to succeed with Bearer header, got error: %v", err)
	}
	if authChanHeader != "mrpoundsign" {
		t.Fatalf("Expected authenticated channel 'mrpoundsign', got '%s'", authChanHeader)
	}

	// 8. Authenticate with missing token fails
	reqNoToken, _ := http.NewRequest("GET", "/ws/host?channel=mrpoundsign", nil)
	_, errNoToken := auth.Authenticate(reqNoToken)
	if errNoToken == nil {
		t.Fatalf("Expected error when token is missing, got nil")
	}

	// 9. InvalidateOlderTokens invalidates past tokens
	auth.InvalidateOlderTokens(channel, 9999999999)
	if auth.ValidateToken(channel, token) {
		t.Fatalf("Expected token to be invalidated after InvalidateOlderTokens with higher timestamp")
	}
}
