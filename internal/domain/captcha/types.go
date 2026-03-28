package captcha

import "time"

// ────────────────────── 验证码类型枚举 ──────────────────────

// CaptchaType 验证码类型
type CaptchaType string

const (
	TypeImage   CaptchaType = "image"   // 静态图形字符验证码
	TypeMath    CaptchaType = "math"    // 算术验证码
	TypeDigit   CaptchaType = "digit"   // 纯数字验证码
	TypeSMS     CaptchaType = "sms"     // 短信验证码
	TypeDynamic CaptchaType = "dynamic" // 动态 GIF 验证码
	TypeAudio   CaptchaType = "audio"   // 音频 WAV 验证码
	TypeChiral  CaptchaType = "chiral"  // 手性碳点选验证码
)

// Purpose 验证码用途
type Purpose string

const (
	PurposeLogin         Purpose = "login"          // 登录
	PurposeRegister      Purpose = "register"       // 注册
	PurposeResetPassword Purpose = "reset_password"  // 重置密码
	PurposeBindPhone     Purpose = "bind_phone"      // 绑定手机
	PurposeVerifyIdentity Purpose = "verify_identity" // 身份验证
	PurposeAdminLogin    Purpose = "admin_login"     // 管理员登录
	PurposeCustom        Purpose = "custom"          // 自定义
)

// Scope 验证码作用域（区分用户/管理员）
type Scope string

const (
	ScopeUser  Scope = "user"
	ScopeAdmin Scope = "admin"
)

// ────────────────────── 图形验证码 ──────────────────────

// GenerateRequest 验证码生成请求
type GenerateRequest struct {
	Type    CaptchaType // 验证码类型
	Purpose Purpose     // 用途
	Scope   Scope       // 作用域
	AppID   int64       // 租户 App ID（图形验证码可选，短信必填）
}

// GenerateResult 验证码生成结果
type GenerateResult struct {
	CaptchaID     string `json:"captchaId"`                // 验证码唯一 ID
	ImageData     string `json:"imageData,omitempty"`      // Base64 图片（PNG/GIF）
	AudioData     string `json:"audioData,omitempty"`      // Base64 音频（WAV）
	MimeType      string `json:"mimeType,omitempty"`       // image/png / image/gif / audio/wav
	ClickRequired bool   `json:"clickRequired,omitempty"`  // 是否需要点击坐标验证
	ImageWidth    int    `json:"imageWidth,omitempty"`      // 图片宽度（前端定位用）
	ImageHeight   int    `json:"imageHeight,omitempty"`     // 图片高度
	Hint          string `json:"hint,omitempty"`            // 提示文字
	ChiralCount   string `json:"chiralCount,omitempty"`    // 手性碳数量（加密，前端解密使用）
	ExpiresAt     int64  `json:"expiresAt"`                // 过期时间（Unix 秒）
}

// VerifyRequest 验证码校验请求
type VerifyRequest struct {
	CaptchaID string // 验证码 ID
	Answer    string // 用户输入的答案
	Clear     bool   // 验证后是否清除（默认 true）
}

// ────────────────────── 短信验证码 ──────────────────────

// SMSSendRequest 短信验证码发送请求
type SMSSendRequest struct {
	AppID         int64   // 租户 App ID
	Phone         string  // 手机号
	Purpose       Purpose // 用途
	ClientIP      string  // 客户端 IP（用于 IP 维度限流）
	CaptchaID     string  // 前置图形验证码 ID（防机器调用）
	CaptchaAnswer string  // 前置图形验证码答案
}

// SMSSendResult 短信验证码发送结果
type SMSSendResult struct {
	RequestID string `json:"requestId"` // 短信平台请求 ID
	ExpiresAt int64  `json:"expiresAt"` // 过期时间（Unix 秒）
}

// SMSVerifyRequest 短信验证码校验请求
type SMSVerifyRequest struct {
	AppID   int64   // 租户 App ID
	Phone   string  // 手机号
	Code    string  // 用户输入的验证码
	Purpose Purpose // 用途
}

// ────────────────────── 短信服务商配置 ──────────────────────

// SMSProviderType 短信服务商类型
type SMSProviderType string

const (
	SMSProviderAliyun  SMSProviderType = "aliyun"  // 阿里云
	SMSProviderTencent SMSProviderType = "tencent"  // 腾讯云
)

// SMSProviderConfig 短信服务商配置（持久化至数据库）
type SMSProviderConfig struct {
	ID         int64           `json:"id"`
	AppID      int64           `json:"appId"`
	Provider   SMSProviderType `json:"provider"`
	Enabled    bool            `json:"enabled"`
	IsDefault  bool            `json:"isDefault"`
	AccessKey  string          `json:"accessKey,omitempty"`
	SecretKey  string          `json:"secretKey,omitempty"`
	Region     string          `json:"region,omitempty"`
	SignName   string          `json:"signName"`           // 短信签名
	TemplateID string          `json:"templateId"`          // 短信模板 ID
	SDKAppID   string          `json:"sdkAppId,omitempty"` // 腾讯云 SDKAppID
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// SMSProviderConfigMutation 短信配置变更
type SMSProviderConfigMutation struct {
	ID         int64
	AppID      int64
	Provider   *SMSProviderType
	Enabled    *bool
	IsDefault  *bool
	AccessKey  *string
	SecretKey  *string
	Region     *string
	SignName   *string
	TemplateID *string
	SDKAppID   *string
}

// ────────────────────── 应用级验证码配置 ──────────────────────

// CaptchaAppConfig 应用级验证码完整配置（存储在 apps.settings.captcha）
type CaptchaAppConfig struct {
	ImageEnabled   bool   `json:"imageEnabled"`
	MathEnabled    bool   `json:"mathEnabled"`
	DigitEnabled   bool   `json:"digitEnabled"`
	DynamicEnabled bool   `json:"dynamicEnabled"` // GIF 动态验证码
	AudioEnabled   bool   `json:"audioEnabled"`   // 音频验证码
	ChiralEnabled  bool   `json:"chiralEnabled"`  // 手性碳点选验证码
	SMSEnabled     bool   `json:"smsEnabled"`
	DefaultType    string `json:"defaultType"` // image / math / digit / dynamic / audio

	SMS       CaptchaSMSConfig       `json:"sms"`
	AntiFlood CaptchaAntiFloodConfig `json:"antiFlood"`
}

// CaptchaSMSConfig 短信服务商配置（嵌入 CaptchaAppConfig）
type CaptchaSMSConfig struct {
	Provider   string `json:"provider"`             // aliyun / tencent
	AccessKey  string `json:"accessKey,omitempty"`
	SecretKey  string `json:"secretKey,omitempty"`
	Region     string `json:"region,omitempty"`
	SignName   string `json:"signName"`
	TemplateID string `json:"templateId"`
	SDKAppID   string `json:"sdkAppId,omitempty"` // 腾讯云专用
}

// CaptchaAntiFloodConfig 防轰炸规则配置
type CaptchaAntiFloodConfig struct {
	RequireCaptcha       bool `json:"requireCaptcha"`       // 发送前需图形验证码
	IPHourlyLimit        int  `json:"ipHourlyLimit"`        // 同 IP 小时限额
	IPDailyLimit         int  `json:"ipDailyLimit"`         // 同 IP 日限额
	PhoneDailyLimit      int  `json:"phoneDailyLimit"`      // 同号码日限额
	GlobalPhoneDailyLimit int `json:"globalPhoneDailyLimit"` // 全局号码日限额
	SendIntervalSeconds  int  `json:"sendIntervalSeconds"`  // 发送间隔秒数
}

// DefaultCaptchaAppConfig 默认应用验证码配置
func DefaultCaptchaAppConfig() CaptchaAppConfig {
	return CaptchaAppConfig{
		ImageEnabled: true,
		MathEnabled:  true,
		DigitEnabled: false,
		SMSEnabled:   false,
		DefaultType:  "image",
		AntiFlood: CaptchaAntiFloodConfig{
			RequireCaptcha:        true,
			IPHourlyLimit:         5,
			IPDailyLimit:          20,
			PhoneDailyLimit:       10,
			GlobalPhoneDailyLimit: 15,
			SendIntervalSeconds:   60,
		},
	}
}

// ────────────────────── 验证码记录（Redis 存储） ──────────────────────

// CaptchaRecord Redis 中存储的验证码记录
type CaptchaRecord struct {
	Answer    string    `json:"answer"`
	Purpose   Purpose   `json:"purpose"`
	Scope     Scope     `json:"scope"`
	AppID     int64     `json:"appId"`
	CreatedAt time.Time `json:"createdAt"`
	Attempts  int       `json:"attempts"` // 已尝试次数
}

// SMSRecord Redis 中存储的短信验证码记录
type SMSRecord struct {
	Code      string    `json:"code"`
	Purpose   Purpose   `json:"purpose"`
	Phone     string    `json:"phone"`
	AppID     int64     `json:"appId"`
	CreatedAt time.Time `json:"createdAt"`
	Attempts  int       `json:"attempts"`
}
