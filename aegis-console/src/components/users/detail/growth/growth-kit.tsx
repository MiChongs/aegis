"use client";

/**
 * 「奖惩」页签的自绘部件（docs/growth-control.md）。
 *
 * 倍率在界面上一律写成 ×N / ÷N / 冻结，背后是分数（num/den）。表盘按**对数刻度**画：
 * ÷10 与 ×10 离中心（×1）一样远，处罚与奖励在视觉上对称，不会因为 ×5 比 ÷5「数字大」就显得更极端。
 */

import * as React from "react";
import { Ban, Flame, Minus, Snowflake, TrendingDown, TrendingUp } from "lucide-react";
import { factorLabel, factorValue, type GrowthEffect, type GrowthRounding, type GrowthRuleStatus } from "@/lib/api/growth";
import { cn } from "@/lib/utils";
import type { Tone } from "../kit";

// ── 效果与状态 ──────────────────────────

export const EFFECT_META: Record<GrowthEffect, { label: string; tone: Tone; icon: React.ComponentType<{ className?: string }> }> = {
  boost: { label: "奖励", tone: "success", icon: TrendingUp },
  penalty: { label: "处罚", tone: "warning", icon: TrendingDown },
  freeze: { label: "冻结", tone: "danger", icon: Snowflake },
  neutral: { label: "中性", tone: "neutral", icon: Minus }
};

export const STATUS_META: Record<GrowthRuleStatus, { label: string; variant: "success" | "info" | "secondary" | "warning" | "outline" }> = {
  active: { label: "生效中", variant: "success" },
  scheduled: { label: "待生效", variant: "info" },
  paused: { label: "已暂停", variant: "warning" },
  expired: { label: "已到期", variant: "secondary" },
  revoked: { label: "已撤销", variant: "outline" }
};

export const ROUNDING_LABEL: Record<GrowthRounding, string> = {
  floor: "向下取整",
  round: "四舍五入",
  ceil: "向上取整",
  random: "按概率进位"
};

/** 效果的色条 / 底色（规则卡左侧的竖条、表盘指针）。 */
export const EFFECT_SURFACE: Record<GrowthEffect, string> = {
  boost: "bg-emerald-500",
  penalty: "bg-amber-500",
  freeze: "bg-red-500",
  neutral: "bg-muted-foreground/40"
};

export function effectOf(num: number, den: number, dailyCap = 0): GrowthEffect {
  if (num === 0) return "freeze";
  if (num < den) return "penalty";
  if (num > den) return "boost";
  return dailyCap > 0 ? "penalty" : "neutral";
}

/** 倍率的大号文字，颜色随效果。 */
export function FactorText({
  num,
  den,
  dailyCap = 0,
  className
}: {
  num: number;
  den: number;
  dailyCap?: number;
  className?: string;
}) {
  const effect = effectOf(num, den, dailyCap);
  return (
    <span
      className={cn(
        "font-semibold tracking-tight tabular-nums",
        effect === "boost" && "text-emerald-600 dark:text-emerald-400",
        effect === "penalty" && "text-amber-600 dark:text-amber-400",
        effect === "freeze" && "text-red-600 dark:text-red-400",
        className
      )}
    >
      {factorLabel(num, den)}
    </span>
  );
}

// ── 分数 ────────────────────────────────

function gcd(a: number, b: number): number {
  while (b) [a, b] = [b, a % b];
  return Math.abs(a);
}

/**
 * 把「×1.5」「÷3」这样的输入换成分数：乘号模式下小数最多 3 位（1.5 → 3/2），除号模式下取倒数。
 * 分子分母都要落在 [0, limit]，越界返回 null。
 */
export function fractionFrom(mode: "mul" | "div", raw: string, limit = 1000): { num: number; den: number } | null {
  const text = raw.trim();
  if (!/^\d+(\.\d{1,3})?$/.test(text)) return null;
  const [intPart, fracPart = ""] = text.split(".");
  const scale = 10 ** fracPart.length;
  let num = Number(intPart) * scale + Number(fracPart || 0);
  let den = scale;
  if (mode === "div") {
    if (num === 0) return null;
    [num, den] = [den, num];
  }
  if (num === 0) return { num: 0, den: 1 };
  const g = gcd(num, den);
  num /= g;
  den /= g;
  if (num > limit || den > limit || den < 1) return null;
  return { num, den };
}

/** 分数还原成编辑框里的「模式 + 数值」。 */
export function fractionToInput(num: number, den: number): { mode: "mul" | "div"; value: string } {
  if (num === 0) return { mode: "mul", value: "0" };
  if (num < den && num === 1) return { mode: "div", value: String(den) };
  if (num < den) return { mode: "div", value: String(Number((den / num).toFixed(3))) };
  return { mode: "mul", value: String(Number((num / den).toFixed(3))) };
}

// ── 表盘 ────────────────────────────────

const DIAL_RANGE = Math.log10(10); // 两端是 ÷10 与 ×10，再往外贴边

/** 倍率映射到 [-1, 1]：对数刻度，冻结贴最左。 */
function dialPosition(num: number, den: number) {
  if (num === 0) return -1;
  const v = Math.log10(factorValue(num, den)) / DIAL_RANGE;
  return Math.max(-1, Math.min(1, v));
}

function polar(cx: number, cy: number, r: number, t: number) {
  // t ∈ [-1, 1] → 角度 180°（左）… 0°（右）
  const angle = Math.PI * (1 - (t + 1) / 2);
  return { x: cx + r * Math.cos(angle), y: cy - r * Math.sin(angle) };
}

function arc(cx: number, cy: number, r: number, from: number, to: number) {
  const a = polar(cx, cy, r, from);
  const b = polar(cx, cy, r, to);
  return `M ${a.x.toFixed(2)} ${a.y.toFixed(2)} A ${r} ${r} 0 0 1 ${b.x.toFixed(2)} ${b.y.toFixed(2)}`;
}

/**
 * 半圆倍率表盘：左半处罚（琥珀）、右半奖励（翠绿）、最左端一小段冻结（红）；指针指向综合倍率。
 * 纯 SVG，颜色走 currentColor 与 Tailwind 语义色，深浅色主题都清楚。
 */
export function RateDial({ num, den, dailyCap = 0, size = 168 }: { num: number; den: number; dailyCap?: number; size?: number }) {
  const width = size;
  const height = size * 0.6;
  const cx = width / 2;
  const cy = height - 6;
  const r = width / 2 - 12;
  const pos = dialPosition(num, den);
  const tip = polar(cx, cy, r - 14, pos);
  const effect = effectOf(num, den, dailyCap);
  const ticks = [-1, -0.699, -0.301, 0, 0.301, 0.699, 1];
  const tickLabels: Record<string, string> = { "-1": "÷10", "-0.301": "÷2", "0": "×1", "0.301": "×2", "1": "×10" };
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`综合倍率 ${factorLabel(num, den)}`}>
      <path d={arc(cx, cy, r, -1, 1)} className="stroke-muted" strokeWidth={10} fill="none" strokeLinecap="round" />
      <path d={arc(cx, cy, r, -1, -0.93)} className="stroke-red-500/70" strokeWidth={10} fill="none" strokeLinecap="round" />
      <path d={arc(cx, cy, r, -0.9, -0.03)} className="stroke-amber-500/55" strokeWidth={10} fill="none" />
      <path d={arc(cx, cy, r, 0.03, 0.97)} className="stroke-emerald-500/55" strokeWidth={10} fill="none" />
      {ticks.map((t) => {
        const a = polar(cx, cy, r + 7, t);
        const b = polar(cx, cy, r + 2, t);
        const label = tickLabels[String(t)];
        const l = polar(cx, cy, r - 20, t);
        return (
          <g key={t}>
            <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} className="stroke-muted-foreground/50" strokeWidth={1} />
            {label ? (
              <text x={l.x} y={l.y + 3} textAnchor="middle" className="fill-muted-foreground text-[9px]">
                {label}
              </text>
            ) : null}
          </g>
        );
      })}
      <line
        x1={cx}
        y1={cy}
        x2={tip.x}
        y2={tip.y}
        strokeWidth={3}
        strokeLinecap="round"
        className={cn(
          "transition-all duration-500",
          effect === "boost" && "stroke-emerald-500",
          effect === "penalty" && "stroke-amber-500",
          effect === "freeze" && "stroke-red-500",
          effect === "neutral" && "stroke-foreground/60"
        )}
      />
      <circle cx={cx} cy={cy} r={5} className="fill-foreground" />
    </svg>
  );
}

// ── 每日上限 ─────────────────────────────

/** 今日已入账 / 每日上限的进度条；到顶变红。 */
export function CapMeter({ used, cap, className }: { used: number; cap: number; className?: string }) {
  if (cap <= 0) {
    return <div className={cn("text-xs text-muted-foreground", className)}>不限每日上限</div>;
  }
  const ratio = Math.min(used / cap, 1);
  const full = used >= cap;
  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">今日 {used.toLocaleString("zh-CN")} / {cap.toLocaleString("zh-CN")}</span>
        <span className={cn("font-medium tabular-nums", full ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
          {full ? "已到上限" : `${Math.round(ratio * 100)}%`}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          className={cn("h-full rounded-full transition-[width] duration-500", full ? "bg-red-500" : ratio > 0.8 ? "bg-amber-500" : "bg-primary")}
          style={{ width: `${ratio * 100}%` }}
        />
      </div>
    </div>
  );
}

/** 小号状态点 + 文字（规则卡标题行）。 */
export function EffectPill({ effect }: { effect: GrowthEffect }) {
  const meta = EFFECT_META[effect];
  const Icon = effect === "freeze" ? Ban : effect === "boost" ? Flame : meta.icon;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium",
        effect === "boost" && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
        effect === "penalty" && "bg-amber-500/10 text-amber-700 dark:text-amber-300",
        effect === "freeze" && "bg-red-500/10 text-red-700 dark:text-red-300",
        effect === "neutral" && "bg-muted text-muted-foreground"
      )}
    >
      <Icon className="size-3" />
      {meta.label}
    </span>
  );
}

/** 剩余时间的细进度条（有开始与结束时间时）。 */
export function WindowBar({ startsAt, endsAt, now }: { startsAt?: string | null; endsAt?: string | null; now: number }) {
  if (!endsAt) return null;
  const end = new Date(endsAt).getTime();
  const start = startsAt ? new Date(startsAt).getTime() : null;
  if (!start || end <= start) return null;
  const ratio = Math.min(Math.max((now - start) / (end - start), 0), 1);
  return (
    <div className="h-1 overflow-hidden rounded-full bg-muted" aria-hidden>
      <div className="h-full rounded-full bg-primary/60" style={{ width: `${ratio * 100}%` }} />
    </div>
  );
}

/** 剩余时长的文字：3 天 4 小时 / 25 分钟。 */
export function remainingText(target: string | null | undefined, now: number) {
  if (!target) return "";
  const ms = new Date(target).getTime() - now;
  if (ms <= 0) return "";
  const minutes = Math.floor(ms / 60_000);
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  if (days > 0) return hours > 0 ? `${days} 天 ${hours} 小时` : `${days} 天`;
  if (hours > 0) return `${hours} 小时 ${minutes % 60} 分钟`;
  return `${Math.max(minutes, 1)} 分钟`;
}
