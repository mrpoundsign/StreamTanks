package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestPowerGaugeWidget_Clamping(t *testing.T) {
	test.NewApp()
	p := NewPowerGaugeWidget(50)
	if p.Power() != 50 {
		t.Errorf("expected 50, got %v", p.Power())
	}

	p.SetPower(-5)
	if p.Power() != 1 {
		t.Errorf("expected 1, got %v", p.Power())
	}

	p.SetPower(150)
	if p.Power() != 100 {
		t.Errorf("expected 100, got %v", p.Power())
	}

	p.SetPower(72.4)
	if p.Power() != 72 {
		t.Errorf("expected 72, got %v", p.Power())
	}
}

func TestPowerGaugeWidget_PositionMath(t *testing.T) {
	test.NewApp()
	p := NewPowerGaugeWidget(50)
	p.Resize(fyne.NewSize(68, 140))

	var changedPower float64
	p.OnPowerChanged = func(val float64) {
		changedPower = val
	}

	// Near top -> 100%
	p.updateFromPosition(fyne.NewPos(34, 5))
	if changedPower != 100 {
		t.Errorf("expected 100 at top, got %v", changedPower)
	}

	// Near bottom -> 1%
	p.updateFromPosition(fyne.NewPos(34, 138))
	if changedPower != 1 {
		t.Errorf("expected 1 at bottom, got %v", changedPower)
	}

	// Middle -> ~50%
	p.updateFromPosition(fyne.NewPos(34, 70))
	if changedPower < 45 || changedPower > 55 {
		t.Errorf("expected ~50 near middle, got %v", changedPower)
	}
}

func TestPowerGaugeWidget_DrawDoesNotPanic(t *testing.T) {
	p := NewPowerGaugeWidget(50)
	img := p.drawPowerGauge(68, 140)
	if img == nil || img.Bounds().Dx() != 68 || img.Bounds().Dy() != 140 {
		t.Errorf("unexpected image output: %v", img)
	}

	// Zero dimensions edge case
	zeroImg := p.drawPowerGauge(0, 0)
	if zeroImg == nil {
		t.Error("expected non-nil image for zero dimensions")
	}
}
