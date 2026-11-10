package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/domain/suspension"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 账号封禁、全站封禁与申诉（docs/account-suspension.md）。
// 客户端三个接口在应用网关下，凭证是请求体里的申诉凭证或会话（二选一）；管理端按应用与全站两组。

func (h *Handler) suspensionReady(c *gin.Context) bool {
	if h.suspension == nil {
		response.Error(c, http.StatusServiceUnavailable, 50331, "封禁申诉服务未接入")
		return false
	}
	return true
}

type suspensionClientRequest struct {
	Token        string `json:"token"`
	Content      string `json:"content"`
	ContactEmail string `json:"contactEmail"`
	AppealID     int64  `json:"appealId"`
}

func (h *Handler) suspensionCredential(c *gin.Context) (service.SuspensionCredential, suspensionClientRequest, bool) {
	var req suspensionClientRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "请求参数不正确")
			return service.SuspensionCredential{}, req, false
		}
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return service.SuspensionCredential{}, req, false
	}
	cred := service.SuspensionCredential{AppID: app.ID, Token: strings.TrimSpace(req.Token)}
	if session, ok := authSession(c); ok {
		cred.Session = session
	}
	return cred, req, true
}

// AppSuspensionStatus POST /api/v1/apps/:appkey/suspension
func (h *Handler) AppSuspensionStatus(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	cred, _, ok := h.suspensionCredential(c)
	if !ok {
		return
	}
	out, err := h.suspension.Status(c.Request.Context(), cred)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", out)
}

// AppSubmitSuspensionAppeal POST /api/v1/apps/:appkey/suspension/appeal
func (h *Handler) AppSubmitSuspensionAppeal(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	cred, req, ok := h.suspensionCredential(c)
	if !ok {
		return
	}
	source := "app"
	if strings.EqualFold(c.GetHeader("X-Client-Kind"), "web") {
		source = "web"
	}
	out, err := h.suspension.SubmitAppeal(c.Request.Context(), service.AppealSubmitInput{
		Credential: cred, Content: req.Content, ContactEmail: req.ContactEmail, Source: source, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "申诉已提交", out)
}

// AppWithdrawSuspensionAppeal POST /api/v1/apps/:appkey/suspension/appeal/withdraw
func (h *Handler) AppWithdrawSuspensionAppeal(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	cred, req, ok := h.suspensionCredential(c)
	if !ok {
		return
	}
	out, err := h.suspension.WithdrawAppeal(c.Request.Context(), cred, req.AppealID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "申诉已撤回", out)
}

// ── 管理端：应用级申诉 ──

type adminAppealModel struct {
	Verdict        string     `json:"verdict"`
	Confidence     float64    `json:"confidence"`
	Summary        string     `json:"summary,omitempty"`
	SuggestedReply string     `json:"suggestedReply,omitempty"`
	Model          string     `json:"model,omitempty"`
	ReviewedAt     *time.Time `json:"reviewedAt"`
	Error          string     `json:"error,omitempty"`
}

type adminAppealBan struct {
	ID                int64      `json:"id"`
	Level             string     `json:"level"`
	BanType           string     `json:"banType"`
	BanScope          string     `json:"banScope"`
	Status            string     `json:"status"`
	ReasonCode        string     `json:"reasonCode"`
	Reason            string     `json:"reason"`
	PublicReason      string     `json:"publicReason"`
	Source            string     `json:"source"`
	PlatformBanID     *int64     `json:"platformBanId"`
	Appealable        bool       `json:"appealable"`
	BannedByAdminName string     `json:"bannedByAdminName"`
	StartAt           time.Time  `json:"startAt"`
	EndAt             *time.Time `json:"endAt"`
}

type adminAppealUser struct {
	ID          int64      `json:"id"`
	PublicID    int64      `json:"publicId,omitempty"`
	Account     string     `json:"account,omitempty"`
	Nickname    string     `json:"nickname,omitempty"`
	Avatar      string     `json:"avatar,omitempty"`
	Email       string     `json:"email,omitempty"`
	Phone       string     `json:"phone,omitempty"`
	Enabled     bool       `json:"enabled"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
}

type adminAppeal struct {
	ID                 int64             `json:"id"`
	AppID              int64             `json:"appid"`
	AppName            string            `json:"appName,omitempty"`
	BanID              int64             `json:"banId"`
	UserID             int64             `json:"userId"`
	Status             string            `json:"status"`
	Content            string            `json:"content"`
	ContactEmail       string            `json:"contactEmail,omitempty"`
	Locale             string            `json:"locale,omitempty"`
	Source             string            `json:"source,omitempty"`
	SubmittedAt        time.Time         `json:"submittedAt"`
	DecidedAt          *time.Time        `json:"decidedAt"`
	DecidedBy          string            `json:"decidedBy"`
	DecidedByAdminName string            `json:"decidedByAdminName,omitempty"`
	Reply              string            `json:"reply,omitempty"`
	Note               string            `json:"note,omitempty"`
	Model              *adminAppealModel `json:"model"`
	User               *adminAppealUser  `json:"user"`
	Ban                *adminAppealBan   `json:"ban"`
}

type adminAppealEvidence struct {
	Level           string   `json:"level"`
	MaxScore        int      `json:"maxScore"`
	BlockedCount    int      `json:"blockedCount"`
	RelatedAccounts int      `json:"relatedAccounts"`
	Rules           []string `json:"rules"`
	Lists           []string `json:"lists"`
	Facts           []string `json:"facts"`
}

type adminAppealDetail struct {
	Appeal   adminAppeal          `json:"appeal"`
	Ban      *adminAppealBan      `json:"ban"`
	User     *adminAppealUser     `json:"user"`
	Evidence *adminAppealEvidence `json:"evidence"`
	History  []adminAppeal        `json:"history"`
}

func toAdminAppealBan(b *userdomain.AccountBan) *adminAppealBan {
	if b == nil {
		return nil
	}
	return &adminAppealBan{ID: b.ID, Level: b.Level, BanType: b.BanType, BanScope: b.BanScope, Status: b.Status, ReasonCode: b.ReasonCode,
		Reason: b.Reason, PublicReason: b.PublicReason, Source: b.Source, PlatformBanID: b.PlatformBanID, Appealable: b.Appealable,
		BannedByAdminName: b.BannedByAdminName, StartAt: b.StartAt, EndAt: b.EndAt}
}

func toAdminAppeal(a suspension.Appeal, ban *userdomain.AccountBan) adminAppeal {
	out := adminAppeal{ID: a.ID, AppID: a.AppID, AppName: a.AppName, UserID: a.UserID, Status: a.Status, Content: a.Content, ContactEmail: a.ContactEmail,
		Locale: a.Locale, Source: a.Source, SubmittedAt: a.CreatedAt, DecidedAt: a.DecidedAt, DecidedBy: a.DecidedBy, DecidedByAdminName: a.ReviewerName,
		Reply: a.Reply, Note: a.ReviewNote, Ban: toAdminAppealBan(ban)}
	if a.BanID != nil {
		out.BanID = *a.BanID
	}
	if a.ModelVerdict != "" || a.ModelError != "" || a.ModelReviewedAt != nil {
		out.Model = &adminAppealModel{Verdict: a.ModelVerdict, Confidence: a.ModelConfidence, Summary: a.ModelSummary, SuggestedReply: a.ModelReply,
			Model: a.ModelName, ReviewedAt: a.ModelReviewedAt, Error: a.ModelError}
	}
	out.User = &adminAppealUser{ID: a.UserID, Account: a.Account, Nickname: a.Nickname}
	return out
}

func toAdminAppealUser(u *userdomain.RiskOpsUser, email, phone string) *adminAppealUser {
	if u == nil {
		return nil
	}
	created := u.CreatedAt
	out := &adminAppealUser{ID: u.ID, PublicID: u.PublicID, Account: u.Account, Nickname: u.Nickname, Avatar: u.Avatar, Enabled: u.Enabled,
		CreatedAt: &created, LastLoginAt: u.LastLoginAt}
	if email != "" {
		out.Email = maskForAdmin(email, true)
	}
	if phone != "" {
		out.Phone = maskForAdmin(phone, false)
	}
	return out
}

func maskForAdmin(v string, email bool) string {
	if email {
		at := strings.LastIndexByte(v, '@')
		if at <= 1 {
			return "***" + v[max(at, 0):]
		}
		return v[:min(2, at)] + "***" + v[at:]
	}
	if len(v) < 7 {
		return "****"
	}
	return v[:3] + "****" + v[len(v)-4:]
}

// toEvidence 风控复评的证据归成控制台的「事实」
func toEvidence(a suspension.Appeal) *adminAppealEvidence {
	if a.RiskLevel == "" {
		return nil
	}
	ev := a.RiskEvidence
	num := func(k string) int {
		switch v := ev[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
		return 0
	}
	str := func(k string) string { s, _ := ev[k].(string); return s }
	out := &adminAppealEvidence{Level: a.RiskLevel, MaxScore: num("maxScore30d"), BlockedCount: num("blocked30d"), RelatedAccounts: num("relatedStrong"),
		Rules: []string{}, Lists: []string{}, Facts: []string{}}
	if rules := str("lastRules"); rules != "" {
		for _, r := range strings.Split(rules, ",") {
			if r = strings.TrimSpace(r); r != "" {
				out.Rules = append(out.Rules, r)
			}
		}
	}
	if n := num("blockLists"); n > 0 {
		out.Lists = append(out.Lists, fmt.Sprintf("账号或它用过的设备在 %d 条黑名单里", n))
	}
	if avail, ok := ev["available"].(bool); ok && !avail {
		out.Facts = append(out.Facts, "复审时读不到风控平台的数据，只按封禁记录判断")
		return out
	}
	out.Facts = append(out.Facts, fmt.Sprintf("近 30 天被风控评估 %d 次，拦截 %d 次，转人工 %d 次，最高风险分 %d", num("evaluated30d"), num("blocked30d"), num("reviewed30d"), num("maxScore30d")))
	if r := str("lastReason"); r != "" {
		out.Facts = append(out.Facts, "最近一次被拦截的原因："+r)
	}
	if n := num("devices"); n > 0 {
		out.Facts = append(out.Facts, fmt.Sprintf("用过 %d 台设备", n))
	}
	if n := num("relatedStrong"); n > 0 {
		out.Facts = append(out.Facts, fmt.Sprintf("关系图里有 %d 个强关联账号，其中 %d 个被封禁", n, num("relatedStrongBanned")))
	}
	switch str("banSource") {
	case userdomain.AccountBanSourceManual:
		out.Facts = append(out.Facts, "封禁由管理员手动下发")
	case userdomain.AccountBanSourceRisk:
		out.Facts = append(out.Facts, "封禁由风控平台自动下发")
	case userdomain.AccountBanSourcePlatform:
		out.Facts = append(out.Facts, "封禁由全站封禁连带")
	}
	return out
}

func (h *Handler) appealDetailDTO(c *gin.Context, d *service.AppealDetail) adminAppealDetail {
	out := adminAppealDetail{Appeal: toAdminAppeal(d.Appeal, d.Ban), Ban: toAdminAppealBan(d.Ban), Evidence: toEvidence(d.Appeal), History: []adminAppeal{}}
	email, phone := "", ""
	if contact, err := h.suspension.Contact(c.Request.Context(), d.Appeal.AppID, d.Appeal.UserID); err == nil && contact != nil {
		email, phone = contact.Email, contact.Phone
	}
	out.User = toAdminAppealUser(d.User, email, phone)
	if out.User != nil {
		out.Appeal.User = out.User
	}
	for _, x := range d.History {
		out.History = append(out.History, toAdminAppeal(x, d.Ban))
	}
	return out
}

func (h *Handler) listAppealsDTO(c *gin.Context, appID int64) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("pageSize"))
	items, total, err := h.suspension.ListAppeals(c.Request.Context(), suspension.AppealQuery{AppID: appID, Status: c.Query("status"), Keyword: c.Query("q"), Page: page, PageSize: size})
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]adminAppeal, 0, len(items))
	for _, a := range items {
		var ban *userdomain.AccountBan
		if a.BanID != nil {
			ban, _ = h.suspension.Ban(c.Request.Context(), a.AppID, *a.BanID)
		}
		out = append(out, toAdminAppeal(a, ban))
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": out, "total": total, "page": max(page, 1), "pageSize": size})
}

// AdminListBanAppeals GET /api/admin/apps/:appkey/ban-appeals
func (h *Handler) AdminListBanAppeals(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	h.listAppealsDTO(c, appID)
}

// AdminBanAppealStats GET /api/admin/apps/:appkey/ban-appeals/stats
func (h *Handler) AdminBanAppealStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	stats, err := h.suspension.AppealStats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

func (h *Handler) appealID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("appealId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的申诉编号")
		return 0, false
	}
	return id, true
}

func (h *Handler) writeAppealDetail(c *gin.Context, appID, id int64, msg string) {
	d, err := h.suspension.AppealDetail(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, msg, h.appealDetailDTO(c, d))
}

// AdminGetBanAppeal GET /api/admin/apps/:appkey/ban-appeals/:appealId
func (h *Handler) AdminGetBanAppeal(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	id, ok := h.appealID(c)
	if !ok {
		return
	}
	h.writeAppealDetail(c, appID, id, "ok")
}

func (h *Handler) decideBanAppeal(c *gin.Context, approve bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	id, ok := h.appealID(c)
	if !ok {
		return
	}
	var req struct {
		Reply string `json:"reply"`
		Note  string `json:"note"`
	}
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	if _, err := h.suspension.DecideAppealByAdmin(c.Request.Context(), appID, id, approve, userdomain.BanOperator{AdminID: adminID, AdminName: adminName}, req.Note, req.Reply); err != nil {
		h.writeError(c, err)
		return
	}
	action, msg := "ban_appeal.reject", "已维持封禁"
	if approve {
		action, msg = "ban_appeal.approve", "已通过并解封"
	}
	h.recordAudit(c, action, "ban_appeal", strconv.FormatInt(id, 10), msg)
	h.writeAppealDetail(c, appID, id, msg)
}

// AdminApproveBanAppeal POST /api/admin/apps/:appkey/ban-appeals/:appealId/approve
func (h *Handler) AdminApproveBanAppeal(c *gin.Context) { h.decideBanAppeal(c, true) }

// AdminRejectBanAppeal POST /api/admin/apps/:appkey/ban-appeals/:appealId/reject
func (h *Handler) AdminRejectBanAppeal(c *gin.Context) { h.decideBanAppeal(c, false) }

// AdminRerunBanAppealReview POST /api/admin/apps/:appkey/ban-appeals/:appealId/review
func (h *Handler) AdminRerunBanAppealReview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	id, ok := h.appealID(c)
	if !ok {
		return
	}
	if _, err := h.suspension.RerunReview(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	h.writeAppealDetail(c, appID, id, "已重新复审")
}

// AdminBanAppealSettings GET /api/admin/apps/:appkey/ban-appeals/settings
func (h *Handler) AdminBanAppealSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	v, err := h.suspension.GetSettings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", v.Settings)
}

// AdminSaveBanAppealSettings PUT /api/admin/apps/:appkey/ban-appeals/settings
func (h *Handler) AdminSaveBanAppealSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.suspensionReady(c) {
		return
	}
	var req suspension.Settings
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, adminName := adminActor(c)
	v, err := h.suspension.SaveSettings(c.Request.Context(), appID, req, adminName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "ban_appeal.settings", "app", strconv.FormatInt(appID, 10), "修改申诉设置")
	response.Success(c, http.StatusOK, "已保存", v.Settings)
}

// ── 管理端：全部应用的申诉与全站封禁（超级管理员） ──

// AdminListAllBanAppeals GET /api/admin/system/ban-appeals
func (h *Handler) AdminListAllBanAppeals(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	appID, _ := strconv.ParseInt(c.Query("appid"), 10, 64)
	h.listAppealsDTO(c, appID)
}

// AdminAllBanAppealStats GET /api/admin/system/ban-appeals/stats
func (h *Handler) AdminAllBanAppealStats(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	stats, err := h.suspension.AppealStats(c.Request.Context(), 0)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

type adminPlatformBan struct {
	suspension.PlatformBan
	OriginAppName      string `json:"originAppName,omitempty"`
	OriginAccount      string `json:"originAccount,omitempty"`
	CreatedByAdminName string `json:"createdByAdminName,omitempty"`
	RevokedByAdminName string `json:"revokedByAdminName,omitempty"`
}

func (h *Handler) platformBanDTO(c *gin.Context, b suspension.PlatformBan) adminPlatformBan {
	out := adminPlatformBan{PlatformBan: b, CreatedByAdminName: b.CreatedByName, RevokedByAdminName: b.RevokedByName}
	if b.OriginAppID != nil && b.OriginUserID != nil {
		if contact, err := h.suspension.Contact(c.Request.Context(), *b.OriginAppID, *b.OriginUserID); err == nil && contact != nil {
			out.OriginAppName, out.OriginAccount = contact.AppName, contact.Account
		}
	}
	return out
}

// AdminListPlatformBans GET /api/admin/system/platform-bans
func (h *Handler) AdminListPlatformBans(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("pageSize"))
	items, total, err := h.suspension.ListPlatformBans(c.Request.Context(), pgrepo.PlatformBanQuery{Status: c.Query("status"), Keyword: c.Query("q"), Page: page, PageSize: size})
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]adminPlatformBan, 0, len(items))
	for _, b := range items {
		out = append(out, h.platformBanDTO(c, b))
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": out, "total": total, "page": max(page, 1), "pageSize": size})
}

func (h *Handler) writePlatformBanDetail(c *gin.Context, id int64, msg string) {
	d, err := h.suspension.PlatformBanDetail(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	subjects, accounts := d.Subjects, d.Accounts
	d.Subjects, d.Accounts = nil, nil
	response.Success(c, http.StatusOK, msg, gin.H{"ban": h.platformBanDTO(c, *d), "subjects": subjects, "accounts": accounts})
}

func platformBanID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("platformBanId"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的全站封禁编号")
		return 0, false
	}
	return id, true
}

// AdminGetPlatformBan GET /api/admin/system/platform-bans/:platformBanId
func (h *Handler) AdminGetPlatformBan(c *gin.Context) {
	if !h.suspensionReady(c) {
		return
	}
	id, ok := platformBanID(c)
	if !ok {
		return
	}
	h.writePlatformBanDetail(c, id, "ok")
}

// AdminRevokePlatformBan POST /api/admin/system/platform-bans/:platformBanId/revoke
func (h *Handler) AdminRevokePlatformBan(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可解除全站封禁")
		return
	}
	if !h.suspensionReady(c) {
		return
	}
	id, ok := platformBanID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		response.Error(c, http.StatusBadRequest, 40000, "请填写解除原因")
		return
	}
	adminID, adminName := adminActor(c)
	n, err := h.suspension.RevokePlatform(c.Request.Context(), id, userdomain.BanOperator{AdminID: adminID, AdminName: adminName}, req.Reason, true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "platform_ban.revoke", "platform_ban", strconv.FormatInt(id, 10), fmt.Sprintf("解除全站封禁，连带解封 %d 个账号", n))
	h.writePlatformBanDetail(c, id, "已解除")
}
