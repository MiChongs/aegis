package dev.aegis.sdk

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Call
import okhttp3.Interceptor
import okhttp3.MediaType
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okio.ByteString.Companion.decodeBase64
import java.io.File
import java.io.IOException
import java.util.UUID

/**
 * AI 中转的调用面（`/api/ai/v1`，docs/ai-gateway.md）：对话、流式对话、嵌入、重排、生图、改图、语音合成、转写。
 *
 * 取得方式：`client.ai.relay` 用当前登录会话的访问令牌（应用需开启 `allowSessionToken`）；
 * `client.ai.relay(apiKey)` 用 `sk-ag-` 密钥。请求体与响应体都是 OpenAI（或 Anthropic）的原生形状，
 * 这里不重新建模，交给调用方按需取字段 —— 各家扩展字段因此都能原样带上。
 *
 * **长调用一律走异步任务**：生图、改图、语音、转写先提交（`Prefer: respond-async`，立刻返回任务编号），
 * 再长轮询取结果。每个请求都很短，不会被中间任何一层反向代理的读超时切断；轮询途中断网会自动重试，
 * 任务不受影响。对话不走任务：要么流式（持续有数据），要么由网关的保活撑住。
 *
 * 全部方法是阻塞的，请在后台线程调用。取消：把 [AegisAICancellation] 传进去，在别的线程调它的 `cancel()` ——
 * 连接立即断开，异步任务一并取消；网关整笔放回，不扣费。取消时抛出的 [AegisException.isCanceled] 为 true。
 *
 * 失败统一抛 [AegisException]：网关的业务错误带 `aegis_code`（402xx，如 40201 余额不足）与 `httpStatus`；
 * 上游供应商的错误 `code` 为 0、`httpStatus` 为网关回的状态；连不上、代理错误页为 NETWORK 类。
 */
class AegisAIRelay internal constructor(
    private val client: AegisClient,
    private val apiKey: String?,
) {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    // ── 模型 ──

    /** 可调用的模型（OpenAI `GET /models` 形状：`{"object":"list","data":[{"id":…}]}`）。 */
    @Throws(IOException::class)
    fun models(): JsonObject = request("GET", "models").use { response -> parseObject(response) }

    // ── 对话 ──

    /** Chat Completions（非流式）。[body] 是 OpenAI 的请求体；`stream` 会被强制为 false。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun chat(body: JsonObject, cancellation: AegisAICancellation? = null): AegisAIResult =
        jsonCall("chat/completions", body.with("stream", JsonPrimitive(false)), cancellation)

    /**
     * Chat Completions 流式：每个 `chat.completion.chunk` 回调一次 [onChunk]（在调用线程上），
     * 最后一块带 `usage`。返回调用编号。流途中出错（含上游中断）抛 [AegisException]。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun chatStream(body: JsonObject, cancellation: AegisAICancellation? = null, onChunk: (JsonObject) -> Unit): String {
        val payload = body.with("stream", JsonPrimitive(true))
        return stream("chat/completions", payload, cancellation) { _, data -> onChunk(data) }
    }

    /** Anthropic Messages（非流式）。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun messages(body: JsonObject, cancellation: AegisAICancellation? = null): AegisAIResult =
        jsonCall("messages", body.with("stream", JsonPrimitive(false)), cancellation)

    /** Anthropic Messages 流式：每个事件回调一次（事件名如 `content_block_delta`，数据为事件体）。返回调用编号。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun messagesStream(
        body: JsonObject,
        cancellation: AegisAICancellation? = null,
        onEvent: (event: String, data: JsonObject) -> Unit,
    ): String = stream("messages", body.with("stream", JsonPrimitive(true)), cancellation, onEvent)

    // ── 嵌入、重排 ──

    @Throws(IOException::class)
    @JvmOverloads
    fun embeddings(body: JsonObject, cancellation: AegisAICancellation? = null): AegisAIResult =
        jsonCall("embeddings", body, cancellation)

    /** 重排（Jina / Cohere / SiliconFlow 同形）。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun rerank(body: JsonObject, cancellation: AegisAICancellation? = null): AegisAIResult =
        jsonCall("rerank", body, cancellation)

    // ── 图像（异步任务） ──

    /**
     * 生图。[body] 是 OpenAI `images/generations` 的请求体（model、prompt、n、size、quality…）。
     * 提交为异步任务后等待完成；[options] 控制最长等待、进度回调与取消。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun generateImages(body: JsonObject, options: AegisAIJobOptions = AegisAIJobOptions()): AegisAIImages {
        val job = submitJob("images/generations", body.toString().toRequestBody(JSON), options)
        return AegisAIImages.from(awaitJob(job, options), json)
    }

    /**
     * 改图。[fields] 是表单字段（model、prompt、n、size、quality…），[images] 是要修改或参考的图片：
     * 一张时字段名为 `image`，多张时为 `image[]`（OpenAI 改图接口的多图写法）；[mask] 可选。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun editImages(
        fields: Map<String, String>,
        images: List<AegisAIFile>,
        mask: AegisAIFile? = null,
        options: AegisAIJobOptions = AegisAIJobOptions(),
    ): AegisAIImages {
        require(images.isNotEmpty()) { "至少要一张图片" }
        val field = if (images.size == 1) "image" else "image[]"
        val form = MultipartBody.Builder().setType(MultipartBody.FORM)
        fields.forEach { (key, value) -> form.addFormDataPart(key, value) }
        images.forEach { file -> form.addFormDataPart(field, file.name, file.body()) }
        mask?.let { form.addFormDataPart("mask", it.name, it.body()) }
        val job = submitJob("images/edits", form.build(), options)
        return AegisAIImages.from(awaitJob(job, options), json)
    }

    // ── 语音（异步任务） ──

    /** 语音合成。返回音频字节与类型（如 `audio/mpeg`）。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun speech(body: JsonObject, options: AegisAIJobOptions = AegisAIJobOptions()): AegisAIBinary {
        val job = awaitJob(submitJob("audio/speech", body.toString().toRequestBody(JSON), options), options)
        return jobContent(job.id)
    }

    /**
     * 语音转写。[fields] 是表单字段（model、language、response_format…），[file] 是音频。
     * `response_format` 为 json / verbose_json（默认）时 [AegisAIResult.body] 是 JSON；text / srt / vtt 时看 [AegisAIResult.text]。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun transcribe(fields: Map<String, String>, file: AegisAIFile, options: AegisAIJobOptions = AegisAIJobOptions()): AegisAIResult =
        audioJob("audio/transcriptions", fields, file, options)

    /** 语音翻译成英文，参数同 [transcribe]。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun translate(fields: Map<String, String>, file: AegisAIFile, options: AegisAIJobOptions = AegisAIJobOptions()): AegisAIResult =
        audioJob("audio/translations", fields, file, options)

    private fun audioJob(path: String, fields: Map<String, String>, file: AegisAIFile, options: AegisAIJobOptions): AegisAIResult {
        val form = MultipartBody.Builder().setType(MultipartBody.FORM)
        fields.forEach { (key, value) -> form.addFormDataPart(key, value) }
        form.addFormDataPart("file", file.name, file.body())
        val job = awaitJob(submitJob(path, form.build(), options), options)
        job.result?.let { return AegisAIResult(job.id, it, job.contentType ?: "application/json", null) }
        val content = jobContent(job.id)
        return AegisAIResult(job.id, null, content.contentType, String(content.bytes, Charsets.UTF_8))
    }

    // ── 异步任务（底层） ──

    /**
     * 以异步任务提交任意原样转发的接口（[path] 如 `images/generations`）。鉴权、余额等错误在这里直接抛出；
     * 成功返回进行中的任务。请求带幂等键：连接阶段失败时自动重发，不会重复扣费。
     *
     * 调用方拿不准上一次提交有没有到达（读超时、断网、进程被杀）时，用**同一个** [AegisAIJobOptions.idempotencyKey]
     * 再调一次：网关回放上一次建立的任务（[AegisAIJob.replayed] 为真），不会新建、不会再扣费。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun submitJob(path: String, body: RequestBody, options: AegisAIJobOptions = AegisAIJobOptions()): AegisAIJob {
        val idempotencyKey = options.idempotencyKey ?: UUID.randomUUID().toString()
        val response = try {
            request(
                "POST", path, body = body, cancellation = options.cancellation,
                headers = mapOf("Prefer" to "respond-async", "Idempotency-Key" to idempotencyKey),
            )
        } catch (failure: AegisException) {
            // 网关没能回放（旧版本网关，或任务行还没写进去）时回 409（40213），信封里带占用这个键的调用编号：
            // 那就是上一次建立的任务，接着轮询它。状态先当进行中，第一次轮询就会拿到真实状态。
            val existing = failure.idempotentRequestNo() ?: throw failure
            return AegisAIJob(id = existing, endpoint = path, status = AegisAIJob.IN_PROGRESS, replayed = true)
        }
        return response.use {
            val replayed = it.header(IDEMPOTENT_REPLAYED).equals("true", ignoreCase = true)
            decodeJob(it).copy(replayed = replayed)
        }
    }

    /** 取任务。[waitSeconds] > 0 时长轮询（≤ 25）：任务还在进行中就由服务端挂着，有结论立刻返回。 */
    @Throws(IOException::class)
    @JvmOverloads
    fun job(id: String, waitSeconds: Int = 0, cancellation: AegisAICancellation? = null): AegisAIJob {
        val wait = waitSeconds.coerceIn(0, MAX_WAIT_SECONDS)
        val query = if (wait > 0) mapOf("wait" to wait.toString()) else emptyMap()
        return request("GET", "jobs/${encodeSegment(id)}", query = query, cancellation = cancellation,
            readTimeoutSeconds = wait + 30L, idempotent = true).use { decodeJob(it) }
    }

    /** 取消任务（不扣费）。已经结束的任务原样返回。 */
    @Throws(IOException::class)
    fun cancelJob(id: String): AegisAIJob =
        request("DELETE", "jobs/${encodeSegment(id)}", idempotent = true).use { decodeJob(it) }

    /** 已完成任务的结果原文（二进制结果用，如语音合成）。 */
    @Throws(IOException::class)
    fun jobContent(id: String): AegisAIBinary =
        request("GET", "jobs/${encodeSegment(id)}/content", idempotent = true, readTimeoutSeconds = 120).use { response ->
            AegisAIBinary(id, response.body?.bytes() ?: ByteArray(0), response.header("Content-Type").orEmpty())
        }

    /**
     * 等任务结束：长轮询（每次服务端最多挂 20 秒），轮询失败按网络错误退避重试直到 [AegisAIJobOptions.timeoutSeconds]。
     * 完成返回任务（结果在 `result` 或 `resultUrl`）；失败抛出与同步调用一致的 [AegisException]；
     * 超时或取消时先取消服务端的任务（不扣费）再抛出。
     */
    @Throws(IOException::class)
    @JvmOverloads
    fun awaitJob(job: AegisAIJob, options: AegisAIJobOptions = AegisAIJobOptions()): AegisAIJob {
        val deadline = System.currentTimeMillis() + options.timeoutSeconds * 1000
        var current = job
        var failures = 0
        while (true) {
            options.onProgress?.let { runCatching { it(current) } }
            when (current.status) {
                AegisAIJob.COMPLETED -> return current
                AegisAIJob.FAILED, AegisAIJob.CANCELED -> throw current.failure()
            }
            val remaining = deadline - System.currentTimeMillis()
            if (remaining <= 0) {
                runCatching { cancelJob(current.id) }
                throw AegisException.network("等待 AI 任务超时（${options.timeoutSeconds} 秒），已取消，本次不扣费",
                    java.net.SocketTimeoutException("job timeout"), AegisNetworkReason.TIMEOUT)
            }
            try {
                current = job(current.id, (remaining / 1000).toInt().coerceIn(1, POLL_WAIT_SECONDS), options.cancellation)
                failures = 0
            } catch (failure: AegisException) {
                if (failure.isCanceled) {
                    runCatching { cancelJob(current.id) }
                    throw failure
                }
                // 轮询是幂等的：网络抖动、代理错误页都可以重来，任务在服务端照常进行。
                if (failure.kind != AegisException.Kind.NETWORK) throw failure
                failures++
                sleep(minOf(1000L * (1 shl minOf(failures, 4)), 15_000L), options.cancellation, current.id)
            }
        }
    }

    // ── HTTP ──

    private fun jsonCall(path: String, body: JsonObject, cancellation: AegisAICancellation?): AegisAIResult =
        request("POST", path, body = body.toString().toRequestBody(JSON), cancellation = cancellation,
            readTimeoutSeconds = SYNC_READ_TIMEOUT_SECONDS).use { response ->
            AegisAIResult(response.header(REQUEST_NO).orEmpty(), parseObject(response),
                response.header("Content-Type").orEmpty(), null)
        }

    /** SSE：OpenAI 只有 data 行（`[DONE]` 结束），Anthropic 带 event 行。 */
    private fun stream(
        path: String,
        body: JsonObject,
        cancellation: AegisAICancellation?,
        onEvent: (String, JsonObject) -> Unit,
    ): String {
        val response = request("POST", path, body = body.toString().toRequestBody(JSON), cancellation = cancellation,
            readTimeoutSeconds = SYNC_READ_TIMEOUT_SECONDS, headers = mapOf("Accept" to "text/event-stream"))
        response.use {
            val requestNo = response.header(REQUEST_NO).orEmpty()
            val source = response.body?.source() ?: return requestNo
            var event = ""
            try {
                while (true) {
                    val line = source.readUtf8Line() ?: break
                    when {
                        line.startsWith("event:") -> event = line.substring(6).trim()
                        line.startsWith("data:") -> {
                            val data = line.substring(5).trim()
                            if (data == "[DONE]") return requestNo
                            val element = runCatching { json.parseToJsonElement(data) }.getOrNull() as? JsonObject
                            if (element != null) {
                                streamError(event, element)?.let { throw it }
                                onEvent(event.ifEmpty { "message" }, element)
                                if (event == "message_stop") return requestNo
                            }
                        }
                        line.isEmpty() -> event = ""
                    }
                }
            } catch (failure: IOException) {
                if (failure is AegisException) throw failure
                if (cancellation?.isCanceled == true) throw AegisException.canceled()
                throw AegisException.network("流式响应中断：${failure.message ?: failure.javaClass.simpleName}", failure)
            }
            return requestNo
        }
    }

    /** 流里的错误块：OpenAI 是带 `error` 的 data，Anthropic 是 `event: error`。 */
    private fun streamError(event: String, data: JsonObject): AegisException? {
        if (event != "error" && data["error"] == null) return null
        return relayFailure(data, 502)
    }

    /**
     * 发一次中转请求，返回 2xx 的响应（调用方负责关闭）。
     *
     * 令牌：密钥模式用密钥；会话模式用访问令牌，401 时单飞刷新一次再发。网络失败：幂等请求按重试节奏重发，
     * 非幂等请求只在「请求确定没发出去」时重发。非 2xx 解析成错误信封抛出；不是信封的（代理错误页）按网络失败。
     */
    private fun request(
        method: String,
        path: String,
        body: RequestBody? = null,
        query: Map<String, String> = emptyMap(),
        headers: Map<String, String> = emptyMap(),
        cancellation: AegisAICancellation? = null,
        readTimeoutSeconds: Long = 60,
        idempotent: Boolean = method == "GET",
    ): Response {
        val http = client.relayHttp(readTimeoutSeconds)
        val policy = client.relayRetryPolicy
        var token = apiKey ?: client.tokens.accessToken()
            ?: throw AegisException.fromCode(40100, "尚未登录：没有可用的访问令牌")
        var refreshed = false
        var attempts = 0
        while (true) {
            cancellation?.ensureActive()
            val builder = Request.Builder()
                .url(client.relayUrl(path, query))
                .header("Authorization", "Bearer $token")
                .method(method, body ?: if (method == "POST" || method == "PUT") ByteArray(0).toRequestBody(null) else null)
            headers.forEach { (key, value) -> builder.header(key, value) }
            val call = http.newCall(builder.build())
            cancellation?.bind(call)
            val response = try {
                call.execute()
            } catch (failure: IOException) {
                if (cancellation?.isCanceled == true) throw AegisException.canceled()
                attempts++
                val resendable = !failure.isPermanent() && (idempotent || failure.failedBeforeSending())
                if (!resendable || attempts >= policy.maxAttempts) {
                    throw AegisException.network("请求失败：${failure.message ?: failure.javaClass.simpleName}", failure)
                }
                sleep(policy.delayBefore(attempts), cancellation, null)
                continue
            } finally {
                cancellation?.unbind(call)
            }
            if (response.isSuccessful) return response
            val status = response.code
            val text = response.body?.string().orEmpty()
            response.close()
            if (status == 401 && apiKey == null && !refreshed) {
                refreshed = true
                if (client.refreshForRelay(token)) {
                    token = client.tokens.accessToken() ?: throw AegisException.fromCode(40100, "登录已失效，请重新登录")
                    continue
                }
            }
            val envelope = runCatching { json.parseToJsonElement(text) as? JsonObject }.getOrNull()
            if (envelope?.get("error") != null) throw relayFailure(envelope, status)
            // 代理与 CDN 的错误页（502 / 504 HTML）：说明这条路此刻不通，幂等请求再试一次。
            attempts++
            if (idempotent && status >= 500 && attempts < policy.maxAttempts) {
                sleep(policy.delayBefore(attempts), cancellation, null)
                continue
            }
            throw AegisException.network(
                if (text.isBlank()) "HTTP $status：响应为空" else "HTTP $status：网关没有返回 AI 响应",
                reason = AegisNetworkReason.HTTP, httpStatus = status,
            )
        }
    }

    /** 2xx 的 JSON 响应。网关保活开始后出的错是 200 + 错误信封，这里一并识别。 */
    private fun parseObject(response: Response): JsonObject {
        val text = response.body?.string().orEmpty()
        val element = try {
            json.parseToJsonElement(text)
        } catch (failure: Exception) {
            throw AegisException.network("AI 响应不是 JSON：${failure.message}", reason = AegisNetworkReason.HTTP, httpStatus = response.code)
        }
        val obj = element as? JsonObject
            ?: throw AegisException.network("AI 响应不是 JSON 对象", reason = AegisNetworkReason.HTTP, httpStatus = response.code)
        if (obj["error"] is JsonObject && obj["choices"] == null && obj["data"] == null) throw relayFailure(obj, 502)
        return obj
    }

    private fun decodeJob(response: Response): AegisAIJob {
        val text = response.body?.string().orEmpty()
        return try {
            json.decodeFromString(AegisAIJob.serializer(), text)
        } catch (failure: Exception) {
            throw AegisException.network("任务响应无法解析：${failure.message}", reason = AegisNetworkReason.HTTP, httpStatus = response.code)
        }
    }

    private fun sleep(millis: Long, cancellation: AegisAICancellation?, jobId: String?) {
        val until = System.currentTimeMillis() + millis
        while (System.currentTimeMillis() < until) {
            if (cancellation?.isCanceled == true) {
                jobId?.let { id -> runCatching { cancelJob(id) } }
                throw AegisException.canceled()
            }
            try {
                Thread.sleep(minOf(200L, until - System.currentTimeMillis()).coerceAtLeast(1))
            } catch (interrupted: InterruptedException) {
                Thread.currentThread().interrupt()
                throw AegisException.canceled()
            }
        }
    }

    internal companion object {
        val JSON: MediaType = "application/json; charset=utf-8".toMediaType()
        const val REQUEST_NO = "X-Aegis-Request-No"
        /** 网关对同键重复提交原样回放上一次的任务时带的头。 */
        const val IDEMPOTENT_REPLAYED = "Idempotent-Replayed"
        /** 幂等键已被一次进行中或已成功的调用占用。 */
        const val IDEMPOTENCY_IN_USE = 40213
        const val MAX_WAIT_SECONDS = 25
        const val POLL_WAIT_SECONDS = 20
        /** 同步调用（对话、嵌入）的读超时：网关在 20 秒后开始每 15 秒写保活，这个值只需盖过一次间隔。 */
        const val SYNC_READ_TIMEOUT_SECONDS = 600L

        fun relayFailure(envelope: JsonObject, fallbackStatus: Int): AegisException {
            val error = envelope["error"] as? JsonObject
            val message = error?.get("message")?.jsonPrimitive?.contentOrNull.orEmpty()
            val code = error?.get("aegis_code")?.jsonPrimitive?.intOrNull ?: 0
            val status = error?.get("status")?.jsonPrimitive?.intOrNull ?: fallbackStatus
            return AegisException.relay(code, message, status, data = error)
        }

        /** 40213（幂等键已被占用）里占用方的调用编号；网关没给（旧版本）时为 null。 */
        internal fun AegisException.idempotentRequestNo(): String? {
            if (code != IDEMPOTENCY_IN_USE) return null
            val error = data as? JsonObject ?: return null
            return error["request_no"]?.jsonPrimitive?.contentOrNull?.takeIf { it.isNotBlank() }
        }

        private fun encodeSegment(value: String): String =
            java.net.URLEncoder.encode(value, "UTF-8").replace("+", "%20")

        private fun JsonObject.with(key: String, value: JsonElement): JsonObject = JsonObject(this + (key to value))
    }
}


/**
 * [AegisAIGatewayApi.relayHttpClient] 的鉴权：给每个请求换上凭据；会话令牌被拒（401）时单飞刷新一次，
 * 换到新令牌才重发，刷新不成就把原来的 401 交给调用方。
 */
internal class AegisRelayAuthInterceptor(
    private val client: AegisClient,
    private val apiKey: String?,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val token = apiKey ?: client.tokens.accessToken()
            ?: throw AegisException.fromCode(40100, "尚未登录：没有可用的访问令牌")
        val response = chain.proceed(chain.request().withBearer(token))
        if (response.code != 401 || apiKey != null) return response
        val refreshed = try {
            client.refreshForRelay(token)
        } catch (failure: IOException) {
            false
        }
        val fresh = client.tokens.accessToken()
        if (!refreshed || fresh == null || fresh == token) return response
        response.close()
        return chain.proceed(chain.request().withBearer(fresh))
    }

    private fun Request.withBearer(token: String): Request =
        newBuilder().header("Authorization", "Bearer $token").build()
}

/**
 * 取消令牌。把它传给 [AegisAIRelay] 的方法，在任意线程调 [cancel]：进行中的请求立即断开，
 * 异步任务一并在服务端取消。网关把这笔调用整笔放回，不扣费。一个令牌只对应一次调用。
 */
class AegisAICancellation {
    @Volatile
    var isCanceled: Boolean = false
        private set

    @Volatile
    private var call: Call? = null

    fun cancel() {
        isCanceled = true
        call?.cancel()
    }

    internal fun bind(call: Call) {
        this.call = call
        if (isCanceled) call.cancel()
    }

    internal fun unbind(call: Call) {
        if (this.call === call) this.call = null
    }

    internal fun ensureActive() {
        if (isCanceled) throw AegisException.canceled()
    }
}

/** 异步任务的等待选项。 */
class AegisAIJobOptions @JvmOverloads constructor(
    /** 最长等多久（秒），超过就取消任务（不扣费）并抛出超时。 */
    val timeoutSeconds: Long = 600,
    /** 每次轮询到新状态时回调（在调用线程上）。 */
    val onProgress: ((AegisAIJob) -> Unit)? = null,
    val cancellation: AegisAICancellation? = null,
    /** 提交用的幂等键；同一次生成的重试带同一个键，网关保证不会重复扣费。默认每次提交随机生成。 */
    val idempotencyKey: String? = null,
)

/** 一次同步调用的结果：JSON 响应在 [body]，纯文本响应（如 text 格式的转写）在 [text]。 */
class AegisAIResult(
    /** 调用编号，对得上 [AegisAIGatewayApi.request] 的明细。 */
    val requestNo: String,
    val body: JsonObject?,
    val contentType: String,
    val text: String?,
)

/** 二进制结果（语音合成）。 */
class AegisAIBinary(val requestNo: String, val bytes: ByteArray, val contentType: String)

/** 上传给中转的文件（改图的图片、转写的音频）。 */
class AegisAIFile private constructor(
    val name: String,
    private val contentType: String,
    private val file: File?,
    private val bytes: ByteArray?,
) {
    internal fun body(): RequestBody {
        val type = contentType.toMediaType()
        return file?.asRequestBody(type) ?: bytes!!.toRequestBody(type)
    }

    companion object {
        @JvmStatic
        fun of(file: File, contentType: String): AegisAIFile = AegisAIFile(file.name, contentType, file, null)

        @JvmStatic
        fun of(name: String, bytes: ByteArray, contentType: String): AegisAIFile = AegisAIFile(name, contentType, null, bytes)
    }
}

/** 异步任务（服务端 `aegis.job`）。 */
@Serializable
data class AegisAIJob(
    val id: String = "",
    val endpoint: String = "",
    /** `in_progress` / `completed` / `failed` / `canceled`。 */
    val status: String = "",
    @SerialName("created_at") val createdAt: Long = 0,
    @SerialName("completed_at") val completedAt: Long? = null,
    @SerialName("elapsed_ms") val elapsedMs: Long = 0,
    /** 完成时的 JSON 结果，与同步调用的响应体相同。 */
    val result: JsonObject? = null,
    @SerialName("content_type") val contentType: String? = null,
    /** 二进制结果的地址（相对站点根），用 [AegisAIRelay.jobContent] 取。 */
    @SerialName("result_url") val resultUrl: String? = null,
    val error: AegisAIJobError? = null,
    @SerialName("poll_after_ms") val pollAfterMs: Int? = null,
    /**
     * 提交时为真：这次提交的幂等键已经有过一次提交，网关回的是那一次建立的任务（没有新建、没有再扣费）。
     * 只在 [AegisAIRelay.submitJob] 的返回值上有意义。
     */
    @kotlinx.serialization.Transient val replayed: Boolean = false,
) {
    val isFinished: Boolean get() = status == COMPLETED || status == FAILED || status == CANCELED

    internal fun failure(): AegisException {
        if (status == CANCELED) return AegisException.canceled()
        val error = error
        return AegisException.relay(error?.aegisCode ?: 0, error?.message.orEmpty(), error?.status ?: 502)
    }

    companion object {
        const val IN_PROGRESS = "in_progress"
        const val COMPLETED = "completed"
        const val FAILED = "failed"
        const val CANCELED = "canceled"
    }
}

@Serializable
data class AegisAIJobError(
    val message: String = "",
    val type: String = "",
    val code: String = "",
    @SerialName("aegis_code") val aegisCode: Int? = null,
    val status: Int = 0,
)

/** 生图 / 改图的结果。 */
class AegisAIImages(
    val requestNo: String,
    val images: List<AegisAIImage>,
    /** 上游报回的用量（gpt-image 系列有），原样。 */
    val usage: JsonObject?,
    /** 完整响应，原样。 */
    val raw: JsonObject,
) {
    internal companion object {
        fun from(job: AegisAIJob, json: Json): AegisAIImages {
            val raw = job.result ?: throw AegisException.relay(0, "任务没有返回图片", 502)
            val images = (raw["data"]?.let { runCatching { it.jsonArray }.getOrNull() } ?: emptyList<JsonElement>())
                .mapNotNull { element ->
                    val item = element as? JsonObject ?: return@mapNotNull null
                    val b64 = (item["b64_json"] as? JsonPrimitive)?.contentOrNull
                    val url = (item["url"] as? JsonPrimitive)?.contentOrNull
                    if (b64.isNullOrBlank() && url.isNullOrBlank()) null
                    else AegisAIImage(b64?.takeIf { it.isNotBlank() }, url?.takeIf { it.isNotBlank() },
                        (item["revised_prompt"] as? JsonPrimitive)?.contentOrNull)
                }
            if (images.isEmpty()) throw AegisException.relay(0, "上游没有返回图片", 502)
            return AegisAIImages(job.id, images, raw["usage"] as? JsonObject, raw)
        }
    }
}

/** 一张图：base64 或临时链接（链接通常一小时内失效，要尽快下载）。 */
class AegisAIImage(val b64Json: String?, val url: String?, val revisedPrompt: String?) {
    /** 解出 base64 的图片字节；只有链接时为 null。 */
    fun bytes(): ByteArray? = b64Json?.decodeBase64()?.toByteArray()
}
