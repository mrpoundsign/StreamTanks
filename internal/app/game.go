package app

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/gempir/go-twitch-irc/v4"
)

var (
	appClock clock.Clock = clock.New()
)

func setClock(c clock.Clock) {
	appClock = c
}

var gameState = GameState{
	Phase:         phaseIdle,
	Players:       make(map[string]*Player),
	InputDuration: 20,
	MoveDistance:  100,
	Leaderboard:   make(map[string]int),
	Prefix:        "%",
	PhysicsSpeed:  0.5,
	IdleMessage:   true,
	BouncyWalls:   false,
	TerrainClimb:  false,
	TerrainMin:    20,
	TerrainMax:    75,
	TerrainColor:  defaultTerrainColor,
	TankColor:     defaultTankColor,
	StartPerm:     "broadcaster",
	ConfigPerm:    "broadcaster",
	MinPlayers:    5,
	BotFill:       true,
	BotPoints:     1,
	BotList:       defaultBotList,
	ProtractorX:   250,
	ProtractorY:   270,
	CCServerURL:   defaultCCServerURL,
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

	// Clean up all bots from match so no bots exist in idle phase
	for key, p := range gameState.Players {
		if p.IsBot {
			delete(gameState.Players, key)
		}
	}

	// Filter out inactive humans who entered 0 commands during the completed match,
	// and revive active players for next game.
	for key, p := range gameState.Players {
		if !p.IsBot {
			if p.CommandsInMatch == 0 || p.Leaving {
				delete(gameState.Players, key)
				continue
			}
			p.CommandsInMatch = 0
		}
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
	minWaitTimer         *clock.Timer
	fastForwardTimerMu   sync.Mutex
	fastForwardTimer     *clock.Timer
	autoRoundTimerMu     sync.Mutex
	autoRoundTimer       *clock.Timer
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
	fastForwardTimer = appClock.AfterFunc(500*time.Millisecond, func() {
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

func isAutoRoundTimerRunning() bool {
	autoRoundTimerMu.Lock()
	defer autoRoundTimerMu.Unlock()
	return autoRoundTimer != nil
}

func triggerAutoRoundOnJoin() {
	gameState.mu.Lock()
	ar := gameState.AutoRound
	phase := gameState.Phase
	gameState.mu.Unlock()

	if phase != phaseIdle || ar == 0 {
		return
	}

	if !isAutoRoundTimerRunning() {
		triggerAutoRound()
	}
}

func getTopPlayerLocked() string {
	var topUser string
	maxScore := 0
	for user, score := range gameState.Leaderboard {
		if score > maxScore {
			maxScore = score
			topUser = user
		} else if score == maxScore && score > 0 {
			if topUser == "" || strings.ToLower(user) < strings.ToLower(topUser) {
				topUser = user
			}
		}
	}
	return topUser
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
	autoRoundTimer = appClock.AfterFunc(delay, func() {
		gameState.mu.Lock()
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
				autoRoundTimerMu.Lock()
				autoRoundTimer = nil
				autoRoundTimerMu.Unlock()
				return
			}
			gameState.mu.Unlock()
			startInputPhase()
		} else {
			gameState.mu.Unlock()
			autoRoundTimerMu.Lock()
			autoRoundTimer = nil
			autoRoundTimerMu.Unlock()
		}
	})
	autoRoundTimerMu.Unlock()
}

func startInputPhase() {
	cancelAutoRoundTimer()
	cancelFastForward()

	gameState.mu.Lock()

	// If starting from IDLE, ensure top player from leaderboard is added and joined
	if gameState.Phase == phaseIdle {
		topUser := getTopPlayerLocked()
		if topUser != "" {
			if p, exists := gameState.Players[topUser]; exists {
				p.Joined = true
			}
		}

		// Drop any unjoined roamers or leaving players
		for key, p := range gameState.Players {
			if !p.IsBot && (!p.Joined || p.Leaving) {
				delete(gameState.Players, key)
			}
		}

		// If top player wasn't already in gameState.Players, spawn them now as joined
		if topUser != "" {
			if _, exists := gameState.Players[topUser]; !exists {
				spawnNewPlayerLocked(topUser, true)
			}
		}
	}

	// Check if at least 1 real joined human is in the game
	humanCount := 0
	for _, p := range gameState.Players {
		if !p.IsBot && p.Joined {
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
		for _, p := range gameState.Players {
			p.ShieldUsed = false
			p.IsShielded = false
		}
	}
	gameState.Phase = phaseInput
	gameState.RoundID++
	inputStartTime = appClock.Now()
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
		p.IsShielded = false
		if p.X <= 0 {
			p.X = rand.Float64()*(float64(defaultTerrainWidth)-200.0) + 100.0
		}
		p.Y = getTerrainHeight(gameState.Terrain, p.X)

		// Bots auto-fire/move/shield immediately so human players don't wait for them
		if p.IsBot && !p.IsDead {
			p.Fired = true
			p.LastActiveRound = gameState.RoundID
			// 10% chance to activate shield if still available (1-time use per match)
			if !p.ShieldUsed && rand.IntN(10) == 0 {
				p.ActionType = actionShield
				p.IsShielded = true
				p.ShieldUsed = true
			} else {
				actionChoice := rand.IntN(3)
				switch actionChoice {
				case 0:
					p.ActionType = actionLeft
				case 1:
					p.ActionType = actionRight
				case 2:
					p.ActionType = actionFire
					p.Angle = rand.IntN(131) + 20
					p.Power = rand.IntN(61) + 30
					p.LastAngle = p.Angle
					p.LastPower = p.Power
				}
			}
		} else if p.Leaving && !p.IsDead {
			p.Fired = true
		}
	}
	if inputCancel != nil {
		close(inputCancel)
	}
	inputCancel = make(chan struct{})
	cancelChan := inputCancel

	gameState.mu.Unlock()

	broadcast(msgStateUpdate, &gameState)

	// Start timer for input phase
	roundID := gameState.RoundID
	roundDuration := time.Duration(initialDurationSec) * time.Second

	// 1-second countdown ticker for synchronized HUD and viewer extension timer updates
	go func() {
		ticker := appClock.Ticker(1 * time.Second)
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
		case <-appClock.After(roundDuration):
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

		elapsed := appClock.Since(inputStartTime)
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
		minWaitTimer = appClock.AfterFunc(remaining, func() {
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

	// Initialize projectiles and movement targets in deterministic alphabetical order
	gameState.Projectiles = []Projectile{}
	gameState.Explosions = []Explosion{}

	playerNames := make([]string, 0, len(gameState.Players))
	for name := range gameState.Players {
		playerNames = append(playerNames, name)
	}
	slices.Sort(playerNames)

	for _, name := range playerNames {
		p := gameState.Players[name]
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

// liveCollisionSink connects app.Engine physics simulation to the live game server:
// score deductions/awards, KillEvent recording, and WebSocket event broadcasting.
type liveCollisionSink struct {
	roundID int
}

func (s *liveCollisionSink) OnCrater(cx, cy, radius float64, shotID string) {
	appliedCratersMu.Lock()
	appliedCraters[shotID] = true
	appliedCratersMu.Unlock()

	broadcastExcept(nil, msgTerrainCrater, CraterPayload{
		ID:     shotID,
		X:      cx,
		Y:      cy,
		Radius: radius,
	})
}

func (s *liveCollisionSink) OnSpark(x, y float64) {}

func (s *liveCollisionSink) OnKill(victim, killer string, cx, cy float64, isBot bool) {
	loss := 0
	if !isBot {
		victimScore := gameState.Leaderboard[victim]
		loss = victimScore / 20
		if loss > 0 {
			gameState.Leaderboard[victim] -= loss
			deductScore(victim, loss)
		}
	}

	killerPlayer := gameState.Players[killer]
	killerIsBot := false
	angle := 0
	power := 0
	bountyAwarded := 0
	pointsAwarded := 0
	if killerPlayer != nil {
		killerIsBot = killerPlayer.IsBot
		angle = killerPlayer.Angle
		power = killerPlayer.Power
		if !killerIsBot {
			pts := 1
			if isBot {
				pts = gameState.BotPoints
			}
			totalPts := pts + loss
			if totalPts > 0 {
				gameState.Leaderboard[killer] += totalPts
				addScore(killer, totalPts)
			}
			bountyAwarded = loss
			pointsAwarded = totalPts
		}
	}

	gameState.MatchKills = append(gameState.MatchKills, KillEvent{
		Killer:        killer,
		KillerIsBot:   killerIsBot,
		Victim:        victim,
		VictimIsBot:   isBot,
		Angle:         angle,
		Power:         power,
		ImpactX:       cx,
		ImpactY:       cy,
		RoundID:       s.roundID,
		Timestamp:     time.Now().UnixMilli(),
		PointsLost:    loss,
		PointsAwarded: pointsAwarded,
	})

	broadcast(msgPlayerDied, PlayerDiedPayload{
		Victim:        victim,
		VictimIsBot:   isBot,
		Killer:        killer,
		KillerIsBot:   killerIsBot,
		PointsLost:    loss,
		PointsAwarded: pointsAwarded,
		BountyAwarded: bountyAwarded,
	})
}

func checkTankCollisions(cx, cy, radius float64, owner string) {
	engine := &Engine{
		Terrain:      gameState.Terrain,
		Players:      gameState.Players,
		BouncyWalls:  gameState.BouncyWalls,
		TerrainClimb: gameState.TerrainClimb,
		MoveDistance: gameState.MoveDistance,
		BotPoints:    gameState.BotPoints,
	}
	sink := &liveCollisionSink{roundID: gameState.RoundID}
	engine.CheckTankCollisions(cx, cy, radius, owner, sink)
}

func updateTankMovements(dtScale float64, bouncyWalls bool) (bool, bool) {
	engine := &Engine{
		Terrain:      gameState.Terrain,
		Players:      gameState.Players,
		Explosions:   gameState.Explosions,
		BouncyWalls:  bouncyWalls,
		TerrainClimb: gameState.TerrainClimb,
		MoveDistance: gameState.MoveDistance,
		BotPoints:    gameState.BotPoints,
	}
	sink := &liveCollisionSink{roundID: gameState.RoundID}
	anyMoving, anyFalling := engine.UpdateTankMovements(dtScale, sink)
	gameState.Explosions = engine.Explosions
	return anyMoving, anyFalling
}

func updateProjectiles(dtScale float64, bouncyWalls bool) {
	engine := &Engine{
		Terrain:      gameState.Terrain,
		Players:      gameState.Players,
		Projectiles:  gameState.Projectiles,
		Explosions:   gameState.Explosions,
		BouncyWalls:  bouncyWalls,
		TerrainClimb: gameState.TerrainClimb,
		MoveDistance: gameState.MoveDistance,
		BotPoints:    gameState.BotPoints,
	}
	sink := &liveCollisionSink{roundID: gameState.RoundID}
	engine.UpdateProjectiles(dtScale, sink)
	gameState.Projectiles = engine.Projectiles
	gameState.Explosions = engine.Explosions
}

func updatePhysicsStep(dtScale float64) bool {
	engine := &Engine{
		Terrain:      gameState.Terrain,
		Players:      gameState.Players,
		Projectiles:  gameState.Projectiles,
		Explosions:   gameState.Explosions,
		BouncyWalls:  gameState.BouncyWalls,
		TerrainClimb: gameState.TerrainClimb,
		MoveDistance: gameState.MoveDistance,
		BotPoints:    gameState.BotPoints,
	}
	sink := &liveCollisionSink{roundID: gameState.RoundID}
	isDone := engine.Step(dtScale, sink)
	gameState.Projectiles = engine.Projectiles
	gameState.Explosions = engine.Explosions
	return isDone
}

func runPhysicsLoop(roundID int) {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	for {
		<-ticker.C

		gameState.mu.Lock()
		if gameState.Phase != phaseAction || gameState.RoundID != roundID {
			gameState.mu.Unlock()
			return
		}

		dtScale := 1.0 * gameState.PhysicsSpeed

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
		appClock.AfterFunc(18*time.Second, func() {
			resetMatchState()
		})
	} else {
		gameState.mu.Unlock()
		startInputPhase()
	}
}

func spawnNewPlayerLocked(username string, joined bool) *Player {
	var botToReplaceKey string
	var botToReplace *Player

	// Check for named bots first (following BotList order)
	for _, botName := range gameState.BotList {
		if p, exists := gameState.Players[botName]; exists && p.IsBot && !p.IsDead {
			botToReplaceKey = botName
			botToReplace = p
			break
		}
	}
	if botToReplace == nil {
		for k, p := range gameState.Players {
			if p.IsBot && !p.IsDead && p.Name != "" {
				botToReplaceKey = k
				botToReplace = p
				break
			}
		}
	}
	// If no named bot found, check for nameless bots
	if botToReplace == nil {
		for k, p := range gameState.Players {
			if p.IsBot && !p.IsDead {
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

	emoteName := ""
	emoteURL := ""
	if savedEmote, savedURL, ok := getPlayerEmote(username); ok && savedEmote != "" {
		emoteName = savedEmote
		emoteURL = savedURL
	} else {
		randIdx := rand.IntN(len(defaultEmotes))
		emoteName = defaultEmotes[randIdx].Name
		emoteURL = defaultEmotes[randIdx].URL
	}

	lastRound := 0
	if gameState.Phase != phaseIdle {
		lastRound = gameState.RoundID
	}
	p := &Player{
		Name:            username,
		IsBot:           false,
		Emote:           emoteName,
		EmoteURL:        emoteURL,
		LastAngle:       45,
		LastPower:       50,
		X:               spawnX,
		Y:               spawnY,
		LastActiveRound: lastRound,
		Joined:          joined,
	}
	gameState.Players[username] = p
	return p
}

func removePlayerFromMatchLocked(playerKey string) {
	delete(gameState.Players, playerKey)

	// Filter out any in-flight projectiles owned by the kicked player
	filtered := make([]Projectile, 0, len(gameState.Projectiles))
	for _, proj := range gameState.Projectiles {
		if !strings.EqualFold(proj.Owner, playerKey) {
			filtered = append(filtered, proj)
		}
	}
	gameState.Projectiles = filtered

	if gameState.Phase == phaseInput {
		aliveCount := 0
		aliveName := ""
		totalPlayers := 0
		humanAliveCount := 0
		humanTotalCount := 0

		for k, p := range gameState.Players {
			totalPlayers++
			if !p.IsBot {
				humanTotalCount++
				if !p.IsDead {
					humanAliveCount++
				}
			}
			if !p.IsDead {
				aliveCount++
				aliveName = k
			}
		}

		isGameOver := (aliveCount <= 1) || (humanTotalCount > 0 && humanAliveCount == 0) || (totalPlayers == 0)

		if isGameOver {
			if inputCancel != nil {
				close(inputCancel)
				inputCancel = nil
			}
			if minWaitTimer != nil {
				minWaitTimer.Stop()
				minWaitTimer = nil
			}
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
			appClock.AfterFunc(18*time.Second, func() {
				resetMatchState()
			})
		} else {
			checkAllPlayersFired()
		}
	}
}

