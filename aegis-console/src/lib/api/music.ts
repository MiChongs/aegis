import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 音乐推荐域 API（docs/music-personalization.md「管理端」）。
 *
 * 主服务把 `/api/admin/apps/:appkey/music/*` 原样反代到 music-rec，响应信封与其它管理接口一致，
 * `apiRequest` 已解开 `{code, data}`。音乐服务未开通（50396）或暂不可用（50397）时界面显示说明，
 * 不当成普通错误。时间一律毫秒时间戳；用户 ID 是应用内主键（跳 `/app-users/<appKey>/<id>` 用），
 * 昵称由控制台另按 ID 批量取。
 *
 * 下面的类型照抄契约文末「管理端返回结构」，改动时两边一起改。
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
  };
  model: { inModel: boolean; version: number; interactions: number; topArtists: Array<{ name: string; weight: number }> };
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
  /** 预览才有：als / itemcf / artist / trending / explore / penalty */
  channels: Record<string, number>;
};
export type MusicRecPreview = {
  recId: string;
  scene: string;
  modelVersion: number;
  personalized: boolean;
  coldStart: boolean;
  items: MusicRecItem[];
  tookMs: number;
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
  metrics: (MusicModelMetrics & { baseline: MusicModelMetrics; testUsers: number }) | null;
  error: string;
};
export type MusicModelPage = { items: MusicModelRun[]; total: number };
export type MusicModelDetail = MusicModelRun & {
  /** 训练时用的配置快照 */
  params: MusicConfig;
  /** load / als / itemcf / trending / eval / save */
  stages: Array<{ name: string; ms: number }>;
  log: string[];
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
  blend: { als: number; itemcf: number; artist: number; trending: number; explore: number };
  artistCap: number;
  recentExcludeDays: number;
  exploreRatio: number;
  schedule: string;
  autoPublish: boolean;
  minLift: number;
  updatedAt: number;
  updatedBy: string;
};

/** PUT /config 的请求体：updatedAt / updatedBy 由服务端写 */
export type MusicConfigInput = Omit<MusicConfig, "updatedAt" | "updatedBy">;

export type MusicScene = "home" | "daily" | "radio" | "similar";
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

export function getMusicUserRecommendations(
  token: string,
  appKey: string,
  userId: number,
  params: { scene: MusicScene; limit?: number; source?: string; songId?: string }
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
