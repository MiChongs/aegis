import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 音乐推荐域 API（docs/music-personalization.md「管理端」）。
 *
 * 主服务把 `/api/admin/apps/:appkey/music/*` 原样反代到 music-rec，响应信封与其它管理接口一致，
 * `apiRequest` 已解开 `{code, data}`。音乐服务未开通（50396）或暂不可用（50397）时界面显示说明，
 * 不当成普通错误。时间一律毫秒时间戳；用户 ID 是应用内主键（跳 `/app-users/<appKey>/<id>` 用），
 * 昵称由控制台另按 ID 批量取。
 *
 * 下面的类型照抄契约文末「管理端返回结构」，改动时两边一起改；第二期（docs/music-personalization-2.md
 * 「六、管理端增量」）与第三期（docs/music-personalization-3.md「五、管理端增量」）新增的结构放在文件后半，
 * 已有结构只加字段。
 */

export type MusicTrack = { source: string; songId: string; title: string; artist?: string; album?: string; cover?: string; durationMs?: number };

export type MusicOverview = {
  days: number;
  from: number;
  to: number;
  totals: {
    events: number;
    listeners: number;
    plays: number;
    listeningMinutes: number;
    completionRate: number;
    skipRate: number;
    likes: number;
    searches: number;
    tracks: number;
  };
  /** 上一个同长周期，算环比 */
  previous: { events: number; listeners: number; plays: number; listeningMinutes: number };
  /** day 为 YYYY-MM-DD（服务器时区） */
  daily: Array<{ day: string; events: number; listeners: number; plays: number; listeningMinutes: number; completionRate: number; skipRate: number }>;
  eventTypes: Array<{ type: string; count: number }>;
  /** 24 个，每小时播放次数 */
  hours: number[];
  scenes: Array<{ scene: string; requests: number; impressions: number; clicks: number; plays: number; ctr: number; playRate: number }>;
  topTracks: Array<{ workKey: string; track: MusicTrack; plays: number; listeners: number; completionRate: number }>;
  topArtists: Array<{ name: string; plays: number; listeners: number }>;
  sources: Array<{ source: string; plays: number }>;
  platforms: Array<{ platform: string; listeners: number }>;
  /** 当前发布的模型 */
  model: MusicModelRun | null;
  /** 消费积压 */
  pipeline: { lagSeconds: number; pending: number; lastEventAt: number };
  /** 第二期：按模型版本的推荐效果 */
  models: Array<{ version: number; requests: number; impressions: number; clicks: number; plays: number; ctr: number; playRate: number }>;
  /** 第二期：运行中的实验摘要 */
  experiments: Array<{ id: number; name: string; scenes: string[]; variants: number; startedAt: number }>;
  /** 第二期：不感兴趣的原因分布 */
  feedbackReasons: Array<{ reason: string; count: number }>;
  /** 第三期：当前发布的深度模型摘要，没有为 null（服务端升级前缺省） */
  deep?: MusicDeepRun | null;
  /** 第三期：内容理解覆盖率，见 MusicOverviewContent */
  content?: MusicOverviewContent;
};

export type MusicUserRow = { userId: number; minutes: number; plays: number; tracks: number; likes: number; skipRate: number; lastActiveAt: number };
export type MusicUserPage = { items: MusicUserRow[]; total: number };

/** 与客户端画像同形 */
export type MusicProfile = {
  days: number;
  listeningMinutes: number;
  plays: number;
  tracks: number;
  artists: number;
  completionRate: number;
  skipRate: number;
  likes: number;
  activeDays: number;
  longestStreak: number;
  newTrackShare: number;
  topArtists: Array<{ name: string; plays: number; minutes: number; share: number }>;
  topTracks: Array<{ track: MusicTrack; plays: number; minutes: number }>;
  hours: number[];
  weekdays: number[];
  sources: Array<{ source: string; plays: number }>;
  persona: { key: string; title: string; text: string };
  traits: Array<{ key: string; title: string; value: number }>;
  updatedAt: number;
};

export type MusicUserDetail = {
  userId: number;
  profile: MusicProfile;
  realtime: {
    /** 最近播放（≤ 50） */
    recent: Array<{ workKey: string; track: MusicTrack | null; at: number }>;
    /** 实时正反馈 */
    positive: Array<{ workKey: string; track: MusicTrack | null; weight: number }>;
    /** 不感兴趣 / 早跳 */
    negative: Array<{ workKey: string; track: MusicTrack | null; at: number }>;
    dislikedArtists: Array<{ name: string; at: number }>;
    /** 第二期：近 7 天被展示未点击的次数（曝光疲劳） */
    fatigue: Array<{ workKey: string; track: MusicTrack | null; count: number }>;
  };
  model: {
    inModel: boolean;
    version: number;
    interactions: number;
    topArtists: Array<{ name: string; weight: number }>;
    /** 第二期：这次是否用了实时折入 */
    foldIn: boolean;
    /** 第二期：所在实验与命中的变体 */
    experiments: Array<{ id: number; name: string; variant: string }>;
  };
  /** 第二期：偏好设置，没设置过为 null */
  preferences: MusicPreferences | null;
  /** 第二期：不喜欢列表（歌曲、歌手、暂不推荐、版本屏蔽） */
  feedback: MusicFeedbackItem[];
};

export type MusicEventContext = {
  page?: string;
  section?: string;
  position?: number;
  query?: string;
  recId?: string;
  scene?: string;
  playlistId?: string;
  queueSource?: string;
};

export type MusicEvent = {
  id: string;
  type: string;
  ts: number;
  receivedAt: number;
  userId: number;
  deviceId: string;
  sessionId: string;
  workKey: string;
  track: MusicTrack | null;
  playedMs: number;
  positionMs: number;
  reason: string;
  context: MusicEventContext;
  platform: string;
  appVersion: string;
  network: string;
  props: Record<string, string>;
};
/** /events */
export type MusicEventPage = { items: MusicEvent[]; total: number };
/** /users/:id/events */
export type MusicTimeline = { items: MusicEvent[]; nextBefore: number | null };

export type MusicRecItem = {
  workKey: string;
  track: MusicTrack;
  score: number;
  reason: { kind: string; text: string; refTitle?: string };
  /**
   * 预览才有：als / itemcf / artist / trending / explore / penalty；第二期加 foldin / ranker / rule / fatigue；
   * 第三期加 seq（序列模型，参与融合）、llm（来自大模型扩展为 1）、tag_match、semantic（0–1 的相似度）
   */
  channels: Record<string, number>;
  /** 第三期：待客户端解析的大模型扩展歌。这时 track 只有 title、artist，source、songId 为空 */
  resolve?: boolean;
};
export type MusicRecPreview = {
  recId: string;
  scene: string;
  modelVersion: number;
  personalized: boolean;
  coldStart: boolean;
  items: MusicRecItem[];
  tookMs: number;
  /** 第二期：命中的实验，不在实验里时为 0 与 "" */
  experimentId: number;
  variant: string;
};

export type MusicTrackRow = {
  workKey: string;
  track: MusicTrack;
  plays: number;
  listeners: number;
  minutes: number;
  completionRate: number;
  skipRate: number;
  likes: number;
};
export type MusicTrackPage = { items: MusicTrackRow[]; total: number };
export type MusicTrackDetail = {
  workKey: string;
  track: MusicTrack;
  variants: Array<{ source: string; songId: string; plays: number; lastSeenAt: number }>;
  stats: { plays: number; listeners: number; minutes: number; completionRate: number; skipRate: number; likes: number; dislikes: number };
  daily: Array<{ day: string; plays: number; listeners: number }>;
  similar: Array<{ workKey: string; track: MusicTrack | null; score: number; via: "itemcf" | "als" }>;
  /** 第三期：内容标签，没打过为 null（服务端升级前缺省） */
  tags?: MusicTags | null;
};

export type MusicModelMetrics = { recall20: number; ndcg20: number; hitRate20: number; coverage: number };

export type MusicModelRun = {
  id: number;
  status: "queued" | "running" | "succeeded" | "failed";
  trigger: "manual" | "schedule";
  note: string;
  operator: string;
  createdAt: number;
  startedAt: number | null;
  finishedAt: number | null;
  publishedAt: number | null;
  current: boolean;
  users: number;
  items: number;
  interactions: number;
  sizeBytes: number;
  /** auc 为第二期新增：精排留出集 AUC，没训练精排时缺省 */
  metrics: (MusicModelMetrics & { baseline: MusicModelMetrics; testUsers: number; auc?: number }) | null;
  error: string;
};
export type MusicModelPage = { items: MusicModelRun[]; total: number };
export type MusicModelDetail = MusicModelRun & {
  /** 训练时用的配置快照 */
  params: MusicConfig;
  /** load / als / itemcf / trending / eval / save */
  stages: Array<{ name: string; ms: number }>;
  log: string[];
  /** 第二期：精排；没训练或没启用时见 reason，旧版本为 null */
  ranker: MusicRanker;
};

export type MusicConfig = {
  enabled: boolean;
  collectEnabled: boolean;
  windowDays: number;
  halfLifeDays: number;
  minItemUsers: number;
  weights: {
    complete: number;
    partial: number;
    skip: number;
    like: number;
    dislike: number;
    playlistAdd: number;
    download: number;
    share: number;
    searchClick: number;
    click: number;
    seekBack: number;
  };
  als: { factors: number; iterations: number; regularization: number; alpha: number };
  itemcf: { neighbors: number; sessionBoost: number };
  /** seq 为第三期新增（序列模型），0–5，默认 1.2 */
  blend: { als: number; itemcf: number; artist: number; trending: number; explore: number; seq: number };
  artistCap: number;
  recentExcludeDays: number;
  exploreRatio: number;
  schedule: string;
  autoPublish: boolean;
  minLift: number;
  /* ── 第二期 ── */
  foldIn: boolean;
  ranker: { enabled: boolean; minSamples: number; l2: number; epochs: number };
  fatigue: { threshold: number; decay: number };
  suppressDays: number;
  discover: { size: number; popularityPenalty: number };
  mixes: { max: number; size: number };
  autoplay: { size: number };
  /* ── 第三期 ── */
  deep: MusicDeepConfig;
  content: MusicContentConfig;
  updatedAt: number;
  updatedBy: string;
};

/** PUT /config 的请求体：updatedAt / updatedBy 由服务端写 */
export type MusicConfigInput = Omit<MusicConfig, "updatedAt" | "updatedBy">;

export type MusicScene = "home" | "daily" | "radio" | "similar" | "autoplay" | "discover" | "mix" | "artist";
export type MusicTrackSort = "plays" | "listeners" | "completion" | "skip";

/** 音乐服务未开通 / 暂不可用：界面显示说明而不是报错 */
export const MUSIC_SERVICE_CODES = {
  notEnabled: 50396,
  unavailable: 50397
} as const;

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/music`;

export function getMusicOverview(token: string, appKey: string, days: number) {
  return apiRequest<MusicOverview>(`${base(appKey)}/overview${buildQuery({ days })}`, { token });
}

export function listMusicUsers(token: string, appKey: string, params: { days?: number; q?: string; page?: number; pageSize?: number }) {
  return apiRequest<MusicUserPage>(`${base(appKey)}/users${buildQuery(params)}`, { token });
}

export function getMusicUser(token: string, appKey: string, userId: number) {
  return apiRequest<MusicUserDetail>(`${base(appKey)}/users/${userId}`, { token });
}

export function getMusicUserTimeline(token: string, appKey: string, userId: number, params: { type?: string; before?: number | null; limit?: number }) {
  return apiRequest<MusicTimeline>(`${base(appKey)}/users/${userId}/events${buildQuery(params)}`, { token });
}

export type MusicRecPreviewParams = {
  scene: MusicScene;
  limit?: number;
  /** similar 的种子 */
  source?: string;
  songId?: string;
  /** autoplay 的种子：`wy:1,tx:2`，最近放的在后 */
  seeds?: string;
  /** scene=mix */
  mixId?: string;
  /** scene=artist */
  artist?: string;
};

export function getMusicUserRecommendations(
  token: string,
  appKey: string,
  userId: number,
  params: MusicRecPreviewParams
) {
  return apiRequest<MusicRecPreview>(`${base(appKey)}/users/${userId}/recommendations${buildQuery(params)}`, { token });
}

export function forgetMusicUser(token: string, appKey: string, userId: number) {
  return apiRequest<unknown>(`${base(appKey)}/users/${userId}/forget`, { method: "POST", token });
}

export function listMusicTracks(
  token: string,
  appKey: string,
  params: { days?: number; q?: string; sort?: MusicTrackSort; page?: number; pageSize?: number }
) {
  return apiRequest<MusicTrackPage>(`${base(appKey)}/tracks${buildQuery(params)}`, { token });
}

export function getMusicTrack(token: string, appKey: string, workKey: string) {
  return apiRequest<MusicTrackDetail>(`${base(appKey)}/tracks/${encodeURIComponent(workKey)}`, { token });
}

export type MusicEventQuery = {
  type?: string;
  userId?: number;
  q?: string;
  /** 毫秒 */
  from?: number;
  /** 毫秒 */
  to?: number;
  page?: number;
  pageSize?: number;
};

export function listMusicEvents(token: string, appKey: string, params: MusicEventQuery) {
  return apiRequest<MusicEventPage>(`${base(appKey)}/events${buildQuery(params)}`, { token });
}

export function listMusicModels(token: string, appKey: string, params: { page?: number; pageSize?: number }) {
  return apiRequest<MusicModelPage>(`${base(appKey)}/models${buildQuery(params)}`, { token });
}

export function getMusicModel(token: string, appKey: string, runId: number) {
  return apiRequest<MusicModelDetail>(`${base(appKey)}/models/${runId}`, { token });
}

export function trainMusicModel(token: string, appKey: string, note?: string) {
  return apiRequest<MusicModelRun>(`${base(appKey)}/models/train`, {
    method: "POST",
    token,
    body: JSON.stringify(note ? { note } : {})
  });
}

export function publishMusicModel(token: string, appKey: string, runId: number) {
  return apiRequest<unknown>(`${base(appKey)}/models/${runId}/publish`, { method: "POST", token });
}

export function getMusicConfig(token: string, appKey: string) {
  return apiRequest<MusicConfig>(`${base(appKey)}/config`, { token });
}

export function updateMusicConfig(token: string, appKey: string, config: MusicConfigInput) {
  return apiRequest<MusicConfig>(`${base(appKey)}/config`, {
    method: "PUT",
    token,
    body: JSON.stringify(config)
  });
}

/* ───────────────────────── 第二期（docs/music-personalization-2.md「六、管理端增量」） ───────────────────────── */

/** 精排模型；快照里没有精排时为 null */
export type MusicRanker = {
  enabled: boolean;
  samples: number;
  positives: number;
  /** 留出集 AUC */
  auc: number;
  /** 基线 = 融合分 */
  baselineAuc: number;
  /** 标准化后的权重，顺序同 model.RankFeatures */
  features: Array<{ name: string; weight: number }>;
  /** 没启用时的原因，如「样本不足 2000」 */
  reason: string;
} | null;

export type MusicPreferences = {
  personalized: boolean;
  languages: string[];
  artists: string[];
  autoplay: boolean;
  onboarded: boolean;
  updatedAt: number;
};

export type MusicFeedbackKind = "track" | "artist" | "suppressed" | "version";
export type MusicFeedbackItem = {
  kind: MusicFeedbackKind;
  workKey: string;
  track: MusicTrack | null;
  artist: string;
  reason: string;
  at: number;
  /** suppressed 的到期时刻，其余为 0 */
  until: number;
};

/* ── 运营干预规则 ── */

export type MusicRuleKind = "block" | "boost" | "pin";
export type MusicRuleTarget = { type: "work" | "artist"; workKey?: string; artist?: string; title?: string; track?: MusicTrack | null };
export type MusicRule = {
  id: number;
  kind: MusicRuleKind;
  target: MusicRuleTarget;
  /** 空 = 全部场景（block 恒为全部） */
  scenes: string[];
  /** boost：0.1–10 */
  weight: number;
  /** pin：1–20（第几位） */
  position: number;
  /** 毫秒，null = 不限 */
  startsAt: number | null;
  endsAt: number | null;
  enabled: boolean;
  note: string;
  createdAt: number;
  updatedAt: number;
  operator: string;
  /** 近 7 天被这条规则影响的曝光与点击（block 恒 0） */
  stats: { served: number; clicks: number };
};
/** 契约只写了「规则列表」，外层按其它列表接口的习惯取 `{ items }` */
export type MusicRuleList = { items: MusicRule[] };
/** target 给 source + songId 时服务端换成作品键；给不存在的作品回 40000 */
export type MusicRuleInput = {
  kind: MusicRuleKind;
  target: { type: "work" | "artist"; workKey?: string; artist?: string; source?: string; songId?: string };
  scenes: string[];
  weight: number;
  position: number;
  startsAt: number | null;
  endsAt: number | null;
  enabled: boolean;
  note: string;
};

/* ── A/B 实验 ── */

/** 实验能覆盖的融合权重：契约只列了第二期的五个通道，第三期的 seq 不在实验覆盖项里 */
export type MusicBlend = Omit<MusicConfig["blend"], "seq">;
export type MusicExperimentOverrides = {
  blend?: Partial<MusicBlend>;
  exploreRatio?: number;
  artistCap?: number;
  ranker?: boolean;
  foldIn?: boolean;
  /** 固定用某次训练的模型（必须是成功的训练） */
  modelRunId?: number;
};
export type MusicExperimentVariant = {
  /** "A"、"B"…（A 为对照） */
  key: string;
  name: string;
  /** 百分比，全部变体之和 ≤ 100（剩下的不进实验） */
  traffic: number;
  overrides: MusicExperimentOverrides;
};
export type MusicExperimentStatus = "draft" | "running" | "stopped";
export type MusicExperiment = {
  id: number;
  name: string;
  description: string;
  /** 至少一个场景；同一场景同时只能有一个 running 的实验（start 时校验，冲突回 40900） */
  scenes: string[];
  status: MusicExperimentStatus;
  variants: MusicExperimentVariant[];
  startedAt: number | null;
  stoppedAt: number | null;
  createdAt: number;
  updatedAt: number;
  operator: string;
};
/** 外层同规则列表，取 `{ items }` */
export type MusicExperimentList = { items: MusicExperiment[] };
export type MusicExperimentInput = Pick<MusicExperiment, "name" | "description" | "scenes" | "variants">;

export type MusicExperimentResults = {
  experimentId: number;
  days: number;
  variants: Array<{
    key: string;
    name: string;
    users: number;
    requests: number;
    impressions: number;
    clicks: number;
    plays: number;
    completes: number;
    minutes: number;
    ctr: number;
    playRate: number;
    completionRate: number;
    minutesPerUser: number;
    /** 相对 A 的提升（A 自己为 null） */
    lift: { ctr: number; playRate: number } | null;
    /** 两比例 z 检验 */
    pValue: { ctr: number; playRate: number } | null;
  }>;
  daily: Array<{ day: string; variant: string; ctr: number; playRate: number }>;
};

/* ── 搜索与歌手 ── */

export type MusicSearchStats = {
  days: number;
  searches: number;
  searchers: number;
  /** 有 search_click 的搜索占比 */
  clickRate: number;
  top: Array<{ query: string; searches: number; users: number; clicks: number; clickRate: number }>;
  /** 搜了没点的 */
  noClick: Array<{ query: string; searches: number; users: number }>;
  daily: Array<{ day: string; searches: number; clickRate: number }>;
};

export type MusicArtistRow = {
  name: string;
  plays: number;
  listeners: number;
  minutes: number;
  tracks: number;
  completionRate: number;
  skipRate: number;
  likes: number;
  cover: string;
};
export type MusicArtistPage = { items: MusicArtistRow[]; total: number };
export type MusicArtistDetail = MusicArtistRow & {
  daily: Array<{ day: string; plays: number; listeners: number }>;
  topTracks: Array<{ workKey: string; track: MusicTrack; plays: number; listeners: number }>;
  /** 模型里的相似歌手 */
  similar: Array<{ name: string; score: number; cover: string }>;
  /** 不喜欢这位歌手的人数 */
  dislikes: number;
};

/* ── 用户的风格歌单与听歌报告（与客户端同形） ── */

export type MusicMix = {
  id: string;
  title: string;
  subtitle: string;
  covers: string[];
  artists: string[];
  size: number;
  lang: string;
};
export type MusicMixes = { items: MusicMix[]; generatedAt: number; expiresAt: number };

export type MusicReportPeriod = "week" | "month" | "year";
export type MusicReport = {
  period: MusicReportPeriod;
  from: number;
  to: number;
  label: string;
  listeningMinutes: number;
  plays: number;
  tracks: number;
  artists: number;
  newTracks: number;
  newArtists: number;
  activeDays: number;
  previous: { listeningMinutes: number; plays: number };
  topTracks: Array<{ track: MusicTrack; plays: number; minutes: number }>;
  topArtists: Array<{ name: string; plays: number; minutes: number; cover: string }>;
  discoveries: Array<{ name: string; firstAt: number; plays: number; cover: string }>;
  firstTrack: { track: MusicTrack; at: number } | null;
  /** 0–5 点之间最晚的一次 */
  latestNight: { track: MusicTrack; at: number } | null;
  mostRepeated: { track: MusicTrack; day: string; plays: number } | null;
  peakDay: { day: string; minutes: number } | null;
  /** 24 个，分钟 */
  hours: number[];
  /** 7 个，分钟，周一起 */
  weekdays: number[];
  /** 12 个，仅 year */
  months: number[];
  persona: { key: string; title: string; text: string };
  highlights: string[];
  empty: boolean;
};

/** 报告最多回看：52 周 / 24 月 / 5 年 */
export const MUSIC_REPORT_MAX_OFFSET: Record<MusicReportPeriod, number> = { week: 52, month: 24, year: 5 };

export function listMusicRules(token: string, appKey: string) {
  return apiRequest<MusicRuleList>(`${base(appKey)}/rules`, { token });
}

export function createMusicRule(token: string, appKey: string, input: MusicRuleInput) {
  return apiRequest<MusicRule>(`${base(appKey)}/rules`, { method: "POST", token, body: JSON.stringify(input) });
}

export function updateMusicRule(token: string, appKey: string, id: number, input: MusicRuleInput) {
  return apiRequest<MusicRule>(`${base(appKey)}/rules/${id}`, { method: "PUT", token, body: JSON.stringify(input) });
}

export function deleteMusicRule(token: string, appKey: string, id: number) {
  return apiRequest<unknown>(`${base(appKey)}/rules/${id}`, { method: "DELETE", token });
}

export function listMusicExperiments(token: string, appKey: string) {
  return apiRequest<MusicExperimentList>(`${base(appKey)}/experiments`, { token });
}

export function createMusicExperiment(token: string, appKey: string, input: MusicExperimentInput) {
  return apiRequest<MusicExperiment>(`${base(appKey)}/experiments`, { method: "POST", token, body: JSON.stringify(input) });
}

export function updateMusicExperiment(token: string, appKey: string, id: number, input: MusicExperimentInput) {
  return apiRequest<MusicExperiment>(`${base(appKey)}/experiments/${id}`, { method: "PUT", token, body: JSON.stringify(input) });
}

/** 运行中不能删，先停止 */
export function deleteMusicExperiment(token: string, appKey: string, id: number) {
  return apiRequest<unknown>(`${base(appKey)}/experiments/${id}`, { method: "DELETE", token });
}

/** 同一场景已有运行中的实验时回 40900 */
export function startMusicExperiment(token: string, appKey: string, id: number) {
  return apiRequest<MusicExperiment>(`${base(appKey)}/experiments/${id}/start`, { method: "POST", token });
}

export function stopMusicExperiment(token: string, appKey: string, id: number) {
  return apiRequest<MusicExperiment>(`${base(appKey)}/experiments/${id}/stop`, { method: "POST", token });
}

export function getMusicExperimentResults(token: string, appKey: string, id: number, days?: number) {
  return apiRequest<MusicExperimentResults>(`${base(appKey)}/experiments/${id}/results${buildQuery({ days })}`, { token });
}

export function getMusicSearchStats(token: string, appKey: string, params: { days?: number; limit?: number }) {
  return apiRequest<MusicSearchStats>(`${base(appKey)}/search${buildQuery(params)}`, { token });
}

export function listMusicArtists(token: string, appKey: string, params: { days?: number; q?: string; page?: number; pageSize?: number }) {
  return apiRequest<MusicArtistPage>(`${base(appKey)}/artists${buildQuery(params)}`, { token });
}

export function getMusicArtist(token: string, appKey: string, name: string, days?: number) {
  return apiRequest<MusicArtistDetail>(`${base(appKey)}/artists/${encodeURIComponent(name)}${buildQuery({ days })}`, { token });
}

export function getMusicUserMixes(token: string, appKey: string, userId: number) {
  return apiRequest<MusicMixes>(`${base(appKey)}/users/${userId}/mixes`, { token });
}

export function getMusicUserReport(token: string, appKey: string, userId: number, params: { period: MusicReportPeriod; offset?: number }) {
  return apiRequest<MusicReport>(`${base(appKey)}/users/${userId}/report${buildQuery(params)}`, { token });
}

/** 第二期配置项的默认值（契约「应用配置增量」）：服务端尚未返回这些字段时用来补齐表单 */
export const MUSIC_CONFIG_V2_DEFAULTS: Pick<MusicConfig, "foldIn" | "ranker" | "fatigue" | "suppressDays" | "discover" | "mixes" | "autoplay"> = {
  foldIn: true,
  ranker: { enabled: true, minSamples: 2000, l2: 0.001, epochs: 8 },
  fatigue: { threshold: 3, decay: 0.6 },
  suppressDays: 30,
  discover: { size: 30, popularityPenalty: 0.5 },
  mixes: { max: 6, size: 25 },
  autoplay: { size: 10 }
};


/* ───────────────────────── 第三期（docs/music-personalization-3.md「五、管理端增量」） ───────────────────────── */

export type MusicDeepConfig = {
  enabled: boolean;
  /** 32 / 48 / 64 / 96 / 128 */
  d: number;
  /** 1–4 */
  layers: number;
  /** 1 / 2 / 4，且能整除 d */
  heads: number;
  /** 10–200 */
  maxLen: number;
  /** 1–100 */
  epochs: number;
  /** 1–50：被多少人交互过才有独立 ID 向量 */
  minItemCount: number;
  /** 0–3：自动发布要求 NDCG@20 不低于热度基线的倍数 */
  minLift: number;
};

export type MusicContentConfig = {
  enabled: boolean;
  /** 100–200000：每天最多给多少首新歌打标签 */
  dailyLimit: number;
  llmExpand: boolean;
  /** 0–0.5：一页里扩展歌最多占比 */
  expandRatio: number;
};

/** 作品的内容标签；键固定，见契约「二、内容理解」的标签表 */
export type MusicTags = {
  genres: string[];
  moods: string[];
  scenes: string[];
  /** 1960s … 2020s 或空 */
  era: string;
  /** 0–1 */
  energy: number;
  /** male / female / group / none / 空 */
  vocal: string;
  /** live / cover / instrumental / remix / acoustic 的子集 */
  flags: string[];
  /** 0–1：大模型对这首歌熟不熟 */
  confidence: number;
  model: string;
  taggedAt: number;
  /** 人工改过 */
  manual: boolean;
};

/** PUT /content/tracks/:workKey 的请求体：改后服务端重算语义向量 */
export type MusicTagsInput = Pick<MusicTags, "genres" | "moods" | "scenes" | "era" | "energy" | "vocal" | "flags">;

export type MusicDeepMetrics = {
  recall20: number;
  ndcg20: number;
  hitRate20: number;
  baseline: { recall20: number; ndcg20: number; hitRate20: number };
  testUsers: number;
  epochs: number;
  trainMs: number;
};

/** LightGBM LambdaRank 精排：没导出时只有 enabled=false 与 reason */
export type MusicDeepRanker = {
  enabled: boolean;
  reason?: string;
  ndcg10?: number;
  baselineNdcg10?: number;
  samples?: number;
  groups?: number;
  importance?: Array<{ name: string; gain: number }>;
} | null;

export type MusicDeepRun = {
  id: number;
  status: "queued" | "running" | "succeeded" | "failed";
  /** auto：ALS 训练成功后自动投递 */
  trigger: "manual" | "auto" | "schedule";
  note: string;
  operator: string;
  createdAt: number;
  startedAt: number | null;
  finishedAt: number | null;
  publishedAt: number | null;
  current: boolean;
  users: number;
  items: number;
  sequences: number;
  error: string;
  metrics: MusicDeepMetrics | null;
  ranker: MusicDeepRanker;
};
/** 外层同 /models，取 `{ items, total }` */
export type MusicDeepRunPage = { items: MusicDeepRun[]; total: number };
export type MusicDeepRunDetail = MusicDeepRun & {
  params: Record<string, unknown>;
  stages: Array<{ name: string; ms: number }>;
  log: string[];
  /** 数值对齐：Go 推理与 PyTorch 逐用例对比，每维误差 > 1e-3 即不通过 */
  parity: { cases: number; maxError: number; ok: boolean } | null;
};

export type MusicContentUsage = { calls: number; tokens: number; items: number };
export type MusicContentOverview = {
  tracks: number;
  tagged: number;
  embedded: number;
  pending: number;
  failed: number;
  /** 服务端没配 MUSIC_LLM_*：不打标签、不做曲库扩展 */
  llmConfigured: boolean;
  today: MusicContentUsage;
  daily: Array<{ day: string } & MusicContentUsage>;
  genres: Array<{ key: string; count: number }>;
  moods: Array<{ key: string; count: number }>;
};

export type MusicContentStatus = "tagged" | "pending" | "failed";
export type MusicContentTrack = { workKey: string; track: MusicTrack | null; tags: MusicTags | null; status: MusicContentStatus; attempts: number };
/** 外层同其它分页列表，取 `{ items, total }` */
export type MusicContentTrackPage = { items: MusicContentTrack[]; total: number };

/**
 * 总览里的内容理解覆盖率。契约只写了「已打标签占比」，没给形状：
 * 可能是 0–1 的比例，也可能是带计数的对象，界面经 `contentCoverage()` 统一读。
 */
export type MusicOverviewContent =
  | number
  | { ratio?: number; tracks?: number; tagged?: number; embedded?: number }
  | null
  | undefined;

/** 第三期配置项的默认值：服务端尚未返回这些字段时用来补齐表单 */
export const MUSIC_CONFIG_V3_DEFAULTS: Pick<MusicConfig, "deep" | "content"> & { blendSeq: number } = {
  deep: { enabled: true, d: 64, layers: 2, heads: 2, maxLen: 50, epochs: 20, minItemCount: 3, minLift: 1 },
  content: { enabled: true, dailyLimit: 5000, llmExpand: true, expandRatio: 0.3 },
  blendSeq: 1.2
};

export function listMusicDeepRuns(token: string, appKey: string, params: { page?: number; pageSize?: number }) {
  return apiRequest<MusicDeepRunPage>(`${base(appKey)}/deep/runs${buildQuery(params)}`, { token });
}

export function getMusicDeepRun(token: string, appKey: string, runId: number) {
  return apiRequest<MusicDeepRunDetail>(`${base(appKey)}/deep/runs/${runId}`, { token });
}

export function trainMusicDeepModel(token: string, appKey: string, note?: string) {
  return apiRequest<MusicDeepRun>(`${base(appKey)}/deep/train`, {
    method: "POST",
    token,
    body: JSON.stringify(note ? { note } : {})
  });
}

/** 发布与回滚是同一个接口：发布比当前旧的版本就是回滚 */
export function publishMusicDeepRun(token: string, appKey: string, runId: number) {
  return apiRequest<unknown>(`${base(appKey)}/deep/runs/${runId}/publish`, { method: "POST", token });
}

export function getMusicContentOverview(token: string, appKey: string, days?: number) {
  return apiRequest<MusicContentOverview>(`${base(appKey)}/content${buildQuery({ days })}`, { token });
}

export function listMusicContentTracks(
  token: string,
  appKey: string,
  params: { status?: MusicContentStatus; q?: string; page?: number; pageSize?: number }
) {
  return apiRequest<MusicContentTrackPage>(`${base(appKey)}/content/tracks${buildQuery(params)}`, { token });
}

export function updateMusicTrackTags(token: string, appKey: string, workKey: string, input: MusicTagsInput) {
  return apiRequest<unknown>(`${base(appKey)}/content/tracks/${encodeURIComponent(workKey)}`, {
    method: "PUT",
    token,
    body: JSON.stringify(input)
  });
}

/** 契约没写返回结构；有 `queued` 时界面显示加入队列的首数 */
export function retagMusicTracks(token: string, appKey: string, input: { workKeys?: string[]; all?: boolean }) {
  return apiRequest<{ queued?: number } | null>(`${base(appKey)}/content/retag`, {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}
