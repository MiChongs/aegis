package httptransport

import (
	"testing"
)

// 组织路由把静态段与参数段放在了同一层（/organizations/tree 与
// /organizations/:orgId），gin 对这种共存有严格规则，注册冲突会直接 panic。
// 这条用例把 panic 从「服务启动时」提前到「测试期」。
func TestOrgRoutesRegisterWithoutConflict(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("路由注册冲突: %v", r)
		}
	}()
	if _, err := NewRouter(RouterDeps{}); err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
}
