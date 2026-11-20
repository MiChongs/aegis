package httptransport

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/leaderboard"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 积分排行榜（新版，docs/leaderboard.md）：用户端（榜单 / 往期 / 奖励记录 / 匿名偏好）与管理端
// （设置 / 实时与往期榜 / 移出名单 / 结算记录与明细 / 重试 / 手动结算 / 概览）。
// 旧版 /leaderboard/summary|me|points|signin 在 leaderboard_handlers.go。

// ── DTO ──

// LeaderboardPageQuery 分页参数（用户端每页最多 50 行，只在前 topLimit 名内翻）。
type LeaderboardPageQuery struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

// LeaderboardPeriodsQuery 往期列表的参数（默认 12 期，最多 50 期）。
type LeaderboardPeriodsQuery struct {
	Limit int `form:"limit"`
}

// LeaderboardPreferenceRequest 匿名上榜开关。
type LeaderboardPreferenceRequest struct {
	Anonymous *bool `json:"anonymous" binding:"required"`
}

// LeaderboardBoardSettingRequest 一个榜单的开关；数组顺序即客户端标签顺序。
type LeaderboardBoardSettingRequest struct {
	Key     string `json:"key" binding:"required"`
	Enabled bool   `json:"enabled"`
}

// LeaderboardTierRequest 一档奖励：第 rankFrom 到第 rankTo 名（含两端）各发一份 rewards（形状同邀请）。
type LeaderboardTierRequest struct {
	RankFrom int                   `json:"rankFrom"`
	RankTo   int                   `json:"rankTo"`
	Rewards  []InviteRewardRequest `json:"rewards"`
}

// LeaderboardBoardRewardsRequest 一个周期榜的奖励配置。
type LeaderboardBoardRewardsRequest struct {
	Enabled bool                     `json:"enabled"`
	Tiers   []LeaderboardTierRequest `json:"tiers"`
}

// LeaderboardSettingsRequest 排行榜的开关与配置。字段含义见 docs/leaderboard.md「设置」。
// minValue 不传为 1；allowAnonymous 不传为 true。
type LeaderboardSettingsRequest struct {
	Enabled          bool                                      `json:"enabled"`
	Boards           []LeaderboardBoardSettingRequest          `json:"boards"`
	TopLimit         int                                       `json:"topLimit"`
	MinValue         *int64                                    `json:"minValue"`
	Identity         string                                    `json:"identity"`
	AllowAnonymous   *bool                                     `json:"allowAnonymous"`
	CountAdminAdjust bool                                      `json:"countAdminAdjust"`
	Rewards          map[string]LeaderboardBoardRewardsRequest `json:"rewards"`
}

// LeaderboardAdminBoardQuery 管理端看榜：periodKey 为空看当前周期。
type LeaderboardAdminBoardQuery struct {
	PeriodKey string `form:"periodKey"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

// LeaderboardExclusionQuery 移出名单的查询参数（keyword 匹配账号、昵称或用户 ID）。
type LeaderboardExclusionQuery struct {
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// LeaderboardExclusionRequest 移出一批人（最多 100 人）。
type LeaderboardExclusionRequest struct {
	UserIDs []int64 `json:"userIds" binding:"required"`
	Reason  string  `json:"reason"`
}

// LeaderboardSnapshotQuery 结算记录的查询参数（board 为空为全部榜）。
type LeaderboardSnapshotQuery struct {
	Board string `form:"board"`
	Page  int    `form:"page"`
	Limit int    `form:"limit"`
}

// LeaderboardEntryQuery 结算明细的查询参数（status：pending / granted / failed / skipped / none）。
type LeaderboardEntryQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// LeaderboardSettleRequest 手动结算一个已结束的周期。
type LeaderboardSettleRequest struct {
	Board     string `json:"board" binding:"required"`
	PeriodKey string `json:"periodKey" binding:"required"`
}

func (req LeaderboardSettingsRequest) toConfig() leaderboard.Config {
	cfg := leaderboard.DefaultConfig()
	cfg.Boards = make([]leaderboard.BoardSetting, 0, len(req.Boards))
	for _, board := range req.Boards {
		cfg.Boards = append(cfg.Boards, leaderboard.BoardSetting{Key: board.Key, Enabled: board.Enabled})
	}
	cfg.TopLimit = req.TopLimit
	if req.MinValue != nil {
		cfg.MinValue = *req.MinValue
	}
	cfg.Identity = req.Identity
	if req.AllowAnonymous != nil {
		cfg.AllowAnonymous = *req.AllowAnonymous
	}
	cfg.CountAdminAdjust = req.CountAdminAdjust
	cfg.Rewards = make(map[string]leaderboard.BoardRewards, len(req.Rewards))
	for key, board := range req.Rewards {
		tiers := make([]leaderboard.Tier, 0, len(board.Tiers))
		for _, tier := range board.Tiers {
			tiers = append(tiers, leaderboard.Tier{RankFrom: tier.RankFrom, RankTo: tier.RankTo, Rewards: inviteRewards(tier.Rewards)})
		}
		cfg.Rewards[key] = leaderboard.BoardRewards{Enabled: board.Enabled, Tiers: tiers}
	}
	return cfg
}

// ── 头像：把原值翻成永久地址（不带账号做 gravatar 兜底：排行榜对所有人可见） ──

func (h *Handler) leaderboardAvatar(c *gin.Context, appID, userID int64, raw string) string {
	if h.avatar == nil || userID <= 0 {
		return ""
	}
	return h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request), appID, userID, raw)
}

func (h *Handler) resolveLeaderboardRows(c *gin.Context, appID int64, rows []leaderboard.ClientRow) {
	owners := make([]int64, 0, len(rows))
	for i := range rows {
		rows[i].Avatar = h.leaderboardAvatar(c, appID, rows[i].AvatarOwner, rows[i].RawAvatar)
		if rows[i].AvatarOwner > 0 {
			owners = append(owners, rows[i].AvatarOwner)
		}
	}
	// 装扮跟头像走：匿名上榜的人 AvatarOwner 为 0，既不出头像也不出装扮（装扮能认出人）
	if h.cosmetic == nil || len(owners) == 0 {
		return
	}
	loadouts := h.cosmetic.LookupByUserIDs(c.Request.Context(), appID, owners, requestBaseURL(c.Request))
	for i := range rows {
		if loadout, ok := loadouts[rows[i].AvatarOwner]; ok && rows[i].AvatarOwner > 0 {
			rows[i].Cosmetics = &loadout
		}
	}
}

func (h *Handler) resolveLeaderboardAdminEntries(c *gin.Context, appID int64, rows []leaderboard.AdminEntry) {
	for i := range rows {
		rows[i].Avatar = h.leaderboardAvatar(c, appID, rows[i].UserID, rows[i].RawAvatar)
	}
}

// ── 用户端 ──

// AppLeaderboardBoards 排行榜首页：开启的榜单（周期、奖励档位）与我的偏好；未开启时 enabled=false。
func (h *Handler) AppLeaderboardBoards(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	home, err := h.leaderboard.Boards(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", home)
}

// AppLeaderboardBoard 一个榜单的实时名次（前 topLimit 名内分页）与我的名次。
func (h *Handler) AppLeaderboardBoard(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query LeaderboardPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.leaderboard.Board(c.Request.Context(), session.AppID, session.UserID, c.Param("key"), query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolveLeaderboardRows(c, session.AppID, page.Items)
	page.Me.Avatar = h.leaderboardAvatar(c, session.AppID, page.Me.AvatarOwner, page.Me.RawAvatar)
	response.Success(c, http.StatusOK, "ok", page)
}

// AppLeaderboardPeriods 周期榜的往期列表。
func (h *Handler) AppLeaderboardPeriods(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query LeaderboardPeriodsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	periods, err := h.leaderboard.Periods(c.Request.Context(), session.AppID, session.UserID, c.Param("key"), query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", periods)
}

// AppLeaderboardPeriod 往期榜单：结算时冻结的名次、我在这一期的名次与奖励。
func (h *Handler) AppLeaderboardPeriod(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query LeaderboardPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.leaderboard.Period(c.Request.Context(), session.AppID, session.UserID, c.Param("key"), c.Param("periodKey"),
		query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolveLeaderboardRows(c, session.AppID, page.Items)
	response.Success(c, http.StatusOK, "ok", page)
}

// AppLeaderboardRewards 我的榜单奖励记录。
func (h *Handler) AppLeaderboardRewards(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query LeaderboardPageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	records, err := h.leaderboard.MyRewards(c.Request.Context(), session.AppID, session.UserID, query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", records)
}

// AppLeaderboardPreference 我的排行榜偏好。
func (h *Handler) AppLeaderboardPreference(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	preference, err := h.leaderboard.Preference(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", preference)
}

// AppUpdateLeaderboardPreference 开关匿名上榜。
func (h *Handler) AppUpdateLeaderboardPreference(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req LeaderboardPreferenceRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	preference, err := h.leaderboard.UpdatePreference(c.Request.Context(), session.AppID, session.UserID, *req.Anonymous)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已关闭匿名上榜"
	if preference.Anonymous {
		message = "已开启匿名上榜"
	}
	response.Success(c, http.StatusOK, message, preference)
}

// ── 管理端 ──

// AdminLeaderboardSettings 设置，连同榜单目录（catalog）与权益目录（rewardCatalog），控制台表单由它驱动。
func (h *Handler) AdminLeaderboardSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	data, err := h.leaderboard.AdminSettings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", data)
}

// AdminSaveLeaderboardSettings 保存开关与配置（校验档位与奖励引用）。
func (h *Handler) AdminSaveLeaderboardSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LeaderboardSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	settings, err := h.leaderboard.SaveSettings(c.Request.Context(), appID, req.Enabled, req.toConfig(), operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "排行榜设置已保存", settings)
}

// AdminLeaderboardBoard 实时榜或往期榜：带真实账号与用户 ID，标出匿名、被移出。
func (h *Handler) AdminLeaderboardBoard(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query LeaderboardAdminBoardQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.leaderboard.AdminBoard(c.Request.Context(), appID, c.Param("key"), query.PeriodKey, query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolveLeaderboardAdminEntries(c, appID, page.Items)
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminLeaderboardExclusions 移出名单。
func (h *Handler) AdminLeaderboardExclusions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query LeaderboardExclusionQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.leaderboard.ListExclusions(c.Request.Context(), appID, query.Keyword, query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminAddLeaderboardExclusions 移出一批人。
func (h *Handler) AdminAddLeaderboardExclusions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LeaderboardExclusionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.leaderboard.AddExclusions(c.Request.Context(), appID, req.UserIDs, req.Reason, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已移出排行榜", result)
}

// AdminRemoveLeaderboardExclusion 把一个人移回排行榜。
func (h *Handler) AdminRemoveLeaderboardExclusion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	if err := h.leaderboard.RemoveExclusion(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已移回排行榜", gin.H{"removed": true})
}

// AdminLeaderboardSnapshots 结算记录（期号、参与人数、发放统计）。
func (h *Handler) AdminLeaderboardSnapshots(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query LeaderboardSnapshotQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.leaderboard.ListSnapshots(c.Request.Context(), appID, query.Board, query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminLeaderboardSnapshotEntries 结算明细（带真实账号、奖励状态、失败原因）。
func (h *Handler) AdminLeaderboardSnapshotEntries(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	snapshotID, ok := cardKeyPathID(c, "snapshotId")
	if !ok {
		return
	}
	var query LeaderboardEntryQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, err := h.leaderboard.SnapshotEntries(c.Request.Context(), appID, snapshotID, query.Status, query.Page, query.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if entries, ok := data["entries"].(*leaderboard.Page[leaderboard.SnapshotEntry]); ok {
		for i := range entries.Items {
			entry := &entries.Items[i]
			entry.Avatar = h.leaderboardAvatar(c, appID, entry.UserID, entry.RawAvatar)
		}
	}
	response.Success(c, http.StatusOK, "ok", data)
}

// AdminRetryLeaderboardSnapshot 重试该期全部发放失败的条目（置回待发放，由结算循环发放）。
func (h *Handler) AdminRetryLeaderboardSnapshot(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	snapshotID, ok := cardKeyPathID(c, "snapshotId")
	if !ok {
		return
	}
	count, err := h.leaderboard.RetrySnapshot(c.Request.Context(), appID, snapshotID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已重新排队 "+strconv.FormatInt(count, 10)+" 份奖励", gin.H{"retried": count})
}

// AdminSettleLeaderboard 手动结算一个已结束的周期（已有快照直接返回它）。
func (h *Handler) AdminSettleLeaderboard(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LeaderboardSettleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	if operator == "" {
		operator = "admin"
	}
	snapshot, created, err := h.leaderboard.Settle(c.Request.Context(), appID, req.Board, req.PeriodKey, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已结算"
	if !created {
		message = "这一期已结算过"
	}
	response.Success(c, http.StatusOK, message, gin.H{"snapshot": snapshot, "created": created})
}

// AdminLeaderboardStats 概览：各榜参与人数、本期前 3、最近一次结算时间、移出人数、待发与失败的奖励数。
func (h *Handler) AdminLeaderboardStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.leaderboard.Stats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if boards, ok := stats["boards"].([]service.LeaderboardBoardStats); ok {
		for i := range boards {
			h.resolveLeaderboardAdminEntries(c, appID, boards[i].Top)
		}
	}
	response.Success(c, http.StatusOK, "ok", stats)
}
