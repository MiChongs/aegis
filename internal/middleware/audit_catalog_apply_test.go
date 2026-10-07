package middleware

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"aegis/internal/auditcatalog"
	appdomain "aegis/internal/domain/app"
	systemdomain "aegis/internal/domain/system"

	"github.com/gin-gonic/gin"
)

// 上传安装包这条审计应当读作「上传安装包 <文件名>」，带上应用名，模块与风险来自目录。
func TestApplyAuditCatalogUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, _ := form.CreateFormFile("file", "voyage-1.0.9-arm64-v8a.apk")
	_, _ = part.Write([]byte("apk-bytes"))
	_ = form.Close()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/api/admin/apps/b7784e2f/releases/assets", body)
	ctx.Request.Header.Set("Content-Type", form.FormDataContentType())
	if _, err := ctx.FormFile("file"); err != nil {
		t.Fatal(err)
	}
	ctx.Set(AuditAppContextKey, &appdomain.App{ID: 10000, Name: "远航"})

	entry := systemdomain.AuditEntry{
		Method: "POST", Route: "/api/admin/apps/:appkey/releases/assets", Category: "app",
		ResourceID: "appkey=b7784e2f", Status: systemdomain.AuditStatusSuccess, Summary: "创建 发布 appkey=b7784e2f",
	}
	ApplyAuditCatalog(ctx, &entry, false)

	if entry.Category != "release" || entry.Kind != auditcatalog.KindWrite || entry.Severity != auditcatalog.SeverityLow {
		t.Fatalf("catalog fields not applied: %+v", entry)
	}
	if entry.AppID != 10000 || entry.AppName != "远航" || entry.TargetName != "voyage-1.0.9-arm64-v8a.apk" {
		t.Fatalf("app or target not captured: %+v", entry)
	}
	if entry.Summary != "上传安装包 voyage-1.0.9-arm64-v8a.apk" {
		t.Fatalf("summary should be readable, got %q", entry.Summary)
	}
	if entry.CatalogRev != auditcatalog.Revision {
		t.Fatalf("catalog revision not stamped")
	}
}

// handler 显式给出的摘要与风险等级不被目录覆盖；失败的身份事件升为高风险。
func TestApplyAuditCatalogKeepsExplicitFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/api/admin/auth/login", nil)
	entry := systemdomain.AuditEntry{Method: "POST", Route: "/api/admin/auth/login", Status: systemdomain.AuditStatusFailed, Summary: "管理员 alice 登录失败（密码）"}
	ApplyAuditCatalog(ctx, &entry, true)
	if entry.Summary != "管理员 alice 登录失败（密码）" {
		t.Fatalf("explicit summary overwritten: %q", entry.Summary)
	}
	if entry.Kind != auditcatalog.KindAuth || entry.Severity != systemdomain.AuditSeverityHigh {
		t.Fatalf("failed auth event should be auth/high, got %s/%s", entry.Kind, entry.Severity)
	}
}
