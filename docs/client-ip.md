# 真实客户端 IP

> 面包屑：[Aegis](../CLAUDE.md) › docs/client-ip

限流、IP 封禁、地理风控、登录审计、异地提醒 —— 这些能力全部建立在「这个请求来自哪个 IP」
之上。这个地址判错**不会报错**，只会让上面每一样一起静默失效，而且失效得很像正常工作：
限流照常计数、封禁照常写库、审计照常记录，只是记的全是同一个地址。

## 两种错法

直接读 `RemoteAddr` 与直接读 `X-Forwarded-For`，都是错的，只是错法不同：

| 做法 | 挂在反代后面时 | 后果 |
|---|---|---|
| 只读 `RemoteAddr` | 拿到反代的地址 | 全站用户共用一个 IP，限流一触即全站封锁，封禁封的是自己的网关 |
| 只读 `X-Forwarded-For` | 拿到客户端**自称**的地址 | 攻击者随便换 IP，限流与封禁形同虚设 |

正确做法只有一条：**先判直连对端可不可信，可信才读转发头**。

```
RemoteAddr 不在受信网段  →  它就是客户端，转发头一律不看
RemoteAddr 在受信网段    →  从转发链最右端往左走，跳过受信条目，
                            第一个不受信的条目就是客户端
```

「从右往左」是关键。转发头是**追加**的：每一跳把自己看到的对端追加到末尾。
客户端能控制的只有它自己写进去的那些（最左边），最右边那一条一定是紧邻的那一跳亲眼看到的。
因此从右往左遇到的第一个不受信条目，是整条链上最后一个由可信方写下的事实。

```
X-Forwarded-For: 1.2.3.4, 203.0.113.9, 10.0.0.4
                 ↑客户端伪造  ↑真实客户端   ↑内网反代（受信，跳过）
```

## 配置

一共五个环境变量，受信集合**只有一个入口**（`TRUSTED_PROXIES`）。

| 变量 | 默认 | 说明 |
|---|---|---|
| `TRUSTED_PROXIES` | `infra` | 受信代理网段：CIDR、单个 IP，或预设名，逗号分隔 |
| `CLIENT_IP_STRATEGY` | `auto` | 判定方式，见下表 |
| `CLIENT_IP_HEADER` | — | 单值头名（`header` 档必填；`auto` 档下覆盖平台探测的选择） |
| `CLIENT_IP_HOPS` | — | 本服务前面的受信代理层数（`trusted-hops` 档必填） |
| `CLIENT_IP_LIST_HEADER` | `X-Forwarded-For` | 转发链头，另可选 `Forwarded`（RFC 7239） |
| `CLIENT_IP_DEBUG_HEADER` | `false` | 在响应里回显判定结果与依据 |

配置写错（网段拼错、预设名拼错、档位不认识、`header` 档没给头名）一律**启动即失败**。
静默跳过一条写错的网段，表现是「线上大部分请求的 IP 突然变成网关地址」，
从这个现象倒查回配置文件要很久。

### 五档判定方式

| 档位 | 判定 | 什么时候用 |
|---|---|---|
| `auto`（默认） | 受信网段判定 + 平台探测 | 绝大多数部署 |
| `peer` | 完全不看转发头 | 直接暴露在公网，前面没有任何代理 |
| `trusted-ranges` | 同 auto，但**不做**平台探测 | 需要判定结果完全可预期 |
| `trusted-hops` | 取转发链右起第 N 条 | 代理出口地址不固定但层数固定 |
| `header` | 只认某个单值头 | 平台注入且会覆写该头（CF / Fly / …） |
| `leftmost` | 取转发链最左公网地址 | **可被伪造**，仅用于链路上有一跳不追加 XFF 的老设备 |

`header` 档取不到那个头时退回直连对端，**不会**再去看转发链 ——「只认这个头」就该是字面意思。

`header` 档也是唯一**不过直连对端那道闸**的一档：显式写下
`CLIENT_IP_HEADER=CF-Connecting-IP` 的人，断言的正是「本服务只经由那个会写这个头的
东西对外提供」。再要求对端落在 `TRUSTED_PROXIES` 里，会让这条配置在入口反代持有
公网地址时静默失效，而那恰恰是它最该起作用的场景。代价是这一档的安全性完全押在
「源站不能被直连」上 —— 与任何 CDN 回源鉴权的前提一致。

### 受信网段预设

| 预设 | 内容 |
|---|---|
| `infra`（默认） | 环回 + RFC1918 + 链路本地 + CGNAT + IPv6 ULA |
| `private` | 仅 RFC1918 与 IPv6 ULA |
| `loopback` | `127.0.0.0/8`、`::1/128` |
| `link-local` | `169.254.0.0/16`、`fe80::/10` |
| `cgnat` | `100.64.0.0/10`（RFC 6598） |
| `cloudflare` | Cloudflare 边缘出口网段（随 realclientip 库发布，不在本仓库另抄一份） |
| `gcp-lb` | `35.191.0.0/16`、`130.211.0.0/22`（Google 外部 LB 与健康检查） |
| `direct-peer` | **直连对端本身**，不管它是什么地址。见下节 |
| `all` | `0.0.0.0/0` + `::/0`，**转发头因此完全可伪造**，启动时告警 |
| `none` | 什么都不信，等价于 `peer` |

默认 `infra` 精确描述了「反代与本服务之间那一段网络」在容器平台上的样子：
Zeabur / Kubernetes / Docker Compose / ECS 里，入口网关到业务容器这一跳一定落在这些网段内。
而对端是公网地址时它一条都不匹配 —— 直连公网的部署因此不会多信任任何东西。

预设与显式网段可以混排：`TRUSTED_PROXIES=infra,cloudflare,198.51.100.0/24`。
**写了就是全部** —— 显式配置会替换默认值，需要保留基础设施网段就把 `infra` 也写上。

### 入口反代持有公网地址时

默认值只覆盖内网网段，前提是「入口到容器这一跳走内网」。这个前提在自建反代与多数
容器平台上成立，但**并非总是成立**：云上的 LB、部分 PaaS 的入口、CDN 的回源节点，
直连过来时都是公网地址。这时对端不在受信集合内，整条转发头被判为不可信而丢掉，
表现是全站客户端 IP 收敛成入口那一个地址。

服务端会在**第一次**遇到这种请求时按对端去重地打一条 warn，把地址、被丢掉的链
和改法一起说出来：

```json
{"level":"warn","msg":"客户端 IP 判定：转发头被忽略，因为直连对端不在受信网段内",
 "peer":"203.0.113.10","forwarded_ignored":["2409:8a4c::ed6","104.22.72.17"],
 "client_ip":"203.0.113.10","consequence":"所有请求的客户端 IP 都会是这个对端地址…"}
```

两条改法，按入口地址会不会漂移选：

| 情况 | 改法 |
|---|---|
| 地址固定 | 把它写进 `TRUSTED_PROXIES`，如 `infra,cloudflare,203.0.113.10` |
| 地址会漂移 | `TRUSTED_PROXIES=infra,cloudflare,direct-peer` |

`direct-peer` 表示信任紧邻的那一跳，**不管它是什么地址**。它比 `all` 弱得多 ——
转发链仍然逐跳按受信集合判定，只是多信任了对端本身。前提是**本服务只能经由自己的
入口访问**；源站若同时能被直连，直连者就成了「受信对端」，可以用一个
`X-Forwarded-For` 伪造自己的 IP。启动时会就此告警一次。

注意 `TRUSTED_PROXIES=direct-peer` 与 `CLIENT_IP_STRATEGY=peer` 意思**相反**
（前者信任对端好去读转发头，后者压根不读转发头），所以 `peer` 刻意不是
`direct-peer` 的别名 —— 写错位置会启动失败，而不是静默生效成相反的行为。

## 平台探测（`auto` 档）

`auto` 档会读**平台自己注入的环境变量**判断当前跑在哪家 PaaS 上。
这是安全的依据：`FLY_APP_NAME` / `ZEABUR_SERVICE_ID` 只有平台的运行时能写进来，
攻击者构造不出，因此据此补充受信网段或选定单值头不会给伪造留口子。

| 平台 | 探测变量 | 档案 |
|---|---|---|
| Fly.io | `FLY_APP_NAME` / `FLY_ALLOC_ID` / `FLY_MACHINE_ID` | 读 `Fly-Client-IP` |
| Google App Engine | `GAE_ENV` / `GAE_SERVICE` / `GAE_INSTANCE` | 读 `X-Appengine-User-Ip` |
| Google Cloud Run | `K_SERVICE` / `K_REVISION` / `K_CONFIGURATION` | 补 `gcp-lb` |
| Netlify | `NETLIFY` | 读 `X-Nf-Client-Connection-Ip` |
| Zeabur | `ZEABUR_SERVICE_ID` / `ZEABUR_PROJECT_ID` / `ZEABUR_ENVIRONMENT_ID` | 补 `cloudflare` |
| Railway | `RAILWAY_ENVIRONMENT` / `RAILWAY_SERVICE_ID` / `RAILWAY_PROJECT_ID` | 补 `cloudflare` |
| Render | `RENDER` / `RENDER_SERVICE_ID` | 补 `cloudflare` |
| Koyeb | `KOYEB_APP_NAME` / `KOYEB_SERVICE_ID` | 补 `cloudflare` |
| Heroku | `DYNO` | 无需补充 |
| Vercel | `VERCEL` / `VERCEL_ENV` | 无需补充 |
| Azure App Service | `WEBSITE_SITE_NAME` / `WEBSITE_INSTANCE_ID` | 无需补充 |
| AWS ECS / App Runner | `ECS_CONTAINER_METADATA_URI_V4` / `AWS_EXECUTION_ENV` | 无需补充 |
| Kubernetes | `KUBERNETES_SERVICE_HOST` | 无需补充 |

**探测顺序是先具体后笼统**，Kubernetes 排在最后：上面几乎每一家 PaaS 底层都是 K8s
也都会注入 `KUBERNETES_SERVICE_HOST`，它排在前面的话所有平台都会被认成「Kubernetes」，
那些平台特有的头与网段就再也用不上了。

几家 PaaS 补 `cloudflare` 的理由是同一个：它们分发的 `*.zeabur.app` / `*.up.railway.app`
这类域名通常还挂着 CDN，而 **CDN 边缘是公网地址** —— 不信任它的话，
「从右往左找第一个不受信条目」会停在 CDN 边缘上，结果是全站用户的 IP
收敛成一小撮机房地址。反过来，链路里没有 CDN 时这几段网段一条也匹配不上，多加无害。

显式选 `trusted-ranges` 则**不做任何补充**：那一档要的就是「受信集合严格等于我写的那几行」。

## 排障：判定结果自己会说话

三个入口，从粗到细：

**启动日志** —— 每次启动打一行生效配置：

```json
{"msg":"client ip resolution ready","strategy":"auto","platform":"zeabur",
 "list_header":"X-Forwarded-For","trusted_prefixes":30}
```

**启动横幅** ——「安全」分区里的「客户端 IP」一行，报的是生效结果
（含平台探测与预设展开之后的网段数量），不是配置原文。

**调试响应头** —— `CLIENT_IP_DEBUG_HEADER=true` 后一次 curl 看到结论与全部依据：

```
X-Aegis-Client-IP: 203.0.113.9
X-Aegis-Client-IP-Source: source=forwarded-chain; strategy=auto; header=X-Forwarded-For;
                          peer=10.42.0.7; peer_trusted=true; platform=zeabur;
                          chain=1.2.3.4|203.0.113.9|172.68.1.1
```

`source` 直接回答「为什么是这个 IP」：

| source | 含义 |
|---|---|
| `peer` | 直连对端。要么它不受信（转发头被忽略），要么转发链没给出可用结论 |
| `header` | 命中了平台单值头 |
| `forwarded-chain` | 转发链上第一个不受信条目 |
| `forwarded-hops` | 转发链右起第 N 条 |
| `forwarded-leftmost` | 转发链最左公网地址 |
| `unresolved` | 连 `RemoteAddr` 都解析不出来（测试构造的裸请求） |

依据里含内网拓扑，查完请关掉。

### 常见现象对照

| 现象 | 原因 | 处置 |
|---|---|---|
| 所有人的 IP 都是同一个内网地址 | 该地址不在受信网段内，转发头被整个忽略 | 把它加进 `TRUSTED_PROXIES` |
| 所有人的 IP 都是同一小撮公网地址 | 判定停在 CDN / LB 边缘上 | 加对应预设或该 CDN 的网段 |
| 用户能随意换 IP 绕过限流 | 受信集合过宽（`all`，或把公网段写了进去） | 收窄到实际反代网段 |
| `source=peer`，`peer_trusted=false` | 直连对端不在受信网段内，转发头被整个忽略 | 见[入口反代持有公网地址时](#入口反代持有公网地址时)；服务端已就此打过 warn |
| `source=peer`，`chain` 为空 | 反代根本没有转发 `X-Forwarded-For` | 让反代追加 XFF；只能设 `X-Real-IP` 的话配 `CLIENT_IP_HEADER=X-Real-IP` |
| `source=peer`，`chain` 非空 | 转发链条目全在受信集合内 | 受信集合过宽，收窄它 |

## 实现

| 位置 | 职责 |
|---|---|
| [`pkg/clientip`](../pkg/clientip) | 判定规则：受信集合、平台档案、直连对端闸门 |
| [`internal/middleware/client_ip.go`](../internal/middleware/client_ip.go) | 中间件：判定并**改写 `RemoteAddr`** |
| `internal/transport/http/router.go` | 挂在中间件栈首位；同时把 gin 自带的转发头解析关掉 |

转发头的解析本身（XFF 与 RFC 7239 Forwarded、端口、IPv6 方括号、zone、多个同名头）
交给 [realclientip-go](https://github.com/realclientip/realclientip-go)。
这些形状每一种都有人踩过，自己写一遍只是把别人修过的 bug 重新引进来一次。
受信网段的集合运算用 [go4.org/netipx](https://github.com/go4org/netipx)，
本包与解析库使用的是**同一个集合**导出的两种表示，因此「谁受信」不可能出现分歧。

### 为什么改写 `RemoteAddr` 而不是换一个取 IP 的函数

`c.ClientIP()` 在这个仓库里散布在 25 个文件、57 处调用中，还有一部分在第三方中间件内部
（限流、WAF、追踪都各自取过一次）。换函数意味着每一处都要改对、且以后每一个新调用点
都要记得别用 gin 那个 —— 漏掉任何一处的表现不是报错，而是那一处悄悄按代理地址限流、
按代理地址封禁。

改写 `RemoteAddr` 则让 `c.ClientIP()`、`c.RemoteIP()`、`Request.RemoteAddr` 三种取法
**同时**变正确，包括还没写出来的那些调用点。直连对端仍然留在 `request.peer_ip`
（`middleware.RequestPeerIP(c)`）里，判定过程留在 `middleware.RequestClientIPDetail(c)` 里。

与之配套，`router.SetTrustedProxies(nil)` 把 gin 自己那套解析关掉：两套同时开着，
只会让「到底谁说了算」变成一个没人答得上来的问题。

## 部署样例

```bash
# Zeabur / Railway / Render / Koyeb / 任意 Kubernetes：什么都不用配
# （auto 档 + infra 默认值 + 平台探测已经覆盖）

# 自建 Nginx / Caddy 反代，与本服务同机
TRUSTED_PROXIES=loopback

# 自建反代在内网另一台机器
TRUSTED_PROXIES=infra

# 站在 Cloudflare 后面（自建源站，且入口到本服务走内网）
TRUSTED_PROXIES=infra,cloudflare

# 站在 Cloudflare 后面，且入口反代持有公网地址（云 LB / 部分 PaaS 入口）
TRUSTED_PROXIES=infra,cloudflare,direct-peer
# 或者只认 Cloudflare 覆写的那个头 —— 这一档不看直连对端是谁
CLIENT_IP_STRATEGY=header
CLIENT_IP_HEADER=CF-Connecting-IP

# 直接暴露在公网，前面什么都没有
CLIENT_IP_STRATEGY=peer

# 前面有两层代理，出口地址不固定
CLIENT_IP_STRATEGY=trusted-hops
CLIENT_IP_HOPS=2
```

> 站在第三方 CDN / WAF 后面时，**只按 IP 认它是不够的**：任何人都能创建一个指向你源站的
> 分发并伪造转发头，那时「受信网段」里的地址就不再代表你的那一份配置。
> 真正的隔离手段是回源鉴权（mTLS、Tunnel、共享密钥头），IP 网段只是补充。
