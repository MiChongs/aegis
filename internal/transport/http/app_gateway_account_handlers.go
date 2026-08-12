package httptransport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	authprotocol "aegis/internal/domain/authprotocol"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 应用接入网关的账户与内容部分 —— /api/v1/apps/{appKey}/* 的其余接口。
//
// 认证生命周期在 app_gateway_handlers.go；这里是登录之后客户端真正要用的东西：
// 资料、安全、会话、签到、积分、通知、钱包、支付、上传、内容。
//
// 绝大多数 handler 从 Bearer 令牌里的 appid 取应用，直接注册即可复用；
// 少数**登录之前**就要调用的接口（找回密码、邮箱验证码、Passkey 登录参数、
// 版本检查、Banner/公告）沿用「请求里带 appid」的老约定，由本文件的适配器
// 把路径上的应用注入进去 —— 网关命名空间下应用由路径唯一确定，
// 让客户端再传一次 appid 等于给了它传错的机会。

// injectGatewayAppID 把路径上的应用写进请求，使旧 handler 无需改动即可复用。
//
// 注入位置随方法而定：无请求体的方法进 query，其余进 JSON 对象。
// 请求里已经带了 appid 时**不覆盖**，而是要求它与路径一致 —— 静默改写会让
// 「路径写 A、body 写 B」这种明显的接入错误变成一个查不出来的行为差异。
func (h *Handler) injectGatewayAppID(c *gin.Context) bool {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return false
	}
	appID := app.ID

	if authprotocol.BodylessMethod(c.Request.Method) {
		query := c.Request.URL.Query()
		if existing := strings.TrimSpace(query.Get("appid")); existing != "" {
			if existing != strconv.FormatInt(appID, 10) {
				response.Error(c, http.StatusBadRequest, 40084, "查询参数 appid 与路径上的应用不一致")
				return false
			}
			return true
		}
		query.Set("appid", strconv.FormatInt(appID, 10))
		c.Request.URL.RawQuery = query.Encode()
		return true
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请求体读取失败")
		return false
	}
	payload := map[string]json.RawMessage{}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "请求体必须是 JSON 对象")
			return false
		}
	}
	if raw, exists := payload["appid"]; exists {
		var declared int64
		if err := json.Unmarshal(raw, &declared); err != nil || declared != appID {
			response.Error(c, http.StatusBadRequest, 40084, "请求体中的 appid 与路径上的应用不一致")
			return false
		}
	} else {
		payload["appid"] = json.RawMessage(strconv.FormatInt(appID, 10))
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 50000, "请求体改写失败")
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(rewritten))
	c.Request.ContentLength = int64(len(rewritten))
	c.Request.Header.Set("Content-Type", "application/json")
	return true
}

// withGatewayAppID 生成一个「先注入应用再转交」的适配器。
func (h *Handler) withGatewayAppID(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.injectGatewayAppID(c) {
			return
		}
		next(c)
	}
}

// ─────────────────────────────────────────────────────────────────────
// 登录前就要用的接口：应用由路径注入
// ─────────────────────────────────────────────────────────────────────

func (h *Handler) AppEmailCode(c *gin.Context)      { h.withGatewayAppID(h.SendEmailCode)(c) }
func (h *Handler) AppEmailVerify(c *gin.Context)    { h.withGatewayAppID(h.VerifyEmailCode)(c) }
func (h *Handler) AppPasswordForgot(c *gin.Context) { h.withGatewayAppID(h.SendPasswordResetEmail)(c) }
func (h *Handler) AppPasswordResetVerify(c *gin.Context) {
	h.withGatewayAppID(h.VerifyResetToken)(c)
}
func (h *Handler) AppPasskeyOptions(c *gin.Context) { h.withGatewayAppID(h.PasskeyAuthOptions)(c) }
func (h *Handler) AppPasskeyLogin(c *gin.Context)   { h.withGatewayAppID(h.PasskeyLogin)(c) }

// ─────────────────────────────────────────────────────────────────────
// 免登录的内容接口
// ─────────────────────────────────────────────────────────────────────

func (h *Handler) AppBanners(c *gin.Context)      { h.withGatewayAppID(h.UserBanner)(c) }
func (h *Handler) AppNotices(c *gin.Context)      { h.withGatewayAppID(h.UserNotice)(c) }
func (h *Handler) AppVersionCheck(c *gin.Context) { h.withGatewayAppID(h.CheckVersion)(c) }

// AppBannerClick 轮播图点击上报。
//
// 没有对应的旧 handler 可以复用，因此直接从路径取应用 —— 这条接口是新加的，
// 不背「请求里带 appid」那套历史包袱。免登录：Banner 本来就在登录前展示。
func (h *Handler) AppBannerClick(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	bannerID, err := pathInt64(c, "bannerId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	if err := h.app.TrackBannerClick(c.Request.Context(), app.ID, bannerID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上报成功", gin.H{"id": bannerID})
}
