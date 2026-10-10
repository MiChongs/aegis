"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { CheckCircle2, Clock3, Eye, Settings2, Users } from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { describeMembership, describeRewards, formatCooldown } from "@/components/apps/rewarded-ad/rewarded-ad-shared";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useRewardedAdConfigQuery, useRewardedAdStatsQuery } from "@/lib/rewarded-ad-hooks";
import { useAdminVipFeaturesQuery, useAdminVipPlansQuery } from "@/lib/vip-hooks";

/** 观看与发放分两个色系：两条线挨得很近时，同色系会让人分不清哪条是哪条。 */
const SERIES: Array<{ key: "views" | "granted" | "users"; label: string; color: ThemedColor }> = [
  { key: "views", label: "观看", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "granted", label: "发放", color: { light: "#10b981", dark: "#34d399" } },
  { key: "users", label: "人数", color: { light: "#f59e0b", dark: "#fbbf24" } }
];

const RANGES = [7, 14, 30] as const;

export function RewardedAdOverviewPanel({ appKey, onOpenConfig }: { appKey: string; onOpenConfig: () => void }) {
  const [days, setDays] = useState<number>(14);
  const configQuery = useRewardedAdConfigQuery(appKey);
  const statsQuery = useRewardedAdStatsQuery(appKey, days);
  const plansQuery = useAdminVipPlansQuery(appKey);
  const featuresQuery = useAdminVipFeaturesQuery(appKey);

  const config = configQuery.data;
  const stats = statsQuery.data;
  const chartConfig = useMemo(() => buildChartConfig(SERIES), []);
  const rows = useMemo(
    () => (stats?.trend ?? []).map((day) => ({ ...day, label: day.date.slice(5) })),
    [stats]
  );
  const planNames = useMemo(
    () => Object.fromEntries((plansQuery.data ?? []).map((plan) => [plan.id, plan.name])),
    [plansQuery.data]
  );
  const featureNames = useMemo(
    () => Object.fromEntries((featuresQuery.data ?? []).map((feature) => [feature.tag, feature.name])),
    [featuresQuery.data]
  );
  const sceneStats = useMemo(
    () => new Map((stats?.scenes ?? []).map((item) => [item.scene, item])),
    [stats]
  );
  const empty = rows.every((row) => row.views === 0);

  if (configQuery.isLoading) {
    return <Skeleton className="h-64 w-full rounded-2xl" />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <StatusDot active={Boolean(config?.enabled)} labelActive="激励广告已启用" labelInactive="激励广告未启用" />
        {!config?.enabled ? (
          <Button size="sm" variant="outline" onClick={onOpenConfig}>
            <Settings2 className="size-3.5" />
            去配置
          </Button>
        ) : null}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="今日观看" icon={Eye} value={stats?.summary.todayViews ?? "—"} />
        <StatTile
          label="今日发放"
          icon={CheckCircle2}
          tone="positive"
          value={stats?.summary.todayGranted ?? "—"}
          hint={stats ? `未发放 ${stats.summary.todayRejected}` : undefined}
        />
        <StatTile label="今日人数" icon={Users} value={stats?.summary.todayUsers ?? "—"} />
        <StatTile
          label="待确认"
          icon={Clock3}
          tone={stats?.summary.pending ? "default" : "muted"}
          value={stats?.summary.pending ?? "—"}
          hint={stats ? `累计发放 ${stats.summary.totalGranted}` : undefined}
        />
      </div>

      <ChartCard
        title="趋势"
        loading={statsQuery.isLoading}
        empty={empty}
        height={260}
        action={
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
        }
      >
        <ChartContainer config={chartConfig} className="h-[260px] w-full">
          <AreaChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <defs>
              {SERIES.map((series) => (
                <linearGradient key={series.key} id={`fill-rewarded-${series.key}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={`var(--color-${series.key})`} stopOpacity={0.3} />
                  <stop offset="95%" stopColor={`var(--color-${series.key})`} stopOpacity={0.02} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
            {SERIES.map((series) => (
              <Area
                key={series.key}
                dataKey={series.key}
                type="monotone"
                stroke={`var(--color-${series.key})`}
                fill={`url(#fill-rewarded-${series.key})`}
                strokeWidth={2}
                dot={false}
              />
            ))}
            <ChartLegend content={<ChartLegendContent />} />
          </AreaChart>
        </ChartContainer>
      </ChartCard>

      <div className="overflow-hidden rounded-2xl border border-border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>场景</TableHead>
              <TableHead>奖励</TableHead>
              <TableHead>限额</TableHead>
              <TableHead className="text-right">{days} 天观看 / 发放</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(config?.scenes ?? []).length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
                  暂无场景
                </TableCell>
              </TableRow>
            ) : (
              (config?.scenes ?? []).map((scene) => {
                const item = sceneStats.get(scene.key);
                return (
                  <TableRow key={scene.key} className={scene.enabled ? undefined : "opacity-60"}>
                    <TableCell className="text-xs">
                      <span className="font-medium">{scene.name}</span>
                      <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{scene.key}</p>
                    </TableCell>
                    <TableCell className="text-xs">
                      {describeRewards(scene.rewards, config?.catalog ?? [], planNames)}
                      {describeMembership(scene, featureNames) ? (
                        <p className="mt-0.5 text-[11px] text-muted-foreground">
                          {describeMembership(scene, featureNames)}
                        </p>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {scene.dailyLimit > 0 ? `${scene.dailyLimit} 次/天` : "不限次"} · 冷却{" "}
                      {formatCooldown(scene.cooldownSeconds)}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs tabular-nums">
                      {item?.views ?? 0} / {item?.granted ?? 0}
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
