// Twitch Extension Frontend Logic (Desktop Protractor Overlay & Mobile UI)
// Connects to the C&C WebSocket server to relay commands from viewer to StreamTanks.

import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import {
    ViewerServerMessageSchema,
    ViewerActionMessageSchema,
    MoveAction_Direction,
    type TankState
} from "./proto/streamtanks/v1/game_pb";

const CC_SERVER_URL = "wss://st-cc.poundsigndesign.com/ws/viewer";

export let latestTerrain: number[] = [];
export let latestTanks: TankState[] = [];
export function getLatestTerrain(): number[] { return latestTerrain; }
export function getLatestTanks(): TankState[] { return latestTanks; }
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
let shieldUsedPlayersList: string[] = [];
let shieldedPlayersList: string[] = [];
let isShieldUsed: boolean = false;
let isShieldActive: boolean = false;
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
        setShieldButtonVisible(false);
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
const btnShield = document.getElementById("btn-shield") as HTMLButtonElement | null;

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

function setShieldButtonVisible(visible: boolean) {
    const shieldButtons = document.querySelectorAll<HTMLElement>("#btn-shield, .shield-btn");
    shieldButtons.forEach(btn => {
        if (visible) {
            btn.classList.remove("hidden");
            btn.style.display = "";
            btn.style.visibility = "visible";
        } else {
            btn.classList.add("hidden");
            btn.style.display = "none";
            btn.style.visibility = "hidden";
        }
    });
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
                setShieldButtonVisible(false);

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
                setShieldButtonVisible(false);
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
                if (isShieldUsed || isShieldActive) {
                    // Remove shield button when player has no shields to use
                    setShieldButtonVisible(false);
                    if (isShieldActive) {
                        if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
                        if (btnLeft) (btnLeft as HTMLButtonElement).disabled = true;
                        if (btnRight) (btnRight as HTMLButtonElement).disabled = true;
                    }
                } else {
                    setShieldButtonVisible(true);
                    if (btnShield) {
                        btnShield.disabled = false;
                        btnShield.textContent = "🛡️ ACTIVATE SHIELD (1/1)";
                        btnShield.classList.remove("shield-active");
                    }
                }
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
        setShieldButtonVisible(false);
    } else if (cleanPhase === "ROUND_OVER" || cleanPhase === "CELEBRATION") {
        setAimingVisible(false);
        setLeaveButtonVisible(false);
        setShieldButtonVisible(false);
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

function handleViewerStateUpdate(payload: any) {
    const phase = String(payload.phase || "IDLE").toUpperCase();
    const timerRemaining = payload.timerRemaining !== undefined ? payload.timerRemaining : payload.timer_remaining;
    const playersCount = payload.playersCount !== undefined ? payload.playersCount : (payload.players_count ?? (Array.isArray(payload.players) ? payload.players.length : 0));
    const winner = payload.winner || "";

    if (payload.terrain && Array.isArray(payload.terrain) && payload.terrain.length > 0) {
        latestTerrain = payload.terrain;
    }
    if (payload.tanks && Array.isArray(payload.tanks) && payload.tanks.length > 0) {
        latestTanks = payload.tanks;
    }

    if (payload.canStart !== undefined) {
        canStartGame = !!payload.canStart;
    } else if (payload.can_start !== undefined) {
        canStartGame = !!payload.can_start;
    } else {
        canStartGame = false;
    }

    if (payload.canJoin !== undefined) {
        canJoinGame = !!payload.canJoin;
    } else if (payload.can_join !== undefined) {
        canJoinGame = !!payload.can_join;
    } else {
        canJoinGame = phase === "IDLE";
    }

    const rawJoined = payload.joinedPlayers ?? payload.joined_players;
    if (Array.isArray(rawJoined)) {
        joinedPlayersList = rawJoined.map((p: string) => String(p).toLowerCase());
    } else {
        joinedPlayersList = [];
    }

    const rawPlayers = payload.players;
    if (Array.isArray(rawPlayers)) {
        activePlayers = rawPlayers.map((p: any) => typeof p === 'string' ? p.toLowerCase() : String(p.name || p.username || "").toLowerCase());
    } else if (rawPlayers && typeof rawPlayers === 'object') {
        activePlayers = Object.values(rawPlayers)
            .filter((p: any) => p && !p.isBot && !p.isDead && (phase !== "IDLE" || p.joined))
            .map((p: any) => String(p.name || p.username || "").toLowerCase());

        if (joinedPlayersList.length === 0) {
            joinedPlayersList = Object.values(rawPlayers)
                .filter((p: any) => p && !p.isBot && p.joined)
                .map((p: any) => String(p.name || p.username || "").toLowerCase());
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
        const px = payload.protractorX ?? payload.protractor_x;
        const py = payload.protractorY ?? payload.protractor_y;
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

    const rawLeaving = payload.leavingPlayers ?? payload.leaving_players;
    if (Array.isArray(rawLeaving)) {
        leavingPlayersList = rawLeaving.map((p: string) => String(p).toLowerCase());
    } else if (payload.players && typeof payload.players === 'object') {
        leavingPlayersList = Object.values(payload.players)
            .filter((p: any) => p && p.leaving)
            .map((p: any) => String(p.name || p.username || "").toLowerCase());
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

    const rawShieldUsed = payload.shieldUsedPlayers ?? payload.shield_used_players;
    if (Array.isArray(rawShieldUsed)) {
        shieldUsedPlayersList = rawShieldUsed.map((p: string) => String(p).toLowerCase());
    } else if (payload.players && typeof payload.players === 'object') {
        shieldUsedPlayersList = Object.values(payload.players)
            .filter((p: any) => p && p.shieldUsed)
            .map((p: any) => String(p.name || p.username || "").toLowerCase());
    } else {
        shieldUsedPlayersList = [];
    }

    const rawShielded = payload.shieldedPlayers ?? payload.shielded_players;
    if (Array.isArray(rawShielded)) {
        shieldedPlayersList = rawShielded.map((p: string) => String(p).toLowerCase());
    } else if (payload.players && typeof payload.players === 'object') {
        shieldedPlayersList = Object.values(payload.players)
            .filter((p: any) => p && p.isShielded)
            .map((p: any) => String(p.name || p.username || "").toLowerCase());
    } else {
        shieldedPlayersList = [];
    }

    if (currentUsername) {
        isShieldUsed = shieldUsedPlayersList.includes(currentUsername);
        isShieldActive = shieldedPlayersList.includes(currentUsername);
    } else if (isLocalDev && joinedPlayersList.length > 0) {
        const u = joinedPlayersList[0];
        isShieldUsed = shieldUsedPlayersList.includes(u);
        isShieldActive = shieldedPlayersList.includes(u);
    } else {
        isShieldUsed = false;
        isShieldActive = false;
    }

    const currentlyJoined = getIsPlayerJoined();
    hasJoined = currentlyJoined;
    if (currentUsername && joinedPlayersList.includes(currentUsername)) {
        joinRequestedAt = 0;
    }

    if (phase === "IDLE") {
        isPlayerDead = false;
        isPlayerLeaving = false;
        isShieldUsed = false;
        isShieldActive = false;
    } else {
        if (currentlyJoined) {
            if (currentUsername) {
                isPlayerDead = !activePlayers.includes(currentUsername);
            } else if (isLocalDev) {
                if (payload.players && typeof payload.players === 'object' && !Array.isArray(payload.players)) {
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
        ws = new WebSocket(`${CC_SERVER_URL}?token=${viewerToken}&format=proto`);
        ws.binaryType = "arraybuffer";
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
            sendAction({ join: {} });
        }
    };

    ws.onmessage = (event) => {
        try {
            if (event.data instanceof ArrayBuffer) {
                const serverMsg = fromBinary(ViewerServerMessageSchema, new Uint8Array(event.data));
                if (serverMsg.payload.case === "context") {
                    const ctx = serverMsg.payload.value;
                    const userStr = ctx.username ? String(ctx.username).trim() : "";
                    if (userStr && (!isOpaque(userStr) || isLocalDev)) {
                        currentUsername = userStr.toLowerCase();
                        isLinked = true;
                    } else {
                        currentUsername = "";
                        isLinked = false;
                    }
                    updateLandingVisibility();
                    updateUIForPhase(currentPhaseStr, localTimerRemaining);
                } else if (serverMsg.payload.case === "state") {
                    handleViewerStateUpdate(serverMsg.payload.value);
                }
                return;
            }

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
                handleViewerStateUpdate(data.payload);
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

function sendAction(actionData: {
    fire?: { angle: number; power: number };
    move?: { direction: MoveAction_Direction };
    shield?: {};
    join?: { emote?: string };
    leave?: {};
    startMatch?: {};
}) {
    if (!isLinked) {
        logMessage("Twitch account link required to play.");
        promptIdentityShare();
        return;
    }
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        logMessage("Error: Not connected.");
        return;
    }

    let actionOneOf: any = { case: undefined };
    let fallbackCmd = "";
    if (actionData.fire) {
        actionOneOf = { case: "fire", value: actionData.fire };
        fallbackCmd = `%fire ${actionData.fire.angle} ${actionData.fire.power}`;
    } else if (actionData.move) {
        actionOneOf = { case: "move", value: actionData.move };
        fallbackCmd = actionData.move.direction === MoveAction_Direction.LEFT ? "%left" : "%right";
    } else if (actionData.shield) {
        actionOneOf = { case: "shield", value: {} };
        fallbackCmd = "%shield";
    } else if (actionData.join) {
        actionOneOf = { case: "join", value: { emote: actionData.join.emote || "" } };
        fallbackCmd = actionData.join.emote ? `%join ${actionData.join.emote}` : "%join";
    } else if (actionData.leave) {
        actionOneOf = { case: "leave", value: {} };
        fallbackCmd = "%leave";
    } else if (actionData.startMatch) {
        actionOneOf = { case: "startMatch", value: {} };
        fallbackCmd = "%startgame";
    }

    if (isLocalDev || ws.binaryType !== "arraybuffer") {
        sendCommand(fallbackCmd);
        return;
    }

    try {
        const actionMsg = create(ViewerActionMessageSchema, {
            action: actionOneOf
        });
        const bytes = toBinary(ViewerActionMessageSchema, actionMsg);
        ws.send(bytes);
    } catch (err) {
        console.error("Failed to serialize protobuf action, falling back to JSON:", err);
        sendCommand(fallbackCmd);
    }
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
    sendAction({ startMatch: {} });
    logMessage("Match starting...");
});

btnJoin?.addEventListener("click", () => {
    if (!isLinked) {
        promptIdentityShare();
        return;
    }
    const isJoined = getIsPlayerJoined();
    if (isJoined || !canJoinGame) return;
    sendAction({ join: {} });
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
    sendAction({ move: { direction: MoveAction_Direction.LEFT } });
    setCurrentAction("LOCKED: MOVE LEFT");
    logMessage("Moving left...");
});

btnRight?.addEventListener("click", () => {
    sendAction({ move: { direction: MoveAction_Direction.RIGHT } });
    setCurrentAction("LOCKED: MOVE RIGHT");
    logMessage("Moving right...");
});

btnFire?.addEventListener("click", () => {
    sendAction({ fire: { angle: currentAngle, power: currentPower } });
    setCurrentAction(`LOCKED: FIRE ${currentAngle}° @ ${currentPower}%`);
    logMessage(`Fired: ${currentAngle}° @ ${currentPower}%`);
});

btnShield?.addEventListener("click", () => {
    sendAction({ shield: {} });
    isShieldActive = true;
    isShieldUsed = true;
    setCurrentAction("LOCKED: SHIELD ACTIVATED");
    logMessage("Shield activated! Invulnerable this round.");
    setShieldButtonVisible(false);
    if (btnFire) (btnFire as HTMLButtonElement).disabled = true;
    if (btnLeft) (btnLeft as HTMLButtonElement).disabled = true;
    if (btnRight) (btnRight as HTMLButtonElement).disabled = true;
});

btnLeave?.addEventListener("click", (e) => {
    e.stopPropagation();
    e.preventDefault();
    sendAction({ leave: {} });
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
