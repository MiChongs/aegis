# 钱包（余额分桶、充值档位、余额兑换积分）

迁移 000154。代码：`internal/domain/wallet`（bucket.go、settings.go）、`internal/repository/postgres/wallet_repository.go`
（账本核心 `applyWalletChangeTx`）、`wallet_center_repository.go`、`internal/service/wallet_center_service.go`、
`internal/transport/http/wallet_center_handlers.go`。

## 1. 余额分两桶

| 桶 | 来源 | 能做什么 |
|---|---|---|
| 充值余额（本金，cash） | 在线充值（`recharge`）、余额支付订单的退款（按原单拆分退回）、管理员按 cash 调账 | 消费、**兑换积分** |
| 赠送余额（bonus） | 充值档位赠送（`recharge_bonus`）、积分商城换余额（`points_exchange`）、卡密、邀请、排行榜、物品发放、AI 退款、管理员默认调账 | 消费，**不能兑换积分** |

- `user_wallets.balance` 仍是可用总余额；`bonus_balance` 是其中的赠送余额；本金 = balance − bonus_balance。
  CHECK：`0 ≤ bonus_balance ≤ balance`、`0 ≤ frozen_bonus ≤ frozen`。
- 出账默认**先扣赠送余额**；兑换积分只扣本金（`cash_only`）；充值退款冲正先扣本金（`cash_first`）。
- 退回类入账（余额支付订单退款、AI 退款）按原出账的拆分先补回赠送余额（`restores_txn_no` 指向原出账），
  防止「用赠送余额下单再退款」把赠送余额洗成本金。
- 为什么要分：积分商城能用积分换余额；如果余额又能换回积分，就是一台套利机。
- AI 网关暂扣（frozen）先冻赠送余额，放回时先还赠送余额。
- 迁移在第一次加列时按历史流水重放回填一次（新规则），之后不再重放。

流水新增字段：`cashAmount`、`bonusAmount`（与 amount 同号，和等于 amount）、`restoresTransactionNo`。
钱包新增字段：`cashBalance`、`bonusBalance`、`frozenBonus`、`totalBonus`、`totalExchanged`。

新流水类型：`recharge_bonus`（充值赠送，入账；退款回收为出账）、`integral_exchange`（兑换积分，出账，计入 totalExchanged 不计入 totalConsumed）。

## 2. 用户端接口

两套路径同一处理函数：`/api/wallet/...` 与应用网关 `/api/v1/apps/:appkey/wallet/...`。

### GET /wallet/overview

```json
{
  "wallet": { "balance": "120.00", "cashBalance": "100.00", "bonusBalance": "20.00", "frozen": "0", "frozenBonus": "0",
              "totalRecharged": "100.00", "totalConsumed": "0", "totalBonus": "20.00", "totalExchanged": "0", ... },
  "currency": "CNY",
  "recharge": {
    "enabled": true, "customAmountEnabled": true, "min": "1", "max": "5000",
    "packages": [
      { "id": 1, "title": "充 100 送 20", "amount": "100", "bonusAmount": "20", "bonusIntegral": 500,
        "badge": "超值", "description": "", "perUserLimit": 0, "isActive": true, "sortOrder": 0,
        "startsAt": null, "endsAt": null, "purchased": 0, "unavailable": "" }
    ]
  },
  "exchange": { "enabled": true, "rate": 100, "exchangeable": "100.00", "min": "1", "max": "0", "dailyMax": "0",
                "usedToday": "0", "remainingToday": "100.00", "maxNow": "100.00" },
  "integralBalance": 3200,
  "notice": "充值须知……"
}
```

- `packages[].unavailable`：空 = 可买；`not_started` / `limit_reached` / `inactive`。已结束的档位不下发。
- `exchange.max` / `dailyMax` 为 `0` 表示不限。`maxNow` = min(本金, 单笔上限, 今日剩余)。
- 兑换比例 `rate` = 应用「交易设置」的 `integralPerCurrency`（与在线直购积分同价，默认 100，即 1 元 = 100 积分）。

### POST /wallet/exchange

请求 `{"amount": "10.00", "idempotencyKey": "…"}` 或 `{"all": true, "idempotencyKey": "…"}`（幂等键必填，≤64 字符）。

积分 = floor(amount × rate)；实扣本金 = ceil(积分 ÷ rate, 0.01) ≤ amount（比例能整除时等于 amount）。
`all` = 此刻能兑的上限（maxNow）。

响应：`{ transaction, wallet, integral, integralBalance, integralTransactionNo, rate, replayed? }`。

### 充值下单（已有 POST /pay/orders）

`metadata.purpose = "wallet_recharge"`；按档位充值再带 `metadata.walletPackageId = <id>`，`amount` 必须等于档位价格。
服务端固化档位快照（`walletPackageTitle`、`walletBonus`、`walletBonusIntegral`），客户端传的同名键一律无效。
支付成功后同一事务：本金入账 + 赠送余额入账 + 赠送积分入账。退款：本金按退款额扣回（先扣本金），
赠送余额按退款比例尽力回收，赠送积分仅全额退款时尽力回收。

### GET /wallet/transactions

`type` 支持逗号分隔多个类型，例如赠送栏：`recharge_bonus,points_exchange,card_key,invite,leaderboard,item_grant`。

## 3. 管理端接口（`/api/admin/apps/:appkey/...`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / PUT | `/wallet/settings` | 钱包设置；GET 附带只读的 `rate`、`currency` |
| GET | `/wallet/packages` | 全部档位（含下架）+ `stat {orders, users, amount}` |
| POST | `/wallet/packages` | 新建档位 |
| PUT | `/wallet/packages/:packageId` | 更新档位 |
| DELETE | `/wallet/packages/:packageId` | 删除档位（已下订单按快照履约，不受影响） |
| PUT | `/wallet/packages-order` | `{"ids":[3,1,2]}` 重写排序 |
| POST | `/wallet/adjust` | 新增 `bucket`：`cash` / `bonus` / 空 |

设置字段（金额都是字符串）：`rechargeEnabled`、`customAmountEnabled`、`rechargeMin`、`rechargeMax`、
`exchangeEnabled`、`exchangeMin`、`exchangeMax`（0 不限）、`exchangeDailyMax`（0 不限）、`notice`（≤500 字）。
默认：充值开、自定义金额 1–5000、兑换开、单笔至少 1、不限上限。

档位字段：`title`（≤60）、`amount`、`bonusAmount`、`bonusIntegral`、`badge`（≤20）、`description`（≤200）、
`perUserLimit`（0 不限，首充专享设 1；退款不让出名额）、`isActive`、`sortOrder`、`startsAt`、`endsAt`。

权限：读走 `app:wallet:read`；设置与档位写入走新权限 `app:wallet:config`；调账仍是 `app:wallet:adjust`。

## 4. 错误码

| 码 | 含义 |
|---|---|
| 40140 | 余额充值暂未开放 |
| 40141 | 自定义充值金额不在允许范围 |
| 40142 | 只能按档位充值 |
| 40143 | 充值档位不存在或已下架 |
| 40144 | 支付金额与档位价格不一致 |
| 40145 | 余额兑换积分暂未开放 |
| 40146 | 兑换金额低于单笔下限或高于单笔上限 |
| 40147 | 超出今日兑换额度 |
| 40148 | 可兑换的充值余额不足（赠送余额不能兑换） |
| 40149 | 赠送余额不足（管理员按 bonus 扣减） |
| 40151 | 兑换金额过小，不足 1 积分 |
| 40152 | 档位每人限购次数已满 |
