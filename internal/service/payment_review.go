package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	notificationdomain "aegis/internal/domain/notification"
	paymentdomain "aegis/internal/domain/payment"
)

// 订单待人工处理（迁移 000150，docs/payment-review.md）。
//
// 爱发电的自选金额付款页上付款人可以改金额。多付照常结单（applyVerifiedResult 记下实付与差额）；
// 少付、或者付款人改不了金额的渠道金额对不上（不正常，可能是配置或渠道问题），不自动结单，
// 把订单标成待人工处理：之后不再每 2 分钟查一次单，并给应用管理员与超级管理员各发一条站内信。

// payerAdjustableAmount 可选能力：付款页上付款人能自己改金额的渠道。
type payerAdjustableAmount interface {
	PayerAdjustsAmount() bool
}

// payerAdjustsAmount 这个渠道的付款人能不能改金额（能改时多付按已支付处理）。
func (s *PaymentService) payerAdjustsAmount(method string) bool {
	provider, ok := s.providers[method]
	if !ok {
		return false
	}
	adjustable, ok := provider.(payerAdjustableAmount)
	return ok && adjustable.PayerAdjustsAmount()
}

// flagOrderForReview 标成待人工处理；第一次标记时通知管理员（重复到达的回调与查单不再通知）。
func (s *PaymentService) flagOrderForReview(ctx context.Context, order *paymentdomain.Order, method string,
	paidAmount decimal.Decimal, note string) {
	first, err := s.pg.FlagPaymentOrderForReview(ctx, order.ID, paymentdomain.ReviewAmountMismatch, note, paidAmount)
	if err != nil {
		s.log.Warn("标记订单待人工处理失败", zap.String("orderNo", order.OrderNo), zap.Error(err))
		return
	}
	if !first {
		return
	}
	s.log.Warn("订单金额与实付不一致，已标为待人工处理", zap.String("orderNo", order.OrderNo),
		zap.String("amount", order.Amount.StringFixed(2)), zap.String("paid", paidAmount.StringFixed(2)))
	go s.notifyOrderReview(context.WithoutCancel(ctx), *order, method, paidAmount)
}

// notifyOrderReview 给应用管理员与超级管理员各发一条站内信（同一笔订单只进一次收件箱）。
func (s *PaymentService) notifyOrderReview(ctx context.Context, order paymentdomain.Order, method string, paidAmount decimal.Decimal) {
	if s.inbox == nil {
		return
	}
	recipients := map[int64]bool{}
	if ids, err := s.pg.ListAppAdminIDs(ctx, order.AppID); err == nil {
		for _, id := range ids {
			recipients[id] = true
		}
	} else {
		s.log.Warn("查询应用管理员失败", zap.Int64("appid", order.AppID), zap.Error(err))
	}
	if ids, err := s.pg.ListSuperAdminIDs(ctx); err == nil {
		for _, id := range ids {
			recipients[id] = true
		}
	} else {
		s.log.Warn("查询超级管理员失败", zap.Error(err))
	}
	if len(recipients) == 0 {
		return
	}
	user := "未知用户"
	if order.UserID != nil {
		user = "用户 " + strconv.FormatInt(*order.UserID, 10)
	}
	content := fmt.Sprintf("订单 %s（%s，%s）：订单金额 %s，渠道实付 %s，未自动结单、也不会再自动查单。"+
		"请核实后处理（补发商品或退款）。", order.OrderNo, order.Subject, user, order.Amount.StringFixed(2),
		paidAmount.StringFixed(2))
	pushes := make([]notificationdomain.AdminInboxPush, 0, len(recipients))
	for adminID := range recipients {
		pushes = append(pushes, notificationdomain.AdminInboxPush{
			AdminID:    adminID,
			Type:       "payment_review",
			Title:      fmt.Sprintf("订单实付与订单金额不一致：%s", order.Subject),
			Content:    content,
			Level:      notificationdomain.AdminLevelWarning,
			Resource:   "payment_order",
			ResourceID: order.OrderNo,
			Link:       fmt.Sprintf("/commerce?tab=orders&appid=%d", order.AppID),
			DedupeKey:  "payment:review:" + order.OrderNo,
			Metadata: map[string]any{"orderNo": order.OrderNo, "method": method, "amount": order.Amount.StringFixed(2),
				"paidAmount": paidAmount.StringFixed(2)},
		})
	}
	if _, err := s.inbox.Push(ctx, pushes); err != nil {
		s.log.Warn("订单待人工处理的通知投递失败", zap.String("orderNo", order.OrderNo), zap.Error(err))
	}
}
