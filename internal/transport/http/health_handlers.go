package httptransport

// 存活与就绪探针。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"
	"time"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Healthz(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	response.Success(c, 200, "ok", gin.H{
		"status":    "healthy",
		"checkedAt": time.Now().UTC(),
	})
}

func (h *Handler) Readyz(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	_, ready := h.monitor.ReadinessReport(c.Request.Context())
	c.Header("Cache-Control", "no-store")
	if !ready {
		response.Error(c, http.StatusServiceUnavailable, 50312, "服务未就绪")
		return
	}
	response.Success(c, 200, "ok", gin.H{
		"status":    "ready",
		"checkedAt": time.Now().UTC(),
	})
}
