package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"

	uaparser "github.com/mileusna/useragent"
)

// RiskEvalEnv 是规则引擎看到的**全部**事实，也是 expr 表达式的类型化环境。
//
// 用具名 struct 而不是 map[string]any 做 expr 的 Env 有一个决定性好处：
// 表达式在**保存规则时**就能做编译期校验。写错一个变量名（`ip_reqeust_count`）
// 在 map 环境下只会在运行期取到 nil、静默判假 —— 那条规则从此永不命中，
// 而管理员完全看不出来。用 struct 环境时它是一个当场返回给管理员的编译错误。
//
// 字段的 expr 标签就是控制台上显示的变量名，与
// securitydomain.VariableCatalogEntries() 一一对应，由
// TestRiskEnvMatchesVariableCatalog 双向钉死：
// 目录里多一条 → 提示里有但表达式里用不了；
// 结构里多一条 → 能用但没人知道它存在。
type RiskEvalEnv struct {
	// ── 请求维度 ──
	Scene     string `expr:"scene" json:"scene"`
	IP        string `expr:"ip" json:"ip"`
	DeviceID  string `expr:"device_id" json:"device_id"`
	Account   string `expr:"account" json:"account"`
	UserAgent string `expr:"user_agent" json:"user_agent"`
	AppID     int64  `expr:"app_id" json:"app_id"`
	UserID    int64  `expr:"user_id" json:"user_id"`
	HourOfDay int    `expr:"hour_of_day" json:"hour_of_day"`
	Weekday   int    `expr:"weekday" json:"weekday"`

	// ── 客户端（User-Agent 解析）──
	UAIsBot          bool   `expr:"ua_is_bot" json:"ua_is_bot"`
	UABrowser        string `expr:"ua_browser" json:"ua_browser"`
	UABrowserVersion string `expr:"ua_browser_version" json:"ua_browser_version"`
	UAOS             string `expr:"ua_os" json:"ua_os"`
	UAOSVersion      string `expr:"ua_os_version" json:"ua_os_version"`
	UADevice         string `expr:"ua_device" json:"ua_device"`
	UADeviceClass    string `expr:"ua_device_class" json:"ua_device_class"`
	UAIsMobile       bool   `expr:"ua_is_mobile" json:"ua_is_mobile"`
	UALength         int    `expr:"ua_length" json:"ua_length"`

	// ── 频率与扩散速度 ──
	IPRequestCount            int64 `expr:"ip_request_count" json:"ip_request_count"`
	AccountRequestCount       int64 `expr:"account_request_count" json:"account_request_count"`
	DeviceRequestCount        int64 `expr:"device_request_count" json:"device_request_count"`
	AccountDeviceRequestCount int64 `expr:"account_device_request_count" json:"account_device_request_count"`
	IPRateLimited             bool  `expr:"ip_rate_limited" json:"ip_rate_limited"`
	AccountRateLimited        bool  `expr:"account_rate_limited" json:"account_rate_limited"`
	DeviceRateLimited         bool  `expr:"device_rate_limited" json:"device_rate_limited"`
	AccountDeviceRateLimited  bool  `expr:"account_device_rate_limited" json:"account_device_rate_limited"`
	IPAccountsSeen            int64 `expr:"ip_accounts_seen" json:"ip_accounts_seen"`
	DeviceAccountsSeen        int64 `expr:"device_accounts_seen" json:"device_accounts_seen"`
	AccountIPsSeen            int64 `expr:"account_ips_seen" json:"account_ips_seen"`

	// ── 设备档案 ──
	DeviceAgeHours  float64 `expr:"device_age_hours" json:"device_age_hours"`
	DeviceSeenCount int64   `expr:"device_seen_count" json:"device_seen_count"`
	DeviceKnown     bool    `expr:"device_known" json:"device_known"`
	DeviceRiskTag   string  `expr:"device_risk_tag" json:"device_risk_tag"`
	DeviceBlocked   bool    `expr:"device_blocked" json:"device_blocked"`

	// ── 网络情报 ──
	IPIsProxy      bool   `expr:"ip_is_proxy" json:"ip_is_proxy"`
	IPIsVPN        bool   `expr:"ip_is_vpn" json:"ip_is_vpn"`
	IPIsTor        bool   `expr:"ip_is_tor" json:"ip_is_tor"`
	IPIsDatacenter bool   `expr:"ip_is_datacenter" json:"ip_is_datacenter"`
	IPRiskScore    int    `expr:"ip_risk_score" json:"ip_risk_score"`
	IPRiskTag      string `expr:"ip_risk_tag" json:"ip_risk_tag"`
	IPKnown        bool   `expr:"ip_known" json:"ip_known"`
	IPTrusted      bool   `expr:"ip_trusted" json:"ip_trusted"`
	IPTotalBlocks  int64  `expr:"ip_total_blocks" json:"ip_total_blocks"`
	IPASN          string `expr:"ip_asn" json:"ip_asn"`
	IPISP          string `expr:"ip_isp" json:"ip_isp"`

	// ── 地理 ──
	GeoCountry string `expr:"geo_country" json:"geo_country"`
	GeoRegion  string `expr:"geo_region" json:"geo_region"`
	GeoCity    string `expr:"geo_city" json:"geo_city"`
	GeoKnown   bool   `expr:"geo_known" json:"geo_known"`

	// ── 调用方透传 ──
	Extra map[string]any `expr:"extra" json:"extra"`

	// evaluatedAt 供 in_time_window 等函数使用，不进表达式环境也不落库。
	evaluatedAt time.Time `expr:"-" json:"-"`
}

// deviceClass 由 UA 解析结果推导出的客户端类型
const (
	deviceClassDesktop = "desktop"
	deviceClassMobile  = "mobile"
	deviceClassTablet  = "tablet"
	deviceClassBot     = "bot"
	deviceClassUnknown = "unknown"
)

// applyUserAgent 解析 User-Agent 并填充客户端维度。
//
// 旧实现用 mssola/useragent，只能给出「浏览器名 / OS 名 / 是不是 bot」三个信号。
// 换成 mileusna/useragent 之后多出设备型号、桌面/移动/平板分类、
// 浏览器与系统版本号 —— 这些恰好是「同一批请求是不是同一个自动化工具打的」
// 最直接的判据，而且它零正则回溯、单次解析在百纳秒级，挂在登录热路径上不会有代价。
func (e *RiskEvalEnv) applyUserAgent(raw string) {
	e.UserAgent = raw
	e.UALength = len([]rune(strings.TrimSpace(raw)))
	if strings.TrimSpace(raw) == "" {
		e.UADeviceClass = deviceClassUnknown
		return
	}

	ua := uaparser.Parse(raw)
	e.UAIsBot = ua.Bot
	e.UABrowser = ua.Name
	e.UABrowserVersion = ua.Version
	e.UAOS = ua.OS
	e.UAOSVersion = ua.OSVersion
	e.UADevice = ua.Device
	e.UAIsMobile = ua.Mobile

	switch {
	case ua.Bot:
		e.UADeviceClass = deviceClassBot
	case ua.Tablet:
		e.UADeviceClass = deviceClassTablet
	case ua.Mobile:
		e.UADeviceClass = deviceClassMobile
	case ua.Desktop:
		e.UADeviceClass = deviceClassDesktop
	default:
		e.UADeviceClass = deviceClassUnknown
	}
}

// newRiskEvalEnv 构造一个各字段均已初始化到「无信号」状态的环境。
// 显式初始化很重要：表达式里读到零值和读到 nil 是两回事，
// 后者会让 `ip_risk_score > 50` 这类比较在运行期报错而不是判假。
func newRiskEvalEnv(now time.Time) *RiskEvalEnv {
	return &RiskEvalEnv{
		UADeviceClass: deviceClassUnknown,
		DeviceRiskTag: securitydomain.TagNormal,
		IPRiskTag:     securitydomain.TagNormal,
		Extra:         map[string]any{},
		HourOfDay:     now.Hour(),
		Weekday:       int(now.Weekday()),
		evaluatedAt:   now,
	}
}

// riskEnvFieldNames 缓存 expr 标签名 → 反射索引，AsMap 与目录自检共用。
var riskEnvFieldNames = buildRiskEnvFieldIndex()

func buildRiskEnvFieldIndex() map[string]int {
	t := reflect.TypeFor[RiskEvalEnv]()
	index := make(map[string]int, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Tag.Get("expr")
		if name == "" || name == "-" {
			continue
		}
		index[name] = i
	}
	return index
}

// RiskEnvVariableNames 返回环境暴露的全部变量名，供目录自检使用。
func RiskEnvVariableNames() []string {
	names := make([]string, 0, len(riskEnvFieldNames))
	for name := range riskEnvFieldNames {
		names = append(names, name)
	}
	return names
}

// AsMap 把环境摊平成可落库、可下发的快照。
// 评估记录里存的就是它 —— 详情页因此能逐条回答「判定当时看到的是什么」。
func (e *RiskEvalEnv) AsMap() map[string]any {
	if e == nil {
		return map[string]any{}
	}
	value := reflect.ValueOf(*e)
	out := make(map[string]any, len(riskEnvFieldNames))
	for name, idx := range riskEnvFieldNames {
		out[name] = value.Field(idx).Interface()
	}
	return out
}

// Snapshot 生成用于持久化的紧凑快照：剔除空字符串与零值扩展字段，
// 避免每条评估记录都拖着一堆 "" 进 JSONB。
func (e *RiskEvalEnv) Snapshot() map[string]any {
	full := e.AsMap()
	out := make(map[string]any, len(full))
	for k, v := range full {
		switch typed := v.(type) {
		case string:
			if typed == "" {
				continue
			}
		case map[string]any:
			if len(typed) == 0 {
				continue
			}
		}
		out[k] = v
	}
	return out
}

// riskEnvFromSnapshot 把落库的快照还原成环境。
// 模拟器「用这条历史记录重跑一遍」依赖它 —— 否则改完规则只能等下一次真实请求。
func riskEnvFromSnapshot(snapshot map[string]any, now time.Time) *RiskEvalEnv {
	env := newRiskEvalEnv(now)
	if len(snapshot) == 0 {
		return env
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return env
	}
	// 快照的键名与 json 标签一致（两者都用 expr 变量名），可直接回灌。
	_ = json.Unmarshal(raw, env)
	if env.Extra == nil {
		env.Extra = map[string]any{}
	}
	if env.UADeviceClass == "" {
		env.UADeviceClass = deviceClassUnknown
	}
	env.evaluatedAt = now
	env.HourOfDay = now.Hour()
	env.Weekday = int(now.Weekday())
	return env
}
