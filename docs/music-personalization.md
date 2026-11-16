# 音乐个性化（Music Personalization）

客户端「音乐」功能的行为数据（点击、搜索、收藏、播放、跳过……）全部上报，训练每个应用自己的个性化模型，
给出「为你推荐 / 每日推荐 / 私人电台 / 相似歌曲」与用户自己能看的「听歌画像」。

## 服务

```
 App ──POST /music/events（批量，≤100 条）──▶ Aegis 主服务 ──JetStream 发布确认后才回 200──▶ NATS 流 MUSIC
  │                                              │                                          │ music.event.<app>
  │   GET /music/recommendations ─────────────▶  │ ── Connect RPC（熔断）──▶ music-rec ◄────┤ music.model.published
  │   GET/DELETE /music/profile ──────────────▶  │                           │  ▲  │         │
  │                                              │                           │  │  └─ Redis（实时状态、每日推荐缓存）
  控制台 /api/admin/apps/:appkey/music/* ──────▶ │ ── Connect RPC ─────────▶ │  └──── PostgreSQL aegis_music（曲库、配置、模型）
                                                                             └─────── ClickHouse aegis_music（事件、曝光）
                                   music-ingest ◄── 持久消费者 music-ingest ──┘
                                     ├─▶ ClickHouse events / impressions（ReplacingMergeTree，event_id 去重）
                                     ├─▶ PostgreSQL tracks（曲库维表 upsert）
                                     └─▶ Redis 实时状态（最近播放、正反馈、负反馈、不喜欢的歌手）
                                   music-trainer ◄── 工作队列 MUSIC_JOBS（手动 / 定时训练）
                                     └─▶ ClickHouse 读交互 → 隐式 ALS + ItemCF + 热度 → 离线评估 → 模型写 PG → 发布通知
```

| 服务 | 入口 | 职责 | 扩容 |
|---|---|---|---|
| `music-rec` | `services/music/cmd/music-rec` | Connect / gRPC 内部接口：推荐、画像、遗忘、管理端全部读写；内存里放每个应用已发布的模型，收到发布通知热加载 | 无状态，水平扩容 |
| `music-ingest` | `services/music/cmd/music-ingest` | 消费 `MUSIC` 流：批量写 ClickHouse、曲库、Redis 实时状态，**全部写成功才确认**；失败不确认，JetStream 重投 | 同一持久消费者多实例分摊 |
| `music-trainer` | `services/music/cmd/music-trainer` | 消费 `MUSIC_JOBS`（工作队列）：训练、评估、按门槛自动发布；定时任务按应用配置往队列里投 | 多实例互斥：一个应用同一时刻只训练一次（PG 行锁） |

三个服务与主服务同一个 Go 模块（同 `services/listentogether`、`services/risk`），镜像 `deploy/docker/music.Dockerfile`
（一个镜像三个二进制）。主服务只做鉴权、校验、发布事件与转发，音乐服务挂了不影响主服务；上报接口在音乐服务全挂时
仍然可用（只依赖 JetStream），事件在流里排队，恢复后补处理。

### 不丢、不重

- **上报**：主服务对每条事件 `PublishMsg` 到 JetStream 并等确认（`Nats-Msg-Id = <app>:<user>:<eventId>`，流的重复窗口 10 分钟）。
  任何一条没确认就回 503，客户端整批重传；重传的事件被窗口去重。
- **落地**：`music-ingest` 拉一批（≤500 条或 1 秒），ClickHouse 写成功（同步插入）、PG 与 Redis 写成功后再逐条确认。
  进程在任意一步崩溃都只会导致重投：ClickHouse 表是 `ReplacingMergeTree`，按 `(app_id, user_id, event_id)` 去重，查询一律带 `FINAL`；
  PG 是 upsert；Redis 实时状态是 `ZADD GT`（同一事件写两次结果不变）。
- **毒消息**：解析失败、字段非法的消息直接 `Term`（不重投），计数进指标。投递 10 次仍失败的进 `music.dead.<app>`。

## 事件协议

`POST /api/v1/apps/{appkey}/music/events`（需要登录，三档传输照常；请求体 ≤ 256 KB）

```json
{
  "events": [
    {
      "id": "01JD3X…",              // 客户端生成的唯一 ID（ULID / UUID，8–64 位 [A-Za-z0-9_-]），重传带同一个
      "type": "play_end",
      "ts": 1763280000000,          // 客户端发生时刻（毫秒）；早于 30 天或晚于服务器 10 分钟的整条丢弃
      "sessionId": "s-…",           // 一次「打开音乐」到「离开音乐」，可空
      "track": {                    // 与歌曲有关的事件必填
        "source": "wy", "songId": "1974443814",
        "title": "晴天", "artist": "周杰伦", "album": "叶惠美", "cover": "https://…", "durationMs": 269000
      },
      "playedMs": 251000,           // play_end：这次实际听了多久（拖动不算）
      "positionMs": 269000,         // seek / play_end：发生时的位置
      "reason": "completed",        // play_end：completed | skipped | switched | stopped | error
      "context": {                  // 从哪里来（都可空）
        "page": "search", "section": "result", "position": 3,   // position 从 0 开始
        "query": "周杰伦",           // search / search_click
        "recId": "01JD…", "scene": "home",   // 来自推荐时带上推荐结果的 recId 与场景
        "playlistId": "pl_123", "queueSource": "playlist"
      },
      "props": { "quality": "lossless" }    // 自由键值，≤ 16 个，键 ≤ 32 字、值 ≤ 128 字
    }
  ],
  "client": { "platform": "android", "appVersion": "3.8.0", "network": "wifi" }
}
```

响应：`{ "accepted": 37, "rejected": [ { "id": "…", "reason": "invalid_type" } ], "serverTime": 1763280000123 }`。
`rejected` 里的不要重传（整条非法）；整批 503（`50397`）才重传。

| type | 含义 | 必填 |
|---|---|---|
| `play_start` | 开始播放一首（真正出声） | track |
| `play_end` | 一首结束：听完、跳过、切走、停止、出错 | track、playedMs、reason |
| `pause` / `resume` | 暂停 / 继续 | track |
| `seek` | 拖动 | track、positionMs（拖到哪）、props.from（从哪） |
| `like` / `unlike` | 收藏 / 取消收藏 | track |
| `dislike` | 不感兴趣（这首）；`props.scope=artist` 表示不喜欢这位歌手 | track |
| `playlist_add` | 加入歌单 | track、context.playlistId |
| `download` / `share` | 下载 / 分享 | track |
| `search` | 提交搜索 | context.query |
| `search_click` | 点了搜索结果 | track、context.query、context.position |
| `click` | 在任意列表点了歌（播放或打开） | track、context.page / section / position |
| `impression` | 推荐结果被展示（每个 recId 一次，props.count=展示了几首） | context.recId、context.scene |
| `lyrics_open` | 打开歌词 / 播放页 | track |

服务端补齐：`app_id`、`user_id`（应用内主键）、`device_id`（会话里的设备）、`received_at`、`ip`、
`work_key`（作品键，见下）、`track_key`（`source:songId`）。

**作品键**：同一首歌在各音源（wy / tx / kg / kw / mg …）里 ID 不同。`work_key = sha1(规范化歌名 | 第一歌手)` 前 16 位，
规范化去掉括号里的「Live / 伴奏 / 翻唱 / 版本」等后缀、全角转半角、繁转简（常用字表）、小写、去空白与标点。
模型以作品为单位，展示时用该作品最近被播放的那个音源版本。

**限额**：每人每分钟 600 条、每天 20000 条（Redis 滑窗），超出 429（`42970`）；客户端退避后重传。

## 隐式反馈打分

一次事件对 (用户, 作品) 的贡献（`services/music/internal/signal`，训练与实时状态共用一份）：

| 事件 | 分 |
|---|---|
| play_end，听完（playedMs ≥ 80% 时长，或 reason=completed） | +1.0 |
| play_end，部分（30%–80%） | +0.4 |
| play_end，早跳（< 30 秒且 < 30%，reason=skipped/switched） | −0.6 |
| like / unlike | +3.0 / −3.0 |
| dislike | −6.0（并进负反馈集合，推荐里排除） |
| playlist_add / download / share | +2.0 / +2.0 / +1.5 |
| search_click / click | +0.3 / +0.2 |
| seek（向后拖过 30% 以上） | −0.1 |

时间衰减 `score × 0.5^(age / halfLife)`，`halfLife` 默认 45 天（应用配置）。偏好 `p = score > 0`，
置信度 `c = 1 + α·ln(1 + |score| / ε)`（α=20、ε=0.5）。权重、半衰期、窗口（默认 180 天）都在控制台改。

## 模型（music-trainer）

1. **交互矩阵**：ClickHouse 一条 SQL 按 (user, work_key) 聚合打分，窗口内作品出现人数 < `minItemUsers`（默认 2）的不进矩阵。
2. **隐式 ALS**（Hu, Koren, Volinsky 2008）：共轭梯度求解（每次 3 步），默认 48 维、正则 0.05、迭代 12 次，多协程。
3. **ItemCF**：正反馈集合上的余弦相似度，加 IUF 惩罚（听得多的人权重小）+ 会话内相邻播放的「下一首」转移加成；每个作品留前 50 个邻居。
4. **热度**：近 7 天播放的贝叶斯平滑完成率 × 对数播放人数，另存近 1 天的「飙升」。
5. **画像**：每个用户最常听的 20 位歌手（加权），推荐时用于歌手偏好通道。
6. **离线评估**：每人按时间留出最后 20% 的正反馈作测试集，在其余数据上训练，算 Recall@20、NDCG@20、命中率、覆盖率，
   与「热度」基线对比；之后用全部数据重训一次作为待发布模型。
7. **发布**：`autoPublish` 打开且 `NDCG@20 ≥ 基线 × minLift`（默认 1.0）自动发布，否则留在「待发布」，控制台可手动发布任一成功的版本（即回滚）。
   模型（zstd 压缩的二进制快照）存 `model_blobs`，发布写 `model_runs.published_at` 并发 NATS `music.model.published`。

定时：应用配置 `schedule`（cron，默认 `0 4 * * *` 每天 4 点），trainer 里只有抢到 PG 咨询锁的实例负责往队列投。

## 推荐（music-rec）

场景：

| scene | 用途 | 候选通道 |
|---|---|---|
| `home` | 首页「为你推荐」 | ALS、ItemCF（最近正反馈作种子）、歌手偏好、热度、探索 |
| `daily` | 每日推荐（30 首，当天固定，次日刷新） | 同 home，按「日期 + 用户」定种子，结果缓存到当天结束 |
| `radio` | 私人电台（连续取下一批，`cursor` 翻页） | 同 home + 当天已出过的不再出 |
| `similar` | 相似歌曲（`source` + `songId` 指定种子） | ItemCF、ALS 余弦、同歌手 |

打分：各通道分数各自归一到 [0,1] 后按配置加权；排除负反馈作品与不喜欢的歌手；近 `recentExcludeDays`（默认 2 天）
播放过的降权；MMR 多样性重排，前 20 首里同一歌手最多 `artistCap`（默认 2）首。每首带一句原因：

| reason.kind | 文案示例 |
|---|---|
| `because_track` | 因为你喜欢《晴天》 |
| `artist` | 你常听 周杰伦 |
| `similar_users` | 和你口味相近的人在听 |
| `trending` | 近期热门 |
| `explore` | 换换口味 |
| `seed` | 与《晴天》相似 |

冷启动（模型里没有这个人）：用 Redis 实时正反馈做 ItemCF 种子；连这个也没有就给热度 + 探索。
每次返回一个 `recId`（ULID），曝光事件与后续点击、播放带上它，控制台据此算各场景点击率与播放率。
推荐结果也会发 `music.rec.served`（落 ClickHouse `impressions`）。

`personalized=false`（用户在客户端关了个性化推荐）时只给热度，不读任何个人数据。

## 客户端接口

**推荐** `GET /api/v1/apps/{appkey}/music/recommendations?scene=home&limit=20&cursor=&source=&songId=&personalized=true`

```json
{
  "recId": "01JD…", "scene": "home", "modelVersion": 12, "personalized": true, "nextCursor": "2",
  "generatedAt": 1763280000000, "expiresAt": 1763283600000,
  "items": [
    { "track": { "source": "wy", "songId": "186016", "title": "晴天", "artist": "周杰伦", "album": "叶惠美", "cover": "https://…", "durationMs": 269000 },
      "score": 0.91, "reason": { "kind": "because_track", "text": "因为你喜欢《七里香》", "refTitle": "七里香" } }
  ]
}
```

`limit` 1–50（daily 固定 30）。未登录 401；音乐服务未开通 `50396`（客户端隐藏推荐分区）；暂不可用 `50397`。

**画像** `GET /api/v1/apps/{appkey}/music/profile?days=30`（days 7 / 30 / 90 / 365）

```json
{
  "days": 30, "listeningMinutes": 1260, "plays": 830, "tracks": 312, "artists": 96,
  "completionRate": 0.71, "skipRate": 0.18, "likes": 42, "activeDays": 24, "longestStreak": 9, "newTrackShare": 0.35,
  "topArtists": [ { "name": "周杰伦", "plays": 120, "minutes": 480, "share": 0.14 } ],
  "topTracks":  [ { "track": { … }, "plays": 31, "minutes": 130 } ],
  "hours": [0, 0, …24 个],        // 每小时听歌分钟
  "weekdays": [ …7 个，周一起 ],
  "sources": [ { "source": "wy", "plays": 500 } ],
  "persona": { "key": "night_owl", "title": "夜猫子", "text": "你 42% 的音乐时间在 22 点之后" },
  "traits": [ { "key": "loyal", "title": "专一", "value": 0.62 }, { "key": "explorer", "title": "探索", "value": 0.35 } ],
  "updatedAt": 1763280000000
}
```

persona 取值：`night_owl` 夜猫子、`early_bird` 早起派、`commuter` 通勤党（7–9、17–19 点占比高）、`loyal` 专一型（第一歌手占比 ≥ 25%）、
`explorer` 探索型（新歌占比 ≥ 50%）、`completionist` 完播型（完播率 ≥ 85%）、`casual` 随性型（兜底）。

**清除听歌数据** `DELETE /api/v1/apps/{appkey}/music/profile` → `{ "deleted": true }`。
删 ClickHouse 里这个人的事件与曝光（轻量删除）、Redis 实时状态、模型里这个人的向量与画像（下次训练前就不再用）。
账号删除（`UserService.AddPurgeHook`）时自动做同样的事。

## 管理端（控制台 /api/admin/apps/:appkey/music/*）

主服务只做登录与权限：`/api/admin/apps/:appkey/music/*path` 原样反向代理到 music-rec 的 `/v1/apps/<应用 ID>/*path`，
带内部令牌（`X-Music-Token`）与操作者（`X-Music-Operator`，URL 编码）；响应信封 `{code, message, data}` 与主服务一致。
读（GET）要 `app:music:read`，写（POST / PUT / DELETE）要 `app:music:manage`（权限树「应用 → 音乐推荐」）。
用户 ID 一律应用内主键（控制台跳 `/users/<id>` 用），昵称由控制台按 ID 另取。返回结构见文末「管理端返回结构」；数组字段一律返回 `[]`，不返回 null。
`POST /models/train` 与 `POST /models/:runId/publish` 返回那条训练记录。

| 方法与路径 | 说明 |
|---|---|
| `GET /overview?days=7` | 总览：每日事件分类型、听歌人数、听歌分钟、完播率、跳过率、各场景曝光 / 点击 / 播放、热门作品与歌手、音源分布、当前模型 |
| `GET /users?days=30&q=&page=&pageSize=` | 听歌用户排行（分钟、播放、最近活跃），q 为用户 ID |
| `GET /users/:userId?days=30` | 用户画像（同客户端画像）+ 实时状态（最近播放、正负反馈、不喜欢的歌手）+ 模型里的口味（是否在模型、Top 歌手） |
| `GET /users/:userId/events?type=&before=&limit=` | 用户事件时间线（按时间倒序，before 为毫秒游标） |
| `GET /users/:userId/recommendations?scene=home&limit=20&source=&songId=` | 推荐预览（带每个通道的分数拆解，不发曝光；similar 要带种子） |
| `POST /users/:userId/forget` | 清除这个人的听歌数据 |
| `GET /tracks?days=7&q=&sort=plays\|listeners\|completion\|skip&page=&pageSize=` | 作品排行 |
| `GET /tracks/:workKey` | 作品详情：各音源版本、统计、相似作品（ItemCF 邻居与 ALS 近邻） |
| `GET /events?type=&userId=&q=&from=&to=&page=&pageSize=` | 事件检索（from / to 为毫秒，默认近 7 天） |
| `GET /models?page=&pageSize=` | 训练记录（id 倒序；状态、指标、规模、耗时、是否当前发布） |
| `GET /models/:runId` | 训练详情（参数、指标对比基线、各阶段耗时、日志） |
| `POST /models/train` `{note?}` | 立即训练（入队） |
| `POST /models/:runId/publish` | 发布（回滚也用它） |
| `GET /config` / `PUT /config` | 应用配置（开关、权重、模型参数、定时、多样性） |

### 应用配置

```json
{
  "enabled": true, "collectEnabled": true,
  "windowDays": 180, "halfLifeDays": 45, "minItemUsers": 2,
  "weights": { "complete": 1.0, "partial": 0.4, "skip": -0.6, "like": 3.0, "dislike": -6.0,
               "playlistAdd": 2.0, "download": 2.0, "share": 1.5, "searchClick": 0.3, "click": 0.2, "seekBack": -0.1 },
  "als": { "factors": 48, "iterations": 12, "regularization": 0.05, "alpha": 20 },
  "itemcf": { "neighbors": 50, "sessionBoost": 0.3 },
  "blend": { "als": 1.0, "itemcf": 0.8, "artist": 0.5, "trending": 0.3, "explore": 0.1 },
  "artistCap": 2, "recentExcludeDays": 2, "exploreRatio": 0.1,
  "schedule": "0 4 * * *", "autoPublish": true, "minLift": 1.0
}
```

`enabled=false`：推荐接口回 `50396`（客户端隐藏入口）；`collectEnabled=false`：上报接口照常回 200 但不再发布事件（`accepted` 为 0，`rejected` 为空）。

## 主服务配置

| 变量 | 说明 |
|---|---|
| `MUSIC_RPC_URL` | music-rec 内部地址，如 `http://music-rec:8096`；为空时推荐、画像与管理端回 `50396` |
| `MUSIC_INTERNAL_TOKEN` | 内部接口令牌，与 `MUSIC_INTERNAL_TOKEN`（音乐服务侧）一致 |
| `MUSIC_EVENTS_ENABLED` | 默认 true；关掉后上报接口直接 200 且不发布 |

音乐服务环境变量（前缀 `MUSIC_`，`services/music/internal/config`）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `MUSIC_PG_DSN` | — | PostgreSQL（库 `aegis_music`，首次启动自动建表） |
| `MUSIC_CLICKHOUSE_ADDR` / `_DB` / `_USER` / `_PASSWORD` | — / `aegis_music` | ClickHouse |
| `MUSIC_REDIS_URL` | — | Redis（实时状态、缓存、限额由主服务的 Redis 负责） |
| `MUSIC_NATS_URL` | — | NATS（JetStream） |
| `MUSIC_INTERNAL_TOKEN` | — | 为空时内部接口拒绝一切调用 |
| `MUSIC_RPC_ADDR` | `:8096` | music-rec 内部 Connect / gRPC + `/metrics` + 健康 |
| `MUSIC_METRICS_ADDR` | `:8097` | ingest / trainer 的指标与健康 |
| `MUSIC_TRAIN_WORKERS` | CPU 数 | 训练并行度 |

## 测试

```bash
go test ./services/music/...        # 纯函数：作品键与校验、打分、ALS / ItemCF（聚类数据上胜过热度）、离线评估、重排、画像
```

端到端（本机 docker 的 aegis-postgres 15432、aegis-redis、aegis-nats、aegis-risk-clickhouse 19000；库用 `aegis_music_it`，
三个服务各自监听不同端口，ingest 与 trainer 的 `MUSIC_METRICS_ADDR` 要错开）：

```bash
# 起三个服务（环境变量见上文，MUSIC_RPC_ADDR=127.0.0.1:18096）后：
MUSIC_IT=1 MUSIC_IT_RPC=http://127.0.0.1:18096 go test ./services/music/internal/e2e -run IT -v   # 发事件 → 落地 → 训练发布 → 推荐 / 画像 / 管理端 / 清除
MUSIC_IT=1 MUSIC_IT_RPC=http://127.0.0.1:18096 go test ./internal/service -run TestMusicServiceIT  # 主服务：校验、去重、限额、转发
```

每次运行用一个新的应用 ID，互不干扰。

## 管理端返回结构（TypeScript，控制台 `src/lib/api/music.ts` 照抄）

```ts
type MusicTrack = { source: string; songId: string; title: string; artist?: string; album?: string; cover?: string; durationMs?: number };

type MusicOverview = {
  days: number; from: number; to: number;                       // 毫秒
  totals: { events: number; listeners: number; plays: number; listeningMinutes: number;
            completionRate: number; skipRate: number; likes: number; searches: number; tracks: number };
  previous: { events: number; listeners: number; plays: number; listeningMinutes: number; completionRate: number; skipRate: number };   // 上一个同长周期，算环比
  daily: Array<{ day: string /* YYYY-MM-DD，服务器时区 */; events: number; listeners: number; plays: number;
                 listeningMinutes: number; completionRate: number; skipRate: number }>;
  eventTypes: Array<{ type: string; count: number }>;
  hours: number[];                                              // 24 个，每小时播放次数
  scenes: Array<{ scene: string; requests: number; impressions: number; clicks: number; plays: number; ctr: number; playRate: number }>;
  topTracks: Array<{ workKey: string; track: MusicTrack; plays: number; listeners: number; completionRate: number }>;
  topArtists: Array<{ name: string; plays: number; listeners: number }>;
  sources: Array<{ source: string; plays: number }>;
  platforms: Array<{ platform: string; listeners: number }>;
  model: MusicModelRun | null;                                  // 当前发布的模型
  pipeline: { lagSeconds: number; pending: number; lastEventAt: number };  // 消费积压
};

type MusicUserRow = { userId: number; minutes: number; plays: number; tracks: number; likes: number; skipRate: number; lastActiveAt: number };
type MusicUserPage = { items: MusicUserRow[]; total: number };

type MusicProfile = {                                           // 与客户端画像同形
  days: number; listeningMinutes: number; plays: number; tracks: number; artists: number;
  completionRate: number; skipRate: number; likes: number; activeDays: number; longestStreak: number; newTrackShare: number;
  topArtists: Array<{ name: string; plays: number; minutes: number; share: number }>;
  topTracks: Array<{ track: MusicTrack; plays: number; minutes: number }>;
  hours: number[]; weekdays: number[];
  sources: Array<{ source: string; plays: number }>;
  persona: { key: string; title: string; text: string };
  traits: Array<{ key: string; title: string; value: number }>;
  updatedAt: number;
};

type MusicUserDetail = {
  userId: number;
  profile: MusicProfile;
  realtime: {
    recent: Array<{ workKey: string; track: MusicTrack | null; at: number }>;      // 最近播放（≤ 50）
    positive: Array<{ workKey: string; track: MusicTrack | null; weight: number }>; // 实时正反馈
    negative: Array<{ workKey: string; track: MusicTrack | null; at: number }>;    // 不感兴趣 / 早跳
    dislikedArtists: Array<{ name: string; at: number }>;
  };
  model: { inModel: boolean; version: number; interactions: number; topArtists: Array<{ name: string; weight: number }> };
};

type MusicEvent = {
  id: string; type: string; ts: number; receivedAt: number; userId: number; deviceId: string; sessionId: string;
  workKey: string; track: MusicTrack | null; playedMs: number; positionMs: number; reason: string;
  context: { page?: string; section?: string; position?: number; query?: string; recId?: string; scene?: string; playlistId?: string; queueSource?: string };
  platform: string; appVersion: string; network: string; props: Record<string, string>;
};
type MusicEventPage = { items: MusicEvent[]; total: number };          // /events
type MusicTimeline = { items: MusicEvent[]; nextBefore: number | null }; // /users/:id/events

type MusicRecItem = {
  workKey: string; track: MusicTrack; score: number;
  reason: { kind: string; text: string; refTitle?: string };
  channels: Record<string, number>;                             // 预览才有：als / itemcf / artist / trending / explore / penalty
};
type MusicRecPreview = { recId: string; scene: string; modelVersion: number; personalized: boolean; coldStart: boolean;
                         items: MusicRecItem[]; tookMs: number };

type MusicTrackRow = { workKey: string; track: MusicTrack; plays: number; listeners: number; minutes: number;
                       completionRate: number; skipRate: number; likes: number };
type MusicTrackPage = { items: MusicTrackRow[]; total: number };
type MusicTrackDetail = {
  workKey: string; track: MusicTrack;
  variants: Array<{ source: string; songId: string; plays: number; lastSeenAt: number }>;
  stats: { plays: number; listeners: number; minutes: number; completionRate: number; skipRate: number; likes: number; dislikes: number };
  daily: Array<{ day: string; plays: number; listeners: number }>;
  similar: Array<{ workKey: string; track: MusicTrack | null; score: number; via: "itemcf" | "als" }>;
};

type MusicModelRun = {
  id: number; status: "queued" | "running" | "succeeded" | "failed"; trigger: "manual" | "schedule";
  note: string; operator: string;
  createdAt: number; startedAt: number | null; finishedAt: number | null; publishedAt: number | null; current: boolean;
  users: number; items: number; interactions: number; sizeBytes: number;
  metrics: { recall20: number; ndcg20: number; hitRate20: number; coverage: number;
             baseline: { recall20: number; ndcg20: number; hitRate20: number; coverage: number }; testUsers: number } | null;
  error: string;
};
type MusicModelPage = { items: MusicModelRun[]; total: number };
type MusicModelDetail = MusicModelRun & {
  params: MusicConfig;                                          // 训练时用的配置快照
  stages: Array<{ name: string; ms: number }>;                  // load / als / itemcf / trending / eval / save
  log: string[];
};

type MusicConfig = {                                            // 见上文「应用配置」
  enabled: boolean; collectEnabled: boolean; windowDays: number; halfLifeDays: number; minItemUsers: number;
  weights: { complete: number; partial: number; skip: number; like: number; dislike: number; playlistAdd: number;
             download: number; share: number; searchClick: number; click: number; seekBack: number };
  als: { factors: number; iterations: number; regularization: number; alpha: number };
  itemcf: { neighbors: number; sessionBoost: number };
  blend: { als: number; itemcf: number; artist: number; trending: number; explore: number };
  artistCap: number; recentExcludeDays: number; exploreRatio: number;
  schedule: string; autoPublish: boolean; minLift: number;
  updatedAt: number; updatedBy: string;
};
```

取值范围（`PUT /config` 校验，越界回 40000 并在 message 里说哪一项）：windowDays 7–730、halfLifeDays 1–365、minItemUsers 1–50、
weights 每项 −20–20、als.factors 8–256、iterations 1–50、regularization 0.0001–10、alpha 1–200、itemcf.neighbors 5–200、
sessionBoost 0–2、blend 每项 0–5、artistCap 1–10、recentExcludeDays 0–30、exploreRatio 0–0.5、schedule 五段 cron 或空（不定时）、minLift 0–3。
