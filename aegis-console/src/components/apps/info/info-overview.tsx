"use client";

import { useMemo } from "react";
import { Activity, TrendingUp, Users2 } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { formatCount } from "@/components/apps/app-shared";
import { DetailPanel, Meter, Metric, PanelLink, RingGauge, TrendBars } from "@/components/apps/detail/app-detail-kit";
import { useAdminAppStatsQuery, useAdminAppTrendQuery } from "@/lib/admin-hooks";
import { appSectionHref } from "@/lib/app-sections";
import { cn } from "@/lib/utils";

/** 后端给的是日历日 `YYYY-MM-DD`，直接取月日；交给 Date 会被当成 UTC 零点，西半球显示成前一天 */
function monthDay(value: string) {
  const matched = /^\d{4}-(\d{2})-(\d{2})/.exec(value);
  return matched ? `${matched[1]}/${matched[2]}` : value;
}

function percent(part: number, whole: number) {
  return whole > 0 ? Math.round((part / whole) * 1000) / 10 : null;
}

/**
 * 运行概况：三栏回答三个问题。
 *
 * - 用户规模：总数，以及其中多少被停用（停用占比高通常意味着批量封禁或风控误伤）；
 * - 今天：新增与登录，登录成功率画成环，失败多时数字变色；
 * - 增长：14 天每日新增的柱形，今天和近 7 日日均放在一起比，看出今天是不是异常。
 */
export function InfoOverview({ appKey }: { appKey: string }) {
  const statsQuery = useAdminAppStatsQuery(appKey);
  // 与「统计分析」区块同一个查询（同 key），来回切换不重复请求
  const trendQuery = useAdminAppTrendQuery(appKey, 14);
  const stats = statsQuery.data;
  const loading = statsQuery.isLoading;

  const total = stats?.totalUsers ?? 0;
  const enabled = stats?.enabledUsers ?? 0;
  const disabled = stats?.disabledUsers ?? 0;
  const success = stats?.loginSuccessToday ?? 0;
  const failure = stats?.loginFailureToday ?? 0;
  const successRate = percent(success, success + failure);
  const failRate = percent(failure, success + failure);
  const weekAvg = stats ? Math.round((stats.newUsersLast7Days / 7) * 10) / 10 : null;
  const todayNew = stats?.newUsersToday ?? 0;

  const trend = useMemo(
    () => (trendQuery.data?.series ?? []).map((point) => ({ label: monthDay(point.date), value: point.count })),
    [trendQuery.data]
  );

  return (
    <DetailPanel
      icon={Activity}
      tone="primary"
      title="运行概况"
      description="用户规模、今日动态与近 14 天增长"
      aside={
        <PanelLink href={appSectionHref(appKey, "stats")}>
          统计分析
          <TrendingUp className="size-3" />
        </PanelLink>
      }
      bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
    >
      <div className="@container/overview border-t border-border/70">
        <div className="grid min-w-0 divide-y divide-border/70 @3xl/overview:grid-cols-[1.05fr_1.2fr_1fr] @3xl/overview:divide-x @3xl/overview:divide-y-0">
          {/* 用户规模 */}
          <div className="min-w-0 space-y-3.5 p-4 sm:p-5">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="text-[11px] text-muted-foreground">总用户</div>
                {loading ? (
                  <Skeleton className="mt-1.5 h-9 w-28 rounded" />
                ) : (
                  <div className="truncate text-[2rem] leading-10 font-semibold tracking-tight tabular-nums">
                    {formatCount(stats?.totalUsers)}
                  </div>
                )}
              </div>
              <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-muted text-muted-foreground">
                <Users2 className="size-4" />
              </span>
            </div>
            <Meter
              segments={[
                { label: "启用", value: enabled, className: "bg-emerald-500" },
                { label: "停用", value: disabled, className: "bg-zinc-400 dark:bg-zinc-500" }
              ]}
            />
            <dl className="grid grid-cols-3 gap-2">
              <Legend dot="bg-emerald-500" label="启用" value={stats?.enabledUsers} hint={percentLabel(enabled, total)} loading={loading} />
              <Legend dot="bg-zinc-400" label="停用" value={stats?.disabledUsers} hint={percentLabel(disabled, total)} loading={loading} />
              <Legend label="第三方绑定" value={stats?.oauthBindCount} loading={loading} />
            </dl>
          </div>

          {/* 今日 */}
          <div className="min-w-0 space-y-3 p-4 sm:p-5">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[11px] text-muted-foreground">今日</span>
              {failRate !== null && (
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 text-[10px] font-medium tabular-nums",
                    failRate >= 20
                      ? "bg-red-500/10 text-red-600 dark:text-red-400"
                      : failRate >= 5
                        ? "bg-amber-500/10 text-amber-700 dark:text-amber-300"
                        : "bg-muted text-muted-foreground"
                  )}
                >
                  失败率 {failRate}%
                </span>
              )}
            </div>
            <div className="grid grid-cols-3 gap-2">
              <Metric label="新增" value={formatCount(stats?.newUsersToday)} loading={loading} className="gap-1 p-2.5" />
              <Metric label="登录成功" value={formatCount(stats?.loginSuccessToday)} loading={loading} className="gap-1 p-2.5" />
              <Metric
                label="登录失败"
                value={formatCount(stats?.loginFailureToday)}
                loading={loading}
                emphasis={failure > 0 ? "warn" : undefined}
                className="gap-1 p-2.5"
              />
            </div>
            <div className="flex items-center gap-3 rounded-xl bg-muted/45 px-3 py-2">
              <RingGauge value={successRate} tone={successRate !== null && successRate < 80 ? "amber" : "emerald"} size={34} stroke={4} />
              <div className="min-w-0 flex-1 text-[11px]">
                <div className="text-muted-foreground">登录成功率</div>
                <div className="font-semibold tabular-nums">
                  {successRate === null ? "今日暂无登录" : `${successRate}%（${formatCount(success + failure)} 次尝试）`}
                </div>
              </div>
            </div>
          </div>

          {/* 增长 */}
          <div className="min-w-0 space-y-3 p-4 sm:p-5">
            <div className="flex items-center justify-between gap-2 text-[11px]">
              <span className="text-muted-foreground">近 14 天新增</span>
              <span className="tabular-nums text-muted-foreground">
                7 日 <b className="font-semibold text-foreground">{formatCount(stats?.newUsersLast7Days)}</b>
                <span className="mx-1.5 text-border">|</span>
                30 日 <b className="font-semibold text-foreground">{formatCount(stats?.newUsersLast30Days)}</b>
              </span>
            </div>
            <TrendBars data={trend} loading={trendQuery.isLoading} tone="sky" />
            {weekAvg !== null && (
              <div className="text-[11px] text-muted-foreground">
                今日新增 <span className="font-medium text-foreground tabular-nums">{formatCount(todayNew)}</span>，近 7 日日均{" "}
                <span className="font-medium text-foreground tabular-nums">{weekAvg}</span>
                {weekAvg > 0 && todayNew > weekAvg * 2 && (
                  <span className="ml-1 text-amber-600 dark:text-amber-400">今日明显偏高</span>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </DetailPanel>
  );
}

function percentLabel(part: number, whole: number) {
  const value = percent(part, whole);
  return value === null ? undefined : `${value}%`;
}

function Legend({
  label,
  value,
  hint,
  dot,
  loading
}: {
  label: string;
  value?: number | null;
  hint?: string;
  dot?: string;
  loading?: boolean;
}) {
  return (
    <div className="min-w-0">
      <dt className="flex items-center gap-1 truncate text-[10px] text-muted-foreground">
        {dot && <span className={cn("size-1.5 shrink-0 rounded-full", dot)} />}
        <span className="truncate">{label}</span>
      </dt>
      <dd className="mt-0.5 flex items-baseline gap-1">
        {loading ? (
          <Skeleton className="h-4 w-10 rounded" />
        ) : (
          <>
            <span className="truncate text-sm font-semibold tabular-nums">{formatCount(value)}</span>
            {hint && <span className="text-[10px] text-muted-foreground tabular-nums">{hint}</span>}
          </>
        )}
      </dd>
    </div>
  );
}
