# 云服务

> 面包屑：[Aegis](../CLAUDE.md) › docs/cloud-service

云服务是[用户云存储](cloud-storage.md)的升级：在「命名空间 / 键」的文档存储之上，加了三块能力。

| 能力 | 是什么 |
|---|---|
| 档位 | 按会员档位区分的存储与同步服务：空间、设备数、历史保留、实时、接力、可同步的内容 |
| 同步 | 记录级的跨设备实时同步：听歌、影视、番剧、短剧、小说、听书…… |
| 设备 | 登记过的同步设备、各自同步了什么、此刻在播放 / 阅读什么，以及把内容接力到另一台设备 |

开关、存储目标、档位都在控制台 `/apps/{appKey}?tab=cloud-storage` 里按应用配置。

参考：同步模型借鉴 [Firefox Sync Storage 1.5](https://mozilla-services.github.io/syncstorage-rs/syncstorage/api-1.5.html)
的「集合 + 记录 + 服务端修改序号」，冲突规则借鉴 [Kotatsu 同步服务](https://github.com/KotatsuApp/kotatsu-syncserver)
的逐条最后写入者胜，设备列表与接力借鉴 Firefox 的 clients 集合与「发送标签页」。只借鉴协议语义，没有搬代码。

## 档位

应用配置上的配额、单条目上限、条目数、修订数、回收站天数是**基础档**。`plans` 是一个按优先级排列的档位表：

```jsonc
{
  "tierId": 5,                 // 会员档位 ID；0 = 免费档（没持有任何档位的人）
  "name": "SVIP",              // 留空时用档位名
  "description": "5 GiB · 6 台设备 · 实时同步",
  "quotaBytes": 5368709120, "maxItemBytes": 26214400, "maxItems": 20000,
  "maxRevisions": 30, "trashRetentionDays": 60,
  "syncEnabled": true,         // 有没有多设备同步
  "maxDevices": 6,             // 能登记几台设备
  "maxSyncRecords": 200000,    // 同步记录（不含墓碑）上限
  "historyRetentionDays": 365, // 历史类集合在云端留多少天，0 = 永久
  "realtime": true,            // 实时推送；关掉时客户端按 pollSeconds 轮询
  "pollSeconds": 300,
  "handoff": true,             // 能否接力到另一台设备
  "collections": []            // 可同步的集合，空 = 全部
}
```

**生效规则**：用户此刻持有的会员档位（`vip.Holdings.Tiers`，与会员判定同一份事实）里，排在表中最前面的那一档；
都没有时用免费档（`tierId: 0`）；没配免费档时用基础档（开同步、实时、接力、3 台设备，与升级前的行为一致）。
管理员在用户行上设的配额覆盖值仍然优先于档位配额。

档位影响到的既有路径：条目写入、配额、单条目上限、条目数、修订保留、回收站保留期全部按用户此刻的档位算。
会员到期后档位自动回落：已经写进去的不删，超出的配额只是写不进新东西；历史保留期变短的，后台按新档位裁剪。

`GET /cloud` 的响应多了 `plan`（当前档位）、`itemBytes`、`syncBytes`、`syncRecords`；`usedBytes` 是条目与同步记录之和。
`GET /cloud/plans` 是档位对比（免费档在前，`current` 标出当前档位）。

## 同步

### 集合与记录

一类数据一个集合，名字是「领域.名称」：`^[a-z][a-z0-9]{0,23}\.[a-z][a-z0-9_-]{0,39}$`。
集合里一条记录一行：

```jsonc
{
  "collection": "music.history",
  "id": "kw:12345",            // 1–200 个可见 ASCII 字符，规则见下
  "payload": { ... },          // JSON 对象，≤ 16 KiB；墓碑没有
  "updatedAt": "2026-11-28T08:00:00.123Z",  // 客户端修改时间（毫秒）
  "deleted": false,
  "deviceId": "…", "deviceName": "Xiaomi 17 Pro Max",
  "seq": 1024,                 // 这一版被接受时拿到的序号（按用户单调递增）
  "size": 312, "createdAt": "…"
}
```

**谁赢**：逐条比较 `updatedAt`，新的胜；同一毫秒按设备号字典序，大的胜（各端算出的胜者相同）。
比服务端快超过 5 分钟的时间按服务端时间算，免得一台时钟错乱的设备永远赢。

**删除**留墓碑（`deleted: true`、内容清空），别的设备拉到后跟着删。墓碑保留 90 天后清除，同时把用户的
「墓碑地平线」推到被清掉的最大序号；带着比地平线还旧的游标来拉取的设备拿到 `reset: true`。

**历史保留**：目录里种类为 `history` 的集合，按档位的 `historyRetentionDays` 在云端裁剪（直接清除、不留墓碑：
这是云端保留期到了，不是用户删除，各设备本地的历史不受影响）。后台清理每小时跑一次。

### 一轮同步

```
启动 / 登录    PUT  /cloud/sync/device                 登记当前设备（超出设备数 → 41364，引导移除旧设备）
本地有改动     POST /cloud/sync/push {changes:[…]}      至多 500 条一批
              ← accepted / seq / applied[{collection,id,seq}] / rejected[云端那一版]
拉取          GET  /cloud/sync/changes?since=N&collections=a,b&limit=500
              ← records（含墓碑，按 seq 升序）/ next / hasMore / reset
```

- 推送：本地每条改动记一个「脏」标记，推送成功（出现在 `applied` 或 `rejected`）后清掉。`rejected` 里是云端更新的那一版，直接写回本地。
- 拉取：带上次保存的 `next`；`hasMore` 时立刻接着拉；`reset` 时把游标归零全量拉一遍，按同一规则与本地合并
  （合并可重入：同一条记录合并多少次结果都一样），然后把本地所有记录重新标脏推一遍。
- 拉到的记录写回本地时不要再标脏（否则会无限回推）。拉到自己设备写的记录（`deviceId` 是自己）照常合并即可。
- 先推后拉，推完 `seq` 不要直接当游标（中间可能有别的设备的写入）：游标只用拉取返回的 `next`。

### 实时

实时连接（`GET /api/ws`）上会收到：

| 事件 | data | 客户端做什么 |
|---|---|---|
| `cloud.sync` | `seq`、`deviceId`、`collections`、`count` | 不是自己推的就拉一次（`collections` 可以只拉这些） |
| `cloud.devices` | `deviceId`、`change`（registered / renamed / removed） | 刷新设备列表；`removed` 且是自己时停止同步并提示 |
| `cloud.activity` | `deviceId`、`activity` | 更新那台设备的「正在播放」 |
| `cloud.handoff` | `handoff` | `toDeviceId` 是自己时弹出「继续播放 / 阅读」 |

事件只是「去拉一下」的提示，不带记录内容；连接断开期间的事件不会补发，重连后拉一次。
档位 `realtime: false` 时服务端不推 `cloud.sync` / `cloud.activity`，客户端按 `pollSeconds` 轮询（实时档位也用它兜底）。

## 设备

| 接口 | 说明 |
|---|---|
| `GET /cloud/sync` | 首页：档位、用量、设备、各集合（目录里的全部列出）的条数 / 字节 / 最后修改 / 按设备分布、当前 seq |
| `GET /cloud/sync/devices` | 设备：在线（3 分钟内有心跳、拉取或上报）、最后同步、正在播放、它写入的各集合条数 |
| `PUT /cloud/sync/devices/{id}` | 改名（之后不再随型号刷新） |
| `DELETE /cloud/sync/devices/{id}` | 移除（腾出名额，它写过的记录保留，之后它的推送回 40434） |
| `POST /cloud/sync/device/heartbeat` | 心跳，前台时约每分钟一次 |
| `PUT /cloud/sync/device/activity` | 正在播放 / 阅读：`{collection, recordId, state, payload}`，`state` 取 playing / paused / reading / idle，`clear: true` 清空 |
| `GET /cloud/sync/records` | 浏览：`collection` / `domain` / `deviceId` / `keyword` 筛，按修改时间倒序（查看别的设备同步了什么） |
| `GET /cloud/sync/records/{collection}/{id}` | 一条记录（含墓碑） |
| `DELETE /cloud/sync/collections/{collection}` | 清空一个集合（全部改成墓碑） |
| `POST /cloud/sync/handoffs` | 接力：`{toDeviceId, collection, recordId, payload}`，15 分钟内有效 |
| `GET /cloud/sync/handoffs` | 发给当前设备、还没领取的接力 |
| `POST /cloud/sync/handoffs/{id}/ack` | 领取（打开或忽略） |

「当前设备」一律取会话签发时的设备号（其次 `X-Device-Id`），请求体里的设备号不被采信。

管理端：`GET /api/admin/apps/{appkey}/users/{userId}/cloud-storage` 多了 `plan`、`devices`、`collections`；
`GET …/cloud-storage/sync/records` 浏览该用户的同步记录（含墓碑）；`DELETE …/cloud-storage/sync/devices/{deviceId}` 移除设备。
「清空云存储」同时清掉同步记录、设备与接力（序号不归零，地平线推到当前序号，旧游标一律 reset）。

## 各端约定的记录格式

记录 ID 的统一规则（四端同一个函数）：

```
syncId(key) = key                       当 key 匹配 ^[\x21-\x7e]{1,120}$
            = "h" + hex(sha1(utf8(key)))  否则（含中文、空白或过长）
```

每条 `payload` 都带展示字段，设备视图与网页据此展示，不必认识各领域的细节：

| 字段 | 说明 |
|---|---|
| `title` | 必填：歌名 / 片名 / 书名 |
| `subtitle` | 歌手、第几集、第几章…… |
| `cover` | 封面地址 |
| `progress` | 0–1 的进度（有时） |
| `positionMs` / `durationMs` | 播放位置与时长（有时） |

领域字段与展示字段平铺在同一个对象里。

| 集合 | 记录 key（再过 syncId） | 领域字段 |
|---|---|---|
| `music.history` | `MusicTrack.key`（`source:id`） | `track{source,id,title,artist,album,artwork,durationMs,albumId}`、`playedAt`（毫秒） |
| `music.favorites` | `MusicTrack.key` | `track{…}`、`addedAt` |
| `music.playlists` | `source:remoteId`（自建歌单 source = `mine`） | `source`、`remoteId`、`title`、`artwork`、`trackCount`、`coverPinned`、`updatedAt` |
| `music.playlist-tracks` | `歌单key#曲目key` | `playlist`（歌单 key）、`order`、`track{…}`（只同步自建歌单的曲目；导入歌单按 source+remoteId 重新拉取） |
| `video.history` / `anime.history` | `workKey`（规范化片名 + `|` + 年份） | `record{workKey,title,cover,year,type,ruleKey,vodId,lineName,episodeIndex,episodeName,episodeCount,positionMs,durationMs,updatedAt}` |
| `video.favorites` / `anime.follows` | `workKey` | 同上 `record` |
| `drama.history` | `Drama.workKey`（规范化剧名） | `drama{source,id,title,cover,…}`、`episodeIndex`、`episodeCount`、`positionMs`、`durationMs` |
| `drama.follows` | `Drama.workKey` | `drama{…}`、`seenEpisodes`、`latestEpisodes`、`followedAt` |
| `novel.bookshelf` | `书名 + "\n" + 作者` | `name`、`author`、`bookUrl`、`origin`（书源地址）、`originName`、`cover`、`kind`、`durChapterIndex`、`durChapterTitle`、`durChapterPos`、`durChapterTime`、`totalChapterNum`、`latestChapterTitle`、`group` |
| `novel.history` | 同上 | 同上（不在书架上的阅读记录） |
| `audiobook.shelf` / `audiobook.history` | `AudioBook.key`（`source:id`） | `book{…}`（原样的书 JSON）、`lastChapterId`、`lastChapterTitle`、`lastChapterIndex`、`lastPositionMs`、`lastDurationMs`、`chapterCount`、`speed`、`favoritedAt`、`lastPlayedAt` |
| `app.favorites` | 工具 ID | `addedAt` |

影视与番剧共用一份本地库，按源规则的 `anime` 标记分到 `video.*` 或 `anime.*`。
小说的跨设备身份是「书名 + 作者」（换源后 bookUrl 会变），与 Legado 的进度同步一致。

## 错误码

| 码 | 名称 | 何时 |
|---|---|---|
| 41363 | CLOUD_SYNC_RECORD_LIMIT | 同步记录条数达到档位上限 |
| 41364 | CLOUD_SYNC_DEVICE_LIMIT | 新设备超出档位设备数 |
| 41365 | CLOUD_SYNC_BATCH_TOO_LARGE | 一批超过 500 条 |
| 41360 | CLOUD_QUOTA_EXCEEDED | 条目与同步记录合计超出配额（与云存储同一个码） |
| 42264 | CLOUD_SYNC_INVALID_RECORD | 集合名 / 记录 ID / 内容 / 时间不合法 |
| 42265 | CLOUD_SYNC_COLLECTION_NOT_IN_PLAN | 当前档位不能同步这个集合 |
| 42266 | CLOUD_SYNC_NOT_IN_PLAN | 当前档位没有同步服务 |
| 42267 | CLOUD_SYNC_HANDOFF_NOT_IN_PLAN | 当前档位没有接力 |
| 42268 | CLOUD_SYNC_INVALID_DEVICE | 设备号缺失或格式不对 |
| 40434 | CLOUD_SYNC_DEVICE_NOT_FOUND | 设备没登记或已被移除（重新登记） |
| 40435 | CLOUD_SYNC_HANDOFF_NOT_FOUND | 接力不存在、已过期或不是发给这台设备的 |

## 表

| 表 | 一行是什么 |
|---|---|
| `app_cloud_storage_configs.plans` | 档位表（JSON） |
| `app_cloud_storage_users` | 多了 `sync_seq`、`sync_floor_seq`（墓碑地平线）、`sync_bytes`、`sync_records`、`last_sync_at` |
| `app_cloud_sync_records` | （用户, 集合, 记录）一行，主键以 `user_id` 打头 |
| `app_cloud_sync_devices` | 一台登记过的设备，含 `activity` |
| `app_cloud_sync_handoffs` | 一次接力 |

迁移 000181。推送与清空集合都先锁用户行，序号在锁内分配，所以提交顺序 = 序号顺序；
拉取先读已提交的 `sync_seq`，再扫 `seq <= sync_seq`，不会漏掉一条「序号更小但还没提交」的写入。
同步用量从记录表重算写回，不做加减（与条目账目同一条纪律）。

## 测法

```
AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55481/postgres?lc_messages=C \
  go test ./internal/service -run TestCloudServiceIntegration
go test ./internal/domain/cloudstorage
```
