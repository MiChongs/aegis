package httptransport

// ────────────────────── 图形验证码 DTO ──────────────────────

// CaptchaGenerateRequest 生成验证码请求
type CaptchaGenerateRequest struct {
	Type    string `json:"type" form:"type" binding:"required"`       // image | math | digit
	Purpose string `json:"purpose" form:"purpose" binding:"required"` // login | register | reset_password | ...
	AppID   int64  `json:"appid" form:"appid"`                        // 可选，多租户场景
}

// CaptchaVerifyRequest 校验验证码请求
type CaptchaVerifyRequest struct {
	CaptchaID string `json:"captchaId" form:"captchaId" binding:"required"`
	Answer    string `json:"answer" form:"answer" binding:"required"`
}

// ────────────────────── 短信验证码 DTO ──────────────────────

// SMSSendCodeRequest 发送短信验证码请求
type SMSSendCodeRequest struct {
	AppID         int64  `json:"appid" form:"appid" binding:"required"`
	Phone         string `json:"phone" form:"phone" binding:"required"`
	Purpose       string `json:"purpose" form:"purpose" binding:"required"`       // login | register | reset_password | ...
	CaptchaID     string `json:"captchaId" form:"captchaId"`                      // 图形验证码 ID（防轰炸前置校验）
	CaptchaAnswer string `json:"captchaAnswer" form:"captchaAnswer"`              // 图形验证码答案
}

// ────────────────────── 管理员验证码配置 DTO ──────────────────────

// CaptchaVerifyClickRequest 坐标点选验证请求（支持多点）
type CaptchaVerifyClickRequest struct {
	CaptchaID string `json:"captchaId" form:"captchaId" binding:"required"`
	Clicks    []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"clicks" binding:"required"`
}

// AdminCaptchaConfigUpdateRequest 更新应用验证码配置
type AdminCaptchaConfigUpdateRequest struct {
	ImageEnabled bool `json:"imageEnabled"`
	MathEnabled  bool `json:"mathEnabled"`
	DigitEnabled bool `json:"digitEnabled"`
	SMSEnabled   bool `json:"smsEnabled"`
	DefaultType  string `json:"defaultType"`
	SMS          struct {
		Provider   string `json:"provider"`
		AccessKey  string `json:"accessKey"`
		SecretKey  string `json:"secretKey"`
		Region     string `json:"region"`
		SignName   string `json:"signName"`
		TemplateID string `json:"templateId"`
		SDKAppID   string `json:"sdkAppId"`
	} `json:"sms"`
	AntiFlood struct {
		RequireCaptcha        bool `json:"requireCaptcha"`
		IPHourlyLimit         int  `json:"ipHourlyLimit"`
		IPDailyLimit          int  `json:"ipDailyLimit"`
		PhoneDailyLimit       int  `json:"phoneDailyLimit"`
		GlobalPhoneDailyLimit int  `json:"globalPhoneDailyLimit"`
		SendIntervalSeconds   int  `json:"sendIntervalSeconds"`
	} `json:"antiFlood"`
}

// AdminTestSMSRequest 测试短信发送
type AdminTestSMSRequest struct {
	Phone string `json:"phone" binding:"required"`
}

// SMSVerifyCodeRequest 校验短信验证码请求
type SMSVerifyCodeRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	Phone   string `json:"phone" form:"phone" binding:"required"`
	Code    string `json:"code" form:"code" binding:"required"`
	Purpose string `json:"purpose" form:"purpose" binding:"required"`
}
