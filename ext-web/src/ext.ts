// Twitch Extension Frontend Logic (Desktop Protractor Overlay & Mobile UI)
// Connects to the C&C WebSocket server to relay commands from viewer to StreamTanks.

declare global {
    interface Window {
        Twitch: any;
    }
}

const CC_SERVER_URL = "wss://st-cc.poundsigndesign.com/ws/viewer";

let ws: WebSocket | null = null;
let viewerToken: string = "";
let hasJoined: boolean = false;
let currentAngle: number = 45;
let currentPower: number = 100;
let pingInterval: number | null = null;

// DOM Element Selectors
const viewportSvg = document.getElementById("viewport-svg") as SVGSVGElement | null;
const protractorHitArea = document.getElementById("protractor-hit-area");
const angleNeedle = document.getElementById("angle-needle");
const needleHead = document.getElementById("needle-head");
const valAngle = document.getElementById("val-angle");

const sliderPower = document.getElementById("slider-power") as HTMLInputElement | null;
const valPower = document.getElementById("val-power");
const phaseBadge = document.getElementById("phase-badge");

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

// Geometry configuration
const isMobile = document.body.classList.contains("mobile-body");
const pivotX = isMobile ? 160 : 250;
const pivotY = isMobile ? 160 : 250;
const needleRadius = isMobile ? 110 : 160;

// Angle setter & needle updater
function setAngle(deg: number) {
    currentAngle = Math.max(0, Math.min(180, Math.round(deg)));

    if (valAngle) {
        valAngle.textContent = `${currentAngle}°`;
    }

    if (angleNeedle && needleHead) {
        const rad = (currentAngle * Math.PI) / 180;
        const targetX = pivotX + Math.cos(rad) * needleRadius;
        const targetY = pivotY - Math.sin(rad) * needleRadius;

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
        isAiming = true;
        protractorHitArea!.classList.add("active");
        try {
            protractorHitArea!.setPointerCapture(e.pointerId);
        } catch (_) {}
        computeAngle(e.clientX, e.clientY);
    });

    protractorHitArea.addEventListener("pointermove", (e) => {
        if (!isAiming) return;
        computeAngle(e.clientX, e.clientY);
    });

    const stopAiming = (e: PointerEvent) => {
        if (!isAiming) return;
        isAiming = false;
        protractorHitArea!.classList.remove("active");
        try {
            protractorHitArea!.releasePointerCapture(e.pointerId);
        } catch (_) {}
    };

    protractorHitArea.addEventListener("pointerup", stopAiming);
    protractorHitArea.addEventListener("pointercancel", stopAiming);
}

// Power Slider listener
if (sliderPower && valPower) {
    sliderPower.addEventListener("input", (e) => {
        currentPower = parseInt((e.target as HTMLInputElement).value, 10);
        valPower.textContent = `${currentPower}%`;
    });
}

// Parse JWT manually to check broadcaster/moderator role
function getUserRole(token: string): string {
    try {
        const payloadStr = atob(token.split('.')[1]);
        const payload = JSON.parse(payloadStr);
        return payload.role || "viewer";
    } catch (_) {
        return "viewer";
    }
}

// UI Phase State Manager
function updateUIForPhase(phase: string, timerRemaining?: number, playersCount?: number, winner?: string) {
    const cleanPhase = (phase || "IDLE").toUpperCase();

    if (phaseBadge) {
        phaseBadge.className = `phase-badge ${cleanPhase.toLowerCase()}`;
        if (cleanPhase === "INPUT" && timerRemaining !== undefined && timerRemaining > 0) {
            phaseBadge.textContent = `INPUT (${timerRemaining}s)`;
        } else if (cleanPhase === "IDLE") {
            const countStr = playersCount !== undefined ? ` (${playersCount} joined)` : "";
            phaseBadge.textContent = `IDLE${countStr}`;
        } else if (cleanPhase === "SIMULATION" || cleanPhase === "ACTION") {
            phaseBadge.textContent = "FIRING";
        } else if (cleanPhase === "ROUND_OVER" || cleanPhase === "CELEBRATION") {
            phaseBadge.textContent = "ROUND OVER";
        } else {
            phaseBadge.textContent = cleanPhase;
        }
    }

    const role = getUserRole(viewerToken);
    const isModOrBroadcaster = role === "broadcaster" || role === "moderator";

    if (cleanPhase === "IDLE") {
        if (adminControls && isModOrBroadcaster) adminControls.classList.remove("hidden");
        if (playerSetup && !hasJoined) playerSetup.classList.remove("hidden");
        if (playerControls) playerControls.classList.add("hidden");
        if (statusMessage) statusMessage.classList.add("hidden");
    } else if (cleanPhase === "INPUT") {
        if (adminControls) adminControls.classList.add("hidden");
        if (hasJoined && playerControls) playerControls.classList.remove("hidden");
        if (playerSetup && !hasJoined) playerSetup.classList.remove("hidden");
        if (statusMessage) statusMessage.classList.add("hidden");
        if (btnFire) (btnFire as HTMLButtonElement).disabled = false;
    } else if (cleanPhase === "SIMULATION" || cleanPhase === "ACTION") {
        if (adminControls) adminControls.classList.add("hidden");
        if (statusMessage) {
            statusMessage.textContent = "CANNONS FIRING...";
            statusMessage.classList.remove("hidden");
        }
        if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
    } else if (cleanPhase === "ROUND_OVER" || cleanPhase === "CELEBRATION") {
        if (statusMessage) {
            statusMessage.textContent = winner ? `WINNER: ${winner}` : "ROUND OVER";
            statusMessage.classList.remove("hidden");
        }
        if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
    }
}

// WebSocket Connection to C&C Relay
function connectWebSocket() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
        return;
    }

    logMessage("Connecting to C&C...");
    ws = new WebSocket(`${CC_SERVER_URL}?token=${viewerToken}`);

    ws.onopen = () => {
        logMessage("Connected!");

        // Send the initial auth payload expected by the server
        ws!.send(JSON.stringify({ jwt: viewerToken }));

        // Start heartbeat ping
        if (pingInterval) clearInterval(pingInterval);
        pingInterval = window.setInterval(() => {
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: "PING" }));
            }
        }, 45000);

        // Initial default phase
        updateUIForPhase("IDLE");
    };

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type === "GAME_STATE" && data.payload) {
                const phase = data.payload.phase;
                const timerRemaining = data.payload.timer_remaining;
                const playersCount = data.payload.players_count;
                const winner = data.payload.winner;

                updateUIForPhase(phase, timerRemaining, playersCount, winner);
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

// Button Click Event Listeners
btnStartMatch?.addEventListener("click", () => {
    sendCommand("%startgame");
    logMessage("Match starting...");
});

btnJoin?.addEventListener("click", () => {
    sendCommand("%join");
    hasJoined = true;
    logMessage("Tank deployed!");
    if (playerSetup) playerSetup.classList.add("hidden");
    if (playerControls) playerControls.classList.remove("hidden");
});

btnLeft?.addEventListener("click", () => {
    sendCommand("%left");
    logMessage("Moving left...");
});

btnRight?.addEventListener("click", () => {
    sendCommand("%right");
    logMessage("Moving right...");
});

btnFire?.addEventListener("click", () => {
    sendCommand(`%fire ${currentAngle} ${currentPower}`);
    logMessage(`Fired: ${currentAngle}° @ ${currentPower}%`);
});

// Twitch Helper Initialization
if (window.Twitch && window.Twitch.ext) {
    window.Twitch.ext.onAuthorized((auth: any) => {
        viewerToken = auth.token;
        connectWebSocket();
    });
} else {
    // Local preview mode without Twitch extension iframe
    console.log("Twitch helper not detected; running in standalone test mode.");
    connectWebSocket();
}

// Initialize Protractor & Initial Angle
initProtractorAiming();
setAngle(45);
