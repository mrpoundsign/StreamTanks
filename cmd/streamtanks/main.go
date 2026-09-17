package main

import (
	"flag"
	"log"

	"streamtanks/internal/app"
)

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
	ccServerURL = flag.String("cc", "", "URL of the Command & Control (C&C) WebSocket server (leave empty to use database setting, 'off' to disable)")
)

func main() {
	flag.Parse()

	cfg := app.Config{
		Channel:     *channelFlag,
		ListenAddr:  *listenAddr,
		DebugMode:   *debugMode,
		BouncyWalls: *bouncyFlag,
		CCServerURL: *ccServerURL,
		Version:     version,
		Commit:      commit,
		Date:        date,
	}

	if err := app.Run(cfg); err != nil {
		log.Println("Server stopped:", err)
	}
}
