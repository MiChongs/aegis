package dev.aegis.sdk

import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import java.util.concurrent.atomic.AtomicLong

/**
 * 服务端时钟。
 *
 * signed / sealed 档最常见的线上故障是设备时钟偏差超过 5 分钟后一路 40071，
 * 而用户那边看到的只是「登录失败」。`/config` 免包装可读，是唯一能在
 * 「还没签成功」时拿到服务端时间的地方 —— 拉一次就把偏移量记下来，
 * 之后所有请求都用校准后的时间戳，设备时钟错得再离谱也不影响。
 */
class AegisClock {
    private val offsetSeconds = AtomicLong(0)

    /** 用一次 /config 的 serverTime 校准。 */
    fun calibrate(serverUnixSeconds: Long) {
        if (serverUnixSeconds > 0) {
            offsetSeconds.set(serverUnixSeconds - System.currentTimeMillis() / 1000)
        }
    }

    fun nowSeconds(): Long = System.currentTimeMillis() / 1000 + offsetSeconds.get()

    /** 当前记录的偏移量（秒），排查用。 */
    fun skewSeconds(): Long = offsetSeconds.get()
}

/**
 * 三档安全等级的统一适配器。
 *
 * 这是整个 SDK 的意义所在：**升档只换这一层，业务代码一行不动**。
 * 请求体、响应体、路径在三档下完全一致，变的只有「怎么包装」。
 *
 *   standard —— 只补一个 X-Aegis-App-Key 头
 *   signed   —— 追加时间戳 / nonce / HMAC-SHA256 签名（v2，覆盖 query）
 *   sealed   —— 在 signed 之上把载荷整体加密，并解开响应
 */
internal class AegisTransportInterceptor(
    private val appKey: String,
    private val appSecret: String?,
    private val clock: AegisClock,
    private val configProvider: () -> AegisConfig,
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        val original = chain.request()
        val config = configProvider()

        // 免包装路径：/config 是「要读配置得先按配置包装」的死锁出口，
        // OAuth 回跳由第三方重定向浏览器发起，客户端没机会包装它。
        if (isUnwrapped(original)) {
            return chain.proceed(original)
        }

        return when (config.security.level) {
            AegisSecurityLevel.SEALED -> proceedSealed(chain, original, config)
            AegisSecurityLevel.SIGNED -> chain.proceed(sign(withAppKey(original), config, null))
            else -> chain.proceed(withAppKey(original))
        }
    }

    private fun isUnwrapped(request: Request): Boolean {
        val path = request.url.encodedPath
        // 包装规格只适用于网关命名空间。实时 WebSocket 握手走的是
        // /api/ws，不归 middleware.AppGateway 管，sealed 档若照样加密它，
        // query 会被替换成 _payload 密文，握手直接失败。
        if (!path.startsWith(GATEWAY_PREFIX)) {
            return true
        }
        return path.endsWith("/config") || path.endsWith("/auth/oauth/callback")
    }

    private fun withAppKey(request: Request): Request =
        request.newBuilder().header("X-Aegis-App-Key", appKey).build()

    // ── signed ────────────────────────────────────────────────────────

    /**
     * 追加签名三件套。[presetNonce] 由 sealed 档传入 —— 那一档的 nonce
     * 同时充当 XChaCha20 的 24 字节 nonce，两处必须是同一个值。
     */
    private fun sign(request: Request, config: AegisConfig, presetNonce: String?): Request {
        val secret = appSecret
            ?: throw AegisException.transport(
                "当前应用处于 ${config.security.level} 档，必须提供 appSecret。" +
                    "appSecret 是真正的密钥，只能放在自己的服务端；移动端 / 前端请留在 standard 档。"
            )
        val timestamp = clock.nowSeconds().toString()
        val nonce = presetNonce ?: AegisCrypto.randomNonce()
        val body = request.bodyBytes()
        val canonical = AegisCanonical.signatureCanonical(
            appKey = appKey,
            method = request.method,
            path = request.url.encodedPath,
            query = request.url.encodedQuery.orEmpty(),
            timestamp = timestamp,
            nonce = nonce,
            body = body,
        )
        return request.newBuilder()
            .header("X-Aegis-Timestamp", timestamp)
            .header("X-Aegis-Nonce", nonce)
            .header("X-Aegis-Signature", AegisCanonical.SIGNATURE_VERSION + AegisCrypto.hmacSha256Hex(secret, canonical))
            .build()
    }

    // ── sealed ────────────────────────────────────────────────────────

    private fun proceedSealed(chain: Interceptor.Chain, original: Request, config: AegisConfig): Response {
        val spec = config.security.transport
            ?: throw AegisException.transport("应用处于 sealed 档，但 /config 没有下发 transport 规格")
        val serverKey = spec.activePublicKey()
            ?: throw AegisException.transport("/config 没有可用的 active 传输公钥，请让管理员轮换一次传输密钥")

        val ephemeral = AegisCrypto.generateEphemeralKeyPair()
        val shared = AegisCrypto.sharedSecret(ephemeral.privateKey, AegisCrypto.decodeBase64Url(serverKey.publicKey))
        val nonce = AegisCrypto.randomBytes(AegisCrypto.NONCE_SIZE)
        val nonceB64 = AegisCrypto.encodeBase64Url(nonce)
        val timestamp = clock.nowSeconds().toString()
        val path = original.url.encodedPath

        val aad = AegisCanonical.requestAad(appKey, serverKey.keyId, original.method, path, timestamp, nonceB64)
        val requestKey = AegisCrypto.deriveKey(
            sharedSecret = shared,
            salt = AegisCanonical.hkdfSalt(appKey, serverKey.keyId),
            info = aad,
        )

        val bodyless = spec.bodylessMethods.any { it.equals(original.method, ignoreCase = true) }
        // 无请求体的方法：明文是真正的 query string，密文放进 `_payload`。
        // 没有查询参数时明文是空串 —— AEAD 对空明文照样产出 16 字节 tag，
        // 于是「有没有参数」不构成分支。
        val plaintext = if (bodyless) {
            original.url.encodedQuery.orEmpty().toByteArray(Charsets.UTF_8)
        } else {
            original.bodyBytes()
        }
        val sealedPayload = AegisCrypto.encodeBase64Url(
            AegisCrypto.seal(requestKey, nonce, plaintext, aad)
        )

        var builder = original.newBuilder()
            .header("X-Aegis-App-Key", appKey)
            .header("X-Aegis-Protocol", AegisCanonical.TRANSPORT_PROTOCOL)
            .header("X-Aegis-Key-Id", serverKey.keyId)
            .header("X-Aegis-Client-Key", AegisCrypto.encodeBase64Url(ephemeral.publicKey))

        builder = if (bodyless) {
            // 整个 query 被替换成单个 `_payload`，原参数已经在密文里。
            val url = original.url.newBuilder()
                .query(null)
                .addQueryParameter(spec.payloadParam, sealedPayload)
                .build()
            builder.url(url).method(original.method, null)
        } else {
            val plainType = original.body?.contentType()?.toString()
            if (plainType != null && !plainType.startsWith("application/json")) {
                // 上传等非 JSON 载荷：原始 Content-Type 由这个头带过去，
                // 否则服务端拆包后会把 multipart 当 JSON 解析。
                builder = builder.header(spec.plainContentTypeHeader, plainType)
            }
            builder.method(
                original.method,
                sealedPayload.toByteArray(Charsets.UTF_8)
                    .toRequestBody("application/octet-stream".toMediaTypeOrNull()),
            )
        }

        // 签的是最终发出去的字节：sealed 档签的就是那串密文，
        // 以及包含密文的那个 query。
        val response = chain.proceed(sign(builder.build(), config, nonceB64))
        return openResponse(response, requestKey, serverKey.keyId, nonceB64, spec)
    }

    /**
     * 解开 sealed 响应。
     *
     * 网关在**拆包之前**就拒掉的请求（签名不符、时间戳过期、载荷格式错）
     * 返回的是明文 JSON —— 那时还没有加密上下文可用。因此这里以
     * `X-Aegis-Response-Nonce` 是否存在来判断，缺了就原样透传。
     * 不判这一下，接入期最需要看到的那些错误信息会全部变成「响应解密失败」。
     */
    private fun openResponse(
        response: Response,
        requestKey: ByteArray,
        keyId: String,
        requestNonceB64: String,
        spec: TransportSpec,
    ): Response {
        val responseNonceB64 = response.header("X-Aegis-Response-Nonce")
        if (responseNonceB64.isNullOrBlank()) {
            return response
        }
        val body = response.body ?: return response
        val ciphertext = AegisCrypto.decodeBase64Url(body.string().trim())
        val plaintext = AegisCrypto.open(
            key = AegisCrypto.deriveResponseKey(requestKey),
            nonce = AegisCrypto.decodeBase64Url(responseNonceB64),
            ciphertext = ciphertext,
            aad = AegisCanonical.responseAad(appKey, keyId, response.code, requestNonceB64, responseNonceB64),
        )
        val plainType = response.header(spec.plainContentTypeHeader) ?: "application/json; charset=utf-8"
        return response.newBuilder()
            .body(plaintext.toResponseBody(plainType.toMediaTypeOrNull()))
            .removeHeader("Content-Length")
            .build()
    }
}

private const val GATEWAY_PREFIX = "/api/v1/apps/"

/** 取请求体字节；无请求体时是空数组（空 body 的 sha256 也要参与签名）。 */
internal fun Request.bodyBytes(): ByteArray {
    val body: RequestBody = body ?: return ByteArray(0)
    val buffer = Buffer()
    body.writeTo(buffer)
    return buffer.readByteArray()
}
