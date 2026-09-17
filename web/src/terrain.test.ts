import { describe, it, expect } from 'vitest';
import { createDefaultTerrain, getTerrainHeight, getTerrainSlopeAngle, applyCrater } from './terrain';
import { WIDTH, HEIGHT } from './types';

describe('Terrain Generation', () => {
  describe('createDefaultTerrain', () => {
    it('should generate an array of length equal to WIDTH', () => {
      const terrain = createDefaultTerrain();
      expect(terrain).toHaveLength(WIDTH);
    });

    it('should generate numeric values within reasonable bounds', () => {
      const terrain = createDefaultTerrain();
      for (const y of terrain) {
        expect(typeof y).toBe('number');
        expect(!isNaN(y)).toBe(true);
        expect(y).toBeGreaterThanOrEqual(0);
        expect(y).toBeLessThanOrEqual(HEIGHT);
      }
    });
  });

  describe('getTerrainHeight', () => {
    it('should return HEIGHT / 2 for an empty array', () => {
      expect(getTerrainHeight([], 100)).toBe(HEIGHT / 2);
    });

    it('should return the correct height for valid x indices', () => {
      const terrain = new Array(WIDTH).fill(0).map((_, i) => i * 2);
      expect(getTerrainHeight(terrain, 10)).toBe(20);
      expect(getTerrainHeight(terrain, 10.5)).toBe(20); // Math.floor(10.5) = 10
      expect(getTerrainHeight(terrain, 50)).toBe(100);
    });

    it('should clamp out-of-bounds x values', () => {
      const terrain = new Array(WIDTH).fill(0).map((_, i) => i * 2);
      expect(getTerrainHeight(terrain, -10)).toBe(0);
      expect(getTerrainHeight(terrain, WIDTH + 10)).toBe(terrain[WIDTH - 1]);
    });
  });

  describe('getTerrainSlopeAngle', () => {
    it('should calculate the correct angle for flat terrain', () => {
      const terrain = new Array(WIDTH).fill(HEIGHT / 2);
      expect(getTerrainSlopeAngle(terrain, 100)).toBe(0);
    });

    it('should calculate the correct angle for a straight slope', () => {
      const terrain = new Array(WIDTH).fill(0).map((_, i) => i); // y = x, slope = 1, angle = 45 deg or PI/4
      // Math.atan2(y2 - y1, x2 - x1) -> Math.atan2(delta * 1, delta * 2) => actually delta y is 10, delta x is 10
      // if delta=5, x1=x-5, x2=x+5. y1=x-5, y2=x+5.
      // y2-y1 = (x+5)-(x-5) = 10
      // x2-x1 = (x+5)-(x-5) = 10
      // Math.atan2(10, 10) = Math.PI / 4
      expect(getTerrainSlopeAngle(terrain, 100)).toBeCloseTo(Math.PI / 4);
    });

    it('should calculate the correct angle for a negative slope', () => {
      const terrain = new Array(WIDTH).fill(0).map((_, i) => HEIGHT - i); // y = -x, slope = -1, angle = -PI/4
      expect(getTerrainSlopeAngle(terrain, 100)).toBeCloseTo(-Math.PI / 4);
    });

    it('should handle indices near the edges gracefully', () => {
      const terrain = new Array(WIDTH).fill(0).map((_, i) => i);
      // At x = 2, delta = 5.
      // x1 = Math.max(0, 2 - 5) = 0
      // x2 = Math.min(WIDTH - 1, 2 + 5) = 7
      // y1 = 0, y2 = 7
      // Math.atan2(7 - 0, 7 - 0) = Math.PI / 4
      expect(getTerrainSlopeAngle(terrain, 2)).toBeCloseTo(Math.PI / 4);

      // At x = WIDTH - 2
      // x1 = WIDTH - 2 - 5 = WIDTH - 7
      // x2 = WIDTH - 1
      // y1 = WIDTH - 7, y2 = WIDTH - 1
      // y2-y1 = 6, x2-x1 = 6
      expect(getTerrainSlopeAngle(terrain, WIDTH - 2)).toBeCloseTo(Math.PI / 4);
    });
  });

  describe('applyCrater', () => {
    it('should deform the terrain downwards within the radius', () => {
      const terrain = new Array(WIDTH).fill(100);
      const cx = 500;
      const cy = 100;
      const radius = 50;

      applyCrater(terrain, cx, cy, radius);

      // At center (cx), the bottom of the circle is cy + radius = 150
      expect(terrain[cx]).toBe(150);

      // Outside the crater, terrain should be unchanged
      expect(terrain[cx - radius - 1]).toBe(100);
      expect(terrain[cx + radius + 1]).toBe(100);

      // Halfway between center and edge
      // dx = 25
      // dy = sqrt(50^2 - 25^2) = sqrt(2500 - 625) = sqrt(1875) ≈ 43.3
      // y should be cy + dy = 100 + 43.3 = 143.3
      expect(terrain[cx + 25]).toBeCloseTo(100 + Math.sqrt(radius * radius - 25 * 25));
    });

    it('should not deform the terrain if it is already deeper than the crater', () => {
      const terrain = new Array(WIDTH).fill(200); // deeper than cy + radius (150)
      const cx = 500;
      const cy = 100;
      const radius = 50;

      applyCrater(terrain, cx, cy, radius);

      // terrain should be unchanged
      expect(terrain[cx]).toBe(200);
    });

    it('should handle craters near the left edge', () => {
      const terrain = new Array(WIDTH).fill(100);
      const cx = 10;
      const cy = 100;
      const radius = 50;

      applyCrater(terrain, cx, cy, radius);

      expect(terrain[cx]).toBe(150);
      // Index 0 should be affected (dx = -10)
      expect(terrain[0]).toBeCloseTo(100 + Math.sqrt(50 * 50 - 10 * 10));
    });

    it('should handle craters near the right edge', () => {
      const terrain = new Array(WIDTH).fill(100);
      const cx = WIDTH - 10;
      const cy = 100;
      const radius = 50;

      applyCrater(terrain, cx, cy, radius);

      expect(terrain[cx]).toBe(150);
      // Index WIDTH - 1 should be affected (dx = 9)
      expect(terrain[WIDTH - 1]).toBeCloseTo(100 + Math.sqrt(50 * 50 - 9 * 9));
    });
  });
});
