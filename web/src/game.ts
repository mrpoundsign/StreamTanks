import {
  WIDTH,
  HEIGHT,
  Player,
  GameState,
  Projectile,
  TrailParticle,
  Explosion,
  GamePhase,
  WSMessage,
  PhaseIdle,
  PhaseInput,
  PhaseAction,
  PhaseCelebration,
  MsgStateUpdate,
  MsgExecuteActions,
  MsgResetTerrain,
  MsgPlayerDied,
  PlayerDiedPayload,
  MsgChatCommand,
  MsgTerrainCrater,
  CraterPayload,
  ActionFire,
  ActionLeft,
  ActionRight,
  ActionShield,
} from './types';
import { createDefaultTerrain, getTerrainHeight, applyCrater } from './terrain';
import { NetworkManager } from './network';
import {
  drawTerrain,
  drawGiantProtractor,
  drawTanks,
  drawTrails,
  drawProjectiles,
  drawExplosions,
} from './renderer';

// DOM Elements
const canvas = document.getElementById('gameCanvas') as HTMLCanvasElement;
const ctx = canvas.getContext('2d')!;
const phaseBadge = document.getElementById('phase-badge') as HTMLElement;
const hudInstructions = document.getElementById('hud-instructions') as HTMLElement;
const hudTop = document.getElementById('hud-top') as HTMLElement;
const leaderboardEl = document.getElementById('leaderboard') as HTMLElement;
const leaderboardList = document.getElementById('leaderboard-list') as HTMLElement;
const leaderboardTicker = document.getElementById('leaderboard-ticker') as HTMLElement | null;
const tickerTrack = document.getElementById('ticker-track') as HTMLElement | null;
const timerDisplay = document.getElementById('timer-display') as HTMLElement;
const killFeed = document.getElementById('kill-feed') as HTMLElement;
const emotesLayer = document.getElementById('emotes-layer') as HTMLElement;
const celebrationDisplay = document.getElementById('celebration-display') as HTMLElement;
const hudAvatar = document.getElementById('hud-avatar') as HTMLImageElement | null;
const celebrationRecap = document.getElementById('celebration-recap') as HTMLElement | null;
const recapList = document.getElementById('recap-list') as HTMLElement | null;
const configModal = document.getElementById('config-modal') as HTMLElement;
const configTableBody = document.getElementById('config-table-body') as HTMLElement;
const configDismissHint = document.getElementById('config-dismiss-hint') as HTMLElement;
const debugBar = document.getElementById('debug-bar') as HTMLElement;
const debugInput = document.getElementById('debug-input') as HTMLInputElement;
const debugSendBtn = document.getElementById('debug-send-btn') as HTMLElement;

// Game State
let terrain: number[] = createDefaultTerrain();
const players: Record<string, Player> = {};
let projectiles: Projectile[] = [];
let trailParticles: TrailParticle[] = [];
const explosions: Explosion[] = [];
let currentPhase: GamePhase = PhaseIdle;
let previousPhase: GamePhase = PhaseIdle;

// Live Preview State
let lastProtractorX = 250;
let lastProtractorY = 350;
let protractorPreviewUntil = 0;
let inputTimer = 0;
let celebrationWinner = '';
let celebrationStartTime = 0;
let lastTime = performance.now();
let stateRef: GameState | null = null;
const appliedCraterIds = new Set<string>();

const avatarCache: Record<string, string> = {};
const avatarImgCache: Record<string, HTMLImageElement> = {};
const emoteCache: Record<string, HTMLImageElement> = {};

function preloadEmote(url: string): void {
  if (!url || emoteCache[url]) return;
  const img = new Image();
  img.src = url;
  emoteCache[url] = img;
}

function preloadPlayerAvatar(name: string): void {
  if (avatarImgCache[name] || name.startsWith('_bot_')) return;
  if (!avatarCache[name]) {
    avatarCache[name] = 'fetching';
    fetch(`https://decapi.me/twitch/avatar/${encodeURIComponent(name)}`)
      .then((r) => r.text())
      .then((url) => {
        if (url && url.startsWith('http')) {
          avatarCache[name] = url;
          const img = new Image();
          img.crossOrigin = 'anonymous';
          img.onload = () => {
            avatarImgCache[name] = img;
          };
          img.src = url;
        }
      })
      .catch(() => {});
  } else if (avatarCache[name] !== 'fetching' && avatarCache[name].startsWith('http')) {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => {
      avatarImgCache[name] = img;
    };
    img.src = avatarCache[name];
    avatarImgCache[name] = img;
  }
}

// Network
const net = new NetworkManager();

function escapeHtml(str: string): string {
  const div = document.createElement('div');
  div.innerText = str;
  return div.innerHTML;
}

function showKillMessage(html: string): void {
  const el = document.createElement('div');
  el.className = 'kill-message';
  el.innerHTML = html;
  killFeed.appendChild(el);
  setTimeout(() => {
    if (killFeed.contains(el)) {
      killFeed.removeChild(el);
    }
  }, 5000);
}

function createWallSpark(cx: number, cy: number): void {
  explosions.push({ x: cx, y: cy, radius: 0, maxRadius: 30, alpha: 1, isSpark: true });
}

function checkTankCollisions(cx: number, cy: number, radius: number, owner: string): void {
  for (const name in players) {
    if (name === owner) continue; // No self-damage
    const p = players[name];
    if (p.isDead || p.isShielded) continue;
    const dist = Math.hypot(p.x - cx, p.y - cy);
    if (dist < radius + 20) {
      p.isDead = true;
      const imgEl = document.getElementById('emote-' + name);
      if (imgEl) imgEl.style.display = 'none';
    }
  }
}

function destroyTerrain(cx: number, cy: number, radius: number, shotId?: string): void {
  if (shotId) {
    if (appliedCraterIds.has(shotId)) {
      return;
    }
    appliedCraterIds.add(shotId);
  }
  applyCrater(terrain, cx, cy, radius);
  explosions.push({ x: cx, y: cy, radius: 0, maxRadius: radius, alpha: 1 });
}

function executeActions(): void {
  projectiles = [];
  trailParticles = [];
  for (const name in players) {
    const p = players[name];
    if (p.isDead) continue;

    if (p.actionType === ActionFire) {
      const rad = ((p.angle ?? 45) * Math.PI) / 180;
      const powerClamped = Math.min(Math.max(p.power ?? 50, 1), 100);
      const powerScaled = powerClamped / 5;
      const vx = Math.cos(rad) * powerScaled;
      const vy = -Math.sin(rad) * powerScaled;
      const shotId = `${stateRef?.roundId ?? 0}_${name}`;

      const muzzleDist = 25;
      const spawnX = p.x + Math.cos(rad) * muzzleDist;
      const spawnY = p.y - 10 - Math.sin(rad) * muzzleDist;

      if (p.emoteUrl) {
        preloadEmote(p.emoteUrl);
      }

      projectiles.push({
        id: shotId,
        x: spawnX,
        y: spawnY,
        vx,
        vy,
        owner: name,
        emoteUrl: p.emoteUrl,
      });
    } else if (p.actionType === ActionLeft) {
      p.moveTarget = p.x - (stateRef?.moveDistance ?? 100);
      p.moving = true;
      p.speedMultiplier = 1.0;
      p.hasBounced = false;
    } else if (p.actionType === ActionRight) {
      p.moveTarget = p.x + (stateRef?.moveDistance ?? 100);
      p.moving = true;
      p.speedMultiplier = 1.0;
      p.hasBounced = false;
    } else if (p.actionType === ActionShield) {
      // Hunkers down defensively with active shield
      p.moving = false;
    }
  }
}

function updatePhysics(dtScale: number): void {
  const bouncyWalls = !!stateRef?.bouncyWalls;

  for (const name in players) {
    const p = players[name];
    if (p.isDead) continue;

    // Execute Action Movement
    if (currentPhase === PhaseAction && p.moving) {
      const currentSpeed = (p.speedMultiplier ?? 1.0) * 2.0 * dtScale;
      if (p.actionType === ActionLeft) {
        p.x -= currentSpeed;
        if (p.x <= 20) {
          if (bouncyWalls && !p.hasBounced) {
            p.x = 20;
            p.actionType = ActionRight;
            p.moveTarget = p.x + (stateRef?.moveDistance ?? 100);
            p.speedMultiplier = 1.5; // +50% speed boost
            p.hasBounced = true;
            createWallSpark(20, p.y);
          } else if ((p.moveTarget !== undefined && p.x <= p.moveTarget) || p.x <= 20) {
            p.moving = false;
            if (p.x < 20) p.x = 20;
          }
        } else if (p.moveTarget !== undefined && p.x <= p.moveTarget) {
          p.moving = false;
        }
      } else if (p.actionType === ActionRight) {
        p.x += currentSpeed;
        if (p.x >= WIDTH - 20) {
          if (bouncyWalls && !p.hasBounced) {
            p.x = WIDTH - 20;
            p.actionType = ActionLeft;
            p.moveTarget = p.x - (stateRef?.moveDistance ?? 100);
            p.speedMultiplier = 1.5; // +50% speed boost
            p.hasBounced = true;
            createWallSpark(WIDTH - 20, p.y);
          } else if ((p.moveTarget !== undefined && p.x >= p.moveTarget) || p.x >= WIDTH - 20) {
            p.moving = false;
            if (p.x > WIDTH - 20) p.x = WIDTH - 20;
          }
        } else if (p.moveTarget !== undefined && p.x >= p.moveTarget) {
          p.moving = false;
        }
      }
    }

    // Roaming in IDLE
    if (currentPhase === PhaseIdle) {
      p.x += (p.dx ?? 1.5) * dtScale;
      if (p.x < 50) {
        p.x = 50;
        p.dx = Math.abs(p.dx ?? 1.5);
      } else if (p.x > WIDTH - 50) {
        p.x = WIDTH - 50;
        p.dx = -Math.abs(p.dx ?? 1.5);
      }
    }

    // Boundary clamping
    if (p.x < 20) p.x = 20;
    if (p.x > WIDTH - 20) p.x = WIDTH - 20;

    // Falling / Ground snapping
    const floorY = getTerrainHeight(terrain, p.x);
    if (p.y < floorY) {
      p.y += 5.0 * dtScale;
      if (p.y > floorY) p.y = floorY;
    } else {
      p.y = floorY;
    }

    // Fall off bottom of screen
    if (p.y >= HEIGHT) {
      p.isDead = true;
      const imgEl = document.getElementById('emote-' + name);
      if (imgEl) imgEl.style.display = 'none';
    }
  }

  // Projectile logic
  for (let i = projectiles.length - 1; i >= 0; i--) {
    const proj = projectiles[i];
    proj.x += proj.vx * dtScale;
    proj.vy += 0.2 * dtScale; // Matching server gravity 0.2
    proj.y += proj.vy * dtScale;

    // Record trail history for smooth contrail ribbon
    proj.trail = proj.trail || [];
    proj.trail.push({ x: proj.x, y: proj.y });
    if (proj.trail.length > 20) {
      proj.trail.shift();
    }

    // Spawn thruster exhaust smoke and sparks from rear nozzle
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
        color: isSmoke ? 'rgba(0, 255, 204, ALPHA)' : 'rgba(255, 120, 50, ALPHA)',
      });
    }

    let hit = false;

    if (proj.y < 0) {
      if (bouncyWalls) {
        proj.y = 0;
        proj.vy = Math.abs(proj.vy) * 1.1; // +10% speed boost downward
        proj.vx *= 1.1;
        proj.bounces = (proj.bounces ?? 0) + 1;
        createWallSpark(Math.max(0, Math.min(WIDTH, proj.x)), 0);
        if (proj.bounces > 15) hit = true;
      }
    } else if (proj.y > HEIGHT) {
      if (bouncyWalls) {
        proj.y = HEIGHT;
        proj.vy = -Math.abs(proj.vy) * 1.1; // +10% speed boost upward
        proj.vx *= 1.1;
        proj.bounces = (proj.bounces ?? 0) + 1;
        createWallSpark(Math.max(0, Math.min(WIDTH, proj.x)), HEIGHT);
        if (proj.bounces > 15) hit = true;
      } else {
        hit = true;
      }
    }

    // Side walls bounce
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

    // Active shield collision: completely absorbs projectile before terrain impact
    if (!hit) {
      for (const name in players) {
        if (name === proj.owner) continue;
        const p = players[name];
        if (p.isDead || !p.isShielded) continue;
        if (Math.hypot(p.x - proj.x, p.y - proj.y) < 45 && proj.y <= p.y + 5) {
          hit = true;
          createWallSpark(proj.x, proj.y);
          break;
        }
      }
    }

    // Terrain collision
    if (!hit && proj.y >= 0 && proj.y >= getTerrainHeight(terrain, proj.x)) {
      hit = true;
      if (currentPhase === PhaseCelebration) {
        explosions.push({ x: proj.x, y: proj.y, radius: 0, maxRadius: 50, alpha: 1 });
      } else {
        destroyTerrain(proj.x, proj.y, 50, proj.id);
        checkTankCollisions(proj.x, proj.y, 50, proj.owner);
      }
    }

    // Direct tank collision
    if (!hit) {
      for (const name in players) {
        if (name === proj.owner) continue;
        const p = players[name];
        if (p.isDead || p.isShielded) continue;
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

  // Update explosions
  for (let i = explosions.length - 1; i >= 0; i--) {
    const exp = explosions[i];
    exp.radius += 2.0 * dtScale;
    exp.alpha -= 0.05 * dtScale;
    if (exp.alpha <= 0) {
      explosions.splice(i, 1);
    }
  }

  // Update trail particles
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

  // Celebration random emote bombs
  if (currentPhase === PhaseCelebration) {
    const elapsed = celebrationStartTime > 0 ? performance.now() - celebrationStartTime : 0;
    if (celebrationWinner && celebrationWinner !== 'AI' && elapsed < 3500 && Math.random() < 0.2) {
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
        emoteUrl: p?.emoteUrl ?? '',
      });
    }
  }
}

function updateLeaderboard(lb: Record<string, number>): void {
  if (!leaderboardList) return;
  const sorted = Object.entries(lb)
    .sort((a, b) => b[1] - a[1]);

  const top3 = sorted.slice(0, 3);
  const runnersUp = sorted.slice(3, 8);

  leaderboardList.innerHTML = top3
    .map(
      ([name, wins]) => {
        const hasUrl = avatarCache[name] && avatarCache[name] !== 'fetching';
        const avatarUrl = hasUrl ? avatarCache[name] : '';
        return `
        <li>
            <div class="lb-player">
                <img id="lb-avatar-${name}" class="lb-avatar" src="${avatarUrl}" style="${hasUrl ? '' : 'display:none;'}">
                <span class="lb-name" title="${escapeHtml(name)}">${escapeHtml(name)}</span>
            </div>
            <span class="lb-score">${wins}</span>
        </li>
    `;
      }
    )
    .join('');

  for (const [name] of top3) {
    if (!avatarCache[name]) {
      avatarCache[name] = 'fetching';
      fetch(`https://decapi.me/twitch/avatar/${name}`)
        .then((r) => r.text())
        .then((url) => {
          avatarCache[name] = url;
          const img = document.getElementById(`lb-avatar-${name}`) as HTMLImageElement | null;
          if (img) {
            img.src = url;
            img.style.display = 'inline-block';
          }
        });
    }
  }

  // Handle Ranks 4-8 scrolling ticker
  if (leaderboardTicker && tickerTrack) {
    if (runnersUp.length > 0) {
      leaderboardTicker.style.display = 'block';
      const itemsHtml = runnersUp
        .map(([name, wins], idx) => {
          const rank = idx + 4;
          return `<span class="ticker-item"><span class="ticker-rank">#${rank}</span> <span class="ticker-name">${escapeHtml(name)}</span> <span class="ticker-score">(${wins})</span></span>`;
        })
        .join('<span class="ticker-sep">•</span>');

      // Duplicate content to achieve a seamless, continuous -50% marquee loop
      tickerTrack.innerHTML = itemsHtml + '<span class="ticker-sep">•</span>' + itemsHtml;
    } else {
      leaderboardTicker.style.display = 'none';
      tickerTrack.innerHTML = '';
    }
  }
}

function renderConfigModal(prefix: string): void {
  if (!configModal || !configTableBody) return;

  const isVisible = !!stateRef?.showConfig;
  if (!isVisible) {
    configModal.style.display = 'none';
    return;
  }

  configModal.style.display = 'flex';

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
  const idleMessageDisplay = idleMessageVal
    ? `<span class="config-val badge-on">On</span>`
    : `<span class="config-val badge-off">Off</span>`;

  const bouncyVal = !!stateRef?.bouncyWalls;
  const bouncyDisplay = bouncyVal
    ? `<span class="config-val badge-on">On (+10% bullet, +50% tank)</span>`
    : `<span class="config-val badge-off">Off</span>`;

  const rows = [
    {
      label: 'Command Prefix',
      value: `<span class="config-val">"${prefix}"</span>`,
      cmd: `<span class="config-cmd">${prefix}prefix <span class="cmd-param">&lt;str&gt;</span></span>`,
    },
    {
      label: 'Physics / Speed',
      value: `<span class="config-val">${speedVal}x</span>`,
      cmd: `<span class="config-cmd">${prefix}speed <span class="cmd-param">&lt;0.1 - 3.0&gt;</span></span>`,
    },
    {
      label: 'Command Time',
      value: `<span class="config-val">${roundDuration}s</span>`,
      cmd: `<span class="config-cmd">${prefix}commandtime <span class="cmd-param">&lt;seconds&gt;</span></span>`,
    },
    {
      label: 'Auto Round',
      value: autoRoundDisplay,
      cmd: `<span class="config-cmd">${prefix}autoround <span class="cmd-param">&lt;minutes|-1|off&gt;</span></span>`,
    },
    {
      label: 'Idle Message',
      value: idleMessageDisplay,
      cmd: `<span class="config-cmd">${prefix}idlemessage <span class="cmd-param">&lt;on|off&gt;</span></span>`,
    },
    {
      label: 'Bouncy Walls',
      value: bouncyDisplay,
      cmd: `<span class="config-cmd">${prefix}bouncywalls <span class="cmd-param">&lt;on|off&gt;</span></span>`,
    },
    {
      label: 'Terrain Bounds',
      value: `<span class="config-val">${stateRef?.terrainMin ?? 20}% - ${stateRef?.terrainMax ?? 75}%</span>`,
      cmd: `<span class="config-cmd">${prefix}terrain <span class="cmd-param">&lt;min%&gt; &lt;max%&gt;</span></span>`,
    },
    {
      label: 'Terrain Color',
      value: `<span class="config-val" style="color: ${stateRef?.terrainColor ?? '#ff003c'}">${stateRef?.terrainColor ?? '#ff003c'}</span>`,
      cmd: `<span class="config-cmd">${prefix}terraincolor <span class="cmd-param">&lt;hex|preset&gt;</span></span>`,
    },
    {
      label: 'Start Game Perm',
      value: `<span class="config-val badge-on">${stateRef?.startPerm ?? 'broadcaster'}</span>`,
      cmd: `<span class="config-cmd">${prefix}startperm <span class="cmd-param">&lt;role&gt;</span></span>`,
    },
    {
      label: 'Config Perm',
      value: `<span class="config-val badge-on">${stateRef?.configPerm ?? 'broadcaster'}</span>`,
      cmd: `<span class="config-cmd">${prefix}configperm <span class="cmd-param">&lt;role&gt;</span></span>`,
    },
    {
      label: 'Min Players',
      value: `<span class="config-val">${stateRef?.minPlayers ?? 5}</span>`,
      cmd: `<span class="config-cmd">${prefix}minplayers <span class="cmd-param">&lt;2-20&gt;</span></span>`,
    },
    {
      label: 'Bot Fill',
      value: (stateRef?.botFill ?? true)
        ? '<span class="config-val badge-on">ON</span>'
        : '<span class="config-val badge-off">OFF</span>',
      cmd: `<span class="config-cmd">${prefix}botfill <span class="cmd-param">&lt;on|off&gt;</span></span>`,
    },
    {
      label: 'Bot Points',
      value: `<span class="config-val">${stateRef?.botPoints ?? 1}</span>`,
      cmd: `<span class="config-cmd">${prefix}botpoints <span class="cmd-param">&lt;0-10&gt;</span></span>`,
    },
    {
      label: 'Clear Leaderboard',
      value: `<span class="config-val badge-off">Wipe</span>`,
      cmd: `<span class="config-cmd">${prefix}clearleaderboard</span>`,
    },
    {
      label: 'Delete Player',
      value: `<span class="config-val badge-off">Remove</span>`,
      cmd: `<span class="config-cmd">${prefix}deleteplayer <span class="cmd-param">&lt;user&gt;</span></span>`,
    },
  ];

  configTableBody.innerHTML = rows
    .map(
      (r) => `
        <tr>
            <td class="config-label">${r.label}</td>
            <td>${r.value}</td>
            <td>${r.cmd}</td>
        </tr>
    `
    )
    .join('');
}

function updateUI(): void {
  const prefix = stateRef?.prefix ?? '%';

  if (currentPhase === PhaseIdle) {
    const showIdle = stateRef?.idleMessage ?? true;
    if (hudTop) hudTop.style.display = showIdle ? 'flex' : 'none';
    if (phaseBadge) {
      phaseBadge.innerText = 'WAITING FOR PLAYERS';
      phaseBadge.className = 'hud-badge idle';
    }
    const canStartGame = Object.values(stateRef?.players || {}).some(p => !p.isBot && p.joined);
    if (hudInstructions) {
      if (canStartGame) {
        hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}startgame</span> to start | <span class="cmd-highlight">${prefix}join</span> to join`;
      } else {
        hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}join</span> to join`;
      }
    }
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'none';
    if (hudAvatar) {
      hudAvatar.style.display = 'none';
      hudAvatar.src = '';
    }
    if (leaderboardEl) leaderboardEl.style.display = showIdle ? 'block' : 'none';
  } else if (currentPhase === PhaseInput) {
    if (hudTop) hudTop.style.display = 'flex';
    if (phaseBadge) {
      phaseBadge.innerText = 'INPUT PHASE';
      phaseBadge.className = 'hud-badge input';
    }
    const joinedHumans = Object.values(stateRef?.players || {}).filter(p => !p.isBot && p.joined);
    const isHumanDead = joinedHumans.length > 0 && joinedHumans.every(p => p.isDead);
    if (hudInstructions) {
      if (isHumanDead) {
        hudInstructions.innerHTML = `<span class="cmd-highlight" style="color: #ff003c; border-color: #ff003c; background: rgba(255, 0, 60, 0.15);">💀 ELIMINATED</span>`;
      } else {
        hudInstructions.innerHTML = `<span class="cmd-highlight">${prefix}fire &lt;angle&gt; &lt;power&gt;</span> | <span class="cmd-highlight">${prefix}left</span> | <span class="cmd-highlight">${prefix}right</span>`;
      }
    }
    if (hudAvatar) {
      hudAvatar.style.display = 'none';
      hudAvatar.src = '';
    }
    timerDisplay.style.display = 'block';
    celebrationDisplay.style.display = 'none';
    if (leaderboardEl) leaderboardEl.style.display = 'none';
    if (previousPhase !== PhaseInput) {
      inputTimer = stateRef?.timerRemaining ?? stateRef?.inputDuration ?? 20;
      timerDisplay.innerText = inputTimer.toString();
      if (inputTimer <= 5 && inputTimer > 0) {
        timerDisplay.style.color = '#ff003c';
        timerDisplay.style.animation = 'pulse 0.5s infinite alternate';
        timerDisplay.style.textShadow = '0 0 15px #ff003c';
      } else {
        timerDisplay.style.color = '#fff';
        timerDisplay.style.animation = 'none';
        timerDisplay.style.textShadow = '0 0 8px #00ffcc';
      }
    } else if (stateRef?.timerRemaining !== undefined && stateRef.timerRemaining < inputTimer) {
      inputTimer = stateRef.timerRemaining;
      timerDisplay.innerText = inputTimer.toString();
      if (inputTimer <= 5 && inputTimer > 0) {
        timerDisplay.style.color = '#ff003c';
        timerDisplay.style.animation = 'pulse 0.5s infinite alternate';
        timerDisplay.style.textShadow = '0 0 15px #ff003c';
      } else {
        timerDisplay.style.color = '#fff';
        timerDisplay.style.animation = 'none';
        timerDisplay.style.textShadow = '0 0 8px #00ffcc';
      }
    }
  } else if (currentPhase === PhaseAction) {
    if (phaseBadge) {
      phaseBadge.innerText = 'ACTION PHASE';
      phaseBadge.className = 'hud-badge action';
    }
    if (hudInstructions) {
      hudInstructions.innerHTML = 'Executing commands...';
    }
    if (hudAvatar) {
      hudAvatar.style.display = 'none';
      hudAvatar.src = '';
    }
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'none';
    if (leaderboardEl) leaderboardEl.style.display = 'block';
  } else if (currentPhase === PhaseCelebration) {
    if (phaseBadge) {
      phaseBadge.innerText = 'GAME OVER';
      phaseBadge.className = 'hud-badge celebration';
    }
    const win = (stateRef?.winner !== undefined && stateRef.winner !== '') ? stateRef.winner : celebrationWinner;

    if (win && win !== 'AI') {
      if (hudInstructions) {
        hudInstructions.innerHTML = `<span class="hud-winner">${escapeHtml(win)} WINS!</span>`;
      }
      if (hudAvatar) {
        if (avatarCache[win] && avatarCache[win] !== 'fetching') {
          hudAvatar.src = avatarCache[win];
          hudAvatar.style.display = 'block';
        } else {
          fetch(`https://decapi.me/twitch/avatar/${win}`)
            .then((r) => r.text())
            .then((url) => {
              avatarCache[win] = url;
              if (hudAvatar) {
                hudAvatar.src = url;
                hudAvatar.style.display = 'block';
              }
            });
        }
      }
    } else {
      if (hudAvatar) {
        hudAvatar.style.display = 'none';
        hudAvatar.src = '';
      }
      if (hudInstructions) {
        hudInstructions.innerHTML = '<span class="hud-ai-winner">Humanity failed to defeat the AI</span>';
      }
    }
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'flex';

    if (celebrationRecap && recapList) {
      recapList.innerHTML = '';
      const kills = stateRef?.matchKills ?? [];
      if (kills.length > 0) {
        celebrationRecap.style.display = 'flex';
        const isMultiCol = kills.length > 6;
        celebrationRecap.classList.toggle('multi-col', isMultiCol);
        recapList.classList.toggle('multi-col', isMultiCol);
        for (const k of kills) {
          const li = document.createElement('li');
          li.className = 'recap-item';

          const victimIsBot = k.victimIsBot;
          const victimClass = victimIsBot ? 'recap-name bot' : 'recap-name player';
          const victimTag = victimIsBot ? '<span class="bot-tag">BOT</span>' : '';
          const victimLoss = (k.pointsLost && k.pointsLost > 0) ? ` <span class="pts-removed">(-${k.pointsLost})</span>` : '';

          if (k.killer) {
            const killerIsBot = k.killerIsBot;
            const killerClass = killerIsBot ? 'recap-name bot' : 'recap-name player';
            const killerTag = killerIsBot ? '<span class="bot-tag">BOT</span>' : '';
            const killerPts = (k.pointsAwarded && k.pointsAwarded > 0) ? ` <span class="pts-added">(+${k.pointsAwarded})</span>` : '';

            li.innerHTML = `<span class="${killerClass}">${escapeHtml(k.killer)}${killerTag}${killerPts}</span><span class="recap-action">💥 destroyed</span><span class="${victimClass}">${escapeHtml(k.victim)}${victimTag}${victimLoss}</span>`;
          } else {
            li.innerHTML = `<span class="${victimClass}">${escapeHtml(k.victim)}${victimTag}${victimLoss}</span><span class="recap-action abyss">fell into the abyss</span>`;
          }
          recapList.appendChild(li);
        }
      } else {
        celebrationRecap.style.display = 'none';
      }
    }
  }

  if (debugBar) {
    debugBar.style.display = stateRef?.debug ? 'flex' : 'none';
  }
  if (debugInput) {
    debugInput.placeholder = `Type command (${prefix}startgame, ${prefix}fire 45 60, ${prefix}left, etc.)...`;
  }

  renderConfigModal(prefix);
  previousPhase = currentPhase;
}

// Timer Interval
setInterval(() => {
  if (currentPhase === 'INPUT' && inputTimer > 0) {
    inputTimer--;
    if (inputTimer <= 5 && inputTimer > 0) {
      timerDisplay.innerText = inputTimer.toString();
      timerDisplay.style.color = '#ff003c';
      timerDisplay.style.animation = 'pulse 0.5s infinite alternate';
      timerDisplay.style.textShadow = '0 0 15px #ff003c';
    } else if (inputTimer > 5) {
      timerDisplay.innerText = inputTimer.toString();
      timerDisplay.style.color = '#ffffff';
      timerDisplay.style.animation = 'none';
      timerDisplay.style.textShadow = '0 0 8px #00ffcc';
    } else {
      timerDisplay.innerText = 'FIRING!';
      timerDisplay.style.color = '#ffaa00';
      timerDisplay.style.animation = 'none';
      timerDisplay.style.textShadow = '0 0 10px #ffaa00';
    }
  }
}, 1000);

// Network Dispatcher
net.onMessage((msg: WSMessage) => {
  if (msg.type === MsgStateUpdate) {
    const state = msg.payload as GameState;
    stateRef = state;
    if (state.phase === PhaseAction && currentPhase !== PhaseAction) {
      executeActions();
    }
    if (state.phase === PhaseCelebration && currentPhase !== PhaseCelebration) {
      celebrationStartTime = performance.now();
    }
    if (state.phase === PhaseIdle && currentPhase !== PhaseIdle) {
      celebrationWinner = '';
      if (hudAvatar) {
        hudAvatar.style.display = 'none';
        hudAvatar.src = '';
      }
      if (recapList) recapList.innerHTML = '';
      if (celebrationRecap) celebrationRecap.style.display = 'none';
    }
    const phaseChangedFromIdle = currentPhase === PhaseIdle && state.phase !== PhaseIdle;
    currentPhase = state.phase;

    if (state.leaderboard) {
      updateLeaderboard(state.leaderboard);
    }
    
    // Position leaderboard relative to Protractor (Base Offset: -210px X, -310px Y)
    if (leaderboardEl) {
      const px = state.protractorX ?? 250;
      const py = state.protractorY ?? 350;
      
      // Trigger Live Preview of the Protractor if it moved
      if (px !== lastProtractorX || py !== lastProtractorY) {
        lastProtractorX = px;
        lastProtractorY = py;
        protractorPreviewUntil = Date.now() + 2000;
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
        let spawnX = typeof newPlayers[name].x === 'number' && newPlayers[name].x > 0 ? newPlayers[name].x : Math.random() * (WIDTH - 100) + 50;
        let spawnY = typeof newPlayers[name].y === 'number' ? newPlayers[name].y : getTerrainHeight(terrain, spawnX);
        let moveDx = (Math.random() > 0.5 ? 1 : -1) * 1.5;
        players[name] = {
          ...newPlayers[name],
          x: spawnX,
          y: spawnY,
          dx: moveDx,
          joined: newPlayers[name].joined,
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
        let imgEl = document.getElementById('emote-' + name) as HTMLImageElement | null;
        if (!imgEl) {
          imgEl = document.createElement('img');
          imgEl.id = 'emote-' + name;
          imgEl.className = 'tank-emote';
          emotesLayer.appendChild(imgEl);
        }
        if (imgEl.src !== url) {
          imgEl.src = url;
        }
      }
    }

    // Remove disconnected players
    for (const name in players) {
      if (!newPlayers[name]) {
        const imgEl = document.getElementById('emote-' + name);
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
    trailParticles = [];
    explosions.length = 0;
    appliedCraterIds.clear();
    celebrationStartTime = 0;
    for (const name in players) {
      if (stateRef && stateRef.players && stateRef.players[name] && typeof stateRef.players[name].x === 'number') {
        players[name].x = stateRef.players[name].x;
      } else {
        players[name].x = Math.random() * (WIDTH - 100) + 50;
      }
      players[name].y = -50;
    }
  } else if (msg.type === MsgPlayerDied) {
    const payload = msg.payload as PlayerDiedPayload;
    const victimLoss = payload.pointsLost ?? 0;
    const victimLossTag = victimLoss > 0 ? ` <span class="pts-removed">(-${victimLoss})</span>` : '';
    if (payload.killer) {
      const killerGain = payload.pointsAwarded ?? payload.bountyAwarded ?? 0;
      const killerGainTag = killerGain > 0 ? ` <span class="pts-added">(+${killerGain})</span>` : '';
      showKillMessage(`${escapeHtml(payload.killer)}${killerGainTag} destroyed ${escapeHtml(payload.victim)}${victimLossTag}!`);
    } else {
      showKillMessage(`${escapeHtml(payload.victim)}${victimLossTag} fell into the abyss!`);
    }
    if (players[payload.victim]) {
      players[payload.victim].isDead = true;
      const imgEl = document.getElementById('emote-' + payload.victim);
      if (imgEl) imgEl.style.display = 'none';
    }
  } else if (msg.type === MsgTerrainCrater) {
    const crater = msg.payload as CraterPayload;
    if (crater && typeof crater.x === 'number') {
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

// Render Loop
function draw(): void {
  ctx.clearRect(0, 0, WIDTH, HEIGHT);
  drawTerrain(ctx, terrain, stateRef?.terrainColor ?? '#ff003c');
  if (currentPhase === 'INPUT' || Date.now() < protractorPreviewUntil) {
    // If we are previewing in IDLE, make it slightly translucent
    const isPreview = currentPhase !== 'INPUT';
    if (isPreview) ctx.globalAlpha = 0.5;
    const joinedHumans = Object.values(players).filter((p) => !p.isBot && p.joined);
    const isHumanDead = joinedHumans.length > 0 && joinedHumans.every((p) => p.isDead);
    drawGiantProtractor(ctx, stateRef?.protractorX ?? 250, stateRef?.protractorY ?? 350, isHumanDead);
    if (isPreview) ctx.globalAlpha = 1.0;
  }
  drawTrails(ctx, projectiles, trailParticles);
  drawTanks(ctx, players, terrain, currentPhase, emotesLayer, emoteCache, avatarImgCache);
  drawProjectiles(ctx, projectiles, emoteCache);
  drawExplosions(ctx, explosions);
}

function gameLoop(time: number): void {
  const rawDt = time - lastTime;
  lastTime = time;

  const dtClamped = Math.min(Math.max(rawDt, 0), 100);
  const baseDtScale = dtClamped / (1000 / 60);
  const speedMultiplier = stateRef && typeof stateRef.physicsSpeed === 'number' ? stateRef.physicsSpeed : 0.5;
  const dtScale = baseDtScale * speedMultiplier;

  updatePhysics(dtScale);
  draw();

  requestAnimationFrame(gameLoop);
}

// Start
requestAnimationFrame(gameLoop);

// Debug Command Bar Handler
function sendDebugCommand(): void {
  if (!debugInput) return;
  const cmd = debugInput.value.trim();
  if (!cmd) return;
  net.send({ type: MsgChatCommand, payload: cmd });
  debugInput.value = '';
}

if (debugSendBtn) {
  debugSendBtn.addEventListener('click', sendDebugCommand);
}
if (debugInput) {
  debugInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      sendDebugCommand();
    }
  });
}
