const canvas = document.getElementById('gameCanvas');
const ctx = canvas.getContext('2d');
const phaseBadge = document.getElementById('phase-badge');
const hudInstructions = document.getElementById('hud-instructions');
const timerDisplay = document.getElementById('timer-display');
const killFeed = document.getElementById('kill-feed');
const emotesLayer = document.getElementById('emotes-layer');
const celebrationDisplay = document.getElementById('celebration-display');
const celebrationText = document.getElementById('celebration-text');
const configModal = document.getElementById('config-modal');
const configTableBody = document.getElementById('config-table-body');
const configDismissHint = document.getElementById('config-dismiss-hint');

const WIDTH = 1920;
const HEIGHT = 1080;
const EXPLOSION_RADIUS = 60;
const GRAVITY = 0.15;

let terrain = new Array(WIDTH);
let players = {}; 
let projectiles = [];
let explosions = [];
let currentPhase = 'IDLE';
let inputTimer = 0;
let lastTime = performance.now();
let celebrationWinner = "";
let celebrationStartTime = 0;
let celebrationSentComplete = false;

const avatarCache = {};
const emoteCache = {};

// Initialize terrain
function initTerrain() {
    projectiles = [];
    explosions = [];
    celebrationStartTime = 0;
    celebrationSentComplete = false;
    // Generate large, smooth rolling hills using a random walk with momentum
    let y = HEIGHT / 2 + (Math.random() * 200 - 100);
    let slope = 0;
    terrain[0] = y;
    for (let x = 1; x < WIDTH; x++) {
        // Change slope smoothly
        slope += (Math.random() - 0.5) * 0.15;
        
        // Limit max steepness
        if (slope > 2) slope = 2;
        if (slope < -2) slope = -2;
        
        y += slope;
        
        // Softly push back towards the center if getting too close to edges
        if (y < 250) slope += 0.05;
        if (y > HEIGHT - 200) slope -= 0.05;
        
        terrain[x] = y;
    }
    
    // Parachute all players back in
    for (const name in players) {
        if (stateRef && stateRef.debug) {
            players[name].x = (name === 'TargetBot') ? (WIDTH / 2 + 100) : (WIDTH / 2 - 100);
        } else {
            players[name].x = Math.random() * (WIDTH - 100) + 50;
        }
        players[name].y = -50;
    }
}

// WebSocket & Connection Resilience Setup
let ws = null;
let stateRef = null;
let isDisconnected = false;
let reconnectInterval = null;
let healthCheckInterval = null;

function handleDisconnect() {
    if (isDisconnected) return;
    isDisconnected = true;

    console.log("Server disconnected or unreachable. Hiding overlay and waiting for server to return...");

    // Make the entire overlay go away (transparent in OBS)
    const container = document.getElementById('game-container');
    if (container) {
        container.style.display = 'none';
    }
    document.body.style.display = 'none';

    if (healthCheckInterval) {
        clearInterval(healthCheckInterval);
        healthCheckInterval = null;
    }

    // Continuously retry until server is back online, then do a full reload
    if (reconnectInterval) clearInterval(reconnectInterval);
    reconnectInterval = setInterval(async () => {
        try {
            const res = await fetch(`/?nocache=${Date.now()}`, {
                method: 'GET',
                cache: 'no-store'
            });
            if (res.ok) {
                console.log("Server is back online! Performing full page reload...");
                clearInterval(reconnectInterval);
                const url = new URL(window.location.href);
                url.searchParams.set('_t', Date.now().toString());
                window.location.replace(url.toString());
            }
        } catch (e) {
            // Still waiting for server to come back
        }
    }, 1000);
}

function safeSend(msg) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        try {
            ws.send(typeof msg === 'string' ? msg : JSON.stringify(msg));
        } catch (e) {
            handleDisconnect();
        }
    }
}

try {
    ws = new WebSocket(`ws://${window.location.host}/ws`);
    ws.onclose = handleDisconnect;
    ws.onerror = handleDisconnect;
} catch (e) {
    handleDisconnect();
}

// Periodic heartbeat to detect frozen or terminated server without TCP FIN
healthCheckInterval = setInterval(() => {
    if (isDisconnected) return;
    if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
        handleDisconnect();
        return;
    }
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 2000);
    fetch(`/?ping=${Date.now()}`, { method: 'GET', cache: 'no-store', signal: controller.signal })
        .then(res => {
            clearTimeout(timeoutId);
            if (!res.ok) handleDisconnect();
        })
        .catch(() => {
            clearTimeout(timeoutId);
            handleDisconnect();
        });
}, 3000);

// Websocket Handling
if (ws) {
ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === 'STATE_UPDATE') {
        const state = msg.payload;
        stateRef = state;
        if (state.phase === 'CELEBRATION' && currentPhase !== 'CELEBRATION') {
            celebrationStartTime = performance.now();
            celebrationSentComplete = false;
        }
        currentPhase = state.phase;
        
        if (state.leaderboard) {
            updateLeaderboard(state.leaderboard);
        }
        
        // Sync players
        const newPlayers = state.players;
        for (const name in newPlayers) {
            if (!players[name]) {
                // New player joining/roaming
                let spawnX = Math.random() * (WIDTH - 100) + 50;
                let moveDx = (Math.random() > 0.5 ? 1 : -1) * 1.5;
                if (stateRef && stateRef.debug) {
                    spawnX = (name === 'TargetBot') ? (WIDTH / 2 + 100) : (WIDTH / 2 - 100);
                    moveDx = 0;
                }
                players[name] = {
                    x: spawnX,
                    y: 0,
                    dx: moveDx,
                    ...newPlayers[name]
                };
                // Preload emote image for projectiles
                const emoteUrl = players[name].emoteUrl || `https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0`;
                if (!emoteCache[emoteUrl]) {
                    const img = new Image();
                    img.src = emoteUrl;
                    emoteCache[emoteUrl] = img;
                }
            } else {
                // Update existing player state
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
            
            // Ensure emote DOM element exists and has correct src
            const url = newPlayers[name].emoteUrl;
            if (url) {
                let imgEl = document.getElementById('emote-' + name);
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
        
        // Remove disconnected/dead players
        for (const name in players) {
            if (!newPlayers[name]) {
                const imgEl = document.getElementById('emote-' + name);
                if (imgEl) imgEl.remove();
                delete players[name];
            }
        }

        updateUI();

    } else if (msg.type === 'EXECUTE_ACTIONS') {
        executeActions();
    } else if (msg.type === 'PLAYER_LOCKED') {
        // Just play a sound or show visual feedback if desired
    } else if (msg.type === 'RESET_TERRAIN') {
        initTerrain();
    }
};
}

let previousPhase = 'IDLE';

function updateUI() {
    const prefix = (stateRef && stateRef.prefix) || '%';

    if (currentPhase === 'IDLE') {
        if (phaseBadge) {
            phaseBadge.innerText = "WAITING FOR PLAYERS";
            phaseBadge.className = "hud-badge idle";
        }
        if (hudInstructions) {
            hudInstructions.innerHTML = `Type <span class="cmd-highlight">${prefix}startgame</span> to start | <span class="cmd-highlight">${prefix}join</span> to join`;
        }
        timerDisplay.style.display = 'none';
        celebrationDisplay.style.display = 'none';
        document.getElementById('leaderboard').style.display = 'block';
    } else if (currentPhase === 'INPUT') {
        if (phaseBadge) {
            phaseBadge.innerText = "INPUT PHASE";
            phaseBadge.className = "hud-badge input";
        }
        if (hudInstructions) {
            hudInstructions.innerHTML = `<span class="cmd-highlight">${prefix}fire &lt;angle&gt; &lt;power&gt;</span> | <span class="cmd-highlight">${prefix}left</span> | <span class="cmd-highlight">${prefix}right</span>`;
        }
        timerDisplay.style.display = 'block';
        celebrationDisplay.style.display = 'none';
        document.getElementById('leaderboard').style.display = 'none'; // Hide for protractor
        if (previousPhase !== 'INPUT') {
            inputTimer = (stateRef && stateRef.inputDuration) ? stateRef.inputDuration : 20;
            timerDisplay.innerText = inputTimer.toString();
            timerDisplay.style.color = "#fff";
            timerDisplay.style.animation = "none";
            timerDisplay.style.textShadow = "0 0 8px #00ffcc";
        }
    } else if (currentPhase === 'ACTION') {
        if (phaseBadge) {
            phaseBadge.innerText = "ACTION PHASE";
            phaseBadge.className = "hud-badge action";
        }
        if (hudInstructions) {
            hudInstructions.innerHTML = "Executing commands...";
        }
        timerDisplay.style.display = 'none';
        celebrationDisplay.style.display = 'none';
        document.getElementById('leaderboard').style.display = 'block';
    } else if (currentPhase === 'CELEBRATION') {
        if (phaseBadge) {
            phaseBadge.innerText = "GAME OVER";
            phaseBadge.className = "hud-badge celebration";
        }
        if (hudInstructions) {
            hudInstructions.innerHTML = celebrationWinner ? `${celebrationWinner} WINS!` : "DRAW!";
        }
        timerDisplay.style.display = 'none';
        celebrationDisplay.style.display = 'block';
    }
    
    const debugBar = document.getElementById('debug-bar');
    if (debugBar) {
        debugBar.style.display = (stateRef && stateRef.debug) ? 'flex' : 'none';
    }

    const debugInput = document.getElementById('debug-input');
    if (debugInput) {
        debugInput.placeholder = `Type command (${prefix}startgame, ${prefix}fire 45 60, ${prefix}left, etc.)...`;
    }

    renderConfigModal(prefix);

    previousPhase = currentPhase;
}

function renderConfigModal(prefix) {
    if (!configModal || !configTableBody) return;

    const isVisible = !!(stateRef && stateRef.showConfig);
    if (!isVisible) {
        configModal.style.display = 'none';
        return;
    }

    configModal.style.display = 'flex';

    if (configDismissHint) {
        configDismissHint.innerText = `${prefix}config off`;
    }

    const speedVal = (stateRef && typeof stateRef.physicsSpeed === 'number') ? stateRef.physicsSpeed : 0.5;
    const roundDuration = (stateRef && stateRef.inputDuration) ? stateRef.inputDuration : 20;

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
            label: "Input Round Timer",
            value: `<span class="config-val">${roundDuration}s</span>`,
            cmd: `<span class="config-cmd">${prefix}roundtime <span class="cmd-param">&lt;seconds&gt;</span></span>`
        },
        {
            label: "Auto Round",
            value: `<span class="config-val badge-off">Off</span>`,
            cmd: `<span class="config-cmd">${prefix}autoround <span class="cmd-param">&lt;minutes|-1|off&gt;</span></span>`
        },
        {
            label: "Idle Message",
            value: `<span class="config-val badge-on">On</span>`,
            cmd: `<span class="config-cmd">${prefix}idlemessage <span class="cmd-param">&lt;on|off&gt;</span></span>`
        }
    ];

    configTableBody.innerHTML = rows.map(r => `
        <tr>
            <td class="config-label">${r.label}</td>
            <td>${r.value}</td>
            <td>${r.cmd}</td>
        </tr>
    `).join('');
}


function updateLeaderboard(lb) {
    const list = document.getElementById('leaderboard-list');
    const sorted = Object.entries(lb).sort((a, b) => b[1] - a[1]).slice(0, 5);
    
    list.innerHTML = sorted.map(([name, wins]) => `
        <li>
            <div class="lb-player">
                <img id="lb-avatar-${name}" class="lb-avatar" src="${avatarCache[name] || ''}" style="${avatarCache[name] ? '' : 'display:none;'}">
                <span>${name}</span>
            </div>
            <span>${wins}</span>
        </li>
    `).join('');

    // Fetch missing avatars
    for (const [name] of sorted) {
        if (!avatarCache[name]) {
            avatarCache[name] = 'fetching'; // Prevent duplicate fetches
            fetch(`https://decapi.me/twitch/avatar/${name}`)
                .then(r => r.text())
                .then(url => {
                    avatarCache[name] = url;
                    const img = document.getElementById(`lb-avatar-${name}`);
                    if (img) {
                        img.src = url;
                        img.style.display = 'block';
                    }
                })
                .catch(e => { delete avatarCache[name]; });
        }
    }
}

// Timer countdown
setInterval(() => {
    if (currentPhase === 'INPUT' && inputTimer > 0) {
        inputTimer--;
        if (inputTimer <= 5 && inputTimer > 0) {
            timerDisplay.innerText = inputTimer.toString();
            timerDisplay.style.color = "#ff003c";
            timerDisplay.style.animation = "pulse 0.5s infinite alternate";
            timerDisplay.style.textShadow = "0 0 12px #ff003c";
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
}, 1000);

// Execute actions for all players
function executeActions() {
    projectiles = [];
    for (const name in players) {
        const p = players[name];
        if (p.isDead) continue;
        
        if (p.actionType === "FIRE") {
            // Convert angle (degrees) to radians. 0 is right, 90 is up, 180 is left.
            const rad = p.angle * Math.PI / 180;
            // Scale power
            const powerScaled = p.power / 5; 
            const vx = Math.cos(rad) * powerScaled;
            const vy = -Math.sin(rad) * powerScaled; // negative because y goes down

            projectiles.push({
                x: p.x,
                y: p.y - 15,
                vx: vx,
                vy: vy,
                owner: name,
                emoteUrl: p.emoteUrl
            });
        } else if (p.actionType === "LEFT") {
            p.moveTarget = p.x - (stateRef.moveDistance || 100);
            p.moving = true;
        } else if (p.actionType === "RIGHT") {
            p.moveTarget = p.x + (stateRef.moveDistance || 100);
            p.moving = true;
        }
    }
}

function destroyTerrain(cx, cy, radius) {
    for (let x = Math.max(0, Math.floor(cx - radius)); x < Math.min(WIDTH, Math.ceil(cx + radius)); x++) {
        // Calculate the bottom half of the circle
        const dx = x - cx;
        const dy = Math.sqrt(radius * radius - dx * dx);
        const circleBottomY = cy + dy;
        
        if (terrain[x] < circleBottomY) {
            terrain[x] = circleBottomY;
        }
    }
    
    // Add explosion effect
    explosions.push({ x: cx, y: cy, radius: 0, maxRadius: radius, alpha: 1 });
}

function checkTankCollisions(cx, cy, radius, owner) {
    for (const name in players) {
        if (name === owner) continue; // No self-damage
        const p = players[name];
        if (p.isDead) continue;
        const dist = Math.hypot(p.x - cx, p.y - cy);
        if (dist < radius + 20) {
            // Tank is destroyed
            p.isDead = true;
            showKillMessage(`${owner} destroyed ${name}!`);
            safeSend({ type: 'PLAYER_DIED', payload: name });
            
            const imgEl = document.getElementById('emote-' + name);
            if (imgEl) imgEl.style.display = 'none';
        }
    }
}

function showKillMessage(msg) {
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

function updatePhysics(dtScale) {
    // Player logic
    let anyMoving = false;
    for (const name in players) {
        const p = players[name];
        if (p.isDead) continue;
        
        // Execute Action Movement
        if (currentPhase === 'ACTION' && p.moving) {
            anyMoving = true;
            const speed = 2.0 * dtScale;
            if (p.actionType === "LEFT") {
                p.x -= speed;
                if (p.x <= p.moveTarget || p.x <= 0) p.moving = false;
            } else if (p.actionType === "RIGHT") {
                p.x += speed;
                if (p.x >= p.moveTarget || p.x >= WIDTH) p.moving = false;
            }
        }

        // Roaming in IDLE
        if (currentPhase === 'IDLE') {
            p.x += p.dx * dtScale;
            if (p.x < 50 || p.x > WIDTH - 50) {
                p.dx *= -1;
            }
        }

        // Falling/Ground snapping
        const floorY = terrain[Math.floor(p.x)];
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
            safeSend({ type: 'PLAYER_DIED', payload: name });
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
        if (proj.x < 0 || proj.x > WIDTH || proj.y > HEIGHT) {
            hit = true;
        } 
        // Terrain collision
        else if (proj.y >= terrain[Math.floor(proj.x)]) {
            hit = true;
            destroyTerrain(proj.x, proj.y, EXPLOSION_RADIUS);
            checkTankCollisions(proj.x, proj.y, EXPLOSION_RADIUS, proj.owner);
        }
        // Direct tank collision (simplified)
        else {
            for (const name in players) {
                if (name === proj.owner) continue; // No self-damage
                const p = players[name];
                if (p.isDead) continue;
                if (Math.hypot(p.x - proj.x, p.y - proj.y) < 20) {
                    hit = true;
                    destroyTerrain(proj.x, proj.y, EXPLOSION_RADIUS);
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
    if (currentPhase === 'CELEBRATION') {
        const elapsed = celebrationStartTime > 0 ? performance.now() - celebrationStartTime : 0;
        if (celebrationWinner && elapsed < 3500 && Math.random() < 0.2) {
            const p = players[celebrationWinner];
            const emoteUrl = p ? p.emoteUrl : "";
            projectiles.push({
                x: Math.random() * WIDTH,
                y: -30,
                vx: (Math.random() - 0.5) * 5,
                vy: Math.random() * 5 + 5,
                owner: celebrationWinner,
                emoteUrl: emoteUrl
            });
        }

        // Only complete celebration after all bombs and explosions have fully settled
        if (elapsed >= 3500 && projectiles.length === 0 && explosions.length === 0 && !celebrationSentComplete) {
            celebrationSentComplete = true;
            safeSend({ type: 'CELEBRATION_COMPLETE' });
        }
    }

    // Phase transition check
    if (currentPhase === 'ACTION' && projectiles.length === 0 && explosions.length === 0 && !anyMoving) {
        // Wait a little bit for tanks to fall if needed, then end phase
        // Here we just end it immediately for simplicity
        let anyFalling = false;
        for (const name in players) {
            if (!players[name].isDead && players[name].y < terrain[Math.floor(players[name].x)]) {
                anyFalling = true; break;
            }
        }
        if (!anyFalling) {
            currentPhase = 'WAITING_NEXT_PHASE'; // Prevent spamming
            safeSend({ type: 'ACTION_COMPLETE' });
            
            // Check win condition
            let aliveCount = 0;
            let aliveName = "";
            let totalPlayers = 0;
            for (const key in players) {
                totalPlayers++;
                if (!players[key].isDead) {
                    aliveCount++;
                    aliveName = key;
                }
            }

            if (aliveCount <= 1 && totalPlayers > 1 || (totalPlayers === 1 && aliveCount === 0)) {
                 const winner = aliveCount === 1 ? aliveName : "";
                 celebrationWinner = winner;
                 safeSend({ type: 'GAME_OVER', payload: winner });
                 
                 const celebImg = document.getElementById('celebration-avatar');
                 celebImg.style.display = 'none';

                 if(winner) {
                     showKillMessage(`${winner} WINS THE GAME!`);
                     celebrationText.innerText = `${winner} WINS!`;
                     
                     if (avatarCache[winner] && avatarCache[winner] !== 'fetching') {
                         celebImg.src = avatarCache[winner];
                         celebImg.style.display = 'block';
                     } else {
                         fetch(`https://decapi.me/twitch/avatar/${winner}`)
                             .then(r => r.text())
                             .then(url => {
                                 avatarCache[winner] = url;
                                 celebImg.src = url;
                                 celebImg.style.display = 'block';
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

function draw() {
    ctx.clearRect(0, 0, WIDTH, HEIGHT);

    // Draw Terrain (Red Neon Line)
    ctx.beginPath();
    ctx.moveTo(0, terrain[0]);
    for (let x = 1; x < WIDTH; x++) {
        ctx.lineTo(x, terrain[x]);
    }
    ctx.strokeStyle = '#ff003c';
    ctx.lineWidth = 4;
    ctx.shadowBlur = 15;
    ctx.shadowColor = '#ff003c';
    ctx.stroke();
    
    // Fill below terrain with slight red tint
    ctx.lineTo(WIDTH, HEIGHT);
    ctx.lineTo(0, HEIGHT);
    ctx.closePath();
    ctx.fillStyle = 'rgba(255, 0, 60, 0.02)';
    ctx.fill();
    ctx.shadowBlur = 0; // reset

    // Draw Giant Protractor during INPUT phase
    if (currentPhase === 'INPUT') {
        ctx.save();
        ctx.translate(250, 250); // Top-left position
        
        ctx.strokeStyle = 'rgba(0, 255, 204, 0.5)';
        ctx.lineWidth = 10;
        ctx.shadowBlur = 20;
        ctx.shadowColor = '#00ffcc';
        
        // Draw giant arc
        ctx.beginPath();
        ctx.arc(0, 0, 150, Math.PI, 0);
        ctx.stroke();

        ctx.fillStyle = '#00ffcc';
        ctx.font = '24px Orbitron';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        
        const angles = [0, 45, 90, 135, 180];
        for (const deg of angles) {
            const rad = deg * Math.PI / 180;
            const innerR = 130;
            const outerR = 150;
            
            ctx.beginPath();
            ctx.lineWidth = 3;
            ctx.moveTo(Math.cos(rad) * innerR, -Math.sin(rad) * innerR);
            ctx.lineTo(Math.cos(rad) * outerR, -Math.sin(rad) * outerR);
            ctx.stroke();
            
            ctx.fillText(deg.toString() + "°", Math.cos(rad) * 180, -Math.sin(rad) * 180);
        }
        ctx.restore();
    }

    // Draw Tanks
    for (const name in players) {
        const p = players[name];
        let imgEl = document.getElementById('emote-' + name);
        if (!imgEl && p.emoteUrl) {
            imgEl = document.createElement('img');
            imgEl.id = 'emote-' + name;
            imgEl.className = 'tank-emote';
            imgEl.src = p.emoteUrl;
            emotesLayer.appendChild(imgEl);
        }
        if (p.isDead) {
            if (imgEl) imgEl.style.display = 'none';
            continue;
        }

        ctx.save();
        ctx.translate(p.x, p.y);
        
        // Calculate slope for climbing walls
        const slopeX1 = Math.max(0, Math.floor(p.x - 5));
        const slopeX2 = Math.min(WIDTH - 1, Math.floor(p.x + 5));
        const angle = Math.atan2(terrain[slopeX2] - terrain[slopeX1], slopeX2 - slopeX1);
        ctx.rotate(angle);

        // Draw Treads (Neon rectangle)
        ctx.strokeStyle = '#ff003c';
        ctx.lineWidth = 2;
        ctx.shadowBlur = 10;
        ctx.shadowColor = '#ff003c';
        ctx.strokeRect(-15, -10, 30, 10);
        
        ctx.restore();

        // Sync DOM Emote position and rotation
        if (imgEl) {
            imgEl.style.display = 'block';
            imgEl.style.left = (p.x - 14) + 'px';
            imgEl.style.top = (p.y - 35) + 'px';
            imgEl.style.transformOrigin = "14px 35px";
            imgEl.style.transform = `rotate(${angle}rad)`;
        }

        // Draw Name (unrotated)
        ctx.fillStyle = '#fff';
        ctx.font = '16px Orbitron';
        ctx.textAlign = 'center';
        ctx.shadowBlur = 5;
        ctx.shadowColor = '#000';
        ctx.fillText(name, p.x, p.y + 20);

        // Draw Protractor & Firing state in INPUT phase
        if (currentPhase === 'INPUT') {
            ctx.strokeStyle = p.fired ? '#00ffcc' : '#ff003c'; // Turns cyan if they locked in
            ctx.shadowColor = ctx.strokeStyle;
            ctx.lineWidth = 2;
            
            // Protractor Arc
            ctx.beginPath();
            ctx.arc(p.x, p.y - 10, 50, Math.PI, 0);
            ctx.stroke();

            // Degree markers
            ctx.fillStyle = ctx.strokeStyle;
            ctx.font = '10px Orbitron';
            const angles = [0, 45, 90, 135, 180];
            for (const deg of angles) {
                const rad = deg * Math.PI / 180;
                const innerR = 45;
                const outerR = 50;
                ctx.beginPath();
                ctx.moveTo(p.x + Math.cos(rad) * innerR, (p.y - 10) - Math.sin(rad) * innerR);
                ctx.lineTo(p.x + Math.cos(rad) * outerR, (p.y - 10) - Math.sin(rad) * outerR);
                ctx.stroke();
                
                // Text for angle
                ctx.fillText(deg.toString(), p.x + Math.cos(rad) * 60, (p.y - 10) - Math.sin(rad) * 60);
            }

            // Aiming line
            const aimAngle = (p.lastAngle) * Math.PI / 180;
            ctx.beginPath();
            ctx.moveTo(p.x, p.y - 10);
            ctx.lineTo(p.x + Math.cos(aimAngle) * 50, (p.y - 10) - Math.sin(aimAngle) * 50);
            ctx.stroke();
        }
    }

    // Draw Projectiles
    for (const proj of projectiles) {
        if (proj.emoteUrl && emoteCache[proj.emoteUrl] && emoteCache[proj.emoteUrl].complete) {
            ctx.shadowBlur = 0;
            ctx.drawImage(emoteCache[proj.emoteUrl], proj.x - 7, proj.y - 7, 14, 14);
        } else {
            ctx.fillStyle = '#00ffcc';
            ctx.shadowBlur = 10;
            ctx.shadowColor = '#00ffcc';
            ctx.beginPath();
            ctx.arc(proj.x, proj.y, 4, 0, Math.PI * 2);
            ctx.fill();
        }
    }

    // Draw Explosions
    for (const exp of explosions) {
        ctx.strokeStyle = `rgba(255, 0, 60, ${exp.alpha})`;
        ctx.shadowBlur = 20;
        ctx.shadowColor = '#ff003c';
        ctx.lineWidth = 4;
        ctx.beginPath();
        ctx.arc(exp.x, exp.y, exp.radius, 0, Math.PI * 2);
        ctx.stroke();
    }
}

function gameLoop(time) {
    if (isDisconnected) return;
    const rawDt = time - lastTime;
    lastTime = time;

    // Normalize dt relative to 60fps (16.667ms)
    // Clamp to prevent huge jumps if tab was backgrounded (max 100ms)
    const dtClamped = Math.min(Math.max(rawDt, 0), 100);
    const baseDtScale = dtClamped / (1000 / 60);
    const speedMultiplier = (stateRef && typeof stateRef.physicsSpeed === 'number') ? stateRef.physicsSpeed : 0.5;
    const dtScale = baseDtScale * speedMultiplier;

    updatePhysics(dtScale);
    draw();

    requestAnimationFrame(gameLoop);
}

// Start
initTerrain();
requestAnimationFrame(gameLoop);

// Debug Command Input
const debugInput = document.getElementById('debug-input');
const debugSendBtn = document.getElementById('debug-send-btn');

function sendDebugCommand() {
    if (!debugInput) return;
    const cmd = debugInput.value.trim();
    if (cmd) {
        safeSend({ type: 'CHAT_COMMAND', payload: cmd });
        debugInput.value = '';
    }
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
