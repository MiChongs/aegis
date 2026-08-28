package postgres

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	appdomain "aegis/internal/domain/app"
	systemdomain "aegis/internal/domain/system"
)

// 内容板块的取值白名单现在有两个执行点：Go 里的 Valid*Types 映射（写入前拒绝）
// 与 000084 落的 CHECK 约束（写入时拒绝）。两者必须逐字相同。
//
// 漂移的两个方向都不会在开发期报错，只会在生产上炸：
//   - Go 里加了一档、迁移没加 → 管理员在控制台选得出来，保存时数据库抛 23514，
//     错误串一路冒到提示条上，长得就跟这轮修掉的 "no rows in result set" 一模一样；
//   - 迁移里多一档、Go 没有 → 那一档谁也写不进去，白名单形同虚设。
//
// 迁移文件是 SQL 侧的唯一事实来源，所以这里直接读它，而不是把枚举再抄一遍。
func TestContentCheckConstraintsMatchDomainCatalogs(t *testing.T) {
	schema := readMigration(t, filepath.Join("..", "..", "..", "migrations", "postgres", "000084_content_storage_contract.up.sql"))

	cases := []struct {
		constraint string
		catalog    map[string]struct{}
	}{
		{"ck_banners_type", appdomain.ValidBannerTypes},
		{"ck_notices_type", appdomain.ValidNoticeTypes},
		{"ck_notices_level", appdomain.ValidNoticeLevels},
		{"ck_notices_status", appdomain.ValidNoticeStatuses},
		{"ck_platform_banners_type", systemdomain.ValidPlatformBannerTypes},
	}

	for _, tc := range cases {
		t.Run(tc.constraint, func(t *testing.T) {
			inSQL := checkVocabulary(t, schema, tc.constraint)
			if !sameVocabulary(inSQL, tc.catalog) {
				t.Errorf("约束与 Go 白名单不一致\n  迁移: %v\n  代码: %v", sortedKeys(inSQL), sortedKeys(tc.catalog))
			}
		})
	}
}

// 加约束之前那几条收敛存量值的 UPDATE，用的必须是同一份白名单。
//
// 少列一档的表现很隐蔽：那一档的存量行不会被收敛，紧接着的 ADD CONSTRAINT
// 就会失败。而迁移运行器对失败只打一行 warn —— 于是整个文件回滚，约束一条都没加上，
// 启动日志看着正常，数据库其实什么都没变。
func TestContentMigrationNormalizesTheSameVocabulary(t *testing.T) {
	schema := readMigration(t, filepath.Join("..", "..", "..", "migrations", "postgres", "000084_content_storage_contract.up.sql"))

	cases := []struct {
		table   string
		column  string
		catalog map[string]struct{}
	}{
		{"banners", "type", appdomain.ValidBannerTypes},
		{"notices", "type", appdomain.ValidNoticeTypes},
		{"notices", "level", appdomain.ValidNoticeLevels},
		{"notices", "status", appdomain.ValidNoticeStatuses},
		{"platform_banners", "type", systemdomain.ValidPlatformBannerTypes},
	}

	for _, tc := range cases {
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			fallback, vocabulary := normalizationRule(t, schema, tc.table, tc.column)
			if !sameVocabulary(vocabulary, tc.catalog) {
				t.Errorf("收敛语句的白名单与代码不一致\n  迁移: %v\n  代码: %v", sortedKeys(vocabulary), sortedKeys(tc.catalog))
			}
			// 兜底值自己必须在白名单里，否则收敛完立刻违反刚加的约束。
			if _, ok := tc.catalog[fallback]; !ok {
				t.Errorf("兜底值 %q 不在白名单里：收敛后仍会违反 CHECK", fallback)
			}
		})
	}
}

// 投放窗口的判定在 Go 里是 `end <= start 即非法`（validateContentWindow），
// 数据库侧必须是同一个方向。写成 >= 会把「起止相同」这种一秒都不生效的窗口放进来。
func TestContentWindowConstraintsAreStrict(t *testing.T) {
	schema := readMigration(t, filepath.Join("..", "..", "..", "migrations", "postgres", "000084_content_storage_contract.up.sql"))

	for _, constraint := range []string{"ck_banners_window", "ck_notices_window", "ck_platform_banners_window"} {
		t.Run(constraint, func(t *testing.T) {
			pattern := regexp.MustCompile(`(?is)ADD\s+CONSTRAINT\s+` + regexp.QuoteMeta(constraint) + `\s+CHECK\s*\((.*?)\)\s*NOT\s+VALID`)
			match := pattern.FindStringSubmatch(schema)
			if match == nil {
				t.Fatalf("迁移里找不到 %s（或它不是 NOT VALID）", constraint)
			}
			body := strings.Join(strings.Fields(match[1]), " ")
			if !strings.Contains(body, "end_time > start_time") {
				t.Errorf("窗口约束必须是严格大于，实际：%s", body)
			}
			// 两端可空表示「不限」，不能被约束顺手拦掉。
			for _, nullable := range []string{"start_time IS NULL", "end_time IS NULL"} {
				if !strings.Contains(body, nullable) {
					t.Errorf("约束没有放行 %s（不限投放时间会被拒绝）：%s", nullable, body)
				}
			}
		})
	}
}

// checkVocabulary 取出 `ADD CONSTRAINT <name> CHECK (<col> IN ('a','b',…))` 里的取值集合。
func checkVocabulary(t *testing.T, schema string, constraint string) map[string]struct{} {
	t.Helper()
	pattern := regexp.MustCompile(`(?is)ADD\s+CONSTRAINT\s+` + regexp.QuoteMeta(constraint) + `\s+CHECK\s*\(\s*\w+\s+IN\s*\(([^)]*)\)`)
	match := pattern.FindStringSubmatch(schema)
	if match == nil {
		t.Fatalf("迁移里找不到约束 %s 的取值列表", constraint)
	}
	return quotedSet(match[1])
}

// normalizationRule 取出 `UPDATE <table> SET <col> = '<fallback>' … <col> NOT IN (…)` 的兜底值与白名单。
func normalizationRule(t *testing.T, schema string, table string, column string) (string, map[string]struct{}) {
	t.Helper()
	pattern := regexp.MustCompile(`(?is)UPDATE\s+` + regexp.QuoteMeta(table) + `\s+SET\s+` + regexp.QuoteMeta(column) +
		`\s*=\s*'([^']*)'.*?` + regexp.QuoteMeta(column) + `\s+NOT\s+IN\s*\(([^)]*)\)`)
	match := pattern.FindStringSubmatch(schema)
	if match == nil {
		t.Fatalf("迁移里找不到 %s.%s 的存量收敛语句", table, column)
	}
	return match[1], quotedSet(match[2])
}

func quotedSet(list string) map[string]struct{} {
	values := make(map[string]struct{})
	for _, raw := range strings.Split(list, ",") {
		if trimmed := strings.Trim(strings.TrimSpace(raw), "'"); trimmed != "" {
			values[trimmed] = struct{}{}
		}
	}
	return values
}

func sameVocabulary(a map[string]struct{}, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
