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
    changes
) VALUES (
    $1,$2,$3,$4,
    $5,$6,$7,
    $8,$9,
    $10,$11,
    $12,$13,$14,$15,$16,$17,$18,
    $19,$20,$21,
    $22,$23,$24,$25,$26,$27,
    $28,$29,$30,
    $31
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

	_ = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE").Scan(&stats.TodayCount)
	_ = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'").Scan(&stats.WeekCount)
	_ = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE AND status IN ('failed','denied','blocked')").Scan(&stats.FailedToday)
	_ = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE AND severity IN ('high','critical')").Scan(&stats.CriticalToday)
	_ = r.pool.QueryRow(ctx, "SELECT COALESCE(AVG(latency_ms)::BIGINT, 0) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '1 day'").Scan(&stats.AvgLatencyMs)

	rows, err := r.pool.Query(ctx, "SELECT admin_name, COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days' GROUP BY admin_name ORDER BY COUNT(*) DESC LIMIT 5")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item systemdomain.AuditStatItem
			_ = rows.Scan(&item.Label, &item.Count)
			item.Key = item.Label
			stats.TopAdmins = append(stats.TopAdmins, item)
		}
	}

	rows2, err := r.pool.Query(ctx, "SELECT action, COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days' GROUP BY action ORDER BY COUNT(*) DESC LIMIT 5")
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var item systemdomain.AuditStatItem
			_ = rows2.Scan(&item.Key, &item.Count)
			item.Label = item.Key
			stats.TopActions = append(stats.TopActions, item)
		}
	}

	rows3, err := r.pool.Query(ctx, "SELECT category, COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days' AND category <> '' GROUP BY category ORDER BY COUNT(*) DESC LIMIT 8")
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var item systemdomain.AuditStatItem
			_ = rows3.Scan(&item.Key, &item.Count)
			item.Label = item.Key
			stats.TopCategories = append(stats.TopCategories, item)
		}
	}

	rows4, err := r.pool.Query(ctx, "SELECT severity, COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days' GROUP BY severity ORDER BY COUNT(*) DESC")
	if err == nil {
		defer rows4.Close()
		for rows4.Next() {
			var item systemdomain.AuditStatItem
			_ = rows4.Scan(&item.Key, &item.Count)
			item.Label = item.Key
			stats.SeverityBuckets = append(stats.SeverityBuckets, item)
		}
	}

	return stats, nil
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
		conditions = append(conditions, fmt.Sprintf("severity = $%d", idx))
		args = append(args, filter.Severity)
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
	if filter.TraceID != "" {
		conditions = append(conditions, fmt.Sprintf("trace_id = $%d", idx))
		args = append(args, filter.TraceID)
		idx++
	}
	if filter.Keyword != "" {
		conditions = append(conditions, fmt.Sprintf(
			"(admin_name ILIKE $%d OR summary ILIKE $%d OR detail ILIKE $%d OR resource_id ILIKE $%d OR action ILIKE $%d OR resource ILIKE $%d OR ip::text ILIKE $%d OR user_agent ILIKE $%d OR error_message ILIKE $%d OR changes::text ILIKE $%d)",
			idx, idx, idx, idx, idx, idx, idx, idx, idx, idx))
		args = append(args, "%"+filter.Keyword+"%")
		idx++
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
changes, created_at`

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
