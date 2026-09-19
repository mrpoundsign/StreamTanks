package app

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	extweb "streamtanks/ext-web"
	"streamtanks/web"

	"golang.org/x/net/websocket"
)

// Config encapsulates server configuration parameters.
type Config struct {
	Channel     string
	ListenAddr  string
	DebugMode   bool
	BouncyWalls bool
	CCServerURL string
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
		gameState.Players[localPlayer] = &Player{
			Name:            localPlayer,
			Emote:           "Kappa",
			EmoteURL:        "https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0",
			LastAngle:       45,
			LastPower:       50,
			X:               p1X,
			Y:               getTerrainHeight(gameState.Terrain, p1X),
			LastActiveRound: gameState.RoundID,
			Joined:          false,
		}
		gameState.mu.Unlock()
		log.Printf("Debug mode enabled: spawned %s", localPlayer)
	}

	// Setup Twitch Client only if channel is provided
	startTwitchBot(cfg.Channel)

	// C&C Relay initialization
	if cfg.CCServerURL == "off" {
		gameState.mu.Lock()
		gameState.CCEnabled = false
		gameState.mu.Unlock()
		saveSetting("cc_enabled", "0")
	} else if cfg.CCServerURL != "" {
		gameState.mu.Lock()
		gameState.CCEnabled = true
		gameState.CCServerURL = cfg.CCServerURL
		gameState.mu.Unlock()
		saveSetting("cc_enabled", "1")
		saveSetting("cc_url", cfg.CCServerURL)
	}

	if cfg.Channel != "" {
		StartCCClientManager(cfg.Channel)
	}

	// Setup WebSocket and HTTP server with no-cache headers for overlay assets
	mux := http.NewServeMux()
	mux.Handle("/ws", websocket.Handler(handleWebSocket))

	var exeDir string
	if exePath, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exePath)
	}

	findDirOrNil := func(candidates ...string) string {
		for _, c := range candidates {
			if c == "" {
				continue
			}
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				return c
			}
		}
		return ""
	}

	// Prefer local ./web/public directory if present (for development), fallback to embedded assets
	var fileSystem http.FileSystem
	if diskWeb := findDirOrNil("./web/public", filepath.Join(exeDir, "web", "public")); diskWeb != "" {
		fileSystem = http.Dir(diskWeb)
	} else {
		subFS, err := web.FS()
		if err != nil {
			log.Fatalf("Failed to initialize embedded filesystem: %v", err)
		}
		fileSystem = http.FS(subFS)
	}

	// Serve extension assets (/ext/), preferring disk then falling back to embedded assets
	var extFileSystem http.FileSystem
	if diskExt := findDirOrNil("./ext-web/public", filepath.Join(exeDir, "ext-web", "public")); diskExt != "" {
		extFileSystem = http.Dir(diskExt)
	} else {
		subFS, err := extweb.FS()
		if err != nil {
			log.Fatalf("Failed to initialize embedded extension filesystem: %v", err)
		}
		extFileSystem = http.FS(subFS)
	}

	extHandler := http.StripPrefix("/ext", http.FileServer(extFileSystem))
	mux.HandleFunc("/ext/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		extHandler.ServeHTTP(w, r)
	})

	fileHandler := http.FileServer(fileSystem)

	// Alias /preview to /preview.html for convenience
	mux.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/preview.html"
		fileHandler.ServeHTTP(w, r)
	})

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
	log.Printf("StreamTanks composite preview running at: %s/preview.html", displayURL)

	return http.ListenAndServe(cfg.ListenAddr, mux)
}
