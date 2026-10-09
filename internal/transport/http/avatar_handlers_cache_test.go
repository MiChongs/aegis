package httptransport

import (
	"testing"

	"aegis/internal/service"
)

// 头像响应的缓存头。降级那一档是这次补上的：之前降级的默认图落在「v 与版本一致」
// 那一档，被当成这张头像以 immutable 缓存一年。
func TestAvatarCacheControl(t *testing.T) {
	cases := []struct {
		name      string
		image     *service.AvatarImage
		requested string
		want      string
	}{
		{"降级图即便 v 匹配也不许缓存", &service.AvatarImage{Version: "2.abc", Degraded: true}, "2.abc", "no-store"},
		{"降级跳转同样不许缓存", &service.AvatarImage{Redirect: "https://example.com/a", Degraded: true}, "", "no-store"},
		{"外部头像短缓存", &service.AvatarImage{Redirect: "https://example.com/a"}, "", "public, max-age=300"},
		{"当前版本长缓存", &service.AvatarImage{Version: "2.abc"}, "2.abc", "public, max-age=31536000, immutable"},
		{"旧地址短缓存", &service.AvatarImage{Version: "2.abc"}, "0123", "public, max-age=300, stale-while-revalidate=86400"},
	}
	for _, c := range cases {
		if got := avatarCacheControl(c.image, c.requested); got != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.name, got, c.want)
		}
	}
}
