# 🚀 StreamTanks

**StreamTanks** is a lightweight, interactive Twitch chat artillery game and transparent browser overlay designed for OBS Studio and stream broadcasts. 

Viewers join the battlefield directly through your Twitch chat, command custom emote tanks, aim their cannons with angle and power, and blow chunks out of dynamic destructible terrain while competing for the stream leaderboard!

---

## 🎮 How It Works

1. **Wait for Players**: In the waiting phase, viewers type `%join` (or `%join <emote>`) to spawn their tank with their favorite Twitch emote.
2. **Start the Match**: The streamer or chat starts the match with `%startgame` (or rounds can start automatically via `%autoround`).
3. **Input Phase**: Viewers have a countdown window (default: 20s) to lock in their aim and shot power (e.g. `%fire 45 60`) or move left/right. An aiming protractor appears above each player's tank showing their aim.
4. **Action Phase**: All shots fire simultaneously! Cannon shells arc across the sky, exploding into destructible terrain and knocking out opponent tanks.
5. **Celebration**: The last surviving commander wins, earning a point on the persistent stream leaderboard, celebrated with an emote victory shower!

---

## ⚡ Quick Setup for Streamers

### 1. Download & Launch
Download the latest executable for your operating system from the **[Releases](https://github.com/mrpoundsign/StreamTanks/releases)** page.

Run StreamTanks (or double-click `StreamTanks.exe`):
```bash
# Windows
.\StreamTanks.exe

# Linux / macOS
./StreamTanks
```

*(Optional flags)*:
- `-addr :8102` — Customize the server port (default: `:8102`).
- `-debug` — Run in local testing mode with an on-screen command bar and target practice bot (no Twitch connection required).
- `-channel <name>` — Directly specify Twitch channel via CLI (optional alternative to the Admin Console).

### 2. Connect Your Channel (Recommended)
1. Open the **Commander Admin Console** at `http://localhost:8102/admin` in your browser.
2. In the top header, click **Set** next to **Channel: None (Local)**.
3. Enter your Twitch channel name (e.g. `mrpoundsign`) and click **Save**. You are given the `%claim <CODE>` command in an authorization banner to link your channel.
4. Type or paste `%claim <CODE>` into your Twitch chat as the broadcaster to authorize. StreamTanks permanently saves your channel and host credentials in SQLite!

### 3. Add to OBS Studio
1. In OBS Studio, add a new **Browser Source** to your scene.
2. Configure the Browser Source:
   - **URL**: `http://localhost:8102`
   - **Width**: `1920`
   - **Height**: `1080`
   - **Shutdown source when not visible**: Checked *(optional)*
   - **Refresh browser when scene becomes active**: Checked *(optional)*
3. The overlay background is fully transparent—your game or camera will show through clearly behind the terrain.

---

## 🏆 Scoring & Leaderboard System

Every match contributes to the persistent stream leaderboard stored in the embedded SQLite database:

- 🏆 **Match Victory (+5 Points)**: The last surviving player standing wins the round and earns **+5 points**. If all human tanks die and AI bots survive, "AI" takes the win and no points are awarded.
- 🎯 **Player Eliminations & 5% Bounty System**:
  - Eliminating a human opponent awards **1 base point** PLUS the victim's **5% bounty penalty** (rounded down: `victimScore / 20`).
  - *Example*: Destroying a rival who has 100 points deducts 5 points from their score and awards you **6 points** total (1 base + 5 bounty)! High-ranking players carry huge targets on their backs.
- 🤖 **AI Bot Kills**: Awards **1 point** (customizable from 0 to 10 points via `%botpoints <N>`). Bots carry 0 score, so no bounty is transferred.
- 🌋 **Crater Falls & Abyss Sinking**: Falling off the map or sinking into a deep crater destroys your tank and triggers the **5% point loss** penalty (bounty is lost to the abyss).
- 🛡️ **Tactical Energy Shield (`%shield`)**: Deploy a one-time emergency shield per match to absorb direct hits and crater blasts, protecting your tank and leaderboard score.
- 🛠️ **Moderation Commands**: Broadcasters can reset the entire leaderboard via `%clearleaderboard` or remove individual players via `%deleteplayer <user>`.

---

## 💬 Chat Command Reference

All commands default to the `%` prefix (customizable via `%prefix`).

### 🎖️ Viewer Commands
| Command | Arguments | Description | Example |
| :--- | :--- | :--- | :--- |
| `%join` | `[emote]` | Join the battlefield. Optionally specify an emote name or custom channel emote. | `%join Kappa` |
| `%icon` | `<emote>` | Change/update your tank's emote icon. Preferences persist across games. | `%icon PogChamp` |
| `%fire` | `<angle> <power>` | Locks in your shot angle (0° to 180°) and power (1 to 100). Defaults to last known values if omitted. | `%fire 60 75` |
| `%left` | *none* | Moves your tank left across the hills. | `%left` |
| `%right` | *none* | Moves your tank right across the hills. | `%right` |
| `%shield` | *none* | Activates your tactical one-time energy shield for the current match. | `%shield` |
| `%leave` | *none* | Leaves the active match or waiting lobby immediately. | `%leave` |
| `%startgame` | *none* | Starts a new artillery match from the waiting phase. | `%startgame` |

### 🛠️ Streamer & Mod Settings Commands
All settings automatically save to the local database and persist across restarts.

| Command | Arguments | Description | Example |
| :--- | :--- | :--- | :--- |
| `%channel` | `<name\|off>` | Sets the target Twitch channel, or switches to offline mode with `off`. | `%channel mrpoundsign` |
| `%claim` | `<code>` | Authorizes and links your local game host to your Twitch channel via one-time challenge code. | `%claim PU5HZX` |
| `%config` | `[on\|off]` | Toggles the live settings modal right in the middle of the screen. | `%config` |
| `%speed` | `<0.1 - 3.0>` | Adjusts physics and animation speed (default: `0.5x`). | `%speed 0.8` |
| `%commandtime` | `<seconds>` | Configures the input phase countdown duration (5s to 120s, default: `20s`). | `%commandtime 15` |
| `%autoround` | `<minutes\|-1\|off>` | Automates round starts: `-1` for immediate, `>0` for scheduled minutes, `off` to disable. | `%autoround 3` |
| `%idlemessage` | `on\|off` | Shows or hides the idle waiting HUD and leaderboard when waiting between matches. | `%idlemessage off` |
| `%bouncywalls` | `[on\|off]` | Enables bouncy screen walls with a +10% speed boost for bullets and +50% speed boost for tanks. | `%bouncywalls on` |
| `%terrain` | `<min%> <max%>` | Sets vertical screen height percentage bounds for terrain generation (`%terrain reset` to restore default 20% - 75%). | `%terrain 30 80` |
| `%terraincolor` | `<hex\|preset>` | Customizes terrain outline glow and ambient fill (`red`, `cyan`, `green`, `purple`, `orange`, `yellow`, `white`, or hex `#00ffcc`; `%terraincolor reset` for default). | `%terraincolor cyan` |
| `%clearleaderboard` | _none_ | Clears all player wins from SQLite database and live overlay leaderboard. | `%clearleaderboard` |
| `%kick` | `<user>` | Kicks a player from the active match immediately, despawning tank and projectiles. | `%kick troll123` |
| `%deleteplayer` | `<user>` | Removes an individual player from SQLite database, live leaderboard, and active match. | `%deleteplayer troll123` |
| `%minplayers` | `<2 - 20>` | Configures minimum player count to fill with bots on match start (default: `5`). | `%minplayers 6` |
| `%botfill` | `[on\|off]` | Toggles filling open slots with bots up to minimum players (default: `on`). | `%botfill off` |
| `%botpoints` | `<0 - 10>` | Points awarded to players for destroying a bot tank (default: `1`). | `%botpoints 2` |
| `%botlist` | `<add\|remove> <name>` | Manages the named bot pool in persistent database. | `%botlist add CyberDrone` |
| `%prefix` | `<symbol>` | Changes the command prefix for all commands. | `%prefix !` |

---

## 🎛️ Commander Admin Console (`/admin`)

StreamTanks provides a dedicated web control dashboard at `http://localhost:8102/admin`:
- **Live Match Monitor**: Current round phase, active players, and leaderboard rankings.
- **Interactive Command Console**: Always-visible terminal console with command history (Up/Down arrow keys) and quick-command pills.
- **One-Click Streamer Controls**: Quick buttons to start matches, toggle bouncy walls, toggle idle HUD, re-roll terrain, and manage players/leaderboard.

---

## 🛠️ Testing Locally (Debug Mode)

You can try StreamTanks locally without connecting to Twitch:
```bash
StreamTanks.exe -debug
```
Open `http://localhost:8102` in your browser. A floating debug command bar will appear at the bottom of the screen, spawning your local player and a target practice bot so you can test firing and configuration commands immediately.

---

## 📄 License
StreamTanks is open source under the [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE).
