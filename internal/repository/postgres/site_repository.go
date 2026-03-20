package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appdomain "aegis/internal/domain/app"
)

func (r *Repository) CreateSite(ctx context.Context, item appdomain.Site) (*appdomain.Site, error) {
	extraJSON, _ := json.Marshal(item.Extra)
	query := `INSERT INTO sites (appid, user_id, header, name, url, type, description, category, status, audit_status, extra, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', 'pending', $9, NOW(), NOW())
RETURNING id, appid, user_id, COALESCE(header, ''), COALESCE(name, ''), COALESCE(url, ''), COALESCE(type, ''), COALESCE(description, ''), COALESCE(category, ''), status, audit_status, COALESCE(audit_reason, ''), is_pinned, view_count, like_count, COALESCE(extra, '{}'::jsonb), created_at, updated_at`
	return scanSite(r.pool.QueryRow(ctx, query, item.AppID, item.UserID, item.Header, item.Name, item.URL, item.Type, item.Description, item.Category, extraJSON))
}

func (r *Repository) UpdateSiteByUser(ctx context.Context, siteID int64, userID int64, appID int64, mutation appdomain.SiteMutation) (*appdomain.Site, error) {
	assignments := []string{"updated_at = NOW()", "status = 'pending'", "audit_status = 'pending'", "audit_reason = ''", "audit_admin_id = NULL", "audit_at = NULL"}
	args := []any{}
	if mutation.Header != nil {
		args = append(args, *mutation.Header)
		assignments = append(assignments, fmt.Sprintf("header = $%d", len(args)))
	}
	if mutation.Name != nil {
		args = append(args, *mutation.Name)
		assignments = append(assignments, fmt.Sprintf("name = $%d", len(args)))
	}
	if mutation.URL != nil {
		args = append(args, *mutation.URL)
		assignments = append(assignments, fmt.Sprintf("url = $%d", len(args)))
	}
	if mutation.Type != nil {
		args = append(args, *mutation.Type)
		assignments = append(assignments, fmt.Sprintf("type = $%d", len(args)))
	}
	if mutation.Description != nil {
		args = append(args, *mutation.Description)
		assignments = append(assignments, fmt.Sprintf("description = $%d", len(args)))
	}
	if mutation.Category != nil {
		args = append(args, *mutation.Category)
		assignments = append(assignments, fmt.Sprintf("category = $%d", len(args)))
	}
	args = append(args, siteID, userID, appID)
	query := `UPDATE sites SET ` + strings.Join(assignments, ", ") + fmt.Sprintf(`
WHERE id = $%d AND user_id = $%d AND appid = $%d
RETURNING id, appid, user_id, COALESCE(header, ''), COALESCE(name, ''), COALESCE(url, ''), COALESCE(type, ''), COALESCE(description, ''), COALESCE(category, ''), status, audit_status, COALESCE(audit_reason, ''), is_pinned, view_count, like_count, COALESCE(extra, '{}'::jsonb), created_at, updated_at`, len(args)-2, len(args)-1, len(args))
	return scanSite(r.pool.QueryRow(ctx, query, args...))
}

func (r *Repository) UpdateSiteAdmin(ctx context.Context, siteID int64, appID int64, mutation appdomain.SiteMutation) (*appdomain.Site, error) {
	assignments := []string{"updated_at = NOW()"}
	args := []any{}
	if mutation.Header != nil {
		args = append(args, *mutation.Header)
		assignments = append(assignments, fmt.Sprintf("header = $%d", len(args)))
	}
	if mutation.Name != nil {
		args = append(args, *mutation.Name)
		assignments = append(assignments, fmt.Sprintf("name = $%d", len(args)))
	}
	if mutation.URL != nil {
		args = append(args, *mutation.URL)
		assignments = append(assignments, fmt.Sprintf("url = $%d", len(args)))
	}
	if mutation.Type != nil {
		args = append(args, *mutation.Type)
		assignments = append(assignments, fmt.Sprintf("type = $%d", len(args)))
	}
	if mutation.Description != nil {
		args = append(args, *mutation.Description)
		assignments = append(assignments, fmt.Sprintf("description = $%d", len(args)))
	}
	if mutation.Category != nil {
		args = append(args, *mutation.Category)
		assignments = append(assignments, fmt.Sprintf("category = $%d", len(args)))
	}
	args = append(args, siteID, appID)
	query := `UPDATE sites SET ` + strings.Join(assignments, ", ") + fmt.Sprintf(`
WHERE id = $%d AND appid = $%d
RETURNING id, appid, user_id, COALESCE(header, ''), COALESCE(name, ''), COALESCE(url, ''), COALESCE(type, ''), COALESCE(description, ''), COALESCE(category, ''), status, audit_status, COALESCE(audit_reason, ''), is_pinned, view_count, like_count, COALESCE(extra, '{}'::jsonb), created_at, updated_at`, len(args)-1, len(args))
	return scanSite(r.pool.QueryRow(ctx, query, args...))
}

func (r *Repository) GetSiteByID(ctx context.Context, siteID int64, appID int64) (*appdomain.Site, error) {
	query := `SELECT s.id, s.appid, s.user_id, COALESCE(s.header, ''), COALESCE(s.name, ''), COALESCE(s.url, ''), COALESCE(s.type, ''), COALESCE(s.description, ''), COALESCE(s.category, ''), s.status, s.audit_status, COALESCE(s.audit_reason, ''), s.is_pinned, s.view_count, s.like_count, COALESCE(s.extra, '{}'::jsonb), s.created_at, s.updated_at,
COALESCE(u.account, ''), COALESCE(p.nickname, ''), COALESCE(p.avatar, '')
FROM sites s
JOIN users u ON u.id = s.user_id
LEFT JOIN user_profiles p ON p.user_id = s.user_id
WHERE s.id = $1 AND s.appid = $2 LIMIT 1`
	return scanSiteWithUser(r.pool.QueryRow(ctx, query, siteID, appID))
}

func (r *Repository) DeleteSiteByUser(ctx context.Context, siteID int64, userID int64, appID int64) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM sites WHERE id = $1 AND user_id = $2 AND appid = $3`, siteID, userID, appID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) DeleteSiteAdmin(ctx context.Context, siteID int64, appID int64) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM sites WHERE id = $1 AND appid = $2`, siteID, appID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) ListSitesPublic(ctx context.Context, appID int64, query appdomain.SiteListQuery) (*appdomain.SiteListResult, error) {
	return r.listSites(ctx, appID, query, "s.status = 'approved'")
}

func (r *Repository) ListSitesByUser(ctx context.Context, userID int64, appID int64, query appdomain.SiteListQuery) (*appdomain.SiteListResult, error) {
	return r.listSites(ctx, appID, query, fmt.Sprintf("s.user_id = %d", userID))
}

func (r *Repository) ListSitesByApp(ctx context.Context, appID int64, query appdomain.SiteListQuery) (*appdomain.SiteListResult, error) {
	filter := "TRUE"
	if query.Status != "" && query.Status != "all" {
		filter = fmt.Sprintf("s.audit_status = '%s'", strings.ReplaceAll(query.Status, "'", ""))
	}
	return r.listSites(ctx, appID, query, filter)
}

func (r *Repository) listSites(ctx context.Context, appID int64, query appdomain.SiteListQuery, scopeFilter string) (*appdomain.SiteListResult, error) {
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

	args := []any{appID}
	baseQuery := ` FROM sites s
JOIN users u ON u.id = s.user_id
LEFT JOIN user_profiles p ON p.user_id = s.user_id
WHERE s.appid = $1 AND ` + scopeFilter
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		baseQuery += fmt.Sprintf(" AND (s.name ILIKE $%d OR s.description ILIKE $%d OR s.url ILIKE $%d OR COALESCE(u.account,'') ILIKE $%d OR COALESCE(p.nickname,'') ILIKE $%d)", len(args), len(args), len(args), len(args), len(args))
	}
	if category := strings.TrimSpace(query.Category); category != "" {
		args = append(args, category)
		baseQuery += fmt.Sprintf(" AND s.category = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	orderBy := "s.is_pinned DESC, s.created_at DESC, s.id DESC"
	switch query.SortBy {
	case "updatedAt":
		orderBy = "s.updated_at"
	case "name":
		orderBy = "s.name"
	case "id":
		orderBy = "s.id"
	case "view_count":
		orderBy = "s.view_count"
	case "like_count":
		orderBy = "s.like_count"
	}
	if !strings.Contains(orderBy, "DESC") && !strings.Contains(orderBy, "ASC") {
		if strings.ToUpper(query.SortOrder) == "ASC" {
			orderBy += " ASC"
		} else {
			orderBy += " DESC"
		}
	}

	args = append(args, limit, offset)
	sql := `SELECT s.id, s.appid, s.user_id, COALESCE(s.header, ''), COALESCE(s.name, ''), COALESCE(s.url, ''), COALESCE(s.type, ''), COALESCE(s.description, ''), COALESCE(s.category, ''), s.status, s.audit_status, COALESCE(s.audit_reason, ''), s.is_pinned, s.view_count, s.like_count, COALESCE(s.extra, '{}'::jsonb), s.created_at, s.updated_at,
COALESCE(u.account, ''), COALESCE(p.nickname, ''), COALESCE(p.avatar, '')` + baseQuery + fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", orderBy, len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Site, 0, limit)
	for rows.Next() {
		item, err := scanSiteWithUser(rows)
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
	return &appdomain.SiteListResult{
		List:        items,
		Page:        page,
		Limit:       limit,
		Total:       total,
		TotalPages:  totalPages,
		HasNextPage: page < totalPages,
		HasPrevPage: page > 1,
	}, nil
}

func (r *Repository) AuditSite(ctx context.Context, siteID int64, appID int64, adminID int64, status string, reason string) (*appdomain.Site, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	siteStatus := "rejected"
	if status == "approved" {
		siteStatus = "approved"
	}
	query := `UPDATE sites SET status = $1, audit_status = $1, audit_reason = $2, audit_admin_id = $3, audit_at = NOW(), updated_at = NOW()
WHERE id = $4 AND appid = $5
RETURNING id, appid, user_id, COALESCE(header, ''), COALESCE(name, ''), COALESCE(url, ''), COALESCE(type, ''), COALESCE(description, ''), COALESCE(category, ''), status, audit_status, COALESCE(audit_reason, ''), is_pinned, view_count, like_count, COALESCE(extra, '{}'::jsonb), created_at, updated_at`
	item, err := scanSite(tx.QueryRow(ctx, query, siteStatus, reason, nullableInt64(adminID), siteID, appID))
	if err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(map[string]any{"reason": reason})
	if _, err := tx.Exec(ctx, `INSERT INTO site_audits (site_id, appid, user_id, status, reason, admin_id, metadata) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		item.ID, item.AppID, item.UserID, siteStatus, reason, nullableInt64(adminID), metadata); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) ToggleSitePinned(ctx context.Context, siteID int64, appID int64, pinned bool) (*appdomain.Site, error) {
	query := `UPDATE sites SET is_pinned = $1, updated_at = NOW() WHERE id = $2 AND appid = $3
RETURNING id, appid, user_id, COALESCE(header, ''), COALESCE(name, ''), COALESCE(url, ''), COALESCE(type, ''), COALESCE(description, ''), COALESCE(category, ''), status, audit_status, COALESCE(audit_reason, ''), is_pinned, view_count, like_count, COALESCE(extra, '{}'::jsonb), created_at, updated_at`
	return scanSite(r.pool.QueryRow(ctx, query, pinned, siteID, appID))
}

func (r *Repository) GetSiteAuditStats(ctx context.Context, appID int64) (*appdomain.SiteAuditStats, error) {
	rows, err := r.pool.Query(ctx, `SELECT audit_status, COUNT(*) FROM sites WHERE appid = $1 GROUP BY audit_status`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &appdomain.SiteAuditStats{AppID: appID, ByStatus: map[string]int64{}}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result.ByStatus[status] = count
		result.Total += count
		switch status {
		case "pending":
			result.Pending = count
		case "approved":
			result.Approved = count
		case "rejected":
			result.Rejected = count
		}
	}
	return result, rows.Err()
}

func scanSite(row interface{ Scan(dest ...any) error }) (*appdomain.Site, error) {
	var item appdomain.Site
	var extra []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Header, &item.Name, &item.URL, &item.Type, &item.Description, &item.Category, &item.Status, &item.AuditStatus, &item.AuditReason, &item.IsPinned, &item.ViewCount, &item.LikeCount, &extra, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(extra, &item.Extra)
	return &item, nil
}

func scanSiteWithUser(row interface{ Scan(dest ...any) error }) (*appdomain.Site, error) {
	var item appdomain.Site
	var extra []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Header, &item.Name, &item.URL, &item.Type, &item.Description, &item.Category, &item.Status, &item.AuditStatus, &item.AuditReason, &item.IsPinned, &item.ViewCount, &item.LikeCount, &extra, &item.CreatedAt, &item.UpdatedAt, &item.Account, &item.Nickname, &item.Avatar); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(extra, &item.Extra)
	return &item, nil
}
