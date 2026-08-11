package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// ── 退款编排 ──
//
// 退款链路的形状与支付相反，但同样要求「钱与账一致」：
//
//	校验可退 → 预占额度（落退款单，pending）→ 提交上游 → 结算回写
//	                                              ├─ 成功：额度保持占用 + 履约冲正
//	                                              ├─ 失败：释放额度，可重试
//	                                              └─ 受理中：额度保持占用，等补偿轮询/查询确认
//
// 预占先于上游调用是关键：并发的两个退款请求会在订单行锁上串行化，
// 第二个请求看到的是已被抬高的 refunded_amount，超额即被拒，
// 因此「同一笔钱被退两次」在数据库层面就不可能发生。

// RefundOptions 发起退款的可选参数
type RefundOptions struct {
	// Amount 为空表示按剩余可退额度全额退款
	Amount string
	Reason string
	// ReverseFulfillment 是否回收已发放的权益（余额/积分/会员时长），默认由调用方显式给出
	ReverseFulfillment bool
	Operator           string
	ClientIP           string
}

// resolveRefunder 取渠道的退款能力，并校验「能力声明」与「接口实现」一致
func (s *PaymentService) resolveRefunder(method string) (paymentProvider, paymentRefunder, error) {
	provider, err := s.resolveProvider(method)
	if err != nil {
		return nil, nil, err
	}
	refunder, ok := provider.(paymentRefunder)
	if !ok || !provider.Describe().Capabilities.Refund {
		return provider, nil, apperrors.New(40099, http.StatusBadRequest,
			providerLabel(provider)+" 不支持退款，请到该渠道的商户后台手动处理")
	}
	return provider, refunder, nil
}

// RefundableInfo 返回订单当前的可退款额度与渠道退款能力，供控制台在发起前展示与校验
func (s *PaymentService) RefundableInfo(ctx context.Context, appID int64, orderNo string) (*paymentdomain.RefundableInfo, error) {
	order, err := s.adminOrder(ctx, appID, orderNo)
	if err != nil {
		return nil, err
	}
	info := &paymentdomain.RefundableInfo{
		OrderNo:        order.OrderNo,
		PaymentMethod:  order.PaymentMethod,
		Amount:         order.Amount,
		RefundedAmount: order.RefundedAmount,
		Refundable:     order.Amount.Sub(order.RefundedAmount),
		RefundStatus:   order.RefundStatus,
	}
	if info.Refundable.IsNegative() {
		info.Refundable = decimal.Zero
	}

	provider, err := s.resolveProvider(order.PaymentMethod)
	if err != nil {
		info.Reason = "未知支付方式：" + order.PaymentMethod
		return info, nil
	}
	caps := provider.Describe().Capabilities
	_, isRefunder := provider.(paymentRefunder)
	info.Supported = caps.Refund && isRefunder
	info.PartialAllowed = caps.PartialRefund
	switch {
	case !info.Supported:
		info.Reason = providerLabel(provider) + " 不支持通过接口退款"
	case order.Status != "paid":
		info.Reason = "订单未处于已支付状态，不可退款"
	case !info.Refundable.IsPositive():
		info.Reason = "订单已全额退款"
	}
	return info, nil
}

// RefundOrder 发起退款
func (s *PaymentService) RefundOrder(ctx context.Context, appID int64, orderNo string, options RefundOptions) (*paymentdomain.Refund, error) {
	order, err := s.adminOrder(ctx, appID, orderNo)
	if err != nil {
		return nil, err
	}
	if order.Status != "paid" {
		return nil, apperrors.New(40100, http.StatusBadRequest, "只有已支付的订单可以退款")
	}

	provider, refunder, err := s.resolveRefunder(order.PaymentMethod)
	if err != nil {
		return nil, err
	}
	caps := provider.Describe().Capabilities

	refundable := order.Amount.Sub(order.RefundedAmount)
	if !refundable.IsPositive() {
		return nil, apperrors.New(40101, http.StatusBadRequest, "订单已无可退款额度")
	}

	amount := refundable
	if raw := strings.TrimSpace(options.Amount); raw != "" {
		parsed, perr := decimal.NewFromString(raw)
		if perr != nil || !parsed.IsPositive() {
			return nil, apperrors.New(40102, http.StatusBadRequest, "退款金额无效")
		}
		amount = parsed
	}
	if amount.GreaterThan(refundable) {
		return nil, apperrors.New(40103, http.StatusBadRequest,
			fmt.Sprintf("退款金额 %s 超出可退额度 %s", amount.StringFixed(2), refundable.StringFixed(2)))
	}
	// 只支持整单退的渠道（如 PAYJS）在发起前就拦下，不要等上游报错
	if !caps.PartialRefund && amount.LessThan(order.Amount) {
		return nil, apperrors.New(40104, http.StatusBadRequest,
			providerLabel(provider)+" 只支持整单退款，请按订单全额发起")
	}

	// 履约冲正指令：从订单 metadata 快照解析，与支付时用的是同一份
	instr, hasInstr, err := s.buildFulfillmentInstruction(order)
	if err != nil {
		return nil, apperrors.New(40105, http.StatusBadRequest, "订单履约快照无效："+err.Error())
	}
	var reverse *paymentdomain.FulfillmentInstruction
	if hasInstr {
		reverse = &instr
	}

	// 预占额度 + 落退款单（并发安全的关键一步）
	refund, err := s.pg.CreatePaymentRefund(ctx, paymentdomain.RefundCreation{
		AppID:    appID,
		Order:    order,
		RefundNo: generateRefundNo(appID),
		Amount:   amount,
		Reason:   strings.TrimSpace(options.Reason),
		Operator: strings.TrimSpace(options.Operator),
		ClientIP: strings.TrimSpace(options.ClientIP),
	})
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrOrderNotRefundable):
			return nil, apperrors.New(40100, http.StatusBadRequest, "订单不处于可退款状态")
		case errors.Is(err, pgrepo.ErrRefundAmountExceeded):
			return nil, apperrors.New(40103, http.StatusBadRequest, "退款金额超出可退额度（可能有并发退款）")
		default:
			return nil, err
		}
	}

	// 余额通道：打款也在本地事务内，退款单结算 + 钱包入账 + 冲正一次成型
	if order.PaymentMethod == paymentdomain.MethodBalance {
		settled, walletTxn, rerr := s.pg.RefundPaymentOrderToWallet(
			ctx, refund.ID, order, amount, refund.RefundNo, reverse, options.ReverseFulfillment)
		if rerr != nil {
			s.settleRefundFailure(ctx, refund, order, reverse, options, "余额退款失败："+rerr.Error())
			if errors.Is(rerr, pgrepo.ErrUserNotFound) {
				return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
			}
			return nil, rerr
		}
		s.log.Info("payment order refunded to wallet",
			zap.String("orderNo", order.OrderNo), zap.String("refundNo", refund.RefundNo),
			zap.String("walletTxn", walletTxn.TransactionNo))
		s.emitRefundHook(order, settled)
		return settled, nil
	}

	// 第三方渠道：提交上游
	result, err := refunder.Refund(ctx, s.refundConfigData(ctx, order), paymentdomain.RefundRequest{
		AppID:           appID,
		OrderNo:         order.OrderNo,
		ProviderOrderNo: order.ProviderOrderNo,
		RefundNo:        refund.RefundNo,
		Reason:          strings.TrimSpace(options.Reason),
		RefundAmount:    amount,
		TotalAmount:     order.Amount,
		NotifyURL:       order.NotifyURL,
		Metadata:        order.Metadata,
	})
	if err != nil {
		// 上游调用异常（网络/配置/参数）：释放额度，允许修正后重试
		s.settleRefundFailure(ctx, refund, order, reverse, options, err.Error())
		return nil, err
	}

	settled, serr := s.pg.SettlePaymentRefund(ctx, paymentdomain.RefundSettlement{
		RefundID:         refund.ID,
		Status:           result.Status,
		ProviderRefundNo: result.ProviderRefundNo,
		ErrorMessage:     result.Message,
		RawResponse:      result.Raw,
	}, order, reverse, options.ReverseFulfillment)
	if serr != nil {
		if errors.Is(serr, pgrepo.ErrRefundNotSettleable) {
			// 已被补偿轮询抢先结算，读取最新状态返回即可
			if latest, gerr := s.pg.GetPaymentRefundByNo(ctx, appID, refund.RefundNo); gerr == nil && latest != nil {
				return latest, nil
			}
		}
		return nil, serr
	}
	s.log.Info("payment order refund submitted",
		zap.String("orderNo", order.OrderNo), zap.String("refundNo", refund.RefundNo),
		zap.String("status", settled.Status), zap.String("amount", amount.StringFixed(2)))
	if settled.Status == paymentdomain.RefundSuccess {
		s.emitRefundHook(order, settled)
	}
	return settled, nil
}

// settleRefundFailure 把退款单置为失败以释放预占额度。
// 该操作本身出错只记日志：额度会由后续的补偿轮询兜底回收，不能因此淹没原始错误。
func (s *PaymentService) settleRefundFailure(
	ctx context.Context,
	refund *paymentdomain.Refund,
	order *paymentdomain.Order,
	reverse *paymentdomain.FulfillmentInstruction,
	options RefundOptions,
	message string,
) {
	if _, err := s.pg.SettlePaymentRefund(ctx, paymentdomain.RefundSettlement{
		RefundID:     refund.ID,
		Status:       paymentdomain.RefundFailed,
		ErrorMessage: message,
	}, order, reverse, options.ReverseFulfillment); err != nil {
		s.log.Error("release refund reservation failed",
			zap.String("refundNo", refund.RefundNo), zap.Error(err))
	}
}

// refundConfigData 取订单所用支付配置的原始配置（退款需与支付使用同一套商户凭据）
func (s *PaymentService) refundConfigData(ctx context.Context, order *paymentdomain.Order) map[string]any {
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil || config == nil {
		return map[string]any{}
	}
	return config.ConfigData
}

// SyncRefundStatus 主动向上游查询退款单状态并结算（processing 态的补偿通道）
func (s *PaymentService) SyncRefundStatus(ctx context.Context, appID int64, refundNo string) (*paymentdomain.Refund, error) {
	refund, err := s.pg.GetPaymentRefundByNo(ctx, appID, strings.TrimSpace(refundNo))
	if err != nil {
		return nil, err
	}
	if refund == nil {
		return nil, apperrors.New(40477, http.StatusNotFound, "退款单不存在")
	}
	if paymentdomain.RefundStatusIsFinal(refund.Status) {
		return refund, nil
	}
	updated, err := s.syncRefund(ctx, *refund)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// syncRefund 查询单个退款单的上游状态并回写
func (s *PaymentService) syncRefund(ctx context.Context, refund paymentdomain.Refund) (*paymentdomain.Refund, error) {
	provider, err := s.resolveProvider(refund.PaymentMethod)
	if err != nil {
		return nil, err
	}
	querier, ok := provider.(refundQuerier)
	if !ok {
		return nil, apperrors.New(40106, http.StatusBadRequest,
			providerLabel(provider)+" 不支持查询退款状态，请到该渠道商户后台核对")
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, refund.OrderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}

	result, err := querier.QueryRefund(ctx, s.refundConfigData(ctx, order), paymentdomain.RefundQuery{
		OrderNo:          refund.OrderNo,
		ProviderOrderNo:  order.ProviderOrderNo,
		RefundNo:         refund.RefundNo,
		ProviderRefundNo: refund.ProviderRefundNo,
	})
	if err != nil {
		return nil, err
	}
	// 仍未出终态：保持现状，等下一轮
	if !paymentdomain.RefundStatusIsFinal(result.Status) {
		return &refund, nil
	}

	instr, hasInstr, ierr := s.buildFulfillmentInstruction(order)
	if ierr != nil {
		return nil, apperrors.New(40105, http.StatusBadRequest, "订单履约快照无效："+ierr.Error())
	}
	var reverse *paymentdomain.FulfillmentInstruction
	if hasInstr {
		reverse = &instr
	}
	// 补偿路径统一按「回收权益」处理：发起时若选择不回收，退款单已在首次结算时记为 skipped，
	// 而 skipped 属非终态之外的标记，不会被这里重复覆盖（仅 pending/processing 才会走到此处）。
	settled, err := s.pg.SettlePaymentRefund(ctx, paymentdomain.RefundSettlement{
		RefundID:         refund.ID,
		Status:           result.Status,
		ProviderRefundNo: result.ProviderRefundNo,
		ErrorMessage:     result.Message,
		RawResponse:      result.Raw,
	}, order, reverse, true)
	if err != nil {
		if errors.Is(err, pgrepo.ErrRefundNotSettleable) {
			return &refund, nil
		}
		return nil, err
	}
	s.log.Info("payment refund settled by sync",
		zap.String("refundNo", refund.RefundNo), zap.String("status", settled.Status))
	if settled.Status == paymentdomain.RefundSuccess {
		s.emitRefundHook(order, settled)
	}
	return settled, nil
}

// ── 查询 ──

func (s *PaymentService) ListOrderRefunds(ctx context.Context, appID int64, orderNo string) ([]paymentdomain.Refund, error) {
	order, err := s.adminOrder(ctx, appID, orderNo)
	if err != nil {
		return nil, err
	}
	return s.pg.ListPaymentRefundsByOrder(ctx, order.ID)
}

func (s *PaymentService) AdminListRefunds(ctx context.Context, appID int64, query paymentdomain.RefundListQuery) (*paymentdomain.RefundListResult, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	items, total, err := s.pg.ListPaymentRefundsByApp(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	return &paymentdomain.RefundListResult{
		Items:      items,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		TotalPages: calcPaymentTotalPages(total, query.Limit),
	}, nil
}

// adminOrder 按应用取订单并校验归属
func (s *PaymentService) adminOrder(ctx context.Context, appID int64, orderNo string) (*paymentdomain.Order, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order == nil || order.AppID != appID {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	return order, nil
}

func (s *PaymentService) emitRefundHook(order *paymentdomain.Order, refund *paymentdomain.Refund) {
	if s.plugin == nil {
		return
	}
	appID := order.AppID
	meta := plugindomain.HookMetadata{AppID: &appID}
	if order.UserID != nil {
		meta.UserID = order.UserID
	}
	go s.plugin.ExecuteHook(context.Background(), HookPaymentRefunded, map[string]any{
		"orderId":  order.ID,
		"refundId": refund.ID,
		"refundNo": refund.RefundNo,
		"amount":   refund.Amount.StringFixed(2),
	}, meta)
}

// ── 未结算退款单的补偿轮询 ──

// syncPendingRefunds 扫描未达终态的退款单并向上游核对。
// 触发场景：微信/支付宝/Paddle 返回「受理中」，或结算写入时进程崩溃。
func (s *PaymentService) syncPendingRefunds() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	refunds, err := s.pg.ListUnsettledPaymentRefunds(ctx, 50)
	if err != nil {
		s.log.Warn("list unsettled payment refunds failed", zap.Error(err))
		return
	}
	for _, refund := range refunds {
		provider, perr := s.resolveProvider(refund.PaymentMethod)
		if perr != nil {
			continue
		}
		// 无查询能力的渠道靠人工核对，跳过以免刷日志
		if _, ok := provider.(refundQuerier); !ok {
			continue
		}
		if _, serr := s.syncRefund(ctx, refund); serr != nil {
			s.log.Warn("sync payment refund failed",
				zap.String("refundNo", refund.RefundNo), zap.Error(serr))
		}
	}
}

// generateRefundNo 生成本地退款单号。
// 字符集限定为数字与大写字母，兼容各渠道对 out_refund_no 的约束（微信最严：数字/字母/_-|*@）。
func generateRefundNo(appID int64) string {
	return fmt.Sprintf("R%d%s%s", appID, time.Now().UTC().Format("20060102150405"), randomDigits(6))
}
