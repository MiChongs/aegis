package httptransport

import (
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
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`{"account":"123456","appid":10000,"markcode":"1000","password":"0000"}`))
	ctx.Request.Header.Set("Content-Type", "text/plain")

	var req PasswordLoginRequest
	if err := bind(ctx, &req); err != nil {
		t.Fatalf("bind returned error: %v", err)
	}

	if req.AppID != 10000 || req.Account != "123456" || req.Password != "0000" || req.MarkCode != "1000" {
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
