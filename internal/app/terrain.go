package app

import (
	"math"
	"math/rand/v2"
)

const (
	defaultTerrainWidth  = 1920
	defaultTerrainHeight = 1080
)

// generateTerrain creates a 1920-point heightmap representing the terrain surface Y for each X,
// bounded vertically between minPct and maxPct percentage of the screen height measured from the bottom up
// (0% = screen bottom, 100% = screen top).
func generateTerrain(minPct, maxPct int) []float64 {
	if minPct < 10 {
		minPct = 10
	}
	if maxPct > 90 {
		maxPct = 90
	}
	if minPct > maxPct-10 {
		minPct = 20
		maxPct = 75
	}

	// In canvas space, Y=0 is top and Y=1080 is bottom.
	// Height measured from bottom up means:
	// - maxPct height corresponds to the lowest canvas Y (highest point toward top of screen).
	// - minPct height corresponds to the highest canvas Y (lowest point toward bottom of screen).
	minY := float64(defaultTerrainHeight) * (1.0 - float64(maxPct)/100.0)
	maxY := float64(defaultTerrainHeight) * (1.0 - float64(minPct)/100.0)
	midY := (minY + maxY) / 2.0
	halfSpan := (maxY - minY) / 4.0

	terrain := make([]float64, defaultTerrainWidth)
	y := midY + (rand.Float64()*halfSpan*2.0 - halfSpan)
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
		if y < minY+40.0 {
			slope += 0.08
		}
		if y > maxY-40.0 {
			slope -= 0.08
		}

		// Hard clamp within [minY, maxY]
		if y < minY {
			y = minY
			if slope < 0 {
				slope = 0
			}
		} else if y > maxY {
			y = maxY
			if slope > 0 {
				slope = 0
			}
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

	lowestY := cy + radius

	for x := startX; x < endX; x++ {
		// Quick exit: If the current terrain is already at or below the lowest possible
		// point of the crater, skip the calculations entirely.
		if terrain[x] >= lowestY {
			continue
		}

		dx := float64(x) - cx
		dx2 := dx * dx
		if dx2 > r2 {
			continue
		}

		dy := math.Sqrt(r2 - dx2)
		circleBottomY := cy + dy

		if terrain[x] < circleBottomY {
			terrain[x] = math.Round(circleBottomY*10) / 10
		}
	}
}
