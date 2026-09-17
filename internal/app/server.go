package app

import (
	"log"
	"net/http"
	"os"
	"strings"

	"streamtanks/web"
)

// Config encapsulates server configuration parameters.
type Config struct {
	Channel     string
	ListenAddr  string
	DebugMode   bool
	BouncyWalls bool
	Version     string
	Commit      string
	Date        string
}

var channelName string

func getDebugUsername() string {
	if channelName != "" {
		return channelName
	}
	return "Player1"
}

// Run starts the StreamTanks server with the specified configuration.
func Run(cfg Config) error {
	channelName = cfg.Channel
	log.Printf("Starting StreamTanks %s (commit: %s, built: %s)", cfg.Version, cfg.Commit, cfg.Date)

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

	if cfg.BouncyWalls {
		gameState.mu.Lock()
		gameState.BouncyWalls = true
		gameState.mu.Unlock()
	}

	if cfg.DebugMode {
		localPlayer := getDebugUsername()
		gameState.mu.Lock()
		gameState.Debug = true
		p1X := float64(defaultTerrainWidth)/2.0 - 100.0
		botX := float64(defaultTerrainWidth)/2.0 + 100.0
		gameState.Players[localPlayer] = &Player{
			Name:            localPlayer,
			Emote:           "Kappa",
			EmoteURL:        "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0",
			LastAngle:       45,
			LastPower:       50,
			X:               p1X,
			Y:               getTerrainHeight(gameState.Terrain, p1X),
			LastActiveRound: gameState.RoundID,
		}
		gameState.Players["TargetBot"] = &Player{
			Name:            "TargetBot",
			IsBot:           true,
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

	// Setup Twitch Client only if channel is provided
	startTwitchBot(cfg.Channel)

	// Setup WebSocket and HTTP server with no-cache headers for overlay assets
	mux := http.NewServeMux()
	mux.Handle("/ws", WebSocketHandler())

	// Prefer local ./web/public directory if present (for development), fallback to embedded assets
	var fileSystem http.FileSystem
	if _, err := os.Stat("./web/public"); err == nil {
		fileSystem = http.Dir("./web/public")
	} else {
		subFS, err := web.FS()
		if err != nil {
			log.Fatalf("Failed to initialize embedded filesystem: %v", err)
		}
		fileSystem = http.FS(subFS)
	}

	fileHandler := http.FileServer(fileSystem)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fileHandler.ServeHTTP(w, r)
	})

	displayURL := cfg.ListenAddr
	if strings.HasPrefix(displayURL, ":") {
		displayURL = "localhost" + displayURL
	}
	if !strings.HasPrefix(displayURL, "http://") && !strings.HasPrefix(displayURL, "https://") {
		displayURL = "http://" + displayURL
	}
	log.Printf("StreamTanks overlay running at: %s", displayURL)
	log.Printf("StreamTanks admin console running at: %s/admin", displayURL)

	return http.ListenAndServe(cfg.ListenAddr, mux)
}
