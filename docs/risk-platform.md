# 风控平台（Risk Platform）

取代 `internal/service/risk_*.go` 那一套进程内风控中心。风控从主服务里拆出来，成为一组独立微服务，
主服务只保留一个很薄的接入层（`RiskService.Guard` / `riskHook` 的签名不变，业务调用点不用改）。

## 组件分工

| 组件 | 在风控里干什么 | 为什么是它 |
|---|---|---|
| **CloudWeGo Kitex** | 决策服务的 RPC（主服务 → `risk-decision`、`risk-admin` → `risk-decision`） | 长连接多路复用 + 客户端负载均衡，决策是每个登录 / 下单都要走的热路径 |
| **CloudWeGo Hertz** | `risk-admin` 的 HTTP 管理接口（控制台经主服务反向代理过来） | 与 Kitex 同一套治理（注册发现、链路追踪） |
| **etcd** | ① Kitex / Hertz 服务注册与发现 ② 配置发布通道：发布版本写到 `/aegis/risk/release/…`，决策实例 watch 热加载 ③ 单例任务的选主（名单过期清理）④ Milvus 元数据 | 强一致 + watch，发布一次全部实例秒级生效 |
| **Redis Cluster** | 名单精确值索引、实时滑窗计数（决策服务自己维护）、RisingWave 写回的流式特征、图 / 向量特征缓存、请求幂等 | 决策热路径上唯一同步依赖的存储，分片水平扩展 |
| **RisingWave** | 流式特征：从 NATS JetStream 读事件，按控制台配置生成的物化视图做长窗口聚合（24h 支付金额、7d 设备数、网段注册数……），用 Redis Sink 写回 Redis Cluster | 写 SQL 就是写特征，窗口与去重由流引擎负责，不在决策路径上算 |
| **Flink CEP** | 行为序列模式：「失败 N 次后成功」「注册后 10 分钟内换 5 台设备登录」「改密后立刻支付」等，命中后发告警事件，由 `risk-ingest` 执行处置 | 跨事件、有时序、有 within 的模式只有 CEP 好写 |
| **NebulaGraph** | 关系图：用户 / 设备 / IP / 网段 / 手机 / 邮箱 / 邀请 / 支付账号。决策时取图特征（同设备账号数、二度黑邻居数、邀请树规模），用户详情页画完整关系图 | 多跳关联查询 |
| **Milvus** | 设备指纹与行为序列的向量：近似检索「和这台设备几乎一样的设备」（改机农场、模拟器批量），相似设备里有多少已封禁 | ANN 检索 |
| **ClickHouse** | 决策流水（含完整轨迹）、业务事件、CEP 告警、处置记录；总览大盘、决策检索、用户风险时间线 | 列存分析，亿级流水秒级聚合 |
| **expr-lang/expr** | 规则条件与派生特征表达式（编译一次、零反射求值） | 已在项目里用，表达式语法与旧规则兼容 |
| **gorules/zen-go** | 决策流（JDM 决策表 / 决策图）：分数段 × 模型分 × 命中 → 处置。控制台用 `@gorules/jdm-editor` 可视化编辑 | 业务同学能看懂的决策表，有官方可视化编辑器 |
| **open-policy-agent/opa** | 最终裁决策略（Rego）：白名单兜底、会员保护、验证码可用性、哪些场景允许自动封禁……以库的形式嵌在决策服务里 | 护栏与合规规则需要可测试、可审计的策略语言 |
| **dmitryikh/leaves** | 树模型推理（LightGBM / XGBoost），模型文件在控制台上传、存 MinIO，按特征映射喂入 | 纯 Go 推理，无 CGO、微秒级 |

PostgreSQL（独立库 `aegis_risk`）是配置与名单的权威存储；NATS JetStream（与主服务共用）是全部事件的总线。

## 服务

```
            ┌───────────── 控制台 ─────────────┐
            │  /api/admin/risk/*               │
            ▼                                  │
     aegis-server ──反向代理（签名身份头）──► risk-admin (Hertz :8891)
       │  Guard() ── Kitex ──► risk-decision (:8890) ◄── Kitex ── risk-admin（模拟 / 回放）
       │                         │  │  │  │
       │                         │  │  │  └─ OPA / zen / expr / leaves（进程内）
       │                         │  │  └──── Redis Cluster（名单、滑窗、流式特征、缓存）
       │                         │  └─────── NebulaGraph（图特征）  Milvus（向量特征）
       │                         └────────── etcd（发布版本 watch、注册）
       │
       ├─ 业务事件 risk.event.*  ─┐
       │                         ▼
       │                  NATS JetStream (RISK 流) ──► RisingWave ──Redis Sink──► Redis Cluster
       │                         │  ▲                └► Flink CEP ──► risk.cep.alert.*
       │                         │  └── risk.decision.*（决策服务发布）
       │                         ▼
       │                  risk-ingest ──► ClickHouse / NebulaGraph / Milvus / 名单落库
       │
       └◄─ risk.command.*（RISK_CMD 流：封号、防火墙封禁、踢会话）── risk-decision / risk-ingest
```

| 服务 | 入口 | 职责 |
|---|---|---|
| `risk-decision` | `services/risk/cmd/risk-decision` | 在线决策：`Evaluate` / `Simulate` / `Explain`。无状态，可水平扩容 |
| `risk-admin` | `services/risk/cmd/risk-admin` | 管理接口：场景、特征、规则、决策流、策略、模型、名单、事件模式、发布、决策检索、复核、关系图；`migrate` 子命令初始化全部存储 |
| `risk-ingest` | `services/risk/cmd/risk-ingest` | 事件落地：ClickHouse 批量写、图与向量更新、名单命令持久化、CEP 告警处置 |
| `risk-cep` | `services/risk/cep`（Java / Maven） | Flink 作业，模式由 `risk-admin` 下发 |

所有 Go 服务与主服务在同一个 Go 模块里（同 `services/listentogether`），各自只编译自己的入口。

## 多场景

**场景**是一等配置：键（`login`、`register`、`payment`……或自定义）、作用应用、入参字段（类型化）、超时预算、
失败策略（放行 / 拒绝）、是否接得了验证码、默认处置。主服务内置的 12 个接入点各对应一个内置场景；
自定义场景可由远程函数等调用方直接走 Kitex 接口评估。

每个场景的决策管线固定为 8 步，每一步的**内容**在控制台可视化配置：

1. **装配**：入参归一（IP → 网段 / 国家 / ASN，邮箱 → 域名，手机号 → 号段）
2. **名单**：精确值查 Redis Cluster，网段 / ASN / 国家 / UA 关键词 / 邮箱域名 / 号段 走内存快照
3. **特征**（并行、各有预算，超时记「降级」不阻塞）：实时滑窗计数、流式特征、图特征、向量特征
4. **派生特征**：expr 表达式
5. **规则**：expr 条件 → 命中、维度分、强制处置；观察模式只留痕
6. **模型**：leaves 推理 → 概率分
7. **决策流**：zen JDM（决策表 / 决策图），没配时按分数段
8. **策略**：OPA Rego 裁决最终处置与要执行的命令

之后：处置命令（名单立即写 Redis、持久化与封号走 `risk.command.*`），决策事件同步发 JetStream（NATS 不可用时
落本地 bbolt 待发队列，恢复后补发，`Nats-Msg-Id` 去重）。

## 配置与发布

配置在 `aegis_risk` 库里编辑（草稿），**发布**把一个场景用到的全部东西编成一个不可变的发布包（规则、特征定义、
决策流、策略、模型引用、小名单版本），写进 `releases` 表并推到 etcd `/aegis/risk/release/scenes/<scene>`。
决策实例 watch 前缀、编译通过才切换（编译失败保留旧版本并上报）。支持：

- **灰度**：候选版本按主体哈希取百分比
- **影子**：候选版本只评估、不生效，结论写 ClickHouse 与正式版本对比
- **回滚**：重新发布任一历史版本

## 事件契约

JetStream 流 `RISK`（subjects `risk.event.>`、`risk.decision.>`、`risk.cep.alert.>`，按时间保留 7 天）与
`RISK_CMD`（`risk.command.>`，工作队列语义）。事件是扁平 JSON（RisingWave 直接解析）：

```json
{"id":"01J…","type":"login.success","scene":"login","ts":1731050000000,"app_id":10000,"user_id":42,
 "account":"alice","ip":"1.2.3.4","ip_subnet":"1.2.3.0/24","device_id":"d-…","ua":"…","country":"CN",
 "asn":4134,"phone_prefix":"1380013","email_domain":"qq.com","amount":0,"target_user_id":0,
 "action":"","score":0,"props":{}}
```

## 主服务接入

- `RiskService.Guard`：Kitex 调 `risk-decision`，场景超时预算内没回来按场景的失败策略处理（默认放行并记日志）。
  拦截 → 403/40398（业务可指定），验证码 → 40015，与旧实现一致，SDK 无需改动。
- 业务结果事件：`RiskService.Report` 发 `risk.event.*`（登录成功 / 失败、注册、支付、邀请、签到……）。
- 命令消费：`risk.command.account_ban` / `account_unban` / `firewall_ban` / `revoke_sessions`，按命令 ID 幂等执行。
- 管理接口：`/api/admin/risk/*` 反向代理到 `risk-admin`（经 etcd 发现实例），读接口要 `risk:read`、写接口要
  `risk:write`，操作者身份用内部令牌 HMAC 签进请求头。

## 部署

`deploy/docker/docker-compose.yml` 的 `risk` profile：etcd ×3、Redis Cluster ×6（+ 初始化）、MinIO、Milvus、
NebulaGraph（metad / storaged / graphd）、ClickHouse、RisingWave、Flink（jobmanager 以 application 模式直接带作业 + taskmanager）、
`risk-migrate`（一次性初始化）、`risk-decision`、`risk-admin`、`risk-ingest`。Go 服务镜像 `deploy/docker/risk.Dockerfile`
（zen-go 需要 CGO，基于 Debian），Flink 作业镜像 `deploy/docker/risk-cep.Dockerfile`（Maven 构建）。

```bash
docker compose --env-file .env -f deploy/docker/docker-compose.yml --profile app --profile risk up -d
```
