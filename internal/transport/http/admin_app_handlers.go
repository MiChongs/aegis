package httptransport

// 应用管理端 —— 管理端最大的一组处理器。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	appdomain "aegis/internal/domain/app"
	notificationdomain "aegis/internal/domain/notification"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminApps(c *gin.Context) {
	items, err := h.app.ListApps(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 非超管：按角色分配过滤可见应用
	session, ok := adminAccessSession(c)
	if ok && session != nil && !session.IsSuperAdmin {
		items = filterAppsByAssignments(items, session.Assignments)
	}
	response.Success(c, 200, "获取成功", items)
}

// filterAppsByAssignments 按管理员角色分配过滤应用列表
// 全局角色（appID == nil）可见所有应用，应用级角色只可见绑定的应用
func filterAppsByAssignments(apps []appdomain.App, assignments []admindomain.Assignment) []appdomain.App {
	// 如果有任何全局角色，返回全部应用
	for _, a := range assignments {
		if a.AppID == nil {
			return apps
		}
	}
	// 收集有权限的 appID
	allowed := make(map[int64]struct{}, len(assignments))
	for _, a := range assignments {
		if a.AppID != nil {
			allowed[*a.AppID] = struct{}{}
		}
	}
	filtered := make([]appdomain.App, 0, len(allowed))
	for _, app := range apps {
		if _, ok := allowed[app.ID]; ok {
			filtered = append(filtered, app)
		}
	}
	return filtered
}

func (h *Handler) AdminApp(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetApp(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetStats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppUserTrend(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminAppTrendQuery
	_ = bind(c, &query)
	item, err := h.app.GetUserTrend(c.Request.Context(), appID, query.Days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppRegionStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminRegionStatsQuery
	_ = bind(c, &query)
	item, err := h.app.GetRegionStats(c.Request.Context(), appID, appdomain.RegionStatsQuery{
		Type:  query.Type,
		Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppAuthSources(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetAuthSourceStats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppLoginAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminLoginAuditQuery
	_ = bind(c, &query)
	item, err := h.app.ListLoginAudits(c.Request.Context(), appID, appdomain.LoginAuditQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Page:    query.Page,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) ExportAdminAppLoginAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminLoginAuditQuery
	_ = bind(c, &query)
	items, err := h.app.ExportLoginAudits(c.Request.Context(), appID, appdomain.LoginAuditExportQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_login_audits_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "user_id", "appid", "account", "nickname", "login_type", "provider", "token_jti", "login_ip", "device_id", "user_agent", "status", "created_at", "metadata"})
	for _, item := range items {
		userID := ""
		if item.UserID != nil {
			userID = strconv.FormatInt(*item.UserID, 10)
		}
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			userID,
			strconv.FormatInt(item.AppID, 10),
			item.Account,
			item.Nickname,
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

func (h *Handler) AdminAppSessionAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminSessionAuditQuery
	_ = bind(c, &query)
	item, err := h.app.ListSessionAudits(c.Request.Context(), appID, appdomain.SessionAuditQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) ExportAdminAppSessionAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminSessionAuditQuery
	_ = bind(c, &query)
	items, err := h.app.ExportSessionAudits(c.Request.Context(), appID, appdomain.SessionAuditExportQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_session_audits_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "user_id", "appid", "account", "nickname", "token_jti", "event_type", "created_at", "metadata"})
	for _, item := range items {
		userID := ""
		if item.UserID != nil {
			userID = strconv.FormatInt(*item.UserID, 10)
		}
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			userID,
			strconv.FormatInt(item.AppID, 10),
			item.Account,
			item.Nickname,
			item.TokenJTI,
			item.EventType,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) AdminBulkNotifyUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminBulkNotificationRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.AdminBulkSend(c.Request.Context(), appID, notificationdomain.AdminBulkSendCommand{
		UserIDs:  req.UserIDs,
		Keyword:  req.Keyword,
		Enabled:  req.Enabled,
		Limit:    req.Limit,
		Type:     req.Type,
		Title:    req.Title,
		Content:  req.Content,
		Level:    req.Level,
		Metadata: req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "通知发送成功", result)
}

func (h *Handler) AdminAppNotifications(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminNotificationListQuery
	_ = bind(c, &query)
	result, err := h.notifications.AdminList(c.Request.Context(), appID, notificationdomain.AdminListQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Page:    query.Page,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) DeleteAdminAppNotifications(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminNotificationDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.AdminDelete(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) DeleteAdminAppNotificationsByFilter(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminNotificationDeleteFilterRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.AdminDeleteByFilter(c.Request.Context(), appID, notificationdomain.AdminExportQuery{
		Keyword: req.Keyword,
		Type:    req.Type,
		Level:   req.Level,
		Limit:   req.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) ExportAdminAppNotifications(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminNotificationListQuery
	_ = bind(c, &query)
	items, err := h.notifications.AdminExport(c.Request.Context(), appID, notificationdomain.AdminExportQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_notifications_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "user_id", "account", "nickname", "type", "title", "content", "level", "status", "read_at", "created_at", "updated_at", "metadata"})
	for _, item := range items {
		userID := ""
		if item.UserID != nil {
			userID = strconv.FormatInt(*item.UserID, 10)
		}
		readAt := ""
		if item.ReadAt != nil {
			readAt = item.ReadAt.UTC().Format(time.RFC3339)
		}
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			userID,
			item.Account,
			item.Nickname,
			item.Type,
			item.Title,
			item.Content,
			item.Level,
			item.Status,
			readAt,
			item.CreatedAt.UTC().Format(time.RFC3339),
			item.UpdatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) AdminAppUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminUserListQuery
	_ = bind(c, &query)
	createdFrom, err := parseOptionalDateTime(query.CreatedFrom)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdFrom 格式错误")
		return
	}
	createdTo, err := parseOptionalDateTime(query.CreatedTo)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdTo 格式错误")
		return
	}
	if createdTo != nil && len(strings.TrimSpace(query.CreatedTo)) == len("2006-01-02") {
		adjusted := createdTo.Add(24*time.Hour - time.Nanosecond)
		createdTo = &adjusted
	}
	items, err := h.user.ListAdminUsers(c.Request.Context(), appID, userdomain.AdminUserQuery{
		Keyword:     query.Keyword,
		Account:     query.Account,
		Nickname:    query.Nickname,
		Email:       query.Email,
		Phone:       query.Phone,
		InviteCode:  query.InviteCode,
		RegisterIP:  query.RegisterIP,
		UserID:      query.UserID,
		Enabled:     query.Enabled,
		CreatedFrom: createdFrom,
		CreatedTo:   createdTo,
		Sort:        query.Sort,
		Order:       query.Order,
		Page:        query.Page,
		Limit:       query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) ExportAdminAppUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminUserListQuery
	_ = bind(c, &query)
	createdFrom, err := parseOptionalDateTime(query.CreatedFrom)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdFrom 格式错误")
		return
	}
	createdTo, err := parseOptionalDateTime(query.CreatedTo)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdTo 格式错误")
		return
	}
	if createdTo != nil && len(strings.TrimSpace(query.CreatedTo)) == len("2006-01-02") {
		adjusted := createdTo.Add(24*time.Hour - time.Nanosecond)
		createdTo = &adjusted
	}
	items, err := h.user.ExportAdminUsers(c.Request.Context(), appID, userdomain.AdminUserQuery{
		Keyword:     query.Keyword,
		Account:     query.Account,
		Nickname:    query.Nickname,
		Email:       query.Email,
		Phone:       query.Phone,
		InviteCode:  query.InviteCode,
		RegisterIP:  query.RegisterIP,
		UserID:      query.UserID,
		Enabled:     query.Enabled,
		CreatedFrom: createdFrom,
		CreatedTo:   createdTo,
		Sort:        query.Sort,
		Order:       query.Order,
		Limit:       query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_users_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "account", "nickname", "email", "phone", "enabled", "integral", "experience", "register_ip", "register_time", "register_province", "register_city", "vip_expire_at"})
	for _, item := range items {
		registerTime := ""
		if item.RegisterTime != nil {
			registerTime = item.RegisterTime.UTC().Format(time.RFC3339)
		}
		vipExpireAt := ""
		if item.VIPExpireAt != nil {
			vipExpireAt = item.VIPExpireAt.UTC().Format(time.RFC3339)
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.Account,
			item.Nickname,
			item.Email,
			item.Phone,
			strconv.FormatBool(item.Enabled),
			strconv.FormatInt(item.Integral, 10),
			strconv.FormatInt(item.Experience, 10),
			item.RegisterIP,
			registerTime,
			item.RegisterProvince,
			item.RegisterCity,
			vipExpireAt,
		})
	}
}

func (h *Handler) AdminAppUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	item, err := h.user.GetAdminUserDetail(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) BatchUpdateAdminAppUserStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminUserBatchStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.BatchUpdateAdminUserStatus(c.Request.Context(), appID, userdomain.AdminUserBatchStatusMutation{
		UserIDs: req.UserIDs,
		AdminUserStatusMutation: userdomain.AdminUserStatusMutation{
			Enabled:              req.Enabled,
			DisabledEndTime:      req.DisabledEndTime,
			ClearDisabledEndTime: req.ClearDisabledEndTime,
			DisabledReason:       req.DisabledReason,
		},
	}, userdomain.BanOperator{AdminID: adminID, AdminName: adminName})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量更新成功", item)
}

func (h *Handler) UpdateAdminAppUserStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminUserStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.UpdateAdminUserStatus(c.Request.Context(), appID, userID, userdomain.AdminUserStatusMutation{
		Enabled:              req.Enabled,
		DisabledEndTime:      req.DisabledEndTime,
		ClearDisabledEndTime: req.ClearDisabledEndTime,
		DisabledReason:       req.DisabledReason,
	}, userdomain.BanOperator{AdminID: adminID, AdminName: adminName})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminUpdateUserProfile(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminUpdateUserProfileRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.user.AdminUpdateUserProfile(c.Request.Context(), appID, userID, req.Nickname, req.Email); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户资料已更新", nil)
}

func (h *Handler) AdminResetUserPassword(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminResetUserPasswordRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.user.AdminResetUserPassword(c.Request.Context(), appID, userID, req.NewPassword); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户密码已重置", nil)
}

func (h *Handler) AdminRevokeUserSessions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if err := h.user.AdminRevokeUserSessions(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户会话已全部踢出", nil)
}

func (h *Handler) AdminListUserSessions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	sessions, err := h.user.AdminListUserSessions(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// GeoIP 位置解析（含经纬度与内网标记，供活动地图定位）
	if h.location != nil {
		ips := make([]string, 0, len(sessions))
		for i := range sessions {
			ips = append(ips, sessions[i].IP)
		}
		located := h.resolveIPLocations(c.Request.Context(), ips)
		for i := range sessions {
			loc, ok := located[sessions[i].IP]
			if !ok {
				continue
			}
			sessions[i].Country = loc.Country
			sessions[i].CountryCode = loc.CountryCode
			sessions[i].Region = loc.Region
			sessions[i].City = loc.City
			sessions[i].ISP = loc.ISP
			sessions[i].Location = loc.Location
			sessions[i].Latitude, sessions[i].Longitude = geoCoords(loc)
			sessions[i].IsPrivate = loc.IsPrivate
		}
	}
	response.Success(c, 200, "获取成功", gin.H{"items": sessions, "total": len(sessions)})
}

func (h *Handler) AdminRevokeUserSession(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	tokenHash := c.Param("tokenHash")
	if err := h.user.AdminRevokeUserSession(c.Request.Context(), appID, userID, tokenHash); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", gin.H{"revoked": 1})
}

func (h *Handler) AdminRevokeUserSessionsBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminSessionRevokeBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	revoked, err := h.user.AdminRevokeUserSessionsBatch(c.Request.Context(), appID, userID, req.TokenHashes)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量撤销完成", gin.H{"revoked": revoked})
}

func (h *Handler) AdminDeleteUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if err := h.user.AdminDeleteUser(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户已删除", nil)
}

// CreateAdminApp 新建应用。
//
// 中间件对这条路由**不要求权限点**（要求 app:write 会让自助注册出来的账号
// 永远建不了第一个应用，也就永远拿不到任何权限）。闸门在这里：
// EnsureCanCreateApp 判定「平台开没开自助创建 + 这个人的配额还剩没剩」，
// 持有全局 app:write 的管理员则直接放行、不受配额约束。
func (h *Handler) CreateAdminApp(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if err := h.admin.EnsureCanCreateApp(c.Request.Context(), session); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminAppCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 超管不需要授权行（他本来就什么都能改），其余人必须拿到应用级角色，
	// 否则建完自己就看不见它了 —— 应用列表按授权过滤。
	creatorRole := ""
	if !session.IsSuperAdmin {
		creatorRole = h.admin.SelfServiceCreatorRole(c.Request.Context())
	}
	item, err := h.app.CreateApp(c.Request.Context(), appdomain.AppMutation{
		ID:                     0,
		Name:                   req.Name,
		Status:                 req.Status,
		DisabledReason:         req.DisabledReason,
		RegisterStatus:         req.RegisterStatus,
		DisabledRegisterReason: req.DisabledRegisterReason,
		LoginStatus:            req.LoginStatus,
		DisabledLoginReason:    req.DisabledLoginReason,
		Settings:               req.Settings,
	}, session.AdminID, creatorRole)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) UpdateAdminApp(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminApp(c, appID, req)
}

// AdminDeleteApp 删除应用。
//
// 两类人可以删：超级管理员，以及**自己把这个应用建起来的那个人**。
//
// 后者是自助路径的必要出口：自助创建有每人配额，而配额只按 apps.created_by 计数。
// 不给创建者删除权，配额就是一条单向棘轮 —— 建满之后除了去找超管，
// 他对自己一手建起来、可能只是拿来试手的应用什么都做不了。
//
// 认「创建者」而不是「应用管理员」是刻意的：超管授权出去的 app_admin 管着应用，
// 但应用不是他拉起来的，删除这种不可逆动作不该跟着授权一起流转。
func (h *Handler) AdminDeleteApp(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if !session.IsSuperAdmin && !h.app.IsAppCreator(c.Request.Context(), appID, session.AdminID) {
		response.Error(c, http.StatusForbidden, 40313,
			"仅超级管理员或该应用的创建者可删除应用")
		return
	}
	if err := h.app.DeleteApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "应用已删除", nil)
}

func (h *Handler) AdminAppEncryption(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetTransportEncryption(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) UpdateAdminAppEncryption(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req appdomain.TransportEncryptionUpdate
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdateTransportEncryption(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "加密配置已更新", item)
}

// AdminAppCommerceSettings 读取应用级交易设置（积分兑换率）。
func (h *Handler) AdminAppCommerceSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetCommerceSettings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// UpdateAdminAppCommerceSettings 更新应用级交易设置。
func (h *Handler) UpdateAdminAppCommerceSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req appdomain.CommerceSettings
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdateCommerceSettings(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "交易设置已更新", item)
}

// AdminAppLoginBaseline 查看某用户的登录绑定基线（绑定设备 / 网段 / 属地 / 上次换绑时间）。
func (h *Handler) AdminAppLoginBaseline(c *gin.Context) {
	appID, userID, ok := resolveAppScopedUserID(c, h.app)
	if !ok {
		return
	}
	baseline, err := h.auth.InspectLoginBaseline(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"appid":    appID,
		"userId":   userID,
		"bound":    baseline != nil,
		"baseline": baseline,
	})
}

// ResetAdminAppLoginBaseline 重置某用户的登录绑定，下次登录重新建立基线。
func (h *Handler) ResetAdminAppLoginBaseline(c *gin.Context) {
	appID, userID, ok := resolveAppScopedUserID(c, h.app)
	if !ok {
		return
	}
	if err := h.auth.ResetLoginBaseline(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "登录绑定已重置", gin.H{"appid": appID, "userId": userID, "bound": false})
}

// resolveAppScopedUserID 解析「应用 + 路径上的 userid」二元组。
func resolveAppScopedUserID(c *gin.Context, appSvc *service.AppService) (int64, int64, bool) {
	appID, ok := resolveAppID(c, appSvc)
	if !ok {
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("userid")), 10, 64)
	if err != nil || userID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "用户 ID 无效")
		return 0, 0, false
	}
	return appID, userID, true
}

func (h *Handler) UpdateAdminAppPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdatePolicy(c.Request.Context(), appID, appdomain.Policy{
		LoginCheckDevice:     req.LoginCheckDevice,
		LoginCheckUser:       req.LoginCheckUser,
		LoginCheckIP:         req.LoginCheckIP,
		DeviceRebindInterval: req.DeviceRebindInterval,
		MultiDeviceLogin:     req.MultiDeviceLogin,
		MultiDeviceLimit:     req.MultiDeviceLimit,
		RegisterCheckIP:      req.RegisterCheckIP,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetPasswordPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取密码策略成功", item)
}

func (h *Handler) UpdateAdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminPasswordPolicyUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SetPasswordPolicy(c.Request.Context(), appID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略设置成功", item)
}

func (h *Handler) TestAdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminPasswordPolicyTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.TestPasswordPolicy(c.Request.Context(), appID, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略测试完成", item)
}

func (h *Handler) ResetAdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.ResetPasswordPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略已重置", item)
}

func (h *Handler) AdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetSignInRewardPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到奖励策略成功", item)
}

func (h *Handler) UpdateAdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminSignInRewardPolicyUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SetSignInRewardPolicy(c.Request.Context(), appID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到奖励策略设置成功", item)
}

func (h *Handler) TestAdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminSignInRewardTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.PreviewSignInReward(c.Request.Context(), appID, appdomain.SignInRewardPreviewInput{
		OccurredAt:      req.OccurredAt,
		ConsecutiveDays: req.ConsecutiveDays,
		TotalSignIns:    req.TotalSignIns,
		UserExperience:  req.UserExperience,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到奖励策略测试完成", item)
}

func (h *Handler) ResetAdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.ResetSignInRewardPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到奖励策略已重置", item)
}

func (h *Handler) SignInRewardTemplates(c *gin.Context) {
	response.Success(c, 200, "获取签到奖励模板成功", gin.H{
		"templates": h.app.GetSignInRewardTemplates(),
		"usage": gin.H{
			"balanced":  "兼容当前默认行为，适合作为迁移基线",
			"growth":    "强调新用户启动和工作日活跃拉升",
			"retention": "强调长周期连签和里程碑奖励",
		},
	})
}

func (h *Handler) AdminAppSignInStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppSignInStatsQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.GetAppSignInStats(c.Request.Context(), appID, req.Days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到统计成功", item)
}

func (h *Handler) AdminAppSignInRecords(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppSignInRecordQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	dateFrom, err := parseOptionalDateInLocation(req.DateFrom, time.Local)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "dateFrom 格式无效，要求 YYYY-MM-DD")
		return
	}
	dateTo, err := parseOptionalDateInLocation(req.DateTo, time.Local)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "dateTo 格式无效，要求 YYYY-MM-DD")
		return
	}
	if dateFrom != nil && dateTo != nil && dateFrom.After(*dateTo) {
		response.Error(c, http.StatusBadRequest, 40000, "dateFrom 不能晚于 dateTo")
		return
	}

	item, err := h.app.ListAppSignInRecords(c.Request.Context(), appID, appdomain.AppSignInRecordQuery{
		Keyword:  req.Keyword,
		Source:   req.Source,
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Page:     req.Page,
		Limit:    req.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到明细成功", item)
}

func (h *Handler) GetAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicyAppIDRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.GetPasswordPolicy(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取密码策略成功", item)
}

func (h *Handler) SetAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicySetRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SetPasswordPolicy(c.Request.Context(), req.AppID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略设置成功", item)
}

func (h *Handler) TestAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicyTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.TestPasswordPolicy(c.Request.Context(), req.AppID, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略测试完成", item)
}

func (h *Handler) PasswordPolicyTemplates(c *gin.Context) {
	response.Success(c, 200, "获取密码策略模板成功", gin.H{
		"templates": h.app.GetPasswordPolicyTemplates(),
		"usage": gin.H{
			"basic":      "适合个人应用或对安全要求不高的场景",
			"standard":   "适合大多数商业应用",
			"strict":     "适合金融、医疗等高安全要求行业",
			"enterprise": "适合大型企业内部系统",
		},
	})
}

func (h *Handler) ResetAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicyAppIDRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.ResetPasswordPolicy(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略已重置", item)
}

func (h *Handler) saveAdminApp(c *gin.Context, appID int64, req AdminAppUpsertRequest) {
	item, err := h.app.SaveApp(c.Request.Context(), appdomain.AppMutation{
		ID:                     appID,
		Name:                   req.Name,
		Status:                 req.Status,
		DisabledReason:         req.DisabledReason,
		RegisterStatus:         req.RegisterStatus,
		DisabledRegisterReason: req.DisabledRegisterReason,
		LoginStatus:            req.LoginStatus,
		DisabledLoginReason:    req.DisabledLoginReason,
		Settings:               req.Settings,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}
