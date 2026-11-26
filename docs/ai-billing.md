# AI 额度与计费（时段价格、会员优惠、额度包）

AI 中转（[ai-gateway.md](ai-gateway.md)）的收费面。2026-11-26 起 **AI 额度是一种独立的资产，与积分不互通**：
积分不再能付 AI 调用、也不能换成 AI 额度；AI 额度只能用余额买（额度包）、按会员身份领（会员额度）、
活动与激励广告赠送、管理员发放。调用时可以直接用余额付。

单位仍是「点」（库里微点，1 点 = 10⁶ 微点），对外文案统一叫「额度」。**1 额度不再等于 1 积分**；
额度值多少钱由额度包的售价与 `walletRate`（余额直付时每额度多少元）决定。

## 扣费来源

| 键 | 名称 | 说明 |
|---|---|---|
| `free_daily` | 每日免费额度 | 所有用户，按应用时区的日历日重置 |
| `vip_daily` / `vip_monthly` | 会员每日 / 每月额度 | 有效会员；数额按会员档位（见「会员优惠」） |
| `pack` | 额度包 | 买来的、新人礼、管理员限期发放的额度，**有到期时间，先到期的先用** |
| `credit` | 永久额度 | 激励广告、活动发放、调用退款进来的额度，不过期 |
| `wallet` | 余额 | 按 `walletRate`（元 / 额度）直接从钱包扣，零头累计凑整 |

`settings.sources` 是启用的来源及顺序，默认 `free_daily → vip_daily → vip_monthly → pack → credit → wallet`。
`integral`（积分）已从目录移除，保存设置时拒绝；迁移 000172 把各应用设置里的 `integral` 去掉并在 `credit` 前补上 `pack`。
旧的积分暂扣、结算与退款仍按原规则走完（只认存量记录）。

## 价格怎么算

```
本次价格 = 计量 × 模型单价 × 倍率
倍率     = 全局倍率 × 会员倍率 × 等级倍率 × 时段倍率      （stacking = multiply，默认）
         = 全局倍率 × min(会员倍率, 时段倍率) × 等级倍率   （stacking = best 且时段倍率 < 1）
```

`stacking = best` 时会员折扣与闲时折扣只取更优惠的一个；时段倍率 > 1（高峰加价）时总是相乘。
倍率在**暂扣那一刻**定下并快照进调用记录（`billing.pricing`），调用跨过时段边界也按开始时的价格结算。

### 时段价格

`settings.timePricing`：

```json
{
  "enabled": true,
  "stacking": "multiply",
  "windows": [
    {"id": "night", "name": "夜间半价", "tone": "off_peak", "weekdays": [], "start": "00:30", "end": "08:30",
     "multiplier": "0.5", "models": [], "kinds": [], "from": null, "to": null, "enabled": true},
    {"id": "peak", "name": "工作日高峰", "tone": "peak", "weekdays": [1,2,3,4,5], "start": "09:00", "end": "18:00",
     "multiplier": "1.2", "models": ["deepseek-v4.1-flash"], "kinds": ["chat"], "enabled": true},
    {"id": "double11", "name": "双十一", "tone": "promo", "weekdays": [], "start": "00:00", "end": "00:00",
     "multiplier": "0.3", "from": "2026-11-11", "to": "2026-11-11", "enabled": true}
  ]
}
```

- 时间按 `settings.timezone`。`start`/`end` 为 `HH:MM`，半开区间 `[start, end)`；`end ≤ start` 表示跨零点
  （`22:00`–`02:00`），`start = end` 表示全天。
- `weekdays` 1 = 周一 … 7 = 周日，空 = 每天；跨零点的时段按**开始那一天**判星期（周五 22:00–02:00 覆盖周六凌晨）。
- `from` / `to`（`YYYY-MM-DD`，含当天，可空）限定活动日期，同样按开始那一天判断。
- `models`（对外模型名）、`kinds`（模型种类）为空表示全部；两者都填时同时满足。
- 多个时段同时命中时取**列表里靠前的一个**（控制台可拖动排序）。`tone` 只影响展示：
  `off_peak` 闲时、`peak` 高峰、`promo` 活动、`normal` 普通。
- 倍率 0.01–100，最多 32 个时段；`id` 为 1–32 位字母数字、`-`、`_`，保存时缺省自动生成。

### 会员优惠

会员按档位（`vip_tiers`，如 VIP / SVIP / 领航者）给不同优惠，`settings.memberTiers` 按优先级排列：

```json
[{"tierId": 2, "label": "领航者", "multiplier": "0.7", "daily": "500", "monthly": "20000",
  "packDiscount": "0.8", "packBonusRate": "0.15", "rpm": 200, "maxConcurrent": 16},
 {"tierId": 5, "label": "SVIP", "multiplier": "0.8", "daily": "300", "monthly": "8000",
  "packDiscount": "0.85", "packBonusRate": "0.1", "rpm": 120, "maxConcurrent": 10}]
```

- 用户此刻持有的档位（会员判定同一份段，`vip.Holdings.Tiers`）里，**列表中最靠前**的那个生效。
- 是会员但持有的档位都没配置（或不挂档位的老会员）时，用通用会员配置：`vipMultiplier`、`vipDaily`、
  `vipMonthly`、`vipPackDiscount`、`vipPackBonusRate`。非会员全部为 1 / 0。
- `multiplier` 调用倍率；`daily` / `monthly` 会员每日 / 每月额度（替代通用值）；`packDiscount` 买额度包的折扣（0.85 = 85 折）；
  `packBonusRate` 买额度包额外多给的比例（0.1 = 多送 10%）；`rpm` / `maxConcurrent` 为 0 时沿用应用设置，否则替代。
- `label` 为空时取档位名。

### 等级倍率

`levelMultipliers` 不变：取用户等级满足的最高一档。

## 额度包

管理员在控制台上架额度包（`ai_gateway_packages`），用户用**钱包余额**购买，得到一个**额度批次**（`ai_gateway_credit_lots`）。

| 字段 | 说明 |
|---|---|
| `points` / `bonusPoints` | 额度 / 赠送额度（都进同一批次，同一到期时间） |
| `price` / `originalPrice` | 售价（元）/ 划线价（0 不显示） |
| `validityDays` | 有效天数，0 = 永久（永久的批次仍按 `pack` 来源扣，排在限期批次之后） |
| `audience` / `tierIds` | `all` 或 `vip`；`tierIds` 非空时只有持有这些档位的会员能买 |
| `memberDiscount` | 是否享受会员的 `packDiscount` 与 `packBonusRate` |
| `perUserLimit` / `monthlyLimit` | 每人累计限购 / 每人每自然月限购，0 不限 |
| `stock` | 总库存，0 不限；`sold` 已售 |
| `startsAt` / `endsAt` | 售卖时间窗（限时包），可空 |
| `badge` / `tone` / `featured` | 角标文字、配色（`blue` `violet` `amber` `emerald` `rose` `slate`）、主推 |

**报价**（纯函数 `aigateway.QuotePackage`）：

```
finalPrice   = memberDiscount ? floor₂(price × packDiscount) : price        （分位向下取整，至少 0.01；price 为 0 的免费包保持 0）
memberBonus  = memberDiscount ? floor(points × packBonusRate) : 0
totalPoints  = points + bonusPoints + memberBonus
unitPrice    = finalPrice / totalPoints × 10000                              （元 / 万额度，展示用）
```

**购买**（单事务，锁序 users → ai_gateway_accounts → ai_gateway_packages → user_wallets）：校验在售、档位、限购、库存，
扣余额（钱包流水 `ai_package`，先扣赠送余额），`sold + 1`，建批次与订单 `ai_gateway_package_orders`，写一条额度流水 `purchase`。
`requestId`（8–64 位）幂等：同一用户同一编号重放返回原订单（`replayed: true`），编号用于别的包时 40242。
带 `expectedPrice` 时与当前报价不符即 40236（价格变了，请刷新）。

**退款**（管理员）：按批次**剩余比例**退钱：`refundAmount = floor₂(finalPrice × remaining / amount)`，批次作废（`revoked`），
钱包流水 `ai_refund`（按原拆分先补赠送余额），额度流水 `order_refund`。批次有调用占用（`frozen > 0`）时 40238。

## 额度批次

| 字段 | 说明 |
|---|---|
| `origin` | `purchase` 购买、`welcome` 新人礼、`admin` 管理员发放、`grant` 发放规则 / 活动 |
| `amount` / `remaining` / `frozen` | 总量 / 可用 / 调用暂扣中 |
| `expiresAt` | 到期时间，null 永久 |
| `status` | 存储值 `active` / `expired` / `revoked`；对外另有计算值 `exhausted`（active 且 remaining + frozen = 0） |

- 暂扣：锁住该用户全部可用批次（`expires_at` 升序、永久最后、同时到期按 id），按顺序从 `remaining` 移进 `frozen`，
  暂扣记录 `holds[].lots` 记每个批次各扣了多少。
- 结算：按 `holds[].lots` 原顺序承担实际费用，没用完的放回 `remaining`；超出暂扣的部分从仍有效的批次补。
  结算时批次已过期也照样扣（暂扣那一刻它是有效的）。
- 退款（调用退款）：批次还有效就退回原批次，已过期或作废的退为永久额度（与周期额度同一规则）。
- 到期：后台每分钟把 `active` 且已过期、或 `expired`/`revoked` 但还有 `remaining` 的批次清零，写额度流水 `expire` / `revoke`；
  有 `frozen` 的部分等结算后的下一轮再清。

## 新人礼

`settings.welcome = {enabled, amount, validityDays}`：每个用户可领一次（`POST /ai/welcome`），得到一个 `welcome` 批次。
领取记在 `ai_gateway_accounts.welcome_claimed_at`。

## 额度商店

`settings.store = {enabled, notice}`：`enabled` 关闭时用户端看不到额度包、不能购买（40233）；`notice` 是购买须知（纯文本，≤ 2000 字）。

## 额度流水

`ai_gateway_credit_ledger` 同时记永久额度与额度包：

| 字段 | 说明 |
|---|---|
| `kind` | `grant` 发放、`consume` 调用消费、`refund` 调用退款、`adjust` 管理员调整、`purchase` 购买、`welcome` 新人礼、`expire` 到期清零、`revoke` 作废、`order_refund` 额度包退款 |
| `bucket` | `credit` 永久额度 / `pack` 额度包 |
| `lotId` | 涉及的批次（`pack` 时） |
| `amount` | 变动（点，正负） |
| `balanceAfter` | 这一桶变动后的余额 |
| `totalAfter` | 变动后的 AI 额度合计（永久 + 额度包，不含暂扣中） |

一次调用结算时，永久额度与额度包各至多一条 `consume`。

## 接口

### 用户端（`/api/v1/apps/{appKey}`，需登录）

| 接口 | 说明 |
|---|---|
| `GET /ai/overview` | 总览（见下） |
| `GET /ai/pricing` | 价格方案：时段、我的倍率、各档位优惠对比、未来 7 天时间轴 |
| `GET /ai/models` | 模型与有效单价；新增 `basePrice`（不含时段）、`factor`、`window`（此刻对这个模型生效的时段） |
| `GET /ai/packages` | 额度商店：在售额度包与我的报价 |
| `POST /ai/packages/{id}/purchase` | 买额度包，body `{requestId, expectedPrice?}` |
| `GET /ai/orders` | 我的额度包订单（`page` `limit`） |
| `GET /ai/lots` | 我的额度批次，`scope=active`（默认，可用与暂扣中的）/ `all` |
| `GET /ai/credits` | 额度流水（永久 + 额度包） |
| `POST /ai/welcome` | 领新人礼 |

`GET /ai/overview`：

```jsonc
{
  "enabled": true, "allowSessionToken": true, "audience": "all", "eligible": true,
  "unit": "额度",
  "sources": [{"key": "free_daily", "label": "每日免费额度", "hint": "…", "periodic": true, "period": "day"}],
  "profile": {"isVip": true, "level": 3, "tierIds": [5], "tierId": 5, "tierName": "SVIP"},
  "member": {"source": "tier", "tierId": 5, "name": "SVIP", "multiplier": "0.8", "daily": "300", "monthly": "8000",
             "packDiscount": "0.85", "packBonusRate": "0.1", "rpm": 120, "maxConcurrent": 10},
  "factor": "0.4",
  "pricing": {"base": "1", "member": "0.8", "level": "1", "time": "0.5", "total": "0.4", "stacking": "multiply",
              "window": {"id": "night", "name": "夜间半价", "tone": "off_peak", "multiplier": "0.5", "endsAt": "2026-11-27T08:30:00+08:00"}},
  "nextChangeAt": "2026-11-27T08:30:00+08:00",
  "quotas": [{"source": "free_daily", "label": "每日免费额度", "period": "2026-11-26", "limit": "100", "used": "20",
              "reserved": "0", "remaining": "80", "eligible": true}],
  "pack": "12000", "packFrozen": "0", "credit": "500", "creditFrozen": "0",
  "balance": "12500",
  "expiring": {"amount": "3000", "at": "2026-11-30T12:00:00+08:00"},
  "wallet": "12.34", "walletRate": "0.001",
  "available": "24920",
  "dailySpendLimit": "0", "todaySpent": "20", "totalSpent": "8800", "totalRequests": 132,
  "rpm": 120, "maxConcurrent": 10, "maxKeysPerUser": 10,
  "welcome": {"enabled": true, "amount": "1000", "validityDays": 30, "claimable": false, "claimedAt": "2026-11-20T10:00:00+08:00"},
  "store": {"enabled": true},
  "baseUrl": "/api/ai/v1"
}
```

`integral` 字段已删除。`balance` = `pack + credit`（不含暂扣中）；`available` 仍是启用来源合计（含余额折算）。
`expiring` 是 7 天内到期的批次合计与最早的到期时间，没有时为 null。`rpm` / `maxConcurrent` 是会员档位生效后的值。

`GET /ai/pricing`：

```jsonc
{
  "timezone": "Asia/Shanghai", "now": "2026-11-26T23:10:00+08:00",
  "enabled": true, "stacking": "multiply", "base": "1",
  "levels": [{"level": 5, "multiplier": "0.95"}],
  "freeDaily": "100",
  "windows": [/* 启用的 TimeWindow，结构同设置 */],
  "current": {/* FactorBreakdown，同 overview.pricing */},
  "nextChangeAt": "2026-11-27T00:30:00+08:00",
  "timeline": [  // 从今天本地零点起 7 天、对全部模型生效的时段，相邻同价段已合并
    {"start": "2026-11-26T00:00:00+08:00", "end": "2026-11-26T00:30:00+08:00", "windowId": "", "name": "标准价", "tone": "normal", "multiplier": "1"},
    {"start": "2026-11-26T00:30:00+08:00", "end": "2026-11-26T08:30:00+08:00", "windowId": "night", "name": "夜间半价", "tone": "off_peak", "multiplier": "0.5"}
  ],
  "member": {/* 我的 MemberBenefit */},
  "tiers": [/* 每个已配置档位的 MemberBenefit（按优先级），末尾是通用会员 source=vip（有任一优惠时才给） */]
}
```

`GET /ai/packages`：

```jsonc
{
  "enabled": true, "notice": "购买后即时到账……",
  "member": {/* MemberBenefit */},
  "wallet": "12.34",
  "items": [{
    "id": 1, "name": "标准包", "subtitle": "日常对话够用一个月", "description": "", "badge": "热销", "tone": "violet", "featured": true,
    "points": "10000", "bonusPoints": "1000", "memberBonusPoints": "1000", "totalPoints": "12000",
    "price": "10.00", "originalPrice": "12.00", "finalPrice": "8.50", "memberSaving": "1.50", "discountLabel": "SVIP 85 折",
    "unitPrice": "7.08",
    "validityDays": 30, "audience": "all", "tierIds": [], "memberDiscount": true,
    "perUserLimit": 0, "monthlyLimit": 3, "purchased": 1, "purchasedThisMonth": 1,
    "stock": 0, "sold": 12, "remainingStock": null,
    "startsAt": null, "endsAt": null,
    "purchasable": true, "reason": "", "affordable": true
  }]
}
```

`reason`（`purchasable=false` 时）：`store_closed` `members_only` `tier_required` `not_started` `ended` `sold_out`
`limit_reached` `monthly_limit_reached`。`affordable` = 余额 ≥ `finalPrice`（不影响 `purchasable`）。

`POST /ai/packages/{id}/purchase` → `{"order": PackageOrder, "lot": CreditLot, "wallet": "3.84", "balance": "24500", "replayed": false}`

```jsonc
// PackageOrder
{"orderNo": "AIP…", "packageId": 1, "packageName": "标准包",
 "listPrice": "10.00", "price": "8.50", "discount": "1.50", "discountLabel": "SVIP 85 折",
 "points": "10000", "bonusPoints": "2000", "totalPoints": "12000",
 "validityDays": 30, "expiresAt": "2026-12-26T23:10:00+08:00", "lotId": 9, "walletTxnNo": "W…",
 "status": "paid", "refundAmount": "0", "refundPoints": "0", "refundReason": "", "refundedAt": null,
 "createdAt": "2026-11-26T23:10:00+08:00",
 "userId": 1, "account": "…", "refundedBy": "…"}          // 后三项只在管理端
// bonusPoints 为包自带赠送 + 会员多送之和

// CreditLot
{"id": 9, "origin": "purchase", "title": "标准包", "amount": "12000", "remaining": "11000", "frozen": "0", "used": "1000",
 "orderNo": "AIP…", "packageId": 1, "expiresAt": "2026-12-26T23:10:00+08:00", "status": "active",
 "createdAt": "…", "userId": 1, "account": "…"}           // 后两项只在管理端
```

`POST /ai/welcome` → `{"lot": CreditLot, "balance": "…"}`

### 管理端（`/api/admin/apps/{appkey}/ai-gateway`，`app:read` / `app:write`）

| 接口 | 说明 |
|---|---|
| `GET /catalog` | 新增 `vipTiers: [{id, name}]`、`tones`、`packageTones` |
| `GET/PUT /settings` | 新增 `memberTiers`、`timePricing`、`welcome`、`store`、`vipPackDiscount`、`vipPackBonusRate` |
| `POST /pricing/preview` | 价格模拟：body `{settings?, model?, kind?, tierId?, isVip?, level?, at?}`（`settings` 为草稿，不传用已保存的）→ `{breakdown, member, timeline, nextChangeAt, price?}` |
| `GET /packages` | 全部额度包，另带 `orders`、`buyers`、`revenue` |
| `POST /packages`、`PUT /packages/{id}`、`DELETE /packages/{id}` | 增改删；有订单的包不能删（40234，请下架） |
| `GET /package-orders` | 订单：`userId` `packageId` `status` `keyword`（订单号前缀）`from` `to` `page` `limit` |
| `POST /package-orders/{orderNo}/refund` | 按剩余比例退款，body `{reason}` |
| `GET /lots` | 批次：`userId` `status`（active / expired / revoked / exhausted）`origin` `page` `limit` |
| `POST /users/{userId}/lots` | 发放限期额度：`{amount, validityDays, title, requestId}` |
| `POST /lots/{lotId}/revoke` | 作废批次剩余额度：`{reason}` |
| `GET /commerce?days=30` | 额度生意：营收、退款、订单数、买家数、售出额度、未消耗负债（额度包 / 永久）、7 天内到期、按包、按日、按到期分布、按时段的调用与让利 |

`GET /commerce`：

```jsonc
{"days": 30, "revenue": "123.45", "refunded": "5.00", "orders": 40, "buyers": 20, "pointsSold": "400000",
 "outstanding": "350000", "outstandingPermanent": "120000", "expiringSoon": "20000",
 "byPackage": [{"packageId": 1, "name": "标准包", "orders": 10, "revenue": "85.00", "points": "120000"}],
 "daily": [{"date": "2026-11-01", "orders": 2, "revenue": "17.00", "refunded": "0"}],
 "expiry": [{"bucket": "7d", "amount": "…"}, {"bucket": "30d", "amount": "…"}, {"bucket": "90d", "amount": "…"},
            {"bucket": "later", "amount": "…"}, {"bucket": "never", "amount": "…"}],
 "windows": [{"windowId": "night", "name": "夜间半价", "requests": 100, "charged": "5000", "saved": "5000"}]}
```

`windows.saved` = 按调用当时的时段倍率折回的让利（`charged / time − charged`，加价时段为负）。

## 错误码

| 码 | HTTP | 含义 |
|---|---|---|
| 40232 | 404 | 额度包不存在或已下架 |
| 40233 | 403 | 额度商店未开放 |
| 40234 | 409 | 额度包已有订单，不能删除（请下架） |
| 40235 | 409 | 不可购买：未开始 / 已结束 / 售罄 / 达到限购（提示里写明是哪一种） |
| 40236 | 409 | 价格已变化，请刷新后重试 |
| 40237 | 402 | 余额不足 |
| 40238 | 409 | 订单不可退款（已退款，或额度正被调用占用） |
| 40239 | 404 | 订单不存在 |
| 40240 | 409 | 新人礼未开放或已领取 |
| 40241 | 404 | 额度批次不存在 |
| 40242 | 409 | 请求编号已用于另一笔购买或发放 |
| 40243 | 403 | 额度包仅限会员（或指定档位）购买 |

## 代码位置

领域纯函数 `internal/domain/aigateway/billing_plan.go`（时段、会员、报价、时间轴）；事务
`internal/repository/postgres/ai_gateway_billing_repository.go`（暂扣 / 结算 / 退款里的 `pack`）与
`ai_gateway_commerce_repository.go`（额度包、订单、批次、新人礼、到期清理、统计）；服务 `service/ai_gateway_commerce.go`；
迁移 `000172_ai_gateway_commerce`。
