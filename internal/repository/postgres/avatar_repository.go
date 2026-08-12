package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	avatardomain "aegis/internal/domain/avatar"

	"github.com/jackc/pgx/v5"
)

const avatarAssetColumns = `id, owner_type, owner_app_id, owner_id, config_id, base_key, content_type,
width, height, size_bytes, checksum, blurhash, dominant_color, animated, COALESCE(variants,'[]'::jsonb),
file_name, source, status, created_at, replaced_at`

// ReplaceAvatarAsset 落一条新的 active 资产，并把该主体原来的那条置为 replaced。
//
// 两步在**同一个事务**里：分开做的话，中间失败会留下零条 active（头像凭空消失）
// 或两条 active（唯一索引直接报错，上传当场失败）。顺序也不能反 ——
// 先插后置换会撞上 uq_avatar_assets_active。
func (r *Repository) ReplaceAvatarAsset(ctx context.Context, asset avatardomain.Asset) (*avatardomain.Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE avatar_assets SET status = 'replaced', replaced_at = NOW()
WHERE owner_type = $1 AND owner_app_id = $2 AND owner_id = $3 AND status = 'active'`,
		asset.Owner.Type, asset.Owner.AppID, asset.Owner.ID); err != nil {
		return nil, err
	}

	variants, err := json.Marshal(normalizeAvatarVariants(asset.Variants))
	if err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `INSERT INTO avatar_assets (
    owner_type, owner_app_id, owner_id, config_id, base_key, content_type,
    width, height, size_bytes, checksum, blurhash, dominant_color, animated,
    variants, file_name, source, status, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'active',NOW())
RETURNING `+avatarAssetColumns,
		asset.Owner.Type, asset.Owner.AppID, asset.Owner.ID, asset.ConfigID, asset.BaseKey, asset.ContentType,
		asset.Width, asset.Height, asset.Bytes, asset.Checksum, asset.Blurhash, asset.DominantColor, asset.Animated,
		variants, asset.FileName, avatarAssetSource(asset.Source))
	saved, err := scanAvatarAsset(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

// GetActiveAvatarAsset 主体当前生效的头像资产，没有返回 (nil, nil)。
func (r *Repository) GetActiveAvatarAsset(ctx context.Context, owner avatardomain.Owner) (*avatardomain.Asset, error) {
	return scanAvatarAsset(r.pool.QueryRow(ctx, `SELECT `+avatarAssetColumns+
		` FROM avatar_assets WHERE owner_type = $1 AND owner_app_id = $2 AND owner_id = $3 AND status = 'active' LIMIT 1`,
		owner.Type, owner.AppID, owner.ID))
}

// GetAvatarAssetByKey 按 (存储配置, 对象键) 反查资产。
//
// 自愈链路用它：库里那一列还留着 storage:// 引用、但元数据（变体 / blurhash）
// 需要从这里补回来。base_key 之外还认变体键，因为存量引用可能指向某个尺寸档。
func (r *Repository) GetAvatarAssetByKey(ctx context.Context, configID int64, objectKey string) (*avatardomain.Asset, error) {
	return scanAvatarAsset(r.pool.QueryRow(ctx, `SELECT `+avatarAssetColumns+
		` FROM avatar_assets
WHERE config_id = $1 AND (base_key = $2 OR variants @> jsonb_build_array(jsonb_build_object('key', $2::text)))
ORDER BY (status = 'active') DESC, created_at DESC LIMIT 1`, configID, objectKey))
}

// ListAvatarAssetHistory 主体的头像历史（含当前），最近的在前。
func (r *Repository) ListAvatarAssetHistory(ctx context.Context, owner avatardomain.Owner, limit int) ([]avatardomain.Asset, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx, `SELECT `+avatarAssetColumns+
		` FROM avatar_assets WHERE owner_type = $1 AND owner_app_id = $2 AND owner_id = $3 AND status <> 'deleted'
ORDER BY (status = 'active') DESC, created_at DESC LIMIT $4`,
		owner.Type, owner.AppID, owner.ID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]avatardomain.Asset, 0, limit)
	for rows.Next() {
		item, err := scanAvatarAsset(rows)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, rows.Err()
}

// GetAvatarAssetByID 按主键取一条，并校验归属。
// 归属校验放在 SQL 里而不是取出来再比：少一次「取到了但忘了比」的机会。
func (r *Repository) GetAvatarAssetByID(ctx context.Context, owner avatardomain.Owner, id int64) (*avatardomain.Asset, error) {
	return scanAvatarAsset(r.pool.QueryRow(ctx, `SELECT `+avatarAssetColumns+
		` FROM avatar_assets WHERE id = $1 AND owner_type = $2 AND owner_app_id = $3 AND owner_id = $4 LIMIT 1`,
		id, owner.Type, owner.AppID, owner.ID))
}

// ActivateAvatarAsset 把历史里的某一条重新置为 active（恢复上一张头像）。
func (r *Repository) ActivateAvatarAsset(ctx context.Context, owner avatardomain.Owner, id int64) (*avatardomain.Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE avatar_assets SET status = 'replaced', replaced_at = NOW()
WHERE owner_type = $1 AND owner_app_id = $2 AND owner_id = $3 AND status = 'active' AND id <> $4`,
		owner.Type, owner.AppID, owner.ID, id); err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `UPDATE avatar_assets SET status = 'active', replaced_at = NULL
WHERE id = $1 AND owner_type = $2 AND owner_app_id = $3 AND owner_id = $4
RETURNING `+avatarAssetColumns, id, owner.Type, owner.AppID, owner.ID)
	saved, err := scanAvatarAsset(row)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

// ClearActiveAvatarAsset 主体主动移除头像。
//
// 置为 deleted 而不是删行：对象还留在存储里，留一条记录才能在
// 「我不小心点了移除」时找回来，也才能让清理任务知道该删哪些对象。
func (r *Repository) ClearActiveAvatarAsset(ctx context.Context, owner avatardomain.Owner) error {
	_, err := r.pool.Exec(ctx, `UPDATE avatar_assets SET status = 'deleted', replaced_at = NOW()
WHERE owner_type = $1 AND owner_app_id = $2 AND owner_id = $3 AND status = 'active'`,
		owner.Type, owner.AppID, owner.ID)
	return err
}

// FindLatestAvatarObjectByKeyPart 从**存储对象索引**里找该主体最近一次上传的头像。
//
// 这是自愈的第二条线索，专门为**本次改动之前**就已经被写坏的那些行准备的：
// 它们没有 avatar_assets 记录（那张表这次才有），但 storage_objects 里
// 一直记着每一次上传 —— 对象键形如 `avatars/apps/{appID}/users/{userID}/…`，
// 主体信息就编码在路径里。少了这一条，升级前丢头像的用户只能自己重新上传一次。
//
// 用 LIKE 包含而不是前缀匹配：存储配置的 root_path 会被拼在键的最前面，
// 按前缀匹配会漏掉所有配了 root_path 的部署。这条路径只在「库里那一列已经坏了」
// 时才走，全表扫一次的代价可以接受，而且成功后会落一条 avatar_assets，
// 同一个人不会扫第二次。
func (r *Repository) FindLatestAvatarObjectByKeyPart(ctx context.Context, keyPart string) (int64, string, error) {
	var (
		configID  int64
		objectKey string
	)
	err := r.pool.QueryRow(ctx, `SELECT config_id, object_key FROM storage_objects
WHERE status = 'active' AND deleted_at IS NULL
  AND object_key LIKE '%' || $1 || '%'
  AND COALESCE(metadata->>'module', '') = 'avatar'
ORDER BY created_at DESC LIMIT 1`, keyPart).Scan(&configID, &objectKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", nil
		}
		return 0, "", err
	}
	return configID, objectKey, nil
}

// SetUserProfileAvatar 只改 user_profiles.avatar 这一列。
//
// 自愈与「移除头像」都走它，刻意不复用 UpsertUserProfile：那条链路是
// 读整份档案 → 改一个字段 → 整份写回，中间隔着一次网络往返，
// 期间用户改了昵称就会被这次写回覆盖掉。何况自愈发生在**读**资料的路径上，
// 在那里做整份写回既慢又危险。空串是合法值（表示移除头像）。
func (r *Repository) SetUserProfileAvatar(ctx context.Context, userID int64, avatar string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE user_profiles SET avatar = $2, updated_at = NOW() WHERE user_id = $1`, userID, avatar)
	return err
}

// SetAdminAvatar 只改 admin_accounts.avatar 这一列。
//
// UpdateAdminProfile 里那一列包了一层 COALESCE + NULLIF —— 空串等于
// 「不修改」。那个语义对「编辑资料时不回显头像」是对的，但也意味着
// **没有任何入口能把头像清空**。这个方法就是那个入口。
func (r *Repository) SetAdminAvatar(ctx context.Context, adminID int64, avatar string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE admin_accounts SET avatar = $2, updated_at = NOW() WHERE id = $1`, adminID, avatar)
	return err
}

func avatarAssetSource(value string) string {
	switch value {
	case avatardomain.SourceImport, avatardomain.SourceMigrated:
		return value
	default:
		return avatardomain.SourceUpload
	}
}

// normalizeAvatarVariants 保证 variants 落库时恒为数组而不是 null，
// 否则 `variants @> ...` 在自愈查询里对 null 行直接判假。
func normalizeAvatarVariants(items []avatardomain.Variant) []avatardomain.Variant {
	if items == nil {
		return []avatardomain.Variant{}
	}
	return items
}

func scanAvatarAsset(row pgx.Row) (*avatardomain.Asset, error) {
	var (
		item        avatardomain.Asset
		variantsRaw []byte
		replacedAt  *time.Time
	)
	err := row.Scan(&item.ID, &item.Owner.Type, &item.Owner.AppID, &item.Owner.ID, &item.ConfigID,
		&item.BaseKey, &item.ContentType, &item.Width, &item.Height, &item.Bytes, &item.Checksum,
		&item.Blurhash, &item.DominantColor, &item.Animated, &variantsRaw, &item.FileName,
		&item.Source, &item.Status, &item.CreatedAt, &replacedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(variantsRaw) > 0 {
		_ = json.Unmarshal(variantsRaw, &item.Variants)
	}
	item.ReplacedAt = replacedAt
	return &item, nil
}
