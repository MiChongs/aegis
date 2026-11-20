# 账号初始化与多标识登录

第三方（QQ、微信……）、短信、卡密这类快速登录在首次登录时直接建号：账号名由系统生成
（`qq_<openid>`、手机号、卡面），也没有密码。账号初始化在登录之后引导用户补设一次用户名和密码。
设好之后，用户可以用用户名、邮箱、手机号或用户 ID 加密码登录。

## 数据

- `user_account_setup`（迁移 000139）：每个快速登录建的号一行，自己注册的账号没有行。
  - `account_pending`：用户名还是系统生成的。用户自助设过一次，或管理员改过名，即为 false。
  - `prompted_at`：首次弹窗已展示。之后只出横幅。
  - `snoozed_until`：用户关掉横幅后，在此之前不再提醒。
  - `completed_at`：用户名已设且有密码。读状态时按 `users.password_hash` 对齐，所以改密、找回密码、
    管理员重置都不需要回头同步这张表。
- 存量回填只在建表那一次做：按 `user_profiles.extra.register_method` 认出 oauth / sms / cardkey 建的号；
  账号名仍是系统生成的（`<provider>_…`、等于档案手机号、卡密）才算用户名待设。
- 建号时登记：`createOAuthUser` / `createPhoneUser` / `createCardKeyUser` 调 `trackAccountSetup`。
  不管策略开没开都登记，策略只决定提不提醒。

## 应用策略

`apps.settings.accountSetup`，控制台应用详情「认证与安全 → 账号初始化」（`?tab=account-setup`）。没配置过时用默认策略。

| 字段 | 默认 | 说明 |
|---|---|---|
| `enabled` | true | 关闭后不再提醒 |
| `sources` | `oauth`、`sms` | 哪些建号方式要引导，可加 `cardkey` |
| `requireAccount` | true | 要求设置用户名 |
| `requirePassword` | true | 要求设置密码 |
| `firstPrompt` | `dialog` | `dialog`：第一次弹窗，之后横幅；`banner`：一律横幅 |
| `snoozeHours` | 72 | 关掉横幅后多久再出现；0 表示横幅不能关闭 |

开启时至少选一种建号方式，且至少要求用户名或密码之一（40968）。

## 网关接口（需要登录）

| 接口 | 说明 |
|---|---|
| `GET /me/account-setup` | 状态：`required`、`accountRequired`、`passwordRequired`、`prompt`（none / dialog / banner）、`dismissible`、`suggestions`、`rules` |
| `POST /me/account-setup` | `{account?, password?}`，两项可以分开提交 |
| `POST /me/account-setup/check` | `{account}` → `{available, message}` |
| `POST /me/account-setup/prompted` | 首次弹窗已展示 |
| `POST /me/account-setup/dismiss` | 关掉横幅；`snoozeHours = 0` 时无效 |

客户端流程：登录后（以及回到前台时）读状态。`prompt = dialog` 时弹窗并立即上报 `prompted`，
不管用户填没填；`prompt = banner` 时在首页显示横幅，关闭时上报 `dismiss`。

规则：

- 用户名 4–24 个字符，以字母或汉字开头，只含字母、汉字、数字、`_`、`.`、`-`，不能以点结尾或含连续的点，
  不能用保留名。所以用户名永远不是纯数字（不会和用户 ID、手机号混淆），也不含 `@`（不会和邮箱混淆）。
  占用按忽略大小写判断，注销后仍在保留期内的名字也算占用。
- 用户名只能自助设一次（40964）。传与当前相同的名字表示沿用，名字须符合规则。
- 密码只在还没有密码时能设（40967），已有密码走 `/auth/password/change`。
- 两项一起提交时先全部校验再落库：密码不合规时用户名也不改。
- 改名后同步已签发会话里的账号名（Redis，保留剩余有效期）、清资料缓存、更新管理端搜索索引。
  令牌里的 `account` 声明要到下次刷新才变。

错误码：40963 用户名已被使用，40964 用户名已设置，40967 已设置过密码，40005 用户名不合规。

## 多标识登录

`POST /auth/login`（password）的 `account` 可以是以下任意一种，按顺序认：

1. 账号名（始终可用，老账号可能就叫一个邮箱或一串数字）；
2. 用户 ID：16 位对外 ID（`identifiers` 含 `id`）；
3. 号码：5–12 位、不以 0 开头的纯数字，只在本应用里找（`identifiers` 含 `number` 或 `id`，见 [user-number.md](user-number.md)）；
4. 邮箱：含 `@`，忽略大小写（`identifiers` 含 `email`）；
5. 手机号（`identifiers` 含 `phone`）。

像号码的纯数字可能同时是老系统导入的纯数字账号名、别人的号码、11 位手机号：这时账号名命中也不提前返回，
三类候选合在一起由密码决定。换下来的旧号冻结期内不能再用来登录。

邮箱或手机号挂在多个账号上时最多取 5 个，密码对得上的那个就是要登录的人。都对不上时统一返回 40101，
不透露账号是否存在。`identifiers` 在接入策略里配置并随 `/config` 下发，客户端据此提示「用户名 / 邮箱 / ID」。
迁移 000139 给存量策略加上了 `id`，管理员可以在接入策略里关掉。

## 管理端

| 接口 | 权限 | 说明 |
|---|---|---|
| `GET / PUT /api/admin/apps/:appkey/account-setup` | app:read / app:write | 策略与统计 |
| `PUT /api/admin/apps/:appkey/users/:userId/account` | app:user:write | 改用户名。沿用注册账号规则（3–64 字节、不含空白），不限次数，改完算用户名已设置 |
| `GET /api/admin/apps/:appkey/users/:userId/account-setup` | app:user:read | 某个用户的初始化情况 |
| `POST /api/admin/apps/:appkey/users/:userId/account-setup/reset` | app:user:write | `{resetAccount}` 要求重新设置：提醒从首次弹窗重新开始，`resetAccount` 时用户名也重新算作待设置 |

测试：`TestAccountSetupIntegration`（真实 Postgres + miniredis + 假 graph.qq.com）。
