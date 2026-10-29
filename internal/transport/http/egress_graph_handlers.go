package httptransport

import (
	"net/http"
	"strconv"

	"aegis/internal/domain/egressgraph"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// 出海网关画布与托管的 mihomo 核心。与路由表同样限超级管理员。

// AdminEgressRollbackRequest 回滚到某个已应用的版本。
type AdminEgressRollbackRequest struct {
	Version int64 `json:"version" binding:"required"`
}

// AdminEgressCoreInstallRequest 安装或升级核心。
type AdminEgressCoreInstallRequest struct {
	// Version 留空或 latest 为最新正式版，如 v1.19.33。
	Version string `json:"version"`
}

// AdminEgressCoreSelectRequest 切换策略组当前节点；Name 留空表示取消固定（url-test / fallback）。
type AdminEgressCoreSelectRequest struct {
	Group string `json:"group" binding:"required"`
	Name  string `json:"name"`
}

// AdminEgressCoreCloseRequest 关闭连接；ID 留空关闭全部。
type AdminEgressCoreCloseRequest struct {
	ID string `json:"id"`
}

// AdminEgressCoreProviderRequest 订阅 / 规则集操作。
type AdminEgressCoreProviderRequest struct {
	Kind   string `json:"kind" binding:"required,oneof=proxy rule"`
	Name   string `json:"name" binding:"required"`
	Action string `json:"action" binding:"required,oneof=update healthcheck"`
}

// AdminEgressCoreDNSRequest 经核心 DNS 解析。
type AdminEgressCoreDNSRequest struct {
	Name string `json:"name" binding:"required"`
	Type string `json:"type"`
}

// AdminEgressCoreMaintenanceRequest 核心维护操作。
type AdminEgressCoreMaintenanceRequest struct {
	Action string `json:"action" binding:"required,oneof=geo flush-dns"`
}

func (h *Handler) requireEgressSuperAdmin(c *gin.Context) bool {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理出海网关")
		return false
	}
	return true
}

func (h *Handler) AdminGetEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	item, err := h.egress.GraphView(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminSaveEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req egressgraph.SaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.SaveGraphDraft(c.Request.Context(), actorIDPtr(c), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "草稿已保存", item)
}

func (h *Handler) AdminValidateEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req egressgraph.SaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.ValidateGraph(c.Request.Context(), req.Graph)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "校验完成", item)
}

func (h *Handler) AdminApplyEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req egressgraph.ApplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.ApplyGraph(c.Request.Context(), actorIDPtr(c), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已应用", item)
}

func (h *Handler) AdminDiscardEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	item, err := h.egress.DiscardGraphDraft(c.Request.Context(), actorIDPtr(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已丢弃草稿", item)
}

func (h *Handler) AdminImportEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req egressgraph.ImportRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.ImportGraph(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "导入完成", item)
}

func (h *Handler) AdminExportEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req egressgraph.ExportRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	yaml, err := h.egress.ExportGraph(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "导出完成", gin.H{"yaml": yaml})
}

func (h *Handler) AdminEgressGraphHistory(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	items, err := h.egress.GraphHistory(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items})
}

func (h *Handler) AdminEgressGraphVersion(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本号无效")
		return
	}
	graph, err := h.egress.GraphVersion(c.Request.Context(), version)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"version": version, "graph": graph})
}

func (h *Handler) AdminRollbackEgressGraph(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressRollbackRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.RollbackGraph(c.Request.Context(), actorIDPtr(c), req.Version)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已回滚", item)
}

func (h *Handler) AdminEgressCoreStatus(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	item, err := h.egress.CoreStatus()
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminEgressCoreInstall(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressCoreInstallRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.InstallCore(c.Request.Context(), req.Version)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "核心已安装", item)
}

func (h *Handler) AdminEgressCoreRestart(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	if err := h.egress.RestartCore(c.Request.Context()); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "核心已重启", nil)
}

func (h *Handler) AdminEgressCoreStop(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	if err := h.egress.StopCore(); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "核心已停止", nil)
}

func (h *Handler) AdminEgressCoreProxies(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	item, err := h.egress.CoreProxies(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminEgressCoreDelay(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req service.CoreDelayRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.CoreDelay(c.Request.Context(), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试完成", gin.H{"delays": item})
}

func (h *Handler) AdminEgressCoreSelect(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressCoreSelectRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.egress.CoreSelect(c.Request.Context(), req.Group, req.Name); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已切换", nil)
}

func (h *Handler) AdminEgressCoreConnections(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	item, err := h.egress.CoreConnections(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminEgressCoreCloseConnection(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressCoreCloseRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.egress.CoreCloseConnection(c.Request.Context(), req.ID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已关闭", nil)
}

func (h *Handler) AdminEgressCoreProvider(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressCoreProviderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.egress.CoreProviderAction(c.Request.Context(), req.Kind, req.Name, req.Action); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "操作完成", nil)
}

func (h *Handler) AdminEgressCoreRules(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	items, err := h.egress.CoreRules(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items})
}

func (h *Handler) AdminEgressCoreLogs(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	after, _ := strconv.ParseUint(c.Query("after"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.egress.CoreLogs(after, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items})
}

func (h *Handler) AdminEgressCoreDNS(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressCoreDNSRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.CoreDNSQuery(c.Request.Context(), req.Name, req.Type)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "解析完成", item)
}

func (h *Handler) AdminEgressCoreMaintenance(c *gin.Context) {
	if !h.requireEgressSuperAdmin(c) {
		return
	}
	var req AdminEgressCoreMaintenanceRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.egress.CoreMaintenance(c.Request.Context(), req.Action); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "操作完成", nil)
}
