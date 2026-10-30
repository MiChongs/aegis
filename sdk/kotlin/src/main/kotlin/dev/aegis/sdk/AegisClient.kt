package dev.aegis.sdk

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.io.IOException
import java.util.concurrent.atomic.AtomicReference

/** 令牌存储。默认在内存里；Android 上换成 EncryptedSharedPreferences 实现即可。 */
interface AegisTokenStore {
    fun accessToken(): String?
    fun refreshToken(): String?
    fun save(accessToken: String, refreshToken: String)
    fun clear()

    // 不加 @JvmStatic：接口伴生对象上的 @JvmStatic 受 jvm-default 设置牵制，
    // 而 Java 侧写 AegisTokenStore.Companion.inMemory() 一样能用，不值得为此加约束。
    companion object {
        fun inMemory(): AegisTokenStore = object : AegisTokenStore {
            private val tokens = AtomicReference<Pair<String, String>?>(null)
            override fun accessToken() = tokens.get()?.first
            override fun refreshToken() = tokens.get()?.second
            override fun save(accessToken: String, refreshToken: String) {
                tokens.set(accessToken to refreshToken)
            }

            override fun clear() = tokens.set(null)
        }
    }
}

/**
 * Aegis 应用接入客户端。
 *
 * 客户端只需要硬编码两样东西：`baseUrl` 与 `appKey`。安全等级、可用登录方式、
 * 接口路径、错误码分类全部从 `/config` 读 —— 管理员在控制台改配置，
 * 客户端下次拉 config 就跟上，不必发版。
 *
 * ```kotlin
 * val client = AegisClient.builder("https://api.example.com", "demo_app").build()
 * val session = client.auth.loginWithPassword("alice", "secret")
 * val me = client.me.profile()
 * ```
 *
 * Java：
 * ```java
 * AegisClient client = AegisClient.builder("https://api.example.com", "demo_app").build();
 * AegisSession session = client.getAuth().loginWithPassword("alice", "secret");
 * ```
 *
 * 线程安全：可以（也应该）全进程共用一个实例，内部的 OkHttpClient 自带连接池。
 */
class AegisClient private constructor(
    private val baseUrl: HttpUrl,
    val appKey: String,
    private val http: OkHttpClient,
    val tokens: AegisTokenStore,
    val clock: AegisClock,
    private val configHolder: AtomicReference<AegisConfig?>,
    private val retryPolicy: AegisRetryPolicy,
    /**
     * 多线路选择器。请求按 [baseUrl] 构造，发出前由选择器换到当前线路；响应里指向其他线路的地址
     * 换成当前线路。没配多条线路时只有 [baseUrl] 这一条，行为与单地址相同。见 [AegisLineSelector]。
     */
    val lines: AegisLineSelector,
    private val signedOutListener: ((AegisException) -> Unit)? = null,
) {

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    val auth: AegisAuthApi = AegisAuthApi(this)
    val me: AegisMeApi = AegisMeApi(this)

    /**
     * 实时通道（WebSocket）。
     *
     * 走 `/api/ws`，不在网关命名空间下，因此不受三档包装规格约束 ——
     * 与 OAuth 回跳同理。用 OkHttp 自带的 WebSocket 实现。
     */
    val realtime: AegisRealtimeApi = AegisRealtimeApi(this, http)
    val content: AegisContentApi = AegisContentApi(this)

    /** 发布中心：检测更新、最新版本、版本历史、漏斗上报与渠道自助加入。见 [AegisReleaseApi]。 */
    val releases: AegisReleaseApi = AegisReleaseApi(this)
    val commerce: AegisCommerceApi = AegisCommerceApi(this)
    val engagement: AegisEngagementApi = AegisEngagementApi(this)

    /** 用户云存储：按「命名空间 / 键」存放任意文档，带修订与回收站。见 [AegisCloudApi]。 */
    val cloud: AegisCloudApi = AegisCloudApi(this)

    /** AI 中转网关的账户侧：总览、模型与单价、API 密钥、调用记录。调用面是 OpenAI 兼容的 `/api/ai/v1`。见 [AegisAIGatewayApi]。 */
    val ai: AegisAIGatewayApi = AegisAIGatewayApi(this)

    /**
     * 版本管控：加密上报包名、版本、签名证书，取回服务端签名的裁决（放行 / 提醒 / 强制更新 / 停用）。
     * 免登录可用。见 [AegisClientControlApi]。
     */
    val control: AegisClientControlApi = AegisClientControlApi(this)

    /** 权益凭证：服务端签名、离线可验的「拥有某项权益」，见 [AegisFeatureTicketApi]。 */
    val featureTickets: AegisFeatureTicketApi = AegisFeatureTicketApi(this)

    /** 一起听：换取连接一起听服务的票据，见 [AegisListenTogetherApi]。 */
    val listenTogether: AegisListenTogetherApi = AegisListenTogetherApi(this)

    // ── /config ───────────────────────────────────────────────────────

    /**
     * 拉取（并缓存）应用配置，顺带用 `serverTime` 校准时钟。
     *
     * 服务端给这个响应设了 60 秒缓存；传输公钥有轮换窗口，因此
     * **不要把公钥单独存下来长期用**，遇到 40074 重新调本方法即可。
     */
    @Throws(IOException::class)
    fun config(forceRefresh: Boolean = false): AegisConfig {
        val cached = configHolder.get()
        if (cached != null && !forceRefresh) return cached
        val request = Request.Builder()
            .url(baseUrl.newBuilder().addPathSegments("api/v1/apps/$appKey/config").build())
            // 服务端给 /config 的缓存头是 public, max-age=60, stale-while-revalidate=300，
            // CDN 节点可以拿一份六分钟前的副本来答。里面的 serverTime 跟着旧了六分钟，
            // 用它校准时钟反而会把时钟准的设备推出 ±300 秒的签名窗口（40071），
            // 而强制刷新拿到的还是同一份旧副本。要求回源，回源不了时再用 Age 头补偿。
            .header("Cache-Control", "no-cache")
            .get()
            .build()
        val outcome = execute(request, requireAuth = false, allowRetry = false, retrySafe = true)
        val config = json.decodeFromJsonElement(AegisConfig.serializer(), outcome.data)
        if (config.serverTime.unix > 0) clock.calibrate(config.serverTime.unix + outcome.ageSeconds)
        configHolder.set(config)
        return config
    }

    /** 当前安全等级。standard / signed / sealed。 */
    fun securityLevel(): String = config().security.level

    // ── 通用调用 ──────────────────────────────────────────────────────

    /**
     * 调用网关命名空间下的任意接口。
     *
     * [path] 是相对 `/api/v1/apps/{appKey}` 的路径，如 `/auth/login`。
     * 打好的类型化封装在 [auth] / [me] / [content] / [commerce] / [engagement] 上，
     * 这个方法是给它们没覆盖到的场景留的口子。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun call(
        method: String,
        path: String,
        body: Any? = null,
        query: Map<String, String> = emptyMap(),
        requireAuth: Boolean = false,
        optionalAuth: Boolean = false,
    ): JsonElement = send(method, path, body, query, requireAuth, optionalAuth)

    /**
     * [call] 加上「能不能自动重发」的判断。
     *
     * [retrySafe] 为空时按 HTTP 方法推断：GET / HEAD 可以重发，其余只在请求确定没发出去时重发。
     * 登录这类重复执行也无害的写操作由调用处显式传 true；刷新令牌必须是 false ——
     * 服务端已经换发过的旧刷新令牌再交一次，会被判为令牌复用，整条会话链都被吊销。
     */
    internal fun send(
        method: String,
        path: String,
        body: Any? = null,
        query: Map<String, String> = emptyMap(),
        requireAuth: Boolean = false,
        optionalAuth: Boolean = false,
        retrySafe: Boolean? = null,
    ): JsonElement = execute(
        buildRequest(method, path, body, query), requireAuth, allowRetry = true,
        optionalAuth = optionalAuth, retrySafe = retrySafe,
    ).data

    /**
     * 上传文件。multipart 在三档下都可用：sealed 档整个 multipart 体被加密。
     *
     * [contentType] 不传时按文件扩展名推断。此前这里对**所有**上传固定写
     * `application/octet-stream`，而服务端的类型校验通常同时看文件名与这个头
     * （头像那条就是「扩展名或 Content-Type 命中其一即可」），
     * 一律写死等于把其中一条线索作废：一个丢了扩展名的临时文件因此无论如何都传不上去。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun upload(
        path: String,
        file: File,
        fieldName: String = "file",
        fields: Map<String, String> = emptyMap(),
        contentType: String? = null,
    ): JsonElement {
        val builder = MultipartBody.Builder().setType(MultipartBody.FORM)
        fields.forEach { (key, value) -> builder.addFormDataPart(key, value) }
        val mediaType = (contentType ?: guessContentType(file.name)).toMediaType()
        builder.addFormDataPart(fieldName, file.name, file.asRequestBody(mediaType))
        val request = requestBuilder(path, emptyMap()).post(builder.build()).build()
        return execute(request, requireAuth = true, allowRetry = true).data
    }

    /**
     * 按扩展名给出一个像样的 Content-Type。
     *
     * 只覆盖平台真正会收的那几类（头像、工单附件、存储上传），认不出来的一律回落到
     * `application/octet-stream` —— 那是"不知道"的正确表达，不是猜一个像的填上去。
     */
    private fun guessContentType(fileName: String): String =
        when (fileName.substringAfterLast('.', "").lowercase()) {
            "jpg", "jpeg" -> "image/jpeg"
            "png" -> "image/png"
            "gif" -> "image/gif"
            "webp" -> "image/webp"
            "bmp" -> "image/bmp"
            "svg" -> "image/svg+xml"
            "heic" -> "image/heic"
            "pdf" -> "application/pdf"
            "txt", "log" -> "text/plain"
            "json" -> "application/json"
            "zip" -> "application/zip"
            "mp4" -> "video/mp4"
            "mp3" -> "audio/mpeg"
            else -> "application/octet-stream"
        }

    /**
     * 实时通道的握手地址。
     *
     * 刻意不走 requestBuilder：那个方法会把路径挂到 `/api/v1/apps/{appKey}` 下，
     * 而 `/api/ws` 是平台级路由，appKey 由令牌本身携带。
     */
    internal fun realtimeUrl(): HttpUrl =
        baseUrl.newBuilder().addPathSegments("api/ws").build()

    /**
     * 免登录的 GET（已登录则带令牌），读超时放宽到 [readTimeoutSeconds]。长轮询用：
     * 默认的 OkHttp 读超时（10 秒）短于服务端挂起时间，会把每一次「没有变化」都变成超时异常。
     */
    internal fun callWithReadTimeout(path: String, query: Map<String, String>, readTimeoutSeconds: Long): JsonElement {
        // 总超时一并放宽：调用方给的 OkHttpClient 可能设了 callTimeout，它比挂起时间短时长轮询同样每次都超时。
        val client = http.newBuilder()
            .readTimeout(readTimeoutSeconds, java.util.concurrent.TimeUnit.SECONDS)
            .callTimeout(readTimeoutSeconds + 30, java.util.concurrent.TimeUnit.SECONDS)
            .build()
        return execute(buildRequest("GET", path, null, query), requireAuth = false, allowRetry = true, optionalAuth = true, httpClient = client).data
    }

    // ── AI 中转（/api/ai/v1）用的口子 ─────────────────────────────────────
    //
    // 中转面回的是 OpenAI / Anthropic 形状的响应，不是平台信封，所以不走 execute；
    // 但连接（多路解析、引擎、设备头）、令牌与单飞刷新、重试节奏都与平台接口共用。见 AegisAIRelay.kt。

    internal fun relayUrl(path: String, query: Map<String, String> = emptyMap()): HttpUrl =
        baseUrl.newBuilder()
            .addPathSegments("api/ai/v1/" + path.trimStart('/'))
            .apply { query.forEach { (key, value) -> addQueryParameter(key, value) } }
            .build()

    /** 中转调用用的 HTTP 客户端：读超时按调用放宽（生图、长轮询），总超时跟着放宽。 */
    internal fun relayHttp(readTimeoutSeconds: Long): OkHttpClient = http.newBuilder()
        .readTimeout(readTimeoutSeconds, java.util.concurrent.TimeUnit.SECONDS)
        .callTimeout(0, java.util.concurrent.TimeUnit.SECONDS)
        .build()

    internal val relayRetryPolicy: AegisRetryPolicy get() = retryPolicy

    /** 访问令牌过期时单飞刷新；没有刷新令牌返回 false。 */
    internal fun refreshForRelay(staleAccessToken: String?): Boolean {
        if (tokens.refreshToken() == null) return false
        refreshTokens(staleAccessToken)
        return true
    }

    /** 把信封里的 data 解成具体类型；各 Api 门面用它，未覆盖的字段自动忽略。 */
    internal fun <T> decodeAs(element: JsonElement, serializer: kotlinx.serialization.DeserializationStrategy<T>): T =
        json.decodeFromJsonElement(serializer, element)

    // ── 内部 ──────────────────────────────────────────────────────────

    private fun requestBuilder(path: String, query: Map<String, String>): Request.Builder {
        val url = baseUrl.newBuilder()
            .addPathSegments("api/v1/apps/$appKey" + path.trimStart('/').let { if (it.isEmpty()) "" else "/$it" })
            .apply { query.forEach { (key, value) -> addQueryParameter(key, value) } }
            .build()
        return Request.Builder().url(url)
    }

    private fun buildRequest(method: String, path: String, body: Any?, query: Map<String, String>): Request {
        val builder = requestBuilder(path, query)
        val verb = method.uppercase()
        val payload: RequestBody? = when {
            body == null -> emptyBodyFor(verb)
            body is RequestBody -> body
            else -> encodeBody(body)
        }
        return builder.method(verb, payload).build()
    }

    /**
     * 没有参数的 POST / PUT / PATCH 发一个空 JSON 对象，而不是什么都不发。
     *
     * 两边都不接受「什么都不发」：
     *
     *   1. OkHttp 直接拒绝构造这样的请求 ——
     *      `IllegalArgumentException: method POST must have a request body`。
     *      抛在构造阶段，请求根本没出设备，于是签到、退出登录、发起 TOTP 绑定、
     *      重置恢复码这类没有入参的接口在**所有**客户端上都是不可用的。
     *   2. 就算绕过它发一个零长度 body，服务端的 bind 会以 EOF 失败并回 40000。
     *      参数全可选的接口（重置恢复码带不带验证码都合法）就是这么被拒的。
     *
     * `{}` 对两边都是合法的「没有参数」，也不会改变已经带上参数的那些调用。
     */
    private fun emptyBodyFor(method: String): RequestBody? = when (method) {
        "POST", "PUT", "PATCH" -> "{}".toRequestBody(JSON_MEDIA_TYPE)
        else -> null
    }

    private fun encodeBody(body: Any): RequestBody {
        val text = when (body) {
            is String -> body
            is JsonElement -> body.toString()
            is Map<*, *> -> JsonObject(
                body.entries.associate { (key, value) -> key.toString() to toJsonElement(value) }
            ).toString()
            else -> throw IllegalArgumentException(
                "请求体只支持 String / JsonElement / Map，或直接传 okhttp 的 RequestBody；实际 ${body::class}"
            )
        }
        return text.toRequestBody(JSON_MEDIA_TYPE)
    }

    private fun toJsonElement(value: Any?): JsonElement = when (value) {
        null -> JsonNull
        is JsonElement -> value
        is String -> JsonPrimitive(value)
        is Number -> JsonPrimitive(value)
        is Boolean -> JsonPrimitive(value)
        is Map<*, *> -> JsonObject(value.entries.associate { it.key.toString() to toJsonElement(it.value) })
        is Iterable<*> -> JsonArray(value.map(::toJsonElement))
        else -> throw IllegalArgumentException(
            "请求体字段只支持字符串 / 数字 / 布尔 / Map / 列表 / JsonElement，实际 ${value::class}"
        )
    }

    /** 一次成功调用的结果：信封里的 data，外加校准时钟要用的响应元数据。 */
    private class Outcome(val data: JsonElement, val ageSeconds: Long)

    /** 拆信封之前的一次 HTTP 往返。[serverDate] 是响应的 Date 头（秒），没有时为 0。 */
    private class Exchange(val envelope: AegisEnvelope, val serverDate: Long, val ageSeconds: Long)

    private val refreshLock = Any()

    /**
     * 发请求并拆信封。
     *
     * 每一轮都从 [request]（未包装的原始请求）重新出发：令牌重新取、签名与加密由传输拦截器
     * 重新做，于是每次重发都带着新的时间戳与 nonce。OkHttp 自己的静默重试已在 Builder 里关掉，
     * 因为它重发的是同一份字节，sealed 档下会撞上 nonce 防重放（40970）。
     *
     * 两类恢复：
     *   - 网络失败：按 [retryPolicy] 退避重发。可以重发的前提是 [retrySafe]，或者失败发生在
     *     连接阶段（请求确定没出设备）。
     *   - 服务端拒绝：传输密钥轮换（40074）、时钟被拒（40071）、nonce 冲突（40970）、
     *     访问令牌过期（40100）各恢复一次。同一种恢复做第二次仍失败，说明问题不在那里，
     *     循环只会把一个明确的失败拖成一串超时。
     */
    private fun execute(
        request: Request,
        requireAuth: Boolean,
        allowRetry: Boolean,
        optionalAuth: Boolean = false,
        httpClient: OkHttpClient = http,
        retrySafe: Boolean? = null,
    ): Outcome {
        val canResend = retrySafe ?: (request.method == "GET" || request.method == "HEAD")
        var networkAttempts = 0
        var configRefreshed = false
        var clockSynced = false
        var nonceRenewed = false
        var tokenRefreshed = false
        var lineSwitches = 0

        while (true) {
            val token = when {
                requireAuth -> tokens.accessToken()
                    ?: throw AegisException.fromCode(40100, "尚未登录：没有可用的访问令牌")
                optionalAuth -> tokens.accessToken()
                else -> null
            }
            // 免登录接口：已登录就带上令牌，服务端据此按用户定向；令牌失效时服务端按未登录处理
            val prepared = token?.let { request.newBuilder().header("Authorization", "Bearer $it").build() } ?: request

            val lineBefore = lines.active()
            val exchange = try {
                exchange(prepared, httpClient)
            } catch (failure: AegisException) {
                val cause = failure.cause as? IOException
                // 选择器已把刚才那条线路判为故障并换了线路时，证书错误也值得在新线路上再试：
                // 被劫持的线路回的正是对不上的证书，而握手没完成说明请求没有发出去。
                val switched = lines.active().id != lineBefore.id
                val resendable = failure.kind == AegisException.Kind.NETWORK &&
                    (switched || cause?.isPermanent() != true) &&
                    (canResend || cause?.failedBeforeSending() == true || (switched && cause is javax.net.ssl.SSLPeerUnverifiedException))
                // 选择器刚把这条线路判为故障并换了线路：在新线路上立即重发，不计入重试次数，也不等待。
                // 每次调用最多把每条线路试一遍，防止全部不通时来回打转。
                if (resendable && switched && lineSwitches < lines.lines.size - 1) {
                    lineSwitches++
                    continue
                }
                networkAttempts++
                if (!resendable || networkAttempts >= retryPolicy.maxAttempts) throw failure
                pause(retryPolicy.delayBefore(networkAttempts))
                continue
            }

            val envelope = exchange.envelope
            if (envelope.code == 200) {
                // 服务端按收到请求的域名或自己的配置拼绝对地址；换线路后统一指向当前线路。
                return Outcome(envelope.data?.let { lines.rewrite(it) } ?: JsonNull, exchange.ageSeconds)
            }

            val descriptor = configHolder.get()?.errors?.firstOrNull { it.code == envelope.code }
            val failure = AegisException.fromCode(envelope.code, envelope.message, descriptor)
            // 本机登录已被收回（被挤下线 / 被移除）：令牌全部作废，刷新也没用，清掉并通知一次
            if (failure.isSignedOut) {
                tokens.clear()
                signedOutListener?.let { listener -> runCatching { listener(failure) } }
                throw failure
            }
            if (!allowRetry) throw failure

            when {
                failure.isRetryableAfterConfigRefresh && !configRefreshed -> {
                    configRefreshed = true
                    config(forceRefresh = true)
                }
                (failure.recovery == AegisRecovery.SYNC_CLOCK || failure.code == CODE_CLOCK_SKEW) && !clockSynced -> {
                    clockSynced = true
                    // 拒绝这次请求的响应本身就带着服务端的当前时间，而且它不可能来自缓存；
                    // 比再拉一次 /config 更准，也少一次往返。
                    if (exchange.serverDate > 0) clock.calibrate(exchange.serverDate + exchange.ageSeconds)
                    else config(forceRefresh = true)
                }
                // 被防重放拦下的请求服务端没有执行，换一个 nonce 重发是安全的。
                (failure.recovery == AegisRecovery.NEW_NONCE || failure.code == CODE_NONCE_REPLAY) && !nonceRenewed -> {
                    nonceRenewed = true
                }
                failure.isRetryableWithRefresh && requireAuth && !tokenRefreshed && tokens.refreshToken() != null -> {
                    tokenRefreshed = true
                    refreshTokens(staleAccessToken = token)
                }
                else -> throw failure
            }
        }
    }

    /**
     * 单飞刷新。
     *
     * 进程刚回到前台时，十来个并发请求会同时拿着过期令牌收到 40100。各自去刷新的话，
     * 只有第一个能成功：后面的拿着已被换掉的刷新令牌，服务端判定为令牌复用，
     * 把整条会话链（包括刚换发的那一份）一起吊销 —— 用户看到的就是「刚登录又掉了」。
     * 这里串行化，并且排在后面的线程发现令牌已经换过就直接用新的。
     */
    private fun refreshTokens(staleAccessToken: String?) {
        synchronized(refreshLock) {
            val current = tokens.accessToken()
            if (current != null && current != staleAccessToken) return
            auth.refresh()
        }
    }

    /** 一次往返：只负责把字节变成信封，以及把「不是信封的响应」归成网络失败。 */
    private fun exchange(request: Request, httpClient: OkHttpClient): Exchange {
        try {
            httpClient.newCall(request).execute().use { response ->
                // 可空写法：上游 SDK 钉的 OkHttp 4 里 body 是可空的。
                val text = response.body?.string().orEmpty()
                val serverDate = response.headers.getDate("Date")?.time?.div(1000) ?: 0L
                val age = response.header("Age")?.trim()?.toLongOrNull()?.coerceAtLeast(0) ?: 0L
                val envelope = if (text.isBlank()) null else try {
                    json.decodeFromString(AegisEnvelope.serializer(), text)
                } catch (_: kotlinx.serialization.SerializationException) {
                    null
                } catch (_: IllegalArgumentException) {
                    null
                }
                // CDN 的 502 / 504 错误页、运营商或防火墙的拦截页、被截断的响应都会走到这里。
                // 它们说明的是「这条路此刻不通」，不是服务端的业务结论，按网络失败处理才会被重试。
                if (envelope == null || (envelope.code == 0 && envelope.message.isEmpty() && envelope.data == null)) {
                    throw AegisException.network(
                        if (text.isBlank()) "HTTP ${response.code}：响应为空" else "HTTP ${response.code}：响应不是平台信封",
                        reason = AegisNetworkReason.HTTP,
                        httpStatus = response.code,
                    )
                }
                return Exchange(envelope, serverDate, age)
            }
        } catch (error: AegisException) {
            throw error
        } catch (error: IOException) {
            throw AegisException.network("请求失败：${error.message ?: error.javaClass.simpleName}", error)
        }
    }

    private fun pause(millis: Long) {
        try {
            Thread.sleep(millis)
        } catch (interrupted: InterruptedException) {
            Thread.currentThread().interrupt()
            throw AegisException.network("请求已取消", java.io.InterruptedIOException("interrupted").apply { initCause(interrupted) })
        }
    }

    class Builder internal constructor(baseUrl: String, private val appKey: String) {
        private val normalizedBase = baseUrl.trimEnd('/').toHttpUrl()
        private var appSecret: String? = null
        private var tokenStore: AegisTokenStore = AegisTokenStore.inMemory()
        private var okHttp: OkHttpClient? = null
        private var deviceProvider: (() -> AegisDevice?)? = null
        private var fallbackAddresses: List<String> = emptyList()
        private var dnsOverHttps = true
        private var retryPolicy = AegisRetryPolicy()
        private var engine: okhttp3.Interceptor? = null
        private var deviceLimitChoice = false
        private var signedOutListener: ((AegisException) -> Unit)? = null
        private var lineSelector: AegisLineSelector? = null
        private val clock = AegisClock()

        /**
         * signed / sealed 档必需。
         *
         * appSecret 是真正的密钥，**只能放在自己的服务端**。移动端与前端
         * 没有安全的地方存它，请把应用留在 standard 档 —— 那一档的安全性
         * 由 HTTPS 提供，并不比「把密钥硬编码进 APK」差。
         */
        fun appSecret(secret: String?) = apply { this.appSecret = secret }

        fun tokenStore(store: AegisTokenStore) = apply { this.tokenStore = store }

        /**
         * 传入自己的 OkHttpClient（超时、代理、日志、证书固定都在这里配）。
         *
         * SDK 在它的基础上再改三处：关掉 OkHttp 的静默重试（重试由 SDK 自己做，见 execute）、
         * 换上多路解析（见 [fallbackAddresses] / [dnsOverHttps]）、没配心跳时补一个 HTTP/2 心跳。
         */
        fun httpClient(client: OkHttpClient) = apply { this.okHttp = client }

        /**
         * 服务端域名的备用 IP（通常是源站），系统 DNS 与 DoH 的结果都连不上时依次尝试。
         *
         * 只换连接地址，TLS 仍按原域名校验证书，所以这里只能填真正持有该域名证书的机器。
         * 用于 CDN 节点在部分地区不可达、或 CDN 的 CNAME 被运营商 DNS 拦截的情况。
         */
        fun fallbackAddresses(addresses: List<String>) = apply {
            this.fallbackAddresses = addresses.map(String::trim).filter(String::isNotEmpty)
        }

        /** 系统 DNS 失败或被劫持到内网 / 回环地址时，改用公共 DNS over HTTPS 解析。默认开启。 */
        fun dnsOverHttps(enabled: Boolean) = apply { this.dnsOverHttps = enabled }

        /**
         * 替换真正发请求的那一层（例如 Android 上的 Cronet）。
         *
         * 它排在所有拦截器之后、签名与加密之后，拿到的是最终要上线的请求，因此不必理解三档包装；
         * 返回的响应再交回传输拦截器解密。可以不发请求而调 `chain.proceed` 交还给 OkHttp ——
         * WebSocket 握手（实时通道）必须这样做，引擎自己决定不了的路线也可以这样兜底。
         *
         * 失败请抛 [java.io.IOException] 的对应子类（UnknownHostException、ConnectException、
         * 带「connect」字样的 SocketTimeoutException、SSLHandshakeException …）：SDK 据此判断
         * 请求是否已送达、能不能重发，以及给出哪一类 [AegisNetworkReason]。
         */
        fun engine(interceptor: okhttp3.Interceptor?) = apply { this.engine = interceptor }

        /** 网络失败的重试节奏，默认 3 次。传 [AegisRetryPolicy.NONE] 关闭。 */
        fun retryPolicy(policy: AegisRetryPolicy) = apply { this.retryPolicy = policy }

        /**
         * 设备信息，随每个请求以 `X-Device-*` 请求头上报，见 [AegisDevice]。
         * 服务端据此在会话列表与登录记录里给出可区分的 `deviceInfo`。
         */
        fun device(device: AegisDevice) = apply { this.deviceProvider = { device } }

        /** 同上，惰性取值：每个请求调用一次 [provider]，设备 ID 首次解析较慢时用它。 */
        fun device(provider: () -> AegisDevice?) = apply { this.deviceProvider = provider }

        /**
         * 声明本客户端能让用户选择下线哪台设备。登录设备数已满、应用设置为「让用户选择」时，
         * 登录结果里会出现 [AegisSession.deviceLimit]（没有令牌），用户选好后调
         * [AegisAuthApi.resolveDeviceLimit]。不声明时服务端按应用设置的兜底方式处理（自动下线或拒绝）。
         */
        fun deviceLimitChoice(enabled: Boolean = true) = apply { this.deviceLimitChoice = enabled }

        /**
         * 本机登录被服务端收回时回调一次（[AegisException.isSignedOut]：账号在别的设备登录被挤下线，
         * 或在设备管理里被移除）。回调前 SDK 已清掉本地令牌；在回调里提示用户并回到登录页。
         * 回调在发起请求的线程上执行。
         */
        fun onSignedOut(listener: ((AegisException) -> Unit)?) = apply { this.signedOutListener = listener }

        /**
         * 多线路。[selector] 的线路里必须有一条与 baseUrl 同源（通常是第一条）：请求按 baseUrl 构造，
         * 再换到当前线路。同一个选择器可以交给多个客户端共用，线路状态随之共享。
         * 不设置时只有 baseUrl 一条线路。
         */
        fun lines(selector: AegisLineSelector?) = apply { this.lineSelector = selector }

        fun build(): AegisClient {
            val configHolder = AtomicReference<AegisConfig?>(null)
            val base = okHttp ?: OkHttpClient.Builder()
                .connectTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                .readTimeout(20, java.util.concurrent.TimeUnit.SECONDS)
                .writeTimeout(20, java.util.concurrent.TimeUnit.SECONDS)
                .build()
            val lines = lineSelector ?: AegisLineSelector(listOf(AegisLine("default", normalizedBase.host, normalizedBase.toString())))
            require(lines.lineOf(normalizedBase) != null) { "线路里没有与 baseUrl（$normalizedBase）同源的一条" }
            // 自己的连接池：选择器在直连与非直连之间切换时要清掉它的空闲连接，不能连带清掉调用方共用的池。
            val pool = okhttp3.ConnectionPool()
            val resolver = buildDns(base)
            val client = base.newBuilder()
                .dns(lines.dns(resolver))
                .connectionPool(pool)
                .retryOnConnectionFailure(false)
                // 排在最前：之后的签名、加密、发送层看到的都是已经换到当前线路的请求。
                .addInterceptor(lines.interceptor(reportOutcome = true))
                .apply { if (base.pingIntervalMillis == 0) pingInterval(20, java.util.concurrent.TimeUnit.SECONDS) }
                .apply { deviceProvider?.let { addInterceptor(AegisDeviceInterceptor(it)) } }
                .apply { if (deviceLimitChoice) addInterceptor(AegisDeviceLimitChoiceInterceptor) }
                .addInterceptor(
                    AegisTransportInterceptor(
                        appKey = appKey,
                        appSecret = appSecret,
                        clock = clock,
                        configProvider = {
                            configHolder.get()
                                ?: AegisConfig(security = SecuritySpec(level = AegisSecurityLevel.STANDARD))
                        },
                    )
                )
                .apply { engine?.let(::addInterceptor) }
                .build()
            lines.attach(AegisLineProber(base, buildDns(base, withFallback = false)), pool)
            return AegisClient(normalizedBase, appKey, client, tokenStore, clock, configHolder, retryPolicy, lines, signedOutListener)
        }

        private fun buildDns(base: OkHttpClient, withFallback: Boolean = true): okhttp3.Dns {
            if ((fallbackAddresses.isEmpty() || !withFallback) && !dnsOverHttps) return base.dns
            // DoH 请求自己走系统解析与短超时，不能绕回这个 Dns，否则查不到时会递归。
            val bootstrap = base.newBuilder()
                .dns(okhttp3.Dns.SYSTEM)
                .connectTimeout(4, java.util.concurrent.TimeUnit.SECONDS)
                .readTimeout(4, java.util.concurrent.TimeUnit.SECONDS)
                .callTimeout(6, java.util.concurrent.TimeUnit.SECONDS)
                .build()
            return AegisDns(
                system = base.dns,
                primaryHosts = setOf(normalizedBase.host.lowercase()),
                // 只接受 IP 字面量：这里填域名就又回到了「先得解析它」的问题。
                // 测速用的解析器不带它们：测的是这条线路本身通不通。
                fallbackAddresses = if (!withFallback) emptyList() else fallbackAddresses
                    .filter { IP_LITERAL.matches(it) }
                    .mapNotNull { runCatching { java.net.InetAddress.getByName(it) }.getOrNull() },
                doh = if (dnsOverHttps) AegisDns.publicResolvers(bootstrap) else emptyList(),
            )
        }
    }

    companion object {
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        private const val CODE_CLOCK_SKEW = 40071
        private const val CODE_NONCE_REPLAY = 40970
        private val IP_LITERAL = Regex("""^(\d{1,3}(\.\d{1,3}){3}|[0-9a-fA-F:]*:[0-9a-fA-F:.]*)$""")

        @JvmStatic
        fun builder(baseUrl: String, appKey: String): Builder = Builder(baseUrl, appKey)
    }
}
