package httptransport

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	cloudstorage "aegis/internal/domain/cloudstorage"
	devicedomain "aegis/internal/domain/device"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 云服务（docs/cloud-service.md）：档位、记录级同步、设备中心与接力。业务规则全部在 CloudStorageService。
//
// 「当前设备」一律取会话签发时的设备号（与登录设备管理同一个号），会话里没有时再看 X-Device-Id：
// 请求体里的设备号不可信 —— 否则一台设备可以冒充另一台去推送、领取别人的接力。

// ── DTO ──

// CloudSyncDeviceRequest 登记 / 刷新当前设备。rename 为真时 name 是用户起的名字。
type CloudSyncDeviceRequest struct {
	Name     string `json:"name"`
	Rename   bool   `json:"rename"`
	Platform string `json:"platform"`
	Model    string `json:"model"`
}

// CloudSyncRenameRequest 给一台设备改名。
type CloudSyncRenameRequest struct {
	Name string `json:"name" binding:"required"`
}

// CloudSyncActivityRequest 当前设备正在播放 / 阅读的内容。state 为 idle 或 clear 为真时清空。
type CloudSyncActivityRequest struct {
	Collection string          `json:"collection"`
	RecordID   string          `json:"recordId"`
	State      string          `json:"state"`
	Payload    json.RawMessage `json:"payload"`
	Clear      bool            `json:"clear"`
}

// CloudSyncChangeDTO 一条改动。updatedAt 是 RFC 3339 时间或毫秒时间戳二选一（updatedAtMs）。
type CloudSyncChangeDTO struct {
	Collection  string          `json:"collection"`
	ID          string          `json:"id"`
	Payload     json.RawMessage `json:"payload"`
	UpdatedAt   *time.Time      `json:"updatedAt"`
	UpdatedAtMs int64           `json:"updatedAtMs"`
	Deleted     bool            `json:"deleted"`
}

// CloudSyncPushRequest 一批改动（至多 500 条）。
type CloudSyncPushRequest struct {
	Changes []CloudSyncChangeDTO `json:"changes" binding:"required"`
}

// CloudSyncChangesQuery 增量拉取。collections 用逗号分隔，省略表示全部。
type CloudSyncChangesQuery struct {
	Since       int64  `json:"since" form:"since"`
	Collections string `json:"collections" form:"collections"`
	Limit       int    `json:"limit" form:"limit"`
}

// CloudSyncRecordsQuery 浏览记录：按集合、领域、写入设备筛。
type CloudSyncRecordsQuery struct {
	Collection string `json:"collection" form:"collection"`
	Domain     string `json:"domain" form:"domain"`
	DeviceID   string `json:"deviceId" form:"deviceId"`
	Keyword    string `json:"keyword" form:"keyword"`
	Page       int    `json:"page" form:"page"`
	Limit      int    `json:"limit" form:"limit"`
}

// CloudSyncHandoffRequest 把一条记录接力到另一台设备。
type CloudSyncHandoffRequest struct {
	ToDeviceID string          `json:"toDeviceId" binding:"required"`
	Collection string          `json:"collection" binding:"required"`
	RecordID   string          `json:"recordId" binding:"required"`
	Payload    json.RawMessage `json:"payload"`
}

// ── 公共 ──

// syncDeviceID 当前设备号：会话里的优先，其次 X-Device-Id。
func syncDeviceID(c *gin.Context, sessionDeviceID string) string {
	if id := devicedomain.NormalizeID(sessionDeviceID); id != "" {
		return id
	}
	return devicedomain.NormalizeID(deviceHeader(c, "X-Device-Id"))
}

func (h *Handler) cloudSyncSession(c *gin.Context) (appID, userID int64, deviceID string, ok bool) {
	session, found := authSession(c)
	if !found {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return 0, 0, "", false
	}
	return session.AppID, session.UserID, syncDeviceID(c, session.DeviceID), true
}

// ── 用户端 ──

// AppCloudPlans 档位对比：每一档的空间、设备数、历史保留、实时与接力，标出当前档位。
func (h *Handler) AppCloudPlans(c *gin.Context) {
	appID, userID, _, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	plans, err := h.cloudStorage.Plans(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": plans})
}

// AppCloudSync 云服务首页：档位、用量、设备、各集合的记录数。
func (h *Handler) AppCloudSync(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	overview, err := h.cloudStorage.Overview(c.Request.Context(), appID, userID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", overview)
}

// AppCloudSyncRegisterDevice 登记（或刷新）当前设备；新设备超出档位设备数时回 41364。
func (h *Handler) AppCloudSyncRegisterDevice(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CloudSyncDeviceRequest
	if c.Request.ContentLength != 0 {
		if err := bind(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, err.Error())
			return
		}
	}
	info := cloudstorage.DeviceInfo{
		DeviceID:   syncDeviceID(c, session.DeviceID),
		Name:       req.Name,
		Platform:   firstNonEmpty(session.DevicePlatform, deviceHeader(c, "X-Device-Platform"), req.Platform),
		Model:      firstNonEmpty(session.DeviceModel, req.Model),
		OS:         deviceHeader(c, "X-Device-OS"),
		OSVersion:  deviceHeader(c, "X-Device-OS-Version"),
		AppVersion: deviceHeader(c, "X-App-Version"),
	}
	if extra := session.DeviceExtra; extra != nil {
		info.OS = firstNonEmpty(info.OS, extra.OS)
		info.OSVersion = firstNonEmpty(info.OSVersion, extra.OSVersion)
		info.AppVersion = firstNonEmpty(info.AppVersion, extra.AppVersion)
	}
	if !req.Rename && info.Name == "" {
		_, name := h.resolveGatewayDevice(c, info.DeviceID, "")
		info.Name = firstNonEmpty(name, session.Device, info.Model)
	}
	device, plan, err := h.cloudStorage.RegisterDevice(c.Request.Context(), session.AppID, session.UserID,
		service.CloudDeviceInput{DeviceInfo: info, Rename: req.Rename})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"device": device, "plan": plan})
}

// AppCloudSyncHeartbeat 当前设备的心跳（前台时约每分钟一次）。
func (h *Handler) AppCloudSyncHeartbeat(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	if err := h.cloudStorage.Heartbeat(c.Request.Context(), appID, userID, deviceID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"serverTime": time.Now()})
}

// AppCloudSyncActivity 上报当前设备正在播放 / 阅读的内容。
func (h *Handler) AppCloudSyncActivity(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	var req CloudSyncActivityRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var activity *cloudstorage.Activity
	if !req.Clear && req.State != cloudstorage.ActivityIdle {
		activity = &cloudstorage.Activity{
			Collection: strings.TrimSpace(req.Collection), RecordID: strings.TrimSpace(req.RecordID),
			State: strings.TrimSpace(req.State), Payload: req.Payload,
		}
	}
	if err := h.cloudStorage.SetActivity(c.Request.Context(), appID, userID, deviceID, activity); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", nil)
}

// AppCloudSyncDevices 登记过的全部设备：在线状态、正在播放、各集合的记录数。
func (h *Handler) AppCloudSyncDevices(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	devices, err := h.cloudStorage.ListDevices(c.Request.Context(), appID, userID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": devices})
}

// AppCloudSyncRenameDevice 给一台设备改名。
func (h *Handler) AppCloudSyncRenameDevice(c *gin.Context) {
	appID, userID, _, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	var req CloudSyncRenameRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.cloudStorage.RenameDevice(c.Request.Context(), appID, userID, c.Param("deviceId"), req.Name); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", nil)
}

// AppCloudSyncRemoveDevice 移除一台设备（腾出名额）。
func (h *Handler) AppCloudSyncRemoveDevice(c *gin.Context) {
	appID, userID, _, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	if err := h.cloudStorage.RemoveDevice(c.Request.Context(), appID, userID, c.Param("deviceId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", nil)
}

// AppCloudSyncPush 推一批改动：逐条最后写入者胜，被拒的交回云端那一版。
func (h *Handler) AppCloudSyncPush(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	var req CloudSyncPushRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	changes := make([]cloudstorage.SyncChange, 0, len(req.Changes))
	for _, item := range req.Changes {
		change := cloudstorage.SyncChange{Collection: item.Collection, ID: item.ID, Payload: item.Payload, Deleted: item.Deleted}
		switch {
		case item.UpdatedAt != nil:
			change.UpdatedAt = *item.UpdatedAt
		case item.UpdatedAtMs > 0:
			change.UpdatedAt = time.UnixMilli(item.UpdatedAtMs)
		}
		changes = append(changes, change)
	}
	result, err := h.cloudStorage.Push(c.Request.Context(), appID, userID, deviceID, changes)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AppCloudSyncChanges 增量拉取：seq 大于 since 的记录（含墓碑）。
func (h *Handler) AppCloudSyncChanges(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	var query CloudSyncChangesQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var collections []string
	if value := strings.TrimSpace(query.Collections); value != "" {
		collections = strings.Split(value, ",")
	}
	page, err := h.cloudStorage.Changes(c.Request.Context(), cloudstorage.SyncChangesQuery{
		AppID: appID, UserID: userID, Since: query.Since, Collections: collections, Limit: query.Limit,
	}, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppCloudSyncRecords 浏览同步记录（查看别的设备同步了什么）。
func (h *Handler) AppCloudSyncRecords(c *gin.Context) {
	appID, userID, _, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	var query CloudSyncRecordsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.cloudStorage.Records(c.Request.Context(), cloudstorage.SyncRecordQuery{
		AppID: appID, UserID: userID, Collection: strings.TrimSpace(query.Collection), Domain: strings.TrimSpace(query.Domain),
		DeviceID: strings.TrimSpace(query.DeviceID), Keyword: query.Keyword, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppCloudSyncRecord 一条记录（含墓碑）。
func (h *Handler) AppCloudSyncRecord(c *gin.Context) {
	appID, userID, _, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	record, err := h.cloudStorage.Record(c.Request.Context(), appID, userID, c.Param("collection"), c.Param("recordId"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", record)
}

// AppCloudSyncClearCollection 清空一个集合（别的设备拉到墓碑后跟着删）。
func (h *Handler) AppCloudSyncClearCollection(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	deleted, err := h.cloudStorage.ClearCollection(c.Request.Context(), appID, userID, c.Param("collection"), deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"deleted": deleted})
}

// AppCloudSyncCreateHandoff 把一条记录接力到另一台设备。
func (h *Handler) AppCloudSyncCreateHandoff(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	var req CloudSyncHandoffRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	handoff, err := h.cloudStorage.CreateHandoff(c.Request.Context(), appID, userID, service.CloudHandoffInput{
		FromDeviceID: deviceID, ToDeviceID: strings.TrimSpace(req.ToDeviceID), Collection: strings.TrimSpace(req.Collection),
		RecordID: strings.TrimSpace(req.RecordID), Payload: req.Payload,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", handoff)
}

// AppCloudSyncHandoffs 发给当前设备、还没领取的接力。
func (h *Handler) AppCloudSyncHandoffs(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	items, err := h.cloudStorage.PendingHandoffs(c.Request.Context(), appID, userID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AppCloudSyncAckHandoff 领取（打开或忽略）一次接力。
func (h *Handler) AppCloudSyncAckHandoff(c *gin.Context) {
	appID, userID, deviceID, ok := h.cloudSyncSession(c)
	if !ok {
		return
	}
	id, err := pathInt64(c, "handoffId")
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "handoffId 无效")
		return
	}
	if err := h.cloudStorage.AckHandoff(c.Request.Context(), appID, userID, id, deviceID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", nil)
}

// ── 管理端 ──

// AdminCloudStorageUserSyncRecords 某个用户的同步记录（按集合 / 设备筛）。
func (h *Handler) AdminCloudStorageUserSyncRecords(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	var query CloudSyncRecordsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.cloudStorage.AdminSyncRecords(c.Request.Context(), cloudstorage.SyncRecordQuery{
		AppID: appID, UserID: userID, Collection: strings.TrimSpace(query.Collection), Domain: strings.TrimSpace(query.Domain),
		DeviceID: strings.TrimSpace(query.DeviceID), Keyword: query.Keyword, Page: query.Page, Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminRemoveCloudStorageUserDevice 移除某个用户的一台同步设备。
func (h *Handler) AdminRemoveCloudStorageUserDevice(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	if err := h.cloudStorage.AdminRemoveDevice(c.Request.Context(), appID, userID, c.Param("deviceId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", nil)
}
