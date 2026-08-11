# internal/domain — 领域类型定义

> 面包屑：[Aegis](../../CLAUDE.md) › internal/domain

## 职责

纯类型层（Go struct），不含任何业务逻辑或数据库操作。Service 和 Repository 均依赖此层，避免循环依赖。

## 子包一览

| 包 | 核心类型 |
|---|---|
| `admin` | `Account`, `Profile`, `Session`, `AccessContext`, `LoginResult`, `Assignment`, `AssignmentMutation` |
| `app` | App 相关类型（多租户应用元数据） |
| `auth` | JWT 声明、OAuth2 Token、用户认证上下文 |
| `email` | 邮件渠道配置（`SMTPConfig` / `ZeaburConfig`）、投递记录 `Delivery`、webhook 事件 |
| `organization` | **组织架构**：`Organization`/`Department`/`Member`/`Position`/`Role`/`ApprovalChain`。对外一律 UUID（`ID` 带 `json:"-"`，`UUID` 序列化成 `id`）；内置角色与权限目录也在这里，`PermissionCatalog()` 经 `/org-metadata` 驱动控制台的权限勾选树 |
| `oauth` | 应用级第三方登录渠道配置、模板目录、绑定记录 |
| `notification` | 两套收件箱：`types.go` 应用用户站内信；`admin_inbox.go` 管理员收件箱（主键空间不同，刻意分开以防串消息） |
| `notify` | 统一通知出口：渠道 / 订阅 / 模板 / 投递记录 + 渠道类型目录（`ChannelKinds`）与事件目录（`KnownEvents`） |
| `ticket` | 工单：`Ticket`/`Message`/`Event`/`Category`/`Group`/`SLAPolicy`，以及可见范围 `Scope` 与动作集 `ActionSet` |
| `platform` | 平台治理：`Governance`/`Restrictions`/`ActionRecord`/`Appeal` + 六档状态与七项能力常量。状态与限制项的**预设**（`PresetRestrictions`）也在这里，后端判定与控制台展示读同一份 |
| `payment` | 支付订单、支付方式 |
| `points` | 积分交易、积分账户 |
| `realtime` | WebSocket 实时消息结构 |
| `storage` | 存储桶、文件元数据 |
| `system` | `settings.go` — 平台设置（防火墙规则、CORS、全局配置） |
| `user` | 用户基本信息、用户设置、等级信息 |
| `workflow` | Temporal 工作流定义、节点类型 |

## 使用约定

- 类型文件统一命名为 `types.go`（`system` 包除外，使用 `settings.go`）
- 所有字段使用 camelCase JSON tag，与前端 API 约定一致
- 不允许在 domain 层 import service 或 repository 包
- 时间类型统一使用 `time.Time`，指针表示可选字段（`*time.Time`）

## admin 包核心类型示例

```go
type Account struct {
    ID          int64      // 管理员 ID
    Account     string     // 登录账号
    IsSuperAdmin bool      // 超级管理员标识
    Status      string     // active / disabled
}

type AccessContext struct {
    Session               // 嵌入会话信息
    Assignments []Assignment  // Casbin 角色分配
}
```
