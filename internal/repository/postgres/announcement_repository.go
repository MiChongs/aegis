package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	systemdomain "aegis/internal/domain/system"
)

// ── 查询 ──

func (r *Repository) ListAnnouncements(ctx context.Context, query systemdomain.AnnouncementListQuery) (*systemdomain.AnnouncementListResult, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	args := []any{}
	where := " WHERE 1=1"
	if query.Status != "" && query.Status != "all" {
		args = append(args, query.Status)
		where += fmt.Sprintf(" AND a.status = $%d", len(args))
	}
	if query.Type != "" && query.Type != "all" {
		args = append(args, query.Type)
		where += fmt.Sprintf(" AND a.type = $%d", len(args))
	}
	if query.Level != "" && query.Level != "all" {
		args = append(args, query.Level)
		where += fmt.Sprintf(" AND a.level = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM system_announcements a`+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, limit, offset)
	sql := announcementSelectCols + ` FROM system_announcements a LEFT JOIN admin_accounts ad ON ad.id = a.admin_id` + where +
		fmt.Sprintf(` ORDER BY a.pinned DESC, a.created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]systemdomain.Announcement, 0, limit)
	for rows.Next() {
		item, err := scanAnnouncement(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}
	return &systemdomain.AnnouncementListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: totalPages}, nil
}

func (r *Repository) GetAnnouncementByID(ctx context.Context, id int64) (*systemdomain.Announcement, error) {
	sql := announcementSelectCols + ` FROM system_announcements a LEFT JOIN admin_accounts ad ON ad.id = a.admin_id WHERE a.id = $1 LIMIT 1`
	return scanAnnouncement(r.pool.QueryRow(ctx, sql, id))
}

func (r *Repository) ListActiveAnnouncements(ctx context.Context) ([]systemdomain.Announcement, error) {
	sql := announcementSelectCols + ` FROM system_announcements a LEFT JOIN admin_accounts ad ON ad.id = a.admin_id
WHERE a.status = 'published' AND (a.expires_at IS NULL OR a.expires_at > NOW())
ORDER BY a.pinned DESC, a.published_at DESC
LIMIT 50`

	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]systemdomain.Announcement, 0, 16)
	for rows.Next() {
		item, err := scanAnnouncement(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ── 写入 ──

func (r *Repository) UpsertAnnouncement(ctx context.Context, m systemdomain.AnnouncementMutation) (*systemdomain.Announcement, error) {
	metaJSON, _ := json.Marshal(m.Metadata)
	var expiresAt any
	if m.ExpiresAt != nil && *m.ExpiresAt != "" {
		expiresAt = *m.ExpiresAt // PostgreSQL 直接解析 ISO 字符串
	}

	if m.ID == 0 {
		typ := valueOrEmpty(m.Type)
		if typ == "" {
			typ = "info"
		}
		level := valueOrEmpty(m.Level)
		if level == "" {
			level = "normal"
		}
		pinned := false
		if m.Pinned != nil {
			pinned = *m.Pinned
		}
		sql := `INSERT INTO system_announcements (admin_id, type, title, content, level, pinned, status, expires_at, metadata, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'draft', $7, COALESCE($8, '{}'::jsonb), NOW(), NOW())
RETURNING ` + announcementReturning
		return scanAnnouncementSimple(r.pool.QueryRow(ctx, sql,
			m.AdminID, typ, valueOrEmpty(m.Title), valueOrEmpty(m.Content), level, pinned, expiresAt, metaJSON))
	}

	pinnedVal := m.Pinned
	sql := `UPDATE system_announcements SET
type       = COALESCE(NULLIF($2,''), type),
title      = COALESCE(NULLIF($3,''), title),
content    = COALESCE(NULLIF($4,''), content),
level      = COALESCE(NULLIF($5,''), level),
pinned     = COALESCE($6, pinned),
expires_at = COALESCE($7, expires_at),
metadata   = CASE WHEN $8::jsonb IS NULL THEN metadata ELSE $8 END,
updated_at = NOW()
WHERE id = $1
RETURNING ` + announcementReturning
	return scanAnnouncementSimple(r.pool.QueryRow(ctx, sql,
		m.ID, valueOrEmpty(m.Type), valueOrEmpty(m.Title), valueOrEmpty(m.Content), valueOrEmpty(m.Level), pinnedVal, expiresAt, metaJSON))
}

func (r *Repository) PublishAnnouncement(ctx context.Context, id int64) (*systemdomain.Announcement, error) {
	sql := `UPDATE system_announcements SET status = 'published', published_at = NOW(), updated_at = NOW() WHERE id = $1 RETURNING ` + announcementReturning
	return scanAnnouncementSimple(r.pool.QueryRow(ctx, sql, id))
}

func (r *Repository) ArchiveAnnouncement(ctx context.Context, id int64) (*systemdomain.Announcement, error) {
	sql := `UPDATE system_announcements SET status = 'archived', updated_at = NOW() WHERE id = $1 RETURNING ` + announcementReturning
	return scanAnnouncementSimple(r.pool.QueryRow(ctx, sql, id))
}

func (r *Repository) DeleteAnnouncement(ctx context.Context, id int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM system_announcements WHERE id = $1`, id)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── scan 辅助 ──

const announcementSelectCols = `SELECT a.id, a.admin_id, COALESCE(ad.display_name, ad.account, ''), a.type, a.title, COALESCE(a.content,''), a.level, a.pinned, a.status, a.published_at, a.expires_at, COALESCE(a.metadata,'{}'::jsonb), a.created_at, a.updated_at`

const announcementReturning = `id, admin_id, '' AS admin_name, type, title, COALESCE(content,''), level, pinned, status, published_at, expires_at, COALESCE(metadata,'{}'::jsonb), created_at, updated_at`

func scanAnnouncement(row interface{ Scan(dest ...any) error }) (*systemdomain.Announcement, error) {
	var item systemdomain.Announcement
	var metaRaw []byte
	if err := row.Scan(
		&item.ID, &item.AdminID, &item.AdminName,
		&item.Type, &item.Title, &item.Content,
		&item.Level, &item.Pinned, &item.Status,
		&item.PublishedAt, &item.ExpiresAt, &metaRaw,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(metaRaw, &item.Metadata)
	return &item, nil
}

// scanAnnouncementSimple 用于 RETURNING 场景（无 JOIN admin_name）
func scanAnnouncementSimple(row interface{ Scan(dest ...any) error }) (*systemdomain.Announcement, error) {
	return scanAnnouncement(row)
}
