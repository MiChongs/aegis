package middleware

import (
	"net/http"
	"strings"
	"testing"

	"aegis/internal/config"
	fw "aegis/internal/firewall"
)

// ruleMethodEnforcement CRS 的方法白名单规则（REQUEST-911-METHOD-ENFORCEMENT.conf）
const ruleMethodEnforcement = 911100

// matchedRules 实跑一次 WAF，返回该方法命中的全部规则 ID。
//
// 断言「命中了哪条规则」而不是「请求是否被中断」：阈值是可调的，断言中断会让用例
// 在阈值变动时给出与方法白名单毫无关系的红 / 绿。
func matchedRules(t *testing.T, waf *fw.WAF, method string) map[int]bool {
	t.Helper()
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "Mozilla/5.0")
	headers.Set("Accept", "application/json")
	body := `{"enabled":true}`
	txn, res := waf.Begin(fw.Request{RemoteIP: "::1", Method: method, URI: "/api/v1/apps/voyage_main/captcha-config", Host: "localhost", Headers: headers, ContentLength: int64(len(body))})
	defer func() { _ = txn.Close() }()
	if res == nil {
		var err error
		if res, err = txn.RequestBody(strings.NewReader(body)); err != nil {
			t.Fatalf("%s 请求体处理失败: %v", method, err)
		}
	}
	hit := map[int]bool{}
	for _, m := range res.Matches {
		hit[m.RuleID] = true
	}
	return hit
}

func newTestWAF(t *testing.T) *fw.WAF {
	t.Helper()
	waf, err := fw.CompileWAF(firewallOptions(config.NormalizeFirewallConfig(config.FirewallConfig{CorazaEnabled: true}), nil))
	if err != nil {
		t.Fatalf("初始化 WAF 失败: %v", err)
	}
	return waf
}

// CRS 默认只放行 GET/HEAD/POST/OPTIONS，而本平台是 REST 风格
// （路由表里 PUT、DELETE、PATCH 都有）。不覆盖 tx.allowed_methods 的话，
// 每个写请求都白吃 5 分异常分——阈值 5 时就是当场 403。
func TestCorazaDoesNotFlagRESTWriteMethods(t *testing.T) {
	waf := newTestWAF(t)

	for _, method := range strings.Fields(CorazaAllowedMethods) {
		if matchedRules(t, waf, method)[ruleMethodEnforcement] {
			t.Errorf("%s 命中方法白名单规则 %d：每个此类请求都会白吃异常分并刷告警日志",
				method, ruleMethodEnforcement)
		}
	}
}

// 放行集不等于「关掉方法校验」。911100 唯一的价值就是这份白名单，
// 用 SecRuleRemoveById 关掉它会让 TRACE（XST 向量）之类一并不再计分。
func TestCorazaStillFlagsDangerousMethods(t *testing.T) {
	waf := newTestWAF(t)

	for _, method := range []string{"TRACE", "TRACK", "CONNECT"} {
		if !matchedRules(t, waf, method)[ruleMethodEnforcement] {
			t.Errorf("%s 未命中规则 %d：方法白名单形同虚设", method, ruleMethodEnforcement)
		}
	}
}

// CRS 的 901160 用 `&TX:allowed_methods "@eq 0"` 守着默认值 ——
// 设置动作排到 901 之后就会被 CRS 的默认值抢先写入而彻底失效，
// 且这种失效在指令串里完全看不出来。
func TestDirectivesSetAllowedMethodsBeforeCRS(t *testing.T) {
	directives := newTestWAF(t).Directives()
	setvar := strings.Index(directives, "setvar:'tx.allowed_methods=")
	include := strings.Index(directives, "Include @owasp_crs/REQUEST-901-INITIALIZATION.conf")
	if setvar < 0 || include < 0 {
		t.Fatalf("指令缺少放行集或 901:\n%s", directives)
	}
	if setvar > include {
		t.Fatalf("tx.allowed_methods 必须设置在 CRS 引入之前，否则不生效:\n%s", directives)
	}
}
