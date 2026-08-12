package httptransport

// 签到、积分、经验与等级。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	pointdomain "aegis/internal/domain/points"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/middleware"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) SignInStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.signin.GetStatus(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

func (h *Handler) SignInHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query PaginationQuery
	_ = bind(c, &query)
	result, err := h.signin.ListHistory(c.Request.Context(), session, normalizePage(query.Page), normalizeLimit(query.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ExportUserSignInHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query PaginationQuery
	_ = bind(c, &query)
	items, err := h.signin.ExportHistory(c.Request.Context(), session, userdomain.SignHistoryExportQuery{
		Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "user_signin_history_" + strconv.FormatInt(session.UserID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "signed_at", "sign_date", "integral_reward", "experience_reward", "integral_before", "integral_after", "experience_before", "experience_after", "consecutive_days", "reward_multiplier", "bonus_type", "bonus_description", "sign_in_source", "device_info", "ip_address", "location", "created_at"})
	for _, item := range items {
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.SignedAt.UTC().Format(time.RFC3339),
			item.SignDate,
			strconv.FormatInt(item.IntegralReward, 10),
			strconv.FormatInt(item.ExperienceReward, 10),
			strconv.FormatInt(item.IntegralBefore, 10),
			strconv.FormatInt(item.IntegralAfter, 10),
			strconv.FormatInt(item.ExperienceBefore, 10),
			strconv.FormatInt(item.ExperienceAfter, 10),
			strconv.Itoa(item.ConsecutiveDays),
			strconv.FormatFloat(item.RewardMultiplier, 'f', -1, 64),
			item.BonusType,
			item.BonusDescription,
			item.SignInSource,
			item.DeviceInfo,
			item.IPAddress,
			item.Location,
			item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func (h *Handler) SignIn(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SignInRequest
	_ = bind(c, &req)
	source := req.Source
	if source == "" {
		source = "manual"
	}
	location := strings.TrimSpace(req.Location)
	if location == "" {
		location = middleware.RequestLocationString(c)
	}
	result, err := h.signin.SignIn(c.Request.Context(), session, source, c.Request.UserAgent(), c.ClientIP(), location)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到成功", result)
}

func (h *Handler) PointsOverview(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	overview, err := h.points.GetOverview(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", overview)
}

func (h *Handler) PointsLevels(c *gin.Context) {
	levels, err := h.points.ListLevels(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", levels)
}

func (h *Handler) LegacyLevelConfig(c *gin.Context) {
	levels, err := h.points.ListLevels(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取等级配置成功", gin.H{
		"levels":     levels,
		"expRewards": []any{},
	})
}

func (h *Handler) MyLevel(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	level, err := h.points.GetMyLevel(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", level)
}

func (h *Handler) IntegralTransactions(c *gin.Context) {
	h.writeTransactions(c, func(session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error) {
		return h.points.ListIntegralTransactions(c.Request.Context(), session, page, limit)
	})
}

func (h *Handler) ExperienceTransactions(c *gin.Context) {
	h.writeTransactions(c, func(session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error) {
		return h.points.ListExperienceTransactions(c.Request.Context(), session, page, limit)
	})
}

func (h *Handler) PointsRankings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RankingQuery
	_ = bind(c, &query)
	rankings, err := h.points.GetRankings(c.Request.Context(), session, query.Type, normalizePage(query.Page), normalizeLimit(query.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", rankings)
}

func (h *Handler) LegacyMyLevel(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	_ = bind(c, &req)
	if req.AppID > 0 && req.AppID != session.AppID {
		response.Error(c, http.StatusForbidden, 40313, "应用不匹配")
		return
	}
	level, err := h.points.GetMyLevel(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取等级信息成功", gin.H{
		"levelInfo":  level.LevelInfo,
		"experience": level.UserInfo.Experience,
		"userInfo":   level.UserInfo,
	})
}

func (h *Handler) LegacyLevelRanking(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rankings, err := h.points.GetLegacyRanking(c.Request.Context(), session, req.AppID, "level", normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取等级排行榜成功", rankings)
}

func (h *Handler) LegacyDailyRank(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rankingType, err := h.points.ResolveLegacyDailyRankingType(req.Type)
	if err != nil {
		h.writeError(c, err)
		return
	}
	rankings, err := h.points.GetLegacyRanking(c.Request.Context(), session, req.AppID, rankingType, normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到排行榜成功", rankings)
}

func (h *Handler) LegacyIntegralRank(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rankings, err := h.points.GetLegacyRanking(c.Request.Context(), session, req.AppID, "integral", normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取积分排行榜成功", rankings)
}

func (h *Handler) LegacyDailySign(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SignInRequest
	_ = bind(c, &req)
	source := req.Source
	if source == "" {
		source = "manual"
	}
	location := strings.TrimSpace(req.Location)
	if location == "" {
		location = middleware.RequestLocationString(c)
	}
	result, err := h.signin.SignIn(c.Request.Context(), session, source, c.Request.UserAgent(), c.ClientIP(), location)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到成功", result)
}

func (h *Handler) AppPointsStats(c *gin.Context) {
	var req AppPointsStatsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.points.GetAppStatistics(c.Request.Context(), req.AppID, req.TimeRange)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取积分经验统计成功", stats)
}

func (h *Handler) AppAdjustIntegral(c *gin.Context) {
	var req AppAdjustIntegralRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminAccount := adminAccount(c)
	result, err := h.points.AdjustUserIntegral(c.Request.Context(), req.UserID, req.AppID, req.Amount, req.Reason, pointdomain.AdminAdjustOptions{
		AdminID:      adminID,
		AdminAccount: adminAccount,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户积分调整成功", result)
}

func (h *Handler) AppAdjustExperience(c *gin.Context) {
	var req AppAdjustExperienceRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminAccount := adminAccount(c)
	result, err := h.points.AdjustUserExperience(c.Request.Context(), req.UserID, req.AppID, req.Amount, req.Reason, pointdomain.AdminAdjustOptions{
		AdminID:      adminID,
		AdminAccount: adminAccount,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户经验值调整成功", result)
}

func (h *Handler) AppBatchAdjustIntegral(c *gin.Context) {
	var req AppBatchAdjustIntegralRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminAccount := adminAccount(c)
	result, err := h.points.BatchAdjustUserIntegral(c.Request.Context(), req.UserIDs, req.AppID, req.Amount, req.OperationType, req.Reason, pointdomain.AdminAdjustOptions{
		AdminID:      adminID,
		AdminAccount: adminAccount,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量调整用户积分成功", result)
}
