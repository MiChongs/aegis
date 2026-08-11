package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	auditmiddleware "aegis/internal/middleware"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// requireAuditReader 审计日志读权限守门
//
//	识别"有效超管"：
//	  (a) 会话的 IsSuperAdmin 布尔列 == true，或者
//	  (b) 会话分配里包含 role_key == "super_admin" 的 Assignment（显式授予超管角色）
//
//	修复要点：旧版仅用 requireSuperAdminSession(c) 且失败时直接 `return`，
//	既不写 403 也不写任何响应体 —— 客户端拿到 200 空体，React Query 报"JSON 解析错误"，
//	UI 上表现为"超管看不到全局审计日志"。这是这次用户反馈的真正根因。
//
//	修复：
//	  1. 失败时显式下发 403 响应（40301），前端能正确提示
//	  2. 放宽到"有效超管"—— 即便 is_super_admin 列为 false，只要拥有 super_admin 角色分配也放行
func (h *Handler) requireAuditReader(c *gin.Context) (*admindomain.AccessContext, bool) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return nil, false
	}
	if session.IsSuperAdmin {
		return session, true
	}
	for _, a := range session.Assignments {
		if a.RoleKey == "super_admin" {
			return session, true
		}
	}
	response.Error(c, http.StatusForbidden, 40301, "仅超级管理员可查看全局审计日志")
	return nil, false
}

func (h *Handler) ListAuditLogs(c *gin.Context) {
	if _, ok := h.requireAuditReader(c); !ok {
		return
	}
	var q AuditLogQuery
	_ = c.ShouldBindQuery(&q)
	page, err := h.audit.ListLogs(c.Request.Context(), filterFromQuery(q))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", page)
}

func (h *Handler) GetAuditLog(c *gin.Context) {
	if _, ok := h.requireAuditReader(c); !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 ID")
		return
	}
	log, err := h.audit.GetLog(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if log == nil {
		response.Error(c, http.StatusNotFound, 40491, "审计日志不存在")
		return
	}
	response.Success(c, 200, "ok", log)
}

func (h *Handler) GetAuditStats(c *gin.Context) {
	if _, ok := h.requireAuditReader(c); !ok {
		return
	}
	stats, err := h.audit.GetStats(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", stats)
}

func (h *Handler) ExportAuditLogs(c *gin.Context) {
	if _, ok := h.requireAuditReader(c); !ok {
		return
	}
	var q AuditLogQuery
	_ = c.ShouldBindQuery(&q)
	logs, err := h.audit.ExportLogs(c.Request.Context(), filterFromQuery(q))
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := fmt.Sprintf("audit-logs-%s.csv", time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)

	var sb strings.Builder
	sb.WriteString("\xEF\xBB\xBF")
	// 列顺序与内容保持与 AuditLog 一致，便于二次分析
	sb.WriteString(strings.Join([]string{
		"ID", "时间",
		"管理员ID", "管理员", "角色", "会话ID",
		"类别", "严重度", "动作", "资源", "资源ID",
		"摘要", "描述",
		"请求ID", "TraceID", "方法", "路径", "路由模板", "状态码", "耗时(ms)",
		"请求字节", "响应字节", "响应摘要",
		"IP", "国家", "地区", "城市", "ISP", "UA",
		"状态", "错误码", "错误信息", "变更详情",
	}, ","))
	sb.WriteString("\n")

	for _, l := range logs {
		changesJSON := ""
		if len(l.Changes) > 0 {
			if data, err := json.Marshal(l.Changes); err == nil {
				changesJSON = string(data)
			}
		}
		row := []string{
			strconv.FormatInt(l.ID, 10),
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			strconv.FormatInt(l.AdminID, 10),
			quoteCSV(l.AdminName),
			quoteCSV(l.AdminRole),
			quoteCSV(l.SessionID),
			quoteCSV(l.Category),
			quoteCSV(l.Severity),
			quoteCSV(l.Action),
			quoteCSV(l.Resource),
			quoteCSV(l.ResourceID),
			quoteCSV(l.Summary),
			quoteCSV(l.Detail),
			quoteCSV(l.RequestID),
			quoteCSV(l.TraceID),
			quoteCSV(l.Method),
			quoteCSV(l.Path),
			quoteCSV(l.Route),
			strconv.Itoa(l.StatusCode),
			strconv.Itoa(l.LatencyMs),
			strconv.Itoa(l.RequestSize),
			strconv.Itoa(l.ResponseSize),
			quoteCSV(l.ResponseSnippet),
			quoteCSV(l.IP),
			quoteCSV(l.Country),
			quoteCSV(l.Region),
			quoteCSV(l.City),
			quoteCSV(l.ISP),
			quoteCSV(l.UserAgent),
			quoteCSV(l.Status),
			quoteCSV(l.ErrorCode),
			quoteCSV(l.ErrorMessage),
			quoteCSV(changesJSON),
		}
		sb.WriteString(strings.Join(row, ","))
		sb.WriteString("\n")
	}
	c.String(200, sb.String())
}

func filterFromQuery(q AuditLogQuery) systemdomain.AuditFilter {
	return systemdomain.AuditFilter{
		Action: q.Action, Resource: q.Resource, Category: q.Category, Severity: q.Severity,
		Status: q.Status, StatusCode: q.StatusCode, AdminID: q.AdminID,
		IP: q.IP, Country: q.Country, RequestID: q.RequestID, TraceID: q.TraceID,
		Keyword: q.Keyword, StartTime: q.StartTime, EndTime: q.EndTime,
		Page: q.Page, Limit: q.Limit,
	}
}

func auditEntryFromContext(c *gin.Context, action, resource, resourceID, detail string) systemdomain.AuditEntry {
	method := ""
	path := ""
	route := ""
	statusCode := 0
	if c.Request != nil {
		method = c.Request.Method
		if c.Request.URL != nil {
			path = c.Request.URL.Path
		}
	}
	route = c.FullPath()
	if c.Writer != nil {
		statusCode = c.Writer.Status()
	}

	category := auditmiddleware.ClassifyAuditCategoryForRoute(route)
	severity := systemdomain.AuditSeverityInfo

	entry := systemdomain.AuditEntry{
		Action:     action,
		Category:   category,
		Severity:   severity,
		Resource:   resource,
		ResourceID: resourceID,
		Summary:    auditmiddleware.BuildAuditSummary(method, route, resource, resourceID, statusCode),
		Detail:     detail,
		Method:     method,
		Path:       path,
		Route:      route,
		StatusCode: statusCode,
		Changes:    auditmiddleware.GetAuditContextChanges(c),
		IP:         c.ClientIP(),
		UserAgent:  c.GetHeader("User-Agent"),
		Status:     systemdomain.AuditStatusSuccess,
	}
	// 管理员会话键：与 AdminAuth/AdminAccess 中间件保持一致（"admin.session"）
	if session, ok := c.Get("admin.session"); ok {
		if ctx, ok := session.(*admindomain.AccessContext); ok {
			entry.AdminID = ctx.AdminID
			entry.AdminName = ctx.Account
			entry.SessionID = ctx.TokenID
			if ctx.IsSuperAdmin {
				entry.AdminRole = "super_admin"
			} else if len(ctx.Assignments) > 0 {
				entry.AdminRole = ctx.Assignments[0].RoleKey
			}
			entry.Severity = auditmiddleware.InferAuditSeverityFor(category, method, route, statusCode, ctx.IsSuperAdmin)
		}
	}
	// 附加请求级元数据
	if rid := strings.TrimSpace(c.Writer.Header().Get("X-Request-ID")); rid != "" {
		entry.RequestID = rid
	} else {
		entry.RequestID = strings.TrimSpace(c.GetHeader("X-Request-ID"))
	}
	loc := auditmiddleware.RequestLocation(c)
	entry.Country = loc.Country
	entry.Region = loc.Region
	entry.City = loc.City
	entry.ISP = loc.ISP
	return entry
}

func (h *Handler) recordAudit(c *gin.Context, action, resource, resourceID, detail string) {
	if h.audit == nil {
		return
	}
	auditmiddleware.MarkAuditRecorded(c)
	h.audit.Record(auditEntryFromContext(c, action, resource, resourceID, detail))
}

func (h *Handler) recordAuditFailed(c *gin.Context, action, resource, resourceID, detail string) {
	if h.audit == nil {
		return
	}
	auditmiddleware.MarkAuditRecorded(c)
	entry := auditEntryFromContext(c, action, resource, resourceID, detail)
	entry.Status = systemdomain.AuditStatusFailed
	if detail != "" {
		entry.ErrorMessage = detail
	}
	h.audit.Record(entry)
}

func (h *Handler) recordAuditWithAdmin(c *gin.Context, adminID int64, adminName, action, resource, resourceID, detail, status string) {
	if h.audit == nil {
		return
	}
	auditmiddleware.MarkAuditRecorded(c)
	entry := auditEntryFromContext(c, action, resource, resourceID, detail)
	entry.AdminID = adminID
	entry.AdminName = adminName
	entry.Status = status
	if status != systemdomain.AuditStatusSuccess {
		entry.ErrorMessage = detail
	}
	h.audit.Record(entry)
}

// ─────────────────── 认证事件专用审计 ───────────────────

// AuthAuditParams 描述一条认证审计
//
//	Provider 取值：password / ldap / oidc / saml / mfa
//	Event    取值：login / register / mfa / logout
//	Status   取值：systemdomain.AuditStatusSuccess / AuditStatusFailed / AuditStatusDenied
type AuthAuditParams struct {
	AdminID     int64
	AdminName   string
	DisplayName string
	Provider    string // password / ldap / oidc / saml / mfa
	Event       string // login / register / mfa / logout
	Status      string // success / failed / denied
	Reason      string // 失败原因
	ErrorCode   string // 业务错误码
	MFARequired bool   // 登录成功但需要 MFA
}

// recordAuditAuth 统一的认证审计入口：
//
//	覆盖 密码登录 / LDAP / OIDC / SAML / 注册 / MFA / 登出，
//	生成 action="auth.<provider>.<event>[.failed]"，
//	且把 provider、account 等上下文以结构化字段写入 changes，方便审计检索。
func (h *Handler) recordAuditAuth(c *gin.Context, p AuthAuditParams) {
	if h.audit == nil {
		return
	}
	auditmiddleware.MarkAuditRecorded(c)

	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider == "" {
		provider = "password"
	}
	event := strings.ToLower(strings.TrimSpace(p.Event))
	if event == "" {
		event = "login"
	}
	status := strings.TrimSpace(p.Status)
	if status == "" {
		status = systemdomain.AuditStatusSuccess
	}

	// action key：auth.<provider>.<event>[.failed]
	action := "auth." + provider + "." + event
	if status != systemdomain.AuditStatusSuccess {
		action += ".failed"
	}

	// 摘要：覆盖 middleware 的自动推断，保证登录日志文案干净一致
	summary := buildAuthAuditSummary(provider, event, status, p)

	// resource / resourceID
	resource := "auth." + provider
	resourceID := ""
	if p.AdminID > 0 {
		resourceID = strconv.FormatInt(p.AdminID, 10)
	} else if strings.TrimSpace(p.AdminName) != "" {
		resourceID = p.AdminName
	}

	// 详细信息：展示给审计 UI 的 detail 字段
	detailParts := []string{}
	if p.AdminName != "" {
		detailParts = append(detailParts, "account="+p.AdminName)
	}
	detailParts = append(detailParts, "provider="+provider, "event="+event, "status="+status)
	if p.MFARequired {
		detailParts = append(detailParts, "mfa_required=true")
	}
	if p.Reason != "" {
		detailParts = append(detailParts, "reason="+p.Reason)
	}
	detail := strings.Join(detailParts, " ")

	entry := auditEntryFromContext(c, action, resource, resourceID, detail)
	entry.AdminID = p.AdminID
	entry.AdminName = p.AdminName
	entry.Category = "auth"
	entry.Summary = summary
	entry.Status = status
	if status != systemdomain.AuditStatusSuccess {
		if p.Reason != "" {
			entry.ErrorMessage = p.Reason
		}
		if p.ErrorCode != "" {
			entry.ErrorCode = p.ErrorCode
		}
	}
	// severity：失败的认证事件对运营要立即可见，统一按 High
	switch status {
	case systemdomain.AuditStatusFailed, systemdomain.AuditStatusDenied, systemdomain.AuditStatusBlocked:
		entry.Severity = systemdomain.AuditSeverityHigh
	default:
		// 成功登录 / 注册：info
		entry.Severity = systemdomain.AuditSeverityInfo
	}

	// 上下文结构化字段，便于 UI 过滤
	if entry.Changes == nil {
		entry.Changes = map[string]any{}
	}
	entry.Changes["provider"] = provider
	entry.Changes["auth_event"] = event
	entry.Changes["mfa_required"] = p.MFARequired
	if p.AdminName != "" {
		entry.Changes["account"] = p.AdminName
	}
	if p.DisplayName != "" {
		entry.Changes["display_name"] = p.DisplayName
	}
	if p.Reason != "" {
		entry.Changes["reason"] = p.Reason
	}

	h.audit.Record(entry)
}

// buildAuthAuditSummary 生成认证事件的人类可读摘要
//
//	示例：
//	  "管理员 superadmin 登录成功（密码）"
//	  "管理员 alice 登录失败（LDAP）：用户不存在"
//	  "管理员 bob 注册成功"
//	  "管理员 superadmin 通过 OIDC 登录"
func buildAuthAuditSummary(provider, event, status string, p AuthAuditParams) string {
	providerLabel := map[string]string{
		"password": "密码", "ldap": "LDAP", "oidc": "OIDC",
		"saml": "SAML", "mfa": "MFA",
	}[provider]
	if providerLabel == "" {
		providerLabel = strings.ToUpper(provider)
	}

	name := strings.TrimSpace(p.AdminName)
	if name == "" {
		name = "匿名"
	}

	var action string
	switch event {
	case "login":
		action = "登录"
	case "register":
		action = "注册"
	case "mfa", "verify":
		action = "二次验证"
	case "logout":
		action = "登出"
	default:
		action = event
	}

	var outcome string
	switch status {
	case systemdomain.AuditStatusSuccess:
		outcome = action + "成功"
	case systemdomain.AuditStatusDenied:
		outcome = action + "被拒"
	case systemdomain.AuditStatusBlocked:
		outcome = action + "被拦截"
	default:
		outcome = action + "失败"
	}

	builder := strings.Builder{}
	builder.WriteString("管理员 ")
	builder.WriteString(name)
	builder.WriteString(" ")
	builder.WriteString(outcome)
	builder.WriteString("（")
	builder.WriteString(providerLabel)
	builder.WriteString("）")
	if p.MFARequired && status == systemdomain.AuditStatusSuccess {
		builder.WriteString("，需二次验证")
	}
	if p.Reason != "" && status != systemdomain.AuditStatusSuccess {
		builder.WriteString("：")
		builder.WriteString(p.Reason)
	}
	return builder.String()
}

// quoteCSV 把值包在双引号里并转义内部双引号，避免逗号/换行破坏 CSV 结构
func quoteCSV(value string) string {
	if value == "" {
		return ""
	}
	escaped := strings.ReplaceAll(value, "\"", "\"\"")
	escaped = strings.ReplaceAll(escaped, "\n", " ")
	escaped = strings.ReplaceAll(escaped, "\r", " ")
	return "\"" + escaped + "\""
}

func csvEscape(value string) string {
	return strings.ReplaceAll(value, "\"", "\"\"")
}
