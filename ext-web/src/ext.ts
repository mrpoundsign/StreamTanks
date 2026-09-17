// Twitch Extension Frontend Logic
// Connects to the C&C WebSocket server to relay commands from the viewer to the local game instance.

declare global {
    interface Window {
        Twitch: any;
    }
}

const CC_SERVER_URL = "wss://st-cc.poundsigndesign.com/ws/viewer";

let ws: WebSocket | null = null;
let viewerToken: string = "";
let hasJoined: boolean = false;

// DOM Elements
const appContainer = document.getElementById("app")!;
const btnToggleMin = document.getElementById("btn-toggle-min")!;
const phaseIndicator = document.getElementById("phase-indicator")!;
const statusIndicator = document.getElementById("status-indicator")!;
const adminControls = document.getElementById("admin-controls")!;
const playerSetup = document.getElementById("player-setup")!;
const playerControls = document.getElementById("player-controls")!;
const msgLog = document.getElementById("message-log")!;
const btnStartMatch = document.getElementById("btn-start-match")!;

// Sliders
const sliderAngle = document.getElementById("slider-angle") as HTMLInputElement;
const valAngle = document.getElementById("val-angle")!;
const sliderPower = document.getElementById("slider-power") as HTMLInputElement;
const valPower = document.getElementById("val-power")!;

// Update slider labels dynamically
sliderAngle.addEventListener("input", (e) => {
    valAngle.innerHTML = `${(e.target as HTMLInputElement).value}&deg;`;
});
sliderPower.addEventListener("input", (e) => {
    valPower.innerHTML = `${(e.target as HTMLInputElement).value}%`;
});

// Twitch Ext Authentication
window.Twitch.ext.onAuthorized((auth: any) => {
    console.log("Twitch Extension Authorized!");
    viewerToken = auth.token;
    
    // Connect to WebSocket server once we have the JWT
    connectWebSocket();
});

// Twitch Ext Context (to check roles)
window.Twitch.ext.onContext((context: any, changed: string[]) => {
    // Context tells us about the stream state, theme, etc.
});

// Parse JWT manually to check role if needed (Twitch Extension JWT payload contains 'role')
function getUserRole(token: string): string {
    try {
        const payloadStr = atob(token.split('.')[1]);
        const payload = JSON.parse(payloadStr);
        return payload.role;
    } catch (e) {
        return "viewer";
    }
}

let pingInterval: number | null = null;

function connectWebSocket() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
        return;
    }

    logMessage("Connecting...");
    ws = new WebSocket(`${CC_SERVER_URL}?token=${viewerToken}`);

    ws.onopen = () => {
        logMessage("Connected!");
        statusIndicator.classList.remove("offline");
        statusIndicator.classList.add("online");
        statusIndicator.querySelector(".text")!.textContent = "ONLINE";

        // Send the initial auth payload expected by the server
        ws!.send(JSON.stringify({ jwt: viewerToken }));
        
        // Start heartbeat to prevent Cloudflare/Proxy idle timeouts
        if (pingInterval) clearInterval(pingInterval);
        pingInterval = window.setInterval(() => {
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: "PING" }));
            }
        }, 45000);

        // Show appropriate UI
        const role = getUserRole(viewerToken);
        if (role === "broadcaster" || role === "moderator") {
            adminControls.classList.remove("hidden");
        }

        if (!hasJoined) {
            playerSetup.classList.remove("hidden");
        } else {
            playerControls.classList.remove("hidden");
        }
    };

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type === "GAME_STATE" && data.payload) {
                const phase = data.payload.phase;
                const timerRemaining = data.payload.timer_remaining;
                const playersCount = data.payload.players_count;

                if (phase) {
                    if (phase === "INPUT" && timerRemaining !== undefined && timerRemaining > 0) {
                        phaseIndicator.textContent = `PHASE: ${phase} (${timerRemaining}s)`;
                    } else if (playersCount !== undefined && phase === "IDLE") {
                        phaseIndicator.textContent = `PHASE: ${phase} (${playersCount} joined)`;
                    } else {
                        phaseIndicator.textContent = `PHASE: ${phase}`;
                    }
                }
                
                // Only show Start Game when IDLE
                if (phase !== "IDLE") {
                    btnStartMatch.style.display = "none";
                } else {
                    btnStartMatch.style.display = "block";
                }
            }
        } catch (e) {
            console.error("Failed to parse ws message", e);
        }
    };

    ws.onclose = () => {
        if (pingInterval) {
            clearInterval(pingInterval);
            pingInterval = null;
        }
        
        logMessage("Disconnected. Reconnecting in 3s...");
        statusIndicator.classList.remove("online");
        statusIndicator.classList.add("offline");
        statusIndicator.querySelector(".text")!.textContent = "OFFLINE";
        
        // Hide panels when disconnected
        adminControls.classList.add("hidden");
        playerSetup.classList.add("hidden");
        playerControls.classList.add("hidden");

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
    msgLog.textContent = msg;
    setTimeout(() => {
        if (msgLog.textContent === msg) {
            msgLog.textContent = "";
        }
    }, 5000);
}

function minimizePanel() {
    appContainer.classList.add("minimized");
    btnToggleMin.textContent = "+";
}

function maximizePanel() {
    appContainer.classList.remove("minimized");
    btnToggleMin.textContent = "_";
}

btnToggleMin.addEventListener("click", () => {
    if (appContainer.classList.contains("minimized")) {
        maximizePanel();
    } else {
        minimizePanel();
    }
});

// Button Bindings
document.getElementById("btn-start-match")?.addEventListener("click", () => {
    sendCommand("%startgame");
    logMessage("Sent: %startgame");
});

document.getElementById("btn-join")?.addEventListener("click", () => {
    // Send join command
    sendCommand("%join");
    logMessage("Deploying Tank...");
    
    // Swap UI to player controls
    hasJoined = true;
    playerSetup.classList.add("hidden");
    playerControls.classList.remove("hidden");
});

document.getElementById("btn-left")?.addEventListener("click", () => {
    sendCommand("%left");
});

document.getElementById("btn-right")?.addEventListener("click", () => {
    sendCommand("%right");
});

document.getElementById("btn-fire")?.addEventListener("click", () => {
    const angle = sliderAngle.value;
    const power = sliderPower.value;
    sendCommand(`%fire ${angle} ${power}`);
    logMessage(`Fired: A:${angle} P:${power}`);
    
    // Auto minimize after firing so they can watch the action
    minimizePanel();
});
