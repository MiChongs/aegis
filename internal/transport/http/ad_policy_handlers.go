package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/adpolicy"
	"aegis/pkg/response"
)

// 广告服务策略：管理端（策略 / 选择 / 变更历史 / 开屏记录 / 统计 / 用户详情）
// 与用户端（拉取策略 / 记录选择 / 开屏上报）。

// ── DTO ──

// AdSplashConfigRequest 开屏广告配置。
type AdSplashConfigRequest struct {
	Enabled            bool   `json:"enabled"`
	PlacementID        string `json:"placementId"`
	MinIntervalSeconds int    `json:"minIntervalSeconds"`
	DailyLimit         int    `json:"dailyLimit"`
}

// SaveAdPolicyRequest 保存广告服务策略。
type SaveAdPolicyRequest struct {
	Enabled        bool                  `json:"enabled"`
	ConsentVersion int                   `json:"consentVersion"`
	PolicyURL      string                `json:"policyUrl"`
	VipExempt      bool                  `json:"vipExempt"`
	BasicTools     []string              `json:"basicTools"`
	Splash         AdSplashConfigRequest `json:"splash"`
}

// AdConsentRequest 记录用户对广告服务的选择。
//
// version 是用户看到的条款版本（/ads/policy 的 consentVersion），不传按当前版本。
// ifAbsent 用于把未登录时在本机做的选择同步到账号上：账号上已经有选择时不覆盖，原样返回。
// source 为 app（默认）/ web / guest_sync。
type AdConsentRequest struct {
	Accepted *bool  `json:"accepted" binding:"required"`
	Version  int    `json:"version"`
	IfAbsent bool   `json:"ifAbsent"`
	Source   string `json:"source"`
	DeviceID string `json:"deviceId"`
}

// AdSplashEventRequest 一次开屏尝试。eventId 由客户端生成（8–64 位字母数字 _ -），重传不会重复记录。
type AdSplashEventRequest struct {
	EventID      string     `json:"eventId"`
	PlacementID  string     `json:"placementId"`
	Status       string     `json:"status"`
	ErrorCode    string     `json:"errorCode"`
	ErrorMessage string     `json:"errorMessage"`
	LoadMs       int        `json:"loadMs"`
	ShownMs      int        `json:"shownMs"`
	OccurredAt   *time.Time `json:"occurredAt"`
}

// AdSplashReportRequest 一批开屏记录（最多 50 条）。
type AdSplashReportRequest struct {
	Events   []AdSplashEventRequest `json:"events" binding:"required"`
	DeviceID string                 `json:"deviceId"`
}

// ── 管理端 ──

// AdminAdPolicy 读取广告服务策略。
func (h *Handler) AdminAdPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	policy, err := h.adPolicy.AdminPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", policy)
}

// AdminSaveAdPolicy 保存广告服务策略。
func (h *Handler) AdminSaveAdPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req SaveAdPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	policy, err := h.adPolicy.SavePolicy(c.Request.Context(), adpolicy.SavePolicyInput{
		AppID:          appID,
		Enabled:        req.Enabled,
		ConsentVersion: req.ConsentVersion,
		PolicyURL:      req.PolicyURL,
		VipExempt:      req.VipExempt,
		BasicTools:     req.BasicTools,
		Splash: adpolicy.SplashConfig{
			Enabled:            req.Splash.Enabled,
			PlacementID:        req.Splash.PlacementID,
			MinIntervalSeconds: req.Splash.MinIntervalSeconds,
			DailyLimit:         req.Splash.DailyLimit,
		},
		Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "广告服务策略已保存", policy)
}

// AdminListAdConsents 按当前选择列用户（accepted / outdated / source / keyword）。
func (h *Handler) AdminListAdConsents(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	query := adpolicy.ConsentQuery{
		AppID:   appID,
		Source:  strings.TrimSpace(c.Query("source")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	}
	if accepted, set := parseOptionalBool(c.Query("accepted")); set {
		query.Accepted = &accepted
	}
	query.Outdated, _ = parseOptionalBool(c.Query("outdated"))
	page, err := h.adPolicy.ListConsents(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminListAdConsentLogs 选择的变更历史（userId / keyword）。
func (h *Handler) AdminListAdConsentLogs(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.adPolicy.ListConsentLogs(c.Request.Context(), adpolicy.ConsentLogQuery{
		AppID:   appID,
		UserID:  userID,
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminListSplashAdEvents 开屏记录（userId / status / keyword / start / end）。
func (h *Handler) AdminListSplashAdEvents(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	query := adpolicy.SplashQuery{
		AppID:   appID,
		UserID:  userID,
		Status:  strings.TrimSpace(c.Query("status")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	}
	query.Start, query.End = parseRewardedAdRange(c.Query("start"), c.Query("end"))
	page, err := h.adPolicy.ListSplashEvents(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminAdPolicyStats 概览与按天趋势。
func (h *Handler) AdminAdPolicyStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.adPolicy.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// AdminUserAdPolicy 一个用户的广告服务情况。
func (h *Handler) AdminUserAdPolicy(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	profile, err := h.adPolicy.UserProfile(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", profile)
}

// ── 用户端 ──

// AppAdPolicy 广告服务策略与当前用户的处境。免登录可用：未登录时开屏频控按设备算，
// 选择按「还没选」回答，客户端以本机的选择为准。
func (h *Handler) AppAdPolicy(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var userID int64
	if session := gatewayReleaseSession(c); session != nil && session.AppID == app.ID {
		userID = session.UserID
	}
	deviceID, _ := h.resolveGatewayDevice(c, c.Query("deviceId"), "")
	policy, err := h.adPolicy.ClientPolicy(c.Request.Context(), app.ID, userID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", policy)
}

// AppSaveAdConsent 记录当前用户对广告服务的选择，返回记录后的策略视图。
func (h *Handler) AppSaveAdConsent(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req AdConsentRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	policy, err := h.adPolicy.SaveConsent(c.Request.Context(), adpolicy.SaveConsentInput{
		AppID:    session.AppID,
		UserID:   session.UserID,
		Accepted: *req.Accepted,
		Version:  req.Version,
		IfAbsent: req.IfAbsent,
		Source:   req.Source,
		DeviceID: deviceID,
		ClientIP: c.ClientIP(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已拒绝广告服务"
	if policy.Consent != nil && policy.Consent.Accepted {
		message = "已同意广告服务"
	}
	response.Success(c, http.StatusOK, message, policy)
}

// AppReportSplashAd 上报一批开屏记录。免登录可用：带令牌时记到账号上，否则只有设备标识。
func (h *Handler) AppReportSplashAd(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AdSplashReportRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var userID int64
	if session := gatewayReleaseSession(c); session != nil && session.AppID == app.ID {
		userID = session.UserID
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	events := make([]adpolicy.SplashEventInput, 0, len(req.Events))
	for _, item := range req.Events {
		events = append(events, adpolicy.SplashEventInput{
			EventID:      item.EventID,
			PlacementID:  item.PlacementID,
			Status:       item.Status,
			ErrorCode:    item.ErrorCode,
			ErrorMessage: item.ErrorMessage,
			LoadMs:       item.LoadMs,
			ShownMs:      item.ShownMs,
			OccurredAt:   item.OccurredAt,
		})
	}
	result, err := h.adPolicy.ReportSplash(c.Request.Context(), adpolicy.SplashReportInput{
		AppID:    app.ID,
		UserID:   userID,
		DeviceID: deviceID,
		ClientIP: c.ClientIP(),
		Events:   events,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已记录", result)
}
