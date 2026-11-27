package postgres

import (
	"aegis/internal/domain/coupon"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/aigateway"
	"aegis/internal/domain/cosmetic"
	"aegis/internal/domain/featuretime"
	"aegis/internal/domain/itemgrant"
	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/usernumber"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
)

// ErrItemGrantRefMissing 物品引用的套餐 / 功能不存在或已停用（整笔发放回滚）。
var ErrItemGrantRefMissing = errors.New("item grant: referenced plan or feature is unavailable")

// itemGrantContext 一笔发放的上下文。
type itemGrantContext struct {
	AppID    int64
	UserID   int64
	RecordID int64
	RecordNo string
	// Title 流水标题（规则名）；Source 流水说明
	Title  string
	Source string
	Meta   map[string]any
	Refs   itemgrant.Refs
	Now    time.Time
}

// grantItemTx 在发放事务里发一项物品。调用方已锁住用户行。
//
// 这里的 switch 与 itemgrant 的物品目录**双向钉死**（TestItemCatalogHasGrantBranch）：
// 目录多一档 → 配得上却发不出来；这里多一档 → 控制台配不出来。
// 每一档都走那个账本自己的事务内入口，所以从这里发出去的东西与积分商城、卡密、抽奖发的完全同口径
// （会员折算、抽奖次数的批次与扣除顺序、功能时长账户、排行榜分类）。
func (r *Repository) grantItemTx(ctx context.Context, tx pgx.Tx, grant itemGrantContext, index int,
	planned itemgrant.PlannedItem) (itemgrant.ItemResult, error) {
	item := planned.Item
	spec, _ := itemgrant.FindItemSpec(item.Type)
	result := itemgrant.ItemResult{
		NodeID: planned.NodeID, OptionID: planned.OptionID, Type: item.Type, Label: spec.Label,
		Detail: itemgrant.DescribeItem(item, grant.Refs),
	}
	appID, userID := grant.AppID, grant.UserID
	recordID := grant.RecordID

	switch item.Type {
	case itemgrant.ItemPoints:
		_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, userID, appID, item.Amount,
			"earn", itemgrant.IntegralCategory, grant.Title, grant.Source, itemgrant.SourceType, &recordID, grant.Meta)
		if err != nil {
			return result, err
		}
		result.Detail += "（余额 " + strconv.FormatInt(after, 10) + "）"
		result.TransactionNo = txnNo

	case itemgrant.ItemExperience:
		exp, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, item.Amount,
			itemgrant.IntegralCategory, grant.Title, grant.Source, itemgrant.SourceType, &recordID, grant.Meta)
		if err != nil {
			return result, err
		}
		if exp.LevelChanged {
			result.Detail += "（升至 Lv." + strconv.Itoa(exp.NewLevel) + "）"
		}
		result.TransactionNo = exp.TransactionNo

	case itemgrant.ItemVipDays:
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanName: grant.Title, DurationDays: int(item.Amount),
			PayChannel: vipdomain.ChannelItemGrant, PayAmount: decimal.Zero, Metadata: grant.Meta, ConvertOwned: true,
		})
		if err != nil {
			return result, err
		}
		result.TransactionNo = txn.TransactionNo
		applyItemGrantConversion(&result, txn)

	case itemgrant.ItemVipPlan:
		plan, err := scanVipPlan(tx.QueryRow(ctx,
			`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, item.RefID))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
		if plan == nil || plan.IsTrial() {
			return result, fmt.Errorf("%w：会员套餐 #%d", ErrItemGrantRefMissing, item.RefID)
		}
		planID := plan.ID
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanID: &planID, PlanName: plan.Name, Features: plan.Features,
			DurationDays: plan.DurationDays, Lifetime: plan.Lifetime, BonusIntegral: plan.BonusIntegral,
			PayChannel: vipdomain.ChannelItemGrant, PayAmount: decimal.Zero, Metadata: grant.Meta, ConvertOwned: true,
		})
		if err != nil {
			return result, err
		}
		result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
		result.TransactionNo = txn.TransactionNo
		applyItemGrantConversion(&result, txn)

	case itemgrant.ItemVipLifetime:
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanName: grant.Title, Lifetime: true,
			PayChannel: vipdomain.ChannelItemGrant, PayAmount: decimal.Zero, Metadata: grant.Meta, ConvertOwned: true,
		})
		if err != nil {
			return result, err
		}
		result.TransactionNo = txn.TransactionNo
		applyItemGrantConversion(&result, txn)

	case itemgrant.ItemFeature:
		if feature, ok := grant.Refs.Features[item.Tag]; !ok || !feature.IsActive {
			return result, fmt.Errorf("%w：功能 %s", ErrItemGrantRefMissing, item.Tag)
		}
		outcome, err := grantFeatureUnlockOrConvertTx(ctx, tx, FeatureUnlockGrant{
			AppID: appID, UserID: userID, Tag: item.Tag, Days: int(item.Amount),
			Source: vipdomain.UnlockSourceItemGrant, SourceRef: grant.RecordNo,
		}, vipdomain.ChannelItemGrant)
		if errors.Is(err, ErrFeatureUnlockOwned) {
			// 已永久拥有且策略是照常发放：没有可记的，写明而不是让整笔失败
			result.Detail += "（已永久拥有）"
			result.Converted = true
			break
		}
		if err != nil {
			return result, err
		}
		if outcome.Conversion != nil {
			result.Detail = outcome.Conversion.Message
			result.TransactionNo = outcome.Conversion.ConversionNo
			result.Converted = true
			break
		}
		result.ExpireAt = outcome.Unlock.EndsAt
		result.TransactionNo = strconv.FormatInt(outcome.Unlock.ID, 10)

	case itemgrant.ItemFeatureTime:
		balance, err := grantFeatureTimeTx(ctx, tx, appID, userID, item.Tag, item.Amount*60,
			featuretime.RefItemGrant, grant.RecordNo, grant.Source)
		if err != nil {
			return result, err
		}
		result.Detail += "（余额 " + strconv.FormatInt(featuretime.FormatMinutes(balance), 10) + " 分钟）"

	case itemgrant.ItemAdFree:
		expires, err := grantAdFreePassTx(ctx, tx, appID, userID, int(item.Amount), grant.Now)
		if err != nil {
			return result, err
		}
		result.ExpireAt = &expires

	case itemgrant.ItemLotteryDraws:
		balance, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
			AppID: appID, UserID: userID, Amount: item.Amount, Origin: lotterydomain.TicketOriginItemGrant,
			Ref: grant.RecordNo, Title: grant.Title,
		})
		if err != nil {
			return result, err
		}
		result.Detail += "（现有 " + strconv.FormatInt(balance, 10) + " 次）"

	case itemgrant.ItemAICredit:
		balance, err := grantAIGatewayCreditTx(ctx, tx, appID, userID, item.Amount*aigateway.MicroPerPoint,
			itemgrant.SourceType, grant.RecordNo, grant.Source)
		if err != nil {
			return result, err
		}
		result.Detail += "（余额 " + aigateway.MicroToPoints(balance).Round(2).String() + " 点）"

	case itemgrant.ItemBalance:
		change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
			UserID: userID, AppID: appID, Type: walletdomain.TxnTypeItemGrant, Amount: item.Money,
			Title: grant.Title, Remark: grant.Source, Metadata: grant.Meta,
			// 一条记录里可能有两项余额：幂等键落到「这条记录的第几项」
			IdempotencyKey: "itemgrant:" + grant.RecordNo + ":" + strconv.Itoa(index),
		})
		if err != nil {
			return result, err
		}
		result.TransactionNo = change.Transaction.TransactionNo

	case itemgrant.ItemLicenseDays:
		expiresAt, err := extendLoginLicenseTx(ctx, tx, appID, userID, int(item.Amount))
		if err != nil {
			return result, err
		}
		result.Detail += "（授权至 " + expiresAt.In(time.Local).Format(time.DateOnly) + "）"
		result.ExpireAt = &expiresAt

	case itemgrant.ItemDeviceSlots:
		total, err := addDeviceSlotsTx(ctx, tx, appID, userID, int(item.Amount))
		if err != nil {
			return result, err
		}
		result.Detail += "（上限 " + strconv.Itoa(total) + " 台）"

	case itemgrant.ItemManual:
		result.Pending = true

	case itemgrant.ItemNumberChange:
		_, note, err := grantNumberRewardTx(ctx, tx, appID, userID, item.Type, item.Amount, 0, usernumber.OriginItemGrant, grant.RecordNo, grant.Title)
		if err != nil {
			return result, err
		}
		result.Detail += note

	case itemgrant.ItemNumberTicket:
		_, note, err := grantNumberRewardTx(ctx, tx, appID, userID, item.Type, item.Amount, item.RefID, usernumber.OriginItemGrant, grant.RecordNo, grant.Title)
		if err != nil {
			return result, err
		}
		result.Detail += note

	case itemgrant.ItemNumberDraw:
		_, note, err := grantNumberRewardTx(ctx, tx, appID, userID, item.Type, item.Amount, 0, usernumber.OriginItemGrant, grant.RecordNo, grant.Title)
		if err != nil {
			return result, err
		}
		result.Detail += note

	case itemgrant.ItemNumberAssign:
		note, err := assignNumberRewardTx(ctx, tx, appID, userID, item.Text, grant.Title)
		if err != nil {
			return result, err
		}
		result.Detail += note

	case itemgrant.ItemCosmetic:
		granted, err := grantCosmeticTx(ctx, tx, cosmetic.GrantInput{
			AppID: appID, UserID: userID, CosmeticID: item.RefID, Days: int(item.Amount),
			Source: cosmetic.SourceItemGrant, SourceRef: grant.RecordNo, Equip: item.Equip,
		})
		if err != nil {
			return result, err
		}
		if granted.Equipped {
			result.Equipped = true
			result.Detail += "（已戴上）"
		}
		if granted.AlreadyPermanent {
			result.Detail += "（已永久拥有）"
			result.Converted = true
			break
		}
		result.ExpireAt = granted.Ownership.EndsAt
		result.TransactionNo = strconv.FormatInt(granted.Ownership.ID, 10)

	case itemgrant.ItemCoupon:
		detail, ids, err := grantCouponRewardTx(ctx, tx, appID, userID, item.RefID, item.Amount, coupon.SourceItemGrant, grant.RecordNo)
		if err != nil {
			return result, err
		}
		result.Detail = detail
		if len(ids) > 0 {
			result.TransactionNo = strconv.FormatInt(ids[0], 10)
		}

	default:
		// 目录里有、这里没有 —— 由测试挡在合并之前
		return result, fmt.Errorf("item grant: unsupported item type %s", item.Type)
	}
	return result, nil
}

// applyItemGrantConversion 会员类物品对这个人是「已拥有」时，按折算策略改发：结果写明折成了什么。
func applyItemGrantConversion(result *itemgrant.ItemResult, txn *vipdomain.Transaction) {
	var conversion *vipdomain.ConversionSummary
	applyVipConversionDetail(&result.Detail, &conversion, txn)
	if conversion != nil {
		result.Converted = true
	}
}

// itemGrantRefsQ 应用的会员套餐与功能目录（含停用的）。
func itemGrantRefsQ(ctx context.Context, q queryExecutor, appID int64) (itemgrant.Refs, error) {
	refs := itemgrant.Refs{Plans: map[int64]itemgrant.PlanRef{}, Features: map[string]itemgrant.FeatureRef{},
		Cosmetics: map[int64]itemgrant.CosmeticRef{}}
	cosmetics, err := cosmeticRefsQ(ctx, q, appID)
	if err != nil {
		return refs, err
	}
	if refs.Coupons, err = couponRefsQ(ctx, q, appID); err != nil {
		return refs, err
	}
	for id, ref := range cosmetics {
		refs.Cosmetics[id] = itemgrant.CosmeticRef{ID: ref.ID, Name: ref.Name, Slot: ref.Slot, Live: ref.Live}
	}
	rows, err := q.Query(ctx, `SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1`, appID)
	if err != nil {
		return refs, err
	}
	for rows.Next() {
		plan, err := scanVipPlan(rows)
		if err != nil {
			rows.Close()
			return refs, err
		}
		if plan == nil {
			continue
		}
		refs.Plans[plan.ID] = itemgrant.PlanRef{
			ID: plan.ID, Name: plan.Name, DurationDays: plan.DurationDays, Lifetime: plan.Lifetime,
			Trial: plan.IsTrial(), Active: plan.IsActive,
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return refs, err
	}
	featureRows, err := q.Query(ctx, `SELECT tag, name, is_active FROM vip_features WHERE appid = $1`, appID)
	if err != nil {
		return refs, err
	}
	defer featureRows.Close()
	for featureRows.Next() {
		var ref itemgrant.FeatureRef
		if err := featureRows.Scan(&ref.Tag, &ref.Name, &ref.IsActive); err != nil {
			return refs, err
		}
		refs.Features[ref.Tag] = ref
	}
	return refs, featureRows.Err()
}

// ItemGrantRefs 应用的会员套餐与功能目录。
func (r *Repository) ItemGrantRefs(ctx context.Context, appID int64) (itemgrant.Refs, error) {
	return itemGrantRefsQ(ctx, r.pool, appID)
}
