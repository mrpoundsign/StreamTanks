package app

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDB(dataSourceName string) error {
	var err error
	db, err = sql.Open("sqlite", dataSourceName)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS leaderboard (username TEXT PRIMARY KEY, wins INTEGER)`)
	if err != nil {
		_ = db.Close()
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		_ = db.Close()
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS bot_list (username TEXT PRIMARY KEY)`)
	if err != nil {
		_ = db.Close()
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS player_emotes (username TEXT PRIMARY KEY, emote TEXT, emote_url TEXT)`)
	if err != nil {
		_ = db.Close()
		return err
	}

	loadPlayerEmotes()

	return nil
}

func closeDB() {
	if db != nil {
		_ = db.Close()
		db = nil
	}
}

func loadLeaderboard() {
	if db == nil {
		return
	}
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	rows, err := db.Query(`SELECT username, wins FROM leaderboard`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			var wins int
			if err := rows.Scan(&name, &wins); err == nil {
				gameState.Leaderboard[name] = wins
			}
		}
	}
}

func incrementWin(username string) {
	addScore(username, 1)
}

func addScore(username string, points int) {
	if db == nil || points <= 0 {
		return
	}
	_, err := db.Exec(`INSERT INTO leaderboard (username, wins) VALUES (?, ?) ON CONFLICT(username) DO UPDATE SET wins = wins + excluded.wins`, username, points)
	if err != nil {
		log.Println("DB addScore error:", err)
	}
}

func deductScore(username string, points int) {
	if db == nil || points <= 0 {
		return
	}
	_, err := db.Exec(`UPDATE leaderboard SET wins = MAX(0, wins - ?) WHERE LOWER(username) = LOWER(?)`, points, username)
	if err != nil {
		log.Println("DB deductScore error:", err)
	}
}

func loadSettings() {
	if db == nil {
		return
	}
	gameState.mu.Lock()
	defer gameState.mu.Unlock()

	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err == nil {
				switch k {
				case "channel":
					clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(v, "#")))
					if clean != "" {
						gameState.Channel = clean
					}
				case "prefix":
					if v != "" {
						gameState.Prefix = v
					}
				case "physics_speed":
					var spd float64
					if _, err := fmt.Sscanf(v, "%f", &spd); err == nil && spd >= 0.1 && spd <= 3.0 {
						gameState.PhysicsSpeed = spd
					}
				case "command_time":
					var dur int
					if _, err := fmt.Sscanf(v, "%d", &dur); err == nil && dur >= 5 && dur <= 120 {
						gameState.InputDuration = dur
					}
				case "auto_round":
					var ar int
					if _, err := fmt.Sscanf(v, "%d", &ar); err == nil && (ar >= -1 && ar <= 60) {
						gameState.AutoRound = ar
					}
				case "idle_message":
					switch v {
					case "0", "false", "off":
						gameState.IdleMessage = false
					case "1", "true", "on":
						gameState.IdleMessage = true
					}
				case "bouncy_walls":
					switch v {
					case "0", "false", "off":
						gameState.BouncyWalls = false
					case "1", "true", "on":
						gameState.BouncyWalls = true
					}
				case "terrain_climb":
					switch v {
					case "0", "false", "off":
						gameState.TerrainClimb = false
					case "1", "true", "on":
						gameState.TerrainClimb = true
					}
				case "terrain_min":
					var tMin int
					if _, err := fmt.Sscanf(v, "%d", &tMin); err == nil && tMin >= 10 && tMin <= 80 {
						gameState.TerrainMin = tMin
					}
				case "terrain_max":
					var tMax int
					if _, err := fmt.Sscanf(v, "%d", &tMax); err == nil && tMax >= 20 && tMax <= 90 {
						gameState.TerrainMax = tMax
					}
				case "terrain_color":
					if col, ok := parseColor(v); ok {
						gameState.TerrainColor = col
					}
				case "tank_color":
					if col, ok := parseColor(v); ok {
						gameState.TankColor = col
					}
				case "start_perm":
					clean := strings.ToLower(v)
					if clean == "broadcaster" || clean == "mod" || clean == "vip" || clean == "sub" || clean == "all" {
						gameState.StartPerm = clean
					}
				case "config_perm":
					clean := strings.ToLower(v)
					if clean == "broadcaster" || clean == "mod" || clean == "vip" || clean == "sub" || clean == "all" {
						gameState.ConfigPerm = clean
					}
				case "min_players":
					var mp int
					if _, err := fmt.Sscanf(v, "%d", &mp); err == nil && mp >= 2 && mp <= 20 {
						gameState.MinPlayers = mp
					}
				case "bot_fill":
					switch v {
					case "0", "false", "off":
						gameState.BotFill = false
					case "1", "true", "on":
						gameState.BotFill = true
					}
				case "bot_points":
					var bp int
					if _, err := fmt.Sscanf(v, "%d", &bp); err == nil && bp >= 0 && bp <= 10 {
						gameState.BotPoints = bp
					}
				case "cc_enabled":
					switch v {
					case "0", "false", "off":
						gameState.CCEnabled = false
					case "1", "true", "on":
						gameState.CCEnabled = true
					}
				case "cc_url":
					if v != "" {
						gameState.CCServerURL = v
					}
				case "protractor_x":
					var px int
					if _, err := fmt.Sscanf(v, "%d", &px); err == nil && px >= 100 && px <= 1820 {
						gameState.ProtractorX = px
					}
				case "protractor_y":
					var py int
					if _, err := fmt.Sscanf(v, "%d", &py); err == nil && py >= 100 && py <= 1000 {
						gameState.ProtractorY = py
					}
				case "leaderboard_x":
					var lx int
					if _, err := fmt.Sscanf(v, "%d", &lx); err == nil && lx >= 0 && lx <= 1820 {
						gameState.LeaderboardX = lx
					}
				case "leaderboard_y":
					var ly int
					if _, err := fmt.Sscanf(v, "%d", &ly); err == nil && ly >= 0 && ly <= 1000 {
						gameState.LeaderboardY = ly
					}
				case "leaderboard_scale":
					var ls float64
					if _, err := fmt.Sscanf(v, "%f", &ls); err == nil && ls >= 0.5 && ls <= 2.0 {
						gameState.LeaderboardScale = ls
					}
				}
			}
		}
	}
	if gameState.CCServerURL == "" {
		gameState.CCServerURL = defaultCCServerURL
	}
	if gameState.CCStatus == "" {
		gameState.CCStatus = "disconnected"
	}
	if gameState.ProtractorX == 0 {
		gameState.ProtractorX = 250
	}
	tMax := gameState.TerrainMax
	if tMax == 0 {
		tMax = 75
	}
	maxProtractorY := max(int(math.Floor(float64(defaultTerrainHeight)*(1.0-float64(tMax)/100.0))), 100)
	if gameState.ProtractorY == 0 {
		gameState.ProtractorY = 350
	}
	if gameState.ProtractorY > maxProtractorY {
		gameState.ProtractorY = maxProtractorY
	}
	if gameState.TerrainColor == "" {
		gameState.TerrainColor = defaultTerrainColor
	}
	if gameState.TankColor == "" {
		gameState.TankColor = defaultTankColor
	}
	if gameState.LeaderboardScale < 0.5 || gameState.LeaderboardScale > 2.0 {
		gameState.LeaderboardScale = 1.0
	}
	gameState.BotList = loadBotList()
	if gameState.Channel != "" {
		channelName = gameState.Channel
	}
}

func getSetting(key string) string {
	if db == nil {
		return ""
	}
	var val string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	if err != nil {
		return ""
	}
	return val
}

func saveSetting(key, value string) {
	if db == nil {
		return
	}
	_, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		log.Println("DB saveSetting error:", err)
	}
}

func deleteSetting(key string) {
	if db == nil {
		return
	}
	_, err := db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	if err != nil {
		log.Println("DB deleteSetting error:", err)
	}
}

func clearLeaderboardDB() {
	if db == nil {
		return
	}
	_, err := db.Exec(`DELETE FROM leaderboard`)
	if err != nil {
		log.Println("DB clearLeaderboard error:", err)
	}
}

func deletePlayerDB(username string) {
	if db == nil {
		return
	}
	deletePlayerEmote(username)
	_, err := db.Exec(`DELETE FROM leaderboard WHERE LOWER(username) = LOWER(?)`, username)
	if err != nil {
		log.Println("DB deletePlayer error:", err)
	}
}

func loadBotList() []string {
	if db == nil {
		return defaultBotList
	}
	rows, err := db.Query(`SELECT username FROM bot_list`)
	if err != nil {
		return defaultBotList
	}
	defer func() { _ = rows.Close() }()

	var bots []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil && name != "" {
			bots = append(bots, name)
		}
	}
	if len(bots) == 0 {
		for _, b := range defaultBotList {
			addBotToList(b)
		}
		return defaultBotList
	}
	return bots
}

func addBotToList(username string) {
	if db == nil || username == "" {
		return
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO bot_list (username) VALUES (?)`, username)
	if err != nil {
		log.Println("DB addBotToList error:", err)
	}
}

func removeBotFromList(username string) {
	if db == nil || username == "" {
		return
	}
	_, err := db.Exec(`DELETE FROM bot_list WHERE LOWER(username) = LOWER(?)`, username)
	if err != nil {
		log.Println("DB removeBotFromList error:", err)
	}
}

type savedEmote struct {
	Emote    string
	EmoteURL string
}

var (
	playerEmotesMu    sync.RWMutex
	playerEmotesCache = make(map[string]savedEmote)
)

func loadPlayerEmotes() {
	if db == nil {
		return
	}
	rows, err := db.Query(`SELECT username, emote, emote_url FROM player_emotes`)
	if err != nil {
		log.Println("DB loadPlayerEmotes error:", err)
		return
	}
	defer func() { _ = rows.Close() }()

	playerEmotesMu.Lock()
	defer playerEmotesMu.Unlock()
	for rows.Next() {
		var user, emote, emoteURL string
		if err := rows.Scan(&user, &emote, &emoteURL); err == nil {
			playerEmotesCache[strings.ToLower(user)] = savedEmote{
				Emote:    emote,
				EmoteURL: emoteURL,
			}
		}
	}
}

func savePlayerEmote(username, emote, emoteURL string) {
	if username == "" || emote == "" {
		return
	}
	cleanUser := strings.ToLower(username)

	playerEmotesMu.Lock()
	playerEmotesCache[cleanUser] = savedEmote{
		Emote:    emote,
		EmoteURL: emoteURL,
	}
	playerEmotesMu.Unlock()

	if db == nil {
		return
	}
	_, err := db.Exec(`INSERT INTO player_emotes (username, emote, emote_url) VALUES (?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET emote = excluded.emote, emote_url = excluded.emote_url`,
		cleanUser, emote, emoteURL)
	if err != nil {
		log.Println("DB savePlayerEmote error:", err)
	}
}

func getPlayerEmote(username string) (string, string, bool) {
	cleanUser := strings.ToLower(username)
	playerEmotesMu.RLock()
	se, ok := playerEmotesCache[cleanUser]
	playerEmotesMu.RUnlock()
	if ok && se.Emote != "" {
		return se.Emote, se.EmoteURL, true
	}
	return "", "", false
}

func deletePlayerEmote(username string) {
	if username == "" {
		return
	}
	cleanUser := strings.ToLower(username)
	playerEmotesMu.Lock()
	delete(playerEmotesCache, cleanUser)
	playerEmotesMu.Unlock()

	if db == nil {
		return
	}
	_, err := db.Exec(`DELETE FROM player_emotes WHERE LOWER(username) = ?`, cleanUser)
	if err != nil {
		log.Println("DB deletePlayerEmote error:", err)
	}
}

var colorPresets = map[string]string{
	"default": "#ff003c",
	"reset":   "#ff003c",
	"red":     "#ff003c",
	"cyan":    "#00ffcc",
	"green":   "#00ff66",
	"purple":  "#bf00ff",
	"orange":  "#ff6600",
	"yellow":  "#ffd700",
	"white":   "#ffffff",
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func parseColor(input string) (string, bool) {
	raw := strings.ToLower(strings.TrimSpace(input))
	if raw == "" {
		return "", false
	}
	if hexVal, ok := colorPresets[raw]; ok {
		return hexVal, true
	}

	trimmed := strings.TrimPrefix(raw, "#")
	if len(trimmed) == 3 {
		for i := range 3 {
			if !isHexDigit(trimmed[i]) {
				return "", false
			}
		}
		r, g, b := trimmed[0], trimmed[1], trimmed[2]
		return fmt.Sprintf("#%c%c%c%c%c%c", r, r, g, g, b, b), true
	} else if len(trimmed) == 6 {
		for i := range 6 {
			if !isHexDigit(trimmed[i]) {
				return "", false
			}
		}
		return "#" + trimmed, true
	}

	return "", false
}

func applySettingsUpdate(update SettingsUpdate) {
	var (
		terrainRegen           bool
		botChannelToSet        *string
		shouldCancelAutoRound  bool
		shouldTriggerAutoRound bool
		shouldStartCC          bool
		shouldStopCC           bool
		shouldResetCC          bool
		ccTargetChannel        string
	)

	gameState.mu.Lock()

	if update.Channel != nil {
		clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(*update.Channel, "#")))
		if clean == "off" || clean == "clear" || clean == "none" || clean == "0" {
			clean = ""
		}
		gameState.Channel = clean
		channelName = clean
		if !update.NoSave {
			if clean != "" {
				saveSetting("channel", clean)
			} else {
				deleteSetting("channel")
			}
		}
		botChannelToSet = &clean
		if clean != "" {
			shouldStartCC = true
			ccTargetChannel = clean
		} else {
			shouldStopCC = true
		}
	}

	if update.Prefix != nil {
		clean := strings.TrimSpace(*update.Prefix)
		if clean != "" {
			gameState.Prefix = clean
			if !update.NoSave {
				saveSetting("prefix", clean)
			}
		}
	}

	if update.PhysicsSpeed != nil {
		spd := *update.PhysicsSpeed
		if spd < 0.1 {
			spd = 0.1
		} else if spd > 3.0 {
			spd = 3.0
		}
		gameState.PhysicsSpeed = spd
		if !update.NoSave {
			saveSetting("physics_speed", fmt.Sprintf("%.2f", spd))
		}
	}

	if update.CommandTime != nil {
		dur := *update.CommandTime
		if dur < 5 {
			dur = 5
		} else if dur > 120 {
			dur = 120
		}
		gameState.InputDuration = dur
		if !update.NoSave {
			saveSetting("command_time", strconv.Itoa(dur))
		}
	}

	if update.AutoRound != nil {
		ar := *update.AutoRound
		if ar < -1 {
			ar = 0
		} else if ar > 60 {
			ar = 60
		}
		gameState.AutoRound = ar
		if !update.NoSave {
			saveSetting("auto_round", strconv.Itoa(ar))
		}
		if ar == 0 {
			shouldCancelAutoRound = true
		} else if gameState.Phase == phaseIdle {
			shouldTriggerAutoRound = true
		}
	}

	if update.IdleMessage != nil {
		val := *update.IdleMessage
		gameState.IdleMessage = val
		if !update.NoSave {
			dbVal := "0"
			if val {
				dbVal = "1"
			}
			saveSetting("idle_message", dbVal)
		}
	}

	if update.BouncyWalls != nil {
		val := *update.BouncyWalls
		gameState.BouncyWalls = val
		if !update.NoSave {
			dbVal := "0"
			if val {
				dbVal = "1"
			}
			saveSetting("bouncy_walls", dbVal)
		}
	}

	if update.TerrainClimb != nil {
		val := *update.TerrainClimb
		gameState.TerrainClimb = val
		if !update.NoSave {
			dbVal := "0"
			if val {
				dbVal = "1"
			}
			saveSetting("terrain_climb", dbVal)
		}
	}

	if update.TerrainMin != nil || update.TerrainMax != nil {
		tMin := gameState.TerrainMin
		tMax := gameState.TerrainMax
		if update.TerrainMin != nil {
			tMin = *update.TerrainMin
		}
		if update.TerrainMax != nil {
			tMax = *update.TerrainMax
		}
		if tMin < 10 {
			tMin = 10
		}
		if tMax > 90 {
			tMax = 90
		}
		if tMin > tMax-10 {
			tMin = tMax - 10
		}
		gameState.TerrainMin = tMin
		gameState.TerrainMax = tMax
		if !update.NoSave {
			saveSetting("terrain_min", strconv.Itoa(tMin))
			saveSetting("terrain_max", strconv.Itoa(tMax))
		}
		maxY := max(int(math.Floor(float64(defaultTerrainHeight)*(1.0-float64(tMax)/100.0))), 100)
		if gameState.ProtractorY > maxY {
			gameState.ProtractorY = maxY
			if !update.NoSave {
				saveSetting("protractor_y", strconv.Itoa(maxY))
			}
		}
		if gameState.Phase == phaseIdle {
			gameState.Terrain = generateTerrain(tMin, tMax)
			for _, p := range gameState.Players {
				p.Y = getTerrainHeight(gameState.Terrain, p.X)
			}
			terrainRegen = true
		}
	}

	if update.TerrainReroll {
		if gameState.Phase == phaseIdle {
			gameState.Terrain = generateTerrain(gameState.TerrainMin, gameState.TerrainMax)
			for _, p := range gameState.Players {
				p.Y = getTerrainHeight(gameState.Terrain, p.X)
			}
			terrainRegen = true
		}
	}

	if update.TerrainColor != nil {
		if col, ok := parseColor(*update.TerrainColor); ok {
			gameState.TerrainColor = col
			if !update.NoSave {
				saveSetting("terrain_color", col)
			}
		}
	}

	if update.TankColor != nil {
		if col, ok := parseColor(*update.TankColor); ok {
			gameState.TankColor = col
			if !update.NoSave {
				saveSetting("tank_color", col)
			}
		}
	}

	if update.StartPerm != nil {
		clean := strings.ToLower(*update.StartPerm)
		if clean == "broadcaster" || clean == "mod" || clean == "vip" || clean == "sub" || clean == "all" {
			gameState.StartPerm = clean
			if !update.NoSave {
				saveSetting("start_perm", clean)
			}
		}
	}

	if update.ConfigPerm != nil {
		clean := strings.ToLower(*update.ConfigPerm)
		if clean == "broadcaster" || clean == "mod" || clean == "vip" || clean == "sub" || clean == "all" {
			gameState.ConfigPerm = clean
			if !update.NoSave {
				saveSetting("config_perm", clean)
			}
		}
	}

	if update.MinPlayers != nil {
		mp := *update.MinPlayers
		if mp < 2 {
			mp = 2
		} else if mp > 20 {
			mp = 20
		}
		gameState.MinPlayers = mp
		if !update.NoSave {
			saveSetting("min_players", strconv.Itoa(mp))
		}
	}

	if update.BotFill != nil {
		val := *update.BotFill
		gameState.BotFill = val
		if !update.NoSave {
			dbVal := "0"
			if val {
				dbVal = "1"
			}
			saveSetting("bot_fill", dbVal)
		}
	}

	if update.BotPoints != nil {
		bp := *update.BotPoints
		if bp < 0 {
			bp = 0
		} else if bp > 10 {
			bp = 10
		}
		gameState.BotPoints = bp
		if !update.NoSave {
			saveSetting("bot_points", strconv.Itoa(bp))
		}
	}

	if update.CCEnabled != nil {
		newVal := *update.CCEnabled
		gameState.CCEnabled = newVal
		if !update.NoSave {
			dbVal := "0"
			if newVal {
				dbVal = "1"
			}
			saveSetting("cc_enabled", dbVal)
		}
		if newVal {
			shouldStartCC = true
			ccTargetChannel = channelName
		} else {
			shouldStopCC = true
		}
	}

	if update.CCServerURL != nil {
		newURL := strings.TrimSpace(*update.CCServerURL)
		if newURL != "" {
			gameState.CCServerURL = newURL
			if !update.NoSave {
				saveSetting("cc_url", newURL)
			}
			if gameState.CCEnabled {
				shouldStartCC = true
				ccTargetChannel = channelName
			}
		}
	}

	if update.ProtractorX != nil || update.ProtractorY != nil {
		if update.ProtractorX != nil {
			px := *update.ProtractorX
			if px < 100 {
				px = 100
			} else if px > 1820 {
				px = 1820
			}
			gameState.ProtractorX = px
			if !update.NoSave {
				saveSetting("protractor_x", strconv.Itoa(px))
			}
		}
		if update.ProtractorY != nil {
			py := *update.ProtractorY
			maxY := max(int(math.Floor(float64(defaultTerrainHeight)*(1.0-float64(gameState.TerrainMax)/100.0))), 100)
			if py < 100 {
				py = 100
			} else if py > maxY {
				py = maxY
			}
			gameState.ProtractorY = py
			if !update.NoSave {
				saveSetting("protractor_y", strconv.Itoa(py))
			}
		}
	}

	if update.LeaderboardX != nil {
		lx := *update.LeaderboardX
		if lx < 0 {
			lx = 0
		} else if lx > 1820 {
			lx = 1820
		}
		gameState.LeaderboardX = lx
		if !update.NoSave {
			saveSetting("leaderboard_x", strconv.Itoa(lx))
		}
	}

	if update.LeaderboardY != nil {
		ly := *update.LeaderboardY
		if ly < 0 {
			ly = 0
		} else if ly > 1000 {
			ly = 1000
		}
		gameState.LeaderboardY = ly
		if !update.NoSave {
			saveSetting("leaderboard_y", strconv.Itoa(ly))
		}
	}

	if update.LeaderboardScale != nil {
		ls := *update.LeaderboardScale
		if ls < 0.5 {
			ls = 0.5
		} else if ls > 2.0 {
			ls = 2.0
		}
		gameState.LeaderboardScale = ls
		if !update.NoSave {
			saveSetting("leaderboard_scale", fmt.Sprintf("%.2f", ls))
		}
	}

	if update.ClearLeaderboard {
		gameState.Leaderboard = make(map[string]int)
		clearLeaderboardDB()
	}

	if update.DeletePlayer != "" {
		target := strings.TrimPrefix(update.DeletePlayer, "@")
		for key := range gameState.Leaderboard {
			if strings.EqualFold(key, target) {
				delete(gameState.Leaderboard, key)
			}
		}
		var playerKey string
		for k := range gameState.Players {
			if strings.EqualFold(k, target) {
				playerKey = k
				break
			}
		}
		if playerKey != "" {
			removePlayerFromMatchLocked(playerKey)
		}
		deletePlayerDB(target)
	}

	if update.AddBot != "" {
		botName := strings.TrimSpace(strings.TrimPrefix(update.AddBot, "@"))
		if botName != "" {
			alreadyExists := false
			for _, b := range gameState.BotList {
				if strings.EqualFold(b, botName) {
					alreadyExists = true
					break
				}
			}
			if !alreadyExists {
				gameState.BotList = append(gameState.BotList, botName)
				addBotToList(botName)
			}
		}
	}

	if update.RemoveBot != "" {
		botName := strings.TrimSpace(strings.TrimPrefix(update.RemoveBot, "@"))
		if botName != "" {
			updated := make([]string, 0, len(gameState.BotList))
			for _, b := range gameState.BotList {
				if !strings.EqualFold(b, botName) {
					updated = append(updated, b)
				}
			}
			gameState.BotList = updated
			removeBotFromList(botName)
		}
	}

	if update.ResetCCKey {
		shouldResetCC = true
		ccTargetChannel = channelName
	}

	gameState.mu.Unlock()

	// External side effects outside gameState.mu to prevent deadlock
	if botChannelToSet != nil {
		setTwitchBotChannel(*botChannelToSet)
	}
	if shouldCancelAutoRound {
		cancelAutoRoundTimer()
	}
	if shouldTriggerAutoRound {
		triggerAutoRound()
	}
	switch {
	case shouldResetCC:
		ResetCCHostToken(ccTargetChannel)
	case shouldStartCC:
		StartCCClientManager(ccTargetChannel)
	case shouldStopCC:
		StopCCClient()
	}

	if terrainRegen {
		broadcast(msgStateUpdate, &gameState)
		broadcast(msgResetTerrain, nil)
	} else {
		broadcast(msgStateUpdate, &gameState)
	}
	BroadcastViewerState()
}

