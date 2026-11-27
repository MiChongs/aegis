# 物品发放

运营在控制台的画布上编排发放规则：**触发器 → 筛选 → 分流 / 限额 → 发放 → 站内信**。规则可以手动执行、按 Cron 定时执行，
或者在用户注册、登录、签到、支付成功时实时触发。全站能发给用户的东西都能发，并且与从积分商城、卡密、抽奖等渠道得到的完全同口径。

代码：`internal/domain/itemgrant`（目录、画布、引擎、模板）、`internal/repository/postgres/item_grant_*.go`（单人发放事务、各账本分支）、
`internal/service/item_grant_service.go`（执行、定时、事件）、控制台 `components/apps/item-grant/`。迁移 `000137`。

## 物品

| 类型 | 账本入口 | 说明 |
|---|---|---|
| `points` 积分 | `applyIntegralChangeTx` | 分类 `item_grant`，不计入周期排行榜 |
| `experience` 经验 | `applyExperienceChangeTx` | 同步等级 |
| `vip_days` 会员天数 | `extendUserVipTx` | 渠道 `item_grant`，`ConvertOwned`：永久会员收到时按折算策略折成积分 |
| `vip_plan` 会员套餐 | 同上 | 试用套餐不能发；已下架的照发（校验给提醒） |
| `vip_lifetime` 永久会员 | 同上 | 开通时被覆盖的限时权益按应用策略折算 |
| `feature` 功能解锁 | `grantFeatureUnlockOrConvertTx` | 0 天为永久；已永久拥有时按策略折算或写明「已永久拥有」 |
| `feature_time` 功能时长 | `grantFeatureTimeTx` | 分钟，与激励广告同一个账户 |
| `ad_free` 免广告时长 | `grantAdFreePassTx` | 天，顺延 |
| `lottery_draws` 抽奖次数 | `grantLotteryDrawsTx` | 一批赠送的次数（`origin = item_grant`） |
| `ai_credit` AI 额度 | `grantAIGatewayCreditTx` | 点，1 点按 1 积分定价 |
| `balance` 钱包余额 | `applyWalletChangeTx` | 不计入累计充值；幂等键 `itemgrant:<记录号>:<下标>` |
| `license_days` 授权时长 | `extendLoginLicenseTx` | 名下没有限时授权卡时这一笔失败 |
| `device_slots` 设备位 | `addDeviceSlotsTx` | 没有授权卡时失败 |
| `manual` 人工发放 | — | 记录进入 `pending`，管理员标记已发放后通知用户 |

`TestItemCatalogHasGrantBranch` 双向钉死目录与 `grantItemTx` 的 switch。数量型物品可以写 `amountExpr`（如 `50 + user.signStreak * 5`），
结果向下取整、截在上下界内，算不出来退回固定数量。

## 画布

形状与 React Flow 相同（`nodes[].id/type/position/data`、`edges[].source/sourceHandle/target`），原样保存原样取回。
控制台的画布是 Flowgram 自由布局编辑器（`item-grant-workspace.tsx`），载入与保存时在 `item-grant-graph.ts` 里换算：
节点位置左上角 ↔ 顶边中点（卡片宽 280）、`sourceHandle` ↔ `sourcePortID`（没写端口的旧连线补成目录里的第一个输出端口），
连线标识按 `源:端口->目标` 生成，校验问题的 `edgeId` 据此定位回画布上的线。

| 节点 | 端口 | 作用 |
|---|---|---|
| 触发器 `trigger` | `next` | `manual` / `schedule`（五段 Cron，应用时区，相邻两次至少 10 分钟）/ `event`（`register` `login` `signin` `payment`） |
| 目标人群 `audience` | `next` | 会员身份、注册天数、最近活跃、等级范围、用户名单 / 排除名单、会员档位（`tierIds`：此刻持有其中任一档位；`tierLifetimeOnly` 只认永久持有，同为永久会员的几档——如领航者与领航者青春版——靠它区分）。**紧接在触发器后面**的那个决定手动与定时执行的候选人（SQL 过滤）；其余位置是逐人判定 |
| 条件判断 `condition` | `yes` / `no` | 可视化条件（全部 / 任一满足）或一条 expr 表达式；环境见下 |
| 时间窗口 `window` | `in` / `out` | 星期、每日时段（可跨零点）、日期范围 |
| 随机分流 `split` | 每个分支一个 | 按权重；`sticky` 时按 FNV(规则, 节点, 用户) 固定分配 |
| 频次限额 `limit` | `pass` / `over` | 每人 / 全站在 累计·天·周·月 内最多几次（按发放成功且经过该节点的记录数） |
| 发放物品 `grant` | `next` | 一组物品（最多 12 项） |
| 随机礼包 `random` | `next` | 按权重不放回抽 `picks` 个选项，每个选项是一组物品 |
| 站内信 `notify` | `next` | 发放成功后发；`{items}` `{nickname}` `{rule}` 占位 |
| 结束 `end` | — | 显式结束 |

解释方式：从触发器出发沿端口走；同一端口连出多条线就都走（并行发放）；多条路汇到同一个节点时只判定一次；画布必须无环。
没有走到任何发放节点的人不写记录（执行统计里算「跳过」）。

条件环境（expr，`internal/domain/itemgrant/engine.go` 的 `Env`）：

- `user.*`：行为画像，与抽奖的行为信号同一份（`registeredDays`、`onlineMinutes30d`、`signStreak`、`member`、`lifetime`、`level`、`paidAmount`、`invites`、`riskScore30d`……），另有 `signedToday`、`memberDays`、`experience`、`integral`、`devices`。只有表达式引用了 `user.` 才去取（事务外取一次）。
- `now.*`：应用时区的 `hour` `minute` `weekday`（1 为周一）`day` `month` `year`。
- `event.*`：事件数据，`signin` 有 `consecutiveDays`，`payment` 有 `amount`（元）。可视化条件引用事件字段时自动写成 `(event.x ?? 0)`。

上线前校验（`Compile` + `CheckRefs`）：恰好一个触发器、端口存在、无环、至少一个发放节点可达、配置合法、表达式能编译、
引用的套餐存在且不是试用、解锁的功能存在且启用。草稿可以存有问题的画布；**生效中的规则保存时画布不能有错误**。

## 执行

单人发放是一个事务（`ExecuteItemGrant`）：锁用户行 → 幂等键已存在则返回 `duplicate` → 判定 → 写记录（占 `UNIQUE(rule_id, dedupe_key)`）→
逐项走账本入口 → 写回结果。任何一项失败整笔回滚，另写一条 `failed` 记录（带原因，占住幂等键，管理员可重试）。
全站限额在数用量之前拿该节点的事务级咨询锁，并发执行不会超发。

| 触发 | 幂等键 | 说明 |
|---|---|---|
| 手动 / 定时 | `run:<执行>:<用户>` | 候选人按用户 ID 升序每批 200 人、4 并发；每批写进度与游标并续心跳；被取消时这批处理完就停 |
| 注册 | `event:register:<用户>` | |
| 登录 | `event:login:<用户>:<毫秒>` | 每次登录一个键，频次交给限额节点 |
| 签到 | `event:signin:<用户>:<日期>` | |
| 支付成功 | `event:payment:<订单 ID>` | |

- 一条规则同时只有一次在执行（部分唯一索引）。心跳超过 3 分钟的执行由任一实例接管、从游标接着跑（`FOR UPDATE SKIP LOCKED`）。
- 定时：后台循环每 30 秒取到点的规则，`next_run_at` CAS 推进，多实例只跑一次。规则有生效期（`startsAt` / `endsAt`）。
- 事件：插件服务的钩子观察者（`PluginService.ObserveHooks`，注册 / 支付成功）、登录观察者（`AuthService.SetLoginObserver`，
  在 `finalizeLogin` 收口，密码、短信、第三方、扫码、卡密、通行密钥、二次验证之后都算；设备数超限待选择时不算）与签到观察者（`SignInService.SetSignInObserver`）
  转成 `Emit`；生效规则按应用与事件缓存 30 秒，改规则时本实例立即失效。事件处理在后台、8 并发，不阻塞登录与签到。
- 账号不可用（停用、冻结、生效中的封禁）的人不进入画布。

## 接口（管理端，`/api/admin/apps/{appkey}/item-grants`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/catalog` | 物品、节点、触发方式、事件、条件字段与运算符、人群、周期、跳转页面、模板、套餐与功能 |
| GET | `/overview?days=` | 规则数、记录数、今日、待处理、失败、执行中、趋势、近 30 天按物品 |
| GET / POST | `/rules` | 列表（附统计与执行中）/ 新建（草稿，画布为空时给初始画布） |
| GET / PUT / DELETE | `/rules/{ruleId}` | 详情 / 保存（版本号加一）/ 删除（有记录的拒删，40900，可归档） |
| PUT | `/rules/{ruleId}/status` | `draft` / `active` / `paused` / `archived`；上线前校验 |
| POST | `/rules/{ruleId}/run` | 立即执行一次（202，后台执行）；事件规则不能批量执行 |
| POST | `/validate` | 校验画布，返回 `issues`（`nodeId` / `edgeId` 定位） |
| POST | `/preview` | 预演，不发放：`user` 给了就追踪这一个人，否则抽样 `samples`（≤1000）人，返回节点 / 端口到达人数与物品推算 |
| GET | `/runs`、`/runs/{runId}` | 执行记录（详情含画布快照） |
| POST | `/runs/{runId}/cancel` | 取消执行 |
| GET | `/records`、`/records/{recordId}` | 发放记录（路径、计划、结果） |
| POST | `/records/{recordId}/fulfill` | 人工发放已处理（通知用户） |
| POST | `/records/{recordId}/retry` | 重试失败记录（按规则当前画布重新判定） |

错误码沿用通用段：40000 参数 / 画布错误、40400 不存在、40900 状态冲突（执行中、有记录拒删、状态不对）。

## 测法

- `go test ./internal/domain/itemgrant/`：编译期校验、路径、限额、固定分流、不放回抽取、窗口、周期、表达式拼接、模板可编译。
- `TestItemGrantIntegration`（真实 Postgres）：`AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55437/postgres?sslmode=disable go test ./internal/service -run TestItemGrantIntegration`。
  覆盖八种账本到账、不可用账号跳过、限额挡住第二次执行、事件幂等、失败回滚与重试、人工发放、抽样与单人预演、定时排程、Cron 过密拒绝、总览、拒删。
