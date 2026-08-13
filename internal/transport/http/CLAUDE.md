# internal/transport/http — HTTP 传输层

> 面包屑：[Aegis](../../CLAUDE.md) › internal/transport/http

## 职责

Gin 路由注册、HTTP Handler 实现、请求/响应 DTO、OpenAPI 规范生成、Postman 集合导出。

## 关键文件

| 文件 | 说明 |
|---|---|
| `router.go` | `NewRouter()` — 中间件栈 + 按域编排（不含任何路由注册） |
| `router_deps.go` | `RouterDeps` 具名依赖 + `Handler` 装配 |
| `router_*.go` | 各域的路由注册，详见下表 |
| `route_groups.go` | **命名空间规则表**（单一事实源）+ `deriveTags()` + `RouteInventory()` |
| `route_chains.go` | 接住 `gin.DebugPrintRouteFunc`：灭掉调试滚屏 + 采集中间件链深度 |
| `route_methods.go` | `NoMethod` 收口：405 + `Allow`，OPTIONS 兜底成 204 |
| `testdata/routes.golden` | 全量路由黄金快照，由 `TestRouteTableMatchesGoldenSnapshot` 钉住 |
| `dto.go` | 通用请求/响应 DTO（用户、认证、通知等） |
| `dto_extra.go` | 扩展 DTO（角色、版本、站点等） |
| `dto_storage.go` | 存储相关 DTO |
| `admin_dto.go` | 管理员专用 DTO |
| `system_settings_dto.go` | 平台设置 DTO |
| `app_gateway_handlers.go` | 应用接入网关的认证生命周期 Handler（`/api/v1/apps/:appkey/auth/*`） |
| `app_gateway_account_handlers.go` | 网关的账户与内容部分：把只认 `appid` 的旧 handler 接进以 appKey 定位的命名空间 |
| `app_content_handlers.go` | 应用级内容中心管理端：Banner（列表 / 排序 / 图片上传）、公告（分页过滤）、总览 |
| `auth_protocol_handlers.go` | 接入配置的管理端 Handler（策略 / 应用密钥 / 自检 / 传输密钥） |
| `docs_gateway.go` | 网关接口的 OpenAPI 元数据（由接入目录生成，多平台客户端从这一段产出） |
| `docs_route_models.go` | **由 `scripts/docsgen` 生成**：路由 → 请求模型映射，勿手改 |
| `admin_handlers.go` | 管理员相关 Handler |
| `avatar_handlers.go` | 头像：永久地址取图 + 上传 / 移除 / 历史 / 恢复（见 [docs](../../../docs/avatar.md)） |
| `feature_handlers.go` | 功能性 Handler（密码策略、积分、版本等） |
| `monitor_handlers.go` | 系统监控 Handler |
| `realtime_handlers.go` | WebSocket Handler |
| `storage_handlers.go` | 存储 Handler |
| `platform_governance_dto.go` / `platform_governance_handlers.go` | 平台治理：全站总览 / 治理动作 / 批量 / 强制下线 / 流水 / 申诉；应用侧只读视图与申诉 |
| `organization_dto.go` / `organization_handlers.go` | 组织 / 部门 / 成员 / 岗位 / 角色 / 邀请 / 导入导出（权限判定下沉到 service） |
| `org_access_handlers.go` | 审批链与实例、权限模板、协作组 |
| `system_settings_handlers.go` | 平台设置 Handler |
| `egress_dto.go` / `egress_handlers.go` | 出海代理网关管理端（配置 / 自测 / 路由解释 / 探测，限超管） |
| `ticket_dto.go` / `ticket_handlers.go` | 工单管理端 + 用户端 Handler（权限判定下沉到 TicketService） |
| `notify_handlers.go` | 统一通知出口管理端 Handler（渠道 / 订阅 / 模板 / 投递记录） |
| `docs.go` | `BuildOpenAPISpec()` — OpenAPI 规范生成；`/openapi.json` 出口 + `/docs` 302 到开发者门户 |
| `postman.go` | `BuildPostmanCollection()` — Postman 集合生成 |

## 路由组织方式

```
GET|HEAD /healthz                健康检查（HEAD 是负载均衡器与 uptime 监控的默认发法）
GET|HEAD /readyz                 就绪检查
GET  /api/system/monitor         系统监控（公开）
GET  /api/app/public             App 公开信息
GET|HEAD /api/avatars/:token     永久头像地址（**免登录是前提**：它出现在 <img src>
                                 与邮件正文里，那两处都没机会带 Authorization 头；
                                 防遍历靠地址自带的签名。docs/avatar.md）
                                 HEAD 供邮件客户端的图片代理与链接检查器预探

# 应用接入网关（接入方唯一需要认识的命名空间，见 docs/app-integration.md）
/api/v1/apps/:appkey/*           免登录部分：config / captcha / auth.* / banners /
                                 notices / version.check
                                 [middleware.AppGateway 按安全等级拆包]
/api/v1/apps/:appkey/*           需 Bearer 的部分：me.* / signin / points /
                                 leaderboard / notifications / wallet / vip / pay /
                                 storage / tickets
                                 [+ middleware.Auth + AppGatewayTokenScope]
                                 接口目录：internal/service/auth_protocol_catalog.go
                                 （随 /config 下发，与路由由测试双向钉死）

# 管理员认证
POST /api/admin/auth/login
GET  /api/admin/auth/me          [AdminAuth]
POST /api/admin/auth/logout      [AdminAuth]

# 管理员 profile
GET|PUT /api/admin/profile       [AdminAuth]
POST    /api/admin/profile/avatar [AdminAuth]

# 管理员功能路由组（均需 AdminAccess）
/api/admin/*                     各类管理 API
/api/app/password-policy/*       密码策略
/api/app/points/*                积分管理
/api/admin/app/version/*         版本管理（compat）
/api/admin/tickets/*             工单（中间件只做进模块的粗粒度闸门，细粒度在 service 层）
/api/admin/notify/*              统一通知出口（写操作叠加 RequireSuperAdmin）
/api/admin/platform/*            平台治理（恒全局作用域，绝不 appScoped）
/api/admin/system/organizations/:orgId/*
                                 组织架构（UUID 定位，子资源一律挂在组织之下，
                                 组织归属由路径携带，service 层据此做隔离校验）

# 用户 API（需 Auth 中间件）
/api/user/tickets/*              用户自助提单 / 追问 / 评价 / 撤单
/api/auth/*                      旧明文认证命名空间（由 App 的 allowLegacy 开关控制）
/api/user/*                      用户信息
/api/notification/*              通知
/api/storage/*                   存储

# WebSocket
GET /api/ws                      实时通信入口
```

## 路由注册的分域

`NewRouter` 只做三件事：装中间件栈、建 `Handler`、按原顺序调用各域的 `register*`。
近千条注册分在这些文件里：

| 文件 | 注册函数 | 覆盖 |
|---|---|---|
| `router_public.go` | `registerPublicRoutes` / `registerPublicCaptchaAndLegalRoutes` | 站点页面、健康探针、公开元数据、验证码、法律文本 |
| `router_gateway.go` | `registerGatewayRoutes` | `/api/v1/apps/:appkey/*` 接入网关 |
| `router_admin.go` | `registerAdminAuthRoutes` / `registerAdminAppRoutes` / `registerAdminModuleRoutes` | 管理端认证、应用管理、工单与通知 |
| `router_platform.go` | `registerPlatformGovernanceRoutes` / `registerPlatformStorageRoutes` | 平台治理与平台级存储（恒全局作用域） |
| `router_admin_system.go` | `registerAdminSystemRoutes` / `registerPlatformBannerActiveRoute` | `/api/admin/system/*` 平台设置 |
| `router_org.go` | `registerOrgRoutes` | 组织架构（收 `*gin.RouterGroup`，不自建组） |
| `router_compat.go` | `registerAppCompatRoutes` / `registerAdminAppConfigRoutes` / `registerWorkflowCompatRoutes` | `/api/app/*` 与 `/api/admin/app/*` 旧命名空间 |
| `router_user.go` | `registerLegacyAuthRoutes` / `registerUserRoutes` / `registerCommerceRoutes` | 旧明文认证、用户端、支付与钱包 |

两条约束：

**注册顺序不能重排。** gin 用前缀树存路由，静态段与参数段在同一层共存时是否 panic
与注册先后有关（`/organizations/tree` 与 `/organizations/:orgId` 就是这种共存）。
`NewRouter` 里那十六行严格复刻拆分前的顺序，按字母重排等于夹带一个只在启动时才炸的行为变更。
`TestOrgRoutesRegisterWithoutConflict` 守这一条。

**子域收路由组，不自建。** `registerOrgRoutes` 的入参是**已经配好中间件的** `adminSystem`
组而不是 `*gin.Engine` —— 重新建一个同路径的组会另起一条中间件链，鉴权与审计就都对不上了。

## 方法收口：405 / OPTIONS / HEAD

路径存在、方法不匹配的请求由 `route_methods.go` 的 `methodNotAllowed` 收口。
它是运维与接入方每天都在制造、却从不出现在功能用例里的那类请求，
因此三条行为各有一条测试钉住（`route_methods_test.go`）。

| 情形 | 响应 | 为什么 |
|---|---|---|
| 方法不匹配 | `405` + `Allow` + `40500` | 见下 |
| OPTIONS 落到兜底 | `204` + `Allow` | CORS 管不到的那两类 |
| HEAD（探针与头像） | 同 GET，无 body | gin 不会让 GET 顺带响应 HEAD |

**405 而不是 501。** 这个位置曾经回 `501 服务能力暂未开放`，三处都不对：
501 的定义是「服务器不认识这个方法」（对任何资源都不支持），而这里的事实是
方法认识、只是这个资源不接受；501 属于 **5xx**，于是「调用方把 GET 写成了 POST」
会被计成服务端故障，抬高错误率与 SLO 违约、把排查方向带向后端；HTTP 客户端与 SDK
普遍**对 5xx 重试、对 4xx 不重试**，用错方法的调用方会带着必然失败的请求一直打回来。

`Allow` 头是 gin 在 `handleHTTPRequest` 里算好后预置的（连同 405 状态），
这里只是不把它改坏 —— RFC 9110 §15.5.6 要求 405 必须携带它。

**OPTIONS 为什么要在这里兜底。** gin-contrib/cors 只处理**带 `Origin` 头**的请求，
且 CORS 未启用时 `middleware.CORS` 整个是直通的。因此两类 OPTIONS 会漏到这里：
关闭 CORS 时浏览器发出的预检，以及非浏览器客户端拿 OPTIONS 探能力。
回 405 等于对「这个资源支持什么方法」回答「不支持提问」，而答案就在 `Allow` 里。
带 Origin 的预检仍由 CORS 中间件在更靠前的位置短路，两条路径不打架。

**HEAD 必须显式注册。** gin 按方法分树，`GET` 不会顺带响应 `HEAD`
（与 `net/http` 的 `ServeMux` 不同，很容易想当然）。而负载均衡器的存活检查、
邮件客户端的图片代理、链接检查器发的都是 HEAD，收到 405 的表现是
「服务好着呢，探针全红」。因此 `/healthz`、`/readyz` 与两个头像地址各注册一条
HEAD，复用 GET 的 handler，响应体由 net/http 按 HEAD 语义自动丢弃。

两处配套约束：

- **不要用 `httptest.NewRecorder` 断言「HEAD 无 body」**。body 的丢弃是 net/http
  在 `response.write` 里按 `req.Method == "HEAD"` 做的，Recorder 只是个 buffer，
  不实现那一层 —— 拿它断言会得到假阳性的失败。`TestHeadIsServedOnProbeAndAvatarRoutes`
  因此起真实服务器。
- **HEAD 不进 OpenAPI**（`docs.go` 里显式跳过）。它与同路径的 GET 共用契约，
  单独列出只会让生成式客户端多出一批名字雷同、永远拿不到数据的方法。

与防火墙层的 501 不冲突：`middleware.blockedMethod` 拦的是
CONNECT / TRACE / TRACK / DEBUG，那是**服务器整体不支持**的方法，501 正是它的定义。

## 路由清单与分组规则

`route_groups.go` 里那张 `routeGroups` 表是「一条路由属于哪个命名空间」的**单一事实源**，
同时喂两个消费方，刻意不许它们各有一套：

| 消费方 | 用它的哪一列 | 影响 |
|---|---|---|
| `deriveTags()` | `Tag`（英文） | OpenAPI 标签 → 门户分组、生成式客户端分包 |
| `RouteInventory()` | `Realm` / `Title` / `Auth`（中文） | 启动横幅的「路由」分区、`routes` 子命令 |

**匹配单位与展示单位是分开的**：一条规则一个 `Tag`（OpenAPI 标签的粒度是既定事实），
但多条规则可以共用一个 `Title`。「公开元数据」里既有 `/api/app/public`（历史上归
`App Compat`）也有 `/api/public/branding`（归 `API`），一组一个 Tag 就只能二选一。

四条测试钉住这张表：

| 测试 | 守什么 |
|---|---|
| `TestDeriveTagsMatchesLegacySwitch` | 逐条比对收敛前的旧 `switch`，OpenAPI 标签零漂移 |
| `TestEveryRouteMatchesAnExplicitGroup` | 没有路由落进兜底分组（新命名空间必须显式登记） |
| `TestRuleTableHasNoUnreachableRule` | 没有被上层规则完全遮住的死规则（`Fallback: true` 例外） |
| `TestRuleLabelsAvoidAmbiguousWidthRunes` | 标注里不含 East Asian Ambiguous 宽度字符 |

最后一条不是文案洁癖：`·`「×」「○」在中日韩控制台里被 go-runewidth 算成 2 列、
实际渲染 1 列，会把路由表右边框顶歪，且只在特定 locale 下发作。分隔一律用 ASCII 的 ` / `。

### gin 的路由调试输出为什么必须接住

`gin.DebugPrintRouteFunc` 同时是两件事的唯一入口：

- **灭掉滚屏**：debug 档下 gin 会把每条路由打成一行，近千条就是近千行，正好冲掉启动横幅
- **拿到链深度**：`engine.Routes()` 返回的 `RouteInfo` 只有 Method / Path / Handler，
  **不含中间件链**，那个 `(14 handlers)` 只在这个回调的 `nuHandlers` 形参里出现

release 档 gin 根本不回调，链深度全为 0，清单里那一列由 go-pretty 的
`SuppressEmptyColumns` 自动消失 —— 这也是 `routes` 子命令在生产机器上仍然有用的原因：
gin 只在 debug 档打路由，而「这个部署暴露了哪些接口」恰恰是生产环境才需要盘点的事。

## Handler 结构

```go
type Handler struct {
    auth, admin, user, signin, points, notifications,
    app, site, version, roleApp, email, payment,
    workflow, storage, avatar, monitor, realtime, system
}
```

所有 Handler 方法签名：`func (h *Handler) XxxHandler(c *gin.Context)`

## DTO 规范

- 请求 DTO：`binding:"required"` 标签用于 gin 验证
- 响应统一通过 `pkg/response.OK(c, data)` 或 `response.Error(c, status, code, msg)`
- 枚举字段使用字符串（与前端对齐）
- 分页参数：`page` + `pageSize`，默认值在 DTO 层设置

## OpenAPI / Postman 生成

```bash
# 导出 OpenAPI JSON
go run ./cmd/server openapi docs/openapi.json

# 导出 Postman 集合
go run ./cmd/server postman
# 输出至 docs/postman/aegis-api-cn.postman_collection.json
```

生成器通过 `getkin/kin-openapi` 解析 Gin 路由树，配合 `DefaultDocsOptions()` 注入元数据。

### 请求体 schema 的三层叠加（后者覆盖前者）

多平台客户端整个是从这份规范生成的，缺 schema 的写接口会产出**没有参数的空方法**：

| 层 | 来源 | 覆盖面 |
|---|---|---|
| 1 | `docs_route_models.go`（`scripts/docsgen` 生成，勿手改） | 由路由表反查 handler 的绑定目标推导，覆盖最广 |
| 2 | `manualRouteDocs()` | 手工登记的摘要与响应示例，只覆盖少数重点接口 |
| 3 | `gatewayRouteDocs()` | 网关命名空间，由接入目录生成 |

### 第 1 层为什么必须由生成器产出

这一层回答的是「**这条路由的请求体是什么类型**」，而这个问题**没有任何 OpenAPI
库能替你回答**：gin 的 handler 签名是 `func(*gin.Context)`，请求类型在运行时
已经被擦掉了。kin-openapi 只能从「已知的 Go 类型」反射出 schema，
却无从知道哪条路由对应哪个类型。出路只有两条：静态分析源码，或改造全部
handler 的签名（huma 那类框架的做法，代价是 1000+ 个 handler 全部重写）。

`scripts/docsgen` 走前者，把两份**都不是人手维护**的事实交叉：

| 来源 | 得到 |
|---|---|
| 运行时装配一次真实路由表 | method + path → handler |
| `x/tools` 加载本包类型信息 | handler → 它绑定的具名类型 |

绑定只认五种写法（`bind` / `bindLimitedJSON` / `c.ShouldBindJSON` /
`c.ShouldBindQuery` / `c.ShouldBind`），它们覆盖了全部 450 处绑定。
另有两处不做不行：

- **沿调用图下探**。大量 handler 把请求解析**委托**出去 ——
  `AdminEmailConfigCreate` 只有一行 `h.adminEmailConfigSave(c, 0)`。
  只看 handler 自己的函数体，这些接口会整片漏成「没有请求体」。
  优先级是自己的绑定压过转手的，反过来会张冠李戴。
- **按 HTTP 方法分流**。同一个 handler 同时挂在 GET 与 POST 上是本仓库的常态，
  不分流的话 GET 会拿到请求体模型、被渲染成一串根本不存在的 query 参数。

跨包的具名类型（handler 直接绑 `service.PolicyOverrideInput` 这类）由产物
`import` 进来，不是「引用不到」。真正覆盖不了的只有匿名 struct ——
生成器会把它们逐条列出来，**提成具名类型即可纳入规范**，这是唯一一类
能修好的缺口，因此不与「本来就没有请求体」混为一谈。

```bash
go generate ./internal/transport/http/   # 改了 handler 的请求绑定后跑，产物一起提交
go run ./scripts/docsgen -check          # 校验产物是否过期（CI 的 docsgen job）
```

**过期不会有任何编译错误**，只会让新接口在生成出来的 SDK 里变成一个不带参数、
调过去必然 400 的空方法 —— 所以它必须由 CI 把关，而不是靠记性。
两条测试从另一侧守着：`TestGeneratedRouteModelsHaveNoDeadEntries`（表里没有
指向空气的条目）与 `TestWriteRouteRequestBodyCoverageHasNotRegressed`（覆盖不倒退）。

网关接口另外三件事必须做到位，否则生成的客户端不可用：**短 `operationId`**
（默认值是从路径拼的 `post__api__v1__apps__by_appkey__auth__login`）、
**`requestBody`**、**`security`**。`docs_gateway_test.go` 逐条钉住。

### 文档页面归属

后端**不再渲染任何 HTML 文档页**，只提供机器可读的 `/openapi.json`：

| 路由 | 行为 |
|---|---|
| `GET /openapi.json` | 实时生成的 OpenAPI 规范（唯一事实源） |
| `GET /docs` | 302 → `DocsOptions.PortalURL`（默认 `/developers`） |
| `GET /docs/tags/:slug` | 302 → `<PortalURL>/api?tag=<slug>`，门户按 slug 反查分组 |

文档本体由 aegis-console 的公开门户 `/developers` 承载（快速接入 + 接口浏览）。
目标地址通过 `DOCS_PORTAL_URL` 配置，经 `NewRouter(..., docsPortalURL string)` 注入。
