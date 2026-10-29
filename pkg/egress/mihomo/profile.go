package mihomo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Profile 交给核心的配置本体。节点、订阅、策略组保持 mihomo 原生的键值形态（map），
// 这样核心新增的协议与字段不需要本包跟着改结构体就能用；常用的全局设置拆成强类型字段。
type Profile struct {
	Settings       Settings                  `json:"settings"`
	DNS            map[string]any            `json:"dns,omitempty"`
	Proxies        []map[string]any          `json:"proxies,omitempty"`
	ProxyProviders map[string]map[string]any `json:"proxyProviders,omitempty"`
	ProxyGroups    []map[string]any          `json:"proxyGroups,omitempty"`
	Rules          []string                  `json:"rules,omitempty"`
	RuleProviders  map[string]map[string]any `json:"ruleProviders,omitempty"`
	// Extra 其余顶层键原样写进配置（hosts、sniffer、ntp、experimental……）。
	// 入站、控制器与 TUN 这些由本包托管的键会被剔除，见 reservedKeys。
	Extra map[string]any `json:"extra,omitempty"`
}

// Settings 核心的全局设置。
type Settings struct {
	Mode                    string            `json:"mode,omitempty"`     // rule / global / direct
	LogLevel                string            `json:"logLevel,omitempty"` // silent / error / warning / info / debug
	IPv6                    bool              `json:"ipv6"`
	UnifiedDelay            bool              `json:"unifiedDelay"`
	TCPConcurrent           bool              `json:"tcpConcurrent"`
	GlobalClientFingerprint string            `json:"globalClientFingerprint,omitempty"`
	KeepAliveIdle           int               `json:"keepAliveIdle,omitempty"`
	KeepAliveInterval       int               `json:"keepAliveInterval,omitempty"`
	GeodataMode             bool              `json:"geodataMode"`
	GeodataLoader           string            `json:"geodataLoader,omitempty"`
	GeoAutoUpdate           bool              `json:"geoAutoUpdate"`
	GeoUpdateInterval       int               `json:"geoUpdateInterval,omitempty"` // 小时
	GeoxURL                 map[string]string `json:"geoxUrl,omitempty"`           // geoip / geosite / mmdb / asn
	// GlobalUA 订阅与规则集下载时的 User-Agent（部分机场按它下发 Clash 格式）。
	GlobalUA string `json:"globalUa,omitempty"`
}

// 内置的出站目标：策略组成员、分流规则目标都可以直接写它们。
var Builtins = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"}

// IsBuiltin 是否为 mihomo 内置出站。
func IsBuiltin(name string) bool { return slices.Contains(Builtins, name) }

// reservedKeys 由本包托管、不允许从 Extra 覆盖的顶层键。
// 入站与控制器一旦被改，网关就再也连不上核心；TUN 会接管整台机器的路由。
var reservedKeys = map[string]bool{
	"port": true, "socks-port": true, "mixed-port": true, "redir-port": true, "tproxy-port": true,
	"allow-lan": true, "bind-address": true, "lan-allowed-ips": true, "lan-disallowed-ips": true,
	"authentication": true, "skip-auth-prefixes": true, "listeners": true, "tun": true, "tunnels": true,
	"external-controller": true, "external-controller-tls": true, "external-controller-unix": true,
	"external-controller-pipe": true, "external-controller-cors": true, "external-doh-server": true,
	"external-ui": true, "external-ui-url": true, "external-ui-name": true, "secret": true,
	"proxies": true, "proxy-providers": true, "proxy-groups": true, "rules": true, "rule-providers": true,
	"sub-rules": true, "dns": true, "mode": true, "log-level": true, "ipv6": true, "profile": true,
	"interface-name": true, "routing-mark": true,
}

// ReservedKey 判断顶层键是否由网关托管（控制台据此提示「该键会被忽略」）。
func ReservedKey(key string) bool { return reservedKeys[strings.ToLower(strings.TrimSpace(key))] }

// System 由核心托管方注入的部分：控制器与入站。
//
// 每个被网关引用的策略组 / 节点各有一个专用 SOCKS5 入站（listeners[].proxy 指向它），
// TCP 与 UDP 都直达目标、不经分流规则；另有一个不指定目标的入站，按 Profile.Rules 分流。
// 不用 IN-USER 按用户名分发，是因为核心的 SOCKS UDP 中继不携带入站用户，UDP 会落到分流规则上。
type System struct {
	ControllerAddr string
	Secret         string
	// InboundPort 「交给核心分流」的入站端口。
	InboundPort int
	Password    string
	// TargetPorts 目标名 → 专用入站端口。
	TargetPorts map[string]int
}

// PlaceholderPorts 预览与导出用的示意端口（运行时由核心托管器分配真实端口）。
func PlaceholderPorts(targets []string) map[string]int {
	ports := map[string]int{}
	for index, target := range uniqueStrings(targets) {
		ports[target] = 20000 + index
	}
	return ports
}

// InboundUser 所有入站共用的用户名（口令随进程随机）。
const InboundUser = "aegis"

// listenerName 目标的入站名：目标名可能含空格、中文与表情，用摘要。
func listenerName(target string) string {
	sum := sha256.Sum256([]byte(target))
	return "aegis-" + hex.EncodeToString(sum[:6])
}

// Clone 深拷贝。
func (p Profile) Clone() Profile {
	out := p
	out.Settings.GeoxURL = maps.Clone(p.Settings.GeoxURL)
	out.DNS = deepCopyMap(p.DNS)
	out.Proxies = make([]map[string]any, len(p.Proxies))
	for i, item := range p.Proxies {
		out.Proxies[i] = deepCopyMap(item)
	}
	out.ProxyGroups = make([]map[string]any, len(p.ProxyGroups))
	for i, item := range p.ProxyGroups {
		out.ProxyGroups[i] = deepCopyMap(item)
	}
	out.ProxyProviders = map[string]map[string]any{}
	for k, v := range p.ProxyProviders {
		out.ProxyProviders[k] = deepCopyMap(v)
	}
	out.RuleProviders = map[string]map[string]any{}
	for k, v := range p.RuleProviders {
		out.RuleProviders[k] = deepCopyMap(v)
	}
	out.Rules = append([]string(nil), p.Rules...)
	out.Extra = deepCopyMap(p.Extra)
	return out
}

// cleaned 把所有键值里经 JSON 往返变成浮点的整数还原（见 CleanValue）。
func (p Profile) cleaned() Profile {
	out := p.Clone()
	cleanMap := func(m map[string]any) map[string]any {
		if m == nil {
			return nil
		}
		return CleanValue(m).(map[string]any)
	}
	out.DNS = cleanMap(out.DNS)
	out.Extra = cleanMap(out.Extra)
	for i := range out.Proxies {
		out.Proxies[i] = cleanMap(out.Proxies[i])
	}
	for i := range out.ProxyGroups {
		out.ProxyGroups[i] = cleanMap(out.ProxyGroups[i])
	}
	for k := range out.ProxyProviders {
		out.ProxyProviders[k] = cleanMap(out.ProxyProviders[k])
	}
	for k := range out.RuleProviders {
		out.RuleProviders[k] = cleanMap(out.RuleProviders[k])
	}
	return out
}

// Empty 没有任何节点、订阅与策略组时不需要拉起核心。
func (p Profile) Empty() bool {
	return len(p.Proxies) == 0 && len(p.ProxyProviders) == 0 && len(p.ProxyGroups) == 0
}

// Names 节点与策略组的全部名字（不含订阅里的节点，它们要等核心拉取后才知道）。
func (p Profile) Names() []string {
	out := make([]string, 0, len(p.Proxies)+len(p.ProxyGroups))
	for _, item := range p.Proxies {
		out = append(out, stringField(item, "name"))
	}
	for _, item := range p.ProxyGroups {
		out = append(out, stringField(item, "name"))
	}
	return out
}

// Validate 只做核心不会替我们做、或报错难以归因的检查；字段级校验交给核心的 -t。
func (p Profile) Validate() error {
	seen := map[string]string{}
	claim := func(name, kind string) error {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s缺少名称", kind)
		}
		if strings.Contains(name, ",") {
			return fmt.Errorf("%s「%s」名称里不能有逗号（分流规则按逗号分段）", kind, name)
		}
		if IsBuiltin(name) {
			return fmt.Errorf("%s「%s」与内置出站重名", kind, name)
		}
		if previous, dup := seen[name]; dup {
			return fmt.Errorf("%s「%s」与%s重名", kind, name, previous)
		}
		seen[name] = kind
		return nil
	}
	for _, item := range p.Proxies {
		if err := claim(stringField(item, "name"), "节点"); err != nil {
			return err
		}
		if stringField(item, "type") == "" {
			return fmt.Errorf("节点「%s」缺少类型", stringField(item, "name"))
		}
	}
	for _, item := range p.ProxyGroups {
		if err := claim(stringField(item, "name"), "策略组"); err != nil {
			return err
		}
	}
	providers := map[string]bool{}
	for name := range p.ProxyProviders {
		if strings.TrimSpace(name) == "" || strings.Contains(name, ",") {
			return fmt.Errorf("订阅名称「%s」无效", name)
		}
		providers[name] = true
	}
	for _, group := range p.ProxyGroups {
		name := stringField(group, "name")
		for _, member := range stringList(group["proxies"]) {
			if _, ok := seen[member]; !ok && !IsBuiltin(member) {
				return fmt.Errorf("策略组「%s」引用了不存在的节点或策略组「%s」", name, member)
			}
		}
		for _, use := range stringList(group["use"]) {
			if !providers[use] {
				return fmt.Errorf("策略组「%s」引用了不存在的订阅「%s」", name, use)
			}
		}
	}
	return nil
}

// Render 生成 config.yaml。顶层键按固定顺序输出，节点与策略组里 name / type 排在最前，
// 让落盘的配置便于人读与比对。
func Render(p Profile, sys System) ([]byte, error) {
	p = p.cleaned()
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if sys.ControllerAddr == "" || sys.InboundPort <= 0 || sys.Password == "" {
		return nil, fmt.Errorf("核心入站与控制器参数不完整")
	}
	s := p.Settings
	top := yaml.MapSlice{}
	put := func(key string, value any) { top = append(top, yaml.MapItem{Key: key, Value: value}) }

	put("mode", firstNonEmpty(s.Mode, "rule"))
	put("log-level", firstNonEmpty(s.LogLevel, "info"))
	put("ipv6", s.IPv6)
	put("allow-lan", false)
	put("unified-delay", s.UnifiedDelay)
	put("tcp-concurrent", s.TCPConcurrent)
	put("find-process-mode", "off")
	if s.GlobalClientFingerprint != "" {
		put("global-client-fingerprint", s.GlobalClientFingerprint)
	}
	if s.KeepAliveIdle > 0 {
		put("keep-alive-idle", s.KeepAliveIdle)
	}
	if s.KeepAliveInterval > 0 {
		put("keep-alive-interval", s.KeepAliveInterval)
	}
	if s.GlobalUA != "" {
		put("global-ua", s.GlobalUA)
	}
	put("geodata-mode", s.GeodataMode)
	if s.GeodataLoader != "" {
		put("geodata-loader", s.GeodataLoader)
	}
	put("geo-auto-update", s.GeoAutoUpdate)
	if s.GeoUpdateInterval > 0 {
		put("geo-update-interval", s.GeoUpdateInterval)
	}
	if len(s.GeoxURL) > 0 {
		put("geox-url", sortedMap(s.GeoxURL))
	}
	put("external-controller", sys.ControllerAddr)
	put("secret", sys.Secret)
	put("profile", yaml.MapSlice{{Key: "store-selected", Value: true}, {Key: "store-fake-ip", Value: false}})

	users := []yaml.MapSlice{{{Key: "username", Value: InboundUser}, {Key: "password", Value: sys.Password}}}
	listener := func(name string, port int, target string) yaml.MapSlice {
		item := yaml.MapSlice{
			{Key: "name", Value: name},
			{Key: "type", Value: "socks"},
			{Key: "listen", Value: "127.0.0.1"},
			{Key: "port", Value: port},
			{Key: "udp", Value: true},
			{Key: "users", Value: users},
		}
		if target != "" {
			item = append(item, yaml.MapItem{Key: "proxy", Value: target})
		}
		return item
	}
	listeners := []yaml.MapSlice{listener("aegis-rules", sys.InboundPort, "")}
	targets := make([]string, 0, len(sys.TargetPorts))
	for target := range sys.TargetPorts {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	known := map[string]bool{}
	for _, name := range p.Names() {
		known[name] = true
	}
	for _, target := range targets {
		if !known[target] && !IsBuiltin(target) {
			return nil, fmt.Errorf("网关引用的「%s」不在核心配置里", target)
		}
		listeners = append(listeners, listener(listenerName(target), sys.TargetPorts[target], target))
	}
	put("listeners", listeners)

	extraKeys := make([]string, 0, len(p.Extra))
	for key := range p.Extra {
		if !ReservedKey(key) {
			extraKeys = append(extraKeys, key)
		}
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		put(key, p.Extra[key])
	}

	if len(p.DNS) > 0 {
		put("dns", p.DNS)
	}
	put("proxies", orderedItems(p.Proxies))
	if len(p.ProxyProviders) > 0 {
		put("proxy-providers", p.ProxyProviders)
	}
	put("proxy-groups", orderedItems(p.ProxyGroups))
	if len(p.RuleProviders) > 0 {
		put("rule-providers", p.RuleProviders)
	}

	rules := make([]string, 0, len(p.Rules)+1)
	hasMatch := false
	for _, rule := range p.Rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "#") {
			continue
		}
		rules = append(rules, rule)
		if strings.HasPrefix(strings.ToUpper(rule), "MATCH,") {
			hasMatch = true
		}
	}
	if !hasMatch {
		// 没写兜底时补一条直连兜底，与「交给核心分流但没有规则命中」的直觉一致。
		rules = append(rules, "MATCH,DIRECT")
	}
	put("rules", rules)

	out, err := yaml.MarshalWithOptions(top, yaml.Indent(2), yaml.IndentSequence(true))
	if err != nil {
		return nil, fmt.Errorf("生成核心配置失败: %w", err)
	}
	return out, nil
}

// orderedItems 让 name、type、server、port 排在最前，其余按键名排序。
func orderedItems(items []map[string]any) []yaml.MapSlice {
	out := make([]yaml.MapSlice, 0, len(items))
	front := []string{"name", "type", "server", "port"}
	for _, item := range items {
		slice := yaml.MapSlice{}
		for _, key := range front {
			if value, ok := item[key]; ok {
				slice = append(slice, yaml.MapItem{Key: key, Value: value})
			}
		}
		rest := make([]string, 0, len(item))
		for key := range item {
			if !slices.Contains(front, key) {
				rest = append(rest, key)
			}
		}
		sort.Strings(rest)
		for _, key := range rest {
			slice = append(slice, yaml.MapItem{Key: key, Value: item[key]})
		}
		out = append(out, slice)
	}
	return out
}

func sortedMap(in map[string]string) yaml.MapSlice {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := yaml.MapSlice{}
	for _, key := range keys {
		if strings.TrimSpace(in[key]) != "" {
			out = append(out, yaml.MapItem{Key: key, Value: in[key]})
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

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func stringList(v any) []string {
	switch items := v.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func deepCopyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = deepCopyValue(value)
	}
	return out
}

func deepCopyValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		return deepCopyMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = deepCopyValue(item)
		}
		return out
	case []string:
		return append([]string(nil), value...)
	case []map[string]any:
		out := make([]map[string]any, len(value))
		for i, item := range value {
			out[i] = deepCopyMap(item)
		}
		return out
	default:
		return value
	}
}
