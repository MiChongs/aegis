package httptransport

import (
	"net/http"
	"strings"

	captchadomain "aegis/internal/domain/captcha"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ────────────────────── 图形验证码 Handler ──────────────────────

// GenerateCaptcha 生成图形/算术/数字/手性碳等验证码
//
// 安全原则：验证码类型由服务端根据目标 App 的 CaptchaAppConfig 决定。
// 客户端请求中的 Type 字段一律忽略（即便传入也无效）。
//
// POST /api/captcha/generate
// 请求体：{ "appid": 10000, "purpose": "login" }
// 响应：{ "captchaId": "...", "type": "chiral|image|...", "imageData": "...", "clickRequired": true/false, ... }
func (h *Handler) GenerateCaptcha(c *gin.Context) {
	var req CaptchaGenerateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	// 服务端决策验证码类型：读取 App 配置 → 按优先级选择实际类型
	captchaType, perr := h.resolveUserCaptchaType(c, req.AppID)
	if perr != nil {
		h.writeError(c, perr)
		return
	}
	if captchaType == "" {
		response.Error(c, http.StatusForbidden, 40310, "当前应用未启用图形验证码")
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

// UserCaptchaPublicConfig 返回 App 的验证码公开配置（无敏感字段）
//
// GET /api/captcha/public-config?appid=10000
// GET /api/captcha/public-config?appid=10000&scene=login
// GET /api/captcha/public-config?appid=10000&scene=register
//
// 用途：
//   - 登录 / 注册 / 重置密码等入口在渲染表单前查询
//   - 前端据此判断**当前场景**是否展示验证码 UI（Login.Required / Register.Required）
//   - 返回服务端决策的类型，前端据此决定是否走点选 (chiral) vs 输入
//
// 公开端点（无需登录），但需提供有效 appid
func (h *Handler) UserCaptchaPublicConfig(c *gin.Context) {
	var req struct {
		AppID int64  `form:"appid"`
		Scene string `form:"scene"`
	}
	_ = c.ShouldBindQuery(&req)
	if req.AppID <= 0 {
		_ = c.ShouldBind(&req)
	}
	if req.AppID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "appid 必填")
		return
	}
	if h.app == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "应用服务不可用")
		return
	}
	cfg, err := h.app.GetCaptchaConfig(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if cfg == nil {
		response.Error(c, http.StatusNotFound, 40410, "应用不存在")
		return
	}

	// 收集启用的图形类型（按服务端优先级排序，与 ResolveCaptchaType 一致）
	available := make([]string, 0, 6)
	ordered := []struct {
		enabled bool
		name    string
	}{
		{cfg.ChiralEnabled, "chiral"},
		{cfg.DynamicEnabled, "dynamic"},
		{cfg.AudioEnabled, "audio"},
		{cfg.ImageEnabled, "image"},
		{cfg.MathEnabled, "math"},
		{cfg.DigitEnabled, "digit"},
	}
	for _, it := range ordered {
		if it.enabled {
			available = append(available, it.name)
		}
	}

	resolved := service.ResolveCaptchaType(cfg)
	resolvedStr := string(resolved)
	anyEnabled := len(available) > 0

	// 分场景要求：与 service.IsCaptchaRequiredForScene 语义对齐
	loginRequired := anyEnabled && cfg.RequireForLogin
	registerRequired := anyEnabled && cfg.RequireForRegister

	resp := CaptchaPublicConfigResponse{
		AppID:         req.AppID,
		Required:      anyEnabled,
		Type:          resolvedStr,
		ClickRequired: resolvedStr == string(captchadomain.TypeChiral),
		Available:     available,
		SMSEnabled:    cfg.SMSEnabled,
		Login:         CaptchaSceneRequirement{Required: loginRequired},
		Register:      CaptchaSceneRequirement{Required: registerRequired},
	}
	// 客户端带 scene 查询参数时，顶层 Required 直接反映该场景，方便精简判断
	switch strings.ToLower(strings.TrimSpace(req.Scene)) {
	case "login":
		resp.Scene = "login"
		resp.Required = loginRequired
	case "register":
		resp.Scene = "register"
		resp.Required = registerRequired
	}

	response.Success(c, http.StatusOK, "ok", resp)
}

// resolveUserCaptchaType 根据 App 配置决定实际下发的验证码类型
// 返回空 CaptchaType 表示该 App 未启用任何图形验证码
func (h *Handler) resolveUserCaptchaType(c *gin.Context, appID int64) (captchadomain.CaptchaType, error) {
	if h.app == nil {
		// AppService 未接入时回退到 image（保持服务可用）
		return captchadomain.TypeImage, nil
	}
	cfg, err := h.app.GetCaptchaConfig(c.Request.Context(), appID)
	if err != nil {
		return "", err
	}
	return service.ResolveCaptchaType(cfg), nil
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
		CaptchaID:     req.CaptchaID,
		Answer:        req.Answer,
		Clear:         true,
		ExpectedScope: captchadomain.ScopeUser,
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
		ImageEnabled:       req.ImageEnabled,
		MathEnabled:        req.MathEnabled,
		DigitEnabled:       req.DigitEnabled,
		DynamicEnabled:     req.DynamicEnabled,
		AudioEnabled:       req.AudioEnabled,
		ChiralEnabled:      req.ChiralEnabled,
		SMSEnabled:         req.SMSEnabled,
		DefaultType:        req.DefaultType,
		RequireForLogin:    req.RequireForLogin,
		RequireForRegister: req.RequireForRegister,
		SMS: captchadomain.CaptchaSMSConfig{
			Provider:     req.SMS.Provider,
			AccessKey:    req.SMS.AccessKey,
			SecretKey:    req.SMS.SecretKey,
			Region:       req.SMS.Region,
			SignName:     req.SMS.SignName,
			TemplateID:   req.SMS.TemplateID,
			CodeParamKey: req.SMS.CodeParamKey,
			SDKAppID:     req.SMS.SDKAppID,
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
	for _, item := range req.SMS.Templates {
		cfg.SMS.Templates = append(cfg.SMS.Templates, captchadomain.CaptchaSMSTemplateConfig{
			Purpose:      item.Purpose,
			Name:         item.Name,
			Enabled:      item.Enabled,
			SignName:     item.SignName,
			TemplateID:   item.TemplateID,
			CodeParamKey: item.CodeParamKey,
		})
	}
	result, err := h.app.UpdateCaptchaConfig(c.Request.Context(), appID, cfg)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", result)
}

func (h *Handler) AdminTestSMS(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminTestSMSRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	purpose := strings.TrimSpace(req.Purpose)
	if purpose == "" {
		purpose = "test"
	}
	appCfg, err := h.app.GetCaptchaConfig(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	providerCfg, err := service.BuildSMSProviderConfig(appID, captchadomain.Purpose(purpose), appCfg.SMS)
	if err != nil {
		h.writeError(c, err)
		return
	}
	result, err := h.captcha.SendTrustedSMSCode(c.Request.Context(), captchadomain.SMSSendRequest{
		AppID:    appID,
		Phone:    req.Phone,
		Purpose:  captchadomain.Purpose(purpose),
		ClientIP: c.ClientIP(),
	}, providerCfg)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试短信已发送", gin.H{
		"purpose":    purpose,
		"templateId": providerCfg.TemplateID,
		"signName":   providerCfg.SignName,
		"result":     result,
	})
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
		CaptchaID:       req.CaptchaID,
		Answer:          req.Answer,
		Clear:           true,
		ExpectedPurpose: captchadomain.PurposeAdminLogin,
		ExpectedScope:   captchadomain.ScopeAdmin,
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
	appCfg, err := h.app.GetCaptchaConfig(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	providerCfg, err := service.BuildSMSProviderConfig(req.AppID, captchadomain.Purpose(req.Purpose), appCfg.SMS)
	if err != nil {
		h.writeError(c, err)
		return
	}
	result, err := h.captcha.SendSMSCode(c.Request.Context(), smsReq, providerCfg)
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
