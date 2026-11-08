package httptransport

import (
	"context"
	"fmt"
	appdomain "aegis/internal/domain/app"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBindSupportsRawJSONFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`{"account":"123456","appid":10000,"deviceId":"dev-001","device":"iPhone 15","markcode":"1000","password":"0000"}`))
	ctx.Request.Header.Set("Content-Type", "text/plain")

	var req PasswordLoginRequest
	if err := bind(ctx, &req); err != nil {
		t.Fatalf("bind returned error: %v", err)
	}

	if req.AppID != 10000 || req.Account != "123456" || req.Password != "0000" || req.DeviceID != "dev-001" || req.Device != "iPhone 15" || req.MarkCode != "1000" {
		t.Fatalf("unexpected bind result: %+v", req)
	}
}

func TestBindReturnsFriendlyEOFError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(""))
	ctx.Request.Header.Set("Content-Type", "application/json")

	var req PasswordLoginRequest
	err := bind(ctx, &req)
	if err == nil {
		t.Fatal("expected bind error")
	}
	if err.Error() != "请求体不能为空" {
		t.Fatalf("unexpected bind error: %v", err)
	}
}

type stubAppIDResolver struct {
	byKey map[string]*appdomain.App
	byID  map[int64]*appdomain.App
}

func (s stubAppIDResolver) GetApp(ctx context.Context, appID int64) (*appdomain.App, error) {
	return s.byID[appID], nil
}

func (s stubAppIDResolver) GetAppByKey(ctx context.Context, appKey string) (*appdomain.App, error) {
	return s.byKey[appKey], nil
}

func TestResolveAppIDSupportsAppKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "appkey", Value: "demo-app"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/system/online/apps/demo-app/users", nil)

	appID, ok := resolveAppID(ctx, stubAppIDResolver{
		byKey: map[string]*appdomain.App{
			"demo-app": {ID: 10000, AppKey: "demo-app"},
		},
	})
	if !ok {
		t.Fatal("expected app key to resolve")
	}
	if appID != 10000 {
		t.Fatalf("expected app id 10000, got %d", appID)
	}
}

func TestResolveAppIDSupportsNumericAppIDFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "appkey", Value: "10000"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/system/online/apps/10000/users", nil)

	appID, ok := resolveAppID(ctx, stubAppIDResolver{
		byID: map[int64]*appdomain.App{
			10000: {ID: 10000, AppKey: "demo-app"},
		},
	})
	if !ok {
		t.Fatal("expected numeric app id to resolve")
	}
	if appID != 10000 {
		t.Fatalf("expected app id 10000, got %d", appID)
	}
}

func TestResolveAppIDReturnsNotFoundForUnknownApp(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "appkey", Value: "missing"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/system/online/apps/missing/users", nil)

	appID, ok := resolveAppID(ctx, stubAppIDResolver{})
	if ok {
		t.Fatalf("expected resolve to fail, got app id %d", appID)
	}
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "\"code\":40404") {
		t.Fatalf("unexpected response body: %s", recorder.Body.String())
	}
}

func TestWriteErrorClientClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/draw", nil).WithContext(ctx)
	(&Handler{}).writeError(c, fmt.Errorf("draw: %w", context.Canceled))
	if c.Writer.Status() != statusClientClosedRequest {
		t.Fatalf("客户端断开时回 499，得到 %d", c.Writer.Status())
	}

	// 请求还在、只是下游自己取消的，仍按服务端错误
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/draw", nil)
	(&Handler{}).writeError(c, context.Canceled)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("请求没断开时不是 499，得到 %d", recorder.Code)
	}
}
