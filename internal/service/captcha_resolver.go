package service

import (
	authprotocol "aegis/internal/domain/authprotocol"
	captchadomain "aegis/internal/domain/captcha"
)

// 服务端验证码类型决策器
//
// 原则：验证码类型完全由服务端配置决定，客户端不参与选择。
//   1. 优先使用 App 配置的 DefaultType（前提是该类型已启用）
//   2. 否则按"强 → 弱"优先级选择第一个启用的类型：
//      chiral（手性碳）> dynamic（GIF）> audio（音频）> image > math > digit
//   3. 若 App 未启用任何类型，返回空字符串（调用方决定是否降级或报错）

// 默认优先级顺序（对抗性从强到弱）
var captchaPreferenceOrder = []captchadomain.CaptchaType{
	captchadomain.TypeChiral,
	captchadomain.TypeDynamic,
	captchadomain.TypeAudio,
	captchadomain.TypeImage,
	captchadomain.TypeMath,
	captchadomain.TypeDigit,
}

// ResolveCaptchaType 由 App 配置决定实际下发的验证码类型
// 返回空 CaptchaType 表示 App 未启用任何类型
func ResolveCaptchaType(cfg *captchadomain.CaptchaAppConfig) captchadomain.CaptchaType {
	if cfg == nil {
		return ""
	}

	enabled := captchaEnabledMap(cfg)
	if len(enabled) == 0 {
		return ""
	}

	// 1) DefaultType 命中且已启用
	if cfg.DefaultType != "" {
		preferred := captchadomain.CaptchaType(cfg.DefaultType)
		if enabled[preferred] {
			return preferred
		}
	}

	// 2) 按优先级回退
	for _, t := range captchaPreferenceOrder {
		if enabled[t] {
			return t
		}
	}
	return ""
}

// IsCaptchaRequired 判断 App 是否启用了任意图形验证码类型
func IsCaptchaRequired(cfg *captchadomain.CaptchaAppConfig) bool {
	return len(captchaEnabledMap(cfg)) > 0
}

// IsCaptchaRequiredForScene 判定某个场景下是否需要验证码
//   - 基础条件：App 至少启用了一个图形验证码类型
//   - 场景条件：Login/Register 对应开关为 true（默认 true 兼容旧行为）
//   - 其它 purpose（reset_password 等）：退化为"启用任一类型即要求"
func IsCaptchaRequiredForScene(cfg *captchadomain.CaptchaAppConfig, purpose captchadomain.Purpose) bool {
	if cfg == nil {
		return false
	}
	if !IsCaptchaRequired(cfg) {
		return false
	}
	switch purpose {
	case captchadomain.PurposeLogin:
		return cfg.RequireForLogin
	case captchadomain.PurposeRegister:
		return cfg.RequireForRegister
	default:
		return true
	}
}

// ResolveCaptchaRequirement 把分散在三处的开关折叠成 /config 要下发的结论。
//
// 这个函数是「客户端看到的结论」与「服务端真正的判定」之间唯一的桥：
//
//	login / register —— 与 verifyGatewayCaptcha 同构：策略上的强制开关为真时
//	                    无视场景一律要求，否则回落到应用验证码配置的场景开关。
//	sms              —— 平台级前置图形验证码，见 CaptchaService.RequiresPreCaptchaForSMS。
//
// 判定逻辑写在这里而不是抄进 handler，是为了让测试能拿同一份代码与真实执行点
// 对照。两边各写一遍的话，改了一边就会得到「/config 说不要、服务端却要」，
// 而这种错误在客户端表现为一个没有任何线索的登录失败。
func ResolveCaptchaRequirement(
	policy *authprotocol.Policy,
	appCfg *captchadomain.CaptchaAppConfig,
	smsPreCaptcha bool,
) authprotocol.CaptchaRequirement {
	forced := policy != nil && policy.RequireCaptcha
	return authprotocol.CaptchaRequirement{
		Login:    forced || IsCaptchaRequiredForScene(appCfg, captchadomain.PurposeLogin),
		Register: forced || IsCaptchaRequiredForScene(appCfg, captchadomain.PurposeRegister),
		SMS:      smsPreCaptcha,
	}
}

func captchaEnabledMap(cfg *captchadomain.CaptchaAppConfig) map[captchadomain.CaptchaType]bool {
	if cfg == nil {
		return nil
	}
	enabled := make(map[captchadomain.CaptchaType]bool, 6)
	if cfg.ImageEnabled {
		enabled[captchadomain.TypeImage] = true
	}
	if cfg.MathEnabled {
		enabled[captchadomain.TypeMath] = true
	}
	if cfg.DigitEnabled {
		enabled[captchadomain.TypeDigit] = true
	}
	if cfg.DynamicEnabled {
		enabled[captchadomain.TypeDynamic] = true
	}
	if cfg.AudioEnabled {
		enabled[captchadomain.TypeAudio] = true
	}
	if cfg.ChiralEnabled {
		enabled[captchadomain.TypeChiral] = true
	}
	return enabled
}
