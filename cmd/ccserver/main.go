package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	twitchSecret := flag.String("twitch-secret", "", "Base64 encoded Twitch Extension Secret")
	clientID := flag.String("client-id", "", "Twitch Client ID (Extension ID)")
	apiSecret := flag.String("api-secret", "", "Twitch API Client Secret for Helix lookups")
	flag.Parse()

	// If secret is not provided via flag, try environment variable
	if *twitchSecret == "" {
		*twitchSecret = strings.TrimSpace(os.Getenv("TWITCH_EXTENSION_SECRET"))
	}

	if *clientID == "" {
		*clientID = strings.TrimSpace(os.Getenv("TWITCH_CLIENT_ID"))
	}
	if *apiSecret == "" {
		*apiSecret = strings.TrimSpace(os.Getenv("TWITCH_API_SECRET"))
	}

	if *twitchSecret == "" {
		log.Fatal("ERROR: A twitch-secret must be provided via flag or TWITCH_EXTENSION_SECRET environment variable")
	}

	var twitchClient *TwitchAPIClient
	if *clientID != "" && *apiSecret != "" {
		twitchClient = NewTwitchAPIClient(*clientID, *apiSecret)
		log.Println("Twitch Helix API client configured.")
	} else {
		log.Println("WARNING: TWITCH_CLIENT_ID or TWITCH_API_SECRET missing. Viewer username resolution will be disabled.")
	}

	hub := NewHub()
	
	// Option 1 Auth: Trust the first connection that claims the channel
	auth := &TrustFirstAuthenticator{}

	http.Handle("/ws/host", hub.HandleHost(auth))
	http.Handle("/ws/viewer", HandleViewer(hub, *twitchSecret, twitchClient))

	// Serve the Twitch Extension frontend files on /ext/ with permissive CORS headers
	extFs := http.FileServer(http.Dir("./ext-web/public"))
	http.HandleFunc("/ext/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.StripPrefix("/ext/", extFs).ServeHTTP(w, r)
	})

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting StreamTanks C&C Server on %s", addr)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
