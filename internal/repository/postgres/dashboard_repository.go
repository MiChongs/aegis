package postgres

import (
	"context"
	"time"
)

// 管理员工作台的全站聚合。
//
// login_audit_logs 与 users 的索引都以 appid 打头，按时间直接扫全表用不上；
// 这里一律从 apps 出发 LATERAL 进每个应用的索引区间，应用再多也只读窗口内的那几页。

// DashboardDayRow 工作台趋势的一天（调用方时区下的日历日）
type DashboardDayRow struct {
	Day           time.Time
	Logins        int64
	Registrations int64
	ActiveUsers   int64
}

// DashboardAppRow 工作台应用活跃排行的一行
type DashboardAppRow struct {
	ID            int64
	Name          string
	AppKey        string
	Status        bool
	Users         int64
	Logins        int64
	Registrations int64
}

// DashboardDailyActivity 统计 since 起每天成功的登录次数、注册数与去重活跃人数。
// 注册也写一行 login_type = 'register'，登录次数里不含它。
func (r *Repository) DashboardDailyActivity(ctx context.Context, since time.Time, tz string) ([]DashboardDayRow, error) {
	rows, err := r.pool.Query(ctx, `
SELECT (l.created_at AT TIME ZONE $2)::date AS d,
       COUNT(*) FILTER (WHERE l.login_type <> 'register'),
       COUNT(*) FILTER (WHERE l.login_type = 'register'),
       COUNT(DISTINCT l.user_id)
FROM apps a
CROSS JOIN LATERAL (
    SELECT created_at, login_type, user_id
    FROM login_audit_logs
    WHERE appid = a.id AND created_at >= $1 AND status = 'success'
) l
GROUP BY 1
ORDER BY 1`, since, tz)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DashboardDayRow, 0, 16)
	for rows.Next() {
		var row DashboardDayRow
		if err := rows.Scan(&row.Day, &row.Logins, &row.Registrations, &row.ActiveUsers); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

// DashboardAppActivity 返回每个应用的用户总数与 since 起的登录、注册次数，按登录次数降序取前 limit 个。
func (r *Repository) DashboardAppActivity(ctx context.Context, since time.Time, limit int) ([]DashboardAppRow, error) {
	if limit <= 0 {
		limit = 6
	}
	rows, err := r.pool.Query(ctx, `
SELECT a.id, a.name, COALESCE(a.app_key, ''), a.status,
       (SELECT COUNT(*) FROM users u WHERE u.appid = a.id),
       l.logins, l.registrations
FROM apps a
CROSS JOIN LATERAL (
    SELECT COUNT(*) FILTER (WHERE login_type <> 'register') AS logins,
           COUNT(*) FILTER (WHERE login_type = 'register') AS registrations
    FROM login_audit_logs
    WHERE appid = a.id AND created_at >= $1 AND status = 'success'
) l
ORDER BY l.logins DESC, a.id ASC
LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DashboardAppRow, 0, limit)
	for rows.Next() {
		var row DashboardAppRow
		if err := rows.Scan(&row.ID, &row.Name, &row.AppKey, &row.Status, &row.Users, &row.Logins, &row.Registrations); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

// CountActiveAdminSessions 统计未撤销且未过期的管理员会话。
func (r *Repository) CountActiveAdminSessions(ctx context.Context) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_sessions WHERE NOT is_revoked AND expires_at > NOW()`).Scan(&total)
	return total, err
}
