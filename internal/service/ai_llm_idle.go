package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// 上游空闲看门狗。
//
// http.Client 的 Timeout 恒为 0（流式响应可以持续很久），响应头另有 ResponseHeaderTimeout；
// 但响应开始之后再没有任何限制：连接半开、上游进程卡死时，这次调用会一直挂着，直到前面的
// 反向代理按读超时把客户端切掉 —— 客户端看到的是流无声无息地断了。
//
// 这里给响应体加一个「两次收到字节之间」的期限：期限内哪怕只来一个字节（SSE 的注释保活也算）
// 就重新计时；超时就取消这次请求，读响应体的一方拿到一个可识别的上游错误。
// 计费上它与其它上游中途失败一样：整笔放回、不扣费（ai_gateway_relay.go）。

// errAIUpstreamIdle 看门狗触发时取消请求的原因（只在本文件内部区分用）。
var errAIUpstreamIdle = errors.New("ai upstream idle")

type aiIdleTransport struct {
	base     http.RoundTripper
	provider string
	idle     time.Duration
}

func (t aiIdleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.idle <= 0 {
		return t.base.RoundTrip(req)
	}
	ctx, cancel := context.WithCancelCause(req.Context())
	response, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil || response == nil || response.Body == nil {
		cancel(nil)
		return response, err
	}
	body := &aiIdleBody{ReadCloser: response.Body, cancel: cancel, idle: t.idle, provider: t.provider}
	body.timer = time.AfterFunc(t.idle, body.fire)
	response.Body = body
	return response, nil
}

// aiIdleBody 每读到字节就重新计时；超时取消请求，此后的读返回上游空闲错误。
type aiIdleBody struct {
	io.ReadCloser
	cancel    context.CancelCauseFunc
	idle      time.Duration
	provider  string
	timer     *time.Timer
	fired     atomic.Bool
	closeOnce sync.Once
}

func (b *aiIdleBody) fire() {
	b.fired.Store(true)
	b.cancel(errAIUpstreamIdle)
}

func (b *aiIdleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 && !b.fired.Load() {
		b.timer.Reset(b.idle)
	}
	if err != nil && err != io.EOF && b.fired.Load() {
		return n, &aiUpstreamError{
			Provider:  b.provider,
			Message:   fmt.Sprintf("上游连续 %s 没有返回任何数据，已主动断开", b.idle),
			Retryable: true,
		}
	}
	return n, err
}

func (b *aiIdleBody) Close() error {
	err := b.ReadCloser.Close()
	b.closeOnce.Do(func() {
		b.timer.Stop()
		b.cancel(nil)
	})
	return err
}
