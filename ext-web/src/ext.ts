// Twitch Extension Frontend Logic (Desktop Protractor Overlay & Mobile UI)
// Connects to the C&C WebSocket server to relay commands from viewer to StreamTanks.

export {};

const CC_SERVER_URL = "wss://st-cc.poundsigndesign.com/ws/viewer";

let ws: WebSocket | null = null;
let viewerToken: string = "";
let currentUsername: string = "";
let activePlayers: string[] = [];
let joinedPlayersList: string[] = [];
let leavingPlayersList: string[] = [];
let canStartGame: boolean = false;
let canJoinGame: boolean = true;
let hasJoined: boolean = false;
let isPlayerDead: boolean = false;
let isPlayerLeaving: boolean = false;
let joinRequestedAt: number = 0;
let currentAngle: number = 45;
let currentPower: number = 100;
let pingInterval: number | null = null;
let countdownInterval: number | null = null;
let localTimerRemaining: number = 0;
let currentPhaseStr: string = "IDLE";
let lastProtractorX: number = -1;
let lastProtractorY: number = -1;
let protractorPreviewUntil: number = 0;
let protractorPreviewTimeout: number | null = null;
const isLocalDev = typeof window !== "undefined" && (window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1");
const isStandaloneDev = isLocalDev || (!window.Twitch || !window.Twitch.ext);

// DOM Element Selectors
const viewportSvg = document.getElementById("viewport-svg") as SVGSVGElement | null;
const protractorOverlayGroup = document.getElementById("protractor-overlay-group");
const protractorHitArea = document.getElementById("protractor-hit-area");
const angleNeedle = document.getElementById("angle-needle");
const needleHead = document.getElementById("needle-head");
const angleBadgeGroup = document.getElementById("angle-badge-group");
const mobileAngleBadge = document.querySelector(".mobile-angle-badge") as HTMLElement | null;
const valAngle = document.getElementById("val-angle");

const pivotCenter = document.getElementById("pivot-center") || document.querySelector(".pivot-center");
const deadSkull = document.getElementById("dead-skull");

function setAimingVisible(visible: boolean) {
    if (protractorOverlayGroup) {
        if (visible) {
            protractorOverlayGroup.classList.remove("hidden");
        } else {
            protractorOverlayGroup.classList.add("hidden");
        }
        (protractorOverlayGroup as HTMLElement).style.display = visible ? "" : "none";
        (protractorOverlayGroup as HTMLElement).style.visibility = visible ? "visible" : "hidden";
        protractorOverlayGroup.setAttribute("visibility", visible ? "visible" : "hidden");
    }
    if (protractorHitArea) {
        (protractorHitArea as HTMLElement).style.display = visible ? "" : "none";
        (protractorHitArea as HTMLElement).style.visibility = visible ? "visible" : "hidden";
    }
    if (angleNeedle) {
        (angleNeedle as HTMLElement).style.display = visible ? "" : "none";
        (angleNeedle as HTMLElement).style.visibility = visible ? "visible" : "hidden";
    }
    if (needleHead) {
        (needleHead as HTMLElement).style.display = visible ? "" : "none";
        (needleHead as HTMLElement).style.visibility = visible ? "visible" : "hidden";
    }
    if (angleBadgeGroup) {
        (angleBadgeGroup as HTMLElement).style.display = visible ? "" : "none";
        (angleBadgeGroup as HTMLElement).style.visibility = visible ? "visible" : "hidden";
    }
    if (pivotCenter) {
        (pivotCenter as HTMLElement).style.display = visible ? "" : "none";
        (pivotCenter as HTMLElement).style.visibility = visible ? "visible" : "hidden";
    }
    if (mobileAngleBadge) {
        mobileAngleBadge.style.display = visible ? "" : "none";
        mobileAngleBadge.style.visibility = visible ? "visible" : "hidden";
    }
    if (deadSkull) {
        deadSkull.classList.add("hidden");
        (deadSkull as HTMLElement).style.display = "none";
    }
    if (!visible) {
        setLeaveButtonVisible(false);
    }
}

function showProtractorPreview(durationMs: number = 2500) {
    protractorPreviewUntil = Date.now() + durationMs;
    setAimingVisible(true);
    if (protractorPreviewTimeout) {
        clearTimeout(protractorPreviewTimeout);
    }
    protractorPreviewTimeout = window.setTimeout(() => {
        if (currentPhaseStr !== "INPUT" && !isStandaloneDev) {
            setAimingVisible(false);
        }
    }, durationMs);
}

const sliderPower = document.getElementById("slider-power") as HTMLInputElement | null;
const valPower = document.getElementById("val-power");
const verticalPowerTrack = document.getElementById("vertical-power-track");
const powerFillBar = document.getElementById("power-fill-bar");
const powerThumb = document.getElementById("power-thumb");
const phaseBadge = document.getElementById("phase-badge");
const desktopTimer = document.getElementById("desktop-timer");
const currentActionBadge = document.getElementById("current-action-badge");

function setCurrentAction(actionText: string) {
    if (currentActionBadge) {
        if (actionText && currentPhaseStr === "INPUT") {
            currentActionBadge.textContent = actionText;
            currentActionBadge.classList.remove("hidden");
        } else {
            currentActionBadge.textContent = "";
            currentActionBadge.classList.add("hidden");
        }
    }
}

const adminControls = document.getElementById("admin-controls");
const playerSetup = document.getElementById("player-setup");
const playerControls = document.getElementById("player-controls");
const statusMessage = document.getElementById("status-message");
const msgLog = document.getElementById("message-log");

const btnStartMatch = document.getElementById("btn-start-match");
const btnJoin = document.getElementById("btn-join");
const btnFire = document.getElementById("btn-fire");
const btnLeft = document.getElementById("btn-left");
const btnRight = document.getElementById("btn-right");
const btnLeave = document.getElementById("btn-leave");

function setLeaveButtonVisible(visible: boolean) {
    if (!btnLeave) return;
    if (visible) {
        btnLeave.classList.remove("hidden");
        (btnLeave as HTMLElement).style.display = "";
        (btnLeave as HTMLElement).style.visibility = "visible";
        btnLeave.setAttribute("visibility", "visible");
    } else {
        btnLeave.classList.add("hidden");
        (btnLeave as HTMLElement).style.display = "none";
        (btnLeave as HTMLElement).style.visibility = "hidden";
        btnLeave.setAttribute("visibility", "hidden");
    }
}

// Landing Page Elements
const landingOverlay = document.getElementById("landing-overlay");
const mobileLanding = document.getElementById("mobile-landing");
const landingLauncher = document.getElementById("landing-launcher");
const btnConnectTwitch = document.getElementById("btn-connect-twitch");
const btnConnectTwitchMobile = document.getElementById("btn-connect-twitch-mobile");
const btnDismissLanding = document.getElementById("btn-dismiss-landing");
const btnCloseLanding = document.getElementById("btn-close-landing");
const btnOpenLanding = document.getElementById("btn-open-landing");

let isLinked: boolean = false;
let landingDismissed: boolean = false;

function isOpaque(name: string): boolean {
    return /^[UA]\d+$/i.test(name);
}

function checkIdentityLinked(token: string): boolean {
    if (isLocalDev || !window.Twitch || !window.Twitch.ext) {
        // Standalone local preview mode without Twitch helper
        return true;
    }
    try {
        const payloadStr = atob(token.split('.')[1]);
        const payload = JSON.parse(payloadStr);
        if (payload.user_id && payload.user_id !== "" && !isOpaque(payload.user_id)) {
            return true;
        }
    } catch (_) {}
    if (window.Twitch.ext.viewer?.isLinked && window.Twitch.ext.viewer?.id && !isOpaque(window.Twitch.ext.viewer.id)) {
        return true;
    }
    return false;
}

function updateLandingVisibility() {
    if (isLinked) {
        if (landingOverlay) landingOverlay.classList.add("hidden");
        if (mobileLanding) mobileLanding.classList.add("hidden");
        if (landingLauncher) landingLauncher.classList.add("hidden");
    } else {
        if (landingDismissed) {
            if (landingOverlay) landingOverlay.classList.add("hidden");
            if (landingLauncher) landingLauncher.classList.remove("hidden");
        } else {
            if (landingOverlay) landingOverlay.classList.remove("hidden");
            if (landingLauncher) landingLauncher.classList.add("hidden");
        }
        if (mobileLanding) mobileLanding.classList.remove("hidden");
    }
}

function promptIdentityShare() {
    if (window.Twitch?.ext?.actions) {
        window.Twitch.ext.actions.requestIdShare();
    }
}

// Geometry configuration
const isMobile = document.body.classList.contains("mobile-body");
let pivotX = isMobile ? 160 : 250;
let pivotY = isMobile ? 160 : 350;
const needleRadius = isMobile ? 110 : 160;

// Angle setter & needle updater
function setAngle(deg: number) {
    currentAngle = Math.max(0, Math.min(180, Math.round(deg)));

    if (valAngle) {
        valAngle.textContent = `${currentAngle}°`;
    }

    if (angleNeedle && needleHead) {
        const rad = (currentAngle * Math.PI) / 180;
        const targetX = (isMobile ? pivotX : 0) + Math.cos(rad) * needleRadius;
        const targetY = (isMobile ? pivotY : 0) - Math.sin(rad) * needleRadius;

        if (isMobile) {
            angleNeedle.setAttribute("x1", pivotX.toString());
            angleNeedle.setAttribute("y1", pivotY.toString());
        } else {
            angleNeedle.setAttribute("x1", "0");
            angleNeedle.setAttribute("y1", "0");
        }

        angleNeedle.setAttribute("x2", targetX.toFixed(1));
        angleNeedle.setAttribute("y2", targetY.toFixed(1));
        needleHead.setAttribute("cx", targetX.toFixed(1));
        needleHead.setAttribute("cy", targetY.toFixed(1));
    }
}

// Interactive Protractor Pointer Events
function initProtractorAiming() {
    if (!viewportSvg || !protractorHitArea) return;

    let isAiming = false;

    function computeAngle(clientX: number, clientY: number) {
        const pt = viewportSvg!.createSVGPoint();
        pt.x = clientX;
        pt.y = clientY;
        const ctm = viewportSvg!.getScreenCTM();
        if (!ctm) return;
        const svgPt = pt.matrixTransform(ctm.inverse());

        const dx = svgPt.x - pivotX;
        const dy = -(svgPt.y - pivotY); // Invert Y so up is positive

        const rad = Math.atan2(dy, dx);
        let deg = Math.round((rad * 180) / Math.PI);

        if (deg < 0) {
            deg = dx >= 0 ? 0 : 180;
        }

        setAngle(deg);
    }

    protractorHitArea.addEventListener("pointerdown", (e) => {
        e.preventDefault();
        isAiming = true;
        protractorHitArea.classList.add("active");
        try {
            protractorHitArea.setPointerCapture(e.pointerId);
        } catch (_) {}
        computeAngle(e.clientX, e.clientY);
    });

    protractorHitArea.addEventListener("pointermove", (e) => {
        if (!isAiming) return;
        e.preventDefault();
        computeAngle(e.clientX, e.clientY);
    });

    const endAiming = (e: PointerEvent) => {
        if (isAiming) {
            isAiming = false;
            protractorHitArea.classList.remove("active");
            try {
                protractorHitArea.releasePointerCapture(e.pointerId);
            } catch (_) {}
        }
    };

    protractorHitArea.addEventListener("pointerup", endAiming);
    protractorHitArea.addEventListener("pointercancel", endAiming);
}

// Power Setter & UI sync
function setPower(val: number) {
    currentPower = Math.max(1, Math.min(100, Math.round(val)));

    if (valPower) {
        valPower.textContent = `${currentPower}%`;
    }

    if (sliderPower && sliderPower.value !== String(currentPower)) {
        sliderPower.value = String(currentPower);
    }

    if (powerFillBar) {
        powerFillBar.style.height = `${currentPower}%`;
    }

    if (powerThumb) {
        powerThumb.style.bottom = `${currentPower}%`;
    }
}

// Vertical Power Gauge Pointer Events (prevents mobile horizontal swipe conflict)
function initVerticalPower() {
    if (!verticalPowerTrack) return;

    let isDragging = false;

    function computePower(clientY: number) {
        const rect = verticalPowerTrack!.getBoundingClientRect();
        if (rect.height <= 0) return;
        // Inverted: top is 100%, bottom is 0%
        const pct = ((rect.bottom - clientY) / rect.height) * 100;
        setPower(pct);
    }

    verticalPowerTrack.addEventListener("pointerdown", (e) => {
        e.preventDefault();
        isDragging = true;
        verticalPowerTrack.classList.add("active");
        try {
            verticalPowerTrack.setPointerCapture(e.pointerId);
        } catch (_) {}
        computePower(e.clientY);
    });

    verticalPowerTrack.addEventListener("pointermove", (e) => {
        if (!isDragging) return;
        e.preventDefault();
        computePower(e.clientY);
    });

    const endDrag = (e: PointerEvent) => {
        if (isDragging) {
            isDragging = false;
            verticalPowerTrack.classList.remove("active");
            try {
                verticalPowerTrack.releasePointerCapture(e.pointerId);
            } catch (_) {}
        }
    };

    verticalPowerTrack.addEventListener("pointerup", endDrag);
    verticalPowerTrack.addEventListener("pointercancel", endDrag);
}

// Power Slider listener (desktop overlay)
if (sliderPower) {
    sliderPower.addEventListener("input", (e) => {
        const val = parseInt((e.target as HTMLInputElement).value, 10);
        setPower(val);
    });
}

// Parse JWT manually to check broadcaster/moderator role
function getUserRole(token: string): string {
    if (isLocalDev || !token) {
        // Test UI always acts as the streamer (broadcaster)
        return "broadcaster";
    }
    try {
        const payloadStr = atob(token.split('.')[1]);
        const payload = JSON.parse(payloadStr);
        return payload.role || (isLocalDev ? "broadcaster" : "viewer");
    } catch (_) {
        return isLocalDev ? "broadcaster" : "viewer";
    }
}

function getIsPlayerJoined(): boolean {
    if (currentUsername) {
        if (joinedPlayersList.includes(currentUsername)) return true;
        if (hasJoined && joinRequestedAt > 0 && Date.now() - joinRequestedAt < 3000) return true;
        return false;
    }
    if (hasJoined && joinRequestedAt > 0 && Date.now() - joinRequestedAt < 3000) return true;
    if (isLocalDev) {
        return joinedPlayersList.length > 0;
    }
    return false;
}

function getIsPlayerDead(): boolean {
    if (currentPhaseStr === "IDLE") return false;
    if (!getIsPlayerJoined()) return false;
    if (currentUsername) {
        return !activePlayers.includes(currentUsername);
    }
    if (isPlayerDead) return true;
    if (isLocalDev) {
        if (joinedPlayersList.length > 0 && activePlayers.length === 0) {
            return true;
        }
    }
    return false;
}

// Local 1-second countdown timer for smooth synchronized UI
function startCountdownTimer() {
    if (countdownInterval) return;
    countdownInterval = window.setInterval(() => {
        if (currentPhaseStr === "INPUT" && localTimerRemaining > 0) {
            localTimerRemaining--;
            if (phaseBadge) {
                phaseBadge.textContent = isMobile ? `INPUT (${localTimerRemaining}s)` : "INPUT PHASE";
            }
            if (desktopTimer) {
                desktopTimer.textContent = `${localTimerRemaining}`;
                if (localTimerRemaining <= 5) {
                    desktopTimer.style.color = "#ff003c";
                    desktopTimer.style.textShadow = "0 0 15px #ff003c";
                } else {
                    desktopTimer.style.color = "#ffffff";
                    desktopTimer.style.textShadow = "0 0 10px #00ffcc";
                }
            }
        }
    }, 1000);
}

// UI Phase State Manager
function updateUIForPhase(phase: string, timerRemaining?: number, playersCount?: number, winner?: string) {
    const cleanPhase = (phase || "IDLE").toUpperCase();
    currentPhaseStr = cleanPhase;

    if (phaseBadge) {
        phaseBadge.className = `phase-badge ${cleanPhase.toLowerCase()}`;
        if (cleanPhase === "INPUT") {
            const displaySec = timerRemaining !== undefined ? timerRemaining : localTimerRemaining;
            phaseBadge.textContent = isMobile ? (displaySec > 0 ? `INPUT (${displaySec}s)` : "INPUT") : "INPUT PHASE";
            if (desktopTimer) {
                desktopTimer.textContent = `${displaySec}`;
                desktopTimer.classList.remove("hidden");
                if (displaySec <= 5) {
                    desktopTimer.style.color = "#ff003c";
                    desktopTimer.style.textShadow = "0 0 15px #ff003c";
                } else {
                    desktopTimer.style.color = "#ffffff";
                    desktopTimer.style.textShadow = "0 0 10px #00ffcc";
                }
            }
        } else {
            setCurrentAction("");
            if (desktopTimer) {
                desktopTimer.classList.add("hidden");
            }
            if (cleanPhase === "IDLE") {
                const countStr = playersCount !== undefined ? ` (${playersCount} joined)` : "";
                phaseBadge.textContent = isMobile ? `IDLE${countStr}` : `WAITING FOR PLAYERS${countStr}`;
            } else if (cleanPhase === "SIMULATION" || cleanPhase === "ACTION") {
                phaseBadge.textContent = "FIRING";
            } else if (cleanPhase === "ROUND_OVER" || cleanPhase === "CELEBRATION") {
                phaseBadge.textContent = "ROUND OVER";
            } else {
                phaseBadge.textContent = cleanPhase;
            }
        }
    }

    if (!isLinked) {
        setAimingVisible(false);
        if (adminControls) adminControls.classList.add("hidden");
        if (playerSetup) playerSetup.classList.add("hidden");
        if (playerControls) playerControls.classList.add("hidden");
        if (desktopTimer) desktopTimer.classList.add("hidden");
        if (statusMessage) statusMessage.classList.add("hidden");
        updateLandingVisibility();
        return;
    }

    updateLandingVisibility();

    const role = getUserRole(viewerToken);
    const isModOrBroadcaster = role === "broadcaster" || role === "moderator";
    const isJoined = getIsPlayerJoined();
    const isDead = isJoined && getIsPlayerDead();
    const canDeploy = !isJoined && canJoinGame;

    if (cleanPhase === "IDLE") {
        isPlayerDead = false;
        if (deadSkull) {
            deadSkull.classList.add("hidden");
            (deadSkull as HTMLElement).style.display = "none";
        }
        if (Date.now() < protractorPreviewUntil) {
            setAimingVisible(true);
        } else {
            setAimingVisible(false);
        }
        if (adminControls) {
            if (isModOrBroadcaster && canStartGame) {
                adminControls.classList.remove("hidden");
            } else {
                adminControls.classList.add("hidden");
            }
        }
        if (playerSetup) {
            if (canDeploy) {
                playerSetup.classList.remove("hidden");
            } else {
                playerSetup.classList.add("hidden");
            }
        }
        if (playerControls) {
            playerControls.classList.add("hidden");
        }
        if (statusMessage) statusMessage.classList.add("hidden");
    } else if (cleanPhase === "INPUT") {
        if (adminControls) adminControls.classList.add("hidden");
        if (isJoined) {
            if (isDead) {
                if (playerControls) playerControls.classList.add("hidden");
                if (playerSetup) playerSetup.classList.add("hidden");
                if (btnFire) (btnFire as HTMLButtonElement).disabled = true;

                if (protractorOverlayGroup) {
                    protractorOverlayGroup.classList.remove("hidden");
                    (protractorOverlayGroup as HTMLElement).style.display = "";
                    (protractorOverlayGroup as HTMLElement).style.visibility = "visible";
                    protractorOverlayGroup.setAttribute("visibility", "visible");
                }
                if (protractorHitArea) {
                    (protractorHitArea as HTMLElement).style.display = "none";
                    (protractorHitArea as HTMLElement).style.visibility = "hidden";
                }
                if (angleNeedle) {
                    (angleNeedle as HTMLElement).style.display = "none";
                    (angleNeedle as HTMLElement).style.visibility = "hidden";
                }
                if (needleHead) {
                    (needleHead as HTMLElement).style.display = "none";
                    (needleHead as HTMLElement).style.visibility = "hidden";
                }
                if (angleBadgeGroup) {
                    (angleBadgeGroup as HTMLElement).style.display = "none";
                    (angleBadgeGroup as HTMLElement).style.visibility = "hidden";
                }
                if (pivotCenter) {
                    (pivotCenter as HTMLElement).style.display = "none";
                    (pivotCenter as HTMLElement).style.visibility = "hidden";
                }
                if (mobileAngleBadge) {
                    mobileAngleBadge.style.display = "none";
                    mobileAngleBadge.style.visibility = "hidden";
                }

                if (deadSkull) {
                    deadSkull.classList.remove("hidden");
                    (deadSkull as HTMLElement).style.display = "";
                    (deadSkull as HTMLElement).style.visibility = "visible";
                }
                setLeaveButtonVisible(false);
                if (statusMessage) {
                    statusMessage.textContent = "ELIMINATED";
                    statusMessage.classList.remove("hidden");
                }
            } else if (isPlayerLeaving) {
                if (deadSkull) {
                    deadSkull.classList.add("hidden");
                    (deadSkull as HTMLElement).style.display = "none";
                }
                setAimingVisible(false);
                setLeaveButtonVisible(false);
                if (playerControls) playerControls.classList.add("hidden");
                if (playerSetup) playerSetup.classList.add("hidden");
                if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
                if (btnLeft) (btnLeft as HTMLButtonElement).disabled = true;
                if (btnRight) (btnRight as HTMLButtonElement).disabled = true;
                setCurrentAction("LEAVING AT END OF MATCH");
                if (statusMessage) {
                    statusMessage.textContent = "LEAVING AT END OF MATCH";
                    statusMessage.classList.remove("hidden");
                }
            } else {
                if (deadSkull) {
                    deadSkull.classList.add("hidden");
                    (deadSkull as HTMLElement).style.display = "none";
                }
                setAimingVisible(true);
                setLeaveButtonVisible(true);
                if (playerControls) playerControls.classList.remove("hidden");
                if (playerSetup) playerSetup.classList.add("hidden");
                if (btnFire) (btnFire as HTMLButtonElement).disabled = false;
                if (btnLeft) (btnLeft as HTMLButtonElement).disabled = false;
                if (btnRight) (btnRight as HTMLButtonElement).disabled = false;
                if (statusMessage) statusMessage.classList.add("hidden");
            }
        } else {
            setAimingVisible(false);
            setLeaveButtonVisible(false);
            if (deadSkull) {
                deadSkull.classList.add("hidden");
                (deadSkull as HTMLElement).style.display = "none";
            }
            if (playerControls) playerControls.classList.add("hidden");
            if (canDeploy) {
                if (playerSetup) playerSetup.classList.remove("hidden");
            } else {
                if (playerSetup) playerSetup.classList.add("hidden");
            }
            if (statusMessage) statusMessage.classList.add("hidden");
        }
    } else if (cleanPhase === "SIMULATION" || cleanPhase === "ACTION") {
        setAimingVisible(false);
        setLeaveButtonVisible(false);
        if (deadSkull) {
            deadSkull.classList.add("hidden");
            (deadSkull as HTMLElement).style.display = "none";
        }
        if (adminControls) adminControls.classList.add("hidden");
        if (playerSetup) playerSetup.classList.add("hidden");
        if (playerControls) playerControls.classList.add("hidden");
        if (statusMessage) {
            statusMessage.textContent = "CANNONS FIRING...";
            statusMessage.classList.remove("hidden");
        }
        if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
    } else if (cleanPhase === "ROUND_OVER" || cleanPhase === "CELEBRATION") {
        setAimingVisible(false);
        setLeaveButtonVisible(false);
        if (deadSkull) {
            deadSkull.classList.add("hidden");
            (deadSkull as HTMLElement).style.display = "none";
        }
        if (adminControls) adminControls.classList.add("hidden");
        if (playerSetup) playerSetup.classList.add("hidden");
        if (playerControls) playerControls.classList.add("hidden");
        if (statusMessage) {
            statusMessage.textContent = winner ? `WINNER: ${winner}` : "ROUND OVER";
            statusMessage.classList.remove("hidden");
        }
        if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
    }
}

// WebSocket Connection to C&C Relay or Local Game Instance
function connectWebSocket() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
        return;
    }

    startCountdownTimer();
    const isLocalDev = window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1";
    if (isLocalDev) {
        logMessage("Connecting to local StreamTanks server...");
        const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
        ws = new WebSocket(`${proto}//${window.location.host}/ws?client=extension`);
    } else {
        logMessage("Connecting to C&C...");
        ws = new WebSocket(`${CC_SERVER_URL}?token=${viewerToken}`);
    }

    ws.onopen = () => {
        logMessage("Connected!");

        // Send the initial auth payload expected by the server
        if (!isLocalDev) {
            ws!.send(JSON.stringify({ jwt: viewerToken }));
        }

        // Start heartbeat ping
        if (pingInterval) clearInterval(pingInterval);
        pingInterval = window.setInterval(() => {
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: "PING" }));
            }
        }, 45000);

        // Initial default phase
        updateUIForPhase("IDLE");

        // Re-send join if viewer deployed before identity refresh
        if (hasJoined) {
            sendCommand("%join");
        }
    };

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type === "VIEWER_INFO" && data.payload) {
                const userStr = data.payload.user ? String(data.payload.user).trim() : "";
                if (userStr && (!isOpaque(userStr) || isLocalDev)) {
                    currentUsername = userStr.toLowerCase();
                    isLinked = true;
                } else {
                    currentUsername = "";
                    isLinked = false;
                }
                updateLandingVisibility();
                updateUIForPhase(currentPhaseStr, localTimerRemaining);
            } else if (data.type === "AUTH_REQUIRED") {
                isLinked = false;
                landingDismissed = false;
                updateLandingVisibility();
                logMessage(data.payload || "Twitch identity link required.");
            } else if ((data.type === "GAME_STATE" || data.type === "STATE_UPDATE") && data.payload) {
                const payload = data.payload;
                const phase = payload.phase;
                const timerRemaining = payload.timer_remaining ?? payload.timerRemaining;
                const playersCount = payload.players_count ?? (payload.players ? Object.keys(payload.players).length : 0);
                const winner = payload.winner;

                if (payload.can_start !== undefined) {
                    canStartGame = !!payload.can_start;
                } else if (payload.canStart !== undefined) {
                    canStartGame = !!payload.canStart;
                } else {
                    canStartGame = false;
                }

                if (payload.can_join !== undefined) {
                    canJoinGame = !!payload.can_join;
                } else if (payload.canJoin !== undefined) {
                    canJoinGame = !!payload.canJoin;
                } else {
                    canJoinGame = phase === "IDLE";
                }

                if (Array.isArray(payload.joined_players)) {
                    joinedPlayersList = payload.joined_players.map((p: string) => String(p).toLowerCase());
                } else if (Array.isArray(payload.joinedPlayers)) {
                    joinedPlayersList = payload.joinedPlayers.map((p: string) => String(p).toLowerCase());
                } else {
                    joinedPlayersList = [];
                }

                if (Array.isArray(payload.players)) {
                    activePlayers = payload.players.map((p: string) => String(p).toLowerCase());
                } else if (payload.players && typeof payload.players === 'object') {
                    activePlayers = Object.values(payload.players)
                        .filter((p: any) => p && !p.isBot && !p.isDead && (phase !== "IDLE" || p.joined))
                        .map((p: any) => String(p.name || "").toLowerCase());

                    if (joinedPlayersList.length === 0) {
                        joinedPlayersList = Object.values(payload.players)
                            .filter((p: any) => p && !p.isBot && p.joined)
                            .map((p: any) => String(p.name || "").toLowerCase());
                    }
                }

                if (isLocalDev && !currentUsername) {
                    if (joinedPlayersList.length > 0) {
                        currentUsername = joinedPlayersList[0];
                    } else if (activePlayers.length > 0) {
                        currentUsername = activePlayers[0];
                    }
                }

                if (timerRemaining !== undefined) {
                    localTimerRemaining = timerRemaining;
                }

                if (!isMobile) {
                    const px = payload.protractor_x ?? payload.protractorX;
                    const py = payload.protractor_y ?? payload.protractorY;
                    let posChanged = false;
                    if (typeof px === 'number') {
                        if (lastProtractorX !== px) posChanged = true;
                        pivotX = px;
                        lastProtractorX = px;
                    }
                    if (typeof py === 'number') {
                        if (lastProtractorY !== py) posChanged = true;
                        pivotY = py;
                        lastProtractorY = py;
                    }
                    if (protractorOverlayGroup) {
                        protractorOverlayGroup.setAttribute("transform", `translate(${pivotX}, ${pivotY})`);
                    }
                    if (posChanged) {
                        setAngle(currentAngle);
                        showProtractorPreview(2500);
                    }
                }

                if (Array.isArray(payload.leaving_players)) {
                    leavingPlayersList = payload.leaving_players.map((p: string) => String(p).toLowerCase());
                } else if (Array.isArray(payload.leavingPlayers)) {
                    leavingPlayersList = payload.leavingPlayers.map((p: string) => String(p).toLowerCase());
                } else if (payload.players && typeof payload.players === 'object') {
                    leavingPlayersList = Object.values(payload.players)
                        .filter((p: any) => p && p.leaving)
                        .map((p: any) => String(p.name || "").toLowerCase());
                } else {
                    leavingPlayersList = [];
                }

                if (currentUsername) {
                    isPlayerLeaving = leavingPlayersList.includes(currentUsername);
                } else if (isLocalDev) {
                    isPlayerLeaving = leavingPlayersList.length > 0;
                } else {
                    isPlayerLeaving = false;
                }

                const currentlyJoined = getIsPlayerJoined();
                hasJoined = currentlyJoined;
                if (currentUsername && joinedPlayersList.includes(currentUsername)) {
                    joinRequestedAt = 0;
                }

                if (phase === "IDLE") {
                    isPlayerDead = false;
                    isPlayerLeaving = false;
                } else {
                    if (currentlyJoined) {
                        if (currentUsername) {
                            isPlayerDead = !activePlayers.includes(currentUsername);
                        } else if (isLocalDev) {
                            if (payload.players && typeof payload.players === 'object') {
                                const humans = Object.values(payload.players).filter((p: any) => p && !p.isBot && p.joined);
                                if (humans.length > 0 && humans.every((p: any) => p.isDead)) {
                                    isPlayerDead = true;
                                } else if (humans.some((p: any) => !p.isDead)) {
                                    isPlayerDead = false;
                                }
                            } else if (joinedPlayersList.length > 0 && activePlayers.length === 0) {
                                isPlayerDead = true;
                            } else if (activePlayers.length > 0) {
                                isPlayerDead = false;
                            }
                        }
                    } else {
                        isPlayerDead = false;
                        isPlayerLeaving = false;
                    }
                }

                updateUIForPhase(phase, timerRemaining, playersCount, winner);
            } else if (data.type === "PLAYER_DIED" && data.payload) {
                const victim = String(data.payload.victim || "").toLowerCase();
                if (currentUsername && victim === currentUsername) {
                    isPlayerDead = true;
                    updateUIForPhase(currentPhaseStr);
                } else if (isLocalDev && (!currentUsername || victim === currentUsername)) {
                    isPlayerDead = true;
                    updateUIForPhase(currentPhaseStr);
                }
            }
        } catch (e) {
            console.error("Failed to parse WebSocket message:", e);
        }
    };

    ws.onclose = () => {
        if (pingInterval) {
            clearInterval(pingInterval);
            pingInterval = null;
        }

        logMessage("Disconnected. Reconnecting...");
        setTimeout(connectWebSocket, 3000);
    };

    ws.onerror = (err) => {
        console.error("WebSocket error:", err);
        ws?.close();
    };
}

function sendCommand(cmd: string) {
    if (!isLinked) {
        logMessage("Twitch account link required to play.");
        promptIdentityShare();
        return;
    }
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        logMessage("Error: Not connected.");
        return;
    }

    const payload = {
        type: "CHAT_COMMAND",
        payload: cmd
    };
    ws.send(JSON.stringify(payload));
}

function logMessage(msg: string) {
    if (!msgLog) return;
    msgLog.textContent = msg;
    setTimeout(() => {
        if (msgLog && msgLog.textContent === msg) {
            msgLog.textContent = "";
        }
    }, 4000);
}

// Landing Page Event Listeners
btnConnectTwitch?.addEventListener("click", promptIdentityShare);
btnConnectTwitchMobile?.addEventListener("click", promptIdentityShare);

btnDismissLanding?.addEventListener("click", () => {
    landingDismissed = true;
    updateLandingVisibility();
});

btnCloseLanding?.addEventListener("click", () => {
    landingDismissed = true;
    updateLandingVisibility();
});

btnOpenLanding?.addEventListener("click", () => {
    landingDismissed = false;
    updateLandingVisibility();
});

// Button Click Event Listeners
btnStartMatch?.addEventListener("click", () => {
    if (!canStartGame) return;
    sendCommand("%startgame");
    logMessage("Match starting...");
});

btnJoin?.addEventListener("click", () => {
    if (!isLinked) {
        promptIdentityShare();
        return;
    }
    const isJoined = getIsPlayerJoined();
    if (isJoined || !canJoinGame) return;
    sendCommand("%join");
    hasJoined = true;
    joinRequestedAt = Date.now();
    logMessage("Tank deployed!");
    if (playerSetup) playerSetup.classList.add("hidden");
    if (currentPhaseStr === "INPUT") {
        if (playerControls) playerControls.classList.remove("hidden");
        setAimingVisible(true);
    }
});

btnLeft?.addEventListener("click", () => {
    sendCommand("%left");
    setCurrentAction("LOCKED: MOVE LEFT");
    logMessage("Moving left...");
});

btnRight?.addEventListener("click", () => {
    sendCommand("%right");
    setCurrentAction("LOCKED: MOVE RIGHT");
    logMessage("Moving right...");
});

btnFire?.addEventListener("click", () => {
    sendCommand(`%fire ${currentAngle} ${currentPower}`);
    setCurrentAction(`LOCKED: FIRE ${currentAngle}° @ ${currentPower}%`);
    logMessage(`Fired: ${currentAngle}° @ ${currentPower}%`);
});

btnLeave?.addEventListener("click", (e) => {
    e.stopPropagation();
    e.preventDefault();
    sendCommand("%leave");
    setCurrentAction("LEAVING AT END OF MATCH");
    logMessage("Leaving match...");
    setLeaveButtonVisible(false);
});

// Twitch Helper Initialization
if (isLocalDev || !window.Twitch || !window.Twitch.ext) {
    // Standalone local preview mode or opened directly in browser
    console.log("Running in standalone/local preview mode.");
    isLinked = true;
    updateLandingVisibility();
    connectWebSocket();
}

if (window.Twitch && window.Twitch.ext) {
    window.Twitch.ext.onAuthorized((auth: any) => {
        const tokenChanged = viewerToken !== "" && viewerToken !== auth.token;
        viewerToken = auth.token;
        const previouslyLinked = isLinked;
        isLinked = checkIdentityLinked(viewerToken);

        updateLandingVisibility();

        if (tokenChanged && ws) {
            // Reconnect WebSocket so C&C server re-authenticates with new identity token
            ws.close();
        } else if (!ws || ws.readyState === WebSocket.CLOSED) {
            connectWebSocket();
        }

        if (!previouslyLinked && isLinked) {
            logMessage("Twitch account connected!");
        }
    });
}

// Initialize Aiming & Initial Values
initProtractorAiming();
setAngle(45);
initVerticalPower();
setPower(100);
setAimingVisible(false);
updateLandingVisibility();
