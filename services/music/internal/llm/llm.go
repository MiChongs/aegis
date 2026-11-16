// Package llm 内容理解与曲库扩展用的大模型客户端（OpenAI 兼容的 /chat/completions 与 /embeddings）。
// 没配置地址时 Enabled() 为 false，调用方据此关闭相关功能（docs/music-personalization-3.md）。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Client OpenAI 兼容客户端。
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	EmbedModel string
	EmbedDim   int
	HTTP       *http.Client
	sem        chan struct{}
}

// New 构造；baseURL 为空返回 nil（关闭）。
func New(baseURL, apiKey, model, embedModel string, embedDim, concurrency int) *Client {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, EmbedModel: embedModel,
		EmbedDim: embedDim, HTTP: &http.Client{Timeout: 120 * time.Second}, sem: make(chan struct{}, max(concurrency, 1))}
}

// Enabled 是否配置了大模型。
func (c *Client) Enabled() bool { return c != nil && c.BaseURL != "" }

// Usage token 用量。
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// ErrDisabled 没配置大模型。
var ErrDisabled = errors.New("llm not configured")

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.sem }()
	raw, _ := json.Marshal(body)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
		} else {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if err := json.Unmarshal(data, out); err != nil {
					return fmt.Errorf("大模型返回格式不对：%w", err)
				}
				return nil
			}
			lastErr = fmt.Errorf("大模型返回 %d：%s", resp.StatusCode, truncate(string(data), 300))
			// 4xx（除 429）重试没有意义
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return lastErr
			}
		}
		select {
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ChatJSON 让模型只输出一个 JSON 对象，解析进 out。
func (c *Client) ChatJSON(ctx context.Context, system, user string, out any) (Usage, error) {
	body := map[string]any{
		"model":           c.Model,
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := c.post(ctx, "/chat/completions", body, &resp); err != nil {
		return Usage{}, err
	}
	if len(resp.Choices) == 0 {
		return resp.Usage, errors.New("大模型没有返回内容")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	// 有的模型会包一层 ```json
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	if i := strings.Index(content, "{"); i > 0 {
		content = content[i:]
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), out); err != nil {
		return resp.Usage, fmt.Errorf("大模型输出不是合法 JSON：%w", err)
	}
	return resp.Usage, nil
}

// Embed 一批文本的语义向量（L2 归一化，长度固定为 EmbedDim：长了截断、短了补零）。
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, Usage, error) {
	body := map[string]any{"model": c.EmbedModel, "input": texts}
	if c.EmbedDim > 0 {
		body["dimensions"] = c.EmbedDim
	}
	var resp struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage Usage `json:"usage"`
	}
	if err := c.post(ctx, "/embeddings", body, &resp); err != nil {
		return nil, Usage{}, err
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			continue
		}
		out[d.Index] = Normalize(d.Embedding, c.EmbedDim)
	}
	for i, v := range out {
		if v == nil {
			return nil, resp.Usage, fmt.Errorf("第 %d 条文本没有返回向量", i)
		}
	}
	return out, resp.Usage, nil
}

// Normalize 截断或补零到 dim 维并 L2 归一化。
func Normalize(v []float64, dim int) []float32 {
	if dim <= 0 {
		dim = len(v)
	}
	out := make([]float32, dim)
	var n float64
	for i := 0; i < dim && i < len(v); i++ {
		n += v[i] * v[i]
	}
	n = math.Sqrt(n)
	for i := 0; i < dim && i < len(v); i++ {
		if n > 0 {
			out[i] = float32(v[i] / n)
		}
	}
	return out
}
