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

	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 积分商城「赠好友」：兑换人付积分，权益发给他选的人。规则见 docs/points-store-gift.md。
//
// 一笔赠送 = 一笔兑换记录（付款人的账：扣积分、限兑、库存都照兑换算）+ 一条赠送记录（发给谁、风控结论、
// 审核与撤销）。三种结局都在一个事务里完成，不做事务外补偿：
//
//   - 放行：锁双方 → 判定 → 扣库存 → 落兑换与赠送记录 → 扣积分 → 给接收人发放 → 回写结果
//   - 送审核：同上但不发放，兑换记为待处理、赠送记为审核中；通过时在一个事务里发放，驳回时原路退回
//   - 拒绝：整笔回滚（没扣积分），由服务层另记一条 blocked 留痕

var (
	ErrPointsStoreGiftNotFound = errors.New("points store gift not found")
	// ErrPointsStoreGiftState 这条赠送当前的状态不允许这次操作（只有审核中的能通过或驳回，只有已送达的能撤销）
	ErrPointsStoreGiftState = errors.New("points store gift state does not allow this")
)

// PointsStoreGiftError 赠送被拒：硬性条件不满足或风控拒绝。Verdict / Facts 在风控拒绝时带出，服务层据此留痕。
type PointsStoreGiftError struct {
	Reason  string
	Message string
	Verdict *pointstore.GiftVerdict
	Facts   *pointstore.GiftFacts
	// Item 被拒时的商品快照（留痕用）
	Item *pointstore.Item
}

func (e *PointsStoreGiftError) Error() string { return "points store gift refused: " + e.Reason }

func giftRefused(reason string) error {
	return &PointsStoreGiftError{Reason: reason, Message: pointstore.GiftReasonMessage(reason)}
}

// ── 策略 ──

// GetPointsStoreGiftPolicy 读赠送风控策略；没保存过时返回推荐默认值（configured=false）。
func (r *Repository) GetPointsStoreGiftPolicy(ctx context.Context, appID int64) (*pointstore.GiftPolicy, error) {
	var raw []byte
	var updatedBy string
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT policy, COALESCE(updated_by, ''), updated_at FROM points_store_gift_policies WHERE appid = $1`,
		appID).Scan(&raw, &updatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		policy := pointstore.DefaultGiftPolicy(appID)
		return &policy, nil
	}
	if err != nil {
		return nil, err
	}
	policy := pointstore.GiftPolicy{Enabled: true}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &policy); err != nil {
			return nil, fmt.Errorf("decode gift policy: %w", err)
		}
	}
	policy = pointstore.NormalizeGiftPolicy(policy)
	policy.AppID, policy.Configured, policy.UpdatedBy, policy.UpdatedAt = appID, true, updatedBy, updatedAt
	return &policy, nil
}

// SavePointsStoreGiftPolicy 保存策略（已由服务层清洗与校验）。
func (r *Repository) SavePointsStoreGiftPolicy(ctx context.Context, policy pointstore.GiftPolicy, operator string) (*pointstore.GiftPolicy, error) {
	raw, _ := json.Marshal(struct {
		Enabled bool                  `json:"enabled"`
		Rules   []pointstore.GiftRule `json:"rules"`
	}{policy.Enabled, policy.Rules})
	if _, err := r.pool.Exec(ctx, `INSERT INTO points_store_gift_policies (appid, policy, updated_by)
VALUES ($1, $2, $3)
ON CONFLICT (appid) DO UPDATE SET policy = EXCLUDED.policy, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		policy.AppID, raw, nullableString(operator)); err != nil {
		return nil, err
	}
	return r.GetPointsStoreGiftPolicy(ctx, policy.AppID)
}

// ── 账号 ──

// PointsStoreGiftAccount 赠送判定所需的一方账号信息。
type PointsStoreGiftAccount struct {
	pointstore.GiftUser
	CreatedAt time.Time
	// Inactive 停用、封禁（应用级或全站连带）或正在注销：不能接收也不能赠送
	Inactive bool
}

const giftAccountSQL = `SELECT u.id, COALESCE(u.public_id, 0), COALESCE(u.number, 0), u.account,
       COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), u.created_at,
       (NOT u.enabled OR (u.disabled_end_time IS NOT NULL AND u.disabled_end_time > NOW())
        OR EXISTS (SELECT 1 FROM user_account_bans b WHERE b.appid = u.appid AND b.user_id = u.id AND b.status = 'active'
                   AND b.start_at <= NOW() AND (b.end_at IS NULL OR b.end_at > NOW()))
        OR EXISTS (SELECT 1 FROM account_deletion_requests d WHERE d.appid = u.appid AND d.user_id = u.id
                   AND d.status IN ('reviewing','scheduled','processing'))) AS inactive
FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND u.id = $2`

func giftAccountQ(ctx context.Context, q queryExecutor, appID, userID int64) (*PointsStoreGiftAccount, error) {
	var account PointsStoreGiftAccount
	err := q.QueryRow(ctx, giftAccountSQL, appID, userID).Scan(&account.UserID, &account.PublicID, &account.Number,
		&account.Account, &account.Nickname, &account.RawAvatar, &account.CreatedAt, &account.Inactive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// PointsStoreGiftAccount 取一方账号（ErrUserNotFound 表示不是这个应用的用户）。
func (r *Repository) PointsStoreGiftAccount(ctx context.Context, appID, userID int64) (*PointsStoreGiftAccount, error) {
	return giftAccountQ(ctx, r.pool, appID, userID)
}

// FindPointsStoreGiftRecipient 按用户号码或对外用户 ID 找人；找不到返回 0。
//
// 对外 ID 是 16 位、号码最多 12 位，两者不会撞：客户端从候选列表里选的人带对外 ID，手输的是号码。
func (r *Repository) FindPointsStoreGiftRecipient(ctx context.Context, appID int64, key string) (int64, error) {
	key = strings.TrimSpace(key)
	value, err := strconv.ParseInt(key, 10, 64)
	if err != nil || value <= 0 {
		return 0, nil
	}
	column := "number"
	if len(key) >= 16 {
		column = "public_id"
	}
	var id int64
	err = r.pool.QueryRow(ctx, `SELECT id FROM users WHERE appid = $1 AND `+column+` = $2`, appID, value).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// PointsStoreGiftCandidates 选人时的候选：最近送过的人、最近送过我的人、邀请我的人、我邀请的人，
// 按最近一次互动排序、同一个人只出现一次。停用、封禁、注销中的账号不列。
func (r *Repository) PointsStoreGiftCandidates(ctx context.Context, appID, userID int64, limit int) ([]pointstore.GiftCandidate, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `WITH c AS (
    SELECT recipient_id AS uid, 'recent_sent' AS relation, MAX(created_at) AS at, 1 AS prio
    FROM points_store_gifts WHERE appid = $1 AND sender_id = $2 AND recipient_id IS NOT NULL
      AND status IN ('delivered','reviewing') AND created_at >= NOW() - INTERVAL '180 days'
    GROUP BY recipient_id
    UNION ALL
    SELECT sender_id, 'recent_received', MAX(created_at), 2
    FROM points_store_gifts WHERE appid = $1 AND recipient_id = $2 AND sender_id IS NOT NULL
      AND status = 'delivered' AND created_at >= NOW() - INTERVAL '180 days'
    GROUP BY sender_id
    UNION ALL
    SELECT inviter_id, 'inviter', created_at, 3 FROM invite_relations
    WHERE appid = $1 AND invitee_id = $2 AND inviter_id IS NOT NULL
    UNION ALL
    SELECT invitee_id, 'invitee', MAX(created_at), 4 FROM invite_relations
    WHERE appid = $1 AND inviter_id = $2
    GROUP BY invitee_id
), ranked AS (
    SELECT DISTINCT ON (uid) uid, relation, at FROM c WHERE uid <> $2 ORDER BY uid, prio, at DESC
)
SELECT u.id, COALESCE(u.public_id, 0), COALESCE(u.number, 0), COALESCE(p.nickname, ''), COALESCE(p.avatar, ''),
       ranked.relation, ranked.at
FROM ranked JOIN users u ON u.id = ranked.uid AND u.appid = $1
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.enabled AND (u.disabled_end_time IS NULL OR u.disabled_end_time <= NOW())
  AND NOT EXISTS (SELECT 1 FROM user_account_bans b WHERE b.appid = u.appid AND b.user_id = u.id AND b.status = 'active'
                  AND b.start_at <= NOW() AND (b.end_at IS NULL OR b.end_at > NOW()))
ORDER BY ranked.at DESC
LIMIT $3`, appID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]pointstore.GiftCandidate, 0, limit)
	for rows.Next() {
		var item pointstore.GiftCandidate
		var at time.Time
		if err := rows.Scan(&item.UserID, &item.PublicID, &item.Number, &item.Nickname, &item.RawAvatar, &item.Relation, &at); err != nil {
			return nil, err
		}
		item.LastAt = &at
		out = append(out, item)
	}
	return out, rows.Err()
}

// ── 风控事实 ──

// giftCountedStatuses 计入当日次数、频次的赠送：驳回的退了积分、拦截的没扣积分，都不算；撤销的算（惩罚性的）。
const giftCountedStatuses = `('delivered','reviewing','revoked')`

// giftDeviceSetSQL 一个账号用过的真实设备（$1 appid、$2 user）：登录设备台账（去掉 UA 摘要与单次会话两种兜底键，
// 同一款机型的 UA 摘要相同，拿它比会把陌生人当成同一台）、注册台账、近 90 天成功登录的设备标识。
const giftDeviceSetSQL = `SELECT lower(device_key) AS d FROM user_login_devices
    WHERE appid = $1 AND user_id = %s AND device_key NOT LIKE 'ua:%%' AND device_key NOT LIKE 's:%%'
    UNION SELECT lower(device_id) FROM register_ip_ledger WHERE appid = $1 AND user_id = %s AND device_id <> ''
    UNION SELECT lower(device_id) FROM login_audit_logs
    WHERE appid = $1 AND user_id = %s AND status = 'success' AND device_id IS NOT NULL AND device_id <> ''
      AND created_at >= NOW() - INTERVAL '90 days'`

// giftNetworkSetSQL 一个账号在时间窗内用过的地址（$1 appid、$4 窗口起点）：成功登录、登录设备的最近地址、注册地址。
// IPv6 按 /64 比（运营商一户一段，隐私扩展地址一天换好几次）；内网与回环地址不算（拿不到真实地址时的兜底值）。
const giftNetworkSetSQL = `SELECT CASE WHEN family(ip) = 6 THEN network(set_masklen(ip, 64))::text ELSE host(ip) END AS n
FROM (
    SELECT login_ip AS ip FROM login_audit_logs
    WHERE appid = $1 AND user_id = %s AND status = 'success' AND login_ip IS NOT NULL AND created_at >= $4
    UNION SELECT CASE WHEN last_ip ~ '^[0-9a-fA-F:.]+$' THEN last_ip::inet END FROM user_login_devices
    WHERE appid = $1 AND user_id = %s AND last_ip <> '' AND last_seen_at >= $4
    UNION SELECT ip FROM register_ip_ledger WHERE appid = $1 AND user_id = %s
) s
WHERE ip IS NOT NULL AND NOT (ip << '10.0.0.0/8'::inet OR ip << '172.16.0.0/12'::inet OR ip << '192.168.0.0/16'::inet
    OR ip << '127.0.0.0/8'::inet OR ip << '100.64.0.0/10'::inet OR ip = '::1'::inet OR ip << 'fc00::/7'::inet
    OR ip << 'fe80::/10'::inet)`

// pointsStoreGiftFactsQ 在锁住双方的事务里取齐判定事实。
func pointsStoreGiftFactsQ(ctx context.Context, q queryExecutor, appID int64, sender, recipient *PointsStoreGiftAccount,
	cost int64, policy pointstore.GiftPolicy, dayStart, now time.Time) (pointstore.GiftFacts, error) {
	facts := pointstore.GiftFacts{Now: now, Cost: cost, SharedDevices: []string{}, SharedNetworks: []string{}}
	facts.SenderAgeDays = int64(now.Sub(sender.CreatedAt) / (24 * time.Hour))
	facts.RecipientAgeDays = int64(now.Sub(recipient.CreatedAt) / (24 * time.Hour))

	if policy.Rule(pointstore.GiftRuleSameDevice).Enabled {
		rows, err := q.Query(ctx, `SELECT d FROM (`+fmt.Sprintf(giftDeviceSetSQL, "$2", "$2", "$2")+`) a
INTERSECT SELECT d FROM (`+fmt.Sprintf(giftDeviceSetSQL, "$3", "$3", "$3")+`) b LIMIT 5`, appID, sender.UserID, recipient.UserID)
		if err != nil {
			return facts, err
		}
		for rows.Next() {
			var device string
			if err := rows.Scan(&device); err != nil {
				rows.Close()
				return facts, err
			}
			facts.SharedDevices = append(facts.SharedDevices, pointstore.MaskDevice(device))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return facts, err
		}
	}
	if rule := policy.Rule(pointstore.GiftRuleSameNetwork); rule.Enabled {
		since := now.Add(-time.Duration(rule.WindowHours) * time.Hour)
		rows, err := q.Query(ctx, `SELECT n FROM (`+fmt.Sprintf(giftNetworkSetSQL, "$2", "$2", "$2")+`) a
INTERSECT SELECT n FROM (`+fmt.Sprintf(giftNetworkSetSQL, "$3", "$3", "$3")+`) b LIMIT 5`,
			appID, sender.UserID, recipient.UserID, since)
		if err != nil {
			return facts, err
		}
		for rows.Next() {
			var network string
			if err := rows.Scan(&network); err != nil {
				rows.Close()
				return facts, err
			}
			facts.SharedNetworks = append(facts.SharedNetworks, network)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return facts, err
		}
	}

	pairSince := now.Add(-time.Duration(maxInt64(policy.Rule(pointstore.GiftRulePairFrequency).WindowHours, 1)) * time.Hour)
	mutualSince := now.Add(-time.Duration(maxInt64(policy.Rule(pointstore.GiftRuleMutual).WindowHours, 1)) * time.Hour)
	err := q.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE sender_id = $2 AND created_at >= $4),
  COALESCE(SUM(cost) FILTER (WHERE sender_id = $2 AND created_at >= $4), 0),
  COUNT(*) FILTER (WHERE recipient_id = $3 AND created_at >= $4),
  COUNT(*) FILTER (WHERE sender_id = $2 AND recipient_id = $3 AND created_at >= $5),
  COUNT(*) FILTER (WHERE sender_id = $3 AND recipient_id = $2 AND created_at >= $6)
FROM points_store_gifts
WHERE appid = $1 AND status IN `+giftCountedStatuses+` AND (sender_id IN ($2, $3) OR recipient_id IN ($2, $3))
  AND created_at >= LEAST($4, $5, $6)`,
		appID, sender.UserID, recipient.UserID, dayStart, pairSince, mutualSince).
		Scan(&facts.SenderToday, &facts.SenderTodayPoints, &facts.RecipientToday, &facts.PairRecent, &facts.MutualRecent)
	return facts, err
}

// recipientHoldingsQ 接收人此刻已经永久拥有什么：判「对方已拥有」用。
func recipientHoldingsQ(ctx context.Context, tx pgx.Tx, appID, userID int64) (vipdomain.Holdings, []int64, error) {
	entitlement, err := entitlementFactsTx(ctx, tx, appID, userID, nil)
	if err != nil {
		return vipdomain.Holdings{}, nil, err
	}
	holdings := vipdomain.HoldingsOf(*entitlement, time.Now())
	rows, err := tx.Query(ctx, `SELECT DISTINCT cosmetic_id FROM user_cosmetics
WHERE appid = $1 AND user_id = $2 AND revoked_at IS NULL AND ends_at IS NULL`, appID, userID)
	if err != nil {
		return holdings, nil, err
	}
	defer rows.Close()
	permanent := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return holdings, nil, err
		}
		permanent = append(permanent, id)
	}
	return holdings, permanent, rows.Err()
}

// lockGiftUsersTx 按主键从小到大锁住双方的用户行：A 送 B 与 B 送 A 同时发生时排队，不会互相等死。
// 返回赠送人的积分与经验。
func lockGiftUsersTx(ctx context.Context, tx pgx.Tx, appID, senderID, recipientID int64, lock bool) (integral, experience int64, err error) {
	clause := ""
	if lock {
		clause = " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, `SELECT id, integral, experience FROM users WHERE appid = $1 AND id = ANY($2) ORDER BY id`+clause,
		appID, []int64{senderID, recipientID})
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var id, in, exp int64
		if err := rows.Scan(&id, &in, &exp); err != nil {
			return 0, 0, err
		}
		seen++
		if id == senderID {
			integral, experience = in, exp
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if seen < 2 {
		return 0, 0, ErrUserNotFound
	}
	return integral, experience, nil
}

// ── 赠送 ──

const pointsStoreGiftColumns = `g.id, g.gift_no, g.appid, g.redemption_id, COALESCE(rd.redemption_no, ''),
COALESCE(g.sender_id, 0), COALESCE(g.recipient_id, 0), g.item_id, g.item_title, g.cost, g.rewards, g.results, g.message,
g.status, g.risk_action, g.risk_hits, g.risk_facts, COALESCE(g.risk_decision_id, ''), COALESCE(g.request_id, ''),
COALESCE(g.device_id, ''), COALESCE(g.client_ip, ''), COALESCE(g.review_note, ''), COALESCE(g.reviewed_by, ''), g.reviewed_at,
COALESCE(g.revoke_note, ''), COALESCE(g.revoked_by, ''), g.revoked_at, g.revocation, COALESCE(g.refund_transaction_no, ''),
g.created_at, g.updated_at,
COALESCE(su.public_id, 0), COALESCE(su.number, 0), COALESCE(su.account, ''), COALESCE(sp.nickname, ''), COALESCE(sp.avatar, ''),
COALESCE(ru.public_id, 0), COALESCE(ru.number, 0), COALESCE(ru.account, ''), COALESCE(rp.nickname, ''), COALESCE(rp.avatar, '')`

const pointsStoreGiftFrom = ` FROM points_store_gifts g
LEFT JOIN points_store_redemptions rd ON rd.id = g.redemption_id
LEFT JOIN users su ON su.id = g.sender_id LEFT JOIN user_profiles sp ON sp.user_id = g.sender_id
LEFT JOIN users ru ON ru.id = g.recipient_id LEFT JOIN user_profiles rp ON rp.user_id = g.recipient_id`

func scanPointsStoreGift(row interface{ Scan(dest ...any) error }) (*pointstore.Gift, error) {
	var gift pointstore.Gift
	var rewards, results, hits, facts, revocation []byte
	err := row.Scan(&gift.ID, &gift.GiftNo, &gift.AppID, &gift.RedemptionID, &gift.RedemptionNo,
		&gift.Sender.UserID, &gift.Recipient.UserID, &gift.ItemID, &gift.ItemTitle, &gift.Cost, &rewards, &results, &gift.Message,
		&gift.Status, &gift.RiskAction, &hits, &facts, &gift.DecisionID, &gift.RequestID,
		&gift.DeviceID, &gift.ClientIP, &gift.ReviewNote, &gift.ReviewedBy, &gift.ReviewedAt,
		&gift.RevokeNote, &gift.RevokedBy, &gift.RevokedAt, &revocation, &gift.RefundTransactionNo,
		&gift.CreatedAt, &gift.UpdatedAt,
		&gift.Sender.PublicID, &gift.Sender.Number, &gift.Sender.Account, &gift.Sender.Nickname, &gift.Sender.RawAvatar,
		&gift.Recipient.PublicID, &gift.Recipient.Number, &gift.Recipient.Account, &gift.Recipient.Nickname, &gift.Recipient.RawAvatar)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPointsStoreGiftNotFound
		}
		return nil, err
	}
	gift.Rewards, gift.Results, gift.RiskHits = []pointstore.Reward{}, []pointstore.RewardResult{}, []pointstore.GiftHit{}
	for _, part := range []struct {
		raw  []byte
		dest any
	}{{rewards, &gift.Rewards}, {results, &gift.Results}, {hits, &gift.RiskHits}} {
		if len(part.raw) > 0 {
			if err := json.Unmarshal(part.raw, part.dest); err != nil {
				return nil, fmt.Errorf("decode gift: %w", err)
			}
		}
	}
	if len(facts) > 2 {
		var decoded pointstore.GiftFacts
		if err := json.Unmarshal(facts, &decoded); err == nil {
			gift.RiskFacts = &decoded
		}
	}
	if len(revocation) > 0 {
		_ = json.Unmarshal(revocation, &gift.Revocation)
	}
	return &gift, nil
}

func getPointsStoreGiftQ(ctx context.Context, q queryExecutor, appID, giftID int64, lock bool) (*pointstore.Gift, error) {
	clause := ""
	if lock {
		clause = " FOR UPDATE OF g"
	}
	return scanPointsStoreGift(q.QueryRow(ctx, `SELECT `+pointsStoreGiftColumns+pointsStoreGiftFrom+`
WHERE g.id = $1 AND g.appid = $2`+clause, giftID, appID))
}

// GetPointsStoreGift 取一条赠送。
func (r *Repository) GetPointsStoreGift(ctx context.Context, appID, giftID int64) (*pointstore.Gift, error) {
	return getPointsStoreGiftQ(ctx, r.pool, appID, giftID, false)
}

// GiftPointsStoreItem 赠送一件商品（单事务）。dryRun 只判定不落库（提交前的核对），不加行锁。
//
// 顺序：锁双方用户行（主键升序）→ 幂等键回放 → 锁商品行 → 接收人状态 → 取齐双方事实并做可兑换判定
// （「已拥有」按接收人算）→ 补货 → 风控事实与判定 → 扣库存 → 落兑换记录与赠送记录 → 扣积分 →
// 放行的给接收人逐项发放（任何一项发不出去整笔回滚）→ 回写结果。
//
// 风控事实（当日次数、同一对人的频次）在锁住双方之后读：同一个赠送人的并发请求在用户行上排队，
// 读到的是提交前的真实次数，不会两笔都读到「今天还能送一次」。
func (r *Repository) GiftPointsStoreItem(ctx context.Context, input pointstore.GiftInput, dryRun bool) (*pointstore.GiftResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	lock := !dryRun

	integral, experience, err := lockGiftUsersTx(ctx, tx, input.AppID, input.SenderID, input.RecipientID, lock)
	if err != nil {
		return nil, err
	}

	if input.RequestID != "" && !dryRun {
		existing, err := scanPointsStoreGift(tx.QueryRow(ctx, `SELECT `+pointsStoreGiftColumns+pointsStoreGiftFrom+`
WHERE rd.appid = $1 AND rd.user_id = $2 AND rd.request_id = $3`, input.AppID, input.SenderID, input.RequestID))
		if err == nil {
			return &pointstore.GiftResult{Gift: *existing, Balance: integral, Replayed: true}, nil
		}
		if !errors.Is(err, ErrPointsStoreGiftNotFound) {
			return nil, err
		}
	}

	itemClause := ""
	if lock {
		itemClause = " FOR UPDATE"
	}
	item, err := scanPointsStoreItem(tx.QueryRow(ctx, `SELECT `+pointsStoreItemColumns+`
FROM points_store_items i WHERE i.id = $1 AND i.appid = $2`+itemClause, input.ItemID, input.AppID))
	if err != nil {
		return nil, err
	}
	if item.Delivery != pointstore.DeliveryGift {
		return nil, giftRefused(pointstore.GiftReasonNotGiftItem)
	}

	sender, err := giftAccountQ(ctx, tx, input.AppID, input.SenderID)
	if err != nil {
		return nil, err
	}
	recipient, err := giftAccountQ(ctx, tx, input.AppID, input.RecipientID)
	if err != nil {
		return nil, err
	}
	if recipient.Inactive {
		return nil, giftRefused(pointstore.GiftReasonRecipientInactive)
	}

	entitlementFacts, err := entitlementFactsTx(ctx, tx, input.AppID, input.SenderID, nil)
	if err != nil {
		return nil, err
	}
	facts, err := r.assemblePointsStoreFacts(ctx, tx, input.AppID, input.SenderID, integral, experience, entitlementFacts)
	if err != nil {
		return nil, err
	}
	// 「已拥有」问的是接收人：赠送人自己是不是永久会员与这份礼物送不送得出去无关
	facts.Holdings, facts.PermanentCosmetics, err = recipientHoldingsQ(ctx, tx, input.AppID, input.RecipientID)
	if err != nil {
		return nil, err
	}
	usage, err := pointsStoreItemUsageTx(ctx, tx, input.AppID, input.SenderID, item.ID, input.Windows)
	if err != nil {
		return nil, err
	}
	if item.GlobalLimited() {
		if usage.Global, err = pointsStoreItemGlobalUsageTx(ctx, tx, input.AppID, item.ID, input.Windows); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	clock := pointstore.Clock{Now: now, Location: input.Location, Windows: input.Windows}
	if clock.Location == nil {
		clock.Location = time.Local
	}
	if stock, rolled := item.CurrentStock(input.Windows); rolled {
		windowStart := input.Windows.Start(item.RestockPeriod)
		item.Stock, item.StockWindowStart = stock, &windowStart
		if lock {
			if _, err := tx.Exec(ctx, `UPDATE points_store_items SET stock = $2, stock_window_start = $3 WHERE id = $1`,
				item.ID, stock, windowStart); err != nil {
				return nil, err
			}
		}
	}
	if availability := pointstore.Evaluate(*item, *facts, usage, clock); !availability.Redeemable {
		if availability.Reason == pointstore.ReasonOwned {
			return nil, giftRefused(pointstore.GiftReasonRecipientOwned)
		}
		return nil, &PointsStoreUnavailableError{Availability: availability}
	}

	giftFacts, err := pointsStoreGiftFactsQ(ctx, tx, input.AppID, sender, recipient, item.Cost, input.Policy, input.Windows.Day, now)
	if err != nil {
		return nil, err
	}
	giftFacts.Platform = input.Platform
	verdict := pointstore.EvaluateGift(input.Policy, giftFacts)
	if hit := verdict.RejectHit(); hit != nil {
		return nil, &PointsStoreGiftError{Reason: pointstore.GiftHitReason(hit.Key), Message: hit.Message,
			Verdict: &verdict, Facts: &giftFacts, Item: item}
	}
	rewards := pointstore.NormalizeRewards(item.Rewards)
	if dryRun {
		preview := pointstore.Gift{ItemTitle: item.Title, Cost: item.Cost, Rewards: rewards, RiskAction: verdict.Action}
		return &pointstore.GiftResult{Gift: preview, Balance: integral}, nil
	}

	if _, err := tx.Exec(ctx, `UPDATE points_store_items
SET stock = CASE WHEN stock IS NULL THEN NULL ELSE stock - 1 END, sold_count = sold_count + 1, updated_at = NOW()
WHERE id = $1`, item.ID); err != nil {
		return nil, err
	}

	review := verdict.Action == pointstore.GiftActionReview
	redemptionStatus, giftStatus := pointstore.RedemptionCompleted, pointstore.GiftDelivered
	if review {
		redemptionStatus, giftStatus = pointstore.RedemptionPending, pointstore.GiftReviewing
	}
	rewardsJSON, _ := json.Marshal(rewards)
	redemption, err := scanPointsStoreRedemption(tx.QueryRow(ctx, `INSERT INTO points_store_redemptions AS r
(redemption_no, appid, user_id, item_id, item_title, cost, rewards, status, request_id, device_id, client_ip, user_agent)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING `+pointsStoreRedemptionColumns,
		generateTransactionNo("PSR"), input.AppID, input.SenderID, item.ID, item.Title, item.Cost, rewardsJSON, redemptionStatus,
		nullableString(input.RequestID), nullableString(input.DeviceID), nullableString(input.ClientIP),
		nullableString(truncateColumn(input.UserAgent, 256))))
	if err != nil {
		return nil, err
	}

	giftNo := generateTransactionNo("PSG")
	hitsJSON, _ := json.Marshal(verdict.Hits)
	factsJSON, _ := json.Marshal(giftFacts)
	decisionID := ""
	if input.Platform != nil {
		decisionID = input.Platform.DecisionID
	}
	var giftID int64
	if err := tx.QueryRow(ctx, `INSERT INTO points_store_gifts
(gift_no, appid, redemption_id, sender_id, recipient_id, item_id, item_title, cost, rewards, message, status,
 risk_action, risk_hits, risk_facts, risk_decision_id, request_id, device_id, client_ip)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`,
		giftNo, input.AppID, redemption.ID, input.SenderID, input.RecipientID, item.ID, item.Title, item.Cost, rewardsJSON,
		input.Message, giftStatus, verdict.Action, hitsJSON, factsJSON, nullableString(decisionID),
		nullableString(input.RequestID), nullableString(input.DeviceID), nullableString(input.ClientIP)).Scan(&giftID); err != nil {
		return nil, err
	}

	redemptionID := redemption.ID
	_, balance, integralTxnNo, err := applyIntegralChangeTx(ctx, tx, input.SenderID, input.AppID, -item.Cost,
		"consume", pointstore.IntegralCategoryRedeem, "积分赠送", item.Title+" · 送给 "+giftUserLabel(recipient.GiftUser),
		"points_store", &redemptionID,
		map[string]any{"redemptionNo": redemption.RedemptionNo, "itemId": item.ID, "giftNo": giftNo, "recipientId": input.RecipientID})
	if err != nil {
		return nil, err
	}

	results := []pointstore.RewardResult{}
	if !review {
		results, err = r.grantPointsStoreRewardsTx(ctx, tx, redemption, input.RecipientID, rewards, facts,
			map[string]any{"giftNo": giftNo, "senderId": input.SenderID})
		if err != nil {
			return nil, err
		}
	} else {
		for _, reward := range rewards {
			described := pointstore.DescribeReward(reward, facts.Plans, facts.Features)
			results = append(results, pointstore.RewardResult{Type: reward.Type, Label: described.Label, Detail: described.Detail, Pending: true})
		}
	}
	resultsJSON, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE points_store_redemptions SET results = $2, integral_transaction_no = $3 WHERE id = $1`,
		redemption.ID, resultsJSON, integralTxnNo); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE points_store_gifts SET results = $2 WHERE id = $1`, giftID, resultsJSON); err != nil {
		return nil, err
	}
	gift, err := getPointsStoreGiftQ(ctx, tx, input.AppID, giftID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &pointstore.GiftResult{Gift: *gift, Balance: balance}, nil
}

func giftUserLabel(user pointstore.GiftUser) string {
	switch {
	case strings.TrimSpace(user.Nickname) != "":
		return user.Nickname
	case user.Number > 0:
		return strconv.FormatInt(user.Number, 10)
	}
	return user.Account
}

// RecordPointsStoreGiftBlock 记一条被风控拦下的赠送（没扣积分，只留痕）：管理员据此看谁在试、试了什么。
// 拦截发生在赠送事务回滚之后，这里单独写一行，与积分账无关，不存在补偿的问题。
func (r *Repository) RecordPointsStoreGiftBlock(ctx context.Context, input pointstore.GiftInput, item *pointstore.Item,
	verdict pointstore.GiftVerdict, facts pointstore.GiftFacts) error {
	if item == nil {
		return nil
	}
	rewardsJSON, _ := json.Marshal(pointstore.NormalizeRewards(item.Rewards))
	hitsJSON, _ := json.Marshal(verdict.Hits)
	factsJSON, _ := json.Marshal(facts)
	decisionID := ""
	if input.Platform != nil {
		decisionID = input.Platform.DecisionID
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO points_store_gifts
(gift_no, appid, sender_id, recipient_id, item_id, item_title, cost, rewards, message, status,
 risk_action, risk_hits, risk_facts, risk_decision_id, request_id, device_id, client_ip)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'blocked','reject',$10,$11,$12,$13,$14,$15)`,
		generateTransactionNo("PSG"), input.AppID, input.SenderID, input.RecipientID, item.ID, item.Title, item.Cost, rewardsJSON,
		input.Message, hitsJSON, factsJSON, nullableString(decisionID), nullableString(input.RequestID),
		nullableString(input.DeviceID), nullableString(input.ClientIP))
	return err
}

// ── 审核 ──

// lockGiftForHandlingTx 先不加锁读出双方，按主键升序锁住双方用户行，再锁住赠送行并复核状态。
// 与提交赠送同一个加锁顺序（用户 → 其它），审核与新的赠送不会互相等死。
func lockGiftForHandlingTx(ctx context.Context, tx pgx.Tx, appID, giftID int64, want string) (*pointstore.Gift, error) {
	peek, err := getPointsStoreGiftQ(ctx, tx, appID, giftID, false)
	if err != nil {
		return nil, err
	}
	if peek.Status != want || peek.RedemptionID == nil {
		// 拦截留痕没有兑换记录，没有可处理的东西
		return nil, ErrPointsStoreGiftState
	}
	// 一方删号后这一侧为 0：只锁还在的人。通过要发给接收人、驳回要退给赠送人，缺了谁由后面的账本操作报 ErrUserNotFound
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE appid = $1 AND id = ANY($2) ORDER BY id FOR UPDATE`,
		appID, []int64{peek.Sender.UserID, peek.Recipient.UserID}); err != nil {
		return nil, err
	}
	gift, err := getPointsStoreGiftQ(ctx, tx, appID, giftID, true)
	if err != nil {
		return nil, err
	}
	if gift.Status != want {
		return nil, ErrPointsStoreGiftState
	}
	return gift, nil
}

// ApprovePointsStoreGift 审核通过：在一个事务里给接收人逐项发放。
//
// 发放前按此刻复核接收人：审核期间对方被封禁、注销，或已经拥有了这份权益，都不能再发 —— 报错让管理员改为驳回
// （驳回会把积分退给赠送人）。
func (r *Repository) ApprovePointsStoreGift(ctx context.Context, input pointstore.GiftReviewInput) (*pointstore.Gift, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	gift, err := lockGiftForHandlingTx(ctx, tx, input.AppID, input.GiftID, pointstore.GiftReviewing)
	if err != nil {
		return nil, err
	}
	redemption, err := scanPointsStoreRedemption(tx.QueryRow(ctx, `SELECT `+pointsStoreRedemptionColumns+`
FROM points_store_redemptions r WHERE r.id = $1 AND r.appid = $2 FOR UPDATE`, *gift.RedemptionID, input.AppID))
	if err != nil {
		return nil, err
	}
	recipientID := gift.Recipient.UserID
	recipient, err := giftAccountQ(ctx, tx, input.AppID, recipientID)
	if err != nil {
		return nil, err
	}
	if recipient.Inactive {
		return nil, giftRefused(pointstore.GiftReasonRecipientInactive)
	}
	var integral, experience int64
	if err := tx.QueryRow(ctx, `SELECT integral, experience FROM users WHERE id = $1 AND appid = $2`, recipientID, input.AppID).
		Scan(&integral, &experience); err != nil {
		return nil, err
	}
	entitlement, err := entitlementFactsTx(ctx, tx, input.AppID, recipientID, nil)
	if err != nil {
		return nil, err
	}
	facts, err := r.assemblePointsStoreFacts(ctx, tx, input.AppID, recipientID, integral, experience, entitlement)
	if err != nil {
		return nil, err
	}
	if pointstore.Owned(gift.Rewards, *facts) {
		return nil, giftRefused(pointstore.GiftReasonRecipientOwned)
	}
	results, err := r.grantPointsStoreRewardsTx(ctx, tx, redemption, recipientID, gift.Rewards, facts,
		map[string]any{"giftNo": gift.GiftNo, "senderId": gift.Sender.UserID})
	if err != nil {
		return nil, err
	}
	resultsJSON, _ := json.Marshal(results)
	note := nullableString(strings.TrimSpace(input.Note))
	if _, err := tx.Exec(ctx, `UPDATE points_store_redemptions SET status = 'completed', results = $2, note = $3,
handled_by = $4, handled_at = NOW(), updated_at = NOW() WHERE id = $1`,
		redemption.ID, resultsJSON, note, nullableString(input.Operator)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE points_store_gifts SET status = 'delivered', results = $2, review_note = $3,
reviewed_by = $4, reviewed_at = NOW(), updated_at = NOW() WHERE id = $1`,
		gift.ID, resultsJSON, note, nullableString(input.Operator)); err != nil {
		return nil, err
	}
	updated, err := getPointsStoreGiftQ(ctx, tx, input.AppID, gift.ID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return updated, nil
}

// RejectPointsStoreGift 审核驳回：积分原路退回赠送人、库存加回，兑换记为已取消（单事务）。
func (r *Repository) RejectPointsStoreGift(ctx context.Context, input pointstore.GiftReviewInput) (*pointstore.Gift, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	gift, err := lockGiftForHandlingTx(ctx, tx, input.AppID, input.GiftID, pointstore.GiftReviewing)
	if err != nil {
		return nil, err
	}
	refundNo, err := refundPointsStoreGiftTx(ctx, tx, gift, "积分赠送退回", true)
	if err != nil {
		return nil, err
	}
	note := nullableString(strings.TrimSpace(input.Note))
	results := make([]pointstore.RewardResult, len(gift.Results))
	copy(results, gift.Results)
	for i := range results {
		results[i].Pending = false
	}
	resultsJSON, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE points_store_redemptions SET status = 'cancelled', results = $2, refund_transaction_no = $3,
note = $4, handled_by = $5, handled_at = NOW(), updated_at = NOW() WHERE id = $1`,
		*gift.RedemptionID, resultsJSON, refundNo, note, nullableString(input.Operator)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE points_store_gifts SET status = 'rejected', results = $2, review_note = $3,
reviewed_by = $4, reviewed_at = NOW(), refund_transaction_no = $5, updated_at = NOW() WHERE id = $1`,
		gift.ID, resultsJSON, note, nullableString(input.Operator), refundNo); err != nil {
		return nil, err
	}
	updated, err := getPointsStoreGiftQ(ctx, tx, input.AppID, gift.ID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return updated, nil
}

// refundPointsStoreGiftTx 把这笔赠送的积分退给赠送人；restock 时库存一并加回（只退本补货周期兑出的那份）。
func refundPointsStoreGiftTx(ctx context.Context, tx pgx.Tx, gift *pointstore.Gift, title string, restock bool) (string, error) {
	_, _, txnNo, err := applyIntegralChangeTx(ctx, tx, gift.Sender.UserID, gift.AppID, gift.Cost,
		"earn", pointstore.IntegralCategoryRefund, title, gift.ItemTitle, "points_store", gift.RedemptionID,
		map[string]any{"redemptionNo": gift.RedemptionNo, "giftNo": gift.GiftNo})
	if err != nil {
		return "", err
	}
	if restock && gift.ItemID != nil {
		if err := restorePointsStoreStockTx(ctx, tx, *gift.ItemID, gift.CreatedAt); err != nil {
			return "", err
		}
	}
	return txnNo, nil
}

// restorePointsStoreStockTx 一笔兑换被取消时把库存加回。开了补货周期时只有本周期兑出的那份才退回：
// 上个周期的货已经随补货清零，退回去会让本周期凭空多出一份。
func restorePointsStoreStockTx(ctx context.Context, tx pgx.Tx, itemID int64, redeemedAt time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE points_store_items
SET stock = CASE
        WHEN stock IS NULL THEN NULL
        WHEN restock_period <> 'none' AND (stock_window_start IS NULL OR $2 < stock_window_start) THEN stock
        WHEN restock_quantity IS NOT NULL THEN LEAST(stock + 1, restock_quantity)
        ELSE stock + 1 END,
    sold_count = GREATEST(sold_count - 1, 0), updated_at = NOW()
WHERE id = $1`, itemID, redeemedAt)
	return err
}

// ── 撤销追回 ──

// RevokePointsStoreGift 撤销一笔已送达的赠送：逐项从接收人处追回，按需把积分退给赠送人（单事务）。
//
//   - 会员（套餐 / 天数）：作废这段开通，扣掉它还没走完的那部分时长，排在后面的各段前移同样的时长；
//     已经过去的时间追不回来，也不该追（那部分对方已经用了）
//   - 功能解锁、个性装扮：收回这一条（与管理员收回同一口径，后面排着的限时段不前移）
//
// 追不到的（对方已被收回、已折算、已过期）如实记在 revocation 里，不让整笔撤销失败：撤销的目的是
// 「对方手上不再有这份东西」，已经没有了就是达成了。
// refundSender 为真时退积分、兑换记为已取消；为假时兑换记为已撤销（积分不退：套利、欺诈的情形）。
func (r *Repository) RevokePointsStoreGift(ctx context.Context, input pointstore.GiftReviewInput) (*pointstore.Gift, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	gift, err := lockGiftForHandlingTx(ctx, tx, input.AppID, input.GiftID, pointstore.GiftDelivered)
	if err != nil {
		return nil, err
	}
	recipientID := gift.Recipient.UserID
	reason := "赠送撤销 " + gift.GiftNo
	revocation := make([]pointstore.GiftRevocationItem, 0, len(gift.Results))
	for _, result := range gift.Results {
		item := pointstore.GiftRevocationItem{Type: result.Type, Label: result.Label, Detail: result.Detail, Outcome: pointstore.RevokeMissing}
		switch result.Type {
		case pointstore.RewardVipPlan, pointstore.RewardVipDays:
			outcome, err := revokeVipGrantTx(ctx, tx, input.AppID, recipientID, result.TransactionNo, reason)
			if err != nil {
				return nil, err
			}
			item.Outcome = outcome
		case pointstore.RewardFeature:
			outcome, err := revokeOwnedRowTx(ctx, tx, "user_feature_unlocks", input.AppID, recipientID, result.TransactionNo, input.Operator, reason)
			if err != nil {
				return nil, err
			}
			item.Outcome = outcome
		case pointstore.RewardCosmetic:
			outcome, err := revokeOwnedRowTx(ctx, tx, "user_cosmetics", input.AppID, recipientID, result.TransactionNo, input.Operator, reason)
			if err != nil {
				return nil, err
			}
			item.Outcome = outcome
		}
		revocation = append(revocation, item)
	}

	refundNo := ""
	redemptionStatus := pointstore.RedemptionRevoked
	if input.RefundSender {
		// 已经送达的那份库存不加回：撤销是事后处置，不是让这件货重新上架
		if refundNo, err = refundPointsStoreGiftTx(ctx, tx, gift, "积分赠送撤销退回", false); err != nil {
			return nil, err
		}
		redemptionStatus = pointstore.RedemptionCancelled
	}
	revocationJSON, _ := json.Marshal(revocation)
	note := nullableString(strings.TrimSpace(input.Note))
	if _, err := tx.Exec(ctx, `UPDATE points_store_redemptions SET status = $2, refund_transaction_no = COALESCE($3, refund_transaction_no),
note = $4, handled_by = $5, handled_at = NOW(), updated_at = NOW() WHERE id = $1`,
		*gift.RedemptionID, redemptionStatus, nullableString(refundNo), note, nullableString(input.Operator)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE points_store_gifts SET status = 'revoked', revocation = $2, revoke_note = $3,
revoked_by = $4, revoked_at = NOW(), refund_transaction_no = COALESCE($5, refund_transaction_no), updated_at = NOW() WHERE id = $1`,
		gift.ID, revocationJSON, note, nullableString(input.Operator), nullableString(refundNo)); err != nil {
		return nil, err
	}
	updated, err := getPointsStoreGiftQ(ctx, tx, input.AppID, gift.ID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return updated, nil
}

// revokeVipGrantTx 作废一笔限时会员开通，扣掉它还没走完的时长（调用方已锁住用户行）。
//
// 剩余时长 = 段终点 − max(段起点, 此刻)：段还没轮到时是整段，正在走时是剩下的部分，已经走完时为 0。
// users.vip_expire_at 减去这么多；起点不早于这段终点的各段（顺延链上排在它后面的）前移同样的时长。
func revokeVipGrantTx(ctx context.Context, tx pgx.Tx, appID, userID int64, transactionNo, reason string) (string, error) {
	transactionNo = strings.TrimSpace(transactionNo)
	if transactionNo == "" {
		return pointstore.RevokeMissing, nil
	}
	var (
		segmentID   int64
		from, until time.Time
	)
	err := tx.QueryRow(ctx, `SELECT vt.id, `+vipSegmentFromSQL+`, `+vipSegmentUntilSQL+`
FROM vip_transactions vt
WHERE vt.appid = $1 AND vt.user_id = $2 AND vt.transaction_no = $3 AND `+vipSegmentScopeSQL+` AND `+vipSegmentTimedSQL+`
FOR UPDATE`, appID, userID, transactionNo).Scan(&segmentID, &from, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return pointstore.RevokeMissing, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE vip_transactions SET revoked_at = NOW(), revoke_reason = $2 WHERE id = $1`,
		segmentID, truncateColumn(reason, 64)); err != nil {
		return "", err
	}
	start := from
	if now := time.Now(); now.After(start) {
		start = now
	}
	remaining := until.Sub(start)
	if remaining <= 0 {
		return pointstore.RevokeExpired, nil
	}
	seconds := remaining.Seconds()
	if _, err := tx.Exec(ctx, `UPDATE users SET vip_expire_at = vip_expire_at - make_interval(secs => $3), updated_at = NOW()
WHERE id = $1 AND appid = $2 AND vip_expire_at IS NOT NULL`, userID, appID, seconds); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE vip_transactions AS vt SET
    active_from = `+vipSegmentFromSQL+` - make_interval(secs => $4),
    active_until = `+vipSegmentUntilSQL+` - make_interval(secs => $4)
WHERE vt.appid = $1 AND vt.user_id = $2 AND vt.id <> $3 AND `+vipSegmentScopeSQL+` AND `+vipSegmentTimedSQL+`
  AND `+vipSegmentFromSQL+` >= $5`,
		appID, userID, segmentID, seconds, until); err != nil {
		return "", err
	}
	return pointstore.RevokeRecovered, nil
}

// revokeOwnedRowTx 收回一条功能解锁或装扮持有（按发放结果里记的编号）。已过期的照样标记收回，结果记为 expired。
func revokeOwnedRowTx(ctx context.Context, tx pgx.Tx, table string, appID, userID int64, rawID, operator, reason string) (string, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id <= 0 {
		return pointstore.RevokeMissing, nil
	}
	var endsAt *time.Time
	err = tx.QueryRow(ctx, `UPDATE `+table+` SET revoked_at = NOW(), revoked_by = $4, revoke_reason = $5
WHERE id = $1 AND appid = $2 AND user_id = $3 AND revoked_at IS NULL
RETURNING ends_at`, id, appID, userID, nullableString(operator), truncateColumn(reason, 256)).Scan(&endsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return pointstore.RevokeMissing, nil
	}
	if err != nil {
		return "", err
	}
	if endsAt != nil && !endsAt.After(time.Now()) {
		return pointstore.RevokeExpired, nil
	}
	return pointstore.RevokeRecovered, nil
}

// ── 查询 ──

// ListPointsStoreGifts 赠送记录，新的在前。
func (r *Repository) ListPointsStoreGifts(ctx context.Context, query pointstore.GiftQuery) (*pointstore.Page[pointstore.Gift], error) {
	where := []string{"g.appid = $1"}
	args := []any{query.AppID}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(cond, "$?", "$"+strconv.Itoa(len(args))))
	}
	if query.UserID > 0 {
		add("(g.sender_id = $? OR g.recipient_id = $?)", query.UserID)
	}
	if query.SenderID > 0 {
		add("g.sender_id = $?", query.SenderID)
	}
	if query.RecipientID > 0 {
		add("g.recipient_id = $?", query.RecipientID)
	}
	if query.ItemID > 0 {
		add("g.item_id = $?", query.ItemID)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		add("g.status = $?", status)
	}
	if query.From != nil {
		add("g.created_at >= $?", *query.From)
	}
	if query.To != nil {
		add("g.created_at < $?", *query.To)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		add(`(g.gift_no ILIKE $? OR g.item_title ILIKE $? OR rd.redemption_no ILIKE $? OR su.account ILIKE $? OR sp.nickname ILIKE $?
 OR ru.account ILIKE $? OR rp.nickname ILIKE $? OR su.number::text = TRIM(BOTH '%' FROM $?) OR ru.number::text = TRIM(BOTH '%' FROM $?))`,
			"%"+keyword+"%")
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+pointsStoreGiftFrom+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+pointsStoreGiftColumns+pointsStoreGiftFrom+clause+
		fmt.Sprintf(" ORDER BY g.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]pointstore.Gift, 0, limit)
	for rows.Next() {
		gift, err := scanPointsStoreGift(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *gift)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &pointstore.Page[pointstore.Gift]{Items: items, Total: total, Page: page, Limit: limit,
		TotalPages: int((total + int64(limit) - 1) / int64(limit))}, nil
}

// PointsStoreGiftSummary 各状态的笔数，以及今天送达的笔数与积分（dayStart 为今天的起点）。
func (r *Repository) PointsStoreGiftSummary(ctx context.Context, appID int64, dayStart time.Time) (*pointstore.GiftSummary, error) {
	var summary pointstore.GiftSummary
	err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE status = 'reviewing'),
  COUNT(*) FILTER (WHERE status = 'delivered'),
  COUNT(*) FILTER (WHERE status = 'rejected'),
  COUNT(*) FILTER (WHERE status = 'revoked'),
  COUNT(*) FILTER (WHERE status = 'blocked'),
  COUNT(*) FILTER (WHERE status = 'delivered' AND created_at >= $2),
  COALESCE(SUM(cost) FILTER (WHERE status = 'delivered' AND created_at >= $2), 0)
FROM points_store_gifts WHERE appid = $1`, appID, dayStart).Scan(&summary.Reviewing, &summary.Delivered, &summary.Rejected,
		&summary.Revoked, &summary.Blocked, &summary.TodayDelivered, &summary.TodayPoints)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}
