# internal/service — 业务逻辑层

> 面包屑：[Aegis](../../CLAUDE.md) › internal/service

## 职责

所有业务逻辑的实现层，是 Handler 和 Repository 之间的唯一桥梁。

## 服务清单

| 文件 | 服务 | 核心功能 |
|---|---|---|
| `admin_service.go` | `AdminService` | 管理员认证、Casbin RBAC、超级管理员引导 |
| `app_oauth_service.go` | `AppOAuthService` | 应用级第三方登录渠道配置（CRUD/密钥加密/自检/解析） |
| `app_oauth_catalog.go` | — | 内置渠道模板目录（微信/QQ/微博/Gitee/GitHub/Google/… 13 个） |
| `app_service.go` | `AppService` | 多租户 App 管理、App 加密密钥 |
| `app_function_service.go` | `AppFunctionService` | **远程函数**：函数与版本、试跑、调用与幂等、并发与频次闸门、统计（见下节） |
| `app_function_sdk.go` | — | 注入脚本的 `aegis` 全局对象；能力逐项绑定，试跑时写只记录不执行 |
| `app_function_script.go` | — | goja 沙箱：编译校验、超时中断、`handle(ctx)` 调用与返回值编码 |
| `app_function_sandbox.go` | — | WASM 沙箱 + HTTP 执行器（Ed25519 双向签名、SSRF 防护、`aegis.fetch` 出口） |
| `app_content.go` | — | 应用级内容中心：Banner 投放位与公告（挂在 `AppService` 上，见下节） |
| `auth_service.go` | `AuthService` | 用户 JWT 认证、Token 刷新、OAuth2 |
| `auth_sms.go` | — | 短信验证码登录/自动建号 + `MobileOAuthLoginScoped`（叠加应用级开关） |
| `auth_protocol_service.go` | `AuthProtocolService` | 接入协议：策略、安全等级、应用密钥签名、Transport v2 |
| `auth_protocol_selftest.go` | — | 接入自检；同时是三档协议的**参考客户端实现** |
| `auto_sign_service.go` | `AutoSignService` | 自动签到调度（Redis Sorted Set） |
| `avatar_service.go` | `AvatarService` | 头像：解析成**永久地址**、上传、移除、历史、取图（见下节） |
| `avatar_link.go` | — | 主体令牌签名 + 永久地址构造 + **写回闸门**（`NormalizeAvatarInput`） |
| `avatar_pipeline.go` | — | 解码校验 / EXIF 纠正 / 方形裁剪 / 多尺寸 / 重编码 / blurhash / 主色 |
| `avatar_identity.go` | — | 默认头像的确定性生成（identicon / 拼音首字母） |
| `captcha_service.go` | `CaptchaService` | 六档验证码的生成与校验入口 + 短信验证码与防轰炸链 |
| `captcha_resolver.go` | — | 服务端决定下发哪一档（客户端不参与选择）+ 场景要求的折叠 |
| `captcha_chiral.go` | — | 手性碳点选：分子生成 / 手性检测 / 2D 渲染 / 坐标校验（RDKit 微服务的降级实现） |
| `captcha_sms.go` / `captcha_sms_config.go` | — | 短信服务商适配与按用途分模板的配置解析 |
| `db_manager.go` | `DatabaseManager` | 数据库生命周期与泄漏监控总入口（采集/告警/历史/会话治理） |
| `db_leak.go` | — | 六类泄漏判定（连接/事务/快照/WAL/两阶段事务/存储）+ 指标趋势检测 |
| `db_sessions.go` | — | pg_stat_activity / 复制槽 / 两阶段事务 / 死元组视图与会话终止 |
| `email_service.go` | `EmailService` | 邮件出口总入口：挑 provider、组织内容、投递留痕、密钥加解密 |
| `email_template.go` | — | 邮件内容模型与渲染（html/template + premailer 内联），全部文案在此 |
| `emailtpl/` | — | 模板资源：`layout.gohtml` / `layout.gotxt` / `theme.css`（go:embed） |
| `email_sender.go` | — | `emailSender` provider 抽象与分派 |
| `email_provider_smtp.go` | — | SMTP 直连发送器（go-mail）与错误分类 |
| `email_provider_zeabur.go` | — | Zeabur Email REST 发送器（错误映射 / 429 不重试 / 独立熔断） |
| `email_webhook.go` | — | Zeabur 投递回执：HMAC 验签、防重放、状态推进 |
| `egress_service.go` | `EgressService` | 出海代理网关管理面：配置持久化 / 密钥加解密 / 数据库覆盖 .env / 自测与路由解释 |
| `geo_analytics_service.go` | `GeoAnalyticsService` | 地理分析：小时聚合 / DBSCAN 攻击聚类 / 用户轨迹 / 分区与画像维护 |
| `geo_ban_service.go` | `GeoBanService` | 地域/ASN/ISP 封禁（DB 存储 + 内存匹配 + 热重载） |
| `geo_fence_service.go` | `GeoFenceService` | 地理围栏（PostGIS 真实来源 + 内存几何判定 + 回测） |
| `geo_math.go` | — | 纯内存几何计算（haversine / GeoJSON 解析 / 射线法） |
| `geo_risk_service.go` | `GeoRiskService` | 登录地理风控（不可能旅行/新国家/远离常驻地，Redis 画像缓存） |
| `risk_service.go` | `RiskService` | **风控中心**：规则评估 / 处置策略 / 设备与 IP 档案 / 大盘 / 模拟与重放（详见下节） |
| `risk_env.go` | — | `RiskEvalEnv` —— 引擎看到的全部事实，同时是 expr 的类型化环境（`expr` 标签即变量名） |
| `risk_expr.go` | — | expr 编译缓存、领域函数（`in_cidr`/`any_cidr`/`contains_any`/`in_time_window`）、表达式校验 |
| `risk_provider.go` / `risk_provider_ipqs.go` | — | 外部 IP 情报源抽象与 IPQualityScore 适配（带熔断与限速） |
| `location_geoip.go` | — | GeoIP2 mmdb 加载 & 自动更新实现 |
| `location_service.go` | `LocationService` | IP 地理位置查询（city + ASN），Redis 缓存 |
| `migration_service.go` | `MigrationService` | 遗留 MySQL 用户迁移 |
| `monitor_service.go` | `MonitorService` | 系统监控数据聚合（所有服务汇总） |
| `notification_service.go` | `NotificationService` | **应用用户**站内信（notifications 表，外键 users） |
| `admin_inbox_service.go` | `AdminInboxService` | **管理员**收件箱（admin_notifications 表，外键 admin_accounts）+ 角标实时推送 |
| `realtime_publisher.go` | — | 仅发布的 NATS 实时事件发布器，供 Worker 把事件送达连在 API 实例上的客户端 |
| `notify_hub.go` | `NotifyHub` | **统一通知出口**：订阅匹配 → 模板渲染 → 多渠道投递 → 留痕重试 |
| `notify_provider_feishu.go` | — | 飞书群机器人（加签）+ 企业自建应用（tenant_access_token）、交互式卡片 |
| `notify_provider_im.go` | — | 钉钉 / 企业微信 / Slack 群机器人 |
| `notify_provider_local.go` | — | 通用 Webhook（HMAC 签名）+ 邮件 / 站内信 / 实时推送 |
| `notify_admin.go` | — | 渠道 / 订阅 / 模板 / 投递记录的管理面 + 测试发送 |
| `ticket_service.go` | `TicketService` | 工单建单、回复、指派、状态流转、附件、评价 |
| `ticket_access.go` | — | 工单权限：`Scope` 可见范围推导 + `ActionSet` 动作集判定 |
| `ticket_sla.go` | — | SLA 计时（支持工作时间窗口）与巡检告警 |
| `ticket_notify.go` | — | 工单 → NotifyHub 的唯一桥梁（事件构造与收件人解析） |
| `ticket_config_service.go` | — | 分类 / 处理组 / SLA 策略 / 快捷回复配置 |
| `oauth_provider.go` | — | OAuth2 协议适配器（按 kind 分流：generic/qq/wechat/weibo/github/microsoft） |
| `login_consistency_service.go` | `LoginConsistencyService` | 登录一致性：设备绑定 / 换绑冷却 / 登录 IP / 登录属地（Redis 基线 + 管理端重置） |
| `organization_service.go` | `OrganizationService` | 组织 / 部门 / 应用绑定 / 概览；权限判定下沉在此层（`orgContext`） |
| `org_member_service.go` | — | 组织成员 / 邀请 / 岗位 / 组织角色（挂在 `OrganizationService` 上，共用同一套权限解析） |
| `org_approval_service.go` | `OrgApprovalService` | 审批链与实例、权限模板、跨部门协作组 |
| `org_access_control.go` | `OrgAccessControl` | Casbin 组织域（带 domain）判定，下发展开后的权限集给前端 |
| `org_import_export.go` | — | 组织架构 Excel 导入导出（excelize），导入两段式：先预检再落库 |
| `password_policy_service.go` | — | 密码策略规则、模板、校验、生命周期（过期时间 / 防重用） |
| `password_strength.go` | — | 强度评估引擎：PRECIS 归一化 + zxcvbn 猜测次数估算 + 0~100 映射 + 模式中文化 |
| `password_dictionary_zh.go` | — | 中文语境与通用弱口令补充词表（zxcvbn 内置词表零覆盖中文） |
| `payment_service.go` | `PaymentService` | 支付网关：渠道注册表、订单创建/查询/回调、统一限额、履约 |
| `payment_receipt.go` | — | 支付凭证：订单 → 凭证文档装配、语言/时区/币种解析、导出落盘与清理、订单上的凭证入口 |
| `payment_receipt_wallet.go` | — | 钱包流水凭证：流水 → 凭证文档装配、订单委派判定、流水上的凭证入口 |
| `payment_receipt_email.go` | — | 凭证邮件：签名下载链接、与 PDF 同语言的正文、频次限制、支付成功自动寄送 |
| `payment_provider.go` | — | `paymentProvider` 接口 + 回调保留键与签名头白名单 |
| `payment_provider_schema.go` | — | 渠道自描述构造器（字段 schema / 分区 / 能力矩阵） |
| `payment_provider_*.go` | — | 16 个渠道适配器，各自实现 `Describe()` 自描述 |
| `wallet_service.go` | `WalletService` | 余额：查询 / 消费 / 流水（带凭证入口）/ 全应用流水与资金面板 / 管理员调账 |
| `vip_service.go` | `VipService` | 会员：套餐、**余额直购**（成功后按应用设置自动寄凭证）、管理端授予 |
| `vip_trial_service.go` | — | **会员判定的唯一入口**（`ResolveEntitlement`）+ 试用期会员：资格、领取、管理端重置（见下节） |
| `vip_verify_service.go` | — | **服务端会员校验**（接入方后端调用）+ 会员功能标识目录（见下节） |
| `platform_governance_service.go` | `PlatformGovernanceService` | **平台治理**：全站应用的限制/冻结/停运/封禁/归档 + 到期结算 + 申诉（内存快照判定） |
| `platform_settings_service.go` | `PlatformSettingsService` | 平台设置读写、防火墙动态配置 |
| `points_service.go` | `PointsService` | 积分 & 经验值调整、统计 |
| `realtime_service.go` | `RealtimeService` | WebSocket 连接管理、NATS 实时推送 |
| `role_application_service.go` | `RoleApplicationService` | 角色申请审批流程 |
| `signin_service.go` | `SignInService` | 用户签到逻辑、积分结算 |
| `site_service.go` | `SiteService` | 站点信息管理 |
| `storage_provider_*.go` | — | 多云存储驱动（Azure/OSS/S3/COS/Qiniu/WebDAV）|
| `storage_service.go` | `StorageService` | 存储桶管理、文件上传/下载/配额 |
| `user_service.go` | `UserService` | 用户 CRUD、状态管理 |
| `user_settings_admin_service.go` | — | 管理员侧用户设置管理 |
| `version_service.go` | `VersionService` | App 版本发布管理 |
| `worker_event_service.go` | `WorkerEventService` | NATS 事件消费处理（登录审计等） |
| `workflow_runtime_temporal.go` | — | Temporal 工作流活动注册 |
| `workflow_service.go` | `WorkflowService` | 工作流定义 CRUD、Temporal 触发 |

## 依赖关系约束

```
Handler → Service → Repository + Domain Types
Service 可以调用其他 Service（如 AvatarService 调用 StorageService + UserService）
Service 不可 import transport/http
```

## 构造函数模式

所有服务均遵循：
```go
func NewXxxService(log *zap.Logger, pg *pgrepo.Queries, ...) *XxxService
```
参数通过 `internal/bootstrap/app.go` 集中注入，服务间不自行初始化依赖。

## 关键服务说明

### AdminService
- **判定全部委托给 [internal/authz](../authz/CLAUDE.md)** —— 本服务不再持有 enforcer。
  它只负责组装主体（本人 + 该作用域下生效的角色）并把结论翻成业务错误
- `EnsureBootstrapSuperAdmin`：首次启动自动创建超级管理员
- 会话存储在 Redis，带 TTL（默认 12h）
- `admin_authorization.go` 权限判定与自助能力；`admin_policy_service.go` 策略管理面

### 授权引擎接管了什么

| 以前 | 现在 |
|---|---|
| `AdminService` 持有一个内存 Casbin enforcer，模型是 `r.obj == p.obj` | 判定走 `authz.Engine`，模型支持域 / 通配 / 拒绝 / 继承 |
| `OrgAccessControl` 持有**第二个** enforcer 与**第二套**模型 | 同一个引擎，靠域 `org:N` 与主体前缀 `orgrole:` 区分 |
| 内置角色写死在 `builtInAdminRoles()`，进程重启即回到编译期状态 | 定义仍在代码（保证升级能 propagate），但落库并可被 `override` 增减 |
| 角色改动只有当前实例知道 | 策略落库 + NATS 广播，所有实例即时重载 |
| `base_role` 只被拿去画关系图 | 落成一条角色继承边，父角色加权限子角色跟着有 |
| 展开权限集与真实判定是两段代码 | 展开就是逐条跑判定，两者不可能漂移 |

**`scopeMatches` 仍留在 Go 里**（过滤"这个角色在本次作用域下算不算数"）：
角色绑定关系每次请求随会话现查，灌进带缓存的策略表会引入
"撤销了角色但还能用一段时间"的窗口 —— 这是授权系统里最不该有的那种延迟。

### 自助能力 —— 零权限账号唯一的出口

自助注册（`RegisterAdmin`）建出来的账号**一条 assignment 都没有**，这是刻意的：
注册是匿名入口，在那里发角色等于谁都能给自己弄一份平台级授权。
但这样一来就有了一个死锁 —— 唯一能让他拿到权限的动作
（建自己的第一个应用、成为它的 `app_admin`）以前要求 `app:write`，
而那是只有平台管理员才有的权限点：

```
注册 → 零角色 → 想建应用 → 需要 app:write → 只有已经有权限的人才有
        ↑                                                  │
        └──────────────────────────────────────────────────┘
```

控制台上的表现就是：填完「新建应用」表单、点提交、拿回一句
「当前管理员无权执行此操作」，而且没有任何地方说得出该怎么办。

解法不是给新账号发角色，而是把这个动作从**权限点判定**里拿出来归入**自助能力**：
它不读写任何既有租户的数据，产物只属于发起人。

| 环节 | 落点 |
|---|---|
| 中间件 | `POST /api/admin/apps` 返回 `("", false)` —— 不要权限点、不按应用作用域 |
| 闸门（唯一执行点） | `AdminService.EnsureCanCreateApp`：平台开关 + 每人配额 |
| 配置（唯一入口） | `platform_settings` 的 `admin.self_service`（`SelfServiceSettingsView`） |
| 配额基数 | `apps.created_by`（迁移 000074），**不是** `admin_assignments` 里的 `app_admin` 条数 |
| 创建 + 授权 | `Repository.CreateAppOwnedBy`，**同一事务** |

几处刻意的取舍：

- **配额只数「自己建的」。** 用 `admin_assignments` 的 `app_admin` 条数来数，
  会让「被超管授权去管理 5 个既有应用」的人凭空吃光配额 —— 他一个都没建过。
- **创建与授权不可分割。** 分两步做时中间失败会留下一个孤儿应用：建它的人管不了它，
  列表里也看不见它（可见范围按授权过滤）。而这条链路的调用者恰恰是那个
  **还没有任何权限**的新账号，他连补救都做不到。
- **持有全局 `app:write` 的人不受配额约束**：那是常规授权路径，配额管的是没有授权的人。
- **配额统计失败时不放行**（`fail-closed`，与登录基线的 fail-open 取向相反）：
  这条链路免授权，判定失效的代价是任何注册账号都能刷应用。
- **创建者可以删自己建的应用**（`AdminDeleteApp` 认 `apps.created_by`，不认 app_admin 授权）。
  不给这个出口，配额就是一条单向棘轮；而认授权会让删除这种不可逆动作跟着授权流转。
- **注册开关也在这份配置里**（`allowRegistration`）。以前关掉注册的做法是把路由那行摘掉，
  于是前端拿到 40400「请求的页面不存在」，看起来像地址写错了；现在返回 40317，
  并且登录页可以先读 `GET /api/admin/auth/self-service` 决定注册入口显不显示。
- 默认值 = 引入这份配置之前的行为（注册开、自助建应用开），
  否则升级本身就是一次静默的行为变更。

### 权限拒绝要说得出缺什么

`Authorize` 拒绝时不再返回一句光秃秃的「当前管理员无权执行此操作」。
那句话同时隐去了三件事：缺的是哪个权限点、判定在哪个作用域、以及该怎么拿到 ——
而最需要这三条信息的人，恰恰是刚注册完什么都没有的那个账号。
现在的文案带权限点代码、目录里的中文名、作用域，以及按情形分支的补救建议
（零角色 / 应用级缺授权 / 平台级权限点应用级角色满足不了）。错误码仍是 `40311`，
前端已有的分支不受影响。

同源的两处：中间件里「路由没登记在权限表里」改用独立的 `40315` 并明说是漏登记 ——
与「没权限」共用文案时，一条漏登记的新路由会伪装成权限配置问题，
把排查方向从"补一行权限表"带偏到"给这个人加授权"。

`/api/admin/auth/me` 现在下发 `permissions`（展开后的生效权限：全局集合 +
按应用的完整集合）与 `selfService`（能不能建应用、配额、不能的话为什么）。
只下发角色 key 的话，控制台得自己维护一份"角色 → 权限"的副本，
那份副本一旦与 `builtInAdminRoles` 漂移，用户看到的就是"按钮在、点了 403"。
`apps[x]` 是**该应用下的完整集合**（已并入全局权限）而不是增量：
让调用方自己复现 `scopeMatches` 那条"全局覆盖应用级"的规则，等于把同一条判定写两遍。

### AuthService
- JWT HS256，声明包含用户 ID、App ID、Token ID
- 支持 Token 刷新（RefreshTTL 内）
- OAuth2 回调通过 `oauth_provider.go` 适配各平台差异
- OAuth 渠道由 `AppOAuthService.Resolve` 解析：**应用级配置 → 平台级 .env 兜底**
- **`finalizeLogin` 是全部登录方式的唯一收口**：密码 / OAuth / 短信走 `completeLogin` 进来，
  Passkey（`VerifyPasskeyLogin`）与 MFA 二次验证（`VerifySecondFactor`）**绕过 `completeLogin`**
  直接进本函数。任何「每次登录都要做一次」的检查都必须挂在 `finalizeLogin`，
  挂在 `completeLogin` 会漏掉后两条链路。登录一致性校验即挂在此处。

### Passkey 的 RP ID 跟着访问域名走（`security_passkey_rp.go`）

WebAuthn 规定 `rp.id` 只能**等于当前页面的有效域名、或它的可注册后缀**。
不满足时浏览器在 `navigator.credentials.create()` 就抛
「The relying party ID is not a registrable domain suffix of, nor equal to the current domain」——
这句只出现在浏览器控制台，服务端日志里一片安静。

因此 RP ID **不是一个静态配置值**，而是每次请求按来源解析：

| 输入 | 来源 |
|---|---|
| 请求来源 | `middleware.RequestOrigin` 写进 context，取值优先级 `Origin` > `Referer` > 转发头 |
| 允许来源白名单 | `SECURITY_PASSKEY_RP_ORIGINS` / 平台配置，**唯一的安全边界**，不匹配即 `40039` 并说清怎么改 |
| RP ID | 配置值是来源域的可注册后缀时沿用（跨子域共享凭据），否则用来源域本身 |

`SECURITY_PASSKEY_RP_ID` **留空是推荐值**，含义是「跟随访问域名」。
以前它会被兜底成 `localhost`，而缺省白名单里同时有 `http://127.0.0.1:3000` ——
两者自相矛盾，从 127.0.0.1 打开控制台必然报上面那句。

Finish 阶段**不重新推导**，而是用 Begin 下发的那个 RP ID
（go-webauthn 已把它写进 `SessionData.RelyingPartyID` 并随挑战落库）。
注意 `CreateCredential` / `ValidateLogin` 读的是 `Config.RPID` 而**不是** session 里那个字段，
所以必须按它重新造一个实例，`WithRegistrationRelyingPartyID` 单用会在 Finish 对不上。

### 应用级认证策略的执行点

`appdomain.Policy` 的每一项都必须有明确执行点。**只落库不生效的开关比没有这个开关更危险** ——
管理员会以为已经防住了。重构前有 5 项属于这种状态（`loginCheckUser`、`loginCheckIp`、
`loginCheckDeviceTimeOut` 全代码库无读取点，`registerCaptcha` / `registerCaptchaTimeOut`
被验证码配置的 `requireForRegister` 取代），现已全部落地或删除。

| 策略字段 | 执行点 | 语义 |
|---|---|---|
| `loginCheckDevice` | `validateLoginPolicy` + `enforceDevicePolicy` + `LoginConsistencyService` | 必须显式携带设备标识，且与已绑定设备一致 |
| `loginCheckDeviceTimeOut` | `LoginConsistencyService` | **设备换绑冷却秒数**（旧字段原义即「登录换绑机器码间隔」），从上次换绑起算 |
| `loginCheckIp` | `LoginConsistencyService` | 与上次成功登录不在同一网段（IPv4 /24、IPv6 /48）即拦截 |
| `loginCheckUser` | `LoginConsistencyService` | 登录属地（国家 + 省/州）与上次不一致即拦截 |
| `multiDeviceLogin` / `multiDeviceLimit` | `enforceSessionPolicy` | 同时在线设备上限，超出踢最旧会话 |
| `registerCheckIp` | `validateRegisterPolicy` | 同一 IP 不允许重复注册 |

三项强绑定策略（device / ip / user）**全关时不产生任何 Redis I/O**，绝大多数应用走这条路径。
开启后唯一的解绑出口是 `ResetLoginBaseline`（控制台「登录绑定 → 重置」）——
没有这个出口，用户换宽带就等于账号报废。基线读写失败一律 fail-open，与 `LoginGuardService` 同取向。

### RiskService —— 风控中心

打分制引擎：**规则各自加分 → 总分映射等级 → 处置策略按分数区间决定动作**。
挂在登录 / 注册主链路上（`AuthService` / `AdminService` 的 `EvaluateRisk`），
只有 `block` / `ban` 会拦下请求。

```
请求 ──► buildEvalEnv（UA 解析 / Redis 计数与基数 / 设备档案 / IP 情报 / 归属地）
     ──► evaluateRules（逐条判定，返回**全部**规则的轨迹与人类可读判据）
     ──► ScoreToLevel + resolveAction
     ──► persistAsync（评估留痕 + 设备档案 + IP 计数 + 规则命中计数）
```

结构性约束：

- **判定同步、留痕异步**。调用方只等「命中了什么、该怎么处置」；
  评估记录等四件事在 `context.WithoutCancel` 的后台 goroutine 里落库。
  留痕失败绝不反噬业务 —— 让一次写库抖动把用户的登录挡在门外是本末倒置。
- **表达式在保存时编译校验**。`RiskEvalEnv` 用具名 struct 做 `expr.Env`，
  写错的变量名当场是编译错误。用 `map[string]any` 做环境时它只会在运行期
  静默判假 —— 那条规则从此永不命中，而列表上一直显示「已启用」。
  编译产物按表达式文本缓存，热路径上不再重复编译。
- **每条判定都返回判据**（`MatchedRule.Reason`）。「命中 IP 高频」与
  「命中 IP 高频：312 次 > 阈值 100」是两种可运维性，复核台上只有后者能用。
- **情报缺失一律判「不命中」**。归属地查不到就把请求算成异常，那不是风控是拒绝服务。
- **人工结论优先于情报源**：`ip_risk_records.source = manual` 的行不会被情报刷新覆盖，
  且 `trusted` 标签在代理 / 信誉分两类条件里直接短路。
- **条件类型目录是单一事实源**（`internal/domain/security/risk_catalog.go`）。
  它同时驱动后端校验与控制台的参数表单，新增一种条件类型前端零改动。
  `TestRiskConditionCatalogHasEvaluator` 与 `TestRiskEnvMatchesVariableCatalog`
  双向钉死「目录 ↔ 判定分支」「目录 ↔ 环境字段」。

重构前修掉的四个「看起来在防、其实没防」：

| 问题 | 后果 |
|---|---|
| `device_fingerprints` 只读不写 | `device_age_hours` 恒为 0，「新设备」规则对**每个**请求都命中 |
| `UpsertIPRisk` 无条件整体覆盖 | 复核拒绝时把国家 / 运营商 / 计数全清零 —— 复核动作本身在销毁证据 |
| `total_requests` / `total_blocks` 从不累加 | 「高风险 IP」列表那两列恒为 0 |
| 表达式每次评估重新编译 | 登录热路径上每个请求做一遍词法/语法/类型检查 |

### PlatformGovernanceService —— 平台强制管控

与 `apps.status` 那三个开关的分工是「谁说了算」：那是应用自治的营业开关，
这是平台强制的结论，**分表存放正是为了让应用管理员改不动它**。两者是与的关系，治理先判。

七项限制的执行点索引（写在服务的文档注释里，同时经 `/catalog` 下发给控制台展示 ——
「这个开关到底管不管用」应该能被直接核对，而不是翻代码）：

| 限制项 | 执行点 |
|---|---|
| `blockLogin` / `blockRegister` | `AppService.EnsureLoginAllowed` / `EnsureRegisterAllowed` |
| `blockApi` | `AuthService.ValidateAccessToken` + `Refresh`（已签发的会话当场失效） |
| `blockPayment` | `PaymentService.CreateOrder`（**只挡新订单，退款不挡**） |
| `blockStorage` | `StorageService.UploadForApp`（所有上传入口的收口处） |
| `blockNotification` | `NotificationService` 三条写入路径 + `EmailService.sendMail` |
| `blockAdminWrite` | `middleware.AdminAccess`（应用作用域的非 GET 请求） |

其余结构性约束：

- **判定走内存快照**（只装非 active 的行），本实例写后立即刷新，跨实例 15s 收敛；
  读库失败时快照为空 = 全部放行（fail-open），与防火墙同取向。
- **状态与流水同事务**：只写状态会失去追责依据，只写流水会让判定读到旧结论。
- **副作用在事务之后且失败不回滚**：结论已生效，回滚会造成「库里没封、判定已封」。
  会话数事后回写（Redis 与 Postgres 不共享事务）。
- **到期结算只有 API 侧跑**，Worker 用 `StartReadOnly` 只收敛快照，否则流水里会出现两条到期恢复。
- **冻结档刻意不锁管理端**：被冻结的应用，管理员还要能排查配置与提交申诉。
- **申诉路径豁免只读闸门**（在中间件里放行），否则停运的应用连喊冤都喊不了。

完整说明见 [docs/platform-governance.md](../../docs/platform-governance.md)。

### 密码强度评估 —— zxcvbn，不是字符类规则

强度判定的唯一入口是 `AnalyzePasswordStrengthWithContext`。它**不再**数大小写数字符号，
而是用 zxcvbn（Dropbox 提出的口令强度估算算法）把口令拆成字典词 / 键盘串 / 重复 /
递增序列 / 日期 / 年份 / l33t 替换 / 倒序 的最优组合，输出**攻击者需要的猜测次数**。

替换掉的旧实现有两个结构性缺陷，不是补几个词能修的：

- **香农熵度量的是字符分布，不是可猜测性。** `abcabcabc` 的每字符熵比 `Xy9$Kw` 还高，
  但前者一秒即破。用它当依据等于奖励"字符种类多"而非"难猜"。
- **弱口令靠 substring 匹配。** 名单里有 `password` 就只挡得住 `password`，
  挡不住 `Pa55word` / `drowssap` / `p@ssw0rd123` —— 而攻击者的字典早就覆盖了这些。

实测对照（默认策略门槛 40 分）：

| 口令 | 旧实现 | 现在 |
|---|---:|---:|
| `password123` | 45（**通过**） | 19 |
| `P@ssw0rd` | 高分 | 8 |
| `woaini1314` | 高分 | 10 |
| `Xy9$Kwe2` | 70 | 55 |
| `7xKq2mVzP4wR` | 80 | 80 |

其余结构性约束：

- **0~100 的刻度不能换。** 它已经落在每个应用的 `passwordPolicy.minScore`、
  `user_password_security_states.password_strength_score` 列和控制台滑块上。
  映射锚点（`passwordScoreAnchors`）压在 zxcvbn 自己的档位边界上，
  改动它会**静默改变所有应用的实际密码要求**，有测试钉住。
- **不能照搬"命中模式即违规"那条旧规则。** zxcvbn 会把任意口令都拆成模式序列，
  强口令里同样有字典片段，照搬会导致几乎没有口令能通过。模式的代价已经计入猜测次数、
  也就是已经反映在分数里，再扣一次是重复计价。只额外拦一种分数说明不了的情况 ——
  **单个模式覆盖整条口令**（`fatalPasswordPattern`），这样 `minScore=0`
  的应用也挡得住「密码 = 123456」「密码 = 账号」。
- **用户上下文必须喂进去**（`PasswordContext`：账号 / 昵称 / 邮箱 / 手机 / 应用名）。
  zxcvbn 把它们当临时字典，于是「账号 zhangsan、密码 Zhangsan2024」会被识别成字典命中。
  注册与改密两条链路都已接上；**改密刻意把取用户提前到校验之前**就是为了这个。
- **中文语境要自己补词表**（`password_dictionary_zh.go`）。zxcvbn 自带的六张表
  （泄露口令榜 / 英文维基 / 英美人名姓氏 / 影视台词）没有一张覆盖中文，
  `woaini1314`、`zhangwei`、`5201314` 在纯 zxcvbn 下会被当成随机串给高分。
  词表**顺序即权重**（rank 按下标定），是数据不是算法。
- **只把前 72 字节交给 zxcvbn。** 这不是近似 —— bcrypt 只哈希前 72 字节，
  后面的内容对攻击者不存在。同时它是必要的 DoS 闸门：zxcvbn 匹配是超线性的，
  256 字符要跑 0.5 秒以上，而口令长度由请求方指定（`MaxLength` 最大可配到 256）。
  截断后最坏约 8ms，远低于 bcrypt 自身开销。
- **PRECIS（RFC 8265 OpaqueString）负责归一化与合法性**（`golang.org/x/text/secure/precis`）：
  统一 Unicode 空格、NFC 规范化、拒绝控制字符与未分配码点。
  **归一化结果只用于评估，不用于哈希** —— 存量哈希是按原始字节算的，
  改哈希输入会让所有老用户登不上去，那种迁移要配合双写，不在此列。
- **长度按字符数判定、另加 72 字节硬闸**。按字节算会让 3 个汉字冒充 9 位密码；
  而 `MaxLength` 上限 256 > bcrypt 的 72 字节，不单独把关就会出现
  「前 72 字节相同的两个口令可以互相登录」。
- **模式明细不回传口令片段**，只给位置区间与来源。这个结构会经
  `/password-policy/test` 出网并进审计日志，回显子串等于把被测口令泄露出去。

> **升级影响**：`password_strength_score` 是**落库**的（注册 / 改密时算一次），
> 存量行仍是旧模型下偏高的分数，`/apps` 的密码合规看板会因此偏乐观，
> 直到用户各自轮换密码后收敛。这是一次性的，不需要迁移脚本。

### 密码策略生命周期

`maxAge` 与 `preventReuse` 此前同样只落库不生效。现在：

- **`maxAge`**：写密码时算出 `password_expires_at`（0 = 永不过期，此时**清空**该列而非 COALESCE 保留旧值）；
  过期判定在 `issueSession` 里现算并置 `passwordChangeRequired`，不依赖定时任务 ——
  定时任务漏跑会让过期密码继续可用。
- **`preventReuse`**：新表 `user_password_history` 只存哈希。bcrypt 自带 salt，判重只能逐条
  `CompareHashAndPassword`，因此策略上限锁在 20 条。设为 0 会清空该用户已积累的历史。
- 三条写密码链路（注册 / 用户改密 / 管理员重置）**都**走 `ResolvePasswordLifecycle`，
  否则「管理员帮用户改一次密码」就绕过了整套策略。
- `maxAge` / `preventReuse` 用 `lookupInt`（区分「键不存在」与「显式为 0」）而不是 `intSetting`：
  这两个字段的 0 是有效取值，用后者会让「关掉过期」被静默改回默认 365 天。

### 动态图片验证码（gifcaptcha）

`dynamic` 档此前调的是 `dchest/captcha` 的 `NewImage().WriteTo()`，而那个方法写的是
**PNG**：类型叫 dynamic、`mimeType` 报 image/png、画面一动不动。现在渲染下沉到
[pkg/gifcaptcha](../../pkg/gifcaptcha)，`CaptchaService` 只做「参数 → 渲染」「答案 → Redis」。

逐帧变化的有五类，缺任何一类都会让逐帧相减重新变得可行：

| 逐帧在变的 | 作用 |
|---|---|
| 每个字符各自的位移 / 旋转 / 缩放 | 单帧模板匹配失效 |
| 每个字符的颜色（HCL 转圈） | 按色彩分割字符失效 |
| 漂移噪点 | 噪点固定的话两帧相减就能消掉 |
| 平移的干扰曲线（字前与字后各有） | 打断连通域切分 |
| 全画面水波扭曲 + 扫过的色带 | 背景也在变，没有可当底图减掉的共同帧 |

结构性约束：

- **循环无缝**：周期运动的相位写成「整数圈 × t」，t=1 与 t=0 重合，
  否则每轮交界处闪一下。`TestAnimationLoopsSeamlessly` 钉住。
- **字体内嵌，不读系统字体目录**：最小镜像里往往一个字体都没有，
  依赖系统字体的实现在那种环境画出来是空白且不报错。
- **调色板固定、索引直接算**：`color.Palette.Index` 是 O(像素 × 256) 的最近色搜索，
  240×80×12 帧要跑 5900 万次距离计算。改成 216 色立方体 + 40 级灰阶后索引由算术得出，
  且所有帧共用一张全局色表。
- **字形变换用 `ApproxBiLinear`**：带核重采样在 2 倍缩小时占整条链路四成时间（41ms → 10.6ms）。
- **参数有上界**：宽 × 高 × 帧数决定渲染耗时与响应体大小，超预算时减帧而不是报错。
- 答案按小写落库；字符集剔除 `0/O`、`1/I/L` 这类易混字符。

#### 外观是动态配置

| 作用域 | 存储 | 管理入口 |
|---|---|---|
| 应用级 | `apps.settings.captcha.dynamic` | `/apps/{appKey}?tab=captcha` |
| 平台级 | `platform_settings` 的 `adminCaptcha.dynamic` | `/configuration?tab=security` |

环境变量只留平台总开关 `CAPTCHA_DYNAMIC_ENABLED`（与 image/math/digit 同档），
外观参数不配环境变量 —— 同一件事两个入口时没人说得清哪个生效。

- **渲染参数由调用方带进来**（`GenerateRequest.Dynamic`）：两种作用域存在两张表里，
  服务自己查就得同时依赖 `AppService` 与 `PlatformSettingsService`。
- **读取先铺默认值再反序列化**：存量 JSON 没有 `dynamic` 键、或只存了一半时自动落回默认，
  同时保住「显式写下的 0」（干扰强度调到 0 不能每次读取被改回 45）。
  `TestAppConfigMergesPartialDynamicJSON` 钉住。
- **更新接口里 `dynamic` 是指针，留空即不修改**：那个 PUT 是全量覆盖式的。
- **落库前 `Normalized()`**：库里存的就是实际生效的值。

#### 样张接口

`POST /api/admin/apps/{appKey}/captcha-config/preview`（应用作用域）与
`POST /api/admin/system/captcha/preview`（平台作用域）共用同一个 handler，
参数全在请求体里，不读也不写配置。

- **不落 Redis**，签发不出能通过校验的东西。`TestPreviewDynamicCaptchaLeavesNoRecord` 断言零写入。
- 返回**夹取后真正生效的参数**与字节数，控制台据此说明「填 60 帧、实际 13 帧」。
- 带样张答案：它不是凭据，而管理员要判断的正是辨识度。

不经控制台看效果（默认跳过）：

```bash
AEGIS_CAPTCHA_PREVIEW_DIR=./preview go test ./pkg/gifcaptcha -run TestDumpDynamicCaptchaPreview
```

`audio` 档同时补上了 `Enabled` 的执行点：它与 `dynamic` 的开关此前都只在配置结构体里，没人读。

### DatabaseManager
- 与 `MemoryManager` 同构：构造即可用，`Start` 拉后台采集，`Snapshot()` 纯内存读不打库
- 连接生命周期钩子挂在 `internal/db`（BeforeAcquire/AfterRelease + pgx Tracer），
  借出连接时抓调用栈，因此**连接泄漏能定位到代码行**
- 会话级 `statement_timeout` / `idle_in_transaction_session_timeout` / `lock_timeout`
  随连接下发，是「一条慢 SQL 拖垮整池」的结构性防线
- Unified 模式下 API 与 Worker 各有一个池：只有 `role=api` 的实例写历史时序、跑清道夫

### NotifyHub —— 统一通知出口

平台**唯一**的对外通知出口。业务侧只做一件事：构造 `notifydomain.Event` 并调用
`DispatchAsync`，绝不直接调飞书 / 邮件 / Webhook。

```
业务事件 ──► 订阅匹配（事件 key / 应用 / 优先级下限 / 分类白名单 / 静默窗口）
         ──► 模板渲染（订阅指定 → 事件+渠道类型 → 事件通用 → 内置默认）
         ──► Provider 投递（9 种渠道，并发上限 8）
         ──► notify_deliveries 留痕 + 指数退避重试 3 次
```

- 新增一种 IM 只需实现 `notifyProvider` 接口并注册到 `NotifyHub.providers`，业务代码零改动
- 渠道密钥 AES-GCM 落库，密钥派生自 `SECURITY_MASTER_KEY`（`aegis.notify.master` 用途盐）
- `DedupeKey` 走 `notify_deliveries.dedupe_key` 唯一索引，同一事件对同一渠道只投一次
- `critical` 级别事件**穿透静默窗口**（SLA 超时不该被"免打扰"挡掉）

#### 受众模型（站内信 / 实时推送必读）

平台里有**两套互不相通的收件人主键空间**：

| 受众 | 表 | 外键 | 实时命名空间 |
|---|---|---|---|
| 应用用户 | `notifications` | `users(id)` | `realtime.user.{appid}.{userId}` |
| 管理员 | `admin_notifications` | `admin_accounts(id)` | `realtime.user.0.{adminId}` ← **appid 恒为 0** |

把 `adminID` 写进 `notifications` 要么违反外键，要么静默命中一个同号的应用用户
（跨租户串消息）。因此 `Event.Recipients` 的 `UserIDs` 与 `AdminIDs` 必须分别投递，
`inapp` / `realtime` 两个 provider 都会做双路分发。

事件同时携带两套视角，由 provider 按收件人类型各取所需：

| 字段 | 受众 | 内容 |
|---|---|---|
| `Link` / `Title` / `Summary` | 处理侧 | 控制台深链，含受理人 / 处理组等内部信息 |
| `UserLink` / `UserTitle` / `UserSummary` | 提单人 | 应用内路径，不含任何内部归属 |

**绝不能把 `Link` 下发给应用用户** —— 既点不开，也泄露内部路由结构。
`notify_provider_local_test.go` 里有一条用例专门守这件事。

另外：`ticketRecipients` 会剔除操作者本人（`ticketActor`），
否则谁点「已解决」谁就先收到一条"工单已解决"。

### TicketService —— 工单权限三层模型

可见与可处理的判定分三层叠加，任一层通过即可：

| 层 | 依据 | 典型角色 |
|---|---|---|
| 全局 | 全局作用域的 `ticket:read` | 超管 / 平台管理员 |
| 应用 | 应用作用域角色的 `ticket:read` | 应用管理员 / 运营 |
| 人员 | 受理人 / 提单人 / 关注人 / 所在处理组 | 工单处理专员、被拉进处理组的任何人 |

第三层是"特定人员处理工单"的落点：把某人加进 `ticket_groups` 即可处理组内工单，
**但绝不会因此看到组外任何工单**。范围在 SQL 层收敛（`ticketScopeClause`），
不是查完再过滤；详情响应里的 `permissions`（`ActionSet`）是后端算好的动作集，
前端据此控制按钮显隐，不会出现"点了才 403"。

### 应用级内容中心（app_content.go）

Banner（投放位）与公告，方法挂在 `AppService` 上。三条结构性约束：

1. **富文本在写入时净化，不在读取时。** 公告正文来自控制台的富文本编辑器，
   最终会进控制台预览、客户端 WebView 与公告邮件。放在读取端意味着每个消费方
   都要自己记得做一次，漏一个就是一次**存储型** XSS；放在写入端只有一个入口。
   用 [bluemonday](https://github.com/microcosm-cc/bluemonday) 而不是手写白名单 ——
   标签闭合、属性大小写、URL 协议、实体编码这些坑它全踩过。
   放行 `class` 但**不放行 `style`**：前者是 tiptap 表达对齐/高亮的方式，丢了排版就塌，
   后者是 XSS 的老入口。外链强制 `nofollow` + `target=_blank`，
   否则点一下就把宿主 WebView 导航走、退不回来。
2. **摘要由服务端提取并落库**（html2text）。列表、推送、客户端通知栏要的都是纯文本
   一段；让每一端各自解析富文本既慢，也必然解析出不同结果。按 rune 截断，
   按字节切会把汉字劈成两半。
3. **图片走对象存储，落库的是 `storage://{configID}/{objectKey}` 引用**，
   读取时现解析成带票据的代理地址。可访问 URL 会过期，存进去过两天就是死链。
   `storageRefPrefix` 与平台横幅共用，两处各定义一份会让同一个引用在一处解析得出、
   在另一处解析不出来。

其余要点：

- **`banners.click_count` 现在有执行点了。** 这一列从建表起就在，却从来没有代码写过它，
  于是控制台上「点击 0」既可能是真没人点、也可能是根本没在统计 ——
  而这两件事会导出完全相反的投放决定。补的入口是免登录的
  `POST /api/v1/apps/{appKey}/banners/{bannerId}/click`（目录 key `bannerClick`，
  Kotlin SDK `content.reportBannerClick()`）。曝光则在下发列表时由服务端自己累加。
- **`banners.type` 从「说不出是什么意思的 'url'」改成展示位枚举**
  （hero / popup / splash / notice / card）：一个接口返回全部 Banner，
  客户端按位取用，比每个位开一条接口好维护。迁移 000072 把存量值统一落到 `hero`。
- **公告补上了生命周期**：草稿 / 已发布 / 已归档 + 置顶 + 投放时间窗。
  展示端只下发已发布且在窗口内的，置顶优先；首次发布才盖 `published_at`，
  归档后重新发布沿用原时间 —— 那是同一条公告，改一次状态就把它顶到最前面会误导所有人。
- **拖拽排序一次提交完整顺序**（`ReorderBanners`，单事务批量改写 `position`）。
  提交「把第 3 条移到第 1 条」这类增量指令，在两个管理员同时拖拽时会算出
  谁也没想要的第三种顺序。
- **零值时间等于清空**（`normalizeContentTime`）：前端删掉时间输入框发来的是零值，
  照原样存下去会得到 0001-01-01，那个时间永远早于 now，
  于是「不限开始时间」和「从公元一年开始」在库里长得一样。
- **Redis 缓存键带 `v2`**：两个结构都变宽了，沿用旧键会让升级后的头两分钟里，
  客户端拿到按新结构反序列化、新字段全是零值的旧缓存 —— 比缓存未命中难查得多。

### AppOAuthService
- 渠道配置存 `app_oauth_providers`（每 App 独立，最多 32 个）
- `client_secret` 以 AES-GCM 落库，密钥派生自 `SECURITY_MASTER_KEY`，出网只给 `clientSecretSet`
- 每个渠道独立控制「允许登录 / 允许自动注册 / 允许绑定」，并可自定义端点、scope、
  token 凭据方式（auto/params/basic）、用户信息字段映射（支持点号路径）
- 同一回调地址承载登录与绑定两条链路，由 state 中的 `purpose` 区分

### 支付网关

16 个渠道共用一套适配器接口（`payment_provider.go` 的 `paymentProvider`）：
`Name` / `Describe` / `ValidateConfig` / `TestConnection` / `CreateOrder` / `QueryRemoteOrder` / `HandleCallback`。

**渠道自描述是这套设计的核心。** `Describe()` 返回的 `ProviderMeta` 同时承载展示信息、
能力矩阵、子支付类型与**配置字段 schema**，由 `POST /api/admin/app/payment-config/methods`
下发给控制台，驱动「渠道市场卡片 + 动态配置表单 + 回调地址提示」三处 UI。
因此**新增渠道只需在 Go 侧加一个 `payment_provider_*.go`，前端零改动即自动出现**。
`payment_provider_schema.go` 提供 `fText`/`fSecret`/`inGroup`/`advanced` 等构造器，
`finalizeMeta` 统一补齐分组中文名与兼容用的扁平子类型列表。

图标走 Simple Icons slug（`ProviderMeta.Icon`），与第三方登录渠道同一套约定，
前端 `components/payment/payment-brand-icon.tsx` 按 slug 查表，未收录的渠道用中性 mark 兜底。

其余结构性约束：

- **限额在网关层统一执行**（`enforceAmountLimits`）。此前仅易支付与虎皮椒在各自
  `CreateOrder` 内自检，其余渠道配了 `minAmount`/`maxAmount` 也不生效；现已上移到
  `CreateOrder` 主链路，任何渠道（含后续新增的）自动获得限额保护。
- **展示顺序由 `methodOrder` 固定**，不能直接遍历 `providers` map —— 迭代顺序随机会让
  控制台渠道列表每次刷新都跳动。测试会断言新增渠道已登记进该列表。
- **回调订单定位有三条链路**：表单字段 `out_trade_no`（易支付系/支付宝）→
  `callbackOrderExtractor.ExtractOrderNo` 从原始报文预提取（Stripe/PayPal/Paddle/
  Lemon Squeezy/Razorpay/Coinbase/Square，其回调地址为平台级配置无法带参数）→
  路径段 `/callback/:method/:appid` 提供应用标识后 config-first 解密（微信 v3）。
  预提取**只用于路由定位**，验签一律在 `HandleCallback` 内基于配置完成。
- **签名头必须登记进 `CallbackSignatureHeaders`**，否则传输层不会透传，验签必然失败。
- 验签通过后服务层仍会交叉校验回调订单号与金额，防「用 A 单的合法回调骗 B 单发货」。

#### 退款（payment_refund_service.go）

12/16 个渠道支持接口退款（不支持的：Coinbase Commerce / 码支付 / V免签 / 虎皮椒）。
链路与支付相反，但同样要求「钱与账一致」：

```
校验可退 → 预占额度（落退款单 pending）→ 提交上游 → 结算回写
                                          ├─ success：额度保持占用 + 履约冲正
                                          ├─ failed ：释放额度，可修正后重试
                                          └─ processing：额度保持占用，等补偿轮询
```

- **预占先于上游调用**是并发安全的关键：`payment_orders.refunded_amount` 记的是
  「已占用额度」而非「已成功退款额」。两个并发退款请求在订单行锁上串行化，
  第二个看到的是已被抬高的额度，超额即拒 —— 「同一笔钱退两次」在数据库层面即不可能。
  DB 侧还有 `CHECK (refunded_amount BETWEEN 0 AND amount)` 兜底。
- **能力矩阵与接口实现由测试强制对齐**：`Capabilities.Refund` 为 true 必须实现
  `paymentRefunder`，反之亦然；否则控制台会显示点了才报错的退款按钮。
  `PartialRefund=false` 的渠道（PAYJS 只能整单退）在发起前就被网关拦下。
- **履约冲正**（`reversal_status`）：退款成功后回收已发放权益。
  余额充值按退款额等额扣回；**积分与会员时长只在全额退款时回收**，部分退款记 skipped
  交人工处理（按比例回收既难解释又易因已消费而失败）。
  冲正失败**不回滚事务** —— 上游的钱已经退出去了，让退款单结算失败会造成更严重的错位，
  因此如实记 `reversal_status=failed` + 原因，控制台以红色徽标提示人工介入。
- **余额通道**走 `RefundPaymentOrderToWallet`：打款也在本地事务内，
  退款单结算 + 钱包入账 + 冲正一次成型，不存在中间态。
- **未结算退款单每 2 分钟补偿轮询**（`syncPendingRefunds`）：微信/支付宝/Paddle 可能
  返回「受理中」，或结算写入时进程崩溃，靠轮询向上游核对收敛到终态。
  微信 `ABNORMAL` 归入 processing 而非 failed —— 资金可能在途，判失败会错误释放额度。
- 本地退款单号即上游 `out_refund_no`，天然幂等键；字符集守微信最严约束（数字/字母/`_-|*@`）。

#### 支付凭证（payment_receipt.go）

用户与管理员都能为一笔订单导出 A4 凭证 PDF，**默认英文**，共 10 种语言。
本文件只负责「订单 + 用户 + 应用 + 退款单 → 一份与业务无关的 `receipt.Document`」，
排版与字体交给 `pkg/receipt` —— 换行、缺字、分页这些问题与支付业务毫无关系，
混在一起会让两边都难改。

- **凭证类型按订单状态推导**：已支付出收据、未支付出账单、全额退款出退款凭证。
  给一笔还没收到的钱开「收据」就是在伪造凭证。
- **凭证编号是 `RCP-<订单号>`**，同一订单反复导出得到同一个编号 ——
  编号是给人对账用的，每次下载都换一个号会让对账无从下手。
- **币种在下单时固化**（`payment_orders.currency`，迁移 000068），
  取值走渠道 `Describe().Currencies` **自述**而不是在这里另建一张 method → 货币的表。
  开凭证时回读 `payment_configs` 是错的：配置会变，已发生的交易不会。
- **「已退款」只统计退款单里已成功的部分**，不是订单上的 `refunded_amount`
  （那是已占用额度，含在途退款）—— 印一笔还没到账的退款会直接引发客诉。
- **语言优先级**：请求参数 → 用户设置 → `Accept-Language` → 平台默认。
  用户设置排在请求头前面：那是一次明确表达，不该被浏览器偏好覆盖。
- **缺中日韩字体时降级而不是报错**：语言退回英文并在 `localeFallback` / `degradedGlyphs`
  里如实上报。拉丁字形内嵌在二进制里，因此「凭证出不来」在任何环境下都不会发生。
- **下载校验同时核对 appid 与 userId**，billId 必须是纯十六进制（挡路径穿越）；
  管理端代开不落盘 —— 一次性下载没有分享场景，落盘只会多留一份交易明细在磁盘上。
- **订单查询自带 `receipt` 区块**（`ListUserOrderViews` / `GetUserOrderView`）：
  凭证类型、推荐语言、能否寄送都由服务端算好。放到客户端会各端各写一套且很快不一致。

#### 钱包流水凭证（payment_receipt_wallet.go）

支付订单与钱包流水是**两条并行的资金记录**，此前只有前者能出凭证。
于是三种「用钱包付的钱」拿不到任何可归档、可报销、可对账的文件：
余额直购会员（`/vip/purchase`）、业务消费（`/wallet/consume`）、管理员调账 ——
它们都只落 `wallet_transactions`，不产生订单。

- **同一笔钱只出一份凭证**。流水挂着 `related_order_no` 时（充值到账、余额支付订单），
  凭证**由订单出具**，钱包这边只是把同一份文档再交付一次。否则同一笔交易会有
  `RCP-WAL…` 与 `RCP-P…` 两个编号，对账时无从判断哪个算数。
  委派前三重校验（订单存在、同应用、同用户）：`related_order_no` 是一列没有外键约束的文本。
- **列表上的凭证入口不查订单表**。一页 20 行逐行确认关联订单就是 20 次查库；
  出具方按 `related_order_no` 是否存在推导即可，而下载入口**恒指向钱包这条路由**，
  因此关联订单被清理时按钮也不会失效（内部退回按流水自行出具）。
- **合计恒为正数**，方向由类型与附注表达。印一个负数总额会让任何一款报销系统都拒收。
- **品名分两种来源**：平台生成的标题走译文键（`wallet.type.*`），否则一份英文凭证的
  商品栏会是中文；`consume` 的标题是**接入方填的**消费说明，是真正的业务内容，
  翻译它等于把用户的数据改掉。`receipt.LineItem.NameKey` 就是为这件事加的。
- **不占用「订单号」与「交易流水号」两个字段**。前者真的没有；后者在支付明细区
  表示的是**上游渠道**的单号，内部账本没有这个东西，填进去会让人拿着它去找渠道对账。
  流水号走附加信息区。
- **变动前后余额必须印**：只写「扣了 50」的凭证无法自证，
  而「变动前 200 → 变动后 150」可以与对账单逐行核对。
- **币种取应用级 `walletCurrency`**（默认 CNY）：钱包余额没有币种列，
  而一份印着数字却不说是哪国钱的凭证既不能报销也不能对账。
- **自动寄送与订单共用同一个开关**（`receiptEmailOnPaid`）：用钱包付的钱与用支付宝
  付的钱，收不收得到收据不该有区别。但只挂在**购买**上（余额直购会员），
  不挂消费与调账 —— `consume` 一天可能发生几百次，每次寄一封信会先把邮件配额烧光。
- **装配是纯函数**（`assembleWalletReceiptDocument` + `walletReceiptEnv`）：
  应用名 / 品牌 / 币种各要打一次库，混在装配里就意味着「凭证长什么样」这件事
  没有数据库就测不了。

#### 凭证邮件（payment_receipt_email.go）

- **附件能力如实声明**（`emailSender.SupportsAttachments`），调用方**先问能力再写正文**。
  顺序反过来，正文已经写着「收据见附件」了才发现带不了。SMTP 支持；
  Zeabur 的 REST 接口没有公开附件字段，即便它的请求体与 Resend 同构也不赌 ——
  未知字段被忽略的表现是「邮件送达、附件不翼而飞」，谁也发现不了。
- **通道带不了附件时当场报错**，绝不静默丢掉附件再把信发出去（`sendMail` 里挡住）。
- **签名下载链接恒附带**，即使已有附件：邮件会被转发、附件会被网关剥离。
  签名覆盖 (应用, 凭证标识, 失效时刻) 三者，少签一项就能改出另一条合法链接。
  这条路由**必须在鉴权组之外** —— 邮件客户端里没有登录态。
- **邮件正文与 PDF 同语言**：共用 `pkg/receipt` 的同一份译文与同一次协商结果。
  中文邮件配英文 PDF 是最难向用户解释的错位。
- **用户自助不接受指定收件地址**，恒为账号绑定邮箱：允许任意填写等于把平台
  变成一个能带 PDF 附件的转发器。补发频次上限走 `email_deliveries` 留痕统计，不另建计数表。
- **自动寄送只挂首次确认支付**，异步且失败不反噬支付链路。

完整说明见 [docs/payment-receipt.md](../../docs/payment-receipt.md)。

### 会员判定与试用期会员

「这个用户是不是会员」以前是一行到处重写的表达式（`vip_expire_at.After(now)`），
散落在仓储、远程函数 SDK、CSV 导出三处。它只回答得了"是/否"，
而客户端真正要的是四个答案，少一个界面就只能猜：

| 问题 | 字段 | 不回答会怎样 |
|---|---|---|
| 是不是会员 | `isVip` | —— |
| 还剩多久 | `expireAt` / `remainingSeconds` / `remainingDays` | 到期前无法提醒 |
| 是不是**试用**会员 | `isTrial` / `trial` | 试用用户被引导去"续费"、付费用户被弹"免费试用" |
| 试用还能不能领、为什么不能 | `trialOffer.available` / `.reason` | 入口该藏还是该亮，客户端只能猜 |

因此判定只有一个入口 `VipService.ResolveEntitlement`：事实一次查齐
（`GetVipEntitlementFacts` 一条 SQL），结论由**纯函数** `vipdomain.Evaluate` 算出。
纯函数意味着这套判定不需要数据库就能测 —— 边界情况（刚好到期、试用中途买了付费、
领过但已过期）在 `internal/domain/vip/entitlement_test.go` 里表驱动跑一遍。
用户端 `/vip/status`、管理端 `/vip/entitlement`、远程函数 `aegis.user.get()`
读的都是这一份结论，不可能各说各话。

#### 试用是套餐的一种，不是另一套时长体系

试用仍然只是把 `vip_expire_at` 往后推、仍然进 `vip_transactions` 账本
（`pay_channel = 'trial'`）。`vip_plans.kind = 'trial'` 标出它是哪个套餐，
新增的只有一张资格账本 `vip_trial_claims`。

把「试用几天」放进 `apps.settings` 是另一种做法，但那会立刻产生两处配置
（套餐里一个时长、设置里另一个时长），且控制台上"改哪个才生效"没人说得清。

| 约束 | 落点 | 为什么 |
|---|---|---|
| 一人一次 | `uq_vip_trial_claims_user` 唯一约束 | 应用层判断会被并发穿透，约束不会 |
| 试用套餐恒 0 元 | `ck_vip_plans_trial_free` + 保存时报错 | 定价 > 0 的试用等于一个可反复触发的免费入口 |
| 每应用至多一个启用中的试用 | `uq_vip_plans_active_trial` | 多于一个时「点领取到底领哪个」只能靠 `ORDER BY` 的偶然顺序 |
| 试用套餐不能购买 | `requireActivePlan` + `PaymentService` 下单校验 | 0 元套餐走购买入口就绕过了全部资格判定 |
| 试用不进 `/vip/plans` | `ListPurchasableVipPlans` | 那是"能买什么"的列表；混进去客户端会渲染一张点了必然报错的卡片 |
| 会员期内不能领 | `ClaimVipTrial` 事务内判定 | 那不是试用，是白送几天 |
| 同设备只能领一次（可选） | 事务内查 + `uq_vip_trial_claims_device` | 开关开着却因为请求没带设备标识而放行，等于没有这个开关 —— 所以缺标识是**拒领**（40040） |

其余几处刻意的取舍：

- **领取没有幂等键**：试用天然一人一次，唯一约束就是幂等键。
  仍在试用期内的重复请求返回上一次的结果（`replayed = true`），
  而不是一句"你已经领过了"—— 那是最常见的一次网络重试。
- **「当前是不是试用中」是推导出来的**：`vip_expire_at` 恰好等于这次试用发到的时刻。
  用户后来买了付费，到期时间被推远，判定自动切换，不需要任何状态迁移，
  也不会出现"买了付费还显示试用中"。
- **试用期内购买是顺延而不是作废**（沿用 `extendUserVipTx` 的既有语义）：
  剩余试用天数叠加到付费时长上。这是唯一不会让用户觉得"我一付钱就少了几天"的做法。
- **管理端重置资格只删资格，不收回已发的时长**：客服要的是"让他重领"，
  顺手扣掉时长会变成用户眼里的"我的会员没了"。
- **管理员代领仍走同一套资格判定**（`AdminClaimTrialFor`），刻意不给"跳过资格"的开关：
  要跳过就是直接送天数，`AdminGrantVip` 已经能做且会如实记成 `admin_grant` ——
  混进试用里会污染转化率，而转化率正是开试用的理由。

### 服务端会员校验与功能标识

判定入口仍然只有 `ResolveEntitlement` 一个，这一节加的是**谁来问**与**问得多细**。

#### 谁来问：接入方自己的后端

此前只有两条问路：客户端拿用户令牌问 `/vip/status`，管理员拿管理端令牌问
`/vip/entitlement`。接入方**自己的服务器**两样都不该有 —— 它没有用户令牌
（那是用户的东西），更不该配管理员账号（那是整个租户的权限）。于是它只能相信
客户端捎上来的那句"我是会员"，而这句话客户端说了不算。

```
POST /api/apps/{appKey}/vip/verify        X-Aegis-Function-Key: afk_…
{ "accessToken": "eyJ…", "feature": "export" }
```

两样凭据各证明一件事：**密钥证明「谁在问」，令牌证明「问的是谁」**。

| 取舍 | 理由 |
|---|---|
| **只接受 accessToken，不接受 userId / account** | 接入方的后端几乎一定会把「当前请求是谁」交给它自己的客户端来说。收 userId 就等于把身份判定外包给客户端：「客户端自报 42 → 接入方转发 → 我们回答 42 是会员 → 接入方放行发起请求的那个人」，攻击者知道任意一个会员的 userId 就能白嫖，而服务端密钥拦不住 —— 犯错的正是持有密钥的那一方 |
| 不在 `/api/v1/apps/*` 网关命名空间下 | 那条命名空间围绕「用户令牌 + 三档包装」设计；服务端调用不该为了问一句话去实现签名与加密 |
| 复用远程函数的调用密钥（`afk_…`） | 再造一套"会员校验专用密钥"只会让接入方在服务器上配两份凭据，而它们的信任级别完全一样 |
| 令牌还要核对归属应用（40372） | 否则拿 A 应用的令牌能问出 B 应用同号用户的状态 |
| 按 userId 批量查走管理端 | 对账、到期提醒、客服工单确实需要它，但那条路有管理员鉴权与审计，且调用方不可能把它误接到客户端上 |

`verifyMembership` 是**不导出**的（小写），对外入口只有 `VerifyMembershipByToken` ——
让"按 userId 校验"在类型层面就不可达，比在文档里写一句警告可靠。

#### 问得多细：功能标识（feature tag）

「是不是会员」只有一个维度。接入方一旦有两档会员（基础版能导出、高级版还能用 AI），
后端就只能拿套餐名做字符串比较 —— 而套餐名是运营随时会改的展示文案。
功能标识把「卖的是哪个套餐」与「解锁的是哪个能力」拆开：

```
vip_features               应用维护的功能目录（tag → 展示名）
vip_plans.features         这个套餐包含哪些功能
vip_transactions.features  开通那一刻的功能快照
```

| 约束 | 落点 | 为什么 |
|---|---|---|
| 校验时传未登记的标识 → 报错 40486 | `VerifyMembership` | 拼错一个字母（`exprot`）在自由字符串方案下表现为"永远返回 false"，没有任何一处说得出为什么 |
| 套餐引用的标识必须都在目录里 | `EnsureFeatureTagsRegistered`（保存套餐时） | 同上，只是把报错时机从"几周后接入方来问"提前到"保存那一刻" |
| 开通时**快照**功能列表 | `Grant.Features` → `vip_transactions.features` | 套餐配置随时会改，已经卖出去的权益不该被追溯改写 |
| 当前权益 = 尚未到期的每一段的**并集** | `activeFeatureUnionSQL` | 会员期是顺延的：先买基础版再买高级版时两段都没到期，两边功能都该生效；用完的那几段自然出集合，权益随时间自己收敛 |
| 功能权益以「是不是会员」为前提 | `Entitlement.HasFeature` | 过期用户的快照仍在账本里，只按标签命中会让到期三个月的人继续用高级功能 |
| 删除功能标识不级联清套餐 | `AdminDeleteFeature` 返回 `affectedPlans` | 删功能是运营动作，不该被"还有套餐在用"卡住；残留引用会在校验入口明确报错，比静默改写一批套餐配置好排查 |

支付直购的功能快照在**下单**时取（`MetaKeyVipFeatures`），与天数、价格同一时刻 ——
从下单到支付成功可能过去几天，用户拿到的必须是他下单时看到的那一份。

客户端侧同一份结论也能拿到：`/vip/status` 的 `features`、远程函数的
`aegis.user.get().vipFeatures`。三处都由 `ResolveEntitlement` 投影而来
（`Entitlement.View()`），不存在"服务端说有、客户端说没有"。

### 远程函数 —— 能力目录是单一事实源

`script` 运行时把接入方的自定义 API 逻辑放进 Aegis 进程内的 goja 沙箱。
沙箱是 deny-by-default：脚本能做的每一件有副作用的事都必须由 `ScriptSDK` 显式注入。

**能力目录在 [internal/domain/appfunction/capabilities.go](../domain/appfunction/capabilities.go)**，
同时驱动四处：服务端校验、SDK 绑定、控制台勾选框、编辑器类型提示。
新增一种能力只需目录加一行 + SDK 加一个 binder，控制台零改动即自动出现 ——
与支付渠道 `Describe()`、风控条件目录同一套做法。
`TestCapabilityCatalogMatchesBinders` 双向钉死「目录 ↔ 绑定分支」：
目录多一条 → 勾得上却没有那个对象；绑定多一条 → 没声明也能调。

TypeScript 声明片段也放在目录里，随 `/function-catalog` 下发。放在控制台
另写一份的后果是「补全里有、运行时没有」，而这种错误要到发版之后才暴露 ——
编辑器提示的全部价值就是提前暴露它。

修掉的几个「看起来能用、其实走不通」：

| 问题 | 后果 |
|---|---|
| `CreateFunction` 只放行 `wasm`/`http` | 控制台默认选 `script`，**创建表单按默认值提交必然 40091**，功能从第一步就断 |
| 版本正文任何接口都取不回来 | 改一行脚本要从零重写整份 |
| 没有试跑 | 验证一行改动的唯一方式是把半成品激活到线上，且每改一次多一条永久版本 |
| 能力/超时/限额建好即锁死 | 想加一项能力只能删掉重建，而删除会连同全部版本与调用审计一起消失 |
| 并发上限硬编码为 8 | 20ms 的脚本与 3s 的 HTTP 转发共用同一个闸门 |

其余结构性约束：

- **试跑读真、写假**。读假数据毫无意义（脚本分支几乎全由服务端状态决定），
  写真数据会让「试一下」变成一次不可撤销的线上操作。`points.add` / `kv.incr`
  在试跑时读一次真实值再算出「如果执行会变成多少」，否则
  「今日额度已用尽」那条分支永远测不到。出站请求按方法分流：GET/HEAD 照发，
  其余跳过 —— POST 可能是一次扣款。
- **试跑失败返回 200**。失败是正常结果，作者要的是错误内容加上那之前的日志与
  effects；回 4xx 会把这些全丢掉。真正的接口错误（函数不存在、语法不过）仍是 4xx。
- **试跑用函数已声明的能力**，请求侧不能临时加：否则「试跑通过」证明不了
  「发版之后能跑」，而那正是试跑本该拦住的事。
- **频次限制走数据库原子自增**（`app_function_kv` + `__aegis:` 保留前缀），不是内存计数。
  内存计数在多实例下的表现是「配了 60/分钟，实际放行 60×实例数」，而控制台上看不出来。
  保留前缀脚本读写不到，否则脚本能把限制自己的那个计数清零。
- **并发闸门记住容量**：`maxConcurrency` 可在控制台改，而 channel 容量创建时定死，
  不比对就会出现「显示 32、实际仍是 8」且无处报错。
- **函数配置（`aegis.config`）顶层必须是对象**：数组或标量会让 `aegis.config.x`
  恒为 undefined，而那种失败不报错，只让阈值静默变回代码里的默认值。
- **`console` 绑成 `aegis.log` 的别名**：「沙箱里没有 DOM」与「没人绑 console」是两回事。
- **`aegis.email.send` 不接受收件地址**（恒为调用者绑定邮箱），与凭证邮件同一条约束：
  允许任意填写等于把平台变成一个谁都能驱动的转发器。

完整说明见 [docs/app-functions.md](../../docs/app-functions.md)。

### 邮件出口 —— 两档 provider

`EmailService.sendMail` 是平台**唯一**的邮件出口，验证码 / 密码重置 / 欢迎信 /
资料变更通知 / NotifyHub 的 `email` 渠道全部经过它。传输方式由配置的 provider 决定：

| provider | 传输 | 何时用 |
|---|---|---|
| `smtp`（默认） | 直连 SMTP | 自有服务器 / 不封出站端口的环境 |
| `zeabur` | Zeabur Email REST API | **Zeabur 部署的唯一选项** —— 平台底层封禁出站 SMTP 端口，直连必然超时 |

新增服务商只需实现 `emailSender` 接口并注册进 `EmailService.senders`，业务代码零改动。
`resolveSender` 对未知 provider **直接报错而不静默回落到 SMTP** ——
回落会让「配了 A 却在用 B」这种故障以超时的形式出现在几层之外。

其余结构性约束：

- **密钥 AES-GCM 落库**（用途盐 `aegis.email.master`），管理接口出网一律抹除，
  只保留 `apiKeySet` / `webhookSecretSet` 布尔位。所有密钥字段**留空即不修改**
  —— 前端编辑态从不回显密钥，直接覆盖会让改个发件人名就把密码清空。
- **投递留痕失败绝不反噬发信**（`recordDelivery` 只记 warn）：信已经交出去了，
  此时报错只会让调用方重发一封。留痕用 `context.WithoutCancel`，
  避免请求结束把事后账一起取消掉。
- **webhook 状态单向推进**（判定写在 SQL 里）：终态不被后到的 `delivery` 覆盖，
  `open`/`click` 只累加计数不动主状态。webhook 到达顺序没有保证，
  乱序会把已退信的邮件显示成投递成功。
- **429 不重试**：Zeabur 日配额按 UTC 00:00 重置，重试只会拖长请求并加深熔断。
  两档 provider 各有独立熔断器，互不牵连。
- SMTP 超时的错误文案**点名 Zeabur** —— 出站被封时表现就是纯超时，
  只说「检查网络」会让排查一路走偏到邮箱服务商那边。

完整接入说明见 [docs/zeabur-email.md](../../docs/zeabur-email.md)。

#### 邮件模板（email_template.go + emailtpl/）

| 文件 | 角色 |
|---|---|
| `email_template.go` | 内容模型（`emailLayout` + `mailBlock`）、全部文案、渲染入口 |
| `emailtpl/layout.gohtml` | HTML 骨架（表格布局），`html/template` 渲染 |
| `emailtpl/layout.gotxt` | 纯文本骨架，`text/template` 渲染 |
| `emailtpl/theme.css` | 样式表，色值逐条对应控制台的设计令牌 |

**业务代码不写 HTML。** 调用方只声明内容 —— `mailParagraph` / `mailCode` /
`mailDetails` / `mailButton` / `mailLink` / `mailNotice` —— 长什么样是模板的事。
样式写成一张类名样式表，由 [premailer](https://github.com/vanng822/go-premailer)
在渲染时内联进标签（邮件客户端普遍不认 `<style>`）。重构前是 400 行 `fmt.Sprintf`
拼字符串加手工 `html.EscapeString`：同一个色值散落在几十处，转义漏一处就是注入。

六条硬约束：

1. **表格布局，不用 flex / div 上的 max-width / div 的 border-radius**。
   Outlook 2007–2021 用 Word 排版引擎，全都不认。旧版字段行用
   `display:flex;justify-content:space-between`，在 Outlook 里标签与值竖排。
   有测试扫描产物里的 `display:flex`。
2. **纯文本版从同一份内容模型渲染**，不是把 HTML 抓一遍。抓取版必然带上
   预览行的零宽字符与按钮重复文案，且 HTML 一改就悄悄劣化。
   只有「正文是外部给的一段 HTML」那条路径才回落到 `htmlToPlainText`（html2text 库）。
3. **`prefers-color-scheme` 与窄屏规则必须留在 `<style>` 里**：premailer 内联不了
   媒体查询，且内联样式优先级更高，所以深色规则一律带 `!important`，
   并且 `WithRemoveClasses(false)`（类名是媒体查询唯一的抓手）。
4. **VML 条件注释由 Go 侧以 `template.HTML` 注入**。`html/template` 会把模板源码里的
   HTML 注释整段删掉，写在 `.gohtml` 里的 `<!--[if mso]>` 到不了收件人手上。
   没有它 Outlook 只会画出一行裸链接。
5. **色值抄控制台的设计令牌**（`aegis-console/src/app/globals.css` 的 `:root` / `.dark`），
   对照表写在 `theme.css` 顶部。邮件是产品的一部分，收件人前一分钟可能还在看控制台。
6. **文案每句只说一次事**。旧版 12 个验证码场景共用同一句「本次验证码用于 XX，
   请在有效期内完成验证」+「请勿将验证码泄露给任何人」，标题、引导句、字段行「用途」
   把同一件事说三遍，真正要看的那行反而被淹没。`TestPurposeCopyStaysSpecific`
   会扫套话黑名单、检查各场景引导句互不重复、以及引导句有没有复述标题。

验证码邮件的主题把码放在最前面（「482913 是您的登录验证码」）：手机通知栏和
邮件列表只显示开头十几个字，放在那里往往不用点开邮件。

人工核对排版用预览导出（默认跳过，不进 CI）：

```bash
AEGIS_MAIL_PREVIEW_DIR=./preview go test ./internal/service -run TestDumpEmailPreviews
```

### AvatarService —— 头像地址必须是永久的

头像和别的资源有一个不一样的性质：**它的地址会被别人存起来**。控制台存进
localStorage、移动端存进本地库、邮件正文里嵌成 `<img>`、中间还可能有 CDN。
因此「当场可用」是不够的。

重构前的链路交出去的是一个 **30 分钟**的存储代理票据（`/api/storage/proxy/{ticket}`），
于是所有那些副本半小时后一起变成死链。更致命的是第二跳：读-改-写的客户端会把
那个临时地址**原样 PUT 回来**，覆盖掉库里唯一那份 `storage://` 引用 ——
这之后头像不是过期，是永久丢失。这条链路只在自定义上传时存在，
因为只有它才产生 storage 引用（第三方头像与 Gravatar 都是永久外链）。

```
落库：storage://{configID}/{objectKey}      （不变，存量零迁移）
出网：/api/avatars/{ownerToken}?v={version}  （永久，编码的是「谁」不是「哪个对象」）
```

服务端解析时**不看** `v` —— 那个参数只用来破缓存。因此换了头像地址不变，
两年前存下的那份副本今天点开拿到的仍是这个人今天的头像。没有头像时它指向
服务端生成的默认头像，所以 `avatar` 字段**恒不为空**，客户端不必各写一套兜底。

其余结构性约束：

- **`NormalizeAvatarInput` 是资料更新链路上唯一的头像入口**（用户端与管理端共用）。
  临时票据地址与自家永久地址一律判回「不修改」；客户端提交的 `storage://` 引用
  **拒绝** —— 不挡的话任何登录用户都能把头像设成 `storage://3/别人的私有文件.pdf`
  再从头像地址上读出来。非 http(s) 协议一并拒绝（`javascript:` 进了 `<a href>` 是可执行的）。
- **解析不做任何存储访问**。原来每读一次资料就往 Redis 写一个票据，一个 20 行的
  用户列表就是 20 次写入，而其中大多数图根本不会被下载。现在推迟到真的有人取图时。
- **自愈**：库里那一列是空的或明显是临时票据地址时，按 `avatar_assets` 找回引用并回写。
  其它形态一律不碰 —— 用户可能就是想设一个外链，自作主张改回去比丢了更糟。
  因此这次修复**不需要迁移脚本**，受影响的行下次被读到时自己好。
- **移除头像以前没有入口**：更新资料时空串的语义是「不修改」，传过一次就再也回不去。
  现在有 `DELETE /me/avatar`，`avatar_assets` 里那条置为 `deleted` 而不是删行
  （对象还在，才有「换回上一张」）。
- **`upload.avatar` 仍是字符串**。改成对象会让所有已发布的 App 在上传成功后
  把一个 `[object Object]` 当图片地址加载；新增的结构挂在 `upload.view` 上。
- **管理员头像走平台级存储**（appID=0）：挂到某个应用下面的话，那个应用被归档时
  管理员的头像会跟着一起没了。
- **取对象失败时回落默认头像而不是 404**。存储配置被删、桶里的文件被清了，
  界面上会变成一个碎图标，而用户什么都没做错 —— 真实原因留在日志里。

图像管线（`avatar_pipeline.go`）与默认头像（`avatar_identity.go`）的逐条取舍
见 [docs/avatar.md](../../docs/avatar.md)。

### RealtimeService
- 每个 WebSocket 连接对应一个 NATS Subject（`realtime.user.{appid}.{userid}`）
- 支持多设备同时在线（同一用户多连接）
- 连接状态同步写入 Redis

### LocationService
- 首次查询写入 Redis 缓存（TTL 24h）
- GeoIP2 mmdb 文件自动从 GitHub 下载并定时更新
- 可选 `ChinaOptimized` 模式和远程 HTTP fallback

### PlatformSettingsService
- 支持动态更新 Firewall 配置（CIDR 黑白名单、限流规则）
- 启动时调用 `Initialize` 从数据库加载持久化设置
