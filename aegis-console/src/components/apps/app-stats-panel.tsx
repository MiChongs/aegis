"use client";

import { useMemo } from "react";
import { Area, AreaChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";
import { Card, CardContent } from "@/components/ui/card";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminAppStatsQuery, useAdminAppTrendQuery } from "@/lib/admin-hooks";

const chartConfig: ChartConfig = { count: { label: "新增用户", color: "#3b82f6" } };

function fmtDate(v: string) {
  try { return new Date(v).toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" }); } catch { return v; }
}

export function AppStatsPanel({ appKey }: { appKey?: string | null }) {
  const statsQuery = useAdminAppStatsQuery(appKey);
  const trendQuery = useAdminAppTrendQuery(appKey, 14);
  const stats = statsQuery.data;
  const trend = trendQuery.data;

  const trendData = useMemo(() => {
    if (!trend?.series) return [];
    return trend.series.map((p: { date: string; count: number }) => ({ date: fmtDate(p.date), count: p.count }));
  }, [trend]);

  if (!appKey) return <div className="py-12 text-center text-sm text-muted-foreground">请先选择应用</div>;

  return (
    <div className="space-y-5">
      {/* 指标卡片 */}
      <div className="grid gap-3 sm:grid-cols-3 lg:grid-cols-6">
        <Metric label="总用户" value={stats?.totalUsers} loading={statsQuery.isLoading} />
        <Metric label="启用" value={stats?.enabledUsers} loading={statsQuery.isLoading} />
        <Metric label="停用" value={stats?.disabledUsers} loading={statsQuery.isLoading} />
        <Metric label="今日新增" value={stats?.newUsersToday} loading={statsQuery.isLoading} />
        <Metric label="今日登录" value={stats?.loginSuccessToday} loading={statsQuery.isLoading} />
        <Metric label="OAuth 绑定" value={stats?.oauthBindCount} loading={statsQuery.isLoading} />
      </div>

      {/* 用户趋势 */}
      <Card>
        <CardContent className="p-4">
          <h3 className="mb-3 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">14 天用户趋势</h3>
          {trendQuery.isLoading ? (
            <Skeleton className="h-48 w-full rounded-lg" />
          ) : trendData.length > 0 ? (
            <ChartContainer config={chartConfig} className="h-48 w-full">
              <AreaChart data={trendData} margin={{ top: 8, right: 8, bottom: 0, left: -12 }}>
                <defs>
                  <linearGradient id="trendGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#3b82f6" stopOpacity={0.2} />
                    <stop offset="100%" stopColor="#3b82f6" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                <XAxis dataKey="date" tick={{ fontSize: 10, fill: "var(--muted-foreground)" }} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 10, fill: "var(--muted-foreground)" }} axisLine={false} tickLine={false} allowDecimals={false} width={36} />
                <Tooltip
                  cursor={{ stroke: "var(--border)", strokeWidth: 1 }}
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    return (
                      <div className="rounded-lg border bg-popover px-3 py-2 shadow-md">
                        <div className="text-[11px] text-muted-foreground">{label}</div>
                        <div className="mt-0.5 text-sm font-semibold tabular-nums">{Number(payload[0].value).toLocaleString()} 新增</div>
                      </div>
                    );
                  }}
                />
                <Area type="monotone" dataKey="count" stroke="#3b82f6" fill="url(#trendGrad)" strokeWidth={2} dot={false} activeDot={{ r: 4, fill: "#3b82f6", stroke: "var(--background)", strokeWidth: 2 }} />
              </AreaChart>
            </ChartContainer>
          ) : (
            <div className="flex h-48 items-center justify-center text-xs text-muted-foreground">暂无趋势数据</div>
          )}
        </CardContent>
      </Card>

      {/* 额外统计 */}
      {stats && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Metric label="7 天新增" value={stats.newUsersLast7Days} />
          <Metric label="30 天新增" value={stats.newUsersLast30Days} />
          <Metric label="Banner 数" value={stats.bannerCount} />
          <Metric label="公告数" value={stats.noticeCount} />
        </div>
      )}
    </div>
  );
}

function Metric({ label, value, loading }: { label: string; value?: number | null; loading?: boolean }) {
  return (
    <div className="rounded-xl border px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      {loading ? <Skeleton className="mt-1 h-5 w-10 rounded" /> : <div className="mt-0.5 text-lg font-semibold tabular-nums">{(value ?? 0).toLocaleString()}</div>}
    </div>
  );
}
