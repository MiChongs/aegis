package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 验证平台解析优先级：Header 最高，UA 次之，默认 android 优先
func TestResolveDevicePlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name   string
		header string
		ua     string
		want0  string // 首选平台
	}{
		{"Header ios first", "ios", "Mozilla/5.0 (Windows)", "ios"},
		{"Header android first", "android", "Mozilla/5.0 (iPhone)", "android"},
		{"UA iphone implies ios", "", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)", "ios"},
		{"UA android implies android", "", "Mozilla/5.0 (Linux; Android 14)", "android"},
		{"Default android first", "", "curl/8.0", "android"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			if c.header != "" {
				r.Header.Set("X-Device-Platform", c.header)
			}
			r.Header.Set("User-Agent", c.ua)
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = r

			got := resolveDevicePlatforms(ctx)
			if len(got) == 0 || got[0] != c.want0 {
				t.Fatalf("got %v, want first %q", got, c.want0)
			}
		})
	}
}
