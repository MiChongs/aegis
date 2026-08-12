package clientip

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// noEnv 让平台探测在测试里恒为「没探测到」。
// 不这么做的话，测试结果会随「跑测试的机器上恰好有哪些环境变量」变化 ——
// CI 跑在 Kubernetes 里就会凭空多出一份平台档案。
func noEnv(string) string { return "" }

func newRequest(t *testing.T, remoteAddr string, headers map[string]string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/user/my", nil)
	req.RemoteAddr = remoteAddr
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req
}

func mustResolver(t *testing.T, cfg Config, env func(string) string) *Resolver {
	t.Helper()
	if env == nil {
		env = noEnv
	}
	resolver, err := NewWithEnv(cfg, env)
	if err != nil {
		t.Fatalf("NewWithEnv(%+v) error = %v", cfg, err)
	}
	return resolver
}

func TestResolveScenarios(t *testing.T) {
	cases := []struct {
		name       string
		cfg        Config
		env        func(string) string
		remoteAddr string
		headers    map[string]string
		wantIP     string
		wantSource Source
	}{
		{
			// 这一条是整个包的安全底座：直连公网的客户端自己写一个 XFF，
			// 判定结果必须仍然是它自己的地址。破了这一条，限流与封禁就形同虚设。
			name:       "直连公网时伪造的转发头不作数",
			remoteAddr: "203.0.113.9:44321",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4", "X-Real-IP": "5.6.7.8"},
			wantIP:     "203.0.113.9",
			wantSource: SourcePeer,
		},
		{
			// 容器平台的常态：入口网关在集群内网，真实客户端在转发链最右端。
			// 修复之前这里拿到的是 10.42.0.7，全站用户共用一个 IP。
			name:       "容器平台内网网关转发",
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9"},
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			name:       "多层内网代理只跳过内网那几跳",
			remoteAddr: "127.0.0.1:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.4, 172.17.0.3"},
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			// 客户端塞在最左边的伪造条目会被真实那一跳挤到左侧，从右往左走时够不着。
			name:       "客户端塞在最左边的伪造条目取不到",
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.9"},
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			name:       "CDN 边缘在受信集合里时跳过它",
			cfg:        Config{TrustedProxies: []string{PresetInfra, PresetCloudflare}},
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.9, 172.68.1.1"},
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			// 同一份请求、不加 CDN 预设时的对照：判定停在 CDN 边缘上。
			// 「线上所有人的 IP 都变成同一小撮机房地址」就是这么来的。
			name:       "不加 CDN 预设时判定停在边缘地址上",
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.9, 172.68.1.1"},
			wantIP:     "172.68.1.1",
			wantSource: SourceChain,
		},
		{
			name:       "转发链整条都在受信集合内时退回直连对端",
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.4, 192.168.1.1"},
			wantIP:     "10.42.0.7",
			wantSource: SourcePeer,
		},
		{
			name:       "没有转发头时退回直连对端",
			remoteAddr: "10.42.0.7:38210",
			wantIP:     "10.42.0.7",
			wantSource: SourcePeer,
		},
		{
			// Azure App Service 这类平台的转发链条目带端口。
			name:       "转发链条目带端口",
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9:52001"},
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			name:       "RFC 7239 Forwarded 头",
			cfg:        Config{ListHeader: HeaderForwarded},
			remoteAddr: "10.42.0.7:38210",
			headers: map[string]string{
				"Forwarded": `for=198.51.100.9;proto=https;by=203.0.113.43, for="[2001:db8::1]:4711"`,
			},
			// 2001:db8::1 是文档保留段，不在受信集合内，所以最右一条就是结论
			wantIP:     "2001:db8::1",
			wantSource: SourceChain,
		},
		{
			name:       "IPv6 直连对端与 IPv6 客户端",
			remoteAddr: "[fd00::1]:38210",
			headers:    map[string]string{"X-Forwarded-For": "2001:db8::99"},
			wantIP:     "2001:db8::99",
			wantSource: SourceChain,
		},
		{
			// 双栈监听下内网对端会以 4-in-6 形式出现。不做 Unmap 的话它会被判成
			// 「不受信」，转发头整个被丢掉 —— 表现和完全没做这次修复一模一样。
			name:       "IPv4-mapped IPv6 的内网对端仍然受信",
			remoteAddr: "[::ffff:10.42.0.7]:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9"},
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			name:       "多个同名转发头按顺序拼成一条链",
			remoteAddr: "10.42.0.7:38210",
			headers:    nil,
			wantIP:     "203.0.113.9",
			wantSource: SourceChain,
		},
		{
			name:       "peer 档完全不看转发头",
			cfg:        Config{Strategy: string(StrategyPeer)},
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9"},
			wantIP:     "10.42.0.7",
			wantSource: SourcePeer,
		},
		{
			name:       "header 档只认配置的那个头",
			cfg:        Config{Strategy: string(StrategyHeader), Header: "CF-Connecting-IP"},
			remoteAddr: "10.42.0.7:38210",
			headers: map[string]string{
				"CF-Connecting-IP": "203.0.113.9",
				"X-Forwarded-For":  "198.51.100.9",
			},
			wantIP:     "203.0.113.9",
			wantSource: SourceHeader,
		},
		{
			name:       "header 档取不到时退回直连对端而不是转发链",
			cfg:        Config{Strategy: string(StrategyHeader), Header: "CF-Connecting-IP"},
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.9"},
			wantIP:     "10.42.0.7",
			wantSource: SourcePeer,
		},
		{
			// Hops=2 表示前面有两层受信代理，取右起第二条。
			name:       "trusted-hops 按代理层数定位",
			cfg:        Config{Strategy: string(StrategyTrustedHops), Hops: 2},
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9, 198.51.100.9, 172.68.1.1"},
			wantIP:     "198.51.100.9",
			wantSource: SourceHops,
		},
		{
			// 这一档判「是不是公网」用的是解析库自带的保留段清单，与本包的受信集合
			// 无关，因此这里必须用真正的公网地址：203.0.113.0/24 这类文档保留段
			// 会被它当成内网跳过。
			name:       "leftmost 取最左公网地址",
			cfg:        Config{Strategy: string(StrategyLeftmost)},
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.9, 9.9.9.9, 8.8.4.4"},
			wantIP:     "9.9.9.9",
			wantSource: SourceLeftmost,
		},
		{
			name:       "显式受信网段：不在其中的对端一律按客户端处理",
			cfg:        Config{Strategy: string(StrategyTrustedRanges), TrustedProxies: []string{"192.0.2.1"}},
			remoteAddr: "10.42.0.7:38210",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.9"},
			wantIP:     "10.42.0.7",
			wantSource: SourcePeer,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := mustResolver(t, tc.cfg, tc.env)
			req := newRequest(t, tc.remoteAddr, tc.headers)
			if tc.name == "多个同名转发头按顺序拼成一条链" {
				req.Header.Add("X-Forwarded-For", "1.2.3.4")
				req.Header.Add("X-Forwarded-For", "203.0.113.9, 10.0.0.4")
			}

			result := resolver.Resolve(req)
			if got := result.IP.String(); got != tc.wantIP {
				t.Fatalf("IP = %s, want %s（判定过程：%s）", got, tc.wantIP, result)
			}
			if result.Source != tc.wantSource {
				t.Fatalf("Source = %s, want %s（判定过程：%s）", result.Source, tc.wantSource, result)
			}
		})
	}
}

// TestResolvePreservesPeer 判定错时排障要问的第一个问题是「这个请求到底从哪台机器连过来」，
// 所以直连对端与端口必须原样留在结果里。
func TestResolvePreservesPeer(t *testing.T) {
	resolver := mustResolver(t, Config{}, nil)
	result := resolver.Resolve(newRequest(t, "10.42.0.7:38210", map[string]string{
		"X-Forwarded-For": "203.0.113.9",
	}))

	if got, want := result.Peer.String(), "10.42.0.7"; got != want {
		t.Fatalf("Peer = %s, want %s", got, want)
	}
	if got, want := result.PeerPort, "38210"; got != want {
		t.Fatalf("PeerPort = %s, want %s", got, want)
	}
	if !result.PeerTrusted {
		t.Fatal("内网对端应当被判定为受信")
	}
	if got, want := strings.Join(result.Chain, "|"), "203.0.113.9"; got != want {
		t.Fatalf("Chain = %s, want %s", got, want)
	}
}

// TestPublicIngressTopology 一套真实拓扑：客户端 → Cloudflare → 自己的入口反代 → 容器，
// 而那个入口反代持有**公网**地址。
//
// 默认受信集合只覆盖内网网段，于是对端不受信、整条转发头被丢掉，全站客户端 IP
// 收敛成入口反代那一个地址 —— 而且判定链路上没有任何一处会报错。
// 这个形状在云上与 PaaS 上很常见，两条改法各自钉一次。
func TestPublicIngressTopology(t *testing.T) {
	const (
		ingress = "8.221.123.21:52104"       // 入口反代，公网地址
		client  = "2409:8a4c:7810:eb7c::ed6" // 真实客户端
		edge    = "104.22.72.17"             // Cloudflare 边缘（104.16.0.0/13）
		chain   = client + ", " + edge       //
	)

	t.Run("默认档取到的是入口反代自己", func(t *testing.T) {
		resolver := mustResolver(t, Config{}, nil)
		result := resolver.Resolve(newRequest(t, ingress, map[string]string{"X-Forwarded-For": chain}))
		if got, want := result.IP.String(), "8.221.123.21"; got != want {
			t.Fatalf("IP = %s, want %s（判定过程：%s）", got, want, result)
		}
		if result.PeerTrusted {
			t.Fatal("公网对端不该被默认受信集合接纳")
		}
		// 被忽略的那条链必须留在结果里，否则中间件没东西可喊
		if len(result.Chain) != 2 {
			t.Fatalf("Chain = %v，应当记下被忽略的两跳", result.Chain)
		}
	})

	t.Run("direct-peer 让对端受信后取到真实客户端", func(t *testing.T) {
		resolver := mustResolver(t, Config{
			TrustedProxies: []string{PresetInfra, PresetCloudflare, PresetDirectPeer},
		}, nil)
		result := resolver.Resolve(newRequest(t, ingress, map[string]string{"X-Forwarded-For": chain}))
		if got := result.IP.String(); got != client {
			t.Fatalf("IP = %s, want %s（判定过程：%s）", got, client, result)
		}
		if result.Source != SourceChain {
			t.Fatalf("Source = %s, want %s", result.Source, SourceChain)
		}
	})

	t.Run("direct-peer 仍然逐跳判定，不等于信任一切", func(t *testing.T) {
		// 不带 cloudflare 预设时，边缘地址不受信，判定就停在它上面 ——
		// 说明 direct-peer 只多信任了紧邻的一跳，转发链本身照旧按受信集合走。
		resolver := mustResolver(t, Config{
			TrustedProxies: []string{PresetInfra, PresetDirectPeer},
		}, nil)
		result := resolver.Resolve(newRequest(t, ingress, map[string]string{"X-Forwarded-For": chain}))
		if got := result.IP.String(); got != edge {
			t.Fatalf("IP = %s, want %s（判定过程：%s）", got, edge, result)
		}
	})

	t.Run("header 档不受直连对端受信与否的影响", func(t *testing.T) {
		// 站在 CDN 后面时最直接的改法：CDN 会覆写这个头，客户端写不进来。
		// 它必须能在对端不受信时照常生效 —— 否则这条显式配置恰好在最需要它的
		// 拓扑（入口反代持公网地址）上静默失效。
		resolver := mustResolver(t, Config{
			Strategy: string(StrategyHeader),
			Header:   "CF-Connecting-IP",
		}, nil)
		result := resolver.Resolve(newRequest(t, ingress, map[string]string{
			"CF-Connecting-IP": client,
			"X-Forwarded-For":  chain,
		}))
		if got := result.IP.String(); got != client {
			t.Fatalf("IP = %s, want %s（判定过程：%s）", got, client, result)
		}
		if result.Source != SourceHeader {
			t.Fatalf("Source = %s, want %s", result.Source, SourceHeader)
		}
		if result.PeerTrusted {
			t.Fatal("header 档不该顺带把对端标成受信")
		}
	})
}

// TestDirectPeerIsNotAliasOfStrategyPeer 「peer」在两个配置项上意思相反：
// CLIENT_IP_STRATEGY=peer 是「转发头一概不看」，TRUSTED_PROXIES=direct-peer 是
// 「信任对端，好去读转发头」。把 peer 收成 direct-peer 的别名，就等于让一个
// 写错位置的配置静默生效成相反的行为，因此它必须是一个启动错误。
func TestDirectPeerIsNotAliasOfStrategyPeer(t *testing.T) {
	if _, err := NewWithEnv(Config{TrustedProxies: []string{"peer"}}, noEnv); err == nil {
		t.Fatal("TRUSTED_PROXIES=peer 应当启动失败，而不是被当成 direct-peer")
	}
}

// TestDirectPeerWarns 信任直连对端的前提是「本服务只能经由自己的入口访问」，
// 这个前提部署方必须自己确认，因此它要在启动时被说出来。
func TestDirectPeerWarns(t *testing.T) {
	desc := mustResolver(t, Config{TrustedProxies: []string{PresetInfra, PresetDirectPeer}}, nil).Describe()
	if !desc.TrustsPeer {
		t.Fatal("Describe().TrustsPeer 应当为真")
	}
	if len(desc.Warnings) == 0 {
		t.Fatal("direct-peer 应当产生告警")
	}
	if desc.TrustsAll {
		t.Fatal("direct-peer 不是「信任一切」，不该被这么报")
	}
}

// TestResolveRecordsIgnoredChain 对端不受信时转发头被整个忽略，但**忽略了什么**要留下来：
// 只在采信时才记录的话，`source=peer` 这个结论在排障时看不出任何缘由。
func TestResolveRecordsIgnoredChain(t *testing.T) {
	resolver := mustResolver(t, Config{}, nil)
	result := resolver.Resolve(newRequest(t, "203.0.113.9:44321", map[string]string{
		"X-Forwarded-For": "1.2.3.4, 5.6.7.8",
	}))

	if result.PeerTrusted {
		t.Fatal("公网对端不该被判定为受信")
	}
	if got, want := strings.Join(result.Chain, "|"), "1.2.3.4|5.6.7.8"; got != want {
		t.Fatalf("Chain = %s, want %s", got, want)
	}
}

// TestResolveWithoutRemoteAddr 没有 RemoteAddr 的裸请求（测试构造出来的那种）
// 不该凭空造出一个客户端地址，中间件据此保持 RemoteAddr 原样。
func TestResolveWithoutRemoteAddr(t *testing.T) {
	resolver := mustResolver(t, Config{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ""

	result := resolver.Resolve(req)
	if result.Valid() {
		t.Fatalf("没有 RemoteAddr 时不应当给出判定结果，得到 %s", result.IP)
	}
	if result.Source != SourceUnresolved {
		t.Fatalf("Source = %s, want %s", result.Source, SourceUnresolved)
	}
}

// TestNilResolverFallsBackToPeer 零依赖装配（openapi / routes 子命令）走这一支。
func TestNilResolverFallsBackToPeer(t *testing.T) {
	var resolver *Resolver
	result := resolver.Resolve(newRequest(t, "10.42.0.7:38210", map[string]string{
		"X-Forwarded-For": "203.0.113.9",
	}))
	if got, want := result.IP.String(), "10.42.0.7"; got != want {
		t.Fatalf("IP = %s, want %s", got, want)
	}
}

func TestPlatformProfilesApplyInAutoStrategy(t *testing.T) {
	t.Run("Fly.io 的单值头优先于转发链", func(t *testing.T) {
		env := func(key string) string {
			if key == "FLY_APP_NAME" {
				return "aegis"
			}
			return ""
		}
		resolver := mustResolver(t, Config{}, env)
		result := resolver.Resolve(newRequest(t, "172.19.0.3:38210", map[string]string{
			"Fly-Client-IP":   "203.0.113.9",
			"X-Forwarded-For": "198.51.100.9",
		}))
		if got, want := result.IP.String(), "203.0.113.9"; got != want {
			t.Fatalf("IP = %s, want %s（判定过程：%s）", got, want, result)
		}
		if result.Platform != "fly" {
			t.Fatalf("Platform = %q, want fly", result.Platform)
		}
	})

	t.Run("Zeabur 自动补上 CDN 边缘网段", func(t *testing.T) {
		env := func(key string) string {
			if key == "ZEABUR_SERVICE_ID" {
				return "svc-123"
			}
			return ""
		}
		resolver := mustResolver(t, Config{}, env)
		result := resolver.Resolve(newRequest(t, "10.42.0.7:38210", map[string]string{
			"X-Forwarded-For": "1.2.3.4, 203.0.113.9, 172.68.1.1",
		}))
		if got, want := result.IP.String(), "203.0.113.9"; got != want {
			t.Fatalf("IP = %s, want %s（判定过程：%s）", got, want, result)
		}
	})

	t.Run("Kubernetes 排在最后，不遮住具体平台", func(t *testing.T) {
		env := func(key string) string {
			switch key {
			case "KUBERNETES_SERVICE_HOST":
				return "10.96.0.1"
			case "ZEABUR_PROJECT_ID":
				return "prj-1"
			}
			return ""
		}
		if got := DetectPlatform(env).Key; got != "zeabur" {
			t.Fatalf("DetectPlatform = %q, want zeabur", got)
		}
	})

	t.Run("显式选 trusted-ranges 时不做平台补充", func(t *testing.T) {
		env := func(key string) string {
			if key == "ZEABUR_SERVICE_ID" {
				return "svc-123"
			}
			return ""
		}
		resolver := mustResolver(t, Config{Strategy: string(StrategyTrustedRanges)}, env)
		result := resolver.Resolve(newRequest(t, "10.42.0.7:38210", map[string]string{
			"X-Forwarded-For": "1.2.3.4, 203.0.113.9, 172.68.1.1",
		}))
		if got, want := result.IP.String(), "172.68.1.1"; got != want {
			t.Fatalf("IP = %s, want %s；trusted-ranges 档应当严格只用配置里的网段", got, want)
		}
	})
}

// TestConfigErrors 配置写错必须启动即失败。
// 静默跳过一条写错的网段，表现是「线上大部分请求的 IP 突然变成网关地址」，
// 从这个现象倒查回配置文件要很久。
func TestConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"未知判定方式", Config{Strategy: "rightmost"}},
		{"未知转发链头", Config{ListHeader: "X-Real-IP"}},
		{"网段拼写错误", Config{TrustedProxies: []string{"10.0.0.0/64"}}},
		{"预设名拼写错误", Config{TrustedProxies: []string{"clouflare"}}},
		{"header 档缺少头名", Config{Strategy: string(StrategyHeader)}},
		{"trusted-hops 缺少层数", Config{Strategy: string(StrategyTrustedHops)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewWithEnv(tc.cfg, noEnv); err == nil {
				t.Fatalf("NewWithEnv(%+v) 应当返回错误", tc.cfg)
			}
		})
	}
}

// TestTrustAllWarns 「谁都信」等于把转发头交给客户端随便写，必须在启动时说出来。
func TestTrustAllWarns(t *testing.T) {
	for _, entries := range [][]string{
		{PresetAll},
		{"0.0.0.0/0", "::/0"},
	} {
		resolver := mustResolver(t, Config{TrustedProxies: entries}, nil)
		desc := resolver.Describe()
		if !desc.TrustsAll {
			t.Fatalf("TRUSTED_PROXIES=%v 应当被判定为信任一切", entries)
		}
		if len(desc.Warnings) == 0 {
			t.Fatalf("TRUSTED_PROXIES=%v 应当产生告警", entries)
		}
	}

	// 对照：默认配置不该产生任何告警，否则告警会被当成噪音而被忽略。
	if warnings := mustResolver(t, Config{}, nil).Describe().Warnings; len(warnings) != 0 {
		t.Fatalf("默认配置不应当产生告警，得到 %v", warnings)
	}
}

// TestDefaultTrustedProxiesCoverContainerNetworks 默认值必须覆盖容器平台上
// 「入口网关 → 业务容器」那一跳可能落在的每一段，漏一段就有一类部署取不到真实 IP。
func TestDefaultTrustedProxiesCoverContainerNetworks(t *testing.T) {
	resolver := mustResolver(t, Config{}, nil)
	for _, peer := range []string{
		"10.42.0.7:1",    // Kubernetes Pod 网段
		"172.17.0.5:1",   // Docker 默认网桥
		"192.168.1.10:1", // 家用/局域网反代
		"127.0.0.1:1",    // 同机反代与 Cloudflare Tunnel sidecar
		"100.64.3.9:1",   // CGNAT，多数 PaaS 的容器网段
		"169.254.10.1:1", // 链路本地，部分 sidecar
		"[fd00::1]:1",    // IPv6 ULA
	} {
		result := resolver.Resolve(newRequest(t, peer, map[string]string{
			"X-Forwarded-For": "203.0.113.9",
		}))
		if got, want := result.IP.String(), "203.0.113.9"; got != want {
			t.Errorf("对端 %s：IP = %s, want %s（判定过程：%s）", peer, got, want, result)
		}
	}
}

func TestDescribeReportsEffectiveSettings(t *testing.T) {
	resolver := mustResolver(t, Config{TrustedProxies: []string{PresetLoopback, "10.0.0.0/8"}}, nil)
	desc := resolver.Describe()

	if desc.Strategy != StrategyAuto {
		t.Fatalf("Strategy = %s, want %s", desc.Strategy, StrategyAuto)
	}
	if desc.ListHeader != HeaderXForwardedFor {
		t.Fatalf("ListHeader = %s, want %s", desc.ListHeader, HeaderXForwardedFor)
	}
	got := strings.Join(desc.PrefixStrings(), ",")
	if want := "10.0.0.0/8,127.0.0.0/8,::1/128"; got != want {
		t.Fatalf("PrefixStrings = %s, want %s", got, want)
	}
}
