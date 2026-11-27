package postgres

import (
	"aegis/internal/domain/coupon"
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/cosmetic"
	"aegis/internal/domain/invite"
	"aegis/internal/domain/leaderboard"
	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	"aegis/internal/domain/usernumber"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
)

// leaderboardGrant 一份榜单奖励的发放参数。
type leaderboardGrant struct {
	AppID      int64
	UserID     int64
	SnapshotID int64
	Board      string
	PeriodKey  string
	Rank       int
	Title      string
	Plans      map[int64]pointstore.PlanRef
	Features   map[string]pointstore.FeatureRef
}

// grantLeaderboardRewardsTx 逐项发放。权益目录与邀请同一份（invite.RewardCatalog），
// 这里的 switch 与目录双向钉死（TestLeaderboardCatalogHasGrantBranch）。任何一项发不出去整份回滚。
//
// 与邀请的发放分开写，是因为账本上的来源必须如实：积分分类 leaderboard（周期榜统计时排除它，
// 否则奖励滚雪球）、会员渠道 / 解锁来源 / 钱包流水类型都是 leaderboard。
func (r *Repository) grantLeaderboardRewardsTx(ctx context.Context, tx pgx.Tx, grant leaderboardGrant, rewards []leaderboard.Reward) ([]leaderboard.RewardResult, error) {
	appID, userID := grant.AppID, grant.UserID
	snapshotID := grant.SnapshotID
	key := LeaderboardIdempotencyKey(snapshotID, userID)
	meta := map[string]any{
		"leaderboardSnapshotId": snapshotID, "board": grant.Board, "periodKey": grant.PeriodKey,
		"rank": grant.Rank, "idempotencyKey": key,
	}
	out := make([]leaderboard.RewardResult, 0, len(rewards))
	for _, reward := range invite.NormalizeRewards(rewards) {
		described := invite.DescribeReward(reward, grant.Plans, grant.Features)
		result := leaderboard.RewardResult{Type: reward.Type, Label: described.Label, Detail: described.Detail}

		switch reward.Type {
		case invite.RewardPoints:
			_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, userID, appID, reward.Amount,
				"earn", leaderboard.IntegralCategory, grant.Title, "", leaderboard.SourceType, &snapshotID, meta)
			if err != nil {
				return nil, err
			}
			result.Detail += "（余额 " + strconv.FormatInt(after, 10) + "）"
			result.TransactionNo = txnNo

		case pointstore.RewardVipPlan:
			plan, err := scanVipPlan(tx.QueryRow(ctx,
				`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, reward.RefID))
			if err != nil {
				return nil, err
			}
			if plan == nil || plan.IsTrial() {
				return nil, ErrLeaderboardRewardUnavailable
			}
			planID := plan.ID
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID: userID, AppID: appID, PlanID: &planID, PlanName: plan.Name, Features: plan.Features,
				DurationDays: plan.DurationDays, Lifetime: plan.Lifetime,
				PayChannel: vipdomain.ChannelLeaderboard, PayAmount: decimal.Zero, Metadata: meta, ConvertOwned: true,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
			result.TransactionNo = txn.TransactionNo
			applyVipConversionDetail(&result.Detail, &result.Conversion, txn)

		case pointstore.RewardVipDays:
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID: userID, AppID: appID, PlanName: grant.Title, DurationDays: int(reward.Amount),
				PayChannel: vipdomain.ChannelLeaderboard, PayAmount: decimal.Zero, Metadata: meta, ConvertOwned: true,
			})
			if err != nil {
				return nil, err
			}
			result.TransactionNo = txn.TransactionNo
			applyVipConversionDetail(&result.Detail, &result.Conversion, txn)

		case pointstore.RewardFeature:
			if feature, ok := grant.Features[reward.Tag]; !ok || !feature.IsActive {
				return nil, ErrLeaderboardRewardUnavailable
			}
			outcome, err := grantFeatureUnlockOrConvertTx(ctx, tx, FeatureUnlockGrant{
				AppID: appID, UserID: userID, Tag: reward.Tag, Days: int(reward.Amount),
				Source: vipdomain.UnlockSourceLeaderboard, SourceRef: key,
			}, vipdomain.ChannelLeaderboard)
			if errors.Is(err, ErrFeatureUnlockOwned) {
				// 已永久拥有：这一项没有可发的，记一笔说明而不是让整份奖励失败
				result.Detail += "（已永久拥有）"
				out = append(out, result)
				continue
			}
			if err != nil {
				return nil, err
			}
			if outcome.Conversion != nil {
				result.Detail = outcome.Conversion.Message
				result.Conversion = outcome.Conversion.Summary()
				result.TransactionNo = outcome.Conversion.ConversionNo
				out = append(out, result)
				continue
			}
			unlock := outcome.Unlock
			result.ExpireAt = unlock.EndsAt
			result.TransactionNo = strconv.FormatInt(unlock.ID, 10)

		case pointstore.RewardExperience:
			expResult, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, reward.Amount,
				leaderboard.IntegralCategory, grant.Title, "", leaderboard.SourceType, &snapshotID, meta)
			if err != nil {
				return nil, err
			}
			if expResult.LevelChanged {
				result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
			}
			result.TransactionNo = expResult.TransactionNo

		case pointstore.RewardBalance:
			change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
				UserID: userID, AppID: appID, Type: walletdomain.TxnTypeLeaderboard, Amount: reward.Money,
				Title: grant.Title, Metadata: meta, IdempotencyKey: key,
			})
			if err != nil {
				return nil, err
			}
			result.TransactionNo = change.Transaction.TransactionNo

		case pointstore.RewardLotteryDraws:
			balance, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
				AppID: appID, UserID: userID, Amount: reward.Amount, Origin: lotterydomain.TicketOriginLeaderboard,
				Ref: key, Title: grant.Title,
			})
			if err != nil {
				return nil, err
			}
			result.Detail += "（剩余 " + strconv.FormatInt(balance, 10) + " 次）"

		case pointstore.RewardNumberChange:
			_, note, err := grantNumberRewardTx(ctx, tx, appID, userID, reward.Type, reward.Amount, 0, usernumber.OriginLeaderboard, key, grant.Title)
			if err != nil {
				return nil, err
			}
			result.Detail += note

		case pointstore.RewardNumberTicket:
			_, note, err := grantNumberRewardTx(ctx, tx, appID, userID, reward.Type, reward.Amount, reward.RefID, usernumber.OriginLeaderboard, key, grant.Title)
			if err != nil {
				return nil, err
			}
			result.Detail += note

		case pointstore.RewardNumberDraw:
			_, note, err := grantNumberRewardTx(ctx, tx, appID, userID, reward.Type, reward.Amount, 0, usernumber.OriginLeaderboard, key, grant.Title)
			if err != nil {
				return nil, err
			}
			result.Detail += note

		case pointstore.RewardCosmetic:
			granted, err := grantCosmeticTx(ctx, tx, cosmetic.GrantInput{
				AppID: appID, UserID: userID, CosmeticID: reward.RefID, Days: int(reward.Amount),
				Source: cosmetic.SourceLeaderboard, SourceRef: key,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = granted.CosmeticName + " " + cosmetic.TermLabel(int(reward.Amount))
			if granted.AlreadyPermanent {
				result.Detail += "（已永久拥有）"
			}

		case pointstore.RewardCoupon:
			detail, _, err := grantCouponRewardTx(ctx, tx, appID, userID, reward.RefID, reward.Amount, coupon.SourceLeaderboard, key)
			if err != nil {
				return nil, err
			}
			result.Detail = detail

		default:
			return nil, fmt.Errorf("leaderboard: unsupported reward type %s", reward.Type)
		}
		out = append(out, result)
	}
	return out, nil
}
