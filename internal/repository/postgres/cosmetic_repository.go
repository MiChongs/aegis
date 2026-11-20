package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"aegis/internal/domain/cosmetic"
	vipdomain "aegis/internal/domain/vip"
)

// 个性装扮账本（docs/cosmetics.md）：装扮、素材、版本、持有、佩戴。规则见 internal/domain/cosmetic。

var (
	ErrCosmeticNotFound       = errors.New("cosmetic not found")
	ErrCosmeticCodeTaken      = errors.New("cosmetic code already used")
	ErrCosmeticNotPublished   = errors.New("cosmetic never published")
	ErrCosmeticHasOwners      = errors.New("cosmetic has ownership records")
	ErrCosmeticAssetNotFound  = errors.New("cosmetic asset not found")
	ErrCosmeticAssetInUse     = errors.New("cosmetic asset in use")
	ErrCosmeticOwnershipGone  = errors.New("cosmetic ownership not found")
	ErrCosmeticRevisionAbsent = errors.New("cosmetic revision not found")
	// ErrCosmeticUnavailable 发放时装扮不存在、未上架或未发布（整笔发放回滚）
	ErrCosmeticUnavailable = errors.New("cosmetic is not available for granting")
)

// ── 设置 ──

// GetCosmeticSettings 应用的装扮设置；没配置过返回默认值。
func (r *Repository) GetCosmeticSettings(ctx context.Context, appID int64) (cosmetic.Settings, error) {
	settings := cosmetic.DefaultSettings(appID)
	var updatedBy *string
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT enabled, badge_slots, updated_by, updated_at FROM cosmetic_settings WHERE appid = $1`,
		appID).Scan(&settings.Enabled, &settings.BadgeSlots, &updatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if updatedBy != nil {
		settings.UpdatedBy = *updatedBy
	}
	settings.UpdatedAt = &updatedAt
	return settings, nil
}

// SaveCosmeticSettings 保存应用设置。
func (r *Repository) SaveCosmeticSettings(ctx context.Context, settings cosmetic.Settings) (cosmetic.Settings, error) {
	if _, err := r.pool.Exec(ctx, `INSERT INTO cosmetic_settings (appid, enabled, badge_slots, updated_by, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (appid) DO UPDATE SET enabled = EXCLUDED.enabled, badge_slots = EXCLUDED.badge_slots,
    updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		settings.AppID, settings.Enabled, settings.BadgeSlots, nullableString(settings.UpdatedBy)); err != nil {
		return settings, err
	}
	// 勋章位调少了：超出的那几位直接清掉（否则「最多戴三枚」的设置在老用户身上不生效）
	if _, err := r.pool.Exec(ctx, `DELETE FROM user_cosmetic_equips WHERE appid = $1 AND slot = $2 AND position >= $3`,
		settings.AppID, cosmetic.SlotBadge, settings.BadgeSlots); err != nil {
		return settings, err
	}
	return r.GetCosmeticSettings(ctx, settings.AppID)
}

// ── 素材 ──

const cosmeticAssetColumns = `a.id, a.appid, a.name, a.kind, a.content_type, a.width, a.height, a.size_bytes, a.sha256,
a.reference, COALESCE(a.uploaded_by, ''), a.created_at`

func scanCosmeticAsset(row interface{ Scan(dest ...any) error }, extra ...any) (*cosmetic.Asset, error) {
	var a cosmetic.Asset
	dest := []any{&a.ID, &a.AppID, &a.Name, &a.Kind, &a.ContentType, &a.Width, &a.Height, &a.Size, &a.SHA256,
		&a.Reference, &a.UploadedBy, &a.CreatedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticAssetNotFound
		}
		return nil, err
	}
	return &a, nil
}

// FindCosmeticAssetBySHA 同一应用里内容相同的素材（重复上传直接复用）。
func (r *Repository) FindCosmeticAssetBySHA(ctx context.Context, appID int64, sha string) (*cosmetic.Asset, error) {
	return scanCosmeticAsset(r.pool.QueryRow(ctx, `SELECT `+cosmeticAssetColumns+` FROM cosmetic_assets a WHERE a.appid = $1 AND a.sha256 = $2`,
		appID, sha))
}

// InsertCosmeticAsset 记一条素材；并发重复上传时返回先到的那条。
func (r *Repository) InsertCosmeticAsset(ctx context.Context, a cosmetic.Asset) (*cosmetic.Asset, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO cosmetic_assets (appid, name, kind, content_type, width, height, size_bytes, sha256, reference, uploaded_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (appid, sha256) DO NOTHING`,
		a.AppID, truncateColumn(a.Name, 128), a.Kind, a.ContentType, a.Width, a.Height, a.Size, a.SHA256, a.Reference,
		nullableString(a.UploadedBy))
	if err != nil {
		return nil, err
	}
	return r.FindCosmeticAssetBySHA(ctx, a.AppID, a.SHA256)
}

// CosmeticAssetFilter 素材列表筛选。
type CosmeticAssetFilter struct {
	AppID   int64
	Kind    string
	Keyword string
	Limit   int
	Offset  int
}

// ListCosmeticAssets 素材库（新的在前），带被引用数。
func (r *Repository) ListCosmeticAssets(ctx context.Context, f CosmeticAssetFilter) ([]cosmetic.Asset, int64, error) {
	where := []string{"a.appid = $1"}
	args := []any{f.AppID}
	if f.Kind != "" {
		args = append(args, f.Kind)
		where = append(where, fmt.Sprintf("a.kind = $%d", len(args)))
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		args = append(args, "%"+escapeLike(kw)+"%")
		where = append(where, fmt.Sprintf("a.name ILIKE $%d", len(args)))
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_assets a WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	args = append(args, limit, max(f.Offset, 0))
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticAssetColumns+`,
    (SELECT COUNT(*) FROM cosmetic_item_assets ia WHERE ia.asset_id = a.id)
FROM cosmetic_assets a WHERE `+cond+fmt.Sprintf(` ORDER BY a.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]cosmetic.Asset, 0, limit)
	for rows.Next() {
		var used int64
		a, err := scanCosmeticAsset(rows, &used)
		if err != nil {
			return nil, 0, err
		}
		a.UsedBy = used
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

// GetCosmeticAssets 按编号批量取本应用的素材。
func (r *Repository) GetCosmeticAssets(ctx context.Context, appID int64, ids []int64) (map[int64]cosmetic.Asset, error) {
	out := make(map[int64]cosmetic.Asset, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticAssetColumns+` FROM cosmetic_assets a WHERE a.appid = $1 AND a.id = ANY($2)`,
		appID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanCosmeticAsset(rows)
		if err != nil {
			return nil, err
		}
		out[a.ID] = *a
	}
	return out, rows.Err()
}

// CountCosmeticAssets 应用的素材数（上限判定）。
func (r *Repository) CountCosmeticAssets(ctx context.Context, appID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_assets WHERE appid = $1`, appID).Scan(&n)
	return n, err
}

// DeleteCosmeticAsset 删一条没被引用的素材，返回被删的那条（调用方据此删对象）。
func (r *Repository) DeleteCosmeticAsset(ctx context.Context, appID, assetID int64) (*cosmetic.Asset, error) {
	asset, err := scanCosmeticAsset(r.pool.QueryRow(ctx, `DELETE FROM cosmetic_assets a WHERE a.appid = $1 AND a.id = $2
RETURNING `+cosmeticAssetColumns, appID, assetID))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return nil, ErrCosmeticAssetInUse
	}
	return asset, err
}

// ── 装扮 ──

const cosmeticItemColumns = `i.id, i.appid, i.slot, i.code, i.name, i.description, i.rarity, i.series, i.status, i.unlock,
i.design, i.draft, i.revision, COALESCE(i.preview_ref, ''), i.sort_order, i.hidden, COALESCE(i.created_by, ''),
i.created_at, i.updated_at, i.published_at`

func scanCosmeticItem(row interface{ Scan(dest ...any) error }, extra ...any) (*cosmetic.Item, error) {
	var (
		item                        cosmetic.Item
		unlockRaw, designRaw, draft []byte
	)
	dest := []any{&item.ID, &item.AppID, &item.Slot, &item.Code, &item.Name, &item.Description, &item.Rarity, &item.Series,
		&item.Status, &unlockRaw, &designRaw, &draft, &item.Revision, &item.PreviewRef, &item.SortOrder, &item.Hidden,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(unlockRaw, &item.Unlock); err != nil {
		return nil, fmt.Errorf("decode cosmetic unlock: %w", err)
	}
	if len(designRaw) > 0 {
		var d cosmetic.Design
		if err := json.Unmarshal(designRaw, &d); err != nil {
			return nil, fmt.Errorf("decode cosmetic design: %w", err)
		}
		item.Design = &d
	}
	if len(draft) > 0 {
		var d cosmetic.Design
		if err := json.Unmarshal(draft, &d); err != nil {
			return nil, fmt.Errorf("decode cosmetic draft: %w", err)
		}
		item.Draft = &d
	}
	item.DraftDirty = item.Revision == 0 || !cosmetic.EqualDesign(item.Design, item.Draft)
	return &item, nil
}

// CreateCosmeticItem 新建一件装扮（草稿状态）。
func (r *Repository) CreateCosmeticItem(ctx context.Context, in cosmetic.ItemInput, draft cosmetic.Design) (*cosmetic.Item, error) {
	unlock, _ := json.Marshal(in.Unlock)
	draftRaw, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanCosmeticItem(tx.QueryRow(ctx, `INSERT INTO cosmetic_items AS i
(appid, slot, code, name, description, rarity, series, unlock, draft, sort_order, hidden, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,
    COALESCE(NULLIF($10, 0), (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM cosmetic_items WHERE appid = $1)), $11, $12)
RETURNING `+cosmeticItemColumns,
		in.AppID, in.Slot, in.Code, in.Name, in.Description, in.Rarity, in.Series, unlock, draftRaw, in.SortOrder,
		in.Hidden, nullableString(in.Operator)))
	if err != nil {
		return nil, mapCosmeticUnique(err)
	}
	if err := syncCosmeticItemAssetsTx(ctx, tx, item.ID, draft.AssetIDs()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

func mapCosmeticUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "code") {
		return ErrCosmeticCodeTaken
	}
	return err
}

// syncCosmeticItemAssetsTx 重写装扮引用的素材集合（草稿 ∪ 已发布）。
func syncCosmeticItemAssetsTx(ctx context.Context, tx pgx.Tx, cosmeticID int64, ids []int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM cosmetic_item_assets WHERE cosmetic_id = $1 AND NOT (asset_id = ANY($2))`,
		cosmeticID, ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO cosmetic_item_assets (cosmetic_id, asset_id)
SELECT $1, unnest($2::bigint[]) ON CONFLICT DO NOTHING`, cosmeticID, ids)
	return err
}

// GetCosmeticItem 取一件装扮（含草稿）。
func (r *Repository) GetCosmeticItem(ctx context.Context, appID, id int64) (*cosmetic.Item, error) {
	return scanCosmeticItem(r.pool.QueryRow(ctx, `SELECT `+cosmeticItemColumns+` FROM cosmetic_items i WHERE i.appid = $1 AND i.id = $2`,
		appID, id))
}

// CosmeticItemFilter 管理端列表筛选。
type CosmeticItemFilter struct {
	AppID   int64
	Slot    string
	Status  string
	Rarity  string
	Keyword string
}

// ListCosmeticItems 管理端列表（含草稿与下架），带持有人数与佩戴人数。
func (r *Repository) ListCosmeticItems(ctx context.Context, f CosmeticItemFilter) ([]cosmetic.Item, error) {
	where := []string{"i.appid = $1"}
	args := []any{f.AppID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Slot != "" {
		add("i.slot = $%d", f.Slot)
	}
	if f.Status != "" {
		add("i.status = $%d", f.Status)
	}
	if f.Rarity != "" {
		add("i.rarity = $%d", f.Rarity)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		args = append(args, "%"+escapeLike(kw)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(i.name ILIKE $%d OR i.code ILIKE $%d OR i.series ILIKE $%d)", n, n, n))
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticItemColumns+`,
    (SELECT COUNT(DISTINCT uc.user_id) FROM user_cosmetics uc WHERE uc.cosmetic_id = i.id AND uc.revoked_at IS NULL
        AND (uc.ends_at IS NULL OR uc.ends_at > NOW())),
    (SELECT COUNT(*) FROM user_cosmetic_equips e WHERE e.cosmetic_id = i.id)
FROM cosmetic_items i WHERE `+strings.Join(where, " AND ")+` ORDER BY i.slot, i.sort_order, i.id LIMIT 2000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cosmetic.Item, 0, 32)
	for rows.Next() {
		var owners, equipped int64
		item, err := scanCosmeticItem(rows, &owners, &equipped)
		if err != nil {
			return nil, err
		}
		item.Owners, item.Equipped = owners, equipped
		out = append(out, *item)
	}
	return out, rows.Err()
}

// ListLiveCosmeticItems 用户端可见与可戴判定用：已上架且发布过的全部装扮（含 hidden，调用方自己过滤列出与否）。
func (r *Repository) ListLiveCosmeticItems(ctx context.Context, appID int64) ([]cosmetic.Item, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticItemColumns+` FROM cosmetic_items i
WHERE i.appid = $1 AND i.status = 'active' AND i.revision > 0 ORDER BY i.slot, i.sort_order, i.id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cosmetic.Item, 0, 32)
	for rows.Next() {
		item, err := scanCosmeticItem(rows)
		if err != nil {
			return nil, err
		}
		item.Draft = nil
		out = append(out, *item)
	}
	return out, rows.Err()
}

// CountCosmeticItems 应用的装扮数（上限判定）。
func (r *Repository) CountCosmeticItems(ctx context.Context, appID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_items WHERE appid = $1`, appID).Scan(&n)
	return n, err
}

// UpdateCosmeticItemMeta 改基本信息。槽位一经创建不能改（设计稿的画布规格随槽位而定）。
func (r *Repository) UpdateCosmeticItemMeta(ctx context.Context, id int64, in cosmetic.ItemInput) (*cosmetic.Item, error) {
	unlock, _ := json.Marshal(in.Unlock)
	item, err := scanCosmeticItem(r.pool.QueryRow(ctx, `UPDATE cosmetic_items i SET code = $3, name = $4, description = $5,
    rarity = $6, series = $7, unlock = $8, sort_order = $9, hidden = $10, updated_at = NOW()
WHERE i.appid = $1 AND i.id = $2 RETURNING `+cosmeticItemColumns,
		in.AppID, id, in.Code, in.Name, in.Description, in.Rarity, in.Series, unlock, in.SortOrder, in.Hidden))
	if err != nil {
		return nil, mapCosmeticUnique(err)
	}
	return item, nil
}

// SaveCosmeticDraft 保存设计器草稿。
func (r *Repository) SaveCosmeticDraft(ctx context.Context, appID, id int64, draft cosmetic.Design) (*cosmetic.Item, error) {
	raw, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanCosmeticItem(tx.QueryRow(ctx, `UPDATE cosmetic_items i SET draft = $3, updated_at = NOW()
WHERE i.appid = $1 AND i.id = $2 RETURNING `+cosmeticItemColumns, appID, id, raw))
	if err != nil {
		return nil, err
	}
	ids := draft.AssetIDs()
	if item.Design != nil {
		ids = mergeIDs(ids, item.Design.AssetIDs())
	}
	if err := syncCosmeticItemAssetsTx(ctx, tx, id, ids); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

func mergeIDs(a, b []int64) []int64 {
	seen := make(map[int64]bool, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, list := range [][]int64{a, b} {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// PublishCosmeticItem 把草稿发布成新版本：版本号 +1、留一份版本记录、只保留最近 MaxRevisionsKept 份。
// previewRef 为空时保留原来的预览图。
func (r *Repository) PublishCosmeticItem(ctx context.Context, appID, id int64, note, previewRef, operator string) (*cosmetic.Item, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanCosmeticItem(tx.QueryRow(ctx, `SELECT `+cosmeticItemColumns+` FROM cosmetic_items i
WHERE i.appid = $1 AND i.id = $2 FOR UPDATE`, appID, id))
	if err != nil {
		return nil, err
	}
	next := current.Revision + 1
	item, err := scanCosmeticItem(tx.QueryRow(ctx, `UPDATE cosmetic_items i SET design = i.draft, revision = $3,
    preview_ref = COALESCE(NULLIF($4, ''), i.preview_ref), published_at = NOW(), updated_at = NOW()
WHERE i.appid = $1 AND i.id = $2 RETURNING `+cosmeticItemColumns, appID, id, next, previewRef))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO cosmetic_revisions (appid, cosmetic_id, revision, design, note, published_by)
SELECT appid, id, revision, design, $2, $3 FROM cosmetic_items WHERE id = $1`,
		id, truncateColumn(strings.TrimSpace(note), 256), nullableString(operator)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cosmetic_revisions WHERE cosmetic_id = $1 AND revision <= $2`,
		id, next-cosmetic.MaxRevisionsKept); err != nil {
		return nil, err
	}
	if item.Design != nil {
		if err := syncCosmeticItemAssetsTx(ctx, tx, id, item.Design.AssetIDs()); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// ListCosmeticRevisions 发布历史（新的在前）。
func (r *Repository) ListCosmeticRevisions(ctx context.Context, appID, id int64) ([]cosmetic.Revision, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, cosmetic_id, revision, design, note, COALESCE(published_by, ''), created_at
FROM cosmetic_revisions WHERE appid = $1 AND cosmetic_id = $2 ORDER BY revision DESC`, appID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cosmetic.Revision, 0, 8)
	for rows.Next() {
		var rev cosmetic.Revision
		var raw []byte
		if err := rows.Scan(&rev.ID, &rev.CosmeticID, &rev.Revision, &raw, &rev.Note, &rev.PublishedBy, &rev.CreatedAt); err != nil {
			return nil, err
		}
		var d cosmetic.Design
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		rev.Design = &d
		out = append(out, rev)
	}
	return out, rows.Err()
}

// GetCosmeticRevision 取某个版本的设计稿。
func (r *Repository) GetCosmeticRevision(ctx context.Context, appID, id int64, revision int) (*cosmetic.Design, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT design FROM cosmetic_revisions WHERE appid = $1 AND cosmetic_id = $2 AND revision = $3`,
		appID, id, revision).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCosmeticRevisionAbsent
	}
	if err != nil {
		return nil, err
	}
	var d cosmetic.Design
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// SetCosmeticItemStatus 上架 / 下架 / 回到草稿。上架要求发布过（表约束兜底）。
func (r *Repository) SetCosmeticItemStatus(ctx context.Context, appID, id int64, status string) (*cosmetic.Item, error) {
	item, err := scanCosmeticItem(r.pool.QueryRow(ctx, `UPDATE cosmetic_items i SET status = $3, updated_at = NOW()
WHERE i.appid = $1 AND i.id = $2 RETURNING `+cosmeticItemColumns, appID, id, status))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" {
		return nil, ErrCosmeticNotPublished
	}
	return item, err
}

// SetCosmeticPreview 换预览图。
func (r *Repository) SetCosmeticPreview(ctx context.Context, appID, id int64, ref string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE cosmetic_items SET preview_ref = $3, updated_at = NOW() WHERE appid = $1 AND id = $2`,
		appID, id, nullableString(ref))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCosmeticNotFound
	}
	return nil
}

// ReorderCosmeticItems 按给定顺序重写排序值 10、20、30……
func (r *Repository) ReorderCosmeticItems(ctx context.Context, appID int64, ids []int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE cosmetic_items c SET sort_order = o.ord * 10, updated_at = NOW()
FROM unnest($2::bigint[]) WITH ORDINALITY AS o(id, ord) WHERE c.appid = $1 AND c.id = o.id`, appID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteCosmeticItem 删除装扮。有人持有过就不让删（持有记录是兑换单、抽奖记录指着的账），改用下架。
func (r *Repository) DeleteCosmeticItem(ctx context.Context, appID, id int64) error {
	var owned bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_cosmetics WHERE appid = $1 AND cosmetic_id = $2)`,
		appID, id).Scan(&owned); err != nil {
		return err
	}
	if owned {
		return ErrCosmeticHasOwners
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM cosmetic_items WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCosmeticNotFound
	}
	return nil
}

// CosmeticRefs 发放分支描述物品用：编号 → 名称与槽位（含未上架的，描述历史记录时也要认得）。
func (r *Repository) CosmeticRefs(ctx context.Context, appID int64) (map[int64]CosmeticRef, error) {
	return cosmeticRefsQ(ctx, r.pool, appID)
}

// CosmeticRef 一件装扮的引用信息。
type CosmeticRef struct {
	ID     int64
	Name   string
	Slot   string
	Rarity string
	Live   bool
}

func cosmeticRefsQ(ctx context.Context, q queryExecutor, appID int64) (map[int64]CosmeticRef, error) {
	rows, err := q.Query(ctx, `SELECT id, name, slot, rarity, (status = 'active' AND revision > 0) FROM cosmetic_items WHERE appid = $1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]CosmeticRef)
	for rows.Next() {
		var ref CosmeticRef
		if err := rows.Scan(&ref.ID, &ref.Name, &ref.Slot, &ref.Rarity, &ref.Live); err != nil {
			return nil, err
		}
		out[ref.ID] = ref
	}
	return out, rows.Err()
}

// ── 持有 ──

const cosmeticOwnershipColumns = `uc.id, uc.appid, uc.user_id, uc.cosmetic_id, uc.source, COALESCE(uc.source_ref, ''), uc.duration_days,
uc.starts_at, uc.ends_at, uc.revoked_at, COALESCE(uc.revoked_by, ''), COALESCE(uc.revoke_reason, ''), COALESCE(uc.operator, ''),
uc.created_at`

func scanCosmeticOwnership(row interface{ Scan(dest ...any) error }, extra ...any) (*cosmetic.Ownership, error) {
	var o cosmetic.Ownership
	dest := []any{&o.ID, &o.AppID, &o.UserID, &o.CosmeticID, &o.Source, &o.SourceRef, &o.DurationDays, &o.StartsAt, &o.EndsAt,
		&o.RevokedAt, &o.RevokedBy, &o.RevokeReason, &o.Operator, &o.CreatedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticOwnershipGone
		}
		return nil, err
	}
	return &o, nil
}

// grantCosmeticTx 事务内发一件装扮。积分商城、抽奖、卡密、物品发放、邀请、排行榜、推广、管理员授予全部走这里。
//
// 与功能解锁同一口径：限时持有顺延（新的一段从「这件装扮仍生效的限时持有里最晚的终点」与现在较晚者开始），
// 永久持有没有终点；已永久持有时不再记新的一段，返回 AlreadyPermanent（发放方按「已拥有」处理，不算失败）。
// 装扮必须是上架且发布过的：发出去一件用户戴不上的东西比报错更糟。
//
// 锁用户行：调用方（兑换、抽奖事务）通常已经锁过；管理员授予没有，这一句让并发的两次顺延串行化。
func grantCosmeticTx(ctx context.Context, tx pgx.Tx, in cosmetic.GrantInput) (*cosmetic.GrantResult, error) {
	if err := cosmetic.ValidateGrantDays(in.Days); err != nil {
		return nil, err
	}
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		in.UserID, in.AppID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	var (
		name, slot, status string
		revision           int
	)
	if err := tx.QueryRow(ctx, `SELECT name, slot, status, revision FROM cosmetic_items WHERE appid = $1 AND id = $2`,
		in.AppID, in.CosmeticID).Scan(&name, &slot, &status, &revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w：装扮 #%d", ErrCosmeticUnavailable, in.CosmeticID)
		}
		return nil, err
	}
	if status != cosmetic.StatusActive || revision == 0 {
		return nil, fmt.Errorf("%w：%s", ErrCosmeticUnavailable, name)
	}
	result := &cosmetic.GrantResult{CosmeticName: name, Slot: slot}

	var permanent bool
	var latestEnd *time.Time
	if err := tx.QueryRow(ctx, `SELECT COALESCE(BOOL_OR(ends_at IS NULL), FALSE), MAX(ends_at)
FROM user_cosmetics
WHERE appid = $1 AND user_id = $2 AND cosmetic_id = $3 AND revoked_at IS NULL AND (ends_at IS NULL OR ends_at > NOW())`,
		in.AppID, in.UserID, in.CosmeticID).Scan(&permanent, &latestEnd); err != nil {
		return nil, err
	}
	if permanent {
		result.AlreadyPermanent = true
		return result, nil
	}
	now := time.Now().UTC()
	start := now
	if latestEnd != nil && latestEnd.After(now) {
		start = latestEnd.UTC()
	}
	var end *time.Time
	if in.Days > 0 {
		value := start.AddDate(0, 0, in.Days)
		end = &value
	} else {
		start = now
	}
	source := in.Source
	if !cosmetic.IsSource(source) {
		source = cosmetic.SourceAdminGrant
	}
	own, err := scanCosmeticOwnership(tx.QueryRow(ctx, `INSERT INTO user_cosmetics AS uc
(appid, user_id, cosmetic_id, source, source_ref, duration_days, starts_at, ends_at, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+cosmeticOwnershipColumns,
		in.AppID, in.UserID, in.CosmeticID, source, nullableString(truncateColumn(in.SourceRef, 64)), in.Days, start, end,
		nullableString(in.Operator)))
	if err != nil {
		return nil, err
	}
	own.CosmeticName, own.Slot = name, slot
	result.Ownership = own
	return result, nil
}

// cosmeticOwnedPermanentlyQ 是否已永久持有（积分商城在扣积分之前判「已拥有」用）。
func cosmeticOwnedPermanentlyQ(ctx context.Context, q queryExecutor, appID, userID, cosmeticID int64) (bool, error) {
	var owned bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_cosmetics WHERE appid = $1 AND user_id = $2 AND cosmetic_id = $3
    AND revoked_at IS NULL AND ends_at IS NULL)`, appID, userID, cosmeticID).Scan(&owned)
	return owned, err
}

// GrantCosmetic 单独一个事务发一件装扮（管理员授予）。
func (r *Repository) GrantCosmetic(ctx context.Context, in cosmetic.GrantInput) (*cosmetic.GrantResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := grantCosmeticTx(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// RevokeCosmeticOwnership 收回一条持有（只标记）。与功能解锁同理：排在它后面的限时段不前移。
func (r *Repository) RevokeCosmeticOwnership(ctx context.Context, appID, userID, ownershipID int64, operator, reason string) (*cosmetic.Ownership, error) {
	return scanCosmeticOwnership(r.pool.QueryRow(ctx, `UPDATE user_cosmetics uc
SET revoked_at = NOW(), revoked_by = $4, revoke_reason = $5
WHERE uc.id = $1 AND uc.appid = $2 AND uc.user_id = $3 AND uc.revoked_at IS NULL
RETURNING `+cosmeticOwnershipColumns, ownershipID, appID, userID,
		nullableString(operator), nullableString(truncateColumn(strings.TrimSpace(reason), 256))))
}

// CosmeticOwnershipFilter 持有记录筛选。
type CosmeticOwnershipFilter struct {
	AppID      int64
	UserID     int64
	CosmeticID int64
	Source     string
	// State live 仍生效 / ended 已结束 / revoked 已收回；空为全部
	State  string
	Limit  int
	Offset int
}

// ListCosmeticOwnerships 持有记录（新的在前），带装扮与用户信息。
func (r *Repository) ListCosmeticOwnerships(ctx context.Context, f CosmeticOwnershipFilter) ([]cosmetic.Ownership, int64, error) {
	where := []string{"uc.appid = $1"}
	args := []any{f.AppID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.UserID > 0 {
		add("uc.user_id = $%d", f.UserID)
	}
	if f.CosmeticID > 0 {
		add("uc.cosmetic_id = $%d", f.CosmeticID)
	}
	if f.Source != "" {
		add("uc.source = $%d", f.Source)
	}
	switch f.State {
	case "live":
		where = append(where, "uc.revoked_at IS NULL AND (uc.ends_at IS NULL OR uc.ends_at > NOW())")
	case "ended":
		where = append(where, "uc.revoked_at IS NULL AND uc.ends_at IS NOT NULL AND uc.ends_at <= NOW()")
	case "revoked":
		where = append(where, "uc.revoked_at IS NOT NULL")
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_cosmetics uc WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, max(f.Offset, 0))
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticOwnershipColumns+`, ci.name, ci.slot, ci.rarity, u.account, COALESCE(p.nickname, '')
FROM user_cosmetics uc
JOIN cosmetic_items ci ON ci.id = uc.cosmetic_id
JOIN users u ON u.id = uc.user_id
LEFT JOIN user_profiles p ON p.user_id = uc.user_id
WHERE `+cond+fmt.Sprintf(` ORDER BY uc.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]cosmetic.Ownership, 0, limit)
	for rows.Next() {
		var o *cosmetic.Ownership
		var name, slot, rarity, account, nickname string
		o, err = scanCosmeticOwnership(rows, &name, &slot, &rarity, &account, &nickname)
		if err != nil {
			return nil, 0, err
		}
		o.CosmeticName, o.Slot, o.Rarity, o.UserAccount, o.UserNickname = name, slot, rarity, account, nickname
		out = append(out, *o)
	}
	return out, total, rows.Err()
}

// ── 佩戴与判定事实 ──

// GetUserCosmeticEquips 一个人全部的佩戴行（未按资格过滤）。
func (r *Repository) GetUserCosmeticEquips(ctx context.Context, appID, userID int64) ([]cosmetic.Equip, error) {
	rows, err := r.pool.Query(ctx, `SELECT slot, position, cosmetic_id, equipped_at FROM user_cosmetic_equips
WHERE appid = $1 AND user_id = $2 ORDER BY slot, position`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cosmetic.Equip, 0, 4)
	for rows.Next() {
		var e cosmetic.Equip
		if err := rows.Scan(&e.Slot, &e.Position, &e.CosmeticID, &e.EquippedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetUserCosmeticEquip 在一个槽位（的某一位）戴上一件；同一件勋章已戴在别的位置时先摘下来（不会戴两枚一样的）。
func (r *Repository) SetUserCosmeticEquip(ctx context.Context, appID, userID int64, slot string, position int, cosmeticID int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM user_cosmetic_equips WHERE appid = $1 AND user_id = $2 AND slot = $3
    AND cosmetic_id = $4 AND position <> $5`, appID, userID, slot, cosmeticID, position); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_cosmetic_equips (appid, user_id, slot, position, cosmetic_id, equipped_at)
VALUES ($1,$2,$3,$4,$5,NOW())
ON CONFLICT (appid, user_id, slot, position) DO UPDATE SET cosmetic_id = EXCLUDED.cosmetic_id, equipped_at = NOW()`,
		appID, userID, slot, position, cosmeticID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ClearUserCosmeticEquip 摘下一个槽位；position < 0 摘下这个槽位的全部（勋章）。
func (r *Repository) ClearUserCosmeticEquip(ctx context.Context, appID, userID int64, slot string, position int) error {
	if position < 0 {
		_, err := r.pool.Exec(ctx, `DELETE FROM user_cosmetic_equips WHERE appid = $1 AND user_id = $2 AND slot = $3`,
			appID, userID, slot)
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM user_cosmetic_equips WHERE appid = $1 AND user_id = $2 AND slot = $3 AND position = $4`,
		appID, userID, slot, position)
	return err
}

// CosmeticUserFacts 一个人的判定事实与佩戴行（批量查询的一项）。
type CosmeticUserFacts struct {
	UserID   int64
	PublicID int64
	Facts    cosmetic.Facts
	Equips   []cosmetic.Equip
}

// GetCosmeticUserFacts 批量取判定事实：会员（与 /vip/status 同一份事实、同一个判定）、等级、持有、佩戴。
//
// 用管道一次往返发完：每人一条会员事实查询（vipEntitlementFactsSQL，与积分商城、抽奖判「已拥有」同一条），
// 外加三条按人数组聚合的查询。onlyEquipped 时只给有佩戴记录的人查会员事实（看别人身上的装扮时，
// 没戴任何东西的人不必判）。
func (r *Repository) GetCosmeticUserFacts(ctx context.Context, appID int64, userIDs []int64, onlyEquipped bool) (map[int64]*CosmeticUserFacts, error) {
	out := make(map[int64]*CosmeticUserFacts, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT u.id, u.public_id, COALESCE(l.current_level, 0)
FROM users u LEFT JOIN user_level_records l ON l.user_id = u.id AND l.appid = u.appid
WHERE u.appid = $1 AND u.id = ANY($2)`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		f := &CosmeticUserFacts{Facts: cosmetic.Facts{Owned: map[int64]*time.Time{}}}
		if err := rows.Scan(&f.UserID, &f.PublicID, &f.Facts.Level); err != nil {
			rows.Close()
			return nil, err
		}
		out[f.UserID] = f
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `SELECT user_id, slot, position, cosmetic_id, equipped_at FROM user_cosmetic_equips
WHERE appid = $1 AND user_id = ANY($2) ORDER BY user_id, slot, position`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var uid int64
		var e cosmetic.Equip
		if err := rows.Scan(&uid, &e.Slot, &e.Position, &e.CosmeticID, &e.EquippedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if f := out[uid]; f != nil {
			f.Equips = append(f.Equips, e)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 持有：每件装扮取仍生效记录里最晚的终点（有永久的即永久）
	rows, err = r.pool.Query(ctx, `SELECT user_id, cosmetic_id, BOOL_OR(ends_at IS NULL), MAX(ends_at)
FROM user_cosmetics WHERE appid = $1 AND user_id = ANY($2) AND revoked_at IS NULL AND (ends_at IS NULL OR ends_at > NOW())
GROUP BY user_id, cosmetic_id`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var uid, cid int64
		var permanent bool
		var end *time.Time
		if err := rows.Scan(&uid, &cid, &permanent, &end); err != nil {
			rows.Close()
			return nil, err
		}
		if f := out[uid]; f != nil {
			if permanent {
				end = nil
			}
			f.Facts.Owned[cid] = end
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	batch := &pgx.Batch{}
	queued := make([]int64, 0, len(out))
	for uid, f := range out {
		if onlyEquipped && len(f.Equips) == 0 {
			continue
		}
		batch.Queue(vipEntitlementFactsSQL, uid, appID)
		queued = append(queued, uid)
	}
	if len(queued) == 0 {
		return out, nil
	}
	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()
	now := time.Now()
	for _, uid := range queued {
		facts, err := scanVipEntitlementFacts(results.QueryRow(), appID, uid)
		if err != nil {
			return nil, err
		}
		ent := vipdomain.Evaluate(*facts, now)
		f := out[uid]
		f.Facts.IsVIP = ent.IsVIP
		f.Facts.Features = ent.Features
		if ent.IsVIP {
			for _, seg := range facts.Segments {
				if seg.TierID != nil {
					f.Facts.TierIDs = append(f.Facts.TierIDs, *seg.TierID)
				}
			}
		}
	}
	return out, nil
}

// ResolveCosmeticPublicIDs 把对外 ID 换成本应用用户的主键（查别人身上的装扮）。
func (r *Repository) ResolveCosmeticPublicIDs(ctx context.Context, appID int64, publicIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(publicIDs))
	if len(publicIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT public_id, id FROM users WHERE appid = $1 AND public_id = ANY($2)`, appID, publicIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pub, id int64
		if err := rows.Scan(&pub, &id); err != nil {
			return nil, err
		}
		out[pub] = id
	}
	return out, rows.Err()
}

// ── 渠道与统计 ──

// CosmeticChannels 装扮在各系统里的获取渠道：上架中的积分商城商品、进行中的抽奖活动。
// 两个系统的权益都存在 JSONB 数组里（type=cosmetic、refId=装扮编号、amount=天数）。
func (r *Repository) CosmeticChannels(ctx context.Context, appID int64) (map[int64][]cosmetic.Channel, error) {
	out := make(map[int64][]cosmetic.Channel)
	rows, err := r.pool.Query(ctx, `SELECT (rw->>'refId')::bigint, s.id, s.title, s.cost, COALESCE((rw->>'amount')::int, 0)
FROM points_store_items s, jsonb_array_elements(s.rewards) rw
WHERE s.appid = $1 AND s.status = 'active' AND rw->>'type' = 'cosmetic'
  AND (s.starts_at IS NULL OR s.starts_at <= NOW()) AND (s.ends_at IS NULL OR s.ends_at > NOW())
ORDER BY s.sort_order, s.id`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid int64
		var ch cosmetic.Channel
		if err := rows.Scan(&cid, &ch.RefID, &ch.Title, &ch.Cost, &ch.Days); err != nil {
			rows.Close()
			return nil, err
		}
		ch.Kind = cosmetic.ChannelPointsStore
		out[cid] = append(out[cid], ch)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = r.pool.Query(ctx, `SELECT DISTINCT ON ((p.reward->>'refId'), a.id)
    (p.reward->>'refId')::bigint, a.id, a.name, COALESCE((p.reward->>'amount')::int, 0)
FROM lottery_prizes p JOIN lottery_activities a ON a.id = p.activity_id
WHERE a.appid = $1 AND a.status = 'active' AND p.kind = 'cosmetic' AND p.reward->>'refId' IS NOT NULL
  AND a.start_time <= NOW() AND a.end_time > NOW()`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var ch cosmetic.Channel
		if err := rows.Scan(&cid, &ch.RefID, &ch.Title, &ch.Days); err != nil {
			return nil, err
		}
		ch.Kind = cosmetic.ChannelLottery
		out[cid] = append(out[cid], ch)
	}
	return out, rows.Err()
}

// CosmeticStats 管理端统计。
func (r *Repository) CosmeticStats(ctx context.Context, appID int64) (*cosmetic.Stats, error) {
	s := &cosmetic.Stats{BySlot: []cosmetic.SlotStat{}, BySource: []cosmetic.SourceStat{}, TopItems: []cosmetic.ItemStat{},
		DailyGrants: []cosmetic.DailyGrantRow{}}
	if err := r.pool.QueryRow(ctx, `SELECT
    (SELECT COUNT(*) FROM cosmetic_items WHERE appid = $1),
    (SELECT COUNT(*) FROM cosmetic_items WHERE appid = $1 AND status = 'active' AND revision > 0),
    (SELECT COUNT(DISTINCT user_id) FROM user_cosmetics WHERE appid = $1 AND revoked_at IS NULL AND (ends_at IS NULL OR ends_at > NOW())),
    (SELECT COUNT(*) FROM user_cosmetics WHERE appid = $1),
    (SELECT COUNT(DISTINCT user_id) FROM user_cosmetic_equips WHERE appid = $1),
    (SELECT COUNT(*) FROM user_cosmetics WHERE appid = $1 AND revoked_at IS NULL AND ends_at > NOW() AND ends_at <= NOW() + INTERVAL '7 days'),
    (SELECT COUNT(*) FROM cosmetic_assets WHERE appid = $1),
    (SELECT COALESCE(SUM(size_bytes), 0) FROM cosmetic_assets WHERE appid = $1),
    (SELECT COUNT(*) FROM cosmetic_items WHERE appid = $1 AND (revision = 0 OR design IS DISTINCT FROM draft))`, appID).Scan(
		&s.Items, &s.LiveItems, &s.Owners, &s.Ownerships, &s.Wearers, &s.ExpiringSoon, &s.AssetCount, &s.AssetBytes, &s.DraftsPending); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT i.slot, COUNT(DISTINCT i.id),
    COUNT(DISTINCT uc.user_id) FILTER (WHERE uc.id IS NOT NULL),
    (SELECT COUNT(*) FROM user_cosmetic_equips e WHERE e.appid = $1 AND e.slot = i.slot)
FROM cosmetic_items i
LEFT JOIN user_cosmetics uc ON uc.cosmetic_id = i.id AND uc.revoked_at IS NULL AND (uc.ends_at IS NULL OR uc.ends_at > NOW())
WHERE i.appid = $1 GROUP BY i.slot`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st cosmetic.SlotStat
		if err := rows.Scan(&st.Slot, &st.Items, &st.Owners, &st.Equipped); err != nil {
			rows.Close()
			return nil, err
		}
		s.BySlot = append(s.BySlot, st)
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT source, COUNT(*) FROM user_cosmetics WHERE appid = $1 GROUP BY source ORDER BY 2 DESC`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st cosmetic.SourceStat
		if err := rows.Scan(&st.Source, &st.Count); err != nil {
			rows.Close()
			return nil, err
		}
		s.BySource = append(s.BySource, st)
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT i.id, i.name, i.slot, i.rarity,
    (SELECT COUNT(DISTINCT uc.user_id) FROM user_cosmetics uc WHERE uc.cosmetic_id = i.id AND uc.revoked_at IS NULL
        AND (uc.ends_at IS NULL OR uc.ends_at > NOW())) AS owners,
    (SELECT COUNT(*) FROM user_cosmetic_equips e WHERE e.cosmetic_id = i.id) AS equipped
FROM cosmetic_items i WHERE i.appid = $1 ORDER BY equipped DESC, owners DESC, i.id LIMIT 10`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st cosmetic.ItemStat
		if err := rows.Scan(&st.ID, &st.Name, &st.Slot, &st.Rarity, &st.Owners, &st.Equipped); err != nil {
			rows.Close()
			return nil, err
		}
		s.TopItems = append(s.TopItems, st)
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT to_char(d, 'YYYY-MM-DD'), COALESCE(c.n, 0)
FROM generate_series(date_trunc('day', NOW()) - INTERVAL '13 days', date_trunc('day', NOW()), INTERVAL '1 day') d
LEFT JOIN (SELECT date_trunc('day', created_at) AS day, COUNT(*) AS n FROM user_cosmetics
    WHERE appid = $1 AND created_at >= date_trunc('day', NOW()) - INTERVAL '13 days' GROUP BY 1) c ON c.day = d
ORDER BY d`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var row cosmetic.DailyGrantRow
		if err := rows.Scan(&row.Day, &row.Count); err != nil {
			rows.Close()
			return nil, err
		}
		s.DailyGrants = append(s.DailyGrants, row)
	}
	rows.Close()
	recent, _, err := r.ListCosmeticOwnerships(ctx, CosmeticOwnershipFilter{AppID: appID, Limit: 8})
	if err != nil {
		return nil, err
	}
	s.RecentGrants = recent
	return s, nil
}
