package httptransport

// 应用公开信息：资料、轮播图、公告、版本检查。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	authdomain "aegis/internal/domain/auth"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AppPublic(c *gin.Context) {
	var query AppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.GetApp(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	// 获取传输加密配置（公开视图，不含私钥）
	var encryptionView any
	if enc, err := h.app.GetTransportEncryption(c.Request.Context(), query.AppID); err == nil && enc != nil {
		enc.SecretHint = ""
		encryptionView = enc
	}

	response.Success(c, 200, "获取成功", gin.H{
		"id":             item.ID,
		"name":           item.Name,
		"status":         item.Status,
		"registerStatus": item.RegisterStatus,
		"loginStatus":    item.LoginStatus,
		"policy":         h.app.ResolvePolicy(item),
		"settings":       h.app.PublicSettings(item),
		"encryption":     encryptionView,
	})
}

func (h *Handler) UserBanner(c *gin.Context) {
	var query AppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.app.GetBanners(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) UserNotice(c *gin.Context) {
	var query AppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.app.GetNotices(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) CheckVersion(c *gin.Context) {
	var query VersionCheckQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var session *authdomain.Session
	if value, ok := c.Get("auth.session"); ok {
		session, _ = value.(*authdomain.Session)
	}
	result, err := h.version.CheckForUpdate(c.Request.Context(), query.AppID, query.VersionCode, query.Platform, session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result == nil || result.Version == nil {
		response.Error(c, http.StatusNotFound, 40430, "暂无新版本信息")
		return
	}
	response.Success(c, 200, "有新版本", result)
}
