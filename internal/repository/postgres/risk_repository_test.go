package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	securitydomain "aegis/internal/domain/security"
)

// TestNormalizeAssessmentShape 钉住「JSONB 列永远不写标量」。
//
// 回归背景：nil 切片的 json.Marshal 结果是 `null` 而不是 `[]`。这样的一行
// 落库之后，大盘里的 jsonb_array_elements(matched_rules) 会抛
// 22023 cannot extract elements from a scalar —— 一条脏数据让整个风控大盘 500。
// 列上的 DEFAULT '[]' 挡不住显式写入的 null，所以必须在写入入口收敛。
func TestNormalizeAssessmentShape(t *testing.T) {
	assessment := securitydomain.RiskAssessment{} // MatchedRules / EvalContext 均为 nil
	normalizeAssessmentShape(&assessment)

	rules, err := json.Marshal(assessment.MatchedRules)
	if err != nil {
		t.Fatalf("marshal matched_rules: %v", err)
	}
	if string(rules) != "[]" {
		t.Fatalf("matched_rules 必须序列化成空数组，实际为 %s", rules)
	}

	context, err := json.Marshal(assessment.EvalContext)
	if err != nil {
		t.Fatalf("marshal eval_context: %v", err)
	}
	if string(context) != "{}" {
		t.Fatalf("eval_context 必须序列化成空对象，实际为 %s", context)
	}
}

// TestNormalizeAssessmentShapeKeepsData 收敛不能顺手清空已有内容。
func TestNormalizeAssessmentShapeKeepsData(t *testing.T) {
	assessment := securitydomain.RiskAssessment{
		MatchedRules: []securitydomain.MatchedRule{{RuleID: 7, RuleName: "A", Score: 20}},
		EvalContext:  map[string]any{"ip": "203.0.113.7"},
	}
	normalizeAssessmentShape(&assessment)

	if len(assessment.MatchedRules) != 1 || assessment.MatchedRules[0].RuleID != 7 {
		t.Fatalf("已有命中记录被改动：%+v", assessment.MatchedRules)
	}
	if assessment.EvalContext["ip"] != "203.0.113.7" {
		t.Fatalf("已有环境快照被改动：%+v", assessment.EvalContext)
	}
}

// TestMatchedRulesArrayGuardsScalars 展开前必须先判类型。
//
// 存量数据里已经躺着标量 null 的行（迁移 000070 会修，但迁移可能还没跑，
// 也可能有别的写入路径漏网）。查询层这一道防御不能被"顺手简化"掉。
func TestMatchedRulesArrayGuardsScalars(t *testing.T) {
	if !strings.Contains(matchedRulesArray, "jsonb_typeof") {
		t.Fatal("matchedRulesArray 必须先判 jsonb_typeof 再展开，否则标量行会让查询整体 22023")
	}
	if !strings.Contains(matchedRulesArray, "'[]'::jsonb") {
		t.Fatal("非数组的行必须退化成空数组，而不是让整条查询失败")
	}
}

// TestNormalizeBucketWhitelist date_trunc 的单位不能参数化，只能拼进 SQL，
// 因此必须收敛成白名单 —— 否则这里就是一个注入点。
func TestNormalizeBucketWhitelist(t *testing.T) {
	cases := map[string]string{
		"day": "day", "DAY": "day",
		"hour": "hour", "": "hour",
		"minute":                                "hour",
		"day'; DROP TABLE risk_assessments; --": "hour",
	}
	for input, want := range cases {
		if got := normalizeBucket(input); got != want {
			t.Errorf("normalizeBucket(%q) = %q，期望 %q", input, got, want)
		}
	}
}
