# 流量管理

流量管理是 Aegis 的内置插件 `traffic`，取代旧的「突发流量爬坡」。它在请求链路上对每个请求做分类，再按规则依次执行速率限制、并发控制、排队削峰和预热爬坡；规则之外还有一道进程级的过载保护。

| 位置 | 内容 |
|---|---|
| `pkg/traffic/` | 引擎：配置结构、编译、判定、指标、模拟。只依赖 net/http，不依赖 Aegis 内部包 |
| `pkg/traffic/redisco/` | Redis 协调器：集群配额租约、实例心跳、配置广播 |
| `internal/nativeplugin/` | 内置插件宿主：登记、启停、重启、多实例启停对账 |
| `internal/plugins/traffic/` | 插件本体：生命周期、Gin 中间件、配置发布与同步、落库、规则模板 |
| `internal/repository/postgres/traffic_repository.go` | 持久化 |
| `migrations/postgres/000148_traffic_management.up.sql` | 表结构 |
| `aegis-console/src/app/(console)/traffic/` | 控制台 |

## 请求链路

```
ClientIP → … → AccessLog → Firewall → 流量管理 → ReplayGuard → AppGateway → …
```

- 排在 Firewall 之后：先由防火墙拒掉恶意流量，剩下的合法流量再整形。
- 排在 ReplayGuard 之前：被拒的请求不消耗 Nonce 与幂等键占位。
- 中间件常驻在链上。插件停用时直接放行，所以启停不需要重建路由。
- 恒免：`/healthz`、`/readyz`、CORS 预检、WebSocket 升级请求。管理端 `/api/admin/*` 默认免检（`exemptAdmin`），因为洪峰期间正需要打开控制台。

拒绝时返回规则（或全局）配置的响应，默认是 `429`、业务码 `42910`，并带 `Retry-After` 头。

## 规则

规则表有序，排在前面的先匹配。所有命中的规则依次生效，任一环节拒绝，请求即被拒绝。`terminal` 规则命中后不再匹配后面的规则。

**匹配条件**：各字段之间是「且」，字段内的多个值是「或」，空字段不限制。

| 字段 | 写法 |
|---|---|
| `methods` | `GET`、`POST`…… |
| `paths` / `excludePaths` | 精确 `/api/auth/login/password`；前缀 `/api/auth/*`（也包含 `/api/auth` 本身）；单段通配 `/api/v1/apps/*/auth/login` |
| `hosts` | 精确或 `*.example.com` |
| `appKeys` | 按 `X-Aegis-App-Key` 请求头或 `appKey` 查询参数 |
| `cidrs` / `excludeCidrs` | 来源网段 |
| `headers` | `exists` / `absent` / `equals` / `prefix` / `contains`（不区分大小写） |

**模式**

- `enforce`：超限即拒绝。
- `observe`：只统计「本应拒绝」，不拦截。上线新规则前先观察命中量。
- `bypass`：命中后直接放行，并停止匹配后续规则。

**档位** `critical` / `high` / `normal` / `low`：请求的档位取第一条命中规则的档位，过载保护按档位从低到高丢弃。

**控制手段**，按以下顺序执行：

1. **预热爬坡**（`warmup`）：准入上限从 `baselineRps` 起步。上一秒的需求达到上限的 90% 时，每个周期抬高「基线 × `stepPct`%」，直到 `maxRps`。需求持续低于基线 `cooldownSeconds` 秒后，按同样步长回落。四个状态是 稳态 → 爬坡 → 饱和 → 回落。
2. **速率限制**（`limits`，每条规则最多 8 项）：每项限制在每 `periodMs` 内最多放行 `rate` 次，允许 `burst` 的瞬时突发。限速键有 `global`、`ip`、`subnet`（IPv4 /24、IPv6 /64）、`app`、`token`（Authorization）、`header`、`query`。取不到键值时，`fallback` 决定改按 IP 计（`ip`）还是不限制（`skip`）。
3. **并发**（`concurrency`）：在途请求数上限。开启 `adaptive` 后，上限在 `[minLimit, maxLimit]` 内按 AIMD 自动调节：平均响应时间超过基线的 `latencyToleranceX10/10` 倍时乘 0.9，否则在需求触顶时加 √上限。
4. **排队**（`queue`）：被限速或并发已满的请求最多等待 `timeoutMs`，最多 `size` 个同时等待，排不下或等不到就拒绝。限速排队是精确预约：GCRA 算出可放行的时刻，请求睡到那一刻。如果算出的等待超过超时，立即拒绝，不白等。

## 算法与热路径

- **GCRA**：每个键只存一个「理论到达时间」，一次判定就是一次 CAS，不需要后台补令牌。
- **按键存储**：512 个分片，每片一把读写锁。键先用 maphash（进程随机种子，防哈希洪水）哈希成 uint64，表里不存键字符串。容量（`maxTrackedKeys`，默认 200 万）满了以后，新键落进本片的溢出桶共用限速，内存有硬上界。后台每 10 秒清掉早已回满的键。
- **计数**：每项计数分 16 条带，随机挑一条累加，避免多核争同一条缓存行。秒级曲线不在热路径上维护，由每秒一次的采样循环对累计值做差分得到。
- **延迟直方图**：24 格，按 2 的幂分档（50µs × 2ⁱ），格内线性插值求分位。
- **热点键**：只在拒绝路径上按 1/8 抽样，用 Space-Saving 算法保留前 32 个，每分钟计数减半。
- **配置**：编译成不可变快照，用原子指针替换。规则的运行态（桶、计数、预热状态、自适应上限）按规则 ID 保留，重载时不清零。

实测数据：i7-14650HX，24 线程，`go test -bench . ./pkg/traffic/`。

| 场景 | 结果 |
|---|---|
| 完整判定：4 条规则、按 IP 限速、200 万个不同来源 IP、24 线程并发 | 233 ns/次，0 次分配，单实例约 430 万次判定/秒 |
| 单个 GCRA 桶，24 线程争用 | 42 ns/次 |
| 200 万个键的内存 | 约 92 MB（每键约 46 字节） |

`TestAdmitZeroAlloc` 守着热路径零分配。

## 过载保护

过载保护是进程级的，与规则无关，思路同 Kratos 与 Sentinel 的 BBR：

```
承载量 = 每秒最大完成数 × 最小平均响应时间（BDP）
丢弃   = CPU ≥ 阈值  且  在途 > 承载量
```

- CPU 是进程 CPU 时间除以「墙钟时间 × GOMAXPROCS」，每 250ms 采样一次，按 EWMA 平滑。Go 1.25 起，GOMAXPROCS 会遵循容器的 CPU 配额。
- 触发后在冷静期内，只要在途仍超过承载量就继续丢弃，避免在阈值附近反复开关。
- `low` 档的 CPU 阈值低 10 个点，承载量按 0.8 倍算；`high` 档阈值高 5 个点，承载量按 1.2 倍算；`critical` 永不因过载被丢。
- `maxInflight` 是全局在途硬上限，用来防止慢请求把内存堆满。

## 集群

开启 `settings.cluster.enabled` 后，作用域为 `cluster` 的限速才跨实例协同。Redis 不在请求热路径上。

- **全局键**：采用租约方式。每个实例从 Redis 租一小批名额（约「总额 ÷ 实例数 ÷ 8」），在本地用原子扣减；余量低于一半时异步续租，一次 Redis 往返覆盖一批请求。计数语义是固定窗口。连续 3 次续租失败后，降级为「总额 ÷ 实例数」的本地 GCRA，每秒探测一次 Redis，恢复后自动切回。
- **按键**：每个实例按「总额 ÷ 存活实例数」限速。负载均衡把同一个键的请求打散到各实例时，正好拼回集群总额；同一个键固定落在某个实例时会更严。
- **心跳**：每个实例每 2 秒把自己的摘要写入 `{prefix}traffic:node:{id}`（TTL 10 秒），并登记到有序集合，存活实例数由此得出。实例数变化时，所有均分的桶自动重算。
- **配置同步**：发布新修订后，在 `{prefix}traffic:reload` 上广播修订号，其他实例收到后从数据库重载。每个实例另外每 10 秒对账一次修订号，作为广播丢失时的兜底。
- **插件启停**：插件宿主每 10 秒对账 `native_plugins` 表，在任一实例上启停插件，都会传到其他实例。

## 持久化

| 表 | 内容 |
|---|---|
| `native_plugins` | 内置插件的启停 |
| `traffic_settings` | 全局设置（单行），以及当前修订号 |
| `traffic_rules` | 规则，`position` 是匹配顺序 |
| `traffic_revisions` | 每次发布的完整快照 |
| `traffic_metrics_minute` | 每个实例每分钟的聚合（`rule_id` 为空串表示整体），保留 30 天 |
| `traffic_events` | 预热开始、饱和、回落，过载保护触发与解除，拒绝突增，自适应并发收缩；保留 30 天 |

所有写操作都经过 `TrafficPublish`：锁住 `traffic_settings` 行，读出完整配置，修改后整份写回，并记一条修订。传入 `expectedRevision` 时，修订号不一致返回 `40990`。回滚就是把旧修订的内容发布成一个新修订。

历史曲线先按分钟把各实例相加，再按步长合并（1h 按 1 分钟，6h 按 5 分钟，24h 按 15 分钟，7d 按 1 小时）。分位取各实例的最大值，偏保守。

## 管理端接口

均挂在 `/api/admin/system/` 下，仅超级管理员可用（权限点 `system:admin:manage`）。

| 方法与路径 | 用途 |
|---|---|
| `GET traffic/overview?seconds=` | 本实例运行态、各实例摘要、集群合计 |
| `GET traffic/config` | 当前配置 |
| `GET traffic/templates` | 规则模板 |
| `PUT traffic/settings` | 替换全局设置 |
| `POST traffic/rules` | 新建规则（`position: top \| bottom`） |
| `PUT traffic/rules/:ruleId` | 修改规则 |
| `DELETE traffic/rules/:ruleId?expectedRevision=` | 删除规则 |
| `POST traffic/rules/:ruleId/toggle` | 启用或停用规则 |
| `POST traffic/rules/reorder` | 调整顺序 |
| `GET traffic/rules/:ruleId/stats?seconds=` | 单条规则的运行态与秒级曲线 |
| `POST traffic/validate` | 校验规则草稿 |
| `POST traffic/simulate` | 模拟一个请求，不消耗配额，可带草稿规则 |
| `GET traffic/revisions`、`GET traffic/revisions/:revision` | 修订列表与快照 |
| `POST traffic/rollback` | 回滚 |
| `GET traffic/history?range=&ruleId=` | 历史曲线 |
| `GET traffic/events?limit=&kind=` | 事件，`kind` 支持前缀，如 `warmup.` |
| `POST traffic/reset-stats` | 清零本实例统计 |
| `GET native-plugins` | 内置插件列表 |
| `POST native-plugins/:pluginId/enable \| disable \| restart` | 启停与重启 |

错误码：`40990` 修订冲突，`40091` 规则无效，`40490` 规则不存在，`40491` 修订不存在，`40492` 插件不存在，`40993` 插件未启用无法重启，`50390` 插件未加载。

## 从「突发流量爬坡」迁移

插件首次启动时，如果 `traffic_settings` 为空，就初始化配置。

- 平台设置里有旧的 `security.traffic_ramp` 时，会转换成一条「全站突发爬坡」规则：路径 `/*`，预热、排队、并发参数照搬，启用状态与旧开关一致。旧的免检前缀 `p` 转成 `p/*`，`Retry-After` 秒数也照搬。
- 环境变量 `TRAFFIC_RAMP_*` 已不再读取。
- 旧接口 `/api/admin/system/traffic-ramp*` 已删除。

## 容量与扩展

单实例的瓶颈在 HTTP 协议栈和业务处理，不在流量管理：按上面的实测，判定只占每个请求约 0.2 µs 的 CPU。要承载 200 万并发用户，做法是水平扩展：

1. 多副本部署，开启 `settings.cluster.enabled`。需要集群总额的限速（登录、短信、应用配额）把作用域设为 `cluster`。
2. `maxTrackedKeys` 按单实例要跟踪的不同键数设置。每个按键限速约 46 字节/键，200 万个键约 92 MB。
3. 用 `overload.maxInflight` 给每个实例的在途请求设硬上限，再用 `critical` 档保护支付回调这类不能丢的请求。
4. 新规则先用 `observe` 模式上线，看「本应拒绝」的量，再切成 `enforce`。

## 测试

- `go test -race ./pkg/traffic/`：路径匹配、GCRA、按键隔离、免检、观察与放行模式、排队、并发与等待队列（含丢失唤醒的回归）、预热状态机、重载保留状态、过载硬上限、请求头与应用匹配、模拟不消耗配额、分位、清扫与容量、热路径零分配。
- `go test -bench . ./pkg/traffic/`：吞吐基准。
- `AEGIS_TEST_PG_DSN=… AEGIS_TEST_REDIS_ADDR=… go test ./internal/plugins/traffic/ -run TestTrafficIntegration`：真实 Postgres 与 Redis 上的全链路测试，覆盖旧配置导入、增改删、乐观锁、中间件限速、插件启停、模拟、分钟聚合与历史、事件、回滚、集群租约与心跳。它会清空 `traffic_*` 表，必须指向专用测试库。
