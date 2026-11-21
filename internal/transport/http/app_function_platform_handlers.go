package httptransport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	functiondomain "aegis/internal/domain/appfunction"
	"aegis/pkg/response"
)

// 远程函数的平台化接口：全景、触发器、触发任务、调用详情与重放（docs/app-functions.md）。

type appFunctionTriggerRequest struct {
	Kind        string          `json:"kind"`
	Event       string          `json:"event"`
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Enabled     *bool           `json:"enabled"`
}

func (r appFunctionTriggerRequest) toInput() functiondomain.TriggerInput {
	return functiondomain.TriggerInput{
		Kind: r.Kind, Event: r.Event, Cron: r.Cron, Timezone: r.Timezone,
		Description: r.Description, Input: r.Input, Enabled: r.Enabled,
	}
}

type fireAppFunctionTriggerRequest struct {
	UserID *int64          `json:"userId"`
	Data   json.RawMessage `json:"data"`
}

func functionPathID(c *gin.Context, name string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "路径参数 "+name+" 无效")
		return 0, false
	}
	return value, true
}

// AdminAppFunctionOverview 一个应用全部函数的运行全景。
func (h *Handler) AdminAppFunctionOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	hours, _ := strconv.Atoi(c.DefaultQuery("windowHours", "24"))
	result, err := h.appFunction.Overview(c.Request.Context(), appID, hours)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// AdminListAppTriggers 整个应用的触发器。
func (h *Handler) AdminListAppTriggers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.appFunction.ListTriggers(c.Request.Context(), appID, "")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"items": items, "events": functiondomain.TriggerEventCatalog()})
}

// AdminListAppFunctionTriggers 一个函数的触发器。
func (h *Handler) AdminListAppFunctionTriggers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.appFunction.ListTriggers(c.Request.Context(), appID, c.Param("functionName"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"items": items})
}

func (h *Handler) AdminCreateAppFunctionTrigger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req appFunctionTriggerRequest
	if err := bindLimitedJSON(c, &req, 64<<10); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	trigger, err := h.appFunction.CreateTrigger(c.Request.Context(), appID, c.Param("functionName"), req.toInput(), currentAdminID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "触发器已创建", trigger)
}

func (h *Handler) AdminUpdateAppFunctionTrigger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	triggerID, ok := functionPathID(c, "triggerId")
	if !ok {
		return
	}
	var req appFunctionTriggerRequest
	if err := bindLimitedJSON(c, &req, 64<<10); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	trigger, err := h.appFunction.UpdateTrigger(c.Request.Context(), appID, c.Param("functionName"), triggerID, req.toInput())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "触发器已更新", trigger)
}

func (h *Handler) AdminDeleteAppFunctionTrigger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	triggerID, ok := functionPathID(c, "triggerId")
	if !ok {
		return
	}
	if err := h.appFunction.DeleteTrigger(c.Request.Context(), appID, c.Param("functionName"), triggerID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "触发器已删除", gin.H{"id": triggerID})
}

// AdminFireAppFunctionTrigger 立即真实执行一次（同步）。失败也回 200，判成功看 ok。
func (h *Handler) AdminFireAppFunctionTrigger(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	triggerID, ok := functionPathID(c, "triggerId")
	if !ok {
		return
	}
	var req fireAppFunctionTriggerRequest
	if err := bindLimitedJSON(c, &req, 256<<10); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.appFunction.FireTrigger(c.Request.Context(), appID, c.Param("functionName"), triggerID,
		functiondomain.TriggerFireInput{UserID: req.UserID, Data: req.Data})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// AdminListAppFunctionTriggerJobs 触发任务（排队、重试、失败）。
func (h *Handler) AdminListAppFunctionTriggerJobs(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	triggerID, _ := strconv.ParseInt(c.Query("triggerId"), 10, 64)
	result, err := h.appFunction.ListTriggerJobs(c.Request.Context(), appID, c.Param("functionName"),
		functiondomain.TriggerJobQuery{
			TriggerID: triggerID, Status: strings.TrimSpace(c.Query("status")), Page: page, Limit: limit,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

func (h *Handler) AdminRetryAppFunctionTriggerJob(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	jobID, ok := functionPathID(c, "jobId")
	if !ok {
		return
	}
	if err := h.appFunction.RetryTriggerJob(c.Request.Context(), appID, c.Param("functionName"), jobID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已放回队列", gin.H{"id": jobID})
}

// AdminGetAppFunctionInvocation 一条调用的完整记录。
func (h *Handler) AdminGetAppFunctionInvocation(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	invocationID, ok := functionPathID(c, "invocationId")
	if !ok {
		return
	}
	result, err := h.appFunction.GetInvocation(c.Request.Context(), appID, c.Param("functionName"), invocationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// AdminReplayAppFunctionInvocation 用这次调用的入参与身份在激活版本上试跑（只记不做）。
func (h *Handler) AdminReplayAppFunctionInvocation(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	invocationID, ok := functionPathID(c, "invocationId")
	if !ok {
		return
	}
	result, err := h.appFunction.ReplayInvocation(c.Request.Context(), appID, c.Param("functionName"), invocationID, currentAdminID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}
