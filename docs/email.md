# 邮件系统

> 面包屑：[Aegis](../CLAUDE.md) › docs/email

平台的邮件出口：**两种作用域、九档服务商、一份目录**。
Zeabur 那一档的接入细节另见 [zeabur-email.md](zeabur-email.md)。

## 两种作用域

| | 应用级 | 平台级 |
|---|---|---|
| 存储 | `app_email_configs.appid = <应用 id>` | `app_email_configs.appid IS NULL` |
| 管理入口 | 控制台 `/apps/{appKey}?tab=email` | 控制台 `/configuration?tab=email` |
| 服务的信 | 该应用用户的验证码 / 密码重置 / 欢迎信 / 资料变更 / 凭证 | 管理员通知、平台告警、平台级测试 |
| Go 侧表示 | `appID` 为正整数 | `appID == emaildomain.PlatformAppID`（即 `0`） |

平台级这一档是这次补上的。此前 `appid` 是 `NOT NULL` 且外键指向 `apps(id)`，
于是 NotifyHub 的**平台级 email 渠道**（`notify_channels.appid = 0`）走到
`EmailService` 时会在「查 appid=0 这个应用」那一步拿到 40410
「无法找到该应用」—— 渠道配得起来、发不出去，而错误信息指向一个不存在的应用。

作用域用 **`appid IS NULL`** 表达而不是沿用 `notify_channels` 的 `appid = 0`：
0 那种写法要求列上没有外键，而这里的外键正是「删掉应用时把它的邮件配置一并带走」
的唯一保证。NULL 能同时满足外键与平台级两件事；`0 ↔ NULL` 的映射收在仓储层一处
（`nullableAppID` / `emailScopeCondition`），上层看到的永远是一个 `int64`。

### 通道解析优先级

```
指名的配置（configName） → 本作用域的默认配置 → 平台级「已共享」的默认通道
```

最后一档只对应用级请求生效，且要求平台管理员在平台通道上**显式打开共享开关**
（`app_email_configs.shared`，默认关）。

默认关是刻意的：打开它意味着该应用发出的信会用平台的发件人身份，
而应用管理员既没参与这个决定、也看不出信是从哪条通道走的。因此这是一次显式授权，
不是升级后的默认行为 —— 存量部署升级后行为零变化。

回落发生时控制台会明说（`GET .../email/channel` 的 `inherited` 为 true，
面板上显示「本应用没有自己的通道，正在借用平台共享通道」）。
不说的话，管理员会对着一个空的邮件配置页纳闷验证码是怎么发出去的。

**投递回执不走这条回落**（`resolveWebhookConfig` 单独实现）：回执必须回填到
**发出这封信的那条通道**上。回落会让 A 应用的回调拿平台通道的密钥去验签，
验过了还会把状态写到错误的作用域里。

## 九档服务商

| provider | 传输 | SDK | 附件 | 投递回执 |
|---|---|---|:--:|:--:|
| `smtp` | 直连 SMTP | [go-mail](https://github.com/wneessen/go-mail) | ✅ | — |
| `zeabur` | REST | 自研（无官方 SDK） | ❌ | ✅ |
| `ses` | AWS SES v2 | `aws-sdk-go-v2/service/sesv2`（官方） | ✅ | ✅（经 SNS） |
| `resend` | REST | `resend-go/v2`（官方） | ✅ | ✅ |
| `sendgrid` | REST | `sendgrid-go`（官方，仅用于组装报文） | ✅ | ✅ |
| `mailgun` | REST | `mailgun-go/v5`（官方） | ✅ | ✅ |
| `postmark` | REST | 自研（无官方 Go SDK） | ✅ | ✅ |
| `aliyun` | 阿里云邮件推送 | `dm-20151123/v2`（官方） | ❌ | ✅ |
| `tencent` | 腾讯云 SES | `tencentcloud-sdk-go/.../ses`（官方） | ✅ | ✅ |

`resolveSender` 对未知 provider **直接报错而不静默回落到 SMTP** ——
回落会让「配了 A 却在用 B」这种故障以超时的形式出现在几层之外。

### 附件能力必须如实自述

调用方是**先问能力再写正文**的（`ResolveChannelCapability`）：能带附件的写
「收据见附件」，不能的写「点下面的按钮下载」。顺序反过来，正文已经写着
「收据见附件」了才发现带不了。

因此 `SupportsAttachments()` 不能乐观地一律返回 true。两家如实声明为不支持：

- **Zeabur**：REST 接口没有公开的附件字段。它的请求体与 Resend 高度同构，
  因此附件**很可能**也用同一套 `attachments` 键 —— 但「很可能」不足以拿来发凭证：
  未知字段被服务端忽略的表现是「邮件正常送达、附件不翼而飞」，谁也发现不了。
- **阿里云**：`SingleSendMail` 的附件字段是 `AttachmentUrl`，要求先把文件传到
  公网可访问的地址。凭证 PDF 恰恰是不该有公网直链的东西 —— 那是用一个更大的问题
  换一个附件。

通道带不了附件时 `sendRenderedMail` **当场报错**，绝不静默把附件丢掉再把信发出去。

### SES 走 Raw MIME

`SendEmail` 的 `Simple` 内容没有附件字段，而凭证 PDF 是这条通道的主要用途之一。
报文由 go-mail 组装（`buildRawMIME`）—— 中文主题的编码、长行折叠、附件文件名
这三处各错一次的坑它已经踩完了，而错了的表现是**部分**邮件客户端显示乱码。

AWS 凭据两项**要么都填、要么都不填**：只填一项时 SDK 会静默回落到默认凭据链
（环境变量 / EC2 实例角色 / ECS 任务角色），于是「我明明配了 Access Key」和
「用的是实例角色」这两件事同时成立，而失败信息只会说没有权限。阿里云同理。

## 配置是自述的，前端零改动

服务商目录（`ProviderMeta`）由各发送器的 `Describe()` 自述，同时驱动四处：

| 消费方 | 用它做什么 |
|---|---|
| 服务端校验 | `validateByCatalog`：必填、邮箱格式、URL 协议、下拉取值 |
| 密钥处理 | `SecretKeys()` 决定哪些值加密落库、哪些出网抹除 |
| 控制台表单 | 按 `Fields` 动态渲染，分区 / 高级折叠 / 密钥掩码全自动 |
| 能力展示 | 附件 / 回执 / 追踪 / 标签四项徽标 |

与支付渠道的 `Provider.Describe()`、风控条件目录、远程函数能力目录同一套做法：
**新增一家邮件服务商只需在 Go 侧加一个发送器**，控制台零改动即自动出现。

配置值放在通用的 `Settings` / `Secrets` 两个袋子里，而不是每家一个具名 struct：
后者在两家时还行，到九家就意味着每加一家要动领域类型、仓储载荷、传输 DTO、
控制台表单四处，而其中任何一处漏改都不报错。

`TestEmailProviderCatalogIsWellFormed` 钉住目录自身的完整性（字段键唯一、
Secret 标记与类型成对、下拉必须有选项、声明了回执就必须给回调地址模板等）。

### 存量数据零迁移

重构前的落库形态是「扁平的 SMTP 字段 + 可选的 `zeabur` 子对象」。
仓储层**读兼容**这两种形态，写入只产出新形态（`options` / `secrets`），
因此存量行下一次保存时自动完成迁移，在那之前照常可用。

两处刻意的处理：

- **按 provider 分流解存量行**：旧代码无论服务商是什么都会把 SMTP 段原样写进去
  （那一段是内嵌的），因此一条 zeabur 配置的 JSON 里同样有 `host` / `fromAddress`。
  不分流的话，Zeabur 的发件地址会被 SMTP 段那个覆盖掉。
- **`useTLS` 布尔位翻成 `encryption` 枚举**（true → `ssl`，false → `starttls`）。
  不翻的话，存量的 465 配置会在控制台上显示成 STARTTLS，而管理员保存一次
  就真的变成 STARTTLS 了 —— 一次静默的配置损坏。

> **顺带修掉的一处**：SMTP 密码此前是**明文**落库的（只在出网时抹除）。
> 现在全部密钥统一 AES-GCM 加密（用途盐 `aegis.email.master`）；
> 存量明文在读取时解进明文袋子、下一次保存时加密写回（自愈），
> 因此不需要迁移脚本。

## 密钥的三条语义

目录里 `Secret: true` 的字段，服务端统一兑现三件事，控制台不必各自实现：

1. **AES-GCM 加密落库**，明文永不进数据库；
2. **任何出网响应一律抹除**，只保留 `secretSet` 布尔位。判据来自目录而不是
   一串写死的字段名 —— 新增一个密钥字段时忘了补一行，表现是密钥经管理接口
   回流到浏览器，而那种泄露不会有任何报错；
3. **提交时留空表示「不修改」**。前端编辑态从不回显密钥，无条件覆盖会让
   「改个发件人名」把 API Key 清空，而这件事要到下一次发信才暴露。
   显式清空走 `clear_secrets`，不与「留空」共用一个表达方式。

## 投递回执

除 SMTP 外的**八家全部接了回执**。地址里的作用域段是应用 id，或字面量 `platform`：

| provider | 路径 | 准入方式 |
|---|---|---|
| `zeabur` | `/api/email/webhook/zeabur/{scope}[/{config}]` | 共享密钥 HMAC-SHA256 over `{timestamp}.{body}` |
| `ses` | `/api/email/webhook/ses/{scope}[/{config}]` | SNS 证书签名（RSA，SigVer 1=SHA1 / 2=SHA256） |
| `resend` | `/api/email/webhook/resend/{scope}[/{config}]` | svix 规格，用官方 SDK 的 `Verify` |
| `sendgrid` | `/api/email/webhook/sendgrid/{scope}[/{config}]` | ECDSA P-256 over `时间戳 + 原始报文` |
| `mailgun` | `/api/email/webhook/mailgun/{scope}[/{config}]` | HMAC-SHA256 over `timestamp + token`（官方 SDK 验签）**+ nonce 防重放** |
| `postmark` | `/api/email/webhook/postmark/{scope}[/{config}]?token=…` | 回调令牌（也接受 Basic Auth 的密码位） |
| `aliyun` | `/api/email/webhook/aliyun/{scope}[/{config}]?token=…` | 回调令牌 |
| `tencent` | `/api/email/webhook/tencent/{scope}[/{config}]?token=…` | 回调令牌 |

用关键字 `platform` 而不是数字 0：这个地址要人工填进服务商控制台，
`/webhook/ses/0` 看起来像个占位符没替换掉，而填错的后果是回执永远匹配不到留痕，
且不报错。

### 不签名的三家：地址即凭据

Postmark / 阿里云 / 腾讯云的回执报文里**没有任何可验证的东西**
（Postmark 官方只提供「在回调地址里放 Basic Auth 凭据」这一种做法，
另外两家连这个都没有）。因此准入靠平台自己下发的**回调令牌**，
它就是这条配置的 `webhookSecret`，拼在地址的 `?token=` 上。

三个来源任一命中即放行：query 的 `token`、`X-Aegis-Webhook-Token` 请求头、
HTTP Basic Auth 的**密码位**（用户名不参与判定 —— 那是管理员随手填的，
拿它当凭据的一部分只会制造一类查不出的失败）。比较是恒定时间的。

代价要说清楚，控制台也直接写在回执地址卡片上：**那个地址等同于密钥**，
不能贴进工单或聊天群。令牌只在编辑时显示（服务端从不回传密钥），
重新打开弹窗会退回占位符 —— 这是刻意的，不是 bug。

### Mailgun 为什么额外需要防重放

Mailgun 的签名**不覆盖报文体**，只覆盖 `timestamp + token`。
也就是说一条被截获的回调在 5 分钟窗口内可以原样重放任意多次，
而每次重放都会再推进一次状态、再累加一次打开数。
因此除验签外还用 Redis 把签名里那个一次性 `token` 记成 nonce（`SETNX`，TTL 5 分钟）。

Redis 不可用时**放行**并记 warn：拒收会让一次缓存抖动变成整批回执丢失，
而回执丢失是不可补的 —— 服务商不会因为你 5xx 就无限重推。

### 阿里云 / 腾讯云的字段拼写是容忍的

这两家的回执字段在不同投递方式与不同版本的文档里有 camelCase / snake_case /
PascalCase 三种写法（阿里云还分「带事件名」与「只有状态码」两种形状）。
钉死一种拼写的代价是换个投递方式就**一条也匹配不上**，
而那个失败是静默的：HTTP 200、零条匹配、界面上完全看不出来。

因此解析按**别名集合**取值（忽略大小写与 `_`/`-`，并下钻一层 `data` 包装），
阿里云在没有事件名时回落到 `status`（`0` = 成功，非 0 = 失败并带上状态码）。
认不出的报文**不猜**：如实记一条带**全部键名**的 info 日志 ——
这是唯一能说明「东西到了，但字段对不上」的线索。

几条硬约束：

- **原始字节**。带签名的几家覆盖的都是原文，任何重新序列化（键序、空白、转义差异）
  都会让验签失败。
- **没有密钥就拒收**。无法验签的回执可被任意伪造，而伪造一条 `delivered`
  就能把一封退信的邮件显示成送达 —— 这类留痕的全部价值就是可信。
  SendGrid 默认**不签名**，必须在控制台打开 Signed Event Webhook。
- **SNS 的安全根是主机名校验**。不校验的话，攻击者只要把 `SigningCertURL`
  指向自己的服务器，就能用自己的私钥签出一条通过验签的回执。只接受
  `sns.<region>.amazonaws.com(.cn)`；配了主题 ARN 时还要匹配。
  订阅确认在验签通过后自动回执 —— 不确认的话 SNS 永远不推事件，
  而管理员在 AWS 控制台上只会看到一个「Pending confirmation」。
- **时间窗口 5 分钟**。签名本身不带有效期，卡住时间戳才能挡住事后重放。
- **状态单向推进**（判定写在 SQL 里）：终态不被后到的 `delivery` 覆盖，
  `open` / `click` 只累加计数不动主状态。回执到达顺序没有保证。
- **延迟与暂时性失败都不是终态**。SES 的 `DeliveryDelay`、SendGrid 的 `deferred`、
  Mailgun 的 `severity=temporary`、Postmark 的 `SoftBounce`、腾讯云的 `SoftBounce`
  一律只记事件与原因，不改主状态 —— 写成失败会让一封最终送达的信永远显示成失败。
- **SendGrid 的消息 ID 要截断**。发信响应头 `X-Message-Id` 给的是 `abc123`，
  事件里的 `sg_message_id` 是 `abc123.filterdrecv-...`。不截断的话**每一条**
  回执都匹配不到留痕，表现为「webhook 明明在推，投递记录永远停在已发送」。

只有 SMTP 没有回执：协议本身不提供，交出信件即是这条链路能确认的最终状态。

## 接口

平台级走 RESTful，应用级沿用已经发出去的动词式旧命名空间（改路径等于让存量集成失效）：

| 作用域 | 接口 |
|---|---|
| 目录 | `GET /api/admin/system/email/providers`、`POST /api/admin/app/email-config/providers` |
| 平台级 | `GET|POST /api/admin/system/email/configs`、`GET|PUT|DELETE .../configs/{id}`、`POST .../configs/{id}/test`、`GET .../channel|deliveries|stats` |
| 应用级 | `POST /api/admin/app/email-config/{list,detail,create,update,delete,test,deliveries,stats,channel}` |

授权：平台级恒按**全局作用域**判定（`email:read` / `email:write`）。
按应用作用域判定的话，任何一个应用管理员都能改到全站的发信出口 ——
与 `/api/admin/platform/*` 那条「恒全局」是同一个理由。
目录接口不要权限点（静态自述，不含租户数据与凭据）。

## 出网一律经过出海代理网关

九档里除 SMTP 外全部走 `pkg/egress` 的客户端（含 SNS 取证书与订阅确认）。
这些服务商的端点多在境外，与 OAuth / 对象存储 / GeoIP 共用同一张域名路由表。
绕开它的后果是「开了代理，别的都通、唯独这家超时」，而那种差异极难联想到出网路由上去。

SendGrid 的官方 SDK 用的是包级默认 HTTP 客户端，接不进来，因此只用它组装与解析报文，
请求本身走平台统一的出网客户端。
