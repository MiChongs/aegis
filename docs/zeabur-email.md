# Zeabur Email 接入

> 面包屑：[Aegis](../CLAUDE.md) › docs/zeabur-email

## 为什么需要这条通道

Zeabur 的底层网络（Akamai / Linode）**封禁出站 SMTP 端口**。
部署在 Zeabur 上的实例无论把 SMTP 主机、端口、加密方式配成什么组合，
连接都只会以超时告终 —— 这不是配置问题，是平台的网络策略。

因此 Aegis 的邮件出口做成了可插拔的 provider。本文只讲 `zeabur` 这一档，
**九档服务商与两种作用域（应用级 / 平台级）的全貌见 [email.md](email.md)**。

在 Zeabur 上部署时，除本档外还可以选任何走 HTTP API 的服务商
（AWS SES / Resend / SendGrid / Mailgun / Postmark / 阿里云 / 腾讯云）——
被封的只是出站 SMTP 端口，HTTPS 一律通。`zeabur` 档的优势是不用另外开账号。

所有 provider 共用同一套业务代码：验证码、密码重置、欢迎信、资料变更通知、
以及 NotifyHub 的 `email` 渠道全部经由 `EmailService.sendRenderedMail` 出口，
切换 provider 不需要改任何业务逻辑。

> **注意**：这条通道**带不了附件**（REST 接口没有公开的附件字段）。
> 需要随信寄送凭证 PDF 时平台会自动改发签名下载链接；要真附件请改用
> SMTP / AWS SES / Resend / SendGrid / Mailgun / Postmark / 腾讯云。

## 配置步骤

### 1. 在 Zeabur 侧准备

1. 控制台 → **Email** → **Domains**，添加发件域名并按提示配置 DKIM / SPF / DMARC 三条 DNS 记录。
   验证前每日配额 100 封，验证后 1000 封（UTC 00:00 重置）。
2. **API Keys** → 新建密钥，权限选 **Send-only**（生产环境的最小权限），
   可另外限制该密钥只能用于指定发件域。
3. （可选但强烈建议）**Webhook** → 新建回调，URL 见下一节，事件建议全选。
   创建后 Zeabur 会生成一个签名 Secret，记下来。

### 2. 在 Aegis 控制台配置

**配置管理 → 邮件配置 → 新建配置**，服务商选 `Zeabur Email`：

| 字段 | 说明 |
|---|---|
| API Key | 上一步创建的密钥。AES-GCM 加密落库，保存后不再回显，留空表示不修改 |
| 发件地址 | 必须属于已在 Zeabur 验证的域名，否则上游返回 403 |
| 发件名称 / 回信地址 | 可选 |
| API 地址 | 留空即用 `https://api.zeabur.com/api/v1/zsend`，仅私有部署需要改 |
| Webhook 签名密钥 | Zeabur 生成的 Secret，同样加密落库 |

保存后用面板里的「发送测试」验证整条链路。

### 3. 回调地址

```
POST {你的后端地址}/api/email/webhook/zeabur/{scope}/{配置名}
POST {你的后端地址}/api/email/webhook/zeabur/{scope}          # 省略配置名时落到该作用域的默认配置
```

`{scope}` 是应用 id，或字面量 **`platform`**（平台级通道）。用关键字而不是数字 0：
这个地址要人工填进 Zeabur 控制台，`/webhook/zeabur/0` 看起来像个占位符没替换掉，
而填错的后果是回执永远匹配不到留痕，且不报错。

控制台的配置表单里会直接算好完整地址并提供复制按钮。

该路由是公开的（Zeabur 不可能携带管理员令牌），**准入完全依赖 HMAC 签名**：

- 签名消息为 `{timestamp}.{原始请求体}`，HMAC-SHA256，头部形如 `X-ZSend-Signature: sha256=<hex>`
- 比较使用恒定时间算法，避免签名被逐字节试探
- 时间戳容忍窗口 5 分钟，超出即按重放拒绝
- **未配置 Webhook 密钥的配置一律拒收回调**（412）—— 无法验签的回执可被任意伪造

## 投递状态如何流转

```
发信 ──► pending（Zeabur 已入队）
          │
          ├─ webhook: send      ──► sent
          ├─ webhook: delivery  ──► delivered
          ├─ webhook: bounce    ──► bounced
          ├─ webhook: complaint ──► complained
          ├─ webhook: reject    ──► rejected
          └─ webhook: open/click ─► 仅累加计数，不改主状态
```

两条硬规则写在 SQL 里，不靠应用层自觉：

1. **终态不可回退**。已经 `bounced` / `complained` / `rejected` / `failed` 的记录
   不会被后到的 `delivery` 事件改回成功 —— webhook 到达顺序没有保证，
   乱序会把一封已退信的邮件显示成投递成功。
2. **open / click 不改主状态**。邮件被打开多少次都不改变「是否送达」这件事。

SMTP 通道没有异步回执，状态止于 `sent`，这是该协议能确认的终点。

投递留痕落在 `email_deliveries` 表，控制台「邮件配置 → 投递记录」可按状态、
收件地址、主题筛选。**留痕失败不会导致发信失败** —— 信已经交出去了，
此时再报错只会让调用方重发一封。

## 错误对照

上游状态码会被翻译成可直接照做的中文提示：

| HTTP | 含义 | 是否重试 |
|---|---|:--:|
| 400 | 参数校验未通过（如主题超过 998 字符），错误详情原样带出 | 否 |
| 401 | API Key 无效或已吊销 | 否 |
| 403 | 密钥无该发件域权限，或账号处于 paused / banned | 否 |
| 429 | 日配额耗尽 | **否** |
| 5xx | 上游故障 | 是（最多 2 次，指数退避） |

429 刻意不重试：配额要等 UTC 次日重置，重试只会拖长请求并加深熔断。

Zeabur 通道有独立的熔断器（`email-zeabur:app-{id}:{配置名}`），
与 SMTP 通道互不影响。

## 平台限制速查

| 项 | 限制 |
|---|---|
| 单封收件人（to + cc + bcc） | 50 |
| 主题长度 | 998 字符（超出自动截断后再提交） |
| 单封总大小 | 10 MB |
| HTML / 纯文本 | 各 5 MB |
| 日配额 | 未验证域名 100 封，已验证 1000 封 |

Aegis 每封信同时提交 HTML 与自动提取的纯文本分片 —— 上游只要求二者有其一，
但两者都给能改善反垃圾评分，也照顾纯文本客户端。

## 密钥与迁移

- API Key 与 Webhook Secret 都以 AES-GCM 密文存进 `app_email_configs.config`，
  密钥派生自 `SECURITY_MASTER_KEY`（用途盐 `aegis.email.master`）。
- 管理接口出网时密钥一律抹除，只保留 `apiKeySet` / `webhookSecretSet` 布尔位。
- **换过 `SECURITY_MASTER_KEY` 就必须重新填写这两个密钥**：
  旧密文解不开，日志里会出现 `decrypt zeabur api key failed`，
  发信时报「API Key 未配置或解密失败」。
- 存量 SMTP 配置不受影响：`config` 这一列的 SMTP 字段仍是扁平结构，
  Zeabur 只是多了一个同级的 `zeabur` 键。

## 相关代码

| 位置 | 角色 |
|---|---|
| `internal/service/email_sender.go` | provider 抽象与分派 |
| `internal/service/email_provider_zeabur.go` | REST 客户端、错误映射、重试策略 |
| `internal/service/email_provider_smtp.go` | SMTP 发送器与错误分类 |
| `internal/service/email_webhook.go` | 回调验签与状态推进 |
| `internal/repository/postgres/email_repository.go` | 配置与投递记录读写 |
| `migrations/postgres/000064_add_email_deliveries.up.sql` | 投递留痕表 |
| `aegis-console/src/components/configuration/email-config-panel.tsx` | 控制台配置与投递记录 |
