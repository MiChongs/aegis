# 音乐个性化（第三期：强模型契约）

第一期（`music-personalization.md`）与第二期（`music-personalization-2.md`）的模型只有隐式 ALS、ItemCF 与逻辑回归精排，
全靠行为数据：数据少时几乎只剩热度。第三期补上三样东西：**内容理解**（大模型给每首歌打标签、算语义向量）、
**深度序列模型**（Transformer，物品表示融合 ID、歌手、语种、标签、语义向量，冷门与新歌也有表示）、
**LightGBM LambdaRank 精排**；再加**大模型曲库扩展**（行为少的人由大模型按口味推荐真实存在但曲库里还没有的歌，客户端按歌名搜索解析）。
本文是契约：Go（music-rec / music-trainer）、Python（music-deep）、控制台、客户端都照这里写。

## 一、架构

```
music-trainer（Go）
  ├─ 原有：ALS + ItemCF + 热度 + LR 精排（快照 gob）
  ├─ 新增 enricher：曲库里没打过标签的歌 → 大模型（OpenAI 兼容 chat，JSON）→ 标签；→ embedding 接口 → 语义向量；写 PG
  └─ ALS 训练成功后，深度模型开启时往 MUSIC_JOBS 投 music.job.deep.<app>
music-deep（Python 3.12，PyTorch CPU + LightGBM，新服务）
  └─ 消费 music.job.deep.*：读 ClickHouse 行为序列与 rank_samples、PG 曲库与标签 → 训练 SASRec 与 LightGBM →
     评估 → 导出（safetensors + meta + LightGBM 文本 + 数值对齐用例）写 PG → 达标自动发布（NATS music.deep.published）
music-rec（Go）
  └─ 加载已发布的深度模型：Go 原生实现 Transformer 前向（与 PyTorch 数值一致），新增通道 seq；LightGBM 用 leaves 推理；
     大模型扩展通道 llm（按人按天缓存）
```

没有配置大模型（`MUSIC_LLM_*` 为空）时：不打标签、不做曲库扩展，深度模型照常训练（内容特征为零）。

## 二、内容理解（music-trainer 的 enricher）

环境变量（music-trainer 与 music-rec 都读）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `MUSIC_LLM_BASE_URL` | — | OpenAI 兼容地址，如 `https://api.openai.com/v1` 或 Aegis 自己的 `/api/ai/v1` |
| `MUSIC_LLM_API_KEY` | — | |
| `MUSIC_LLM_MODEL` | `gpt-4o-mini` | 打标签与曲库扩展用的对话模型 |
| `MUSIC_EMBED_MODEL` | `text-embedding-3-small` | 语义向量模型 |
| `MUSIC_EMBED_DIM` | `256` | 向量维度（请求时带 `dimensions`；不支持的模型按返回长度截断或补零到此维度） |
| `MUSIC_LLM_CONCURRENCY` | `2` | 同时进行的大模型请求数 |

应用配置新增 `content`：`{ "enabled": true, "dailyLimit": 5000, "llmExpand": true, "expandRatio": 0.3 }`
（dailyLimit：每天最多给多少首新歌打标签，100–200000；expandRatio：一页里扩展歌最多占比，0–0.5）。

### 标签表（键固定，Go `contract/tags.go` 与 Python `music_deep/tags.py` 各一份，内容必须一致）

- **genres**（最多 3 个）：`pop` 流行、`rock` 摇滚、`folk` 民谣、`electronic` 电子、`hiphop` 嘻哈说唱、`rnb` R&B、`jazz` 爵士、
  `classical` 古典、`light` 轻音乐、`guofeng` 国风古风、`soundtrack` 影视原声、`acg` 动漫、`game` 游戏音乐、`children` 儿歌、
  `metal` 金属、`punk` 朋克、`indie` 独立、`country` 乡村、`blues` 布鲁斯、`reggae` 雷鬼、`latin` 拉丁、`dance` 舞曲、
  `postrock` 后摇、`instrumental` 器乐、`chinese_opera` 戏曲、`world` 世界音乐、`ethnic` 民族、`religious` 宗教、
  `internet` 网络歌曲、`city_pop` 城市流行、`ambient` 氛围、`lofi` Lo-fi（共 32）
- **moods**（最多 3 个）：`happy` 快乐、`sad` 伤感、`calm` 平静、`energetic` 活力、`romantic` 浪漫、`lonely` 孤独、`nostalgic` 怀旧、
  `healing` 治愈、`angry` 愤怒、`dreamy` 梦幻、`epic` 燃、`chill` 松弛、`sweet` 甜蜜、`dark` 暗黑、`hopeful` 希望、`tense` 紧张（共 16）
- **scenes**（最多 3 个）：`workout` 运动、`study` 学习、`sleep` 睡前、`commute` 通勤、`party` 派对、`driving` 开车、`work` 工作、
  `relax` 放松、`rainy` 雨天、`night` 深夜、`morning` 清晨、`travel` 旅行、`gaming` 打游戏、`wedding` 婚礼（共 14）
- **era**：`1960s` … `2020s` 或空；**energy**：0–1；**vocal**：`male` / `female` / `group` / `none` / 空；
  **flags**：`live`、`cover`、`instrumental`、`remix`、`acoustic` 的子集；**confidence**：0–1（大模型对这首歌熟不熟）。

大模型每次 20 首，请求体给 `[{ "i": 0, "title": "晴天", "artist": "周杰伦", "album": "叶惠美" }]`，要求只输出
`{ "items": [ { "i": 0, "genres": [...], "moods": [...], "scenes": [...], "era": "2000s", "energy": 0.4, "vocal": "male", "flags": [], "confidence": 0.95 } ] }`；
不在表里的键丢弃。语义向量的输入文本：`歌名 - 歌手；专辑：…；曲风：流行、民谣；情绪：…；场景：…；年代：2000s`。

歌手知识（每位歌手一次）：`{ "artist": "周杰伦", "similar": ["林俊杰", …最多 10], "genres": [...], "lang": "zh" }`，
用于数据少时补歌手相似度。

### PG 表（music-trainer 建）

```sql
music_track_tags (app_id, work_key, genres text[], moods text[], scenes text[], era text, energy real, vocal text,
                  flags text[], confidence real, embedding real[], model text, tagged_at timestamptz,
                  PRIMARY KEY (app_id, work_key))
music_artist_tags (app_id, artist text /* 规范化名 */, name text, similar_artists text[], genres text[], lang text, tagged_at timestamptz,
                   PRIMARY KEY (app_id, artist))
music_llm_usage (app_id, day date, kind text /* tag | embed | expand */, calls int, tokens bigint, items int, PRIMARY KEY (app_id, day, kind))
```

`embedding` 为 `MUSIC_EMBED_DIM` 维、L2 归一化。打标签失败的歌 1 小时后重试，连续 3 次失败当天不再试。

## 三、深度模型（music-deep，Python）

### 任务

- 主题 `music.job.deep.<app>`（MUSIC_JOBS 流，消费者 `music-deep`，工作队列），消息 `{ "run_id": 12, "app_id": 10000 }`。
- 训练记录表 `music_deep_runs`（Go 的 music-trainer 建表、建记录并投任务；music-deep 只更新）：
  `id bigserial, app_id, status (queued|running|succeeded|failed), trigger, note, operator, params jsonb, metrics jsonb,
  stages jsonb, log jsonb, users int, items int, sequences int, created_at, started_at, finished_at, published_at, heartbeat_at, error`，
  部分唯一索引：一个应用同时只有一个 queued / running。产物表 `music_deep_blobs (run_id, kind text, data bytea, PRIMARY KEY (run_id, kind))`，
  kind：`model`（safetensors）、`meta`（JSON）、`ranker`（LightGBM 文本，可无）、`parity`（JSON）。
- 发布：`published_at = now()` 后发 NATS（普通发布订阅）`music.deep.published`：`{ "app_id": 10000, "run_id": 12 }`。
  自动发布条件：留出集 NDCG@20 ≥ 热度基线 × `deep.minLift`（默认 1.0）且（有上一版时）≥ 上一版 × 0.97。

### 数据

- **物品**：PG `music_tracks` 里这个应用的全部作品（不只是被听过的），按 `work_key` 排序编号 0..N-1。
- **序列**：ClickHouse `events FINAL` 里窗口内（`windowDays`）的正向行为，按 (user, ts) 排序：
  play_end 且（completed 或播放 ≥ 30 秒）、like、playlist_add、download、share、search_click；同一作品连续出现只留一次；
  dislike 的作品从这个人的序列里删掉。长度 < 3 的人不进训练。
- **物品特征**：歌手（第一歌手规范化名，出现在 ≥ 2 首歌里才单独编号，否则归「其他」）、语种（`contract.DetectLang` 同规则，Python 复写）、
  标签多热（genres + moods + scenes + era + vocal + flags）、语义向量（256 维，没有时为 0 并有 `has_embedding` 标志位）、
  ID（被 ≥ `deep.minItemCount`（默认 3）人交互过才有独立 ID 向量，否则 0）。

### 模型（SASRec，pre-LN Transformer，因果注意力）

物品表示 `e_i = W_id[i]·m_i + W_artist[a_i] + W_lang[l_i] + mean_{t∈tags(i)} W_tag[t] + P · text_i`（m_i 为是否有独立 ID），维度 d。
默认 d=64、heads=2、layers=2、ffn=256、maxLen=50、dropout=0.2（只在训练时）。损失：批内 + 按热度采样的负例的 sampled softmax，
负例按 logQ 修正；AdamW，学习率 1e-3，epochs 默认 20，早停看留出集 NDCG@20。CPU 线程数取 `MUSIC_DEEP_THREADS`（默认 4）。

评估：每人最后一个正向行为为测试目标，前面为输入；在全部物品上打分（排除输入里出现过的），算 Recall@20、NDCG@20、HitRate@20，
与「热度」基线对比；之后用全部数据再训几轮作为导出模型（或直接导出最优轮）。

### 导出（Go 推理的唯一依据）

`model`：safetensors（小端 float32，键与形状如下；**线性层一律存成 [in, out]**，即 `y = x · W + b`；PyTorch 的 `nn.Linear.weight` 要转置）：

| 键 | 形状 |
|---|---|
| `item_repr` | [N, d]：已经算好的 e_i（含内容），第 k 行对应 meta.items[k] |
| `pos_emb` | [maxLen, d] |
| `blocks.{b}.ln1.weight` / `.bias` | [d] |
| `blocks.{b}.attn.wq` / `wk` / `wv` / `wo` | [d, d] |
| `blocks.{b}.attn.bq` / `bk` / `bv` / `bo` | [d] |
| `blocks.{b}.ln2.weight` / `.bias` | [d] |
| `blocks.{b}.ffn.w1` | [d, ffn]，`.b1` [ffn] |
| `blocks.{b}.ffn.w2` | [ffn, d]，`.b2` [d] |
| `final_ln.weight` / `.bias` | [d] |

前向（Go 原样实现）：输入序列 s（物品下标，旧 → 新，只取最后 maxLen 个，长度 n）。
1. `x_t = item_repr[s_t] + pos_emb[maxLen − n + t]`（t = 0..n−1，右对齐：最后一个永远用 pos_emb[maxLen−1]）。
2. 每个 block：`h = LN1(x)`；`Q = h·wq + bq`，K、V 同理；按 heads 切成 dh = d/heads；
   `A = softmax(Q·Kᵀ / √dh + mask)`，mask 为因果（j > t 为 −∞）；`x = x + concat(A·V)·wo + bo`；
   `h2 = LN2(x)`；`x = x + GELU(h2·w1 + b1)·w2 + b2`。
3. `u = final_ln(x_{n−1})`；物品 i 的分 `score_i = u · item_repr[i]`。

LayerNorm：`(x − mean) / sqrt(var + eps) · weight + bias`（var 为总体方差，eps 见 meta）；GELU 为精确版 `0.5·x·(1 + erf(x/√2))`。

`meta`（JSON）：

```json
{ "format": 1, "appId": 10000, "runId": 12, "d": 64, "heads": 2, "layers": 2, "ffn": 256, "maxLen": 50, "eps": 1e-5,
  "items": ["work_key0", "…"], "itemCount": [3, 0, …], "trainedAt": 1763280000000,
  "metrics": { "recall20": 0.31, "ndcg20": 0.18, "hitRate20": 0.31, "baseline": { "recall20": 0.08, "ndcg20": 0.04, "hitRate20": 0.08 },
               "testUsers": 812, "epochs": 14, "trainMs": 51234 },
  "rankerFeatures": ["als", "…20 个，见下"] }
```

`parity`（JSON）：`{ "cases": [ { "seq": [5, 9, 2], "user": [d 个 float], "top": [[idx, score], …5 个] } ] }`，至少 5 个用例；
Go 加载时逐个对比，`user` 每维误差 > 1e-3 就拒绝加载该版本（日志说明）。

### 精排（LightGBM LambdaRank）

- 样本：ClickHouse `rank_samples` 中特征长度为 **20** 的（第三期特征版本，见下），标签规则同第二期；按 `rec_id` 分组；
  `objective=lambdarank`、`metric=ndcg@10`，num_leaves 31，学习率 0.05，最多 300 棵树，早停 30；按时间留出 20% 评估 NDCG@10，
  与「融合分」（前 5 个特征按应用配置 blend 加权求和）排序的 NDCG@10 比较，更好才导出 `ranker`。
- 不导出时 meta 里 `ranker` 写 `{ "enabled": false, "reason": "…" }`；导出时 `{ "enabled": true, "ndcg10": …, "baselineNdcg10": …,
  "samples": …, "groups": …, "importance": [{ "name": "seq", "gain": 123.4 }, …] }`。
- LightGBM 4.x 的文本头是 `version=v4`，leaves 只认 v2 / v3：导出时改写为 `version=v3`（树格式没变，分数一致，`model/testdata` 有对齐用例）；lambdarank 加载时不做输出变换。
- Go 推理（leaves）：每个候选算原始分，**在本次候选里转成百分位 p ∈ [0, 1]**，最终分 = 融合分 × (0.5 + p)。

### 第三期精排特征（20 个，顺序固定，Go `model.RankFeatures` 与 Python 一致）

第二期的 16 个（als、itemcf、artist、trending、explore、foldin、pop、complete、skip、artist_aff、recent、fatigue、lang_match、fresh、
pref_artist、position）之后追加：
16 `seq`（序列模型分，在本次候选里按最大值归一）、17 `tag_match`（物品标签与用户近期标签分布的余弦）、
18 `semantic`（物品语义向量与用户近期正反馈语义向量均值的余弦，归一到 0–1）、19 `llm`（来自大模型扩展 = 1）。

## 四、推荐增量（music-rec）

- **seq 通道**：用户的输入序列 = 实时状态里最近的正反馈与播放（新 → 旧取 50，再反转为旧 → 新），不在模型物品表里的跳过；
  长度 ≥ 1 就算 u，对全部物品打分取前 400。新歌（曲库里有但没人听过）因为有内容表示，也能被召回。
  融合权重 `blend.seq`（默认 1.2）。场景 home / daily / radio / discover / autoplay（autoplay 的输入序列 = 请求里的 seeds）都用；similar 用种子歌单曲序列。
- **llm 通道（曲库扩展）**：`content.llmExpand` 开、配置了大模型、这个人模型里没有或交互 < 30 次（或场景为 discover）时：
  以「最近喜欢的 20 首 + 偏好歌手 + 语种」请大模型推荐 30 首**真实存在**、不在列表里的歌，结果按人按天缓存（Redis `llmexp:<day>`）；
  命中曲库（按作品键）的进普通打分，`llm` 特征为 1；没命中的作为**待解析项**，最多占一页的 `expandRatio`，按大模型给的顺序插在融合排序里（每 3 首插 1 首）。
- **标签与语义**：`tag_match`、`semantic` 两个特征；风格歌单的标题在有标签时改为「{主曲风}{主情绪}」风格，如「华语流行，治愈」（仍不用「·」）。

### 待解析项（客户端改动）

推荐结果的 item 新增 `"resolve": true`；这时 `track` 只有 `title`、`artist`（`source`、`songId` 为空），`workKey` 照常（歌名|歌手）。
客户端：展示时显示歌名歌手（封面用占位）；播放前用自己的多音源搜索 `歌名 歌手` 取第一个歌名与歌手都匹配（规范化后相等或包含）的结果；
搜不到就跳过这首并上报 `dislike`（`props.reason = wrong_song`、`props.resolve = failed`，track 只带 title / artist，source 与 songId 留空；服务端接受这种事件，把这个作品当作「暂不推荐」`suppressDays` 天，不算口味、不进曲库）。解析成功后的所有事件照常带真实的 source / songId，
作品键一致，服务端据此学到。主服务 `MusicRecItem` 透传 `resolve`。

## 五、管理端增量

| 方法与路径 | 说明 |
|---|---|
| `GET /deep/runs?page=&pageSize=` | 深度模型训练记录 |
| `GET /deep/runs/:id` | 详情：参数、指标对比热度、精排（NDCG@10、特征重要性）、各阶段耗时、日志、数值对齐结果 |
| `POST /deep/train` `{note?}` | 立即训练深度模型 |
| `POST /deep/runs/:id/publish` | 发布 / 回滚 |
| `GET /content?days=` | 内容理解总览：曲库作品数、已打标签数、有向量数、今日用量（调用、token、首数）、近 14 天每日用量、曲风 / 情绪分布、待打标签数、失败数 |
| `GET /content/tracks?status=tagged|pending|failed&q=&page=&pageSize=` | 作品标签列表 |
| `PUT /content/tracks/:workKey` | 人工修改标签（`{genres, moods, scenes, era, energy, vocal, flags}`，改后重算向量） |
| `POST /content/retag` `{workKeys?: [], all?: false}` | 重新打标签 |
| `GET /tracks/:workKey` | 新增 `tags`（同上结构，无则 null） |
| `GET /overview` | `model` 旁新增 `deep`（当前发布的深度模型摘要，无则 null）与 `content`（已打标签占比） |
| `GET /users/:userId/recommendations` | `channels` 新增 `seq`、`llm`、`tag_match`、`semantic`；结果项带 `resolve` |

TS：

```ts
type MusicTags = { genres: string[]; moods: string[]; scenes: string[]; era: string; energy: number; vocal: string;
                   flags: string[]; confidence: number; model: string; taggedAt: number; manual: boolean };
type MusicDeepRun = { id: number; status: "queued" | "running" | "succeeded" | "failed"; trigger: "manual" | "auto" | "schedule";
  note: string; operator: string; createdAt: number; startedAt: number | null; finishedAt: number | null; publishedAt: number | null;
  current: boolean; users: number; items: number; sequences: number; error: string;
  metrics: { recall20: number; ndcg20: number; hitRate20: number; baseline: { recall20: number; ndcg20: number; hitRate20: number };
             testUsers: number; epochs: number; trainMs: number } | null;
  ranker: { enabled: boolean; reason?: string; ndcg10?: number; baselineNdcg10?: number; samples?: number; groups?: number;
            importance?: Array<{ name: string; gain: number }> } | null };
type MusicDeepRunDetail = MusicDeepRun & { params: Record<string, unknown>; stages: Array<{ name: string; ms: number }>; log: string[];
  parity: { cases: number; maxError: number; ok: boolean } | null };
type MusicContentOverview = { tracks: number; tagged: number; embedded: number; pending: number; failed: number; llmConfigured: boolean;
  today: { calls: number; tokens: number; items: number }; daily: Array<{ day: string; calls: number; tokens: number; items: number }>;
  genres: Array<{ key: string; count: number }>; moods: Array<{ key: string; count: number }> };
type MusicContentTrack = { workKey: string; track: MusicTrack | null; tags: MusicTags | null; status: "tagged" | "pending" | "failed"; attempts: number };
```

应用配置新增：`"deep": { "enabled": true, "d": 64, "layers": 2, "heads": 2, "maxLen": 50, "epochs": 20, "minItemCount": 3, "minLift": 1.0 }`、
`"content": { … 见上 }`、`blend.seq`（0–5，默认 1.2）。范围：d ∈ {32, 48, 64, 96, 128}、layers 1–4、heads ∈ {1, 2, 4}（d 能整除）、maxLen 10–200、epochs 1–100、
minItemCount 1–50、minLift 0–3。

## 六、部署

- 新镜像 `deploy/docker/music-deep.Dockerfile`（python:3.12-slim，CPU 版 torch，依赖锁在 `services/music/deep/uv.lock`），
  compose 服务 `music-deep`（内存上限 6g，`MUSIC_DEEP_THREADS=4`），环境变量同其它音乐服务（`MUSIC_PG_DSN`、`MUSIC_CLICKHOUSE_*`、`MUSIC_NATS_URL`）。
- music-trainer / music-rec 加 `MUSIC_LLM_*`、`MUSIC_EMBED_*`。
