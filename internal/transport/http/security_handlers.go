package httptransport

import (
	"bytes"
	"encoding/json"
	"net/http"

	authdomain "aegis/internal/domain/auth"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) VerifySecondFactor(c *gin.Context) {
	if h.auth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "认证服务暂不可用")
		return
	}

	var req SecondFactorVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	result, err := h.auth.VerifySecondFactor(c.Request.Context(), req.ChallengeID, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "认证成功", result)
}

func (h *Handler) BeginTOTPEnrollment(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	result, err := h.security.BeginTOTPEnrollment(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) EnableTOTP(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	var req TOTPEnableRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	status, recovery, err := h.security.EnableTOTP(c.Request.Context(), session, req.EnrollmentID, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "启用成功", gin.H{"twoFactor": status, "recoveryCodes": recovery})
}

func (h *Handler) DisableTOTP(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	var req TOTPDisableRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	status, err := h.security.DisableTOTP(c.Request.Context(), session, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "关闭成功", status)
}

func (h *Handler) ListRecoveryCodes(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	summary, err := h.security.ListRecoveryCodes(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", summary)
}

func (h *Handler) GenerateRecoveryCodes(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	var req RecoveryCodesRegenerateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	result, err := h.security.GenerateRecoveryCodes(c.Request.Context(), session, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "生成成功", result)
}

func (h *Handler) RegenerateRecoveryCodes(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	var req RecoveryCodesRegenerateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	result, err := h.security.RegenerateRecoveryCodes(c.Request.Context(), session, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重置成功", result)
}

func (h *Handler) BeginPasskeyRegistration(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	regSession, options, err := h.security.BeginPasskeyRegistration(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"session": regSession, "options": options})
}

func (h *Handler) FinishPasskeyRegistration(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	rawBody, _ := snapshotRequestBody(c)
	var req PasskeyRegistrationFinishRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	payload := credentialPayload(rawBody, req.Credential, req.Payload)
	if len(payload) == 0 {
		response.Error(c, http.StatusBadRequest, 40000, "Passkey 凭证数据不能为空")
		return
	}

	item, err := h.security.FinishPasskeyRegistration(c.Request.Context(), session, req.ChallengeID, payload, req.CredentialName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "绑定成功", item)
}

func (h *Handler) ListPasskeys(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	summary, err := h.security.ListPasskeys(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", summary)
}

func (h *Handler) DeletePasskey(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	if err := h.security.RemovePasskey(c.Request.Context(), session, c.Param("credentialId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "移除成功", gin.H{"deleted": true})
}

func (h *Handler) PasskeyAuthOptions(c *gin.Context) {
	if h.auth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "认证服务暂不可用")
		return
	}

	var req PasskeyLoginBeginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	loginSession, options, err := h.auth.BeginPasskeyLogin(c.Request.Context(), req.AppID, req.MarkCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"session": loginSession, "options": options})
}

func (h *Handler) PasskeyLogin(c *gin.Context) {
	if h.auth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "认证服务暂不可用")
		return
	}

	rawBody, _ := snapshotRequestBody(c)
	var req PasskeyLoginVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	payload := credentialPayload(rawBody, req.Credential, req.Payload)
	if len(payload) == 0 {
		response.Error(c, http.StatusBadRequest, 40000, "Passkey 凭证数据不能为空")
		return
	}

	result, err := h.auth.VerifyPasskeyLogin(c.Request.Context(), req.AppID, req.ChallengeID, payload, req.MarkCode, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "登录成功"), result)
}

func authResultMessage(result *authdomain.LoginResult, successMessage string) string {
	if result != nil && result.RequiresSecondFactor {
		return "需要完成二次认证"
	}
	return successMessage
}

func credentialPayload(rawBody []byte, payloads ...json.RawMessage) []byte {
	for _, item := range payloads {
		trimmed := bytes.TrimSpace(item)
		if len(trimmed) > 0 {
			return trimmed
		}
	}

	trimmed := bytes.TrimSpace(rawBody)
	if len(trimmed) == 0 {
		return nil
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil
	}
	for _, key := range []string{"credential", "payload", "attestation", "assertion"} {
		if item, ok := envelope[key]; ok {
			item = bytes.TrimSpace(item)
			if len(item) > 0 {
				return item
			}
		}
	}
	return nil
}
