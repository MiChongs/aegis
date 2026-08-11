# internal/transport/http — HTTP 传输层

> 面包屑：[Aegis](../../CLAUDE.md) › internal/transport/http

## 职责

Gin 路由注册、HTTP Handler 实现、请求/响应 DTO、OpenAPI 规范生成、Postman 集合导出。

## 关键文件

| 文件 | 说明 |
|---|---|
| `router.go` | `NewRouter()` — 路由注册入口（单文件，含所有路由组） |
| `dto.go` | 通用请求/响应 DTO（用户、认证、通知等） |
| `dto_extra.go` | 扩展 DTO（角色、版本、站点等） |
| `dto_storage.go` | 存储相关 DTO |
| `admin_dto.go` | 管理员专用 DTO |
| `system_settings_dto.go` | 平台设置 DTO |
| `app_gateway_handlers.go` | 应用接入网关的认证生命周期 Handler（`/api/v1/apps/:appkey/auth/*`） |
| `app_gateway_account_handlers.go` | 网关的账户与内容部分：把只认 `appid` 的旧 handler 接进以 appKey 定位的命名空间 |
| `auth_protocol_handlers.go` | 接入配置的管理端 Handler（策略 / 应用密钥 / 自检 / 传输密钥） |
| `docs_gateway.go` | 网关接口的 OpenAPI 元数据（由接入目录生成，多平台客户端从这一段产出） |
| `docs_route_models.go` | **机器生成**：路由 → 请求模型映射，不要手工编辑 |
| `admin_handlers.go` | 管理员相关 Handler |
| `avatar_handlers.go` | 头像上传 Handler |
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
GET  /healthz                    健康检查
GET  /readyz                     就绪检查
GET  /api/system/monitor         系统监控（公开）
GET  /api/app/public             App 公开信息

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
| 1 | `docs_route_models.go`（机器生成，勿手改） | 由路由表反查 handler 的绑定目标推导，覆盖最广 |
| 2 | `manualRouteDocs()` | 手工登记的摘要与响应示例，只覆盖少数重点接口 |
| 3 | `gatewayRouteDocs()` | 网关命名空间，由接入目录生成 |

第 1 层的重新生成方式：取运行时 gin 路由表 → 找到每条路由的 handler →
回源码看它 `bind` / `bindLimitedJSON` / `c.ShouldBind*` 到了哪个**具名**类型。
因此每一项都与 handler 真正解析的结构一致。覆盖不到的只有两类，都不是遗漏：
请求体是匿名 struct（没有类型名可引用，应提成具名类型），或该接口本来就没有请求体。

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
