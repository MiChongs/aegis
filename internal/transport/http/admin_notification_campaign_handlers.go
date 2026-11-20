package httptransport

// 站内信群发任务（docs/notification-center.md「群发任务」）：
//
//	GET  /api/admin/apps/:appkey/notifications/campaigns/catalog          表单目录（预设人群、平台、会员身份……）
//	POST /api/admin/apps/:appkey/notifications/campaigns/preview          人群预览（人数 + 抽样）
//	GET  /api/admin/apps/:appkey/notifications/campaigns                  任务列表
//	POST /api/admin/apps/:appkey/notifications/campaigns                  发起（一键全站 / 指定用户 / 条件筛选，可定时）
//	GET  /api/admin/apps/:appkey/notifications/campaigns/:campaignId      详情（含已读人数）
//	POST /api/admin/apps/:appkey/notifications/campaigns/:campaignId/cancel
//	POST /api/admin/apps/:appkey/notifications/campaigns/:campaignId/recall

import (
	"net/http"

	notificationdomain "aegis/internal/domain/notification"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AdminNotificationCampaignCatalog GET /api/admin/apps/:appkey/notifications/campaigns/catalog
func (h *Handler) AdminNotificationCampaignCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	response.Success(c, http.StatusOK, "获取成功", h.notifications.CampaignCatalog())
}

// AdminPreviewNotificationAudience POST /api/admin/apps/:appkey/notifications/campaigns/preview
func (h *Handler) AdminPreviewNotificationAudience(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminNotificationAudienceRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.PreviewAudience(c.Request.Context(), appID, req.Audience)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminListNotificationCampaigns GET /api/admin/apps/:appkey/notifications/campaigns
func (h *Handler) AdminListNotificationCampaigns(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminNotificationCampaignListQuery
	_ = bind(c, &query)
	result, err := h.notifications.ListCampaigns(c.Request.Context(), notificationdomain.CampaignQuery{
		AppID: appID, Status: query.Status, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminCreateNotificationCampaign POST /api/admin/apps/:appkey/notifications/campaigns
//
// 立刻返回任务，后台分批发送；同一 requestId 重复提交返回第一次建的任务（created=false）。
func (h *Handler) AdminCreateNotificationCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminNotificationCampaignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	campaign, created, err := h.notifications.CreateCampaign(c.Request.Context(), notificationdomain.CampaignInput{
		AppID: appID, RequestID: req.RequestID, Type: req.Type, Category: req.Category, Title: req.Title, Content: req.Content,
		Level: req.Level, Metadata: req.Metadata, Action: req.Action, Image: req.Image, ExpiresAt: req.ExpiresAt,
		Audience: req.Audience, ScheduledAt: req.ScheduledAt, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "群发任务已创建"
	if !created {
		message = "群发任务已存在"
	}
	response.Success(c, http.StatusOK, message, gin.H{"campaign": campaign, "created": created})
}

// AdminGetNotificationCampaign GET /api/admin/apps/:appkey/notifications/campaigns/:campaignId
func (h *Handler) AdminGetNotificationCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	campaignID, ok := cardKeyPathID(c, "campaignId")
	if !ok {
		return
	}
	campaign, err := h.notifications.GetCampaign(c.Request.Context(), appID, campaignID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", campaign)
}

// AdminCancelNotificationCampaign POST /api/admin/apps/:appkey/notifications/campaigns/:campaignId/cancel
func (h *Handler) AdminCancelNotificationCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	campaignID, ok := cardKeyPathID(c, "campaignId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	campaign, err := h.notifications.CancelCampaign(c.Request.Context(), appID, campaignID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已取消", campaign)
}

// AdminRecallNotificationCampaign POST /api/admin/apps/:appkey/notifications/campaigns/:campaignId/recall
func (h *Handler) AdminRecallNotificationCampaign(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	campaignID, ok := cardKeyPathID(c, "campaignId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	campaign, err := h.notifications.RecallCampaign(c.Request.Context(), appID, campaignID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已开始撤回", campaign)
}
