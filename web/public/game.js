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
  var ActionShield = "SHIELD";

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
        terrain2[x] = Math.round(circleBottomY * 10) / 10;
      }
    }
  }

  // web/src/simulation.ts
  function executeActions(state) {
    state.projectiles = [];
    const roundId = state.roundId ?? 0;
    const playerNames = Object.keys(state.players).sort();
    for (const name of playerNames) {
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
  function checkTankCollisions(players2, cx, cy, radius, owner) {
    const kills = [];
    const playerNames = Object.keys(players2).sort();
    for (const name of playerNames) {
      if (name === owner) continue;
      const p = players2[name];
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
    const playerNames = Object.keys(state.players).sort();
    for (const name of playerNames) {
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
        for (const name of playerNames) {
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
        for (const name of playerNames) {
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
  var rocketImg = new Image();
  rocketImg.src = "/rocket.svg";
  function hexToRgba(hex, alpha) {
    let c = hex.replace(/^#/, "");
    if (c.length === 3) {
      c = c.split("").map((x) => x + x).join("");
    }
    const num = parseInt(c, 16);
    if (isNaN(num) || c.length !== 6) {
      return `rgba(255, 0, 60, ${alpha})`;
    }
    const r = num >> 16 & 255;
    const g = num >> 8 & 255;
    const b = num & 255;
    return `rgba(${r}, ${g}, ${b}, ${alpha})`;
  }
  function drawTerrain(ctx2, terrain2, color = "#ff003c") {
    if (terrain2.length === 0) return;
    const strokeColor = color || "#ff003c";
    const fillColor = hexToRgba(strokeColor, 0.02);
    ctx2.beginPath();
    ctx2.moveTo(0, terrain2[0]);
    for (let x = 1; x < WIDTH; x++) {
      ctx2.lineTo(x, terrain2[x]);
    }
    ctx2.strokeStyle = strokeColor;
    ctx2.lineWidth = 4;
    ctx2.shadowBlur = 15;
    ctx2.shadowColor = strokeColor;
    ctx2.stroke();
    ctx2.lineTo(WIDTH, HEIGHT);
    ctx2.lineTo(0, HEIGHT);
    ctx2.closePath();
    ctx2.fillStyle = fillColor;
    ctx2.fill();
    ctx2.shadowBlur = 0;
  }
  function drawGiantProtractor(ctx2, centerX, centerY, isDead = false) {
    ctx2.save();
    ctx2.translate(centerX, centerY);
    const arcColor = isDead ? "rgba(255, 0, 60, 0.6)" : "rgba(0, 255, 204, 0.5)";
    const glowColor = isDead ? "#ff003c" : "#00ffcc";
    ctx2.strokeStyle = arcColor;
    ctx2.lineWidth = 10;
    ctx2.shadowBlur = 20;
    ctx2.shadowColor = glowColor;
    ctx2.beginPath();
    ctx2.arc(0, 0, 150, Math.PI, 0);
    ctx2.stroke();
    ctx2.fillStyle = glowColor;
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
    if (isDead) {
      ctx2.font = "72px sans-serif";
      ctx2.textAlign = "center";
      ctx2.textBaseline = "middle";
      ctx2.shadowBlur = 30;
      ctx2.shadowColor = "#ff003c";
      ctx2.fillText("\u{1F480}", 0, -25);
    } else {
      ctx2.fillStyle = "#ff003c";
      ctx2.shadowColor = "#ff003c";
      ctx2.beginPath();
      ctx2.arc(0, 0, 8, 0, Math.PI * 2);
      ctx2.fill();
    }
    ctx2.restore();
  }
  function drawTanks(ctx2, players2, terrain2, currentPhase2, emotesLayer2, emoteCache2, avatarImgCache2, tankColor = "#ff003c") {
    const treadColor = tankColor || "#ff003c";
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
      ctx2.strokeStyle = treadColor;
      ctx2.lineWidth = 2;
      ctx2.shadowBlur = 10;
      ctx2.shadowColor = treadColor;
      ctx2.strokeRect(-15, -10, 30, 10);
      ctx2.restore();
      if (imgEl) {
        imgEl.style.display = "block";
        imgEl.style.left = p.x - 14 + "px";
        imgEl.style.top = p.y - 35 + "px";
        imgEl.style.transformOrigin = "14px 35px";
        imgEl.style.transform = `rotate(${angle}rad)`;
      }
      const isHuman = !p.isBot && !name.startsWith("_bot_");
      const isJoinedInIdle = isHuman && currentPhase2 === "IDLE" && !!p.joined;
      const showCommandMarker = isHuman && currentPhase2 === "INPUT" || isJoinedInIdle;
      const statusColor = isJoinedInIdle ? "#00ffcc" : p.fired ? "#00ffcc" : "#ff003c";
      const displayName = p.name && !p.name.startsWith("_bot_") ? p.name : !name.startsWith("_bot_") && !p.isBot ? name : "";
      if (displayName) {
        if (showCommandMarker) {
          const markerX = p.x;
          const markerY = p.y - 142;
          const radius = 32;
          const avatarImg = avatarImgCache2 && avatarImgCache2[name] || p.emoteUrl && emoteCache2[p.emoteUrl] || null;
          ctx2.save();
          ctx2.beginPath();
          ctx2.arc(markerX, markerY, radius, 0, Math.PI * 2);
          ctx2.closePath();
          ctx2.clip();
          if (avatarImg && avatarImg.complete && avatarImg.naturalWidth > 0) {
            ctx2.drawImage(avatarImg, markerX - radius, markerY - radius, radius * 2, radius * 2);
          } else {
            ctx2.fillStyle = "#12161e";
            ctx2.fillRect(markerX - radius, markerY - radius, radius * 2, radius * 2);
            ctx2.fillStyle = statusColor;
            ctx2.font = "bold 22px Orbitron";
            ctx2.textAlign = "center";
            ctx2.textBaseline = "middle";
            ctx2.fillText(displayName.charAt(0).toUpperCase(), markerX, markerY);
          }
          ctx2.restore();
          ctx2.beginPath();
          ctx2.arc(markerX, markerY, radius + 2.5, 0, Math.PI * 2);
          ctx2.strokeStyle = statusColor;
          ctx2.lineWidth = 4;
          ctx2.shadowBlur = 15;
          ctx2.shadowColor = statusColor;
          ctx2.stroke();
          ctx2.beginPath();
          ctx2.moveTo(markerX - 8, markerY + radius + 4);
          ctx2.lineTo(markerX, markerY + radius + 14);
          ctx2.lineTo(markerX + 8, markerY + radius + 4);
          ctx2.strokeStyle = statusColor;
          ctx2.lineWidth = 4;
          ctx2.shadowBlur = 10;
          ctx2.shadowColor = statusColor;
          ctx2.stroke();
          ctx2.shadowBlur = 0;
          const nameY = p.y - 75;
          ctx2.font = "bold 28px Orbitron";
          ctx2.textAlign = "center";
          ctx2.textBaseline = "alphabetic";
          ctx2.strokeStyle = "#000000";
          ctx2.lineWidth = 6;
          ctx2.strokeText(displayName, markerX, nameY);
          ctx2.fillStyle = "#ffffff";
          ctx2.shadowBlur = 10;
          ctx2.shadowColor = statusColor;
          ctx2.fillText(displayName, markerX, nameY);
          ctx2.shadowBlur = 0;
        } else {
          ctx2.fillStyle = "#fff";
          ctx2.font = "16px Orbitron";
          ctx2.textAlign = "center";
          ctx2.textBaseline = "alphabetic";
          ctx2.shadowBlur = 5;
          ctx2.shadowColor = "#000";
          ctx2.fillText(displayName, p.x, p.y + 20);
          ctx2.shadowBlur = 0;
        }
      }
      if (currentPhase2 === "INPUT") {
        ctx2.strokeStyle = p.fired ? "#00ffcc" : "#ff003c";
        ctx2.shadowColor = ctx2.strokeStyle;
        ctx2.lineWidth = 2;
        const angles = [0, 45, 90, 135, 180];
        for (const deg of angles) {
          const rad = deg * Math.PI / 180;
          const innerR = 45;
          const outerR = 50;
          ctx2.beginPath();
          ctx2.moveTo(p.x + Math.cos(rad) * innerR, p.y - 10 - Math.sin(rad) * innerR);
          ctx2.lineTo(p.x + Math.cos(rad) * outerR, p.y - 10 - Math.sin(rad) * outerR);
          ctx2.stroke();
        }
        const aimAngle = (p.lastAngle ?? 45) * Math.PI / 180;
        ctx2.beginPath();
        ctx2.moveTo(p.x, p.y - 10);
        ctx2.lineTo(p.x + Math.cos(aimAngle) * 50, p.y - 10 - Math.sin(aimAngle) * 50);
        ctx2.stroke();
      }
      if (currentPhase2 !== "INPUT" && p.isShielded && !p.isDead) {
        ctx2.save();
        ctx2.translate(p.x, p.y);
        const terrainAngle = getTerrainSlopeAngle(terrain2, p.x);
        ctx2.rotate(terrainAngle);
        const shieldRadius = 45;
        ctx2.beginPath();
        ctx2.arc(0, 0, shieldRadius, Math.PI, 0);
        ctx2.strokeStyle = "#00e5ff";
        ctx2.lineWidth = 3;
        ctx2.shadowBlur = 18;
        ctx2.shadowColor = "#00e5ff";
        ctx2.stroke();
        const grad = ctx2.createRadialGradient(
          0,
          0,
          5,
          0,
          0,
          shieldRadius
        );
        grad.addColorStop(0, "rgba(0, 229, 255, 0.05)");
        grad.addColorStop(0.7, "rgba(0, 229, 255, 0.2)");
        grad.addColorStop(1, "rgba(0, 229, 255, 0.4)");
        ctx2.fillStyle = grad;
        ctx2.beginPath();
        ctx2.arc(0, 0, shieldRadius, Math.PI, 0);
        ctx2.closePath();
        ctx2.fill();
        ctx2.beginPath();
        ctx2.arc(0, 0, shieldRadius - 6, Math.PI, 0);
        ctx2.strokeStyle = "rgba(255, 255, 255, 0.6)";
        ctx2.lineWidth = 1.5;
        ctx2.shadowBlur = 8;
        ctx2.shadowColor = "#ffffff";
        ctx2.stroke();
        ctx2.restore();
      }
    }
  }
  function drawTrails(ctx2, projectiles2, trailParticles2) {
    for (const proj of projectiles2) {
      if (!proj.trail || proj.trail.length < 2) continue;
      const len = proj.trail.length;
      for (let i = 0; i < len - 1; i++) {
        const p1 = proj.trail[i];
        const p2 = proj.trail[i + 1];
        const progress = (i + 1) / len;
        ctx2.beginPath();
        ctx2.moveTo(p1.x, p1.y);
        ctx2.lineTo(p2.x, p2.y);
        ctx2.strokeStyle = `rgba(0, 255, 204, ${progress * 0.55})`;
        ctx2.lineWidth = 1.5 + progress * 2.5;
        ctx2.shadowBlur = progress * 8;
        ctx2.shadowColor = "#00ffcc";
        ctx2.stroke();
      }
    }
    for (const p of trailParticles2) {
      ctx2.save();
      ctx2.beginPath();
      ctx2.arc(p.x, p.y, Math.max(0.5, p.radius), 0, Math.PI * 2);
      ctx2.fillStyle = p.color.replace("ALPHA", Math.max(0, p.alpha).toFixed(3));
      ctx2.shadowBlur = Math.min(10, p.alpha * 10);
      ctx2.shadowColor = p.color.includes("255, 120") ? "#ff7832" : "#00ffcc";
      ctx2.fill();
      ctx2.restore();
    }
  }
  function drawProjectiles(ctx2, projectiles2, emoteCache2) {
    for (const proj of projectiles2) {
      if (proj.emoteUrl && !emoteCache2[proj.emoteUrl]) {
        const img2 = new Image();
        img2.src = proj.emoteUrl;
        emoteCache2[proj.emoteUrl] = img2;
      }
      const angle = Math.atan2(proj.vy, proj.vx);
      ctx2.save();
      ctx2.translate(proj.x, proj.y);
      ctx2.rotate(angle);
      const rocketW = 84;
      const rocketH = 40;
      const rocketX = -36;
      const rocketY = -20;
      const nozzleX = rocketX + 2;
      const flameLen = 12 + Math.random() * 8;
      const flameW = 4 + Math.random() * 1.5;
      const flameGrad = ctx2.createLinearGradient(nozzleX, 0, nozzleX - flameLen, 0);
      flameGrad.addColorStop(0, "#ffffff");
      flameGrad.addColorStop(0.2, "#00ffcc");
      flameGrad.addColorStop(0.6, "#ff007f");
      flameGrad.addColorStop(1, "rgba(255, 0, 127, 0)");
      ctx2.beginPath();
      ctx2.moveTo(nozzleX, -flameW);
      ctx2.lineTo(nozzleX - flameLen, 0);
      ctx2.lineTo(nozzleX, flameW);
      ctx2.closePath();
      ctx2.fillStyle = flameGrad;
      ctx2.shadowBlur = 10;
      ctx2.shadowColor = "#00ffcc";
      ctx2.fill();
      if (rocketImg.complete && rocketImg.naturalWidth > 0) {
        ctx2.shadowBlur = 4;
        ctx2.shadowColor = "#00ffcc";
        ctx2.drawImage(rocketImg, rocketX, rocketY, rocketW, rocketH);
      } else {
        ctx2.fillStyle = "#0f172a";
        ctx2.strokeStyle = "#00ffcc";
        ctx2.lineWidth = 0.75;
        ctx2.shadowBlur = 4;
        ctx2.shadowColor = "#00ffcc";
        ctx2.beginPath();
        ctx2.moveTo(-32, -8);
        ctx2.lineTo(20, -8);
        ctx2.lineTo(44, 0);
        ctx2.lineTo(20, 8);
        ctx2.lineTo(-32, 8);
        ctx2.closePath();
        ctx2.fill();
        ctx2.stroke();
      }
      const stickerRadius = 14.5;
      const img = proj.emoteUrl ? emoteCache2[proj.emoteUrl] : null;
      if (img && img.complete && img.naturalWidth > 0) {
        ctx2.save();
        ctx2.beginPath();
        ctx2.arc(0, 0, stickerRadius, 0, Math.PI * 2);
        ctx2.clip();
        ctx2.drawImage(img, -stickerRadius, -stickerRadius, stickerRadius * 2, stickerRadius * 2);
        ctx2.restore();
        ctx2.beginPath();
        ctx2.arc(0, 0, stickerRadius, 0, Math.PI * 2);
        ctx2.strokeStyle = "#00ffcc";
        ctx2.lineWidth = 0.75;
        ctx2.shadowBlur = 4;
        ctx2.shadowColor = "#00ffcc";
        ctx2.stroke();
      } else {
        ctx2.beginPath();
        ctx2.arc(0, 0, 8, 0, Math.PI * 2);
        ctx2.fillStyle = "#00ffcc";
        ctx2.shadowBlur = 8;
        ctx2.shadowColor = "#00ffcc";
        ctx2.fill();
      }
      ctx2.restore();
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
  var trailParticles = [];
  var explosions = [];
  var currentPhase = PhaseIdle;
  var previousPhase = PhaseIdle;
  var lastProtractorX = 250;
  var lastProtractorY = 350;
  var protractorPreviewUntil = 0;
  var inputTimer = 0;
  var celebrationWinner = "";
  var celebrationStartTime = 0;
  var lastTime = performance.now();
  var stateRef = null;
  var appliedCraterIds = /* @__PURE__ */ new Set();
  var avatarCache = {};
  var avatarImgCache = {};
  var emoteCache = {};
  function preloadEmote(url) {
    if (!url || emoteCache[url]) return;
    const img = new Image();
    img.src = url;
    emoteCache[url] = img;
  }
  function preloadPlayerAvatar(name) {
    if (avatarImgCache[name] || name.startsWith("_bot_")) return;
    if (!avatarCache[name]) {
      avatarCache[name] = "fetching";
      fetch(`https://decapi.me/twitch/avatar/${encodeURIComponent(name)}`).then((r) => r.text()).then((url) => {
        if (url && url.startsWith("http")) {
          avatarCache[name] = url;
          const img = new Image();
          img.crossOrigin = "anonymous";
          img.onload = () => {
            avatarImgCache[name] = img;
          };
          img.src = url;
        }
      }).catch(() => {
      });
    } else if (avatarCache[name] !== "fetching" && avatarCache[name].startsWith("http")) {
      const img = new Image();
      img.crossOrigin = "anonymous";
      img.onload = () => {
        avatarImgCache[name] = img;
      };
      img.src = avatarCache[name];
      avatarImgCache[name] = img;
    }
  }
  var net = new NetworkManager();
  function escapeHtml(str) {
    const div = document.createElement("div");
    div.innerText = str;
    return div.innerHTML;
  }
  function showKillMessage(html) {
    const el = document.createElement("div");
    el.className = "kill-message";
    el.innerHTML = html;
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
  var lastExecutedRoundId = -1;
  function executeActions2() {
    const currentRoundId = stateRef?.roundId ?? 0;
    if (lastExecutedRoundId === currentRoundId && currentPhase === PhaseAction) {
      return;
    }
    lastExecutedRoundId = currentRoundId;
    const simState = {
      players,
      projectiles,
      terrain,
      bouncyWalls: !!stateRef?.bouncyWalls,
      terrainClimb: !!stateRef?.terrainClimb,
      moveDistance: stateRef?.moveDistance ?? 100,
      physicsSpeed: stateRef?.physicsSpeed ?? 0.5,
      roundId: stateRef?.roundId ?? 0
    };
    executeActions(simState);
    projectiles = simState.projectiles;
    trailParticles = [];
    for (const name in players) {
      const p = players[name];
      if (p.emoteUrl) {
        preloadEmote(p.emoteUrl);
      }
    }
  }
  function updatePhysics(dtScale) {
    const terrainClimb = !!stateRef?.terrainClimb;
    if (currentPhase === PhaseIdle) {
      for (const name in players) {
        const p = players[name];
        if (p.isDead) continue;
        const roamDx = (p.dx ?? 1.5) * dtScale;
        const nextX = p.x + roamDx;
        const currIdx = Math.floor(p.x);
        const nextIdx = Math.floor(nextX);
        let blocked = false;
        if (!terrainClimb && currIdx !== nextIdx) {
          const stepX = Math.abs(currIdx - nextIdx);
          const rise = getTerrainHeight(terrain, currIdx) - getTerrainHeight(terrain, nextIdx);
          if (rise > 0 && rise / stepX > 4) {
            blocked = true;
          }
        }
        if (blocked) {
          p.dx = roamDx > 0 ? -Math.abs(p.dx ?? 1.5) : Math.abs(p.dx ?? 1.5);
        } else {
          p.x = nextX;
          if (p.x < 50) {
            p.x = 50;
            p.dx = Math.abs(p.dx ?? 1.5);
          } else if (p.x > WIDTH - 50) {
            p.x = WIDTH - 50;
            p.dx = -Math.abs(p.dx ?? 1.5);
          }
        }
        p.y = getTerrainHeight(terrain, p.x);
      }
    }
    if (currentPhase !== PhaseCelebration) {
      const simState = {
        players,
        projectiles,
        terrain,
        bouncyWalls: !!stateRef?.bouncyWalls,
        terrainClimb: !!stateRef?.terrainClimb,
        moveDistance: stateRef?.moveDistance ?? 100,
        physicsSpeed: stateRef?.physicsSpeed ?? 0.5,
        roundId: stateRef?.roundId ?? 0
      };
      const events = stepSimulation(simState, dtScale);
      for (const spark of events.wallSparks) {
        createWallSpark(spark.x, spark.y);
      }
      for (const impact of events.impacts) {
        if (impact.id) {
          appliedCraterIds.add(impact.id);
        }
        explosions.push({
          x: impact.x,
          y: impact.y,
          radius: 0,
          maxRadius: impact.radius,
          alpha: 1
        });
      }
      for (const kill of events.kills) {
        const imgEl = document.getElementById("emote-" + kill.victim);
        if (imgEl) imgEl.style.display = "none";
      }
      for (const proj of projectiles) {
        const heading = Math.atan2(proj.vy, proj.vx);
        const nozzleDist = 34;
        const nozzleX = proj.x - Math.cos(heading) * nozzleDist;
        const nozzleY = proj.y - Math.sin(heading) * nozzleDist;
        for (let k = 0; k < 2; k++) {
          const spreadAngle = heading + Math.PI + (Math.random() - 0.5) * 0.6;
          const speed = 0.5 + Math.random() * 1.5;
          const isSmoke = Math.random() > 0.45;
          trailParticles.push({
            x: nozzleX + (Math.random() - 0.5) * 3,
            y: nozzleY + (Math.random() - 0.5) * 3,
            vx: Math.cos(spreadAngle) * speed,
            vy: Math.sin(spreadAngle) * speed,
            radius: isSmoke ? 2.5 : 1.2,
            maxRadius: isSmoke ? 8 + Math.random() * 4 : 2.2,
            alpha: 0.75,
            decay: isSmoke ? 0.035 : 0.065,
            color: isSmoke ? "rgba(0, 255, 204, ALPHA)" : "rgba(255, 120, 50, ALPHA)"
          });
        }
      }
    } else {
      for (let i = projectiles.length - 1; i >= 0; i--) {
        const proj = projectiles[i];
        proj.x += proj.vx * dtScale;
        proj.vy += 0.2 * dtScale;
        proj.y += proj.vy * dtScale;
        if (proj.y >= getTerrainHeight(terrain, proj.x) || proj.y > HEIGHT) {
          explosions.push({ x: proj.x, y: proj.y, radius: 0, maxRadius: 50, alpha: 1 });
          projectiles.splice(i, 1);
        }
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
    for (let i = trailParticles.length - 1; i >= 0; i--) {
      const tp = trailParticles[i];
      tp.x += tp.vx * dtScale;
      tp.y += tp.vy * dtScale;
      tp.radius += (tp.maxRadius - tp.radius) * 0.08 * dtScale;
      tp.alpha -= tp.decay * dtScale;
      if (tp.alpha <= 0) {
        trailParticles.splice(i, 1);
      }
    }
    if (currentPhase === PhaseCelebration) {
      const elapsed = celebrationStartTime > 0 ? performance.now() - celebrationStartTime : 0;
      if (celebrationWinner && celebrationWinner !== "AI" && elapsed < 3500 && Math.random() < 0.2) {
        const p = players[celebrationWinner];
        if (p?.emoteUrl) {
          preloadEmote(p.emoteUrl);
        }
        projectiles.push({
          id: `celeb_${Math.random()}`,
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
    const terrainClimbVal = !!stateRef?.terrainClimb;
    const terrainClimbDisplay = terrainClimbVal ? `<span class="config-val badge-on">On (Steep Slopes Allowed)</span>` : `<span class="config-val badge-off">Off (Steep Slopes Blocked)</span>`;
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
        label: "Terrain Climb",
        value: terrainClimbDisplay,
        cmd: `<span class="config-cmd">${prefix}terrainclimb <span class="cmd-param">&lt;on|off&gt;</span></span>`
      },
      {
        label: "Terrain Bounds",
        value: `<span class="config-val">${stateRef?.terrainMin ?? 20}% - ${stateRef?.terrainMax ?? 75}%</span>`,
        cmd: `<span class="config-cmd">${prefix}terrain <span class="cmd-param">&lt;min%&gt; &lt;max%&gt;</span></span>`
      },
      {
        label: "Terrain Color",
        value: `<span class="config-val" style="color: ${stateRef?.terrainColor ?? "#ff003c"}">${stateRef?.terrainColor ?? "#ff003c"}</span>`,
        cmd: `<span class="config-cmd">${prefix}terraincolor <span class="cmd-param">&lt;hex|preset&gt;</span></span>`
      },
      {
        label: "Tank Color",
        value: `<span class="config-val" style="color: ${stateRef?.tankColor ?? "#ff003c"}">${stateRef?.tankColor ?? "#ff003c"}</span>`,
        cmd: `<span class="config-cmd">${prefix}tankcolor <span class="cmd-param">&lt;hex|preset&gt;</span></span>`
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
      const canStartGame = Object.values(stateRef?.players || {}).some((p) => !p.isBot && p.joined);
      if (hudInstructions) {
        if (canStartGame) {
          hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}startgame</span> to start | <span class="cmd-highlight">${prefix}join</span> to join`;
        } else {
          hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}join</span> to join`;
        }
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
      const joinedHumans = Object.values(stateRef?.players || {}).filter((p) => !p.isBot && p.joined);
      const isHumanDead = joinedHumans.length > 0 && joinedHumans.every((p) => p.isDead);
      if (hudInstructions) {
        if (isHumanDead) {
          hudInstructions.innerHTML = `<span class="cmd-highlight" style="color: #ff003c; border-color: #ff003c; background: rgba(255, 0, 60, 0.15);">\u{1F480} ELIMINATED</span>`;
        } else {
          hudInstructions.innerHTML = `<span class="cmd-highlight">${prefix}fire &lt;angle&gt; &lt;power&gt;</span> | <span class="cmd-highlight">${prefix}left</span> | <span class="cmd-highlight">${prefix}right</span>`;
        }
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
          const isMultiCol = kills.length > 6;
          celebrationRecap.classList.toggle("multi-col", isMultiCol);
          recapList.classList.toggle("multi-col", isMultiCol);
          for (const k of kills) {
            const li = document.createElement("li");
            li.className = "recap-item";
            const victimIsBot = k.victimIsBot;
            const victimClass = victimIsBot ? "recap-name bot" : "recap-name player";
            const victimTag = victimIsBot ? '<span class="bot-tag">BOT</span>' : "";
            const victimLoss = k.pointsLost && k.pointsLost > 0 ? ` <span class="pts-removed">(-${k.pointsLost})</span>` : "";
            if (k.killer) {
              const killerIsBot = k.killerIsBot;
              const killerClass = killerIsBot ? "recap-name bot" : "recap-name player";
              const killerTag = killerIsBot ? '<span class="bot-tag">BOT</span>' : "";
              const killerPts = k.pointsAwarded && k.pointsAwarded > 0 ? ` <span class="pts-added">(+${k.pointsAwarded})</span>` : "";
              li.innerHTML = `<span class="${killerClass}">${escapeHtml(k.killer)}${killerTag}${killerPts}</span><span class="recap-action">\u{1F4A5} destroyed</span><span class="${victimClass}">${escapeHtml(k.victim)}${victimTag}${victimLoss}</span>`;
            } else {
              li.innerHTML = `<span class="${victimClass}">${escapeHtml(k.victim)}${victimTag}${victimLoss}</span><span class="recap-action abyss">fell into the abyss</span>`;
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
        executeActions2();
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
      const phaseChangedFromIdle = currentPhase === PhaseIdle && state.phase !== PhaseIdle;
      currentPhase = state.phase;
      if (state.leaderboard) {
        updateLeaderboard(state.leaderboard);
      }
      if (leaderboardEl) {
        const px = state.protractorX ?? 250;
        const py = state.protractorY ?? 350;
        if (px !== lastProtractorX || py !== lastProtractorY) {
          lastProtractorX = px;
          lastProtractorY = py;
          protractorPreviewUntil = Date.now() + 2e3;
        }
        const leftX = Math.min(Math.max(10, px - 210), 1920 - 360);
        const topY = Math.min(Math.max(10, py - 310), 1080 - 200);
        leaderboardEl.style.left = `${leftX}px`;
        leaderboardEl.style.top = `${topY}px`;
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
            dx: moveDx,
            joined: newPlayers[name].joined
          };
          const emoteUrl = players[name].emoteUrl || `https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0`;
          preloadEmote(emoteUrl);
        } else {
          if (currentPhase !== PhaseIdle || phaseChangedFromIdle) {
            players[name].x = newPlayers[name].x;
            players[name].y = newPlayers[name].y;
          }
          players[name].joined = newPlayers[name].joined;
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
          players[name].shieldUsed = newPlayers[name].shieldUsed;
          players[name].isShielded = newPlayers[name].isShielded;
        }
        if (!newPlayers[name].isBot) {
          preloadPlayerAvatar(name);
        }
        const url = newPlayers[name].emoteUrl;
        if (url) {
          preloadEmote(url);
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
      executeActions2();
    } else if (msg.type === MsgResetTerrain) {
      if (stateRef && Array.isArray(stateRef.terrain) && stateRef.terrain.length === WIDTH) {
        terrain = stateRef.terrain;
      } else {
        terrain = createDefaultTerrain();
      }
      projectiles = [];
      trailParticles = [];
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
      const victimLoss = payload.pointsLost ?? 0;
      const victimLossTag = victimLoss > 0 ? ` <span class="pts-removed">(-${victimLoss})</span>` : "";
      if (payload.killer) {
        const killerGain = payload.pointsAwarded ?? payload.bountyAwarded ?? 0;
        const killerGainTag = killerGain > 0 ? ` <span class="pts-added">(+${killerGain})</span>` : "";
        showKillMessage(`${escapeHtml(payload.killer)}${killerGainTag} destroyed ${escapeHtml(payload.victim)}${victimLossTag}!`);
      } else {
        showKillMessage(`${escapeHtml(payload.victim)}${victimLossTag} fell into the abyss!`);
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
          const pIdx = projectiles.findIndex((p) => p.id === crater.id);
          if (pIdx !== -1) {
            projectiles.splice(pIdx, 1);
          }
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
    drawTerrain(ctx, terrain, stateRef?.terrainColor ?? "#ff003c");
    if (currentPhase === "INPUT" || Date.now() < protractorPreviewUntil) {
      const isPreview = currentPhase !== "INPUT";
      if (isPreview) ctx.globalAlpha = 0.5;
      const joinedHumans = Object.values(players).filter((p) => !p.isBot && p.joined);
      const isHumanDead = joinedHumans.length > 0 && joinedHumans.every((p) => p.isDead);
      drawGiantProtractor(ctx, stateRef?.protractorX ?? 250, stateRef?.protractorY ?? 350, isHumanDead);
      if (isPreview) ctx.globalAlpha = 1;
    }
    drawTrails(ctx, projectiles, trailParticles);
    drawTanks(ctx, players, terrain, currentPhase, emotesLayer, emoteCache, avatarImgCache, stateRef?.tankColor ?? "#ff003c");
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
