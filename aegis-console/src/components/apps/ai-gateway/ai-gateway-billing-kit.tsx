"use client";

import { useEffect, useState } from "react";
import { Clock3, Crown, Gauge, Globe2, Sparkles } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { formatPoints } from "@/components/apps/ai-gateway/ai-gateway-shared";
import type {
  AIGatewayCreditKind,
  AIGatewayFactorBreakdown,
  AIGatewayLotStatus,
  AIGatewayPackageTone,
  AIGatewayTimeTone,
  AIGatewayTimeWindow
} from "@/lib/api/ai-gateway";
import { formatDateTimeMinute } from "@/lib/datetime";
import { cn } from "@/lib/utils";

/**
 * 计费方案、额度商店、额度视图共用的零件：时段色调、折扣说法、倍率分解、批次与流水的中文名，
 * 以及一份与后端 `aigateway` 规则一致的时段求值（时间轴与热力图在前端自算，不必每改一格都请求一次）。
 */

// ── 色调 ──

export type ToneMeta = {
  label: string;
  /** 实底（条、格子）。 */
  solid: string;
  /** SVG 填充。 */
  fill: string;
  /** 浅底 + 文字（徽标）。 */
  soft: string;
  /** 文字色。 */
  text: string;
  /** 左侧色条 / 描边。 */
  ring: string;
};

export const TIME_TONES: Record<AIGatewayTimeTone, ToneMeta> = {
  off_peak: {
    label: "闲时",
    solid: "bg-emerald-500",
    fill: "fill-emerald-500",
    soft: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    text: "text-emerald-600 dark:text-emerald-400",
    ring: "border-emerald-500"
  },
  peak: {
    label: "高峰",
    solid: "bg-rose-500",
    fill: "fill-rose-500",
    soft: "bg-rose-500/10 text-rose-700 dark:text-rose-300",
    text: "text-rose-600 dark:text-rose-400",
    ring: "border-rose-500"
  },
  promo: {
    label: "活动",
    solid: "bg-violet-500",
    fill: "fill-violet-500",
    soft: "bg-violet-500/10 text-violet-700 dark:text-violet-300",
    text: "text-violet-600 dark:text-violet-400",
    ring: "border-violet-500"
  },
  normal: {
    label: "普通",
    solid: "bg-sky-500",
    fill: "fill-sky-500",
    soft: "bg-sky-500/10 text-sky-700 dark:text-sky-300",
    text: "text-sky-600 dark:text-sky-400",
    ring: "border-sky-500"
  }
};

export const TIME_TONE_KEYS = Object.keys(TIME_TONES) as AIGatewayTimeTone[];

export function toneMeta(tone?: string | null): ToneMeta {
  return TIME_TONES[(tone as AIGatewayTimeTone) ?? "normal"] ?? TIME_TONES.normal;
}

/** 倍率偏离 1 越多颜色越深：0.25（几乎不变）… 1（半价以下或加价一倍以上）。 */
export function toneStrength(multiplier: number) {
  if (!Number.isFinite(multiplier) || multiplier === 1) return 0.28;
  const delta = multiplier < 1 ? (1 - multiplier) * 2 : multiplier - 1;
  return Math.min(1, 0.32 + Math.min(delta, 1) * 0.68);
}

/** 额度包配色：卡片头部的实底、浅底与文字。 */
export const PACKAGE_TONES: Record<AIGatewayPackageTone, { label: string; solid: string; soft: string; text: string; ring: string }> = {
  blue: { label: "蓝", solid: "bg-blue-600", soft: "bg-blue-500/10", text: "text-blue-600 dark:text-blue-400", ring: "ring-blue-500/50" },
  violet: { label: "紫", solid: "bg-violet-600", soft: "bg-violet-500/10", text: "text-violet-600 dark:text-violet-400", ring: "ring-violet-500/50" },
  amber: { label: "金", solid: "bg-amber-500", soft: "bg-amber-500/10", text: "text-amber-600 dark:text-amber-400", ring: "ring-amber-500/50" },
  emerald: { label: "绿", solid: "bg-emerald-600", soft: "bg-emerald-500/10", text: "text-emerald-600 dark:text-emerald-400", ring: "ring-emerald-500/50" },
  rose: { label: "红", solid: "bg-rose-600", soft: "bg-rose-500/10", text: "text-rose-600 dark:text-rose-400", ring: "ring-rose-500/50" },
  slate: { label: "灰", solid: "bg-slate-700", soft: "bg-slate-500/10", text: "text-slate-700 dark:text-slate-300", ring: "ring-slate-500/50" }
};

export const PACKAGE_TONE_KEYS = Object.keys(PACKAGE_TONES) as AIGatewayPackageTone[];

export function packageTone(tone?: string | null) {
  return PACKAGE_TONES[(tone as AIGatewayPackageTone) ?? "blue"] ?? PACKAGE_TONES.blue;
}

// ── 数字说法 ──

function trimNumber(value: number, digits = 2) {
  return Number(value.toFixed(digits)).toString();
}

/** 倍率：×0.5。 */
export function formatFactor(value?: string | number | null) {
  const num = Number(value);
  if (value === null || value === undefined || value === "" || !Number.isFinite(num)) return "—";
  return `×${trimNumber(num, 4)}`;
}

/**
 * 倍率的中文说法：0.7 → 7 折，0.85 → 85 折，1 → 原价，1.2 → 加价 20%。
 */
export function discountLabel(value?: string | number | null) {
  const num = Number(value);
  if (value === null || value === undefined || value === "" || !Number.isFinite(num)) return "—";
  if (num === 1) return "原价";
  if (num > 1) return `加价 ${trimNumber((num - 1) * 100, 1)}%`;
  if (num <= 0) return "免费";
  const tens = num * 10;
  if (Math.abs(tens - Math.round(tens)) < 1e-9) return `${Math.round(tens)} 折`;
  return `${trimNumber(num * 100, 1)} 折`;
}

/** 比例：0.1 → 10%。 */
export function formatRate(value?: string | number | null) {
  const num = Number(value);
  if (!Number.isFinite(num)) return "—";
  return `${trimNumber(num * 100, 2)}%`;
}

export function formatYuan(value?: string | number | null) {
  const num = Number(value);
  if (value === null || value === undefined || value === "" || !Number.isFinite(num)) return "—";
  return `¥${num.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

/** 大额度的紧凑写法：12000 → 1.2 万。 */
export function formatCompactPoints(value?: string | number | null) {
  const num = Number(value);
  if (!Number.isFinite(num)) return "—";
  if (Math.abs(num) >= 100_000_000) return `${trimNumber(num / 100_000_000, 2)} 亿`;
  if (Math.abs(num) >= 10_000) return `${trimNumber(num / 10_000, 2)} 万`;
  return formatPoints(num);
}

/** 小数输入：非负、最多 6 位小数。 */
export const DECIMAL_RE = /^\d+(\.\d{1,6})?$/;

/** 去掉十进制字符串末尾的 0。 */
export function trimDecimal(value?: string | null, fallback = "0") {
  if (value === null || value === undefined || value === "") return fallback;
  return value.includes(".") ? value.replace(/0+$/, "").replace(/\.$/, "") : value;
}

// ── 流水与批次 ──

export const CREDIT_KIND_LABEL: Record<AIGatewayCreditKind, string> = {
  grant: "发放",
  consume: "调用消费",
  refund: "调用退款",
  adjust: "管理员调整",
  purchase: "购买额度包",
  welcome: "新人礼",
  expire: "到期清零",
  revoke: "作废",
  order_refund: "额度包退款"
};

export const BUCKET_LABEL: Record<string, string> = { credit: "永久额度", pack: "额度包" };

export const LOT_ORIGIN_LABEL: Record<string, string> = {
  purchase: "购买",
  welcome: "新人礼",
  admin: "管理员发放",
  grant: "活动发放"
};

export const LOT_STATUS: Record<AIGatewayLotStatus, { label: string; variant: "success" | "outline" | "danger" | "warning" }> = {
  active: { label: "可用", variant: "success" },
  exhausted: { label: "已用完", variant: "outline" },
  expired: { label: "已过期", variant: "warning" },
  revoked: { label: "已作废", variant: "danger" }
};

export function LotStatusBadge({ status }: { status: AIGatewayLotStatus }) {
  const meta = LOT_STATUS[status] ?? { label: status, variant: "outline" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

export const ORDER_STATUS: Record<string, { label: string; variant: "success" | "info" | "outline" }> = {
  paid: { label: "已支付", variant: "success" },
  refunded: { label: "已退款", variant: "info" }
};

export function OrderStatusBadge({ status }: { status: string }) {
  const meta = ORDER_STATUS[status] ?? { label: status, variant: "outline" as const };
  return (
    <Badge variant={meta.variant} size="sm">
      {meta.label}
    </Badge>
  );
}

// ── 倍率分解 ──

/**
 * 倍率分解：全局 × 会员 × 等级 × 时段 = 合计。stacking = best 且时段打折时，会员与时段只取更优惠的一个，
 * 被舍掉的那一项划掉显示。
 */
export function FactorChain({
  breakdown,
  className,
  size = "default"
}: {
  breakdown: AIGatewayFactorBreakdown;
  className?: string;
  size?: "default" | "sm";
}) {
  const member = Number(breakdown.member);
  const time = Number(breakdown.time);
  const best = breakdown.stacking === "best" && time < 1;
  const dropped: "member" | "time" | null = best ? (time <= member ? "member" : "time") : null;
  const items = [
    { key: "base", label: "全局", value: breakdown.base, icon: Globe2 },
    { key: "member", label: "会员", value: breakdown.member, icon: Crown },
    { key: "level", label: "等级", value: breakdown.level, icon: Gauge },
    { key: "time", label: breakdown.window?.name || "时段", value: breakdown.time, icon: Clock3, tone: breakdown.window?.tone }
  ];
  const compact = size === "sm";
  return (
    <div className={cn("flex flex-wrap items-center gap-1", className)}>
      {items.map((item, index) => {
        const off = dropped === item.key;
        const tone = item.tone ? toneMeta(item.tone) : null;
        return (
          <span key={item.key} className="flex items-center gap-1">
            {index > 0 ? <span className="text-[10px] text-muted-foreground">×</span> : null}
            <span
              className={cn(
                "inline-flex items-center gap-1 rounded-md border px-1.5 font-mono tabular-nums",
                compact ? "h-5 text-[10px]" : "h-6 text-[11px]",
                Number(item.value) === 1 ? "border-border text-muted-foreground" : "border-transparent bg-muted text-foreground",
                tone && Number(item.value) !== 1 ? tone.soft : null,
                off && "line-through opacity-50"
              )}
              title={off ? "叠加方式为「取更优惠」，这一项被舍掉" : undefined}
            >
              <item.icon className={compact ? "size-2.5" : "size-3"} />
              <span className="font-sans">{item.label}</span>
              {trimNumber(Number(item.value), 4)}
            </span>
          </span>
        );
      })}
      <span className="text-[10px] text-muted-foreground">=</span>
      <span
        className={cn(
          "inline-flex items-center gap-1 rounded-md bg-foreground px-1.5 font-mono font-semibold text-background tabular-nums",
          compact ? "h-5 text-[10px]" : "h-6 text-[11px]"
        )}
      >
        <Sparkles className={compact ? "size-2.5" : "size-3"} />
        {formatFactor(breakdown.total)}
        <span className="font-sans font-normal opacity-80">{discountLabel(breakdown.total)}</span>
      </span>
    </div>
  );
}

/** 命中时段的一行说明：「夜间半价 ×0.5，到 08:30」。 */
export function WindowHitLine({ breakdown }: { breakdown: AIGatewayFactorBreakdown }) {
  const window = breakdown.window;
  if (!window) return <span className="text-muted-foreground">标准价时段</span>;
  const tone = toneMeta(window.tone);
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      <span className={cn("size-2 rounded-full", tone.solid)} />
      <span className="font-medium">{window.name}</span>
      <span className={cn("font-mono", tone.text)}>{formatFactor(window.multiplier)}</span>
      {window.endsAt ? <span className="text-muted-foreground">到 {formatDateTimeMinute(window.endsAt)}</span> : null}
    </span>
  );
}

// ── 时段求值（与 docs/ai-billing.md「时段价格」一致）──

export const WEEKDAY_LABEL = ["", "一", "二", "三", "四", "五", "六", "日"];

/** "HH:MM" → 分钟；非法返回 null。 */
export function parseClock(value: string): number | null {
  const matched = /^(\d{2}):(\d{2})$/.exec(value.trim());
  if (!matched) return null;
  const hours = Number(matched[1]);
  const minutes = Number(matched[2]);
  if (hours > 23 || minutes > 59) return null;
  return hours * 60 + minutes;
}

export function formatClockMinutes(minutes: number) {
  const value = ((minutes % 1440) + 1440) % 1440;
  return `${String(Math.floor(value / 60)).padStart(2, "0")}:${String(value % 60).padStart(2, "0")}`;
}

/** 某时刻在指定时区的日期键（YYYY-MM-DD）与当天第几分钟。 */
export function zonedNow(at: Date, timeZone: string) {
  let parts: Intl.DateTimeFormatPart[];
  try {
    parts = new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23"
    }).formatToParts(at);
  } catch {
    parts = new Intl.DateTimeFormat("en-CA", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23"
    }).formatToParts(at);
  }
  const get = (type: string) => parts.find((part) => part.type === type)?.value ?? "00";
  return { day: `${get("year")}-${get("month")}-${get("day")}`, minute: Number(get("hour")) * 60 + Number(get("minute")) };
}

/** 日期键加减天数（纯日历运算，不受时区影响）。 */
export function addDays(day: string, delta: number) {
  const [y, m, d] = day.split("-").map(Number);
  const date = new Date(Date.UTC(y, m - 1, d + delta));
  return date.toISOString().slice(0, 10);
}

/** 1 = 周一 … 7 = 周日。 */
export function weekdayOf(day: string) {
  const [y, m, d] = day.split("-").map(Number);
  const value = new Date(Date.UTC(y, m - 1, d)).getUTCDay();
  return value === 0 ? 7 : value;
}

/** 时段求值的对象：不给模型时只看「对全部模型生效」的时段（与用户端时间轴一致）。 */
export type WindowScope = { model?: string; kind?: string };

function appliesTo(window: AIGatewayTimeWindow, scope: WindowScope) {
  const models = window.models ?? [];
  const kinds = window.kinds ?? [];
  if (!scope.model && !scope.kind) return models.length === 0 && kinds.length === 0;
  if (models.length > 0 && (!scope.model || !models.includes(scope.model))) return false;
  if (kinds.length > 0 && (!scope.kind || !kinds.includes(scope.kind))) return false;
  return true;
}

function dayAllowed(window: AIGatewayTimeWindow, startDay: string) {
  const weekdays = window.weekdays ?? [];
  if (weekdays.length > 0 && !weekdays.includes(weekdayOf(startDay))) return false;
  if (window.from && startDay < window.from) return false;
  if (window.to && startDay > window.to) return false;
  return true;
}

/** 这个时段在 day 的第 minute 分钟是否生效。半开区间；跨零点按开始那一天判星期与活动日期。 */
export function windowCovers(window: AIGatewayTimeWindow, day: string, minute: number) {
  const start = parseClock(window.start);
  const end = parseClock(window.end);
  if (start === null || end === null) return false;
  if (start === end) return dayAllowed(window, day);
  if (start < end) return minute >= start && minute < end && dayAllowed(window, day);
  if (minute >= start) return dayAllowed(window, day);
  if (minute < end) return dayAllowed(window, addDays(day, -1));
  return false;
}

/** 列表里第一个命中的时段；都不命中返回 null（标准价）。 */
export function resolveWindow(
  windows: AIGatewayTimeWindow[],
  day: string,
  minute: number,
  scope: WindowScope = {}
): AIGatewayTimeWindow | null {
  for (const window of windows) {
    if (!window.enabled || !appliesTo(window, scope)) continue;
    if (windowCovers(window, day, minute)) return window;
  }
  return null;
}

export type DaySegment = { start: number; end: number; window: AIGatewayTimeWindow | null };

/** 一天按时段切成若干段（相邻同一时段已合并），start/end 为当天分钟。 */
export function daySegments(windows: AIGatewayTimeWindow[], day: string, scope: WindowScope = {}): DaySegment[] {
  const marks = new Set<number>([0, 1440]);
  for (const window of windows) {
    const start = parseClock(window.start);
    const end = parseClock(window.end);
    if (start !== null) marks.add(start);
    if (end !== null) marks.add(end);
  }
  const points = [...marks].sort((a, b) => a - b);
  const segments: DaySegment[] = [];
  for (let index = 0; index < points.length - 1; index += 1) {
    const start = points[index];
    const end = points[index + 1];
    if (end <= start) continue;
    const window = resolveWindow(windows, day, start, scope);
    const last = segments[segments.length - 1];
    if (last && last.window === window) last.end = end;
    else segments.push({ start, end, window });
  }
  return segments;
}

/** 每 interval 毫秒刷新一次的「现在」。 */
export function useNow(interval = 30_000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), interval);
    return () => window.clearInterval(timer);
  }, [interval]);
  return now;
}

/** 生成时段编号：与后端规则一致（1–32 位字母数字、-、_）。 */
export function newWindowId() {
  return `w${Math.random().toString(36).slice(2, 10)}`;
}
