package main

import (
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	streamtankspbv1 "streamtanks/internal/proto/streamtanks/v1"
)

// MinimapWidget renders the real-time 1920x1080 destructible terrain and tanks
type MinimapWidget struct {
	widget.BaseWidget
	raster          *canvas.Raster
	mu              sync.RWMutex
	terrain         []int32
	tanks           []*streamtankspbv1.TankState
	phase           string
	currentUsername string
	currentAngle    float64
	currentPower    float64
}

func NewMinimapWidget(currentUsername string) *MinimapWidget {
	m := &MinimapWidget{
		currentUsername: currentUsername,
		currentAngle:    45,
		currentPower:    60,
	}
	m.raster = canvas.NewRaster(m.drawMinimap)
	m.ExtendBaseWidget(m)
	return m
}

func (m *MinimapWidget) SetState(terrain []int32, tanks []*streamtankspbv1.TankState, phase ...string) {
	m.mu.Lock()
	if len(terrain) > 0 {
		m.terrain = terrain
	}
	m.tanks = tanks
	if len(phase) > 0 {
		m.phase = phase[0]
	}
	m.mu.Unlock()

	if fyne.CurrentApp() != nil {
		fyne.Do(func() {
			m.raster.Refresh()
		})
	} else if m.raster != nil {
		m.raster.Refresh()
	}
}

func (m *MinimapWidget) SetAim(angle, power float64) {
	m.mu.Lock()
	m.currentAngle = angle
	m.currentPower = power
	m.mu.Unlock()

	if fyne.CurrentApp() != nil {
		fyne.Do(func() {
			m.raster.Refresh()
		})
	} else if m.raster != nil {
		m.raster.Refresh()
	}
}

func (m *MinimapWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.raster)
}

func (m *MinimapWidget) MinSize() fyne.Size {
	return fyne.NewSize(380, 214) // 16:9 aspect ratio standard
}

// drawMinimap renders pixels into an image buffer
func (m *MinimapWidget) drawMinimap(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}

	m.mu.RLock()
	terrain := m.terrain
	tanks := m.tanks
	currUser := m.currentUsername
	aimAngle := m.currentAngle
	aimPower := m.currentPower
	m.mu.RUnlock()

	// Theme colors
	skyColor := color.RGBA{R: 0x0b, G: 0x0c, B: 0x10, A: 0xff}         // #0b0c10
	surfaceColor := color.RGBA{R: 0x00, G: 0xff, B: 0xcc, A: 0xff}     // Neon Cyan #00ffcc
	undergroundColor := color.RGBA{R: 0x09, G: 0x22, B: 0x22, A: 0xff} // Dark Teal #092222

	hasTerrain := len(terrain) == 1920

	// 1. Draw Terrain
	for x := range w {
		var surfaceY int
		if hasTerrain {
			worldX := (x * 1920) / w
			if worldX >= 1920 {
				worldX = 1919
			}
			worldY := int(terrain[worldX])
			surfaceY = (worldY * h) / 1080
		} else {
			surfaceY = (h * 3) / 4 // flat default
		}

		for y := range h {
			switch {
			case y < surfaceY:
				img.SetRGBA(x, y, skyColor)
			case y <= surfaceY+2:
				img.SetRGBA(x, y, surfaceColor)
			default:
				img.SetRGBA(x, y, undergroundColor)
			}
		}
	}

	// 2. Draw Tanks
	tankWidth := int(math.Max(6, float64(w)/100))
	tankHeight := int(math.Max(4, float64(h)/80))

	for _, tank := range tanks {
		if tank.Health <= 0 {
			continue
		}

		tankPxX := int((float64(tank.X) * float64(w)) / 1920.0)
		tankPxY := int((float64(tank.Y) * float64(h)) / 1080.0)

		isMe := strings.EqualFold(tank.Username, currUser)

		tankColor := color.RGBA{R: 0xff, G: 0x6b, B: 0x6b, A: 0xff} // enemy red/coral
		if isMe {
			tankColor = color.RGBA{R: 0x00, G: 0xff, B: 0xcc, A: 0xff} // player bright neon cyan
		} else if tank.IsBot {
			tankColor = color.RGBA{R: 0x88, G: 0x99, B: 0xa6, A: 0xff} // bot silver/slate
		}

		// Draw tank body
		for dx := -tankWidth; dx <= tankWidth; dx++ {
			for dy := -tankHeight; dy <= tankHeight; dy++ {
				px := tankPxX + dx
				py := tankPxY + dy
				if px >= 0 && px < w && py >= 0 && py < h {
					img.SetRGBA(px, py, tankColor)
				}
			}
		}

		// Draw glowing gold border around player tank body
		if isMe {
			gold := color.RGBA{R: 0xff, G: 0xd7, B: 0x00, A: 0xff}
			for dx := -tankWidth - 1; dx <= tankWidth+1; dx++ {
				for dy := -tankHeight - 1; dy <= tankHeight+1; dy++ {
					if dx == -tankWidth-1 || dx == tankWidth+1 || dy == -tankHeight-1 || dy == tankHeight+1 {
						px := tankPxX + dx
						py := tankPxY + dy
						if px >= 0 && px < w && py >= 0 && py < h {
							img.SetRGBA(px, py, gold)
						}
					}
				}
			}
		}

		// Draw health bar above tank
		healthBarWidth := tankWidth * 2
		healthWidth := (int(tank.Health) * healthBarWidth) / 100
		barY := tankPxY - tankHeight - 4

		for dx := -tankWidth; dx <= tankWidth; dx++ {
			px := tankPxX + dx
			if px >= 0 && px < w && barY >= 0 && barY < h {
				if dx+tankWidth < healthWidth {
					img.SetRGBA(px, barY, color.RGBA{R: 0x00, G: 0xff, B: 0x88, A: 0xff}) // green health
				} else {
					img.SetRGBA(px, barY, color.RGBA{R: 0x55, G: 0x00, B: 0x00, A: 0xff}) // depleted red
				}
			}
		}

		// Draw player/bot name label badge above tank
		var label string
		var textColor color.RGBA
		var borderColor color.RGBA

		switch {
		case isMe:
			label = truncateName(tank.Username, 10)
			textColor = color.RGBA{R: 0xff, G: 0xd7, B: 0x00, A: 0xff} // Gold
			borderColor = color.RGBA{R: 0xff, G: 0xd7, B: 0x00, A: 0xff}
		case tank.IsBot:
			// Do not display bot names
			label = ""
		default:
			label = truncateName(tank.Username, 10)
			textColor = color.RGBA{R: 0x00, G: 0xe5, B: 0xff, A: 0xff} // Cyan
			borderColor = color.RGBA{R: 0x00, G: 0xaa, B: 0xcc, A: 0xff}
		}

		if label != "" {
			drawTankLabel(img, tankPxX, barY-2, label, textColor, borderColor, isMe)
		}

		// Draw aiming needle for current player
		if isMe {
			angleRad := (aimAngle * math.Pi) / 180.0
			rayLen := float64(tankWidth) * 3.0 * (math.Max(0.4, aimPower/100.0))
			rayEndX := tankPxX + int(math.Cos(angleRad)*rayLen)
			rayEndY := tankPxY - int(math.Sin(angleRad)*rayLen)

			drawBresenhamLine(img, tankPxX, tankPxY, rayEndX, rayEndY, color.RGBA{R: 0xff, G: 0xd7, B: 0x00, A: 0xff}) // Gold aim needle
		}
	}

	return img
}

// truncateName truncates names longer than maxLen with trailing dots
func truncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	if maxLen <= 2 {
		return name[:maxLen]
	}
	return name[:maxLen-2] + ".."
}

// drawTankLabel renders a cyberpunk pill badge with player/bot username above a tank
func drawTankLabel(img *image.RGBA, centerX, bottomY int, text string, textCol, borderCol color.RGBA, isMe bool) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return
	}

	charW := 7
	charH := 13
	textW := len(text) * charW

	padX := 3
	pillW := textW + padX*2
	pillH := charH + 2
	chevronH := 0
	if isMe {
		chevronH = 3
	}

	pillX := centerX - pillW/2
	pillY := bottomY - pillH - chevronH

	// Clamp to canvas boundaries
	if pillX < 2 {
		pillX = 2
	}
	if pillX+pillW > w-2 {
		pillX = w - 2 - pillW
	}
	if pillY < 2 {
		pillY = 2
	}

	bgCol := color.RGBA{R: 0x0b, G: 0x0c, B: 0x10, A: 0xee} // Dark translucent navy

	// 1. Draw pill background
	for y := pillY; y < pillY+pillH; y++ {
		for x := pillX; x < pillX+pillW; x++ {
			if image.Pt(x, y).In(bounds) {
				img.SetRGBA(x, y, bgCol)
			}
		}
	}

	// 2. Draw 1px border around pill
	for x := pillX; x < pillX+pillW; x++ {
		if image.Pt(x, pillY).In(bounds) {
			img.SetRGBA(x, pillY, borderCol)
		}
		if image.Pt(x, pillY+pillH-1).In(bounds) {
			img.SetRGBA(x, pillY+pillH-1, borderCol)
		}
	}
	for y := pillY; y < pillY+pillH; y++ {
		if image.Pt(pillX, y).In(bounds) {
			img.SetRGBA(pillX, y, borderCol)
		}
		if image.Pt(pillX+pillW-1, y).In(bounds) {
			img.SetRGBA(pillX+pillW-1, y, borderCol)
		}
	}

	// 3. If isMe, draw small downward pointer chevron connecting pill to tank
	if isMe {
		for dy := range 3 {
			cy := pillY + pillH + dy
			hw := 2 - dy
			for dx := -hw; dx <= hw; dx++ {
				cx := centerX + dx
				if image.Pt(cx, cy).In(bounds) {
					img.SetRGBA(cx, cy, borderCol)
				}
			}
		}
	}

	// 4. Render text glyphs using basicfont
	textX := pillX + padX
	textY := pillY + 11 // Baseline offset for Face7x13 (ascent is 11)
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(textCol),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(textX, textY),
	}
	d.DrawString(text)
}

// drawBresenhamLine renders a single-pixel line into an RGBA image
func drawBresenhamLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	bounds := img.Bounds()
	dx := int(math.Abs(float64(x1 - x0)))
	dy := int(math.Abs(float64(y1 - y0)))
	sx := 1
	if x0 >= x1 {
		sx = -1
	}
	sy := 1
	if y0 >= y1 {
		sy = -1
	}
	err := dx - dy

	for {
		if image.Pt(x0, y0).In(bounds) {
			img.SetRGBA(x0, y0, col)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}
