"use strict";
(() => {
  // ext-web/src/ext.ts
  var CC_SERVER_URL = "wss://st-cc.poundsigndesign.com/ws/viewer";
  var ws = null;
  var viewerToken = "";
  var currentUsername = "";
  var activePlayers = [];
  var hasJoined = false;
  var currentAngle = 45;
  var currentPower = 100;
  var pingInterval = null;
  var countdownInterval = null;
  var localTimerRemaining = 0;
  var currentPhaseStr = "IDLE";
  var lastProtractorX = -1;
  var lastProtractorY = -1;
  var protractorPreviewUntil = 0;
  var protractorPreviewTimeout = null;
  var isStandaloneDev = !window.Twitch || !window.Twitch.ext;
  var viewportSvg = document.getElementById("viewport-svg");
  var protractorOverlayGroup = document.getElementById("protractor-overlay-group");
  var protractorHitArea = document.getElementById("protractor-hit-area");
  var angleNeedle = document.getElementById("angle-needle");
  var needleHead = document.getElementById("needle-head");
  var angleBadgeGroup = document.getElementById("angle-badge-group");
  var mobileAngleBadge = document.querySelector(".mobile-angle-badge");
  var valAngle = document.getElementById("val-angle");
  var pivotCenter = document.getElementById("pivot-center") || document.querySelector(".pivot-center");
  function setAimingVisible(visible) {
    if (protractorOverlayGroup) {
      if (visible) {
        protractorOverlayGroup.classList.remove("hidden");
      } else {
        protractorOverlayGroup.classList.add("hidden");
      }
      protractorOverlayGroup.style.display = visible ? "" : "none";
      protractorOverlayGroup.style.visibility = visible ? "visible" : "hidden";
      protractorOverlayGroup.setAttribute("visibility", visible ? "visible" : "hidden");
    }
    if (protractorHitArea) {
      protractorHitArea.style.display = visible ? "" : "none";
      protractorHitArea.style.visibility = visible ? "visible" : "hidden";
    }
    if (angleNeedle) {
      angleNeedle.style.display = visible ? "" : "none";
      angleNeedle.style.visibility = visible ? "visible" : "hidden";
    }
    if (needleHead) {
      needleHead.style.display = visible ? "" : "none";
      needleHead.style.visibility = visible ? "visible" : "hidden";
    }
    if (angleBadgeGroup) {
      angleBadgeGroup.style.display = visible ? "" : "none";
      angleBadgeGroup.style.visibility = visible ? "visible" : "hidden";
    }
    if (pivotCenter) {
      pivotCenter.style.display = visible ? "" : "none";
      pivotCenter.style.visibility = visible ? "visible" : "hidden";
    }
    if (mobileAngleBadge) {
      mobileAngleBadge.style.display = visible ? "" : "none";
      mobileAngleBadge.style.visibility = visible ? "visible" : "hidden";
    }
  }
  function showProtractorPreview(durationMs = 2500) {
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
  var sliderPower = document.getElementById("slider-power");
  var valPower = document.getElementById("val-power");
  var verticalPowerTrack = document.getElementById("vertical-power-track");
  var powerFillBar = document.getElementById("power-fill-bar");
  var powerThumb = document.getElementById("power-thumb");
  var phaseBadge = document.getElementById("phase-badge");
  var desktopTimer = document.getElementById("desktop-timer");
  var currentActionBadge = document.getElementById("current-action-badge");
  function setCurrentAction(actionText) {
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
  var adminControls = document.getElementById("admin-controls");
  var playerSetup = document.getElementById("player-setup");
  var playerControls = document.getElementById("player-controls");
  var statusMessage = document.getElementById("status-message");
  var msgLog = document.getElementById("message-log");
  var btnStartMatch = document.getElementById("btn-start-match");
  var btnJoin = document.getElementById("btn-join");
  var btnFire = document.getElementById("btn-fire");
  var btnLeft = document.getElementById("btn-left");
  var btnRight = document.getElementById("btn-right");
  var landingOverlay = document.getElementById("landing-overlay");
  var mobileLanding = document.getElementById("mobile-landing");
  var landingLauncher = document.getElementById("landing-launcher");
  var btnConnectTwitch = document.getElementById("btn-connect-twitch");
  var btnConnectTwitchMobile = document.getElementById("btn-connect-twitch-mobile");
  var btnDismissLanding = document.getElementById("btn-dismiss-landing");
  var btnCloseLanding = document.getElementById("btn-close-landing");
  var btnOpenLanding = document.getElementById("btn-open-landing");
  var isLinked = false;
  var landingDismissed = false;
  function isOpaque(name) {
    return /^[UA]\d+$/i.test(name);
  }
  function checkIdentityLinked(token) {
    if (!window.Twitch || !window.Twitch.ext) {
      return true;
    }
    try {
      const payloadStr = atob(token.split(".")[1]);
      const payload = JSON.parse(payloadStr);
      if (payload.user_id && payload.user_id !== "" && !isOpaque(payload.user_id)) {
        return true;
      }
    } catch (_) {
    }
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
  var isMobile = document.body.classList.contains("mobile-body");
  var pivotX = isMobile ? 160 : 250;
  var pivotY = isMobile ? 160 : 350;
  var needleRadius = isMobile ? 110 : 160;
  function setAngle(deg) {
    currentAngle = Math.max(0, Math.min(180, Math.round(deg)));
    if (valAngle) {
      valAngle.textContent = `${currentAngle}\xB0`;
    }
    if (angleNeedle && needleHead) {
      const rad = currentAngle * Math.PI / 180;
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
  function initProtractorAiming() {
    if (!viewportSvg || !protractorHitArea) return;
    let isAiming = false;
    function computeAngle(clientX, clientY) {
      const pt = viewportSvg.createSVGPoint();
      pt.x = clientX;
      pt.y = clientY;
      const ctm = viewportSvg.getScreenCTM();
      if (!ctm) return;
      const svgPt = pt.matrixTransform(ctm.inverse());
      const dx = svgPt.x - pivotX;
      const dy = -(svgPt.y - pivotY);
      const rad = Math.atan2(dy, dx);
      let deg = Math.round(rad * 180 / Math.PI);
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
      } catch (_) {
      }
      computeAngle(e.clientX, e.clientY);
    });
    protractorHitArea.addEventListener("pointermove", (e) => {
      if (!isAiming) return;
      e.preventDefault();
      computeAngle(e.clientX, e.clientY);
    });
    const endAiming = (e) => {
      if (isAiming) {
        isAiming = false;
        protractorHitArea.classList.remove("active");
        try {
          protractorHitArea.releasePointerCapture(e.pointerId);
        } catch (_) {
        }
      }
    };
    protractorHitArea.addEventListener("pointerup", endAiming);
    protractorHitArea.addEventListener("pointercancel", endAiming);
  }
  function setPower(val) {
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
  function initVerticalPower() {
    if (!verticalPowerTrack) return;
    let isDragging = false;
    function computePower(clientY) {
      const rect = verticalPowerTrack.getBoundingClientRect();
      if (rect.height <= 0) return;
      const pct = (rect.bottom - clientY) / rect.height * 100;
      setPower(pct);
    }
    verticalPowerTrack.addEventListener("pointerdown", (e) => {
      e.preventDefault();
      isDragging = true;
      verticalPowerTrack.classList.add("active");
      try {
        verticalPowerTrack.setPointerCapture(e.pointerId);
      } catch (_) {
      }
      computePower(e.clientY);
    });
    verticalPowerTrack.addEventListener("pointermove", (e) => {
      if (!isDragging) return;
      e.preventDefault();
      computePower(e.clientY);
    });
    const endDrag = (e) => {
      if (isDragging) {
        isDragging = false;
        verticalPowerTrack.classList.remove("active");
        try {
          verticalPowerTrack.releasePointerCapture(e.pointerId);
        } catch (_) {
        }
      }
    };
    verticalPowerTrack.addEventListener("pointerup", endDrag);
    verticalPowerTrack.addEventListener("pointercancel", endDrag);
  }
  if (sliderPower) {
    sliderPower.addEventListener("input", (e) => {
      const val = parseInt(e.target.value, 10);
      setPower(val);
    });
  }
  function getUserRole(token) {
    try {
      const payloadStr = atob(token.split(".")[1]);
      const payload = JSON.parse(payloadStr);
      return payload.role || "viewer";
    } catch (_) {
      return "viewer";
    }
  }
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
    }, 1e3);
  }
  function updateUIForPhase(phase, timerRemaining, playersCount, winner) {
    const cleanPhase = (phase || "IDLE").toUpperCase();
    currentPhaseStr = cleanPhase;
    if (phaseBadge) {
      phaseBadge.className = `phase-badge ${cleanPhase.toLowerCase()}`;
      if (cleanPhase === "INPUT") {
        const displaySec = timerRemaining !== void 0 ? timerRemaining : localTimerRemaining;
        phaseBadge.textContent = isMobile ? displaySec > 0 ? `INPUT (${displaySec}s)` : "INPUT" : "INPUT PHASE";
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
          const countStr = playersCount !== void 0 ? ` (${playersCount} joined)` : "";
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
    if (cleanPhase === "IDLE" && playersCount === 0) {
      hasJoined = false;
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
    const isOnBattlefield = currentUsername && activePlayers.includes(currentUsername) || hasJoined;
    if (cleanPhase === "IDLE") {
      if (Date.now() < protractorPreviewUntil || isStandaloneDev) {
        setAimingVisible(true);
      } else {
        setAimingVisible(false);
      }
      if (adminControls && isModOrBroadcaster) adminControls.classList.remove("hidden");
      if (playerSetup) {
        if (!isOnBattlefield && !isStandaloneDev) {
          playerSetup.classList.remove("hidden");
        } else {
          playerSetup.classList.add("hidden");
        }
      }
      if (playerControls) {
        if (isStandaloneDev) {
          playerControls.classList.remove("hidden");
        } else {
          playerControls.classList.add("hidden");
        }
      }
      if (statusMessage) statusMessage.classList.add("hidden");
    } else if (cleanPhase === "INPUT") {
      if (adminControls) adminControls.classList.add("hidden");
      if (isOnBattlefield || isStandaloneDev) {
        setAimingVisible(true);
        if (playerControls) playerControls.classList.remove("hidden");
        if (playerSetup) playerSetup.classList.add("hidden");
        if (btnFire) btnFire.disabled = false;
      } else {
        setAimingVisible(false);
        if (playerSetup) playerSetup.classList.remove("hidden");
        if (playerControls) playerControls.classList.add("hidden");
      }
      if (statusMessage) statusMessage.classList.add("hidden");
    } else if (cleanPhase === "SIMULATION" || cleanPhase === "ACTION") {
      setAimingVisible(false);
      if (adminControls) adminControls.classList.add("hidden");
      if (statusMessage) {
        statusMessage.textContent = "CANNONS FIRING...";
        statusMessage.classList.remove("hidden");
      }
      if (btnFire) btnFire.disabled = true;
    } else if (cleanPhase === "ROUND_OVER" || cleanPhase === "CELEBRATION") {
      setAimingVisible(false);
      if (statusMessage) {
        statusMessage.textContent = winner ? `WINNER: ${winner}` : "ROUND OVER";
        statusMessage.classList.remove("hidden");
      }
      if (btnFire) btnFire.disabled = true;
    }
  }
  function connectWebSocket() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    startCountdownTimer();
    const isLocalDev2 = window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1";
    if (isLocalDev2) {
      logMessage("Connecting to local StreamTanks server...");
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${proto}//${window.location.host}/ws?client=extension`);
    } else {
      logMessage("Connecting to C&C...");
      ws = new WebSocket(`${CC_SERVER_URL}?token=${viewerToken}`);
    }
    ws.onopen = () => {
      logMessage("Connected!");
      if (!isLocalDev2) {
        ws.send(JSON.stringify({ jwt: viewerToken }));
      }
      if (pingInterval) clearInterval(pingInterval);
      pingInterval = window.setInterval(() => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: "PING" }));
        }
      }, 45e3);
      updateUIForPhase("IDLE");
      if (hasJoined) {
        sendCommand("%join");
      }
    };
    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === "VIEWER_INFO" && data.payload) {
          const userStr = data.payload.user ? String(data.payload.user).trim() : "";
          if (userStr && !isOpaque(userStr)) {
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
          if (Array.isArray(payload.players)) {
            activePlayers = payload.players.map((p) => String(p).toLowerCase());
          } else if (payload.players && typeof payload.players === "object") {
            activePlayers = Object.keys(payload.players).map((p) => p.toLowerCase());
          }
          if (timerRemaining !== void 0) {
            localTimerRemaining = timerRemaining;
          }
          if (!isMobile) {
            const px = payload.protractor_x ?? payload.protractorX;
            const py = payload.protractor_y ?? payload.protractorY;
            let posChanged = false;
            if (typeof px === "number") {
              if (lastProtractorX !== px) posChanged = true;
              pivotX = px;
              lastProtractorX = px;
            }
            if (typeof py === "number") {
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
      setTimeout(connectWebSocket, 3e3);
    };
    ws.onerror = (err) => {
      console.error("WebSocket error:", err);
      ws?.close();
    };
  }
  function sendCommand(cmd) {
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
  function logMessage(msg) {
    if (!msgLog) return;
    msgLog.textContent = msg;
    setTimeout(() => {
      if (msgLog && msgLog.textContent === msg) {
        msgLog.textContent = "";
      }
    }, 4e3);
  }
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
  btnStartMatch?.addEventListener("click", () => {
    sendCommand("%startgame");
    logMessage("Match starting...");
  });
  btnJoin?.addEventListener("click", () => {
    if (!isLinked) {
      promptIdentityShare();
      return;
    }
    sendCommand("%join");
    hasJoined = true;
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
    setCurrentAction(`LOCKED: FIRE ${currentAngle}\xB0 @ ${currentPower}%`);
    logMessage(`Fired: ${currentAngle}\xB0 @ ${currentPower}%`);
  });
  var isLocalDev = window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1";
  if (isLocalDev || !window.Twitch || !window.Twitch.ext) {
    console.log("Running in standalone/local preview mode.");
    isLinked = true;
    updateLandingVisibility();
    connectWebSocket();
  }
  if (window.Twitch && window.Twitch.ext) {
    window.Twitch.ext.onAuthorized((auth) => {
      const tokenChanged = viewerToken !== "" && viewerToken !== auth.token;
      viewerToken = auth.token;
      const previouslyLinked = isLinked;
      isLinked = checkIdentityLinked(viewerToken);
      updateLandingVisibility();
      if (tokenChanged && ws) {
        ws.close();
      } else if (!ws || ws.readyState === WebSocket.CLOSED) {
        connectWebSocket();
      }
      if (!previouslyLinked && isLinked) {
        logMessage("Twitch account connected!");
      }
    });
  }
  initProtractorAiming();
  setAngle(45);
  initVerticalPower();
  setPower(100);
  setAimingVisible(isStandaloneDev);
  updateLandingVisibility();
})();
//# sourceMappingURL=ext.js.map
