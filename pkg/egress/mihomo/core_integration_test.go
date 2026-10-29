package mihomo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegis/pkg/egress"
)

// TestCoreEndToEnd 拉起真实的 mihomo 核心，经网关的 mihomo 端点访问本机 HTTP 服务。
//
//	MIHOMO_BINARY=/path/to/mihomo go test ./pkg/egress/mihomo/ -run TestCoreEndToEnd -v
//
// 没有核心程序时跳过；MIHOMO_INSTALL_DIR 设了则先从发布页自动安装到那里。
func TestCoreEndToEnd(t *testing.T) {
	binary := os.Getenv("MIHOMO_BINARY")
	installDir := os.Getenv("MIHOMO_INSTALL_DIR")
	if binary == "" && installDir == "" {
		t.Skip("未设置 MIHOMO_BINARY / MIHOMO_INSTALL_DIR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	home := t.TempDir()
	opts := Options{Home: home, Binary: binary}
	if binary == "" {
		opts = Options{Home: installDir, AutoInstall: true, Mirror: os.Getenv("MIHOMO_MIRROR")}
	}
	core := NewCore(opts)
	defer core.Close()
	SetDefault(core)
	defer SetDefault(nil)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello via "+r.Host)
	}))
	defer upstream.Close()

	profile := Profile{
		Settings: Settings{Mode: "rule", LogLevel: "debug"},
		Proxies: []map[string]any{
			{"name": "本机直连", "type": "direct"},
		},
		ProxyGroups: []map[string]any{
			{"name": "🚀 节点选择", "type": "select", "proxies": []any{"本机直连", "DIRECT"}},
		},
		Rules: []string{"DOMAIN-SUFFIX,example.com,REJECT", "MATCH,🚀 节点选择"},
	}
	if err := core.Apply(ctx, profile, []string{"🚀 节点选择"}); err != nil {
		for _, line := range core.Logs(0, 50) {
			t.Log(line.Source, line.Level, line.Message)
		}
		t.Fatalf("Apply: %v", err)
	}
	status := core.Status()
	if status.Status != StatusRunning || status.Version == "" {
		t.Fatalf("核心未就绪: %+v", status)
	}
	t.Logf("核心 %s 已在 %s 运行", status.Version, status.ControllerAddr)

	gw, err := egress.New(egress.Config{
		Enabled: true,
		Endpoints: []egress.EndpointConfig{
			{Name: "m-group", Protocol: Protocol, Options: map[string]string{OptionTarget: "🚀 节点选择"}},
			{Name: "m-rules", Protocol: Protocol},
		},
		Rules: []egress.RuleConfig{
			{Name: "group", Endpoints: []string{"m-group"}, Match: egress.MatchConfig{Profiles: []string{"via.group"}}},
			{Name: "rules", Endpoints: []string{"m-rules"}, Match: egress.MatchConfig{Profiles: []string{"via.rules"}}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("egress.New: %v", err)
	}
	defer gw.Close()

	client := gw.Client(egress.Profile{Name: "via.group", Timeout: 10 * time.Second})
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("经策略组访问失败: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(string(body), "hello via") {
		t.Fatalf("响应不对: %q", body)
	}

	api, err := core.Client()
	if err != nil {
		t.Fatal(err)
	}
	conns, err := api.Connections(ctx)
	if err != nil {
		t.Fatalf("Connections: %v", err)
	}
	t.Logf("累计上行 %d 字节、下行 %d 字节", conns.UploadTotal, conns.DownloadTotal)

	if err := api.Select(ctx, "🚀 节点选择", "DIRECT"); err != nil {
		t.Fatalf("Select: %v", err)
	}
	proxies, err := api.Proxies(ctx)
	if err != nil {
		t.Fatalf("Proxies: %v", err)
	}
	if proxies["🚀 节点选择"].Now != "DIRECT" {
		t.Fatalf("切换没有生效: %+v", proxies["🚀 节点选择"])
	}

	// 分流规则：example.com 被 REJECT。
	rejected := gw.Client(egress.Profile{Name: "via.rules", Timeout: 10 * time.Second})
	if _, err := rejected.Get("http://www.example.com/"); err == nil {
		t.Fatalf("example.com 应该被核心的分流规则拒绝")
	}

	// 热重载：新增一个目标后旧入站继续可用。
	profile.ProxyGroups = append(profile.ProxyGroups, map[string]any{"name": "备用", "type": "fallback", "proxies": []any{"本机直连"}, "url": upstream.URL, "interval": 300})
	if err := core.Apply(ctx, profile, []string{"🚀 节点选择", "备用"}); err != nil {
		t.Fatalf("热重载失败: %v", err)
	}
	resp, err = client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("热重载后访问失败: %v", err)
	}
	resp.Body.Close()

	if err := core.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !core.Running() {
		t.Fatalf("重启后未运行")
	}
	if _, err := os.Stat(filepath.Join(core.opts.Home, "config.yaml")); err != nil {
		t.Fatalf("配置没有落盘: %v", err)
	}
	if err := core.Apply(ctx, Profile{}, nil); err != nil {
		t.Fatalf("清空配置: %v", err)
	}
	if core.Status().Status != StatusDisabled {
		t.Fatalf("清空后应停止核心")
	}
}
