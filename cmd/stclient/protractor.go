package main

import (
	"image"
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// ProtractorWidget is an interactive semi-circular aiming arc supporting drag and tap to aim
type ProtractorWidget struct {
	widget.BaseWidget
	mu             sync.RWMutex
	angle          float64 // 0 to 180 degrees
	OnAngleChanged func(float64)
	raster         *canvas.Raster
}

// Ensure interface implementations
var (
	_ fyne.Tappable  = (*ProtractorWidget)(nil)
	_ fyne.Draggable = (*ProtractorWidget)(nil)
)

func NewProtractorWidget(initialAngle float64) *ProtractorWidget {
	p := &ProtractorWidget{
		angle: initialAngle,
	}
	p.raster = canvas.NewRaster(p.drawProtractor)
	p.ExtendBaseWidget(p)
	return p
}

func (p *ProtractorWidget) Angle() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.angle
}

func (p *ProtractorWidget) SetAngle(angle float64) {
	clamped := math.Round(math.Max(0, math.Min(180, angle)))
	p.mu.Lock()
	if p.angle == clamped {
		p.mu.Unlock()
		return
	}
	p.angle = clamped
	p.mu.Unlock()

	if fyne.CurrentApp() != nil {
		fyne.Do(func() {
			p.raster.Refresh()
		})
	} else if p.raster != nil {
		p.raster.Refresh()
	}
}

func (p *ProtractorWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.raster)
}

func (p *ProtractorWidget) MinSize() fyne.Size {
	return fyne.NewSize(280, 140)
}

func (p *ProtractorWidget) updateAngleFromPosition(pos fyne.Position) {
	size := p.Size()
	w := float64(size.Width)
	h := float64(size.Height)
	if w <= 0 || h <= 0 {
		return
	}

	centerX := w / 2.0
	centerY := h - 16.0

	dx := float64(pos.X) - centerX
	dy := -(float64(pos.Y) - centerY) // Upward is positive

	rad := math.Atan2(dy, dx)
	deg := math.Round((rad * 180.0) / math.Pi)

	if deg < 0 {
		if dx >= 0 {
			deg = 0
		} else {
			deg = 180
		}
	}
	if deg > 180 {
		deg = 180
	}

	p.SetAngle(deg)
	if p.OnAngleChanged != nil {
		p.OnAngleChanged(deg)
	}
}

func (p *ProtractorWidget) Tapped(e *fyne.PointEvent) {
	p.updateAngleFromPosition(e.Position)
}

func (p *ProtractorWidget) Dragged(e *fyne.DragEvent) {
	p.updateAngleFromPosition(e.Position)
}

func (p *ProtractorWidget) DragEnd() {}

// drawProtractor renders the cyberpunk vector protractor into an RGBA pixel buffer
func (p *ProtractorWidget) drawProtractor(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}

	p.mu.RLock()
	currentAngle := p.angle
	p.mu.RUnlock()

	// Colors
	cardBg := color.RGBA{R: 0x12, G: 0x16, B: 0x1e, A: 0xff}       // Tactical dark panel
	cardBorder := color.RGBA{R: 0x00, G: 0xff, B: 0xcc, A: 0x55}   // Neon Cyan border
	arcCyan := color.RGBA{R: 0x00, G: 0xff, B: 0xcc, A: 0xff}       // Neon Cyan #00ffcc
	arcDim := color.RGBA{R: 0x09, G: 0x44, B: 0x44, A: 0xff}        // Dim Cyan
	needleGold := color.RGBA{R: 0xff, G: 0xd7, B: 0x00, A: 0xff}    // Gold #ffd700
	needleGlow := color.RGBA{R: 0xff, G: 0xaa, B: 0x00, A: 0xaa}    // Amber
	pivotColor := color.RGBA{R: 0xff, G: 0x00, B: 0x55, A: 0xff}    // Neon Red/Pink
	headCenter := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}    // White

	// Draw card panel with subtle border
	for y := range h {
		for x := range w {
			if x == 0 || x == w-1 || y == 0 || y == h-1 {
				img.SetRGBA(x, y, cardBorder)
			} else {
				img.SetRGBA(x, y, cardBg)
			}
		}
	}

	centerX := float64(w) / 2.0
	centerY := float64(h) - 16.0
	radius := math.Min(centerX-20.0, centerY-20.0)
	if radius < 30 {
		radius = 30
	}

	// 1. Draw Semi-Circular Outer and Inner Arcs
	steps := int(radius * math.Pi * 2.0)
	for i := range steps {
		theta := (float64(i) / float64(steps)) * math.Pi
		cosT := math.Cos(theta)
		sinT := math.Sin(theta)

		// Outer thick arc
		for dr := -1; dr <= 1; dr++ {
			px := int(centerX + cosT*(radius+float64(dr)))
			py := int(centerY - sinT*(radius+float64(dr)))
			if px >= 0 && px < w && py >= 0 && py < h {
				img.SetRGBA(px, py, arcCyan)
			}
		}

		// Inner reference arc
		innerPx := int(centerX + cosT*(radius-12.0))
		innerPy := int(centerY - sinT*(radius-12.0))
		if innerPx >= 0 && innerPx < w && innerPy >= 0 && innerPy < h {
			img.SetRGBA(innerPx, innerPy, arcDim)
		}
	}

	// 2. Draw Tick Marks (Every 15°, major every 45°)
	for deg := 0; deg <= 180; deg += 15 {
		theta := (float64(deg) * math.Pi) / 180.0
		cosT := math.Cos(theta)
		sinT := math.Sin(theta)

		isMajor := deg%45 == 0
		tickInner := radius - 10.0
		tickOuter := radius + 1.0
		tickCol := arcCyan

		if isMajor {
			tickInner = radius - 18.0
			tickOuter = radius + 3.0
		}

		x0 := int(centerX + cosT*tickInner)
		y0 := int(centerY - sinT*tickInner)
		x1 := int(centerX + cosT*tickOuter)
		y1 := int(centerY - sinT*tickOuter)

		drawBresenhamLine(img, x0, y0, x1, y1, tickCol)
		if isMajor {
			// Thicken major ticks
			drawBresenhamLine(img, x0+1, y0, x1+1, y1, tickCol)
		}
	}

	// 3. Draw Baseline
	drawBresenhamLine(img, int(centerX-radius), int(centerY), int(centerX+radius), int(centerY), arcDim)

	// 4. Draw Aiming Needle
	needleRad := (currentAngle * math.Pi) / 180.0
	needleCos := math.Cos(needleRad)
	needleSin := math.Sin(needleRad)

	tipX := int(centerX + needleCos*radius)
	tipY := int(centerY - needleSin*radius)

	// Needle body line with slight glow
	drawBresenhamLine(img, int(centerX), int(centerY), tipX, tipY, needleGold)
	drawBresenhamLine(img, int(centerX)+1, int(centerY), tipX+1, tipY, needleGlow)

	// 5. Draw Draggable Handle at Needle Tip
	handleRadius := 6
	for dx := -handleRadius; dx <= handleRadius; dx++ {
		for dy := -handleRadius; dy <= handleRadius; dy++ {
			distSq := dx*dx + dy*dy
			if distSq <= handleRadius*handleRadius {
				hx := tipX + dx
				hy := tipY + dy
				if hx >= 0 && hx < w && hy >= 0 && hy < h {
					if distSq <= 4 {
						img.SetRGBA(hx, hy, headCenter)
					} else {
						img.SetRGBA(hx, hy, needleGold)
					}
				}
			}
		}
	}

	// 6. Center Pivot
	pivotR := 4
	for dx := -pivotR; dx <= pivotR; dx++ {
		for dy := -pivotR; dy <= pivotR; dy++ {
			if dx*dx+dy*dy <= pivotR*pivotR {
				px := int(centerX) + dx
				py := int(centerY) + dy
				if px >= 0 && px < w && py >= 0 && py < h {
					img.SetRGBA(px, py, pivotColor)
				}
			}
		}
	}

	return img
}
