"use client";

import { useMemo } from "react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminAppStatsQuery, useAdminAppTrendQuery } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import type { UserQueryState } from "./shared";

/**
 * 概览带 —— 同时是筛选器。
 *
 * 「受限 3」和「点一下只看这 3 个」在管理员脑子里本来就是同一个动作，
 * 所以三个状态分段都可以点，选中态与当前筛选保持一致。
 * 分段下面的比例条把「正常 / 受限」的占比画出来：数字要读，比例一眼就能看。
 *
 * 趋势图用的是 `/stats/user-trend` 的真实序列，不是拿 stats 里三个数字插值。
 */

type MetricKey = "all" | "enabled" | "disabled";

function formatCount(value?: number) {
  return (value ?? 0).toLocaleString("zh-CN");
}

export function AppUsersMetrics({
  appKey,
  status,
  onStatusChange
}: {
  appKey: string | null;
  status: UserQueryState["status"];
  onStatusChange: (next: MetricKey) => void;
}) {
  const statsQuery = useAdminAppStatsQuery(appKey);
  const trendQuery = useAdminAppTrendQuery(appKey, 30);

  const stats = statsQuery.data;
  const loading = statsQuery.isLoading;
  const series = useMemo(
    () =>
      (trendQuery.data?.series ?? []).map((point) => ({
        date: point.date,
        count: point.count,
        label: point.date.slice(5)
      })),
    [trendQuery.data]
  );

  const total = stats?.totalUsers ?? 0;
  const enabledShare = total > 0 ? ((stats?.enabledUsers ?? 0) / total) * 100 : 0;

  const segments: Array<{ key: MetricKey; label: string; value?: number; dot?: string }> = [
    { key: "all", label: "全部用户", value: stats?.totalUsers },
    { key: "enabled", label: "正常", value: stats?.enabledUsers, dot: "bg-emerald-500" },
    { key: "disabled", label: "受限", value: stats?.disabledUsers, dot: "bg-red-500" }
  ];

  return (
    <div className="grid overflow-hidden rounded-2xl border bg-card text-card-foreground lg:grid-cols-[minmax(0,1.35fr)_minmax(0,0.65fr)_minmax(0,1fr)]">
      {/* 状态分段 + 占比 */}
      <div className="flex flex-col gap-3 p-4">
        <div className="grid grid-cols-3 gap-1.5">
          {segments.map((segment) => {
            const active = status === segment.key;
            return (
              <button
                key={segment.key}
                type="button"
                aria-pressed={active}
                onClick={() => onStatusChange(segment.key)}
                className={cn(
                  "rounded-xl px-3 py-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                  active ? "bg-muted" : "hover:bg-muted/50"
                )}
              >
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  {segment.dot ? <span className={cn("size-1.5 rounded-full", segment.dot)} /> : null}
                  {segment.label}
                </span>
                {loading ? (
                  <Skeleton className="mt-1.5 h-7 w-16" />
                ) : (
                  <span className="mt-1 block text-[26px] leading-8 font-semibold tracking-tight tabular-nums">
                    {formatCount(segment.value)}
                  </span>
                )}
              </button>
            );
          })}
        </div>
        <div className="px-3">
          {loading ? (
            <Skeleton className="h-1.5 w-full rounded-full" />
          ) : (
            <div
              className="flex h-1.5 overflow-hidden rounded-full bg-muted"
              role="img"
              aria-label={`正常用户占 ${enabledShare.toFixed(1)}%`}
            >
              <div className="h-full bg-emerald-500" style={{ width: `${enabledShare}%` }} />
              <div className="h-full bg-red-500" style={{ width: `${total > 0 ? 100 - enabledShare : 0}%` }} />
            </div>
          )}
          <p className="mt-1.5 text-[11px] text-muted-foreground tabular-nums">
            {loading ? " " : total > 0 ? `正常占比 ${enabledShare.toFixed(1)}%` : "暂无用户"}
          </p>
        </div>
      </div>

      {/* 新增 */}
      <div className="flex flex-col justify-center gap-2.5 border-t p-4 lg:border-t-0 lg:border-l">
        <span className="text-xs text-muted-foreground">今日新增</span>
        {loading ? (
          <Skeleton className="h-7 w-14" />
        ) : (
          <span className="text-[26px] leading-8 font-semibold tracking-tight tabular-nums">
            {stats?.newUsersToday ? `+${formatCount(stats.newUsersToday)}` : "0"}
          </span>
        )}
        <dl className="grid grid-cols-2 gap-2 text-[11px]">
          <div>
            <dt className="text-muted-foreground">近 7 日</dt>
            <dd className="font-medium tabular-nums">{loading ? "—" : formatCount(stats?.newUsersLast7Days)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">近 30 日</dt>
            <dd className="font-medium tabular-nums">{loading ? "—" : formatCount(stats?.newUsersLast30Days)}</dd>
          </div>
        </dl>
      </div>

      {/* 趋势 */}
      <div className="flex min-w-0 flex-col border-t p-4 lg:border-t-0 lg:border-l">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">近 30 天注册趋势</span>
          <span className="text-xs text-muted-foreground tabular-nums">
            合计 <span className="font-medium text-foreground">{formatCount(trendQuery.data?.totalNew)}</span>
          </span>
        </div>
        <div className="mt-2 h-[76px] flex-1">
          {trendQuery.isLoading ? (
            <Skeleton className="size-full rounded-lg" />
          ) : series.length ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={series} margin={{ top: 4, right: 2, bottom: 0, left: 2 }}>
                {/* 轴只用于建立坐标系，不渲染 —— 这个高度放不下刻度文字 */}
                <XAxis dataKey="label" hide />
                <YAxis hide domain={[0, "dataMax"]} />
                <Tooltip
                  cursor={{ stroke: "var(--color-border)" }}
                  contentStyle={{
                    background: "var(--color-popover)",
                    border: "1px solid var(--color-border)",
                    borderRadius: 8,
                    fontSize: 12,
                    padding: "4px 8px"
                  }}
                  labelStyle={{ color: "var(--color-muted-foreground)" }}
                  formatter={(value) => [`${Number(value ?? 0)} 人`, "新增"]}
                />
                {/* 主题的 primary 是黑白单色，画成线几乎看不出层次；趋势单独用一种蓝，深浅色都可读，
                    也不与状态分段的绿/红混淆 */}
                <Area
                  type="monotone"
                  dataKey="count"
                  stroke="#0ea5e9"
                  strokeWidth={1.75}
                  fill="#0ea5e9"
                  fillOpacity={0.12}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <div className="flex size-full items-center justify-center text-xs text-muted-foreground">
              暂无趋势数据
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
