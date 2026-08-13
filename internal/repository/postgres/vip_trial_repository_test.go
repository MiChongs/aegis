package postgres

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 会员判定事实那条 SQL 的可空侧必须逐列兜住 NULL。
//
// 它的两个 LEFT JOIN 对**绝大多数用户都不命中**：没领过试用（`c`）、
// 还没有过任何一次开通（`t`）。不命中时那一侧的每一列都是 NULL，
// 漏兜一列的表现是运行期 `cannot scan NULL into *string` ——
// 而这条链路同时是 `/vip/status`、管理端 `/vip/entitlement` 与远程函数
// `aegis.user.get()` 的唯一入口，一列没兜住等于这三处对新用户全部报错。
//
// 编译期查不出这件事（`*string` 与 `**string` 都是合法的 Scan 目标），
// 开发机上也未必撞得到（随手建的测试账号往往恰好领过试用），所以在这里钉住：
// 可空侧的列**要么** COALESCE，**要么**在下面这张表里登记成"用指针接"。
func TestVipEntitlementFactsNullableColumnsAreNullSafe(t *testing.T) {
	// 允许裸列的，只有 GetVipEntitlementFacts 里真的用指针接收的那几个。
	// 改动查询时同步改这里 —— 加一列却不登记，测试会直接指出是哪一列。
	pointerScanned := map[string]bool{
		"c.id":            true, // *int64，同时是"这个人领没领过"的判据
		"c.plan_id":       true, // *int64
		"c.trial_ends_at": true, // *time.Time
		"c.created_at":    true, // *time.Time
		"t.pay_channel":   true, // *string
		"t.plan_name":     true, // *string
	}

	cases := []struct {
		name           string
		sql            string
		nullableAlias  []string
		expectPointers []string
	}{
		{
			name:           "GetVipEntitlementFacts",
			sql:            vipEntitlementFactsSQL,
			nullableAlias:  []string{"c", "t"},
			expectPointers: []string{"c.created_at", "c.id", "c.plan_id", "c.trial_ends_at", "t.pay_channel", "t.plan_name"},
		},
		{
			name:           "entitlementFactsTx",
			sql:            vipEntitlementFactsTxSQL,
			nullableAlias:  []string{"t"},
			expectPointers: []string{"t.pay_channel", "t.plan_name"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bare := bareAliasColumns(t, tc.sql, tc.nullableAlias)
			for _, col := range bare {
				if !pointerScanned[col] {
					t.Errorf("%s 取自可空的 LEFT JOIN 一侧，却既没 COALESCE 也没登记成指针接收 —— "+
						"没领过试用 / 还没开通过会员的用户会在这里 scan NULL 失败", col)
				}
			}
			if got := strings.Join(bare, ","); got != strings.Join(tc.expectPointers, ",") {
				t.Errorf("可空侧的裸列集合变了：期望 [%s]，实际 [%s]。"+
					"新增裸列要确认 Scan 目标是指针，删除裸列要同步这张表", strings.Join(tc.expectPointers, ","), got)
			}
		})
	}
}

// bareAliasColumns 取 SELECT 列表里未被 COALESCE 兜住的 `alias.column`。
//
// 只看 SELECT 与 FROM 之间：ON / WHERE 里引用可空侧是正常的（NULL 参与比较
// 得到 NULL，行被过滤掉而已），出事的只有被 Scan 读走的那些列。
func bareAliasColumns(t *testing.T, sql string, aliases []string) []string {
	t.Helper()

	upper := strings.ToUpper(sql)
	start := strings.Index(upper, "SELECT")
	if start < 0 {
		t.Fatalf("SQL 里找不到 SELECT：%s", sql)
	}
	start += len("SELECT")
	end := strings.Index(upper[start:], "\nFROM ")
	if end < 0 {
		t.Fatalf("SQL 里找不到顶层 FROM：%s", sql)
	}
	selectList := stripCalls(sql[start:start+end], "COALESCE", "ARRAY", "NULLIF")

	found := map[string]bool{}
	for _, alias := range aliases {
		// 前置的 [^\w.] 是为了不把 `vt.appid` 里的 `t.` 当成别名 t 的列
		pattern := regexp.MustCompile(`(^|[^\w.])` + regexp.QuoteMeta(alias) + `\.(\w+)`)
		for _, m := range pattern.FindAllStringSubmatch(selectList, -1) {
			found[alias+"."+m[2]] = true
		}
	}

	cols := make([]string, 0, len(found))
	for col := range found {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	return cols
}

// stripCalls 挖掉 `fn(...)` 整段（含嵌套括号），剩下的就是没被包住的裸列。
func stripCalls(sql string, fns ...string) string {
	var out strings.Builder
	upper := strings.ToUpper(sql)
	for i := 0; i < len(sql); {
		matched := -1
		for _, fn := range fns {
			if !strings.HasPrefix(upper[i:], fn) {
				continue
			}
			j := i + len(fn)
			for j < len(sql) && (sql[j] == ' ' || sql[j] == '\n' || sql[j] == '\t') {
				j++
			}
			if j < len(sql) && sql[j] == '(' {
				matched = j
				break
			}
		}
		if matched < 0 {
			out.WriteByte(sql[i])
			i++
			continue
		}
		depth := 0
		j := matched
		for ; j < len(sql); j++ {
			if sql[j] == '(' {
				depth++
			} else if sql[j] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		i = j + 1
	}
	return out.String()
}
