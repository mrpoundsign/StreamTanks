import { WSMessage } from './types';

export type MessageCallback = (msg: WSMessage) => void;

export class NetworkManager {
  private ws: WebSocket | null = null;
  private isDisconnected = false;
  private reconnectInterval: ReturnType<typeof setInterval> | null = null;
  private healthCheckInterval: ReturnType<typeof setInterval> | null = null;
  private messageHandlers: MessageCallback[] = [];

  constructor() {
    this.connect();
  }

  public onMessage(handler: MessageCallback): void {
    this.messageHandlers.push(handler);
  }

  public send(msg: WSMessage): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private connect(): void {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    try {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        console.log('Connected to StreamTanks server');
        if (this.isDisconnected) {
          window.location.reload();
          return;
        }

        if (this.healthCheckInterval) clearInterval(this.healthCheckInterval);
        this.healthCheckInterval = setInterval(() => {
          if (this.ws && this.ws.readyState === WebSocket.CLOSED) {
            this.handleDisconnect();
          }
        }, 2000);
      };

      this.ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data) as WSMessage;
          for (const handler of this.messageHandlers) {
            handler(msg);
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err);
        }
      };

      this.ws.onclose = () => this.handleDisconnect();
      this.ws.onerror = () => this.handleDisconnect();
    } catch {
      this.handleDisconnect();
    }
  }

  private handleDisconnect(): void {
    if (this.isDisconnected) return;
    this.isDisconnected = true;

    console.log('Server disconnected or unreachable. Hiding overlay and waiting for server to return...');

    const container = document.getElementById('game-container');
    if (container) {
      container.style.display = 'none';
    }
    document.body.style.display = 'none';

    if (this.healthCheckInterval) {
      clearInterval(this.healthCheckInterval);
      this.healthCheckInterval = null;
    }

    if (this.reconnectInterval) clearInterval(this.reconnectInterval);
    this.reconnectInterval = setInterval(async () => {
      try {
        const res = await fetch('/', { method: 'HEAD', cache: 'no-store' });
        if (res.ok) {
          console.log('Server detected back online! Reloading page...');
          clearInterval(this.reconnectInterval!);
          window.location.reload();
        }
      } catch {
        // Still down, keep waiting
      }
    }, 1500);
  }
}
