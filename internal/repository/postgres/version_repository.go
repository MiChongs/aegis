package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	appdomain "aegis/internal/domain/app"
)

func (r *Repository) EnsureDefaultVersionChannel(ctx context.Context, appID int64) (*appdomain.AppVersionChannel, error) {
	query := `INSERT INTO app_version_channels (appid, name, code, description, is_default, status, target_audience, created_at, updated_at)
VALUES ($1, '默认渠道', 'default', '系统默认渠道', true, true, '{}'::jsonb, NOW(), NOW())
ON CONFLICT (appid, code) DO UPDATE SET is_default = true, status = true, updated_at = NOW()
RETURNING id, appid, name, code, description, is_default, status, COALESCE(target_audience, '{}'::jsonb), created_at, updated_at`
	return scanVersionChannel(r.pool.QueryRow(ctx, query, appID))
}

func (r *Repository) ListVersionChannels(ctx context.Context, appID int64) ([]appdomain.AppVersionChannel, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id, c.appid, c.name, c.code, c.description, c.is_default, c.status, COALESCE(c.target_audience, '{}'::jsonb), c.created_at, c.updated_at,
COUNT(cu.user_id) AS user_count
FROM app_version_channels c
LEFT JOIN app_version_channel_users cu ON cu.channel_id = c.id
WHERE c.appid = $1
GROUP BY c.id
ORDER BY c.is_default DESC, c.id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]appdomain.AppVersionChannel, 0, 8)
	for rows.Next() {
		item, err := scanVersionChannelWithCount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetVersionChannelByID(ctx context.Context, channelID int64, appID int64) (*appdomain.AppVersionChannel, error) {
	query := `SELECT id, appid, name, code, description, is_default, status, COALESCE(target_audience, '{}'::jsonb), created_at, updated_at FROM app_version_channels WHERE id = $1 AND appid = $2 LIMIT 1`
	return scanVersionChannel(r.pool.QueryRow(ctx, query, channelID, appID))
}

func (r *Repository) UpsertVersionChannel(ctx context.Context, mutation appdomain.AppVersionChannelMutation) (*appdomain.AppVersionChannel, error) {
	targetJSON, _ := json.Marshal(mutation.TargetAudience)
	if mutation.ID == 0 {
		query := `INSERT INTO app_version_channels (appid, name, code, description, is_default, status, target_audience, created_at, updated_at)
VALUES ($1, $2, $3, $4, COALESCE($5, false), COALESCE($6, true), COALESCE($7, '{}'::jsonb), NOW(), NOW())
RETURNING id, appid, name, code, description, is_default, status, COALESCE(target_audience, '{}'::jsonb), created_at, updated_at`
		return scanVersionChannel(r.pool.QueryRow(ctx, query, mutation.AppID, valueOrEmpty(mutation.Name), valueOrEmpty(mutation.Code), valueOrEmpty(mutation.Description), mutation.IsDefault, mutation.Status, targetJSON))
	}
	query := `UPDATE app_version_channels SET
name = COALESCE($1, name),
code = COALESCE($2, code),
description = COALESCE($3, description),
is_default = COALESCE($4, is_default),
status = COALESCE($5, status),
target_audience = CASE WHEN $6::jsonb IS NULL THEN target_audience ELSE $6 END,
updated_at = NOW()
WHERE id = $7 AND appid = $8
RETURNING id, appid, name, code, description, is_default, status, COALESCE(target_audience, '{}'::jsonb), created_at, updated_at`
	return scanVersionChannel(r.pool.QueryRow(ctx, query, nullableString(valueOrEmpty(mutation.Name)), nullableString(valueOrEmpty(mutation.Code)), nullableString(valueOrEmpty(mutation.Description)), mutation.IsDefault, mutation.Status, nullableJSON(mutation.TargetAudience, targetJSON), mutation.ID, mutation.AppID))
}

func (r *Repository) DeleteVersionChannel(ctx context.Context, channelID int64, appID int64) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM app_version_channels WHERE id = $1 AND appid = $2`, channelID, appID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) ListAppVersions(ctx context.Context, appID int64, query appdomain.AppVersionListQuery) (*appdomain.AppVersionListResult, error) {
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
	baseQuery := ` FROM app_versions v LEFT JOIN app_version_channels c ON c.id = v.channel_id WHERE v.appid = $1`
	if query.Status != "" && query.Status != "all" {
		args = append(args, query.Status)
		baseQuery += fmt.Sprintf(" AND v.status = $%d", len(args))
	}
	if query.Platform != "" && query.Platform != "all" {
		args = append(args, query.Platform)
		baseQuery += fmt.Sprintf(" AND (v.platform = $%d OR v.platform = 'all')", len(args))
	}
	if query.ChannelID > 0 {
		args = append(args, query.ChannelID)
		baseQuery += fmt.Sprintf(" AND v.channel_id = $%d", len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT v.id, v.appid, v.channel_id, COALESCE(c.name, ''), v.version, v.version_code, COALESCE(v.description, ''), COALESCE(v.release_notes, ''), COALESCE(v.download_url, ''), v.file_size, COALESCE(v.file_hash, ''), v.force_update, COALESCE(v.update_type, ''), COALESCE(v.platform, ''), COALESCE(v.min_os_version, ''), COALESCE(v.status, ''), v.download_count, COALESCE(v.metadata, '{}'::jsonb), v.created_at, v.updated_at`+baseQuery+fmt.Sprintf(" ORDER BY v.version_code DESC, v.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]appdomain.AppVersion, 0, limit)
	for rows.Next() {
		item, err := scanAppVersion(rows)
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
	return &appdomain.AppVersionListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: totalPages}, nil
}

func (r *Repository) GetAppVersionByID(ctx context.Context, versionID int64, appID int64) (*appdomain.AppVersion, error) {
	query := `SELECT v.id, v.appid, v.channel_id, COALESCE(c.name, ''), v.version, v.version_code, COALESCE(v.description, ''), COALESCE(v.release_notes, ''), COALESCE(v.download_url, ''), v.file_size, COALESCE(v.file_hash, ''), v.force_update, COALESCE(v.update_type, ''), COALESCE(v.platform, ''), COALESCE(v.min_os_version, ''), COALESCE(v.status, ''), v.download_count, COALESCE(v.metadata, '{}'::jsonb), v.created_at, v.updated_at
FROM app_versions v LEFT JOIN app_version_channels c ON c.id = v.channel_id
WHERE v.id = $1 AND v.appid = $2 LIMIT 1`
	return scanAppVersion(r.pool.QueryRow(ctx, query, versionID, appID))
}

func (r *Repository) UpsertAppVersion(ctx context.Context, mutation appdomain.AppVersionMutation) (*appdomain.AppVersion, error) {
	metaJSON, _ := json.Marshal(mutation.Metadata)
	if mutation.ID == 0 {
		query := `INSERT INTO app_versions (appid, channel_id, version, version_code, description, release_notes, download_url, file_size, file_hash, force_update, update_type, platform, min_os_version, status, metadata, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, 0), $9, COALESCE($10, false), COALESCE($11, 'optional'), COALESCE($12, 'all'), $13, COALESCE($14, 'published'), COALESCE($15, '{}'::jsonb), NOW(), NOW())
RETURNING id, appid, channel_id, '' as channel_name, version, version_code, description, release_notes, download_url, file_size, file_hash, force_update, update_type, platform, min_os_version, status, download_count, metadata, created_at, updated_at`
		return scanAppVersion(r.pool.QueryRow(ctx, query, mutation.AppID, mutation.ChannelID, valueOrEmpty(mutation.Version), valueOrZeroInt64(mutation.VersionCode), valueOrEmpty(mutation.Description), valueOrEmpty(mutation.ReleaseNotes), valueOrEmpty(mutation.DownloadURL), mutation.FileSize, valueOrEmpty(mutation.FileHash), mutation.ForceUpdate, mutation.UpdateType, mutation.Platform, valueOrEmpty(mutation.MinOSVersion), mutation.Status, metaJSON))
	}
	query := `UPDATE app_versions SET
channel_id = COALESCE($1, channel_id),
version = COALESCE($2, version),
version_code = COALESCE($3, version_code),
description = COALESCE($4, description),
release_notes = COALESCE($5, release_notes),
download_url = COALESCE($6, download_url),
file_size = COALESCE($7, file_size),
file_hash = COALESCE($8, file_hash),
force_update = COALESCE($9, force_update),
update_type = COALESCE($10, update_type),
platform = COALESCE($11, platform),
min_os_version = COALESCE($12, min_os_version),
status = COALESCE($13, status),
metadata = CASE WHEN $14::jsonb IS NULL THEN metadata ELSE $14 END,
updated_at = NOW()
WHERE id = $15 AND appid = $16
RETURNING id, appid, channel_id, '' as channel_name, version, version_code, description, release_notes, download_url, file_size, file_hash, force_update, update_type, platform, min_os_version, status, download_count, metadata, created_at, updated_at`
	return scanAppVersion(r.pool.QueryRow(ctx, query, mutation.ChannelID, nullableString(valueOrEmpty(mutation.Version)), mutation.VersionCode, nullableString(valueOrEmpty(mutation.Description)), nullableString(valueOrEmpty(mutation.ReleaseNotes)), nullableString(valueOrEmpty(mutation.DownloadURL)), mutation.FileSize, nullableString(valueOrEmpty(mutation.FileHash)), mutation.ForceUpdate, nullableString(valueOrEmpty(mutation.UpdateType)), nullableString(valueOrEmpty(mutation.Platform)), nullableString(valueOrEmpty(mutation.MinOSVersion)), nullableString(valueOrEmpty(mutation.Status)), nullableJSON(mutation.Metadata, metaJSON), mutation.ID, mutation.AppID))
}

func (r *Repository) DeleteAppVersion(ctx context.Context, versionID int64, appID int64) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM app_versions WHERE id = $1 AND appid = $2`, versionID, appID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) AddUsersToVersionChannel(ctx context.Context, channelID int64, appID int64, userIDs []int64) (int64, error) {
	var added int64
	for _, userID := range userIDs {
		result, err := r.pool.Exec(ctx, `INSERT INTO app_version_channel_users (channel_id, user_id, appid, created_at) VALUES ($1, $2, $3, NOW()) ON CONFLICT DO NOTHING`, channelID, userID, appID)
		if err != nil {
			return added, err
		}
		added += result.RowsAffected()
	}
	return added, nil
}

func (r *Repository) RemoveUsersFromVersionChannel(ctx context.Context, channelID int64, appID int64, userIDs []int64) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM app_version_channel_users WHERE channel_id = $1 AND appid = $2 AND user_id = ANY($3)`, channelID, appID, userIDs)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) ListVersionChannelUsers(ctx context.Context, channelID int64, appID int64, page int, limit int) ([]appdomain.Site, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_version_channel_users WHERE channel_id = $1 AND appid = $2`, channelID, appID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT u.id, u.appid, u.id as user_id, '' AS header, '' AS name, '' AS url, '' AS type, '' AS description, '' AS category, '' AS status, '' AS audit_status, '' AS audit_reason, false AS is_pinned, 0 AS view_count, 0 AS like_count, '{}'::jsonb AS extra, u.created_at, u.updated_at, COALESCE(u.account,''), COALESCE(p.nickname,''), COALESCE(p.avatar,'')
FROM app_version_channel_users cu
JOIN users u ON u.id = cu.user_id
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE cu.channel_id = $1 AND cu.appid = $2
ORDER BY cu.created_at DESC
LIMIT $3 OFFSET $4`, channelID, appID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]appdomain.Site, 0, limit)
	for rows.Next() {
		item, err := scanSiteWithUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ResolveVersionChannelForUser(ctx context.Context, appID int64, userID int64) (*appdomain.AppVersionChannel, error) {
	query := `SELECT c.id, c.appid, c.name, c.code, c.description, c.is_default, c.status, COALESCE(c.target_audience, '{}'::jsonb), c.created_at, c.updated_at
FROM app_version_channels c
JOIN app_version_channel_users cu ON cu.channel_id = c.id
WHERE cu.appid = $1 AND cu.user_id = $2
ORDER BY c.is_default DESC, c.id ASC
LIMIT 1`
	channel, err := scanVersionChannel(r.pool.QueryRow(ctx, query, appID, userID))
	if err != nil {
		return nil, err
	}
	if channel != nil {
		return channel, nil
	}
	return scanVersionChannel(r.pool.QueryRow(ctx, `SELECT id, appid, name, code, description, is_default, status, COALESCE(target_audience, '{}'::jsonb), created_at, updated_at FROM app_version_channels WHERE appid = $1 AND is_default = true ORDER BY id ASC LIMIT 1`, appID))
}

func (r *Repository) FindLatestVersionForUpdate(ctx context.Context, appID int64, channelID *int64, versionCode int64, platform string) (*appdomain.AppVersion, error) {
	args := []any{appID, versionCode}
	query := `SELECT v.id, v.appid, v.channel_id, COALESCE(c.name, ''), v.version, v.version_code, COALESCE(v.description, ''), COALESCE(v.release_notes, ''), COALESCE(v.download_url, ''), v.file_size, COALESCE(v.file_hash, ''), v.force_update, COALESCE(v.update_type, ''), COALESCE(v.platform, ''), COALESCE(v.min_os_version, ''), COALESCE(v.status, ''), v.download_count, COALESCE(v.metadata, '{}'::jsonb), v.created_at, v.updated_at
FROM app_versions v
LEFT JOIN app_version_channels c ON c.id = v.channel_id
WHERE v.appid = $1 AND v.status = 'published' AND v.version_code > $2`
	if channelID != nil {
		args = append(args, *channelID)
		query += fmt.Sprintf(" AND (v.channel_id = $%d OR v.channel_id IS NULL)", len(args))
	}
	if platform != "" && platform != "all" {
		args = append(args, platform)
		query += fmt.Sprintf(" AND (v.platform = $%d OR v.platform = 'all')", len(args))
	}
	query += ` ORDER BY v.force_update DESC, v.version_code DESC, v.created_at DESC LIMIT 1`
	return scanAppVersion(r.pool.QueryRow(ctx, query, args...))
}

func (r *Repository) GetAppVersionStats(ctx context.Context, appID int64) (*appdomain.AppVersionStats, error) {
	stats := &appdomain.AppVersionStats{AppID: appID, PlatformCounts: map[string]int64{}}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'published'), (SELECT COUNT(*) FROM app_version_channels WHERE appid = $1) FROM app_versions WHERE appid = $1`, appID).Scan(&stats.TotalVersions, &stats.PublishedCount, &stats.ChannelCount); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT platform, COUNT(*) FROM app_versions WHERE appid = $1 GROUP BY platform`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var platform string
		var count int64
		if err := rows.Scan(&platform, &count); err != nil {
			return nil, err
		}
		stats.PlatformCounts[platform] = count
	}
	return stats, rows.Err()
}

func scanVersionChannel(row interface{ Scan(dest ...any) error }) (*appdomain.AppVersionChannel, error) {
	var item appdomain.AppVersionChannel
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Code, &item.Description, &item.IsDefault, &item.Status, &raw, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.TargetAudience)
	return &item, nil
}

func scanVersionChannelWithCount(row interface{ Scan(dest ...any) error }) (*appdomain.AppVersionChannel, error) {
	var item appdomain.AppVersionChannel
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Code, &item.Description, &item.IsDefault, &item.Status, &raw, &item.CreatedAt, &item.UpdatedAt, &item.UserCount); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.TargetAudience)
	return &item, nil
}

func scanAppVersion(row interface{ Scan(dest ...any) error }) (*appdomain.AppVersion, error) {
	var item appdomain.AppVersion
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.ChannelID, &item.ChannelName, &item.Version, &item.VersionCode, &item.Description, &item.ReleaseNotes, &item.DownloadURL, &item.FileSize, &item.FileHash, &item.ForceUpdate, &item.UpdateType, &item.Platform, &item.MinOSVersion, &item.Status, &item.DownloadCount, &raw, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.Metadata)
	return &item, nil
}

func nullableJSON(src map[string]any, raw []byte) any {
	if src == nil {
		return nil
	}
	return raw
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueOrZeroInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
