package httptransport

// 用户资料、设置、安全与会话（用户端）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	userdomain "aegis/internal/domain/user"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Profile(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	profile, err := h.user.GetProfile(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachUserProfileAvatar(c, session, profile)
	response.Success(c, 200, "获取成功", profile)
}

func (h *Handler) UpdateProfile(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req UpdateProfileRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.UpdateProfile(c.Request.Context(), session, userdomain.ProfileUpdate(req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result != nil && result.Profile != nil {
		h.attachUserProfileAvatar(c, session, result.Profile)
	}
	response.Success(c, 200, "更新成功", result)
}

func (h *Handler) ConfirmProfileChange(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req ConfirmProfileChangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.ConfirmSensitiveProfileChange(c.Request.Context(), session, req.Field, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result != nil && result.Profile != nil {
		h.attachUserProfileAvatar(c, session, result.Profile)
	}
	response.Success(c, 200, "资料变更已生效", result)
}

func (h *Handler) Settings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	settings, err := h.user.GetSettings(c.Request.Context(), session, c.Query("category"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", settings)
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req UpdateSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	settings, err := h.user.UpdateSettings(c.Request.Context(), session, req.Category, req.Settings)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", settings)
}

func (h *Handler) LegacyUserSettings(c *gin.Context) {
	h.Settings(c)
}

func (h *Handler) LegacyUpdateUserSettings(c *gin.Context) {
	h.UpdateSettings(c)
}

func (h *Handler) LegacyResetUserSettings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req ResetSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	settings, err := h.user.ResetSettings(c.Request.Context(), session, req.Category)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重置成功", settings)
}

func (h *Handler) UserSettingCategories(c *gin.Context) {
	response.Success(c, 200, "获取成功", gin.H{
		"categories": h.user.ListSettingCategories(),
	})
}

func (h *Handler) LegacyAutoSignStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	settings, err := h.user.GetSettings(c.Request.Context(), session, "autoSign")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"enabled":   settings.Settings["enabled"],
		"category":  settings.Category,
		"settings":  settings.Settings,
		"version":   settings.Version,
		"isActive":  settings.IsActive,
		"updatedAt": settings.UpdatedAt,
	})
}

func (h *Handler) LegacyAutoSignTestNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if err := h.notifications.SendUserNotification(c.Request.Context(), session, "system", "自动签到测试通知", "自动签到通知链路正常，当前配置已可用。", "info", map[string]any{
		"scene": "auto_sign_test",
	}); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试通知发送成功", gin.H{"sent": true})
}

func (h *Handler) Security(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.user.GetSecurityStatus(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

func (h *Handler) UserLoginAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserLoginAuditQuery
	_ = bind(c, &query)
	result, err := h.user.ListLoginAudits(c.Request.Context(), session, userdomain.LoginAuditQuery{
		Status: query.Status,
		Page:   query.Page,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ExportUserLoginAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserLoginAuditQuery
	_ = bind(c, &query)
	items, err := h.user.ExportLoginAudits(c.Request.Context(), session, userdomain.LoginAuditExportQuery{
		Status: query.Status,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "user_login_audits_" + strconv.FormatInt(session.UserID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "login_type", "provider", "token_jti", "login_ip", "device_id", "user_agent", "status", "created_at", "metadata"})
	for _, item := range items {
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.LoginType,
			item.Provider,
			item.TokenJTI,
			item.LoginIP,
			item.DeviceID,
			item.UserAgent,
			item.Status,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) UserSessionAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserSessionAuditQuery
	_ = bind(c, &query)
	result, err := h.user.ListSessionAudits(c.Request.Context(), session, userdomain.SessionAuditQuery{
		EventType: query.EventType,
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ExportUserSessionAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserSessionAuditQuery
	_ = bind(c, &query)
	items, err := h.user.ExportSessionAudits(c.Request.Context(), session, userdomain.SessionAuditExportQuery{
		EventType: query.EventType,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "user_session_audits_" + strconv.FormatInt(session.UserID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "token_jti", "event_type", "created_at", "metadata"})
	for _, item := range items {
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.TokenJTI,
			item.EventType,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) UserSessions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	items, err := h.user.ListSessions(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) RevokeUserSession(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.user.RevokeSession(c.Request.Context(), session, c.Param("tokenHash"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", result)
}

func (h *Handler) RevokeAllUserSessions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req UserSessionRevokeAllRequest
	_ = bind(c, &req)
	result, err := h.user.RevokeAllSessions(c.Request.Context(), session, req.IncludeCurrent)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", result)
}
