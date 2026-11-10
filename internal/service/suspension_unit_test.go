package service

import (
	"strings"
	"testing"

	"aegis/internal/domain/suspension"
)

func TestSuspensionAutoDecision(t *testing.T) {
	auto := suspension.DefaultSettings()
	auto.AutoReview = suspension.AutoReviewAuto
	cases := []struct {
		name     string
		settings suspension.Settings
		verdict  string
		conf     float64
		level    string
		manual   bool
		want     string
	}{
		{"像误判且模型很确定：自动通过", auto, "approve", 0.9, suspension.RiskWeak, false, "approve"},
		{"置信度不够：转人工", auto, "approve", 0.7, suspension.RiskWeak, false, "escalate"},
		{"风控证据强：不自动通过", auto, "approve", 0.99, suspension.RiskStrong, false, "escalate"},
		{"管理员手动封的：默认不自动通过", auto, "approve", 0.99, suspension.RiskWeak, true, "escalate"},
		{"证据强且模型驳回：自动驳回", auto, "reject", 0.95, suspension.RiskStrong, true, "reject"},
		{"证据不强时不自动驳回", auto, "reject", 0.99, suspension.RiskMedium, false, "escalate"},
		{"只给建议的模式从不自动结案", suspension.DefaultSettings(), "approve", 1, suspension.RiskWeak, false, "escalate"},
	}
	manualOK := auto
	manualOK.AutoApproveManual = true
	cases = append(cases, struct {
		name     string
		settings suspension.Settings
		verdict  string
		conf     float64
		level    string
		manual   bool
		want     string
	}{"放开手动封禁后可自动通过", manualOK, "approve", 0.9, suspension.RiskMedium, true, "approve"})
	for _, c := range cases {
		if got := autoDecision(c.settings, c.verdict, c.conf, c.level, c.manual); got != c.want {
			t.Errorf("%s：得到 %s，应为 %s", c.name, got, c.want)
		}
	}
}

// 四类通知在五种语言里都有完整文案，占位都被替换
func TestSuspensionNoticeCopy(t *testing.T) {
	vars := map[string]string{"app": "Voyage", "account": "alice", "id": "#9", "date": "2026-11-12", "until": "2026-12-01"}
	for _, kind := range []string{suspension.NoticeSuspended, suspension.NoticeReinstated, suspension.NoticeAppealReceived, suspension.NoticeAppealUpheld} {
		for _, locale := range suspension.Locales {
			c := suspension.Notice(kind, locale, vars)
			all := strings.Join(append([]string{c.Subject, c.Title, c.Lead, c.Footer, c.SignOff, c.Greeting}, c.Body...), "\n")
			if c.Subject == "" || c.Title == "" || c.Lead == "" || len(c.Body) == 0 {
				t.Errorf("%s/%s 文案不完整：%+v", kind, locale, c)
			}
			if strings.Contains(all, "{") || strings.Contains(all, "·") {
				t.Errorf("%s/%s 有没替换的占位或禁用符号：%s", kind, locale, all)
			}
		}
	}
	if suspension.Notice(suspension.NoticeSuspended, suspension.LocaleEN, vars).Subject != "Your account has been suspended" {
		t.Fatalf("英文封禁通知标题应为 Your account has been suspended")
	}
	for _, code := range []string{"policy", "fraud", "spam", "abuse", "security", "payment", "other", "unknown"} {
		for _, locale := range suspension.Locales {
			if r := suspension.Reason(code, locale); r.Title == "" || r.Description == "" {
				t.Errorf("原因 %s/%s 缺文案", code, locale)
			}
		}
	}
}
