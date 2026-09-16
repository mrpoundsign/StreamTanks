package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/websocket"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	twitchSecret := flag.String("twitch-secret", "", "Base64 encoded Twitch Extension Secret")
	flag.Parse()

	// If secret is not provided via flag, try environment variable
	if *twitchSecret == "" {
		*twitchSecret = os.Getenv("TWITCH_EXTENSION_SECRET")
	}

	if *twitchSecret == "" {
		log.Fatal("ERROR: A twitch-secret must be provided via flag or TWITCH_EXTENSION_SECRET environment variable")
	}

	hub := NewHub()
	
	// Option 1 Auth: Trust the first connection that claims the channel
	auth := &TrustFirstAuthenticator{}

	http.Handle("/ws/host", websocket.Handler(hub.HandleHost(auth)))
	http.Handle("/ws/viewer", websocket.Handler(HandleViewer(hub, *twitchSecret)))

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting StreamTanks C&C Server on %s", addr)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
