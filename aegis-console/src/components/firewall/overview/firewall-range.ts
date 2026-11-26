import type { FirewallStatsQuery } from "@/lib/api/firewall";

/**
 * 防火墙总览与事件页共用的时间窗。
 *
 * 粒度由跨度决定：6 小时以内按分钟、7 天以内按小时、再长按天（服务端对 minute 另有 6 小时上限）。
 * 窗口只给起点、不给终点：服务端缺省终点为「现在」，实时刷新时新事件自然落进窗口。
 */

export type FirewallRangeKey = "1h" | "24h" | "7d" | "30d";
export type FirewallGranularity = NonNullable<FirewallStatsQuery["granularity"]>;

const HOUR = 3_600_000;
const DAY = 86_400_000;

export const FIREWALL_RANGES: ReadonlyArray<{ value: FirewallRangeKey; label: string; ms: number }> = [
  { value: "1h", label: "1 小时", ms: HOUR },
  { value: "24h", label: "24 小时", ms: 24 * HOUR },
  { value: "7d", label: "7 天", ms: 7 * DAY },
  { value: "30d", label: "30 天", ms: 30 * DAY }
];

export function rangeMs(key: FirewallRangeKey) {
  return FIREWALL_RANGES.find((item) => item.value === key)?.ms ?? 24 * HOUR;
}

export function granularityOf(key: FirewallRangeKey): FirewallGranularity {
  const ms = rangeMs(key);
  if (ms <= 6 * HOUR) return "minute";
  if (ms <= 7 * DAY) return "hour";
  return "day";
}

export const GRANULARITY_STEP: Record<FirewallGranularity, number> = {
  minute: 60_000,
  hour: HOUR,
  day: DAY
};

export const GRANULARITY_LABEL: Record<FirewallGranularity, string> = {
  minute: "按分钟",
  hour: "按小时",
  day: "按天"
};

/** 窗口起点：锚点向下取整到分钟，同一分钟内查询键不变 */
export function rangeStart(key: FirewallRangeKey, anchor: number) {
  const floored = Math.floor(anchor / 60_000) * 60_000;
  return new Date(floored - rangeMs(key)).toISOString();
}

/** 总览带过来的起点 → 最接近的预设（事件页的时间窗只有预设） */
export function nearestRange(startTime: string | undefined, now: number): FirewallRangeKey | null {
  if (!startTime) return null;
  const start = Date.parse(startTime);
  if (Number.isNaN(start)) return null;
  const span = Math.max(0, now - start);
  let best: FirewallRangeKey = "24h";
  let bestDiff = Number.POSITIVE_INFINITY;
  for (const item of FIREWALL_RANGES) {
    const diff = Math.abs(Math.log((span + 1) / item.ms));
    if (diff < bestDiff) {
      bestDiff = diff;
      best = item.value;
    }
  }
  return best;
}
