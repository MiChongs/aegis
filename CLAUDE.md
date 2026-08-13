# Aegis — AI 上下文索引

> 更新时间：2026-03-21 | 项目语言：Go 1.26 + TypeScript

## 项目概览

**Aegis** 是一套生产级多租户用户系统平台，提供：
- 完整的用户认证 / 授权 / OAuth2 / 多因子 / 会话管理
- 多应用（App）隔离，每个 App 独立用户库
- 积分、签到、等级、通知、工作流、存储等增值能力
- 管理后台 API + 管理前端（aegis-console）

## 架构总览

```mermaid
graph TD
  subgraph cmd["入口 (cmd/)"]
    API["cmd/api — API Only"]
    Server["cmd/server — Unified (API+Worker)"]
    Worker["cmd/worker — Worker Only"]
  end

  subgraph core["后端核心 (internal/)"]
    Bootstrap["bootstrap — 依赖组装 & 应用生命周期"]
    Config["config — 配置加载 (Viper/.env)"]
    DB["db — 连接层 (Postgres/Redis/NATS/Temporal/MySQL)"]
    Domain["domain — 领域类型 (admin/auth/user/app/...)"]
    Event["event — NATS 事件主题 & 发布者"]
    Middleware["middleware — Gin 中间件栈"]
    Repository["repository — 数据访问层"]
    Service["service — 业务逻辑服务 (20+)"]
    Transport["transport/http — Gin 路由 & 处理器 & DTO"]
  end

  subgraph infra["基础设施"]
    PG[("PostgreSQL")]
    Redis[("Redis")]
    NATS[("NATS JetStream")]
    Temporal[("Temporal")]
    MySQL[("Legacy MySQL")]
    GeoIP[("GeoIP2 mmdb")]
  end

  subgraph pkg["共享包 (pkg/)"]
    Errors["errors"]
    Logger["logger (Zap)"]
    Response["response (HTTP)"]
    Tracing["tracing (OpenTelemetry)"]
    Egress["egress (出海代理网关)"]
  end

  subgraph console["前端 (aegis-console/)"]
    NextApp["Next.js 16 + React 19 + Tailwind CSS 4"]
  end

  Server --> Bootstrap
  API --> Bootstrap
  Worker --> Bootstrap
  Bootstrap --> Config
  Bootstrap --> DB
  Bootstrap --> Service
  Bootstrap --> Middleware
  Bootstrap --> Transport
  Service --> Repository
  Service --> Event
  Repository --> PG
  Repository --> Redis
  Repository --> MySQL
  DB --> PG & Redis & NATS & Temporal & MySQL
  Service --> GeoIP
  Service --> Egress
  Egress --> Overseas[(境外 API<br/>Stripe / Google / S3 …)]
  Transport --> Service
  console --> Transport
```

## 模块索引

| 路径 | 说明 | 详细文档 |
|---|---|---|
| `cmd/` | 三个可执行入口 | — |
| `internal/bootstrap/` | 应用启动 & CLI 命令 | [CLAUDE.md](internal/bootstrap/CLAUDE.md) |
| `internal/config/` | 配置结构 & 加载 | [CLAUDE.md](internal/config/CLAUDE.md) |
| `internal/db/` | 数据库/中间件连接 | — |
| `internal/authz/` | **授权引擎**：Casbin 模型 + 权限词汇 + 内置角色 + 路由规则表 + 策略存储 | [CLAUDE.md](internal/authz/CLAUDE.md) |
| `internal/domain/` | 所有领域类型定义 | [CLAUDE.md](internal/domain/CLAUDE.md) |
| `internal/domain/organization/` | **组织架构**：租户边界、UUID 对外标识、内置角色与权限目录 | [docs](docs/organization.md) |
| `internal/domain/appfunction/` | **远程函数能力目录**：能力键 / 风险档 / TS 声明 / 内置模板，一份目录同时驱动服务端校验、SDK 绑定、控制台勾选框与编辑器提示 | [docs](docs/app-functions.md) |
| `internal/event/` | NATS 事件主题常量 | — |
| `internal/middleware/` | Gin 中间件（防火墙/认证/加密/限流） | [CLAUDE.md](internal/middleware/CLAUDE.md) |
| `internal/repository/` | Postgres / Redis / LegacyMySQL 数据访问 | [CLAUDE.md](internal/repository/CLAUDE.md) |
| `internal/service/` | 所有业务逻辑服务 | [CLAUDE.md](internal/service/CLAUDE.md) |
| `internal/transport/http/` | Gin 路由、Handler、DTO、OpenAPI | [CLAUDE.md](internal/transport/http/CLAUDE.md) |
| **平台治理** | 全站应用的冻结 / 封禁 / 限制 / 申诉（超管与平台管理员） | [docs](docs/platform-governance.md) |
| `pkg/clientip/` | **真实客户端 IP**：受信代理网段 + 平台探测 + 转发链判定 | [docs](docs/client-ip.md) |
| `pkg/egress/` | **出海代理网关**：域名后缀路由 + 多协议端点 + 健康检查 | [docs](docs/egress-gateway.md) |
| `pkg/banner/` | **启动横幅渲染引擎**：FIGlet 艺术字 + 明细表格 + 终端能力降级 | [bootstrap](internal/bootstrap/CLAUDE.md#启动横幅) |
| `pkg/routetable/` | **路由清单渲染**：分组表格 / 树形 / Markdown / CSV / HTML / JSON，宽度自适应 | [transport/http](internal/transport/http/CLAUDE.md#路由清单与分组规则) |
| `pkg/gifcaptcha/` | **动态图片验证码**：逐帧动画 GIF（字形位移/旋转/变色 + 漂移噪点 + 干扰线 + 水波扭曲），纯 Go、字体内嵌 | [service](internal/service/CLAUDE.md#动态图片验证码gifcaptcha) |
| **邮件系统** | 平台级 + 应用级两种作用域，九档服务商（SMTP / Zeabur / AWS SES / Resend / SendGrid / Mailgun / Postmark / 阿里云 / 腾讯云）一律优先官方 SDK；服务商目录自述驱动服务端校验与控制台表单 | [docs](docs/email.md) |
| **头像服务** | 地址**永久**不失效（编码的是「谁」不是「哪个对象」）+ EXIF 纠正 / 多尺寸 / blurhash + 服务端自绘默认头像 | [docs](docs/avatar.md) |
| **地图底图** | 13 家供应商自述式目录，默认档跟随**浏览器语言**（简体中文走境内、其余走全球）；GCJ-02 底图在瓦片管线里纠偏，业务坐标恒为 WGS-84 | [aegis-console](aegis-console/CLAUDE.md#地图底图多供应商--跟随浏览器语言) |
| `pkg/receipt/` | **支付凭证 PDF**：10 语言 A4 排版 + 字体决策 + 分页 | [docs](docs/payment-receipt.md) |
| **交易与凭证** | 订单与钱包流水**两类主体**都能出凭证；同一笔钱只出一份（挂着订单的流水由订单出具） | [docs](docs/payment-receipt.md#两类凭证主体) |
| **会员与试用** | 会员判定收成一个入口（是不是会员 / 还剩多久 / 是不是**试用** / 试用还能不能领）；试用是套餐的一种，一人一次由唯一约束保证 | [service](internal/service/CLAUDE.md#会员判定与试用期会员) |
| **卡密** | 一张卡两种形态：**授权卡**（卡即登录凭证，绑设备、有授权期）与**兑换卡**（发会员/积分/经验/余额/抽奖次数/设备位）。七档权益由目录驱动，一码一用由三道保证叠出来 | [docs](docs/card-key.md) |
| **远程函数** | 接入方把自定义 API 逻辑放进服务端 JS 沙箱；**试跑读真写假**、版本正文取得回、能力/闸门/配置随时可改 | [docs](docs/app-functions.md) |
| **服务端会员校验** | 接入方后端用应用密钥直接问「这个用户是不是会员」，可细到**功能标识**（`export` / `ai.chat`）；套餐改名不影响判定 | [service](internal/service/CLAUDE.md#服务端会员校验与功能标识) |
| `pkg/i18n/` | **通用国际化**：语言协商 + CLDR 复数 + 定点金额/日期格式化 | [docs](docs/payment-receipt.md#语言协商) |
| `pkg/fontkit/` | **字体归一化**：TTC 拆分成独立 sfnt + 字符覆盖度查询 | [docs](docs/payment-receipt.md#中日韩字体) |
| `pkg/` | 共享工具包（errors/logger/response/tracing） | — |
| `migrations/postgres/` | 顺序 SQL 迁移文件（000001–000080） | — |
| `sql/queries/` | **sqlc 查询源**：迁移目录即 schema，可空性由生成器算出（含 LEFT JOIN 污染的那些列），产物落 `internal/repository/postgres/sqlcgen` | [docs](docs/sqlc.md) |
| `scripts/docsgen/` | **OpenAPI 请求模型生成器**：运行时路由表 × `x/tools` 静态分析，推出每条路由的请求类型。gin 的 handler 签名擦掉了类型，没有哪个 OpenAPI 库能代劳 | [transport/http](internal/transport/http/CLAUDE.md#第-1-层为什么必须由生成器产出) |
| `sdk/kotlin/` | **官方 Kotlin/Java 客户端**：三档 transport 适配器 + 全量 API，Android 与 JVM 服务端共用 | [README](sdk/kotlin/README.md) |
| `aegis-console/` | Next.js 管理前端 | [CLAUDE.md](aegis-console/CLAUDE.md) |

## 开发命令

### 后端

```bash
# 启动（Unified 模式：API + Worker，推荐）
go run ./cmd/server

# 仅 API 服务
go run ./cmd/api

# 仅 Worker
go run ./cmd/worker

# 数据库迁移（顺序执行 migrations/postgres/*.up.sql）
go run ./cmd/server migrate

# 导出 OpenAPI 规范
go run ./cmd/server openapi docs/openapi.json

# 重新生成「路由 → 请求模型」映射表（改了 handler 的请求绑定后必须跑，产物要提交）
go generate ./internal/transport/http/
go run ./scripts/docsgen -check   # CI 跑的就是这条：产物过期即失败

# 导出 Postman 集合
go run ./cmd/server postman

# 路由清单（不连数据库，生产机器上也能安全跑一次）
go run ./cmd/server routes
go run ./cmd/server routes --format tree --group 管理端
go run ./cmd/server routes --format json --out docs/routes.json
go run ./cmd/server routes --method post,delete --path /platform

# 遗留用户迁移（需配置 LEGACY_MYSQL_DSN）
go run ./cmd/server sync-legacy-user <user_id>
go run ./cmd/server sync-legacy-batch [lastID] [limit]

# 旧 Node.js 系统 mysqldump 文件直导（无需 MySQL 实例；统一密码 + 指定应用，详见 docs/import-nodejs-dump.md）
go run ./cmd/server import-dump <dump.sql> --appid <id> --password <统一密码> [--dry-run]

# 运行测试
go test ./...
```

#### sqlc（从迁移文件生成类型安全的查询代码）

```bash
sqlc generate   # 改了 migrations/ 或 sql/queries/ 之后必须跑，产物要一起提交
sqlc vet        # 按 sqlc.yaml 的规则检查每条查询（不连库）
sqlc diff       # 产物是否与当前 schema/queries 一致；CI 跑的就是 vet + diff
```

版本钉在 `1.31.1`（与 CI 一致，浮动版本会让 `diff` 随机变红）：
`scoop install sqlc` / `brew install sqlc` / [Releases](https://github.com/sqlc-dev/sqlc/releases/tag/v1.31.1)。
定位是**渐进接管**：新查询优先写成 `.sql`，存量手写 pgx 保持原样，
两者共用同一个连接池。详见 [docs/sqlc.md](docs/sqlc.md)。

### 前端（aegis-console）

```bash
cd aegis-console
pnpm dev        # 开发服务器
pnpm build      # 生产构建 = tsc --noEmit && next build（类型检查已从 next build 内部前移）
pnpm typecheck  # 类型检查（TS 7 原生编译器）
pnpm lint       # ESLint
```

### 容器构建

```bash
# 后端（BuildKit cache mount 持有 GOMODCACHE + GOCACHE，改一个包不再全量重编 1800+ 个）
docker build -f deploy/docker/Dockerfile -t aegis-server .
docker build -f deploy/docker/Dockerfile --build-arg WITH_CJK_FONTS=0 -t aegis-server .  # 不出中日韩凭证，镜像 370MB → 197MB

# 前端（构建上下文是 aegis-console/，后端地址是构建期烘死的，必须传且必须是内网地址）
docker build -f deploy/docker/console.Dockerfile \
  --build-arg AEGIS_API_BACKEND=http://aegis-server:8088 \
  -t aegis-console aegis-console
```

前端镜像的四条硬约束（漏一条都是「容器起得来但功能静默失准」）见
[aegis-console/CLAUDE.md](aegis-console/CLAUDE.md#容器镜像deploydockerconsoledockerfile)。

## 必填环境变量

| 变量 | 说明 |
|---|---|
| `JWT_SECRET` | JWT 签名密钥 |
| `POSTGRES_DSN` | PostgreSQL 连接串 |
| `REDIS_ADDR` | Redis 地址 |
| `NATS_URL` | NATS 连接地址 |
| `ADMIN_API_TOKEN` | 管理员 API 静态令牌 |
| `ADMIN_BOOTSTRAP_*` | 超级管理员初始账号信息 |

详见 `.env.example`，默认端口 `8088`。

可选：`EGRESS_*` —— 出海代理网关。配好端点与域名后缀规则后，平台内所有出网调用
（OAuth / 支付 / 对象存储 / GeoIP / Webhook / 邮件）按同一张路由表决定走直连还是境外线路。
关闭时全部直连，详见 [docs/egress-gateway.md](docs/egress-gateway.md)。

可选：`TRUSTED_PROXIES` / `CLIENT_IP_*` —— 真实客户端 IP 的判定方式。
**默认值（`auto` + `infra`）在 Zeabur / Kubernetes / Docker / 同机反代下开箱即用**，
站在 Cloudflare 后面时填 `TRUSTED_PROXIES=infra,cloudflare`。
限流、封禁、地理风控、审计全部建立在它算出来的地址上，判错不报错只失效，
详见 [docs/client-ip.md](docs/client-ip.md)。

可选：`DOCS_PORTAL_URL`（默认 `/developers`）—— 后端 `/docs` 的 302 目标。
文档由 aegis-console 的公开门户承载，后端只保留 `/openapi.json`；前后端分域部署时填绝对地址。

## 全局规范

- **错误响应**：统一使用 `pkg/response` 的 `response.Error()` / `response.OK()`，禁止裸 `c.JSON`
- **日志**：`go.uber.org/zap` 结构化日志，生产代码禁用 `fmt.Println`
- **分层严格性**：handler → service → repository，handler 禁止直接调用 repository
- **配置注入**：所有配置通过 `internal/config.Config` 传入，禁止业务代码调用 `os.Getenv`
- **数据库事务**：复杂写操作必须使用 pgx 显式事务
- **代码注释**：使用中文（与现有代码库保持一致）
- **配置项必须有执行点**：任何可配置的开关/阈值，落库的同时必须有代码真正读它。
  只存不读的配置比没有这个配置更危险 —— 管理员会以为已经防住了。
  同一件事也不允许有两处配置入口（接入方无从判断哪个生效）。

## 平台治理（谁说了算）

除了「应用级 / 平台级」这条**配置**的作用域线，还有一条**权力**的线：
应用自治的开关，和平台强制的结论。

| | 应用自治 | 平台治理 |
|---|---|---|
| 载体 | `apps.status` / `login_status` / `register_status` | `app_governance_states` 表 |
| 谁能改 | 该应用管理员（`app:write`） | 平台级管理员（全局作用域 `platform:app:govern`） |
| 入口 | 控制台 `/apps/{appKey}` | 控制台 `/platform` |

两者是**与**的关系，且治理先判 —— 被冻结的应用把自己的 `status` 打开也没用。
之所以分表存放，正是为了让应用管理员改不动平台的结论。

六档状态 `active / restricted / frozen / suspended / banned / archived`，
七项限制（登录 / 注册 / 接口 / 支付 / 存储 / 通知 / 管理端写）**每一项都有明确执行点**，
索引见 [docs/platform-governance.md](docs/platform-governance.md#七项限制与它们的执行点)。

`/api/admin/platform/*` 全前缀恒按**全局作用域**鉴权：一旦变成应用级，
被冻结应用自己的管理员就能给自己解封。这条有测试钉住。

## 配置的两种作用域

| 作用域 | 存储 | 管理入口 | 判据 |
|---|---|---|---|
| 应用级 | `apps.settings` JSONB（`policy` / `passwordPolicy` / `captcha` / `transportEncryption` / `integralPerCurrency`） | 控制台 `/apps/{appKey}`，路径里的 appKey 即作用域 | 换个应用这项会不同 |
| 平台级 | `platform_settings` K/V（firewall / security / adminCaptcha / ldap / oidc / saml / branding / selfService）；邮件通道另在 `app_email_configs` 里以 `appid IS NULL` 表示 | 控制台 `/configuration`（超管，无应用选择器） | 对所有应用一视同仁 |

新增配置项先确定归属，**不要两边都放**。应用级策略的逐项执行点索引见
[internal/service/CLAUDE.md](internal/service/CLAUDE.md#应用级认证策略的执行点)。

## 授权：一个引擎，一份策略表

管理端授权此前有**两个 Casbin enforcer、两套模型、都只活在内存里**，
外加一段 250 行的路由 switch。现在只有 [internal/authz](internal/authz/CLAUDE.md)：
一份模型、一张 `authz_policies` 表、一个引擎，平台/应用/组织三种作用域靠**域**区分。

| 维度 | 取值 |
|---|---|
| 主体 | `role:<key>` / `admin:<id>` / `orgrole:<key>` / `orgrole:<orgID>:<key>` |
| 域 | 请求侧 `platform` / `app:N` / `org:N`；策略侧另可用 `*`、`app:*` |
| 权限点 | 支持结尾通配 `ticket:*` / `*` |
| 效果 | `allow` / `deny`，**显式拒绝跨主体压倒放行** |

因此这四件以前做不到的事现在都是一条策略：给某人单独加一个权限点（不建角色）、
收回某人某项能力（不动他的角色）、给**内置角色**补一项或砍一项、让角色继承角色
（`base_role` 终于有执行点）。策略落库并经 NATS 跨实例广播 —— 改一次角色，所有实例立刻生效。

两处刻意**不进策略表**，因为策略带缓存而它们要的是即时性：
「谁有哪个角色、绑在哪个应用上」（撤销必须立刻生效）与临时权限（价值就是到点失效）。

排障入口：`POST /api/admin/system/authz/explain` 回答「某人在某作用域下能不能做某事，为什么」，
返回判定用到的全部主体与命中的策略行。

## 零权限账号的出口（自助能力）

管理端 RBAC 有一个起点问题：自助注册出来的管理员**一条角色分配都没有**。
在这里给他发角色是不行的（注册是匿名入口，等于谁都能给自己弄一份授权），
所以出口只能是一类**不读写任何既有租户数据、产物只属于发起人**的动作 ——
实际上就是「建自己的第一个应用、成为它的 app_admin」。

因此这个动作**不走权限点判定**（要求 `app:write` 会造出一个死锁：
唯一能拿到权限的动作本身要求权限），闸门换成开关 + 每人配额，
执行点只有一个 `AdminService.EnsureCanCreateApp`，配置只有一处
`platform_settings` 的 `admin.self_service`。详见
[internal/service/CLAUDE.md](internal/service/CLAUDE.md#自助能力零权限账号唯一的出口)。

## 应用接入协议（App Protocol v1）

接入方只需要认识一个命名空间：`/api/v1/apps/{appKey}/*`，覆盖登录之后客户端
真正要用的**全部**能力：认证生命周期、资料与设置、二次认证与 Passkey、会话与审计、
签到 / 积分 / 排行榜、站内信、钱包 / 会员 / 支付、存储上传、工单、轮播图 / 公告 / 版本检查。

接口目录是**单一事实源**（`internal/service/auth_protocol_catalog.go`），随 `/config`
以机器可读形式下发（`operations` 带方法与鉴权要求，`errors` 带错误码与恢复动作）。
目录与真实路由由 `TestGatewayCatalogMatchesRegisteredRoutes` 双向钉死：
目录多一条 → 生成式客户端会调出 404；路由多一条 → 那个能力对生成式客户端不存在。

官方客户端：[sdk/kotlin](sdk/kotlin) —— Kotlin/JVM 纯实现，Android 与 Java 服务端共用一份产物。

认证方式由 `loginMethods` / `registerMethods` 控制，两者可选集合刻意不同：

| 方式 | 登录 | 注册 | 入口 |
|---|:--:|:--:|---|
| `password` | ✅ | ✅ | `/auth/login`、`/auth/register`（`method` 字段分流） |
| `sms` | ✅ | ✅ | 先 `/auth/sms/code` 取码，再走同两个入口 |
| `oauth` | ✅ | — | `/auth/oauth/url` + `callback`，或原生 `exchange` |
| `cardkey` | ✅ | — | `/auth/login`，卡即凭证；首次使用自动建号并绑卡 |

**卡密没有独立的注册开关** —— 卡是运营发出去的，发出去就意味着允许它建号。
再加一个开关会造出「卡有效但登不进去」这种没人解释得清的状态；要停发就停用批次或作废卡。
启用 `cardkey` 时请求必须携带设备标识，否则登录被拒（`40343`），
因为「一卡几机」的限制离开设备标识就不存在。

**第三方没有应用级注册开关** —— 能否自动建号由每个渠道自己的 `allowRegister` 决定。
在这里再加一个开关会变成两处配同一件事，接入方无从判断哪个生效。
启用 `sms` 时 `identifiers` 必须含 `phone`（`40086`）。

`/config` 与 `/auth/oauth/callback` 是仅有的两条**免包装**路径：前者是
"要读配置得先按配置包装"的死锁出口，后者由第三方重定向浏览器发起、
客户端根本没机会包装它（因此 sealed 档下回跳也是明文的，需要全链路加密请走 `exchange`）。

三档安全等级 **standard / signed / sealed** 共用同一批路径与同一份 JSON 结构，
只改变请求"怎么包装"——升档只替换一层 transport 适配器，业务代码不动：

| 等级 | 客户端要做的事 |
|---|---|
| `standard`（默认） | HTTPS + JSON。无密钥、无握手、无密码学库 |
| `signed` | 额外算一个 HMAC-SHA256 请求签名（v2，**覆盖 query string**） |
| `sealed` | 在 signed 之上再叠 Transport v2 端到端加密载荷 |

等级累加：`sealed = signed + 加密`。AEAD 只证明密文没被改过（服务端公钥公开，
谁都能造合法密文），签名才证明调用方持有 `appSecret`，两者缺一不可。

三条覆盖全部 HTTP 形状的规则（缺一条就有一类接口进不了这个命名空间）：

| 形状 | 怎么包装 |
|---|---|
| 有请求体（POST/PUT） | 密文走 body |
| 无请求体（GET/DELETE/HEAD） | 密文走 `?_payload=`，明文是真正的 query string |
| 上传（multipart） | 整个 multipart 体加密，原始类型由 `X-Aegis-Plain-Content-Type` 声明 |

**GET 不能带 body** —— HTTP 允许，但 OkHttp / URLSession / fetch 全都拒绝构造，
恰好就是 Android / iOS / Web 三端。因此无请求体的方法只能走 query。

签名 v2 比 v1 多一行原样 query。v1 只在请求没有 query 时才被接受（否则 `40176`）：
不签 query 意味着 `?page=1` 能被改成 `?page=999` 而签名照过。

**包装规格有四处真实来源，改协议必须同步**，否则控制台的「接入自检」会立刻红掉：

| 位置 | 角色 |
|---|---|
| `internal/service/auth_protocol_service.go` | 服务端校验 |
| `internal/service/auth_protocol_selftest.go` | 参考客户端实现（自检实跑，body 与 query 两条链路各跑一次） |
| `aegis-console/src/lib/integration-snippets.ts` | 给接入方抄的示例（门户与控制台共用） |
| `sdk/kotlin/.../AegisCanonical.kt` | 官方 SDK 里唯一允许拼协议字符串的地方 |

前两处与最后一处另有**逐字节互锚**的测试：`auth_protocol_canonical_test.go`
与 Kotlin 的 `AegisCanonicalTest` 断言同一批字面量。签名对不上时错误只会说「不对」，
不会说「哪一行不对」，所以这串字节必须有测试直接盯着。

完整规格见 [docs/app-integration.md](docs/app-integration.md)。
旧 `/api/auth/*` 明文命名空间由每个应用的 `allowLegacy` 开关控制，与安全等级正交。

## 技术栈

| 层 | 技术 |
|---|---|
| HTTP 框架 | Gin v1.12 |
| 主数据库 | PostgreSQL + PostGIS（pgx/v5 连接池；地理风控/分析依赖 PostGIS，镜像见 deploy/docker/postgres/Dockerfile） |
| 缓存/会话 | Redis v9 |
| 消息队列 | NATS JetStream |
| 工作流引擎 | Temporal |
| WAF | Coraza v3 + OWASP CRS v4 |
| 权限控制 | Casbin v2 (RBAC) |
| 密码强度 | zxcvbn（trustelem 端口，猜测次数估算）+ PRECIS RFC 8265 OpaqueString（x/text，归一化与合法性）+ 自带中文语境弱口令补充表，详见 [internal/service](internal/service/CLAUDE.md#密码强度评估--zxcvbn不是字符类规则) |
| 富文本净化 | bluemonday（公告正文写入时净化，白名单放行 tiptap 的排版标签与 class，拒绝 style 与事件属性）+ html2text（提取纯文本摘要），详见 [internal/service](internal/service/CLAUDE.md#应用级内容中心app_contentgo) |
| 多云存储 | Azure Blob / Aliyun OSS / AWS S3 / Tencent COS / Qiniu / WebDAV |
| OAuth2 | QQ / 微信 / GitHub / Google / Microsoft / 微博 |
| 支付渠道 | 16 个内置渠道，均由 `Provider.Describe()` 自描述（详见 [internal/service/CLAUDE.md](internal/service/CLAUDE.md#支付网关)）：<br>内部钱包 / 支付宝 (smartwalle) / 微信支付 (官方 wechatpay-go) / 易支付系聚合（易支付·彩虹·虎皮椒·PAYJS·码支付·V免签）/ Stripe (stripe-go) / PayPal (plutov) / Paddle / Lemon Squeezy / Square / Razorpay / Coinbase Commerce |
| 邮件出口 | 九档可插拔 provider，一律优先官方 SDK：`smtp` 直连（go-mail）/ `zeabur` REST / `ses`（aws-sdk-go-v2 sesv2，Raw MIME 带附件）/ `resend`（resend-go）/ `sendgrid`（sendgrid-go）/ `mailgun`（mailgun-go v5）/ `postmark`（REST，无官方 Go SDK）/ `aliyun`（dm-20151123）/ `tencent`（tencentcloud-sdk-go ses）。配置全部落库、控制台动态表单由服务商自述驱动，详见 [docs/email.md](docs/email.md)<br>**Zeabur 平台封禁出站 SMTP 端口**，部署在其上时不能用 `smtp` 档 |
| 出海代理 | 自研网关（`pkg/egress`）：域名后缀路由 + http/https/socks5/socks5h/ssh/trojan/shadowsocks，协议实现分别来自 x/net/proxy、x/crypto/ssh、go-shadowsocks2 |
| 启动横幅 | `pkg/banner`：go-figure（FIGlet 艺术字，内嵌 148 字体）+ go-pretty（表格/着色，自带 NO_COLOR 与 Windows VT 识别）+ gopsutil（主机事实）+ go-humanize + go-isatty/x/term（终端探测） |
| 支付凭证 | `pkg/receipt`：gopdf（PDF 引擎，支持 TTF 子集嵌入）+ `pkg/fontkit`（TTC → 独立 sfnt，gopdf 读不了 TTC/OTF）+ `pkg/i18n`（x/text 的语言协商与 CLDR 复数）+ x/image/gofont（内嵌拉丁字形，保证英文凭证零依赖）+ go-colorful（Lab 空间配色派生与 WCAG 对比度）+ boombuler/barcode（矢量二维码）。10 语言、默认 en、Claude 暖调配色，详见 [docs](docs/payment-receipt.md) |
| 客户端 IP | realclientip-go（XFF 与 RFC 7239 Forwarded 解析 + 五种取值策略 + 随库发布的 Cloudflare 网段）+ go4.org/netipx（受信网段集合运算），详见 [docs](docs/client-ip.md) |
| 验证码 | 静态图形 / 算术 / 数字走 base64Captcha；音频 WAV 走 dchest/captcha；**动态 GIF 是自研的 `pkg/gifcaptcha`**：image/gif（逐帧编码 + 全局色表）+ fogleman/gg（噪点与干扰曲线）+ x/image 的 opentype/gofont（内嵌字体）与 draw（仿射变换）+ go-colorful（HCL 取色）。外观按应用/平台分别动态配置，详见 [internal/service](internal/service/CLAUDE.md#动态图片验证码gifcaptcha) |
| 地理位置 | GeoIP2 MaxMind mmdb (自动更新) |
| 风控引擎 | expr-lang/expr（表达式规则，**类型化 Env + 编译缓存**，写错变量名在保存时即报错）+ mileusna/useragent（客户端解析：设备型号 / 桌面·移动·平板·Bot 分类 / 浏览器与系统版本）+ redis_rate GCRA 限流 + Redis HyperLogLog 基数统计（账号扩散速度），详见 [internal/service](internal/service/CLAUDE.md#riskservice--风控中心) |
| 可观测性 | OpenTelemetry + Zap |
| 前端 | Next.js 16 + React 19 + Tailwind CSS 4 + shadcn/ui |
