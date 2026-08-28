package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialAgentSocket 起一个只挂 serveAIAgentSocket 的测试服务器并建立客户端连接。
func dialAgentSocket(t *testing.T, run aiAgentSocketRun) *websocket.Conn {
	t.Helper()
	upgrader := websocket.Upgrader{Subprotocols: []string{aiAgentWSSubprotocol}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade 失败：%v", err)
			return
		}
		serveAIAgentSocket(r.Context(), conn, run)
	}))
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := websocket.Dialer{Subprotocols: []string{aiAgentWSSubprotocol, "aegis.jwt.test-token"}}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial 失败：%v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendRunFrame(t *testing.T, conn *websocket.Conn, message string) {
	t.Helper()
	frame := map[string]any{"type": "run", "payload": map[string]any{"message": message}}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("发送 run 帧失败：%v", err)
	}
}

func readServerFrame(t *testing.T, conn *websocket.Conn) aiAgentWSServerFrame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var frame aiAgentWSServerFrame
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("读服务端帧失败：%v", err)
	}
	return frame
}

// 完整一轮：chunk 逐帧到达，done 收尾。
func TestAgentSocketRunRoundTrip(t *testing.T) {
	t.Parallel()
	conn := dialAgentSocket(t, func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error {
		if req.Message != "你好" {
			t.Errorf("payload 透传错误，得到 %q", req.Message)
		}
		if err := emit(map[string]any{"type": "text-start", "id": "t1"}); err != nil {
			return err
		}
		return emit(map[string]any{"type": "text-delta", "id": "t1", "delta": "hi"})
	})

	sendRunFrame(t, conn, "你好")

	first := readServerFrame(t, conn)
	if first.Kind != "chunk" {
		t.Fatalf("期望 chunk 帧，得到 %+v", first)
	}
	chunk, _ := first.Chunk.(map[string]any)
	if chunk["type"] != "text-start" {
		t.Fatalf("chunk 载荷错乱：%+v", first.Chunk)
	}
	second := readServerFrame(t, conn)
	if second.Kind != "chunk" {
		t.Fatalf("期望第二个 chunk 帧，得到 %+v", second)
	}
	final := readServerFrame(t, conn)
	if final.Kind != "done" {
		t.Fatalf("期望 done 帧，得到 %+v", final)
	}
}

// 开跑前失败（一个 chunk 都没发出）必须以 error 帧收尾，且不再补 done。
func TestAgentSocketPreRunFailure(t *testing.T) {
	t.Parallel()
	conn := dialAgentSocket(t, func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error {
		return errors.New("消息不能为空")
	})

	sendRunFrame(t, conn, "")
	frame := readServerFrame(t, conn)
	if frame.Kind != "error" || frame.ErrorText != "消息不能为空" {
		t.Fatalf("期望 error 帧，得到 %+v", frame)
	}
}

// 一轮没跑完时的并发 run 请求要被拒绝，且不影响进行中的一轮。
func TestAgentSocketRejectsConcurrentRun(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	conn := dialAgentSocket(t, func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error {
		_ = emit(map[string]any{"type": "start"})
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	sendRunFrame(t, conn, "第一轮")
	if frame := readServerFrame(t, conn); frame.Kind != "chunk" {
		t.Fatalf("第一轮未开跑：%+v", frame)
	}

	sendRunFrame(t, conn, "第二轮")
	busy := readServerFrame(t, conn)
	if busy.Kind != "error" || !strings.Contains(busy.ErrorText, "尚未结束") {
		t.Fatalf("并发 run 未被拒绝：%+v", busy)
	}

	close(release)
	if frame := readServerFrame(t, conn); frame.Kind != "done" {
		t.Fatalf("第一轮未正常收尾：%+v", frame)
	}
}

// cancel 帧取消进行中的一轮：以 done 收尾（不是 error），随后可以开下一轮。
func TestAgentSocketCancel(t *testing.T) {
	t.Parallel()
	conn := dialAgentSocket(t, func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error {
		_ = emit(map[string]any{"type": "start"})
		<-ctx.Done()
		return ctx.Err()
	})

	sendRunFrame(t, conn, "会被取消")
	if frame := readServerFrame(t, conn); frame.Kind != "chunk" {
		t.Fatalf("一轮未开跑：%+v", frame)
	}
	if err := conn.WriteJSON(map[string]any{"type": "cancel"}); err != nil {
		t.Fatalf("发送 cancel 帧失败：%v", err)
	}
	if frame := readServerFrame(t, conn); frame.Kind != "done" {
		t.Fatalf("被取消的一轮应以 done 收尾，得到 %+v", frame)
	}

	// 取消后连接仍可复用
	sendRunFrame(t, conn, "下一轮")
	if frame := readServerFrame(t, conn); frame.Kind != "done" && frame.Kind != "chunk" {
		t.Fatalf("取消后无法开启新一轮：%+v", frame)
	}
}

// 帧不是合法 JSON 时回 error 帧但不断连。
func TestAgentSocketMalformedFrame(t *testing.T) {
	t.Parallel()
	conn := dialAgentSocket(t, func(ctx context.Context, req adminAIAgentStreamRequest, emit func(chunk any) error) error {
		return nil
	})

	if err := conn.WriteMessage(websocket.TextMessage, []byte("not-json")); err != nil {
		t.Fatalf("写坏帧失败：%v", err)
	}
	frame := readServerFrame(t, conn)
	if frame.Kind != "error" {
		t.Fatalf("坏帧应回 error 帧，得到 %+v", frame)
	}

	// 连接未被断开，正常帧仍可用
	sendRunFrame(t, conn, "继续")
	if frame := readServerFrame(t, conn); frame.Kind != "done" {
		t.Fatalf("坏帧后连接不可用：%+v", frame)
	}
}

// 服务端帧的 JSON 形状是前端传输层的契约：kind 必填，chunk 原样透传。
func TestAgentSocketFrameShape(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(aiAgentWSServerFrame{Kind: "chunk", Chunk: map[string]any{"type": "finish"}})
	if err != nil {
		t.Fatalf("marshal 失败：%v", err)
	}
	want := `{"kind":"chunk","chunk":{"type":"finish"}}`
	if string(payload) != want {
		t.Fatalf("帧形状漂移：%s", payload)
	}
}
