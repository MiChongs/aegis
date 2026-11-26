package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/pointstore"
	"aegis/pkg/response"
)

// 积分商城：管理端（开关 / 商品 / 兑换记录与人工发放 / 统计 / 用户的功能解锁）
// 与用户端（商城首页 / 兑换 / 我的兑换记录）。

// ── DTO ──

// PointsStoreRewardRequest 一项权益。取值字段由权益目录的 value 决定用哪个：
// amount（整数量；功能解锁时是天数，0 为永久）、money（金额，字符串）、refId（会员套餐）、
// tag（功能标识）、text（人工发放的内容）。
type PointsStoreRewardRequest struct {
	Type   string `json:"type" binding:"required"`
	Amount int64  `json:"amount"`
	Money  string `json:"money"`
	RefID  int64  `json:"refId"`
	Tag    string `json:"tag"`
	Text   string `json:"text"`
}

// PointsStoreItemRequest 新建 / 更新一件商品。
type PointsStoreItemRequest struct {
	Title        string                     `json:"title" binding:"required"`
	Subtitle     string                     `json:"subtitle"`
	Description  string                     `json:"description"`
	Category     string                     `json:"category"`
	Badge        string                     `json:"badge"`
	Cost         int64                      `json:"cost"`
	OriginalCost *int64                     `json:"originalCost"`
	Rewards      []PointsStoreRewardRequest `json:"rewards"`
	// Delivery 发给谁：self（默认）兑换人自己 / gift 兑换人选的好友（只能配会员套餐、会员天数、功能解锁、个性装扮）
	Delivery string `json:"delivery"`
	// Stock 剩余库存，不传或 null 为不限；开了补货周期时是本周期还剩多少（不传则从满货开始）
	Stock *int64 `json:"stock"`
	// RestockPeriod 补货周期 none / day / week / month；RestockQuantity 每个周期补到多少
	RestockPeriod   string `json:"restockPeriod"`
	RestockQuantity *int64 `json:"restockQuantity"`
	// GlobalLimitPeriod / GlobalLimitCount 全站名额：所有人合计每个周期最多兑多少份，0 为不限
	GlobalLimitPeriod string `json:"globalLimitPeriod"`
	GlobalLimitCount  int    `json:"globalLimitCount"`
	LimitPeriod       string `json:"limitPeriod"`
	LimitCount        int    `json:"limitCount"`
	// UserTotalLimit 每人累计上限，与按周期限兑叠加，0 为不限
	UserTotalLimit int `json:"userTotalLimit"`
	// DailyOpenMinute / DailyCloseMinute 每日开放时段（应用默认时区，自零点起的分钟数），都不传为全天
	DailyOpenMinute  *int       `json:"dailyOpenMinute"`
	DailyCloseMinute *int       `json:"dailyCloseMinute"`
	Audience         string     `json:"audience"`
	MinLevel         int        `json:"minLevel"`
	StartsAt         *time.Time `json:"startsAt"`
	EndsAt           *time.Time `json:"endsAt"`
	Status           string     `json:"status"`
	SortOrder        int        `json:"sortOrder"`
}

// PointsStoreSettingsRequest 积分商城开关。
type PointsStoreSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

// PointsStoreItemStatusRequest 上架 / 下架。
type PointsStoreItemStatusRequest struct {
	Active bool `json:"active"`
}

// PointsStoreItemOrderRequest 商品排序：按数组顺序重写排序值。
type PointsStoreItemOrderRequest struct {
	ItemIDs []int64 `json:"itemIds" binding:"required"`
}

// PointsStoreHandleRequest 处理一笔待发放的兑换；note 会写进给用户的站内信。
type PointsStoreHandleRequest struct {
	Note string `json:"note"`
}

// PointsStoreRedeemRequest 用户兑换。requestId 由客户端生成（建议 UUID），
// 网络重试时带同一个值，不会重复扣积分。
type PointsStoreRedeemRequest struct {
	ItemID    int64  `json:"itemId" binding:"required"`
	RequestID string `json:"requestId"`
	DeviceID  string `json:"deviceId"`
}

// FeatureUnlockGrantRequest 管理员授予功能解锁；days 为 0 即永久。
type FeatureUnlockGrantRequest struct {
	Tag  string `json:"tag" binding:"required"`
	Days int    `json:"days"`
}

// FeatureUnlockRevokeRequest 收回一条功能解锁。
type FeatureUnlockRevokeRequest struct {
	Reason string `json:"reason"`
}

func (req PointsStoreItemRequest) toInput(appID int64, operator string) pointstore.ItemInput {
	rewards := make([]pointstore.Reward, 0, len(req.Rewards))
	for _, item := range req.Rewards {
		reward := pointstore.Reward{Type: item.Type, Amount: item.Amount, RefID: item.RefID, Tag: item.Tag, Text: item.Text}
		// 金额按字符串传输：JSON number 在前端是双精度浮点
		if money := strings.TrimSpace(item.Money); money != "" {
			if parsed, err := decimal.NewFromString(money); err == nil {
				reward.Money = parsed
			}
		}
		rewards = append(rewards, reward)
	}
	return pointstore.ItemInput{
		AppID:             appID,
		Title:             req.Title,
		Subtitle:          req.Subtitle,
		Description:       req.Description,
		Category:          req.Category,
		Badge:             req.Badge,
		Cost:              req.Cost,
		OriginalCost:      req.OriginalCost,
		Rewards:           rewards,
		Delivery:          strings.TrimSpace(req.Delivery),
		Stock:             req.Stock,
		RestockPeriod:     req.RestockPeriod,
		RestockQuantity:   req.RestockQuantity,
		GlobalLimitPeriod: req.GlobalLimitPeriod,
		GlobalLimitCount:  req.GlobalLimitCount,
		LimitPeriod:       req.LimitPeriod,
		LimitCount:        req.LimitCount,
		UserTotalLimit:    req.UserTotalLimit,
		DailyOpenMinute:   req.DailyOpenMinute,
		DailyCloseMinute:  req.DailyCloseMinute,
		Audience:          req.Audience,
		MinLevel:          req.MinLevel,
		StartsAt:          req.StartsAt,
		EndsAt:            req.EndsAt,
		Status:            req.Status,
		SortOrder:         req.SortOrder,
		Operator:          operator,
	}
}

// ── 管理端：目录与开关 ──

// AdminPointsStoreCatalog 权益目录、限兑周期、兑换对象与已用过的分类。控制台的商品表单由它驱动。
func (h *Handler) AdminPointsStoreCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	catalog, err := h.pointsStore.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", catalog)
}

// AdminPointsStoreSettings 积分商城开关。
func (h *Handler) AdminPointsStoreSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	settings, err := h.pointsStore.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", settings)
}

// AdminSavePointsStoreSettings 开启 / 关闭积分商城。
func (h *Handler) AdminSavePointsStoreSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req PointsStoreSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	settings, err := h.pointsStore.SaveSettings(c.Request.Context(), appID, req.Enabled, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "积分商城已关闭"
	if settings.Enabled {
		message = "积分商城已开启"
	}
	response.Success(c, http.StatusOK, message, settings)
}

// ── 管理端：商品 ──

// AdminListPointsStoreItems 商品列表（含下架的，带兑换汇总）。
func (h *Handler) AdminListPointsStoreItems(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.pointsStore.ListItems(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminCreatePointsStoreItem 新建商品。
func (h *Handler) AdminCreatePointsStoreItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req PointsStoreItemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.pointsStore.CreateItem(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "商品已创建", item)
}

// AdminUpdatePointsStoreItem 更新商品。已兑出的记录是快照，不受影响。
func (h *Handler) AdminUpdatePointsStoreItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	itemID, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req PointsStoreItemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.pointsStore.UpdateItem(c.Request.Context(), itemID, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "商品已保存", item)
}

// AdminReorderPointsStoreItems 按给定顺序重排商品（排序值 10、20、30……）。
func (h *Handler) AdminReorderPointsStoreItems(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req PointsStoreItemOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	updated, err := h.pointsStore.ReorderItems(c.Request.Context(), appID, req.ItemIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "排序已保存", gin.H{"updated": updated})
}

// AdminSetPointsStoreItemStatus 上架 / 下架。
func (h *Handler) AdminSetPointsStoreItemStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	itemID, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req PointsStoreItemStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.pointsStore.SetItemStatus(c.Request.Context(), appID, itemID, req.Active)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已下架"
	if req.Active {
		message = "已上架"
	}
	response.Success(c, http.StatusOK, message, item)
}

// AdminDeletePointsStoreItem 删除商品（兑换记录保留）。
func (h *Handler) AdminDeletePointsStoreItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	itemID, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	if err := h.pointsStore.DeleteItem(c.Request.Context(), appID, itemID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// ── 管理端：兑换记录 ──

// AdminListPointsStoreRedemptions 兑换记录（userId / itemId / status / keyword）。
func (h *Handler) AdminListPointsStoreRedemptions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	itemID, _ := strconv.ParseInt(c.Query("itemId"), 10, 64)
	page, err := h.pointsStore.ListRedemptions(c.Request.Context(), pointstore.RedemptionQuery{
		AppID:   appID,
		UserID:  userID,
		ItemID:  itemID,
		Status:  strings.TrimSpace(c.Query("status")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		From:    cardKeyQueryTime(c.Query("from")),
		To:      cardKeyQueryTime(c.Query("to")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminFulfillPointsStoreRedemption 标记一笔待发放的兑换为已发放（站内信通知用户）。
func (h *Handler) AdminFulfillPointsStoreRedemption(c *gin.Context) {
	h.handlePointsStoreRedemption(c, false)
}

// AdminCancelPointsStoreRedemption 取消一笔待发放的兑换：退回积分、库存加回（站内信通知用户）。
func (h *Handler) AdminCancelPointsStoreRedemption(c *gin.Context) {
	h.handlePointsStoreRedemption(c, true)
}

func (h *Handler) handlePointsStoreRedemption(c *gin.Context, cancel bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	redemptionID, ok := cardKeyPathID(c, "redemptionId")
	if !ok {
		return
	}
	var req PointsStoreHandleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	input := pointstore.HandleInput{AppID: appID, RedemptionID: redemptionID, Note: req.Note, Operator: operator}
	if cancel {
		redemption, err := h.pointsStore.Cancel(c.Request.Context(), input)
		if err != nil {
			h.writeError(c, err)
			return
		}
		response.Success(c, http.StatusOK, "已取消，积分已退回", redemption)
		return
	}
	redemption, err := h.pointsStore.Fulfill(c.Request.Context(), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已标记为已发放", redemption)
}

// AdminPointsStoreStats 概览与按天趋势（days，默认 14，最多 90）。
func (h *Handler) AdminPointsStoreStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.pointsStore.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14), c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// ── 管理端：用户的功能解锁 ──

// AdminUserFeatureUnlocks 一个用户的功能解锁（全部记录 + 当前生效）。
func (h *Handler) AdminUserFeatureUnlocks(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	result, err := h.pointsStore.UserUnlocks(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGrantFeatureUnlock 授予功能解锁。
func (h *Handler) AdminGrantFeatureUnlock(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	var req FeatureUnlockGrantRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	unlock, err := h.pointsStore.GrantUnlock(c.Request.Context(), pointstore.UnlockGrantInput{
		AppID: appID, UserID: userID, Tag: req.Tag, Days: req.Days, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已解锁", unlock)
}

// AdminRevokeFeatureUnlock 收回一条功能解锁。
func (h *Handler) AdminRevokeFeatureUnlock(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	unlockID, ok := cardKeyPathID(c, "unlockId")
	if !ok {
		return
	}
	var req FeatureUnlockRevokeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	unlock, err := h.pointsStore.RevokeUnlock(c.Request.Context(), appID, userID, unlockID, operator, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已收回", unlock)
}

// ── 用户端 ──

// AppPointsStore 积分商城首页：余额、等级、商品与每件商品对当前用户的可兑换判定。
// 未开启时 enabled=false、items 为空（不报错），客户端据此隐藏入口。
func (h *Handler) AppPointsStore(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	store, err := h.pointsStore.Store(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", store)
}

// AppRedeemPointsStoreItem 兑换一件商品。
func (h *Handler) AppRedeemPointsStoreItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req PointsStoreRedeemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	result, err := h.pointsStore.Redeem(c.Request.Context(), pointstore.RedeemInput{
		AppID:     session.AppID,
		UserID:    session.UserID,
		ItemID:    req.ItemID,
		RequestID: req.RequestID,
		DeviceID:  deviceID,
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "兑换成功"
	if result.Redemption.Status == pointstore.RedemptionPending {
		message = "兑换成功，等待发放"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppPointsStoreRedemptions 我的兑换记录。
func (h *Handler) AppPointsStoreRedemptions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	page, err := h.pointsStore.MyRedemptions(c.Request.Context(), session.AppID, session.UserID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}
