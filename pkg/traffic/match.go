package traffic

import (
	"hash/maphash"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strings"

	"go4.org/netipx"
)

// ── 路径模式 ──

// pathPattern 编译后的路径模式。三种写法：
//
//	/api/auth/login      精确
//	/api/auth/*          前缀（含 /api/auth 本身）
//	/api/apps/*/users    单段通配（* 匹配恰好一段），可与末尾 /* 组合
type pathPattern struct {
	raw    string
	exact  string
	prefix string   // 末尾 /* 时的前缀（不含 /*）
	segs   []string // 含中间 * 时按段匹配；"*" 表示任意一段
	tail   bool     // segs 模式下末尾是否 /*
}

func compilePath(p string) pathPattern {
	pp := pathPattern{raw: p}
	if p == "/*" || p == "*" {
		pp.prefix = ""
		pp.tail = true
		return pp
	}
	tail := strings.HasSuffix(p, "/*")
	body := p
	if tail {
		body = strings.TrimSuffix(p, "/*")
	}
	if !strings.Contains(body, "*") {
		if tail {
			pp.prefix = body
			pp.tail = true
		} else {
			pp.exact = body
		}
		return pp
	}
	pp.segs = strings.Split(strings.TrimPrefix(body, "/"), "/")
	pp.tail = tail
	return pp
}

func (pp *pathPattern) match(path string) bool {
	if pp.segs == nil {
		if pp.tail {
			if pp.prefix == "" {
				return true
			}
			if !strings.HasPrefix(path, pp.prefix) {
				return false
			}
			rest := path[len(pp.prefix):]
			return rest == "" || rest[0] == '/'
		}
		return path == pp.exact
	}
	// 逐段扫描，不切分字符串。
	i := 0
	if len(path) > 0 && path[0] == '/' {
		i = 1
	}
	for _, seg := range pp.segs {
		if i > len(path) {
			return false
		}
		j := strings.IndexByte(path[i:], '/')
		var cur string
		if j < 0 {
			cur = path[i:]
			i = len(path) + 1
		} else {
			cur = path[i : i+j]
			i += j + 1
		}
		if seg != "*" && seg != cur {
			return false
		}
		if seg == "*" && cur == "" {
			return false
		}
	}
	if i > len(path) {
		return true
	}
	return pp.tail
}

type pathSet []pathPattern

func compilePaths(in []string) pathSet {
	if len(in) == 0 {
		return nil
	}
	out := make(pathSet, 0, len(in))
	for _, p := range in {
		out = append(out, compilePath(p))
	}
	return out
}

func (ps pathSet) match(path string) bool {
	for i := range ps {
		if ps[i].match(path) {
			return true
		}
	}
	return false
}

// ── 方法位图 ──

func methodBit(m string) uint8 {
	switch m {
	case http.MethodGet:
		return 1 << 0
	case http.MethodPost:
		return 1 << 1
	case http.MethodPut:
		return 1 << 2
	case http.MethodPatch:
		return 1 << 3
	case http.MethodDelete:
		return 1 << 4
	case http.MethodOptions:
		return 1 << 5
	case http.MethodHead:
		return 1 << 6
	}
	return 1 << 7
}

// ── 主机名 ──

type hostPattern struct {
	exact  string
	suffix string // *.example.com → .example.com
}

func compileHosts(in []string) []hostPattern {
	out := make([]hostPattern, 0, len(in))
	for _, h := range in {
		if strings.HasPrefix(h, "*.") {
			out = append(out, hostPattern{suffix: h[1:]})
		} else {
			out = append(out, hostPattern{exact: h})
		}
	}
	return out
}

func hostOnly(h string) string {
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return h
}

func matchHost(ps []hostPattern, host string) bool {
	host = hostOnly(host)
	for _, p := range ps {
		if p.exact != "" {
			if strings.EqualFold(host, p.exact) {
				return true
			}
			continue
		}
		if len(host) > len(p.suffix) && strings.EqualFold(host[len(host)-len(p.suffix):], p.suffix) {
			return true
		}
	}
	return false
}

// ── 请求头条件 ──

type headerCond struct {
	name  string // 规范化后的名字
	op    string
	value string
	lower string
}

func compileHeaders(in []HeaderMatch) []headerCond {
	out := make([]headerCond, 0, len(in))
	for _, h := range in {
		out = append(out, headerCond{
			name:  textproto.CanonicalMIMEHeaderKey(h.Name),
			op:    h.Op,
			value: h.Value,
			lower: strings.ToLower(h.Value),
		})
	}
	return out
}

func headerValue(h http.Header, canonical string) string {
	if v := h[canonical]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (hc *headerCond) match(h http.Header) bool {
	vals, ok := h[hc.name]
	switch hc.op {
	case "exists":
		return ok && len(vals) > 0
	case "absent":
		return !ok || len(vals) == 0
	}
	if !ok || len(vals) == 0 {
		return false
	}
	v := vals[0]
	switch hc.op {
	case "equals":
		return v == hc.value
	case "prefix":
		return strings.HasPrefix(v, hc.value)
	case "contains":
		return strings.Contains(strings.ToLower(v), hc.lower)
	}
	return false
}

// ── 网段 ──

func compileCIDRs(in []string) *netipx.IPSet {
	if len(in) == 0 {
		return nil
	}
	var b netipx.IPSetBuilder
	for _, c := range in {
		if p, err := parsePrefix(c); err == nil {
			b.AddPrefix(p)
		}
	}
	set, err := b.IPSet()
	if err != nil {
		return nil
	}
	return set
}

// ── 查询参数（不解析整个 RawQuery，避免分配） ──

func queryValue(raw, name string) string {
	for raw != "" {
		var part string
		if i := strings.IndexByte(raw, '&'); i >= 0 {
			part, raw = raw[:i], raw[i+1:]
		} else {
			part, raw = raw, ""
		}
		k, v, _ := strings.Cut(part, "=")
		if k != name {
			continue
		}
		if strings.ContainsAny(v, "%+") {
			if dec, err := url.QueryUnescape(v); err == nil {
				return dec
			}
		}
		return v
	}
	return ""
}

const appKeyHeader = "X-Aegis-App-Key"

func requestAppKey(r *http.Request) string {
	if v := headerValue(r.Header, appKeyHeader); v != "" {
		return strings.TrimSpace(v)
	}
	if v := queryValue(r.URL.RawQuery, "appKey"); v != "" {
		return v
	}
	return queryValue(r.URL.RawQuery, "appkey")
}

// ── 键提取 ──

// keyOf 计算一项限速在这个请求上的键哈希。ok=false 表示取不到键值且回退为 skip。
func (l *limitRT) keyOf(seed maphash.Seed, r *http.Request, ip netip.Addr) (uint64, bool) {
	var h maphash.Hash
	h.SetSeed(seed)
	switch l.spec.Key {
	case KeyIP:
		writeAddr(&h, ip)
		return h.Sum64(), true
	case KeySubnet:
		writeAddr(&h, subnetOf(ip).Addr())
		return h.Sum64(), true
	case KeyApp:
		if v := requestAppKey(r); v != "" {
			h.WriteString(v)
			return h.Sum64(), true
		}
	case KeyToken:
		if v := headerValue(r.Header, "Authorization"); v != "" {
			h.WriteString(v)
			return h.Sum64(), true
		}
	case KeyHeader:
		if v := headerValue(r.Header, l.headerName); v != "" {
			h.WriteString(v)
			return h.Sum64(), true
		}
	case KeyQuery:
		if v := queryValue(r.URL.RawQuery, l.spec.Name); v != "" {
			h.WriteString(v)
			return h.Sum64(), true
		}
	}
	if l.spec.Fallback == "skip" {
		return 0, false
	}
	h.Reset()
	h.WriteByte(0xFF) // 与正常键值分开
	writeAddr(&h, ip)
	return h.Sum64(), true
}

// keyDisplay 键的展示形式（只在抽样与模拟时调用）。凭据只露前几位。
func (l *limitRT) keyDisplay(r *http.Request, ip netip.Addr) string {
	switch l.spec.Key {
	case KeyGlobal:
		return "全局"
	case KeyIP:
		return ip.String()
	case KeySubnet:
		return subnetOf(ip).String()
	case KeyApp:
		if v := requestAppKey(r); v != "" {
			return truncate(v, 64)
		}
	case KeyToken:
		if v := headerValue(r.Header, "Authorization"); v != "" {
			return maskSecret(v)
		}
	case KeyHeader:
		if v := headerValue(r.Header, l.headerName); v != "" {
			return truncate(v, 64)
		}
	case KeyQuery:
		if v := queryValue(r.URL.RawQuery, l.spec.Name); v != "" {
			return truncate(v, 64)
		}
	}
	if l.spec.Fallback == "skip" {
		return ""
	}
	return ip.String()
}

func writeAddr(h *maphash.Hash, ip netip.Addr) {
	b := ip.As16()
	h.Write(b[:])
}

func subnetOf(ip netip.Addr) netip.Prefix {
	ip = ip.Unmap()
	bits := 64
	if ip.Is4() {
		bits = 24
	}
	if !ip.IsValid() {
		return netip.Prefix{}
	}
	p, err := ip.Prefix(bits)
	if err != nil {
		return netip.PrefixFrom(ip, ip.BitLen())
	}
	return p
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func maskSecret(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		v = strings.TrimSpace(v[7:])
	}
	if len(v) <= 8 {
		return "****"
	}
	return v[:6] + "…" + v[len(v)-2:]
}

// lowerPath 路径小写化；已经是小写时不分配。
func lowerPath(p string) string {
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c >= 'A' && c <= 'Z' {
			return strings.ToLower(p)
		}
	}
	return p
}
