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
}

func init() {
	gameState.Terrain = generateTerrain()
}

func resetMatchState() {
	gameState.mu.Lock()
	if gameState.Phase != phaseCelebration {
		gameState.mu.Unlock()
		return
	}
	gameState.Phase = phaseIdle
	gameState.Terrain = generateTerrain()

	// Revive all players for next game and reposition
	for name, p := range gameState.Players {
		p.IsDead = false
		p.Fired = false
		p.ActionType = ""
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

	broadcast(msgStateUpdate, &gameState)
	broadcast(msgResetTerrain, nil)
	triggerAutoRound()
}

var inputCancel chan struct{}
var (
	autoRoundTimerMu sync.Mutex
	autoRoundTimer   *time.Timer
)

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
		go func() {
			time.Sleep(500 * time.Millisecond)
			gameState.mu.Lock()
			if gameState.Phase == phaseIdle {
				gameState.mu.Unlock()
				startInputPhase()
			} else {
				gameState.mu.Unlock()
			}
		}()
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

	gameState.mu.Lock()
	gameState.Phase = phaseInput
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
	go func() {
		select {
		case <-time.After(time.Duration(gameState.InputDuration) * time.Second):
			executeActionPhase()
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
	alivePlayers := 0
	for _, p := range gameState.Players {
		if !p.IsDead {
			alivePlayers++
			if !p.Fired {
				return
			}
		}
	}
	if alivePlayers > 0 {
		close(inputCancel)
		inputCancel = nil
		go func() {
			time.Sleep(500 * time.Millisecond)
			executeActionPhase()
		}()
	}
}

func executeActionPhase() {
	gameState.mu.Lock()
	if gameState.Phase != phaseInput {
		gameState.mu.Unlock()
		return
	}
	if inputCancel != nil {
		close(inputCancel)
		inputCancel = nil
	}
	gameState.Phase = phaseAction

	// Apply last known values for those who didn't fire
	for _, p := range gameState.Players {
		if p.IsDead {
			continue
		}
		if !p.Fired || p.ActionType == "" {
			if rand.IntN(2) == 0 {
				p.ActionType = actionLeft
			} else {
				p.ActionType = actionRight
			}
			p.Fired = true
		}
	}
	gameState.mu.Unlock()

	// Send state update which tells frontend to execute the shots/moves
	broadcast(msgStateUpdate, &gameState)
	broadcast(msgExecuteActions, nil)
}

func processCommand(username string, msg string, emotes []*twitch.Emote) {
	gameState.mu.Lock()

	// Ensure player exists in state
	if _, exists := gameState.Players[username]; !exists {
		randIdx := rand.IntN(len(defaultEmotes))
		defEmote := defaultEmotes[randIdx]
		spawnX := rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		spawnY := getTerrainHeight(gameState.Terrain, spawnX)
		gameState.Players[username] = &Player{
			Name:      username,
			Emote:     defEmote.Name,
			EmoteURL:  defEmote.URL,
			LastAngle: 45,
			LastPower: 50,
			X:         spawnX,
			Y:         spawnY,
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
		if len(parts) > 1 {
			newPrefix := parts[1]
			gameState.Prefix = newPrefix
			gameState.mu.Unlock()
			saveSetting("prefix", newPrefix)
			broadcast(msgStateUpdate, &gameState)
			return
		}

	case "speed", "physicsspeed":
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

	case "commandtime":
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

	case "join":
		player := gameState.Players[username]
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

	case "startgame":
		if gameState.Phase == phaseIdle {
			gameState.mu.Unlock()
			startInputPhase()
			return
		}

	case "fire", "left", "right":
		if gameState.Phase == phaseInput {
			player := gameState.Players[username]
			if cmd == "fire" {
				if len(parts) >= 3 {
					var angle, power int
					_, _ = fmt.Sscanf(parts[1], "%d", &angle)
					_, _ = fmt.Sscanf(parts[2], "%d", &power)

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
