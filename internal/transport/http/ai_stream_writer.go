package httptransport

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 对话流（Chat Completions / Messages 的 SSE）的写出层：应用函数网关与用户中转共用。
//
// 流式对话前面隔着好几层反向代理（CDN 的 nginx 默认 60 秒读超时、Cloudflare 约 100 秒），
// 推理模型出第一个字前可能想上几分钟，工具调用的入参有的上游也会憋一阵才吐。期间一个字节
// 都不写，代理就按读超时切断：响应头还没发时客户端拿到 504 / 524 的 HTML，已经开流时流
// 无声无息地断掉 —— 客户端看起来就是「生成到一半自己停了」。
//
// 所以这里：
//   - 开流（Begin）就立刻回 200 与 SSE 响应头并 Flush；用户中转在暂扣成功后开流，
//     没调用 Begin 的（应用函数网关）在第一个增量或第一次心跳时开流；
//   - 每 aiStreamHeartbeatEvery 写一次心跳（OpenAI 形状是 SSE 注释行 `: ping`，
//     Anthropic 形状是官方的 `event: ping`），SSE 解析器都会忽略它；
//   - 解除服务器的 WriteTimeout：它是整个响应的绝对期限，任何有限值都会把长流拦腰截断；
//   - 心跳与数据块在同一把锁下写，处理器返回前停掉心跳并等它退出（gin 会复用 Context）。
//
// 开流之后出错只能以协议里的错误事件收尾（HTTP 状态改不了），见 streamOpenAIChat / streamAnthropicChat。

// aiStreamHeartbeatEvery 心跳间隔。变量而不是常量：测试把它调短。
var aiStreamHeartbeatEvery = 15 * time.Second

var errAIStreamClientGone = errors.New("客户端已断开")

var (
	aiStreamPingOpenAI    = []byte(": ping\n\n")
	aiStreamPingAnthropic = []byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")
)

type aiSSEStream struct {
	c    *gin.Context
	ping []byte

	mu     sync.Mutex
	begun  bool
	broken bool

	stop chan struct{}
	done chan struct{}
}

// newAISSEStream 解除写超时并起心跳；调用方必须 defer Close()。
func newAISSEStream(c *gin.Context, ping []byte) *aiSSEStream {
	// 失败（底层不支持，如测试里的 recorder）就沿用服务器设置，不影响写出。
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	s := &aiSSEStream{c: c, ping: ping, stop: make(chan struct{}), done: make(chan struct{})}
	go s.heartbeat()
	return s
}

func (s *aiSSEStream) heartbeat() {
	defer close(s.done)
	ticker := time.NewTicker(aiStreamHeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if err := s.write(s.ping); err != nil {
				return
			}
		}
	}
}

// Close 停掉心跳并等它退出。处理器返回前调用。
func (s *aiSSEStream) Close() {
	close(s.stop)
	<-s.done
}

// beginLocked 回 200 与 SSE 响应头。响应头只在这里（持锁）写，心跳协程与处理器不会同时改它。
func (s *aiSSEStream) beginLocked() {
	if s.begun {
		return
	}
	s.begun = true
	header := s.c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	// no-transform：拦住链路上任何压缩/改写中间层的响应缓冲（详见 ai_agent_handlers.go）。
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	s.c.Status(http.StatusOK)
	s.c.Writer.WriteHeaderNow()
	s.c.Writer.Flush()
}

// Begin 开流。requestNo 非空时先写 X-Aegis-Request-No（心跳抢先开了流就写不进去了，只是少个头）。
func (s *aiSSEStream) Begin(requestNo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.begun && requestNo != "" {
		s.c.Header("X-Aegis-Request-No", requestNo)
	}
	s.beginLocked()
}

// Started 流是否已经开了（开了就只能以错误事件收尾）。
func (s *aiSSEStream) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.begun
}

// write 写一段完整的 SSE 文本并 Flush；还没开流就先开流。写失败后不再写。
func (s *aiSSEStream) write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return errAIStreamClientGone
	}
	s.beginLocked()
	if _, err := s.c.Writer.Write(data); err != nil {
		s.broken = true
		return err
	}
	s.c.Writer.Flush()
	return nil
}

// failBeforeStart 还没开流时回一个普通的错误响应；已经开流返回 false，调用方改写错误事件。
func (s *aiSSEStream) failBeforeStart(status int, body any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.begun {
		return false
	}
	// 从此不再开流：心跳之后的写都当作客户端已走。
	s.begun, s.broken = true, true
	s.c.JSON(status, body)
	return true
}
