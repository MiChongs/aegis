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
	{Key: "deviceLimitResolve", Method: "POST", Path: "/auth/device-limit/resolve",
		Summary: "登录设备数已满时，凭登录结果里 deviceLimit.ticket 与勾选要下线的设备完成登录"},
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
	{Key: "oauthExchange", Method: "POST", Path: "/auth/oauth/exchange",
		Summary: "原生 SDK 用第三方 accessToken 换会话；服务端核实身份，deferRegistration 时未绑定返回 oauthLink"},
	{Key: "oauthLinkRegister", Method: "POST", Path: "/auth/oauth/link/register", Summary: "凭 oauthLink 关联凭证快速注册"},
	{Key: "oauthLinkBind", Method: "POST", Path: "/auth/oauth/link/bind", Auth: true, Summary: "登录已有账号后凭 oauthLink 关联凭证绑定"},
	{Key: "oauthBindURL", Method: "POST", Path: "/auth/oauth/bind/url", Auth: true, Summary: "取第三方账号绑定授权地址"},
	{Key: "oauthBindToken", Method: "POST", Path: "/auth/oauth/bind/token", Auth: true, Summary: "用原生 SDK 拿到的第三方 accessToken 绑定"},
	{Key: "oauthBindings", Method: "GET", Path: "/auth/oauth/bindings", Auth: true, Summary: "我已绑定的第三方账号"},
	{Key: "oauthUnbind", Method: "DELETE", Path: "/auth/oauth/bindings/{provider}", Auth: true, Summary: "解绑第三方账号"},

	// ── 邮箱验证码与找回密码 ──
	{Key: "emailCode", Method: "POST", Path: "/auth/email/code", Summary: "发送邮箱验证码"},
	{Key: "emailVerify", Method: "POST", Path: "/auth/email/verify", Summary: "校验邮箱验证码"},
	{Key: "passwordForgot", Method: "POST", Path: "/auth/password/forgot", Summary: "发送密码重置邮件"},
	{Key: "passwordResetVerify", Method: "POST", Path: "/auth/password/reset/verify", Summary: "校验密码重置令牌"},
	{Key: "passwordRecoveryStart", Method: "POST", Path: "/auth/password/recovery/start",
		Summary: "找回密码：凭账号、邮箱或手机号发起，返回流程令牌与要完成的验证"},
	{Key: "passwordRecoveryStatus", Method: "POST", Path: "/auth/password/recovery/status", Summary: "找回密码：读取流程状态"},
	{Key: "passwordRecoverySend", Method: "POST", Path: "/auth/password/recovery/send",
		Summary: "找回密码：向绑定的邮箱或手机号发验证码（未登录时需填完整联系方式）"},
	{Key: "passwordRecoveryVerify", Method: "POST", Path: "/auth/password/recovery/verify",
		Summary: "找回密码：核验邮箱 / 短信验证码、身份验证器动态码或恢复码"},
	{Key: "passwordRecoveryComplete", Method: "POST", Path: "/auth/password/recovery/complete",
		Summary: "找回密码：设置新密码，其余登录全部下线"},
	{Key: "passwordRecoveryMine", Method: "POST", Path: "/me/password/recovery", Auth: true,
		Summary: "已登录时找回密码（忘记原密码）：发起流程，完成后保留当前登录"},
	{Key: "passwordVerify", Method: "POST", Path: "/auth/password/verify", Auth: true, Summary: "校验当前密码"},
	{Key: "passwordChange", Method: "POST", Path: "/auth/password/change", Auth: true, Summary: "修改密码"},

	// ── Passkey 登录 ──
	{Key: "passkeyOptions", Method: "POST", Path: "/auth/passkey/options", Summary: "取 Passkey 登录参数"},
	{Key: "passkeyLogin", Method: "POST", Path: "/auth/passkey/login", Summary: "Passkey 登录校验"},

	// ── 卡密 ──
	//
	// 授权卡登录不在这里：它是 /auth/login 的一档 method（cardkey），
	// 加登录方式不加路由，客户端也就不需要认识第二条登录入口。
	{Key: "cardKeyPreview", Method: "POST", Path: "/card-keys/preview", Auth: true,
		Summary: "兑换前预览：这张卡会发什么、现在能不能用（不能用时 usable=false 并给出 reasonCode）"},
	{Key: "cardKeyRedeem", Method: "POST", Path: "/card-keys/redeem", Auth: true,
		Summary: "兑换卡密（会员 / 功能解锁 / 积分 / 经验 / 余额 / 抽奖次数 / 授权时长 / 设备位）"},
	{Key: "cardKeyMine", Method: "GET", Path: "/card-keys/mine", Auth: true,
		Summary: "我名下的授权卡：授权到期时间、已绑定的设备与剩余的自助解绑次数"},
	{Key: "cardKeyUnbindDevice", Method: "DELETE", Path: "/card-keys/mine/{cardId}/devices/{deviceId}", Auth: true,
		Summary: "自助解绑授权卡上的一台设备（每 30 天次数由批次决定）"},
	{Key: "cardKeyRedemptions", Method: "GET", Path: "/card-keys/redemptions", Auth: true,
		Summary: "我的卡密兑换记录（分页）"},

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

	// ── 功能时长 ──
	//
	// 看激励广告领到的「功能时长」按实际使用扣：开始使用前租一段（≤ 600 秒，先从余额里扣走），
	// 用完再租下一段，停下时结算退回没用完的部分。会员或已拥有该功能的用户免扣（exempt=true）。
	{Key: "featureTime", Method: "GET", Path: "/features/time/{feature}", Auth: true,
		Summary: "功能时长：余额、累计获得与用掉的秒数、当前是否免扣"},
	{Key: "featureTimeLease", Method: "POST", Path: "/features/time/{feature}/leases", Auth: true,
		Summary: "开始使用前租一段时长（seconds 1–600）；免扣时不扣余额，余额为 0 返回 40328"},
	{Key: "featureTimeSettle", Method: "POST", Path: "/features/time/leases/{leaseNo}/settle", Auth: true,
		Summary: "停止使用时结算租约，没用完的退回余额；重复结算返回第一次的结论，过期租约不退款"},

	// ── 广告服务策略 ──
	//
	// 是否同意广告服务记在账号上：拒绝或尚未选择的用户只能使用 basicTools 里的基础服务。
	// 策略与开屏上报免登录可用（未登录时开屏按设备记录、选择由客户端记在本机），
	// 登录后用 ifAbsent 把本机的选择同步到账号上。
	{Key: "adPolicy", Method: "GET", Path: "/ads/policy",
		Summary: "广告服务策略：是否要求同意、基础服务清单、开屏广告配置与频控，以及当前账号的选择与服务模式"},
	{Key: "adConsent", Method: "POST", Path: "/ads/consent", Auth: true,
		Summary: "记录对广告服务的选择（同意 / 拒绝）；ifAbsent 时账号上已有选择则不覆盖"},
	{Key: "splashAdEvents", Method: "POST", Path: "/ads/splash/events",
		Summary: "上报开屏广告记录（shown / clicked / failed / timeout），带令牌时记到账号上"},

	// ── 崩溃报告 ──
	//
	// 只在用户明确同意发送这一份报告后调用（如崩溃屏上的「发送此报告」），免登录可用。
	{Key: "crashReport", Method: "POST", Path: "/crash-reports",
		Summary: "上报一份崩溃报告（kind: java | native | anr | exit，report 上限 256 KB）；reportId 重复时返回原来那一条，按设备与 IP 限频"},

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
	// 自动签到：应用的开放策略决定谁能开，个人设置决定几点签；available=false 时 reason / message 说明原因
	{Key: "autoSignStatus", Method: "GET", Path: "/me/auto-sign", Auth: true,
		Summary: "自动签到状态：开放策略、资格、个人设置、下一次执行时刻与最近一次结果"},
	{Key: "autoSignUpdate", Method: "PUT", Path: "/me/auto-sign", Auth: true,
		Summary: "修改自动签到设置（只改传了的字段）；从关到开时校验开放策略，返回修改后的状态"},
	{Key: "security", Method: "GET", Path: "/me/security", Auth: true, Summary: "账户安全概览"},
	// 账号初始化：第三方 / 短信 / 卡密快速登录建的号补设用户名与密码（docs/account-setup.md）。
	// prompt 为 dialog 时弹窗一次并上报 prompted，之后为 banner；用户关掉横幅上报 dismiss
	{Key: "accountSetupStatus", Method: "GET", Path: "/me/account-setup", Auth: true,
		Summary: "账号初始化状态：还缺用户名 / 密码、现在该弹窗还是出横幅、可选用户名与规则"},
	{Key: "accountSetupComplete", Method: "POST", Path: "/me/account-setup", Auth: true,
		Summary: "设置用户名（只能自助设一次）与密码（没有密码时），两项可分开提交，返回新的状态"},
	{Key: "accountSetupCheck", Method: "POST", Path: "/me/account-setup/check", Auth: true,
		Summary: "检查用户名是否可用（规则、占用、注销保留期）"},
	{Key: "accountSetupPrompted", Method: "POST", Path: "/me/account-setup/prompted", Auth: true,
		Summary: "首次弹窗已展示：之后只出横幅"},
	{Key: "accountSetupDismiss", Method: "POST", Path: "/me/account-setup/dismiss", Auth: true,
		Summary: "关掉横幅：按应用策略隔一段时间再出现（策略不许关时状态不变）"},
	// 账号注销：enabled=false（且没有进行中的申请）时入口应当整个隐藏；verification 说明提交时要带哪些凭据
	{Key: "deletion", Method: "GET", Path: "/me/deletion", Auth: true,
		Summary: "注销页：可选方式、须知、原因、要过的验证、前置条件与进行中的申请"},
	{Key: "deletionCode", Method: "POST", Path: "/me/deletion/code", Auth: true,
		Summary: "向绑定的邮箱或手机号发注销验证码（channel: email | phone）"},
	{Key: "deletionSubmit", Method: "POST", Path: "/me/deletion", Auth: true,
		Summary: "提交注销（mode: cooling | immediate）；deleted 为真时账号已删除，清掉本地登录"},
	{Key: "deletionCancel", Method: "POST", Path: "/me/deletion/cancel", Auth: true,
		Summary: "撤销进行中的注销申请（待审核、冷静期中、执行失败）"},

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
	{Key: "devices", Method: "GET", Path: "/me/devices", Auth: true,
		Summary: "我的登录设备：按设备合并的在线登录、生效的设备数上限与各终端类别的占用"},
	{Key: "devicesRemove", Method: "POST", Path: "/me/devices/remove", Auth: true,
		Summary: "下线我的其他设备（deviceKeys），不能下线当前设备"},
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
	// 达成奖励（docs/levels.md）：升级经过的每一级各一份，claimable 的由用户在这里领取
	{Key: "pointsLevelRewards", Method: "GET", Path: "/points/level/rewards", Auth: true, Summary: "我的等级达成奖励"},
	{Key: "pointsLevelRewardClaim", Method: "POST", Path: "/points/level/rewards/{rewardId}/claim", Auth: true, Summary: "领取等级达成奖励"},
	{Key: "pointsLevelHistory", Method: "GET", Path: "/points/level/history", Auth: true, Summary: "我的升级记录"},
	{Key: "integralTransactions", Method: "GET", Path: "/points/integral-transactions", Auth: true, Summary: "积分流水"},
	{Key: "experienceTransactions", Method: "GET", Path: "/points/experience-transactions", Auth: true, Summary: "经验流水"},
	// 积分商城：每件商品随附 availability（能不能兑、为什么不能），与兑换失败的错误码同一套判据。
	// 未开启时 enabled=false、items 为空，不报错。
	{Key: "pointsStore", Method: "GET", Path: "/points/store", Auth: true,
		Summary: "积分商城：余额、等级、商品与每件商品对当前用户的可兑换判定"},
	{Key: "pointsStoreRedeem", Method: "POST", Path: "/points/store/redeem", Auth: true,
		Summary: "用积分兑换一件商品（requestId 幂等）；含人工发放的商品兑换后为 pending"},
	{Key: "pointsStoreRedemptions", Method: "GET", Path: "/points/store/redemptions", Auth: true, Summary: "我的兑换记录"},
	// 抽奖：抽中什么只由服务端决定并在同一个事务里到账，客户端只说用哪一种次数抽一次。
	// 次数三种来源：每日免费（free）、抽奖次数余额（bonus，看激励广告 / 卡密 / 积分商城等发放）、积分（points）。
	// 扣除顺序只有一条：今日免费 → 赠送的次数 → 兑换的次数 → 积分；source 只是「最多用到这一档」，免费永远先扣。
	// 视频抽奖 = 活动页 chances.video 给出的激励广告场景：按激励广告流程看完并上报，到账的是抽奖次数，再用 bonus 抽。
	{Key: "lotteryActivities", Method: "GET", Path: "/lottery", Auth: true,
		Summary: "进行中的抽奖活动（没有时为空列表，入口应当隐藏）"},
	{Key: "lotteryState", Method: "GET", Path: "/lottery/activities/{activityId}", Auth: true,
		Summary: "抽奖活动页：奖品与公示概率、我的各种次数（含视频抽奖的广告场景）、保底进度"},
	{Key: "lotteryDraw", Method: "POST", Path: "/lottery/activities/{activityId}/draw", Auth: true,
		Summary: "抽一次（source: free / bonus / points 为最多用到哪一档，留空不设上限，都先扣免费；requestId 幂等）；返回结果与抽完后的次数"},
	{Key: "lotteryDrawBatch", Method: "POST", Path: "/lottery/activities/{activityId}/draw/batch", Auth: true,
		Summary: "连抽（十连抽，count 1–10）：逐次抽、逐次到账；中途次数用完时停下并返回已抽到的（stopped 写明原因）"},
	{Key: "lotteryDraws", Method: "GET", Path: "/lottery/draws", Auth: true, Summary: "我的抽奖记录（activityId 可选）"},
	{Key: "lotteryTickets", Method: "GET", Path: "/lottery/tickets", Auth: true,
		Summary: "我的抽奖次数：余额、赠送与兑换两档、还有剩余的批次（按使用顺序）与次数流水（page / limit）"},
	// 邀请：未开启时 /invite 回 enabled=false（不报错），入口整个隐藏。注册表单上的邀请码字段由
	// /config 的 auth.invite 决定，填了的码随 /auth/register 的 inviteCode 一起提交。
	{Key: "invite", Method: "GET", Path: "/invite", Auth: true,
		Summary: "邀请页：我的邀请码、分享文案、奖励规则、统计，以及我还能不能填写邀请码（bind）"},
	{Key: "inviteInvitees", Method: "GET", Path: "/invite/invitees", Auth: true, Summary: "我邀请的人"},
	{Key: "inviteBind", Method: "POST", Path: "/invite/bind", Auth: true,
		Summary: "注册后在时限内补填邀请码（默认 72 小时，按应用配置）"},
	{Key: "inviteCode", Method: "PUT", Path: "/invite/code", Auth: true,
		Summary: "换我的邀请码：code 非空为自定义（应用允许时），为空则重新生成"},
	{Key: "invitePreview", Method: "POST", Path: "/invite/preview",
		Summary: "注册页预览邀请码：能不能用、邀请人、填了能拿什么（不需要登录）"},
	{Key: "leaderboardSummary", Method: "GET", Path: "/leaderboard/summary", Auth: true, Summary: "排行榜综合概览"},
	{Key: "leaderboardMe", Method: "GET", Path: "/leaderboard/me", Auth: true, Summary: "我在各榜单的排名"},
	{Key: "leaderboardPoints", Method: "GET", Path: "/leaderboard/points/{type}", Auth: true, Summary: "积分榜（type: integral | experience | level）"},
	{Key: "leaderboardSignIn", Method: "GET", Path: "/leaderboard/signin/{type}", Auth: true, Summary: "签到榜（type: today | consecutive | monthly）"},
	// 积分排行榜（新版，docs/leaderboard.md）：未开启时 /leaderboard/boards 回 enabled=false（不报错），入口整个隐藏。
	// 接口永远不返回原始账号；匿名上榜的人对别人显示「匿名用户」。
	{Key: "leaderboardBoards", Method: "GET", Path: "/leaderboard/boards", Auth: true,
		Summary: "排行榜首页：开启的榜单（当前周期、奖励档位）与我的偏好（匿名、是否被移出）"},
	{Key: "leaderboardBoard", Method: "GET", Path: "/leaderboard/boards/{key}", Auth: true,
		Summary: "一个榜单的实时名次（并列同名次，只在前 topLimit 名内分页）与我的名次（含距上一名的差距）"},
	{Key: "leaderboardPeriods", Method: "GET", Path: "/leaderboard/boards/{key}/periods", Auth: true,
		Summary: "周期榜的往期列表（已结算的快照），新的在前"},
	{Key: "leaderboardPeriod", Method: "GET", Path: "/leaderboard/boards/{key}/periods/{periodKey}", Auth: true,
		Summary: "往期榜单：结算时冻结的名次、我在这一期的名次与奖励"},
	{Key: "leaderboardRewards", Method: "GET", Path: "/leaderboard/rewards", Auth: true, Summary: "我的榜单奖励记录"},
	{Key: "leaderboardPreference", Method: "GET", Path: "/leaderboard/preference", Auth: true, Summary: "我的排行榜偏好（匿名上榜）"},
	{Key: "leaderboardPreferenceUpdate", Method: "PUT", Path: "/leaderboard/preference", Auth: true,
		Summary: "开关匿名上榜：名次与奖励照旧，别人看到的是「匿名用户」"},

	// ── 站内信（消息中心，docs/notification-center.md）──
	// 写操作都带回 unread 与 categoryUnread；新消息、已读、删除另经 /api/ws 实时推送
	// （notification.created / notification.read / notification.deleted）。
	{Key: "notifications", Method: "GET", Path: "/notifications", Auth: true,
		Summary: "站内信列表（status=all|unread|read、category 筛选，已过期的不出现）"},
	{Key: "notificationSummary", Method: "GET", Path: "/notifications/summary", Auth: true,
		Summary: "消息中心首页：总未读与每个分类的未读、总数、最新一条"},
	{Key: "notificationsUnread", Method: "GET", Path: "/notifications/unread-count", Auth: true, Summary: "未读数（含按分类的 categories）"},
	{Key: "notificationDetail", Method: "GET", Path: "/notifications/{notificationId}", Auth: true, Summary: "单条消息详情"},
	{Key: "notificationRead", Method: "POST", Path: "/notifications/read", Auth: true, Summary: "标记已读"},
	{Key: "notificationUnread", Method: "POST", Path: "/notifications/unread", Auth: true, Summary: "标为未读"},
	{Key: "notificationReadBatch", Method: "POST", Path: "/notifications/read-batch", Auth: true, Summary: "批量标记已读（≤ 200 条）"},
	{Key: "notificationReadAll", Method: "POST", Path: "/notifications/read-all", Auth: true, Summary: "全部标记已读（可按分类）"},
	{Key: "notificationDeleteBatch", Method: "POST", Path: "/notifications/delete-batch", Auth: true, Summary: "批量删除（≤ 200 条）"},
	{Key: "notificationClear", Method: "POST", Path: "/notifications/clear", Auth: true, Summary: "清空站内信（可按状态与分类）"},
	{Key: "notificationDelete", Method: "DELETE", Path: "/notifications/{notificationId}", Auth: true, Summary: "删除一条站内信"},
	{Key: "notificationPreferences", Method: "GET", Path: "/notifications/preferences", Auth: true, Summary: "各分类的提醒开关"},
	{Key: "notificationPreferencesUpdate", Method: "PUT", Path: "/notifications/preferences", Auth: true,
		Summary: "改提醒开关：关闭只影响提醒（实时事件 silent=true），消息照常入库、计未读；账号安全类不能关"},

	// ── AI 中转网关 ──
	//
	// 调用面在 /api/ai/v1（OpenAI / Anthropic SDK 直连，baseURL 见 aiOverview.baseUrl），这里是它的账户侧：
	// 能用多少、扣费顺序、可用模型与单价、API 密钥、调用记录。规格见 docs/ai-gateway.md。
	{Key: "aiOverview", Method: "GET", Path: "/ai/overview", Auth: true,
		Summary: "AI 中转总览：是否开放、扣费顺序、各额度余量、积分与余额、价格倍率、今日已用、调用地址"},
	{Key: "aiModels", Method: "GET", Path: "/ai/models", Auth: true,
		Summary: "可调用的模型与有效单价（已乘上本人的会员与等级倍率），会员专享模型标 available=false"},
	{Key: "aiCatalogLookup", Method: "GET", Path: "/ai/catalog/lookup", Auth: false,
		Summary: "公开模型目录按名字批量查能力：识图、工具、思考与可选强度、上下文与输出上限（models、providers 逗号分隔）"},
	{Key: "aiKeys", Method: "GET", Path: "/ai/keys", Auth: true, Summary: "我的 API 密钥（只含提示，不含明文）"},
	{Key: "aiKeyCreate", Method: "POST", Path: "/ai/keys", Auth: true,
		Summary: "新建 API 密钥（名称、可用模型、消费上限、到期时间）；明文 secret 只在这次返回"},
	{Key: "aiKeyUpdate", Method: "PUT", Path: "/ai/keys/{keyId}", Auth: true,
		Summary: "修改 API 密钥：名称、启用 / 停用、可用模型、消费上限、到期时间（clearExpiry 清除）"},
	{Key: "aiKeyDelete", Method: "DELETE", Path: "/ai/keys/{keyId}", Auth: true,
		Summary: "删除 API 密钥，立即失效；进行中的调用照常结算"},
	{Key: "aiRequests", Method: "GET", Path: "/ai/requests", Auth: true,
		Summary: "我的调用记录：用量、价格、从哪些来源扣了多少；失败的调用不扣费也会列出"},
	{Key: "aiRequest", Method: "GET", Path: "/ai/requests/{requestNo}", Auth: true,
		Summary: "一次调用的明细（requestNo 即调用响应头 X-Aegis-Request-No）"},
	{Key: "aiCredits", Method: "GET", Path: "/ai/credits", Auth: true,
		Summary: "赠送额度流水：看广告与管理员发放、调用消费、退款"},

	// ── 钱包 / 会员 / 支付 ──
	{Key: "wallet", Method: "GET", Path: "/wallet", Auth: true, Summary: "我的余额"},
	{Key: "walletTransactions", Method: "GET", Path: "/wallet/transactions", Auth: true, Summary: "余额流水"},
	{Key: "walletConsume", Method: "POST", Path: "/wallet/consume", Auth: true, Summary: "余额消费"},
	{Key: "vipPlans", Method: "GET", Path: "/vip/plans", Auth: true, Summary: "会员套餐"},
	{Key: "vipStatus", Method: "GET", Path: "/vip/status", Auth: true, Summary: "我的会员状态"},
	{Key: "vipFeatures", Method: "GET", Path: "/vip/features", Auth: true, Summary: "会员权益目录（启用中的标识、名称与说明）"},
	{Key: "vipFeatureTicket", Method: "POST", Path: "/vip/feature-ticket", Auth: true, Summary: "权益凭证：拥有某项权益时签发绑定账号与设备的 Ed25519 凭证，客户端离线验签"},
	{Key: "listenTogetherTicket", Method: "POST", Path: "/listen-together/ticket", Auth: true,
		Summary: "一起听票据：拥有 listen_together 权益时签发短期 EdDSA 票据并给出一起听服务的 wss 地址（docs/listen-together.md）"},
	{Key: "vipTransactions", Method: "GET", Path: "/vip/transactions", Auth: true, Summary: "会员流水"},
	{Key: "vipConversions", Method: "GET", Path: "/vip/conversions", Auth: true, Summary: "会员折算记录（已拥有的发放、开通永久时折掉的存量）"},
	{Key: "vipPurchase", Method: "POST", Path: "/vip/purchase", Auth: true, Summary: "购买会员"},
	{Key: "vipTrial", Method: "POST", Path: "/vip/trial", Auth: true, Summary: "领取试用会员（一人一次，资格见 /vip/status 的 trialOffer）"},
	{Key: "payMethods", Method: "GET", Path: "/pay/methods", Auth: true,
		Summary: "可用的付款方式（渠道配置按子类型展开；purpose=wallet_recharge 时不含余额支付）"},
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

	// ── 意见反馈 ──
	// 反馈是 kind=feedback 的工单：图片（≤4，按魔数判定）与附件（≤2）分开传，
	// 来源与客户端信息由服务端记录，同一用户每小时 5 条、每天 20 条。
	{Key: "feedbackCategories", Method: "GET", Path: "/feedback/categories", Auth: true, Summary: "反馈分类"},
	{Key: "feedbackAttachment", Method: "POST", Path: "/feedback/attachments", Auth: true, Upload: true,
		Summary: "上传反馈图片或附件（multipart/form-data：file，kind=image|file）"},
	{Key: "feedbackCreate", Method: "POST", Path: "/feedback", Auth: true, Summary: "提交反馈"},
	{Key: "feedbackList", Method: "GET", Path: "/feedback", Auth: true, Summary: "我的反馈"},
	{Key: "feedbackDetail", Method: "GET", Path: "/feedback/{feedbackId}", Auth: true, Summary: "反馈详情"},
	{Key: "feedbackReply", Method: "POST", Path: "/feedback/{feedbackId}/replies", Auth: true, Summary: "补充反馈"},
	{Key: "feedbackRating", Method: "POST", Path: "/feedback/{feedbackId}/rating", Auth: true, Summary: "评价反馈处理结果"},
	{Key: "feedbackCancel", Method: "POST", Path: "/feedback/{feedbackId}/cancel", Auth: true, Summary: "撤回反馈"},

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

	// ── 版本管控（免登录；上报与裁决走独立加密通道，见 docs/client-control.md）──
	{Key: "clientControlCheck", Method: "POST", Path: "/client-control/check", Summary: "加密上报包名、版本与签名，取回签名裁决：放行 / 提醒 / 强制更新 / 停用"},
	{Key: "clientControlWatch", Method: "GET", Path: "/client-control/watch", Summary: "长轮询：策略版本变化时立即返回，客户端随即重新校验"},
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
	// ── 登录设备数（docs/device-limit.md）──
	{Code: 40130, Name: "DEVICE_LIMIT_SIGNED_OUT", Message: "账号已在其他设备登录，当前设备已退出登录",
		Recovery: authprotocol.RecoveryReauth,
		Hint:     "登录设备数超出上限，本机被自动下线；访问令牌与刷新令牌都已失效，不要再刷新，清掉本地登录态并提示用户"},
	{Code: 40131, Name: "DEVICE_REMOVED", Message: "当前设备已在设备管理中被移除",
		Recovery: authprotocol.RecoveryReauth, Hint: "用户在别的设备上或管理员把本机下线了，清掉本地登录态回到登录页"},
	{Code: 40997, Name: "DEVICE_LIMIT_REACHED", Message: "同时登录的设备已达上限",
		Recovery: authprotocol.RecoveryNone,
		Hint:     "应用设置为拒绝超出的登录；提示用户先在已登录的设备上退出，或到设备管理里下线其他设备"},
	{Code: 40998, Name: "NEW_DEVICE_LIMITED", Message: "近期在新设备上登录的次数已达上限",
		Recovery: authprotocol.RecoveryNone, Hint: "引导用户在常用设备上登录，或等统计窗口过去"},
	{Code: 40496, Name: "DEVICE_LIMIT_TICKET_INVALID", Message: "设备选择已失效",
		Recovery: authprotocol.RecoveryReauth, Hint: "选择页凭证 5 分钟有效、用过即作废，回到登录页重新登录"},
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
	{Code: errCodeOAuthTokenInvalid, Name: "OAUTH_TOKEN_INVALID", Message: "第三方授权无效、已过期或不属于本应用",
		Recovery: authprotocol.RecoveryNone, Hint: "让用户在端上重新走一遍第三方 SDK 授权，拿新的 accessToken"},
	{Code: errCodeOAuthLinkTicketInvalid, Name: "OAUTH_LINK_TICKET_INVALID", Message: "关联凭证不存在、已用过或已过期",
		Recovery: authprotocol.RecoveryNone, Hint: "凭证 10 分钟有效且只能用一次；重新调 /auth/oauth/exchange 换一张"},
	{Code: errCodeOAuthIdentityTaken, Name: "OAUTH_IDENTITY_TAKEN", Message: "该第三方账号已绑定其他账号",
		Recovery: authprotocol.RecoveryNone, Hint: "先在原账号里解绑，或直接用这个第三方账号登录原账号"},
	{Code: errCodeOAuthProviderBound, Name: "OAUTH_PROVIDER_BOUND", Message: "当前账号已绑定该渠道的其他账号",
		Recovery: authprotocol.RecoveryNone, Hint: "同一渠道一个账号只能绑一个第三方账号，先解绑再绑新的"},
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
	// ── 永久会员 ──
	{Code: 40116, Name: "PAY_TYPE_UNAVAILABLE", Message: "该付款方式未开放",
		Recovery: authprotocol.RecoveryNone,
		Hint:     "下单的 type 必须是 /pay/methods 里同一条配置列出的类型；后台关掉某个类型后，旧的选择要重新拉一次列表"},
	{Code: 40378, Name: "VIP_LIFETIME_INCLUDED", Message: "永久会员已包含该套餐的全部权益，无需购买",
		Recovery: authprotocol.RecoveryNone,
		Hint:     "/vip/plans 里 included=true 的套餐不要给购买按钮；/vip/purchase 与会员直购下单都会拒绝它"},
	// ── 权益凭证 ──
	{Code: errCodeFeatureNotGranted, Name: "FEATURE_NOT_GRANTED", Message: "当前账号未拥有该会员权益",
		Recovery: authprotocol.RecoveryNone, Hint: "/vip/feature-ticket 拒绝签发；客户端应删掉本地保存的旧凭证并关闭该功能"},
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
	{Code: 40346, Name: "CARD_KEY_NO_LOGIN_CARD", Message: "名下没有可用的授权卡",
		Recovery: authprotocol.RecoveryNone, Hint: "「设备位」「授权时长」两档权益要加在授权卡上；先用授权卡登录或绑定一张"},
	{Code: errCodeCardKeyNotStarted, Name: "CARD_KEY_NOT_STARTED", Message: "该卡密还没到生效时间",
		Recovery: authprotocol.RecoveryNone, Hint: "批次设置了生效时间，message 里带具体时刻"},
	{Code: errCodeCardKeyAlreadyRedeemed, Name: "CARD_KEY_ALREADY_REDEEMED", Message: "你已经兑换过这个卡密",
		Recovery: authprotocol.RecoveryNone, Hint: "活动码每人只能用一次"},
	{Code: errCodeCardKeyUserLimit, Name: "CARD_KEY_USER_LIMIT", Message: "这一批卡的兑换张数已达上限",
		Recovery: authprotocol.RecoveryNone, Hint: "批次设置了每人限兑张数"},
	{Code: errCodeCardKeyAudience, Name: "CARD_KEY_AUDIENCE", Message: "不在这张卡的领取对象里",
		Recovery: authprotocol.RecoveryNone, Hint: "批次限定了仅会员或仅非会员，message 里说明是哪一种"},
	{Code: errCodeCardKeyUnbindLimit, Name: "CARD_KEY_UNBIND_LIMIT", Message: "自助解绑次数已用完",
		Recovery: authprotocol.RecoveryNone, Hint: "按 30 天滚动窗口计；/card-keys/mine 的 selfUnbindRemaining 是剩余次数"},
	{Code: errCodeCardKeyUnbindDisabled, Name: "CARD_KEY_UNBIND_DISABLED", Message: "这张卡不支持自助解绑",
		Recovery: authprotocol.RecoveryNone, Hint: "selfUnbindLimit 为 0；引导联系客服在控制台解绑"},
	{Code: errCodeCardKeyLicensePermanent, Name: "CARD_KEY_LICENSE_PERMANENT", Message: "授权卡已是永久授权",
		Recovery: authprotocol.RecoveryNone, Hint: "续费卡（授权时长）不必兑换，可以留给别人"},
	{Code: errCodeCardKeyRewardOwned, Name: "CARD_KEY_REWARD_OWNED", Message: "已永久拥有这张卡要解锁的功能",
		Recovery: authprotocol.RecoveryNone},
	{Code: errCodeCardKeyRewardUnavailable, Name: "CARD_KEY_REWARD_UNAVAILABLE", Message: "这张卡的权益已下架",
		Recovery: authprotocol.RecoveryNone, Hint: "引用的会员套餐被删除或功能被停用，引导联系客服"},
	{Code: errCodeCardKeyDeviceMissing, Name: "CARD_KEY_DEVICE_NOT_BOUND", Message: "这台设备没有绑定在该卡上",
		Recovery: authprotocol.RecoveryNone, Hint: "重新拉一次 /card-keys/mine"},
	{Code: errCodeCardKeyTooManyAttempts, Name: "CARD_KEY_TOO_MANY_ATTEMPTS", Message: "输错卡密的次数过多",
		Recovery: authprotocol.RecoveryNone, Hint: "只数「卡密不存在」；按账号与 IP 计，锁定 15 分钟，message 里带剩余分钟数"},
	// ── 账号初始化 ──
	{Code: errCodeUsernameTaken, Name: "USERNAME_TAKEN", Message: "该用户名已被使用",
		Recovery: authprotocol.RecoveryNone, Hint: "也用于注销后仍在保留期内的用户名；换一个或选用状态里的 suggestions"},
	{Code: errCodeAccountSetupDone, Name: "ACCOUNT_SETUP_DONE", Message: "用户名已设置",
		Recovery: authprotocol.RecoveryNone, Hint: "自助只能设一次，之后由管理员在控制台修改"},
	{Code: errCodePasswordAlreadySet, Name: "PASSWORD_ALREADY_SET", Message: "已设置过密码",
		Recovery: authprotocol.RecoveryNone, Hint: "改走 /auth/password/change"},
	// ── 自动签到 ──
	// 与 /me/auto-sign 的 reason 一一对应：状态里说「为什么不能开」，开启时报同一句话。
	{Code: errCodeAutoSignDisabled, Name: "AUTO_SIGN_DISABLED", Message: "自动签到暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "应用未开放自动签到（reason: disabled），入口应当隐藏或置灰"},
	{Code: errCodeAutoSignNotEligible, Name: "AUTO_SIGN_NOT_ELIGIBLE", Message: "不满足自动签到的开放条件",
		Recovery: authprotocol.RecoveryNone, Hint: "开放对象为会员 / 永久会员 / 指定功能而当前用户不满足（reason: vip_required / trial_excluded / lifetime_required / feature_required），message 说明具体是哪一条"},
	// ── 积分商城 ──
	// 与 /points/store 里每件商品的 availability.reason 一一对应：列表上灰着的原因与点了之后的报错是同一句话。
	{Code: errCodePointsStoreDisabled, Name: "POINTS_STORE_DISABLED", Message: "积分商城暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "/points/store 的 enabled 为 false 时入口应当整个隐藏"},
	{Code: errCodePointsStoreUnavailable, Name: "POINTS_STORE_ITEM_UNAVAILABLE", Message: "商品暂不可兑换",
		Recovery: authprotocol.RecoveryNone, Hint: "已下架、未开始、已结束或配置的权益已失效（reason: inactive / not_started / ended / reward_unavailable），重新拉一次 /points/store"},
	{Code: errCodePointsStoreSoldOut, Name: "POINTS_STORE_SOLD_OUT", Message: "已兑完", Recovery: authprotocol.RecoveryNone},
	{Code: errCodePointsStoreLimitReached, Name: "POINTS_STORE_LIMIT_REACHED", Message: "已达到兑换次数上限",
		Recovery: authprotocol.RecoveryNone, Hint: "按商品的 limitPeriod 计：total 为累计，day / week / month 按应用时区的自然日、周、月"},
	{Code: errCodePointsInsufficient, Name: "POINTS_INSUFFICIENT", Message: "积分不足", Recovery: authprotocol.RecoveryNone},
	{Code: errCodePointsStoreRequirement, Name: "POINTS_STORE_REQUIREMENT_UNMET", Message: "不满足兑换条件",
		Recovery: authprotocol.RecoveryNone, Hint: "等级门槛（minLevel）或兑换对象（audience: vip / non_vip / lifetime）不符，message 说明具体是哪一条"},
	{Code: errCodePointsStoreOwned, Name: "POINTS_STORE_OWNED", Message: "已拥有该权益，无需兑换",
		Recovery: authprotocol.RecoveryNone, Hint: "永久会员已包含所兑的会员，或所兑功能已永久解锁"},
	{Code: errCodePointsStoreItemNotFound, Name: "POINTS_STORE_ITEM_NOT_FOUND", Message: "商品不存在或已删除",
		Recovery: authprotocol.RecoveryNone},
	{Code: errCodePointsStoreRedeeming, Name: "POINTS_STORE_REDEEMING", Message: "兑换正在处理",
		Recovery: authprotocol.RecoveryNone, Hint: "同一个 requestId 的并发提交；稍后用同一个 requestId 重试会原样拿到结果"},
	// ── 抽奖 ──
	// 与活动页 chances 的判定一一对应：显示「还剩 0 次」与点了之后的报错是同一件事。
	{Code: errCodeLotteryNotFound, Name: "LOTTERY_NOT_FOUND", Message: "活动不存在或已下线",
		Recovery: authprotocol.RecoveryNone, Hint: "活动被删除、暂停或还是草稿，重新拉一次 /lottery"},
	{Code: errCodeLotteryNotStarted, Name: "LOTTERY_NOT_STARTED", Message: "活动尚未开始", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeLotteryEnded, Name: "LOTTERY_ENDED", Message: "活动已结束", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeLotteryFreeExhausted, Name: "LOTTERY_FREE_EXHAUSTED", Message: "今日免费次数已用完",
		Recovery: authprotocol.RecoveryNone, Hint: "可改用 bonus / points，或引导看视频（chances.video）"},
	{Code: errCodeLotteryPointsLimit, Name: "LOTTERY_POINTS_DAILY_LIMIT", Message: "今日积分抽奖次数已用完",
		Recovery: authprotocol.RecoveryNone},
	{Code: errCodeLotteryNoChance, Name: "LOTTERY_NO_CHANCE", Message: "没有可用的抽奖次数",
		Recovery: authprotocol.RecoveryNone, Hint: "指定 bonus 但余额为 0，或不指定来源且三种次数都没有（chances.next 为空）"},
	{Code: errCodeLotterySourceUnavailable, Name: "LOTTERY_SOURCE_UNAVAILABLE", Message: "该抽奖方式未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "活动未开放积分抽奖，或 source 不是 free / bonus / points"},
	{Code: errCodeLotteryPrizeUnavailable, Name: "LOTTERY_PRIZE_UNAVAILABLE", Message: "奖品配置已失效",
		Recovery: authprotocol.RecoveryNone, Hint: "抽中的奖品引用的套餐或功能已失效，整次抽奖已回滚，次数与积分未扣"},
	{Code: errCodeLotteryDrawing, Name: "LOTTERY_DRAWING", Message: "抽奖正在处理",
		Recovery: authprotocol.RecoveryNone, Hint: "同一个 requestId 的并发提交；稍后用同一个 requestId 重试会原样拿到结果"},
	{Code: errCodeLotteryRiskBlocked, Name: "LOTTERY_RISK_BLOCKED", Message: "当前环境存在风险，暂时无法抽奖",
		Recovery: authprotocol.RecoveryNone},
	// ── 账号注销 ──
	{Code: errCodeDeletionDisabled, Name: "ACCOUNT_DELETION_DISABLED", Message: "账号注销暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "/me/deletion 的 enabled 为 false 时入口应当整个隐藏"},
	{Code: errCodeDeletionVerifyFailed, Name: "ACCOUNT_DELETION_VERIFY_FAILED", Message: "身份验证未通过",
		Recovery: authprotocol.RecoveryNone, Hint: "密码、验证码或动态码不对；连续失败 8 次锁定 1 小时（42912）"},
	{Code: errCodeDeletionCodeMissing, Name: "ACCOUNT_DELETION_CODE_MISSING", Message: "验证码已失效，请重新获取",
		Recovery: authprotocol.RecoveryNone, Hint: "先调 /me/deletion/code，验证码 10 分钟内有效"},
	{Code: errCodeDeletionVerifyRequired, Name: "ACCOUNT_DELETION_VERIFY_REQUIRED", Message: "请完成身份验证",
		Recovery: authprotocol.RecoveryNone, Hint: "按 /me/deletion 的 verification 带上 password 或 channel+code；totp 为真时再带 totpCode 或 recoveryCode；available 为假时 message 说明要先绑定什么"},
	{Code: errCodeDeletionNotFound, Name: "ACCOUNT_DELETION_NOT_FOUND", Message: "没有进行中的注销申请", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeDeletionOpenExists, Name: "ACCOUNT_DELETION_OPEN_EXISTS", Message: "已有进行中的注销申请",
		Recovery: authprotocol.RecoveryNone, Hint: "重新拉一次 /me/deletion，按 request 展示冷静期或审核状态"},
	{Code: errCodeDeletionStateChanged, Name: "ACCOUNT_DELETION_STATE_CHANGED", Message: "申请状态已变化",
		Recovery: authprotocol.RecoveryNone, Hint: "例如正在执行时撤销；重新拉一次 /me/deletion"},
	{Code: errCodeDeletionBlocked, Name: "ACCOUNT_DELETION_BLOCKED", Message: "暂不满足注销条件",
		Recovery: authprotocol.RecoveryNone, Hint: "/me/deletion 的 checks 里 level=block 且 passed=false 的一项，message 说明具体原因"},
	{Code: errCodeDeletionReregister, Name: "ACCOUNT_DELETION_REREGISTER_LIMITED", Message: "该账号注销未满限制期，暂不能重新注册",
		Recovery: authprotocol.RecoveryNone, Hint: "注册接口返回；message 带可以重新注册的日期"},
	{Code: errCodeDeletionResendTooSoon, Name: "ACCOUNT_DELETION_CODE_TOO_FREQUENT", Message: "验证码发送过于频繁",
		Recovery: authprotocol.RecoveryNone, Hint: "同一渠道 60 秒一次，每天 10 次"},
	{Code: errCodeDeletionTooManyFailure, Name: "ACCOUNT_DELETION_LOCKED", Message: "验证失败次数过多", Recovery: authprotocol.RecoveryNone},
	// ── 邀请 ──
	// 与 /invite 的 bind.reason、/invite/preview 的 reason 同一套判据。
	{Code: errCodeInviteDisabled, Name: "INVITE_DISABLED", Message: "邀请活动暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "/invite 的 enabled 为 false 时入口应当整个隐藏"},
	{Code: errCodeInviteWindowClosed, Name: "INVITE_WINDOW_CLOSED", Message: "已超过填写期限",
		Recovery: authprotocol.RecoveryNone, Hint: "注册后只能在 auth.invite.bindWindowHours 小时内补填；为 0 时只能在注册时填写"},
	{Code: errCodeInviteAlreadyBound, Name: "INVITE_ALREADY_BOUND", Message: "已填写过邀请码", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeInviteCodeInvalid, Name: "INVITE_CODE_INVALID", Message: "邀请码不可用",
		Recovery: authprotocol.RecoveryNone, Hint: "不存在、已停用、未生效、已过期、次数已满或邀请人暂不满足邀请条件，message 说明是哪一种"},
	{Code: errCodeInviteSelf, Name: "INVITE_SELF", Message: "不能填写自己或由你邀请的用户的邀请码", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeInviteLimited, Name: "INVITE_LIMITED", Message: "当前设备或网络的填写次数已满", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeInviteRequired, Name: "INVITE_REQUIRED", Message: "请填写邀请码",
		Recovery: authprotocol.RecoveryNone, Hint: "auth.invite.field 为 required 时注册必须带 inviteCode"},
	{Code: errCodeInviteRewardUnavailable, Name: "INVITE_REWARD_UNAVAILABLE", Message: "邀请奖励配置已失效", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeInviteCustomForbidden, Name: "INVITE_CUSTOM_FORBIDDEN", Message: "当前应用不支持自定义邀请码", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeInviteNotEligible, Name: "INVITE_NOT_ELIGIBLE", Message: "暂不满足邀请条件",
		Recovery: authprotocol.RecoveryNone, Hint: "/invite 的 canInvite 为 false，inviteMessage 说明还差什么"},
	{Code: errCodeInviteCodeTaken, Name: "INVITE_CODE_TAKEN", Message: "邀请码已被占用", Recovery: authprotocol.RecoveryNone},
	// ── 消息中心 ──
	{Code: errCodeNotificationNotFound, Name: "NOTIFICATION_NOT_FOUND", Message: "消息不存在",
		Recovery: authprotocol.RecoveryNone, Hint: "不存在、不属于当前用户或已过期；客户端从本地列表移除这一条"},
	{Code: errCodeNotificationPushLocked, Name: "NOTIFICATION_PUSH_LOCKED", Message: "该类消息不能关闭提醒",
		Recovery: authprotocol.RecoveryNone, Hint: "/notifications/preferences 里 mutable=false 的分类不要展示关闭开关"},
	{Code: errCodeNotificationActionInvalid, Name: "NOTIFICATION_ACTION_INVALID", Message: "动作不合法",
		Recovery: authprotocol.RecoveryNone, Hint: "action.type 只能是 route（target 取路由键）或 url（https 地址）"},
	{Code: errCodeNotificationCategoryInvalid, Name: "NOTIFICATION_CATEGORY_INVALID", Message: "分类不存在",
		Recovery: authprotocol.RecoveryNone, Hint: "分类以 /notifications/summary 返回的 key 为准"},
	// ── 积分排行榜 ──
	{Code: errCodeLeaderboardDisabled, Name: "LEADERBOARD_DISABLED", Message: "排行榜暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "/leaderboard/boards 的 enabled 为 false 时入口应当整个隐藏"},
	{Code: errCodeLeaderboardNotFound, Name: "LEADERBOARD_NOT_FOUND", Message: "排行榜不存在",
		Recovery: authprotocol.RecoveryNone, Hint: "榜单 key 不存在或该榜未开启；以 /leaderboard/boards 返回的榜单为准"},
	{Code: errCodeLeaderboardSnapshotNotFound, Name: "LEADERBOARD_PERIOD_NOT_FOUND", Message: "该期榜单不存在或尚未结算",
		Recovery: authprotocol.RecoveryNone, Hint: "往期以 /leaderboard/boards/{key}/periods 返回的期号为准"},
	{Code: errCodeLeaderboardAnonymousForbidden, Name: "LEADERBOARD_ANONYMOUS_FORBIDDEN", Message: "当前应用不允许匿名上榜",
		Recovery: authprotocol.RecoveryNone, Hint: "/leaderboard/boards 的 me.allowAnonymous 为 false 时不要展示匿名开关"},
	// ── 激励广告 ──
	// 只有「这次上报本身不成立」才是错误；限额、冷却等「看了但不发」的结论
	// 在 /ads/rewarded/claim 的返回体里（status=rejected + reason），不走错误码。
	{Code: 40350, Name: "REWARDED_AD_DISABLED", Message: "激励广告暂未开放",
		Recovery: authprotocol.RecoveryNone, Hint: "管理员没有启用激励广告，入口应当整个隐藏"},
	{Code: 40351, Name: "REWARDED_AD_SCENE_UNAVAILABLE", Message: "奖励场景不存在或已停用",
		Recovery: authprotocol.RecoveryNone, Hint: "重新拉一次 /ads/rewarded，按最新的场景列表展示"},
	{Code: 40352, Name: "REWARDED_AD_USER_MISMATCH", Message: "这次观看不属于当前账号",
		Recovery: authprotocol.RecoveryNone, Hint: "SDK 的 userId 必须用 /ads/rewarded 下发的那一个；切换账号后要重建广告对象"},
	// ── 功能时长 ──
	{Code: errCodeFeatureTimeExhausted, Name: "FEATURE_TIME_EXHAUSTED", Message: "使用时长已用完",
		Recovery: authprotocol.RecoveryNone, Hint: "引导用户看激励广告领取功能时长，或开通会员（会员使用时不扣时长）"},
	{Code: errCodeFeatureTimeLeaseNotFound, Name: "FEATURE_TIME_LEASE_NOT_FOUND", Message: "租约不存在",
		Recovery: authprotocol.RecoveryNone, Hint: "租约号不存在或不属于当前用户；换了账号之后旧租约不能再结算"},
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
	{Code: 41060, Name: "CLOUD_CONTENT_LOST", Message: "云端内容已丢失",
		Recovery: authprotocol.RecoveryNone, Hint: "这个修订的内容在存储里已经不存在，重试也读不回来；用本机数据带当前 revision（从 revisions 取 current）重新写入即可覆盖"},
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
	// ── 通用限频 ──
	// 防火墙全局限流与崩溃报告（同一设备 / 同一 IP 每小时 20 份）共用这一个码。
	{Code: errCodeCrashReportRateLimited, Name: "RATE_LIMITED", Message: "请求过于频繁，请稍后再试",
		Recovery: authprotocol.RecoveryNone, Hint: "不要立即重试；崩溃报告留在本机，过一段时间（如下次启动）再发"},
	// ── 意见反馈与工单附件 ──
	{Code: errCodeFeedbackRateLimited, Name: "FEEDBACK_RATE_LIMITED", Message: feedbackRateLimitedMessage,
		Recovery: authprotocol.RecoveryNone, Hint: "同一用户每小时最多 5 条、每天最多 20 条反馈，稍后再提交"},
	{Code: errCodeTicketAttachmentLimit, Name: "TICKET_ATTACHMENT_LIMIT", Message: "图片或附件数量超过上限",
		Recovery: authprotocol.RecoveryNone, Hint: "反馈每次最多 4 张图片（imageIds）、2 个附件（attachmentIds）"},
	{Code: errCodeTicketAttachmentUnavailable, Name: "TICKET_ATTACHMENT_UNAVAILABLE", Message: feedbackAttachmentUnavailableNotice,
		Recovery: authprotocol.RecoveryNone, Hint: "附件只能由上传者本人使用一次；已提交过的附件要重新上传"},
	{Code: errCodeFeedbackAttachmentKind, Name: "FEEDBACK_ATTACHMENT_KIND_MISMATCH", Message: "图片与附件的位置不符",
		Recovery: authprotocol.RecoveryNone, Hint: "kind=image 上传的放 imageIds，kind=file 上传的放 attachmentIds"},
	{Code: errCodeFeedbackImageUnsupported, Name: "FEEDBACK_IMAGE_UNSUPPORTED", Message: "图片仅支持 PNG、JPEG、WebP、GIF 格式",
		Recovery: authprotocol.RecoveryNone, Hint: "按文件内容判定，改扩展名或 Content-Type 无效；其它文件请用 kind=file 上传"},
	{Code: errCodeFeedbackAttachmentTooLarge, Name: "FEEDBACK_ATTACHMENT_TOO_LARGE", Message: "图片或附件过大",
		Recovery: authprotocol.RecoveryNone, Hint: "图片不超过 10MB，附件不超过 20MB"},
	{Code: errCodeFeedbackNotFound, Name: "FEEDBACK_NOT_FOUND", Message: "反馈不存在", Recovery: authprotocol.RecoveryNone},
	{Code: errCodeFeedbackCategoryUnavailable, Name: "FEEDBACK_CATEGORY_UNAVAILABLE", Message: "反馈分类不存在或已停用",
		Recovery: authprotocol.RecoveryNone, Hint: "重新拉一次 /feedback/categories"},
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
	// 版本管控（docs/client-control.md）
	{Code: 40329, Name: "CLIENT_BLOCKED", Message: "当前客户端已停用", Recovery: authprotocol.RecoveryNone,
		Hint: "服务端已停用这个包名或版本；客户端应立即调 /client-control/check 取签名裁决并展示停用页"},
	{Code: 40326, Name: "CLIENT_UPDATE_REQUIRED", Message: "当前版本需要更新", Recovery: authprotocol.RecoveryNone,
		Hint: "客户端应立即调 /client-control/check 取签名裁决，引导用户更新；/releases/* 不受影响"},
	{Code: 40457, Name: "CLIENT_CONTROL_KEY_UNKNOWN", Message: "管控加密密钥不存在或已失效",
		Recovery: authprotocol.RecoveryRefreshConfig,
		Hint:     "强制刷新 /config 取新的 clientControl.boxKey，用新密钥重新加密后重试一次"},
	{Code: 50391, Name: "CLIENT_CONTROL_UNAVAILABLE", Message: "版本管控未启用", Recovery: authprotocol.RecoveryNone,
		Hint: "服务端未部署版本管控；客户端沿用已保存的裁决"},
	{Code: 42670, Name: "UPGRADE_REQUIRED", Message: "该应用要求使用加密载荷",
		Recovery: authprotocol.RecoveryRefreshConfig, Hint: "应用已升到 sealed 档，不能再发明文"},
	{Code: 50372, Name: "SIGNING_SECRET_MISSING", Message: "尚未签发应用密钥",
		Recovery: authprotocol.RecoveryNone, Hint: "让管理员在控制台轮换一次应用密钥"},
	// ── 一起听（docs/listen-together.md）──没有权益沿用 40230 FEATURE_NOT_GRANTED
	{Code: errCodeListenNotEnabled, Name: "LISTEN_TOGETHER_NOT_ENABLED", Message: "一起听服务尚未开通",
		Recovery: authprotocol.RecoveryNone, Hint: "服务端没有配置 LISTEN_TOGETHER_ENDPOINTS；客户端隐藏一起听入口"},
	{Code: errCodeListenUnavailable, Name: "LISTEN_TOGETHER_UNAVAILABLE", Message: "一起听服务暂时不可用",
		Recovery: authprotocol.RecoveryNone, Hint: "稍后重试"},
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
