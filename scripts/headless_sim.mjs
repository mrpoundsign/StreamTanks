#!/usr/bin/env node
import { runFullSimulation, runOverlaySimulation } from '../web/dist/simulation.mjs';
import fs from 'fs';

// Read all JSON input from stdin
const input = fs.readFileSync(0, 'utf-8');
if (!input.trim()) {
  process.exit(0);
}

try {
  const data = JSON.parse(input);
  const items = Array.isArray(data) ? data : [data];
  const results = [];

  for (const item of items) {
    const sc = item.scenario || item;
    const serverImpacts = item.serverImpacts || [];

    const clientFps = item.clientFps || 60;
    const physicsSpeed = sc.rules?.physicsSpeed || 0.5;
    const clientDtScale = item.clientDtScale !== undefined
      ? item.clientDtScale
      : ((60.0 / clientFps) * physicsSpeed);
    const maxSteps = Math.ceil(2000 * (clientFps / 60));

    const state = {
      players: sc.players || {},
      projectiles: [],
      terrain: sc.terrain ? [...sc.terrain] : [],
      bouncyWalls: !!sc.rules?.bouncyWalls,
      terrainClimb: !!sc.rules?.terrainClimb,
      moveDistance: sc.rules?.moveDistance || 100,
      physicsSpeed: physicsSpeed,
      roundId: 1,
    };

    const res = item.serverImpacts !== undefined
      ? runOverlaySimulation(state, clientDtScale, serverImpacts, maxSteps)
      : runFullSimulation(state, clientDtScale, maxSteps);

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
