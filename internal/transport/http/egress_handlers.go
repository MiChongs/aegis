package httptransport

import (
	"net/http"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/egress"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// 出海代理网关管理端。全部限超级管理员：路由表决定平台所有对外调用走哪条线，
// 改错一条规则的影响面等同于改防火墙。

func (h *Handler) AdminGetEgressSettings(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看出海网关配置")
		return
	}
	item, err := h.egress.GetSettings(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminUpdateEgressSettings(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可调整出海网关配置")
		return
	}
	var req systemdomain.EgressSettingsUpdate
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.UpdateSettings(c.Request.Context(), actorIDPtr(c), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminResetEgressSettings(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可重置出海网关配置")
		return
	}
	item, err := h.egress.ResetToEnv(c.Request.Context(), actorIDPtr(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已恢复为环境变量配置", item)
}

func (h *Handler) AdminTestEgress(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可执行出海自测")
		return
	}
	var req AdminEgressTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.egress.Test(c.Request.Context(), egress.TestRequest{
		URL:       req.URL,
		Endpoint:  req.Endpoint,
		Profile:   req.Profile,
		TimeoutMS: req.TimeoutMs,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "自测完成", result)
}

func (h *Handler) AdminExplainEgress(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查询出海路由")
		return
	}
	var req AdminEgressExplainRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.egress.Explain(systemdomain.EgressExplainRequest{
		Host: req.Host, Port: req.Port, Scheme: req.Scheme, Profile: req.Profile,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "查询成功", item)
}

func (h *Handler) AdminProbeEgress(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可触发出海探测")
		return
	}
	results, err := h.egress.Probe(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "探测完成", gin.H{"results": results})
}

// actorIDPtr 取当前管理员 ID，未登录或异常时返回 nil（审计字段允许为空）。
func actorIDPtr(c *gin.Context) *int64 {
	adminID, _ := adminActor(c)
	if adminID <= 0 {
		return nil
	}
	return &adminID
}
