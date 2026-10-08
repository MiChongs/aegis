package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	devicedomain "aegis/internal/domain/device"

	"github.com/gin-gonic/gin"
)

// 验证平台解析优先级：Header 最高，UA 次之，都没有时为空（查字典时再按安卓优先）
func TestResolveDevicePlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		header string
		ua     string
		want   string
	}{
		{"Header ios wins", "ios", "Mozilla/5.0 (Windows)", "ios"},
		{"Header android wins", "Android", "Mozilla/5.0 (iPhone)", "android"},
		{"Header harmony", "OpenHarmony", "", "harmonyos"},
		{"UA iphone implies ios", "", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)", "ios"},
		{"UA android implies android", "", "Mozilla/5.0 (Linux; Android 14)", "android"},
		{"Unknown stays empty", "", "curl/8.0", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			if c.header != "" {
				r.Header.Set("X-Device-Platform", c.header)
			}
			r.Header.Set("User-Agent", c.ua)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = r
			if got := resolveDevicePlatform(ctx); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// 登录入口在翻译设备名的同时，必须把原始型号与平台挂到请求上下文上，供会话签发时保存
func TestEnrichDeviceKeepsRawModelOnContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("X-Device-Platform", "android")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = r

	h := &Handler{}
	if got := h.enrichDeviceFromDict(ctx, "dev-1", "SM-G998B"); got != "SM-G998B" {
		t.Fatalf("没有字典时应原样返回，got %q", got)
	}
	client := devicedomain.ClientFrom(ctx.Request.Context())
	if client.Model != "SM-G998B" || client.Platform != "android" {
		t.Fatalf("原始设备信息未挂到上下文：%+v", client)
	}
}
