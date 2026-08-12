package service

import (
	"strings"
	"testing"

	avatardomain "aegis/internal/domain/avatar"
)

func TestOwnerTokenRoundTrips(t *testing.T) {
	signer := newAvatarLinkSigner("test-master-key")
	cases := []avatardomain.Owner{
		{Type: avatardomain.OwnerUser, AppID: 1, ID: 42},
		{Type: avatardomain.OwnerUser, AppID: 987654321, ID: 1234567890},
		{Type: avatardomain.OwnerAdmin, ID: 7},
	}
	for _, owner := range cases {
		token := signer.EncodeOwner(owner)
		if token == "" {
			t.Fatalf("%+v 编码失败", owner)
		}
		decoded, ok := signer.DecodeOwner(token)
		if !ok {
			t.Fatalf("%+v 的令牌 %q 解不出来", owner, token)
		}
		if decoded != owner {
			t.Fatalf("往返不一致：%+v → %+v", owner, decoded)
		}
	}
}

// 令牌必须稳定：它就是头像的永久地址本身。同一个人今天和明天算出来不一样，
// 等于所有已经发出去的地址（邮件、客户端本地缓存）一夜之间全部失效。
func TestOwnerTokenIsStable(t *testing.T) {
	owner := avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: 3, ID: 9}
	first := newAvatarLinkSigner("master").EncodeOwner(owner)
	second := newAvatarLinkSigner("master").EncodeOwner(owner)
	if first != second {
		t.Fatalf("同一密钥下令牌不稳定：%q vs %q", first, second)
	}
}

// 签名的唯一职责：挡住按 ID 遍历。改一位就必须解不出来。
func TestOwnerTokenRejectsTampering(t *testing.T) {
	signer := newAvatarLinkSigner("master")
	token := signer.EncodeOwner(avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: 1, ID: 42})

	if _, ok := signer.DecodeOwner(token[:len(token)-1] + "A"); ok {
		t.Fatal("改掉签名末位后仍然解出了主体")
	}
	// 换一把密钥的服务不该认得上一把签出来的令牌
	if _, ok := newAvatarLinkSigner("another-master").DecodeOwner(token); ok {
		t.Fatal("换密钥后旧令牌仍然通过验签")
	}
	for _, bad := range []string{"", ".", "abc", "abc.", ".abc", "!!!.???"} {
		if _, ok := signer.DecodeOwner(bad); ok {
			t.Fatalf("垃圾输入 %q 被接受", bad)
		}
	}
}

// 这条测试守的就是「头像过一阵子就没了」的根因。
//
// 客户端读回资料再原样 PUT 回来时，avatar 字段带的是**展示地址**。
// 把它当成新值写进库，就把唯一那份 storage:// 引用覆盖掉了 ——
// 老版本下发的还是 30 分钟就失效的代理票据，覆盖之后头像永久丢失。
func TestNormalizeAvatarInputProtectsStoredReference(t *testing.T) {
	const current = "storage://3/avatars%2Fapps%2F1%2Fusers%2F42%2F2026%2F08%2F13120000_avatar.jpg"

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"临时代理票据地址被判回不修改", "https://api.example.com/api/storage/proxy/abc123", current},
		{"自家永久头像地址被判回不修改", "https://api.example.com/api/avatars/dTEuNDI.AAAA?v=deadbeef", current},
		{"相对形式的永久地址同样识别", "/api/avatars/dTEuNDI.AAAA", current},
		{"空值不修改", "   ", ""},
		{"与当前一致的引用原样保留", current, current},
		{"第三方外链放行", "https://cdn.example.com/u/42.png", "https://cdn.example.com/u/42.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAvatarInput(tc.input, current)
			if err != nil {
				t.Fatalf("不该报错：%v", err)
			}
			if tc.input == "   " {
				// 空输入的语义是"不修改"，由调用方按空串处理
				if got != "" {
					t.Fatalf("空输入应返回空串，得到 %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// 客户端能自由指定 storage:// 引用的话，任何登录用户都可以把头像设成
// 别人的私有文件，然后从头像地址上把它读出来。这是一条越权读。
func TestNormalizeAvatarInputRejectsForgedReference(t *testing.T) {
	const current = "storage://3/avatars%2Fapps%2F1%2Fusers%2F42%2Fa.jpg"
	if _, err := NormalizeAvatarInput("storage://3/secret%2Fpayroll.pdf", current); err == nil {
		t.Fatal("客户端伪造的 storage:// 引用被接受了")
	}
	if _, err := NormalizeAvatarInput("storage://9/whatever.png", ""); err == nil {
		t.Fatal("没有当前值时伪造引用也应被拒绝")
	}
}

// http(s) 之外的协议一律拒绝：avatar 这一列会被渲染进 <img src>，
// 某些界面还会把它放进 <a href>，那里 javascript: 是可执行的。
func TestNormalizeAvatarInputRejectsNonHTTPScheme(t *testing.T) {
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",
		"file:///etc/passwd",
		"//evil.example.com/x.png",
		"../../etc/passwd",
	} {
		if _, err := NormalizeAvatarInput(bad, ""); err == nil {
			t.Fatalf("%q 应被拒绝", bad)
		}
	}
}

// 第三方登录自动注册时，avatar 在原生 exchange 那条链路上来自客户端请求体。
// 不挡的话可以填成任意 storage 引用，注册完就能从自己的头像地址上读出来。
func TestSanitizeExternalAvatar(t *testing.T) {
	cases := map[string]string{
		"https://q.qlogo.cn/x/0":                  "https://q.qlogo.cn/x/0",
		"http://cdn.example.com/a.png":            "http://cdn.example.com/a.png",
		"storage://3/secret%2Fpayroll.pdf":        "",
		"javascript:alert(1)":                     "",
		"/api/avatars/tok.sig":                    "",
		"https://api.x.com/api/storage/proxy/abc": "",
		"https://api.x.com/api/avatars/tok.sig":   "",
		"":                                        "",
		"   ":                                     "",
	}
	for input, want := range cases {
		if got := SanitizeExternalAvatar(input); got != want {
			t.Errorf("SanitizeExternalAvatar(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildAvatarLinkCarriesVersion(t *testing.T) {
	link := buildAvatarLink("https://api.example.com", "tok.sig", "abc123")
	if !strings.HasPrefix(link, "https://api.example.com/api/avatars/tok.sig") {
		t.Fatalf("地址前缀不对：%s", link)
	}
	if !strings.Contains(link, "v=abc123") {
		t.Fatalf("缺少版本参数：%s", link)
	}
	if got := buildAvatarLink("https://api.example.com", "", "v"); got != "" {
		t.Fatalf("空令牌应得到空地址，得到 %q", got)
	}
}

// If-None-Match 按 RFC 9110 解析：可以是逗号分隔的一串、可以是 `*`、
// 也可能带 W/ 弱校验前缀。做字符串相等会让绝大多数真实的条件请求判不中，
// 表现是「每次刷新都重新下载一遍所有头像」，而且只在浏览器上发作。
func TestAvatarETagMatches(t *testing.T) {
	etag := avatarETag("9f3a1c22", 256)
	if etag != `"9f3a1c22-256"` {
		t.Fatalf("ETag 形状变了：%s", etag)
	}
	for _, header := range []string{
		`"9f3a1c22-256"`,
		`W/"9f3a1c22-256"`,
		`"other", "9f3a1c22-256"`,
		`*`,
	} {
		if !avatarETagMatches(header, etag) {
			t.Errorf("If-None-Match %q 应命中 %s", header, etag)
		}
	}
	for _, header := range []string{
		"",
		`"9f3a1c22-64"`, // 同版本不同尺寸是两份字节，绝不能判成命中
		`"deadbeef-256"`,
		`"9f3a1c22"`,
	} {
		if avatarETagMatches(header, etag) {
			t.Errorf("If-None-Match %q 不该命中 %s", header, etag)
		}
	}
}

// 同一份内容必须得到同一个版本，否则每次重传同一张头像都会让全网缓存失效一次。
func TestAvatarVersionIsContentDerived(t *testing.T) {
	if avatarVersionOf("storage://1/a.jpg") != avatarVersionOf("storage://1/a.jpg") {
		t.Fatal("同一输入算出了不同版本")
	}
	if avatarVersionOf("storage://1/a.jpg") == avatarVersionOf("storage://1/b.jpg") {
		t.Fatal("不同输入算出了相同版本")
	}
	if avatarVersionOf("  ") != "" {
		t.Fatal("空输入应得到空版本")
	}
}
