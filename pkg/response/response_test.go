package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "aegis/pkg/errors"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// ─── sanitizeMessage ───

func TestSanitizeMessagePreservesBusiness4xxMessage(t *testing.T) {
	if got := sanitizeMessage(401, "账号或密码错误"); got != "账号或密码错误" {
		t.Fatalf("unexpected message: %s", got)
	}
}

func TestSanitizeMessageKeepsServerErrorSanitized(t *testing.T) {
	if got := sanitizeMessage(500, "sql: no rows in result set"); got != "服务暂时不可用" {
		t.Fatalf("unexpected message: %s", got)
	}
}

func TestSanitizeMessageBadRequestFallback(t *testing.T) {
	if got := sanitizeMessage(400, ""); got != "请求未能完成" {
		t.Fatalf("unexpected fallback: %s", got)
	}
}

// ─── Helpers for writer recording ───

func ctxWith(method, target string, accept string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	if accept != "" {
		c.Request.Header.Set("Accept", accept)
	}
	return c, w
}

func decode(t *testing.T, body string) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode envelope: %v | body=%s", err, body)
	}
	return env
}

// ─── Success family ───

func TestOK(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	OK(c, gin.H{"k": "v"})
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	env := decode(t, w.Body.String())
	if env.Code != codeOK || env.Message != "ok" {
		t.Fatalf("envelope: %+v", env)
	}
	if env.Timestamp == 0 {
		t.Fatalf("timestamp should be set")
	}
}

func TestCreated(t *testing.T) {
	c, w := ctxWith(http.MethodPost, "/", "application/json")
	Created(c, nil)
	if w.Code != 201 {
		t.Fatalf("want 201, got %d", w.Code)
	}
}

func TestNoContent(t *testing.T) {
	c, w := ctxWith(http.MethodDelete, "/", "application/json")
	NoContent(c)
	if w.Code != 204 {
		t.Fatalf("want 204, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("204 should have empty body, got %q", w.Body.String())
	}
}

func TestPaginated(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	Paginated(c, []int{1, 2, 3}, 25, 2, 10)
	var raw struct {
		Data PagedData `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.Data.Total != 25 || raw.Data.Page != 2 || raw.Data.PageSize != 10 {
		t.Fatalf("pagination fields wrong: %+v", raw.Data)
	}
	if !raw.Data.HasMore {
		t.Fatalf("hasMore should be true (2*10 < 25)")
	}
}

func TestPaginatedLastPage(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	Paginated(c, []int{}, 10, 1, 10)
	var raw struct {
		Data PagedData `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if raw.Data.HasMore {
		t.Fatalf("hasMore should be false (1*10 == 10)")
	}
}

// ─── Error family ───

func TestErrorJSON(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/foo", "application/json")
	Error(c, http.StatusNotFound, 40400, "找不到")
	if w.Code != 404 {
		t.Fatalf("want 404, got %d", w.Code)
	}
	env := decode(t, w.Body.String())
	if env.Code != 40400 || env.Message != "找不到" {
		t.Fatalf("envelope wrong: %+v", env)
	}
}

func TestErrorHTML(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/foo", "text/html")
	Error(c, http.StatusNotFound, 40400, "")
	if w.Code != 404 {
		t.Fatalf("want 404, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("unexpected Content-Type: %s", ct)
	}
	body := w.Body.String()
	for _, want := range []string{"这里没有内容", "Resource Not Found", "404", "Aegis Platform"} {
		if !strings.Contains(body, want) {
			t.Errorf("html missing %q", want)
		}
	}
}

func TestErrorQueryFormatForcesJSON(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/x?format=json", "text/html")
	c.Request.URL.RawQuery = "format=json"
	Error(c, http.StatusNotFound, 40400, "x")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON when format=json, got %s", ct)
	}
}

func TestErrorQueryFormatForcesHTML(t *testing.T) {
	c, w := ctxWith(http.MethodPost, "/x", "application/json") // POST normally blocks html
	c.Request.URL.RawQuery = "format=html"
	Error(c, http.StatusBadRequest, 40000, "x")
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected HTML when format=html, got %s", ct)
	}
}

func TestErrorE_AppError(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	ae := apperrors.New(40471, http.StatusNotFound, "组织不存在")
	if !ErrorE(c, ae) {
		t.Fatalf("ErrorE should return true")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
	env := decode(t, w.Body.String())
	if env.Code != 40471 {
		t.Fatalf("business code lost: %+v", env)
	}
}

func TestErrorE_PlainError(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	ErrorE(c, http.ErrMissingFile) // any stdlib error
	if w.Code != 500 {
		t.Fatalf("want 500 fallback, got %d", w.Code)
	}
}

func TestErrorE_Nil(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	if ErrorE(c, nil) {
		t.Fatalf("nil error should not write response")
	}
	if w.Code != 200 {
		t.Fatalf("no write expected, got %d", w.Code)
	}
}

func TestShortcutErrors(t *testing.T) {
	cases := []struct {
		name   string
		fn     func(c *gin.Context)
		status int
	}{
		{"BadRequest", func(c *gin.Context) { BadRequest(c, 0, "") }, 400},
		{"Unauthorized", func(c *gin.Context) { Unauthorized(c, "") }, 401},
		{"Forbidden", func(c *gin.Context) { Forbidden(c, "") }, 403},
		{"NotFound", func(c *gin.Context) { NotFound(c, "") }, 404},
		{"Conflict", func(c *gin.Context) { Conflict(c, 0, "x") }, 409},
		{"Unprocessable", func(c *gin.Context) { UnprocessableEntity(c, 0, "x") }, 422},
		{"TooManyRequests", func(c *gin.Context) { TooManyRequests(c, "") }, 429},
		{"Internal", func(c *gin.Context) { Internal(c, "") }, 500},
		{"ServiceUnavailable", func(c *gin.Context) { ServiceUnavailable(c, "") }, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := ctxWith(http.MethodGet, "/", "application/json")
			tc.fn(c)
			if w.Code != tc.status {
				t.Fatalf("%s: want %d, got %d", tc.name, tc.status, w.Code)
			}
		})
	}
}

// ─── Headers ───

func TestRequestIDHeader(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "application/json")
	c.Set("request_id", "rid-abc-123")
	OK(c, nil)
	if got := w.Header().Get(headerRequestID); got != "rid-abc-123" {
		t.Fatalf("X-Request-ID not set, got %q", got)
	}
	env := decode(t, w.Body.String())
	if env.RequestID != "rid-abc-123" {
		t.Fatalf("envelope RequestID wrong: %+v", env)
	}
}

func TestErrorSecurityHeadersOnHTML(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/x", "text/html")
	Error(c, http.StatusForbidden, 40300, "x")
	if w.Header().Get("Cache-Control") == "" {
		t.Errorf("Cache-Control missing")
	}
	if w.Header().Get("X-Content-Type-Options") == "" {
		t.Errorf("X-Content-Type-Options missing")
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("Content-Security-Policy missing")
	}
}

// ─── wantsHTML content negotiation ───

func TestWantsHTML_AcceptJSON(t *testing.T) {
	c, _ := ctxWith(http.MethodGet, "/", "application/json")
	if wantsHTML(c) {
		t.Fatalf("application/json should NOT want html")
	}
}

func TestWantsHTML_AcceptHTMLOnGet(t *testing.T) {
	c, _ := ctxWith(http.MethodGet, "/", "text/html")
	if !wantsHTML(c) {
		t.Fatalf("text/html on GET should want html")
	}
}

func TestWantsHTML_AcceptHTMLOnPost(t *testing.T) {
	c, _ := ctxWith(http.MethodPost, "/", "text/html")
	if wantsHTML(c) {
		t.Fatalf("POST should never auto-want html without format=html")
	}
}

func TestWantsHTML_MozillaNoAccept(t *testing.T) {
	c, _ := ctxWith(http.MethodGet, "/", "")
	c.Request.Header.Set("User-Agent", "Mozilla/5.0 (...)")
	if !wantsHTML(c) {
		t.Fatalf("Mozilla UA with no Accept should want html")
	}
}

// ─── All 5xx / 4xx status pages produce valid HTML ───

func TestRenderHomepage(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "text/html")
	c.Set("request_id", "home-req-1")
	RenderHomepage(c, HomePageData{Version: "v1.2.3", CommitSHA: "abc1234", BuildTime: "2026-04-22"})

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("want html ct, got %s", ct)
	}
	body := w.Body.String()
	for _, want := range []string{
		"<!DOCTYPE html>",
		"Aegis",
		"身份与访问",      // hero
		"Core Capabilities", // section kicker
		"身份与认证",        // capability
		"多租户应用",
		"运行态可观测",
		"工作流引擎",
		"多云存储",
		"API 优先",
		"v1.2.3",
		"abc1234",
		"All systems operational",
		"home-req-1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage missing %q", want)
		}
	}
}

func TestRenderStatusPage_Available(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/status", "text/html")
	c.Set("request_id", "status-req-1")
	RenderStatusPage(c, StatusPageData{
		Status:           "available",
		Score:            100,
		AvailabilityRate: 100.0,
		Summary:          "共 4 项，正常 4，降级 0，不可用 0。",
		Runtime: StatusRuntime{
			AppName:       "Aegis",
			Environment:   "production",
			Version:       "v1.2.3",
			GoVersion:     "go1.26",
			Hostname:      "aegis-01",
			Platform:      "Linux",
			UptimeSeconds: 3600 * 24 * 2,
			CPUUsage:      35.6,
			MemUsedPct:    62.1,
			DiskUsedPct:   45.0,
			CPUCores:      4,
		},
		Counts: StatusCounts{Total: 4, Available: 4},
		Infrastructure: []StatusComponent{
			{Key: "postgres", Name: "PostgreSQL", Status: "available", Summary: "主数据库连接正常"},
			{Key: "redis", Name: "Redis", Status: "available", Summary: "缓存服务正常"},
		},
		Modules: []StatusComponent{
			{Key: "auth", Name: "认证服务", Status: "available", Summary: "JWT 签发正常"},
		},
	})
	if w.Code != 200 {
		t.Fatalf("available status should return 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"<!DOCTYPE html>", "全部系统运行中", "All systems operational",
		"PostgreSQL", "Redis", "认证服务",
		"v1.2.3", "Platform Status", "Infrastructure", "System Modules",
		"Resource Utilization", "Runtime Details",
		"status-req-1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status page missing %q", want)
		}
	}
}

func TestRenderStatusPage_Unavailable503(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/status", "text/html")
	RenderStatusPage(c, StatusPageData{
		Status:  "unavailable",
		Summary: "关键依赖不可用。",
	})
	if w.Code != 503 {
		t.Fatalf("unavailable should return 503, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("Retry-After header should be set on 503")
	}
}

func TestRenderStatusPage_DegradedRetryAfter(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/status", "text/html")
	RenderStatusPage(c, StatusPageData{Status: "degraded"})
	if w.Code != 200 {
		t.Fatalf("degraded should still return 200, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("Retry-After should be set on degraded")
	}
	if !strings.Contains(w.Body.String(), "部分服务降级") {
		t.Errorf("missing degraded label in shared terminal animation footer")
	}
}

func TestRenderAnnouncementsPage_WithItems(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/announcements", "text/html")
	c.Set("request_id", "ann-req-1")
	pub := time.Now().Add(-2 * time.Hour)
	exp := time.Now().Add(48 * time.Hour)
	RenderAnnouncementsPage(c, AnnouncementsPageData{
		Items: []AnnouncementItem{
			{
				ID:          1,
				Type:        "maintenance",
				Title:       "周四凌晨数据库升级",
				Content:     "周四 02:00-04:00 期间进行 PostgreSQL 主从升级。",
				Level:       "important",
				Pinned:      true,
				AdminName:   "平台 SRE",
				PublishedAt: &pub,
				ExpiresAt:   &exp,
				CreatedAt:   pub,
			},
			{
				ID:          2,
				Type:        "security",
				Title:       "紧急：请轮换管理员令牌",
				Content:     "发现上游依赖 CVE-2026-XXXX。",
				Level:       "critical",
				AdminName:   "安全团队",
				PublishedAt: &pub,
				CreatedAt:   pub,
			},
			{
				ID:        3,
				Type:      "update",
				Title:     "新增工作流画布",
				Content:   "工作流画布现已支持 dagre 自动布局。",
				Level:     "normal",
				CreatedAt: pub,
			},
		},
	})

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"<!DOCTYPE html>",
		"平台公告",
		"System Announcements",
		"周四凌晨数据库升级",
		"紧急：请轮换管理员令牌",
		"新增工作流画布",
		"置顶",
		"紧急",
		"重要",
		"常规",
		"Maintenance",
		"Security",
		"Update",
		"Aegis Platform",
		"ann-req-1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("announcements page missing %q", want)
		}
	}
}

func TestRenderAnnouncementsPage_Empty(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/announcements", "text/html")
	RenderAnnouncementsPage(c, AnnouncementsPageData{})
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"暂无公告", "一切如常", "System Announcements"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty announcements page missing %q", want)
		}
	}
}

func TestAnnouncementsPinnedSortedFirst(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/announcements", "text/html")
	now := time.Now()
	older := now.Add(-24 * time.Hour)
	RenderAnnouncementsPage(c, AnnouncementsPageData{
		Items: []AnnouncementItem{
			{ID: 1, Title: "较新未置顶", Type: "info", Level: "normal", Pinned: false, PublishedAt: &now, CreatedAt: now},
			{ID: 2, Title: "较旧但置顶", Type: "info", Level: "normal", Pinned: true, PublishedAt: &older, CreatedAt: older},
		},
	})
	body := w.Body.String()
	idxPinned := strings.Index(body, "较旧但置顶")
	idxNewer := strings.Index(body, "较新未置顶")
	if idxPinned == -1 || idxNewer == -1 {
		t.Fatalf("titles not both present")
	}
	if idxPinned >= idxNewer {
		t.Errorf("pinned should appear before newer unpinned (idx %d vs %d)", idxPinned, idxNewer)
	}
}

func TestRenderEnvPage_Basic(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/cons-env", "text/html")
	c.Set("request_id", "env-req-1")
	c.Request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	c.Request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	data := ExtractEnvPageData(c)
	data.Country = "China"
	data.Region = "Shanghai"
	data.City = "Shanghai"
	data.ASN = "AS4812"
	data.ISP = "China Telecom"

	RenderEnvPage(c, data)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"<!DOCTYPE html>",
		"环境查看",
		"Environment Inspector",
		"当前请求",
		"网络与来源",
		"浏览器",
		"操作系统 / 平台",
		"显示与窗口",
		"硬件",
		"语言与时区",
		"客户端存储",
		"功能支持",
		"权限状态",
		"China",
		"Shanghai",
		"AS4812",
		"Mozilla/5.0",
		"env-req-1",
		"data-env=\"browserBrief\"",
		"navigator.userAgent",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("env page missing %q", want)
		}
	}
}

func TestRenderEnvPage_IncludesCSPForInlineJS(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/cons-env", "text/html")
	RenderEnvPage(c, EnvPageData{ClientIP: "127.0.0.1"})
	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("CSP header should be set")
	}
	if !strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Errorf("CSP must allow inline script for diagnostic JS, got: %s", csp)
	}
}

func TestRenderHomepageDefaultVersion(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "text/html")
	RenderHomepage(c, HomePageData{})
	if !strings.Contains(w.Body.String(), "Aegis dev") {
		t.Fatalf("expected default 'dev' version fallback")
	}
}

func TestErrorPageButtonsNoJavascriptScheme(t *testing.T) {
	// 所有错误页按钮不得使用 javascript: URL 方案（CSP 会拦截）
	for _, status := range []int{400, 401, 403, 404, 405, 409, 422, 429, 500, 502, 503, 504} {
		c, w := ctxWith(http.MethodGet, "/", "text/html")
		Error(c, status, 0, "")
		body := w.Body.String()
		if strings.Contains(body, `href="javascript:`) {
			t.Errorf("status %d: button still uses javascript: URL", status)
		}
	}
}

func TestErrorPageBackAndReloadWired(t *testing.T) {
	// 404 默认应该有"返回上一页"，带 data-nav="back"
	c, w := ctxWith(http.MethodGet, "/", "text/html")
	Error(c, 404, 0, "")
	body := w.Body.String()
	if !strings.Contains(body, `data-nav="back"`) {
		t.Errorf("404 page missing data-nav=back")
	}
	// 500 应该有"刷新重试"，带 data-nav="reload"
	c2, w2 := ctxWith(http.MethodGet, "/", "text/html")
	Error(c2, 500, 0, "")
	if !strings.Contains(w2.Body.String(), `data-nav="reload"`) {
		t.Errorf("500 page missing data-nav=reload")
	}
	_ = body
}

func TestErrorPageCSPAllowsInlineScript(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "text/html")
	Error(c, 404, 0, "")
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Fatalf("CSP must allow inline scripts for navigation JS; got: %s", csp)
	}
}

func TestErrorPageHasNavigationScript(t *testing.T) {
	c, w := ctxWith(http.MethodGet, "/", "text/html")
	Error(c, 404, 0, "")
	body := w.Body.String()
	for _, need := range []string{"<script>", "data-nav", "history.back()", "location.reload()"} {
		if !strings.Contains(body, need) {
			t.Errorf("error page missing navigation element %q", need)
		}
	}
}

func TestAllStatusPagesRender(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict,
		http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		http.StatusNotImplemented, http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout, http.StatusInternalServerError, 418, 599,
	}
	for _, status := range statuses {
		c, w := ctxWith(http.MethodGet, "/", "text/html")
		Error(c, status, 0, "")
		if w.Code != status {
			t.Errorf("status %d: got %d", status, w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "<!DOCTYPE html>") {
			t.Errorf("status %d: not valid HTML", status)
		}
		if !strings.Contains(body, "Aegis") {
			t.Errorf("status %d: missing brand", status)
		}
	}
}
