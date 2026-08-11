package dev.aegis.sdk

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * 包装规格的**跨语言锚点**。
 *
 * 这里断言的是逐字节的字面量，服务端 `internal/service/auth_protocol_canonical_test.go`
 * 里有一份一模一样的。两边钉住同一串字节，任何一方改了拼接方式都会当场红掉 ——
 * 而不是等到接入方在生产环境撞上 40175，再花半天去数换行符。
 *
 * 改协议的正确姿势：先改这两个测试里的字面量，两边同时变绿，才算改完。
 */
class AegisCanonicalTest {

    private val appKey = "demo_app"
    private val keyId = "atk_11111111-2222-3333-4444-555555555555"
    private val path = "/api/v1/apps/demo_app/auth/login"
    private val timestamp = "1716175200"
    private val body = """{"account":"alice"}""".toByteArray()

    @Test
    fun `signature canonical is eight lines with the query in position five`() {
        val canonical = AegisCanonical.signatureCanonical(
            appKey = appKey,
            method = "post",
            path = path,
            query = "page=1&pageSize=20",
            timestamp = timestamp,
            nonce = "nonce-1",
            body = body,
        )
        assertEquals(
            "aegis-hmac-sha256\n" +
                "demo_app\n" +
                "POST\n" +
                "/api/v1/apps/demo_app/auth/login\n" +
                "page=1&pageSize=20\n" +
                "1716175200\n" +
                "nonce-1\n" +
                "ebdd4c28e7af5634fac89a3b251466a49b62b50c4dce0df05ac98886934ad1ec",
            canonical,
        )
        assertEquals(8, canonical.split("\n").size)
        assertEquals(false, canonical.endsWith("\n"), "末尾不能有换行")
    }

    /** 没有 query 时那一行是**空行**，不是省略 —— 少一行签名就全错。 */
    @Test
    fun `signature canonical keeps an empty line when there is no query`() {
        val canonical = AegisCanonical.signatureCanonical(
            appKey = appKey, method = "GET", path = "/api/v1/apps/demo_app/me",
            query = "", timestamp = timestamp, nonce = "nonce-1", body = ByteArray(0),
        )
        assertEquals(
            "aegis-hmac-sha256\n" +
                "demo_app\n" +
                "GET\n" +
                "/api/v1/apps/demo_app/me\n" +
                "\n" +
                "1716175200\n" +
                "nonce-1\n" +
                "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
            canonical,
        )
        assertEquals(8, canonical.split("\n").size)
    }

    @Test
    fun `request aad is seven lines and excludes the query`() {
        val aad = AegisCanonical.requestAad(
            appKey = appKey, keyId = keyId, method = "post", path = path,
            timestamp = timestamp, nonceB64 = "cmVxdWVzdC1ub25jZS0yNGJ5dGVzISE",
        ).toString(Charsets.UTF_8)
        assertEquals(
            "aegis-transport-v2\n" +
                "demo_app\n" +
                "$keyId\n" +
                "POST\n" +
                "/api/v1/apps/demo_app/auth/login\n" +
                "1716175200\n" +
                "cmVxdWVzdC1ub25jZS0yNGJ5dGVzISE",
            aad,
        )
        assertEquals(7, aad.split("\n").size)
    }

    @Test
    fun `response aad is six lines and binds the status code`() {
        val aad = AegisCanonical.responseAad(
            appKey = appKey, keyId = keyId, statusCode = 200,
            requestNonceB64 = "cmVxdWVzdC1ub25jZQ", responseNonceB64 = "cmVzcG9uc2Utbm9uY2U",
        ).toString(Charsets.UTF_8)
        assertEquals(
            "aegis-transport-v2\n" +
                "demo_app\n" +
                "$keyId\n" +
                "200\n" +
                "cmVxdWVzdC1ub25jZQ\n" +
                "cmVzcG9uc2Utbm9uY2U",
            aad,
        )
        assertEquals(6, aad.split("\n").size)
    }

    @Test
    fun `hkdf salt is sha256 of appKey colon keyId`() {
        val salt = AegisCanonical.hkdfSalt(appKey, keyId)
        assertEquals(32, salt.size)
        assertEquals(
            AegisCrypto.sha256("demo_app:$keyId".toByteArray(Charsets.UTF_8)).toList(),
            salt.toList(),
        )
    }
}
