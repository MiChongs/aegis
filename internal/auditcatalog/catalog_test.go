package auditcatalog

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"unicode"
)

// adminRoutes 从路由快照里取出全部管理端路由（快照由 transport/http 的 golden 测试维护）。
func adminRoutes(t *testing.T) []string {
	t.Helper()
	file, err := os.Open("../transport/http/testdata/routes.golden")
	if err != nil {
		t.Fatalf("读取路由快照失败：%v", err)
	}
	defer file.Close()
	var keys []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "/api/admin/") {
			continue
		}
		keys = append(keys, fields[0]+" "+fields[1])
	}
	if len(keys) < 500 {
		t.Fatalf("只读到 %d 条管理端路由，快照明显不完整", len(keys))
	}
	return keys
}

// 每个管理端接口都必须登记：漏登记的接口在审计日志里只能显示成推断出来的机器文本。
func TestCatalogCoversEveryAdminRoute(t *testing.T) {
	routes := adminRoutes(t)
	missing := 0
	for _, key := range routes {
		if _, ok := registry[key]; !ok {
			t.Errorf("审计目录缺少 %s", key)
			missing++
		}
	}
	if missing > 0 {
		t.Logf("共缺 %d / %d 条", missing, len(routes))
	}
}

// 目录里不能有已经不存在的路由：它们永远不会命中，却会让人以为某个接口已经登记过了。
func TestCatalogHasNoStaleEntries(t *testing.T) {
	live := map[string]bool{}
	for _, key := range adminRoutes(t) {
		live[key] = true
	}
	for key := range registry {
		if !live[key] {
			t.Errorf("审计目录里的 %s 不是已注册的管理端路由", key)
		}
	}
}

func TestCatalogEntriesAreReadable(t *testing.T) {
	for key, op := range registry {
		if !containsHan(op.Name) {
			t.Errorf("%s 的操作名 %q 不是中文", key, op.Name)
		}
		if strings.ContainsAny(op.Name, "·_/") || strings.Contains(op.Name, "appkey") {
			t.Errorf("%s 的操作名 %q 含有机器字符", key, op.Name)
		}
		if op.Target != "" && !containsHan(op.Target) && op.Target != strings.ToUpper(op.Target) {
			t.Errorf("%s 的对象 %q 不是中文", key, op.Target)
		}
		method := strings.SplitN(key, " ", 2)[0]
		if op.Kind == KindRead && op.Severity != SeverityInfo {
			t.Errorf("%s 是查看类，风险等级应为 info", key)
		}
		if method == "DELETE" && op.Kind == KindWrite && (op.Severity == SeverityInfo || op.Severity == SeverityLow) {
			t.Errorf("%s 是删除操作，风险等级至少 medium", key)
		}
		if method == "GET" && op.Kind == KindWrite {
			t.Errorf("%s 是 GET 却登记为变更", key)
		}
	}
}

func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
