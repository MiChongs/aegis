package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) ListAuditLogs(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	var q AuditLogQuery
	_ = c.ShouldBindQuery(&q)
	page, err := h.audit.ListLogs(c.Request.Context(), systemdomain.AuditFilter{
		Action: q.Action, Resource: q.Resource, AdminID: q.AdminID,
		Keyword: q.Keyword, StartTime: q.StartTime, EndTime: q.EndTime,
		Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", page)
}

func (h *Handler) GetAuditLog(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
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
	if _, ok := requireSuperAdminSession(c); !ok {
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
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	var q AuditLogQuery
	_ = c.ShouldBindQuery(&q)
	logs, err := h.audit.ExportLogs(c.Request.Context(), systemdomain.AuditFilter{
		Action: q.Action, Resource: q.Resource, AdminID: q.AdminID,
		Keyword: q.Keyword, StartTime: q.StartTime, EndTime: q.EndTime,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := fmt.Sprintf("audit-logs-%s.csv", time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)

	var sb strings.Builder
	sb.WriteString("\xEF\xBB\xBF") // UTF-8 BOM
	sb.WriteString("ID,管理员ID,管理员,操作,资源,资源ID,描述,IP,状态,时间\n")
	for _, l := range logs {
		sb.WriteString(fmt.Sprintf("%d,%d,%s,%s,%s,%s,\"%s\",%s,%s,%s\n",
			l.ID, l.AdminID, l.AdminName, l.Action, l.Resource, l.ResourceID,
			strings.ReplaceAll(l.Detail, "\"", "\"\""), l.IP, l.Status,
			l.CreatedAt.Format("2006-01-02 15:04:05")))
	}
	c.String(200, sb.String())
}

// auditEntryFromContext 从 gin.Context 构建审计条目
func auditEntryFromContext(c *gin.Context, action, resource, resourceID, detail string) systemdomain.AuditEntry {
	entry := systemdomain.AuditEntry{
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Detail:     detail,
		IP:         c.ClientIP(),
		UserAgent:  c.GetHeader("User-Agent"),
		Status:     "success",
	}
	if session, ok := c.Get("admin_access"); ok {
		if ctx, ok := session.(*admindomain.AccessContext); ok {
			entry.AdminID = ctx.AdminID
			entry.AdminName = ctx.Account
		}
	}
	return entry
}

// recordAudit nil-safe 审计记录辅助方法
func (h *Handler) recordAudit(c *gin.Context, action, resource, resourceID, detail string) {
	if h.audit == nil {
		return
	}
	h.audit.Record(auditEntryFromContext(c, action, resource, resourceID, detail))
}

// recordAuditFailed 记录失败操作
func (h *Handler) recordAuditFailed(c *gin.Context, action, resource, resourceID, detail string) {
	if h.audit == nil {
		return
	}
	entry := auditEntryFromContext(c, action, resource, resourceID, detail)
	entry.Status = "failed"
	h.audit.Record(entry)
}

// recordAuditWithAdmin 手动指定管理员信息（登录场景，session 尚未设置）
func (h *Handler) recordAuditWithAdmin(c *gin.Context, adminID int64, adminName, action, resource, resourceID, detail, status string) {
	if h.audit == nil {
		return
	}
	h.audit.Record(systemdomain.AuditEntry{
		AdminID: adminID, AdminName: adminName,
		Action: action, Resource: resource, ResourceID: resourceID, Detail: detail,
		IP: c.ClientIP(), UserAgent: c.GetHeader("User-Agent"), Status: status,
	})
}
