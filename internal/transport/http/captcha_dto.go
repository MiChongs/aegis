package httptransport

// ────────────────────── 图形验证码 DTO ──────────────────────

// CaptchaGenerateRequest 生成验证码请求
//
// 安全原则：验证码类型由服务端根据 App 配置决定，客户端不参与选择。
//   - 客户端只提交 Purpose + AppID
//   - 服务端读取 AppService.GetCaptchaConfig 按优先级选出实际类型
//   - 若客户端传了 Type 字段（兼容旧代码），一律忽略
//
// Type 字段保留在 DTO 仅为兼容旧前端不报错，实际语义由服务端覆盖。
type CaptchaGenerateRequest struct {
	Type    string `json:"type" form:"type"`                          // 已废弃：服务端强制忽略客户端传入
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
	Purpose       string `json:"purpose" form:"purpose" binding:"required"` // login | register | reset_password | ...
	CaptchaID     string `json:"captchaId" form:"captchaId"`                // 图形验证码 ID（防轰炸前置校验）
	CaptchaAnswer string `json:"captchaAnswer" form:"captchaAnswer"`        // 图形验证码答案
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
	ImageEnabled       bool   `json:"imageEnabled"`
	MathEnabled        bool   `json:"mathEnabled"`
	DigitEnabled       bool   `json:"digitEnabled"`
	DynamicEnabled     bool   `json:"dynamicEnabled"`
	AudioEnabled       bool   `json:"audioEnabled"`
	ChiralEnabled      bool   `json:"chiralEnabled"`
	SMSEnabled         bool   `json:"smsEnabled"`
	DefaultType        string `json:"defaultType"`
	RequireForLogin    bool   `json:"requireForLogin"`
	RequireForRegister bool   `json:"requireForRegister"`
	SMS          struct {
		Provider     string `json:"provider"`
		AccessKey    string `json:"accessKey"`
		SecretKey    string `json:"secretKey"`
		Region       string `json:"region"`
		SignName     string `json:"signName"`
		TemplateID   string `json:"templateId"`
		CodeParamKey string `json:"codeParamKey"`
		SDKAppID     string `json:"sdkAppId"`
		Templates    []struct {
			Purpose      string `json:"purpose"`
			Name         string `json:"name"`
			Enabled      bool   `json:"enabled"`
			SignName     string `json:"signName"`
			TemplateID   string `json:"templateId"`
			CodeParamKey string `json:"codeParamKey"`
		} `json:"templates"`
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
	Phone   string `json:"phone" binding:"required"`
	Purpose string `json:"purpose"`
}

// CaptchaSceneRequirement 单个场景（login / register）的验证码策略
type CaptchaSceneRequirement struct {
	Required bool `json:"required"` // 此场景是否强制要求验证码
}

// CaptchaPublicConfigResponse 公开暴露的 App 验证码配置
// 用途：登录 / 注册等入口在渲染 UI 前查询，判定是否需要展示验证码、展示何种类型
// 仅包含"前端决策需要的"字段，不暴露短信密钥等敏感配置
//
// 顶层 Required 表示"该 App 是否启用了任意图形验证码类型"（兜底标志）；
// 细粒度场景由 Login.Required / Register.Required 控制。
// 若请求带 ?scene=login|register，则顶层 Required 会被覆盖为对应场景的要求，
// 便于只关心某一场景的客户端直接读取。
type CaptchaPublicConfigResponse struct {
	AppID         int64                   `json:"appid"`
	Required      bool                    `json:"required"`
	Type          string                  `json:"type"`
	ClickRequired bool                    `json:"clickRequired"`
	Available     []string                `json:"available"`
	SMSEnabled    bool                    `json:"smsEnabled"`
	Login         CaptchaSceneRequirement `json:"login"`
	Register      CaptchaSceneRequirement `json:"register"`
	Scene         string                  `json:"scene,omitempty"`
}

// SMSVerifyCodeRequest 校验短信验证码请求
type SMSVerifyCodeRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	Phone   string `json:"phone" form:"phone" binding:"required"`
	Code    string `json:"code" form:"code" binding:"required"`
	Purpose string `json:"purpose" form:"purpose" binding:"required"`
}
