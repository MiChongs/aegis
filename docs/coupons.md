# 优惠券与优惠活动

> 迁移 000177 · 领域 `internal/domain/coupon` · 仓储 `coupon_repository.go` / `coupon_order_repository.go` · 服务 `coupon_service.go` · MCP `coupons_`

一个应用的优惠由两样东西组成：

- **优惠券**：先配一种券（模板），再发到用户手里（用户券）。用户下单时自己选用哪张。
- **优惠活动**：对符合条件的订单自动生效，用户不用做任何事。三种：**限时折扣**（打折 / 立减 / 特价）、**满减**（多档）、**支付返券返积分**（付款满额后发券、发积分，不改价格）。

两者都在支付网关下单时结算，接入方不需要另调接口。

## 适用范围

范围对应订单用途：

| 范围 | 订单 | 能打折 | 能返券 |
|---|---|---|---|
| `vip_purchase` | 买会员套餐（可再按套餐限定） | ✓ | ✓ |
| `integral_purchase` | 积分充值：按**原价**发积分，用户少付钱 | ✓ | ✓ |
| `goods` | 不带用途的普通商品单（价格由接入方给，接入方自己发货） | ✓ | ✓ |
| `wallet_recharge` | 余额充值（可再按档位限定） | ✗ | ✓ |

余额充值不能打折：充进去的本金必须等于实付，打折充值等于凭空印钱；充值档位本来就有附赠，这里只开放返券返积分。

## 优惠券

| 字段 | 说明 |
|---|---|
| `kind` | `amount_off` 立减 `amount` 元；`percent_off` 折扣，`rate` 是付款比例（85 即 8.5 折），`maxDiscount` 封顶（0 不封顶） |
| `minAmount` | 使用门槛（0 无门槛）；按**活动价之后**的金额判 |
| `scopes` / `planIds` | 适用订单与指定套餐 |
| `stackWithPromotion` | 能否与活动同享 |
| `validityMode` | `fixed`：`validFrom`（空为发放时）到 `validUntil`；`relative`：发放后 `validDelayDays` 天生效、再过 `validDays` 天失效 |
| `totalQuantity` | 发放总量，全部渠道共用（0 不限） |
| `claimable` / `claimStartsAt` / `claimEndsAt` / `perUserLimit` / `audience` | 领券中心：开放领取、领取时段、每人限领、人群 |
| `refundReturn` | 订单全额退款后把券还给用户（还在有效期内时） |
| `status` | `draft` → `active` ⇄ `paused` → `ended`（终态）。暂停：不能再领、再发，已发的照常能用；结束同样不影响已发的券 |

**发出第一张之后规则字段锁定**（方式、面额、门槛、范围、叠加、有效期）：已经在用户手里的券改了面额，用户看到的和用的就对不上了。之后只能改名称、说明、色调、库存（不能少于已发张数）、领取设置与退款返还；要换规则就新建一种券。

人群（`audience.kind`）：`all` / `new_user`（注册 `newUserDays` 天内）/ `first_order`（在本应用还没付过款）/ `non_vip` / `vip`（会员段、永久身份或到期时间任一生效）。

用户券状态：`available` 可用 → `locked` 已用于一笔待支付订单 → `used`；或 `expired`、`revoked`。展示时可用的券再按时间推导出 `not_started`（未生效）与 `expired`。

### 发券渠道

一切发券只走 `grantCouponTx`（条件扣库存 → 逐张写入，券号 `C` + 12 位，应用内唯一）：

| 渠道 | 来源 `source` | 说明 |
|---|---|---|
| 领券中心 | `claim` | 锁用户行后数已领张数、判人群与时段，同一事务发 |
| 管理员发放 | `admin` | 每人一个事务；暂停中的券也能发；可选站内信 |
| 支付返券 | `rebate` | 付款结算时发；暂停中的券也能发 |
| 卡密 / 积分商城 / 抽奖 / 物品发放（含等级奖励）/ 邀请（含排行榜、推广活动） | `card_key` / `points_store` / `lottery` / `item_grant` / `invite` / `leaderboard` | 奖励目录里的 `coupon`：`refId` 券编号、`amount` 张数（1–20）。券发不出去时：卡密兑换失败、卡不作废；积分商城整笔回滚、积分不扣；抽奖这一抽失败、不扣次数；物品发放这一笔记失败可重试 |

## 优惠活动

| 字段 | 说明 |
|---|---|
| `kind` + `rule` | `discount`：`discountMode` 为 `percent`（`rate`、`maxDiscount`）/ `amount_off`（`amount`）/ `fixed_price`（`amount`，只能用于指定会员套餐）；`threshold`：`tiers [{minAmount, off}]` 1–5 档，门槛越高减得越多；`rebate`：`minAmount` + `rebateCoupons [{templateId, count}]` + `rebateIntegral` |
| `scopes` / `planIds` / `packageIds` | 范围、指定套餐、指定充值档位（只有返券活动能选充值） |
| `audience` / `startsAt` / `endsAt` | 人群与时段 |
| `stackWithCoupon` | 能否与券同享 |
| `perUserLimit` / `totalLimit` / `budget` | 每人参与次数、总次数、优惠总额上限（0 不限）；参与按**下单**计，没付款关闭的单与全额退款的单不算 |
| `priority` | 两期活动优惠一样多时优先级高的生效；返券活动只取优先级最高的一期 |
| `tag` / `theme` | 角标（空则按规则生成「限时 8 折」「满 100 减 20」）与色调 |

改规则只影响之后的订单；有订单参与过的活动不能改类型、不能删除（改为结束）。

## 定价

`coupon.PriceQuote` 一个纯函数，试算（`POST /pay/quote`）、下单、管理端试算用的是同一份：

1. 价格活动：每期按原价算优惠，取**最多的一期**（相同取优先级高的）。预算不够打这一单的折扣时这期不命中（不打半截）。
2. 每张券：先判状态、时间、范围、套餐；再分别算「配能同享的最优活动」与「只用券」，取总优惠更多的那个。用券反而不比不用券更省的券**不可选**，原因写明（`reasonCode` + `reason`）。
3. 自动选券（`coupon_mode=auto`）取总优惠最大的一张，相同取先过期的。
4. 优惠后至少付 **0.01**（渠道不接受 0 元单）；金额按分四舍五入。
5. 返券活动：命中优先级最高的一期；都没到门槛时给出差得最少的那期作预告（`rebate.gap`）。

`Quote` 返回 `originalAmount` / `promotionDiscount` / `couponDiscount` / `payableAmount`、用上的 `promotion` 与 `coupon`、每张券的 `coupons[]`（`usable`、`reasonCode`、`discount`、`totalDiscount`、`payable`、`best`、`dropsPromotion`）、`bestCouponId`、`rebate` 与 `notes`。

原因代码：`locked` 已用于待支付订单 / `not_started` / `expired` / `scope` / `plan` / `threshold`（`还差 X 元`）/ `stack`（不能与活动同享，活动更优惠）/ `no_discount` / `status`；活动另有 `audience` / `limit` / `sold_out` / `budget` / `window`。

## 订单上的优惠

```
下单    锁用户 → 预占活动名额与预算（条件更新）→ 占用券 available→locked → 写订单 → 写 payment_order_promotions(reserved)   一个事务
付款    SettleOrder：券 locked→used、模板计入使用、返券返积分（保存点内，失败只记在这一单上）；reserved→settled     幂等
关闭    订单没付款、过期满 15 分钟：券还回去（过期了就标过期）、名额与预算退回；reserved→released                   每分钟扫描
退款    全额退款结算的同一事务里：名额与预算退回，券按 refundReturn 返还，返出去的券（还没用的）收回、返积分扣回；settled→refunded
```

- **下单**：`POST /pay/orders` 新增 `coupon_id`（指定用户券）、`coupon_mode`（`auto` / `none`）、`expected_amount`（试算时看到的应付）。活动自动生效。订单 `amount` 是优惠后的应付；套餐价、档位价、积分充值的发放数按**原价**校验与履约。优惠快照写进订单 `metadata`（`originalAmount`、`promotionId`、`couponId`……，客户端传来的同名键一律清掉），订单 JSON 由它推导出 `discount` 块。
- `expected_amount` 与服务端算出来的不一致 → 42318（`data.payableAmount`），不下单；券在试算之后被别的订单用掉、活动名额刚满 → 42319，整笔回滚。
- **结算**挂在三处：渠道通知确认付款后、余额支付的同一请求里、用户查单时；另有后台每分钟补结算已付款未结算的单。
- **晚到的付款**：渠道过了失效时间仍收了钱（释放之后才到账）照样结算：重新计入名额与预算（可能因此略超，钱已经收了）；券还没被别的订单用掉就记为这一单用了，已被用掉则在记录 `note` 上写明（同一张券优惠了两次，不追）。
- **部分退款**不动优惠；累计退满订单金额那一笔触发冲正。冲正失败只写进退款单的 `reversalMessage`，不让退款结算失败。

## 接口

用户端（网关，需登录）：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/pay/quote` | 下单前试算，参数与 `POST /pay/orders` 同名（`purpose`、`plan_id`、`package_id`、`amount`、`coupon_id`、`coupon_mode`） |
| GET | `/coupons/center` | 领券中心：`items[]` 券面、剩余（-1 为不限）、我已领几张、`canClaim` 与原因 |
| POST | `/coupons/claim` | 领一张：`{templateId}` |
| GET | `/coupons` | 我的券：`status` 为 `usable`（默认）/ `available` / `not_started` / `locked` / `used` / `expired` / `revoked` / 空（全部），带 `summary` |
| GET | `/promotions` | 进行中与即将开始的活动（我能不能参加）、每个在售套餐的活动价 `planPrices`、可用券张数 `usableCoupons` |

实时事件 `coupon.changed`（券包变化）；站内信类型 `coupon`（奖励分类），路由键 `coupons`。

管理端 `/api/admin/apps/{appKey}/coupons/*`：`catalog`、`overview?tz=&days=`、`templates`（增删改查、`/{id}/status`、`/{id}/issue`）、`user-coupons`（`/{id}/revoke`）、`promotions`（增删改查、`/{id}/status`）、`orders`、`quote`（替用户试算，只算不写）。权限菜单「优惠券与活动」：`app:coupon:read`（含试算）/ `write` / `delete` / `issue`（发券与收回，危险）。

## 错误码

| 码 | HTTP | 含义 |
|---|---|---|
| 42310 | 404 | 券不存在 |
| 42311 | 404 | 活动不存在 |
| 42312 | 404 | 用户券不存在 |
| 42313 | 422 | 现在不能领 / 发（`data.reasonCode`） |
| 42314 | 409 | 已抢光 |
| 42315 | 409 | 已领够 |
| 42316 | 403 | 不满足人群条件 |
| 42317 | 422 | 指定的券不能用于这笔订单（`data.reasonCode`、`data.couponId`） |
| 42318 | 409 | 应付金额与 `expected_amount` 不一致（`data.payableAmount`） |
| 42319 | 409 | 下单时券或活动已变化（被占用、名额满、预算用完、活动结束） |
| 42320 | 400 | 券的配置不合法 |
| 42321 | 400 | 活动的配置不合法 |
| 42322 | 409 | 已发过券的模板不能改规则字段 |
| 42324 | 409 | 已发券的模板 / 有订单的活动不能删除 |
| 42325 | 409 | 状态不能这样改；或这张券不能收回（被占用、已使用） |

## 统计口径

概览只统计**已结算**的订单（`settled`）：订单数、实付、原价、券优惠、活动优惠、返出的积分与券；趋势按 `tz` 分日（发券按发放时间、用券按使用时间、订单按结算时间）。积分流水分类 `coupon_rebate`（计入周期榜）与 `coupon_rebate_refund`（扣减，不计入）。
