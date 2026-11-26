"use client";

import { useMemo } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { ChartArea, ShieldCheck } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Spinner } from "@/components/ui/spinner";
import { CardEmpty, OverviewCard } from "@/components/overview/overview-kit";
import { SEVERITY_META, SeverityDot } from "@/components/firewall/firewall-kit";
import type { FirewallStatsV2, FirewallTimePoint } from "@/lib/api/firewall";
import { formatClock, formatShortDateTime, formatWith } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { GRANULARITY_LABEL, GRANULARITY_STEP, type FirewallGranularity, type FirewallRangeKey } from "./firewall-range";

const SEVERITIES = ["critical", "high", "medium", "low"] as const;
type SeverityKey = (typeof SEVERITIES)[number];

/** 与 firewall-kit 的 SeverityDot 同色（red-500 / orange-500 / amber-400 / slate-400） */
const CONFIG = {
  critical: { label: "严重", theme: { light: "#ef4444", dark: "#f87171" } },
  high: { label: "高危", theme: { light: "#f97316", dark: "#fb923c" } },
  medium: { label: "中危", theme: { light: "#fbbf24", dark: "#fcd34d" } },
  low: { label: "低危", theme: { light: "#94a3b8", dark: "#64748b" } }
} satisfies ChartConfig;

type Row = { t: number } & Record<SeverityKey, number>;

/**
 * 服务端只返回有拦截的桶，空桶要前端补 0，否则面积图会把两次攻击之间连成一条斜坡。
 * 按天的桶边界取决于数据库会话时区，所以对齐用返回点自身的偏移，没有点时退回 UTC。
 */
function fillSeries(points: FirewallTimePoint[], startMs: number, endMs: number, granularity: FirewallGranularity): Row[] {
  const step = GRANULARITY_STEP[granularity];
  const byTime = new Map<number, FirewallTimePoint>();
  for (const point of points) {
    const t = Date.parse(point.time);
    if (!Number.isNaN(t)) byTime.set(t, point);
  }
  const first = byTime.keys().next();
  const offset = first.done ? 0 : ((first.value % step) + step) % step;
  const rows: Row[] = [];
  let t = Math.floor((startMs - offset) / step) * step + offset;
  while (t <= endMs && rows.length < 2000) {
    const point = byTime.get(t);
    rows.push({
      t,
      critical: point?.critical ?? 0,
      high: point?.high ?? 0,
      medium: point?.medium ?? 0,
      low: point?.low ?? 0
    });
    t += step;
  }
  return rows;
}

function tickLabel(t: number, granularity: FirewallGranularity, range: FirewallRangeKey) {
  if (granularity === "day") return formatWith(t, { month: "numeric", day: "numeric" });
  if (granularity === "hour" && range !== "24h") return formatWith(t, { month: "numeric", day: "numeric", hour: "2-digit", hourCycle: "h23" });
  return formatClock(t);
}

function fullLabel(t: number, granularity: FirewallGranularity) {
  if (granularity === "day") return formatWith(t, { year: "numeric", month: "numeric", day: "numeric", weekday: "short" });
  return formatShortDateTime(t);
}

export function SeverityTrendCard({
  stats,
  startMs,
  endMs,
  granularity,
  range,
  refreshing,
  className
}: {
  stats: FirewallStatsV2;
  startMs: number;
  endMs: number;
  granularity: FirewallGranularity;
  range: FirewallRangeKey;
  refreshing?: boolean;
  className?: string;
}) {
  const rows = useMemo(() => fillSeries(stats.timeSeries ?? [], startMs, endMs, granularity), [stats.timeSeries, startMs, endMs, granularity]);
  const counts = useMemo(() => {
    const map = new Map((stats.severityCounts ?? []).map((item) => [item.key, item.count] as const));
    return SEVERITIES.map((key) => ({ key, count: map.get(key) ?? 0 }));
  }, [stats.severityCounts]);
  const peak = useMemo(() => {
    let best: Row | null = null;
    let bestTotal = 0;
    for (const row of rows) {
      const total = row.critical + row.high + row.medium + row.low;
      if (total > bestTotal) {
        bestTotal = total;
        best = row;
      }
    }
    return best ? { t: best.t, total: bestTotal } : null;
  }, [rows]);

  return (
    <OverviewCard
      icon={ChartArea}
      tone="rose"
      title="拦截趋势"
      description={`${GRANULARITY_LABEL[granularity]} · 按严重性堆叠`}
      className={className}
      action={refreshing ? <Spinner className="size-3.5 text-muted-foreground" /> : null}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
        {counts.map((item) => (
          <span key={item.key} className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <SeverityDot severity={item.key} />
            {SEVERITY_META[item.key]?.label}
            <span className={cn("font-data font-medium tabular-nums", item.count > 0 ? "text-foreground" : undefined)}>
              {item.count.toLocaleString("zh-CN")}
            </span>
          </span>
        ))}
        {peak ? (
          <span className="ml-auto text-[11px] text-muted-foreground tabular-nums">
            峰值 {fullLabel(peak.t, granularity)} · <span className="font-medium text-foreground">{peak.total.toLocaleString("zh-CN")}</span>
          </span>
        ) : null}
      </div>
      <div className="mt-3 min-h-[240px] flex-1">
        {stats.totalBlocked === 0 || !peak ? (
          <CardEmpty icon={ShieldCheck} title="暂无拦截" />
        ) : (
          <ChartContainer config={CONFIG} className="aspect-auto h-[240px] w-full">
            <AreaChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis
                dataKey="t"
                type="number"
                scale="time"
                domain={["dataMin", "dataMax"]}
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={40}
                fontSize={11}
                tickFormatter={(value: number) => tickLabel(value, granularity, range)}
              />
              <YAxis tickLine={false} axisLine={false} width={40} fontSize={11} allowDecimals={false} />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    indicator="dot"
                    labelFormatter={(_, payload) => {
                      const t = (payload?.[0]?.payload as Row | undefined)?.t;
                      return typeof t === "number" ? fullLabel(t, granularity) : "";
                    }}
                  />
                }
              />
              {SEVERITIES.map((key) => (
                <Area
                  key={key}
                  dataKey={key}
                  type="monotone"
                  stackId="severity"
                  stroke={`var(--color-${key})`}
                  fill={`var(--color-${key})`}
                  fillOpacity={0.3}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                />
              ))}
            </AreaChart>
          </ChartContainer>
        )}
      </div>
    </OverviewCard>
  );
}
