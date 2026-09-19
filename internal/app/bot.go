package app

import (
	"log"
	"strings"
	"sync"

	"github.com/gempir/go-twitch-irc/v4"
)

var (
	botMu        sync.Mutex
	twitchClient *twitch.Client
)

func startTwitchBot(channel string) {
	setTwitchBotChannel(channel)
}

func setTwitchBotChannel(channel string) {
	botMu.Lock()
	defer botMu.Unlock()

	if twitchClient != nil {
		log.Println("Disconnecting existing Twitch client...")
		_ = twitchClient.Disconnect()
		twitchClient = nil
	}

	cleanChannel := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(channel, "#")))
	if cleanChannel == "" || cleanChannel == "off" || cleanChannel == "none" || cleanChannel == "clear" {
		log.Println("No Twitch channel specified; running in local overlay mode (Twitch chat disabled)")
		return
	}

	client := twitch.NewAnonymousClient()
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		processCommand(message.User.Name, message.Message, message.Emotes, &message.User)
	})
	client.Join(cleanChannel)
	twitchClient = client

	go func(c *twitch.Client, ch string) {
		log.Printf("Connecting to Twitch channel: %s", ch)
		err := c.Connect()
		if err != nil && err != twitch.ErrClientDisconnected {
			log.Printf("Twitch client error for %s: %v", ch, err)
		}
	}(client, cleanChannel)
}
