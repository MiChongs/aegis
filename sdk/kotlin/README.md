# Aegis Kotlin / Java SDK

面向 **Android 与 JVM 服务端** 的官方客户端。同一份产物两边通用：

| 场景 | 安全等级 | 说明 |
|---|---|---|
| Android / 桌面客户端 | `standard` | 只要 HTTPS。**不要**把 `appSecret` 打进 APK |
| Java / Kotlin 服务端 | `signed` / `sealed` | `appSecret` 放在自己的服务端 |

纯 JVM 实现，不依赖 Android SDK、不需要 NDK，也不受「`XDH` 要 API 33+」的限制。

## 依赖

```kotlin
dependencies {
    implementation("dev.aegis:aegis-sdk:1.0.0")
}
```

传递依赖：OkHttp 4、kotlinx-serialization-json、BouncyCastle（`bcprov-jdk18on`）。

Android 上记得保留 BouncyCastle 与序列化器：

```proguard
-keep class org.bouncycastle.** { *; }
-keepclassmembers class dev.aegis.sdk.** { *** Companion; }
-keepclasseswithmembers class dev.aegis.sdk.** { kotlinx.serialization.KSerializer serializer(...); }
```

## 三行接入

```kotlin
val client = AegisClient.builder("https://api.example.com", "demo_app").build()
val session = client.auth.loginWithPassword("alice", "secret")
val profile = client.me.profile()
```

客户端**只硬编码 baseUrl 与 appKey**。安全等级、可用登录方式、注册表单字段、
接口路径、错误码分类全部从 `/config` 读 —— 管理员在控制台改了配置，
客户端下次拉 config 就跟上，不必发版。

Java 调用方式完全一致：

```java
AegisClient client = AegisClient.builder("https://api.example.com", "demo_app").build();
AegisSession session = client.getAuth().loginWithPassword("alice", "secret");
JsonElement profile = client.getMe().profile();
```

## 升档只换一层

三档安全等级共用同一批路径、同一份请求体与同一份响应体，**业务代码一行不动**：

```kotlin
// standard —— 移动端就停在这里
AegisClient.builder(baseUrl, appKey).build()

// signed / sealed —— 只多一个 appSecret，别的都不变
AegisClient.builder(baseUrl, appKey).appSecret(System.getenv("AEGIS_APP_SECRET")).build()
```

`appSecret` 是真正的密钥，**只能放在自己的服务端**。移动端与前端没有安全的地方存它，
请把应用留在 standard 档 —— 那一档的安全性由 HTTPS 提供，并不比「把密钥硬编码进 APK」差。

## SDK 替你处理掉的三件事

1. **时钟漂移**。signed / sealed 档最常见的线上故障是设备时钟偏差超过 5 分钟后一路
   `40071`，而用户只看到「登录失败」。SDK 从 `/config` 的 `serverTime` 学到偏移量，
   之后所有请求都用校准后的时间戳（见 `AegisClock`）。
2. **传输密钥轮换**。sealed 档的服务端公钥会轮换，旧钥最多保留 24 小时。
   收到 `40074` 时 SDK 自动重拉一次 `/config` 并重试一次 —— 只重试一次，
   无限重试只会把一个明确的失败拖成一串超时。
3. **令牌过期**。`40100` 且本地有 `refreshToken` 时自动刷新并重试一次。

## 错误分类

所有失败都是 `AegisException`，按**调用方该怎么办**分类，而不是按 HTTP 状态码：

| kind | 含义 | 该怎么办 |
|---|---|---|
| `TRANSPORT` | 包装方式不对（签名 / 加密 / 时钟 / nonce） | 接入期的问题，照 `hint` 改 |
| `AUTH` | 令牌过期或不属于本应用 | 刷新令牌或回登录页 |
| `BUSINESS` | 服务端按业务规则拒绝 | 把 `message` 原样显示给用户 |
| `NETWORK` | 连不上 / 超时 | 可以重试 |

把 TRANSPORT 与 BUSINESS 混在一起是接入期最费时间的一件事 ——
「签名算错了」和「密码输错了」都返回 400，不分类的话每次都得人肉看 code。

```kotlin
try {
    client.auth.loginWithPassword(account, password)
} catch (error: AegisException) {
    when (error.kind) {
        AegisException.Kind.BUSINESS -> showToast(error.message)
        AegisException.Kind.AUTH -> goToLogin()
        AegisException.Kind.NETWORK -> showRetry()
        AegisException.Kind.TRANSPORT -> log("接入配置有误：${error.hint}")
    }
}
```

## 二次认证

登录返回 `requiresSecondFactor = true` 时 **`accessToken` 是空的**，会话还没建立。
把这一步当成「登录成功」是最常见的接入错误：

```kotlin
val first = client.auth.loginWithPassword(account, password)
val session = if (first.requiresSecondFactor) {
    client.auth.verifySecondFactor(first.challenge!!.challengeId, code = userInput)
} else {
    first
}
if (session.passwordChangeRequired) goToChangePassword()
```

## 注册表单要动态渲染

```kotlin
val schema = client.config().auth.registrationSchema
// 按 schema 渲染输入框，再把值放进 profile 提交
client.auth.register(account = a, password = p, profile = mapOf("company" to "Aegis Labs"))
```

**不要硬编码注册表单**，也不要硬编码第三方登录渠道列表
（读 `config.auth.oauthProviders`）—— 否则管理员改一次配置就得发一次版。

## 令牌存储

默认在内存里。Android 上换成加密存储：

```kotlin
class PrefsTokenStore(context: Context) : AegisTokenStore {
    private val prefs = EncryptedSharedPreferences.create(/* ... */)
    override fun accessToken() = prefs.getString("access", null)
    override fun refreshToken() = prefs.getString("refresh", null)
    override fun save(accessToken: String, refreshToken: String) =
        prefs.edit().putString("access", accessToken).putString("refresh", refreshToken).apply()
    override fun clear() = prefs.edit().clear().apply()
}

AegisClient.builder(baseUrl, appKey).tokenStore(PrefsTokenStore(context)).build()
```

## 线程模型

所有方法都是**同步**的，SDK 不替调用方决定并发模型。
Android 上放进协程 / IO 线程；服务端直接调。
`AegisClient` 线程安全，全进程共用一个实例即可（内部 OkHttpClient 自带连接池）。

## 服务端校验会员（AegisServerClient）

`AegisClient` 是**终端用户**的客户端：凭据是登录换来的用户令牌。
接入方**自己的后端**要问「这个用户是不是会员」时两样都没有 ——
它没有用户令牌（那是用户的东西），也不该配管理员账号（那是整个租户的权限）。
这条路由 `AegisServerClient` 走：

```kotlin
val server = AegisServerClient.builder(baseUrl, appKey, "afk_xxx").build()

// userToken 是客户端调你的接口时带上来的那个 aegis 访问令牌，原样转过来即可
if (!server.isMember(userToken)) return deny()                     // 只问是不是会员
if (!server.hasFeature(userToken, "export")) return deny()         // 问的是"他能不能用导出"

// 要拿字段时用完整版
val check = server.verifyMembership(userToken, feature = "export")
check.membership.isTrial          // 是不是试用会员 —— 决定引导"升级"还是"续费"
check.membership.remainingDays    // 还剩几天
check.membership.features         // 当前生效的全部功能标识
```

**被校验的用户由访问令牌指明，不能传 userId。** 这不是少做了一种便利：

> 你的后端几乎一定会把「当前请求是谁」交给它自己的客户端来说。一旦这个接口收
> userId，那条链路就是「客户端自报 42 → 你转发 42 → 我们回答 42 是会员 →
> 你放行**发起请求的那个人**」。攻击者只要知道任意一个会员的 userId 就能白嫖，
> 而服务端密钥拦不住这件事 —— 犯错的正是持有密钥的那一方。

令牌则是平台签发、平台验证的：它同时证明了「是谁」和「这个人现在在场」。
需要按 userId 批量查（对账、到期提醒、客服工单）走管理端
`/api/admin/apps/{appKey}/vip/entitlement`，那条路有管理员鉴权与审计。

| | `AegisClient` | `AegisServerClient` |
|---|---|---|
| 调用方 | 终端用户的 App / 网页 | 接入方自己的后端 |
| 凭据 | 用户令牌 | 应用服务端密钥 `afk_…`（控制台「远程函数 → 调用密钥」签发） |
| 命名空间 | `/api/v1/apps/{appKey}/*` | `/api/apps/{appKey}/*` |
| 包装 | 按安全等级三档 | 纯 JSON |

> **`afk_…` 只能放在服务器上。** 打进 APK 或前端包等于把它公开：
> 谁拿到它都能问出任意用户的会员状态。要求与 `appSecret` 同档。

功能标识必须先在控制台的**会员功能目录**里登记，再勾进套餐。
传一个没登记的标识会得到 `40486` 而不是静默的 `false` ——
拼错一个字母（`exprot`）和"他没有这项权益"是完全不同的两件事，
而后者在自由字符串方案里永远查不出来。

## 目录里没封的接口

类型化门面覆盖了 `/config` 目录里的全部接口。要调新增的（或自定义的）接口：

```kotlin
val data = client.call("POST", "/some/new/path", mapOf("k" to "v"), requireAuth = true)
```

路径与参数照 `client.config().operations` 抄 —— 那是服务端下发的机器可读目录。

## 协议规格

包装规格的逐字节定义在 `AegisCanonical.kt`，是整个 SDK 里**唯一**允许拼接协议字符串的地方。
它与服务端的 `internal/service/auth_protocol_service.go` 由两侧的
`AegisCanonicalTest` / `auth_protocol_canonical_test.go` 钉在同一批字面量上，
任何一方改了拼接方式都会当场红掉。

完整协议说明见 [docs/app-integration.md](../../docs/app-integration.md)。

## 构建

```bash
cd sdk/kotlin
gradle build     # 或 ./gradlew build
gradle test
```
