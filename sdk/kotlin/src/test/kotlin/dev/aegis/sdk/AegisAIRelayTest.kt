package dev.aegis.sdk

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.RequestBody.Companion.toRequestBody
import java.net.InetSocketAddress
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * AI 中转调用面在客户端这一侧的恢复行为：轮询途中代理回错误页、访问令牌过期、保活后出错。
 * 与服务端连起来的端到端流程由 Go 侧的 TestAIRelayHarness 提供服务端、单独跑。
 */
class AegisAIRelayTest {

    private lateinit var server: HttpServer
    private val polls = AtomicInteger()
    private val refreshes = AtomicInteger()
    private val submits = AtomicInteger()

    private val port get() = server.address.port

    @BeforeTest
    fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.executor = Executors.newFixedThreadPool(8)
        server.createContext("/") { exchange ->
            val path = exchange.requestURI.path
            val auth = exchange.requestHeaders.getFirst("Authorization").orEmpty()
            when {
                path.endsWith("/auth/refresh") -> {
                    refreshes.incrementAndGet()
                    exchange.reply(200, """{"code":200,"data":{"accessToken":"fresh","refreshToken":"r2"}}""")
                }
                auth == "Bearer stale" -> exchange.reply(401,
                    """{"error":{"message":"访问令牌已过期","type":"authentication_error","code":"invalid_api_key","aegis_code":40206}}""")
                path.endsWith("/images/generations") -> {
                    submits.incrementAndGet()
                    check(exchange.requestHeaders.getFirst("Prefer") == "respond-async")
                    check(!exchange.requestHeaders.getFirst("Idempotency-Key").isNullOrBlank())
                    exchange.reply(202, """{"id":"AIG1","object":"aegis.job","status":"in_progress","created_at":1}""")
                }
                path.endsWith("/jobs/AIG1") -> when (polls.incrementAndGet()) {
                    // 代理在长轮询时回了错误页：客户端应当退避后接着轮询，而不是把任务当成失败。
                    1 -> exchange.reply(504, "<html>504 Gateway Time-out</html>", "text/html")
                    2 -> exchange.reply(200, """{"id":"AIG1","status":"in_progress","created_at":1}""")
                    else -> exchange.reply(200,
                        """{"id":"AIG1","status":"completed","created_at":1,"content_type":"application/json","result":{"data":[{"b64_json":"AQID"}]}}""")
                }
                path.endsWith("/images/edits") -> {
                    submits.incrementAndGet()
                    when (exchange.requestHeaders.getFirst("Idempotency-Key")) {
                        // 新网关：同键重复提交原样回放上一次的任务。
                        "replay" -> {
                            exchange.responseHeaders.add("Idempotent-Replayed", "true")
                            exchange.reply(200, """{"id":"AIG1","object":"aegis.job","status":"in_progress","created_at":1}""")
                        }
                        // 旧网关（或任务行还没写进去）：409 + 信封里带占用方的编号。
                        else -> exchange.reply(409,
                            """{"error":{"message":"相同的 Idempotency-Key 已有一次进行中或已成功的调用（AIG1）","type":"invalid_request_error","code":"conflict","aegis_code":40213,"request_no":"AIG1"}}""")
                    }
                }
                path.endsWith("/jobs/AIG2") -> exchange.reply(200,
                    """{"id":"AIG2","status":"failed","created_at":1,"error":{"message":"余额不足","type":"insufficient_quota","code":"insufficient_quota","aegis_code":40201,"status":402}}""")
                path.endsWith("/chat/completions") ->
                    // 网关保活开始后出的错：200 + 前导空格 + 错误信封。
                    exchange.reply(200, """   {"error":{"message":"上游超时","type":"api_error","code":"upstream_error"}}""")
                path.endsWith("/models") -> exchange.reply(200, """{"object":"list","data":[]}""")
                else -> exchange.reply(404, "")
            }
        }
        server.start()
    }

    @AfterTest
    fun stop() = server.stop(0)

    private fun client(tokens: AegisTokenStore = AegisTokenStore.inMemory()): AegisClient =
        AegisClient.builder("http://127.0.0.1:$port", "k")
            .tokenStore(tokens)
            .dnsOverHttps(false)
            .retryPolicy(AegisRetryPolicy(maxAttempts = 3, baseDelayMillis = 10))
            .build()

    @Test
    fun jobPollingSurvivesGatewayErrorPages() {
        val images = client().ai.relay("sk-ag-test").generateImages(buildJsonObject { put("model", "draw") })
        assertEquals(listOf(1, 2, 3), images.images.single().bytes()!!.map { it.toInt() })
        assertEquals(1, submits.get())
        assertEquals(3, polls.get())
    }

    @Test
    fun replayedSubmitIsMarked() {
        val body = "{}".toByteArray().toRequestBody(null)
        val job = client().ai.relay("sk-ag-test").submitJob("images/edits", body, AegisAIJobOptions(idempotencyKey = "replay"))
        assertEquals("AIG1", job.id)
        assertTrue(job.replayed)
    }

    @Test
    fun conflictingSubmitContinuesWithTheExistingJob() {
        val body = "{}".toByteArray().toRequestBody(null)
        val job = client().ai.relay("sk-ag-test").submitJob("images/edits", body, AegisAIJobOptions(idempotencyKey = "conflict"))
        assertEquals("AIG1", job.id)
        assertEquals(AegisAIJob.IN_PROGRESS, job.status)
        assertTrue(job.replayed)
        assertEquals(1, submits.get())
    }

    @Test
    fun failedJobThrowsGatewayError() {
        val failure = assertFailsWith<AegisException> {
            client().ai.relay("sk-ag-test").awaitJob(AegisAIJob(id = "AIG2", status = AegisAIJob.IN_PROGRESS))
        }
        assertEquals(40201, failure.code)
        assertEquals(402, failure.httpStatus)
        assertEquals("余额不足", failure.message)
    }

    @Test
    fun expiredSessionTokenIsRefreshedOnce() {
        val tokens = AegisTokenStore.inMemory().apply { save("stale", "r1") }
        client(tokens).ai.relay.models()
        assertEquals(1, refreshes.get())
        assertEquals("fresh", tokens.accessToken())
    }

    @Test
    fun errorAfterKeepaliveIsAFailure() {
        val failure = assertFailsWith<AegisException> {
            client().ai.relay("sk-ag-test").chat(buildJsonObject { put("model", "chat") })
        }
        assertTrue(failure.message!!.contains("上游超时"))
    }

    @Test
    fun cancellationBeforeSendingThrowsCanceled() {
        val cancellation = AegisAICancellation().apply { cancel() }
        val failure = assertFailsWith<AegisException> {
            client().ai.relay("sk-ag-test").chat(buildJsonObject { put("model", "chat") }, cancellation)
        }
        assertTrue(failure.isCanceled)
    }

    private fun HttpExchange.reply(status: Int, body: String, type: String = "application/json") {
        val bytes = body.toByteArray()
        responseHeaders.add("Content-Type", type)
        sendResponseHeaders(status, if (bytes.isEmpty()) -1 else bytes.size.toLong())
        if (bytes.isNotEmpty()) responseBody.use { it.write(bytes) } else close()
    }
}
