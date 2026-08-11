package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	notifydomain "aegis/internal/domain/notify"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 统一通知出口的管理端接口。
// 路由挂在 /api/admin/notify/*，写操作要求超级管理员（渠道里存着 IM 凭据，属平台级敏感资源）。

// ─────────────── DTO ───────────────

// NotifyChannelRequest 渠道新建/更新。
//
// secret 语义：
//   - 不传        → 保持原值
//   - ""          → 保持原值（前端把占位符原样回传时不会误清空）
//   - "-"         → 清空
//   - 其它        → 覆盖
type NotifyChannelRequest struct {
	AppID     int64          `json:"appid"`
	Key       *string        `json:"key"`
	Name      *string        `json:"name"`
	Kind      *string        `json:"kind"`
	Config    map[string]any `json:"config"`
	Secret    *string        `json:"secret"`
	Enabled   *bool          `json:"enabled"`
	RateLimit *int           `json:"rateLimitPerMinute"`
}

// NotifySubscriptionRequest 订阅新建/更新。
type NotifySubscriptionRequest struct {
	ChannelID   int64                   `json:"channelId" binding:"required"`
	EventKey    string                  `json:"eventKey" binding:"required"`
	AppID       *int64                  `json:"appid"`
	MinPriority string                  `json:"minPriority"`
	CategoryIDs []int64                 `json:"categoryIds"`
	TemplateID  *int64                  `json:"templateId"`
	QuietHours  *notifydomain.QuietHours `json:"quietHours"`
	Enabled     *bool                   `json:"enabled"`
}

// NotifyTemplateRequest 模板新建/更新。
type NotifyTemplateRequest struct {
	AppID         int64          `json:"appid"`
	Key           *string        `json:"key"`
	Name          *string        `json:"name"`
	EventKey      *string        `json:"eventKey"`
	ChannelKind   *string        `json:"channelKind"`
	TitleTemplate *string        `json:"titleTemplate"`
	BodyTemplate  *string        `json:"bodyTemplate"`
	CardTemplate  map[string]any `json:"cardTemplate"`
	Enabled       *bool          `json:"enabled"`
}

// NotifyTemplatePreviewRequest 模板预览。
type NotifyTemplatePreviewRequest struct {
	TitleTemplate string         `json:"titleTemplate"`
	BodyTemplate  string         `json:"bodyTemplate"`
	Vars          map[string]any `json:"vars"`
}

// ─────────────── 元数据 ───────────────

// AdminNotifyCatalog 渠道类型目录 + 事件目录。
// 前端据此动态渲染渠道配置表单与事件下拉，新增渠道类型无需改前端。
// GET /api/admin/notify/catalog
func (h *Handler) AdminNotifyCatalog(c *gin.Context) {
	response.Success(c, http.StatusOK, "获取成功", gin.H{
		"kinds":  h.notify.SupportedKinds(),
		"events": h.notify.Events(),
	})
}

// ─────────────── 渠道 ───────────────

// AdminListNotifyChannels 渠道列表
// GET /api/admin/notify/channels
func (h *Handler) AdminListNotifyChannels(c *gin.Context) {
	appID := int64(-1)
	if raw := strings.TrimSpace(c.Query("appid")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			appID = parsed
		}
	}
	items, err := h.notify.ListChannels(c.Request.Context(), appID, c.Query("kind"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminGetNotifyChannel 渠道详情
// GET /api/admin/notify/channels/:id
func (h *Handler) AdminGetNotifyChannel(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的渠道标识")
		return
	}
	item, err := h.notify.GetChannel(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", item)
}

// AdminSaveNotifyChannel 新建 / 更新渠道
// POST /api/admin/notify/channels  |  PUT /api/admin/notify/channels/:id
func (h *Handler) AdminSaveNotifyChannel(c *gin.Context) {
	var req NotifyChannelRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	mutation := notifydomain.ChannelMutation{
		ID: id, AppID: req.AppID, Key: req.Key, Name: req.Name, Kind: req.Kind,
		Config: req.Config, Secret: req.Secret, Enabled: req.Enabled, RateLimit: req.RateLimit,
	}
	if id == 0 {
		if session, ok := adminAccessSession(c); ok && session != nil {
			creator := session.AdminID
			mutation.CreatedBy = &creator
		}
	}
	item, err := h.notify.SaveChannel(c.Request.Context(), mutation)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", item)
}

// AdminDeleteNotifyChannel 删除渠道
// DELETE /api/admin/notify/channels/:id
func (h *Handler) AdminDeleteNotifyChannel(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的渠道标识")
		return
	}
	if err := h.notify.DeleteChannel(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminTestNotifyChannel 发送测试消息
// POST /api/admin/notify/channels/:id/test
func (h *Handler) AdminTestNotifyChannel(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的渠道标识")
		return
	}
	operator := ""
	if session, ok := adminAccessSession(c); ok && session != nil {
		operator = session.DisplayName
		if strings.TrimSpace(operator) == "" {
			operator = session.Account
		}
	}
	result, err := h.notify.TestChannel(c.Request.Context(), id, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "测试消息已发送", result)
}

// ─────────────── 订阅 ───────────────

// AdminListNotifySubscriptions 订阅列表
// GET /api/admin/notify/subscriptions
func (h *Handler) AdminListNotifySubscriptions(c *gin.Context) {
	channelID := queryInt64Default(c, "channelId", 0)
	items, err := h.notify.ListSubscriptions(c.Request.Context(), channelID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminSaveNotifySubscription 新建 / 更新订阅
// POST /api/admin/notify/subscriptions  |  PUT /api/admin/notify/subscriptions/:id
func (h *Handler) AdminSaveNotifySubscription(c *gin.Context) {
	var req NotifySubscriptionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	item, err := h.notify.SaveSubscription(c.Request.Context(), notifydomain.SubscriptionMutation{
		ID: id, ChannelID: req.ChannelID, EventKey: req.EventKey, AppID: req.AppID,
		MinPriority: req.MinPriority, CategoryIDs: req.CategoryIDs, TemplateID: req.TemplateID,
		QuietHours: req.QuietHours, Enabled: req.Enabled,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", item)
}

// AdminDeleteNotifySubscription 删除订阅
// DELETE /api/admin/notify/subscriptions/:id
func (h *Handler) AdminDeleteNotifySubscription(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的订阅标识")
		return
	}
	if err := h.notify.DeleteSubscription(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// ─────────────── 模板 ───────────────

// AdminListNotifyTemplates 模板列表
// GET /api/admin/notify/templates
func (h *Handler) AdminListNotifyTemplates(c *gin.Context) {
	items, err := h.notify.ListTemplates(c.Request.Context(), queryInt64Default(c, "appid", 0))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminSaveNotifyTemplate 新建 / 更新模板
// POST /api/admin/notify/templates  |  PUT /api/admin/notify/templates/:id
func (h *Handler) AdminSaveNotifyTemplate(c *gin.Context) {
	var req NotifyTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	item, err := h.notify.SaveTemplate(c.Request.Context(), notifydomain.TemplateMutation{
		ID: id, AppID: req.AppID, Key: req.Key, Name: req.Name, EventKey: req.EventKey,
		ChannelKind: req.ChannelKind, TitleTemplate: req.TitleTemplate,
		BodyTemplate: req.BodyTemplate, CardTemplate: req.CardTemplate, Enabled: req.Enabled,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", item)
}

// AdminDeleteNotifyTemplate 删除模板
// DELETE /api/admin/notify/templates/:id
func (h *Handler) AdminDeleteNotifyTemplate(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的模板标识")
		return
	}
	if err := h.notify.DeleteTemplate(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminPreviewNotifyTemplate 模板预览
// POST /api/admin/notify/templates/preview
func (h *Handler) AdminPreviewNotifyTemplate(c *gin.Context) {
	var req NotifyTemplatePreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	title, body, err := h.notify.PreviewTemplate(c.Request.Context(), req.TitleTemplate, req.BodyTemplate, req.Vars)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "渲染成功", gin.H{"title": title, "body": body})
}

// ─────────────── 投递记录 ───────────────

// AdminListNotifyDeliveries 投递记录
// GET /api/admin/notify/deliveries
func (h *Handler) AdminListNotifyDeliveries(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	query := notifydomain.DeliveryQuery{
		ChannelID:  optionalQueryInt64(c, "channelId"),
		EventKey:   c.Query("eventKey"),
		Status:     c.Query("status"),
		Resource:   c.Query("resource"),
		ResourceID: c.Query("resourceId"),
		AppID:      optionalQueryInt64(c, "appid"),
		Page:       page,
		Limit:      limit,
	}
	result, err := h.notify.ListDeliveries(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminNotifyDeliveryStats 投递健康度
// GET /api/admin/notify/deliveries/stats?days=7
func (h *Handler) AdminNotifyDeliveryStats(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	stats, err := h.notify.DeliveryStats(c.Request.Context(), days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", stats)
}

// AdminPurgeNotifyDeliveries 清理历史投递记录
// DELETE /api/admin/notify/deliveries?days=30
func (h *Handler) AdminPurgeNotifyDeliveries(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	affected, err := h.notify.PurgeDeliveries(c.Request.Context(), days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "清理完成", gin.H{"deleted": affected})
}
