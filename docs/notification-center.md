# 用户消息中心

> 面包屑：[Aegis](../CLAUDE.md) › 用户消息中心
>
> 状态：已实现（2026-10-19，后端与控制台）。Voyage App 按本文对接。与最初契约的偏离见文末「实现说明」。
> 2026-11-20：管理员群发重构为**群发任务**（一键全站、人群分类、定时、取消、撤回，后台分批写入），见「群发任务」；
> 缓存改为每人一个哈希键（失效不再 SCAN 全库）。

应用用户的站内信：分类、跳转动作、完整的已读/未读/删除接口、按分类的推送偏好，
以及**实时推送**——新消息、已读、删除都经 `/api/ws` 推到该用户所有在线设备，多端状态一致。

旧接口（`/notifications`、`unread-count`、`read`、`read-batch`、`read-all`、`clear`、`DELETE /:id`）
路径与请求形状不变，响应只**加字段**，老客户端不受影响。旧实时事件 `notification.state.changed` 继续发。

## 分类（单一事实源：`internal/domain/notification/category.go`）

| key | 名称 | 归入的 `type` | 推送可关 |
|---|---|---|---|
| `system` | 系统通知 | `system` 与其余一切未登记的 type | 是 |
| `announcement` | 公告活动 | `announcement`、`campaign`、`marketing`，管理员群发默认 | 是 |
| `security` | 账号安全 | `security`、`geo_login`、`password_reset`、`two_factor`、`passkey` | **否** |
| `membership` | 会员与订单 | `vip`、`payment`、`order`、`wallet`、`refund`、`account_deletion` | 是 |
| `reward` | 积分与奖励 | `signin`、`auto_sign`、`points_store`、`invite`、`leaderboard`、`lottery`、`rewarded_ad`、`cardkey` | 是 |
| `ticket` | 工单与反馈 | `ticket`、`feedback` | 是 |

- 列 `notifications.category`：写入时由 `notification.CategoryOf(type)` 推出（大小写与首尾空白不敏感），
  调用方也可显式指定（必须是目录里的 key，否则 42231）。
- 分类顺序即客户端展示顺序；summary 与偏好接口都按这个顺序列出全部分类。
- 新增写入点时先在映射表登记 type：`TestNotificationWriterTypesAreCategorized` 扫描 `internal/` 全部源码里
  `DeliverInput{Type: ...}`、`SendUserNotification`、`NotifyUser`、`notifySecurityEvent` 的 type 字面量，
  没登记的 type 直接让测试失败（非字面量的透传参数要在测试里登记）。

### 各写入点

| 写入点 | type | 分类 | 动作 |
|---|---|---|---|
| 两步验证开启 / 关闭、Passkey 添加 / 移除（`security_service.go`） | `security` | security | route `security`「查看账号安全」 |
| 密码已重置（`auth_password_recovery.go`） | `security` | security | route `security`「查看账号安全」 |
| 异常登录提醒（`geo_risk_service.go`，Worker） | `security` | security | route `security`「查看账号安全」 |
| 账号注销各状态（`account_deletion_service.go`） | `account_deletion` | membership | route `security`「查看注销进度」 |
| 签到成功（`worker_event_service.go`，Worker） | `signin` | reward | route `signin`「查看签到」 |
| 自动签到成功 / 失败 / 暂停（`auto_sign_service.go`） | `auto_sign` | reward | route `signin`「查看签到」 |
| 自动签到测试通知（`/api/user-settings/auto-sign/test-notification`） | `auto_sign` | reward | — |
| 积分商城人工发放结果（`points_store_service.go`） | `points_store` | reward | route `points_store`「查看兑换记录」 |
| 邀请奖励、阶梯奖励（`invite_service.go`） | `invite` | reward | route `invite`「查看邀请」 |
| 排行榜奖励到账（`leaderboard_service.go`） | `leaderboard` | reward | route `leaderboard`「查看排行榜」 |
| 工单 / 反馈事件（NotifyHub 站内信渠道） | 渠道配置的 type，默认事件的 `ticket` | ticket | route `tickets`「查看工单」 |
| 远程函数 `aegis.notification.send` | 脚本传入，默认 `system` | 由 type 推出 | — |
| 管理员群发 | 管理员填写（控制台固定 `system`） | 管理员选择，默认 announcement | 管理员选择 |

会员与订单类目前没有服务端写入点（支付、会员开通不发站内信），分类先留着给管理员群发与后续写入点用。

## 消息形状（`notification.Item`）

```json
{
  "id": 1, "appid": 1, "userId": 2,
  "type": "leaderboard", "category": "reward", "categoryLabel": "积分与奖励",
  "title": "排行榜奖励已到账", "content": "你在 2026 年第 43 周积分周榜获得第 3 名。",
  "level": "info",
  "status": "unread", "read": false, "readAt": null,
  "action": { "type": "route", "target": "leaderboard", "label": "查看排行榜" },
  "image": null,
  "expiresAt": null,
  "metadata": {}, "createdAt": "...", "updatedAt": "..."
}
```

- `readAt`、`action`、`image`、`expiresAt` **恒在**，没有值时为 `null`（客户端按「字段恒在、可空」声明即可）；
  `metadata` 为空对象时省略。
- `status` 只有 `unread` / `read` 两个值（回执表里的 `delivered` 对外一律叫 `unread`），`read` 是它的布尔版。
- `level`：`info` | `success` | `warning` | `error`。写入时旧写法 `critical` / `danger` 归为 `error`、`warn` 归为 `warning`，其它值 40010。
- `action.type`：`route`（App 内页面，`target` 取下表的键，客户端不认识的键不显示按钮）或 `url`（`target` 为 https 地址，
  ≤ 2048 字符，不带账号密码与空白）。`label` 可空（≤ 16 字），空时客户端用默认文案。
- 路由键：`points`（积分中心）、`points_store`（积分商城）、`leaderboard`（排行榜）、`signin`（签到）、
  `membership`（会员）、`wallet`（钱包）、`security`（账号安全）、`invite`（邀请）、`tickets`（工单）、`profile`（个人资料）。
- `image`：https 地址（≤ 2048 字符），不合法 40010。
- `expiresAt`：必须晚于现在且不超过一年，否则 40010。
- 标题 ≤ 200 字、type ≤ 64 字符；正文为空时用标题兜底。

## 单一投递入口

`NotificationService.Deliver(ctx, notification.DeliverInput{AppID, UserID, Type, Category, Title, Content, Level,
Metadata, Action, Image, ExpiresAt}) (*notification.Item, error)`：校验 → 平台治理闸门 → 落库（消息 + 回执同一事务，返回完整一行）
→ 失效缓存 → 异步发实时事件 → 插件钩子 `notification.onCreated`（payload 加了 `id`、`category`）。

- `NotifyUser`、`SendUserNotification`、NotifyHub 站内信渠道都走它；其它服务只依赖接口
  `notificationDeliverer`（`Deliver` 一个方法），测试里可以换成假实现。
- `TestOnlyNotificationServiceCreatesUserNotifications` 扫描 `internal/` 全部非测试源码：除 `notification_service.go`
  外不得调用 `CreateUserNotification`。
- 管理员群发是第二条（也是唯一另一条）写入路径：群发任务一条语句写一批人（见「群发任务」），同样做校验、治理闸门、
  缓存失效与实时事件。`TestOnlyCampaignRunnerWritesCampaignBatches` 钉住只有 `notification_campaign.go` 调批量写入。
- 单条落库是**一条语句**（CTE 同时写消息与回执并带回完整一行），不再开事务做四次往返。
- 单条投递的 created 事件只发给在线的人（`RealtimeService.FilterOnlineUsers`）：不在线的不再为他查计数与偏好。
- Worker 进程的 `NotificationService` 接的是 NATS 发布器（`NewNATSUserEventPublisher`），Worker 发出的签到、异常登录、
  自动签到、工单站内信同样经 API 实例实时推到设备。
- 签到提醒在 Worker 里投递失败只记日志、不让 NATS 消息重投（签到审计已经落库，重投会重复）。

## 用户端接口

网关 `/api/v1/apps/:appkey/notifications*`（需登录）与 `/api/notifications*`（JWT）挂同一组 handler。
响应外壳同其它接口（`{code, message, data}`），下文只写 `data`。

| 方法 路径 | 请求 | data |
|---|---|---|
| `GET /notifications` | query：`status=all\|unread\|read`、`category`、`page`、`limit`(≤100)；旧的 `type`、`level` 仍可用 | `{items, page, limit, total, totalPages, unread, categoryUnread}` |
| `GET /notifications/summary` | — | `{unread, categories:[{key, label, unread, total, latest: Item\|null}]}`（全部分类都列出，没有消息的 `total=0`、`latest=null`） |
| `GET /notifications/unread-count` | — | `{unread, categories: {<key>: n}}`（只含 n > 0 的分类） |
| `GET /notifications/:notificationId` | — | `Item` |
| `POST /notifications/read` | `{notificationId}` | `{notificationId, read: true, unread, categoryUnread}` |
| `POST /notifications/unread` | `{notificationId}` | `{notificationId, read: false, unread, categoryUnread}` |
| `POST /notifications/read-batch` | `{ids}`(≤200) | `{appid, userId, requested, updated, ids, unread, categoryUnread}` |
| `POST /notifications/read-all` | `{category?}`（不传或 `all` 即全部；请求体可空） | `{readAll: true, updated, category?, unread, categoryUnread}` |
| `DELETE /notifications/:notificationId` | — | `{appid, userId, deleted, clearedAll: false, unread, categoryUnread}` |
| `POST /notifications/delete-batch` | `{ids}`(≤200) | `{requested, deleted, ids（真正删掉的）, unread, categoryUnread}` |
| `POST /notifications/clear` | `{status?: all\|read\|unread, category?}`（旧的 `type`、`level` 仍可用） | `{appid, userId, deleted, status, category?, clearedAll: true, unread, categoryUnread}` |
| `GET /notifications/preferences` | — | `{categories:[{key, label, push, mutable}]}` |
| `PUT /notifications/preferences` | `{categories: {<key>: bool}}`（只改传了的分类） | 同 GET |

- `categoryUnread` 是 `{<key>: n}`，只含 n > 0 的分类，恒为对象（没有未读时为 `{}`）。
- 单条操作（详情、`read`、`unread`、`DELETE /:id`）遇到不存在、不属于本人或已过期的消息一律 **40455**；
  批量操作（`read-batch`、`delete-batch`）只处理本人未过期的那些，其余静默跳过（看 `updated` / `deleted`）。
- `ids` 去重并去掉非正数；空 40010，去重后超过 200 条 40010。
- 推送偏好表 `notification_preferences(appid, user_id, category, push, updated_at)`，没有行即开启。
  关闭只影响**提醒**（实时事件里 `silent = true`，客户端不弹横幅、不发系统通知），消息照常入库、照常计未读。
  PUT 里关闭 `mutable=false` 的分类 → 40347；把它设为 `true` 视为无操作；未知分类 → 42231；空对象 → 40010。
- 过期消息（`expires_at <= now()`）一律不出现、不计数；读写接口对它们按「不存在」处理。清空（`clear`）时顺带删掉
  命中条件的已过期消息，但不计入返回的 `deleted`。
- 列表的 `status` 只接受 `all` / `unread` / `read`（其它值 40010），`category` 未知 42231。

## 实时事件（`/api/ws`，信封同现有 `realtime.Event`：`{id, type, appid, userId, timestamp, data}`）

| type | data |
|---|---|
| `notification.created` | `{notification: Item, silent: bool, unread, categoryUnread}` |
| `notification.read` | `{ids: [...], all: bool, category?: string, read: true\|false, unread, categoryUnread}` |
| `notification.deleted` | `{ids: [...], cleared: bool, status?: string, category?: string, unread, categoryUnread}` |
| `notification.state.changed` | 旧事件照发：`{reason, refreshRequired: true, stateChangedAt, unread}` |

- 每个新事件后面紧跟一条 `notification.state.changed`（`reason`：`created`、`mark_read`、`mark_unread`、`mark_read_batch`、
  `mark_all_read`、`delete`、`delete_batch`、`clear`、`admin_delete`、`admin_delete_by_filter`）。
- `read`：单条标已读 / 未读时 `ids` 是那一条；`read-batch` 时是**真正从未读变成已读**的那些；`read-all` 时 `ids=[]`、`all=true`
  （按分类时带 `category`）。没有任何变化的批量操作不发事件。
- `deleted`：单条与批量删除时 `ids` 是真正删掉的；`clear` 时 `ids=[]`、`cleared=true`，带 `status` 与（按分类时）`category`；
  管理员在控制台删除消息时，被删消息的主人也会收到 `deleted`（`ids` 为他的那几条）。
- `unread` / `categoryUnread` 是事件发出时的计数；极少数情况下计数查询失败，事件里不带这两个字段，客户端保留原值即可。
- 事件发给**该用户的全部连接**（含触发操作的那台设备，客户端按 id 幂等处理）。多实例经 NATS 已有通路。
- 发布是异步的、不阻塞请求；同时在发的事件任务最多 16 个（`notificationEventConcurrency`），群发上千人时不会占满数据库连接。
- 客户端重连后自行拉 `summary` 与第一页补齐，不做服务端补发。

## 缓存

- 每个用户一个哈希键 `<prefix>:notification:cache:<appid>:<userId>`：字段 `counts` 是分类未读计数，
  字段 `list:<status|category|type|level>:<页>:<条数>` 是一页列表。换分类不会读到别的分类。
- 失效只要 `DEL` 这一个键（连同总未读 `notification:unread:*` 与「我的」页聚合）；一批人一次管道往返
  （`SessionRepository.InvalidateNotificationCaches`）。旧实现按前缀 `SCAN` 整个 Redis，群发十万人就是十万次全库扫描。
- 字段各有寿命：默认 60 秒，且不超过最早一条（未读 / 筛选范围内）消息的过期时间。哈希只能整键过期，所以每个值包一层
  `{e: 到期毫秒, v: 值}`，读到过期字段当没有并顺手删掉；整键过期时间固定 60 秒（`NotificationCacheMaxTTL`），每次写入顺延。
- 任何写操作（含 Deliver 与群发）先失效缓存；写操作返回的计数直接查库，不回填缓存。
- 上线切换：旧键 `notification:list:*` 不再读写，60 秒内自然过期；滚动发布期间旧版本实例写的旧键不会被新实例失效，
  最多旧 60 秒。

## 群发任务

管理员群发不在请求里逐人投递：发起即落一个任务（`notification_campaigns`，迁移 000158），立刻返回；任意实例认领后
按用户 ID 升序分批写入。代码在 `internal/service/notification_campaign.go` 与
`internal/repository/postgres/notification_campaign_repository.go`，人群与任务的形状在 `internal/domain/notification/campaign.go`。

### 接口（`/api/admin/apps/:appkey/notifications/campaigns`）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/campaigns/catalog` | 查看 | 表单目录：`presets`（预设人群）、`platforms`、`memberships`、`accounts`、`categories`、`statuses`、`limits` |
| POST | `/campaigns/preview` | 查看 | `{audience}` → `{audience（归一后）, label, total, samples[≤10]}`；只数人不写数据 |
| GET | `/campaigns?status=&page=&limit=` | 查看 | 任务列表（新的在前，limit ≤ 100） |
| POST | `/campaigns` | 发送与删除 | 发起，返回 `{campaign, created}`；同一 `requestId` 重复提交返回第一次的任务、`created=false` |
| GET | `/campaigns/:campaignId` | 查看 | 详情，现算 `readCount`（已读人数） |
| POST | `/campaigns/:campaignId/cancel` | 发送与删除 | 取消定时 / 排队 / 发送中的任务；发送中的写完当前一批就停，**已发出的保留** |
| POST | `/campaigns/:campaignId/recall` | 发送与删除 | 撤回：后台按批把这个任务发出的消息从所有人的消息中心删掉（推 `notification.deleted`）；还没开始的直接取消 |

发起的请求体：

```jsonc
{
  "requestId": "可选，≤64 字符，幂等键",
  "type": "可选，默认 announcement",
  "category": "可选，默认 announcement（公告活动）",
  "title": "必填，≤200 字",
  "content": "可选，空用标题兜底",
  "level": "info | success | warning | error",
  "action": {"type": "route", "target": "points", "label": "去看看"},
  "image": "https://…",
  "expiresAt": "RFC 3339，一年内；定时发送时必须晚于定时时间",
  "metadata": {},
  "scheduledAt": "可选，RFC 3339，90 天内；已到点（或差不到一分钟）即立刻排队",
  "audience": { /* 见下 */ }
}
```

内容与 Deliver 同一套校验（42231 / 42230 / 40010）；立刻发送且人群为空时报 40401（定时任务不预先判空）。

### 人群 `audience`

| 字段 | 说明 |
|---|---|
| `scope` | `all` 全站（一键全站）/ `users` 指定用户 / `filter` 条件筛选。空时：给了 `userIds` 即 `users`，否则 `all` |
| `userIds` | `scope=users` 必填，≤ 10000（更多请用条件筛选） |
| `excludeUserIds` | 排除名单，任何范围都生效，≤ 10000 |
| `account` | `available`（默认：已启用、不在禁用期、没有生效中的封禁，与排行榜资格同一口径）/ `all` / `disabled` |
| `membership` | `member` / `non_member` / `lifetime` / `non_lifetime`，空不限（仅 filter） |
| `registeredWithinDays` / `registeredBeforeDays` | 注册不超过 / 满 N 天（仅 filter，≤ 3650） |
| `activeWithinDays` / `inactiveDays` | 最近 N 天登录或在线过 / 超过 N 天没有（沉默召回）；二者不能同时设（仅 filter） |
| `minLevel` / `maxLevel` | 等级范围（仅 filter，≤ 1000） |
| `platforms` | 用这些平台的设备登录过：`android` `ios` `harmonyos` `windows` `macos` `linux` `web`（按 `device.NormalizePlatform` 归一） |
| `keyword` | 账号、昵称、邮箱、手机号模糊匹配，或等于用户 ID（仅 filter，≤ 64 字） |
| `limit` | 最多发给多少人（按用户 ID 升序取前 N 个），0 不限 |

全站与指定用户只看 `account` 与 `excludeUserIds`，其余条件被清掉；filter 的条件同时满足（AND）。预设人群
（全站、会员、非会员、永久会员、新用户、活跃、沉默、Android、iOS）在 `notification.AudiencePresets()`。

### 任务

状态：`scheduled` 定时 → `queued` 排队 → `running` 发送中 → `succeeded` 完成 / `failed` 失败 / `cancelled` 已取消；
撤回：`recalling` 撤回中 → `recalled` 已撤回。列表项字段：`id, appid, requestId, type, category, categoryLabel, title,
content, level, metadata, action, image, expiresAt, audience, audienceLabel（中文摘要）, status, statusLabel, scheduledAt,
total（开始发送时人群的人数，开始前为 null）, processed（扫过的人）, delivered（写入的条数）, skipped（已有、跳过）,
recalled（撤回删掉的条数）, readCount（只有详情有）, attempts, message（失败 / 取消 / 撤回说明）, operator, startedAt,
finishedAt, recalledAt, heartbeatAt, createdAt, updatedAt`。

### 执行

- **认领**：每个 API 实例一个执行器（`NotificationService.StartCampaigns`，同时最多 2 个任务）；`UPDATE … WHERE id IN
  (SELECT … FOR UPDATE SKIP LOCKED)` 认领排队的、到点的定时任务、心跳超过 2 分钟的发送中 / 撤回中任务。发送被中断 5 次
  判失败（已发出的保留）；撤回不限次数。实例关闭时把手上的任务放回去（不等心跳过期，这次不算次数）。
- **定格人群**：第一次开始发送时记下人群人数（`total`，估计值）与这一刻最大的用户 ID（`max_user_id`）——发送期间新注册的人
  不追加，被接手的任务也发给同一批人。
- **每批**（2000 人）一个事务：锁任务行 → 只有仍是 `running` 且持有者是自己才继续 → 一条语句按游标取目标、写消息、写回执
  （`ON CONFLICT (campaign_id, user_id) DO NOTHING`）→ 推进游标与计数 → 扫到的不足一批或到了人数上限就在同一事务里结束任务。
  进度与写入一起提交，接手的实例从游标继续，不重不漏；重放同一批只会计入 `skipped`。
- **取消 / 撤回**改的是同一行，必须等这一批的行锁：撤回开始之后不会再有新消息写进来。持有者被清空，正在发的实例在下一批的
  检查里停下（本实例上的直接叫停）。
- **提交之后**：这批人的缓存失效与 `notification.created` 推送交给每个任务一个的处理协程，与下一批写入并行；队列最多 4 批，
  满了写入就等着，不在内存里堆积。事件只推**在线**的人（`RealtimeService.FilterOnlineUsers`，每 1000 人一条 `ZMSCORE`），
  未读计数（`NotificationUnreadCountsBatch`）与提醒偏好（`NotificationPushMutedUsers`）各一条语句按批查。实例在这之间崩溃，
  这批人的缓存最多旧 60 秒；事件本来就是尽力而为，客户端重连会拉 summary。
- 发完整个任务发一次插件钩子 `notification.onSent`（payload 带 `campaignId`、`delivered`）。逐人的 `notification.onCreated` 不发。
- 平台治理：发起时与每次开始执行时都过 `CapabilityNotification` 闸门，被限制的应用任务判失败。
- **撤回**每批删 2000 条（回执随外键级联），删完结束为 `recalled`；逐批失效缓存、给在线的收件人推 `notification.deleted`
  （`ids` 是这个人被删的消息，`cleared=false`）。

实测（2026-11-20，Windows 便携版 PG 18 默认配置 + miniredis，10 万用户）：旧的逐人投递约 1500 人/秒（还不含旧缓存失效的
全库 SCAN，且一次最多 2000 人）；群发任务每批 1000 人时约 6800 人/秒（含 miniredis 缓存失效与全部事件）、只算数据库约
9300 人/秒，每批 2500 人时只算数据库约 11500 人/秒（于是取 2000）；撤回 10 万条约 3 秒（不含 miniredis）；
管理端列表首页 17 毫秒（新索引 `idx_notifications_app_time`）。miniredis 是进程内模拟，比真实 Redis 慢得多，线上缓存失效的占比更小。

### 旧群发接口

`POST /api/admin/apps/:appkey/notifications/bulk` 请求与响应形状不变，内部也建一个群发任务：

- 选人口径照旧：给了 `userIds` 就发这些人（不论账号状态）；否则按 `keyword` / `enabled` 筛，人数上限 `limit` 默认 200、最多 2000
  （顺序由「最新注册在前」改为用户 ID 升序）。`enabled` 不给即不论账号状态，`true` 即可用账号，`false` 即禁用账号。
- 人数不超过 2000：以请求为持有者就地发完，返回 `delivered` 与 `recipientIds`（真正收到的人），`async=false`。请求中途断了，
  任务心跳过期后由执行器接着发完。
- 更多（只会出现在 `userIds` 很长时）：转后台任务，立刻返回 `async=true`、`delivered=0`、`campaign`（看进度）。
- 响应新增 `async`、`campaign` 两个字段。

## 管理端

- 列表 `GET /api/admin/apps/:appkey/notifications`、导出、按条件删除的筛选：`keyword`、`category`、`type`、`level`，
  新增 `status`（`read` / `unread`）、`userId`、`campaignId`；列表项新增 `campaignId`（单条投递为 null）。
  列表项有 `category`、`categoryLabel`、`action`、`image`、`expiresAt`，`status` 的 `delivered` 叫 `unread`；
  CSV 导出有 `category`、`expires_at` 两列。管理端列表**包含**已过期的消息。
- 列表的计数只连筛选用得到的表（不按关键词不连用户与资料表，不按已读不连回执），翻页走 `(appid, created_at DESC, id DESC)`
  索引（迁移 000160）。
- 管理员删除（按 ID、按条件）：一批人一次管道失效缓存，在线的人收到 `notification.deleted`。
- 控制台：
  - 用户列表批量操作「发通知」（`components/users/app-users/bulk-notify-dialog.tsx`）：标题、正文、分类（默认公告活动）、级别
    （普通 / 成功 / 提醒 / 重要）、跳转（无 / 应用内页面〔路由键下拉，中文名〕/ 链接）、按钮文案、配图地址、过期时间（1 / 7 / 30 天快捷）。
  - 应用详情审计区块里的「通知」表（`components/apps/app-audit-panel.tsx`）：分类筛选、分页，列出收件人、分类、级别、跳转、
    已读状态、过期时间（已过期标出）。
  - 分类、路由键、级别的中文名在 `lib/user-notification-meta.ts`，与后端目录一一对应。

## 错误码

| 码 | HTTP | 名称 | 场景 |
|---|---|---|---|
| 40455 | 404 | `NOTIFICATION_NOT_FOUND` 消息不存在 | 详情、标已读 / 未读、删除单条时不存在、不属于本人或已过期 |
| 40347 | 403 | `NOTIFICATION_PUSH_LOCKED` 该类消息不能关闭提醒 | 偏好里关闭账号安全类 |
| 42230 | 422 | `NOTIFICATION_ACTION_INVALID` 动作不合法 | 动作类型不是 route / url、路由键不在白名单、链接不是 https、按钮文案超长 |
| 42231 | 422 | `NOTIFICATION_CATEGORY_INVALID` 分类不存在 | 写入、筛选、read-all、clear、偏好里出现未知分类 |
| 40010 | 400 | 参数无效 | 标题为空或超长、级别未知、配图不是 https、过期时间不在未来一年内、ids 为空或超过 200、status 未知 |
| 40000 | 400 | 请求格式错误 | 请求体绑定失败、路径里的 ID 不是数字 |

| 40427 | 404 | 群发任务不存在 | 详情、取消、撤回的任务不存在或不属于这个应用（管理端） |
| 40958 | 409 | 群发任务状态不允许 | 取消已结束的任务、重复撤回 |
| 40401 | 404 | 未找到可发送的目标用户 | 立刻发送的群发人群为空 |

前四个码登记在网关错误目录（`auth_protocol_catalog.go` 的 `gatewayErrors`）；群发的码只在管理端出现。

## 迁移 000108_notification_center

- `notifications` 加列：`category VARCHAR(32) NOT NULL DEFAULT 'system'`、`action JSONB NULL`、`image TEXT NULL`、`expires_at TIMESTAMPTZ NULL`；
  索引 `idx_notifications_app_user_category_time (appid, user_id, category, created_at DESC)`。
- 新表 `notification_preferences`（主键 `(appid, user_id, category)`，随应用与用户级联删除）。
- 回填：`category` 先加成可空列，只回填仍为 `NULL` 的行，再补默认值与 `NOT NULL`——迁移器每次启动重跑也不会覆盖写入时显式指定的分类。
  按上面的映射表回填，另外认出两种历史写法：`metadata.module = 'account_deletion'`（旧的账号注销消息用的是 `security`）→ membership，
  `metadata.scene in ('auto_sign', 'auto_sign_test')`（旧的自动签到消息用的是 `system`）→ reward。

## 迁移 000158–000160 群发任务

- 000158：新表 `notification_campaigns`（`(appid, request_id)` 部分唯一、`(appid, id DESC)`、未结束任务的部分索引），
  `notifications` 加列 `campaign_id BIGINT NULL`（不加外键：任务随应用级联删除，外键只会让每批写入多一次检查）。
- 000159：`uq_notifications_campaign_user (campaign_id, user_id) WHERE campaign_id IS NOT NULL`，`CONCURRENTLY`、单语句文件。
- 000160：`idx_notifications_app_time (appid, created_at DESC, id DESC)`，`CONCURRENTLY`、单语句文件。

## 测试

- `internal/domain/notification/category_test.go`：分类目录与顺序、type 映射、偏好判定、级别归一、动作校验、可空字段输出 null。
- `internal/service/notification_writers_test.go`：只有 NotificationService 调 `CreateUserNotification`；全部写入点的 type 都已登记。
- `internal/service/notify_provider_local_test.go`：工单站内信带 `tickets` 动作。
- `internal/domain/notification/campaign_test.go`：人群归一与校验、摘要、预设人群合法、目录完整。
- `TestNotificationCampaignIntegration`（真实 Postgres + miniredis + 假的实时发布器，2300 个用户）：人群预览各条件的人数、
  一键全站跨批发完一人一条、缓存失效、created 事件、requestId 幂等、中断后接手（旧持有者被拒、不重不漏）、重放只跳过、
  人数上限、定时不提前认领、取消与重复取消、撤回删光并推 deleted、旧接口就地发与转后台、任务列表筛选、关机放回任务。
- `TestNotificationCenterIntegration`（真实 Postgres + miniredis + 假的实时发布器）：回填与重跑、Deliver 校验与完整 Item、
  created 事件内容与 silent、偏好、分类 / 状态过滤与过期排除、缓存键含分类、按分类未读、summary、详情与越权、标已读 / 未读、
  批量已读、按分类全部已读、批量删除、按分类清空、管理员群发与列表。

```bash
# 只能指向一次性的测试库：openTestDatabase 会 DROP SCHEMA public
AEGIS_TEST_PG_DSN=postgres://aegis:<密码>@127.0.0.1:15432/aegis_notification_test?sslmode=disable \
  go test ./internal/service -run TestNotificationCenterIntegration
```

## 实现说明（与最初契约的差异）

- **两个写入点换了 type**：账号注销由 `security` 改为 `account_deletion`（契约把账号注销归入会员与订单，原 type 会落进账号安全），
  自动签到由 `system` 改为 `auto_sign`（契约把它归入积分与奖励）。按 type 筛选的老客户端会看到这两类消息的 type 变了。
  账号注销的动作给的是 `security`（注销入口在账号安全页），不是 `membership`。
- **单条写操作的 404**：契约只写了详情返回「消息不存在」；按「读写接口对过期消息按不存在处理」，`read`、`unread`、`DELETE /:id`
  遇到不存在 / 别人的 / 过期的消息也返回 40455（旧实现静默成功）。
- **`clear` 的 `status`** 除契约的 `all` / `read` 外仍接受 `unread`（旧接口就支持）。
- **`unread-count` 的 `categories`** 与 `categoryUnread` 同样只含 n > 0 的分类。
- **`readAt` 恒在**（旧实现为空时省略），`status` 不再出现 `delivered`。
- **级别**：契约只列了四个值；旧控制台发的是 `critical`，因此写入时归一而不是报错。
- **管理员删除也推 `notification.deleted`**（契约没写，多端一致需要）。
- **`created` 插件钩子**：契约要求 Deliver 发钩子；群发时为避免一次群发触发上千次钩子，逐人钩子不发，保留原有的整批 `notification.onSent`。
- 配图与过期时间不合法用 40010（参数类），没有单独的码。
