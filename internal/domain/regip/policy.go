// Package regip 注册 IP 控制：同一网络能注册多少账号、注册多快，超出怎么处置。
//
// 规则见 docs/register-ip.md。这里只放纯数据与纯函数（策略的规整、网络主体的推导、
// 名单编译、突增检测、惩罚时长与最终判定），计数、名单读写与记录在 service 层的 RegisterIPService。
//
// 判定分两段：先用不需要计数的事实（名单、惩罚、属地、运营商、突增）做前置判定，
// 再把配额规则交给 Redis 一个脚本原子地「数一遍 → 都没超才占位」。两段共用 Decide，
// 线上、模拟与 IP 查询说的是同一套话。
package regip

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// SettingsKey 应用设置（apps.settings）里存放策略的键。
const SettingsKey = "registerIp"

// LegacySettingsKey 旧开关「同一 IP 不允许重复注册」。没保存过新策略时按它推导。
const LegacySettingsKey = "registerCheckIp"

// 运行模式。
const (
	// ModeEnforce 按规则拦截
	ModeEnforce = "enforce"
	// ModeObserve 只记录「本该拦截」，一律放行。新规则上线前先观察几天，看会误伤多少人
	ModeObserve = "observe"
)

// 规则的处置。
const (
	ActionReject    = "reject"    // 拒绝注册
	ActionChallenge = "challenge" // 要求先完成图形验证码（40015），通过后放行
	ActionObserve   = "observe"   // 只记录，不拦截
)

// 配额统计的主体。
const (
	// ScopeIP 单个地址。IPv6 按 Network.IPv6Host 前缀聚合：运营商给每户分一整段 /64，
	// 隐私扩展地址一天能换好几次，按 /128 数等于没数
	ScopeIP = "ip"
	// ScopeSubnet 网段：IPv4 按 Network.IPv4Subnet、IPv6 按 Network.IPv6Subnet
	ScopeSubnet = "subnet"
	// ScopeDevice 同一设备（客户端上报的设备标识）。与地址互补：换网络、开代理挡不住设备，
	// 一台设备上开多开、改机挡不住地址
	ScopeDevice = "device"
	// ScopeIPDevices 同一地址在窗口内出现的不同设备数：一个出口后面突然冒出几十台「新手机」是群控 / 模拟器农场
	ScopeIPDevices = "ip_devices"
	// ScopeDeviceIPs 同一设备在窗口内用过的不同地址数（按「单个地址」口径聚合）：一台设备频繁换出口是代理池轮换
	ScopeDeviceIPs = "device_ips"
	// ScopeCity 同一属地（国家 / 省 / 市）。属地查不到时这条规则不参与判定
	ScopeCity = "city"
	// ScopeApp 整个应用。与突增检测互补：一个是写死的硬上限，一个跟着平时的量走
	ScopeApp = "app"
)

// Scopes 全部统计主体，顺序即展示顺序。
var Scopes = []string{ScopeIP, ScopeSubnet, ScopeDevice, ScopeIPDevices, ScopeDeviceIPs, ScopeCity, ScopeApp}

// DistinctScope 这个主体数的是「不同成员数」而不是注册数（地址下的不同设备、设备下的不同地址）。
func DistinctScope(scope string) bool { return scope == ScopeIPDevices || scope == ScopeDeviceIPs }

// DeviceScope 这个主体的统计对象是设备（缺少有效设备标识时不参与）。
func DeviceScope(scope string) bool { return scope == ScopeDevice || scope == ScopeDeviceIPs }

// IPScope 这个主体的统计对象是地址（缺少注册 IP 时不参与）。
func IPScope(scope string) bool {
	return scope == ScopeIP || scope == ScopeSubnet || scope == ScopeIPDevices
}

// 注册方式。规则的 Methods 为空表示全部方式都算。
const (
	MethodPassword = "password"
	MethodSMS      = "sms"
	MethodOAuth    = "oauth"
	MethodCardKey  = "cardkey"
)

// Methods 全部注册方式。
var Methods = []string{MethodPassword, MethodSMS, MethodOAuth, MethodCardKey}

// 缺少注册 IP（内网调用、接入层没透传）时的处置。
const (
	MissingIPAllow  = "allow"
	MissingIPReject = "reject"
)

// 计数存储（Redis / 数据库）出错时的处置。
const (
	// FailOpen 放行并记 warn（默认）：注册 IP 控制是治理能力，不能因为它的一次抖动让全体用户注册不了
	FailOpen = "open"
	// FailClosed 拒绝注册：宁可这段时间注册不了，也不能让批量注册趁机灌进来
	FailClosed = "closed"
)

// 属地限制方式。
const (
	GeoOff   = "off"
	GeoAllow = "allow" // 只允许名单内的属地
	GeoDeny  = "deny"  // 拒绝名单内的属地
)

// 取值范围。
const (
	MaxRules          = 20
	MaxLimit          = 1_000_000
	MaxWindowSeconds  = 365 * 24 * 3600
	MinWindowSeconds  = 60
	MaxMessageLength  = 200
	MaxKeywords       = 50
	MaxGeoEntries     = 100
	MaxPenaltySeconds = 30 * 24 * 3600
)

// Policy 应用的注册 IP 策略，存在 apps.settings.registerIp。
type Policy struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	// Network 地址聚合口径
	Network Network `json:"network"`
	// Rules 配额规则：每条在自己的统计主体与窗口内数注册数，各自独立判定
	Rules []Rule `json:"rules"`
	// Penalty 屡次触发上限的地址关进惩罚期，时长按次数指数增长
	Penalty Penalty `json:"penalty"`
	// Surge 全应用注册量突增检测
	Surge Surge `json:"surge"`
	// ISP 按运营商名称关键词处置（机房、云厂商出口）
	ISP ISPRule `json:"isp"`
	// Geo 按属地放行或拒绝
	Geo GeoRule `json:"geo"`
	// Device 设备标识的质量要求：缺少或明显无效时怎么办
	Device DeviceRule `json:"device"`
	// MissingIP 拿不到注册 IP 时的处置
	MissingIP string `json:"missingIp"`
	// FailMode 计数存储出错时的处置
	FailMode string `json:"failMode"`
	// RejectMessage 配额拒绝时的提示，留空用默认文案。可用 {limit}、{window} 占位
	RejectMessage string `json:"rejectMessage"`
}

// Network 地址聚合口径（前缀长度）。
type Network struct {
	// IPv4Subnet 网段规则里 IPv4 的前缀长度，默认 24
	IPv4Subnet int `json:"ipv4Subnet"`
	// IPv6Host 「单个地址」规则里 IPv6 的前缀长度，默认 64（一户一段）
	IPv6Host int `json:"ipv6Host"`
	// IPv6Subnet 网段规则里 IPv6 的前缀长度，默认 48
	IPv6Subnet int `json:"ipv6Subnet"`
}

// Rule 一条配额规则：Scope 主体在 WindowSeconds 内最多注册 Limit 个账号。
type Rule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Scope   string `json:"scope"`
	// WindowSeconds 滑动窗口长度；0 表示不限时间（累计），以注册台账为准
	WindowSeconds int    `json:"windowSeconds"`
	Limit         int    `json:"limit"`
	Action        string `json:"action"`
	// Methods 只统计、只约束这些注册方式；为空表示全部
	Methods []string `json:"methods,omitempty"`
}

// Lifetime 这条规则是否不限时间（累计）。
func (r Rule) Lifetime() bool { return r.WindowSeconds == 0 }

// Applies 这条规则是否约束该注册方式。
func (r Rule) Applies(method string) bool {
	return r.Enabled && (len(r.Methods) == 0 || slices.Contains(r.Methods, method))
}

// Penalty 惩罚期：Strikes 次被配额拒绝（StrikeWindowSeconds 内）后，地址被整体禁止注册。
// 第 n 次进惩罚期时长 = BaseSeconds × Multiplier^(n-1)，封顶 MaxSeconds。
type Penalty struct {
	Enabled             bool    `json:"enabled"`
	Strikes             int     `json:"strikes"`
	StrikeWindowSeconds int     `json:"strikeWindowSeconds"`
	BaseSeconds         int     `json:"baseSeconds"`
	Multiplier          float64 `json:"multiplier"`
	MaxSeconds          int     `json:"maxSeconds"`
	// Scope 地址一侧的惩罚主体：ip 或 subnet
	Scope string `json:"scope"`
	// Device 设备一侧的规则（同一设备、同一设备换地址）拒绝时，惩罚落在设备上：
	// 进了惩罚期的设备换任何网络都注册不了，进了惩罚期的地址换任何设备也一样
	Device bool `json:"device"`
}

// Surge 突增检测：最近 WindowMinutes 分钟的注册数明显高于此前 BaselineMinutes 分钟的平常水平时触发。
type Surge struct {
	Enabled         bool `json:"enabled"`
	WindowMinutes   int  `json:"windowMinutes"`
	BaselineMinutes int  `json:"baselineMinutes"`
	// Sensitivity 超出期望多少个标准差算突增，越小越敏感
	Sensitivity float64 `json:"sensitivity"`
	// MinRegistrations 窗口内注册数不到这个数永不触发：冷门应用一分钟来三个人不算攻击
	MinRegistrations int    `json:"minRegistrations"`
	Action           string `json:"action"`
	// TightenFactor 突增期间所有配额乘以这个系数（0–1），1 表示不收紧
	TightenFactor float64 `json:"tightenFactor"`
}

// ISPRule 运营商名称包含任一关键词（不分大小写）时按 Action 处置。
type ISPRule struct {
	Enabled  bool     `json:"enabled"`
	Keywords []string `json:"keywords"`
	Action   string   `json:"action"`
}

// GeoRule 属地限制。名单项是「国家」或「国家/省」，例如「中国」「中国/广东」。
type GeoRule struct {
	Mode    string   `json:"mode"`
	Regions []string `json:"regions"`
	Action  string   `json:"action"`
	// Unknown 属地查不到时：allow 放行（默认）/ reject 按 Action 处置
	Unknown string `json:"unknown"`
}

// DeviceRule 设备标识的质量要求。
//
// 「无效」指上报了值但明显不是真实设备：过短、全是同一个字符、含不可见字符、超长。
// 占位值（unknown、全 0、Android 9774d56d682e549c 等）在接入层已被当成没上报。
// 无效的设备标识不参与任何设备规则 —— 否则所有上报 "aaaaaaaa" 的人会挤在同一台「设备」上互相触发上限。
type DeviceRule struct {
	// Missing 没上报设备标识时：allow / challenge / reject
	Missing string `json:"missing"`
	// Invalid 上报了无效标识时：allow / challenge / reject
	Invalid string `json:"invalid"`
	// MinLength 有效标识的最短长度，默认 8
	MinLength int `json:"minLength"`
}

// 缺少 / 无效设备的处置。
const (
	DeviceAllow     = "allow"
	DeviceChallenge = "challenge"
	DeviceReject    = "reject"
)

// DefaultISPKeywords 常见机房与云厂商出口。正常用户几乎不会从这些网络注册。
var DefaultISPKeywords = []string{"阿里云", "腾讯云", "华为云", "百度云", "亚马逊", "Amazon", "Google", "Microsoft", "DigitalOcean", "Linode", "Vultr", "OVH", "Hetzner", "数据中心", "IDC"}

// DefaultPolicy 未配置时的策略：不限制。规则给一套常用起点，开启即可用。
func DefaultPolicy() Policy {
	return Policy{
		Enabled: false,
		Mode:    ModeEnforce,
		Network: Network{IPv4Subnet: 24, IPv6Host: 64, IPv6Subnet: 48},
		Rules: []Rule{
			{ID: "ip-hour", Name: "同一地址每小时", Enabled: true, Scope: ScopeIP, WindowSeconds: 3600, Limit: 3, Action: ActionChallenge},
			{ID: "ip-day", Name: "同一地址每天", Enabled: true, Scope: ScopeIP, WindowSeconds: 86400, Limit: 10, Action: ActionReject},
			{ID: "subnet-day", Name: "同一网段每天", Enabled: true, Scope: ScopeSubnet, WindowSeconds: 86400, Limit: 50, Action: ActionReject},
			{ID: "device-day", Name: "同一设备每天", Enabled: true, Scope: ScopeDevice, WindowSeconds: 86400, Limit: 3, Action: ActionReject},
			{ID: "ip-devices-hour", Name: "同一地址每小时出现的设备", Enabled: true, Scope: ScopeIPDevices, WindowSeconds: 3600, Limit: 10, Action: ActionChallenge},
			{ID: "device-ips-day", Name: "同一设备每天换的地址", Enabled: true, Scope: ScopeDeviceIPs, WindowSeconds: 86400, Limit: 5, Action: ActionChallenge},
		},
		Penalty:   Penalty{Enabled: true, Strikes: 5, StrikeWindowSeconds: 3600, BaseSeconds: 1800, Multiplier: 2, MaxSeconds: 7 * 86400, Scope: ScopeIP, Device: true},
		Surge:     Surge{Enabled: false, WindowMinutes: 5, BaselineMinutes: 60, Sensitivity: 4, MinRegistrations: 30, Action: ActionChallenge, TightenFactor: 0.5},
		ISP:       ISPRule{Enabled: false, Keywords: slices.Clone(DefaultISPKeywords), Action: ActionChallenge},
		Geo:       GeoRule{Mode: GeoOff, Regions: []string{}, Action: ActionReject, Unknown: MissingIPAllow},
		Device:    DeviceRule{Missing: DeviceAllow, Invalid: DeviceChallenge, MinLength: 8},
		MissingIP: MissingIPAllow,
		FailMode:  FailOpen,
	}
}

// Normalize 规整并校验。返回的错误文案直接给管理员看。
func (p Policy) Normalize() (Policy, error) {
	out := p
	switch out.Mode {
	case "":
		out.Mode = ModeEnforce
	case ModeEnforce, ModeObserve:
	default:
		return out, fmt.Errorf("不认识的运行模式 %q", out.Mode)
	}
	network, err := out.Network.normalize()
	if err != nil {
		return out, err
	}
	out.Network = network
	if len(out.Rules) > MaxRules {
		return out, fmt.Errorf("配额规则最多 %d 条", MaxRules)
	}
	rules := make([]Rule, 0, len(out.Rules))
	seen := map[string]bool{}
	for i, rule := range out.Rules {
		normalized, err := rule.normalize(i)
		if err != nil {
			return out, err
		}
		if seen[normalized.ID] {
			normalized.ID = fmt.Sprintf("%s-%d", normalized.ID, i+1)
		}
		seen[normalized.ID] = true
		rules = append(rules, normalized)
	}
	out.Rules = rules
	if out.Penalty, err = out.Penalty.normalize(); err != nil {
		return out, err
	}
	if out.Surge, err = out.Surge.normalize(); err != nil {
		return out, err
	}
	if out.ISP, err = out.ISP.normalize(); err != nil {
		return out, err
	}
	if out.Geo, err = out.Geo.normalize(); err != nil {
		return out, err
	}
	if out.Device, err = out.Device.normalize(); err != nil {
		return out, err
	}
	switch out.MissingIP {
	case "":
		out.MissingIP = MissingIPAllow
	case MissingIPAllow, MissingIPReject:
	default:
		return out, fmt.Errorf("不认识的缺少 IP 处置 %q", out.MissingIP)
	}
	switch out.FailMode {
	case "":
		out.FailMode = FailOpen
	case FailOpen, FailClosed:
	default:
		return out, fmt.Errorf("不认识的故障处置 %q", out.FailMode)
	}
	out.RejectMessage = strings.TrimSpace(out.RejectMessage)
	if len([]rune(out.RejectMessage)) > MaxMessageLength {
		return out, fmt.Errorf("拒绝提示最长 %d 个字", MaxMessageLength)
	}
	return out, nil
}

func (n Network) normalize() (Network, error) {
	out := n
	if out.IPv4Subnet == 0 {
		out.IPv4Subnet = 24
	}
	if out.IPv6Host == 0 {
		out.IPv6Host = 64
	}
	if out.IPv6Subnet == 0 {
		out.IPv6Subnet = 48
	}
	if out.IPv4Subnet < 8 || out.IPv4Subnet > 32 {
		return out, fmt.Errorf("IPv4 网段前缀需要在 /8–/32 之间")
	}
	if out.IPv6Host < 32 || out.IPv6Host > 128 {
		return out, fmt.Errorf("IPv6 单地址前缀需要在 /32–/128 之间")
	}
	if out.IPv6Subnet < 16 || out.IPv6Subnet > out.IPv6Host {
		return out, fmt.Errorf("IPv6 网段前缀需要在 /16 与单地址前缀（/%d）之间", out.IPv6Host)
	}
	return out, nil
}

func normalizeAction(action, label string, allowObserve bool) (string, error) {
	switch action {
	case "":
		return ActionReject, nil
	case ActionReject, ActionChallenge:
		return action, nil
	case ActionObserve:
		if allowObserve {
			return action, nil
		}
	}
	return "", fmt.Errorf("%s：不认识的处置方式 %q", label, action)
}

func (r Rule) normalize(index int) (Rule, error) {
	out := r
	out.ID = strings.TrimSpace(out.ID)
	if out.ID == "" {
		out.ID = fmt.Sprintf("rule-%d", index+1)
	}
	if len(out.ID) > 32 || strings.ContainsAny(out.ID, ":{} ") {
		return out, fmt.Errorf("第 %d 条规则的标识只能用 32 位以内的字母、数字与横线", index+1)
	}
	out.Name = strings.TrimSpace(out.Name)
	label := out.Name
	if label == "" {
		label = fmt.Sprintf("第 %d 条规则", index+1)
	}
	if !slices.Contains(Scopes, out.Scope) {
		return out, fmt.Errorf("%s：不认识的统计主体 %q", label, out.Scope)
	}
	if out.WindowSeconds < 0 || out.WindowSeconds > MaxWindowSeconds {
		return out, fmt.Errorf("%s：统计窗口最长 365 天", label)
	}
	if out.WindowSeconds > 0 && out.WindowSeconds < MinWindowSeconds {
		return out, fmt.Errorf("%s：统计窗口至少 1 分钟", label)
	}
	if out.Lifetime() && out.Scope == ScopeApp {
		return out, fmt.Errorf("%s：整个应用的累计注册数不该有上限，请设一个统计窗口", label)
	}
	if out.Lifetime() && DistinctScope(out.Scope) {
		return out, fmt.Errorf("%s：不同设备数 / 不同地址数需要设一个统计窗口", label)
	}
	if out.Limit < 1 || out.Limit > MaxLimit {
		return out, fmt.Errorf("%s：上限需要在 1–%d 之间", label, MaxLimit)
	}
	action, err := normalizeAction(out.Action, label, true)
	if err != nil {
		return out, err
	}
	out.Action = action
	methods := make([]string, 0, len(out.Methods))
	for _, method := range out.Methods {
		method = strings.TrimSpace(method)
		if !slices.Contains(Methods, method) {
			return out, fmt.Errorf("%s：不认识的注册方式 %q", label, method)
		}
		if !slices.Contains(methods, method) {
			methods = append(methods, method)
		}
	}
	if len(methods) == 0 || len(methods) == len(Methods) {
		methods = nil
	}
	out.Methods = methods
	if out.Name == "" {
		out.Name = DescribeRule(out)
	}
	return out, nil
}

func (p Penalty) normalize() (Penalty, error) {
	out := p
	if out.Strikes == 0 {
		out.Strikes = 5
	}
	if out.StrikeWindowSeconds == 0 {
		out.StrikeWindowSeconds = 3600
	}
	if out.BaseSeconds == 0 {
		out.BaseSeconds = 1800
	}
	if out.Multiplier == 0 {
		out.Multiplier = 2
	}
	if out.MaxSeconds == 0 {
		out.MaxSeconds = 7 * 86400
	}
	if out.Scope == "" {
		out.Scope = ScopeIP
	}
	switch {
	case out.Strikes < 1 || out.Strikes > 1000:
		return out, fmt.Errorf("惩罚期：触发次数需要在 1–1000 之间")
	case out.StrikeWindowSeconds < MinWindowSeconds || out.StrikeWindowSeconds > 7*86400:
		return out, fmt.Errorf("惩罚期：计次窗口需要在 1 分钟到 7 天之间")
	case out.BaseSeconds < MinWindowSeconds || out.BaseSeconds > MaxPenaltySeconds:
		return out, fmt.Errorf("惩罚期：首次时长需要在 1 分钟到 30 天之间")
	case out.Multiplier < 1 || out.Multiplier > 10:
		return out, fmt.Errorf("惩罚期：递增倍数需要在 1–10 之间")
	case out.MaxSeconds < out.BaseSeconds || out.MaxSeconds > MaxPenaltySeconds:
		return out, fmt.Errorf("惩罚期：最长时长需要不短于首次时长、不超过 30 天")
	case out.Scope != ScopeIP && out.Scope != ScopeSubnet:
		return out, fmt.Errorf("惩罚期：主体只能是单个地址或网段")
	}
	return out, nil
}

func (d DeviceRule) normalize() (DeviceRule, error) {
	out := d
	if out.Missing == "" {
		out.Missing = DeviceAllow
	}
	if out.Invalid == "" {
		out.Invalid = DeviceChallenge
	}
	if out.MinLength == 0 {
		out.MinLength = 8
	}
	for _, value := range []string{out.Missing, out.Invalid} {
		switch value {
		case DeviceAllow, DeviceChallenge, DeviceReject:
		default:
			return out, fmt.Errorf("不认识的设备标识处置 %q", value)
		}
	}
	if out.MinLength < 4 || out.MinLength > 64 {
		return out, fmt.Errorf("设备标识最短长度需要在 4–64 之间")
	}
	return out, nil
}

func (s Surge) normalize() (Surge, error) {
	out := s
	if out.WindowMinutes == 0 {
		out.WindowMinutes = 5
	}
	if out.BaselineMinutes == 0 {
		out.BaselineMinutes = 60
	}
	if out.Sensitivity == 0 {
		out.Sensitivity = 4
	}
	if out.MinRegistrations == 0 {
		out.MinRegistrations = 30
	}
	if out.TightenFactor == 0 {
		out.TightenFactor = 1
	}
	switch {
	case out.WindowMinutes < 1 || out.WindowMinutes > 60:
		return out, fmt.Errorf("突增检测：观察窗口需要在 1–60 分钟之间")
	case out.BaselineMinutes < out.WindowMinutes*3 || out.BaselineMinutes > MaxSurgeBaselineMinutes:
		return out, fmt.Errorf("突增检测：基线至少是观察窗口的 3 倍，最长 %d 分钟", MaxSurgeBaselineMinutes)
	case out.Sensitivity < 1 || out.Sensitivity > 20:
		return out, fmt.Errorf("突增检测：灵敏度需要在 1–20 个标准差之间")
	case out.MinRegistrations < 1:
		return out, fmt.Errorf("突增检测：最小注册数至少为 1")
	case out.TightenFactor <= 0 || out.TightenFactor > 1:
		return out, fmt.Errorf("突增检测：收紧系数需要在 0–1 之间")
	}
	action, err := normalizeAction(out.Action, "突增检测", true)
	if err != nil {
		return out, err
	}
	out.Action = action
	return out, nil
}

func (r ISPRule) normalize() (ISPRule, error) {
	out := r
	keywords := make([]string, 0, len(out.Keywords))
	for _, keyword := range out.Keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" || slices.ContainsFunc(keywords, func(k string) bool { return strings.EqualFold(k, keyword) }) {
			continue
		}
		keywords = append(keywords, keyword)
	}
	if len(keywords) > MaxKeywords {
		return out, fmt.Errorf("运营商关键词最多 %d 个", MaxKeywords)
	}
	if out.Enabled && len(keywords) == 0 {
		return out, fmt.Errorf("开启运营商限制时至少填一个关键词")
	}
	out.Keywords = keywords
	action, err := normalizeAction(out.Action, "运营商限制", true)
	if err != nil {
		return out, err
	}
	out.Action = action
	return out, nil
}

func (g GeoRule) normalize() (GeoRule, error) {
	out := g
	switch out.Mode {
	case "":
		out.Mode = GeoOff
	case GeoOff, GeoAllow, GeoDeny:
	default:
		return out, fmt.Errorf("不认识的属地限制方式 %q", out.Mode)
	}
	regions := make([]string, 0, len(out.Regions))
	for _, region := range out.Regions {
		region = NormalizeRegion(region)
		if region == "" || slices.Contains(regions, region) {
			continue
		}
		regions = append(regions, region)
	}
	if len(regions) > MaxGeoEntries {
		return out, fmt.Errorf("属地名单最多 %d 项", MaxGeoEntries)
	}
	if out.Mode != GeoOff && len(regions) == 0 {
		return out, fmt.Errorf("开启属地限制时至少填一个属地")
	}
	out.Regions = regions
	action, err := normalizeAction(out.Action, "属地限制", true)
	if err != nil {
		return out, err
	}
	out.Action = action
	switch out.Unknown {
	case "":
		out.Unknown = MissingIPAllow
	case MissingIPAllow, MissingIPReject:
	default:
		return out, fmt.Errorf("不认识的属地未知处置 %q", out.Unknown)
	}
	return out, nil
}

// NormalizeRegion 属地名单项规整成「国家」或「国家/省」，去掉多余的空白与斜杠。
func NormalizeRegion(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '｜' || r == '|' })
	out := make([]string, 0, 2)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
		if len(out) == 2 {
			break
		}
	}
	return strings.Join(out, "/")
}

// DescribeRule 规则的默认名称，例如「同一地址每小时最多 3 个」「同一地址的不同设备每小时最多 10 台」。
func DescribeRule(r Rule) string {
	unit := "个"
	if r.Scope == ScopeIPDevices {
		unit = "台"
	}
	return fmt.Sprintf("%s%s最多 %d %s", ScopeLabel(r.Scope), WindowLabel(r.WindowSeconds), r.Limit, unit)
}

// ScopeLabel 统计主体的中文名。
func ScopeLabel(scope string) string {
	switch scope {
	case ScopeIP:
		return "同一地址"
	case ScopeSubnet:
		return "同一网段"
	case ScopeDevice:
		return "同一设备"
	case ScopeIPDevices:
		return "同一地址的不同设备"
	case ScopeDeviceIPs:
		return "同一设备的不同地址"
	case ScopeCity:
		return "同一城市"
	case ScopeApp:
		return "整个应用"
	}
	return scope
}

// WindowLabel 窗口的中文说法：「累计」「每小时」「每 15 分钟」「每 3 天」。
func WindowLabel(seconds int) string {
	switch {
	case seconds == 0:
		return "累计"
	case seconds%86400 == 0:
		if seconds == 86400 {
			return "每天"
		}
		return fmt.Sprintf("每 %d 天", seconds/86400)
	case seconds%3600 == 0:
		if seconds == 3600 {
			return "每小时"
		}
		return fmt.Sprintf("每 %d 小时", seconds/3600)
	case seconds%60 == 0:
		return fmt.Sprintf("每 %d 分钟", seconds/60)
	}
	return fmt.Sprintf("每 %d 秒", seconds)
}

// FromSettings 从应用设置读出策略。第二个返回值表示是否用新策略保存过。
//
// 没有 registerIp 键时按旧开关 registerCheckIp 推导：开着就是「同一地址累计最多 1 个、缺 IP 拒绝」，
// 与升级前的行为一致。存进去的策略都校验过，读出来不对只可能被手工改坏，那时按不限制处理 ——
// 把正常用户挡在注册页外面比少拦一次代价大得多。
func FromSettings(settings map[string]any) (Policy, bool) {
	raw, ok := settings[SettingsKey]
	if !ok || raw == nil {
		return legacyPolicy(settings), false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return DefaultPolicy(), true
	}
	policy := DefaultPolicy()
	// 先清空切片：json 解到已有切片上会按下标覆盖，留下默认规则的尾巴
	policy.Rules = nil
	policy.ISP.Keywords = nil
	policy.Geo.Regions = nil
	if err := json.Unmarshal(data, &policy); err != nil {
		return DefaultPolicy(), true
	}
	normalized, err := policy.Normalize()
	if err != nil {
		return DefaultPolicy(), true
	}
	return normalized, true
}

func legacyPolicy(settings map[string]any) Policy {
	policy := DefaultPolicy()
	if !legacyEnabled(settings) {
		return policy
	}
	policy.Enabled = true
	policy.Rules = []Rule{{ID: "legacy-ip", Name: "同一地址累计最多 1 个（旧开关）", Enabled: true, Scope: ScopeIP, WindowSeconds: 0, Limit: 1, Action: ActionReject}}
	policy.Penalty.Enabled = false
	policy.MissingIP = MissingIPReject
	policy.Device = DeviceRule{Missing: DeviceAllow, Invalid: DeviceAllow, MinLength: 8}
	// 旧开关按完整地址比对，IPv6 也是 /128
	policy.Network.IPv6Host = 128
	policy.Network.IPv6Subnet = 64
	return policy
}

func legacyEnabled(settings map[string]any) bool {
	switch value := settings[LegacySettingsKey].(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			return true
		}
	case float64:
		return value != 0
	}
	return false
}

// ToSettings 存进 apps.settings 的形态。
func (p Policy) ToSettings() map[string]any {
	data, _ := json.Marshal(p)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

// LegacyValue 写回旧开关的值：有一条「同一地址累计最多 1 个、拒绝」的规则时为真。
// 只给还在读 registerCheckIp 的旧客户端与导出脚本看，判定一律以新策略为准。
func (p Policy) LegacyValue() bool {
	if !p.Enabled || p.Mode != ModeEnforce {
		return false
	}
	return slices.ContainsFunc(p.Rules, func(r Rule) bool {
		return r.Enabled && r.Scope == ScopeIP && r.Lifetime() && r.Limit == 1 && r.Action == ActionReject && len(r.Methods) == 0
	})
}

// NeedsLocation 判定是否用得到属地 / 运营商：用不到就不查，省一次远程调用。
func (p Policy) NeedsLocation() bool {
	if !p.Enabled {
		return false
	}
	if p.ISP.Enabled || p.Geo.Mode != GeoOff {
		return true
	}
	return slices.ContainsFunc(p.Rules, func(r Rule) bool { return r.Enabled && r.Scope == ScopeCity })
}
