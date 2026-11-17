# 订单履约：付款后自动发放会员、余额、积分

## 起因（2026-11，元匣工具箱）

自动履约只认订单 `metadata.purpose` 与它的参数（`vip_purchase` + `vipPlanId` 等）。这个约定此前只写在
Voyage 客户端与 Kotlin SDK 里。应用 10004（元匣工具箱）接入爱发电时按直觉传参：

```jsonc
// 买永久会员
{ "subject": "永久会员", "amount": "9.99", "type": "vip", "metadata": { "planId": "19", "renew": true } }
// 充值
{ "subject": "余额充值", "amount": "10", "type": "wallet_recharge" }
```

网关一律放行：`type` 当成子类型记进 `provider_type`，`planId` 没人认，订单成了**普通商品单**。
付款、爱发电回查、结单都正常，唯独不发货，也不报任何错；控制台订单详情还把它显示成「无需履约」。
11-14 至 11-16 的已付订单全部如此，应用管理员只能手工补开会员（「补发订单」），
另建一条物品发放规则（规则 7「余额充值履约」）按订单金额往**赠送余额**里补钱。

## 现在的规则

### 1. 下单：用途归一化（`service/payment_order_intent.go`，词表在 `domain/payment/intent.go`）

用途按以下顺序取第一个能认的：

1. `metadata.purpose`
2. 请求顶层 `purpose`
3. 请求的 `type`（是用途词时；同时清空子类型，它从来不是付款方式）
4. 带了套餐 ID → 会员直购；带了档位 ID → 充值

| 规范值 | 也认的写法 |
|---|---|
| `vip_purchase` | `vip` `vip_plan` `membership` `member` `subscription` |
| `wallet_recharge` | `recharge` `wallet` `balance_recharge` `topup` `top_up` `deposit` |
| `integral_purchase` | `integral` `points` `points_purchase` |

大小写、连字符不讲究。套餐 ID 认 `vipPlanId` `planId` `plan_id` `vip_plan_id` 或请求顶层 `plan_id`；
档位 ID 认 `walletPackageId` `packageId` `package_id` `rechargePackageId` 或顶层 `package_id`。数字、数字字符串都行。

归一化后写回规范键（`purpose`、`vipPlanId`、`walletPackageId`），之后的价格校验与快照固化照旧
（金额对不上套餐价 `40089`、档位价 `40144`，充值受应用充值上下限约束）。
显式写了却不认识的 `purpose`、或顶层与 metadata 的用途矛盾，报 `40091`。

### 2. 下单：服务端定价

带了用途时 `amount`、`subject` 可以不填：会员取套餐价与套餐名，按档位充值取档位价（标题「余额充值：档位名」），
自选金额充值标题为「余额充值」。普通商品单仍必须给金额（`40072`）与标题（`40079`）。

```jsonc
// 推荐写法
POST /pay/orders  { "purpose": "vip_purchase", "plan_id": 19, "payment_method": "afdian" }
POST /pay/orders  { "purpose": "wallet_recharge", "package_id": 3 }
POST /pay/orders  { "purpose": "wallet_recharge", "amount": "10" }
```

Kotlin SDK：`createVipOrder(planId, paymentMethod, configName, type)`、`createRechargeOrder(amount, packageId, …)`。

### 3. 付款后：履约兜底（`service/payment_fulfillment.go`）

原有触发点：渠道回调（含主动查单）、用户查单。新增：

- **后台每 2 分钟**：7 天内付款、`metadata.purpose` 有值、未退款、未履约的订单重试一次（仓储层条件抢占，重复执行无害）。
- 没写 `purpose`、靠推断才看得出用途的**老订单不自动补**：管理员多半已经手工补过，自动补就是重复发放。

### 4. 订单的履约状况（每次读订单时推导，`Order.DeriveFulfillment`）

| `fulfillment_state` | 含义 |
|---|---|
| `not_required` | 未付款，或普通商品单 |
| `pending` | 付了款、写清了用途、还没发（系统自动重试） |
| `unrecognized` | 付了款、看得出要发东西，但下单时没写规范用途（老客户端），等管理员确认 |
| `done` | 已自动发放 |
| `manual` | 管理员确认已在别处补发（`fulfillment_status = manual`），平台不再发放 |

订单 JSON 新增 `fulfillment_status`、`fulfilled_at`、`purpose`（规范化或推断出的用途）、`fulfillment_state`。

### 5. 控制台：待履约

「商业 → 订单」新增筛选「待履约」（后端 `status=unfulfilled`：已付款、未退款、未履约、看得出要发东西），
列表与详情带红色「待履约」标记。详情里两个动作（`POST /api/admin/app/payment-config/orders/fulfill`，需 `payment:write`）：

| 动作 | 请求 | 效果 |
|---|---|---|
| 补发 | `action=fulfill`，先 `dry_run=true` 预览 | 按推断（或 `purpose` / `plan_id` / `package_id` 指定）的用途，用**当前**套餐 / 档位固化快照后发放。不校验价格（钱已按当时价格收了），现价不同、用户已是永久会员等情况作为提示返回 |
| 已在别处补发 | `action=manual`，可带 `note` | 标成 `manual`，移出「待履约」，不动余额与会员 |

两种动作都记一条回调日志（`admin_fulfilled` / `manual_fulfilled`），写明操作人与备注。
只能处理已付款、未退款、未履约的订单（否则 `40118`）；推断不出用途且没指定时 `40119`。

## 爱发电

- **留言兜底**：下单链接的留言写着「订单 P…」。付款人改掉链接参数、`custom_order_id` 丢了时，
  推送、查单、主动补单都从留言里认出订单号。认领别人的订单号等于替别人付钱，金额照样要对得上，不构成风险。
- **多个应用共用一个爱发电账号**：Webhook 是创作者级的一个地址，按订单号定位到各自应用的订单，本来就能共用。
  后台补单现在按账号（`user_id` + Token）合并成一轮查单，不再每个应用各翻一遍最近订单。

## 上线注意

- 元匣工具箱（10004）的规则 7「余额充值履约」在付款事件上按金额补余额。修复后充值订单会自动进余额本金，
  **规则 7 不停用就会重复入账**，部署时一并停用。
- 修复前那些已付款的老订单（10004 的会员与充值单）已经由管理员手工补过，在「待履约」里逐笔「已在别处补发」，不要补发。
