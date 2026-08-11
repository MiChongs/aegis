# internal/bootstrap — 应用启动与生命周期

> 面包屑：[Aegis](../../CLAUDE.md) › internal/bootstrap

## 职责

负责依赖注入与应用生命周期管理，是整个后端的"装配车间"。

## 关键文件

| 文件 | 说明 |
|---|---|
| `app.go` | `APIApp` — API 服务装配与关闭 |
| `worker.go` | `WorkerApp` — Worker 装配、NATS 订阅、AutoSign 调度循环 |
| `server.go` | `UnifiedApp` — 同时运行 API + Worker（cmd/server 使用） |
| `egress.go` | 出海网关进程级单例（Unified 模式下 API 与 Worker 共用同一张路由表） |
| `commands.go` | CLI 命令实现：`RunMigrations` / `RunSyncLegacyUser` / `RunSyncLegacyBatch` / `RunExportOpenAPI` / `RunExportPostman` |
| `banner.go` | 启动横幅的业务侧组装（渲染引擎在 [pkg/banner](../../pkg/banner)） |

## 启动横幅

打印分两次，因为「让人立刻看到进程活了」和「把事实交代清楚」是两件事：

| 时机 | 函数 | 内容 |
|---|---|---|
| 依赖装配**之前** | `PrintBootBanner(cfg, role)` | FIGlet 艺术字 + 角色/环境/版本，零 I/O |
| 装配完成、开始服务**之前** | `PrintReadyBanner(ctx, rt)` | 摘要行 + 七个分区的明细表 + 页脚 |

`BannerRuntime` 由 `(*APIApp).BannerRuntimeOf` / `(*WorkerApp).BannerRuntimeOf` 组装，
字段全部允许为 nil —— 某个组件没装上就少一行，横幅不该成为进程起不来的理由。

分区随角色裁剪：Worker 不对外提供 HTTP，就不打「入口」与 CORS / 受信代理，
否则等于指引人去访问一个不存在的端口。

**横幅里不允许出现任何密钥**。DSN 一律交给对应驱动自己的解析器
（`pgconn.ParseConfig` / `mysql.ParseDSN` / `url.Parse`）只取 host/port/db/user，
`banner_test.go` 里的 `TestReadyBannerNeverLeaksSecrets` 钉住这条。

对应配置见 `BANNER_*`（[internal/config](../config/CLAUDE.md)）。

## 依赖装配顺序（APIApp）

```
Config → Logger → Tracing → Postgres → Redis → NATS → Temporal
→ Repositories (pg, sessions, realtime)
→ Services (app, auth, admin, user, signin, points, realtime,
            notification, site, version, roleApp, email, payment,
            workflow, storage, avatar, monitor, location, system)
→ Firewall Middleware
→ EnsureBootstrapSuperAdmin
→ SystemService.Initialize
→ Router (gin.Engine)
→ http.Server
```

## 关闭顺序（逆序释放）

Server → Realtime → Location → Redis → NATS → Temporal → Postgres → Tracing → Logger.Sync

## Worker 事件队列

| NATS Subject | 队列名 | 处理函数 |
|---|---|---|
| `auth.login.audit.requested` | aegis-worker-auth-login-audit | `HandleAuthLoginAudit` |
| `auth.session.audit.requested` | aegis-worker-session-audit | `HandleSessionAudit` |
| `user.my.accessed` | aegis-worker-user-my-accessed | `HandleUserMyAccessed` |
| `user.profile.cache.refresh.requested` | aegis-worker-user-profile-cache | 仅日志 |
| `user.signin.completed` | aegis-worker-user-signed-in | `HandleUserSignedIn` |
| `user.autosign.sync.requested` | aegis-worker-auto-sign-sync | `AutoSign.SyncUserSchedule` |
| `firewall.blocked` | aegis-worker-firewall-blocked | `FirewallLogs.HandleFirewallBlocked` |
| `auth.login.audit.requested` | aegis-worker-geo-risk | `GeoRisk.HandleLoginEvent`（与登录审计独立 Queue Group） |

## 地理分析循环（runGeoAnalyticsLoop）

- 启动即跑一轮（确保分区存在 + 补齐聚合缺口）
- `GEORISK_ROLLUP_INTERVAL`（默认 10m）：滚动重算最近 3 个小时桶到 `geo_stats_hourly`
- 每 24h：分区滚动（建下月 / DROP 过期）+ 用户地理画像基线重算

## AutoSign 调度循环

- `TickInterval`（默认 1m）：`AutoSign.RunDue` 执行到期签到
- `RebuildInterval`（默认 15m）：`AutoSign.RebuildSchedule` 全量重建调度表

## 出海代理网关

`ensureEgressGateway` 保证进程内只有一份网关：Unified 模式把 API 与 Worker 跑在同一进程，
各建一份会导致两套健康探测、两份统计、以及「控制台改了配置但 Worker 还用旧路由」。
先创建的一方持有 `OwnsEgress`，负责 `Start`（健康探测循环）与 `Close`（释放 SSH 长连接）。

## 注意事项

- 任何新服务接入必须同时修改 `app.go`（APIApp）和 `worker.go`（如需）以及 `server.go`（UnifiedApp）
- 关闭逻辑必须对应添加，防止资源泄漏
- `RunExportOpenAPI` / `RunExportPostman` 以 nil 服务初始化路由（不连接数据库），仅用于路由结构扫描
