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
	// Trend 近 14 天（含今天）的应用用户登录与注册，按调用方时区分日、没有数据的日子补 0（仅超管）
	Trend []DashboardDay `json:"trend"`
	// TopApps 近 7 天登录次数最多的应用（仅超管）
	TopApps []DashboardApp `json:"topApps"`
}

// DashboardDay 工作台趋势的一天
type DashboardDay struct {
	Date          string `json:"date"`
	Logins        int64  `json:"logins"`
	Registrations int64  `json:"registrations"`
	ActiveUsers   int64  `json:"activeUsers"`
}

// DashboardApp 工作台应用活跃排行的一项
type DashboardApp struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	AppKey        string `json:"appKey"`
	Status        bool   `json:"status"`
	Users         int64  `json:"users"`
	Logins        int64  `json:"logins"`
	Registrations int64  `json:"registrations"`
}

// dashboardTrendDays 趋势窗口天数（含今天）
const dashboardTrendDays = 14

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
func (s *DashboardService) GetDashboard(ctx context.Context, adminID int64, isSuperAdmin bool, loc *time.Location) (*AdminDashboard, error) {
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
	if logs, err := s.pg.ListRecentAuditLogs(ctx, adminID, 8); err == nil {
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
	dash.Stats = s.collectStats(ctx, isSuperAdmin, loc)
	dash.Trend = []DashboardDay{}
	dash.TopApps = []DashboardApp{}
	if isSuperAdmin {
		s.collectActivity(ctx, dash, loc)
	}

	return dash, nil
}

func (s *DashboardService) collectAlerts(ctx context.Context) []DashboardAlert {
	var alerts []DashboardAlert

	// 近24小时防火墙拦截数
	if count, err := s.pg.CountRecentFirewallBlocks(ctx, 24*time.Hour); err == nil && count > 0 {
		alerts = append(alerts, DashboardAlert{
			Type: "firewall_block", Title: "防火墙拦截",
			Detail: formatCount(count) + " 次拦截（近24小时）",
			Time:   time.Now(),
		})
	}

	// 近24小时登录失败数
	if count, err := s.pg.CountRecentLoginFailures(ctx, 24*time.Hour); err == nil && count > 10 {
		alerts = append(alerts, DashboardAlert{
			Type: "login_failed", Title: "登录失败激增",
			Detail: formatCount(count) + " 次失败（近24小时）",
			Time:   time.Now(),
		})
	}

	return alerts
}

// collectStats 中「今日」按 loc 的零点起算。
func (s *DashboardService) collectStats(ctx context.Context, isSuperAdmin bool, loc *time.Location) DashboardStats {
	var stats DashboardStats
	if isSuperAdmin {
		if loc == nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		stats.TotalApps, _ = s.pg.CountApps(ctx)
		stats.TotalUsers, _ = s.pg.CountUsers(ctx)
		stats.TodayLogins, _ = s.pg.CountLoginsSince(ctx, startOfDay)
		stats.ActiveSessions, _ = s.pg.CountActiveAdminSessions(ctx)
	}
	return stats
}

// collectActivity 填趋势与应用排行；查询失败只记日志，工作台其余部分照常返回。
func (s *DashboardService) collectActivity(ctx context.Context, dash *AdminDashboard, loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	trendStart := today.AddDate(0, 0, -(dashboardTrendDays - 1))

	rows, err := s.pg.DashboardDailyActivity(ctx, trendStart, loc.String())
	if err != nil {
		s.log.Warn("工作台趋势查询失败", zap.Error(err))
	}
	byDay := make(map[string]DashboardDay, len(rows))
	for _, row := range rows {
		key := row.Day.Format(time.DateOnly)
		byDay[key] = DashboardDay{Date: key, Logins: row.Logins, Registrations: row.Registrations, ActiveUsers: row.ActiveUsers}
	}
	dash.Trend = make([]DashboardDay, 0, dashboardTrendDays)
	for i := 0; i < dashboardTrendDays; i++ {
		key := trendStart.AddDate(0, 0, i).Format(time.DateOnly)
		day, ok := byDay[key]
		if !ok {
			day = DashboardDay{Date: key}
		}
		dash.Trend = append(dash.Trend, day)
	}

	apps, err := s.pg.DashboardAppActivity(ctx, today.AddDate(0, 0, -6), 6)
	if err != nil {
		s.log.Warn("工作台应用排行查询失败", zap.Error(err))
		return
	}
	for _, app := range apps {
		dash.TopApps = append(dash.TopApps, DashboardApp{
			ID: app.ID, Name: app.Name, AppKey: app.AppKey, Status: app.Status,
			Users: app.Users, Logins: app.Logins, Registrations: app.Registrations,
		})
	}
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
