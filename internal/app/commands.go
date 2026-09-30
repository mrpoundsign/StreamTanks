package app

import (
	"fmt"
	"math/rand/v2"
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
	case "kick":
		handleKickCmd(user, parts)
	case "join", "icon", "emote", "skin":
		handleJoinCmd(user, parts, username, emotes)
	case "leave":
		handleLeaveCmd(username)
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
				player.EmoteURL = ""
				for _, de := range defaultEmotes {
					if strings.EqualFold(de.Name, parts[1]) {
						player.EmoteURL = de.URL
						break
					}
				}
			}
			emoteChanged = true
			savePlayerEmote(username, player.Emote, player.EmoteURL)
		}
		gameState.mu.Unlock()
		if emoteChanged || wasLeaving {
			broadcast(msgStateUpdate, &gameState)
		}
		if wasLeaving {
			triggerAutoRoundOnJoin()
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
			player.EmoteURL = ""
			for _, de := range defaultEmotes {
				if strings.EqualFold(de.Name, parts[1]) {
					player.EmoteURL = de.URL
					break
				}
			}
		}
		savePlayerEmote(username, player.Emote, player.EmoteURL)
	} else {
		if savedEmote, savedURL, ok := getPlayerEmote(username); ok && savedEmote != "" {
			player.Emote = savedEmote
			player.EmoteURL = savedURL
		}
	}
	if player.EmoteURL == "" {
		randIdx := rand.IntN(len(defaultEmotes))
		player.Emote = defaultEmotes[randIdx].Name
		player.EmoteURL = defaultEmotes[randIdx].URL
	}
	gameState.mu.Unlock()
	broadcast(msgStateUpdate, &gameState)
	triggerAutoRoundOnJoin()
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
