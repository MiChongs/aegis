package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"

	"github.com/jackc/pgx/v5"
)

// ════════════════════════════════════════════════════════════
//  文件管理
// ════════════════════════════════════════════════════════════

// IndexStorageObject 索引一条存储对象记录
func (r *Repository) IndexStorageObject(ctx context.Context, obj storagedomain.StorageObject) (*storagedomain.StorageObject, error) {
	metaRaw, _ := json.Marshal(obj.Metadata)
	if obj.Metadata == nil {
		metaRaw = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO storage_objects (config_id, app_id, object_key, file_name, content_type, size, etag, uploaded_by, uploader_type, status, metadata, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
RETURNING id, config_id, app_id, object_key, file_name, content_type, size, etag, uploaded_by, uploader_type, status, COALESCE(metadata,'{}'::jsonb), created_at, deleted_at`,
		obj.ConfigID, obj.AppID, obj.ObjectKey, obj.FileName, obj.ContentType, obj.Size, obj.ETag, obj.UploadedBy, obj.UploaderType, obj.Status, metaRaw)
	return scanStorageObject(row)
}

// ListStorageObjects 分页查询存储对象，支持多条件过滤
func (r *Repository) ListStorageObjects(ctx context.Context, query storagedomain.ObjectListQuery) ([]storagedomain.StorageObject, int64, error) {
	args := make([]any, 0, 8)
	where := buildObjectWhere(query, &args)

	// 计数
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM storage_objects WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, limit := query.Page, query.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}
	offset := (page - 1) * limit
	args = append(args, limit, offset)

	sql := fmt.Sprintf(`SELECT id, config_id, app_id, object_key, file_name, content_type, size, etag, uploaded_by, uploader_type, status, COALESCE(metadata,'{}'::jsonb), created_at, deleted_at
FROM storage_objects WHERE %s ORDER BY id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]storagedomain.StorageObject, 0, limit)
	for rows.Next() {
		obj, err := scanStorageObject(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *obj)
	}
	return items, total, rows.Err()
}

// GetStorageObject 根据 ID 获取存储对象
func (r *Repository) GetStorageObject(ctx context.Context, id int64) (*storagedomain.StorageObject, error) {
	return scanStorageObject(r.pool.QueryRow(ctx, `SELECT id, config_id, app_id, object_key, file_name, content_type, size, etag, uploaded_by, uploader_type, status, COALESCE(metadata,'{}'::jsonb), created_at, deleted_at
FROM storage_objects WHERE id = $1`, id))
}

// SoftDeleteStorageObject 软删除存储对象
func (r *Repository) SoftDeleteStorageObject(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE storage_objects SET status = 'deleted', deleted_at = NOW() WHERE id = $1 AND status <> 'deleted'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("对象不存在或已删除")
	}
	return nil
}

// RestoreStorageObject 恢复已软删除的对象
func (r *Repository) RestoreStorageObject(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE storage_objects SET status = 'active', deleted_at = NULL WHERE id = $1 AND status = 'deleted'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("对象不存在或未处于已删除状态")
	}
	return nil
}

// PermanentDeleteStorageObject 永久删除存储对象
func (r *Repository) PermanentDeleteStorageObject(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_objects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("对象不存在")
	}
	return nil
}

// ListDeletedObjects 查询回收站中的对象
func (r *Repository) ListDeletedObjects(ctx context.Context, configID *int64, page, limit int) ([]storagedomain.StorageObject, int64, error) {
	args := make([]any, 0, 4)
	where := "status = 'deleted'"
	if configID != nil {
		args = append(args, *configID)
		where += fmt.Sprintf(" AND config_id = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM storage_objects WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}
	offset := (page - 1) * limit
	args = append(args, limit, offset)

	sql := fmt.Sprintf(`SELECT id, config_id, app_id, object_key, file_name, content_type, size, etag, uploaded_by, uploader_type, status, COALESCE(metadata,'{}'::jsonb), created_at, deleted_at
FROM storage_objects WHERE %s ORDER BY deleted_at DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]storagedomain.StorageObject, 0, limit)
	for rows.Next() {
		obj, err := scanStorageObject(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *obj)
	}
	return items, total, rows.Err()
}

// CleanupDeletedObjects 清理超过指定时间的已删除对象
func (r *Repository) CleanupDeletedObjects(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_objects WHERE status = 'deleted' AND deleted_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ════════════════════════════════════════════════════════════
//  规则管理
// ════════════════════════════════════════════════════════════

// CreateStorageRule 创建存储规则
func (r *Repository) CreateStorageRule(ctx context.Context, input storagedomain.CreateRuleInput) (*storagedomain.StorageRule, error) {
	ruleRaw, _ := json.Marshal(input.RuleData)
	if input.RuleData == nil {
		ruleRaw = []byte("{}")
	}
	var rule storagedomain.StorageRule
	var ruleDataRaw []byte
	err := r.pool.QueryRow(ctx, `INSERT INTO storage_rules (config_id, app_id, name, rule_type, rule_data, is_active, created_at)
VALUES ($1,$2,$3,$4,$5,TRUE,NOW())
RETURNING id, config_id, app_id, name, rule_type, COALESCE(rule_data,'{}'::jsonb), is_active, created_at`,
		input.ConfigID, input.AppID, input.Name, input.RuleType, ruleRaw).
		Scan(&rule.ID, &rule.ConfigID, &rule.AppID, &rule.Name, &rule.RuleType, &ruleDataRaw, &rule.IsActive, &rule.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(ruleDataRaw, &rule.RuleData)
	if rule.RuleData == nil {
		rule.RuleData = map[string]any{}
	}
	return &rule, nil
}

// ListStorageRules 查询存储规则
func (r *Repository) ListStorageRules(ctx context.Context, configID *int64, appID *int64) ([]storagedomain.StorageRule, error) {
	args := make([]any, 0, 2)
	where := "1=1"
	if configID != nil {
		args = append(args, *configID)
		where += fmt.Sprintf(" AND config_id = $%d", len(args))
	}
	if appID != nil {
		args = append(args, *appID)
		where += fmt.Sprintf(" AND app_id = $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, `SELECT id, config_id, app_id, name, rule_type, COALESCE(rule_data,'{}'::jsonb), is_active, created_at FROM storage_rules WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStorageRules(rows)
}

// UpdateStorageRule 更新存储规则
func (r *Repository) UpdateStorageRule(ctx context.Context, id int64, name string, ruleData map[string]any, isActive bool) error {
	ruleRaw, _ := json.Marshal(ruleData)
	if ruleData == nil {
		ruleRaw = []byte("{}")
	}
	tag, err := r.pool.Exec(ctx, `UPDATE storage_rules SET name = $2, rule_data = $3, is_active = $4 WHERE id = $1`, id, name, ruleRaw, isActive)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("规则不存在")
	}
	return nil
}

// DeleteStorageRule 删除存储规则
func (r *Repository) DeleteStorageRule(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("规则不存在")
	}
	return nil
}

// GetActiveUploadRules 获取指定存储配置的激活上传规则
func (r *Repository) GetActiveUploadRules(ctx context.Context, configID int64, appID *int64) ([]storagedomain.StorageRule, error) {
	args := []any{configID}
	where := "is_active = TRUE AND (config_id = $1 OR config_id IS NULL)"
	if appID != nil {
		args = append(args, *appID)
		where += fmt.Sprintf(" AND (app_id = $%d OR app_id IS NULL)", len(args))
	}
	rows, err := r.pool.Query(ctx, `SELECT id, config_id, app_id, name, rule_type, COALESCE(rule_data,'{}'::jsonb), is_active, created_at FROM storage_rules WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStorageRules(rows)
}

// ════════════════════════════════════════════════════════════
//  CDN 配置
// ════════════════════════════════════════════════════════════

// UpsertCDNConfig 创建或更新 CDN 配置（按 config_id 唯一）
func (r *Repository) UpsertCDNConfig(ctx context.Context, configID int64, input storagedomain.UpsertCDNConfigInput) (*storagedomain.CDNConfig, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 尝试查找现有记录
	var existingID int64
	err = tx.QueryRow(ctx, `SELECT id FROM storage_cdn_configs WHERE config_id = $1 LIMIT 1`, configID).Scan(&existingID)

	var cdn storagedomain.CDNConfig
	if err == pgx.ErrNoRows {
		// 插入新记录
		err = tx.QueryRow(ctx, `INSERT INTO storage_cdn_configs (config_id, cdn_domain, cdn_protocol, cache_max_age, referer_whitelist, referer_blacklist, ip_whitelist, sign_url_enabled, sign_url_secret, sign_url_ttl, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
RETURNING id, config_id, cdn_domain, cdn_protocol, cache_max_age, referer_whitelist, referer_blacklist, ip_whitelist, sign_url_enabled, sign_url_secret, sign_url_ttl, created_at, updated_at`,
			configID, input.CDNDomain, input.CDNProtocol, input.CacheMaxAge,
			ensureStringSlice(input.RefererWhitelist), ensureStringSlice(input.RefererBlacklist), ensureStringSlice(input.IPWhitelist),
			input.SignURLEnabled, input.SignURLSecret, input.SignURLTTL).
			Scan(&cdn.ID, &cdn.ConfigID, &cdn.CDNDomain, &cdn.CDNProtocol, &cdn.CacheMaxAge,
				&cdn.RefererWhitelist, &cdn.RefererBlacklist, &cdn.IPWhitelist,
				&cdn.SignURLEnabled, &cdn.SignURLSecret, &cdn.SignURLTTL, &cdn.CreatedAt, &cdn.UpdatedAt)
	} else if err == nil {
		// 更新已有记录
		err = tx.QueryRow(ctx, `UPDATE storage_cdn_configs SET cdn_domain=$2, cdn_protocol=$3, cache_max_age=$4, referer_whitelist=$5, referer_blacklist=$6, ip_whitelist=$7, sign_url_enabled=$8, sign_url_secret=$9, sign_url_ttl=$10, updated_at=NOW()
WHERE id = $1
RETURNING id, config_id, cdn_domain, cdn_protocol, cache_max_age, referer_whitelist, referer_blacklist, ip_whitelist, sign_url_enabled, sign_url_secret, sign_url_ttl, created_at, updated_at`,
			existingID, input.CDNDomain, input.CDNProtocol, input.CacheMaxAge,
			ensureStringSlice(input.RefererWhitelist), ensureStringSlice(input.RefererBlacklist), ensureStringSlice(input.IPWhitelist),
			input.SignURLEnabled, input.SignURLSecret, input.SignURLTTL).
			Scan(&cdn.ID, &cdn.ConfigID, &cdn.CDNDomain, &cdn.CDNProtocol, &cdn.CacheMaxAge,
				&cdn.RefererWhitelist, &cdn.RefererBlacklist, &cdn.IPWhitelist,
				&cdn.SignURLEnabled, &cdn.SignURLSecret, &cdn.SignURLTTL, &cdn.CreatedAt, &cdn.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &cdn, nil
}

// GetCDNConfig 获取 CDN 配置
func (r *Repository) GetCDNConfig(ctx context.Context, configID int64) (*storagedomain.CDNConfig, error) {
	var cdn storagedomain.CDNConfig
	err := r.pool.QueryRow(ctx, `SELECT id, config_id, cdn_domain, cdn_protocol, cache_max_age, referer_whitelist, referer_blacklist, ip_whitelist, sign_url_enabled, sign_url_secret, sign_url_ttl, created_at, updated_at
FROM storage_cdn_configs WHERE config_id = $1`, configID).
		Scan(&cdn.ID, &cdn.ConfigID, &cdn.CDNDomain, &cdn.CDNProtocol, &cdn.CacheMaxAge,
			&cdn.RefererWhitelist, &cdn.RefererBlacklist, &cdn.IPWhitelist,
			&cdn.SignURLEnabled, &cdn.SignURLSecret, &cdn.SignURLTTL, &cdn.CreatedAt, &cdn.UpdatedAt)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return &cdn, nil
}

// DeleteCDNConfig 删除 CDN 配置
func (r *Repository) DeleteCDNConfig(ctx context.Context, configID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_cdn_configs WHERE config_id = $1`, configID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("CDN 配置不存在")
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  图片规则
// ════════════════════════════════════════════════════════════

// CreateImageRule 创建图片处理规则
func (r *Repository) CreateImageRule(ctx context.Context, input storagedomain.CreateImageRuleInput) (*storagedomain.ImageRule, error) {
	ruleRaw, _ := json.Marshal(input.RuleData)
	if input.RuleData == nil {
		ruleRaw = []byte("{}")
	}
	var rule storagedomain.ImageRule
	var ruleDataRaw []byte
	err := r.pool.QueryRow(ctx, `INSERT INTO storage_image_rules (config_id, name, rule_type, rule_data, is_active, created_at)
VALUES ($1,$2,$3,$4,TRUE,NOW())
RETURNING id, config_id, name, rule_type, COALESCE(rule_data,'{}'::jsonb), is_active, created_at`,
		input.ConfigID, input.Name, input.RuleType, ruleRaw).
		Scan(&rule.ID, &rule.ConfigID, &rule.Name, &rule.RuleType, &ruleDataRaw, &rule.IsActive, &rule.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(ruleDataRaw, &rule.RuleData)
	if rule.RuleData == nil {
		rule.RuleData = map[string]any{}
	}
	return &rule, nil
}

// ListImageRules 查询图片处理规则
func (r *Repository) ListImageRules(ctx context.Context, configID *int64) ([]storagedomain.ImageRule, error) {
	args := make([]any, 0, 1)
	where := "1=1"
	if configID != nil {
		args = append(args, *configID)
		where += fmt.Sprintf(" AND config_id = $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, `SELECT id, config_id, name, rule_type, COALESCE(rule_data,'{}'::jsonb), is_active, created_at FROM storage_image_rules WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []storagedomain.ImageRule
	for rows.Next() {
		var rule storagedomain.ImageRule
		var ruleDataRaw []byte
		if err := rows.Scan(&rule.ID, &rule.ConfigID, &rule.Name, &rule.RuleType, &ruleDataRaw, &rule.IsActive, &rule.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ruleDataRaw, &rule.RuleData)
		if rule.RuleData == nil {
			rule.RuleData = map[string]any{}
		}
		items = append(items, rule)
	}
	return items, rows.Err()
}

// DeleteImageRule 删除图片处理规则
func (r *Repository) DeleteImageRule(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_image_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("图片规则不存在")
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  用量统计
// ════════════════════════════════════════════════════════════

// CreateUsageSnapshot 写入一条用量快照
func (r *Repository) CreateUsageSnapshot(ctx context.Context, snapshot storagedomain.UsageSnapshot) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO storage_usage_snapshots (config_id, app_id, total_files, total_size, active_files, deleted_files, snapshot_at)
VALUES ($1,$2,$3,$4,$5,$6,NOW())`,
		snapshot.ConfigID, snapshot.AppID, snapshot.TotalFiles, snapshot.TotalSize, snapshot.ActiveFiles, snapshot.DeletedFiles)
	return err
}

// GetLatestUsageSnapshot 获取最新用量快照
func (r *Repository) GetLatestUsageSnapshot(ctx context.Context, configID int64) (*storagedomain.UsageSnapshot, error) {
	var s storagedomain.UsageSnapshot
	err := r.pool.QueryRow(ctx, `SELECT id, config_id, app_id, total_files, total_size, active_files, deleted_files, snapshot_at
FROM storage_usage_snapshots WHERE config_id = $1 ORDER BY snapshot_at DESC LIMIT 1`, configID).
		Scan(&s.ID, &s.ConfigID, &s.AppID, &s.TotalFiles, &s.TotalSize, &s.ActiveFiles, &s.DeletedFiles, &s.SnapshotAt)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return &s, nil
}

// GetUsageHistory 获取用量历史（最近 N 天）
func (r *Repository) GetUsageHistory(ctx context.Context, configID int64, days int) ([]storagedomain.UsageSnapshot, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := r.pool.Query(ctx, `SELECT id, config_id, app_id, total_files, total_size, active_files, deleted_files, snapshot_at
FROM storage_usage_snapshots WHERE config_id = $1 AND snapshot_at > $2 ORDER BY snapshot_at`, configID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []storagedomain.UsageSnapshot
	for rows.Next() {
		var s storagedomain.UsageSnapshot
		if err := rows.Scan(&s.ID, &s.ConfigID, &s.AppID, &s.TotalFiles, &s.TotalSize, &s.ActiveFiles, &s.DeletedFiles, &s.SnapshotAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// GetRealtimeUsageStats 从 storage_objects 实时聚合用量（configID=0 时全局汇总）
func (r *Repository) GetRealtimeUsageStats(ctx context.Context, configID int64) (*storagedomain.UsageStats, error) {
	args := make([]any, 0, 1)
	where := ""
	if configID > 0 {
		args = append(args, configID)
		where = fmt.Sprintf(" WHERE config_id = $%d", len(args))
	}
	query := `SELECT
		COUNT(*) AS total_files,
		COALESCE(SUM(size), 0) AS total_size,
		COUNT(*) FILTER (WHERE status = 'active') AS active_files,
		COUNT(*) FILTER (WHERE status = 'deleted') AS deleted_files
	FROM storage_objects` + where
	var stats storagedomain.UsageStats
	stats.ConfigID = configID
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&stats.TotalFiles, &stats.TotalSize, &stats.ActiveFiles, &stats.DeletedFiles); err != nil {
		return nil, err
	}
	return &stats, nil
}

// GetObjectTypeStats 按文件类型统计对象数量和大小
func (r *Repository) GetObjectTypeStats(ctx context.Context, configID *int64) ([]storagedomain.TypeStat, error) {
	args := make([]any, 0, 1)
	where := "status = 'active'"
	if configID != nil {
		args = append(args, *configID)
		where += fmt.Sprintf(" AND config_id = $%d", len(args))
	}
	rows, err := r.pool.Query(ctx, `SELECT content_type, COUNT(*), COALESCE(SUM(size),0) FROM storage_objects WHERE `+where+` GROUP BY content_type ORDER BY COUNT(*) DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []storagedomain.TypeStat
	for rows.Next() {
		var t storagedomain.TypeStat
		if err := rows.Scan(&t.ContentType, &t.Count, &t.Size); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  内部辅助
// ════════════════════════════════════════════════════════════

func buildObjectWhere(query storagedomain.ObjectListQuery, args *[]any) string {
	parts := []string{"1=1"}
	if query.ConfigID != nil {
		*args = append(*args, *query.ConfigID)
		parts = append(parts, fmt.Sprintf("config_id = $%d", len(*args)))
	}
	if query.AppID != nil {
		*args = append(*args, *query.AppID)
		parts = append(parts, fmt.Sprintf("app_id = $%d", len(*args)))
	}
	if p := strings.TrimSpace(query.Prefix); p != "" {
		*args = append(*args, p+"%")
		parts = append(parts, fmt.Sprintf("object_key LIKE $%d", len(*args)))
	}
	if ct := strings.TrimSpace(query.ContentType); ct != "" {
		*args = append(*args, ct)
		parts = append(parts, fmt.Sprintf("content_type = $%d", len(*args)))
	}
	if s := strings.TrimSpace(query.Status); s != "" {
		*args = append(*args, s)
		parts = append(parts, fmt.Sprintf("status = $%d", len(*args)))
	}
	return strings.Join(parts, " AND ")
}

func scanStorageObject(row interface{ Scan(dest ...any) error }) (*storagedomain.StorageObject, error) {
	var obj storagedomain.StorageObject
	var metaRaw []byte
	if err := row.Scan(&obj.ID, &obj.ConfigID, &obj.AppID, &obj.ObjectKey, &obj.FileName, &obj.ContentType,
		&obj.Size, &obj.ETag, &obj.UploadedBy, &obj.UploaderType, &obj.Status, &metaRaw, &obj.CreatedAt, &obj.DeletedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(metaRaw, &obj.Metadata)
	if obj.Metadata == nil {
		obj.Metadata = map[string]any{}
	}
	return &obj, nil
}

func scanStorageRules(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]storagedomain.StorageRule, error) {
	var items []storagedomain.StorageRule
	for rows.Next() {
		var rule storagedomain.StorageRule
		var ruleDataRaw []byte
		if err := rows.Scan(&rule.ID, &rule.ConfigID, &rule.AppID, &rule.Name, &rule.RuleType, &ruleDataRaw, &rule.IsActive, &rule.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ruleDataRaw, &rule.RuleData)
		if rule.RuleData == nil {
			rule.RuleData = map[string]any{}
		}
		items = append(items, rule)
	}
	return items, rows.Err()
}

// ensureStringSlice 确保 []string 不为 nil（pgx 写入 TEXT[] 时需要非 nil 切片）
func ensureStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
