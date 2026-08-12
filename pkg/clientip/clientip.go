// Package clientip 回答一个问题：这个请求真正来自哪个 IP。
//
// 直接读 `http.Request.RemoteAddr` 在任何反向代理后面都是错的 —— 拿到的是代理的
// 地址。而直接读 `X-Forwarded-For` 又是**可伪造**的：这个头由客户端就能写。
// 两者都错，只是错法不同：前者让全站用户共用一个 IP（限流、封禁、地理风控全部失效），
// 后者让攻击者随便换 IP（限流、封禁、地理风控同样全部失效）。
//
// 正确做法只有一条：**先判直连对端可不可信，可信才读转发头**。
//
//	RemoteAddr 不在受信网段  →  它就是客户端，转发头一律不看
//	RemoteAddr 在受信网段    →  从转发链最右端往左走，跳过受信条目，
//	                            第一个不受信的条目就是客户端
//
// 「从右往左」是关键：转发头是**追加**的 —— 每一跳把自己看到的对端追加到末尾。
// 客户端能控制的只有它自己写进去的那些（最左边），而最右边那一条一定是紧邻的
// 那一跳亲眼看到的。因此从右往左遇到的第一个不受信条目，是整条链上最后一个
// 由可信方写下的事实。
//
// 转发头的解析（XFF 与 RFC 7239 Forwarded、端口、IPv6 方括号、zone、多个同名头）
// 交给 github.com/realclientip/realclientip-go —— 这些形状每一种都有人踩过，
// 自己写一遍只是把别人修过的 bug 重新引进来一次。本包负责的是它刻意不管的那件事：
// 受信集合怎么来、直连对端该不该信、以及判定结果怎么解释给排障的人看。
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"

	realclientip "github.com/realclientip/realclientip-go"
	"go4.org/netipx"
)

// Strategy 判定方式。
type Strategy string

const (
	// StrategyAuto 默认档：受信网段取「基础设施网段 + 探测到的平台档案 + 显式配置」，
	// 转发链从右往左找第一个不受信条目。绝大多数部署（Zeabur / K8s / Docker /
	// Nginx / Cloudflare）都该用这一档。
	StrategyAuto Strategy = "auto"
	// StrategyPeer 完全不看转发头，直连对端就是客户端。
	// 服务直接暴露在公网、前面没有任何代理时用它。
	StrategyPeer Strategy = "peer"
	// StrategyTrustedRanges 与 auto 同样是「从右往左找第一个不受信条目」，
	// 区别是**不做平台探测、不合并平台档案**：受信集合严格等于 TrustedProxies。
	// 需要判定结果完全可预期时用它。
	StrategyTrustedRanges Strategy = "trusted-ranges"
	// StrategyTrustedHops 按「本服务前面有几层受信代理」定位：N 层代理就取转发链
	// 右起第 N 条。代理的出口地址不固定（弹性伸缩的 LB、无法枚举网段的 CDN）、
	// 但层数固定时用它。层数写错会稳定地取到相邻那一跳，因此只在确知层数时才用。
	StrategyTrustedHops Strategy = "trusted-hops"
	// StrategyHeader 只认某一个单值头（CF-Connecting-IP / Fly-Client-IP / True-Client-IP …）。
	// 取不到就退回直连对端，**不会**再去看转发链 —— 「只认这个头」就该是字面意思。
	StrategyHeader Strategy = "header"
	// StrategyLeftmost 取转发链最左边的公网地址。
	//
	// **这一档可被伪造**，客户端自己写一个 XFF 就能决定判定结果。它只为一种真实存在
	// 的拓扑保留：链路上有一跳既不追加也不覆盖 XFF（一些老设备与部分 CDN 的默认档）。
	// 除此之外不要用。
	StrategyLeftmost Strategy = "leftmost"
)

// Source 说明这一次判定的结论是从哪里得来的，排障时回答「为什么是这个 IP」。
type Source string

const (
	SourcePeer       Source = "peer"               // 直连对端（不受信，或没有可用的转发信息）
	SourceHeader     Source = "header"             // 平台单值头
	SourceChain      Source = "forwarded-chain"    // 转发链上第一个不受信条目
	SourceHops       Source = "forwarded-hops"     // 转发链固定跳数
	SourceLeftmost   Source = "forwarded-leftmost" // 转发链最左公网地址
	SourceUnresolved Source = "unresolved"         // RemoteAddr 都解析不出来（测试构造的裸请求）
)

// 转发链头。只有这两个是「列表头」，其余（X-Real-IP / CF-Connecting-IP …）都是单值头。
const (
	HeaderXForwardedFor = "X-Forwarded-For"
	HeaderForwarded     = "Forwarded"
)

// Gin 上下文键。判定结果要跨包读（中间件写、诊断页与业务侧读），
// 键名各处各写一份迟早会对不上，因此定在判定器这一侧。
const (
	ContextKeyClientIP = "request.client_ip"
	ContextKeyPeerIP   = "request.peer_ip"
	ContextKeyDetail   = "request.client_ip_detail"
)

// Config 客户端 IP 判定配置。
//
// 刻意只有一处受信集合入口（TrustedProxies）：同一件事有两个开关时，
// 接入方无从判断哪个生效，而这里判错的代价是限流与封禁静默失效。
type Config struct {
	// Strategy 见 Strategy 常量，留空等价于 auto。
	Strategy string
	// TrustedProxies 受信网段：CIDR、单个 IP，或预设名（见 presets.go）。
	// 留空时取 DefaultTrustedProxies（基础设施网段）。
	TrustedProxies []string
	// Header 单值头名。strategy=header 时必填；auto 档下填了就覆盖平台档案的选择。
	Header string
	// Hops 本服务前面的受信代理层数，必须 ≥ 1：1 表示取转发链最右一条，
	// 2 表示取右起第二条，以此类推。strategy=trusted-hops 时必填。
	Hops int
	// ListHeader 转发链头，X-Forwarded-For（默认）或 Forwarded。
	ListHeader string
	// DebugHeader 是否在响应里回显判定过程（X-Aegis-Client-IP*）。
	// 排「线上拿到的 IP 不对」时打开，它把结论和依据一次说清。
	DebugHeader bool
}

// Result 一次判定的完整结果。
type Result struct {
	IP          netip.Addr // 判定出的客户端地址
	Peer        netip.Addr // 直连对端
	PeerPort    string     // 直连对端端口（回写 RemoteAddr 时保留）
	PeerTrusted bool       // 直连对端是否在受信集合内
	Source      Source
	Header      string   // Source 为 header/chain 系时命中的头名
	Chain       []string // 转发链原样条目，仅用于排障
	Strategy    Strategy
	Platform    string // auto 档下探测到的平台（未探测到为空）
}

// Valid 判定是否得到了一个可用地址。
func (r Result) Valid() bool { return r.IP.IsValid() }

// String 一行式说明，直接作为调试响应头的值。
func (r Result) String() string {
	parts := []string{
		"source=" + string(r.Source),
		"strategy=" + string(r.Strategy),
	}
	if r.Header != "" {
		parts = append(parts, "header="+r.Header)
	}
	if r.Peer.IsValid() {
		parts = append(parts, "peer="+r.Peer.String())
	}
	parts = append(parts, fmt.Sprintf("peer_trusted=%t", r.PeerTrusted))
	if r.Platform != "" {
		parts = append(parts, "platform="+r.Platform)
	}
	if len(r.Chain) > 0 {
		parts = append(parts, "chain="+strings.Join(r.Chain, "|"))
	}
	return strings.Join(parts, "; ")
}

// Resolver 判定器。构造一次、并发只读，Resolve 可安全并发调用。
type Resolver struct {
	strategy    Strategy
	listHeader  string
	singleHdrs  []string
	hops        int
	trusted     *netipx.IPSet
	prefixes    []netip.Prefix
	chain       realclientip.Strategy
	chainSource Source
	platform    Platform
	debugHeader bool
	warnings    []string
}

// New 按配置构造判定器，平台探测读进程环境变量。
func New(cfg Config) (*Resolver, error) {
	return NewWithEnv(cfg, nil)
}

// NewWithEnv 与 New 相同，但把「平台探测读哪里」暴露出来，供测试注入。
// getenv 为 nil 时用 os.Getenv。
func NewWithEnv(cfg Config, getenv func(string) string) (*Resolver, error) {
	strategy, err := parseStrategy(cfg.Strategy)
	if err != nil {
		return nil, err
	}
	listHeader, err := parseListHeader(cfg.ListHeader)
	if err != nil {
		return nil, err
	}

	r := &Resolver{
		strategy:    strategy,
		listHeader:  listHeader,
		debugHeader: cfg.DebugHeader,
	}

	entries := trimmedNonEmpty(cfg.TrustedProxies)
	explicit := len(entries) > 0
	if !explicit {
		entries = DefaultTrustedProxies()
	}

	// auto 档才做平台探测：平台自己注入的环境变量（FLY_APP_NAME / ZEABUR_SERVICE_ID …）
	// 是攻击者写不进来的事实，据此补上该平台的受信网段与单值头是安全的。
	// 显式选了 trusted-ranges 的人要的是「受信集合严格等于我写的那几行」，
	// 那一档就不做任何补充 —— 否则这个配置项说了不算。
	if strategy == StrategyAuto {
		r.platform = DetectPlatform(getenv)
		if len(r.platform.TrustedPresets) > 0 {
			entries = append(entries, r.platform.TrustedPresets...)
		}
	}

	prefixes, err := resolveTrustedEntries(entries)
	if err != nil {
		return nil, err
	}

	var builder netipx.IPSetBuilder
	for _, prefix := range prefixes {
		builder.AddPrefix(prefix)
	}
	set, err := builder.IPSet()
	if err != nil {
		return nil, fmt.Errorf("构建受信网段集合失败: %w", err)
	}
	r.trusted = set
	r.prefixes = set.Prefixes()

	// 「谁都信」按结果判定而不是按字面判定：写 all、写 0.0.0.0/0、写两条各覆盖一半
	// 最后合并成全集，都是同一件事，而只认字面的检查会漏掉后两种。
	if strategy != StrategyPeer &&
		(set.Contains(netip.IPv4Unspecified()) || set.Contains(netip.IPv6Unspecified())) {
		r.warnings = append(r.warnings,
			"受信网段覆盖了全部地址：任何客户端都能用一个 X-Forwarded-For 伪造自己的 IP，限流与封禁将随之失效")
	}

	// 单值头：只有被显式配置、或探测到的平台档案声明了才读。
	//
	// 默认不读 X-Real-IP 这类头是刻意的：它们没有链、没有顺序，一旦最外层代理没有
	// 覆写（只是转发），客户端就完全掌握了判定结果。转发链至少还有「最右一跳由
	// 可信方写下」这个约束可依赖。
	switch {
	case strings.TrimSpace(cfg.Header) != "":
		r.singleHdrs = []string{http.CanonicalHeaderKey(strings.TrimSpace(cfg.Header))}
	case strategy == StrategyAuto && r.platform.Header != "":
		r.singleHdrs = []string{http.CanonicalHeaderKey(r.platform.Header)}
	}

	if strategy == StrategyHeader && len(r.singleHdrs) == 0 {
		return nil, fmt.Errorf("CLIENT_IP_STRATEGY=header 必须同时配置 CLIENT_IP_HEADER")
	}

	// auto 档下平台档案可以把判定收敛成「固定跳数」（如某些 LB 恒定追加一跳）。
	hops := cfg.Hops
	if strategy == StrategyAuto && r.platform.Hops > 0 && hops <= 0 {
		hops = r.platform.Hops
	}
	r.hops = hops

	if err := r.buildChainStrategy(strategy, hops); err != nil {
		return nil, err
	}
	return r, nil
}

// buildChainStrategy 把「怎么读转发链」这一步交给 realclientip 的对应策略。
func (r *Resolver) buildChainStrategy(strategy Strategy, hops int) error {
	switch strategy {
	case StrategyPeer, StrategyHeader:
		// 都不读转发链：peer 档什么都不读，header 档只读那一个头。
		return nil
	case StrategyTrustedHops:
		if hops < 1 {
			return fmt.Errorf("CLIENT_IP_STRATEGY=trusted-hops 必须配置 CLIENT_IP_HOPS ≥ 1")
		}
		strat, err := realclientip.NewRightmostTrustedCountStrategy(r.listHeader, hops)
		if err != nil {
			return fmt.Errorf("构建固定跳数策略失败: %w", err)
		}
		r.chain, r.chainSource = strat, SourceHops
		return nil
	case StrategyLeftmost:
		// 注意这一档判「公网」用的是解析库自带的保留段清单（RFC1918 + 环回 +
		// CGNAT + 文档/测试段 …），**不看** TrustedProxies。这一档本来就不该配
		// 受信网段 —— 它信的是「最左边那条」，而不是「谁写的」。
		strat, err := realclientip.NewLeftmostNonPrivateStrategy(r.listHeader)
		if err != nil {
			return fmt.Errorf("构建最左公网策略失败: %w", err)
		}
		r.chain, r.chainSource = strat, SourceLeftmost
		return nil
	default: // auto / trusted-ranges
		if hops > 0 {
			strat, err := realclientip.NewRightmostTrustedCountStrategy(r.listHeader, hops)
			if err != nil {
				return fmt.Errorf("构建固定跳数策略失败: %w", err)
			}
			r.chain, r.chainSource = strat, SourceHops
			return nil
		}
		strat, err := realclientip.NewRightmostTrustedRangeStrategy(r.listHeader, prefixesToIPNets(r.prefixes))
		if err != nil {
			return fmt.Errorf("构建受信网段策略失败: %w", err)
		}
		r.chain, r.chainSource = strat, SourceChain
		return nil
	}
}

// Resolve 判定一个请求的客户端 IP。r 为 nil 时退化为「直连对端」，
// 这样零依赖装配（openapi / routes 子命令）不必再判一次 nil。
func (r *Resolver) Resolve(req *http.Request) Result {
	if req == nil {
		return Result{Source: SourceUnresolved}
	}
	res := Result{Source: SourceUnresolved}
	if r != nil {
		res.Strategy = r.strategy
		res.Platform = r.platform.Key
	} else {
		res.Strategy = StrategyPeer
	}

	peer, port, ok := splitPeer(req.RemoteAddr)
	if !ok {
		// 连对端都解析不出来（httptest 构造的裸请求就是这样）。
		// 这里返回无效结果而不是硬编一个地址：调用方据此保持 RemoteAddr 原样，
		// 免得在测试里凭空造出一个不存在的客户端。
		return res
	}
	res.Peer, res.PeerPort = peer, port
	res.IP, res.Source = peer, SourcePeer

	if r == nil || r.strategy == StrategyPeer {
		return res
	}

	// 转发链先记下来，不受信时也记：排障时「被忽略掉的是什么」和「采信了什么」
	// 一样重要 —— 只在采信时才记的话，`source=peer` 这个结论看不出任何缘由。
	res.Chain = forwardedChain(req.Header, r.listHeader)

	res.PeerTrusted = r.trusted != nil && r.trusted.Contains(peer)
	if !res.PeerTrusted {
		// 直连对端不受信 —— 它就是客户端本人，它写的任何转发头都不作数。
		// 这一条是整个包的安全底座：少了它，谁都能用一个 XFF 换 IP。
		return res
	}

	for _, header := range r.singleHdrs {
		raw := strings.TrimSpace(lastHeaderValue(req.Header, header))
		if raw == "" {
			continue
		}
		if addr, ok := parseAddr(raw); ok {
			res.IP, res.Source, res.Header = addr, SourceHeader, header
			return res
		}
	}

	if r.chain != nil {
		if raw := r.chain.ClientIP(req.Header, req.RemoteAddr); raw != "" {
			if addr, ok := parseAddr(raw); ok {
				res.IP, res.Source, res.Header = addr, r.chainSource, r.listHeader
				return res
			}
		}
	}

	// 转发链没给出可用结论（没有该头、条目全在受信集合内、或全是坏值）：
	// 保持在直连对端上。这比返回空好 —— 下游的限流与防火墙拿到空 IP 会直接拒绝请求。
	return res
}

// DebugHeader 是否回显判定过程。
func (r *Resolver) DebugHeader() bool {
	return r != nil && r.debugHeader
}

// Description 判定器的最终生效形态，供启动横幅与启动日志展示。
type Description struct {
	Strategy   Strategy
	Platform   string
	PlatformCN string
	ListHeader string
	Headers    []string
	Hops       int
	Prefixes   []netip.Prefix
	Warnings   []string
	TrustsAll  bool
}

// Describe 汇报生效配置。判定规则藏在几个环境变量的组合里，
// 不打出来的话「为什么线上 IP 不对」只能靠猜。
func (r *Resolver) Describe() Description {
	if r == nil {
		return Description{Strategy: StrategyPeer}
	}
	desc := Description{
		Strategy:   r.strategy,
		Platform:   r.platform.Key,
		PlatformCN: r.platform.Name,
		ListHeader: r.listHeader,
		Headers:    append([]string(nil), r.singleHdrs...),
		Hops:       r.hops,
		Prefixes:   append([]netip.Prefix(nil), r.prefixes...),
		Warnings:   append([]string(nil), r.warnings...),
	}
	if r.trusted != nil {
		// 受信集合覆盖了 0.0.0.0 / :: 就等于「谁都信」，此时转发头完全可伪造。
		desc.TrustsAll = r.trusted.Contains(netip.IPv4Unspecified()) || r.trusted.Contains(netip.IPv6Unspecified())
	}
	return desc
}

// PrefixStrings 受信网段的字符串形式（排序后），横幅与日志直接用。
func (d Description) PrefixStrings() []string {
	items := make([]string, 0, len(d.Prefixes))
	for _, prefix := range d.Prefixes {
		items = append(items, prefix.String())
	}
	sort.Strings(items)
	return items
}

// ── 解析辅助 ───────────────────────────────────────────────────────────

func parseStrategy(raw string) (Strategy, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", string(StrategyAuto):
		return StrategyAuto, nil
	case string(StrategyPeer), "remote-addr", "remoteaddr":
		return StrategyPeer, nil
	case string(StrategyTrustedRanges), "trusted-range", "ranges":
		return StrategyTrustedRanges, nil
	case string(StrategyTrustedHops), "trusted-hop", "hops":
		return StrategyTrustedHops, nil
	case string(StrategyHeader), "single-header":
		return StrategyHeader, nil
	case string(StrategyLeftmost), "leftmost-non-private":
		return StrategyLeftmost, nil
	default:
		return "", fmt.Errorf("未知的客户端 IP 判定方式 %q（可选 auto / peer / trusted-ranges / trusted-hops / header / leftmost）", raw)
	}
}

func parseListHeader(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return HeaderXForwardedFor, nil
	}
	switch http.CanonicalHeaderKey(value) {
	case HeaderXForwardedFor:
		return HeaderXForwardedFor, nil
	case HeaderForwarded:
		return HeaderForwarded, nil
	default:
		return "", fmt.Errorf("转发链头只能是 %s 或 %s，收到 %q", HeaderXForwardedFor, HeaderForwarded, raw)
	}
}

// splitPeer 从 RemoteAddr 里拆出地址与端口。
// 兼容没有端口的写法（部分测试与 unix socket 场景）。
func splitPeer(remoteAddr string) (netip.Addr, string, bool) {
	raw := strings.TrimSpace(remoteAddr)
	if raw == "" {
		return netip.Addr{}, "", false
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		if addr, ok := parseAddr(host); ok {
			return addr, port, true
		}
		return netip.Addr{}, "", false
	}
	if addr, ok := parseAddr(raw); ok {
		return addr, "", true
	}
	return netip.Addr{}, "", false
}

// parseAddr 归一化一个地址字符串。
//
// Unmap 是必须的：netip 里 ::ffff:10.0.0.1 与 10.0.0.1 是**不同**的值，
// 而 netip.Prefix.Contains 对 4-in-6 地址一律返回 false ——
// 少了这一步，双栈监听下的内网对端会被判成「不受信」，转发头就全被丢掉。
func parseAddr(raw string) (netip.Addr, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return netip.Addr{}, false
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	if idx := strings.LastIndexByte(value, '%'); idx > 0 {
		value = value[:idx] // 去掉 IPv6 zone：受信判定不看 zone
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() {
		return netip.Addr{}, false
	}
	return addr, true
}

// forwardedChain 取转发链的原样条目，仅供排障展示，不参与判定。
func forwardedChain(headers http.Header, listHeader string) []string {
	values := headers.Values(listHeader)
	if len(values) == 0 {
		return nil
	}
	items := make([]string, 0, len(values))
	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

// lastHeaderValue 取同名头的最后一个值。
// 单值头按 RFC 2616 本不该出现多次，真出现时最后一个来自最靠近本服务的那一跳。
func lastHeaderValue(headers http.Header, name string) string {
	values := headers.Values(name)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// prefixesToIPNets 把受信网段交给 realclientip 使用。
// 两边同源于一个 netipx.IPSet，因此「谁受信」在本包与库内部不可能出现分歧。
func prefixesToIPNets(prefixes []netip.Prefix) []net.IPNet {
	items := make([]net.IPNet, 0, len(prefixes))
	for _, prefix := range prefixes {
		if ipNet := netipx.PrefixIPNet(prefix); ipNet != nil {
			items = append(items, *ipNet)
		}
	}
	return items
}

func trimmedNonEmpty(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			items = append(items, value)
		}
	}
	return items
}
