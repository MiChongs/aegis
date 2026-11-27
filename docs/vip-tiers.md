# 会员档位

> 面包屑：[Aegis](../CLAUDE.md) › 会员档位

会员的权益只属于**档位**（`vip_tiers`）。套餐只是档位的售卖规格（价格 × 时长 / 永久）；
每段会员期（`vip_transactions`）记的是档位，权益按档位**当前**的功能算。

## 为什么

权益原先跟着套餐行走（`plan_id`），套餐一删就定格成开通时的快照：

- 删掉旧套餐、重新上架一个，新套餐是另一行，老会员与它没有任何关系；
- 管理员自定义赠送的永久会员不挂套餐，永远停在发放那一刻的功能。

结果是「整理商品目录」实际改掉了存量会员的权益。档位不随套餐删除而消失，删档位必须指定会员迁往哪里，权益不会再悬空。

## 规则

| 场景 | 落在哪个档位 | 功能 |
|---|---|---|
| 按套餐开通（购买、直购、发放、卡密、抽奖、积分商城、邀请、排行榜、激励广告……） | 套餐的档位 | 档位当前的功能 |
| 自定义发放，指定了 `tierId` | 该档位 | 档位功能 ∪ `features`（档位之外另给的，记在 `extra_features`） |
| 不挂套餐的**永久**开通，没指定档位（管理员 / 远程函数 `vip.grantLifetime`、老系统迁入、抽奖或卡密的永久会员） | 应用的**永久档位** | 档位功能 ∪ `features` |
| 看广告领的会员天数 | 不挂档位 | 按奖励场景当前的会员权益 |
| 其余不挂套餐的会员天数（邀请奖励天数、卡密赠送天数、限时自定义发放） | 不挂档位 | 发放时的功能快照（与以前一样） |

**永久档位**：在售永久套餐所属档位（排序靠前、同序取最新）；永久套餐都删了时，取最近一笔仍生效、有档位的永久开通所在的档位；
都没有时这笔开通不挂档位。档位列表的 `defaultLifetime` 标出它。

解析只有一个入口：`resolveGrantTierTx`（`internal/repository/postgres/vip_tier_repository.go`），在 `extendUserVipTx` 最前面调用，
所有发放渠道都经过它；「已拥有」判据（`vip.Holdings`）看的是并上档位功能之后的这一笔。发放预览走同一条规则（`ResolveVipGrantTier`）。

判定：`Segment.EffectiveFeatures`（`internal/domain/vip/segment.go`）—— 有档位按档位，其次套餐（滚动发布期间旧实例写的记录）、激励广告场景、快照。

## 套餐与档位

- 套餐的 `features` 就是所属档位的功能（只读）；`vip_plans.features` 列只是镜像，由仓储在保存档位 / 套餐时同步（`syncTierPlanFeaturesTx`），
  留给滚动发布中的旧实例和按列统计（删功能标识时的 `affectedPlans`）。
- 保存套餐：`tierId` 指定档位；新建不传时，有功能完全相同的档位就并进去（删掉再上架同样的套餐自动回到原档位），否则按套餐名新建一个。
  传 `features` 改的是档位的功能（同档位的套餐与全部在期会员一起变）。改 `tierId` 只影响以后的售卖，已开通的会员仍在原档位。
- 删套餐不动任何会员。

## 权益标签

会员页上「这个档位给你什么」的一句话，**只用于展示**，与功能标识（`features`）无关：不参与「已拥有」判断、会员折算，
也不解锁任何功能。结构 `{key, label, detail, source}`，两种来源：

| source | 说明 |
|---|---|
| `ai` | 按应用 AI 计费设置（[ai-billing.md](ai-billing.md) 的会员优惠，与计费同一口径）自动生成，改价后自动跟着变：`ai.rate`「AI 调用 9 折」、`ai.daily`「每天 200 AI 额度」、`ai.monthly`「每月 6,000 AI 额度」、`ai.pack`「额度包 9 折·多送 5%」（额度商店开着时）、`ai.limits`「AI 并发 8 路」（高于应用默认时）。AI 中转关闭时没有；档位没在 `memberTiers` 里配置时按通用会员优惠 |
| `custom` | 管理员在档位上手填的补充标签（`vip_tiers.perks`，迁移 000178），最多 12 个，标签 ≤ 16 字、说明 ≤ 60 字，同字去重，key 重编为 `custom.N` |

- 用户端 `GET /vip/plans`：每个套餐带所属档位的 `perks`（自动的在前、手填的在后，与自动标签同字的手填项去掉）；读不到计费设置时只少自动标签，列表不失败。
- 管理端 `GET /vip/tiers`：`perks`（手填，可编辑）与 `autoPerks`（自动，只读预览）；`POST /vip/tiers` 收 `perks`（不传不变更、空数组清空）。
- 代码：`internal/domain/vip/perk.go`（类型与校验）、`internal/service/vip_perks.go`（自动生成与合成，`TestAIMemberPerks`）。

## 删档位

`DELETE /api/admin/apps/:appkey/vip/tiers/:tierId?moveTo=`

- 档位下没有套餐、没有在期会员：直接删，已结束的开通记录 `tier_id` 置空（快照仍是当时的留档）。
- 否则必须给 `moveTo`：套餐改卖目标档位，**全部**开通记录改挂目标档位，会员从此按目标档位的功能算。没给报 40996。
- 档位不存在、`moveTo` 不存在或等于自己：40499。

## 接口

| 接口 | 说明 |
|---|---|
| `GET /api/admin/apps/:appkey/vip/tiers` | 档位列表：功能、`plans`、在期会员数 `members` / `lifetimeMembers`、`defaultLifetime` |
| `POST /api/admin/apps/:appkey/vip/tiers` | 新建（`id` 为 0）或更新：`name`、`description`、`features`、`sortOrder`。改功能立即对档位下全部在期会员生效 |
| `DELETE /api/admin/apps/:appkey/vip/tiers/:tierId?moveTo=` | 见上 |
| `POST .../vip/plans` | 新增 `tierId`；返回的套餐带 `tierId`、`tierName` |
| `POST .../vip/grant`、`.../grant/preview` | 新增 `tierId`（自定义发放归入的档位） |

会员流水（`vip_transactions`）返回 `tierId`、`extraFeatures`。用户侧 `/vip/status`、`/vip/plans` 的契约不变。

控制台：会员区块「档位」视图（`vip-tiers-panel.tsx`）；套餐编辑选档位；用户详情自定义发放可选档位。MCP：`vip_tiers`、`vip_save_tier`、`vip_delete_tier`，`vip_save_plan` / `vip_grant` 加 `tier_id`。

## 迁移 000138

迁移器每次启动重跑，下面 1–3 每次都跑（幂等），4–5 只在加 `vip_transactions.tier_id` 列的那一次做：

1. 没有档位的套餐按（应用, 功能集合）分组建档位（已有功能相同的档位就并进去）。名称：全是永久套餐叫「永久会员」，没有功能叫「基础会员」，否则取第一个套餐名。
2. 套餐还在的开通记录挂上套餐的档位。
3. 老系统迁入的永久开通（`legacy_import`）挂上永久档位。
4. 套餐已删的开通记录：同应用里有同名、同形态（永久 / 限时）的套餐，就挂它的档位（删掉再上架的那个）。
5. 仍生效、还没有档位的永久开通（看广告领的除外）挂永久档位；不挂套餐的自定义发放把当时选的功能留作 `extra_features`。

## 测法

`TestVipTierIntegration`（嵌入式 PG 18，`unix_socket_directories=''`）：自定义赠送永久会员 → 删套餐 → 重新上架 → 删旧档位迁移 → 改档位功能，
以及删掉两列后重跑迁移验证第 4、5 步。
