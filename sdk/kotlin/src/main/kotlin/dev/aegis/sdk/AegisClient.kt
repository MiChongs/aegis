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

    /** 上传文件。multipart 在三档下都可用：sealed 档整个 multipart 体被加密。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun upload(
        path: String,
        file: File,
        fieldName: String = "file",
        fields: Map<String, String> = emptyMap(),
    ): JsonElement {
        val builder = MultipartBody.Builder().setType(MultipartBody.FORM)
        fields.forEach { (key, value) -> builder.addFormDataPart(key, value) }
        builder.addFormDataPart(
            fieldName, file.name,
            file.asRequestBody("application/octet-stream".toMediaType()),
        )
        val request = requestBuilder(path, emptyMap()).post(builder.build()).build()
        return execute(request, requireAuth = true, allowRetry = true)
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
        val payload: RequestBody? = when {
            body == null -> null
            body is RequestBody -> body
            else -> encodeBody(body)
        }
        return builder.method(method.uppercase(), payload).build()
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
        return text.toRequestBody("application/json; charset=utf-8".toMediaType())
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
        @JvmStatic
        fun builder(baseUrl: String, appKey: String): Builder = Builder(baseUrl, appKey)
    }
}
