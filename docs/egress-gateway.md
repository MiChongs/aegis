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
```

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
控制台（platform_settings.platform.egress）  ←  优先
        ↑ 落库 + 立即生效
.env / EGRESS_CONFIG_FILE                    ←  基线
        ↑ fsnotify 热重载
```

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

控制台入口：**配置 → 出海代理**（`/configuration?tab=egress`，仅超管可见）。
一屏同时承载配置（端点 / 规则 / 健康 / 连接参数）与运行态（端点健康、延迟、流量、规则命中数），
右下角内置「连通性自测」与「路由解释」两个排障工具。

排障建议先打 `explain`：

```bash
curl -X POST .../api/admin/system/egress/explain \
  -d '{"host":"api.stripe.com","port":443,"profile":"payment.stripe"}'
```

返回里 `evaluated` 会按顺序列出每条规则是否命中、命中原因（`domainSuffix=stripe.com`），
以及最终选中的端点与端点链。

`test` 把 URL 指向「查询出口 IP」类服务，响应体会被截断保留在 `body` 里，
可以直接确认落地是否真的在境外。

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
| 支付 REST 渠道（共用 resty 客户端） | `payment.gateway` | Paddle / Lemon Squeezy / Square / Razorpay / Coinbase / 易支付系 |
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
