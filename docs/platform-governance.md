# 平台治理（超级管理员 / 平台管理员）

> 面包屑：[Aegis](../CLAUDE.md) › docs/platform-governance

超级管理员与平台管理员对**全站应用**的强制管控：限制、冻结、停运、封禁、归档，
以及配套的期限、流水、申诉与通知。

## 为什么不复用 `apps.status`

`apps` 表上已有三个开关（`status` / `login_status` / `register_status`），
它们是**应用自治**的营业开关 —— 应用管理员自己就能开关，用来表达「我这两天不开放注册」。

如果平台的封禁结论也写在那里，被封的应用把开关拨回去就复活了。所以治理状态**单独存表、单独鉴权**：

| | 应用自治开关 | 平台治理状态 |
|---|---|---|
| 存储 | `apps.status` / `login_status` / `register_status` | `app_governance_states` |
| 谁能改 | 该应用的管理员（`app:write`） | 平台级管理员（全局作用域的 `platform:app:govern`） |
| 管理入口 | 控制台 `/apps` | 控制台 `/platform` |
| 判定顺序 | 后判 | **先判** |

两者是**与**的关系：任一为关都拒绝服务。治理先判，因此「把 status 打开」绕不过冻结。

## 六档状态

| 状态 | 动作 | 停用什么 | 期限 | 谁能解 |
|---|---|---|---|---|
| `active` 正常 | `restore` | — | — | — |
| `restricted` 部分受限 | `restrict` | 操作者逐项勾选 | 可设 | govern |
| `frozen` 冻结 | `freeze` | 登录 / 注册 / 接口 / 支付 / 存储 | 可设 | govern |
| `suspended` 停运 | `suspend` | 冻结 + 通知 + **管理端写操作** | 可设 | govern |
| `banned` 封禁 | `ban` | 同停运 | **永久** | danger |
| `archived` 归档 | `archive` | 同停运 | **永久** | danger |

两条刻意的设计：

- **冻结不锁管理端**。被冻结的应用，它的管理员还要能进控制台排查配置、看审计、提交申诉。
  把管理端一起锁死是停运档才做的事。
- **封禁与归档是永久的**，不接受到期时间。「永久封禁但三天后自动解封」这种状态没有意义，
  服务端直接拒绝带期限的封禁请求。

## 七项限制与它们的执行点

**每一项都必须有真实执行点**。只落库不生效的开关比没有这个开关更危险 ——
管理员会以为已经防住了。执行点索引（同时写在 `service.PlatformGovernanceService`
的文档注释与 `/api/admin/platform/catalog` 的返回值里，控制台直接展示）：

| 限制项 | 执行点 | 语义 |
|---|---|---|
| `blockLogin` | `AppService.EnsureLoginAllowed` | 密码 / 短信 / 第三方 / Passkey 全部登录入口 |
| `blockRegister` | `AppService.EnsureRegisterAllowed` | 含第三方与短信自动建号 |
| `blockApi` | `AuthService.ValidateAccessToken` + `Refresh` | 现存会话立即失效，刷新令牌也换不出新会话 |
| `blockPayment` | `PaymentService.CreateOrder` | **只挡新订单，不挡退款** |
| `blockStorage` | `StorageService.UploadForApp` | 上传与写入类操作，读取与下载不受影响 |
| `blockNotification` | `NotificationService`（站内信三条写入路径）+ `EmailService.sendMail` | 平台唯一的邮件出口 |
| `blockAdminWrite` | `middleware.AdminAccess` | 应用作用域的一切非 GET 请求 |

几处值得说明的取舍：

- **退款不挡**。冻结一个应用不该把用户已经付进去的钱一起锁死。停运 / 封禁档通过
  `blockAdminWrite` 把退款操作收归平台管理员，而不是让退款通道整个消失。
- **`blockApi` 挂在令牌校验上而不是只挂登录**。只在登录入口拦截是不够的 ——
  已经登录的人可以拿着令牌一直用到过期。
- **申诉路径豁免 `blockAdminWrite`**。`/governance/appeals` 在中间件里被显式放行，
  否则「停运的应用连喊冤都喊不了」。

## 权限模型

| 权限点 | 能做什么 | 默认授予 |
|---|---|---|
| `platform:app:read` | 全站总览、治理详情、流水、申诉列表 | 平台管理员 |
| `platform:app:govern` | 限制 / 冻结 / 停运 / 解除 | 平台管理员 |
| `platform:app:danger` | 封禁 / 归档 / 强制下线全站会话 | **仅超管** |
| `platform:appeal:review` | 申诉审批 | 平台管理员 |

**这些权限点只在全局作用域下有意义。** `/api/admin/platform/*` 的整个前缀在
`resolveAdminPermission` 里恒返回 `appScoped=false`，原因是：`scopeMatches` 在
`requestAppID` 非空时会认可「绑定到该应用的角色」所持有的权限点 —— 一旦治理路由变成应用级，
被冻结应用自己的管理员只要拿到 `platform:app:govern` 就能给自己解封。
`internal/middleware/admin_permission_test.go` 里有用例钉住这条。

## 接口

### 平台侧（全局作用域）

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/api/admin/platform/catalog` | 任意管理员（静态目录） |
| GET | `/api/admin/platform/overview`、`/apps` | `platform:app:read` |
| GET | `/api/admin/platform/apps/:appkey/governance` | `platform:app:read` |
| POST | `/api/admin/platform/apps/:appkey/governance` | `platform:app:govern` |
| POST | `/api/admin/platform/apps/batch-governance` | `platform:app:govern` |
| POST | `/api/admin/platform/apps/:appkey/revoke-sessions` | `platform:app:danger` |
| GET | `/api/admin/platform/governance/actions` | `platform:app:read` |
| GET | `/api/admin/platform/governance/appeals` | `platform:app:read` |
| POST | `/api/admin/platform/governance/appeals/:appealId/review` | `platform:appeal:review` |

### 应用侧（应用作用域，只读 + 申诉）

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/api/admin/apps/:appkey/governance` | `app:read` |
| GET | `/api/admin/apps/:appkey/governance/history` | `app:read` |
| POST | `/api/admin/apps/:appkey/governance/appeals` | `app:write` |
| POST | `/api/admin/apps/:appkey/governance/appeals/:appealId/withdraw` | `app:write` |

治理动作请求体：

```json
{
  "action": "freeze",
  "reason": "批量注册异常，风控命中 3 万次",
  "durationSeconds": 604800,
  "restrictions": { "blockRegister": true },
  "revokeSessions": true,
  "notifyAdmins": true
}
```

`restrictions` 只在 `restrict` / `update` 生效，其余档位用服务端预设 ——
不让前端传一组限制、后端按另一组执行。`endAt` 与 `durationSeconds` 二选一，都不传即无限期。

## 错误码

| 码 | 含义 |
|---|---|
| 40330–40336 | 对应七项能力被限制（登录 / 注册 / 接口 / 支付 / 存储 / 通知 / 管理端写） |
| 40337 / 40338 | 需要治理权限 / 需要危险操作权限 |
| 40339 | 状态机非法（如对未治理的应用执行「调整治理」） |
| 40031–40038 | 入参问题（动作 / 理由 / 期限 / 限制项 / 申诉内容 / 裁决） |
| 40493 / 40494 | 应用不存在 / 申诉不存在 |
| 40904 | 该应用已有待审申诉 |

面向被拒方的文案只说「被平台怎么了、哪一项不可用、到什么时候」，
**不带操作者与证据** —— 那些是内部信息，只在控制台的治理详情页可见。

## 判定为什么走内存

登录与每次带令牌的请求都要判一次，打库不现实。因此：

- 状态表在内存里留一份快照（只装非 `active` 的行，绝大多数应用不占内存）
- 本实例的写操作立即刷新
- 跨实例靠后台 tick 收敛，默认 **15 秒**
- 需要强一致的场景（管理端读详情）走 `Get`，直接读库
- 快照里残留的过期记录按时间直接放行，用户不必多等一个 tick

治理表读不出来时快照为空 = 全部放行（fail-open），与防火墙同取向：
读不出规则就把整个平台锁死，代价远大于漏放一会儿。

## 到期与副作用

- 到期结算由 **API 实例唯一负责**（`RunExpiry`，`FOR UPDATE SKIP LOCKED`）。
  Worker 只 `StartReadOnly` 收敛快照，否则流水里会出现两条「到期自动恢复」。
- 状态与流水**同事务**落库：只写状态会失去追责依据，只写流水会让判定读到旧结论。
- 副作用（踢会话、发通知）在事务提交**之后**，且失败不回滚 ——
  治理结论已经生效，回滚反而会造成「库里没封、判定已封」的错位。
  会话数事后回写进流水（Redis 与 Postgres 不共享事务）。

## 申诉

被治理应用的管理员在 `/apps` 页顶部的横幅里提交申诉，平台管理员在 `/platform?tab=appeals` 裁决。

- 同一应用同时只允许一份待审申诉（DB 唯一部分索引），避免刷单式申诉淹没审核队列
- 通过申诉时**先解除治理、再标记通过**：反过来一旦解除失败，
  被治理方看到的是「通过了却还是用不了」
- 裁决结果经管理员收件箱通知提交人

## 数据表

| 表 | 作用 |
|---|---|
| `app_governance_states` | 每应用一行的当前状态（判定读它） |
| `app_governance_actions` | 只增不改的动作流水（追责读它） |
| `app_governance_appeals` | 申诉与裁决 |

迁移：`migrations/postgres/000067_platform_governance.{up,down}.sql`

## 控制台

`/platform`（侧边栏「平台治理 → 治理台」，需 `platform:app:read`）：

- **应用** —— 全站应用列表：治理状态、用量指标、单个/批量治理、详情抽屉（流水 + 待审申诉 + 强制下线）
- **申诉** —— 待审队列，通过即解除治理
- **治理流水** —— 全站动作时间线

被治理应用的管理员在 `/apps` 顶部看到红色横幅：被怎么了、为什么、到什么时候、以及申诉入口。
没有这块横幅，他看到的只是一连串不明所以的 403。
