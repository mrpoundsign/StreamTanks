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

// PowerGaugeWidget is a vertical tactical power slider matching the mobile extension
type PowerGaugeWidget struct {
	widget.BaseWidget
	mu             sync.RWMutex
	power          float64 // 1 to 100
	OnPowerChanged func(float64)
	raster         *canvas.Raster
}

var (
	_ fyne.Tappable  = (*PowerGaugeWidget)(nil)
	_ fyne.Draggable = (*PowerGaugeWidget)(nil)
)

func NewPowerGaugeWidget(initialPower float64) *PowerGaugeWidget {
	p := &PowerGaugeWidget{
		power: math.Max(1, math.Min(100, initialPower)),
	}
	p.raster = canvas.NewRaster(p.drawPowerGauge)
	p.ExtendBaseWidget(p)
	return p
}

func (p *PowerGaugeWidget) Power() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.power
}

func (p *PowerGaugeWidget) SetPower(val float64) {
	clamped := math.Round(math.Max(1, math.Min(100, val)))
	p.mu.Lock()
	if p.power == clamped {
		p.mu.Unlock()
		return
	}
	p.power = clamped
	p.mu.Unlock()

	if fyne.CurrentApp() != nil {
		fyne.Do(func() {
			p.raster.Refresh()
		})
	} else if p.raster != nil {
		p.raster.Refresh()
	}
}

func (p *PowerGaugeWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.raster)
}

func (p *PowerGaugeWidget) MinSize() fyne.Size {
	return fyne.NewSize(68, 140)
}

func (p *PowerGaugeWidget) updateFromPosition(pos fyne.Position) {
	size := p.Size()
	h := float64(size.Height)
	if h <= 0 {
		return
	}

	trackTop := 8.0
	trackBottom := h - 8.0
	trackH := trackBottom - trackTop
	if trackH <= 0 {
		return
	}

	ratio := 1.0 - (float64(pos.Y)-trackTop)/trackH
	ratio = math.Max(0.01, math.Min(1.0, ratio))
	newPower := math.Round(ratio * 100.0)

	p.SetPower(newPower)
	if p.OnPowerChanged != nil {
		p.OnPowerChanged(newPower)
	}
}

func (p *PowerGaugeWidget) Tapped(e *fyne.PointEvent) {
	p.updateFromPosition(e.Position)
}

func (p *PowerGaugeWidget) Dragged(e *fyne.DragEvent) {
	p.updateFromPosition(e.Position)
}

func (p *PowerGaugeWidget) DragEnd() {}

func (p *PowerGaugeWidget) drawPowerGauge(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}

	p.mu.RLock()
	currentPower := p.power
	p.mu.RUnlock()

	// Theme Colors
	cardBg := color.RGBA{R: 0x12, G: 0x16, B: 0x1e, A: 0xff}       // Dark panel
	cardBorder := color.RGBA{R: 0xff, G: 0x00, B: 0x7f, A: 0x66}   // Magenta border
	trackBg := color.RGBA{R: 0x1f, G: 0x24, B: 0x30, A: 0xff}      // Dark inner track
	trackBorder := color.RGBA{R: 0xff, G: 0x00, B: 0x7f, A: 0xaa}  // Magenta track border
	thumbWhite := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}   // Crisp white thumb
	thumbMagenta := color.RGBA{R: 0xff, G: 0x33, B: 0x99, A: 0xff} // Thumb highlight
	tickColor := color.RGBA{R: 0x66, G: 0x77, B: 0x88, A: 0xff}    // Ticks

	// 1. Draw Card Background with Border
	for y := range h {
		for x := range w {
			if x == 0 || x == w-1 || y == 0 || y == h-1 {
				img.SetRGBA(x, y, cardBorder)
			} else {
				img.SetRGBA(x, y, cardBg)
			}
		}
	}

	trackTop := 10
	trackBottom := h - 10
	trackH := trackBottom - trackTop
	if trackH <= 0 {
		return img
	}

	centerX := w / 2
	trackWidth := 18
	halfW := trackWidth / 2
	trackLeft := centerX - halfW
	trackRight := centerX + halfW

	// 2. Draw Track Background
	for y := trackTop; y <= trackBottom; y++ {
		for x := trackLeft; x <= trackRight; x++ {
			if x == trackLeft || x == trackRight || y == trackTop || y == trackBottom {
				img.SetRGBA(x, y, trackBorder)
			} else {
				img.SetRGBA(x, y, trackBg)
			}
		}
	}

	// 3. Draw Graduation Ticks (25%, 50%, 75%)
	for _, pct := range []float64{0.25, 0.50, 0.75} {
		tickY := trackBottom - int(pct*float64(trackH))
		// Left and right tick notches
		for x := trackLeft - 4; x < trackLeft; x++ {
			if x >= 0 {
				img.SetRGBA(x, tickY, tickColor)
			}
		}
		for x := trackRight + 1; x <= trackRight+4; x++ {
			if x < w {
				img.SetRGBA(x, tickY, tickColor)
			}
		}
	}

	// 4. Draw Neon Magenta Power Fill
	fillH := int((currentPower / 100.0) * float64(trackH))
	fillTop := max(trackBottom-fillH, trackTop)

	for y := fillTop; y < trackBottom; y++ {
		// Subtle gradient from top (bright) to bottom (rich)
		progress := float64(trackBottom-y) / float64(trackH)
		r := uint8(math.Min(255, 210+progress*45))
		g := uint8(math.Min(255, progress*50))
		b := uint8(math.Min(255, 100+progress*50))
		fillCol := color.RGBA{R: r, G: g, B: b, A: 0xff}

		for x := trackLeft + 1; x < trackRight; x++ {
			img.SetRGBA(x, y, fillCol)
		}
	}

	// 5. Draw Glowing Thumb / Indicator Bar at the current level
	thumbY := fillTop
	for dy := -2; dy <= 2; dy++ {
		ty := thumbY + dy
		if ty >= 0 && ty < h {
			for x := trackLeft - 3; x <= trackRight+3; x++ {
				if x >= 0 && x < w {
					if dy == 0 {
						img.SetRGBA(x, ty, thumbWhite)
					} else {
						img.SetRGBA(x, ty, thumbMagenta)
					}
				}
			}
		}
	}

	return img
}
