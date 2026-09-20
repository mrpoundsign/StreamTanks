// StreamTanks Admin Console Client Logic
(function () {
    let ws = null;
    let stateRef = null;
    let reconnectTimer = null;
    const avatarCache = {};
    let isTerrainDirty = false;
    let isProtractorDirty = false;
    let savedProtractorX = 250;
    let savedProtractorY = 350;
    let protractorDebounce = null;

    // DOM Elements
    const phaseBadge = document.getElementById('phase-badge');
    const connectionPill = document.getElementById('connection-pill');
    const connectionText = document.getElementById('connection-text');
    const btnQuickStart = document.getElementById('btn-quick-start');
    const btnQuickBouncy = document.getElementById('btn-quick-bouncy');
    const bouncyStatusText = document.getElementById('bouncy-status-text');
    const btnToggleIdle = document.getElementById('btn-toggle-idle');
    const idleStatusText = document.getElementById('idle-status-text');
    const btnToggleBotfill = document.getElementById('btn-toggle-botfill');
    const botfillStatusText = document.getElementById('botfill-status-text');
    const btnResetTerrain = document.getElementById('btn-reset-terrain');
    const btnClearLb = document.getElementById('btn-clear-lb');

    // Header Channel Pill Elements
    const channelDisplay = document.getElementById('channel-display');
    const channelEditForm = document.getElementById('channel-edit-form');
    const headerChannelName = document.getElementById('header-channel-name');
    const btnEditChannel = document.getElementById('btn-edit-channel');
    const headerChannelInput = document.getElementById('header-channel-input');
    const btnSaveChannel = document.getElementById('btn-save-channel');
    const btnCancelChannel = document.getElementById('btn-cancel-channel');
    let isEditingChannel = false;

    // Config Inputs
    const cfgPrefix = document.getElementById('cfg-prefix');
    const cfgCommandtime = document.getElementById('cfg-commandtime');
    const autoroundCurrentVal = document.getElementById('autoround-current-val');
    const cfgAutoroundCustom = document.getElementById('cfg-autoround-custom');
    const cfgMinplayers = document.getElementById('cfg-minplayers');
    const cfgBotpoints = document.getElementById('cfg-botpoints');
    const cfgSpeed = document.getElementById('cfg-speed');
    const cfgStartperm = document.getElementById('cfg-startperm');
    const cfgConfigperm = document.getElementById('cfg-configperm');
    const cfgTerrainMin = document.getElementById('cfg-terrain-min');
    const cfgTerrainMax = document.getElementById('cfg-terrain-max');
    const terrainRangeVal = document.getElementById('terrain-range-val');
    const cfgTerrainColor = document.getElementById('cfg-terrain-color');
    const terrainColorVal = document.getElementById('terrain-color-val');
    const btnApplyTerrainColor = document.getElementById('btn-apply-terrain-color');
    const btnResetTerrainColor = document.getElementById('btn-reset-terrain-color');
    const colorSwatches = document.querySelectorAll('.btn-swatch');
    let isColorDirty = false;
    const cfgProtractorX = document.getElementById('cfg-protractor-x');
    const cfgProtractorY = document.getElementById('cfg-protractor-y');
    const protractorPosVal = document.getElementById('protractor-pos-val');

    // C&C Relay Elements
    const ccStatusBadge = document.getElementById('cc-status-badge');
    const ccClaimBanner = document.getElementById('cc-claim-banner');
    const ccClaimCode = document.getElementById('cc-claim-code');
    const btnCopyClaim = document.getElementById('btn-copy-claim');
    const cfgCcUrl = document.getElementById('cfg-cc-url');
    const btnApplyCcUrl = document.getElementById('btn-apply-cc-url');
    const btnToggleCc = document.getElementById('btn-toggle-cc');
    const ccStatusText = document.getElementById('cc-status-text');
    const btnReconnectCc = document.getElementById('btn-reconnect-cc');
    const btnResetCcKey = document.getElementById('btn-reset-cc-key');

    // Tables
    const playersTableBody = document.getElementById('players-table-body');
    const playerCountBadge = document.getElementById('player-count-badge');
    const leaderboardTableBody = document.getElementById('leaderboard-table-body');


    // Connect WebSocket
    function connectWS() {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${proto}//${window.location.host}/ws?client=admin`;

        connectionPill.className = 'connection-pill';
        connectionText.innerText = 'Connecting...';

        try {
            ws = new WebSocket(wsUrl);

            ws.onopen = () => {
                connectionPill.className = 'connection-pill connected';
                connectionText.innerText = 'Connected';
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
        } else {
            console.warn(`WebSocket not connected. Cannot send: ${formatted}`);
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
        const hasJoinedHumans = Object.values(state.players || {}).some((p) => !p.isBot && p.joined);
        const canStartGame = (state.phase === 'IDLE' || !state.phase) && hasJoinedHumans;
        if (btnQuickStart) {
            btnQuickStart.disabled = !canStartGame;
        }


        const isBouncy = !!state.bouncyWalls;
        bouncyStatusText.innerText = isBouncy ? 'On (+10% bullet)' : 'Off';
        btnQuickBouncy.className = isBouncy ? 'btn btn-outline' : 'btn btn-secondary';

        const isIdle = state.idleMessage !== false;
        idleStatusText.innerText = isIdle ? 'On' : 'Off';
        btnToggleIdle.className = isIdle ? 'btn btn-outline' : 'btn btn-secondary';

        const isBotFill = state.botFill !== false;
        botfillStatusText.innerText = isBotFill ? 'On' : 'Off';
        btnToggleBotfill.className = isBotFill ? 'btn btn-outline' : 'btn btn-secondary';

        // Auto Round Display & Preset Highlights
        const ar = state.autoRound !== undefined ? state.autoRound : 0;
        let arText = 'Off';
        if (ar === -1) arText = 'Instant';
        else if (ar > 0) arText = `${ar}m`;
        autoroundCurrentVal.innerText = arText;

        document.querySelectorAll('.btn-preset').forEach((btn) => {
            const val = parseInt(btn.dataset.ar, 10);
            if (val === ar) {
                btn.classList.add('active');
            } else {
                btn.classList.remove('active');
            }
        });

        // Header Channel Pill Update
        const currentChannel = state.channel || '';
        if (headerChannelName && !isEditingChannel) {
            if (currentChannel) {
                headerChannelName.innerText = currentChannel;
                headerChannelName.className = 'channel-val';
                if (btnEditChannel) {
                    btnEditChannel.innerText = 'Edit';
                    btnEditChannel.className = 'btn btn-tiny btn-secondary';
                    btnEditChannel.title = 'Change Twitch Channel';
                }
            } else {
                headerChannelName.innerText = 'None (Local)';
                headerChannelName.className = 'channel-val unset';
                if (btnEditChannel) {
                    btnEditChannel.innerText = 'Set';
                    btnEditChannel.className = 'btn btn-tiny btn-primary';
                    btnEditChannel.title = 'Set Twitch Channel';
                }
            }
        }

        // Populate Form Controls (only if user is not actively focused on them)
        if (document.activeElement !== cfgPrefix) cfgPrefix.value = prefix;
        if (document.activeElement !== cfgCommandtime) cfgCommandtime.value = state.inputDuration || 20;
        if (document.activeElement !== cfgMinplayers) cfgMinplayers.value = state.minPlayers || 5;
        if (document.activeElement !== cfgBotpoints) cfgBotpoints.value = state.botPoints !== undefined ? state.botPoints : 1;
        if (document.activeElement !== cfgSpeed) cfgSpeed.value = state.physicsSpeed || 0.5;
        if (document.activeElement !== cfgStartperm) cfgStartperm.value = state.startPerm || 'broadcaster';
        if (document.activeElement !== cfgConfigperm) cfgConfigperm.value = state.configPerm || 'broadcaster';

        if (!isTerrainDirty) {
            const tMin = state.terrainMin || 20;
            const tMax = state.terrainMax || 75;
            if (cfgTerrainMin) cfgTerrainMin.value = tMin;
            if (cfgTerrainMax) cfgTerrainMax.value = tMax;
            if (terrainRangeVal) terrainRangeVal.innerText = `${tMin}% — ${tMax}%`;
            
            // Highest point a tank can be is at the highest terrain elevation
            const maxPy = Math.max(100, Math.floor(1080 * (1 - tMax / 100)));
            if (cfgProtractorY) {
                cfgProtractorY.max = maxPy;
            }
        }

        const activeColor = state.terrainColor || '#ff003c';
        if (!isColorDirty) {
            if (cfgTerrainColor && document.activeElement !== cfgTerrainColor) {
                cfgTerrainColor.value = activeColor;
            }
            if (terrainColorVal) {
                terrainColorVal.innerText = activeColor;
                terrainColorVal.style.color = activeColor;
            }
            colorSwatches.forEach(swatch => {
                if (swatch.dataset.color && swatch.dataset.color.toLowerCase() === activeColor.toLowerCase()) {
                    swatch.classList.add('active');
                } else {
                    swatch.classList.remove('active');
                }
            });
        }

        if (!isProtractorDirty) {
            const px = state.protractorX ?? 250;
            const py = state.protractorY ?? 350;
            savedProtractorX = px;
            savedProtractorY = py;
            if (cfgProtractorX) cfgProtractorX.value = px;
            if (cfgProtractorY) cfgProtractorY.value = py;
            if (protractorPosVal) protractorPosVal.innerText = `X: ${px}, Y: ${py}`;
        }

        // C&C Relay UI Sync
        const isCcEnabled = !!state.ccEnabled;
        const ccStatus = state.ccStatus || 'disconnected';
        const claimCode = state.claimCode || '';
        const ccUrl = state.ccServerUrl || 'wss://st-cc.poundsigndesign.com';

        if (ccStatusText) {
            ccStatusText.innerText = isCcEnabled ? 'Enabled' : 'Disabled';
        }
        if (btnToggleCc) {
            btnToggleCc.className = isCcEnabled ? 'btn btn-outline' : 'btn btn-secondary';
        }
        if (cfgCcUrl && document.activeElement !== cfgCcUrl) {
            cfgCcUrl.value = ccUrl;
        }

        if (ccStatusBadge) {
            ccStatusBadge.className = `card-badge cc-badge ${ccStatus}`;
            if (ccStatus === 'connected') {
                ccStatusBadge.innerText = 'CONNECTED';
            } else if (ccStatus === 'connecting') {
                ccStatusBadge.innerText = 'CONNECTING';
            } else if (ccStatus === 'pending_claim') {
                ccStatusBadge.innerText = 'PENDING CLAIM';
            } else {
                ccStatusBadge.innerText = 'DISCONNECTED';
            }
        }

        if (ccClaimBanner && ccClaimCode) {
            if (claimCode && ccStatus === 'pending_claim') {
                ccClaimBanner.style.display = 'block';
                ccClaimCode.innerText = `%claim ${claimCode}`;
            } else {
                ccClaimBanner.style.display = 'none';
            }
        }

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
                const isBot = !!p.isBot;
                let statusClass = 'alive';
                let statusText = 'Aiming';
                if (isDead) {
                    statusClass = 'dead';
                    statusText = 'Dead';
                } else if (state.phase === 'IDLE' && !isBot) {
                    statusClass = p.joined ? 'alive' : 'idle';
                    statusText = p.joined ? 'Joined' : 'Roaming';
                } else if (fired) {
                    statusClass = 'fired';
                    statusText = (state.phase !== 'INPUT' && p.isShielded) ? '🛡️ Shielded' : 'Locked In';
                }

                const emoteUrl = p.emoteUrl || 'https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/2.0';
                const displayName = (p.name && !p.name.startsWith('_bot_')) ? p.name : (!name.startsWith('_bot_') && !isBot ? name : '');
                const playerLabel = isBot
                    ? `${escapeHtml(displayName || 'Nameless Bot')} <span class="bot-badge">BOT</span>`
                    : escapeHtml(name);

                return `
                    <tr>
                        <td>
                            <div class="player-cell">
                                <img class="player-avatar" src="${emoteUrl}" alt="" />
                                <span class="player-name">${playerLabel}</span>
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
        if (btnQuickStart.disabled) return;
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

    btnToggleBotfill.addEventListener('click', () => {
        const nextVal = stateRef?.botFill ? 'off' : 'on';
        sendCommand(`botfill ${nextVal}`);
    });

    btnResetTerrain.addEventListener('click', () => {
        sendCommand('terrain reroll');
    });

    btnClearLb.addEventListener('click', () => {
        if (confirm('Are you sure you want to CLEAR the entire leaderboard? This will wipe all statistics from SQLite.')) {
            sendCommand('clearleaderboard');
        }
    });

    // Event Listeners: Auto Round Presets & Custom Input
    document.querySelectorAll('.btn-preset').forEach((btn) => {
        btn.addEventListener('click', () => {
            const ar = btn.dataset.ar;
            if (ar !== undefined) {
                sendCommand(`autoround ${ar}`);
            }
        });
    });

    const btnApplyAutoRoundCustom = document.getElementById('btn-apply-autoround-custom');
    if (btnApplyAutoRoundCustom) {
        btnApplyAutoRoundCustom.addEventListener('click', () => {
            const val = parseInt(cfgAutoroundCustom.value, 10);
            if (val >= 1 && val <= 60) {
                sendCommand(`autoround ${val}`);
                cfgAutoroundCustom.value = '';
            }
        });
    }

    // Channel Header Edit Handlers
    function startEditChannel() {
        isEditingChannel = true;
        if (channelDisplay) channelDisplay.style.display = 'none';
        if (channelEditForm) channelEditForm.style.display = 'flex';
        if (headerChannelInput) {
            headerChannelInput.value = stateRef?.channel || '';
            headerChannelInput.focus();
            headerChannelInput.select();
        }
    }

    function cancelEditChannel() {
        isEditingChannel = false;
        if (channelEditForm) channelEditForm.style.display = 'none';
        if (channelDisplay) channelDisplay.style.display = 'flex';
    }

    function saveChannel() {
        let val = headerChannelInput ? headerChannelInput.value.trim() : '';
        if (val.startsWith('%') || val.startsWith('!')) {
            val = val.substring(1).trim();
        }
        if (val.toLowerCase().startsWith('channel ')) {
            sendCommand(val);
        } else {
            sendCommand(`channel ${val || 'off'}`);
        }
        cancelEditChannel();
    }

    if (btnEditChannel) {
        btnEditChannel.addEventListener('click', startEditChannel);
    }
    if (btnSaveChannel) {
        btnSaveChannel.addEventListener('click', saveChannel);
    }
    if (btnCancelChannel) {
        btnCancelChannel.addEventListener('click', cancelEditChannel);
    }
    if (headerChannelInput) {
        headerChannelInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                saveChannel();
            } else if (e.key === 'Escape') {
                cancelEditChannel();
            }
        });
    }

    // Event Listeners: Config Apply Buttons

    document.getElementById('btn-apply-prefix').addEventListener('click', () => {
        const val = cfgPrefix.value.trim();
        if (val) sendCommand(`prefix ${val}`);
    });

    document.getElementById('btn-apply-commandtime').addEventListener('click', () => {
        const val = parseInt(cfgCommandtime.value, 10);
        if (val >= 5 && val <= 120) sendCommand(`roundtime ${val}`);
    });

    const btnApplyMinPlayers = document.getElementById('btn-apply-minplayers');
    if (btnApplyMinPlayers) {
        btnApplyMinPlayers.addEventListener('click', () => {
            const val = parseInt(cfgMinplayers.value, 10);
            if (val >= 2 && val <= 20) sendCommand(`minplayers ${val}`);
        });
    }

    const btnApplyBotPoints = document.getElementById('btn-apply-botpoints');
    if (btnApplyBotPoints) {
        btnApplyBotPoints.addEventListener('click', () => {
            const val = parseInt(cfgBotpoints.value, 10);
            if (val >= 0 && val <= 10) sendCommand(`botpoints ${val}`);
        });
    }

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
        isTerrainDirty = true;
        let minVal = parseInt(cfgTerrainMin.value, 10);
        let maxVal = parseInt(cfgTerrainMax.value, 10);
        if (minVal > maxVal - 10) minVal = maxVal - 10;
        terrainRangeVal.innerText = `${minVal}% — ${maxVal}%`;

        const maxPy = Math.max(100, Math.floor(1080 * (1 - maxVal / 100)));
        if (cfgProtractorY) {
            cfgProtractorY.max = maxPy;
            if (parseInt(cfgProtractorY.value, 10) > maxPy) {
                cfgProtractorY.value = maxPy;
                if (protractorPosVal) {
                    protractorPosVal.innerText = `X: ${cfgProtractorX.value}, Y: ${maxPy}`;
                }
            }
        }
    }
    cfgTerrainMin.addEventListener('input', syncTerrainSliderLabel);
    cfgTerrainMax.addEventListener('input', syncTerrainSliderLabel);

    document.getElementById('btn-apply-terrain').addEventListener('click', () => {
        isTerrainDirty = false;
        let minVal = parseInt(cfgTerrainMin.value, 10);
        let maxVal = parseInt(cfgTerrainMax.value, 10);
        if (minVal > maxVal - 10) minVal = maxVal - 10;
        sendCommand(`terrain ${minVal} ${maxVal}`);
    });

    if (cfgTerrainColor) {
        cfgTerrainColor.addEventListener('input', () => {
            isColorDirty = true;
            const chosen = cfgTerrainColor.value;
            if (terrainColorVal) {
                terrainColorVal.innerText = chosen;
                terrainColorVal.style.color = chosen;
            }
            colorSwatches.forEach(swatch => {
                if (swatch.dataset.color && swatch.dataset.color.toLowerCase() === chosen.toLowerCase()) {
                    swatch.classList.add('active');
                } else {
                    swatch.classList.remove('active');
                }
            });
        });
    }

    colorSwatches.forEach(swatch => {
        swatch.addEventListener('click', () => {
            const color = swatch.dataset.color;
            if (cfgTerrainColor) cfgTerrainColor.value = color;
            if (terrainColorVal) {
                terrainColorVal.innerText = color;
                terrainColorVal.style.color = color;
            }
            colorSwatches.forEach(s => s.classList.remove('active'));
            swatch.classList.add('active');
            isColorDirty = false;
            sendCommand(`terraincolor ${color}`);
        });
    });

    if (btnApplyTerrainColor) {
        btnApplyTerrainColor.addEventListener('click', () => {
            isColorDirty = false;
            if (cfgTerrainColor) {
                sendCommand(`terraincolor ${cfgTerrainColor.value}`);
            }
        });
    }

    if (btnResetTerrainColor) {
        btnResetTerrainColor.addEventListener('click', () => {
            isColorDirty = false;
            sendCommand('terraincolor reset');
        });
    }

    // Real-time HUD position slider update
    function syncProtractorSliderLabel() {
        isProtractorDirty = true;
        const px = parseInt(cfgProtractorX.value, 10);
        const py = parseInt(cfgProtractorY.value, 10);
        if (protractorPosVal) protractorPosVal.innerText = `X: ${px}, Y: ${py}`;
        
        if (protractorDebounce) clearTimeout(protractorDebounce);
        protractorDebounce = setTimeout(() => {
            sendCommand(`protractor ${px} ${py} nosave`);
        }, 50);
    }
    if (cfgProtractorX && cfgProtractorY) {
        cfgProtractorX.addEventListener('input', syncProtractorSliderLabel);
        cfgProtractorY.addEventListener('input', syncProtractorSliderLabel);
    }

    const btnApplyProtractor = document.getElementById('btn-apply-protractor');
    if (btnApplyProtractor) {
        btnApplyProtractor.addEventListener('click', () => {
            isProtractorDirty = false;
            if (protractorDebounce) clearTimeout(protractorDebounce);
            const px = parseInt(cfgProtractorX.value, 10);
            const py = parseInt(cfgProtractorY.value, 10);
            sendCommand(`protractor ${px} ${py}`);
        });
    }

    const btnCancelProtractor = document.getElementById('btn-cancel-protractor');
    if (btnCancelProtractor) {
        btnCancelProtractor.addEventListener('click', () => {
            isProtractorDirty = false;
            if (protractorDebounce) clearTimeout(protractorDebounce);
            cfgProtractorX.value = savedProtractorX;
            cfgProtractorY.value = savedProtractorY;
            if (protractorPosVal) protractorPosVal.innerText = `X: ${savedProtractorX}, Y: ${savedProtractorY}`;
            sendCommand(`protractor ${savedProtractorX} ${savedProtractorY}`);
        });
    }

    // Event Listeners: C&C Relay
    if (btnToggleCc) {
        btnToggleCc.addEventListener('click', () => {
            const nextVal = stateRef?.ccEnabled ? 'off' : 'on';
            sendCommand(`cc ${nextVal}`);
        });
    }

    if (btnReconnectCc) {
        btnReconnectCc.addEventListener('click', () => {
            sendCommand('cc on');
        });
    }

    if (btnResetCcKey) {
        btnResetCcKey.addEventListener('click', () => {
            if (confirm('Reset C&C authentication key? This will revoke the existing token and require a new %claim code in Twitch chat.')) {
                sendCommand('cc reset');
            }
        });
    }

    if (btnApplyCcUrl && cfgCcUrl) {
        btnApplyCcUrl.addEventListener('click', () => {
            const url = cfgCcUrl.value.trim();
            if (url) {
                sendCommand(`cc url ${url}`);
            }
        });
    }

    if (btnCopyClaim) {
        btnCopyClaim.addEventListener('click', () => {
            const code = stateRef?.claimCode;
            if (code) {
                const cmd = `%claim ${code}`;
                navigator.clipboard.writeText(cmd).then(() => {
                    const orig = btnCopyClaim.innerText;
                    btnCopyClaim.innerText = 'Copied!';
                    setTimeout(() => { btnCopyClaim.innerText = orig; }, 2000);
                }).catch(() => {
                    prompt('Copy this command and paste into your Twitch chat:', cmd);
                });
            }
        });
    }

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
            if (player && confirm(`Kick "${player}" from the active match?`)) {
                sendCommand(`kick ${player}`);
            }
            return;
        }
    });

    // Initialize
    connectWS();
})();
