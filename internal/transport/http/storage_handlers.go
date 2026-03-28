package httptransport

import (
	storagedomain "aegis/internal/domain/storage"
	"aegis/pkg/response"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminAppStorageConfigList(c *gin.Context) {
	h.adminStorageConfigList(c, storagedomain.ScopeApp)
}
func (h *Handler) AdminGlobalStorageConfigList(c *gin.Context) {
	h.adminStorageConfigList(c, storagedomain.ScopeGlobal)
}

func (h *Handler) adminStorageConfigList(c *gin.Context, scope string) {
	var req AdminStorageConfigListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.storage.ListConfigs(c.Request.Context(), scope, optionalAppIDForScope(scope, req.AppID), req.Provider)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminAppStorageConfigDetail(c *gin.Context) {
	h.adminStorageConfigDetail(c, storagedomain.ScopeApp)
}
func (h *Handler) AdminGlobalStorageConfigDetail(c *gin.Context) {
	h.adminStorageConfigDetail(c, storagedomain.ScopeGlobal)
}

func (h *Handler) adminStorageConfigDetail(c *gin.Context, scope string) {
	var req AdminStorageConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.storage.Detail(c.Request.Context(), scope, optionalAppIDForScope(scope, req.AppID), req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppStorageConfigCreate(c *gin.Context) {
	h.adminStorageConfigSave(c, storagedomain.ScopeApp, 0)
}
func (h *Handler) AdminGlobalStorageConfigCreate(c *gin.Context) {
	h.adminStorageConfigSave(c, storagedomain.ScopeGlobal, 0)
}

func (h *Handler) AdminAppStorageConfigUpdate(c *gin.Context) {
	var req AdminStorageConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminStorageConfigSaveWithReq(c, storagedomain.ScopeApp, req, req.ConfigID)
}

func (h *Handler) AdminGlobalStorageConfigUpdate(c *gin.Context) {
	var req AdminStorageConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminStorageConfigSaveWithReq(c, storagedomain.ScopeGlobal, req, req.ConfigID)
}

func (h *Handler) adminStorageConfigSave(c *gin.Context, scope string, id int64) {
	var req AdminStorageConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminStorageConfigSaveWithReq(c, scope, req, id)
}

func (h *Handler) adminStorageConfigSaveWithReq(c *gin.Context, scope string, req AdminStorageConfigSaveRequest, id int64) {
	item, err := h.storage.Save(c.Request.Context(), storagedomain.ConfigMutation{
		ID:            id,
		Scope:         scope,
		AppID:         optionalAppIDForScope(scope, req.AppID),
		Provider:      maybeString(req.Provider),
		ConfigName:    maybeString(req.ConfigName),
		AccessMode:    maybeString(req.AccessMode),
		Enabled:       req.Enabled,
		IsDefault:     req.IsDefault,
		ProxyDownload: req.ProxyDownload,
		BaseURL:       maybeString(req.BaseURL),
		RootPath:      maybeString(req.RootPath),
		Description:   maybeString(req.Description),
		ConfigData:    req.ConfigData,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) AdminAppStorageConfigDelete(c *gin.Context) {
	h.adminStorageConfigDelete(c, storagedomain.ScopeApp)
}
func (h *Handler) AdminGlobalStorageConfigDelete(c *gin.Context) {
	h.adminStorageConfigDelete(c, storagedomain.ScopeGlobal)
}

func (h *Handler) adminStorageConfigDelete(c *gin.Context, scope string) {
	var req AdminStorageConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.storage.Delete(c.Request.Context(), scope, optionalAppIDForScope(scope, req.AppID), req.ConfigID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminAppStorageConfigTest(c *gin.Context) {
	h.adminStorageConfigTest(c, storagedomain.ScopeApp)
}
func (h *Handler) AdminGlobalStorageConfigTest(c *gin.Context) {
	h.adminStorageConfigTest(c, storagedomain.ScopeGlobal)
}

func (h *Handler) adminStorageConfigTest(c *gin.Context, scope string) {
	var req AdminStorageConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.storage.TestConfig(c.Request.Context(), scope, optionalAppIDForScope(scope, req.AppID), req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试完成", result)
}

func (h *Handler) StorageUpload(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()
	metadata := map[string]string{}
	if raw := strings.TrimSpace(c.PostForm("metadata")); raw != "" {
		_ = json.Unmarshal([]byte(raw), &metadata)
	}
	uid := session.UserID
	result, err := h.storage.Upload(c.Request.Context(), session, storagedomain.UploadInput{
		ConfigName:    strings.TrimSpace(c.PostForm("config_name")),
		ObjectKey:     strings.TrimSpace(c.PostForm("object_key")),
		FileName:      httpFirstNonEmpty(strings.TrimSpace(c.PostForm("file_name")), file.Filename),
		ContentType:   httpFirstNonEmpty(strings.TrimSpace(c.PostForm("content_type")), file.Header.Get("Content-Type")),
		CacheControl:  strings.TrimSpace(c.PostForm("cache_control")),
		ContentLength: file.Size,
		Metadata:      metadata,
		Content:       opened,
		UploadedBy:    &uid,
		UploaderType:  "user",
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "上传成功", result)
}

func (h *Handler) StorageObjectLink(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req StorageObjectLinkRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, ticketID, err := h.storage.CreateObjectLink(c.Request.Context(), session, storagedomain.LinkRequest{
		AppID:      session.AppID,
		ConfigName: req.ConfigName,
		ObjectKey:  req.ObjectKey,
		Download:   req.Download,
		FileName:   req.FileName,
		ExpiresIn:  time.Duration(req.ExpiresIn) * time.Second,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if ticketID != "" {
		result.URL = proxyURLFromRequest(c.Request, ticketID)
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) StorageProxyDownload(c *gin.Context) {
	ticket, _, reader, err := h.storage.OpenProxyObject(c.Request.Context(), c.Param("ticket"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	defer reader.Body.Close()
	contentType := reader.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(reader.FileName)))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	if reader.CacheControl != "" {
		c.Header("Cache-Control", reader.CacheControl)
	}
	if reader.ETag != "" {
		c.Header("ETag", reader.ETag)
	}
	if reader.Size > 0 {
		c.Header("Content-Length", strconv.FormatInt(reader.Size, 10))
	}
	if reader.LastModified != nil && !reader.LastModified.IsZero() {
		c.Header("Last-Modified", reader.LastModified.UTC().Format(http.TimeFormat))
	}
	if ticket != nil && ticket.Download {
		fileName := strings.TrimSpace(httpFirstNonEmpty(reader.FileName, ticket.FileName, filepath.Base(ticket.ObjectKey)))
		if fileName == "" {
			fileName = "download"
		}
		c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(fileName))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader.Body); err != nil {
		c.Error(err)
	}
}

func optionalAppIDForScope(scope string, appID int64) *int64 {
	if scope == storagedomain.ScopeApp && appID > 0 {
		return &appID
	}
	return nil
}

func httpFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func proxyURLFromRequest(r *http.Request, ticketID string) string {
	if r == nil {
		return "/api/storage/proxy/" + url.PathEscape(ticketID)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + "/api/storage/proxy/" + url.PathEscape(ticketID)
}
