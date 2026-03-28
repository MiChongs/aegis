package httptransport

import (
	"net/http"

	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminSecurity(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	status, err := h.security.GetAdminSecurityStatus(c.Request.Context(), access)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

func (h *Handler) BeginAdminTOTPEnrollment(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	result, err := h.security.BeginAdminTOTPEnrollment(c.Request.Context(), access)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) EnableAdminTOTP(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
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

	status, recovery, err := h.security.EnableAdminTOTP(c.Request.Context(), access, req.EnrollmentID, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "启用成功", gin.H{"twoFactor": status, "recoveryCodes": recovery})
}

func (h *Handler) DisableAdminTOTP(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
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

	status, err := h.security.DisableAdminTOTP(c.Request.Context(), access, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "关闭成功", status)
}

func (h *Handler) ListAdminRecoveryCodes(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	summary, err := h.security.ListAdminRecoveryCodes(c.Request.Context(), access)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", summary)
}

func (h *Handler) GenerateAdminRecoveryCodes(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
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

	result, err := h.security.GenerateAdminRecoveryCodes(c.Request.Context(), access, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "生成成功", result)
}

func (h *Handler) RegenerateAdminRecoveryCodes(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
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

	result, err := h.security.RegenerateAdminRecoveryCodes(c.Request.Context(), access, req.Code, req.RecoveryCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重置成功", result)
}

func (h *Handler) BeginAdminPasskeyRegistration(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	regSession, options, err := h.security.BeginAdminPasskeyRegistration(c.Request.Context(), access)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"session": regSession, "options": options})
}

func (h *Handler) FinishAdminPasskeyRegistration(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
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

	item, err := h.security.FinishAdminPasskeyRegistration(c.Request.Context(), access, req.ChallengeID, payload, req.CredentialName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "绑定成功", item)
}

func (h *Handler) ListAdminPasskeys(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	summary, err := h.security.ListAdminPasskeys(c.Request.Context(), access)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", summary)
}

func (h *Handler) DeleteAdminPasskey(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	if h.security == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "安全模块暂不可用")
		return
	}

	if err := h.security.RemoveAdminPasskey(c.Request.Context(), access, c.Param("credentialId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "移除成功", gin.H{"deleted": true})
}
