package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/pkg/egress"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// aiMCPClient 基于官方 MCP Go SDK（modelcontextprotocol/go-sdk）的客户端封装。
// 协议握手、会话管理、Streamable HTTP 的 SSE 解码、断线重连全部交给 SDK；
// 这里只保留平台语义：懒建连、双传输回退、自定义鉴权头、错误消息收敛。
//
// 对外仍只暴露 Agent 需要的两步：tools/list 与 tools/call —— resources /
// prompts 在「给函数作者接外部工具」这个场景里没有消费方。
type aiMCPClient struct {
	log    *zap.Logger
	server aidomain.MCPServer

	mu      sync.Mutex
	session *mcp.ClientSession
	// cancelLife 会话生命周期的开关：Close 时掐掉，连带终止 SDK 里的后台流。
	cancelLife context.CancelFunc
}

func newAIMCPClient(log *zap.Logger, server aidomain.MCPServer) *aiMCPClient {
	return &aiMCPClient{log: log, server: server}
}

// mcpMaxResponseBytes 单次响应上限。MCP 服务器是外部系统，
// 一个失控的工具结果不该把 Agent 的内存与上下文一起撑爆。
const mcpMaxResponseBytes = 4 << 20

func (c *aiMCPClient) httpClient() *http.Client {
	client := egress.NewClient(egress.Profile{
		Name:                  "ai.mcp",
		Timeout:               0,
		DialTimeout:           10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		// MCP 地址由管理员配置，通常在内网 —— 不做私网目标拦截。
	})
	client.Transport = &mcpRoundTripper{
		base:    client.Transport,
		headers: c.server.Headers,
	}
	return client
}

// mcpRoundTripper 给 SDK 发出的每个请求注入管理员配置的鉴权头，
// 并给响应体加上限 —— SDK 自己不设防，读多少全听服务器的。
type mcpRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *mcpRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	for key, value := range t.headers {
		request.Header.Set(key, value)
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &cappedReadCloser{reader: io.LimitReader(response.Body, mcpMaxResponseBytes), closer: response.Body}
	return response, nil
}

type cappedReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (c *cappedReadCloser) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *cappedReadCloser) Close() error               { return c.closer.Close() }

// connect 懒建立会话（每个客户端实例一条，寿命 = 一轮 Agent 对话）。
//
// 首选 Streamable HTTP（2025-03-26 起的现行传输），失败再试一次旧版
// HTTP+SSE（2024-11-05）—— 存量 MCP 服务器不少还停在 /sse 端点上，
// 官方 SDK 两种都会说，回退后老服务器照用。
func (c *aiMCPClient) connect(ctx context.Context) (*mcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		return c.session, nil
	}

	// 会话要活过本次调用（后续 CallTool 还用它），生命周期不能挂在
	// 调用方的短时 ctx 上：从中剥离出可长活的 lifeCtx，Close 时才掐。
	// 旧版 SSE 传输会把常驻事件流绑在 Connect 的 ctx 上，这一步是必须的。
	lifeCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	httpClient := c.httpClient()
	transports := []mcp.Transport{
		&mcp.StreamableClientTransport{
			Endpoint:   c.server.URL,
			HTTPClient: httpClient,
			// 只做「请求 → 响应」：不开常驻 SSE 流，服务器主动通知没有消费方。
			DisableStandaloneSSE: true,
		},
		&mcp.SSEClientTransport{Endpoint: c.server.URL, HTTPClient: httpClient},
	}

	var lastErr error
	for _, transport := range transports {
		session, err := c.connectWith(ctx, lifeCtx, transport)
		if err == nil {
			c.session = session
			c.cancelLife = cancel
			return session, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break // 调用方超时/取消：别再拿第二种传输撞一次
		}
	}
	cancel()
	return nil, fmt.Errorf("MCP 服务器「%s」连接失败：%s", c.server.Name, mcpErrText(lastErr))
}

// connectWith 用 lifeCtx 建连，但握手时长受调用方 ctx 约束。
// 两个 ctx 不能是同一个：调用方的超时只该管「等多久」，不该管「会话活多久」。
func (c *aiMCPClient) connectWith(ctx, lifeCtx context.Context, transport mcp.Transport) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "aegis-agent", Version: "1.0"}, nil)
	type outcome struct {
		session *mcp.ClientSession
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		session, err := client.Connect(lifeCtx, transport, nil)
		done <- outcome{session, err}
	}()
	select {
	case result := <-done:
		return result.session, result.err
	case <-ctx.Done():
		// 握手超时：放弃这次尝试。迟到的会话直接关掉，别泄漏连接。
		go func() {
			if result := <-done; result.session != nil {
				_ = result.session.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// Close 结束会话并释放底层连接。可以对未建连的客户端安全调用。
func (c *aiMCPClient) Close() {
	c.mu.Lock()
	session := c.session
	cancel := c.cancelLife
	c.session = nil
	c.cancelLife = nil
	c.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
	if cancel != nil {
		cancel()
	}
}

// ListTools 列出服务器声明的工具。
func (c *aiMCPClient) ListTools(ctx context.Context) ([]aidomain.MCPTool, error) {
	session, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("MCP 服务器「%s」报错：%s", c.server.Name, mcpErrText(err))
	}
	tools := make([]aidomain.MCPTool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		item := aidomain.MCPTool{Name: tool.Name, Description: tool.Description}
		if tool.InputSchema != nil {
			if schema, err := json.Marshal(tool.InputSchema); err == nil {
				item.InputSchema = schema
			}
		}
		tools = append(tools, item)
	}
	return tools, nil
}

// CallTool 调一个工具，把 content 里的文本段拼成结果字符串。
func (c *aiMCPClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	session, err := c.connect(ctx)
	if err != nil {
		return "", err
	}
	params := &mcp.CallToolParams{Name: name}
	if len(arguments) > 0 && string(arguments) != "null" {
		params.Arguments = arguments
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		return "", fmt.Errorf("MCP 服务器「%s」报错：%s", c.server.Name, mcpErrText(err))
	}
	var builder strings.Builder
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(text.Text)
		}
	}
	text := builder.String()
	if result.IsError {
		return "", fmt.Errorf("MCP 工具 %s 执行失败：%s", name, text)
	}
	return text, nil
}

// mcpErrText 收敛 SDK 错误为一行短消息：外部服务器的报错可能携带
// 整页 HTML（拦截页）或超长堆栈，原样外传只会污染界面与模型上下文。
func mcpErrText(err error) string {
	message := strings.TrimSpace(err.Error())
	if parsed := parseUpstreamErrorMessage([]byte(message)); parsed != "" {
		message = parsed
	}
	if index := strings.IndexByte(message, '\n'); index >= 0 {
		message = message[:index]
	}
	const limit = 300
	if len(message) > limit {
		message = message[:limit] + "…"
	}
	return message
}

// closeMCPClients 一轮对话收尾时释放全部 MCP 会话。
func (run *aiAgentRun) closeMCPClients() {
	for _, client := range run.mcpClients {
		client.Close()
	}
}
