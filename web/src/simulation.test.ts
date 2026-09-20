import { describe, it, expect } from 'vitest';
import {
  executeActions,
  stepSimulation,
  runFullSimulation,
  checkTankCollisions,
  SimulationState,
} from './simulation';
import { WIDTH, ActionFire, ActionLeft, ActionRight } from './types';

function createFlatTerrain(height: number = 600): number[] {
  return new Array(WIDTH).fill(height);
}

describe('Simulation Engine', () => {
  describe('executeActions', () => {
    it('spawns projectiles for ActionFire', () => {
      const state: SimulationState = {
        players: {
          Alice: { name: 'Alice', x: 200, y: 600, angle: 45, power: 50, actionType: ActionFire },
          Bob: { name: 'Bob', x: 800, y: 600, angle: 135, power: 75, actionType: ActionFire },
        },
        projectiles: [],
        terrain: createFlatTerrain(),
        bouncyWalls: false,
        terrainClimb: false,
        moveDistance: 100,
        physicsSpeed: 0.5,
        roundId: 1,
      };

      executeActions(state);
      expect(state.projectiles).toHaveLength(2);
      expect(state.projectiles[0].owner).toBe('Alice');
      expect(state.projectiles[0].vx).toBeGreaterThan(0);
      expect(state.projectiles[0].vy).toBeLessThan(0); // Launching upwards (negative canvas Y)
      expect(state.projectiles[1].owner).toBe('Bob');
      expect(state.projectiles[1].vx).toBeLessThan(0);
    });

    it('sets movement targets for ActionLeft and ActionRight', () => {
      const state: SimulationState = {
        players: {
          LeftMover: { name: 'LeftMover', x: 500, y: 600, actionType: ActionLeft },
          RightMover: { name: 'RightMover', x: 500, y: 600, actionType: ActionRight },
        },
        projectiles: [],
        terrain: createFlatTerrain(),
        bouncyWalls: false,
        terrainClimb: false,
        moveDistance: 120,
        physicsSpeed: 0.5,
      };

      executeActions(state);
      expect(state.players.LeftMover.moving).toBe(true);
      expect(state.players.LeftMover.moveTarget).toBe(380);
      expect(state.players.RightMover.moving).toBe(true);
      expect(state.players.RightMover.moveTarget).toBe(620);
    });
  });

  describe('checkTankCollisions', () => {
    it('eliminates tanks within the blast radius', () => {
      const players = {
        Victim: { name: 'Victim', x: 520, y: 600, isDead: false },
        FarAway: { name: 'FarAway', x: 900, y: 600, isDead: false },
        Shooter: { name: 'Shooter', x: 500, y: 600, isDead: false },
      };

      // Blast radius = 50 + 20 = 70. Distance to Victim is 20px.
      const kills = checkTankCollisions(players, 500, 600, 50, 'Shooter');
      expect(kills).toHaveLength(1);
      expect(kills[0].victim).toBe('Victim');
      expect(players.Victim.isDead).toBe(true);
      expect(players.FarAway.isDead).toBe(false);
      expect(players.Shooter.isDead).toBe(false); // No self-damage
    });

    it('does not eliminate shielded tanks', () => {
      const players = {
        ShieldedTank: { name: 'ShieldedTank', x: 510, y: 600, isDead: false, isShielded: true },
      };

      const kills = checkTankCollisions(players, 500, 600, 50, 'Attacker');
      expect(kills).toHaveLength(0);
      expect(players.ShieldedTank.isDead).toBe(false);
    });
  });

  describe('stepSimulation', () => {
    it('moves projectiles along ballistic arc with gravity', () => {
      const state: SimulationState = {
        players: {},
        projectiles: [
          { id: '1_test', x: 100, y: 200, vx: 5, vy: -5, owner: 'test' },
        ],
        terrain: createFlatTerrain(800),
        bouncyWalls: false,
        terrainClimb: false,
        moveDistance: 100,
        physicsSpeed: 0.5,
      };

      stepSimulation(state, 1.0);
      expect(state.projectiles[0].x).toBe(105);
      expect(state.projectiles[0].vy).toBeCloseTo(-4.8); // -5 + 0.2
      expect(state.projectiles[0].y).toBeCloseTo(195.2);
    });

    it('absorbs projectile and triggers spark when hitting active shield', () => {
      const state: SimulationState = {
        players: {
          Defender: { name: 'Defender', x: 300, y: 400, isShielded: true },
        },
        projectiles: [
          { id: '1_shot', x: 290, y: 390, vx: 5, vy: 0, owner: 'Attacker' },
        ],
        terrain: createFlatTerrain(800),
        bouncyWalls: false,
        terrainClimb: false,
        moveDistance: 100,
        physicsSpeed: 0.5,
      };

      const events = stepSimulation(state, 1.0);
      expect(events.impacts).toHaveLength(1);
      expect(events.impacts[0].hitType).toBe('shield');
      expect(state.projectiles).toHaveLength(0); // Absorbed
    });
  });

  describe('runFullSimulation', () => {
    it('runs a complete round to steady state', () => {
      const state: SimulationState = {
        players: {
          P1: { name: 'P1', x: 200, y: 600, angle: 45, power: 40, actionType: ActionFire },
          P2: { name: 'P2', x: 1400, y: 600, angle: 135, power: 40, actionType: ActionFire },
        },
        projectiles: [],
        terrain: createFlatTerrain(600),
        bouncyWalls: false,
        terrainClimb: false,
        moveDistance: 100,
        physicsSpeed: 0.5,
      };

      const summary = runFullSimulation(state, 1.0, 1000);
      expect(summary.totalSteps).toBeGreaterThan(0);
      expect(summary.totalSteps).toBeLessThan(1000);
      expect(summary.impacts.length).toBeGreaterThanOrEqual(2);
      expect(state.projectiles).toHaveLength(0);
    });
  });
});
