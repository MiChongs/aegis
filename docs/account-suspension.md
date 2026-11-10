# 账号封禁、全站封禁与申诉

本文是 Aegis 账号封禁（应用级 / 全站）、封禁通知与申诉复审的统一契约。App（Voyage Android）、官网、控制台都按这里对接。

## 1. 概念

| 名称 | 说明 |
| --- | --- |
| 应用级封禁 `level=app` | 只封当前应用里的这一个账号（`user_account_bans` 一条记录）。 |
| 全站封禁 `level=platform` | 封「这个人」：记在一条 `platform_bans` 上，主体是这个账号绑定的邮箱、手机号、第三方身份、用过的设备（只存 HMAC 摘要与打码值）。所有应用里与这些主体相连的现有账号都会被封（各自一条 `user_account_bans`，`level=platform`、`platformBanId` 指向同一条全站封禁）；之后用这些主体注册、登录也会被拦。解除全站封禁会一并解除所有连带的账号封禁。 |
| 封禁来源 `source` | `manual`（管理员）/ `risk`（风控平台）/ `platform`（全站封禁连带）。 |
| 原因类别 `reasonCode` | `policy` 违反使用条款、`fraud` 欺诈或作弊、`spam` 垃圾信息或批量注册、`abuse` 滥用服务、`security` 账号安全风险、`payment` 支付风险、`other` 其他。给用户看的原因优先用 `publicReason`（管理员写的对外说明），为空时用类别的标准文案（多语言）。内部原因 `reason` 只在控制台显示。 |
| 申诉 | 被封禁的用户提交一次复审请求。先由决策模型（风控复评 + 大模型）审，置信度不够时转人工。每条封禁同时只能有一条进行中的申诉。 |

## 2. 语言

支持 `en`、`zh-CN`、`zh-TW`、`ja`、`ko`。客户端每个请求带 `Accept-Language`（App 用系统 / 应用语言，官网用页面语言）。Aegis 登录、刷新时把它记到用户资料（`user_profiles.extra.locale`），之后的封禁通知、申诉结果通知都按这个语言发；取不到时 `zh-CN`。

`zh-Hans*`、`zh-CN`、`zh-SG`、`zh` → `zh-CN`；`zh-Hant*`、`zh-TW`、`zh-HK`、`zh-MO` → `zh-TW`；其余取主语言（`ja-JP` → `ja`），不支持的回落 `en`。

## 3. 被封禁时客户端拿到什么

登录、刷新令牌、带会话的业务接口在账号被封禁时返回：

- `40301` 永久封禁 / `40302` 临时封禁（HTTP 403），`message` 按 `Accept-Language` 本地化。
- **`data.suspension`**（新增，结构如下）。旧客户端只读 `message`，不受影响。

```jsonc
{
  "code": 40301,
  "message": "Your account has been suspended.",
  "data": {
    "suspension": {
      "id": 123,                       // user_account_bans.id
      "level": "app",                  // app | platform
      "type": "permanent",             // permanent | temporary
      "scope": "all",                  // login（仅禁止登录）| all（禁止全部访问）
      "reasonCode": "spam",
      "reasonTitle": "Spam or bulk registration",   // 类别的本地化标题
      "reason": "…",                   // 给用户看的原因（publicReason 或类别的标准说明），已本地化
      "startAt": "2026-11-11T02:00:00Z",
      "endAt": null,                   // 临时封禁的解封时间
      "appeal": {
        "allowed": true,               // 现在能不能提交申诉
        "blockedReason": "",          // 不能提交时的原因：not_appealable | open | limit | cooldown | disabled
        "token": "sus1.xxxxx",         // 申诉凭证（不登录也能查看与申诉，7 天有效）
        "status": "none",              // none | pending | reviewing | approved | rejected | withdrawn（最近一条）
        "appealId": null,
        "submittedAt": null,
        "decidedAt": null,
        "reply": "",                   // 审核给用户的回复（已本地化或原文）
        "remaining": 2,                // 这条封禁还能申诉几次（撤回的不算）
        "nextAllowedAt": null,         // 冷却中时下次可申诉的时间
        "minLength": 10,               // 申诉内容字数范围（按 Unicode 字符数）
        "maxLength": 2000
      }
    }
  }
}
```

## 4. 统一的客户端接口（App 与官网共用）

都在应用网关 `/api/v1/apps/{appkey}` 下（sealed 档按现有协议加密）。**凭证二选一**：请求体里的 `token`（申诉凭证，见上）或有效会话（`Authorization`）。官网 BFF 用同一组接口。

### 4.1 查看封禁 `POST /suspension`

请求 `{ "token": "sus1.…" }`（有会话时可省略）。返回 `{ "suspension": {…同上…}, "appeals": [AppealView…] }`；账号当前没有封禁时 `suspension` 为 `null`，`appeals` 仍返回历史。

```jsonc
// AppealView
{
  "id": 9,
  "status": "pending",          // pending | reviewing | approved | rejected | withdrawn
  "content": "我是正常用户……",
  "contactEmail": "a@b.com",
  "submittedAt": "…",
  "decidedAt": null,
  "decidedBy": "",               // model | human | ""
  "reply": ""                    // 给用户的回复
}
```

### 4.2 提交申诉 `POST /suspension/appeal`

请求 `{ "token"?: "…", "content": "…", "contactEmail"?: "…" }`。`content` 长度按应用申诉设置（默认 10–2000 字），`contactEmail` 可选（账号没绑邮箱时用于接收结果）。返回 `{ "appeal": AppealView, "suspension": {…} }`。

错误码：`42220` 申诉内容字数不在范围内（message 写明范围，`data.minLength` / `data.maxLength`）、`42221` 联系邮箱格式不对、`40422` 没有可申诉的封禁（已解封或凭证对应的封禁不存在）、`40150` 申诉凭证无效或过期、`40933` 已有进行中的申诉、`40935` 这条封禁不允许申诉、`42920` 申诉次数已用完、`42921` 冷却中（`data.nextAllowedAt`，ISO 时间）、`42222` 应用未开放申诉。撤回：`40423` 申诉不存在、`40949` 申诉已结案不能撤回。用被全站封禁的邮箱、手机号、第三方身份注册：`45100`（`data.platformSuspended=true`）。

### 4.3 撤回申诉 `POST /suspension/appeal/withdraw`

请求 `{ "token"?: "…", "appealId": 9 }`。还没结案的（`pending`、`reviewing`）都能撤回，已结案的回 `40949`。返回 `{ "appeal": AppealView, "suspension": {…} }`。

### 4.4 站内消息

申诉受理、审核结果、封禁、解封都会进消息中心（分类 `security`），动作路由键 `account/suspension`（App 打开封禁与申诉页）。

## 5. 通知

封禁、解封、申诉受理、申诉结果四类事件，按用户语言发：

- **邮件**（账号绑定了邮箱，或申诉时填了联系邮箱）：统一模板，版式与措辞参照主流平台的账号处置通知（标题如 *Your account has been suspended*），品牌（名称、Logo、强调色、页脚）取应用的邮件策略。
  - `account_suspended` 账号已被封禁：原因、期限、怎么申诉（有申诉页地址时给按钮）。
  - `account_reinstated` 账号已恢复：解封（含申诉通过、到期、管理员解除）。
  - `appeal_received` 已收到申诉：编号与预计处理方式。
  - `appeal_upheld` 申诉未通过：维持封禁，附审核回复。
- **站内消息**：同上四类，进消息中心与实时推送（用户能登录时可见）。

全站封禁连带的多个账号共用一个邮箱时只发一封。

## 6. 决策模型复审

申诉提交后异步复审（每分钟兜底认领没审过的）：

1. **风控复评**：从风控平台取这个账号近 30 天的最高风险分、拦截次数、命中规则、名单与关联账号，归成 `strong`（证据确凿）/ `medium` / `weak`（像误判）。
2. **大模型**：把封禁信息、风控证据、申诉内容（作为不可信数据）交给应用的 AI 通道（没有则用平台通道；都没有就跳过），要求输出 `verdict`（approve / reject / escalate）、`confidence`、给审核员看的摘要、给用户的回复（用户语言）。
3. **合并**（应用申诉设置 `autoReview`）：
   - `off`：只人工。
   - `suggest`：记下模型结论，等人工。
   - `auto`：模型 approve 且置信度 ≥ `approveThreshold` 且风控证据不是 `strong` 且封禁不是管理员手动下的（`autoApproveManual` 可放开）→ 自动通过；模型 reject 且置信度 ≥ `rejectThreshold` 且风控证据是 `strong` → 自动驳回；其余转人工。

**通过**：解除封禁（全站封禁整条解除），并按「一键解除风控」撤掉风控对这个账号的名单与复核单、清计数，加 `allowDays` 天白名单保护；发 `account_reinstated`。**驳回**：维持封禁，发 `appeal_upheld`。

## 7. 管理端接口

应用级（权限同用户封禁）：

- `GET /api/admin/apps/{appkey}/ban-appeals?status=&page=&pageSize=&q=` 列表；`GET …/ban-appeals/stats`
- `GET /api/admin/apps/{appkey}/ban-appeals/{id}` 详情（封禁、账号、模型结论、风控证据、同一封禁的历次申诉）
- `POST …/ban-appeals/{id}/approve` `{ "reply": "", "note": "" }`；`POST …/ban-appeals/{id}/reject` `{ "reply": "", "note": "" }`；`POST …/ban-appeals/{id}/review`（重新跑模型复审）
- `GET|PUT /api/admin/apps/{appkey}/ban-appeals/settings`

```jsonc
// 申诉设置
{
  "enabled": true,
  "autoReview": "suggest",        // off | suggest | auto
  "approveThreshold": 0.85,
  "rejectThreshold": 0.9,
  "autoApproveManual": false,
  "allowDays": 30,                 // 通过后白名单保护天数
  "maxAppealsPerBan": 2,
  "cooldownHours": 24,
  "minLength": 10,
  "maxLength": 2000,
  "appealUrl": "https://example.com/appeal",   // 邮件里「提交申诉」按钮的地址，凭证会以 ?token= 拼上；空则提示到 App 里申诉
  "aiConfigId": 0                  // 指定 AI 通道；0 = 自动
}
```

封禁（已有接口扩展）：`POST /api/admin/apps/{appkey}/users/{userId}/bans` 请求体新增 `level`（app / platform）、`reasonCode`、`publicReason`、`appealable`（默认 true）、`notify`（默认 true，是否发封禁通知）。

全站（超级管理员）：

- `GET /api/admin/system/platform-bans?status=&page=&q=`、`GET /api/admin/system/platform-bans/{id}`（主体打码列表、连带账号）
- `POST /api/admin/system/platform-bans/{id}/revoke` `{ "reason": "" }`
- `GET /api/admin/system/ban-appeals?…`（全部应用的申诉，字段同上，带 `appid`、`appName`）

风控平台：场景处置 `enforcement.level`（app / platform）；人工处置 `targets` 含 `ban_account` 时可带 `level`。风控下发的封号 `reasonCode` 默认 `fraud`，`publicReason` 用场景配置的对外说明（为空用类别标准文案）。

## 8. 客户端要做的

- **App**：所有 Aegis 请求带 `Accept-Language`；登录 / 刷新 / 会话失效收到 40301、40302 时打开「账号已被封禁」页（原因、期限、范围、申诉入口与进度，用 `data.suspension`）；没有 `data` 的旧服务端照旧只显示 message。申诉页提交、撤回、刷新状态都走第 4 节接口；消息中心动作 `account/suspension` 打开同一页。
- **官网**：登录失败遇到 40301 / 40302 时进 `/account/suspended`（凭证存 httpOnly cookie，不放在地址栏里）；邮件里的申诉链接 `/appeal?token=…` 打开同一页；BFF 只转发第 4 节接口。
