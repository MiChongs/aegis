package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/pkg/egress"

	"go.uber.org/zap"
)

// aiMCPClient 是 MCP（Model Context Protocol）的最小可用客户端，
// 只走 Streamable HTTP 传输：POST JSON-RPC，响应可能是 application/json
// 也可能是 text/event-stream —— 两种都收。
//
// 只实现 Agent 需要的三步：initialize → tools/list → tools/call。
// resources / prompts / sampling 这些能力在「给函数作者接外部工具」这个场景里
// 没有消费方，实现了也只是死代码。
type aiMCPClient struct {
	log    *zap.Logger
	server aidomain.MCPServer

	mu        sync.Mutex
	sessionID string
	// initialized 每个客户端实例握手一次；实例的寿命是一轮 Agent 对话。
	initialized bool
}

func newAIMCPClient(log *zap.Logger, server aidomain.MCPServer) *aiMCPClient {
	return &aiMCPClient{log: log, server: server}
}

// mcpProtocolVersion 客户端声明的协议版本。
const mcpProtocolVersion = "2025-06-18"

// mcpMaxResponseBytes 单次响应上限。MCP 服务器是外部系统，
// 一个失控的工具结果不该把 Agent 的内存与上下文一起撑爆。
const mcpMaxResponseBytes = 4 << 20

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *aiMCPClient) httpClient() *http.Client {
	return egress.NewClient(egress.Profile{
		Name:                  "ai.mcp",
		Timeout:               0,
		DialTimeout:           10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		// MCP 地址由管理员配置，通常在内网 —— 不做私网目标拦截。
	})
}

// call 发一个 JSON-RPC 请求并解出结果。notification 为 true 时不等待结果体。
func (c *aiMCPClient) call(ctx context.Context, id int64, method string, params any, notification bool) (json.RawMessage, error) {
	if notification {
		id = 0 // 通知不带 id；字段是 omitempty，0 即省略
	}
	payload, err := json.Marshal(mcpRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.server.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", mcpProtocolVersion)
	c.mu.Lock()
	if c.sessionID != "" {
		request.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()
	for key, value := range c.server.Headers {
		request.Header.Set(key, value)
	}

	response, err := c.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("MCP 服务器「%s」连接失败：%w", c.server.Name, err)
	}
	defer response.Body.Close()

	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		c.mu.Lock()
		c.sessionID = session
		c.mu.Unlock()
	}
	if response.StatusCode == http.StatusAccepted {
		return nil, nil // 通知被接受，无响应体
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return nil, fmt.Errorf("MCP 服务器「%s」返回 %d：%s", c.server.Name, response.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := response.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return c.readSSEResult(response.Body, id)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, mcpMaxResponseBytes))
	if err != nil {
		return nil, err
	}
	if notification || len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var parsed mcpResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("MCP 响应不是合法 JSON-RPC：%w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("MCP 服务器「%s」报错：%s", c.server.Name, parsed.Error.Message)
	}
	return parsed.Result, nil
}

// readSSEResult 从事件流里等出与本次请求 id 匹配的那条响应。
func (c *aiMCPClient) readSSEResult(body io.Reader, id int64) (json.RawMessage, error) {
	var result json.RawMessage
	var callErr error
	limited := io.LimitReader(body, mcpMaxResponseBytes)
	err := readSSE(limited, func(event sseEvent) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		var parsed mcpResponse
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return nil
		}
		var parsedID int64
		if len(parsed.ID) > 0 {
			_ = json.Unmarshal(parsed.ID, &parsedID)
		}
		if parsedID != id {
			return nil // 服务器主动推的其它消息（日志/进度），跳过
		}
		if parsed.Error != nil {
			callErr = fmt.Errorf("MCP 服务器「%s」报错：%s", c.server.Name, parsed.Error.Message)
		} else {
			result = parsed.Result
		}
		return errStopStream
	})
	if callErr != nil {
		return nil, callErr
	}
	if err != nil && !errors.Is(err, errStopStream) {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("MCP 服务器「%s」没有返回本次请求的结果", c.server.Name)
	}
	return result, nil
}

func (c *aiMCPClient) ensureInitialized(ctx context.Context) error {
	c.mu.Lock()
	done := c.initialized
	c.mu.Unlock()
	if done {
		return nil
	}
	_, err := c.call(ctx, 1, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aegis-agent", "version": "1.0"},
	}, false)
	if err != nil {
		return err
	}
	// initialized 通知失败不阻断：多数服务器不强求它。
	if _, err := c.call(ctx, 0, "notifications/initialized", map[string]any{}, true); err != nil {
		c.log.Debug("mcp initialized notification failed", zap.String("server", c.server.Name), zap.Error(err))
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// ListTools 列出服务器声明的工具。
func (c *aiMCPClient) ListTools(ctx context.Context) ([]aidomain.MCPTool, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	result, err := c.call(ctx, 2, "tools/list", map[string]any{}, false)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, fmt.Errorf("tools/list 结果解析失败：%w", err)
	}
	tools := make([]aidomain.MCPTool, 0, len(parsed.Tools))
	for _, tool := range parsed.Tools {
		tools = append(tools, aidomain.MCPTool{
			Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema,
		})
	}
	return tools, nil
}

// CallTool 调一个工具，把 content 里的文本段拼成结果字符串。
func (c *aiMCPClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return "", err
	}
	var input any = map[string]any{}
	if len(arguments) > 0 {
		var parsed any
		if err := json.Unmarshal(arguments, &parsed); err == nil && parsed != nil {
			input = parsed
		}
	}
	result, err := c.call(ctx, 3, "tools/call", map[string]any{
		"name": name, "arguments": input,
	}, false)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return "", fmt.Errorf("tools/call 结果解析失败：%w", err)
	}
	var builder strings.Builder
	for _, item := range parsed.Content {
		if item.Type == "text" {
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(item.Text)
		}
	}
	text := builder.String()
	if parsed.IsError {
		return "", fmt.Errorf("MCP 工具 %s 执行失败：%s", name, text)
	}
	return text, nil
}
