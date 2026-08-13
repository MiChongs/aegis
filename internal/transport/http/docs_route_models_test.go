package httptransport

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// docs_route_models.go 由 scripts/docsgen 生成，「产物是否过期」由 CI 的
// `go run ./scripts/docsgen -check` 把关（跑一次要装配路由表 + 加载类型信息，
// 太慢，不适合放进单元测试）。
//
// 这里守的是另外两件不需要重跑生成器就能验的事：表里没有指向空气的条目，
// 以及写接口的 schema 覆盖没有倒退。

// 表里的每一条都必须对应一条真实路由。
//
// 死条目的来路是删路由时没重新生成。它不会报错也不会影响规范
// （没有路由去引用它），但会让人误以为某个接口还在，
// 并且掩盖「这张表已经过期了」这个事实。
func TestGeneratedRouteModelsHaveNoDeadEntries(t *testing.T) {
	engine := newTestRouter(t)

	live := map[string]bool{}
	for _, r := range engine.Routes() {
		live[routeKey(r.Method, normalizeOpenAPIPath(r.Path))] = true
	}

	var dead []string
	for key := range generatedRouteModels() {
		if !live[key] {
			dead = append(dead, key)
		}
	}
	sort.Strings(dead)

	if len(dead) > 0 {
		t.Errorf("映射表里有 %d 条指向不存在的路由，说明产物已过期，"+
			"跑 `go generate ./internal/transport/http/`：\n  %s",
			len(dead), strings.Join(dead, "\n  "))
	}
}

// 写接口的请求体覆盖不得倒退。
//
// 多平台客户端整个是从 OpenAPI 规范生成的，缺 requestBody 的写接口会产出
// 一个不带参数的方法 —— 调用方按签名传不了任何东西，请求打过去必然 400，
// 而这一切在编译期、测试期都毫无迹象。
//
// 下限刻意留了余量：新增一批本来就无请求体的接口（reset / rotate / logout）
// 会拉低比值却不是退步，卡得太死只会让人习惯性地改基线。
const minWriteRouteBodyCoverage = 400

func TestWriteRouteRequestBodyCoverageHasNotRegressed(t *testing.T) {
	engine := newTestRouter(t)
	spec, err := BuildOpenAPISpec(engine, DefaultDocsOptions())
	if err != nil {
		t.Fatalf("BuildOpenAPISpec: %v", err)
	}

	var total, withBody int
	for _, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			switch method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
			default:
				continue
			}
			total++
			if op.RequestBody != nil {
				withBody++
			}
		}
	}

	t.Logf("写接口 %d 条，其中 %d 条带 requestBody", total, withBody)
	if withBody < minWriteRouteBodyCoverage {
		t.Errorf("带 requestBody 的写接口只剩 %d 条（下限 %d）："+
			"生成式客户端会为缺失的那些产出无参数的空方法。"+
			"若是删接口导致的正常下降，再调低这个下限",
			withBody, minWriteRouteBodyCoverage)
	}
}
