package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"aegis/internal/domain/cosmetic"
)

// 自定义工坊（docs/cosmetics.md「自定义工坊」）：零件与作品。作品是 cosmetic_items 里 origin = 'workshop' 的行。

var (
	ErrWorkshopPartNotFound  = errors.New("workshop part not found")
	ErrWorkshopPartDuplicate = errors.New("workshop part already exists for asset")
	// ErrWorkshopQuotaFull 作品数已达额度
	ErrWorkshopQuotaFull = errors.New("workshop quota full")
	// ErrWorkshopTakenDown 作品已被运营下架，不能再改
	ErrWorkshopTakenDown = errors.New("workshop item taken down")
)

// ── 零件 ──

const workshopPartColumns = `p.id, p.appid, p.asset_id, p.name, p.category, p.slots, p.columns_, p.rows_, p.frame_count, p.fps,
p.sort_order, p.enabled, p.created_at, p.updated_at`

func scanWorkshopPart(row interface{ Scan(dest ...any) error }) (*cosmetic.Part, error) {
	var p cosmetic.Part
	if err := row.Scan(&p.ID, &p.AppID, &p.AssetID, &p.Name, &p.Category, &p.Slots, &p.Columns, &p.Rows, &p.FrameCount,
		&p.FPS, &p.SortOrder, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWorkshopPartNotFound
		}
		return nil, err
	}
	if p.Slots == nil {
		p.Slots = []string{}
	}
	return &p, nil
}

// ListWorkshopParts 应用的零件；onlyEnabled 时只要启用的（用户端）。
func (r *Repository) ListWorkshopParts(ctx context.Context, appID int64, onlyEnabled bool) ([]cosmetic.Part, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+workshopPartColumns+` FROM cosmetic_workshop_parts p
WHERE p.appid = $1 AND ($2 = FALSE OR p.enabled) ORDER BY p.sort_order, p.id`, appID, onlyEnabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cosmetic.Part, 0, 32)
	for rows.Next() {
		p, err := scanWorkshopPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// CountWorkshopParts 应用的零件数（上限判定）。
func (r *Repository) CountWorkshopParts(ctx context.Context, appID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_workshop_parts WHERE appid = $1`, appID).Scan(&n)
	return n, err
}

func mapWorkshopPartErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrWorkshopPartDuplicate
	}
	return err
}

// CreateWorkshopPart 登记零件。
func (r *Repository) CreateWorkshopPart(ctx context.Context, appID int64, in cosmetic.PartInput) (*cosmetic.Part, error) {
	p, err := scanWorkshopPart(r.pool.QueryRow(ctx, `INSERT INTO cosmetic_workshop_parts AS p
(appid, asset_id, name, category, slots, columns_, rows_, frame_count, fps, sort_order, enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
    COALESCE(NULLIF($10, 0), (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM cosmetic_workshop_parts WHERE appid = $1)), $11)
RETURNING `+workshopPartColumns, appID, in.AssetID, in.Name, in.Category, in.Slots, in.Columns, in.Rows, in.FrameCount,
		in.FPS, in.SortOrder, in.Enabled))
	if err != nil {
		return nil, mapWorkshopPartErr(err)
	}
	return p, nil
}

// UpdateWorkshopPart 修改零件。已在作品里用着的零件改了切分或下线，作品照旧能戴（只影响新保存）。
func (r *Repository) UpdateWorkshopPart(ctx context.Context, appID, id int64, in cosmetic.PartInput) (*cosmetic.Part, error) {
	p, err := scanWorkshopPart(r.pool.QueryRow(ctx, `UPDATE cosmetic_workshop_parts p SET asset_id = $3, name = $4,
    category = $5, slots = $6, columns_ = $7, rows_ = $8, frame_count = $9, fps = $10, sort_order = $11, enabled = $12,
    updated_at = NOW()
WHERE p.appid = $1 AND p.id = $2 RETURNING `+workshopPartColumns, appID, id, in.AssetID, in.Name, in.Category, in.Slots,
		in.Columns, in.Rows, in.FrameCount, in.FPS, in.SortOrder, in.Enabled))
	if err != nil {
		return nil, mapWorkshopPartErr(err)
	}
	return p, nil
}

// DeleteWorkshopPart 删零件（素材本身留在素材库里；已用了它的作品照旧能戴）。
func (r *Repository) DeleteWorkshopPart(ctx context.Context, appID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM cosmetic_workshop_parts WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWorkshopPartNotFound
	}
	return nil
}

// ── 作品 ──

func workshopCode(userID int64) (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("ws-%d-%s", userID, hex.EncodeToString(buf)), nil
}

// CreateWorkshopItem 新建作品：锁用户行后数作品数，满了报 ErrWorkshopQuotaFull；作品直接是上架、发布过的第 1 版。
func (r *Repository) CreateWorkshopItem(ctx context.Context, appID, userID int64, quota int, in cosmetic.WorkshopInput) (*cosmetic.Item, error) {
	raw, err := json.Marshal(in.Design)
	if err != nil {
		return nil, err
	}
	unlock, _ := json.Marshal(cosmetic.Unlock{Mode: cosmetic.UnlockCreator})
	code, err := workshopCode(userID)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_items WHERE appid = $1 AND origin = 'workshop' AND creator_user_id = $2`,
		appID, userID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= quota {
		return nil, ErrWorkshopQuotaFull
	}
	item, err := scanCosmeticItem(tx.QueryRow(ctx, `INSERT INTO cosmetic_items AS i
(appid, slot, code, name, rarity, status, unlock, design, draft, revision, origin, creator_user_id, published_at)
VALUES ($1, $2, $3, $4, 'common', 'active', $5, $6, $6, 1, 'workshop', $7, NOW())
RETURNING `+cosmeticItemColumns, appID, in.Slot, code, in.Name, unlock, raw, userID))
	if err != nil {
		return nil, err
	}
	if err := syncCosmeticItemAssetsTx(ctx, tx, item.ID, in.Design.AssetIDs()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateWorkshopItem 修改作品（名称与设计稿），版本号 +1。被下架的作品不能改。
func (r *Repository) UpdateWorkshopItem(ctx context.Context, appID, userID, id int64, name string, design cosmetic.Design) (*cosmetic.Item, error) {
	raw, err := json.Marshal(design)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanCosmeticItem(tx.QueryRow(ctx, `SELECT `+cosmeticItemColumns+` FROM cosmetic_items i
WHERE i.appid = $1 AND i.id = $2 AND i.origin = 'workshop' AND i.creator_user_id = $3 FOR UPDATE`, appID, id, userID))
	if err != nil {
		return nil, err
	}
	if current.Status != cosmetic.StatusActive {
		return nil, ErrWorkshopTakenDown
	}
	item, err := scanCosmeticItem(tx.QueryRow(ctx, `UPDATE cosmetic_items i SET name = $3, design = $4, draft = $4,
    revision = i.revision + 1, published_at = NOW(), updated_at = NOW()
WHERE i.appid = $1 AND i.id = $2 RETURNING `+cosmeticItemColumns, appID, id, name, raw))
	if err != nil {
		return nil, err
	}
	if err := syncCosmeticItemAssetsTx(ctx, tx, id, design.AssetIDs()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// DeleteWorkshopItem 作者删自己的作品（佩戴行随外键一起删掉）。
func (r *Repository) DeleteWorkshopItem(ctx context.Context, appID, userID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM cosmetic_items WHERE appid = $1 AND id = $2 AND origin = 'workshop' AND creator_user_id = $3`,
		appID, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCosmeticNotFound
	}
	return nil
}

// ListUserWorkshopItems 作者的全部作品（含被下架的），新的在前。
func (r *Repository) ListUserWorkshopItems(ctx context.Context, appID, userID int64) ([]cosmetic.Item, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticItemColumns+` FROM cosmetic_items i
WHERE i.appid = $1 AND i.origin = 'workshop' AND i.creator_user_id = $2 ORDER BY i.updated_at DESC, i.id DESC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cosmetic.Item, 0, 8)
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

// GetWorkshopItems 按编号取工坊作品（佩戴、读取别人身上的装扮时用；不论状态，调用方按 Eligibility 判）。
func (r *Repository) GetWorkshopItems(ctx context.Context, appID int64, ids []int64) (map[int64]cosmetic.Item, error) {
	out := make(map[int64]cosmetic.Item, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticItemColumns+` FROM cosmetic_items i
WHERE i.appid = $1 AND i.origin = 'workshop' AND i.id = ANY($2)`, appID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanCosmeticItem(rows)
		if err != nil {
			return nil, err
		}
		item.Draft = nil
		out[item.ID] = *item
	}
	return out, rows.Err()
}

// WorkshopItemFilter 管理端作品列表筛选。
type WorkshopItemFilter struct {
	AppID   int64
	UserID  int64
	Slot    string
	Status  string
	Keyword string
	Limit   int
	Offset  int
}

// ListWorkshopItemsAdmin 管理端作品列表（带作者），新的在前。
func (r *Repository) ListWorkshopItemsAdmin(ctx context.Context, f WorkshopItemFilter) ([]cosmetic.WorkshopAdminItem, int64, error) {
	where := []string{"i.appid = $1", "i.origin = 'workshop'"}
	args := []any{f.AppID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.UserID > 0 {
		add("i.creator_user_id = $%d", f.UserID)
	}
	if f.Slot != "" {
		add("i.slot = $%d", f.Slot)
	}
	if f.Status != "" {
		add("i.status = $%d", f.Status)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		args = append(args, "%"+escapeLike(kw)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(i.name ILIKE $%d OR u.account ILIKE $%d OR pf.nickname ILIKE $%d)", n, n, n))
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_items i LEFT JOIN users u ON u.id = i.creator_user_id
LEFT JOIN user_profiles pf ON pf.user_id = i.creator_user_id
WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args = append(args, limit, max(f.Offset, 0))
	rows, err := r.pool.Query(ctx, `SELECT `+cosmeticItemColumns+`, COALESCE(u.account, ''), COALESCE(pf.nickname, '')
FROM cosmetic_items i LEFT JOIN users u ON u.id = i.creator_user_id
LEFT JOIN user_profiles pf ON pf.user_id = i.creator_user_id
WHERE `+cond+fmt.Sprintf(` ORDER BY i.updated_at DESC, i.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]cosmetic.WorkshopAdminItem, 0, limit)
	for rows.Next() {
		var account, nickname string
		item, err := scanCosmeticItem(rows, &account, &nickname)
		if err != nil {
			return nil, 0, err
		}
		item.Draft = nil
		out = append(out, cosmetic.WorkshopAdminItem{Item: *item, CreatorUserID: item.CreatorUserID,
			CreatorAccount: account, CreatorNickname: nickname})
	}
	return out, total, rows.Err()
}

// SetWorkshopItemStatus 运营下架（archived + 原因）或恢复（active）一件作品。
func (r *Repository) SetWorkshopItemStatus(ctx context.Context, appID, id int64, status, reason string) (*cosmetic.Item, error) {
	return scanCosmeticItem(r.pool.QueryRow(ctx, `UPDATE cosmetic_items i SET status = $3, takedown_reason = $4, updated_at = NOW()
WHERE i.appid = $1 AND i.id = $2 AND i.origin = 'workshop' RETURNING `+cosmeticItemColumns,
		appID, id, status, nullableString(truncateColumn(strings.TrimSpace(reason), cosmetic.MaxTakedownRunes))))
}
