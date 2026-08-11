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

func TestResolveAdminPermissionCreateAppRequiresPlatformWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.POST("/api/admin/apps", func(c *gin.Context) {
		permission, appScoped, err := resolveAdminPermission(c)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if permission != "app:write" {
			t.Fatalf("expected app:write, got %q", permission)
		}
		if appScoped {
			t.Fatal("创建应用必须使用平台级权限，不能复用某个应用的作用域")
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
