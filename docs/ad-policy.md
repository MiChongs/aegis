# 广告服务策略

用户是否同意广告服务记在**账号**上。应用开启策略后，拒绝或尚未选择的用户只能使用应用配置的
**基础服务**，其余功能需要先同意；有效会员可以免除（默认开启）。开屏广告的展示记录同样挂在账号上。

## 判定

服务模式只有一处判定（`adpolicy.Evaluate`），网关下发给客户端的 `mode` 与控制台用户详情里的 `mode` 都出自它：

| 情形 | mode |
|---|---|
| 策略未开启 | `full` |
| 会员身份免广告（`Entitlement.adFree`）且开启了会员免除 | `full` |
| 按**当前版本**条款同意过 | `full` |
| 拒绝、没选过，或按旧版本条款做的选择 | `basic` |

「会员身份免广告」不等于「是会员」：看广告赠送的会员按场景设置可以不免广告（见 [rewarded-ads.md](rewarded-ads.md#看广告赠送的会员权益)），
此时与非会员一样要同意广告服务、照常展示开屏。`/ads/policy` 同时给出 `vip` 与 `adFree`。

`decisionRequired` 单独回答「要不要请用户（重新）选择」：没选过，或选择所依据的条款版本低于当前版本。
会员免除时 `mode` 是 `full`，但 `decisionRequired` 照实回答 —— 会员看激励视频同样要先同意，只是不必在启动时追着问。

**条款版本**只能往上调。调高后所有人都要按新版本重新选择一次，在此之前按旧版本同意的人也只有基础服务。

## 未登录用户

App 不登录也能用，所以未登录时的选择由客户端记在本机，并按本机的选择决定服务模式。网关对未登录请求一律按「还没选」回答。

登录后以账号上的选择为准；账号上还没有选择时，客户端用 `ifAbsent: true` 把本机的选择同步上来（`source: guest_sync`）。
账号上已经有选择时 `ifAbsent` 不覆盖、原样返回已有的那一条：两台设备同时同步，只有先到的那一个算数。

## 未配置

应用没保存过策略时，`/ads/policy` 返回 `configured: false`，其余字段是默认值。客户端此时应按**自己内置的默认策略**处理，
而不是把默认值当成「不要求同意」—— 否则后端一上线，客户端里已经在执行的策略反而会失效。

## 开屏广告

一次开屏尝试结束后（展示后关闭、点击、加载失败或超时），客户端上报一条记录：

| status | 含义 |
|---|---|
| `shown` | 展示后关闭或跳过 |
| `clicked` | 展示并被点击 |
| `failed` | 加载或展示失败（`errorCode` / `errorMessage` 记平台给的原因） |
| `timeout` | 加载超时 |

- 带令牌时记到账号上，未登录时只有设备标识。账号只能来自上报时的令牌；这次开屏发生时没有登录、或登录的是另一个账号的记录，
  客户端置 `anonymous: true`，只留设备标识 —— 否则未登录时攒下的记录在登录后补报，会被记到登录的账号上。
- `eventId` 由客户端生成（8–64 位字母数字 `_` `-`），`UNIQUE(appid, event_id)` 保证离线重传不会记两笔。单条不合法时直接丢弃，不让整批失败。
- `occurredAt` 超出「7 天前到 5 分钟后」的按收到时间记。

频控只数 `shown` / `clicked`：登录用户按账号、未登录按设备。`splash.available` 是服务端的结论（开着、没被会员免除、
同意了、没超过每日次数与间隔）；未登录时服务端不知道本机的选择，同意这一条由客户端自己判断。

开屏请求要把 `adUserId` 原样传给广告 SDK（灰鲸 `HJSplashAdRequest` 的 `userId`），它与激励视频是同一个带签名的用户标识，
平台后台里同一个人的开屏与激励视频对得上。广告位 ID 留空时客户端使用包里内置的那一个。

## 接口

### 客户端（App Protocol v1）

| 目录 key | 方法 | 路径 | 登录 | 说明 |
|---|---|---|---|---|
| `adPolicy` | GET | `/ads/policy` | 可选 | 策略、基础服务清单、开屏配置与频控，以及当前账号的选择与 `mode` |
| `adConsent` | POST | `/ads/consent` | 需要 | `{accepted, version?, ifAbsent?, source?, deviceId?}`，返回记录后的策略视图 |
| `splashAdEvents` | POST | `/ads/splash/events` | 可选 | `{events: [...], deviceId?}`，每次最多 50 条 |

`/ads/policy` 的返回体：

```json
{
  "configured": true,
  "enabled": true,
  "consentVersion": 2,
  "policyUrl": "https://…",
  "vipExempt": true,
  "basicTools": ["todo", "stopwatch"],
  "splash": {
    "enabled": true, "placementId": "…", "minIntervalSeconds": 600, "dailyLimit": 3,
    "todayCount": 1, "lastShownAt": "…", "nextAvailableAt": "…", "available": false
  },
  "signedIn": true,
  "consent": { "accepted": true, "version": 2, "source": "app", "decidedAt": "…" },
  "vip": false,
  "adFree": false,
  "exempt": false,
  "decisionRequired": false,
  "mode": "full",
  "adUserId": "42.1a2b3c4d5e6f7a8b",
  "serverTime": "…"
}
```

`version` 不传按当前版本；高于当前版本或未知的 `source`（`app` / `web` / `guest_sync` 以外）返回 `40000`。
与当前选择完全相同（同意与否、版本都一样）的上报什么都不写，变更历史里不会留下一串重复的「同意」。

### 实时事件

| 事件 | 推给 | data |
|---|---|---|
| `ad.consent.changed` | 这个用户的全部在线连接 | `{accepted, version, source, mode}` |
| `ad.policy.changed` | 应用下的全部在线连接 | `{enabled, consentVersion}` |

在官网撤回同意后，App 收到 `ad.consent.changed` 即可立即切换到基础模式，不必等下次启动。

### 管理端（`app:read` / `app:write`，落在应用级兜底规则里）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / PUT | `/api/admin/apps/{appKey}/ad-policy/config` | 策略；GET 在没配过时返回默认值与 `configured: false` |
| GET | `/api/admin/apps/{appKey}/ad-policy/consents` | 用户当前的选择（`accepted` / `outdated` / `source` / `keyword`） |
| GET | `/api/admin/apps/{appKey}/ad-policy/consent-logs` | 选择的变更历史（`userId` / `keyword`） |
| GET | `/api/admin/apps/{appKey}/ad-policy/splash-events` | 开屏记录（`userId` / `status` / `keyword` / `start` / `end`） |
| GET | `/api/admin/apps/{appKey}/ad-policy/stats?days=14` | 选择分布、今日开屏与按天趋势（缺失日期补零） |
| GET | `/api/admin/apps/{appKey}/users/{userId}/ad-policy` | 一个用户的选择、服务模式、变更历史与开屏记录（`app_user:read`） |

## 存储

| 表 | 说明 |
|---|---|
| `app_ad_policies` | 一应用一行；基础服务清单以 JSONB 整体保存 |
| `user_ad_consents` | 用户当前的选择，`PRIMARY KEY (appid, user_id)` |
| `user_ad_consent_logs` | 选择的变更历史，只追加 |
| `app_splash_ad_events` | 开屏记录，`UNIQUE (appid, event_id)`；频控走 `status IN ('shown','clicked')` 的部分索引 |

迁移 `000093_ad_policy`。
