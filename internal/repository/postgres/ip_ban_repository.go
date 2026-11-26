package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"

	"github.com/jackc/pgx/v5"
)

const ipBanColumns = `id, ip, reason, source, mode, trigger_rule, severity, duration, expires_at,
	status, revoked_by, revoked_at,
	country, country_code, region, city, isp,
	trigger_count, created_at, updated_at, revoke_note, delay_ms, updated_by`

// InsertIPBan 插入 IP 封禁记录，返回自增 ID；该 IP 已有生效中的封禁时返回 0。
func (r *Repository) InsertIPBan(ctx context.Context, ban firewalldomain.IPBan) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO ip_bans (
			ip, reason, source, mode, trigger_rule, severity, duration, expires_at, status,
			country, country_code, region, city, isp, trigger_count, delay_ms
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (ip) WHERE status = 'active' DO NOTHING
		RETURNING id`,
		ban.IP, ban.Reason, ban.Source, ban.Mode, ban.TriggerRule, ban.Severity,
		ban.Duration, ban.ExpiresAt, "active",
		ban.Country, ban.CountryCode, ban.Region, ban.City, ban.ISP,
		ban.TriggerCount, ban.DelayMs,
	).Scan(&id)
	if err != nil {
		// ON CONFLICT DO NOTHING 时 Scan 会返回 pgx.ErrNoRows
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// GetActiveIPBan 获取指定 IP 的活跃封禁记录；没有时返回 nil, nil。
func (r *Repository) GetActiveIPBan(ctx context.Context, ip string) (*firewalldomain.IPBan, error) {
	ban, err := scanIPBan(r.pool.QueryRow(ctx, `SELECT `+ipBanColumns+` FROM ip_bans WHERE ip=$1 AND status='active' LIMIT 1`, ip))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ban, err
}

// GetIPBan 获取单条封禁记录（含已过期 / 已解封）；不存在返回 nil, nil。
func (r *Repository) GetIPBan(ctx context.Context, id int64) (*firewalldomain.IPBan, error) {
	ban, err := scanIPBan(r.pool.QueryRow(ctx, `SELECT `+ipBanColumns+` FROM ip_bans WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ban, err
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
	if pageSize > 500 {
		pageSize = 500
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	order := "created_at DESC, id DESC"
	switch filter.Sort {
	case "created_asc":
		order = "created_at ASC, id ASC"
	case "expires_asc":
		order = "expires_at ASC NULLS LAST, id DESC"
	case "ip":
		order = "ip ASC, id DESC"
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`SELECT %s FROM ip_bans%s ORDER BY %s LIMIT $%d OFFSET $%d`,
		ipBanColumns, where, order, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list ip_bans: %w", err)
	}
	defer rows.Close()

	items := make([]firewalldomain.IPBan, 0)
	for rows.Next() {
		ban, err := scanIPBan(rows)
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
	}, rows.Err()
}

// CountIPBans 按筛选计数（批量解封前的二次确认用）
func (r *Repository) CountIPBans(ctx context.Context, filter firewalldomain.IPBanFilter) (int64, error) {
	where, args := buildIPBanWhere(filter)
	var total int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ip_bans"+where, args...).Scan(&total)
	return total, err
}

// ListIPBanHistory 某个 IP 的全部封禁记录（最新在前）
func (r *Repository) ListIPBanHistory(ctx context.Context, ip string, limit int) ([]firewalldomain.IPBan, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT `+ipBanColumns+` FROM ip_bans WHERE ip=$1 ORDER BY created_at DESC, id DESC LIMIT $2`, ip, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]firewalldomain.IPBan, 0)
	for rows.Next() {
		ban, err := scanIPBan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *ban)
	}
	return items, rows.Err()
}

// RevokeIPBan 手动解封（status → revoked）
func (r *Repository) RevokeIPBan(ctx context.Context, id int64, adminID int64) error {
	revoked, err := r.RevokeIPBansByIDs(ctx, []int64{id}, adminID, "")
	if err != nil {
		return err
	}
	if len(revoked) == 0 {
		return fmt.Errorf("封禁记录不存在或已非活跃状态")
	}
	return nil
}

// RevokeIPBansByIDs 批量解封指定记录（只动生效中的），返回实际解封的记录（ID 与 IP）。
func (r *Repository) RevokeIPBansByIDs(ctx context.Context, ids []int64, adminID int64, note string) ([]firewalldomain.IPBan, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE ip_bans SET status='revoked', revoked_by=$2, revoked_at=NOW(), updated_at=NOW(), revoke_note=$3, updated_by=$2
		WHERE id = ANY($1) AND status='active'
		RETURNING `+ipBanColumns, ids, adminID, note)
	if err != nil {
		return nil, err
	}
	return collectIPBans(rows)
}

// RevokeIPBansByFilter 按筛选批量解封（只动生效中的），一次最多 limit 条，返回实际解封的记录。
// 子查询加 FOR UPDATE SKIP LOCKED：两个管理员同时按同一条件解封不会互相等锁，各解各的。
func (r *Repository) RevokeIPBansByFilter(ctx context.Context, filter firewalldomain.IPBanFilter, adminID int64, note string, limit int) ([]firewalldomain.IPBan, error) {
	filter.Status = "active"
	where, args := buildIPBanWhere(filter)
	args = append(args, adminID, note, limit)
	n := len(args)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		UPDATE ip_bans SET status='revoked', revoked_by=$%d, revoked_at=NOW(), updated_at=NOW(), revoke_note=$%d, updated_by=$%d
		WHERE id IN (SELECT id FROM ip_bans%s ORDER BY id LIMIT $%d FOR UPDATE SKIP LOCKED)
		RETURNING %s`, n-2, n-1, n-2, where, n, ipBanColumns), args...)
	if err != nil {
		return nil, err
	}
	return collectIPBans(rows)
}

// UpdateIPBan 修改一条生效中的封禁；不存在或已失效返回 nil, nil。
func (r *Repository) UpdateIPBan(ctx context.Context, id int64, mode, reason string, duration int64, expiresAt *time.Time, delayMs int, adminID int64) (*firewalldomain.IPBan, error) {
	ban, err := scanIPBan(r.pool.QueryRow(ctx, `
		UPDATE ip_bans SET mode=$2, reason=$3, duration=$4, expires_at=$5, delay_ms=$6, updated_by=$7, updated_at=NOW()
		WHERE id=$1 AND status='active'
		RETURNING `+ipBanColumns, id, mode, reason, duration, expiresAt, delayMs, adminID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ban, err
}

// IPBanStats 封禁概况
func (r *Repository) IPBanStats(ctx context.Context) (*firewalldomain.IPBanStats, error) {
	stats := &firewalldomain.IPBanStats{}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status='active' AND (expires_at IS NULL OR expires_at > NOW())),
			COUNT(*) FILTER (WHERE status='active' AND expires_at IS NULL),
			COUNT(*) FILTER (WHERE status='active' AND expires_at > NOW() AND expires_at <= NOW() + INTERVAL '24 hours'),
			COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours'),
			COUNT(*) FILTER (WHERE status='revoked' AND revoked_at > NOW() - INTERVAL '24 hours'),
			COUNT(*) FILTER (WHERE status='expired' AND updated_at > NOW() - INTERVAL '24 hours')
		FROM ip_bans`).Scan(&stats.Active, &stats.Permanent, &stats.ExpiringSoon, &stats.Created24h, &stats.Revoked24h, &stats.Expired24h); err != nil {
		return nil, err
	}
	ranked := func(col string) ([]firewalldomain.RankedItem, error) {
		rows, err := r.pool.Query(ctx, fmt.Sprintf(`
			SELECT %s AS k, COUNT(*) FROM ip_bans
			WHERE status='active' AND (expires_at IS NULL OR expires_at > NOW())
			GROUP BY k ORDER BY 2 DESC LIMIT 12`, col))
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
	var err error
	if stats.BySource, err = ranked("source"); err != nil {
		return nil, err
	}
	if stats.ByMode, err = ranked("CASE WHEN mode = '' THEN 'default' ELSE mode END"); err != nil {
		return nil, err
	}
	if stats.ByCountry, err = ranked("COALESCE(NULLIF(country_code, ''), '??')"); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		WITH days AS (
			SELECT generate_series(CURRENT_DATE - 13, CURRENT_DATE, INTERVAL '1 day')::date AS d
		)
		SELECT to_char(d, 'YYYY-MM-DD'),
			(SELECT COUNT(*) FROM ip_bans WHERE created_at >= d AND created_at < d + 1),
			(SELECT COUNT(*) FROM ip_bans WHERE status='revoked' AND revoked_at >= d AND revoked_at < d + 1),
			(SELECT COUNT(*) FROM ip_bans WHERE status='expired' AND updated_at >= d AND updated_at < d + 1)
		FROM days ORDER BY d`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats.Daily = []firewalldomain.BanDaily{}
	for rows.Next() {
		var d firewalldomain.BanDaily
		if err := rows.Scan(&d.Day, &d.Created, &d.Revoked, &d.Expired); err != nil {
			return nil, err
		}
		stats.Daily = append(stats.Daily, d)
	}
	return stats, rows.Err()
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
		"SELECT COUNT(*) FROM firewall_logs WHERE ip=$1 AND blocked_at >= $2 AND action = 'block'",
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
		"SELECT COUNT(*) FROM firewall_logs WHERE ip=$1 AND blocked_at >= $2 AND action = 'block' AND severity = ANY($3)",
		ip, since, severities,
	).Scan(&count)
	return count, err
}

// CountRecentBlocksByReason 统计某 IP 在指定时间窗口内、指定拦截原因的次数
// （自动封禁规则的 ReasonFilter 使用，如仅统计 rate_limited）
func (r *Repository) CountRecentBlocksByReason(ctx context.Context, ip string, since time.Time, reasons []string) (int, error) {
	if len(reasons) == 0 {
		return r.CountRecentBlocks(ctx, ip, since)
	}
	var count int
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM firewall_logs WHERE ip=$1 AND blocked_at >= $2 AND action = 'block' AND reason = ANY($3)",
		ip, since, reasons,
	).Scan(&count)
	return count, err
}

// ListActiveIPBanIPs 获取所有活跃封禁（用于同步到 Redis）
func (r *Repository) ListActiveIPBanIPs(ctx context.Context) ([]firewalldomain.IPBan, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+ipBanColumns+` FROM ip_bans WHERE status='active'`)
	if err != nil {
		return nil, err
	}
	return collectIPBans(rows)
}

// ── 内部辅助 ──────────────────────────

func buildIPBanWhere(f firewalldomain.IPBanFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, strings.ReplaceAll(cond, "$?", fmt.Sprintf("$%d", len(args))))
	}
	if f.IP != "" {
		add("ip = $?", f.IP)
	}
	switch f.Status {
	case "", "all":
	case "active":
		// 到期但还没被清理任务标成 expired 的不算生效
		conds = append(conds, "status = 'active' AND (expires_at IS NULL OR expires_at > NOW())")
	default:
		add("status = $?", f.Status)
	}
	if f.Source != "" && f.Source != "all" {
		add("source = $?", f.Source)
	}
	if f.Country != "" {
		add("country_code = $?", strings.ToUpper(f.Country))
	}
	switch f.Mode {
	case "":
	case "default":
		conds = append(conds, "mode = ''")
	default:
		add("mode = $?", f.Mode)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		args = append(args, escapeLike(kw))
		n := len(args)
		conds = append(conds, fmt.Sprintf("(ip LIKE $%d || '%%' OR reason ILIKE '%%' || $%d || '%%' OR trigger_rule ILIKE '%%' || $%d || '%%' OR isp ILIKE '%%' || $%d || '%%' OR city ILIKE '%%' || $%d || '%%')", n, n, n, n, n))
	}
	if f.ExpiringWithin > 0 {
		add("expires_at IS NOT NULL AND expires_at > NOW() AND expires_at <= NOW() + make_interval(secs => $?)", float64(f.ExpiringWithin))
	}
	if f.Permanent != nil {
		if *f.Permanent {
			conds = append(conds, "expires_at IS NULL")
		} else {
			conds = append(conds, "expires_at IS NOT NULL")
		}
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func collectIPBans(rows pgx.Rows) ([]firewalldomain.IPBan, error) {
	defer rows.Close()
	items := make([]firewalldomain.IPBan, 0)
	for rows.Next() {
		ban, err := scanIPBan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *ban)
	}
	return items, rows.Err()
}

func scanIPBan(row pgx.Row) (*firewalldomain.IPBan, error) {
	var b firewalldomain.IPBan
	err := row.Scan(
		&b.ID, &b.IP, &b.Reason, &b.Source, &b.Mode, &b.TriggerRule, &b.Severity,
		&b.Duration, &b.ExpiresAt, &b.Status, &b.RevokedBy, &b.RevokedAt,
		&b.Country, &b.CountryCode, &b.Region, &b.City, &b.ISP,
		&b.TriggerCount, &b.CreatedAt, &b.UpdatedAt, &b.RevokeNote, &b.DelayMs, &b.UpdatedBy,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}
