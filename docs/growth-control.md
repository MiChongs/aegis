# 成长调控（积分 / 经验获取速度的奖惩）

管理员在用户详情的「奖惩」页签里控制一个人（或全应用）获取积分与经验的速度，并控制等级的上下限。

## 倍率规则

一条规则由这些部分组成：

| 字段 | 说明 |
|---|---|
| 作用对象 | 某个用户（`userId`），或全应用（`userId = 0`） |
| 类型 | `integral` 积分 / `experience` 经验 |
| 倍率 | 分数 `numerator / denominator`：×2 = 2/1，÷3 = 1/3，×1.5 = 3/2，冻结 = 0/1。分子 0–1000，分母 1–1000，保存时约分 |
| 取整 | `floor` 向下（默认）/ `round` 四舍五入 / `ceil` 向上 / `random` 按小数部分的概率进位（长期期望与倍率一致，适合小额收入） |
| 每日上限 | 当天受管来源**实际入账**的合计上限，0 = 不限 |
| 来源 | 空 = 全部默认来源；也可以只选几个（见下表） |
| 生效时间 | `startsAt` / `endsAt`，都可空；不填结束时间就一直生效到撤销 |
| 启用 | 暂停的规则不生效，可随时恢复 |

×1 且不设每日上限的规则没有作用，会被拒绝。同一对象同一类型最多 20 条未结束的规则。

### 叠加

一笔收入入账时，取这个人此刻生效的全部规则（个人 + 全应用）里**作用于这个来源**的那些：

- 倍率相乘（大数运算后约分，叠多少条都不会溢出）；
- 每日上限取最小的非零值；
- 取整方式取个人规则里最新的一条，没有个人规则时取全应用最新的一条。

先按倍率换算、再按每日上限截断。被换算成 0 的收入（冻结、到了上限）照样写一条流水，金额为 0，便于追溯。

### 受管来源

| 键 | 来源 | 默认受管 |
|---|---|---|
| `signin` | 每日签到（等级签到加成之后的数值） | 是 |
| `rewarded_ad` | 激励广告 | 是 |
| `lottery` | 抽奖奖品 | 是 |
| `invite` | 邀请与推广档位 | 是 |
| `leaderboard` | 排行榜结算 | 是 |
| `item_grant` | 物品发放、等级达成奖励、礼包 | 是 |
| `card_key` | 卡密赠送（常为售卖品） | 否，需显式勾选 |
| `points_store` | 积分商城兑换的经验包（用户已付积分，只有经验） | 否，需显式勾选 |

**永远不受管**：管理员手动调整、付费充值、会员套餐赠送、会员折算、各种退回与冲正、消费。

### 记账

- 流水的 `multiplier` 列写综合倍率（截到 99.99），`extra_data.growth` 写完整结果：`base`（规则前）、`scaled`、`granted`（实际入账）、`num`/`den`、`rounding`、`dailyCap`、`usedToday`、`capped`、`ruleIds`、`source`。
- 有规则作用时，`growth_daily_usage` 按 人 × 类型 × 天 累计 `base_amount`、`granted_amount`、`suppressed_amount`、`events`；每日上限按 `granted_amount` 判断。日界与物品发放相同（服务器本地时间零点）。
- 入账时调用方已锁住用户行，同一个人的入账串行，每日上限不会被并发绕过。规则每次入账都查库（部分索引，只含未撤销的行），多实例下新加的处罚立即生效。

## 等级控制

- **封顶**（`user_level_records.capped_level`）：经验照常累计，等级停在封顶处。封顶低于现有等级时必须同时勾选「降级」：等级降到封顶，保底锁定一并截到封顶，写一条升级记录（原因 `penalty`，不通知）；已发放的达成奖励不收回。
- **解除封顶**：按当前体系补判一次，经验已够的级数立即升上去；经过的达成奖励按 `(appid, user_id, reward_key)` 去重，不会重复发放。
- **强制升级**与**锁定保底**沿用等级体系的调级接口（docs/levels.md）；强制升级的目标高于封顶时返回 40900。

## 扣减经验

管理员可以扣减经验（用户详情「资产」页、`POST /api/app/points/adjust-experience` 传负数、批量、冲正）。扣减后经验不足整笔拒绝（40009）。
自动发放的路径（云函数等内部调用）仍然不能扣经验。扣减时选等级处理 `level_mode`：

| 值 | 行为 |
|---|---|
| `keep`（默认） | 保留现有等级（已达到的等级是保底），只扣经验；进度按新经验显示，要重新攒够才会再升级 |
| `recalc` | 按扣减后的经验从锁定的保底起重新判定（与「重算并允许降级」同一套逻辑），可以降级；封顶照旧；降级写升级记录（原因 `admin`），不通知 |

两种都不收回已发放的等级达成奖励；以后再升回同一级，奖励按 用户 × 奖励键 去重，不会重复发。
流水是 `type = consume` 的负数行，`extra_data.levelMode` 记处理方式，`level_before / level_after` 记前后等级。

- 批量：`POST /api/app/points/batch-adjust` 带 `kind=experience`（与 `level_mode`），逐人在各自的事务里执行，带请求编号时每人的编号为「编号-用户ID」，整批重试只补上没成功的人。
- 冲正：`POST /api/admin/apps/:appkey/users/:userId/points/experience/transactions/:transactionNo/reverse`（`reason`、`levelMode`）。冲正一笔获得是扣回（按 `levelMode` 处理等级），冲正一笔扣减是还回（按经验正常升级）。同一笔只能冲正一次，冲正流水不能再冲正。
- MCP：`points_adjust_experience`（可加可扣）、`points_batch_adjust(kind=experience)`、`points_reverse(kind=experience)`；aegis-mcp 0.4.1。

## 接口（管理端）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/apps/:appkey/growth/catalog` | 来源、取整方式、常用倍率、等级上限与数值边界 |
| GET | `/api/admin/apps/:appkey/growth/rules` | 规则列表：`userId`、`scope`（all/app/user）、`kind`、`status`（active/scheduled/paused/expired/revoked/live）、`effect`（boost/penalty/freeze）、`keyword`、`page`、`limit` |
| POST | `/api/admin/apps/:appkey/growth/rules` | 新建（`userId` 为 0 是全应用） |
| GET / PUT / DELETE | `/api/admin/apps/:appkey/growth/rules/:ruleId` | 详情 / 修改（对象与类型不可改，已撤销不可改）/ 删除 |
| POST | `/api/admin/apps/:appkey/growth/rules/:ruleId/revoke` | 撤销（立即失效，保留记录） |
| POST | `/api/admin/apps/:appkey/growth/preview` | 推演：`base` 数量的 `source` 收入在当前规则（+ 可选草稿 `draft`，`excludeRuleId` 替换正在编辑的那条）下入账多少 |
| GET | `/api/admin/apps/:appkey/growth/events` | 调控记录（`userId` 不为 0 时含全应用记录） |
| GET | `/api/admin/apps/:appkey/users/:userId/growth` | 用户「奖惩」总览：各类型综合倍率与各来源矩阵、今日上限进度、近 30 天各来源收入、近 14 天作用效果、等级与封顶、规则、调控记录。九组查询在服务端并行 |
| PUT / DELETE | `/api/admin/apps/:appkey/users/:userId/growth/level-cap` | 等级封顶（`level`、`demote`、`note`）/ 解除 |

错误码：40000 输入不合法；40400 规则或用户不存在；40900 规则已撤销、规则数到上限、封顶低于现有等级未勾选降级、强制升级越过封顶。

## 用户搜索的性能

管理端用户搜索（bleve）从第 4 版起：

- 索引按用户 ID 取模分成 N 个分片（`ADMIN_USER_SEARCH_SHARDS`，默认 CPU 核数，2–16），检索经 `IndexAlias` 并行打到全部分片后归并排序；
- 同步是三段流水线：读库（游标分页，下一页与本页的建索引重叠）→ 多核构建文档（拼音多音字展开）→ 各分片并行提交批次；
- 同一检索条件 15 秒内复用结果，任何写入让该应用的缓存代次 +1 立即作废；
- 目录名带分片数（`admin_users_v4_s8`），改分片数会建一份新索引；旧版本目录在启动后后台删除。
