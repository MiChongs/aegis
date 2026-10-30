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
     * 卡密登录（软件授权码）。卡本身就是凭证，首次使用自动建号并与卡绑定。
     *
     * 与另外两种登录方式的区别：**[deviceId] 是必填的**。一张卡能在几台设备上用
     * 由卡决定，没有设备标识就无从判断这次登录算不算超出上限 ——
     * 服务端在缺标识时拒绝（40343）而不是放行，否则那个限制等于不存在。
     *
     * 响应在标准会话字段之外多一个 `authorization`：授权到期时间、可绑设备数、
     * 已绑设备数。客户端应当直接用它渲染「授权剩余 N 天 / 已用 x/y 台」，
     * 不必再调 [AegisCommerceApi.myCardKeys]。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun loginWithCardKey(
        cardKey: String,
        deviceId: String,
        device: String? = null,
    ): AegisSession = login(
        buildBody("method" to "cardkey", "cardKey" to cardKey, "deviceId" to deviceId, "device" to device)
    )

    /**
     * 注册。`profile` 里能放哪些字段由 `/config` 的 `auth.registrationSchema` 决定，
     * **不要在客户端硬编码表单** —— 管理员加一个必填字段就得发版。
     *
     * [inviteCode] 出不出现、是否必填由 `/config` 的 `auth.invite` 决定。填了却不能用时在建号之前
     * 被拒（40366 不可用 / 40367 自己的码 / 40368 次数已满 / 40369 必填却没填）；
     * 建号之后的填写结论在 [AegisSession.invite]。
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
        inviteCode: String? = null,
    ): AegisSession = login(
        buildBody(
            "method" to method, "account" to account, "password" to password,
            "phone" to phone, "code" to code, "nickname" to nickname, "profile" to profile,
            "captchaId" to captchaId, "captchaAnswer" to captchaAnswer, "inviteCode" to inviteCode,
        ),
        path = "/auth/register",
    )

    /**
     * 注册页预览邀请码：能不能用（`valid` / `reason` / `message`）、邀请人（`inviter`）、
     * 填了能拿什么（`rewards`）。不需要登录；码不能用不报错。
     */
    @Throws(IOException::class)
    fun previewInvite(code: String): JsonElement =
        client.call("POST", "/invite/preview", mapOf("code" to code))

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

    /**
     * 登录设备数已满时完成登录：[ticket] 取自 [AegisSession.deviceLimit]，[deviceKeys] 是用户勾选要下线的
     * [LoginDevice.key]，至少 `mustRemove` 台。成功返回与登录相同的会话并写进 tokenStore；
     * 勾得不够时报 40997，凭证过期或已用过报 40496（回登录页重新登录）。
     */
    @Throws(IOException::class)
    fun resolveDeviceLimit(ticket: String, deviceKeys: List<String>): AegisSession = login(
        mapOf("ticket" to ticket, "deviceKeys" to deviceKeys),
        path = "/auth/device-limit/resolve",
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
     * 原生 SDK（QQ 互联、微信等）在端上授权后，用拿到的 [accessToken] 换取 Aegis 会话。
     *
     * 身份由服务端拿令牌向服务商核实（QQ 还会核对令牌签给本应用），端上自报的昵称头像不作数。
     * [openId] 微信、微博必填，QQ 可不填；填了就必须与令牌查出来的一致，否则 40380。
     *
     * [deferRegistration] 为 true 且这个第三方账号还没关联任何账号时，不建号也不签发令牌，
     * 返回的 [AegisSession.oauthLink] 带一张 10 分钟有效的一次性凭证：
     * 用户选「快速注册」就调 [oauthLinkRegister]；选「绑定已有账号」就先用已有账号正常登录
     * （含二次认证），再调 [oauthLinkBind]。为 false 时沿用渠道的自动注册开关直接建号。
     *
     * 这条路径受完整安全等级包装保护；浏览器回跳那条（`/auth/oauth/callback`）
     * 无法被客户端包装，sealed 档下也是明文的。要求全链路加密就走这里。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun oauthExchange(
        provider: String,
        accessToken: String,
        openId: String? = null,
        deferRegistration: Boolean = false,
        deviceId: String? = null,
        device: String? = null,
    ): AegisSession = login(
        buildBody(
            "provider" to provider, "accessToken" to accessToken, "openId" to openId,
            "deferRegistration" to deferRegistration, "deviceId" to deviceId, "device" to device,
        ),
        path = "/auth/oauth/exchange",
    )

    /**
     * 第三方账号未关联时选「快速注册」：凭 [AegisSession.oauthLink] 的 `ticket` 建号并登录。
     * 凭证只能用一次；过期或用过报 40495，要重新走一遍第三方授权。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun oauthLinkRegister(ticket: String, deviceId: String? = null, device: String? = null): AegisSession =
        login(
            buildBody("ticket" to ticket, "deviceId" to deviceId, "device" to device),
            path = "/auth/oauth/link/register",
        )

    /**
     * 第三方账号未关联时选「绑定已有账号」：用户用已有账号登录成功后，凭 `ticket` 把第三方账号挂上来。
     * 返回绑定记录。该第三方账号已绑别人报 40950，当前账号已绑同渠道的另一个报 40953。
     */
    @Throws(IOException::class)
    fun oauthLinkBind(ticket: String): JsonElement =
        client.call("POST", "/auth/oauth/link/bind", mapOf("ticket" to ticket), requireAuth = true)

    /**
     * 已登录用户在设置里绑定：端上 SDK 授权拿到 [accessToken] 后直接绑定，返回绑定记录。
     * [openId] 规则同 [oauthExchange]。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun oauthBindToken(provider: String, accessToken: String, openId: String? = null): JsonElement =
        client.call(
            "POST", "/auth/oauth/bind/token",
            buildBody("provider" to provider, "accessToken" to accessToken, "openId" to openId),
            requireAuth = true,
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

    /**
     * 旧的邮件链接式重置：只寄信，响应里不再带令牌。App 请改用下面的 passwordRecovery 系列 ——
     * 它按账号的绑定情况要求邮箱 / 短信 / 身份验证器，并在完成后吊销其余登录。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun forgotPassword(email: String, resetUrl: String? = null): JsonElement =
        client.call("POST", "/auth/password/forgot", buildBody("email" to email, "resetUrl" to resetUrl))

    @Throws(IOException::class)
    fun verifyResetToken(email: String, token: String): JsonElement =
        client.call("POST", "/auth/password/reset/verify", mapOf("email" to email, "token" to token))

    // ── 找回密码 ──
    //
    // 一次找回是服务端保管的流程：passwordRecoveryStart（已登录时用 AegisMeApi.passwordRecoveryStart）
    // 返回 flowToken 与要完成的验证 `requirements`（每项 `any` 里任选一种通过即可），按要求
    // send / verify，`ready` 为真后 complete。流程绑定在发起它的设备上：SDK 配了 device 时
    // X-Device-Id 头会自动带上，换设备继续会被拒（40121）。

    /**
     * 未登录发起找回。[account] 可以是账号名、绑定的邮箱或手机号。
     *
     * `/config` 的 `auth.captcha.recovery` 为真时先用 `captcha("reset_password")` 取图形验证码。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun passwordRecoveryStart(
        account: String,
        captchaId: String? = null,
        captchaAnswer: String? = null,
    ): JsonElement = client.call(
        "POST", "/auth/password/recovery/start",
        buildBody("account" to account, "captchaId" to captchaId, "captchaAnswer" to captchaAnswer),
    )

    /** 读流程当前状态（页面重建时用）。 */
    @Throws(IOException::class)
    fun passwordRecoveryStatus(flowToken: String): JsonElement =
        client.call("POST", "/auth/password/recovery/status", mapOf("flowToken" to flowToken))

    /**
     * 向 [channel]（`email` | `phone`）发验证码。未登录发起时 [target] 必填：完整的邮箱或手机号，
     * 必须与绑定的一致，对不上算一次失败。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun passwordRecoverySend(flowToken: String, channel: String, target: String? = null): JsonElement =
        client.call(
            "POST", "/auth/password/recovery/send",
            buildBody("flowToken" to flowToken, "channel" to channel, "target" to target),
        )

    /** 核验一项：[factor] 为 `email` | `phone` | `totp` | `recovery`（恢复码，可替代 totp）。 */
    @Throws(IOException::class)
    fun passwordRecoveryVerify(flowToken: String, factor: String, code: String): JsonElement =
        client.call(
            "POST", "/auth/password/recovery/verify",
            mapOf("flowToken" to flowToken, "factor" to factor, "code" to code),
        )

    /**
     * 设新密码。成功后其余登录全部下线（已登录发起时保留当前会话），不签发新令牌：
     * 未登录找回的人用新密码重新登录一次。
     */
    @Throws(IOException::class)
    fun passwordRecoveryComplete(flowToken: String, newPassword: String): JsonElement =
        client.call(
            "POST", "/auth/password/recovery/complete",
            mapOf("flowToken" to flowToken, "newPassword" to newPassword),
        )

    @Throws(IOException::class)
    fun verifyPassword(password: String): JsonElement =
        client.call("POST", "/auth/password/verify", mapOf("password" to password), requireAuth = true)

    @Throws(IOException::class)
    fun changePassword(oldPassword: String, newPassword: String): JsonElement = client.call(
        "POST", "/auth/password/change",
        // 网关读的是 currentPassword；这里曾经发 oldPassword，于是改密码一律被当成「当前密码错误」
        mapOf("currentPassword" to oldPassword, "newPassword" to newPassword),
        requireAuth = true,
    )

    // ── Passkey 登录 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun passkeyOptions(account: String? = null, deviceId: String? = null): JsonElement =
        client.call("POST", "/auth/passkey/options", buildBody("account" to account, "deviceId" to deviceId))

    /**
     * Passkey 登录。[sessionId] 是 [passkeyOptions] 返回的 `session.challengeId`；
     * 服务端两个字段名都收，这里一并带上。
     */
    @Throws(IOException::class)
    fun passkeyLogin(credential: Map<String, Any?>, sessionId: String? = null): AegisSession =
        login(
            buildBody("credential" to credential, "challengeId" to sessionId, "sessionId" to sessionId),
            path = "/auth/passkey/login",
        )

    /**
     * Passkey 登录：[credential] 是平台 API 原样给出的断言 JSON
     * （安卓 Credential Manager 的 `authenticationResponseJson`、浏览器 `PublicKeyCredential.toJSON()`）。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun passkeyLogin(
        credential: JsonElement,
        challengeId: String,
        deviceId: String? = null,
        device: String? = null,
    ): AegisSession =
        login(
            buildBody(
                "credential" to credential,
                "challengeId" to challengeId,
                "sessionId" to challengeId,
                "deviceId" to deviceId,
                "device" to device,
            ),
            path = "/auth/passkey/login",
        )

    // ── 网页扫码登录 ──

    /**
     * 网页（BFF）一侧：申请一张扫码登录票据。
     *
     * 返回 `ticketId`（画进二维码，公开）、`pollToken`（只留在发起端，换取会话时必须出示）、
     * `expiresAt` 与建议的轮询间隔 `interval`。[deviceId] / [device] 描述发起登录的浏览器，
     * 会展示在扫码确认页上，确认后签发的会话也绑定在这台设备上。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun qrLoginCreate(deviceId: String? = null, device: String? = null): JsonElement =
        client.call("POST", "/auth/qr/create", buildBody("deviceId" to deviceId, "device" to device))

    /**
     * 网页（BFF）一侧：轮询票据状态。`status` 为 pending / scanned / confirmed / cancelled / expired；
     * 第一次读到 confirmed 时附带 `session`（与登录结果同形），票据随即作废。
     */
    @Throws(IOException::class)
    fun qrLoginPoll(ticketId: String, pollToken: String): JsonElement =
        client.call("POST", "/auth/qr/poll", mapOf("ticketId" to ticketId, "pollToken" to pollToken))

    /**
     * 应用一侧：扫到网页登录二维码后登记「已扫码」，返回发起端的设备、IP、位置与有效期，
     * 供用户确认。同一用户重复扫码是幂等的；已被他人扫过的票据会被拒绝。
     */
    @Throws(IOException::class)
    fun qrLoginScan(ticketId: String): JsonElement =
        client.call("POST", "/auth/qr/scan", mapOf("ticketId" to ticketId), requireAuth = true)

    /** 应用一侧：确认网页登录。网页端下一次轮询时拿到属于它自己的新会话。 */
    @Throws(IOException::class)
    fun qrLoginConfirm(ticketId: String): JsonElement =
        client.call("POST", "/auth/qr/confirm", mapOf("ticketId" to ticketId), requireAuth = true)

    /** 应用一侧：拒绝本次网页登录，票据作废。 */
    @Throws(IOException::class)
    fun qrLoginCancel(ticketId: String): JsonElement =
        client.call("POST", "/auth/qr/cancel", mapOf("ticketId" to ticketId), requireAuth = true)

    /** 登录类接口的公共收口：解析会话并写进 tokenStore。 */
    private fun login(body: Map<String, Any?>, path: String = "/auth/login"): AegisSession {
        // 账号密码 / 短信 / 卡密登录重复执行只会多出一个会话，网络断在半路时可以自动重发。
        // 其余几条都消耗一次性凭据（刷新令牌、二次认证挑战、OAuth 票据、Passkey 断言），
        // 重发会被拒；刷新令牌更会被判为复用，整条会话链被吊销。它们只在确定没发出去时重发。
        val retrySafe = path == "/auth/login"
        val session = client.decodeAs(client.send("POST", path, body, retrySafe = retrySafe), AegisSession.serializer())
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

    /**
     * 已登录时找回密码（忘了原密码）：发起与 [AegisAuthApi.passwordRecoveryStart] 同一套流程，
     * 当前会话算一项验证，完成后保留当前登录。后续步骤用 AegisAuthApi 的 passwordRecovery 系列。
     */
    @Throws(IOException::class)
    fun passwordRecoveryStart(): JsonElement =
        client.call("POST", "/me/password/recovery", emptyMap<String, Any?>(), requireAuth = true)

    /**
     * 上传头像。sealed 档下整个 multipart 体会被加密，调用方式不变。
     *
     * 可选的 [crop] 是**EXIF 纠正之后**坐标系里的裁剪框（x, y, width, height）。
     * 传原始像素坐标会让所有竖拍照片裁错位置 —— 用户在预览里看到的正是纠正后的图。
     * 不传时服务端居中取最大方形。
     *
     * 返回体里的 `upload.avatar` 是**永久地址**：它不会过期，也不需要在本地
     * 缓存里做失效处理。`upload.view` 里另有 blurhash、主色与可用尺寸档，
     * 可以直接拿去做加载占位。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun uploadAvatar(file: File, crop: IntArray? = null): JsonElement {
        val fields = if (crop != null && crop.size == 4) {
            mapOf(
                "crop_x" to crop[0].toString(),
                "crop_y" to crop[1].toString(),
                "crop_width" to crop[2].toString(),
                "crop_height" to crop[3].toString(),
            )
        } else {
            emptyMap()
        }
        return client.upload("/me/avatar", file, fields = fields)
    }

    /** 移除头像，回到服务端生成的默认头像。 */
    @Throws(IOException::class)
    fun removeAvatar(): JsonElement = client.call("DELETE", "/me/avatar", requireAuth = true)

    /** 头像历史（含当前），用于「换回上一张」。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun avatarHistory(limit: Int = 10): JsonElement =
        client.call("GET", "/me/avatar/history", query = mapOf("limit" to limit.toString()), requireAuth = true)

    /** 把历史里的某一张重新设为当前头像。[assetId] 取自 [avatarHistory] 的 `items[].id`。 */
    @Throws(IOException::class)
    fun restoreAvatar(assetId: Long): JsonElement =
        client.call("POST", "/me/avatar/restore", mapOf("assetId" to assetId), requireAuth = true)

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

    // ── 自动签到 ──

    /**
     * 自动签到状态：应用的开放策略（access）、当前用户的资格（available / reason / message）、
     * 个人设置（settings）、下一次执行时刻（nextRunAt）与最近一次结果（lastRun）。
     */
    @Throws(IOException::class)
    fun autoSignStatus(): JsonElement = client.call("GET", "/me/auto-sign", requireAuth = true)

    /**
     * 修改自动签到设置，只改传了非空值的字段，返回修改后的状态。
     *
     * [time] 为 HH:MM，按状态里的 timezone 解释；[maxRetries] 为 0–10。
     * 从关到开时服务端校验应用的开放策略：未开放返回 40353，不满足开放对象返回 40354。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun updateAutoSign(
        enabled: Boolean? = null,
        time: String? = null,
        retryOnFail: Boolean? = null,
        maxRetries: Int? = null,
        notifyOnSuccess: Boolean? = null,
        notifyOnFail: Boolean? = null,
    ): JsonElement = client.call(
        "PUT", "/me/auto-sign",
        buildBody(
            "enabled" to enabled, "time" to time, "retryOnFail" to retryOnFail,
            "maxRetries" to maxRetries, "notifyOnSuccess" to notifyOnSuccess, "notifyOnFail" to notifyOnFail,
        ),
        requireAuth = true,
    )

    @Throws(IOException::class)
    fun security(): JsonElement = client.call("GET", "/me/security", requireAuth = true)

    // ── 账号初始化 ──
    //
    // 第三方 / 短信 / 卡密快速登录建的号：账号名由系统生成、也没有密码。登录后读 [accountSetupStatus]，
    // prompt = dialog 时弹窗一次并调 [markAccountSetupPrompted]，之后只出横幅（prompt = banner）；
    // 用户关掉横幅调 [dismissAccountSetup]，按应用策略隔一段时间再出现。

    /** 账号初始化状态：还缺用户名 / 密码、现在该怎么提醒、可直接选用的用户名与用户名规则。 */
    @Throws(IOException::class)
    fun accountSetupStatus(): AegisAccountSetupStatus = client.decodeAs(
        client.call("GET", "/me/account-setup", requireAuth = true),
        AegisAccountSetupStatus.serializer(),
    )

    /**
     * 设置用户名与（或）密码，两项可以分开提交，传 null 表示这次不设。
     *
     * 用户名只能自助设一次（之后 40964），被占用 40963；已有密码时设密码 40967，改走 [AegisAuthApi.changePassword]。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun completeAccountSetup(account: String? = null, password: String? = null): AegisAccountSetupResult =
        client.decodeAs(
            client.call(
                "POST", "/me/account-setup",
                buildBody("account" to account, "password" to password), requireAuth = true,
            ),
            AegisAccountSetupResult.serializer(),
        )

    /** 检查用户名能不能用：规则、占用、注销后的保留期。 */
    @Throws(IOException::class)
    fun checkAccountSetupUsername(account: String): AegisUsernameCheck = client.decodeAs(
        client.call("POST", "/me/account-setup/check", mapOf("account" to account), requireAuth = true),
        AegisUsernameCheck.serializer(),
    )

    /** 首次弹窗已展示：之后只出横幅。返回新的状态。 */
    @Throws(IOException::class)
    fun markAccountSetupPrompted(): AegisAccountSetupStatus = client.decodeAs(
        client.call("POST", "/me/account-setup/prompted", emptyMap<String, Any?>(), requireAuth = true),
        AegisAccountSetupStatus.serializer(),
    )

    /** 关掉横幅：按应用策略隔一段时间再出现（策略不许关时状态不变）。返回新的状态。 */
    @Throws(IOException::class)
    fun dismissAccountSetup(): AegisAccountSetupStatus = client.decodeAs(
        client.call("POST", "/me/account-setup/dismiss", emptyMap<String, Any?>(), requireAuth = true),
        AegisAccountSetupStatus.serializer(),
    )

    // ── 账号注销 ──

    /**
     * 注销页：可选方式（modes）、须知（notice）、原因（reasons）、要过的验证（verification）、
     * 前置条件（checks / blocked）与进行中的申请（request）。enabled 为 false 且没有 request 时入口应当隐藏。
     */
    @Throws(IOException::class)
    fun accountDeletion(): JsonElement = client.call("GET", "/me/deletion", requireAuth = true)

    /**
     * 向绑定的邮箱或手机号发注销验证码，[channel] 为 email 或 phone（须在 verification.methods 里）。
     * 同一渠道 60 秒一次（42911），验证码 10 分钟内有效。
     */
    @Throws(IOException::class)
    fun sendAccountDeletionCode(channel: String): JsonElement =
        client.call("POST", "/me/deletion/code", mapOf("channel" to channel), requireAuth = true)

    /**
     * 提交注销。[mode] 为 cooling（冷静期）或 immediate（立即），不传按应用默认；[agree] 必须为 true。
     * 身份验证：[password]，或 [channel] + [code]；verification.totp 为真时再带 [totpCode] 或 [recoveryCode]。
     * 返回 `{ request, deleted, signedOut }`，deleted 为 true 时账号已删除，应当清掉本地登录。
     * 失败码：40371 未开放 / 40915 已有申请 / 40917 不满足条件 / 40127 验证未通过 / 40128 验证码失效 /
     * 40129 缺少验证 / 42912 失败过多。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun submitAccountDeletion(
        agree: Boolean,
        mode: String? = null,
        reasonCode: String? = null,
        reasonText: String? = null,
        password: String? = null,
        channel: String? = null,
        code: String? = null,
        totpCode: String? = null,
        recoveryCode: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/me/deletion",
        buildBody(
            "agree" to agree, "mode" to mode, "reasonCode" to reasonCode, "reasonText" to reasonText,
            "password" to password, "channel" to channel, "code" to code, "totpCode" to totpCode,
            "recoveryCode" to recoveryCode, "deviceId" to deviceId,
        ),
        requireAuth = true,
    )

    /** 撤销进行中的注销申请（待审核、冷静期中、执行失败），返回撤销后的申请。 */
    @Throws(IOException::class)
    fun cancelAccountDeletion(): JsonElement = client.call("POST", "/me/deletion/cancel", requireAuth = true)

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

    /**
     * 完成 Passkey 注册：[credential] 是平台 API 原样给出的注册响应 JSON
     * （安卓 Credential Manager 的 `registrationResponseJson`）。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun registerPasskey(
        challengeId: String,
        credential: JsonElement,
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
        client.call("DELETE", "/me/passkeys/${encodePathSegment(credentialId)}", requireAuth = true)

    // ── 会话与审计 ──

    @Throws(IOException::class)
    fun sessions(): JsonElement = client.call("GET", "/me/sessions", requireAuth = true)

    @Throws(IOException::class)
    fun revokeSession(tokenHash: String): JsonElement =
        client.call("DELETE", "/me/sessions/$tokenHash", requireAuth = true)

    @Throws(IOException::class)
    fun revokeAllSessions(): JsonElement = client.call("POST", "/me/sessions/revoke-all", requireAuth = true)

    // ── 登录设备 ──

    /**
     * 我的登录设备：同一台设备上的多次登录合成一台，本机排在最前（[LoginDevice.current]），
     * 外加此刻生效的设备数上限与各终端类别的占用。
     */
    @Throws(IOException::class)
    fun devices(): LoginDeviceStatus =
        client.decodeAs(client.call("GET", "/me/devices", requireAuth = true), LoginDeviceStatus.serializer())

    /** 下线我的其他设备（[LoginDevice.key]），返回下线后的状况。本机不能在这里下线，请用 [AegisAuthApi.logout]。 */
    @Throws(IOException::class)
    fun removeDevices(deviceKeys: List<String>): LoginDeviceStatus = client.decodeAs(
        client.call("POST", "/me/devices/remove", mapOf("deviceKeys" to deviceKeys), requireAuth = true),
        LoginDeviceStatus.serializer(),
    )

    @Throws(IOException::class)
    @JvmOverloads
    fun loginAudits(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/me/audits/login", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun sessionAudits(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/me/audits/sessions", query = pageQuery(page, limit), requireAuth = true)

    // ── 已授权的第三方应用（OAuth2 授权服务器）──

    /** 当前用户授权过的第三方应用（经 Aegis 的 OAuth2 / OIDC 授权登录过的客户端）。 */
    @Throws(IOException::class)
    fun oauth2Grants(): JsonElement = client.call("GET", "/me/oauth2/grants", requireAuth = true)

    /** 取消对某个第三方应用的授权，并吊销它持有的访问令牌与刷新令牌。 */
    @Throws(IOException::class)
    fun revokeOAuth2Grant(clientId: String): JsonElement {
        val id = java.net.URLEncoder.encode(clientId, "UTF-8").replace("+", "%20")
        return client.call("DELETE", "/me/oauth2/grants/$id", requireAuth = true)
    }
}

/** 签到、积分、排行榜、站内信、工单、意见反馈。 */
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

    /**
     * 我的等级达成奖励（升级经过的每一级各一份）。[status] 可选 `claimable` 待领取 / `granted` 已到账 /
     * `queued` 发放中 / `pending` 人工处理中 / `failed`；为空时全部（待领取在前）。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun levelRewards(status: String? = null): JsonElement =
        client.call("GET", "/points/level/rewards", query = status?.let { mapOf("status" to it) } ?: emptyMap(), requireAuth = true)

    /** 领取一份待领取（`claimable`）的达成奖励。已领取过为 40900。 */
    @Throws(IOException::class)
    fun claimLevelReward(rewardId: Long): JsonElement =
        client.call("POST", "/points/level/rewards/$rewardId/claim", requireAuth = true)

    /** 我的升级记录。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun levelHistory(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/points/level/history", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun integralTransactions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/points/integral-transactions", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    @JvmOverloads
    fun experienceTransactions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/points/experience-transactions", query = pageQuery(page, limit), requireAuth = true)

    /**
     * 积分商城首页：余额、等级、商品，以及每件商品对当前用户的可兑换判定（`availability`）。
     *
     * 未开启时 `enabled=false`、`items` 为空（不报错），入口应当整个隐藏。
     */
    @Throws(IOException::class)
    fun pointsStore(): JsonElement = client.call("GET", "/points/store", requireAuth = true)

    /**
     * 用积分兑换一件商品。
     *
     * [requestId] 由调用方生成（建议 UUID），网络重试时带同一个值：服务端原样返回第一次的结果
     * （`replayed=true`），不会重复扣积分。失败码与 `availability.reason` 一一对应：
     * 40303 未开放 / 40304 暂不可兑换 / 40305 已兑完 / 40306 次数已满 / 40307 积分不足 /
     * 40308 不满足条件 / 40309 已拥有 / 40443 商品不存在。
     * 含人工发放的商品兑换后 `redemption.status` 为 `pending`，处理结果以站内信通知。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun redeemPointsStoreItem(itemId: Long, requestId: String? = null, deviceId: String? = null): JsonElement =
        client.call(
            "POST", "/points/store/redeem",
            mapOf("itemId" to itemId, "requestId" to requestId, "deviceId" to deviceId), requireAuth = true,
        )

    /** 我的积分兑换记录。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun pointsStoreRedemptions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/points/store/redemptions", query = pageQuery(page, limit), requireAuth = true)

    // ── 抽奖（docs/lottery.md）──

    /** 进行中的抽奖活动。没有活动时 `activities` 为空（不报错），入口应当整个隐藏。 */
    @Throws(IOException::class)
    fun lotteryActivities(): JsonElement = client.call("GET", "/lottery", requireAuth = true)

    /**
     * 抽奖活动页：活动与奖品（`probability` 为公示概率）、我的各种次数（`chances`）与保底进度（`pity`）。
     *
     * `chances.next` 是「点一下抽奖」会用的次数来源，为空表示没有可用次数。
     * 视频抽奖看 `chances.video`：用其中的 `scene` / `placementId` / `userId` 按激励广告的流程看完并上报
     * （[AegisEngagementApi.claimRewardedAd]），到账的是抽奖次数，再用 `bonus` 抽。
     */
    @Throws(IOException::class)
    fun lotteryState(activityId: Long): JsonElement =
        client.call("GET", "/lottery/activities/$activityId", requireAuth = true)

    /**
     * 抽一次。抽中什么由服务端决定并在同一个事务里到账，返回 `draw`（含 `position`，奖盘上的格子）、
     * 抽完后的 `chances` 与 `pity`。
     *
     * [source] 为 `free` / `bonus` / `points`，是「最多用到这一档」：为空时不设上限，两种情况都按
     * 今日免费 → 赠送的次数 → 兑换的次数 → 积分 取第一个可用的，免费次数永远先扣。实际用的在 `draw.source` / `draw.ticket`。
     * [requestId] 由调用方生成（建议 UUID），网络重试时带同一个值：服务端原样返回第一次的结果
     * （`replayed = true`），不会多抽一次。失败码：40402 活动不存在 / 40923 未开始 / 40924 已结束 /
     * 42913 免费次数用完 / 42914 积分抽奖今日已满 / 42915 没有可用次数 / 40307 积分不足 /
     * 40928 该方式未开放 / 40929 奖品已失效（已回滚） / 40348 风控拦截。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun drawLottery(
        activityId: Long,
        source: String? = null,
        requestId: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/lottery/activities/$activityId/draw",
        mapOf("source" to source, "requestId" to requestId, "deviceId" to deviceId), requireAuth = true,
    )

    /**
     * 连抽（十连抽）：[count] 为 1–10。逐次抽、逐次到账（保底、每日运气在十次之间照常推进），返回 `draws`、
     * 抽完后的 `chances` 与 `pity`。中途次数用完时停下，已抽到的照常返回，`stopped`（`code` / `message`）写明原因；
     * 第一次就抽不了时与 [drawLottery] 报同一个错误码。
     *
     * [requestId] 不超过 61 位：第 i 次用 `requestId#i`。网络重试带同一个值时已抽过的原样返回，没抽到的接着抽。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun drawLotteryBatch(
        activityId: Long,
        count: Int,
        source: String? = null,
        requestId: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/lottery/activities/$activityId/draw/batch",
        mapOf("count" to count, "source" to source, "requestId" to requestId, "deviceId" to deviceId), requireAuth = true,
    )

    /** 我的抽奖记录，新的在前；[activityId] 为空时不限活动。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun lotteryDraws(activityId: Long? = null, page: Int = 1, limit: Int = 20): JsonElement {
        val query = pageQuery(page, limit).toMutableMap()
        if (activityId != null) query["activityId"] = activityId.toString()
        return client.call("GET", "/lottery/draws", query = query, requireAuth = true)
    }

    /**
     * 我的抽奖次数：`account`（余额 `balance`、`tickets.gift` 赠送的 / `tickets.purchased` 兑换的、累计、
     * 还有剩余的批次 `lots`，排在前面的先用）与次数流水 `ledger`（分页，新的在前）。
     *
     * 扣除顺序只有一条：今日免费 → 赠送的次数 → 兑换的次数 → 积分（`account.order`）。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun lotteryTickets(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/lottery/tickets", query = pageQuery(page, limit), requireAuth = true)

    /**
     * 邀请页：我的邀请码（`code`，不满足邀请条件时为空、原因在 `inviteMessage`）、分享文案与链接、
     * 双方奖励与阶梯（`milestones`）、统计（`stats`），以及我还能不能填写邀请码（`bind`）。
     *
     * 未开启时 `enabled=false`（不报错），入口应当整个隐藏。
     */
    @Throws(IOException::class)
    fun invite(): JsonElement = client.call("GET", "/invite", requireAuth = true)

    /** 我邀请的人：昵称、头像、邀请人奖励的状态（`pending` / `granted` / `limited` / `cancelled`）与结果。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun invitees(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/invite/invitees", query = pageQuery(page, limit), requireAuth = true)

    /**
     * 注册后在时限内补填邀请码（时限见 `/invite` 的 `bind.deadline`）。
     * 失败码：40363 未开放 / 40364 已过期限 / 40365 已填写过 / 40366 邀请码不可用 /
     * 40367 自己的或下级的码 / 40368 设备或网络次数已满 / 40355 奖励配置已失效。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun bindInvite(code: String, deviceId: String? = null): JsonElement =
        client.call("POST", "/invite/bind", mapOf("code" to code, "deviceId" to deviceId), requireAuth = true)

    /**
     * 换我的邀请码：[code] 非空为自定义（应用允许时，否则 40356；被占用 40948），
     * 为空则按应用的格式重新生成。旧码立即失效，已经填写过旧码的人不受影响。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun updateInviteCode(code: String? = null): JsonElement =
        client.call("PUT", "/invite/code", mapOf("code" to code), requireAuth = true)

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

    // ── 积分排行榜（docs/leaderboard.md）──

    /** 排行榜首页：开启的榜单（周期、奖励档位）与我的偏好（匿名、是否被移出）。 */
    @Throws(IOException::class)
    fun leaderboardBoards(): JsonElement = client.call("GET", "/leaderboard/boards", requireAuth = true)

    /** 一个榜单的实时名次。key: `points_total` | `points_daily` | `points_weekly` | `points_monthly` | `experience` | `level` */
    @Throws(IOException::class)
    @JvmOverloads
    fun leaderboardBoard(key: String, page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/leaderboard/boards/${encodePathSegment(key)}", query = pageQuery(page, limit), requireAuth = true)

    /** 周期榜的往期列表（已结算的快照），新的在前。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun leaderboardPeriods(key: String, limit: Int = 12): JsonElement =
        client.call(
            "GET", "/leaderboard/boards/${encodePathSegment(key)}/periods",
            query = mapOf("limit" to limit.toString()), requireAuth = true,
        )

    /** 往期榜单：某一期结算时冻结的名次、我在这一期的名次与奖励。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun leaderboardPeriod(key: String, periodKey: String, page: Int = 1, limit: Int = 20): JsonElement =
        client.call(
            "GET", "/leaderboard/boards/${encodePathSegment(key)}/periods/${encodePathSegment(periodKey)}",
            query = pageQuery(page, limit), requireAuth = true,
        )

    /** 我的榜单奖励记录。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun leaderboardRewards(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/leaderboard/rewards", query = pageQuery(page, limit), requireAuth = true)

    /** 我的排行榜偏好：`anonymous` 匿名上榜。 */
    @Throws(IOException::class)
    fun leaderboardPreference(): JsonElement = client.call("GET", "/leaderboard/preference", requireAuth = true)

    /** 开关匿名上榜：名次与奖励照旧，别人看到的是「匿名用户」。 */
    @Throws(IOException::class)
    fun updateLeaderboardPreference(anonymous: Boolean): JsonElement =
        client.call("PUT", "/leaderboard/preference", mapOf("anonymous" to anonymous), requireAuth = true)

    // ── 激励广告 ──

    /**
     * 激励广告状态：各奖励场景、今日剩余次数与冷却，以及传给广告 SDK 的 `userId`。
     *
     * 加载广告前先调这个：`enabled = false` 时整个入口应当隐藏；某个场景 `available = false`
     * 时不要让用户去看 —— 看完也领不到。`userId` 必须原样传给广告 SDK 的请求对象，
     * 平台回调靠它认出是谁看的，客户端不要自己拼。
     */
    @Throws(IOException::class)
    fun rewardedAdStatus(): JsonElement = client.call("GET", "/ads/rewarded", requireAuth = true)

    /**
     * 上报一次看完的激励广告并领取奖励。
     *
     * [transId] 是 SDK 在 `onVideoRewarded` 里给的那一个。返回体的 `status`：
     * `granted` 已到账；`rejected` 看了但不发（`reason` 说明原因，如 `daily_limit`）；
     * `pending` 还在等广告平台的服务端回调 —— 隔一两秒用同一个 [transId] 再调一次即可，
     * 重复上报是安全的，奖励只会发一次。
     *
     * [verified] / [errorCode] 是 SDK 自己的奖励校验结论（Apex `AdRewardVerify`），
     * 只在服务端配置为客户端校验模式时作数。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun claimRewardedAd(
        scene: String,
        transId: String,
        placementId: String? = null,
        verified: Boolean? = null,
        errorCode: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/ads/rewarded/claim",
        mapOf(
            "scene" to scene,
            "transId" to transId,
            "placementId" to placementId,
            "verified" to verified,
            "errorCode" to errorCode,
            "deviceId" to deviceId,
        ),
        requireAuth = true,
    )

    /** 我的激励广告观看与领奖记录。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun rewardedAdRecords(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/ads/rewarded/records", query = pageQuery(page, limit), requireAuth = true)

    // ── 功能时长 ──

    /**
     * 当前用户在一个功能上的时长：余额、累计获得与用掉的秒数，以及当前是否免扣。
     *
     * 功能时长由激励广告的「功能时长」奖励发放（[AegisRewardedAdReward.FEATURE_MINUTES]），
     * 按实际使用扣；会员或已拥有该功能的用户 `exempt = true`，使用时不扣。
     */
    @Throws(IOException::class)
    fun featureTime(feature: String): AegisFeatureTime {
        val tag = encodePathSegment(feature)
        return client.decodeAs(
            client.call("GET", "/features/time/$tag", requireAuth = true),
            AegisFeatureTime.serializer(),
        )
    }

    /**
     * 开始使用前租一段时长（[seconds] 为 1–600，超过按 600 截断，余额不足时按余额给）。
     *
     * 这段时长立刻从余额里扣走，用完再租下一段；停下时用 [settleFeatureTimeLease] 结算，
     * 没用完的退回。免扣时返回 `exempt = true`、不扣余额也没有租约号。
     * 余额为 0 时抛 [AegisException]，code 为 [AegisFeatureTimeCodes.FEATURE_TIME_EXHAUSTED]。
     */
    @Throws(IOException::class)
    fun leaseFeatureTime(feature: String, seconds: Int): AegisFeatureTimeLease {
        val tag = encodePathSegment(feature)
        return client.decodeAs(
            client.call("POST", "/features/time/$tag/leases", mapOf("seconds" to seconds), requireAuth = true),
            AegisFeatureTimeLease.serializer(),
        )
    }

    /**
     * 停止使用时结算租约，[usedSeconds] 为这段租约实际用了多少秒，没用完的退回余额。
     *
     * 重复结算是安全的，返回第一次的结论；租约过期（租出两小时后）再结算 `status = expired`、不退款。
     * 租约不存在或不属于当前用户时抛 [AegisException]，code 为
     * [AegisFeatureTimeCodes.FEATURE_TIME_LEASE_NOT_FOUND]。
     */
    @Throws(IOException::class)
    fun settleFeatureTimeLease(leaseNo: String, usedSeconds: Int): AegisFeatureTimeSettlement {
        val no = encodePathSegment(leaseNo)
        return client.decodeAs(
            client.call(
                "POST", "/features/time/leases/$no/settle",
                mapOf("usedSeconds" to usedSeconds), requireAuth = true,
            ),
            AegisFeatureTimeSettlement.serializer(),
        )
    }

    // ── 广告服务策略 ──

    /**
     * 广告服务策略与当前账号的处境。免登录可用，带令牌时按账号回答。
     *
     * `mode = basic` 时只有 `basicTools` 里的功能可用；`decisionRequired` 时应请用户（重新）选择。
     * `configured = false` 表示应用没配过策略，客户端按自己内置的默认策略处理。
     * 未登录时选择一律按「还没选」回答，客户端以本机的选择为准。
     * `adUserId` 原样传给广告 SDK 的请求对象（开屏与激励视频是同一个）。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun adPolicy(deviceId: String? = null): JsonElement = client.call(
        "GET", "/ads/policy",
        query = buildQuery("deviceId" to deviceId),
        optionalAuth = true,
    )

    /**
     * 记录对广告服务的选择，返回记录后的策略视图（与 [adPolicy] 同构）。
     *
     * [version] 传用户看到的 `consentVersion`，不传按当前版本。[ifAbsent] 用于把未登录时
     * 在本机做的选择同步到账号上：账号上已经有选择时不覆盖，原样返回已有的那一条。
     * [source]：`app`（默认）/ `web` / `guest_sync`。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun saveAdConsent(
        accepted: Boolean,
        version: Int? = null,
        ifAbsent: Boolean = false,
        source: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/ads/consent",
        buildBody(
            "accepted" to accepted,
            "version" to version,
            "ifAbsent" to ifAbsent,
            "source" to source,
            "deviceId" to deviceId,
        ),
        requireAuth = true,
    )

    /**
     * 上报一批开屏记录（每次最多 50 条）。带令牌时记到账号上，未登录时只有设备标识。
     *
     * 每条记录的字段：`eventId`（客户端生成，8–64 位字母数字 `_` `-`，重传不会重复记录）、
     * `status`（`shown` / `clicked` / `failed` / `timeout`）、`placementId`、`errorCode`、
     * `errorMessage`、`loadMs`、`shownMs`、`occurredAt`（RFC 3339）、`anonymous`
     * （这次开屏发生时没有登录或登录的是另一个账号：不挂到上报时的账号上）。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun reportSplashAdEvents(events: List<Map<String, Any?>>, deviceId: String? = null): JsonElement =
        client.call(
            "POST", "/ads/splash/events",
            buildBody("events" to events, "deviceId" to deviceId),
            optionalAuth = true,
        )

    // ── 崩溃报告 ──

    /**
     * 上报一份崩溃报告。免登录；带令牌时挂到账号上。只在用户明确同意发送这一份报告后调用
     * （例如在崩溃屏上点了「发送此报告」）。
     *
     * [reportId] 是客户端自己的记录编号（8–64 位字母数字 `_` `-`），同一应用内重复上报返回
     * 原来那一条，`duplicate = true`。[kind] 取 `java` / `native` / `anr` / `exit`。[report] 是
     * 完整的纯文本报告，上限 256 KB；[occurredAt] 是崩溃发生的时间（RFC 3339）。
     *
     * 返回 `{ id, duplicate }`。按设备与 IP 限频，超出时报 429。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun submitCrashReport(
        reportId: String,
        kind: String,
        title: String,
        report: String,
        occurredAt: String,
        detail: String? = null,
        appVersion: String? = null,
        versionCode: Long? = null,
        osVersion: String? = null,
        model: String? = null,
        abi: String? = null,
        processName: String? = null,
        source: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/crash-reports",
        buildBody(
            "reportId" to reportId,
            "kind" to kind,
            "title" to title,
            "report" to report,
            "occurredAt" to occurredAt,
            "detail" to detail,
            "appVersion" to appVersion,
            "versionCode" to versionCode,
            "osVersion" to osVersion,
            "model" to model,
            "abi" to abi,
            "processName" to processName,
            "source" to source,
            "deviceId" to deviceId,
        ),
        optionalAuth = true,
    )

    // ── 站内信 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun notifications(page: Int = 1, limit: Int = 20, status: String? = null, category: String? = null): JsonElement =
        client.call(
            "GET", "/notifications",
            query = pageQuery(page, limit) + listOfNotNull(
                status?.let { "status" to it },
                category?.let { "category" to it },
            ),
            requireAuth = true,
        )

    /** 消息中心首页：总未读与每个分类的未读、总数、最新一条（docs/notification-center.md）。 */
    @Throws(IOException::class)
    fun notificationSummary(): JsonElement = client.call("GET", "/notifications/summary", requireAuth = true)

    /** 单条消息详情。 */
    @Throws(IOException::class)
    fun notification(notificationId: Long): JsonElement =
        client.call("GET", "/notifications/$notificationId", requireAuth = true)

    @Throws(IOException::class)
    fun unreadCount(): JsonElement = client.call("GET", "/notifications/unread-count", requireAuth = true)

    @Throws(IOException::class)
    fun readNotification(notificationId: Long): JsonElement =
        client.call("POST", "/notifications/read", mapOf("notificationId" to notificationId), requireAuth = true)

    @Throws(IOException::class)
    fun readNotifications(ids: List<Long>): JsonElement =
        // 服务端字段是 ids（NotificationReadBatchRequest）。
        client.call("POST", "/notifications/read-batch", mapOf("ids" to ids), requireAuth = true)

    /** 全部标为已读；给了 [category] 只处理这一类。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun readAllNotifications(category: String? = null): JsonElement =
        client.call("POST", "/notifications/read-all", buildBody("category" to category), requireAuth = true)

    /** 标为未读。 */
    @Throws(IOException::class)
    fun unreadNotification(notificationId: Long): JsonElement =
        client.call("POST", "/notifications/unread", mapOf("notificationId" to notificationId), requireAuth = true)

    @Throws(IOException::class)
    fun deleteNotification(notificationId: Long): JsonElement =
        client.call("DELETE", "/notifications/$notificationId", requireAuth = true)

    /** 批量删除（≤ 200 条）。 */
    @Throws(IOException::class)
    fun deleteNotifications(ids: List<Long>): JsonElement =
        client.call("POST", "/notifications/delete-batch", mapOf("ids" to ids), requireAuth = true)

    /** 清空。status：`all` | `read`；给了 [category] 只清这一类。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun clearNotifications(status: String? = null, category: String? = null): JsonElement =
        client.call("POST", "/notifications/clear", buildBody("status" to status, "category" to category), requireAuth = true)

    /** 各分类的提醒开关。 */
    @Throws(IOException::class)
    fun notificationPreferences(): JsonElement =
        client.call("GET", "/notifications/preferences", requireAuth = true)

    /** 改提醒开关：分类 key → 是否提醒。账号安全类不能关闭。 */
    @Throws(IOException::class)
    fun updateNotificationPreferences(categories: Map<String, Boolean>): JsonElement =
        client.call("PUT", "/notifications/preferences", mapOf("categories" to categories), requireAuth = true)

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
        attachmentIds: List<Long>? = null,
    ): JsonElement = client.call(
        "POST", "/tickets",
        buildBody(
            "title" to title,
            "content" to content,
            "categoryId" to categoryId,
            "priority" to priority,
            "contentType" to contentType,
            // 服务端字段是 attachmentIds（[uploadTicketAttachment] 返回的 id），曾误写成 attachments，附件被静默丢弃。
            "attachmentIds" to attachmentIds,
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

    // ── 意见反馈 ──
    // 反馈是独立入口的工单：图片与附件分开上传、分开引用，频率受限（每小时 5 条、每天 20 条，超出 42960）。

    /** 反馈分类：`[{id, key, name, description}]`。 */
    @Throws(IOException::class)
    fun feedbackCategories(): JsonElement = client.call("GET", "/feedback/categories", requireAuth = true)

    /**
     * 上传反馈图片或附件。[kind] 为 `image` 时按文件内容判定，只收 PNG / JPEG / WebP / GIF 且不超过 10MB；
     * `file` 不限类型，不超过 20MB。返回的 `id` 分别放进 [createFeedback] 的 imageIds / attachmentIds。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun uploadFeedbackAttachment(file: File, kind: String = "file"): JsonElement =
        client.upload("/feedback/attachments", file, fields = mapOf("kind" to kind))

    /**
     * 提交反馈。[imageIds] 至多 4 个、[attachmentIds] 至多 2 个，都必须是本人刚上传、尚未使用的。
     * [platform] / [version] / [device] 记为客户端信息（各至多 128 字），platform 为 `web` 时来源记为官网。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun createFeedback(
        categoryId: Long,
        title: String,
        content: String,
        contact: String? = null,
        imageIds: List<Long>? = null,
        attachmentIds: List<Long>? = null,
        platform: String? = null,
        version: String? = null,
        device: String? = null,
    ): JsonElement {
        val clientInfo = buildBody("platform" to platform, "version" to version, "device" to device)
        return client.call(
            "POST", "/feedback",
            buildBody(
                "categoryId" to categoryId,
                "title" to title,
                "content" to content,
                "contact" to contact,
                "imageIds" to imageIds,
                "attachmentIds" to attachmentIds,
                "client" to clientInfo.ifEmpty { null },
            ),
            requireAuth = true,
        )
    }

    /** 我的反馈。[status] 逗号分隔，可空；[limit] 至多 50。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun feedbackList(page: Int = 1, limit: Int = 20, status: String? = null): JsonElement =
        client.call("GET", "/feedback", query = pageQuery(page, limit) + buildQuery("status" to status), requireAuth = true)

    @Throws(IOException::class)
    fun feedback(feedbackId: String): JsonElement = client.call("GET", "/feedback/$feedbackId", requireAuth = true)

    /** 补充反馈，返回最新的反馈详情。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun replyFeedback(
        feedbackId: String,
        content: String,
        imageIds: List<Long>? = null,
        attachmentIds: List<Long>? = null,
    ): JsonElement = client.call(
        "POST", "/feedback/$feedbackId/replies",
        buildBody("content" to content, "imageIds" to imageIds, "attachmentIds" to attachmentIds),
        requireAuth = true,
    )

    /** 评价处理结果（1–5 星），只能在已解决或已关闭后评价一次。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun rateFeedback(feedbackId: String, rating: Int, comment: String? = null): JsonElement = client.call(
        "POST", "/feedback/$feedbackId/rating",
        buildBody("rating" to rating, "comment" to comment), requireAuth = true,
    )

    @Throws(IOException::class)
    @JvmOverloads
    fun cancelFeedback(feedbackId: String, reason: String? = null): JsonElement =
        client.call("POST", "/feedback/$feedbackId/cancel", buildBody("reason" to reason), requireAuth = true)
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
    fun vipTransactions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/vip/transactions", query = pageQuery(page, limit), requireAuth = true)

    /**
     * 会员折算记录：已拥有的权益被发放时折成的积分，开通永久会员时折掉的限时会员、功能解锁、功能时长与免广告时长。
     * `status = restored` 表示永久会员被取消后已还回。
     */
    @Throws(IOException::class)
    fun vipConversions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/vip/conversions", query = pageQuery(page, limit), requireAuth = true)

    /** 启用中的会员权益目录：标识、名称与说明。[vipStatus] 与套餐里只有标识，名称从这里取。 */
    @Throws(IOException::class)
    fun vipFeatures(): JsonElement = client.call("GET", "/vip/features", requireAuth = true)

    @Throws(IOException::class)
    /** 购买会员。[idempotencyKey] 同 [consumeWallet]：重复提交只购买一次。 */
    fun purchaseVip(planId: Long, idempotencyKey: String): JsonElement = client.call(
        "POST", "/vip/purchase",
        mapOf("planId" to planId, "idempotencyKey" to idempotencyKey), requireAuth = true,
    )

    @Throws(IOException::class)
    /**
     * 领取试用会员。
     *
     * 没有参数也没有幂等键：试用一人一次，领哪个套餐由服务端的试用配置决定 ——
     * 让客户端传 planId 就等于把"领哪个"交给了客户端。
     *
     * 领之前先看 [vipStatus] 里的 `trialOffer`：`available` 决定入口显不显示，
     * 不可领时 `reason` 说明原因（already_claimed / member_active /
     * device_claimed / device_required / not_configured）。
     * 直接调用而资格不足时返回 403 与对应业务码（40373 / 40374 / 40375）。
     *
     * 重复调用是安全的：仍在试用期内会原样返回上一次的结果（`replayed = true`）。
     */
    fun claimVipTrial(): JsonElement = client.call("POST", "/vip/trial", requireAuth = true)

    @Throws(IOException::class)
    /**
     * 兑换前预览：这张卡会发什么、现在能不能用。
     *
     * 不能用时仍然返回成功，`usable = false` 并带上 `reasonCode` / `reason`（与兑换时的错误码一致），
     * 让界面在用户按下「兑换」之前就能说清楚为什么不行。卡密不存在照常以 40441 报错，
     * 并和兑换一样计入输错次数（连续输错会被 42916 锁定 15 分钟）。
     */
    fun previewCardKey(code: String): JsonElement = client.call(
        "POST", "/card-keys/preview", mapOf("code" to code), requireAuth = true,
    )

    @Throws(IOException::class)
    /**
     * 兑换卡密。
     *
     * 卡面大小写与分隔符由服务端归一化（抄卡密的人会用小写、把 `-` 打成空格），
     * 客户端原样上送即可，不要自己"清洗"一遍 —— 两边各洗一次反而会洗出分歧。
     *
     * 失败一律是 4xx 且业务码区分得很细：40340 已作废 / 40341 已使用 / 40342 已过期 /
     * 40343 设备数已满 / 40344 已绑定他人 / 40345 类型不符 / 40346 名下没有授权卡 /
     * 40441 卡密不存在 / 40975 未到生效时间 / 40976 已兑换过（活动码每人一次）/
     * 40977 每人限兑已满 / 40978 不在领取对象里 / 40981 授权卡已是永久 / 40983 已永久拥有该功能 /
     * 40984 权益已下架 / 42916 输错次数过多。把它们合并成一句"兑换失败"会让用户无从判断下一步该做什么。
     *
     * 成功时返回 `results`：每一项权益实际发成了什么（如「30 天」「1000 分（余额 2300）」）。
     */
    @JvmOverloads
    fun redeemCardKey(code: String, deviceId: String? = null): JsonElement = client.call(
        "POST", "/card-keys/redeem",
        mapOf("code" to code, "deviceId" to deviceId), requireAuth = true,
    )

    @Throws(IOException::class)
    /**
     * 我名下的授权卡：授权到期时间、已绑定的设备（`current` 标出本机）与剩余的自助解绑次数。
     *
     * 用卡密登录（`login(method = "cardkey")`）时，到期时间与设备数已经随登录结果一起下发了；
     * 这个接口是给「登录之后过了一段时间再看一眼」与设备管理用的。
     */
    @JvmOverloads
    fun myCardKeys(deviceId: String? = null): JsonElement = client.call(
        "GET", "/card-keys/mine",
        query = if (deviceId.isNullOrBlank()) emptyMap() else mapOf("deviceId" to deviceId),
        requireAuth = true,
    )

    @Throws(IOException::class)
    /**
     * 自助解绑授权卡上的一台设备，把名额还回来（换手机、重装系统之后用）。
     *
     * 每 30 天能解绑几次由批次决定（`selfUnbindLimit`）：次数用完返回 40979，
     * 不允许自助解绑返回 40995，设备不在这张卡上返回 40458。
     */
    fun unbindMyCardKeyDevice(cardId: Long, deviceId: String): JsonElement = client.call(
        "DELETE", "/card-keys/mine/$cardId/devices/${encodePathSegment(deviceId)}", requireAuth = true,
    )

    @Throws(IOException::class)
    /** 我的卡密兑换记录（新的在前）。 */
    @JvmOverloads
    fun cardKeyRedemptions(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/card-keys/redemptions", query = pageQuery(page, limit), requireAuth = true)

    @Throws(IOException::class)
    /**
     * 可用的付款方式。
     *
     * 应用在后台配置的支付渠道按子类型展开：一条易支付配置放行了支付宝、微信、QQ 钱包，
     * 这里就是三项。每项的 `method` / `configName` / `type` 原样传给 [createOrder] 的
     * `paymentMethod` / `configName` / `type`；`isDefault` 是不指定时服务端会用的那一项，
     * `key` 可以拿来记住用户上一次的选择。
     *
     * [purpose] 传订单用途：`wallet_recharge` 时不列余额支付（充值不能用余额付）。
     */
    @JvmOverloads
    fun paymentMethods(purpose: String? = null): JsonElement = client.call(
        "GET", "/pay/methods",
        query = purpose?.takeIf { it.isNotBlank() }?.let { mapOf("purpose" to it) } ?: emptyMap(),
        requireAuth = true,
    )

    @Throws(IOException::class)
    /**
     * 我的订单，新的在前。
     *
     * [status] 只看某一种状态的订单：`pending` 待支付 / `paid` 已支付 / `expired` 已过期 /
     * `failed` 支付失败；不传就是全部。`total` 是这个筛选条件下的总数。
     */
    @JvmOverloads
    fun orders(page: Int = 1, limit: Int = 20, status: String? = null): JsonElement = client.call(
        "GET", "/pay/orders",
        query = pageQuery(page, limit) + (status?.takeIf { it.isNotBlank() }?.let { mapOf("status" to it) } ?: emptyMap()),
        requireAuth = true,
    )

    @Throws(IOException::class)
    /**
     * 创建支付订单。[amount] 是十进制字符串，理由同 [consumeWallet]。
     *
     * 注意 paymentMethod / configName / notifyUrl / returnUrl 在服务端是 snake_case
     * （`payment_method` / `config_name` / `notify_url` / `return_url`）——
     * 这类大小写差异正是让接入方反复吃 40000 的地方，这里替调用方处理掉。
     *
     * 让用户选付款方式时，[type]、[configName]、[paymentMethod] 取自 [paymentMethods] 的同一项。
     * 三者都不传就用应用的默认配置与默认类型。[type] 不在那条配置放行的范围内时返回 40116。
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
        paymentMethod: String? = null,
    ): JsonElement = client.call(
        "POST", "/pay/orders",
        buildBody(
            "subject" to subject,
            "amount" to amount,
            "body" to body,
            "type" to type,
            "payment_method" to paymentMethod,
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

/**
 * 用户云存储：每个用户一块私有空间，按「命名空间 / 键」存放任意文档 ——
 * 收藏夹、偏好设置、草稿、存档，由接入方自己决定怎么分。
 *
 * 命名空间与键直接出现在路径里，SDK 不做转义：命名空间限小写字母、数字与 `. _ -`，
 * 键限字母、数字与 `. _ -`（都不含斜杠），不合规的值服务端回 42260 / 42261。
 *
 * **并发写入靠修订号。** 读到的 `revision` 原样作为 `ifRevision` 写回；期间若有别的设备
 * 写过，服务端回 40965（CLOUD_REVISION_CONFLICT）—— 重新读取、与本地合并之后再写。
 * 不带 `ifRevision` 等于无条件覆盖，只适合「这个键只有一台设备在写」的场景。
 *
 * 每次写入留一个修订（保留数由管理员配置），可回滚；删除默认进回收站，保留期内可恢复。
 */
class AegisCloudApi internal constructor(private val client: AegisClient) {

    /**
     * 状态：是否开放、是否可写、用量与配额、各命名空间用量与限制。
     * `enabled = false` 时整个入口应当隐藏；`writable = false`（被冻结）时只读。
     */
    @Throws(IOException::class)
    fun status(): JsonElement = client.call("GET", "/cloud", requireAuth = true)

    /** 我的条目（不含内容）。[status] 取 `active`（默认）/ `deleted`（回收站）/ `all`。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun items(
        namespace: String? = null,
        status: String? = null,
        keyword: String? = null,
        page: Int = 1,
        limit: Int = 50,
    ): JsonElement = client.call(
        "GET", "/cloud/items",
        query = buildQuery(
            "namespace" to namespace,
            "status" to status,
            "keyword" to keyword,
            "page" to page.toString(),
            "limit" to limit.toString(),
        ),
        requireAuth = true,
    )

    /**
     * 读取条目与内容。[revision] 为 null 读当前修订，否则读那个历史修订。
     *
     * 内容超过 `limits.inlineContentBytes` 时响应里 `contentOmitted = true`、没有 `content`，
     * 改用 [link] 下载。条目在回收站里时回 40465。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun item(namespace: String, key: String, revision: Long? = null): JsonElement = client.call(
        "GET", "/cloud/items/$namespace/$key",
        query = buildQuery("revision" to revision?.toString()),
        requireAuth = true,
    )

    /**
     * 写入条目。
     *
     * [content] 的形态由 [encoding] 决定：`json` 时是任意 JSON 值（Map / List / JsonElement /
     * 字符串 / 数字），原样落盘、原样读回；`text` 时是字符串；`base64` 时是 base64 字符串
     * （二进制请用 [putBytes]）。
     *
     * [ifRevision]：null 无条件覆盖；`0` 只在条目不存在时创建；其余值必须等于服务端当前修订。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun put(
        namespace: String,
        key: String,
        content: Any,
        ifRevision: Long? = null,
        encoding: String = ENCODING_JSON,
        contentType: String? = null,
        metadata: Map<String, Any?>? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "PUT", "/cloud/items/$namespace/$key",
        buildBody(
            "content" to content,
            "encoding" to encoding,
            "contentType" to contentType,
            "metadata" to metadata,
            "ifRevision" to ifRevision,
            "deviceId" to deviceId,
        ),
        requireAuth = true,
    )

    /** 写入一段文本（`text` 编码）。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun putText(
        namespace: String,
        key: String,
        text: String,
        ifRevision: Long? = null,
        contentType: String? = null,
    ): JsonElement = put(namespace, key, text, ifRevision, ENCODING_TEXT, contentType)

    /**
     * 写入二进制（`base64` 编码，JSON 请求体）。超过 `limits.jsonWriteBytes` 的内容请用 [upload]：
     * JSON 请求体有 8 MiB 上限，base64 又会膨胀三分之一。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun putBytes(
        namespace: String,
        key: String,
        bytes: ByteArray,
        ifRevision: Long? = null,
        contentType: String? = null,
    ): JsonElement = put(namespace, key, AegisCrypto.encodeBase64Url(bytes), ifRevision, ENCODING_BASE64, contentType)

    /**
     * 以文件上传写入条目（multipart，上限 `limits.maxItemBytes`）。
     * [encoding] 默认 `base64`，即按原始字节存取；上传的是 JSON / 文本文件时可声明为 `json` / `text`，
     * 读取时就按那种形态交回。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun upload(
        namespace: String,
        key: String,
        file: File,
        ifRevision: Long? = null,
        encoding: String? = null,
        contentType: String? = null,
        deviceId: String? = null,
    ): JsonElement = client.upload(
        "/cloud/items/$namespace/$key/upload", file,
        fields = buildQuery(
            "ifRevision" to ifRevision?.toString(),
            "encoding" to encoding,
            "contentType" to contentType,
            "deviceId" to deviceId,
        ),
    )

    /**
     * 删除条目：默认移入回收站（保留期见 `limits.trashRetentionDays`），[permanent] 为真时直接清除。
     * [ifRevision] 非 null 时，只在服务端当前修订与之相等时才删。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun delete(namespace: String, key: String, ifRevision: Long? = null, permanent: Boolean = false): JsonElement =
        client.call(
            "DELETE", "/cloud/items/$namespace/$key",
            query = buildQuery(
                "ifRevision" to ifRevision?.toString(),
                "permanent" to if (permanent) "true" else null,
            ),
            requireAuth = true,
        )

    /** 从回收站恢复条目。 */
    @Throws(IOException::class)
    fun restore(namespace: String, key: String): JsonElement =
        client.call("POST", "/cloud/items/$namespace/$key/restore", requireAuth = true)

    /** 条目的留存修订，新的在前（`items` 数组）。 */
    @Throws(IOException::class)
    fun revisions(namespace: String, key: String): JsonElement =
        client.call("GET", "/cloud/items/$namespace/$key/revisions", requireAuth = true)

    /** 回滚到 [revision]：以那个修订的内容生成一个新修订，历史不改写。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun rollback(
        namespace: String,
        key: String,
        revision: Long,
        ifRevision: Long? = null,
        deviceId: String? = null,
    ): JsonElement = client.call(
        "POST", "/cloud/items/$namespace/$key/rollback",
        buildBody("revision" to revision, "ifRevision" to ifRevision, "deviceId" to deviceId),
        requireAuth = true,
    )

    /** 换取内容的短时下载地址（大内容不内联时用）。[revision] 为 null 取当前修订。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun link(namespace: String, key: String, revision: Long? = null, download: Boolean = false): JsonElement =
        client.call(
            "POST", "/cloud/items/$namespace/$key/link",
            buildBody("revision" to revision, "download" to download),
            requireAuth = true,
        )

    companion object {
        const val ENCODING_JSON = "json"
        const val ENCODING_TEXT = "text"
        const val ENCODING_BASE64 = "base64"
    }
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

/**
 * 发布中心。
 *
 * 检测、最新版本、历史与上报免登录；已登录时自动带上令牌，服务端据此按用户的渠道、
 * 内测名单与灰度定向。渠道的自助加入与退出需要登录。
 */
class AegisReleaseApi internal constructor(private val client: AegisClient) {

    /**
     * 检测更新。始终返回 200：`hasUpdate` 为假表示已是最新。
     *
     * [abis] 按偏好顺序传设备支持的 ABI（Android 为 `Build.SUPPORTED_ABIS`），服务端据此挑安装包；
     * [deviceId] 用于未登录时的灰度分桶与内测名单，应在同一台设备上保持不变。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun check(
        versionCode: Long,
        platform: String = "android",
        abis: List<String> = emptyList(),
        osVersion: Int? = null,
        deviceId: String? = null,
        model: String? = null,
        locale: String? = null,
        region: String? = null,
    ): JsonElement = client.call(
        "GET", "/releases/check",
        query = buildQuery(
            "versionCode" to versionCode.toString(),
            "platform" to platform,
            "abis" to abis.takeIf { it.isNotEmpty() }?.joinToString(","),
            "osVersion" to osVersion?.toString(),
            "deviceId" to deviceId,
            "model" to model,
            "locale" to locale,
            "region" to region,
        ),
        optionalAuth = true,
    )

    /** 当前可获取的最新版本；没有时 data 为 null。未登录只看得到公开且已全量的版本。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun latest(platform: String = "android", deviceId: String? = null): JsonElement = client.call(
        "GET", "/releases/latest",
        query = buildQuery("platform" to platform, "deviceId" to deviceId),
        optionalAuth = true,
    )

    /** 可见的版本历史，按版本码倒序。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun history(page: Int = 1, limit: Int = 10, platform: String = "android", deviceId: String? = null): JsonElement =
        client.call(
            "GET", "/releases",
            query = pageQuery(page, limit) + buildQuery("platform" to platform, "deviceId" to deviceId),
            optionalAuth = true,
        )

    /**
     * 更新漏斗上报。[event] 取 `downloaded` / `installed` / `failed` / `dismissed`；
     * 「下发」由服务端在检测时自己计，不必上报。同一设备同一天同一事件只计一次。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun reportEvent(releaseId: Long, event: String, assetId: Long? = null, deviceId: String? = null): JsonElement =
        client.call(
            "POST", "/releases/events",
            buildBody("releaseId" to releaseId, "event" to event, "assetId" to assetId, "deviceId" to deviceId),
            optionalAuth = true,
        )

    /** 可自助加入的发布渠道，以及当前用户已在的渠道。 */
    @Throws(IOException::class)
    fun channels(): JsonElement = client.call("GET", "/releases/channels", requireAuth = true)

    @Throws(IOException::class)
    fun joinChannel(code: String): JsonElement =
        client.call("POST", "/releases/channels/$code/join", requireAuth = true)

    @Throws(IOException::class)
    fun leaveChannel(code: String): JsonElement =
        client.call("POST", "/releases/channels/$code/leave", requireAuth = true)
}

/**
 * AI 中转网关的账户侧（docs/ai-gateway.md）。
 *
 * 调用面在 [relay]（会话令牌）与 `relay(apiKey)`（密钥）上；也可以把任意 OpenAI / Anthropic SDK 的
 * baseURL 指到 `{站点}/api/ai/v1`（[overview] 返回的 `baseUrl`），api key 填 [createKey] 返回的 `secret`；
 * 应用允许时（`allowSessionToken = true`）也可以直接填当前登录会话的访问令牌。
 *
 * 费用按模型单价 × 本人倍率计算，按 `sources` 的顺序从免费额度、会员额度、赠送额度、积分、余额里扣。
 * 调用失败不扣费；每次调用的响应头 `X-Aegis-Request-No` 对得上 [request] 的明细。
 *
 * 金额（点、元）一律是十进制字符串，与钱包同一惯例：用 Double 会丢精度。
 */
class AegisAIGatewayApi internal constructor(private val client: AegisClient) {

    /**
     * 调用面（对话、流式、嵌入、重排、生图、改图、语音、转写），用当前登录会话的访问令牌。
     * 应用需开启 `allowSessionToken`。见 [AegisAIRelay]。
     */
    val relay: AegisAIRelay = AegisAIRelay(client, null)

    /** 调用面，用 [createKey] 领到的 `sk-ag-` 密钥（服务端、脚本、不想绑定登录态的场景）。 */
    fun relay(apiKey: String): AegisAIRelay = AegisAIRelay(client, apiKey)

    /** 中转调用面的地址（`{站点}/api/ai/v1`，不带结尾斜杠），给 OpenAI 兼容客户端当 baseURL。 */
    val relayBaseUrl: String get() = client.relayUrl("").toString().trimEnd('/')

    /**
     * 给第三方 OpenAI / Anthropic 兼容客户端（Koog、openai-java、LangChain4j 等）用的 HTTP 客户端。
     *
     * 连接（多路解析、引擎、设备头）与平台接口共用，读超时放宽到 10 分钟、不设总超时（流式对话可能很长）。
     * 每个请求的 `Authorization` 换成 [apiKey]，为空时换成当前登录会话的访问令牌（调用方自己带的会被覆盖）；
     * 会话令牌被拒（401）时单飞刷新一次再重发。只换凭据、不解析响应：网关的错误信封原样交给调用方。
     */
    @JvmOverloads
    fun relayHttpClient(apiKey: String? = null): okhttp3.OkHttpClient =
        client.relayHttp(AegisAIRelay.SYNC_READ_TIMEOUT_SECONDS).newBuilder()
            // 必须排在最前：链尾可能挂着自己发请求、不再往后传的发送层（如 Cronet），追加在它后面的拦截器不会执行。
            .apply { interceptors().add(0, AegisRelayAuthInterceptor(client, apiKey)) }
            .build()

    /** 总览：是否开放、能不能用、扣费顺序、各额度余量、积分与余额、价格倍率、今日已用、调用地址。 */
    @Throws(IOException::class)
    fun overview(): JsonElement = client.call("GET", "/ai/overview", requireAuth = true)

    /** 可调用的模型与有效单价（已乘上本人的倍率）。会员专享而本人不是会员的，`available = false`。 */
    @Throws(IOException::class)
    fun models(): JsonElement = client.call("GET", "/ai/models", requireAuth = true)

    /**
     * 公开模型目录（models.dev、OpenRouter、LiteLLM 合并）按名字批量查能力：识图、工具、思考与可选强度、
     * 上下文与输出上限。免登录，一次最多 200 个；[providers] 是目录里的供应商名（openai、anthropic、google…），
     * 作为匹配提示。查不到的项 `found = false`，只有「包含」把握的匹配不算查到。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun catalogLookup(models: List<String>, providers: List<String> = emptyList()): JsonElement = client.call(
        "GET", "/ai/catalog/lookup",
        query = buildQuery(
            "models" to models.joinToString(",").takeIf { it.isNotEmpty() },
            "providers" to providers.joinToString(",").takeIf { it.isNotEmpty() },
        ),
        optionalAuth = true,
    )

    /** 我的 API 密钥（只有 `hint`，没有明文）。 */
    @Throws(IOException::class)
    fun keys(): JsonElement = client.call("GET", "/ai/keys", requireAuth = true)

    /**
     * 新建 API 密钥。响应里的 `secret` **只出现这一次**，丢了只能删掉重建。
     *
     * [models] 为空表示不限模型；[spendLimit] 是这把密钥累计最多花多少点（"0" 或 null 不限）；
     * [expiresAt] 为 RFC 3339 时间。数量超过应用上限时返回 40215。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun createKey(
        name: String? = null,
        models: List<String>? = null,
        spendLimit: String? = null,
        expiresAt: String? = null,
    ): JsonElement = client.call(
        "POST", "/ai/keys",
        buildBody("name" to name, "models" to models, "spendLimit" to spendLimit, "expiresAt" to expiresAt),
        requireAuth = true,
    )

    /**
     * 修改 API 密钥：只改传了的字段（null 表示不改）。[status] 取 `active` / `disabled`；
     * [clearExpiry] 为 true 时清掉到期时间。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun updateKey(
        keyId: Long,
        name: String? = null,
        status: String? = null,
        models: List<String>? = null,
        spendLimit: String? = null,
        expiresAt: String? = null,
        clearExpiry: Boolean = false,
    ): JsonElement = client.call(
        "PUT", "/ai/keys/$keyId",
        buildBody(
            "name" to name,
            "status" to status,
            "models" to models,
            "spendLimit" to spendLimit,
            "expiresAt" to expiresAt,
            "clearExpiry" to if (clearExpiry) true else null,
        ),
        requireAuth = true,
    )

    /** 删除 API 密钥，立即失效；正在进行的调用照常结算。 */
    @Throws(IOException::class)
    fun deleteKey(keyId: Long): JsonElement = client.call("DELETE", "/ai/keys/$keyId", requireAuth = true)

    /**
     * 我的调用记录（新的在前）。[status]：`succeeded` / `aborted`（流式中途断开，按已生成的内容计费）/
     * `failed` / `expired`（这两种不扣费）/ `refunded` / `reserved`（进行中）。[start] / [end] 为 RFC 3339。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun requests(
        model: String? = null,
        status: String? = null,
        keyId: Long? = null,
        start: String? = null,
        end: String? = null,
        page: Int = 1,
        limit: Int = 20,
    ): JsonElement = client.call(
        "GET", "/ai/requests",
        query = buildQuery(
            "model" to model,
            "status" to status,
            "keyId" to keyId?.toString(),
            "start" to start,
            "end" to end,
            "page" to page.toString(),
            "limit" to limit.toString(),
        ),
        requireAuth = true,
    )

    /** 一次调用的明细：用量、价格、从哪些来源各扣了多少。[requestNo] 即响应头 `X-Aegis-Request-No`。 */
    @Throws(IOException::class)
    fun request(requestNo: String): JsonElement =
        client.call("GET", "/ai/requests/${encodePathSegment(requestNo)}", requireAuth = true)

    /** 赠送额度流水：看广告与管理员发放、调用消费、退款。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun credits(page: Int = 1, limit: Int = 20): JsonElement =
        client.call("GET", "/ai/credits", query = pageQuery(page, limit), requireAuth = true)
}

@kotlinx.serialization.Serializable
internal data class OAuthUrl(val url: String = "")

/** 路径上的一段：按 URL 编码（空格编成 %20 而不是 +）。 */
private fun encodePathSegment(value: String): String =
    java.net.URLEncoder.encode(value, "UTF-8").replace("+", "%20")

/** 丢掉 null 值：服务端对「字段缺失」和「字段为 null」的处理不总是一致，不传最稳。 */
private fun buildBody(vararg pairs: Pair<String, Any?>): Map<String, Any?> =
    pairs.filter { it.second != null }.toMap()

/**
 * 一起听（服务端 docs/listen-together.md）。
 *
 * 房间与长连接在独立的一起听微服务里，这里只换票据：拥有 `listen_together` 权益时，
 * 服务端签一张短期 EdDSA 票据并给出一起听服务的 wss 地址。客户端连上后第一帧发 `hello` 带票据；
 * 过了 [AegisListenTogetherTicket.refreshAfter] 再调一次 [ticket]，在连接上发 `auth` 续期。
 */
class AegisListenTogetherApi internal constructor(private val client: AegisClient) {

    /** 签一张票据。没有权益抛 [AegisException]（code [CODE_NOT_GRANTED]），服务未开通 code 为 [CODE_NOT_ENABLED]。 */
    @Throws(IOException::class)
    fun ticket(): AegisListenTogetherTicket = client.decodeAs(
        client.call("POST", "/listen-together/ticket", body = emptyMap<String, String>(), requireAuth = true),
        AegisListenTogetherTicket.serializer(),
    )

    companion object {
        /** 会员功能目录里的权益标识。 */
        const val FEATURE = "listen_together"
        const val CODE_NOT_GRANTED = 40230
        const val CODE_NOT_ENABLED = 50360
        const val CODE_UNAVAILABLE = 50361
    }
}

/** 一起听票据（时间均为毫秒时间戳，[serverTime] 可用来粗校时钟）。 */
@kotlinx.serialization.Serializable
data class AegisListenTogetherTicket(
    val ticket: String,
    val expiresAt: Long,
    val refreshAfter: Long,
    val endpoints: List<String> = emptyList(),
    val serverTime: Long = 0,
    val user: AegisListenTogetherUser = AegisListenTogetherUser(),
)

/** 票据里给房间其他人看的资料（[id] 为对外用户 ID）。 */
@kotlinx.serialization.Serializable
data class AegisListenTogetherUser(
    val id: Long = 0,
    val nickname: String = "",
    val avatar: String = "",
)

private fun buildQuery(vararg pairs: Pair<String, String?>): Map<String, String> =
    pairs.mapNotNull { (key, value) -> value?.let { key to it } }.toMap()

private fun pageQuery(page: Int, limit: Int): Map<String, String> =
    mapOf("page" to page.toString(), "limit" to limit.toString())
