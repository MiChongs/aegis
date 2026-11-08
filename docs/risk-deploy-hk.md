# 风控平台上线方案（HK Dokploy）

> 2026-11-08 起草。阶段 A 的代码改动与编排见 `deploy/dokploy/risk-stack.yml`，分支 `risk-platform`。

## 1. 现状

| 项 | 实测 |
|---|---|
| 机器 | 8 vCPU、31 GiB 内存（已用约 6 GiB，可用约 24 GiB）、无 swap；系统盘 117 GB，剩 61 GB |
| 现有基础设施 | Dokploy compose 项目 `aegis-infra-e2mufe`：PostgreSQL 17（库 `zeabur_db`）、Redis Stack、NATS 2.14.4、Temporal；都挂 `dokploy-network`，别名 `aegis-postgres` / `aegis-redis` / `aegis-nats` / `aegis-temporal`，数据绑定在 `/srv/aegis/*` |
| NATS | 账户 `APP`（用户 zeabur，带密码）；JetStream 全局上限 `max_file 8G`、`max_mem 1G`，现用 34 MB |
| 旧风控的量 | 近 7 天日均约 8 千次评估，今天 2.1 万次（抽奖 1.85 万）；名单 0 条；规则 4 条 |
| 旧风控实际拦了什么 | 近 7 天 block 24 次全部是 `login_bruteforce`（登录防爆破，留在主服务里不受影响），review 5 次来自 `geo_login`（地理风控，改为上报事件）。4 条规则里：登录 VPN +20 分单独到不了拦截线，3 条支付规则是观察模式 |

结论：**切换期间不会丢掉任何正在生效的拦截**，可以按「先观察、后生效」平滑上线。

## 2. 上线前要补的代码

1. **RisingWave 的 NATS 源带鉴权**：生产 NATS 要用户名密码，`stream.SQL` 现在写死 `connect_mode = 'plain'`。改为从 `RISK_NATS_URL` 解出用户密码，用 `connect_mode = 'user_and_password'`。Go 服务与 Flink（jnats）都直接支持 URL 里带凭据，不用改。
2. **RISK 流的容量可配**：现在 `MaxBytes` 写死 20 GB，超过服务器 8 GB 上限会建流失败。改成 `RISK_STREAM_MAX_BYTES`，生产给 3 GB（按每次决策事件约 5 KB、日 2 万次算，7 天约 0.7 GB，留 4 倍余量）。
3. **Nebula 默认密码**：graphd 首次启动是 `root / nebula`。`risk-admin migrate` 在 `RISK_NEBULA_PASSWORD` 不是默认值时先用默认密码登录、执行 `CHANGE PASSWORD root FROM 'nebula' TO ...`，并等 graphd 从 metad 同步账号（实测约 8 秒）后用新密码重连。
4. **生产编排** `deploy/dokploy/risk-stack.yml`（新文件，不动开发用的 compose）：
   - 复用现有 Postgres（建库 `aegis_risk`）与 NATS（同一个账户），不另起；
   - 单机部署：etcd 1 节点、Redis Cluster 3 主 0 从（单机上副本不带来任何高可用，只多吃内存），其余各 1；
   - 全部挂 `dokploy-network`、别名 `risk-*`，不映射任何宿主端口；数据绑定到 `/srv/aegis/risk/<组件>`；
   - 每个容器设内存上限（见下表），ClickHouse 设 `max_server_memory_usage`，RisingWave 设 `--total-memory-bytes`；
   - `risk-decision` 两个固定实例（`risk-decision-1/2`）：各用自己的本地暂存目录，bbolt 文件锁不能两个进程共用。

## 3. 资源预算

| 组件 | 上限 | 预计常驻 |
|---|---|---|
| RisingWave 单节点 | 9 GiB（总内存 8 GiB：单节点给 compactor 1/8，6 GiB 时只有 768 MiB，启动断言失败） | 4–6 GiB |
| Flink jobmanager + taskmanager | 1.5 + 2.5 GiB | 2.5 GiB |
| Milvus standalone | 3 GiB | 1.5–2 GiB |
| ClickHouse | 3 GiB | 0.8–1.5 GiB |
| NebulaGraph（meta / storage / graph） | 0.5 + 1 + 0.5 GiB | 0.8 GiB |
| Redis Cluster 3 主 | 3 × 384 MiB | 0.3 GiB |
| etcd、MinIO | 256 + 512 MiB | 0.3 GiB |
| risk-decision × 2、risk-admin、risk-ingest | 4 × 512 MiB | 0.6 GiB |
| **合计** | **约 23 GiB** | **约 11–14 GiB** |

现有负载约 6 GiB，上线后常驻约 17–20 GiB，峰值上限加起来接近 27 GiB。**建议先加 8 GiB swapfile 兜底**（只防偶发峰值把内核逼到 OOM，不指望它承载常驻），或者把机器升到 64 GiB。

磁盘：镜像约 10 GB；ClickHouse 流水按日 2 万次、每行压缩后约 2 KB 算，180 天约 7 GB；JetStream 上限 3 GB。合计约 20 GB，剩余空间够。

## 4. 步骤

**阶段 A：只建风控平台，主服务不动（线上零影响）**

1. 补第 2 节的代码，推到 GitHub **新分支** `risk-platform`（不推 `main`：推 `main` 会让 aegis-api 与控制台自动部署，主服务就先于风控平台上线了）。
2. 服务器上建目录 `/srv/aegis/risk/{etcd,redis-1,redis-2,redis-3,minio,milvus,nebula-meta,nebula-storage,clickhouse,risingwave,flink,spool}`，按各镜像的用户设属主（milvus、clickhouse 101、flink 9999）。
3. 生成密钥：`RISK_INTERNAL_TOKEN`、Redis、ClickHouse、MinIO、Nebula 各一个随机强密码；`RISK_PG_DSN` / `RISK_PG_ADMIN_DSN` 用现有 Postgres 账号；`RISK_NATS_URL` 用现有 NATS 凭据。写进 Dokploy 环境变量，并同步到本机备份 `~/Documents/zeabur-migration/env/aegis/aegis-risk.env`（chmod 600）。
4. Dokploy 新建 compose 项目 `aegis-risk`：GitHub 仓库、分支 `risk-platform`、compose 路径 `deploy/dokploy/risk-stack.yml`。部署：服务器上构建 `aegis-risk` 与 `aegis-risk-cep` 两个镜像（约 3 分钟），`risk-migrate` 一次性初始化。
5. 验收（都在服务器上经容器网络做）：
   - `risk-admin /v1/status` 9 个组件全 ok；
   - `risk-admin migrate` 日志：建库、内置场景、旧名单导入（0 条）；
   - `risk-admin eval` 打一条登录评估、`emit` 一条登录失败，ClickHouse 里能查到。
6. 配置第一批策略，**全部用观察模式**：12 个内置场景装「基础防护」，登录 / 找回密码 / 管理员登录装「账号安全」，注册装「注册反作弊」，营销类装「营销反作弊」，支付装「支付风控」，验证码装「短信轰炸」；逐个发布（全量）。观察模式命中只留痕，处置一律放行。

**阶段 B：主服务切过来**

7. aegis-api 加环境变量：`RISK_ETCD_ENDPOINTS=risk-etcd:2379`、`RISK_INTERNAL_TOKEN=<同上>`、`RISK_TIMEOUT=300ms`。三处一致：Dokploy、运行中的服务、本机备份。
8. `risk-platform` 合进 `main` 并推送：aegis-api 与 aegis-console 自动部署。aegis-api 的 watchPaths 上次没触发，需要时用部署 webhook 补。
9. 验收：
   - 线上登录、抽奖等请求的决策出现在 ClickHouse，P99 延迟在 300 ms 预算内；
   - 业务事件在流（登录成功 / 失败、抽奖、签到）；
   - 控制台各页签有数据；
   - 主服务日志里没有「风控评估失败，按放行处理」。

**阶段 C：观察到生效（约 3 天）**

10. 每天在控制台看「决策流水」与总览：观察规则各命中多少、误伤样本。阈值不合适的改完再发。
11. 确认后把规则逐场景改为生效（先营销类与验证码，再登录与注册），发布可以先灰度 10% 再转正。
12. 打开行为模式：「连续失败后登录成功」「注册后频繁换设备」，处置先只建复核单。

**阶段 D（另行决定）**：观察一个月没问题后，再加删除旧 `risk_*` 表的迁移（不可逆，需要你确认）。

## 5. 回滚

| 情况 | 做法 | 影响 |
|---|---|---|
| 风控平台某个组件挂了 | 不用动：Guard 超时 / 出错自动放行，图、向量、流式特征缺了只按默认值算 | 风控变弱，业务不受影响 |
| 风控平台整体要撤 | aegis-api 去掉 `RISK_ETCD_ENDPOINTS` 重新部署 | Guard 一律放行，与当前旧规则的实际效果几乎一样 |
| 新主服务本身有问题 | Dokploy 回滚到 f4e0cbf 那次部署 | 旧风控中心原样恢复（旧表没删、数据没动） |

## 6. 风险与限制

- 单机：所有组件和主服务在同一台机器上，这台机器出问题风控也一起没了（主服务本来也一样）。
- RisingWave 免费许可证 4 核；Flink 作业改模式时会停作业做保存点再起，期间约 10 秒不出告警（事件不丢，起来后补算）。
- 关系图与相似设备要攒一段时间的数据才有意义；上线头几天图特征基本是 0，所以第一批策略用观察模式。
