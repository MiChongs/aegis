# 注册 IP 控制

> 面包屑：[Aegis](../CLAUDE.md) › docs/register-ip
> 代码：`internal/domain/regip`（策略、网络主体、名单编译、突增检测与判定，纯函数）·
> `internal/service/register_ip_service.go`（准入与票）· `register_ip_admin.go`（事实来源、批量写入、管理端）·
> `internal/repository/redis/register_ip_repository.go`（计数脚本）· `internal/transport/http/register_ip_handlers.go` ·
> 控制台应用详情 `?tab=register-ip`

同一个网络能注册多少账号、注册多快，超出时怎么处置。取代旧开关 `registerCheckIp`（「同一 IP 不允许重复注册」）。

## 判定

一次注册按下面的顺序判，前面的有结论就不再往后看：

| 顺序 | 项 | 结论 |
|---|---|---|
| 1 | 缺少注册 IP | `missingIp`：放行（默认）或拒绝（40025） |
| 2 | 名单 | 拒绝名单 → 40919；放行名单 → 跳过后面全部（照样计数）；放宽名单 → 配额乘倍数 |
| 3 | 惩罚期 | 42917，提示剩余时长 |
| 4 | 属地 | `geo.mode=allow` 不在名单内 / `deny` 在名单内 → 按 `geo.action` |
| 5 | 运营商 | 名称含关键词（机房、云厂商）→ 按 `isp.action` |
| 6 | 突增 | 应用注册量异常 → 按 `surge.action`，并把全部配额乘 `tightenFactor` |
| 7 | 配额规则 | 每条规则独立数；多条超限时 拒绝 > 加验 > 观察，同档取第一条 |

处置：`reject` 拒绝、`challenge` 要求图形验证码（40015，客户端带验证码重试即放行）、`observe` 只记录。
`mode=observe` 时整套策略只记录「本该」的结论，一律放行 —— 新规则先观察几天再切 `enforce`。

### 配额规则

```json
{ "id": "ip-day", "scope": "ip", "windowSeconds": 86400, "limit": 10, "action": "reject", "methods": ["password", "sms"] }
```

| `scope` | 主体 |
|---|---|
| `ip` | 单个地址。IPv6 按 `network.ipv6Host`（默认 /64）聚合：运营商一户一段 /64，隐私扩展地址一天换好几次 |
| `subnet` | 网段：IPv4 `network.ipv4Subnet`（默认 /24）、IPv6 `network.ipv6Subnet`（默认 /48） |
| `city` | 同一属地（国家 / 省 / 市）。属地查不到时这条规则不参与 |
| `app` | 整个应用（必须有窗口） |

`windowSeconds=0` 表示累计（不限时间），以**注册台账**为准；`methods` 为空表示四种注册方式（`password` / `sms` / `oauth` / `cardkey`）都算。

### 惩罚期

`penalty.strikes` 次配额拒绝（`strikeWindowSeconds` 内）后，地址（或网段，`penalty.scope`）整体禁止注册。
第 n 次进惩罚期时长 = `baseSeconds × multiplier^(n-1)`，封顶 `maxSeconds`；等级保留 `maxSeconds × 2 + 1 天`，再犯从上一级接着翻倍。

### 突增检测

每分钟注册数记在 Redis 分钟桶里。最近 `windowMinutes` 分钟之和与此前 `baselineMinutes` 分钟的平常水平比：

- 平常水平用 EWMA（α = 2/(N+1)）估计每分钟均值 m 与方差 v；窗口期望 μ = m·W，σ = max(√(v·W), √μ, 1)
- 触发线 = max(`minRegistrations`, μ + `sensitivity`·σ)。泊松下限 √μ 让平时几乎没人注册的应用不会因为来了两个人就「偏离几十个标准差」
- 状态 5 秒一算（一次 MGET），概览里实时展示当前值、期望、触发线与偏离

## 计数怎么做

全部规则在一个 Lua 脚本里**原子地数完、都没有挡住的超限才占位**，并发注册不可能一起钻过上限（集成测试：24 个并发请求、网段每小时 5 个，恰好放行 5 个）。

| 计数方式 | 用于 | 说明 |
|---|---|---|
| 滑动窗口日志（ZSET） | 窗口规则，上限 ≤ 2000 | 精确 |
| 滑动窗口子桶（HASH，60 桶） | 窗口规则，上限 > 2000 | 误差不超过窗口的 1/60，内存与上限无关 |
| 累计计数 + 待确认占位 | 累计规则 | 计数缺失时按台账回填（带索引的范围 COUNT，最多 4 条并行），30 天续期 |

注册写库之前占位，拿到一张票：账号建好 `Commit`（占位转正、分钟桶加一，异步）、没建成 `Release`（名额还回去）。
累计规则的占位 5 分钟没确认自动作废（中途崩溃的兜底）。时间一律取 Redis 服务端时间，多实例不受时钟漂移影响。
同一应用的键都带 `{regip:<appid>}` 哈希标签，集群部署时落在同一个槽。

热路径不查库：名单是 15 秒的内存快照（`netipx.IPSet` 有序区间表，O(log n)；过期时后台刷新、旧快照先顶上），
名单、属地、突增三样并行取；台账与拦截记录由多协程批量写（台账一条 `INSERT … unnest`、记录一次 `COPY`，属地在批量写时用 `ResolveMany` 一次补齐）。

计数存储出错时按 `failMode`：`open` 放行并记 warn（默认），`closed` 拒绝（42919）。

## 旧开关

没保存过新策略的应用按 `registerCheckIp` 推导：开着就是「同一地址累计最多 1 个、拒绝，缺 IP 拒绝」，IPv6 按完整地址比对，行为与升级前一致。
保存新策略时写回 `registerCheckIp`（有一条「同一地址累计 1 个、拒绝、全部方式」的规则时为真），只给还在读它的旧客户端看。
旧的 `PUT /apps/:appkey/policy` 在保存过新策略之后不再改这个键。

迁移 000143 第一次建表时从用户档案回填台账（`register_ip` 是自由文本，带端口、方括号、IPv4 映射的都认，解析不了的跳过），
否则开启累计规则的那一刻所有老地址都从零算起。删号后台账行保留（`user_id` 置空）：否则「注册 → 注销 → 再注册」就能绕过累计上限。

## 错误码

| 码 | HTTP | 含义 |
|---|---|---|
| 40015 | 400 | 需要图形验证码（加验），带验证码重试 |
| 40025 | 400 | 缺少注册 IP 且策略要求拒绝 |
| 40902 | 409 | 当前网络注册的账号数已达上限（沿用旧开关的码） |
| 40919 | 409 | 当前网络环境暂不允许注册（拒绝名单、属地、运营商） |
| 42917 | 429 | 惩罚期内，message 带剩余时长 |
| 42918 | 429 | 注册量突增，暂停注册 |
| 42919 | 429 | 计数存储不可用且 `failMode=closed` |

## 接口

管理端（`/api/admin/apps/:appkey/register-ip`，读 `app:user:read`；改策略与名单 `app:write`；解除 `app:user:write`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `` | 概览：`days`、`tz`（浏览器时区）。注册与拦截趋势、原因、规则命中、每地址账号数分布、排行、惩罚期、突增状态、写入队列 |
| GET / PUT | `/policy` | 策略（GET 带 `defaults` 推荐值） |
| POST | `/simulate` | 模拟：草稿策略 + 地址；计数取真实值只数不占，可改写 `counts`、`location`、`surgeActive`、`penaltySeconds` |
| GET | `/inspect?ip=` | IP 查询：名单、惩罚期、各规则用量与重置时间、此刻注册会怎样、该地址与网段注册过的账号、最近拦截 |
| POST | `/release` | 解除：`{ip, scope: ip|subnet, counters, penalty, resetLevel, ledger, note}` |
| GET | `/penalties` | 正在惩罚期的地址 / 网段 |
| GET | `/events` | 拦截记录：`kind`、`reason`、`ruleId`、`method`、`ip`（地址或网段）、`keyword`、`from`、`to` |
| GET | `/registrations` | 注册台账：`ip`、`method`、`released`、`keyword`、`from`、`to` |
| GET / POST | `/lists` | 名单列表（`keyword` 是地址时列出包含它的网段）/ 批量添加（多行粘贴，最多 500 项，已存在覆盖） |
| PUT | `/lists/:id` | 修改一项 |
| POST | `/lists/delete` | 批量删除 `{ids}` |

## 数据

- `register_ip_ledger`：注册台账，每个新账号一行（地址、方式、属地、设备）。`(appid, ip)` 部分索引走范围扫描
- `register_ip_lists`：名单，`(appid, cidr)` 唯一；命中次数 10 秒批量落库
- `register_ip_events`：拦截记录（rejected / challenged / challenge_passed / observed / penalized / released），保留 180 天
- 过期超过 7 天的名单项与旧记录由 API 进程每 6 小时清理

## 测试

- 判定：`go test ./internal/domain/regip/`
- 端到端（真实 Postgres + miniredis，迁移跑两遍验证可重复执行）：
  `AEGIS_TEST_PG_DSN=postgres://…/空库 go test ./internal/service -run TestRegisterIPIntegration`
  （测试会清空 schema，必须指向专用的空库）
