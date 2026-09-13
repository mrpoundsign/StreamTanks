package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/websocket"
)

//go:embed public/*
var embeddedPublic embed.FS

// Build version info injected by GoReleaser ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	channelFlag = flag.String("channel", "", "Twitch channel to join (optional)")
	listenAddr  = flag.String("addr", ":8102", "HTTP listen address")
	debugMode   = flag.Bool("debug", false, "Enable debug mode with a test bot for single-player testing")
	bouncyFlag  = flag.Bool("bouncy", false, "Enable bouncy walls for bullets (+10% speed) and tanks (+50% speed)")
)

func getDebugUsername() string {
	if channelFlag != nil && *channelFlag != "" {
		return *channelFlag
	}
	return "Player1"
}

func main() {
	flag.Parse()

	log.Printf("Starting StreamTanks %s (commit: %s, built: %s)", version, commit, date)

	if err := initDB("streamtanks.db"); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer closeDB()

	// Load leaderboard and settings from DB
	loadLeaderboard()
	loadSettings()

	gameState.mu.Lock()
	gameState.Terrain = generateTerrain(gameState.TerrainMin, gameState.TerrainMax)
	gameState.mu.Unlock()

	if *bouncyFlag {
		gameState.mu.Lock()
		gameState.BouncyWalls = true
		gameState.mu.Unlock()
	}

	if *debugMode {
		localPlayer := getDebugUsername()
		gameState.mu.Lock()
		gameState.Debug = true
		p1X := float64(defaultTerrainWidth)/2.0 - 100.0
		botX := float64(defaultTerrainWidth)/2.0 + 100.0
		gameState.Players[localPlayer] = &Player{
			Name:      localPlayer,
			Emote:     "Kappa",
			EmoteURL:  "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0",
			LastAngle: 45,
			LastPower:       50,
			X:               p1X,
			Y:               getTerrainHeight(gameState.Terrain, p1X),
			LastActiveRound: gameState.RoundID,
		}
		gameState.Players["TargetBot"] = &Player{
			Name:            "TargetBot",
			Emote:           "PogChamp",
			EmoteURL:        "https://static-cdn.jtvnw.net/emoticons/v2/305954156/default/dark/2.0",
			LastAngle:       135,
			LastPower:       50,
			X:               botX,
			Y:               getTerrainHeight(gameState.Terrain, botX),
			LastActiveRound: gameState.RoundID,
		}
		gameState.mu.Unlock()
		log.Printf("Debug mode enabled: spawned %s and TargetBot", localPlayer)
	}

	// Setup Twitch Client only if channel flag is provided
	startTwitchBot(*channelFlag)

	// Setup WebSocket and HTTP server with no-cache headers for overlay assets
	http.Handle("/ws", websocket.Handler(handleWebSocket))

	// Prefer local ./public directory if present (for development), fallback to embedded assets
	var fileSystem http.FileSystem
	if _, err := os.Stat("./public"); err == nil {
		fileSystem = http.Dir("./public")
	} else {
		subFS, err := fs.Sub(embeddedPublic, "public")
		if err != nil {
			log.Fatalf("Failed to initialize embedded filesystem: %v", err)
		}
		fileSystem = http.FS(subFS)
	}

	fileHandler := http.FileServer(fileSystem)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fileHandler.ServeHTTP(w, r)
	})

	displayURL := *listenAddr
	if strings.HasPrefix(displayURL, ":") {
		displayURL = "localhost" + displayURL
	}
	if !strings.HasPrefix(displayURL, "http://") && !strings.HasPrefix(displayURL, "https://") {
		displayURL = "http://" + displayURL
	}
	log.Printf("StreamTanks overlay running at: %s", displayURL)
	log.Printf("StreamTanks admin console running at: %s/admin", displayURL)
	if err := http.ListenAndServe(*listenAddr, nil); err != nil {
		log.Println("Server stopped:", err)
	}
}
