package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// AI Agent 的 WebSocket 对话通道（应用级）。
//
// 与 POST /ai/agent/stream 承载**同一套 UI Message Stream 载荷**：SSE 把 chunk
// 写成 `data: {...}` 行，这里把 chunk 装进 {"kind":"chunk"} 信封帧。加一条
// WebSocket 通道的动机是长对话的传输韧性：双向长连接不背「响应该结束了」的
// 包袱（写超时、压缩缓冲、代理的响应空闲上限都冲着"响应"来），且途中喊停
// 是一个 cancel 帧，而不是掐断整条连接再重来。
//
// 帧协议（文本 JSON）：
//
//	客户端 → 服务端：{"type":"run","payload":{同 adminAIAgentStreamRequest}}
//	                 {"type":"cancel"}
//	服务端 → 客户端：{"kind":"chunk","chunk":{...}}   —— UI Message Stream 分片
//	                 {"kind":"done"}                  —— 一轮对话收尾（含被取消）
//	                 {"kind":"error","errorText":..}  —— 开跑前失败 / 协议错误（流中失败走 error chunk）
//
// 鉴权：浏览器无法给握手加自定义头，令牌按平台既有约定放在
// Sec-WebSocket-Protocol 的 "aegis.jwt.<token>" 项里（middleware.adminBearerToken
// 已识别，与 /api/ws 同一套），服务端只回显 "aegis" —— 令牌项绝不回显，
// 否则它会进反代与网关的访问日志。
const (
	aiAgentWSWriteWait   = 10 * time.Second
	aiAgentWSPongWait    = 70 * time.Second
	aiAgentWSPingPeriod  = 25 * time.Second
	aiAgentWSSubprotocol = "aegis"
	// aiAgentWSMaxFrame 请求帧上限：payload 里带编辑器草稿全文，1MB 打得住。
	aiAgentWSMaxFrame = 1 << 20
)

type aiAgentWSClientFrame struct {
	Type    string                    `json:"type"`
	Payload adminAIAgentStreamRequest `json:"payload"`
}

type aiAgentWSServerFrame struct {
	Kind      string `json:"kind"`
	Chunk     any    `json:"chunk,omitempty"`
	ErrorText string `json:"errorText,omitempty"`
}

// aiAgentSocketRun 一轮对话的执行体；由 handler 绑定应用与管理员后注入，
// 连接协议循环（serveAIAgentSocket）便可脱离 service 依赖单测。
type aiAgentSocketRun func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error

// AdminAppAIAgentWS 建立 Agent 对话的 WebSocket 通道。
func (h *Handler) AdminAppAIAgentWS(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	session, ok := adminAccessSession(c)
	if !ok || session.AdminID <= 0 {
		response.Error(c, http.StatusUnauthorized, 40100, "管理员会话无效")
		return
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     h.aiAgentWSOrigin,
		// 必须声明并回显子协议，否则 Chromium 按握手失败处理（close 1006），
		// 详见 realtime_service.go 里同一段教训。
		Subprotocols: []string{aiAgentWSSubprotocol},
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// gorilla 失败时已写好 HTTP 错误响应，这里只留痕
		_ = c.Error(err)
		return
	}
	serveAIAgentSocket(c.Request.Context(), conn, func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error {
		return h.aiAgent.Run(ctx, service.AIAgentRunInput{
			AppID:          appID,
			AdminID:        session.AdminID,
			ConversationID: req.ConversationID,
			Scene:          req.Scene,
			Ref:            strings.TrimSpace(req.Ref),
			UserText:       req.Message,
			DraftSource:    req.DraftSource,
			ConfigID:       req.ConfigID,
			Model:          strings.TrimSpace(req.Model),
			SkillKeys:      req.SkillKeys,
			DisableWrites:  req.DisableWrites,
		}, emit)
	})
}

// serveAIAgentSocket 驱动一条连接的完整生命周期：读帧、跑对话、心跳、收摊。
// 阻塞到连接关闭；进行中的一轮随连接断开被取消。
func serveAIAgentSocket(parent context.Context, conn *websocket.Conn, run aiAgentSocketRun) {
	defer conn.Close()

	// 连接级 ctx：连接断开 / 服务停机 → 掐掉进行中的一轮
	connCtx, cancelConn := context.WithCancel(parent)
	defer cancelConn()

	// gorilla 不允许并发写：业务帧与协议 ping 共用一把锁
	var writeMu sync.Mutex
	writeFrame := func(frame aiAgentWSServerFrame) error {
		payload, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(aiAgentWSWriteWait))
		return conn.WriteMessage(websocket.TextMessage, payload)
	}

	conn.SetReadLimit(aiAgentWSMaxFrame)
	_ = conn.SetReadDeadline(time.Now().Add(aiAgentWSPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(aiAgentWSPongWait))
	})
	// 心跳：保活中间层，也让死连接在 pongWait 内被读循环发现
	go func() {
		ticker := time.NewTicker(aiAgentWSPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(aiAgentWSWriteWait))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					cancelConn()
					return
				}
			}
		}
	}()

	// 单连接同时只跑一轮：一个会话本来就是串行对话，并行只会把 UI 流搅成一锅。
	var runMu sync.Mutex
	var cancelRun context.CancelFunc

	beginRun := func() (context.Context, bool) {
		runMu.Lock()
		defer runMu.Unlock()
		if cancelRun != nil {
			return nil, false
		}
		ctx, cancel := context.WithCancel(connCtx)
		cancelRun = cancel
		return ctx, true
	}
	endRun := func() {
		runMu.Lock()
		if cancelRun != nil {
			cancelRun()
			cancelRun = nil
		}
		runMu.Unlock()
	}
	stopRun := func() {
		runMu.Lock()
		if cancelRun != nil {
			cancelRun()
		}
		runMu.Unlock()
	}

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			// 连接没了（关闭 / 读超时）：终止进行中的一轮后收摊
			cancelConn()
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var frame aiAgentWSClientFrame
		if err := json.Unmarshal(payload, &frame); err != nil {
			_ = writeFrame(aiAgentWSServerFrame{Kind: "error", ErrorText: "无法解析请求帧"})
			continue
		}
		switch frame.Type {
		case "run":
			runCtx, ok := beginRun()
			if !ok {
				_ = writeFrame(aiAgentWSServerFrame{Kind: "error", ErrorText: "上一轮对话尚未结束"})
				continue
			}
			go func(req adminAIAgentStreamRequest) {
				defer endRun()
				// Run 内部已有工具级恐慌兜底；这里再兜一层连接级的，
				// 保证任何意外都以帧收尾而不是让客户端干等。
				defer func() {
					if recovered := recover(); recovered != nil {
						_ = writeFrame(aiAgentWSServerFrame{Kind: "error", ErrorText: "对话执行异常中止"})
					}
				}()
				wroteAny := false
				emit := func(chunk any) error {
					wroteAny = true
					return writeFrame(aiAgentWSServerFrame{Kind: "chunk", Chunk: chunk})
				}
				err := run(runCtx, req, emit)
				// 开跑前就失败（会话不存在、消息为空）以 error 帧收尾；
				// 流中失败 Run 已发过 error chunk，被取消的一轮按正常收尾 —— 两者都走 done。
				if err != nil && !wroteAny && runCtx.Err() == nil {
					_ = writeFrame(aiAgentWSServerFrame{Kind: "error", ErrorText: err.Error()})
					return
				}
				_ = writeFrame(aiAgentWSServerFrame{Kind: "done"})
			}(frame.Payload)
		case "cancel":
			stopRun()
		}
	}
}
