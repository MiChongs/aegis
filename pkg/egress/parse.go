package egress

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// 本文件提供三种配置写法之间的统一入口：
//
//	紧凑 DSL   写在 .env 里，一行讲清「哪些域名走哪条线」
//	JSON       管理端 API 与配置文件用，字段全集
//	端点 URL   两者共用的端点简写，socks5h://user:pass@host:1080?region=hk
//
// 三种写法最终都收敛到同一个 Config，因此不存在「DSL 能表达但 API 不能」的差异。

// ParseConfigJSON 解析完整配置 JSON。
func ParseConfigJSON(data []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析出海网关配置失败: %w", err)
	}
	return cfg, nil
}

// ParseEndpoints 解析端点定义，自动识别 JSON 数组与紧凑 DSL。
//
// DSL 形如（; 或换行分隔）：
//
//	hk=socks5h://user:pass@10.0.0.1:1080?region=hk&weight=2
//	us=http://proxy.us.internal:3128
//	jump=ssh://ops@bastion.internal:22?user=ops
func ParseEndpoints(raw string) ([]EndpointConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var items []EndpointConfig
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("解析端点 JSON 失败: %w", err)
		}
		return items, nil
	}

	out := make([]EndpointConfig, 0, 4)
	for index, item := range splitEntries(raw) {
		name, rawURL := splitNamedEntry(item)
		endpoint, err := ParseEndpointURL(name, rawURL)
		if err != nil {
			return nil, fmt.Errorf("第 %d 个端点: %w", index+1, err)
		}
		out = append(out, endpoint)
	}
	return out, nil
}

// ParseEndpointURL 把一条端点 URL 解析成端点配置。
//
// 支持的 scheme：direct / http / https / socks5 / socks5h / socks / ssh /
// trojan / ss（shadowsocks）。
//
// 通用查询参数：weight / region / remark / via / probeUrl / dialTimeoutMs /
// tls / sni / insecure / alpn / forward。
// 协议特有：ssh 用 user；ss 用 method（也可写成 ss://method:password@host:port）。
func ParseEndpointURL(name, raw string) (EndpointConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return EndpointConfig{}, fmt.Errorf("端点地址为空")
	}
	if !strings.Contains(raw, "://") {
		// 裸 host:port 默认按 http 代理处理，这是最常见的写法。
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return EndpointConfig{}, fmt.Errorf("端点地址 %q 无效: %w", raw, err)
	}

	cfg := EndpointConfig{Name: strings.TrimSpace(name)}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "direct":
		cfg.Protocol = ProtocolDirect
	case "http":
		cfg.Protocol = ProtocolHTTP
	case "https":
		cfg.Protocol = ProtocolHTTPS
	case "ssh":
		cfg.Protocol = ProtocolSSH
	case "trojan":
		cfg.Protocol = ProtocolTrojan
	case "ss", "shadowsocks":
		cfg.Protocol = ProtocolShadowsocks
	default:
		protocol, ok := socksSchemeProtocol(scheme)
		if !ok {
			return EndpointConfig{}, fmt.Errorf("不支持的端点协议: %s", scheme)
		}
		cfg.Protocol = protocol
	}

	if cfg.Protocol != ProtocolDirect {
		cfg.Address = withDefaultPort(parsed.Host, cfg.Protocol)
		if _, _, err := net.SplitHostPort(cfg.Address); err != nil {
			return EndpointConfig{}, fmt.Errorf("端点地址缺少端口: %s", parsed.Host)
		}
	}
	if cfg.Name == "" {
		cfg.Name = deriveEndpointName(cfg)
	}

	username, password := credentialsFromURL(parsed, cfg.Protocol)
	query := parsed.Query()

	switch cfg.Protocol {
	case ProtocolShadowsocks:
		// ss URI 约定 userinfo 是 method:password（可能整体 base64）。
		cfg.Shadowsocks.Method = firstNonEmpty(query.Get("method"), username, "aes-256-gcm")
		cfg.Shadowsocks.Password = password
	case ProtocolSSH:
		cfg.SSH.User = firstNonEmpty(query.Get("user"), username)
		cfg.SSH.Password = password
		cfg.SSH.HostKeyFingerprint = query.Get("hostKeyFingerprint")
	case ProtocolTrojan:
		// trojan://password@host:port，口令写在 user 位。
		cfg.Password = firstNonEmpty(password, username)
	default:
		cfg.Username = username
		cfg.Password = password
	}

	cfg.Region = query.Get("region")
	cfg.Remark = query.Get("remark")
	cfg.Via = query.Get("via")
	cfg.ProbeURL = query.Get("probeUrl")
	if v := query.Get("weight"); v != "" {
		weight, err := strconv.Atoi(v)
		if err != nil || weight <= 0 {
			return EndpointConfig{}, fmt.Errorf("weight 必须是正整数: %s", v)
		}
		cfg.Weight = weight
	}
	if v := query.Get("dialTimeoutMs"); v != "" {
		timeout, err := strconv.Atoi(v)
		if err != nil || timeout <= 0 {
			return EndpointConfig{}, fmt.Errorf("dialTimeoutMs 必须是正整数: %s", v)
		}
		cfg.DialTimeoutMS = timeout
	}
	if truthy(query.Get("disabled")) {
		cfg.Enabled = boolPtr(false)
	}
	if truthy(query.Get("forward")) {
		cfg.HTTPForwardMode = true
	}
	if truthy(query.Get("tls")) || cfg.Protocol == ProtocolHTTPS || cfg.Protocol == ProtocolTrojan {
		cfg.TLS.Enabled = true
	}
	if sni := query.Get("sni"); sni != "" {
		cfg.TLS.ServerName = sni
	}
	if truthy(query.Get("insecure")) {
		cfg.TLS.InsecureSkipVerify = true
	}
	if alpn := query.Get("alpn"); alpn != "" {
		cfg.TLS.ALPN = splitCSV(alpn)
	}
	return cfg, nil
}

// ParseRules 解析规则定义，自动识别 JSON 数组与紧凑 DSL。
//
// DSL 形如（; 或换行分隔）：
//
//	hk|us=*.google.com,*.googleapis.com
//	us@round_robin=*.stripe.com
//	direct=*.aliyuncs.com
//	reject=*.doubleclick.net
//
// 等号左边是端点名列表（| 分隔，可选 @策略），也可以是 direct / reject；
// 右边是域名后缀列表。更复杂的条件（端口 / CIDR / Profile）请用 JSON。
func ParseRules(raw string) ([]RuleConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var items []RuleConfig
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("解析规则 JSON 失败: %w", err)
		}
		return items, nil
	}

	out := make([]RuleConfig, 0, 4)
	for index, item := range splitEntries(raw) {
		targets, suffixes, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("第 %d 条规则缺少 '='：%s", index+1, item)
		}
		targets, strategy := parseStrategySuffix(targets)
		rule := RuleConfig{
			Name:     fmt.Sprintf("env-%d", index+1),
			Priority: index + 1,
			Strategy: strategy,
			Match:    MatchConfig{DomainSuffixes: splitCSV(suffixes)},
		}
		names := splitPipe(targets)
		switch {
		case len(names) == 1 && strings.EqualFold(names[0], string(ActionDirect)):
			rule.Action = ActionDirect
		case len(names) == 1 && strings.EqualFold(names[0], string(ActionReject)):
			rule.Action = ActionReject
		case len(names) == 0:
			return nil, fmt.Errorf("第 %d 条规则没有指定端点", index+1)
		default:
			rule.Action = ActionProxy
			rule.Endpoints = names
		}
		if len(rule.Match.DomainSuffixes) == 0 {
			return nil, fmt.Errorf("第 %d 条规则没有域名后缀", index+1)
		}
		out = append(out, rule)
	}
	return out, nil
}

// parseStrategySuffix 拆出 "hk|us@round_robin" 里的策略部分。
func parseStrategySuffix(raw string) (string, Strategy) {
	targets, strategy, ok := strings.Cut(raw, "@")
	if !ok {
		return strings.TrimSpace(raw), ""
	}
	return strings.TrimSpace(targets), normalizeStrategy(Strategy(strategy))
}

// splitEntries 按 ; 或换行拆分条目，忽略空行与 # 注释。
func splitEntries(raw string) []string {
	replaced := strings.ReplaceAll(raw, "\r\n", "\n")
	replaced = strings.ReplaceAll(replaced, ";", "\n")
	out := make([]string, 0, 8)
	for _, line := range strings.Split(replaced, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitNamedEntry 从 "hk=socks5h://..." 里拆出名字。
// 只有当 '=' 出现在 '://' 之前才当作名字分隔符，否则查询串里的 '=' 会被误伤。
func splitNamedEntry(item string) (name, rest string) {
	eq := strings.Index(item, "=")
	scheme := strings.Index(item, "://")
	if eq >= 0 && (scheme < 0 || eq < scheme) {
		return strings.TrimSpace(item[:eq]), strings.TrimSpace(item[eq+1:])
	}
	return "", strings.TrimSpace(item)
}

// credentialsFromURL 取出 userinfo；shadowsocks 允许整体 base64 编码。
func credentialsFromURL(parsed *url.URL, protocol Protocol) (username, password string) {
	if parsed.User == nil {
		return "", ""
	}
	username = parsed.User.Username()
	password, _ = parsed.User.Password()
	if protocol == ProtocolShadowsocks && password == "" && username != "" {
		if decoded, err := decodeBase64Loose(username); err == nil {
			if method, secret, ok := strings.Cut(decoded, ":"); ok {
				return method, secret
			}
		}
	}
	return username, password
}

func decodeBase64Loose(raw string) (string, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(raw); err == nil {
			return string(decoded), nil
		}
	}
	return "", fmt.Errorf("不是合法的 base64")
}

// withDefaultPort 端点省略端口时按协议补默认值。
func withDefaultPort(host string, protocol Protocol) string {
	if host == "" {
		return host
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	switch protocol {
	case ProtocolHTTP:
		return net.JoinHostPort(host, "8080")
	case ProtocolHTTPS, ProtocolTrojan:
		return net.JoinHostPort(host, "443")
	case ProtocolSOCKS5, ProtocolSOCKS5H:
		return net.JoinHostPort(host, "1080")
	case ProtocolSSH:
		return net.JoinHostPort(host, "22")
	case ProtocolShadowsocks:
		return net.JoinHostPort(host, "8388")
	default:
		return host
	}
}

func deriveEndpointName(cfg EndpointConfig) string {
	if cfg.Address == "" {
		return string(cfg.Protocol)
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" {
		return string(cfg.Protocol)
	}
	return strings.ReplaceAll(host, ":", "-")
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func splitPipe(raw string) []string {
	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
