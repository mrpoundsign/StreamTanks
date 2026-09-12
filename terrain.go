package main

import (
	"math"
	"math/rand/v2"
)

const (
	defaultTerrainWidth  = 1920
	defaultTerrainHeight = 1080
)

// generateTerrain creates a 1920-point heightmap representing the terrain surface Y for each X.
func generateTerrain() []float64 {
	terrain := make([]float64, defaultTerrainWidth)
	y := float64(defaultTerrainHeight)/2.0 + (rand.Float64()*200.0 - 100.0)
	slope := 0.0
	terrain[0] = math.Round(y*10) / 10

	for x := 1; x < defaultTerrainWidth; x++ {
		slope += (rand.Float64() - 0.5) * 0.15
		if slope > 2.0 {
			slope = 2.0
		}
		if slope < -2.0 {
			slope = -2.0
		}

		y += slope

		// Softly push back towards the center if getting too close to edges
		if y < 250.0 {
			slope += 0.05
		}
		if y > float64(defaultTerrainHeight)-200.0 {
			slope -= 0.05
		}

		terrain[x] = math.Round(y*10) / 10
	}

	return terrain
}

// getTerrainHeight returns the terrain surface Y at coordinate x (clamped to screen boundaries).
func getTerrainHeight(terrain []float64, x float64) float64 {
	if len(terrain) == 0 {
		return float64(defaultTerrainHeight) / 2.0
	}
	idx := int(math.Floor(x))
	if idx < 0 {
		idx = 0
	} else if idx >= len(terrain) {
		idx = len(terrain) - 1
	}
	return terrain[idx]
}

// applyCrater modifies the terrain heightmap to carve out a circular crater centered at (cx, cy) with the given radius.
func applyCrater(terrain []float64, cx, cy, radius float64) {
	if len(terrain) == 0 || radius <= 0 {
		return
	}
	startX := int(math.Max(0, math.Floor(cx-radius)))
	endX := int(math.Min(float64(len(terrain)), math.Ceil(cx+radius)))
	r2 := radius * radius

	for x := startX; x < endX; x++ {
		dx := float64(x) - cx
		dy := math.Sqrt(r2 - dx*dx)
		circleBottomY := cy + dy

		if terrain[x] < circleBottomY {
			terrain[x] = math.Round(circleBottomY*10) / 10
		}
	}
}
