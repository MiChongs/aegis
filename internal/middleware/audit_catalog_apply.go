package middleware

import (
	"mime/multipart"
	"net/http"
	"strings"

	"aegis/internal/auditcatalog"
	appdomain "aegis/internal/domain/app"
	systemdomain "aegis/internal/domain/system"

	"github.com/gin-gonic/gin"
)

const (
	// AuditAppContextKey resolveAppID 解析出应用后放在这里，审计据此记下应用 ID 与名称。
	AuditAppContextKey         = "admin_audit.app"
	auditContextTargetNameKey  = "admin_audit.target_name"
)

// SetAuditTarget handler 指定操作对象的名称（如「远航 1.1.0」「Beta 体验计划」），
// 审计列表据此显示「编辑版本 远航 1.1.0」而不是「编辑版本 #12」。
func SetAuditTarget(c *gin.Context, name string) {
	if c == nil {
		return
	}
	if name = strings.TrimSpace(name); name != "" {
		c.Set(auditContextTargetNameKey, trimAuditString(name))
	}
}

// ApplyAuditCatalog 按操作目录补全条目：模块、类型、风险等级、所属应用、对象名称与摘要。
// 中间件的自动记录与 handler 的显式记录都经过这里，两条路径的说法因此一致。
//
// 已经由 handler 明确给出的字段（摘要、风险等级、对象名）不会被覆盖。
func ApplyAuditCatalog(c *gin.Context, entry *systemdomain.AuditEntry, explicitSummary bool) {
	entry.CatalogRev = auditcatalog.Revision
	op, known := auditcatalog.Lookup(entry.Method, entry.Route)
	if known {
		entry.Category = op.Module
		entry.Kind = op.Kind
		if !hasExplicitSeverity(c) {
			entry.Severity = op.Severity
			// 失败的身份事件对运营要立即可见
			if op.Kind == auditcatalog.KindAuth && entry.Status != systemdomain.AuditStatusSuccess {
				entry.Severity = systemdomain.AuditSeverityHigh
			}
		}
	} else {
		entry.Category = auditcatalog.ModuleFor(entry.Category)
		if entry.Kind == "" {
			entry.Kind = auditcatalog.KindWrite
			if entry.Method == http.MethodGet || entry.Method == http.MethodHead {
				entry.Kind = auditcatalog.KindRead
			}
		}
	}

	if c != nil {
		if value, ok := c.Get(AuditAppContextKey); ok {
			if app, _ := value.(*appdomain.App); app != nil {
				entry.AppID = app.ID
				entry.AppName = app.Name
			}
		}
		if value, ok := c.Get(auditContextTargetNameKey); ok {
			if name, _ := value.(string); name != "" {
				entry.TargetName = name
			}
		}
		if entry.TargetName == "" {
			if files := uploadedFiles(c); len(files) > 0 {
				entry.TargetName = files[0]["name"].(string)
				if entry.Changes == nil {
					entry.Changes = map[string]any{}
				}
				entry.Changes["files"] = files
			}
		}
	}

	if known && !explicitSummary {
		display := auditcatalog.Describe(entry.Method, entry.Route, entry.ResourceID, entry.TargetName)
		entry.Summary = strings.TrimSpace(display.OperationName + " " + display.TargetLabel)
	}
}

func hasExplicitSeverity(c *gin.Context) bool {
	if c == nil {
		return false
	}
	_, ok := c.Get(auditContextSeverityKey)
	return ok
}

// uploadedFiles 本次请求上传的文件（名称与大小）。只读 handler 已经解析过的表单，不会触发解析。
func uploadedFiles(c *gin.Context) []map[string]any {
	if c.Request == nil || c.Request.MultipartForm == nil {
		return nil
	}
	var files []map[string]any
	for _, headers := range c.Request.MultipartForm.File {
		for _, header := range headers {
			files = append(files, fileInfo(header))
		}
	}
	return files
}

func fileInfo(header *multipart.FileHeader) map[string]any {
	return map[string]any{"name": trimAuditString(header.Filename), "size": header.Size}
}
