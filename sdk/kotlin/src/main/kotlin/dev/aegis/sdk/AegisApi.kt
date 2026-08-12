package dev.aegis.sdk

import kotlinx.serialization.json.JsonElement
import java.io.File
import java.io.IOException

/**
 * 类型化门面 —— 覆盖 `/api/v1/apps/{appKey}` 命名空间下的全部接口。
 *
 * 这里**不能**把路径写成带通配符的形式：Kotlin 的块注释可以嵌套，
 * 注释体里出现一个 `/` 加 `*` 就会多开一层，其后的 `*` 加 `/` 只关掉内层，
 * 整个文件会被当成注释一路吃到 EOF，报一句位置在文件末尾的 Unclosed comment。
 *
 * 分组与 `/config` 下发的接口目录（服务端 `auth_protocol_catalog.go`）一一对应。
 * 需要目录里有但这里没封的接口时，用 [AegisClient.call] 直接调，
 * 路径与参数照 `config.operations` 抄。
 *
 * 所有方法都是**同步**的：Android 上请放在 IO 线程 / 协程里调用，
 * 服务端 Java 里直接调。SDK 不替调用方决定并发模型。
 */

/** 认证生命周期：注册、登录、令牌、第三方、找回密码。 */
class AegisAuthApi internal constructor(private val client: AegisClient) {

    /** 按策略签发图形验证码。purpose: `login` | `register`。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun captcha(purpose: String = "login"): JsonElement =
        client.call("POST", "/captcha", mapOf("purpose" to purpose))

    /** 申请短信验证码。应用开了图形验证码时要一并带上 captchaId/captchaAnswer 防轰炸。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun sendSmsCode(
        phone: String,
        purpose: String = "login",
        captchaId: String? = null,
        captchaAnswer: String? = null,
    ): JsonElement = client.call(
        "POST", "/auth/sms/code",
        buildBody(
            "phone" to phone, "purpose" to purpose,
            "captchaId" to captchaId, "captchaAnswer" to captchaAnswer,
        )
    )

    @Throws(IOException::class)
    @JvmOverloads
    fun loginWithPassword(
        account: String,
        password: String,
        deviceId: String? = null,
        device: String? = null,
        captchaId: String? = null,
        captchaAnswer: String? = null,
    ): AegisSession = login(
        buildBody(
            "method" to "password", "account" to account, "password" to password,
            "deviceId" to deviceId, "device" to device,
            "captchaId" to captchaId, "captchaAnswer" to captchaAnswer,
        )
    )

    /** 短信登录。手机号未注册时：应用开了短信注册就自动建号，否则 40394。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun loginWithSms(
        phone: String,
        code: String,
        deviceId: String? = null,
        device: String? = null,
    ): AegisSession = login(
        buildBody("method" to "sms", "phone" to phone, "code" to code, "deviceId" to deviceId, "device" to device)
    )

    /**
     * 注册。`profile` 里能放哪些字段由 `/config` 的 `auth.registrationSchema` 决定，
     * **不要在客户端硬编码表单** —— 管理员加一个必填字段就得发版。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun register(
        account: String? = null,
        password: String? = null,
        phone: String? = null,
        code: String? = null,
        nickname: String? = null,
        profile: Map<String, Any?>? = null,
        method: String = "password",
        captchaId: String? = null,
        captchaAnswer: String? = null,
    ): AegisSession = login(
        buildBody(
            "method" to method, "account" to account, "password" to password,
            "phone" to phone, "code" to code, "nickname" to nickname, "profile" to profile,
            "captchaId" to captchaId, "captchaAnswer" to captchaAnswer,
        ),
        path = "/auth/register",
    )

    /** 用保存的 refreshToken 换一对新令牌，并写回 tokenStore。 */
    @Throws(IOException::class)
    fun refresh(): AegisSession {
        val refreshToken = client.tokens.refreshToken()
            ?: throw AegisException.fromCode(40100, "没有可用的 refreshToken，请重新登录")
        return login(mapOf("refreshToken" to refreshToken), path = "/auth/refresh")
    }

    /**
     * 完成登录返回的二次认证挑战。
     *
     * `challengeId` 取自 [AegisSession.challenge]。用恢复码时把它传给 [recoveryCode]，
     * [code] 留空。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun verifySecondFactor(
        challengeId: String,
        code: String? = null,
        recoveryCode: String? = null,
    ): AegisSession = login(
        buildBody("challengeId" to challengeId, "code" to code, "recoveryCode" to recoveryCode),
        path = "/auth/2fa/verify",
    )

    @Throws(IOException::class)
    fun logout() {
        client.call("POST", "/auth/logout", requireAuth = true)
        client.tokens.clear()
    }

    // ── 第三方登录 ──

    /** 取授权地址（Web / 系统浏览器跳转流程的第一步）。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun oauthAuthorizeUrl(provider: String, deviceId: String? = null): String {
        val data = client.call("POST", "/auth/oauth/url", buildBody("provider" to provider, "deviceId" to deviceId))
        return client.decodeAs(data, OAuthUrl.serializer()).url
    }

    /**
     * 原生 SDK 拿到第三方 profile 后换取 Aegis 会话。
     *
     * 这条路径受完整安全等级包装保护；浏览器回跳那条（`/auth/oauth/callback`）
     * 无法被客户端包装，sealed 档下也是明文的。要求全链路加密就走这里。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun oauthExchange(
        provider: String,
        providerUserId: String,
        unionId: String? = null,
        nickname: String? = null,
        avatar: String? = null,
        email: String? = null,
        accessToken: String? = null,
        deviceId: String? = null,
    ): AegisSession = login(
        buildBody(
            "provider" to provider, "providerUserId" to providerUserId, "unionId" to unionId,
            "nickname" to nickname, "avatar" to avatar, "email" to email,
            "accessToken" to accessToken, "deviceId" to deviceId,
        ),
        path = "/auth/oauth/exchange",
    )

    @Throws(IOException::class)
    fun oauthBindUrl(provider: String): String {
        val data = client.call("POST", "/auth/oauth/bind/url", mapOf("provider" to provider), requireAuth = true)
        return client.decodeAs(data, OAuthUrl.serializer()).url
    }

    @Throws(IOException::class)
    fun oauthBindings(): JsonElement = client.call("GET", "/auth/oauth/bindings", requireAuth = true)

    @Throws(IOException::class)
    fun oauthUnbind(provider: String): JsonElement =
        client.call("DELETE", "/auth/oauth/bindings/$provider", requireAuth = true)

    // ── 邮箱与密码 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun sendEmailCode(email: String, purpose: String = "register"): JsonElement =
        client.call("POST", "/auth/email/code", mapOf("email" to email, "purpose" to purpose))

    @Throws(IOException::class)
    @JvmOverloads
    fun verifyEmailCode(email: String, code: String, purpose: String = "register"): JsonElement =
        client.call("POST", "/auth/email/verify", mapOf("email" to email, "code" to code, "purpose" to purpose))

    @Throws(IOException::class)
    @JvmOverloads
    fun forgotPassword(email: String, resetUrl: String? = null): JsonElement =
        client.call("POST", "/auth/password/forgot", buildBody("email" to email, "resetUrl" to resetUrl))

    @Throws(IOException::class)
    fun verifyResetToken(email: String, token: String): JsonElement =
        client.call("POST", "/auth/password/reset/verify", mapOf("email" to email, "token" to token))

    @Throws(IOException::class)
    fun verifyPassword(password: String): JsonElement =
        client.call("POST", "/auth/password/verify", mapOf("password" to password), requireAuth = true)

    @Throws(IOException::class)
    fun changePassword(oldPassword: String, newPassword: String): JsonElement = client.call(
        "POST", "/auth/password/change",
        mapOf("oldPassword" to oldPassword, "newPassword" to newPassword),
        requireAuth = true,
    )

    // ── Passkey 登录 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun passkeyOptions(account: String? = null): JsonElement =
        client.call("POST", "/auth/passkey/options", buildBody("account" to account))

    @Throws(IOException::class)
    fun passkeyLogin(credential: Map<String, Any?>, sessionId: String? = null): AegisSession =
        login(buildBody("credential" to credential, "sessionId" to sessionId), path = "/auth/passkey/login")

    /** 登录类接口的公共收口：解析会话并写进 tokenStore。 */
    private fun login(body: Map<String, Any?>, path: String = "/auth/login"): AegisSession {
        val session = client.decodeAs(client.call("POST", path, body), AegisSession.serializer())
        if (session.accessToken.isNotEmpty()) {
            client.tokens.save(session.accessToken, session.refreshToken)
        }
        return session
    }
}

/** 当前用户：资料、设置、安全、会话、二次认证、Passkey。 */
class AegisMeApi internal constructor(private val client: AegisClient) {

    @Throws(IOException::class)
    fun summary(): JsonElement = client.call("GET", "/me", requireAuth = true)

    @Throws(IOException::class)
    fun profile(): JsonElement = client.call("GET", "/me/profile", requireAuth = true)

    @Throws(IOException::class)
    fun updateProfile(fields: Map<String, Any?>): JsonElement =
        client.call("PUT", "/me/profile", fields, requireAuth = true)

    /**
     * 确认一次敏感字段变更（改邮箱 / 改手机号）。
     *
     * [field] 是 [updateProfile] 返回的 `pendingChanges[].field`，取值如 `email` /
     * `phone`；[code] 是服务端发到**新**邮箱或新手机号的验证码。
     *
     * 服务端要的就是这两个字段（`ConfirmProfileChangeRequest`，两个都是必填）。
     * 这里曾经传的是 `token`，于是每次确认都以「缺少 field」被拒 ——
     * 绑定邮箱这条链路因此从来没有走通过。
     */
    @Throws(IOException::class)
    fun confirmProfileChange(field: String, code: String): JsonElement =
        client.call("POST", "/me/profile/changes/confirm", mapOf("field" to field, "code" to code), requireAuth = true)

    /** 上传头像。sealed 档下整个 multipart 体会被加密，调用方式不变。 */
    @Throws(IOException::class)
    fun uploadAvatar(file: File): JsonElement = client.upload("/me/avatar", file)

    @Throws(IOException::class)
    @JvmOverloads
    fun settings(category: String? = null): JsonElement =
        client.call("GET", "/me/settings", query = buildQuery("category" to category), requireAuth = true)

    @Throws(IOException::class)
    /**
     * 更新某一类用户设置。
     *
     * 服务端要的是 `{category, settings}` 两层结构（UpdateSettingsRequest），
     * 不是把设置项摊平在请求体顶层 —— 摊平的写法会因为缺 category 被直接拒掉。
     */
    fun updateSettings(category: String, settings: Map<String, Any?>): JsonElement = client.call(
        "PUT", "/me/settings",
        mapOf("category" to category, "settings" to settings), requireAuth = true,
    )

    @Throws(IOException::class)
    fun security(): JsonElement = client.call("GET", "/me/security", requireAuth = true)

    // ── 二次认证 ──

    @Throws(IOException::class)
    fun enrollTotp(): JsonElement = client.call("POST", "/me/2fa/totp/enroll", requireAuth = true)

    @Throws(IOException::class)
    /**
     * 开启 TOTP。[enrollmentId] 取自 [enrollTotp] 的返回值，服务端用它定位本次绑定，
     * 缺了它这一步一定失败 —— 只发 code 是不够的。
     */
    fun enableTotp(enrollmentId: String, code: String): JsonElement = client.call(
        "POST", "/me/2fa/totp/enable",
        mapOf("enrollmentId" to enrollmentId, "code" to code), requireAuth = true,
    )

    @Throws(IOException::class)
    fun disableTotp(code: String): JsonElement =
        client.call("POST", "/me/2fa/totp/disable", mapOf("code" to code), requireAuth = true)

    @Throws(IOException::class)
    fun recoveryCodes(): JsonElement = client.call("GET", "/me/2fa/recovery-codes", requireAuth = true)

    @Throws(IOException::class)
    fun generateRecoveryCodes(): JsonElement = client.call("POST", "/me/2fa/recovery-codes", requireAuth = true)

    @Throws(IOException::class)
    fun regenerateRecoveryCodes(): JsonElement =
        client.call("POST", "/me/2fa/recovery-codes/regenerate", requireAuth = true)

    // ── Passkey 管理 ──

    @Throws(IOException::class)
    fun passkeys(): JsonElement = client.call("GET", "/me/passkeys", requireAuth = true)

    @Throws(IOException::class)
    fun passkeyRegisterOptions(): JsonElement = client.call("POST", "/me/passkeys/options", requireAuth = true)

    @Throws(IOException::class)
    /**
     * 完成 Passkey 注册。[challengeId] 取自 [passkeyRegisterOptions] 的返回值 ——
     * 服务端靠它把这次凭据与刚才那次挑战对上，是必填项。
     */
    @JvmOverloads
    fun registerPasskey(
        challengeId: String,
        credential: Map<String, Any?>,
        credentialName: String? = null,
    ): JsonElement = client.call(
        "POST", "/me/passkeys",
        buildBody(
            "challengeId" to challengeId,
            "credential" to credential,
            "credentialName" to credentialName,
        ),
        requireAuth = true,
    )

    @Throws(IOException::class)
    fun deletePasskey(credentialId: String): JsonElement =
        client.call("DELETE", "/me/passkeys/$credentialId", requireAuth = true)

    // ── 会话与审计 ──

    @Throws(IOException::class)
    fun sessions(): JsonElement = client.call("GET", "/me/sessions", requireAuth = true)

    @Throws(IOException::class)
    fun revokeSession(tokenHash: String): JsonElement =
        client.call("DELETE", "/me/sessions/$tokenHash", requireAuth = true)

    @Throws(IOException::class)
    fun revokeAllSessions(): JsonElement = client.call("POST", "/me/sessions/revoke-all", requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun loginAudits(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/me/audits/login", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun sessionAudits(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/me/audits/sessions", query = pageQuery(page, limit), requireAuth = true)
}

/** 签到、积分、排行榜、站内信、工单。 */
class AegisEngagementApi internal constructor(private val client: AegisClient) {

    @Throws(IOException::class)
    fun signInStatus(): JsonElement = client.call("GET", "/signin/status", requireAuth = true)

    @Throws(IOException::class)
    fun signIn(): JsonElement = client.call("POST", "/signin", requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun signInHistory(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/signin/history", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    fun pointsOverview(): JsonElement = client.call("GET", "/points/overview", requireAuth = true)

    @Throws(IOException::class)
    fun level(): JsonElement = client.call("GET", "/points/level", requireAuth = true)

    @Throws(IOException::class)
    fun levels(): JsonElement = client.call("GET", "/points/levels", requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun integralTransactions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/points/integral-transactions", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun experienceTransactions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/points/experience-transactions", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    fun leaderboardSummary(): JsonElement = client.call("GET", "/leaderboard/summary", requireAuth = true)

    @Throws(IOException::class)
    fun leaderboardMe(): JsonElement = client.call("GET", "/leaderboard/me", requireAuth = true)

    /** type: `integral` | `experience` | `level` */
    @Throws(IOException::class)
    fun leaderboardPoints(type: String): JsonElement =
        client.call("GET", "/leaderboard/points/$type", requireAuth = true)

    /** type: `today` | `consecutive` | `monthly` */
    @Throws(IOException::class)
    fun leaderboardSignIn(type: String): JsonElement =
        client.call("GET", "/leaderboard/signin/$type", requireAuth = true)

    // ── 站内信 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun notifications(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/notifications", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    fun unreadCount(): JsonElement = client.call("GET", "/notifications/unread-count", requireAuth = true)

    @Throws(IOException::class)
    fun readNotification(notificationId: Long): JsonElement =
        client.call("POST", "/notifications/read", mapOf("notificationId" to notificationId), requireAuth = true)

    @Throws(IOException::class)
    fun readNotifications(ids: List<Long>): JsonElement =
        // 服务端字段是 ids（NotificationReadBatchRequest）。
        client.call("POST", "/notifications/read-batch", mapOf("ids" to ids), requireAuth = true)

    @Throws(IOException::class)
    fun readAllNotifications(): JsonElement = client.call("POST", "/notifications/read-all", requireAuth = true)

    @Throws(IOException::class)
    fun deleteNotification(notificationId: Long): JsonElement =
        client.call("DELETE", "/notifications/$notificationId", requireAuth = true)

    @Throws(IOException::class)
    fun clearNotifications(): JsonElement = client.call("POST", "/notifications/clear", requireAuth = true)

    // ── 工单 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun tickets(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/tickets", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    /** 提交工单。分类取自 [ticketCategories]，附件先走 [uploadTicketAttachment]。 */
    @JvmOverloads
    fun createTicket(
        title: String,
        content: String,
        categoryId: Long? = null,
        priority: String? = null,
        contentType: String? = null,
        attachments: List<Any?>? = null,
    ): JsonElement = client.call(
        "POST", "/tickets",
        buildBody(
            "title" to title,
            "content" to content,
            "categoryId" to categoryId,
            "priority" to priority,
            "contentType" to contentType,
            "attachments" to attachments,
        ),
        requireAuth = true,
    )

    @Throws(IOException::class)
    fun ticketCategories(): JsonElement = client.call("GET", "/tickets/categories", requireAuth = true)

    @Throws(IOException::class)
    fun uploadTicketAttachment(file: File): JsonElement = client.upload("/tickets/attachments", file)

    @Throws(IOException::class)
    fun ticket(ticketId: String): JsonElement = client.call("GET", "/tickets/$ticketId", requireAuth = true)

    @Throws(IOException::class)
    fun replyTicket(ticketId: String, content: String): JsonElement =
        client.call("POST", "/tickets/$ticketId/replies", mapOf("content" to content), requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun rateTicket(ticketId: String, score: Int, comment: String? = null): JsonElement = client.call(
        "POST", "/tickets/$ticketId/rating",
        // 服务端字段是 rating（TicketRatingRequest），不是 score。
        buildBody("rating" to score, "comment" to comment), requireAuth = true,
    )

    @Throws(IOException::class)
    @JvmOverloads
    fun cancelTicket(ticketId: String, reason: String? = null): JsonElement =
        client.call("POST", "/tickets/$ticketId/cancel", buildBody("reason" to reason), requireAuth = true)
}

/** 钱包、会员、支付、存储。 */
class AegisCommerceApi internal constructor(private val client: AegisClient) {

    @Throws(IOException::class)
    fun wallet(): JsonElement = client.call("GET", "/wallet", requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun walletTransactions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/wallet/transactions", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    /**
     * 钱包消费。
     *
     * [amount] 是十进制字符串（服务端按定点数解析，用 Double 会丢精度）。
     * [idempotencyKey] 必填且由调用方生成：同一个 key 重复提交只扣一次，
     * 这是"点两下按钮扣两次钱"与不扣两次之间的唯一区别。
     */
    @JvmOverloads
    fun consumeWallet(
        amount: String,
        title: String,
        idempotencyKey: String,
        remark: String? = null,
    ): JsonElement = client.call(
        "POST", "/wallet/consume",
        buildBody(
            "amount" to amount,
            "title" to title,
            "idempotencyKey" to idempotencyKey,
            "remark" to remark,
        ),
        requireAuth = true,
    )

    @Throws(IOException::class)
    fun vipPlans(): JsonElement = client.call("GET", "/vip/plans", requireAuth = true)

    @Throws(IOException::class)
    fun vipStatus(): JsonElement = client.call("GET", "/vip/status", requireAuth = true)

    @Throws(IOException::class)
    fun vipTransactions(): JsonElement = client.call("GET", "/vip/transactions", requireAuth = true)

    @Throws(IOException::class)
    /** 购买会员。[idempotencyKey] 同 [consumeWallet]：重复提交只购买一次。 */
    fun purchaseVip(planId: Long, idempotencyKey: String): JsonElement = client.call(
        "POST", "/vip/purchase",
        mapOf("planId" to planId, "idempotencyKey" to idempotencyKey), requireAuth = true,
    )

    @Throws(IOException::class)
    @JvmOverloads
    fun orders(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/pay/orders", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    /**
     * 创建支付订单。[amount] 是十进制字符串，理由同 [consumeWallet]。
     *
     * 注意 configName / notifyUrl / returnUrl 三项在服务端是 snake_case
     * （`config_name` / `notify_url` / `return_url`）——
     * 这类大小写差异正是让接入方反复吃 40000 的地方，这里替调用方处理掉。
     */
    @JvmOverloads
    fun createOrder(
        subject: String,
        amount: String,
        body: String? = null,
        type: String? = null,
        configName: String? = null,
        notifyUrl: String? = null,
        returnUrl: String? = null,
        metadata: Map<String, Any?>? = null,
    ): JsonElement = client.call(
        "POST", "/pay/orders",
        buildBody(
            "subject" to subject,
            "amount" to amount,
            "body" to body,
            "type" to type,
            "config_name" to configName,
            "notify_url" to notifyUrl,
            "return_url" to returnUrl,
            "metadata" to metadata,
        ),
        requireAuth = true,
    )

    @Throws(IOException::class)
    fun order(orderNo: String): JsonElement = client.call("GET", "/pay/orders/$orderNo", requireAuth = true)

    /** 上传文件到平台存储。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun upload(file: File, fields: Map<String, String> = emptyMap()): JsonElement =
        client.upload("/storage/upload", file, fields = fields)

    @Throws(IOException::class)
    /**
     * 取对象的访问链接。服务端这组字段是 snake_case（`object_key` 等），
     * 与平台其余接口的 camelCase 不同 —— 由这里统一，调用方不必记住这个例外。
     */
    @JvmOverloads
    fun objectLink(
        objectKey: String,
        configName: String? = null,
        download: Boolean? = null,
        fileName: String? = null,
        expiresInSeconds: Int? = null,
    ): JsonElement = client.call(
        "POST", "/storage/object-link",
        buildBody(
            "object_key" to objectKey,
            "config_name" to configName,
            "download" to download,
            "file_name" to fileName,
            "expires_in" to expiresInSeconds,
        ),
        requireAuth = true,
    )
}

/** 免登录内容：轮播图、公告、版本检查。 */
class AegisContentApi internal constructor(private val client: AegisClient) {

    @Throws(IOException::class)
    fun banners(): JsonElement = client.call("GET", "/banners")

    /**
     * 轮播图点击上报。曝光由服务端在下发列表时自己算，点击只有客户端知道，
     * 因此这一条必须由调用方在用户点开 Banner 时显式调用 —— 不调的表现是
     * 控制台上点击率恒为 0，而那个数字是投放决策的唯一依据。
     */
    @Throws(IOException::class)
    fun reportBannerClick(bannerId: Long): JsonElement =
        client.call("POST", "/banners/$bannerId/click")

    @Throws(IOException::class)
    fun notices(): JsonElement = client.call("GET", "/notices")

    /**
     * 版本检查。没有新版本时服务端返回 40430，SDK 把它抛成 BUSINESS 类异常 ——
     * 调用方按「无更新」处理即可，不必当成错误上报。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun checkVersion(versionCode: Int, platform: String = "android"): JsonElement = client.call(
        "GET", "/version/check",
        query = mapOf("versionCode" to versionCode.toString(), "platform" to platform),
    )
}

@kotlinx.serialization.Serializable
internal data class OAuthUrl(val url: String = "")

/** 丢掉 null 值：服务端对「字段缺失」和「字段为 null」的处理不总是一致，不传最稳。 */
private fun buildBody(vararg pairs: Pair<String, Any?>): Map<String, Any?> =
    pairs.filter { it.second != null }.toMap()

private fun buildQuery(vararg pairs: Pair<String, String?>): Map<String, String> =
    pairs.mapNotNull { (key, value) -> value?.let { key to it } }.toMap()

private fun pageQuery(page: Int, limit: Int): Map<String, String> =
    mapOf("page" to page.toString(), "limit" to limit.toString())
