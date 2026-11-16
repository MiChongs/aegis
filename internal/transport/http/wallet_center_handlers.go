package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	walletdomain "aegis/internal/domain/wallet"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ── 用户端：钱包总览与兑换 ──

// WalletExchangeRequest 充值余额兑换积分。amount 与 all 二选一；比例由服务端决定。
type WalletExchangeRequest struct {
	Amount         string `json:"amount"`
	All            bool   `json:"all"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
}

// MyWalletOverview GET /api/wallet/overview
func (h *Handler) MyWalletOverview(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.wallet.Overview(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取钱包总览成功", result)
}

// WalletExchange POST /api/wallet/exchange
func (h *Handler) WalletExchange(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req WalletExchangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.wallet.Exchange(c.Request.Context(), session, service.WalletExchangeRequest{
		Amount: req.Amount, All: req.All, IdempotencyKey: req.IdempotencyKey,
		ClientIP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "兑换成功"
	if result.Replayed {
		message = "重复请求，返回首次兑换结果"
	}
	response.Success(c, 200, message, result)
}

// ── 管理端：钱包设置 ──

// AdminWalletSettingsRequest 钱包设置。金额用字符串，避免浮点。
type AdminWalletSettingsRequest struct {
	RechargeEnabled     bool   `json:"rechargeEnabled"`
	CustomAmountEnabled bool   `json:"customAmountEnabled"`
	RechargeMin         string `json:"rechargeMin"`
	RechargeMax         string `json:"rechargeMax"`
	ExchangeEnabled     bool   `json:"exchangeEnabled"`
	ExchangeMin         string `json:"exchangeMin"`
	ExchangeMax         string `json:"exchangeMax"`
	ExchangeDailyMax    string `json:"exchangeDailyMax"`
	Notice              string `json:"notice"`
}

// AdminAppWalletSettings GET /api/admin/apps/:appkey/wallet/settings
func (h *Handler) AdminAppWalletSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.wallet.AdminGetSettings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// UpdateAdminAppWalletSettings PUT /api/admin/apps/:appkey/wallet/settings
func (h *Handler) UpdateAdminAppWalletSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminWalletSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	settings := walletdomain.Settings{
		RechargeEnabled: req.RechargeEnabled, CustomAmountEnabled: req.CustomAmountEnabled,
		ExchangeEnabled: req.ExchangeEnabled, Notice: req.Notice,
	}
	for _, f := range []struct {
		name string
		raw  string
		dst  *decimal.Decimal
	}{
		{"rechargeMin", req.RechargeMin, &settings.RechargeMin},
		{"rechargeMax", req.RechargeMax, &settings.RechargeMax},
		{"exchangeMin", req.ExchangeMin, &settings.ExchangeMin},
		{"exchangeMax", req.ExchangeMax, &settings.ExchangeMax},
		{"exchangeDailyMax", req.ExchangeDailyMax, &settings.ExchangeDailyMax},
	} {
		value, err := parseMoneyField(f.raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, f.name+" 金额格式无效")
			return
		}
		*f.dst = value
	}
	result, err := h.wallet.AdminUpdateSettings(c.Request.Context(), appID, settings)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "钱包设置已更新", result)
}

// parseMoneyField 空串按 0 处理（「不限」类字段留空即可）。
func parseMoneyField(raw string) (decimal.Decimal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(raw)
}

// ── 管理端：充值档位 ──

// AdminWalletPackageRequest 充值档位。
type AdminWalletPackageRequest struct {
	Title         string     `json:"title"`
	Amount        string     `json:"amount"`
	BonusAmount   string     `json:"bonusAmount"`
	BonusIntegral int64      `json:"bonusIntegral"`
	Badge         string     `json:"badge"`
	Description   string     `json:"description"`
	PerUserLimit  int        `json:"perUserLimit"`
	IsActive      bool       `json:"isActive"`
	SortOrder     int        `json:"sortOrder"`
	StartsAt      *time.Time `json:"startsAt"`
	EndsAt        *time.Time `json:"endsAt"`
}

// AdminWalletPackageOrderRequest 档位排序。
type AdminWalletPackageOrderRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

// AdminAppWalletPackages GET /api/admin/apps/:appkey/wallet/packages
func (h *Handler) AdminAppWalletPackages(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.wallet.AdminListPackages(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items})
}

// CreateAdminAppWalletPackage POST /api/admin/apps/:appkey/wallet/packages
func (h *Handler) CreateAdminAppWalletPackage(c *gin.Context) {
	h.saveWalletPackage(c, 0)
}

// UpdateAdminAppWalletPackage PUT /api/admin/apps/:appkey/wallet/packages/:packageId
func (h *Handler) UpdateAdminAppWalletPackage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("packageId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "档位ID无效")
		return
	}
	h.saveWalletPackage(c, id)
}

func (h *Handler) saveWalletPackage(c *gin.Context, id int64) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminWalletPackageRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "充值金额格式无效")
		return
	}
	bonus, err := parseMoneyField(req.BonusAmount)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "赠送余额格式无效")
		return
	}
	saved, err := h.wallet.AdminSavePackage(c.Request.Context(), appID, walletdomain.RechargePackage{
		ID: id, Title: req.Title, Amount: amount, BonusAmount: bonus, BonusIntegral: req.BonusIntegral,
		Badge: req.Badge, Description: req.Description, PerUserLimit: req.PerUserLimit,
		IsActive: req.IsActive, SortOrder: req.SortOrder, StartsAt: req.StartsAt, EndsAt: req.EndsAt,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "充值档位已保存", saved)
}

// DeleteAdminAppWalletPackage DELETE /api/admin/apps/:appkey/wallet/packages/:packageId
func (h *Handler) DeleteAdminAppWalletPackage(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("packageId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "档位ID无效")
		return
	}
	if err := h.wallet.AdminDeletePackage(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "充值档位已删除", gin.H{"id": id})
}

// ReorderAdminAppWalletPackages PUT /api/admin/apps/:appkey/wallet/packages-order
func (h *Handler) ReorderAdminAppWalletPackages(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminWalletPackageOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.wallet.AdminReorderPackages(c.Request.Context(), appID, req.IDs); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "排序已保存", gin.H{"ids": req.IDs})
}
