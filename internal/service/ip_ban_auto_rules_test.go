package service

import (
	"testing"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
)

func TestParseAutoBanRules(t *testing.T) {
	t.Run("空字符串返回 nil", func(t *testing.T) {
		rules, err := ParseAutoBanRules("   ")
		if err != nil || rules != nil {
			t.Fatalf("空输入应返回 (nil, nil)，got (%v, %v)", rules, err)
		}
	})

	t.Run("完整规则解析", func(t *testing.T) {
		raw := `[{"name":"rl_abuse","window":"10m","threshold":120,"banDuration":"1h",
			"severity":"high","reasonFilter":["rate_limited"]}]`
		rules, err := ParseAutoBanRules(raw)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if len(rules) != 1 {
			t.Fatalf("规则数 = %d, want 1", len(rules))
		}
		r := rules[0]
		if r.Name != "rl_abuse" || r.Window != 10*time.Minute || r.Threshold != 120 ||
			r.BanDuration != time.Hour || r.Severity != firewalldomain.SeverityHigh {
			t.Fatalf("规则字段不符: %+v", r)
		}
		if len(r.ReasonFilter) != 1 || r.ReasonFilter[0] != "rate_limited" {
			t.Fatalf("ReasonFilter 不符: %v", r.ReasonFilter)
		}
	})

	t.Run("banDuration 为 0 表示永久", func(t *testing.T) {
		raw := `[{"name":"perma","window":"1h","threshold":10,"banDuration":"0"}]`
		rules, err := ParseAutoBanRules(raw)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if rules[0].BanDuration != 0 {
			t.Fatalf("BanDuration = %v, want 0（永久）", rules[0].BanDuration)
		}
		if rules[0].Severity != firewalldomain.SeverityMedium {
			t.Fatalf("缺省 severity 应为 medium，got %s", rules[0].Severity)
		}
	})

	t.Run("非法输入逐项拒绝", func(t *testing.T) {
		bad := []string{
			`{`,                                // 非法 JSON
			`[{"window":"10m","threshold":1}]`, // 缺 name
			`[{"name":"x","window":"abc","threshold":1}]`,                      // window 非法
			`[{"name":"x","window":"10m","threshold":0}]`,                      // threshold 非法
			`[{"name":"x","window":"10m","threshold":1,"severity":"extreme"}]`, // severity 非法
			`[{"name":"x","window":"10m","threshold":1,"banDuration":"oops"}]`, // banDuration 非法
		}
		for _, raw := range bad {
			if _, err := ParseAutoBanRules(raw); err == nil {
				t.Fatalf("应拒绝非法输入: %s", raw)
			}
		}
	})
}

func TestDefaultAutoBanRulesContainRateLimitEscalation(t *testing.T) {
	idxRateLimit, idxHighFreq := -1, -1
	for i, r := range defaultAutoBanRules {
		switch r.Name {
		case "rate_limit_abuse":
			idxRateLimit = i
			if len(r.ReasonFilter) != 1 || r.ReasonFilter[0] != "rate_limited" {
				t.Fatalf("rate_limit_abuse 应仅统计 rate_limited 原因: %v", r.ReasonFilter)
			}
			if r.BanDuration <= 0 {
				t.Fatal("rate_limit_abuse 应为临时封禁（BanDuration > 0）")
			}
		case "high_frequency_block":
			idxHighFreq = i
		}
	}
	if idxRateLimit == -1 {
		t.Fatal("默认规则缺少 rate_limit_abuse")
	}
	if idxHighFreq != -1 && idxRateLimit > idxHighFreq {
		t.Fatal("rate_limit_abuse 应排在 high_frequency_block 之前（从严到宽）")
	}
}
