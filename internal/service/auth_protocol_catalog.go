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
	{Key: "login", Method: "POST", Path: "/auth/login", Summary: "登录（method: password | sms | cardkey）"},
	{Key: "refresh", Method: "POST", Path: "/auth/refresh", Summary: "刷新访问令牌"},
	{Key: "secondFactor", Method: "POST", Path: "/auth/2fa/verify", Summary: "完成登录返回的二次认证挑战"},
	{Key: "logout", Method: "POST", Path: "/auth/logout", Auth: true, Summary: "注销当前会话"},

	// ── 网页扫码登录 ──
	{Key: "qrLoginCreate", Method: "POST", Path: "/auth/qr/create", Summary: "网页申请扫码登录票据（由网站服务端代为调用）"},
	{Key: "qrLoginPoll", Method: "POST", Path: "/auth/qr/poll", Summary: "网页轮询扫码状态，确认后领取会话"},
	{Key: "qrLoginScan", Method: "POST", Path: "/auth/qr/scan", Auth: true, Summary: "移动端扫码，返回发起端设备信息"},
	{Key: "qrLoginConfirm", Method: "POST", Path: "/auth/qr/confirm", Auth: true, Summary: "移动端确认网页登录"},
	{Key: "qrLoginCancel", Method: "POST", Path: "/auth/qr/cancel", Auth: true, Summary: "移动端拒绝网页登录"},

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

	// ── 卡密 ──
	//
	// 授权卡登录不在这里：它是 /auth/login 的一档 method（cardkey），
	// 加登录方式不加路由，客户端也就不需要认识第二条登录入口。
	{Key: "cardKeyRedeem", Method: "POST", Path: "/card-keys/redeem", Auth: true,
		Summary: "兑换卡密（会员 / 积分 / 经验 / 余额 / 抽奖次数 / 设备位）"},
	{Key: "cardKeyMine", Method: "GET", Path: "/card-keys/mine", Auth: true,
		Summary: "我名下的授权卡：授权到期时间与已绑定设备数"},

	// ── 激励广告 ──
	//
	// 一次观看由两方确认：广告平台的服务端回调（不在网关里）与这里的上报，靠 transId 对上。
	// 先拉状态拿到 userId 与各场景的剩余次数，再加载广告；看完后带 transId 上报，
	// 结果是 pending 时用同一个 transId 再报一次即可轮询。
	{Key: "rewardedAdStatus", Method: "GET", Path: "/ads/rewarded", Auth: true,
		Summary: "激励广告状态：各奖励场景、今日剩余次数、冷却，以及传给广告 SDK 的用户标识"},
	{Key: "rewardedAdClaim", Method: "POST", Path: "/ads/rewarded/claim", Auth: true,
		Summary: "上报一次看完的激励广告并领取奖励（同一 transId 可重复上报以轮询结果）"},
	{Key: "rewardedAdRecords", Method: "GET", Path: "/ads/rewarded/records", Auth: true,
		Summary: "我的激励广告观看与领奖记录"},

	// ── 当前用户 ──
	{Key: "me", Method: "GET", Path: "/me", Auth: true, Summary: "当前登录用户资料"},
	{Key: "profile", Method: "GET", Path: "/me/profile", Auth: true, Summary: "个人资料详情"},
	{Key: "profileUpdate", Method: "PUT", Path: "/me/profile", Auth: true, Summary: "更新个人资料"},
	{Key: "profileConfirm", Method: "POST", Path: "/me/profile/changes/confirm", Auth: true, Summary: "确认敏感资料变更"},
	{Key: "avatarUpload", Method: "POST", Path: "/me/avatar", Auth: true, Upload: true, Summary: "上传头像（multipart/form-data，可带 crop_* 裁剪框）"},
	{Key: "avatarRemove", Method: "DELETE", Path: "/me/avatar", Auth: true, Summary: "移除头像，回到默认头像"},
	{Key: "avatarHistory", Method: "GET", Path: "/me/avatar/history", Auth: true, Summary: "头像历史"},
	{Key: "avatarRestore", Method: "POST", Path: "/me/avatar/restore", Auth: true, Summary: "恢复历史头像"},
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

	// ── 已授权的第三方应用（经 OAuth2 授权服务器登录过的客户端）──
	{Key: "oauth2Grants", Method: "GET", Path: "/me/oauth2/grants", Auth: true, Summary: "已授权的第三方应用"},
	{Key: "oauth2GrantRevoke", Method: "DELETE", Path: "/me/oauth2/grants/{clientId}", Auth: true, Summary: "取消对第三方应用的授权"},

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
	{Key: "vipTrial", Method: "POST", Path: "/vip/trial", Auth: true, Summary: "领取试用会员（一人一次，资格见 /vip/status 的 trialOffer）"},
	{Key: "payOrders", Method: "GET", Path: "/pay/orders", Auth: true, Summary: "我的订单"},
	{Key: "payOrderCreate", Method: "POST", Path: "/pay/orders", Auth: true, Summary: "创建支付订单"},
	{Key: "payOrderDetail", Method: "GET", Path: "/pay/orders/{orderNo}", Auth: true, Summary: "订单详情"},

	// ── 存储 ──
	{Key: "storageUpload", Method: "POST", Path: "/storage/upload", Auth: true, Upload: true, Summary: "上传文件（multipart/form-data）"},
	{Key: "storageObjectLink", Method: "POST", Path: "/storage/object-link", Auth: true, Summary: "换取对象访问链接"},

	// ── 用户云存储 ──
	//
	// 每个用户一块私有空间，按「命名空间 / 键」存放任意文档。先拉 /cloud 看开没开、
	// 能不能写、限制是多少；写入带 ifRevision 做乐观并发，冲突时（40965）重新读取、
	// 合并后再写。删除默认进回收站，保留期内可恢复。
	{Key: "cloudStatus", Method: "GET", Path: "/cloud", Auth: true,
		Summary: "云存储状态：是否开放、是否可写、用量与配额、各命名空间用量、限制"},
	{Key: "cloudItems", Method: "GET", Path: "/cloud/items", Auth: true,
		Summary: "我的云存储条目（不含内容；status=deleted 查看回收站）"},
	{Key: "cloudItem", Method: "GET", Path: "/cloud/items/{namespace}/{key}", Auth: true,
		Summary: "读取条目与内容（revision 指定历史修订；超过内联上限时只回元数据）"},
	{Key: "cloudItemPut", Method: "PUT", Path: "/cloud/items/{namespace}/{key}", Auth: true,
		Summary: "写入条目（ifRevision 乐观并发：省略为覆盖，0 为仅创建）"},
	{Key: "cloudItemDelete", Method: "DELETE", Path: "/cloud/items/{namespace}/{key}", Auth: true,
		Summary: "删除条目（默认移入回收站，permanent=true 直接清除）"},
	{Key: "cloudItemUpload", Method: "POST", Path: "/cloud/items/{namespace}/{key}/upload", Auth: true, Upload: true,
		Summary: "以文件上传写入条目（multipart/form-data，大内容与二进制）"},
	{Key: "cloudItemRestore", Method: "POST", Path: "/cloud/items/{namespace}/{key}/restore", Auth: true,
		Summary: "从回收站恢复条目"},
	{Key: "cloudItemRevisions", Method: "GET", Path: "/cloud/items/{namespace}/{key}/revisions", Auth: true,
		Summary: "条目的留存修订"},
	{Key: "cloudItemRollback", Method: "POST", Path: "/cloud/items/{namespace}/{key}/rollback", Auth: true,
		Summary: "回滚到某个修订（生成一个新修订）"},
	{Key: "cloudItemLink", Method: "POST", Path: "/cloud/items/{namespace}/{key}/link", Auth: true,
		Summary: "换取条目内容的短时下载地址"},

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
	{Key: "versionCheck", Method: "GET", Path: "/version/check", Summary: "版本检查与更新（旧形状，新接入请用 releaseCheck）"},

	// ── 发布中心（免登录；带令牌时按用户定向）──
	{Key: "releaseCheck", Method: "GET", Path: "/releases/check", Summary: "检测更新：定向、灰度、按 ABI 选包、跨版本合并说明"},
	{Key: "releaseLatest", Method: "GET", Path: "/releases/latest", Summary: "当前可获取的最新版本（未登录仅公开且已全量的版本）"},
	{Key: "releaseHistory", Method: "GET", Path: "/releases", Summary: "可见的版本历史"},
	{Key: "releaseEvent", Method: "POST", Path: "/releases/events", Summary: "更新漏斗上报：downloaded / installed / failed / dismissed"},
	{Key: "releaseChannels", Method: "GET", Path: "/releases/channels", Auth: true, Summary: "可自助加入的发布渠道"},
	{Key: "releaseChannelJoin", Method: "POST", Path: "/releases/channels/{code}/join", Auth: true, Summary: "加入发布渠道"},
	{Key: "releaseChannelLeave", Method: "POST", Path: "/releases/channels/{code}/leave", Auth: true, Summary: "退出发布渠道"},
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
	// ── 试用期会员 ──
	// 判据与 /vip/status 里 trialOffer.reason 一一对应：客户端拿到 403 之后
	// 不必再查一次状态就知道该显示什么，也不必对中文文案做字符串匹配。
	{Code: 40040, Name: "TRIAL_DEVICE_REQUIRED", Message: "领取试用需要携带设备标识",
		Recovery: authprotocol.RecoveryReauth,
		Hint:     "该应用的试用限制为一台设备一次，登录时必须带上设备标识（deviceId / markcode）"},
	{Code: 40373, Name: "TRIAL_ALREADY_CLAIMED", Message: "试用资格已使用",
		Recovery: authprotocol.RecoveryNone, Hint: "试用一人一次，改为引导购买正式套餐"},
	{Code: 40374, Name: "TRIAL_MEMBER_ACTIVE", Message: "当前已是会员，无需领取试用",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40375, Name: "TRIAL_DEVICE_CLAIMED", Message: "该设备已领取过试用",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40376, Name: "TRIAL_PLAN_NOT_PURCHASABLE", Message: "试用套餐只能领取，不能购买",
		Recovery: authprotocol.RecoveryNone, Hint: "kind=trial 的套餐走 /vip/trial，不要传给 /vip/purchase"},
	// ── 卡密 ──
	// 每个判据一个码，客户端据此分支。文案区分得比较细是有原因的：
	// 「已作废」「已用过」「已过期」对用户是三件完全不同的事，
	// 合成一句「卡密无效」的结果是客服无从判断该补发、该解释、还是该退款。
	{Code: 40340, Name: "CARD_KEY_DISABLED", Message: "该卡密已被作废",
		Recovery: authprotocol.RecoveryNone, Hint: "运营主动作废了这张卡或整个批次，引导用户联系客服"},
	{Code: 40341, Name: "CARD_KEY_USED", Message: "该卡密已被使用",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40342, Name: "CARD_KEY_EXPIRED", Message: "该卡密已过期",
		Recovery: authprotocol.RecoveryNone, Hint: "授权卡的授权期已结束，引导续期"},
	{Code: 40343, Name: "CARD_KEY_DEVICE_LIMIT", Message: "该卡可绑定的设备数已满",
		Recovery: authprotocol.RecoveryNone,
		Hint:     "在其它设备上退出，或让管理员在控制台解绑一台；请求必须带设备标识（deviceId）"},
	{Code: 40344, Name: "CARD_KEY_BOUND_OTHER", Message: "该卡密已绑定其它账号",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40345, Name: "CARD_KEY_KIND_MISMATCH", Message: "卡密类型不符",
		Recovery: authprotocol.RecoveryNone, Hint: "授权卡走 /auth/login 的 cardkey 方式，兑换卡走 /card-keys/redeem"},
	{Code: 40346, Name: "CARD_KEY_NO_LOGIN_CARD", Message: "名下没有可加设备位的授权卡",
		Recovery: authprotocol.RecoveryNone},
	// ── 激励广告 ──
	// 只有「这次上报本身不成立」才是错误；限额、冷却等「看了但不发」的结论
	// 在 /ads/rewarded/claim 的返回体里（status=rejected + reason），不走错误码。
	{Code: 40350, Name: "REWARDED_AD_DISABLED", Message: "激励广告暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "管理员没有启用激励广告，入口应当整个隐藏"},
	{Code: 40351, Name: "REWARDED_AD_SCENE_UNAVAILABLE", Message: "奖励场景不存在或已停用",
		Recovery: authprotocol.RecoveryNone, Hint: "重新拉一次 /ads/rewarded，按最新的场景列表展示"},
	{Code: 40352, Name: "REWARDED_AD_USER_MISMATCH", Message: "这次观看不属于当前账号",
		Recovery: authprotocol.RecoveryNone, Hint: "SDK 的 userId 必须用 /ads/rewarded 下发的那一个；切换账号后要重建广告对象"},
	// ── 用户云存储 ──
	{Code: 40360, Name: "CLOUD_STORAGE_DISABLED", Message: "云存储暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "管理员没有启用云存储，入口应当整个隐藏（/cloud 的 enabled 为 false）"},
	{Code: 40361, Name: "CLOUD_STORAGE_FROZEN", Message: "云存储已被冻结，暂时只能读取",
		Recovery: authprotocol.RecoveryNone, Hint: "管理员冻结了该账号的云存储；读取与下载照常，写入、删除、回滚都会被拒绝"},
	{Code: 40362, Name: "CLOUD_NAMESPACE_FORBIDDEN", Message: "该命名空间不允许写入",
		Recovery: authprotocol.RecoveryNone, Hint: "应用限定了命名空间目录，可写的见 /cloud 的 catalog"},
	{Code: 40465, Name: "CLOUD_ITEM_NOT_FOUND", Message: "条目不存在", Recovery: authprotocol.RecoveryNone},
	{Code: 40466, Name: "CLOUD_REVISION_NOT_FOUND", Message: "修订不存在或已被清理",
		Recovery: authprotocol.RecoveryNone, Hint: "超出保留数的旧修订会在写入时被裁掉，先拉 revisions 再选"},
	{Code: 40965, Name: "CLOUD_REVISION_CONFLICT", Message: "条目已被其它设备修改",
		Recovery: authprotocol.RecoveryNone, Hint: "重新读取条目拿到最新 revision，与本地合并后再带新的 ifRevision 写入"},
	{Code: 40966, Name: "CLOUD_NOT_IN_TRASH", Message: "条目不在回收站中", Recovery: authprotocol.RecoveryNone},
	{Code: 41360, Name: "CLOUD_QUOTA_EXCEEDED", Message: "云存储空间不足",
		Recovery: authprotocol.RecoveryNone, Hint: "配额计入全部留存修订与回收站；清空回收站或删除条目可以腾出空间"},
	{Code: 41361, Name: "CLOUD_ITEM_TOO_LARGE", Message: "单个条目超过上限",
		Recovery: authprotocol.RecoveryNone, Hint: "上限见 /cloud 的 limits.maxItemBytes；JSON 写入另受 limits.jsonWriteBytes 约束，更大的内容走 upload"},
	{Code: 41362, Name: "CLOUD_ITEM_LIMIT", Message: "条目数量已达上限", Recovery: authprotocol.RecoveryNone},
	{Code: 42260, Name: "CLOUD_INVALID_NAMESPACE", Message: "命名空间格式无效",
		Recovery: authprotocol.RecoveryNone, Hint: "小写字母或数字开头，可含 . _ -，至多 64 位"},
	{Code: 42261, Name: "CLOUD_INVALID_KEY", Message: "条目键格式无效",
		Recovery: authprotocol.RecoveryNone, Hint: "字母或数字开头，可含 . _ -，至多 128 位，不含斜杠"},
	{Code: 42262, Name: "CLOUD_INVALID_CONTENT", Message: "内容与声明的编码不符",
		Recovery: authprotocol.RecoveryNone, Hint: "json 编码时 content 是 JSON 值，text 时是字符串，base64 时是 base64 字符串"},
	{Code: 42263, Name: "CLOUD_INVALID_METADATA", Message: "元数据需为不超过 4 KiB 的 JSON 对象",
		Recovery: authprotocol.RecoveryNone},
	{Code: 50260, Name: "CLOUD_CONTENT_UNREADABLE", Message: "内容读取失败",
		Recovery: authprotocol.RecoveryNone, Hint: "存储桶里的对象暂时读不出来，稍后重试；持续失败请联系管理员检查存储配置"},
	{Code: 40441, Name: "CARD_KEY_NOT_FOUND", Message: "卡密不存在",
		Recovery: authprotocol.RecoveryNone, Hint: "服务端已忽略大小写与分隔符差异，走到这里就是真的没有这张卡"},
	{Code: 40910, Name: "CARD_KEY_REDEEMING", Message: "该卡密正在被核销",
		Recovery: authprotocol.RecoveryNone},
	{Code: 40470, Name: "APP_NOT_FOUND", Message: "应用不存在或已停用", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeQRLoginTicketInvalid, Name: "QR_LOGIN_TICKET_INVALID", Message: "登录二维码无效",
		Recovery: authprotocol.RecoveryNone, Hint: "票据不存在，或网页轮询时 pollToken 与票据不符"},
	{Code: errCodeQRLoginScannedByOther, Name: "QR_LOGIN_SCANNED_BY_OTHER", Message: "该二维码已被其他账号扫描",
		Recovery: authprotocol.RecoveryNone},
	{Code: errCodeQRLoginNotScanned, Name: "QR_LOGIN_NOT_SCANNED", Message: "请先扫描网页上的登录二维码",
		Recovery: authprotocol.RecoveryNone, Hint: "确认与拒绝之前必须先调 /auth/qr/scan"},
	{Code: errCodeQRLoginTicketUsed, Name: "QR_LOGIN_TICKET_USED", Message: "该二维码已失效，请在网页上刷新后重新扫描",
		Recovery: authprotocol.RecoveryNone, Hint: "票据已确认、已拒绝或已被网页领取"},
	{Code: errCodeQRLoginTicketExpired, Name: "QR_LOGIN_TICKET_EXPIRED", Message: "登录二维码已过期，请在网页上刷新后重新扫描",
		Recovery: authprotocol.RecoveryNone, Hint: "票据两分钟有效"},
	{Code: 40484, Name: "TRIAL_NOT_AVAILABLE", Message: "当前应用未开放试用",
		Recovery: authprotocol.RecoveryNone, Hint: "管理员没有配置启用中的试用套餐，入口应当整个隐藏"},
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
