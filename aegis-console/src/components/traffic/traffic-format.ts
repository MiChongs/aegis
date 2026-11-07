import type {
  ConcurrencySpec,
  Counts,
  HeaderOp,
  LimitKey,
  LimitScope,
  LimitSpec,
  NativePluginState,
  QueueSpec,
  ResponseSpec,
  SimulateResult,
  SimulateStep,
  TrafficClass,
  TrafficMode,
  TrafficRule,
  WarmupSpec,
  WarmupState
} from "@/lib/api/traffic";

/**
 * 流量管理的中文名称、配色与数值格式。所有页面从这里取，同一个概念只有一种叫法。
 */

/* ── 名称 ── */

export const MODE_LABEL: Record<TrafficMode, string> = { enforce: "生效", observe: "观察", bypass: "放行" };
export const MODE_ORDER: TrafficMode[] = ["enforce", "observe", "bypass"];

export const CLASS_LABEL: Record<TrafficClass, string> = { critical: "关键", high: "高", normal: "普通", low: "低" };
export const CLASS_ORDER: TrafficClass[] = ["critical", "high", "normal", "low"];

/** 限速键在下拉框里的名字 */
export const KEY_LABEL: Record<LimitKey, string> = {
  global: "全局",
  ip: "IP",
  subnet: "网段",
  app: "应用",
  token: "凭据",
  header: "请求头",
  query: "查询参数"
};
export const KEY_ORDER: LimitKey[] = ["global", "ip", "subnet", "app", "token", "header", "query"];

export const SCOPE_LABEL: Record<LimitScope, string> = { local: "单实例", cluster: "集群" };

export const HEADER_OP_LABEL: Record<HeaderOp, string> = {
  exists: "存在",
  absent: "不存在",
  equals: "等于",
  prefix: "前缀为",
  contains: "包含"
};
export const HEADER_OP_ORDER: HeaderOp[] = ["equals", "prefix", "contains", "exists", "absent"];

export const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"] as const;

export const PERIODS: Array<{ ms: number; label: string }> = [
  { ms: 1_000, label: "秒" },
  { ms: 60_000, label: "分钟" },
  { ms: 3_600_000, label: "小时" },
  { ms: 86_400_000, label: "天" }
];

export const STATUS_OPTIONS = [429, 503, 403] as const;

export const ACTION_LABEL: Record<string, string> = {
  settings: "修改全局设置",
  "rule.create": "新建规则",
  "rule.update": "修改规则",
  "rule.delete": "删除规则",
  "rule.reorder": "调整顺序",
  "rule.toggle": "启停规则",
  rollback: "回滚",
  import: "导入",
  seed: "初始化"
};

export const EVENT_LABEL: Record<string, string> = {
  "warmup.start": "开始爬坡",
  "warmup.saturated": "爬坡到顶",
  "warmup.settled": "爬坡回落",
  "overload.start": "过载保护触发",
  "overload.end": "过载保护解除",
  "reject.burst": "拒绝突增",
  "concurrency.shrink": "并发收缩"
};

export const PLUGIN_STATE_LABEL: Record<NativePluginState, string> = {
  running: "运行中",
  stopped: "已停止",
  starting: "启动中",
  stopping: "停止中",
  failed: "异常"
};

export const WARMUP_STATE_LABEL: Record<WarmupState, string> = {
  stable: "平稳",
  ramping: "爬坡中",
  saturated: "已到顶",
  cooldown: "回落中"
};

export const OUTCOME_LABEL: Record<SimulateStep["outcome"], string> = {
  pass: "通过",
  reject: "拒绝",
  observe: "观察",
  bypass: "放行",
  skip: "跳过"
};

export const VERDICT_LABEL: Record<SimulateResult["verdict"], string> = {
  allow: "放行",
  reject: "拒绝",
  observe: "观察"
};

export const REJECT_PARTS: Array<{ key: keyof Counts; label: string }> = [
  { key: "rejectedRate", label: "限速" },
  { key: "rejectedConcurrency", label: "并发" },
  { key: "rejectedQueue", label: "队列满" },
  { key: "rejectedTimeout", label: "排队超时" },
  { key: "rejectedOverload", label: "过载" }
];

/* ── 状态色（语义色相：放行 / 排队 / 观察 / 拒绝） ── */

export type StreamKey = "arrivals" | "admitted" | "queued" | "observed" | "rejected";

export const STREAM: Record<
  StreamKey,
  { label: string; text: string; bg: string; fill: string; solid: string; stroke: string; soft: string; chart: { light: string; dark: string } }
> = {
  arrivals: {
    label: "到达",
    text: "text-sky-600 dark:text-sky-400",
    bg: "bg-sky-500",
    solid: "fill-sky-500",
    fill: "fill-sky-500/20 dark:fill-sky-400/20",
    stroke: "stroke-sky-600 dark:stroke-sky-300",
    soft: "bg-sky-500/12",
    chart: { light: "#0284c7", dark: "#38bdf8" }
  },
  admitted: {
    label: "放行",
    text: "text-emerald-600 dark:text-emerald-400",
    bg: "bg-emerald-500",
    solid: "fill-emerald-500",
    fill: "fill-emerald-500/22 dark:fill-emerald-400/22",
    stroke: "stroke-emerald-600 dark:stroke-emerald-300",
    soft: "bg-emerald-500/12",
    chart: { light: "#10b981", dark: "#34d399" }
  },
  queued: {
    label: "排队",
    text: "text-amber-600 dark:text-amber-400",
    bg: "bg-amber-500",
    solid: "fill-amber-500",
    fill: "fill-amber-500/24 dark:fill-amber-400/24",
    stroke: "stroke-amber-600 dark:stroke-amber-300",
    soft: "bg-amber-500/12",
    chart: { light: "#f59e0b", dark: "#fbbf24" }
  },
  observed: {
    label: "观察",
    text: "text-violet-600 dark:text-violet-400",
    bg: "bg-violet-500",
    solid: "fill-violet-500",
    fill: "fill-violet-500/22 dark:fill-violet-400/22",
    stroke: "stroke-violet-600 dark:stroke-violet-300",
    soft: "bg-violet-500/12",
    chart: { light: "#8b5cf6", dark: "#a78bfa" }
  },
  rejected: {
    label: "拒绝",
    text: "text-rose-600 dark:text-rose-400",
    bg: "bg-rose-500",
    solid: "fill-rose-500",
    fill: "fill-rose-500/22 dark:fill-rose-400/22",
    stroke: "stroke-rose-600 dark:stroke-rose-300",
    soft: "bg-rose-500/12",
    chart: { light: "#f43f5e", dark: "#fb7185" }
  }
};

/* ── 数值 ── */

const compact = new Intl.NumberFormat("zh-CN", { notation: "compact", maximumFractionDigits: 1 });
const plain = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 });

/** 1.2万、3.4亿；一万以下照常显示 */
export function fmtNum(value: number | null | undefined) {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  return Math.abs(value) >= 10_000 ? compact.format(value) : plain.format(value);
}

export const COMPACT_FLOW = { notation: "compact", maximumFractionDigits: 1 } as const;

/** 响应时间：小于 10ms 保留一位小数，过秒换算成秒 */
export function fmtMs(value: number | null | undefined) {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10_000 ? 0 : 1)} 秒`;
  if (value < 10) return `${value.toFixed(1)} 毫秒`;
  return `${Math.round(value)} 毫秒`;
}

/** 时长（毫秒入参）：1 秒、1.5 分钟 */
export function fmtDuration(ms: number) {
  if (ms < 1000) return `${ms} 毫秒`;
  if (ms < 60_000) return `${plain.format(ms / 1000)} 秒`;
  if (ms < 3_600_000) return `${plain.format(ms / 60_000)} 分钟`;
  return `${plain.format(ms / 3_600_000)} 小时`;
}

export function fmtPct(ratio: number) {
  if (!Number.isFinite(ratio)) return "—";
  const pct = ratio * 100;
  return `${pct >= 10 || pct === 0 ? Math.round(pct) : pct.toFixed(1)}%`;
}

export function rejectedOf(counts: Counts | null | undefined) {
  if (!counts) return 0;
  return (
    (counts.rejectedRate ?? 0) +
    (counts.rejectedConcurrency ?? 0) +
    (counts.rejectedQueue ?? 0) +
    (counts.rejectedTimeout ?? 0) +
    (counts.rejectedOverload ?? 0)
  );
}

export function periodLabel(periodMs: number) {
  const preset = PERIODS.find((item) => item.ms === periodMs);
  if (preset) return preset.label;
  return fmtDuration(periodMs);
}

/** 「每 IP 20/秒」「全局 300/分钟」「每 X-Device 5/秒」 */
export function limitPhrase(limit: Pick<LimitSpec, "key" | "name" | "rate" | "periodMs">) {
  const subject =
    limit.key === "global"
      ? "全局"
      : limit.key === "header" || limit.key === "query"
        ? `每 ${limit.name || KEY_LABEL[limit.key]}`
        : `每 ${KEY_LABEL[limit.key]}`;
  return `${subject} ${fmtNum(limit.rate)}/${periodLabel(limit.periodMs)}`;
}

export function concurrencyPhrase(spec: ConcurrencySpec) {
  return spec.adaptive ? `并发 ${fmtNum(spec.minLimit)}–${fmtNum(spec.maxLimit)} 自适应` : `并发 ${fmtNum(spec.max)}`;
}

export function queuePhrase(spec: QueueSpec) {
  return `排队 ${fmtNum(spec.size)} 等 ${fmtDuration(spec.timeoutMs)}`;
}

export function warmupPhrase(spec: WarmupSpec) {
  return `爬坡 ${fmtNum(spec.baselineRps)} 至 ${fmtNum(spec.maxRps)}/秒`;
}

export function responsePhrase(spec: ResponseSpec) {
  return `${spec.status} ${spec.code}`;
}

/* ── 规则形状：后端的 nil 切片会序列化成 null，进表单前统一铺成空数组 ── */

const arr = <T,>(value: T[] | null | undefined): T[] => (Array.isArray(value) ? value : []);

export function normalizeRuleShape(rule: TrafficRule): TrafficRule {
  const match = rule.match ?? ({} as TrafficRule["match"]);
  return {
    ...rule,
    id: rule.id ?? "",
    name: rule.name ?? "",
    description: rule.description ?? "",
    mode: rule.mode ?? "enforce",
    class: rule.class ?? "normal",
    terminal: Boolean(rule.terminal),
    enabled: rule.enabled ?? true,
    match: {
      methods: arr(match.methods),
      paths: arr(match.paths),
      excludePaths: arr(match.excludePaths),
      hosts: arr(match.hosts),
      appKeys: arr(match.appKeys),
      cidrs: arr(match.cidrs),
      excludeCidrs: arr(match.excludeCidrs),
      headers: arr(match.headers)
    },
    limits: arr(rule.limits),
    concurrency: rule.concurrency ?? null,
    queue: rule.queue ?? null,
    warmup: rule.warmup ?? null,
    response: rule.response ?? null
  };
}

export function blankRule(): TrafficRule {
  return {
    id: "",
    name: "",
    description: "",
    enabled: true,
    mode: "enforce",
    class: "normal",
    terminal: false,
    match: { methods: [], paths: [], excludePaths: [], hosts: [], appKeys: [], cidrs: [], excludeCidrs: [], headers: [] },
    limits: [{ key: "ip", name: "", rate: 20, periodMs: 1000, burst: 20, scope: "local", fallback: "ip" }],
    concurrency: null,
    queue: null,
    warmup: null,
    response: null
  };
}

export function blankLimit(): LimitSpec {
  return { key: "ip", name: "", rate: 20, periodMs: 1000, burst: 20, scope: "local", fallback: "ip" };
}

/** 预热曲线：从基线起每个周期抬高「基线 × stepPct%」，到 max 为止（与后端 warmup.evaluate 一致） */
export function warmupCurve(spec: WarmupSpec, maxSteps = 400) {
  const points: Array<{ t: number; ceiling: number }> = [];
  const baseline = Math.max(1, spec.baselineRps || 1);
  const max = Math.max(baseline, spec.maxRps || baseline);
  const step = Math.max(1, (baseline * Math.max(1, spec.stepPct || 1)) / 100);
  const interval = Math.max(100, spec.intervalMs || 1000);
  let ceiling = baseline;
  let t = 0;
  points.push({ t: 0, ceiling });
  for (let i = 0; i < maxSteps && ceiling < max; i += 1) {
    t += interval;
    ceiling = Math.min(max, ceiling + step);
    points.push({ t: t / 1000, ceiling });
  }
  return { points, seconds: t / 1000, reached: ceiling >= max };
}
