#!/usr/bin/env node
import { runFullSimulation, runOverlaySimulation } from '../web/dist/simulation.mjs';
import readline from 'readline';

function processItem(item) {
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

  return {
    scenarioId: sc.id,
    kills: res.kills,
    impacts: res.impacts,
    finalTerrain: res.finalTerrain,
    finalPlayers: res.finalPlayers,
    totalSteps: res.totalSteps,
  };
}

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false,
  crlfDelay: Infinity,
});

rl.on('line', (line) => {
  const trimmed = line.trim();
  if (!trimmed) return;

  if (trimmed === 'RESET') {
    process.stdout.write(JSON.stringify({ status: 'ok' }) + '\n');
    return;
  }

  try {
    const data = JSON.parse(trimmed);
    const items = Array.isArray(data) ? data : [data];
    const results = items.map(processItem);
    process.stdout.write(JSON.stringify({ results }) + '\n');
  } catch (err) {
    process.stdout.write(JSON.stringify({ error: err.message }) + '\n');
  }
});
