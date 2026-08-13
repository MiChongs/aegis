package postgres

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	cardkeydomain "aegis/internal/domain/cardkey"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
)

// 卡密核销与授权卡登录。
//
// 两条链路共用同一个发放函数与同一张核销流水表：授权卡首次激活时随带的权益，
// 与兑换卡被兑换时发的权益，在账本上应当长得一模一样。

// RedeemCardKey 核销一张卡（单事务，恰好一次）。
//
// 幂等核心与 FulfillPaymentOrder 同构：先锁卡行，再用**条件 UPDATE 抢占**核销权
// （`status = 'unused'`），抢不到说明已被别人核销或状态不对，直接返回相应错误；
// 抢到后在同一事务内完成全部发放，任何一步失败整体回滚（核销权也随之释放）。
//
// 三道保证叠在一起：行锁（并发串行化）、条件 UPDATE（状态机唯一跃迁）、
// 以及 card_key_redemptions 上的唯一约束（兜住前两道被改坏的情况，
// 那种 bug 的表现是重复发钱）。
func (r *Repository) RedeemCardKey(ctx context.Context, input cardkeydomain.RedeemInput) (*cardkeydomain.RedeemResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	card, err := scanCardKey(tx.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND code = $2 FOR UPDATE`,
		input.AppID, input.Code))
	if err != nil {
		return nil, err
	}

	batch, err := scanCardKeyBatch(tx.QueryRow(ctx,
		`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE id = $1`, card.BatchID))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := checkCardUsable(card, batch, now); err != nil {
		return nil, err
	}

	// 授权卡与兑换卡的终态不同：前者变成「已激活」（在授权期内可反复登录），
	// 后者变成「已核销」（终态）。放同一条 UPDATE 里是因为抢占必须是一次原子跃迁。
	nextStatus := cardkeydomain.StatusUsed
	var expiresAt *time.Time
	if card.Kind == cardkeydomain.KindLogin {
		nextStatus = cardkeydomain.StatusActive
		expiresAt = resolveCardExpiry(batch, now)
	}

	var claimed int64
	err = tx.QueryRow(ctx, `UPDATE card_keys
SET status = $3, bound_user_id = $4, activated_at = $5, used_at = $5,
    expires_at = COALESCE($6, expires_at), updated_at = NOW()
WHERE id = $1 AND appid = $2 AND status = 'unused'
RETURNING id`, card.ID, input.AppID, nextStatus, input.UserID, now, expiresAt).Scan(&claimed)
	if err != nil {
		if err == pgx.ErrNoRows {
			// 行锁已经握在手里，走到这里只可能是状态在锁之前就不是 unused。
			return nil, ErrCardKeyUsed
		}
		return nil, err
	}

	results, err := r.grantCardRewardsTx(ctx, tx, input.AppID, input.UserID, batch.Rewards, card)
	if err != nil {
		return nil, err
	}

	if err := insertCardRedemptionTx(ctx, tx, card, batch, input, results); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	// 重新读一次到期时间：固定到期模式下它在生成时就写死了，上面的 COALESCE 没有覆盖它。
	final := expiresAt
	if final == nil {
		final = card.ExpiresAt
	}
	return &cardkeydomain.RedeemResult{
		Code:      card.Code,
		Kind:      card.Kind,
		Results:   results,
		ExpiresAt: final,
	}, nil
}

// ActivateLoginCard 授权卡登录：首次激活建立绑定并发权益，之后每次登录只续设备。
//
// 用户必须**已经存在**才能调这里 —— 建号发生在服务层（它才有资料、邀请码、
// 搜索同步这些东西）。账号名由卡面派生且唯一，因此并发的两次首登不会造出两个账号：
// 第二个会撞 uq_users_appid_account，服务层据此回读同一个用户。
func (r *Repository) ActivateLoginCard(ctx context.Context, input cardkeydomain.ActivateLoginInput) (*cardkeydomain.LoginAuthorization, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	card, err := scanCardKey(tx.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND id = $2 FOR UPDATE`,
		input.AppID, input.CardID))
	if err != nil {
		return nil, err
	}
	if card.Kind != cardkeydomain.KindLogin {
		return nil, ErrCardKeyKindMismatch
	}

	batch, err := scanCardKeyBatch(tx.QueryRow(ctx,
		`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE id = $1`, card.BatchID))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if card.Status == cardkeydomain.StatusDisabled || batch.Status != cardkeydomain.BatchActive {
		return nil, ErrCardKeyDisabled
	}
	if card.BoundUserID != nil && *card.BoundUserID != input.UserID {
		return nil, ErrCardKeyBoundOther
	}

	firstActivation := card.Status == cardkeydomain.StatusUnused
	if !firstActivation && card.Expired(now) {
		return nil, ErrCardKeyExpired
	}

	if firstActivation {
		expiresAt := resolveCardExpiry(batch, now)
		if _, err := tx.Exec(ctx, `UPDATE card_keys
SET status = 'active', bound_user_id = $3, activated_at = $4,
    expires_at = COALESCE($5, expires_at), updated_at = NOW()
WHERE id = $1 AND appid = $2`, card.ID, input.AppID, input.UserID, now, expiresAt); err != nil {
			return nil, err
		}
		if expiresAt != nil {
			card.ExpiresAt = expiresAt
		}
		// 固定到期模式的卡在生成时就带着到期时间，这里要在激活的当下再判一次：
		// 一张已经过了统一到期日的卡，不该因为「从没被用过」而被激活。
		if card.Expired(now) {
			return nil, ErrCardKeyExpired
		}
	}

	deviceCount, err := bindCardDeviceTx(ctx, tx, card, input.DeviceID, input.DeviceName)
	if err != nil {
		return nil, err
	}

	if firstActivation && len(batch.Rewards) > 0 {
		results, err := r.grantCardRewardsTx(ctx, tx, input.AppID, input.UserID, batch.Rewards, card)
		if err != nil {
			return nil, err
		}
		redeemInput := cardkeydomain.RedeemInput{
			AppID:     input.AppID,
			Code:      card.Code,
			UserID:    input.UserID,
			Source:    cardkeydomain.SourceLogin,
			DeviceID:  input.DeviceID,
			ClientIP:  input.ClientIP,
			UserAgent: input.UserAgent,
		}
		if err := insertCardRedemptionTx(ctx, tx, card, batch, redeemInput, results); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	return &cardkeydomain.LoginAuthorization{
		Code:            card.Code,
		ExpiresAt:       card.ExpiresAt,
		MaxDevices:      card.MaxDevices,
		DeviceCount:     deviceCount,
		FirstActivation: firstActivation,
	}, nil
}

// checkCardUsable 兑换前的可用性判定。顺序即错误优先级：
// 先说「这张卡被作废了」，再说「已经用过了」，最后才说「过期了」——
// 反过来会让一张被作废的卡报「已过期」，客服据此给出的解释是错的。
func checkCardUsable(card *cardkeydomain.Card, batch *cardkeydomain.Batch, now time.Time) error {
	if card.Status == cardkeydomain.StatusDisabled || batch.Status != cardkeydomain.BatchActive {
		return ErrCardKeyDisabled
	}
	if card.Status != cardkeydomain.StatusUnused {
		return ErrCardKeyUsed
	}
	if card.Expired(now) {
		return ErrCardKeyExpired
	}
	return nil
}

// resolveCardExpiry 激活时算出到期时间。
//
// 固定到期与永久两档返回 nil：前者在生成时就写进行里了，用 COALESCE 保留原值；
// 这里返回一个新值会把它覆盖成「从今天起算」，那就不是固定到期了。
func resolveCardExpiry(batch *cardkeydomain.Batch, now time.Time) *time.Time {
	if batch.ValidityMode != cardkeydomain.ValidityFromFirstUse || batch.ValidityDays <= 0 {
		return nil
	}
	expiry := now.AddDate(0, 0, batch.ValidityDays)
	return &expiry
}

// bindCardDeviceTx 把设备记到卡上；超出上限时拒绝。
//
// 先 UPSERT 再数总数，不是「先数再插」：后者在同一张卡的两台设备并发首登时
// 会双双读到 count = max-1，各自插入，结果绑上 max+1 台。
// UPSERT 之后再数，两个事务在唯一索引上串行化，后者数到的是插入之后的真实值。
func bindCardDeviceTx(ctx context.Context, tx pgx.Tx, card *cardkeydomain.Card, deviceID string, deviceName string) (int, error) {
	if deviceID == "" {
		// 不带设备标识却开着设备限制，等于这个限制不存在。
		// 与试用的设备去重同一取向：拒绝，而不是放行。
		return 0, ErrCardKeyDeviceLimit
	}
	if _, err := tx.Exec(ctx, `INSERT INTO card_key_devices (card_key_id, appid, device_id, device_name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (card_key_id, device_id) DO UPDATE
SET last_seen_at = NOW(), seen_count = card_key_devices.seen_count + 1,
    device_name = COALESCE(NULLIF(EXCLUDED.device_name, ''), card_key_devices.device_name)`,
		card.ID, card.AppID, deviceID, deviceName); err != nil {
		return 0, err
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM card_key_devices WHERE card_key_id = $1`, card.ID).Scan(&count); err != nil {
		return 0, err
	}
	if count > card.MaxDevices {
		return count, ErrCardKeyDeviceLimit
	}
	return count, nil
}

// insertCardRedemptionTx 写核销流水。唯一约束在这里兜底「一卡一次」。
func insertCardRedemptionTx(ctx context.Context, tx pgx.Tx, card *cardkeydomain.Card,
	batch *cardkeydomain.Batch, input cardkeydomain.RedeemInput, results []cardkeydomain.RewardResult) error {
	rewards, _ := json.Marshal(batch.Rewards)
	payload, _ := json.Marshal(results)
	source := input.Source
	if source == "" {
		source = cardkeydomain.SourceRedeem
	}
	_, err := tx.Exec(ctx, `INSERT INTO card_key_redemptions
(appid, card_key_id, batch_id, code, user_id, rewards, results, source, device_id, client_ip, user_agent, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		input.AppID, card.ID, batch.ID, card.Code, input.UserID, rewards, payload, source,
		nullableString(input.DeviceID), nullableString(input.ClientIP),
		nullableString(truncateColumn(input.UserAgent, 256)), nullableString(input.Operator))
	return err
}

// grantCardRewardsTx 在同一事务里发放一张卡的全部权益。
//
// 这里的 switch 与 internal/domain/cardkey 的权益目录**双向钉死**
// （TestRewardCatalogHasGrantBranch）：目录多一档 → 配得上却发不出来；
// 这里多一档 → 控制台配不出来。
//
// 顺序按目录，不按配置顺序：先发会员再发积分，核销结果的展示顺序才是稳定的。
func (r *Repository) grantCardRewardsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64,
	rewards []cardkeydomain.Reward, card *cardkeydomain.Card) ([]cardkeydomain.RewardResult, error) {
	out := make([]cardkeydomain.RewardResult, 0, len(rewards))
	source := "卡密 " + card.Code

	for _, reward := range cardkeydomain.NormalizeRewards(rewards) {
		spec, _ := cardkeydomain.FindRewardSpec(reward.Type)
		result := cardkeydomain.RewardResult{Type: reward.Type, Label: spec.Label}

		switch reward.Type {
		case cardkeydomain.RewardVipPlan:
			plan, err := scanVipPlan(tx.QueryRow(ctx,
				`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, reward.RefID))
			if err != nil {
				return nil, err
			}
			planID := plan.ID
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:        userID,
				AppID:         appID,
				PlanID:        &planID,
				PlanName:      plan.Name,
				Features:      plan.Features,
				DurationDays:  plan.DurationDays,
				PayChannel:    vipdomain.ChannelCardKey,
				PayAmount:     decimal.Zero,
				BonusIntegral: plan.BonusIntegral,
				Metadata:      map[string]any{"cardKey": card.Code, "cardKeyId": card.ID},
			})
			if err != nil {
				return nil, err
			}
			result.Detail = plan.Name + "（" + strconv.Itoa(plan.DurationDays) + " 天）"
			result.TransactionNo = txn.TransactionNo

		case cardkeydomain.RewardVipDays:
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:       userID,
				AppID:        appID,
				PlanName:     "卡密赠送",
				DurationDays: int(reward.Amount),
				PayChannel:   vipdomain.ChannelCardKey,
				PayAmount:    decimal.Zero,
				Metadata:     map[string]any{"cardKey": card.Code, "cardKeyId": card.ID},
			})
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward)
			result.TransactionNo = txn.TransactionNo

		case cardkeydomain.RewardIntegral:
			cardID := card.ID
			_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, userID, appID, reward.Amount,
				"earn", "card_key", "卡密赠送积分", source, "card_key", &cardID,
				map[string]any{"cardKey": card.Code})
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + "（余额 " + strconv.FormatInt(after, 10) + "）"
			result.TransactionNo = txnNo

		case cardkeydomain.RewardExperience:
			cardID := card.ID
			expResult, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, reward.Amount,
				"card_key", "卡密赠送经验", source, "card_key", &cardID,
				map[string]any{"cardKey": card.Code})
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward)
			if expResult.LevelChanged {
				result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
			}
			result.TransactionNo = expResult.TransactionNo

		case cardkeydomain.RewardBalance:
			change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
				UserID:   userID,
				AppID:    appID,
				Type:     walletdomain.TxnTypeCardKey,
				Amount:   reward.Money,
				Title:    "卡密充值",
				Remark:   source,
				Metadata: map[string]any{"cardKey": card.Code, "cardKeyId": card.ID},
				// 幂等键用卡号：一张卡天生只该入账一次，这是最贴切的幂等键。
				IdempotencyKey: "cardkey:" + strconv.FormatInt(appID, 10) + ":" + card.Code,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward)
			result.TransactionNo = change.Transaction.TransactionNo

		case cardkeydomain.RewardLotteryDraws:
			balance, err := grantLotteryDrawsTx(ctx, tx, appID, userID, reward.Amount)
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + "（剩余 " + strconv.FormatInt(balance, 10) + " 次）"

		case cardkeydomain.RewardDeviceSlots:
			total, err := addDeviceSlotsTx(ctx, tx, appID, userID, int(reward.Amount))
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + "（上限 " + strconv.Itoa(total) + " 台）"

		default:
			// 目录里有、这里没有 —— 由测试挡在合并之前，运行期走到这里说明测试被绕过了。
			return nil, ErrCardKeyKindMismatch
		}

		out = append(out, result)
	}
	return out, nil
}

// addDeviceSlotsTx 给用户名下**授权期最长**的那张授权卡加设备位。
//
// 挑「最长」而不是「最近」是因为设备位加在一张三天后就过期的卡上等于没加。
// 永久卡排在最前（`expires_at IS NULL`）。找不到可用授权卡时报错而不是静默成功 ——
// 静默成功的表现是用户以为买到了，而客户端上什么都没变。
func addDeviceSlotsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, slots int) (int, error) {
	var cardID int64
	err := tx.QueryRow(ctx, `SELECT id FROM card_keys
WHERE appid = $1 AND bound_user_id = $2 AND kind = 'login' AND status = 'active'
  AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY (expires_at IS NULL) DESC, expires_at DESC, id DESC
LIMIT 1 FOR UPDATE`, appID, userID).Scan(&cardID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrCardKeyNoLoginCard
		}
		return 0, err
	}

	var total int
	// 上限 64 与建表时的 CHECK 一致：撞上 CHECK 会让整个核销事务回滚，
	// 而用户看到的是「兑换失败」，卡却已经作废。夹取比报错合适。
	if err := tx.QueryRow(ctx, `UPDATE card_keys
SET max_devices = LEAST(max_devices + $2, 64), updated_at = NOW()
WHERE id = $1 RETURNING max_devices`, cardID, slots).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
