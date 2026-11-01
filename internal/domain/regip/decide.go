package regip

import (
	"fmt"
	"strings"
)

// 判定结论。
const (
	OutcomeAllow     = "allow"     // 放行
	OutcomeChallenge = "challenge" // 要求先完成图形验证码
	OutcomeReject    = "reject"    // 拒绝
)

// 结论的原因，也是拦截记录的 reason。
const (
	ReasonNone          = ""
	ReasonDisabled      = "disabled"       // 没开启
	ReasonMissingIP     = "missing_ip"     // 拿不到注册 IP
	ReasonMissingDevice = "missing_device" // 没上报设备标识
	ReasonInvalidDevice = "invalid_device" // 设备标识无效
	ReasonListAllow     = "list_allow"     // 命中放行名单
	ReasonListDeny      = "list_deny"      // 命中拒绝名单
	ReasonPenalty       = "penalty"        // 惩罚期内
	ReasonGeo           = "geo"            // 属地限制
	ReasonISP           = "isp"            // 运营商限制
	ReasonSurge         = "surge"          // 注册量突增
	ReasonRule          = "rule"           // 配额规则
	ReasonFailure       = "failure"        // 计数存储出错且策略要求拒绝
)

// Facts 一次判定需要的全部事实。线上由 RegisterIPService 取齐，模拟时可以部分虚构。
type Facts struct {
	Method string
	// Address 解析出的地址；Valid() 为假表示缺少或无法解析
	Address Address
	// Device 上报的设备标识
	Device Device
	// CaptchaPassed 这次请求已经通过了图形验证码（风控加验或应用自己要求的都算）
	CaptchaPassed bool
	// List 地址名单与设备名单合并后的结论（CombineLists）
	List     *ListMatch
	Location Location
	// PenaltySeconds 地址一侧惩罚期剩余秒数，0 表示不在惩罚期
	PenaltySeconds int
	// DevicePenaltySeconds 设备一侧惩罚期剩余秒数
	DevicePenaltySeconds int
	Surge                SurgeState
	// Counts 规则 ID → 该规则统计主体里已有的注册数（不含这一次）。缺失表示这次无从统计
	Counts map[string]int
	// StoreFailed 计数存储出错，Counts 不可信
	StoreFailed bool
}

// RuleHit 一条规则这一次的判定细节。
type RuleHit struct {
	RuleID        string `json:"ruleId"`
	Name          string `json:"name"`
	Scope         string `json:"scope"`
	Subject       string `json:"subject"`
	WindowSeconds int    `json:"windowSeconds"`
	Action        string `json:"action"`
	Count         int    `json:"count"`
	Limit         int    `json:"limit"`
	// Configured 规则里写的上限；Limit 是放宽 / 收紧之后的
	Configured int  `json:"configured"`
	Exceeded   bool `json:"exceeded"`
	// Skipped 这条规则这次不参与（不约束这种注册方式、属地查不到、没有计数）
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skipReason,omitempty"`
}

// Decision 判定结果。
type Decision struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	// Observed 观察模式下「本该」的结论：Outcome 恒为 allow，这里记下本该拒绝还是加验
	Observed string `json:"observed,omitempty"`
	// RuleID / RuleName 由哪条规则得出（reason=rule 时）
	RuleID   string `json:"ruleId,omitempty"`
	RuleName string `json:"ruleName,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Message  string `json:"message,omitempty"`
	// RetryAfter 建议多少秒后再试（惩罚期剩余时长等），0 表示无从估计
	RetryAfter int       `json:"retryAfter,omitempty"`
	Rules      []RuleHit `json:"rules"`
	// Factor 名单放宽倍数；Tighten 突增收紧系数
	Factor  float64 `json:"factor,omitempty"`
	Tighten float64 `json:"tighten,omitempty"`
	// ChallengePassed 有规则要求加验，而这次已经过了验证码
	ChallengePassed bool `json:"challengePassed,omitempty"`
}

// Blocking 这个结论会不会挡住注册（观察模式恒为否）。
func (d Decision) Blocking() bool { return d.Outcome != OutcomeAllow }

// Notable 值不值得记一条拦截记录：挡住了、观察到了，或加验通过。
func (d Decision) Notable() bool {
	return d.Blocking() || d.Observed != "" || d.ChallengePassed || d.Reason == ReasonListDeny
}

// RuleBlocks 规则超限时会不会挡住这次注册。Redis 脚本据此决定「都没超才占位」：
// 加验型规则在已过验证码时不挡，观察型规则与观察模式从来不挡。
func RuleBlocks(policy Policy, rule Rule, captchaPassed bool) bool {
	if policy.Mode != ModeEnforce {
		return false
	}
	switch rule.Action {
	case ActionReject:
		return true
	case ActionChallenge:
		return !captchaPassed
	}
	return false
}

// PreDecide 只用不需要计数的事实做前置判定。返回 final=true 表示已经有结论，不必再去计数。
//
// 顺序即优先级：缺 IP → 设备标识质量 → 名单（地址与设备合并）→ 惩罚期（地址或设备）→ 属地 → 运营商 → 突增。
// 地址与设备互为补充：缺了一边，只依赖那一边的规则不参与，另一边照常判；两边都缺时无从计数。
func PreDecide(policy Policy, facts Facts) (Decision, bool) {
	d := Decision{Outcome: OutcomeAllow, Rules: []RuleHit{}}
	if !policy.Enabled {
		d.Reason = ReasonDisabled
		return d, true
	}
	if !facts.Address.Valid() && policy.MissingIP == MissingIPReject {
		return finalize(policy, d, OutcomeReject, ReasonMissingIP, "缺少注册 IP", "当前应用要求提供注册 IP")
	}
	// step 落一个前置项的处置：挡住了就是最终结论；观察或加验已过只留痕，继续往下判
	step := func(action, reason, detail, message string) bool {
		out, final := applyAction(policy, d, action, reason, detail, message, facts.CaptchaPassed)
		d = out
		return final
	}
	switch dev := facts.Device; {
	case dev.Invalid && policy.Device.Invalid != DeviceAllow:
		if step(policy.Device.Invalid, ReasonInvalidDevice, "设备标识无效："+dev.InvalidReason, "当前设备标识无效，请更新客户端后重试") {
			return d, true
		}
	case !dev.Present() && policy.Device.Missing != DeviceAllow:
		if step(policy.Device.Missing, ReasonMissingDevice, "没有上报设备标识", "当前应用要求提供设备标识，请更新客户端后重试") {
			return d, true
		}
	}
	if !facts.Address.Valid() && !facts.Device.Valid {
		if d.Reason == ReasonNone {
			d.Reason = ReasonMissingIP
			d.Detail = "缺少注册 IP 与有效设备标识，无从计数，按策略放行"
		}
		return d, true
	}
	if facts.List != nil {
		target := facts.List.CIDR
		if facts.List.Target == ListTargetDevice {
			target = "设备 " + facts.List.Device
		}
		switch facts.List.Kind {
		case ListAllow:
			d.Reason = ReasonListAllow
			d.Detail = "命中放行名单 " + target
			return d, true
		case ListDeny:
			message := "当前网络环境暂不允许注册"
			if facts.List.Target == ListTargetDevice {
				message = "当前设备暂不允许注册"
			}
			return finalize(policy, d, OutcomeReject, ReasonListDeny, "命中拒绝名单 "+target, message)
		case ListElevated:
			d.Factor = facts.List.Factor
		}
	}
	if seconds := max(facts.PenaltySeconds, facts.DevicePenaltySeconds); seconds > 0 {
		d.RetryAfter = seconds
		side, who := "地址", "当前网络"
		if facts.DevicePenaltySeconds >= facts.PenaltySeconds {
			side, who = "设备", "当前设备"
		}
		return finalize(policy, d, OutcomeReject, ReasonPenalty,
			fmt.Sprintf("%s在惩罚期内，剩余 %s", side, DurationLabel(seconds)),
			fmt.Sprintf("%s注册过于频繁，请 %s后再试", who, DurationLabel(seconds)))
	}
	if policy.Geo.Mode != GeoOff {
		matched := ""
		for _, region := range policy.Geo.Regions {
			if facts.Location.MatchRegion(region) {
				matched = region
				break
			}
		}
		blocked := false
		switch {
		case !facts.Location.Resolved:
			blocked = policy.Geo.Unknown == MissingIPReject &&
				step(policy.Geo.Action, ReasonGeo, "属地未知", "当前网络环境暂不允许注册")
		case policy.Geo.Mode == GeoAllow && matched == "":
			blocked = step(policy.Geo.Action, ReasonGeo, "属地 "+facts.Location.CityKey()+" 不在放行名单内", "当前地区暂不开放注册")
		case policy.Geo.Mode == GeoDeny && matched != "":
			blocked = step(policy.Geo.Action, ReasonGeo, "属地命中拒绝项 "+matched, "当前地区暂不开放注册")
		}
		if blocked {
			return d, true
		}
	}
	if policy.ISP.Enabled {
		if keyword, ok := MatchISP(facts.Location.ISP, policy.ISP.Keywords); ok &&
			step(policy.ISP.Action, ReasonISP, "运营商「"+facts.Location.ISP+"」命中关键词 "+keyword, "当前网络环境暂不允许注册") {
			return d, true
		}
	}
	if policy.Surge.Enabled && facts.Surge.Active {
		d.Tighten = policy.Surge.TightenFactor
		detail := fmt.Sprintf("注册量突增：最近 %d 分钟 %d 个，期望 %.1f", policy.Surge.WindowMinutes, facts.Surge.Current, facts.Surge.Expected)
		if step(policy.Surge.Action, ReasonSurge, detail, "注册人数过多，请稍后再试") {
			return d, true
		}
	}
	return d, false
}

// applyAction 把一个前置项的处置落到结论上。final=true 表示这一项已经挡住注册（或观察模式下记了一笔且无须再看后面）。
// 观察型处置与已过验证码的加验不挡，只留痕后继续往下判。
func applyAction(policy Policy, d Decision, action, reason, detail, message string, captchaPassed bool) (Decision, bool) {
	switch action {
	case ActionObserve:
		if d.Observed == "" {
			d.Observed = OutcomeReject
			d.Reason, d.Detail = reason, detail
		}
		return d, false
	case ActionChallenge:
		if captchaPassed {
			d.ChallengePassed = true
			if d.Reason == ReasonNone {
				d.Reason, d.Detail = reason, detail+"（已通过验证码）"
			}
			return d, false
		}
		out, _ := finalize(policy, d, OutcomeChallenge, reason, detail, "当前环境需要先完成图形验证码")
		return out, out.Blocking()
	default:
		out, _ := finalize(policy, d, OutcomeReject, reason, detail, message)
		return out, out.Blocking()
	}
}

// finalize 落一个阻断性结论。观察模式下改成放行，并把本该的结论记进 Observed。
func finalize(policy Policy, d Decision, outcome, reason, detail, message string) (Decision, bool) {
	d.Reason, d.Detail = reason, detail
	if policy.Mode == ModeObserve && reason != ReasonMissingIP {
		d.Outcome = OutcomeAllow
		if d.Observed == "" {
			d.Observed = outcome
		}
		d.Message = ""
		return d, true
	}
	d.Outcome = outcome
	d.Message = message
	return d, true
}

// Decide 完整判定：前置判定没有结论时，按计数逐条看配额规则。
//
// 多条规则同时超限时，拒绝优先于加验，加验优先于观察；同一档里取第一条（规则顺序即管理员的优先级）。
func Decide(policy Policy, facts Facts) Decision {
	d, final := PreDecide(policy, facts)
	if final {
		// 放行名单、观察模式的前置结论照样带上各规则的计数，模拟与 IP 查询里看得到「如果不放行会怎样」
		if !d.Blocking() && facts.Counts != nil && facts.Address.Valid() {
			d.Rules = describeRules(policy, facts, d)
		}
		return d
	}
	if facts.StoreFailed {
		d.Rules = describeRules(policy, facts, d)
		if policy.FailMode == FailClosed {
			out, _ := finalize(policy, d, OutcomeReject, ReasonFailure, "计数存储不可用，按策略拒绝", "注册服务繁忙，请稍后再试")
			return out
		}
		if d.Reason == ReasonNone {
			d.Reason, d.Detail = ReasonFailure, "计数存储不可用，按策略放行"
		}
		return d
	}
	d.Rules = describeRules(policy, facts, d)
	var reject, challenge, observe *RuleHit
	for i := range d.Rules {
		hit := &d.Rules[i]
		if hit.Skipped || !hit.Exceeded {
			continue
		}
		switch {
		case hit.Action == ActionReject && reject == nil:
			reject = hit
		case hit.Action == ActionChallenge && challenge == nil:
			challenge = hit
		case hit.Action == ActionObserve && observe == nil:
			observe = hit
		}
	}
	pick := func(hit *RuleHit, outcome, message string) Decision {
		d.RuleID, d.RuleName = hit.RuleID, hit.Name
		verb := "已注册"
		if DistinctScope(hit.Scope) {
			verb = "已有"
		}
		detail := fmt.Sprintf("%s：%s %s%s %d 个，上限 %d", hit.Name, ScopeLabel(hit.Scope), WindowLabel(hit.WindowSeconds), verb, hit.Count, hit.Limit)
		if hit.WindowSeconds > 0 {
			d.RetryAfter = hit.WindowSeconds
		}
		out, _ := finalize(policy, d, outcome, ReasonRule, detail, message)
		return out
	}
	switch {
	case reject != nil:
		return pick(reject, OutcomeReject, RejectMessage(policy, *reject))
	case challenge != nil && !facts.CaptchaPassed:
		return pick(challenge, OutcomeChallenge, "当前网络注册较多，请先完成图形验证码")
	case challenge != nil:
		d.ChallengePassed = true
		if d.Reason == ReasonNone {
			d.RuleID, d.RuleName = challenge.RuleID, challenge.Name
			d.Reason, d.Detail = ReasonRule, challenge.Name+"超限，已通过验证码"
		}
	case observe != nil && d.Observed == "":
		d.RuleID, d.RuleName = observe.RuleID, observe.Name
		d.Observed = OutcomeReject
		d.Reason = ReasonRule
		d.Detail = fmt.Sprintf("%s：已注册 %d 个，上限 %d（观察）", observe.Name, observe.Count, observe.Limit)
	}
	return d
}

func describeRules(policy Policy, facts Facts, d Decision) []RuleHit {
	hits := make([]RuleHit, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		if !rule.Enabled {
			continue
		}
		hit := RuleHit{
			RuleID: rule.ID, Name: rule.Name, Scope: rule.Scope, WindowSeconds: rule.WindowSeconds,
			Action: rule.Action, Configured: rule.Limit,
			Limit:   EffectiveLimit(rule.Limit, d.Factor, d.Tighten),
			Subject: SubjectKey(rule.Scope, facts.Address, facts.Device, facts.Location),
		}
		member := MemberKey(rule.Scope, facts.Address, facts.Device)
		switch count, ok := facts.Counts[rule.ID]; {
		case !rule.Applies(facts.Method):
			hit.Skipped, hit.SkipReason = true, "不约束这种注册方式"
		case hit.Subject == "" || DistinctScope(rule.Scope) && member == "":
			hit.Skipped, hit.SkipReason = true, skipReason(rule.Scope, facts)
		case !ok:
			hit.Skipped, hit.SkipReason = true, "没有计数"
		default:
			hit.Count = count
			hit.Exceeded = count >= hit.Limit
		}
		hits = append(hits, hit)
	}
	return hits
}

func skipReason(scope string, facts Facts) string {
	switch {
	case scope == ScopeCity:
		return "属地未知，无从统计"
	case DeviceScope(scope) && !facts.Device.Valid, scope == ScopeIPDevices && !facts.Device.Valid:
		return "没有有效设备标识，无从统计"
	case !facts.Address.Valid():
		return "缺少注册 IP，无从统计"
	}
	return "无从统计"
}

// RejectMessage 配额拒绝的提示：管理员配了文案就用它（{limit}、{window} 占位），否则用默认。
func RejectMessage(policy Policy, hit RuleHit) string {
	if policy.RejectMessage != "" {
		return strings.NewReplacer("{limit}", fmt.Sprint(hit.Limit), "{window}", WindowLabel(hit.WindowSeconds)).Replace(policy.RejectMessage)
	}
	switch hit.Scope {
	case ScopeApp:
		return "注册人数已达上限，请稍后再试"
	case ScopeIPDevices:
		return "当前网络下注册的设备过多，请稍后再试"
	case ScopeDeviceIPs:
		return "当前设备更换网络过于频繁，请稍后再试"
	case ScopeDevice:
		if hit.WindowSeconds == 0 {
			return fmt.Sprintf("当前设备注册的账号数已达上限（%d 个）", hit.Limit)
		}
		return fmt.Sprintf("当前设备%s最多注册 %d 个账号，请稍后再试", WindowLabel(hit.WindowSeconds), hit.Limit)
	}
	if hit.WindowSeconds == 0 {
		return fmt.Sprintf("当前网络注册的账号数已达上限（%d 个）", hit.Limit)
	}
	return fmt.Sprintf("当前网络%s最多注册 %d 个账号，请稍后再试", WindowLabel(hit.WindowSeconds), hit.Limit)
}

// DurationLabel 秒数的中文说法：「45 秒」「12 分钟」「3 小时」「2 天」。
func DurationLabel(seconds int) string {
	switch {
	case seconds < 60:
		return fmt.Sprintf("%d 秒", max(seconds, 1))
	case seconds < 3600:
		return fmt.Sprintf("%d 分钟", (seconds+59)/60)
	case seconds < 86400:
		return fmt.Sprintf("%d 小时", (seconds+3599)/3600)
	}
	return fmt.Sprintf("%d 天", (seconds+86399)/86400)
}
