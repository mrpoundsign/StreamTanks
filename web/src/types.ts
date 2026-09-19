// Game Constants
export const WIDTH = 1920;
export const HEIGHT = 1080;
export const EXPLOSION_RADIUS = 60;
export const GRAVITY = 0.15;

// Game Phases
export const PhaseIdle = 'IDLE';
export const PhaseInput = 'INPUT';
export const PhaseAction = 'ACTION';
export const PhaseCelebration = 'CELEBRATION';
export const PhaseWaitingNextPhase = 'WAITING_NEXT_PHASE';

export type GamePhase =
  | typeof PhaseIdle
  | typeof PhaseInput
  | typeof PhaseAction
  | typeof PhaseCelebration
  | typeof PhaseWaitingNextPhase;

// WebSocket Message Types
export const MsgStateUpdate = 'STATE_UPDATE';
export const MsgExecuteActions = 'EXECUTE_ACTIONS';
export const MsgPlayerLocked = 'PLAYER_LOCKED';
export const MsgResetTerrain = 'RESET_TERRAIN';
export const MsgActionComplete = 'ACTION_COMPLETE';
export const MsgPlayerDied = 'PLAYER_DIED';
export const MsgGameOver = 'GAME_OVER';
export const MsgCelebrationComplete = 'CELEBRATION_COMPLETE';
export const MsgChatCommand = 'CHAT_COMMAND';
export const MsgDebugCommand = 'DEBUG_COMMAND';
export const MsgTerrainCrater = 'TERRAIN_CRATER';

// Player Actions
export const ActionFire = 'FIRE';
export const ActionLeft = 'LEFT';
export const ActionRight = 'RIGHT';

export type ActionType = typeof ActionFire | typeof ActionLeft | typeof ActionRight | string;

export interface Player {
  name: string;
  emote?: string;
  emoteUrl?: string;
  lastAngle?: number;
  lastPower?: number;
  fired?: boolean;
  actionType?: ActionType;
  angle?: number;
  power?: number;
  isDead?: boolean;
  isBot?: boolean;

  // Client-side physics & animation state
  x: number;
  y: number;
  dx: number;
  moving?: boolean;
  moveTarget?: number;
  speedMultiplier?: number;
  hasBounced?: boolean;
  lastActiveRound?: number;
}

export interface GameState {
  phase: GamePhase;
  players: Record<string, Player>;
  inputDuration: number;
  moveDistance: number;
  leaderboard: Record<string, number>;
  debug: boolean;
  prefix: string;
  physicsSpeed: number;
  showConfig: boolean;
  autoRound: number;
  idleMessage: boolean;
  bouncyWalls: boolean;
  terrain?: number[];
  terrainMin?: number;
  terrainMax?: number;
  roundId?: number;
  startPerm?: string;
  configPerm?: string;
  minPlayers?: number;
  botFill?: boolean;
  botPoints?: number;
  botList?: string[];
  winner?: string;
  timerRemaining?: number;
  matchKills?: KillEvent[];
  projectiles?: Projectile[];
  explosions?: Explosion[];
  protractorX?: number;
  protractorY?: number;
}

export interface KillEvent {
  killer?: string;
  killerIsBot?: boolean;
  victim: string;
  victimIsBot: boolean;
  angle?: number;
  power?: number;
  impactX?: number;
  impactY?: number;
  roundId?: number;
  timestamp?: number;
}

export interface Projectile {
  id?: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  owner: string;
  emoteUrl?: string;
  bounces?: number;
}

export interface Explosion {
  x: number;
  y: number;
  radius: number;
  maxRadius: number;
  alpha: number;
  isSpark?: boolean;
}

export interface CraterPayload {
  id?: string;
  x: number;
  y: number;
  radius: number;
}

export interface PlayerDiedPayload {
  victim: string;
  victimIsBot?: boolean;
  killer?: string;
  killerIsBot?: boolean;
}

export interface WSMessage<T = unknown> {
  type: string;
  payload?: T;
}
