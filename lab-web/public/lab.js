"use strict";
(() => {
  // web/src/types.ts
  var WIDTH = 1920;
  var HEIGHT = 1080;
  var ActionFire = "FIRE";
  var ActionLeft = "LEFT";
  var ActionRight = "RIGHT";
  var ActionShield = "SHIELD";

  // web/src/terrain.ts
  function getTerrainHeight(terrain, x) {
    if (terrain.length === 0) return HEIGHT / 2;
    const idx = Math.min(WIDTH - 1, Math.max(0, Math.floor(x)));
    return terrain[idx];
  }
  function applyCrater(terrain, cx, cy, radius) {
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

  // web/src/simulation.ts
  function executeActions(state) {
    state.projectiles = [];
    const roundId = state.roundId ?? 0;
    for (const name in state.players) {
      const p = state.players[name];
      if (p.isDead) continue;
      if (p.actionType === ActionFire) {
        const rad = (p.angle ?? 45) * Math.PI / 180;
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
          emoteUrl: p.emoteUrl
        });
      } else if (p.actionType === ActionLeft) {
        p.moveTarget = p.x - (state.moveDistance || 100);
        p.moving = true;
        p.speedMultiplier = 1;
        p.hasBounced = false;
      } else if (p.actionType === ActionRight) {
        p.moveTarget = p.x + (state.moveDistance || 100);
        p.moving = true;
        p.speedMultiplier = 1;
        p.hasBounced = false;
      } else if (p.actionType === ActionShield) {
        p.moving = false;
      }
    }
  }
  function checkTankCollisions(players, cx, cy, radius, owner) {
    const kills = [];
    for (const name in players) {
      if (name === owner) continue;
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
          reason: "blast"
        });
      }
    }
    return kills;
  }
  function stepSimulation(state, dtScale) {
    const events = {
      impacts: [],
      kills: [],
      wallSparks: [],
      anyMoving: false
    };
    const { bouncyWalls, terrainClimb } = state;
    for (const name in state.players) {
      const p = state.players[name];
      if (p.isDead) continue;
      if (p.moving) {
        events.anyMoving = true;
        const currentSpeed = (p.speedMultiplier ?? 1) * 2 * dtScale;
        if (p.actionType === ActionLeft) {
          const nextX = p.x - currentSpeed;
          const currIdx = Math.floor(p.x);
          const nextIdx = Math.floor(nextX);
          let blocked = false;
          if (!terrainClimb && currIdx !== nextIdx) {
            const stepX = Math.abs(currIdx - nextIdx);
            const rise = getTerrainHeight(state.terrain, currIdx) - getTerrainHeight(state.terrain, nextIdx);
            if (rise > 0 && rise / stepX > 4) {
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
              } else if (p.moveTarget !== void 0 && p.x <= p.moveTarget || p.x <= 20) {
                p.moving = false;
                if (p.x < 20) p.x = 20;
              }
            } else if (p.moveTarget !== void 0 && p.x <= p.moveTarget) {
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
            if (rise > 0 && rise / stepX > 4) {
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
              } else if (p.moveTarget !== void 0 && p.x >= p.moveTarget || p.x >= WIDTH - 20) {
                p.moving = false;
                if (p.x > WIDTH - 20) p.x = WIDTH - 20;
              }
            } else if (p.moveTarget !== void 0 && p.x >= p.moveTarget) {
              p.moving = false;
            }
          }
        }
      }
      if (p.x < 20) p.x = 20;
      if (p.x > WIDTH - 20) p.x = WIDTH - 20;
      const floorY = getTerrainHeight(state.terrain, p.x);
      if (p.y < floorY) {
        p.y += 5 * dtScale;
        if (p.y > floorY) p.y = floorY;
      } else {
        p.y = floorY;
      }
      if (p.y >= HEIGHT) {
        p.isDead = true;
        events.kills.push({
          killer: "",
          victim: name,
          x: p.x,
          y: p.y,
          reason: "abyss"
        });
      }
    }
    for (let i = state.projectiles.length - 1; i >= 0; i--) {
      const proj = state.projectiles[i];
      proj.x += proj.vx * dtScale;
      proj.vy += 0.2 * dtScale;
      proj.y += proj.vy * dtScale;
      if (proj.trail) {
        proj.trail.push({ x: proj.x, y: proj.y });
        if (proj.trail.length > 20) proj.trail.shift();
      }
      let hit = false;
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
              hitType: "shield"
            });
            break;
          }
        }
      }
      if (!hit && proj.y >= 0 && proj.y >= getTerrainHeight(state.terrain, proj.x)) {
        hit = true;
        applyCrater(state.terrain, proj.x, proj.y, 50);
        events.impacts.push({
          id: proj.id,
          x: proj.x,
          y: proj.y,
          radius: 50,
          owner: proj.owner,
          hitType: "terrain"
        });
        const blastKills = checkTankCollisions(state.players, proj.x, proj.y, 50, proj.owner);
        events.kills.push(...blastKills);
      }
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
              hitType: "tank"
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
  function runFullSimulation(state, dtScale = 1, maxSteps = 2e3) {
    executeActions(state);
    const allKills = [];
    const allImpacts = [];
    let step = 0;
    while (step < maxSteps) {
      step++;
      const events = stepSimulation(state, dtScale);
      allKills.push(...events.kills);
      allImpacts.push(...events.impacts);
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
    const finalPlayers = {};
    for (const name in state.players) {
      const p = state.players[name];
      finalPlayers[name] = {
        x: p.x,
        y: p.y,
        isDead: !!p.isDead
      };
    }
    return {
      kills: allKills,
      impacts: allImpacts,
      finalTerrain: [...state.terrain],
      finalPlayers,
      totalSteps: step
    };
  }

  // lab-web/src/lab.ts
  var canvas = document.getElementById("replayCanvas");
  var ctx = canvas.getContext("2d");
  var diffStatusPill = document.getElementById("diff-status-pill");
  var diffStatusText = document.getElementById("diff-status-text");
  var scenarioListEl = document.getElementById("scenario-list");
  var scenarioCountBadge = document.getElementById("scenario-count-badge");
  var btnRefresh = document.getElementById("btn-refresh-scenarios");
  var btnRunFuzz = document.getElementById("btn-run-fuzz");
  var fuzzProgressContainer = document.getElementById("fuzz-progress-container");
  var fuzzProgressBar = document.getElementById("fuzz-progress-bar");
  var fuzzProgressLabel = document.getElementById("fuzz-progress-label");
  var fuzzStatsLabel = document.getElementById("fuzz-stats-label");
  var batchButtons = document.querySelectorAll(".btn-batch");
  var btnRewind = document.getElementById("btn-rewind");
  var btnStepBack = document.getElementById("btn-step-back");
  var btnPlayPause = document.getElementById("btn-play-pause");
  var btnStepForward = document.getElementById("btn-step-forward");
  var timelineScrubber = document.getElementById("timeline-scrubber");
  var timelineCurrentFrame = document.getElementById("timeline-current-frame");
  var timelineTotalFrames = document.getElementById("timeline-total-frames");
  var selectSpeed = document.getElementById("select-speed");
  var inspectorStatusBox = document.getElementById("inspector-status-box");
  var metricTerrainDiff = document.getElementById("metric-terrain-diff");
  var metricPosDiff = document.getElementById("metric-pos-diff");
  var metricKillMismatch = document.getElementById("metric-kill-mismatch");
  var metricSteps = document.getElementById("metric-steps");
  var killsComparisonBody = document.getElementById("kills-comparison-body");
  var inspectorTankTbody = document.getElementById("inspector-tank-tbody");
  var btnToggleJson = document.getElementById("btn-toggle-json");
  var rawJsonViewer = document.getElementById("raw-json-viewer");
  var currentScenario = null;
  var currentServerResult = null;
  var frameSnapshots = [];
  var currentFrame = 0;
  var isPlaying = false;
  var playTimer = null;
  var playbackSpeed = 0.5;
  var fuzzBatchSize = 100;
  batchButtons.forEach((btn) => {
    btn.addEventListener("click", () => {
      batchButtons.forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      fuzzBatchSize = parseInt(btn.dataset.count || "100", 10);
    });
  });
  init();
  async function init() {
    setupEventListeners();
    await loadScenarioLibrary();
  }
  function setupEventListeners() {
    btnRefresh.addEventListener("click", loadScenarioLibrary);
    btnRunFuzz.addEventListener("click", runFuzzSuite);
    btnPlayPause.addEventListener("click", togglePlayPause);
    btnRewind.addEventListener("click", () => seekFrame(0));
    btnStepBack.addEventListener("click", () => seekFrame(Math.max(0, currentFrame - 1)));
    btnStepForward.addEventListener("click", () => seekFrame(Math.min(frameSnapshots.length - 1, currentFrame + 1)));
    timelineScrubber.addEventListener("input", () => {
      seekFrame(parseInt(timelineScrubber.value, 10));
    });
    selectSpeed.addEventListener("change", () => {
      playbackSpeed = parseFloat(selectSpeed.value);
      if (isPlaying) {
        pausePlayback();
        startPlayback();
      }
    });
    btnToggleJson.addEventListener("click", () => {
      const isHidden = rawJsonViewer.style.display === "none";
      rawJsonViewer.style.display = isHidden ? "block" : "none";
    });
    window.addEventListener("keydown", (e) => {
      if (e.key === " " && e.target !== btnRunFuzz) {
        e.preventDefault();
        togglePlayPause();
      } else if (e.key === "ArrowRight") {
        seekFrame(Math.min(frameSnapshots.length - 1, currentFrame + 1));
      } else if (e.key === "ArrowLeft") {
        seekFrame(Math.max(0, currentFrame - 1));
      }
    });
  }
  async function loadScenarioLibrary() {
    try {
      const res = await fetch("/api/scenarios/list");
      const data = await res.json();
      renderScenarioList(data.scenarios || []);
    } catch (err) {
      scenarioListEl.innerHTML = `<div class="empty-text">Failed to load scenarios: ${String(err)}</div>`;
    }
  }
  function renderScenarioList(scenarios) {
    scenarioCountBadge.textContent = String(scenarios.length);
    if (scenarios.length === 0) {
      scenarioListEl.innerHTML = '<div class="empty-text">No scenarios found. Run fuzz suite to generate test cases.</div>';
      return;
    }
    scenarioListEl.innerHTML = scenarios.map(
      (sc) => `
        <div class="scenario-item ${sc.source} ${sc.source === "saved" ? "mismatch" : ""}" data-id="${sc.id}">
            <div class="scenario-item-header">
                <span class="scenario-name" title="${escapeHtml(sc.name)}">${escapeHtml(sc.name)}</span>
                <span class="scenario-source-tag ${sc.source}">${sc.source.toUpperCase()}</span>
            </div>
            <div class="scenario-desc">${escapeHtml(sc.description || sc.filename || sc.id)}</div>
        </div>
      `
    ).join("");
    document.querySelectorAll(".scenario-item").forEach((item) => {
      item.addEventListener("click", () => {
        document.querySelectorAll(".scenario-item").forEach((i) => i.classList.remove("active"));
        item.classList.add("active");
        const id = item.dataset.id;
        if (id) loadAndRunScenario(id);
      });
    });
    if (!currentScenario && scenarios.length > 0) {
      const first = document.querySelector(".scenario-item");
      if (first) first.click();
    }
  }
  async function loadAndRunScenario(id) {
    pausePlayback();
    try {
      const res = await fetch(`/api/scenarios/load?id=${encodeURIComponent(id)}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const sc = await res.json();
      currentScenario = sc;
      const serverRes = await fetch("/api/scenarios/run", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(sc)
      });
      currentServerResult = await serverRes.json();
      recordOverlaySimulation(sc);
      updateInspector(sc, currentServerResult);
      seekFrame(0);
      rawJsonViewer.textContent = JSON.stringify(sc, null, 2);
    } catch (err) {
      console.error("Failed to load scenario:", err);
    }
  }
  function recordOverlaySimulation(sc) {
    frameSnapshots = [];
    const simPlayers = {};
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
        isDead: p.isDead
      };
    }
    const state = {
      players: simPlayers,
      projectiles: [],
      terrain: [...sc.terrain],
      bouncyWalls: sc.rules.bouncyWalls,
      terrainClimb: sc.rules.terrainClimb,
      moveDistance: sc.rules.moveDistance,
      physicsSpeed: sc.rules.physicsSpeed,
      roundId: 1
    };
    executeActions(state);
    saveFrameSnapshot(0, state, [], []);
    const accumulatedImpacts = [];
    const accumulatedKills = [];
    let step = 0;
    const maxSteps = 2e3;
    while (step < maxSteps) {
      step++;
      const events = stepSimulation(state, 1);
      accumulatedImpacts.push(...events.impacts);
      accumulatedKills.push(...events.kills);
      saveFrameSnapshot(step, state, accumulatedImpacts, accumulatedKills);
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
  function saveFrameSnapshot(step, state, impacts, kills) {
    const pMap = {};
    for (const name in state.players) {
      const p = state.players[name];
      pMap[name] = {
        x: p.x,
        y: p.y,
        isDead: !!p.isDead,
        isShielded: !!p.isShielded
      };
    }
    frameSnapshots.push({
      step,
      players: pMap,
      projectiles: state.projectiles.map((proj) => ({ x: proj.x, y: proj.y, vx: proj.vx, vy: proj.vy, owner: proj.owner })),
      terrain: [...state.terrain],
      impacts: [...impacts],
      kills: [...kills]
    });
  }
  function seekFrame(frameIndex) {
    currentFrame = Math.max(0, Math.min(frameSnapshots.length - 1, frameIndex));
    timelineScrubber.value = String(currentFrame);
    timelineCurrentFrame.textContent = `Frame ${currentFrame}`;
    renderCanvasFrame();
    updateFrameInspector();
  }
  function togglePlayPause() {
    if (isPlaying) {
      pausePlayback();
    } else {
      startPlayback();
    }
  }
  function startPlayback() {
    if (frameSnapshots.length === 0) return;
    if (currentFrame >= frameSnapshots.length - 1) {
      currentFrame = 0;
    }
    isPlaying = true;
    btnPlayPause.innerHTML = "&#10074;&#10074; Pause";
    const frameInterval = 1e3 / 60 / playbackSpeed;
    playTimer = window.setInterval(() => {
      if (currentFrame < frameSnapshots.length - 1) {
        seekFrame(currentFrame + 1);
      } else {
        pausePlayback();
      }
    }, frameInterval);
  }
  function pausePlayback() {
    isPlaying = false;
    btnPlayPause.innerHTML = "&#9654; Play";
    if (playTimer !== null) {
      clearInterval(playTimer);
      playTimer = null;
    }
  }
  function renderCanvasFrame() {
    if (frameSnapshots.length === 0 || !currentScenario) return;
    const snap = frameSnapshots[currentFrame];
    ctx.clearRect(0, 0, WIDTH, HEIGHT);
    ctx.beginPath();
    ctx.moveTo(0, snap.terrain[0]);
    for (let x = 1; x < WIDTH; x++) {
      ctx.lineTo(x, snap.terrain[x]);
    }
    ctx.strokeStyle = "#ff003c";
    ctx.lineWidth = 3;
    ctx.shadowBlur = 10;
    ctx.shadowColor = "#ff003c";
    ctx.stroke();
    ctx.lineTo(WIDTH, HEIGHT);
    ctx.lineTo(0, HEIGHT);
    ctx.closePath();
    ctx.fillStyle = "rgba(255, 0, 60, 0.03)";
    ctx.fill();
    ctx.shadowBlur = 0;
    if (currentServerResult && currentServerResult.finalTerrain.length === WIDTH) {
      ctx.beginPath();
      ctx.moveTo(0, currentServerResult.finalTerrain[0]);
      for (let x = 1; x < WIDTH; x++) {
        ctx.lineTo(x, currentServerResult.finalTerrain[x]);
      }
      ctx.strokeStyle = "rgba(0, 255, 204, 0.4)";
      ctx.lineWidth = 2;
      ctx.setLineDash([4, 4]);
      ctx.stroke();
      ctx.setLineDash([]);
    }
    if (currentServerResult) {
      for (let x = 0; x < WIDTH; x++) {
        const diff = Math.abs(snap.terrain[x] - currentServerResult.finalTerrain[x]);
        if (diff > 0.5) {
          ctx.fillStyle = "rgba(255, 183, 3, 0.35)";
          const topY = Math.min(snap.terrain[x], currentServerResult.finalTerrain[x]);
          const h = Math.abs(snap.terrain[x] - currentServerResult.finalTerrain[x]);
          ctx.fillRect(x, topY, 1, Math.max(h, 4));
        }
      }
    }
    for (const proj of snap.projectiles) {
      ctx.beginPath();
      ctx.arc(proj.x, proj.y, 6, 0, Math.PI * 2);
      ctx.fillStyle = "#ff003c";
      ctx.shadowBlur = 15;
      ctx.shadowColor = "#ff003c";
      ctx.fill();
      ctx.shadowBlur = 0;
    }
    for (const imp of snap.impacts) {
      ctx.beginPath();
      ctx.arc(imp.x, imp.y, imp.radius, 0, Math.PI * 2);
      ctx.strokeStyle = "rgba(255, 0, 60, 0.6)";
      ctx.lineWidth = 2;
      ctx.stroke();
    }
    if (currentServerResult) {
      for (const sImp of currentServerResult.impacts) {
        ctx.beginPath();
        ctx.arc(sImp.x, sImp.y, sImp.radius, 0, Math.PI * 2);
        ctx.strokeStyle = "rgba(0, 255, 204, 0.5)";
        ctx.lineWidth = 2;
        ctx.setLineDash([6, 6]);
        ctx.stroke();
        ctx.setLineDash([]);
      }
    }
    for (const name in snap.players) {
      const p = snap.players[name];
      if (p.isDead) continue;
      ctx.fillStyle = "#ff003c";
      ctx.fillRect(p.x - 15, p.y - 10, 30, 10);
      if (p.isShielded) {
        ctx.beginPath();
        ctx.arc(p.x, p.y - 5, 40, Math.PI, 0);
        ctx.strokeStyle = "rgba(0, 255, 204, 0.8)";
        ctx.lineWidth = 3;
        ctx.stroke();
      }
      if (currentServerResult && currentServerResult.finalPlayers[name]) {
        const sP = currentServerResult.finalPlayers[name];
        const dist = Math.hypot(p.x - sP.x, p.y - sP.y);
        if (dist > 1) {
          ctx.strokeStyle = "#00ffcc";
          ctx.lineWidth = 1;
          ctx.strokeRect(sP.x - 15, sP.y - 10, 30, 10);
        }
      }
      ctx.font = "12px Orbitron";
      ctx.fillStyle = "#f0f4fc";
      ctx.textAlign = "center";
      ctx.fillText(name, p.x, p.y - 16);
    }
  }
  function updateInspector(_sc, serverRes) {
    if (!serverRes || frameSnapshots.length === 0) return;
    const finalSnap = frameSnapshots[frameSnapshots.length - 1];
    let maxTerrainDiff = 0;
    let terrainDiffCount = 0;
    for (let x = 0; x < WIDTH; x++) {
      const d = Math.abs(finalSnap.terrain[x] - serverRes.finalTerrain[x]);
      if (d > 0.15) {
        terrainDiffCount++;
        if (d > maxTerrainDiff) maxTerrainDiff = d;
      }
    }
    let maxPosDiff = 0;
    for (const name in serverRes.finalPlayers) {
      const sP = serverRes.finalPlayers[name];
      const cP = finalSnap.players[name];
      if (cP) {
        const d = Math.hypot(sP.x - cP.x, sP.y - cP.y);
        if (d > maxPosDiff) maxPosDiff = d;
      }
    }
    const serverKills = new Set(serverRes.kills.map((k) => k.victim));
    const clientKills = new Set(finalSnap.kills.map((k) => k.victim));
    const killMismatches = [];
    for (const v of serverKills) {
      if (!clientKills.has(v)) killMismatches.push(`Missed: Server killed ${v}`);
    }
    for (const v of clientKills) {
      if (!serverKills.has(v)) killMismatches.push(`Ghost: Overlay killed ${v}`);
    }
    const hasDiff = killMismatches.length > 0 || terrainDiffCount > 5 || maxPosDiff > 2;
    if (hasDiff) {
      diffStatusPill.className = "status-pill diff";
      diffStatusText.textContent = "DISCREPANCY";
      inspectorStatusBox.className = "status-box diff";
      inspectorStatusBox.innerHTML = `
      <div class="status-title">DISCREPANCY DETECTED</div>
      <div class="status-desc">${killMismatches.length ? killMismatches[0] : `Terrain drifted by ${maxTerrainDiff.toFixed(1)}px`}</div>
    `;
    } else {
      diffStatusPill.className = "status-pill running";
      diffStatusText.textContent = "MATCH";
      inspectorStatusBox.className = "status-box match";
      inspectorStatusBox.innerHTML = `
      <div class="status-title">PERFECT 100% MATCH</div>
      <div class="status-desc">Server and Overlay outcomes are completely identical.</div>
    `;
    }
    metricTerrainDiff.textContent = `${maxTerrainDiff.toFixed(1)}px`;
    metricPosDiff.textContent = `${maxPosDiff.toFixed(1)}px`;
    metricKillMismatch.textContent = String(killMismatches.length);
    metricSteps.textContent = String(finalSnap.step);
    if (serverRes.kills.length === 0 && finalSnap.kills.length === 0) {
      killsComparisonBody.innerHTML = '<div class="empty-text">No casualties in this round</div>';
    } else {
      const allVictims = /* @__PURE__ */ new Set([...serverKills, ...clientKills]);
      killsComparisonBody.innerHTML = Array.from(allVictims).map((victim) => {
        const inServer = serverKills.has(victim);
        const inClient = clientKills.has(victim);
        const isMismatch = inServer !== inClient;
        const tag = isMismatch ? inClient ? "[GHOST KILL]" : "[MISSED KILL]" : "[VERIFIED KILL]";
        return `
          <div class="comparison-item ${isMismatch ? "mismatch" : ""}">
              ${tag} <strong>${escapeHtml(victim)}</strong> \u2014 Server: ${inServer ? "DEAD" : "ALIVE"} | Overlay: ${inClient ? "DEAD" : "ALIVE"}
          </div>
        `;
      }).join("");
    }
  }
  function updateFrameInspector() {
    if (frameSnapshots.length === 0 || !currentServerResult) return;
    const snap = frameSnapshots[currentFrame];
    const rows = [];
    for (const name in snap.players) {
      const p = snap.players[name];
      const sP = currentServerResult.finalPlayers[name];
      const sStr = sP ? `(${sP.x.toFixed(1)}, ${sP.y.toFixed(1)})` : "N/A";
      const cStr = `(${p.x.toFixed(1)}, ${p.y.toFixed(1)})`;
      const delta = sP ? Math.hypot(p.x - sP.x, p.y - sP.y).toFixed(1) : "-";
      rows.push(`
      <tr>
          <td><strong>${escapeHtml(name)}</strong></td>
          <td>${sStr}</td>
          <td>${cStr}</td>
          <td style="${parseFloat(delta) > 1 ? "color: var(--neon-red); font-weight: bold;" : ""}">${delta}px</td>
      </tr>
    `);
    }
    inspectorTankTbody.innerHTML = rows.join("") || '<tr><td colspan="4" class="empty-text">No tanks</td></tr>';
  }
  async function runFuzzSuite() {
    btnRunFuzz.disabled = true;
    fuzzProgressContainer.style.display = "block";
    diffStatusPill.className = "status-pill running";
    diffStatusText.textContent = "FUZZING...";
    const total = fuzzBatchSize;
    let passed = 0;
    let discrepancies = 0;
    const batchSize = 25;
    const totalBatches = Math.ceil(total / batchSize);
    try {
      for (let b = 0; b < totalBatches; b++) {
        const count = Math.min(batchSize, total - b * batchSize);
        const res = await fetch("/api/scenarios/generate", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ count, seed: Date.now() + b * 1337 })
        });
        const data = await res.json();
        const items = data.scenarios || [];
        for (const item of items) {
          const state = {
            players: { ...item.scenario.players },
            projectiles: [],
            terrain: [...item.scenario.terrain],
            bouncyWalls: item.scenario.rules.bouncyWalls,
            terrainClimb: item.scenario.rules.terrainClimb,
            moveDistance: item.scenario.rules.moveDistance,
            physicsSpeed: item.scenario.rules.physicsSpeed,
            roundId: 1
          };
          const clientRes = runFullSimulation(state, 1);
          const serverKills = new Set(item.serverResult.kills.map((k) => k.victim));
          const clientKills = new Set(clientRes.kills.map((k) => k.victim));
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
            await fetch("/api/scenarios/save", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({
                scenario: item.scenario,
                diffReport: {
                  scenarioId: item.scenario.id,
                  hasDiscrepancy: true,
                  summary: "Browser differential fuzz mismatch"
                }
              })
            });
          } else {
            passed++;
          }
        }
        const completed = Math.min(total, (b + 1) * batchSize);
        const pct = Math.round(completed / total * 100);
        fuzzProgressBar.style.width = `${pct}%`;
        fuzzProgressLabel.textContent = `${pct}% (${completed}/${total})`;
        fuzzStatsLabel.textContent = `Passed: ${passed} | Diffs: ${discrepancies}`;
      }
      await loadScenarioLibrary();
    } catch (err) {
      console.error("Fuzz suite error:", err);
    } finally {
      btnRunFuzz.disabled = false;
      diffStatusPill.className = discrepancies > 0 ? "status-pill diff" : "status-pill idle";
      diffStatusText.textContent = discrepancies > 0 ? "DIFFS FOUND" : "READY";
    }
  }
  function escapeHtml(str) {
    const div = document.createElement("div");
    div.textContent = str;
    return div.innerHTML;
  }
})();
//# sourceMappingURL=lab.js.map
