package httptransport

import (
	"encoding/json"
	"net/http"

	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// Cloud Mail 邮局代管：一条 provider=skymail 的邮件通道，在控制台里可以直接管它背后的邮局。
//
// 两个作用域各两条路由 —— query（只读）与 action（写）。资源与动作的白名单、
// 参数校验都在服务层（service/skymail_admin.go），这里只负责取作用域与限流请求体。
//
//	平台级  POST /api/admin/system/email/configs/:configId/skymail/query
//	        POST /api/admin/system/email/configs/:configId/skymail/action
//	应用级  POST /api/admin/app/email-config/skymail/query    （appid、config_id 在请求体）
//	        POST /api/admin/app/email-config/skymail/action

// skymailMaxRequestBytes 写信可以带 10 个附件（解码后共 25MB），Base64 再膨胀三分之一。
const skymailMaxRequestBytes = 40 << 20

// SkymailQueryRequest 平台级查询：resource 是资源名，params 是该资源的参数。
type SkymailQueryRequest struct {
	Resource string          `json:"resource" binding:"required"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// SkymailActionRequest 平台级写操作。
type SkymailActionRequest struct {
	Action string          `json:"action" binding:"required"`
	Params json.RawMessage `json:"params,omitempty"`
}

// AdminSkymailQueryRequest 应用级查询。
type AdminSkymailQueryRequest struct {
	AppID    int64           `json:"appid" binding:"required"`
	ConfigID int64           `json:"config_id" binding:"required"`
	Resource string          `json:"resource" binding:"required"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// AdminSkymailActionRequest 应用级写操作。
type AdminSkymailActionRequest struct {
	AppID    int64           `json:"appid" binding:"required"`
	ConfigID int64           `json:"config_id" binding:"required"`
	Action   string          `json:"action" binding:"required"`
	Params   json.RawMessage `json:"params,omitempty"`
}

func (h *Handler) AdminPlatformEmailSkymailQuery(c *gin.Context) {
	configID, ok := parseEmailConfigID(c)
	if !ok {
		return
	}
	var req SkymailQueryRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, err := h.email.SkymailQuery(c.Request.Context(), emaildomain.PlatformAppID, configID, req.Resource, req.Params)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", data)
}

func (h *Handler) AdminPlatformEmailSkymailAction(c *gin.Context) {
	configID, ok := parseEmailConfigID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, skymailMaxRequestBytes)
	var req SkymailActionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, err := h.email.SkymailAction(c.Request.Context(), emaildomain.PlatformAppID, configID, req.Action, req.Params)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "操作成功", data)
}

func (h *Handler) AdminEmailSkymailQuery(c *gin.Context) {
	var req AdminSkymailQueryRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, err := h.email.SkymailQuery(c.Request.Context(), req.AppID, req.ConfigID, req.Resource, req.Params)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", data)
}

func (h *Handler) AdminEmailSkymailAction(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, skymailMaxRequestBytes)
	var req AdminSkymailActionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, err := h.email.SkymailAction(c.Request.Context(), req.AppID, req.ConfigID, req.Action, req.Params)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "操作成功", data)
}
