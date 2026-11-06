"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Activity, LogIn, TrendingDown, TrendingUp, UserPlus, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAdminDashboardQuery } from "@/lib/admin-hooks";
import type { AdminDashboardDay } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { CardEmpty, changeRatio, formatCount, formatRatio, longDay, OverviewCard, shortDay } from "./overview-kit";

type SeriesKey = "logins" | "registrations" | "activeUsers";

const SERIES: Array<{
  key: SeriesKey;
  label: string;
  /** 汇总口径：次数按区间求和，活跃人数取日均（逐日去重人数相加没有意义） */
  summary: "sum" | "avg";
  summaryLabel: string;
  icon: typeof LogIn;
  /** 汇总格选中时顶边的颜色，与图表配色一致（格子在 ChartContainer 外，取不到 --color-*） */
  accent: string;
}> = [
  { key: "logins", label: "登录次数", summary: "sum", summaryLabel: "合计", icon: LogIn, accent: "bg-blue-600 dark:bg-blue-400" },
  { key: "registrations", label: "新增用户", summary: "sum", summaryLabel: "合计", icon: UserPlus, accent: "bg-emerald-600 dark:bg-emerald-400" },
  { key: "activeUsers", label: "活跃用户", summary: "avg", summaryLabel: "日均", icon: Users, accent: "bg-violet-600 dark:bg-violet-400" }
];

const CHART_CONFIG = {
  logins: { label: "登录次数", theme: { light: "#2563eb", dark: "#60a5fa" } },
  registrations: { label: "新增用户", theme: { light: "#059669", dark: "#34d399" } },
  activeUsers: { label: "活跃用户", theme: { light: "#7c3aed", dark: "#a78bfa" } }
} satisfies ChartConfig;

const RANGES = [
  { value: "7", label: "7 天" },
  { value: "14", label: "14 天" }
] as const;

function summarize(days: AdminDashboardDay[], key: SeriesKey, mode: "sum" | "avg") {
  if (days.length === 0) return 0;
  const total = days.reduce((sum, day) => sum + day[key], 0);
  return mode === "avg" ? Math.round(total / days.length) : total;
}

/**
 * 用户活跃趋势：三项指标做成可点选的汇总格，下面一张面积图只画选中的那一项。
 * 选 7 天时与再往前 7 天比较给出环比；14 天已是接口给的全部窗口，不再比较。
 */
export function ActivityTrend({ className }: { className?: string }) {
  const { data, isLoading } = useAdminDashboardQuery();
  const [active, setActive] = useState<SeriesKey>("logins");
  const [range, setRange] = useState<"7" | "14">("7");
  const trend = useMemo(() => data?.trend ?? [], [data?.trend]);

  const days = Number(range);
  const current = trend.slice(-days);
  const previous = range === "7" ? trend.slice(-14, -7) : [];
  const empty = !isLoading && trend.every((day) => day.logins === 0 && day.registrations === 0 && day.activeUsers === 0);

  const rows = current.map((day) => ({ ...day, label: shortDay(day.date) }));

  return (
    <OverviewCard
      icon={Activity}
      tone="sky"
      title="用户活跃趋势"
      description="全部应用"
      className={className}
      action={
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={range}
          onValueChange={(value) => value && setRange(value as "7" | "14")}
          aria-label="统计范围"
        >
          {RANGES.map((item) => (
            <ToggleGroupItem key={item.value} value={item.value} className="px-2.5 text-xs">
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      }
    >
      <div role="tablist" aria-label="趋势指标" className="grid grid-cols-3 gap-2">
        {SERIES.map((series) => {
          const value = summarize(current, series.key, series.summary);
          const ratio = previous.length > 0 ? changeRatio(value, summarize(previous, series.key, series.summary)) : undefined;
          const selected = active === series.key;
          return (
            <button
              key={series.key}
              type="button"
              role="tab"
              aria-selected={selected}
              onClick={() => setActive(series.key)}
              className={cn(
                "group/tile relative flex min-w-0 flex-col gap-1.5 overflow-hidden rounded-lg border px-2.5 py-2 text-left transition-colors sm:px-3 sm:py-2.5",
                "focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                selected ? "border-foreground/15 bg-muted/60" : "hover:bg-muted/40"
              )}
            >
              <span
                aria-hidden
                className={cn("absolute inset-x-0 top-0 h-0.5 transition-opacity", series.accent, selected ? "opacity-100" : "opacity-0")}
              />
              <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
                <series.icon className="size-3.5 shrink-0" />
                <span className="truncate">{series.label}</span>
              </span>
              {isLoading ? (
                <Skeleton className="h-6 w-14" />
              ) : (
                <span className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-1">
                  <span className="font-data text-lg leading-6 font-semibold tabular-nums sm:text-xl">{formatCount(value)}</span>
                  <span className="text-[10px] text-muted-foreground">{series.summaryLabel}</span>
                </span>
              )}
              {!isLoading && ratio !== undefined ? <RatioBadge ratio={ratio} /> : null}
            </button>
          );
        })}
      </div>

      <div className="mt-4 min-h-[200px] flex-1 sm:min-h-[220px]">
        {isLoading ? (
          <Skeleton className="h-[200px] w-full rounded-lg sm:h-[220px]" />
        ) : empty ? (
          <CardEmpty icon={Activity} title="暂无用户活动" description="应用用户登录或注册后在此显示" />
        ) : (
          <ChartContainer config={CHART_CONFIG} className="aspect-auto h-[200px] w-full sm:h-[220px]">
            <AreaChart data={rows} margin={{ left: 0, right: 4, top: 8, bottom: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis
                dataKey="label"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                minTickGap={16}
                fontSize={11}
              />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} width={36} fontSize={11} tickFormatter={(value: number) => formatCount(value)} />
              <ChartTooltip
                cursor={{ strokeDasharray: "3 3" }}
                content={
                  <ChartTooltipContent
                    indicator="line"
                    labelFormatter={(_, payload) => {
                      const date = (payload?.[0]?.payload as { date?: string } | undefined)?.date;
                      return date ? longDay(date) : "";
                    }}
                  />
                }
              />
              <Area
                key={active}
                dataKey={active}
                type="monotone"
                stroke={`var(--color-${active})`}
                strokeWidth={2}
                fill={`var(--color-${active})`}
                fillOpacity={0.12}
                activeDot={{ r: 4 }}
              />
            </AreaChart>
          </ChartContainer>
        )}
      </div>
    </OverviewCard>
  );
}

function RatioBadge({ ratio }: { ratio: number | null }) {
  if (ratio === null) {
    return (
      <Badge variant="info" size="sm" className="w-fit">
        新增
      </Badge>
    );
  }
  const flat = Math.abs(ratio) < 0.0005;
  const up = ratio > 0;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={flat ? "secondary" : up ? "success" : "danger"} size="sm" className="w-fit gap-0.5 font-data tabular-nums">
          {flat ? null : up ? <TrendingUp className="size-3" /> : <TrendingDown className="size-3" />}
          {flat ? "持平" : formatRatio(ratio)}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>较前 7 天</TooltipContent>
    </Tooltip>
  );
}
