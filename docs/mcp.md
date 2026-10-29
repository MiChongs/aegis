# MCP 密钥与 MCP 服务

管理员给 AI 助手签发长期凭证（MCP 密钥），助手经 [aegis-mcp](../mcp/README.md)（Python MCP 服务）
以该管理员的身份调用管理端接口。

## 判定：两层交集，每次请求现查

一把密钥恒代表签发它的管理员。一次调用能不能做，是两层交集：

1. **管理员本人此刻的权限**：角色、临时权限、显式拒绝照常判定。密钥不缓存角色，
   撤了角色、停了账号，下一次调用就收窄或失效。
2. **密钥自己的范围**：

| 字段 | 含义 |
|---|---|
| `permissions` | 权限点模式。具体代码（`app:user:read`）、整组（`ticket:*`，以后新增到这一组的也生效）、`*`（全部） |
| `appIds` | 空为不限应用；限定后只能访问这些应用，且**不能调用平台级写接口** |
| `readOnly` | 只允许查询：GET / HEAD，以及规则表判为读权限点（`*:read`）的 POST 查询接口（支付配置列表、订单详情、积分统计、发布模拟等） |
| `allowIps` | IP 或 CIDR，空为不限 |
| `rateLimitPerMinute` | 每分钟调用上限（Redis 固定窗口），0 为不限；计数器故障时放行 |
| `expiresAt` | 空为永不过期 |

执行点：

| 位置 | 管什么 |
|---|---|
| `AdminService.Authorize` 第一道（`authorizeMCPScope`） | 权限点与应用范围。超管的密钥同样受约束；service 层的细粒度判定（工单动作等）也经过这里 |
| `middleware.enforceMCPKeyRoute`（AdminAccess 内、Authorize 之前） | 只读；无权限点的路由；限定应用的密钥不能平台级写 |
| `RequireSuperAdmin` | 超管专属接口要求密钥为 `*` |
| `AdminAuth` 白名单（`mcpKeySessionRoutes`） | 只挂 AdminAuth 的「管理员自己」那组路由只放行 `/auth/me`、`/apps`、`/system/banners/active`、收件箱读取与 `/api/admin/mcp/*`，其余（资料、密码、两步验证、MCP 密钥管理、登出）一律 40167 |
| `ensureMCPKeyManager` | 密钥管理接口在 service 层再拦一次：不能拿密钥签发或放宽密钥 |

**无权限点的路由**（规则表里 `Permission == ""`，细粒度下沉 service 层）：

- 工单 `/api/admin/tickets*`、组织 `/api/admin/system/organizations*`：service 层对超管直接短路，
  密钥范围在那里没有落点，所以入口处要求密钥覆盖 `ticket:read` / `org:dept:read`；
  超管的密钥写这两个模块要求覆盖整组（`ticket:*` / `org:*`）。非超管的写交给 service 层逐项判。
- 其余（工作台、各类静态目录等）读放行，写要求 `*`。

生效权限快照（`/api/admin/auth/me`、`/api/admin/mcp/whoami`）按密钥范围收窄，说的就是实际能做的。

## 凭证

- 明文 `aegis_mcp_` + 32 字节 base64url，只在签发与轮换时返回一次；库里存 SHA-256（唯一索引），
  `key_prefix`（前 16 位）供辨认。
- 中间件按前缀分流：`aegis_mcp_` 开头走密钥校验，其余按会话 JWT。
- 审计：`AccessContext.TokenID = "mcp:<前缀>"`，审计日志的会话列即是它，可按 `sessionId` 回查一把密钥做过什么。
- 使用记录（`last_used_at` / `last_used_ip` / `use_count`）每把密钥最多 30 秒落一次库。
- 每个管理员最多 20 把未撤销的密钥。

## 接口

签发与管理，**只接受控制台会话**（`/api/admin/profile/mcp-keys`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/mcp-keys` | 我的密钥；超管 `?scope=all` 看全部 |
| GET | `/mcp-keys/catalog` | 权限目录 + 我的生效权限（签发表单用） |
| POST | `/mcp-keys` | 签发，返回 `secret` |
| GET / PUT | `/mcp-keys/:keyId` | 查看 / 修改范围（PUT 是整体替换；不带过期字段保持原值，`neverExpires: true` 改为永不过期） |
| POST | `/mcp-keys/:keyId/rotate` | 换明文，旧明文立即失效 |
| POST | `/mcp-keys/:keyId/enable`、`/disable` | 启用 / 停用（可逆） |
| DELETE | `/mcp-keys/:keyId` | 撤销（不可逆） |

MCP 服务自述（AdminAuth，密钥与会话都能调）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/mcp/whoami` | 管理员、密钥范围、收窄后的生效权限、可访问的应用 |
| GET | `/api/admin/mcp/endpoints` | 管理端接口目录（`q` / `group` / `method` / `allowed=1` / `limit`），逐条给出 `allowed` 与 `reason` |

目录的 `allowed` 是按同一套规则的**预判**，超管专属与 service 层细粒度判定看不到，以实际调用为准。

## 错误码

| 码 | HTTP | 含义 |
|---|---|---|
| 40160 | 401 | 密钥无效（含已被轮换的旧明文） |
| 40161 | 401 | 已过期 |
| 40162 | 401 | 已停用 / 已撤销 |
| 40163 | 403 | 来源 IP 不在允许列表 |
| 40164 | 403 | 权限点不在密钥范围（含无权限点模块、超管专属接口） |
| 40165 | 403 | 应用不在密钥范围 / 限定应用的密钥调用平台级写接口 |
| 40166 | 403 | 只读密钥调用写接口（非 GET 且权限点不是 `*:read`） |
| 40167 | 403 | 该接口只接受控制台会话 |
| 40168 | 429 | 超出每分钟调用上限 |
| 40169 | 400 | 签发 / 修改参数无效 |
| 40498 | 404 | 密钥不存在或已撤销 |

## 文件

| 位置 | 内容 |
|---|---|
| `migrations/postgres/000133_admin_mcp_keys.*` | `admin_mcp_keys` 表 |
| `internal/domain/admin/mcp_key.go` | 类型、范围匹配（`MCPPermissionMatches`） |
| `internal/service/admin_mcp_key_service.go` | 签发、校验、范围判定、快照收窄、使用记录节流 |
| `internal/middleware/admin_mcp_key.go` | 分流、AdminAuth 白名单、路由级约束 |
| `internal/transport/http/admin_mcp_key_handlers.go`、`router_mcp.go` | 管理接口与自述接口 |
| `aegis-console/src/components/profile/*mcp-key*` | 控制台「个人资料 → MCP 密钥」 |
| `mcp/` | Python MCP 服务：FastMCP 4（mcp SDK 2.3），`core/` 内核 + `modules/` 25 个领域模块、432 个工具，清单见 `mcp/TOOLS.md` |

## 测法

- 单元：`go test ./internal/middleware/ -run MCP`、`go test ./internal/service/ -run MCP`；`cd mcp && .venv/bin/python -m pytest`（267 个，含对 routes.golden 与 route_permissions.json 的约束检查）。
- 端到端（2026-10-28 跑通）：本地 compose 起 postgres / redis / nats，`aegis-server migrate` 后以 `HTTP_PORT=8099` 启动；
  用 `ADMIN_BOOTSTRAP_*` 登录拿会话令牌，签发一把只读限定应用的密钥与一把 `*` 可写密钥，
  逐项验证 40160–40167 的触发；再用 `mcp.Client(StdioServerParameters(command="aegis-mcp", env=...))`
  走 stdio 调工具，`aegis-mcp --transport streamable-http` 下用不同的 `Authorization` 头验证按客户端分密钥。
- 全量冒烟（2026-10-28）：用一把 `*` 可写密钥建一次性应用，按每个工具的 input schema 自动填参逐个真实调用（影响全站的写工具跳过），
  把「后端报参数格式 / 缺字段 / 500」的结果挑出来逐条对 Go 源码核实；再用只读密钥跑全部只读工具，确认没有 40166 / 40164。
  首轮查出并修掉：旧版本保存缺 appid、工单来源取值、工单分类 / 处理组 key 必填。
- 核心链路（同日）：本地库插一个用户，走资料修改 → 积分与经验调整与流水 → 钱包调整 → 建套餐与功能标识 → 会员预览 / 发放 / 权益 / 收回 / 流水 →
  功能解锁 → 抽奖建活动（8 奖品）/ 修改 / 启用 / 统计 / 删除 → 封禁 / 解封 → 审计按 mcp:<前缀> 回查，全部通过后删除测试数据。
  注意：用户检索走后端全文索引，SQL 直接插入的用户检索不到，属正常。已知后端缺陷：发布渠道成员接口在渠道不存在时返回 500（外键）而不是 404。
