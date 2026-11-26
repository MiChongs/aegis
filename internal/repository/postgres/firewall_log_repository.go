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

// firewall_logs 自规则集 v2（迁移 000174）起不只记拦截：观察模式「本该拦截」、自定义规则的记录动作、
// 出站检测都落这张表，用 action 区分。凡是回答「拦了多少」的查询都必须带 action = 'block'。

const firewallLogColumns = `id, request_id, ip, method, path, query_string, user_agent, headers,
	reason, http_status, response_code,
	waf_rule_id, waf_action, waf_data,
	country, country_code, region, city, isp, asn, timezone, latitude, longitude,
	severity, blocked_at,
	action, stage, transport, app_key, anomaly_score, matches, rule_ref, rule_name`

// ──────────────────────────────────────
// 写入
// ──────────────────────────────────────

// InsertFirewallLog 插入一条防火墙事件，返回自增 ID
func (r *Repository) InsertFirewallLog(ctx context.Context, log firewalldomain.FirewallLog) (int64, error) {
	headersJSON, _ := json.Marshal(log.Headers)
	var matchesJSON []byte
	if len(log.Matches) > 0 {
		matchesJSON, _ = json.Marshal(log.Matches)
	}
	action := log.Action
	if action == "" {
		action = firewalldomain.EventActionBlock
	}
	stage := log.Stage
	if stage == "" {
		stage = firewalldomain.StageEdge
	}
	transport := log.Transport
	if transport == "" {
		transport = firewalldomain.TransportPlain
	}
	var ruleRef *int64
	if log.RuleRef > 0 {
		ruleRef = &log.RuleRef
	}

	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO firewall_logs (
			request_id, ip, method, path, query_string, user_agent, headers,
			reason, http_status, response_code,
			waf_rule_id, waf_action, waf_data,
			severity, blocked_at,
			action, stage, transport, app_key, anomaly_score, matches, rule_ref, rule_name
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		RETURNING id`,
		log.RequestID, log.IP, log.Method, log.Path, log.QueryString, log.UserAgent, headersJSON,
		log.Reason, log.HTTPStatus, log.ResponseCode,
		log.WAFRuleID, log.WAFAction, log.WAFData,
		log.Severity, log.BlockedAt,
		action, stage, transport, log.AppKey, log.AnomalyScore, matchesJSON, ruleRef, log.RuleName,
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
	return scanFirewallLog(r.pool.QueryRow(ctx, `SELECT `+firewallLogColumns+` FROM firewall_logs WHERE id=$1`, id))
}

// ListFirewallLogs 分页查询防火墙日志（支持多维过滤）
func (r *Repository) ListFirewallLogs(ctx context.Context, filter firewalldomain.FirewallLogFilter) (*firewalldomain.FirewallLogPage, error) {
	where, args := buildFirewallLogWhere(filter)

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

	sortCol := "blocked_at"
	switch filter.SortBy {
	case "ip":
		sortCol = "ip"
	case "score":
		sortCol = "anomaly_score"
	}
	sortDir := "DESC"
	if strings.EqualFold(filter.SortOrder, "asc") {
		sortDir = "ASC"
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`SELECT %s FROM firewall_logs%s ORDER BY %s %s, id DESC LIMIT $%d OFFSET $%d`,
		firewallLogColumns, where, sortCol, sortDir, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list firewall logs: %w", err)
	}
	defer rows.Close()

	items := make([]firewalldomain.FirewallLog, 0)
	for rows.Next() {
		item, err := scanFirewallLog(rows)
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
	}, rows.Err()
}

// ──────────────────────────────────────
// 聚合统计
// ──────────────────────────────────────

// FirewallLogStats 聚合统计（Top N + 时间序列）。排行与时间序列只数拦截（action = block）。
func (r *Repository) FirewallLogStats(ctx context.Context, start, end time.Time, granularity string, topN int) (*firewalldomain.FirewallStats, error) {
	if topN < 1 {
		topN = 10
	}
	if topN > 50 {
		topN = 50
	}

	stats := &firewalldomain.FirewallStats{}
	timeFilter := " WHERE blocked_at >= $1 AND blocked_at <= $2"
	blockFilter := timeFilter + " AND action = 'block'"

	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE action = 'block'),
			COUNT(*) FILTER (WHERE action = 'observe'),
			COUNT(*) FILTER (WHERE action = 'log'),
			COUNT(DISTINCT ip) FILTER (WHERE action = 'block')
		FROM firewall_logs`+timeFilter, start, end).Scan(&stats.TotalBlocked, &stats.TotalObserved, &stats.TotalLogged, &stats.UniqueIPs); err != nil {
		return nil, err
	}
	window := end.Sub(start)
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM firewall_logs WHERE blocked_at >= $1 AND blocked_at < $2 AND action = 'block'`,
		start.Add(-window), start).Scan(&stats.PreviousBlocked); err != nil {
		return nil, err
	}

	var err error
	ranked := func(col, filter string) []firewalldomain.RankedItem {
		if err != nil {
			return []firewalldomain.RankedItem{}
		}
		var items []firewalldomain.RankedItem
		items, err = r.queryRankedWhere(ctx, col, filter, start, end, topN)
		return items
	}
	stats.TopIPs = ranked("ip", blockFilter)
	stats.TopCountries = ranked("country_code", blockFilter+" AND country_code <> ''")
	stats.TopRules = ranked("waf_rule_id::TEXT", blockFilter+" AND waf_rule_id IS NOT NULL")
	stats.TopPaths = ranked("path", blockFilter)
	stats.TopReasons = ranked("reason", blockFilter)
	stats.SeverityCounts = ranked("severity", blockFilter)
	stats.ActionCounts = ranked("action", timeFilter)
	stats.StageCounts = ranked("stage", blockFilter)
	stats.TransportCounts = ranked("transport", blockFilter)
	stats.TopCustomRules = ranked("rule_name", blockFilter+" AND rule_ref IS NOT NULL AND rule_name <> ''")
	stats.TopApps = ranked("app_key", blockFilter+" AND app_key <> ''")
	if err != nil {
		return nil, err
	}

	// 规则组：从命中明细里展开（一条事件命中多个组时每组各计一次）。
	groupRows, err := r.pool.Query(ctx, `
		SELECT g.grp, COUNT(*) AS c
		FROM firewall_logs l,
			LATERAL (SELECT DISTINCT m->>'group' AS grp FROM jsonb_array_elements(COALESCE(l.matches, '[]'::jsonb)) m) g
		`+strings.Replace(blockFilter, "blocked_at", "l.blocked_at", 2)+` AND l.matches IS NOT NULL AND g.grp IS NOT NULL AND g.grp NOT IN ('', '949', '959', '980', 'baseline')
		GROUP BY g.grp ORDER BY c DESC LIMIT $3`, start, end, topN)
	if err != nil {
		return nil, err
	}
	stats.TopGroups = []firewalldomain.RankedItem{}
	for groupRows.Next() {
		var item firewalldomain.RankedItem
		if err := groupRows.Scan(&item.Key, &item.Count); err != nil {
			groupRows.Close()
			return nil, err
		}
		stats.TopGroups = append(stats.TopGroups, item)
	}
	groupRows.Close()

	trunc := "hour"
	switch granularity {
	case "day":
		trunc = "day"
	case "minute":
		trunc = "minute"
	}
	tsRows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT date_trunc('%s', blocked_at) AS t,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE severity = 'critical') AS critical,
			COUNT(*) FILTER (WHERE severity = 'high') AS high,
			COUNT(*) FILTER (WHERE severity = 'medium') AS medium,
			COUNT(*) FILTER (WHERE severity = 'low') AS low
		FROM firewall_logs%s
		GROUP BY t ORDER BY t`, trunc, blockFilter), start, end)
	if err != nil {
		return nil, err
	}
	defer tsRows.Close()
	stats.TimeSeries = []firewalldomain.TimeSeriesPoint{}
	for tsRows.Next() {
		var pt firewalldomain.TimeSeriesPoint
		if err := tsRows.Scan(&pt.Time, &pt.Count, &pt.Critical, &pt.High, &pt.Medium, &pt.Low); err != nil {
			return nil, err
		}
		stats.TimeSeries = append(stats.TimeSeries, pt)
	}
	return stats, tsRows.Err()
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
		conds = append(conds, strings.ReplaceAll(cond, "$?", fmt.Sprintf("$%d", idx)))
		args = append(args, val)
		idx++
	}

	if f.StartTime != nil {
		add("blocked_at >= $?", *f.StartTime)
	}
	if f.EndTime != nil {
		add("blocked_at <= $?", *f.EndTime)
	}
	if f.IP != "" {
		add("ip = $?", f.IP)
	}
	if f.Country != "" {
		add("country_code = $?", f.Country)
	}
	if f.Reason != "" {
		add("reason = $?", f.Reason)
	}
	if f.WAFRuleID != nil {
		// 命中明细里的任意一条都算（v1 只记了打断请求的那一条）
		add("(waf_rule_id = $? OR matches @> jsonb_build_array(jsonb_build_object('id', $?::int)))", *f.WAFRuleID)
	}
	if f.PathPattern != "" {
		add("path ILIKE $?", "%"+escapeLike(f.PathPattern)+"%")
	}
	if f.Severity != "" {
		add("severity = $?", f.Severity)
	}
	if f.Action != "" {
		add("action = $?", f.Action)
	}
	if f.Stage != "" {
		add("stage = $?", f.Stage)
	}
	if f.Transport != "" {
		add("transport = $?", f.Transport)
	}
	if f.AppKey != "" {
		add("app_key = $?", f.AppKey)
	}
	if f.RuleRef != nil {
		add("rule_ref = $?", *f.RuleRef)
	}
	if f.Keyword != "" {
		// 请求 ID 精确比较用原值，模糊匹配用转义后的值（否则带 _ 或 % 的请求 ID 永远对不上）
		conds = append(conds, fmt.Sprintf("(request_id = $%d OR path ILIKE '%%' || $%d || '%%' OR user_agent ILIKE '%%' || $%d || '%%' OR rule_name ILIKE '%%' || $%d || '%%')", idx, idx+1, idx+1, idx+1))
		args = append(args, f.Keyword, escapeLike(f.Keyword))
		idx += 2
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (r *Repository) queryRankedWhere(ctx context.Context, col, filter string, start, end time.Time, topN int) ([]firewalldomain.RankedItem, error) {
	sql := fmt.Sprintf(`SELECT %s AS key, COUNT(*) AS c FROM firewall_logs%s GROUP BY key ORDER BY c DESC LIMIT $3`, col, filter)
	rows, err := r.pool.Query(ctx, sql, start, end, topN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []firewalldomain.RankedItem{}
	for rows.Next() {
		var item firewalldomain.RankedItem
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanFirewallLog(row pgx.Row) (*firewalldomain.FirewallLog, error) {
	var l firewalldomain.FirewallLog
	var headersJSON, matchesJSON []byte
	var ruleRef *int64
	err := row.Scan(
		&l.ID, &l.RequestID, &l.IP, &l.Method, &l.Path, &l.QueryString, &l.UserAgent, &headersJSON,
		&l.Reason, &l.HTTPStatus, &l.ResponseCode,
		&l.WAFRuleID, &l.WAFAction, &l.WAFData,
		&l.Country, &l.CountryCode, &l.Region, &l.City, &l.ISP, &l.ASN, &l.Timezone, &l.Latitude, &l.Longitude,
		&l.Severity, &l.BlockedAt,
		&l.Action, &l.Stage, &l.Transport, &l.AppKey, &l.AnomalyScore, &matchesJSON, &ruleRef, &l.RuleName,
	)
	if err != nil {
		return nil, err
	}
	if len(headersJSON) > 0 {
		_ = json.Unmarshal(headersJSON, &l.Headers)
	}
	if len(matchesJSON) > 0 {
		_ = json.Unmarshal(matchesJSON, &l.Matches)
	}
	if ruleRef != nil {
		l.RuleRef = *ruleRef
	}
	return &l, nil
}
