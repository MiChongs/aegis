package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveAdminPermissionSignInRewardTemplates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/apps/signin-reward/templates", nil)
	ctx.Request = req
	ctx.Params = gin.Params{}
	ctx.Request.URL.Path = "/api/admin/apps/signin-reward/templates"

	permission, appScoped, err := resolveAdminPermission(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if permission != "platform:app:read" {
		t.Fatalf("expected platform:app:read, got %q", permission)
	}
	if appScoped {
		t.Fatal("expected non app scoped permission")
	}
}

// 走真实路由（因此 c.FullPath() 有值）确认建应用的判定：不要权限点、不按应用作用域。
//
// 这条测试原先断言的是 app:write —— 而那正是把自助注册出来的管理员锁死在
// 零权限里的那一行：他唯一能拿到权限的动作（建自己的第一个应用、成为它的
// app_admin）被一个只有平台管理员才有的权限点挡在门外。闸门改到了
// AdminService.EnsureCanCreateApp（平台开关 + 每人配额），那里才打得了库。
//
// appScoped 必须保持 false 的理由没变：建的时候应用还不存在，
// 按应用作用域判定会先被 40058「缺少有效的应用标识」拦掉。
func TestResolveAdminPermissionCreateAppIsSelfService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.POST("/api/admin/apps", func(c *gin.Context) {
		permission, appScoped, err := resolveAdminPermission(c)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if permission != "" {
			t.Fatalf("建应用不得要求权限点，得到 %q", permission)
		}
		if appScoped {
			t.Fatal("建应用时应用还不存在，不能按应用作用域判定")
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/apps", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, recorder.Code)
	}
}

func TestResolveAdminPermissionSignInRewardAppScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/admin/apps/:appkey/signin-reward", func(c *gin.Context) {
		permission, appScoped, err := resolveAdminPermission(c)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if permission != "app:read" {
			t.Fatalf("expected app:read, got %q", permission)
		}
		if !appScoped {
			t.Fatal("expected app scoped permission")
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/apps/demo/signin-reward", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, recorder.Code)
	}
}
