package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ── DTO ──

type WalletConsumeRequest struct {
	Amount         string `json:"amount" binding:"required"`
	Title          string `json:"title" binding:"required"`
	Remark         string `json:"remark"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
}

type WalletTransactionsQuery struct {
	Type  string `form:"type" json:"type"`
	Page  int    `form:"page" json:"page"`
	Limit int    `form:"limit" json:"limit"`
}

type VipPurchaseRequest struct {
	PlanID         int64  `json:"planId" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
}

type AdminWalletAdjustRequest struct {
	UserID int64  `json:"userId" binding:"required"`
	Amount string `json:"amount" binding:"required"`
	Reason string `json:"reason"`
}

type AdminVipPlanRequest struct {
	ID            int64   `json:"id"`
	Name          *string `json:"name"`
	DurationDays  *int    `json:"durationDays"`
	Price         *string `json:"price"`
	OriginalPrice *string `json:"originalPrice"`
	BonusIntegral *int64  `json:"bonusIntegral"`
	Description   *string `json:"description"`
	IsActive      *bool   `json:"isActive"`
	SortOrder     *int    `json:"sortOrder"`
}

type AdminVipGrantRequest struct {
	UserID        int64  `json:"userId" binding:"required"`
	Days          int    `json:"days" binding:"required"`
	Reason        string `json:"reason"`
	BonusIntegral int64  `json:"bonusIntegral"`
}

// ── 用户端：钱包 ──

// MyWallet GET /api/wallet
func (h *Handler) MyWallet(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	w, err := h.wallet.GetMyWallet(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取钱包成功", w)
}

// MyWalletTransactions GET /api/wallet/transactions
func (h *Handler) MyWalletTransactions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query WalletTransactionsQuery
	_ = c.ShouldBindQuery(&query)
	result, err := h.wallet.ListMyTransactions(c.Request.Context(), session, walletdomain.ListQuery{
		Type: query.Type, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取钱包流水成功", result)
}

// WalletConsume POST /api/wallet/consume
func (h *Handler) WalletConsume(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req WalletConsumeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40080, "消费金额格式无效")
		return
	}
	result, err := h.wallet.Consume(c.Request.Context(), session, amount, req.Title, req.Remark, req.IdempotencyKey, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "消费成功"
	if result.Replayed {
		message = "重复请求，返回首次消费结果"
	}
	response.Success(c, 200, message, result)
}

// ── 用户端：VIP ──

// VipPlans GET /api/vip/plans
func (h *Handler) VipPlans(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	plans, err := h.vip.ListActivePlans(c.Request.Context(), session.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取套餐列表成功", plans)
}

// MyVipStatus GET /api/vip/status
func (h *Handler) MyVipStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.vip.MyStatus(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取会员状态成功", status)
}

// MyVipTransactions GET /api/vip/transactions
func (h *Handler) MyVipTransactions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, total, err := h.vip.MyTransactions(c.Request.Context(), session, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取会员记录成功", gin.H{
		"items": items, "total": total, "page": page, "limit": limit,
	})
}

// PurchaseVip POST /api/vip/purchase （余额支付）
func (h *Handler) PurchaseVip(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req VipPurchaseRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.vip.PurchaseWithWallet(c.Request.Context(), session, req.PlanID, req.IdempotencyKey, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "购买成功", result)
}

// ── 管理端：VIP 套餐 ──

// AdminAppVipPlans GET /api/admin/apps/:appkey/vip/plans
func (h *Handler) AdminAppVipPlans(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	plans, err := h.vip.AdminListPlans(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取套餐列表成功", plans)
}

// AdminSaveAppVipPlan POST /api/admin/apps/:appkey/vip/plans
func (h *Handler) AdminSaveAppVipPlan(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminVipPlanRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	mutation := vipdomain.PlanMutation{
		ID:            req.ID,
		AppID:         appID,
		Name:          req.Name,
		DurationDays:  req.DurationDays,
		BonusIntegral: req.BonusIntegral,
		Description:   req.Description,
		IsActive:      req.IsActive,
		SortOrder:     req.SortOrder,
	}
	if req.Price != nil {
		price, err := decimal.NewFromString(strings.TrimSpace(*req.Price))
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40087, "套餐价格格式无效")
			return
		}
		mutation.Price = &price
	}
	if req.OriginalPrice != nil {
		op, err := decimal.NewFromString(strings.TrimSpace(*req.OriginalPrice))
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40087, "套餐原价格式无效")
			return
		}
		mutation.OriginalPrice = &op
	}
	plan, err := h.vip.AdminSavePlan(c.Request.Context(), mutation)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存套餐成功", plan)
}

// AdminDeleteAppVipPlan DELETE /api/admin/apps/:appkey/vip/plans/:planId
func (h *Handler) AdminDeleteAppVipPlan(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	planID, err := strconv.ParseInt(c.Param("planId"), 10, 64)
	if err != nil || planID <= 0 {
		response.Error(c, http.StatusBadRequest, 40084, "套餐ID无效")
		return
	}
	if err := h.vip.AdminDeletePlan(c.Request.Context(), appID, planID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除套餐成功", gin.H{"deleted": true})
}

// AdminGrantAppUserVip POST /api/admin/apps/:appkey/vip/grant
func (h *Handler) AdminGrantAppUserVip(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminVipGrantRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	txn, err := h.vip.AdminGrantVip(c.Request.Context(), req.UserID, appID, req.Days, req.Reason, req.BonusIntegral, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "授予 VIP 成功", txn)
}

// AdminAppVipTransactions GET /api/admin/apps/:appkey/vip/transactions
func (h *Handler) AdminAppVipTransactions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.DefaultQuery("userId", "0"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, total, err := h.vip.AdminListTransactions(c.Request.Context(), appID, userID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取会员记录成功", gin.H{
		"items": items, "total": total, "page": page, "limit": limit,
	})
}

// ── 管理端：钱包 ──

// AdminAppUserWallet GET /api/admin/apps/:appkey/users/:userId/wallet
func (h *Handler) AdminAppUserWallet(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(c.Param("userId"), 10, 64)
	if err != nil || userID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "用户ID无效")
		return
	}
	w, err := h.wallet.AdminGetWallet(c.Request.Context(), userID, appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取用户钱包成功", w)
}

// AdminAppUserWalletTransactions GET /api/admin/apps/:appkey/users/:userId/wallet/transactions
func (h *Handler) AdminAppUserWalletTransactions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(c.Param("userId"), 10, 64)
	if err != nil || userID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "用户ID无效")
		return
	}
	var query WalletTransactionsQuery
	_ = c.ShouldBindQuery(&query)
	result, err := h.wallet.AdminListTransactions(c.Request.Context(), userID, appID, walletdomain.ListQuery{
		Type: query.Type, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取用户钱包流水成功", result)
}

// AdminAdjustAppUserWallet POST /api/admin/apps/:appkey/wallet/adjust
func (h *Handler) AdminAdjustAppUserWallet(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminWalletAdjustRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40080, "调整金额格式无效")
		return
	}
	_, operator := adminAccount(c)
	result, err := h.wallet.AdminAdjust(c.Request.Context(), req.UserID, appID, amount, req.Reason, operator, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "余额调整成功", result)
}
