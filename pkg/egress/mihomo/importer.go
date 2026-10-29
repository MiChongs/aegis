package mihomo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// ImportResult 导入一份配置或一组分享链接的结果。
type ImportResult struct {
	Profile  Profile  `json:"profile"`
	Format   string   `json:"format"` // yaml / uri
	Warnings []string `json:"warnings,omitempty"`
}

// Import 识别并解析 Clash / mihomo 的 YAML 配置，或 ss:// vmess:// 这类分享链接
// （一行一个，也接受整段 base64，即常见的 v2ray 订阅格式）。
func Import(data []byte) (ImportResult, error) {
	text := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if text == "" {
		return ImportResult{}, fmt.Errorf("内容为空")
	}
	if !strings.Contains(text, "://") {
		if decoded, ok := decodeBase64(text); ok && strings.Contains(decoded, "://") {
			text = strings.TrimSpace(decoded)
		}
	}
	if looksLikeURIList(text) {
		return importURIs(text)
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
		return ImportResult{}, fmt.Errorf("既不是 YAML 配置也不是分享链接: %v", err)
	}
	if raw == nil {
		return ImportResult{}, fmt.Errorf("YAML 里没有任何内容")
	}
	return importYAML(raw)
}

func looksLikeURIList(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		scheme, _, ok := strings.Cut(line, "://")
		return ok && !strings.ContainsAny(scheme, " :\t")
	}
	return false
}

func importYAML(raw map[string]any) (ImportResult, error) {
	out := ImportResult{Format: "yaml"}
	p := &out.Profile
	ignored := []string{}

	for key, value := range raw {
		switch key {
		case "proxies":
			for _, item := range asList(value) {
				if m := asMap(item); m != nil {
					p.Proxies = append(p.Proxies, m)
				}
			}
		case "proxy-providers":
			p.ProxyProviders = asMapOfMaps(value)
		case "proxy-groups":
			for _, item := range asList(value) {
				if m := asMap(item); m != nil {
					p.ProxyGroups = append(p.ProxyGroups, m)
				}
			}
		case "rules":
			for _, item := range asList(value) {
				if s, ok := item.(string); ok {
					p.Rules = append(p.Rules, strings.TrimSpace(s))
				}
			}
		case "rule-providers":
			p.RuleProviders = asMapOfMaps(value)
		case "dns":
			p.DNS = asMap(value)
		case "mode":
			p.Settings.Mode = strings.ToLower(fmt.Sprint(value))
		case "log-level":
			p.Settings.LogLevel = fmt.Sprint(value)
		case "ipv6":
			p.Settings.IPv6 = asBool(value)
		case "unified-delay":
			p.Settings.UnifiedDelay = asBool(value)
		case "tcp-concurrent":
			p.Settings.TCPConcurrent = asBool(value)
		case "global-client-fingerprint":
			p.Settings.GlobalClientFingerprint = fmt.Sprint(value)
		case "keep-alive-idle":
			p.Settings.KeepAliveIdle = asInt(value)
		case "keep-alive-interval":
			p.Settings.KeepAliveInterval = asInt(value)
		case "geodata-mode":
			p.Settings.GeodataMode = asBool(value)
		case "geodata-loader":
			p.Settings.GeodataLoader = fmt.Sprint(value)
		case "geo-auto-update":
			p.Settings.GeoAutoUpdate = asBool(value)
		case "geo-update-interval":
			p.Settings.GeoUpdateInterval = asInt(value)
		case "global-ua":
			p.Settings.GlobalUA = fmt.Sprint(value)
		case "geox-url":
			p.Settings.GeoxURL = map[string]string{}
			for k, v := range asMap(value) {
				p.Settings.GeoxURL[k] = fmt.Sprint(v)
			}
		case "find-process-mode", "unified-delay-url":
			// 托管核心只服务网关自己的出站，按进程匹配没有意义。
		default:
			if ReservedKey(key) {
				ignored = append(ignored, key)
				continue
			}
			if p.Extra == nil {
				p.Extra = map[string]any{}
			}
			p.Extra[key] = value
		}
	}
	if len(ignored) > 0 {
		sort.Strings(ignored)
		out.Warnings = append(out.Warnings, "以下键由网关托管，已忽略: "+strings.Join(ignored, "、"))
	}
	if p.Empty() && len(p.Rules) == 0 {
		return out, fmt.Errorf("配置里没有节点、订阅或策略组")
	}
	dropped := 0
	kept := p.Rules[:0]
	for _, rule := range p.Rules {
		if strings.HasPrefix(strings.ToUpper(rule), "PROCESS-") || strings.HasPrefix(strings.ToUpper(rule), "UID,") || strings.HasPrefix(strings.ToUpper(rule), "IN-PORT,") {
			dropped++
			continue
		}
		kept = append(kept, rule)
	}
	p.Rules = kept
	if dropped > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("去掉了 %d 条按进程 / 入站端口匹配的规则（网关是唯一的入站）", dropped))
	}
	return out, nil
}

func importURIs(text string) (ImportResult, error) {
	out := ImportResult{Format: "uri"}
	names := map[string]int{}
	failed := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxy, err := ParseURI(line)
		if err != nil {
			failed++
			if failed <= 5 {
				out.Warnings = append(out.Warnings, err.Error())
			}
			continue
		}
		name := stringField(proxy, "name")
		if name == "" {
			name = fmt.Sprintf("%s %s", proxy["type"], proxy["server"])
		}
		name = strings.ReplaceAll(name, ",", " ")
		if n := names[name]; n > 0 {
			names[name] = n + 1
			name = fmt.Sprintf("%s %d", name, n+1)
		} else {
			names[name] = 1
		}
		proxy["name"] = name
		out.Profile.Proxies = append(out.Profile.Proxies, proxy)
	}
	if failed > 5 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("另有 %d 条链接无法识别", failed-5))
	}
	if len(out.Profile.Proxies) == 0 {
		return out, fmt.Errorf("没有识别出任何节点")
	}
	return out, nil
}

// ParseURI 解析一条分享链接为 mihomo 节点。
func ParseURI(raw string) (map[string]any, error) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return nil, fmt.Errorf("不是分享链接: %s", truncate(raw, 40))
	}
	switch strings.ToLower(scheme) {
	case "ss":
		return parseSS(rest)
	case "ssr":
		return parseSSR(rest)
	case "vmess":
		return parseVMess(rest)
	case "vless":
		return parseStdURI("vless", raw)
	case "trojan":
		return parseStdURI("trojan", raw)
	case "hysteria2", "hy2":
		return parseStdURI("hysteria2", raw)
	case "hysteria":
		return parseStdURI("hysteria", raw)
	case "tuic":
		return parseStdURI("tuic", raw)
	case "anytls":
		return parseStdURI("anytls", raw)
	case "socks", "socks5", "socks5h":
		return parseStdURI("socks5", raw)
	case "http", "https":
		return parseStdURI("http", raw)
	default:
		return nil, fmt.Errorf("暂不支持 %s:// 链接", scheme)
	}
}

func parseSS(rest string) (map[string]any, error) {
	body, fragment, _ := strings.Cut(rest, "#")
	name, _ := url.PathUnescape(fragment)
	body, query, _ := strings.Cut(body, "?")
	body = strings.TrimSuffix(body, "/")

	var userinfo, hostport string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userinfo, hostport = body[:at], body[at+1:]
		if decoded, ok := decodeBase64(userinfo); ok {
			userinfo = decoded
		} else if unescaped, err := url.PathUnescape(userinfo); err == nil {
			userinfo = unescaped
		}
	} else {
		decoded, ok := decodeBase64(body)
		if !ok {
			return nil, fmt.Errorf("ss 链接无法解码")
		}
		at := strings.LastIndex(decoded, "@")
		if at < 0 {
			return nil, fmt.Errorf("ss 链接缺少服务器地址")
		}
		userinfo, hostport = decoded[:at], decoded[at+1:]
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok {
		return nil, fmt.Errorf("ss 链接缺少加密方式或密码")
	}
	host, port, err := splitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	proxy := map[string]any{"name": name, "type": "ss", "server": host, "port": port, "cipher": method, "password": password, "udp": true}
	if query != "" {
		values, _ := url.ParseQuery(query)
		if plugin := values.Get("plugin"); plugin != "" {
			parts := strings.Split(plugin, ";")
			opts := map[string]any{}
			switch {
			case strings.Contains(parts[0], "obfs"):
				proxy["plugin"] = "obfs"
				for _, part := range parts[1:] {
					k, v, _ := strings.Cut(part, "=")
					switch k {
					case "obfs":
						opts["mode"] = v
					case "obfs-host":
						opts["host"] = v
					}
				}
			case strings.Contains(parts[0], "v2ray"):
				proxy["plugin"] = "v2ray-plugin"
				opts["mode"] = "websocket"
				for _, part := range parts[1:] {
					k, v, _ := strings.Cut(part, "=")
					switch k {
					case "host", "path", "mode":
						opts[k] = v
					case "tls":
						opts["tls"] = true
					}
				}
			}
			if len(opts) > 0 {
				proxy["plugin-opts"] = opts
			}
		}
	}
	return proxy, nil
}

func parseSSR(rest string) (map[string]any, error) {
	decoded, ok := decodeBase64(rest)
	if !ok {
		return nil, fmt.Errorf("ssr 链接无法解码")
	}
	main, query, _ := strings.Cut(decoded, "/?")
	parts := strings.Split(main, ":")
	if len(parts) < 6 {
		return nil, fmt.Errorf("ssr 链接字段不全")
	}
	n := len(parts)
	password, _ := decodeBase64(parts[n-1])
	host := strings.Join(parts[:n-5], ":")
	port, err := strconv.Atoi(parts[n-5])
	if err != nil {
		return nil, fmt.Errorf("ssr 端口无效")
	}
	proxy := map[string]any{
		"type": "ssr", "server": host, "port": port, "protocol": parts[n-4], "cipher": parts[n-3],
		"obfs": parts[n-2], "password": password, "udp": true,
	}
	values, _ := url.ParseQuery(query)
	if v, ok := decodeBase64(values.Get("obfsparam")); ok && v != "" {
		proxy["obfs-param"] = v
	}
	if v, ok := decodeBase64(values.Get("protoparam")); ok && v != "" {
		proxy["protocol-param"] = v
	}
	if v, ok := decodeBase64(values.Get("remarks")); ok {
		proxy["name"] = v
	}
	return proxy, nil
}

func parseVMess(rest string) (map[string]any, error) {
	decoded, ok := decodeBase64(strings.SplitN(rest, "#", 2)[0])
	if !ok {
		return nil, fmt.Errorf("vmess 链接无法解码")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(decoded), &v); err != nil {
		return nil, fmt.Errorf("vmess 链接不是 JSON")
	}
	str := func(key string) string {
		if value, ok := v[key]; ok && value != nil {
			return strings.TrimSpace(fmt.Sprint(value))
		}
		return ""
	}
	port, err := strconv.Atoi(str("port"))
	if err != nil {
		return nil, fmt.Errorf("vmess 端口无效")
	}
	proxy := map[string]any{
		"name": str("ps"), "type": "vmess", "server": str("add"), "port": port, "uuid": str("id"),
		"alterId": asInt(v["aid"]), "cipher": firstNonEmpty(str("scy"), "auto"), "udp": true,
	}
	if str("tls") == "tls" {
		proxy["tls"] = true
		if sni := firstNonEmpty(str("sni"), str("host")); sni != "" {
			proxy["servername"] = sni
		}
		if fp := str("fp"); fp != "" {
			proxy["client-fingerprint"] = fp
		}
		if alpn := str("alpn"); alpn != "" {
			proxy["alpn"] = splitList(alpn)
		}
	}
	applyTransport(proxy, firstNonEmpty(str("net"), "tcp"), str("path"), str("host"), str("path"), str("type"))
	return proxy, nil
}

// parseStdURI 处理 scheme://userinfo@host:port?query#name 形态的链接。
func parseStdURI(kind, raw string) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%s 链接无效: %v", kind, err)
	}
	host := u.Hostname()
	port := 0
	if u.Port() != "" {
		port, _ = strconv.Atoi(u.Port())
	}
	if host == "" {
		return nil, fmt.Errorf("%s 链接缺少服务器地址", kind)
	}
	q := u.Query()
	name := u.Fragment
	user := u.User.Username()
	pass, _ := u.User.Password()
	proxy := map[string]any{"name": name, "type": kind, "server": host}
	if port > 0 {
		proxy["port"] = port
	}
	insecure := q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" || q.Get("skip-cert-verify") == "true"
	if insecure {
		proxy["skip-cert-verify"] = true
	}
	if alpn := q.Get("alpn"); alpn != "" {
		proxy["alpn"] = splitList(alpn)
	}
	if fp := q.Get("fp"); fp != "" {
		proxy["client-fingerprint"] = fp
	}

	switch kind {
	case "vless":
		proxy["uuid"] = user
		proxy["udp"] = true
		if flow := q.Get("flow"); flow != "" {
			proxy["flow"] = flow
		}
		if enc := q.Get("encryption"); enc != "" && enc != "none" {
			proxy["encryption"] = enc
		}
		switch q.Get("security") {
		case "tls":
			proxy["tls"] = true
		case "reality":
			proxy["tls"] = true
			opts := map[string]any{"public-key": q.Get("pbk")}
			if sid := q.Get("sid"); sid != "" {
				opts["short-id"] = sid
			}
			proxy["reality-opts"] = opts
		}
		if sni := q.Get("sni"); sni != "" {
			proxy["servername"] = sni
		}
		applyTransport(proxy, firstNonEmpty(q.Get("type"), "tcp"), q.Get("path"), q.Get("host"), q.Get("serviceName"), q.Get("headerType"))
		if q.Get("type") == "xhttp" {
			opts := map[string]any{}
			if p := q.Get("path"); p != "" {
				opts["path"] = p
			}
			if h := q.Get("host"); h != "" {
				opts["host"] = h
			}
			if m := q.Get("mode"); m != "" {
				opts["mode"] = m
			}
			proxy["network"] = "xhttp"
			proxy["xhttp-opts"] = opts
		}
	case "trojan":
		proxy["password"] = firstNonEmpty(pass, user)
		proxy["udp"] = true
		if sni := firstNonEmpty(q.Get("sni"), q.Get("peer")); sni != "" {
			proxy["sni"] = sni
		}
		applyTransport(proxy, firstNonEmpty(q.Get("type"), "tcp"), q.Get("path"), q.Get("host"), q.Get("serviceName"), "")
	case "hysteria2":
		// hysteria2://<auth>@…，auth 可以是「用户:密码」形式，整段就是口令。
		proxy["password"] = firstNonEmpty(strings.Trim(user+":"+pass, ":"), q.Get("auth"))
		if sni := q.Get("sni"); sni != "" {
			proxy["sni"] = sni
		}
		if obfs := q.Get("obfs"); obfs != "" && obfs != "none" {
			proxy["obfs"] = obfs
			proxy["obfs-password"] = q.Get("obfs-password")
		}
		if ports := firstNonEmpty(q.Get("mport"), q.Get("ports")); ports != "" {
			proxy["ports"] = ports
		}
		if fp := q.Get("pinSHA256"); fp != "" {
			proxy["fingerprint"] = fp
		}
	case "hysteria":
		if auth := firstNonEmpty(q.Get("auth"), user); auth != "" {
			proxy["auth-str"] = auth
		}
		proxy["up"] = firstNonEmpty(q.Get("upmbps"), "30") + " Mbps"
		proxy["down"] = firstNonEmpty(q.Get("downmbps"), "100") + " Mbps"
		if p := q.Get("protocol"); p != "" {
			proxy["protocol"] = p
		}
		if sni := firstNonEmpty(q.Get("peer"), q.Get("sni")); sni != "" {
			proxy["sni"] = sni
		}
		if obfs := q.Get("obfsParam"); obfs != "" {
			proxy["obfs"] = obfs
		}
	case "tuic":
		proxy["uuid"] = user
		proxy["password"] = pass
		if sni := q.Get("sni"); sni != "" {
			proxy["sni"] = sni
		}
		if cc := q.Get("congestion_control"); cc != "" {
			proxy["congestion-controller"] = cc
		}
		if mode := q.Get("udp_relay_mode"); mode != "" {
			proxy["udp-relay-mode"] = mode
		}
		if q.Get("disable_sni") == "1" {
			proxy["disable-sni"] = true
		}
	case "anytls":
		proxy["password"] = firstNonEmpty(pass, user)
		proxy["udp"] = true
		if sni := q.Get("sni"); sni != "" {
			proxy["sni"] = sni
		}
	case "socks5", "http":
		if user != "" {
			proxy["username"] = user
		}
		if pass != "" {
			proxy["password"] = pass
		}
		if u.Scheme == "https" {
			proxy["tls"] = true
		}
		if kind == "socks5" {
			proxy["udp"] = true
		}
		if port == 0 {
			if kind == "http" {
				proxy["port"] = 8080
			} else {
				proxy["port"] = 1080
			}
		}
	}
	if _, ok := proxy["port"]; !ok {
		proxy["port"] = 443
	}
	return proxy, nil
}

// applyTransport 把 v2ray 风格的 net / path / host 落到 mihomo 的 *-opts。
func applyTransport(proxy map[string]any, network, path, host, service, headerType string) {
	switch network {
	case "ws", "httpupgrade":
		opts := map[string]any{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		if network == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		proxy["network"] = "ws"
		proxy["ws-opts"] = opts
	case "grpc":
		proxy["network"] = "grpc"
		proxy["grpc-opts"] = map[string]any{"grpc-service-name": firstNonEmpty(service, path)}
	case "h2":
		opts := map[string]any{}
		if path != "" {
			opts["path"] = path
		}
		if host != "" {
			opts["host"] = splitList(host)
		}
		proxy["network"] = "h2"
		proxy["h2-opts"] = opts
	case "tcp", "":
		if headerType == "http" {
			opts := map[string]any{"method": "GET"}
			if path != "" {
				opts["path"] = splitList(path)
			}
			if host != "" {
				opts["headers"] = map[string]any{"Host": splitList(host)}
			}
			proxy["network"] = "http"
			proxy["http-opts"] = opts
		}
	}
}

func splitHostPort(hostport string) (string, int, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(hostport))
	if err != nil {
		return "", 0, fmt.Errorf("服务器地址 %q 无效", hostport)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("端口 %q 无效", portText)
	}
	return host, port, nil
}

func decodeBase64(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	if s == "" {
		return "", false
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := enc.DecodeString(s); err == nil {
			return string(decoded), true
		}
	}
	return "", false
}

func splitList(s string) []any {
	out := []any{}
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func asList(v any) []any {
	switch items := v.(type) {
	case []any:
		return items
	case []map[string]any:
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out
	default:
		return nil
	}
}

func asMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, value := range m {
			out[fmt.Sprint(k)] = value
		}
		return out
	default:
		return nil
	}
}

func asMapOfMaps(v any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for key, value := range asMap(v) {
		if m := asMap(value); m != nil {
			out[key] = m
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true")
	default:
		return false
	}
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

// CleanValue 把经过 JSON 往返的数字还原成整数，避免 3600 写成 3600.0 被核心按字符串解析。
func CleanValue(v any) any {
	switch value := v.(type) {
	case float64:
		if value == math.Trunc(value) && math.Abs(value) < 1<<53 {
			return int64(value)
		}
		return value
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return i
		}
		if f, err := value.Float64(); err == nil {
			return f
		}
		return value.String()
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, item := range value {
			out[k] = CleanValue(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = CleanValue(item)
		}
		return out
	default:
		return value
	}
}
