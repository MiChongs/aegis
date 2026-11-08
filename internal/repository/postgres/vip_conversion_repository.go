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
	"github.com/shopspring/decimal"

	"aegis/internal/domain/featuretime"
	vipdomain "aegis/internal/domain/vip"
)

// 会员权益的统一折算（规则见 internal/domain/vip/conversion.go，文档 docs/vip-conversion.md）。
//
// 三个入口都在发放核心里，调用方不必各接一遍：
//
//	extendUserVipTx       Grant.ConvertOwned 时，已拥有的会员发放折成积分（convertOwnedVipTx）
//	grantLifetimeVipTx    永久开通之后折掉被覆盖的存量（convertForLifetimeUpgradeTx）
//	grantFeatureUnlockTx  FeatureUnlockGrant.ConvertOwned 时，已拥有的功能解锁折成积分
//
// 取消永久会员（管理员 / 退款）之后，按策略把开通时折掉的存量还回去（restoreLifetimeConversionsTx）。

// vipConversionIntegralCategory 折算积分的流水分类。
const vipConversionIntegralCategory = "vip_convert"

// unlockRevokeReasonConvert 开通永久时折掉的功能解锁的作废原因。
const unlockRevokeReasonConvert = "lifetime_convert"

// ── 策略 ──

// GetVipConversionPolicy 应用的折算策略；没配过时返回默认策略（Configured 为 false）。
func (r *Repository) GetVipConversionPolicy(ctx context.Context, appID int64) (vipdomain.ConversionPolicy, error) {
	return vipConversionPolicyQ(ctx, r.pool, appID)
}

// vipConversionPolicySQL 应用的折算策略（$1 appid）。
const vipConversionPolicySQL = `SELECT owned_policy, upgrade_policy, per_vip_day, per_feature_day, per_feature_hour,
per_lifetime, restore_on_revoke, updated_by, updated_at
FROM vip_conversion_policies WHERE appid = $1`

func vipConversionPolicyQ(ctx context.Context, q queryExecutor, appID int64) (vipdomain.ConversionPolicy, error) {
	return scanVipConversionPolicy(q.QueryRow(ctx, vipConversionPolicySQL, appID), appID)
}

// scanVipConversionPolicy 解析 vipConversionPolicySQL 的一行；没配过时返回默认策略（单发与批量管道共用）。
func scanVipConversionPolicy(row pgx.Row, appID int64) (vipdomain.ConversionPolicy, error) {
	policy := vipdomain.DefaultConversionPolicy(appID)
	var (
		updatedBy *string
		updatedAt time.Time
	)
	err := row.Scan(&policy.Owned, &policy.Upgrade, &policy.PerVipDay,
		&policy.PerFeatureDay, &policy.PerFeatureHour, &policy.PerLifetime, &policy.RestoreOnRevoke, &updatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	policy.Configured = true
	policy.UpdatedBy = derefString(updatedBy)
	policy.UpdatedAt = &updatedAt
	return policy, nil
}

// SaveVipConversionPolicy 整体覆盖保存。
func (r *Repository) SaveVipConversionPolicy(ctx context.Context, policy vipdomain.ConversionPolicy) (vipdomain.ConversionPolicy, error) {
	if _, err := r.pool.Exec(ctx, `INSERT INTO vip_conversion_policies
(appid, owned_policy, upgrade_policy, per_vip_day, per_feature_day, per_feature_hour, per_lifetime, restore_on_revoke,
 updated_by, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
ON CONFLICT (appid) DO UPDATE SET
  owned_policy = EXCLUDED.owned_policy,
  upgrade_policy = EXCLUDED.upgrade_policy,
  per_vip_day = EXCLUDED.per_vip_day,
  per_feature_day = EXCLUDED.per_feature_day,
  per_feature_hour = EXCLUDED.per_feature_hour,
  per_lifetime = EXCLUDED.per_lifetime,
  restore_on_revoke = EXCLUDED.restore_on_revoke,
  updated_by = EXCLUDED.updated_by,
  updated_at = NOW()`,
		policy.AppID, policy.Owned, policy.Upgrade, policy.PerVipDay, policy.PerFeatureDay, policy.PerFeatureHour,
		policy.PerLifetime, policy.RestoreOnRevoke, nullableString(policy.UpdatedBy)); err != nil {
		return policy, err
	}
	return r.GetVipConversionPolicy(ctx, policy.AppID)
}

// ── 已拥有 ──

// vipHoldingsQ 一个人已经永久拥有什么（判「已拥有」的唯一事实来源）：
// 由会员判定的同一份事实推出（vip.HoldingsOf），与 /vip/status 不会各说各话。
func vipHoldingsQ(ctx context.Context, q queryExecutor, appID, userID int64) (vipdomain.Holdings, error) {
	facts, err := vipEntitlementFactsQ(ctx, q, appID, userID)
	if err != nil {
		return vipdomain.Holdings{}, err
	}
	return vipdomain.HoldingsOf(*facts, time.Now()), nil
}

// GetVipHoldings 见 vipHoldingsQ。
func (r *Repository) GetVipHoldings(ctx context.Context, appID, userID int64) (vipdomain.Holdings, error) {
	return vipHoldingsQ(ctx, r.pool, appID, userID)
}

// lockUserTx 锁用户行（同一用户的发放、折算、还回全部串行）。
func lockUserTx(ctx context.Context, tx pgx.Tx, appID, userID int64) error {
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// ── 记录 ──

const vipConversionColumns = `id, conversion_no, appid, user_id, kind, source, reason, items, points,
COALESCE(integral_transaction_no, ''), COALESCE(trigger_transaction_no, ''), COALESCE(vip_transaction_no, ''),
message, status, restored_at, restore, COALESCE(operator, ''), created_at`

func scanVipConversion(row interface{ Scan(dest ...any) error }) (*vipdomain.Conversion, error) {
	var (
		item    vipdomain.Conversion
		items   []byte
		restore []byte
	)
	if err := row.Scan(&item.ID, &item.ConversionNo, &item.AppID, &item.UserID, &item.Kind, &item.Source, &item.Reason,
		&items, &item.Points, &item.IntegralTransactionNo, &item.TriggerTransactionNo, &item.VipTransactionNo,
		&item.Message, &item.Status, &item.RestoredAt, &restore, &item.Operator, &item.CreatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := json.Unmarshal(items, &item.Items); err != nil {
		return nil, fmt.Errorf("decode vip conversion items: %w", err)
	}
	if item.Items == nil {
		item.Items = []vipdomain.ConversionItem{}
	}
	if len(restore) > 0 {
		_ = json.Unmarshal(restore, &item.Restore)
	}
	return &item, nil
}

func insertVipConversionTx(ctx context.Context, tx pgx.Tx, item *vipdomain.Conversion) error {
	items, err := json.Marshal(item.Items)
	if err != nil {
		return err
	}
	if item.ConversionNo == "" {
		item.ConversionNo = generateTransactionNo("VCV")
	}
	if item.Status == "" {
		item.Status = vipdomain.ConversionApplied
	}
	return tx.QueryRow(ctx, `INSERT INTO vip_conversions
(conversion_no, appid, user_id, kind, source, reason, items, points, integral_transaction_no, trigger_transaction_no,
 vip_transaction_no, message, status, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING id, created_at`,
		item.ConversionNo, item.AppID, item.UserID, item.Kind, item.Source, item.Reason, items, item.Points,
		nullableString(item.IntegralTransactionNo), nullableString(item.TriggerTransactionNo),
		nullableString(item.VipTransactionNo), truncateColumn(item.Message, 512), item.Status,
		nullableString(item.Operator)).Scan(&item.ID, &item.CreatedAt)
}

// ListVipConversions 折算记录，新的在前。userID 为 0 查全应用。
func (r *Repository) ListVipConversions(ctx context.Context, appID, userID int64, page, limit int) ([]vipdomain.Conversion, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	args := []any{appID}
	where := ` WHERE appid = $1`
	if userID > 0 {
		args = append(args, userID)
		where += ` AND user_id = $2`
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM vip_conversions`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+vipConversionColumns+` FROM vip_conversions`+where+
		fmt.Sprintf(` ORDER BY id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]vipdomain.Conversion, 0, limit)
	for rows.Next() {
		item, err := scanVipConversion(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *item)
	}
	return out, total, rows.Err()
}

// grantConversionPointsTx 折算积分入账；0 积分不记流水。
func grantConversionPointsTx(ctx context.Context, tx pgx.Tx, item *vipdomain.Conversion) error {
	if item.Points <= 0 {
		return nil
	}
	title := "会员权益折算"
	if item.Kind == vipdomain.ConversionUpgrade {
		title = "开通永久会员折算"
	}
	_, _, txnNo, err := applyIntegralChangeTx(ctx, tx, item.UserID, item.AppID, item.Points,
		"earn", vipConversionIntegralCategory, title, vipdomain.ItemsLabel(item.Items), "vip_conversion", nil,
		map[string]any{"conversionNo": item.ConversionNo, "kind": item.Kind, "source": item.Source})
	if err != nil {
		return err
	}
	item.IntegralTransactionNo = txnNo
	return nil
}

// insertVipNoteTx 会员账本里的一行说明（不是会员段：时长不为正、不是永久）。
func insertVipNoteTx(ctx context.Context, tx pgx.Tx, txn *vipdomain.Transaction) error {
	metaJSON, _ := json.Marshal(txn.Metadata)
	expireAfter := txn.ExpireAfter
	if expireAfter == nil {
		// 限时记录必须有 expire_after（ck_vip_transactions_expire_after）：没有限时会员时记此刻
		now := time.Now().UTC()
		expireAfter = &now
	}
	return tx.QueryRow(ctx,
		`INSERT INTO vip_transactions (transaction_no, user_id, appid, plan_id, plan_name, features, duration_days, lifetime,
pay_channel, pay_amount, related_order_no, bonus_integral, expire_before, expire_after, operator, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
RETURNING id, created_at`,
		txn.TransactionNo, txn.UserID, txn.AppID, txn.PlanID, txn.PlanName, txn.Features, txn.DurationDays,
		txn.PayChannel, txn.PayAmount.StringFixed(2), nullableString(txn.RelatedOrderNo), txn.BonusIntegral,
		txn.ExpireBefore, expireAfter, nullableString(txn.Operator), metaJSON).
		Scan(&txn.ID, &txn.CreatedAt)
}

// ── 已拥有的会员发放 ──

// convertOwnedVipTx 这一笔会员发放对他是空的、且策略是折算时：折成积分、记一行说明，返回 handled=true。
// 不是已拥有、或策略是照常发放时 handled=false，调用方照常发放。
func convertOwnedVipTx(ctx context.Context, tx pgx.Tx, grant vipdomain.Grant) (*vipdomain.Transaction, bool, error) {
	if err := lockUserTx(ctx, tx, grant.AppID, grant.UserID); err != nil {
		return nil, false, err
	}
	policy, err := vipConversionPolicyQ(ctx, tx, grant.AppID)
	if err != nil {
		return nil, false, err
	}
	if policy.Owned != vipdomain.OwnedConvert {
		return nil, false, nil
	}
	holdings, err := vipHoldingsQ(ctx, tx, grant.AppID, grant.UserID)
	if err != nil {
		return nil, false, err
	}
	if !holdings.Covers(vipdomain.GrantBenefit(grant)) {
		return nil, false, nil
	}

	label := strings.TrimSpace(grant.PlanName)
	if label == "" {
		label = "会员"
	}
	if grant.Lifetime {
		label += "（永久）"
	} else {
		label += "（" + strconv.Itoa(grant.DurationDays) + " 天）"
	}
	item := policy.OwnedVipItem(label, grant.DurationDays, grant.Lifetime, grant.Features)
	conversion := &vipdomain.Conversion{
		ConversionNo: generateTransactionNo("VCV"),
		AppID:        grant.AppID, UserID: grant.UserID,
		Kind: vipdomain.ConversionOwned, Source: grant.PayChannel, Reason: vipdomain.OwnedLifetime,
		Items: []vipdomain.ConversionItem{item}, Points: item.Points, Operator: grant.Operator,
	}
	conversion.Message = vipdomain.ConversionMessage(conversion.Kind, conversion.Reason, conversion.Items, conversion.Points)
	if err := grantConversionPointsTx(ctx, tx, conversion); err != nil {
		return nil, false, err
	}

	var expireAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1 AND appid = $2`,
		grant.UserID, grant.AppID).Scan(&expireAt); err != nil {
		return nil, false, err
	}
	metadata := make(map[string]any, len(grant.Metadata)+1)
	for key, value := range grant.Metadata {
		metadata[key] = value
	}
	summary := conversion.Summary()
	metadata["conversion"] = summary
	// 这一行是说明，不是会员段：0 天、不是永久，到期时间前后相同
	txn := vipdomain.Transaction{
		TransactionNo:  generateTransactionNo("VIP"),
		UserID:         grant.UserID,
		AppID:          grant.AppID,
		PlanID:         grant.PlanID,
		PlanName:       truncateColumn(strings.TrimSpace(grant.PlanName), vipPlanNameMaxRunes),
		Features:       vipdomain.NormalizeFeatureTags(grant.Features),
		PayChannel:     grant.PayChannel,
		PayAmount:      grant.PayAmount,
		RelatedOrderNo: strings.TrimSpace(grant.RelatedOrderNo),
		BonusIntegral:  grant.BonusIntegral,
		ExpireBefore:   expireAt,
		ExpireAfter:    expireAt,
		Operator:       grant.Operator,
		Metadata:       metadata,
		Conversion:     summary,
	}
	if txn.PlanName == "" {
		txn.PlanName = "会员权益折算"
	}
	if err := insertVipNoteTx(ctx, tx, &txn); err != nil {
		return nil, false, err
	}
	conversion.VipTransactionNo = txn.TransactionNo
	if err := insertVipConversionTx(ctx, tx, conversion); err != nil {
		return nil, false, err
	}
	// 套餐附赠的积分照发：那是套餐的一部分，不因会员已拥有而落空
	if err := grantVipBonusTx(ctx, tx, grant, &txn); err != nil {
		return nil, false, err
	}
	return &txn, true, nil
}

// applyVipConversionDetail 发放结果里的一项被折成了积分时，把说明换成折算的那句话。
func applyVipConversionDetail(detail *string, conversion **vipdomain.ConversionSummary, txn *vipdomain.Transaction) {
	if txn == nil || txn.Conversion == nil || txn.Conversion.Kind != vipdomain.ConversionOwned {
		return
	}
	*detail = txn.Conversion.Message
	*conversion = txn.Conversion
}

// ── 已拥有的功能解锁 ──

// FeatureUnlockOutcome 一次功能解锁发放的结果：发了一段，或折成了积分。
type FeatureUnlockOutcome struct {
	Unlock     *vipdomain.FeatureUnlock
	Conversion *vipdomain.Conversion
}

// grantFeatureUnlockOrConvertTx 功能解锁发放的统一入口（赠送类渠道）：
// 已拥有（已永久解锁 / 永久会员已包含）且策略是折算时折成积分；
// 策略是照常发放时，永久会员已包含的照常记一段，已永久解锁的报 ErrFeatureUnlockOwned（没有可记的）。
func grantFeatureUnlockOrConvertTx(ctx context.Context, tx pgx.Tx, grant FeatureUnlockGrant, source string) (*FeatureUnlockOutcome, error) {
	if err := lockUserTx(ctx, tx, grant.AppID, grant.UserID); err != nil {
		return nil, err
	}
	policy, err := vipConversionPolicyQ(ctx, tx, grant.AppID)
	if err != nil {
		return nil, err
	}
	holdings, err := vipHoldingsQ(ctx, tx, grant.AppID, grant.UserID)
	if err != nil {
		return nil, err
	}
	reason, owned := holdings.CoversFeature(grant.Tag)
	if !owned || policy.Owned != vipdomain.OwnedConvert {
		unlock, err := grantFeatureUnlockTx(ctx, tx, grant)
		if err != nil {
			return nil, err
		}
		return &FeatureUnlockOutcome{Unlock: unlock}, nil
	}
	name, err := featureNameTx(ctx, tx, grant.AppID, grant.Tag)
	if err != nil {
		return nil, err
	}
	label := name + " 永久"
	if grant.Days > 0 {
		label = name + " " + strconv.Itoa(grant.Days) + " 天"
	}
	item := policy.OwnedUnlockItem(label, grant.Tag, grant.Days)
	conversion := &vipdomain.Conversion{
		ConversionNo: generateTransactionNo("VCV"),
		AppID:        grant.AppID, UserID: grant.UserID,
		Kind: vipdomain.ConversionOwned, Source: source, Reason: reason,
		Items: []vipdomain.ConversionItem{item}, Points: item.Points, Operator: grant.Operator,
	}
	conversion.Message = vipdomain.ConversionMessage(conversion.Kind, reason, conversion.Items, conversion.Points)
	if err := grantConversionPointsTx(ctx, tx, conversion); err != nil {
		return nil, err
	}
	if err := insertVipConversionTx(ctx, tx, conversion); err != nil {
		return nil, err
	}
	return &FeatureUnlockOutcome{Conversion: conversion}, nil
}

// ── 开通永久：折掉被覆盖的存量 ──

// loadUpgradeInputQ 读开通永久那一刻的存量。
//
// assume 非空时按「假如此刻再开通一笔带这些功能的永久」算持有（预览用：那一笔还没落账）。
func loadUpgradeInputQ(ctx context.Context, q queryExecutor, appID, userID int64, assume *[]string) (vipdomain.UpgradeInput, error) {
	in := vipdomain.UpgradeInput{Now: time.Now().UTC()}
	facts, err := vipEntitlementFactsQ(ctx, q, appID, userID)
	if err != nil {
		return in, err
	}
	in.ExpireAt = facts.ExpireAt
	holdings := vipdomain.HoldingsOf(*facts, in.Now)
	for _, segment := range facts.Segments {
		if segment.Lifetime || !segment.LiveAt(in.Now) {
			continue
		}
		in.Segments = append(in.Segments, vipdomain.TimedSegment{
			ID: segment.ID, TransactionNo: segment.TransactionNo, Label: segment.EffectivePlanName(),
			Features: segment.EffectiveFeatures(), AdFree: segment.GrantsAdFree(),
			From: segment.ActiveFrom, Until: segment.ActiveUntil,
		})
	}
	if assume != nil {
		// 预览的永久开通来自套餐（购买 / 发放），一律免广告
		holdings = holdings.Assume(vipdomain.MembershipBenefit(*assume, true, true))
	}
	in.Holdings = holdings

	rows, err := q.Query(ctx, `SELECT x.id, x.feature_tag, COALESCE(f.name, ''), x.starts_at, x.ends_at
FROM user_feature_unlocks x
LEFT JOIN vip_features f ON f.appid = x.appid AND f.tag = x.feature_tag
WHERE x.appid = $1 AND x.user_id = $2 AND x.revoked_at IS NULL AND x.ends_at > NOW()
ORDER BY x.id`, appID, userID)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var unlock vipdomain.TimedUnlock
		if err := rows.Scan(&unlock.ID, &unlock.Tag, &unlock.Name, &unlock.Starts, &unlock.Ends); err != nil {
			rows.Close()
			return in, err
		}
		in.Unlocks = append(in.Unlocks, unlock)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return in, err
	}

	rows, err = q.Query(ctx, `SELECT a.feature, COALESCE(f.name, ''), a.balance_seconds
FROM feature_time_accounts a
LEFT JOIN vip_features f ON f.appid = a.appid AND f.tag = a.feature
WHERE a.appid = $1 AND a.user_id = $2 AND a.balance_seconds > 0
ORDER BY a.feature`, appID, userID)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var balance vipdomain.FeatureTimeBalance
		if err := rows.Scan(&balance.Feature, &balance.Name, &balance.Seconds); err != nil {
			rows.Close()
			return in, err
		}
		in.Balances = append(in.Balances, balance)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return in, err
	}

	var adFree time.Time
	err = q.QueryRow(ctx, `SELECT expires_at FROM user_ad_free_passes WHERE appid = $1 AND user_id = $2 AND expires_at > NOW()`,
		appID, userID).Scan(&adFree)
	switch {
	case err == nil:
		in.AdFree = &adFree
	case !errors.Is(err, pgx.ErrNoRows):
		return in, err
	}
	return in, nil
}

// PreviewLifetimeUpgrade 假如此刻开通一笔带这些功能的永久会员，存量会怎么折（不落账）。
// 策略是保留时 Plan 为空、policy 原样返回，调用方据此说明「将保留」。
func (r *Repository) PreviewLifetimeUpgrade(ctx context.Context, appID, userID int64, features []string) (vipdomain.UpgradePlan, vipdomain.ConversionPolicy, error) {
	policy, err := vipConversionPolicyQ(ctx, r.pool, appID)
	if err != nil {
		return vipdomain.UpgradePlan{}, policy, err
	}
	in, err := loadUpgradeInputQ(ctx, r.pool, appID, userID, &features)
	if err != nil {
		return vipdomain.UpgradePlan{}, policy, err
	}
	in.Policy = policy
	return vipdomain.PlanUpgrade(in), policy, nil
}

// convertForLifetimeUpgradeTx 永久开通之后折掉被覆盖的存量（调用方已锁用户行、已记下永久开通 trigger）。
// 没有可折的或策略是保留时返回 nil。
func convertForLifetimeUpgradeTx(ctx context.Context, tx pgx.Tx, grant vipdomain.Grant, trigger *vipdomain.Transaction) (*vipdomain.Conversion, error) {
	policy, err := vipConversionPolicyQ(ctx, tx, grant.AppID)
	if err != nil {
		return nil, err
	}
	mode := policy.Upgrade
	if grant.UpgradeMode != "" {
		mode = grant.UpgradeMode
	}
	if mode != vipdomain.UpgradeConvert {
		return nil, nil
	}
	in, err := loadUpgradeInputQ(ctx, tx, grant.AppID, grant.UserID, nil)
	if err != nil {
		return nil, err
	}
	in.Policy = policy
	plan := vipdomain.PlanUpgrade(in)
	if plan.Empty() {
		return nil, nil
	}

	for _, move := range plan.Moves {
		if _, err := tx.Exec(ctx, `UPDATE vip_transactions SET active_from = $2, active_until = $3 WHERE id = $1`,
			move.ID, move.From, move.Until); err != nil {
			return nil, err
		}
	}
	if plan.NewExpireAt != nil {
		if _, err := tx.Exec(ctx, `UPDATE users SET vip_expire_at = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
			*plan.NewExpireAt, grant.UserID, grant.AppID); err != nil {
			return nil, err
		}
	}
	if len(plan.UnlockIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE user_feature_unlocks SET revoked_at = NOW(), revoked_by = 'system', revoke_reason = $3
WHERE appid = $1 AND user_id = $2 AND id = ANY($4) AND revoked_at IS NULL`,
			grant.AppID, grant.UserID, unlockRevokeReasonConvert, plan.UnlockIDs); err != nil {
			return nil, err
		}
	}
	conversion := &vipdomain.Conversion{
		ConversionNo: generateTransactionNo("VCV"),
		AppID:        grant.AppID, UserID: grant.UserID,
		Kind: vipdomain.ConversionUpgrade, Source: grant.PayChannel, Reason: vipdomain.OwnedLifetime,
		Items: plan.Items, Points: plan.Points, TriggerTransactionNo: trigger.TransactionNo, Operator: grant.Operator,
	}
	for _, balance := range plan.Balances {
		var after int64
		if err := tx.QueryRow(ctx, `UPDATE feature_time_accounts SET balance_seconds = GREATEST(balance_seconds - $4, 0), updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND feature = $3 RETURNING balance_seconds`,
			grant.AppID, grant.UserID, balance.Feature, balance.Seconds).Scan(&after); err != nil {
			return nil, err
		}
		if err := insertFeatureTimeLedgerTx(ctx, tx, grant.AppID, grant.UserID, balance.Feature, -balance.Seconds, after,
			featuretime.KindConvert, "vip_conversion", conversion.ConversionNo, "开通永久会员折算"); err != nil {
			return nil, err
		}
	}
	if plan.EndAdFree {
		if _, err := tx.Exec(ctx, `UPDATE user_ad_free_passes SET expires_at = NOW(), updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND expires_at > NOW()`, grant.AppID, grant.UserID); err != nil {
			return nil, err
		}
	}
	conversion.Message = vipdomain.ConversionMessage(conversion.Kind, conversion.Reason, conversion.Items, conversion.Points)
	if err := grantConversionPointsTx(ctx, tx, conversion); err != nil {
		return nil, err
	}

	summary := conversion.Summary()
	removedDays := 0
	if plan.RemovedSeconds > 0 {
		removedDays = int((plan.RemovedSeconds + 86399) / 86400)
	}
	expireAfter := in.ExpireAt
	if plan.NewExpireAt != nil {
		expireAfter = plan.NewExpireAt
	}
	note := vipdomain.Transaction{
		TransactionNo: generateTransactionNo("VIP"),
		UserID:        grant.UserID,
		AppID:         grant.AppID,
		PlanName:      "开通永久会员折算",
		Features:      []string{},
		DurationDays:  -removedDays,
		PayChannel:    vipdomain.ChannelLifetimeConvert,
		PayAmount:     decimal.Zero,
		ExpireBefore:  in.ExpireAt,
		ExpireAfter:   expireAfter,
		Operator:      grant.Operator,
		Metadata:      map[string]any{"conversion": summary, "trigger": trigger.TransactionNo},
		Conversion:    summary,
	}
	if err := insertVipNoteTx(ctx, tx, &note); err != nil {
		return nil, err
	}
	conversion.VipTransactionNo = note.TransactionNo
	if err := insertVipConversionTx(ctx, tx, conversion); err != nil {
		return nil, err
	}
	return conversion, nil
}

// ── 取消永久会员：还回折掉的存量 ──

// restoreLifetimeConversionsTx 永久身份已经没了（管理员取消 / 退款）时，把开通时折掉的存量还回去、收回折给的积分。
//
// 调用方已锁用户行。还是永久会员（另有永久开通）时什么也不做：存量仍被覆盖。
// 积分收到 0 为止（用户可能已经花掉），收不回的差额记在 restore 里。返回还回的记录数。
func restoreLifetimeConversionsTx(ctx context.Context, tx pgx.Tx, appID, userID int64, operator string) (int, error) {
	var lifetime bool
	if err := tx.QueryRow(ctx, `SELECT vip_lifetime_at IS NOT NULL FROM users WHERE id = $1 AND appid = $2`,
		userID, appID).Scan(&lifetime); err != nil {
		return 0, err
	}
	if lifetime {
		return 0, nil
	}
	policy, err := vipConversionPolicyQ(ctx, tx, appID)
	if err != nil {
		return 0, err
	}
	if !policy.RestoreOnRevoke {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+vipConversionColumns+` FROM vip_conversions
WHERE appid = $1 AND user_id = $2 AND kind = $3 AND status = $4 ORDER BY id FOR UPDATE`,
		appID, userID, vipdomain.ConversionUpgrade, vipdomain.ConversionApplied)
	if err != nil {
		return 0, err
	}
	conversions := []vipdomain.Conversion{}
	for rows.Next() {
		item, err := scanVipConversion(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		conversions = append(conversions, *item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	for _, conversion := range conversions {
		restored := []string{}
		for _, item := range conversion.Items {
			if item.Seconds <= 0 {
				continue
			}
			days := int((item.Seconds + 86399) / 86400)
			switch item.Kind {
			case vipdomain.ItemVipTime:
				txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
					UserID: userID, AppID: appID, PlanName: truncateColumn("还回 "+item.Label, vipPlanNameMaxRunes),
					Features: item.Features, DurationDays: days, Seconds: item.Seconds,
					PayChannel: vipdomain.ChannelLifetimeConvert, PayAmount: decimal.Zero, Operator: operator,
					Metadata: map[string]any{"restoreOf": conversion.ConversionNo, "ref": item.Ref},
				})
				if err != nil {
					return 0, err
				}
				restored = append(restored, txn.TransactionNo)
			case vipdomain.ItemFeatureUnlock:
				unlock, err := grantFeatureUnlockTx(ctx, tx, FeatureUnlockGrant{
					AppID: appID, UserID: userID, Tag: item.Feature, Days: days,
					Source: vipdomain.UnlockSourceAdminGrant, SourceRef: conversion.ConversionNo, Operator: operator,
				})
				if errors.Is(err, ErrFeatureUnlockOwned) {
					continue
				}
				if err != nil {
					return 0, err
				}
				restored = append(restored, "unlock:"+strconv.FormatInt(unlock.ID, 10))
			case vipdomain.ItemFeatureTime:
				var balance int64
				if err := tx.QueryRow(ctx, `INSERT INTO feature_time_accounts (appid, user_id, feature, balance_seconds, earned_seconds)
VALUES ($1, $2, $3, $4::bigint, 0)
ON CONFLICT (appid, user_id, feature) DO UPDATE
SET balance_seconds = feature_time_accounts.balance_seconds + EXCLUDED.balance_seconds, updated_at = NOW()
RETURNING balance_seconds`, appID, userID, item.Feature, item.Seconds).Scan(&balance); err != nil {
					return 0, err
				}
				if err := insertFeatureTimeLedgerTx(ctx, tx, appID, userID, item.Feature, item.Seconds, balance,
					featuretime.KindRestore, "vip_conversion", conversion.ConversionNo, "取消永久会员还回"); err != nil {
					return 0, err
				}
				restored = append(restored, "time:"+item.Feature)
			case vipdomain.ItemAdFree:
				if _, err := tx.Exec(ctx, `INSERT INTO user_ad_free_passes (appid, user_id, expires_at, granted_days)
VALUES ($1, $2, $3::timestamptz + make_interval(secs => $4::bigint), 0)
ON CONFLICT (appid, user_id) DO UPDATE
SET expires_at = GREATEST(user_ad_free_passes.expires_at, $3::timestamptz) + make_interval(secs => $4::bigint),
    updated_at = NOW()`, appID, userID, now, item.Seconds); err != nil {
					return 0, err
				}
				restored = append(restored, "ad_free")
			}
		}

		var clawed, shortfall int64
		if conversion.Points > 0 {
			var balance int64
			if err := tx.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2`, userID, appID).Scan(&balance); err != nil {
				return 0, err
			}
			clawed = min(conversion.Points, max(balance, 0))
			shortfall = conversion.Points - clawed
			if clawed > 0 {
				if _, _, _, err := applyIntegralChangeTx(ctx, tx, userID, appID, -clawed,
					"consume", vipConversionIntegralCategory, "取消永久会员收回折算积分", vipdomain.ItemsLabel(conversion.Items),
					"vip_conversion", nil, map[string]any{"conversionNo": conversion.ConversionNo}); err != nil {
					return 0, err
				}
			}
		}
		detail, _ := json.Marshal(map[string]any{
			"restored": restored, "pointsClawedBack": clawed, "pointsShortfall": shortfall, "operator": operator,
		})
		if _, err := tx.Exec(ctx, `UPDATE vip_conversions SET status = $2, restored_at = NOW(), restore = $3 WHERE id = $1`,
			conversion.ID, vipdomain.ConversionRestored, detail); err != nil {
			return 0, err
		}
	}
	return len(conversions), nil
}
