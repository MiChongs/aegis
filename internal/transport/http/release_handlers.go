package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 发布中心接口。
//
// 管理端：/api/admin/apps/:appkey/releases/*，权限 version:read / version:write。
// 网关：  /api/v1/apps/:appkey/releases/*，检测、最新版本、历史与事件上报免登录（带令牌则按用户定向），
//        渠道的自助加入与退出需要登录。
//
// 旧的 /versions、/version/check 与 /api/user/check-version 原样保留，后两者内部改走同一套检测引擎。

// AdminReleaseSaveRequest 新建 / 更新版本。字段缺省表示不修改；channelId 为 null 或 0 表示不限渠道。
type AdminReleaseSaveRequest struct {
	Version          *string                       `json:"version"`
	VersionCode      *int64                        `json:"versionCode"`
	Title            *string                       `json:"title"`
	Notes            *string                       `json:"notes"`
	Platform         *string                       `json:"platform"`
	MinOSVersion     *string                       `json:"minOsVersion"`
	UpdateType       *string                       `json:"updateType"`
	MinSupportedCode *int64                        `json:"minSupportedCode"`
	Visibility       *string                       `json:"visibility"`
	Targeting        *appdomain.ReleaseTargeting   `json:"targeting"`
	RolloutPct       *int                          `json:"rolloutPct"`
	ChannelID        *int64                        `json:"channelId"`
	Assets           []appdomain.ReleaseAssetInput `json:"assets"`
}

// AdminReleasePublishRequest publishAt 为空或已过去表示立即发布。
type AdminReleasePublishRequest struct {
	PublishAt *time.Time `json:"publishAt"`
}

type AdminReleaseRolloutRequest struct {
	RolloutPct int `json:"rolloutPct"`
}

// AdminReleaseSimulateRequest 以某个客户端的身份模拟一次检测。
type AdminReleaseSimulateRequest struct {
	VersionCode int64    `json:"versionCode"`
	Platform    string   `json:"platform"`
	UserID      int64    `json:"userId"`
	DeviceID    string   `json:"deviceId"`
	Abis        []string `json:"abis"`
	OSVersion   int      `json:"osVersion"`
	DeviceModel string   `json:"deviceModel"`
	Locale      string   `json:"locale"`
	Region      string   `json:"region"`
}

// AppReleaseCheckQuery 检测更新的查询参数（仅用于接口文档）。abis 以逗号分隔、按偏好排序。
type AppReleaseCheckQuery struct {
	VersionCode int64  `json:"versionCode" form:"versionCode" binding:"required"`
	Platform    string `json:"platform" form:"platform"`
	Abis        string `json:"abis" form:"abis"`
	OSVersion   int    `json:"osVersion" form:"osVersion"`
	DeviceID    string `json:"deviceId" form:"deviceId"`
	Model       string `json:"model" form:"model"`
	Locale      string `json:"locale" form:"locale"`
	Region      string `json:"region" form:"region"`
}

// AppReleaseLatestQuery 最新版本的查询参数（仅用于接口文档）。
type AppReleaseLatestQuery struct {
	Platform string `json:"platform" form:"platform"`
	DeviceID string `json:"deviceId" form:"deviceId"`
}

// AppReleaseHistoryQuery 版本历史的查询参数（仅用于接口文档）。
type AppReleaseHistoryQuery struct {
	Page     int    `json:"page" form:"page"`
	Limit    int    `json:"limit" form:"limit"`
	Platform string `json:"platform" form:"platform"`
	DeviceID string `json:"deviceId" form:"deviceId"`
}

// AppReleaseEventRequest 客户端漏斗上报。event：downloaded / installed / failed / dismissed。
type AppReleaseEventRequest struct {
	ReleaseID int64  `json:"releaseId" binding:"required"`
	Event     string `json:"event" binding:"required"`
	AssetID   int64  `json:"assetId"`
	DeviceID  string `json:"deviceId"`
}

/* ───────────────────── 管理端 ───────────────────── */

func (h *Handler) releaseReady(c *gin.Context) bool {
	if h.release == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "发布中心未启用")
		return false
	}
	return true
}

func releaseIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("rid"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本 ID 无效")
		return 0, false
	}
	return id, true
}

// AdminListReleases GET /api/admin/apps/:appkey/releases
func (h *Handler) AdminListReleases(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	channelID, _ := strconv.ParseInt(c.Query("channelId"), 10, 64)
	result, err := h.release.List(c.Request.Context(), appID, appdomain.ReleaseListQuery{
		Status:    c.Query("status"),
		Platform:  c.Query("platform"),
		ChannelID: channelID,
		Keyword:   c.Query("keyword"),
		Page:      queryInt(c, "page", 1),
		Limit:     queryInt(c, "limit", 20),
	}, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminReleaseOverview GET /api/admin/apps/:appkey/releases/overview
func (h *Handler) AdminReleaseOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	result, err := h.release.Overview(c.Request.Context(), appID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminCreateRelease POST /api/admin/apps/:appkey/releases
func (h *Handler) AdminCreateRelease(c *gin.Context) {
	h.saveRelease(c, 0)
}

// AdminUpdateRelease PUT /api/admin/apps/:appkey/releases/:rid
func (h *Handler) AdminUpdateRelease(c *gin.Context) {
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	h.saveRelease(c, id)
}

func (h *Handler) saveRelease(c *gin.Context, releaseID int64) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	raw, _ := snapshotRequestBody(c)
	var req AdminReleaseSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	mutation := appdomain.ReleaseMutation{
		ID:               releaseID,
		AppID:            appID,
		Version:          req.Version,
		VersionCode:      req.VersionCode,
		Title:            req.Title,
		Notes:            req.Notes,
		Platform:         req.Platform,
		MinOSVersion:     req.MinOSVersion,
		UpdateType:       req.UpdateType,
		MinSupportedCode: req.MinSupportedCode,
		Visibility:       req.Visibility,
		Targeting:        req.Targeting,
		RolloutPct:       req.RolloutPct,
		Assets:           req.Assets,
		CreatedBy:        currentAdminID(c),
	}
	switch {
	case req.ChannelID != nil && *req.ChannelID > 0:
		mutation.ChannelID = req.ChannelID
	case req.ChannelID != nil || explicitNullFields(raw, "channelId")["channelId"]:
		mutation.ClearChannel = true
	}
	item, err := h.release.Save(c.Request.Context(), mutation, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	status := http.StatusOK
	if releaseID == 0 {
		status = http.StatusCreated
	}
	response.Success(c, status, "保存成功", item)
}

// AdminGetRelease GET /api/admin/apps/:appkey/releases/:rid
func (h *Handler) AdminGetRelease(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	item, err := h.release.Detail(c.Request.Context(), appID, id, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", item)
}

// AdminDeleteRelease DELETE /api/admin/apps/:appkey/releases/:rid
func (h *Handler) AdminDeleteRelease(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	if err := h.release.Delete(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminPublishRelease POST /api/admin/apps/:appkey/releases/:rid/publish
func (h *Handler) AdminPublishRelease(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	var req AdminReleasePublishRequest
	if c.Request.ContentLength != 0 {
		if err := bind(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, err.Error())
			return
		}
	}
	item, err := h.release.Publish(c.Request.Context(), appID, id, req.PublishAt, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已发布"
	if item.Status == appdomain.ReleaseStatusScheduled {
		message = "已设定定时发布"
	}
	response.Success(c, http.StatusOK, message, item)
}

func (h *Handler) transitionRelease(c *gin.Context, action string, message string) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	item, err := h.release.Transition(c.Request.Context(), appID, id, action, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, message, item)
}

// AdminPauseRelease POST /api/admin/apps/:appkey/releases/:rid/pause
func (h *Handler) AdminPauseRelease(c *gin.Context) {
	h.transitionRelease(c, "pause", "已暂停下发")
}

// AdminResumeRelease POST /api/admin/apps/:appkey/releases/:rid/resume
func (h *Handler) AdminResumeRelease(c *gin.Context) {
	h.transitionRelease(c, "resume", "已恢复下发")
}

// AdminRevokeRelease POST /api/admin/apps/:appkey/releases/:rid/revoke
func (h *Handler) AdminRevokeRelease(c *gin.Context) { h.transitionRelease(c, "revoke", "已撤回") }

// AdminSetReleaseRollout PUT /api/admin/apps/:appkey/releases/:rid/rollout
func (h *Handler) AdminSetReleaseRollout(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	var req AdminReleaseRolloutRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.release.SetRollout(c.Request.Context(), appID, id, req.RolloutPct, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "灰度比例已更新", item)
}

// AdminReleaseStats GET /api/admin/apps/:appkey/releases/:rid/stats
func (h *Handler) AdminReleaseStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	id, ok := releaseIDParam(c)
	if !ok {
		return
	}
	stats, err := h.release.Stats(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", stats)
}

// AdminSimulateRelease POST /api/admin/apps/:appkey/releases/simulate
func (h *Handler) AdminSimulateRelease(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	var req AdminReleaseSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.release.Simulate(c.Request.Context(), appID, appdomain.ReleaseClient{
		VersionCode: req.VersionCode, Platform: strings.ToLower(strings.TrimSpace(req.Platform)),
		UserID: req.UserID, DeviceID: req.DeviceID, Abis: req.Abis, OSVersion: req.OSVersion,
		DeviceModel: req.DeviceModel, Locale: req.Locale, Region: req.Region,
	}, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "模拟完成", result)
}

// AdminUploadReleaseAsset POST /api/admin/apps/:appkey/releases/assets（multipart，file 字段）
func (h *Handler) AdminUploadReleaseAsset(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.releaseReady(c) {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()
	result, err := h.release.UploadAsset(c.Request.Context(), appID, requestBaseURL(c.Request), service.ContentImageUploadInput{
		ConfigName:    strings.TrimSpace(c.PostForm("config_name")),
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploadedBy:    currentAdminID(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", result)
}

/* ───────────────────── 网关 ───────────────────── */

// releaseClientFromQuery 从查询参数与（可选的）登录态拼出客户端身份。
func releaseClientFromQuery(c *gin.Context, appID int64) appdomain.ReleaseClient {
	client := appdomain.ReleaseClient{
		Platform:    strings.ToLower(strings.TrimSpace(c.DefaultQuery("platform", "android"))),
		DeviceID:    strings.TrimSpace(c.Query("deviceId")),
		DeviceModel: strings.TrimSpace(c.Query("model")),
		Locale:      strings.TrimSpace(c.Query("locale")),
		Region:      strings.TrimSpace(c.Query("region")),
	}
	client.VersionCode, _ = strconv.ParseInt(strings.TrimSpace(c.Query("versionCode")), 10, 64)
	client.OSVersion, _ = strconv.Atoi(strings.TrimSpace(c.Query("osVersion")))
	for _, abi := range strings.Split(c.Query("abis"), ",") {
		if abi = strings.TrimSpace(abi); abi != "" {
			client.Abis = append(client.Abis, abi)
		}
	}
	if session := gatewayReleaseSession(c); session != nil && session.AppID == appID {
		client.UserID = session.UserID
		if client.DeviceID == "" {
			client.DeviceID = session.DeviceID
		}
	}
	return client
}

func gatewayReleaseSession(c *gin.Context) *authdomain.Session {
	value, ok := c.Get("auth.session")
	if !ok {
		return nil
	}
	session, _ := value.(*authdomain.Session)
	return session
}

// AppReleaseCheck GET /api/v1/apps/:appkey/releases/check
func (h *Handler) AppReleaseCheck(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok || !h.releaseReady(c) {
		return
	}
	if strings.TrimSpace(c.Query("versionCode")) == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少 versionCode")
		return
	}
	result, err := h.release.Check(c.Request.Context(), app.ID, releaseClientFromQuery(c, app.ID), requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已是最新版本"
	if result.HasUpdate {
		message = "有新版本"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppReleaseLatest GET /api/v1/apps/:appkey/releases/latest
// 未登录时只返回公开且已全量的版本；登录后按该用户的渠道、定向与灰度返回。没有可用版本时 data 为 null。
func (h *Handler) AppReleaseLatest(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok || !h.releaseReady(c) {
		return
	}
	result, err := h.release.Latest(c.Request.Context(), app.ID, releaseClientFromQuery(c, app.ID), requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AppReleaseHistory GET /api/v1/apps/:appkey/releases
func (h *Handler) AppReleaseHistory(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok || !h.releaseReady(c) {
		return
	}
	result, err := h.release.History(c.Request.Context(), app.ID, releaseClientFromQuery(c, app.ID),
		queryInt(c, "page", 1), queryInt(c, "limit", 10), requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AppReleaseEvent POST /api/v1/apps/:appkey/releases/events
func (h *Handler) AppReleaseEvent(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok || !h.releaseReady(c) {
		return
	}
	var req AppReleaseEventRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	client := appdomain.ReleaseClient{DeviceID: strings.TrimSpace(req.DeviceID)}
	if session := gatewayReleaseSession(c); session != nil && session.AppID == app.ID {
		client.UserID = session.UserID
	}
	if err := h.release.RecordEvent(c.Request.Context(), app.ID, req.ReleaseID, strings.TrimSpace(req.Event), req.AssetID, client); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已记录", gin.H{"recorded": true})
}

// AppReleaseChannels GET /api/v1/apps/:appkey/releases/channels（需登录）
func (h *Handler) AppReleaseChannels(c *gin.Context) {
	session, ok := h.releaseGatewayUser(c)
	if !ok {
		return
	}
	items, err := h.release.Channels(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AppReleaseChannelJoin POST /api/v1/apps/:appkey/releases/channels/:code/join（需登录）
func (h *Handler) AppReleaseChannelJoin(c *gin.Context) { h.setReleaseChannel(c, true, "已加入") }

// AppReleaseChannelLeave POST /api/v1/apps/:appkey/releases/channels/:code/leave（需登录）
func (h *Handler) AppReleaseChannelLeave(c *gin.Context) { h.setReleaseChannel(c, false, "已退出") }

func (h *Handler) setReleaseChannel(c *gin.Context, join bool, message string) {
	session, ok := h.releaseGatewayUser(c)
	if !ok {
		return
	}
	items, err := h.release.SetChannelMembership(c.Request.Context(), session.AppID, session.UserID, c.Param("code"), join)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, message, items)
}

func (h *Handler) releaseGatewayUser(c *gin.Context) (*authdomain.Session, bool) {
	if !h.releaseReady(c) {
		return nil, false
	}
	session := gatewayReleaseSession(c)
	if session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "请先登录")
		return nil, false
	}
	return session, true
}

/* ───────────────────── 旧检测接口 ───────────────────── */

// checkVersionViaRelease 旧 /version/check 与 /api/user/check-version 的新实现：响应仍是 AppVersionCheckResult。
func (h *Handler) checkVersionViaRelease(c *gin.Context, query VersionCheckQuery, session *authdomain.Session) {
	client := appdomain.ReleaseClient{
		VersionCode: query.VersionCode,
		Platform:    strings.ToLower(strings.TrimSpace(query.Platform)),
		DeviceID:    strings.TrimSpace(c.Query("deviceId")),
	}
	if client.Platform == "" || client.Platform == "all" {
		client.Platform = "android"
	}
	if session != nil && session.AppID == query.AppID {
		client.UserID = session.UserID
		if client.DeviceID == "" {
			client.DeviceID = session.DeviceID
		}
	}
	result, err := h.release.Check(c.Request.Context(), query.AppID, client, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	if !result.HasUpdate || result.Release == nil {
		response.Error(c, http.StatusNotFound, 40430, "暂无新版本信息")
		return
	}
	rel := result.Release
	legacy := &appdomain.AppVersion{
		ID: rel.ID, AppID: query.AppID, Version: rel.Version, VersionCode: rel.VersionCode,
		Description: rel.Summary, ReleaseNotes: rel.Notes, ForceUpdate: result.UpdateType == appdomain.ReleaseUpdateForce,
		UpdateType: result.UpdateType, Platform: rel.Platform, MinOSVersion: rel.MinOSVersion, Status: appdomain.ReleaseStatusPublished,
	}
	if rel.Channel != nil {
		legacy.ChannelID = &rel.Channel.ID
		legacy.ChannelName = rel.Channel.Name
	}
	if rel.PublishedAt != nil {
		legacy.CreatedAt, legacy.UpdatedAt = *rel.PublishedAt, *rel.PublishedAt
	}
	if result.Asset != nil {
		legacy.DownloadURL, legacy.FileSize, legacy.FileHash = result.Asset.DownloadURL, result.Asset.FileSize, result.Asset.SHA256
		legacy.DownloadCount = result.Asset.DownloadCount
	}
	result2 := &appdomain.AppVersionCheckResult{Version: legacy}
	if rel.Channel != nil {
		result2.ChannelName = rel.Channel.Name
	}
	response.Success(c, http.StatusOK, "有新版本", result2)
}
