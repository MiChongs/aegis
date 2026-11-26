package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// AI 额度的收费面（docs/ai-billing.md）：价格方案、额度商店、额度批次、新人礼（用户端 /api/v1/apps/{appKey}/ai/*），
// 以及额度包、订单、批次、经营统计、价格模拟（管理端 /api/admin/apps/{appkey}/ai-gateway/*）。

// AIGatewayPurchaseRequest 买额度包。requestId 8-64 位，同一编号重放返回原订单；expectedPrice 是页面上展示的实付，
// 与当前报价不符时 40236。
type AIGatewayPurchaseRequest struct {
	RequestID     string           `json:"requestId"`
	ExpectedPrice *decimal.Decimal `json:"expectedPrice"`
}

// AIGatewayPackageRequest 新建 / 修改额度包（整体替换）。额度单位为点，钱单位为元。
type AIGatewayPackageRequest struct {
	Name           string          `json:"name"`
	Subtitle       string          `json:"subtitle"`
	Description    string          `json:"description"`
	Badge          string          `json:"badge"`
	Tone           string          `json:"tone"`
	Points         decimal.Decimal `json:"points"`
	BonusPoints    decimal.Decimal `json:"bonusPoints"`
	Price          decimal.Decimal `json:"price"`
	OriginalPrice  decimal.Decimal `json:"originalPrice"`
	ValidityDays   int             `json:"validityDays"`
	Audience       string          `json:"audience"`
	TierIDs        []int64         `json:"tierIds"`
	MemberDiscount *bool           `json:"memberDiscount"`
	PerUserLimit   int             `json:"perUserLimit"`
	MonthlyLimit   int             `json:"monthlyLimit"`
	Stock          int             `json:"stock"`
	StartsAt       *time.Time      `json:"startsAt"`
	EndsAt         *time.Time      `json:"endsAt"`
	Featured       bool            `json:"featured"`
	Enabled        *bool           `json:"enabled"`
	SortOrder      int             `json:"sortOrder"`
}

func (req AIGatewayPackageRequest) toInput() aigateway.PackageInput {
	return aigateway.PackageInput{Name: req.Name, Subtitle: req.Subtitle, Description: req.Description, Badge: req.Badge,
		Tone: req.Tone, Points: req.Points, BonusPoints: req.BonusPoints, Price: req.Price, OriginalPrice: req.OriginalPrice,
		ValidityDays: req.ValidityDays, Audience: req.Audience, TierIDs: req.TierIDs, MemberDiscount: req.MemberDiscount,
		PerUserLimit: req.PerUserLimit, MonthlyLimit: req.MonthlyLimit, Stock: req.Stock, StartsAt: req.StartsAt,
		EndsAt: req.EndsAt, Featured: req.Featured, Enabled: req.Enabled, SortOrder: req.SortOrder}
}

// AIGatewayPackageStatusRequest 上架 / 下架。
type AIGatewayPackageStatusRequest struct {
	Enabled bool `json:"enabled"`
}

// AIGatewayReasonRequest 带原因的操作（订单退款、作废批次）。
type AIGatewayReasonRequest struct {
	Reason string `json:"reason"`
}

// AIGatewayLotGrantRequest 管理员发放限期额度：amount 点，validityDays 0 为永久，requestId 幂等。
type AIGatewayLotGrantRequest struct {
	Amount       decimal.Decimal `json:"amount"`
	ValidityDays int             `json:"validityDays"`
	Title        string          `json:"title"`
	RequestID    string          `json:"requestId"`
}

// AIGatewayPricingPreviewRequest 价格模拟（settings 为草稿，不传用已保存的）。
type AIGatewayPricingPreviewRequest struct {
	Settings *aigateway.Settings `json:"settings"`
	Model    string              `json:"model"`
	Kind     string              `json:"kind"`
	TierID   int64               `json:"tierId"`
	IsVip    bool                `json:"isVip"`
	Level    int                 `json:"level"`
	At       *time.Time          `json:"at"`
}

func aiGatewayQueryTime(c *gin.Context, keys ...string) *time.Time {
	for _, key := range keys {
		if value := cardKeyQueryTime(c.Query(key)); value != nil {
			return value
		}
	}
	return nil
}

// ── 用户端 ──

// AppAIPricing GET /api/v1/apps/:appkey/ai/pricing
func (h *Handler) AppAIPricing(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	plan, err := h.aiGateway.PricingPlan(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", plan)
}

// AppAIPackages GET /api/v1/apps/:appkey/ai/packages
func (h *Handler) AppAIPackages(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	store, err := h.aiGateway.Store(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", store)
}

// AppAIPurchasePackage POST /api/v1/apps/:appkey/ai/packages/:packageId/purchase
func (h *Handler) AppAIPurchasePackage(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	packageID, ok := cardKeyPathID(c, "packageId")
	if !ok {
		return
	}
	var req AIGatewayPurchaseRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	result, err := h.aiGateway.Purchase(c.Request.Context(), session, packageID, requestID, req.ExpectedPrice, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AppAIOrders GET /api/v1/apps/:appkey/ai/orders
func (h *Handler) AppAIOrders(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	page, err := h.aiGateway.MyOrders(c.Request.Context(), session,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppAILots GET /api/v1/apps/:appkey/ai/lots
func (h *Handler) AppAILots(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	page, err := h.aiGateway.MyLots(c.Request.Context(), session, strings.TrimSpace(c.Query("scope")),
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 50))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppAIClaimWelcome POST /api/v1/apps/:appkey/ai/welcome
func (h *Handler) AppAIClaimWelcome(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	result, err := h.aiGateway.ClaimWelcome(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// ── 管理端 ──

// AdminAIGatewayPricingPreview POST /api/admin/apps/:appkey/ai-gateway/pricing/preview
func (h *Handler) AdminAIGatewayPricingPreview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req AIGatewayPricingPreviewRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	preview, err := h.aiGateway.PricingPreview(c.Request.Context(), appID, service.AIGatewayPricingPreviewInput{
		Settings: req.Settings, Model: req.Model, Kind: req.Kind, TierID: req.TierID, IsVip: req.IsVip, Level: req.Level, At: req.At,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", preview)
}

// AdminAIGatewayPackages GET /api/admin/apps/:appkey/ai-gateway/packages
func (h *Handler) AdminAIGatewayPackages(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	items, err := h.aiGateway.ListPackages(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AdminCreateAIGatewayPackage POST /api/admin/apps/:appkey/ai-gateway/packages
func (h *Handler) AdminCreateAIGatewayPackage(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req AIGatewayPackageRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	pkg, err := h.aiGateway.CreatePackage(c.Request.Context(), appID, req.toInput())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", pkg)
}

// AdminUpdateAIGatewayPackage PUT /api/admin/apps/:appkey/ai-gateway/packages/:packageId
func (h *Handler) AdminUpdateAIGatewayPackage(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	packageID, ok := cardKeyPathID(c, "packageId")
	if !ok {
		return
	}
	var req AIGatewayPackageRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	pkg, err := h.aiGateway.UpdatePackage(c.Request.Context(), appID, packageID, req.toInput())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", pkg)
}

// AdminSetAIGatewayPackageStatus PUT /api/admin/apps/:appkey/ai-gateway/packages/:packageId/status
func (h *Handler) AdminSetAIGatewayPackageStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	packageID, ok := cardKeyPathID(c, "packageId")
	if !ok {
		return
	}
	var req AIGatewayPackageStatusRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	pkg, err := h.aiGateway.SetPackageEnabled(c.Request.Context(), appID, packageID, req.Enabled)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", pkg)
}

// AdminDeleteAIGatewayPackage DELETE /api/admin/apps/:appkey/ai-gateway/packages/:packageId
func (h *Handler) AdminDeleteAIGatewayPackage(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	packageID, ok := cardKeyPathID(c, "packageId")
	if !ok {
		return
	}
	if err := h.aiGateway.DeletePackage(c.Request.Context(), appID, packageID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"deleted": true})
}

// AdminAIGatewayPackageOrders GET /api/admin/apps/:appkey/ai-gateway/package-orders
func (h *Handler) AdminAIGatewayPackageOrders(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	query := aigateway.OrderQuery{
		AppID: appID, Status: strings.TrimSpace(c.Query("status")), Keyword: strings.TrimSpace(c.Query("keyword")),
		From: aiGatewayQueryTime(c, "from", "start"), To: aiGatewayQueryTime(c, "to", "end"),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	}
	query.UserID, _ = strconv.ParseInt(c.Query("userId"), 10, 64)
	query.PackageID, _ = strconv.ParseInt(c.Query("packageId"), 10, 64)
	page, err := h.aiGateway.ListOrders(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminRefundAIGatewayPackageOrder POST /api/admin/apps/:appkey/ai-gateway/package-orders/:orderNo/refund
func (h *Handler) AdminRefundAIGatewayPackageOrder(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req AIGatewayReasonRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	_, operator := adminAccount(c)
	order, err := h.aiGateway.RefundOrder(c.Request.Context(), aigateway.OrderRefundInput{
		AppID: appID, OrderNo: c.Param("orderNo"), Reason: req.Reason, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", order)
}

// AdminAIGatewayLots GET /api/admin/apps/:appkey/ai-gateway/lots
func (h *Handler) AdminAIGatewayLots(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	query := aigateway.LotQuery{
		AppID: appID, Status: strings.TrimSpace(c.Query("status")), Origin: strings.TrimSpace(c.Query("origin")),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	}
	query.UserID, _ = strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.aiGateway.ListLots(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminGrantAIGatewayLot POST /api/admin/apps/:appkey/ai-gateway/users/:userId/lots
func (h *Handler) AdminGrantAIGatewayLot(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req AIGatewayLotGrantRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	_, operator := adminAccount(c)
	lot, err := h.aiGateway.GrantLot(c.Request.Context(), aigateway.LotGrantInput{
		AppID: appID, UserID: userID, Title: req.Title, Amount: req.Amount, ValidityDays: req.ValidityDays,
		RequestID: requestID, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", lot)
}

// AdminRevokeAIGatewayLot POST /api/admin/apps/:appkey/ai-gateway/lots/:lotId/revoke
func (h *Handler) AdminRevokeAIGatewayLot(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	lotID, ok := cardKeyPathID(c, "lotId")
	if !ok {
		return
	}
	var req AIGatewayReasonRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	_, operator := adminAccount(c)
	lot, err := h.aiGateway.RevokeLot(c.Request.Context(), appID, lotID, req.Reason, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", lot)
}

// AdminAIGatewayCommerce GET /api/admin/apps/:appkey/ai-gateway/commerce
func (h *Handler) AdminAIGatewayCommerce(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	stats, err := h.aiGateway.CommerceStats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 30))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}
