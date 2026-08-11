# internal/config — 配置加载

> 面包屑：[Aegis](../../CLAUDE.md) › internal/config

## 职责

从 `.env` 文件 + 环境变量加载强类型配置结构，全局单例，启动时注入所有组件。

## 入口

```go
cfg, err := config.Load()  // 唯一入口，返回 Config 值类型
```

## Config 结构速览

```
Config
├── AppName / AppEnv / HTTPPort
├── AdminAPIToken / AdminSessionTTL / AdminBootstrap
├── ReadTimeout / WriteTimeout / ShutdownTimeout
├── CORS       → CORSConfig
├── JWT        → JWTConfig (Secret, Issuer, TTL, RefreshTTL)
├── Firewall   → FirewallConfig (WAF、限流、CIDR 黑白名单、UA 过滤)
├── Postgres   → PostgresConfig (DSN, 连接池, 连接寿命/空闲回收, 会话级超时)
├── Database   → DatabaseConfig (采集/慢查询/泄漏阈值/清道夫/排空)
├── LegacyMySQL → LegacyMySQLConfig (迁移用)
├── Redis      → RedisConfig (Addr, Password, DB, KeyPrefix)
├── Egress     → egress.Config (出海代理端点 + 域名后缀规则 + 健康检查)
├── GeoIP      → GeoIPConfig (mmdb 路径、自动更新)
├── NATS       → NATSConfig (URL, StreamName)
├── Temporal   → TemporalConfig (HostPort, Namespace, TaskQueue, 超时)
├── AutoSign   → AutoSignConfig (定时签到参数)
├── Banner     → BannerConfig (启动横幅：开关/详略/字体/着色/宽度/主机采集)
└── OAuth      → map[string]OAuthProviderConfig (qq/wechat/github/google/microsoft/weibo)
```

## 关键规则

- **必填项**：`JWT_SECRET`、`POSTGRES_DSN`、`REDIS_ADDR`、`NATS_URL`，缺失直接返回 error
- **防火墙默认值**：通过 `NormalizeFirewallConfig` 统一处理，调用者可单独复用
- **OAuth 默认端点**：内置 qq/wechat/github/google/microsoft/weibo 端点，仅需配置 ClientID/Secret
- **环境变量优先**：Viper `AutomaticEnv` 已开启，环境变量覆盖 `.env` 文件
- **出海网关**：`egress.go` 支持三种写法（紧凑 DSL / 内联 JSON / 配置文件），解析或校验失败直接返回 error —— 静默忽略一条写错的规则会让境外调用变成难以归因的超时

## 默认值摘要

| 配置项 | 默认值 |
|---|---|
| HTTP_PORT | 8088 |
| ADMIN_SESSION_TTL | 12h |
| JWT_TTL | 720h (30天) |
| JWT_REFRESH_TTL | 168h (7天) |
| POSTGRES_MAX_CONNS | 10 |
| REDIS_KEY_PREFIX | "aegis" |
| NATS_STREAM_NAME | "AEGIS_EVENTS" |
| TEMPORAL_TASK_QUEUE | "aegis-workflow" |
| GEOIP_DATABASE_DIR | ".runtime/geoip" |
| AUTO_SIGN_TICK_INTERVAL | 1m |
| AUTO_SIGN_REBUILD_INTERVAL | 15m |
| BANNER_STYLE | auto（跟随终端能力） |
| BANNER_FONT | slant |

## 启动横幅（BANNER_*）

`DefaultBannerConfig()` 除了给 `setDefaults` 用，还专门服务一个场景：
`config.Load` 失败时调用方拿到的是零值 `Config`，其中 `Enabled=false` 会让横幅整个消失——
而恰恰是那种时候最需要横幅先证明进程活着。三个入口都会在拿到错误时用它兜底。
