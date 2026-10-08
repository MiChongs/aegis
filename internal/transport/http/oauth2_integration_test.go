package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// OAuth2 授权服务器的端到端测试：真实 Postgres + Redis + Ory Hydra 进程，
// 用带 Cookie 的 HTTP 客户端扮演浏览器，把登录 / 两步验证 / 同意 / 令牌 / 刷新 /
// 封禁 / 跨应用切换 / 设备码 / 撤销 / 登出整条链路走一遍。
//
// 三个环境变量缺一不可，缺了就跳过：
//
//	AEGIS_TEST_PG_DSN=postgres://postgres:aegis@127.0.0.1:25432/postgres?sslmode=disable
//	AEGIS_TEST_REDIS_ADDR=127.0.0.1:26379
//	AEGIS_TEST_HYDRA_BIN=/path/to/hydra   （v26，sqlite 版，DSN=memory 即可）
func TestOAuth2ServerEndToEnd(t *testing.T) {
	dsn, redisAddr, hydraBin := os.Getenv("AEGIS_TEST_PG_DSN"), os.Getenv("AEGIS_TEST_REDIS_ADDR"), os.Getenv("AEGIS_TEST_HYDRA_BIN")
	if dsn == "" || redisAddr == "" || hydraBin == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN / AEGIS_TEST_REDIS_ADDR / AEGIS_TEST_HYDRA_BIN")
	}
	ctx := context.Background()

	// ── 数据库 ──
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetOAuth2TestDatabase(t, ctx, pool)
	pg := pgrepo.New(pool)
	rdb := redislib.NewClient(&redislib.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	prefix := fmt.Sprintf("oauth2test:%d:", time.Now().UnixNano())

	// ── 端口与配置 ──
	hydraPublic, hydraAdmin := freePort(t), freePort(t)
	var handler http.Handler = http.NotFoundHandler()
	aegis := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(aegis.Close)
	publicURL := fmt.Sprintf("http://127.0.0.1:%d", hydraPublic)
	adminURL := fmt.Sprintf("http://127.0.0.1:%d", hydraAdmin)
	const hookSecret = "hook-secret-for-tests"

	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	writeFile(t, envFile, strings.Join([]string{
		"APP_ENV=test",
		"POSTGRES_DSN=" + dsn,
		"REDIS_ADDR=" + redisAddr,
		"NATS_URL=nats://127.0.0.1:1", // 只为通过配置校验，本测试不连 NATS
		"JWT_SECRET=" + strings.Repeat("j", 48),
		"SECURITY_MASTER_KEY=" + strings.Repeat("m", 48),
		"API_BASE_URL=" + aegis.URL,
		"OAUTH2_SERVER_ENABLED=true",
		"HYDRA_ADMIN_URL=" + adminURL,
		"HYDRA_PUBLIC_URL=" + publicURL,
		"HYDRA_TOKEN_HOOK_SECRET=" + hookSecret,
		"CAPTCHA_ENABLED=false",
		"",
	}, "\n"))
	t.Setenv("AEGIS_ENV_FILE", envFile)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	log := zap.NewNop()
	sessions := redisrepo.NewSessionRepository(rdb, prefix)
	appService := service.NewAppService(log, pg, sessions)
	security := service.NewSecurityService(cfg, log, pg, sessions, appService)
	auth := service.NewAuthService(cfg, log, pg, sessions, nil, appService, security)
	oauth2 := service.NewOAuth2ServerService(cfg, log, pg, auth, appService, nil)
	router, err := NewRouter(RouterDeps{
		Auth: auth, App: appService, Security: security, Sessions: sessions,
		OAuth2Server: oauth2, Logger: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler = router

	// ── Hydra ──
	hydraCfg := filepath.Join(dir, "hydra.yml")
	writeFile(t, hydraCfg, fmt.Sprintf(`
serve:
  public: { port: %d, host: 127.0.0.1 }
  admin: { port: %d, host: 127.0.0.1 }
urls:
  self: { issuer: %s }
  login: %s/oauth2/login
  consent: %s/oauth2/consent
  logout: %s/oauth2/logout
  error: %s/oauth2/error
  post_logout_redirect: %s/oauth2/logged-out
  device:
    verification: %s/oauth2/device
    success: %s/oauth2/device/done
secrets:
  system: [ "%s" ]
oauth2:
  pkce: { enforced_for_public_clients: true }
  token_hook:
    url: %s/api/oauth2/hooks/token
    auth:
      type: api_key
      config: { name: Authorization, value: "placeholder", in: header }
oidc:
  subject_identifiers:
    supported_types: [ public, pairwise ]
    pairwise: { salt: "pairwise-salt-for-tests-0123456789" }
log: { level: warn }
`, hydraPublic, hydraAdmin, publicURL,
		aegis.URL, aegis.URL, aegis.URL, aegis.URL, aegis.URL, aegis.URL, aegis.URL,
		strings.Repeat("s", 40), aegis.URL))
	hydraLogPath := filepath.Join(dir, "hydra.log")
	if custom := os.Getenv("AEGIS_TEST_HYDRA_LOG"); custom != "" {
		hydraLogPath = custom
	}
	hydraLog, _ := os.Create(hydraLogPath)
	cmd := exec.Command(hydraBin, "serve", "all", "--dev", "-c", hydraCfg)
	// 钩子密钥走环境变量，同时验证 docker-compose 里的注入方式可用
	cmd.Env = append(os.Environ(), "DSN=memory", "LOG_LEVEL=debug", "OAUTH2_TOKEN_HOOK_AUTH_CONFIG_VALUE=Bearer "+hookSecret)
	cmd.Stdout, cmd.Stderr = hydraLog, hydraLog
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			raw, _ := os.ReadFile(hydraLogPath)
			t.Logf("hydra log:\n%s", tail(string(raw), 4000))
		}
	})
	waitReady(t, adminURL+"/health/ready")

	// ── 两个应用、三个用户 ──
	appA := mustApp(t, ctx, pg, "应用甲")
	appB := mustApp(t, ctx, pg, "应用乙")
	alice := mustUser(t, ctx, pg, appA.ID, "alice", "Passw0rd!alice")
	bob := mustUser(t, ctx, pg, appB.ID, "bob", "Passw0rd!bob")
	carol := mustUser(t, ctx, pg, appA.ID, "carol", "Passw0rd!carol")
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname, email) VALUES ($1, '爱丽丝', 'alice@example.com')
		ON CONFLICT (user_id) DO UPDATE SET nickname = EXCLUDED.nickname, email = EXCLUDED.email`, alice.ID); err != nil {
		t.Fatal(err)
	}

	// ── 客户端（经控制台接口同一套服务创建）──
	const callback = "http://127.0.0.1:9/cb"
	clientA, err := oauth2.CreateClient(ctx, appA.ID, service.OAuth2ClientInput{
		Name: "甲的网站", RedirectURIs: []string{callback}, PostLogoutRedirectURIs: []string{"http://127.0.0.1:9/bye"},
		Scopes:     []string{"openid", "offline_access", "profile", "email"},
		GrantTypes: []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if clientA.ClientSecret == "" {
		t.Fatal("创建客户端应返回一次明文密钥")
	}
	clientB, err := oauth2.CreateClient(ctx, appB.ID, service.OAuth2ClientInput{
		Name: "乙的网站", RedirectURIs: []string{callback}, Scopes: []string{"openid", "profile"}, SkipConsent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oauth2.GetClient(ctx, appB.ID, clientA.ClientID); err == nil {
		t.Fatal("应用乙不应读到应用甲的客户端")
	}
	if _, err := oauth2.CreateClient(ctx, appA.ID, service.OAuth2ClientInput{Name: "坏", RedirectURIs: []string{"http://evil.example/cb"}}); err == nil {
		t.Fatal("非回环的 http 回调地址应被拒绝")
	}

	browser := newBrowser(t)

	// ── 1. 授权码 + 登录 + 同意 ──
	page := browser.get(authorizeURL(publicURL, clientA.ClientID, callback, "openid offline_access profile email"))
	if !strings.Contains(page.body, "使用应用甲账号登录") {
		t.Fatalf("应进入应用甲的登录页，实际：%s", snippet(page.body))
	}
	// 错误密码：留在登录页并提示
	page = browser.post(aegis.URL+"/oauth2/login", formOf(page.body, url.Values{"account": {"alice"}, "password": {"wrong"}, "action": {"login"}}))
	if !strings.Contains(page.body, "账号或密码错误") {
		t.Fatalf("错误密码应提示，实际：%s", snippet(page.body))
	}
	// CSRF 令牌不对：拒绝
	bad := formOf(page.body, url.Values{"account": {"alice"}, "password": {"Passw0rd!alice"}, "action": {"login"}})
	bad.Set("csrf", "forged")
	if forged := browser.post(aegis.URL+"/oauth2/login", bad); !strings.Contains(forged.body, "页面已过期") {
		t.Fatalf("伪造的 CSRF 应被拒绝，实际：%s", snippet(forged.body))
	}
	page = browser.post(aegis.URL+"/oauth2/login", formOf(page.body, url.Values{"account": {"alice"}, "password": {"Passw0rd!alice"}, "action": {"login"}, "remember": {"1"}}))
	if !strings.Contains(page.body, "请求访问你的应用甲账号") || !strings.Contains(page.body, "爱丽丝") {
		t.Fatalf("应进入同意页并显示用户，实际：%s", snippet(page.body))
	}
	// 只保留 profile，取消 email
	page = browser.post(aegis.URL+"/oauth2/consent", formOf(page.body, url.Values{"action": {"allow"}, "scope": {"profile"}, "remember": {"1"}}))
	code := page.callbackParam(t, "code")
	tokens := exchangeCode(t, publicURL, clientA.ClientID, clientA.ClientSecret, callback, code)
	claims := jwtClaims(t, tokens.IDToken)
	if claims["sub"] != fmt.Sprint(alice.ID) || claims["preferred_username"] != "alice" || claims["name"] != "爱丽丝" {
		t.Fatalf("ID Token claims 不对：%v", claims)
	}
	if _, ok := claims["email"]; ok {
		t.Fatalf("用户取消了 email 授权，ID Token 不应带邮箱：%v", claims)
	}
	if fmt.Sprint(claims["appid"]) != fmt.Sprint(appA.ID) {
		t.Fatalf("ID Token 应带 appid：%v", claims)
	}
	if !slices.Contains(strings.Fields(tokens.Scope), "offline_access") || slices.Contains(strings.Fields(tokens.Scope), "email") {
		t.Fatalf("授予的权限不对：%q", tokens.Scope)
	}
	info := userinfo(t, publicURL, tokens.AccessToken)
	if info["sub"] != fmt.Sprint(alice.ID) {
		t.Fatalf("userinfo 不对：%v", info)
	}

	// ── 2. 刷新：令牌钩子放行并按当前资料重算 claims ──
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET nickname = '爱丽丝二号' WHERE user_id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	refreshed, status := refreshTokens(t, publicURL, clientA.ClientID, clientA.ClientSecret, tokens.RefreshToken)
	if status != http.StatusOK {
		t.Fatalf("刷新应成功，状态 %d", status)
	}
	if got := jwtClaims(t, refreshed.IDToken)["name"]; got != "爱丽丝二号" {
		t.Fatalf("刷新后的 ID Token 应带新昵称，实际 %v", got)
	}

	// ── 3. 记住登录 + 记住同意：再次授权不出现任何页面 ──
	page = browser.get(authorizeURL(publicURL, clientA.ClientID, callback, "openid offline_access profile"))
	code = page.callbackParam(t, "code")
	tokens = exchangeCode(t, publicURL, clientA.ClientID, clientA.ClientSecret, callback, code)

	// ── 4. 同一浏览器换到应用乙的客户端：不能沿用甲的登录态 ──
	page = browser.get(authorizeURL(publicURL, clientB.ClientID, callback, "openid profile"))
	if !strings.Contains(page.body, "使用应用乙账号登录") {
		t.Fatalf("跨应用应重新登录，实际：%s", snippet(page.body))
	}
	if denied := browser.post(aegis.URL+"/oauth2/login", formOf(page.body, url.Values{"account": {"alice"}, "password": {"Passw0rd!alice"}, "action": {"login"}})); !strings.Contains(denied.body, "账号或密码错误") {
		t.Fatalf("应用甲的账号不应能登录应用乙，实际：%s", snippet(denied.body))
	}
	page = browser.get(authorizeURL(publicURL, clientB.ClientID, callback, "openid profile"))
	page = browser.post(aegis.URL+"/oauth2/login", formOf(page.body, url.Values{"account": {"bob"}, "password": {"Passw0rd!bob"}, "action": {"login"}}))
	// 乙的客户端免同意，登录后直接回调
	code = page.callbackParam(t, "code")
	tokensB := exchangeCode(t, publicURL, clientB.ClientID, clientB.ClientSecret, callback, code)
	if sub := jwtClaims(t, tokensB.IDToken)["sub"]; sub != fmt.Sprint(bob.ID) {
		t.Fatalf("应为 bob，实际 %v", sub)
	}

	// ── 5. 封禁后刷新被令牌钩子拒绝 ──
	if _, err := pool.Exec(ctx, `UPDATE users SET enabled = FALSE WHERE id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, status := refreshTokens(t, publicURL, clientA.ClientID, clientA.ClientSecret, tokens.RefreshToken); status == http.StatusOK {
		t.Fatal("被禁用的账号不应能刷新令牌")
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET enabled = TRUE WHERE id = $1`, alice.ID); err != nil {
		t.Fatal(err)
	}

	// ── 6. 两步验证 ──
	enrollment, err := security.BeginTOTPEnrollment(ctx, &authdomain.Session{AppID: appA.ID, UserID: carol.ID, Account: "carol"})
	if err != nil {
		t.Fatal(err)
	}
	totpCode, _ := totp.GenerateCode(enrollment.Secret, time.Now())
	if _, _, err := security.EnableTOTP(ctx, &authdomain.Session{AppID: appA.ID, UserID: carol.ID, Account: "carol"}, enrollment.EnrollmentID, totpCode); err != nil {
		t.Fatal(err)
	}
	fresh := newBrowser(t)
	page = fresh.get(authorizeURL(publicURL, clientA.ClientID, callback, "openid profile"))
	page = fresh.post(aegis.URL+"/oauth2/login", formOf(page.body, url.Values{"account": {"carol"}, "password": {"Passw0rd!carol"}, "action": {"login"}}))
	if !strings.Contains(page.body, "两步验证") {
		t.Fatalf("开启两步验证的账号应进入验证码页，实际：%s", snippet(page.body))
	}
	wrong := fresh.post(aegis.URL+"/oauth2/login/mfa", formOf(page.body, url.Values{"code": {"000000"}, "action": {"verify"}}))
	if !strings.Contains(wrong.body, "两步验证") || !strings.Contains(wrong.body, `class="alert"`) {
		t.Fatalf("错误验证码应留在本页提示，实际：%s", snippet(wrong.body))
	}
	totpCode, _ = totp.GenerateCode(enrollment.Secret, time.Now())
	page = fresh.post(aegis.URL+"/oauth2/login/mfa", formOf(wrong.body, url.Values{"code": {totpCode}, "action": {"verify"}}))
	page = fresh.post(aegis.URL+"/oauth2/consent", formOf(page.body, url.Values{"action": {"allow"}, "scope": {"profile"}}))
	code = page.callbackParam(t, "code")
	carolTokens := exchangeCode(t, publicURL, clientA.ClientID, clientA.ClientSecret, callback, code)
	carolClaims := jwtClaims(t, carolTokens.IDToken)
	if carolClaims["sub"] != fmt.Sprint(carol.ID) || carolClaims["acr"] != "urn:aegis:acr:mfa" {
		t.Fatalf("两步验证登录的 claims 不对：%v", carolClaims)
	}

	// ── 7. 设备码（RFC 8628）──
	device := deviceAuthorize(t, publicURL, clientA.ClientID, clientA.ClientSecret, "openid profile")
	page = browser.get(device.VerificationURI)
	if !strings.Contains(page.body, "设备登录") {
		t.Fatalf("应进入设备码页，实际：%s", snippet(page.body))
	}
	if badCode := browser.post(aegis.URL+"/oauth2/device", formOf(page.body, url.Values{"userCode": {"ZZZZZZZZ"}})); !strings.Contains(badCode.body, "代码无效") {
		t.Fatalf("错误的设备代码应提示，实际：%s", snippet(badCode.body))
	}
	page = browser.get(device.VerificationURI)
	// 用户可能照着分段显示输入分隔符与空格（Hydra 的用户码区分大小写，大小写须原样）
	typed := " " + device.UserCode[:4] + "-" + device.UserCode[4:] + " "
	page = browser.post(aegis.URL+"/oauth2/device", formOf(page.body, url.Values{"userCode": {typed}}))
	// 这个浏览器在第 4 步换成了应用乙的 bob，设备属于应用甲的客户端，必须重新登录
	if !strings.Contains(page.body, "使用应用甲账号登录") {
		t.Fatalf("设备码通过后应要求登录应用甲，实际：%s", snippet(page.body))
	}
	page = browser.post(aegis.URL+"/oauth2/login", formOf(page.body, url.Values{"account": {"alice"}, "password": {"Passw0rd!alice"}, "action": {"login"}, "remember": {"1"}}))
	if strings.Contains(page.body, "授权请求") {
		page = browser.post(aegis.URL+"/oauth2/consent", formOf(page.body, url.Values{"action": {"allow"}, "scope": {"profile"}}))
	}
	if !strings.Contains(page.body, "设备已登录") {
		t.Fatalf("设备授权应完成，实际：%s", snippet(page.body))
	}
	deviceTokens := pollDeviceToken(t, publicURL, clientA.ClientID, clientA.ClientSecret, device.DeviceCode)
	if sub := jwtClaims(t, deviceTokens.IDToken)["sub"]; sub != fmt.Sprint(alice.ID) {
		t.Fatalf("设备码登录应为 alice，实际 %v", sub)
	}

	// ── 8. 授权记录与撤销 ──
	grants, err := oauth2.ListUserGrants(ctx, appA.ID, alice.ID)
	if err != nil || len(grants) != 1 || grants[0].ClientID != clientA.ClientID {
		t.Fatalf("alice 应有一条对甲的网站的授权：%v %v", grants, err)
	}
	if other, _ := oauth2.ListUserGrants(ctx, appB.ID, alice.ID); len(other) != 0 {
		t.Fatalf("应用乙不应看到 alice 的授权：%v", other)
	}
	if err := oauth2.RevokeUserGrants(ctx, appA.ID, alice.ID, clientA.ClientID); err != nil {
		t.Fatal(err)
	}
	if _, status := refreshTokens(t, publicURL, clientA.ClientID, clientA.ClientSecret, deviceTokens.RefreshToken); status == http.StatusOK {
		t.Fatal("撤销授权后刷新令牌应失效")
	}

	// ── 9. 密钥轮换后旧密钥失效 ──
	rotated, err := oauth2.RotateClientSecret(ctx, appA.ID, clientA.ClientID)
	if err != nil || rotated.ClientSecret == "" || rotated.ClientSecret == clientA.ClientSecret {
		t.Fatalf("轮换密钥失败：%v", err)
	}
	if _, status := refreshTokens(t, publicURL, clientA.ClientID, clientA.ClientSecret, carolTokens.RefreshToken); status != http.StatusUnauthorized {
		t.Fatalf("旧密钥应被拒绝，状态 %d", status)
	}

	// ── 10. 登出（RP 发起）──
	// 这个浏览器当前的登录态是第 7 步勾选了「保持登录」的 alice，用她的 ID Token 发起登出。
	// 未勾选时 Hydra 不保留登录态，登出会直接跳过确认页
	logout := publicURL + "/oauth2/sessions/logout?" + url.Values{"id_token_hint": {deviceTokens.IDToken}}.Encode()
	page = browser.get(logout)
	if !strings.Contains(page.body, "确认退出") {
		t.Fatalf("应进入登出确认页，实际：%s", snippet(page.body))
	}
	page = browser.post(aegis.URL+"/oauth2/logout", formOf(page.body, url.Values{"action": {"yes"}}))
	if !strings.Contains(page.body, "已退出登录") {
		t.Fatalf("登出应落到已退出页，实际：%s", snippet(page.body))
	}
	page = browser.get(authorizeURL(publicURL, clientA.ClientID, callback, "openid profile"))
	if !strings.Contains(page.body, "使用应用甲账号登录") {
		t.Fatalf("登出后应重新登录，实际：%s", snippet(page.body))
	}

	// ── 11. 钩子认证 ──
	resp, err := http.Post(aegis.URL+"/api/oauth2/hooks/token", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("不带密钥调用钩子应 401，实际 %d", resp.StatusCode)
	}
}

// ── 浏览器 ──────────────────────────────────────────────────────────

type browserClient struct {
	t      *testing.T
	client *http.Client
}

type browserPage struct {
	url  *url.URL
	body string
}

func newBrowser(t *testing.T) *browserClient {
	jar, _ := cookiejar.New(nil)
	return &browserClient{t: t, client: &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
		// 跟随重定向，但停在接入方的回调地址（那里没有服务）
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Port() == "9" {
				return http.ErrUseLastResponse
			}
			if len(via) > 20 {
				return fmt.Errorf("重定向过多")
			}
			return nil
		},
	}}
}

func (b *browserClient) do(req *http.Request) browserPage {
	b.t.Helper()
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	page := browserPage{url: resp.Request.URL, body: string(raw)}
	if loc := resp.Header.Get("Location"); loc != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		page.url, _ = url.Parse(loc)
	}
	return page
}

func (b *browserClient) get(target string) browserPage {
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	return b.do(req)
}

func (b *browserClient) post(target string, form url.Values) browserPage {
	req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	parsed, _ := url.Parse(target)
	// 浏览器对同源表单提交也会带 Origin
	req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
	return b.do(req)
}

func (p browserPage) callbackParam(t *testing.T, name string) string {
	t.Helper()
	if p.url == nil || p.url.Port() != "9" {
		t.Fatalf("应回到接入方回调地址，实际停在 %v：%s", p.url, snippet(p.body))
	}
	if e := p.url.Query().Get("error"); e != "" {
		t.Fatalf("授权失败：%s %s", e, p.url.Query().Get("error_description"))
	}
	return p.url.Query().Get(name)
}

var hiddenInput = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// formOf 取出页面里的隐藏字段（challenge / csrf / mfa / remember），叠加上要填的字段。
func formOf(body string, extra url.Values) url.Values {
	form := url.Values{}
	for _, m := range hiddenInput.FindAllStringSubmatch(body, -1) {
		form.Set(m[1], html.UnescapeString(m[2]))
	}
	for k, v := range extra {
		form[k] = v
	}
	return form
}

// ── OAuth2 客户端动作 ──────────────────────────────────────────────

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func authorizeURL(public, clientID, redirect, scope string) string {
	return public + "/oauth2/auth?" + url.Values{
		"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect},
		"scope": {scope}, "state": {"state-" + fmt.Sprint(time.Now().UnixNano())}, "nonce": {"nonce-123456"},
	}.Encode()
}

func tokenRequest(t *testing.T, public, clientID, secret string, form url.Values) (tokenResponse, int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, public+"/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out tokenResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode
}

func exchangeCode(t *testing.T, public, clientID, secret, redirect, code string) tokenResponse {
	t.Helper()
	out, status := tokenRequest(t, public, clientID, secret, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
	})
	if status != http.StatusOK {
		t.Fatalf("换取令牌失败 %d：%s %s", status, out.Error, out.Description)
	}
	return out
}

func refreshTokens(t *testing.T, public, clientID, secret, refresh string) (tokenResponse, int) {
	return tokenRequest(t, public, clientID, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
}

type deviceResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
}

func deviceAuthorize(t *testing.T, public, clientID, secret, scope string) deviceResponse {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, public+"/oauth2/device/auth", strings.NewReader(url.Values{"scope": {scope}, "client_id": {clientID}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out deviceResponse
	if err := json.Unmarshal(raw, &out); err != nil || out.DeviceCode == "" {
		t.Fatalf("设备授权请求失败 %d：%s", resp.StatusCode, raw)
	}
	return out
}

func pollDeviceToken(t *testing.T, public, clientID, secret, deviceCode string) tokenResponse {
	t.Helper()
	for range 10 {
		out, status := tokenRequest(t, public, clientID, secret, url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {deviceCode}, "client_id": {clientID},
		})
		if status == http.StatusOK {
			return out
		}
		if out.Error != "authorization_pending" && out.Error != "slow_down" {
			t.Fatalf("设备码换令牌失败 %d：%s %s", status, out.Error, out.Description)
		}
		time.Sleep(time.Second)
	}
	t.Fatal("设备码换令牌超时")
	return tokenResponse{}
}

func userinfo(t *testing.T, public, accessToken string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, public+"/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func jwtClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("不是 JWT：%q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

// ── 环境 ────────────────────────────────────────────────────────────

func resetOAuth2TestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../../migrations/postgres/*.up.sql")
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("找不到迁移文件")
	}
	for _, file := range files {
		content, _ := os.ReadFile(file)
		sql := strings.TrimSpace(string(content))
		if sql == "" {
			continue
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			// 测试库不带 pgvector / PostGIS，AI 与地理模块与这里无关
			if msg := err.Error(); strings.Contains(msg, "vector") || strings.Contains(msg, "postgis") ||
				strings.Contains(msg, "geography") || strings.Contains(msg, "geometry") {
				continue
			}
			t.Fatalf("apply %s: %v", filepath.Base(file), err)
		}
	}
}

func mustApp(t *testing.T, ctx context.Context, pg *pgrepo.Repository, name string) *appdomain.App {
	t.Helper()
	app, err := pg.UpsertApp(ctx, appdomain.App{Name: name, Status: true, RegisterStatus: true, LoginStatus: true})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

type testUser struct{ ID int64 }

func mustUser(t *testing.T, ctx context.Context, pg *pgrepo.Repository, appID int64, account, password string) testUser {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	user, err := pg.CreateUser(ctx, appID, account, string(hash))
	if err != nil {
		t.Fatal(err)
	}
	return testUser{ID: user.ID}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitReady(t *testing.T, target string) {
	t.Helper()
	for range 100 {
		if resp, err := http.Get(target); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s 未就绪", target)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func snippet(body string) string {
	text := regexp.MustCompile(`(?s)<style.*?</style>|<[^>]+>`).ReplaceAllString(body, " ")
	text = strings.Join(strings.Fields(html.UnescapeString(text)), " ")
	if len([]rune(text)) > 300 {
		return string([]rune(text)[:300])
	}
	return text
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
