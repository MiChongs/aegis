package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/growth"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 成长调控（docs/growth-control.md）：积分 / 经验获取倍率规则、每日上限、等级封顶；用户详情页「奖惩」页签的总览。

// GrowthRuleRequest 新建 / 修改规则。userId 为 0 表示全应用（修改时忽略 userId 与 kind）。
// 倍率用分数：×2 = 2/1，÷3 = 1/3，冻结 = 0/1。
type GrowthRuleRequest struct {
	UserID      int64      `json:"userId"`
	Kind        string     `json:"kind"`
	Numerator   int        `json:"numerator"`
	Denominator int        `json:"denominator"`
	Rounding    string     `json:"rounding"`
	DailyCap    int64      `json:"dailyCap"`
	Sources     []string   `json:"sources"`
	StartsAt    *time.Time `json:"startsAt"`
	EndsAt      *time.Time `json:"endsAt"`
	Enabled     *bool      `json:"enabled"`
	Label       string     `json:"label"`
	Reason      string     `json:"reason"`
}

func (r GrowthRuleRequest) input(appID int64, operator string) growth.RuleInput {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return growth.RuleInput{
		AppID: appID, UserID: r.UserID, Kind: growth.Kind(r.Kind), Numerator: r.Numerator, Denominator: r.Denominator,
		Rounding: growth.Rounding(r.Rounding), DailyCap: r.DailyCap, Sources: r.Sources, StartsAt: r.StartsAt, EndsAt: r.EndsAt,
		Enabled: enabled, Label: r.Label, Reason: r.Reason, Operator: operator,
	}
}

// GrowthRevokeRequest 撤销规则的备注。
type GrowthRevokeRequest struct {
	Note string `json:"note"`
}

// GrowthPreviewRequest 推演：base 数量的 source 收入在当前规则（加上可选的草稿规则）下会入账多少。
type GrowthPreviewRequest struct {
	UserID        int64              `json:"userId"`
	Kind          string             `json:"kind"`
	Source        string             `json:"source"`
	Base          int64              `json:"base"`
	Draft         *GrowthRuleRequest `json:"draft"`
	ExcludeRuleID int64              `json:"excludeRuleId"`
}

// GrowthLevelCapRequest 等级封顶：demote 为 true 时现有等级高于封顶就降到封顶。
type GrowthLevelCapRequest struct {
	Level  int    `json:"level" binding:"required"`
	Demote bool   `json:"demote"`
	Note   string `json:"note"`
}

// AdminGrowthCatalog 来源、取整方式、常用倍率与数值边界。
func (h *Handler) AdminGrowthCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.growth.Catalog(c.Request.Context(), appID)
	h.levelRespond(c, result, err)
}

// AdminListGrowthRules 规则列表。
func (h *Handler) AdminListGrowthRules(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.growth.ListRules(c.Request.Context(), growth.RuleQuery{
		AppID: appID, UserID: userID, Scope: c.Query("scope"), Kind: growth.Kind(c.Query("kind")), Status: c.Query("status"),
		Effect: c.Query("effect"), Keyword: c.Query("keyword"),
		Page: parsePositiveInt(c.Query("page"), 1), Limit: parsePositiveInt(c.Query("limit"), 20),
	})
	h.levelRespond(c, result, err)
}

// AdminGetGrowthRule 一条规则。
func (h *Handler) AdminGetGrowthRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	result, err := h.growth.GetRule(c.Request.Context(), appID, ruleID)
	h.levelRespond(c, result, err)
}

// AdminCreateGrowthRule 新建规则。
func (h *Handler) AdminCreateGrowthRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GrowthRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.growth.CreateRule(c.Request.Context(), req.input(appID, operator))
	h.levelRespond(c, result, err)
}

// AdminUpdateGrowthRule 修改规则（作用对象与类型不可改）。
func (h *Handler) AdminUpdateGrowthRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	var req GrowthRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.growth.UpdateRule(c.Request.Context(), ruleID, req.input(appID, operator))
	h.levelRespond(c, result, err)
}

// AdminRevokeGrowthRule 撤销规则（立即失效，记录保留）。
func (h *Handler) AdminRevokeGrowthRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	var req GrowthRevokeRequest
	_ = bind(c, &req)
	_, operator := adminAccount(c)
	result, err := h.growth.RevokeRule(c.Request.Context(), appID, ruleID, operator, req.Note)
	h.levelRespond(c, result, err)
}

// AdminDeleteGrowthRule 删除规则。
func (h *Handler) AdminDeleteGrowthRule(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	ruleID, ok := cardKeyPathID(c, "ruleId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	err := h.growth.DeleteRule(c.Request.Context(), appID, ruleID, operator)
	h.levelRespond(c, gin.H{"deleted": err == nil}, err)
}

// AdminPreviewGrowth 推演一笔收入。
func (h *Handler) AdminPreviewGrowth(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GrowthPreviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := service.GrowthPreviewInput{
		AppID: appID, UserID: req.UserID, Kind: growth.Kind(req.Kind), Source: req.Source, Base: req.Base, ExcludeRuleID: req.ExcludeRuleID,
	}
	if req.Draft != nil {
		draft := req.Draft.input(appID, "")
		input.Draft = &draft
	}
	result, err := h.growth.Preview(c.Request.Context(), input)
	h.levelRespond(c, result, err)
}

// AdminListGrowthEvents 调控记录（userId 不为 0 时含全应用记录）。
func (h *Handler) AdminListGrowthEvents(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	result, err := h.growth.ListEvents(c.Request.Context(), appID, userID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	h.levelRespond(c, result, err)
}

// AdminAppUserGrowth 一个人的调控总览。
func (h *Handler) AdminAppUserGrowth(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	result, err := h.growth.Profile(c.Request.Context(), appID, userID)
	h.levelRespond(c, result, err)
}

// AdminSetAppUserLevelCap 等级封顶（可同时降级）。
func (h *Handler) AdminSetAppUserLevelCap(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req GrowthLevelCapRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.growth.SetLevelCap(c.Request.Context(), appID, userID, req.Level, req.Demote, operator, req.Note)
	h.levelRespond(c, result, err)
}

// AdminClearAppUserLevelCap 解除等级封顶。
func (h *Handler) AdminClearAppUserLevelCap(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	result, err := h.growth.ClearLevelCap(c.Request.Context(), appID, userID, operator, c.Query("note"))
	h.levelRespond(c, result, err)
}
