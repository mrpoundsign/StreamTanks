import { WIDTH, HEIGHT, Player, Projectile, Explosion, GamePhase } from './types';
import { getTerrainSlopeAngle } from './terrain';

export function drawTerrain(ctx: CanvasRenderingContext2D, terrain: number[]): void {
  if (terrain.length === 0) return;

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

  // Fill below terrain with subtle red tint
  ctx.lineTo(WIDTH, HEIGHT);
  ctx.lineTo(0, HEIGHT);
  ctx.closePath();
  ctx.fillStyle = 'rgba(255, 0, 60, 0.02)';
  ctx.fill();
  ctx.shadowBlur = 0; // reset
}

export function drawGiantProtractor(
  ctx: CanvasRenderingContext2D,
  centerX: number,
  centerY: number,
  isDead: boolean = false
): void {
  ctx.save();
  ctx.translate(centerX, centerY); // Configurable position

  const arcColor = isDead ? 'rgba(255, 0, 60, 0.6)' : 'rgba(0, 255, 204, 0.5)';
  const glowColor = isDead ? '#ff003c' : '#00ffcc';

  ctx.strokeStyle = arcColor;
  ctx.lineWidth = 10;
  ctx.shadowBlur = 20;
  ctx.shadowColor = glowColor;

  // Draw giant arc
  ctx.beginPath();
  ctx.arc(0, 0, 150, Math.PI, 0);
  ctx.stroke();

  ctx.fillStyle = glowColor;
  ctx.font = '24px Orbitron';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';

  // Major angles with labels
  const majorAngles = [
    { deg: 0, label: '0°' },
    { deg: 45, label: '45°' },
    { deg: 90, label: '90°' },
    { deg: 135, label: '135°' },
    { deg: 180, label: '180°' },
  ];

  for (const { deg, label } of majorAngles) {
    const rad = (deg * Math.PI) / 180;
    const x1 = Math.cos(rad) * 140;
    const y1 = -Math.sin(rad) * 140;
    const x2 = Math.cos(rad) * 160;
    const y2 = -Math.sin(rad) * 160;

    ctx.lineWidth = 4;
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();

    const tx = Math.cos(rad) * 190;
    const ty = -Math.sin(rad) * 190;
    ctx.fillText(label, tx, ty);
  }

  // Minor ticks every 15 degrees
  ctx.lineWidth = 2;
  for (let deg = 15; deg < 180; deg += 15) {
    if (deg % 45 === 0) continue;
    const rad = (deg * Math.PI) / 180;
    const x1 = Math.cos(rad) * 145;
    const y1 = -Math.sin(rad) * 145;
    const x2 = Math.cos(rad) * 155;
    const y2 = -Math.sin(rad) * 155;

    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();
  }

  if (isDead) {
    // Large skull emoji centered in the protractor
    ctx.font = '72px sans-serif';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.shadowBlur = 30;
    ctx.shadowColor = '#ff003c';
    ctx.fillText('💀', 0, -25);
  } else {
    // Center indicator & crosshair
    ctx.fillStyle = '#ff003c';
    ctx.shadowColor = '#ff003c';
    ctx.beginPath();
    ctx.arc(0, 0, 8, 0, Math.PI * 2);
    ctx.fill();
  }

  ctx.restore();
}

export function drawTanks(
  ctx: CanvasRenderingContext2D,
  players: Record<string, Player>,
  terrain: number[],
  currentPhase: GamePhase,
  emotesLayer: HTMLElement,
  emoteCache: Record<string, HTMLImageElement>,
  avatarImgCache?: Record<string, HTMLImageElement>
): void {
  for (const name in players) {
    const p = players[name];
    let imgEl = document.getElementById('emote-' + name) as HTMLImageElement | null;
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

    const angle = getTerrainSlopeAngle(terrain, p.x);
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
      imgEl.style.left = p.x - 14 + 'px';
      imgEl.style.top = p.y - 35 + 'px';
      imgEl.style.transformOrigin = '14px 35px';
      imgEl.style.transform = `rotate(${angle}rad)`;
    }

    const isHuman = !p.isBot && !name.startsWith('_bot_');
    const isJoinedInIdle = isHuman && currentPhase === 'IDLE' && !!p.joined;
    const showCommandMarker = (isHuman && currentPhase === 'INPUT') || isJoinedInIdle;
    const statusColor = isJoinedInIdle ? '#00ffcc' : (p.fired ? '#00ffcc' : '#ff003c');

    // Draw Name and Avatar Marker
    const displayName = (p.name && !p.name.startsWith('_bot_')) ? p.name : (!name.startsWith('_bot_') && !p.isBot ? name : '');
    if (displayName) {
      if (showCommandMarker) {
        // --- INPUT Phase: Prominent Avatar Pin & Enlarged Name ---
        const markerX = p.x;
        const markerY = p.y - 142;
        const radius = 32; // 64px diameter for clear mobile visibility

        // 1. Circular Avatar Image
        const avatarImg = (avatarImgCache && avatarImgCache[name]) || (p.emoteUrl && emoteCache[p.emoteUrl]) || null;
        ctx.save();
        ctx.beginPath();
        ctx.arc(markerX, markerY, radius, 0, Math.PI * 2);
        ctx.closePath();
        ctx.clip();
        if (avatarImg && avatarImg.complete && avatarImg.naturalWidth > 0) {
          ctx.drawImage(avatarImg, markerX - radius, markerY - radius, radius * 2, radius * 2);
        } else {
          ctx.fillStyle = '#12161e';
          ctx.fillRect(markerX - radius, markerY - radius, radius * 2, radius * 2);
          ctx.fillStyle = statusColor;
          ctx.font = 'bold 22px Orbitron';
          ctx.textAlign = 'center';
          ctx.textBaseline = 'middle';
          ctx.fillText(displayName.charAt(0).toUpperCase(), markerX, markerY);
        }
        ctx.restore();

        // 2. Glowing Neon Border around Avatar
        ctx.beginPath();
        ctx.arc(markerX, markerY, radius + 2.5, 0, Math.PI * 2);
        ctx.strokeStyle = statusColor;
        ctx.lineWidth = 4;
        ctx.shadowBlur = 15;
        ctx.shadowColor = statusColor;
        ctx.stroke();

        // 3. Downward Chevron Pin
        ctx.beginPath();
        ctx.moveTo(markerX - 8, markerY + radius + 4);
        ctx.lineTo(markerX, markerY + radius + 14);
        ctx.lineTo(markerX + 8, markerY + radius + 4);
        ctx.strokeStyle = statusColor;
        ctx.lineWidth = 4;
        ctx.shadowBlur = 10;
        ctx.shadowColor = statusColor;
        ctx.stroke();
        ctx.shadowBlur = 0;

        // 4. Enlarged Name Below Avatar (above protractor)
        const nameY = p.y - 75;
        ctx.font = 'bold 28px Orbitron';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'alphabetic';

        // Dark text outline for maximum legibility on stream
        ctx.strokeStyle = '#000000';
        ctx.lineWidth = 6;
        ctx.strokeText(displayName, markerX, nameY);

        // Glowing text fill
        ctx.fillStyle = '#ffffff';
        ctx.shadowBlur = 10;
        ctx.shadowColor = statusColor;
        ctx.fillText(displayName, markerX, nameY);
        ctx.shadowBlur = 0;
      } else {
        // --- Standard Phase / Bots: Default Name Below Treads ---
        ctx.fillStyle = '#fff';
        ctx.font = '16px Orbitron';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'alphabetic';
        ctx.shadowBlur = 5;
        ctx.shadowColor = '#000';
        ctx.fillText(displayName, p.x, p.y + 20);
        ctx.shadowBlur = 0;
      }
    }

    // Draw Protractor & Firing state in INPUT phase
    if (currentPhase === 'INPUT') {
      ctx.strokeStyle = p.fired ? '#00ffcc' : '#ff003c'; // Turns cyan if locked in
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
        const rad = (deg * Math.PI) / 180;
        const innerR = 45;
        const outerR = 50;
        ctx.beginPath();
        ctx.moveTo(p.x + Math.cos(rad) * innerR, p.y - 10 - Math.sin(rad) * innerR);
        ctx.lineTo(p.x + Math.cos(rad) * outerR, p.y - 10 - Math.sin(rad) * outerR);
        ctx.stroke();

        ctx.fillText(deg.toString(), p.x + Math.cos(rad) * 60, p.y - 10 - Math.sin(rad) * 60);
      }

      // Aiming line
      const aimAngle = ((p.lastAngle ?? 45) * Math.PI) / 180;
      ctx.beginPath();
      ctx.moveTo(p.x, p.y - 10);
      ctx.lineTo(p.x + Math.cos(aimAngle) * 50, p.y - 10 - Math.sin(aimAngle) * 50);
      ctx.stroke();
    }
  }
}

export function drawProjectiles(
  ctx: CanvasRenderingContext2D,
  projectiles: Projectile[],
  emoteCache: Record<string, HTMLImageElement>
): void {
  for (const proj of projectiles) {
    const img = proj.emoteUrl ? emoteCache[proj.emoteUrl] : null;
    if (img && img.complete && img.naturalWidth > 0) {
      ctx.shadowBlur = 0;
      ctx.drawImage(img, proj.x - 7, proj.y - 7, 14, 14);
    } else {
      ctx.fillStyle = '#00ffcc';
      ctx.shadowBlur = 10;
      ctx.shadowColor = '#00ffcc';
      ctx.beginPath();
      ctx.arc(proj.x, proj.y, 4, 0, Math.PI * 2);
      ctx.fill();
    }
  }
}

export function drawExplosions(ctx: CanvasRenderingContext2D, explosions: Explosion[]): void {
  for (const exp of explosions) {
    if (exp.isSpark) {
      ctx.strokeStyle = `rgba(0, 255, 204, ${exp.alpha})`;
      ctx.shadowBlur = 15;
      ctx.shadowColor = '#00ffcc';
      ctx.lineWidth = 3;
    } else {
      ctx.strokeStyle = `rgba(255, 0, 60, ${exp.alpha})`;
      ctx.shadowBlur = 20;
      ctx.shadowColor = '#ff003c';
      ctx.lineWidth = 4;
    }
    ctx.beginPath();
    ctx.arc(exp.x, exp.y, exp.radius, 0, Math.PI * 2);
    ctx.stroke();
  }
}
