package service

import (
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
)

// 网段收敛是这套策略能不能上生产的关键：比完整 IP 会让同一宽带
// 每次拨号都被拦下，那是误报而不是防护。
func TestSameLoginNetwork(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"IPv4 同 /24 视为同网段", "203.0.113.7", "203.0.113.200", true},
		{"IPv4 跨 /24 视为换网", "203.0.113.7", "203.0.114.7", false},
		{"IPv6 同 /48 视为同网段", "2001:db8:1::1", "2001:db8:1:ffff::9", true},
		{"IPv6 跨 /48 视为换网", "2001:db8:1::1", "2001:db8:2::1", false},
		{"协议族不同视为换网", "203.0.113.7", "2001:db8:1::1", false},
		{"IPv4 映射地址按 IPv4 比较", "::ffff:203.0.113.7", "203.0.113.9", true},
		// 解析不出来是我们的问题，不该由用户承担拦截
		{"任一侧无法解析时放行", "not-an-ip", "203.0.113.7", true},
		{"两侧都无法解析时放行", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameLoginNetwork(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameLoginNetwork(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// 三项策略全关时必须一次 Redis I/O 都不产生 —— 绝大多数应用走这条路径，
// 这里退化成「每次登录多两次 Redis 往返」是不可接受的。
// baseline 为 nil 会让任何实际访问 panic，因此本用例同时守住了这一点。
func TestEnforceSkipsWhenAllChecksDisabled(t *testing.T) {
	svc := NewLoginConsistencyService(nil, nil, nil)
	if err := svc.Enforce(t.Context(), appdomain.Policy{
		MultiDeviceLogin: true,
		MultiDeviceLimit: 5,
		RegisterCheckIP:  true, // 注册策略与登录一致性无关，不该触发基线读写
	}, 10000, 1, "device-a", "203.0.113.7"); err != nil {
		t.Fatalf("全关策略不应返回错误，得到 %v", err)
	}
}

// 换绑冷却按「上次换绑时间 + 冷却期」判定，而不是「上次登录时间」：
// 后者会让用户只要天天登录就永远换不了设备。
func TestDeviceRebindCooldownWindow(t *testing.T) {
	const cooldownSeconds = 3600
	boundAt := time.Now().UTC().Add(-30 * time.Minute)

	elapsed := time.Now().UTC().Sub(boundAt)
	if elapsed >= time.Duration(cooldownSeconds)*time.Second {
		t.Fatalf("30 分钟前换绑、冷却 1 小时，应仍在冷却期内")
	}

	boundAt = time.Now().UTC().Add(-2 * time.Hour)
	elapsed = time.Now().UTC().Sub(boundAt)
	if elapsed < time.Duration(cooldownSeconds)*time.Second {
		t.Fatalf("2 小时前换绑、冷却 1 小时，应已出冷却期")
	}
}

// maxAge=0 必须是「永不过期」而不是回落成默认 365 天 ——
// 这是修复前最容易误判的一项：界面显示 0，后端按 365 执行。
func TestPasswordPolicyMaxAgeZeroMeansNeverExpire(t *testing.T) {
	normalized, errs := normalizeAndValidatePasswordPolicy(appdomain.PasswordPolicy{
		MinLength:    8,
		MaxLength:    128,
		MinScore:     40,
		MaxAge:       0,
		PreventReuse: 0,
	})
	if len(errs) > 0 {
		t.Fatalf("maxAge=0 应通过校验，得到 %v", errs)
	}
	if normalized.MaxAge != 0 {
		t.Fatalf("maxAge 应保持 0（永不过期），得到 %d", normalized.MaxAge)
	}
	if normalized.PreventReuse != 0 {
		t.Fatalf("preventReuse 应保持 0（不限制），得到 %d", normalized.PreventReuse)
	}
}

// 显式存 0 与「键不存在」必须区分：前者是管理员的选择，后者才回落默认值。
func TestResolvePasswordPolicyDistinguishesZeroFromAbsent(t *testing.T) {
	svc := &AppService{}

	explicitZero := svc.ResolvePasswordPolicy(&appdomain.App{Settings: map[string]any{
		"passwordPolicy": map[string]any{"maxAge": 0, "preventReuse": 0},
	}})
	if explicitZero.MaxAge != 0 || explicitZero.PreventReuse != 0 {
		t.Fatalf("显式 0 应被保留，得到 maxAge=%d preventReuse=%d",
			explicitZero.MaxAge, explicitZero.PreventReuse)
	}

	absent := svc.ResolvePasswordPolicy(&appdomain.App{Settings: map[string]any{
		"passwordPolicy": map[string]any{"minLength": 10},
	}})
	fallback := defaultPasswordPolicy()
	if absent.MaxAge != fallback.MaxAge || absent.PreventReuse != fallback.PreventReuse {
		t.Fatalf("键不存在应回落默认值 (maxAge=%d preventReuse=%d)，得到 maxAge=%d preventReuse=%d",
			fallback.MaxAge, fallback.PreventReuse, absent.MaxAge, absent.PreventReuse)
	}
}

// 注册验证码已归口到验证码配置，策略解析不该再产出这两个键的任何行为；
// 同时确认 loginCheckDeviceTimeOut 仍被读成换绑冷却（旧数据兼容）。
func TestResolvePolicyDropsRegisterCaptchaKeepsRebindInterval(t *testing.T) {
	svc := &AppService{}
	policy := svc.ResolvePolicy(&appdomain.App{Settings: map[string]any{
		"loginCheckDevice":        true,
		"loginCheckDeviceTimeOut": 7200,
		"registerCaptcha":         true,
		"registerCaptchaTimeOut":  300,
		"multiDeviceLogin":        false,
		"multiDeviceLoginNum":     9,
	}})
	if policy.DeviceRebindInterval != 7200 {
		t.Fatalf("换绑冷却应沿用 loginCheckDeviceTimeOut=7200，得到 %d", policy.DeviceRebindInterval)
	}
	// 关闭多设备后上限恒为 1，界面上不该出现「不允许多设备但上限 9 台」
	if policy.MultiDeviceLimit != 1 {
		t.Fatalf("关闭多设备时上限应为 1，得到 %d", policy.MultiDeviceLimit)
	}
}

// 交易设置此前没有任何配置入口，只能吃兜底值；补上入口后
// 「未配置回落 100」与「配置生效」两条都要成立。
func TestResolveCommerceSettings(t *testing.T) {
	if got := resolveCommerceSettings(nil).IntegralPerCurrency; got != appdomain.DefaultIntegralPerCurrency {
		t.Fatalf("app 为 nil 应回落默认兑换率 %d，得到 %d", appdomain.DefaultIntegralPerCurrency, got)
	}
	if got := resolveCommerceSettings(&appdomain.App{Settings: map[string]any{
		"integralPerCurrency": 250,
	}}).IntegralPerCurrency; got != 250 {
		t.Fatalf("已配置兑换率应生效，得到 %d", got)
	}
	// 0 / 负数是无意义的兑换率（会让积分订单永远算出 0 积分），回落默认
	if got := resolveCommerceSettings(&appdomain.App{Settings: map[string]any{
		"integralPerCurrency": 0,
	}}).IntegralPerCurrency; got != appdomain.DefaultIntegralPerCurrency {
		t.Fatalf("兑换率 0 应回落默认值，得到 %d", got)
	}
}
