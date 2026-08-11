package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	functiondomain "aegis/internal/domain/appfunction"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

type createAppFunctionRequest struct {
	Name             string   `json:"name" binding:"required"`
	Description      string   `json:"description"`
	Runtime          string   `json:"runtime" binding:"required"`
	Capabilities     []string `json:"capabilities"`
	TimeoutMs        int      `json:"timeoutMs"`
	MaxRequestBytes  int      `json:"maxRequestBytes"`
	MaxResponseBytes int      `json:"maxResponseBytes"`
}

type updateAppFunctionRequest struct {
	Description      *string  `json:"description"`
	Status           *string  `json:"status"`
	Capabilities     []string `json:"capabilities"`
	TimeoutMs        *int     `json:"timeoutMs"`
	MaxRequestBytes  *int     `json:"maxRequestBytes"`
	MaxResponseBytes *int     `json:"maxResponseBytes"`
}

type createAppFunctionVersionRequest struct {
	Version           string `json:"version" binding:"required"`
	EndpointURL       string `json:"endpointUrl"`
	ResponsePublicKey string `json:"responsePublicKey"`
	WASMBase64        string `json:"wasmBase64"`
	// Source 是 script 运行时的脚本正文，只在管理端出现，永不下发给接入方
	Source string `json:"source"`
}

type invokeAppFunctionRequest struct {
	EventID string          `json:"eventId"`
	Input   json.RawMessage `json:"input"`
}

type createAppFunctionKeyRequest struct {
	Name string `json:"name" binding:"required"`
}

// AppFunctionSigningKey 返回 Aegis 对远程函数请求签名所使用的 Ed25519 公钥。
func (h *Handler) AppFunctionSigningKey(c *gin.Context) {
	if h.appFunction == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "应用函数服务不可用")
		return
	}
	response.Success(c, 200, "ok", gin.H{
		"algorithm":            "Ed25519",
		"publicKey":            h.appFunction.SigningPublicKey(),
		"requestSigningInput":  "timestamp\\neventId\\ncontentSha256",
		"responseSigningInput": "eventId\\ncontentSha256",
	})
}

func (h *Handler) AdminCreateAppFunction(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req createAppFunctionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.appFunction.CreateFunction(c.Request.Context(), functiondomain.CreateFunctionInput{
		AppID: appID, Name: req.Name, Description: req.Description, Runtime: req.Runtime,
		Capabilities: req.Capabilities, TimeoutMs: req.TimeoutMs,
		MaxRequestBytes: req.MaxRequestBytes, MaxResponseBytes: req.MaxResponseBytes,
		CreatedBy: currentAdminID(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "应用函数已创建", item)
}

func (h *Handler) AdminListAppFunctions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.appFunction.ListFunctions(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) AdminGetAppFunction(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.appFunction.GetFunction(c.Request.Context(), appID, c.Param("functionName"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", item)
}

func (h *Handler) AdminUpdateAppFunction(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req updateAppFunctionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.appFunction.UpdateFunction(c.Request.Context(), appID, c.Param("functionName"),
		functiondomain.UpdateFunctionInput{
			Description: req.Description, Status: req.Status, Capabilities: req.Capabilities,
			TimeoutMs: req.TimeoutMs, MaxRequestBytes: req.MaxRequestBytes,
			MaxResponseBytes: req.MaxResponseBytes,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "应用函数已更新", item)
}

func (h *Handler) AdminDeleteAppFunction(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if err := h.appFunction.DeleteFunction(c.Request.Context(), appID, c.Param("functionName")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "应用函数已删除", gin.H{"deleted": true})
}

func (h *Handler) AdminCreateAppFunctionVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3<<20)
	var req createAppFunctionVersionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	function, err := h.appFunction.GetFunction(c.Request.Context(), appID, c.Param("functionName"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	var module []byte
	if strings.TrimSpace(req.WASMBase64) != "" {
		module, err = base64.StdEncoding.DecodeString(req.WASMBase64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40096, "wasmBase64 不是有效 Base64")
			return
		}
	}
	item, err := h.appFunction.CreateVersion(c.Request.Context(), function, functiondomain.CreateVersionInput{
		Version: req.Version, EndpointURL: strings.TrimSpace(req.EndpointURL),
		ResponsePublicKey: strings.TrimSpace(req.ResponsePublicKey), WASMModule: module,
		Source:    req.Source,
		CreatedBy: currentAdminID(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "函数版本已创建", item)
}

func (h *Handler) AdminListAppFunctionVersions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.appFunction.ListVersions(c.Request.Context(), appID, c.Param("functionName"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) AdminActivateAppFunctionVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if err := h.appFunction.ActivateVersion(c.Request.Context(), appID,
		c.Param("functionName"), c.Param("version")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "函数版本已激活", gin.H{"version": c.Param("version")})
}

func (h *Handler) AdminInvokeAppFunction(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req invokeAppFunctionRequest
	if err := bindLimitedJSON(c, &req, 1<<20); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID := currentAdminID(c)
	result, err := h.appFunction.Invoke(c.Request.Context(), appID, c.Param("functionName"),
		req.EventID, req.Input, functiondomain.Caller{Type: "admin", AdminID: adminID})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "函数调用成功", result)
}

func (h *Handler) AdminListAppFunctionInvocations(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.appFunction.ListInvocations(c.Request.Context(), appID, c.Param("functionName"), limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) AdminCreateAppFunctionKey(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req createAppFunctionKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.appFunction.CreateKey(c.Request.Context(), appID, req.Name, currentAdminID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "函数密钥已创建，请立即安全保存", item)
}

func (h *Handler) AdminListAppFunctionKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.appFunction.ListKeys(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) AdminRevokeAppFunctionKey(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	keyID, err := strconv.ParseInt(c.Param("keyId"), 10, 64)
	if err != nil || keyID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "keyId 无效")
		return
	}
	if err := h.appFunction.RevokeKey(c.Request.Context(), appID, keyID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "函数密钥已撤销", gin.H{"revoked": true})
}

// InvokeAppFunction 供已登录的接入应用用户调用，只允许调用自身 App 下的函数。
func (h *Handler) InvokeAppFunction(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	caller, ok := h.resolveAppFunctionCaller(c, appID)
	if !ok {
		return
	}
	var req invokeAppFunctionRequest
	if err := bindLimitedJSON(c, &req, 1<<20); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.appFunction.Invoke(c.Request.Context(), appID, c.Param("functionName"),
		req.EventID, req.Input, caller)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "函数调用成功", result)
}

func (h *Handler) resolveAppFunctionCaller(c *gin.Context, appID int64) (functiondomain.Caller, bool) {
	if secret := strings.TrimSpace(c.GetHeader("X-Aegis-Function-Key")); secret != "" {
		key, err := h.appFunction.AuthenticateKey(c.Request.Context(), appID, secret)
		if err != nil {
			h.writeError(c, err)
			return functiondomain.Caller{}, false
		}
		keyID := key.ID
		return functiondomain.Caller{Type: "app", KeyID: &keyID}, true
	}
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(authorization) < 8 || !strings.EqualFold(authorization[:7], "Bearer ") {
		response.Error(c, http.StatusUnauthorized, 40100, "缺少用户令牌或函数密钥")
		return functiondomain.Caller{}, false
	}
	session, err := h.auth.ValidateAccessToken(c.Request.Context(), strings.TrimSpace(authorization[7:]))
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 40100, "用户令牌无效")
		return functiondomain.Caller{}, false
	}
	if session.AppID != appID {
		response.Error(c, http.StatusForbidden, 40390, "令牌不属于当前应用")
		return functiondomain.Caller{}, false
	}
	userID := session.UserID
	return functiondomain.Caller{Type: "user", UserID: &userID}, true
}

func currentAdminID(c *gin.Context) *int64 {
	if session, ok := adminAccessSession(c); ok && session.AdminID > 0 {
		value := session.AdminID
		return &value
	}
	return nil
}

func bindLimitedJSON(c *gin.Context, target any, limit int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}
