# 对外用户 ID

> 面包屑：[Aegis](../CLAUDE.md) › 对外用户 ID

`users.id` 是 BIGSERIAL：挂在 App 资料页、排行榜、OAuth2 `sub` 上，等于公开了注册顺序和用户总量，
也让「挨个试 ID」变得很便宜。迁移 000112 给每个用户发一个随机的 **对外用户 ID**（`users.public_id`）：
凡是用户或第三方能看到的地方只给它，`users.id` 退回成纯内部主键（外键、管理端、日志）。

## 形状

16 位十进制数字，取值 `[1000000000000000, 9007199254740991]`。

- **仍是整数。** 老客户端把 `id` / `userId` 当 Long / number 解析，换的是值不是类型，已发布的版本照常工作。
- **上界是 2^53 − 1。** JavaScript 的 Number 能精确表示，前端不会丢尾数。
- **号段约 8×10^15。** 主键远小于下界（10^15），凡是接受「用户 ID」的入口都可以按大小区分两种 ID，不会混淆。
- **随机源是 `gen_random_uuid()`**（强随机数），不是可预测的 `random()`。

## 长期稳定

| 保证 | 落点 |
|---|---|
| 每个用户都有号 | 列默认值就是生成函数 `aegis_new_user_public_id()`：任何建号路径（注册、短信、第三方、卡密、导入、迁移、管理员新建）不写这一列也拿得到号；`NOT NULL` + `CHECK` 号段 |
| 不重复 | 唯一索引 `uq_users_public_id`；生成时查重，32 次都撞上才报错（实际不可能） |
| 终生不变 | 触发器 `trg_users_public_id_guard` 拒绝任何改号的 `UPDATE` |
| 注销后不复用 | 同一个触发器在 `DELETE` 时把号写进 `user_public_id_retired`，生成函数跳过退役号；第三方按 `sub` 存的数据不会串到新用户身上 |
| 回滚不丢号 | down 迁移先把全部号写进退役表再删列；重新上线时不会把发过的号发给别人 |

存量用户由迁移逐行回填（唯一索引先于回填建好，查重走索引）。迁移可重复执行。

## 哪些地方给对外 ID

### 调用者自己的 ID：响应层统一换

用户端几十种结构（签到记录、积分 / 钱包 / 会员流水、订单、兑换、抽奖、通知、设置、工单、第三方绑定……）
里都挂着调用者自己的 `userId`，这些结构与管理端共用，逐个改字段改不全、还会把管理端改坏。

落点在响应层：`middleware.Auth` / `OptionalAuth` 认出会话后调用 `response.SetUserIDAlias(主键, 对外 ID)`，
`writeJSON` 把数据里**名字是用户 ID、值恰好是调用者主键**的字段改写为对外 ID
（`pkg/response/user_id_alias.go`）。名字规则：`uid`、`userId`、`user_id`、`*UserId`、`*_user_id`、`uploadedById`。

- 名字叫 `id` 的字段不碰（订单号、流水号与主键撞值是常事），需要的地方显式给：`/me` 的 `id`、等级资料的 `userInfo.id`。
- 别人的 ID 不碰，由各自的接口显式换算（下一节）。
- 以后新增的用户端接口不用做任何事，自己的主键自动不出门。
- 实时推送（`/api/ws`）的每一帧走同一套改写（`realtimeClient.enqueue`）。

### 显式换算

| 位置 | 做法 |
|---|---|
| `/me`、`/auth/*` 登录结果 | `MyView.ID`、`LoginResult.PublicUserID`（JSON `userId`；`LoginResult.UserID` 是主键，`json:"-"`） |
| 资料 `/me/profile` | 传输层返回副本，`userId` 为对外 ID |
| 新排行榜行、老排行榜 | `LeaderboardService.publicizeRows`、`publicizeRankingUsers`；换不到的行省略 / 写 0 |
| 公开站点列表 | `Handler.publicSites` |
| 头像地址 | 令牌载荷 `p{appid}.{对外 ID}`；切换前流出去的 `u{appid}.{主键}` 永远有效 |
| 激励广告 / 开屏的用户标识 | `{对外 ID}.{签名}`；回调两种形态都认（切换前发出、还没回调的是主键形态） |
| OAuth2 / OIDC `sub` | 对外 ID；见下 |
| 服务端会员校验 `/vip/verify` | `userId` 为对外 ID |
| 访问令牌 | 不再含用户标识：令牌已改为 PASETO v4.local，载荷加密、只有 `jti` 等会话字段（见 user-auth-and-app-transport-encryption.md 6.1.1）；升级前的 JWT 过渡期内 `uid` claim 为对外 ID |
| 远程函数 | 用户对象多了 `publicId`；`aegis.user.get(id)` 等既收主键也收对外 ID |

### OAuth2 / OIDC

`sub` 是对外 ID。切换前签发、仍在有效期内的 Hydra 登录态与刷新令牌里，`sub` 还是旧的主键数字：

- `ResolveOAuth2Subject` 两种都认（≥ 10^15 按对外 ID 查，否则按主键）。
- 记住登录态（skip）时 Hydra 要求沿用原 subject，所以旧会话在过期前继续给旧 `sub`。
- 授权列表与撤销同时覆盖两种 subject。

接入方若按 `sub` 存过用户，用户下一次完整登录时会以新 `sub` 出现，需要按账号或邮箱重新关联一次。

## 管理端

管理端照旧以主键寻址（路由、筛选、审计），用户视图多一个 `publicId`。用户报来的 ID 可以直接搜：
关键字是 16 位纯数字，或 `#` 加 16 位数字，按对外 ID 直达；`#` 加较小的数字仍按主键直达。

## 客户端须知

- 用户 ID 的类型不变（整数），值变成 16 位。本地以用户 ID 为键存的东西（会话归属、广告豁免台账、同步标记）
  在升级后第一次拉取 `/me` 时会看到「换了个人」，按换号处理一次即可。
- 激励广告标识点号前那段与 `/me` 的 `id` 是同一种 ID，可以继续用来核对归属。
