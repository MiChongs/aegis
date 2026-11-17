package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	storagedomain "aegis/internal/domain/storage"
)

// storageObjectColumns 与 scanStorageObject 的列顺序一致。
const storageObjectColumns = `id, config_id, app_id, object_key, file_name, content_type, size, etag, uploaded_by, uploader_type, status, COALESCE(metadata,'{}'::jsonb), created_at, deleted_at`

// ErrStoragePoolNotFound 存储池不存在。
var ErrStoragePoolNotFound = errors.New("storage pool not found")

// ErrStoragePoolNameTaken 存储池名称已被占用。
var ErrStoragePoolNameTaken = errors.New("storage pool name taken")

// ErrStorageMigrationNotFound 迁移任务不存在。
var ErrStorageMigrationNotFound = errors.New("storage migration not found")

const storagePoolColumns = `id, name, description, strategy, enabled, is_default, read_fallback, max_attempts, warn_percent, created_at, updated_at`

func scanStoragePool(row pgx.Row) (*storagedomain.Pool, error) {
	var pool storagedomain.Pool
	if err := row.Scan(&pool.ID, &pool.Name, &pool.Description, &pool.Strategy, &pool.Enabled, &pool.IsDefault,
		&pool.ReadFallback, &pool.MaxAttempts, &pool.WarnPercent, &pool.CreatedAt, &pool.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStoragePoolNotFound
		}
		return nil, err
	}
	pool.Members = []storagedomain.PoolMember{}
	return &pool, nil
}

// ListStoragePools 全部存储池（含成员，按优先级排好）。
func (r *Repository) ListStoragePools(ctx context.Context) ([]storagedomain.Pool, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+storagePoolColumns+` FROM storage_pools ORDER BY is_default DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	pools := []storagedomain.Pool{}
	index := map[int64]int{}
	for rows.Next() {
		pool, err := scanStoragePool(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[pool.ID] = len(pools)
		pools = append(pools, *pool)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return pools, nil
	}
	memberRows, err := r.pool.Query(ctx, `SELECT pool_id, config_id, priority, weight, capacity_bytes, mode
FROM storage_pool_members ORDER BY pool_id, priority, config_id`)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var poolID int64
		var member storagedomain.PoolMember
		if err := memberRows.Scan(&poolID, &member.ConfigID, &member.Priority, &member.Weight, &member.CapacityBytes, &member.Mode); err != nil {
			return nil, err
		}
		if i, ok := index[poolID]; ok {
			pools[i].Members = append(pools[i].Members, member)
		}
	}
	return pools, memberRows.Err()
}

// GetStoragePool 一个存储池（含成员）。
func (r *Repository) GetStoragePool(ctx context.Context, id int64) (*storagedomain.Pool, error) {
	pool, err := scanStoragePool(r.pool.QueryRow(ctx, `SELECT `+storagePoolColumns+` FROM storage_pools WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	members, err := r.storagePoolMembers(ctx, r.pool, id)
	if err != nil {
		return nil, err
	}
	pool.Members = members
	return pool, nil
}

func (r *Repository) storagePoolMembers(ctx context.Context, q cloudQuerier, poolID int64) ([]storagedomain.PoolMember, error) {
	rows, err := q.Query(ctx, `SELECT config_id, priority, weight, capacity_bytes, mode
FROM storage_pool_members WHERE pool_id = $1 ORDER BY priority, config_id`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []storagedomain.PoolMember{}
	for rows.Next() {
		var member storagedomain.PoolMember
		if err := rows.Scan(&member.ConfigID, &member.Priority, &member.Weight, &member.CapacityBytes, &member.Mode); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// SaveStoragePool 新建（id = 0）或整体替换一个存储池及其成员。设为默认时取消其他池的默认。
func (r *Repository) SaveStoragePool(ctx context.Context, id int64, input storagedomain.PoolInput) (saved *storagedomain.Pool, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var taken bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM storage_pools WHERE name = $1 AND id <> $2)`, input.Name, id).Scan(&taken); err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrStoragePoolNameTaken
	}
	if input.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE storage_pools SET is_default = FALSE, updated_at = NOW() WHERE is_default AND id <> $1`, id); err != nil {
			return nil, err
		}
	}
	if id > 0 {
		var tag interface{ RowsAffected() int64 }
		tag, err = tx.Exec(ctx, `UPDATE storage_pools SET name = $2, description = $3, strategy = $4, enabled = $5,
is_default = $6, read_fallback = $7, max_attempts = $8, warn_percent = $9, updated_at = NOW() WHERE id = $1`,
			id, input.Name, input.Description, input.Strategy, input.Enabled, input.IsDefault, input.ReadFallback,
			input.MaxAttempts, input.WarnPercent)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrStoragePoolNotFound
		}
		if _, err = tx.Exec(ctx, `DELETE FROM storage_pool_members WHERE pool_id = $1`, id); err != nil {
			return nil, err
		}
	} else {
		if err = tx.QueryRow(ctx, `INSERT INTO storage_pools (name, description, strategy, enabled, is_default, read_fallback, max_attempts, warn_percent)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			input.Name, input.Description, input.Strategy, input.Enabled, input.IsDefault, input.ReadFallback,
			input.MaxAttempts, input.WarnPercent).Scan(&id); err != nil {
			return nil, err
		}
	}
	for _, member := range input.Members {
		if _, err = tx.Exec(ctx, `INSERT INTO storage_pool_members (pool_id, config_id, priority, weight, capacity_bytes, mode)
VALUES ($1,$2,$3,$4,$5,$6)`, id, member.ConfigID, member.Priority, member.Weight, member.CapacityBytes, member.Mode); err != nil {
			return nil, err
		}
	}
	pool, err := scanStoragePool(tx.QueryRow(ctx, `SELECT `+storagePoolColumns+` FROM storage_pools WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	if pool.Members, err = r.storagePoolMembers(ctx, tx, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return pool, nil
}

// DeleteStoragePool 删除存储池（成员随之删除，存储配置本身不动）。
func (r *Repository) DeleteStoragePool(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_pools WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStoragePoolNotFound
	}
	return nil
}

// StorageConfigPoolIDs 每个存储配置所在的池。
func (r *Repository) StorageConfigPoolIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT config_id, pool_id FROM storage_pool_members ORDER BY config_id, pool_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int64][]int64{}
	for rows.Next() {
		var configID, poolID int64
		if err := rows.Scan(&configID, &poolID); err != nil {
			return nil, err
		}
		result[configID] = append(result[configID], poolID)
	}
	return result, rows.Err()
}

// StorageConfigUsage 各存储配置下索引过的对象总量（含回收站里的：它们在被清除前仍占着空间）。
func (r *Repository) StorageConfigUsage(ctx context.Context) (map[int64]storagedomain.MemberUsage, error) {
	rows, err := r.pool.Query(ctx, `SELECT config_id, COALESCE(SUM(size), 0), COUNT(*) FROM storage_objects GROUP BY config_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	result := map[int64]storagedomain.MemberUsage{}
	for rows.Next() {
		var configID int64
		var usage storagedomain.MemberUsage
		if err := rows.Scan(&configID, &usage.UsedBytes, &usage.Objects); err != nil {
			return nil, err
		}
		usage.Source = "index"
		usage.UpdatedAt = now
		result[configID] = usage
	}
	return result, rows.Err()
}

// SetStorageConfigRedirect 设置 / 清除读取转向。
func (r *Repository) SetStorageConfigRedirect(ctx context.Context, configID int64, target *int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE storage_configs SET redirect_config_id = $2, updated_at = NOW() WHERE id = $1`, configID, target)
	return err
}

// ── 事件 ──────────────────────────

// InsertStoragePoolEvent 追加一条事件。
func (r *Repository) InsertStoragePoolEvent(ctx context.Context, event storagedomain.PoolEvent) error {
	detail, _ := json.Marshal(event.Detail)
	if event.Detail == nil {
		detail = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO storage_pool_events (pool_id, config_id, level, kind, message, detail)
VALUES ($1,$2,$3,$4,$5,$6)`, event.PoolID, event.ConfigID, event.Level, event.Kind, event.Message, detail)
	return err
}

// ListStoragePoolEvents 最近的事件，新的在前。
func (r *Repository) ListStoragePoolEvents(ctx context.Context, query storagedomain.EventQuery) ([]storagedomain.PoolEvent, error) {
	args := []any{}
	where := []string{"TRUE"}
	if query.PoolID != nil {
		args = append(args, *query.PoolID)
		where = append(where, fmt.Sprintf("e.pool_id = $%d", len(args)))
	}
	if query.ConfigID != nil {
		args = append(args, *query.ConfigID)
		where = append(where, fmt.Sprintf("e.config_id = $%d", len(args)))
	}
	if level := strings.TrimSpace(query.Level); level != "" {
		args = append(args, level)
		where = append(where, fmt.Sprintf("e.level = $%d", len(args)))
	}
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, `SELECT e.id, e.pool_id, e.config_id, e.level, e.kind, e.message, e.detail, e.created_at,
COALESCE(p.name, ''), COALESCE(c.config_name, '')
FROM storage_pool_events e
LEFT JOIN storage_pools p ON p.id = e.pool_id
LEFT JOIN storage_configs c ON c.id = e.config_id
WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(` ORDER BY e.created_at DESC, e.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []storagedomain.PoolEvent{}
	for rows.Next() {
		var event storagedomain.PoolEvent
		var detail []byte
		if err := rows.Scan(&event.ID, &event.PoolID, &event.ConfigID, &event.Level, &event.Kind, &event.Message, &detail,
			&event.CreatedAt, &event.PoolName, &event.ConfigName); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(detail, &event.Detail)
		events = append(events, event)
	}
	return events, rows.Err()
}

// PruneStoragePoolEvents 清掉早于 before 的事件。
func (r *Repository) PruneStoragePoolEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM storage_pool_events WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 迁移 ──────────────────────────

const storageMigrationColumns = `m.id, m.source_config_id, m.target_config_id, m.status, m.delete_source, m.set_redirect,
m.total_objects, m.total_bytes, m.copied_objects, m.copied_bytes, m.recovered_objects, m.skipped_objects, m.failed_objects, m.last_object_id,
m.last_error, m.created_by, m.created_at, m.started_at, m.finished_at, m.updated_at,
COALESCE(s.config_name, ''), COALESCE(t.config_name, '')`

const storageMigrationFrom = ` FROM storage_migrations m
LEFT JOIN storage_configs s ON s.id = m.source_config_id
LEFT JOIN storage_configs t ON t.id = m.target_config_id`

func scanStorageMigration(row pgx.Row) (*storagedomain.Migration, error) {
	var m storagedomain.Migration
	if err := row.Scan(&m.ID, &m.SourceConfigID, &m.TargetConfigID, &m.Status, &m.DeleteSource, &m.SetRedirect,
		&m.TotalObjects, &m.TotalBytes, &m.CopiedObjects, &m.CopiedBytes, &m.RecoveredObjects, &m.SkippedObjects, &m.FailedObjects, &m.LastObjectID,
		&m.LastError, &m.CreatedBy, &m.CreatedAt, &m.StartedAt, &m.FinishedAt, &m.UpdatedAt, &m.SourceName, &m.TargetName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStorageMigrationNotFound
		}
		return nil, err
	}
	return &m, nil
}

// CreateStorageMigration 新建迁移任务，并统计源配置下待迁移的对象数与字节数。
func (r *Repository) CreateStorageMigration(ctx context.Context, input storagedomain.MigrationInput, createdBy string) (*storagedomain.Migration, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO storage_migrations
(source_config_id, target_config_id, delete_source, set_redirect, created_by, total_objects, total_bytes)
SELECT $1, $2, $3, $4, $5, COUNT(*), COALESCE(SUM(size), 0) FROM storage_objects WHERE config_id = $1
RETURNING id`, input.SourceConfigID, input.TargetConfigID, input.DeleteSource, input.SetRedirect, createdBy).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetStorageMigration(ctx, id)
}

// GetStorageMigration 一个迁移任务。
func (r *Repository) GetStorageMigration(ctx context.Context, id int64) (*storagedomain.Migration, error) {
	return scanStorageMigration(r.pool.QueryRow(ctx, `SELECT `+storageMigrationColumns+storageMigrationFrom+` WHERE m.id = $1`, id))
}

// ListStorageMigrations 最近的迁移任务。
func (r *Repository) ListStorageMigrations(ctx context.Context, limit int) ([]storagedomain.Migration, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT `+storageMigrationColumns+storageMigrationFrom+` ORDER BY m.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []storagedomain.Migration{}
	for rows.Next() {
		m, err := scanStorageMigration(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *m)
	}
	return items, rows.Err()
}

// ActiveStorageMigrationFor 某个源配置上进行中的迁移（避免同一个源并行跑两个任务）。
func (r *Repository) ActiveStorageMigrationFor(ctx context.Context, sourceConfigID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM storage_migrations
WHERE source_config_id = $1 AND status IN ('pending', 'running', 'paused'))`, sourceConfigID).Scan(&exists)
	return exists, err
}

// StorageMigrationsWithStatus 指定状态的迁移（进程启动时恢复运行中的任务）。
func (r *Repository) StorageMigrationsWithStatus(ctx context.Context, statuses ...string) ([]storagedomain.Migration, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+storageMigrationColumns+storageMigrationFrom+` WHERE m.status = ANY($1) ORDER BY m.id`, statuses)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []storagedomain.Migration{}
	for rows.Next() {
		m, err := scanStorageMigration(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *m)
	}
	return items, rows.Err()
}

// SetStorageMigrationStatus 改状态；from 非空时只在当前状态属于 from 时生效（并发保护）。
func (r *Repository) SetStorageMigrationStatus(ctx context.Context, id int64, status string, from []string, lastError string) (bool, error) {
	query := `UPDATE storage_migrations SET status = $2::VARCHAR, last_error = CASE WHEN $3::TEXT = '' THEN last_error ELSE $3::TEXT END,
started_at = CASE WHEN $2::VARCHAR = 'running' AND started_at IS NULL THEN NOW() ELSE started_at END,
finished_at = CASE WHEN $2::VARCHAR IN ('completed', 'failed', 'cancelled') THEN NOW() ELSE finished_at END,
updated_at = NOW() WHERE id = $1`
	args := []any{id, status, lastError}
	if len(from) > 0 {
		query += ` AND status = ANY($4)`
		args = append(args, from)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimStorageMigration 认领一个迁移任务并置为 running：同一时刻只有一个实例能跑同一个任务。
//
// 能认领的条件：状态属于 from，且没人持有、持有者就是自己、或持有者的心跳已经超过 staleAfter 没更新（宕机了）。
// force 为 true 时不看持有者（单实例部署：没有别人会来抢，进程重启后不必干等旧心跳过期）。
// 认领与状态变更在同一条语句里完成，两个实例同时来认领只有一个能成。
func (r *Repository) ClaimStorageMigration(ctx context.Context, id int64, owner string, from []string, staleAfter time.Duration, force bool) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE storage_migrations SET status = 'running', owner = $2, heartbeat_at = NOW(),
started_at = COALESCE(started_at, NOW()), updated_at = NOW()
WHERE id = $1 AND status = ANY($3)
  AND ($5 OR owner = '' OR owner = $2 OR heartbeat_at IS NULL OR heartbeat_at < NOW() - make_interval(secs => $4))`,
		id, owner, from, staleAfter.Seconds(), force)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// HeartbeatStorageMigration 持有者续心跳。返回 false 表示任务已经不归自己跑了：
// 被别的实例接手、或者被暂停 / 取消（状态不再是 running）—— 调用方应当立刻停下。
func (r *Repository) HeartbeatStorageMigration(ctx context.Context, id int64, owner string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE storage_migrations SET heartbeat_at = NOW()
WHERE id = $1 AND owner = $2 AND status = 'running'`, id, owner)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ReleaseStorageMigration 持有者停下时放手，让别的实例（或「继续」之后的本实例）不必等心跳过期就能接手。
func (r *Repository) ReleaseStorageMigration(ctx context.Context, id int64, owner string) error {
	_, err := r.pool.Exec(ctx, `UPDATE storage_migrations SET owner = '' WHERE id = $1 AND owner = $2`, id, owner)
	return err
}

// StorageMigrationProgress 一批对象处理完后推进游标与计数。
type StorageMigrationProgress struct {
	LastObjectID     int64
	CopiedObjects    int64
	CopiedBytes      int64
	RecoveredObjects int64
	SkippedObjects   int64
	FailedObjects    int64
	LastError        string
}

// AdvanceStorageMigration 累加计数并推进游标。
func (r *Repository) AdvanceStorageMigration(ctx context.Context, id int64, p StorageMigrationProgress) error {
	_, err := r.pool.Exec(ctx, `UPDATE storage_migrations SET last_object_id = GREATEST(last_object_id, $2),
copied_objects = copied_objects + $3, copied_bytes = copied_bytes + $4, skipped_objects = skipped_objects + $5,
failed_objects = failed_objects + $6, last_error = CASE WHEN $7::TEXT = '' THEN last_error ELSE $7::TEXT END,
recovered_objects = recovered_objects + $8, updated_at = NOW()
WHERE id = $1`, id, p.LastObjectID, p.CopiedObjects, p.CopiedBytes, p.SkippedObjects, p.FailedObjects, p.LastError, p.RecoveredObjects)
	return err
}

// StorageObjectsAfter 按 id 游标取一个配置下的一批对象。
func (r *Repository) StorageObjectsAfter(ctx context.Context, configID, afterID int64, limit int) ([]storagedomain.StorageObject, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+storageObjectColumns+` FROM storage_objects
WHERE config_id = $1 AND id > $2 ORDER BY id LIMIT $3`, configID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []storagedomain.StorageObject{}
	for rows.Next() {
		item, err := scanStorageObject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// RestartStorageMigration 继续一个暂停或失败的任务：游标归零、失败计数清空，从头再扫一遍源配置。
//
// 复制成功或已跳过的对象索引早已改到目标配置，重扫只会碰到上次失败和还没处理的那些 ——
// 「继续」因此同时就是「重试失败项」，不会重复复制。
func (r *Repository) RestartStorageMigration(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE storage_migrations SET status = 'pending', last_object_id = 0, failed_objects = 0,
last_error = '', finished_at = NULL, updated_at = NOW()
WHERE id = $1 AND status IN ('paused', 'failed')`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkStorageObjectMissing 源对象已丢失且没有副本：索引改到目标配置（免得反复扫到），
// 在用的标记为 missing —— 文件管理与用量不再把它当成真实存在的文件。
func (r *Repository) MarkStorageObjectMissing(ctx context.Context, objectID, targetConfigID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE storage_objects SET config_id = $2,
status = CASE WHEN status = 'active' THEN 'missing' ELSE status END WHERE id = $1`, objectID, targetConfigID)
	return err
}

// StorageObjectCopy 数据库里某个对象的字节副本：头像副本（avatar_asset_blobs，size 0 是原图，
// 其余对应 variants 里同尺寸的那一档）或云存储修订内容（app_cloud_item_revisions.content）。
// 没有副本时 data 为 nil。
func (r *Repository) StorageObjectCopy(ctx context.Context, configID int64, objectKey string) (string, []byte, error) {
	var contentType string
	var data []byte
	err := r.pool.QueryRow(ctx, `SELECT b.content_type, b.data FROM avatar_assets a
JOIN avatar_asset_blobs b ON b.asset_id = a.id
WHERE a.config_id = $1 AND (
    (b.size = 0 AND a.base_key = $2)
    OR EXISTS (SELECT 1 FROM jsonb_array_elements(a.variants) v WHERE v->>'key' = $2 AND (v->>'size')::int = b.size)
)
ORDER BY a.id DESC LIMIT 1`, configID, objectKey).Scan(&contentType, &data)
	if err == nil {
		return contentType, data, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	err = r.pool.QueryRow(ctx, `SELECT content_type, content FROM app_cloud_item_revisions
WHERE storage_config_id = $1 AND object_key = $2 AND content IS NOT NULL
ORDER BY id DESC LIMIT 1`, configID, objectKey).Scan(&contentType, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return contentType, data, nil
}

// MoveStorageObject 对象复制到目标配置后，把索引行改记到目标配置下。
func (r *Repository) MoveStorageObject(ctx context.Context, objectID, targetConfigID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE storage_objects SET config_id = $2 WHERE id = $1`, objectID, targetConfigID)
	return err
}

// GetStoragePoolByName 按名称取池（含成员）。
func (r *Repository) GetStoragePoolByName(ctx context.Context, name string) (*storagedomain.Pool, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM storage_pools WHERE name = $1`, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStoragePoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.GetStoragePool(ctx, id)
}

// DefaultStoragePool 启用中的默认池；没有时返回 ErrStoragePoolNotFound。
func (r *Repository) DefaultStoragePool(ctx context.Context) (*storagedomain.Pool, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM storage_pools WHERE is_default AND enabled`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStoragePoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.GetStoragePool(ctx, id)
}

// StorageObjectTotals 一个应用（可再限定到某个存储配置）索引过的对象总字节数与文件数（配额规则用）。
func (r *Repository) StorageObjectTotals(ctx context.Context, appID int64, configID *int64) (int64, int64, error) {
	var bytes, files int64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(size), 0), COUNT(*) FROM storage_objects
WHERE app_id = $1 AND ($2::BIGINT IS NULL OR config_id = $2)`, appID, configID).Scan(&bytes, &files)
	return bytes, files, err
}

// DeletedStorageObjectsBefore 回收站里删除时间早于 cutoff 的对象（清理用，按批取）。
func (r *Repository) DeletedStorageObjectsBefore(ctx context.Context, cutoff time.Time, limit int) ([]storagedomain.StorageObject, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+storageObjectColumns+` FROM storage_objects
WHERE status = 'deleted' AND deleted_at < $1 ORDER BY id LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []storagedomain.StorageObject{}
	for rows.Next() {
		item, err := scanStorageObject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
