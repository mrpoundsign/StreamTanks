package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestProtractorWidget_AngleClamping(t *testing.T) {
	test.NewApp()
	p := NewProtractorWidget(45)
	if p.Angle() != 45 {
		t.Errorf("expected 45, got %v", p.Angle())
	}

	p.SetAngle(-10)
	if p.Angle() != 0 {
		t.Errorf("expected 0, got %v", p.Angle())
	}

	p.SetAngle(190)
	if p.Angle() != 180 {
		t.Errorf("expected 180, got %v", p.Angle())
	}

	p.SetAngle(67.4)
	if p.Angle() != 67 {
		t.Errorf("expected 67, got %v", p.Angle())
	}
}

func TestProtractorWidget_PositionMath(t *testing.T) {
	p := NewProtractorWidget(45)
	p.Resize(fyne.NewSize(200, 100))

	var changedAngle float64
	p.OnAngleChanged = func(deg float64) {
		changedAngle = deg
	}

	// Directly above pivot (pivot is at x=100, y=84)
	p.updateAngleFromPosition(fyne.NewPos(100, 20))
	if changedAngle != 90 {
		t.Errorf("expected 90° directly above pivot, got %v", changedAngle)
	}

	// Directly to the right (x=180, y=84)
	p.updateAngleFromPosition(fyne.NewPos(180, 84))
	if changedAngle != 0 {
		t.Errorf("expected 0° directly to the right, got %v", changedAngle)
	}

	// Directly to the left (x=20, y=84)
	p.updateAngleFromPosition(fyne.NewPos(20, 84))
	if changedAngle != 180 {
		t.Errorf("expected 180° directly to the left, got %v", changedAngle)
	}
}

func TestProtractorWidget_DrawDoesNotPanic(t *testing.T) {
	p := NewProtractorWidget(45)
	img := p.drawProtractor(280, 140)
	if img == nil || img.Bounds().Dx() != 280 || img.Bounds().Dy() != 140 {
		t.Errorf("unexpected image output: %v", img)
	}

	// Zero size edge case
	zeroImg := p.drawProtractor(0, 0)
	if zeroImg == nil {
		t.Error("expected non-nil image for zero dimensions")
	}
}
