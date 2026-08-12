package service

import (
	authprotocol "aegis/internal/domain/authprotocol"
)

// 接入目录 —— /config 下发给客户端的「这个命名空间里有什么」。
//
// 它是**给机器看的**：生成式 SDK 照着它产出方法签名，调试台照着它渲染表单，
// 控制台的接入自检照着它逐条探活。人看的版本在 docs/app-integration.md。
//
// 与 internal/transport/http/router.go 的 appGateway / appGatewayAuthed 两组路由
// 必须逐条对齐，`TestGatewayCatalogMatchesRegisteredRoutes` 钉住这件事 ——
// 目录里多一条，客户端会调一个 404；少一条，那个能力对所有生成式客户端都不存在。
//
// 路径是相对 /api/v1/apps/{appKey} 的，占位符统一写成 {name}。

// gatewayOperations 全量接口目录，顺序即控制台与文档里的展示顺序。
var gatewayOperations = []authprotocol.Operation{
	// ── 协议与能力 ──
	{Key: "config", Method: "GET", Path: "/config", Unwrapped: true,
		Summary: "读取应用能力与当前安全等级规格；任何等级下都免包装可读"},

	// ── 认证生命周期 ──
	{Key: "captcha", Method: "POST", Path: "/captcha", Summary: "按策略签发图形验证码"},
	{Key: "smsCode", Method: "POST", Path: "/auth/sms/code", Summary: "申请短信验证码（purpose: login | register）"},
	{Key: "register", Method: "POST", Path: "/auth/register", Summary: "注册（method: password | sms）"},
	{Key: "login", Method: "POST", Path: "/auth/login", Summary: "登录（method: password | sms）"},
	{Key: "refresh", Method: "POST", Path: "/auth/refresh", Summary: "刷新访问令牌"},
	{Key: "secondFactor", Method: "POST", Path: "/auth/2fa/verify", Summary: "完成登录返回的二次认证挑战"},
	{Key: "logout", Method: "POST", Path: "/auth/logout", Auth: true, Summary: "注销当前会话"},

	// ── 第三方登录 ──
	{Key: "oauthURL", Method: "POST", Path: "/auth/oauth/url", Summary: "取第三方登录授权地址"},
	{Key: "oauthCallback", Method: "GET", Path: "/auth/oauth/callback", Unwrapped: true,
		Summary: "第三方授权回跳落点；由浏览器发起，免包装"},
	{Key: "oauthExchange", Method: "POST", Path: "/auth/oauth/exchange", Summary: "原生 SDK 用第三方 profile 换会话"},
	{Key: "oauthBindURL", Method: "POST", Path: "/auth/oauth/bind/url", Auth: true, Summary: "取第三方账号绑定授权地址"},
	{Key: "oauthBindings", Method: "GET", Path: "/auth/oauth/bindings", Auth: true, Summary: "我已绑定的第三方账号"},
	{Key: "oauthUnbind", Method: "DELETE", Path: "/auth/oauth/bindings/{provider}", Auth: true, Summary: "解绑第三方账号"},

	// ── 邮箱验证码与找回密码 ──
	{Key: "emailCode", Method: "POST", Path: "/auth/email/code", Summary: "发送邮箱验证码"},
	{Key: "emailVerify", Method: "POST", Path: "/auth/email/verify", Summary: "校验邮箱验证码"},
	{Key: "passwordForgot", Method: "POST", Path: "/auth/password/forgot", Summary: "发送密码重置邮件"},
	{Key: "passwordResetVerify", Method: "POST", Path: "/auth/password/reset/verify", Summary: "校验密码重置令牌"},
	{Key: "passwordVerify", Method: "POST", Path: "/auth/password/verify", Auth: true, Summary: "校验当前密码"},
	{Key: "passwordChange", Method: "POST", Path: "/auth/password/change", Auth: true, Summary: "修改密码"},

	// ── Passkey 登录 ──
	{Key: "passkeyOptions", Method: "POST", Path: "/auth/passkey/options", Summary: "取 Passkey 登录参数"},
	{Key: "passkeyLogin", Method: "POST", Path: "/auth/passkey/login", Summary: "Passkey 登录校验"},

	// ── 当前用户 ──
	{Key: "me", Method: "GET", Path: "/me", Auth: true, Summary: "当前登录用户资料"},
	{Key: "profile", Method: "GET", Path: "/me/profile", Auth: true, Summary: "个人资料详情"},
	{Key: "profileUpdate", Method: "PUT", Path: "/me/profile", Auth: true, Summary: "更新个人资料"},
	{Key: "profileConfirm", Method: "POST", Path: "/me/profile/changes/confirm", Auth: true, Summary: "确认敏感资料变更"},
	{Key: "avatarUpload", Method: "POST", Path: "/me/avatar", Auth: true, Upload: true, Summary: "上传头像（multipart/form-data）"},
	{Key: "settings", Method: "GET", Path: "/me/settings", Auth: true, Summary: "读取用户设置"},
	{Key: "settingsUpdate", Method: "PUT", Path: "/me/settings", Auth: true, Summary: "更新用户设置"},
	{Key: "security", Method: "GET", Path: "/me/security", Auth: true, Summary: "账户安全概览"},

	// ── 二次认证 ──
	{Key: "totpEnroll", Method: "POST", Path: "/me/2fa/totp/enroll", Auth: true, Summary: "发起 TOTP 绑定"},
	{Key: "totpEnable", Method: "POST", Path: "/me/2fa/totp/enable", Auth: true, Summary: "启用 TOTP"},
	{Key: "totpDisable", Method: "POST", Path: "/me/2fa/totp/disable", Auth: true, Summary: "关闭 TOTP"},
	{Key: "recoveryCodes", Method: "GET", Path: "/me/2fa/recovery-codes", Auth: true, Summary: "恢复码摘要"},
	{Key: "recoveryCodesCreate", Method: "POST", Path: "/me/2fa/recovery-codes", Auth: true, Summary: "生成恢复码"},
	{Key: "recoveryCodesRegenerate", Method: "POST", Path: "/me/2fa/recovery-codes/regenerate", Auth: true, Summary: "重置恢复码"},

	// ── Passkey 管理 ──
	{Key: "passkeyList", Method: "GET", Path: "/me/passkeys", Auth: true, Summary: "我的 Passkey 列表"},
	{Key: "passkeyRegisterOptions", Method: "POST", Path: "/me/passkeys/options", Auth: true, Summary: "取 Passkey 注册参数"},
	{Key: "passkeyRegister", Method: "POST", Path: "/me/passkeys", Auth: true, Summary: "完成 Passkey 注册"},
	{Key: "passkeyDelete", Method: "DELETE", Path: "/me/passkeys/{credentialId}", Auth: true, Summary: "删除 Passkey"},

	// ── 会话与审计 ──
	{Key: "sessions", Method: "GET", Path: "/me/sessions", Auth: true, Summary: "当前在线会话列表"},
	{Key: "sessionRevoke", Method: "DELETE", Path: "/me/sessions/{tokenHash}", Auth: true, Summary: "踢出单个会话"},
	{Key: "sessionRevokeAll", Method: "POST", Path: "/me/sessions/revoke-all", Auth: true, Summary: "踢出全部会话"},
	{Key: "loginAudits", Method: "GET", Path: "/me/audits/login", Auth: true, Summary: "我的登录记录"},
	{Key: "sessionAudits", Method: "GET", Path: "/me/audits/sessions", Auth: true, Summary: "我的会话记录"},

	// ── 签到 / 积分 / 排行榜 ──
	{Key: "signinStatus", Method: "GET", Path: "/signin/status", Auth: true, Summary: "签到状态"},
	{Key: "signin", Method: "POST", Path: "/signin", Auth: true, Summary: "签到"},
	{Key: "signinHistory", Method: "GET", Path: "/signin/history", Auth: true, Summary: "签到历史"},
	{Key: "pointsOverview", Method: "GET", Path: "/points/overview", Auth: true, Summary: "积分与经验概览"},
	{Key: "pointsLevel", Method: "GET", Path: "/points/level", Auth: true, Summary: "我的等级"},
	{Key: "pointsLevels", Method: "GET", Path: "/points/levels", Auth: true, Summary: "等级配置"},
	{Key: "integralTransactions", Method: "GET", Path: "/points/integral-transactions", Auth: true, Summary: "积分流水"},
	{Key: "experienceTransactions", Method: "GET", Path: "/points/experience-transactions", Auth: true, Summary: "经验流水"},
	{Key: "leaderboardSummary", Method: "GET", Path: "/leaderboard/summary", Auth: true, Summary: "排行榜综合概览"},
	{Key: "leaderboardMe", Method: "GET", Path: "/leaderboard/me", Auth: true, Summary: "我在各榜单的排名"},
	{Key: "leaderboardPoints", Method: "GET", Path: "/leaderboard/points/{type}", Auth: true, Summary: "积分榜（type: integral | experience | level）"},
	{Key: "leaderboardSignIn", Method: "GET", Path: "/leaderboard/signin/{type}", Auth: true, Summary: "签到榜（type: today | consecutive | monthly）"},

	// ── 站内信 ──
	{Key: "notifications", Method: "GET", Path: "/notifications", Auth: true, Summary: "站内信列表"},
	{Key: "notificationsUnread", Method: "GET", Path: "/notifications/unread-count", Auth: true, Summary: "未读数"},
	{Key: "notificationRead", Method: "POST", Path: "/notifications/read", Auth: true, Summary: "标记已读"},
	{Key: "notificationReadBatch", Method: "POST", Path: "/notifications/read-batch", Auth: true, Summary: "批量标记已读"},
	{Key: "notificationReadAll", Method: "POST", Path: "/notifications/read-all", Auth: true, Summary: "全部标记已读"},
	{Key: "notificationClear", Method: "POST", Path: "/notifications/clear", Auth: true, Summary: "清空站内信"},
	{Key: "notificationDelete", Method: "DELETE", Path: "/notifications/{notificationId}", Auth: true, Summary: "删除一条站内信"},

	// ── 钱包 / 会员 / 支付 ──
	{Key: "wallet", Method: "GET", Path: "/wallet", Auth: true, Summary: "我的余额"},
	{Key: "walletTransactions", Method: "GET", Path: "/wallet/transactions", Auth: true, Summary: "余额流水"},
	{Key: "walletConsume", Method: "POST", Path: "/wallet/consume", Auth: true, Summary: "余额消费"},
	{Key: "vipPlans", Method: "GET", Path: "/vip/plans", Auth: true, Summary: "会员套餐"},
	{Key: "vipStatus", Method: "GET", Path: "/vip/status", Auth: true, Summary: "我的会员状态"},
	{Key: "vipTransactions", Method: "GET", Path: "/vip/transactions", Auth: true, Summary: "会员流水"},
	{Key: "vipPurchase", Method: "POST", Path: "/vip/purchase", Auth: true, Summary: "购买会员"},
	{Key: "payOrders", Method: "GET", Path: "/pay/orders", Auth: true, Summary: "我的订单"},
	{Key: "payOrderCreate", Method: "POST", Path: "/pay/orders", Auth: true, Summary: "创建支付订单"},
	{Key: "payOrderDetail", Method: "GET", Path: "/pay/orders/{orderNo}", Auth: true, Summary: "订单详情"},

	// ── 存储 ──
	{Key: "storageUpload", Method: "POST", Path: "/storage/upload", Auth: true, Upload: true, Summary: "上传文件（multipart/form-data）"},
	{Key: "storageObjectLink", Method: "POST", Path: "/storage/object-link", Auth: true, Summary: "换取对象访问链接"},

	// ── 工单（用户自助）──
	{Key: "tickets", Method: "GET", Path: "/tickets", Auth: true, Summary: "我的工单"},
	{Key: "ticketCreate", Method: "POST", Path: "/tickets", Auth: true, Summary: "提交工单"},
	{Key: "ticketCategories", Method: "GET", Path: "/tickets/categories", Auth: true, Summary: "工单分类"},
	{Key: "ticketAttachment", Method: "POST", Path: "/tickets/attachments", Auth: true, Upload: true, Summary: "上传工单附件（multipart/form-data）"},
	{Key: "ticketDetail", Method: "GET", Path: "/tickets/{ticketId}", Auth: true, Summary: "工单详情"},
	{Key: "ticketReply", Method: "POST", Path: "/tickets/{ticketId}/replies", Auth: true, Summary: "追问"},
	{Key: "ticketRating", Method: "POST", Path: "/tickets/{ticketId}/rating", Auth: true, Summary: "评价"},
	{Key: "ticketCancel", Method: "POST", Path: "/tickets/{ticketId}/cancel", Auth: true, Summary: "撤单"},

	// ── 内容与版本（免登录）──
	{Key: "banners", Method: "GET", Path: "/banners", Summary: "轮播图"},
	{Key: "bannerClick", Method: "POST", Path: "/banners/{bannerId}/click", Summary: "轮播图点击上报"},
	{Key: "notices", Method: "GET", Path: "/notices", Summary: "公告"},
	{Key: "versionCheck", Method: "GET", Path: "/version/check", Summary: "版本检查与更新"},
}

// gatewayErrors 机器可读的错误码目录。
//
// 生成式 SDK 据此把业务码映射成分类异常，而不是拿 message 做字符串匹配 ——
// 后者会在任何一次文案调整时静默失效，而且中文文案对多语言客户端毫无意义。
// Recovery 明确告诉客户端「这个错能不能自动重试、重试前要先做什么」。
var gatewayErrors = []authprotocol.ErrorDescriptor{
	{Code: 40071, Name: "TIMESTAMP_INVALID", Message: "请求时间戳无效或已过期",
		Recovery: authprotocol.RecoverySyncClock,
		Hint:     "用 /config 的 serverTime 算出与服务端的偏移量，之后的请求带校准后的时间戳"},
	{Code: 40072, Name: "NONCE_INVALID", Message: "请求 nonce 无效",
		Recovery: authprotocol.RecoveryNewNonce,
		Hint:     "signed 档 8–128 字符；sealed 档必须是 24 字节随机值的 base64url"},
	{Code: 40073, Name: "CLIENT_KEY_INVALID", Message: "客户端临时公钥无效",
		Recovery: authprotocol.RecoveryNone, Hint: "X25519 公钥必须是 32 字节的 base64url"},
	{Code: 40074, Name: "TRANSPORT_KEY_UNUSABLE", Message: "传输密钥不存在、已撤销或已过期",
		Recovery: authprotocol.RecoveryRefreshConfig,
		Hint:     "重新拉 /config 取 activeKeyId，最多自动重试一次"},
	{Code: 40075, Name: "KEY_AGREEMENT_FAILED", Message: "密钥协商失败", Recovery: authprotocol.RecoveryNone},
	{Code: 40076, Name: "PAYLOAD_MALFORMED", Message: "加密载荷格式无效",
		Recovery: authprotocol.RecoveryNone, Hint: "密文必须是无 padding 的 base64url"},
	{Code: 40077, Name: "PAYLOAD_AUTH_FAILED", Message: "加密载荷认证失败",
		Recovery: authprotocol.RecoveryNone, Hint: "核对 AAD 七行拼接与 HKDF 盐"},
	{Code: 40078, Name: "PAYLOAD_INVALID", Message: "载荷无效或超过限制",
		Recovery: authprotocol.RecoveryNone,
		Hint:     "无请求体的方法要把密文放在 " + authprotocol.SealedPayloadParam + " 查询参数里"},
	{Code: 40084, Name: "APP_KEY_MISMATCH", Message: "AppKey 与路由不一致",
		Recovery: authprotocol.RecoveryNone, Hint: "去掉头/字段，或改成与路径一致"},
	{Code: 40100, Name: "UNAUTHENTICATED", Message: "缺少或无效的访问令牌",
		Recovery: authprotocol.RecoveryRefreshToken},
	{Code: 40174, Name: "SIGNATURE_MALFORMED", Message: "请求签名格式无效",
		Recovery: authprotocol.RecoveryNone, Hint: "必须是 v2= 加 64 位十六进制"},
	{Code: 40175, Name: "SIGNATURE_MISMATCH", Message: "请求签名校验失败",
		Recovery: authprotocol.RecoveryNone,
		Hint:     "核对 canonical 的换行与字段顺序，确认用的是最新 appSecret"},
	{Code: 40176, Name: "SIGNATURE_VERSION_TOO_LOW", Message: "带 query 的请求必须使用 v2 签名",
		Recovery: authprotocol.RecoveryNone, Hint: "待签名字符串在 path 之后加一行原样 query"},
	{Code: 40370, Name: "METHOD_DISABLED", Message: "当前应用未启用该认证方式",
		Recovery: authprotocol.RecoveryRefreshConfig},
	{Code: 40372, Name: "TOKEN_APP_MISMATCH", Message: "访问令牌不属于该应用",
		Recovery: authprotocol.RecoveryReauth, Hint: "用本应用的登录结果换取令牌"},
	{Code: 40391, Name: "OAUTH_BIND_ONLY", Message: "该渠道仅开放绑定，未开放直接登录",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40393, Name: "OAUTH_NOT_BOUND", Message: "第三方账号未绑定且渠道未开放自动注册",
		Recovery: authprotocol.RecoveryNone, Hint: "引导用户先用已有账号登录再绑定"},
	{Code: 40394, Name: "PHONE_NOT_REGISTERED", Message: "手机号尚未注册且应用未开放短信注册",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40470, Name: "APP_NOT_FOUND", Message: "应用不存在或已停用", Recovery: authprotocol.RecoveryNone},
	{Code: 40970, Name: "NONCE_REPLAYED", Message: "请求 nonce 已使用", Recovery: authprotocol.RecoveryNewNonce},
	{Code: 42670, Name: "UPGRADE_REQUIRED", Message: "该应用要求使用加密载荷",
		Recovery: authprotocol.RecoveryRefreshConfig, Hint: "应用已升到 sealed 档，不能再发明文"},
	{Code: 50372, Name: "SIGNING_SECRET_MISSING", Message: "尚未签发应用密钥",
		Recovery: authprotocol.RecoveryNone, Hint: "让管理员在控制台轮换一次应用密钥"},
}

// GatewayOperations 供 transport 层做「目录与路由是否一致」的漂移检查。
func GatewayOperations() []authprotocol.Operation {
	items := make([]authprotocol.Operation, len(gatewayOperations))
	copy(items, gatewayOperations)
	return items
}

// buildGatewayCatalog 把相对路径展开成完整路径，产出 /config 的三块目录数据。
func buildGatewayCatalog(base string) (map[string]string, []authprotocol.Operation) {
	endpoints := make(map[string]string, len(gatewayOperations))
	operations := make([]authprotocol.Operation, 0, len(gatewayOperations))
	for _, item := range gatewayOperations {
		full := base + item.Path
		// Endpoints 是给「手写客户端」用的简表，同一个键只保留一条；
		// 键按资源命名，同路径不同方法（如 payOrders / payOrderCreate）各占一个键。
		endpoints[item.Key] = full
		item.Path = full
		operations = append(operations, item)
	}
	return endpoints, operations
}
