# 用户号码（可自定义外部 ID、靓号、摇号）

> 代码：`internal/domain/usernumber`（纯函数：等级、规律、造号、设置、权益选择）、
> `internal/repository/postgres/user_number_repository.go`、`internal/service/user_number_service.go`、
> `internal/transport/http/user_number_handlers.go`；迁移 `000165_user_number`。

## 两层身份

| | 对外 ID（`users.public_id`，[user-public-id.md](user-public-id.md)） | 号码（`users.number`） |
|---|---|---|
| 形状 | 16 位随机数字 | 5–12 位纯数字（不以 0 开头） |
| 唯一范围 | 全站 | **按应用**（不同应用可以有同一个号） |
| 能不能改 | 终生不变 | 可以更换：改号卡、靓号券、摇号、管理员指定、物品发放「指定号码」 |
| 用途 | OAuth2 `sub`、头像与广告令牌、一起听、客户端本地数据的键（`/me` 的 `id`、登录结果的 `userId`） | **展示与查找**：资料页、会员卡、客服报号、管理端搜索 |

任何令牌、签名、第三方关联都不挂在号码上，换号不会让任何东西失效。客户端仍然用 `id` 做本地数据的键，展示改用 `number`。

`/me` 增加 `number`、`numberTier`；登录结果增加 `number`；管理端用户视图增加 `number`、`numberTier`；
管理端用户搜索的关键字会匹配号码（5–12 位纯数字不走 Bleve 索引）。

## 发号

* 建号：`BEFORE INSERT` 触发器按应用的默认位数（设置 `defaultLength`，8–12，缺省 9）随机发一个**普通号**。
  数据库函数 `aegis_user_number_is_plain` 是 Go 侧规律目录在默认位数下的超集过滤，放行的号在 Go 侧一定判普通
  （`TestPlainFilterIsSuperset` 钉住）：靓号不会被白送。
* 存量用户：迁移逐行回填，同样是默认位数的普通号。
* 删号（注销、管理员删除）：号码按应用的冻结天数冻结、不挂原主人，冻结期内谁都拿不到。

## 等级与规律

五级：`0 普通`、`1 精品`、`2 稀有`、`3 传说`、`4 至尊`（`usernumber.Tiers()` 带名称与建议主色）。

| 规律键 | 名称 | 默认等级 |
|---|---|---|
| `all_same` | 全同号 | 至尊 |
| `len5` | 五位号 | 至尊 |
| `len6` / `len7` | 六位号 / 七位号 | 传说 |
| `leopard5` | 豹子号（五连及以上） | 传说 |
| `mirror` | 对称号（回文，至少 7 位） | 稀有 |
| `run4` | 四连号 | 稀有 |
| `straight4` | 顺子号（四位，升或降） | 稀有 |
| `year_tail` | 年份尾号（1950–2039） | 稀有 |
| `run3_tail` | 尾号三连 | 精品 |
| `aabb` / `abab` | AABB / ABAB | 精品 |
| `lucky_tail` | 吉利尾号（520、1314、168、518、666、888、999） | 精品 |

一个号码命中多条时取最高的一级；应用可以在设置 `patternTiers` 里改某条规律的等级，或写 `-1` 停用。
等级在**换号那一刻**判定并记在 `users.number_tier`（统计用）；展示时按当前规则实时再判一次。

## 权益

| 种类 `kind` | `level` | 名称 | 用途 |
|---|---|---|---|
| `change` | 0 | 改号卡 | 自选一个普通号 |
| `ticket` | 1–4 | 精品 / 稀有 / 传说 / 至尊靓号券 | 自选不高于该级的号（也能换普通号） |
| `draw` | 0 | 摇号次数 | 摇一次给 N 个候选号，锁定若干分钟，任选其一直接换上（不再扣别的权益） |

自选时不指定用哪份权益，就自动选能用的里最便宜的：普通号先用改号卡，其余用等级最低的够用的靓号券。

账户 `user_number_rights`（`balance ≥ 0`、`earned`、`used`）+ 流水 `user_number_ledger`（`delta`、`balance_after`、`origin`、`ref`、`title`）。
流水来源 `origin`：`item_grant`、`level`、`card_key`、`points_store`、`lottery`、`invite`、`leaderboard`、`promo`、`rewarded_ad`、`admin`、`change`（换号用掉）、`draw`（摇号用掉）。

### 发放渠道

全部在各自的事务里调用仓储的 `grantUserNumberRightTx`。奖励类型在各渠道的目录里同名：

| 类型 | 取值形态 | 物品发放 / 等级 | 卡密 | 积分商城 | 邀请 / 排行榜 / 推广 | 抽奖 | 激励广告 |
|---|---|---|---|---|---|---|---|
| `number_change` 改号卡 | `amount`（张） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `number_ticket` 靓号券 | `number_ticket`：`amount`（张）+ `refId`（等级 1–4） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `number_draw` 摇号次数 | `amount`（次） | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `number_assign` 指定号码 | `text`（号码） | ✓ | | | | | |

`number_assign` 不扣权益、不看冷却，但不能拿保留号、别人的冻结号或正在用的号（那一笔发放失败、可重试）。

## 换号规则

* **冷却**：两次换号之间至少隔 `cooldownDays` 天（默认 7）；管理员指定、回滚、物品发放不受限。摇号也先判冷却，冷却中不扣次数。
* **旧号冻结**：换下来的旧号冻结 `freezeDays` 天（默认 90），冻结期内只有原主人能换回、换回免费（`source=frozen`）。
* **保留号**：后台保留的号用户自选与摇号都拿不到，只能由管理员指定（`force`）。
* **摇号**：一次摇 `drawCandidates` 个（默认 6）候选号，锁定 `drawHoldMinutes` 分钟（默认 15）。各级出现的概率按 `drawWeights`（默认 70 / 20 / 7 / 2.5 / 0.5），
  非普通级的权重乘以**今日号运**：与抽奖的每日运气同一个模型（对数正态、按人按天哈希，`luckSpread` 默认 0.3，活动号 −1，与任何抽奖活动独立），
  `luckSupreme` 开启时叠加抽奖的应用级至尊加成（永久会员 / 指定档位）。新摇一次会作废上一张打开的单；放弃或过期次数不退。
  候选号在锁定期内只给摇出它的人。
* **开关**：`enabled=false` 时用户端不能换号与摇号（号码照常展示）；`selfPick=false` 时只能摇号。

## 用户端接口（`/api/v1/apps/:appkey`，需登录）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/numbers/me` | 我的号码页：`number`、`tier`、`tierName`、`patterns`、`patternNames`、`changedAt`、`nextChangeAt`、`rights`、`draw`（打开的摇号单）、`luck`（今日号运 + 各级概率 `odds` / `baseOdds`）、`frozen`（自己冻结中的旧号）、`rules`、`tiers`、`catalog`（生效的规律） |
| GET | `/numbers/check?number=` | 查号：`valid`、`available`、`reason`（`available` / `invalid_format` / `invalid_length` / `self` / `taken` / `reserved` / `frozen` / `locked`）、`message`、`tier`、`patterns`、`ownFrozen`、`needKind` / `needLevel` / `needName`、`canChange` |
| GET | `/numbers/suggest?q=&tier=&count=` | 自选联想：含 `q` 的空闲号码；`q` 为空时给一组 `tier` 级的空闲号码（`items` 同查号结构） |
| POST | `/numbers/draw` | 摇号：`draw`（`id`、`candidates[{number,tier,patterns}]`、`luck`、`expiresAt`）、`luck`、`rights` |
| POST | `/numbers/draws/:drawId/discard` | 放弃打开的摇号单（次数不退） |
| POST | `/numbers/change` | 换号：`{number: "字符串", drawId?, useKind?, useLevel?}` → `number`、`tier`、`oldNumber`、`source`、`used`、`rights`、`nextChangeAt` |
| GET | `/numbers/history?page=&limit=` | 我的换号记录 |
| GET | `/numbers/ledger?page=&limit=` | 我的号码权益流水 |

号码在请求里用**字符串**传，响应里是整数（≤ 12 位，JS 与 Kotlin Long 都精确）。

错误码：`42980` 未开放、`42981` 格式或位数不对、`42982` 已被使用、`42983` 保留 / 冻结 / 锁定中、`42984` 冷却中（429）、
`42985` 没有够用的权益、`42986` 只能摇号、`42987` 摇号次数不足、`42988` 摇号单不存在或已过期、`42989` 不在这张摇号单里、
`42950` 与当前号码相同、`42951` 没摇到空闲候选（次数不扣）、`42952` 换号记录不存在、`42953` 扣减会变负、`42954` 用户不存在。

## 管理端接口（`/api/admin/apps/:appkey/numbers`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/catalog` | 等级、规律（带本应用等级）、权益种类、流水来源、设置边界 |
| GET | `/overview` | 用户数、各级号码数、今日与近 7 天换号、今日摇号、打开的摇号单、各类占用数、未用的权益合计 |
| GET / PUT | `/settings` | 读 / 保存设置（整份） |
| POST | `/settings/odds` | `{settings, luck}` 试算各级概率 |
| GET | `/inspect?number=` | 查一个号码：等级、规律、谁在用、占用 |
| GET | `/users/:userId` | 一个用户的号码、权益、摇号单、冻结的旧号、最近换号与今日号运（`userId` 为主键） |
| POST | `/users/:userId/assign` | `{number, force, note}` 指定号码 |
| POST | `/users/:userId/rollback` | `{historyId}` 回到那次换号之前的号码 |
| POST | `/users/:userId/rights` | `{kind, level, delta, note}` 调整权益余额 |
| GET | `/history?userId=&page=&limit=` | 换号记录 |
| GET | `/ledger?userId=&kind=&page=&limit=` | 权益流水 |
| GET | `/draws?userId=&page=&limit=` | 摇号单 |
| GET | `/holds?kind=&page=&limit=` | 占用（`reserved` / `frozen` / `candidate`） |
| POST | `/holds` | `{numbers: ["..."], note, expiresAt?}` 保留一批号码（≤ 500，有人在用的跳过） |
| DELETE | `/holds/:number` | 删掉一条占用（保留号放出、冻结提前解除、候选释放） |

权限：读 `app:number:read`，改设置 / 保留号 `app:number:write`，指定 / 回滚 / 调权益 `app:number:grant`。
