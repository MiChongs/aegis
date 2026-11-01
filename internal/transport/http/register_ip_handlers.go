package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/domain/regip"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 注册准入（地址与设备）管理端接口。规则见 docs/register-ip.md。
//
// /api/admin/apps/:appkey/register-ip/*：读（含模拟与 IP 查询）app:user:read；改策略、名单 app:write；
// 解除限制 app:user:write。网关侧没有新接口 —— 准入在四条建号路径里，拦截以业务错误码返回。

// AdminRegisterIPOverviewQuery 概览参数（仅用于接口文档）。
type AdminRegisterIPOverviewQuery struct {
	Days int    `json:"days" form:"days"`
	TZ   string `json:"tz" form:"tz"`
}

// AdminRegisterIPInspectQuery IP 查询参数（仅用于接口文档）。
type AdminRegisterIPInspectQuery struct {
	IP string `json:"ip" form:"ip"`
}

func (h *Handler) registerIPReady(c *gin.Context) (int64, bool) {
	if h.registerIP == nil {
		response.Error(c, http.StatusServiceUnavailable, 50393, "注册 IP 控制未启用")
		return 0, false
	}
	return resolveAppID(c, h.app)
}

func queryTime(c *gin.Context, key string) *time.Time {
	if value, err := time.Parse(time.RFC3339, c.Query(key)); err == nil {
		return &value
	}
	return nil
}

// AdminRegisterIPOverview GET /api/admin/apps/:appkey/register-ip?days=7&tz=Asia/Shanghai
func (h *Handler) AdminRegisterIPOverview(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(c.Query("days"))
	result, err := h.registerIP.Overview(c.Request.Context(), appID, days, c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminRegisterIPPolicy GET /api/admin/apps/:appkey/register-ip/policy
func (h *Handler) AdminRegisterIPPolicy(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	result, err := h.registerIP.Policy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminUpdateRegisterIPPolicy PUT /api/admin/apps/:appkey/register-ip/policy
func (h *Handler) AdminUpdateRegisterIPPolicy(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req regip.Policy
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.registerIP.SavePolicy(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", result)
}

// AdminSimulateRegisterIP POST /api/admin/apps/:appkey/register-ip/simulate
// 草稿策略（policy 为空用已保存的）+ 地址与场景；计数取真实值、只数不占，可整体改写。
func (h *Handler) AdminSimulateRegisterIP(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req regip.SimulateInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.registerIP.Simulate(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "模拟完成", result)
}

// AdminInspectRegisterIP GET /api/admin/apps/:appkey/register-ip/inspect?ip=
// 一个地址此刻的全部状况：名单、惩罚期、各规则用量、此刻注册会怎样、注册过的账号与最近的拦截。
func (h *Handler) AdminInspectRegisterIP(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	result, err := h.registerIP.Inspect(c.Request.Context(), appID, c.Query("ip"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminReleaseRegisterIP POST /api/admin/apps/:appkey/register-ip/release
func (h *Handler) AdminReleaseRegisterIP(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req service.RegisterIPReleaseInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.registerIP.Release(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已解除", result)
}

// AdminRegisterIPPenalties GET /api/admin/apps/:appkey/register-ip/penalties?limit=
func (h *Handler) AdminRegisterIPPenalties(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 100
	}
	items, total, err := h.registerIP.Penalties(c.Request.Context(), appID, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", regip.PenaltyList{Items: items, Total: total})
}

// AdminRegisterIPEvents GET /api/admin/apps/:appkey/register-ip/events
// 参数：page、limit、kind、reason、ruleId、method、ip（地址或网段）、deviceId、keyword、from、to（RFC 3339）。
func (h *Handler) AdminRegisterIPEvents(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	query := regip.EventQuery{
		AppID: appID, Kind: strings.TrimSpace(c.Query("kind")), Reason: strings.TrimSpace(c.Query("reason")),
		RuleID: strings.TrimSpace(c.Query("ruleId")), Method: strings.TrimSpace(c.Query("method")), IP: c.Query("ip"),
		DeviceID: strings.TrimSpace(c.Query("deviceId")),
		Keyword: c.Query("keyword"), From: queryTime(c, "from"), To: queryTime(c, "to"), Page: page, PageSize: limit,
	}
	items, total, err := h.registerIP.Events(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	page, limit = normalizedPage(page, limit)
	response.Success(c, http.StatusOK, "获取成功", regip.EventList{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminRegisterIPRegistrations GET /api/admin/apps/:appkey/register-ip/registrations
// 参数：page、limit、ip（地址或网段）、deviceId、method、keyword、released（true/false）、from、to。
func (h *Handler) AdminRegisterIPRegistrations(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	query := regip.RegistrationQuery{
		AppID: appID, IP: c.Query("ip"), DeviceID: strings.TrimSpace(c.Query("deviceId")), Method: strings.TrimSpace(c.Query("method")), Keyword: c.Query("keyword"),
		From: queryTime(c, "from"), To: queryTime(c, "to"), Page: page, PageSize: limit,
	}
	if released, err := strconv.ParseBool(c.Query("released")); err == nil {
		query.Released = &released
	}
	items, total, err := h.registerIP.Registrations(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	page, limit = normalizedPage(page, limit)
	response.Success(c, http.StatusOK, "获取成功", regip.RegistrationList{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminRegisterIPLists GET /api/admin/apps/:appkey/register-ip/lists?kind=&keyword=&expired=
func (h *Handler) AdminRegisterIPLists(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	expired, _ := strconv.ParseBool(c.Query("expired"))
	items, total, err := h.registerIP.ListLists(c.Request.Context(), regip.ListQuery{
		AppID: appID, Kind: strings.TrimSpace(c.Query("kind")), Keyword: c.Query("keyword"), IncludeExpired: expired,
		Page: page, PageSize: limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	page, limit = normalizedPage(page, limit)
	response.Success(c, http.StatusOK, "获取成功", regip.ListEntryList{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminAddRegisterIPLists POST /api/admin/apps/:appkey/register-ip/lists
// values 可以一次贴多行（换行、逗号、空格分隔），单个地址或网段，最多 500 项。
func (h *Handler) AdminAddRegisterIPLists(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req regip.ListInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	items, err := h.registerIP.AddLists(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已添加", gin.H{"items": items})
}

// AdminUpdateRegisterIPList PUT /api/admin/apps/:appkey/register-ip/lists/:id
func (h *Handler) AdminUpdateRegisterIPList(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的名单项")
		return
	}
	var req regip.ListUpdate
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.registerIP.UpdateList(c.Request.Context(), appID, id, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", item)
}

// AdminDeleteRegisterIPLists POST /api/admin/apps/:appkey/register-ip/lists/delete
func (h *Handler) AdminDeleteRegisterIPLists(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req regip.ListDeleteInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, err := h.registerIP.DeleteLists(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": deleted})
}

// AdminRegisterDeviceInspectQuery 设备查询参数（仅用于接口文档）。
type AdminRegisterDeviceInspectQuery struct {
	Device string `json:"device" form:"device"`
}

// AdminInspectRegisterDevice GET /api/admin/apps/:appkey/register-ip/devices/inspect?device=
// 一台设备此刻的全部状况：名单、惩罚期、设备规则用量、注册过的账号、用过的地址（关联）、最近拦截。
func (h *Handler) AdminInspectRegisterDevice(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	result, err := h.registerIP.InspectDevice(c.Request.Context(), appID, c.Query("device"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminRegisterDeviceLists GET /api/admin/apps/:appkey/register-ip/device-lists?kind=&keyword=&expired=
func (h *Handler) AdminRegisterDeviceLists(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	expired, _ := strconv.ParseBool(c.Query("expired"))
	items, total, err := h.registerIP.ListDeviceLists(c.Request.Context(), regip.ListQuery{
		AppID: appID, Kind: strings.TrimSpace(c.Query("kind")), Keyword: c.Query("keyword"), IncludeExpired: expired,
		Page: page, PageSize: limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	page, limit = normalizedPage(page, limit)
	response.Success(c, http.StatusOK, "获取成功", regip.DeviceListList{Items: items, Total: total, Page: page, Limit: limit})
}

// AdminAddRegisterDeviceLists POST /api/admin/apps/:appkey/register-ip/device-lists
// values 可以一次贴多行（换行、逗号、空格分隔），最多 500 项；无效的设备标识只能加入拒绝名单。
func (h *Handler) AdminAddRegisterDeviceLists(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req regip.ListInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	items, err := h.registerIP.AddDeviceLists(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已添加", gin.H{"items": items})
}

// AdminUpdateRegisterDeviceList PUT /api/admin/apps/:appkey/register-ip/device-lists/:id
func (h *Handler) AdminUpdateRegisterDeviceList(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的名单项")
		return
	}
	var req regip.ListUpdate
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.registerIP.UpdateDeviceList(c.Request.Context(), appID, id, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", item)
}

// AdminDeleteRegisterDeviceLists POST /api/admin/apps/:appkey/register-ip/device-lists/delete
func (h *Handler) AdminDeleteRegisterDeviceLists(c *gin.Context) {
	appID, ok := h.registerIPReady(c)
	if !ok {
		return
	}
	var req regip.ListDeleteInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, err := h.registerIP.DeleteDeviceLists(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": deleted})
}

// normalizedPage 回显给前端的分页参数，与服务端实际使用的口径一致。
func normalizedPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	return page, min(limit, 200)
}
