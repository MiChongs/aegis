import type { ChatTransport, UIMessage, UIMessageChunk } from "ai";

/**
 * Agent 对话的 WebSocket 传输层：实现 AI SDK 的 ChatTransport 接口。
 *
 * 与后端 /ai/agent/ws 的帧协议对齐（见 ai_agent_ws_handlers.go）：
 * - 发送：{"type":"run","payload":{…}}，途中喊停发 {"type":"cancel"}；
 * - 接收：{"kind":"chunk","chunk":{UI Message Stream 分片}} / {"kind":"done"} /
 *   {"kind":"error","errorText"}。chunk 原样进 useChat 的流，UI 层零改动。
 *
 * 鉴权按平台约定走 Sec-WebSocket-Protocol 子协议（"aegis.jwt.<token>"）——
 * 浏览器不允许给握手加自定义头。**每轮对话一条连接**：生命周期与一轮
 * 严格对齐，没有跨轮的连接状态要维护，断没断一目了然。
 *
 * WS 建连失败（代理不透传升级、握手超时）自动退回 SSE 传输，
 * 用户至多损失一点韧性，不会失去对话能力。
 */

type SendOptions<M extends UIMessage> = Parameters<ChatTransport<M>["sendMessages"]>[0];

type ServerFrame = {
  kind?: "chunk" | "done" | "error";
  chunk?: UIMessageChunk;
  errorText?: string;
};

export type AgentSocketRequest = {
  /** 管理员令牌；空串表示尚未登录（此时上层不该发起对话）。 */
  token: string;
  /** run 帧的 payload，与 SSE 端点的请求体同构。 */
  body: Record<string, unknown>;
};

export class AgentSocketTransport<M extends UIMessage> implements ChatTransport<M> {
  private readonly socketPath: () => string;
  private readonly request: (options: SendOptions<M>) => AgentSocketRequest;
  private readonly fallback: ChatTransport<M>;
  private readonly connectTimeoutMs: number;

  constructor(options: {
    /** WS 路径（http(s) 或相对路径均可，内部换算成 ws(s)）。 */
    socketPath: () => string;
    /** 每次发送时取鉴权与业务载荷（读组件的最新现场）。 */
    request: (options: SendOptions<M>) => AgentSocketRequest;
    /** WS 建连失败时的兜底传输（SSE）。 */
    fallback: ChatTransport<M>;
    connectTimeoutMs?: number;
  }) {
    this.socketPath = options.socketPath;
    this.request = options.request;
    this.fallback = options.fallback;
    this.connectTimeoutMs = options.connectTimeoutMs ?? 4000;
  }

  async sendMessages(options: SendOptions<M>): Promise<ReadableStream<UIMessageChunk>> {
    const { token, body } = this.request(options);
    let socket: WebSocket;
    try {
      socket = await this.connect(token);
    } catch {
      return this.fallback.sendMessages(options);
    }
    return openRunStream(socket, body, options.abortSignal);
  }

  async reconnectToStream(): Promise<ReadableStream<UIMessageChunk> | null> {
    // 服务端不保留可重连的流；刷新后的历史由会话详情接口回放。
    return null;
  }

  private connect(token: string): Promise<WebSocket> {
    const url = toWebSocketUrl(this.socketPath());
    return new Promise((resolve, reject) => {
      let socket: WebSocket;
      try {
        socket = new WebSocket(url, ["aegis", `aegis.jwt.${token}`]);
      } catch (error) {
        reject(error instanceof Error ? error : new Error("WebSocket 创建失败"));
        return;
      }
      const timer = window.setTimeout(() => {
        socket.close();
        reject(new Error("WebSocket 连接超时"));
      }, this.connectTimeoutMs);
      socket.onopen = () => {
        window.clearTimeout(timer);
        resolve(socket);
      };
      socket.onerror = () => {
        window.clearTimeout(timer);
        socket.close();
        reject(new Error("WebSocket 连接失败"));
      };
    });
  }
}

/** 把 http(s)/相对路径换算成同源 ws(s) 地址。 */
function toWebSocketUrl(path: string): string {
  const url = new URL(path, window.location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

/** 发出 run 帧并把服务端帧翻译成 UI Message Stream。 */
function openRunStream(
  socket: WebSocket,
  body: Record<string, unknown>,
  abortSignal: AbortSignal | undefined
): ReadableStream<UIMessageChunk> {
  return new ReadableStream<UIMessageChunk>({
    start(controller) {
      let settled = false;
      const finish = (error?: Error) => {
        if (settled) return;
        settled = true;
        socket.onmessage = null;
        socket.onclose = null;
        socket.onerror = null;
        if (error) controller.error(error);
        else controller.close();
        try {
          socket.close();
        } catch {
          // 已断开
        }
      };

      socket.onmessage = (event) => {
        let frame: ServerFrame;
        try {
          frame = JSON.parse(String(event.data)) as ServerFrame;
        } catch {
          return; // 非 JSON 帧一律忽略
        }
        if (frame.kind === "chunk" && frame.chunk) {
          controller.enqueue(frame.chunk);
        } else if (frame.kind === "done") {
          finish();
        } else if (frame.kind === "error") {
          finish(new Error(frame.errorText || "对话失败"));
        }
      };
      // done 帧之前连接断开 = 本轮没送完
      socket.onclose = () => finish(new Error("连接已断开，本轮回复可能不完整"));
      socket.onerror = () => finish(new Error("连接异常断开"));

      if (abortSignal) {
        const abort = () => {
          try {
            socket.send(JSON.stringify({ type: "cancel" }));
          } catch {
            // 已断开：直接收尾
          }
          finish();
        };
        if (abortSignal.aborted) {
          abort();
          return;
        }
        abortSignal.addEventListener("abort", abort, { once: true });
      }

      try {
        socket.send(JSON.stringify({ type: "run", payload: body }));
      } catch {
        finish(new Error("发送对话请求失败"));
      }
    },
    cancel() {
      try {
        socket.close();
      } catch {
        // 已断开
      }
    }
  });
}
