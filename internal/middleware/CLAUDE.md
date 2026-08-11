# internal/middleware — Gin 中间件

> 面包屑：[Aegis](../../CLAUDE.md) › internal/middleware

## 职责

提供所有 Gin 中间件，在 `internal/transport/http/router.go` 中按路由组装配。

## 中间件一览

| 文件 | 中间件 | 作用 |
|---|---|---|
| `requestid.go` | `RequestID()` | 注入 `X-Request-Id` 响应头 |
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
RequestID → CrashRecovery → Tracing → CORS → gin.Logger → Firewall
  → ReplayGuard → AppGateway → AppEncryption → Location
[路由组中追加] AdminAuth 或 Auth
```

## AdminAccess 的权限判定：appScoped 是最容易把页面搞空的一处

`resolveAdminPermission` 为每条管理路由返回 `(权限点, 是否应用级)`。**「是否应用级」判错不会报错，
只会让页面静默空掉** —— 因为链路是：`appScoped=true` → `extractAdminAppID` 找不到应用标识 →
`40058 缺少有效的应用标识`，而控制台的 React Query 只是拿不到数据，面板照常渲染成「暂无」。

判据很简单：**这个接口的返回值会因应用不同而不同吗？** 不会的一律不是应用级。
典型的「不是应用级」是编译进二进制的静态目录：

| 路由 | 返回什么 | 判定 |
|---|---|---|
| `/api/admin/app/payment-config/methods` | 平台支持哪些支付渠道（`Provider.Describe()`：能力矩阵 + 字段 schema） | `"", false` |
| `/api/app/workflow/node-types` | 工作流节点类型目录 | `"", false` |
| `/api/app/workflow/engine/status` | Temporal 连通性 | `"", false` |
| `/api/admin/oauth-providers/templates` | 第三方登录渠道模板 | `"", false` |

注意这类接口**光把 `appScoped` 改成 false 还不够**，权限点也要一并去掉：
`scopeMatches(assignmentAppID, nil)` 在 `requestAppID` 为 nil 时只认**全局作用域**的授权，
于是应用级管理员会从 400 变成 403 —— 页面同样是空的。它们不含租户数据与凭据，
任意已登录管理员可读是安全的。

`internal/middleware/admin_permission_test.go` 钉住了上述两组用例（静态目录不得 appScoped、
真正的应用资源必须 appScoped），新增路由时往表里加一行即可。

**`/api/admin/platform/*` 是第三类：恒为全局作用域。** 这不是为了页面能显示，
而是整套平台治理的地基 —— `scopeMatches` 在 `requestAppID` 非空时会认可
「绑定到该应用的角色」所持有的权限点，一旦治理路由变成应用级，
被冻结应用自己的管理员只要拿到 `platform:app:govern` 就能给自己解封。同一个测试文件里有用例守它。

## AdminAccess 的第二道闸门：治理只读期

授权通过之后还有一道 `enforceGovernanceAdminWrite`：应用被平台**停运 / 封禁 / 归档**时，
该应用作用域下的一切非 GET 请求对**应用管理员**返回 403（`40336`），
但对平台级管理员（超管或全局 `platform:app:govern`）放行 —— 否则谁都改不动，
连解除治理本身都做不到。

两处刻意的豁免：

- **GET 一律放行**：只读化不等于失明，被治理方还要能看审计与配置排查问题。
- **`/governance/appeals` 放行**：申诉是被治理方唯一的出口，挡住它就成了
  「停运的应用连喊冤都喊不了」。

判定读的是 `PlatformGovernanceService` 的内存快照（不打库），冻结档不设 `blockAdminWrite`，
因此只有停运及以上才会触发这道闸门。

另一个同源坑：`isCompatReadPath` 是**后缀匹配**。写操作路径若不小心以 `/list`、`/detail` 结尾
会被误判成读；反过来漏登记读路径（如 `/deliveries`、`/refunds/refundable`）会让只有读权限的
管理员在打开列表时吃 403。

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
