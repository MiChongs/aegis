package middleware

import (
	"fmt"
	"strings"

	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	"aegis/internal/service"

	"github.com/gin-gonic/gin"
)

// 不记录审计的路径前缀（高频轮询/只读监控）
var auditExcludePaths = []string{
	"/api/admin/system/monitor",
	"/api/admin/system/audit-logs",
	"/api/ws",
	"/healthz",
	"/readyz",
}

// AuditMiddleware 全量请求审计中间件（挂载在 AdminAccess 之后）
func AuditMiddleware(auditSvc *service.AuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if auditSvc == nil {
			return
		}

		// 跳过排除路径
		path := c.Request.URL.Path
		for _, prefix := range auditExcludePaths {
			if strings.HasPrefix(path, prefix) {
				return
			}
		}

		// 仅记录写操作（POST/PUT/DELETE）
		method := c.Request.Method
		if method == "GET" || method == "HEAD" || method == "OPTIONS" {
			return
		}

		session, ok := c.Get("admin_access")
		if !ok {
			return
		}
		ctx, ok := session.(*admindomain.AccessContext)
		if !ok || ctx == nil {
			return
		}

		status := c.Writer.Status()
		statusStr := "success"
		if status >= 400 {
			statusStr = "failed"
		}

		auditSvc.Record(systemdomain.AuditEntry{
			AdminID:    ctx.AdminID,
			AdminName:  ctx.Account,
			Action:     "request." + strings.ToLower(method),
			Resource:   "api",
			ResourceID: c.FullPath(),
			Detail:     fmt.Sprintf("%s %s → %d", method, path, status),
			IP:         c.ClientIP(),
			UserAgent:  c.GetHeader("User-Agent"),
			Status:     statusStr,
		})
	}
}
