package httptransport

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 路由表黄金快照。
//
// 存在的理由：router.go 里近千条注册要按域拆分成多个文件，而这种搬迁的失败方式
// 不是编译不过，是**悄悄少一条**——某个 `{}` 块的边界挪错、某段被合并进了相邻的
// 路由组、复制粘贴时漏了一行。这类错误的表现是某个功能在控制台上变成 40400，
// 而全部单元测试照常绿。
//
// 既有的三条路由测试各守一个侧面（网关目录、控制台调用、WAF 方法），
// 但没有任何一条回答「整张表还是不是原来那张」。快照回答的就是这个。
//
// 快照里刻意不含中间件链深度：那个数只在 gin 的 debug 档采集得到，
// 把它写进快照会让同一份代码在不同 GIN_MODE 下给出不同结论。
var updateRouteGolden = flag.Bool("update-route-golden", false,
	"重写路由黄金快照（确认路由变更是有意的之后再用）")

const routeGoldenPath = "testdata/routes.golden"

func TestRouteTableMatchesGoldenSnapshot(t *testing.T) {
	engine := newTestRouter(t)

	lines := make([]string, 0, len(engine.Routes()))
	for _, route := range engine.Routes() {
		lines = append(lines, strings.Join([]string{
			route.Method, route.Path, shortHandlerName(route.Handler),
		}, "\t"))
	}
	sort.Strings(lines)
	current := strings.Join(lines, "\n") + "\n"

	if *updateRouteGolden {
		if err := os.MkdirAll(filepath.Dir(routeGoldenPath), 0o755); err != nil {
			t.Fatalf("建 testdata 目录失败：%v", err)
		}
		if err := os.WriteFile(routeGoldenPath, []byte(current), 0o644); err != nil {
			t.Fatalf("写快照失败：%v", err)
		}
		t.Logf("已重写快照：%d 条路由", len(lines))
		return
	}

	want, err := os.ReadFile(routeGoldenPath)
	if err != nil {
		t.Fatalf("读不到路由快照（%v）。首次生成：go test ./internal/transport/http/ -run TestRouteTableMatchesGoldenSnapshot -update-route-golden", err)
	}

	if string(want) == current {
		return
	}

	// 逐行报差异而不是把两份上千行的文本都打出来：
	// 拆分路由文件时真正要看的是「哪几条不见了 / 多出来了」。
	wantSet := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimRight(string(want), "\n"), "\n") {
		if line != "" {
			wantSet[line] = true
		}
	}
	gotSet := map[string]bool{}
	for _, line := range lines {
		gotSet[line] = true
	}

	var missing, added []string
	for line := range wantSet {
		if !gotSet[line] {
			missing = append(missing, line)
		}
	}
	for line := range gotSet {
		if !wantSet[line] {
			added = append(added, line)
		}
	}
	sort.Strings(missing)
	sort.Strings(added)

	if len(missing) > 0 {
		t.Errorf("快照里有、现在没有的路由（%d 条）——拆分路由文件时最可能是漏搬：\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(added) > 0 {
		t.Errorf("新增的路由（%d 条）：\n  %s", len(added), strings.Join(added, "\n  "))
	}
	t.Log("确认这些变更是有意的之后，用 -update-route-golden 重写快照")
}
