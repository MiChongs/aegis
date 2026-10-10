# 激励广告

用户看完一段激励视频，按「奖励场景」发放会员 / 积分 / 经验 / 抽奖次数。
目前接入的平台是**灰鲸**（huimiaokeji，底层为 ToBid / WindMill 聚合）。

## 一次观看怎么才算数

一次观看由两方各确认一次，两方靠平台下发的 `transId` 对上：

| 一方 | 证明什么 | 怎么鉴别 |
|---|---|---|
| 平台服务端回调 | 这次观看真的发生了 | `sign = sha256(securityKey + ":" + transId)` |
| 客户端上报 | 这次是为哪个场景、哪个账号看的 | 网关用户令牌 |

只信客户端等于谁都能伪造一次观看；只信回调则不知道是为哪个场景看的，也无法拒绝
「拿别人的 transId 来领奖」。因此默认的 **双方确认**（`dual`）模式下，两方都到齐才发奖。

| 校验模式 | 发奖条件 | 用途 |
|---|---|---|
| `dual`（默认） | 回调验签通过 **且** 客户端已上报 | 生产 |
| `server` | 回调验签通过即发；场景取客户端上报、回调 `extrainfo` 里的 `aegisScene`，或广告位对应的第一个场景 | 客户端无法上报时 |
| `client` | 客户端上报即发，不验签 | **仅联调** |

两方到达的先后不确定，平台还会重试回调（无响应时每 200ms 重试 3 次）、客户端会轮询上报。
所以只有一个结算入口 `Repository.RecordRewardedAdView`，所有请求都落在 `(appid, trans_id)` 这一行上：

1. 先到的一方建行（`INSERT … ON CONFLICT DO NOTHING`），随后 `SELECT … FOR UPDATE` 锁行；
2. 每一方只补自己那一半的事实（回调：验签时间、广告位、奖励名、extra；上报：场景、设备、IP），从不覆盖另一方；
3. 条件满足时锁用户行、判日限额与冷却、同事务发奖；状态只有一次跃迁 `pending → granted / rejected`。

日限额与冷却在**锁住用户行之后**判定，同一用户的两次结算因此串行，不会被并发穿透。
`UNIQUE (appid, trans_id)` 是「一次观看只发一次」的最后一道保证。

## 用户标识

`/ads/rewarded` 下发的 `userId` 形如 `42.1a2b3c4d5e6f7a8b`：用户 ID + HMAC 签名（密钥派生自 `SECURITY_MASTER_KEY`）。
客户端必须把它**原样**传给 SDK 的 `HJRewardAdRequest(placementId, userId, options)`。

平台的回调签名只覆盖 `transId`、不覆盖 `userId`。不带签名的话，能伪造一次回调的人就能把奖励记到任意账号头上；
带上签名后，回调里的 `userId` 只能是我们发出去的那一个。认不出的用户记为 `user_not_found`，不发奖。

## 接口

### 平台回调（服务端命名空间，无令牌）

```
GET /api/apps/{appKey}/ads/huijing/callback?userId=…&transId=…&sign=…&placementId=…&rewardAmount=…&rewardName=…&extrainfo=…
→ {"isValid": true | false}
```

参数名与灰鲸后台「配置回调URL」的默认模板一致；`user_id` / `USER_ID` 等写法也认。
`isValid` 回答「这次观看算不算数」：验签不过、应用没开、用户认不出、被限额挡下都是 `false`；
`dual` 模式下等客户端上报时是 `true`。响应不套本项目的信封 —— 对方只认它自己的格式。

它不在网关 `/api/v1/apps/*` 下：平台服务器没有用户令牌，也无法做 signed / sealed 包装。

### 客户端（App Protocol v1，需登录）

| 目录 key | 方法 | 路径 | 说明 |
|---|---|---|---|
| `rewardedAdStatus` | GET | `/ads/rewarded` | 是否启用、各场景剩余次数与冷却、传给 SDK 的 `userId` |
| `rewardedAdClaim` | POST | `/ads/rewarded/claim` | 上报 `{scene, transId, placementId?, verified?, errorCode?, deviceId?}` |
| `rewardedAdRecords` | GET | `/ads/rewarded/records` | 我的观看记录 |

上报的返回体 `status`：`granted` 已到账；`rejected` 看了但不发（`reason`：`daily_limit` / `scene_daily_limit` /
`cooldown` / `scene_unavailable` / `user_mismatch` / `client_unverified` / `user_not_found` / `disabled`）；
`pending` 还在等平台回调 —— 用同一个 `transId` 再上报一次即可轮询，奖励只会发一次。

只有「这次上报本身不成立」才是错误码：`40350` 未启用、`40351` 场景不存在或已停用、`40352` 这次观看属于另一个账号。

### 管理端（`app:read` / `app:write`，落在应用级兜底规则里）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / PUT | `/api/admin/apps/{appKey}/rewarded-ads/config` | 配置；GET 附带权益目录与回调地址，Security Key 只回「配没配」 |
| GET | `/api/admin/apps/{appKey}/rewarded-ads/views` | 观看记录（status / scene / keyword / start / end / userId） |
| POST | `/api/admin/apps/{appKey}/rewarded-ads/views/{viewId}/grant` | 补发：不等另一方、不看限额，一条只能补发一次（`40930`） |
| GET | `/api/admin/apps/{appKey}/rewarded-ads/stats?days=14` | 概览与按天趋势（缺失日期补零） |

## 配置

存放在 `app_rewarded_ad_configs`（一应用一行），场景以 JSONB 整体保存。Security Key 以 AES-GCM 密文落库，
派生自 `SECURITY_MASTER_KEY`，留空表示不修改。开启开关前会挡下「开了也发不出去」的配置：
没有启用中的场景，或非 `client` 模式却没有 Security Key。

权益复用卡密的权益目录与数据形态，但只开放 `vip_plan` / `vip_days` / `integral` / `experience` / `lottery_draws`
（余额是真钱、设备位挂在授权卡上，都不该由广告发）。目录与发放分支由 `TestRewardedAdCatalogHasGrantBranch` 双向钉死。
会员发放走同一套账本，渠道为 `ad_reward`。`vip_plan` 不能选永久套餐：广告可以每天反复看，
永久会员看一次就到头了，之后每次观看只是白发套餐附赠的积分。

## 看广告赠送的会员权益

每个场景有一份**会员权益**（`scene.membership`），与套餐一样**跟随当前配置**：改了它，已经领到、仍在期内的会员随即按新的权益算。

| 字段 | 作用 |
|---|---|
| `features` | 「会员天数」这一档带哪些功能标识（必须是本应用功能目录里的）。「会员套餐」那一档的功能跟随所选套餐，不看这里；不送会员天数却配了功能的场景保存时报错 |
| `adFree` | 这个场景送出的会员（两档都算）是否免广告：免除广告服务的同意要求、不展示开屏广告。不填按免广告，与这项配置出现之前一致 |

判定时，看广告领的会员段（渠道 `ad_reward`）按开通记录 metadata 里的 `scene` 找到场景、取它现在的会员权益
（`vip.Segment.AdTerms`）；场景被删除后回落到开通那一刻的快照（`vip_transactions.features` 与 `metadata.adFree`）。
会员是否免广告（`Entitlement.adFree`）取仍生效各段的「或」：只要有一段免广告就免 —— 付费、试用、兑换、管理员发放的会员一律免广告，
所以只有全部会员期都来自「不免广告」的场景时才是 false。`/vip/status`、服务端会员校验与广告服务策略都读这一个结论，
广告服务的会员免除看的是它而不是「是不是会员」，否则「看一次广告换一天不看广告」会让同意要求形同虚设。

客户端的场景列表带 `membership`，`rewardSummary` 也会补上一句会员权益（如「会员天数 1 天（会员可用 AI 对话，会员期间免广告）」，
只列启用中的功能）。

## 灰鲸后台怎么配

1. 【流量管理】→【广告位管理】→ 激励视频广告位 →【配置回调URL】；
2. 打开「服务器回调」，回调地址填控制台「激励广告 → 配置 → 回调地址」里的那一条，参数沿用默认模板；
3. Security Key 填一串随机值，并在控制台同一处填入相同的值；
4. 控制台里把场景的「广告位 ID」填成该广告位的 ID。
