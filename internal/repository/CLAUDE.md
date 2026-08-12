# internal/repository — 数据访问层

> 面包屑：[Aegis](../../CLAUDE.md) › internal/repository

## 职责

封装所有数据库读写操作，向 Service 层暴露接口，禁止被 transport/http 直接调用。

## 子包结构

```
repository/
├── postgres/          # PostgreSQL 主数据库
│   ├── repository.go            # Queries 结构体（根聚合，pgx/v5）
│   ├── admin_repository.go      # 管理员账号 CRUD、Casbin 策略
│   ├── app_content_repository.go # 应用级 Banner 与公告：展示端/管理端两套过滤、排序事务、总览聚合
│   ├── app_oauth_repository.go  # 应用级第三方登录渠道 & oauth_bindings 绑定记录
│   ├── email_repository.go      # 邮件渠道配置 + 投递留痕（状态推进判定写在 SQL 里）
│   ├── org_repository.go        # 组织 CRUD / 层级 / 概览统计 / 活动留痕 / 应用绑定
│   ├── org_department_repository.go # 部门：物化路径 + 闭包表双写、环检测、三种删除策略
│   ├── org_member_repository.go # 组织成员 + 部门成员（岗位/汇报线/代理）+ 汇报环检测
│   ├── org_position_repository.go # 岗位 + 组织邀请
│   ├── org_role_repository.go   # 组织自定义角色与授权（可限定部门子树）
│   ├── org_access_repository.go # 审批链 / 实例 / 权限模板 / 协作组
│   ├── payment_repository.go    # 支付订单
│   ├── payment_refund_repository.go  # 退款单：额度预占/释放、结算、履约冲正（均为单事务）
│   ├── platform_governance_repository.go # 平台治理：状态 + 流水（同事务）+ 申诉 + 全站总览聚合
│   ├── platform_settings_repository.go  # 平台设置 K/V 存储
│   ├── role_repository.go       # 角色申请
│   ├── site_repository.go       # 站点信息
│   ├── storage_repository.go    # 存储桶 & 文件元数据
│   ├── version_repository.go    # App 版本 & 渠道
│   ├── workflow_repository.go   # 工作流定义
│   └── workflow_scan.go         # 工作流结果扫描辅助
├── redis/             # Redis 缓存与会话
│   ├── session_repository.go        # 用户会话 Token（JWT 黑名单 & 活跃会话）
│   ├── admin_session_repository.go  # 管理员会话
│   ├── realtime_repository.go       # 实时连接状态
│   └── auto_sign_repository.go      # 自动签到调度（Sorted Set）
└── legacymysql/       # 遗留 MySQL（仅数据迁移用）
    └── repository.go
```

## 初始化方式

```go
// PostgreSQL
pg := pgrepo.New(pgxpool)         // 返回 *Queries

// Redis
sessions := redisrepo.NewSessionRepository(redisClient, keyPrefix)
adminSessions := redisrepo.NewAdminSessionRepository(redisClient, keyPrefix)
realtimeRepo := redisrepo.NewRealtimeRepository(redisClient, keyPrefix)
schedules := redisrepo.NewAutoSignRepository(redisClient, keyPrefix)

// LegacyMySQL
legacy := legacyrepo.New(db)
```

## 约定

- PostgreSQL 查询使用 pgx/v5 原生 SQL（非 ORM），所有 SQL 写在各 `*_repository.go` 中
- Redis key 统一由 `keyPrefix` 参数前缀化，格式为 `{prefix}:{domain}:{id}`
- AutoSign 使用 Redis Sorted Set，score 为 Unix 时间戳（下次签到时间）
- LegacyMySQL 仅在 `cmd/server sync-legacy-*` 命令中使用，正常运行时不连接

## 扩展说明

添加新的 Repository 方法：
1. 在对应 `*_repository.go` 文件中添加方法到 `Queries` 结构体
2. 在 Service 层通过 `pg.*Method()` 调用
3. 无需额外注册，`Queries` 已在 bootstrap 统一传递
