import {
  WIDTH,
  HEIGHT,
  EXPLOSION_RADIUS,
  GRAVITY,
  Player,
  GameState,
  Projectile,
  Explosion,
  GamePhase,
  WSMessage,
  PhaseIdle,
  PhaseInput,
  PhaseAction,
  PhaseCelebration,
  PhaseWaitingNextPhase,
  MsgStateUpdate,
  MsgExecuteActions,
  MsgResetTerrain,
  MsgActionComplete,
  MsgPlayerDied,
  MsgGameOver,
  MsgCelebrationComplete,
  MsgChatCommand,
  MsgTerrainCrater,
  CraterPayload,
  ActionFire,
  ActionLeft,
  ActionRight,
} from './types';
import { createDefaultTerrain, getTerrainHeight, applyCrater } from './terrain';
import { NetworkManager } from './network';
import {
  drawTerrain,
  drawGiantProtractor,
  drawTanks,
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
const timerDisplay = document.getElementById('timer-display') as HTMLElement;
const killFeed = document.getElementById('kill-feed') as HTMLElement;
const emotesLayer = document.getElementById('emotes-layer') as HTMLElement;
const celebrationDisplay = document.getElementById('celebration-display') as HTMLElement;
const celebrationText = document.getElementById('celebration-text') as HTMLElement;
const celebrationAvatar = document.getElementById('celebration-avatar') as HTMLImageElement;
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
const explosions: Explosion[] = [];
let currentPhase: GamePhase = PhaseIdle;
let previousPhase: GamePhase = PhaseIdle;
let inputTimer = 0;
let lastTime = performance.now();
let celebrationWinner = '';
let celebrationStartTime = 0;
let celebrationSentComplete = false;
let stateRef: GameState | null = null;

const avatarCache: Record<string, string> = {};
const emoteCache: Record<string, HTMLImageElement> = {};

// Network
const net = new NetworkManager();

function createWallSpark(cx: number, cy: number): void {
  explosions.push({ x: cx, y: cy, radius: 0, maxRadius: 30, alpha: 1, isSpark: true });
}

function showKillMessage(msg: string): void {
  const el = document.createElement('div');
  el.className = 'kill-message';
  el.innerText = msg;
  killFeed.appendChild(el);
  setTimeout(() => {
    if (killFeed.contains(el)) {
      killFeed.removeChild(el);
    }
  }, 5000);
}

function checkTankCollisions(cx: number, cy: number, radius: number, owner: string): void {
  for (const name in players) {
    if (name === owner) continue; // No self-damage
    const p = players[name];
    if (p.isDead) continue;
    const dist = Math.hypot(p.x - cx, p.y - cy);
    if (dist < radius + 20) {
      p.isDead = true;
      showKillMessage(`${owner} destroyed ${name}!`);
      net.send({ type: MsgPlayerDied, payload: name });

      const imgEl = document.getElementById('emote-' + name);
      if (imgEl) imgEl.style.display = 'none';
    }
  }
}

const appliedCraterIds = new Set<string>();

function destroyTerrain(cx: number, cy: number, radius: number, shotId?: string): void {
  if (shotId) {
    if (appliedCraterIds.has(shotId)) {
      return;
    }
    appliedCraterIds.add(shotId);
  }
  applyCrater(terrain, cx, cy, radius);
  explosions.push({ x: cx, y: cy, radius: 0, maxRadius: radius, alpha: 1 });
  net.send({
    type: MsgTerrainCrater,
    payload: { id: shotId, x: cx, y: cy, radius },
  });
}

function executeActions(): void {
  projectiles = [];
  for (const name in players) {
    const p = players[name];
    if (p.isDead) continue;

    if (p.actionType === ActionFire) {
      const rad = ((p.angle ?? 45) * Math.PI) / 180;
      const powerScaled = (p.power ?? 50) / 5;
      const vx = Math.cos(rad) * powerScaled;
      const vy = -Math.sin(rad) * powerScaled;
      const shotId = `${stateRef?.roundId ?? 0}_${name}`;

      projectiles.push({
        id: shotId,
        x: p.x,
        y: p.y - 15,
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
    }
  }
}

function updatePhysics(dtScale: number): void {
  let anyMoving = false;
  const bouncyWalls = !!stateRef?.bouncyWalls;

  for (const name in players) {
    const p = players[name];
    if (p.isDead) continue;

    // Execute Action Movement
    if (currentPhase === PhaseAction && p.moving) {
      anyMoving = true;
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
      p.x += p.dx * dtScale;
      if (p.x < 50) {
        p.x = 50;
        p.dx = Math.abs(p.dx);
      } else if (p.x > WIDTH - 50) {
        p.x = WIDTH - 50;
        p.dx = -Math.abs(p.dx);
      }
    }

    // Boundary clamping: ensure tanks never leave the screen boundaries [20, WIDTH - 20]
    if (p.x < 20) p.x = 20;
    if (p.x > WIDTH - 20) p.x = WIDTH - 20;

    // Falling / Ground snapping
    const floorY = getTerrainHeight(terrain, p.x);
    if (p.y < floorY) {
      p.y += 5.0 * dtScale; // Falling speed
      if (p.y > floorY) p.y = floorY;
    } else {
      p.y = floorY;
    }

    // Fall off bottom of screen
    if (p.y >= HEIGHT) {
      p.isDead = true;
      showKillMessage(`${name} fell into the abyss!`);
      net.send({ type: MsgPlayerDied, payload: name });
      const imgEl = document.getElementById('emote-' + name);
      if (imgEl) imgEl.style.display = 'none';
    }
  }

  // Projectile logic
  for (let i = projectiles.length - 1; i >= 0; i--) {
    const proj = projectiles[i];
    proj.x += proj.vx * dtScale;
    proj.vy += GRAVITY * dtScale;
    proj.y += proj.vy * dtScale;

    let hit = false;

    // Out of bounds (side or bottom)
    if (proj.y > HEIGHT) {
      hit = true;
    } else if (proj.x < 0) {
      if (bouncyWalls) {
        proj.x = 0;
        proj.vx = -proj.vx * 1.1; // +10% speed boost
        proj.vy *= 1.1;
        proj.bounces = (proj.bounces ?? 0) + 1;
        createWallSpark(0, proj.y);
        if (proj.bounces > 15) hit = true;
      } else {
        hit = true;
      }
    } else if (proj.x > WIDTH) {
      if (bouncyWalls) {
        proj.x = WIDTH;
        proj.vx = -proj.vx * 1.1; // +10% speed boost
        proj.vy *= 1.1;
        proj.bounces = (proj.bounces ?? 0) + 1;
        createWallSpark(WIDTH, proj.y);
        if (proj.bounces > 15) hit = true;
      } else {
        hit = true;
      }
    }
    // Terrain collision
    else if (proj.y >= getTerrainHeight(terrain, proj.x)) {
      hit = true;
      if (currentPhase === PhaseCelebration) {
        explosions.push({ x: proj.x, y: proj.y, radius: 0, maxRadius: EXPLOSION_RADIUS, alpha: 1 });
      } else {
        destroyTerrain(proj.x, proj.y, EXPLOSION_RADIUS, proj.id);
        checkTankCollisions(proj.x, proj.y, EXPLOSION_RADIUS, proj.owner);
      }
    }
    // Direct tank collision
    else {
      for (const name in players) {
        if (name === proj.owner) continue;
        const p = players[name];
        if (p.isDead) continue;
        if (Math.hypot(p.x - proj.x, p.y - proj.y) < 20) {
          hit = true;
          destroyTerrain(proj.x, proj.y, EXPLOSION_RADIUS, proj.id);
          checkTankCollisions(proj.x, proj.y, EXPLOSION_RADIUS, proj.owner);
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

  // Celebration random emote bombs
  if (currentPhase === PhaseCelebration) {
    const elapsed = celebrationStartTime > 0 ? performance.now() - celebrationStartTime : 0;
    if (celebrationWinner && elapsed < 3500 && Math.random() < 0.2) {
      const p = players[celebrationWinner];
      projectiles.push({
        x: Math.random() * WIDTH,
        y: -30,
        vx: (Math.random() - 0.5) * 5,
        vy: Math.random() * 5 + 5,
        owner: celebrationWinner,
        emoteUrl: p?.emoteUrl ?? '',
      });
    }

    // Only complete celebration after all bombs and explosions have fully settled
    if (elapsed > 4000 && projectiles.length === 0 && explosions.length === 0 && !celebrationSentComplete) {
      celebrationSentComplete = true;
      net.send({ type: MsgCelebrationComplete });
    }
  }

  // Phase transition check
  if (currentPhase === PhaseAction && projectiles.length === 0 && explosions.length === 0 && !anyMoving) {
    let anyFalling = false;
    for (const name in players) {
      if (!players[name].isDead && players[name].y < getTerrainHeight(terrain, players[name].x)) {
        anyFalling = true;
        break;
      }
    }
    if (!anyFalling) {
      currentPhase = PhaseWaitingNextPhase;
      net.send({ type: MsgActionComplete });

      // Check win condition
      let aliveCount = 0;
      let aliveName = '';
      let totalPlayers = 0;
      for (const key in players) {
        totalPlayers++;
        if (!players[key].isDead) {
          aliveCount++;
          aliveName = key;
        }
      }

      if ((aliveCount <= 1 && totalPlayers > 1) || (totalPlayers === 1 && aliveCount === 0)) {
        const winner = aliveCount === 1 ? aliveName : '';
        celebrationWinner = winner;
        net.send({ type: MsgGameOver, payload: winner });

        celebrationAvatar.style.display = 'none';

        if (winner) {
          showKillMessage(`${winner} WINS THE GAME!`);
          celebrationText.innerText = `${winner} WINS!`;

          if (avatarCache[winner] && avatarCache[winner] !== 'fetching') {
            celebrationAvatar.src = avatarCache[winner];
            celebrationAvatar.style.display = 'block';
          } else {
            fetch(`https://decapi.me/twitch/avatar/${winner}`)
              .then((r) => r.text())
              .then((url) => {
                avatarCache[winner] = url;
                celebrationAvatar.src = url;
                celebrationAvatar.style.display = 'block';
              });
          }
        } else {
          showKillMessage(`DRAW! Everyone died.`);
          celebrationText.innerText = `DRAW!`;
        }
      }
    }
  }
}

function updateLeaderboard(lb: Record<string, number>): void {
  if (!leaderboardList) return;
  const sorted = Object.entries(lb)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5);

  leaderboardList.innerHTML = sorted
    .map(
      ([name, wins]) => `
        <li>
            <div class="lb-player">
                <img id="lb-avatar-${name}" class="lb-avatar" src="${avatarCache[name] || ''}" style="${avatarCache[name] ? '' : 'display:none;'}">
                <span>${name}</span>
            </div>
            <span>${wins}</span>
        </li>
    `
    )
    .join('');

  for (const [name] of sorted) {
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
    if (hudInstructions) {
      hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}startgame</span> to start | <span class="cmd-highlight">${prefix}join</span> to join`;
    }
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'none';
    if (leaderboardEl) leaderboardEl.style.display = showIdle ? 'block' : 'none';
  } else if (currentPhase === PhaseInput) {
    if (hudTop) hudTop.style.display = 'flex';
    if (phaseBadge) {
      phaseBadge.innerText = 'INPUT PHASE';
      phaseBadge.className = 'hud-badge input';
    }
    if (hudInstructions) {
      hudInstructions.innerHTML = `<span class="cmd-highlight">${prefix}fire &lt;angle&gt; &lt;power&gt;</span> | <span class="cmd-highlight">${prefix}left</span> | <span class="cmd-highlight">${prefix}right</span>`;
    }
    timerDisplay.style.display = 'block';
    celebrationDisplay.style.display = 'none';
    if (leaderboardEl) leaderboardEl.style.display = 'none';
    if (previousPhase !== PhaseInput) {
      inputTimer = stateRef?.inputDuration ?? 20;
      timerDisplay.innerText = inputTimer.toString();
      timerDisplay.style.color = '#fff';
      timerDisplay.style.animation = 'none';
      timerDisplay.style.textShadow = '0 0 8px #00ffcc';
    }
  } else if (currentPhase === PhaseAction) {
    if (phaseBadge) {
      phaseBadge.innerText = 'ACTION PHASE';
      phaseBadge.className = 'hud-badge action';
    }
    if (hudInstructions) {
      hudInstructions.innerHTML = 'Executing commands...';
    }
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'none';
    if (leaderboardEl) leaderboardEl.style.display = 'block';
  } else if (currentPhase === PhaseCelebration) {
    if (phaseBadge) {
      phaseBadge.innerText = 'GAME OVER';
      phaseBadge.className = 'hud-badge celebration';
    }
    if (hudInstructions) {
      hudInstructions.innerHTML = celebrationWinner ? `${celebrationWinner} WINS!` : 'DRAW!';
    }
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'block';
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
        let spawnX = typeof newPlayers[name].x === 'number' && newPlayers[name].x > 0 ? newPlayers[name].x : Math.random() * (WIDTH - 100) + 50;
        let spawnY = typeof newPlayers[name].y === 'number' ? newPlayers[name].y : getTerrainHeight(terrain, spawnX);
        let moveDx = (Math.random() > 0.5 ? 1 : -1) * 1.5;
        if (stateRef && stateRef.debug) {
          spawnX = name === 'TargetBot' ? WIDTH / 2 + 100 : WIDTH / 2 - 100;
          spawnY = getTerrainHeight(terrain, spawnX);
          moveDx = 0;
        }
        players[name] = {
          ...newPlayers[name],
          x: spawnX,
          y: spawnY,
          dx: moveDx,
        };
        const emoteUrl = players[name].emoteUrl || `https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0`;
        if (!emoteCache[emoteUrl]) {
          const img = new Image();
          img.src = emoteUrl;
          emoteCache[emoteUrl] = img;
        }
      } else {
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
    explosions.length = 0;
    appliedCraterIds.clear();
    celebrationStartTime = 0;
    celebrationSentComplete = false;
    for (const name in players) {
      if (stateRef && stateRef.players && stateRef.players[name] && typeof stateRef.players[name].x === 'number') {
        players[name].x = stateRef.players[name].x;
      } else if (stateRef && stateRef.debug) {
        players[name].x = name === 'TargetBot' ? WIDTH / 2 + 100 : WIDTH / 2 - 100;
      } else {
        players[name].x = Math.random() * (WIDTH - 100) + 50;
      }
      players[name].y = -50;
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
  drawTerrain(ctx, terrain);
  if (currentPhase === 'INPUT') {
    drawGiantProtractor(ctx);
  }
  drawTanks(ctx, players, terrain, currentPhase, emotesLayer, emoteCache);
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
