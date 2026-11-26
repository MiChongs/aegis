# 积分商城：赠好友

> 面包屑：[Aegis](../CLAUDE.md) › [积分商城](points-store.md) › 赠好友
> 代码：`internal/domain/pointstore/gift.go`（发给谁、可赠权益、风控判据目录与判定，纯函数）·
> `internal/repository/postgres/points_store_gift_repository.go`（赠送、审核、撤销追回，各自一个事务）·
> `internal/service/points_store_gift_service.go` · `internal/transport/http/points_store_gift_handlers.go` ·
> 迁移 000169 · 控制台积分商城的「赠送」「赠送风控」页签

付积分的人把一件商品送给同一应用里的另一个账号：兑换时直接选人，扣积分与给对方发放在同一个事务里完成。
取代以前的做法：把商品配成「人工发放」，让用户兑换后去意见反馈里写清送给谁、由客服手工开通。

## 商品：发给谁

`points_store_items.delivery`：`self`（默认，兑换人自己）/ `gift`（兑换人选的好友）。

- 赠送商品只能配撤销时追得回来的权益：会员套餐（限时）、会员天数、功能解锁、个性装扮（`GiftableRewardTypes`）。
  余额等于转账；经验、抽奖次数、号码权益发出去马上能用掉，追不回来；人工发放正是要去掉的东西；
  永久会员一开通就把对方的存量折算掉，撤销时还原不了。保存商品时就拒收（40000）。
- 赠送商品不能走 `/points/store/redeem`（42254），普通商品不能走 `/points/store/gift`（42254）。
- 可兑换判定照旧（库存、名额、限兑、兑换对象、等级、积分都按**付款的人**算），只有「已拥有」改按**接收人**算。
  商城列表上赠送商品不按兑换人自己的持有判「已拥有」（选人之前无从判断）。

## 一次赠送

```
锁双方用户行（主键升序）→ 幂等键回放 → 锁商品行 → 接收人状态 → 双方事实与可兑换判定 → 补货
→ 赠送风控事实与判定 → 扣库存 → 落兑换记录与赠送记录 → 扣积分 → 放行的给接收人逐项发放 → 回写结果
```

- **锁双方、按主键升序**：A 送 B 与 B 送 A 同时发生时排队，不会互相等死；同一个赠送人的并发请求在他的用户行上排队，
  当日次数、额度在锁里数，不会两笔都读到「今天还能送一次」（集成测试：6 个并发、上限 2，恰好 2 笔成功）。
- 兑换记录（付款人的账）照常记：限兑、名额、库存、统计都照兑换算，`giftNo` 指向赠送记录。
- 发放复用兑换的发放分支，受益人换成接收人；各账本流水的 metadata 带 `giftNo` 与 `senderId`，追得到是谁送的。
- 幂等：`requestId` 与兑换同一个唯一索引（兑换记录上），回放返回第一次的赠送（`replayed=true`）。
- 硬性条件（不可配置）：不能送自己（42251）；对方停用、封禁（应用级或全站连带）、正在注销（42252）；
  对方已拥有（42253）；查无此人（40432）。

结局三种，都不做事务外补偿：

| 风控结论 | 结果 |
|---|---|
| 放行 | 当场送达（`delivered`），双方站内信，接收人实时推送 `points.changed` |
| 审核 | 积分已扣、库存已扣，权益未发：兑换记为 `pending`、赠送记为 `reviewing`，进审核队列 |
| 拒绝 | 整笔回滚、不扣积分，错误里是给用户看的原因；另记一条 `blocked` 留痕（与积分账无关的一行日志） |

## 风控

策略一个应用一行（`points_store_gift_policies`），没保存过按推荐默认值。每条判据独立判，命中多条取最重的处置，
全部命中都记在赠送记录上（`risk_hits`），连同判定时的事实快照（`risk_facts`）给管理员看。

| 判据 | 默认 | 说明 |
|---|---|---|
| `sender_age` 赠送人注册天数 | < 7 天拒绝 | 新号刷积分送主号 |
| `recipient_age` 接收人注册天数 | < 3 天审核 | 小号接收 |
| `same_device` 同一设备 | 拒绝 | 登录设备台账（去掉 UA 摘要与单次会话两种兜底键）、注册台账、近 90 天成功登录的设备标识，两边有交集 |
| `same_network` 同一网络 | 30 天内审核 | 成功登录地址、登录设备的最近地址、注册地址；IPv6 按 /64，内网与回环不算。家人室友也会命中，所以默认审核 |
| `sender_daily_count` 每日赠送次数 | 3 次 | 应用默认时区的自然日；审核中与已撤销的算，驳回与拦截的不算 |
| `sender_daily_points` 每日赠送额度 | 30000 积分 | 已送加本笔超过即拒 |
| `recipient_daily_count` 每日接收次数 | 3 份 | 多个小号集中送一个人 |
| `pair_frequency` 频繁送同一人 | 24 小时 1 次 | 42931 |
| `mutual` 互相赠送 | 7 天内审核 | 对方刚送过你：互刷限兑名额与活动折扣 |
| `large_gift` 大额赠送 | 关 | 单笔达到阈值送审核 |
| `risk_platform` 风险决策平台 | 审核 | `points_gift` 场景（services/risk 目录已登记，营销反作弊模板覆盖）。平台判拦截 / 封禁一律拒绝；判复核、加验时按这里的处置；平台没接入或出错时跳过，其余判据照常 |

错误码：风控拒绝 42255（新号、同设备、同网络、平台）、次数与额度 42930、频次 42931、赠送未开放 42256。
`message` 是具体原因（如「账号注册满 7 天后才能赠送」「不能赠送给与你使用同一台设备的账号」），客户端原样展示。

## 审核与撤销（管理端）

- **通过**：在一个事务里给接收人发放。发放前按此刻复核对方状态与是否已拥有，不满足时报错（42252 / 42253），应改为驳回。
- **驳回**：积分原路退回（流水分类 `points_store_refund`）、本补货周期的库存加回，兑换记为已取消。只通知赠送人。
- **撤销**（已送达的）：逐项追回，结果记在 `revocation`：
  - 会员（套餐 / 天数）：作废这段开通，扣掉它**还没走完**的时长（段终点 − max(段起点, 此刻)），顺延链上排在后面的各段前移同样的时长。已经过去的时间不追。
  - 功能解锁、个性装扮：按发放结果里的编号收回这一条（与管理员收回同一口径，后面排着的限时段不前移）。
  - 追不到的（已被收回、已折算、已过期）记为 `missing` / `expired`，不让整笔撤销失败。
  - `refundSender`：误送、客诉时退积分（兑换记为已取消）；套利、欺诈不退（兑换记为 `revoked`，计入限兑与统计）。
- 赠送的兑换不能在兑换记录里「标记发放 / 取消」（40969）：那样东西发不到接收人手上。

状态码：40433 赠送记录不存在、40909 当前状态不允许这次操作（只有审核中的能通过或驳回，只有已送达的能撤销）。

## 接口

用户端（需登录）：

```
GET  /points/store/gift/recipients?q=   选人：不带 q 给最近互动过的人（送过的、送过我的、邀请关系），带 q 按用户号码找
POST /points/store/gift/preview         { itemId, recipient } 核对：对方资料、会收到什么、allowed / reason / message；不扣积分
POST /points/store/gift                 { itemId, recipient, message, requestId, deviceId }
GET  /points/store/gifts?box=sent|received   我的赠送记录
```

`recipient` 是对方的用户号码，或候选里的 `publicId`（16 位，不受改号影响）。另一方只给对外 ID、号码、昵称与头像；
接收人看不到对方花了多少积分，也看不到审核中、被驳回的赠送；被拦下的尝试不出现在用户的记录里。
商城每件商品多出 `delivery`；兑换记录多出 `giftNo`，状态多一个 `revoked`。

管理端：`/api/admin/apps/{appKey}/points-store/gifts`（`status=reviewing` 即审核队列，带 `summary` 各状态笔数）、
`/gifts/{giftId}`、`/gifts/{giftId}/approve|reject|revoke`（`{note, refundSender}`）、`GET|PUT /gift-policy`。
审核与撤销挂在权限点 `app:points_store:gift_review`。MCP：`store_gifts` / `store_gift` / `store_approve_gift` /
`store_reject_gift` / `store_revoke_gift` / `store_gift_policy` / `store_save_gift_policy`。

Kotlin SDK：`engagement.pointsStoreGiftRecipients(q)` / `previewPointsStoreGift` / `giftPointsStoreItem` / `pointsStoreGifts(box)`。

## 迁移已有的「人工发放」赠送商品

迁移不动现网商品（不写死应用与商品编号）。上线后在控制台把原来写着「兑换后在意见反馈中提交好友账号」的商品改成：
发给谁 = 赠送好友、权益换成对应的会员天数 / 套餐 / 功能解锁，并删掉描述里引导去意见反馈的话。
已兑换、还在待发放的旧单（当时没有记下送给谁）仍按人工发放在兑换记录里处理。

## 验证

```
AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPointsStoreGiftIntegration
```

覆盖：赠送商品的保存校验、兑换方式不符、选人与核对、送达（只从赠送人扣积分、会员发到接收人）、幂等回放、
硬性条件（自己、查无此人、对方停用、对方已拥有）、风控拦截（频繁送同一人、同设备、UA 摘要不算设备、新号赠送、
每日次数与额度、对方每日接收）、进入审核（同网络、内网地址不算、新号接收、互送）后通过与驳回（退积分、加回库存）、
在兑换记录里处理赠送被拒、撤销追回（不退 / 退积分、会员时长、功能解锁）、重复撤销、双方看到的记录、并发下每日次数不被突破。
