package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// ── 履约兜底与补履约（docs/payment-fulfillment.md）──
//
// 履约原本只有两个触发点：渠道回调（含主动查单）与用户查单。两者都等不来时 ——
// 比如回调那次履约失败了、用户再也没打开订单页 —— 钱就一直挂着。这里补两样：
//
//	后台每 2 分钟：7 天内付款、写清了用途、还没履约的订单重试一次（仓储层条件抢占，重复执行无害）
//	控制台「待履约」：没写规范用途的老订单逐笔确认 —— 按推断出的用途补发，或标成「已在别处补发」
//
// 老订单不自动补：应用管理员多半已经手工开过会员、调过余额，自动补就是重复发放。

const (
	fulfillmentRetryWindow = 7 * 24 * time.Hour
	fulfillmentRetryBatch  = 100
)

// 补履约相关错误码
const (
	errCodeOrderNotFulfillable = 40118 // 订单当前不能补履约（未付款、已退款、已履约）
	errCodeOrderIntentUnknown  = 40119 // 推断不出订单用途，也没有指定
)

// retryPendingFulfillments 后台补履约：写清了用途却没履约的已付款订单再试一次。
func (s *PaymentService) retryPendingFulfillments() {
	if s.pg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	orders, err := s.pg.ListPaidUnfulfilledPaymentOrders(ctx, time.Now().Add(-fulfillmentRetryWindow), fulfillmentRetryBatch)
	if err != nil {
		s.log.Warn("list unfulfilled paid orders failed", zap.Error(err))
		return
	}
	done := 0
	for i := range orders {
		order := &orders[i]
		if err := s.ensureOrderFulfilled(ctx, order); err != nil {
			s.log.Warn("retry order fulfillment failed",
				zap.String("orderNo", order.OrderNo), zap.Int64("appid", order.AppID), zap.Error(err))
			continue
		}
		done++
	}
	if done > 0 {
		s.log.Info("retried pending order fulfillments", zap.Int("count", done))
	}
}

// AdminFulfillRequest 管理端补履约
type AdminFulfillRequest struct {
	// Action fulfill：按用途发放（默认）；manual：确认已在别处补发，平台不再发放
	Action string
	// Purpose / PlanID / PackageID 覆盖推断结果（推断不出、或推断错了时用）
	Purpose   string
	PlanID    int64
	PackageID int64
	Note      string
	// DryRun 只预览将写入的快照与提示，不改任何东西
	DryRun   bool
	Operator string
}

// AdminFulfillResult 补履约结果（预览时 Applied 为 false）
type AdminFulfillResult struct {
	OrderNo  string               `json:"orderNo"`
	Action   string               `json:"action"`
	DryRun   bool                 `json:"dryRun"`
	Applied  bool                 `json:"applied"`
	Purpose  string               `json:"purpose,omitempty"`
	Snapshot map[string]any       `json:"snapshot,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
	Order    *paymentdomain.Order `json:"order,omitempty"`
}

const (
	FulfillActionFulfill = "fulfill"
	FulfillActionManual  = "manual"
)

// AdminFulfillOrder 管理端处理一笔「钱收了、东西没发」的订单。
func (s *PaymentService) AdminFulfillOrder(ctx context.Context, appID int64, orderNo string, req AdminFulfillRequest) (*AdminFulfillResult, error) {
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order == nil || order.AppID != appID {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = FulfillActionFulfill
	}
	if action != FulfillActionFulfill && action != FulfillActionManual {
		return nil, apperrors.New(40000, http.StatusBadRequest, "action 只能是 fulfill 或 manual")
	}
	switch {
	case order.Status != "paid":
		return nil, apperrors.New(errCodeOrderNotFulfillable, http.StatusConflict, "订单未付款，不能补履约")
	case order.FulfillmentStatus != paymentdomain.FulfillmentNone:
		return nil, apperrors.New(errCodeOrderNotFulfillable, http.StatusConflict, "订单已履约或已标记处理，无需重复处理")
	case order.RefundStatus != "" && order.RefundStatus != "none":
		return nil, apperrors.New(errCodeOrderNotFulfillable, http.StatusConflict, "订单有退款，不能补履约")
	case order.UserID == nil || *order.UserID <= 0:
		return nil, apperrors.New(errCodeOrderNotFulfillable, http.StatusConflict, "订单没有归属用户，无法发放")
	}
	result := &AdminFulfillResult{OrderNo: order.OrderNo, Action: action, DryRun: req.DryRun}
	operator := pickString(strings.TrimSpace(req.Operator), "admin")

	if action == FulfillActionManual {
		result.Purpose = order.Purpose
		result.Snapshot = map[string]any{"manualFulfillment": map[string]any{
			"operator": operator, "note": strings.TrimSpace(req.Note), "at": time.Now().UTC().Format(time.RFC3339),
		}}
		if req.DryRun {
			result.Order = order
			return result, nil
		}
		ok, err := s.pg.MarkPaymentOrderFulfilledManually(ctx, order.ID, result.Snapshot)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperrors.New(errCodeOrderNotFulfillable, http.StatusConflict, "订单状态已变化，请刷新后重试")
		}
		_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, order.PaymentMethod, "ADMIN", "", result.Snapshot,
			"manual_fulfilled", "管理员 "+operator+" 确认已在别处补发："+strings.TrimSpace(req.Note))
		result.Applied = true
		result.Order = s.reloadOrderAfterPayment(ctx, order)
		return result, nil
	}

	// 下单时写清了用途：快照是现成的，直接重试
	if explicit := paymentdomain.CanonicalPurpose(metaString(order.Metadata, paymentdomain.MetaKeyPurpose)); explicit != "" &&
		req.Purpose == "" && req.PlanID <= 0 && req.PackageID <= 0 {
		result.Purpose = explicit
		result.Snapshot = order.Metadata
		result.Warnings = s.fulfillWarnings(ctx, order, explicit, order.Metadata)
		if req.DryRun {
			result.Order = order
			return result, nil
		}
		if err := s.ensureOrderFulfilled(ctx, order); err != nil {
			return nil, apperrors.New(50080, http.StatusInternalServerError, "履约失败："+err.Error())
		}
		result.Applied = true
		result.Order = s.reloadOrderAfterPayment(ctx, order)
		return result, nil
	}

	metadata, err := s.buildBackfillMetadata(ctx, order, req)
	if err != nil {
		return nil, err
	}
	purpose := metaString(metadata, paymentdomain.MetaKeyPurpose)
	result.Purpose = purpose
	result.Snapshot = metadata
	result.Warnings = s.fulfillWarnings(ctx, order, purpose, metadata)
	if req.DryRun {
		result.Order = order
		return result, nil
	}
	ok, err := s.pg.SetPaymentOrderIntent(ctx, order.ID, metadata)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperrors.New(errCodeOrderNotFulfillable, http.StatusConflict, "订单状态已变化，请刷新后重试")
	}
	order.Metadata = metadata
	if err := s.ensureOrderFulfilled(ctx, order); err != nil {
		return nil, apperrors.New(50080, http.StatusInternalServerError, "履约失败："+err.Error())
	}
	_ = s.pg.CreatePaymentCallbackLog(ctx, order.AppID, &order.ID, order.PaymentMethod, "ADMIN", "", metadata,
		"admin_fulfilled", strings.TrimSpace("管理员 "+operator+" 补发："+purpose+" "+strings.TrimSpace(req.Note)))
	result.Applied = true
	result.Order = s.reloadOrderAfterPayment(ctx, order)
	return result, nil
}

// buildBackfillMetadata 给老订单补上规范用途与快照：推断（或按管理员指定）出用途，再按当前的套餐 / 档位固化。
//
// 与下单时不同，这里**不校验价格**：钱已经按当时的价格收了，套餐之后改过价不影响这一单该发什么；
// 金额对不上只作为提示返回（fulfillWarnings）。
func (s *PaymentService) buildBackfillMetadata(ctx context.Context, order *paymentdomain.Order, req AdminFulfillRequest) (map[string]any, error) {
	purpose, planID, packageID := paymentdomain.InferIntent(order.ProviderType, order.Metadata)
	if raw := strings.TrimSpace(req.Purpose); raw != "" {
		if purpose = paymentdomain.CanonicalPurpose(raw); purpose == "" {
			return nil, apperrors.New(40091, http.StatusBadRequest, "不支持的订单用途: "+raw)
		}
	}
	if req.PlanID > 0 {
		planID = req.PlanID
		if purpose == "" {
			purpose = paymentdomain.PurposeVipPurchase
		}
	}
	if req.PackageID > 0 {
		packageID = req.PackageID
	}
	if purpose == "" {
		return nil, apperrors.New(errCodeOrderIntentUnknown, http.StatusBadRequest,
			"看不出这笔订单要发什么，请指定用途（会员 / 充值 / 积分）")
	}
	out := make(map[string]any, len(order.Metadata)+8)
	for k, v := range order.Metadata {
		out[k] = v
	}
	out[paymentdomain.MetaKeyPurpose] = purpose
	out["fulfillmentBackfill"] = map[string]any{
		"operator": pickString(strings.TrimSpace(req.Operator), "admin"),
		"at":       time.Now().UTC().Format(time.RFC3339),
		"note":     strings.TrimSpace(req.Note),
	}
	switch purpose {
	case paymentdomain.PurposeVipPurchase:
		if planID <= 0 {
			return nil, apperrors.New(errCodeOrderIntentUnknown, http.StatusBadRequest, "会员订单需要指定套餐")
		}
		plan, err := s.pg.GetVipPlan(ctx, order.AppID, planID)
		if err != nil {
			return nil, err
		}
		if plan == nil {
			return nil, apperrors.New(40480, http.StatusNotFound, "套餐不存在")
		}
		if plan.IsTrial() {
			return nil, apperrors.New(errCodeTrialPlanNotPurchase, http.StatusBadRequest, "试用套餐不能作为付费订单发放")
		}
		applyVipPlanSnapshot(out, plan)
	case paymentdomain.PurposeWalletRecharge:
		for _, key := range []string{paymentdomain.MetaKeyWalletPackageID, paymentdomain.MetaKeyWalletPackageTitle,
			paymentdomain.MetaKeyWalletBonus, paymentdomain.MetaKeyWalletBonusIntegral} {
			delete(out, key)
		}
		if packageID > 0 {
			p, err := s.pg.GetWalletPackage(ctx, order.AppID, packageID)
			if err != nil && !errors.Is(err, pgrepo.ErrWalletPackageNotFound) {
				return nil, err
			}
			if p != nil {
				out[paymentdomain.MetaKeyWalletPackageID] = p.ID
				out[paymentdomain.MetaKeyWalletPackageTitle] = p.Title
				out[paymentdomain.MetaKeyWalletBonus] = p.BonusAmount.StringFixed(2)
				out[paymentdomain.MetaKeyWalletBonusIntegral] = p.BonusIntegral
			}
		}
	case paymentdomain.PurposeIntegralPurchase:
		app, err := s.pg.GetAppByID(ctx, order.AppID)
		if err != nil {
			return nil, err
		}
		if app == nil {
			return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
		}
		rate := int64(resolveCommerceSettings(app).IntegralPerCurrency)
		integral := order.Amount.Mul(decimal.NewFromInt(rate)).IntPart()
		if integral <= 0 {
			return nil, apperrors.New(40090, http.StatusBadRequest, "订单金额过小，无法折算积分")
		}
		out[paymentdomain.MetaKeyIntegralAmount] = integral
	}
	return out, nil
}

// fulfillWarnings 补发前值得管理员看一眼的情况（不阻止补发）。
func (s *PaymentService) fulfillWarnings(ctx context.Context, order *paymentdomain.Order, purpose string, metadata map[string]any) []string {
	warnings := make([]string, 0)
	if order.UserID == nil {
		return warnings
	}
	switch purpose {
	case paymentdomain.PurposeVipPurchase:
		planID := metaInt64(metadata, paymentdomain.MetaKeyVipPlanID)
		plan, err := s.pg.GetVipPlan(ctx, order.AppID, planID)
		if err != nil || plan == nil {
			break
		}
		if !plan.Price.Equal(order.Amount) {
			warnings = append(warnings, fmt.Sprintf("套餐现价 %s 与订单金额 %s 不同（下单后改过价）", plan.Price.StringFixed(2), order.Amount.StringFixed(2)))
		}
		holdings, err := s.pg.GetVipHoldings(ctx, order.AppID, *order.UserID)
		if err == nil && holdings.CoversPlan(*plan) {
			warnings = append(warnings, "用户已持有覆盖该套餐的永久会员（可能已手工补发），补发会按会员折算规则折成积分；已补过请改用「标记已处理」")
		}
	case paymentdomain.PurposeWalletRecharge:
		warnings = append(warnings, fmt.Sprintf("将向余额本金入账 %s；若已用物品发放规则或手工调账补过，请改用「标记已处理」", order.Amount.StringFixed(2)))
	}
	return warnings
}

// applyVipPlanSnapshot 把套餐快照写进订单 metadata（下单与补履约共用）。
func applyVipPlanSnapshot(out map[string]any, plan *vipdomain.Plan) {
	out[paymentdomain.MetaKeyVipPlanID] = plan.ID
	out[paymentdomain.MetaKeyVipPlanName] = plan.Name
	out[paymentdomain.MetaKeyVipDays] = plan.DurationDays
	// metadata 是客户端传来的：不是永久套餐时必须把这个键删掉，
	// 否则在月卡订单里塞一个 vipLifetime: true 就能按月卡的价钱开出永久会员。
	if plan.Lifetime {
		out[paymentdomain.MetaKeyVipLifetime] = true
	} else {
		delete(out, paymentdomain.MetaKeyVipLifetime)
	}
	out[paymentdomain.MetaKeyVipBonus] = plan.BonusIntegral
	out[paymentdomain.MetaKeyVipFeatures] = vipdomain.NormalizeFeatureTags(plan.Features)
}
