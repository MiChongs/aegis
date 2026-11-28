# 账号注销

应用用户在 App 内注销自己的账号，支持**立即注销**与**冷静期注销**两种方式，可选**人工审核**。
管理员在控制台「用户运营 → 账号注销」配置规则、审核与处理申请、解除重新注册限制，
也可以在用户详情里代用户发起注销。

| 位置 | 内容 |
|---|---|
| `internal/domain/accountdeletion` | 配置、状态机、前置条件与验证方式的纯函数判定 |
| `internal/service/account_deletion_service.go` | 用户端：注销页、验证码、提交、撤销、登录与注册回调、执行 |
| `internal/service/account_deletion_admin.go` | 管理端操作、统计、后台循环 |
| `internal/repository/postgres/account_deletion_repository.go` | 配置 / 申请 / 墓碑 |
| `migrations/postgres/000101_account_deletion.up.sql` | 三张表 |

## 申请的生命周期

```
用户提交 ─┬─ 需要审核 ─→ reviewing ─┬─ 批准 ─┬─ 冷静期 → scheduled
          │                         │        └─ 立即   → processing
          │                         └─ 驳回 → rejected
          ├─ 冷静期 ──→ scheduled ──┬─ 到期（后台）/ 管理员立即执行 → processing
          │                         └─ 本人撤销 / 冷静期内登录（按配置）/ 管理员撤销 → cancelled
          └─ 立即 ────→ processing ─┬─ 成功 → completed
                                    └─ 失败 → failed（管理员重试或撤销；本人也可撤销）
```

- 一个账号同时只有一条进行中的申请（reviewing / scheduled / processing / failed，部分唯一索引兜底）。
- 冷静期从提交时起算；需要审核时从**批准**时起算。
- 状态迁移都是条件更新（`WHERE status IN (...)`），用户撤销与后台到点执行并发时只有一个生效。
- 后台循环（API 进程，每分钟）：先发到期提醒（`remind_at`），再认领到期的 scheduled 执行。
  多实例靠 `FOR UPDATE SKIP LOCKED` 认领。启动时把超过 30 分钟仍是 processing 的申请改成 failed，交给人看。

## 执行做了什么

1. 向在线设备推 `account.deleted`，然后吊销全部访问会话、刷新令牌与第三方（OAuth2）授权。
2. 清空用户云存储（失败只记日志）。
3. `UserService.PurgeUser`（即 `PurgeUsers` 删一个）：删 `users` 行（TOTP、恢复码等关联表 CASCADE；订单、退款、审计日志 `SET NULL` 留存），
   清缓存与搜索索引，触发插件钩子 `user.onDeleted`。管理员「删除用户」也走这一条。
   每个从 `users` 级联下去的外键列都有以它开头的索引（迁移 `000179_fk_index_*`，`TestPurgeUsersIntegration` 守门），
   级联删除按索引查子表；新加引用 `users` 的外键时要一起建索引，否则测试会失败。
4. `reregisterDays > 0` 时写墓碑：账号名、邮箱、手机号的 HMAC-SHA256 摘要（密钥为平台主密钥）+ 打码提示，
   期满前三种注册（密码、短信、第三方）都会被 40918 拦下。
5. 申请记为 completed，给绑定邮箱发最后一封告知信。

申请行不挂外键：账号删除之后它仍在，记录谁、何时、为何注销以及谁批准的。

## 配置（`account_deletion_settings.config`）

| 字段 | 默认 | 说明 |
|---|---|---|
| `allowImmediate` / `allowCooling` | false / true | 至少开一种 |
| `defaultMode` | cooling | 只开一种时自动跟随 |
| `coolingDays` | 15 | 1–180 |
| `requireReview` | false | 提交后先待审核 |
| `verifyMethods` | password, email, phone | 任选其一；账号没绑或应用发不出验证码的方式不列出 |
| `requireTotp` | true | 开了两步验证的账号再过一次动态码或恢复码 |
| `recentLoginMinutes` | 10 | 一种验证方式都没有时，会话签发不超过 N 分钟即可放行；0 为不放行 |
| `reasons` / `reasonRequired` / `allowCustomReason` | 6 项 / false / true | 原因选项；`code` 为 `[a-z0-9_-]{1,64}` |
| `noticeTitle` / `noticeContent` / `agreementLabel` | 默认须知 | 正文按纯文本分段展示 |
| `supportContact` | 空 | 页面底部的客服联系方式 |
| `guards.*` | 见下 | 每项 off / warn / block |
| `loginDuringCooling` | keep | keep 登录不影响；cancel 冷静期内登录即撤销 |
| `signOutOthers` | true | 进入冷静期或待审核时退出当前设备以外的登录 |
| `remindBeforeHours` | 24 | 到期前提醒；0 不提醒 |
| `reregisterDays` | 0 | 注销后 N 天内不能用同一账号名、邮箱、手机号注册 |
| `notifyEmail` | true | 提交、撤销、驳回、提醒、完成时发信 |

前置条件（`guards`，默认值）：`activeBan` 封禁中（block）、`pendingOrders` 48 小时内待支付订单（block）、
`pendingRefunds` 处理中的退款（block）、`walletBalance` 钱包有余额（warn）、`activeVip` 会员未到期（warn）、
`lifetimeVip` 永久会员（warn）、`points` 有积分（off）。block 挡提交，warn 只在页面上提示。

## 身份验证

`PlanVerification` 给出 `verification`：`methods` 任选其一；`totp` 为真时再加动态码；`session` 为真表示凭最近登录放行；
`available` 为假时 `message` 说明要先绑定什么。

- 邮箱码由注销服务生成，只存 HMAC 摘要（不写公开 `/auth/email/verify` 用的 key），10 分钟有效、错 5 次作废。
- 短信码走 CaptchaService（purpose `verify_identity`），用应用的短信模板。
- 同一渠道 60 秒一次、每天 10 次；任意验证连续失败 8 次锁定 1 小时（42912）。
- 刚换绑 72 小时内的邮箱或手机号不算可用的验证方式（与找回密码同一个冷静期键）。

## 用户端接口（`/api/v1/apps/{appKey}`，均需登录）

| 接口 | 说明 |
|---|---|
| `GET /me/deletion` | 注销页：`enabled`、`modes`、`defaultMode`、`coolingDays`、`requireReview`、`notice`、`reasons`、`reasonRequired`、`allowCustomReason`、`verification`、`checks`、`blocked`、`reregisterDays`、`loginCancels`、`supportContact`、`request` |
| `POST /me/deletion/code` | `{channel: email\|phone}` → `{channel, masked, expiresAt, resendAt}` |
| `POST /me/deletion` | `{mode, reasonCode, reasonText, agree, password \| channel+code, totpCode \| recoveryCode, deviceId}` → `{request, deleted, signedOut}` |
| `POST /me/deletion/cancel` | 撤销，返回撤销后的 `request` |

`enabled=false` 且没有 `request` 时入口隐藏。`request.status` 为 reviewing / scheduled / failed 时页面展示进度与「撤销」；
rejected 在驳回后 30 天内展示 `reviewNote`，不影响重新提交。`deleted=true` 时客户端清掉本地登录。

登录结果多一个 `accountDeletion`（`status`、`mode`、`scheduledAt`、`cancelled`）：冷静期或待审核中的账号登录时出现，
`cancelled=true` 表示这次登录按配置撤销了冷静期。实时事件：`account.deletion`（状态变化）、`account.deleted`（即将删除）。

错误码：40371 未开放、40127 验证未通过、40128 验证码失效、40129 缺少验证、40448 没有申请、40915 已有申请、
40916 状态已变化、40917 前置条件不满足、40918 不能重新注册、42911 发码过频、42912 失败过多、50071 执行失败。

## 管理端接口（`/api/admin/apps/{appkey}`，用户读写权限点）

| 接口 | 说明 |
|---|---|
| `GET account-deletion/catalog` | 枚举、默认配置、全部取值与字数上限（`limits`） |
| `GET/PUT account-deletion/settings` | `{enabled, config}` |
| `GET account-deletion/stats?days=&tz=` | 不限时间：`total`、`open`（待审核 / 冷静期 / 执行中 / 失败）、7 天内到期、墓碑数、挽留率；最近 `days` 天：`windowTotal`、按状态 / 方式 / 来源 / 原因（管理员发起且没选原因的归 `@admin`）、趋势（按 `tz` 分日） |
| `GET account-deletion/requests` | `status, mode, source, userId, keyword, from, to, page, limit` |
| `GET account-deletion/requests/:id` | 详情（含提交时的账号快照） |
| `POST …/:id/approve` | `{note}` |
| `POST …/:id/reject` | `{note}` 必填，用户可见 |
| `POST …/:id/cancel` | `{note, notify}` |
| `POST …/:id/execute` | `{confirm}` 填账号名；scheduled / failed |
| `POST …/:id/reschedule` | `{scheduledAt, notify}`，晚于现在、不晚于一年 |
| `GET account-deletion/tombstones` / `DELETE …/:tombstoneId` | 重新注册限制与解除 |
| `GET users/:userId/account-deletion` | 进行中的申请与历史 |
| `POST users/:userId/account-deletion` | `{mode, coolingDays, reasonCode, reason, notify, confirm}`；`reasonCode`（配置里的原因）与 `reason` 至少给一个；不过验证与前置条件，立即注销须 `confirm` 账号名 |

## 验证

```
AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55491/postgres go test ./internal/service -run TestAccountDeletionIntegration
go test ./internal/domain/accountdeletion/
```
