"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { CircleCheck, CircleX, Eye, MousePointerClick, RefreshCw, Settings2, TriangleAlert, Users } from "lucide-react";
import { formatInterval } from "@/components/apps/ad-policy/ad-policy-shared";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { buildChartConfig, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAdPolicyQuery, useAdPolicyStatsQuery } from "@/lib/ad-policy-hooks";

/** 开屏与选择变更分两张图：一个是每天上千的展示，一个是零星的选择，放一张图里后者会贴着横轴。 */
const SPLASH_SERIES: Array<{ key: "shown" | "clicked" | "failed"; label: string; color: ThemedColor }> = [
  { key: "shown", label: "展示", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "clicked", label: "点击", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "failed", label: "失败", color: { light: "#f43f5e", dark: "#fb7185" } }
];

const CONSENT_SERIES: Array<{ key: "accepted" | "declined"; label: string; color: ThemedColor }> = [
  { key: "accepted", label: "同意", color: { light: "#10b981", dark: "#34d399" } },
  { key: "declined", label: "拒绝", color: { light: "#f59e0b", dark: "#fbbf24" } }
];

const RANGES = [7, 14, 30] as const;

export function AdPolicyOverviewPanel({ appKey, onOpenConfig }: { appKey: string; onOpenConfig: () => void }) {
  const [days, setDays] = useState<number>(14);
  const policyQuery = useAdPolicyQuery(appKey);
  const statsQuery = useAdPolicyStatsQuery(appKey, days);

  const policy = policyQuery.data;
  const stats = statsQuery.data;
  const splashConfig = useMemo(() => buildChartConfig(SPLASH_SERIES), []);
  const consentConfig = useMemo(() => buildChartConfig(CONSENT_SERIES), []);
  const rows = useMemo(
    () => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5) })),
    [stats]
  );
  const splashEmpty = rows.every((row) => row.shown === 0 && row.failed === 0);
  const consentEmpty = rows.every((row) => row.accepted === 0 && row.declined === 0);

  if (policyQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  const rangePicker = (
    <ToggleGroup
      type="single"
      size="sm"
      value={String(days)}
      onValueChange={(value) => value && setDays(Number(value))}
      className="shrink-0"
    >
      {RANGES.map((range) => (
        <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
          {range} 天
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-4">
          <StatusDot
            active={Boolean(policy?.enabled)}
            labelActive="拒绝时仅提供基础服务"
            labelInactive={policy?.configured ? "未要求同意" : "未配置，客户端使用内置策略"}
          />
          {policy?.configured ? (
            <span className="text-xs text-muted-foreground">
              条款 v{policy.consentVersion} · 基础服务 {policy.basicTools.length} 项 · 开屏
              {policy.splash.enabled
                ? ` 间隔 ${formatInterval(policy.splash.minIntervalSeconds)} · ${
                    policy.splash.dailyLimit > 0 ? `${policy.splash.dailyLimit} 次/天` : "不限次"
                  }`
                : "已关闭"}
            </span>
          ) : null}
        </div>
        {!policy?.configured ? (
          <Button size="sm" variant="outline" onClick={onOpenConfig}>
            <Settings2 className="size-3.5" />
            去配置
          </Button>
        ) : null}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="已同意" icon={CircleCheck} tone="positive" value={stats?.summary.accepted ?? "—"} />
        <StatTile label="已拒绝" icon={CircleX} value={stats?.summary.declined ?? "—"} />
        <StatTile
          label="待重新选择"
          icon={RefreshCw}
          tone={stats?.summary.outdated ? "default" : "muted"}
          value={stats?.summary.outdated ?? "—"}
        />
        <StatTile label="今日开屏人数" icon={Users} value={stats?.summary.todayUsers ?? "—"} />
        <StatTile label="今日展示" icon={Eye} value={stats?.summary.todayShown ?? "—"} />
        <StatTile label="今日点击" icon={MousePointerClick} value={stats?.summary.todayClicked ?? "—"} />
        <StatTile
          label="今日失败"
          icon={TriangleAlert}
          tone={stats?.summary.todayFailed ? "default" : "muted"}
          value={stats?.summary.todayFailed ?? "—"}
        />
      </div>

      <ChartCard title="开屏" loading={statsQuery.isLoading} empty={splashEmpty} height={240} action={rangePicker}>
        <ChartContainer config={splashConfig} className="h-[240px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {SPLASH_SERIES.map((series) => (
              <Bar key={series.key} dataKey={series.key} fill={`var(--color-${series.key})`} radius={[3, 3, 0, 0]} />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>

      <ChartCard title="选择变更" loading={statsQuery.isLoading} empty={consentEmpty} height={200}>
        <ChartContainer config={consentConfig} className="h-[200px] w-full">
          <BarChart data={rows} margin={{ left: 0, right: 8, top: 8 }} barGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent />} />
            {CONSENT_SERIES.map((series) => (
              <Bar key={series.key} dataKey={series.key} fill={`var(--color-${series.key})`} radius={[3, 3, 0, 0]} />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </BarChart>
        </ChartContainer>
      </ChartCard>
    </div>
  );
}
