package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"

	"github.com/jackc/pgx/v5"
)

// InsertIPBan 插入 IP 封禁记录，返回自增 ID
func (r *Repository) InsertIPBan(ctx context.Context, ban firewalldomain.IPBan) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO ip_bans (
			ip, reason, source, trigger_rule, severity, duration, expires_at, status,
			country, country_code, region, city, isp, trigger_count
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (ip) WHERE status = 'active' DO NOTHING
		RETURNING id`,
		ban.IP, ban.Reason, ban.Source, ban.TriggerRule, ban.Severity,
		ban.Duration, ban.ExpiresAt, "active",
		ban.Country, ban.CountryCode, ban.Region, ban.City, ban.ISP,
		ban.TriggerCount,
	).Scan(&id)
	if err != nil {
		// ON CONFLICT DO NOTHING 时 Scan 会返回 pgx.ErrNoRows
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// GetActiveIPBan 获取指定 IP 的活跃封禁记录
func (r *Repository) GetActiveIPBan(ctx context.Context, ip string) (*firewalldomain.IPBan, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, ip, reason, source, trigger_rule, severity, duration, expires_at,
			status, revoked_by, revoked_at,
			country, country_code, region, city, isp,
			trigger_count, created_at, updated_at
		FROM ip_bans WHERE ip=$1 AND status='active'
		LIMIT 1`, ip)
	return scanIPBan(row)
}

// GetIPBan 获取单条封禁记录（含已过期/已解封）
func (r *Repository) GetIPBan(ctx context.Context, id int64) (*firewalldomain.IPBan, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, ip, reason, source, trigger_rule, severity, duration, expires_at,
			status, revoked_by, revoked_at,
			country, country_code, region, city, isp,
			trigger_count, created_at, updated_at
		FROM ip_bans WHERE id=$1`, id)
	return scanIPBan(row)
}

// ListIPBans 分页查询封禁列表
func (r *Repository) ListIPBans(ctx context.Context, filter firewalldomain.IPBanFilter) (*firewalldomain.IPBanPage, error) {
	where, args := buildIPBanWhere(filter)

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ip_bans"+where, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count ip_bans: %w", err)
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

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT id, ip, reason, source, trigger_rule, severity, duration, expires_at,
			status, revoked_by, revoked_at,
			country, country_code, region, city, isp,
			trigger_count, created_at, updated_at
		FROM ip_bans%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list ip_bans: %w", err)
	}
	defer rows.Close()

	items := make([]firewalldomain.IPBan, 0)
	for rows.Next() {
		ban, err := scanIPBanFromRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *ban)
	}

	return &firewalldomain.IPBanPage{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// RevokeIPBan 手动解封（status → revoked）
func (r *Repository) RevokeIPBan(ctx context.Context, id int64, adminID int64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ip_bans SET status='revoked', revoked_by=$2, revoked_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND status='active'`, id, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("封禁记录不存在或已非活跃状态")
	}
	return nil
}

// ExpireIPBans 批量将已到期的封禁标记为 expired
func (r *Repository) ExpireIPBans(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE ip_bans SET status='expired', updated_at=NOW()
		WHERE status='active' AND expires_at IS NOT NULL AND expires_at <= NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountRecentBlocks 统计某 IP 在指定时间窗口内的拦截次数
func (r *Repository) CountRecentBlocks(ctx context.Context, ip string, since time.Time) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM firewall_logs WHERE ip=$1 AND blocked_at >= $2",
		ip, since,
	).Scan(&count)
	return count, err
}

// CountRecentBlocksBySeverity 统计某 IP 在指定时间窗口内指定严重性的拦截次数
func (r *Repository) CountRecentBlocksBySeverity(ctx context.Context, ip string, since time.Time, severities []string) (int, error) {
	if len(severities) == 0 {
		return r.CountRecentBlocks(ctx, ip, since)
	}
	var count int
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM firewall_logs WHERE ip=$1 AND blocked_at >= $2 AND severity = ANY($3)",
		ip, since, severities,
	).Scan(&count)
	return count, err
}

// ListActiveIPBanIPs 获取所有活跃封禁的 IP 列表（用于同步到 Redis）
func (r *Repository) ListActiveIPBanIPs(ctx context.Context) ([]firewalldomain.IPBan, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, ip, reason, source, trigger_rule, severity, duration, expires_at,
			status, revoked_by, revoked_at,
			country, country_code, region, city, isp,
			trigger_count, created_at, updated_at
		FROM ip_bans WHERE status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []firewalldomain.IPBan
	for rows.Next() {
		ban, err := scanIPBanFromRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *ban)
	}
	return items, nil
}

// ── 内部辅助 ──────────────────────────

func buildIPBanWhere(f firewalldomain.IPBanFilter) (string, []any) {
	var conds []string
	var args []any
	idx := 1

	if f.IP != "" {
		conds = append(conds, fmt.Sprintf("ip = $%d", idx))
		args = append(args, f.IP)
		idx++
	}
	if f.Status != "" && f.Status != "all" {
		conds = append(conds, fmt.Sprintf("status = $%d", idx))
		args = append(args, f.Status)
		idx++
	}
	if f.Source != "" && f.Source != "all" {
		conds = append(conds, fmt.Sprintf("source = $%d", idx))
		args = append(args, f.Source)
		idx++
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func scanIPBan(row pgx.Row) (*firewalldomain.IPBan, error) {
	var b firewalldomain.IPBan
	err := row.Scan(
		&b.ID, &b.IP, &b.Reason, &b.Source, &b.TriggerRule, &b.Severity,
		&b.Duration, &b.ExpiresAt, &b.Status, &b.RevokedBy, &b.RevokedAt,
		&b.Country, &b.CountryCode, &b.Region, &b.City, &b.ISP,
		&b.TriggerCount, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func scanIPBanFromRows(rows pgx.Rows) (*firewalldomain.IPBan, error) {
	var b firewalldomain.IPBan
	err := rows.Scan(
		&b.ID, &b.IP, &b.Reason, &b.Source, &b.TriggerRule, &b.Severity,
		&b.Duration, &b.ExpiresAt, &b.Status, &b.RevokedBy, &b.RevokedAt,
		&b.Country, &b.CountryCode, &b.Region, &b.City, &b.ISP,
		&b.TriggerCount, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}
