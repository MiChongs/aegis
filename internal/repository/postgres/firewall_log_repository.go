package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"

	"github.com/jackc/pgx/v5"
)

// ──────────────────────────────────────
// 写入
// ──────────────────────────────────────

// InsertFirewallLog 插入一条防火墙拦截日志，返回自增 ID
func (r *Repository) InsertFirewallLog(ctx context.Context, log firewalldomain.FirewallLog) (int64, error) {
	headersJSON, _ := json.Marshal(log.Headers)

	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO firewall_logs (
			request_id, ip, method, path, query_string, user_agent, headers,
			reason, http_status, response_code,
			waf_rule_id, waf_action, waf_data,
			severity, blocked_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id`,
		log.RequestID, log.IP, log.Method, log.Path, log.QueryString, log.UserAgent, headersJSON,
		log.Reason, log.HTTPStatus, log.ResponseCode,
		log.WAFRuleID, log.WAFAction, log.WAFData,
		log.Severity, log.BlockedAt,
	).Scan(&id)
	return id, err
}

// UpdateFirewallLogGeoIP 回填 GeoIP 地理信息（Worker 异步调用）
func (r *Repository) UpdateFirewallLogGeoIP(ctx context.Context, id int64, country, countryCode, region, city, isp, asn, timezone string, lat, lng *float64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE firewall_logs SET
			country=$2, country_code=$3, region=$4, city=$5,
			isp=$6, asn=$7, timezone=$8, latitude=$9, longitude=$10
		WHERE id=$1`,
		id, country, countryCode, region, city, isp, asn, timezone, lat, lng,
	)
	return err
}

// ──────────────────────────────────────
// 读取
// ──────────────────────────────────────

// GetFirewallLog 获取单条防火墙日志
func (r *Repository) GetFirewallLog(ctx context.Context, id int64) (*firewalldomain.FirewallLog, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, request_id, ip, method, path, query_string, user_agent, headers,
			reason, http_status, response_code,
			waf_rule_id, waf_action, waf_data,
			country, country_code, region, city, isp, asn, timezone, latitude, longitude,
			severity, blocked_at
		FROM firewall_logs WHERE id=$1`, id)
	return scanFirewallLog(row)
}

// ListFirewallLogs 分页查询防火墙日志（支持多维过滤）
func (r *Repository) ListFirewallLogs(ctx context.Context, filter firewalldomain.FirewallLogFilter) (*firewalldomain.FirewallLogPage, error) {
	where, args := buildFirewallLogWhere(filter)

	// 统计总数
	countSQL := "SELECT COUNT(*) FROM firewall_logs" + where
	var total int64
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count firewall logs: %w", err)
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	// 排序
	sortCol := "blocked_at"
	if filter.SortBy == "ip" {
		sortCol = "ip"
	}
	sortDir := "DESC"
	if strings.EqualFold(filter.SortOrder, "asc") {
		sortDir = "ASC"
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT id, request_id, ip, method, path, query_string, user_agent, headers,
			reason, http_status, response_code,
			waf_rule_id, waf_action, waf_data,
			country, country_code, region, city, isp, asn, timezone, latitude, longitude,
			severity, blocked_at
		FROM firewall_logs%s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d`,
		where, sortCol, sortDir, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list firewall logs: %w", err)
	}
	defer rows.Close()

	items := make([]firewalldomain.FirewallLog, 0)
	for rows.Next() {
		item, err := scanFirewallLogFromRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}

	return &firewalldomain.FirewallLogPage{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// ──────────────────────────────────────
// 聚合统计
// ──────────────────────────────────────

// FirewallLogStats 聚合统计（Top N + 时间序列）
func (r *Repository) FirewallLogStats(ctx context.Context, start, end time.Time, granularity string, topN int) (*firewalldomain.FirewallStats, error) {
	if topN < 1 {
		topN = 10
	}
	if topN > 50 {
		topN = 50
	}

	stats := &firewalldomain.FirewallStats{}
	timeFilter := " WHERE blocked_at >= $1 AND blocked_at <= $2"

	// 总数
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM firewall_logs"+timeFilter, start, end).Scan(&stats.TotalBlocked); err != nil {
		return nil, err
	}

	// Top IPs
	stats.TopIPs, _ = r.queryRanked(ctx, "ip", timeFilter, start, end, topN)
	// Top Countries
	stats.TopCountries, _ = r.queryRanked(ctx, "country_code", timeFilter, start, end, topN)
	// Top Rules（仅 WAF 拦截）
	stats.TopRules, _ = r.queryRankedNonEmpty(ctx, "waf_rule_id::TEXT", "waf_rule_id IS NOT NULL", timeFilter, start, end, topN)
	// Top Paths
	stats.TopPaths, _ = r.queryRanked(ctx, "path", timeFilter, start, end, topN)
	// Top Reasons
	stats.TopReasons, _ = r.queryRanked(ctx, "reason", timeFilter, start, end, topN)
	// Severity Counts
	stats.SeverityCounts, _ = r.queryRanked(ctx, "severity", timeFilter, start, end, topN)

	// 时间序列（按严重性分维度）
	trunc := "hour"
	if granularity == "day" {
		trunc = "day"
	}
	tsSQL := fmt.Sprintf(`
		SELECT date_trunc('%s', blocked_at) AS t,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE severity = 'critical') AS critical,
			COUNT(*) FILTER (WHERE severity = 'high') AS high,
			COUNT(*) FILTER (WHERE severity = 'medium') AS medium,
			COUNT(*) FILTER (WHERE severity = 'low') AS low
		FROM firewall_logs%s
		GROUP BY t ORDER BY t`, trunc, timeFilter)
	tsRows, err := r.pool.Query(ctx, tsSQL, start, end)
	if err == nil {
		defer tsRows.Close()
		for tsRows.Next() {
			var pt firewalldomain.TimeSeriesPoint
			if err := tsRows.Scan(&pt.Time, &pt.Count, &pt.Critical, &pt.High, &pt.Medium, &pt.Low); err == nil {
				stats.TimeSeries = append(stats.TimeSeries, pt)
			}
		}
	}
	if stats.TimeSeries == nil {
		stats.TimeSeries = []firewalldomain.TimeSeriesPoint{}
	}

	return stats, nil
}

// ──────────────────────────────────────
// 清理
// ──────────────────────────────────────

// DeleteFirewallLogsBefore 删除指定时间之前的日志，返回删除行数
func (r *Repository) DeleteFirewallLogsBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, "DELETE FROM firewall_logs WHERE blocked_at < $1", before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ──────────────────────────────────────
// 内部辅助
// ──────────────────────────────────────

func buildFirewallLogWhere(f firewalldomain.FirewallLogFilter) (string, []any) {
	var conds []string
	var args []any
	idx := 1

	add := func(cond string, val any) {
		conds = append(conds, fmt.Sprintf(cond, idx))
		args = append(args, val)
		idx++
	}

	if f.StartTime != nil {
		add("blocked_at >= $%d", *f.StartTime)
	}
	if f.EndTime != nil {
		add("blocked_at <= $%d", *f.EndTime)
	}
	if f.IP != "" {
		add("ip = $%d", f.IP)
	}
	if f.Country != "" {
		add("country_code = $%d", f.Country)
	}
	if f.Reason != "" {
		add("reason = $%d", f.Reason)
	}
	if f.WAFRuleID != nil {
		add("waf_rule_id = $%d", *f.WAFRuleID)
	}
	if f.PathPattern != "" {
		add("path LIKE $%d", "%"+f.PathPattern+"%")
	}
	if f.Severity != "" {
		add("severity = $%d", f.Severity)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (r *Repository) queryRanked(ctx context.Context, col, timeFilter string, start, end time.Time, topN int) ([]firewalldomain.RankedItem, error) {
	sql := fmt.Sprintf(`
		SELECT %s AS key, COUNT(*) AS c FROM firewall_logs%s
		GROUP BY key ORDER BY c DESC LIMIT $3`, col, timeFilter)
	return r.scanRanked(ctx, sql, start, end, topN)
}

func (r *Repository) queryRankedNonEmpty(ctx context.Context, col, extraCond, timeFilter string, start, end time.Time, topN int) ([]firewalldomain.RankedItem, error) {
	filter := timeFilter + " AND " + extraCond
	sql := fmt.Sprintf(`
		SELECT %s AS key, COUNT(*) AS c FROM firewall_logs%s
		GROUP BY key ORDER BY c DESC LIMIT $3`, col, filter)
	return r.scanRanked(ctx, sql, start, end, topN)
}

func (r *Repository) scanRanked(ctx context.Context, sql string, start, end time.Time, topN int) ([]firewalldomain.RankedItem, error) {
	rows, err := r.pool.Query(ctx, sql, start, end, topN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []firewalldomain.RankedItem
	for rows.Next() {
		var item firewalldomain.RankedItem
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			continue
		}
		items = append(items, item)
	}
	if items == nil {
		items = []firewalldomain.RankedItem{}
	}
	return items, nil
}

func scanFirewallLog(row pgx.Row) (*firewalldomain.FirewallLog, error) {
	var l firewalldomain.FirewallLog
	var headersJSON []byte
	err := row.Scan(
		&l.ID, &l.RequestID, &l.IP, &l.Method, &l.Path, &l.QueryString, &l.UserAgent, &headersJSON,
		&l.Reason, &l.HTTPStatus, &l.ResponseCode,
		&l.WAFRuleID, &l.WAFAction, &l.WAFData,
		&l.Country, &l.CountryCode, &l.Region, &l.City, &l.ISP, &l.ASN, &l.Timezone, &l.Latitude, &l.Longitude,
		&l.Severity, &l.BlockedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(headersJSON) > 0 {
		_ = json.Unmarshal(headersJSON, &l.Headers)
	}
	return &l, nil
}

func scanFirewallLogFromRows(rows pgx.Rows) (*firewalldomain.FirewallLog, error) {
	var l firewalldomain.FirewallLog
	var headersJSON []byte
	err := rows.Scan(
		&l.ID, &l.RequestID, &l.IP, &l.Method, &l.Path, &l.QueryString, &l.UserAgent, &headersJSON,
		&l.Reason, &l.HTTPStatus, &l.ResponseCode,
		&l.WAFRuleID, &l.WAFAction, &l.WAFData,
		&l.Country, &l.CountryCode, &l.Region, &l.City, &l.ISP, &l.ASN, &l.Timezone, &l.Latitude, &l.Longitude,
		&l.Severity, &l.BlockedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(headersJSON) > 0 {
		_ = json.Unmarshal(headersJSON, &l.Headers)
	}
	return &l, nil
}
