package httptransport

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	cloudstorage "aegis/internal/domain/cloudstorage"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 用户云存储：用户端（网关 /cloud/*）与管理端（应用级配置 / 概览 / 用户列表，
// 以及挂在单个用户之下的条目管理）。业务规则全部在 CloudStorageService。

// ── 用户端 DTO ──

// CloudItemListQuery 条目列表。status 取 active（默认）/ deleted（回收站）/ all。
type CloudItemListQuery struct {
	Namespace string `json:"namespace" form:"namespace"`
	Keyword   string `json:"keyword" form:"keyword"`
	Status    string `json:"status" form:"status"`
	Page      int    `json:"page" form:"page"`
	Limit     int    `json:"limit" form:"limit"`
}

// CloudItemReadQuery 读取条目。revision 省略或为 0 读当前修订。
type CloudItemReadQuery struct {
	Revision int64 `json:"revision" form:"revision"`
}

// CloudItemPutRequest 写入条目。
//
// content 的形态由 encoding 决定：json 时是任意 JSON 值，text 时是字符串，
// base64 时是 base64 字符串。ifRevision 是乐观并发的前提：省略表示无条件覆盖，
// 0 表示「只在条目不存在时创建」，其余值必须等于服务端当前修订，否则 40965。
type CloudItemPutRequest struct {
	Content     json.RawMessage `json:"content" binding:"required"`
	Encoding    string          `json:"encoding"`
	ContentType string          `json:"contentType"`
	Metadata    map[string]any  `json:"metadata"`
	IfRevision  *int64          `json:"ifRevision"`
	DeviceID    string          `json:"deviceId"`
}

// CloudItemDeleteQuery 删除条目。permanent 为真时跳过回收站直接清除。
type CloudItemDeleteQuery struct {
	IfRevision *int64 `json:"ifRevision" form:"ifRevision"`
	Permanent  bool   `json:"permanent" form:"permanent"`
}

// CloudItemRollbackRequest 回滚到某个修订（生成一个新修订）。
type CloudItemRollbackRequest struct {
	Revision   int64  `json:"revision" binding:"required"`
	IfRevision *int64 `json:"ifRevision"`
	DeviceID   string `json:"deviceId"`
}

// CloudItemLinkRequest 下载地址。revision 省略读当前修订。
type CloudItemLinkRequest struct {
	Revision int64 `json:"revision"`
	Download bool  `json:"download"`
}

// ── 管理端 DTO ──

// SaveCloudStorageConfigRequest 保存应用级配置。
type SaveCloudStorageConfigRequest struct {
	Enabled            bool                     `json:"enabled"`
	StorageConfigName  string                   `json:"storageConfigName"`
	QuotaBytes         int64                    `json:"quotaBytes"`
	MaxItemBytes       int64                    `json:"maxItemBytes"`
	MaxItems           int                      `json:"maxItems"`
	MaxRevisions       int                      `json:"maxRevisions"`
	TrashRetentionDays int                      `json:"trashRetentionDays"`
	RestrictNamespaces bool                     `json:"restrictNamespaces"`
	Namespaces         []cloudstorage.Namespace `json:"namespaces"`
}

// CloudStorageUserListQuery 管理端用户列表。sort 取 usage（默认）/ items / recent；
// frozen 取 true / false，省略表示不筛。
type CloudStorageUserListQuery struct {
	Keyword string `json:"keyword" form:"keyword"`
	Sort    string `json:"sort" form:"sort"`
	Frozen  string `json:"frozen" form:"frozen"`
	Page    int    `json:"page" form:"page"`
	Limit   int    `json:"limit" form:"limit"`
}

// SaveCloudStorageUserRequest 单用户设置。quotaBytes 为 null 表示沿用应用默认配额。
type SaveCloudStorageUserRequest struct {
	QuotaBytes   *int64 `json:"quotaBytes"`
	Frozen       bool   `json:"frozen"`
	FrozenReason string `json:"frozenReason"`
	Note         string `json:"note"`
}

// AdminCloudRollbackRequest 管理端回滚。
type AdminCloudRollbackRequest struct {
	Revision int64 `json:"revision" binding:"required"`
}

// AdminCloudItemDeleteQuery 管理端删除。
type AdminCloudItemDeleteQuery struct {
	Permanent bool `json:"permanent" form:"permanent"`
}

// ── 用户端 ──

// AppCloudStatus 云存储状态：开没开、能不能写、用了多少、限制是什么。
func (h *Handler) AppCloudStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	status, err := h.cloudStorage.Status(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", status)
}

// AppCloudItems 我的条目列表（不含内容）。
func (h *Handler) AppCloudItems(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query CloudItemListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.cloudStorage.ListItems(c.Request.Context(), cloudstorage.ItemQuery{
		AppID:     session.AppID,
		UserID:    session.UserID,
		Namespace: strings.TrimSpace(query.Namespace),
		Keyword:   query.Keyword,
		Status:    strings.TrimSpace(query.Status),
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppCloudItem 读取条目与内容。
func (h *Handler) AppCloudItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query CloudItemReadQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cloudStorage.GetItem(c.Request.Context(), session.AppID, session.UserID,
		c.Param("namespace"), c.Param("key"), query.Revision)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", item)
}

// AppPutCloudItem 写入条目（JSON 请求体）。
func (h *Handler) AppPutCloudItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CloudItemPutRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	encoding := strings.TrimSpace(req.Encoding)
	if encoding == "" {
		encoding = cloudstorage.EncodingJSON
	}
	data, err := cloudstorage.DecodeContent(encoding, req.Content)
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 42262, "内容与声明的编码不符")
		return
	}
	if int64(len(data)) > cloudstorage.JSONWriteLimit {
		response.Error(c, http.StatusRequestEntityTooLarge, 41361, "内容过大，请改用文件上传接口")
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	item, err := h.cloudStorage.PutItem(c.Request.Context(), service.CloudWriteInput{
		AppID:       session.AppID,
		UserID:      session.UserID,
		Namespace:   c.Param("namespace"),
		Key:         c.Param("key"),
		Data:        data,
		Encoding:    encoding,
		ContentType: req.ContentType,
		Metadata:    req.Metadata,
		DeviceID:    deviceID,
		IfRevision:  req.IfRevision,
		Source:      cloudstorage.SourceWrite,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", item)
}

// AppUploadCloudItem 以 multipart 上传条目内容（大文件 / 二进制）。
//
// 表单字段：file（必填）、encoding（默认 base64，即按原始字节存取）、contentType、
// metadata（JSON 对象字符串）、ifRevision、deviceId。
func (h *Handler) AppUploadCloudItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	if file.Size > cloudstorage.MaxItemBytesCap {
		response.Error(c, http.StatusRequestEntityTooLarge, 41361, "单个条目不能超过 "+cloudstorage.FormatBytes(cloudstorage.MaxItemBytesCap))
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, cloudstorage.MaxItemBytesCap+1))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	encoding := strings.TrimSpace(c.PostForm("encoding"))
	if encoding == "" {
		encoding = cloudstorage.EncodingBase64
	}
	if err := cloudstorage.ValidateUploadEncoding(encoding, data); err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 42262, "内容与声明的编码不符")
		return
	}
	var metadata map[string]any
	if raw := strings.TrimSpace(c.PostForm("metadata")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			response.Error(c, http.StatusUnprocessableEntity, 42263, "元数据需为 JSON 对象")
			return
		}
	}
	var ifRevision *int64
	if raw := strings.TrimSpace(c.PostForm("ifRevision")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			response.Error(c, http.StatusBadRequest, 40000, "ifRevision 无效")
			return
		}
		ifRevision = &value
	}
	contentType := strings.TrimSpace(c.PostForm("contentType"))
	if contentType == "" {
		contentType = strings.TrimSpace(file.Header.Get("Content-Type"))
	}
	deviceID, _ := h.resolveGatewayDevice(c, c.PostForm("deviceId"), "")
	item, err := h.cloudStorage.PutItem(c.Request.Context(), service.CloudWriteInput{
		AppID:       session.AppID,
		UserID:      session.UserID,
		Namespace:   c.Param("namespace"),
		Key:         c.Param("key"),
		Data:        data,
		Encoding:    encoding,
		ContentType: contentType,
		Metadata:    metadata,
		DeviceID:    deviceID,
		IfRevision:  ifRevision,
		Source:      cloudstorage.SourceUpload,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已上传", item)
}

// AppDeleteCloudItem 删除条目（默认进回收站）。
func (h *Handler) AppDeleteCloudItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query CloudItemDeleteQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cloudStorage.DeleteItem(c.Request.Context(), session.AppID, session.UserID,
		c.Param("namespace"), c.Param("key"), query.IfRevision, query.Permanent)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已移入回收站"
	if item.PurgeAt != nil && item.RevisionCount == 0 {
		message = "已永久删除"
	}
	response.Success(c, http.StatusOK, message, item)
}

// AppRestoreCloudItem 从回收站恢复条目。
func (h *Handler) AppRestoreCloudItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	item, err := h.cloudStorage.RestoreItem(c.Request.Context(), session.AppID, session.UserID,
		c.Param("namespace"), c.Param("key"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已恢复", item)
}

// AppCloudItemRevisions 条目的留存修订。
func (h *Handler) AppCloudItemRevisions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.cloudStorage.ListRevisions(c.Request.Context(), session.AppID, session.UserID,
		c.Param("namespace"), c.Param("key"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AppRollbackCloudItem 回滚到某个修订。
func (h *Handler) AppRollbackCloudItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CloudItemRollbackRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	item, err := h.cloudStorage.Rollback(c.Request.Context(), session.AppID, session.UserID,
		c.Param("namespace"), c.Param("key"), req.Revision, req.IfRevision, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已回滚", item)
}

// AppCloudItemLink 条目内容的短时下载地址。
func (h *Handler) AppCloudItemLink(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CloudItemLinkRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, ticket, err := h.cloudStorage.ItemLink(c.Request.Context(), session.AppID, session.UserID,
		c.Param("namespace"), c.Param("key"), req.Revision, req.Download)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if ticket != "" {
		result.URL = proxyURLFromRequest(c.Request, ticket)
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// ── 管理端：应用级 ──

// AdminCloudStorageConfig 读取配置（含实际写往的存储与可选存储配置）。
func (h *Handler) AdminCloudStorageConfig(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cfg, err := h.cloudStorage.AdminConfig(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", cfg)
}

// AdminSaveCloudStorageConfig 保存配置。
func (h *Handler) AdminSaveCloudStorageConfig(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req SaveCloudStorageConfigRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	cfg, err := h.cloudStorage.SaveConfig(c.Request.Context(), cloudstorage.Config{
		AppID:              appID,
		Enabled:            req.Enabled,
		StorageConfigName:  req.StorageConfigName,
		QuotaBytes:         req.QuotaBytes,
		MaxItemBytes:       req.MaxItemBytes,
		MaxItems:           req.MaxItems,
		MaxRevisions:       req.MaxRevisions,
		TrashRetentionDays: req.TrashRetentionDays,
		RestrictNamespaces: req.RestrictNamespaces,
		Namespaces:         req.Namespaces,
	}, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "云存储配置已保存", cfg)
}

// AdminCloudStorageStats 概览与写入趋势。
func (h *Handler) AdminCloudStorageStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.cloudStorage.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// AdminCloudStorageUsers 用过云存储的用户，默认按用量从高到低。
func (h *Handler) AdminCloudStorageUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query CloudStorageUserListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var frozen *bool
	switch strings.TrimSpace(query.Frozen) {
	case "true":
		value := true
		frozen = &value
	case "false":
		value := false
		frozen = &value
	}
	page, err := h.cloudStorage.ListUsers(c.Request.Context(), cloudstorage.UserQuery{
		AppID:   appID,
		Keyword: query.Keyword,
		Sort:    strings.TrimSpace(query.Sort),
		Frozen:  frozen,
		Page:    query.Page,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminPurgeExpiredCloudTrash 立即清除过了保留期的回收站条目（平时由后台每小时做一次）。
func (h *Handler) AdminPurgeExpiredCloudTrash(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	purged, err := h.cloudStorage.PurgeExpired(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "清理完成", gin.H{"purged": purged})
}

// ── 管理端：单个用户 ──

func cloudUserParams(c *gin.Context, h *Handler) (int64, int64, bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return 0, 0, false
	}
	userID, err := pathInt64(c, "userId")
	if err != nil || userID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "userId 无效")
		return 0, 0, false
	}
	return appID, userID, true
}

func cloudItemParams(c *gin.Context, h *Handler) (int64, int64, int64, bool) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return 0, 0, 0, false
	}
	itemID, err := pathInt64(c, "itemId")
	if err != nil || itemID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "itemId 无效")
		return 0, 0, 0, false
	}
	return appID, userID, itemID, true
}

// AdminCloudStorageUser 单个用户的账目、限制与各命名空间用量。
func (h *Handler) AdminCloudStorageUser(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	view, err := h.cloudStorage.AdminUser(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", view)
}

// AdminSaveCloudStorageUser 保存单用户的配额覆盖、冻结与备注。
func (h *Handler) AdminSaveCloudStorageUser(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	var req SaveCloudStorageUserRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	view, err := h.cloudStorage.AdminSaveUser(c.Request.Context(), appID, userID, service.CloudUserOverrides{
		QuotaBytes:   req.QuotaBytes,
		Frozen:       req.Frozen,
		FrozenReason: req.FrozenReason,
		Note:         req.Note,
	}, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", view)
}

// AdminPurgeCloudStorageUser 清空该用户的全部云存储数据（不可恢复）。
func (h *Handler) AdminPurgeCloudStorageUser(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	purged, err := h.cloudStorage.AdminPurgeUser(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已清空", gin.H{"objects": purged})
}

// AdminCloudStorageUserItems 该用户的条目（含回收站，按 status 筛）。
func (h *Handler) AdminCloudStorageUserItems(c *gin.Context) {
	appID, userID, ok := cloudUserParams(c, h)
	if !ok {
		return
	}
	var query CloudItemListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.cloudStorage.AdminListItems(c.Request.Context(), cloudstorage.ItemQuery{
		AppID:     appID,
		UserID:    userID,
		Namespace: strings.TrimSpace(query.Namespace),
		Keyword:   query.Keyword,
		Status:    strings.TrimSpace(query.Status),
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminCloudStorageUserItem 读取条目内容（可指定修订）。
func (h *Handler) AdminCloudStorageUserItem(c *gin.Context) {
	appID, userID, itemID, ok := cloudItemParams(c, h)
	if !ok {
		return
	}
	var query CloudItemReadQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cloudStorage.AdminGetItem(c.Request.Context(), appID, userID, itemID, query.Revision)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", item)
}

// AdminCloudStorageUserItemRevisions 条目的留存修订。
func (h *Handler) AdminCloudStorageUserItemRevisions(c *gin.Context) {
	appID, userID, itemID, ok := cloudItemParams(c, h)
	if !ok {
		return
	}
	items, err := h.cloudStorage.AdminRevisions(c.Request.Context(), appID, userID, itemID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AdminRollbackCloudStorageUserItem 回滚到某个修订。
func (h *Handler) AdminRollbackCloudStorageUserItem(c *gin.Context) {
	appID, userID, itemID, ok := cloudItemParams(c, h)
	if !ok {
		return
	}
	var req AdminCloudRollbackRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.cloudStorage.AdminRollback(c.Request.Context(), appID, userID, itemID, req.Revision, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已回滚", item)
}

// AdminRestoreCloudStorageUserItem 从回收站恢复。
func (h *Handler) AdminRestoreCloudStorageUserItem(c *gin.Context) {
	appID, userID, itemID, ok := cloudItemParams(c, h)
	if !ok {
		return
	}
	item, err := h.cloudStorage.AdminRestore(c.Request.Context(), appID, userID, itemID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已恢复", item)
}

// AdminDeleteCloudStorageUserItem 删除条目：默认进回收站，permanent=true 直接清除。
func (h *Handler) AdminDeleteCloudStorageUserItem(c *gin.Context) {
	appID, userID, itemID, ok := cloudItemParams(c, h)
	if !ok {
		return
	}
	var query AdminCloudItemDeleteQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cloudStorage.AdminDelete(c.Request.Context(), appID, userID, itemID, query.Permanent)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已移入回收站"
	if query.Permanent {
		message = "已永久删除"
	}
	response.Success(c, http.StatusOK, message, item)
}

// AdminCloudStorageUserItemLink 条目内容的短时下载地址。
func (h *Handler) AdminCloudStorageUserItemLink(c *gin.Context) {
	appID, userID, itemID, ok := cloudItemParams(c, h)
	if !ok {
		return
	}
	var req CloudItemLinkRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, ticket, err := h.cloudStorage.AdminItemLink(c.Request.Context(), appID, userID, itemID, req.Revision, req.Download)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if ticket != "" {
		result.URL = proxyURLFromRequest(c.Request, ticket)
	}
	response.Success(c, http.StatusOK, "ok", result)
}
