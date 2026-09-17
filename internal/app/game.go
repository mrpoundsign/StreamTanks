package app

import (
	"fmt"
	"math"
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
	MinPlayers:    5,
	BotFill:       true,
	BotPoints:     1,
	BotList:       defaultBotList,
}

func hasPermission(user *twitch.User, requiredRole string) bool {
	if user == nil {
		return true
	}
	if user.IsBroadcaster || (user.Badges != nil && user.Badges["broadcaster"] > 0) || (channelName != "" && strings.EqualFold(user.Name, channelName)) {
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
	gameState.Winner = ""
	gameState.RoundID = 0
	gameState.TimerRemaining = 0
	gameState.MatchKills = nil
	gameState.Terrain = generateTerrain(gameState.TerrainMin, gameState.TerrainMax)
	gameState.Projectiles = []Projectile{}
	gameState.Explosions = []Explosion{}

	// Clean up bots from match so next match fills fresh based on current humans
	for key, p := range gameState.Players {
		if p.IsBot && (!gameState.Debug || key != "TargetBot") {
			delete(gameState.Players, key)
		}
	}

	// Revive all players for next game and reposition
	for _, p := range gameState.Players {
		p.IsDead = false
		p.Fired = false
		p.ActionType = ""
		p.Moving = false
		p.LastActiveRound = gameState.RoundID
		p.X = rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
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

	delay := time.Duration(ar) * time.Minute
	if ar == -1 {
		delay = 500 * time.Millisecond
	}

	autoRoundTimerMu.Lock()
	autoRoundTimer = time.AfterFunc(delay, func() {
		gameState.mu.Lock()
		if gameState.Phase == phaseIdle {
			humanCount := 0
			for _, p := range gameState.Players {
				if !p.IsBot {
					humanCount++
				}
			}
			if humanCount == 0 {
				gameState.mu.Unlock()
				return
			}
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

	// Check if at least 1 real human is in the game
	humanCount := 0
	for _, p := range gameState.Players {
		if !p.IsBot {
			humanCount++
		}
	}
	if humanCount == 0 {
		gameState.Phase = phaseIdle
		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)
		return
	}

	// Bot fill logic: if BotFill is enabled, fill up to MinPlayers
	if gameState.BotFill && len(gameState.Players) < gameState.MinPlayers {
		needed := gameState.MinPlayers - len(gameState.Players)
		// 1. Try to spawn named bots from BotList
		for _, botName := range gameState.BotList {
			if needed <= 0 {
				break
			}
			if _, exists := gameState.Players[botName]; !exists {
				randIdx := rand.IntN(len(defaultEmotes))
				defEmote := defaultEmotes[randIdx]
				spawnX := rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
				spawnY := getTerrainHeight(gameState.Terrain, spawnX)
				gameState.Players[botName] = &Player{
					Name:            botName,
					IsBot:           true,
					Emote:           defEmote.Name,
					EmoteURL:        defEmote.URL,
					LastAngle:       rand.IntN(131) + 20,
					LastPower:       rand.IntN(41) + 40,
					X:               spawnX,
					Y:               spawnY,
					LastActiveRound: gameState.RoundID + 1,
				}
				needed--
			}
		}

		// 2. If still needed, spawn nameless bots (_bot_N)
		botIdx := 1
		for needed > 0 {
			botKey := fmt.Sprintf("_bot_%d", botIdx)
			botIdx++
			if _, exists := gameState.Players[botKey]; !exists {
				randIdx := rand.IntN(len(defaultEmotes))
				defEmote := defaultEmotes[randIdx]
				spawnX := rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
				spawnY := getTerrainHeight(gameState.Terrain, spawnX)
				gameState.Players[botKey] = &Player{
					Name:            "", // nameless!
					IsBot:           true,
					Emote:           defEmote.Name,
					EmoteURL:        defEmote.URL,
					LastAngle:       rand.IntN(131) + 20,
					LastPower:       rand.IntN(41) + 40,
					X:               spawnX,
					Y:               spawnY,
					LastActiveRound: gameState.RoundID + 1,
				}
				needed--
			}
		}
	}

	if gameState.Phase == phaseIdle {
		gameState.MatchKills = nil
	}
	gameState.Phase = phaseInput
	gameState.RoundID++
	inputStartTime = time.Now()
	fastForwardScheduled = false
	if minWaitTimer != nil {
		minWaitTimer.Stop()
		minWaitTimer = nil
	}

	activeHumansLastRound := 0
	if gameState.RoundID > 1 {
		for _, p := range gameState.Players {
			if !p.IsDead && !p.IsBot && p.LastActiveRound == gameState.RoundID-1 {
				activeHumansLastRound++
			}
		}
	}
	prevRoundHadCommands = (activeHumansLastRound > 0)

	// In Round 2+, if no humans commanded in the previous round, only wait 10 seconds total from round start
	initialDurationSec := gameState.InputDuration
	if gameState.RoundID > 1 && activeHumansLastRound == 0 {
		if initialDurationSec > 10 {
			initialDurationSec = 10
		}
	}
	gameState.TimerRemaining = initialDurationSec

	// Reset fired status and action for all players, ensuring valid terrain coordinates
	for _, p := range gameState.Players {
		p.Fired = false
		p.ActionType = ""
		if p.X <= 0 {
			p.X = rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		}
		p.Y = getTerrainHeight(gameState.Terrain, p.X)

		// Bots auto-fire/move immediately so human players don't wait for them
		if p.IsBot && !p.IsDead {
			p.Fired = true
			p.LastActiveRound = gameState.RoundID
			actionChoice := rand.IntN(3)
			switch actionChoice {
			case 0:
				p.ActionType = actionLeft
			case 1:
				p.ActionType = actionRight
			case 2:
				p.ActionType = actionFire
				p.Angle = rand.IntN(131) + 20
				p.Power = rand.IntN(41) + 40
				p.LastAngle = p.Angle
				p.LastPower = p.Power
			}
		}
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
	roundDuration := time.Duration(initialDurationSec) * time.Second

	// 1-second countdown ticker for synchronized HUD and viewer extension timer updates
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				gameState.mu.Lock()
				if gameState.Phase != phaseInput || gameState.RoundID != roundID {
					gameState.mu.Unlock()
					return
				}
				if gameState.TimerRemaining > 0 {
					gameState.TimerRemaining--
					gameState.mu.Unlock()
					broadcast(msgStateUpdate, &gameState)
				} else {
					gameState.mu.Unlock()
				}
			case <-cancelChan:
				return
			}
		}
	}()

	go func() {
		select {
		case <-time.After(roundDuration):
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

	aliveHumanCount := 0
	firedHumanCount := 0
	activeHumansLastRound := 0
	unfiredActiveHumans := 0

	for _, p := range gameState.Players {
		if !p.IsDead && !p.IsBot {
			aliveHumanCount++
			if p.Fired {
				firedHumanCount++
			}
			if gameState.RoundID > 1 && p.LastActiveRound == gameState.RoundID-1 {
				activeHumansLastRound++
				if !p.Fired {
					unfiredActiveHumans++
				}
			}
		}
	}

	if aliveHumanCount == 0 {
		// Only bots are alive; fast-forward immediately
		if minWaitTimer != nil {
			minWaitTimer.Stop()
			minWaitTimer = nil
		}
		close(inputCancel)
		inputCancel = nil
		scheduleFastForward(gameState.RoundID)
		return
	}

	roundID := gameState.RoundID

	// Rule 1: If all alive humans have fired, fast-forward immediately regardless
	if firedHumanCount == aliveHumanCount {
		if minWaitTimer != nil {
			minWaitTimer.Stop()
			minWaitTimer = nil
		}
		close(inputCancel)
		inputCancel = nil
		scheduleFastForward(roundID)
		return
	}

	// Rule 2: In Round 1, timer runs all the way unless all humans fire (handled above)
	if roundID <= 1 {
		return
	}

	// Rule 3: In Round 2+, if all humans who commanded last round have commanded this round:
	// Wait only until at least 10 seconds after the start of the round to begin.
	if unfiredActiveHumans == 0 {
		minDuration := 10 * time.Second
		if dur := time.Duration(gameState.InputDuration) * time.Second; dur < minDuration {
			minDuration = dur
		}

		elapsed := time.Since(inputStartTime)
		if elapsed >= minDuration {
			// At least 10s has already elapsed: fast-forward immediately
			if minWaitTimer != nil {
				minWaitTimer.Stop()
				minWaitTimer = nil
			}
			close(inputCancel)
			inputCancel = nil
			scheduleFastForward(roundID)
			return
		}

		// Less than 10s elapsed: schedule countdown to the 10s mark and update HUD timer
		remaining := minDuration - elapsed
		remSec := int(math.Ceil(remaining.Seconds()))
		if remSec <= 0 {
			remSec = 1
		}

		fastForwardScheduled = true
		gameState.TimerRemaining = remSec

		cancelChan := inputCancel
		if minWaitTimer != nil {
			minWaitTimer.Stop()
		}
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
	gameState.TimerRemaining = 0

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

	// Initialize projectiles and movement targets
	gameState.Projectiles = []Projectile{}
	gameState.Explosions = []Explosion{}

	for name, p := range gameState.Players {
		if p.IsDead {
			continue
		}

		switch p.ActionType {
		case actionFire:
			rad := float64(p.Angle) * math.Pi / 180.0
			powerClamped := p.Power
			if powerClamped < 1 {
				powerClamped = 1
			} else if powerClamped > 100 {
				powerClamped = 100
			}
			powerScaled := float64(powerClamped) / 5.0
			vx := math.Cos(rad) * powerScaled
			vy := -math.Sin(rad) * powerScaled
			shotId := fmt.Sprintf("%d_%s", gameState.RoundID, name)

			muzzleDist := 25.0
			spawnX := p.X + math.Cos(rad)*muzzleDist
			spawnY := p.Y - 10.0 - math.Sin(rad)*muzzleDist

			gameState.Projectiles = append(gameState.Projectiles, Projectile{
				ID:       shotId,
				X:        spawnX,
				Y:        spawnY,
				VX:       vx,
				VY:       vy,
				Owner:    name,
				EmoteURL: p.EmoteURL,
			})
		case actionLeft:
			p.MoveTarget = p.X - float64(gameState.MoveDistance)
			p.Moving = true
			p.SpeedMultiplier = 1.0
			p.HasBounced = false
		case actionRight:
			p.MoveTarget = p.X + float64(gameState.MoveDistance)
			p.Moving = true
			p.SpeedMultiplier = 1.0
			p.HasBounced = false
		}
	}

	gameState.mu.Unlock()

	// Send initial state update
	broadcast(msgStateUpdate, &gameState)
	broadcast(msgExecuteActions, nil)

	go runPhysicsLoop(roundID)
}

func createWallSpark(cx, cy float64) {
	gameState.Explosions = append(gameState.Explosions, Explosion{
		X:         cx,
		Y:         cy,
		Radius:    0,
		MaxRadius: 30,
		Alpha:     1.0,
		IsSpark:   true,
	})
}

func destroyTerrain(cx, cy, radius float64, shotId string) {
	appliedCratersMu.Lock()
	if !appliedCraters[shotId] {
		appliedCraters[shotId] = true
		applyCrater(gameState.Terrain, cx, cy, radius)
	}
	appliedCratersMu.Unlock()

	gameState.Explosions = append(gameState.Explosions, Explosion{
		X:         cx,
		Y:         cy,
		Radius:    0,
		MaxRadius: radius,
		Alpha:     1.0,
		IsSpark:   false,
	})
	broadcastExcept(nil, msgTerrainCrater, CraterPayload{
		ID:     shotId,
		X:      cx,
		Y:      cy,
		Radius: radius,
	})
}

func checkTankCollisions(cx, cy, radius float64, owner string) {
	for name, p := range gameState.Players {
		if name == owner || p.IsDead {
			continue
		}
		dist := math.Hypot(p.X-cx, p.Y-cy)
		if dist < radius+20.0 {
			p.IsDead = true

			// Handle kill attribution
			killerPlayer := gameState.Players[owner]
			killerIsBot := false
			angle := 0
			power := 0
			if killerPlayer != nil {
				killerIsBot = killerPlayer.IsBot
				angle = killerPlayer.Angle
				power = killerPlayer.Power
				if !killerIsBot {
					pts := 1
					if p.IsBot {
						pts = gameState.BotPoints
					}
					if pts > 0 {
						gameState.Leaderboard[owner] += pts
						addScore(owner, pts)
					}
				}
			}
			gameState.MatchKills = append(gameState.MatchKills, KillEvent{
				Killer:      owner,
				KillerIsBot: killerIsBot,
				Victim:      name,
				VictimIsBot: p.IsBot,
				Angle:       angle,
				Power:       power,
				ImpactX:     cx,
				ImpactY:     cy,
				RoundID:     gameState.RoundID,
				Timestamp:   time.Now().UnixMilli(),
			})
			broadcast(msgPlayerDied, PlayerDiedPayload{
				Victim:      name,
				VictimIsBot: p.IsBot,
				Killer:      owner,
				KillerIsBot: killerIsBot,
			})
		}
	}
}

func updateTankMovements(dtScale float64, bouncyWalls bool) bool {
	anyMoving := false
	for name, p := range gameState.Players {
		if p.IsDead {
			continue
		}

		// Execute Action Movement
		if gameState.Phase == phaseAction && p.Moving {
			anyMoving = true
			currentSpeed := p.SpeedMultiplier * 2.0 * dtScale
			switch p.ActionType {
			case actionLeft:
				p.X -= currentSpeed
				if p.X <= 20 {
					if bouncyWalls && !p.HasBounced {
						p.X = 20
						p.ActionType = actionRight
						p.MoveTarget = p.X + float64(gameState.MoveDistance)
						p.SpeedMultiplier = 1.5
						p.HasBounced = true
						createWallSpark(20, p.Y)
					} else if p.MoveTarget != 0 && p.X <= p.MoveTarget || p.X <= 20 {
						p.Moving = false
						if p.X < 20 {
							p.X = 20
						}
					}
				} else if p.MoveTarget != 0 && p.X <= p.MoveTarget {
					p.Moving = false
				}
			case actionRight:
				p.X += currentSpeed
				if p.X >= defaultTerrainWidth-20 {
					if bouncyWalls && !p.HasBounced {
						p.X = defaultTerrainWidth - 20
						p.ActionType = actionLeft
						p.MoveTarget = p.X - float64(gameState.MoveDistance)
						p.SpeedMultiplier = 1.5
						p.HasBounced = true
						createWallSpark(defaultTerrainWidth-20, p.Y)
					} else if p.MoveTarget != 0 && p.X >= p.MoveTarget || p.X >= defaultTerrainWidth-20 {
						p.Moving = false
						if p.X > defaultTerrainWidth-20 {
							p.X = defaultTerrainWidth - 20
						}
					}
				} else if p.MoveTarget != 0 && p.X >= p.MoveTarget {
					p.Moving = false
				}
			}
		}

		// Boundary clamping
		if p.X < 20 {
			p.X = 20
		}
		if p.X > defaultTerrainWidth-20 {
			p.X = defaultTerrainWidth - 20
		}

		// Falling / Ground snapping
		floorY := getTerrainHeight(gameState.Terrain, p.X)
		if p.Y < floorY {
			p.Y += 5.0 * dtScale
			if p.Y > floorY {
				p.Y = floorY
			}
		} else {
			p.Y = floorY
		}

		// Fall off bottom of screen
		if p.Y >= defaultTerrainHeight {
			if !p.IsDead {
				p.IsDead = true
				gameState.MatchKills = append(gameState.MatchKills, KillEvent{
					Victim:      name,
					VictimIsBot: p.IsBot,
					ImpactX:     p.X,
					ImpactY:     p.Y,
					RoundID:     gameState.RoundID,
					Timestamp:   time.Now().UnixMilli(),
				})
				broadcast(msgPlayerDied, PlayerDiedPayload{
					Victim:      name,
					VictimIsBot: p.IsBot,
				})
			}
		}
	}
	return anyMoving
}

func updateProjectiles(dtScale float64, bouncyWalls bool) {
	gravity := 0.2
	for i := len(gameState.Projectiles) - 1; i >= 0; i-- {
		proj := &gameState.Projectiles[i]
		proj.X += proj.VX * dtScale
		proj.VY += gravity * dtScale
		proj.Y += proj.VY * dtScale

		hit := false

		if proj.Y < 0 {
			if bouncyWalls {
				proj.Y = 0
				proj.VY = math.Abs(proj.VY) * 1.1
				proj.VX *= 1.1
				proj.Bounces++
				cx := proj.X
				if cx < 0 {
					cx = 0
				} else if cx > defaultTerrainWidth {
					cx = defaultTerrainWidth
				}
				createWallSpark(cx, 0)
				if proj.Bounces > 15 {
					hit = true
				}
			}
		} else if proj.Y > defaultTerrainHeight {
			if bouncyWalls {
				proj.Y = defaultTerrainHeight
				proj.VY = -math.Abs(proj.VY) * 1.1
				proj.VX *= 1.1
				proj.Bounces++
				cx := proj.X
				if cx < 0 {
					cx = 0
				} else if cx > defaultTerrainWidth {
					cx = defaultTerrainWidth
				}
				createWallSpark(cx, defaultTerrainHeight)
				if proj.Bounces > 15 {
					hit = true
				}
			} else {
				hit = true
			}
		}

		if !hit {
			if proj.X < 0 {
				if bouncyWalls {
					proj.X = 0
					proj.VX = math.Abs(proj.VX) * 1.1
					proj.VY *= 1.1
					proj.Bounces++
					cy := proj.Y
					if cy < 0 {
						cy = 0
					} else if cy > defaultTerrainHeight {
						cy = defaultTerrainHeight
					}
					createWallSpark(0, cy)
					if proj.Bounces > 15 {
						hit = true
					}
				} else {
					hit = true
				}
			} else if proj.X > defaultTerrainWidth {
				if bouncyWalls {
					proj.X = defaultTerrainWidth
					proj.VX = -math.Abs(proj.VX) * 1.1
					proj.VY *= 1.1
					proj.Bounces++
					cy := proj.Y
					if cy < 0 {
						cy = 0
					} else if cy > defaultTerrainHeight {
						cy = defaultTerrainHeight
					}
					createWallSpark(defaultTerrainWidth, cy)
					if proj.Bounces > 15 {
						hit = true
					}
				} else {
					hit = true
				}
			}
		}

		// Terrain collision
		if !hit && proj.Y >= 0 && proj.Y >= getTerrainHeight(gameState.Terrain, proj.X) {
			hit = true
			destroyTerrain(proj.X, proj.Y, 50.0, proj.ID) // EXPLOSION_RADIUS = 50
			checkTankCollisions(proj.X, proj.Y, 50.0, proj.Owner)
		}

		// Direct tank collision
		if !hit {
			for name, p := range gameState.Players {
				if name == proj.Owner || p.IsDead {
					continue
				}
				if math.Hypot(p.X-proj.X, p.Y-proj.Y) < 20 {
					hit = true
					destroyTerrain(proj.X, proj.Y, 50.0, proj.ID)
					checkTankCollisions(proj.X, proj.Y, 50.0, proj.Owner)
					break
				}
			}
		}

		if hit {
			gameState.Projectiles = append(gameState.Projectiles[:i], gameState.Projectiles[i+1:]...)
		}
	}
}

func updatePhysicsStep(dtScale float64) bool {
	bouncyWalls := gameState.BouncyWalls

	anyMoving := updateTankMovements(dtScale, bouncyWalls)
	updateProjectiles(dtScale, bouncyWalls)

	// Update explosions
	for i := len(gameState.Explosions) - 1; i >= 0; i-- {
		exp := &gameState.Explosions[i]
		exp.Radius += 2.0 * dtScale
		exp.Alpha -= 0.05 * dtScale
		if exp.Alpha <= 0 {
			gameState.Explosions = append(gameState.Explosions[:i], gameState.Explosions[i+1:]...)
		}
	}

	// Phase transition check
	if gameState.Phase == phaseAction && len(gameState.Projectiles) == 0 && len(gameState.Explosions) == 0 && !anyMoving {
		anyFalling := false
		for _, p := range gameState.Players {
			if !p.IsDead && p.Y < getTerrainHeight(gameState.Terrain, p.X) {
				anyFalling = true
				break
			}
		}
		if !anyFalling {
			return true // Action is finished
		}
	}

	return false
}

func runPhysicsLoop(roundID int) {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	lastTime := time.Now()

	for {
		<-ticker.C
		now := time.Now()
		rawDt := float64(now.Sub(lastTime).Milliseconds())
		lastTime = now

		if rawDt < 0 {
			rawDt = 0
		} else if rawDt > 100 {
			rawDt = 100
		}
		baseDtScale := rawDt / (1000.0 / 60.0)

		gameState.mu.Lock()
		if gameState.Phase != phaseAction || gameState.RoundID != roundID {
			gameState.mu.Unlock()
			return
		}

		dtScale := baseDtScale * gameState.PhysicsSpeed

		isDone := updatePhysicsStep(dtScale)

		if isDone {
			checkGameOverAndTransition()
			return
		}

		gameState.mu.Unlock()
	}
}

func checkGameOverAndTransition() {
	// determine win condition
	aliveCount := 0
	aliveName := ""
	totalPlayers := 0
	humanAliveCount := 0
	humanTotalCount := 0

	for key, p := range gameState.Players {
		totalPlayers++
		if !p.IsBot {
			humanTotalCount++
			if !p.IsDead {
				humanAliveCount++
			}
		}
		if !p.IsDead {
			aliveCount++
			aliveName = key
		}
	}

	isGameOver := (aliveCount <= 1 && totalPlayers > 1) || (totalPlayers == 1 && aliveCount == 0) || (humanTotalCount > 0 && humanAliveCount == 0)

	if isGameOver {
		winner := "AI"
		if aliveCount == 1 && !gameState.Players[aliveName].IsBot {
			winner = aliveName
		}
		gameState.Phase = phaseCelebration
		gameState.Winner = winner
		if winner != "AI" && winner != "" {
			gameState.Leaderboard[winner] += 5
			addScore(winner, 5)
		}

		gameState.mu.Unlock()
		broadcast(msgStateUpdate, &gameState)

		// Wait 18 seconds for celebration then reset to idle/next match
		time.AfterFunc(18*time.Second, func() {
			resetMatchState()
		})
	} else {
		gameState.mu.Unlock()
		startInputPhase()
	}
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

func processCommand(username string, msg string, emotes []*twitch.Emote, userOpt ...*twitch.User) {
	var user *twitch.User
	if len(userOpt) > 0 {
		user = userOpt[0]
	}

	gameState.mu.Lock()

	// Ensure player exists in state
	if _, exists := gameState.Players[username]; !exists {
		// A new human player is joining!
		// Check if we can cull/replace a bot:
		// Priority 1: Named bots (p.IsBot && p.Name != "")
		// Priority 2: Nameless bots (p.IsBot && p.Name == "")
		var botToReplaceKey string
		var botToReplace *Player

		// Check for named bots first (following BotList order)
		for _, botName := range gameState.BotList {
			if p, exists := gameState.Players[botName]; exists && p.IsBot {
				botToReplaceKey = botName
				botToReplace = p
				break
			}
		}
		if botToReplace == nil {
			for k, p := range gameState.Players {
				if p.IsBot && p.Name != "" {
					botToReplaceKey = k
					botToReplace = p
					break
				}
			}
		}
		// If no named bot found, check for nameless bots
		if botToReplace == nil {
			for k, p := range gameState.Players {
				if p.IsBot {
					botToReplaceKey = k
					botToReplace = p
					break
				}
			}
		}

		spawnX := rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		spawnY := getTerrainHeight(gameState.Terrain, spawnX)

		if botToReplace != nil {
			// Inherit bot's position
			spawnX = botToReplace.X
			spawnY = botToReplace.Y
			delete(gameState.Players, botToReplaceKey)
		}

		randIdx := rand.IntN(len(defaultEmotes))
		defEmote := defaultEmotes[randIdx]
		lastRound := 0
		if gameState.Phase != phaseIdle {
			lastRound = gameState.RoundID
		}
		gameState.Players[username] = &Player{
			Name:            username,
			IsBot:           false,
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
		if len(parts) > 1 && strings.EqualFold(parts[1], "cc") {
			handleCCCommand(user, parts[2:])
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

	case "cc":
		handleCCCommand(user, parts[1:])
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

	case "clearleaderboard", "resetleaderboard":
		if !hasPermission(user, gameState.ConfigPerm) {
			gameState.mu.Unlock()
			return
		}
		gameState.Leaderboard = make(map[string]int)
		gameState.mu.Unlock()
		clearLeaderboardDB()
		broadcast(msgStateUpdate, &gameState)
		return

	case "deleteplayer", "removeplayer":
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
				gameState.mu.Unlock()
				deletePlayerDB(target)
				broadcast(msgStateUpdate, &gameState)
				return
			}
		}
		gameState.mu.Unlock()
		return

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

	case "minplayers":
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
		return

	case "botfill":
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
		return

	case "botpoints":
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
		return

	case "botlist":
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
		return

	case "startgame", "start":
		if !hasPermission(user, gameState.StartPerm) {
			gameState.mu.Unlock()
			return
		}
		if gameState.Phase == phaseIdle {
			humanCount := 0
			for _, p := range gameState.Players {
				if !p.IsBot {
					humanCount++
				}
			}
			if humanCount == 0 {
				gameState.mu.Unlock()
				return
			}
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
