package httptransport

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	authdomain "aegis/internal/domain/auth"
	pointdomain "aegis/internal/domain/points"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 用户侧排行榜
//   - 支持积分 / 经验 / 等级 / 今日签到 / 连续签到 / 月度签到 共 6 类
//   - service 层已接入 Redis 缓存 + singleflight，下面只做聚合 / 参数兜底 / HTTP 缓存头
//   - 独立路由组可挂载专用限流器（见 router.go）
//   - 响应添加 Cache-Control: private, max-age=20，让浏览器/CDN 复用 20s
//   - 并发聚合 summary 时使用 errgroup 样式的 goroutine

const (
	// 排行榜最大分页大小，防止大查询
	leaderboardMaxLimit = 100
	// summary 每个榜单 Top 数
	leaderboardTopN = 10
	// HTTP 缓存头（浏览器/CDN 短期缓存）
	leaderboardCacheControl = "private, max-age=20"
)

var leaderboardPointsTypes = map[string]string{
	"integral":   "integral",
	"experience": "experience",
	"level":      "level",
}

var leaderboardSignTypes = map[string]string{
	"today":       "sign_today",
	"consecutive": "sign_consecutive",
	"streak":      "sign_consecutive",
	"monthly":     "sign_monthly",
	"month":       "sign_monthly",
}

func (h *Handler) LeaderboardPoints(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	alias := strings.ToLower(strings.TrimSpace(c.Param("type")))
	rankingType, valid := leaderboardPointsTypes[alias]
	if !valid {
		response.Error(c, http.StatusBadRequest, 40009, "排行榜类型无效，可选：integral / experience / level")
		return
	}
	page, limit := leaderboardNormalize(c)
	result, err := h.points.GetRankings(c.Request.Context(), session, rankingType, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", leaderboardCacheControl)
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) LeaderboardSignIn(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	alias := strings.ToLower(strings.TrimSpace(c.Param("type")))
	rankingType, valid := leaderboardSignTypes[alias]
	if !valid {
		response.Error(c, http.StatusBadRequest, 40009, "签到排行类型无效，可选：today / consecutive / monthly")
		return
	}
	page, limit := leaderboardNormalize(c)
	result, err := h.points.GetRankings(c.Request.Context(), session, rankingType, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", leaderboardCacheControl)
	response.Success(c, 200, "获取成功", result)
}

// LeaderboardSummary 首屏综合概览：并发拉取 6 类榜单 Top N，失败的榜单降级为 nil
func (h *Handler) LeaderboardSummary(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}

	types := []string{"integral", "experience", "level", "sign_today", "sign_consecutive", "sign_monthly"}
	results := make(map[string]*pointdomain.RankingResponse, len(types))
	var mu sync.Mutex
	var wg sync.WaitGroup

	ctx := c.Request.Context()
	for _, t := range types {
		wg.Add(1)
		go func(rankingType string) {
			defer wg.Done()
			resp, err := h.points.GetRankings(ctx, session, rankingType, 1, leaderboardTopN)
			if err != nil {
				// 某个榜单失败不影响其他榜单，仅记录
				return
			}
			mu.Lock()
			results[rankingType] = resp
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	summary := LeaderboardSummary{
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
	if r := results["integral"]; r != nil {
		summary.Integral = toLeaderboardSection(r)
	}
	if r := results["experience"]; r != nil {
		summary.Experience = toLeaderboardSection(r)
	}
	if r := results["level"]; r != nil {
		summary.Level = toLeaderboardSection(r)
	}
	if r := results["sign_today"]; r != nil {
		summary.SignToday = toLeaderboardSection(r)
	}
	if r := results["sign_consecutive"]; r != nil {
		summary.SignStreak = toLeaderboardSection(r)
	}
	if r := results["sign_monthly"]; r != nil {
		summary.SignMonthly = toLeaderboardSection(r)
	}

	c.Header("Cache-Control", leaderboardCacheControl)
	response.Success(c, 200, "获取成功", summary)
}

// LeaderboardMe 返回当前用户在所有榜单中的排名，供用户"我的排名"页面使用
func (h *Handler) LeaderboardMe(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	types := []string{"integral", "experience", "level", "sign_today", "sign_consecutive", "sign_monthly"}
	my := make(map[string]*pointdomain.RankingItem, len(types))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, t := range types {
		wg.Add(1)
		go func(rankingType string) {
			defer wg.Done()
			// 复用 GetRankings，limit=1，只取我的排名
			resp, err := h.points.GetRankings(c.Request.Context(), session, rankingType, 1, 1)
			if err != nil || resp == nil {
				return
			}
			mu.Lock()
			my[rankingType] = resp.MyRank
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	c.Header("Cache-Control", "private, max-age=10")
	response.Success(c, 200, "获取成功", my)
}

// ── 辅助 ────────────────────────────────────

func leaderboardNormalize(c *gin.Context) (int, int) {
	var q LeaderboardListQuery
	_ = bind(c, &q)
	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit < 1 {
		limit = 20
	}
	if limit > leaderboardMaxLimit {
		limit = leaderboardMaxLimit
	}
	return page, limit
}

func toLeaderboardSection(resp *pointdomain.RankingResponse) *LeaderboardTopSection {
	if resp == nil {
		return nil
	}
	return &LeaderboardTopSection{
		Type:   resp.Type,
		Total:  resp.Total,
		Items:  resp.Items,
		MyRank: resp.MyRank,
	}
}

// 为了不在 router.go 里直接引用 session 字段，暴露一个简化取 session 的 helper（已在 router.go 有 authSession，但排行榜聚合可能需要做校验）
// 目前不做额外校验，仅保留扩展点
var _ = fmt.Sprintf // 保留 fmt 引用，便于后续扩展
var _ = authdomain.Session{}
