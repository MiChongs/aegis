# 发布中心

应用的版本发布：多安装包、富文本与 Markdown 更新说明、定时发布、渠道、可见范围、定向、灰度与更新漏斗统计。
与旧的 `/versions` 接口共用 `app_versions` 表；旧接口原样保留，旧的检测接口改走同一套检测引擎。

## 概念

| 概念 | 说明 |
|---|---|
| 版本 | 版本名 + 单调递增的版本码；同平台同渠道下未撤回的版本码不能重复 |
| 安装包 | 一个版本挂多个，按 `abi` 区分（`arm64-v8a`、`armeabi-v7a`、`x86`、`x86_64`、`universal` 或任意小写标识）；地址可以是外链，也可以是上传到对象存储得到的 `storage://` 引用 |
| 更新说明 | 两种撰写格式，见下节「更新说明」。无论哪种，`notes` 都是净化后的 HTML，服务端另提取纯文本摘要 `summary` |
| 更新类型 | `optional` 可选 / `recommended` 推荐 / `force` 强制 |
| 最低支持版本码 | `minSupportedCode`：低于它的客户端必须更新 |
| 渠道 | 版本挂在某个渠道上时，只有该渠道成员能收到；默认渠道 = 所有人；不挂渠道 = 所有渠道。渠道可开放「自助加入」（如 Beta 体验计划） |
| 可见范围 | `public` 任何人（含未登录与官网） / `signed_in` 仅登录用户 / `testers` 仅内测名单 |
| 定向 | 地区、语言（前缀匹配）、机型（包含匹配）、ABI、系统版本范围、来源版本范围、排除名单，同时设置时取交集 |
| 内测名单 | 用户 ID 或设备 ID；命中即收到，跳过定向与灰度（排除名单仍然优先） |
| 灰度 | `rolloutPct` 0–100，按「版本 ID + 用户（未登录时为设备）」稳定哈希分桶：放量变大时已进入的人不会被踢出。未登录又没有设备 ID 的请求不参与非全量灰度 |

### 更新说明

| 字段 | 说明 |
|---|---|
| `notesFormat` | `html`（富文本编辑器）或 `markdown`；存量版本与旧接口改过正文的版本都是 `html` |
| `notesMarkdown` | `notesFormat=markdown` 时的 Markdown 原文，否则不出现 |
| `notes` | 净化后的 HTML。`markdown` 格式下由服务端从原文渲染再净化，只认 HTML 的客户端（官网、电脑版、iOS、旧版 App）照旧读它 |
| `summary` | 纯文本摘要，≤160 字 |

Markdown 的渲染规则（客户端自己排版时照这几条来，两边才长得一样）：

- CommonMark + GFM：表格、删除线、任务列表、裸链接自动识别；
- 段落里的单个换行就是换行，不折成空格；
- 允许行内 HTML，但只有白名单里的能留下：文字颜色写作 `<span style="color:#D32F2F">…</span>`
  或 `<font color="#D32F2F">…</font>`，`style` 只认 `color`、`background-color`，取值只认十六进制与 `rgb()`/`rgba()`；
- 原文 ≤20000 字符，换行统一为 `\n`。

白名单与公告共用：公告正文里的文字颜色同样会保留。旧接口（`/versions`）改了 `release_notes` 时格式退回 `html`、原文清空。

### 状态

```
draft ──发布──▶ published ⇄ paused（暂停）
  │                │
  └─定时发布─▶ scheduled（到点即视为 published，无需定时任务）
任意状态 ──撤回──▶ revoked；只有非下发中的版本可以删除
```

`effectiveStatus` 是计入定时发布之后的实际状态。

### 检测规则

1. 候选：下发中的版本、平台匹配（`all` 匹配一切）、版本码高于当前、渠道 / 可见范围 / 定向 / 灰度全部通过
2. 目标：候选中版本码最高的那个；按客户端 `abis` 的偏好顺序选包，没有匹配时用 `universal`，再没有用第一个
3. 更新类型取跨越的全部版本中最重的一档：中间任何一个是强制，结果就是强制（`forceReason=skipped`）；
   目标本身强制为 `release`；当前版本低于任一跨越版本的 `minSupportedCode` 为 `unsupported`
4. `changelog` 列出跨越的全部版本（最多 20 个，倒序）
5. 有更新时计一次「下发」；漏斗事件按「主体 + 版本 + 事件 + 天」去重

## 网关接口（`/api/v1/apps/{appKey}`）

检测、最新版本、历史与上报免登录；带 Bearer 令牌时按该用户定向，令牌无效按未登录处理。
Kotlin SDK：`client.releases.*`（已登录时自动带令牌）。

### `GET /releases/check`

| 参数 | 说明 |
|---|---|
| `versionCode` | 必填，当前版本码 |
| `platform` | 默认 `android`。取值：`android`、`android_tv`、`ios`、`harmony`、`windows`、`macos`、`linux`、`web`；版本的平台为 `all` 时各平台都收。电视版（另一个安装包）报 `android_tv`，与手机版的 `android` 互不下发 |
| `abis` | 逗号分隔，按偏好排序（Android：`Build.SUPPORTED_ABIS`） |
| `osVersion` | 系统版本（Android：API Level） |
| `deviceId` | 设备标识，同一设备保持不变；用于灰度与内测名单 |
| `model` / `locale` / `region` | 机型、语言标签（`zh-CN`）、地区码（`CN`） |

响应 `data`：

```json
{
  "hasUpdate": true,
  "currentVersionCode": 1000901,
  "updateType": "force",
  "forceReason": "skipped",
  "release": {
    "id": 12, "version": "1.1.0", "versionCode": 1010099, "title": "秋季更新",
    "notes": "<h2>修复</h2><ul><li>…</li></ul>", "notesFormat": "markdown", "notesMarkdown": "## 修复\n- …",
    "summary": "…", "platform": "android", "minOsVersion": "",
    "updateType": "optional", "channel": {"id": 3, "code": "beta", "name": "Beta", "level": "beta"},
    "assets": [{"id": 40, "abi": "arm64-v8a", "label": "", "downloadUrl": "https://…", "fileSize": 52428800, "sha256": "…", "position": 0, "downloadCount": 0}],
    "publishedAt": "2026-10-08T02:00:00Z"
  },
  "asset": {"id": 40, "abi": "arm64-v8a", "downloadUrl": "https://…", "fileSize": 52428800, "sha256": "…"},
  "changelog": [{"version": "1.1.0", "versionCode": 1010099, "title": "…", "summary": "…", "notes": "<p>…</p>", "notesFormat": "html", "updateType": "optional", "publishedAt": "…"}],
  "checkedAt": "2026-10-08T03:00:00Z",
  "nextCheckAfter": 3600
}
```

没有更新时只有 `hasUpdate=false`、`currentVersionCode`、`checkedAt`、`nextCheckAfter`。
`downloadUrl` 对存储中的安装包是永久代理地址（签名令牌，不依赖 Redis），安装包被删除后才会失效。

### `GET /releases/latest`

参数：`platform`、`deviceId`。返回这个人此刻能拿到的最新版本（不比较版本码，结构同 `release`）；没有时 `data` 为 `null`。
**未登录时只返回公开且已全量的版本**；登录后按用户的渠道、定向与灰度返回。官网下载区用它。

### `GET /releases`

参数：`page`、`limit`（≤50）、`platform`、`deviceId`。可见的版本历史，可见性规则同 `latest`。
`data`：`{items, page, limit, total, totalPages}`。

### `POST /releases/events`

```json
{"releaseId": 12, "event": "downloaded", "assetId": 40, "deviceId": "…"}
```

`event`：`downloaded`（开始下载）/ `installed`（新版本首次启动）/ `failed`（下载或校验失败）/ `dismissed`（用户跳过此版本）。

### 渠道自助加入（需登录）

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/releases/channels` | 开放自助加入的渠道与默认渠道，带 `joined` |
| `POST` | `/releases/channels/{code}/join` | 加入，返回最新渠道列表 |
| `POST` | `/releases/channels/{code}/leave` | 退出，返回最新渠道列表 |

### 实时事件

版本发布、修改（下发中）、暂停、恢复、撤回、调整灰度时，向该应用的全部在线连接推送 `release.changed`：
`data = {action, releaseId, version, versionCode, updateType, platform}`，`action` 为 `published` / `updated` / `paused` / `revoked`。
客户端收到后重新检测即可，事件本身不携带安装包地址。

## 管理端接口（`/api/admin/apps/{appKey}`，权限 `version:read` / `version:write`）

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/releases` | 列表：`status`、`platform`、`channelId`、`keyword`、`page`、`limit` |
| `POST` | `/releases` | 新建（落在草稿） |
| `GET` | `/releases/overview` | 总览：各状态计数、灰度中数量、各渠道最新下发版本、近 14 天漏斗 |
| `POST` | `/releases/assets` | 上传安装包（multipart，`file` 字段，≤2 GB），返回 `{reference, downloadUrl, fileName, fileSize, sha256, abi}`；`reference` 填进安装包的 `url`，`abi` 由文件名猜出 |
| `POST` | `/releases/assets/presign` | 申请浏览器直传：`{fileName, fileSize, contentType?, configName?}` → `{mode, method, uploadUrl, headers, token, expiresAt, provider, reason}`。`mode=direct` 时浏览器带 `headers` 直接 `PUT` 到 `uploadUrl`（1 小时内有效）；`mode=server` 表示当前存储不支持直传，改用上面的 multipart 接口 |
| `POST` | `/releases/assets/complete` | 直传完成：`{token, sha256?}` → 与 multipart 上传相同的结果。服务端 HEAD 一次确认对象存在且大小与申请时一致，令牌只能用一次 |
| `POST` | `/releases/notes/preview` | 更新说明预览：`{format, markdown, notes}` → `{format, html, summary}`，与保存走同一条渲染与净化，不落库 |
| `POST` | `/releases/simulate` | 模拟检测：`{versionCode, platform, userId, deviceId, abis, osVersion, deviceModel, locale, region}` → `{result, decisions}`，`decisions` 给出每个候选版本能否收到与原因 |
| `GET` | `/releases/{rid}` | 详情 |
| `PUT` | `/releases/{rid}` | 更新（字段缺省不修改；`assets` 提交即整组替换，带 `id` 的保留下载计数；`channelId` 为 `null` 或 `0` 表示不限渠道） |
| `DELETE` | `/releases/{rid}` | 删除（下发中的不能删） |
| `POST` | `/releases/{rid}/publish` | `{publishAt?}`：省略或已过去为立即发布，否则定时发布；没有安装包不能发布 |
| `POST` | `/releases/{rid}/pause` | 暂停下发 |
| `POST` | `/releases/{rid}/resume` | 恢复下发 |
| `POST` | `/releases/{rid}/revoke` | 撤回 |
| `PUT` | `/releases/{rid}/rollout` | `{rolloutPct}` 调整灰度 |
| `GET` | `/releases/{rid}/stats` | 漏斗：`{total, daily[]}`，近 14 天 |

新建 / 更新的请求体：

```json
{
  "version": "1.1.0", "versionCode": 1010099, "title": "秋季更新",
  "notesFormat": "markdown", "notesMarkdown": "## 新增\n- **钢琴**\n- <span style=\"color:#D32F2F\">重要</span>",
  "platform": "android", "minOsVersion": "7.0", "updateType": "recommended", "minSupportedCode": 1000099,
  "visibility": "public", "rolloutPct": 20, "channelId": null,
  "targeting": {
    "testerUserIds": [1], "testerDeviceIds": [], "excludeUserIds": [], "excludeDeviceIds": [],
    "regions": ["CN"], "locales": ["zh"], "deviceModels": [], "abis": [],
    "minOsVersion": 24, "maxOsVersion": 0, "minSourceVersionCode": 0, "maxSourceVersionCode": 0
  },
  "assets": [{"id": 0, "abi": "arm64-v8a", "label": "", "url": "storage://1/apps%2F10000%2Freleases%2F…", "fileSize": 52428800, "sha256": "…"}]
}
```

管理端视图在公开视图之外还有：`status`、`effectiveStatus`、`visibility`、`targeting`、`rolloutPct`、`minSupportedCode`、
`publishAt`、`downloadCount`、`createdBy`、`createdAt`、`updatedAt`，以及安装包的落库地址 `url`。

渠道沿用 `/channels` 接口，新增字段 `self_join`（允许用户自助加入）。渠道上旧的 `rollout_pct`、`rules`、
`platforms`、`min/max_version_code` 不参与新的检测，灰度与定向一律设在版本上。

## 浏览器直传与本地解析

控制台发布编辑器在浏览器里解析安装包（APK 的清单、资源表、v2/v3 签名证书，AAB 的 protobuf 清单，
IPA 的 Info.plist，HAP 的 module.json），自动填写版本名、版本码、平台与最低系统要求，
并在本地计算 SHA-256。文件随后直接 `PUT` 到对象存储，不经过 Next.js 代理与 Aegis 进程。

- 支持直传的存储：S3 兼容（含 R2）、MinIO、RustFS、阿里云 OSS、腾讯云 COS。其余存储（本地磁盘、WebDAV、网盘类）自动退回经服务器上传（受同源代理 1 GB 上限约束）。
- 存储桶需要允许控制台域名跨域 `PUT`（CORS：`AllowedOrigins` 为控制台地址，`AllowedMethods` 含 `PUT`，`AllowedHeaders` 含 `Content-Type`）。未配置时浏览器会拦下请求，控制台自动改为经服务器上传并提示原因。
- 直传时 SHA-256 由浏览器计算后随 `complete` 提交，服务端只能核对大小；经服务器上传时服务端边传边算，与本地值不一致时控制台判为上传失败。

## 旧接口

`GET /version/check`（网关）与 `GET /api/user/check-version` 改走新引擎，响应仍是旧形状
`{version: AppVersion, channelName}`，没有更新时 40430。新接入请用 `/releases/check`。
