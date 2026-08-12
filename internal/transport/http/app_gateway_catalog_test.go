package httptransport

import (
	"sort"
	"strings"
	"testing"

	"aegis/internal/service"

	"github.com/gin-gonic/gin"
)

// 目录（service.GatewayOperations）与真实路由必须双向对齐。
//
// 这份目录不是文档，是 /config 下发给客户端的机器可读清单：生成式 SDK 照着它
// 产出方法，控制台调试台照着它渲染表单。因此两个方向都要守：
//
//	目录里多一条 → 所有生成式客户端都会调出一个 404；
//	路由里多一条 → 那个能力对生成式客户端根本不存在（只有手写客户端能碰到）。
//
// 加一条网关路由时，同时在 auth_protocol_catalog.go 里加一条即可。
func TestGatewayCatalogMatchesRegisteredRoutes(t *testing.T) {
	engine := newTestRouter(t)

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, gatewayRoutePrefix) {
			registered[route.Method+" "+route.Path] = true
		}
	}

	cataloged := map[string]bool{}
	for _, operation := range service.GatewayOperations() {
		key := operation.Method + " " + gatewayRoutePrefix + ginPlaceholders(operation.Path)
		cataloged[key] = true
		if !registered[key] {
			t.Errorf("目录里的 %s（key=%s）没有对应路由", key, operation.Key)
		}
	}

	for route := range registered {
		if !cataloged[route] {
			t.Errorf("路由 %s 未登记进 auth_protocol_catalog.go，生成式客户端看不到它", route)
		}
	}
}

// 目录的 key 是客户端用来索引 endpoints 的，重复会让后一条静默覆盖前一条。
func TestGatewayCatalogKeysAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, operation := range service.GatewayOperations() {
		if previous, exists := seen[operation.Key]; exists {
			t.Errorf("目录 key %q 重复：%s 与 %s %s", operation.Key, previous, operation.Method, operation.Path)
		}
		seen[operation.Key] = operation.Method + " " + operation.Path
	}
}

// 免包装路径只有 /config 与 OAuth 回跳两条，且必须与中间件的放行清单一致。
// 多放一条就是在安全等级上开了个洞，因此这里把数量也钉死。
func TestGatewayCatalogUnwrappedOperationsStayMinimal(t *testing.T) {
	var unwrapped []string
	for _, operation := range service.GatewayOperations() {
		if operation.Unwrapped {
			unwrapped = append(unwrapped, operation.Path)
		}
	}
	sort.Strings(unwrapped)
	want := []string{"/auth/oauth/callback", "/config"}
	if len(unwrapped) != len(want) {
		t.Fatalf("免包装路径应恰好 %v，实际 %v", want, unwrapped)
	}
	for index := range want {
		if unwrapped[index] != want[index] {
			t.Fatalf("免包装路径应恰好 %v，实际 %v", want, unwrapped)
		}
	}
}

// 需要 Bearer 的接口不能出现在免包装清单里 —— 那两条路径连签名都不验，
// 挂上鉴权接口等于把它暴露在所有包装之外。
func TestGatewayCatalogUnwrappedOperationsNeverRequireAuth(t *testing.T) {
	for _, operation := range service.GatewayOperations() {
		if operation.Unwrapped && operation.Auth {
			t.Errorf("%s %s 既免包装又要求鉴权，两者不能同时成立", operation.Method, operation.Path)
		}
	}
}

const gatewayRoutePrefix = "/api/v1/apps/:appkey"

// ginPlaceholders 把目录里的 {name} 占位符换成 gin 的 :name。
// 目录对外用 {name}（OpenAPI / RFC 6570 通行写法），gin 内部用 :name。
func ginPlaceholders(path string) string {
	var builder strings.Builder
	for index := 0; index < len(path); index++ {
		if path[index] != '{' {
			builder.WriteByte(path[index])
			continue
		}
		end := strings.IndexByte(path[index:], '}')
		if end < 0 {
			builder.WriteByte(path[index])
			continue
		}
		builder.WriteByte(':')
		builder.WriteString(path[index+1 : index+end])
		index += end
	}
	return builder.String()
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	// 零值 RouterDeps 即「所有服务都是 nil」：这些用例只看路由结构，不调服务
	engine, err := NewRouter(RouterDeps{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return engine
}
