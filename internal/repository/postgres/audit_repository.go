package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	systemdomain "aegis/internal/domain/system"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) InsertAuditLog(ctx context.Context, entry systemdomain.AuditEntry) error {
	changesJSON, _ := json.Marshal(entry.Changes)
	_, err := r.pool.Exec(ctx, `INSERT INTO admin_audit_logs (admin_id, admin_name, action, resource, resource_id, detail, changes, ip, user_agent, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		entry.AdminID, entry.AdminName, entry.Action, entry.Resource, entry.ResourceID, entry.Detail, changesJSON, entry.IP, entry.UserAgent, entry.Status)
	return err
}

func (r *Repository) ListAuditLogs(ctx context.Context, filter systemdomain.AuditFilter) (*systemdomain.AuditPage, error) {
	var conditions []string
	var args []any
	idx := 1

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
	if filter.AdminID != nil {
		conditions = append(conditions, fmt.Sprintf("admin_id = $%d", idx))
		args = append(args, *filter.AdminID)
		idx++
	}
	if filter.Keyword != "" {
		conditions = append(conditions, fmt.Sprintf("(admin_name ILIKE $%d OR detail ILIKE $%d OR resource_id ILIKE $%d)", idx, idx, idx))
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

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// 总数
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

	query := fmt.Sprintf("SELECT id, admin_id, admin_name, action, resource, resource_id, detail, changes, ip, user_agent, status, created_at FROM admin_audit_logs %s ORDER BY created_at DESC LIMIT %d OFFSET %d", where, limit, offset)
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
	row := r.pool.QueryRow(ctx, "SELECT id, admin_id, admin_name, action, resource, resource_id, detail, changes, ip, user_agent, status, created_at FROM admin_audit_logs WHERE id = $1", id)
	return scanAuditLog(row)
}

func (r *Repository) GetAuditStats(ctx context.Context) (*systemdomain.AuditStats, error) {
	stats := &systemdomain.AuditStats{}

	_ = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE").Scan(&stats.TodayCount)
	_ = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_audit_logs WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'").Scan(&stats.WeekCount)

	// Top admins
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

	// Top actions
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

type auditScanner interface {
	Scan(dest ...any) error
}

func scanAuditLog(row auditScanner) (*systemdomain.AuditLog, error) {
	var log systemdomain.AuditLog
	var changesRaw []byte
	if err := row.Scan(&log.ID, &log.AdminID, &log.AdminName, &log.Action, &log.Resource, &log.ResourceID, &log.Detail, &changesRaw, &log.IP, &log.UserAgent, &log.Status, &log.CreatedAt); err != nil {
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
