package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	pointdomain "aegis/internal/domain/points"
	"aegis/pkg/response"
)

// AdminPointLedgerQuery 用户积分 / 经验流水筛选。from / to 为 RFC 3339 时间（控制台按浏览器时区换算好再传）。
type AdminPointLedgerQuery struct {
	Direction string `form:"direction"`
	Category  string `form:"category"`
	Keyword   string `form:"keyword"`
	From      string `form:"from"`
	To        string `form:"to"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

// AdminPointSummaryQuery 汇总的统计窗口（天）与切日时区。
type AdminPointSummaryQuery struct {
	Days int    `form:"days"`
	TZ   string `form:"tz"`
}

// AdminPointReverseRequest 冲正原因，留空时写「冲正 <流水号>」。
type AdminPointReverseRequest struct {
	Reason string `json:"reason"`
	// LevelMode 冲正经验时扣回的那部分怎么处理等级：keep（默认）/ recalc
	LevelMode string `json:"levelMode"`
}

func pathUserID(c *gin.Context) (int64, bool) {
	userID, err := strconv.ParseInt(c.Param("userId"), 10, 64)
	if err != nil || userID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "用户ID无效")
		return 0, false
	}
	return userID, true
}

// AdminAppUserPointLedger GET /api/admin/apps/:appkey/users/:userId/points/:kind/transactions
func (h *Handler) AdminAppUserPointLedger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := pathUserID(c)
	if !ok {
		return
	}
	var query AdminPointLedgerQuery
	_ = c.ShouldBindQuery(&query)
	from, err := parseOptionalRFC3339(query.From)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "from 格式无效，要求 RFC 3339")
		return
	}
	to, err := parseOptionalRFC3339(query.To)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "to 格式无效，要求 RFC 3339")
		return
	}
	result, err := h.points.AdminListLedger(c.Request.Context(), c.Param("kind"), pointdomain.LedgerQuery{
		AppID: appID, UserID: userID,
		Direction: strings.TrimSpace(query.Direction), Category: query.Category, Keyword: query.Keyword,
		From: from, To: to, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取流水成功", result)
}

// AdminAppUserPointSummary GET /api/admin/apps/:appkey/users/:userId/points/:kind/summary
func (h *Handler) AdminAppUserPointSummary(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := pathUserID(c)
	if !ok {
		return
	}
	var query AdminPointSummaryQuery
	_ = c.ShouldBindQuery(&query)
	if query.Days == 0 {
		query.Days = 30
	}
	result, err := h.points.AdminLedgerSummary(c.Request.Context(), c.Param("kind"), appID, userID, query.Days, query.TZ)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取流水汇总成功", result)
}

// AdminReverseAppUserIntegral POST /api/admin/apps/:appkey/users/:userId/points/integral/transactions/:transactionNo/reverse
func (h *Handler) AdminReverseAppUserIntegral(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := pathUserID(c)
	if !ok {
		return
	}
	var req AdminPointReverseRequest
	_ = c.ShouldBindJSON(&req)
	result, err := h.points.AdminReverseIntegral(c.Request.Context(), appID, userID, c.Param("transactionNo"), req.Reason, h.pointsAdjustOptions(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "冲正成功", result)
}

// AdminReverseAppUserExperience POST /api/admin/apps/:appkey/users/:userId/points/experience/transactions/:transactionNo/reverse
func (h *Handler) AdminReverseAppUserExperience(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := pathUserID(c)
	if !ok {
		return
	}
	var req AdminPointReverseRequest
	_ = c.ShouldBindJSON(&req)
	result, err := h.points.AdminReverseExperience(c.Request.Context(), appID, userID, c.Param("transactionNo"), req.Reason, req.LevelMode, h.pointsAdjustOptions(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "冲正成功", result)
}
