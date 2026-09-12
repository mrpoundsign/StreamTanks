# 🚀 StreamTanks

**StreamTanks** is a lightweight, interactive Twitch chat artillery game and transparent browser overlay designed for OBS Studio and stream broadcasts. 

Viewers join the battlefield directly through your Twitch chat, command custom neon tanks, aim their cannons with angle and power, and blow chunks out of dynamic destructible terrain while competing for the stream leaderboard!

---

## 🎮 How It Works

1. **Wait for Players**: In the waiting phase, viewers type `%join` (or `%join <emote>`) to spawn their tank with their favorite Twitch emote.
2. **Start the Match**: The streamer or chat starts the match with `%startgame` (or rounds can start automatically via `%autoround`).
3. **Input Phase**: Viewers have a countdown window (default: 20s) to lock in their aim and shot power (e.g. `%fire 45 60`) or move left/right. An aiming protractor appears above each player's tank showing their aim.
4. **Action Phase**: All shots fire simultaneously! Cannon shells arc across the sky, exploding into destructible terrain and knocking out opponent tanks.
5. **Celebration**: The last surviving commander wins, earning a point on the persistent stream leaderboard, celebrated with an emote victory shower!

---

## ⚡ Quick Setup for Streamers

### 1. Download & Run
Download the latest executable for your operating system from the **[Releases](https://github.com/mrpoundsign/StreamTanks/releases)** page.

Run StreamTanks from your terminal or command prompt:
```bash
# Windows
StreamTanks.exe -channel your_channel_name

# Linux / macOS
./StreamTanks -channel your_channel_name
```

*(Optional flags)*:
- `-addr :8102` — Customize the server port (default: `:8102`).
- `-debug` — Run in local testing mode with an on-screen command bar and target practice bot (no Twitch connection required).

### 2. Add to OBS Studio
1. In OBS Studio, add a new **Browser Source** to your scene.
2. Configure the Browser Source:
   - **URL**: `http://localhost:8102`
   - **Width**: `1920`
   - **Height**: `1080`
   - **Shutdown source when not visible**: Checked *(optional)*
   - **Refresh browser when scene becomes active**: Checked *(optional)*
3. The overlay background is fully transparent—your game or camera will show through clearly behind the terrain.

---

## 💬 Chat Command Reference

All commands default to the `%` prefix (customizable via `%prefix`).

### 🎖️ Viewer Commands
| Command | Arguments | Description | Example |
| :--- | :--- | :--- | :--- |
| `%join` | `[emote]` | Join the battlefield. Optionally specify an emote name or custom channel emote. | `%join Kappa` |
| `%startgame` | *none* | Starts a new artillery match from the waiting phase. | `%startgame` |
| `%fire` | `<angle> <power>` | Locks in your shot angle (0° to 180°) and power (1 to 100). Defaults to last known values if omitted. | `%fire 60 75` |
| `%left` | *none* | Moves your tank left across the hills. | `%left` |
| `%right` | *none* | Moves your tank right across the hills. | `%right` |

### 🛠️ Streamer & Mod Settings Commands
All settings automatically save to the local database and persist across restarts.

| Command | Arguments | Description | Example |
| :--- | :--- | :--- | :--- |
| `%config` | `[on\|off]` | Toggles the live settings modal right in the middle of the screen. | `%config` |
| `%speed` | `<0.1 - 3.0>` | Adjusts physics and animation speed (default: `0.5x`). | `%speed 0.8` |
| `%commandtime` | `<seconds>` | Configures the input phase countdown duration (5s to 120s, default: `20s`). | `%commandtime 15` |
| `%autoround` | `<minutes\|-1\|off>` | Automates round starts: `-1` for immediate, `>0` for scheduled minutes, `off` to disable. | `%autoround 3` |
| `%idlemessage` | `on\|off` | Shows or hides the idle waiting HUD and leaderboard when waiting between matches. | `%idlemessage off` |
| `%bouncywalls` | `[on\|off]` | Enables bouncy screen walls with a +10% speed boost for bullets and +50% speed boost for tanks. | `%bouncywalls on` |
| `%prefix` | `<symbol>` | Changes the command prefix for all commands. | `%prefix !` |

---

## 🛠️ Testing Locally (Debug Mode)

You can try StreamTanks locally without connecting to Twitch:
```bash
StreamTanks.exe -debug
```
Open `http://localhost:8102` in your browser. A floating debug command bar will appear at the bottom of the screen, spawning your local player and a target practice bot so you can test firing and configuration commands immediately.

---

## 📄 License
StreamTanks is open source under the [MIT License](LICENSE).
