package app

import (
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/gempir/go-twitch-irc/v4"
)

func processCommand(username string, msg string, emotes []*twitch.Emote, userOpt ...*twitch.User) {
	var user *twitch.User
	if len(userOpt) > 0 {
		user = userOpt[0]
	}

	gameState.mu.Lock()

	currPrefix := gameState.Prefix
	if currPrefix == "" {
		currPrefix = "%"
	}

	trimmedMsg := strings.TrimSpace(msg)
	var cmdStr string
	isCommand := true
	switch {
	case strings.HasPrefix(trimmedMsg, currPrefix):
		cmdStr = strings.TrimPrefix(trimmedMsg, currPrefix)
	case strings.HasPrefix(trimmedMsg, "%"):
		cmdStr = strings.TrimPrefix(trimmedMsg, "%")
	case strings.HasPrefix(trimmedMsg, "!"):
		cmdStr = strings.TrimPrefix(trimmedMsg, "!")
	default:
		isCommand = false
	}

	if !isCommand {
		// In IDLE phase, regular chatters spawn as ambient roamers (Joined = false)
		if gameState.Phase == phaseIdle {
			if _, exists := gameState.Players[username]; !exists {
				spawnNewPlayerLocked(username, false)
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)
				return
			}
		}
		gameState.mu.Unlock()
		return
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		gameState.mu.Unlock()
		return
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "channel":
		handleChannelCmd(user, parts)
	case "prefix":
		handlePrefixCmd(user, parts)
	case "speed", "physicsspeed":
		handleSpeedCmd(user, parts)
	case "config", "settings":
		handleConfigCmd(user, parts)
	case "cc":
		handleCCCommand(user, parts[1:])
	case "commandtime", "roundtime":
		handleCommandTimeCmd(user, parts)
	case "autoround":
		handleAutoRoundCmd(user, parts)
	case "idlemessage":
		handleIdleMessageCmd(user, parts)
	case "bouncywalls", "bouncy":
		handleBouncyWallsCmd(user, parts)
	case "terrain":
		handleTerrainCmd(user, parts)
	case "startperm":
		handleStartPermCmd(user, parts)
	case "configperm":
		handleConfigPermCmd(user, parts)
	case "perm", "perms", "permission", "permissions":
		handlePermCmd(user, parts)
	case "clearleaderboard", "resetleaderboard":
		handleClearLeaderboardCmd(user)
	case "kick":
		handleKickCmd(user, parts)
	case "deleteplayer", "removeplayer":
		handleDeletePlayerCmd(user, parts)
	case "join":
		handleJoinCmd(user, parts, username, emotes)
	case "leave":
		handleLeaveCmd(username)
	case "minplayers":
		handleMinPlayersCmd(user, parts)
	case "botfill":
		handleBotFillCmd(user, parts)
	case "botpoints":
		handleBotPointsCmd(user, parts)
	case "botlist":
		handleBotListCmd(user, parts)
	case "protractor", "uipos":
		handleProtractorCmd(user, parts)
	case "startgame", "start":
		handleStartGameCmd(user)
	case "shield":
		handleShieldCmd(username)
	case "fire", "left", "right":
		handleFireCmd(parts, username, cmd)
	default:
		gameState.mu.Unlock()
	}
}

func handleChannelCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		newChannel := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(parts[1], "#")))
		if newChannel == "off" || newChannel == "clear" || newChannel == "none" || newChannel == "0" {
			newChannel = ""
		}
		gameState.Channel = newChannel
		channelName = newChannel
		gameState.mu.Unlock()

		if newChannel != "" {
			log.Printf("Twitch channel set to: %s", newChannel)
			saveSetting("channel", newChannel)
		} else {
			log.Println("Twitch channel cleared; running in local overlay mode")
			deleteSetting("channel")
		}

		setTwitchBotChannel(newChannel)

		if newChannel != "" {
			StartCCClientManager(newChannel)
		} else {
			StopCCClient()
		}

		broadcast(msgStateUpdate, &gameState)
		return
	}
	gameState.mu.Unlock()
}

func handlePrefixCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		newPrefix := parts[1]
		gameState.Prefix = newPrefix
		gameState.mu.Unlock()
		saveSetting("prefix", newPrefix)
		broadcast(msgStateUpdate, &gameState)
		return
	}
	gameState.mu.Unlock()
}

func handleSpeedCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		var spd float64
		if _, err := fmt.Sscanf(parts[1], "%f", &spd); err == nil {
			if spd < 0.1 {
				spd = 0.1
			} else if spd > 3.0 {
				spd = 3.0
			}
			gameState.PhysicsSpeed = spd
			gameState.mu.Unlock()
			saveSetting("physics_speed", fmt.Sprintf("%.2f", spd))
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handleConfigCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 && strings.EqualFold(parts[1], "cc") {
		handleCCCommand(user, parts[2:])
		return
	}
	if len(parts) > 1 {
		arg := strings.ToLower(parts[1])
		gameState.ShowConfig = parseBoolArg(arg)
	} else {
		// Toggle config modal
		gameState.ShowConfig = !gameState.ShowConfig
	}
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)
}

func handleCCCommand(user *twitch.User, args []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}

	if len(args) == 0 {
		newVal := !gameState.CCEnabled
		gameState.CCEnabled = newVal
		gameState.mu.Unlock()
		dbVal := "0"
		if newVal {
			dbVal = "1"
			saveSetting("cc_enabled", dbVal)
			StartCCClientManager(channelName)
		} else {
			saveSetting("cc_enabled", dbVal)
			StopCCClient()
		}
		broadcast(msgStateUpdate, &gameState)
		return
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "on", "enable", "true", "1":
		gameState.CCEnabled = true
		gameState.mu.Unlock()
		saveSetting("cc_enabled", "1")
		StartCCClientManager(channelName)
		broadcast(msgStateUpdate, &gameState)
		return

	case "off", "disable", "false", "0":
		gameState.CCEnabled = false
		gameState.mu.Unlock()
		saveSetting("cc_enabled", "0")
		StopCCClient()
		broadcast(msgStateUpdate, &gameState)
		return

	case "url", "server":
		if len(args) > 1 {
			newURL := strings.TrimSpace(args[1])
			if newURL != "" {
				gameState.CCServerURL = newURL
				enabled := gameState.CCEnabled
				gameState.mu.Unlock()
				saveSetting("cc_url", newURL)
				if enabled {
					StartCCClientManager(channelName)
				}
				broadcast(msgStateUpdate, &gameState)
				return
			}
		}
		gameState.mu.Unlock()
		return

	case "status":
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		return

	case "reset", "reclaim", "repair", "re-pair":
		gameState.mu.Unlock()
		ResetCCHostToken(channelName)
		return

	default:
		if strings.HasPrefix(sub, "ws://") || strings.HasPrefix(sub, "wss://") {
			gameState.CCServerURL = args[0]
			gameState.CCEnabled = true
			gameState.mu.Unlock()
			saveSetting("cc_url", args[0])
			saveSetting("cc_enabled", "1")
			StartCCClientManager(channelName)
			broadcast(msgStateUpdate, &gameState)
			return
		}
		gameState.mu.Unlock()
		return
	}
}

func handleCommandTimeCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		var dur int
		if _, err := fmt.Sscanf(parts[1], "%d", &dur); err == nil {
			if dur < 5 {
				dur = 5
			} else if dur > 120 {
				dur = 120
			}
			gameState.InputDuration = dur
			gameState.mu.Unlock()
			saveSetting("command_time", strconv.Itoa(dur))
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handleAutoRoundCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		arg := strings.ToLower(parts[1])
		var ar int
		switch arg {
		case "off", "false", "0":
			ar = 0
		case "-1", "immediate", "instant":
			ar = -1
		default:
			if _, err := fmt.Sscanf(parts[1], "%d", &ar); err != nil || ar < 1 {
				gameState.mu.Unlock()
				return
			}
			if ar > 60 {
				ar = 60
			}
		}

		gameState.AutoRound = ar
		gameState.mu.Unlock()
		saveSetting("auto_round", strconv.Itoa(ar))
		broadcast(msgStateUpdate, &gameState)

		if ar == 0 {
			cancelAutoRoundTimer()
		}
		return
	}
	gameState.mu.Unlock()
}

func handleIdleMessageCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	var val bool
	if len(parts) > 1 {
		arg := strings.ToLower(parts[1])
		val = parseBoolArg(arg)
	} else {
		val = !gameState.IdleMessage
	}

	gameState.IdleMessage = val
	gameState.mu.Unlock()
	dbVal := "0"
	if val {
		dbVal = "1"
	}
	saveSetting("idle_message", dbVal)
	broadcast(msgStateUpdate, &gameState)
}

func handleBouncyWallsCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	var val bool
	if len(parts) > 1 {
		arg := strings.ToLower(parts[1])
		val = parseBoolArg(arg)
	} else {
		val = !gameState.BouncyWalls
	}

	gameState.BouncyWalls = val
	gameState.mu.Unlock()
	dbVal := "0"
	if val {
		dbVal = "1"
	}
	saveSetting("bouncy_walls", dbVal)
	broadcast(msgStateUpdate, &gameState)
}

func handleTerrainCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		arg1 := strings.ToLower(parts[1])
		switch {
		case arg1 == "reroll" || arg1 == "roll":
			if gameState.Phase == phaseIdle {
				gameState.Terrain = generateTerrain(gameState.TerrainMin, gameState.TerrainMax)
				for _, p := range gameState.Players {
					p.Y = getTerrainHeight(gameState.Terrain, p.X)
				}
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)
				broadcast(msgResetTerrain, nil)
			} else {
				gameState.mu.Unlock()
			}
			return
		case arg1 == "reset" || arg1 == "default":
			gameState.TerrainMin = 20
			gameState.TerrainMax = 75
		case len(parts) >= 3:
			var minVal, maxVal int
			clean1 := strings.TrimSuffix(parts[1], "%")
			clean2 := strings.TrimSuffix(parts[2], "%")
			_, err1 := fmt.Sscanf(clean1, "%d", &minVal)
			_, err2 := fmt.Sscanf(clean2, "%d", &maxVal)
			if err1 != nil || err2 != nil {
				gameState.mu.Unlock()
				return
			}
			if minVal < 10 {
				minVal = 10
			}
			if maxVal > 90 {
				maxVal = 90
			}
			if minVal > maxVal-10 {
				gameState.mu.Unlock()
				return
			}
			gameState.TerrainMin = minVal
			gameState.TerrainMax = maxVal
		default:
			gameState.mu.Unlock()
			return
		}

		tMin := gameState.TerrainMin
		tMax := gameState.TerrainMax
		saveSetting("terrain_min", strconv.Itoa(tMin))
		saveSetting("terrain_max", strconv.Itoa(tMax))

		// Clamp ProtractorY if it now exceeds highest terrain point
		maxY := max(int(math.Floor(float64(defaultTerrainHeight)*(1.0-float64(tMax)/100.0))), 100)
		if gameState.ProtractorY > maxY {
			gameState.ProtractorY = maxY
			saveSetting("protractor_y", strconv.Itoa(maxY))
		}

		if gameState.Phase == phaseIdle {
			gameState.Terrain = generateTerrain(tMin, tMax)
			for _, p := range gameState.Players {
				p.Y = getTerrainHeight(gameState.Terrain, p.X)
			}
			gameState.mu.Unlock()
			broadcast(msgStateUpdate, &gameState)
			broadcast(msgResetTerrain, nil)
		} else {
			gameState.mu.Unlock()
			broadcast(msgStateUpdate, &gameState)
		}
		return
	}
	gameState.mu.Unlock()
}

func handleStartPermCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, "broadcaster") {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		role := strings.ToLower(parts[1])
		if role == "broadcaster" || role == "mod" || role == "vip" || role == "sub" || role == "all" {
			gameState.StartPerm = role
			gameState.mu.Unlock()
			saveSetting("start_perm", role)
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handleConfigPermCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, "broadcaster") {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		role := strings.ToLower(parts[1])
		if role == "broadcaster" || role == "mod" || role == "vip" || role == "sub" || role == "all" {
			gameState.ConfigPerm = role
			gameState.mu.Unlock()
			saveSetting("config_perm", role)
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handlePermCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, "broadcaster") {
		gameState.mu.Unlock()
		return
	}
	if len(parts) >= 3 {
		target := strings.ToLower(parts[1])
		role := strings.ToLower(parts[2])
		if role == "broadcaster" || role == "mod" || role == "vip" || role == "sub" || role == "all" {
			switch target {
			case "start", "startgame":
				gameState.StartPerm = role
				gameState.mu.Unlock()
				saveSetting("start_perm", role)
				broadcast(msgStateUpdate, &gameState)
				return
			case "config", "settings":
				gameState.ConfigPerm = role
				gameState.mu.Unlock()
				saveSetting("config_perm", role)
				broadcast(msgStateUpdate, &gameState)
				return
			}
		}
	}
	gameState.mu.Unlock()
}

func handleClearLeaderboardCmd(user *twitch.User) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	gameState.Leaderboard = make(map[string]int)
	gameState.mu.Unlock()
	clearLeaderboardDB()
	broadcast(msgStateUpdate, &gameState)
}

func handleKickCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		target := strings.TrimPrefix(parts[1], "@")
		if target != "" {
			var playerKey string
			for k := range gameState.Players {
				if strings.EqualFold(k, target) {
					playerKey = k
					break
				}
			}
			if playerKey != "" {
				removePlayerFromMatchLocked(playerKey)
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)
				return
			}
		}
	}
	gameState.mu.Unlock()
}

func handleDeletePlayerCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		target := strings.TrimPrefix(parts[1], "@")
		if target != "" {
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
			gameState.mu.Unlock()
			deletePlayerDB(target)
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handleJoinCmd(user *twitch.User, parts []string, username string, emotes []*twitch.Emote) {
	player, exists := gameState.Players[username]
	if exists && player.Joined {
		wasLeaving := player.Leaving
		player.Leaving = false
		emoteChanged := false
		if len(parts) > 1 {
			player.Emote = parts[1]
			if len(emotes) > 0 {
				player.EmoteURL = fmt.Sprintf("https://static-cdn.jtvnw.net/emoticons/v2/%s/default/dark/2.0", emotes[0].ID)
			} else {
				for _, de := range defaultEmotes {
					if strings.EqualFold(de.Name, parts[1]) {
						player.EmoteURL = de.URL
						break
					}
				}
			}
			emoteChanged = true
		}
		gameState.mu.Unlock()
		if emoteChanged || wasLeaving {
			broadcast(msgStateUpdate, &gameState)
		}
		return
	}

	if gameState.Phase != phaseIdle && (!exists || !player.Joined) {
		if gameState.Phase != phaseInput {
			gameState.mu.Unlock()
			return
		}
		hasAliveBot := false
		for _, p := range gameState.Players {
			if p.IsBot && !p.IsDead {
				hasAliveBot = true
				break
			}
		}
		if !hasAliveBot {
			gameState.mu.Unlock()
			return
		}
	}

	if !exists {
		player = spawnNewPlayerLocked(username, true)
	} else {
		player.Joined = true
		player.Leaving = false
	}
	if gameState.Phase != phaseIdle {
		player.LastActiveRound = gameState.RoundID
	}
	if len(parts) > 1 {
		player.Emote = parts[1]
		if len(emotes) > 0 {
			player.EmoteURL = fmt.Sprintf("https://static-cdn.jtvnw.net/emoticons/v2/%s/default/dark/2.0", emotes[0].ID)
		} else {
			for _, de := range defaultEmotes {
				if strings.EqualFold(de.Name, parts[1]) {
					player.EmoteURL = de.URL
					break
				}
			}
		}
	}
	if player.EmoteURL == "" {
		randIdx := rand.IntN(len(defaultEmotes))
		player.Emote = defaultEmotes[randIdx].Name
		player.EmoteURL = defaultEmotes[randIdx].URL
	}
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)
}

func handleLeaveCmd(username string) {
	player, exists := gameState.Players[username]
	if !exists {
		gameState.mu.Unlock()
		return
	}

	if gameState.Phase == phaseIdle || !player.Joined {
		delete(gameState.Players, username)
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		return
	}

	player.Leaving = true
	if gameState.Phase == phaseInput {
		player.Fired = true
		checkAllPlayersFired()
	}
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)
}

func handleMinPlayersCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		var mp int
		if _, err := fmt.Sscanf(parts[1], "%d", &mp); err == nil {
			if mp < 2 {
				mp = 2
			} else if mp > 20 {
				mp = 20
			}
			gameState.MinPlayers = mp
			gameState.mu.Unlock()
			saveSetting("min_players", strconv.Itoa(mp))
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handleBotFillCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	var val bool
	if len(parts) > 1 {
		arg := strings.ToLower(parts[1])
		val = parseBoolArg(arg)
	} else {
		val = !gameState.BotFill
	}
	gameState.BotFill = val
	gameState.mu.Unlock()
	dbVal := "0"
	if val {
		dbVal = "1"
	}
	saveSetting("bot_fill", dbVal)
	broadcast(msgStateUpdate, &gameState)
}

func handleBotPointsCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		var bp int
		if _, err := fmt.Sscanf(parts[1], "%d", &bp); err == nil {
			if bp < 0 {
				bp = 0
			} else if bp > 10 {
				bp = 10
			}
			gameState.BotPoints = bp
			gameState.mu.Unlock()
			saveSetting("bot_points", strconv.Itoa(bp))
			broadcast(msgStateUpdate, &gameState)
			return
		}
	}
	gameState.mu.Unlock()
}

func handleBotListCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 2 {
		subCmd := strings.ToLower(parts[1])
		botName := strings.TrimPrefix(parts[2], "@")
		if botName != "" {
			switch subCmd {
			case "add":
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
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)
				return
			case "remove", "del", "delete":
				updated := make([]string, 0, len(gameState.BotList))
				for _, b := range gameState.BotList {
					if !strings.EqualFold(b, botName) {
						updated = append(updated, b)
					}
				}
				gameState.BotList = updated
				removeBotFromList(botName)
				gameState.mu.Unlock()
				broadcast(msgStateUpdate, &gameState)
				return
			}
		}
	}
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)
}

func handleProtractorCmd(user *twitch.User, parts []string) {
	if !hasPermission(user, gameState.ConfigPerm) {
		gameState.mu.Unlock()
		return
	}
	if len(parts) > 1 {
		arg1 := strings.ToLower(parts[1])
		switch {
		case arg1 == "reset":
			gameState.ProtractorX = 250
			maxY := max(int(math.Floor(float64(defaultTerrainHeight)*(1.0-float64(gameState.TerrainMax)/100.0))), 100)
			py := min(350, maxY)
			gameState.ProtractorY = py
		case len(parts) >= 3:
			var px, py int
			_, err1 := fmt.Sscanf(parts[1], "%d", &px)
			_, err2 := fmt.Sscanf(parts[2], "%d", &py)
			if err1 != nil || err2 != nil {
				gameState.mu.Unlock()
				return
			}
			if px < 100 {
				px = 100
			} else if px > 1820 {
				px = 1820
			}
			maxY := max(int(math.Floor(float64(defaultTerrainHeight)*(1.0-float64(gameState.TerrainMax)/100.0))), 100)
			if py < 100 {
				py = 100
			} else if py > maxY {
				py = maxY
			}
			gameState.ProtractorX = px
			gameState.ProtractorY = py
		default:
			gameState.mu.Unlock()
			return
		}
		px := gameState.ProtractorX
		py := gameState.ProtractorY

		nosave := false
		if len(parts) >= 4 && strings.ToLower(parts[3]) == "nosave" {
			nosave = true
		}

		gameState.mu.Unlock()

		if !nosave {
			saveSetting("protractor_x", strconv.Itoa(px))
			saveSetting("protractor_y", strconv.Itoa(py))
		}

		broadcast(msgStateUpdate, &gameState)
		BroadcastViewerState()
		return
	}
	gameState.mu.Unlock()
}

func handleStartGameCmd(user *twitch.User) {
	if !hasPermission(user, gameState.StartPerm) {
		gameState.mu.Unlock()
		return
	}
	if gameState.Phase == phaseIdle {
		humanCount := 0
		for _, p := range gameState.Players {
			if !p.IsBot && p.Joined {
				humanCount++
			}
		}
		topUser := getTopPlayerLocked()
		if humanCount == 0 && topUser == "" {
			gameState.mu.Unlock()
			return
		}
		gameState.mu.Unlock()
		startInputPhase()
		return
	}
	gameState.mu.Unlock()
}

func handleShieldCmd(username string) {
	if gameState.Phase == phaseInput {
		player, exists := gameState.Players[username]
		if !exists || player.IsDead || !player.Joined || player.Leaving {
			gameState.mu.Unlock()
			return
		}
		if player.ShieldUsed {
			gameState.mu.Unlock()
			return
		}

		player.CommandsInMatch++
		player.LastActiveRound = gameState.RoundID
		player.ActionType = actionShield
		player.IsShielded = true
		player.ShieldUsed = true
		player.Fired = true

		checkAllPlayersFired()
		gameState.mu.Unlock()
		broadcast(msgPlayerLocked, username)
		broadcast(msgStateUpdate, &gameState)
		BroadcastViewerState()
		return
	}
	gameState.mu.Unlock()
}

func handleFireCmd(parts []string, username string, cmd string) {
	if gameState.Phase == phaseInput {
		player, exists := gameState.Players[username]
		if !exists || player.IsDead || !player.Joined || player.Leaving || player.IsShielded {
			gameState.mu.Unlock()
			return
		}
		player.CommandsInMatch++
		player.LastActiveRound = gameState.RoundID
		if cmd == "fire" {
			if len(parts) >= 3 {
				var angle, power int
				_, _ = fmt.Sscanf(parts[1], "%d", &angle)
				_, _ = fmt.Sscanf(parts[2], "%d", &power)

				if angle < 0 {
					angle = 0
				} else if angle > 180 {
					angle = 180
				}

				if power < 1 {
					power = 1
				} else if power > 100 {
					power = 100
				}

				player.Angle = angle
				player.Power = power
				player.LastAngle = angle
				player.LastPower = power
			} else {
				player.Angle = player.LastAngle
				player.Power = player.LastPower
			}
			player.ActionType = actionFire
			player.Fired = true
		} else {
			player.ActionType = strings.ToUpper(cmd)
			player.Fired = true
		}

		checkAllPlayersFired()
		gameState.mu.Unlock()
		broadcast(msgPlayerLocked, username)
		broadcast(msgStateUpdate, &gameState)
		return
	}
	gameState.mu.Unlock()
}

func parseBoolArg(arg string) bool {
	if arg == "off" || arg == "false" || arg == "0" || arg == "hide" || arg == "close" {
		return false
	}
	return true
}
