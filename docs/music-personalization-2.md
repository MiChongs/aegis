# 音乐个性化（第二期：完整版契约）

在 `docs/music-personalization.md`（第一期：上报、ALS + ItemCF、四个场景、画像、控制台）之上补齐。本文是**契约**：
后端、控制台、安卓、电脑版都照这里写，字段名、取值、错误码以本文为准。第一期的接口与结构不变（只加字段）。

## 一、总览

| 方向 | 新增 |
|---|---|
| 模型 | 实时折入（ALS fold-in，训练间隙按最新行为更新口味）、精排（基于曝光与点击的逻辑回归排序，特征在出推荐时记录）、歌手相似度、作品语种、作品完播 / 跳过率 |
| 新用户 | 首次进入选喜欢的歌手与语种（onboarding），冷启动直接用 |
| 场景 | `autoplay` 智能续播、`discover` 每周雷达、`mix` 风格歌单、推荐歌手、听歌报告（周 / 月 / 年）、热搜、搜索结果按口味重排 |
| 用户控制 | 不感兴趣带原因、我的不喜欢列表与撤销、偏好设置（多端同步）、导出我的听歌记录 |
| 运营 | 干预规则（屏蔽、加权、置顶）、A/B 实验（分桶、覆盖参数、效果与显著性）、曝光疲劳 |
| 平台 | MCP 工具 `music_*`、电脑版（Flutter）全量接入、安卓补齐 |

iOS Swift 版没有音乐功能，本期不涉及。

## 二、事件协议增量

新增 / 扩展的事件（`POST /music/events`，其余规则不变）：

| type | 含义 | 必填 |
|---|---|---|
| `dislike` | 新增 `props.reason`：`not_interested`（默认）\| `heard_too_much`（听腻了）\| `dislike_artist`（不喜欢这位歌手，等同 `props.scope=artist`）\| `poor_quality`（音质差）\| `wrong_song`（歌曲不对 / 版本错） | track |
| `undislike` | 撤销不感兴趣（客户端一般不用发，走「撤销」接口由服务端生成）；`props.scope=artist` 时撤销歌手 | track（或 `props.artist`） |
| `autoplay` | 智能续播接上了一批（每批一次，`context.recId`） | context.recId |
| `report_view` | 打开听歌报告（`props.period` = week / month / year） | — |
| `onboarding` | 完成或跳过新用户偏好选择（`props.action` = done / skip，`props.artists` 逗号分隔） | — |

打分（`signal`）：

| 事件 | 分 | 其他效果 |
|---|---|---|
| dislike / not_interested、dislike_artist | −6（配置 `weights.dislike`） | 作品进负反馈；dislike_artist 时歌手进「不喜欢的歌手」 |
| dislike / heard_too_much | −1 | 作品进「暂不推荐」`suppressDays`（默认 30）天，不当成不喜欢 |
| dislike / poor_quality、wrong_song | 0 | 只屏蔽这个**音源版本**（track_key），作品本身不受影响 |
| undislike | 与原 dislike 相反（+6 / +1 / 0） | 从对应集合移除 |

## 三、模型增量（music-trainer）

1. **作品语种** `lang`：按歌名 + 歌手的文字判定（`contract.DetectLang`）：含假名 → `ja`，含谚文 → `ko`，含汉字 → `zh`，
   拉丁字母占多数 → `en`，其余 `other`。曲库表 `music_tracks.lang` 在落地时写。
2. **作品统计**：近 30 天完播率、早跳率（贝叶斯平滑），存快照。
3. **歌手相似度**：每人加权 Top 30 歌手，按共同听众算余弦（IUF 惩罚），每位歌手留前 20 个相似歌手。
4. **折入用的常量**：快照存 `YᵀY`（f × f）、正则 λ、α。
5. **精排**（ranker）：训练样本 = ClickHouse `rank_samples`（出推荐时逐首记录的特征）关联同一 `recId` 的后续行为：
   被展示（该 recId 有 `impression`）且对这首有 `click` / `play_start` / `like` / `playlist_add` / 播放 ≥ 30 秒 → 1；
   被展示而无正向行为、或早跳 → 0。逻辑回归（L2，小批量梯度下降，特征标准化），按时间留出最后 20% 算 AUC，
   与「融合分」基线 AUC 对比；样本 < `ranker.minSamples`（默认 2000）或 AUC 不高于基线时不启用，快照里 `ranker=null`。
   特征（顺序固定，`model.RankFeatures`）：

   | # | 名 | 含义 |
   |---|---|---|
   | 0–4 | als、itemcf、artist、trending、explore | 各通道归一化分 |
   | 5 | foldin | 折入向量与作品的点积（归一化） |
   | 6 | pop | 作品热度 |
   | 7 | complete | 作品完播率 |
   | 8 | skip | 作品早跳率 |
   | 9 | artist_aff | 用户对该歌手的偏好（0–1） |
   | 10 | recent | 近期播放过（0/1） |
   | 11 | fatigue | 近 7 天被展示未点击次数 log1p |
   | 12 | lang_match | 用户在该语种上的占比（含偏好设置） |
   | 13 | fresh | 用户从没听过（0/1） |
   | 14 | pref_artist | 用户在偏好里选过这位歌手（0/1） |
   | 15 | position | 展示位置 log1p（只在训练时用，线上置 0，去位置偏差） |

6. 快照格式版本升到 2；旧快照加载失败时照常热度兜底，控制台提示重新训练。

## 四、推荐增量（music-rec）

### 打分管线

`召回 → 特征 → 打分（融合分；有 ranker 且开启时 × (0.5 + sigmoid(w·x))）→ 业务规则 → 多样性 → 置顶插入`

精排只在融合分上做 0.5–1.5 倍的调整，不替换融合分：从没被展示过的候选在训练分布之外，单用精排概率会给出不可靠的分。

- **实时折入**（`foldIn`，默认开）：用户最近正反馈与最近播放的作品向量解一次最小二乘得到当下口味向量；
  不在模型里的人直接用它（冷启动也能个性化）；在模型里的人与训练向量按「训练后新增正反馈数」混合（最多 50%）。
- **偏好设置**：选过的歌手作冷启动种子（歌手通道），选过的语种在 `lang_match` 上加成；`personalized=false`（服务端设置）
  与请求参数取「与」。
- **曝光疲劳**：同一首近 7 天对同一人展示 ≥ `fatigue.threshold`（默认 3）次且没点，分数 × `fatigue.decay`^(次数−阈值+1)（默认 0.6）；
  点击或播放后清零。
- **暂不推荐 / 版本屏蔽**：heard_too_much 的作品到期前不出；poor_quality / wrong_song 的音源版本对这个人不出。
- **运营规则**（全应用生效，见管理端）：`block` 直接过滤；`boost` 分数乘 `weight`；`pin` 在结果第 `position` 位插入（每人每天每条规则最多一次）。
- **实验**：用户按 `fnv(实验 ID:用户 ID) % 100` 分桶，命中的变体覆盖本次请求的参数（见管理端「实验」）；结果与曝光都带 `experimentId`、`variant`。

### 新场景

| scene | 用途 | 规则 |
|---|---|---|
| `autoplay` | 智能续播：队列快放完时接着放 | 种子 = 请求里的 `seeds`（最近放的在后，≤ 20），ItemCF + 口味 + 同歌手延续；排除 `exclude`（≤ 300）与最近 2 小时放过的；默认 10 首 |
| `discover` | 每周雷达：没听过的新歌 | 只出这个人从没交互过的作品，分数 × (1 − 热度)^0.5 偏长尾；30 首，按 ISO 周固定，周一 0 点刷新 |
| `mix` | 风格歌单里的歌 | `mixId` 指定；见下 |
| `artist` | 某位歌手的个性化热门 | `artist` 指定歌手名；该歌手的作品按口味排序，30 首 |

**风格歌单**（mixes）：取这个人近 90 天加权 Top 120 作品的模型向量做 k-means（k = 2–6，按数据量），每簇一个歌单：
40% 是簇里自己常听的、60% 是簇中心附近没听过的，25 首；标题「{歌手1}、{歌手2} 等」，副标题「包含 {前三位歌手}」，
`lang` 为簇里占多数的语种；ID 稳定一天。数据不足（< 12 首有向量的作品）时返回空列表。

## 五、客户端接口增量

全部需要登录，路径前缀 `/api/v1/apps/{appkey}`，信封与错误码同第一期（`50396` 未开通、`50397` 暂不可用、`40000` 参数错）。

### 推荐（扩展）

`GET /music/recommendations` 新增参数：`mixId`（scene=mix）、`artist`（scene=artist）。
`POST /music/recommendations`（scene=autoplay 用，也可以发其他场景）：

```json
{ "scene": "autoplay", "limit": 10, "personalized": true,
  "seeds":   [ { "source": "wy", "songId": "186016", "title": "晴天", "artist": "周杰伦" } ],
  "exclude": [ "wy:186016", "tx:002" ],
  "cursor": "", "mixId": "", "artist": "", "source": "", "songId": "" }
```

返回与 GET 相同，另加两个字段：`"experimentId": 3, "variant": "B"`（不在实验里时为 0 与 ""）。
`seeds` 里的歌带上标题与歌手：即使服务端曲库还没有这首，也能按作品键找到。

### 风格歌单 `GET /music/mixes`

```json
{ "items": [
    { "id": "mx_3f9a…", "title": "周杰伦、林俊杰 等", "subtitle": "包含 周杰伦、林俊杰、陈奕迅",
      "covers": ["https://…", "https://…", "https://…", "https://…"], "artists": ["周杰伦", "林俊杰", "陈奕迅"],
      "size": 25, "lang": "zh" } ],
  "generatedAt": 1763280000000, "expiresAt": 1763308800000 }
```

### 推荐歌手 `GET /music/artists/recommended?limit=12`

```json
{ "items": [ { "name": "林俊杰", "cover": "https://…", "reason": "与 周杰伦 风格相近",
               "tracks": [ { "source": "wy", "songId": "…", "title": "…", "artist": "林俊杰", "cover": "…", "durationMs": 0 } ] } ] }
```

`tracks` 是这位歌手的 3 首代表作（直接可播）。

### 新用户偏好 `GET /music/onboarding`

```json
{ "needed": true,
  "artists": [ { "name": "周杰伦", "cover": "https://…", "lang": "zh" } ],
  "languages": [ { "key": "zh", "title": "华语" }, { "key": "en", "title": "欧美" }, { "key": "ja", "title": "日语" },
                 { "key": "ko", "title": "韩语" }, { "key": "other", "title": "其他" } ] }
```

`needed`：没选过也没跳过、且听歌记录少于 20 次。`artists` 最多 40 位，按语种穿插。

### 偏好设置 `GET /music/preferences`、`PUT /music/preferences`

```json
{ "personalized": true, "languages": ["zh", "en"], "artists": ["周杰伦", "林俊杰"],
  "autoplay": true, "onboarded": true, "updatedAt": 1763280000000 }
```

PUT 整体替换（字段可缺省，缺省的保持原值）；`artists` ≤ 50、每个 ≤ 64 字，`languages` 只能是上面五个键。
`personalized=false` 后服务端所有推荐都不读个人数据（多端一致）；客户端本地开关继续保留，两者取「与」。

### 我的不喜欢 `GET /music/feedback?kind=track|artist|suppressed&page=1&pageSize=50`

```json
{ "items": [ { "kind": "track", "workKey": "…", "track": { … }, "artist": "", "reason": "not_interested", "at": 1763280000000, "until": 0 } ],
  "total": 12 }
```

`suppressed` 是「听腻了」的作品（`until` 为到期时刻）。撤销：`DELETE /music/feedback`，请求体
`{ "kind": "track", "workKey": "…" }` 或 `{ "kind": "artist", "artist": "周杰伦" }` 或 `{ "kind": "suppressed", "workKey": "…" }` →
`{ "removed": true }`（服务端同时写一条 `undislike` 事件，训练时抵消）。

### 听歌报告 `GET /music/report?period=week|month|year&offset=0`

`offset=0` 本周 / 本月 / 今年，1 是上一期，最多回看 52 周 / 24 月 / 5 年。

```json
{ "period": "week", "from": 1763251200000, "to": 1763856000000, "label": "11 月 10 日至 11 月 16 日",
  "listeningMinutes": 620, "plays": 410, "tracks": 230, "artists": 71, "newTracks": 96, "newArtists": 14, "activeDays": 6,
  "previous": { "listeningMinutes": 540, "plays": 380 },
  "topTracks":  [ { "track": { … }, "plays": 21, "minutes": 88 } ],
  "topArtists": [ { "name": "周杰伦", "plays": 60, "minutes": 240, "cover": "https://…" } ],
  "discoveries": [ { "name": "告五人", "firstAt": 1763300000000, "plays": 12, "cover": "https://…" } ],
  "firstTrack": { "track": { … }, "at": 1763251800000 },
  "latestNight": { "track": { … }, "at": 1763330000000 },
  "mostRepeated": { "track": { … }, "day": "2026-11-12", "plays": 9 },
  "peakDay": { "day": "2026-11-14", "minutes": 160 },
  "hours": [ …24 个，分钟 ], "weekdays": [ …7 个，分钟 ], "months": [ …12 个，仅 year ],
  "persona": { "key": "night_owl", "title": "夜猫子", "text": "…" },
  "highlights": [ "这周你听了 620 分钟，比上周多 15%", "新认识了 14 位歌手" ],
  "empty": false }
```

没有数据时 `empty=true`，其余字段为零值与空数组。`latestNight` 取 0–5 点之间最晚的一次，没有则为 null；
`firstTrack`、`mostRepeated`、`peakDay` 没有时为 null。

### 热搜 `GET /music/search/trending?limit=10`

```json
{ "items": [ { "query": "晴天", "users": 31, "rising": true } ], "updatedAt": 1763280000000 }
```

近 24 小时按搜索人数排序，至少 3 个不同用户搜过才出现（不暴露个人搜索）；`rising`：比前 24 小时人数翻倍。

### 搜索结果按口味重排 `POST /music/search/rerank`

```json
{ "query": "晴天", "items": [ { "source": "wy", "songId": "…", "title": "…", "artist": "…" } ] }
→ { "order": [2, 0, 1, …], "personalized": true }
```

`items` ≤ 100；`order` 是原下标的新顺序。只做轻度调整：一首最多前移 3 位；不感兴趣的歌与不喜欢的歌手沉底但不删除。
关了个性化时原样返回。

### 导出我的听歌记录 `GET /music/export?before=&limit=500`

```json
{ "items": [ { "id": "…", "type": "play_end", "ts": 1763280000000, "track": { … }, "playedMs": 200000, "reason": "completed",
               "context": { … } } ],
  "nextBefore": 1763270000000 }
```

按时间倒序分页，`limit` ≤ 1000；不含 IP 与设备 ID 以外的他人数据（自己的设备 ID 也不返回）。

## 六、管理端增量（`/api/admin/apps/:appkey/music/*`）

| 方法与路径 | 说明 |
|---|---|
| `GET /rules` | 干预规则列表 |
| `POST /rules` / `PUT /rules/:id` / `DELETE /rules/:id` | 新建 / 修改 / 删除 |
| `GET /experiments` | 实验列表 |
| `POST /experiments` / `PUT /experiments/:id` / `DELETE /experiments/:id` | 新建 / 修改 / 删除（运行中不能删，先停止） |
| `POST /experiments/:id/start` / `POST /experiments/:id/stop` | 开始 / 停止 |
| `GET /experiments/:id/results?days=` | 各变体效果与显著性 |
| `GET /search?days=7&limit=50` | 搜索分析：热门词、无点击的词、搜索后点击率 |
| `GET /artists?days=30&q=&page=&pageSize=` | 歌手排行 |
| `GET /artists/:name?days=30` | 歌手详情：统计、代表作、相似歌手、听众数走势 |
| `GET /users/:userId` | 新增 `preferences`、`feedback`（见下） |
| `GET /users/:userId/recommendations` | scene 支持全部场景；autoplay 用 `seeds=wy:1,tx:2`；新增 `mixId`、`artist` |
| `GET /users/:userId/mixes` | 这个人的风格歌单 |
| `GET /users/:userId/report?period=&offset=` | 这个人的听歌报告 |
| `GET /models/:runId` | 新增 `ranker`（见下） |
| `GET /overview` | 新增 `models`（按模型版本的点击率）、`experiments`（运行中的实验摘要）、`feedbackReasons` |

### 返回结构（TypeScript）

```ts
type MusicRuleTarget = { type: "work" | "artist"; workKey?: string; artist?: string; title?: string; track?: MusicTrack | null };
type MusicRule = {
  id: number; kind: "block" | "boost" | "pin"; target: MusicRuleTarget;
  scenes: string[];                 // 空 = 全部场景（block 恒为全部）
  weight: number;                   // boost：0.1–10
  position: number;                 // pin：1–20（第几位）
  startsAt: number | null; endsAt: number | null;   // 毫秒，null = 不限
  enabled: boolean; note: string;
  createdAt: number; updatedAt: number; operator: string;
  stats: { served: number; clicks: number };        // 近 7 天被这条规则影响的曝光与点击（block 恒 0）
};
// POST/PUT 请求体：{ kind, target: { type, workKey? | artist? | source?+songId? }, scenes, weight, position, startsAt, endsAt, enabled, note }
// target 给 source + songId 时服务端换成作品键；给不存在的作品回 40000。

type MusicExperimentVariant = {
  key: string;                      // "A"、"B"…（A 为对照）
  name: string; traffic: number;    // 百分比，全部变体之和 ≤ 100（剩下的不进实验）
  overrides: {
    blend?: { als?: number; itemcf?: number; artist?: number; trending?: number; explore?: number };
    exploreRatio?: number; artistCap?: number; ranker?: boolean; foldIn?: boolean;
    modelRunId?: number;            // 固定用某次训练的模型（必须是成功的训练）
  };
};
type MusicExperiment = {
  id: number; name: string; description: string; scenes: string[];   // 至少一个场景
  status: "draft" | "running" | "stopped";
  variants: MusicExperimentVariant[];
  startedAt: number | null; stoppedAt: number | null;
  createdAt: number; updatedAt: number; operator: string;
};
// 同一场景同时只能有一个 running 的实验（start 时校验，冲突回 40900）。

type MusicExperimentResults = {
  experimentId: number; days: number;
  variants: Array<{
    key: string; name: string; users: number; requests: number; impressions: number;
    clicks: number; plays: number; completes: number; minutes: number;
    ctr: number; playRate: number; completionRate: number; minutesPerUser: number;
    lift: { ctr: number; playRate: number } | null;          // 相对 A 的提升（A 自己为 null）
    pValue: { ctr: number; playRate: number } | null;        // 两比例 z 检验
  }>;
  daily: Array<{ day: string; variant: string; ctr: number; playRate: number }>;
};

type MusicSearchStats = {
  days: number; searches: number; searchers: number; clickRate: number;   // 有 search_click 的搜索占比
  top: Array<{ query: string; searches: number; users: number; clicks: number; clickRate: number }>;
  noClick: Array<{ query: string; searches: number; users: number }>;     // 搜了没点的
  daily: Array<{ day: string; searches: number; clickRate: number }>;
};

type MusicArtistRow = { name: string; plays: number; listeners: number; minutes: number; tracks: number;
                        completionRate: number; skipRate: number; likes: number; cover: string };
type MusicArtistPage = { items: MusicArtistRow[]; total: number };
type MusicArtistDetail = MusicArtistRow & {
  daily: Array<{ day: string; plays: number; listeners: number }>;
  topTracks: Array<{ workKey: string; track: MusicTrack; plays: number; listeners: number }>;
  similar: Array<{ name: string; score: number; cover: string }>;          // 模型里的相似歌手
  dislikes: number;                                                         // 不喜欢这位歌手的人数
};

type MusicPreferences = { personalized: boolean; languages: string[]; artists: string[]; autoplay: boolean;
                          onboarded: boolean; updatedAt: number };
type MusicFeedbackItem = { kind: "track" | "artist" | "suppressed" | "version"; workKey: string; track: MusicTrack | null;
                           artist: string; reason: string; at: number; until: number };
// MusicUserDetail 新增：preferences: MusicPreferences | null; feedback: MusicFeedbackItem[];
//   realtime 新增：fatigue: Array<{ workKey: string; track: MusicTrack | null; count: number }>;
//   model 新增：foldIn: boolean（这次是否用了折入）、experiments: Array<{ id: number; name: string; variant: string }>

type MusicRanker = {
  enabled: boolean; samples: number; positives: number;
  auc: number; baselineAuc: number;                                        // 留出集 AUC；基线 = 融合分
  features: Array<{ name: string; weight: number }>;                       // 标准化后的权重
  reason: string;                                                          // 没启用时的原因，如「样本不足 2000」
} | null;
// MusicModelDetail 新增 ranker: MusicRanker；MusicModelRun.metrics 新增 auc?: number

// MusicOverview 新增：
//   models: Array<{ version: number; requests: number; impressions: number; clicks: number; plays: number; ctr: number; playRate: number }>;
//   experiments: Array<{ id: number; name: string; scenes: string[]; variants: number; startedAt: number }>;
//   feedbackReasons: Array<{ reason: string; count: number }>;
// MusicRecPreview 新增 experimentId、variant；MusicRecItem.channels 新增 foldin、ranker、rule、fatigue
```

### 应用配置增量

```json
{ "foldIn": true,
  "ranker": { "enabled": true, "minSamples": 2000, "l2": 0.001, "epochs": 8 },
  "fatigue": { "threshold": 3, "decay": 0.6 },
  "suppressDays": 30,
  "discover": { "size": 30, "popularityPenalty": 0.5 },
  "mixes": { "max": 6, "size": 25 },
  "autoplay": { "size": 10 } }
```

范围：ranker.minSamples 200–1000000、l2 0–1、epochs 1–50；fatigue.threshold 1–20、decay 0.1–1；suppressDays 1–180；
discover.size 10–50、popularityPenalty 0–2；mixes.max 0–10（0 = 关）、size 10–50；autoplay.size 5–30。

## 七、MCP

Aegis MCP 新增工具（领域前缀 `music_`）：`music_overview`、`music_user`、`music_preview`、`music_tracks`、`music_track`、
`music_artists`、`music_search_stats`、`music_models`、`music_train`、`music_publish`、`music_config`、`music_update_config`、
`music_rules`、`music_save_rule`、`music_delete_rule`、`music_experiments`、`music_save_experiment`、`music_experiment_action`、
`music_experiment_results`、`music_forget_user`。写操作（训练、发布、改配置、规则、实验、清除）照 MCP 约定先复述再执行。

## 八、客户端（安卓与电脑版）要做的事

1. **上报**：第一期的全部事件；`dislike` 带原因（长按 / 更多菜单里「不感兴趣」弹原因选择：不喜欢这首、听腻了、不喜欢这位歌手、音质差、歌曲不对）；
   `autoplay`、`report_view`、`onboarding`。
2. **首页**：为你推荐、每日推荐、每周雷达（discover）、风格歌单（mixes，横向卡片）、推荐歌手、私人电台入口。
3. **智能续播**：设置里「智能续播」（默认开，同步到服务端 `autoplay`）；播放队列剩 2 首且不是单曲循环时，
   POST scene=autoplay（seeds = 最近 5 首，exclude = 队列全部），接到队列末尾，队列里标「为你续播」。
4. **新用户偏好**：首次进入音乐首页且 `onboarding.needed` 时弹全屏选择页（语种多选 + 歌手网格多选，至少 3 位或跳过）。
5. **搜索**：搜索页空状态显示热搜；结果拿到后调重排（200 毫秒超时，超时用原顺序）。
6. **听歌报告**：周 / 月 / 年切换与翻页，大数字、Top 列表、时段分布、亮点句子；可分享成图片（客户端自己画）。
7. **我的不喜欢**：设置里「不感兴趣的内容」三个分组（歌曲、歌手、暂不推荐），可撤销。
8. **偏好设置**：个性化推荐开关（本地 + 服务端）、语种偏好、喜欢的歌手、智能续播、导出听歌记录（保存为 JSON 文件）、清除听歌数据。
9. **歌手页**：「为你推荐」排序（scene=artist）。
