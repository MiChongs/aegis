package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 应用接入网关 /api/v1/apps/:appkey/*。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerGatewayRoutes 注册应用接入网关 —— 接入方唯一需要认识的命名空间。
// 完整规格见 docs/app-integration.md。
func registerGatewayRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		authService = deps.Auth
	)
	// 应用接入网关：接入方唯一需要认识的命名空间。
	// 同一批路径在 standard / signed / sealed 三档下结构完全一致，
	// 由 middleware.AppGateway 按应用策略决定请求的包装方式。
	appGateway := router.Group("/api/v1/apps/:appkey")
	{
		appGateway.GET("/config", h.AppConfig)
		// 公开模型目录按名字批量查能力（免登录：用户接自己的 AI 供应商时不一定登录了）
		appGateway.GET("/ai/catalog/lookup", h.AppAICatalogLookup)
		appGateway.POST("/captcha", h.AppCaptcha)
		appGateway.POST("/auth/sms/code", h.AppSMSCode)
		appGateway.POST("/auth/register", h.AppRegister)
		appGateway.POST("/auth/login", h.AppLogin)
		appGateway.POST("/auth/refresh", h.AppRefresh)
		appGateway.POST("/auth/2fa/verify", h.AppSecondFactor)
		// 登录设备数已满时，用户选好要下线的设备后凭凭证完成登录（docs/device-limit.md）
		appGateway.POST("/auth/device-limit/resolve", h.AppDeviceLimitResolve)
		// 网页扫码登录：网页发码与轮询（网站服务端代为调用），扫码与确认在 Bearer 组
		appGateway.POST("/auth/qr/create", h.AppQRLoginCreate)
		appGateway.POST("/auth/qr/poll", h.AppQRLoginPoll)
		appGateway.POST("/auth/oauth/url", h.AppOAuthURL)
		// 回跳由第三方平台重定向浏览器发起，客户端无法给它签名或加密，
		// 因此 AppGateway 对该路径放行（详见 AppOAuthCallback 的注释）
		appGateway.GET("/auth/oauth/callback", h.AppOAuthCallback)
		appGateway.POST("/auth/oauth/exchange", h.AppOAuthExchange)
		// 原生登录核实的身份还没关联账号时，用户选「快速注册」；选「绑定已有账号」走 Bearer 组的 link/bind
		appGateway.POST("/auth/oauth/link/register", h.AppOAuthLinkRegister)
		// 登录之前就要用的入口：应用由路径注入（见 injectGatewayAppID）
		appGateway.POST("/auth/email/code", h.AppEmailCode)
		appGateway.POST("/auth/email/verify", h.AppEmailVerify)
		appGateway.POST("/auth/password/forgot", h.AppPasswordForgot)
		appGateway.POST("/auth/password/reset/verify", h.AppPasswordResetVerify)
		// 找回密码：流程令牌贯穿四步，规则见 service/auth_password_recovery.go
		appGateway.POST("/auth/password/recovery/start", h.AppPasswordRecoveryStart)
		appGateway.POST("/auth/password/recovery/status", h.AppPasswordRecoveryStatus)
		appGateway.POST("/auth/password/recovery/send", h.AppPasswordRecoverySend)
		appGateway.POST("/auth/password/recovery/verify", h.AppPasswordRecoveryVerify)
		appGateway.POST("/auth/password/recovery/complete", h.AppPasswordRecoveryComplete)
		appGateway.POST("/auth/passkey/options", h.AppPasskeyOptions)
		appGateway.POST("/auth/passkey/login", h.AppPasskeyLogin)
		// 免登录的内容接口
		appGateway.GET("/banners", h.AppBanners)
		// 点击上报：banners.click_count 从建表起就在，却从来没有代码写过它。
		// 没有这条接口，控制台上的「点击 0」既可能是真没人点、也可能是根本没统计。
		appGateway.POST("/banners/:bannerId/click", h.AppBannerClick)
		appGateway.GET("/notices", h.AppNotices)
		appGateway.GET("/version/check", h.AppVersionCheck)
		// 注册页填完邀请码后的预览（码能不能用、谁邀请的、填了能拿什么）
		appGateway.POST("/invite/preview", h.AppPreviewInvite)
	}

	// 发布中心：免登录可用，带令牌时按用户定向（渠道、内测名单、灰度）。
	// 令牌无效按未登录处理，令牌属于别的应用则拒绝（与 Bearer 组同一口径）。
	appGatewayOptional := router.Group("/api/v1/apps/:appkey")
	appGatewayOptional.Use(middleware.OptionalAuth(authService), middleware.AppGatewayTokenScope())
	{
		appGatewayOptional.GET("/releases", h.AppReleaseHistory)
		appGatewayOptional.GET("/releases/check", h.AppReleaseCheck)
		appGatewayOptional.GET("/releases/latest", h.AppReleaseLatest)
		appGatewayOptional.POST("/releases/events", h.AppReleaseEvent)
		// 版本管控：加密上报身份、取签名裁决，以及免登录的长轮询变更通知（见 docs/client-control.md）
		appGatewayOptional.POST("/client-control/check", h.AppClientControlCheck)
		appGatewayOptional.GET("/client-control/watch", h.AppClientControlWatch)
		// 广告服务策略与开屏上报：未登录也要知道基础服务有哪些、开屏能不能放；
		// 带令牌时按账号回答，开屏记录也挂到账号上。
		appGatewayOptional.GET("/ads/policy", h.AppAdPolicy)
		appGatewayOptional.POST("/ads/splash/events", h.AppReportSplashAd)
		// 崩溃报告：用户在崩溃屏上逐份同意后上报，未登录也能发；带令牌时挂到账号上（docs/crash-reports.md）
		appGatewayOptional.POST("/crash-reports", h.AppSubmitCrashReport)
	}

	// 网关内需要 Bearer 令牌的部分。
	//
	// AppGatewayTokenScope 挂在 Auth 之后：Auth 证明令牌有效，它证明令牌是**这个应用**的 ——
	// 否则拿 A 应用的令牌请求 /apps/B/me 会成功并返回 A 的资料，两边都不报错。
	//
	// 这一组存在的意义是让接入方只认识一个命名空间：同样的三档包装、同样的信封、
	// 同样的错误码，覆盖登录之后客户端真正要用的全部能力。旧的 /api/user/* 等
	// 命名空间原样保留，由各应用的 allowLegacy 控制，两者正交。
	appGatewayAuthed := router.Group("/api/v1/apps/:appkey")
	appGatewayAuthed.Use(middleware.Auth(authService), middleware.AppGatewayTokenScope())
	{
		appGatewayAuthed.POST("/auth/logout", h.AppLogout)
		// 发布渠道的自助加入与退出（如 Beta 体验计划）
		appGatewayAuthed.GET("/releases/channels", h.AppReleaseChannels)
		appGatewayAuthed.POST("/releases/channels/:code/join", h.AppReleaseChannelJoin)
		appGatewayAuthed.POST("/releases/channels/:code/leave", h.AppReleaseChannelLeave)
		appGatewayAuthed.POST("/auth/password/verify", h.VerifyPassword)
		appGatewayAuthed.POST("/auth/password/change", h.ChangePassword)
		// 已登录时忘了原密码：发起同一套找回流程，后续步骤走 /auth/password/recovery/*
		appGatewayAuthed.POST("/me/password/recovery", h.AppMyPasswordRecoveryStart)
		appGatewayAuthed.POST("/auth/oauth/bind/url", h.OAuthBindURL)
		appGatewayAuthed.POST("/auth/oauth/bind/token", h.OAuthBindToken)
		appGatewayAuthed.POST("/auth/oauth/link/bind", h.OAuthLinkBind)
		appGatewayAuthed.GET("/auth/oauth/bindings", h.ListMyOAuthBindings)
		appGatewayAuthed.DELETE("/auth/oauth/bindings/:provider", h.UnbindMyOAuthProvider)

		// 卡密：兑换与「我的授权」。授权卡登录本身不在这里 ——
		// 它是 /auth/login 的一档 method，加登录方式不加路由。
		appGatewayAuthed.POST("/card-keys/preview", h.AppPreviewCardKey)
		appGatewayAuthed.POST("/card-keys/redeem", h.AppRedeemCardKey)
		appGatewayAuthed.GET("/card-keys/mine", h.AppMyCardKeys)
		appGatewayAuthed.DELETE("/card-keys/mine/:cardId/devices/:deviceId", h.AppUnbindMyCardKeyDevice)
		appGatewayAuthed.GET("/card-keys/redemptions", h.AppMyCardKeyRedemptions)

		// 激励广告：状态（含传给 SDK 的用户标识）、看完上报、我的记录。
		// 平台的服务端回调不在这里 —— 它没有用户令牌可带，走 /api/apps/:appkey/ads/*。
		appGatewayAuthed.GET("/ads/rewarded", h.AppRewardedAdStatus)
		appGatewayAuthed.POST("/ads/rewarded/claim", h.AppClaimRewardedAd)
		appGatewayAuthed.GET("/ads/rewarded/records", h.AppRewardedAdRecords)
		// 对广告服务的选择记在账号上；未登录时的选择由客户端在登录后用 ifAbsent 同步上来。
		appGatewayAuthed.POST("/ads/consent", h.AppSaveAdConsent)

		// 功能时长：余额、先租后用、停用时结算（看激励广告领到的「在线听歌 60 分钟」按实际使用扣）
		appGatewayAuthed.GET("/features/time/:feature", h.AppFeatureTimeStatus)
		appGatewayAuthed.POST("/features/time/:feature/leases", h.AppLeaseFeatureTime)
		appGatewayAuthed.POST("/features/time/leases/:leaseNo/settle", h.AppSettleFeatureTimeLease)

		// 当前用户：资料 / 设置 / 安全概览
		appGatewayAuthed.GET("/me", h.AppMe)
		appGatewayAuthed.GET("/me/profile", h.Profile)
		appGatewayAuthed.PUT("/me/profile", h.UpdateProfile)
		appGatewayAuthed.POST("/me/profile/changes/confirm", h.ConfirmProfileChange)
		appGatewayAuthed.POST("/me/avatar", h.UploadUserAvatar)
		appGatewayAuthed.DELETE("/me/avatar", h.RemoveUserAvatar)
		appGatewayAuthed.GET("/me/avatar/history", h.ListUserAvatarHistory)
		appGatewayAuthed.POST("/me/avatar/restore", h.RestoreUserAvatar)
		appGatewayAuthed.GET("/me/settings", h.Settings)
		appGatewayAuthed.PUT("/me/settings", h.UpdateSettings)
		// 自动签到：状态（开放策略、资格、下一次执行、最近一次结果）与设置
		appGatewayAuthed.GET("/me/auto-sign", h.AppAutoSignStatus)
		appGatewayAuthed.PUT("/me/auto-sign", h.AppUpdateAutoSign)
		appGatewayAuthed.GET("/me/security", h.Security)
		// 账号初始化：快速登录建的号补设用户名与密码；首次弹窗、之后横幅（docs/account-setup.md）
		appGatewayAuthed.GET("/me/account-setup", h.AppAccountSetupStatus)
		appGatewayAuthed.POST("/me/account-setup", h.AppCompleteAccountSetup)
		appGatewayAuthed.POST("/me/account-setup/check", h.AppAccountSetupCheck)
		appGatewayAuthed.POST("/me/account-setup/prompted", h.AppAccountSetupPrompted)
		appGatewayAuthed.POST("/me/account-setup/dismiss", h.AppAccountSetupDismiss)
		// 账号注销：注销页（enabled=false 时入口隐藏）/ 发验证码 / 提交 / 撤销
		appGatewayAuthed.GET("/me/deletion", h.AppAccountDeletion)
		appGatewayAuthed.POST("/me/deletion/code", h.AppAccountDeletionCode)
		appGatewayAuthed.POST("/me/deletion", h.AppSubmitAccountDeletion)
		appGatewayAuthed.POST("/me/deletion/cancel", h.AppCancelAccountDeletion)

		// 二次认证
		appGatewayAuthed.POST("/me/2fa/totp/enroll", h.BeginTOTPEnrollment)
		appGatewayAuthed.POST("/me/2fa/totp/enable", h.EnableTOTP)
		appGatewayAuthed.POST("/me/2fa/totp/disable", h.DisableTOTP)
		appGatewayAuthed.GET("/me/2fa/recovery-codes", h.ListRecoveryCodes)
		appGatewayAuthed.POST("/me/2fa/recovery-codes", h.GenerateRecoveryCodes)
		appGatewayAuthed.POST("/me/2fa/recovery-codes/regenerate", h.RegenerateRecoveryCodes)

		// Passkey 管理
		appGatewayAuthed.GET("/me/passkeys", h.ListPasskeys)
		appGatewayAuthed.POST("/me/passkeys/options", h.BeginPasskeyRegistration)
		appGatewayAuthed.POST("/me/passkeys", h.FinishPasskeyRegistration)
		appGatewayAuthed.DELETE("/me/passkeys/:credentialId", h.DeletePasskey)

		// 会话与登录审计
		appGatewayAuthed.GET("/me/sessions", h.UserSessions)
		appGatewayAuthed.DELETE("/me/sessions/:tokenHash", h.RevokeUserSession)
		appGatewayAuthed.POST("/me/sessions/revoke-all", h.RevokeAllUserSessions)
		// 登录设备：按设备合并的在线登录、生效上限与占用，下线其他设备
		appGatewayAuthed.GET("/me/devices", h.AppMyDevices)
		appGatewayAuthed.POST("/me/devices/remove", h.AppRemoveMyDevices)
		appGatewayAuthed.GET("/me/oauth2/grants", h.AppMyOAuth2Grants)
		appGatewayAuthed.DELETE("/me/oauth2/grants/:clientId", h.AppRevokeMyOAuth2Grant)

		// 网页扫码登录：移动端扫码、确认与拒绝
		appGatewayAuthed.POST("/auth/qr/scan", h.AppQRLoginScan)
		appGatewayAuthed.POST("/auth/qr/confirm", h.AppQRLoginConfirm)
		appGatewayAuthed.POST("/auth/qr/cancel", h.AppQRLoginCancel)
		appGatewayAuthed.GET("/me/audits/login", h.UserLoginAudits)
		appGatewayAuthed.GET("/me/audits/sessions", h.UserSessionAudits)

		// 签到 / 积分 / 排行榜
		appGatewayAuthed.GET("/signin/status", h.SignInStatus)
		appGatewayAuthed.POST("/signin", h.SignIn)
		appGatewayAuthed.GET("/signin/history", h.SignInHistory)
		appGatewayAuthed.GET("/points/overview", h.PointsOverview)
		appGatewayAuthed.GET("/points/level", h.MyLevel)
		appGatewayAuthed.GET("/points/levels", h.PointsLevels)
		appGatewayAuthed.GET("/points/level/rewards", h.MyLevelRewards)
		appGatewayAuthed.POST("/points/level/rewards/:rewardId/claim", h.ClaimLevelReward)
		appGatewayAuthed.GET("/points/level/history", h.MyLevelHistory)
		appGatewayAuthed.GET("/points/integral-transactions", h.IntegralTransactions)
		appGatewayAuthed.GET("/points/experience-transactions", h.ExperienceTransactions)
		// 积分商城：首页（含每件商品的可兑换判定）/ 兑换 / 我的兑换记录
		appGatewayAuthed.GET("/points/store", h.AppPointsStore)
		appGatewayAuthed.POST("/points/store/redeem", h.AppRedeemPointsStoreItem)
		appGatewayAuthed.GET("/points/store/redemptions", h.AppPointsStoreRedemptions)
		// 抽奖：活动列表、活动页（奖品与公示概率、我的次数、保底进度）、抽一次、我的记录。
		// 抽中什么由服务端在一个事务里决定并到账；视频抽奖走激励广告场景，看完发的是抽奖次数。
		appGatewayAuthed.GET("/lottery", h.AppLotteryActivities)
		appGatewayAuthed.GET("/lottery/activities/:activityId", h.AppLotteryState)
		appGatewayAuthed.POST("/lottery/activities/:activityId/draw", h.AppLotteryDraw)
		appGatewayAuthed.POST("/lottery/activities/:activityId/draw/batch", h.AppLotteryDrawBatch)
		appGatewayAuthed.GET("/lottery/draws", h.AppLotteryDraws)
		appGatewayAuthed.GET("/lottery/tickets", h.AppLotteryTickets)
		// 邀请：邀请页 / 我邀请的人 / 注册后限时补填 / 换我的邀请码
		appGatewayAuthed.GET("/invite", h.AppInviteCenter)
		appGatewayAuthed.GET("/invite/invitees", h.AppInvitees)
		appGatewayAuthed.POST("/invite/bind", h.AppBindInvite)
		appGatewayAuthed.PUT("/invite/code", h.AppUpdateInviteCode)
		appGatewayAuthed.GET("/leaderboard/summary", h.LeaderboardSummary)
		appGatewayAuthed.GET("/leaderboard/me", h.LeaderboardMe)
		appGatewayAuthed.GET("/leaderboard/points/:type", h.LeaderboardPoints)
		appGatewayAuthed.GET("/leaderboard/signin/:type", h.LeaderboardSignIn)
		// 积分排行榜（新版，docs/leaderboard.md）：榜单 / 往期 / 我的奖励记录 / 匿名偏好。
		// 叠一层排行榜专用限流（每用户每分钟 60 次），与 /api/leaderboard 同一档。
		leaderboardLimit := middleware.LeaderboardRateLimit(60)
		appGatewayAuthed.GET("/leaderboard/boards", leaderboardLimit, h.AppLeaderboardBoards)
		appGatewayAuthed.GET("/leaderboard/boards/:key", leaderboardLimit, h.AppLeaderboardBoard)
		appGatewayAuthed.GET("/leaderboard/boards/:key/periods", leaderboardLimit, h.AppLeaderboardPeriods)
		appGatewayAuthed.GET("/leaderboard/boards/:key/periods/:periodKey", leaderboardLimit, h.AppLeaderboardPeriod)
		appGatewayAuthed.GET("/leaderboard/rewards", leaderboardLimit, h.AppLeaderboardRewards)
		appGatewayAuthed.GET("/leaderboard/preference", leaderboardLimit, h.AppLeaderboardPreference)
		appGatewayAuthed.PUT("/leaderboard/preference", leaderboardLimit, h.AppUpdateLeaderboardPreference)

		// 站内信
		// 消息中心（docs/notification-center.md）；与 /api/notifications 同一组 handler
		appGatewayAuthed.GET("/notifications", h.Notifications)
		appGatewayAuthed.GET("/notifications/summary", h.NotificationSummary)
		appGatewayAuthed.GET("/notifications/unread-count", h.NotificationUnreadCount)
		appGatewayAuthed.GET("/notifications/preferences", h.NotificationPreferences)
		appGatewayAuthed.PUT("/notifications/preferences", h.UpdateNotificationPreferences)
		appGatewayAuthed.GET("/notifications/:notificationId", h.NotificationDetail)
		appGatewayAuthed.POST("/notifications/read", h.ReadNotification)
		appGatewayAuthed.POST("/notifications/unread", h.UnreadNotification)
		appGatewayAuthed.POST("/notifications/read-batch", h.ReadNotificationsBatch)
		appGatewayAuthed.POST("/notifications/read-all", h.ReadAllNotifications)
		appGatewayAuthed.POST("/notifications/delete-batch", h.DeleteNotificationsBatch)
		appGatewayAuthed.POST("/notifications/clear", h.ClearNotifications)
		appGatewayAuthed.DELETE("/notifications/:notificationId", h.DeleteNotification)

		// 钱包 / 会员 / 支付
		appGatewayAuthed.GET("/wallet", h.MyWallet)
		appGatewayAuthed.GET("/wallet/transactions", h.MyWalletTransactions)
		appGatewayAuthed.POST("/wallet/consume", h.WalletConsume)
		appGatewayAuthed.GET("/vip/plans", h.VipPlans)
		appGatewayAuthed.GET("/vip/status", h.MyVipStatus)
		appGatewayAuthed.GET("/vip/features", h.VipFeatures)
		appGatewayAuthed.POST("/vip/feature-ticket", h.VipFeatureTicket)
		// 一起听：签短期票据并给出一起听微服务的连接地址（docs/listen-together.md）
		appGatewayAuthed.POST("/listen-together/ticket", h.AppListenTogetherTicket)
		appGatewayAuthed.GET("/vip/transactions", h.MyVipTransactions)
		appGatewayAuthed.GET("/vip/conversions", h.MyVipConversions)
		appGatewayAuthed.POST("/vip/purchase", h.PurchaseVip)
		appGatewayAuthed.POST("/vip/trial", h.ClaimVipTrial)
		appGatewayAuthed.GET("/pay/methods", h.UserPaymentMethods)
		appGatewayAuthed.GET("/pay/orders", h.PaymentOrders)
		appGatewayAuthed.POST("/pay/orders", h.CreatePaymentOrder)
		appGatewayAuthed.GET("/pay/orders/:orderNo", h.PaymentOrderDetail)

		// AI 中转网关（docs/ai-gateway.md）：总览、可用模型与单价、API 密钥、调用记录、赠送额度流水。
		// 调用面本身在 /api/ai/v1（OpenAI SDK 直连，不经网关包装）。
		appGatewayAuthed.GET("/ai/overview", h.AppAIOverview)
		appGatewayAuthed.GET("/ai/models", h.AppAIModels)
		appGatewayAuthed.GET("/ai/keys", h.AppAIKeys)
		appGatewayAuthed.POST("/ai/keys", h.AppCreateAIKey)
		appGatewayAuthed.PUT("/ai/keys/:keyId", h.AppUpdateAIKey)
		appGatewayAuthed.DELETE("/ai/keys/:keyId", h.AppDeleteAIKey)
		appGatewayAuthed.GET("/ai/requests", h.AppAIRequests)
		appGatewayAuthed.GET("/ai/requests/:requestNo", h.AppAIRequest)
		appGatewayAuthed.GET("/ai/credits", h.AppAICredits)

		// 存储
		appGatewayAuthed.POST("/storage/upload", h.StorageUpload)
		appGatewayAuthed.POST("/storage/object-link", h.StorageObjectLink)

		// 用户云存储：按「命名空间 / 键」存放任意文档，修订可回滚、删除先进回收站。
		// 键不含斜杠（见 cloudstorage.ValidKey），所以 :key 之后的静态段不会与键本身混淆。
		appGatewayAuthed.GET("/cloud", h.AppCloudStatus)
		appGatewayAuthed.GET("/cloud/items", h.AppCloudItems)
		appGatewayAuthed.GET("/cloud/items/:namespace/:key", h.AppCloudItem)
		appGatewayAuthed.PUT("/cloud/items/:namespace/:key", h.AppPutCloudItem)
		appGatewayAuthed.DELETE("/cloud/items/:namespace/:key", h.AppDeleteCloudItem)
		appGatewayAuthed.POST("/cloud/items/:namespace/:key/upload", h.AppUploadCloudItem)
		appGatewayAuthed.POST("/cloud/items/:namespace/:key/restore", h.AppRestoreCloudItem)
		appGatewayAuthed.GET("/cloud/items/:namespace/:key/revisions", h.AppCloudItemRevisions)
		appGatewayAuthed.POST("/cloud/items/:namespace/:key/rollback", h.AppRollbackCloudItem)
		appGatewayAuthed.POST("/cloud/items/:namespace/:key/link", h.AppCloudItemLink)

		// 工单（用户自助）
		appGatewayAuthed.GET("/tickets", h.UserListTickets)
		appGatewayAuthed.POST("/tickets", h.UserCreateTicket)
		appGatewayAuthed.GET("/tickets/categories", h.UserListTicketCategories)
		appGatewayAuthed.POST("/tickets/attachments", h.UserUploadTicketAttachment)
		appGatewayAuthed.GET("/tickets/:ticketId", h.UserGetTicket)
		appGatewayAuthed.POST("/tickets/:ticketId/replies", h.UserReplyTicket)
		appGatewayAuthed.POST("/tickets/:ticketId/rating", h.UserRateTicket)
		appGatewayAuthed.POST("/tickets/:ticketId/cancel", h.UserCancelTicket)

		// 意见反馈：kind=feedback 的工单，独立入口与更严的入参（见 feedback_service.go）
		appGatewayAuthed.GET("/feedback/categories", h.AppFeedbackCategories)
		appGatewayAuthed.POST("/feedback/attachments", h.AppUploadFeedbackAttachment)
		appGatewayAuthed.POST("/feedback", h.AppCreateFeedback)
		appGatewayAuthed.GET("/feedback", h.AppListFeedback)
		appGatewayAuthed.GET("/feedback/:feedbackId", h.AppGetFeedback)
		appGatewayAuthed.POST("/feedback/:feedbackId/replies", h.AppReplyFeedback)
		appGatewayAuthed.POST("/feedback/:feedbackId/rating", h.AppRateFeedback)
		appGatewayAuthed.POST("/feedback/:feedbackId/cancel", h.AppCancelFeedback)
	}
}
