package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	systemdomain "aegis/internal/domain/system"
)

// scanPlatformBanner 统一扫描 platform_banners 表一行。
func scanPlatformBanner(row interface{ Scan(dest ...any) error }) (*systemdomain.PlatformBanner, error) {
	item := &systemdomain.PlatformBanner{}
	var (
		st        *time.Time
		et        *time.Time
		createdBy *int64
	)
	err := row.Scan(
		&item.ID,
		&item.Title,
		&item.Description,
		&item.ImageURL,
		&item.ClickURL,
		&item.Type,
		&item.Position,
		&item.Status,
		&st,
		&et,
		&createdBy,
		&item.ViewCount,
		&item.ClickCount,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.StartTime = st
	item.EndTime = et
	item.CreatedBy = createdBy
	return item, nil
}

const platformBannerColumns = `id, title, COALESCE(description, ''), image_url, COALESCE(click_url, ''), type, position, status, start_time, end_time, created_by, view_count, click_count, created_at, updated_at`

// ListActivePlatformBanners 返回当前有效的平台 Banner，按 position ASC, id DESC 排序。
// 扫描同时累加 view_count（与应用 Banner 行为一致）。
func (r *Repository) ListActivePlatformBanners(ctx context.Context, now time.Time) ([]systemdomain.PlatformBanner, error) {
	query := `SELECT ` + platformBannerColumns + `
FROM platform_banners
WHERE status = TRUE
  AND (start_time IS NULL OR start_time <= $1)
  AND (end_time IS NULL OR end_time >= $1)
ORDER BY position ASC, id DESC`
	rows, err := r.pool.Query(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]systemdomain.PlatformBanner, 0, 8)
	ids := make([]int64, 0, 8)
	for rows.Next() {
		item, err := scanPlatformBanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, _ = r.pool.Exec(ctx, `UPDATE platform_banners SET view_count = view_count + 1 WHERE id = ANY($1)`, ids)
	}
	return items, nil
}

// ListPlatformBanners 管理后台列表（含停用条目，可过滤）。
func (r *Repository) ListPlatformBanners(ctx context.Context, filter systemdomain.PlatformBannerFilter) ([]systemdomain.PlatformBanner, int64, error) {
	conds := []string{"1=1"}
	args := []any{}

	if filter.Status != nil {
		args = append(args, *filter.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if s := strings.TrimSpace(filter.Type); s != "" {
		args = append(args, s)
		conds = append(conds, fmt.Sprintf("type = $%d", len(args)))
	}
	if k := strings.TrimSpace(filter.Keyword); k != "" {
		args = append(args, "%"+k+"%")
		idx := len(args)
		conds = append(conds, fmt.Sprintf("(title ILIKE $%d OR description ILIKE $%d)", idx, idx))
	}

	where := strings.Join(conds, " AND ")

	// 统计总数
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM platform_banners WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 分页查询
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	query := fmt.Sprintf(
		`SELECT %s FROM platform_banners WHERE %s ORDER BY position ASC, id DESC LIMIT $%d OFFSET $%d`,
		platformBannerColumns, where, len(args)-1, len(args),
	)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]systemdomain.PlatformBanner, 0, limit)
	for rows.Next() {
		item, err := scanPlatformBanner(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// GetPlatformBannerByID 单条查询。
func (r *Repository) GetPlatformBannerByID(ctx context.Context, id int64) (*systemdomain.PlatformBanner, error) {
	query := `SELECT ` + platformBannerColumns + ` FROM platform_banners WHERE id = $1 LIMIT 1`
	return scanPlatformBanner(r.pool.QueryRow(ctx, query, id))
}

// UpsertPlatformBanner 新建或更新。item.ID > 0 视为更新。
func (r *Repository) UpsertPlatformBanner(ctx context.Context, item systemdomain.PlatformBanner) (*systemdomain.PlatformBanner, error) {
	if item.ID > 0 {
		query := `UPDATE platform_banners
SET title = $2,
    description = $3,
    image_url = $4,
    click_url = $5,
    type = $6,
    position = $7,
    status = $8,
    start_time = $9,
    end_time = $10,
    updated_at = NOW()
WHERE id = $1
RETURNING ` + platformBannerColumns
		return scanPlatformBanner(r.pool.QueryRow(ctx, query,
			item.ID,
			item.Title,
			item.Description,
			item.ImageURL,
			item.ClickURL,
			item.Type,
			item.Position,
			item.Status,
			item.StartTime,
			item.EndTime,
		))
	}

	// 注意：description / click_url 均为 NOT NULL DEFAULT ''，此处必须透传空串而非 NULL
	// （nullableString 会把 "" 转成 nil → PostgreSQL 23502 "violates not-null constraint"）。
	query := `INSERT INTO platform_banners (title, description, image_url, click_url, type, position, status, start_time, end_time, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
RETURNING ` + platformBannerColumns
	var createdBy any
	if item.CreatedBy != nil {
		createdBy = *item.CreatedBy
	}
	return scanPlatformBanner(r.pool.QueryRow(ctx, query,
		item.Title,
		item.Description,
		item.ImageURL,
		item.ClickURL,
		item.Type,
		item.Position,
		item.Status,
		item.StartTime,
		item.EndTime,
		createdBy,
	))
}

// DeletePlatformBanner 单条删除。
func (r *Repository) DeletePlatformBanner(ctx context.Context, id int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM platform_banners WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// DeletePlatformBanners 批量删除，返回影响行数。
func (r *Repository) DeletePlatformBanners(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result, err := r.pool.Exec(ctx, `DELETE FROM platform_banners WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
