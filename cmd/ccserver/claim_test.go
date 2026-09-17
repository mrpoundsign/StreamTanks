package main

import (
	"testing"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
)

func TestClaimManagerFlow(t *testing.T) {
	cm := &ClaimManager{
		claims:           make(map[string]*PendingClaim),
		channelRefCounts: make(map[string]int),
		client:           twitch.NewAnonymousClient(),
	}

	channel := "mrpoundsign"
	code, approvedChan, cancel := cm.CreateChallenge(channel)

	if len(code) != claimCodeLength {
		t.Fatalf("Expected code length %d, got %d (%s)", claimCodeLength, len(code), code)
	}

	// 1. Ordinary viewer typing %claim should NOT approve the challenge
	viewerMsg := twitch.PrivateMessage{
		Channel: channel,
		User: twitch.User{
			Name:   "random_viewer",
			Badges: map[string]int{"subscriber": 1},
		},
		Message: "%claim " + code,
	}
	cm.handleMessage(viewerMsg)

	select {
	case <-approvedChan:
		t.Fatalf("Challenge approved by non-broadcaster!")
	case <-time.After(50 * time.Millisecond):
		// Expected: still waiting
	}

	// 2. Message on wrong channel should NOT approve
	wrongChannelMsg := twitch.PrivateMessage{
		Channel: "otherchannel",
		User: twitch.User{
			Name:   "otherchannel",
			Badges: map[string]int{"broadcaster": 1},
		},
		Message: "%claim " + code,
	}
	cm.handleMessage(wrongChannelMsg)

	select {
	case <-approvedChan:
		t.Fatalf("Challenge approved from wrong channel!")
	case <-time.After(50 * time.Millisecond):
		// Expected: still waiting
	}

	// 3. Broadcaster typing %claim on the matching channel SHOULD approve
	broadcasterMsg := twitch.PrivateMessage{
		Channel: channel,
		User: twitch.User{
			Name:   "mrpoundsign",
			Badges: map[string]int{"broadcaster": 1},
		},
		Message: "%claim " + code,
	}
	cm.handleMessage(broadcasterMsg)

	select {
	case <-approvedChan:
		// Success!
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Challenge was NOT approved by broadcaster within timeout")
	}

	cancel()
}

func TestClaimManagerCancel(t *testing.T) {
	cm := &ClaimManager{
		claims:           make(map[string]*PendingClaim),
		channelRefCounts: make(map[string]int),
		client:           twitch.NewAnonymousClient(),
	}

	code, _, cancel := cm.CreateChallenge("testchannel")
	if _, exists := cm.claims[code]; !exists {
		t.Fatalf("Challenge should exist in claims map")
	}

	cancel()

	if _, exists := cm.claims[code]; exists {
		t.Fatalf("Challenge should be removed after cancel")
	}
}
