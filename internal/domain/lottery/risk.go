package lottery

import (
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// 抽奖风控规则。
//
// 旧版只有「同设备 / 同 IP 账号数超过上限 → 只出未中奖」两项，trace 里只有一个 risk 布尔：
// 管理端看到每个奖品都是「设备聚集 0」、概率全是 0%，却又抽中了兜底奖，看不出几个账号、上限多少、
// 为什么还发了东西。
//
// 现在风控是一组规则，每条 = 信号 比较 阈值 → 处置。信号是这一次抽奖时的事实（RiskSignals），
// 处置从轻到重：
//
//	damp    降权：中奖奖品的权重 × factor，多条命中时相乘
//	rare    停发稀有：公示概率低于 rareBelow 的中奖奖品这一次不出
//	noPity  不触发保底：硬保底、软保底、伪随机分布都按「还没连败」算
//	floor   只发兜底：兜底奖以外的中奖奖品都不出（奖盘有「谢谢参与」时照常可出）
//	deny    拒绝抽奖：这一次不抽，次数与积分不扣，单独记一条风控事件
//
// 每条规则的取值、阈值、是否命中都写进 trace.riskReport，管理端逐条可见；命中的信号与最重的处置
// 另写进 lottery_draws.risk_rules / risk_action，按风控筛记录走索引。
//
// 旧字段 deviceAccounts / ipAccounts 读入时转成两条「只发兜底」规则，已保存的活动行为不变。

// 风控处置。
const (
	RiskDamp   = "damp"
	RiskRare   = "rare"
	RiskNoPity = "noPity"
	RiskFloor  = "floor"
	RiskDeny   = "deny"
)

// 改发兜底奖的原因（trace.fallbackReason）。
const (
	// FallbackEmpty 这一次所有奖品的最终权重都是 0（风控、限量、预算一起把奖盘清空了）
	FallbackEmpty = "empty"
	// FallbackContended 抽中的限量奖品在占库存时被并发抢光
	FallbackContended = "contended"
)

// 比较方式。
const (
	RiskGT  = "gt"
	RiskGTE = "gte"
	RiskLT  = "lt"
	RiskLTE = "lte"
)

// 风控信号。
const (
	RiskSignalDeviceAccounts = "deviceAccounts"
	RiskSignalIPAccounts     = "ipAccounts"
	RiskSignalSubnetAccounts = "subnetAccounts"
	RiskSignalDeviceHistory  = "deviceHistory"
	RiskSignalAccountDevices = "accountDevices"
	RiskSignalAccountAge     = "accountAgeHours"
	RiskSignalBurst          = "burst10m"
	RiskSignalTodayDraws     = "todayDraws"
	RiskSignalWinsToday      = "winsToday"
	RiskSignalRiskScore      = "riskScore"
	RiskSignalNoDevice       = "noDevice"
	// RiskSignalLegacy 迁移前记录里的「设备或网络聚集」（分不清是哪一项），只出现在记录里
	RiskSignalLegacy = "legacyCluster"
)

// 风控规则的规模与取值上界。
const (
	MaxRiskRules     = 16
	MaxRiskThreshold = 1_000_000
	// BurstWindowMinutes 「近期抽奖次数」的窗口
	BurstWindowMinutes = 10
)

// RiskRule 一条风控规则。
type RiskRule struct {
	Name      string  `json:"name,omitempty"`
	Signal    string  `json:"signal"`
	Op        string  `json:"op"`
	Threshold float64 `json:"threshold"`
	Action    string  `json:"action"`
	// Factor damp 的系数（0–1）
	Factor float64 `json:"factor,omitempty"`
	// RareBelow rare 的公示概率线（0–1）：低于它的中奖奖品停发
	RareBelow float64 `json:"rareBelow,omitempty"`
	// Kinds damp / rare 只作用于这些奖品类型；空为全部中奖奖品
	Kinds    []string `json:"kinds,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

// RiskGuard 活动的风控（strategy.risk）。
type RiskGuard struct {
	// 旧字段：同设备 / 同 IP 账号数上限，读入时转成「只发兜底」规则
	DeviceAccounts int        `json:"deviceAccounts,omitempty"`
	IPAccounts     int        `json:"ipAccounts,omitempty"`
	Rules          []RiskRule `json:"rules,omitempty"`
}

// EffectiveRules 生效的规则：旧字段转成的规则在前，停用的不算。
func (g *RiskGuard) EffectiveRules() []RiskRule {
	if g == nil {
		return nil
	}
	out := make([]RiskRule, 0, len(g.Rules)+2)
	out = append(out, legacyRiskRules(g)...)
	for _, rule := range g.Rules {
		if !rule.Disabled {
			out = append(out, rule)
		}
	}
	return out
}

// Uses 是否有生效的规则用到这个信号（仓储据此决定要不要去数）。
func (g *RiskGuard) Uses(signal string) bool {
	for _, rule := range g.EffectiveRules() {
		if rule.Signal == signal {
			return true
		}
	}
	return false
}

func legacyRiskRules(g *RiskGuard) []RiskRule {
	var out []RiskRule
	if g.DeviceAccounts > 0 {
		out = append(out, RiskRule{Name: "同设备多账号", Signal: RiskSignalDeviceAccounts, Op: RiskGT,
			Threshold: float64(g.DeviceAccounts), Action: RiskFloor})
	}
	if g.IPAccounts > 0 {
		out = append(out, RiskRule{Name: "同网络多账号", Signal: RiskSignalIPAccounts, Op: RiskGT,
			Threshold: float64(g.IPAccounts), Action: RiskFloor})
	}
	return out
}

// normalizeRisk 旧字段并进规则，清洗规则；没有规则时整项去掉。
func normalizeRisk(g *RiskGuard) *RiskGuard {
	if g == nil {
		return nil
	}
	rules := legacyRiskRules(g)
	for _, rule := range g.Rules {
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Signal = strings.TrimSpace(rule.Signal)
		if rule.Op == "" {
			if spec, ok := FindRiskSignal(rule.Signal); ok {
				rule.Op = spec.Op
			}
		}
		if rule.Action != RiskDamp {
			rule.Factor = 0
		}
		if rule.Action != RiskRare {
			rule.RareBelow = 0
		}
		if rule.Action != RiskDamp && rule.Action != RiskRare {
			rule.Kinds = nil
		}
		kinds := make([]string, 0, len(rule.Kinds))
		for _, kind := range rule.Kinds {
			if kind = strings.TrimSpace(kind); kind != "" && !slices.Contains(kinds, kind) {
				kinds = append(kinds, kind)
			}
		}
		rule.Kinds = kinds
		if len(rule.Kinds) == 0 {
			rule.Kinds = nil
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return nil
	}
	return &RiskGuard{Rules: rules}
}

func validateRisk(g *RiskGuard) error {
	if g == nil {
		return nil
	}
	if g.DeviceAccounts < 0 || g.DeviceAccounts > MaxRiskAccounts || g.IPAccounts < 0 || g.IPAccounts > MaxRiskAccounts {
		return fmt.Errorf("同设备 / 同网络账号数上限需要在 0–%d 之间", MaxRiskAccounts)
	}
	if len(g.Rules) > MaxRiskRules {
		return fmt.Errorf("风控规则最多 %d 条", MaxRiskRules)
	}
	for index, rule := range g.Rules {
		label := fmt.Sprintf("第 %d 条风控规则", index+1)
		if rule.Name != "" {
			label = fmt.Sprintf("风控规则「%s」", rule.Name)
		}
		if utf8.RuneCountInString(rule.Name) > MaxRuleNameRunes {
			return fmt.Errorf("%s的名称不能超过 %d 字", label, MaxRuleNameRunes)
		}
		spec, ok := FindRiskSignal(rule.Signal)
		if !ok || spec.Hidden {
			return fmt.Errorf("%s的信号 %s 不存在", label, rule.Signal)
		}
		switch rule.Op {
		case RiskGT, RiskGTE, RiskLT, RiskLTE:
		default:
			return fmt.Errorf("%s的比较方式无效", label)
		}
		if rule.Threshold < 0 || rule.Threshold > MaxRiskThreshold || math.IsNaN(rule.Threshold) {
			return fmt.Errorf("%s的阈值需要在 0–%d 之间", label, MaxRiskThreshold)
		}
		switch rule.Action {
		case RiskDamp:
			if rule.Factor <= 0 || rule.Factor >= 1 {
				return fmt.Errorf("%s的降权系数需要在 0–1 之间（不含 0 和 1）", label)
			}
		case RiskRare:
			if rule.RareBelow <= 0 || rule.RareBelow > 1 {
				return fmt.Errorf("%s的稀有概率线需要在 0–100%% 之间", label)
			}
		case RiskNoPity, RiskFloor, RiskDeny:
		default:
			return fmt.Errorf("%s的处置方式无效", label)
		}
		for _, kind := range rule.Kinds {
			if _, ok := FindSpec(kind); !ok {
				return fmt.Errorf("%s里的奖品类型 %s 不存在", label, kind)
			}
		}
	}
	return nil
}

// ── 目录 ──

// RiskSignalSpec 一个风控信号的说明（控制台据此渲染规则编辑器与方案）。
type RiskSignalSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	Hint  string `json:"hint"`
	Group string `json:"group"`
	// Op / Threshold 新建规则时的默认比较与阈值
	Op        string  `json:"op"`
	Threshold float64 `json:"threshold"`
	// Bool 取值只有 0 / 1（是或否）
	Bool   bool `json:"bool,omitempty"`
	Hidden bool `json:"hidden,omitempty"`
}

var riskSignals = []RiskSignalSpec{
	{Key: RiskSignalDeviceAccounts, Label: "同设备今日账号", Unit: "个", Group: "聚集", Op: RiskGT, Threshold: 3,
		Hint: "本活动今天在这台设备上抽过奖的账号数，含本人"},
	{Key: RiskSignalIPAccounts, Label: "同 IP 今日账号", Unit: "个", Group: "聚集", Op: RiskGT, Threshold: 5,
		Hint: "本活动今天从这个 IP 抽过奖的账号数，含本人"},
	{Key: RiskSignalSubnetAccounts, Label: "同网段今日账号", Unit: "个", Group: "聚集", Op: RiskGT, Threshold: 12,
		Hint: "IPv4 按 /24、IPv6 按 /64 合并后的账号数，含本人，挡换 IP 不换网段的批量号"},
	{Key: RiskSignalDeviceHistory, Label: "设备历史账号", Unit: "个", Group: "设备", Op: RiskGT, Threshold: 5,
		Hint: "这台设备在应用里登录过的账号总数"},
	{Key: RiskSignalAccountDevices, Label: "账号登录设备", Unit: "台", Group: "设备", Op: RiskGT, Threshold: 8,
		Hint: "这个账号登录过的设备数"},
	{Key: RiskSignalNoDevice, Label: "缺少设备标识", Unit: "", Group: "设备", Op: RiskGTE, Threshold: 1, Bool: true,
		Hint: "请求没有带设备号（改包、脚本直调接口）"},
	{Key: RiskSignalAccountAge, Label: "注册时长", Unit: "小时", Group: "账号", Op: RiskLT, Threshold: 24,
		Hint: "账号注册至今的小时数"},
	{Key: RiskSignalRiskScore, Label: "近 30 天风险分", Unit: "分", Group: "账号", Op: RiskGTE, Threshold: 60,
		Hint: "风控中心近 30 天给这个账号打出的最高分"},
	{Key: RiskSignalBurst, Label: "近 10 分钟抽奖", Unit: "次", Group: "频率", Op: RiskGT, Threshold: 30,
		Hint: "这个人最近 10 分钟在本活动抽了几次，不含这一次"},
	{Key: RiskSignalTodayDraws, Label: "今日抽奖", Unit: "次", Group: "频率", Op: RiskGT, Threshold: 200,
		Hint: "这个人今天在本活动抽了几次，不含这一次"},
	{Key: RiskSignalWinsToday, Label: "今日中奖", Unit: "次", Group: "频率", Op: RiskGT, Threshold: 50,
		Hint: "这个人今天在本活动中了几次（不含谢谢参与）"},
	{Key: RiskSignalLegacy, Label: "设备或网络聚集", Unit: "", Group: "聚集", Op: RiskGT, Hidden: true,
		Hint: "迁移前的记录，只知道命中了同设备或同网络上限"},
}

// RiskSignals 风控信号目录（含只出现在旧记录里的隐藏项）。
func RiskSignals() []RiskSignalSpec { return riskSignals }

// FindRiskSignal 按键取信号。
func FindRiskSignal(key string) (RiskSignalSpec, bool) {
	for _, spec := range riskSignals {
		if spec.Key == key {
			return spec, true
		}
	}
	return RiskSignalSpec{}, false
}

// RiskActionSpec 一种处置的说明。Severity 越大越重，多条命中时取最重的作记录的处置。
type RiskActionSpec struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Hint     string `json:"hint"`
	Severity int    `json:"severity"`
}

var riskActions = []RiskActionSpec{
	{Key: RiskDamp, Label: "降权", Hint: "中奖奖品权重乘以系数，多条相乘", Severity: 1},
	{Key: RiskNoPity, Label: "不触发保底", Hint: "硬保底、软保底、伪随机分布按未连败计算", Severity: 2},
	{Key: RiskRare, Label: "停发稀有", Hint: "公示概率低于设定值的中奖奖品不出", Severity: 3},
	{Key: RiskFloor, Label: "只发兜底", Hint: "兜底奖以外的中奖奖品都不出", Severity: 4},
	{Key: RiskDeny, Label: "拒绝抽奖", Hint: "本次不抽，次数与积分不扣，记一条风控事件", Severity: 5},
}

// RiskActions 处置目录（从轻到重）。
func RiskActions() []RiskActionSpec { return riskActions }

func riskSeverity(action string) int {
	for _, spec := range riskActions {
		if spec.Key == action {
			return spec.Severity
		}
	}
	return 0
}

// ── 评估 ──

// RiskCheck 一条规则在这一次里的评估结果。
type RiskCheck struct {
	Name      string  `json:"name,omitempty"`
	Signal    string  `json:"signal"`
	Op        string  `json:"op"`
	Threshold float64 `json:"threshold"`
	Value     float64 `json:"value"`
	// Missing 取不到这个信号（例如请求没有设备号时数不了同设备账号），按未命中处理
	Missing   bool     `json:"missing,omitempty"`
	Hit       bool     `json:"hit"`
	Action    string   `json:"action"`
	Factor    float64  `json:"factor,omitempty"`
	RareBelow float64  `json:"rareBelow,omitempty"`
	Kinds     []string `json:"kinds,omitempty"`
}

// RiskReport 一次抽奖的风控评估：逐条结果与合并后的处置。
type RiskReport struct {
	Checks []RiskCheck `json:"checks"`
	// Action 命中规则里最重的处置；没命中为空
	Action string `json:"action,omitempty"`
	// Hits 命中的信号（去重，按规则顺序）
	Hits []string `json:"hits,omitempty"`
	// NoPity 保底不触发（noPity、floor、deny 都会）
	NoPity bool `json:"noPity,omitempty"`
}

// Floor 是否只发兜底。
func (r *RiskReport) Floor() bool {
	return r != nil && riskSeverity(r.Action) >= riskSeverity(RiskFloor)
}

// Denied 是否拒绝这一次抽奖。
func (r *RiskReport) Denied() bool { return r != nil && r.Action == RiskDeny }

// RiskValue 信号在这一次里的取值；ok 为假表示取不到。
func RiskValue(signal string, state DrawState) (float64, bool) {
	switch signal {
	case RiskSignalDeviceAccounts:
		return float64(state.DeviceAccounts), !state.DeviceMissing
	case RiskSignalIPAccounts:
		return float64(state.IPAccounts), !state.IPMissing
	case RiskSignalSubnetAccounts:
		return float64(state.SubnetAccounts), !state.IPMissing
	case RiskSignalDeviceHistory:
		return float64(state.DeviceHistory), !state.DeviceMissing
	case RiskSignalAccountDevices:
		return state.Profile.Devices, true
	case RiskSignalNoDevice:
		if state.DeviceMissing {
			return 1, true
		}
		return 0, true
	case RiskSignalAccountAge:
		return math.Round(state.Profile.RegisteredDays*24*10) / 10, true
	case RiskSignalRiskScore:
		return state.Profile.RiskScore30d, true
	case RiskSignalBurst:
		return float64(state.RecentDraws), true
	case RiskSignalTodayDraws:
		return float64(state.TodayDraws), true
	case RiskSignalWinsToday:
		return float64(state.WinsToday), true
	}
	return 0, false
}

func riskCompare(op string, value, threshold float64) bool {
	switch op {
	case RiskGT:
		return value > threshold
	case RiskGTE:
		return value >= threshold
	case RiskLT:
		return value < threshold
	case RiskLTE:
		return value <= threshold
	}
	return false
}

// EvaluateRisk 逐条评估风控规则。没有规则时返回 nil。
func EvaluateRisk(guard *RiskGuard, state DrawState) *RiskReport {
	rules := guard.EffectiveRules()
	if len(rules) == 0 {
		return nil
	}
	report := &RiskReport{Checks: make([]RiskCheck, 0, len(rules))}
	for _, rule := range rules {
		value, ok := RiskValue(rule.Signal, state)
		check := RiskCheck{
			Name: rule.Name, Signal: rule.Signal, Op: rule.Op, Threshold: rule.Threshold, Value: value,
			Missing: !ok, Action: rule.Action, Factor: rule.Factor, RareBelow: rule.RareBelow, Kinds: rule.Kinds,
		}
		check.Hit = ok && riskCompare(rule.Op, value, rule.Threshold)
		if check.Hit {
			if riskSeverity(rule.Action) > riskSeverity(report.Action) {
				report.Action = rule.Action
			}
			if !slices.Contains(report.Hits, rule.Signal) {
				report.Hits = append(report.Hits, rule.Signal)
			}
			if rule.Action == RiskNoPity || rule.Action == RiskFloor || rule.Action == RiskDeny {
				report.NoPity = true
			}
		}
		report.Checks = append(report.Checks, check)
	}
	return report
}

// riskKindApplies 规则的奖品类型范围是否包含这个奖品。
func riskKindApplies(kinds []string, prize Prize) bool {
	return len(kinds) == 0 || slices.Contains(kinds, prize.Kind)
}

// riskPrizeEffect 风控对一个中奖奖品的作用：block 为真时停发（note 说明原因），否则 damp 是要乘的系数（1 为不动）。
// floorID 是兜底奖：只发兜底时它照常可出，降权与停发稀有也不碰它（风控的底线就是兜底奖）。
func riskPrizeEffect(report *RiskReport, prize Prize, baseProbability float64, floorID int64) (block bool, note string, damp float64) {
	damp = 1
	if report == nil || report.Action == "" || !prize.Winning() || prize.ID == floorID {
		return false, "", 1
	}
	var dampNotes []string
	for _, check := range report.Checks {
		if !check.Hit {
			continue
		}
		label := riskCheckLabel(check)
		switch check.Action {
		case RiskFloor, RiskDeny:
			return true, label + "，只发兜底", 1
		case RiskRare:
			if riskKindApplies(check.Kinds, prize) && baseProbability < check.RareBelow {
				return true, fmt.Sprintf("%s，公示概率低于 %s", label, formatShare(check.RareBelow)), 1
			}
		case RiskDamp:
			if riskKindApplies(check.Kinds, prize) {
				damp *= check.Factor
				dampNotes = append(dampNotes, label)
			}
		}
	}
	if damp < 1 {
		return false, strings.Join(dampNotes, "；"), round6(damp)
	}
	return false, "", 1
}

// riskCheckLabel 「同设备今日账号 5 > 3」这样的一句话。
func riskCheckLabel(check RiskCheck) string {
	name := check.Name
	if name == "" {
		if spec, ok := FindRiskSignal(check.Signal); ok {
			name = spec.Label
		} else {
			name = check.Signal
		}
	}
	return fmt.Sprintf("%s %s %s %s", name, trimFloat(check.Value), riskOpSymbol(check.Op), trimFloat(check.Threshold))
}

func riskOpSymbol(op string) string {
	switch op {
	case RiskGT:
		return ">"
	case RiskGTE:
		return "≥"
	case RiskLT:
		return "<"
	case RiskLTE:
		return "≤"
	}
	return op
}

func formatShare(v float64) string {
	return trimFloat(v*100) + "%"
}

// SubnetOf IPv4 取 /24、IPv6 取 /64 的网段（与仓储里数同网段账号的 SQL 同一口径）；解析不了时返回空。
func SubnetOf(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ""
	}
	addr = addr.Unmap().WithZone("")
	bits := 64
	if addr.Is4() {
		bits = 24
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.String()
}

// ── 管理端风控总览 ──

// RiskCount 按处置或信号的计数（Draws 是命中后仍抽了的记录，Denied 是被拒绝的请求）。
type RiskCount struct {
	Key    string `json:"key"`
	Draws  int64  `json:"draws"`
	Denied int64  `json:"denied"`
}

// RiskDay 一天的抽奖、命中与拒绝。
type RiskDay struct {
	Date    string `json:"date"`
	Draws   int64  `json:"draws"`
	Flagged int64  `json:"flagged"`
	Denied  int64  `json:"denied"`
}

// RiskCluster 一个设备 / IP / 网段在时间窗里的聚集情况。
type RiskCluster struct {
	Key      string    `json:"key"`
	Accounts int64     `json:"accounts"`
	Draws    int64     `json:"draws"`
	Flagged  int64     `json:"flagged"`
	LastAt   time.Time `json:"lastAt"`
}

// RiskEvent 一条被拒绝的抽奖请求。
type RiskEvent struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"userId"`
	Account   string      `json:"account"`
	Nickname  string      `json:"nickname,omitempty"`
	Action    string      `json:"action"`
	Rules     []string    `json:"rules"`
	Report    *RiskReport `json:"report,omitempty"`
	Source    string      `json:"source,omitempty"`
	DeviceID  string      `json:"deviceId,omitempty"`
	ClientIP  string      `json:"clientIp,omitempty"`
	CreatedAt time.Time   `json:"createdAt"`
}

// RiskOverview 一个活动最近 Days 天的风控总览。
type RiskOverview struct {
	ActivityID   int64         `json:"activityId"`
	Days         int           `json:"days"`
	Since        time.Time     `json:"since"`
	Draws        int64         `json:"draws"`
	Users        int64         `json:"users"`
	Flagged      int64         `json:"flagged"`
	FlaggedUsers int64         `json:"flaggedUsers"`
	Denied       int64         `json:"denied"`
	Actions      []RiskCount   `json:"actions"`
	Signals      []RiskCount   `json:"signals"`
	Trend        []RiskDay     `json:"trend"`
	Devices      []RiskCluster `json:"devices"`
	IPs          []RiskCluster `json:"ips"`
	Subnets      []RiskCluster `json:"subnets"`
	Events       []RiskEvent   `json:"events"`
	// Rules 活动当前生效的规则（含旧字段转成的）
	Rules []RiskRule `json:"rules"`
	// TookMs 并行查询总耗时
	TookMs int64 `json:"tookMs"`
}
