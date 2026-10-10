package adpolicy

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	toolIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,63}$`)
	placementIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)
	eventIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
)

// Verdict 一个用户在当前策略下的处境。
type Verdict struct {
	Mode             string
	DecisionRequired bool
	Exempt           bool
}

// Evaluate 判定服务模式。
//
// 只有「按当前版本条款同意过」才算同意：按旧版本同意的人在重新选择之前也只有基础服务，
// 否则调高版本就失去了意义。会员免除时不受同意要求约束，但 DecisionRequired 照实回答 ——
// 会员看激励广告同样要先同意，只是不必在启动时追着问。
func Evaluate(policy Policy, consent *Consent, vip bool) Verdict {
	verdict := Verdict{Exempt: vip && policy.VipExempt}
	current := consent != nil && consent.Version >= policy.ConsentVersion
	verdict.DecisionRequired = !current
	switch {
	case !policy.Enabled, verdict.Exempt, current && consent.Accepted:
		verdict.Mode = ModeFull
	default:
		verdict.Mode = ModeBasic
	}
	return verdict
}

// Sources 选择的全部来源。
func Sources() []string {
	return []string{SourceApp, SourceWeb, SourceGuestSync}
}

// ValidSource 来源是否合法。
func ValidSource(source string) bool {
	return slices.Contains(Sources(), source)
}

// SplashStatuses 开屏记录的全部结局。
func SplashStatuses() []string {
	return []string{SplashShown, SplashClicked, SplashFailed, SplashTimeout}
}

// ValidSplashStatus 结局是否合法。
func ValidSplashStatus(status string) bool {
	return slices.Contains(SplashStatuses(), status)
}

// SplashDisplayed 这次开屏是否真的展示了（频控只数展示过的）。
func SplashDisplayed(status string) bool {
	return status == SplashShown || status == SplashClicked
}

// ValidEventID 客户端生成的记录标识。
func ValidEventID(eventID string) bool {
	return eventIDPattern.MatchString(eventID)
}

// NormalizeBasicTools 去空白、转小写、去重，保持配置时的顺序。
func NormalizeBasicTools(tools []string) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.ToLower(strings.TrimSpace(tool))
		if tool == "" || slices.Contains(out, tool) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// NormalizePolicy 清洗管理端提交的策略，不做合法性判断。
func NormalizePolicy(input SavePolicyInput) Policy {
	policy := Policy{
		AppID:          input.AppID,
		Enabled:        input.Enabled,
		ConsentVersion: input.ConsentVersion,
		PolicyURL:      strings.TrimSpace(input.PolicyURL),
		VipExempt:      input.VipExempt,
		BasicTools:     NormalizeBasicTools(input.BasicTools),
		Splash:         input.Splash,
		UpdatedBy:      input.Operator,
	}
	policy.Splash.PlacementID = strings.TrimSpace(policy.Splash.PlacementID)
	if policy.ConsentVersion == 0 {
		policy.ConsentVersion = 1
	}
	return policy
}

// ValidatePolicy 保存前的合法性检查。
func ValidatePolicy(policy Policy) error {
	if policy.ConsentVersion < 1 || policy.ConsentVersion > MaxConsentVersion {
		return fmt.Errorf("条款版本需在 1–%d 之间", MaxConsentVersion)
	}
	if policy.PolicyURL != "" {
		if utf8.RuneCountInString(policy.PolicyURL) > 512 {
			return fmt.Errorf("广告服务条款链接过长")
		}
		parsed, err := url.Parse(policy.PolicyURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return fmt.Errorf("广告服务条款链接需为 http(s) 地址")
		}
	}
	if len(policy.BasicTools) > MaxBasicTools {
		return fmt.Errorf("基础服务最多 %d 项", MaxBasicTools)
	}
	for _, tool := range policy.BasicTools {
		if !toolIDPattern.MatchString(tool) {
			return fmt.Errorf("功能标识「%s」无效：需为小写字母、数字与 _ . : -，最长 64 位", tool)
		}
	}
	if !placementIDPattern.MatchString(policy.Splash.PlacementID) {
		return fmt.Errorf("开屏广告位 ID 需为 64 位以内的字母、数字、_ 或 -")
	}
	if policy.Splash.MinIntervalSeconds < 0 || policy.Splash.MinIntervalSeconds > MaxSplashIntervalSeconds {
		return fmt.Errorf("开屏间隔需在 0–%d 秒之间", MaxSplashIntervalSeconds)
	}
	if policy.Splash.DailyLimit < 0 || policy.Splash.DailyLimit > MaxSplashDailyLimit {
		return fmt.Errorf("开屏每日次数需在 0–%d 之间", MaxSplashDailyLimit)
	}
	return nil
}

// SplashVerdict 开屏广告现在能不能展示，以及间隔未到时最早什么时候可以。
//
// 同意是展示的前提：没同意时广告 SDK 根本不会初始化，这里回答 false 只是与客户端口径一致。
func SplashVerdict(cfg SplashConfig, usage SplashUsage, accepted, exempt bool, now time.Time) (bool, *time.Time) {
	if !cfg.Enabled || exempt || !accepted {
		return false, nil
	}
	if cfg.DailyLimit > 0 && usage.TodayCount >= cfg.DailyLimit {
		return false, nil
	}
	if cfg.MinIntervalSeconds > 0 && usage.LastShownAt != nil {
		next := usage.LastShownAt.Add(time.Duration(cfg.MinIntervalSeconds) * time.Second)
		if now.Before(next) {
			nextUTC := next.UTC()
			return false, &nextUTC
		}
	}
	return true, nil
}

// ClampOccurredAt 客户端报来的发生时间：缺失、太早或在未来的都按收到的时间算。
func ClampOccurredAt(occurred *time.Time, now time.Time) time.Time {
	if occurred == nil || occurred.IsZero() {
		return now
	}
	if occurred.Before(now.Add(-SplashEventMaxAge)) || occurred.After(now.Add(5*time.Minute)) {
		return now
	}
	return *occurred
}

// TruncateRunes 截断到给定字符数，避免超出列宽。
func TruncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
