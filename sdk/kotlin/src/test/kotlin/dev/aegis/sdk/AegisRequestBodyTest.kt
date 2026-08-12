package dev.aegis.sdk

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

/**
 * 没有参数的 POST 也必须能构造出请求。
 *
 * OkHttp 对 POST / PUT / PATCH 拒绝 `body == null`，抛的是
 * `IllegalArgumentException: method POST must have a request body`。它发生在**构造阶段**，
 * 请求根本没出设备 —— 于是签到、退出登录、发起 TOTP 绑定、重置恢复码、清空站内信
 * 这些没有入参的接口在所有客户端上一起失效，而错误信息看上去像是调用方传错了东西。
 *
 * 这里连一个服务端都不需要：只要请求能被构造出来、失败落在网络层而不是构造阶段，
 * 这个回归就被钉住了。
 */
class AegisRequestBodyTest {

    /** 端口 1 上不会有人监听，连接会立刻被拒 —— 不依赖网络，也不会挂住。 */
    private fun client(): AegisClient = AegisClient.builder("http://127.0.0.1:1", "demo_app").build()

    @Test
    fun `body-less POST is built and fails at the network, not while building`() {
        val failure = runCatching { client().call("POST", "/signin") }.exceptionOrNull()

        assertIs<AegisException>(failure, "预期是网络异常，实际 $failure")
        assertEquals(
            AegisException.Kind.NETWORK,
            failure.kind,
            "请求应当发出去并在传输层失败，而不是在构造阶段被 OkHttp 拒绝",
        )
    }

    @Test
    fun `body-less PUT and PATCH are built too`() {
        listOf("PUT", "PATCH").forEach { method ->
            val failure = runCatching { client().call(method, "/me/profile") }.exceptionOrNull()

            assertIs<AegisException>(failure, "$method 预期是网络异常，实际 $failure")
            assertEquals(AegisException.Kind.NETWORK, failure.kind, "$method 不该在构造阶段失败")
        }
    }

    /**
     * 上传的 Content-Type 按扩展名推断。
     *
     * 服务端的类型校验通常「文件名或 Content-Type 命中其一即可」，两条线索都作废时
     * 一张真正的 JPEG 也会被拒。这里用一个临时文件走一遍真实的 [AegisClient.upload]：
     * 请求同样只应当在网络层失败。
     */
    @Test
    fun `upload builds a multipart request with a typed part`() {
        // 上传要求认证，没有令牌时会在**构造之前**就以 AUTH 失败 ——
        // 那样这条用例什么也验证不到，所以先放一个令牌进去。
        val tokens = AegisTokenStore.inMemory().apply { save("access-token", "refresh-token") }
        val client = AegisClient.builder("http://127.0.0.1:1", "demo_app")
            .tokenStore(tokens)
            .build()
        val file = java.io.File.createTempFile("aegis-avatar", ".png")
        file.writeBytes(byteArrayOf(0x89.toByte(), 0x50, 0x4E, 0x47))
        try {
            val failure = runCatching { client.upload("/me/avatar", file) }.exceptionOrNull()

            assertIs<AegisException>(failure, "预期是网络异常，实际 $failure")
            assertEquals(
                AegisException.Kind.NETWORK,
                failure.kind,
                "multipart 应当被构造出来并在传输层失败",
            )
        } finally {
            file.delete()
        }
    }

    /** GET / DELETE / HEAD 不能带 body，补一个空对象反而会被一部分服务端拒绝。 */
    @Test
    fun `body-less GET and DELETE stay body-less`() {
        listOf("GET", "DELETE", "HEAD").forEach { method ->
            val failure = runCatching { client().call(method, "/banners") }.exceptionOrNull()

            assertIs<AegisException>(failure, "$method 预期是网络异常，实际 $failure")
            assertEquals(AegisException.Kind.NETWORK, failure.kind)
        }
    }
}
