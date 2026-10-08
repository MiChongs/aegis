package dev.aegis.sdk

import java.net.URLEncoder
import okhttp3.Interceptor
import okhttp3.Response

/**
 * 客户端设备信息，随每个请求以 `X-Device-*` 请求头上报。
 *
 * 服务端把型号按设备字典翻译成可读名称，并在会话列表、登录记录、扫码登录的发起端里以
 * `deviceInfo` 给出（厂商图标、设备图、系统与 App 版本等），用户据此区分自己的多台设备。
 * 除 [id] 在应用开启「登录设备检查」时必填外，其余都只用于展示。
 *
 * **[model] 报原始型号**（Android `Build.MODEL`、iOS 机器标识如 `iPhone14,3`），不要报营销名：
 * 字典以原始型号为键，报营销名会让每一次查询都落空。
 *
 * ```kotlin
 * AegisClient.builder(baseUrl, appKey)
 *     .device { AegisDevice.android(id = myStableDeviceId, appVersion = BuildConfig.VERSION_NAME) }
 *     .build()
 * ```
 */
data class AegisDevice @JvmOverloads constructor(
    /** 稳定的设备唯一码 → `X-Device-Id` */
    val id: String? = null,
    /** 原始型号 → `X-Device-Name` */
    val model: String? = null,
    /** android / ios / harmonyos / windows / macos / linux / web → `X-Device-Platform` */
    val platform: String? = null,
    /** 厂商（`Build.MANUFACTURER`）→ `X-Device-Manufacturer` */
    val manufacturer: String? = null,
    /** 品牌（`Build.BRAND`）→ `X-Device-Brand` */
    val brand: String? = null,
    /** 设备代号（`Build.DEVICE`），型号查不到时作为字典的第二个键 → `X-Device-Codename` */
    val codename: String? = null,
    /** 系统名 → `X-Device-OS` */
    val os: String? = null,
    /** 系统版本（`Build.VERSION.RELEASE`）→ `X-Device-OS-Version` */
    val osVersion: String? = null,
    /** 客户端应用版本 → `X-App-Version` */
    val appVersion: String? = null,
) {

    /** 要附加的请求头。空值不发；非 ASCII 的值按 UTF-8 百分号编码（HTTP 头只能放 ASCII）。 */
    fun headers(): Map<String, String> = linkedMapOf(
        HEADER_ID to id,
        HEADER_MODEL to model,
        HEADER_PLATFORM to platform,
        HEADER_MANUFACTURER to manufacturer,
        HEADER_BRAND to brand,
        HEADER_CODENAME to codename,
        HEADER_OS to os,
        HEADER_OS_VERSION to osVersion,
        HEADER_APP_VERSION to appVersion,
    ).mapNotNull { (name, value) ->
        value?.trim()?.takeIf { it.isNotEmpty() }?.let { name to headerSafe(it) }
    }.toMap()

    companion object {
        const val HEADER_ID = "X-Device-Id"
        const val HEADER_MODEL = "X-Device-Name"
        const val HEADER_PLATFORM = "X-Device-Platform"
        const val HEADER_MANUFACTURER = "X-Device-Manufacturer"
        const val HEADER_BRAND = "X-Device-Brand"
        const val HEADER_CODENAME = "X-Device-Codename"
        const val HEADER_OS = "X-Device-OS"
        const val HEADER_OS_VERSION = "X-Device-OS-Version"
        const val HEADER_APP_VERSION = "X-App-Version"

        /**
         * 从 `android.os.Build` 读出本机信息。
         *
         * SDK 是纯 JVM 库、不依赖 Android，这里经反射读取；不在 Android 上运行时
         * 这些字段为空，只剩传入的 [id] 与 [appVersion]。
         */
        @JvmStatic
        @JvmOverloads
        fun android(id: String?, appVersion: String? = null): AegisDevice {
            val model = buildField("MODEL")
            return AegisDevice(
                id = id,
                model = model ?: buildField("DEVICE"),
                platform = "android",
                manufacturer = buildField("MANUFACTURER"),
                brand = buildField("BRAND"),
                codename = buildField("DEVICE"),
                os = "Android",
                osVersion = buildVersionField("RELEASE"),
                appVersion = appVersion,
            )
        }

        private fun buildField(name: String): String? = runCatching {
            Class.forName("android.os.Build").getField(name).get(null) as? String
        }.getOrNull()?.trim()?.takeIf { it.isNotEmpty() && it != "unknown" }

        private fun buildVersionField(name: String): String? = runCatching {
            Class.forName("android.os.Build\$VERSION").getField(name).get(null) as? String
        }.getOrNull()?.trim()?.takeIf { it.isNotEmpty() }

        /** 只含可见 ASCII 时原样发送，否则整体按 UTF-8 百分号编码（服务端据 `%` 解码）。 */
        internal fun headerSafe(value: String): String =
            if (value.all { it in ' '..'~' } && !value.contains('%')) value
            else URLEncoder.encode(value, "UTF-8").replace("+", "%20")
    }
}

/**
 * 给每个请求附加设备请求头。请求上已经显式带了的同名头不覆盖。
 * [provider] 每次请求调用一次，可以惰性取值（设备 ID 首次解析可能较慢）。
 */
internal class AegisDeviceInterceptor(private val provider: () -> AegisDevice?) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val device = runCatching(provider).getOrNull() ?: return chain.proceed(request)
        val builder = request.newBuilder()
        device.headers().forEach { (name, value) ->
            if (request.header(name) == null) builder.header(name, value)
        }
        return chain.proceed(builder.build())
    }
}
