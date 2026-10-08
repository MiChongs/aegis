package service

import (
	"strings"
	"testing"

	"aegis/pkg/hydra"
)

func TestValidateRedirectURIs(t *testing.T) {
	ok := []string{
		"https://app.example.com/callback",
		"http://localhost:3000/cb",
		"http://127.0.0.1:8080/cb",
		"http://[::1]:8080/cb",
		"com.example.app:/oauth2redirect",
	}
	if _, err := validateRedirectURIs(ok, "回调地址"); err != nil {
		t.Fatalf("合法地址被拒：%v", err)
	}
	for _, bad := range []string{
		"http://app.example.com/cb",    // 非回环的 http
		"https://app.example.com/cb#x", // 带片段
		"javascript:alert(1)",          // 危险协议
		"myapp://cb",                   // 私有协议不是反向域名
		"/relative/path",               // 相对地址
		"https:///no-host",             // 缺主机
	} {
		if _, err := validateRedirectURIs([]string{bad}, "回调地址"); err == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
	}
}

func TestWithPromptLogin(t *testing.T) {
	got, ok := withPromptLogin("https://auth.example.com/oauth2/auth?client_id=a&prompt=consent&scope=openid")
	if !ok || !strings.Contains(got, "prompt=login") || strings.Contains(got, "prompt=consent") {
		t.Fatalf("应改写为 prompt=login：%q %v", got, ok)
	}
	// 客户端要求静默授权时不能弹登录页，交给调用方返回 login_required
	if _, ok := withPromptLogin("https://auth.example.com/oauth2/auth?client_id=a&prompt=none"); ok {
		t.Fatal("prompt=none 不应被改写")
	}
	if _, ok := withPromptLogin(""); ok {
		t.Fatal("空地址不应被改写")
	}
}

func TestClientAppID(t *testing.T) {
	cases := []struct {
		name   string
		client hydra.OAuth2Client
		want   int64
		ok     bool
	}{
		{"owner 与 metadata 一致", hydra.OAuth2Client{Owner: "aegis-app:7", Metadata: map[string]any{"aegisAppId": float64(7)}}, 7, true},
		{"只有 owner", hydra.OAuth2Client{Owner: "aegis-app:7"}, 7, true},
		{"metadata 被改过", hydra.OAuth2Client{Owner: "aegis-app:7", Metadata: map[string]any{"aegisAppId": float64(8)}}, 0, false},
		{"不是 Aegis 建的客户端", hydra.OAuth2Client{Owner: "someone"}, 0, false},
		{"非法 ID", hydra.OAuth2Client{Owner: "aegis-app:-1"}, 0, false},
	}
	for _, tc := range cases {
		got, ok := clientAppID(tc.client)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s：得到 %d/%v，期望 %d/%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBuildHydraClientRules(t *testing.T) {
	if _, err := buildHydraClient(1, OAuth2ClientInput{Name: "x", TokenEndpointAuthMethod: "none", GrantTypes: []string{"client_credentials"}}); err == nil {
		t.Fatal("公开客户端不能用客户端凭据模式")
	}
	if _, err := buildHydraClient(1, OAuth2ClientInput{Name: "x", GrantTypes: []string{"authorization_code"}}); err == nil {
		t.Fatal("授权码模式缺回调地址应被拒")
	}
	if _, err := buildHydraClient(1, OAuth2ClientInput{Name: "x", GrantTypes: []string{"implicit"}, RedirectURIs: []string{"https://a.example/cb"}}); err == nil {
		t.Fatal("隐式模式不应被允许")
	}
	client, err := buildHydraClient(42, OAuth2ClientInput{Name: " 站点 ", RedirectURIs: []string{"https://a.example/cb", "https://a.example/cb"}})
	if err != nil {
		t.Fatal(err)
	}
	if client.Owner != "aegis-app:42" || client.Metadata["aegisAppId"] != int64(42) || client.ClientName != "站点" {
		t.Fatalf("应用绑定或名称不对：%+v", client)
	}
	if len(client.RedirectURIs) != 1 || client.Scope != "openid offline_access profile" || client.TokenEndpointAuthMethod != "client_secret_basic" {
		t.Fatalf("默认值不对：%+v", client)
	}
	if strings.Join(client.ResponseTypes, ",") != "code" {
		t.Fatalf("授权码模式应带 code 响应类型：%v", client.ResponseTypes)
	}
}
