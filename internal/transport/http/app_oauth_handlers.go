package httptransport

import (
	"net/http"
	"net/url"
	"strings"

	appdomain "aegis/internal/domain/app"
	oauthdomain "aegis/internal/domain/oauth"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ── 公开 / 用户端 ──

// AppOAuthPublicProviders 登录页拉取该应用可用的第三方登录渠道（不含任何凭据）。
func (h *Handler) AppOAuthPublicProviders(c *gin.Context) {
	if h.appOAuth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "第三方登录服务不可用")
		return
	}
	var query OAuthProvidersQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.appOAuth.PublicProviders(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=30")
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// OAuthBindURL 已登录用户发起第三方账号绑定的授权链接。
func (h *Handler) OAuthBindURL(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req OAuthBindURLRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := resolveDeviceInfo(c, req.DeviceID, req.Device, req.MarkCode)
	authURL, err := h.auth.BuildOAuthBindURL(c.Request.Context(), session.AppID, session.UserID, req.Provider, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取授权地址成功", gin.H{"url": authURL})
}

// ListMyOAuthBindings 当前登录用户已绑定的第三方账号。
func (h *Handler) ListMyOAuthBindings(c *gin.Context) {
	if h.appOAuth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "第三方登录服务不可用")
		return
	}
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	items, err := h.appOAuth.ListUserBindings(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// UnbindMyOAuthProvider 当前登录用户解绑第三方账号。
// 解绑最后一个登录方式会被拒绝（防止把自己锁在门外）。
func (h *Handler) UnbindMyOAuthProvider(c *gin.Context) {
	if h.appOAuth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "第三方登录服务不可用")
		return
	}
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if err := h.appOAuth.Unbind(c.Request.Context(), session.AppID, session.UserID, c.Param("provider"), false); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "解绑成功", gin.H{"unbound": true})
}

// ── 管理端 ──

// AdminOAuthProviderTemplates 内置渠道模板目录，供"添加渠道"一键预填。
func (h *Handler) AdminOAuthProviderTemplates(c *gin.Context) {
	if h.appOAuth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "第三方登录服务不可用")
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	response.Success(c, http.StatusOK, "获取成功", gin.H{
		"items":             h.appOAuth.Templates(),
		"callbackUrlPrefix": oauthCallbackPrefix(c),
	})
}

// AdminListAppOAuthProviders 渠道列表：应用级配置 + 平台级兜底渠道。
func (h *Handler) AdminListAppOAuthProviders(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	items, err := h.appOAuth.List(c.Request.Context(), app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{
		"items":             items,
		"callbackUrlPrefix": oauthCallbackPrefix(c),
	})
}

// AdminGetAppOAuthProvider 单个渠道详情。
func (h *Handler) AdminGetAppOAuthProvider(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	item, err := h.appOAuth.Get(c.Request.Context(), app.ID, c.Param("provider"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", item)
}

// AdminCreateAppOAuthProvider 新建渠道（provider 由请求体给出）。
func (h *Handler) AdminCreateAppOAuthProvider(c *gin.Context) {
	h.saveAppOAuthProvider(c, "", http.StatusCreated, "渠道已创建")
}

// AdminUpdateAppOAuthProvider 更新渠道（provider 取自路径）。
func (h *Handler) AdminUpdateAppOAuthProvider(c *gin.Context) {
	h.saveAppOAuthProvider(c, c.Param("provider"), http.StatusOK, "渠道已更新")
}

func (h *Handler) saveAppOAuthProvider(c *gin.Context, provider string, successStatus int, successMessage string) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	var req AppOAuthProviderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if strings.TrimSpace(provider) != "" {
		req.Provider = provider
	}
	if strings.TrimSpace(req.Provider) == "" {
		response.Error(c, http.StatusBadRequest, 40090, "缺少渠道标识")
		return
	}
	// 回调地址留空时按当前 API 域名自动补全，避免管理员手抄出错
	if strings.TrimSpace(req.RedirectURL) == "" {
		req.RedirectURL = oauthCallbackPrefix(c) + url.QueryEscape(strings.ToLower(strings.TrimSpace(req.Provider)))
	}
	saved, err := h.appOAuth.Save(c.Request.Context(), app.ID, oauthdomain.SaveInput{
		Provider: req.Provider, Kind: req.Kind, DisplayName: req.DisplayName,
		Icon: req.Icon, Color: req.Color, Enabled: req.Enabled,
		ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		ClearClientSecret: req.ClearClientSecret, RedirectURL: req.RedirectURL,
		AuthURL: req.AuthURL, TokenURL: req.TokenURL, UserInfoURL: req.UserInfoURL,
		Scopes: req.Scopes, TokenAuthStyle: req.TokenAuthStyle,
		UserInfoAuthStyle: req.UserInfoAuthStyle, ProfileMapping: req.ProfileMapping,
		ExtraAuthParams: req.ExtraAuthParams, AllowLogin: req.AllowLogin,
		AllowRegister: req.AllowRegister, AllowBind: req.AllowBind,
		SortOrder: req.SortOrder, Remark: req.Remark,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, successStatus, successMessage, saved)
}

// AdminSetAppOAuthProviderEnabled 一键启停。
func (h *Handler) AdminSetAppOAuthProviderEnabled(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	var req AppOAuthProviderEnabledRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.appOAuth.SetEnabled(c.Request.Context(), app.ID, c.Param("provider"), req.Enabled)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "渠道已停用"
	if req.Enabled {
		message = "渠道已启用"
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, message, item)
}

// AdminDeleteAppOAuthProvider 删除渠道配置（已有用户绑定不受影响）。
func (h *Handler) AdminDeleteAppOAuthProvider(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	if err := h.appOAuth.Delete(c.Request.Context(), app.ID, c.Param("provider")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "渠道已删除", gin.H{"deleted": true})
}

// AdminReorderAppOAuthProviders 重排登录页展示顺序。
func (h *Handler) AdminReorderAppOAuthProviders(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	var req AppOAuthProviderReorderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.appOAuth.Reorder(c.Request.Context(), app.ID, req.Providers)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "顺序已更新", gin.H{"items": items})
}

// AdminTestAppOAuthProvider 渠道自检：配置完整性 + 端点可达性 + 示例授权链接。
func (h *Handler) AdminTestAppOAuthProvider(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	result, err := h.appOAuth.Test(c.Request.Context(), app.ID, c.Param("provider"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "自检完成", result)
}

// AdminListAppOAuthBindings 管理端分页查看第三方账号绑定记录。
func (h *Handler) AdminListAppOAuthBindings(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	var query AppOAuthBindingQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.appOAuth.ListBindings(c.Request.Context(), oauthdomain.BindingQuery{
		AppID: app.ID, Provider: query.Provider, UserID: query.UserID,
		Keyword: query.Keyword, Page: query.Page, PageSize: query.PageSize,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", page)
}

// AdminDeleteAppOAuthBinding 管理端强制解绑。
func (h *Handler) AdminDeleteAppOAuthBinding(c *gin.Context) {
	app, ok := h.resolveAdminOAuthApp(c)
	if !ok {
		return
	}
	var req AdminUnbindOAuthRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.appOAuth.Unbind(c.Request.Context(), app.ID, req.UserID, req.Provider, req.Force); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "解绑成功", gin.H{"unbound": true})
}

// resolveAdminOAuthApp 把路径上的 appkey 解析成 App，并确保服务可用。
func (h *Handler) resolveAdminOAuthApp(c *gin.Context) (*appdomain.App, bool) {
	if h.appOAuth == nil || h.app == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "第三方登录服务不可用")
		return nil, false
	}
	app, err := h.app.GetAppByKey(c.Request.Context(), c.Param("appkey"))
	if err != nil {
		h.writeError(c, err)
		return nil, false
	}
	if app == nil {
		response.Error(c, http.StatusNotFound, 40470, "应用不存在")
		return nil, false
	}
	return app, true
}

// oauthCallbackPrefix 依据当前请求推导回调地址前缀，管理端可直接复制到服务商后台。
// 反代场景优先采信 X-Forwarded-Proto / X-Forwarded-Host（Gin 已按可信代理白名单过滤）。
func oauthCallbackPrefix(c *gin.Context) string {
	scheme := "http"
	if c.Request != nil && c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		if index := strings.Index(forwarded, ","); index > 0 {
			forwarded = forwarded[:index]
		}
		scheme = strings.ToLower(strings.TrimSpace(forwarded))
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" && c.Request != nil {
		host = c.Request.Host
	}
	if host == "" {
		return "/api/auth/oauth2/callback?provider="
	}
	return scheme + "://" + host + "/api/auth/oauth2/callback?provider="
}
