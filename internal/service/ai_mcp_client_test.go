package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	aidomain "aegis/internal/domain/ai"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// 这组测试用官方 SDK 的服务端（NewStreamableHTTPHandler）当对手盘，
// 走真实 HTTP 往返：握手、tools/list、tools/call、鉴权头注入、错误收敛
// 全部按线上路径验证 —— 客户端与服务端同 SDK，协议兼容由上游保证，
// 这里盯的是我们的封装语义。

type mcpEchoArgs struct {
	Text string `json:"text"`
}

// newMCPTestServer 起一个带 echo / boom 两个工具的真 MCP 服务器。
func newMCPTestServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "回显文本"},
		func(_ context.Context, _ *mcp.CallToolRequest, args mcpEchoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "echo:" + args.Text}},
			}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "boom", Description: "总是失败"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ mcpEchoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "内部炸了"}},
			}, nil, nil
		})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	var authedRequests atomic.Int64
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") == "sekrit" {
			authedRequests.Add(1)
		}
		handler.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(wrapped)
	t.Cleanup(ts.Close)
	return ts, &authedRequests
}

func TestMCPClientRoundTrip(t *testing.T) {
	ts, authed := newMCPTestServer(t)
	client := newAIMCPClient(zap.NewNop(), aidomain.MCPServer{
		Name: "测试台", URL: ts.URL,
		Headers: map[string]string{"X-Api-Key": "sekrit"},
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("tools/list 失败：%v", err)
	}
	byName := map[string]aidomain.MCPTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	echo, ok := byName["echo"]
	if !ok {
		t.Fatalf("工具清单缺 echo：%v", tools)
	}
	if echo.Description != "回显文本" {
		t.Fatalf("echo 描述不对：%s", echo.Description)
	}
	// AddTool 会从 Go 类型推断 JSON Schema，客户端必须原样带回（喂给 LLM 的工具声明）。
	var schema struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(echo.InputSchema, &schema); err != nil || schema.Type != "object" {
		t.Fatalf("echo 的 InputSchema 不是对象 schema：%s（err=%v）", echo.InputSchema, err)
	}
	if !strings.Contains(string(schema.Properties), "text") {
		t.Fatalf("schema 应包含 text 属性：%s", schema.Properties)
	}

	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"text":"你好"}`))
	if err != nil {
		t.Fatalf("tools/call 失败：%v", err)
	}
	if result != "echo:你好" {
		t.Fatalf("回显结果不对：%q", result)
	}

	if authed.Load() == 0 {
		t.Fatal("自定义鉴权头没有随请求注入")
	}
}

func TestMCPClientToolError(t *testing.T) {
	ts, _ := newMCPTestServer(t)
	client := newAIMCPClient(zap.NewNop(), aidomain.MCPServer{Name: "测试台", URL: ts.URL})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := client.CallTool(ctx, "boom", json.RawMessage(`{"text":"x"}`))
	if err == nil {
		t.Fatal("IsError 结果应转成 error")
	}
	if !strings.Contains(err.Error(), "boom 执行失败") || !strings.Contains(err.Error(), "内部炸了") {
		t.Fatalf("错误消息应带工具名与文本内容：%v", err)
	}
}

func TestMCPClientConnectFailure(t *testing.T) {
	// 一个只会 404 的端点：Streamable 与旧版 SSE 两种传输都该失败，
	// 错误必须收敛成带服务器名的一行短消息。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "<html>Not Found</html>", http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	client := newAIMCPClient(zap.NewNop(), aidomain.MCPServer{Name: "坏站", URL: ts.URL})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := client.ListTools(ctx)
	if err == nil {
		t.Fatal("连接失败应报错")
	}
	if !strings.Contains(err.Error(), "MCP 服务器「坏站」连接失败") {
		t.Fatalf("错误应带服务器名前缀：%v", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("错误消息不该带换行（外部报错要收敛成一行）：%q", err.Error())
	}
}

func TestMCPClientCloseWithoutConnect(t *testing.T) {
	client := newAIMCPClient(zap.NewNop(), aidomain.MCPServer{Name: "从未建连", URL: "http://127.0.0.1:0"})
	client.Close() // 不该恐慌
	client.Close() // 重复关闭也不该恐慌
}
