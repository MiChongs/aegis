package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	cardkeydomain "aegis/internal/domain/cardkey"
)

// 卡密核销与登录的判定结果。服务层用 errors.Is 比对后翻成业务错误码 ——
// 仓储层不认识 HTTP 状态码，也不该认识。
var (
	ErrCardKeyNotFound     = errors.New("card key not found")
	ErrCardKeyUsed         = errors.New("card key already used")
	ErrCardKeyDisabled     = errors.New("card key disabled")
	ErrCardKeyExpired      = errors.New("card key expired")
	ErrCardKeyKindMismatch = errors.New("card key kind mismatch")
	ErrCardKeyDeviceLimit  = errors.New("card key device limit reached")
	ErrCardKeyBoundOther   = errors.New("card key bound to another user")
	ErrCardKeyNoLoginCard  = errors.New("no active login card")
	ErrCardKeyBatchMissing = errors.New("card key batch not found")
)

// cardKeyColumns 与 scanCardKey 的读取顺序严格一一对应。
// 提成常量是因为它出现在六处 SQL 里，逐处手抄迟早会漏掉新加的列，
// 而 pgx 的表现是「Scan 位置错位」而不是编译错误。
const cardKeyColumns = `id, appid, batch_id, code, kind, status, bound_user_id, max_devices,
activated_at, expires_at, used_at, disabled_at, disabled_reason, remark, created_at, updated_at`

const cardKeyBatchColumns = `id, appid, name, kind, remark, code_prefix, segments, segment_length,
rewards, max_devices, validity_mode, validity_days, valid_until, total, status, created_by,
created_at, updated_at`

func scanCardKey(row pgx.Row) (*cardkeydomain.Card, error) {
	var item cardkeydomain.Card
	if err := row.Scan(&item.ID, &item.AppID, &item.BatchID, &item.Code, &item.Kind, &item.Status,
		&item.BoundUserID, &item.MaxDevices, &item.ActivatedAt, &item.ExpiresAt, &item.UsedAt,
		&item.DisabledAt, &item.DisabledReason, &item.Remark, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrCardKeyNotFound
		}
		return nil, err
	}
	return &item, nil
}

func scanCardKeyBatch(row pgx.Row) (*cardkeydomain.Batch, error) {
	var item cardkeydomain.Batch
	var rewards []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Kind, &item.Remark, &item.CodePrefix,
		&item.Segments, &item.SegmentLength, &rewards, &item.MaxDevices, &item.ValidityMode,
		&item.ValidityDays, &item.ValidUntil, &item.Total, &item.Status, &item.CreatedBy,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrCardKeyBatchMissing
		}
		return nil, err
	}
	_ = json.Unmarshal(rewards, &item.Rewards)
	if item.Rewards == nil {
		item.Rewards = []cardkeydomain.Reward{}
	}
	return &item, nil
}

// ── 批次 ──

// CreateCardKeyBatch 建批次并一次性写入全部卡面（单事务）。
//
// 批次与卡必须同事务：只建批次会留下一个「总数 500、一张卡都没有」的空壳，
// 而运营看到的是生成成功。批量插入用 CopyFrom —— 五千张卡逐条 INSERT
// 是五千次往返，控制台上的表现是点了生成之后转圈半分钟。
func (r *Repository) CreateCardKeyBatch(ctx context.Context, input cardkeydomain.GenerateInput, codes []string) (*cardkeydomain.Batch, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	rewards, err := json.Marshal(cardkeydomain.NormalizeRewards(input.Rewards))
	if err != nil {
		return nil, err
	}

	batch, err := scanCardKeyBatch(tx.QueryRow(ctx, `INSERT INTO card_key_batches
(appid, name, kind, remark, code_prefix, segments, segment_length, rewards, max_devices,
validity_mode, validity_days, valid_until, total, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING `+cardKeyBatchColumns,
		input.AppID, input.Name, input.Kind, input.Remark, input.CodePrefix, input.Segments,
		input.SegmentLength, rewards, input.MaxDevices, input.ValidityMode, input.ValidityDays,
		input.ValidUntil, len(codes), input.Operator))
	if err != nil {
		return nil, err
	}

	// 固定到期模式在生成时就写死 expires_at；激活即计时的那一档留空，首次使用时才写。
	var expiresAt *time.Time
	if input.ValidityMode == cardkeydomain.ValidityFixedUntil {
		expiresAt = input.ValidUntil
	}

	rows := make([][]any, 0, len(codes))
	for _, code := range codes {
		rows = append(rows, []any{
			input.AppID, batch.ID, code, input.Kind, cardkeydomain.StatusUnused,
			input.MaxDevices, expiresAt,
		})
	}
	if _, err := tx.CopyFrom(ctx,
		pgx.Identifier{"card_keys"},
		[]string{"appid", "batch_id", "code", "kind", "status", "max_devices", "expires_at"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return batch, nil
}

// ListCardKeyBatches 批次列表，随行带上各状态的卡数量。
//
// 汇总跟着列表一起下发而不是另开一个接口：运营看批次列表时唯一关心的就是
// 「发出去多少、用掉多少」，分两次请求会出现「列表已刷新、进度还是上一次」的画面。
func (r *Repository) ListCardKeyBatches(ctx context.Context, appID int64) ([]cardkeydomain.Batch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE appid = $1 ORDER BY id DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Batch, 0, 16)
	for rows.Next() {
		batch, err := scanCardKeyBatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *batch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}

	stats, err := r.cardKeyBatchStats(ctx, appID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if stat, ok := stats[items[index].ID]; ok {
			items[index].Stats = stat
		} else {
			items[index].Stats = &cardkeydomain.BatchStats{}
		}
	}
	return items, nil
}

// cardKeyBatchStats 一次查出全部批次的核销进度。
//
// 「已过期」在 SQL 里算而不是取一档状态：过期是时间比出来的结论，
// 存成状态就需要定时任务去翻，而那个任务停掉的表现是「过期卡还显示可用」。
func (r *Repository) cardKeyBatchStats(ctx context.Context, appID int64) (map[int64]*cardkeydomain.BatchStats, error) {
	rows, err := r.pool.Query(ctx, `SELECT batch_id,
COUNT(*),
COUNT(*) FILTER (WHERE status = 'unused'),
COUNT(*) FILTER (WHERE status = 'active'),
COUNT(*) FILTER (WHERE status = 'used'),
COUNT(*) FILTER (WHERE status = 'disabled'),
COUNT(*) FILTER (WHERE expires_at IS NOT NULL AND expires_at <= NOW())
FROM card_keys WHERE appid = $1 GROUP BY batch_id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]*cardkeydomain.BatchStats)
	for rows.Next() {
		var batchID int64
		stat := &cardkeydomain.BatchStats{}
		if err := rows.Scan(&batchID, &stat.Total, &stat.Unused, &stat.Active,
			&stat.Used, &stat.Disabled, &stat.Expired); err != nil {
			return nil, err
		}
		out[batchID] = stat
	}
	return out, rows.Err()
}

// GetCardKeyBatch 取单个批次。
func (r *Repository) GetCardKeyBatch(ctx context.Context, appID int64, batchID int64) (*cardkeydomain.Batch, error) {
	return scanCardKeyBatch(r.pool.QueryRow(ctx,
		`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE appid = $1 AND id = $2`, appID, batchID))
}

// SetCardKeyBatchStatus 启停批次。
//
// 停用批次**不改单卡状态**：卡还是那些卡，只是这一批整体不再可用。
// 逐张改成 disabled 会让「先停批次、再启用」变成一次不可逆的操作 ——
// 那些本来就已经被作废的卡分不出来了。
func (r *Repository) SetCardKeyBatchStatus(ctx context.Context, appID int64, batchID int64, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE card_key_batches SET status = $3, updated_at = NOW() WHERE appid = $1 AND id = $2`,
		appID, batchID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCardKeyBatchMissing
	}
	return nil
}

// DeleteCardKeyBatch 删除批次（级联删除其下全部卡与核销记录）。
func (r *Repository) DeleteCardKeyBatch(ctx context.Context, appID int64, batchID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM card_key_batches WHERE appid = $1 AND id = $2`, appID, batchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCardKeyBatchMissing
	}
	return nil
}

// ── 单卡 ──

// ListCardKeys 单卡列表（服务端筛选 + 分页）。
func (r *Repository) ListCardKeys(ctx context.Context, query cardkeydomain.CardQuery) (*cardkeydomain.CardPage, error) {
	where := []string{"c.appid = $1"}
	args := []any{query.AppID}

	if query.BatchID > 0 {
		args = append(args, query.BatchID)
		where = append(where, fmt.Sprintf("c.batch_id = $%d", len(args)))
	}
	if query.Kind != "" {
		args = append(args, query.Kind)
		where = append(where, fmt.Sprintf("c.kind = $%d", len(args)))
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("c.bound_user_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(c.code ILIKE $%d OR c.remark ILIKE $%d)", len(args), len(args)))
	}
	// 「已过期」不是一档状态，它在这里被翻译成时间条件。
	switch query.Status {
	case "":
	case "expired":
		where = append(where, "c.expires_at IS NOT NULL AND c.expires_at <= NOW()")
	default:
		args = append(args, query.Status)
		where = append(where, fmt.Sprintf("c.status = $%d", len(args)))
	}

	clause := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM card_keys c `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	args = append(args, limit, (page-1)*limit)

	rows, err := r.pool.Query(ctx, `SELECT c.id, c.appid, c.batch_id, c.code, c.kind, c.status,
c.bound_user_id, c.max_devices, c.activated_at, c.expires_at, c.used_at, c.disabled_at,
c.disabled_reason, c.remark, c.created_at, c.updated_at,
COALESCE(b.name, ''), COALESCE(u.account, ''),
(SELECT COUNT(*) FROM card_key_devices d WHERE d.card_key_id = c.id)
FROM card_keys c
LEFT JOIN card_key_batches b ON b.id = c.batch_id
LEFT JOIN users u ON u.id = c.bound_user_id
`+clause+fmt.Sprintf(" ORDER BY c.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Card, 0, limit)
	for rows.Next() {
		var item cardkeydomain.Card
		if err := rows.Scan(&item.ID, &item.AppID, &item.BatchID, &item.Code, &item.Kind, &item.Status,
			&item.BoundUserID, &item.MaxDevices, &item.ActivatedAt, &item.ExpiresAt, &item.UsedAt,
			&item.DisabledAt, &item.DisabledReason, &item.Remark, &item.CreatedAt, &item.UpdatedAt,
			&item.BatchName, &item.BoundAccount, &item.DeviceCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &cardkeydomain.CardPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

// ListCardKeyCodesForExport 按批次取出全部卡面，供导出使用。
//
// 刻意不复用列表接口：导出的是**这一批**而不是「当前筛选条件」。
// 生成完立刻导出时列表可能还没刷新到那一批，按筛选导出会得到一份不完整的卡。
func (r *Repository) ListCardKeyCodesForExport(ctx context.Context, appID int64, batchID int64) ([]cardkeydomain.Card, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND batch_id = $2 ORDER BY id`,
		appID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Card, 0, 256)
	for rows.Next() {
		card, err := scanCardKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *card)
	}
	return items, rows.Err()
}

// FindCardKeyByCode 按卡面文本查卡。兑换与登录都是从这里进来的。
func (r *Repository) FindCardKeyByCode(ctx context.Context, appID int64, code string) (*cardkeydomain.Card, error) {
	return scanCardKey(r.pool.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND code = $2`, appID, code))
}

// FindCardKeyByID 按主键查卡（管理端入口，顺带校验它属于这个应用）。
func (r *Repository) FindCardKeyByID(ctx context.Context, appID int64, cardID int64) (*cardkeydomain.Card, error) {
	return scanCardKey(r.pool.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND id = $2`, appID, cardID))
}

// FilterExistingCardKeyCodes 返回这批卡面里已经被占用的那些。
//
// 生成时的批外去重。卡面格式可以配得很短（1 段 3 位只有 32768 种组合），
// 而同一个应用里很可能已经存在同样格式的老批次 —— 不查一次的话，
// 撞码会以「CopyFrom 违反唯一约束」的形式让整批生成失败。
func (r *Repository) FilterExistingCardKeyCodes(ctx context.Context, appID int64, codes []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(codes) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT code FROM card_keys WHERE appid = $1 AND code = ANY($2)`, appID, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out[code] = struct{}{}
	}
	return out, rows.Err()
}

// DisableCardKeys 批量作废。
//
// 按选中的 id 列表作废，不提供「按筛选条件批量」：管理员看到的列表与实际执行
// 之间存在时间差（翻页期间有人兑换），按条件批量会误伤没被看过的卡。
//
// 已核销的卡不再改动 —— 那笔权益已经发出去了，把它标成「已作废」
// 会让核销记录与卡状态自相矛盾。
func (r *Repository) DisableCardKeys(ctx context.Context, appID int64, ids []int64, reason string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `UPDATE card_keys
SET status = 'disabled', disabled_at = NOW(), disabled_reason = $3, updated_at = NOW()
WHERE appid = $1 AND id = ANY($2) AND status <> 'used' AND status <> 'disabled'`,
		appID, ids, reason)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RestoreCardKeys 撤销作废，把卡放回未使用。
func (r *Repository) RestoreCardKeys(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `UPDATE card_keys
SET status = CASE WHEN bound_user_id IS NULL THEN 'unused' ELSE 'active' END,
    disabled_at = NULL, disabled_reason = '', updated_at = NOW()
WHERE appid = $1 AND id = ANY($2) AND status = 'disabled'`, appID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 设备绑定 ──

// ListCardKeyDevices 一张卡绑定的设备。
func (r *Repository) ListCardKeyDevices(ctx context.Context, cardKeyID int64) ([]cardkeydomain.Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, card_key_id, device_id, device_name, first_seen_at, last_seen_at, seen_count
FROM card_key_devices WHERE card_key_id = $1 ORDER BY last_seen_at DESC`, cardKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Device, 0, 8)
	for rows.Next() {
		var item cardkeydomain.Device
		if err := rows.Scan(&item.ID, &item.CardKeyID, &item.DeviceID, &item.DeviceName,
			&item.FirstSeenAt, &item.LastSeenAt, &item.SeenCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UnbindCardKeyDevice 解绑一台设备，把名额还回去。
//
// 这是「用户换电脑了」唯一的出口。没有它，一张一机卡在用户重装系统之后
// 就永久报废了 —— 而设备标识会随重装变化。
func (r *Repository) UnbindCardKeyDevice(ctx context.Context, appID int64, cardKeyID int64, deviceID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM card_key_devices WHERE appid = $1 AND card_key_id = $2 AND device_id = $3`,
		appID, cardKeyID, deviceID)
	return err
}

// ── 核销记录 ──

// ListCardKeyRedemptions 核销记录（服务端筛选 + 分页）。
func (r *Repository) ListCardKeyRedemptions(ctx context.Context, query cardkeydomain.RedemptionQuery) (*cardkeydomain.RedemptionPage, error) {
	where := []string{"x.appid = $1"}
	args := []any{query.AppID}

	if query.BatchID > 0 {
		args = append(args, query.BatchID)
		where = append(where, fmt.Sprintf("x.batch_id = $%d", len(args)))
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("x.user_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(x.code ILIKE $%d OR u.account ILIKE $%d)", len(args), len(args)))
	}
	clause := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM card_key_redemptions x LEFT JOIN users u ON u.id = x.user_id `+clause,
		args...).Scan(&total); err != nil {
		return nil, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	args = append(args, limit, (page-1)*limit)

	rows, err := r.pool.Query(ctx, `SELECT x.id, x.appid, x.card_key_id, x.batch_id, x.code, x.user_id,
x.rewards, x.results, x.source, x.device_id, x.client_ip, x.user_agent, x.operator, x.created_at,
COALESCE(u.account, ''), COALESCE(b.name, '')
FROM card_key_redemptions x
LEFT JOIN users u ON u.id = x.user_id
LEFT JOIN card_key_batches b ON b.id = x.batch_id
`+clause+fmt.Sprintf(" ORDER BY x.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Redemption, 0, limit)
	for rows.Next() {
		var item cardkeydomain.Redemption
		var rewards, results []byte
		var deviceID, clientIP, userAgent, operator *string
		if err := rows.Scan(&item.ID, &item.AppID, &item.CardKeyID, &item.BatchID, &item.Code,
			&item.UserID, &rewards, &results, &item.Source, &deviceID, &clientIP, &userAgent,
			&operator, &item.CreatedAt, &item.Account, &item.BatchName); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rewards, &item.Rewards)
		_ = json.Unmarshal(results, &item.Results)
		item.DeviceID = derefString(deviceID)
		item.ClientIP = derefString(clientIP)
		item.UserAgent = derefString(userAgent)
		item.Operator = derefString(operator)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &cardkeydomain.RedemptionPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
