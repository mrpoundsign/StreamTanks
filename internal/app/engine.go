package app

import (
	"math"
	"slices"
)

// CollisionSink receives collision, elimination, and spark events during physics stepping.
// Consumers implement this interface to handle live broadcast/storage or in-memory recording.
type CollisionSink interface {
	OnCrater(cx, cy, radius float64, shotID string)
	OnKill(victim, killer string, cx, cy float64, isBot bool)
	OnSpark(x, y float64)
}

// Engine encapsulates match simulation state and physics calculations.
type Engine struct {
	Terrain      []float64
	Players      map[string]*Player
	Projectiles  []Projectile
	Explosions   []Explosion
	BouncyWalls  bool
	TerrainClimb bool
	MoveDistance int
	BotPoints    int
}

// NewEngine constructs a simulation engine.
func NewEngine(terrain []float64, players map[string]*Player, bouncyWalls, terrainClimb bool, moveDistance, botPoints int) *Engine {
	return &Engine{
		Terrain:      terrain,
		Players:      players,
		BouncyWalls:  bouncyWalls,
		TerrainClimb: terrainClimb,
		MoveDistance: moveDistance,
		BotPoints:    botPoints,
	}
}

// sortedPlayerNames returns player names sorted alphabetically for deterministic iteration.
func (e *Engine) sortedPlayerNames() []string {
	names := make([]string, 0, len(e.Players))
	for name := range e.Players {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Step advances the physics simulation by dtScale and notifies the sink of events.
// Returns true when the action phase has concluded (no moving projectiles or falling tanks).
func (e *Engine) Step(dtScale float64, sink CollisionSink) bool {
	anyMoving, anyFalling := e.UpdateTankMovements(dtScale, sink)
	e.UpdateProjectiles(dtScale, sink)

	// Update explosions
	for i := len(e.Explosions) - 1; i >= 0; i-- {
		exp := &e.Explosions[i]
		exp.Radius += 2.0 * dtScale
		exp.Alpha -= 0.05 * dtScale
		if exp.Alpha <= 0 {
			e.Explosions = append(e.Explosions[:i], e.Explosions[i+1:]...)
		}
	}

	// Completion check: no projectiles, no explosions, no tanks moving or falling
	if len(e.Projectiles) == 0 && len(e.Explosions) == 0 && !anyMoving && !anyFalling {
		return true
	}

	return false
}

// UpdateTankMovements executes tank translation, boundary clamping, and falling.
func (e *Engine) UpdateTankMovements(dtScale float64, sink CollisionSink) (bool, bool) {
	anyMoving := false
	anyFalling := false
	for _, name := range e.sortedPlayerNames() {
		p := e.Players[name]
		if p.IsDead {
			continue
		}

		if p.Moving {
			anyMoving = true
			currentSpeed := p.SpeedMultiplier * 2.0 * dtScale
			switch p.ActionType {
			case actionLeft:
				nextX := p.X - currentSpeed
				currIdx := int(math.Floor(p.X))
				nextIdx := int(math.Floor(nextX))
				blocked := false
				if !e.TerrainClimb && currIdx != nextIdx {
					stepX := math.Abs(float64(currIdx - nextIdx))
					rise := getTerrainHeight(e.Terrain, float64(currIdx)) - getTerrainHeight(e.Terrain, float64(nextIdx))
					if rise > 0 && (rise/stepX) > 4.0 {
						blocked = true
					}
				}
				if blocked {
					p.Moving = false
				} else {
					p.X = nextX
					if p.X <= 20 {
						if e.BouncyWalls && !p.HasBounced {
							p.X = 20
							p.ActionType = actionRight
							p.MoveTarget = p.X + float64(e.MoveDistance)
							p.SpeedMultiplier = 1.5
							p.HasBounced = true
							e.Explosions = append(e.Explosions, Explosion{
								X:         20,
								Y:         p.Y,
								Radius:    0,
								MaxRadius: 30,
								Alpha:     1.0,
								IsSpark:   true,
							})
							if sink != nil {
								sink.OnSpark(20, p.Y)
							}
						} else if (p.MoveTarget != 0 && p.X <= p.MoveTarget) || p.X <= 20 {
							p.Moving = false
							if p.X < 20 {
								p.X = 20
							}
						}
					} else if p.MoveTarget != 0 && p.X <= p.MoveTarget {
						p.Moving = false
					}
				}
			case actionRight:
				nextX := p.X + currentSpeed
				currIdx := int(math.Floor(p.X))
				nextIdx := int(math.Floor(nextX))
				blocked := false
				if !e.TerrainClimb && currIdx != nextIdx {
					stepX := math.Abs(float64(currIdx - nextIdx))
					rise := getTerrainHeight(e.Terrain, float64(currIdx)) - getTerrainHeight(e.Terrain, float64(nextIdx))
					if rise > 0 && (rise/stepX) > 4.0 {
						blocked = true
					}
				}
				if blocked {
					p.Moving = false
				} else {
					p.X = nextX
					if p.X >= defaultTerrainWidth-20 {
						if e.BouncyWalls && !p.HasBounced {
							p.X = defaultTerrainWidth - 20
							p.ActionType = actionLeft
							p.MoveTarget = p.X - float64(e.MoveDistance)
							p.SpeedMultiplier = 1.5
							p.HasBounced = true
							e.Explosions = append(e.Explosions, Explosion{
								X:         defaultTerrainWidth - 20,
								Y:         p.Y,
								Radius:    0,
								MaxRadius: 30,
								Alpha:     1.0,
								IsSpark:   true,
							})
							if sink != nil {
								sink.OnSpark(defaultTerrainWidth-20, p.Y)
							}
						} else if (p.MoveTarget != 0 && p.X >= p.MoveTarget) || p.X >= defaultTerrainWidth-20 {
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
		}

		// Clamping
		if p.X < 20 {
			p.X = 20
		}
		if p.X > defaultTerrainWidth-20 {
			p.X = defaultTerrainWidth - 20
		}

		// Ground snapping / falling
		floorY := getTerrainHeight(e.Terrain, p.X)
		if p.Y < floorY {
			p.Y += 5.0 * dtScale
			if p.Y > floorY {
				p.Y = floorY
			} else if p.Y < floorY {
				anyFalling = true
			}
		} else {
			p.Y = floorY
		}

		// Fall off bottom of screen into abyss
		if p.Y >= defaultTerrainHeight {
			if !p.IsDead {
				p.IsDead = true
				if sink != nil {
					sink.OnKill(p.Name, "", p.X, p.Y, p.IsBot)
				}
			}
		}
	}
	return anyMoving, anyFalling
}

// UpdateProjectiles updates projectile ballistics, boundary ricochets, and collision detection.
func (e *Engine) UpdateProjectiles(dtScale float64, sink CollisionSink) {
	const gravity = 0.2
	for i := len(e.Projectiles) - 1; i >= 0; i-- {
		proj := &e.Projectiles[i]
		proj.X += proj.VX * dtScale
		proj.VY += gravity * dtScale
		proj.Y += proj.VY * dtScale

		hit := false

		// Ceiling & Floor
		if proj.Y < 0 {
			if e.BouncyWalls {
				proj.Y = 0
				proj.VY = math.Abs(proj.VY) * 1.1
				proj.VX *= 1.1
				proj.Bounces++
				cx := math.Max(0, math.Min(defaultTerrainWidth, proj.X))
				e.Explosions = append(e.Explosions, Explosion{
					X:         cx,
					Y:         0,
					Radius:    0,
					MaxRadius: 30,
					Alpha:     1.0,
					IsSpark:   true,
				})
				if sink != nil {
					sink.OnSpark(cx, 0)
				}
				if proj.Bounces > 15 {
					hit = true
				}
			}
		} else if proj.Y > defaultTerrainHeight {
			if e.BouncyWalls {
				proj.Y = defaultTerrainHeight
				proj.VY = -math.Abs(proj.VY) * 1.1
				proj.VX *= 1.1
				proj.Bounces++
				cx := math.Max(0, math.Min(defaultTerrainWidth, proj.X))
				e.Explosions = append(e.Explosions, Explosion{
					X:         cx,
					Y:         defaultTerrainHeight,
					Radius:    0,
					MaxRadius: 30,
					Alpha:     1.0,
					IsSpark:   true,
				})
				if sink != nil {
					sink.OnSpark(cx, defaultTerrainHeight)
				}
				if proj.Bounces > 15 {
					hit = true
				}
			} else {
				hit = true
			}
		}

		// Side walls
		if !hit {
			if proj.X < 0 {
				if e.BouncyWalls {
					proj.X = 0
					proj.VX = math.Abs(proj.VX) * 1.1
					proj.VY *= 1.1
					proj.Bounces++
					cy := math.Max(0, math.Min(defaultTerrainHeight, proj.Y))
					e.Explosions = append(e.Explosions, Explosion{
						X:         0,
						Y:         cy,
						Radius:    0,
						MaxRadius: 30,
						Alpha:     1.0,
						IsSpark:   true,
					})
					if sink != nil {
						sink.OnSpark(0, cy)
					}
					if proj.Bounces > 15 {
						hit = true
					}
				} else {
					hit = true
				}
			} else if proj.X > defaultTerrainWidth {
				if e.BouncyWalls {
					proj.X = defaultTerrainWidth
					proj.VX = -math.Abs(proj.VX) * 1.1
					proj.VY *= 1.1
					proj.Bounces++
					cy := math.Max(0, math.Min(defaultTerrainHeight, proj.Y))
					e.Explosions = append(e.Explosions, Explosion{
						X:         defaultTerrainWidth,
						Y:         cy,
						Radius:    0,
						MaxRadius: 30,
						Alpha:     1.0,
						IsSpark:   true,
					})
					if sink != nil {
						sink.OnSpark(defaultTerrainWidth, cy)
					}
					if proj.Bounces > 15 {
						hit = true
					}
				} else {
					hit = true
				}
			}
		}

		// Active shield collision: completely absorbs projectile before terrain impact
		if !hit {
			for _, name := range e.sortedPlayerNames() {
				p := e.Players[name]
				if name == proj.Owner || p.IsDead || !p.IsShielded {
					continue
				}
				if math.Hypot(p.X-proj.X, p.Y-proj.Y) < 45 && proj.Y <= p.Y+5 {
					hit = true
					e.Explosions = append(e.Explosions, Explosion{
						X:         proj.X,
						Y:         proj.Y,
						Radius:    0,
						MaxRadius: 30,
						Alpha:     1.0,
						IsSpark:   true,
					})
					if sink != nil {
						sink.OnSpark(proj.X, proj.Y)
					}
					break
				}
			}
		}

		// Terrain collision
		groundY := getTerrainHeight(e.Terrain, proj.X)
		if !hit && proj.Y >= 0 && proj.Y >= groundY {
			hit = true
			applyCrater(e.Terrain, proj.X, groundY, 50.0)
			e.Explosions = append(e.Explosions, Explosion{
				X:         proj.X,
				Y:         groundY,
				Radius:    0,
				MaxRadius: 50.0,
				Alpha:     1.0,
				IsSpark:   false,
			})
			if sink != nil {
				sink.OnCrater(proj.X, groundY, 50.0, proj.ID)
			}
			e.CheckTankCollisions(proj.X, groundY, 50.0, proj.Owner, sink)
		}

		// Direct tank collision
		if !hit {
			for _, name := range e.sortedPlayerNames() {
				p := e.Players[name]
				if name == proj.Owner || p.IsDead || p.IsShielded {
					continue
				}
				if math.Hypot(p.X-proj.X, p.Y-proj.Y) < 20 {
					hit = true
					applyCrater(e.Terrain, proj.X, proj.Y, 50.0)
					e.Explosions = append(e.Explosions, Explosion{
						X:         proj.X,
						Y:         proj.Y,
						Radius:    0,
						MaxRadius: 50.0,
						Alpha:     1.0,
						IsSpark:   false,
					})
					if sink != nil {
						sink.OnCrater(proj.X, proj.Y, 50.0, proj.ID)
					}
					e.CheckTankCollisions(proj.X, proj.Y, 50.0, proj.Owner, sink)
					break
				}
			}
		}

		if hit {
			e.Projectiles = append(e.Projectiles[:i], e.Projectiles[i+1:]...)
		}
	}
}

// CheckTankCollisions evaluates blast radius against all tanks and triggers eliminations.
func (e *Engine) CheckTankCollisions(cx, cy, radius float64, owner string, sink CollisionSink) {
	for _, name := range e.sortedPlayerNames() {
		p := e.Players[name]
		if name == owner || p.IsDead || p.IsShielded {
			continue
		}
		dist := math.Hypot(p.X-cx, p.Y-cy)
		if dist < radius+20.0 {
			p.IsDead = true
			if sink != nil {
				sink.OnKill(name, owner, cx, cy, p.IsBot)
			}
		}
	}
}
