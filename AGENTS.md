# StreamTanks — Agent & Project Knowledge Base (AGENTS.md)

## 1. User Preferences & Working Rules

> [!IMPORTANT]
> Always adhere strictly to these core working preferences (inherited from `../InkAnim`):
> 1. **True Pair Programming Assistant (No "Vibe Coding")**: The user is an experienced, knowledgeable developer and lead. Never act like an autonomous black box. When investigating an issue or unexpected behavior, always communicate technical findings, underlying root causes, and mechanics to the user *first*. Discuss and align before creating GitHub issues or making code changes.
> 2. **Feedback & Explicit Approval on Decisions**: Always present clear options, findings, and proposed designs, then wait for user approval before moving forward. Never jump ahead to file tickets, create branches, or write code based on assumptions. Once a specific plan is approved, executing that agreed plan autonomously is expected, but any new findings, deviations, or decisions must pause for communication and approval.
> 3. **One ticket at a time**: We work on exactly one issue/ticket at a time unless explicitly directed otherwise. Exactly **1 commit per issue**.
> 4. **User tests GUI before committing**: Always stop and let the user manually test the GUI/overlay before any changes are committed to git.
> 5. **Prioritize Built-in Agent Tools Over Shelling Out**: Always use built-in tools (`grep_search`, `list_dir`, `view_file`, and Go MCP tools) for searching code, finding files, and inspecting contents. Do not shell out to PowerShell commands (e.g., `Select-String`, `grep`, `dir`, `cat`) when built-in tools are capable of performing the search or inspection. Only shell out via `run_command` when strictly necessary (e.g., git operations, building/running code, tests, or `gh` CLI).
> 6. **Always Check for Go Modernization**: Always run `go fix -diff ./...` to check for modernization diffs to evaluate before completing Go backend tasks.

---

## 2. Project Overview & Mission
**StreamTanks** is an open-source, interactive Twitch chat artillery game and OBS browser overlay. Stream viewers join the battlefield via Twitch chat commands, control neon artillery tanks, aim angle and shot power, maneuver across dynamic destructible terrain, and battle for the stream leaderboard.

- **Primary Repository**: `mrpoundsign/StreamTanks`
- **Stack**: Pure Go backend (`main.go`), WebSocket hub (`golang.org/x/net/websocket`), embedded SQLite database (`modernc.org/sqlite`), Twitch IRC client (`github.com/gempir/go-twitch-irc/v4`), vanilla HTML5 Canvas + JavaScript frontend overlay (`public/`).
- **Overlay Resolution**: Standard 1920x1080 transparent canvas designed for OBS Studio browser sources.

---

## 3. Architecture & Codebase Map

```
StreamTanks/
├── cmd/
│   ├── streamtanks/
│   │   └── main.go       # CLI entrypoint, flag parsing, server bootstrap
│   └── ccserver/         # Twitch Extension Command & Control (C&C) relay server
│       ├── main.go       # C&C HTTP/WS entrypoint, static file server, health check
│       ├── hub.go        # Broadcaster host connection pool & viewer broadcast hub
│       ├── auth.go       # Twitch extension HMAC/JWT authentication
│       ├── claim.go      # Streamer claim & host registration manager
│       └── viewer.go     # Viewer WebSocket handler & Twitch chat command dispatcher
├── internal/
│   └── app/              # Core backend engine & services
│       ├── game.go       # Phase state machine, game loop, physics, commands
│       ├── types.go      # GameState, Player, WSMessage structs & typed constants
│       ├── terrain.go    # Heightmap generation, math, crater deformation
│       ├── storage.go    # SQLite database schema, settings & leaderboard persistence
│       ├── bot.go        # Twitch IRC anonymous client listener
│       ├── ws.go         # WebSocket client pool, message pump, thread-safe broadcast
│       ├── cc_client.go  # Outbound WebSocket client connecting local game to C&C server
│       ├── server.go     # HTTP asset router, static/embedded FS server, app lifecycle
│       └── app_test.go   # Go test suite covering commands, config, bots, and concurrency
├── web/                  # Frontend OBS overlay & assets
│   ├── embed.go          # //go:embed public/* exporter
│   ├── src/              # TypeScript source files (bundled to web/public/game.js)
│   │   ├── game.ts       # Game loop, physics updates, command dispatching, debug controls
│   │   ├── network.ts    # WebSocket client with auto-reconnect polling
│   │   ├── renderer.ts   # Canvas rendering: terrain, protractor, tanks, projectiles, sparks
│   │   ├── terrain.ts    # Heightmap generation, slope calculation, crater deformation
│   │   └── types.ts      # Frontend TypeScript types and exported constants
│   └── public/           # Frontend overlay files served via HTTP and OBS browser source
│       ├── index.html    # HTML layout with canvas, HUD layers, leaderboard & cache-control tags
│       ├── game.js       # Production bundled JavaScript (compiled from web/src/)
│       ├── style.css     # Neon cyberpunk styling, transparent OBS background, animations
│       └── admin/        # Dedicated Commander Admin Console (/admin)
│           ├── index.html# Dashboard layout: controls, player table, leaderboard, console
│           ├── admin.css # Cyberpunk neon dashboard styling
│           └── admin.js  # Live WebSocket sync, command dispatcher, history
├── ext-web/              # Twitch Extension (Desktop Video Overlay & Mobile Views)
│   ├── src/
│   │   └── ext.ts        # Protractor aiming, power gauge, Twitch JWT auth, C&C WebSocket
│   └── public/
│       ├── video_overlay.html # Desktop video player transparent SVG protractor overlay
│       ├── mobile.html        # Mobile extension view with standalone protractor & power gauge
│       ├── config.html        # Broadcaster configuration panel
│       ├── ext.css            # Extension styling, glowing neon elements, touch targets
│       └── ext.js             # Bundled extension script
├── dist/                 # Release artifacts (git-ignored; e.g. dist/extension.zip)
├── build.ps1             # PowerShell script: TypeScript build, Go tests, lint, compilation
├── build.bat             # Batch launcher for build.ps1
├── go.mod                # Go module definitions
├── go.sum                # Checksums
├── package.json          # Frontend build tooling (TypeScript, esbuild)
├── tsconfig.json         # Strict TypeScript configuration (ES2022)
├── AGENTS.md             # Coding standards, architecture documentation, agent instructions
└── .gitignore            # Ignores .exe binaries, .db files, dist/, and IDE state
```

---

## 4. Issue Tracking & Backlog

Active issues and feature requests are tracked exclusively via **[GitHub Issues](https://github.com/mrpoundsign/StreamTanks/issues)** (the single source of truth):
- **Inspect Open Issues**: `gh issue list --state open`
- **View Specific Issue Details**: `gh issue view <issue-number>`
- **Close Issue with Commit**: Reference `Fixes #<number>` or `Closes #<number>` in git commit messages.

---

## 5. Twitch Chat Commands & Configuration

All commands default to the `%` prefix (configurable via `%prefix`):
- `%startgame`: Starts an artillery match from the IDLE phase.
- `%join [emote]`: Joins the battlefield (supports custom Twitch emotes or defaults to built-in emotes).
- `%fire [angle] [power]`: Aims and fires a cannon shot (e.g. `%fire 45 60`). Defaults to last known angle/power if omitted.
- `%left`: Moves the tank left across the terrain.
- `%right`: Moves the tank right across the terrain.

### Streamer / Mod Configuration Commands:
- `%prefix <str>`: Sets the command prefix (supports multi-character prefixes).
- `%idlemessage on|off`: Toggles visibility of the idle waiting message and leaderboard.
- `%bouncywalls on|off`: Toggles bouncy screen walls (+10% bullet speed, +50% tank speed).
- `%roundtime <seconds>`: Configures the input phase timer duration (e.g. `%roundtime 15`).
- `%autoround <minutes>`: Configures automated round starts (`-1` for immediate, `> 0` for scheduled minutes, `0` or `off` to disable).
- `%terrain <min%> <max%>`: Configures vertical screen height percentage bounds for terrain generation (e.g. `%terrain 30 80` or `%terrain reset`).
- `%startperm <broadcaster|mod|vip|sub|all>`: Configures required role for `%startgame` (default: `broadcaster`).
- `%configperm <broadcaster|mod|vip|sub|all>`: Configures required role for settings commands (default: `broadcaster`).
- `%perm <start|config> <role>`: Unified permission management command.
- `%clearleaderboard` / `%resetleaderboard`: Clears all leaderboard statistics from SQLite and connected overlays.
- `%deleteplayer <user>` / `%removeplayer <user>`: Removes a specific player from SQLite leaderboard and connected overlays.
- `%minplayers <N>`: Configures minimum player count to fill with bots on match start (default: `5`, range `2`–`20`).
- `%botfill on|off`: Toggles automatic bot filling up to minimum players (default: `true`).
- `%botpoints <N>`: Configures leaderboard points awarded to human players for destroying a bot tank (default: `1`, range `0`–`10`).
- `%botlist <add|remove|list> [name]`: Manages the named bot pool in persistent SQLite database.

---

## 6. Build, Run, and Testing Guidelines

1. **One-Command Build & Test Pipeline**:
   ```cmd
   .\build.bat
   ```
   Runs TypeScript build (`npm run build:prod`), Go unit test suite (`go test -v ./...`), linter (`golangci-lint-v2 run ./...`), and binary compilation (`go build -o streamtanks.exe ./cmd/streamtanks`).
   - Optional flags:
     - `-FixDiff`: Inspects Go modernization diffs (`go fix -diff ./...`) without modifying files.
     - `-Fix`: Applies standard Go modernizations (`go fix ./...`).
     - `-SkipFrontend`, `-SkipTest`, `-SkipLint`: Skips respective pipeline stages.
2. **Running the Server**:
   ```bash
   go run ./cmd/streamtanks -channel <channel_name> -addr :8102
   # or with debug mode enabled:
   go run ./cmd/streamtanks -debug -addr :8102
   ```
3. **Overlay Access**:
   Open `http://localhost:8102` in a browser or add as an OBS Browser Source (Width: 1920, Height: 1080).
4. **Connection Resilience**:
   The frontend automatically detects server disconnects, hides the overlay transparently, polls the server, and triggers a full page reload when the server comes back online.
5. **Admin Console Access**:
   Open `http://localhost:8102/admin` for the dedicated command console, live match monitoring, and streamer management dashboard.

---

## 7. Twitch Extension & Command & Control (C&C) Architecture

### Architecture Overview
1. **Local Game Instance** (`cmd/streamtanks`):
   Runs on the streamer's local machine, manages physics, game loop, and the OBS browser source overlay. Connects outbound to the C&C server (`internal/app/cc_client.go`) over WebSockets.
2. **C&C Relay Server** (`cmd/ccserver`):
   Cloud-hosted gateway (`wss://st-cc.poundsigndesign.com`). Authenticates viewers using Twitch Extension JWT tokens, pairs broadcasters via claim handshake, and relays chat commands and real-time game state broadcasts.
3. **Twitch Extension Frontend** (`ext-web/`):
   Runs in the viewer's browser or mobile app. Connects to `wss://st-cc.poundsigndesign.com/ws/viewer` to send actions (`%join`, `%fire`, `%left`, `%right`) and render live aiming controls.

### Versioning Guidelines
- **StreamTanks Game & Server**: Tracks application semantic versioning (e.g. `v0.2.0`).
- **Twitch Extension Asset Release**: Versioned independently in the Twitch Developer Console (initial release: `v0.0.1`).

### Packaging & Twitch Review Requirements
- **One-Command Extension Package**:
  ```powershell
  npm run package:ext
  ```
  Packages `ext-web/public/*` into `dist/extension.zip`.
- **Zip Structure**: All extension files (`video_overlay.html`, `mobile.html`, `config.html`, `ext.js`, `ext.css`) must reside directly at the **root of the `.zip`** (no enclosing directory).
- **Twitch Review Human-Readability Requirement**: Twitch Extension review strictly forbids obfuscated code and requires readable JavaScript. Therefore, `npm run build` (without `--minify`) is used when bundling `ext.js` for extension review uploads.

