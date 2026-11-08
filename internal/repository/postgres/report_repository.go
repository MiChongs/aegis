package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// 报表分析中心的只读查询。
//
// 约定：所有查询的参数位一致 —— $1 应用 ID、$2 窗口开始（含）、$3 窗口结束（不含）、$4 调用方时区。
// 按日分桶一律 `(t AT TIME ZONE $4)::date`，所以「哪一天」是调用方时区下的日历日，不是数据库会话时区。
//
// 去重人数不在 SQL 里算：明细按（用户, 日）去重后流给服务层，由 roaring 位图做 DAU / WAU / MAU、
// 留存与漏斗。SQL 只做一次扫描，窗口怎么滚、口径怎么组合都不必再回数据库。

// ReportGroupRow 维度聚合的一行：键、展示名、次数、去重人数、金额（元）与第二指标
type ReportGroupRow struct {
	Key    string
	Label  string
	Count  int64
	Users  int64
	Amount float64
	Value  float64
}

// ReportDailyRow 按日聚合的一行
type ReportDailyRow struct {
	Day     time.Time
	Key     string
	A, B, C float64
}

// ReportHeatCell 星期（ISO，1 = 周一）× 小时的一格
type ReportHeatCell struct {
	Weekday int
	Hour    int
	Count   int64
}

type reportQuery struct {
	sql string
	// global 为真时不按应用过滤（防火墙日志没有应用维度），参数从 $1 起依次为开始、结束、时区
	global bool
	tz     bool
}

const loginWindow = `appid = $1 AND created_at >= $2 AND created_at < $3`

// reportGroupQueries 列依次为 key, label, count, users, amount, value
var reportGroupQueries = map[string]reportQuery{
	"users.sources": {sql: `
SELECT COALESCE(NULLIF(provider, ''), 'password'), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM login_audit_logs
WHERE ` + loginWindow + ` AND login_type = 'register' AND status = 'success'
GROUP BY 1`},
	"users.provinces": {sql: `
SELECT COALESCE(NULLIF(p.register_province, ''), ''), '', COUNT(*), COUNT(*), 0::float8, 0::float8
FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND u.created_at >= $2 AND u.created_at < $3
GROUP BY 1`},

	"login.types": {sql: `
SELECT COALESCE(NULLIF(login_type, ''), 'unknown'), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8,
       COUNT(*) FILTER (WHERE status <> 'success')::float8
FROM login_audit_logs WHERE ` + loginWindow + `
GROUP BY 1`},
	"login.providers": {sql: `
SELECT COALESCE(NULLIF(provider, ''), 'password'), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM login_audit_logs WHERE ` + loginWindow + ` AND status = 'success'
GROUP BY 1`},
	"login.failures": {sql: `
SELECT COALESCE(NULLIF(metadata->>'reason', ''), 'unknown'), '', COUNT(*), COUNT(DISTINCT COALESCE(user_id::text, metadata->>'account')), 0::float8, 0::float8
FROM login_audit_logs WHERE ` + loginWindow + ` AND status <> 'success'
GROUP BY 1`},
	"login.failure_ips": {sql: `
SELECT COALESCE(host(login_ip), ''), '', COUNT(*), COUNT(DISTINCT COALESCE(user_id::text, metadata->>'account')), 0::float8, 0::float8
FROM login_audit_logs WHERE ` + loginWindow + ` AND status <> 'success'
GROUP BY 1 ORDER BY 3 DESC LIMIT 20`},
	"login.per_user": {sql: `
SELECT n::text, '', COUNT(*), 0::bigint, 0::float8, 0::float8
FROM (SELECT COUNT(*) AS n FROM login_audit_logs WHERE ` + loginWindow + ` AND status = 'success' AND user_id IS NOT NULL GROUP BY user_id) t
GROUP BY n`},

	"region.countries": {sql: `
SELECT COALESCE(NULLIF(country_code, ''), ''), COALESCE(MAX(country), ''), COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM login_geo_events WHERE app_id = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"region.provinces": {sql: `
SELECT COALESCE(NULLIF(region, ''), ''), COALESCE(MAX(country_code), ''), COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM login_geo_events WHERE app_id = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"region.cities": {sql: `
SELECT COALESCE(NULLIF(city, ''), ''), COALESCE(MAX(region), ''), COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM login_geo_events WHERE app_id = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1 ORDER BY 4 DESC, 3 DESC LIMIT 50`},
	"region.isps": {sql: `
SELECT COALESCE(NULLIF(isp, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM login_geo_events WHERE app_id = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1 ORDER BY 4 DESC, 3 DESC LIMIT 30`},

	"channel.invites": {sql: `
SELECT COALESCE(NULLIF(source, ''), ''), COALESCE(MAX(code_kind), ''), COUNT(*), COUNT(DISTINCT inviter_id), 0::float8,
       COUNT(*) FILTER (WHERE inviter_status = 'granted')::float8
FROM invite_relations WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"channel.cardkeys": {sql: `
SELECT COALESCE(NULLIF(source, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM card_key_redemptions WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},

	"payment.statuses": {sql: `
SELECT COALESCE(NULLIF(status, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), COALESCE(SUM(amount), 0)::float8, 0::float8
FROM payment_orders WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"payment.vip": {sql: `
SELECT COALESCE(NULLIF(plan_name, ''), ''), CASE WHEN bool_or(lifetime) THEN 'lifetime' ELSE '' END, COUNT(*), COUNT(DISTINCT user_id),
       COALESCE(SUM(pay_amount), 0)::float8, COALESCE(SUM(duration_days), 0)::float8
FROM vip_transactions WHERE appid = $1 AND created_at >= $2 AND created_at < $3 AND revoked_at IS NULL
GROUP BY 1`},

	"notification.types": {sql: `
SELECT COALESCE(NULLIF(n.type, ''), ''), '', COUNT(DISTINCT n.id), COUNT(nr.user_id), 0::float8, COUNT(nr.read_at)::float8
FROM notifications n LEFT JOIN notification_receipts nr ON nr.notification_id = n.id
WHERE n.appid = $1 AND n.created_at >= $2 AND n.created_at < $3
GROUP BY 1`},
	"notification.levels": {sql: `
SELECT COALESCE(NULLIF(n.level, ''), ''), '', COUNT(DISTINCT n.id), COUNT(nr.user_id), 0::float8, COUNT(nr.read_at)::float8
FROM notifications n LEFT JOIN notification_receipts nr ON nr.notification_id = n.id
WHERE n.appid = $1 AND n.created_at >= $2 AND n.created_at < $3
GROUP BY 1`},

	"firewall.severity": {global: true, sql: `
SELECT COALESCE(NULLIF(severity, ''), ''), '', COUNT(*), COUNT(DISTINCT ip), 0::float8, 0::float8
FROM firewall_logs WHERE blocked_at >= $1 AND blocked_at < $2
GROUP BY 1`},
	"firewall.rules": {global: true, sql: `
SELECT COALESCE(waf_rule_id::text, NULLIF(reason, ''), ''), COALESCE(MAX(reason), ''), COUNT(*), COUNT(DISTINCT ip), 0::float8, 0::float8
FROM firewall_logs WHERE blocked_at >= $1 AND blocked_at < $2
GROUP BY 1 ORDER BY 3 DESC LIMIT 20`},
	"firewall.countries": {global: true, sql: `
SELECT COALESCE(NULLIF(country_code, ''), ''), COALESCE(MAX(country), ''), COUNT(*), COUNT(DISTINCT ip), 0::float8, 0::float8
FROM firewall_logs WHERE blocked_at >= $1 AND blocked_at < $2
GROUP BY 1 ORDER BY 3 DESC LIMIT 20`},
	"firewall.paths": {global: true, sql: `
SELECT COALESCE(NULLIF(path, ''), ''), '', COUNT(*), COUNT(DISTINCT ip), 0::float8, 0::float8
FROM firewall_logs WHERE blocked_at >= $1 AND blocked_at < $2
GROUP BY 1 ORDER BY 3 DESC LIMIT 20`},

	"activity.prizes": {sql: `
SELECT COALESCE(NULLIF(prize_name, ''), ''), COALESCE(MAX(prize_kind), ''), COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM lottery_draws WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1 ORDER BY 3 DESC LIMIT 30`},
	"activity.kinds": {sql: `
SELECT COALESCE(NULLIF(prize_kind, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, 0::float8
FROM lottery_draws WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"activity.sources": {sql: `
SELECT COALESCE(NULLIF(source, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, COALESCE(SUM(points_cost), 0)::float8
FROM lottery_draws WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},

	"engagement.sign_sources": {sql: `
SELECT COALESCE(NULLIF(sign_in_source, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, COALESCE(SUM(integral_reward), 0)::float8
FROM daily_signins WHERE appid = $1 AND signed_at >= $2 AND signed_at < $3
GROUP BY 1`},
	"engagement.streaks": {sql: `
SELECT s::text, '', COUNT(*), 0::bigint, 0::float8, 0::float8
FROM (SELECT MAX(consecutive_days) AS s FROM daily_signins WHERE appid = $1 AND signed_at >= $2 AND signed_at < $3 GROUP BY user_id) t
GROUP BY s`},
	"engagement.points_earned": {sql: `
SELECT COALESCE(NULLIF(category, ''), NULLIF(source_type, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, COALESCE(SUM(amount), 0)::float8
FROM integral_transactions WHERE appid = $1 AND created_at >= $2 AND created_at < $3 AND amount > 0 AND status = 'completed'
GROUP BY 1`},
	"engagement.points_spent": {sql: `
SELECT COALESCE(NULLIF(category, ''), NULLIF(source_type, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, COALESCE(SUM(-amount), 0)::float8
FROM integral_transactions WHERE appid = $1 AND created_at >= $2 AND created_at < $3 AND amount < 0 AND status = 'completed'
GROUP BY 1`},
	"engagement.rewarded": {sql: `
SELECT COALESCE(NULLIF(status, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8, COALESCE(SUM(reward_amount), 0)::float8
FROM app_rewarded_ad_views WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"engagement.rewarded_scenes": {sql: `
SELECT COALESCE(NULLIF(scene, ''), ''), '', COUNT(*), COUNT(DISTINCT user_id), 0::float8,
       COUNT(*) FILTER (WHERE status = 'granted')::float8
FROM app_rewarded_ad_views WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"engagement.splash": {sql: `
SELECT COALESCE(NULLIF(status, ''), ''), '', COUNT(*), COUNT(DISTINCT COALESCE(user_id::text, device_id)), 0::float8, COALESCE(AVG(load_ms), 0)::float8
FROM app_splash_ad_events WHERE appid = $1 AND occurred_at >= $2 AND occurred_at < $3
GROUP BY 1`},
}

// reportDailyQueries 列依次为 day, key, a, b, c
var reportDailyQueries = map[string]reportQuery{
	"login.daily": {tz: true, sql: `
SELECT (created_at AT TIME ZONE $4::text)::date, '',
       COUNT(*) FILTER (WHERE status = 'success')::float8,
       COUNT(*) FILTER (WHERE status <> 'success')::float8,
       COUNT(DISTINCT user_id) FILTER (WHERE status = 'success')::float8
FROM login_audit_logs WHERE ` + loginWindow + `
GROUP BY 1`},
	"channel.register_daily": {tz: true, sql: `
SELECT (created_at AT TIME ZONE $4::text)::date, COALESCE(NULLIF(provider, ''), 'password'), COUNT(*)::float8, 0::float8, 0::float8
FROM login_audit_logs WHERE ` + loginWindow + ` AND login_type = 'register' AND status = 'success'
GROUP BY 1, 2`},
	"payment.created_daily": {tz: true, sql: `
SELECT (created_at AT TIME ZONE $4::text)::date, '', COUNT(*)::float8,
       COUNT(*) FILTER (WHERE status = 'paid')::float8, 0::float8
FROM payment_orders WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"payment.refund_daily": {tz: true, sql: `
SELECT (COALESCE(refunded_at, updated_at) AT TIME ZONE $4::text)::date, '', COALESCE(SUM(amount), 0)::float8, COUNT(*)::float8, 0::float8
FROM payment_refunds WHERE appid = $1 AND status = 'success'
  AND COALESCE(refunded_at, updated_at) >= $2 AND COALESCE(refunded_at, updated_at) < $3
GROUP BY 1`},
	"notification.sent_daily": {tz: true, sql: `
SELECT (n.created_at AT TIME ZONE $4::text)::date, '', COUNT(DISTINCT n.id)::float8, COUNT(nr.user_id)::float8, 0::float8
FROM notifications n LEFT JOIN notification_receipts nr ON nr.notification_id = n.id
WHERE n.appid = $1 AND n.created_at >= $2 AND n.created_at < $3
GROUP BY 1`},
	"notification.read_daily": {tz: true, sql: `
SELECT (nr.read_at AT TIME ZONE $4::text)::date, '', COUNT(*)::float8, 0::float8, 0::float8
FROM notification_receipts nr JOIN notifications n ON n.id = nr.notification_id
WHERE n.appid = $1 AND nr.read_at >= $2 AND nr.read_at < $3
GROUP BY 1`},
	"firewall.daily": {global: true, tz: true, sql: `
SELECT (blocked_at AT TIME ZONE $3::text)::date, COALESCE(NULLIF(severity, ''), 'unknown'), COUNT(*)::float8, 0::float8, 0::float8
FROM firewall_logs WHERE blocked_at >= $1 AND blocked_at < $2
GROUP BY 1, 2`},
	"activity.daily": {tz: true, sql: `
SELECT (created_at AT TIME ZONE $4::text)::date, '', COUNT(*)::float8,
       COUNT(*) FILTER (WHERE prize_kind <> 'none')::float8, COALESCE(SUM(points_cost), 0)::float8
FROM lottery_draws WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
	"engagement.sign_daily": {tz: true, sql: `
SELECT (signed_at AT TIME ZONE $4::text)::date, '', COUNT(*)::float8, COALESCE(SUM(integral_reward), 0)::float8, 0::float8
FROM daily_signins WHERE appid = $1 AND signed_at >= $2 AND signed_at < $3
GROUP BY 1`},
	"engagement.points_daily": {tz: true, sql: `
SELECT (created_at AT TIME ZONE $4::text)::date, '',
       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0)::float8,
       COALESCE(SUM(-amount) FILTER (WHERE amount < 0), 0)::float8, 0::float8
FROM integral_transactions WHERE appid = $1 AND created_at >= $2 AND created_at < $3 AND status = 'completed'
GROUP BY 1`},
	"engagement.ads_daily": {tz: true, sql: `
SELECT (created_at AT TIME ZONE $4::text)::date, '', COUNT(*)::float8,
       COUNT(*) FILTER (WHERE status = 'granted')::float8, 0::float8
FROM app_rewarded_ad_views WHERE appid = $1 AND created_at >= $2 AND created_at < $3
GROUP BY 1`},
}

// reportHeatQueries 列依次为 isodow, hour, count
var reportHeatQueries = map[string]reportQuery{
	"login.heatmap": {tz: true, sql: `
SELECT EXTRACT(ISODOW FROM created_at AT TIME ZONE $4::text)::int, EXTRACT(HOUR FROM created_at AT TIME ZONE $4::text)::int, COUNT(*)
FROM login_audit_logs WHERE ` + loginWindow + ` AND status = 'success'
GROUP BY 1, 2`},
	"active.heatmap": {tz: true, sql: `
SELECT d, h, SUM(c)::bigint FROM (
  SELECT EXTRACT(ISODOW FROM created_at AT TIME ZONE $4::text)::int AS d, EXTRACT(HOUR FROM created_at AT TIME ZONE $4::text)::int AS h, COUNT(*) AS c
  FROM login_audit_logs WHERE ` + loginWindow + ` AND status = 'success' GROUP BY 1, 2
  UNION ALL
  SELECT EXTRACT(ISODOW FROM created_at AT TIME ZONE $4::text)::int, EXTRACT(HOUR FROM created_at AT TIME ZONE $4::text)::int, COUNT(*)
  FROM session_audit_logs WHERE ` + loginWindow + ` AND user_id IS NOT NULL GROUP BY 1, 2
) t GROUP BY 1, 2`},
}

func (r *Repository) reportArgs(q reportQuery, appID int64, from, to time.Time, tz string) []any {
	args := make([]any, 0, 4)
	if !q.global {
		args = append(args, appID)
	}
	args = append(args, from, to)
	if q.tz {
		args = append(args, tz)
	}
	return args
}

// ReportGroups 执行一条具名的维度聚合
func (r *Repository) ReportGroups(ctx context.Context, name string, appID int64, from, to time.Time, tz string) ([]ReportGroupRow, error) {
	q, ok := reportGroupQueries[name]
	if !ok {
		return nil, fmt.Errorf("unknown report group query %q", name)
	}
	rows, err := r.pool.Query(ctx, q.sql, r.reportArgs(q, appID, from, to, tz)...)
	if err != nil {
		return nil, fmt.Errorf("report %s: %w", name, err)
	}
	defer rows.Close()
	out := make([]ReportGroupRow, 0, 16)
	for rows.Next() {
		var g ReportGroupRow
		if err := rows.Scan(&g.Key, &g.Label, &g.Count, &g.Users, &g.Amount, &g.Value); err != nil {
			return nil, fmt.Errorf("report %s: %w", name, err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ReportDaily 执行一条具名的按日聚合
func (r *Repository) ReportDaily(ctx context.Context, name string, appID int64, from, to time.Time, tz string) ([]ReportDailyRow, error) {
	q, ok := reportDailyQueries[name]
	if !ok {
		return nil, fmt.Errorf("unknown report daily query %q", name)
	}
	rows, err := r.pool.Query(ctx, q.sql, r.reportArgs(q, appID, from, to, tz)...)
	if err != nil {
		return nil, fmt.Errorf("report %s: %w", name, err)
	}
	defer rows.Close()
	out := make([]ReportDailyRow, 0, 64)
	for rows.Next() {
		var d ReportDailyRow
		if err := rows.Scan(&d.Day, &d.Key, &d.A, &d.B, &d.C); err != nil {
			return nil, fmt.Errorf("report %s: %w", name, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ReportHeatmap 执行一条具名的星期 × 小时聚合
func (r *Repository) ReportHeatmap(ctx context.Context, name string, appID int64, from, to time.Time, tz string) ([]ReportHeatCell, error) {
	q, ok := reportHeatQueries[name]
	if !ok {
		return nil, fmt.Errorf("unknown report heatmap query %q", name)
	}
	rows, err := r.pool.Query(ctx, q.sql, r.reportArgs(q, appID, from, to, tz)...)
	if err != nil {
		return nil, fmt.Errorf("report %s: %w", name, err)
	}
	defer rows.Close()
	out := make([]ReportHeatCell, 0, 168)
	for rows.Next() {
		var c ReportHeatCell
		if err := rows.Scan(&c.Weekday, &c.Hour, &c.Count); err != nil {
			return nil, fmt.Errorf("report %s: %w", name, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// 活跃信号
const (
	ReportSignalLogin   = 1 // 登录成功（含注册后的自动登录）
	ReportSignalSession = 2 // 会话事件：签发、刷新、打开「我的」等
	ReportSignalSignIn  = 3 // 签到
)

// ReportActivityDays 窗口内每个（信号, 用户, 日）一行：三类活跃信号各自按日去重后流出
func (r *Repository) ReportActivityDays(ctx context.Context, appID int64, from, to time.Time, tz string, fn func(signal int, userID int64, day time.Time) error) error {
	const query = `
SELECT 1, user_id, (created_at AT TIME ZONE $4::text)::date FROM login_audit_logs
WHERE ` + loginWindow + ` AND status = 'success' AND user_id IS NOT NULL GROUP BY 2, 3
UNION ALL
SELECT 2, user_id, (created_at AT TIME ZONE $4::text)::date FROM session_audit_logs
WHERE ` + loginWindow + ` AND user_id IS NOT NULL GROUP BY 2, 3
UNION ALL
SELECT 3, user_id, (signed_at AT TIME ZONE $4::text)::date FROM daily_signins
WHERE appid = $1 AND signed_at >= $2 AND signed_at < $3 GROUP BY 2, 3`
	return r.reportStream(ctx, query, []any{appID, from, to, tz}, func(rows pgx.Rows) error {
		var signal int
		var userID int64
		var day time.Time
		if err := rows.Scan(&signal, &userID, &day); err != nil {
			return err
		}
		return fn(signal, userID, day)
	})
}

// ReportRegistrations 窗口内注册的用户
func (r *Repository) ReportRegistrations(ctx context.Context, appID int64, from, to time.Time, fn func(userID int64, createdAt time.Time) error) error {
	const query = `SELECT id, created_at FROM users WHERE appid = $1 AND created_at >= $2 AND created_at < $3`
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var userID int64
		var createdAt time.Time
		if err := rows.Scan(&userID, &createdAt); err != nil {
			return err
		}
		return fn(userID, createdAt)
	})
}

// ReportUsersBefore 某时刻之前注册的用户数（累计用户曲线的起点）
func (r *Repository) ReportUsersBefore(ctx context.Context, appID int64, before time.Time) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1 AND created_at < $2`, appID, before).Scan(&n)
	return n, err
}

// ReportPaidOrder 一笔已支付订单
type ReportPaidOrder struct {
	UserID   int64
	Cents    int64
	PaidAt   time.Time
	Method   string
	Provider string
	Subject  string
}

// ReportPaidOrders 窗口内到账的订单（按 paid_at：钱什么时候到的就算哪天）
func (r *Repository) ReportPaidOrders(ctx context.Context, appID int64, from, to time.Time, fn func(ReportPaidOrder) error) error {
	const query = `
SELECT COALESCE(user_id, 0), ROUND(amount * 100)::bigint, paid_at,
       COALESCE(payment_method, ''), COALESCE(provider_type, ''), COALESCE(subject, '')
FROM payment_orders
WHERE appid = $1 AND status = 'paid' AND paid_at >= $2 AND paid_at < $3`
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var o ReportPaidOrder
		if err := rows.Scan(&o.UserID, &o.Cents, &o.PaidAt, &o.Method, &o.Provider, &o.Subject); err != nil {
			return err
		}
		return fn(o)
	})
}

// ReportFirstPayments 首次付费落在 [from, to) 的用户
func (r *Repository) ReportFirstPayments(ctx context.Context, appID int64, from, to time.Time, fn func(userID int64, firstPaidAt time.Time) error) error {
	const query = `
SELECT user_id, MIN(paid_at) FROM payment_orders
WHERE appid = $1 AND status = 'paid' AND user_id IS NOT NULL AND paid_at IS NOT NULL AND paid_at < $3
GROUP BY user_id HAVING MIN(paid_at) >= $2`
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var userID int64
		var at time.Time
		if err := rows.Scan(&userID, &at); err != nil {
			return err
		}
		return fn(userID, at)
	})
}

// ReportCohortPayments 在 [from, to) 注册的用户各自的首次付费时刻（没付过的不出现）
func (r *Repository) ReportCohortPayments(ctx context.Context, appID int64, from, to time.Time, fn func(userID int64, createdAt, firstPaidAt time.Time) error) error {
	const query = `
SELECT u.id, u.created_at, MIN(o.paid_at)
FROM users u JOIN payment_orders o ON o.user_id = u.id AND o.appid = u.appid AND o.status = 'paid' AND o.paid_at IS NOT NULL
WHERE u.appid = $1 AND u.created_at >= $2 AND u.created_at < $3
GROUP BY u.id, u.created_at`
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var userID int64
		var createdAt, paidAt time.Time
		if err := rows.Scan(&userID, &createdAt, &paidAt); err != nil {
			return err
		}
		return fn(userID, createdAt, paidAt)
	})
}

// ReportEngagedUsers 窗口内参与过互动（签到、抽奖、激励广告、积分商城兑换）的用户
func (r *Repository) ReportEngagedUsers(ctx context.Context, appID int64, from, to time.Time, fn func(userID int64) error) error {
	const query = `
SELECT user_id FROM daily_signins WHERE appid = $1 AND signed_at >= $2 AND signed_at < $3
UNION
SELECT user_id FROM lottery_draws WHERE appid = $1 AND created_at >= $2 AND created_at < $3
UNION
SELECT user_id FROM app_rewarded_ad_views WHERE appid = $1 AND created_at >= $2 AND created_at < $3 AND user_id IS NOT NULL
UNION
SELECT user_id FROM integral_transactions WHERE appid = $1 AND created_at >= $2 AND created_at < $3 AND amount < 0 AND status = 'completed'`
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return err
		}
		return fn(userID)
	})
}

// ReportDeviceRow 窗口内一台设备最近一次登录时上报的信息
type ReportDeviceRow struct {
	UserID       int64
	DeviceID     string
	UserAgent    string
	Name         string
	Platform     string
	Model        string
	Manufacturer string
	Brand        string
	Codename     string
	OS           string
	OSVersion    string
	AppVersion   string
}

// ReportDevices 每台设备一行（没有设备 ID 的旧记录按「用户 + UA」视为一台）
func (r *Repository) ReportDevices(ctx context.Context, appID int64, from, to time.Time, fn func(ReportDeviceRow) error) error {
	const query = `
SELECT DISTINCT ON (k) user_id, device_id, ua, name, platform, model, manufacturer, brand, codename, os, os_version, app_version
FROM (
  SELECT COALESCE(NULLIF(device_id, ''), 'u' || COALESCE(user_id, 0)::text || ':' || md5(COALESCE(user_agent, ''))) AS k,
         COALESCE(user_id, 0) AS user_id, COALESCE(device_id, '') AS device_id, COALESCE(user_agent, '') AS ua,
         COALESCE(metadata->>'device', '') AS name,
         COALESCE(metadata->>'device_platform', '') AS platform,
         COALESCE(metadata->>'device_model', '') AS model,
         COALESCE(metadata->'device_extra'->>'manufacturer', '') AS manufacturer,
         COALESCE(metadata->'device_extra'->>'brand', '') AS brand,
         COALESCE(metadata->'device_extra'->>'codename', '') AS codename,
         COALESCE(metadata->'device_extra'->>'os', '') AS os,
         COALESCE(metadata->'device_extra'->>'osVersion', '') AS os_version,
         COALESCE(metadata->'device_extra'->>'appVersion', '') AS app_version,
         created_at
  FROM login_audit_logs
  WHERE ` + loginWindow + ` AND status = 'success'
) t
ORDER BY k, created_at DESC`
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var d ReportDeviceRow
		if err := rows.Scan(&d.UserID, &d.DeviceID, &d.UserAgent, &d.Name, &d.Platform, &d.Model,
			&d.Manufacturer, &d.Brand, &d.Codename, &d.OS, &d.OSVersion, &d.AppVersion); err != nil {
			return err
		}
		return fn(d)
	})
}

// reportSampleLimit 分布类统计最多读取的明细行数；超过时取最近的这么多行，分位数仍然可靠
const reportSampleLimit = 500000

// ReportReadLatencies 窗口内发出、已读的通知从发送到已读的分钟数
func (r *Repository) ReportReadLatencies(ctx context.Context, appID int64, from, to time.Time, fn func(minutes float64) error) error {
	query := `
SELECT EXTRACT(EPOCH FROM (nr.read_at - n.created_at)) / 60
FROM notification_receipts nr JOIN notifications n ON n.id = nr.notification_id
WHERE n.appid = $1 AND n.created_at >= $2 AND n.created_at < $3 AND nr.read_at IS NOT NULL AND nr.read_at >= n.created_at
ORDER BY n.created_at DESC LIMIT ` + fmt.Sprint(reportSampleLimit)
	return r.reportStream(ctx, query, []any{appID, from, to}, func(rows pgx.Rows) error {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return err
		}
		return fn(v)
	})
}

// ReportLotteryActivity 一个抽奖活动在窗口内的汇总
type ReportLotteryActivity struct {
	ID           int64
	Name         string
	Status       string
	Draws        int64
	Participants int64
	Wins         int64
	PointsCost   int64
	Pity         int64
}

// ReportLotteryActivities 窗口内有抽奖记录、或窗口内处于进行中的活动
func (r *Repository) ReportLotteryActivities(ctx context.Context, appID int64, from, to time.Time) ([]ReportLotteryActivity, error) {
	const query = `
SELECT a.id, a.name, a.status,
       COUNT(d.id), COUNT(DISTINCT d.user_id),
       COUNT(d.id) FILTER (WHERE d.prize_kind <> 'none'),
       COALESCE(SUM(d.points_cost), 0)::bigint,
       COUNT(d.id) FILTER (WHERE d.pity)
FROM lottery_activities a
LEFT JOIN lottery_draws d ON d.activity_id = a.id AND d.created_at >= $2 AND d.created_at < $3
WHERE a.appid = $1
GROUP BY a.id, a.name, a.status, a.sort_order
HAVING COUNT(d.id) > 0 OR a.status = 'active'
ORDER BY COUNT(d.id) DESC, a.sort_order, a.id`
	rows, err := r.pool.Query(ctx, query, appID, from, to)
	if err != nil {
		return nil, fmt.Errorf("report lottery activities: %w", err)
	}
	defer rows.Close()
	out := make([]ReportLotteryActivity, 0, 8)
	for rows.Next() {
		var a ReportLotteryActivity
		if err := rows.Scan(&a.ID, &a.Name, &a.Status, &a.Draws, &a.Participants, &a.Wins, &a.PointsCost, &a.Pity); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReportLotteryTotals 窗口内的抽奖人数（跨活动去重）
func (r *Repository) ReportLotteryTotals(ctx context.Context, appID int64, from, to time.Time) (draws, participants, wins int64, err error) {
	err = r.pool.QueryRow(ctx, `
SELECT COUNT(*), COUNT(DISTINCT user_id), COUNT(*) FILTER (WHERE prize_kind <> 'none')
FROM lottery_draws WHERE appid = $1 AND created_at >= $2 AND created_at < $3`, appID, from, to).
		Scan(&draws, &participants, &wins)
	return
}

func (r *Repository) reportStream(ctx context.Context, query string, args []any, scan func(pgx.Rows) error) error {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("report query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
