"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { CalendarCheck2, Coins, Crown, Flame, Settings2, Sparkles, TrendingUp, Users } from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useAdminAppSignInStatsQuery } from "@/lib/admin-hooks";
import type { SignInRewardPolicy } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { MetricTile, Panel, bonusLabel, buildBonusLabels, formatNumber, sourceLabel } from "./signin-shared";

const RANGES = [7, 14, 30, 90] as const;

type TrendMetric = "count" | "integralReward" | "experienceReward";
const TREND_META: Record<TrendMetric, { label: string; unit: string; light: string; dark: string }> = {
  count: { label: "签到人数", unit: "人", light: "#10b981", dark: "#34d399" },
  integralReward: { label: "积分发放", unit: "积分", light: "#f59e0b", dark: "#fbbf24" },
  experienceReward: { label: "经验发放", unit: "经验", light: "#0ea5e9", dark: "#38bdf8" }
};

const HOUR_CHART: ChartConfig = { count: { label: "签到次数", theme: { light: "#6366f1", dark: "#818cf8" } } };

function percentChange(current: number, previous: number) {
  if (previous <= 0) return current > 0 ? null : 0;
  return ((current - previous) / previous) * 100;
}

export function SignInOverview({
  appKey,
  policy,
  onEditPolicy
}: {
  appKey: string;
  policy: SignInRewardPolicy;
  onEditPolicy: () => void;
}) {
  const [days, setDays] = useState<number>(14);
  const [metric, setMetric] = useState<TrendMetric>("count");
  const statsQuery = useAdminAppSignInStatsQuery(appKey, days);
  const stats = statsQuery.data;
  const loading = statsQuery.isLoading;
  const labels = useMemo(() => buildBonusLabels(policy), [policy]);

  const trend = useMemo(
    () => (stats?.trend ?? []).map((point) => ({ ...point, label: point.date.slice(5).replace("-", "/") })),
    [stats]
  );
  const trendConfig = useMemo<ChartConfig>(
    () => ({ [metric]: { label: TREND_META[metric].label, theme: { light: TREND_META[metric].light, dark: TREND_META[metric].dark } } }),
    [metric]
  );
  const hours = useMemo(
    () => (stats?.hourDistribution ?? Array(24).fill(0)).map((count, hour) => ({ hour: `${hour}`, label: `${hour}:00`, count })),
    [stats]
  );
  const peakHour = hours.reduce((best, item) => (item.count > best.count ? item : best), { hour: "-", label: "", count: 0 });
  const trendEmpty = trend.every((point) => point.count === 0);
  const windowAvg = stats && stats.days > 0 ? stats.windowSignCount / stats.days : 0;
  const streakMax = Math.max(1, ...(stats?.streakBuckets ?? []).map((b) => b.count));
  const bonusTotal = (stats?.bonusTypes ?? []).reduce((sum, item) => sum + item.count, 0);
  const sourceTotal = (stats?.sources ?? []).reduce((sum, item) => sum + item.count, 0);

  return (
    <div className="space-y-4">
      {/* 状态行 */}
      <div className="flex flex-col gap-3 rounded-2xl border bg-card p-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <StatusDot active={policy.enabled} labelActive="签到奖励发放中" labelInactive="签到奖励已关闭" />
            <Badge variant={policy.isDefault ? "outline" : "info"} size="sm">{policy.isDefault ? "系统默认策略" : "自定义策略"}</Badge>
          </div>
          <p className="truncate text-sm font-medium">{policy.name}</p>
          <p className="text-xs text-muted-foreground">
            基础 {formatNumber(policy.baseIntegral)} 积分与 {formatNumber(policy.baseExperience)} 经验，
            {policy.rules.filter((r) => r.enabled).length} 条加成规则生效，{policy.milestones.length} 个连签里程碑，按 {policy.timezone} 计日
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <ToggleGroup
            type="single"
            value={String(days)}
            onValueChange={(value) => value && setDays(Number(value))}
            className="rounded-lg border bg-muted/40 p-0.5"
          >
            {RANGES.map((range) => (
              <ToggleGroupItem
                key={range}
                value={String(range)}
                className="h-7 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm"
              >
                {range} 天
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Button size="sm" variant="outline" className="h-8 gap-1.5" onClick={onEditPolicy}>
            <Settings2 className="size-3.5" />
            编辑策略
          </Button>
        </div>
      </div>

      {/* 指标 */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <MetricTile
          label="今日签到"
          icon={<CalendarCheck2 className="size-3" />}
          value={formatNumber(stats?.todaySignCount)}
          delta={stats ? percentChange(stats.todaySignCount, stats.yesterdaySignCount) : null}
          hint={`昨日 ${formatNumber(stats?.yesterdaySignCount)}`}
          loading={loading}
        />
        <MetricTile
          label={`近 ${days} 天签到`}
          icon={<TrendingUp className="size-3" />}
          value={formatNumber(stats?.windowSignCount)}
          hint={`日均 ${formatNumber(windowAvg, 1)} 次`}
          loading={loading}
        />
        <MetricTile
          label={`近 ${days} 天签到人数`}
          icon={<Users className="size-3" />}
          value={formatNumber(stats?.windowUniqueUsers)}
          hint={
            stats && stats.windowUniqueUsers > 0
              ? `人均签到 ${formatNumber(stats.windowSignCount / stats.windowUniqueUsers, 1)} 天`
              : "窗口内无人签到"
          }
          loading={loading}
        />
        <MetricTile
          label="连签延续中"
          icon={<Flame className="size-3" />}
          value={formatNumber(stats?.activeStreakUsers)}
          hint={`最高连签 ${formatNumber(stats?.maxConsecutiveDays)} 天`}
          loading={loading}
        />
        <MetricTile
          label={`近 ${days} 天发放积分`}
          icon={<Coins className="size-3" />}
          tone="integral"
          value={formatNumber(stats?.windowIntegralReward)}
          hint={stats && stats.windowSignCount > 0 ? `单次平均 ${formatNumber(stats.windowIntegralReward / stats.windowSignCount, 1)}` : "暂无发放"}
          loading={loading}
        />
        <MetricTile
          label={`近 ${days} 天发放经验`}
          icon={<Sparkles className="size-3" />}
          tone="experience"
          value={formatNumber(stats?.windowExperienceReward)}
          hint={stats && stats.windowSignCount > 0 ? `单次平均 ${formatNumber(stats.windowExperienceReward / stats.windowSignCount, 1)}` : "暂无发放"}
          loading={loading}
        />
      </div>

      {/* 趋势 */}
      <Panel
        title="签到趋势"
        description={`近 ${days} 天每日数据，按 ${stats?.timezone || policy.timezone} 计日`}
        action={
          <ToggleGroup type="single" value={metric} onValueChange={(value) => value && setMetric(value as TrendMetric)} className="rounded-lg border bg-muted/40 p-0.5">
            {(Object.keys(TREND_META) as TrendMetric[]).map((key) => (
              <ToggleGroupItem key={key} value={key} className="h-7 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:shadow-sm">
                {TREND_META[key].label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      >
        {loading ? (
          <Skeleton className="h-64 w-full rounded-xl" />
        ) : trendEmpty ? (
          <div className="flex h-64 flex-col items-center justify-center gap-1 rounded-xl border border-dashed text-center">
            <CalendarCheck2 className="size-5 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">近 {days} 天暂无签到</p>
          </div>
        ) : (
          <ChartContainer config={trendConfig} className="h-64 w-full">
            <AreaChart data={trend} margin={{ top: 8, right: 8, bottom: 0, left: -8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/50" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={24} tickMargin={8} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={44} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              <Area
                dataKey={metric}
                type="monotone"
                stroke={`var(--color-${metric})`}
                fill={`var(--color-${metric})`}
                fillOpacity={0.12}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
            </AreaChart>
          </ChartContainer>
        )}
      </Panel>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* 连签分布 */}
        <Panel title="连签分布" description="连签仍在延续的用户（今天或昨天签过）按当前连签天数分段" icon={<Flame className="size-4" />}>
          {loading ? (
            <div className="space-y-2.5">
              {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-6 w-full" />)}
            </div>
          ) : (
            <ul className="space-y-2.5">
              {(stats?.streakBuckets ?? []).map((bucket) => {
                const share = stats && stats.activeStreakUsers > 0 ? (bucket.count / stats.activeStreakUsers) * 100 : 0;
                return (
                  <li key={bucket.label} className="grid grid-cols-[4.5rem_minmax(0,1fr)_4.5rem] items-center gap-3 text-xs">
                    <span className="text-muted-foreground">{bucket.label}</span>
                    <div className="h-2 overflow-hidden rounded-full bg-muted">
                      <div className="h-full rounded-full bg-orange-500" style={{ width: `${(bucket.count / streakMax) * 100}%` }} />
                    </div>
                    <span className="text-right tabular-nums">
                      <span className="font-medium">{formatNumber(bucket.count)}</span>
                      <span className="ml-1 text-muted-foreground">{share.toFixed(0)}%</span>
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </Panel>

        {/* 签到时段 */}
        <Panel
          title="签到时段"
          description={peakHour.count > 0 ? `近 ${days} 天签到最集中在 ${peakHour.hour}:00 到 ${Number(peakHour.hour) + 1}:00` : `近 ${days} 天各小时签到次数`}
          icon={<CalendarCheck2 className="size-4" />}
        >
          {loading ? (
            <Skeleton className="h-44 w-full rounded-xl" />
          ) : (
            <ChartContainer config={HOUR_CHART} className="h-44 w-full">
              <BarChart data={hours} margin={{ top: 4, right: 4, bottom: 0, left: -8 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/50" />
                <XAxis dataKey="hour" tickLine={false} axisLine={false} fontSize={10} interval={2} />
                <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={10} width={36} />
                <ChartTooltip content={<ChartTooltipContent labelFormatter={(_, payload) => payload?.[0]?.payload?.label ?? ""} />} />
                <Bar dataKey="count" fill="var(--color-count)" radius={[3, 3, 0, 0]} />
              </BarChart>
            </ChartContainer>
          )}
        </Panel>

        {/* 奖励类型 */}
        <Panel title="奖励构成" description={`近 ${days} 天每次签到命中的奖励类型`} icon={<Sparkles className="size-4" />}>
          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-8 w-full" />)}
            </div>
          ) : bonusTotal === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">暂无数据</p>
          ) : (
            <ul className="space-y-2">
              {stats?.bonusTypes.map((item) => (
                <li key={item.bonusType} className="relative overflow-hidden rounded-lg border px-3 py-2 text-xs">
                  <div className="absolute inset-y-0 left-0 bg-primary/5" style={{ width: `${(item.count / bonusTotal) * 100}%` }} />
                  <div className="relative flex items-center justify-between gap-3">
                    <span className="min-w-0 truncate">
                      {bonusLabel(labels, item.bonusType)}
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{item.bonusType}</span>
                    </span>
                    <span className="shrink-0 tabular-nums">
                      <span className="font-medium">{formatNumber(item.count)}</span>
                      <span className="ml-1.5 text-muted-foreground">{((item.count / bonusTotal) * 100).toFixed(1)}%</span>
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Panel>

        {/* 连签排行 */}
        <Panel title="连签排行" description="当前连签最长的用户" icon={<Crown className="size-4" />}>
          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
            </div>
          ) : (stats?.topStreaks.length ?? 0) === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">暂无连签中的用户</p>
          ) : (
            <ol className="divide-y">
              {stats?.topStreaks.map((user, index) => (
                <li key={user.userId}>
                  <Link
                    href={`/app-users/${encodeURIComponent(appKey)}/${user.userId}`}
                    className="-mx-2 flex items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-muted/50"
                  >
                    <span
                      className={cn(
                        "grid size-6 shrink-0 place-items-center rounded-md text-[11px] font-semibold tabular-nums",
                        index === 0 ? "bg-amber-500/15 text-amber-700 dark:text-amber-300" : "bg-muted text-muted-foreground"
                      )}
                    >
                      {index + 1}
                    </span>
                    <Avatar className="size-8 rounded-lg border">
                      <AvatarImage src={user.avatar} alt={user.nickname || user.account} />
                      <AvatarFallback className="rounded-lg text-[10px]">{(user.nickname || user.account).slice(0, 2)}</AvatarFallback>
                    </Avatar>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">{user.nickname || user.account}</span>
                      <span className="block truncate text-[11px] text-muted-foreground">累计 {formatNumber(user.totalSignDays)} 天，最近 {user.lastSignDate}</span>
                    </span>
                    <span className="shrink-0 text-right">
                      <span className="block text-sm font-semibold text-orange-600 tabular-nums dark:text-orange-400">{formatNumber(user.consecutiveDays)}</span>
                      <span className="block text-[10px] text-muted-foreground">连签天数</span>
                    </span>
                  </Link>
                </li>
              ))}
            </ol>
          )}
        </Panel>
      </div>

      {/* 累计 */}
      <Panel title="累计数据" description="自应用开始签到以来的全量统计">
        {loading ? (
          <Skeleton className="h-16 w-full" />
        ) : (
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3 lg:grid-cols-6">
            {[
              ["签到记录", formatNumber(stats?.totalSignRecords)],
              ["签到用户", formatNumber(stats?.uniqueSignedUsers)],
              ["发放积分", formatNumber(stats?.totalIntegralReward)],
              ["发放经验", formatNumber(stats?.totalExperienceReward)],
              ["平均连签", `${formatNumber(stats?.avgConsecutiveDays, 1)} 天`],
              ["最高连签", `${formatNumber(stats?.maxConsecutiveDays)} 天`]
            ].map(([label, value]) => (
              <div key={label} className="min-w-0">
                <dt className="text-xs text-muted-foreground">{label}</dt>
                <dd className="mt-0.5 truncate text-lg font-semibold tabular-nums">{value}</dd>
              </div>
            ))}
          </dl>
        )}
        {!loading && sourceTotal > 0 ? (
          <div className="mt-4 border-t pt-4">
            <div className="mb-2 text-xs text-muted-foreground">签到来源</div>
            <div className="flex h-2 overflow-hidden rounded-full bg-muted">
              {stats?.sources.map((item, index) => (
                <div
                  key={item.source}
                  className={cn("h-full", ["bg-emerald-500", "bg-sky-500", "bg-violet-500", "bg-amber-500"][index % 4])}
                  style={{ width: `${(item.count / sourceTotal) * 100}%` }}
                />
              ))}
            </div>
            <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs">
              {stats?.sources.map((item, index) => (
                <span key={item.source} className="flex items-center gap-1.5">
                  <span className={cn("size-2 rounded-full", ["bg-emerald-500", "bg-sky-500", "bg-violet-500", "bg-amber-500"][index % 4])} />
                  {sourceLabel(item.source)}
                  <span className="text-muted-foreground tabular-nums">{formatNumber(item.count)}</span>
                </span>
              ))}
            </div>
          </div>
        ) : null}
      </Panel>
    </div>
  );
}
