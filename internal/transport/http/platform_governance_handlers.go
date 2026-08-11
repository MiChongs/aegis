package httptransport

import (
	"net/http"
	"strings"

	platformdomain "aegis/internal/domain/platform"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// ── 平台侧（/api/admin/platform/*，全站作用域）──

// AdminPlatformOverview 全站应用总览 + 聚合指标。
func (h *Handler) AdminPlatformOverview(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	query := platformdomain.OverviewQuery{
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		State:     strings.TrimSpace(c.Query("state")),
		Governed:  strings.EqualFold(strings.TrimSpace(c.Query("governed")), "true"),
		SortBy:    strings.TrimSpace(c.Query("sortBy")),
		SortOrder: strings.TrimSpace(c.Query("sortOrder")),
		Page:      queryInt(c, "page", 1),
		Limit:     queryInt(c, "limit", 20),
	}
	result, err := h.governance.Overview(c.Request.Context(), query, h.governanceScope(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminPlatformGovernanceCatalog 治理目录：状态 / 能力 / 常用时长。
func (h *Handler) AdminPlatformGovernanceCatalog(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	response.Success(c, 200, "获取成功", h.governance.Catalog())
}

// AdminPlatformAppGovernance 单应用治理详情（平台视角）。
func (h *Handler) AdminPlatformAppGovernance(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.writeGovernanceDetail(c, appID)
}

// AdminPlatformApplyGovernance 执行治理动作。
func (h *Handler) AdminPlatformApplyGovernance(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GovernanceActionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input, err := req.ToInput()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "到期时间格式无效")
		return
	}
	state, record, err := h.governance.Apply(c.Request.Context(), appID, input, h.governanceActor(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "治理动作已执行", gin.H{"governance": state, "action": record})
}

// AdminPlatformBatchGovernance 批量治理。
func (h *Handler) AdminPlatformBatchGovernance(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	var req GovernanceBatchActionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input, err := req.GovernanceActionRequest.ToInput()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "到期时间格式无效")
		return
	}
	result, err := h.governance.BatchApply(c.Request.Context(), platformdomain.BatchActionInput{
		AppIDs:      req.AppIDs,
		ActionInput: input,
	}, h.governanceActor(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量治理已执行", result)
}

// AdminPlatformRevokeAppSessions 强制下线该应用全部在线用户（危险操作）。
func (h *Handler) AdminPlatformRevokeAppSessions(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GovernanceRevokeSessionsRequest
	_ = bind(c, &req)
	record, err := h.governance.RevokeAppSessions(c.Request.Context(), appID, req.Reason, h.governanceActor(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已强制下线该应用全部在线会话", record)
}

// AdminPlatformGovernanceActions 全站治理流水。
func (h *Handler) AdminPlatformGovernanceActions(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	result, err := h.governance.ListActions(c.Request.Context(), platformdomain.ActionQuery{
		AppID:   int64(queryInt(c, "appid", 0)),
		Action:  strings.TrimSpace(c.Query("action")),
		State:   strings.TrimSpace(c.Query("state")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    queryInt(c, "page", 1),
		Limit:   queryInt(c, "limit", 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminPlatformListAppeals 治理申诉列表。
func (h *Handler) AdminPlatformListAppeals(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	result, err := h.governance.ListAppeals(c.Request.Context(), platformdomain.AppealQuery{
		AppID:   int64(queryInt(c, "appid", 0)),
		Status:  strings.TrimSpace(c.Query("status")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    queryInt(c, "page", 1),
		Limit:   queryInt(c, "limit", 20),
	}, h.governanceScope(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminPlatformReviewAppeal 裁决治理申诉。
func (h *Handler) AdminPlatformReviewAppeal(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	appealID, err := pathInt64(c, "appealId")
	if err != nil || appealID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的申诉标识")
		return
	}
	var req GovernanceAppealReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	appeal, err := h.governance.ReviewAppeal(c.Request.Context(), appealID, platformdomain.AppealReviewInput{
		Decision: req.Decision,
		Note:     req.Note,
		Restore:  req.Restore,
	}, h.governanceActor(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "申诉已处理", appeal)
}

// ── 应用侧（/api/admin/apps/:appkey/governance*，应用作用域）──

// AdminAppGovernance 应用管理员查看自己应用的治理状态。
func (h *Handler) AdminAppGovernance(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.writeGovernanceDetail(c, appID)
}

// AdminAppSubmitGovernanceAppeal 应用管理员提交申诉。
//
// 这条路径刻意不受 blockAdminWrite 只读闸门约束（见 middleware.enforceGovernanceAdminWrite）：
// 停运状态下唯一还能做的事就是申诉。
func (h *Handler) AdminAppSubmitGovernanceAppeal(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GovernanceAppealRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	appeal, err := h.governance.SubmitAppeal(c.Request.Context(), appID, platformdomain.AppealCreateInput{
		Content:     req.Content,
		Attachments: req.Attachments,
	}, adminID, adminName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "申诉已提交，等待平台审核", appeal)
}

// AdminAppWithdrawGovernanceAppeal 撤回自己提交的待审申诉。
func (h *Handler) AdminAppWithdrawGovernanceAppeal(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	appealID, err := pathInt64(c, "appealId")
	if err != nil || appealID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的申诉标识")
		return
	}
	adminID, _ := adminActor(c)
	appeal, err := h.governance.WithdrawAppeal(c.Request.Context(), appealID, adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "申诉已撤回", appeal)
}

// AdminAppGovernanceHistory 应用自己的治理流水。
func (h *Handler) AdminAppGovernanceHistory(c *gin.Context) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.governance.ListActions(c.Request.Context(), platformdomain.ActionQuery{
		AppID: appID,
		Page:  queryInt(c, "page", 1),
		Limit: queryInt(c, "limit", 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// ── 内部 ──

func (h *Handler) writeGovernanceDetail(c *gin.Context, appID int64) {
	if h.governance == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "平台治理服务暂不可用")
		return
	}
	state, err := h.governance.Get(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	history, err := h.governance.ListActions(c.Request.Context(), platformdomain.ActionQuery{AppID: appID, Page: 1, Limit: 10})
	if err != nil {
		h.writeError(c, err)
		return
	}
	appeals, err := h.governance.ListAppeals(c.Request.Context(), platformdomain.AppealQuery{
		AppID: appID, Status: platformdomain.AppealStatusPending, Page: 1, Limit: 1,
	}, nil)
	if err != nil {
		h.writeError(c, err)
		return
	}
	var pending *platformdomain.Appeal
	if len(appeals.Items) > 0 {
		pending = &appeals.Items[0]
	}
	actor := h.governanceActor(c)
	response.Success(c, 200, "获取成功", GovernanceDetailResponse{
		Governance:    *state,
		RecentActions: history.Items,
		PendingAppeal: pending,
		CanGovern:     h.hasPlatformPermission(c, service.PermissionPlatformAppGovern),
		CanDanger:     actor.CanDanger,
	})
}

// governanceActor 由当前会话构造治理操作者。
//
// CanDanger 在这里一次性解析好，服务层不再回头查权限 ——
// 判权分散在两层会让"到底谁能封禁"变成需要对照两处代码才能回答的问题。
func (h *Handler) governanceActor(c *gin.Context) platformdomain.Actor {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		return platformdomain.Actor{}
	}
	return platformdomain.Actor{
		AdminID:      session.AdminID,
		AdminName:    governanceActorName(session.DisplayName, session.Account),
		IP:           c.ClientIP(),
		IsSuperAdmin: session.IsSuperAdmin,
		CanDanger:    session.IsSuperAdmin || h.hasPlatformPermission(c, service.PermissionPlatformAppDanger),
	}
}

// governanceScope 当前管理员的可见应用范围；nil 表示不限范围。
func (h *Handler) governanceScope(c *gin.Context) []int64 {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		return []int64{-1}
	}
	return service.AllowedGovernanceAppIDs(session.IsSuperAdmin, session.Assignments)
}

func (h *Handler) hasPlatformPermission(c *gin.Context, permission string) bool {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		return false
	}
	if session.IsSuperAdmin {
		return true
	}
	if h.admin == nil {
		return false
	}
	// 传 nil 作用域：平台级权限点只在全局作用域下才算数
	return h.admin.Authorize(c.Request.Context(), session, permission, nil) == nil
}

func governanceActorName(displayName, account string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	return strings.TrimSpace(account)
}
