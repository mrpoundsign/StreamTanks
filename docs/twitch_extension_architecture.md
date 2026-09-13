# StreamTanks - Twitch Extension & C&C Server Architecture

## Goal
To provide a seamless, interactive UI directly over the Twitch video player, allowing viewers to aim, fire, and control their tanks without typing commands in chat or looking down at a mobile app. 

## The Core Challenge: Networking
When a viewer interacts with a web interface, their browser is on the outside internet. The streamer is running `StreamTanks.exe` inside their house, behind a router and firewall. Viewers cannot connect directly to the streamer's PC without the streamer setting up Port Forwarding (which is a massive security risk and support headache).

To solve this, we must introduce a **Command & Control (C&C) Server** to act as a public relay.

---

## Architecture Overview

The system consists of three distinct components:

### 1. Twitch Extension (The Frontend)
- A tiny web app (HTML/JS/CSS) hosted by Twitch and embedded as an invisible `<iframe>` over the video player.
- When viewers hover over the stream, the UI appears (D-pad, Fire button, Angle/Power sliders).
- **Authentication**: Twitch automatically passes a secure JWT (JSON Web Token) to this extension identifying the user (e.g., `mrpoundsign`). No login buttons or chat codes are required.
- **Action**: Connects to the C&C Server via WebSocket/HTTPS to send commands.

### 2. C&C Server / Extension Backend Service (The Relay)
- A lightweight Go server hosted on the public internet.
- **Validation**: Verifies the Twitch JWT using the Twitch Extension Secret to ensure commands are authentic.
- **Routing**: Manages WebSocket rooms for each active channel. Receives a command from the viewer and routes it down to the correct streamer's local instance.
- **Hosting / Security Warning**: *Do not host this raw on your home server.* Because the Twitch Extension connects directly to this server, viewers can see its IP address. To host at home safely, it must be put behind a proxy like a **Cloudflare Tunnel** to hide your home IP address. Otherwise, host it on a cheap cloud provider (DigitalOcean, Google Cloud Run).

### 3. StreamTanks Local (Game Host)
- The existing instance running on the streamer's PC (the OBS overlay).
- On launch, connects *out* to the C&C Server (e.g., `StreamTanks.exe -cc wss://api.streamtanks.com`).
- Receives commands from the C&C server (e.g., `{"user": "mrpoundsign", "action": "FIRE", "angle": 45}`) and injects them into the game state machine exactly as if they were typed in chat.
- Still supports native chat commands for viewers who don't want to use the overlay.

---

## The Request Flow

1. **Launch**: Streamer runs `StreamTanks.exe -cc wss://api.streamtanks.com`. It connects and registers itself as the host for channel `streamer_name`.
2. **Viewer Connects**: Viewer opens the stream. Twitch loads the overlay extension and provides a secure JWT identifying the viewer as `mrpoundsign`.
3. **Action**: Viewer drags the angle slider and clicks "Fire".
4. **Relay**: The Extension sends `(JWT, ACTION_FIRE, 45, 80)` to the C&C Server.
5. **Validation**: The C&C Server verifies the JWT using the Twitch Extension Secret.
6. **Delivery**: The C&C Server forwards the command down the open WebSocket to the streamer's local `StreamTanks.exe`.
7. **Execution**: The tank fires on the OBS overlay.

---

## Work Breakdown (Implementation Phases)

### Phase 1: C&C Server Foundation (The EBS)
- Create `cmd/ccserver/main.go`.
- Implement JWT validation using the Twitch Extension Secret.
- Implement WebSocket hub logic to route messages between viewers and hosts.

### Phase 2: StreamTanks Local Integration
- Add CLI flag `-cc-url`.
- Add a WebSocket client to `main.go` to connect to the C&C Server.
- Forward received actions from the C&C Server into the local `game.go` state machine, treating them as trusted inputs.

### Phase 3: Twitch Extension Frontend
- Create a new directory `ext/` for the Twitch Extension frontend code.
- Build the transparent overlay UI (gamepad controls).
- Implement the Twitch Extension Helper library (`window.Twitch.ext`) to grab the JWT.
- Connect to the C&C Server via WebSocket/HTTPS to send commands.

### Phase 4: Twitch Developer Console Setup
- Register the Extension on the Twitch Developer Console.
- Configure the Extension Backend Service (EBS) URL.
- Package the `ext/` directory into a zip and upload for testing/review.
