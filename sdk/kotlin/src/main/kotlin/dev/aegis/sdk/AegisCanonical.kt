package dev.aegis.sdk

/**
 * 包装规格的**逐字节定义**。
 *
 * 这个文件是整个 SDK 里唯一允许拼接协议字符串的地方。之所以单独拎出来：
 * 三档安全等级里所有难查的故障（40175 签名不符、40077 载荷认证失败）
 * 都源于某一处换行、大小写或字段顺序对不上，而报错信息只能告诉你「不对」，
 * 不会告诉你「哪一行不对」。集中在一处，就只有一处可能出错，并且能被测试逐字节钉住。
 *
 * 服务端对应实现：
 *   internal/service/auth_protocol_service.go  computeRequestSignature / transportRequestAAD
 *   internal/service/auth_protocol_selftest.go 参考客户端（控制台「接入自检」会实跑）
 *
 * 改协议必须三处同步，否则自检立刻红掉 —— 那正是它存在的意义。
 */
internal object AegisCanonical {

    const val SIGNATURE_SCHEME = "aegis-hmac-sha256"
    const val TRANSPORT_PROTOCOL = "aegis-transport-v2"
    const val SIGNATURE_VERSION = "v2="
    const val RESPONSE_KEY_INFO = "aegis-response-v2"

    /**
     * signed 档的待签名字符串（v2）。
     *
     * 八行，`\n` 分隔，**末尾没有换行**：
     * ```
     * aegis-hmac-sha256
     * {appKey}
     * {大写 HTTP 方法}
     * {请求路径，不含 query}
     * {原样 query string，不含 `?`}
     * {Unix 秒级时间戳}
     * {nonce}
     * {sha256Hex(请求体字节)}
     * ```
     *
     * query 原样参与、不排序也不重新编码：签的就是放到线上的那串字节。
     * 排序会引入「谁的编码规则不一样」的跨语言差异，而它带来的好处为零 ——
     * 服务端读的也是原样 RawQuery。
     *
     * v1 少 query 那一行，服务端只在请求没有 query 时才接受它（40176）。
     * 本 SDK 一律发 v2。
     */
    fun signatureCanonical(
        appKey: String,
        method: String,
        path: String,
        query: String,
        timestamp: String,
        nonce: String,
        body: ByteArray,
    ): String = listOf(
        SIGNATURE_SCHEME,
        appKey,
        method.uppercase(),
        path,
        query,
        timestamp,
        nonce,
        AegisCrypto.sha256Hex(body),
    ).joinToString("\n")

    /**
     * sealed 档的请求 AAD。
     *
     * 七行，`\n` 分隔，末尾无换行：
     * ```
     * aegis-transport-v2
     * {appKey}
     * {keyId}
     * {大写 HTTP 方法}
     * {请求路径，不含 query}
     * {Unix 秒级时间戳}
     * {请求 nonce 的 base64url}
     * ```
     *
     * 注意**不含 query**：无请求体的方法把密文放在 query 里，
     * 把 query 算进 AAD 会变成「要加密得先知道密文」的死循环。
     * query 的完整性由 v2 签名保证 —— 密文本身就在被签的那串字节里。
     */
    fun requestAad(
        appKey: String,
        keyId: String,
        method: String,
        path: String,
        timestamp: String,
        nonceB64: String,
    ): ByteArray = listOf(
        TRANSPORT_PROTOCOL,
        appKey,
        keyId,
        method.uppercase(),
        path,
        timestamp,
        nonceB64,
    ).joinToString("\n").toByteArray(Charsets.UTF_8)

    /**
     * sealed 档的响应 AAD。
     *
     * 六行，`\n` 分隔，末尾无换行。绑定 HTTP 状态码与**请求** nonce：
     * 前者让 200 的响应体不能被重放成 403，后者把响应钉死在这一次请求上。
     * ```
     * aegis-transport-v2
     * {appKey}
     * {keyId}
     * {HTTP 状态码}
     * {请求 nonce 的 base64url}
     * {响应 nonce 的 base64url}
     * ```
     */
    fun responseAad(
        appKey: String,
        keyId: String,
        statusCode: Int,
        requestNonceB64: String,
        responseNonceB64: String,
    ): ByteArray = listOf(
        TRANSPORT_PROTOCOL,
        appKey,
        keyId,
        statusCode.toString(),
        requestNonceB64,
        responseNonceB64,
    ).joinToString("\n").toByteArray(Charsets.UTF_8)

    /** HKDF 盐：`SHA-256("{appKey}:{keyId}")`。取公开的 appKey，客户端手上本来就有。 */
    fun hkdfSalt(appKey: String, keyId: String): ByteArray =
        AegisCrypto.sha256("$appKey:$keyId".toByteArray(Charsets.UTF_8))
}
