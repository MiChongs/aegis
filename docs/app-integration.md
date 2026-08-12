# Aegis 应用接入协议（App Protocol v1）

面向**接入方**的完整规格。控制台侧的配置说明见「应用 → 接入」页；
可直接运行的多语言示例在公开门户 `/developers`。

## 设计约束

接入方只需要认识一个命名空间：

```
/api/v1/apps/{appKey}/...
```

`appKey` 是公开标识符，可以安全地放进客户端；它不是密钥，也不参与任何密钥派生。

三档安全等级 **standard / signed / sealed** 共用同一批路径、同一份请求体与
同一份响应体。等级只决定请求**怎么包装**，因此客户端升档时只替换一层
transport 适配器，业务代码一行不动。

| 等级 | 客户端要做的事 | 适用场景 |
|---|---|---|
| `standard`（默认） | HTTPS + JSON。无密钥、无握手、无密码学库 | 绝大多数 App / Web / 小程序 |
| `signed` | 额外算一个 HMAC-SHA256 请求签名 | 有自己服务端，要防篡改与重放 |
| `sealed` | 在 signed 之上再叠端到端加密载荷 | 强合规 / 抗中间人解包 |

等级是**累加**的：`sealed = signed + 载荷加密`。

> **为什么 sealed 仍要签名**：AEAD 只证明"这段密文没被改过"。服务端公钥是公开的，
> 任何人都能生成临时密钥对造出一段合法密文。签名才证明"调用方确实持有 appSecret"。
> 两者相加才同时得到机密性与调用方身份。

## 官方 SDK

不想自己实现 transport 的话，直接用官方 Kotlin / Java SDK
（[sdk/kotlin](../sdk/kotlin)，Android 与 JVM 服务端共用一份产物）：

```kotlin
val client = AegisClient.builder("https://api.example.com", "demo_app").build()
val session = client.auth.loginWithPassword("alice", "secret")
val profile = client.me.profile()
```

它已经处理了本文档里最容易出错的三件事：**时钟校准**（从 `/config` 的 `serverTime`
学偏移量）、**传输密钥轮换**（`40074` 自动重拉 config 重试一次）、**令牌刷新**。
其余语言按本文档手写即可，规格逐字节写明。

## 接口清单

全部位于 `/api/v1/apps/{appKey}` 之下。**完整清单以 `/config` 的 `operations`
字段为准**（机器可读，带方法、是否需要 Bearer、是否是上传），下面按能力分组列出：

### 认证生命周期

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/config` | 应用能力与当前等级规格；**任何等级下都免包装可读** |
| `POST` | `/captcha` | 按策略签发图形验证码 |
| `POST` | `/auth/sms/code` | 申请短信验证码（`purpose`: `login` \| `register`） |
| `POST` | `/auth/register` | 注册（`method`: `password` \| `sms`） |
| `POST` | `/auth/login` | 登录（`method`: `password` \| `sms`） |
| `POST` | `/auth/refresh` | 刷新访问令牌 |
| `POST` | `/auth/logout` | 注销当前会话（Bearer） |
| `POST` | `/auth/2fa/verify` | 完成二次认证挑战 |
| `POST` | `/auth/oauth/url` | 取第三方登录授权地址 |
| `GET` | `/auth/oauth/callback` | 第三方授权回跳落点；**免包装** |
| `POST` | `/auth/oauth/exchange` | 原生 SDK 用第三方 profile 换会话 |
| `POST` | `/auth/oauth/bind/url` | 取第三方账号绑定授权地址（Bearer） |
| `GET` `DELETE` | `/auth/oauth/bindings[/{provider}]` | 查询 / 解绑第三方账号（Bearer） |
| `POST` | `/auth/email/code`、`/auth/email/verify` | 邮箱验证码 |
| `POST` | `/auth/password/forgot`、`/auth/password/reset/verify` | 找回密码 |
| `POST` | `/auth/password/verify`、`/auth/password/change` | 校验 / 修改密码（Bearer） |
| `POST` | `/auth/passkey/options`、`/auth/passkey/login` | Passkey 登录 |

### 当前用户（均需 Bearer）

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/me` | 当前登录用户资料 |
| `GET` `PUT` | `/me/profile` | 个人资料读写 |
| `POST` | `/me/profile/changes/confirm` | 确认敏感资料变更 |
| `POST` `DELETE` | `/me/avatar` | 上传 / 移除头像（上传为 `multipart/form-data`，可带 `crop_*` 裁剪框） |
| `GET` `POST` | `/me/avatar/history`、`/me/avatar/restore` | 头像历史与恢复上一张 |
| `GET` `PUT` | `/me/settings` | 用户设置 |
| `GET` | `/me/security` | 账户安全概览 |
| `POST` | `/me/2fa/totp/{enroll,enable,disable}` | TOTP 绑定 / 启用 / 关闭 |
| `GET` `POST` | `/me/2fa/recovery-codes[/regenerate]` | 恢复码 |
| `GET` `POST` `DELETE` | `/me/passkeys[/options\|/{credentialId}]` | Passkey 管理 |
| `GET` `DELETE` `POST` | `/me/sessions[/{tokenHash}\|/revoke-all]` | 会话管理 |
| `GET` | `/me/audits/login`、`/me/audits/sessions` | 我的登录 / 会话记录 |

### 运营能力（均需 Bearer）

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` `POST` | `/signin/status`、`/signin`、`/signin/history` | 签到 |
| `GET` | `/points/*` | 积分 / 经验 / 等级与流水 |
| `GET` | `/leaderboard/*` | 排行榜 |
| `GET` `POST` `DELETE` | `/notifications/*` | 站内信 |
| `GET` `POST` | `/wallet/*`、`/vip/*`、`/pay/orders*` | 钱包 / 会员 / 支付（含试用，见下节） |
| `POST` | `/storage/upload`（`multipart`）、`/storage/object-link` | 存储 |
| `GET` `POST` | `/tickets/*` | 工单自助 |

### 会员与试用（`/vip/*`）

判断「用户是不是会员」只有一个接口：`GET /vip/status`。它回答的不只是是/否：

```jsonc
{
  "isVip": true,
  "isTrial": true,                 // 当前这段会员期是试用给的
  "source": "trial",               // none / trial / wallet / payment_order / admin_grant / unknown
  "planName": "7 天试用",
  "expireAt": "2026-03-08T12:00:00Z",
  "remainingSeconds": 518400,
  "remainingDays": 6,
  "trial": {                       // 领取过才有；过期后仍保留，用于把"免费试用"换成"续费"
    "active": true, "claimedAt": "...", "endsAt": "...", "durationDays": 7,
    "planId": 7, "planName": "7 天试用", "remainingSeconds": 518400
  },
  "trialOffer": {                  // 现在能不能领
    "available": false,
    "reason": "already_claimed",   // eligible / not_configured / already_claimed /
                                   // member_active / device_claimed / device_required
    "message": "试用资格已使用",
    "planId": 7, "planName": "7 天试用", "durationDays": 7
  }
}
```

**入口显隐读 `trialOffer.available`，文案分支读 `reason`**（不要匹配 `message`，
那是会随文案调整变化的中文）。领取：

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/vip/trial` | 领取试用，无请求体。资格由服务端判定，领哪个套餐也由服务端决定 |

- **一人一次**，没有幂等键 —— 唯一约束就是幂等键。仍在试用期内重复调用会原样返回
  上次结果（`replayed: true`），不会重复发放，也不会报错。
- 资格不足时返回业务码：`40373` 已领过 / `40374` 当前已是会员 /
  `40375` 该设备已领过 / `40040` 需要设备标识 / `40484` 应用未开放试用。
- **试用套餐不出现在 `/vip/plans` 里**：那是"能买什么"的列表，而试用不能买。
  它的套餐名、天数、描述都在 `trialOffer` 里。硬编码 planId 去买试用会被拒（`40376`）——
  它是 0 元的，走购买链路等于绕开全部资格判定。
- 试用期内购买正式套餐是**顺延**：剩余试用天数叠加到付费时长上。

官方 Kotlin SDK：`api.vipStatus()` / `api.claimVipTrial()`。

`/vip/status` 里还有一项 `features`：当前生效的**功能标识**（见下节）。
两档会员（基础版能导出、高级版还能用 AI）时按它决定界面上哪些入口可用，
不要拿 `planName` 做字符串比较 —— 那是运营随时会改的展示文案。

### 服务端校验会员（接入方后端调用）

上面那些接口的凭据都是**用户令牌**。接入方自己的服务器没有用户令牌，
也不该配管理员账号，于是只能相信客户端捎上来的那句「我是会员」——
而这句话客户端说了不算。服务端校验就是补这条路：

```http
POST /api/apps/{appKey}/vip/verify
X-Aegis-Function-Key: afk_xxxxxxxx          # 应用服务端密钥，控制台签发
Content-Type: application/json

{ "accessToken": "eyJhbGciOi...", "feature": "export" }
```

两样凭据各证明一件事，缺一不可：

| 凭据 | 证明 |
|---|---|
| `X-Aegis-Function-Key` | **谁在问** —— 只有你的后端持有 |
| `accessToken` | **问的是谁** —— 平台签发、平台验证 |

`accessToken` 就是用户登录后拿到的那个令牌，客户端调你的接口时带上来，你原样转发。
`feature` 留空即通用档（只问是不是会员）。

> **不接受 `userId` / `account`，这是刻意的。** 你的后端几乎一定会把
> 「当前请求是谁」交给它自己的客户端来说。一旦这个接口收 userId，那条链路就是
> 「客户端自报 42 → 你转发 42 → 我们回答 42 是会员 → 你放行**发起请求的那个人**」，
> 攻击者只要知道任意一个会员的 userId 就能白嫖 —— 而服务端密钥拦不住这件事，
> 因为犯错的正是持有密钥的那一方。
>
> 需要按 userId 批量查（对账、到期提醒、客服工单）走管理端
> `GET /api/admin/apps/{appKey}/vip/entitlement?userId=`，那条路有管理员鉴权与审计。

```jsonc
{
  "granted": true,                 // 放行只看这一个字段
  "matched": true,
  "userId": 42, "account": "zhangsan",
  "membership": {
    "isVip": true, "isTrial": false, "source": "wallet",
    "planName": "高级版", "expireAt": "2026-04-01T00:00:00Z",
    "remainingSeconds": 2592000, "remainingDays": 30,
    "features": ["ai.chat", "export"]
  },
  "feature": { "tag": "export", "name": "批量导出", "granted": true },
  "checkedAt": "2026-03-02T03:00:00Z"
}
```

- **密钥只放服务器**。`afk_…` 打进 APK 或前端包等于把它公开。要求与 `appSecret` 同档。
  它与远程函数调用共用同一把钥匙 —— 再造一套"会员校验专用密钥"只会让你在服务器上
  配两份凭据，而它们的信任级别完全一样。
- **令牌无效 / 过期返回 401（40100）**，令牌不属于该应用返回 403（40372）。
  这两种都不要当成"不是会员"处理：前者该让客户端刷新令牌，后者是接入配置错了。
- **缓存结论按 `checkedAt` 算 TTL**，别用本地时间 —— 两端时钟差会直接变成
  权益的提前失效或延后失效。

#### 功能标识（feature tag）

`feature` 传的不是套餐名，而是**功能标识**：先在控制台的会员功能目录里登记
（`export` / `ai.chat` 这类短标识），再勾进套餐。这样套餐改名、拆分、合并
都不影响判定，接入方代码里那句 `feature == "export"` 永远成立。

传一个没登记的标识会得到 `40486` 而不是静默的 `false` ——
拼错一个字母和「他没有这项权益」是完全不同的两件事，
而后者在自由字符串方案里永远查不出来。

用户当前的功能权益是**尚未到期的每一段开通的并集**：先买基础版再买高级版时
两段都没到期，两边的功能都生效；用完的那一段自动出局。
开通时会把套餐当时的功能列表**快照**进账本，因此运营明天改套餐配置，
不会让已经卖出去的会员当场少一项权益。

官方 Kotlin SDK：`AegisServerClient.verifyMembership(userId = 42, feature = "export")`，
详见 [sdk/kotlin/README.md](../sdk/kotlin/README.md#服务端校验会员aegisserverclient)。

### 免登录内容

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/banners`、`/notices` | 轮播图与公告 |
| `POST` | `/banners/{bannerId}/click` | 轮播图点击上报 |
| `GET` | `/version/check` | 版本检查（`versionCode` + `platform`） |

> 曝光由服务端在下发 `/banners` 时自己累加，**点击只有客户端知道**：
> 用户点开一条 Banner 时要显式调一次上报口。不调的表现是控制台上点击率恒为 0，
> 而那个数字是运营调整投放的唯一依据。

> 这一整套接口在网关命名空间下**共用同一套包装**。旧的 `/api/user/*`、
> `/api/points/*` 等命名空间仍然可用，但它们走的是另一套加密机制
> （`X-Aegis-Encrypted`），接入新客户端时不要混用两者。

管理端接口（管理员 Token + App 作用域授权）：

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/admin/apps/{appKey}/auth-protocol` | 读取接入配置 |
| `PUT` | `/api/admin/apps/{appKey}/auth-protocol` | 更新接入配置（含安全等级） |
| `POST` | `/api/admin/apps/{appKey}/auth-protocol/secret/rotate` | 轮换应用密钥，明文仅返回一次 |
| `POST` | `/api/admin/apps/{appKey}/auth-protocol/selftest` | 按当前等级实跑一遍接入链路 |
| `GET` | `/api/admin/apps/{appKey}/auth-protocol/transport/keys` | 列出传输公钥元数据 |
| `POST` | `/api/admin/apps/{appKey}/auth-protocol/transport/rotate` | 轮换传输密钥 |
| `DELETE` | `/api/admin/apps/{appKey}/auth-protocol/transport/keys/{keyId}` | 安全撤销传输密钥 |

## 1. 读取 /config

```http
GET /api/v1/apps/demo_app/config
```

响应可缓存 60 秒。关键字段：

- `auth.identifiers` —— 允许当账号用的标识类型；
- `auth.loginMethods` / `registerMethods` —— 启用的认证方式；
- `auth.registrationSchema` —— 客户端应动态渲染并提交的注册字段；
- `auth.captcha` —— **逐入口**给出是否必须先取图形验证码
  （`login` / `register` / `sms`，见下）；
- `auth.oauthProviders` —— 可用的第三方登录渠道；
- `security.level` —— 当前安全等级；
- `security.signature` —— 仅 signed / sealed 下发，含待签名字符串模板；
- `security.transport` —— 仅 sealed 下发，含 X25519 公钥与算法规格；
- `serverTime` —— 服务端时间（`unix` + `iso`），**客户端据此校准时钟**；
- `limits` —— 请求体上限、上传上限、允许的时钟偏差、nonce 长度区间；
- `endpoints` —— **完整相对路径清单**，客户端不需要自己拼 appKey；
- `operations` —— 带方法与鉴权要求的完整接口目录（机器可读）；
- `errors` —— 错误码目录，每项含 `recovery`（能不能自动重试、重试前要做什么）。

### `auth.captcha` —— 结论，不是配置

```json
"captcha": { "login": true, "register": false, "sms": true }
```

「要不要图形验证码」在服务端由**三处独立开关**共同决定，分属三个管理入口：

| 开关 | 归属 | 作用 |
|---|---|---|
| 接入策略的 `requireCaptcha` | 应用（控制台「接入协议」） | 强制要求，无视场景 |
| 验证码配置的 `requireForLogin` / `requireForRegister` | 应用（控制台「验证码」） | 分场景要求 |
| 平台验证码配置的短信 `requireCaptcha` | 平台（超管） | 发短信前的前置验证码（防轰炸） |

三者是**或**的关系。接入方不需要理解它们，也**不要**把这套判断复制到客户端 ——
复制就意味着管理员改了配置而客户端不跟着改。`auth.captcha` 给的是
「调这个接口要不要先取验证码」的直接答案，照做即可。

服务端的闸门与这里下发的结论出自同一个函数（`service.ResolveCaptchaRequirement`），
因此不存在「config 说不用、服务端却拒」的情况。

**`login` 与 `sms` 可能同时为 true**：那时短信登录需要两个验证码 ——
一个用于 `/auth/sms/code`，一个用于 `/auth/login`。验证码是一次性消费，
不能共用同一个 `captchaId`。

### 一定要用 serverTime 校准时钟

signed / sealed 档最常见的线上故障是**设备时钟偏差超过 5 分钟**后一路 `40071`，
而客户端自己不知道慢了多少 —— 用户只看到「登录失败」。

`/config` 免包装可读，是唯一能在「还没签成功」时拿到服务端时间的地方：

```text
offset = config.serverTime.unix - 本地 Unix 秒
之后所有请求的 X-Aegis-Timestamp = 本地 Unix 秒 + offset
```

官方 SDK 已内置（`AegisClock`）。手写客户端请务必做这一步，
移动端尤其容易遇到用户手动改过时间。

除下面两条**免包装路径**外，所有接口都必须按 `security.level` 包装：

| 路径 | 为什么免包装 |
|---|---|
| `/config` | 客户端得先知道自己该用哪一档，否则陷入「要读配置得先按配置包装」的死锁 |
| `/auth/oauth/callback` | 由第三方平台重定向浏览器发起，客户端没有任何机会给它签名或加密 |

两者都不接受请求体；回跳的越权与 CSRF 由 OAuth `state` 校验兜住。

## 1.5 认证方式

`auth.loginMethods` 与 `auth.registerMethods` 的可选集合刻意不同：

| 方式 | 登录 | 注册 | 说明 |
|---|:--:|:--:|---|
| `password` | ✅ | ✅ | `account` + `password` |
| `sms` | ✅ | ✅ | `phone` + `code` |
| `oauth` | ✅ | — | 走 `/auth/oauth/*` 三个接口 |

**第三方没有应用级注册开关**：某个渠道能否自动建号，由该渠道自己的
`allowRegister`（控制台「第三方登录」页）决定。若在这里再开一个 `oauth` 注册开关，
同一件事就有两处配置，接入方无从判断哪个生效。

启用 `sms` 时 `identifiers` 必须包含 `phone`，否则保存策略会被拒（`40086`）——
短信认证以手机号为身份，标识符里没有它客户端就拿不到可用的登录入口。

## 1.6 短信登录与注册

```http
POST /api/v1/apps/{appKey}/auth/sms/code
{ "purpose": "login", "phone": "13800138000" }
```

`purpose` 决定这串码只能用于登录还是注册，与后续 `/auth/login`、`/auth/register`
的校验用途一一对应 —— 拿注册码去登录会被拒，防止用途混用绕过策略。
应用开启了图形验证码时，还需一并带上 `captchaId` / `captchaAnswer`（防短信轰炸）。

```http
POST /api/v1/apps/{appKey}/auth/login
{ "method": "sms", "phone": "13800138000", "code": "123456" }
```

响应结构与密码登录完全一致。手机号尚未注册时：

- 应用启用了短信注册 → 自动建号并直接返回会话；
- 未启用 → `40394「该手机号尚未注册」`。

短信建号以手机号为账号且**不设密码**，这类账号只能靠短信登录，
想用密码登录需要走「设置密码」流程。

显式注册（需要额外收集昵称或自定义字段时）走 `/auth/register`，`method` 同样为 `sms`；
此时 `registrationSchema` 里的 `account` / `password` 两项自动跳过必填判定，
其余自定义字段规则与密码注册完全一致。

短信依赖控制台「验证码」页配置好的短信服务商；未配置时 `/auth/sms/code` 直接报错。

## 1.7 第三方登录

两条路径，按客户端形态选：

**Web / 系统浏览器跳转**

1. `POST /auth/oauth/url` 取授权地址 → 跳转；
2. 第三方授权后回跳 `GET /auth/oauth/callback?provider=..&code=..&state=..`，
   直接返回登录结果信封。

> ⚠️ 回跳无法被客户端包装，**即使应用处于 sealed 档它也是明文的**。
> 要求全链路加密的应用请改用下面的原生 SDK 方案。

**原生 SDK**

端上用微信 / QQ / Apple 等官方 SDK 完成授权，拿到 profile 后
`POST /auth/oauth/exchange` 换取 Aegis 会话。该路径受完整安全等级包装保护。

可用渠道读 `/config` 的 `auth.oauthProviders`；应用没启用 `oauth` 登录方式时
该数组为空。**不要在客户端硬编码渠道列表**，否则管理员改配置就得发版。

首次登录能否自动建号 = 渠道的 `allowLogin` ∧ `allowRegister`；
关闭时返回 `40393`，此时应引导用户先用已有账号登录再绑定。

## 2. standard 档

除 HTTPS 外没有任何额外要求。`X-Aegis-App-Key` 头是可选的，
路径上的 appKey 才是唯一事实来源；两者同时出现且不一致时请求会被拒绝（`40084`）。

```bash
curl -X POST "https://api.example.com/api/v1/apps/demo_app/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"account":"alice","password":"secret"}'
```

响应统一是 `{ code, message, data }` 信封，`code === 200` 表示成功。

## 3. signed 档

在 standard 之上增加四个请求头：

```http
X-Aegis-App-Key: {appKey}
X-Aegis-Timestamp: {Unix 秒级时间戳}
X-Aegis-Nonce: {随机值，8–128 字符}
X-Aegis-Signature: v2={hex(HMAC-SHA256(appSecret, canonical))}
```

待签名字符串 `canonical` 逐字节按下列格式构造，字段之间为 `\n`，**末尾没有换行**：

```text
aegis-hmac-sha256
{appKey}
{UPPERCASE_HTTP_METHOD}
{requestPath}
{rawQueryString}
{unixTimestampSeconds}
{nonce}
{sha256Hex(requestBodyBytes)}
```

- `requestPath` 不含 query string；
- **`rawQueryString` 是原样的查询串**（不含 `?`），不排序、不重新编码 ——
  签的就是放到线上的那串字节。**没有 query 时这一行是空行，不能省**：
  少一行，整串签名就全错了；
- `sha256Hex` 是请求体原始字节的 SHA-256 十六进制小写；空 body 也要算（即空串的哈希）；
- 时间戳与服务端相差超过 **5 分钟**会被拒绝（`40071`），先用 `serverTime` 校准；
- 同一个 nonce 在 5 分钟窗口内只能用一次（`40970`）。

> **v1 与 v2**：早期的 `v1=` 待签名字符串少 query 这一行，因此服务端**只在请求
> 没有 query 时**才接受它，带 query 的 v1 签名返回 `40176`。当年网关只有
> 「POST + JSON body」这一种形状，碰不到这个洞；接口面铺开到带分页、带筛选的 GET 之后，
> 不签 query 意味着 `?page=1` 能被中间人改成 `?page=999` 而签名照样通过。
> 新接入一律用 `v2=`；已有客户端不改也能继续跑，直到它们开始调带 query 的接口。

`appSecret` 由管理员在控制台轮换生成，格式为 `sk_` + 43 位 base64url。
**明文只在轮换响应里出现一次**，服务端只保留 AES-GCM 密文与提示串。

> appSecret 是真正的密钥，只能放在自己的服务端。移动端 / 前端场景请留在 standard 档。

## 4. sealed 档

在 signed 之上，把 JSON 请求体加密成 base64url 密文，再对**那串密文**签名。

算法套件固定为：

```text
x25519-xchacha20-poly1305
```

### 4.1 加密请求

每次请求都必须生成新的 X25519 临时密钥对和 24 字节随机 Nonce：

1. 用客户端临时私钥与 `/config` 下发的服务端公钥执行 X25519；
2. 构造请求 AAD；
3. 用 HKDF-SHA256 派生 32 字节请求密钥；
4. 用 XChaCha20-Poly1305 加密原始 JSON；
5. 密文经 base64url（无 padding）编码后作为整个 HTTP body。

请求 AAD 逐字节按下列格式构造，字段之间为 `\n`，末尾没有换行：

```text
aegis-transport-v2
{appKey}
{keyId}
{UPPERCASE_HTTP_METHOD}
{requestPath}
{unixTimestampSeconds}
{requestNonceBase64Url}
```

HKDF 参数：

```text
hash = SHA-256
IKM  = X25519 shared secret
salt = SHA-256("{appKey}:{keyId}")
info = 请求 AAD
L    = 32
```

> 盐取自公开的 `appKey` 而非内部数字主键：客户端手上本来就有 appKey，
> 不必再从 `/config` 里挖内部 ID，也就少了一处「数字还是字符串」对不齐的坑。

请求头：

```http
X-Aegis-Protocol: aegis-transport-v2
X-Aegis-App-Key: {appKey}
X-Aegis-Key-Id: {keyId}
X-Aegis-Client-Key: {客户端临时公钥 base64url}
X-Aegis-Timestamp: {Unix 秒级时间戳}
X-Aegis-Nonce: {24 字节随机 nonce 的 base64url}
X-Aegis-Signature: v2={对密文算出的 HMAC}
Content-Type: application/octet-stream
```

`X-Aegis-Nonce` 同时充当签名 nonce 与 XChaCha20 的 nonce，两处必须是同一个值。

请求 AAD **不含 query**：无请求体的方法把密文放在 query 里（见下），
把 query 算进 AAD 会变成「要加密得先知道密文」的死循环。query 的完整性
由 v2 签名保证 —— 密文本身就在被签的那串字节里。

### 4.1.1 无请求体的方法（GET / DELETE / HEAD）

HTTP 允许 GET 带 body，但 **OkHttp、URLSession 与浏览器 fetch 全都拒绝构造这种请求**
—— 恰好就是 Android / iOS / Web 三端。因此这类方法的密文走查询参数：

```text
明文 = 真正的 query string（如 "page=3&pageSize=20"）
密文 = base64url(XChaCha20-Poly1305(明文))
请求 = GET /api/v1/apps/{appKey}/me/sessions?_payload={密文}
```

参数名由 `/config` 的 `security.transport.payloadParam` 下发（当前是 `_payload`），
适用方法由 `bodylessMethods` 下发。

**没有查询参数要传时，`_payload` 是空串的密文** —— AEAD 对空明文照样产出 16 字节 tag，
于是「有没有参数」不构成分支，客户端一套代码走到底。漏带 `_payload` 会返回 `40078`。

签名照旧覆盖最终发出的字节：此时 body 为空，而 canonical 的 query 行是
`_payload={密文}`，因此密文本身也被签名保护，改不动。

### 4.1.2 文件上传

`multipart/form-data` 在三档下都可用：

- **standard / signed**：正常发 multipart，signed 档只是多算一个签名；
- **sealed**：把整个 multipart 请求体当作明文加密，密文照旧走 body，
  原始 Content-Type 由 `X-Aegis-Plain-Content-Type` 头带过去
  （头名由 `security.transport.plainContentTypeHeader` 下发）。

上传上限见 `/config` 的 `limits.maxUploadBytes`（默认 32 MiB），
其余请求见 `limits.maxRequestBytes`（默认 8 MiB）。

### 4.2 解密响应

响应体是 base64url 密文，`X-Aegis-Response-Nonce` 头带回响应 nonce。

```text
responseKey = SHA-256(requestKey ‖ "aegis-response-v2")
```

响应 AAD 为 6 行，`\n` 分隔，末尾无换行：

```text
aegis-transport-v2
{appKey}
{keyId}
{HTTP 状态码}
{requestNonceBase64Url}
{responseNonceBase64Url}
```

### 4.3 密钥轮换

`/config` 会同时下发 `active` 与短期兼容的 `retiring` 公钥。客户端必须按
`activeKeyId` 选取公钥，**不能固定保存单个公钥**；遇到 `40074` 时重新拉取
`/config`，且最多自动重试一次。旧密钥在轮换后最多保留 24 小时。

## 5. 错误码

| 业务码 | 含义 | 处理 |
|---|---|---|
| `40071` | 时间戳无效或过期 | 用 `/config` 的 `serverTime` 校准时钟，偏差需在 5 分钟内 |
| `40072` | nonce 格式无效 | signed 档 8–128 字符；sealed 档必须是 24 字节 base64url |
| `40073`–`40078` | 加密载荷构造有误 | 核对 AAD 拼接、HKDF 盐、临时公钥长度；无请求体的方法要把密文放进 `_payload` |
| `40084` | Header AppKey 与路由不一致 | 去掉头或改成与路径一致 |
| `40100` | 缺少或无效的访问令牌 | 用 refreshToken 换一次新令牌 |
| `40174` | 签名格式无效 | 必须是 `v2=` + 64 位十六进制 |
| `40175` | 签名校验失败 | 核对 canonical 的换行与字段顺序、确认用的是最新 appSecret |
| `40176` | 带 query 的请求用了 v1 签名 | 改用 v2：待签名字符串在 path 之后加一行原样 query |
| `40370` | 未启用该认证方式 | 在控制台的认证策略里勾选 |
| `40372` | 访问令牌不属于该应用 | 用本应用的登录结果换取令牌，不要跨应用复用令牌 |
| `40391` | 该渠道仅开放绑定，未开放直接登录 | 在渠道配置里打开「允许登录」 |
| `40393` | 第三方账号未绑定且渠道未开放自动注册 | 引导用户先用已有账号登录再绑定 |
| `40394` | 手机号尚未注册且应用未开放短信注册 | 在注册方式里勾选短信，或引导改用其他方式 |
| `40470` | 应用不存在或已停用 | 核对 appKey 与应用状态 |
| `40970` | nonce 已使用 | 每个请求换一个新的随机 nonce |
| `42670` | 该应用要求加密载荷 | 应用处于 sealed 档，不能发明文 |
| `50372` | 尚未签发应用密钥 | 让管理员在控制台轮换一次密钥 |

网关层错误（`4007x` / `4017x` / `42670` / `40970`）表示请求还没进到业务逻辑
就被拦下了 —— 这类问题一定出在包装方式上，不是账号密码不对。

## 6. 接入自检

管理员可在控制台点「运行接入自检」，或直接调用：

```http
POST /api/admin/apps/{appKey}/auth-protocol/selftest
{ "baseUrl": "https://api.example.com" }
```

服务端会用与本文档完全相同的规格，按当前等级实跑一遍
`/config → 密钥就绪 → 传输公钥 → 登录 → 只读 GET → 注册开关`，逐步返回结果与修复建议。

**有请求体与无请求体是两条不同的包装链路**，自检各跑一次：
登录走 body，`GET /me` 走 `?_payload=`。只跑其中一条会漏掉另一条的问题。

两次探测都使用随机到不可能存在的账号 / 不带令牌：只要服务端回的是**业务级**拒绝
（账号不存在、未认证），就证明「网关拆包 → Handler → 响应封包」整条链路都通。
**不会创建任何账号，也不产生任何副作用。**

自检实现（`internal/service/auth_protocol_selftest.go`）同时是三档协议的
**参考实现**。它与门户示例代码、官方 SDK 必须逐字节一致 —— 任何一方改了
包装方式，自检会第一时间红掉。

包装规格另有两处逐字节测试互相锚定：服务端
`internal/service/auth_protocol_canonical_test.go` 与 Kotlin SDK 的
`AegisCanonicalTest`，两边断言同一批字面量。改协议的正确姿势是先改这两个测试，
两边同时变绿才算改完。

## 7. 与旧接口的关系

旧的明文命名空间 `/api/auth/*` 仍然可用，由每个应用的 `allowLegacy` 开关控制，
**与新协议的安全等级正交**：应用可以一边跑 sealed 的 v1，一边保留旧接口过渡；
全部接入方迁移完成后再关掉。

`/api/v2/apps/*` 已被移除，其能力全部并入 `/api/v1/apps/{appKey}` 的 sealed 档。
