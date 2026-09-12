package main

import (
	"log"

	"github.com/gempir/go-twitch-irc/v4"
)

func startTwitchBot(channel string) {
	if channel == "" {
		log.Println("No Twitch channel specified; running in local overlay mode (Twitch chat disabled)")
		return
	}

	client := twitch.NewAnonymousClient()
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		processCommand(message.User.Name, message.Message, message.Emotes)
	})
	client.Join(channel)

	go func() {
		log.Printf("Connecting to Twitch channel: %s", channel)
		err := client.Connect()
		if err != nil {
			log.Fatalf("Twitch client error: %v", err)
		}
	}()
}
