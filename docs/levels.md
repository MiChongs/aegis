# 等级体系

每个应用一套等级体系，运营在控制台的画布上编排：**体系设置 → 等级 → 晋级条件 → 等级 …**，等级上挂**达成奖励**与**等级权益**。
草稿与已发布分开存，发布时留一份版本快照（可恢复到草稿），发布后可以起一个重算任务让全体用户按新体系判定一次。

代码：`internal/domain/levels`（目录、画布编译、判定、默认体系与模板）、`internal/repository/postgres/level_*.go`（体系缓存、同步等级记录、
奖励发放、管理端查询、重算任务）、`internal/service/level_service.go`（发布、推演、强制升级、后台循环）、控制台 `components/apps/levels/`。
迁移 `000140`（体系）、`000141`（等级锁定 `user_level_records.locked_level`）。

## 默认体系

没有发布过的应用使用默认体系：由旧版全站等级表 `user_levels`（迁移 000005 的 18 级）原样转换，同一个经验值对应的等级、名称、
签到经验倍率与重构前完全一致。旧表里的「权益文字」与「奖励」从没生效过（只是展示用 JSON），不搬。`user_levels` 保留只读，
控制台第一次打开时草稿就是这份默认画布，改了再发布即成为本应用自己的体系。

## 画布

形状与物品发放相同（`nodes[].id/type/position/data`、`edges[].source/sourceHandle/target`），控制台用 Flowgram 自由布局编辑，
位置换算口径同物品发放（左上角 ↔ 顶边中点）。

| 节点 | 端口 | 作用 |
|---|---|---|
| 体系设置 `scheme` | `next` | 恰好一个。体系名称、升级通知（标题 / 内容 / 跳转页，占位 `{level}` `{name}` `{from}` `{nickname}` `{rewards}`）、`hideLocked`（App 只露出下一级） |
| 等级 `level` | `next` `attach` | 名称、累计经验门槛、图标（目录里的 lucide 名）、颜色 `#RRGGBB`、说明。**等级号不存**，按主干上的先后顺序编为 Lv.1、Lv.2… |
| 晋级条件 `gate` | `next` | 放在两级之间：经验够了还要满足条件才能升到下一级。可视化规则（全部 / 任一）或 expr 表达式，环境是 `user.*` 行为画像（与抽奖、物品发放同一份，去掉 `user.level` 与 `user.experience`）；`hint` 是给用户看的说明 |
| 达成奖励 `reward` | — | 挂在等级的 `attach` 上。物品与物品发放同一套 14 种（数量固定，不支持按人计算）；`claim` = `auto` 升级后台自动到账 / `manual` 用户在 App 里领取 |
| 等级权益 `privilege` | — | 挂在等级的 `attach` 上。`exp_boost` 签到经验倍率、`integral_boost` 签到积分倍率、`display` 展示型、`custom` 自定义键（App 按键判断，可附数值）。`scope` = `inherit` 本级及以上 / `only` 仅本级 |

编译规则（`levels.Compile`）：主干从体系设置的 `next` 出发、一条链不分叉、无环；第一级门槛必须为 0、门槛严格递增、最多 100 级；
晋级条件不能在第一级之前、不能两个相连、后面必须接等级；奖励与权益只能挂在一个等级上，每级最多 12 个；
没接上主干的等级 / 条件、没挂到等级上的奖励 / 权益是提醒。奖励引用的套餐与功能按物品发放的 `CheckRefs` 校验。
**草稿可以有错误，发布时不能有。**

权益的实际享有（`Scheme.Effective`）：本级及以下所有 `inherit` 的权益 + 本级 `only` 的权益；倍率类与同一个键的自定义权益取**最高等级**那一条，展示权益逐条保留。

## 判定

- 等级记录 `user_level_records` 是准的：自动判定时等级**只升不降**，记录里的等级是保底；降级只发生在管理员显式操作时（扣减经验选「按经验重新判定」、等级封顶并降级、重算并允许降级，见 docs/growth-control.md）。判定从保底的下一级开始往上：经验够、（有条件时）条件满足就升，遇到第一个不满足的条件停下（`BlockedBy` 告诉 App 卡在哪）。
- 判定发生在：经验变化（经验账本、签到，在原事务里）、`GET /points/level`（短事务补判：条件后来满足了、体系改过门槛）、管理员调级、重算任务。行为画像只有跨晋级条件时才取。
- 体系级数变少时保底截到新的最高级；调高门槛不会让人掉级，要按新门槛降级须在发布时勾「重算并允许降级」（或单独起重算任务）。
  允许降级的重算从**锁定的等级**（没有锁定为 0）起判定，所以被锁定的人不会降到锁定等级以下。
- 第一次建记录时的补判（新用户第一次打开等级页）不算升级、不发通知，但会写经过的达成奖励（因此 Lv.1 的奖励就是新人礼）。
- 排行榜等级榜、物品发放人群的等级范围、抽奖 / 物品发放条件里的 `user.level`、积分商城与邀请的等级门槛全部读等级记录（没有记录按 Lv.1）。等级榜排名先比等级再比经验。

## 强制升级

管理员可以不看经验门槛与晋级条件，把一个人或一批人直接升到目标等级（`POST /users/{userId}/adjust`、`POST /users/adjust`）：

- 等级只能调高：目标不高于当前等级时不变（批量里算成功、未变化）。经验不扣减，所以调低没有意义，下次判定会升回去。
- 判定从 `max(当前等级, 目标)` 起继续往上：经验够、后面的条件也满足的，会顺势升得更高。
- `topUp`：先把经验补到目标等级的门槛（经验流水记为管理员调整，原因「管理员强制升级至 Lv.N」），否则等级高于经验对应的等级，进度从 0 开始。
- `skipRewards`：不写这次经过的各级达成奖励。之后也不会再自动写（奖励按「经过的等级」写，这些等级已经过了），需要时对奖励节点「补发」。
- `silent`：不发升级通知；升级记录照写（原因 `admin`、操作人与备注）。
- `lock`：把判定后的等级锁定为保底（`locked_level`），「重算并允许降级」不会降到它以下；`unlock` 解除锁定，当前等级不变。
  `level = 0` 配 `lock` 即「按当前体系补判后锁定当前等级」。
- 批量最多 500 人，逐人在自己的事务里执行，个别失败（用户不存在、补经验失败）不影响其他人，结果逐人列出 `from` `to` `error`。

## 达成奖励

升级从 a 到 b 时，a+1…b 每一级上的每个奖励节点各写一条 `user_level_rewards`（唯一键 `等级:节点`，同一个人只会有一条）：
`auto` 进入 `queued`，后台循环逐条在**独立事务**里发放（物品走物品发放的 `grantItemTx`，与其他渠道同口径；流水来源记为物品发放、
`extraData.source = level_reward`）；任何一项失败整笔回滚、记录标 `failed`（管理员可重试），不影响触发它的那次经验变化。
`manual` 进入 `claimable`，用户 `POST /points/level/rewards/{id}/claim` 领取。含人工物品的进入 `pending`，管理员标记处理后通知用户。

补发：新挂到某级上的奖励只发给之后升上来的人；要发给已经达到该级的人，在控制台对这个奖励节点点「补发」（发布之后才能补发）。

## 升级通知与实时事件

体系开启通知时，每次升级（`to > from`）写一条待发通知，后台循环按发送时的体系拼标题与内容（`{rewards}` = 已到账的物品与待领取的礼包），
站内信类型 `level`（分类「积分与奖励」），默认跳转路由 `level`（我的等级）。实时事件：`level.up`、`level.reward`、`level.changed`。

## 接口

用户端（`/api/points/...` 与网关 `/api/v1/apps/{appkey}/points/...` 同一组 handler）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/points/level` | 我的等级：原有字段 + `icon` `color` `description` `maxLevel` `privileges`（实际享有）`claimableRewards` `blockedBy` `schemeName` `schemeVersion` `hideLocked` |
| GET | `/points/levels` | 全部等级：原有字段 + `privileges`（本级配置）`effective`（实际享有）`rewards`（`items` 为文字描述）`gate` `reached`；`hideLocked` 时只到下一级 |
| GET | `/points/level/rewards?status=` | 我的达成奖励（待领取在前） |
| POST | `/points/level/rewards/{rewardId}/claim` | 领取；已领取 40900，不存在 40400，物品发不出去 50000（记录标为失败，等管理员重试） |
| GET | `/points/level/history?page=&limit=` | 我的升级记录 |

`expMultiplier`、`privileges`、`rewards` 的形状变了：`privileges` 从字符串数组变为对象数组，`rewards` 从对象变为数组。

管理端（`/api/admin/apps/{appkey}/levels`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/catalog` | 节点、权益种类、图标、物品、晋级条件字段与运算符、跳转页、套餐与功能、模板、占位符、上界 |
| GET | `/overview?days=` | 等级分布、平均等级、升级趋势、奖励各状态数、正在跑的任务 |
| GET / PUT | `/scheme` | 草稿（含校验结果与逐级展开）/ 保存草稿（`revision` 对不上 40900；0 = 还没有草稿） |
| POST | `/validate` | 校验画布 |
| POST | `/publish` | 发布（`note`、`recalc`、`downgrade`），返回版本号与任务 |
| POST | `/simulate` | 推演：给经验值或用户（ID / 账号），`graph` 为空时用已发布的体系 |
| GET | `/versions`、`/versions/{v}` | 发布历史 / 一版的画布 |
| POST | `/versions/{v}/restore` | 取回草稿（不自动发布） |
| GET | `/users`、`/users/{userId}` | 用户等级列表（等级、范围、关键字、排序）/ 详情（等级信息、升级记录、奖励） |
| POST | `/users/{userId}/adjust` | 强制升级：`level` 至少到这一级（越过门槛与条件；0 = 只补判），`topUp` 补经验到门槛，`skipRewards` 不发经过的奖励，`silent` 不发通知，`lock` / `unlock` 锁定或解除保底，`note` |
| POST | `/users/adjust` | 批量强制升级：`userIds`（最多 500）+ 同上选项；返回 `total` `succeeded` `changed` `failed` 与逐人结果 |
| GET | `/events` | 升级记录（用户、原因、等级、时间） |
| GET | `/rewards`、`/rewards/{id}` | 达成奖励记录 |
| POST | `/rewards/{id}/retry`、`/rewards/{id}/fulfill`、`/rewards/backfill` | 重试失败 / 人工物品已处理 / 补发（`nodeId`） |
| GET / POST | `/jobs` | 重算任务列表 / 起一个（`downgrade`，202；一个应用同时只有一个，40900） |
| GET | `/jobs/{id}`、POST `/jobs/{id}/cancel` | 进度 / 取消 |

## 控制台

应用详情「用户运营 → 等级体系」（`components/apps/levels/`）：体系（逐级展开）、总览、用户等级（勾选多人批量强制升级；详情里补判、强制升级、锁定当前等级 / 解除锁定，列表里锁定的人等级旁有锁形标记）、升级记录、奖励记录、版本与重算。
画布编辑器是全屏弹层（`level-editor.tsx` → `level-workspace.tsx`，next/dynamic 只在浏览器加载），晋级条件复用物品发放的 `ConditionForm`，奖励复用 `ItemListEditor`（`asItemGrantCatalog` 把等级目录适配过去）。

Flowgram 在 React 严格模式的**开发环境**下会把插件加载两次（`useMemo` 被调用两次），页面报 `Ambiguous match found for serviceIdentifier: FlowRendererRegistry`，物品发放的画布同样如此；生产构建不受影响。本地要在浏览器里看画布，临时把 `next.config.ts` 的 `reactStrictMode` 改成 false。

## 测法

- `go test ./internal/domain/levels/`：默认体系与旧表一致、模板可编译、晋级条件、保底与截断、编译错误、权益继承与覆盖、奖励区间。
- `TestLevelIntegration`（真实 Postgres）：`AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55438/postgres?sslmode=disable go test ./internal/service -run TestLevelIntegration`。
  覆盖默认体系、草稿并发保护、发布、升级与自动 / 手动奖励、通知、重复领取、晋级条件补判、倍率、调级、重算降级、补发、版本恢复、推演、总览、排名。
  `TestLevelForceUpgradeIntegration`：越过门槛与条件、锁定扛住降级重算、不发奖励与不发通知、批量逐人与单人失败、解除锁定、参数校验。
