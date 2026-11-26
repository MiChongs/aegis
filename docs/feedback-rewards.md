# 有效反馈奖励积分

管理员在服务台给被采纳的意见反馈（`kind=feedback` 的工单）发积分；误发可以撤回、扣回积分。
默认关闭：应用没有配置策略、或策略未开启时，一切与原来一样。

## 策略（一个应用一行，`feedback_reward_policies`）

| 字段 | 含义 |
|---|---|
| `enabled` | 开关。未开启时发放返回 42257 |
| `defaultPoints` | 默认建议积分：分类没单独配时预填它 |
| `categoryPoints` | 反馈分类 key → 建议积分，如 `feedback_bug`（问题反馈）、`feedback_feature`（功能建议）、`feedback_experience`（体验吐槽） |
| `maxPoints` | 单条上限 |
| `userDailyLimit` / `userMonthlyLimit` | 每用户自然日 / 自然月累计上限（按服务器默认时区切分，已撤回的不计） |

上限字段 0 = 不限；任何字段不超过 1 亿（与管理员单次调整积分同一上限）。没有配置时接口返回推荐默认值
（未开启；默认 20，分类 50 / 30 / 20，单条 500，每天 500，每月 2000），`configured=false`。

建议值只是预填：管理员可以改数额，但不能超过单条与累计上限（42258，提示里带今天 / 本月还可发多少）。

## 谁能发

发放与撤回都要两样都有：

1. 能处理这张反馈：该应用的 `ticket:close`（结单与重开），或与我有关（受理人、处理组、关注人…），与改状态同一口径；结了单也能发。
2. 能改该应用用户的积分：`app:user:asset`（资产冲正）或 `points:write`（积分调整）。

详情的 `permissions.reward` / `permissions.revokeReward` 就是这个判定，控制台只看它。改策略要该应用的 `ticket:manage`。

只能发给**提单人是该应用用户**的反馈；用户撤销的、「不采纳」的不能发（42259）。

## 发放

`POST /api/admin/tickets/:ticketId/reward`，`{points, reason, requestId}`；`points` 省略按建议值。
也可以在改状态时一起发：`POST /api/admin/tickets/:ticketId/status` 带 `reward: {points, reason, requestId}`
——先发奖励、失败则状态不变；目标状态是不采纳 / 已撤销时直接拒绝。

一个事务里：锁反馈行 → 查有效奖励 → 锁用户行 → 判上限 → 记积分流水（分类 `feedback_reward`、来源 `feedback_reward`、
`source_id` = 反馈 ID、描述「反馈 FB-xxx 被采纳：理由」）→ 写奖励记录 → 时间线记 `reward_granted`。
提交后经 `NotificationService.Deliver` 给用户发站内信（分类「积分与奖励」，type `feedback_reward`）：
「你的反馈 #xxx 被采纳，获得 N 积分。」+ 理由。站内信只有这一个写入口，没法放进事务；发送失败只记日志，积分照样到账。

**一张反馈只发一次。** 同一时刻至多一条已发放的记录（部分唯一索引 `uq_feedback_rewards_active` 兜底）：

- 已有有效奖励时，同一 `requestId` 或同样的数额 → 原样返回（`replayed=true`），不再动积分；
- 数额不同 → 40999，要改数额先撤回再发；
- 并发点击在反馈行锁上排队，只落一笔。

## 撤回

`POST /api/admin/tickets/:ticketId/reward/revoke`，`{reason, allowPartial, requestId}`。

- 默认（`allowPartial=false`）：余额不够整笔拒绝（40009，提示当前余额）——与积分流水冲正同一规则；
- `allowPartial=true`：扣到 0 为止，差额记 `shortfall`——与取消永久会员收回折算积分同一规则；
- 扣回的流水分类 `feedback_reward_revoke`，`extra.reversalOf` 是发放那笔的流水号；时间线记 `reward_revoked`；
- 当初那条「奖励到账」站内信删除，另发一条「反馈奖励已撤回」；
- 撤回后可以按正确的数额重新发；记录全部留档（详情 `reward.history`）。

两个积分分类都在排行榜目录里登记为**不计入**周期榜：运营发的不是用户挣来的，撤回又是扣减，计入会让「发了再撤」在榜上留下凭空的分数。

## 用户侧

`GET /feedback/:id` 与 `GET /feedback` 的每一项多一个 `reward`：`{points, reason, grantedAt}`，只有当前有效的那一笔；
没发或已撤回时不下发。客户端据此显示「已获得 N 积分」。

## 管理端接口

| 接口 | 说明 |
|---|---|
| `GET /api/admin/tickets/reward-policy?appid=` | 读策略 |
| `PUT /api/admin/tickets/reward-policy?appid=` | 改策略（ticket:manage） |
| `GET /api/admin/tickets/:id/reward` | 奖励区块（建议值、本次最多可发、今日 / 本月已领、有效奖励、历史）；详情里的 `reward` 同形 |
| `POST /api/admin/tickets/:id/reward` | 发放 |
| `POST /api/admin/tickets/:id/reward/revoke` | 撤回 |

MCP：`tickets_reward`（`action=grant|revoke`，危险类）。

## 错误码

| 码 | 含义 |
|---|---|
| 40009 | 撤回时用户积分不足（未选扣到 0） |
| 40311 | 没有处理权限或积分调整权限 |
| 40407 | 没有可撤回的奖励 |
| 40921 | 请求编号已用于另一笔（发放后被撤回的旧请求重放） |
| 40999 | 已发放过数额不同的奖励 |
| 42257 | 应用未开启反馈奖励 |
| 42258 | 超过单条 / 每日 / 每月上限 |
| 42259 | 这张单子不能发奖励（不是反馈、提单人不是用户、已撤销、不采纳） |

## 参考

- HackerOne 赏金表：按资产与严重度给出建议金额，审核人可调整；奖励权限与处理权限分开授予；一份报告的赏金只增不删
  （这里允许撤回，是因为积分误发比现金更常见，撤回全程留档）。
- Discourse Gamification：按动作配分值、限定可得分的分类。
