package main

import (
	"crypto/rand"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
)

const (
	claimCodeChars  = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	claimCodeLength = 6
	claimDuration   = 5 * time.Minute
)

// PendingClaim represents an active in-chat authorization challenge.
type PendingClaim struct {
	Channel   string
	Code      string
	CreatedAt time.Time
	Approved  chan struct{}
}

// ClaimManager tracks in-flight authorization challenges and verifies
// them against public Twitch IRC broadcaster messages.
type ClaimManager struct {
	mu               sync.Mutex
	claims           map[string]*PendingClaim // code -> PendingClaim
	channelRefCounts map[string]int           // channel -> active claims count
	client           *twitch.Client
}

// NewClaimManager initializes the IRC client and starts listening for %claim commands.
func NewClaimManager() *ClaimManager {
	cm := &ClaimManager{
		claims:           make(map[string]*PendingClaim),
		channelRefCounts: make(map[string]int),
		client:           twitch.NewAnonymousClient(),
	}

	cm.client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		cm.handleMessage(message)
	})

	go func() {
		for {
			log.Println("[ClaimManager] Connecting to Twitch IRC anonymously...")
			if err := cm.client.Connect(); err != nil {
				log.Printf("[ClaimManager] Twitch IRC disconnected: %v. Reconnecting in 5s...", err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	// Periodic cleanup for expired claims
	go cm.cleanupLoop()

	return cm
}

func (cm *ClaimManager) generateCode() string {
	b := make([]byte, claimCodeLength)
	for i := range b {
		idx := make([]byte, 1)
		_, _ = rand.Read(idx)
		b[i] = claimCodeChars[int(idx[0])%len(claimCodeChars)]
	}
	return string(b)
}

// CreateChallenge registers a new claim challenge for a channel.
// Returns the code, a channel that closes when approved, and a cancel function.
func (cm *ClaimManager) CreateChallenge(channel string) (string, <-chan struct{}, func()) {
	cleanChannel := strings.ToLower(strings.TrimSpace(channel))

	cm.mu.Lock()
	defer cm.mu.Unlock()

	var code string
	for {
		code = cm.generateCode()
		if _, exists := cm.claims[code]; !exists {
			break
		}
	}

	approved := make(chan struct{})
	claim := &PendingClaim{
		Channel:   cleanChannel,
		Code:      code,
		CreatedAt: time.Now(),
		Approved:  approved,
	}

	cm.claims[code] = claim

	if cm.channelRefCounts[cleanChannel] == 0 {
		log.Printf("[ClaimManager] Joining Twitch IRC channel: #%s for claim challenge", cleanChannel)
		cm.client.Join(cleanChannel)
	}
	cm.channelRefCounts[cleanChannel]++

	cancel := func() {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		if c, exists := cm.claims[code]; exists {
			delete(cm.claims, code)
			cm.channelRefCounts[c.Channel]--
			if cm.channelRefCounts[c.Channel] <= 0 {
				delete(cm.channelRefCounts, c.Channel)
				log.Printf("[ClaimManager] Departing Twitch IRC channel: #%s", c.Channel)
				cm.client.Depart(c.Channel)
			}
		}
	}

	return code, approved, cancel
}

func (cm *ClaimManager) handleMessage(message twitch.PrivateMessage) {
	// Only consider messages from the channel broadcaster
	isBroadcaster := (message.User.Badges != nil && message.User.Badges["broadcaster"] > 0) || strings.EqualFold(message.User.Name, message.Channel)
	if !isBroadcaster {
		return
	}

	trimmed := strings.TrimSpace(message.Message)
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return
	}

	cmd := strings.ToLower(parts[0])
	if cmd != "%claim" && cmd != "!claim" {
		return
	}

	code := strings.ToUpper(parts[1])

	cm.mu.Lock()
	defer cm.mu.Unlock()

	claim, exists := cm.claims[code]
	if !exists {
		return
	}

	if !strings.EqualFold(claim.Channel, message.Channel) {
		return
	}

	log.Printf("[ClaimManager] Valid claim for channel #%s verified from broadcaster %s! (code: %s)", claim.Channel, message.User.Name, code)
	close(claim.Approved)
	delete(cm.claims, code)

	cm.channelRefCounts[claim.Channel]--
	if cm.channelRefCounts[claim.Channel] <= 0 {
		delete(cm.channelRefCounts, claim.Channel)
		cm.client.Depart(claim.Channel)
	}
}

func (cm *ClaimManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cm.mu.Lock()
		now := time.Now()
		for code, claim := range cm.claims {
			if now.Sub(claim.CreatedAt) > claimDuration {
				delete(cm.claims, code)
				cm.channelRefCounts[claim.Channel]--
				if cm.channelRefCounts[claim.Channel] <= 0 {
					delete(cm.channelRefCounts, claim.Channel)
					cm.client.Depart(claim.Channel)
				}
				log.Printf("[ClaimManager] Expired claim code %s for channel #%s", code, claim.Channel)
			}
		}
		cm.mu.Unlock()
	}
}
