package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/promo"
	"aegis/pkg/response"
)

// 推广活动（docs/promo.md）：管理端（目录 / 活动 / 审核 / 统计 / 排行）与用户端（活动列表与详情 / 排行 /
// 领取邀请档位 / 我的提交 / 提交文章）。

// ── DTO ──

// PromoTierRequest 邀请任务的一档。
type PromoTierRequest struct {
	Count   int                   `json:"count"`
	Title   string                `json:"title"`
	Rewards []InviteRewardRequest `json:"rewards"`
}

// PromoInviteTaskRequest 邀请任务。
type PromoInviteTaskRequest struct {
	Enabled         bool               `json:"enabled"`
	Tiers           []PromoTierRequest `json:"tiers"`
	Leaderboard     bool               `json:"leaderboard"`
	LeaderboardSize int                `json:"leaderboardSize"`
}

// PromoShareTaskRequest 分享任务。
type PromoShareTaskRequest struct {
	Enabled                bool                  `json:"enabled"`
	Platforms              []string              `json:"platforms"`
	Rewards                []InviteRewardRequest `json:"rewards"`
	MaxApprovedPerUser     int                   `json:"maxApprovedPerUser"`
	MaxApprovedPerPlatform int                   `json:"maxApprovedPerPlatform"`
	DailySubmitLimit       int                   `json:"dailySubmitLimit"`
	BlockedDomains         []string              `json:"blockedDomains"`
	ShareTemplate          string                `json:"shareTemplate"`
}

// PromoConfigRequest 活动配置。
type PromoConfigRequest struct {
	ClaimGraceDays int                    `json:"claimGraceDays"`
	Invite         PromoInviteTaskRequest `json:"invite"`
	Share          PromoShareTaskRequest  `json:"share"`
}

// PromoCampaignRequest 新建 / 修改活动。
type PromoCampaignRequest struct {
	Title     string             `json:"title" binding:"required"`
	Subtitle  string             `json:"subtitle"`
	RulesText string             `json:"rulesText"`
	StartsAt  time.Time          `json:"startsAt" binding:"required"`
	EndsAt    time.Time          `json:"endsAt" binding:"required"`
	Config    PromoConfigRequest `json:"config"`
}

// PromoStatusRequest 改活动状态：draft / online / offline。
type PromoStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// PromoReviewRequest 批量审核：action 为 approve / reject，驳回必须带 reason。
type PromoReviewRequest struct {
	IDs    []int64 `json:"ids" binding:"required"`
	Action string  `json:"action" binding:"required"`
	Reason string  `json:"reason"`
}

// PromoCampaignQuery 管理端活动列表的查询参数。
type PromoCampaignQuery struct {
	Status  string `form:"status"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// PromoSubmissionQuery 管理端提交列表的查询参数。
type PromoSubmissionQuery struct {
	CampaignID int64  `form:"campaignId"`
	UserID     int64  `form:"userId"`
	Status     string `form:"status"`
	Platform   string `form:"platform"`
	Keyword    string `form:"keyword"`
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
}

// PromoClaimRequest 领取一档：count 为档位人数。
type PromoClaimRequest struct {
	Count int `json:"count" binding:"required"`
}

// PromoSubmitRequest 提交一篇推广文章。
type PromoSubmitRequest struct {
	Platform string `json:"platform" binding:"required"`
	URL      string `json:"url" binding:"required"`
	Note     string `json:"note"`
	DeviceID string `json:"deviceId"`
}

func (req PromoCampaignRequest) toInput(appID int64, operator string) promo.CampaignInput {
	tiers := make([]promo.Tier, 0, len(req.Config.Invite.Tiers))
	for _, tier := range req.Config.Invite.Tiers {
		tiers = append(tiers, promo.Tier{Count: tier.Count, Title: tier.Title, Rewards: inviteRewards(tier.Rewards)})
	}
	share := req.Config.Share
	return promo.CampaignInput{
		AppID: appID, Title: req.Title, Subtitle: req.Subtitle, RulesText: req.RulesText,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, Operator: operator,
		Config: promo.Config{
			ClaimGraceDays: req.Config.ClaimGraceDays,
			Invite: promo.InviteTask{
				Enabled: req.Config.Invite.Enabled, Tiers: tiers,
				Leaderboard: req.Config.Invite.Leaderboard, LeaderboardSize: req.Config.Invite.LeaderboardSize,
			},
			Share: promo.ShareTask{
				Enabled: share.Enabled, Platforms: share.Platforms, Rewards: inviteRewards(share.Rewards),
				MaxApprovedPerUser: share.MaxApprovedPerUser, MaxApprovedPerPlatform: share.MaxApprovedPerPlatform,
				DailySubmitLimit: share.DailySubmitLimit, BlockedDomains: share.BlockedDomains, ShareTemplate: share.ShareTemplate,
			},
		},
	}
}

func promoIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的编号")
		return 0, false
	}
	return id, true
}

func (h *Handler) resolvePromoBoard(c *gin.Context, appID int64, board *promo.Leaderboard, keepIdentity bool) {
	for index := range board.Items {
		item := &board.Items[index]
		if h.avatar != nil && item.UserID != 0 {
			item.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
				appID, item.UserID, item.RawAvatar, item.Account)
		}
		if !keepIdentity {
			item.UserID, item.Account = 0, ""
		}
	}
}

// ── 管理端 ──

func (h *Handler) AdminPromoCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	response.Success(c, http.StatusOK, "ok", h.promo.Catalog())
}

func (h *Handler) AdminListPromoCampaigns(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query PromoCampaignQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.promo.ListCampaigns(c.Request.Context(), promo.CampaignQuery{
		AppID: appID, Status: query.Status, Keyword: query.Keyword, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AdminCreatePromoCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req PromoCampaignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	campaign, err := h.promo.CreateCampaign(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已创建", campaign)
}

func (h *Handler) AdminGetPromoCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	campaign, err := h.promo.GetCampaign(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", campaign)
}

func (h *Handler) AdminUpdatePromoCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	var req PromoCampaignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	campaign, err := h.promo.UpdateCampaign(c.Request.Context(), id, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已保存", campaign)
}

func (h *Handler) AdminSetPromoCampaignStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	var req PromoStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	campaign, err := h.promo.SetStatus(c.Request.Context(), appID, id, req.Status, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动状态已更新", campaign)
}

func (h *Handler) AdminDeletePromoCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	if err := h.promo.DeleteCampaign(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "活动已删除", gin.H{"id": id})
}

func (h *Handler) AdminPromoCampaignStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	stats, err := h.promo.Stats(c.Request.Context(), appID, id, c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

func (h *Handler) AdminPromoCampaignLeaderboard(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	board, err := h.promo.AdminLeaderboard(c.Request.Context(), appID, id, parsePositiveInt(c.Query("limit"), 100))
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolvePromoBoard(c, appID, board, true)
	response.Success(c, http.StatusOK, "ok", board)
}

func (h *Handler) AdminListPromoSubmissions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query PromoSubmissionQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.promo.ListSubmissions(c.Request.Context(), promo.SubmissionQuery{
		AppID: appID, CampaignID: query.CampaignID, UserID: query.UserID, Status: query.Status,
		Platform: query.Platform, Keyword: query.Keyword, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if h.avatar != nil {
		for index := range page.Items {
			item := &page.Items[index]
			item.UserAvatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
				appID, item.UserID, item.UserAvatar, item.UserAccount)
		}
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AdminReviewPromoSubmissions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req PromoReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	outcomes, err := h.promo.Review(c.Request.Context(), promo.ReviewInput{
		AppID: appID, IDs: req.IDs, Action: req.Action, Reason: req.Reason, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	succeeded := 0
	for _, outcome := range outcomes {
		if outcome.OK {
			succeeded++
		}
	}
	response.Success(c, http.StatusOK, "已处理 "+strconv.Itoa(succeeded)+" 条", gin.H{"items": outcomes, "succeeded": succeeded})
}

// ── 用户端 ──

func (h *Handler) AppPromoCampaigns(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.promo.Campaigns(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"campaigns": items})
}

func (h *Handler) AppPromoCampaign(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	campaign, err := h.promo.Campaign(c.Request.Context(), session.AppID, session.UserID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", campaign)
}

func (h *Handler) AppPromoLeaderboard(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	board, err := h.promo.Leaderboard(c.Request.Context(), session.AppID, session.UserID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolvePromoBoard(c, session.AppID, board, false)
	response.Success(c, http.StatusOK, "ok", board)
}

func (h *Handler) AppClaimPromoTier(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	var req PromoClaimRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	grant, err := h.promo.ClaimTier(c.Request.Context(), session.AppID, session.UserID, id, req.Count)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "领取成功", grant)
}

func (h *Handler) AppPromoSubmissions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	page, err := h.promo.MySubmissions(c.Request.Context(), session.AppID, session.UserID, id,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

func (h *Handler) AppSubmitPromo(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, ok := promoIDParam(c, "campaignId")
	if !ok {
		return
	}
	var req PromoSubmitRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	item, err := h.promo.Submit(c.Request.Context(), promo.SubmitInput{
		AppID: session.AppID, CampaignID: id, UserID: session.UserID, Platform: req.Platform, URL: req.URL,
		Note: req.Note, DeviceID: deviceID, ClientIP: c.ClientIP(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已提交，等待审核", item)
}
