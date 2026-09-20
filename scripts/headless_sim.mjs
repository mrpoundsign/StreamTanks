#!/usr/bin/env node
import { runFullSimulation } from '../web/dist/simulation.mjs';
import fs from 'fs';

// Read all JSON input from stdin
const input = fs.readFileSync(0, 'utf-8');
if (!input.trim()) {
  process.exit(0);
}

try {
  const data = JSON.parse(input);
  const scenarios = Array.isArray(data) ? data : [data];
  const results = [];

  for (const sc of scenarios) {
    const state = {
      players: sc.players || {},
      projectiles: [],
      terrain: sc.terrain ? [...sc.terrain] : [],
      bouncyWalls: !!sc.rules?.bouncyWalls,
      terrainClimb: !!sc.rules?.terrainClimb,
      moveDistance: sc.rules?.moveDistance || 100,
      physicsSpeed: sc.rules?.physicsSpeed || 0.5,
      roundId: 1,
    };

    const res = runFullSimulation(state, 1.0);
    results.push({
      scenarioId: sc.id,
      kills: res.kills,
      impacts: res.impacts,
      finalTerrain: res.finalTerrain,
      finalPlayers: res.finalPlayers,
      totalSteps: res.totalSteps,
    });
  }

  process.stdout.write(JSON.stringify(results));
} catch (err) {
  process.stderr.write(`headless_sim error: ${err.message}\n`);
  process.exit(1);
}
