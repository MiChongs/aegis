package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/invite"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 邀请：管理端（配置 / 邀请码 / 邀请关系 / 统计）与用户端（邀请页 / 我邀请的人 / 填写邀请码 /
// 换邀请码 / 注册页预览）。

// ── DTO ──

// InviteRewardRequest 一项奖励。取值字段由权益目录的 value 决定：amount（整数量；功能解锁时是天数，
// 0 为永久）、money（金额，字符串）、refId（会员套餐）、tag（功能标识）。
type InviteRewardRequest struct {
	Type   string `json:"type" binding:"required"`
	Amount int64  `json:"amount"`
	Money  string `json:"money"`
	RefID  int64  `json:"refId"`
	Tag    string `json:"tag"`
}

// InviteMilestoneRequest 一档阶梯奖励：累计成功邀请满 count 人时发一次。
type InviteMilestoneRequest struct {
	Count   int                   `json:"count"`
	Title   string                `json:"title"`
	Rewards []InviteRewardRequest `json:"rewards"`
}

// InviteSettingsRequest 邀请的开关与配置。字段含义见 docs/invite.md。
type InviteSettingsRequest struct {
	Enabled           bool                     `json:"enabled"`
	RegisterField     string                   `json:"registerField"`
	FieldLabel        string                   `json:"fieldLabel"`
	FieldPlaceholder  string                   `json:"fieldPlaceholder"`
	BindWindowHours   int                      `json:"bindWindowHours"`
	CodeLength        int                      `json:"codeLength"`
	CodeCharset       string                   `json:"codeCharset"`
	CodePrefix        string                   `json:"codePrefix"`
	AllowCustomCode   bool                     `json:"allowCustomCode"`
	InviterAudience   string                   `json:"inviterAudience"`
	InviterMinLevel   int                      `json:"inviterMinLevel"`
	InviteeRewards    []InviteRewardRequest    `json:"inviteeRewards"`
	InviterRewards    []InviteRewardRequest    `json:"inviterRewards"`
	Milestones        []InviteMilestoneRequest `json:"milestones"`
	InviterTrigger    string                   `json:"inviterTrigger"`
	TriggerSignInDays int                      `json:"triggerSignInDays"`
	InviterDailyLimit int                      `json:"inviterDailyLimit"`
	InviterTotalLimit int                      `json:"inviterTotalLimit"`
	NotifyInviter     bool                     `json:"notifyInviter"`
	DeviceLimit       int                      `json:"deviceLimit"`
	IPDailyLimit      int                      `json:"ipDailyLimit"`
	RulesText         string                   `json:"rulesText"`
	ShareTemplate     string                   `json:"shareTemplate"`
	LandingURL        string                   `json:"landingUrl"`
}

// InviteCodeRequest 创建 / 更新活动码。
//
// 创建时：code 非空为指定码（单个）；为空时按应用的格式生成 count 个，prefix 可另给前缀。
// rewards 不传或 null 为跟随应用默认的被邀请人奖励；传空数组为这个码不发奖励。
// maxUses 不传、null 或 0 为不限次数。ownerUserId 为推广人（拿邀请人奖励），不传为纯渠道码。
type InviteCodeRequest struct {
	Code        string                 `json:"code"`
	Count       int                    `json:"count"`
	Prefix      string                 `json:"prefix"`
	OwnerUserID *int64                 `json:"ownerUserId"`
	Title       string                 `json:"title" binding:"required"`
	Note        string                 `json:"note"`
	Rewards     *[]InviteRewardRequest `json:"rewards"`
	MaxUses     *int64                 `json:"maxUses"`
	StartsAt    *time.Time             `json:"startsAt"`
	EndsAt      *time.Time             `json:"endsAt"`
	Status      string                 `json:"status"`
}

// InviteCodeStatusRequest 启用 / 停用一个码。
type InviteCodeStatusRequest struct {
	Active bool `json:"active"`
}

// InviteBindRequest 填写邀请码。
type InviteBindRequest struct {
	Code     string `json:"code" binding:"required"`
	DeviceID string `json:"deviceId"`
}

// InviteCodeUpdateRequest 换我的邀请码：code 非空为自定义（应用允许时），为空则重新生成。
type InviteCodeUpdateRequest struct {
	Code string `json:"code"`
}

// InvitePreviewRequest 注册页预览一个邀请码。
type InvitePreviewRequest struct {
	Code string `json:"code" binding:"required"`
}

// InviteRelationQuery 管理端邀请关系列表的查询参数。
type InviteRelationQuery struct {
	InviterID int64  `form:"inviterId"`
	InviteeID int64  `form:"inviteeId"`
	CodeID    int64  `form:"codeId"`
	Status    string `form:"status"`
	Source    string `form:"source"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

// InviteCodeQuery 管理端邀请码列表的查询参数。
type InviteCodeQuery struct {
	Kind    string `form:"kind"`
	Status  string `form:"status"`
	OwnerID int64  `form:"ownerId"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// InviteRegisterResult 注册结果：登录结果之外，多一个邀请码的结论。
type InviteRegisterResult struct {
	*authdomain.LoginResult
	Invite *invite.RegisterOutcome `json:"invite,omitempty"`
}

func inviteRewards(items []InviteRewardRequest) []invite.Reward {
	rewards := make([]invite.Reward, 0, len(items))
	for _, item := range items {
		reward := invite.Reward{Type: item.Type, Amount: item.Amount, RefID: item.RefID, Tag: item.Tag}
		// 金额按字符串传输：JSON number 在前端是双精度浮点
		if money := strings.TrimSpace(item.Money); money != "" {
			if parsed, err := decimal.NewFromString(money); err == nil {
				reward.Money = parsed
			}
		}
		rewards = append(rewards, reward)
	}
	return rewards
}

func (req InviteSettingsRequest) toConfig() invite.Config {
	milestones := make([]invite.Milestone, 0, len(req.Milestones))
	for _, milestone := range req.Milestones {
		milestones = append(milestones, invite.Milestone{
			Count: milestone.Count, Title: milestone.Title, Rewards: inviteRewards(milestone.Rewards),
		})
	}
	return invite.Config{
		RegisterField:     req.RegisterField,
		FieldLabel:        req.FieldLabel,
		FieldPlaceholder:  req.FieldPlaceholder,
		BindWindowHours:   req.BindWindowHours,
		CodeLength:        req.CodeLength,
		CodeCharset:       req.CodeCharset,
		CodePrefix:        req.CodePrefix,
		AllowCustomCode:   req.AllowCustomCode,
		InviterAudience:   req.InviterAudience,
		InviterMinLevel:   req.InviterMinLevel,
		InviteeRewards:    inviteRewards(req.InviteeRewards),
		InviterRewards:    inviteRewards(req.InviterRewards),
		Milestones:        milestones,
		InviterTrigger:    req.InviterTrigger,
		TriggerSignInDays: req.TriggerSignInDays,
		InviterDailyLimit: req.InviterDailyLimit,
		InviterTotalLimit: req.InviterTotalLimit,
		NotifyInviter:     req.NotifyInviter,
		DeviceLimit:       req.DeviceLimit,
		IPDailyLimit:      req.IPDailyLimit,
		RulesText:         req.RulesText,
		ShareTemplate:     req.ShareTemplate,
		LandingURL:        req.LandingURL,
	}
}

func (req InviteCodeRequest) toInput(appID int64, operator string) invite.CodeInput {
	input := invite.CodeInput{
		AppID: appID, Code: req.Code, Count: req.Count, Prefix: req.Prefix, OwnerUserID: req.OwnerUserID,
		Title: req.Title, Note: req.Note, MaxUses: req.MaxUses, StartsAt: req.StartsAt, EndsAt: req.EndsAt,
		Status: req.Status, Operator: operator,
	}
	if req.Rewards != nil {
		rewards := inviteRewards(*req.Rewards)
		input.Rewards = &rewards
	}
	return input
}

// resolveInviteUser 把对外展示的人的头像原值翻成地址。
func (h *Handler) resolveInviteUser(c *gin.Context, appID int64, user *invite.ClientUser) {
	if user == nil || h.avatar == nil {
		return
	}
	user.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
		appID, user.UserID, user.RawAvatar, user.Account)
}

// ── 管理端：目录与配置 ──

// AdminInviteCatalog 权益目录、各枚举、默认配置与上限。控制台的配置表单与活动码表单由它驱动。
func (h *Handler) AdminInviteCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	catalog, err := h.invite.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", catalog)
}

// AdminInviteSettings 邀请的开关与配置。
func (h *Handler) AdminInviteSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	settings, err := h.invite.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", settings)
}

// AdminSaveInviteSettings 保存邀请的开关与配置。
func (h *Handler) AdminSaveInviteSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req InviteSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	settings, err := h.invite.SaveSettings(c.Request.Context(), appID, req.Enabled, req.toConfig(), operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "邀请配置已保存", settings)
}

// ── 管理端：邀请码 ──

// AdminListInviteCodes 邀请码列表（kind / status / ownerId / keyword）。
func (h *Handler) AdminListInviteCodes(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query InviteCodeQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.invite.ListCodes(c.Request.Context(), invite.CodeQuery{
		AppID: appID, Kind: query.Kind, Status: query.Status, OwnerID: query.OwnerID, Keyword: query.Keyword,
		Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminCreateInviteCodes 创建活动码（指定一个或批量生成）。
func (h *Handler) AdminCreateInviteCodes(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req InviteCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	codes, err := h.invite.CreateCampaignCodes(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已创建 "+strconv.Itoa(len(codes))+" 个邀请码", gin.H{"items": codes})
}

// AdminUpdateInviteCode 更新活动码（码本身不改）。
func (h *Handler) AdminUpdateInviteCode(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	codeID, ok := cardKeyPathID(c, "codeId")
	if !ok {
		return
	}
	var req InviteCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	code, err := h.invite.UpdateCampaignCode(c.Request.Context(), codeID, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "邀请码已保存", code)
}

// AdminSetInviteCodeStatus 启用 / 停用一个码（个人码也可以停用）。
func (h *Handler) AdminSetInviteCodeStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	codeID, ok := cardKeyPathID(c, "codeId")
	if !ok {
		return
	}
	var req InviteCodeStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	code, err := h.invite.SetCodeStatus(c.Request.Context(), appID, codeID, req.Active)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已停用"
	if req.Active {
		message = "已启用"
	}
	response.Success(c, http.StatusOK, message, code)
}

// AdminDeleteInviteCode 删除活动码（邀请关系保留码的快照）。
func (h *Handler) AdminDeleteInviteCode(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	codeID, ok := cardKeyPathID(c, "codeId")
	if !ok {
		return
	}
	if err := h.invite.DeleteCode(c.Request.Context(), appID, codeID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// ── 管理端：邀请关系与统计 ──

// AdminListInviteRelations 邀请关系（inviterId / inviteeId / codeId / status / source / keyword）。
func (h *Handler) AdminListInviteRelations(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query InviteRelationQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.invite.ListRelations(c.Request.Context(), invite.RelationQuery{
		AppID: appID, InviterID: query.InviterID, InviteeID: query.InviteeID, CodeID: query.CodeID,
		InviterStatus: query.Status, Source: query.Source, Keyword: query.Keyword, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if h.avatar != nil {
		for index := range page.Items {
			item := &page.Items[index]
			item.InviteeAvatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
				appID, item.InviteeID, item.InviteeAvatar, item.InviteeAccount)
			if item.InviterID != nil {
				item.InviterAvatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
					appID, *item.InviterID, item.InviterAvatar, item.InviterAccount)
			}
		}
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminSettleInviteRelation 立即发放一条等待中的邀请人奖励（不看发放条件与上限）。
func (h *Handler) AdminSettleInviteRelation(c *gin.Context) {
	h.handleInviteRelation(c, true)
}

// AdminCancelInviteRelation 取消一条等待中的邀请人奖励（被邀请人已到账的不收回）。
func (h *Handler) AdminCancelInviteRelation(c *gin.Context) {
	h.handleInviteRelation(c, false)
}

func (h *Handler) handleInviteRelation(c *gin.Context, settle bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	relationID, ok := cardKeyPathID(c, "relationId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	if settle {
		relation, err := h.invite.SettleRelation(c.Request.Context(), appID, relationID, operator)
		if err != nil {
			h.writeError(c, err)
			return
		}
		response.Success(c, http.StatusOK, "邀请人奖励已发放", relation)
		return
	}
	relation, err := h.invite.CancelRelation(c.Request.Context(), appID, relationID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已取消", relation)
}

// AdminInviteStats 概览、按天趋势（days，默认 14，最多 90）、邀请最多的人与用得最多的活动码。
func (h *Handler) AdminInviteStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.invite.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// ── 用户端 ──

// AppInviteCenter 邀请页：我的邀请码、奖励规则、统计，以及我能不能填写邀请码。
// 未开启时 enabled=false（不报错），客户端据此隐藏入口。
func (h *Handler) AppInviteCenter(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	center, err := h.invite.Center(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if center.Bind.Bound != nil {
		h.resolveInviteUser(c, session.AppID, center.Bind.Bound.Inviter)
	}
	response.Success(c, http.StatusOK, "ok", center)
}

// AppInvitees 我邀请的人。
func (h *Handler) AppInvitees(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	page, err := h.invite.MyInvitees(c.Request.Context(), session.AppID, session.UserID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	if h.avatar != nil {
		for index := range page.Items {
			item := &page.Items[index]
			item.Avatar = h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request),
				session.AppID, item.UserID, item.RawAvatar, item.Account)
		}
	}
	if h.cosmetic != nil && len(page.Items) > 0 {
		ids := make([]int64, 0, len(page.Items))
		for _, item := range page.Items {
			ids = append(ids, item.UserID)
		}
		loadouts := h.cosmetic.LookupByUserIDs(c.Request.Context(), session.AppID, ids, requestBaseURL(c.Request))
		for index := range page.Items {
			if loadout, ok := loadouts[page.Items[index].UserID]; ok {
				page.Items[index].Cosmetics = &loadout
			}
		}
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppBindInvite 注册后在时限内补填邀请码。
func (h *Handler) AppBindInvite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req InviteBindRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	result, err := h.invite.Bind(c.Request.Context(), service.InviteBindRequest{
		AppID: session.AppID, UserID: session.UserID, Code: req.Code,
		DeviceID: deviceID, ClientIP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolveInviteUser(c, session.AppID, result.Inviter)
	response.Success(c, http.StatusOK, "填写成功", result)
}

// AppUpdateInviteCode 换我的邀请码：自定义（应用允许时）或重新生成。旧码立即失效。
func (h *Handler) AppUpdateInviteCode(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req InviteCodeUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	code, err := h.invite.UpdateMyCode(c.Request.Context(), session.AppID, session.UserID, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "邀请码已更新", gin.H{"code": code.Code, "custom": code.Custom})
}

// AppPreviewInvite 注册页预览邀请码：能不能用、谁邀请的、填了能拿什么。不需要登录。
// 码不能用不报错（valid=false + reason），注册页据此在输入框下提示。
func (h *Handler) AppPreviewInvite(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req InvitePreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	preview, err := h.invite.Preview(c.Request.Context(), app.ID, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolveInviteUser(c, app.ID, preview.Inviter)
	response.Success(c, http.StatusOK, "ok", preview)
}
