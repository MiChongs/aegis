"use client";

import { useMemo } from "react";
import {
  WEEKDAY_LABEL,
  addDays,
  daySegments,
  formatClockMinutes,
  formatFactor,
  resolveWindow,
  toneMeta,
  toneStrength,
  weekdayOf,
  type DaySegment,
  type WindowScope
} from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import type { AIGatewayTimeWindow } from "@/lib/api/ai-gateway";
import { cn } from "@/lib/utils";

/**
 * 时段价格的两张图：
 * - 24 小时环形时钟：选中那一天的各段，按色调着色、按倍率深浅，指针指向「现在」；
 * - 7 天 × 48 个半小时的热力图：从今天起一周，今天一行标出现在的位置，点哪一行时钟就切到哪一天。
 *
 * 都由 daySegments / resolveWindow 前端自算，规则与后端一致（半开区间、跨零点按开始日、首个命中优先）。
 */

const SIZE = 260;
const CENTER = SIZE / 2;
const OUTER = 116;
const INNER = 82;

function polar(radius: number, minute: number) {
  const angle = (minute / 1440) * Math.PI * 2 - Math.PI / 2;
  return { x: CENTER + radius * Math.cos(angle), y: CENTER + radius * Math.sin(angle) };
}

function arcPath(start: number, end: number, outer = OUTER, inner = INNER): string {
  // 整圈拆成两半画，单条弧画不出 360°
  if (end - start >= 1440) return `${arcPath(0, 720, outer, inner)} ${arcPath(720, 1440, outer, inner)}`;
  const large = end - start > 720 ? 1 : 0;
  const a = polar(outer, start);
  const b = polar(outer, end);
  const c = polar(inner, end);
  const d = polar(inner, start);
  return [
    `M ${a.x} ${a.y}`,
    `A ${outer} ${outer} 0 ${large} 1 ${b.x} ${b.y}`,
    `L ${c.x} ${c.y}`,
    `A ${inner} ${inner} 0 ${large} 0 ${d.x} ${d.y}`,
    "Z"
  ].join(" ");
}

function dayLabel(day: string, today: string) {
  const weekday = `周${WEEKDAY_LABEL[weekdayOf(day)]}`;
  if (day === today) return `今天 · ${weekday}`;
  if (day === addDays(today, 1)) return `明天 · ${weekday}`;
  return `${day.slice(5)} · ${weekday}`;
}

export function PricingClock({
  windows,
  day,
  today,
  nowMinute,
  scope
}: {
  windows: AIGatewayTimeWindow[];
  day: string;
  today: string;
  nowMinute: number;
  scope: WindowScope;
}) {
  const segments = useMemo(() => daySegments(windows, day, scope), [windows, day, scope]);
  const isToday = day === today;
  const current = isToday ? resolveWindow(windows, day, nowMinute, scope) : null;
  const currentSegment = isToday ? segments.find((item) => nowMinute >= item.start && nowMinute < item.end) : null;
  const needle = polar(OUTER + 6, nowMinute);
  const needleBase = polar(INNER - 10, nowMinute);

  return (
    <div className="flex flex-col items-center gap-2">
      <svg viewBox={`0 0 ${SIZE} ${SIZE}`} className="w-full max-w-[260px]" role="img" aria-label="24 小时价格时钟">
        <circle cx={CENTER} cy={CENTER} r={(OUTER + INNER) / 2} className="fill-none stroke-muted" strokeWidth={OUTER - INNER} />
        {segments.map((segment) => (
          <ClockSegment key={`${segment.start}-${segment.end}`} segment={segment} />
        ))}
        {Array.from({ length: 24 }, (_, hour) => {
          const outer = polar(INNER - 2, hour * 60);
          const inner = polar(INNER - (hour % 6 === 0 ? 9 : 5), hour * 60);
          return (
            <line
              key={hour}
              x1={outer.x}
              y1={outer.y}
              x2={inner.x}
              y2={inner.y}
              className={hour % 6 === 0 ? "stroke-foreground/60" : "stroke-muted-foreground/40"}
              strokeWidth={hour % 6 === 0 ? 1.5 : 1}
            />
          );
        })}
        {[0, 3, 6, 9, 12, 15, 18, 21].map((hour) => {
          const point = polar(INNER - 19, hour * 60);
          return (
            <text
              key={hour}
              x={point.x}
              y={point.y}
              textAnchor="middle"
              dominantBaseline="central"
              className={cn("fill-muted-foreground font-mono", hour % 6 === 0 ? "text-[10px] font-semibold" : "text-[8px]")}
            >
              {hour}
            </text>
          );
        })}
        {isToday ? (
          <g>
            <line x1={needleBase.x} y1={needleBase.y} x2={needle.x} y2={needle.y} className="stroke-foreground" strokeWidth={2} strokeLinecap="round" />
            <circle cx={needle.x} cy={needle.y} r={4} className="fill-foreground stroke-background" strokeWidth={2} />
          </g>
        ) : null}
        <text x={CENTER} y={CENTER - 22} textAnchor="middle" className="fill-muted-foreground text-[10px]">
          {isToday ? `现在 ${formatClockMinutes(nowMinute)}` : dayLabel(day, today)}
        </text>
        <text x={CENTER} y={CENTER + 4} textAnchor="middle" className="fill-foreground font-mono text-[24px] font-bold">
          {isToday ? formatFactor(current?.multiplier ?? "1") : `${segments.filter((item) => item.window).length} 段`}
        </text>
        <text x={CENTER} y={CENTER + 24} textAnchor="middle" className="fill-muted-foreground text-[10px]">
          {isToday
            ? `${current?.name ?? "标准价"}${currentSegment ? ` · 到 ${formatClockMinutes(currentSegment.end)}` : ""}`
            : "当天的价格时段"}
        </text>
      </svg>
    </div>
  );
}

function ClockSegment({ segment }: { segment: DaySegment }) {
  if (!segment.window) return null;
  const multiplier = Number(segment.window.multiplier);
  const tone = toneMeta(segment.window.tone);
  return (
    <path d={arcPath(segment.start, segment.end)} className={cn(tone.fill, "stroke-background")} strokeWidth={1.5} fillOpacity={toneStrength(multiplier)}>
      <title>
        {`${segment.window.name} ${formatClockMinutes(segment.start)}–${formatClockMinutes(segment.end)} ${formatFactor(segment.window.multiplier)}`}
      </title>
    </path>
  );
}

/** 一周里各色调累计多少小时（以 7 天为窗）。 */
export function toneHours(windows: AIGatewayTimeWindow[], today: string, scope: WindowScope) {
  const totals = new Map<string, number>();
  for (let offset = 0; offset < 7; offset += 1) {
    for (const segment of daySegments(windows, addDays(today, offset), scope)) {
      const key = segment.window ? segment.window.tone : "standard";
      totals.set(key, (totals.get(key) ?? 0) + (segment.end - segment.start) / 60);
    }
  }
  return totals;
}

export function WeekHeatmap({
  windows,
  today,
  nowMinute,
  scope,
  selectedDay,
  onSelectDay
}: {
  windows: AIGatewayTimeWindow[];
  today: string;
  nowMinute: number;
  scope: WindowScope;
  selectedDay: string;
  onSelectDay: (day: string) => void;
}) {
  const days = useMemo(() => Array.from({ length: 7 }, (_, offset) => addDays(today, offset)), [today]);
  const rows = useMemo(
    () =>
      days.map((day) => ({
        day,
        cells: Array.from({ length: 48 }, (_, slot) => resolveWindow(windows, day, slot * 30, scope))
      })),
    [days, windows, scope]
  );

  return (
    <div className="min-w-0 space-y-1.5">
      <div className="grid grid-cols-[4.75rem_minmax(0,1fr)] items-end gap-2">
        <span />
        <div className="relative h-3.5">
          {[0, 3, 6, 9, 12, 15, 18, 21, 24].map((hour) => (
            <span
              key={hour}
              className="absolute -translate-x-1/2 font-mono text-[9px] text-muted-foreground"
              style={{ left: `${(hour / 24) * 100}%` }}
            >
              {hour}
            </span>
          ))}
        </div>
      </div>
      {rows.map((row) => {
        const isToday = row.day === today;
        const selected = row.day === selectedDay;
        return (
          <button
            key={row.day}
            type="button"
            onClick={() => onSelectDay(row.day)}
            className={cn(
              "grid w-full grid-cols-[4.75rem_minmax(0,1fr)] items-center gap-2 rounded-md text-left transition-colors",
              selected ? "bg-muted/70" : "hover:bg-muted/40"
            )}
          >
            <span className={cn("truncate pl-1 text-[11px]", isToday ? "font-semibold text-foreground" : "text-muted-foreground")}>
              {dayLabel(row.day, today)}
            </span>
            <span className="relative flex h-6 gap-px overflow-hidden rounded-md py-0.5">
              {row.cells.map((window, slot) => {
                const tone = window ? toneMeta(window.tone) : null;
                return (
                  <span
                    key={slot}
                    className={cn(
                      "h-full min-w-0 flex-1",
                      tone ? tone.solid : "bg-muted",
                      slot % 2 === 0 ? "rounded-l-[2px]" : "mr-px rounded-r-[2px]"
                    )}
                    style={tone ? { opacity: toneStrength(Number(window?.multiplier)) } : undefined}
                    title={`${row.day.slice(5)} 周${WEEKDAY_LABEL[weekdayOf(row.day)]} ${formatClockMinutes(slot * 30)}–${formatClockMinutes(slot * 30 + 30)} · ${window ? `${window.name} ${formatFactor(window.multiplier)}` : "标准价 ×1"}`}
                  />
                );
              })}
              {isToday ? (
                <span
                  className="pointer-events-none absolute inset-y-0 w-0.5 -translate-x-1/2 rounded-full bg-foreground shadow-[0_0_0_2px_var(--background)]"
                  style={{ left: `${(nowMinute / 1440) * 100}%` }}
                />
              ) : null}
            </span>
          </button>
        );
      })}
    </div>
  );
}
