package httptransport

import (
	"net/http"

	ticketdomain "aegis/internal/domain/ticket"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 有效反馈奖励积分（docs/feedback-rewards.md）。权限判定全在 TicketService：
// 发放 / 撤回要能处理这条反馈 + 该应用的积分调整权限；改策略要 ticket:manage。

// FeedbackRewardPolicyRequest 保存反馈奖励策略。appid 也可放在查询参数里。上限字段 0 表示不限。
type FeedbackRewardPolicyRequest struct {
	AppID            int64            `json:"appid"`
	Enabled          bool             `json:"enabled"`
	DefaultPoints    int64            `json:"defaultPoints"`
	CategoryPoints   map[string]int64 `json:"categoryPoints"`
	MaxPoints        int64            `json:"maxPoints"`
	UserDailyLimit   int64            `json:"userDailyLimit"`
	UserMonthlyLimit int64            `json:"userMonthlyLimit"`
}

// FeedbackRewardGrantRequest 发放奖励。points 省略或为 0 时按策略的建议值发；requestId 防重复提交。
type FeedbackRewardGrantRequest struct {
	Points    int64  `json:"points"`
	Reason    string `json:"reason"`
	RequestID string `json:"requestId"`
}

// FeedbackRewardRevokeRequest 撤回奖励。allowPartial 为真时余额不足扣到 0 为止，否则整笔拒绝。
type FeedbackRewardRevokeRequest struct {
	Reason       string `json:"reason"`
	AllowPartial bool   `json:"allowPartial"`
	RequestID    string `json:"requestId"`
}

// AdminGetFeedbackRewardPolicy 应用的反馈奖励策略
// GET /api/admin/tickets/reward-policy?appid=
func (h *Handler) AdminGetFeedbackRewardPolicy(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	policy, err := h.ticket.FeedbackRewardPolicy(c.Request.Context(), session, queryInt64Default(c, "appid", 0))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", policy)
}

// AdminSaveFeedbackRewardPolicy 修改应用的反馈奖励策略
// PUT /api/admin/tickets/reward-policy?appid=
func (h *Handler) AdminSaveFeedbackRewardPolicy(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req FeedbackRewardPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	appID := queryInt64Default(c, "appid", 0)
	if appID > 0 && req.AppID > 0 && appID != req.AppID {
		response.Error(c, http.StatusBadRequest, 40058, "查询参数与请求体里的应用不一致")
		return
	}
	if appID <= 0 {
		appID = req.AppID
	}
	policy, err := h.ticket.SaveFeedbackRewardPolicy(c.Request.Context(), session, ticketdomain.RewardPolicy{
		AppID: appID, Enabled: req.Enabled, DefaultPoints: req.DefaultPoints, CategoryPoints: req.CategoryPoints,
		MaxPoints: req.MaxPoints, UserDailyLimit: req.UserDailyLimit, UserMonthlyLimit: req.UserMonthlyLimit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", policy)
}

// AdminGetFeedbackReward 一条反馈的奖励区块：建议积分、本次最多可发、已发与历史
// GET /api/admin/tickets/:ticketId/reward
func (h *Handler) AdminGetFeedbackReward(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的反馈标识")
		return
	}
	info, err := h.ticket.FeedbackRewardInfo(c.Request.Context(), session, ticketID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", info)
}

// AdminGrantFeedbackReward 给一条反馈发放奖励积分
// POST /api/admin/tickets/:ticketId/reward
func (h *Handler) AdminGrantFeedbackReward(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的反馈标识")
		return
	}
	var req FeedbackRewardGrantRequest
	if c.Request.ContentLength > 0 {
		if err := bind(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, err.Error())
			return
		}
	}
	if req.RequestID == "" {
		req.RequestID = c.GetHeader("Idempotency-Key")
	}
	result, err := h.ticket.GrantFeedbackReward(c.Request.Context(), session, ticketID, ticketdomain.RewardGrantCommand{
		Points: req.Points, Reason: req.Reason, RequestID: req.RequestID,
		ClientIP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "奖励已发放"
	if result.Replayed {
		message = "这条反馈已发放过同样的奖励"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AdminRevokeFeedbackReward 撤回一条反馈的奖励并扣回积分
// POST /api/admin/tickets/:ticketId/reward/revoke
func (h *Handler) AdminRevokeFeedbackReward(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的反馈标识")
		return
	}
	var req FeedbackRewardRevokeRequest
	if c.Request.ContentLength > 0 {
		if err := bind(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, err.Error())
			return
		}
	}
	if req.RequestID == "" {
		req.RequestID = c.GetHeader("Idempotency-Key")
	}
	result, err := h.ticket.RevokeFeedbackReward(c.Request.Context(), session, ticketID, ticketdomain.RewardRevokeCommand{
		Reason: req.Reason, AllowPartial: req.AllowPartial, RequestID: req.RequestID,
		ClientIP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "奖励已撤回", result)
}
