"use client";

import { useMemo } from "react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { ShieldCheck, ShieldX, TrendingUp, Users } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminAppStatsQuery, useAdminAppTrendQuery } from "@/lib/admin-hooks";
import { cn } from "@/lib/utils";
import type { UserQueryState } from "./shared";

/**
 * 指标带 —— 同时是筛选器。
 *
 * 「受限 3」和「点一下只看这 3 个」在管理员脑子里本来就是同一个动作，
 * 拆成"上面看数字、下面自己去下拉框选状态"是白白多一步。
 * 因此每张卡都对应一个 status 取值，选中态与当前筛选保持一致。
 *
 * 趋势图用的是 `/stats/user-trend` 的真实序列，不是拿 stats 里三个数字插值 ——
 * 后者画出来是一条假曲线，比不画更糟。
 */

type MetricKey = "all" | "enabled" | "disabled";

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
  const series = useMemo(
    () =>
      (trendQuery.data?.series ?? []).map((point) => ({
        date: point.date,
        count: point.count,
        label: point.date.slice(5)
      })),
    [trendQuery.data]
  );

  const cards: Array<{
    key: MetricKey;
    label: string;
    value: number | undefined;
    icon: React.ReactNode;
    tone: string;
  }> = [
    {
      key: "all",
      label: "总用户",
      value: stats?.totalUsers,
      icon: <Users className="size-3.5" />,
      tone: "data-[active=true]:border-foreground/40"
    },
    {
      key: "enabled",
      label: "启用",
      value: stats?.enabledUsers,
      icon: <ShieldCheck className="size-3.5" />,
      tone: "data-[active=true]:border-emerald-400 data-[active=true]:bg-emerald-50/60 dark:data-[active=true]:border-emerald-800 dark:data-[active=true]:bg-emerald-950/30"
    },
    {
      key: "disabled",
      label: "受限",
      value: stats?.disabledUsers,
      icon: <ShieldX className="size-3.5" />,
      tone: "data-[active=true]:border-red-400 data-[active=true]:bg-red-50/60 dark:data-[active=true]:border-red-900 dark:data-[active=true]:bg-red-950/30"
    }
  ];

  const loading = statsQuery.isLoading;

  return (
    <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(280px,0.85fr)]">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {cards.map((card) => (
          <button
            key={card.key}
            type="button"
            data-active={status === card.key}
            onClick={() => onStatusChange(card.key)}
            className={cn(
              "rounded-2xl border px-4 py-3 text-left transition-colors",
              "hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              card.tone
            )}
          >
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              {card.icon}
              {card.label}
            </span>
            {loading ? (
              <Skeleton className="mt-2 h-7 w-14" />
            ) : (
              <span className="mt-1 block text-2xl font-semibold tabular-nums">
                {(card.value ?? 0).toLocaleString("zh-CN")}
              </span>
            )}
          </button>
        ))}

        {/* 今日新增不是筛选项 —— 后端没有「按注册日期精确到今天」以外的语义，
            点它等于设一次日期范围，与左边三张卡的「切换状态」不是一类动作。 */}
        <div className="rounded-2xl border px-4 py-3">
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <TrendingUp className="size-3.5" />
            今日新增
          </span>
          {loading ? (
            <Skeleton className="mt-2 h-7 w-14" />
          ) : (
            <span className="mt-1 block text-2xl font-semibold tabular-nums">
              {stats?.newUsersToday ? `+${stats.newUsersToday.toLocaleString("zh-CN")}` : 0}
            </span>
          )}
          <span className="mt-0.5 block text-[11px] text-muted-foreground">
            7 日 {stats?.newUsersLast7Days ?? 0} · 30 日 {stats?.newUsersLast30Days ?? 0}
          </span>
        </div>
      </div>

      <div className="rounded-2xl border px-4 py-3">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">近 30 天新增</span>
          <span className="text-xs tabular-nums text-muted-foreground">
            共 {trendQuery.data?.totalNew ?? 0}
          </span>
        </div>
        <div className="mt-2 h-[52px]">
          {trendQuery.isLoading ? (
            <Skeleton className="size-full rounded-lg" />
          ) : series.length ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={series} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
                <defs>
                  <linearGradient id="app-users-trend" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-primary)" stopOpacity={0.35} />
                    <stop offset="100%" stopColor="var(--color-primary)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                {/* 轴只用于建立坐标系，不渲染 —— 52px 高度放不下任何刻度文字 */}
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
                <Area
                  type="monotone"
                  dataKey="count"
                  stroke="var(--color-primary)"
                  strokeWidth={1.5}
                  fill="url(#app-users-trend)"
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
