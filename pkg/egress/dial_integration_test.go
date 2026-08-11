package egress

import (
	"bufio"
	"context"
	"errors"
	"io"
	stdlog "log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	socks5server "github.com/things-go/go-socks5"
)

// 这些用例把客户端拨号器接到**真实的服务端实现**上跑：
// SOCKS5 用 things-go/go-socks5，Shadowsocks 用官方参考实现的服务端。
// 只有这样才能验证互操作性——自己写一个对端只能验证「我和我自己一致」。

func startOriginServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "origin-ok")
	}))
	t.Cleanup(server.Close)
	return server
}

func startSocks5Server(t *testing.T, user, password string) string {
	t.Helper()
	opts := []socks5server.Option{
		socks5server.WithLogger(socks5server.NewLogger(stdlog.New(io.Discard, "", 0))),
	}
	if user != "" {
		opts = append(opts, socks5server.WithAuthMethods([]socks5server.Authenticator{
			socks5server.UserPassAuthenticator{Credentials: socks5server.StaticCredentials{user: password}},
		}))
	}
	server := socks5server.NewServer(opts...)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听 SOCKS5 端口失败: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String()
}

// startShadowsocksServer 起一个基于官方 core 包的 Shadowsocks 服务端。
func startShadowsocksServer(t *testing.T, method, password string) string {
	t.Helper()
	cipher, err := core.PickCipher(method, nil, password)
	if err != nil {
		t.Fatalf("初始化 shadowsocks 服务端加密失败: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听 shadowsocks 端口失败: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func(raw net.Conn) {
				defer raw.Close()
				conn := cipher.StreamConn(raw)
				target, err := socks.ReadAddr(conn)
				if err != nil {
					return
				}
				upstream, err := net.Dial("tcp", target.String())
				if err != nil {
					return
				}
				defer upstream.Close()
				go func() { _, _ = io.Copy(upstream, conn) }()
				_, _ = io.Copy(conn, upstream)
			}(raw)
		}
	}()
	return listener.Addr().String()
}

// startConnectProxy 起一个只支持 CONNECT 的 HTTP 代理。
// 服务端用 net/http 的 Hijack，隧道之后就是纯字节搬运。
func startConnectProxy(t *testing.T, requireAuth string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听代理端口失败: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireAuth != "" && r.Header.Get("Proxy-Authorization") != requireAuth {
			http.Error(w, "proxy auth required", http.StatusProxyAuthRequired)
			return
		}
		if r.Method != http.MethodConnect {
			// 明文转发模式：代理直接代替客户端发起请求。
			resp, err := http.DefaultTransport.RoundTrip(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}
		upstream, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { defer upstream.Close(); _, _ = io.Copy(upstream, client) }()
		go func() { defer client.Close(); _, _ = io.Copy(client, upstream) }()
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

func fetchThrough(t *testing.T, gw *Gateway, url string) (string, error) {
	t.Helper()
	client := gw.Client(Profile{Name: "test", Timeout: 10 * time.Second, DisableKeepAlives: true})
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func proxyAllConfig(endpoint EndpointConfig) Config {
	return Config{
		Enabled:          true,
		DefaultAction:    ActionProxy,
		DefaultEndpoints: []string{endpoint.Name},
		Endpoints:        []EndpointConfig{endpoint},
		Health:           HealthConfig{Enabled: false},
	}
}

func TestDialThroughSocks5(t *testing.T) {
	origin := startOriginServer(t)
	proxyAddr := startSocks5Server(t, "", "")

	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "socks", Protocol: ProtocolSOCKS5H, Address: proxyAddr,
	}))
	body, err := fetchThrough(t, gw, origin.URL)
	if err != nil {
		t.Fatalf("经 SOCKS5 请求失败: %v", err)
	}
	if body != "origin-ok" {
		t.Fatalf("响应体 = %q", body)
	}
}

func TestDialThroughSocks5WithAuth(t *testing.T) {
	origin := startOriginServer(t)
	proxyAddr := startSocks5Server(t, "alice", "s3cret")

	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "socks", Protocol: ProtocolSOCKS5H, Address: proxyAddr,
		Username: "alice", Password: "s3cret",
	}))
	if body, err := fetchThrough(t, gw, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("带认证的 SOCKS5 请求失败: body=%q err=%v", body, err)
	}

	// 口令错误必须失败，否则「配错密码但一切正常」会掩盖真实问题。
	bad := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "socks", Protocol: ProtocolSOCKS5H, Address: proxyAddr,
		Username: "alice", Password: "wrong",
	}))
	if _, err := fetchThrough(t, bad, origin.URL); err == nil {
		t.Fatal("口令错误时不应连接成功")
	}
}

func TestDialThroughHTTPConnect(t *testing.T) {
	origin := startOriginServer(t)
	proxyAddr := startConnectProxy(t, "")

	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "http", Protocol: ProtocolHTTP, Address: proxyAddr,
	}))
	if body, err := fetchThrough(t, gw, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("经 HTTP CONNECT 请求失败: body=%q err=%v", body, err)
	}
}

func TestDialThroughHTTPForwardMode(t *testing.T) {
	origin := startOriginServer(t)
	proxyAddr := startConnectProxy(t, "")

	// forward 模式下明文请求交给 net/http 用 absolute-URI 发给代理，
	// 不再打 CONNECT 隧道。
	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "http", Protocol: ProtocolHTTP, Address: proxyAddr, HTTPForwardMode: true,
	}))
	if body, err := fetchThrough(t, gw, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("转发模式请求失败: body=%q err=%v", body, err)
	}
}

func TestDialThroughShadowsocks(t *testing.T) {
	origin := startOriginServer(t)
	serverAddr := startShadowsocksServer(t, "aes-256-gcm", "pa55word")

	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "ss", Protocol: ProtocolShadowsocks, Address: serverAddr,
		Shadowsocks: ShadowsocksConfig{Method: "aes-256-gcm", Password: "pa55word"},
	}))
	if body, err := fetchThrough(t, gw, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("经 Shadowsocks 请求失败: body=%q err=%v", body, err)
	}
}

func TestShadowsocksWrongPasswordFails(t *testing.T) {
	origin := startOriginServer(t)
	serverAddr := startShadowsocksServer(t, "aes-256-gcm", "pa55word")

	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "ss", Protocol: ProtocolShadowsocks, Address: serverAddr,
		Shadowsocks: ShadowsocksConfig{Method: "aes-256-gcm", Password: "wrong"},
	}))
	if _, err := fetchThrough(t, gw, origin.URL); err == nil {
		t.Fatal("口令错误时不应连接成功")
	}
}

// TestChainedEndpoints 验证代理链：SS 落地端点先经由 SOCKS5 跳板连出去。
func TestChainedEndpoints(t *testing.T) {
	origin := startOriginServer(t)
	jumpAddr := startSocks5Server(t, "", "")
	exitAddr := startShadowsocksServer(t, "chacha20-ietf-poly1305", "chain-pass")

	gw := testGateway(t, Config{
		Enabled:          true,
		DefaultAction:    ActionProxy,
		DefaultEndpoints: []string{"exit"},
		Health:           HealthConfig{Enabled: false},
		Endpoints: []EndpointConfig{
			{Name: "jump", Protocol: ProtocolSOCKS5H, Address: jumpAddr},
			{Name: "exit", Protocol: ProtocolShadowsocks, Address: exitAddr, Via: "jump",
				Shadowsocks: ShadowsocksConfig{Method: "chacha20-ietf-poly1305", Password: "chain-pass"}},
		},
	})
	if body, err := fetchThrough(t, gw, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("链式端点请求失败: body=%q err=%v", body, err)
	}

	exit, _ := gw.Endpoint("exit")
	if chain := strings.Join(exit.chain(), "→"); chain != "exit→jump" {
		t.Fatalf("端点链 = %q", chain)
	}
	jump, _ := gw.Endpoint("jump")
	if jump.stats.dials.Load() != 0 && exit.stats.dials.Load() == 0 {
		t.Fatal("统计应记在落地端点上")
	}
}

func TestRejectActionReturnsTypedError(t *testing.T) {
	gw := testGateway(t, Config{
		Enabled:       true,
		DefaultAction: ActionDirect,
		Rules: []RuleConfig{{
			Name: "封禁", Action: ActionReject,
			Match: MatchConfig{DomainSuffixes: []string{"blocked.example"}},
		}},
	})
	_, err := gw.DialContext(context.Background(), "tcp", "api.blocked.example:443")
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("应返回 ErrRejected，实际 %v", err)
	}
}

func TestRawDialContextRoutesThroughProxy(t *testing.T) {
	// 裸 TCP 出站（SMTP / LDAP 场景）同样按规则走代理。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听回显端口失败: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "hello-tcp\n")
	}()

	proxyAddr := startSocks5Server(t, "", "")
	gw := testGateway(t, proxyAllConfig(EndpointConfig{
		Name: "socks", Protocol: ProtocolSOCKS5H, Address: proxyAddr,
	}))

	conn, err := gw.DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("裸 TCP 拨号失败: %v", err)
	}
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "hello-tcp" {
		t.Fatalf("读取回显失败: %q %v", line, err)
	}

	endpoint, _ := gw.Endpoint("socks")
	if endpoint.stats.bytesIn.Load() == 0 {
		t.Fatal("字节统计未生效")
	}
}

func TestProbeMarksEndpointUnhealthy(t *testing.T) {
	origin := startOriginServer(t)
	gw := testGateway(t, Config{
		Enabled:       true,
		DefaultAction: ActionDirect,
		Health: HealthConfig{
			Enabled: true, ProbeURL: origin.URL, TimeoutSeconds: 3,
			FailureThreshold: 1, SuccessThreshold: 1,
		},
		Endpoints: []EndpointConfig{
			{Name: "good", Protocol: ProtocolDirect},
			// 指向一个必然连不通的地址。
			{Name: "bad", Protocol: ProtocolSOCKS5H, Address: "127.0.0.1:1"},
		},
	})

	results := gw.ProbeAll(context.Background())
	byName := map[string]ProbeResult{}
	for _, item := range results {
		byName[item.Endpoint] = item
	}
	if !byName["good"].OK {
		t.Fatalf("direct 端点探测应通过: %+v", byName["good"])
	}
	if byName["bad"].OK {
		t.Fatal("不可达端点探测应失败")
	}
	bad, _ := gw.Endpoint("bad")
	if healthy, _, _, _, _, _ := bad.snapshotHealth(); healthy {
		t.Fatal("探测失败后端点应被标记为不健康")
	}
}

func TestDisabledGatewayHonorsEnvironmentProxy(t *testing.T) {
	origin := startOriginServer(t)
	proxyAddr := startConnectProxy(t, "")

	// 未启用网关时应保持标准库行为：继续走 HTTP_PROXY。
	t.Setenv("HTTP_PROXY", "http://"+proxyAddr)
	t.Setenv("NO_PROXY", "")

	gw := testGateway(t, Config{Enabled: false})
	if body, err := fetchThrough(t, gw, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("环境变量代理未生效: body=%q err=%v", body, err)
	}

	// 一旦启用，路由表就是唯一权威，环境变量不再参与决策。
	enabled := testGateway(t, Config{Enabled: true, DefaultAction: ActionDirect})
	if body, err := fetchThrough(t, enabled, origin.URL); err != nil || body != "origin-ok" {
		t.Fatalf("启用网关后直连失败: body=%q err=%v", body, err)
	}
}
