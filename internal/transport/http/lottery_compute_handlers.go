package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 抽奖计算引擎的管理端接口：精确分析、奖盘求解、大规模推演任务。规则见 docs/lottery.md「计算引擎」。
// 三者都只算不写（推演任务只写它自己的任务表），不发奖、不扣次数。

// ── DTO ──

// LotteryPersonaRequest 一种人群：身份由几个开关给出（与策略模拟里虚拟用户的特权身份同一套）。
// 永久会员按「已拥有奖盘里全部会员与功能奖品」算。
type LotteryPersonaRequest struct {
	// Key 人群的标识（结果里原样带回）；Name 显示名，留空按身份取
	Key  string `json:"key"`
	Name string `json:"name"`
	// Share 占比（推演用，任意正数，会归一）
	Share float64 `json:"share"`
	// DrawsPerDay 这类人每天抽几次（推演用）；0 为跟随场景
	DrawsPerDay        int     `json:"drawsPerDay"`
	Member             bool    `json:"member"`
	Lifetime           bool    `json:"lifetime"`
	AdFree             bool    `json:"adFree"`
	SignedToday        bool    `json:"signedToday"`
	OnlineMinutesToday float64 `json:"onlineMinutesToday"`
	// Tiers 所在的会员档位（至尊加成按档位分级时用）
	Tiers []int64 `json:"tiers"`
}

func (r LotteryPersonaRequest) toSpec() service.LotteryPersonaSpec {
	return service.LotteryPersonaSpec{
		Key: r.Key, Name: r.Name, Share: r.Share, DrawsPerDay: r.DrawsPerDay, Member: r.Member, Lifetime: r.Lifetime,
		AdFree: r.AdFree, SignedToday: r.SignedToday, OnlineMinutesToday: r.OnlineMinutesToday, Tiers: r.Tiers,
	}
}

func personaSpecs(items []LotteryPersonaRequest) []service.LotteryPersonaSpec {
	out := make([]service.LotteryPersonaSpec, 0, len(items))
	for _, item := range items {
		out = append(out, item.toSpec())
	}
	return out
}

// LotteryAnalyzeRequest 精确分析：每种人群的综合概率（含保底）、每抽期望价值、各奖品多少抽能出。
// activity 是控制台里还没保存的编辑内容（不传用 activityId 对应的已保存活动）；personas 不传时只算普通用户。
type LotteryAnalyzeRequest struct {
	ActivityID int64                   `json:"activityId"`
	Activity   *LotteryActivityRequest `json:"activity"`
	Personas   []LotteryPersonaRequest `json:"personas"`
}

// LotterySolveRequest 奖盘求解：按目标解出新的权重（不保存）。目标提在 persona 这类人眼里
// （不传为普通用户），fortune 为按哪一天的运气算：average（默认）/ high / low。
type LotterySolveRequest struct {
	ActivityID int64                   `json:"activityId"`
	Activity   *LotteryActivityRequest `json:"activity"`
	Persona    *LotteryPersonaRequest  `json:"persona"`
	Fortune    string                  `json:"fortune"`
	Goal       lotterydomain.SolveGoal `json:"goal"`
}

// LotteryComputeRequest 发起一次大规模推演：users 个人（按 personas 的占比分成几种身份）连续抽 days 天、
// 每人每天 drawsPerDay 次，独立演 replications 遍。seed 是十进制字符串（64 位整数超出 JSON 数字的安全范围），
// 不传时随机取一个。
type LotteryComputeRequest struct {
	ActivityID   int64                   `json:"activityId"`
	Activity     *LotteryActivityRequest `json:"activity"`
	Name         string                  `json:"name"`
	Users        int                     `json:"users"`
	Days         int                     `json:"days"`
	DrawsPerDay  int                     `json:"drawsPerDay"`
	Replications int                     `json:"replications"`
	Seed         string                  `json:"seed"`
	Source       string                  `json:"source"`
	Personas     []LotteryPersonaRequest `json:"personas"`
}

// ── 处理器 ──

// AdminAnalyzeLottery POST /api/admin/apps/:appkey/lottery/analyze
func (h *Handler) AdminAnalyzeLottery(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LotteryAnalyzeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	analysis := service.LotteryAnalysisRequest{AppID: appID, ActivityID: req.ActivityID, Personas: personaSpecs(req.Personas)}
	if req.Activity != nil {
		_, operator := adminAccount(c)
		input := req.Activity.toInput(appID, operator)
		analysis.Input = &input
	}
	result, err := h.lottery.Analyze(c.Request.Context(), analysis)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminSolveLottery POST /api/admin/apps/:appkey/lottery/solve
func (h *Handler) AdminSolveLottery(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LotterySolveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	solve := service.LotterySolveRequest{AppID: appID, ActivityID: req.ActivityID, Fortune: req.Fortune, Goal: req.Goal}
	if req.Activity != nil {
		_, operator := adminAccount(c)
		input := req.Activity.toInput(appID, operator)
		solve.Input = &input
	}
	if req.Persona != nil {
		spec := req.Persona.toSpec()
		solve.Persona = &spec
	}
	result, err := h.lottery.Solve(c.Request.Context(), solve)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminCreateLotteryComputeJob POST /api/admin/apps/:appkey/lottery/compute/jobs
func (h *Handler) AdminCreateLotteryComputeJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req LotteryComputeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var seed uint64
	if text := strings.TrimSpace(req.Seed); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "种子需要是非负整数")
			return
		}
		seed = parsed
	}
	_, operator := adminAccount(c)
	compute := service.LotteryComputeRequest{
		AppID: appID, ActivityID: req.ActivityID, Operator: operator, Name: req.Name,
		Users: req.Users, Days: req.Days, DrawsPerDay: req.DrawsPerDay, Replications: req.Replications,
		Seed: seed, Source: strings.TrimSpace(req.Source), Personas: personaSpecs(req.Personas),
	}
	if req.Activity != nil {
		input := req.Activity.toInput(appID, operator)
		compute.Input = &input
	}
	job, err := h.lottery.CreateComputeJob(c.Request.Context(), compute)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", job)
}

// AdminListLotteryComputeJobs GET /api/admin/apps/:appkey/lottery/compute/jobs
func (h *Handler) AdminListLotteryComputeJobs(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	activityID, _ := strconv.ParseInt(c.Query("activityId"), 10, 64)
	result, err := h.lottery.ListComputeJobs(c.Request.Context(), appID, max(activityID, 0),
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGetLotteryComputeJob GET /api/admin/apps/:appkey/lottery/compute/jobs/:jobId
func (h *Handler) AdminGetLotteryComputeJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	jobID, ok := cardKeyPathID(c, "jobId")
	if !ok {
		return
	}
	job, err := h.lottery.GetComputeJob(c.Request.Context(), appID, jobID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", job)
}

// AdminCancelLotteryComputeJob POST /api/admin/apps/:appkey/lottery/compute/jobs/:jobId/cancel
func (h *Handler) AdminCancelLotteryComputeJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	jobID, ok := cardKeyPathID(c, "jobId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	job, err := h.lottery.CancelComputeJob(c.Request.Context(), appID, jobID, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", job)
}

// AdminDeleteLotteryComputeJob DELETE /api/admin/apps/:appkey/lottery/compute/jobs/:jobId
func (h *Handler) AdminDeleteLotteryComputeJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	jobID, ok := cardKeyPathID(c, "jobId")
	if !ok {
		return
	}
	if err := h.lottery.DeleteComputeJob(c.Request.Context(), appID, jobID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"deleted": true})
}
