// StreamTanks Admin Console Client Logic
(function () {
    let ws = null;
    let stateRef = null;
    let reconnectTimer = null;
    const avatarCache = {};
    const commandHistory = JSON.parse(localStorage.getItem('st_admin_history') || '[]');
    let historyIdx = -1;

    // DOM Elements
    const phaseBadge = document.getElementById('phase-badge');
    const connectionPill = document.getElementById('connection-pill');
    const connectionText = document.getElementById('connection-text');
    const btnQuickStart = document.getElementById('btn-quick-start');
    const btnQuickBouncy = document.getElementById('btn-quick-bouncy');
    const bouncyStatusText = document.getElementById('bouncy-status-text');
    const btnQuickJoin = document.getElementById('btn-quick-join');
    const btnToggleIdle = document.getElementById('btn-toggle-idle');
    const idleStatusText = document.getElementById('idle-status-text');
    const btnResetTerrain = document.getElementById('btn-reset-terrain');
    const btnClearLb = document.getElementById('btn-clear-lb');

    // Config Inputs
    const cfgPrefix = document.getElementById('cfg-prefix');
    const cfgCommandtime = document.getElementById('cfg-commandtime');
    const cfgAutoround = document.getElementById('cfg-autoround');
    const cfgSpeed = document.getElementById('cfg-speed');
    const cfgStartperm = document.getElementById('cfg-startperm');
    const cfgConfigperm = document.getElementById('cfg-configperm');
    const cfgTerrainMin = document.getElementById('cfg-terrain-min');
    const cfgTerrainMax = document.getElementById('cfg-terrain-max');
    const terrainRangeVal = document.getElementById('terrain-range-val');

    // Tables
    const playersTableBody = document.getElementById('players-table-body');
    const playerCountBadge = document.getElementById('player-count-badge');
    const leaderboardTableBody = document.getElementById('leaderboard-table-body');

    // Console
    const consoleOutput = document.getElementById('console-output');
    const consoleForm = document.getElementById('console-form');
    const consoleInput = document.getElementById('console-input');
    const btnClearConsole = document.getElementById('btn-clear-console');

    // Log to Console UI
    function logConsole(msg, type = 'system') {
        const line = document.createElement('div');
        line.className = `console-line ${type}`;
        const time = new Date().toLocaleTimeString();
        line.innerText = `[${time}] ${msg}`;
        consoleOutput.appendChild(line);
        consoleOutput.scrollTop = consoleOutput.scrollHeight;
    }

    // Connect WebSocket
    function connectWS() {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${proto}//${window.location.host}/ws`;

        connectionPill.className = 'connection-pill';
        connectionText.innerText = 'Connecting...';

        try {
            ws = new WebSocket(wsUrl);

            ws.onopen = () => {
                connectionPill.className = 'connection-pill connected';
                connectionText.innerText = 'Connected';
                logConsole('Connected to StreamTanks live server.', 'system');
                if (reconnectTimer) {
                    clearInterval(reconnectTimer);
                    reconnectTimer = null;
                }
            };

            ws.onmessage = (evt) => {
                try {
                    const msg = JSON.parse(evt.data);
                    handleServerMessage(msg);
                } catch (e) {
                    console.error('Failed to parse WebSocket message:', e);
                }
            };

            ws.onclose = () => {
                connectionPill.className = 'connection-pill disconnected';
                connectionText.innerText = 'Disconnected';
                logConsole('WebSocket disconnected. Retrying in 2s...', 'error');
                scheduleReconnect();
            };

            ws.onerror = () => {
                connectionPill.className = 'connection-pill disconnected';
                connectionText.innerText = 'Connection Error';
            };
        } catch (e) {
            scheduleReconnect();
        }
    }

    function scheduleReconnect() {
        if (!reconnectTimer) {
            reconnectTimer = setInterval(connectWS, 2000);
        }
    }

    // Send Command via WebSocket
    function sendCommand(cmdStr) {
        if (!cmdStr) return;
        const prefix = stateRef?.prefix || '%';
        let formatted = cmdStr.trim();
        if (!formatted.startsWith(prefix) && !formatted.startsWith('!')) {
            formatted = prefix + formatted;
        }

        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'CHAT_COMMAND', payload: formatted }));
            logConsole(`> ${formatted}`, 'cmd');

            // Save to history
            if (commandHistory[commandHistory.length - 1] !== formatted) {
                commandHistory.push(formatted);
                if (commandHistory.length > 50) commandHistory.shift();
                localStorage.setItem('st_admin_history', JSON.stringify(commandHistory));
            }
            historyIdx = -1;
        } else {
            logConsole(`Error: WebSocket not connected. Cannot send: ${formatted}`, 'error');
        }
    }

    // Handle WebSocket Message
    function handleServerMessage(msg) {
        if (msg.type === 'STATE_UPDATE') {
            const state = msg.payload;
            stateRef = state;
            updateDashboard(state);
        }
    }

    // Update Dashboard UI from GameState
    function updateDashboard(state) {
        const prefix = state.prefix || '%';

        // Phase Badge
        phaseBadge.innerText = state.phase || 'IDLE';
        phaseBadge.className = `phase-badge ${(state.phase || 'idle').toLowerCase()}`;

        // Quick button states
        const isBouncy = !!state.bouncyWalls;
        bouncyStatusText.innerText = isBouncy ? 'On (+10% bullet)' : 'Off';
        btnQuickBouncy.className = isBouncy ? 'btn btn-outline' : 'btn btn-secondary';

        const isIdle = state.idleMessage !== false;
        idleStatusText.innerText = isIdle ? 'On' : 'Off';
        btnToggleIdle.className = isIdle ? 'btn btn-outline' : 'btn btn-secondary';

        // Populate Form Controls (only if user is not actively focused on them)
        if (document.activeElement !== cfgPrefix) cfgPrefix.value = prefix;
        if (document.activeElement !== cfgCommandtime) cfgCommandtime.value = state.inputDuration || 20;
        if (document.activeElement !== cfgAutoround) cfgAutoround.value = state.autoRound !== undefined ? state.autoRound.toString() : '0';
        if (document.activeElement !== cfgSpeed) cfgSpeed.value = state.physicsSpeed || 0.5;
        if (document.activeElement !== cfgStartperm) cfgStartperm.value = state.startPerm || 'broadcaster';
        if (document.activeElement !== cfgConfigperm) cfgConfigperm.value = state.configPerm || 'broadcaster';

        const tMin = state.terrainMin || 20;
        const tMax = state.terrainMax || 75;
        if (document.activeElement !== cfgTerrainMin) cfgTerrainMin.value = tMin;
        if (document.activeElement !== cfgTerrainMax) cfgTerrainMax.value = tMax;
        terrainRangeVal.innerText = `${tMin}% — ${tMax}%`;

        // Render Active Players Table
        const players = state.players || {};
        const playerNames = Object.keys(players);
        playerCountBadge.innerText = playerNames.length.toString();

        if (playerNames.length === 0) {
            playersTableBody.innerHTML = `
                <tr class="empty-row">
                    <td colspan="5">No active players. Waiting for match or ${prefix}join commands.</td>
                </tr>
            `;
        } else {
            playersTableBody.innerHTML = playerNames.map((name) => {
                const p = players[name];
                const isDead = !!p.isDead;
                const fired = !!p.fired;
                let statusClass = 'alive';
                let statusText = 'Aiming';
                if (isDead) {
                    statusClass = 'dead';
                    statusText = 'Dead';
                } else if (fired) {
                    statusClass = 'fired';
                    statusText = 'Locked In';
                }

                const emoteUrl = p.emoteUrl || 'https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0';

                return `
                    <tr>
                        <td>
                            <div class="player-cell">
                                <img class="player-avatar" src="${emoteUrl}" alt="" />
                                <span class="player-name">${escapeHtml(name)}</span>
                            </div>
                        </td>
                        <td>${p.angle ?? p.lastAngle ?? 45}°</td>
                        <td>${p.power ?? p.lastPower ?? 50}%</td>
                        <td><span class="status-badge ${statusClass}">${statusText}</span></td>
                        <td>
                            <button class="btn btn-tiny btn-danger btn-kick-player" data-player="${escapeHtml(name)}">Kick</button>
                        </td>
                    </tr>
                `;
            }).join('');
        }

        // Render Leaderboard Table
        const lb = state.leaderboard || {};
        const sortedLb = Object.entries(lb).sort((a, b) => b[1] - a[1]);

        if (sortedLb.length === 0) {
            leaderboardTableBody.innerHTML = `
                <tr class="empty-row">
                    <td colspan="4">No scores recorded yet.</td>
                </tr>
            `;
        } else {
            leaderboardTableBody.innerHTML = sortedLb.map(([name, wins], index) => {
                return `
                    <tr>
                        <td style="font-family: var(--font-mono); color: var(--text-muted);">${index + 1}</td>
                        <td>
                            <div class="player-cell">
                                <img id="admin-avatar-${escapeHtml(name)}" class="player-avatar" src="${avatarCache[name] || ''}" style="${avatarCache[name] ? '' : 'display:none;'}" />
                                <span class="player-name">${escapeHtml(name)}</span>
                            </div>
                        </td>
                        <td style="font-family: var(--font-mono); font-weight: 700; color: var(--neon-cyan);">${wins}</td>
                        <td>
                            <button class="btn btn-tiny btn-danger btn-del-player" data-player="${escapeHtml(name)}">Delete</button>
                        </td>
                    </tr>
                `;
            }).join('');

            // Lazy fetch avatars for leaderboard
            for (const [name] of sortedLb) {
                if (!avatarCache[name]) {
                    avatarCache[name] = 'fetching';
                    fetch(`https://decapi.me/twitch/avatar/${name}`)
                        .then((r) => r.text())
                        .then((url) => {
                            avatarCache[name] = url;
                            const el = document.getElementById(`admin-avatar-${name}`);
                            if (el) {
                                el.src = url;
                                el.style.display = 'inline-block';
                            }
                        })
                        .catch(() => {});
                }
            }
        }
    }

    function escapeHtml(str) {
        return (str || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    // Event Listeners: Quick Action Buttons
    btnQuickStart.addEventListener('click', () => {
        sendCommand('startgame');
    });

    btnQuickBouncy.addEventListener('click', () => {
        const nextVal = stateRef?.bouncyWalls ? 'off' : 'on';
        sendCommand(`bouncywalls ${nextVal}`);
    });

    btnToggleIdle.addEventListener('click', () => {
        const nextVal = stateRef?.idleMessage ? 'off' : 'on';
        sendCommand(`idlemessage ${nextVal}`);
    });

    btnQuickJoin.addEventListener('click', () => {
        sendCommand('join TargetBot');
    });

    btnResetTerrain.addEventListener('click', () => {
        sendCommand('terrain reset');
    });

    btnClearLb.addEventListener('click', () => {
        if (confirm('Are you sure you want to CLEAR the entire leaderboard? This will wipe all statistics from SQLite.')) {
            sendCommand('clearleaderboard');
        }
    });

    // Event Listeners: Config Apply Buttons
    document.getElementById('btn-apply-prefix').addEventListener('click', () => {
        const val = cfgPrefix.value.trim();
        if (val) sendCommand(`prefix ${val}`);
    });

    document.getElementById('btn-apply-commandtime').addEventListener('click', () => {
        const val = parseInt(cfgCommandtime.value, 10);
        if (val >= 5 && val <= 120) sendCommand(`roundtime ${val}`);
    });

    document.getElementById('btn-apply-autoround').addEventListener('click', () => {
        sendCommand(`autoround ${cfgAutoround.value}`);
    });

    document.getElementById('btn-apply-speed').addEventListener('click', () => {
        sendCommand(`speed ${cfgSpeed.value}`);
    });

    document.getElementById('btn-apply-startperm').addEventListener('click', () => {
        sendCommand(`startperm ${cfgStartperm.value}`);
    });

    document.getElementById('btn-apply-configperm').addEventListener('click', () => {
        sendCommand(`configperm ${cfgConfigperm.value}`);
    });

    // Real-time slider update
    function syncTerrainSliderLabel() {
        let minVal = parseInt(cfgTerrainMin.value, 10);
        let maxVal = parseInt(cfgTerrainMax.value, 10);
        if (minVal > maxVal - 10) minVal = maxVal - 10;
        terrainRangeVal.innerText = `${minVal}% — ${maxVal}%`;
    }
    cfgTerrainMin.addEventListener('input', syncTerrainSliderLabel);
    cfgTerrainMax.addEventListener('input', syncTerrainSliderLabel);

    document.getElementById('btn-apply-terrain').addEventListener('click', () => {
        let minVal = parseInt(cfgTerrainMin.value, 10);
        let maxVal = parseInt(cfgTerrainMax.value, 10);
        if (minVal > maxVal - 10) minVal = maxVal - 10;
        sendCommand(`terrain ${minVal} ${maxVal}`);
    });

    // Event Delegation: Delete Player from Leaderboard & Kick
    document.addEventListener('click', (e) => {
        const delBtn = e.target.closest('.btn-del-player');
        if (delBtn) {
            const player = delBtn.dataset.player;
            if (player && confirm(`Delete "${player}" from the persistent leaderboard?`)) {
                sendCommand(`deleteplayer ${player}`);
            }
            return;
        }

        const kickBtn = e.target.closest('.btn-kick-player');
        if (kickBtn) {
            const player = kickBtn.dataset.player;
            if (player) {
                sendCommand(`deleteplayer ${player}`);
            }
            return;
        }

        const pill = e.target.closest('.console-pill');
        if (pill) {
            const cmd = pill.dataset.cmd;
            if (cmd) {
                consoleInput.value = cmd;
                consoleInput.focus();
            }
            return;
        }
    });

    // Console Command Input Handling
    consoleForm.addEventListener('submit', (e) => {
        e.preventDefault();
        const cmd = consoleInput.value.trim();
        if (cmd) {
            sendCommand(cmd);
            consoleInput.value = '';
        }
    });

    // Command History (Up / Down Arrows)
    consoleInput.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (commandHistory.length > 0) {
                if (historyIdx === -1) historyIdx = commandHistory.length - 1;
                else if (historyIdx > 0) historyIdx--;
                consoleInput.value = commandHistory[historyIdx];
            }
        } else if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (commandHistory.length > 0 && historyIdx !== -1) {
                if (historyIdx < commandHistory.length - 1) {
                    historyIdx++;
                    consoleInput.value = commandHistory[historyIdx];
                } else {
                    historyIdx = -1;
                    consoleInput.value = '';
                }
            }
        }
    });

    btnClearConsole.addEventListener('click', () => {
        consoleOutput.innerHTML = '<div class="console-line system">[SYSTEM] Console log cleared.</div>';
    });

    // Initialize
    connectWS();
})();
