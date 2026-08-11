package httptransport

import (
	"fmt"
	"net/http"

	paymentdomain "aegis/internal/domain/payment"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ── 退款管理端 DTO ──

// AdminPaymentRefundCreateRequest 发起退款
type AdminPaymentRefundCreateRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	OrderNo string `json:"order_no" form:"order_no" binding:"required"`
	// Amount 留空表示按剩余可退额度全额退款
	Amount string `json:"amount" form:"amount"`
	Reason string `json:"reason" form:"reason"`
	// ReverseFulfillment 是否回收已发放的权益（余额 / 积分 / 会员时长）。
	// 用指针区分「未传」与「显式 false」，未传时默认回收 —— 退款却不收回权益等同于白送。
	ReverseFulfillment *bool `json:"reverse_fulfillment" form:"reverse_fulfillment"`
}

// AdminPaymentRefundListRequest 退款单分页查询
type AdminPaymentRefundListRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	Status  string `json:"status" form:"status"`
	Method  string `json:"payment_method" form:"payment_method"`
	Keyword string `json:"keyword" form:"keyword"`
	Page    int    `json:"page" form:"page"`
	Limit   int    `json:"limit" form:"limit"`
}

// AdminPaymentRefundOrderRequest 按订单查询退款单 / 可退额度
type AdminPaymentRefundOrderRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	OrderNo string `json:"order_no" form:"order_no" binding:"required"`
}

// AdminPaymentRefundSyncRequest 主动同步退款单状态
type AdminPaymentRefundSyncRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	RefundNo string `json:"refund_no" form:"refund_no" binding:"required"`
}

// ── Handler ──

// AdminPaymentRefundable 查询订单可退款额度与渠道退款能力
func (h *Handler) AdminPaymentRefundable(c *gin.Context) {
	var req AdminPaymentRefundOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	info, err := h.payment.RefundableInfo(c.Request.Context(), req.AppID, req.OrderNo)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", info)
}

// AdminPaymentRefundCreate 发起退款
func (h *Handler) AdminPaymentRefundCreate(c *gin.Context) {
	var req AdminPaymentRefundCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 默认回收权益：不回收属于「退钱又留货」，必须由操作方显式选择
	reverse := true
	if req.ReverseFulfillment != nil {
		reverse = *req.ReverseFulfillment
	}
	refund, err := h.payment.RefundOrder(c.Request.Context(), req.AppID, req.OrderNo, service.RefundOptions{
		Amount:             req.Amount,
		Reason:             req.Reason,
		ReverseFulfillment: reverse,
		Operator:           refundOperatorLabel(c),
		ClientIP:           c.ClientIP(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "退款已提交", refund)
}

// refundOperatorLabel 退款操作留痕：记录「显示名(管理员ID)」，便于事后追责
func refundOperatorLabel(c *gin.Context) string {
	adminID, displayName := adminActor(c)
	if adminID <= 0 {
		return ""
	}
	if displayName == "" {
		return fmt.Sprintf("admin#%d", adminID)
	}
	return fmt.Sprintf("%s(#%d)", displayName, adminID)
}

// AdminPaymentRefundList 退款单分页查询
func (h *Handler) AdminPaymentRefundList(c *gin.Context) {
	var req AdminPaymentRefundListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.payment.AdminListRefunds(c.Request.Context(), req.AppID, paymentdomain.RefundListQuery{
		Status:  req.Status,
		Method:  req.Method,
		Keyword: req.Keyword,
		Page:    req.Page,
		Limit:   req.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminPaymentRefundOrderList 查询单个订单的退款记录
func (h *Handler) AdminPaymentRefundOrderList(c *gin.Context) {
	var req AdminPaymentRefundOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.payment.ListOrderRefunds(c.Request.Context(), req.AppID, req.OrderNo)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminPaymentRefundSync 主动向上游同步退款单状态
func (h *Handler) AdminPaymentRefundSync(c *gin.Context) {
	var req AdminPaymentRefundSyncRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	refund, err := h.payment.SyncRefundStatus(c.Request.Context(), req.AppID, req.RefundNo)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "同步完成", refund)
}
