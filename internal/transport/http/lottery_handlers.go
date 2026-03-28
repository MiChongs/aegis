package httptransport

import (
	"net/http"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════
//  管理员 — 活动 CRUD
// ════════════════════════════════════════════════════════════

// AdminCreateLotteryActivity 创建抽奖活动
// POST /api/admin/apps/:appkey/lottery/activities
func (h *Handler) AdminCreateLotteryActivity(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LotteryActivityCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	activity := lotterydomain.Activity{
		Name:          req.Name,
		Description:   req.Description,
		UIMode:        req.UIMode,
		Status:        req.Status,
		JoinMode:      req.JoinMode,
		AutoJoinRules: req.AutoJoinRules,
		CostType:      req.CostType,
		CostAmount:    req.CostAmount,
		DailyLimit:    req.DailyLimit,
		TotalLimit:    req.TotalLimit,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
	}
	result, err := h.lottery.CreateActivity(c.Request.Context(), appID, activity)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", result)
}

// AdminUpdateLotteryActivity 更新抽奖活动
// PUT /api/admin/apps/:appkey/lottery/activities/:id
func (h *Handler) AdminUpdateLotteryActivity(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	var req LotteryActivityUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	mutation := lotterydomain.ActivityMutation{
		Name:          req.Name,
		Description:   req.Description,
		UIMode:        req.UIMode,
		Status:        req.Status,
		JoinMode:      req.JoinMode,
		AutoJoinRules: req.AutoJoinRules,
		CostType:      req.CostType,
		CostAmount:    req.CostAmount,
		DailyLimit:    req.DailyLimit,
		TotalLimit:    req.TotalLimit,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
	}
	result, err := h.lottery.UpdateActivity(c.Request.Context(), activityID, mutation)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", result)
}

// AdminGetLotteryActivity 获取抽奖活动详情
// GET /api/admin/apps/:appkey/lottery/activities/:id
func (h *Handler) AdminGetLotteryActivity(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	result, err := h.lottery.GetActivity(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminListLotteryActivities 分页查询抽奖活动列表
// GET /api/admin/apps/:appkey/lottery/activities
func (h *Handler) AdminListLotteryActivities(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query LotteryActivityListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.lottery.ListActivities(c.Request.Context(), lotterydomain.ActivityListQuery{
		AppID:   appID,
		Status:  query.Status,
		Keyword: query.Keyword,
		Page:    normalizePage(query.Page),
		Limit:   normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminDeleteLotteryActivity 删除抽奖活动
// DELETE /api/admin/apps/:appkey/lottery/activities/:id
func (h *Handler) AdminDeleteLotteryActivity(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	if err := h.lottery.DeleteActivity(c.Request.Context(), activityID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

// ════════════════════════════════════════════════════════════
//  管理员 — 奖品 CRUD
// ════════════════════════════════════════════════════════════

// AdminCreateLotteryPrize 创建奖品
// POST /api/admin/apps/:appkey/lottery/activities/:id/prizes
func (h *Handler) AdminCreateLotteryPrize(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	var req LotteryPrizeCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	prize := lotterydomain.Prize{
		Name:      req.Name,
		Type:      req.Type,
		Value:     req.Value,
		ImageURL:  req.ImageURL,
		Quantity:  req.Quantity,
		Weight:    req.Weight,
		Position:  req.Position,
		IsDefault: req.IsDefault,
		Extra:     req.Extra,
	}
	result, err := h.lottery.CreatePrize(c.Request.Context(), activityID, prize)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", result)
}

// AdminUpdateLotteryPrize 更新奖品
// PUT /api/admin/apps/:appkey/lottery/prizes/:id
func (h *Handler) AdminUpdateLotteryPrize(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	prizeID, err := pathInt64(c, "id")
	if err != nil || prizeID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的奖品 ID")
		return
	}
	var req LotteryPrizeUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	mutation := lotterydomain.PrizeMutation{
		Name:      req.Name,
		Type:      req.Type,
		Value:     req.Value,
		ImageURL:  req.ImageURL,
		Quantity:  req.Quantity,
		Weight:    req.Weight,
		Position:  req.Position,
		IsDefault: req.IsDefault,
		Extra:     req.Extra,
	}
	result, err := h.lottery.UpdatePrize(c.Request.Context(), prizeID, mutation)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", result)
}

// AdminDeleteLotteryPrize 删除奖品
// DELETE /api/admin/apps/:appkey/lottery/prizes/:id
func (h *Handler) AdminDeleteLotteryPrize(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	prizeID, err := pathInt64(c, "id")
	if err != nil || prizeID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的奖品 ID")
		return
	}
	if err := h.lottery.DeletePrize(c.Request.Context(), prizeID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

// AdminListLotteryPrizes 查询活动奖品列表
// GET /api/admin/apps/:appkey/lottery/activities/:id/prizes
func (h *Handler) AdminListLotteryPrizes(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	prizes, err := h.lottery.ListPrizes(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", prizes)
}

// ════════════════════════════════════════════════════════════
//  管理员 — 抽奖记录 & 统计 & 种子
// ════════════════════════════════════════════════════════════

// AdminListLotteryDraws 查询抽奖记录
// GET /api/admin/apps/:appkey/lottery/draws
func (h *Handler) AdminListLotteryDraws(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	var query LotteryDrawListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.lottery.ListDraws(c.Request.Context(), lotterydomain.DrawListQuery{
		ActivityID: query.ActivityID,
		UserID:     query.UserID,
		Status:     query.Status,
		Page:       normalizePage(query.Page),
		Limit:      normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminLotteryActivityStats 获取活动统计
// GET /api/admin/apps/:appkey/lottery/activities/:id/stats
func (h *Handler) AdminLotteryActivityStats(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	stats, err := h.lottery.GetStats(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

// AdminLotteryCommitSeed 创建种子承诺
// POST /api/admin/apps/:appkey/lottery/activities/:id/seed/commit
func (h *Handler) AdminLotteryCommitSeed(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	commitment, err := h.lottery.CommitSeed(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "种子承诺已创建", commitment)
}

// AdminLotteryRevealSeed 公开种子
// POST /api/admin/apps/:appkey/lottery/activities/:id/seed/reveal
func (h *Handler) AdminLotteryRevealSeed(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	commitment, err := h.lottery.RevealSeed(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "种子已公开", commitment)
}

// ════════════════════════════════════════════════════════════
//  用户端 — 抽奖 / 参与 / 查询 / 验证
// ════════════════════════════════════════════════════════════

// UserLotteryActivities 用户查看活动列表
// GET /api/lottery/activities
func (h *Handler) UserLotteryActivities(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query LotteryActivityListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.lottery.ListActivities(c.Request.Context(), lotterydomain.ActivityListQuery{
		AppID:   session.AppID,
		Status:  "active",
		Keyword: query.Keyword,
		Page:    normalizePage(query.Page),
		Limit:   normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// UserLotteryActivityDetail 用户查看活动详情
// GET /api/lottery/activities/:id
func (h *Handler) UserLotteryActivityDetail(c *gin.Context) {
	_, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	result, err := h.lottery.GetActivity(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// UserLotteryActivityPrizes 用户查看活动奖品
// GET /api/lottery/activities/:id/prizes
func (h *Handler) UserLotteryActivityPrizes(c *gin.Context) {
	_, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	prizes, err := h.lottery.ListPrizes(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", prizes)
}

// UserLotteryJoin 用户加入活动
// POST /api/lottery/join
func (h *Handler) UserLotteryJoin(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LotteryJoinRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.lottery.JoinActivity(c.Request.Context(), req.ActivityID, session.UserID, "manual"); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "报名成功", nil)
}

// UserLotteryDraw 用户执行抽奖
// POST /api/lottery/draw
func (h *Handler) UserLotteryDraw(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LotteryDrawRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.lottery.Draw(c.Request.Context(), session.UserID, session.AppID, req.ActivityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "抽奖成功", result)
}

// UserLotteryDrawHistory 用户查看自己的抽奖记录
// GET /api/lottery/draws
func (h *Handler) UserLotteryDrawHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query LotteryMyDrawListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.lottery.ListDraws(c.Request.Context(), lotterydomain.DrawListQuery{
		ActivityID: query.ActivityID,
		UserID:     session.UserID,
		Status:     query.Status,
		Page:       normalizePage(query.Page),
		Limit:      normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// UserLotteryVerify 用户验证活动公正性
// GET /api/lottery/activities/:id/verify
func (h *Handler) UserLotteryVerify(c *gin.Context) {
	_, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	activityID, err := pathInt64(c, "id")
	if err != nil || activityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的活动 ID")
		return
	}
	result, err := h.lottery.VerifyActivity(c.Request.Context(), activityID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证完成", result)
}
