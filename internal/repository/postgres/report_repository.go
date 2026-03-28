package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
)

// ──────────────────────────────────────────────────────────────────
// 1. RegistrationReport — 每日注册趋势
// ──────────────────────────────────────────────────────────────────

func (r *Repository) RegistrationReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RegistrationReport, error) {
	query := `
		SELECT date_trunc('day', created_at)::date AS d, COUNT(*)
		FROM users
		WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY d ORDER BY d`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("registration report: %w", err)
	}
	defer rows.Close()

	report := &appdomain.RegistrationReport{Series: make([]appdomain.TimeSeriesPoint, 0)}
	for rows.Next() {
		var d time.Time
		var count int64
		if err := rows.Scan(&d, &count); err != nil {
			return nil, err
		}
		report.Total += count
		report.Series = append(report.Series, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: count,
		})
	}
	return report, rows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 2. LoginReport — 每日登录成功/失败趋势
// ──────────────────────────────────────────────────────────────────

func (r *Repository) LoginReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.LoginReport, error) {
	query := `
		SELECT date_trunc('day', created_at)::date AS d,
			COUNT(*) FILTER (WHERE status = 'success') AS success,
			COUNT(*) FILTER (WHERE status = 'failure') AS failure
		FROM login_audit_logs
		WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY d ORDER BY d`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("login report: %w", err)
	}
	defer rows.Close()

	report := &appdomain.LoginReport{Series: make([]appdomain.TimeSeriesPoint, 0)}
	for rows.Next() {
		var d time.Time
		var success, failure int64
		if err := rows.Scan(&d, &success, &failure); err != nil {
			return nil, err
		}
		report.TotalSuccess += success
		report.TotalFailure += failure
		report.Series = append(report.Series, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: success + failure,
			Extra: map[string]any{"success": success, "failure": failure},
		})
	}
	return report, rows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 3. RetentionReport — 用户留存分析
// ──────────────────────────────────────────────────────────────────

func (r *Repository) RetentionReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RetentionReport, error) {
	query := `
		WITH cohorts AS (
			SELECT id, date_trunc('day', created_at)::date AS cohort_date
			FROM users
			WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		)
		SELECT c.cohort_date, COUNT(DISTINCT c.id) AS cohort_size,
			COUNT(DISTINCT CASE WHEN l.created_at::date = c.cohort_date + 1  THEN c.id END) AS d1,
			COUNT(DISTINCT CASE WHEN l.created_at::date = c.cohort_date + 3  THEN c.id END) AS d3,
			COUNT(DISTINCT CASE WHEN l.created_at::date = c.cohort_date + 7  THEN c.id END) AS d7,
			COUNT(DISTINCT CASE WHEN l.created_at::date = c.cohort_date + 14 THEN c.id END) AS d14,
			COUNT(DISTINCT CASE WHEN l.created_at::date = c.cohort_date + 30 THEN c.id END) AS d30
		FROM cohorts c
		LEFT JOIN login_audit_logs l ON l.user_id = c.id AND l.appid = $1 AND l.status = 'success'
		GROUP BY c.cohort_date ORDER BY c.cohort_date`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("retention report: %w", err)
	}
	defer rows.Close()

	days := []int{1, 3, 7, 14, 30}
	report := &appdomain.RetentionReport{Days: days, Rows: make([]appdomain.RetentionRow, 0)}
	for rows.Next() {
		var cohortDate time.Time
		var cohortSize, d1, d3, d7, d14, d30 int64
		if err := rows.Scan(&cohortDate, &cohortSize, &d1, &d3, &d7, &d14, &d30); err != nil {
			return nil, err
		}
		retained := []int64{d1, d3, d7, d14, d30}
		rates := make([]float64, len(retained))
		for i, v := range retained {
			if cohortSize > 0 {
				rates[i] = float64(v) / float64(cohortSize) * 100
			}
		}
		report.Rows = append(report.Rows, appdomain.RetentionRow{
			CohortDate: cohortDate.Format("2006-01-02"),
			CohortSize: cohortSize,
			Retained:   retained,
			Rates:      rates,
		})
	}
	return report, rows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 4. ActiveReport — DAU 活跃用户趋势
// ──────────────────────────────────────────────────────────────────

func (r *Repository) ActiveReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.ActiveReport, error) {
	query := `
		SELECT created_at::date AS d, COUNT(DISTINCT user_id)
		FROM login_audit_logs
		WHERE appid = $1 AND status = 'success' AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY d ORDER BY d`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("active report: %w", err)
	}
	defer rows.Close()

	report := &appdomain.ActiveReport{DAU: make([]appdomain.TimeSeriesPoint, 0)}
	for rows.Next() {
		var d time.Time
		var count int64
		if err := rows.Scan(&d, &count); err != nil {
			return nil, err
		}
		report.DAU = append(report.DAU, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: count,
		})
	}
	return report, rows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 5. DeviceReport — 设备 OS/浏览器分布
// ──────────────────────────────────────────────────────────────────

func (r *Repository) DeviceReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.DeviceReport, error) {
	query := `
		SELECT user_agent
		FROM login_audit_logs
		WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("device report: %w", err)
	}
	defer rows.Close()

	osMap := make(map[string]int64)
	browserMap := make(map[string]int64)
	platformMap := make(map[string]int64)
	var total int64

	for rows.Next() {
		var ua string
		if err := rows.Scan(&ua); err != nil {
			return nil, err
		}
		total++
		uaLower := strings.ToLower(ua)

		// OS 分类
		os := classifyOS(uaLower)
		osMap[os]++

		// 浏览器分类
		browser := classifyBrowser(uaLower)
		browserMap[browser]++

		// 平台分类
		platform := classifyPlatform(uaLower)
		platformMap[platform]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &appdomain.DeviceReport{
		OS:       dimensionFromMap(osMap, total),
		Browser:  dimensionFromMap(browserMap, total),
		Platform: dimensionFromMap(platformMap, total),
	}, nil
}

// classifyOS 从 User-Agent 字符串中分类操作系统
func classifyOS(ua string) string {
	switch {
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad"):
		return "iOS"
	case strings.Contains(ua, "android"):
		return "Android"
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os"):
		return "macOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return "Other"
	}
}

// classifyBrowser 从 User-Agent 字符串中分类浏览器
func classifyBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "edg"):
		return "Edge"
	case strings.Contains(ua, "chrome") && !strings.Contains(ua, "edg"):
		return "Chrome"
	case strings.Contains(ua, "firefox"):
		return "Firefox"
	case strings.Contains(ua, "safari") && !strings.Contains(ua, "chrome"):
		return "Safari"
	default:
		return "Other"
	}
}

// classifyPlatform 从 User-Agent 字符串中分类平台类型
func classifyPlatform(ua string) string {
	switch {
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "android") && strings.Contains(ua, "mobile"):
		return "Mobile"
	case strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet"):
		return "Tablet"
	default:
		return "Desktop"
	}
}

// dimensionFromMap 将 map 转换为排序的 DimensionPoint 切片
func dimensionFromMap(m map[string]int64, total int64) []appdomain.DimensionPoint {
	points := make([]appdomain.DimensionPoint, 0, len(m))
	for label, count := range m {
		pct := float64(0)
		if total > 0 {
			pct = float64(count) / float64(total) * 100
		}
		points = append(points, appdomain.DimensionPoint{
			Label:      label,
			Count:      count,
			Percentage: pct,
		})
	}
	// 按 Count 降序排序
	for i := 0; i < len(points); i++ {
		for j := i + 1; j < len(points); j++ {
			if points[j].Count > points[i].Count {
				points[i], points[j] = points[j], points[i]
			}
		}
	}
	return points
}

// ──────────────────────────────────────────────────────────────────
// 6. RegionReport — 地域（IP）分布
// ──────────────────────────────────────────────────────────────────

func (r *Repository) RegionReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RegionReport, error) {
	query := `
		SELECT COALESCE(login_ip, 'unknown') AS label, COUNT(*)
		FROM login_audit_logs
		WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY label ORDER BY COUNT(*) DESC LIMIT 50`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("region report: %w", err)
	}
	defer rows.Close()

	var total int64
	items := make([]appdomain.DimensionPoint, 0)
	for rows.Next() {
		var label string
		var count int64
		if err := rows.Scan(&label, &count); err != nil {
			return nil, err
		}
		total += count
		items = append(items, appdomain.DimensionPoint{Label: label, Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 计算百分比
	for i := range items {
		if total > 0 {
			items[i].Percentage = float64(items[i].Count) / float64(total) * 100
		}
	}
	return &appdomain.RegionReport{TopIPs: items}, nil
}

// ──────────────────────────────────────────────────────────────────
// 7. ChannelReport — 登录渠道来源分布
// ──────────────────────────────────────────────────────────────────

func (r *Repository) ChannelReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.ChannelReport, error) {
	query := `
		SELECT COALESCE(provider, 'password') AS label, COUNT(*)
		FROM login_audit_logs
		WHERE appid = $1 AND status = 'success' AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY label ORDER BY COUNT(*) DESC`
	rows, err := r.pool.Query(ctx, query, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("channel report: %w", err)
	}
	defer rows.Close()

	var total int64
	items := make([]appdomain.DimensionPoint, 0)
	for rows.Next() {
		var label string
		var count int64
		if err := rows.Scan(&label, &count); err != nil {
			return nil, err
		}
		total += count
		items = append(items, appdomain.DimensionPoint{Label: label, Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if total > 0 {
			items[i].Percentage = float64(items[i].Count) / float64(total) * 100
		}
	}
	return &appdomain.ChannelReport{Channels: items}, nil
}

// ──────────────────────────────────────────────────────────────────
// 8. PaymentReport — 支付趋势与方式分布
// ──────────────────────────────────────────────────────────────────

func (r *Repository) PaymentReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.PaymentReport, error) {
	// 按日聚合
	seriesQuery := `
		SELECT date_trunc('day', paid_at)::date AS d, COUNT(*), COALESCE(SUM(amount), 0)
		FROM payment_orders
		WHERE appid = $1 AND status = 'paid' AND paid_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY d ORDER BY d`
	rows, err := r.pool.Query(ctx, seriesQuery, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("payment report series: %w", err)
	}
	defer rows.Close()

	report := &appdomain.PaymentReport{Series: make([]appdomain.TimeSeriesPoint, 0), Methods: make([]appdomain.DimensionPoint, 0)}
	for rows.Next() {
		var d time.Time
		var orders, amount int64
		if err := rows.Scan(&d, &orders, &amount); err != nil {
			return nil, err
		}
		report.TotalOrders += orders
		report.TotalAmount += amount
		report.Series = append(report.Series, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: orders,
			Extra: map[string]any{"amount": amount, "orders": orders},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 支付方式分布
	methodQuery := `
		SELECT COALESCE(payment_method, 'unknown'), COUNT(*), COALESCE(SUM(amount), 0)
		FROM payment_orders
		WHERE appid = $1 AND status = 'paid' AND paid_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY payment_method ORDER BY SUM(amount) DESC`
	mrows, err := r.pool.Query(ctx, methodQuery, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("payment report methods: %w", err)
	}
	defer mrows.Close()

	for mrows.Next() {
		var method string
		var count, amount int64
		if err := mrows.Scan(&method, &count, &amount); err != nil {
			return nil, err
		}
		pct := float64(0)
		if report.TotalOrders > 0 {
			pct = float64(count) / float64(report.TotalOrders) * 100
		}
		report.Methods = append(report.Methods, appdomain.DimensionPoint{
			Label:      method,
			Count:      count,
			Percentage: pct,
		})
	}
	return report, mrows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 9. NotificationReport — 通知发送趋势与类型分布
// ──────────────────────────────────────────────────────────────────

func (r *Repository) NotificationReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.NotificationReport, error) {
	// 按日聚合
	seriesQuery := `
		SELECT date_trunc('day', created_at)::date AS d, COUNT(*)
		FROM notifications
		WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY d ORDER BY d`
	rows, err := r.pool.Query(ctx, seriesQuery, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("notification report series: %w", err)
	}
	defer rows.Close()

	report := &appdomain.NotificationReport{Series: make([]appdomain.TimeSeriesPoint, 0), Types: make([]appdomain.DimensionPoint, 0)}
	for rows.Next() {
		var d time.Time
		var count int64
		if err := rows.Scan(&d, &count); err != nil {
			return nil, err
		}
		report.TotalSent += count
		report.Series = append(report.Series, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 已读数
	readQuery := `
		SELECT COUNT(*)
		FROM notifications
		WHERE appid = $1 AND status = 'read' AND created_at BETWEEN $2::timestamptz AND $3::timestamptz`
	_ = r.pool.QueryRow(ctx, readQuery, appID, start, end).Scan(&report.TotalRead)

	// 类型分布
	typeQuery := `
		SELECT COALESCE(type, 'unknown') AS label, COUNT(*)
		FROM notifications
		WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz
		GROUP BY label ORDER BY COUNT(*) DESC`
	trows, err := r.pool.Query(ctx, typeQuery, appID, start, end)
	if err != nil {
		return nil, fmt.Errorf("notification report types: %w", err)
	}
	defer trows.Close()

	for trows.Next() {
		var label string
		var count int64
		if err := trows.Scan(&label, &count); err != nil {
			return nil, err
		}
		pct := float64(0)
		if report.TotalSent > 0 {
			pct = float64(count) / float64(report.TotalSent) * 100
		}
		report.Types = append(report.Types, appdomain.DimensionPoint{
			Label:      label,
			Count:      count,
			Percentage: pct,
		})
	}
	return report, trows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 10. RiskReport — 风控拦截趋势与规则分布
// ──────────────────────────────────────────────────────────────────

func (r *Repository) RiskReport(ctx context.Context, start, end time.Time) (*appdomain.RiskReport, error) {
	// firewall_logs 无 appid 字段，返回全局数据
	seriesQuery := `
		SELECT date_trunc('day', blocked_at)::date AS d, COUNT(*)
		FROM firewall_logs
		WHERE blocked_at BETWEEN $1::timestamptz AND $2::timestamptz
		GROUP BY d ORDER BY d`
	rows, err := r.pool.Query(ctx, seriesQuery, start, end)
	if err != nil {
		return nil, fmt.Errorf("risk report series: %w", err)
	}
	defer rows.Close()

	report := &appdomain.RiskReport{Series: make([]appdomain.TimeSeriesPoint, 0), Severity: make([]appdomain.DimensionPoint, 0), TopRules: make([]appdomain.DimensionPoint, 0)}
	for rows.Next() {
		var d time.Time
		var count int64
		if err := rows.Scan(&d, &count); err != nil {
			return nil, err
		}
		report.TotalBlocked += count
		report.Series = append(report.Series, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 严重等级分布
	sevQuery := `
		SELECT COALESCE(severity, 'unknown') AS label, COUNT(*)
		FROM firewall_logs
		WHERE blocked_at BETWEEN $1::timestamptz AND $2::timestamptz
		GROUP BY label ORDER BY COUNT(*) DESC`
	srows, err := r.pool.Query(ctx, sevQuery, start, end)
	if err != nil {
		return nil, fmt.Errorf("risk report severity: %w", err)
	}
	defer srows.Close()

	for srows.Next() {
		var label string
		var count int64
		if err := srows.Scan(&label, &count); err != nil {
			return nil, err
		}
		pct := float64(0)
		if report.TotalBlocked > 0 {
			pct = float64(count) / float64(report.TotalBlocked) * 100
		}
		report.Severity = append(report.Severity, appdomain.DimensionPoint{
			Label:      label,
			Count:      count,
			Percentage: pct,
		})
	}
	if err := srows.Err(); err != nil {
		return nil, err
	}

	// TOP 拦截规则
	ruleQuery := `
		SELECT COALESCE(waf_rule_id, 'unknown') AS label, COUNT(*)
		FROM firewall_logs
		WHERE blocked_at BETWEEN $1::timestamptz AND $2::timestamptz
		GROUP BY label ORDER BY COUNT(*) DESC LIMIT 20`
	rrows, err := r.pool.Query(ctx, ruleQuery, start, end)
	if err != nil {
		return nil, fmt.Errorf("risk report rules: %w", err)
	}
	defer rrows.Close()

	for rrows.Next() {
		var label string
		var count int64
		if err := rrows.Scan(&label, &count); err != nil {
			return nil, err
		}
		pct := float64(0)
		if report.TotalBlocked > 0 {
			pct = float64(count) / float64(report.TotalBlocked) * 100
		}
		report.TopRules = append(report.TopRules, appdomain.DimensionPoint{
			Label:      label,
			Count:      count,
			Percentage: pct,
		})
	}
	return report, rrows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 11. ActivityReport — 抽奖活动数据
// ──────────────────────────────────────────────────────────────────

func (r *Repository) ActivityReport(ctx context.Context, appID int64, start, end time.Time, activityID *int64) (*appdomain.ActivityReport, error) {
	// 基础统计
	baseQuery := `
		SELECT COUNT(DISTINCT d.user_id) AS participants,
			COUNT(*) AS draws,
			COUNT(*) FILTER (WHERE d.prize_id IS NOT NULL) AS wins
		FROM lottery_draws d
		JOIN lottery_activities a ON d.activity_id = a.id
		WHERE a.appid = $1 AND d.created_at BETWEEN $2::timestamptz AND $3::timestamptz`
	args := []any{appID, start, end}
	if activityID != nil {
		baseQuery += ` AND a.id = $4`
		args = append(args, *activityID)
	}

	report := &appdomain.ActivityReport{Prizes: make([]appdomain.DimensionPoint, 0)}
	var participants, draws, wins int64
	if err := r.pool.QueryRow(ctx, baseQuery, args...).Scan(&participants, &draws, &wins); err != nil {
		return nil, fmt.Errorf("activity report base: %w", err)
	}
	report.TotalParticipants = participants
	report.TotalDraws = draws
	if draws > 0 {
		report.WinRate = float64(wins) / float64(draws) * 100
	}

	// 奖品消耗分布
	prizeQuery := `
		SELECT COALESCE(p.name, '未中奖') AS label, COUNT(*)
		FROM lottery_draws d
		JOIN lottery_activities a ON d.activity_id = a.id
		LEFT JOIN lottery_prizes p ON d.prize_id = p.id
		WHERE a.appid = $1 AND d.created_at BETWEEN $2::timestamptz AND $3::timestamptz`
	prizeArgs := []any{appID, start, end}
	if activityID != nil {
		prizeQuery += ` AND a.id = $4`
		prizeArgs = append(prizeArgs, *activityID)
	}
	prizeQuery += ` GROUP BY label ORDER BY COUNT(*) DESC`

	prows, err := r.pool.Query(ctx, prizeQuery, prizeArgs...)
	if err != nil {
		return nil, fmt.Errorf("activity report prizes: %w", err)
	}
	defer prows.Close()

	for prows.Next() {
		var label string
		var count int64
		if err := prows.Scan(&label, &count); err != nil {
			return nil, err
		}
		pct := float64(0)
		if draws > 0 {
			pct = float64(count) / float64(draws) * 100
		}
		report.Prizes = append(report.Prizes, appdomain.DimensionPoint{
			Label:      label,
			Count:      count,
			Percentage: pct,
		})
	}
	return report, prows.Err()
}

// ──────────────────────────────────────────────────────────────────
// 12. FunnelReport — 用户转化漏斗
// ──────────────────────────────────────────────────────────────────

func (r *Repository) FunnelReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.FunnelReport, error) {
	report := &appdomain.FunnelReport{Steps: make([]appdomain.FunnelStep, 0, 4)}

	// 步骤1: 注册用户数
	var registered int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE appid = $1 AND created_at BETWEEN $2::timestamptz AND $3::timestamptz`,
		appID, start, end).Scan(&registered)
	if err != nil {
		return nil, fmt.Errorf("funnel step1: %w", err)
	}

	// 步骤2: 有过登录的用户数
	var loggedIn int64
	err = r.pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM login_audit_logs WHERE appid = $1 AND status = 'success' AND created_at BETWEEN $2::timestamptz AND $3::timestamptz`,
		appID, start, end).Scan(&loggedIn)
	if err != nil {
		return nil, fmt.Errorf("funnel step2: %w", err)
	}

	// 步骤3: 登录超过 3 次的活跃用户
	var activeUsers int64
	err = r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM (SELECT user_id FROM login_audit_logs WHERE appid = $1 AND status = 'success' AND created_at BETWEEN $2::timestamptz AND $3::timestamptz GROUP BY user_id HAVING COUNT(*) >= 3) sub`,
		appID, start, end).Scan(&activeUsers)
	if err != nil {
		return nil, fmt.Errorf("funnel step3: %w", err)
	}

	// 步骤4: 有付费记录的用户
	var paidUsers int64
	err = r.pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM payment_orders WHERE appid = $1 AND status = 'paid' AND created_at BETWEEN $2::timestamptz AND $3::timestamptz`,
		appID, start, end).Scan(&paidUsers)
	if err != nil {
		return nil, fmt.Errorf("funnel step4: %w", err)
	}

	steps := []struct {
		name  string
		count int64
	}{
		{"注册", registered},
		{"登录", loggedIn},
		{"活跃（>=3次登录）", activeUsers},
		{"付费", paidUsers},
	}

	prevCount := int64(0)
	for i, s := range steps {
		rate := float64(100)
		if i > 0 && prevCount > 0 {
			rate = float64(s.count) / float64(prevCount) * 100
		} else if i > 0 {
			rate = 0
		}
		report.Steps = append(report.Steps, appdomain.FunnelStep{
			Step:  s.name,
			Count: s.count,
			Rate:  rate,
		})
		prevCount = s.count
	}
	return report, nil
}

// ──────────────────────────────────────────────────────────────────
// 13. SignInReport — 签到趋势
// ──────────────────────────────────────────────────────────────────

func (r *Repository) SignInReport(ctx context.Context, appID int64, start, end time.Time) (*appdomain.RegistrationReport, error) {
	query := `
		SELECT sign_date, COUNT(*)
		FROM daily_signins
		WHERE appid = $1 AND sign_date BETWEEN $2::date AND $3::date
		GROUP BY sign_date ORDER BY sign_date`
	rows, err := r.pool.Query(ctx, query, appID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("signin report: %w", err)
	}
	defer rows.Close()

	report := &appdomain.RegistrationReport{Series: make([]appdomain.TimeSeriesPoint, 0)}
	for rows.Next() {
		var d time.Time
		var count int64
		if err := rows.Scan(&d, &count); err != nil {
			return nil, err
		}
		report.Total += count
		report.Series = append(report.Series, appdomain.TimeSeriesPoint{
			Date:  d.Format("2006-01-02"),
			Count: count,
		})
	}
	return report, rows.Err()
}
