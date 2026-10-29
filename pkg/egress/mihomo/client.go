package mihomo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client 核心的 external-controller 接口。控制器只监听回环地址，
// 因此这里用一个普通的 http.Client，不经过出海网关。
type Client struct {
	base   string
	secret string
	http   *http.Client
}

// NewClient 构造控制器客户端，addr 形如 127.0.0.1:9090。
func NewClient(addr, secret string) *Client {
	return &Client{
		base:   "http://" + addr,
		secret: secret,
		http:   &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 4}},
	}
}

// APIError 控制器返回的非 2xx 响应。
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("mihomo 控制器返回 %d", e.Status)
	}
	return fmt.Sprintf("mihomo: %s", e.Message)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &failure)
		if failure.Message == "" {
			failure.Message = strings.TrimSpace(string(data))
		}
		return &APIError{Status: resp.StatusCode, Message: failure.Message}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Version 核心版本。
type Version struct {
	Version string `json:"version"`
	Meta    bool   `json:"meta"`
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var out Version
	err := c.do(ctx, http.MethodGet, "/version", nil, &out)
	return out, err
}

// ReloadConfig 让核心从磁盘重新加载配置（路径必须在核心的家目录内）。
func (c *Client) ReloadConfig(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodPut, "/configs?force=true", map[string]string{"path": path}, nil)
}

// DelayHistory 一次延迟测试记录。
type DelayHistory struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

// ProxyInfo /proxies 里的一项（节点或策略组）。
type ProxyInfo struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	UDP     bool           `json:"udp"`
	Alive   bool           `json:"alive"`
	History []DelayHistory `json:"history"`
	// 策略组才有
	Now     string   `json:"now,omitempty"`
	All     []string `json:"all,omitempty"`
	Fixed   string   `json:"fixed,omitempty"`
	Hidden  bool     `json:"hidden,omitempty"`
	TestURL string   `json:"testUrl,omitempty"`
	Icon    string   `json:"icon,omitempty"`
	// ProviderName 来自订阅的节点
	ProviderName string `json:"provider-name,omitempty"`
}

// LastDelay 最近一次延迟（毫秒），0 表示未测或失败。
func (p ProxyInfo) LastDelay() int {
	if len(p.History) == 0 {
		return 0
	}
	return p.History[len(p.History)-1].Delay
}

func (c *Client) Proxies(ctx context.Context) (map[string]ProxyInfo, error) {
	var out struct {
		Proxies map[string]ProxyInfo `json:"proxies"`
	}
	err := c.do(ctx, http.MethodGet, "/proxies", nil, &out)
	return out.Proxies, err
}

// Select 切换 select 类策略组的当前节点。
func (c *Client) Select(ctx context.Context, group, name string) error {
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// Unfix 取消 url-test / fallback 策略组的手动固定。
func (c *Client) Unfix(ctx context.Context, group string) error {
	return c.do(ctx, http.MethodDelete, "/proxies/"+url.PathEscape(group), nil, nil)
}

func delayQuery(testURL string, timeout time.Duration, expected string) string {
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", strconv.Itoa(int(timeout/time.Millisecond)))
	if expected != "" {
		q.Set("expected", expected)
	}
	return q.Encode()
}

// Delay 测单个节点（或策略组当前选中的节点）的延迟。
func (c *Client) Delay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error) {
	var out struct {
		Delay int `json:"delay"`
	}
	err := c.do(ctx, http.MethodGet, "/proxies/"+url.PathEscape(name)+"/delay?"+delayQuery(testURL, timeout, ""), nil, &out)
	return out.Delay, err
}

// GroupDelay 测策略组内全部节点，返回 名字 → 延迟（失败的节点不在结果里）。
func (c *Client) GroupDelay(ctx context.Context, group, testURL string, timeout time.Duration) (map[string]int, error) {
	out := map[string]int{}
	err := c.do(ctx, http.MethodGet, "/group/"+url.PathEscape(group)+"/delay?"+delayQuery(testURL, timeout, ""), nil, &out)
	return out, err
}

// ProviderInfo 订阅（proxy-provider）状态。
type ProviderInfo struct {
	Name             string      `json:"name"`
	Type             string      `json:"type"`
	VehicleType      string      `json:"vehicleType"`
	UpdatedAt        *time.Time  `json:"updatedAt,omitempty"`
	Proxies          []ProxyInfo `json:"proxies"`
	SubscriptionInfo *struct {
		Upload   int64 `json:"Upload"`
		Download int64 `json:"Download"`
		Total    int64 `json:"Total"`
		Expire   int64 `json:"Expire"`
	} `json:"subscriptionInfo,omitempty"`
}

func (c *Client) Providers(ctx context.Context) (map[string]ProviderInfo, error) {
	var out struct {
		Providers map[string]ProviderInfo `json:"providers"`
	}
	err := c.do(ctx, http.MethodGet, "/providers/proxies", nil, &out)
	return out.Providers, err
}

func (c *Client) UpdateProvider(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPut, "/providers/proxies/"+url.PathEscape(name), nil, nil)
}

func (c *Client) HealthcheckProvider(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodGet, "/providers/proxies/"+url.PathEscape(name)+"/healthcheck", nil, nil)
}

// RuleProviderInfo 规则集状态。
type RuleProviderInfo struct {
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	VehicleType string     `json:"vehicleType"`
	Behavior    string     `json:"behavior"`
	Format      string     `json:"format"`
	RuleCount   int        `json:"ruleCount"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
}

func (c *Client) RuleProviders(ctx context.Context) (map[string]RuleProviderInfo, error) {
	var out struct {
		Providers map[string]RuleProviderInfo `json:"providers"`
	}
	err := c.do(ctx, http.MethodGet, "/providers/rules", nil, &out)
	return out.Providers, err
}

func (c *Client) UpdateRuleProvider(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPut, "/providers/rules/"+url.PathEscape(name), nil, nil)
}

// RuleInfo 核心当前生效的一条规则。
type RuleInfo struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
	Size    int    `json:"size"`
	Extra   *struct {
		Disabled  bool   `json:"disabled"`
		HitCount  uint64 `json:"hitCount"`
		HitAt     string `json:"hitAt"`
		MissCount uint64 `json:"missCount"`
	} `json:"extra,omitempty"`
}

func (c *Client) Rules(ctx context.Context) ([]RuleInfo, error) {
	var out struct {
		Rules []RuleInfo `json:"rules"`
	}
	err := c.do(ctx, http.MethodGet, "/rules", nil, &out)
	return out.Rules, err
}

// Connection 一条在途连接。
type Connection struct {
	ID          string         `json:"id"`
	Metadata    map[string]any `json:"metadata"`
	Upload      int64          `json:"upload"`
	Download    int64          `json:"download"`
	Start       time.Time      `json:"start"`
	Chains      []string       `json:"chains"`
	Rule        string         `json:"rule"`
	RulePayload string         `json:"rulePayload"`
}

// Connections 在途连接快照与累计流量。
type Connections struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Memory        int64        `json:"memory"`
	Connections   []Connection `json:"connections"`
}

func (c *Client) Connections(ctx context.Context) (Connections, error) {
	var out Connections
	err := c.do(ctx, http.MethodGet, "/connections", nil, &out)
	return out, err
}

// CloseConnection 关闭一条连接；id 为空时关闭全部。
func (c *Client) CloseConnection(ctx context.Context, id string) error {
	if id == "" {
		return c.do(ctx, http.MethodDelete, "/connections", nil, nil)
	}
	return c.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil)
}

// DNSQuery 经核心的 DNS 模块解析。
func (c *Client) DNSQuery(ctx context.Context, name, qtype string) (map[string]any, error) {
	q := url.Values{}
	q.Set("name", name)
	if qtype != "" {
		q.Set("type", qtype)
	}
	out := map[string]any{}
	err := c.do(ctx, http.MethodGet, "/dns/query?"+q.Encode(), nil, &out)
	return out, err
}

// UpdateGeo 让核心重新下载 GeoIP / GeoSite 数据库。
func (c *Client) UpdateGeo(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/configs/geo", map[string]string{}, nil)
}

// FlushDNSCache 清空核心的 DNS 缓存。
func (c *Client) FlushDNSCache(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/cache/dns/flush", nil, nil)
}

// Stream 读取一条按行推送 JSON 的接口（/traffic、/memory），每行回调一次，直到 ctx 取消或连接断开。
func (c *Client) Stream(ctx context.Context, path string, onLine func([]byte)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	client := &http.Client{Transport: c.http.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) > 0 {
			onLine(line)
		}
	}
	return scanner.Err()
}
