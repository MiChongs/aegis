# 积分商城

> 面包屑：[Aegis](../CLAUDE.md) › 积分商城

应用级的积分兑换：用户拿签到、看广告、卡密攒下的积分，兑换会员、单项功能、经验、余额、
抽奖次数，或由运营人工发放的东西（周边、兑换码）。

| 表 | 内容 |
|---|---|
| `points_store_settings` | 开关，一个应用一行；没有这一行按关闭处理 |
| `points_store_items` | 商品：积分价、权益清单、库存、每人限兑、兑换对象、等级门槛、上下架时间 |
| `points_store_redemptions` | 兑换记录：扣了多少、发了什么（`results`）、人工发放的处理结果 |
| `user_feature_unlocks` | 功能解锁账本：与会员段并列的第三条线 |

## 权益目录

[`internal/domain/pointstore/catalog.go`](../internal/domain/pointstore/catalog.go) 是单一事实源，
服务端校验、控制台的商品表单、用户端的权益描述都读它。`TestPointsStoreCatalogHasGrantBranch`
把目录与发放分支双向钉死。

| 权益 | 取值 | 落到哪 |
|---|---|---|
| `vip_plan` | `refId` | `extendUserVipTx`，渠道 `points_store`；永久套餐即发永久会员；**不发套餐的赠送积分** |
| `vip_days` | `amount` 天 | `extendUserVipTx`，不带功能标识 |
| `feature` | `tag` + `amount` 天（0 为永久） | `user_feature_unlocks`，见下节 |
| `experience` | `amount` 点 | `applyExperienceChangeTx` |
| `balance` | `money`（字符串） | `applyWalletChangeTx`，类型 `points_exchange`，不计入累计充值 |
| `lottery_draws` | `amount` 次 | `lottery_draw_grants` |
| `manual` | `text` | 不自动发，兑换记录进入 `pending`，由管理员处理 |

一件商品最多 6 项权益；`feature` 可以出现多次（一次解锁几个不同的功能），其余每类一项。
`manual` 只能单独成为一件商品：整笔兑换要等人处理，不能与自动到账的混在一起，
否则「取消并退积分」就要回答「已经到账的那部分怎么办」。

引用（套餐、功能标识）在**保存商品时**就核对：试用套餐、未登记的功能直接拒收。
兑换时再核一次（套餐被删、功能被停用），失败即整笔回滚，原因 `reward_unavailable`。

套餐的赠送积分不随兑换发：用积分换来的会员再返积分，等于给商品打了一个运营没算过的折扣。

## 功能解锁：不当会员，也能单独拥有某个功能

会员段一旦存在，这个人就**是会员**：免广告、会员专属入口全部跟着打开。兑换「在线音乐 7 天」
的用户只该拿到在线音乐，所以功能解锁是独立的一条线（[`vip/unlock.go`](../internal/domain/vip/unlock.go)）：

- 只往 `features` 里加东西，不改 `isVip` / `adFree` / 会员来源。`/vip/status` 多一个 `unlocks`
  （每个功能一条：`tag`、`permanent`、`expireAt`），`features` 已经包含它们。
- 与会员功能同一个口径：与**启用中的**功能目录取交集。停用一个功能标识对会员与解锁同时生效。
- `HasFeature` 不再要求会员身份 —— 它只看 `features`，而 `features` 只含仍生效的东西。
  服务端会员校验（`/vip/verify`）与远程函数的 `hasFeature` 随之认解锁。
- 同一功能多次限时解锁时**顺延**：新的一段从这个功能仍生效的最晚终点开始。
- 已永久解锁的功能不能再兑（`owned`），管理员也不能再授予（40913）。
- 收回只标记这一条，后面排着的限时解锁不前移。

旧客户端判功能时多半还要求 `isVip`，它们看不到非会员的解锁 —— 这是向前兼容的代价，
新版客户端只看 `features`。

## 可兑换判定：一个纯函数

`pointstore.Evaluate(item, facts, usage, clock)` 回答「这件商品此刻能不能被这个人兑换」。
`/points/store` 里每件商品的 `availability` 与兑换失败的错误码出自同一个函数，
列表上「为什么灰着」与点了之后的报错永远是同一句话。判据顺序即优先级：

| reason | 码 | 含义 |
|---|---|---|
| `inactive` / `not_started` / `ended` / `reward_unavailable` | 40304 | 商品本身不可兑 |
| `closed_now` | 40304 | 不在每日开放时段内（提示「每日 10:00–22:00 开放兑换」） |
| `sold_out` | 40305 | 库存为 0（按补货周期换算后；开了补货时提示「已兑完，明日补货」） |
| `quota_reached` | 40305 | 全站名额已满（提示「今日名额已满」） |
| `limit_reached` | 40306 | 每人限兑：`limitCount` 次 / `limitPeriod`（total / 自然日 / ISO 周 / 自然月，应用默认时区），或每人累计上限 `userTotalLimit` |
| `vip_only` / `non_vip_only` / `lifetime_only` / `level_required` | 40308 | 兑换对象或等级门槛不符 |
| `owned` | 40309 | 永久会员已包含所兑会员（会员天数、功能已全包含的套餐），或功能已永久解锁 |
| `insufficient_points` | 40307 | 积分不足 —— 排最后：它是唯一一个用户自己马上能解决的原因 |

另有 40303（未开启）、40443（商品不存在）、40911（同一 requestId 并发提交）。
管理端：40444 兑换记录不存在、40912 已处理过、40445 解锁记录不存在或已收回、40913 已永久解锁。

## 时间规则：补货、名额、累计上限、开放时段

周期边界一律是应用默认时区的自然日、ISO 周（周一起）、自然月，与每人限兑同一口径（`pointstore.Clock`）。

- **补货周期** `restockPeriod`（none / day / week / month）+ `restockQuantity`：每到新周期，剩余库存回到补货数量。
  补货是**惰性**的，不跑定时任务：`stock` 属于 `stock_window_start` 那个周期，跨周期后读到时按补满看待
  （`Item.CurrentStock`），新周期第一笔兑换在锁住的商品行上把两者一起回写——补货与扣库存在同一把锁里。
  保存商品时填的剩余库存属于当前周期，不填则从满货开始。取消兑换时只有本周期兑出的那份退回库存，
  上个周期的货已随补货清零。
- **全站名额** `globalLimitPeriod` + `globalLimitCount`：所有人合计每个周期最多兑多少份，按兑换记录计数，
  取消的那笔自动让出名额。与库存的区别：库存是运营放进去的货，名额是速率上限，两者可以叠加。
  兑换时在锁住的商品行上计数，同一件商品的并发兑换在这里排队。
- **每人累计上限** `userTotalLimit`：与按周期限兑叠加（每日 1 次、累计 10 次）。`limitPeriod` 为 total 时没有意义，保存时抹为 0。
- **每日开放时段** `dailyOpenMinute` / `dailyCloseMinute`（自零点起的分钟数，关闭可写 1440 即 24:00）：
  两端都为空时全天开放；开放晚于关闭表示跨零点（22:00–02:00）。

`/points/store` 的每件商品因此多出：`stock`（换算后的此刻库存）、`restockPeriod`、`restockQuantity`、`nextRestockAt`、
`globalLimitPeriod`、`globalLimitCount`、`globalUsed`、`globalResetsAt`、`limitResetsAt`、`userTotalLimit`、`usedTotal`、
`openHours`、`openNow`、`nextOpenAt`；商城本身多出 `timezone`。管理端商品列表多出 `currentStock`、`nextRestockAt`、
`globalUsed`、`globalResetsAt`、`openNow`。

## 兑换：一个事务

锁用户行 → 幂等键回放 → 锁商品行 → 取齐事实（含全站名额计数）→ 跨周期则补货 → 判定 → 扣库存 → 落兑换记录 → 扣积分 → 逐项发放 → 回写结果。

- **锁用户行在最前**：限兑次数、积分余额、「已拥有」都是这个人的事实，不锁就有两个并发请求
  都读到「还剩一次」的窗口。锁商品行在其次：库存是共享的，判定与扣减在同一把锁里。
  所有路径都按「用户 → 商品」加锁。
- **幂等**：`requestId`（客户端生成）+ `UNIQUE(appid, user_id, request_id)`。回放查询排在锁用户行之后，
  同一个键的第二个并发请求会等第一个提交、然后原样拿到它的记录（`replayed=true`），不再扣积分。
- 任何一项发不出去整笔回滚：积分退回、库存复原，不存在「扣了积分少发一样」。
- 兑换记录存**快照**（标题、积分、权益）。商品改价、改权益、被删除之后，旧记录说的仍是当时那一份；
  删除商品时 `item_id` 置空，记录保留。

## 赠好友

`delivery=gift` 的商品兑换时先选人，权益直接发给对方，带风控、审核与撤销追回，见 [points-store-gift.md](points-store-gift.md)。
不要再用人工发放做「送好友」：赠送商品不能配 `manual`。

## 人工发放

含 `manual` 的商品兑换后为 `pending`。管理员在控制台的兑换记录里：

- **标记已发放**：写一句给用户的话（兑换码、快递单号），站内信通知。
- **取消**：退回积分（流水分类 `points_store_refund`）、库存加回、站内信通知。

只有待发放的能处理；已到账的权益已经被用上了，取消它要回答「已经用掉的怎么办」，那不是一个按钮能回答的。

## 接口

用户端（需登录）：

```
GET  /api/v1/apps/{appKey}/points/store                 首页：enabled、balance、level、isVip、categories、items[availability]
POST /api/v1/apps/{appKey}/points/store/redeem          { itemId, requestId, deviceId }
GET  /api/v1/apps/{appKey}/points/store/redemptions     我的兑换记录
```

未开启时 `/points/store` 回 `enabled=false`、空列表，不报错：客户端据此隐藏入口，
报错的话「没开」和「接口坏了」在客户端上长得一样。兑换成功后向用户的其它在线设备推送 `points.changed`。

管理端：`/api/admin/apps/{appKey}/points-store/{catalog,settings,items,redemptions,stats}`，
用户的功能解锁挂在 `/api/admin/apps/{appKey}/users/{userId}/feature-unlocks`（按用户数据权限判定）。

- `PUT .../points-store/item-order` `{ "itemIds": [...] }`：按数组顺序把排序值写成 10、20、30……（控制台拖拽排序），
  只改排序值，不碰库存，避免和并发兑换互相覆盖。
- `GET .../points-store/redemptions` 支持 `from` / `to`（RFC3339，左闭右开）按下单时间筛选。
- `GET .../points-store/stats?tz=` 按给定 IANA 时区切「今天」与按天分桶（控制台传浏览器时区），
  `summary.cancelled` 是窗口内被取消的笔数（不计入兑换汇总）。

Kotlin SDK：`engagement.pointsStore()` / `redeemPointsStoreItem(itemId, requestId)` / `pointsStoreRedemptions()`。

## 验证

`TestPointsStoreIntegration` 在真实 Postgres 上走完开关、引用校验、积分不足、功能解锁（不成为会员、顺延、幂等、限兑）、
库存、人工发放、永久解锁与已拥有、管理员授予与收回、永久套餐、并发抢库存、同一 requestId 并发、统计与删除：

```
AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPointsStoreIntegration
```

> 顺带修掉的既有问题：`grantLotteryDrawsTx` 的 `$3` 同时落进 INTEGER 与 BIGINT 两列，
> 真实 Postgres 上报 42P08 —— 卡密与激励广告的「抽奖次数」权益此前一发就整笔回滚。
