# 推广活动

> 面包屑：[Aegis](../CLAUDE.md) › 推广活动

运营在一段时间内办的「推广 App」活动。一期活动有两项任务，可以只开一项：

1. **邀请好友**：活动期间成功邀请的人数达到某一档，用户自己领取这档奖励。人数沿用[邀请](invite.md)的邀请关系，
   不另造一套：邀请码、填写、防刷（设备 / IP / 风控）、「被邀请人签满 N 天才算」都还是邀请那边的规则。
2. **分享推广**：用户在**允许的平台**（默认微信公众号、小红书、酷安）发文推荐 App，提交文章链接，审核通过后得奖励。
   **抖音系平台永远不可用**（`ForbiddenPlatforms`，写死在代码里，控制台关不掉），另有可编辑的禁用域名（默认快手）。

| 表 | 内容 |
|---|---|
| `promo_campaigns` | 活动：标题、副标题、规则、时间、状态，其余配置在 `config` JSONB |
| `promo_tier_grants` | 邀请档位的领取记录，每人每档一次（`uq_promo_tier_grants`） |
| `promo_submissions` | 分享提交：平台、链接、状态、审核结论与发放结果。同一活动里同一篇文章（`url_key`）只能有一条未驳回的提交 |

## 活动

| 字段 | 说明 |
|---|---|
| `title` `subtitle` | 最多 32 / 64 字 |
| `rulesText` | 活动规则，最多 2000 字，用户端原样展示 |
| `startsAt` `endsAt` | 必填，`endsAt` 晚于 `startsAt`，最长 180 天 |
| `status` | `draft` 草稿 / `online` 上线 / `offline` 下线。用户端只看得到 `online` 的 |
| `config.claimGraceDays` | 结束后还能领取邀请档位的天数，0–30，默认 7 |
| `config.invite` | `enabled`、`tiers`（最多 10 档，`count` 严格递增，每档 `title` 最多 16 字、`rewards` 1–6 项）、`leaderboard`（是否展示排行）、`leaderboardSize`（10–100） |
| `config.share` | `enabled`、`platforms`（启用的平台键，至少一个）、`rewards`（每篇通过的文章发一次，1–6 项）、`maxApprovedPerUser`（每人最多几篇通过，1–50）、`maxApprovedPerPlatform`（每人每个平台最多几篇，1–50）、`dailySubmitLimit`（每人每天最多提交几次，1–50）、`blockedDomains`（额外禁用的域名，最多 20 个）、`shareTemplate`（推荐文案，最多 300 字，`{code}` `{link}` `{app}` `{platform}`） |

奖励目录与邀请相同（`invite.RewardCatalog`：积分、会员套餐、会员天数、功能解锁、经验、余额、抽奖次数），
走同一个发放函数，账本上的来源也是 `invite`，流水标题是「推广活动：<标题>」。引用的套餐与功能在保存时核对，发放时再核对一次。

上线中的活动可以改规则、奖励、时间，但不能删；删除只对草稿与下线的活动有效，而且已经有人领取或提交过的不能删（只能下线）。

### 平台目录

`promo.PlatformCatalog()`：

| 键 | 名称 | 认可的域名 | 安卓包名 | 默认启用 |
|---|---|---|---|---|
| `wechat_mp` | 微信公众号 | `mp.weixin.qq.com` | （无，公众号在后台发文） | 是 |
| `xiaohongshu` | 小红书 | `xiaohongshu.com` `xhslink.com` | `com.xingin.xhs` | 是 |
| `coolapk` | 酷安 | `coolapk.com` `coolapk1s.com` | `com.coolapk.market` | 是 |
| `zhihu` | 知乎 | `zhihu.com` | `com.zhihu.android` | 否 |
| `tieba` | 百度贴吧 | `tieba.baidu.com` | `com.baidu.tieba` | 否 |
| `weibo` | 微博 | `weibo.com` `weibo.cn` | `com.sina.weibo` | 否 |

写死的禁用平台（`ForbiddenPlatforms`）：抖音（`douyin.com` `iesdouyin.com` `amemv.com` `douyinvod.com` `douyinpic.com`）、
抖音火山版（`huoshan.com`）。默认的可编辑禁用域名：`kuaishou.com` `gifshow.com` `kwai.com` `chenzhongtech.com`。

域名按标签边界后缀匹配：`www.xiaohongshu.com` 算小红书，`evilxiaohongshu.com` 不算。

## 判定：纯函数

- `promo.Phase(campaign, now)`：`upcoming` 未开始 / `active` 进行中 / `ended` 已结束（`endsAt` 之后，或已下线）。
- `promo.EvaluateTier(campaign, tierCount, facts, now)`：能不能领这一档。
- `promo.EvaluateSubmit(campaign, input, facts, now)`：能不能提交。先校验链接（`promo.ParseLink`）再看次数。

| reason | 码 | 含义 |
|---|---|---|
| `not_found` | 40415 | 活动不存在或未上线 |
| `not_started` / `ended` | 42240 | 活动未开始 / 已结束（领取看 `claimGraceDays`） |
| `invite_disabled` | 42241 | 本期没有邀请任务 |
| `tier_not_found` / `tier_locked` | 42242 | 没有这一档 / 人数还不够 |
| `tier_claimed` | 40955 | 这一档已经领过 |
| `share_disabled` | 42243 | 本期没有分享任务 |
| `platform_unavailable` | 42244 | 平台未启用 |
| `link_invalid` / `link_platform_mismatch` | 42245 | 不是 http(s) 链接 / 链接不属于所选平台 |
| `link_forbidden` | 42246 | 抖音等禁止的平台，或禁用域名 |
| `link_duplicate` | 40956 | 这篇文章已经提交过（自己或别人，未被驳回） |
| `daily_limited` / `user_limited` / `platform_limited` | 42247 | 今天的提交次数用完 / 通过篇数已满 / 这个平台的篇数已满（待审核的也占名额） |
| `reward_unavailable` | 42248 | 奖励引用的套餐或功能已失效 |

管理端另有 40416 提交不存在、40954 提交已经审核过。

## 邀请人数

`count` = 当前用户作为邀请人、**创建时间在活动时间内**、`inviter_status` 为 `granted` 或 `limited` 的邀请关系条数；
`pending` = 同一时间段内 `inviter_status = pending` 的条数（等被邀请人签满天数）。被取消的不算。
排行榜按 `count` 降序、达到这个人数的时间升序；用户端只展示昵称（没有昵称时账号打码）与头像，不返回账号与用户 ID。

领取一档：一个事务里锁用户行 → 重算人数 → 判定 → 写 `promo_tier_grants` → 发奖励。重复请求撞唯一键回 40955。

## 分享提交

`ParseLink`：去掉首尾空白、只认 `http` / `https`、主机转小写、去掉 `www.` 以外不做改写；`url_key` = 主机 + 路径（去掉末尾 `/`），
查询串与锚点不参与（小红书的 `xsec_token` 每次分享都不一样）。公众号文章的路径是 `/s` 时，`url_key` 带上查询串里的 `__biz` `mid` `idx` `sn`。

提交：一个事务里锁用户行 → 数今天的提交次数、已通过与待审核的篇数 → 判定 → 写入（`pending`）。

审核（管理端）：
- **通过**：锁提交行，只处理 `pending`；再核一次「通过篇数」上限（只数 `approved`），超了就不能通过（40954 之外回 42247）；发奖励，结果写进 `results`；给用户发站内信（类型 `invite`，路由 `promo`）。
- **驳回**：必须写原因（最多 200 字），发站内信。被驳回的链接可以重新提交。
- 批量：`ids` 最多 100 个，逐条处理，返回每条的结论。

## 接口

用户端（需登录）：

```
GET  /api/v1/apps/{appKey}/promo                          { campaigns: [...] } 进行中与刚结束（领取期内）的活动
GET  /api/v1/apps/{appKey}/promo/{id}                     活动详情（见下）
GET  /api/v1/apps/{appKey}/promo/{id}/leaderboard         排行榜 { items[{rank,nickname,avatar,count}], me{rank,count} }
POST /api/v1/apps/{appKey}/promo/{id}/tiers/claim         { count } 领取一档 → { count, results[RewardResult] }
GET  /api/v1/apps/{appKey}/promo/{id}/submissions         我的提交（page / limit）
POST /api/v1/apps/{appKey}/promo/{id}/submissions         { platform, url, note, deviceId } → 提交记录
```

列表项 `ClientCampaignSummary`：`id` `title` `subtitle` `phase` `startsAt` `endsAt` `hasInvite` `hasShare` `claimable`（有可领的档位）。

详情 `ClientCampaign`：

```jsonc
{
  "id": 3, "title": "...", "subtitle": "...", "rulesText": "...",
  "phase": "active", "startsAt": "...", "endsAt": "...", "claimUntil": "...",
  "invite": {                      // 本期没有邀请任务时为 null
    "count": 4, "pending": 1,
    "code": "AB12CD34", "shareText": "...", "shareLink": "...",   // 来自邀请页；邀请未开启或不满足邀请人资格时为空，canInvite=false
    "canInvite": true, "inviteMessage": "",
    "tiers": [{ "count": 3, "title": "...", "rewards": [ClientReward], "state": "claimable", "claimedAt": null }],
    // state: locked 人数不够 / claimable 可领取 / claimed 已领取 / expired 过了领取期
    "leaderboard": true
  },
  "share": {                       // 本期没有分享任务时为 null
    "platforms": [{ "key": "xiaohongshu", "name": "小红书", "hint": "...", "packageName": "com.xingin.xhs", "domains": ["xiaohongshu.com","xhslink.com"] }],
    "forbidden": ["抖音", "抖音火山版"],          // 用户端要明示「禁止在这些平台发布」
    "rewards": [ClientReward],
    "shareText": "...",                         // shareTemplate 填好后的推荐文案
    "maxApprovedPerUser": 3, "maxApprovedPerPlatform": 1, "dailySubmitLimit": 5,
    "approved": 1, "pending": 1, "todaySubmitted": 2,
    "canSubmit": true, "reason": "", "message": ""
  }
}
```

提交记录 `ClientSubmission`：`id` `platform` `platformName` `url` `note` `status`（`pending` / `approved` / `rejected`）`rejectReason` `results` `createdAt` `reviewedAt`。

奖励到账、审核有结论时向用户推送 `promo.changed`（`{ campaignId }`）。

管理端：

```
GET    /api/admin/apps/{appKey}/promo/catalog                          平台目录、禁用平台、权益目录、默认配置与上限
GET    /api/admin/apps/{appKey}/promo/campaigns                        列表（status / keyword / page / limit），带简要统计
POST   /api/admin/apps/{appKey}/promo/campaigns                        新建（默认 draft）
GET    /api/admin/apps/{appKey}/promo/campaigns/{id}                   详情
PUT    /api/admin/apps/{appKey}/promo/campaigns/{id}                   修改
PUT    /api/admin/apps/{appKey}/promo/campaigns/{id}/status            { status }
DELETE /api/admin/apps/{appKey}/promo/campaigns/{id}                   删除（见上）
GET    /api/admin/apps/{appKey}/promo/campaigns/{id}/stats             统计
GET    /api/admin/apps/{appKey}/promo/campaigns/{id}/leaderboard       排行（不打码，带账号）
GET    /api/admin/apps/{appKey}/promo/submissions                      提交（campaignId / status / platform / userId / keyword / page / limit）
POST   /api/admin/apps/{appKey}/promo/submissions/review               { ids, action: approve|reject, reason } 批量审核
```

审核返回 `{ items: [ReviewOutcome{id, ok, status, reason, message, results}], succeeded }`。

统计：`participants`（领过档位或提交过的人数）、`invited`（活动时间内的有效邀请）、`pendingInvites`、
`tierClaims[{count,claimed}]`、`submissions{pending,approved,rejected}`、`byPlatform[{platform,name,pending,approved,rejected}]`、`daily[{date,invited,submitted,approved}]`。

## 验证

`go test ./internal/domain/promo/`（链接解析、配置校验、判定）与 `TestPromoIntegration`（真实 Postgres）：配置校验、
草稿不可见、活动开始前的邀请不计数、领取（重复 / 未达到 / 并发只成一次）、未开始的活动、提交（抖音禁止 / 平台不符 /
未启用平台 / 重复 / 平台与每人篇数）、审核（驳回要原因、驳回后可重交、通过发奖、重复审核、去重）、排行、统计、删除限制。

```
AEGIS_TEST_PG_DSN=postgres://aegis:<密码>@127.0.0.1:15432/aegis_promo_test go test ./internal/service -run TestPromoIntegration
```

`openTestDatabase` 会清空库，**不要指向开发库 `aegis`**，用临时库，测完 DROP。
