package httptransport

import (
	"net/http"

	captchadomain "aegis/internal/domain/captcha"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ────────────────────── 图形验证码 Handler ──────────────────────

// GenerateCaptcha 生成图形/算术/数字验证码
// POST /api/captcha/generate
func (h *Handler) GenerateCaptcha(c *gin.Context) {
	var req CaptchaGenerateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	captchaType := captchadomain.CaptchaType(req.Type)
	switch captchaType {
	case captchadomain.TypeImage, captchadomain.TypeMath, captchadomain.TypeDigit, captchadomain.TypeDynamic, captchadomain.TypeAudio, captchadomain.TypeChiral:
		// 合法类型
	default:
		response.Error(c, http.StatusBadRequest, 40001, "不支持的验证码类型，可选: image, math, digit, dynamic, audio, chiral")
		return
	}

	purpose := captchadomain.Purpose(req.Purpose)
	genReq := captchadomain.GenerateRequest{
		Type:    captchaType,
		Purpose: purpose,
		Scope:   captchadomain.ScopeUser,
		AppID:   req.AppID,
	}

	result, err := h.captcha.Generate(c.Request.Context(), captchaType, genReq)
	if err != nil {
		h.writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "验证码生成成功", result)
}

// VerifyCaptcha 校验图形/算术/数字验证码
// POST /api/captcha/verify
func (h *Handler) VerifyCaptcha(c *gin.Context) {
	var req CaptchaVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	verifyReq := captchadomain.VerifyRequest{
		CaptchaID: req.CaptchaID,
		Answer:    req.Answer,
		Clear:     true,
	}

	valid, err := h.captcha.Verify(c.Request.Context(), verifyReq)
	if err != nil {
		h.writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "验证完成", gin.H{"valid": valid})
}

// ────────────────────── 管理员验证码 Handler ──────────────────────

// AdminGenerateCaptcha 管理员登录验证码
// POST /api/admin/captcha/generate
func (h *Handler) AdminGenerateCaptcha(c *gin.Context) {
	var req CaptchaGenerateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	captchaType := captchadomain.CaptchaType(req.Type)
	switch captchaType {
	case captchadomain.TypeImage, captchadomain.TypeMath, captchadomain.TypeDigit, captchadomain.TypeDynamic, captchadomain.TypeAudio, captchadomain.TypeChiral:
	default:
		captchaType = captchadomain.TypeImage // 管理员默认图形验证码
	}

	purpose := captchadomain.Purpose(req.Purpose)
	if purpose == "" {
		purpose = captchadomain.PurposeAdminLogin
	}

	genReq := captchadomain.GenerateRequest{
		Type:    captchaType,
		Purpose: purpose,
		Scope:   captchadomain.ScopeAdmin,
	}

	result, err := h.captcha.Generate(c.Request.Context(), captchaType, genReq)
	if err != nil {
		h.writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "验证码生成成功", result)
}

// ────────────────────── 管理员验证码配置 ──────────────────────

func (h *Handler) AdminGetCaptchaConfig(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cfg, err := h.app.GetCaptchaConfig(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", cfg)
}

func (h *Handler) AdminUpdateCaptchaConfig(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminCaptchaConfigUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	cfg := captchadomain.CaptchaAppConfig{
		ImageEnabled: req.ImageEnabled,
		MathEnabled:  req.MathEnabled,
		DigitEnabled: req.DigitEnabled,
		SMSEnabled:   req.SMSEnabled,
		DefaultType:  req.DefaultType,
		SMS: captchadomain.CaptchaSMSConfig{
			Provider:   req.SMS.Provider,
			AccessKey:  req.SMS.AccessKey,
			SecretKey:  req.SMS.SecretKey,
			Region:     req.SMS.Region,
			SignName:   req.SMS.SignName,
			TemplateID: req.SMS.TemplateID,
			SDKAppID:   req.SMS.SDKAppID,
		},
		AntiFlood: captchadomain.CaptchaAntiFloodConfig{
			RequireCaptcha:        req.AntiFlood.RequireCaptcha,
			IPHourlyLimit:         req.AntiFlood.IPHourlyLimit,
			IPDailyLimit:          req.AntiFlood.IPDailyLimit,
			PhoneDailyLimit:       req.AntiFlood.PhoneDailyLimit,
			GlobalPhoneDailyLimit: req.AntiFlood.GlobalPhoneDailyLimit,
			SendIntervalSeconds:   req.AntiFlood.SendIntervalSeconds,
		},
	}
	result, err := h.app.UpdateCaptchaConfig(c.Request.Context(), appID, cfg)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", result)
}

// AdminVerifyCaptcha 管理员验证码校验
// POST /api/admin/captcha/verify
func (h *Handler) AdminVerifyCaptcha(c *gin.Context) {
	var req CaptchaVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	verifyReq := captchadomain.VerifyRequest{
		CaptchaID: req.CaptchaID,
		Answer:    req.Answer,
		Clear:     true,
	}

	valid, err := h.captcha.Verify(c.Request.Context(), verifyReq)
	if err != nil {
		h.writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "验证完成", gin.H{"valid": valid})
}

// ────────────────────── 短信验证码 Handler ──────────────────────

// SendSMSCode 发送短信验证码
// POST /api/captcha/sms/send
//
// 安全流程：
//  1. 客户端先调用 POST /api/captcha/generate 获取图形验证码
//  2. 用户填写图形验证码答案后，连同 captchaId + captchaAnswer 一起提交
//  3. 服务端校验图形验证码 → IP 限流 → 手机号限流 → 发送短信
func (h *Handler) SendSMSCode(c *gin.Context) {
	var req SMSSendCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	smsReq := captchadomain.SMSSendRequest{
		AppID:         req.AppID,
		Phone:         req.Phone,
		Purpose:       captchadomain.Purpose(req.Purpose),
		ClientIP:      c.ClientIP(),
		CaptchaID:     req.CaptchaID,
		CaptchaAnswer: req.CaptchaAnswer,
	}

	// TODO: 从数据库加载 App 对应的 SMSProviderConfig
	// providerCfg, err := h.app.GetSMSProviderConfig(c.Request.Context(), req.AppID)
	result, err := h.captcha.SendSMSCode(c.Request.Context(), smsReq, nil)
	if err != nil {
		h.writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "短信验证码已发送", result)
}

// VerifySMSCode 校验短信验证码
// POST /api/captcha/sms/verify
func (h *Handler) VerifySMSCode(c *gin.Context) {
	var req SMSVerifyCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	smsReq := captchadomain.SMSVerifyRequest{
		AppID:   req.AppID,
		Phone:   req.Phone,
		Code:    req.Code,
		Purpose: captchadomain.Purpose(req.Purpose),
	}

	valid, err := h.captcha.VerifySMSCode(c.Request.Context(), smsReq)
	if err != nil {
		h.writeError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "验证完成", gin.H{"valid": valid})
}

// VerifyCaptchaClick 坐标点选验证（手性碳等）
func (h *Handler) VerifyCaptchaClick(c *gin.Context) {
	var req CaptchaVerifyClickRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	clicks := make([]service.ChiralClickPoint, len(req.Clicks))
	for i, click := range req.Clicks {
		clicks[i] = service.ChiralClickPoint{X: click.X, Y: click.Y}
	}
	valid, err := h.captcha.VerifyClick(c.Request.Context(), req.CaptchaID, clicks)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "验证完成", gin.H{"valid": valid})
}
