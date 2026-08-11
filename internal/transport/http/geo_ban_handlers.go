package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	systemdomain "aegis/internal/domain/system"
	auditmiddleware "aegis/internal/middleware"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// GeoBanUpsertRequest 创建/更新规则
type GeoBanUpsertRequest struct {
	ScopeType  string `json:"scopeType" binding:"required"` // country | region | city | asn | isp
	ScopeValue string `json:"scopeValue" binding:"required"`
	Mode       string `json:"mode"` // 空 = 使用平台默认
	Reason     string `json:"reason"`
	Enabled    *bool  `json:"enabled"`
	ExpiresAt  string `json:"expiresAt"` // RFC3339；空=永久
}

// GeoBanToggleRequest 启用/禁用
type GeoBanToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// AdminListGeoBans GET /api/admin/system/firewall/geo-bans
func (h *Handler) AdminListGeoBans(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地域封禁")
		return
	}
	if h.geoBan == nil {
		response.Success(c, 200, "ok", []firewalldomain.GeoBan{})
		return
	}
	list, err := h.geoBan.ListAll(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", list)
}

// AdminUpsertGeoBan POST /api/admin/system/firewall/geo-bans
func (h *Handler) AdminUpsertGeoBan(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地域封禁")
		return
	}
	if h.geoBan == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地域封禁服务未就绪")
		return
	}
	var req GeoBanUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !validScopeType(req.ScopeType) {
		response.Error(c, http.StatusBadRequest, 40000, "无效的作用域类型（允许: country/region/city/asn/isp）")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "expiresAt 必须为 RFC3339 格式")
			return
		}
		expiresAt = &t
	}
	item, err := h.geoBan.Upsert(c.Request.Context(), firewalldomain.GeoBanMutation{
		ScopeType:  req.ScopeType,
		ScopeValue: req.ScopeValue,
		Mode:       req.Mode,
		Reason:     req.Reason,
		Enabled:    enabled,
		ExpiresAt:  expiresAt,
	}, &session.AdminID)
	if err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_ban", fmt.Sprintf("%s:%s", req.ScopeType, req.ScopeValue))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("保存地域封禁规则失败 %s=%s", req.ScopeType, req.ScopeValue))
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_ban", strconv.FormatInt(item.ID, 10))
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("保存地域封禁规则 %s=%s", item.ScopeType, item.ScopeValue))
	auditmiddleware.SetAuditDetail(c, fmt.Sprintf("scope=%s value=%s mode=%s enabled=%t reason=%q expires_at=%s",
		item.ScopeType, item.ScopeValue, item.Mode, item.Enabled, item.Reason, formatGeoBanExpiry(item.ExpiresAt)))
	auditmiddleware.SetAuditDiff(c, nil, item)
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "保存成功", item)
}

func formatGeoBanExpiry(t *time.Time) string {
	if t == nil {
		return "永久"
	}
	return t.UTC().Format(time.RFC3339)
}

// AdminToggleGeoBan PATCH /api/admin/system/firewall/geo-bans/:id
func (h *Handler) AdminToggleGeoBan(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地域封禁")
		return
	}
	if h.geoBan == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地域封禁服务未就绪")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	var req GeoBanToggleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	before, _ := h.geoBan.GetByID(c.Request.Context(), id)
	if err := h.geoBan.Toggle(c.Request.Context(), id, req.Enabled); err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_ban", strconv.FormatInt(id, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("切换地域封禁规则 #%d 失败", id))
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_ban", strconv.FormatInt(id, 10))
	if before != nil {
		verb := "启用"
		if !req.Enabled {
			verb = "禁用"
		}
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("%s地域封禁规则 %s=%s", verb, before.ScopeType, before.ScopeValue))
		auditmiddleware.SetAuditDetail(c, fmt.Sprintf("scope=%s value=%s enabled: %t → %t", before.ScopeType, before.ScopeValue, before.Enabled, req.Enabled))
		auditmiddleware.SetAuditDiff(c, map[string]any{"enabled": before.Enabled}, map[string]any{"enabled": req.Enabled})
	} else {
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("切换地域封禁规则 #%d 启用状态 → %t", id, req.Enabled))
	}
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已更新", gin.H{"id": id, "enabled": req.Enabled})
}

// AdminDeleteGeoBan DELETE /api/admin/system/firewall/geo-bans/:id
func (h *Handler) AdminDeleteGeoBan(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理地域封禁")
		return
	}
	if h.geoBan == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "地域封禁服务未就绪")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	before, _ := h.geoBan.GetByID(c.Request.Context(), id)
	if err := h.geoBan.Delete(c.Request.Context(), id); err != nil {
		auditmiddleware.SetAuditResource(c, "firewall.geo_ban", strconv.FormatInt(id, 10))
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除地域封禁规则 #%d 失败", id))
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.geo_ban", strconv.FormatInt(id, 10))
	if before != nil {
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除地域封禁规则 %s=%s", before.ScopeType, before.ScopeValue))
		auditmiddleware.SetAuditDetail(c, fmt.Sprintf("scope=%s value=%s mode=%s reason=%q", before.ScopeType, before.ScopeValue, before.Mode, before.Reason))
		auditmiddleware.SetAuditDiff(c, before, nil)
	} else {
		auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除地域封禁规则 #%d", id))
	}
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityCritical)
	response.Success(c, 200, "已删除", gin.H{"id": id})
}

func validScopeType(t string) bool {
	switch t {
	case firewalldomain.GeoScopeCountry, firewalldomain.GeoScopeRegion,
		firewalldomain.GeoScopeCity, firewalldomain.GeoScopeASN, firewalldomain.GeoScopeISP:
		return true
	}
	return false
}
