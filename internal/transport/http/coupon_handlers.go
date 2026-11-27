package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/coupon"
	paymentdomain "aegis/internal/domain/payment"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 优惠系统（docs/coupons.md）：管理端（目录 / 概览 / 券 / 发券 / 用户券 / 活动 / 订单优惠 / 试算）与
// 用户端（领券中心 / 领取 / 我的券 / 活动与套餐活动价 / 下单试算）。

// ── DTO ──

// CouponAudienceRequest 人群条件。
type CouponAudienceRequest struct {
	Kind        string `json:"kind"`
	NewUserDays int    `json:"newUserDays"`
}

func (a CouponAudienceRequest) domain() coupon.Audience {
	return coupon.Audience{Kind: a.Kind, NewUserDays: a.NewUserDays}
}

// CouponTemplateRequest 新建 / 修改券。金额为十进制字符串或数字。
type CouponTemplateRequest struct {
	Name               string                `json:"name" binding:"required"`
	Description        string                `json:"description"`
	Theme              string                `json:"theme"`
	Kind               string                `json:"kind" binding:"required"`
	Amount             decimal.Decimal       `json:"amount"`
	Rate               int                   `json:"rate"`
	MaxDiscount        decimal.Decimal       `json:"maxDiscount"`
	MinAmount          decimal.Decimal       `json:"minAmount"`
	Scopes             []string              `json:"scopes"`
	PlanIDs            []int64               `json:"planIds"`
	StackWithPromotion bool                  `json:"stackWithPromotion"`
	ValidityMode       string                `json:"validityMode" binding:"required"`
	ValidFrom          *time.Time            `json:"validFrom"`
	ValidUntil         *time.Time            `json:"validUntil"`
	ValidDays          int                   `json:"validDays"`
	ValidDelayDays     int                   `json:"validDelayDays"`
	TotalQuantity      int64                 `json:"totalQuantity"`
	PerUserLimit       int                   `json:"perUserLimit"`
	Claimable          bool                  `json:"claimable"`
	ClaimStartsAt      *time.Time            `json:"claimStartsAt"`
	ClaimEndsAt        *time.Time            `json:"claimEndsAt"`
	Audience           CouponAudienceRequest `json:"audience"`
	RefundReturn       bool                  `json:"refundReturn"`
}

func (req CouponTemplateRequest) toInput(appID int64, operator string) coupon.TemplateInput {
	return coupon.TemplateInput{
		AppID: appID, Name: req.Name, Description: req.Description, Theme: req.Theme,
		Kind: req.Kind, Amount: req.Amount, Rate: req.Rate, MaxDiscount: req.MaxDiscount, MinAmount: req.MinAmount,
		Scopes: req.Scopes, PlanIDs: req.PlanIDs, StackWithPromotion: req.StackWithPromotion,
		ValidityMode: req.ValidityMode, ValidFrom: req.ValidFrom, ValidUntil: req.ValidUntil,
		ValidDays: req.ValidDays, ValidDelayDays: req.ValidDelayDays,
		TotalQuantity: req.TotalQuantity, PerUserLimit: req.PerUserLimit, Claimable: req.Claimable,
		ClaimStartsAt: req.ClaimStartsAt, ClaimEndsAt: req.ClaimEndsAt, Audience: req.Audience.domain(),
		RefundReturn: req.RefundReturn, Operator: operator,
	}
}

// CouponPromotionRequest 新建 / 修改活动。
type CouponPromotionRequest struct {
	Name            string                `json:"name" binding:"required"`
	Description     string                `json:"description"`
	Tag             string                `json:"tag"`
	Theme           string                `json:"theme"`
	Kind            string                `json:"kind" binding:"required"`
	Rule            coupon.PromotionRule  `json:"rule"`
	Priority        int                   `json:"priority"`
	Scopes          []string              `json:"scopes"`
	PlanIDs         []int64               `json:"planIds"`
	PackageIDs      []int64               `json:"packageIds"`
	Audience        CouponAudienceRequest `json:"audience"`
	StackWithCoupon bool                  `json:"stackWithCoupon"`
	StartsAt        *time.Time            `json:"startsAt"`
	EndsAt          *time.Time            `json:"endsAt"`
	PerUserLimit    int                   `json:"perUserLimit"`
	TotalLimit      int64                 `json:"totalLimit"`
	Budget          decimal.Decimal       `json:"budget"`
}

func (req CouponPromotionRequest) toInput(appID int64, operator string) coupon.PromotionInput {
	return coupon.PromotionInput{
		AppID: appID, Name: req.Name, Description: req.Description, Tag: req.Tag, Theme: req.Theme,
		Kind: req.Kind, Rule: req.Rule, Priority: req.Priority, Scopes: req.Scopes, PlanIDs: req.PlanIDs,
		PackageIDs: req.PackageIDs, Audience: req.Audience.domain(), StackWithCoupon: req.StackWithCoupon,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, PerUserLimit: req.PerUserLimit, TotalLimit: req.TotalLimit,
		Budget: req.Budget, Operator: operator,
	}
}

// CouponStatusRequest 改状态：draft / active / paused / ended。
type CouponStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// CouponIssueRequest 管理员发券：userIds 为管理端用户 ID（一次最多 500 人），count 每人张数（1–20）。
type CouponIssueRequest struct {
	UserIDs []int64 `json:"userIds" binding:"required"`
	Count   int     `json:"count"`
	// Notify 发站内信告诉用户（默认否，只推实时刷新）
	Notify bool `json:"notify"`
}

// CouponRevokeRequest 收回一张券。
type CouponRevokeRequest struct {
	Reason string `json:"reason"`
}

// CouponTemplateQuery 券列表查询。
type CouponTemplateQuery struct {
	Status  string `form:"status"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// CouponPromotionQuery 活动列表查询。
type CouponPromotionQuery struct {
	Kind    string `form:"kind"`
	Status  string `form:"status"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// CouponUserCouponQuery 用户券查询。
type CouponUserCouponQuery struct {
	TemplateID int64  `form:"templateId"`
	UserID     int64  `form:"userId"`
	Status     string `form:"status"`
	Source     string `form:"source"`
	Keyword    string `form:"keyword"`
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
}

// CouponOrderQuery 订单优惠记录查询。
type CouponOrderQuery struct {
	PromotionID int64  `form:"promotionId"`
	TemplateID  int64  `form:"templateId"`
	UserID      int64  `form:"userId"`
	Status      string `form:"status"`
	Keyword     string `form:"keyword"`
	Page        int    `form:"page"`
	Limit       int    `form:"limit"`
}

// CouponOverviewQuery 概览查询。
type CouponOverviewQuery struct {
	TZ   string `form:"tz"`
	Days int    `form:"days"`
}

// AdminCouponQuoteRequest 管理端替某个用户试算（只算不写）。
type AdminCouponQuoteRequest struct {
	UserID     int64  `json:"userId" binding:"required"`
	Purpose    string `json:"purpose"`
	PlanID     int64  `json:"planId"`
	PackageID  int64  `json:"packageId"`
	Amount     string `json:"amount"`
	CouponID   int64  `json:"couponId"`
	CouponMode string `json:"couponMode"`
}

// CouponClaimRequest 领券。
type CouponClaimRequest struct {
	TemplateID int64 `json:"templateId" binding:"required"`
}

// MyCouponsQuery 我的券。status：usable（可用与未生效，默认）/ available / not_started / locked / used / expired / revoked / 空为全部。
type MyCouponsQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// PaymentQuoteRequest 下单前试算。字段与 POST /pay/orders 同名同义；amount 可不填（会员、按档位充值时由服务端定价）。
type PaymentQuoteRequest struct {
	Purpose    string         `json:"purpose"`
	PlanID     int64          `json:"plan_id"`
	PackageID  int64          `json:"package_id"`
	Amount     string         `json:"amount"`
	Type       string         `json:"type"`
	Metadata   map[string]any `json:"metadata"`
	CouponID   int64          `json:"coupon_id"`
	CouponMode string         `json:"coupon_mode"`
}

func couponIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的编号")
		return 0, false
	}
	return id, true
}

// ── 管理端 ──

func (h *Handler) AdminCouponCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	response.Success(c, http.StatusOK, "ok", h.coupons.Catalog())
}

func (h *Handler) AdminCouponOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q CouponOverviewQuery
	_ = c.ShouldBindQuery(&q)
	overview, err := h.coupons.Overview(c.Request.Context(), appID, q.TZ, q.Days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", overview)
}

func (h *Handler) AdminListCouponTemplates(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q CouponTemplateQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.coupons.ListTemplates(c.Request.Context(), coupon.TemplateQuery{
		AppID: appID, Status: q.Status, Keyword: q.Keyword, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AdminCreateCouponTemplate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CouponTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	t, err := h.coupons.CreateTemplate(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "优惠券已创建", t)
}

func (h *Handler) AdminGetCouponTemplate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "templateId")
	if !ok {
		return
	}
	t, err := h.coupons.GetTemplate(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", t)
}

func (h *Handler) AdminUpdateCouponTemplate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "templateId")
	if !ok {
		return
	}
	var req CouponTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	t, err := h.coupons.UpdateTemplate(c.Request.Context(), id, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "优惠券已保存", t)
}

func (h *Handler) AdminSetCouponTemplateStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "templateId")
	if !ok {
		return
	}
	var req CouponStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	t, err := h.coupons.SetTemplateStatus(c.Request.Context(), appID, id, req.Status, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "状态已更新", t)
}

func (h *Handler) AdminDeleteCouponTemplate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "templateId")
	if !ok {
		return
	}
	if err := h.coupons.DeleteTemplate(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "优惠券已删除", gin.H{"id": id})
}

func (h *Handler) AdminIssueCoupons(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "templateId")
	if !ok {
		return
	}
	var req CouponIssueRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.Count == 0 {
		req.Count = 1
	}
	_, operator := adminAccount(c)
	outcomes, err := h.coupons.Issue(c.Request.Context(), appID, id, req.UserIDs, req.Count, operator, req.Notify)
	if err != nil {
		h.writeError(c, err)
		return
	}
	succeeded := 0
	for _, o := range outcomes {
		if o.OK {
			succeeded++
		}
	}
	response.Success(c, http.StatusOK, "已发给 "+strconv.Itoa(succeeded)+" 人", gin.H{"items": outcomes, "succeeded": succeeded})
}

func (h *Handler) AdminListUserCoupons(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q CouponUserCouponQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.coupons.ListUserCoupons(c.Request.Context(), coupon.UserCouponQuery{
		AppID: appID, UserID: q.UserID, TemplateID: q.TemplateID, Status: q.Status, Source: q.Source,
		Keyword: q.Keyword, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AdminRevokeUserCoupon(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "couponId")
	if !ok {
		return
	}
	var req CouponRevokeRequest
	_ = bind(c, &req)
	item, err := h.coupons.RevokeUserCoupon(c.Request.Context(), appID, id, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已收回", item)
}

func (h *Handler) AdminListCouponPromotions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q CouponPromotionQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.coupons.ListPromotions(c.Request.Context(), coupon.PromotionQuery{
		AppID: appID, Kind: q.Kind, Status: q.Status, Keyword: q.Keyword, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AdminCreateCouponPromotion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CouponPromotionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	p, err := h.coupons.CreatePromotion(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已创建", p)
}

func (h *Handler) AdminGetCouponPromotion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "promotionId")
	if !ok {
		return
	}
	p, err := h.coupons.GetPromotion(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", p)
}

func (h *Handler) AdminUpdateCouponPromotion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "promotionId")
	if !ok {
		return
	}
	var req CouponPromotionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	p, err := h.coupons.UpdatePromotion(c.Request.Context(), id, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已保存", p)
}

func (h *Handler) AdminSetCouponPromotionStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "promotionId")
	if !ok {
		return
	}
	var req CouponStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	p, err := h.coupons.SetPromotionStatus(c.Request.Context(), appID, id, req.Status, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "状态已更新", p)
}

func (h *Handler) AdminDeleteCouponPromotion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := couponIDParam(c, "promotionId")
	if !ok {
		return
	}
	if err := h.coupons.DeletePromotion(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已删除", gin.H{"id": id})
}

func (h *Handler) AdminListCouponOrders(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var q CouponOrderQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.coupons.ListOrders(c.Request.Context(), coupon.OrderPromotionQuery{
		AppID: appID, UserID: q.UserID, PromotionID: q.PromotionID, TemplateID: q.TemplateID,
		Status: q.Status, Keyword: q.Keyword, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AdminCouponQuote(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminCouponQuoteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	amount, err := h.quoteAmount(c, appID, req.Purpose, req.PlanID, req.PackageID, req.Amount)
	if err != nil {
		h.writeError(c, err)
		return
	}
	quote, _, err := h.coupons.Quote(c.Request.Context(), service.QuoteRequest{
		AppID: appID, UserID: req.UserID, Purpose: paymentdomain.CanonicalPurpose(req.Purpose), PlanID: req.PlanID, PackageID: req.PackageID,
		Amount: amount, CouponID: req.CouponID, CouponMode: req.CouponMode,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", quote)
}

// quoteAmount 试算用的原价：会员取套餐价，按档位充值取档位价，否则用请求里的金额。
func (h *Handler) quoteAmount(c *gin.Context, appID int64, purpose string, planID, packageID int64, amount string) (decimal.Decimal, error) {
	price, err := h.payment.ListPrice(c.Request.Context(), appID, paymentdomain.CanonicalPurpose(purpose), planID, packageID, amount)
	if err != nil {
		return decimal.Zero, err
	}
	return price, nil
}

// ── 用户端 ──

func (h *Handler) AppCouponCenter(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.coupons.Center(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

func (h *Handler) AppClaimCoupon(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CouponClaimRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.coupons.Claim(c.Request.Context(), session.AppID, session.UserID, req.TemplateID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "领取成功", item)
}

func (h *Handler) AppMyCoupons(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var q MyCouponsQuery
	_ = c.ShouldBindQuery(&q)
	if _, present := c.GetQuery("status"); !present {
		q.Status = "usable"
	}
	result, err := h.coupons.MyCoupons(c.Request.Context(), session.AppID, session.UserID, q.Status, q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

func (h *Handler) AppPromotions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	view, err := h.coupons.Promotions(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", view)
}

// PaymentQuote 下单前试算：活动、可用的券与最后要付多少。下单时带上同样的参数（与 expected_amount）即得同一个价格。
func (h *Handler) PaymentQuote(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentQuoteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	intent, err := h.payment.QuoteIntent(req.Type, service.OrderIntent{Purpose: req.Purpose, PlanID: req.PlanID, PackageID: req.PackageID}, req.Metadata)
	if err != nil {
		h.writeError(c, err)
		return
	}
	amount, err := h.quoteAmount(c, session.AppID, intent.Purpose, intent.PlanID, intent.PackageID, req.Amount)
	if err != nil {
		h.writeError(c, err)
		return
	}
	quote, _, err := h.coupons.Quote(c.Request.Context(), service.QuoteRequest{
		AppID: session.AppID, UserID: session.UserID, Purpose: intent.Purpose, PlanID: intent.PlanID, PackageID: intent.PackageID,
		Amount: amount, CouponID: req.CouponID, CouponMode: req.CouponMode,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", quote)
}
