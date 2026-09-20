import { WIDTH, HEIGHT } from '../../web/src/types';
import {
  SimulationState,
  executeActions,
  stepSimulation,
  runFullSimulation,
  SimImpact,
  SimKill,
  SimPlayer,
} from '../../web/src/simulation';

interface ScenarioPlayer {
  name: string;
  x: number;
  y: number;
  angle: number;
  power: number;
  actionType: string;
  isBot?: boolean;
  isShielded?: boolean;
  isDead?: boolean;
}

interface Scenario {
  id: string;
  name: string;
  description?: string;
  seed?: number;
  rules: {
    bouncyWalls: boolean;
    terrainClimb: boolean;
    moveDistance: number;
    physicsSpeed: number;
    botPoints: number;
  };
  terrain: number[];
  players: Record<string, ScenarioPlayer>;
}

interface ServerSimulationResult {
  kills: SimKill[];
  impacts: SimImpact[];
  finalTerrain: number[];
  finalPlayers: Record<string, { x: number; y: number; isDead: boolean }>;
  winner: string;
  totalSteps: number;
}

interface FrameSnapshot {
  step: number;
  players: Record<string, { x: number; y: number; isDead: boolean; isShielded: boolean }>;
  projectiles: { x: number; y: number; vx: number; vy: number; owner: string }[];
  terrain: number[];
  impacts: SimImpact[];
  kills: SimKill[];
}

interface ScenarioListItem {
  id: string;
  name: string;
  description?: string;
  source: string;
  filename?: string;
  maxTerrainDiff?: number;
  maxPositionDiff?: number;
  maxPixelDiff?: number;
  terrainDiffCount?: number;
  hasDiscrepancy?: boolean;
  summary?: string;
}

// DOM References
const canvas = document.getElementById('replayCanvas') as HTMLCanvasElement;
const ctx = canvas.getContext('2d')!;

const diffStatusPill = document.getElementById('diff-status-pill') as HTMLElement;
const diffStatusText = document.getElementById('diff-status-text') as HTMLElement;
const scenarioListEl = document.getElementById('scenario-list') as HTMLElement;
const scenarioCountBadge = document.getElementById('scenario-count-badge') as HTMLElement;
const selectScenarioSort = document.getElementById('select-scenario-sort') as HTMLSelectElement | null;
const btnRefresh = document.getElementById('btn-refresh-scenarios') as HTMLButtonElement;

const btnRunFuzz = document.getElementById('btn-run-fuzz') as HTMLButtonElement;
const fuzzProgressContainer = document.getElementById('fuzz-progress-container') as HTMLElement;
const fuzzProgressBar = document.getElementById('fuzz-progress-bar') as HTMLElement;
const fuzzProgressLabel = document.getElementById('fuzz-progress-label') as HTMLElement;
const fuzzStatsLabel = document.getElementById('fuzz-stats-label') as HTMLElement;
const batchButtons = document.querySelectorAll<HTMLButtonElement>('.btn-batch');

const btnRewind = document.getElementById('btn-rewind') as HTMLButtonElement;
const btnStepBack = document.getElementById('btn-step-back') as HTMLButtonElement;
const btnPlayPause = document.getElementById('btn-play-pause') as HTMLButtonElement;
const btnStepForward = document.getElementById('btn-step-forward') as HTMLButtonElement;
const timelineScrubber = document.getElementById('timeline-scrubber') as HTMLInputElement;
const timelineCurrentFrame = document.getElementById('timeline-current-frame') as HTMLElement;
const timelineTotalFrames = document.getElementById('timeline-total-frames') as HTMLElement;
const selectSpeed = document.getElementById('select-speed') as HTMLSelectElement;

const inspectorStatusBox = document.getElementById('inspector-status-box') as HTMLElement;
const metricTerrainDiff = document.getElementById('metric-terrain-diff') as HTMLElement;
const metricPosDiff = document.getElementById('metric-pos-diff') as HTMLElement;
const metricKillMismatch = document.getElementById('metric-kill-mismatch') as HTMLElement;
const metricSteps = document.getElementById('metric-steps') as HTMLElement;
const killsComparisonBody = document.getElementById('kills-comparison-body') as HTMLElement;
const inspectorTankTbody = document.getElementById('inspector-tank-tbody') as HTMLElement;
const btnToggleJson = document.getElementById('btn-toggle-json') as HTMLButtonElement;
const rawJsonViewer = document.getElementById('raw-json-viewer') as HTMLElement;

// State Variables
let currentScenario: Scenario | null = null;
let currentServerResult: ServerSimulationResult | null = null;
let frameSnapshots: FrameSnapshot[] = [];
let currentFrame = 0;
let isPlaying = false;
let playTimer: number | null = null;
let playbackSpeed = 0.5;
let fuzzBatchSize = 100;
let loadedScenarios: ScenarioListItem[] = [];

// Batch Button Selectors
batchButtons.forEach((btn) => {
  btn.addEventListener('click', () => {
    batchButtons.forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    fuzzBatchSize = parseInt(btn.dataset.count || '100', 10);
  });
});

// Initialize
init();

async function init(): Promise<void> {
  setupEventListeners();
  await loadScenarioLibrary();
}

function setupEventListeners(): void {
  btnRefresh.addEventListener('click', loadScenarioLibrary);
  if (selectScenarioSort) {
    selectScenarioSort.addEventListener('change', () => {
      applyScenarioSorting();
    });
  }
  btnRunFuzz.addEventListener('click', runFuzzSuite);

  btnPlayPause.addEventListener('click', togglePlayPause);
  btnRewind.addEventListener('click', () => seekFrame(0));
  btnStepBack.addEventListener('click', () => seekFrame(Math.max(0, currentFrame - 1)));
  btnStepForward.addEventListener('click', () => seekFrame(Math.min(frameSnapshots.length - 1, currentFrame + 1)));

  timelineScrubber.addEventListener('input', () => {
    seekFrame(parseInt(timelineScrubber.value, 10));
  });

  selectSpeed.addEventListener('change', () => {
    playbackSpeed = parseFloat(selectSpeed.value);
    if (isPlaying) {
      pausePlayback();
      startPlayback();
    }
  });

  btnToggleJson.addEventListener('click', () => {
    const isHidden = rawJsonViewer.style.display === 'none';
    rawJsonViewer.style.display = isHidden ? 'block' : 'none';
  });

  // Keyboard navigation shortcuts
  window.addEventListener('keydown', (e) => {
    if (e.key === ' ' && e.target !== btnRunFuzz) {
      e.preventDefault();
      togglePlayPause();
    } else if (e.key === 'ArrowRight') {
      seekFrame(Math.min(frameSnapshots.length - 1, currentFrame + 1));
    } else if (e.key === 'ArrowLeft') {
      seekFrame(Math.max(0, currentFrame - 1));
    }
  });
}

async function loadScenarioLibrary(): Promise<void> {
  try {
    const sortMode = selectScenarioSort ? selectScenarioSort.value : 'diff';
    const res = await fetch(`/api/scenarios/list?sort=${encodeURIComponent(sortMode)}`);
    const data = await res.json();
    loadedScenarios = (data.scenarios || []) as ScenarioListItem[];
    applyScenarioSorting();
  } catch (err) {
    scenarioListEl.innerHTML = `<div class="empty-text">Failed to load scenarios: ${String(err)}</div>`;
  }
}

function applyScenarioSorting(): void {
  const sortMode = selectScenarioSort ? selectScenarioSort.value : 'diff';
  const sorted = [...loadedScenarios];
  if (sortMode === 'name') {
    sorted.sort((a, b) => a.name.localeCompare(b.name));
  } else if (sortMode === 'source') {
    sorted.sort((a, b) => {
      if (a.source !== b.source) return a.source.localeCompare(b.source);
      return a.name.localeCompare(b.name);
    });
  } else {
    // Default: Sort by MaxPixelDiff descending
    sorted.sort((a, b) => {
      const diffA = a.maxPixelDiff ?? 0;
      const diffB = b.maxPixelDiff ?? 0;
      if (diffA !== diffB) return diffB - diffA;
      if (a.source !== b.source) return a.source === 'saved' ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
  }
  renderScenarioList(sorted);
}

function renderDiffBadge(sc: ScenarioListItem): string {
  const diff = sc.maxPixelDiff ?? 0;
  if (sc.source === 'builtin') {
    return `<span class="diff-badge clean" title="Built-in Scenario (0px diff)">0px</span>`;
  }
  if (sc.hasDiscrepancy || diff >= 1.0) {
    return `<span class="diff-badge severe" title="Max Pixel Diff: ${diff.toFixed(1)}px (Terrain: ${(sc.maxTerrainDiff || 0).toFixed(1)}px, Pos: ${(sc.maxPositionDiff || 0).toFixed(1)}px)">Δ ${diff.toFixed(1)}px</span>`;
  }
  if (diff > 0) {
    return `<span class="diff-badge minor" title="Max Pixel Diff: ${diff.toFixed(2)}px">Δ ${diff.toFixed(2)}px</span>`;
  }
  return `<span class="diff-badge clean" title="Zero Discrepancy">✓ 0px</span>`;
}

function renderScenarioList(scenarios: ScenarioListItem[]): void {
  scenarioCountBadge.textContent = String(scenarios.length);
  if (scenarios.length === 0) {
    scenarioListEl.innerHTML = '<div class="empty-text">No scenarios found. Run fuzz suite to generate test cases.</div>';
    return;
  }

  const activeId = currentScenario?.id;

  scenarioListEl.innerHTML = scenarios
    .map(
      (sc) => `
        <div class="scenario-item ${sc.source} ${sc.source === 'saved' && (sc.hasDiscrepancy || (sc.maxPixelDiff ?? 0) > 0) ? 'mismatch' : ''} ${sc.id === activeId ? 'active' : ''}" data-id="${sc.id}">
            <div class="scenario-item-header">
                <span class="scenario-name" title="${escapeHtml(sc.name)}">${escapeHtml(sc.name)}</span>
                <div class="scenario-item-tags">
                    ${renderDiffBadge(sc)}
                    <span class="scenario-source-tag ${sc.source}">${sc.source.toUpperCase()}</span>
                </div>
            </div>
            <div class="scenario-desc" title="${escapeHtml(sc.summary || sc.description || sc.filename || sc.id)}">${escapeHtml(sc.summary || sc.description || sc.filename || sc.id)}</div>
        </div>
      `
    )
    .join('');

  document.querySelectorAll<HTMLElement>('.scenario-item').forEach((item) => {
    item.addEventListener('click', () => {
      document.querySelectorAll('.scenario-item').forEach((i) => i.classList.remove('active'));
      item.classList.add('active');
      const id = item.dataset.id;
      if (id) loadAndRunScenario(id);
    });
  });

  // Auto-select first item if none active
  if (!currentScenario && scenarios.length > 0) {
    const first = document.querySelector<HTMLElement>('.scenario-item');
    if (first) first.click();
  }
}

async function loadAndRunScenario(id: string): Promise<void> {
  pausePlayback();
  try {
    const res = await fetch(`/api/scenarios/load?id=${encodeURIComponent(id)}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const sc = (await res.json()) as Scenario;
    currentScenario = sc;

    // Run on server
    const serverRes = await fetch('/api/scenarios/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(sc),
    });
    currentServerResult = (await serverRes.json()) as ServerSimulationResult;

    // Simulate step-by-step on overlay
    recordOverlaySimulation(sc);

    // Update UI & Inspector
    updateInspector(sc, currentServerResult);
    seekFrame(0);
    rawJsonViewer.textContent = JSON.stringify(sc, null, 2);
  } catch (err) {
    console.error('Failed to load scenario:', err);
  }
}

function recordOverlaySimulation(sc: Scenario): void {
  frameSnapshots = [];

  const simPlayers: Record<string, SimPlayer> = {};
  for (const name in sc.players) {
    const p = sc.players[name];
    simPlayers[name] = {
      name: p.name,
      x: p.x,
      y: p.y,
      angle: p.angle,
      power: p.power,
      actionType: p.actionType,
      isBot: p.isBot,
      isShielded: p.isShielded,
      isDead: p.isDead,
    };
  }

  const state: SimulationState = {
    players: simPlayers,
    projectiles: [],
    terrain: [...sc.terrain],
    bouncyWalls: sc.rules.bouncyWalls,
    terrainClimb: sc.rules.terrainClimb,
    moveDistance: sc.rules.moveDistance,
    physicsSpeed: sc.rules.physicsSpeed,
    roundId: 1,
  };

  // Step 0 Snapshot
  executeActions(state);
  saveFrameSnapshot(0, state, [], []);

  const accumulatedImpacts: SimImpact[] = [];
  const accumulatedKills: SimKill[] = [];
  let step = 0;
  const maxSteps = 2000;

  while (step < maxSteps) {
    step++;
    const events = stepSimulation(state, 1.0);
    accumulatedImpacts.push(...events.impacts);
    accumulatedKills.push(...events.kills);

    saveFrameSnapshot(step, state, accumulatedImpacts, accumulatedKills);

    // Stop condition
    if (state.projectiles.length === 0 && !events.anyMoving) {
      let anyFalling = false;
      for (const name in state.players) {
        const p = state.players[name];
        if (!p.isDead && p.y < state.terrain[Math.min(WIDTH - 1, Math.max(0, Math.floor(p.x)))]) {
          anyFalling = true;
          break;
        }
      }
      if (!anyFalling) break;
    }
  }

  timelineScrubber.max = String(frameSnapshots.length - 1);
  timelineTotalFrames.textContent = `Total: ${frameSnapshots.length - 1}`;
}

function saveFrameSnapshot(step: number, state: SimulationState, impacts: SimImpact[], kills: SimKill[]): void {
  const pMap: Record<string, { x: number; y: number; isDead: boolean; isShielded: boolean }> = {};
  for (const name in state.players) {
    const p = state.players[name];
    pMap[name] = {
      x: p.x,
      y: p.y,
      isDead: !!p.isDead,
      isShielded: !!p.isShielded,
    };
  }

  frameSnapshots.push({
    step,
    players: pMap,
    projectiles: state.projectiles.map((proj) => ({ x: proj.x, y: proj.y, vx: proj.vx, vy: proj.vy, owner: proj.owner })),
    terrain: [...state.terrain],
    impacts: [...impacts],
    kills: [...kills],
  });
}

function seekFrame(frameIndex: number): void {
  currentFrame = Math.max(0, Math.min(frameSnapshots.length - 1, frameIndex));
  timelineScrubber.value = String(currentFrame);
  timelineCurrentFrame.textContent = `Frame ${currentFrame}`;

  renderCanvasFrame();
  updateFrameInspector();
}

function togglePlayPause(): void {
  if (isPlaying) {
    pausePlayback();
  } else {
    startPlayback();
  }
}

function startPlayback(): void {
  if (frameSnapshots.length === 0) return;
  if (currentFrame >= frameSnapshots.length - 1) {
    currentFrame = 0;
  }
  isPlaying = true;
  btnPlayPause.innerHTML = '&#10074;&#10074; Pause';

  const frameInterval = (1000 / 60) / playbackSpeed;
  playTimer = window.setInterval(() => {
    if (currentFrame < frameSnapshots.length - 1) {
      seekFrame(currentFrame + 1);
    } else {
      pausePlayback();
    }
  }, frameInterval);
}

function pausePlayback(): void {
  isPlaying = false;
  btnPlayPause.innerHTML = '&#9654; Play';
  if (playTimer !== null) {
    clearInterval(playTimer);
    playTimer = null;
  }
}

function renderCanvasFrame(): void {
  if (frameSnapshots.length === 0 || !currentScenario) return;
  const snap = frameSnapshots[currentFrame];

  ctx.clearRect(0, 0, WIDTH, HEIGHT);

  // 1. Draw Base Overlay Terrain Line (Neon Red)
  ctx.beginPath();
  ctx.moveTo(0, snap.terrain[0]);
  for (let x = 1; x < WIDTH; x++) {
    ctx.lineTo(x, snap.terrain[x]);
  }
  ctx.strokeStyle = '#ff003c';
  ctx.lineWidth = 3;
  ctx.shadowBlur = 10;
  ctx.shadowColor = '#ff003c';
  ctx.stroke();

  // Fill terrain below
  ctx.lineTo(WIDTH, HEIGHT);
  ctx.lineTo(0, HEIGHT);
  ctx.closePath();
  ctx.fillStyle = 'rgba(255, 0, 60, 0.03)';
  ctx.fill();
  ctx.shadowBlur = 0;

  // 2. Draw Server Final Terrain (Cyan Ghost Line) if available
  if (currentServerResult && currentServerResult.finalTerrain.length === WIDTH) {
    ctx.beginPath();
    ctx.moveTo(0, currentServerResult.finalTerrain[0]);
    for (let x = 1; x < WIDTH; x++) {
      ctx.lineTo(x, currentServerResult.finalTerrain[x]);
    }
    ctx.strokeStyle = 'rgba(0, 255, 204, 0.4)';
    ctx.lineWidth = 2;
    ctx.setLineDash([4, 4]);
    ctx.stroke();
    ctx.setLineDash([]); // reset
  }

  // 3. Highlight Terrain Discrepancy Zones (Yellow Neon Fill)
  if (currentServerResult) {
    for (let x = 0; x < WIDTH; x++) {
      const diff = Math.abs(snap.terrain[x] - currentServerResult.finalTerrain[x]);
      if (diff > 0.5) {
        ctx.fillStyle = 'rgba(255, 183, 3, 0.35)';
        const topY = Math.min(snap.terrain[x], currentServerResult.finalTerrain[x]);
        const h = Math.abs(snap.terrain[x] - currentServerResult.finalTerrain[x]);
        ctx.fillRect(x, topY, 1, Math.max(h, 4));
      }
    }
  }

  // 4. Draw Projectiles (Overlay Pink/Red)
  for (const proj of snap.projectiles) {
    ctx.beginPath();
    ctx.arc(proj.x, proj.y, 6, 0, Math.PI * 2);
    ctx.fillStyle = '#ff003c';
    ctx.shadowBlur = 15;
    ctx.shadowColor = '#ff003c';
    ctx.fill();
    ctx.shadowBlur = 0;
  }

  // 5. Draw Impacts
  // Overlay impacts (Pink)
  for (const imp of snap.impacts) {
    ctx.beginPath();
    ctx.arc(imp.x, imp.y, imp.radius, 0, Math.PI * 2);
    ctx.strokeStyle = 'rgba(255, 0, 60, 0.6)';
    ctx.lineWidth = 2;
    ctx.stroke();
  }

  // Server impacts (Cyan ghost rings)
  if (currentServerResult && Array.isArray(currentServerResult.impacts)) {
    for (const sImp of currentServerResult.impacts) {
      ctx.beginPath();
      ctx.arc(sImp.x, sImp.y, sImp.radius, 0, Math.PI * 2);
      ctx.strokeStyle = 'rgba(0, 255, 204, 0.5)';
      ctx.lineWidth = 2;
      ctx.setLineDash([6, 6]);
      ctx.stroke();
      ctx.setLineDash([]);
    }
  }

  // 6. Draw Tanks
  for (const name in snap.players) {
    const p = snap.players[name];
    if (p.isDead) continue;

    // Tank Hull
    ctx.fillStyle = '#ff003c';
    ctx.fillRect(p.x - 15, p.y - 10, 30, 10);

    // Shield Bubble if active
    if (p.isShielded) {
      ctx.beginPath();
      ctx.arc(p.x, p.y - 5, 40, Math.PI, 0);
      ctx.strokeStyle = 'rgba(0, 255, 204, 0.8)';
      ctx.lineWidth = 3;
      ctx.stroke();
    }

    // Server Ghost Tank (Cyan) if positions drifted
    if (currentServerResult && currentServerResult.finalPlayers[name]) {
      const sP = currentServerResult.finalPlayers[name];
      const dist = Math.hypot(p.x - sP.x, p.y - sP.y);
      if (dist > 1.0) {
        ctx.strokeStyle = '#00ffcc';
        ctx.lineWidth = 1;
        ctx.strokeRect(sP.x - 15, sP.y - 10, 30, 10);
      }
    }

    // Name Tag
    ctx.font = '12px Orbitron';
    ctx.fillStyle = '#f0f4fc';
    ctx.textAlign = 'center';
    ctx.fillText(name, p.x, p.y - 16);
  }
}

function updateInspector(_sc: Scenario, serverRes: ServerSimulationResult | null): void {
  if (!serverRes || frameSnapshots.length === 0) return;
  const finalSnap = frameSnapshots[frameSnapshots.length - 1];

  // Compare terrain
  let maxTerrainDiff = 0;
  let terrainDiffCount = 0;
  for (let x = 0; x < WIDTH; x++) {
    const d = Math.abs(finalSnap.terrain[x] - serverRes.finalTerrain[x]);
    if (d > 0.15) {
      terrainDiffCount++;
      if (d > maxTerrainDiff) maxTerrainDiff = d;
    }
  }

  // Compare positions
  let maxPosDiff = 0;
  for (const name in serverRes.finalPlayers) {
    const sP = serverRes.finalPlayers[name];
    const cP = finalSnap.players[name];
    if (cP) {
      const d = Math.hypot(sP.x - cP.x, sP.y - cP.y);
      if (d > maxPosDiff) maxPosDiff = d;
    }
  }

  // Compare kills
  const serverKills = new Set((serverRes.kills || []).map((k) => k.victim));
  const clientKills = new Set((finalSnap.kills || []).map((k) => k.victim));
  const killMismatches: string[] = [];
  for (const v of serverKills) {
    if (!clientKills.has(v)) killMismatches.push(`Missed: Server killed ${v}`);
  }
  for (const v of clientKills) {
    if (!serverKills.has(v)) killMismatches.push(`Ghost: Overlay killed ${v}`);
  }

  const hasDiff = killMismatches.length > 0 || terrainDiffCount > 5 || maxPosDiff > 2.0;

  // Status Box
  if (hasDiff) {
    diffStatusPill.className = 'status-pill diff';
    diffStatusText.textContent = 'DISCREPANCY';
    inspectorStatusBox.className = 'status-box diff';
    inspectorStatusBox.innerHTML = `
      <div class="status-title">DISCREPANCY DETECTED</div>
      <div class="status-desc">${killMismatches.length ? killMismatches[0] : `Terrain drifted by ${maxTerrainDiff.toFixed(1)}px`}</div>
    `;
  } else {
    diffStatusPill.className = 'status-pill running';
    diffStatusText.textContent = 'MATCH';
    inspectorStatusBox.className = 'status-box match';
    inspectorStatusBox.innerHTML = `
      <div class="status-title">PERFECT 100% MATCH</div>
      <div class="status-desc">Server and Overlay outcomes are completely identical.</div>
    `;
  }

  metricTerrainDiff.textContent = `${maxTerrainDiff.toFixed(1)}px`;
  metricPosDiff.textContent = `${maxPosDiff.toFixed(1)}px`;
  metricKillMismatch.textContent = String(killMismatches.length);
  metricSteps.textContent = String(finalSnap.step);

  // Kills Breakdown
  if ((serverRes.kills || []).length === 0 && (finalSnap.kills || []).length === 0) {
    killsComparisonBody.innerHTML = '<div class="empty-text">No casualties in this round</div>';
  } else {
    const allVictims = new Set([...serverKills, ...clientKills]);
    killsComparisonBody.innerHTML = Array.from(allVictims)
      .map((victim) => {
        const inServer = serverKills.has(victim);
        const inClient = clientKills.has(victim);
        const isMismatch = inServer !== inClient;
        const tag = isMismatch ? (inClient ? '[GHOST KILL]' : '[MISSED KILL]') : '[VERIFIED KILL]';
        return `
          <div class="comparison-item ${isMismatch ? 'mismatch' : ''}">
              ${tag} <strong>${escapeHtml(victim)}</strong> — Server: ${inServer ? 'DEAD' : 'ALIVE'} | Overlay: ${inClient ? 'DEAD' : 'ALIVE'}
          </div>
        `;
      })
      .join('');
  }
}

function updateFrameInspector(): void {
  if (frameSnapshots.length === 0 || !currentServerResult) return;
  const snap = frameSnapshots[currentFrame];

  const rows: string[] = [];
  for (const name in snap.players) {
    const p = snap.players[name];
    const sP = currentServerResult.finalPlayers[name];
    const sStr = sP ? `(${sP.x.toFixed(1)}, ${sP.y.toFixed(1)})` : 'N/A';
    const cStr = `(${p.x.toFixed(1)}, ${p.y.toFixed(1)})`;
    const delta = sP ? Math.hypot(p.x - sP.x, p.y - sP.y).toFixed(1) : '-';

    rows.push(`
      <tr>
          <td><strong>${escapeHtml(name)}</strong></td>
          <td>${sStr}</td>
          <td>${cStr}</td>
          <td style="${parseFloat(delta) > 1 ? 'color: var(--neon-red); font-weight: bold;' : ''}">${delta}px</td>
      </tr>
    `);
  }

  inspectorTankTbody.innerHTML = rows.join('') || '<tr><td colspan="4" class="empty-text">No tanks</td></tr>';
}

async function runFuzzSuite(): Promise<void> {
  btnRunFuzz.disabled = true;
  fuzzProgressContainer.style.display = 'block';
  diffStatusPill.className = 'status-pill running';
  diffStatusText.textContent = 'FUZZING...';

  const total = fuzzBatchSize;
  let passed = 0;
  let discrepancies = 0;
  const batchSize = 25;
  const totalBatches = Math.ceil(total / batchSize);

  try {
    for (let b = 0; b < totalBatches; b++) {
      const count = Math.min(batchSize, total - b * batchSize);
      const res = await fetch('/api/scenarios/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ count, seed: Date.now() + b * 1337 }),
      });
      const data = await res.json();
      const items: { scenario: Scenario; serverResult: ServerSimulationResult }[] = data.scenarios || [];

      for (const item of items) {
        const state: SimulationState = {
          players: { ...item.scenario.players },
          projectiles: [],
          terrain: [...item.scenario.terrain],
          bouncyWalls: item.scenario.rules.bouncyWalls,
          terrainClimb: item.scenario.rules.terrainClimb,
          moveDistance: item.scenario.rules.moveDistance,
          physicsSpeed: item.scenario.rules.physicsSpeed,
          roundId: 1,
        };

        const clientRes = runFullSimulation(state, 1.0);

        // Check for mismatch
        const serverKills = new Set((item.serverResult.kills || []).map((k) => k.victim));
        const clientKills = new Set((clientRes.kills || []).map((k) => k.victim));
        let hasMismatch = false;

        for (const v of serverKills) if (!clientKills.has(v)) hasMismatch = true;
        for (const v of clientKills) if (!serverKills.has(v)) hasMismatch = true;

        if (!hasMismatch) {
          for (let x = 0; x < WIDTH; x++) {
            if (Math.abs(clientRes.finalTerrain[x] - item.serverResult.finalTerrain[x]) > 0.5) {
              hasMismatch = true;
              break;
            }
          }
        }

        if (hasMismatch) {
          discrepancies++;
          // Save to server
          await fetch('/api/scenarios/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              scenario: item.scenario,
              diffReport: {
                scenarioId: item.scenario.id,
                hasDiscrepancy: true,
                summary: 'Browser differential fuzz mismatch',
              },
            }),
          });
        } else {
          passed++;
        }
      }

      const completed = Math.min(total, (b + 1) * batchSize);
      const pct = Math.round((completed / total) * 100);
      fuzzProgressBar.style.width = `${pct}%`;
      fuzzProgressLabel.textContent = `${pct}% (${completed}/${total})`;
      fuzzStatsLabel.textContent = `Passed: ${passed} | Diffs: ${discrepancies}`;
    }

    await loadScenarioLibrary();
  } catch (err) {
    console.error('Fuzz suite error:', err);
  } finally {
    btnRunFuzz.disabled = false;
    diffStatusPill.className = discrepancies > 0 ? 'status-pill diff' : 'status-pill idle';
    diffStatusText.textContent = discrepancies > 0 ? 'DIFFS FOUND' : 'READY';
  }
}

function escapeHtml(str: string): string {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}
