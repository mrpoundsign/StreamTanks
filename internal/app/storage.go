package app

import (
	"database/sql"
	"fmt"
	"log"
	"math"
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

