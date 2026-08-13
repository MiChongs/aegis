package middleware

import "strings"

// ─────────────────────────────────────────────────────────────────────────
// 防重复请求的路由策略表
//
// 这张表存在的理由，是旧实现里那条**默认拦截**的规则：
// 任何非 GET 请求都会被算一次指纹（method + path + IP + token 前缀 + body 前 2KB），
// 5 秒内指纹相同就回 403「重复请求」。
//
// 它的隐含假设是「两次一模一样的请求必然是重复提交」，而这个假设是错的：
// 大量接口**天生就要被原样重复调用**。最典型的是验证码 ——
// 点一次「换一张」发的就是一模一样的请求体，于是第二次点击稳定收到 403。
// 短信重发、验证码校验（同一个错码再试一次）、样张预览、POST 检索
// 全是同一类。这些接口不是漏配了什么，它们本来就该允许重复。
//
// 反过来，指纹对真正要防的东西又几乎无效：攻击者改一个字节就绕过去了，
// 而用户在第 6 秒重复点一次「创建订单」照样能创建两单。
// 也就是说这条默认规则**误伤了它不该管的，又没挡住它该挡的**。
//
// 所以判据从「猜」改成「声明」：
//
//	off        —— 明确允许原样重复。不做任何去重
//	idempotent —— 默认档。带了 Idempotency-Key 就走幂等重放，没带就直通
//	guarded    —— 副作用不可撤销（下单 / 退款 / 调账 / 发放）。
//	              带 Idempotency-Key 走幂等；没带则用指纹兜底，
//	              且返回 409 + Retry-After 而不是 403
//
// **未登记的路由默认是 idempotent，即不做指纹去重。** 这与旧实现相反，
// 也是本次改动的核心：新增一个接口时忘记登记，后果是"少了一层兜底"，
// 而不是"这个接口在生产上随机返回 403"。前者能靠 code review 发现，
// 后者只会变成一张没人能复现的工单。
// ─────────────────────────────────────────────────────────────────────────

// ReplayPolicy 一条路由的防重复处置
type ReplayPolicy uint8

const (
	// PolicyIdempotent 默认档：尊重 Idempotency-Key，不主动去重
	PolicyIdempotent ReplayPolicy = iota
	// PolicyOff 明确允许原样重复，连 Idempotency-Key 都不处理
	PolicyOff
	// PolicyGuarded 副作用不可撤销，缺少幂等键时用指纹兜底
	PolicyGuarded
)

func (p ReplayPolicy) String() string {
	switch p {
	case PolicyOff:
		return "off"
	case PolicyGuarded:
		return "guarded"
	default:
		return "idempotent"
	}
}

// replayRule 一条按路径段锚定的规则。
//
// 匹配单位是**路径段**而不是子串：`strings.Contains(path, "/captcha")` 会把
// `/api/admin/apps/:appkey/captcha-config` 一并放行，而那是个写配置的接口。
// 段匹配里 `:` 开头吃一段（任意值），`*` 吃剩余的全部（含零段）。
type replayRule struct {
	methods []string // 空表示所有方法
	segs    []string
	policy  ReplayPolicy
	reason  string
}

// replayRules 有序规则表，**先命中先生效**，因此更具体的规则必须写在前面。
var replayRules = []replayRule{
	// ── 验证码：整族允许原样重复 ──
	// 「换一张」「重发」「同一个错码再试一次」都是合法的重复调用。
	// 它们的滥用防护归限流（Firewall）与验证码服务自己的发送频率闸门，
	// 不归防重放 —— 用 403「重复请求」回答一次正常的刷新，
	// 使用者只会认为系统坏了。
	{segs: split("/api/captcha/*"), policy: PolicyOff, reason: "验证码生成与校验天然可重复"},
	{segs: split("/api/admin/captcha/*"), policy: PolicyOff, reason: "管理端验证码同上"},

	// ── 应用接入网关整段让开 ──
	// 网关自带强制时间窗、一次性 Nonce 与 HMAC/AEAD 完整性，严格强于这里的兜底。
	// 而且 sealed 档的密文每次都不同（AEAD 用随机 nonce），
	// 指纹与幂等请求哈希在这一层拿到的都是密文，两者在这里都算不出正确结论。
	// 这条必须排在下面所有 /api/v1/apps 规则之前，否则那些规则会先命中。
	{segs: split("/api/v1/apps/*"), policy: PolicyOff, reason: "接入网关自带更强的防重放"},

	// ── 一次性口令与通知重发 ──
	// 「没收到，再发一次」是用户的正常动作。频率限制由业务层的冷却时间负责，
	// 那里能给出「还需等待 47 秒」，而这里只能给出一句「重复请求」。
	{segs: split("/api/auth/sms/*"), policy: PolicyOff, reason: "短信验证码重发"},
	{segs: split("/api/auth/email/code"), policy: PolicyOff, reason: "邮件验证码重发"},
	{segs: split("/api/user/2fa/challenge"), policy: PolicyOff, reason: "二次认证挑战可重发"},

	// ── 只读语义的 POST ──
	// 用 POST 只是因为参数放不进 query，它们没有副作用，重复调用无害。
	{segs: split("/api/admin/system/authz/explain"), policy: PolicyOff, reason: "权限判定解释，只读"},
	{segs: split("/api/admin/system/captcha/preview"), policy: PolicyOff, reason: "样张预览，只读"},
	{segs: split("/api/admin/apps/:appkey/captcha-config/preview"), policy: PolicyOff, reason: "样张预览，只读"},
	{segs: split("/api/admin/apps/:appkey/auth-protocol/selftest"), policy: PolicyOff, reason: "接入自检要能连点"},
	{segs: split("/api/admin/risk/simulate"), policy: PolicyOff, reason: "风控模拟器，只读试跑"},
	{segs: split("/api/admin/apps/:appkey/functions/:name/dry-run"), policy: PolicyOff, reason: "远程函数试跑，读真写假"},
	{segs: split("/api/admin/egress/probe"), policy: PolicyOff, reason: "出海网关探测，只读"},
	{segs: split("/api/admin/egress/resolve"), policy: PolicyOff, reason: "路由解释，只读"},

	// ── 登录与令牌 ──
	// 同一个人用同一个错密码连试两次是完全正常的行为（多半是键盘布局问题），
	// 把第二次判成「重复请求」会让他以为账号被锁了。
	// 登录的滥用防护是登录守卫 + 限流，两者都会给出准确的原因。
	{segs: split("/api/auth/login"), policy: PolicyOff, reason: "重复登录尝试不是重放"},
	{segs: split("/api/admin/auth/login"), policy: PolicyOff, reason: "同上"},
	{segs: split("/api/auth/refresh"), policy: PolicyOff, reason: "刷新令牌可并发重试"},
	{segs: split("/api/auth/logout"), policy: PolicyOff, reason: "登出天然幂等"},
	{segs: split("/api/admin/auth/logout"), policy: PolicyOff, reason: "同上"},

	// ── 资金与不可撤销的发放：这里才需要兜底 ──
	{methods: []string{"POST"}, segs: split("/api/pay/*"), policy: PolicyGuarded, reason: "支付下单"},
	{methods: []string{"POST"}, segs: split("/api/admin/payments/:id/refund"), policy: PolicyGuarded, reason: "退款不可撤销"},
	{methods: []string{"POST"}, segs: split("/api/admin/apps/:appkey/wallet/adjust"), policy: PolicyGuarded, reason: "管理员调账不可撤销"},
	{methods: []string{"POST"}, segs: split("/api/admin/apps/:appkey/users/:userid/wallet/adjust"), policy: PolicyGuarded, reason: "同上"},
	{methods: []string{"POST"}, segs: split("/api/admin/apps/:appkey/vip/grant"), policy: PolicyGuarded, reason: "会员发放"},
	{methods: []string{"POST"}, segs: split("/api/admin/apps/:appkey/points/grant"), policy: PolicyGuarded, reason: "积分发放"},
}

// ResolveReplayPolicy 返回某条请求的处置。未登记的路由回落到 PolicyIdempotent。
func ResolveReplayPolicy(method, path string) (ReplayPolicy, string) {
	segs := split(path)
	upper := strings.ToUpper(method)
	for _, rule := range replayRules {
		if !rule.matchMethod(upper) {
			continue
		}
		if rule.matchPath(segs) {
			return rule.policy, rule.reason
		}
	}
	return PolicyIdempotent, ""
}

func (r replayRule) matchMethod(method string) bool {
	if len(r.methods) == 0 {
		return true
	}
	for _, m := range r.methods {
		if m == method {
			return true
		}
	}
	return false
}

func (r replayRule) matchPath(segs []string) bool {
	for i, want := range r.segs {
		if want == "*" {
			return true // 通配吃掉剩余的全部，含零段
		}
		if i >= len(segs) {
			return false
		}
		if strings.HasPrefix(want, ":") {
			continue // 参数段吃一段，不比较内容
		}
		if !strings.EqualFold(want, segs[i]) {
			return false
		}
	}
	// 规则用完而路径还有剩余：不算命中，否则 /api/captcha 会匹配上
	// /api/captcha-config 之外的任何更深的路径
	return len(segs) == len(r.segs)
}

func split(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
