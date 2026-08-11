import { WebSocket as PartySocket } from "partysocket";
import { appConfig } from "@/lib/env";

export type RealtimeEvent = {
  id?: string;
  type: string;
  appid?: number;
  userId?: number;
  timestamp?: string;
  data?: Record<string, unknown>;
  [key: string]: unknown;
};

type Handler = (event: RealtimeEvent) => void;

/** 长连接状态。UI 据此提示"实时通知已断开"，避免静默失联 */
export type RealtimeStatus = "idle" | "connecting" | "open" | "closed";

type StatusHandler = (status: RealtimeStatus) => void;

/**
 * RealtimeClient — 基于 partysocket 的 WebSocket 长连接管理
 *
 * - 自动重连（指数退避）
 * - 客户端心跳保活（20s）
 * - 幂等连接（相同 token 不重复建连）
 * - 事件分发
 */
class RealtimeClient {
  private ws: PartySocket | null = null;
  private currentToken: string | null = null;
  private listeners = new Map<string, Set<Handler>>();
  private globalListeners = new Set<Handler>();
  private statusListeners = new Set<StatusHandler>();
  private currentStatus: RealtimeStatus = "idle";
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;

  /** 建立长连接（相同 token 幂等） */
  connect(token: string) {
    if (this.ws && this.currentToken === token) return;
    if (this.ws) this.disconnect();

    this.currentToken = token;

    this.ws = new PartySocket(this.buildUrl(), ["aegis", `aegis.jwt.${token}`], {
      maxRetries: Infinity,
      minReconnectionDelay: 1000,
      maxReconnectionDelay: 30000,
      reconnectionDelayGrowFactor: 1.5,
      connectionTimeout: 10000,
      startClosed: false,
    });

    this.ws.addEventListener("message", this.handleMessage);
    this.ws.addEventListener("open", this.handleOpen);
    this.ws.addEventListener("close", this.handleClose);
    this.ws.addEventListener("error", this.handleClose);

    this.setStatus("connecting");
    this.startHeartbeat();
  }

  /** 断开连接 */
  disconnect() {
    this.stopHeartbeat();
    this.currentToken = null;
    if (this.ws) {
      this.ws.removeEventListener("message", this.handleMessage);
      this.ws.removeEventListener("open", this.handleOpen);
      this.ws.removeEventListener("close", this.handleClose);
      this.ws.removeEventListener("error", this.handleClose);
      this.ws.close();
      this.ws = null;
    }
    this.setStatus("idle");
  }

  /** 监听指定事件类型，返回取消函数 */
  on(type: string, handler: Handler): () => void {
    let set = this.listeners.get(type);
    if (!set) {
      set = new Set();
      this.listeners.set(type, set);
    }
    set.add(handler);
    return () => { set?.delete(handler); };
  }

  /** 监听所有事件 */
  onAny(handler: Handler): () => void {
    this.globalListeners.add(handler);
    return () => { this.globalListeners.delete(handler); };
  }

  /** 当前是否已连接 */
  get connected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  /** 当前连接状态 */
  get status(): RealtimeStatus {
    return this.currentStatus;
  }

  /**
   * 订阅连接状态变化，返回取消函数。
   * 订阅时会立即回调一次当前状态，便于组件挂载即拿到初值。
   */
  onStatus(handler: StatusHandler): () => void {
    this.statusListeners.add(handler);
    handler(this.currentStatus);
    return () => {
      this.statusListeners.delete(handler);
    };
  }

  // ── 内部 ──

  private buildUrl() {
    return appConfig.wsUrl;
  }

  private handleOpen = () => {
    this.setStatus("open");
    // 连接成功后立即发一次心跳确认
    this.sendPing();
  };

  // partysocket 会自动重连，这里只负责把"当前断开"如实反映到 UI
  private handleClose = () => {
    if (this.currentToken) this.setStatus("closed");
  };

  private setStatus(next: RealtimeStatus) {
    if (this.currentStatus === next) return;
    this.currentStatus = next;
    this.statusListeners.forEach((h) => h(next));
  }

  private handleMessage = (msg: MessageEvent) => {
    try {
      const evt = JSON.parse(msg.data as string) as RealtimeEvent;
      if (!evt.type) return;
      const t = evt.type;
      if (t === "system.ping" || t === "ping") { this.sendPing(); return; }
      if (t === "system.welcome" || t === "system.pong") return;
      this.dispatch(evt);
    } catch { /* 忽略非 JSON */ }
  };

  private sendPing() {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type: "ping" }));
    }
  }

  private dispatch(evt: RealtimeEvent) {
    this.listeners.get(evt.type)?.forEach((h) => h(evt));
    this.globalListeners.forEach((h) => h(evt));
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => this.sendPing(), 20_000);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }
}

export const realtimeClient = new RealtimeClient();
