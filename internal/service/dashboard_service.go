package service

import (
	"context"
	"time"

	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// AdminDashboard 管理员工作台数据
type AdminDashboard struct {
	PendingInvitations int64            `json:"pendingInvitations"`
	PendingRoleApps    int64            `json:"pendingRoleApps"`
	RecentAuditLogs    []AuditLogEntry  `json:"recentAuditLogs"`
	Alerts             []DashboardAlert `json:"alerts"`
	Stats              DashboardStats   `json:"stats"`
}

// AuditLogEntry 审计日志摘要
type AuditLogEntry struct {
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
}

// DashboardAlert 异常提醒
type DashboardAlert struct {
	Type   string    `json:"type"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Time   time.Time `json:"time"`
}

// DashboardStats 快速统计
type DashboardStats struct {
	TotalApps      int64 `json:"totalApps"`
	TotalUsers     int64 `json:"totalUsers"`
	TodayLogins    int64 `json:"todayLogins"`
	ActiveSessions int64 `json:"activeSessions"`
}

// DashboardService 管理员工作台聚合服务
type DashboardService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

// NewDashboardService 创建工作台服务
func NewDashboardService(log *zap.Logger, pg *pgrepo.Repository) *DashboardService {
	return &DashboardService{log: log, pg: pg}
}

// GetDashboard 获取管理员工作台数据
func (s *DashboardService) GetDashboard(ctx context.Context, adminID int64, isSuperAdmin bool) (*AdminDashboard, error) {
	dash := &AdminDashboard{}

	// 待处理邀请
	if count, err := s.pg.CountPendingInvitations(ctx, adminID); err == nil {
		dash.PendingInvitations = count
	}

	// 待审核角色申请（超管/平台管理员可见）
	if isSuperAdmin {
		if count, err := s.pg.CountPendingRoleApplications(ctx); err == nil {
			dash.PendingRoleApps = count
		}
	}

	// 最近审计日志
	if logs, err := s.pg.ListRecentAuditLogs(ctx, adminID, 5); err == nil {
		for _, l := range logs {
			dash.RecentAuditLogs = append(dash.RecentAuditLogs, AuditLogEntry{
				Action: l.Action, Detail: l.Detail, IP: l.IP, CreatedAt: l.CreatedAt,
			})
		}
	}
	if dash.RecentAuditLogs == nil {
		dash.RecentAuditLogs = []AuditLogEntry{}
	}

	// 异常提醒（仅超管）
	if isSuperAdmin {
		dash.Alerts = s.collectAlerts(ctx)
	}
	if dash.Alerts == nil {
		dash.Alerts = []DashboardAlert{}
	}

	// 快速统计
	dash.Stats = s.collectStats(ctx, isSuperAdmin)

	return dash, nil
}

func (s *DashboardService) collectAlerts(ctx context.Context) []DashboardAlert {
	var alerts []DashboardAlert

	// 近24小时防火墙拦截数
	if count, err := s.pg.CountRecentFirewallBlocks(ctx, 24*time.Hour); err == nil && count > 0 {
		alerts = append(alerts, DashboardAlert{
			Type: "firewall_block", Title: "防火墙拦截",
			Detail: formatCount(count) + " 次拦截（近24小时）",
			Time: time.Now(),
		})
	}

	// 近24小时登录失败数
	if count, err := s.pg.CountRecentLoginFailures(ctx, 24*time.Hour); err == nil && count > 10 {
		alerts = append(alerts, DashboardAlert{
			Type: "login_failed", Title: "登录失败激增",
			Detail: formatCount(count) + " 次失败（近24小时）",
			Time: time.Now(),
		})
	}

	return alerts
}

func (s *DashboardService) collectStats(ctx context.Context, isSuperAdmin bool) DashboardStats {
	var stats DashboardStats
	if isSuperAdmin {
		stats.TotalApps, _ = s.pg.CountApps(ctx)
		stats.TotalUsers, _ = s.pg.CountUsers(ctx)
		stats.TodayLogins, _ = s.pg.CountTodayLogins(ctx)
	}
	return stats
}

func formatCount(n int64) string {
	if n >= 10000 {
		return string(rune('0'+(n/10000)%10)) + "." + string(rune('0'+(n/1000)%10)) + "万"
	}
	return formatInt(n)
}

func formatInt(n int64) string {
	s := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
