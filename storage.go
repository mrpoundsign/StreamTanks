package main

import (
	"database/sql"
	"fmt"
	"log"

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
	if db == nil {
		return
	}
	_, err := db.Exec(`INSERT INTO leaderboard (username, wins) VALUES (?, 1) ON CONFLICT(username) DO UPDATE SET wins = wins + 1`, username)
	if err != nil {
		log.Println("DB error:", err)
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
				}
			}
		}
	}
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
