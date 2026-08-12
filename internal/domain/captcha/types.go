package captcha

import (
	"time"

	"aegis/pkg/gifcaptcha"
)

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
	PurposeLogin          Purpose = "login"           // 登录
	PurposeRegister       Purpose = "register"        // 注册
	PurposeResetPassword  Purpose = "reset_password"  // 重置密码
	PurposeBindPhone      Purpose = "bind_phone"      // 绑定手机
	PurposeVerifyIdentity Purpose = "verify_identity" // 身份验证
	PurposeAdminLogin     Purpose = "admin_login"     // 管理员登录
	PurposeCustom         Purpose = "custom"          // 自定义
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

	// Dynamic 动态验证码渲染参数，由调用方按作用域取来（应用侧取应用配置，
	// 管理端取平台配置）。留空 = 用默认值。
	Dynamic *DynamicConfig
}

// GenerateResult 验证码生成结果
type GenerateResult struct {
	CaptchaID     string      `json:"captchaId"`               // 验证码唯一 ID
	Type          CaptchaType `json:"type,omitempty"`          // 服务端实际下发的验证码类型（前端据此渲染 UI）
	ImageData     string      `json:"imageData,omitempty"`     // Base64 图片（PNG/GIF）
	AudioData     string      `json:"audioData,omitempty"`     // Base64 音频（WAV）
	MimeType      string      `json:"mimeType,omitempty"`      // image/png / image/gif / audio/wav
	ClickRequired bool        `json:"clickRequired,omitempty"` // 是否需要点击坐标验证
	ImageWidth    int         `json:"imageWidth,omitempty"`    // 图片宽度（前端定位用）
	ImageHeight   int         `json:"imageHeight,omitempty"`   // 图片高度
	Hint          string      `json:"hint,omitempty"`          // 提示文字
	ChiralCount   string      `json:"chiralCount,omitempty"`   // 手性碳数量（加密，前端解密使用）
	ExpiresAt     int64       `json:"expiresAt"`               // 过期时间（Unix 秒）
}

// VerifyRequest 验证码校验请求
type VerifyRequest struct {
	CaptchaID       string  // 验证码 ID
	Answer          string  // 用户输入的答案
	Clear           bool    // 验证后是否清除（默认 true）
	ExpectedAppID   int64   // 非零时必须与生成验证码的 App 一致
	ExpectedPurpose Purpose // 非空时必须与生成用途一致
	ExpectedScope   Scope   // 非空时必须与用户/管理员作用域一致
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
	SMSProviderTencent SMSProviderType = "tencent" // 腾讯云
)

// SMSProviderConfig 短信服务商配置（持久化至数据库）
type SMSProviderConfig struct {
	ID           int64           `json:"id"`
	AppID        int64           `json:"appId"`
	Provider     SMSProviderType `json:"provider"`
	Enabled      bool            `json:"enabled"`
	IsDefault    bool            `json:"isDefault"`
	AccessKey    string          `json:"accessKey,omitempty"`
	SecretKey    string          `json:"secretKey,omitempty"`
	Region       string          `json:"region,omitempty"`
	SignName     string          `json:"signName"`   // 短信签名
	TemplateID   string          `json:"templateId"` // 短信模板 ID
	CodeParamKey string          `json:"codeParamKey,omitempty"`
	SDKAppID     string          `json:"sdkAppId,omitempty"` // 腾讯云 SDKAppID
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// SMSProviderConfigMutation 短信配置变更
type SMSProviderConfigMutation struct {
	ID           int64
	AppID        int64
	Provider     *SMSProviderType
	Enabled      *bool
	IsDefault    *bool
	AccessKey    *string
	SecretKey    *string
	Region       *string
	SignName     *string
	TemplateID   *string
	CodeParamKey *string
	SDKAppID     *string
}

// ────────────────────── 动态验证码渲染参数 ──────────────────────

// DynamicConfig 动态验证码外观。动态配置：应用级存 apps.settings.captcha.dynamic，
// 管理端存 platform_settings 的 adminCaptcha.dynamic，控制台可改、即时生效。
// 取值区间与默认值由 pkg/gifcaptcha 统一裁决，这里不重复夹取。
type DynamicConfig struct {
	Length       int    `json:"length"`       // 字符数 3-8
	Width        int    `json:"width"`        // 画布宽度 80-640
	Height       int    `json:"height"`       // 画布高度 40-240
	Frames       int    `json:"frames"`       // 帧数 4-40
	FrameDelayMs int    `json:"frameDelayMs"` // 帧间隔（毫秒）20-1000
	Mode         string `json:"mode"`         // 字符集：alnum / alpha / digit
	Noise        int    `json:"noise"`        // 干扰强度 0-100
	Wobble       int    `json:"wobble"`       // 运动幅度 0-100
}

// DefaultDynamicConfig 默认参数，取自渲染引擎
func DefaultDynamicConfig() DynamicConfig {
	return fromRenderOptions(gifcaptcha.DefaultOptions())
}

// RenderOptions 翻成渲染引擎参数。新增参数必须出现在这里，否则它就是个存了不用的开关。
func (c DynamicConfig) RenderOptions() gifcaptcha.Options {
	return gifcaptcha.Options{
		Width:      c.Width,
		Height:     c.Height,
		Length:     c.Length,
		Frames:     c.Frames,
		FrameDelay: time.Duration(c.FrameDelayMs) * time.Millisecond,
		Mode:       gifcaptcha.Mode(c.Mode),
		Noise:      c.Noise,
		Wobble:     c.Wobble,
	}
}

// Normalized 回填默认值并夹进合法区间。落库前要过一遍，让读到的配置就是生效的值。
func (c DynamicConfig) Normalized() DynamicConfig {
	return fromRenderOptions(c.RenderOptions().Normalize())
}

func fromRenderOptions(opts gifcaptcha.Options) DynamicConfig {
	return DynamicConfig{
		Length:       opts.Length,
		Width:        opts.Width,
		Height:       opts.Height,
		Frames:       opts.Frames,
		FrameDelayMs: int(opts.FrameDelay / time.Millisecond),
		Mode:         string(opts.Mode),
		Noise:        opts.Noise,
		Wobble:       opts.Wobble,
	}
}

// DynamicPreview 动态验证码样张，不落库、不可用于校验
type DynamicPreview struct {
	ImageData    string        `json:"imageData"`    // data:image/gif;base64,...
	MimeType     string        `json:"mimeType"`     // image/gif
	Answer       string        `json:"answer"`       // 样张答案，供判断辨识度
	Width        int           `json:"width"`        //
	Height       int           `json:"height"`       //
	Frames       int           `json:"frames"`       //
	FrameDelayMs int           `json:"frameDelayMs"` //
	DurationMs   int           `json:"durationMs"`   // 一轮动画时长
	ByteSize     int           `json:"byteSize"`     // GIF 字节数
	Applied      DynamicConfig `json:"applied"`      // 夹取后真正生效的参数
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

	// 场景级开关：分别控制"登录"和"注册"是否要求图形验证码
	// 老数据中若这些字段缺失（历史 JSON 没有），JSON Unmarshal 保持 default 值 true，
	// 与旧行为（任一类型启用即要求验证码）对齐；新 App / 管理员可显式关闭
	RequireForLogin    bool `json:"requireForLogin"`
	RequireForRegister bool `json:"requireForRegister"`

	// Dynamic 动态验证码外观。存量 JSON 没有这个键时保留默认值，
	// 因此库里可以只存部分字段，缺的自动落回默认。
	Dynamic DynamicConfig `json:"dynamic"`

	SMS       CaptchaSMSConfig       `json:"sms"`
	AntiFlood CaptchaAntiFloodConfig `json:"antiFlood"`
}

// CaptchaSMSConfig 短信服务商配置（嵌入 CaptchaAppConfig）
type CaptchaSMSConfig struct {
	Provider     string                     `json:"provider"` // aliyun / tencent
	AccessKey    string                     `json:"accessKey,omitempty"`
	SecretKey    string                     `json:"secretKey,omitempty"`
	Region       string                     `json:"region,omitempty"`
	SignName     string                     `json:"signName"`
	TemplateID   string                     `json:"templateId"`
	CodeParamKey string                     `json:"codeParamKey,omitempty"`
	SDKAppID     string                     `json:"sdkAppId,omitempty"` // 腾讯云专用
	Templates    []CaptchaSMSTemplateConfig `json:"templates,omitempty"`
}

type CaptchaSMSTemplateConfig struct {
	Purpose      string `json:"purpose"`
	Name         string `json:"name,omitempty"`
	Enabled      bool   `json:"enabled"`
	SignName     string `json:"signName,omitempty"`
	TemplateID   string `json:"templateId"`
	CodeParamKey string `json:"codeParamKey,omitempty"`
}

// CaptchaAntiFloodConfig 防轰炸规则配置
type CaptchaAntiFloodConfig struct {
	RequireCaptcha        bool `json:"requireCaptcha"`        // 发送前需图形验证码
	IPHourlyLimit         int  `json:"ipHourlyLimit"`         // 同 IP 小时限额
	IPDailyLimit          int  `json:"ipDailyLimit"`          // 同 IP 日限额
	PhoneDailyLimit       int  `json:"phoneDailyLimit"`       // 同号码日限额
	GlobalPhoneDailyLimit int  `json:"globalPhoneDailyLimit"` // 全局号码日限额
	SendIntervalSeconds   int  `json:"sendIntervalSeconds"`   // 发送间隔秒数
}

// DefaultCaptchaAppConfig 默认应用验证码配置
// RequireForLogin / RequireForRegister 默认 true —— 与旧行为兼容（启用任一类型即要求）
func DefaultCaptchaAppConfig() CaptchaAppConfig {
	return CaptchaAppConfig{
		ImageEnabled:       true,
		MathEnabled:        true,
		DigitEnabled:       false,
		SMSEnabled:         false,
		DefaultType:        "image",
		RequireForLogin:    true,
		RequireForRegister: true,
		Dynamic:            DefaultDynamicConfig(),
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
