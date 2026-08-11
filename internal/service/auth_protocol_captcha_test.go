package service

import (
	"testing"

	authprotocol "aegis/internal/domain/authprotocol"
	captchadomain "aegis/internal/domain/captcha"
)

// /config 下发的验证码结论必须与服务端真正的判定一致。
//
// 「要不要图形验证码」这件事在服务端由三处独立开关决定，分属三个管理入口：
//
//	Policy.RequireCaptcha                接入协议策略（无视场景的强制开关）
//	CaptchaAppConfig.RequireForLogin/…   应用验证码配置的分场景开关
//	config.Captcha.SMS.RequireCaptcha    平台级短信前置验证码
//
// 客户端只看得到 /config。之前那里只下发了第一处，于是下面 forcedOffButAppRequires
// 这种再普通不过的组合，会让登录直接被拒而登录页上毫无提示 —— 用户按"不需要验证码"
// 填完表单，服务端按"需要"拒绝，两边都认为自己是对的。
//
// 现在网关闸门（verifyGatewayCaptcha）与 /config 用的是本文件测的这同一个函数，
// 因此不一致在结构上已不可能；这些用例钉住的是折叠规则本身不被改错。
func TestResolveCaptchaRequirementFoldsEverySwitch(t *testing.T) {
	imageOnly := func(login, register bool) *captchadomain.CaptchaAppConfig {
		return &captchadomain.CaptchaAppConfig{
			ImageEnabled:       true,
			RequireForLogin:    login,
			RequireForRegister: register,
		}
	}

	cases := []struct {
		name         string
		policyForced bool
		appCfg       *captchadomain.CaptchaAppConfig
		smsPre       bool
		want         authprotocol.CaptchaRequirement
	}{
		{
			name: "全关",
			want: authprotocol.CaptchaRequirement{},
		},
		{
			// 这一条就是线上撞到的那个组合。
			name:   "策略没开但应用要求登录验证码",
			appCfg: imageOnly(true, false),
			want:   authprotocol.CaptchaRequirement{Login: true},
		},
		{
			name:   "应用只要求注册验证码",
			appCfg: imageOnly(false, true),
			want:   authprotocol.CaptchaRequirement{Register: true},
		},
		{
			// 策略上的强制开关不看场景，两个入口一起要求。
			name:         "策略强制，应用场景开关全关",
			policyForced: true,
			appCfg:       imageOnly(false, false),
			want:         authprotocol.CaptchaRequirement{Login: true, Register: true},
		},
		{
			// 一个类型都没启用时，签发验证码本身就不可能，场景开关再开也没用。
			name:   "未启用任何验证码类型",
			appCfg: &captchadomain.CaptchaAppConfig{RequireForLogin: true, RequireForRegister: true},
			want:   authprotocol.CaptchaRequirement{},
		},
		{
			// 策略强制会越过"没启用任何类型"——如实下发，客户端去取验证码时
			// 会拿到明确错误，而不是在登录时被一个无从解释的拒绝挡住。
			name:         "策略强制且未启用任何类型",
			policyForced: true,
			appCfg:       &captchadomain.CaptchaAppConfig{},
			want:         authprotocol.CaptchaRequirement{Login: true, Register: true},
		},
		{
			// 短信前置验证码是平台级的，与应用配置无关。
			name:   "仅短信前置验证码",
			smsPre: true,
			want:   authprotocol.CaptchaRequirement{SMS: true},
		},
		{
			name:   "读不到应用配置时按未开启处理",
			appCfg: nil,
			want:   authprotocol.CaptchaRequirement{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var policy *authprotocol.Policy
			if tc.policyForced {
				policy = &authprotocol.Policy{RequireCaptcha: true}
			}
			got := ResolveCaptchaRequirement(policy, tc.appCfg, tc.smsPre)
			if got != tc.want {
				t.Fatalf("结论不符：期望 %+v，实际 %+v", tc.want, got)
			}
		})
	}
}

// 入口名是客户端用来索引结论的键，改了名等于把那个入口从客户端视野里抹掉。
func TestCaptchaRequirementLooksUpByEntryName(t *testing.T) {
	requirement := authprotocol.CaptchaRequirement{Login: true, Register: false, SMS: true}

	if !requirement.Required(authprotocol.CaptchaEntryLogin) {
		t.Error("login 入口应当要求验证码")
	}
	if requirement.Required(authprotocol.CaptchaEntryRegister) {
		t.Error("register 入口不应要求验证码")
	}
	if !requirement.Required(authprotocol.CaptchaEntrySMS) {
		t.Error("sms 入口应当要求验证码")
	}
	// 未知入口一律 false：宁可漏要求也不要凭空要求一个客户端拿不到答案的验证码。
	if requirement.Required("something-else") {
		t.Error("未知入口不应要求验证码")
	}
}

// 与 IsCaptchaRequiredForScene 的关系：策略没强制时，两者必须给出同一个答案。
// 折叠函数若哪天绕开了场景判定，这里会红。
func TestResolveCaptchaRequirementMatchesSceneRuleWhenNotForced(t *testing.T) {
	for _, login := range []bool{false, true} {
		for _, register := range []bool{false, true} {
			cfg := &captchadomain.CaptchaAppConfig{
				ImageEnabled:       true,
				RequireForLogin:    login,
				RequireForRegister: register,
			}
			got := ResolveCaptchaRequirement(nil, cfg, false)
			if want := IsCaptchaRequiredForScene(cfg, captchadomain.PurposeLogin); got.Login != want {
				t.Errorf("login=%v register=%v：登录结论 %v，场景判定 %v", login, register, got.Login, want)
			}
			if want := IsCaptchaRequiredForScene(cfg, captchadomain.PurposeRegister); got.Register != want {
				t.Errorf("login=%v register=%v：注册结论 %v，场景判定 %v", login, register, got.Register, want)
			}
		}
	}
}
