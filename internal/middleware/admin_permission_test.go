package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"aegis/internal/service"
	"github.com/gin-gonic/gin"
)

// newPermissionTestContext 构造一个只带请求路径的 gin.Context。
// resolveAdminPermission 在 c.FullPath() 为空时回落到 URL.Path，
// 因此不注册路由也能覆盖它的判定逻辑。
func newPermissionTestContext(method, path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, path, nil)
	return c
}

// 平台级静态目录（"平台支持哪些渠道 / 有哪些节点类型"）绝不能按 appScoped 判定。
//
// 这类接口控制台是**不带 appid** 调用的：
//   - appScoped=true  → extractAdminAppID 找不到应用标识，40058 直接拦掉
//   - appScoped=false 但要权限点 → scopeMatches 在 requestAppID 为 nil 时只认全局授权，
//     应用级管理员转而拿到 403
//
// 两种都会让页面静默空掉（渠道市场显示「平台支持 0 种支付方式」、
// 工作流节点面板整个是空的），且只有打开对应页面才看得出来 —— 所以在这里钉住。
func TestStaticCatalogsAreNotAppScoped(t *testing.T) {
	catalogs := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/admin/app/payment-config/methods"},
		{http.MethodPost, "/api/app/workflow/node-types"},
		{http.MethodPost, "/api/app/workflow/engine/status"},
		{http.MethodGet, "/api/admin/oauth-providers/templates"},
	}
	for _, tc := range catalogs {
		t.Run(tc.path, func(t *testing.T) {
			permission, appScoped, err := resolveAdminPermission(newPermissionTestContext(tc.method, tc.path))
			if err != nil {
				t.Fatalf("resolveAdminPermission 返回错误：%v", err)
			}
			if appScoped {
				t.Errorf("%s 不该按 appScoped 判定：控制台不带 appid，会被 40058 拦掉", tc.path)
			}
			if permission != "" {
				t.Errorf("%s 不该要求权限点（得到 %q）：静态目录无租户数据，"+
					"要求权限点会让应用级管理员拿到 403", tc.path, permission)
			}
		})
	}
}

// 真正与应用相关的接口必须保持 appScoped=true，否则会绕过按应用的授权隔离。
// 与上面的用例配对存在：防止「为了让页面能显示」把整组接口都改成非应用级。
func TestAppResourcesStayAppScoped(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/api/admin/app/payment-config/list", "payment:read"},
		{http.MethodPost, "/api/admin/app/payment-config/create", "payment:write"},
		{http.MethodPost, "/api/admin/app/email-config/list", "email:read"},
		{http.MethodPost, "/api/admin/app/email-config/deliveries", "email:read"},
		{http.MethodPost, "/api/app/workflow/list", "workflow:read"},
		{http.MethodPost, "/api/app/workflow/create", "workflow:write"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			permission, appScoped, err := resolveAdminPermission(newPermissionTestContext(tc.method, tc.path))
			if err != nil {
				t.Fatalf("resolveAdminPermission 返回错误：%v", err)
			}
			if !appScoped {
				t.Errorf("%s 必须按 appScoped 判定，否则跨应用授权隔离失效", tc.path)
			}
			if permission != tc.want {
				t.Errorf("%s 权限点应为 %q，得到 %q", tc.path, tc.want, permission)
			}
		})
	}
}

// 平台治理接口**永远不能按 appScoped 判定**。
//
// 这是整套治理机制的地基：`scopeMatches` 在 requestAppID 非空时，会认可
// 「绑定到该应用的角色」所持有的权限点。一旦治理路由变成应用级，
// 被冻结应用自己的管理员只要拿到 platform:app:govern 就能给自己解封 ——
// 而那正是这套机制存在的意义所反对的。
func TestPlatformGovernanceRoutesAreGlobalScoped(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/api/admin/platform/overview", service.PermissionPlatformAppRead},
		{http.MethodGet, "/api/admin/platform/apps", service.PermissionPlatformAppRead},
		{http.MethodGet, "/api/admin/platform/apps/:appkey/governance", service.PermissionPlatformAppRead},
		{http.MethodPost, "/api/admin/platform/apps/:appkey/governance", service.PermissionPlatformAppGovern},
		{http.MethodPost, "/api/admin/platform/apps/batch-governance", service.PermissionPlatformAppGovern},
		{http.MethodPost, "/api/admin/platform/apps/:appkey/revoke-sessions", service.PermissionPlatformAppDanger},
		{http.MethodGet, "/api/admin/platform/governance/actions", service.PermissionPlatformAppRead},
		{http.MethodGet, "/api/admin/platform/governance/appeals", service.PermissionPlatformAppRead},
		{http.MethodPost, "/api/admin/platform/governance/appeals/:appealId/review", service.PermissionPlatformAppealReview},
		{http.MethodPost, "/api/admin/platform/storage-config/create", service.PermissionPlatformStorageWrite},
		{http.MethodPost, "/api/admin/platform/storage-config/list", service.PermissionPlatformStorageRead},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			permission, appScoped, err := resolveAdminPermission(newPermissionTestContext(tc.method, tc.path))
			if err != nil {
				t.Fatalf("resolveAdminPermission 返回错误：%v", err)
			}
			if appScoped {
				t.Errorf("%s 绝不能按 appScoped 判定：会让被治理应用的管理员自己解自己的封", tc.path)
			}
			if permission != tc.want {
				t.Errorf("%s 权限点应为 %q，得到 %q", tc.path, tc.want, permission)
			}
		})
	}

	// 治理目录是编译进二进制的静态表，被治理方也要靠它读懂自己的处境，故不要权限点
	permission, appScoped, err := resolveAdminPermission(
		newPermissionTestContext(http.MethodGet, "/api/admin/platform/catalog"))
	if err != nil {
		t.Fatalf("resolveAdminPermission 返回错误：%v", err)
	}
	if appScoped || permission != "" {
		t.Errorf("治理目录应对任意已登录管理员开放，得到 permission=%q appScoped=%v", permission, appScoped)
	}
}

// 应用侧的治理视图必须留在应用作用域：应用管理员在这里只能看和申诉，
// 授权按他绑定的应用判定，跨应用一律看不到。
func TestAppGovernanceViewStaysAppScoped(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/api/admin/apps/:appkey/governance", "app:read"},
		{http.MethodGet, "/api/admin/apps/:appkey/governance/history", "app:read"},
		{http.MethodPost, "/api/admin/apps/:appkey/governance/appeals", "app:write"},
		{http.MethodPost, "/api/admin/apps/:appkey/governance/appeals/:appealId/withdraw", "app:write"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			permission, appScoped, err := resolveAdminPermission(newPermissionTestContext(tc.method, tc.path))
			if err != nil {
				t.Fatalf("resolveAdminPermission 返回错误：%v", err)
			}
			if !appScoped {
				t.Errorf("%s 必须按 appScoped 判定，否则跨应用隔离失效", tc.path)
			}
			if permission != tc.want {
				t.Errorf("%s 权限点应为 %q，得到 %q", tc.path, tc.want, permission)
			}
		})
	}
}

// 退款查询类接口属于读操作。此前它们落到默认分支要 payment:write，
// 只有 payment:read 的管理员打开退款列表会 403。
func TestPaymentRefundQueriesAreReads(t *testing.T) {
	for _, path := range []string{
		"/api/admin/app/payment-config/refunds/list",
		"/api/admin/app/payment-config/refunds/order",
		"/api/admin/app/payment-config/refunds/refundable",
		"/api/admin/app/payment-config/orders/list",
		"/api/admin/app/payment-config/orders/detail",
	} {
		t.Run(path, func(t *testing.T) {
			permission, _, err := resolveAdminPermission(newPermissionTestContext(http.MethodPost, path))
			if err != nil {
				t.Fatalf("resolveAdminPermission 返回错误：%v", err)
			}
			if permission != "payment:read" {
				t.Errorf("%s 应为 payment:read，得到 %q", path, permission)
			}
		})
	}
	// 反向守一条：真正的写操作不能被误判成读
	permission, _, err := resolveAdminPermission(
		newPermissionTestContext(http.MethodPost, "/api/admin/app/payment-config/refunds/create"))
	if err != nil {
		t.Fatalf("resolveAdminPermission 返回错误：%v", err)
	}
	if permission != "payment:write" {
		t.Errorf("发起退款应为 payment:write，得到 %q", permission)
	}
}
