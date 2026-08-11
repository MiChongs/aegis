package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	"aegis/internal/service"
	"aegis/pkg/textutil"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

// auditExcludePaths 这些前缀下的请求**不产生审计记录**。
//
// 目的：阻止"极高频 + 零管理价值"的端点把审计表刷爆：
//   - /api/admin/system/monitor     → 总览页 15s 轮询
//   - /api/admin/system/runtime     → 运行时信息轮询
//   - /api/admin/system/audit-logs  → 审计页自身（防止查审计 → 产生新审计的环）
//   - /api/admin/system/memory      → 内存监控轮询
//   - /api/admin/system/admins/online → 在线管理员轮询
//   - /api/admin/system/sessions    → 会话列表轮询
//   - /api/admin/apps/:appkey/stats / signin-reward / signin/stats → 仪表盘图表刷新
//   - /api/admin/app/workflow/engine/status → 引擎状态轮询
//   - /api/admin/apps/.*/monitor → 应用监控
//   - /api/admin/system/banners/active → 横幅轮询（总览页每次渲染都请求）
//   - /api/system/monitor 公开监控接口
//   - /api/ws / /healthz / /readyz → WebSocket / K8s 探针
//
// **其余所有读写都会进入审计**。之前为了降噪把 GET/HEAD 一刀切排除，
// 结果超管视图里只剩写操作，远看像是"站点活动稀疏"；现在精准只排掉
// 高频轮询类路径，正常的"管理员点开了某某页"之类的 GET 都会留痕。
var auditExcludePaths = []string{
	"/api/admin/system/monitor",
	"/api/admin/system/audit-logs",
	"/api/admin/system/runtime",
	"/api/admin/system/memory",
	"/api/admin/system/admins/online",
	"/api/admin/system/sessions",
	"/api/admin/system/banners/active",
	"/api/admin/system/crashlogs",
	"/api/admin/app/workflow/engine/status",
	"/api/system/monitor",
	"/api/system/announcements/active",
	"/api/public/branding",
	"/api/avatar/",
	"/api/storage/proxy/",
	"/api/ws",
	"/healthz",
	"/readyz",
}

// auditExcludePathContains 这些片段出现在路径里则排除（对轮询型应用路径管用）
var auditExcludePathContains = []string{
	"/monitor/",
	"/signin/stats",
	"/signin-reward",
	"/stats/user-trend",
	"/stats/regions",
	"/stats/auth-sources",
}

const (
	auditContextStartKey      = "admin_audit.start"
	auditContextBodyKey       = "admin_audit.body"
	auditContextBodySizeKey   = "admin_audit.body_size"
	auditContextRecordedKey   = "admin_audit.recorded"
	auditContextErrorKey      = "admin_audit.error"
	auditContextErrorCodeKey  = "admin_audit.error_code"
	auditContextSummaryKey    = "admin_audit.summary"
	auditContextDetailKey     = "admin_audit.detail"
	auditContextResourceKey   = "admin_audit.resource"
	auditContextResourceIDKey = "admin_audit.resource_id"
	auditContextSeverityKey   = "admin_audit.severity"
	auditContextDiffKey       = "admin_audit.diff"
	maxAuditBodyBytes         = 8 * 1024
	maxAuditStringBytes       = 512
	maxAuditResponseSnippet   = 2 * 1024
)

var auditSensitiveKeys = []string{
	"password", "passwd", "pwd", "secret", "token", "access_token", "refresh_token",
	"authorization", "cookie", "session", "api_key", "apikey", "private_key", "client_secret",
	"totp", "otp", "code", "captcha", "recovery", "credential", "assertion", "signature",
}

// ── 响应捕获 writer（Gin 默认不允许读响应体；我们包一层拿到响应摘要）

type auditResponseWriter struct {
	gin.ResponseWriter
	buf  *bytes.Buffer
	size int
	// capped 摘要已写满。写满之后必须彻底停止累积，不能只看 buf.Len()：
	// 截断处丢掉的那半个字符会让缓冲区没填满，下一片的续写正好接上剩余的
	// 延续字节，拼出来的仍是非法序列。
	capped bool
}

func (w *auditResponseWriter) Write(p []byte) (int, error) {
	if w.buf != nil && !w.capped {
		if remain := maxAuditResponseSnippet - w.buf.Len(); remain >= len(p) {
			w.buf.Write(p)
		} else {
			// 摘要写满。截断点几乎必然落在某个多字节字符中间，而半个字符落到
			// Postgres 的 text 列会让整条审计以 22021 被拒收，报错却出现在
			// 几层之外的写库处。边界只能在拼好的缓冲区上判定：分片的头部本身
			// 可能是上一片某个字符的延续，单看 p 是算不准的。
			w.buf.Write(p[:remain])
			w.buf.Truncate(len(textutil.TrimPartialRune(w.buf.String())))
			w.capped = true
		}
	}
	w.size += len(p)
	return w.ResponseWriter.Write(p)
}

func (w *auditResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// ── Handler 侧显式上报 API

// MarkAuditRecorded 标记该请求已被 handler 层显式记录，跳过中间件自动记录
func MarkAuditRecorded(c *gin.Context) {
	if c != nil {
		c.Set(auditContextRecordedKey, true)
	}
}

// SetAuditFailure 在 handler 中主动标记错误原因（业务错误，非 HTTP 错误）
func SetAuditFailure(c *gin.Context, err error) {
	if c == nil || err == nil {
		return
	}
	summary := textutil.TruncateBytes(strings.TrimSpace(err.Error()), 300)
	if summary != "" {
		c.Set(auditContextErrorKey, summary)
	}
}

// SetAuditErrorCode 绑定业务错误码
func SetAuditErrorCode(c *gin.Context, code string) {
	if c == nil || code == "" {
		return
	}
	c.Set(auditContextErrorCodeKey, code)
}

// SetAuditSummary handler 主动提供人类可读摘要（覆盖默认推断）
func SetAuditSummary(c *gin.Context, summary string) {
	if c == nil || summary == "" {
		return
	}
	c.Set(auditContextSummaryKey, strings.TrimSpace(summary))
}

// SetAuditDetail handler 提供额外详情文字
func SetAuditDetail(c *gin.Context, detail string) {
	if c == nil || detail == "" {
		return
	}
	c.Set(auditContextDetailKey, strings.TrimSpace(detail))
}

// SetAuditResource handler 覆盖 resource/resourceID（用于精确标记目标）
func SetAuditResource(c *gin.Context, resource, resourceID string) {
	if c == nil {
		return
	}
	if resource != "" {
		c.Set(auditContextResourceKey, resource)
	}
	if resourceID != "" {
		c.Set(auditContextResourceIDKey, resourceID)
	}
}

// SetAuditSeverity handler 主动设置严重度
func SetAuditSeverity(c *gin.Context, severity string) {
	if c == nil || severity == "" {
		return
	}
	c.Set(auditContextSeverityKey, severity)
}

// SetAuditDiff handler 记录操作前后的结构化差异（用于关键变更）
func SetAuditDiff(c *gin.Context, before, after any) {
	if c == nil {
		return
	}
	c.Set(auditContextDiffKey, map[string]any{"before": before, "after": after})
}

// ── 上下文采集：changes 字段组装

func GetAuditContextChanges(c *gin.Context) map[string]any {
	if c == nil {
		return nil
	}
	changes := map[string]any{
		"method": c.Request.Method,
		"path":   c.Request.URL.Path,
	}
	if route := c.FullPath(); route != "" {
		changes["route"] = route
	}
	if params := extractRouteParams(c); len(params) > 0 {
		changes["route_params"] = params
	}
	if query := sanitizeValues(c.Request.URL.Query()); len(query) > 0 {
		changes["query"] = query
	}
	if requestBody, ok := c.Get(auditContextBodyKey); ok && requestBody != nil {
		changes["request_body"] = requestBody
	}
	if startedAt, ok := c.Get(auditContextStartKey); ok {
		if ts, ok := startedAt.(time.Time); ok {
			changes["latency_ms"] = time.Since(ts).Milliseconds()
		}
	}
	if statusCode := c.Writer.Status(); statusCode > 0 {
		changes["status_code"] = statusCode
	}
	if requestID := strings.TrimSpace(c.Writer.Header().Get("X-Request-ID")); requestID != "" {
		changes["request_id"] = requestID
	} else if requestID := strings.TrimSpace(c.GetHeader("X-Request-ID")); requestID != "" {
		changes["request_id"] = requestID
	}
	if diff, ok := c.Get(auditContextDiffKey); ok && diff != nil {
		changes["diff"] = diff
	}
	if auditError, ok := c.Get(auditContextErrorKey); ok {
		changes["error"] = auditError
	} else if lastErr := c.Errors.Last(); lastErr != nil {
		changes["error"] = trimAuditString(lastErr.Error())
	}
	// 关键请求头快照（排除认证头）
	headerSnap := map[string]any{}
	for _, key := range []string{"X-Forwarded-For", "X-Real-IP", "Referer", "Origin", "Accept-Language", "X-App-Key"} {
		if v := strings.TrimSpace(c.GetHeader(key)); v != "" {
			headerSnap[key] = trimAuditString(v)
		}
	}
	if len(headerSnap) > 0 {
		changes["headers"] = headerSnap
	}
	return changes
}

// ── 中间件主体

func AuditMiddleware(auditSvc *service.AuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(auditContextStartKey, time.Now())

		var requestSize int
		if requestBody := captureAuditRequestBody(c); requestBody != nil {
			c.Set(auditContextBodyKey, requestBody)
		}
		if size, ok := c.Get(auditContextBodySizeKey); ok {
			if sz, ok := size.(int); ok {
				requestSize = sz
			}
		}

		// 包裹 ResponseWriter 捕获响应摘要
		buf := &bytes.Buffer{}
		writer := &auditResponseWriter{ResponseWriter: c.Writer, buf: buf}
		c.Writer = writer

		c.Next()

		if auditSvc == nil {
			return
		}
		if wasAuditRecorded(c) {
			return
		}

		path := c.Request.URL.Path
		for _, prefix := range auditExcludePaths {
			if strings.HasPrefix(path, prefix) {
				return
			}
		}
		for _, frag := range auditExcludePathContains {
			if strings.Contains(path, frag) {
				return
			}
		}

		method := c.Request.Method
		// OPTIONS 是 CORS 预检，本身无业务语义 —— 继续排除
		if method == http.MethodOptions {
			return
		}
		// GET / HEAD 不再一刀切排除；它们会以 severity=info 记录为"谁在什么时候看了什么"，
		// 高频轮询类已在 auditExcludePaths / auditExcludePathContains 预先过滤

		// 读取管理员会话：AdminAuth / AdminAccess 中间件均以 "admin.session" 作为键
		//
		// 特殊路径：auth 路由（登录 / 注册 / OIDC / SAML / MFA）在 handler 执行前没有会话。
		// 正常路径下 handler 会主动调用 recordAuditAuth(MarkAuditRecorded)，上面已提前 return；
		// 仅当 handler 失败在更早阶段（验证码错误、bind 错误等）没来得及审计时，才走这条兜底。
		session, ok := c.Get("admin.session")
		if !ok {
			// 未认证路径的兜底审计（只对 auth 路由生效，避免对公开接口产生噪声）
			if strings.HasPrefix(c.FullPath(), "/api/admin/auth/") {
				recordAnonymousAuthFallback(c, auditSvc, requestSize, writer)
			}
			return
		}
		ctx, ok := session.(*admindomain.AccessContext)
		if !ok || ctx == nil {
			return
		}

		statusCode := c.Writer.Status()
		status := auditStatusFromCode(statusCode)

		route := c.FullPath()
		category := classifyAuditCategory(route)
		action := inferAuditAction(method, route, category)
		resource := classifyAuditResource(route)
		if v, ok := c.Get(auditContextResourceKey); ok {
			if s, _ := v.(string); s != "" {
				resource = s
			}
		}
		resourceID := classifyAuditResourceID(c)
		if v, ok := c.Get(auditContextResourceIDKey); ok {
			if s, _ := v.(string); s != "" {
				resourceID = s
			}
		}

		// 优先使用 handler 显式设置的 severity
		severity := InferAuditSeverity(category, method, route, statusCode, ctx.IsSuperAdmin)
		if v, ok := c.Get(auditContextSeverityKey); ok {
			if s, _ := v.(string); s != "" {
				severity = s
			}
		}

		// summary 默认由推断生成，handler 可覆盖
		summary := BuildAuditSummary(method, route, resource, resourceID, statusCode)
		if v, ok := c.Get(auditContextSummaryKey); ok {
			if s, _ := v.(string); s != "" {
				summary = s
			}
		}

		// detail：描述 + handler 附加
		detail := fmt.Sprintf("%s %s → %d (%dms)", method, path, statusCode, elapsedMs(c))
		if v, ok := c.Get(auditContextDetailKey); ok {
			if s, _ := v.(string); s != "" {
				detail = detail + " | " + s
			}
		}

		// 错误信息
		errorMsg := ""
		if v, ok := c.Get(auditContextErrorKey); ok {
			if s, _ := v.(string); s != "" {
				errorMsg = s
			}
		}
		if errorMsg == "" {
			if lastErr := c.Errors.Last(); lastErr != nil {
				errorMsg = trimAuditString(lastErr.Error())
			}
		}
		errorCode := ""
		if v, ok := c.Get(auditContextErrorCodeKey); ok {
			if s, _ := v.(string); s != "" {
				errorCode = s
			}
		}

		// 地理位置（若 Location 中间件已注入）
		loc := RequestLocation(c)

		// 管理员角色（取首个 Assignment.RoleKey，超管直接 super_admin）
		adminRole := ""
		if ctx.IsSuperAdmin {
			adminRole = "super_admin"
		} else if len(ctx.Assignments) > 0 {
			adminRole = ctx.Assignments[0].RoleKey
		}

		// TraceID
		traceID := ""
		if sc := trace.SpanContextFromContext(c.Request.Context()); sc.IsValid() {
			traceID = sc.TraceID().String()
		}

		// 响应摘要（提取错误信息 / 关键 ID）
		responseSnippet, inferredErrCode, inferredErrMsg := extractResponseSnippet(buf.Bytes(), statusCode)
		if errorCode == "" {
			errorCode = inferredErrCode
		}
		if errorMsg == "" && inferredErrMsg != "" {
			errorMsg = inferredErrMsg
		}

		entry := systemdomain.AuditEntry{
			AdminID:   ctx.AdminID,
			AdminName: ctx.Account,
			AdminRole: adminRole,
			SessionID: ctx.TokenID,

			Action:   action,
			Category: category,
			Severity: severity,

			Resource:   resource,
			ResourceID: resourceID,

			Summary: summary,
			Detail:  detail,

			RequestID:  strings.TrimSpace(c.Writer.Header().Get("X-Request-ID")),
			TraceID:    traceID,
			Method:     method,
			Path:       path,
			Route:      route,
			StatusCode: statusCode,
			LatencyMs:  elapsedMs(c),

			RequestSize:     requestSize,
			ResponseSize:    writer.size,
			ResponseSnippet: responseSnippet,

			IP:        c.ClientIP(),
			Country:   loc.Country,
			Region:    loc.Region,
			City:      loc.City,
			ISP:       loc.ISP,
			UserAgent: c.GetHeader("User-Agent"),

			Status:       status,
			ErrorCode:    errorCode,
			ErrorMessage: errorMsg,

			Changes: GetAuditContextChanges(c),
		}
		auditSvc.Record(entry)
		// 标记已记录，防止嵌套 / 重复绑定 AuditMiddleware 时产生两条审计
		MarkAuditRecorded(c)
	}
}

// recordAnonymousAuthFallback 处理 "auth 路由 + 无会话 + handler 未显式审计" 的兜底场景：
//
// 常见触发点：
//   - /api/admin/auth/login 请求体 bind 失败（400）
//   - 登录前的验证码校验失败（403）
//   - bindings 错误被 h.writeError 直接返回，handler 未到达 recordAuditAuth
//
// 以请求体里的 account 字段作为最大程度的身份记录，避免失败事件无迹可寻。
func recordAnonymousAuthFallback(c *gin.Context, auditSvc *service.AuditService, requestSize int, writer *auditResponseWriter) {
	path := c.Request.URL.Path
	route := c.FullPath()
	method := c.Request.Method
	statusCode := c.Writer.Status()

	// 只在失败或非 2xx 时兜底，成功路径由 handler 自己处理
	if statusCode < 400 {
		return
	}

	category := "auth"
	provider := inferAuthProviderFromPath(route)
	event := inferAuthEventFromPath(route)
	action := "auth." + provider + "." + event + ".failed"

	// 尝试从请求 body 拿 account（handler 没机会执行完就失败时，captureAuditRequestBody 已经抓过了）
	account := ""
	if v, ok := c.Get(auditContextBodyKey); ok && v != nil {
		if m, ok := v.(map[string]any); ok {
			if acc, ok := m["account"].(string); ok {
				account = strings.TrimSpace(acc)
			} else if email, ok := m["email"].(string); ok {
				account = strings.TrimSpace(email)
			}
		}
	}

	errorMsg := ""
	if v, ok := c.Get(auditContextErrorKey); ok {
		if s, _ := v.(string); s != "" {
			errorMsg = s
		}
	}
	if errorMsg == "" {
		if lastErr := c.Errors.Last(); lastErr != nil {
			errorMsg = trimAuditString(lastErr.Error())
		}
	}
	// 从响应 body 提取错误信息
	if writer != nil && writer.buf != nil {
		_, _, inferredErr := extractResponseSnippet(writer.buf.Bytes(), statusCode)
		if errorMsg == "" && inferredErr != "" {
			errorMsg = inferredErr
		}
	}

	summary := "匿名"
	if account != "" {
		summary = account
	}
	summary = "管理员 " + summary + " " + authEventChineseVerb(event) + "失败（" + authProviderChineseLabel(provider) + "）"
	if errorMsg != "" {
		summary += "：" + errorMsg
	}

	loc := RequestLocation(c)

	entry := systemdomain.AuditEntry{
		AdminID:   0,
		AdminName: account,
		Category:  category,
		Action:    action,
		Resource:  "auth." + provider,
		Summary:   summary,
		Detail:    fmt.Sprintf("%s %s → %d (%dms) | provider=%s event=%s", method, path, statusCode, elapsedMs(c), provider, event),

		Method:     method,
		Path:       path,
		Route:      route,
		StatusCode: statusCode,
		LatencyMs:  elapsedMs(c),

		RequestSize: requestSize,
		ResponseSize: func() int {
			if writer != nil {
				return writer.size
			}
			return 0
		}(),

		IP:        c.ClientIP(),
		Country:   loc.Country,
		Region:    loc.Region,
		City:      loc.City,
		ISP:       loc.ISP,
		UserAgent: c.GetHeader("User-Agent"),

		Status:       auditStatusFromCode(statusCode),
		ErrorMessage: errorMsg,
		Severity:     systemdomain.AuditSeverityHigh,

		Changes: GetAuditContextChanges(c),
	}
	if entry.Changes == nil {
		entry.Changes = map[string]any{}
	}
	entry.Changes["provider"] = provider
	entry.Changes["auth_event"] = event
	entry.Changes["fallback"] = true
	if account != "" {
		entry.Changes["account"] = account
	}
	auditSvc.Record(entry)
}

// inferAuthProviderFromPath 从 /api/admin/auth/xxx 路径推断 provider
func inferAuthProviderFromPath(route string) string {
	lower := strings.ToLower(route)
	switch {
	case strings.Contains(lower, "/oidc/"):
		return "oidc"
	case strings.Contains(lower, "/saml/"):
		return "saml"
	case strings.Contains(lower, "/ldap/"):
		return "ldap"
	case strings.Contains(lower, "/verify-mfa"):
		return "mfa"
	case strings.Contains(lower, "/register"):
		return "password"
	default:
		return "password"
	}
}

// inferAuthEventFromPath 从路径推断事件
func inferAuthEventFromPath(route string) string {
	lower := strings.ToLower(route)
	switch {
	case strings.Contains(lower, "/register"):
		return "register"
	case strings.Contains(lower, "/verify-mfa"):
		return "verify"
	case strings.Contains(lower, "/logout"):
		return "logout"
	default:
		return "login"
	}
}

func authProviderChineseLabel(p string) string {
	switch p {
	case "password":
		return "密码"
	case "ldap":
		return "LDAP"
	case "oidc":
		return "OIDC"
	case "saml":
		return "SAML"
	case "mfa":
		return "MFA"
	}
	return strings.ToUpper(p)
}

func authEventChineseVerb(event string) string {
	switch event {
	case "login":
		return "登录"
	case "register":
		return "注册"
	case "verify", "mfa":
		return "二次验证"
	case "logout":
		return "登出"
	}
	return event
}

// ── 辅助函数

func wasAuditRecorded(c *gin.Context) bool {
	flag, ok := c.Get(auditContextRecordedKey)
	if !ok {
		return false
	}
	v, ok := flag.(bool)
	return ok && v
}

func elapsedMs(c *gin.Context) int {
	if startedAt, ok := c.Get(auditContextStartKey); ok {
		if ts, ok := startedAt.(time.Time); ok {
			return int(time.Since(ts).Milliseconds())
		}
	}
	return 0
}

func auditStatusFromCode(code int) string {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return systemdomain.AuditStatusDenied
	case code == http.StatusTooManyRequests || code == 451:
		return systemdomain.AuditStatusBlocked
	case code >= 400:
		return systemdomain.AuditStatusFailed
	default:
		return systemdomain.AuditStatusSuccess
	}
}

// extractResponseSnippet 从响应体提取摘要（尽量结构化）
// 返回：snippet, errorCode, errorMessage（尝试从 {"code":..., "message":...} 里抽出）
func extractResponseSnippet(raw []byte, statusCode int) (string, string, string) {
	if len(raw) == 0 {
		return "", "", ""
	}
	snippet := textutil.TruncateBytes(string(raw), maxAuditResponseSnippet)
	if len(snippet) < len(raw) {
		snippet += "...<truncated>"
	}
	// 尝试 JSON 解析提取 code/message
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err == nil {
		errCode := ""
		errMsg := ""
		// 常见形态：{"code":0,"message":"ok","data":...}
		if v, ok := body["code"]; ok && statusCode >= 400 {
			errCode = fmt.Sprintf("%v", v)
		}
		if v, ok := body["message"]; ok {
			errMsg = fmt.Sprintf("%v", v)
		} else if v, ok := body["error"]; ok {
			errMsg = fmt.Sprintf("%v", v)
		}
		if statusCode < 400 {
			return snippet, "", ""
		}
		return snippet, errCode, trimAuditString(errMsg)
	}
	return snippet, "", ""
}

// classifyAuditResource 从路由模板推断 "<域>.<资源>" 形式的资源标识
//
//	优先级剥掉 `/api/admin/` 前缀（后台路径）；其次剥 `/api/` 前缀（开放 / 兼容路径）
//	例：
//	  /api/admin/app/role-application/list   → app.role-application
//	  /api/app/workflow/logs                 → app.workflow       （不再把 "api" 当成资源段）
//	  /api/user/settings                     → user.settings
func classifyAuditResource(fullPath string) string {
	trimmed := fullPath
	// 先剥后台前缀
	if strings.HasPrefix(trimmed, "/api/admin/") {
		trimmed = strings.TrimPrefix(trimmed, "/api/admin/")
	} else if strings.HasPrefix(trimmed, "/api/") {
		// 非后台路径（/api/app / /api/user / /api/auth 等）：只剥 /api/，保留剩余的业务前缀
		trimmed = strings.TrimPrefix(trimmed, "/api/")
	}
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "api"
	}
	segments := strings.Split(trimmed, "/")
	parts := make([]string, 0, 2)
	for _, segment := range segments {
		if segment == "" || strings.HasPrefix(segment, ":") {
			continue
		}
		parts = append(parts, segment)
		if len(parts) == 2 {
			break
		}
	}
	if len(parts) == 0 {
		return "api"
	}
	return strings.Join(parts, ".")
}

func classifyAuditResourceID(c *gin.Context) string {
	if params := extractRouteParams(c); len(params) > 0 {
		keys := make([]string, 0, len(params))
		for key := range params {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", key, params[key]))
		}
		return strings.Join(parts, ",")
	}
	if route := c.FullPath(); route != "" {
		return route
	}
	return c.Request.URL.Path
}

func extractRouteParams(c *gin.Context) map[string]any {
	if len(c.Params) == 0 {
		return nil
	}
	params := make(map[string]any, len(c.Params))
	for _, param := range c.Params {
		if param.Key == "" || param.Value == "" {
			continue
		}
		params[param.Key] = trimAuditString(param.Value)
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func captureAuditRequestBody(c *gin.Context) any {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil
	}
	method := c.Request.Method
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return nil
	}
	contentType := strings.ToLower(strings.TrimSpace(c.ContentType()))
	if strings.HasPrefix(contentType, "multipart/form-data") || strings.HasPrefix(contentType, "application/octet-stream") {
		return map[string]any{"content_type": contentType, "omitted": true}
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAuditBodyBytes+1))
	if err != nil {
		return map[string]any{"content_type": contentType, "read_error": err.Error()}
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(raw))
	c.Set(auditContextBodySizeKey, len(raw))
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxAuditBodyBytes {
		return map[string]any{"content_type": contentType, "truncated": true, "size": len(raw)}
	}

	if strings.HasPrefix(contentType, "application/json") {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			return sanitizeAuditValue(decoded)
		}
	}
	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		if values, err := url.ParseQuery(string(raw)); err == nil {
			return sanitizeValues(values)
		}
	}
	return trimAuditString(string(raw))
}

func sanitizeValues(values url.Values) map[string]any {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]any, len(values))
	for key, items := range values {
		if len(items) == 0 {
			continue
		}
		if shouldRedactAuditKey(key) {
			result[key] = "[REDACTED]"
			continue
		}
		if len(items) == 1 {
			result[key] = trimAuditString(items[0])
			continue
		}
		sanitized := make([]any, 0, len(items))
		for _, item := range items {
			sanitized = append(sanitized, trimAuditString(item))
		}
		result[key] = sanitized
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sanitizeAuditValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if shouldRedactAuditKey(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = sanitizeAuditValue(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitizeAuditValue(item))
		}
		return result
	case string:
		return trimAuditString(typed)
	default:
		return typed
	}
}

func shouldRedactAuditKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, sensitive := range auditSensitiveKeys {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

func trimAuditString(value string) string {
	return textutil.TruncateBytes(strings.TrimSpace(value), maxAuditStringBytes)
}
