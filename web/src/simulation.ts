import { WIDTH, HEIGHT, ActionFire, ActionLeft, ActionRight, ActionShield } from './types';
import { getTerrainHeight, applyCrater } from './terrain';

export interface SimPlayer {
  name: string;
  x: number;
  y: number;
  dx?: number;
  angle?: number;
  power?: number;
  isDead?: boolean;
  isBot?: boolean;
  isShielded?: boolean;
  shieldUsed?: boolean;
  actionType?: string;
  moveTarget?: number;
  moving?: boolean;
  speedMultiplier?: number;
  hasBounced?: boolean;
  emoteUrl?: string;
}

export interface SimProjectile {
  id?: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  owner: string;
  emoteUrl?: string;
  bounces?: number;
  trail?: { x: number; y: number }[];
}

export interface SimImpact {
  id?: string;
  x: number;
  y: number;
  radius: number;
  owner: string;
  hitType: 'terrain' | 'tank' | 'shield';
}

export interface SimKill {
  killer: string;
  victim: string;
  x: number;
  y: number;
  reason: 'blast' | 'abyss';
}

export interface SimWallSpark {
  x: number;
  y: number;
}

export interface SimulationState {
  players: Record<string, SimPlayer>;
  projectiles: SimProjectile[];
  terrain: number[];
  bouncyWalls: boolean;
  terrainClimb: boolean;
  moveDistance: number;
  physicsSpeed: number;
  roundId?: number;
}

export interface SimulationStepEvents {
  impacts: SimImpact[];
  kills: SimKill[];
  wallSparks: SimWallSpark[];
  anyMoving: boolean;
}

export interface SimulationSummary {
  kills: SimKill[];
  impacts: SimImpact[];
  finalTerrain: number[];
  finalPlayers: Record<string, { x: number; y: number; isDead: boolean }>;
  totalSteps: number;
}

/**
 * Spawns projectiles and initiates movement based on locked player actions.
 */
export function executeActions(state: SimulationState): void {
  state.projectiles = [];
  const roundId = state.roundId ?? 0;

  for (const name in state.players) {
    const p = state.players[name];
    if (p.isDead) continue;

    if (p.actionType === ActionFire) {
      const rad = ((p.angle ?? 45) * Math.PI) / 180;
      const powerClamped = Math.min(Math.max(p.power ?? 50, 1), 100);
      const powerScaled = powerClamped / 5;
      const vx = Math.cos(rad) * powerScaled;
      const vy = -Math.sin(rad) * powerScaled;
      const shotId = `${roundId}_${name}`;

      const muzzleDist = 25;
      const spawnX = p.x + Math.cos(rad) * muzzleDist;
      const spawnY = p.y - 10 - Math.sin(rad) * muzzleDist;

      state.projectiles.push({
        id: shotId,
        x: spawnX,
        y: spawnY,
        vx,
        vy,
        owner: name,
        emoteUrl: p.emoteUrl,
      });
    } else if (p.actionType === ActionLeft) {
      p.moveTarget = p.x - (state.moveDistance || 100);
      p.moving = true;
      p.speedMultiplier = 1.0;
      p.hasBounced = false;
    } else if (p.actionType === ActionRight) {
      p.moveTarget = p.x + (state.moveDistance || 100);
      p.moving = true;
      p.speedMultiplier = 1.0;
      p.hasBounced = false;
    } else if (p.actionType === ActionShield) {
      p.moving = false;
    }
  }
}

/**
 * Checks for tanks caught in an explosion radius and marks them eliminated.
 */
export function checkTankCollisions(
  players: Record<string, SimPlayer>,
  cx: number,
  cy: number,
  radius: number,
  owner: string
): SimKill[] {
  const kills: SimKill[] = [];
  for (const name in players) {
    if (name === owner) continue; // No self-damage
    const p = players[name];
    if (p.isDead || p.isShielded) continue;
    const dist = Math.hypot(p.x - cx, p.y - cy);
    if (dist < radius + 20) {
      p.isDead = true;
      kills.push({
        killer: owner,
        victim: name,
        x: cx,
        y: cy,
        reason: 'blast',
      });
    }
  }
  return kills;
}

/**
 * Steps the physics simulation forward by dtScale.
 * Pure calculation — returns all collision, crater, and death events for rendering/diagnostics.
 */
export function stepSimulation(state: SimulationState, dtScale: number): SimulationStepEvents {
  const events: SimulationStepEvents = {
    impacts: [],
    kills: [],
    wallSparks: [],
    anyMoving: false,
  };

  const { bouncyWalls, terrainClimb } = state;

  // 1. Tank Movement & Falling
  for (const name in state.players) {
    const p = state.players[name];
    if (p.isDead) continue;

    if (p.moving) {
      events.anyMoving = true;
      const currentSpeed = (p.speedMultiplier ?? 1.0) * 2.0 * dtScale;
      if (p.actionType === ActionLeft) {
        const nextX = p.x - currentSpeed;
        const currIdx = Math.floor(p.x);
        const nextIdx = Math.floor(nextX);
        let blocked = false;
        if (!terrainClimb && currIdx !== nextIdx) {
          const stepX = Math.abs(currIdx - nextIdx);
          const rise = getTerrainHeight(state.terrain, currIdx) - getTerrainHeight(state.terrain, nextIdx);
          if (rise > 0 && rise / stepX > 4.0) {
            blocked = true;
          }
        }
        if (blocked) {
          p.moving = false;
        } else {
          p.x = nextX;
          if (p.x <= 20) {
            if (bouncyWalls && !p.hasBounced) {
              p.x = 20;
              p.actionType = ActionRight;
              p.moveTarget = p.x + (state.moveDistance || 100);
              p.speedMultiplier = 1.5;
              p.hasBounced = true;
              events.wallSparks.push({ x: 20, y: p.y });
            } else if ((p.moveTarget !== undefined && p.x <= p.moveTarget) || p.x <= 20) {
              p.moving = false;
              if (p.x < 20) p.x = 20;
            }
          } else if (p.moveTarget !== undefined && p.x <= p.moveTarget) {
            p.moving = false;
          }
        }
      } else if (p.actionType === ActionRight) {
        const nextX = p.x + currentSpeed;
        const currIdx = Math.floor(p.x);
        const nextIdx = Math.floor(nextX);
        let blocked = false;
        if (!terrainClimb && currIdx !== nextIdx) {
          const stepX = Math.abs(currIdx - nextIdx);
          const rise = getTerrainHeight(state.terrain, currIdx) - getTerrainHeight(state.terrain, nextIdx);
          if (rise > 0 && rise / stepX > 4.0) {
            blocked = true;
          }
        }
        if (blocked) {
          p.moving = false;
        } else {
          p.x = nextX;
          if (p.x >= WIDTH - 20) {
            if (bouncyWalls && !p.hasBounced) {
              p.x = WIDTH - 20;
              p.actionType = ActionLeft;
              p.moveTarget = p.x - (state.moveDistance || 100);
              p.speedMultiplier = 1.5;
              p.hasBounced = true;
              events.wallSparks.push({ x: WIDTH - 20, y: p.y });
            } else if ((p.moveTarget !== undefined && p.x >= p.moveTarget) || p.x >= WIDTH - 20) {
              p.moving = false;
              if (p.x > WIDTH - 20) p.x = WIDTH - 20;
            }
          } else if (p.moveTarget !== undefined && p.x >= p.moveTarget) {
            p.moving = false;
          }
        }
      }
    }

    // Boundary clamping
    if (p.x < 20) p.x = 20;
    if (p.x > WIDTH - 20) p.x = WIDTH - 20;

    // Falling / Ground snapping
    const floorY = getTerrainHeight(state.terrain, p.x);
    if (p.y < floorY) {
      p.y += 5.0 * dtScale;
      if (p.y > floorY) p.y = floorY;
    } else {
      p.y = floorY;
    }

    // Fall off bottom of screen
    if (p.y >= HEIGHT) {
      p.isDead = true;
      events.kills.push({
        killer: '',
        victim: name,
        x: p.x,
        y: p.y,
        reason: 'abyss',
      });
    }
  }

  // 2. Projectile Movement & Collisions
  for (let i = state.projectiles.length - 1; i >= 0; i--) {
    const proj = state.projectiles[i];
    proj.x += proj.vx * dtScale;
    proj.vy += 0.2 * dtScale; // Matching server gravity 0.2
    proj.y += proj.vy * dtScale;

    if (proj.trail) {
      proj.trail.push({ x: proj.x, y: proj.y });
      if (proj.trail.length > 20) proj.trail.shift();
    }

    let hit = false;

    // Ceiling & Floor Bounce / Bounds
    if (proj.y < 0) {
      if (bouncyWalls) {
        proj.y = 0;
        proj.vy = Math.abs(proj.vy) * 1.1;
        proj.vx *= 1.1;
        proj.bounces = (proj.bounces ?? 0) + 1;
        events.wallSparks.push({ x: Math.max(0, Math.min(WIDTH, proj.x)), y: 0 });
        if (proj.bounces > 15) hit = true;
      }
    } else if (proj.y > HEIGHT) {
      if (bouncyWalls) {
        proj.y = HEIGHT;
        proj.vy = -Math.abs(proj.vy) * 1.1;
        proj.vx *= 1.1;
        proj.bounces = (proj.bounces ?? 0) + 1;
        events.wallSparks.push({ x: Math.max(0, Math.min(WIDTH, proj.x)), y: HEIGHT });
        if (proj.bounces > 15) hit = true;
      } else {
        hit = true;
      }
    }

    // Side Walls Bounce
    if (!hit) {
      if (proj.x < 0) {
        if (bouncyWalls) {
          proj.x = 0;
          proj.vx = Math.abs(proj.vx) * 1.1;
          proj.vy *= 1.1;
          proj.bounces = (proj.bounces ?? 0) + 1;
          events.wallSparks.push({ x: 0, y: Math.max(0, Math.min(HEIGHT, proj.y)) });
          if (proj.bounces > 15) hit = true;
        } else {
          hit = true;
        }
      } else if (proj.x > WIDTH) {
        if (bouncyWalls) {
          proj.x = WIDTH;
          proj.vx = -Math.abs(proj.vx) * 1.1;
          proj.vy *= 1.1;
          proj.bounces = (proj.bounces ?? 0) + 1;
          events.wallSparks.push({ x: WIDTH, y: Math.max(0, Math.min(HEIGHT, proj.y)) });
          if (proj.bounces > 15) hit = true;
        } else {
          hit = true;
        }
      }
    }

    // Active Shield Collision (completely absorbs projectile)
    if (!hit) {
      for (const name in state.players) {
        if (name === proj.owner) continue;
        const p = state.players[name];
        if (p.isDead || !p.isShielded) continue;
        if (Math.hypot(p.x - proj.x, p.y - proj.y) < 45 && proj.y <= p.y + 5) {
          hit = true;
          events.wallSparks.push({ x: proj.x, y: proj.y });
          events.impacts.push({
            id: proj.id,
            x: proj.x,
            y: proj.y,
            radius: 45,
            owner: proj.owner,
            hitType: 'shield',
          });
          break;
        }
      }
    }

    // Terrain Collision
    if (!hit && proj.y >= 0 && proj.y >= getTerrainHeight(state.terrain, proj.x)) {
      hit = true;
      applyCrater(state.terrain, proj.x, proj.y, 50);
      events.impacts.push({
        id: proj.id,
        x: proj.x,
        y: proj.y,
        radius: 50,
        owner: proj.owner,
        hitType: 'terrain',
      });
      const blastKills = checkTankCollisions(state.players, proj.x, proj.y, 50, proj.owner);
      events.kills.push(...blastKills);
    }

    // Direct Tank Collision
    if (!hit) {
      for (const name in state.players) {
        if (name === proj.owner) continue;
        const p = state.players[name];
        if (p.isDead || p.isShielded) continue;
        if (Math.hypot(p.x - proj.x, p.y - proj.y) < 20) {
          hit = true;
          applyCrater(state.terrain, proj.x, proj.y, 50);
          events.impacts.push({
            id: proj.id,
            x: proj.x,
            y: proj.y,
            radius: 50,
            owner: proj.owner,
            hitType: 'tank',
          });
          const blastKills = checkTankCollisions(state.players, proj.x, proj.y, 50, proj.owner);
          events.kills.push(...blastKills);
          break;
        }
      }
    }

    if (hit) {
      state.projectiles.splice(i, 1);
    }
  }

  return events;
}

/**
 * Runs a complete scenario simulation to completion (all projectiles cleared, tanks stationary).
 * Returns summary statistics for comparison against server pre-calculations.
 */
export function runFullSimulation(
  state: SimulationState,
  dtScale: number = 1.0,
  maxSteps: number = 2000
): SimulationSummary {
  executeActions(state);

  const allKills: SimKill[] = [];
  const allImpacts: SimImpact[] = [];
  let step = 0;

  while (step < maxSteps) {
    step++;
    const events = stepSimulation(state, dtScale);
    allKills.push(...events.kills);
    allImpacts.push(...events.impacts);

    // Stop condition: no in-flight projectiles and no tanks moving or falling
    if (state.projectiles.length === 0 && !events.anyMoving) {
      let anyFalling = false;
      for (const name in state.players) {
        const p = state.players[name];
        if (!p.isDead && p.y < getTerrainHeight(state.terrain, p.x)) {
          anyFalling = true;
          break;
        }
      }
      if (!anyFalling) {
        break;
      }
    }
  }

  const finalPlayers: Record<string, { x: number; y: number; isDead: boolean }> = {};
  for (const name in state.players) {
    const p = state.players[name];
    finalPlayers[name] = {
      x: p.x,
      y: p.y,
      isDead: !!p.isDead,
    };
  }

  return {
    kills: allKills,
    impacts: allImpacts,
    finalTerrain: [...state.terrain],
    finalPlayers,
    totalSteps: step,
  };
}
