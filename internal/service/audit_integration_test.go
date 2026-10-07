package service

import (
	"testing"
	"time"

	"aegis/internal/auditcatalog"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
)

// 审计可读性的端到端检查：真实 Postgres 上写入、按「操作 / 查看」筛选、总览、筛选项与目录回填。
// AEGIS_TEST_PG_DSN 未设置时跳过。
func TestAuditReadabilityIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewAuditService(nil, pg)

	base := systemdomain.AuditEntry{AdminID: 1, AdminName: "superadmin", AdminRole: "super_admin", IP: "45.196.236.77",
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
		Country: "中国", City: "上海", CatalogRev: auditcatalog.Revision}
	upload := base
	upload.Method, upload.Route, upload.Path = "POST", "/api/admin/apps/:appkey/releases/assets", "/api/admin/apps/b7784e2f/releases/assets"
	upload.Category, upload.Kind, upload.Severity = "release", auditcatalog.KindWrite, "low"
	upload.Status, upload.StatusCode, upload.ErrorMessage = "failed", 400, "安装包大小应在 2 GB 以内"
	upload.AppID, upload.AppName, upload.TargetName = 10000, "远航", "voyage-1.0.9-arm64-v8a.apk"
	upload.ResourceID = "appkey=b7784e2f"
	read := base
	read.Method, read.Route, read.Path = "GET", "/api/admin/apps/:appkey/releases", "/api/admin/apps/b7784e2f/releases"
	read.Category, read.Kind, read.Severity, read.Status, read.StatusCode = "release", auditcatalog.KindRead, "info", "success", 200
	read.AppID, read.AppName = 10000, "远航"
	for _, entry := range []systemdomain.AuditEntry{upload, read} {
		if err := pg.InsertAuditLog(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	// 重构前写入的旧行：旧分类、无 kind、目录版本 0
	if _, err := pool.Exec(ctx, `INSERT INTO admin_audit_logs (admin_id, admin_name, action, category, method, path, route, resource_id, summary, status, status_code)
VALUES (1, 'superadmin', 'app.channels.users.delete', 'app', 'DELETE', '/api/admin/apps/x/channels/3/users', '/api/admin/apps/:appkey/channels/:cid/users', 'appkey=x,cid=3', '删除 apps.channels appkey=x', 'success', 200)`); err != nil {
		t.Fatal(err)
	}

	ops, err := svc.ListLogs(ctx, systemdomain.AuditFilter{Kind: "operation", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if ops.Total != 2 {
		t.Fatalf("operation view should hide reads, got %d rows", ops.Total)
	}
	var uploaded *systemdomain.AuditLog
	for i := range ops.Items {
		if ops.Items[i].Route == upload.Route {
			uploaded = &ops.Items[i]
		}
	}
	if uploaded == nil || uploaded.OperationName != "上传安装包" || uploaded.ModuleLabel != "发布中心" ||
		uploaded.TargetLabel != "voyage-1.0.9-arm64-v8a.apk" || uploaded.AppName != "远航" ||
		uploaded.Browser != "Chrome 141" || uploaded.OS != "macOS" || uploaded.Location != "中国 上海" {
		t.Fatalf("upload row not readable: %+v", uploaded)
	}

	// 旧行在回填之前也能读：目录现查得出名称，对象去掉 appkey
	all, _ := svc.ListLogs(ctx, systemdomain.AuditFilter{Limit: 20})
	for _, item := range all.Items {
		if item.Route == "/api/admin/apps/:appkey/channels/:cid/users" && (item.OperationName != "移除渠道成员" || item.TargetLabel != "#3") {
			t.Fatalf("legacy row not readable: %+v", item)
		}
	}

	svc.BackfillCatalog(ctx)
	var category, kind string
	var rev int
	if err := pool.QueryRow(ctx, `SELECT category, kind, catalog_rev FROM admin_audit_logs WHERE action = 'app.channels.users.delete'`).Scan(&category, &kind, &rev); err != nil {
		t.Fatal(err)
	}
	if category != "release" || kind != "write" || rev != auditcatalog.Revision {
		t.Fatalf("backfill should move the legacy row to release/write, got %s/%s rev %d", category, kind, rev)
	}
	release := "release"
	if page, _ := svc.ListLogs(ctx, systemdomain.AuditFilter{Category: release, Kind: "operation", Limit: 20}); page.Total != 2 {
		t.Fatalf("module filter after backfill should find 2 operations, got %d", page.Total)
	}

	if page, _ := svc.ListLogs(ctx, systemdomain.AuditFilter{Severity: "low,high", Limit: 20}); page.Total != 1 {
		t.Fatalf("multi-value severity should match the low upload only, got %d", page.Total)
	}

	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.TodayOperations != 2 || overview.TodayFailed != 1 || overview.TodayReads != 1 || overview.ActiveAdmins != 1 || len(overview.Trend) != 14 {
		t.Fatalf("unexpected overview: %+v", overview)
	}
	if overview.Trend[13].Day != time.Now().Format("2006-01-02") || overview.Trend[13].Operations != 2 {
		t.Fatalf("trend should end today with 2 operations: %+v", overview.Trend[13])
	}
	if len(overview.TopModules) == 0 || overview.TopModules[0].Label != "发布中心" {
		t.Fatalf("top modules should be labelled: %+v", overview.TopModules)
	}

	facets, err := svc.Facets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Modules) != len(auditcatalog.Modules()) || len(facets.Admins) != 1 || len(facets.Apps) != 1 || facets.Apps[0].Label != "远航" {
		t.Fatalf("unexpected facets: %+v", facets)
	}
}
