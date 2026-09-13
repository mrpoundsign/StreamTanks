import {
  WIDTH,
  HEIGHT,
  Player,
  GameState,
  Explosion,
  GamePhase,
  WSMessage,
  PhaseIdle,
  PhaseInput,
  PhaseAction,
  PhaseCelebration,
  MsgStateUpdate,
  MsgResetTerrain,
  MsgPlayerDied,
  PlayerDiedPayload,
  MsgChatCommand,
  MsgTerrainCrater,
  CraterPayload,
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
const explosions: Explosion[] = [];
let currentPhase: GamePhase = PhaseIdle;
let previousPhase: GamePhase = PhaseIdle;
let inputTimer = 0;
let celebrationWinner = '';
let stateRef: GameState | null = null;
const appliedCraterIds = new Set<string>();

const avatarCache: Record<string, string> = {};
const emoteCache: Record<string, HTMLImageElement> = {};

// Network
const net = new NetworkManager();

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



function updateLeaderboard(lb: Record<string, number>): void {
  if (!leaderboardList) return;
  const sorted = Object.entries(lb)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5);

  leaderboardList.innerHTML = sorted
    .map(
      ([name, wins]) => {
        const hasUrl = avatarCache[name] && avatarCache[name] !== 'fetching';
        const avatarUrl = hasUrl ? avatarCache[name] : '';
        return `
        <li>
            <div class="lb-player">
                <img id="lb-avatar-${name}" class="lb-avatar" src="${avatarUrl}" style="${hasUrl ? '' : 'display:none;'}">
                <span>${name}</span>
            </div>
            <span>${wins}</span>
        </li>
    `;
      }
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
    timerDisplay.style.display = 'none';
    celebrationDisplay.style.display = 'none';
    if (leaderboardEl) leaderboardEl.style.display = 'block';
  } else if (currentPhase === PhaseCelebration) {
    if (phaseBadge) {
      phaseBadge.innerText = 'GAME OVER';
      phaseBadge.className = 'hud-badge celebration';
    }
    const win = (stateRef?.winner !== undefined && stateRef.winner !== '') ? stateRef.winner : celebrationWinner;
    celebrationAvatar.style.display = 'none';

    if (win && win !== 'AI') {
      celebrationText.classList.remove('bot-win');
      celebrationText.innerText = `${win} WINS!`;
      if (hudInstructions) {
        hudInstructions.innerHTML = `${win} WINS!`;
      }
      if (avatarCache[win] && avatarCache[win] !== 'fetching') {
        celebrationAvatar.src = avatarCache[win];
        celebrationAvatar.style.display = 'block';
      } else {
        fetch(`https://decapi.me/twitch/avatar/${win}`)
          .then((r) => r.text())
          .then((url) => {
            avatarCache[win] = url;
            celebrationAvatar.src = url;
            celebrationAvatar.style.display = 'block';
          });
      }
    } else {
      celebrationText.classList.add('bot-win');
      celebrationText.innerText = 'Humanity failed to defeat the AI';
      if (hudInstructions) {
        hudInstructions.innerHTML = 'Humanity failed to defeat the AI';
      }
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
    if (state.phase === PhaseIdle && currentPhase !== PhaseIdle) {
      celebrationWinner = '';
      celebrationText.innerText = '';
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
        let spawnX = typeof newPlayers[name].x === 'number' && newPlayers[name].x > 0 ? newPlayers[name].x : Math.random() * (WIDTH - 100) + 50;
        let spawnY = typeof newPlayers[name].y === 'number' ? newPlayers[name].y : getTerrainHeight(terrain, spawnX);
        let moveDx = (Math.random() > 0.5 ? 1 : -1) * 1.5;
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
  } else if (msg.type === MsgResetTerrain) {
    if (stateRef && Array.isArray(stateRef.terrain) && stateRef.terrain.length === WIDTH) {
      terrain = stateRef.terrain;
    } else {
      terrain = createDefaultTerrain();
    }
    explosions.length = 0;
    appliedCraterIds.clear();
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
    if (payload.killer) {
      showKillMessage(`${payload.killer} destroyed ${payload.victim}!`);
    } else {
      showKillMessage(`${payload.victim} fell into the abyss!`);
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
  
  const activeProjectiles = stateRef?.projectiles || [];
  const activeExplosions = stateRef?.explosions || [];
  
  drawProjectiles(ctx, activeProjectiles, emoteCache);
  drawExplosions(ctx, activeExplosions);
}

function gameLoop(): void {
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
