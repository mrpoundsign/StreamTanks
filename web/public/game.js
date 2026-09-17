"use strict";
(() => {
  // web/src/types.ts
  var WIDTH = 1920;
  var HEIGHT = 1080;
  var PhaseIdle = "IDLE";
  var PhaseInput = "INPUT";
  var PhaseAction = "ACTION";
  var PhaseCelebration = "CELEBRATION";
  var MsgStateUpdate = "STATE_UPDATE";
  var MsgExecuteActions = "EXECUTE_ACTIONS";
  var MsgResetTerrain = "RESET_TERRAIN";
  var MsgPlayerDied = "PLAYER_DIED";
  var MsgChatCommand = "CHAT_COMMAND";
  var MsgTerrainCrater = "TERRAIN_CRATER";
  var ActionFire = "FIRE";
  var ActionLeft = "LEFT";
  var ActionRight = "RIGHT";

  // web/src/terrain.ts
  function createDefaultTerrain() {
    const terrain2 = new Array(WIDTH);
    let y = HEIGHT / 2 + (Math.random() * 200 - 100);
    let slope = 0;
    terrain2[0] = y;
    for (let x = 1; x < WIDTH; x++) {
      slope += (Math.random() - 0.5) * 0.15;
      if (slope > 2) slope = 2;
      if (slope < -2) slope = -2;
      y += slope;
      if (y < 250) slope += 0.05;
      if (y > HEIGHT - 200) slope -= 0.05;
      terrain2[x] = y;
    }
    return terrain2;
  }
  function getTerrainHeight(terrain2, x) {
    if (terrain2.length === 0) return HEIGHT / 2;
    const idx = Math.min(WIDTH - 1, Math.max(0, Math.floor(x)));
    return terrain2[idx];
  }
  function getTerrainSlopeAngle(terrain2, x, delta = 5) {
    const x1 = Math.max(0, Math.floor(x - delta));
    const x2 = Math.min(WIDTH - 1, Math.floor(x + delta));
    const y1 = terrain2[x1];
    const y2 = terrain2[x2];
    return Math.atan2(y2 - y1, x2 - x1);
  }
  function applyCrater(terrain2, cx, cy, radius) {
    const startX = Math.max(0, Math.floor(cx - radius));
    const endX = Math.min(WIDTH, Math.ceil(cx + radius));
    for (let x = startX; x < endX; x++) {
      const dx = x - cx;
      const dy = Math.sqrt(radius * radius - dx * dx);
      const circleBottomY = cy + dy;
      if (terrain2[x] < circleBottomY) {
        terrain2[x] = circleBottomY;
      }
    }
  }

  // web/src/network.ts
  var NetworkManager = class {
    ws = null;
    isDisconnected = false;
    reconnectInterval = null;
    healthCheckInterval = null;
    messageHandlers = [];
    constructor() {
      this.connect();
    }
    onMessage(handler) {
      this.messageHandlers.push(handler);
    }
    send(msg) {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify(msg));
      }
    }
    connect() {
      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
      const wsUrl = `${protocol}//${window.location.host}/ws`;
      try {
        this.ws = new WebSocket(wsUrl);
        this.ws.onopen = () => {
          console.log("Connected to StreamTanks server");
          if (this.isDisconnected) {
            window.location.reload();
            return;
          }
          if (this.healthCheckInterval) clearInterval(this.healthCheckInterval);
          this.healthCheckInterval = setInterval(() => {
            if (this.ws && this.ws.readyState === WebSocket.CLOSED) {
              this.handleDisconnect();
            }
          }, 2e3);
        };
        this.ws.onmessage = (event) => {
          try {
            const msg = JSON.parse(event.data);
            for (const handler of this.messageHandlers) {
              handler(msg);
            }
          } catch (err) {
            console.error("Failed to parse WebSocket message:", err);
          }
        };
        this.ws.onclose = () => this.handleDisconnect();
        this.ws.onerror = () => this.handleDisconnect();
      } catch {
        this.handleDisconnect();
      }
    }
    handleDisconnect() {
      if (this.isDisconnected) return;
      this.isDisconnected = true;
      console.log("Server disconnected or unreachable. Hiding overlay and waiting for server to return...");
      const container = document.getElementById("game-container");
      if (container) {
        container.style.display = "none";
      }
      document.body.style.display = "none";
      if (this.healthCheckInterval) {
        clearInterval(this.healthCheckInterval);
        this.healthCheckInterval = null;
      }
      if (this.reconnectInterval) clearInterval(this.reconnectInterval);
      this.reconnectInterval = setInterval(async () => {
        try {
          const res = await fetch("/", { method: "HEAD", cache: "no-store" });
          if (res.ok) {
            console.log("Server detected back online! Reloading page...");
            clearInterval(this.reconnectInterval);
            window.location.reload();
          }
        } catch {
        }
      }, 1500);
    }
  };

  // web/src/renderer.ts
  function drawTerrain(ctx2, terrain2) {
    if (terrain2.length === 0) return;
    ctx2.beginPath();
    ctx2.moveTo(0, terrain2[0]);
    for (let x = 1; x < WIDTH; x++) {
      ctx2.lineTo(x, terrain2[x]);
    }
    ctx2.strokeStyle = "#ff003c";
    ctx2.lineWidth = 4;
    ctx2.shadowBlur = 15;
    ctx2.shadowColor = "#ff003c";
    ctx2.stroke();
    ctx2.lineTo(WIDTH, HEIGHT);
    ctx2.lineTo(0, HEIGHT);
    ctx2.closePath();
    ctx2.fillStyle = "rgba(255, 0, 60, 0.02)";
    ctx2.fill();
    ctx2.shadowBlur = 0;
  }
  function drawGiantProtractor(ctx2) {
    ctx2.save();
    ctx2.translate(250, 250);
    ctx2.strokeStyle = "rgba(0, 255, 204, 0.5)";
    ctx2.lineWidth = 10;
    ctx2.shadowBlur = 20;
    ctx2.shadowColor = "#00ffcc";
    ctx2.beginPath();
    ctx2.arc(0, 0, 150, Math.PI, 0);
    ctx2.stroke();
    ctx2.fillStyle = "#00ffcc";
    ctx2.font = "24px Orbitron";
    ctx2.textAlign = "center";
    ctx2.textBaseline = "middle";
    const majorAngles = [
      { deg: 0, label: "0\xB0" },
      { deg: 45, label: "45\xB0" },
      { deg: 90, label: "90\xB0" },
      { deg: 135, label: "135\xB0" },
      { deg: 180, label: "180\xB0" }
    ];
    for (const { deg, label } of majorAngles) {
      const rad = deg * Math.PI / 180;
      const x1 = Math.cos(rad) * 140;
      const y1 = -Math.sin(rad) * 140;
      const x2 = Math.cos(rad) * 160;
      const y2 = -Math.sin(rad) * 160;
      ctx2.lineWidth = 4;
      ctx2.beginPath();
      ctx2.moveTo(x1, y1);
      ctx2.lineTo(x2, y2);
      ctx2.stroke();
      const tx = Math.cos(rad) * 190;
      const ty = -Math.sin(rad) * 190;
      ctx2.fillText(label, tx, ty);
    }
    ctx2.lineWidth = 2;
    for (let deg = 15; deg < 180; deg += 15) {
      if (deg % 45 === 0) continue;
      const rad = deg * Math.PI / 180;
      const x1 = Math.cos(rad) * 145;
      const y1 = -Math.sin(rad) * 145;
      const x2 = Math.cos(rad) * 155;
      const y2 = -Math.sin(rad) * 155;
      ctx2.beginPath();
      ctx2.moveTo(x1, y1);
      ctx2.lineTo(x2, y2);
      ctx2.stroke();
    }
    ctx2.fillStyle = "#ff003c";
    ctx2.shadowColor = "#ff003c";
    ctx2.beginPath();
    ctx2.arc(0, 0, 8, 0, Math.PI * 2);
    ctx2.fill();
    ctx2.restore();
  }
  function drawTanks(ctx2, players2, terrain2, currentPhase2, emotesLayer2, _emoteCache) {
    for (const name in players2) {
      const p = players2[name];
      let imgEl = document.getElementById("emote-" + name);
      if (!imgEl && p.emoteUrl) {
        imgEl = document.createElement("img");
        imgEl.id = "emote-" + name;
        imgEl.className = "tank-emote";
        imgEl.src = p.emoteUrl;
        emotesLayer2.appendChild(imgEl);
      }
      if (p.isDead) {
        if (imgEl) imgEl.style.display = "none";
        continue;
      }
      ctx2.save();
      ctx2.translate(p.x, p.y);
      const angle = getTerrainSlopeAngle(terrain2, p.x);
      ctx2.rotate(angle);
      ctx2.strokeStyle = "#ff003c";
      ctx2.lineWidth = 2;
      ctx2.shadowBlur = 10;
      ctx2.shadowColor = "#ff003c";
      ctx2.strokeRect(-15, -10, 30, 10);
      ctx2.restore();
      if (imgEl) {
        imgEl.style.display = "block";
        imgEl.style.left = p.x - 14 + "px";
        imgEl.style.top = p.y - 35 + "px";
        imgEl.style.transformOrigin = "14px 35px";
        imgEl.style.transform = `rotate(${angle}rad)`;
      }
      const displayName = p.name && !p.name.startsWith("_bot_") ? p.name : !name.startsWith("_bot_") && !p.isBot ? name : "";
      if (displayName) {
        ctx2.fillStyle = "#fff";
        ctx2.font = "16px Orbitron";
        ctx2.textAlign = "center";
        ctx2.shadowBlur = 5;
        ctx2.shadowColor = "#000";
        ctx2.fillText(displayName, p.x, p.y + 20);
      }
      if (currentPhase2 === "INPUT") {
        ctx2.strokeStyle = p.fired ? "#00ffcc" : "#ff003c";
        ctx2.shadowColor = ctx2.strokeStyle;
        ctx2.lineWidth = 2;
        ctx2.beginPath();
        ctx2.arc(p.x, p.y - 10, 50, Math.PI, 0);
        ctx2.stroke();
        ctx2.fillStyle = ctx2.strokeStyle;
        ctx2.font = "10px Orbitron";
        const angles = [0, 45, 90, 135, 180];
        for (const deg of angles) {
          const rad = deg * Math.PI / 180;
          const innerR = 45;
          const outerR = 50;
          ctx2.beginPath();
          ctx2.moveTo(p.x + Math.cos(rad) * innerR, p.y - 10 - Math.sin(rad) * innerR);
          ctx2.lineTo(p.x + Math.cos(rad) * outerR, p.y - 10 - Math.sin(rad) * outerR);
          ctx2.stroke();
          ctx2.fillText(deg.toString(), p.x + Math.cos(rad) * 60, p.y - 10 - Math.sin(rad) * 60);
        }
        const aimAngle = (p.lastAngle ?? 45) * Math.PI / 180;
        ctx2.beginPath();
        ctx2.moveTo(p.x, p.y - 10);
        ctx2.lineTo(p.x + Math.cos(aimAngle) * 50, p.y - 10 - Math.sin(aimAngle) * 50);
        ctx2.stroke();
      }
    }
  }
  function drawProjectiles(ctx2, projectiles2, emoteCache2) {
    for (const proj of projectiles2) {
      const img = proj.emoteUrl ? emoteCache2[proj.emoteUrl] : null;
      if (img && img.complete && img.naturalWidth > 0) {
        ctx2.shadowBlur = 0;
        ctx2.drawImage(img, proj.x - 7, proj.y - 7, 14, 14);
      } else {
        ctx2.fillStyle = "#00ffcc";
        ctx2.shadowBlur = 10;
        ctx2.shadowColor = "#00ffcc";
        ctx2.beginPath();
        ctx2.arc(proj.x, proj.y, 4, 0, Math.PI * 2);
        ctx2.fill();
      }
    }
  }
  function drawExplosions(ctx2, explosions2) {
    for (const exp of explosions2) {
      if (exp.isSpark) {
        ctx2.strokeStyle = `rgba(0, 255, 204, ${exp.alpha})`;
        ctx2.shadowBlur = 15;
        ctx2.shadowColor = "#00ffcc";
        ctx2.lineWidth = 3;
      } else {
        ctx2.strokeStyle = `rgba(255, 0, 60, ${exp.alpha})`;
        ctx2.shadowBlur = 20;
        ctx2.shadowColor = "#ff003c";
        ctx2.lineWidth = 4;
      }
      ctx2.beginPath();
      ctx2.arc(exp.x, exp.y, exp.radius, 0, Math.PI * 2);
      ctx2.stroke();
    }
  }

  // web/src/game.ts
  var canvas = document.getElementById("gameCanvas");
  var ctx = canvas.getContext("2d");
  var phaseBadge = document.getElementById("phase-badge");
  var hudInstructions = document.getElementById("hud-instructions");
  var hudTop = document.getElementById("hud-top");
  var leaderboardEl = document.getElementById("leaderboard");
  var leaderboardList = document.getElementById("leaderboard-list");
  var leaderboardTicker = document.getElementById("leaderboard-ticker");
  var tickerTrack = document.getElementById("ticker-track");
  var timerDisplay = document.getElementById("timer-display");
  var killFeed = document.getElementById("kill-feed");
  var emotesLayer = document.getElementById("emotes-layer");
  var celebrationDisplay = document.getElementById("celebration-display");
  var hudAvatar = document.getElementById("hud-avatar");
  var celebrationRecap = document.getElementById("celebration-recap");
  var recapList = document.getElementById("recap-list");
  var configModal = document.getElementById("config-modal");
  var configTableBody = document.getElementById("config-table-body");
  var configDismissHint = document.getElementById("config-dismiss-hint");
  var debugBar = document.getElementById("debug-bar");
  var debugInput = document.getElementById("debug-input");
  var debugSendBtn = document.getElementById("debug-send-btn");
  var terrain = createDefaultTerrain();
  var players = {};
  var projectiles = [];
  var explosions = [];
  var currentPhase = PhaseIdle;
  var previousPhase = PhaseIdle;
  var inputTimer = 0;
  var celebrationWinner = "";
  var celebrationStartTime = 0;
  var lastTime = performance.now();
  var stateRef = null;
  var appliedCraterIds = /* @__PURE__ */ new Set();
  var avatarCache = {};
  var emoteCache = {};
  var net = new NetworkManager();
  function escapeHtml(str) {
    return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
  }
  function showKillMessage(msg) {
    const el = document.createElement("div");
    el.className = "kill-message";
    el.innerText = msg;
    killFeed.appendChild(el);
    setTimeout(() => {
      if (killFeed.contains(el)) {
        killFeed.removeChild(el);
      }
    }, 5e3);
  }
  function createWallSpark(cx, cy) {
    explosions.push({ x: cx, y: cy, radius: 0, maxRadius: 30, alpha: 1, isSpark: true });
  }
  function checkTankCollisions(cx, cy, radius, owner) {
    for (const name in players) {
      if (name === owner) continue;
      const p = players[name];
      if (p.isDead) continue;
      const dist = Math.hypot(p.x - cx, p.y - cy);
      if (dist < radius + 20) {
        p.isDead = true;
        const imgEl = document.getElementById("emote-" + name);
        if (imgEl) imgEl.style.display = "none";
      }
    }
  }
  function destroyTerrain(cx, cy, radius, shotId) {
    if (shotId) {
      if (appliedCraterIds.has(shotId)) {
        return;
      }
      appliedCraterIds.add(shotId);
    }
    applyCrater(terrain, cx, cy, radius);
    explosions.push({ x: cx, y: cy, radius: 0, maxRadius: radius, alpha: 1 });
  }
  function executeActions() {
    projectiles = [];
    for (const name in players) {
      const p = players[name];
      if (p.isDead) continue;
      if (p.actionType === ActionFire) {
        const rad = (p.angle ?? 45) * Math.PI / 180;
        const powerClamped = Math.min(Math.max(p.power ?? 50, 1), 100);
        const powerScaled = powerClamped / 5;
        const vx = Math.cos(rad) * powerScaled;
        const vy = -Math.sin(rad) * powerScaled;
        const shotId = `${stateRef?.roundId ?? 0}_${name}`;
        const muzzleDist = 25;
        const spawnX = p.x + Math.cos(rad) * muzzleDist;
        const spawnY = p.y - 10 - Math.sin(rad) * muzzleDist;
        projectiles.push({
          id: shotId,
          x: spawnX,
          y: spawnY,
          vx,
          vy,
          owner: name,
          emoteUrl: p.emoteUrl
        });
      } else if (p.actionType === ActionLeft) {
        p.moveTarget = p.x - (stateRef?.moveDistance ?? 100);
        p.moving = true;
        p.speedMultiplier = 1;
        p.hasBounced = false;
      } else if (p.actionType === ActionRight) {
        p.moveTarget = p.x + (stateRef?.moveDistance ?? 100);
        p.moving = true;
        p.speedMultiplier = 1;
        p.hasBounced = false;
      }
    }
  }
  function updatePhysics(dtScale) {
    const bouncyWalls = !!stateRef?.bouncyWalls;
    for (const name in players) {
      const p = players[name];
      if (p.isDead) continue;
      if (currentPhase === PhaseAction && p.moving) {
        const currentSpeed = (p.speedMultiplier ?? 1) * 2 * dtScale;
        if (p.actionType === ActionLeft) {
          p.x -= currentSpeed;
          if (p.x <= 20) {
            if (bouncyWalls && !p.hasBounced) {
              p.x = 20;
              p.actionType = ActionRight;
              p.moveTarget = p.x + (stateRef?.moveDistance ?? 100);
              p.speedMultiplier = 1.5;
              p.hasBounced = true;
              createWallSpark(20, p.y);
            } else if (p.moveTarget !== void 0 && p.x <= p.moveTarget || p.x <= 20) {
              p.moving = false;
              if (p.x < 20) p.x = 20;
            }
          } else if (p.moveTarget !== void 0 && p.x <= p.moveTarget) {
            p.moving = false;
          }
        } else if (p.actionType === ActionRight) {
          p.x += currentSpeed;
          if (p.x >= WIDTH - 20) {
            if (bouncyWalls && !p.hasBounced) {
              p.x = WIDTH - 20;
              p.actionType = ActionLeft;
              p.moveTarget = p.x - (stateRef?.moveDistance ?? 100);
              p.speedMultiplier = 1.5;
              p.hasBounced = true;
              createWallSpark(WIDTH - 20, p.y);
            } else if (p.moveTarget !== void 0 && p.x >= p.moveTarget || p.x >= WIDTH - 20) {
              p.moving = false;
              if (p.x > WIDTH - 20) p.x = WIDTH - 20;
            }
          } else if (p.moveTarget !== void 0 && p.x >= p.moveTarget) {
            p.moving = false;
          }
        }
      }
      if (p.x < 20) p.x = 20;
      if (p.x > WIDTH - 20) p.x = WIDTH - 20;
      const floorY = getTerrainHeight(terrain, p.x);
      if (p.y < floorY) {
        p.y += 5 * dtScale;
        if (p.y > floorY) p.y = floorY;
      } else {
        p.y = floorY;
      }
      if (p.y >= HEIGHT) {
        p.isDead = true;
        const imgEl = document.getElementById("emote-" + name);
        if (imgEl) imgEl.style.display = "none";
      }
    }
    for (let i = projectiles.length - 1; i >= 0; i--) {
      const proj = projectiles[i];
      proj.x += proj.vx * dtScale;
      proj.vy += 0.2 * dtScale;
      proj.y += proj.vy * dtScale;
      let hit = false;
      if (proj.y < 0) {
        if (bouncyWalls) {
          proj.y = 0;
          proj.vy = Math.abs(proj.vy) * 1.1;
          proj.vx *= 1.1;
          proj.bounces = (proj.bounces ?? 0) + 1;
          createWallSpark(Math.max(0, Math.min(WIDTH, proj.x)), 0);
          if (proj.bounces > 15) hit = true;
        }
      } else if (proj.y > HEIGHT) {
        if (bouncyWalls) {
          proj.y = HEIGHT;
          proj.vy = -Math.abs(proj.vy) * 1.1;
          proj.vx *= 1.1;
          proj.bounces = (proj.bounces ?? 0) + 1;
          createWallSpark(Math.max(0, Math.min(WIDTH, proj.x)), HEIGHT);
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
            createWallSpark(0, Math.max(0, Math.min(HEIGHT, proj.y)));
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
            createWallSpark(WIDTH, Math.max(0, Math.min(HEIGHT, proj.y)));
            if (proj.bounces > 15) hit = true;
          } else {
            hit = true;
          }
        }
      }
      if (!hit && proj.y >= 0 && proj.y >= getTerrainHeight(terrain, proj.x)) {
        hit = true;
        if (currentPhase === PhaseCelebration) {
          explosions.push({ x: proj.x, y: proj.y, radius: 0, maxRadius: 50, alpha: 1 });
        } else {
          destroyTerrain(proj.x, proj.y, 50, proj.id);
          checkTankCollisions(proj.x, proj.y, 50, proj.owner);
        }
      }
      if (!hit) {
        for (const name in players) {
          if (name === proj.owner) continue;
          const p = players[name];
          if (p.isDead) continue;
          if (Math.hypot(p.x - proj.x, p.y - proj.y) < 20) {
            hit = true;
            destroyTerrain(proj.x, proj.y, 50, proj.id);
            checkTankCollisions(proj.x, proj.y, 50, proj.owner);
            break;
          }
        }
      }
      if (hit) {
        projectiles.splice(i, 1);
      }
    }
    for (let i = explosions.length - 1; i >= 0; i--) {
      const exp = explosions[i];
      exp.radius += 2 * dtScale;
      exp.alpha -= 0.05 * dtScale;
      if (exp.alpha <= 0) {
        explosions.splice(i, 1);
      }
    }
    if (currentPhase === PhaseCelebration) {
      const elapsed = celebrationStartTime > 0 ? performance.now() - celebrationStartTime : 0;
      if (celebrationWinner && celebrationWinner !== "AI" && elapsed < 3500 && Math.random() < 0.2) {
        const p = players[celebrationWinner];
        projectiles.push({
          x: Math.random() * WIDTH,
          y: -30,
          vx: (Math.random() - 0.5) * 5,
          vy: Math.random() * 5 + 5,
          owner: celebrationWinner,
          emoteUrl: p?.emoteUrl ?? ""
        });
      }
    }
  }
  function updateLeaderboard(lb) {
    if (!leaderboardList) return;
    const sorted = Object.entries(lb).sort((a, b) => b[1] - a[1]);
    const top3 = sorted.slice(0, 3);
    const runnersUp = sorted.slice(3, 8);
    leaderboardList.innerHTML = top3.map(
      ([name, wins]) => {
        const hasUrl = avatarCache[name] && avatarCache[name] !== "fetching";
        const avatarUrl = hasUrl ? avatarCache[name] : "";
        return `
        <li>
            <div class="lb-player">
                <img id="lb-avatar-${name}" class="lb-avatar" src="${avatarUrl}" style="${hasUrl ? "" : "display:none;"}">
                <span class="lb-name" title="${escapeHtml(name)}">${escapeHtml(name)}</span>
            </div>
            <span class="lb-score">${wins}</span>
        </li>
    `;
      }
    ).join("");
    for (const [name] of top3) {
      if (!avatarCache[name]) {
        avatarCache[name] = "fetching";
        fetch(`https://decapi.me/twitch/avatar/${name}`).then((r) => r.text()).then((url) => {
          avatarCache[name] = url;
          const img = document.getElementById(`lb-avatar-${name}`);
          if (img) {
            img.src = url;
            img.style.display = "inline-block";
          }
        });
      }
    }
    if (leaderboardTicker && tickerTrack) {
      if (runnersUp.length > 0) {
        leaderboardTicker.style.display = "block";
        const itemsHtml = runnersUp.map(([name, wins], idx) => {
          const rank = idx + 4;
          return `<span class="ticker-item"><span class="ticker-rank">#${rank}</span> <span class="ticker-name">${escapeHtml(name)}</span> <span class="ticker-score">(${wins})</span></span>`;
        }).join('<span class="ticker-sep">\u2022</span>');
        tickerTrack.innerHTML = itemsHtml + '<span class="ticker-sep">\u2022</span>' + itemsHtml;
      } else {
        leaderboardTicker.style.display = "none";
        tickerTrack.innerHTML = "";
      }
    }
  }
  function renderConfigModal(prefix) {
    if (!configModal || !configTableBody) return;
    const isVisible = !!stateRef?.showConfig;
    if (!isVisible) {
      configModal.style.display = "none";
      return;
    }
    configModal.style.display = "flex";
    if (configDismissHint) {
      configDismissHint.innerText = `${prefix}config off`;
    }
    const speedVal = stateRef?.physicsSpeed ?? 0.5;
    const roundDuration = stateRef?.inputDuration ?? 20;
    const autoRoundVal = stateRef?.autoRound ?? 0;
    let autoRoundDisplay = `<span class="config-val badge-off">Off</span>`;
    if (autoRoundVal === -1) {
      autoRoundDisplay = `<span class="config-val badge-on">Immediate</span>`;
    } else if (autoRoundVal > 0) {
      autoRoundDisplay = `<span class="config-val badge-on">${autoRoundVal} min</span>`;
    }
    const idleMessageVal = stateRef?.idleMessage ?? true;
    const idleMessageDisplay = idleMessageVal ? `<span class="config-val badge-on">On</span>` : `<span class="config-val badge-off">Off</span>`;
    const bouncyVal = !!stateRef?.bouncyWalls;
    const bouncyDisplay = bouncyVal ? `<span class="config-val badge-on">On (+10% bullet, +50% tank)</span>` : `<span class="config-val badge-off">Off</span>`;
    const rows = [
      {
        label: "Command Prefix",
        value: `<span class="config-val">"${prefix}"</span>`,
        cmd: `<span class="config-cmd">${prefix}prefix <span class="cmd-param">&lt;str&gt;</span></span>`
      },
      {
        label: "Physics / Speed",
        value: `<span class="config-val">${speedVal}x</span>`,
        cmd: `<span class="config-cmd">${prefix}speed <span class="cmd-param">&lt;0.1 - 3.0&gt;</span></span>`
      },
      {
        label: "Command Time",
        value: `<span class="config-val">${roundDuration}s</span>`,
        cmd: `<span class="config-cmd">${prefix}commandtime <span class="cmd-param">&lt;seconds&gt;</span></span>`
      },
      {
        label: "Auto Round",
        value: autoRoundDisplay,
        cmd: `<span class="config-cmd">${prefix}autoround <span class="cmd-param">&lt;minutes|-1|off&gt;</span></span>`
      },
      {
        label: "Idle Message",
        value: idleMessageDisplay,
        cmd: `<span class="config-cmd">${prefix}idlemessage <span class="cmd-param">&lt;on|off&gt;</span></span>`
      },
      {
        label: "Bouncy Walls",
        value: bouncyDisplay,
        cmd: `<span class="config-cmd">${prefix}bouncywalls <span class="cmd-param">&lt;on|off&gt;</span></span>`
      },
      {
        label: "Terrain Bounds",
        value: `<span class="config-val">${stateRef?.terrainMin ?? 20}% - ${stateRef?.terrainMax ?? 75}%</span>`,
        cmd: `<span class="config-cmd">${prefix}terrain <span class="cmd-param">&lt;min%&gt; &lt;max%&gt;</span></span>`
      },
      {
        label: "Start Game Perm",
        value: `<span class="config-val badge-on">${stateRef?.startPerm ?? "broadcaster"}</span>`,
        cmd: `<span class="config-cmd">${prefix}startperm <span class="cmd-param">&lt;role&gt;</span></span>`
      },
      {
        label: "Config Perm",
        value: `<span class="config-val badge-on">${stateRef?.configPerm ?? "broadcaster"}</span>`,
        cmd: `<span class="config-cmd">${prefix}configperm <span class="cmd-param">&lt;role&gt;</span></span>`
      },
      {
        label: "Min Players",
        value: `<span class="config-val">${stateRef?.minPlayers ?? 5}</span>`,
        cmd: `<span class="config-cmd">${prefix}minplayers <span class="cmd-param">&lt;2-20&gt;</span></span>`
      },
      {
        label: "Bot Fill",
        value: stateRef?.botFill ?? true ? '<span class="config-val badge-on">ON</span>' : '<span class="config-val badge-off">OFF</span>',
        cmd: `<span class="config-cmd">${prefix}botfill <span class="cmd-param">&lt;on|off&gt;</span></span>`
      },
      {
        label: "Bot Points",
        value: `<span class="config-val">${stateRef?.botPoints ?? 1}</span>`,
        cmd: `<span class="config-cmd">${prefix}botpoints <span class="cmd-param">&lt;0-10&gt;</span></span>`
      },
      {
        label: "Clear Leaderboard",
        value: `<span class="config-val badge-off">Wipe</span>`,
        cmd: `<span class="config-cmd">${prefix}clearleaderboard</span>`
      },
      {
        label: "Delete Player",
        value: `<span class="config-val badge-off">Remove</span>`,
        cmd: `<span class="config-cmd">${prefix}deleteplayer <span class="cmd-param">&lt;user&gt;</span></span>`
      }
    ];
    configTableBody.innerHTML = rows.map(
      (r) => `
        <tr>
            <td class="config-label">${r.label}</td>
            <td>${r.value}</td>
            <td>${r.cmd}</td>
        </tr>
    `
    ).join("");
  }
  function updateUI() {
    const prefix = stateRef?.prefix ?? "%";
    if (currentPhase === PhaseIdle) {
      const showIdle = stateRef?.idleMessage ?? true;
      if (hudTop) hudTop.style.display = showIdle ? "flex" : "none";
      if (phaseBadge) {
        phaseBadge.innerText = "WAITING FOR PLAYERS";
        phaseBadge.className = "hud-badge idle";
      }
      if (hudInstructions) {
        hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}startgame</span> to start | <span class="cmd-highlight">${prefix}join</span> to join`;
      }
      timerDisplay.style.display = "none";
      celebrationDisplay.style.display = "none";
      if (hudAvatar) {
        hudAvatar.style.display = "none";
        hudAvatar.src = "";
      }
      if (leaderboardEl) leaderboardEl.style.display = showIdle ? "block" : "none";
    } else if (currentPhase === PhaseInput) {
      if (hudTop) hudTop.style.display = "flex";
      if (phaseBadge) {
        phaseBadge.innerText = "INPUT PHASE";
        phaseBadge.className = "hud-badge input";
      }
      if (hudInstructions) {
        hudInstructions.innerHTML = `<span class="cmd-highlight">${prefix}fire &lt;angle&gt; &lt;power&gt;</span> | <span class="cmd-highlight">${prefix}left</span> | <span class="cmd-highlight">${prefix}right</span>`;
      }
      if (hudAvatar) {
        hudAvatar.style.display = "none";
        hudAvatar.src = "";
      }
      timerDisplay.style.display = "block";
      celebrationDisplay.style.display = "none";
      if (leaderboardEl) leaderboardEl.style.display = "none";
      if (previousPhase !== PhaseInput) {
        inputTimer = stateRef?.timerRemaining ?? stateRef?.inputDuration ?? 20;
        timerDisplay.innerText = inputTimer.toString();
        if (inputTimer <= 5 && inputTimer > 0) {
          timerDisplay.style.color = "#ff003c";
          timerDisplay.style.animation = "pulse 0.5s infinite alternate";
          timerDisplay.style.textShadow = "0 0 15px #ff003c";
        } else {
          timerDisplay.style.color = "#fff";
          timerDisplay.style.animation = "none";
          timerDisplay.style.textShadow = "0 0 8px #00ffcc";
        }
      } else if (stateRef?.timerRemaining !== void 0 && stateRef.timerRemaining < inputTimer) {
        inputTimer = stateRef.timerRemaining;
        timerDisplay.innerText = inputTimer.toString();
        if (inputTimer <= 5 && inputTimer > 0) {
          timerDisplay.style.color = "#ff003c";
          timerDisplay.style.animation = "pulse 0.5s infinite alternate";
          timerDisplay.style.textShadow = "0 0 15px #ff003c";
        } else {
          timerDisplay.style.color = "#fff";
          timerDisplay.style.animation = "none";
          timerDisplay.style.textShadow = "0 0 8px #00ffcc";
        }
      }
    } else if (currentPhase === PhaseAction) {
      if (phaseBadge) {
        phaseBadge.innerText = "ACTION PHASE";
        phaseBadge.className = "hud-badge action";
      }
      if (hudInstructions) {
        hudInstructions.innerHTML = "Executing commands...";
      }
      if (hudAvatar) {
        hudAvatar.style.display = "none";
        hudAvatar.src = "";
      }
      timerDisplay.style.display = "none";
      celebrationDisplay.style.display = "none";
      if (leaderboardEl) leaderboardEl.style.display = "block";
    } else if (currentPhase === PhaseCelebration) {
      if (phaseBadge) {
        phaseBadge.innerText = "GAME OVER";
        phaseBadge.className = "hud-badge celebration";
      }
      const win = stateRef?.winner !== void 0 && stateRef.winner !== "" ? stateRef.winner : celebrationWinner;
      if (win && win !== "AI") {
        if (hudInstructions) {
          hudInstructions.innerHTML = `<span class="hud-winner">${escapeHtml(win)} WINS!</span>`;
        }
        if (hudAvatar) {
          if (avatarCache[win] && avatarCache[win] !== "fetching") {
            hudAvatar.src = avatarCache[win];
            hudAvatar.style.display = "block";
          } else {
            fetch(`https://decapi.me/twitch/avatar/${win}`).then((r) => r.text()).then((url) => {
              avatarCache[win] = url;
              if (hudAvatar) {
                hudAvatar.src = url;
                hudAvatar.style.display = "block";
              }
            });
          }
        }
      } else {
        if (hudAvatar) {
          hudAvatar.style.display = "none";
          hudAvatar.src = "";
        }
        if (hudInstructions) {
          hudInstructions.innerHTML = '<span class="hud-ai-winner">Humanity failed to defeat the AI</span>';
        }
      }
      timerDisplay.style.display = "none";
      celebrationDisplay.style.display = "flex";
      if (celebrationRecap && recapList) {
        recapList.innerHTML = "";
        const kills = stateRef?.matchKills ?? [];
        if (kills.length > 0) {
          celebrationRecap.style.display = "flex";
          for (const k of kills) {
            const li = document.createElement("li");
            li.className = "recap-item";
            const victimIsBot = k.victimIsBot;
            const victimClass = victimIsBot ? "recap-name bot" : "recap-name player";
            const victimTag = victimIsBot ? '<span class="bot-tag">BOT</span>' : "";
            if (k.killer) {
              const killerIsBot = k.killerIsBot;
              const killerClass = killerIsBot ? "recap-name bot" : "recap-name player";
              const killerTag = killerIsBot ? '<span class="bot-tag">BOT</span>' : "";
              li.innerHTML = `<span class="${killerClass}">${escapeHtml(k.killer)}${killerTag}</span><span class="recap-action">\u{1F4A5} destroyed</span><span class="${victimClass}">${escapeHtml(k.victim)}${victimTag}</span>`;
            } else {
              li.innerHTML = `<span class="${victimClass}">${escapeHtml(k.victim)}${victimTag}</span><span class="recap-action abyss">fell into the abyss</span>`;
            }
            recapList.appendChild(li);
          }
        } else {
          celebrationRecap.style.display = "none";
        }
      }
    }
    if (debugBar) {
      debugBar.style.display = stateRef?.debug ? "flex" : "none";
    }
    if (debugInput) {
      debugInput.placeholder = `Type command (${prefix}startgame, ${prefix}fire 45 60, ${prefix}left, etc.)...`;
    }
    renderConfigModal(prefix);
    previousPhase = currentPhase;
  }
  setInterval(() => {
    if (currentPhase === "INPUT" && inputTimer > 0) {
      inputTimer--;
      if (inputTimer <= 5 && inputTimer > 0) {
        timerDisplay.innerText = inputTimer.toString();
        timerDisplay.style.color = "#ff003c";
        timerDisplay.style.animation = "pulse 0.5s infinite alternate";
        timerDisplay.style.textShadow = "0 0 15px #ff003c";
      } else if (inputTimer > 5) {
        timerDisplay.innerText = inputTimer.toString();
        timerDisplay.style.color = "#ffffff";
        timerDisplay.style.animation = "none";
        timerDisplay.style.textShadow = "0 0 8px #00ffcc";
      } else {
        timerDisplay.innerText = "FIRING!";
        timerDisplay.style.color = "#ffaa00";
        timerDisplay.style.animation = "none";
        timerDisplay.style.textShadow = "0 0 10px #ffaa00";
      }
    }
  }, 1e3);
  net.onMessage((msg) => {
    if (msg.type === MsgStateUpdate) {
      const state = msg.payload;
      stateRef = state;
      if (state.phase === PhaseAction && currentPhase !== PhaseAction) {
        executeActions();
      }
      if (state.phase === PhaseCelebration && currentPhase !== PhaseCelebration) {
        celebrationStartTime = performance.now();
      }
      if (state.phase === PhaseIdle && currentPhase !== PhaseIdle) {
        celebrationWinner = "";
        if (hudAvatar) {
          hudAvatar.style.display = "none";
          hudAvatar.src = "";
        }
        if (recapList) recapList.innerHTML = "";
        if (celebrationRecap) celebrationRecap.style.display = "none";
      }
      currentPhase = state.phase;
      if (state.leaderboard) {
        updateLeaderboard(state.leaderboard);
      }
      if (Array.isArray(state.terrain) && state.terrain.length === WIDTH) {
        terrain = state.terrain;
      }
      const newPlayers = state.players;
      for (const name in newPlayers) {
        if (!players[name]) {
          let spawnX = typeof newPlayers[name].x === "number" && newPlayers[name].x > 0 ? newPlayers[name].x : Math.random() * (WIDTH - 100) + 50;
          let spawnY = typeof newPlayers[name].y === "number" ? newPlayers[name].y : getTerrainHeight(terrain, spawnX);
          let moveDx = (Math.random() > 0.5 ? 1 : -1) * 1.5;
          players[name] = {
            ...newPlayers[name],
            x: spawnX,
            y: spawnY,
            dx: moveDx
          };
          const emoteUrl = players[name].emoteUrl || `https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0`;
          if (!emoteCache[emoteUrl]) {
            const img = new Image();
            img.src = emoteUrl;
            emoteCache[emoteUrl] = img;
          }
        } else {
          players[name].x = newPlayers[name].x;
          players[name].y = newPlayers[name].y;
          players[name].moving = newPlayers[name].moving;
          players[name].moveTarget = newPlayers[name].moveTarget;
          players[name].speedMultiplier = newPlayers[name].speedMultiplier;
          players[name].hasBounced = newPlayers[name].hasBounced;
          players[name].isBot = newPlayers[name].isBot;
          players[name].lastAngle = newPlayers[name].lastAngle;
          players[name].lastPower = newPlayers[name].lastPower;
          players[name].fired = newPlayers[name].fired;
          players[name].angle = newPlayers[name].angle;
          players[name].power = newPlayers[name].power;
          players[name].emote = newPlayers[name].emote;
          players[name].emoteUrl = newPlayers[name].emoteUrl;
          players[name].isDead = newPlayers[name].isDead;
          players[name].actionType = newPlayers[name].actionType;
        }
        const url = newPlayers[name].emoteUrl;
        if (url) {
          let imgEl = document.getElementById("emote-" + name);
          if (!imgEl) {
            imgEl = document.createElement("img");
            imgEl.id = "emote-" + name;
            imgEl.className = "tank-emote";
            emotesLayer.appendChild(imgEl);
          }
          if (imgEl.src !== url) {
            imgEl.src = url;
          }
        }
      }
      for (const name in players) {
        if (!newPlayers[name]) {
          const imgEl = document.getElementById("emote-" + name);
          if (imgEl) imgEl.remove();
          delete players[name];
        }
      }
      updateUI();
    } else if (msg.type === MsgExecuteActions) {
      executeActions();
    } else if (msg.type === MsgResetTerrain) {
      if (stateRef && Array.isArray(stateRef.terrain) && stateRef.terrain.length === WIDTH) {
        terrain = stateRef.terrain;
      } else {
        terrain = createDefaultTerrain();
      }
      projectiles = [];
      explosions.length = 0;
      appliedCraterIds.clear();
      celebrationStartTime = 0;
      for (const name in players) {
        if (stateRef && stateRef.players && stateRef.players[name] && typeof stateRef.players[name].x === "number") {
          players[name].x = stateRef.players[name].x;
        } else {
          players[name].x = Math.random() * (WIDTH - 100) + 50;
        }
        players[name].y = -50;
      }
    } else if (msg.type === MsgPlayerDied) {
      const payload = msg.payload;
      if (payload.killer) {
        showKillMessage(`${payload.killer} destroyed ${payload.victim}!`);
      } else {
        showKillMessage(`${payload.victim} fell into the abyss!`);
      }
      if (players[payload.victim]) {
        players[payload.victim].isDead = true;
        const imgEl = document.getElementById("emote-" + payload.victim);
        if (imgEl) imgEl.style.display = "none";
      }
    } else if (msg.type === MsgTerrainCrater) {
      const crater = msg.payload;
      if (crater && typeof crater.x === "number") {
        if (crater.id && appliedCraterIds.has(crater.id)) {
          return;
        }
        if (crater.id) {
          appliedCraterIds.add(crater.id);
        }
        applyCrater(terrain, crater.x, crater.y, crater.radius);
        const hasExplosion = explosions.some(
          (e) => Math.hypot(e.x - crater.x, e.y - crater.y) < crater.radius
        );
        if (!hasExplosion) {
          explosions.push({ x: crater.x, y: crater.y, radius: 0, maxRadius: crater.radius, alpha: 1 });
        }
      }
    }
  });
  function draw() {
    ctx.clearRect(0, 0, WIDTH, HEIGHT);
    drawTerrain(ctx, terrain);
    if (currentPhase === "INPUT") {
      drawGiantProtractor(ctx);
    }
    drawTanks(ctx, players, terrain, currentPhase, emotesLayer, emoteCache);
    drawProjectiles(ctx, projectiles, emoteCache);
    drawExplosions(ctx, explosions);
  }
  function gameLoop(time) {
    const rawDt = time - lastTime;
    lastTime = time;
    const dtClamped = Math.min(Math.max(rawDt, 0), 100);
    const baseDtScale = dtClamped / (1e3 / 60);
    const speedMultiplier = stateRef && typeof stateRef.physicsSpeed === "number" ? stateRef.physicsSpeed : 0.5;
    const dtScale = baseDtScale * speedMultiplier;
    updatePhysics(dtScale);
    draw();
    requestAnimationFrame(gameLoop);
  }
  requestAnimationFrame(gameLoop);
  function sendDebugCommand() {
    if (!debugInput) return;
    const cmd = debugInput.value.trim();
    if (!cmd) return;
    net.send({ type: MsgChatCommand, payload: cmd });
    debugInput.value = "";
  }
  if (debugSendBtn) {
    debugSendBtn.addEventListener("click", sendDebugCommand);
  }
  if (debugInput) {
    debugInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        sendDebugCommand();
      }
    });
  }
})();
//# sourceMappingURL=game.js.map
