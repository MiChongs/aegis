package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"aegis/internal/domain/usernumber"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 用户号码（docs/user-number.md）：用户端查看、查号、联想、摇号、换号；管理端设置、目录、查询、指定、回滚、保留号、权益与各类记录。

// UserNumberChangeRequest 换号。
type UserNumberChangeRequest struct {
	// Number 要换的号码（纯数字字符串，避免大数在 JS 里丢精度）
	Number string `json:"number"`
	// DrawID 从这张摇号单里选（不再扣别的权益）
	DrawID int64 `json:"drawId"`
	// UseKind / UseLevel 指定用哪份权益（change / ticket + 1–4）；留空自动选最便宜的
	UseKind  string `json:"useKind"`
	UseLevel int    `json:"useLevel"`
}

// UserNumberPage 分页结果。
type UserNumberPage[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

func (h *Handler) userNumberReady(c *gin.Context) bool {
	if h.userNumber == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "号码服务不可用")
		return false
	}
	return true
}

// AppUserNumber 我的号码页：号码与等级、冷却、权益、打开的摇号单、今日号运与规则。
func (h *Handler) AppUserNumber(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	view, err := h.userNumber.Me(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", view)
}

// AppCheckUserNumber 查一个号码：等级、是否空闲、要什么权益、现在能不能换。
func (h *Handler) AppCheckUserNumber(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	result, err := h.userNumber.Check(c.Request.Context(), session.AppID, session.UserID, c.Query("number"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AppSuggestUserNumbers 自选联想：含 q 的空闲号码；q 为空时按 tier 给一组这一级的空闲号码。
func (h *Handler) AppSuggestUserNumbers(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	tier := -1
	if raw := c.Query("tier"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			tier = v
		}
	}
	items, err := h.userNumber.Suggest(c.Request.Context(), session.AppID, session.UserID, c.Query("q"), tier,
		parsePositiveInt(c.Query("count"), 12))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AppDrawUserNumbers 摇一次号（扣一次摇号次数），返回候选号与今日号运。
func (h *Handler) AppDrawUserNumbers(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	result, err := h.userNumber.Draw(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "摇号成功", result)
}

// AppDiscardUserNumberDraw 放弃打开的摇号单（次数不退）。
func (h *Handler) AppDiscardUserNumberDraw(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	drawID, err := strconv.ParseInt(c.Param("drawId"), 10, 64)
	if err != nil || drawID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "摇号单编号不合法")
		return
	}
	if err := h.userNumber.DiscardDraw(c.Request.Context(), session.AppID, session.UserID, drawID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已放弃", nil)
}

// AppChangeUserNumber 换号。
func (h *Handler) AppChangeUserNumber(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	var req UserNumberChangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.userNumber.Change(c.Request.Context(), session.AppID, session.UserID, req.Number, req.DrawID, req.UseKind, req.UseLevel)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "号码已更换", result)
}

// AppUserNumberHistory 我的换号记录。
func (h *Handler) AppUserNumberHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	page, limit := parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20)
	items, total, err := h.userNumber.History(c.Request.Context(), session.AppID, session.UserID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", UserNumberPage[usernumber.History]{Items: items, Total: total, Page: page, Limit: limit})
}

// AppUserNumberLedger 我的号码权益流水。
func (h *Handler) AppUserNumberLedger(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if !h.userNumberReady(c) {
		return
	}
	page, limit := parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20)
	items, total, err := h.userNumber.Ledger(c.Request.Context(), session.AppID, session.UserID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", UserNumberPage[usernumber.LedgerEntry]{Items: items, Total: total, Page: page, Limit: limit})
}

// ── 管理端 ──

// AdminUserNumberCatalog 等级、规律（带本应用的等级）、权益种类与流水来源。
func (h *Handler) AdminUserNumberCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	result, err := h.userNumber.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminUserNumberOverview 总览：各级号码数、今日与近 7 天换号、摇号、占用与未用的权益。
func (h *Handler) AdminUserNumberOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	result, err := h.userNumber.Overview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminUserNumberSettings 读设置。
func (h *Handler) AdminUserNumberSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	result, err := h.userNumber.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminSaveUserNumberSettings 保存设置（整份替换）。
func (h *Handler) AdminSaveUserNumberSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	var req usernumber.Settings
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.userNumber.SaveSettings(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// UserNumberOddsRequest 试算各级概率。
type UserNumberOddsRequest struct {
	Settings usernumber.Settings `json:"settings"`
	// Luck 号运系数（1 为平均）
	Luck float64 `json:"luck"`
}

// AdminPreviewUserNumberOdds 用一份（未保存的）设置试算某个号运下各级的出现概率。
func (h *Handler) AdminPreviewUserNumberOdds(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.userNumberReady(c) {
		return
	}
	var req UserNumberOddsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.Luck <= 0 {
		req.Luck = 1
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"odds": h.userNumber.PreviewOdds(req.Settings, req.Luck), "luck": req.Luck})
}

// AdminInspectUserNumber 查一个号码：等级、命中的规律、谁在用、占用。
func (h *Handler) AdminInspectUserNumber(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	result, err := h.userNumber.Inspect(c.Request.Context(), appID, c.Query("number"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminUserNumberOfUser 一个用户的号码、权益、摇号单、冻结的旧号与最近的换号记录。
func (h *Handler) AdminUserNumberOfUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	result, err := h.userNumber.AdminUser(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// UserNumberAssignRequest 管理员指定号码。
type UserNumberAssignRequest struct {
	Number string `json:"number"`
	// Force 可以拿保留号、别人的冻结号与候选号（仍不能抢别人正在用的号）
	Force bool   `json:"force"`
	Note  string `json:"note"`
}

// AdminAssignUserNumber 给用户指定号码（不扣权益、不看冷却）。
func (h *Handler) AdminAssignUserNumber(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req UserNumberAssignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.userNumber.Assign(c.Request.Context(), appID, userID, req.Number, req.Force, req.Note, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已指定", result)
}

// UserNumberRollbackRequest 回滚到某次换号之前的号码。
type UserNumberRollbackRequest struct {
	HistoryID int64 `json:"historyId"`
}

// AdminRollbackUserNumber 回滚号码。
func (h *Handler) AdminRollbackUserNumber(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req UserNumberRollbackRequest
	if err := bind(c, &req); err != nil || req.HistoryID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "historyId 不合法")
		return
	}
	_, operator := adminAccount(c)
	result, err := h.userNumber.Rollback(c.Request.Context(), appID, userID, req.HistoryID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已回滚", result)
}

// UserNumberRightAdjustRequest 调整号码权益：delta 为正发放、为负扣减。
type UserNumberRightAdjustRequest struct {
	Kind  string `json:"kind"`
	Level int    `json:"level"`
	Delta int64  `json:"delta"`
	Note  string `json:"note"`
}

// AdminAdjustUserNumberRight 调整一个用户的号码权益余额。
func (h *Handler) AdminAdjustUserNumberRight(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req UserNumberRightAdjustRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.userNumber.AdjustRight(c.Request.Context(), appID, userID, req.Kind, req.Level, req.Delta, req.Note, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已发放"
	if req.Delta < 0 {
		message = "已扣减"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AdminUserNumberHistory 换号记录（userId 可选）。
func (h *Handler) AdminUserNumberHistory(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, limit := parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20)
	items, total, err := h.userNumber.AdminHistory(c.Request.Context(), appID, userID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", UserNumberPage[usernumber.History]{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminUserNumberLedger 权益流水（userId、kind 可选）。
func (h *Handler) AdminUserNumberLedger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, limit := parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20)
	items, total, err := h.userNumber.AdminLedger(c.Request.Context(), appID, userID, c.Query("kind"), page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", UserNumberPage[usernumber.LedgerEntry]{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminUserNumberDraws 摇号单（userId 可选）。
func (h *Handler) AdminUserNumberDraws(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, limit := parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20)
	items, total, err := h.userNumber.AdminDraws(c.Request.Context(), appID, userID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", UserNumberPage[usernumber.Draw]{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminUserNumberHolds 占用列表（kind：reserved / frozen / candidate，可选）。
func (h *Handler) AdminUserNumberHolds(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	page, limit := parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20)
	items, total, err := h.userNumber.Holds(c.Request.Context(), appID, c.Query("kind"), page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", UserNumberPage[usernumber.Hold]{Items: items, Total: total, Page: page, Limit: limit})
}

// UserNumberReserveRequest 保留一批号码。
type UserNumberReserveRequest struct {
	Numbers []string `json:"numbers"`
	Note    string   `json:"note"`
	// ExpiresAt 到期自动放出（RFC 3339），留空长期保留
	ExpiresAt *time.Time `json:"expiresAt"`
}

// AdminReserveUserNumbers 把一批号码设为保留号（已有人在用的跳过）。
func (h *Handler) AdminReserveUserNumbers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	var req UserNumberReserveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.userNumber.Reserve(c.Request.Context(), appID, req.Numbers, req.Note, req.ExpiresAt, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保留", result)
}

// AdminReleaseUserNumberHold 删掉一条占用（保留号放出、冻结提前解除、候选号释放）。
func (h *Handler) AdminReleaseUserNumberHold(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.userNumberReady(c) {
		return
	}
	if err := h.userNumber.Release(c.Request.Context(), appID, c.Param("number")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已放出", nil)
}
