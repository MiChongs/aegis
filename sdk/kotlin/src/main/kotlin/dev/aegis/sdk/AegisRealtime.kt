package dev.aegis.sdk

import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlin.math.min
import kotlin.math.pow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

/**
 * 服务端推给当前用户的一条实时事件。
 *
 * 与服务端 `internal/domain/realtime.Event` 同构。[data] 的形状随 [type] 变，
 * 因此保持为 JsonObject —— 由调用方按自己关心的事件解析。
 */
@Serializable
data class AegisRealtimeEvent(
    val id: String = "",
    val type: String = "",
    @SerialName("appid") val appId: Long = 0,
    val userId: Long = 0,
    /** RFC 3339 时间串。 */
    val timestamp: String = "",
    val data: JsonObject? = null,
)

/** 实时连接的状态回调。所有回调都在 OkHttp 的 WebSocket 线程上执行。 */
interface AegisRealtimeListener {
    /** 握手完成。重连成功后会再次触发。 */
    fun onOpen() {}

    fun onEvent(event: AegisRealtimeEvent) {}

    /**
     * 连接断开。[willReconnect] 为 false 表示不会再自动重连
     * （调用方主动关闭，或退避次数已用尽）。
     */
    fun onClosed(code: Int, reason: String, willReconnect: Boolean) {}

    /** 握手或传输失败。[willReconnect] 含义同上。 */
    fun onFailure(error: Throwable, willReconnect: Boolean) {}
}

/** 一条实时连接的句柄。 */
interface AegisRealtimeConnection {
    val isConnected: Boolean

    /**
     * 发一个应用层 ping，服务端回一条 `system.pong` 事件。
     *
     * 保活本身不需要它 —— 协议级 ping/pong 由 OkHttp 与服务端自动完成。
     * 它的用处是主动确认这条连接还能双向通信（协议级 pong 由系统栈回复，
     * 证明不了服务端的读循环还活着）。
     */
    fun ping()

    /** 主动关闭，不再重连。 */
    fun close()
}

/**
 * 实时通道。
 *
 * 服务端在 `GET /api/ws` 上做 WebSocket 升级，鉴权认 `Authorization: Bearer`
 * 或 `aegis.jwt.<token>` 子协议；连接建立后单向推送 JSON 事件，
 * 客户端唯一能发的是 `{"type":"ping"}`。
 *
 * **这条路径不在 `/api/v1/apps/{appKey}` 网关命名空间下**，因此不受三档安全等级
 * 的包装规格约束 —— 与 OAuth 回跳同理，sealed 档下它也是明文的（TLS 之外无额外加密）。
 *
 * 传输用 OkHttp 自带的 WebSocket 实现，不自行拼协议帧。
 */
class AegisRealtimeApi internal constructor(
    private val client: AegisClient,
    baseHttp: OkHttpClient,
) {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    /**
     * 主动发协议级 ping，间隔比服务端的 25s 略长。
     *
     * 服务端本来就会 ping，OkHttp 也会自动回 pong，所以保活不靠这个。它解决的是
     * 另一半问题：链路半开时（NAT 超时、移动网络切换）客户端不发包就永远不知道
     * 对端已经没了，会一直以为自己在线。有了它，一个 ping 收不到 pong 就触发
     * onFailure，进而走重连。
     */
    private val http: OkHttpClient = baseHttp.newBuilder()
        .pingInterval(30, TimeUnit.SECONDS)
        // WebSocket 是长连接，读超时必须关掉，否则会被当成空闲连接掐断。
        .readTimeout(0, TimeUnit.SECONDS)
        .build()

    /**
     * 建立连接并开始接收事件。
     *
     * 需要已登录：token 取自 tokenStore，取不到会立刻抛 40100。
     *
     * @param autoReconnect 断线后是否自动重连（指数退避，上限 [maxReconnectDelaySeconds]）。
     * @param maxReconnectAttempts 连续失败多少次后放弃；0 表示不限次数。
     */
    @JvmOverloads
    fun connect(
        listener: AegisRealtimeListener,
        autoReconnect: Boolean = true,
        maxReconnectAttempts: Int = 0,
        maxReconnectDelaySeconds: Long = 60,
    ): AegisRealtimeConnection {
        val token = client.tokens.accessToken()
            ?: throw AegisException.fromCode(40100, "尚未登录：实时连接需要访问令牌")
        val connection = ReconnectingConnection(
            listener = listener,
            autoReconnect = autoReconnect,
            maxAttempts = maxReconnectAttempts,
            maxDelaySeconds = maxReconnectDelaySeconds,
        )
        connection.open(token)
        return connection
    }

    private fun request(token: String): Request = Request.Builder()
        .url(client.realtimeUrl())
        // OkHttp 能设任意请求头，所以用 Authorization 这条更直白的路子。
        // 子协议那条（aegis.jwt.<token>）是给浏览器留的 —— 浏览器的 WebSocket
        // API 不允许自定义请求头，只能把令牌塞进子协议名里。
        .header("Authorization", "Bearer $token")
        .build()

    private inner class ReconnectingConnection(
        private val listener: AegisRealtimeListener,
        private val autoReconnect: Boolean,
        private val maxAttempts: Int,
        private val maxDelaySeconds: Long,
    ) : AegisRealtimeConnection, WebSocketListener() {

        private val socket = AtomicReference<WebSocket?>(null)
        private val closedByCaller = AtomicBoolean(false)
        private val attempts = AtomicInteger(0)
        private val open = AtomicBoolean(false)

        override val isConnected: Boolean get() = open.get()

        fun open(token: String) {
            socket.set(http.newWebSocket(request(token), this))
        }

        override fun ping() {
            socket.get()?.send("""{"type":"ping"}""")
        }

        override fun close() {
            closedByCaller.set(true)
            open.set(false)
            // 1000 = 正常关闭。服务端把它当作预期内的断开，不记异常日志。
            socket.getAndSet(null)?.close(1000, "client closed")
        }

        override fun onOpen(webSocket: WebSocket, response: Response) {
            open.set(true)
            attempts.set(0)
            listener.onOpen()
        }

        override fun onMessage(webSocket: WebSocket, text: String) {
            val event = runCatching {
                json.decodeFromString(AegisRealtimeEvent.serializer(), text)
            }.getOrNull() ?: return // 解不出来的帧直接丢弃，一条坏帧不该断开整条连接
            listener.onEvent(event)
        }

        override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
            webSocket.close(code, reason)
        }

        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
            open.set(false)
            val retry = scheduleReconnect()
            listener.onClosed(code, reason, retry)
        }

        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
            open.set(false)
            // 令牌过期时先刷新再重连：否则退避重试只会用同一个过期令牌一直撞 401。
            if (response?.code == 401) {
                runCatching { client.auth.refresh() }
            }
            val retry = scheduleReconnect()
            listener.onFailure(t, retry)
        }

        /**
         * 指数退避重连。返回是否真的会重连。
         *
         * 退避是必需的：服务端重启时所有客户端会在同一刻掉线，固定间隔重连等于
         * 让它们排着队一起回来，正好砸在最脆弱的时候。
         */
        private fun scheduleReconnect(): Boolean {
            if (!autoReconnect || closedByCaller.get()) return false
            val attempt = attempts.incrementAndGet()
            if (maxAttempts > 0 && attempt > maxAttempts) return false
            val token = client.tokens.accessToken() ?: return false

            val delaySeconds = min(2.0.pow(attempt - 1).toLong(), maxDelaySeconds)
            val worker = Thread {
                try {
                    TimeUnit.SECONDS.sleep(delaySeconds)
                } catch (interrupted: InterruptedException) {
                    Thread.currentThread().interrupt()
                    return@Thread
                }
                if (!closedByCaller.get()) {
                    open(token)
                }
            }
            worker.isDaemon = true
            worker.name = "aegis-realtime-reconnect"
            worker.start()
            return true
        }
    }
}
