package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	userdomain "aegis/internal/domain/user"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 账号处置（一键解除风控、关联删除风险账号）：/api/admin/system/risk-ops/*。
// 编排在 service/risk_ops.go；查看清单对能进系统设置的管理员开放，解除与删除限超级管理员。

const riskOpsForbiddenMsg = "仅超级管理员可解除风控或删除账号"

func riskOpsOperator(c *gin.Context) service.RiskOpsOperator {
	op := service.RiskOpsOperator{}
	if session, ok := adminAccessSession(c); ok && session != nil {
		op.AdminID = session.AdminID
		op.Name = strings.TrimSpace(session.DisplayName)
		if op.Name == "" {
			op.Name = session.Account
		}
	}
	return op
}

// AdminRiskOpsBanned 正被风控平台封禁的账号
func (h *Handler) AdminRiskOpsBanned(c *gin.Context) {
	if h.risk == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "风控未接入")
		return
	}
	appID, _ := strconv.ParseInt(c.Query("app"), 10, 64)
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, total, err := h.risk.RiskOpsBanned(c.Request.Context(), userdomain.RiskBannedQuery{AppID: appID, Keyword: c.Query("q"), Page: page, Limit: limit})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items, "total": total, "page": max(page, 1)})
}

// AdminRiskOpsInspect 一组账号的处置清单与关联账号
func (h *Handler) AdminRiskOpsInspect(c *gin.Context) {
	if h.risk == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "风控未接入")
		return
	}
	var in struct {
		AppID   int64   `json:"appId"`
		UserIDs []int64 `json:"userIds"`
		Related bool    `json:"related"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请求参数不正确")
		return
	}
	out, err := h.risk.RiskOpsInspect(c.Request.Context(), in.AppID, in.UserIDs, in.Related)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", out)
}

// AdminRiskOpsRelease 一键解除风控
func (h *Handler) AdminRiskOpsRelease(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, riskOpsForbiddenMsg)
		return
	}
	if h.risk == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "风控未接入")
		return
	}
	var in service.RiskOpsReleaseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请求参数不正确")
		return
	}
	out, err := h.risk.RiskOpsRelease(c.Request.Context(), riskOpsOperator(c), in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, 200, "已解除", out)
}

// AdminRiskOpsPurge 删除风险账号（可连同关联账号）
func (h *Handler) AdminRiskOpsPurge(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, riskOpsForbiddenMsg)
		return
	}
	if h.risk == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "风控未接入")
		return
	}
	var in service.RiskOpsPurgeInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请求参数不正确")
		return
	}
	out, err := h.risk.RiskOpsPurge(c.Request.Context(), riskOpsOperator(c), in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, 200, "已删除", out)
}
