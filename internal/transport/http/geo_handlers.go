package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	geodomain "aegis/internal/domain/geo"
	systemdomain "aegis/internal/domain/system"
	auditmiddleware "aegis/internal/middleware"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ──────────────────────────────────────
// DTO
// ──────────────────────────────────────

// GeoFenceUpsertRequest 创建/更新围栏。
// 多边形围栏（fence GeoJSON）与圆形围栏（centerLat+centerLng+radiusM）二选一。
type GeoFenceUpsertRequest struct {
	AppID     *int64          `json:"appId"`           // 空 = 平台级
	Name      string          `json:"name" binding:"required"`
	Mode      string          `json:"mode" binding:"required"` // deny | allow | review
	Fence     json.RawMessage `json:"fence"`           // GeoJSON Polygon / MultiPolygon
	CenterLat *float64        `json:"centerLat"`
	CenterLng *float64        `json:"centerLng"`
	RadiusM   *float64        `json:"radiusM"`
	BanMode   string          `json:"banMode"` // 空 = 平台默认响应模式
	Reason    string          `json:"reason"`
	Enabled   *bool           `json:"enabled"`
	ExpiresAt string          `json:"expiresAt"` // RFC3339；空 = 永久
}

// GeoFencePreviewRequest 围栏回测。
type GeoFencePreviewRequest struct {
	GeoFenceUpsertRequest
	WindowDays int `json:"windowDays"` // 默认 7
}

// GeoFenceToggleRequest 启用/禁用。
type GeoFenceToggleRequest struct {
	Enabled bool `json:"enabled"`
}

func (req *GeoFenceUpsertRequest) toMutation() (geodomain.FenceMutation, error) {
	m := geodomain.FenceMutation{
		AppID:     req.AppID,
		Name:      req.Name,
		Mode:      req.Mode,
		CenterLat: req.CenterLat,
		CenterLng: req.CenterLng,
		RadiusM:   req.RadiusM,
		BanMode:   req.BanMode,
		Reason:    req.Reason,
		Enabled:   true,
	}
	if len(req.Fence) > 0 && string(req.Fence) != "null" {
		m.FenceGeoJSON = string(req.Fence)
	}
	if req.Enabled != nil {
		m.Enabled = *req.Enabled
	}
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			return m, fmt.Errorf("expiresAt 必须为 RFC3339 格式")
		}
		m.ExpiresAt = &t
	}
	return m, nil
}

// ──────────────────────────────────────
// 围栏管理（仅超级管理员）
// ──────────────────────────────────────

// AdminListGeoFences GET /api/admin/system/firewall/geo-fences
func (h *Handler) AdminListGeoFences(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地理围栏")
		return
	}
	if h.geoFence == nil {
		response.Success(c, 200, "ok", []geodomain.Fence{})
		return
	}
	list, err := h.geoFence.ListAll(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if list == nil {
		list = []geodomain.Fence{}
	}
	response.Success(c, 200, "ok", list)
}

// AdminCreateGeoFence POST /api/admin/system/firewall/geo-fences
func (h *Handler) AdminCreateGeoFence(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地理围栏")
		return
	}
	if h.geoFence == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理围栏服务未就绪")
		return
	}
	var req GeoFenceUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	m, err := req.toMutation()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.geoFence.Create(c.Request.Context(), m, &session.AdminID)
	if err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_fence", req.Name)
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("创建地理围栏失败 %s", req.Name))
		auditmiddleware.SetAuditFailure(c, err)
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(item.ID, 10))
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("创建地理围栏 %s（mode=%s）", item.Name, item.Mode))
	auditmiddleware.SetAuditDiff(c, nil, item)
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "创建成功", item)
}

// AdminUpdateGeoFence PUT /api/admin/system/firewall/geo-fences/:id
func (h *Handler) AdminUpdateGeoFence(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地理围栏")
		return
	}
	if h.geoFence == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理围栏服务未就绪")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的围栏 ID")
		return
	}
	var req GeoFenceUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	m, err := req.toMutation()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	before, _ := h.geoFence.GetByID(c.Request.Context(), id)
	item, err := h.geoFence.Update(c.Request.Context(), id, m)
	if err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(id, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("更新地理围栏 #%d 失败", id))
		auditmiddleware.SetAuditFailure(c, err)
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(id, 10))
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("更新地理围栏 %s（mode=%s）", item.Name, item.Mode))
	auditmiddleware.SetAuditDiff(c, before, item)
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "更新成功", item)
}

// AdminToggleGeoFence PATCH /api/admin/system/firewall/geo-fences/:id
func (h *Handler) AdminToggleGeoFence(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地理围栏")
		return
	}
	if h.geoFence == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理围栏服务未就绪")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的围栏 ID")
		return
	}
	var req GeoFenceToggleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	before, _ := h.geoFence.GetByID(c.Request.Context(), id)
	if err := h.geoFence.Toggle(c.Request.Context(), id, req.Enabled); err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(id, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("切换地理围栏 #%d 失败", id))
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(id, 10))
	if before != nil {
		verb := "启用"
		if !req.Enabled {
			verb = "禁用"
		}
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("%s地理围栏 %s", verb, before.Name))
		auditmiddleware.SetAuditDiff(c, map[string]any{"enabled": before.Enabled}, map[string]any{"enabled": req.Enabled})
	} else {
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("切换地理围栏 #%d 启用状态 → %t", id, req.Enabled))
	}
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已更新", gin.H{"id": id, "enabled": req.Enabled})
}

// AdminDeleteGeoFence DELETE /api/admin/system/firewall/geo-fences/:id
func (h *Handler) AdminDeleteGeoFence(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地理围栏")
		return
	}
	if h.geoFence == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理围栏服务未就绪")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的围栏 ID")
		return
	}
	before, _ := h.geoFence.GetByID(c.Request.Context(), id)
	if err := h.geoFence.Delete(c.Request.Context(), id); err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(id, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除地理围栏 #%d 失败", id))
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_fence", strconv.FormatInt(id, 10))
	if before != nil {
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除地理围栏 %s", before.Name))
		auditmiddleware.SetAuditDiff(c, before, nil)
	} else {
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除地理围栏 #%d", id))
	}
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityCritical)
	response.Success(c, 200, "已删除", gin.H{"id": id})
}

// AdminPreviewGeoFence POST /api/admin/system/firewall/geo-fences/preview
// 回测围栏影响面（创建前评估过去 N 天会命中多少登录/拦截）。
func (h *Handler) AdminPreviewGeoFence(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地理围栏")
		return
	}
	if h.geoFence == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理围栏服务未就绪")
		return
	}
	var req GeoFencePreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	m, err := req.toMutation()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	preview, err := h.geoFence.Preview(c.Request.Context(), m, req.WindowDays)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "ok", preview)
}

// ──────────────────────────────────────
// 地理分析（仅超级管理员）
// ──────────────────────────────────────

// AdminGeoHeatmap GET /api/admin/system/geo/heatmap?kind=block&start=&end=&country=&limit=
// 数据源为 geo_stats_hourly 预聚合表。
func (h *Handler) AdminGeoHeatmap(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看地理分析")
		return
	}
	if h.geoAnalytics == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理分析服务未就绪")
		return
	}
	q := geodomain.HeatmapQuery{
		Kind:    c.DefaultQuery("kind", geodomain.StatsKindBlock),
		Country: c.Query("country"),
	}
	if v := c.Query("start"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "start 必须为 RFC3339 格式")
			return
		}
		q.Start = t
	}
	if v := c.Query("end"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "end 必须为 RFC3339 格式")
			return
		}
		q.End = t
	}
	if v := c.Query("limit"); v != "" {
		q.Limit, _ = strconv.Atoi(v)
	}
	result, err := h.geoAnalytics.Heatmap(c.Request.Context(), q)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "ok", result)
}

// AdminGeoClusters GET /api/admin/system/geo/clusters?hours=24&eps=0.5&minPoints=10&limit=20
// 对近期防火墙拦截做 DBSCAN 空间聚类。
func (h *Handler) AdminGeoClusters(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看地理分析")
		return
	}
	if h.geoAnalytics == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理分析服务未就绪")
		return
	}
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	eps, _ := strconv.ParseFloat(c.DefaultQuery("eps", "0.5"), 64)
	minPoints, _ := strconv.Atoi(c.DefaultQuery("minPoints", "10"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	clusters, err := h.geoAnalytics.Clusters(c.Request.Context(), hours, eps, minPoints, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if clusters == nil {
		clusters = []geodomain.Cluster{}
	}
	response.Success(c, 200, "ok", clusters)
}

// AdminUserGeoTrail GET /api/admin/system/geo/trail?userId=&appId=&limit=100
// 用户登录轨迹回放。
func (h *Handler) AdminUserGeoTrail(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看登录轨迹")
		return
	}
	if h.geoAnalytics == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地理分析服务未就绪")
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	appID, _ := strconv.ParseInt(c.Query("appId"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	trail, err := h.geoAnalytics.UserTrail(c.Request.Context(), userID, appID, limit)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if trail == nil {
		trail = []geodomain.TrailPoint{}
	}
	response.Success(c, 200, "ok", trail)
}
