package middleware

import (
	"strings"
	"testing"

	"aegis/internal/config"

	"github.com/corazawaf/coraza/v3"
	"go.uber.org/zap"
)

// ruleMethodEnforcement CRS 的方法白名单规则（REQUEST-911-METHOD-ENFORCEMENT.conf）
const ruleMethodEnforcement = 911100

// matchedRules 实跑一次 Coraza，返回该方法命中的全部规则 ID。
//
// 断言「命中了哪条规则」而不是「请求是否被中断」：CRS v4 是异常计分模式，
// 911100 只 +5 分（critical_anomaly_score），而本项目把 inbound 阈值放宽到 40，
// 因此单独一个不合规方法根本到不了 949110 的阻断线。若改为断言中断，
// 用例会在阈值默认值变动时给出与方法白名单毫无关系的红/绿。
func matchedRules(t *testing.T, waf coraza.WAF, method string) map[int]bool {
	t.Helper()

	tx := waf.NewTransaction()
	tx.ProcessConnection("::1", 0, "", 0)
	tx.ProcessURI("/api/admin/apps/ffc67368-bf37-423e-80b5-5f2ae9c865fb/captcha-config", method, "HTTP/1.1")
	tx.AddRequestHeader("Host", "localhost")
	tx.AddRequestHeader("Content-Type", "application/json")
	tx.AddRequestHeader("User-Agent", "Mozilla/5.0")
	tx.AddRequestHeader("Accept", "application/json")

	if interruption := tx.ProcessRequestHeaders(); interruption == nil {
		if _, _, err := tx.ReadRequestBodyFrom(strings.NewReader(`{"enabled":true}`)); err != nil {
			t.Fatalf("%s 请求体读取失败: %v", method, err)
		}
		if _, err := tx.ProcessRequestBody(); err != nil {
			t.Fatalf("%s 请求体处理失败: %v", method, err)
		}
	}

	hit := map[int]bool{}
	for _, rule := range tx.MatchedRules() {
		hit[rule.Rule().ID()] = true
	}
	if err := tx.Close(); err != nil {
		t.Fatalf("关闭事务失败: %v", err)
	}
	return hit
}

func newTestWAF(t *testing.T) coraza.WAF {
	t.Helper()
	waf, err := newCorazaWAF(config.FirewallConfig{CorazaEnabled: true}, zap.NewNop())
	if err != nil {
		t.Fatalf("初始化 Coraza 失败: %v", err)
	}
	return waf
}

// CRS 默认只放行 GET/HEAD/POST/OPTIONS，而本平台是 REST 风格
// （路由表里 PUT 70 条、DELETE 75 条、PATCH 3 条）。不覆盖 tx.allowed_methods
// 的话，每个写请求都白吃 5 分异常分（阈值 40 的 12.5%），并刷一条 911100 日志。
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
// 设置动作排到 Include 之后就会被 CRS 的默认值抢先写入而彻底失效，
// 且这种失效在指令串里完全看不出来。
func TestBuildCorazaDirectivesSetsAllowedMethodsBeforeCRS(t *testing.T) {
	directives := buildCorazaDirectives(config.FirewallConfig{})

	setvar := strings.Index(directives, "setvar:tx.allowed_methods=")
	include := strings.Index(directives, "Include @owasp_crs/*.conf")
	if setvar < 0 {
		t.Fatalf("未设置 tx.allowed_methods:\n%s", directives)
	}
	if setvar > include {
		t.Fatalf("tx.allowed_methods 必须设置在 CRS 引入之前，否则不生效:\n%s", directives)
	}
}
