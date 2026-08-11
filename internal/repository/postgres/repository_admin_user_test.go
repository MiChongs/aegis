package postgres

import (
	"strings"
	"testing"
	"time"

	userdomain "aegis/internal/domain/user"
)

func TestHydrateAdminUserLegacyFieldsUsesMigratedExtra(t *testing.T) {
	createdAt := time.Date(2026, 3, 29, 2, 0, 0, 0, time.UTC)
	item := userdomain.AdminUserView{
		CreatedAt: createdAt,
		Extra: map[string]any{
			"register_ip":       "220.195.74.125",
			"register_province": "北京市",
			"register_city":     "西城区",
			"register_isp":      "联通",
			"disabled_reason":   "legacy disabled",
			"legacy_vip_time":   float64(1716687647),
			"register_time":     "2024-05-26T00:40:47Z",
		},
	}

	hydrateAdminUserLegacyFields(&item)

	if item.RegisterIP != "220.195.74.125" {
		t.Fatalf("expected register ip to be hydrated, got %q", item.RegisterIP)
	}
	if item.RegisterProvince != "北京市" || item.RegisterCity != "西城区" {
		t.Fatalf("expected register location to be hydrated, got province=%q city=%q", item.RegisterProvince, item.RegisterCity)
	}
	if item.RegisterISP != "联通" {
		t.Fatalf("expected register isp to be hydrated, got %q", item.RegisterISP)
	}
	if item.DisabledReason != "legacy disabled" {
		t.Fatalf("expected disabled reason to be hydrated, got %q", item.DisabledReason)
	}
	if item.RegisterTime == nil || item.RegisterTime.UTC().Format(time.RFC3339) != "2024-05-26T00:40:47Z" {
		t.Fatalf("expected register time from extra, got %v", item.RegisterTime)
	}
	if item.VIPExpireAt == nil || item.VIPExpireAt.UTC().Unix() != 1716687647 {
		t.Fatalf("expected vip expire time from legacy extra, got %v", item.VIPExpireAt)
	}
}

func TestHydrateAdminUserLegacyFieldsFallsBackToCreatedAt(t *testing.T) {
	createdAt := time.Date(2026, 3, 29, 3, 30, 0, 0, time.UTC)
	item := userdomain.AdminUserView{
		CreatedAt: createdAt,
		Extra:     map[string]any{},
	}

	hydrateAdminUserLegacyFields(&item)

	if item.RegisterTime == nil || !item.RegisterTime.Equal(createdAt) {
		t.Fatalf("expected register time to fall back to createdAt, got %v", item.RegisterTime)
	}
}

func TestTimeFromMapSupportsUnixMillisecondsAndNumericStrings(t *testing.T) {
	millis := int64(1716687647000)
	data := map[string]any{
		"millis": millis,
		"secs":   "1716687647",
	}

	fromMillis := timeFromMap(data, "millis")
	if fromMillis == nil || fromMillis.UTC().Unix() != 1716687647 {
		t.Fatalf("expected unix millis to parse, got %v", fromMillis)
	}

	fromString := timeFromMap(data, "secs")
	if fromString == nil || fromString.UTC().Unix() != 1716687647 {
		t.Fatalf("expected numeric string unix time to parse, got %v", fromString)
	}
}

// 排序字段直接拼进 SQL（占位符替不了标识符），白名单是唯一的防线。
// 非法值必须**回落**而不是报错：一个拼错的 sort 参数不该让整个用户列表打不开。
func TestBuildAdminUserOrderByRejectsUnknownField(t *testing.T) {
	cases := []string{
		"",
		"unknown",
		"u.created_at",         // 已限定的列名不是对外名，不该被接受
		"created_at",           // SQL 列名不是对外名
		"id; DROP TABLE users", // 注入尝试
		"integral, (SELECT 1)", // 注入尝试
	}
	fallback := buildAdminUserOrderBy("createdAt", "desc", adminUserSortColumns, "", "u.id")
	for _, sort := range cases {
		if got := buildAdminUserOrderBy(sort, "desc", adminUserSortColumns, "", "u.id"); got != fallback {
			t.Fatalf("sort %q should fall back to default order, got %q", sort, got)
		}
	}
}

// 按非唯一列排序时必须有确定性 tiebreaker，否则分页会在页与页之间
// 重复或漏掉行 —— 绝大多数用户 integral 都是 0，这不是理论风险。
func TestBuildAdminUserOrderByAlwaysBreaksTiesById(t *testing.T) {
	for name := range adminUserSortColumns {
		clause := buildAdminUserOrderBy(name, "asc", adminUserSortColumns, "", "u.id")
		if !strings.HasSuffix(clause, "u.id ASC") {
			t.Fatalf("sort %q must break ties by id, got %q", name, clause)
		}
		if !strings.Contains(clause, "NULLS LAST") {
			t.Fatalf("sort %q must place NULLs last, got %q", name, clause)
		}
	}
}

// 快路径的 CTE 不 JOIN user_profiles，落在 user_profiles 上的排序字段
// 必须让查询退回普通路径，否则 ORDER BY 会引用不到列。
func TestFastPathRejectsProfileSortFields(t *testing.T) {
	for _, sort := range []string{"nickname", "email", "registerTime"} {
		if isAdminUserFastPath(userdomain.AdminUserQuery{Sort: sort}) {
			t.Fatalf("sort %q lives on user_profiles and must leave the fast path", sort)
		}
	}
	for _, sort := range []string{"", "createdAt", "integral", "experience", "account"} {
		if !isAdminUserFastPath(userdomain.AdminUserQuery{Sort: sort}) {
			t.Fatalf("sort %q lives on users and should keep the fast path", sort)
		}
	}
}
