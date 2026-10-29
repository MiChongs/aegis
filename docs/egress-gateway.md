# 出海代理网关

> 目标：**配好一次，全平台所有出站流量按目标域名后缀自动决定走直连还是走境外线路。**

境内部署的 Aegis 需要访问一批境外 API —— Stripe、PayPal、Google OAuth、Apple、
GitHub、S3、Zeabur Email、MaxMind GeoIP。与其让每个 service 各自 `new(http.Client)`
再各自处理代理，不如把「这条出站流量该怎么出去」收敛成一张**按域名后缀匹配的路由表**。

```mermaid
graph LR
  subgraph svc["业务侧（只认这一行）"]
    A["egress.NewClient(Profile{Name: \"payment.stripe\"})"]
  end
  subgraph gw["pkg/egress 网关"]
    R["规则匹配<br/>域名后缀 Trie"]
    P["端点选路<br/>failover / 轮询 / 加权 / 最低延迟"]
    H["健康检查<br/>主动探测 + 被动熔断"]
  end
  subgraph out["出口"]
    D["直连"]
    E1["http / https 代理"]
    E2["socks5 / socks5h"]
    E3["ssh 隧道"]
    E4["trojan / shadowsocks"]
    X["拒绝"]
  end
  A --> R --> P --> E1 & E2 & E3 & E4
  R --> D
  R --> X
  H -.-> P
```

## 三个概念

| 概念 | 是什么 |
|---|---|
| **端点 Endpoint** | 一条真实线路：协议 + 地址 + 凭据。可用 `via` 串成代理链 |
| **规则 Rule** | 匹配条件（域名后缀为主）→ 动作（`proxy` / `direct` / `reject`） |
| **网关 Gateway** | 持有端点与规则的原子快照，对外提供 Transport / Client / DialContext |

## 业务侧怎么用

```go
// HTTP 客户端 —— 绑定的是全局默认网关，配置热重载后无需重建
client := egress.NewClient(egress.Profile{
    Name:    "payment.stripe",   // 调用方标识，可参与规则匹配
    Timeout: 15 * time.Second,
})

// 第三方 SDK 自带 transport 时，只接管出口
resty.New().SetTransport(egress.NewTransport(egress.Profile{Name: "payment.gateway"}))
stripeAPI.Init(key, stripe.NewBackends(egress.NewClient(egress.Profile{Name: "payment.stripe"})))

// 裸 TCP（SMTP / LDAP / 自定义协议）
conn, err := egress.DialContext(ctx, "tcp", "smtp.example.com:465")

// UDP：已连接的数据报连接，每次 Write / Read 一个报文；带调用方身份用 egress.Dial
conn, err := egress.Dial(ctx, egress.Profile{Name: "ntp"}, "udp", "time.google.com:123")

// 一个套接字发往多个目标（QUIC、STUN）：按每个目标各自路由，目标是域名时用 egress.Addr
pc, err := egress.ListenPacket(ctx, egress.Profile{Name: "stun"})
pc.WriteTo(payload, egress.Addr{Host: "stun.l.google.com", Port: 19302})

// 经网关解析 DNS（指定境外服务器，路由表把它送进代理即可避开本地污染）
resolver := egress.Resolver(egress.Profile{Name: "dns"}, "1.1.1.1:53")

// HTTP/3：QUIC 报文按路由表走（pkg/egress/egressh3，单独成包以免核心依赖 quic-go）
client := egressh3.NewClient(egress.Profile{Name: "h3.api", Timeout: 15 * time.Second})
```

## UDP

| 线路协议 | UDP 怎么承载 | 能否接在跳板后面 |
|---|---|---|
| direct | 系统 UDP 套接字 | — |
| socks5 / socks5h | UDP ASSOCIATE：TCP 控制连接 + 到中继地址的 UDP（服务端回 0.0.0.0 时用代理主机） | 跳板也要能承载 UDP |
| shadowsocks | UDP relay，每个报文单独 AEAD 加密 | 跳板也要能承载 UDP |
| trojan | UDP ASSOCIATE，报文按「地址 + 长度 + CRLF + 载荷」封装在 TLS 流里 | 任意跳板（只需要 TCP） |
| mihomo | 经核心的专用 SOCKS5 入站，节点自己的 UDP 能力由核心决定（节点上的 `udp`） | — |
| http / https / ssh | 不支持（CONNECT 与 direct-tcpip 都只有 TCP） | — |

- **选路**：UDP 流量只在能承载 UDP 的线路里选；规则连的线路都不能承载时这次连接失败（不悄悄直连），画布编译时会提醒。
  线路上 `udp: false`（DSL 写 `?udp=false`）可以让它只走 TCP，例如服务端没开 UDP 转发。
- **匹配**：规则多了「传输层」维度 `networks: ["udp"]`，与其它条件同时满足才命中，常用来单独管 QUIC（UDP 443）或 DNS。
- **健康**：UDP 健康单独记——TCP 通而 UDP 不通（服务端没开 UDP）很常见，不能让 UDP 的失败把 TCP 流量也赶走。
  主动探测对能承载 UDP 的线路经它向 `udpProbeServer`（默认 `1.1.1.1:53`）查一次 `udpProbeName`，
  被动熔断也按传输层分开记；「最低延迟」策略对 UDP 用 UDP 探测的延迟。
- **统计**：每条线路的 UDP 会话数与收发报文数；网关的 UDP 判定数。
- **自测与解释**：`POST /egress/test` 带 `network: "udp"` 时经路由表（或指定线路）向 DNS 服务器发一次查询并返回解析结果；
  `POST /egress/explain` 带 `network: "udp"` 按 UDP 判定。

未装配网关时自动退化为直连，单元测试与 CLI 不需要任何初始化。

## 配置

### 快速上手（.env）

```bash
EGRESS_ENABLED=true
EGRESS_ENDPOINTS=hk=socks5h://user:pass@10.0.0.1:1080?region=hk;us=http://proxy.us:3128
EGRESS_RULES=hk=*.google.com,*.googleapis.com,*.gstatic.com;us=*.stripe.com,*.paypal.com
```

含义：命中这些后缀的出站请求分别走 `hk` / `us` 线路，其余一律直连。

**域名后缀按标签边界匹配**：`google.com` 命中 `google.com` 与 `www.google.com`，
**不**命中 `notgoogle.com`。规则数量上千时匹配复杂度只与域名标签数（个位数）有关。

### 端点 URL 语法

```
<协议>://[用户名[:口令]@]主机:端口[?参数]
```

| 协议 | 写法 | 说明 |
|---|---|---|
| HTTP 正向代理 | `http://user:pass@host:3128` | 默认打 CONNECT 隧道 |
| HTTPS 正向代理 | `https://host:443` | 与代理之间先 TLS |
| SOCKS5 | `socks5://host:1080` | 域名**本地**解析后送 IP |
| SOCKS5h | `socks5h://host:1080` | 域名交给代理解析（出海推荐，绕开境内 DNS 污染） |
| SSH 隧道 | `ssh://ops:pass@bastion:22?user=ops` | direct-tcpip 通道，长连接复用 |
| Trojan | `trojan://password@edge.example.com:443?sni=www.example.com` | 强制 TLS |
| Shadowsocks | `ss://aes-256-gcm:password@relay:8388` | AEAD，userinfo 也可整体 base64 |
| 直连 | `direct://` | 放在 failover 末位实现「代理全挂就直连」 |

通用查询参数：`region` `remark` `weight` `via` `probeUrl` `dialTimeoutMs`
`tls` `sni` `insecure` `alpn` `forward` `disabled`。

### 规则 DSL

```
<端点列表>=<域名后缀列表>
```

- 端点列表用 `|` 分隔，可追加 `@策略`：`hk|us@round_robin=...`
- 端点列表也可以写 `direct` 或 `reject`
- 声明顺序即优先级
- 更复杂的维度（端口 / CIDR / 调用方 profile / 正则 / 例外后缀）请用 JSON 或管理端

### 全量 JSON

```jsonc
{
  "enabled": true,
  "defaultAction": "direct",
  "endpoints": [
    { "name": "jump", "protocol": "socks5h", "address": "10.0.0.1:1080" },
    { "name": "hk", "protocol": "trojan", "address": "hk.example.com:443",
      "password": "***", "via": "jump", "region": "hk", "weight": 2 },
    { "name": "fallback", "protocol": "direct" }
  ],
  "rules": [
    { "name": "内网直连", "priority": 1, "action": "direct",
      "match": { "domainSuffixes": ["internal.corp"] } },
    { "name": "支付走固定出口", "priority": 10, "action": "proxy",
      "endpoints": ["hk"], "strategy": "failover",
      "match": { "domainSuffixes": ["stripe.com"], "profiles": ["payment.*"] } },
    { "name": "谷歌系", "priority": 20, "action": "proxy",
      "endpoints": ["hk", "fallback"],
      "match": { "domainSuffixes": ["*.google.com"],
                 "excludeDomainSuffixes": ["cn.google.com"] } }
  ]
}
```

用 `EGRESS_CONFIG_FILE=config/egress.json` 或 `EGRESS_CONFIG=<内联 JSON>` 加载。

**匹配语义**：同一条规则内，**不同维度之间是「与」**（写了后缀又写了端口，两者都要满足），
同一维度内的多个值是「或」。这是刻意的 —— 出海规则最怕「本想只放 443，结果整域全放」。

## 配置优先级与热重载

```
控制台画布（platform_settings.platform.egress.graph）  ←  优先（已应用的那一版）
旧版控制台（platform_settings.platform.egress）        ←  没有画布时
        ↑ 落库 + 立即生效
.env / EGRESS_CONFIG_FILE                             ←  基线
        ↑ fsnotify 热重载
```

- 用旧接口 `PUT /egress` 改过配置，会撤掉已应用的画布（草稿保留），以免下次启动又按画布覆盖回去；
  画布里有 mihomo 线路时旧接口直接拒绝

- 数据库里存在覆盖时，保存 `.env` **只更新基线**，不会冲掉控制台里配好的路由
- `POST /api/admin/system/egress/reset` 丢弃数据库覆盖，回到 `.env`
- 重载失败保留旧配置：一次误配不应该把所有境外调用打回直连
- 重载后受管 transport 会 `CloseIdleConnections()`，避免空闲连接还指向被删掉的端点

## 管理端 API

全部限超级管理员，挂在 `/api/admin/system/egress`：

| 方法 | 路径 | 用途 |
|---|---|---|
| `GET` | `/egress` | 配置 + 端点健康 + 流量统计 + 可选项目录 |
| `PUT` | `/egress` | 整份替换配置（校验 → 落库 → 热生效） |
| `POST` | `/egress/reset` | 恢复为 `.env` 基线 |
| `POST` | `/egress/test` | 实跑一次出站请求（可指定端点 / profile） |
| `POST` | `/egress/explain` | 「这个域名会怎么出去」，逐条列出规则匹配过程 |
| `POST` | `/egress/probe` | 立即探测全部端点 |

**密钥永不出网**：`GET` 只回传 `passwordSet` / `privateKeySet`；
`PUT` 时密钥字段留空表示保持原值，要真正清空需把该端点的 `clearSecrets` 置为 `true`。
落库前口令 / SSH 私钥 / 客户端证书私钥用 AES-GCM 加密，密钥派生自 `SECURITY_MASTER_KEY`。

控制台入口：**配置 → 出海代理**（`/configuration?tab=egress`，仅超管可见），见下一节「画布与 mihomo」。

排障建议先打 `explain`：

```bash
curl -X POST .../api/admin/system/egress/explain \
  -d '{"host":"api.stripe.com","port":443,"profile":"payment.stripe"}'
```

返回里 `evaluated` 会按顺序列出每条规则是否命中、命中原因（`domainSuffix=stripe.com`），
以及最终选中的端点与端点链。

`test` 把 URL 指向「查询出口 IP」类服务，响应体会被截断保留在 `body` 里，
可以直接确认落地是否真的在境外。

## 画布与 mihomo

控制台用一张画布（Flowgram 自由布局，与物品发放、等级体系同形）同时编排网关路由表与 mihomo 核心：

```
网关设置 ──规则链──▶ 规则 ──下一条──▶ 规则 ──▶ …        自上而下依次匹配，都没命中走「未命中」
   │                  │
   └──未命中──▶        └──命中──▶ 网关线路 / 策略组 / mihomo 节点 / mihomo 分流
网关线路 ──经由──▶ 网关线路                               代理链（跳板 → 落地）
策略组 ──成员──▶ 节点 / 策略组 / 订阅                      select / url-test / fallback / load-balance / relay
节点、订阅 ──前置代理──▶ 节点 / 策略组                     dialer-proxy
```

后端 `internal/domain/egressgraph` 把画布编译成两份东西：网关的 `egress.Config`，以及交给核心的
`mihomo.Profile`（节点、订阅、策略组保持 mihomo 原生键值，核心支持的协议与字段都能写；控制台表单覆盖常用字段，
其余在节点的「YAML」页签里编辑）。规则顺序由规则链决定，不再填优先级。

### mihomo 怎么接进来

mihomo（Clash Meta）以 GPL-3.0 发布，网关**不把它编进本程序**，而是作为旁车进程托管（`pkg/egress/mihomo`）：

- **安装**：画布里第一次出现 mihomo 节点时，从 [MetaCubeX/mihomo 发布页](https://github.com/MetaCubeX/mihomo/releases)
  下载当前平台的包（amd64 选 compatible 版），逐字节校验发布页给出的 sha256，再跑一次 `-v` 确认能执行。
  下载先走 `EGRESS_MIHOMO_MIRROR` 前缀，失败回落直连（经网关）。也可以在控制台手动安装 / 升级，或用
  `EGRESS_MIHOMO_BINARY` 指向自带的程序。核心是静态编译的 Go 程序，Alpine 镜像里可以直接跑。
  部署平台每次发布清空本地盘时（Zeabur 无持久卷），核心会在启动后重新下载；想避免就把 `EGRESS_MIHOMO_HOME` 放到持久卷上。
- **运行**：配置先用核心自己的 `-t` 校验，再落盘、经 external-controller `PUT /configs` 热重载；
  首次启动等控制器就绪。进程意外退出按 1、2、4…64 秒退避自动拉起；Linux 上设了 `Pdeathsig`，网关进程被强杀时核心一起退出。
- **接入**：画布里每个被规则引用的策略组 / 节点，编译成一条 `mihomo` 协议的网关端点（`mihomo.<节点 ID>`），
  在核心里对应一个专用的 SOCKS5 入站（监听 `127.0.0.1`、带鉴权、开 UDP，`listeners[].proxy` 指向这个目标），
  TCP 与 UDP 都直达目标、不经分流规则；健康检查、故障转移、统计都照常按网关端点算。另有一个不指定目标的入站，
  连到「mihomo 分流」的流量从这里进，按核心自己的分流表走。目标的入站端口在热重载之间保持不变，核心会原样保留这些入站。
  不用 `IN-USER` 按用户名分发，是因为核心的 SOCKS UDP 中继不带入站用户，UDP 会落到分流规则上。
- **托管键**：入站、控制器、TUN、`allow-lan`、`external-ui` 这些由网关托管，导入或「其它顶层键」里写了一律剔除。
- **多进程**：每个进程（API、Worker、多副本）各托管一份核心，家目录 `EGRESS_MIHOMO_HOME/<api|worker>` 分开
  （核心的缓存文件有独占锁）；Unified 模式共用一份。应用只发生在收到请求的进程里，其余进程每 20 秒比对已应用版本号，
  变了就跟着应用。

### 草稿、应用与版本

- **保存草稿**不影响线上；**应用** = 编译 → 核心接受新配置 → 网关热替换 → 落库并记一个版本（保留最近 20 个）。
  核心拒绝就不动网关；网关拒绝就把核心退回旧配置。
- 草稿带修订号，别人先保存过会得到 40900，刷新后再保存。
- **回滚**把某个历史版本重新应用一次（记为新版本）；版本也可以先载入画布查看、改完再应用。
- **密钥**：读回的画布里所有密钥（口令、UUID、私钥、预共享密钥、订阅地址……）都是占位
  `__AEGIS_SECRET_KEPT__`，原样提交即保持原值，提交空串即清空；复制出来的节点按「类型 + 名称」找回原值。
  落库前逐个用 `SECURITY_MASTER_KEY` 派生的密钥 AES-GCM 加密。导出核心 YAML 默认打码。

### 导入

画布顶栏「导入」接受：Clash / mihomo 的 YAML 配置；分享链接（ss、ssr、vmess、vless 含 REALITY、trojan、hysteria、
hy2、tuic、anytls、socks、http，一行一条，也接受整段 base64 的 v2ray 订阅）；订阅地址（推荐作为「订阅」节点由核心定时更新，
也可以一次性展开成节点）。可以并入画布，或替换画布上的 mihomo 部分（网关规则按名字接回新节点）。
按进程、UID、入站端口匹配的分流规则会被去掉（网关是唯一的入站）。

### 运行态

出海代理页的上半部分是网关判定计数与核心状态（每秒速率曲线、内存、版本、PID）；下半部分页签：
线路健康与规则命中、策略组（全组测速、点选切换、取消固定）、订阅用量与规则集更新、在途连接（可关闭）、
核心日志（最近 2000 行，含托管器自己的事件）、诊断（出站自测、路由解释、核心 DNS、核心生效的规则）、版本。
画布卡片上也叠着这些运行态，右侧「追踪」输入域名后把 规则 → 线路 → 策略组当前成员 这条路径在画布上点亮。

### 画布与核心的管理端 API

| 方法 | 路径 | 用途 |
|---|---|---|
| `GET` | `/egress/graph` | 画布（打码）+ 编译结果 + 目录 + 核心状态 + 网关运行态；没有画布时由当前配置生成 |
| `PUT` | `/egress/graph` | 保存草稿（带 `revision`） |
| `POST` | `/egress/graph/validate` | 编译校验，附路由表与核心 YAML 预览 |
| `POST` | `/egress/graph/apply` | 应用（同时保存草稿、记版本） |
| `POST` | `/egress/graph/discard` | 丢弃草稿，回到已应用的版本 |
| `POST` | `/egress/graph/import` | 导入 YAML / 分享链接 / 订阅地址，返回新画布（不保存） |
| `POST` | `/egress/graph/export` | 导出核心 YAML（`includeSecrets` 默认 false） |
| `GET` | `/egress/graph/history` · `/egress/graph/history/:version` | 版本记录 · 某一版的画布 |
| `POST` | `/egress/graph/rollback` | 回滚到某一版 |
| `GET` | `/egress/mihomo/status` | 核心状态 + 最近 5 分钟每秒流量采样 |
| `POST` | `/egress/mihomo/install` · `restart` · `stop` | 安装或升级（`version` 留空为最新）、重启、停止 |
| `GET` | `/egress/mihomo/proxies` | 节点、策略组、订阅、规则集运行态 |
| `POST` | `/egress/mihomo/delay` · `select` | 测速（单个或全组）· 切换策略组节点（`name` 留空取消固定） |
| `GET` / `POST` | `/egress/mihomo/connections` · `connections/close` | 在途连接 · 关闭一条或全部 |
| `POST` | `/egress/mihomo/providers` | 订阅 / 规则集更新、订阅健康检查 |
| `GET` | `/egress/mihomo/rules` · `logs?after=` | 核心生效的规则 · 日志 |
| `POST` | `/egress/mihomo/dns` · `maintenance` | 经核心解析 · 更新 Geo 数据库或清空 DNS 缓存 |

### 测法

```bash
# UDP 互操作（go-socks5 的 UDP ASSOCIATE、官方 Shadowsocks、最小 Trojan 服务端）与 HTTP/3 经 SOCKS5
go test ./pkg/egress ./pkg/egress/egressh3
# 自动安装真实核心到指定目录，端到端跑一遍（专用入站的 TCP 与 UDP、分流拒绝、切换、热重载、重启）
MIHOMO_INSTALL_DIR=/tmp/mihomo MIHOMO_MIRROR=https://axisnow.gh-proxy.org/ go test ./pkg/egress/mihomo -run TestCoreEndToEnd -v
# 服务层：真实 Postgres + 真实核心（草稿打码与并发保护、应用、回滚、另一进程按画布启动、恢复为环境变量）
AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55441/postgres MIHOMO_BINARY=/tmp/mihomo/bin/mihomo \
  go test ./internal/service -run TestEgressGraphIntegration -v
go test ./internal/domain/egressgraph ./pkg/egress/...
```

## 选路与健康

| 策略 | 行为 |
|---|---|
| `failover`（默认） | 按声明顺序取第一个健康端点 |
| `round_robin` | 在健康端点间轮询 |
| `random` / `weighted` | 随机 / 按 `weight` 加权随机 |
| `latency` | 取探测延迟最低的健康端点 |

- **主动探测**：默认 60s 一轮，通过端点访问 `EGRESS_HEALTH_PROBE_URL`；
  端点没有出网能力（纯内网跳板）时把它的 `probeUrl` 留空，退化为 TCP 连通性检查
- **被动熔断**：拨号失败即计数，连续失败达阈值后进入冷却期，冷却期内不参与选路
- **没有可用端点时返回错误，不会静默降级直连** —— 本该出海的流量走了内网出口，
  是一种会被误当成成功的失败。需要兜底就在端点列表末位显式挂一个 `direct` 端点

## 已接入的出网调用

| 模块 | Profile | 说明 |
|---|---|---|
| OAuth 令牌交换 / 用户信息 | `auth.oauth` | |
| OIDC Discovery / 令牌交换 | `auth.oidc` | 覆盖 Auth0 / Okta / Entra ID |
| SAML IdP metadata | `auth.saml_metadata` | |
| 第三方认证源健康探测 | `auth.provider_health` | |
| 应用级 OAuth 渠道自检 | `oauth.probe` | |
| 支付 REST 渠道（共用 resty 客户端） | `payment.gateway` | Paddle / Lemon Squeezy / Square / Razorpay / Coinbase / 易支付系 / 爱发电 |
| Stripe SDK | `payment.stripe` | 经 `stripe.Backends` 注入 |
| PayPal SDK | `payment.paypal` | 经 `SetHTTPClient` 注入 |
| 对象存储驱动 | `storage.outbound` | S3 / Azure / COS / WebDAV，**直连路径保留 SSRF 校验** |
| GeoIP mmdb 下载 | `geoip.download` | |
| IP 归属地远程兜底 | `location.lookup` | |
| 统一通知出口 Webhook / IM | `notify.webhook` | Slack / Discord 等 |
| Zeabur 邮件 API | `email.zeabur` | |
| IPQualityScore 风控 | `risk.ipqs` | |
| 验证码微服务 | `captcha.audio` / `captcha.chiral` | 目标是本机服务，规则不命中即直连 |

### 刻意没有接入的两处

- **应用远程函数沙箱**（`app_function_sandbox.go`）：URL 由租户提供，
  它自带一套比通用 SSRF 更严的地址白名单（还额外拦文档网段、`240/4` 等）。
  接进网关会同时带来两个问题：削弱那套校验，以及让租户能借平台的境外线路当开放中继。
- **接入协议自检**（`auth_protocol_selftest.go`）：它调用的是平台自己的 API，
  同时承担「三档协议的参考客户端实现」这一角色，不应引入网关依赖。

## 扩展新协议

内置协议全部基于成熟库实现（SOCKS5 用 `golang.org/x/net/proxy`，Shadowsocks 用
`github.com/shadowsocks/go-shadowsocks2`，SSH 用 `golang.org/x/crypto/ssh`，
HTTP CONNECT 用标准库的 `http.Request.Write` / `http.ReadResponse`）。

要加 VMess / Hysteria2 / 自研隧道，实现 `egress.Dialer` 后注册即可，网关核心零改动：

```go
egress.RegisterProtocol("vmess", func(cfg egress.EndpointConfig) (egress.Dialer, error) {
    return myVMessDialer(cfg), nil
})
```

注册后校验层会自动接受该协议，管理端下拉框（`catalog.protocols`）也会自动出现。

> 关于 sing-box / xray-core：二者协议覆盖最全，但分别是 **GPL-3.0** 与 **MPL-2.0**，
> 直接编进本平台会带来许可传染问题，因此没有内置。需要时由使用方自行通过上面的
> 扩展点接入，许可风险自负。

## 实现要点（改代码前先看这里）

- **路由判定发生在 `RoundTrip` 入口**，而不是 `DialContext` 里。只有在那里才同时看得到
  scheme、Host 与调用方 Profile；判定结果随 context 下发给 `Proxy` 与 `DialContext`，
  保证同一次请求的两个回调看到的是同一个决策。
- **`http.Transport` 的连接池按 (proxy, scheme, host) 分桶**。走 `DialContext` 路由时
  池键里没有端点信息，因此已建立的连接会粘在当初拨号的那条线路上 —— `round_robin`
  的实际分布跟随「连接创建」而非「请求」。这是刻意接受的取舍（换取连接复用）。
- **`forward` 模式**（`httpForwardMode=true`）只对明文 `http://` 生效，
  交给 `net/http` 用 absolute-URI 转发；`https://` 一律走 CONNECT 隧道。
  只有在代理禁止 CONNECT 到 80 端口时才需要打开它。
- **端点链（`via`）在构造期做无环与深度校验**（上限 8 跳），运行期递归拼 base dialer。
- **热重载不会掐断在途连接**：被替换掉的端点延迟 60s 才关闭（SSH 端点持有长连接）。
