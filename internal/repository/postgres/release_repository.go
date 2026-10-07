package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"

	"github.com/jackc/pgx/v5"
)

// 发布中心的数据访问。与 version_repository.go 共用 app_versions 表：
// 那边是旧控制台与旧检测接口的读写口，这边是新的。两边写入的列互不冲突 ——
// 旧接口不认识的新列都有默认值，旧接口写的 download_url 在这里被当作兜底的通用包。

const releaseColumns = `v.id, v.appid, v.version, v.version_code, v.title, COALESCE(v.release_notes, ''), v.notes_summary,
COALESCE(v.platform, 'all'), COALESCE(v.min_os_version, ''), v.update_type, v.min_supported_code, v.status,
v.visibility, COALESCE(v.targeting, '{}'::jsonb), v.rollout_pct, v.channel_id,
COALESCE(c.code, ''), COALESCE(c.name, ''), COALESCE(c.level, ''), COALESCE(c.color, ''),
v.publish_at, COALESCE(v.published_at, CASE WHEN v.status = 'published' THEN v.created_at END),
v.download_count, v.created_by, v.created_at, v.updated_at,
COALESCE(v.download_url, ''), v.file_size, COALESCE(v.file_hash, '')`

const releaseFrom = ` FROM app_versions v LEFT JOIN app_version_channels c ON c.id = v.channel_id`

func scanRelease(row interface{ Scan(dest ...any) error }) (*appdomain.Release, error) {
	var item appdomain.Release
	var targetingRaw []byte
	var channelID *int64
	var channelCode, channelName, channelLevel, channelColor string
	var legacyURL, legacyHash string
	var legacySize int64
	if err := row.Scan(&item.ID, &item.AppID, &item.Version, &item.VersionCode, &item.Title, &item.Notes, &item.Summary,
		&item.Platform, &item.MinOSVersion, &item.UpdateType, &item.MinSupportedCode, &item.Status,
		&item.Visibility, &targetingRaw, &item.RolloutPct, &channelID,
		&channelCode, &channelName, &channelLevel, &channelColor,
		&item.PublishAt, &item.PublishedAt,
		&item.DownloadCount, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&legacyURL, &legacySize, &legacyHash); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(targetingRaw, &item.Targeting)
	if channelID != nil {
		item.Channel = &appdomain.ReleaseChannelRef{ID: *channelID, Code: channelCode, Name: channelName, Level: channelLevel, Color: channelColor}
	}
	item.Assets = []appdomain.ReleaseAsset{}
	// 旧接口创建、还没有安装包记录的版本：把那条下载地址当作通用包
	if strings.TrimSpace(legacyURL) != "" {
		item.Assets = append(item.Assets, appdomain.ReleaseAsset{
			Abi: appdomain.ReleaseAssetUniversal, URL: legacyURL, FileSize: legacySize, SHA256: legacyHash,
		})
	}
	return &item, nil
}

// attachReleaseAssets 一次查回这批版本的全部安装包，替换掉兜底的旧地址。
func (r *Repository) attachReleaseAssets(ctx context.Context, items []appdomain.Release) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	index := make(map[int64]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
		index[items[i].ID] = i
	}
	rows, err := r.pool.Query(ctx, `SELECT id, version_id, abi, label, url, file_size, sha256, position, download_count
FROM app_version_assets WHERE version_id = ANY($1) ORDER BY version_id, position, id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	replaced := map[int64]bool{}
	for rows.Next() {
		var asset appdomain.ReleaseAsset
		var versionID int64
		if err := rows.Scan(&asset.ID, &versionID, &asset.Abi, &asset.Label, &asset.URL, &asset.FileSize, &asset.SHA256, &asset.Position, &asset.DownloadCount); err != nil {
			return err
		}
		i, ok := index[versionID]
		if !ok {
			continue
		}
		if !replaced[versionID] {
			items[i].Assets = items[i].Assets[:0]
			replaced[versionID] = true
		}
		items[i].Assets = append(items[i].Assets, asset)
	}
	return rows.Err()
}

func (r *Repository) queryReleases(ctx context.Context, query string, args ...any) ([]appdomain.Release, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]appdomain.Release, 0, 16)
	for rows.Next() {
		item, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := r.attachReleaseAssets(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

// ListReleases 管理端分页列表，按版本码倒序。
func (r *Repository) ListReleases(ctx context.Context, appID int64, query appdomain.ReleaseListQuery) ([]appdomain.Release, int64, error) {
	conditions := []string{"v.appid = $1"}
	args := []any{appID}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("v.status = $%d", len(args)))
	}
	if platform := strings.TrimSpace(query.Platform); platform != "" {
		args = append(args, platform)
		conditions = append(conditions, fmt.Sprintf("v.platform = $%d", len(args)))
	}
	if query.ChannelID > 0 {
		args = append(args, query.ChannelID)
		conditions = append(conditions, fmt.Sprintf("v.channel_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		conditions = append(conditions, fmt.Sprintf("(v.version ILIKE $%d OR v.title ILIKE $%d OR v.notes_summary ILIKE $%d)", len(args), len(args), len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_versions v WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []appdomain.Release{}, 0, nil
	}
	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	items, err := r.queryReleases(ctx, fmt.Sprintf(`SELECT %s%s WHERE %s ORDER BY v.version_code DESC, v.id DESC LIMIT $%d OFFSET $%d`,
		releaseColumns, releaseFrom, where, len(args)-1, len(args)), args...)
	return items, total, err
}

// GetRelease 查不到返回 (nil, nil)。
func (r *Repository) GetRelease(ctx context.Context, appID int64, releaseID int64) (*appdomain.Release, error) {
	items, err := r.queryReleases(ctx, `SELECT `+releaseColumns+releaseFrom+` WHERE v.appid = $1 AND v.id = $2`, appID, releaseID)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

// ListDeliverableReleases 检测引擎的候选集：已发布与定时发布的全部版本（是否到点由引擎判）。
func (r *Repository) ListDeliverableReleases(ctx context.Context, appID int64) ([]appdomain.Release, error) {
	return r.queryReleases(ctx, `SELECT `+releaseColumns+releaseFrom+`
WHERE v.appid = $1 AND v.status IN ('published', 'scheduled') AND (v.channel_id IS NULL OR c.status = true)
ORDER BY v.version_code DESC, v.id DESC`, appID)
}

// ReleaseVersionCodeTaken 同一应用、同一平台、同一渠道下，未撤回的版本不能重复版本码。
func (r *Repository) ReleaseVersionCodeTaken(ctx context.Context, appID int64, platform string, channelID *int64, versionCode int64, excludeID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM app_versions
WHERE appid = $1 AND platform = $2 AND version_code = $3 AND id <> $4 AND status <> 'revoked'
  AND channel_id IS NOT DISTINCT FROM $5)`, appID, platform, versionCode, excludeID, channelID).Scan(&exists)
	return exists, err
}

// SaveRelease 新建或更新一个版本，并整组替换安装包（item.Assets 为 nil 时不动安装包）。
// 主下载地址同步写回 download_url / file_size / file_hash，旧检测接口读到的仍是最新的包。
func (r *Repository) SaveRelease(ctx context.Context, item appdomain.Release, assets []appdomain.ReleaseAssetInput) (int64, error) {
	targeting, _ := json.Marshal(item.Targeting)
	var channelID *int64
	if item.Channel != nil {
		channelID = &item.Channel.ID
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	id := item.ID
	if id == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO app_versions
(appid, channel_id, version, version_code, title, release_notes, notes_summary, description, platform, min_os_version,
 update_type, force_update, min_supported_code, status, visibility, targeting, rollout_pct, publish_at, published_at, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::text, $7::text, $8, $9, $10::text, $10::text = 'force', $11, $12, $13, $14, $15, $16, $17, $18, NOW(), NOW())
RETURNING id`,
			item.AppID, channelID, item.Version, item.VersionCode, item.Title, item.Notes, item.Summary, item.Platform, item.MinOSVersion,
			item.UpdateType, item.MinSupportedCode, item.Status, item.Visibility, targeting, item.RolloutPct, item.PublishAt, item.PublishedAt, item.CreatedBy,
		).Scan(&id)
	} else {
		var tag pgconnCommandTag
		tag, err = tx.Exec(ctx, `UPDATE app_versions SET
channel_id = $3, version = $4, version_code = $5, title = $6, release_notes = $7, notes_summary = $8::text, description = $8::text,
platform = $9, min_os_version = $10, update_type = $11::text, force_update = $11::text = 'force', min_supported_code = $12,
status = $13, visibility = $14, targeting = $15, rollout_pct = $16, publish_at = $17, published_at = $18, updated_at = NOW()
WHERE appid = $1 AND id = $2`,
			item.AppID, id, channelID, item.Version, item.VersionCode, item.Title, item.Notes, item.Summary,
			item.Platform, item.MinOSVersion, item.UpdateType, item.MinSupportedCode,
			item.Status, item.Visibility, targeting, item.RolloutPct, item.PublishAt, item.PublishedAt)
		if err == nil && tag.RowsAffected() == 0 {
			return 0, pgx.ErrNoRows
		}
	}
	if err != nil {
		return 0, err
	}

	if assets != nil {
		if err := replaceReleaseAssets(ctx, tx, item.AppID, id, assets); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// pgconnCommandTag 只为拿 RowsAffected，避免在签名里直接依赖 pgconn 包。
type pgconnCommandTag = interface{ RowsAffected() int64 }

func replaceReleaseAssets(ctx context.Context, tx pgx.Tx, appID int64, versionID int64, assets []appdomain.ReleaseAssetInput) error {
	keep := make([]int64, 0, len(assets))
	for _, asset := range assets {
		if asset.ID > 0 {
			keep = append(keep, asset.ID)
		}
	}
	// 留下来的安装包保住下载计数，其余删掉
	if _, err := tx.Exec(ctx, `DELETE FROM app_version_assets WHERE version_id = $1 AND NOT (id = ANY($2))`, versionID, keep); err != nil {
		return err
	}
	for position, asset := range assets {
		if asset.ID > 0 {
			tag, err := tx.Exec(ctx, `UPDATE app_version_assets SET abi = $3, label = $4, url = $5, file_size = $6, sha256 = $7, position = $8, updated_at = NOW()
WHERE id = $1 AND version_id = $2`, asset.ID, versionID, asset.Abi, asset.Label, asset.URL, asset.FileSize, asset.SHA256, position)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				continue
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app_version_assets (version_id, appid, abi, label, url, file_size, sha256, position)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, versionID, appID, asset.Abi, asset.Label, asset.URL, asset.FileSize, asset.SHA256, position); err != nil {
			return err
		}
	}
	// 主下载地址回写旧列：优先通用包，否则第一个
	primary := ""
	var size int64
	hash := ""
	for i, asset := range assets {
		if i == 0 || asset.Abi == appdomain.ReleaseAssetUniversal {
			primary, size, hash = asset.URL, asset.FileSize, asset.SHA256
			if asset.Abi == appdomain.ReleaseAssetUniversal {
				break
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE app_versions SET download_url = $2, file_size = $3, file_hash = $4 WHERE id = $1`, versionID, primary, size, hash)
	return err
}

// SetReleaseStatus 改状态与发布时间。返回 false 表示版本不存在。
func (r *Repository) SetReleaseStatus(ctx context.Context, appID int64, releaseID int64, status string, publishAt *time.Time, publishedAt *time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_versions SET status = $3, publish_at = $4,
published_at = COALESCE(published_at, $5), updated_at = NOW() WHERE appid = $1 AND id = $2`,
		appID, releaseID, status, publishAt, publishedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) SetReleaseRollout(ctx context.Context, appID int64, releaseID int64, pct int) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_versions SET rollout_pct = $3, updated_at = NOW() WHERE appid = $1 AND id = $2`, appID, releaseID, pct)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) DeleteRelease(ctx context.Context, appID int64, releaseID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_versions WHERE appid = $1 AND id = $2`, appID, releaseID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

/* ───────────────────── 渠道成员 ───────────────────── */

// ReleaseUserChannelIDs 用户所在的渠道（显式加入的，不含默认渠道）。
func (r *Repository) ReleaseUserChannelIDs(ctx context.Context, appID int64, userID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT cu.channel_id FROM app_version_channel_users cu
JOIN app_version_channels c ON c.id = cu.channel_id
WHERE cu.appid = $1 AND cu.user_id = $2 AND c.status = true`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) GetVersionChannelByCode(ctx context.Context, appID int64, code string) (*appdomain.AppVersionChannel, error) {
	query := `SELECT id, appid, name, code, description, is_default, status, priority, color, level, rollout_pct, platforms, min_version_code, max_version_code, COALESCE(rules,'[]'::jsonb), COALESCE(target_audience, '{}'::jsonb), self_join, created_at, updated_at FROM app_version_channels WHERE appid = $1 AND code = $2 LIMIT 1`
	return scanVersionChannel(r.pool.QueryRow(ctx, query, appID, code))
}

func (r *Repository) JoinVersionChannel(ctx context.Context, appID int64, channelID int64, userID int64) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO app_version_channel_users (channel_id, user_id, appid, created_at)
VALUES ($1, $2, $3, NOW()) ON CONFLICT (channel_id, user_id) DO NOTHING`, channelID, userID, appID)
	return err
}

func (r *Repository) LeaveVersionChannel(ctx context.Context, appID int64, channelID int64, userID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM app_version_channel_users WHERE channel_id = $1 AND user_id = $2 AND appid = $3`, channelID, userID, appID)
	return err
}

/* ───────────────────── 漏斗统计 ───────────────────── */

var releaseEventColumns = map[string]string{
	appdomain.ReleaseEventOffered:    "offered",
	appdomain.ReleaseEventDownloaded: "downloaded",
	appdomain.ReleaseEventInstalled:  "installed",
	appdomain.ReleaseEventFailed:     "failed",
	appdomain.ReleaseEventDismissed:  "dismissed",
}

// RecordReleaseEvent 计一次漏斗事件。下载事件同时累加版本与安装包的下载数。
func (r *Repository) RecordReleaseEvent(ctx context.Context, releaseID int64, event string, assetID int64, day time.Time) error {
	column, ok := releaseEventColumns[event]
	if !ok {
		return fmt.Errorf("unknown release event %q", event)
	}
	if _, err := r.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO app_version_daily_stats (version_id, day, %[1]s) VALUES ($1, $2, 1)
ON CONFLICT (version_id, day) DO UPDATE SET %[1]s = app_version_daily_stats.%[1]s + 1`, column), releaseID, day.Format("2006-01-02")); err != nil {
		return err
	}
	if event != appdomain.ReleaseEventDownloaded {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `UPDATE app_versions SET download_count = download_count + 1 WHERE id = $1`, releaseID); err != nil {
		return err
	}
	if assetID > 0 {
		_, err := r.pool.Exec(ctx, `UPDATE app_version_assets SET download_count = download_count + 1 WHERE id = $1 AND version_id = $2`, assetID, releaseID)
		return err
	}
	return nil
}

func (r *Repository) ReleaseStats(ctx context.Context, releaseID int64, since time.Time) (*appdomain.ReleaseStats, error) {
	stats := &appdomain.ReleaseStats{ReleaseID: releaseID, Daily: []appdomain.ReleaseDailyStat{}}
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(offered),0), COALESCE(SUM(downloaded),0), COALESCE(SUM(installed),0), COALESCE(SUM(failed),0), COALESCE(SUM(dismissed),0)
FROM app_version_daily_stats WHERE version_id = $1`, releaseID).Scan(
		&stats.Total.Offered, &stats.Total.Downloaded, &stats.Total.Installed, &stats.Total.Failed, &stats.Total.Dismissed); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), offered, downloaded, installed, failed, dismissed
FROM app_version_daily_stats WHERE version_id = $1 AND day >= $2 ORDER BY day`, releaseID, since.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var day appdomain.ReleaseDailyStat
		if err := rows.Scan(&day.Day, &day.Offered, &day.Downloaded, &day.Installed, &day.Failed, &day.Dismissed); err != nil {
			return nil, err
		}
		stats.Daily = append(stats.Daily, day)
	}
	return stats, rows.Err()
}

// ReleaseOverviewCounts 各状态计数与近 14 天漏斗。到点的定时发布计入下发中。
func (r *Repository) ReleaseOverviewCounts(ctx context.Context, appID int64, now time.Time) (*appdomain.ReleaseOverview, error) {
	overview := &appdomain.ReleaseOverview{Latest: []appdomain.Release{}}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
COUNT(*) FILTER (WHERE status = 'draft'),
COUNT(*) FILTER (WHERE status = 'scheduled' AND (publish_at IS NULL OR publish_at > $2)),
COUNT(*) FILTER (WHERE status = 'published' OR (status = 'scheduled' AND publish_at <= $2)),
COUNT(*) FILTER (WHERE status = 'paused'),
COUNT(*) FILTER (WHERE status = 'revoked'),
COUNT(*) FILTER (WHERE (status = 'published' OR (status = 'scheduled' AND publish_at <= $2)) AND rollout_pct < 100)
FROM app_versions WHERE appid = $1`, appID, now).Scan(
		&overview.Total, &overview.Draft, &overview.Scheduled, &overview.Live, &overview.Paused, &overview.Revoked, &overview.RollingOut); err != nil {
		return nil, err
	}
	since := now.AddDate(0, 0, -13).Format("2006-01-02")
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(s.offered),0), COALESCE(SUM(s.downloaded),0), COALESCE(SUM(s.installed),0), COALESCE(SUM(s.failed),0), COALESCE(SUM(s.dismissed),0)
FROM app_version_daily_stats s JOIN app_versions v ON v.id = s.version_id
WHERE v.appid = $1 AND s.day >= $2`, appID, since).Scan(
		&overview.Last14d.Offered, &overview.Last14d.Downloaded, &overview.Last14d.Installed, &overview.Last14d.Failed, &overview.Last14d.Dismissed); err != nil {
		return nil, err
	}
	return overview, nil
}
