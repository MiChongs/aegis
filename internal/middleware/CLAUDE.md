# internal/middleware — Gin 中间件

> 面包屑：[Aegis](../../CLAUDE.md) › internal/middleware

## 职责

提供所有 Gin 中间件，在 `internal/transport/http/router.go` 中按路由组装配。

## 中间件一览

| 文件 | 中间件 | 作用 |
|---|---|---|
| `requestid.go` | `RequestID()` | 注入 `X-Request-Id` 响应头 |
| `access_log.go` | `AccessLog(log, skipPaths...)` | zap 结构化访问日志，取代 `gin.Logger()` |
| `cors.go` | `CORS(cfg)` | 跨域处理（gin-contrib/cors 封装） |
| `firewall.go` | `Firewall.Handler()` | WAF + 限流 + IP 过滤 |
| `auth.go` | `Auth(authSvc)` | 用户 JWT 认证，注入用户上下文 |
| `admin.go` | `AdminAuth(adminSvc)` / `AdminAccess(adminSvc, appSvc, governanceSvc)` | 管理员会话验证；RBAC 权限检查；治理只读期闸门 |
| `app_gateway.go` | `AppGateway(authProtocolSvc)` | 应用接入网关：按安全等级拆包 `/api/v1/apps/{appKey}/*` |
| `app_gateway_scope.go` | `AppGatewayTokenScope()` | 校验 Bearer 令牌属于路径上的那个应用（挂在 `Auth` 之后） |
| `app_encryption.go` | `AppEncryption(appSvc)` | 请求/响应加密（X-Aegis-Encrypted 头），旧命名空间用 |
| `location.go` | `Location(locationSvc)` | IP 地理位置信息注入 |

## Firewall 详解

`Firewall` 结构体（`firewall.go`）集成：
- **ulule/limiter**：全局 / 认证 / 管理员 三段限流（令牌桶，Redis 存储）
- **Coraza WAF**：OWASP CRS v4 规则集，可选开启，偏执等级 1-4
- **CIDR 过滤**：白名单（AllowedCIDRs）/ 黑名单（BlockedCIDRs）
- **UA 过滤**：默认屏蔽 sqlmap、nikto、acunetix 等扫描器
- **路径前缀过滤**：默认屏蔽 `/.git`、`/wp-admin` 等探测路径
- **请求体限制**：`RequestBodyLimit`（默认 13 MB）

默认限流规则：
- 全局：`1200-M`（每分钟 1200 次/IP）
- 认证路由：`180-M`
- 管理员路由：`360-M`

## 中间件在路由中的组装顺序

```
RequestID → RequestOrigin → CrashRecovery → Tracing → CORS → AccessLog → Firewall
  → ReplayGuard → AppGateway → AppEncryption → Location
[路由组中追加] AdminAuth 或 Auth
```

## AccessLog 为什么取代 gin.Logger()

不是嫌它不好看，而是它与这套系统的日志出口**不是一条链路**：它按自己的格式写
`gin.DefaultWriter` 并自带 ANSI 着色，而平台其余部分全走 zap 结构化输出。
两者混在同一个 stdout 里，采集端按 JSON 行解析，于是每条请求日志都是一次解析失败；
想按状态码或延迟做告警更无从下手 —— 那些值只存在于一行没有结构的文本里。

字段里 `path` 与 `route` 都留：`path` 是实际请求路径（定位那一次具体请求），
`route` 是命中的路由模板（聚合「这个接口整体的延迟与错误率」）。
只留 `path` 的话，带 ID 的路径会把同一个接口打散成成千上万个互不相干的 key。

两处刻意的取舍：4xx 记 **warn** 而不是 info（这一档混着「客户端用错了」和
「有人在探接口」，都该在默认级别下被看到）；`/healthz` 与 `/readyz` **不记**
（编排系统每几秒打一次且永远 200，记下来只会把真实流量冲淡）。

## AdminAccess 的权限判定：规则表在 internal/authz

`resolveAdminPermission` 现在只是一次查表 —— 规则表、权限词汇、角色定义、判定引擎
全部在 [internal/authz](../authz)。这里曾经是 250 行嵌套 switch，三类问题都不会在
编译期暴露：

| 旧写法 | 后果 |
|---|---|
| `strings.Contains(path, "/users")` 不锚定 | 任何路径里出现 users 的接口都被当成用户接口 |
| `isCompatReadPath` 后缀匹配 | 以 `/list` 结尾的**写**接口被判成读；漏登记读路径（`/deliveries`）让只读管理员打开列表就 403 |
| 分支顺序即优先级，顺序藏在嵌套里 | 新规则被上面的宽前缀整个遮住，不报错、只是永不生效 |

现在是一张有序、按段锚定的规则表，`:param` 吃一段、`*` 吃剩余（含零段）。
两条测试钉住它：`internal/authz/testdata/route_permissions.json` 逐条守住
**941 条真实路由**的判定结果与重构前一致；`TestNoUnreachableRouteRule`
保证没有哪条规则被上面的规则完全遮住（旧实现里就沉淀了三段这样的死代码）。

新增管理端路由时往 `adminRouteRules` 补一行。忘了补的表现是 **40315**
「该管理端接口尚未登记权限规则」，而不是静默放行；
`TestEveryAdminRouteHasAuthzRule` 会在 CI 里先一步抓到。

### appScoped 仍是最容易把页面搞空的一处

**「是否应用级」判错不会报错，只会让页面静默空掉** —— 链路是：`ScopeApp` →
`extractAdminAppID` 找不到应用标识 → `40058 缺少有效的应用标识`，
而控制台的 React Query 只是拿不到数据，面板照常渲染成「暂无」。

判据很简单：**这个接口的返回值会因应用不同而不同吗？** 不会的一律不是应用级。
典型的「不是应用级」是编译进二进制的静态目录（支付渠道目录、工作流节点类型、
第三方登录模板）。注意这类接口**光把作用域改成全局还不够**，权限点也要一并去掉：
全局作用域只认全局授权，应用级管理员会从 400 变成 403，页面同样是空的。

`/api/admin/platform/*` 是第三类：**恒为全局作用域**。这不是为了页面能显示，
而是整套平台治理的地基 —— 作用域匹配在请求带应用时会认可「绑定到该应用的角色」
所持有的权限点，一旦治理路由变成应用级，被冻结应用自己的管理员只要拿到
`platform:app:govern` 就能给自己解封。

`POST /api/admin/apps` 是第四类：**不要权限点**。要求 `app:write` 会造出死锁 ——
自助注册出来的管理员一条角色都没有，而唯一能让他拿到权限的动作（建自己的第一个
应用、成为它的 app_admin）本身就被 `app:write` 挡住。真正的闸门
（平台开关 + 每人配额）在 `AdminService.EnsureCanCreateApp`。
放开的只有「建」这一个动作，列表与改既有应用的闸门不变。

### 没登记的路由：默认拒绝，但要说清是"没登记"

规则表没命中时中间件回 `40315`，文案明说是权限规则未登记。与「没权限」共用
`40312` 那句通用文案时，一条漏登记的新路由会伪装成一次权限配置问题，
于是排查方向从「补一行规则表」歪到「给这个人加授权」。

同理 `writeAdminError` 只对**非** `AppError` 兜底：`AdminService.Authorize` 的
拒绝文案里带着缺失的权限点、作用域、以及（被 deny 挡住时）那条策略行本身，
在传输层改写成通用文案等于把刚算出来的信息丢掉。

## AdminAccess 的第二道闸门：治理只读期

授权通过之后还有一道 `enforceGovernanceAdminWrite`：应用被平台**停运 / 封禁 / 归档**时，
该应用作用域下的一切非 GET 请求对**应用管理员**返回 403（`40336`），
但对平台级管理员（超管或全局 `platform:app:govern`）放行 —— 否则谁都改不动，
连解除治理本身都做不到。

两处刻意的豁免：**GET 一律放行**（只读化不等于失明，被治理方还要能看审计排查问题）、
**`/governance/appeals` 放行**（申诉是被治理方唯一的出口，挡住它就成了
「停运的应用连喊冤都喊不了」）。判定读 `PlatformGovernanceService` 的内存快照（不打库），
冻结档不设 `blockAdminWrite`，因此只有停运及以上才会触发这道闸门。

## AppGateway —— 应用接入网关

`/api/v1/apps/{appKey}/*` 是接入方唯一需要认识的命名空间。三档安全等级
**共用同一批路径与同一份 JSON 结构**，中间件只决定请求怎么拆包：

| 等级 | 中间件做的事 |
|---|---|
| `standard` | 直通。只校验路径 appKey 与可选的 `X-Aegis-App-Key` 头是否一致 |
| `signed` | 校验 HMAC-SHA256 签名（v2，含原样 query）+ 时间窗 + 一次性 Nonce |
| `sealed` | 先验签，再解开 Transport v2 加密载荷，并把响应重新封包 |

### 三种请求形状，三条拆包路径

只按「有 body」一种形状实现，会让整类接口进不了这个命名空间：

| 形状 | 密文在哪 | 拆包后 |
|---|---|---|
| 有请求体（POST/PUT） | body | 明文放回 body，Content-Type 取 `X-Aegis-Plain-Content-Type`，缺省 JSON |
| 无请求体（GET/DELETE/HEAD） | `?_payload=` | 明文是 query string，回填 `URL.RawQuery` |
| 上传（multipart） | body（整段 multipart 被加密） | 原始 Content-Type 从 `X-Aegis-Plain-Content-Type` 还原 |

**GET 不能带 body**：HTTP 允许，但 OkHttp / URLSession / 浏览器 fetch 全都拒绝构造，
恰好就是 Android / iOS / Web 三端。所以无请求体的方法只能走 query。
没有查询参数时 `_payload` 是**空串的密文**（AEAD 对空明文照样产出 tag），
于是「有没有参数」不构成分支，客户端一套代码走到底。

`signed` 档**不碰载荷**：Content-Type 必须原样保留，否则 multipart 上传会被下游
当成 JSON 解析，而报错指向的是「字段绑定失败」，与文件毫无关系。

请求体上限读 `authprotocol.MaxRequestBytes` / `MaxUploadBytes`（`/config` 下发同一份值），
上传类按后者放宽 —— 一刀切成 1 MiB 会让「换头像」以一个和图片无关的错误失败。

两条不能改的约束：

1. **`/config` 永远免包装可读**。客户端得先知道自己该用哪一档，
   否则会陷入"要读配置得先按配置包装"的死锁。
2. **sealed 仍然要验签**。AEAD 只证明密文没被改过；服务端公钥是公开的，
   任何人都能造出合法密文。签名才证明调用方持有 `appSecret`。

签名与加密各自走独立的 Redis 防重放作用域（`appv1:sig:*` 与 `appv1:sealed:<keyId>:*`），
所以 sealed 档同一个 nonce 过两道校验不会自己撞自己。

`ReplayGuard` 对整个 `/api/v1/apps/` 前缀让开（见 `replay_guard.go` 内注释）：
网关自带的防护严格更强，而它的 body 指纹去重在这里只会误杀。

## AppEncryption

使用 `X-Aegis-Encrypted` / `X-Aegis-Nonce` / `X-Aegis-Algorithm` 头识别加密请求。加密密钥与应用（App）绑定，从 AppService 查询。明文 Content-Type 由 `X-Aegis-Plain-Content-Type` 携带。

## 测试文件

- `firewall_test.go`：防火墙规则单元测试
- `app_encryption_test.go`：加密中间件单元测试
