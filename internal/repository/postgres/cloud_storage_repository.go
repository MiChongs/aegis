package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	cloudstorage "aegis/internal/domain/cloudstorage"
)

// 用户云存储：配置、用户账目、条目与修订。
//
// 所有改变用量的写操作都遵循同一个顺序：先锁用户行（app_cloud_storage_users FOR UPDATE），
// 再锁条目行，最后在同一事务里从修订表**重算**用量写回。锁用户行把同一个用户的并发写入
// 串起来 —— 否则两次各自没超配额的写入可以一起把配额撑破；重算而不是加减，
// 是为了让账目永远等于修订表的真实总和（见 000087 迁移的注释）。
//
// 存储桶里的对象不在这里删：事务只交回「哪些对象该删了」，由服务层在提交之后去删。
// 先删对象再提交会在事务回滚时留下一个指向已删除对象的修订。

var (
	// ErrCloudItemNotFound 条目不存在（或不属于该用户）。
	ErrCloudItemNotFound = errors.New("cloud item not found")
	// ErrCloudRevisionNotFound 修订不存在（可能已被裁掉）。
	ErrCloudRevisionNotFound = errors.New("cloud revision not found")
	// ErrCloudFrozen 该用户的云存储已被冻结。
	ErrCloudFrozen = errors.New("cloud storage frozen")
	// ErrCloudItemLimit 条目数已达上限。
	ErrCloudItemLimit = errors.New("cloud item limit reached")
	// ErrCloudNotInTrash 条目不在回收站里，无需恢复。
	ErrCloudNotInTrash = errors.New("cloud item not in trash")
)

// CloudConflictError ifRevision 与服务端当前修订不一致。
type CloudConflictError struct {
	// Current 服务端当前修订；条目不存在或在回收站里时为 0。
	Current int64
	Exists  bool
}

func (e *CloudConflictError) Error() string {
	return fmt.Sprintf("cloud revision conflict: current=%d", e.Current)
}

// CloudQuotaError 写入后会超出配额。
type CloudQuotaError struct {
	Used  int64
	Quota int64
	Need  int64
}

func (e *CloudQuotaError) Error() string {
	return fmt.Sprintf("cloud quota exceeded: used=%d need=%d quota=%d", e.Used, e.Need, e.Quota)
}

// CloudWritePlan 一次写入在事务里需要的全部事实（对象已经传到存储桶里了）。
type CloudWritePlan struct {
	AppID        int64
	UserID       int64
	Namespace    string
	Key          string
	IfRevision   *int64
	ContentType  string
	Encoding     string
	Size         int64
	SHA256       string
	Metadata     map[string]any
	DeviceID     string
	Source       string
	RestoredFrom *int64
	Operator     string

	StorageConfigID int64
	ObjectKey       string

	// 应用默认配额（用户行上的覆盖值优先）与另两项限制。
	QuotaBytes   int64
	MaxItems     int
	MaxRevisions int
	// BypassFrozen 管理端代写不受冻结约束。
	BypassFrozen bool
}

// CloudWriteResult 写入结果与需要在提交后删除的对象。
type CloudWriteResult struct {
	Item   *cloudstorage.Item
	Pruned []cloudstorage.StoredBlob
}

// cloudQuerier 连接池与事务的公共子集。
type cloudQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ── 配置 ──

const cloudConfigColumns = `appid, enabled, storage_config_name, quota_bytes, max_item_bytes, max_items,
max_revisions, trash_retention_days, restrict_namespaces, namespaces, updated_by, updated_at`

func scanCloudConfig(row pgx.Row) (*cloudstorage.Config, error) {
	var cfg cloudstorage.Config
	var namespaces []byte
	var updatedBy *string
	var updatedAt time.Time
	if err := row.Scan(&cfg.AppID, &cfg.Enabled, &cfg.StorageConfigName, &cfg.QuotaBytes, &cfg.MaxItemBytes,
		&cfg.MaxItems, &cfg.MaxRevisions, &cfg.TrashRetentionDays, &cfg.RestrictNamespaces, &namespaces,
		&updatedBy, &updatedAt); err != nil {
		return nil, err
	}
	if len(namespaces) > 0 {
		if err := json.Unmarshal(namespaces, &cfg.Namespaces); err != nil {
			return nil, fmt.Errorf("decode cloud namespaces: %w", err)
		}
	}
	if cfg.Namespaces == nil {
		cfg.Namespaces = []cloudstorage.Namespace{}
	}
	cfg.UpdatedBy = derefString(updatedBy)
	cfg.UpdatedAt = &updatedAt
	cfg.Configured = true
	return &cfg, nil
}

// GetCloudStorageConfig 读取应用配置；没配过时返回 (nil, nil)。
func (r *Repository) GetCloudStorageConfig(ctx context.Context, appID int64) (*cloudstorage.Config, error) {
	cfg, err := scanCloudConfig(r.pool.QueryRow(ctx, `SELECT `+cloudConfigColumns+`
FROM app_cloud_storage_configs WHERE appid = $1`, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return cfg, err
}

// SaveCloudStorageConfig 整体覆盖保存。
func (r *Repository) SaveCloudStorageConfig(ctx context.Context, cfg cloudstorage.Config) (*cloudstorage.Config, error) {
	namespaces := cfg.Namespaces
	if namespaces == nil {
		namespaces = []cloudstorage.Namespace{}
	}
	payload, err := json.Marshal(namespaces)
	if err != nil {
		return nil, err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO app_cloud_storage_configs
(appid, enabled, storage_config_name, quota_bytes, max_item_bytes, max_items, max_revisions,
 trash_retention_days, restrict_namespaces, namespaces, updated_by, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
ON CONFLICT (appid) DO UPDATE SET
  enabled = EXCLUDED.enabled,
  storage_config_name = EXCLUDED.storage_config_name,
  quota_bytes = EXCLUDED.quota_bytes,
  max_item_bytes = EXCLUDED.max_item_bytes,
  max_items = EXCLUDED.max_items,
  max_revisions = EXCLUDED.max_revisions,
  trash_retention_days = EXCLUDED.trash_retention_days,
  restrict_namespaces = EXCLUDED.restrict_namespaces,
  namespaces = EXCLUDED.namespaces,
  updated_by = EXCLUDED.updated_by,
  updated_at = NOW()`,
		cfg.AppID, cfg.Enabled, cfg.StorageConfigName, cfg.QuotaBytes, cfg.MaxItemBytes, cfg.MaxItems,
		cfg.MaxRevisions, cfg.TrashRetentionDays, cfg.RestrictNamespaces, payload, nullableString(cfg.UpdatedBy))
	if err != nil {
		return nil, err
	}
	return r.GetCloudStorageConfig(ctx, cfg.AppID)
}

// ListCloudStorageConfigs 全部应用的配置（后台清理回收站用）。
func (r *Repository) ListCloudStorageConfigs(ctx context.Context) ([]cloudstorage.Config, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cloudConfigColumns+` FROM app_cloud_storage_configs ORDER BY appid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cloudstorage.Config{}
	for rows.Next() {
		cfg, err := scanCloudConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *cfg)
	}
	return items, rows.Err()
}

// ── 用户账目 ──

const cloudUserColumns = `s.appid, s.user_id, COALESCE(u.account, ''), COALESCE(p.nickname, ''), s.quota_bytes,
s.frozen, s.frozen_reason, s.note, s.used_bytes, s.item_count, s.trash_count, s.revision_count,
s.last_write_at, s.updated_by, s.created_at, s.updated_at`

const cloudUserFrom = `app_cloud_storage_users s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN user_profiles p ON p.user_id = s.user_id`

func scanCloudUser(row pgx.Row) (*cloudstorage.UserState, error) {
	var state cloudstorage.UserState
	var updatedBy *string
	var createdAt, updatedAt time.Time
	if err := row.Scan(&state.AppID, &state.UserID, &state.Account, &state.Nickname, &state.QuotaOverride,
		&state.Frozen, &state.FrozenReason, &state.Note, &state.UsedBytes, &state.ItemCount, &state.TrashCount,
		&state.RevisionCount, &state.LastWriteAt, &updatedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	state.UpdatedBy = derefString(updatedBy)
	state.CreatedAt = &createdAt
	state.UpdatedAt = &updatedAt
	return &state, nil
}

// GetCloudUserState 读取用户账目；从没用过云存储的用户返回 (nil, nil)。
func (r *Repository) GetCloudUserState(ctx context.Context, appID, userID int64) (*cloudstorage.UserState, error) {
	state, err := scanCloudUser(r.pool.QueryRow(ctx, `SELECT `+cloudUserColumns+` FROM `+cloudUserFrom+`
WHERE s.appid = $1 AND s.user_id = $2`, appID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return state, err
}

// CloudUserBelongsToApp 用户是否属于该应用（管理端按路径上的 userId 操作前必须核对）。
func (r *Repository) CloudUserBelongsToApp(ctx context.Context, appID, userID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND appid = $2)`, userID, appID).Scan(&exists)
	return exists, err
}

// SaveCloudUserOverrides 保存配额覆盖、冻结与备注。
func (r *Repository) SaveCloudUserOverrides(ctx context.Context, appID, userID int64, quota *int64, frozen bool, reason, note, operator string) (*cloudstorage.UserState, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO app_cloud_storage_users
(appid, user_id, quota_bytes, frozen, frozen_reason, note, updated_by, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
ON CONFLICT (appid, user_id) DO UPDATE SET
  quota_bytes = EXCLUDED.quota_bytes,
  frozen = EXCLUDED.frozen,
  frozen_reason = EXCLUDED.frozen_reason,
  note = EXCLUDED.note,
  updated_by = EXCLUDED.updated_by,
  updated_at = NOW()`, appID, userID, quota, frozen, reason, note, nullableString(operator))
	if err != nil {
		return nil, err
	}
	return r.GetCloudUserState(ctx, appID, userID)
}

// ListCloudUsers 管理端用户列表：只列出在云存储里留下过账目的用户。
func (r *Repository) ListCloudUsers(ctx context.Context, query cloudstorage.UserQuery) (*cloudstorage.UserPage, error) {
	where := []string{"s.appid = $1"}
	args := []any{query.AppID}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		idx := len(args)
		clause := fmt.Sprintf("(u.account ILIKE $%d OR p.nickname ILIKE $%d", idx, idx)
		if id, err := strconv.ParseInt(keyword, 10, 64); err == nil && id > 0 {
			args = append(args, id)
			clause += fmt.Sprintf(" OR s.user_id = $%d", len(args))
		}
		where = append(where, clause+")")
	}
	if query.Frozen != nil {
		args = append(args, *query.Frozen)
		where = append(where, fmt.Sprintf("s.frozen = $%d", len(args)))
	}
	filter := strings.Join(where, " AND ")

	page := &cloudstorage.UserPage{Items: []cloudstorage.UserState{}, Page: query.Page, Limit: query.Limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+cloudUserFrom+` WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return nil, err
	}

	order := "s.used_bytes DESC, s.user_id DESC"
	switch query.Sort {
	case "items":
		order = "s.item_count DESC, s.user_id DESC"
	case "recent":
		order = "s.last_write_at DESC NULLS LAST, s.user_id DESC"
	}
	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+cloudUserColumns+` FROM `+cloudUserFrom+` WHERE `+filter+
		` ORDER BY `+order+fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		state, err := scanCloudUser(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *state)
	}
	return page, rows.Err()
}

// ── 条目与修订（读） ──

const cloudItemColumns = `i.id, i.appid, i.user_id, i.namespace, i.item_key, i.revision, i.content_type, i.encoding,
i.size, i.sha256, i.metadata, i.device_id, i.deleted_at, i.created_at, i.updated_at,
(SELECT COUNT(*) FROM app_cloud_item_revisions r WHERE r.item_id = i.id),
(SELECT COALESCE(SUM(r.size), 0) FROM app_cloud_item_revisions r WHERE r.item_id = i.id)`

func scanCloudItem(row pgx.Row) (*cloudstorage.Item, error) {
	var item cloudstorage.Item
	var metadata []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Namespace, &item.Key, &item.Revision,
		&item.ContentType, &item.Encoding, &item.Size, &item.SHA256, &metadata, &item.DeviceID, &item.DeletedAt,
		&item.CreatedAt, &item.UpdatedAt, &item.RevisionCount, &item.StoredBytes); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCloudItemNotFound
		}
		return nil, err
	}
	item.Metadata = decodeCloudMetadata(metadata)
	item.Deleted = item.DeletedAt != nil
	return &item, nil
}

func decodeCloudMetadata(raw []byte) map[string]any {
	metadata := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &metadata)
	}
	return metadata
}

func encodeCloudMetadata(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	return json.Marshal(metadata)
}

func getCloudItem(ctx context.Context, q cloudQuerier, appID, userID int64, itemID int64) (*cloudstorage.Item, error) {
	return scanCloudItem(q.QueryRow(ctx, `SELECT `+cloudItemColumns+` FROM app_cloud_items i
WHERE i.appid = $1 AND i.user_id = $2 AND i.id = $3`, appID, userID, itemID))
}

// GetCloudItem 按（命名空间, 键）取条目，含回收站里的。
func (r *Repository) GetCloudItem(ctx context.Context, appID, userID int64, namespace, key string) (*cloudstorage.Item, error) {
	return scanCloudItem(r.pool.QueryRow(ctx, `SELECT `+cloudItemColumns+` FROM app_cloud_items i
WHERE i.appid = $1 AND i.user_id = $2 AND i.namespace = $3 AND i.item_key = $4`, appID, userID, namespace, key))
}

// GetCloudItemByID 按 ID 取条目（限定在该用户名下）。
func (r *Repository) GetCloudItemByID(ctx context.Context, appID, userID, itemID int64) (*cloudstorage.Item, error) {
	return getCloudItem(ctx, r.pool, appID, userID, itemID)
}

// ListCloudItems 条目分页。
func (r *Repository) ListCloudItems(ctx context.Context, query cloudstorage.ItemQuery) (*cloudstorage.ItemPage, error) {
	where := []string{"i.appid = $1", "i.user_id = $2"}
	args := []any{query.AppID, query.UserID}
	if query.Namespace != "" {
		args = append(args, query.Namespace)
		where = append(where, fmt.Sprintf("i.namespace = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		where = append(where, fmt.Sprintf("(i.item_key ILIKE $%d OR i.namespace ILIKE $%d)", len(args), len(args)))
	}
	switch query.Status {
	case cloudstorage.StatusDeleted:
		where = append(where, "i.deleted_at IS NOT NULL")
	case cloudstorage.StatusAll:
	default:
		where = append(where, "i.deleted_at IS NULL")
	}
	filter := strings.Join(where, " AND ")

	page := &cloudstorage.ItemPage{Items: []cloudstorage.Item{}, Page: query.Page, Limit: query.Limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_cloud_items i WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+cloudItemColumns+` FROM app_cloud_items i WHERE `+filter+
		fmt.Sprintf(` ORDER BY i.namespace, i.updated_at DESC, i.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanCloudItem(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *item)
	}
	return page, rows.Err()
}

const cloudRevisionColumns = `r.id, r.item_id, r.revision, r.content_type, r.encoding, r.size, r.sha256, r.metadata,
r.device_id, r.source, r.restored_from, r.operator, r.created_at, r.storage_config_id, r.object_key,
r.revision = i.revision`

func scanCloudRevision(row pgx.Row) (*cloudstorage.Revision, error) {
	var rev cloudstorage.Revision
	var metadata []byte
	if err := row.Scan(&rev.ID, &rev.ItemID, &rev.Revision, &rev.ContentType, &rev.Encoding, &rev.Size, &rev.SHA256,
		&metadata, &rev.DeviceID, &rev.Source, &rev.RestoredFrom, &rev.Operator, &rev.CreatedAt,
		&rev.StorageConfigID, &rev.ObjectKey, &rev.Current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCloudRevisionNotFound
		}
		return nil, err
	}
	rev.Metadata = decodeCloudMetadata(metadata)
	return &rev, nil
}

// GetCloudRevision 取一个修订；revision <= 0 表示当前修订。
func (r *Repository) GetCloudRevision(ctx context.Context, itemID, revision int64) (*cloudstorage.Revision, error) {
	if revision <= 0 {
		return scanCloudRevision(r.pool.QueryRow(ctx, `SELECT `+cloudRevisionColumns+`
FROM app_cloud_item_revisions r JOIN app_cloud_items i ON i.id = r.item_id
WHERE r.item_id = $1 AND r.revision = i.revision`, itemID))
	}
	return scanCloudRevision(r.pool.QueryRow(ctx, `SELECT `+cloudRevisionColumns+`
FROM app_cloud_item_revisions r JOIN app_cloud_items i ON i.id = r.item_id
WHERE r.item_id = $1 AND r.revision = $2`, itemID, revision))
}

// ListCloudRevisions 条目的全部留存修订，新的在前。
func (r *Repository) ListCloudRevisions(ctx context.Context, itemID int64) ([]cloudstorage.Revision, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cloudRevisionColumns+`
FROM app_cloud_item_revisions r JOIN app_cloud_items i ON i.id = r.item_id
WHERE r.item_id = $1 ORDER BY r.revision DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cloudstorage.Revision{}
	for rows.Next() {
		rev, err := scanCloudRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *rev)
	}
	return items, rows.Err()
}

// CloudNamespaceUsage 各命名空间的用量；userID 为 0 时统计整个应用。
func (r *Repository) CloudNamespaceUsage(ctx context.Context, appID, userID int64) ([]cloudstorage.NamespaceUsage, error) {
	filter := "i.appid = $1"
	args := []any{appID}
	if userID > 0 {
		filter += " AND i.user_id = $2"
		args = append(args, userID)
	}
	rows, err := r.pool.Query(ctx, `SELECT i.namespace,
  COUNT(*) FILTER (WHERE i.deleted_at IS NULL),
  COUNT(*) FILTER (WHERE i.deleted_at IS NOT NULL),
  COALESCE(SUM((SELECT COALESCE(SUM(r.size), 0) FROM app_cloud_item_revisions r WHERE r.item_id = i.id)), 0)
FROM app_cloud_items i WHERE `+filter+`
GROUP BY i.namespace ORDER BY 4 DESC, i.namespace`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cloudstorage.NamespaceUsage{}
	for rows.Next() {
		var usage cloudstorage.NamespaceUsage
		if err := rows.Scan(&usage.Namespace, &usage.ItemCount, &usage.TrashCount, &usage.StoredBytes); err != nil {
			return nil, err
		}
		items = append(items, usage)
	}
	return items, rows.Err()
}

// ── 写入 ──

// lockCloudUser 建好（如需）并锁住用户行，返回配额覆盖值与冻结状态。
func lockCloudUser(ctx context.Context, tx pgx.Tx, appID, userID int64) (*int64, bool, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO app_cloud_storage_users (appid, user_id) VALUES ($1, $2)
ON CONFLICT (appid, user_id) DO NOTHING`, appID, userID); err != nil {
		return nil, false, err
	}
	var quota *int64
	var frozen bool
	err := tx.QueryRow(ctx, `SELECT quota_bytes, frozen FROM app_cloud_storage_users
WHERE appid = $1 AND user_id = $2 FOR UPDATE`, appID, userID).Scan(&quota, &frozen)
	return quota, frozen, err
}

// refreshCloudUsage 从源表重算用户账目。touch 为真时顺带记一次写入时间。
func refreshCloudUsage(ctx context.Context, tx pgx.Tx, appID, userID int64, touch bool) error {
	_, err := tx.Exec(ctx, `UPDATE app_cloud_storage_users s SET
  used_bytes = COALESCE((SELECT SUM(r.size) FROM app_cloud_item_revisions r WHERE r.appid = s.appid AND r.user_id = s.user_id), 0),
  revision_count = (SELECT COUNT(*) FROM app_cloud_item_revisions r WHERE r.appid = s.appid AND r.user_id = s.user_id),
  item_count = (SELECT COUNT(*) FROM app_cloud_items i WHERE i.appid = s.appid AND i.user_id = s.user_id AND i.deleted_at IS NULL),
  trash_count = (SELECT COUNT(*) FROM app_cloud_items i WHERE i.appid = s.appid AND i.user_id = s.user_id AND i.deleted_at IS NOT NULL),
  last_write_at = CASE WHEN $3 THEN NOW() ELSE s.last_write_at END,
  updated_at = NOW()
WHERE s.appid = $1 AND s.user_id = $2`, appID, userID, touch)
	return err
}

// CommitCloudWrite 在一个事务里落下一次写入：并发校验、条目数与配额校验、新修订、
// 裁掉超出保留数的旧修订、重算账目。对象已在存储桶里；失败时由调用方删掉它。
func (r *Repository) CommitCloudWrite(ctx context.Context, plan CloudWritePlan) (result *CloudWriteResult, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	quotaOverride, frozen, err := lockCloudUser(ctx, tx, plan.AppID, plan.UserID)
	if err != nil {
		return nil, err
	}
	if frozen && !plan.BypassFrozen {
		return nil, ErrCloudFrozen
	}
	quota := plan.QuotaBytes
	if quotaOverride != nil && *quotaOverride > 0 {
		quota = *quotaOverride
	}

	var itemID, revision int64
	var deletedAt *time.Time
	exists := true
	err = tx.QueryRow(ctx, `SELECT id, revision, deleted_at FROM app_cloud_items
WHERE appid = $1 AND user_id = $2 AND namespace = $3 AND item_key = $4 FOR UPDATE`,
		plan.AppID, plan.UserID, plan.Namespace, plan.Key).Scan(&itemID, &revision, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, err = false, nil
	}
	if err != nil {
		return nil, err
	}
	live := exists && deletedAt == nil

	if plan.IfRevision != nil {
		current := int64(0)
		if live {
			current = revision
		}
		if *plan.IfRevision != current {
			return nil, &CloudConflictError{Current: current, Exists: live}
		}
	}

	if !live {
		var active int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM app_cloud_items
WHERE appid = $1 AND user_id = $2 AND deleted_at IS NULL`, plan.AppID, plan.UserID).Scan(&active); err != nil {
			return nil, err
		}
		if active+1 > plan.MaxItems {
			return nil, ErrCloudItemLimit
		}
	}

	var used int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(size), 0) FROM app_cloud_item_revisions
WHERE appid = $1 AND user_id = $2`, plan.AppID, plan.UserID).Scan(&used); err != nil {
		return nil, err
	}

	// 新修订进来之后只留 maxRevisions 个：现有修订里第 maxRevisions-1 个之后的都要裁掉。
	var pruneIDs []int64
	var pruned []cloudstorage.StoredBlob
	var prunedBytes int64
	if exists {
		keep := plan.MaxRevisions - 1
		if keep < 0 {
			keep = 0
		}
		rows, qerr := tx.Query(ctx, `SELECT id, size, storage_config_id, object_key FROM app_cloud_item_revisions
WHERE item_id = $1 ORDER BY revision DESC OFFSET $2`, itemID, keep)
		if qerr != nil {
			return nil, qerr
		}
		for rows.Next() {
			var id, size int64
			var blob cloudstorage.StoredBlob
			if err = rows.Scan(&id, &size, &blob.StorageConfigID, &blob.ObjectKey); err != nil {
				rows.Close()
				return nil, err
			}
			pruneIDs = append(pruneIDs, id)
			pruned = append(pruned, blob)
			prunedBytes += size
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}

	if used-prunedBytes+plan.Size > quota {
		return nil, &CloudQuotaError{Used: used, Quota: quota, Need: plan.Size}
	}

	metadata, err := encodeCloudMetadata(plan.Metadata)
	if err != nil {
		return nil, err
	}
	nextRevision := revision + 1
	if !exists {
		nextRevision = 1
		err = tx.QueryRow(ctx, `INSERT INTO app_cloud_items
(appid, user_id, namespace, item_key, revision, content_type, encoding, size, sha256, metadata, device_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
			plan.AppID, plan.UserID, plan.Namespace, plan.Key, nextRevision, plan.ContentType, plan.Encoding,
			plan.Size, plan.SHA256, metadata, plan.DeviceID).Scan(&itemID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE app_cloud_items SET revision = $2, content_type = $3, encoding = $4, size = $5,
sha256 = $6, metadata = $7, device_id = $8, deleted_at = NULL, updated_at = NOW() WHERE id = $1`,
			itemID, nextRevision, plan.ContentType, plan.Encoding, plan.Size, plan.SHA256, metadata, plan.DeviceID)
	}
	if err != nil {
		return nil, err
	}

	if _, err = tx.Exec(ctx, `INSERT INTO app_cloud_item_revisions
(item_id, appid, user_id, revision, storage_config_id, object_key, content_type, encoding, size, sha256,
 metadata, device_id, source, restored_from, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		itemID, plan.AppID, plan.UserID, nextRevision, plan.StorageConfigID, plan.ObjectKey, plan.ContentType,
		plan.Encoding, plan.Size, plan.SHA256, metadata, plan.DeviceID, plan.Source, plan.RestoredFrom,
		plan.Operator); err != nil {
		return nil, err
	}
	if len(pruneIDs) > 0 {
		if _, err = tx.Exec(ctx, `DELETE FROM app_cloud_item_revisions WHERE id = ANY($1)`, pruneIDs); err != nil {
			return nil, err
		}
	}
	if err = refreshCloudUsage(ctx, tx, plan.AppID, plan.UserID, true); err != nil {
		return nil, err
	}
	item, err := getCloudItem(ctx, tx, plan.AppID, plan.UserID, itemID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &CloudWriteResult{Item: item, Pruned: pruned}, nil
}

// TrashCloudItem 把条目移入回收站。已在回收站里的原样返回（幂等）。
func (r *Repository) TrashCloudItem(ctx context.Context, appID, userID, itemID int64, ifRevision *int64, bypassFrozen bool) (item *cloudstorage.Item, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	_, frozen, err := lockCloudUser(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	if frozen && !bypassFrozen {
		return nil, ErrCloudFrozen
	}
	var revision int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT revision, deleted_at FROM app_cloud_items
WHERE appid = $1 AND user_id = $2 AND id = $3 FOR UPDATE`, appID, userID, itemID).Scan(&revision, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCloudItemNotFound
	}
	if err != nil {
		return nil, err
	}
	if deletedAt == nil {
		if ifRevision != nil && *ifRevision != revision {
			return nil, &CloudConflictError{Current: revision, Exists: true}
		}
		if _, err = tx.Exec(ctx, `UPDATE app_cloud_items SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1`, itemID); err != nil {
			return nil, err
		}
		if err = refreshCloudUsage(ctx, tx, appID, userID, true); err != nil {
			return nil, err
		}
	}
	item, err = getCloudItem(ctx, tx, appID, userID, itemID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// RestoreCloudItem 把条目从回收站恢复。
func (r *Repository) RestoreCloudItem(ctx context.Context, appID, userID, itemID int64, maxItems int, bypassFrozen bool) (item *cloudstorage.Item, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	_, frozen, err := lockCloudUser(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	if frozen && !bypassFrozen {
		return nil, ErrCloudFrozen
	}
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT deleted_at FROM app_cloud_items
WHERE appid = $1 AND user_id = $2 AND id = $3 FOR UPDATE`, appID, userID, itemID).Scan(&deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCloudItemNotFound
	}
	if err != nil {
		return nil, err
	}
	if deletedAt == nil {
		return nil, ErrCloudNotInTrash
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM app_cloud_items
WHERE appid = $1 AND user_id = $2 AND deleted_at IS NULL`, appID, userID).Scan(&active); err != nil {
		return nil, err
	}
	if active+1 > maxItems {
		return nil, ErrCloudItemLimit
	}
	if _, err = tx.Exec(ctx, `UPDATE app_cloud_items SET deleted_at = NULL, updated_at = NOW() WHERE id = $1`, itemID); err != nil {
		return nil, err
	}
	if err = refreshCloudUsage(ctx, tx, appID, userID, true); err != nil {
		return nil, err
	}
	item, err = getCloudItem(ctx, tx, appID, userID, itemID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// PurgeCloudItem 永久删除条目及其全部修订，交回需要从存储桶删掉的对象。
//
// deletedBefore 非空时只在条目仍在回收站且删除时间早于它时才动手 —— 后台清理
// 先挑候选、再逐条进事务，挑完到动手之间用户可能已经把它恢复了。
func (r *Repository) PurgeCloudItem(ctx context.Context, appID, userID, itemID int64, deletedBefore *time.Time, bypassFrozen bool) (blobs []cloudstorage.StoredBlob, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	_, frozen, err := lockCloudUser(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	if frozen && !bypassFrozen {
		return nil, ErrCloudFrozen
	}
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT deleted_at FROM app_cloud_items
WHERE appid = $1 AND user_id = $2 AND id = $3 FOR UPDATE`, appID, userID, itemID).Scan(&deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCloudItemNotFound
	}
	if err != nil {
		return nil, err
	}
	if deletedBefore != nil && (deletedAt == nil || !deletedAt.Before(*deletedBefore)) {
		_ = tx.Rollback(ctx)
		return nil, nil
	}
	blobs, err = collectCloudBlobs(ctx, tx, `SELECT storage_config_id, object_key FROM app_cloud_item_revisions WHERE item_id = $1`, itemID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM app_cloud_items WHERE id = $1`, itemID); err != nil {
		return nil, err
	}
	if err = refreshCloudUsage(ctx, tx, appID, userID, false); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return blobs, nil
}

// PurgeCloudUser 清空一个用户的全部云存储数据（账目行保留，配额覆盖与冻结不受影响）。
func (r *Repository) PurgeCloudUser(ctx context.Context, appID, userID int64) (blobs []cloudstorage.StoredBlob, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, _, err = lockCloudUser(ctx, tx, appID, userID); err != nil {
		return nil, err
	}
	blobs, err = collectCloudBlobs(ctx, tx, `SELECT storage_config_id, object_key FROM app_cloud_item_revisions
WHERE appid = $1 AND user_id = $2`, appID, userID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM app_cloud_items WHERE appid = $1 AND user_id = $2`, appID, userID); err != nil {
		return nil, err
	}
	if err = refreshCloudUsage(ctx, tx, appID, userID, false); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return blobs, nil
}

func collectCloudBlobs(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]cloudstorage.StoredBlob, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blobs := []cloudstorage.StoredBlob{}
	for rows.Next() {
		var blob cloudstorage.StoredBlob
		if err := rows.Scan(&blob.StorageConfigID, &blob.ObjectKey); err != nil {
			return nil, err
		}
		blobs = append(blobs, blob)
	}
	return blobs, rows.Err()
}

// CloudTrashCandidate 回收站里过了保留期的条目。
type CloudTrashCandidate struct {
	UserID int64
	ItemID int64
}

// ListExpiredCloudTrash 挑出删除时间早于 before 的回收站条目（不加锁，逐条清除时再确认）。
func (r *Repository) ListExpiredCloudTrash(ctx context.Context, appID int64, before time.Time, limit int) ([]CloudTrashCandidate, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id, id FROM app_cloud_items
WHERE appid = $1 AND deleted_at IS NOT NULL AND deleted_at < $2
ORDER BY deleted_at LIMIT $3`, appID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CloudTrashCandidate{}
	for rows.Next() {
		var item CloudTrashCandidate
		if err := rows.Scan(&item.UserID, &item.ItemID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ── 概览 ──

// CloudStorageStats 应用级概览：总量、今日写入、趋势与用量最高的用户。
func (r *Repository) CloudStorageStats(ctx context.Context, appID int64, since, todayStart time.Time, timezone string, top int) (*cloudstorage.Stats, error) {
	stats := &cloudstorage.Stats{
		Namespaces: []cloudstorage.NamespaceUsage{},
		TopUsers:   []cloudstorage.UserState{},
		Trend:      []cloudstorage.DailyWrites{},
	}
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE revision_count > 0 OR trash_count > 0),
  COUNT(*) FILTER (WHERE frozen),
  COALESCE(SUM(used_bytes), 0),
  COALESCE(SUM(item_count), 0),
  COALESCE(SUM(trash_count), 0),
  COALESCE(SUM(revision_count), 0)
FROM app_cloud_storage_users WHERE appid = $1`, appID).Scan(&stats.Users, &stats.FrozenUsers, &stats.UsedBytes,
		&stats.Items, &stats.TrashItems, &stats.Revisions); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_cloud_item_revisions
WHERE appid = $1 AND created_at >= $2`, appID, todayStart).Scan(&stats.WritesToday); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
  COUNT(*), COALESCE(SUM(size), 0), COUNT(DISTINCT user_id)
FROM app_cloud_item_revisions
WHERE appid = $1 AND created_at >= $2
GROUP BY day ORDER BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day cloudstorage.DailyWrites
		if err := rows.Scan(&day.Day, &day.Writes, &day.Bytes, &day.Users); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	namespaces, err := r.CloudNamespaceUsage(ctx, appID, 0)
	if err != nil {
		return nil, err
	}
	stats.Namespaces = namespaces

	page, err := r.ListCloudUsers(ctx, cloudstorage.UserQuery{AppID: appID, Page: 1, Limit: top})
	if err != nil {
		return nil, err
	}
	for _, user := range page.Items {
		if user.UsedBytes > 0 {
			stats.TopUsers = append(stats.TopUsers, user)
		}
	}
	return stats, nil
}

// DeleteStorageObjectIndex 移除存储中心里的对象索引行（对象本身由调用方先删掉）。
//
// 云存储的每个修订都经 StorageService 上传，因此在 storage_objects 里各有一行索引；
// 修订被清除之后那一行若不跟着走，存储中心的文件列表与用量统计就会一直算着它。
func (r *Repository) DeleteStorageObjectIndex(ctx context.Context, configID int64, objectKey string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM storage_objects WHERE config_id = $1 AND object_key = $2`, configID, objectKey)
	return err
}
