/**
 * 音乐推荐的统一文案层：事件、场景、音源、通道、模型状态的叫法与数值格式都只在这里定义。
 *
 * 规则同风控平台（components/risk/kit/humanize.ts）：文案正式简洁，不用「·」分隔；给运营看的地方不露字段名，
 * 只有模型指标保留 Recall@20 / NDCG@20 这类业内通用名，并配一句短说明。
 */
import type {
  MusicContentStatus,
  MusicDeepRun,
  MusicEvent,
  MusicExperimentStatus,
  MusicFeedbackKind,
  MusicModelRun,
  MusicReportPeriod,
  MusicRuleKind,
  MusicScene,
  MusicTrackSort
} from "@/lib/api/music";

export const EVENT_LABEL: Record<string, string> = {
  play_start: "开始播放",
  play_end: "播放结束",
  pause: "暂停",
  resume: "继续播放",
  seek: "拖动进度",
  like: "收藏",
  unlike: "取消收藏",
  dislike: "不感兴趣",
  playlist_add: "加入歌单",
  download: "下载",
  share: "分享",
  search: "搜索",
  search_click: "点击搜索结果",
  click: "点击歌曲",
  impression: "推荐展示",
  lyrics_open: "打开歌词",
  undislike: "撤销不感兴趣",
  autoplay: "智能续播",
  report_view: "查看听歌报告",
  onboarding: "新用户偏好"
};

export const EVENT_TYPES = Object.keys(EVENT_LABEL);

export function eventLabel(type: string) {
  return EVENT_LABEL[type] ?? type;
}

export const SCENE_LABEL: Record<string, string> = {
  home: "为你推荐",
  daily: "每日推荐",
  radio: "私人电台",
  similar: "相似歌曲",
  autoplay: "智能续播",
  discover: "每周雷达",
  mix: "风格歌单",
  artist: "歌手热门"
};

export const SCENES: MusicScene[] = ["home", "daily", "radio", "similar", "autoplay", "discover", "mix", "artist"];

export function sceneLabel(scene: string) {
  return SCENE_LABEL[scene] ?? scene;
}

export const SOURCE_LABEL: Record<string, string> = {
  wy: "网易云音乐",
  tx: "QQ 音乐",
  kg: "酷狗音乐",
  kw: "酷我音乐",
  mg: "咪咕音乐",
  local: "本地音乐"
};

export function sourceLabel(source: string) {
  return SOURCE_LABEL[source] ?? source.toUpperCase();
}

export const PLATFORM_LABEL: Record<string, string> = {
  android: "安卓",
  ios: "iOS",
  harmony: "鸿蒙",
  windows: "Windows",
  linux: "Linux",
  macos: "macOS",
  desktop: "电脑版",
  web: "网页"
};

export function platformLabel(platform: string) {
  if (!platform) return "未知";
  return PLATFORM_LABEL[platform.toLowerCase()] ?? platform;
}

export const PLAY_END_REASON: Record<string, string> = {
  completed: "听完",
  skipped: "跳过",
  switched: "切歌",
  stopped: "停止",
  error: "播放出错"
};

/** 推荐分数拆解的通道 */
export const CHANNEL_LABEL: Record<string, string> = {
  als: "口味模型",
  itemcf: "相似歌曲",
  artist: "常听歌手",
  trending: "近期热门",
  explore: "探索",
  penalty: "降权",
  foldin: "实时口味",
  ranker: "精排",
  rule: "运营干预",
  fatigue: "曝光疲劳",
  seq: "序列模型",
  llm: "大模型扩展",
  tag_match: "标签匹配",
  semantic: "语义相似"
};

/** 叠加成融合分的通道（堆叠条按这个顺序画）；foldin 为第二期的实时折入，seq 为第三期的序列模型 */
export const CHANNEL_ORDER = ["als", "itemcf", "artist", "trending", "explore", "foldin", "seq"] as const;

/** 第二期：作用在融合分之后的调整项，单独列出不进堆叠条 */
export const ADJUST_CHANNELS = ["ranker", "rule", "fatigue"] as const;

/** 第三期：内容特征。tag_match、semantic 为 0–1 的相似度，llm 为 1 表示来自大模型扩展；只进精排，不进堆叠条 */
export const CONTENT_CHANNELS = ["tag_match", "semantic", "llm"] as const;

/** 通道配色：同一通道在各页面同一个颜色 */
export const CHANNEL_COLOR: Record<string, string> = {
  als: "bg-sky-500",
  itemcf: "bg-violet-500",
  artist: "bg-amber-500",
  trending: "bg-rose-500",
  explore: "bg-emerald-500",
  foldin: "bg-teal-500",
  seq: "bg-indigo-500",
  penalty: "bg-zinc-400"
};

export function channelLabel(key: string) {
  return CHANNEL_LABEL[key] ?? key;
}

export const REASON_KIND_LABEL: Record<string, string> = {
  because_track: "因为喜欢某首歌",
  artist: "常听歌手",
  similar_users: "口味相近的人在听",
  trending: "近期热门",
  explore: "换换口味",
  seed: "与种子歌曲相似"
};

export const SORT_LABEL: Record<MusicTrackSort, string> = {
  plays: "播放次数",
  listeners: "听众",
  completion: "完播率",
  skip: "跳过率"
};

export const MODEL_STATUS: Record<MusicModelRun["status"], { label: string; tone: "secondary" | "info" | "success" | "danger" }> = {
  queued: { label: "排队中", tone: "secondary" },
  running: { label: "训练中", tone: "info" },
  succeeded: { label: "成功", tone: "success" },
  failed: { label: "失败", tone: "danger" }
};

export const TRIGGER_LABEL: Record<MusicModelRun["trigger"], string> = {
  manual: "手动",
  schedule: "定时"
};

export const STAGE_LABEL: Record<string, string> = {
  load: "读取行为数据",
  als: "训练口味模型",
  itemcf: "计算相似歌曲",
  trending: "统计热度",
  eval: "离线评估",
  save: "保存模型"
};

/** 模型指标：名称 + 一句说明（指标名保留业内叫法，说明给运营看） */
export const METRIC_META = [
  { key: "ndcg20", label: "NDCG@20", note: "用户之后真正听的歌在前 20 条推荐里排得越靠前越高" },
  { key: "recall20", label: "Recall@20", note: "用户之后真正听的歌，有多少出现在前 20 条推荐里" },
  { key: "hitRate20", label: "命中率", note: "前 20 条推荐里至少命中一首的用户占比" },
  { key: "coverage", label: "覆盖率", note: "推荐结果覆盖了曲库里多少作品，越高越不扎堆" }
] as const;

/* ── 第二期：反馈原因、规则、实验、精排特征、语种 ── */

/** 不感兴趣的原因（事件 props.reason） */
export const DISLIKE_REASON_LABEL: Record<string, string> = {
  not_interested: "不喜欢这首",
  heard_too_much: "听腻了",
  dislike_artist: "不喜欢这位歌手",
  poor_quality: "音质差",
  wrong_song: "歌曲不对"
};

export function dislikeReasonLabel(reason: string) {
  if (!reason) return DISLIKE_REASON_LABEL.not_interested;
  return DISLIKE_REASON_LABEL[reason] ?? reason;
}

export const FEEDBACK_KIND_LABEL: Record<MusicFeedbackKind, string> = {
  track: "不喜欢的歌",
  artist: "不喜欢的歌手",
  suppressed: "暂不推荐",
  version: "屏蔽的版本"
};

export const RULE_KIND: Record<MusicRuleKind, { label: string; tone: "danger" | "info" | "warning"; note: string }> = {
  block: { label: "屏蔽", tone: "danger", note: "全部场景直接过滤" },
  boost: { label: "加权", tone: "info", note: "分数乘以权重" },
  pin: { label: "置顶", tone: "warning", note: "插入到指定位置，每人每天最多一次" }
};

export const EXPERIMENT_STATUS: Record<MusicExperimentStatus, { label: string; tone: "secondary" | "success" | "outline" }> = {
  draft: { label: "草稿", tone: "secondary" },
  running: { label: "运行中", tone: "success" },
  stopped: { label: "已停止", tone: "outline" }
};

/** 精排特征（model.RankFeatures 的顺序与名字） */
export const RANK_FEATURE_LABEL: Record<string, string> = {
  als: "口味模型",
  itemcf: "相似歌曲",
  artist: "常听歌手",
  trending: "近期热门",
  explore: "探索",
  foldin: "实时口味",
  pop: "作品热度",
  complete: "作品完播率",
  skip: "作品早跳率",
  artist_aff: "歌手偏好",
  recent: "近期播放过",
  fatigue: "曝光疲劳",
  lang_match: "语种匹配",
  fresh: "从没听过",
  pref_artist: "偏好里选过的歌手",
  position: "展示位置",
  seq: "序列模型",
  tag_match: "标签匹配",
  semantic: "语义相似",
  llm: "大模型扩展"
};

export function rankFeatureLabel(name: string) {
  return RANK_FEATURE_LABEL[name] ?? name;
}

export const LANG_LABEL: Record<string, string> = {
  zh: "华语",
  en: "欧美",
  ja: "日语",
  ko: "韩语",
  other: "其他"
};

export function langLabel(lang: string) {
  return LANG_LABEL[lang] ?? lang;
}

export const REPORT_PERIOD_LABEL: Record<MusicReportPeriod, string> = { week: "周报", month: "月报", year: "年报" };

/** 显著性阈值：p 值低于它算显著 */
export const SIGNIFICANCE = 0.05;

export function pValueText(p: number | null | undefined) {
  if (typeof p !== "number" || !Number.isFinite(p)) return "—";
  if (p < 0.001) return "< 0.001";
  return p.toFixed(3);
}

/** 相对提升：0.123 → 「+12.3%」 */
export function signedPct(ratio: number | null | undefined) {
  if (typeof ratio !== "number" || !Number.isFinite(ratio)) return "—";
  const v = ratio * 100;
  return `${v >= 0 ? "+" : "−"}${Math.abs(v).toFixed(Math.abs(v) >= 100 ? 0 : 1)}%`;
}

/* ── 数值 ── */

const nf = new Intl.NumberFormat("zh-CN");

export function num(value: number | null | undefined) {
  return typeof value === "number" && Number.isFinite(value) ? nf.format(value) : "—";
}

/** 紧凑数量：「1.2万」 */
export function compact(value: number) {
  if (Math.abs(value) >= 1e8) return `${(value / 1e8).toFixed(1)}亿`;
  if (Math.abs(value) >= 1e4) return `${(value / 1e4).toFixed(value >= 1e5 ? 0 : 1)}万`;
  return nf.format(Math.round(value));
}

/** 0–1 的比例 → 「71.2%」；极小值保留两位 */
export function pct(ratio: number | null | undefined, digits?: number) {
  if (typeof ratio !== "number" || !Number.isFinite(ratio)) return "—";
  const v = ratio * 100;
  const d = digits ?? (v === 0 ? 0 : Math.abs(v) >= 10 ? 1 : Math.abs(v) >= 1 ? 1 : 2);
  return `${v.toFixed(d)}%`;
}

/** 听歌时长（分钟）：不足 10 小时按分钟，否则按小时 */
export function minutesText(minutes: number) {
  if (!Number.isFinite(minutes)) return "—";
  if (minutes < 600) return `${nf.format(Math.round(minutes))} 分钟`;
  return `${compact(Math.round(minutes / 60))} 小时`;
}

/** 歌曲时长 / 播放时长（毫秒）→ 「3:12」「1:02:03」 */
export function clockText(ms: number | null | undefined) {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "—";
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

/** 耗时（毫秒）→ 「820 毫秒」「12.4 秒」「3 分 20 秒」 */
export function elapsedText(ms: number | null | undefined) {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1000) return `${Math.round(ms)} 毫秒`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} 秒`;
  const m = Math.floor(ms / 60_000);
  const s = Math.round((ms % 60_000) / 1000);
  if (m < 60) return s ? `${m} 分 ${s} 秒` : `${m} 分钟`;
  const h = Math.floor(m / 60);
  return `${h} 小时 ${m % 60} 分`;
}

/** 积压延迟（秒） */
export function lagText(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "无延迟";
  if (seconds < 60) return `${Math.round(seconds)} 秒`;
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`;
  return `${(seconds / 3600).toFixed(1)} 小时`;
}

export function bytesText(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "—";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 100 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

/** 本期相对上期的变化；上期为 0 时无法比较 */
export function changeOf(current: number, previous: number) {
  if (!previous || previous <= 0) return null;
  return (current - previous) / previous;
}

export function changeWord(ratio: number | null) {
  if (ratio === null) return "";
  const v = Math.abs(ratio) * 100;
  if (v < 1) return "基本持平";
  return `${ratio > 0 ? "增加" : "减少"} ${v >= 100 ? Math.round(v) : v.toFixed(v >= 10 ? 0 : 1)}%`;
}

/** 模型指标相对基线的提升：「+18.2%」 */
export function liftText(value: number, baseline: number) {
  if (!baseline) return value > 0 ? "基线为 0" : "—";
  const lift = (value - baseline) / baseline;
  return `${lift >= 0 ? "+" : "−"}${Math.abs(lift * 100).toFixed(1)}%`;
}

/** 模型版本耗时 */
export function runDuration(run: Pick<MusicModelRun, "startedAt" | "finishedAt">) {
  if (!run.startedAt || !run.finishedAt) return null;
  return run.finishedAt - run.startedAt;
}

/* ── 事件的一句话 ── */

// context.position 按客户端上报原样显示（契约未写明从 0 还是从 1 起），不自行加一。

/** 「听了 3:12，跳过」「搜索「周杰伦」」「为你推荐展示 20 首」 */
export function eventSummary(event: MusicEvent): string {
  const ctx = event.context ?? {};
  switch (event.type) {
    case "play_end": {
      const reason = PLAY_END_REASON[event.reason] ?? event.reason;
      const total = event.track?.durationMs;
      const played = clockText(event.playedMs);
      return total ? `听了 ${played} / ${clockText(total)}${reason ? `，${reason}` : ""}` : `听了 ${played}${reason ? `，${reason}` : ""}`;
    }
    case "seek": {
      const from = Number(event.props?.from);
      return Number.isFinite(from) ? `从 ${clockText(from)} 拖到 ${clockText(event.positionMs)}` : `拖到 ${clockText(event.positionMs)}`;
    }
    case "search":
      return ctx.query ? `搜索「${ctx.query}」` : "搜索";
    case "search_click":
      return ctx.position != null ? `在「${ctx.query ?? ""}」的结果里点了第 ${ctx.position} 位` : `在「${ctx.query ?? ""}」的结果里点击`;
    case "impression": {
      const count = event.props?.count;
      return `${sceneLabel(ctx.scene ?? "")}展示${count ? ` ${count} 首` : ""}`;
    }
    case "dislike":
      if (event.props?.scope === "artist") return DISLIKE_REASON_LABEL.dislike_artist;
      return dislikeReasonLabel(event.props?.reason ?? "");
    case "undislike":
      return event.props?.scope === "artist" ? `撤销不喜欢歌手${event.props?.artist ? `「${event.props.artist}」` : ""}` : "撤销不感兴趣";
    case "report_view": {
      const period = event.props?.period as MusicReportPeriod | undefined;
      return period && REPORT_PERIOD_LABEL[period] ? `打开${REPORT_PERIOD_LABEL[period]}` : "打开听歌报告";
    }
    case "onboarding":
      return event.props?.action === "skip" ? "跳过偏好选择" : event.props?.artists ? `选了 ${event.props.artists.split(",").filter(Boolean).length} 位歌手` : "完成偏好选择";
    case "autoplay":
      return "接上一批续播";
    case "playlist_add":
      return ctx.playlistId ? `加入歌单 ${ctx.playlistId}` : "加入歌单";
    case "click": {
      const where = [ctx.page, ctx.section].filter(Boolean).join(" / ");
      const at = ctx.position != null ? ` 第 ${ctx.position} 位` : "";
      return where ? `在 ${where}${at}点击` : "点击";
    }
    default:
      if (ctx.scene) return `来自${sceneLabel(ctx.scene)}`;
      return "";
  }
}

/** 星期标签，周一起（与画像的 weekdays 对齐） */
export const WEEKDAY_LABEL = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];

/** 小时分布的高峰：「21 点最多」 */
export function peakHour(hours: number[]) {
  let best = -1;
  let max = 0;
  hours.forEach((value, index) => {
    if (value > max) {
      max = value;
      best = index;
    }
  });
  return best >= 0 ? best : null;
}

/* ── 第三期：深度模型与内容理解 ── */

export const DEEP_TRIGGER_LABEL: Record<MusicDeepRun["trigger"], string> = {
  manual: "手动",
  auto: "随口味模型",
  schedule: "定时"
};

/** 深度模型的训练阶段。契约没列阶段名，这里收常见叫法，未收录的原样显示 */
export const DEEP_STAGE_LABEL: Record<string, string> = {
  load: "读取数据",
  items: "整理曲库",
  sequences: "构建序列",
  features: "物品特征",
  train: "训练序列模型",
  eval: "离线评估",
  final: "全量再训练",
  ranker: "训练精排",
  export: "导出模型",
  parity: "数值对齐",
  save: "保存产物",
  publish: "发布"
};

/** 深度模型参数快照的叫法；未收录的键原样显示 */
export const DEEP_PARAM_LABEL: Record<string, string> = {
  enabled: "深度模型",
  d: "表示维度",
  heads: "注意力头数",
  layers: "层数",
  ffn: "前馈层宽度",
  maxLen: "序列最大长度",
  dropout: "Dropout",
  epochs: "最多训练轮数",
  lr: "学习率",
  learningRate: "学习率",
  batchSize: "批大小",
  negatives: "负例数",
  minItemCount: "独立 ID 向量的最少交互人数",
  minLift: "自动发布要求的基线倍数",
  windowDays: "行为窗口",
  threads: "CPU 线程数",
  embedDim: "语义向量维度"
};

export const CONTENT_STATUS: Record<MusicContentStatus, { label: string; tone: "success" | "secondary" | "danger" }> = {
  tagged: { label: "已打标签", tone: "success" },
  pending: { label: "待打标签", tone: "secondary" },
  failed: { label: "失败", tone: "danger" }
};
