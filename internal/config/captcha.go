package config

import "time"

// CaptchaConfig 验证码总配置
type CaptchaConfig struct {
	Enabled bool          // 验证码服务总开关
	TTL     time.Duration // 验证码有效期（默认 5 分钟）
	Image   ImageCaptchaConfig
	Math    MathCaptchaConfig
	Digit   DigitCaptchaConfig
	Dynamic DynamicCaptchaConfig
	Audio   AudioCaptchaConfig
	SMS     SMSCaptchaConfig
}

// DynamicCaptchaConfig 动态 GIF 验证码配置
type DynamicCaptchaConfig struct {
	Enabled bool // 是否启用
	Length  int  // 数字位数（默认 6）
	Width   int  // 图片宽度（默认 240）
	Height  int  // 图片高度（默认 80）
}

// AudioCaptchaConfig 音频 WAV 验证码配置
type AudioCaptchaConfig struct {
	Enabled bool   // 是否启用
	Length  int    // 数字位数（默认 6）
	Lang    string // 语言（默认 en，支持 en/ru/zh）
}

// ImageCaptchaConfig 图形字符验证码配置
type ImageCaptchaConfig struct {
	Enabled    bool // 是否启用图形验证码
	Length     int  // 字符长度（默认 4）
	Width      int  // 图片宽度（默认 240）
	Height     int  // 图片高度（默认 80）
	NoiseCount int  // 干扰点数量（默认 4）
	ShowLine   bool // 是否显示干扰线（默认 true）
}

// MathCaptchaConfig 算术验证码配置
type MathCaptchaConfig struct {
	Enabled   bool // 是否启用算术验证码
	MaxNumber int  // 最大数字（默认 20）
	Width     int  // 图片宽度（默认 240）
	Height    int  // 图片高度（默认 80）
}

// DigitCaptchaConfig 纯数字验证码配置
type DigitCaptchaConfig struct {
	Enabled bool // 是否启用数字验证码
	Length  int  // 数字长度（默认 6）
	Width   int  // 图片宽度（默认 240）
	Height  int  // 图片高度（默认 80）
}

// SMSCaptchaConfig 短信验证码配置
type SMSCaptchaConfig struct {
	Enabled      bool          // 是否启用短信验证码
	CodeLength   int           // 验证码长度（默认 6）
	TTL          time.Duration // 短信验证码有效期（默认 5 分钟）
	SendInterval time.Duration // 同一手机号发送间隔（防刷，默认 60 秒）
	MaxAttempts  int           // 最大验证尝试次数（默认 5）
	DailyLimit   int           // 同一手机号每日发送上限（默认 10）

	// ── 防短信轰炸 ──

	RequireCaptcha       bool // 发送短信前必须通过图形验证码（默认 true）
	IPHourlyLimit        int  // 同一 IP 每小时发送上限（默认 5）
	IPDailyLimit         int  // 同一 IP 每日发送上限（默认 20）
	GlobalPhoneDailyLimit int  // 同一手机号跨所有 AppID 每日上限（默认 15）
}

// NormalizeCaptchaConfig 填充验证码配置默认值
func NormalizeCaptchaConfig(cfg CaptchaConfig) CaptchaConfig {
	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Minute
	}

	// 图形验证码默认值
	if cfg.Image.Length <= 0 {
		cfg.Image.Length = 4
	}
	if cfg.Image.Width <= 0 {
		cfg.Image.Width = 240
	}
	if cfg.Image.Height <= 0 {
		cfg.Image.Height = 80
	}
	if cfg.Image.NoiseCount <= 0 {
		cfg.Image.NoiseCount = 4
	}

	// 算术验证码默认值
	if cfg.Math.MaxNumber <= 0 {
		cfg.Math.MaxNumber = 20
	}
	if cfg.Math.Width <= 0 {
		cfg.Math.Width = 240
	}
	if cfg.Math.Height <= 0 {
		cfg.Math.Height = 80
	}

	// 数字验证码默认值
	if cfg.Digit.Length <= 0 {
		cfg.Digit.Length = 6
	}
	if cfg.Digit.Width <= 0 {
		cfg.Digit.Width = 240
	}
	if cfg.Digit.Height <= 0 {
		cfg.Digit.Height = 80
	}

	// 动态 GIF 验证码默认值
	if cfg.Dynamic.Length <= 0 {
		cfg.Dynamic.Length = 6
	}
	if cfg.Dynamic.Width <= 0 {
		cfg.Dynamic.Width = 240
	}
	if cfg.Dynamic.Height <= 0 {
		cfg.Dynamic.Height = 80
	}

	// 音频验证码默认值
	if cfg.Audio.Length <= 0 {
		cfg.Audio.Length = 6
	}
	if cfg.Audio.Lang == "" {
		cfg.Audio.Lang = "en"
	}

	// 短信验证码默认值
	if cfg.SMS.CodeLength <= 0 {
		cfg.SMS.CodeLength = 6
	}
	if cfg.SMS.TTL <= 0 {
		cfg.SMS.TTL = 5 * time.Minute
	}
	if cfg.SMS.SendInterval <= 0 {
		cfg.SMS.SendInterval = 60 * time.Second
	}
	if cfg.SMS.MaxAttempts <= 0 {
		cfg.SMS.MaxAttempts = 5
	}
	if cfg.SMS.DailyLimit <= 0 {
		cfg.SMS.DailyLimit = 10
	}
	// 防轰炸默认值
	if cfg.SMS.Enabled && !cfg.SMS.RequireCaptcha {
		cfg.SMS.RequireCaptcha = true // 默认强制要求图形验证码前置
	}
	if cfg.SMS.IPHourlyLimit <= 0 {
		cfg.SMS.IPHourlyLimit = 5
	}
	if cfg.SMS.IPDailyLimit <= 0 {
		cfg.SMS.IPDailyLimit = 20
	}
	if cfg.SMS.GlobalPhoneDailyLimit <= 0 {
		cfg.SMS.GlobalPhoneDailyLimit = 15
	}

	return cfg
}
