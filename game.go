package main

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
)

var gameState = GameState{
	Phase:         phaseIdle,
	Players:       make(map[string]*Player),
	InputDuration: 20,
	MoveDistance:  100,
	Leaderboard:   make(map[string]int),
	Prefix:        "%",
	PhysicsSpeed:  0.5,
	IdleMessage:   true,
	TerrainMin:    20,
	TerrainMax:    75,
	StartPerm:     "broadcaster",
	ConfigPerm:    "broadcaster",
}

func hasPermission(user *twitch.User, requiredRole string) bool {
	if user == nil {
		return true
	}
	if user.IsBroadcaster || (channelFlag != nil && *channelFlag != "" && strings.EqualFold(user.Name, *channelFlag)) {
		return true
	}

	switch strings.ToLower(requiredRole) {
	case "broadcaster":
		return false
	case "mod":
		return user.IsMod
	case "vip":
		return user.IsMod || user.IsVip
	case "sub", "subscriber":
		if user.IsMod || user.IsVip {
			return true
		}
		if _, ok := user.Badges["subscriber"]; ok {
			return true
		}
		if _, ok := user.Badges["founder"]; ok {
			return true
		}
		return false
	case "all", "everyone", "anyone":
		return true
	default:
		return false
	}
}

func init() {
	gameState.Terrain = generateTerrain(gameState.TerrainMin, gameState.TerrainMax)
}

func resetMatchState() {
	gameState.mu.Lock()
	if gameState.Phase != phaseCelebration {
		gameState.mu.Unlock()
		return
	}
	gameState.Phase = phaseIdle
	gameState.Terrain = generateTerrain(gameState.TerrainMin, gameState.TerrainMax)

	// Revive all players for next game and reposition
	for name, p := range gameState.Players {
		p.IsDead = false
		p.Fired = false
		p.ActionType = ""
		p.LastActiveRound = gameState.RoundID
		if gameState.Debug {
			if name == "TargetBot" {
				p.X = float64(defaultTerrainWidth)/2.0 + 100.0
			} else {
				p.X = float64(defaultTerrainWidth)/2.0 - 100.0
			}
		} else {
			p.X = rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		}
		p.Y = getTerrainHeight(gameState.Terrain, p.X)
	}
	gameState.mu.Unlock()

	clearAppliedCraters()
	broadcast(msgStateUpdate, &gameState)
	broadcast(msgResetTerrain, nil)
	triggerAutoRound()
}

var (
	inputCancel          chan struct{}
	inputStartTime       time.Time
	prevRoundHadCommands bool
	fastForwardScheduled bool
	minWaitTimer         *time.Timer
	fastForwardTimerMu   sync.Mutex
	fastForwardTimer     *time.Timer
	autoRoundTimerMu     sync.Mutex
	autoRoundTimer       *time.Timer
)

func cancelFastForward() {
	fastForwardTimerMu.Lock()
	defer fastForwardTimerMu.Unlock()
	if fastForwardTimer != nil {
		fastForwardTimer.Stop()
		fastForwardTimer = nil
	}
}

func scheduleFastForward(roundID int) {
	fastForwardTimerMu.Lock()
	defer fastForwardTimerMu.Unlock()
	if fastForwardTimer != nil {
		fastForwardTimer.Stop()
	}
	fastForwardTimer = time.AfterFunc(500*time.Millisecond, func() {
		executeActionPhaseForRound(roundID)
	})
}

func cancelAutoRoundTimer() {
	autoRoundTimerMu.Lock()
	defer autoRoundTimerMu.Unlock()
	if autoRoundTimer != nil {
		autoRoundTimer.Stop()
		autoRoundTimer = nil
	}
}

func triggerAutoRound() {
	gameState.mu.Lock()
	ar := gameState.AutoRound
	gameState.mu.Unlock()

	if ar == 0 {
		return
	}

	cancelAutoRoundTimer()

	if ar == -1 {
		// Immediate next round
		autoRoundTimerMu.Lock()
		autoRoundTimer = time.AfterFunc(500*time.Millisecond, func() {
			gameState.mu.Lock()
			if gameState.Phase == phaseIdle {
				gameState.mu.Unlock()
				startInputPhase()
			} else {
				gameState.mu.Unlock()
			}
		})
		autoRoundTimerMu.Unlock()
		return
	}

	// Scheduled minutes
	delay := time.Duration(ar) * time.Minute
	autoRoundTimerMu.Lock()
	autoRoundTimer = time.AfterFunc(delay, func() {
		gameState.mu.Lock()
		if gameState.Phase == phaseIdle {
			gameState.mu.Unlock()
			startInputPhase()
		} else {
			gameState.mu.Unlock()
		}
	})
	autoRoundTimerMu.Unlock()
}

func startInputPhase() {
	cancelAutoRoundTimer()
	cancelFastForward()

	gameState.mu.Lock()
	gameState.Phase = phaseInput
	gameState.RoundID++
	inputStartTime = time.Now()
	fastForwardScheduled = false
	if minWaitTimer != nil {
		minWaitTimer.Stop()
		minWaitTimer = nil
	}

	prevRoundHadCommands = false
	if gameState.RoundID > 1 {
		for _, p := range gameState.Players {
			if !p.IsDead && p.LastActiveRound == gameState.RoundID-1 {
				prevRoundHadCommands = true
				break
			}
		}
	}

	// Reset fired status and action for all players, ensuring valid terrain coordinates
	for _, p := range gameState.Players {
		p.Fired = false
		p.ActionType = ""
		if p.X <= 0 {
			p.X = rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		}
		p.Y = getTerrainHeight(gameState.Terrain, p.X)
	}
	if inputCancel != nil {
		close(inputCancel)
	}
	inputCancel = make(chan struct{})
	cancelChan := inputCancel

	// In debug mode, auto-ready the TargetBot after 1s so single player can test
	if gameState.Debug {
		if bot, exists := gameState.Players["TargetBot"]; exists && !bot.IsDead {
			go func() {
				time.Sleep(1 * time.Second)
				gameState.mu.Lock()
				if gameState.Phase == phaseInput && !bot.IsDead {
					bot.Fired = true
					bot.LastActiveRound = gameState.RoundID
					if rand.IntN(2) == 0 {
						bot.ActionType = actionLeft
					} else {
						bot.ActionType = actionRight
					}
					checkAllPlayersFired()
					gameState.mu.Unlock()
					broadcast(msgStateUpdate, &gameState)
				} else {
					gameState.mu.Unlock()
				}
			}()
		}
	}
	gameState.mu.Unlock()

	broadcast(msgStateUpdate, &gameState)

	// Start timer for input phase
	roundID := gameState.RoundID
	go func() {
		select {
		case <-time.After(time.Duration(gameState.InputDuration) * time.Second):
			executeActionPhaseForRound(roundID)
		case <-cancelChan:
			return
		}
	}()
}

func checkAllPlayersFired() {
	// Assumes gameState.mu is held
	if gameState.Phase != phaseInput || inputCancel == nil {
		return
	}

	aliveCount := 0
	firedCount := 0
	unfiredActiveCount := 0

	for _, p := range gameState.Players {
		if !p.IsDead {
			aliveCount++
			if p.Fired {
				firedCount++
			} else if p.LastActiveRound == gameState.RoundID-1 {
				unfiredActiveCount++
			}
		}
	}

	if aliveCount == 0 {
		return
	}

	roundID := gameState.RoundID

	// If all alive players have fired, fast-forward immediately regardless
	if firedCount == aliveCount {
		if minWaitTimer != nil {
			minWaitTimer.Stop()
			minWaitTimer = nil
		}
		close(inputCancel)
		inputCancel = nil
		scheduleFastForward(roundID)
		return
	}

	// If players were active in the previous round, fast-forward once all active players have fired
	if prevRoundHadCommands {
		if firedCount > 0 && unfiredActiveCount == 0 {
			close(inputCancel)
			inputCancel = nil
			scheduleFastForward(roundID)
		}
		return
	}

	// If no players entered a command in the previous round, enforce a 10s minimum before fast-forwarding
	if firedCount > 0 && !fastForwardScheduled {
		minDuration := 10 * time.Second
		if dur := time.Duration(gameState.InputDuration) * time.Second; dur < minDuration {
			minDuration = dur
		}

		elapsed := time.Since(inputStartTime)
		if elapsed >= minDuration {
			close(inputCancel)
			inputCancel = nil
			scheduleFastForward(roundID)
			return
		}

		fastForwardScheduled = true
		remaining := minDuration - elapsed
		cancelChan := inputCancel
		minWaitTimer = time.AfterFunc(remaining, func() {
			gameState.mu.Lock()
			defer gameState.mu.Unlock()
			select {
			case <-cancelChan:
				return
			default:
			}
			if gameState.Phase == phaseInput && gameState.RoundID == roundID && inputCancel != nil {
				close(inputCancel)
				inputCancel = nil
				scheduleFastForward(roundID)
			}
		})
	}
}

func executeActionPhase() {
	gameState.mu.Lock()
	rID := gameState.RoundID
	gameState.mu.Unlock()
	executeActionPhaseForRound(rID)
}

func executeActionPhaseForRound(roundID int) {
	gameState.mu.Lock()
	if gameState.Phase != phaseInput || gameState.RoundID != roundID {
		gameState.mu.Unlock()
		return
	}
	cancelFastForward()
	if inputCancel != nil {
		close(inputCancel)
		inputCancel = nil
	}
	if minWaitTimer != nil {
		minWaitTimer.Stop()
		minWaitTimer = nil
	}
	fastForwardScheduled = false
	gameState.Phase = phaseAction

	// Apply action for those who didn't command: random mix of move and fire
	for _, p := range gameState.Players {
		if p.IsDead {
			continue
		}
		if !p.Fired || p.ActionType == "" {
			actionChoice := rand.IntN(3)
			switch actionChoice {
			case 0:
				p.ActionType = actionLeft
			case 1:
				p.ActionType = actionRight
			case 2:
				p.ActionType = actionFire
				p.Angle = rand.IntN(131) + 20 // 20 to 150
				p.Power = rand.IntN(41) + 40  // 40 to 80
				p.LastAngle = p.Angle
				p.LastPower = p.Power
			}
			p.Fired = true
		}
	}
	gameState.mu.Unlock()

	// Send state update which tells frontend to execute the shots/moves
	broadcast(msgStateUpdate, &gameState)
	broadcast(msgExecuteActions, nil)
}

func processCommand(username string, msg string, emotes []*twitch.Emote, userOpt ...*twitch.User) {
	var user *twitch.User
	if len(userOpt) > 0 {
		user = userOpt[0]
	}

	gameState.mu.Lock()

	// Ensure player exists in state
	if _, exists := gameState.Players[username]; !exists {
		randIdx := rand.IntN(len(defaultEmotes))
		defEmote := defaultEmotes[randIdx]
		spawnX := rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		spawnY := getTerrainHeight(gameState.Terrain, spawnX)
		lastRound := 0
		if gameState.Phase != phaseIdle {
			lastRound = gameState.RoundID
		}
		gameState.Players[username] = &Player{
			Name:            username,
			Emote:           defEmote.Name,
			EmoteURL:        defEmote.URL,
			LastAngle:       45,
			LastPower:       50,
			X:               spawnX,
			Y:               spawnY,
			LastActiveRound: lastRound,
		}
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		gameState.mu.Lock()
	}

	currPrefix := gameState.Prefix
	if currPrefix == "" {
		currPrefix = "%"
	}

	trimmedMsg := strings.TrimSpace(msg)
	var cmdStr string
	switch {
	case strings.HasPrefix(trimmedMsg, currPrefix):
		cmdStr = strings.TrimPrefix(trimmedMsg, currPrefix)
	case strings.HasPrefix(trimmedMsg, "%"):
		cmdStr = strings.TrimPrefix(trimmedMsg, "%")
	case strings.HasPrefix(trimmedMsg, "!"):
		cmdStr = strings.TrimPrefix(trimmedMsg, "!")
	default:
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
	case "prefix":
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

	case "speed", "physicsspeed":
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

	case "config", "settings":
		if !hasPermission(user, gameState.ConfigPerm) {
			gameState.mu.Unlock()
			return
		}
		if len(parts) > 1 {
			arg := strings.ToLower(parts[1])
			if arg == "off" || arg == "hide" || arg == "close" || arg == "false" || arg == "0" {
				gameState.ShowConfig = false
			} else {
				gameState.ShowConfig = true
			}
		} else {
			// Toggle config modal
			gameState.ShowConfig = !gameState.ShowConfig
		}
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		return

	case "commandtime", "roundtime":
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

	case "autoround":
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

	case "idlemessage":
		if !hasPermission(user, gameState.ConfigPerm) {
			gameState.mu.Unlock()
			return
		}
		var val bool
		if len(parts) > 1 {
			arg := strings.ToLower(parts[1])
			if arg == "off" || arg == "false" || arg == "0" || arg == "hide" {
				val = false
			} else {
				val = true
			}
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
		return

	case "bouncywalls", "bouncy":
		if !hasPermission(user, gameState.ConfigPerm) {
			gameState.mu.Unlock()
			return
		}
		var val bool
		if len(parts) > 1 {
			arg := strings.ToLower(parts[1])
			if arg == "off" || arg == "false" || arg == "0" {
				val = false
			} else {
				val = true
			}
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
		return

	case "terrain":
		if !hasPermission(user, gameState.ConfigPerm) {
			gameState.mu.Unlock()
			return
		}
		if len(parts) > 1 {
			arg1 := strings.ToLower(parts[1])
			switch {
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

	case "startperm":
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

	case "configperm":
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

	case "perm", "perms", "permission", "permissions":
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

	case "join":
		player := gameState.Players[username]
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
		return

	case "startgame", "start":
		if !hasPermission(user, gameState.StartPerm) {
			gameState.mu.Unlock()
			return
		}
		if gameState.Phase == phaseIdle {
			gameState.mu.Unlock()
			startInputPhase()
			return
		}

	case "fire", "left", "right":
		if gameState.Phase == phaseInput {
			player := gameState.Players[username]
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
	}

	gameState.mu.Unlock()
}
