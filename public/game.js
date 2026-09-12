const canvas = document.getElementById('gameCanvas');
const ctx = canvas.getContext('2d');
const phaseDisplay = document.getElementById('phase-display');
const timerDisplay = document.getElementById('timer-display');
const killFeed = document.getElementById('kill-feed');
const emotesLayer = document.getElementById('emotes-layer');
const celebrationDisplay = document.getElementById('celebration-display');
const celebrationText = document.getElementById('celebration-text');

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

const avatarCache = {};

// Initialize terrain
function initTerrain() {
    // Generate some wavy hills
    let y = HEIGHT / 2 + Math.random() * 100 - 50;
    terrain[0] = y;
    for (let x = 1; x < WIDTH; x++) {
        let slope = (Math.random() - 0.5) * 2;
        y += slope;
        // Keep in bounds roughly
        if (y < 200) y = 200;
        if (y > HEIGHT - 100) y = HEIGHT - 100;
        terrain[x] = y;
    }
    
    // Parachute all players back in
    for (const name in players) {
        players[name].x = Math.random() * (WIDTH - 100) + 50;
        players[name].y = -50;
    }
}

// WebSocket setup
const ws = new WebSocket(`ws://${window.location.host}/ws`);

ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === 'STATE_UPDATE') {
        const state = msg.payload;
        currentPhase = state.phase;
        
        if (state.leaderboard) {
            updateLeaderboard(state.leaderboard);
        }
        
        // Sync players
        const newPlayers = state.players;
        for (const name in newPlayers) {
            if (!players[name]) {
                // New player joining/roaming
                players[name] = {
                    x: Math.random() * (WIDTH - 100) + 50,
                    y: 0,
                    dx: (Math.random() > 0.5 ? 1 : -1) * 1.5, // Roaming speed
                    ...newPlayers[name]
                };
                // Ensure emote is loaded (dummy loading for now, ideally fetch from Twitch)
                if (!emoteCache[players[name].emote]) {
                    const img = new Image();
                    // Using a placeholder image for emotes. In a real app, use Twitch Emote API.
                    img.src = `https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0`; // Kappa placeholder
                    emoteCache[players[name].emote] = img;
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

    } else if (msg.type === 'EXECUTE_FIRE') {
        executeFire();
    } else if (msg.type === 'RESET_TERRAIN') {
        initTerrain();
    }
};

function updateUI() {
    if (currentPhase === 'IDLE') {
        phaseDisplay.style.display = 'none';
        timerDisplay.style.display = 'none';
        celebrationDisplay.style.display = 'none';
    } else if (currentPhase === 'INPUT') {
        phaseDisplay.style.display = 'block';
        phaseDisplay.innerText = "INPUT PHASE - !fire <angle> <power>";
        timerDisplay.style.display = 'block';
        celebrationDisplay.style.display = 'none';
        inputTimer = 20; // 15s + 5s lag
        timerDisplay.innerText = inputTimer.toString();
        timerDisplay.style.color = "#fff";
        timerDisplay.style.animation = "none";
    } else if (currentPhase === 'ACTION') {
        phaseDisplay.style.display = 'block';
        phaseDisplay.innerText = "ACTION PHASE";
        timerDisplay.style.display = 'none';
        celebrationDisplay.style.display = 'none';
    } else if (currentPhase === 'CELEBRATION') {
        phaseDisplay.style.display = 'none';
        timerDisplay.style.display = 'none';
        celebrationDisplay.style.display = 'block';
    }
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
            timerDisplay.innerText = "FIRING IN " + inputTimer + "!";
            timerDisplay.style.color = "#ff003c";
            timerDisplay.style.animation = "pulse 0.5s infinite alternate";
            timerDisplay.style.textShadow = "0 0 10px #ff003c";
        } else if (inputTimer > 5) {
            timerDisplay.innerText = inputTimer.toString();
            timerDisplay.style.color = "#fff";
            timerDisplay.style.animation = "none";
            timerDisplay.style.textShadow = "0 0 5px #00ffcc";
        } else {
            timerDisplay.innerText = "FIRING!";
        }
    }
}, 1000);

function executeFire() {
    projectiles = [];
    for (const name in players) {
        const p = players[name];
        if (p.isDead) continue;
        // Convert angle (degrees) to radians. 0 is right, 90 is up, 180 is left.
        const rad = p.angle * Math.PI / 180;
        // Scale power
        const velocity = p.power * 0.15; 
        
        projectiles.push({
            owner: name,
            x: p.x,
            y: p.y - 20, // Fire from slightly above the tank
            vx: Math.cos(rad) * velocity,
            vy: -Math.sin(rad) * velocity // -y is up in canvas
        });
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
        const p = players[name];
        if (p.isDead) continue;
        const dist = Math.hypot(p.x - cx, p.y - cy);
        if (dist < radius + 20) {
            // Tank is destroyed
            p.isDead = true;
            showKillMessage(`${owner} destroyed ${name}!`);
            ws.send(JSON.stringify({ type: 'PLAYER_DIED', payload: name }));
            
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

function updatePhysics(dt) {
    // Player logic
    for (const name in players) {
        const p = players[name];
        if (p.isDead) continue;
        
        // Roaming in IDLE
        if (currentPhase === 'IDLE') {
            p.x += p.dx;
            if (p.x < 50 || p.x > WIDTH - 50) {
                p.dx *= -1;
            }
        }

        // Falling/Ground snapping
        const floorY = terrain[Math.floor(p.x)];
        if (p.y < floorY) {
            p.y += 5; // Falling speed
            if (p.y > floorY) p.y = floorY;
        } else {
            p.y = floorY;
        }

        // Fall off bottom of screen
        if (p.y >= HEIGHT) {
            p.isDead = true;
            showKillMessage(`${name} fell into the abyss!`);
            ws.send(JSON.stringify({ type: 'PLAYER_DIED', payload: name }));
            const imgEl = document.getElementById('emote-' + name);
            if (imgEl) imgEl.style.display = 'none';
        }
    }

    // Projectile logic
    for (let i = projectiles.length - 1; i >= 0; i--) {
        const proj = projectiles[i];
        proj.x += proj.vx;
        proj.vy += GRAVITY;
        proj.y += proj.vy;

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
        exp.radius += 2;
        exp.alpha -= 0.05;
        if (exp.alpha <= 0) {
            explosions.splice(i, 1);
        }
    }

    // Celebration random emote bombs
    if (currentPhase === 'CELEBRATION' && celebrationWinner) {
        if (Math.random() < 0.2) {
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
    }

    // Phase transition check
    if (currentPhase === 'ACTION' && projectiles.length === 0 && explosions.length === 0) {
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
            ws.send(JSON.stringify({ type: 'ACTION_COMPLETE' }));
            
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
                 ws.send(JSON.stringify({ type: 'GAME_OVER', payload: winner }));
                 
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

    // Draw Tanks
    for (const name in players) {
        const p = players[name];
        const imgEl = document.getElementById('emote-' + name);
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
            ctx.drawImage(emoteCache[proj.emoteUrl], proj.x - 14, proj.y - 14, 28, 28);
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
    const dt = time - lastTime;
    lastTime = time;

    updatePhysics(dt);
    draw();

    requestAnimationFrame(gameLoop);
}

// Start
initTerrain();
requestAnimationFrame(gameLoop);
