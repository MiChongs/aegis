package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// probeConcurrency 同时探测的端点上限。端点数量通常个位数，
// 这个上限只是防止有人配了几十条线路时探测把出口打满。
const probeConcurrency = 8

// ProbeResult 单个端点的探测结果。
type ProbeResult struct {
	Endpoint  string   `json:"endpoint"`
	OK        bool     `json:"ok"`
	LatencyMS int64    `json:"latencyMs"`
	Error     string   `json:"error,omitempty"`
	ProbeURL  string   `json:"probeUrl,omitempty"`
	Chain     []string `json:"chain,omitempty"`
}

// Start 拉起后台健康探测循环。重复调用只生效一次；ctx 取消即退出。
func (g *Gateway) Start(ctx context.Context) {
	g.startOnce.Do(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		g.healthCancel = cancel
		g.healthDone = make(chan struct{})
		go g.healthLoop(loopCtx)
	})
}

func (g *Gateway) healthLoop(ctx context.Context) {
	defer close(g.healthDone)

	timer := time.NewTimer(g.probeInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			snap := g.snap.Load()
			if snap != nil && snap.cfg.Enabled && snap.cfg.Health.Enabled {
				g.ProbeAll(ctx)
			}
			// 每轮重新读间隔：热重载改了探测频率无需重启循环。
			timer.Reset(g.probeInterval())
		}
	}
}

func (g *Gateway) probeInterval() time.Duration {
	snap := g.snap.Load()
	if snap == nil || snap.probeInterval <= 0 {
		return DefaultHealthInterval
	}
	return snap.probeInterval
}

// ProbeAll 立即探测全部启用端点并写回健康状态。
// 管理端「立即检测」与后台循环共用这一条路径。
func (g *Gateway) ProbeAll(ctx context.Context) []ProbeResult {
	snap := g.snap.Load()
	if snap == nil {
		return nil
	}
	targets := make([]*Endpoint, 0, len(snap.endpoints))
	for _, endpoint := range snap.endpoints {
		if endpoint.enabled() {
			targets = append(targets, endpoint)
		}
	}
	results := make([]ProbeResult, len(targets))
	sem := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup
	for i, endpoint := range targets {
		wg.Add(1)
		go func(idx int, item *Endpoint) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = ProbeResult{Endpoint: item.Name(), Error: ctx.Err().Error()}
				return
			}
			results[idx] = g.probeEndpoint(ctx, snap, item, true)
		}(i, endpoint)
	}
	wg.Wait()

	for _, result := range results {
		if !result.OK {
			g.log.Warn("出海端点探测失败",
				zap.String("endpoint", result.Endpoint),
				zap.String("probeUrl", result.ProbeURL),
				zap.String("error", result.Error),
			)
		}
	}
	return results
}

// probeEndpoint 通过指定端点访问探测地址。record=false 时只报告结果、
// 不改动健康状态，供管理端「试一下这条线路」使用。
func (g *Gateway) probeEndpoint(ctx context.Context, snap *snapshot, endpoint *Endpoint, record bool) ProbeResult {
	probeURL := endpoint.cfg.ProbeURL
	if probeURL == "" {
		probeURL = snap.cfg.Health.ProbeURL
	}
	timeout := snap.probeTimeout
	if timeout <= 0 {
		timeout = DefaultHealthTimeout
	}
	result := ProbeResult{Endpoint: endpoint.Name(), ProbeURL: probeURL, Chain: endpoint.chain()}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	err := runProbe(probeCtx, endpoint, probeURL)
	latency := time.Since(start)

	result.LatencyMS = latency.Milliseconds()
	if err != nil {
		result.Error = err.Error()
	} else {
		result.OK = true
	}
	if record {
		endpoint.recordProbe(latency, err, snap.failureThreshold, snap.successThreshold)
	}
	return result
}

// runProbe 探测地址为空时退化成到端点自身的 TCP 连通性检查——
// 内网跳板没有出网能力时，HTTP 探测会永远失败并误判整条链路。
func runProbe(ctx context.Context, endpoint *Endpoint, probeURL string) error {
	if probeURL == "" {
		conn, err := endpoint.dialSelf(ctx, "tcp")
		if err != nil {
			return err
		}
		return conn.Close()
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return endpoint.dial(ctx, network, address, false, 0, 0)
			},
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: DefaultTLSHandshakeTimeout,
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("探测地址无效: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 探测只关心「链路能不能通」，响应体一律丢弃但要读完，否则连接无法复用。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("探测返回 HTTP %d", resp.StatusCode)
	}
	return nil
}
