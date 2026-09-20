import { WIDTH, HEIGHT } from './types';

export function createDefaultTerrain(): number[] {
  const terrain = new Array<number>(WIDTH);
  let y = HEIGHT / 2 + (Math.random() * 200 - 100);
  let slope = 0;
  terrain[0] = y;

  for (let x = 1; x < WIDTH; x++) {
    slope += (Math.random() - 0.5) * 0.15;
    if (slope > 2) slope = 2;
    if (slope < -2) slope = -2;

    y += slope;

    // Softly push back towards the center if getting too close to edges
    if (y < 250) slope += 0.05;
    if (y > HEIGHT - 200) slope -= 0.05;

    terrain[x] = y;
  }

  return terrain;
}

export function getTerrainHeight(terrain: number[], x: number): number {
  if (terrain.length === 0) return HEIGHT / 2;
  const idx = Math.min(WIDTH - 1, Math.max(0, Math.floor(x)));
  return terrain[idx];
}

export function getTerrainSlopeAngle(terrain: number[], x: number, delta = 5): number {
  const x1 = Math.max(0, Math.floor(x - delta));
  const x2 = Math.min(WIDTH - 1, Math.floor(x + delta));
  const y1 = terrain[x1];
  const y2 = terrain[x2];
  return Math.atan2(y2 - y1, x2 - x1);
}

export function applyCrater(terrain: number[], cx: number, cy: number, radius: number): void {
  const startX = Math.max(0, Math.floor(cx - radius));
  const endX = Math.min(WIDTH, Math.ceil(cx + radius));

  for (let x = startX; x < endX; x++) {
    const dx = x - cx;
    const dy = Math.sqrt(radius * radius - dx * dx);
    const circleBottomY = cy + dy;

    if (terrain[x] < circleBottomY) {
      terrain[x] = Math.round(circleBottomY * 10) / 10;
    }
  }
}
