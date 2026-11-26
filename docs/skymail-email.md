# Cloud Mail 邮件通道与邮局代管

> 面包屑：[Aegis](../CLAUDE.md) › [docs/email](email.md) › skymail-email

[Cloud Mail](https://docs.skymail.ink/api/api-doc.html) 是部署在 Cloudflare Workers 上的自建邮局，收发一体。
平台把它接成邮件的第十档服务商（`provider = skymail`），并在控制台「邮件 → 邮局」里代管它的整套邮局。

| 代码 | 内容 |
|---|---|
| `internal/service/skymail_client.go` | HTTP 客户端：登录、令牌缓存、401 重登、18 个接口的类型化封装 |
| `internal/service/email_provider_skymail.go` | 发信器：目录自述、校验、`Send` |
| `internal/service/skymail_admin.go` | 邮局代管：`SkymailQuery`（只读）/ `SkymailAction`（写），白名单 + 参数校验 |
| `internal/service/skymail_sync.go` | 投递状态对账循环 |
| `internal/transport/http/email_skymail_handlers.go` | 四条管理路由 |
| `aegis-console/src/components/email/mailbox/` | 控制台「邮局」视图 |

## 接口约定里的几处坑

文档只给了成功示例，下面这些是对着 [开源实现](https://github.com/maillab/cloud-mail) 核对出来的：

- **鉴权头是 `Authorization: <token>`，不带 `Bearer`。**
- **业务错误一律 HTTP 200**，真正的结果在信封 `{code, message, data}` 的 `code` 里：401 登录失效、403 无权限、
  500 业务拒绝（发件账号不存在、超出发件次数……）。只看 HTTP 状态码会把「账号被禁用」当成发信成功。
- **同一用户最多保留约 11 个令牌**，再登录就挤掉最早的。每封信都登录一次，会把管理员浏览器里的会话挤下线。
  因此令牌按「站点 + 邮箱 + 密码摘要」缓存 25 天（服务端 30 天），只在信封回 401 时重登一次；
  同一凭据的并发登录合并成一次请求。
- 时间是 UTC 的 `yyyy-MM-dd HH:mm:ss`，没有时区后缀。后端统一转成 RFC 3339 再下发。
- GET / DELETE 的参数全部走查询串；批量 id 用英文逗号拼接。
- 发信要的是**邮箱账号 id**（`accountId`），不是地址。发件地址留空时用登录主邮箱（`loginUserInfo.account`），
  否则翻 `account/list`（只有游标分页）按地址匹配，结果缓存 10 分钟。

## 发信

| 配置键 | 说明 |
|---|---|
| `baseUrl` | 邮局根地址。误带的 `/api`、`/api/login`、末尾 `/` 会被裁掉 |
| `loginEmail` | 登录邮箱 |
| `password` | 登录密码（密钥：加密落库、出网抹除、留空不改） |
| `fromAddress` | 可选，必须是登录用户名下的邮箱 |
| `fromName` | 可选，发件人名称 |

- 附件 ≤ 10 个，Base64 上送；超过就在本地拒绝，不静默丢弃。
- Cloud Mail 没有单独的回信地址字段，`replyTo` 不在这一档的目录里。
- 发信**不重试**：一次 5xx 之后对方可能已经发出去了，重试就是重复发信。熔断照常。
- 服务商邮件 ID 记成 `<emailId>@<站点主机>`：Cloud Mail 的 id 是自增整数，两套邮局各发一封就会撞号。
- 站外收件人要靠邮局后台绑定的 Resend 或 Cloudflare Email 发出；只配了收信的邮局只能发站内地址，
  否则 Cloud Mail 回「没有发件服务」（500），平台报 40070。

## 投递状态对账

Cloud Mail 把投递结果回写到自己的发件箱，但不推送。`StartSkymailSync` 每两分钟（Redis 锁保证单实例）：

1. 取三天内 `provider = skymail` 且仍在 `pending / sent` 的信（`000171` 的部分索引，只收未结的信）；
2. 按通道分组，登录对应邮局，用 `email/list?type=1&allReceive=1` 从新到旧翻发件箱（每通道最多 20 页）；
3. 找到同一封就按下表翻译，走 `applyDeliveryEvent` —— 与 webhook 回执同一个入口，单向推进、时间线、自动抑制一条不少。

| Cloud Mail `status` | 平台状态 | 事件 |
|---|---|---|
| 1 已发送 / 6 保存中 | 不变 | — |
| 2 已送达 | `delivered` | `delivered` |
| 3 退信 | `bounced` | `bounced` |
| 4 投诉 | `complained` | `complained` |
| 5 延迟 | 不变 | `delayed`（每封只记一次） |
| 7 无人收件 | `bounced` | `bounced`（站内地址不存在，与硬退信同性质） |
| 8 失败 | `failed` | `failed` |

回填按**通道**的作用域匹配：借道平台共享通道的信记在应用名下，但对账用的是平台通道的凭据。

## 邮局代管

| 作用域 | 只读 | 写 |
|---|---|---|
| 平台级 | `POST /api/admin/system/email/configs/:configId/skymail/query` | `…/skymail/action` |
| 应用级 | `POST /api/admin/app/email-config/skymail/query`（`appid`、`config_id` 在请求体） | `…/skymail/action` |

查询需要 `email:read`，写需要 `email:write`；写操作审计级别为高。请求体 `{resource|action, params}`，
`params` 严格解码（拼错的字段名直接 400）。

| 资源 / 动作 | Cloud Mail 接口 |
|---|---|
| `overview` | `my/loginUserInfo` + `account/list` 首页 + 发信账号解析 |
| `emails` / `email` | `email/list`（单封用「id+1 作游标、取 1 条、full=1」取正文） |
| `accounts` | `account/list` |
| `users` | `user/list` |
| `allEmails` / `allEmail` | `allEmail/list` |
| `regKeys` / `regKeyHistory` | `regKey/list` / `regKey/history` |
| `roles` | `role/selectUse`（文档未列；注册码必须指定身份 id，这是 Cloud Mail 前端下拉用的接口） |
| `send` | `email/send`（新信 / `reply` / `forward`，≤50 收件人、≤10 附件、解码后 ≤25MB） |
| `deleteEmails` | `email/delete` |
| `addAccount` / `deleteAccount` | `account/add` / `account/delete` |
| `addUser` / `setUserStatus` / `deleteUsers` | `user/add` / `user/setStatus` / `user/delete` |
| `deleteAllEmails` | `allEmail/delete`（永久） |
| `addRegKey` / `deleteRegKeys` | `regKey/add` / `regKey/delete` |

控制台按 `overview.isAdmin`（`permKeys` 含 `*`）决定显示不显示用户、全部邮件、注册码三页；
普通账号越权的请求由 Cloud Mail 以 403 拒绝，平台翻成 40072。邮件正文放进不带任何权限的沙箱 iframe。
