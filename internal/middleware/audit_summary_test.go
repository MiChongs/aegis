package middleware

import (
	"net/http"
	"testing"

	systemdomain "aegis/internal/domain/system"
)

// TestAuditActionInference 核心用例：确保常见审计路径生成正确的 action / resource / summary / severity
func TestAuditActionInference(t *testing.T) {
	cases := []struct {
		name          string
		method        string
		route         string
		resource      string
		resourceID    string
		statusCode    int
		isSuperAdmin  bool
		wantCategory  string
		wantAction    string
		wantSummary   string
		wantSeverity  string
	}{
		{
			name:         "角色申请统计（POST-as-read）",
			method:       http.MethodPost,
			route:        "/api/admin/app/role-application/statistics",
			resource:     "app.role-application",
			statusCode:   http.StatusOK,
			wantCategory: "app",
			wantAction:   "app.role_application.statistics",
			wantSummary:  "查询统计 角色申请",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			name:         "角色申请批量审批",
			method:       http.MethodPost,
			route:        "/api/admin/app/role-application/batch-review",
			resource:     "app.role-application",
			statusCode:   http.StatusOK,
			wantCategory: "app",
			wantAction:   "app.role_application.batch_review",
			wantSummary:  "批量审批 角色申请",
			wantSeverity: systemdomain.AuditSeverityMedium,
		},
		{
			name:         "角色申请列表查询",
			method:       http.MethodPost,
			route:        "/api/admin/app/role-application/list",
			resource:     "app.role-application",
			statusCode:   http.StatusOK,
			wantCategory: "app",
			wantAction:   "app.role_application.list",
			wantSummary:  "查询列表 角色申请",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			name:         "用户删除",
			method:       http.MethodDelete,
			route:        "/api/admin/users/:id",
			resource:     "users",
			resourceID:   "id=42",
			statusCode:   http.StatusOK,
			wantCategory: "user",
			wantAction:   "user.delete",
			wantSummary:  "删除 用户 42",
			wantSeverity: systemdomain.AuditSeverityHigh,
		},
		{
			name:         "用户冻结",
			method:       http.MethodPost,
			route:        "/api/admin/users/:id/freeze",
			resource:     "users",
			resourceID:   "id=7",
			statusCode:   http.StatusOK,
			wantCategory: "user",
			wantAction:   "user.freeze",
			wantSummary:  "冻结 用户 7",
			wantSeverity: systemdomain.AuditSeverityMedium,
		},
		{
			name:         "系统运行时查询",
			method:       http.MethodGet,
			route:        "/api/admin/system/runtime",
			resource:     "system.runtime",
			statusCode:   http.StatusOK,
			wantCategory: "monitor",
			wantAction:   "monitor.runtime.read",
			wantSummary:  "查询 运行时",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			name:         "崩溃日志列表",
			method:       http.MethodGet,
			route:        "/api/admin/system/crashlogs",
			resource:     "system.crashlogs",
			statusCode:   http.StatusOK,
			wantCategory: "monitor",
			wantAction:   "monitor.crashlogs.read",
			wantSummary:  "查询 崩溃日志",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			// "stats" 在 registry 中归一化到 "statistics"（两者语义一致，机器键统一）
			name:         "App 统计查询",
			method:       http.MethodGet,
			route:        "/api/admin/apps/:appkey/stats",
			resource:     "apps.stats",
			statusCode:   http.StatusOK,
			wantCategory: "app",
			wantAction:   "app.statistics",
			wantSummary:  "查询统计 统计",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			name:         "App 登录审计导出",
			method:       http.MethodGet,
			route:        "/api/admin/apps/:appkey/audits/login/export",
			resource:     "apps.audits",
			statusCode:   http.StatusOK,
			wantCategory: "app",
			wantAction:   "app.audits.login.export",
			wantSummary:  "导出 审计",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			name:         "平台横幅批量删除",
			method:       http.MethodPost,
			route:        "/api/admin/system/banners/bulk-delete",
			resource:     "system.banners",
			statusCode:   http.StatusOK,
			wantCategory: "monitor",
			wantAction:   "monitor.banners.bulk_delete",
			wantSummary:  "批量删除 横幅",
			wantSeverity: systemdomain.AuditSeverityLow,
		},
		{
			name:         "管理员角色分配",
			method:       http.MethodPut,
			route:        "/api/admin/admins/:id/access",
			resource:     "admins.access",
			statusCode:   http.StatusOK,
			isSuperAdmin: true,
			wantCategory: "admin",
			wantAction:   "admin.access.update",
			wantSummary:  "更新 admins.access",
			wantSeverity: systemdomain.AuditSeverityHigh,
		},
		{
			name:         "失败 403",
			method:       http.MethodPost,
			route:        "/api/admin/users",
			resource:     "users",
			statusCode:   http.StatusForbidden,
			wantCategory: "user",
			wantAction:   "user.create",
			wantSummary:  "创建 用户（失败 403）",
			wantSeverity: systemdomain.AuditSeverityHigh,
		},
		{
			// /api/app/... 兼容路径（非 /api/admin/*）
			name:         "工作流日志查询（POST-as-read，/api/app 下）",
			method:       http.MethodPost,
			route:        "/api/app/workflow/logs",
			resource:     "app.workflow",
			statusCode:   http.StatusOK,
			wantCategory: "app",
			wantAction:   "app.workflow.logs",
			wantSummary:  "查询日志 工作流",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
		{
			// 末段 "settings" 的标签翻译成"平台设置"（resourceLabelMap 末段回退规则）
			name:         "用户侧 settings 列表查询",
			method:       http.MethodPost,
			route:        "/api/user/settings/list",
			resource:     "user.settings",
			statusCode:   http.StatusOK,
			wantCategory: "user",
			wantAction:   "user.settings.list",
			wantSummary:  "查询列表 平台设置",
			wantSeverity: systemdomain.AuditSeverityInfo,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			category := classifyAuditCategory(tc.route)
			if category != tc.wantCategory {
				t.Errorf("category = %q, want %q", category, tc.wantCategory)
			}

			action := inferAuditAction(tc.method, tc.route, category)
			if action != tc.wantAction {
				t.Errorf("action = %q, want %q", action, tc.wantAction)
			}

			summary := BuildAuditSummary(tc.method, tc.route, tc.resource, tc.resourceID, tc.statusCode)
			if summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tc.wantSummary)
			}

			severity := InferAuditSeverity(category, tc.method, tc.route, tc.statusCode, tc.isSuperAdmin)
			if severity != tc.wantSeverity {
				t.Errorf("severity = %q, want %q", severity, tc.wantSeverity)
			}
		})
	}
}

func TestEquivalentEntity(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"user", "users", true},
		{"admin", "admins", true},
		{"app", "apps", true},
		{"category", "categories", true},
		{"box", "boxes", true},
		{"role", "roles", true},
		{"user", "app", false},
		{"", "user", false},
		{"admin", "", false},
	}
	for _, tc := range cases {
		if got := equivalentEntity(tc.a, tc.b); got != tc.want {
			t.Errorf("equivalentEntity(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
