package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/textutil"

	"github.com/jackc/pgx/v5"
)

// 审计日志读写 —— 写入时覆盖所有扩展字段，读取时完整回传供前端展示

// scrubAuditText 把条目里所有文本字段归一成合法 UTF-8。
//
// 审计的取材面就是不可信输入：User-Agent、URL 路径与查询、非 JSON 请求体都原样进来，
// 而 Postgres 的 text 列拒收非法 UTF-8（SQLSTATE 22021）—— 任何人往请求头里塞一个
// 裸字节就能让这条审计写不进去，而写入是 fire-and-forget 的，只在日志里留一条 warn。
// 「想抹掉自己的审计记录」正好是攻击者最有动机做的事，因此这道归一化必须在最后一关。
//
// changes 是 jsonb，不需要处理：encoding/json 编码时已把非法字节换成 U+FFFD。
func scrubAuditText(e *systemdomain.AuditEntry) {
	for _, field := range []*string{
		&e.AdminName, &e.AdminRole, &e.SessionID,
		&e.Action, &e.Category, &e.Severity,
		&e.Resource, &e.ResourceID,
		&e.Summary, &e.Detail,
		&e.RequestID, &e.TraceID, &e.Method, &e.Path, &e.Route,
		&e.ResponseSnippet,
		&e.IP, &e.Country, &e.Region, &e.City, &e.ISP, &e.UserAgent,
		&e.Status, &e.ErrorCode, &e.ErrorMessage,
		&e.Kind, &e.AppName, &e.TargetName,
	} {
		*field = textutil.SanitizeUTF8(*field)
	}
}

func (r *Repository) InsertAuditLog(ctx context.Context, entry systemdomain.AuditEntry) error {
	scrubAuditText(&entry)
	changesJSON, _ := json.Marshal(entry.Changes)
	_, err := r.pool.Exec(ctx, `
INSERT INTO admin_audit_logs (
    admin_id, admin_name, admin_role, session_id,
    action, category, severity,
    resource, resource_id,
    summary, detail,
    request_id, trace_id, method, path, route, status_code, latency_ms,
    request_size, response_size, response_snippet,
    ip, country, region, city, isp, user_agent,
    status, error_code, error_message,
    changes,
    kind, app_id, app_name, target_name, catalog_rev
) VALUES (
    $1,$2,$3,$4,
    $5,$6,$7,
    $8,$9,
    $10,$11,
    $12,$13,$14,$15,$16,$17,$18,
    $19,$20,$21,
    $22,$23,$24,$25,$26,$27,
    $28,$29,$30,
    $31,
    $32,$33,$34,$35,$36
)`,
		entry.AdminID, entry.AdminName, entry.AdminRole, entry.SessionID,
		entry.Action, entry.Category, nonEmpty(entry.Severity, systemdomain.AuditSeverityInfo),
		entry.Resource, entry.ResourceID,
		entry.Summary, entry.Detail,
		entry.RequestID, entry.TraceID, entry.Method, entry.Path, entry.Route, entry.StatusCode, entry.LatencyMs,
		entry.RequestSize, entry.ResponseSize, entry.ResponseSnippet,
		entry.IP, entry.Country, entry.Region, entry.City, entry.ISP, entry.UserAgent,
		nonEmpty(entry.Status, systemdomain.AuditStatusSuccess), entry.ErrorCode, entry.ErrorMessage,
		changesJSON,
		nonEmpty(entry.Kind, "write"), entry.AppID, entry.AppName, entry.TargetName, entry.CatalogRev,
	)
	return err
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func (r *Repository) ListAuditLogs(ctx context.Context, filter systemdomain.AuditFilter) (*systemdomain.AuditPage, error) {
	conditions, args, _ := buildAuditWhere(filter, 1)
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs "+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	query := fmt.Sprintf(`
SELECT %s
FROM admin_audit_logs
%s
ORDER BY created_at DESC LIMIT %d OFFSET %d`, auditColumns, where, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []systemdomain.AuditLog
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *log)
	}
	return &systemdomain.AuditPage{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

func (r *Repository) GetAuditLog(ctx context.Context, id int64) (*systemdomain.AuditLog, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+auditColumns+" FROM admin_audit_logs WHERE id = $1", id)
	return scanAuditLog(row)
}

func (r *Repository) GetAuditStats(ctx context.Context) (*systemdomain.AuditStats, error) {
	stats := &systemdomain.AuditStats{}
	_ = r.pool.QueryRow(ctx, `SELECT
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND kind <> 'read'),
COUNT(*) FILTER (WHERE kind <> 'read'),
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND status IN ('failed','denied','blocked')),
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND severity IN ('high','critical') AND kind <> 'read'),
COALESCE(AVG(latency_ms)::BIGINT, 0)
FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'`).Scan(
		&stats.TodayCount, &stats.WeekCount, &stats.FailedToday, &stats.CriticalToday, &stats.AvgLatencyMs)
	stats.TopAdmins = r.auditGroupCounts(ctx, "admin_name", "kind <> 'read' AND created_at >= CURRENT_DATE - INTERVAL '7 days'", 5)
	stats.TopActions = r.auditGroupCounts(ctx, "action", "kind <> 'read' AND created_at >= CURRENT_DATE - INTERVAL '7 days'", 5)
	stats.TopCategories = r.auditGroupCounts(ctx, "category", "kind <> 'read' AND created_at >= CURRENT_DATE - INTERVAL '7 days'", 8)
	stats.SeverityBuckets = r.auditGroupCounts(ctx, "severity", "kind <> 'read' AND created_at >= CURRENT_DATE - INTERVAL '7 days'", 8)
	return stats, nil
}

// auditGroupCounts 按某列分组计数（列名只来自本文件的常量，不接受外部输入）。
func (r *Repository) auditGroupCounts(ctx context.Context, column string, where string, limit int) []systemdomain.AuditStatItem {
	items := []systemdomain.AuditStatItem{}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %[1]s, COUNT(*) FROM admin_audit_logs
WHERE %[2]s AND %[1]s <> '' GROUP BY %[1]s ORDER BY COUNT(*) DESC LIMIT %[3]d`, column, where, limit))
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var item systemdomain.AuditStatItem
		if err := rows.Scan(&item.Key, &item.Count); err == nil {
			item.Label = item.Key
			items = append(items, item)
		}
	}
	return items
}

// GetAuditOverview 控制台顶部总览：今日操作、失败、高危、近 7 天活跃管理员、近 14 天趋势。
// 「操作」一律不含查看类请求 —— 把每次翻页都算进去，「今日操作 3000」就失去了意义。
func (r *Repository) GetAuditOverview(ctx context.Context) (*systemdomain.AuditOverview, error) {
	overview := &systemdomain.AuditOverview{Trend: []systemdomain.AuditTrendDay{}}
	if err := r.pool.QueryRow(ctx, `SELECT
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND kind <> 'read'),
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND kind <> 'read' AND status <> 'success'),
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND kind <> 'read' AND severity IN ('high','critical')),
COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE AND kind = 'read'),
COUNT(*) FILTER (WHERE kind <> 'read'),
COUNT(DISTINCT admin_id) FILTER (WHERE kind <> 'read' AND admin_id > 0)
FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '6 days'`).Scan(
		&overview.TodayOperations, &overview.TodayFailed, &overview.TodayHighRisk, &overview.TodayReads,
		&overview.WeekOperations, &overview.ActiveAdmins); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT to_char(d::date, 'YYYY-MM-DD'),
COUNT(l.id) FILTER (WHERE l.kind <> 'read'),
COUNT(l.id) FILTER (WHERE l.kind <> 'read' AND l.status <> 'success'),
COUNT(l.id) FILTER (WHERE l.kind <> 'read' AND l.severity IN ('high','critical'))
FROM generate_series(CURRENT_DATE - INTERVAL '13 days', CURRENT_DATE, INTERVAL '1 day') d
LEFT JOIN admin_audit_logs l ON l.created_at >= d AND l.created_at < d + INTERVAL '1 day'
GROUP BY d ORDER BY d`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day systemdomain.AuditTrendDay
		if err := rows.Scan(&day.Day, &day.Operations, &day.Failed, &day.HighRisk); err != nil {
			return nil, err
		}
		overview.Trend = append(overview.Trend, day)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	week := "kind <> 'read' AND created_at >= CURRENT_DATE - INTERVAL '6 days'"
	overview.TopAdmins = r.auditGroupCounts(ctx, "admin_name", week, 5)
	overview.TopModules = r.auditGroupCounts(ctx, "category", week, 8)
	return overview, nil
}

// GetAuditFacets 筛选项：近 30 天出现过的模块、操作者与应用。
func (r *Repository) GetAuditFacets(ctx context.Context) (*systemdomain.AuditFacets, error) {
	month := "created_at >= CURRENT_DATE - INTERVAL '30 days'"
	facets := &systemdomain.AuditFacets{
		Modules: r.auditGroupCounts(ctx, "category", month, 50),
		Admins:  []systemdomain.AuditStatItem{},
		Apps:    []systemdomain.AuditStatItem{},
	}
	rows, err := r.pool.Query(ctx, `SELECT admin_id, MAX(admin_name), COUNT(*) FROM admin_audit_logs
WHERE `+month+` AND admin_id > 0 GROUP BY admin_id ORDER BY COUNT(*) DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item systemdomain.AuditStatItem
		var id int64
		if err := rows.Scan(&id, &item.Label, &item.Count); err != nil {
			rows.Close()
			return nil, err
		}
		item.Key = fmt.Sprint(id)
		facets.Admins = append(facets.Admins, item)
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT app_id, MAX(app_name), COUNT(*) FROM admin_audit_logs
WHERE `+month+` AND app_id > 0 GROUP BY app_id ORDER BY COUNT(*) DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item systemdomain.AuditStatItem
		var id int64
		if err := rows.Scan(&id, &item.Label, &item.Count); err != nil {
			return nil, err
		}
		item.Key = fmt.Sprint(id)
		facets.Apps = append(facets.Apps, item)
	}
	return facets, rows.Err()
}

// AuditRoutePair 一种 (method, route) 组合在目录里的归属。
type AuditRoutePair struct {
	Method, Route, Module, Kind string
}

// ListStaleAuditRoutes 目录版本落后的日志里出现过的 (method, route) 组合。
func (r *Repository) ListStaleAuditRoutes(ctx context.Context, revision int) ([][2]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT method, route FROM admin_audit_logs WHERE catalog_rev < $1 AND route <> ''`, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pairs [][2]string
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return nil, err
		}
		pairs = append(pairs, pair)
	}
	return pairs, rows.Err()
}

// BackfillAuditRoute 把某个 (method, route) 的历史日志改到当前目录的模块与类型。
func (r *Repository) BackfillAuditRoute(ctx context.Context, pair AuditRoutePair, revision int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_audit_logs SET category = $3, kind = $4, catalog_rev = $5
WHERE method = $1 AND route = $2 AND catalog_rev < $5`, pair.Method, pair.Route, pair.Module, pair.Kind, revision)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// MarkAuditCatalogRevision 目录里没有对应接口的旧日志（路由为空、接口已下线）只标版本，不再反复扫描。
func (r *Repository) MarkAuditCatalogRevision(ctx context.Context, revision int) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_audit_logs SET catalog_rev = $1 WHERE catalog_rev < $1`, revision)
	return err
}

func (r *Repository) ListAuditLogsForExport(ctx context.Context, filter systemdomain.AuditFilter) ([]systemdomain.AuditLog, error) {
	filter.Page = 1
	filter.Limit = 5000
	page, err := r.ListAuditLogs(ctx, filter)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// buildAuditWhere 统一组装过滤条件，供 list / export / count 复用
func buildAuditWhere(filter systemdomain.AuditFilter, startIdx int) ([]string, []any, int) {
	var conditions []string
	var args []any
	idx := startIdx

	switch filter.Kind {
	case "":
	case "operation":
		conditions = append(conditions, "kind <> 'read'")
	default:
		conditions = append(conditions, fmt.Sprintf("kind = $%d", idx))
		args = append(args, filter.Kind)
		idx++
	}
	if filter.AppID != nil {
		conditions = append(conditions, fmt.Sprintf("app_id = $%d", idx))
		args = append(args, *filter.AppID)
		idx++
	}
	if filter.Action != "" {
		conditions = append(conditions, fmt.Sprintf("action LIKE $%d", idx))
		args = append(args, filter.Action+"%")
		idx++
	}
	if filter.Resource != "" {
		conditions = append(conditions, fmt.Sprintf("resource = $%d", idx))
		args = append(args, filter.Resource)
		idx++
	}
	if filter.Category != "" {
		conditions = append(conditions, fmt.Sprintf("category = $%d", idx))
		args = append(args, filter.Category)
		idx++
	}
	if filter.Severity != "" {
		// 逗号分隔多个等级：控制台「高风险」= high,critical
		levels := []string{}
		for _, level := range strings.Split(filter.Severity, ",") {
			if level = strings.TrimSpace(level); level != "" {
				levels = append(levels, level)
			}
		}
		conditions = append(conditions, fmt.Sprintf("severity = ANY($%d)", idx))
		args = append(args, levels)
		idx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", idx))
		args = append(args, filter.Status)
		idx++
	}
	if filter.StatusCode != nil {
		conditions = append(conditions, fmt.Sprintf("status_code = $%d", idx))
		args = append(args, *filter.StatusCode)
		idx++
	}
	if filter.AdminID != nil {
		conditions = append(conditions, fmt.Sprintf("admin_id = $%d", idx))
		args = append(args, *filter.AdminID)
		idx++
	}
	if filter.IP != "" {
		conditions = append(conditions, fmt.Sprintf("ip = $%d", idx))
		args = append(args, filter.IP)
		idx++
	}
	if filter.Country != "" {
		conditions = append(conditions, fmt.Sprintf("country = $%d", idx))
		args = append(args, filter.Country)
		idx++
	}
	if filter.RequestID != "" {
		conditions = append(conditions, fmt.Sprintf("request_id = $%d", idx))
		args = append(args, filter.RequestID)
		idx++
	}
	if filter.SessionID != "" {
		conditions = append(conditions, fmt.Sprintf("session_id = $%d", idx))
		args = append(args, filter.SessionID)
		idx++
	}
	if filter.TraceID != "" {
		conditions = append(conditions, fmt.Sprintf("trace_id = $%d", idx))
		args = append(args, filter.TraceID)
		idx++
	}
	if filter.Keyword != "" {
		conditions = append(conditions, fmt.Sprintf(
			"(admin_name ILIKE $%[1]d OR summary ILIKE $%[1]d OR detail ILIKE $%[1]d OR resource_id ILIKE $%[1]d OR action ILIKE $%[1]d OR path ILIKE $%[1]d OR ip::text ILIKE $%[1]d OR error_message ILIKE $%[1]d OR app_name ILIKE $%[1]d OR target_name ILIKE $%[1]d OR request_id = $%[2]d)",
			idx, idx+1))
		args = append(args, "%"+filter.Keyword+"%", strings.TrimSpace(filter.Keyword))
		idx += 2
	}
	if filter.StartTime != "" {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", idx))
		args = append(args, filter.StartTime)
		idx++
	}
	if filter.EndTime != "" {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", idx))
		args = append(args, filter.EndTime)
		idx++
	}
	return conditions, args, idx
}

// 统一的 SELECT 列顺序
const auditColumns = `id,
admin_id, admin_name, admin_role, session_id,
action, category, severity,
resource, resource_id,
summary, detail,
request_id, trace_id, method, path, route, status_code, latency_ms,
request_size, response_size, response_snippet,
ip, country, region, city, isp, user_agent,
status, error_code, error_message,
changes, created_at,
kind, app_id, app_name, target_name`

type auditScanner interface {
	Scan(dest ...any) error
}

func scanAuditLog(row auditScanner) (*systemdomain.AuditLog, error) {
	var log systemdomain.AuditLog
	var changesRaw []byte
	if err := row.Scan(
		&log.ID,
		&log.AdminID, &log.AdminName, &log.AdminRole, &log.SessionID,
		&log.Action, &log.Category, &log.Severity,
		&log.Resource, &log.ResourceID,
		&log.Summary, &log.Detail,
		&log.RequestID, &log.TraceID, &log.Method, &log.Path, &log.Route, &log.StatusCode, &log.LatencyMs,
		&log.RequestSize, &log.ResponseSize, &log.ResponseSnippet,
		&log.IP, &log.Country, &log.Region, &log.City, &log.ISP, &log.UserAgent,
		&log.Status, &log.ErrorCode, &log.ErrorMessage,
		&changesRaw, &log.CreatedAt,
		&log.Kind, &log.AppID, &log.AppName, &log.TargetName,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if len(changesRaw) > 0 {
		_ = json.Unmarshal(changesRaw, &log.Changes)
	}
	return &log, nil
}
