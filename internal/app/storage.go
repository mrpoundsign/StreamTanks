package app

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

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

	return nil
}

func closeDB() {
	if db != nil {
		_ = db.Close()
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
				}
			}
		}
	}
	gameState.BotList = loadBotList()
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



