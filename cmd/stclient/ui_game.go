package main

import (
	"fmt"
	"log"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
)

// ShowGame transitions the UI into the live Game Controller view for target channel
func (ctx *AppContext) ShowGame(channel string) {
	ctx.Window.SetContent(ctx.makeGameView(channel))
}

// makeGameView constructs the tactical match controller matching the mobile extension
func (ctx *AppContext) makeGameView(channel string) fyne.CanvasObject {
	log.Printf("[ui_game] Opening Game View for channel: %q (pilot: %q)", channel, ctx.Username)
	gameClient := NewGameClient(ctx.CCClient.BaseURL, channel, ctx.JWT)

	// Top Bar: Channel & Pilot identity on left, Status & Leave on right
	channelTitle := widget.NewLabelWithStyle(channel, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	pilotBadge := widget.NewLabelWithStyle("Pilot: "+ctx.Username, fyne.TextAlignLeading, fyne.TextStyle{})
	playerStatusBadge := widget.NewLabelWithStyle("[SPECTATING]", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})

	leaveBtn := widget.NewButton("LEAVE", func() {
		log.Printf("[ui_game] LEAVE clicked -> closing game client")
		gameClient.Close()
		ctx.ShowDashboard()
	})
	leaveBtn.Importance = widget.DangerImportance

	topBarRight := container.NewHBox(playerStatusBadge, leaveBtn)
	topBar := container.NewBorder(nil, nil, container.NewVBox(channelTitle, pilotBadge), topBarRight)

	minimap := NewMinimapWidget(ctx.Username)

	// Aim & Power Controls State
	var currentAngle float64 = 45
	var currentPower float64 = 100

	// Phase & Timer to the left of Aim
	phaseBadge := widget.NewLabelWithStyle("CONNECTING...", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	timerLabel := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	phaseBox := container.NewGridWrap(fyne.NewSize(85, 40), container.NewVBox(phaseBadge, timerLabel))

	angleTitle := widget.NewLabelWithStyle("AIM", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	angleReadout := widget.NewLabelWithStyle(fmt.Sprintf("%.0f°", currentAngle), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	aimBox := container.NewVBox(angleTitle, angleReadout)
	dummySpacer := container.NewGridWrap(fyne.NewSize(85, 40), container.NewVBox())

	protractorHeader := container.NewBorder(nil, nil, phaseBox, dummySpacer, container.NewCenter(aimBox))
	protractor := NewProtractorWidget(currentAngle)
	protractorCol := container.NewBorder(protractorHeader, nil, nil, nil, protractor)

	powerTitle := widget.NewLabelWithStyle("PWR", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	powerReadout := widget.NewLabelWithStyle(fmt.Sprintf("%.0f%%", currentPower), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	powerGauge := NewPowerGaugeWidget(currentPower)
	updateAim := func() {
		angleReadout.SetText(fmt.Sprintf("%.0f°", currentAngle))
		powerReadout.SetText(fmt.Sprintf("%.0f%%", currentPower))
		minimap.SetAim(currentAngle, currentPower)
	}

	protractor.OnAngleChanged = func(deg float64) {
		currentAngle = deg
		updateAim()
	}

	powerGauge.OnPowerChanged = func(val float64) {
		currentPower = val
		updateAim()
	}

	powerCol := container.NewBorder(container.NewVBox(powerTitle, powerReadout), nil, nil, nil, powerGauge)

	// Side-by-side Aiming Controls Row
	aimingRow := container.NewBorder(nil, nil, nil, container.NewPadded(powerCol), container.NewPadded(protractorCol))

	// Gameplay Action Buttons
	var fireBtn *widget.Button
	var joinBtn *widget.Button
	var shieldBtn *widget.Button
	var moveLeftBtn *widget.Button
	var moveRightBtn *widget.Button

	fireBtn = widget.NewButton("🔥 FIRE CANNON", func() {
		log.Printf("[ui_game] FIRE CANNON (angle=%.0f, power=%.0f)", currentAngle, currentPower)
		_ = gameClient.SendFire(float32(currentAngle), float32(currentPower))
	})
	fireBtn.Importance = widget.HighImportance

	joinBtn = widget.NewButton("DEPLOY TANK", func() {
		log.Printf("[ui_game] DEPLOY TANK clicked")
		_ = gameClient.SendJoin("")
	})
	joinBtn.Importance = widget.SuccessImportance

	shieldBtn = widget.NewButton("🛡️ SHIELD (1/1)", func() {
		log.Printf("[ui_game] SHIELD clicked")
		_ = gameClient.SendShield()
	})

	moveLeftBtn = widget.NewButton("◀ MOVE L", func() {
		log.Printf("[ui_game] MOVE L clicked")
		_ = gameClient.SendMove(streamtankspbv1.MoveAction_DIRECTION_LEFT)
	})

	moveRightBtn = widget.NewButton("MOVE R ▶", func() {
		log.Printf("[ui_game] MOVE R clicked")
		_ = gameClient.SendMove(streamtankspbv1.MoveAction_DIRECTION_RIGHT)
	})

	// Wire WebSocket Callbacks
	gameClient.OnStateUpdate = func(state *streamtankspbv1.ViewerState) {
		log.Printf("[ui_game] OnStateUpdate: phase=%s timer=%ds players=%d tanks=%d canJoin=%v canStart=%v",
			state.Phase, state.TimerRemaining, len(state.Players), len(state.Tanks), state.CanJoin, state.CanStart)
		fyne.Do(func() {
			phaseBadge.SetText(state.Phase)
			if state.TimerRemaining > 0 {
				timerLabel.SetText(fmt.Sprintf("%ds", state.TimerRemaining))
			} else {
				timerLabel.SetText("")
			}

			minimap.SetState(state.Terrain, state.Tanks, state.Phase)

			// 1. Determine if player has joined the match
			isJoined := false
			for _, p := range state.JoinedPlayers {
				if strings.EqualFold(p, ctx.Username) {
					isJoined = true
					break
				}
			}

			// 2. Determine if player is alive (in active alive players or tanks)
			isAlive := false
			for _, p := range state.Players {
				if strings.EqualFold(p, ctx.Username) {
					isAlive = true
					isJoined = true
					break
				}
			}
			if !isAlive {
				for _, t := range state.Tanks {
					if strings.EqualFold(t.Username, ctx.Username) {
						isAlive = true
						isJoined = true
						break
					}
				}
			}

			// 3. Shield state
			isShielded := false
			for _, p := range state.ShieldedPlayers {
				if strings.EqualFold(p, ctx.Username) {
					isShielded = true
					break
				}
			}
			isShieldUsed := false
			for _, p := range state.ShieldUsedPlayers {
				if strings.EqualFold(p, ctx.Username) {
					isShieldUsed = true
					break
				}
			}

			log.Printf("[ui_game] Pilot state: joined=%v alive=%v shielded=%v shield_used=%v",
				isJoined, isAlive, isShielded, isShieldUsed)

			// 4. Update Pilot Status & Deploy Button
			if state.Phase == "IDLE" {
				if isJoined {
					playerStatusBadge.SetText("[DEPLOYED]")
					joinBtn.Disable()
					joinBtn.SetText("DEPLOYED")
				} else {
					playerStatusBadge.SetText("[SPECTATING]")
					joinBtn.Enable()
					joinBtn.SetText("DEPLOY TANK")
				}
			} else {
				// Match in progress
				switch {
				case !isJoined:
					playerStatusBadge.SetText("[SPECTATING]")
					if state.CanJoin {
						joinBtn.Enable()
						joinBtn.SetText("DEPLOY TANK")
					} else {
						joinBtn.Disable()
						joinBtn.SetText("MATCH IN PROGRESS")
					}
				case isAlive:
					playerStatusBadge.SetText("[ACTIVE PILOT]")
					joinBtn.Disable()
					joinBtn.SetText("DEPLOYED")
				default:
					// Joined, but dead!
					playerStatusBadge.SetText("💀 [DESTROYED / KIA]")
					joinBtn.Disable()
					joinBtn.SetText("DESTROYED")
				}
			}

			// 5. Update Action / Combat Controls
			if state.Phase == "INPUT" && isJoined && isAlive {
				fireBtn.Enable()
				moveLeftBtn.Enable()
				moveRightBtn.Enable()
				switch {
				case isShielded:
					shieldBtn.SetText("🛡️ SHIELD ACTIVE")
					shieldBtn.Disable()
				case isShieldUsed:
					shieldBtn.SetText("🛡️ SHIELD (0/1)")
					shieldBtn.Disable()
				default:
					shieldBtn.SetText("🛡️ SHIELD (1/1)")
					shieldBtn.Enable()
				}
			} else {
				fireBtn.Disable()
				moveLeftBtn.Disable()
				moveRightBtn.Disable()
				shieldBtn.Disable()
				switch {
				case isShielded:
					shieldBtn.SetText("🛡️ SHIELD ACTIVE")
				case isShieldUsed:
					shieldBtn.SetText("🛡️ SHIELD (0/1)")
				default:
					shieldBtn.SetText("🛡️ SHIELD (1/1)")
				}
			}
		})
	}

	gameClient.OnDisconnect = func(err error) {
		log.Printf("[ui_game] OnDisconnect: %v", err)
		fyne.Do(func() {
			phaseBadge.SetText("DISCONNECTED")
			timerLabel.SetText("")
			playerStatusBadge.SetText("[OFFLINE]")
			fireBtn.Disable()
			moveLeftBtn.Disable()
			moveRightBtn.Disable()
			shieldBtn.Disable()
			joinBtn.Disable()
		})
	}

	// Connect in background
	go func() {
		log.Printf("[ui_game] Initiating connection to %s...", channel)
		if err := gameClient.Connect(); err != nil {
			log.Printf("[ui_game] Connection failed: %v", err)
			fyne.Do(func() {
				phaseBadge.SetText("DISCONNECTED")
			})
		} else {
			log.Printf("[ui_game] Connect call returned successfully")
		}
	}()

	controls := container.NewVBox(
		aimingRow,
		container.NewGridWithColumns(2, moveLeftBtn, moveRightBtn),
		fireBtn,
		container.NewGridWithColumns(2, joinBtn, shieldBtn),
	)

	content := container.NewVBox(
		topBar,
		widget.NewSeparator(),
		minimap,
		widget.NewSeparator(),
		controls,
	)

	return container.NewPadded(container.NewVScroll(content))
}
