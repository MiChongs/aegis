# 抽奖

应用级抽奖活动：九宫格（8 个奖品）或转盘（2–12 个），每人每天的免费次数、看激励视频换次数、花积分抽，
以及「连续 N 次没抽中必中某个奖品」的保底。2026-10 重构（迁移 `000110_lottery_rework`），取代旧版的
报名 / 种子承诺 / 上链那一套。

## 结果只在服务端产生

客户端只说「用哪一种次数抽一次」：

```
POST /api/v1/apps/{appKey}/lottery/activities/{activityId}/draw
{"source": "free" | "bonus" | "points" | "", "requestId": "<uuid>"}
```

`source` 是「最多用到这一档」，不是「必须用这一档」，见[扣除顺序](#扣除顺序)。

抽中哪一格、发什么、发多少，全部在**同一个数据库事务**里决定并到账（`Repository.DrawLottery`）：

1. 读活动（`FOR SHARE`，与「改奖盘」互斥）、判状态与时间窗；
2. 锁用户行 —— 今天用了几次、次数余额、积分余额、连续未中次数都是这个人的事实，同一个人的抽奖从这里开始串行；
3. `requestId` 回放：同一个键再来一次，原样返回第一次的记录（`replayed: true`）；
4. 判定用哪一种次数（`lottery.EvaluateChances` / `ResolveSource`，与活动页显示的剩余次数同一个函数）；
5. 扣次数：bonus 按[使用顺序](#抽奖次数)取一批、记一条流水，与汇总行同一把锁；积分在落记录之后按记录 ID 记账；
6. 选奖品：按[出奖策略](#出奖策略)算出这个人这一次每个奖品的最终概率；到了保底且保底奖有资格时直接给它，
   否则按最终概率抽（gonum `sampleuv` 加权抽样，随机源是每次用 `crypto/rand` 播种的 ChaCha8）；
7. 占名额：设了全站每日限量的先占当天名额（`lottery_prize_daily` 条件 UPSERT），限量奖品再扣库存（`used < quantity`），
   任何一步恰好被别人抢光时改发兜底奖；
8. 落记录（奖品存快照）→ 发奖（与积分商城、邀请同一套账本）→ 更新连续未中次数。

任何一步失败整笔回滚：免费次数、bonus、积分都没有花出去。不存在「客户端上报抽中了 X，服务端照着发」这条路。

## 三种次数

| 来源 | 是什么 | 限制 |
|---|---|---|
| `free` | 每人每天的免费次数（活动的 `freeDaily`） | 按应用时区的自然日 |
| `bonus` | 应用级的抽奖次数，分批记账（见[抽奖次数](#抽奖次数)），任意活动可用 | 余额 |
| `points` | 每次消耗 `pointsCost` 积分 | `pointsDaily`（0 不限）与积分余额 |

### 扣除顺序

从头到尾只有一条，单抽、十连抽、视频抽奖之后的抽奖、管理员扣减都一样（`lottery.ConsumptionOrder`）：

```
今日免费次数 → 赠送的抽奖次数（先到先用）→ 兑换的抽奖次数（先到先用）→ 积分
```

活动页的 `chances.next` 就是这个顺序的结论；`chances.tickets` 把 `bonus` 拆成 `gift`（赠送的）与 `purchased`（兑换的）。

客户端指定 `source` 时，`ResolveSource` 按这个顺序走到它为止、取第一个可用的：活动页可能是零点前、签到前拉的，
按缓存的 `next` 传上来的 `bonus` / `points` 不会跳过已经可用的免费次数（实际用的写在 `draw.source` 与 `draw.ticket`）。
走到指定的那一档仍不可用时报那一档自己的错误：指定 `free` 而免费已用完时报 `42913`，不会越级去用次数或积分。

### 视频抽奖

视频抽奖不是第四种来源。活动的 `videoScene` 指向一个**激励广告场景**，那个场景发的是「抽奖次数」：

1. 活动页的 `chances.video` 给出场景、广告位、签名用户标识（与 `/ads/rewarded` 的 `userId` 相同）、今天还能看几次、冷却；
2. 客户端按激励广告的流程加载、播放、上报（见 [激励广告](rewarded-ads.md)）—— 看没看完由 Apex 的**服务端回调**说了算；
3. 结算后次数进 bonus（一批来源为 `rewarded_ad` 的赠送次数），客户端再点抽奖（`source` 留空），按扣除顺序用掉。

因此视频抽奖的每日次数、冷却都在激励广告的场景里配；保存活动时会核对场景存在且发「抽奖次数」。
广告看完但回调还没到时（`pending`），次数会在回调到达的那一刻补发，客户端下次进页面就能看到。

## 抽奖次数

`bonus` 是应用级的、任意活动可用。每一次发放是一批（`lottery_draw_lots`：来源、是否兑换所得 `paid`、数量、剩余、来源单号），
每一次增减一条流水（`lottery_draw_ledger`：`grant` 获得 / `consume` 抽奖使用 / `deduct` 系统扣减，带变动后余额）；
`lottery_draw_grants` 是这个人全部批次的汇总，发放与扣减都先锁它这一行。抽奖记录的 `ticket_lot_id` / `ticket_origin` / `ticket_paid`
记下这一次用的是哪一批。

| 来源 `origin` | 发放处 | 档 |
|---|---|---|
| `rewarded_ad` | 激励广告场景的「抽奖次数」（视频抽奖） | 赠送 |
| `lottery` | 抽中的「抽奖次数」、已拥有奖品改发的次数 | 赠送 |
| `invite` / `leaderboard` | 邀请奖励、排行榜结算 | 赠送 |
| `cardkey` | 卡密：一卡一用（卖给一个人的码）算兑换，活动码（`maxUses > 1`）算赠送 | 兑换 / 赠送 |
| `points_store` | 积分商城：花了积分的算兑换，0 积分的福利商品算赠送 | 兑换 / 赠送 |
| `admin` | 管理员发放（可选按兑换所得记） | 赠送 / 兑换 |
| `legacy` | 分批记账之前的余额（000136 与事务内对齐补的一批） | 赠送 |

使用顺序 `paid ASC, id ASC`：赠送的先于兑换的，同一档先到先用。用户付过代价的部分留到最后，对账、撤销兑换时它们还在。

对齐（`lotteryLockTicketsTx`）：汇总比批次多（分批记账之前的余额、滚动发布期间旧版本发的）时补一批 `legacy`；
汇总比批次少（旧版本直接扣了汇总）时按使用顺序从批次里扣掉差额。对齐之后两边恒等。

用户端 `GET /lottery/tickets?page=&limit=`：`account`（`balance`、`tickets.gift` / `tickets.purchased`、累计、还有剩余的批次 `lots`
按使用顺序、`order` 扣除顺序文案）与流水 `ledger`（不回显操作人）。管理端见[接口](#管理端appread--appwrite)。

## 人群特权

活动可以配最多 10 条 `perks`（迁移 `000130`）：**谁**（人群 + 今天的条件）得到**什么**（效果）。特权只改写
「这个人此刻眼里的活动」（`PerkEffects.Personalize`），不新增次数来源、不改客户端契约，所以**老版本 App 不升级就生效** ——
它本来就照着服务端给的 `chances.next` 抽、照着 `video.remaining` 拦视频。活动页与抽奖事务调同一套函数（`perks.go`）。

| 字段 | 含义 |
|---|---|
| `audience` | `all` 全部 / `member` 会员（含永久）/ `lifetime` 永久会员 / `non_member` / `non_lifetime` / `ad_free` 免广告时长有效期内 |
| `signedToday` | 今天签到之后才生效 |
| `onlineMinutes` | 今天在线满 N 分钟之后才生效（`user_online_daily`，实时连接心跳累计） |
| `when` | 附加条件（expr，变量：画像的全部 `user.*`、`user.adFree`、`user.onlineMinutesToday`、`user.totalDraws`、`draw.hour` / `draw.weekday`），保存时编译 |
| `extraFree` | 每天多几次免费抽奖 |
| `adSkip` / `adSkipSlots` | 视频抽奖**免看广告**的概率（%）。今天的第 k 次视频抽奖（k = 1…场景每日上限；不限次时 1…`adSkipSlots`，默认 5）各自独立判定，**按顺序逐次解锁**（见下） |
| `pointsDiscount` / `extraPoints` | 积分抽奖省 N%（向上取整，至少 1 积分）/ 每天多几次（活动设了每日上限时） |
| `pityCut` | 保底提前 N 次（最少保留 2 次） |
| `winBoost` | 中奖奖品的权重 × 系数（1–3），公示概率随之重算 |

多条同时满足时叠加：次数相加（免费合计不超过 100），免看广告的概率按独立事件合并 1 − Π(1 − p)，折扣取最大，
保底提前相加，中奖加成相乘（封顶 3 倍）。

**免看广告按顺序逐次解锁**（`WalkAdSkip`）：第 k 次视频抽奖**轮到时**才看它免不免 —— 免看的那一次作为 1 次免费给出
（`perks.nextWaived`），用掉之后才轮到第 k+1 次；不免的那一次要看完广告才往下走。任何时刻最多只解锁「当前这一次」。
进度由今天看完并发放的视频次数（场景的 `todayCount`）与已用掉的免看次数（免费次数里超出「基础 + 额外免费」的部分）推出，
不另存状态。2026-10-26 修过一次：最初的实现一早就把全天判中的次数一次性并进免费次数（46 次视频 × 25% → 开页就多 8 次）。

**判定是确定的**：SHA-256(活动, 用户, 当天零点, k) 映射到 [0, 1) 与 p 比较。同一个人同一天刷新多少次、
抽奖事务里再算一次都是同一个结论；用户改不了输入，提前算出结论也换不来更多次数。
已解锁的免看次数从 `video.remaining` 里扣掉（App 据此拦视频按钮），激励广告场景自己的每日上限不变 ——
绕过 App 直接调激励广告接口的人最多多拿当天解锁的那几次。

客户端看到的：

- `chances.free.daily` / `remaining` 变大，`chances.video.remaining` 变小（判中的视频次数）、`points.cost` / `daily`、`pity.threshold` / `remaining` 按特权改写；
- 新字段 `perks`：`names`、`lines`（一项一句）、`waived`（今天已解锁的免看次数）、`nextWaived`（下一次视频抽奖免看）、`message`；
- **老客户端兼容**：顶部提示卡 `lifetime` 并入特权说明（`policy: "perk"`，原处理方式移到 `ownedPolicy`）——老 App 遇到不认识的 policy 时原样显示 `message`。

每次抽奖生效的特权写进 `trace.perks`（管理端「方案」里显示）。模拟接口按 `userId` 的真实身份，
或按 `perk`（`{member, lifetime, adFree, signedToday, onlineMinutesToday}`）给虚拟用户一个身份，保底提前与中奖加成计入模拟，结果多 `perks`。

```json
"perks": [
  {"name": "永久会员特权", "audience": "lifetime", "adSkip": 100, "extraFree": 1, "pointsDiscount": 20},
  {"name": "会员概率免广告", "audience": "member", "adSkip": 30},
  {"name": "签到送次数", "audience": "all", "signedToday": true, "extraFree": 1},
  {"name": "晚间加成", "audience": "all", "when": "draw.hour >= 20", "winBoost": 1.2}
]
```

## 保底

`pityThreshold = N`、`pityPrizeId` 指向一个不是「谢谢参与」的奖品：连续 N−1 次没抽中（谢谢参与），
第 N 次必中保底奖（记录上 `pity: true`）。抽中任何奖品都会清零。活动页的 `pity.remaining` 是「最多再抽几次必中」，
保底奖抽光后不再返回 `pity`。连续未中次数存在 `lottery_user_states`，按人按活动。

## 出奖策略

旧版只有「固定权重随机 + 硬保底」。现在活动可以配一份 `strategy`（迁移 `000124`），在基础权重上逐个维度乘系数，
算出**这个人、这一次**的最终概率。全部不配、奖品不设限量时与旧版完全相同。

| 维度 | 配置 | 作用 |
|---|---|---|
| 硬性资格 | 奖品的 `dailyLimit` / `userLimit` / `userDailyLimit`，库存 | 全站每天最多出几个、每人最多中几次、每人每天最多中几次；不满足的这一次权重为 0（`blocked`） |
| 风控规则 | `risk.rules[]` | 见下节「风控规则」：信号比阈值，命中后降权、停发稀有、不触发保底、只发兜底或拒绝抽奖 |
| 库存匀速释放 | `pacing.slack`（提前量，库存比例）/ `pacing.maxBoost` | 限量奖品按活动时间进度放量：已放量 = 库存 ×（进度 + 提前量）。超前时在提前量内线性降到 0，落后超过一个提前量时放大到 `maxBoost` 倍。需要结束时间 |
| 积分预算 | `budget.dailyPoints` | 本活动当天发出的积分（含折算）过半后，积分奖按剩余比例线性收紧；单个奖品超过剩余预算时不出。并发下是软上限 |
| 行为画像 | `behavior` | 见下节 |
| 中奖冷却 | `cooldown.draws` / `cooldown.factor` | 这个人最近 N 次里中过的奖品，权重 × factor |
| 自定义规则 | `rules[]` | expr 表达式为真时，给命中的奖品（类型 / 位置）乘系数，0 等于这一次剔除 |
| 软保底 | `softPity.start` / `softPity.step` | 连续未中达到 start 次起，每多一次未中，中奖总概率抬高 step 个百分点（中奖奖品之间比例不变，只缩小未中奖） |
| 硬保底 | `pityThreshold`（原有） | 到点直接给保底奖；保底奖这一次被硬性资格挡掉时退回常规抽取 |

每一次的完整方案（各奖品的基础权重、每个维度的系数与原因、最终概率、行为分明细、风控逐条结果、是否保底 / 改发兜底及原因）
写进 `lottery_draws.trace`，管理端记录里点「方案」可看；客户端接口一律不返回。

`trace.floor` 是这一次的兜底奖；`trace.fallbackReason` 说明为什么改发兜底：`empty`（所有奖品最终权重都是 0）、
`contended`（抽中的限量奖品占库存时被并发抢光）。

### 风控规则

旧版风控只有「同设备 / 同 IP 账号数超过上限 → 只出未中奖」两项，trace 里只有一个 `risk: true`：奖盘没有「谢谢参与」时，
管理端看到每个奖品都是「设备聚集 0」、概率全是 0%，却又抽中了兜底奖，看不出几个账号、上限多少。

现在 `strategy.risk.rules[]` 是一组规则（`risk.go`，迁移 `000145`），每条 = 信号 比较 阈值 → 处置：

```json
"risk": {"rules": [
  {"name": "同设备多账号", "signal": "deviceAccounts", "op": "gt", "threshold": 3, "action": "floor"},
  {"signal": "subnetAccounts", "op": "gt", "threshold": 12, "action": "damp", "factor": 0.5},
  {"signal": "accountAgeHours", "op": "lt", "threshold": 24, "action": "rare", "rareBelow": 0.01, "kinds": ["vip_plan"]},
  {"signal": "noDevice", "op": "gte", "threshold": 1, "action": "deny"}
]}
```

| 信号 | 含义 |
|---|---|
| `deviceAccounts` / `ipAccounts` | 本活动今天同设备 / 同 IP 抽过奖的账号数（含本人） |
| `subnetAccounts` | 同网段（IPv4 /24、IPv6 /64）今天的账号数，挡换 IP 不换网段的批量号 |
| `deviceHistory` | 这台设备在应用里登录过的账号数（`user_login_devices`） |
| `accountDevices` | 这个账号登录过的设备数 |
| `noDevice` | 请求没带设备号（0 / 1） |
| `accountAgeHours` | 注册时长（小时） |
| `riskScore` | 风控中心近 30 天给这个账号的最高分 |
| `burst10m` / `todayDraws` / `winsToday` | 近 10 分钟抽奖次数 / 今天抽奖次数 / 今天中奖次数（本活动，不含这一次） |

处置从轻到重：`damp` 降权（中奖奖品权重 × factor，多条相乘，可限奖品类型）→ `noPity` 不触发保底（硬保底、软保底、
伪随机都按未连败算）→ `rare` 停发稀有（公示概率低于 `rareBelow` 的中奖奖品不出，可限类型）→ `floor` 只发兜底
（兜底奖以外的中奖奖品不出；兜底奖照常可出，它本是纯保底格、权重 0 时这一次按 1 算，方案里显示 100%）→
`deny` 拒绝抽奖（事务回滚、次数与积分不扣，用户收到 `40348`，另记一条 `lottery_risk_events`）。兜底奖不受降权与停发影响。

取不到的信号（请求没有设备号时数不了同设备账号）记 `missing`、按未命中处理。旧字段 `deviceAccounts` / `ipAccounts`
读入时转成两条 `floor` 规则，已保存的活动行为不变。

每条规则的取值、阈值、是否命中写进 `trace.riskReport.checks`；命中的信号与最重的处置另写 `lottery_draws.risk_rules` /
`risk_action`（GIN 与部分索引），按风控筛记录不用解 JSON。迁移前命中风控的记录回填为 `legacyCluster` / `floor`。

`GET /lottery/activities/{id}/risk?days=7` 是风控总览：抽奖与命中人次、按处置与信号的命中 / 拒绝、按天趋势、
设备 / IP / 网段聚集排行（2 个账号以上）、最近被拒绝的请求、当前生效的规则。八组聚合互不依赖，在各自的连接上并行查（errgroup）。

### 概率控制层

各维度系数是连乘的，叠多了中奖率与发出去的价值都会失控。控制层（`strategy_control.go`，迁移 `000125`）在系数之后、
抽样之前按顺序把「这一次」的分布整体校正，每一步都有明确的数学含义：

| 步骤 | 配置 | 原理 |
|---|---|---|
| 运气平衡 | `luck.strength` | 每次抽奖是成功率不同的伯努利试验，中奖次数服从**泊松二项分布**。逐次累加均值 Σp、方差 Σp(1−p)、三阶矩 Σp(1−p)(1−2p)（`lottery_user_states.luck_*`），用带偏度修正的**精细正态近似（RNA）**求 mid-p 分位，再取 Φ⁻¹ 得 z。倒霉（z < 0）的在 logit 上加 −strength·z，走运的减，截断 ±3σ。期望按**控制层之前的公平中奖率**（`trace.fair`）累计，否则被补偿的人期望跟着涨，会来回过冲 |
| 波动模式 | `variance: "prd"` | **伪随机分布**：距上次中奖第 n 次的中奖率为 min(1, n·C)，C 由当前中奖率 p 二分反解（使 1/E[次数] = p）。长期中奖率不变，连败与连中都大幅减少；与软保底二选一 |
| 全站反馈 | `winRate.target` / `gain` / `prior` | 今日全站中奖率用 **Beta–二项共轭后验**估计：先验 Beta(κ·目标, κ·(1−目标))，后验均值 (k + κ·目标)/(n + κ)；样本少时收缩到目标、不乱调。校正量 = gain ×（logit(目标) − logit(后验)） |
| 积分节奏 | `budget.paced` | 允许已发 = 预算 ×（当天时间进度 + 5%），系数 exp(−3(已发/允许 − 1)) 截在 [0.05, 2]：超前收紧、落后放宽 |
| 护栏 | `guard.minWinRate` / `maxWinRate` / `maxValue` / `kindCaps` | 中奖率区间、每次期望价值（积分当量）上限、按奖品类型的单次占比上限，全部是线性不等式。取满足约束且与当前分布 **KL 散度最小**的分布（**I-投影**，最小鉴别信息原理）：解为指数族 p_i ∝ q_i·exp(−λ·a_i)，在凹对偶函数上做带有效集的**投影牛顿法**，KKT 条件满足即停；约束在可发奖品上不可行时 `trace.guardInfeasible` |

奖品的积分当量（`prize.value`，库列 `points_value`）：0 为按类型估算（积分按数量；经验 × `guard.experienceRate`（默认 0.1）；
抽奖次数 × `guard.drawValue`（默认积分抽奖价，再默认 10）；会员类奖品与已拥有时折算的积分同价（默认折算策略 + 活动单价）；
会员套餐按 30 天估）。人工发放与会员套餐建议显式设置。`trace` 多 `fair`（控制前中奖率）、`value`（这一次的期望价值）、`luck`（z）、`prd`。

同一奖盘 2000 人 × 60 次的模拟（`strategy_control_test.go`）：独立抽取最长连败 P99 为 13 次、每人中奖次数 P10–P90 为 21–31；
PRD 把连败 P99 压到 4、长期中奖率不变；运气平衡把中奖次数区间收窄到 23–29；两者叠加 24–28。

### 每日运气

`fortune.spread`（σ，0–1，`fortune.go`）：运气平衡让人人趋同，每日运气反过来让不同的人、同一个人的不同日子手气不一样。
每人每天一个系数 exp(σz − σ²/2)，z = Φ⁻¹(哈希(活动, 用户, 日期))，截在 ±2；系数服从对数正态，所有人平均约为 1。
系数乘在兜底奖、谢谢参与以外的奖品权重上（真实奖品之间的相对比例不变），同一天里不变、过零点换一个。
σ = 0.3 时系数在 0.52–1.74 之间，最走运的人真实奖品的权重是最倒霉的 3.3 倍。

它在个人奖盘上生效，与人群特权的中奖加成同一层：活动页的公示概率是这个人今天的概率，无套利比例也按乘过之后的奖盘算。
控制台积分账另外按运气最好的系数算一遍永久会员，那天会被缩减时给出警告。抽奖记录的 `trace.fortune` 是当次的系数；
模拟虚拟用户时每人各抽一个系数（结果里的 `fortune` 是分布），指定真实用户时用他今天的系数。

活动页的 `fortune`（开了每日运气时才有，`ReadFortune`）把这个系数讲给用户：`factor`（与出奖用的是同一个）、
`percentile`（今天手气不如他的人占多少，1–99）、`level` 与 `title`（z ≥ 1.5 `burst` 运气大爆发约 7%，≥ 0.5 `lucky` 鸿运当头约 24%，
> −0.5 `steady` 运势平稳约 38%，其余 `quiet` 手气平淡）、`low` / `high`（系数范围）、`winRate` / `baseWinRate`
（抽中兜底奖以外奖品的概率，乘运气之后与之前）、`resetsAt`（次日零点）。每个奖品另有 `baseProbability`：活动公示的原概率
（只去掉对他不参与抽取的已拥有奖品，不乘中奖加成与运气），与 `probability` 一比就是他今天比公示高还是低。

### 运气控制

管理员可以干预每日运气（`fortune_control.go`，表 `lottery_fortune_controls`，迁移 000135）。干预按人（每人一条）或全站，
带生效时间窗（默认从现在到明天零点），可撤销，全部留痕（操作人、备注、撤销人）。三种干预都作用在 z 上，最后仍截在 ±2，
所以系数永远在 `FortuneRange` 之内，积分账与无套利的最坏情况照旧成立：

| kind | 作用 |
|---|---|
| `reroll` | 重抽：哈希里加一段随机盐，换一个新的 z（分布不变，只是换了手气）。全站重抽就是所有人今天重新摇一次 |
| `pin` | 定档：z 定在 `level`（运气大爆发 1.75 / 鸿运当头 1 / 运势平稳 0 / 手气平淡 −1），或按 `factor` 用当前差异反算 |
| `shift` | 偏移：z 加减 `shift`（±4 以内），全站 +0.5 就是整体走运一点 |

合成顺序：全站与本人的 reroll 依次加盐算出 z → 有 pin 时取 pin（个人优先于全站，同层取最新）→ 没有 pin 时加上全部 shift →
截在 ±2。活动页（`ReadFortuneControlled`）、抽奖事务（`FortuneFactor`，在事务连接里取干预）与单人模拟都走 `ResolveFortune`，
用户看到的与抽到的是同一个系数；用户端看不到有没有被干预。

管理端接口（`/api/admin/apps/{appKey}/lottery/activities/{id}/fortune…`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/fortune` | 总览：是否开启、差异、范围、各档位（定档 z、系数、理论占比）、今天抽过奖的人（最多 5000）干预前后的档位分布与平均系数、全站干预、干预数 |
| PUT | `/fortune/settings` | `{spread}` 调整运气差异（0 关闭），只改策略里这一项，其余原样过保存校验 |
| GET | `/fortune/users/{userId}` | 一个人此刻的运气：干预前后的读数与 z、生效的干预、他名下的干预 |
| GET | `/fortune/controls` | 干预记录（`userId` / `kind` / `status`：active / scheduled / expired / revoked） |
| POST | `/fortune/controls` | 下干预：`{userIds | allUsers, kind, level | factor | shift, startsAt?, endsAt?, note}`；没开每日运气、跨应用用户都会被拒 |
| POST | `/fortune/controls/preview` | 预演：同样的请求体，按今天抽过奖的人（或所选用户）算干预之后的分布，不落库 |
| POST | `/fortune/controls/revoke` | `{ids}` 撤销几条，`{all: true}` 撤销全部生效中与待生效的（恢复自然运气） |

### 至尊加成

永久会员（或指定会员档位）的每日运气在自然运气之上再抬一截（`supreme.go`）。自然运气的系数 f 先按上面的规则算出（计入管理员干预），
再按下式加成，所以干预作用在加成之前：

    f' = max(f, min(max(f × 加成幅度, 保底运气), 上限))

| 参数 | 范围 | 作用 |
|---|---|---|
| `boost` 加成幅度 | 1–3（0 为不乘） | 系数乘多少 |
| `floor` 保底运气 | 0–5（0 为不设） | 加成之后至少多少；1 即「至少平均水平」 |
| `cap` 上限 | 1–5（0 按 5 封顶） | 加成之后最多多少；自然运气已经高过上限的那天按自然运气算，加成不会让运气变差 |

f' 关于 f 单调不减，所以拿到加成的人的系数范围就是把自然范围两端各套一次（`SupremeRange`），活动页的 `low` / `high` 换成这个范围。
生效点与每日运气完全相同（`FortunePrizes`：兜底奖与谢谢参与以外的奖品权重 × f'），公示概率、中奖率护栏、无套利比例都按乘过之后的
个人奖盘算，概率照常归一。控制台积分账检查「永久会员最走运那天」时用 `SupremeHigh`（所有可能落到永久会员身上的参数组取最高）。

**谁能得到**（判据只用 `vip.Holdings`，与会员判定同一份事实）：

- `scope: "lifetime"`：只对永久会员（`Holdings.Lifetime`），用顶层的 `boost` / `floor` / `cap`。
- `scope: "tiers"`：按会员档位分级（`Holdings.Tiers`：此刻仍生效的会员期所属档位，同一档位里有永久开通的记为永久）。
  `tiers[]` 每档 `{tierId, boost, floor, cap, lifetimeOnly}`，`lifetimeOnly` 只认这个档位里的永久开通。同时落在几档时取对他今天最好的
  那一档；没落在任何一档的永久会员，顶层参数有效果时按顶层参数算。

**配置两层**：应用的默认（表 `lottery_app_settings.supreme`，迁移 000142）与活动策略里的 `strategy.fortune.supreme`。
活动里不写时跟随应用，写了以活动为准（`{enabled: false}` 即这个活动不加成）。只有开了每日运气的活动才有加成；关掉运气差异
（`spread = 0`）会连活动自己的加成设置一起去掉，`PUT /fortune/settings` 只改差异时保留它。`title` 是展示给用户的名称，默认「至尊加成」。

**用户端**：活动页 `fortune` 的 `factor` / `level` / `title` / `percentile` / `low` / `high` / `winRate` 都是加成之后的（老客户端不升级
也照着新数字显示），另有 `fortune.supreme`（拿到加成时才有）：

| 字段 | 说明 |
|---|---|
| `title` / `tier` | 加成名称；按档位分级时落在的档位名 |
| `baseFactor` / `baseLevel` / `baseTitle` / `basePercentile` | 加成之前的运气 |
| `boost` / `floor` / `cap` | 生效的参数 |
| `lift` | 加成之后是之前的几倍（1 为自然运气已高过上限） |
| `baseWinRate` | 加成之前（只乘自然运气）抽中兜底奖以外奖品的概率（百分比） |
| `message` | 一句话说明，如「永久会员至尊加成：今日运气由 ×0.85 提升至 ×1.28」 |

抽奖记录的 `trace.fortune` 是加成之后的系数，`trace.fortuneBase` 与 `trace.supreme` 是加成之前的系数与生效的参数组。

**管理端**：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/apps/{appKey}/lottery/settings` | 应用设置：`supreme`、可选的会员档位（`tiers`，含会员数）、开了每日运气的活动各自的来源（`activities[].source`：activity / app） |
| PUT | `/api/admin/apps/{appKey}/lottery/settings` | `{supreme}` 整体替换；引用的档位必须属于这个应用 |

运气总览（`GET /fortune`）多 `supreme`（实际生效的配置）、`supremeSource`、`supremeHigh`；今日分布不含加成。单人读数
（`GET /fortune/users/{uid}`）的 `current` 计入加成，另给 `lifetime` 与 `tiers`。模拟（`POST /lottery/simulate`）有生效的配置时
返回 `supreme`：`applied`（这个人或虚拟用户拿到没有）、`grades`、指定真实用户时的 `baseFactor` / `factor`、`baseline`（同一批随机数、
不加成的完整模拟结果）；虚拟用户按 `perk.lifetime` 与 `perk.tiers` 判定，每人按自己抽到的自然运气取最好的一组，`result.fortuneBase`
是加成之前的系数分布。MCP 工具 `lottery_settings` / `lottery_save_settings`。

### 精确分析与审计

- **首中分布**（`markov.go`）：把「已连续未中 k 次」看成马尔可夫链的状态，每个状态的中奖率 h_k（风险函数）由同一套
  `PlanDraw` / `PityChoice` 给出，P(T > n) = Π(1 − h_k)，E[T]、方差、分位数都是精确值，尾部按几何分布补齐；
  由**更新报酬定理**，长期中奖率 = 1/E[T]。模拟接口返回 `analytic`，与蒙特卡洛结果互相印证（测试里两者偏差 < 1%）。
- **出奖审计**（`audit.go`，`GET /lottery/activities/{id}/audit?days=14`）：每次的概率都不同，普通卡方检验不适用。
  第 t 次是参数 p_t 的单次多项试验，合计 O 的期望 E = Σp_t、协方差 Σ = Σ(diag(p_t) − p_t p_tᵀ)；由多元中心极限定理
  T = (O − E)ᵀ Σ⁺ (O − E) → χ²(rank Σ)，Σ⁺ 用 SVD 求伪逆。另对每个奖品做泊松二项 z 检验，**Holm–Bonferroni** 校正，α = 0.01。
  硬保底与改发兜底的那几次不计入。用来证明线上出奖确实服从算法给出的概率（随机源、实现、数据都没出问题）。

### 行为画像

抽奖前（事务外，一条语句）取这个人在应用里的全部行为，按活动配置的权重加权成 0–1 的行为分：

| 信号 | 来源 | 映射 |
|---|---|---|
| 注册时长 | `users.created_at` | x / (x + half) |
| 近 7 / 30 天在线时长、近 30 天在线天数 | `user_online_daily`：实时连接心跳满 5 分钟与断开时按 UTC 自然日累计，单日封顶 86400 秒 | 同上 |
| 近 30 天登录天数、距上次活跃 | `login_audit_logs`、`user_online_daily`、`user_login_devices` | 距上次活跃为 half / (x + half) |
| 连续签到、近 30 天签到天数、累计签到 | `sign_stats`、`daily_signins`（连签只在昨天或今天签过时算） | x / (x + half) |
| 当前会员、永久会员、会员率 | `users.vip_expire_at` / `vip_lifetime_at`、`vip_transactions` | 会员率 = 累计会员天数 / 注册天数（永久会员为 1） |
| 累计付费、付费笔数 | `payment_orders`（status = paid） | x / (x + half) |
| 等级、近 30 天获得积分 | `users.experience` → `user_levels`、`integral_transactions` | 同上 |
| 邀请人数、近 30 天看完激励广告、兑换次数 | `invite_relations`、`app_rewarded_ad_views`、`points_store_redemptions` + `card_key_redemptions` | 同上 |
| 近 30 天风险分（负向） | `risk_assessments` 最高分 | 乘法惩罚 |

行为分 = 正向信号的加权平均 × Π(1 − s)^权重（负向信号不参与平均，否则「没有风险」会白送分数给新号）。
再折成中奖奖品的系数：`minFactor + (maxBoost − minFactor) × 行为分`，可限定只作用于某些奖品类型。
`signals` 不填时用目录里每个信号的推荐权重与半饱和值；控制台可逐项改。画像取不到时按空画像继续，不拦抽奖。

### 自定义规则

表达式用 [expr-lang/expr](https://expr-lang.org)，强类型环境，写错变量名在**保存时**就报错，执行有编译缓存：

- `user.*`：画像的全部字段（`registeredDays`、`onlineMinutes30d`、`signStreak`、`memberRate`、`paidAmount`、`level`、`invites`、`adViews30d`、`riskScore30d`、`signedToday`、`devices`……），
  本活动里的 `totalDraws`、`todayDraws`、`missStreak`、`winsTotal`、`winsToday`、`prizeWins`、`prizeWinsToday`，行为分 `score`
- `draw.*`：`source`、`hour` / `weekday`（应用时区）、`deviceAccounts`、`ipAccounts`
- `prize.*`：`id`、`kind`、`position`、`amount`、`weight`、`probability`（公示 %）、`limited`、`remaining`
- `activity.*`：`progress`（0–1）、`daysLeft`、`pointsIssuedToday`

```json
"strategy": {
  "softPity": {"start": 5, "step": 5},
  "pacing": {"slack": 0.05, "maxBoost": 1.5},
  "cooldown": {"draws": 5, "factor": 0.3},
  "budget": {"dailyPoints": 10000},
  "risk": {"rules": [{"signal": "deviceAccounts", "op": "gt", "threshold": 3, "action": "floor"}]},
  "behavior": {"minFactor": 0.8, "maxBoost": 1.5, "kinds": ["points", "vip_days"],
               "signals": [{"key": "onlineMinutes30d", "weight": 2, "half": 900}]},
  "rules": [{"name": "连签一周", "when": "user.signStreak >= 7", "kinds": ["points"], "factor": 1.5}]
}
```

### 模拟

`POST /api/admin/apps/{appKey}/lottery/simulate` 让一批虚拟用户（同一画像）各自连续抽若干次，每一次走与线上同一套
`PlanDraw` / `PityChoice` / `Sample`；软保底、冷却、个人上限随每个人的进度演变，库存、每日限量与积分预算默认所有人共享
（`isolatedStock` 时各用一份）。返回实际中奖率对公示概率、首次中奖与最长连续未中的分位数（gonum `stat`）、保底触发次数、
各奖品出现比例与受限次数。`activity` 传控制台里未保存的编辑内容；`userId` 用真实用户的画像、已拥有的奖品与本活动进度，
并附他此刻再抽一次的方案（`current`）。规模上限：5000 人、每人 1000 次、合计 20 万次。不写库、不发奖。

## 奖品

奖品发的东西与积分商城同构（`reward` JSON），由目录 `lottery.Catalog()` 驱动：

| `kind` | 说明 |
|---|---|
| `none` | 谢谢参与 |
| `points` | 积分（流水分类 `lottery`，计入周期排行榜） |
| `experience` | 经验 |
| `vip_days` / `vip_plan` | 会员（渠道 `lottery`，不能选试用套餐） |
| `feature` | 功能解锁（天数，0 为永久；已永久拥有时记一笔说明） |
| `lottery_draws` | 抽奖次数（进 bonus） |
| `vip_lifetime` | 永久会员（不挂套餐、不携带功能标识，与 `vip_days` 同口径；渠道 `lottery`）。要带功能的永久会员用 `vip_plan` 选永久套餐 |
| `feature_time` | 功能时长：`tag` + `amount`（分钟），进激励广告发的同一个功能时长账户（流水 ref_type `lottery`） |
| `ad_free` | 免广告时长（`amount` 天，多次抽中顺延，`user_ad_free_passes`）：有效期内 `/ads/policy` 的 `exempt` 为真（不放开屏、不受同意要求约束，不看策略的会员免除开关），功能时长一律免扣（看激励广告换时长的功能直接可用），并属于特权人群 `ad_free` |
| `manual` | 人工发放：抽中后为 `pending`，管理员在记录里标记已发放并写一句话给用户 |

后三档是抽奖独有的（积分商城、卡密里没有）。**老客户端兼容**：用户端的 `prizes[].kind` 与记录的 `prizeKind`
按最接近的已知类型下发（`vip_lifetime` → `vip_plan`，`feature_time` / `ad_free` → `feature`），老 App 的图标、配色与
「抽中后刷新会员与功能缓存」照常工作；真实类型放在新字段 `rewardType`。管理端接口一律是真实类型。

不含钱包余额。目录与发放分支由 `TestLotteryCatalogHasGrantBranch` 双向钉死。

奖盘约束（保存与上线时校验）：九宫格恰好 8 个、转盘 2–12 个；位置不重复；权重合计大于 0；
**至少一个不限库存的奖品**（否则奖池总有被抽空的一天）；最多一个兜底奖且必须不限库存；
库存不能改到已抽中数量以下。不限库存的奖品不维护 `used`（热门奖品行上没有锁竞争），抽中次数看统计。

用户端每个奖品带 `probability`：常规抽取下的公示概率（百分比，两位小数），不含保底与库存的影响。

## 已拥有的奖品（永久会员抽中会员）

永久会员抽中「会员 7 天」，照常记一段限时会员什么也没给他（功能取两条线的并集，永久那条已经全包）。
同类的还有：永久会员抽中他已含功能的套餐 / 功能、已永久解锁某功能的人再抽中这个功能。判据（`MemberCoverage.Owned`）走统一的
`vip.Holdings`（见 [会员权益折算](vip-conversion.md)「已拥有的判据」），与购买、积分商城、激励广告、卡密同一套：

| 奖品 | 何时算已拥有 |
|---|---|
| `vip_days` | 永久会员，且永久那条线免广告（看广告领到的永久会员可以不免，这时会员天数带来免广告，不算已拥有） |
| `vip_plan` | `vip.Holdings.CoversPlan`：套餐里启用中的功能都已永久拥有（永久开通或永久解锁），且免广告也已有；永久基础版抽中限时高级版不算 |
| `feature` | 已永久解锁这个功能，或永久会员的功能里有它 |
| `vip_lifetime` | 永久会员且免广告 |
| `ad_free` | 永久会员且免广告 |
| `feature_time` | 永久会员（会员一律免扣），或已永久解锁这个功能 |

积分、经验、抽奖次数、人工发放、谢谢参与从来不算。活动的 `lifetimePolicy` 决定怎么办（迁移 `000114`，已有活动默认 `points`）：

| `lifetimePolicy` | 行为 |
|---|---|
| `points`（默认） | 折算积分，单价与卡密、邀请、管理员发放同一套（应用的[会员折算策略](vip-conversion.md)）：会员按 `perVipDay`、限时功能与免广告按 `perFeatureDay`（按秒向上取整）、功能时长按 `perFeatureHour`、永久套餐 / 永久功能 / 永久会员给 `perLifetime`。活动的 `lifetimePointsPerDay` 大于 0 时覆盖会员与限时功能、免广告的每天单价，`lifetimePointsLifetime` 大于 0 时覆盖永久；两项默认 0 即完全跟随应用策略。单次封顶 1 亿 |
| `draws` | 改发 `lifetimeDraws` 次抽奖（默认 1） |
| `exclude` | 这个人的常规抽取里剔除这些奖品（权重按 0 算，公示概率随之重算）；保底 / 兜底恰好是这类奖品时按积分折算 —— 保底承诺的是「必中」 |
| `grant` | 照常发放（旧行为） |

奖品可单独设 `lifetimeReward`（积分 / 经验 / 抽奖次数），设了就不按公式折算（`grant` 时不生效）。
判定与发放都在抽奖事务里、锁用户行之后：抽奖与购买 / 兑换会员串行，不会出现「刚开通永久就抽到会员却没折算」。

### 无套利比例

积分能换到这个活动的次数时（积分抽奖价，或积分商城里卖「抽奖次数」，取单价最低的），一次抽奖的期望积分回报不能超过价格，
否则「用积分买次数、抽完再买」能稳赚（2026-10 线上出过：商城 10 积分一次，永久会员抽中月度会员折 300 积分，58 人几天刷走约 19 万积分）。
在这个人眼里的奖盘（剔除、特权加权之后的公示概率）上：

```
E = (P + V·k) / (1 − Q)
```

P 积分奖品的期望积分，V 已拥有、会改发成积分或抽奖次数的格子按原价折算的期望积分（次数按价格折成积分），Q 抽奖次数奖品的期望次数，
k 已拥有格子的发放比例。

- 原价折算（k = 1）时 E 不超过价格：什么都不动。正常的奖盘都是这样，永久会员抽中会员 7 天照样拿 70 积分。
- 原价折算会稳赚时：所有已拥有格子按同一个比例 `k = (价格·(1 − Q) − P) / V` 缩到刚好回本，相对大小不变；
  缩到不足 1 积分的改发经验（最多 100 点，换不成积分），不会是「谢谢参与」。
- 积分换不到次数（只有免费、看视频、卡密）时不缩减。

规则在 `domain/lottery/economy.go`。2026-10-28 之前是一刀切的上限 c（所有格子封到同一个积分数、c 为 0 时改发谢谢参与），
健康的奖盘上也会把高价值的格子压到个位数，影响抽奖意愿，改成了按比例且只在套利时生效；旧记录里的 `conversion.reward.type = none` 仍按「不再重复发放」显示。

管理端活动带 `economy`（`price`、`memberReturn`、`lifetimeReturn`、`lifetimeScale`、`warnings`）：奖盘本身（不算折算）的期望回报就超过价格、
或对永久会员剔除之后超过价格、或抽一次期望再得一次以上、或永久会员的折算被缩减时给出警告，控制台活动列表上显示。

记录上 `prizeKind` / `reward` 仍是抽中的那一格（统计按格子算），实际发的写在 `conversion`：

```json
"conversion": {"reason": "lifetime", "reward": {"type": "points", "amount": 70}, "label": "70 积分",
               "message": "你已是永久会员，「会员 7 天」已转为70 积分"}
```

`results` 是实际到账的流水（积分流水标题「抽奖奖品折算」，metadata 带 `converted`、`originalKind`）。
每次改发另记一条会员折算记录（`vip_conversions`，`kind = owned`、`source = lottery`，改发成积分时带积分流水号），
`conversion.conversionNo` 指向它：用户的 `/vip/conversions` 与控制台的折算视图里能看到抽奖折掉的东西。
活动页的奖品带 `swap`（`convert` + 改发的东西，或 `excluded`），页顶带 `lifetime` 提示（只在这个人有已拥有的奖品时出现）。
统计概览多 `converted`、`convertedPoints`，奖品分布多 `converted`。

## 容量与并发

2026-11-08 01:20–02:45 线上出过一次整站超时：一个持有 5.6 万次卡密次数的人用旧版 App 连续并发十连抽，
76 分钟抽了 3.6 万次。旧的抽奖事务在锁住用户行之后逐次扫 `lottery_draws` 求「本活动今天抽了几次 / 发了多少积分 /
同设备同 IP 几个账号 / 这个人每个奖品中过几次」，这些是 O(当天全部记录) 与 O(这个人全部记录) 的聚合：每一次都比上一次慢
（积分预算那一条单次 1 秒），同一个人的其余请求排在用户行锁后面，整站共用的 10 条连接被占满，所有人的请求一起超时。

现在抽奖事务里没有任何对记录的聚合，也没有全站一行的热点；所有计数都随抽奖事务提交，不会因为进程崩溃而丢
（迁移 `000149_lottery_scale`）：

| 事实 | 存在哪 | 读写 |
|---|---|---|
| 这个人今天的免费 / 积分 / 抽奖 / 中奖次数、每个奖品中过几次、最近 100 次抽中的奖品、近 10 分钟抽了几次 | `lottery_user_states` 一行 | 锁内读、锁内写（与连续未中、运气累计同一行） |
| 本活动每天全站抽了几次、中了几次、发了多少积分（全站反馈、积分预算、管理端汇总） | `lottery_activity_deltas` → `lottery_activity_daily` | 事务里追加一行增量（只插入，不争行锁）；后台每 500 ms 在一条语句里删掉增量、加进日计数（原子，多实例用 `SKIP LOCKED` 各取各的）；读 = 日计数 + 未合并的增量，按活动按天缓存 250 ms |
| 限量奖品的总库存与每日限量 | `lottery_prize_stock`，每个上限拆成最多 16 个分片 | 事务里挑一个有余量、没被锁着的分片 +1；表上 `CHECK (used <= cap)` 兜底 |
| 本活动今天同设备 / 同 IP / 同网段抽过奖的账号 | `lottery_risk_keys`（主键 = 活动、日、种类、键、用户） | 事务里插入；主键范围计数；只在风控用到聚集信号时记，保留 3 天 |

- **计数自校验**：状态行的 `counted_draws` 记着计数里算进去的抽奖次数。它与 `total_draws` 不等（新加的列是 −1、新建的行、
  滚动发布期间旧版本抽过）时，下一次抽奖在锁内按记录重算这个人的计数（每人一次）；活动页读到对不上的计数时也会重算，
  并在没人同时抽奖时写回。
- **首次启动补齐**：`LotteryService.Start` 在开始接请求之前，从历史记录补出全站每天的计数（分片 −1）与今天的风控聚集键，
  `schema_backfills` 记一笔，只补一次；多实例同时启动时用咨询锁串行。限量奖品的库存分片在迁移里按现有的 `quantity / used` 建出
  （已出的记在 0 号分片、余量平均分）；每日限量的分片在当天第一个人抽中时建，当天已出的从旧表 `lottery_prize_daily` 接上。
- **库存分片怎么取**：先 `FOR UPDATE SKIP LOCKED` 挑一个有余量、没人锁着的分片（按用户错开起点）；有余量的都被锁着时，
  不加锁地读出一片、只在这一行上等，等到时它可能刚被取满，再从头挑。一个事务在一个上限里最多持有一片的锁，等锁的事务手里没有
  分片锁，每日限量先于总库存取，所以不会死锁。每日限量与总库存在一个保存点里取，任何一步取不到都回到保存点，改发兜底奖。
  改库存 / 每日限量时，在活动的独占咨询锁下按「已出的保留、余量重新分配」重建分片（此时没有进行中的抽奖），不能改到已出数以下。
- **同一个人的抽奖在进程内排队**：`pkg/keyedgate`，同一时刻只跑一个，后面最多排 4 个、最多等 5 秒，超出的立刻回 40925
  「抽奖正在处理，请稍后重试」。排队的请求不占数据库连接。多实例之间仍由用户行锁串行，抽奖事务把等锁上限压到 3 秒，
  等不到同样回 40925（事务已回滚，次数与积分都没动）。
- **十连抽是一个事务**：N 次在同一个事务里逐次推进，一次加锁、一次提交（以前是 N 个事务、N 次提交与刷盘）。
  每一次在自己的保存点里抽：这一次抽不了（次数用完、风控拒绝、库里出错）只回滚这一次，前面抽到的照常提交，批次停下并写明原因。
  第 2 次起先重读会被上一次改变的事实（次数余额、积分、特权、会员、风控聚集），状态行的计数在内存里逐次累加、最后写一次。
  单抽是 1 次的批次，走同一段代码（`lottery_draw_tx.go`）。
- **互不依赖的读走批量管道**：pgx 的 Batch 把一批语句一次往返发出（`pgpipe.go`）。一次抽奖的往返：锁与基础读一批（咨询锁、
  活动、锁用户行、奖品、幂等回放、锁状态行）→ 判定要用的读一批（次数余额、积分、特权、会员、折算策略、套餐目录、运气干预、
  至尊加成、商城单价、全站计数、限量已出、风控聚集、预取记录 ID）→ 扣次数、占库存、扣积分、发奖（有先后依赖，逐条）→
  记录、全站增量、风控聚集键一批写 → 状态行写回与读回记录一批 → 提交。每一次的保存点随前一批读开、随这一次的写释放；
  扣次数是一条语句（修改型 CTE：挑批、扣批、记流水、扣汇总）。`TestLotteryRoundTrips` 实测：单抽 7 次往返（以前二十多次），
  十连抽 34 次往返（以前十个事务约 140 次）。
- **活动用咨询锁**：抽奖拿 `pg_advisory_xact_lock_shared('LOTT', 活动 ID)`，改奖盘拿独占锁。原来的 `FOR SHARE` 行锁在
  几千个并发事务下会变成 MultiXact，每一次都要写活动行。
- **记录一次写完**：记录 ID 先取号，发完奖再一次插入整条记录（不再「先插后改」，少一个死元组、少一轮索引更新）。
  `lottery_draws` 删掉了 4 个只为聚合服务的索引；出奖方案、奖励、发放结果改用 lz4 压缩，行超过 512 字节移出行外。
- **行为画像缓存**：画像是十几张表的历史聚合，抽奖时同一个人 30 秒内复用（最多缓存 10 万人）；活动页与模拟照常取最新的。
  缓存只影响出奖权重里的行为系数，不影响次数、库存与任何记录。
- **风控留痕**：抽奖前的风控评估留痕经 JetStream 可靠队列写库，一条不丢，见 docs/database.md「风控留痕」。

实测（本机 Docker Postgres 17）：

- `TestLotteryScaleIntegration`：单次抽奖约 4 ms（含风控评估、画像、发奖）；一个人有 5 万条历史时仍是约 4–5 ms（首次重算约 20 ms）；
  同一个人在两个实例上同时打 60 个请求，0.1 秒内全部答复（抽成或 40925）；50 个人同时十连抽 500 次约 0.5 秒，全部抽成；
  连接池只有 4 条时 40 人并发抽限量奖品不卡；200 人同时抢 37 份库存恰好出 37 份。
- `TestLotteryLoad`（`AEGIS_LOTTERY_LOAD=1`，连接池 40 条，`AEGIS_LOTTERY_LOAD_BATCH=1` 时每人一次十连抽）：
  2000 人各抽 10 次共 2 万次，0 失败。与改造前的代码在同一时段交替跑（本机负载 10–40 之间波动，每组新旧挨着跑）：

  | | 改造前 | 改造后 |
  |---|---|---|
  | 单抽 | 2269 / 1290 次/秒 | 3419 / 2301 次/秒 |
  | 十连抽 | 2783 / 1825 次/秒 | 5310 / 3271 次/秒 |

  单抽的延迟 p50 约 0.5–0.8 秒，几乎全是 2000 个并发请求在 40 条连接外排队的时间。

## 接口

### 客户端（App Protocol v1，需登录）

| 目录 key | 方法 | 路径 | 说明 |
|---|---|---|---|
| `lotteryActivities` | GET | `/lottery` | 进行中的活动（含未开始的）；没有时为空列表 |
| `lotteryState` | GET | `/lottery/activities/{activityId}` | 活动与奖品、`chances`、`pity` |
| `lotteryDraw` | POST | `/lottery/activities/{activityId}/draw` | 抽一次，返回 `draw`（含 `position`）、抽完后的 `chances` 与 `pity` |
| `lotteryDrawBatch` | POST | `/lottery/activities/{activityId}/draw/batch` | 连抽（`count` 1–10），见下 |
| `lotteryDraws` | GET | `/lottery/draws?activityId=&page=&limit=` | 我的抽奖记录（`source = bonus` 的带 `ticket`：来源与是否兑换所得） |
| `lotteryTickets` | GET | `/lottery/tickets?page=&limit=` | 我的抽奖次数与流水，见[抽奖次数](#抽奖次数) |

错误码：`40402` 活动不存在或已下线 / `40923` 未开始 / `40924` 已结束 / `42913` 今日免费次数已用完 /
`42914` 今日积分抽奖次数已用完 / `42915` 没有可用次数 / `40307` 积分不足 / `40928` 该方式未开放 /
`40929` 奖品配置已失效（整笔已回滚） / `40925` 同一 requestId 正在处理 / `40348` 风控拦截（场景 `lottery`）。

活动页另有 `viewer.lifetime`（当前用户是否永久会员，客户端换专属外观用）与上面的 `fortune`。

连抽（十连抽，`DrawBatch`）逐次调用与单抽相同的 `DrawLottery`：每一次一个事务、各自到账，保底、每日运气、库存与奖品限额在十次之间照常推进，
不是一次算好十个结果。第 i 次的 requestId 是 `requestId#i`（所以 requestId 不超过 61 位），重试时已抽过的原样返回、没抽到的接着抽；
全部是回放时 `replayed = true`。中途抽不了（次数用完、积分不够、活动刚好结束）时停下，返回已抽到的 `draws` 与 `stopped`
（`code` 与单抽报的错误码相同、`message`）；第一次就抽不了时整个请求报那个错误。风控只在开始时评估一次。每一次各自按扣除顺序选，
所以一次十连里先用完今天的免费、再用赠送的、兑换的次数，最后才花积分。

抽中积分或花了积分时向用户的其它在线设备推 `points.changed`；抽中人工发放的奖品发站内信（类型 `lottery`，跳转路由 `lottery`）。

### 管理端（`app:read` / `app:write`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/apps/{appKey}/lottery/catalog` | 奖品目录、界面形态、状态、可选的会员套餐 / 功能 / 发抽奖次数的激励广告场景 |
| GET / POST | `/api/admin/apps/{appKey}/lottery/activities` | 活动列表（含奖品与汇总）/ 新建（连同奖品） |
| GET / PUT / DELETE | `/api/admin/apps/{appKey}/lottery/activities/{activityId}` | 详情 / 整体更新活动与奖品 / 删除（有记录时 `40927`，改为结束） |
| PUT | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/status` | `draft` / `active` / `paused` / `ended`；上线前按当前奖盘再校验一次 |
| GET | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/stats?days=14` | 概览、按天趋势、来源与奖品分布 |
| GET | `/api/admin/apps/{appKey}/lottery/draws` | 记录（activityId / userId / prizeId / status / source / winning / risk / riskAction / outcome / keyword / start / end），见下 |
| GET | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/risk?days=7` | 风控总览（见上文「风控规则」） |
| POST | `/api/admin/apps/{appKey}/lottery/draws/{drawId}/fulfill` | 人工发放的奖品标记已发放（`{note}` 写进站内信；重复标记 `40926`） |
| POST | `/api/admin/apps/{appKey}/lottery/simulate` | 出奖策略模拟（见上文「模拟」），附首中精确分析 `analytic` |
| GET | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/audit?days=14` | 出奖审计（见上文「精确分析与审计」） |
| GET | `/api/admin/apps/{appKey}/lottery/tickets` | 抽奖次数总览：持有人数、未用次数（两档）、今天发放与使用、各来源累计与剩余 |
| GET | `/api/admin/apps/{appKey}/lottery/tickets/accounts?keyword=` | 有次数账户的人，余额多的在前 |
| GET | `/api/admin/apps/{appKey}/lottery/tickets/ledger` | 次数流水（userId / kind / origin / keyword） |
| GET | `/api/admin/apps/{appKey}/lottery/tickets/users/{userId}` | 一个人的账户与批次 |
| POST | `/api/admin/apps/{appKey}/lottery/tickets/users/{userId}/adjust` | `{amount, reason, paid}`：正数发放（发站内信）、负数按扣除顺序扣减（不够扣 `40000`） |

记录检索：`keyword` 是检索式（`search.go`），空格分隔的多段同时满足。完整单号走唯一索引，`LTD` 开头的前缀按前缀匹配；
IP 精确匹配；纯数字匹配记录 ID、用户 ID、数字账号或奖品名里的数；其余匹配奖品名、账号、昵称（三元组索引，账号与昵称先在
用户表里解析成 user_id 集合）。可指定字段：`user:` `prize:` `device:` `ip:` `no:` `id:`（也认「用户 / 奖品 / 设备 / 单号」），
引号包起来的值可以带空格。`risk` 为 `any` / `clean` / 信号键，`outcome` 为 `pity` / `fallback` / `conversion`。
总数与这一页在两个连接上并行查。

活动的保存体：

```json
{
  "name": "幸运抽奖", "description": "", "rules": "", "uiMode": "grid", "status": "draft",
  "startTime": null, "endTime": null,
  "freeDaily": 1, "pointsCost": 0, "pointsDaily": 0, "videoScene": "lottery",
  "pityThreshold": 6, "pityPosition": 1, "sortOrder": 0,
  "lifetimePolicy": "points", "lifetimePointsPerDay": 0, "lifetimePointsLifetime": 0, "lifetimeDraws": 1,
  "prizes": [
    {"id": 0, "name": "", "reward": {"type": "none"}, "imageUrl": "", "quantity": -1, "weight": 40, "position": 0, "fallback": true},
    {"id": 0, "name": "2 积分", "reward": {"type": "points", "amount": 2}, "quantity": -1, "weight": 30, "position": 1},
    {"id": 0, "name": "会员 7 天", "reward": {"type": "vip_days", "amount": 7}, "quantity": 100, "weight": 2, "position": 2,
     "lifetimeReward": {"type": "points", "amount": 100}}
  ]
}
```

奖品按 `id` 对上：带 id 的更新（保留已抽中数量），不带的新建，原有而这次没带的删除（记录有快照，不受影响）。
保底奖按位置（`pityPosition`）指定，名字留空时按奖品内容生成（「30 积分」「会员 7 天」）。

## 迁移

`000110_lottery_rework` 在旧列还在时一次性搬迁（迁移器每次启动都重跑全部 `*.up.sql`，所以搬迁放在以旧列为条件的 DO 块里）：
旧 `cost_type=free` 的活动搬成每天 `max(daily_limit, 1)` 次免费（新版没有「不限次免费」）、`points` 的搬成积分抽奖；
旧奖品 `points` / `experience` 且值为数字的照搬，名字像「谢谢参与」的搬成 `none`，其余搬成 `manual`；
旧记录补单号与快照，`claimed` 记为已发放，用户已被删除的记录清掉。`lottery_participants` 与 `lottery_seed_commitments` 删除。

`000136_lottery_draw_lots` 建批次表与流水表、给抽奖记录加批次列，并把每个人汇总里多出批次的部分补成一批 `legacy`（带一条流水）；
差额为 0 时不再补，所以可重复执行。

`000142_lottery_app_settings` 建应用级设置表（至尊加成），只有 `CREATE TABLE IF NOT EXISTS`，不碰旧表搬迁。

`000145_lottery_risk` 给记录加 `risk_rules` / `risk_action` 与检索索引、建 `lottery_risk_events`。旧记录的回填放在
「`idx_lottery_draws_risk` 还不存在」的 DO 块里，只跑一次。

`000149_lottery_scale` 给状态行加计数列、建 `lottery_activity_deltas` / `lottery_activity_daily` / `lottery_prize_stock`
（并按现有库存建出分片）/ `lottery_risk_keys` / `schema_backfills` / `risk_trail_events`，
删 4 个聚合用的索引（000024 / 000124 不再建它们），改压缩与填充因子（DO 块里先判断，只做一次）。数据补齐不在迁移里
（迁移不知道应用时区），在 `LotteryService.Start`。

## 验证

`TestLotteryScaleIntegration`：同一个人在两个实例上各并发 30 个单抽（只有抽成与 40925 两种结果、记录数 = 次数余额差 = 计数）、
50 个人同时十连抽（全部抽成、同设备第 3 个账号起只发兜底、全站增量合并之后日计数 = 记录数、增量清空、管理端汇总取计数）、
计数作废后重算并写回、5 万条历史的人每次抽奖不变慢、连接池只有 4 条时并发抽限量奖品不卡（每日限量分片之和 = 记录数、
没有分片超过上限）、200 人抢 37 份库存恰好出 37 份、加库存后已出的保留且只多出加的部分、库存不能改到已出数以下、
首次启动补齐只补一次。每一步都把状态行的计数与记录逐项对账。`TestLotteryRoundTrips` 量单抽与十连抽的往返次数，
并核对奖品本身是抽奖次数时连抽，余额始终等于批次之和、不会误补「早期次数」。`TestLotteryLoad` 是压测，默认跳过。

`TestLotteryIntegration`（真实 Postgres）：保存校验、草稿不可见、免费次数与幂等回放、视频抽奖（client 模式的激励广告发次数）、
积分抽奖与每日上限、保底、限量奖品并发只出库存数、同一 requestId 并发只抽一次、会员 / 功能 / 人工发放、
已拥有的奖品（四种处理方式、单奖品改发、剔除后的概率与保底、已永久解锁的功能、统计）、删除与统计，
最后在旧表形状上跑 000110 + 000114 + 000124 + 000125 + 000130 + 000136 + 000145 + 000149 两遍核对搬迁。

`TestLotteryRiskIntegration`：同设备与同网段聚集（第 3 个账号起只发兜底、兜底奖照常发出）、风控列与逐条结果落库、
按信号筛记录、缺设备标识拒绝且次数不扣、拒绝事件、风控总览的并行聚合（处置 / 信号 / 设备与网段聚集），以及各种检索式。

`TestLotteryTicketsIntegration`：早期余额对齐成一批、管理员发放两档、活动页拆分、过时页面传 `points` 也先扣免费、
指定 `free` 不越级、单抽与十连抽都按「免费 → 赠送 → 兑换 → 积分」、流水与累计、扣减先扣赠送的且跨批记两条、总览与账户搜索。

`TestLotteryPerksIntegration`：永久会员奖品（老客户端看到 `vip_plan`）与再抽中的折算、永久会员特权（4 次视频全部免看广告并进免费次数、
额外免费、积分 5 折与额外次数、保底提前、提示卡并入说明、trace 记特权）、签到条件、功能时长奖品、免广告时长（顺延、功能时长免扣、
开屏免除、`ad_free` 人群）、永久会员抽中免广告 / 功能时长的折算、模拟里的特权身份。单元测试在 `perks_test.go`（含免看广告判定的稳定性与比例校准）。

`TestLotteryFortuneControlIntegration`：定档后活动页与抽奖事务的系数一致、个人定档优先于全站偏移、全站 −4 落到最低系数、
总览的今日分布、预演不落库、记录与状态、全部撤销恢复自然运气、重复撤销 40404、重抽、跨应用用户 40401、关闭 / 重开运气差异不动奖盘。
合成规则的单元测试在 `fortune_control_test.go`。

`TestLotterySupremeIntegration`：应用设置的校验（幅度超限、别的应用的档位）与保存、永久会员活动页与抽奖事务同一个加成后的系数、
限时会员与普通用户不受影响、模拟（真实用户带对照组、普通用户没有、虚拟永久会员）、活动单独关闭、调整运气差异保留活动的设置、
按档位分级（限时档位、只认永久开通、未列档位的永久会员）、单人读数与总览、应用关闭后回到自然运气。公式、单调性、范围、
档位择优、用户端读数、归一与模拟的单元测试在 `supreme_test.go`。

`TestLotteryBatchIntegration`：十连抽在 3 次免费后停下并写明 42915、同一 requestId 重试原样返回且不多扣、一次都抽不了时报错、
次数上限；活动页的今日运气与出奖系数一致、`baseProbability`、`viewer`。今日运气档位占比的单元测试在 `fortune_reading_test.go`。

`TestLotteryStrategyIntegration`：12 人并发下全站每日限量不超发、个人上限、积分预算、同设备聚集、
行为画像整条查询（签到、会员率、邀请、在线时长累计）与规则、方案落库且不给用户端、模拟与单人预览、策略回读与非法规则。
策略、画像、规则、卡方抽样检验与模拟的单元测试在 `internal/domain/lottery/strategy_test.go`。

```
AEGIS_TEST_PG_DSN=postgres://... go test ./internal/service -run 'TestLotteryIntegration|TestLotteryStrategyIntegration|TestLotteryPerksIntegration'
```
