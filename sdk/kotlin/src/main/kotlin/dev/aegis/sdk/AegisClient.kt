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
    val commerce: AegisCommerceApi = AegisCommerceApi(this)
    val engagement: AegisEngagementApi = AegisEngagementApi(this)

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
            .get()
            .build()
        val element = execute(request, requireAuth = false, allowRetry = false)
        val config = json.decodeFromJsonElement(AegisConfig.serializer(), element)
        clock.calibrate(config.serverTime.unix)
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
    ): JsonElement {
        return execute(buildRequest(method, path, body, query), requireAuth, allowRetry = true)
    }

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
        return execute(request, requireAuth = true, allowRetry = true)
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

    /**
     * 发请求并拆信封。
     *
     * 两处自动重试，都**只做一次**：
     *   1. 传输密钥刚轮换（40074）→ 重新拉 /config 再发一次；
     *   2. 访问令牌过期（40100）→ 用 refreshToken 换新令牌再发一次。
     * 无限重试只会把一个明确的失败拖成一串超时。
     */
    private fun execute(request: Request, requireAuth: Boolean, allowRetry: Boolean): JsonElement {
        val prepared = if (requireAuth) {
            val token = tokens.accessToken()
                ?: throw AegisException.fromCode(40100, "尚未登录：没有可用的访问令牌")
            request.newBuilder().header("Authorization", "Bearer $token").build()
        } else {
            request
        }

        val envelope = try {
            http.newCall(prepared).execute().use { response ->
                val text = response.body?.string().orEmpty()
                if (text.isBlank()) {
                    throw AegisException.network("HTTP ${response.code}：响应为空")
                }
                json.decodeFromString(AegisEnvelope.serializer(), text)
            }
        } catch (error: AegisException) {
            throw error
        } catch (error: IOException) {
            throw AegisException.network("请求失败：${error.message}", error)
        }

        if (envelope.code == 200) {
            return envelope.data ?: JsonNull
        }

        val descriptor = configHolder.get()?.errors?.firstOrNull { it.code == envelope.code }
        val failure = AegisException.fromCode(envelope.code, envelope.message, descriptor)
        if (!allowRetry) throw failure

        if (failure.isRetryableAfterConfigRefresh) {
            config(forceRefresh = true)
            return execute(request, requireAuth, allowRetry = false)
        }
        if (failure.isRetryableWithRefresh && requireAuth && tokens.refreshToken() != null) {
            auth.refresh()
            return execute(request, requireAuth, allowRetry = false)
        }
        throw failure
    }

    class Builder internal constructor(baseUrl: String, private val appKey: String) {
        private val normalizedBase = baseUrl.trimEnd('/').toHttpUrl()
        private var appSecret: String? = null
        private var tokenStore: AegisTokenStore = AegisTokenStore.inMemory()
        private var okHttp: OkHttpClient? = null
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

        /** 传入自己的 OkHttpClient（超时、代理、日志、证书固定都在这里配）。 */
        fun httpClient(client: OkHttpClient) = apply { this.okHttp = client }

        fun build(): AegisClient {
            val configHolder = AtomicReference<AegisConfig?>(null)
            val base = okHttp ?: OkHttpClient()
            val client = base.newBuilder()
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
                .build()
            return AegisClient(normalizedBase, appKey, client, tokenStore, clock, configHolder)
        }
    }

    companion object {
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()

        @JvmStatic
        fun builder(baseUrl: String, appKey: String): Builder = Builder(baseUrl, appKey)
    }
}
