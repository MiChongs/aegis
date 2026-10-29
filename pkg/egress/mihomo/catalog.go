package mihomo

// 控制台表单目录：每种节点 / 策略组 / 订阅 / 规则集有哪些字段、怎么填。
//
// 字段键与 mihomo 配置一一对应，嵌套字段用点号（ws-opts.path）。目录只覆盖常用字段，
// 其余字段在控制台的「YAML」页签里原样编辑，所以核心支持的东西这里都能配，只是有没有表单的区别。

// FieldSpec 一个表单字段。
type FieldSpec struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"` // text / number / bool / select / secret / list / textarea / map
	Section     string   `json:"section,omitempty"`
	Options     []string `json:"options,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Hint        string   `json:"hint,omitempty"`
	// ShowWhen 只在另一个字段取这些值时显示，形如 {"network": ["ws"]}。
	ShowWhen map[string][]string `json:"showWhen,omitempty"`
}

// TypeSpec 一种节点 / 策略组类型。
type TypeSpec struct {
	Type   string      `json:"type"`
	Label  string      `json:"label"`
	Hint   string      `json:"hint,omitempty"`
	Server bool        `json:"server"` // 需要 server / port
	Fields []FieldSpec `json:"fields"`
}

// RuleTypeSpec 一种分流规则。
type RuleTypeSpec struct {
	Type      string   `json:"type"`
	Label     string   `json:"label"`
	Hint      string   `json:"hint,omitempty"`
	Example   string   `json:"example,omitempty"`
	NoPayload bool     `json:"noPayload,omitempty"`
	Params    []string `json:"params,omitempty"` // no-resolve / src
	Logic     bool     `json:"logic,omitempty"`  // AND / OR / NOT / SUB-RULE：payload 是括号表达式
}

// Catalog 全部目录。
type Catalog struct {
	ProxyTypes         []TypeSpec     `json:"proxyTypes"`
	GroupTypes         []TypeSpec     `json:"groupTypes"`
	ProviderFields     []FieldSpec    `json:"providerFields"`
	RuleProviderFields []FieldSpec    `json:"ruleProviderFields"`
	RuleTypes          []RuleTypeSpec `json:"ruleTypes"`
	DNSFields          []FieldSpec    `json:"dnsFields"`
	Builtins           []string       `json:"builtins"`
	SecretKeys         []string       `json:"secretKeys"`
	ReservedKeys       []string       `json:"reservedKeys"`
	DefaultTestURL     string         `json:"defaultTestUrl"`
}

// DefaultTestURL 策略组与延迟测试的默认地址。
const DefaultTestURL = "https://www.gstatic.com/generate_204"

// SecretKeys 视为密钥的字段名（任意层级）：管理端读取时打码，保存时留空保持原值。
var SecretKeys = []string{
	"password", "uuid", "private-key", "private-key-passphrase", "pre-shared-key",
	"auth", "auth-str", "psk", "obfs-password", "token", "age-secret-key",
}

var (
	ssCiphers = []string{
		"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
		"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
		"chacha20-ietf", "xchacha20", "rc4-md5", "none",
	}
	clientFingerprints = []string{"", "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random"}
	networks           = []string{"tcp", "ws", "http", "h2", "grpc"}
)

func f(key, label, kind, section string) FieldSpec {
	return FieldSpec{Key: key, Label: label, Kind: kind, Section: section}
}

func (s FieldSpec) req() FieldSpec                   { s.Required = true; return s }
func (s FieldSpec) opts(options ...string) FieldSpec { s.Options = options; return s }
func (s FieldSpec) def(v any) FieldSpec              { s.Default = v; return s }
func (s FieldSpec) ph(v string) FieldSpec            { s.Placeholder = v; return s }
func (s FieldSpec) hint(v string) FieldSpec          { s.Hint = v; return s }
func (s FieldSpec) when(key string, values ...string) FieldSpec {
	s.ShowWhen = map[string][]string{key: values}
	return s
}

func udp() FieldSpec { return f("udp", "转发 UDP", "bool", "basic").def(true) }

// tlsFields 证书校验与指纹。sniKey 是该协议的 SNI 字段名（servername 或 sni）。
func tlsFields(sniKey string, gate string) []FieldSpec {
	out := []FieldSpec{
		f(sniKey, "SNI", "text", "tls").ph("留空用服务器地址"),
		f("skip-cert-verify", "跳过证书校验", "bool", "tls").hint("只用于自签名证书的自建节点"),
		f("client-fingerprint", "客户端指纹", "select", "tls").opts(clientFingerprints...).hint("uTLS 模拟浏览器的 TLS 指纹"),
		f("fingerprint", "证书指纹", "text", "tls").ph("SHA256，固定服务端证书"),
		f("alpn", "ALPN", "list", "tls").ph("h2\nhttp/1.1"),
	}
	if gate != "" {
		for i := range out {
			out[i] = out[i].when(gate, "true")
		}
	}
	return out
}

func realityFields() []FieldSpec {
	return []FieldSpec{
		f("reality-opts.public-key", "REALITY 公钥", "text", "tls").hint("填了公钥即启用 REALITY"),
		f("reality-opts.short-id", "REALITY Short ID", "text", "tls"),
	}
}

func transportFields(options ...string) []FieldSpec {
	if len(options) == 0 {
		options = networks
	}
	return []FieldSpec{
		f("network", "传输方式", "select", "transport").opts(options...).def("tcp"),
		f("ws-opts.path", "WebSocket 路径", "text", "transport").ph("/").when("network", "ws"),
		f("ws-opts.headers", "WebSocket 请求头", "map", "transport").ph("Host: example.com").when("network", "ws"),
		f("ws-opts.max-early-data", "Early Data 字节数", "number", "transport").when("network", "ws"),
		f("ws-opts.early-data-header-name", "Early Data 请求头", "text", "transport").ph("Sec-WebSocket-Protocol").when("network", "ws"),
		f("ws-opts.v2ray-http-upgrade", "HTTPUpgrade", "bool", "transport").when("network", "ws"),
		f("grpc-opts.grpc-service-name", "gRPC 服务名", "text", "transport").when("network", "grpc"),
		f("h2-opts.host", "HTTP/2 Host", "list", "transport").when("network", "h2"),
		f("h2-opts.path", "HTTP/2 路径", "text", "transport").when("network", "h2"),
		f("http-opts.path", "HTTP 路径", "list", "transport").when("network", "http"),
		f("http-opts.headers", "HTTP 请求头", "map", "transport").when("network", "http"),
		f("xhttp-opts.path", "XHTTP 路径", "text", "transport").when("network", "xhttp"),
		f("xhttp-opts.host", "XHTTP Host", "text", "transport").when("network", "xhttp"),
		f("xhttp-opts.mode", "XHTTP 模式", "select", "transport").opts("", "auto", "stream-one", "stream-up", "packet-up").when("network", "xhttp"),
	}
}

// dialFields 所有节点通用的拨号参数。dialer-proxy 在画布上用连线表达，不在这里。
func dialFields() []FieldSpec {
	return []FieldSpec{
		f("ip-version", "IP 版本", "select", "advanced").opts("", "dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer"),
		f("tfo", "TCP Fast Open", "bool", "advanced"),
		f("mptcp", "MPTCP", "bool", "advanced"),
		f("interface-name", "出站网卡", "text", "advanced"),
		f("routing-mark", "路由标记", "number", "advanced"),
		f("smux.enabled", "多路复用", "bool", "advanced"),
		f("smux.protocol", "复用协议", "select", "advanced").opts("smux", "yamux", "h2mux").def("h2mux").when("smux.enabled", "true"),
		f("smux.max-connections", "最大连接数", "number", "advanced").when("smux.enabled", "true"),
		f("smux.padding", "填充", "bool", "advanced").when("smux.enabled", "true"),
	}
}

func join(groups ...[]FieldSpec) []FieldSpec {
	out := []FieldSpec{}
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func proxyTypes() []TypeSpec {
	return []TypeSpec{
		{Type: "ss", Label: "Shadowsocks", Server: true, Hint: "含 2022 系列加密与 obfs / v2ray-plugin / shadow-tls 插件", Fields: join([]FieldSpec{
			f("cipher", "加密方式", "select", "basic").opts(ssCiphers...).def("aes-256-gcm").req(),
			f("password", "密码", "secret", "basic").req(),
			udp(),
			f("udp-over-tcp", "UDP over TCP", "bool", "basic"),
			f("plugin", "插件", "select", "transport").opts("", "obfs", "v2ray-plugin", "gost-plugin", "shadow-tls", "restls", "kcptun"),
			f("plugin-opts.mode", "插件模式", "text", "transport").ph("http / tls / websocket").when("plugin", "obfs", "v2ray-plugin", "gost-plugin"),
			f("plugin-opts.host", "插件 Host", "text", "transport").when("plugin", "obfs", "v2ray-plugin", "gost-plugin", "shadow-tls", "restls"),
			f("plugin-opts.path", "插件路径", "text", "transport").when("plugin", "v2ray-plugin", "gost-plugin"),
			f("plugin-opts.tls", "插件 TLS", "bool", "transport").when("plugin", "v2ray-plugin", "gost-plugin"),
			f("plugin-opts.password", "插件密码", "secret", "transport").when("plugin", "shadow-tls", "restls"),
			f("plugin-opts.version", "shadow-tls 版本", "number", "transport").def(3).when("plugin", "shadow-tls"),
			f("client-fingerprint", "客户端指纹", "select", "tls").opts(clientFingerprints...),
		}, dialFields())},
		{Type: "ssr", Label: "ShadowsocksR", Server: true, Fields: join([]FieldSpec{
			f("cipher", "加密方式", "select", "basic").opts("aes-128-cfb", "aes-192-cfb", "aes-256-cfb", "aes-128-ctr", "aes-256-ctr", "chacha20-ietf", "rc4-md5", "none").def("aes-256-cfb").req(),
			f("password", "密码", "secret", "basic").req(),
			f("obfs", "混淆", "select", "basic").opts("plain", "http_simple", "http_post", "random_head", "tls1.2_ticket_auth", "tls1.2_ticket_fastauth").def("plain").req(),
			f("obfs-param", "混淆参数", "text", "basic"),
			f("protocol", "协议", "select", "basic").opts("origin", "auth_sha1_v4", "auth_aes128_md5", "auth_aes128_sha1", "auth_chain_a", "auth_chain_b").def("origin").req(),
			f("protocol-param", "协议参数", "text", "basic"),
			udp(),
		}, dialFields())},
		{Type: "vmess", Label: "VMess", Server: true, Fields: join([]FieldSpec{
			f("uuid", "UUID", "secret", "basic").req(),
			f("alterId", "alterId", "number", "basic").def(0).hint("新节点一律为 0（AEAD）"),
			f("cipher", "加密", "select", "basic").opts("auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305").def("auto"),
			udp(),
			f("packet-encoding", "UDP 包编码", "select", "basic").opts("", "packetaddr", "xudp"),
			f("tls", "TLS", "bool", "tls"),
		}, tlsFields("servername", "tls"), realityFields(), transportFields(), dialFields())},
		{Type: "vless", Label: "VLESS", Server: true, Hint: "支持 XTLS Vision、REALITY、XHTTP", Fields: join([]FieldSpec{
			f("uuid", "UUID", "secret", "basic").req(),
			f("flow", "流控", "select", "basic").opts("", "xtls-rprx-vision"),
			f("encryption", "加密", "text", "basic").ph("留空为 none"),
			udp(),
			f("packet-encoding", "UDP 包编码", "select", "basic").opts("", "packetaddr", "xudp").def("xudp"),
			f("tls", "TLS", "bool", "tls").def(true),
		}, tlsFields("servername", "tls"), realityFields(), transportFields("tcp", "ws", "http", "h2", "grpc", "xhttp"), dialFields())},
		{Type: "trojan", Label: "Trojan", Server: true, Fields: join([]FieldSpec{
			f("password", "密码", "secret", "basic").req(),
			udp(),
		}, tlsFields("sni", ""), realityFields(), transportFields("tcp", "ws", "grpc"), []FieldSpec{
			f("ss-opts.enabled", "Trojan-Go 加密层", "bool", "transport"),
			f("ss-opts.method", "加密层方式", "select", "transport").opts("aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305").when("ss-opts.enabled", "true"),
			f("ss-opts.password", "加密层密码", "secret", "transport").when("ss-opts.enabled", "true"),
		}, dialFields())},
		{Type: "hysteria2", Label: "Hysteria 2", Server: true, Hint: "QUIC，支持端口跳跃与 salamander 混淆", Fields: join([]FieldSpec{
			f("password", "密码", "secret", "basic").req(),
			f("ports", "端口跳跃", "text", "basic").ph("20000-30000,443").hint("填了则忽略上面的端口"),
			f("hop-interval", "跳跃间隔（秒）", "number", "basic").when("ports", "*"),
			f("up", "上行带宽", "text", "basic").ph("30 Mbps"),
			f("down", "下行带宽", "text", "basic").ph("200 Mbps"),
			f("obfs", "混淆", "select", "basic").opts("", "salamander"),
			f("obfs-password", "混淆密码", "secret", "basic").when("obfs", "salamander"),
			f("sni", "SNI", "text", "tls"),
			f("skip-cert-verify", "跳过证书校验", "bool", "tls"),
			f("fingerprint", "证书指纹", "text", "tls"),
			f("alpn", "ALPN", "list", "tls").ph("h3"),
		}, dialFields())},
		{Type: "hysteria", Label: "Hysteria", Server: true, Fields: join([]FieldSpec{
			f("auth-str", "认证字符串", "secret", "basic"),
			f("ports", "端口跳跃", "text", "basic"),
			f("up", "上行带宽", "text", "basic").ph("30 Mbps").req(),
			f("down", "下行带宽", "text", "basic").ph("200 Mbps").req(),
			f("protocol", "传输", "select", "basic").opts("udp", "wechat-video", "faketcp").def("udp"),
			f("obfs", "混淆密码", "secret", "basic"),
			f("sni", "SNI", "text", "tls"),
			f("skip-cert-verify", "跳过证书校验", "bool", "tls"),
			f("alpn", "ALPN", "list", "tls"),
			f("fast-open", "Fast Open", "bool", "advanced"),
		}, dialFields())},
		{Type: "tuic", Label: "TUIC", Server: true, Hint: "v5 填 UUID 与密码，v4 填 token", Fields: join([]FieldSpec{
			f("uuid", "UUID", "secret", "basic"),
			f("password", "密码", "secret", "basic"),
			f("token", "Token（v4）", "secret", "basic"),
			f("ip", "服务器 IP", "text", "basic").hint("覆盖 DNS 解析结果"),
			f("congestion-controller", "拥塞控制", "select", "basic").opts("cubic", "new_reno", "bbr").def("bbr"),
			f("udp-relay-mode", "UDP 中继", "select", "basic").opts("native", "quic").def("native"),
			f("reduce-rtt", "0-RTT", "bool", "basic"),
			f("heartbeat-interval", "心跳间隔（毫秒）", "number", "advanced"),
			f("request-timeout", "请求超时（毫秒）", "number", "advanced"),
			f("sni", "SNI", "text", "tls"),
			f("disable-sni", "不发送 SNI", "bool", "tls"),
			f("skip-cert-verify", "跳过证书校验", "bool", "tls"),
			f("alpn", "ALPN", "list", "tls").ph("h3"),
		}, dialFields())},
		{Type: "wireguard", Label: "WireGuard", Server: true, Fields: join([]FieldSpec{
			f("ip", "本端 IPv4", "text", "basic").ph("172.16.0.2").req(),
			f("ipv6", "本端 IPv6", "text", "basic"),
			f("private-key", "本端私钥", "secret", "basic").req(),
			f("public-key", "对端公钥", "text", "basic").req(),
			f("pre-shared-key", "预共享密钥", "secret", "basic"),
			f("allowed-ips", "允许的地址", "list", "basic").ph("0.0.0.0/0\n::/0"),
			f("reserved", "reserved", "text", "basic").ph("209,98,59 或 base64"),
			f("mtu", "MTU", "number", "advanced").def(1408),
			udp(),
			f("remote-dns-resolve", "隧道内解析 DNS", "bool", "advanced"),
			f("dns", "隧道 DNS", "list", "advanced").when("remote-dns-resolve", "true"),
			f("persistent-keepalive", "保活间隔（秒）", "number", "advanced"),
		}, dialFields())},
		{Type: "anytls", Label: "AnyTLS", Server: true, Fields: join([]FieldSpec{
			f("password", "密码", "secret", "basic").req(),
			udp(),
			f("idle-session-check-interval", "空闲检查间隔（秒）", "number", "advanced"),
			f("idle-session-timeout", "空闲超时（秒）", "number", "advanced"),
			f("min-idle-session", "最少空闲会话", "number", "advanced"),
		}, tlsFields("sni", ""), dialFields())},
		{Type: "snell", Label: "Snell", Server: true, Fields: join([]FieldSpec{
			f("psk", "PSK", "secret", "basic").req(),
			f("version", "版本", "select", "basic").opts("1", "2", "3", "4", "5").def("3"),
			udp(),
			f("obfs-opts.mode", "混淆", "select", "transport").opts("", "http", "tls"),
			f("obfs-opts.host", "混淆 Host", "text", "transport"),
		}, dialFields())},
		{Type: "mieru", Label: "Mieru", Server: true, Fields: join([]FieldSpec{
			f("port-range", "端口范围", "text", "basic").ph("2090-2099").hint("与端口二选一"),
			f("transport", "传输", "select", "basic").opts("TCP", "UDP").def("TCP").req(),
			f("username", "用户名", "text", "basic").req(),
			f("password", "密码", "secret", "basic").req(),
			f("multiplexing", "多路复用", "select", "basic").opts("", "MULTIPLEXING_OFF", "MULTIPLEXING_LOW", "MULTIPLEXING_MIDDLE", "MULTIPLEXING_HIGH"),
			udp(),
		}, dialFields())},
		{Type: "socks5", Label: "SOCKS5", Server: true, Fields: join([]FieldSpec{
			f("username", "用户名", "text", "basic"),
			f("password", "密码", "secret", "basic"),
			udp(),
			f("tls", "TLS", "bool", "tls"),
			f("skip-cert-verify", "跳过证书校验", "bool", "tls").when("tls", "true"),
			f("fingerprint", "证书指纹", "text", "tls").when("tls", "true"),
		}, dialFields())},
		{Type: "http", Label: "HTTP", Server: true, Fields: join([]FieldSpec{
			f("username", "用户名", "text", "basic"),
			f("password", "密码", "secret", "basic"),
			f("headers", "附加请求头", "map", "basic"),
			f("tls", "HTTPS", "bool", "tls"),
			f("sni", "SNI", "text", "tls").when("tls", "true"),
			f("skip-cert-verify", "跳过证书校验", "bool", "tls").when("tls", "true"),
			f("fingerprint", "证书指纹", "text", "tls").when("tls", "true"),
		}, dialFields())},
		{Type: "ssh", Label: "SSH", Server: true, Fields: join([]FieldSpec{
			f("username", "用户名", "text", "basic").req(),
			f("password", "密码", "secret", "basic"),
			f("private-key", "私钥", "secret", "basic").hint("PEM 全文"),
			f("private-key-passphrase", "私钥口令", "secret", "basic"),
			f("host-key", "主机公钥", "list", "basic").hint("一行一个，留空不校验"),
			f("host-key-algorithms", "主机公钥算法", "list", "advanced"),
		}, dialFields())},
		{Type: "direct", Label: "直连", Hint: "可指定出站网卡或 IP 版本的直连出口", Fields: dialFields()},
		{Type: "dns", Label: "DNS 劫持", Hint: "把连接交给核心的 DNS 模块", Fields: nil},
	}
}

func groupTypes() []TypeSpec {
	common := []FieldSpec{
		f("filter", "筛选（正则）", "text", "members").ph("(?i)港|hk|hong kong").hint("只保留名字匹配的订阅节点"),
		f("exclude-filter", "排除（正则）", "text", "members").ph("(?i)过期|剩余|官网"),
		f("exclude-type", "排除类型", "text", "members").ph("ss|http"),
		f("include-all-proxies", "包含全部节点", "bool", "members"),
		f("include-all-providers", "包含全部订阅", "bool", "members"),
		f("disable-udp", "禁用 UDP", "bool", "advanced"),
		f("hidden", "在面板中隐藏", "bool", "advanced"),
		f("icon", "图标地址", "text", "advanced"),
	}
	health := []FieldSpec{
		f("url", "测试地址", "text", "health").def(DefaultTestURL),
		f("interval", "测试间隔（秒）", "number", "health").def(300),
		f("timeout", "超时（毫秒）", "number", "health").def(5000),
		f("lazy", "无流量时不测", "bool", "health").def(true),
		f("max-failed-times", "失败几次判死", "number", "health").def(5),
		f("expected-status", "期望状态码", "text", "health").ph("204 或 200-299"),
	}
	return []TypeSpec{
		{Type: "select", Label: "手动选择", Hint: "在面板上手动切换，选择会被记住；第一个成员是默认选项", Fields: join(common)},
		{Type: "url-test", Label: "自动测速", Hint: "定时测速，选延迟最低的节点", Fields: join(health, []FieldSpec{
			f("tolerance", "容差（毫秒）", "number", "health").def(50).hint("新节点比当前快超过这个值才切换"),
		}, common)},
		{Type: "fallback", Label: "故障转移", Hint: "按成员顺序选第一个可用节点", Fields: join(health, common)},
		{Type: "load-balance", Label: "负载均衡", Hint: "在可用节点间分摊连接", Fields: join([]FieldSpec{
			f("strategy", "分摊方式", "select", "basic").opts("consistent-hashing", "round-robin", "sticky-sessions").def("consistent-hashing").hint("一致性哈希：同一目标地址固定走同一节点"),
		}, health, common)},
		{Type: "relay", Label: "链式中继", Hint: "按成员顺序逐跳转发（已不推荐，节点上的「前置代理」连线更灵活）", Fields: nil},
	}
}

func providerFields() []FieldSpec {
	return []FieldSpec{
		f("type", "来源", "select", "basic").opts("http", "file", "inline").def("http").req(),
		f("url", "订阅地址", "secret", "basic").when("type", "http").hint("订阅地址通常带令牌，按密钥保存"),
		f("path", "本地文件", "text", "basic").ph("./proxies/name.yaml").hint("http 来源的缓存位置，file 来源的读取位置"),
		f("interval", "更新间隔（秒）", "number", "basic").def(3600).when("type", "http"),
		f("proxy", "下载经由", "text", "basic").ph("DIRECT").hint("下载订阅时走的出站，填策略组或节点名").when("type", "http"),
		f("header", "请求头", "map", "basic").ph("User-Agent: clash.meta").when("type", "http"),
		f("size-limit", "大小上限（字节）", "number", "advanced").when("type", "http"),
		f("filter", "筛选（正则）", "text", "filter"),
		f("exclude-filter", "排除（正则）", "text", "filter").ph("(?i)过期|剩余|流量|官网"),
		f("exclude-type", "排除类型", "text", "filter"),
		f("health-check.enable", "健康检查", "bool", "health").def(true),
		f("health-check.url", "测试地址", "text", "health").def(DefaultTestURL).when("health-check.enable", "true"),
		f("health-check.interval", "检查间隔（秒）", "number", "health").def(600).when("health-check.enable", "true"),
		f("health-check.timeout", "超时（毫秒）", "number", "health").def(5000).when("health-check.enable", "true"),
		f("health-check.lazy", "无流量时不测", "bool", "health").def(true).when("health-check.enable", "true"),
		f("override.additional-prefix", "名称前缀", "text", "override").ph("[机场] "),
		f("override.additional-suffix", "名称后缀", "text", "override"),
		f("override.udp", "强制 UDP", "bool", "override"),
		f("override.skip-cert-verify", "强制跳过证书校验", "bool", "override"),
		f("override.ip-version", "IP 版本", "select", "override").opts("", "dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer"),
		f("override.tfo", "TCP Fast Open", "bool", "override"),
	}
}

func ruleProviderFields() []FieldSpec {
	return []FieldSpec{
		f("type", "来源", "select", "basic").opts("http", "file", "inline").def("http").req(),
		f("behavior", "内容类型", "select", "basic").opts("domain", "ipcidr", "classical").def("domain").req(),
		f("format", "格式", "select", "basic").opts("yaml", "text", "mrs").def("mrs").hint("mrs 是二进制格式，体积小、加载快，只支持 domain / ipcidr"),
		f("url", "地址", "text", "basic").when("type", "http"),
		f("path", "本地文件", "text", "basic"),
		f("interval", "更新间隔（秒）", "number", "basic").def(86400).when("type", "http"),
		f("proxy", "下载经由", "text", "basic").when("type", "http"),
		f("payload", "内容", "list", "basic").when("type", "inline"),
	}
}

func ruleTypes() []RuleTypeSpec {
	return []RuleTypeSpec{
		{Type: "DOMAIN-SUFFIX", Label: "域名后缀", Example: "google.com"},
		{Type: "DOMAIN", Label: "完整域名", Example: "www.google.com"},
		{Type: "DOMAIN-KEYWORD", Label: "域名关键字", Example: "google"},
		{Type: "DOMAIN-WILDCARD", Label: "域名通配", Example: "*.google.com"},
		{Type: "DOMAIN-REGEX", Label: "域名正则", Example: `^api\.`},
		{Type: "GEOSITE", Label: "GeoSite 分类", Example: "openai", Hint: "需要 GeoSite 数据库"},
		{Type: "RULE-SET", Label: "规则集", Example: "规则集名称", Params: []string{"no-resolve", "src"}},
		{Type: "IP-CIDR", Label: "IPv4 网段", Example: "8.8.8.0/24", Params: []string{"no-resolve", "src"}},
		{Type: "IP-CIDR6", Label: "IPv6 网段", Example: "2001:db8::/32", Params: []string{"no-resolve", "src"}},
		{Type: "IP-SUFFIX", Label: "IP 后缀", Example: "8.8.8.8/24", Params: []string{"no-resolve", "src"}},
		{Type: "IP-ASN", Label: "ASN", Example: "13335", Params: []string{"no-resolve", "src"}},
		{Type: "GEOIP", Label: "GeoIP 国家", Example: "CN", Params: []string{"no-resolve", "src"}},
		{Type: "DST-PORT", Label: "目标端口", Example: "443 或 8000-9000"},
		{Type: "SRC-PORT", Label: "来源端口", Example: "7890"},
		{Type: "NETWORK", Label: "网络类型", Example: "tcp"},
		{Type: "IN-USER", Label: "入站用户", Hint: "按 SOCKS / HTTP 入站的用户名匹配；网关流量都用同一个用户，一般用不到"},
		{Type: "AND", Label: "同时满足", Example: "((DOMAIN-SUFFIX,google.com),(DST-PORT,443))", Logic: true},
		{Type: "OR", Label: "任一满足", Example: "((DOMAIN,a.com),(DOMAIN,b.com))", Logic: true},
		{Type: "NOT", Label: "不满足", Example: "((GEOIP,CN))", Logic: true},
		{Type: "SUB-RULE", Label: "子规则", Example: "(NETWORK,tcp)", Logic: true},
		{Type: "MATCH", Label: "兜底", NoPayload: true, Hint: "以上都没命中时；写在最后"},
	}
}

func dnsFields() []FieldSpec {
	return []FieldSpec{
		f("enable", "启用核心 DNS", "bool", "basic").def(true),
		f("ipv6", "解析 IPv6", "bool", "basic"),
		f("cache-algorithm", "缓存算法", "select", "basic").opts("lru", "arc").def("arc"),
		f("respect-rules", "按分流规则选 DNS 出站", "bool", "basic").hint("解析请求本身也按规则走代理，避免 DNS 泄露"),
		f("default-nameserver", "引导 DNS", "list", "servers").ph("223.5.5.5\n119.29.29.29").hint("只用于解析下面那些 DoH / DoT 服务器的域名，必须是 IP"),
		f("nameserver", "默认 DNS", "list", "servers").ph("https://dns.alidns.com/dns-query\nhttps://doh.pub/dns-query"),
		f("proxy-server-nameserver", "节点域名解析", "list", "servers").ph("https://dns.alidns.com/dns-query").hint("解析节点服务器地址用"),
		f("direct-nameserver", "直连流量 DNS", "list", "servers"),
		f("fallback", "后备 DNS", "list", "servers").ph("https://1.1.1.1/dns-query\ntls://8.8.4.4"),
		f("nameserver-policy", "按域名指定 DNS", "map", "servers").ph("geosite:cn: https://dns.alidns.com/dns-query"),
	}
}

// BuildCatalog 返回完整目录。
func BuildCatalog() Catalog {
	reserved := make([]string, 0, len(reservedKeys))
	for key := range reservedKeys {
		reserved = append(reserved, key)
	}
	return Catalog{
		ProxyTypes:         proxyTypes(),
		GroupTypes:         groupTypes(),
		ProviderFields:     providerFields(),
		RuleProviderFields: ruleProviderFields(),
		RuleTypes:          ruleTypes(),
		DNSFields:          dnsFields(),
		Builtins:           Builtins,
		SecretKeys:         SecretKeys,
		ReservedKeys:       uniqueStrings(reserved),
		DefaultTestURL:     DefaultTestURL,
	}
}
