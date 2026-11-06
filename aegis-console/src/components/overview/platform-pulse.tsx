"use client";

import { useMemo } from "react";
import Link from "next/link";
import { Cell, Label, Pie, PieChart } from "recharts";
import { Cable, Gauge, MonitorSmartphone, ShieldCheck, Users } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminAppsQuery, useAdminDashboardQuery, useSystemOnlineStatsQuery } from "@/lib/admin-hooks";
import type { AppSummary } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { CardLink, formatCount, OverviewCard } from "./overview-kit";

type AppState = "running" | "limited" | "disabled";

const STATES: Array<{ key: AppState; label: string; dot: string }> = [
  { key: "running", label: "正常运行", dot: "bg-emerald-500" },
  { key: "limited", label: "限制登录或注册", dot: "bg-amber-500" },
  { key: "disabled", label: "已停用", dot: "bg-rose-500" }
];

const CHART_CONFIG = {
  running: { label: "正常运行", theme: { light: "#10b981", dark: "#34d399" } },
  limited: { label: "限制登录或注册", theme: { light: "#f59e0b", dark: "#fbbf24" } },
  disabled: { label: "已停用", theme: { light: "#f43f5e", dark: "#fb7185" } }
} satisfies ChartConfig;

function appState(app: AppSummary): AppState {
  if (!app.status) return "disabled";
  if (!app.loginStatus || !app.registerStatus) return "limited";
  return "running";
}

/**
 * 平台状态：应用运行状态的环形图，加上实时在线人数与管理员会话。
 * 在线统计走 Redis 实时计数，30 秒刷新一次；接口要求超管，由调用方判定后再挂载。
 */
export function PlatformPulse({ className }: { className?: string }) {
  const apps = useAdminAppsQuery();
  const dashboard = useAdminDashboardQuery();
  const online = useSystemOnlineStatsQuery({ refetchInterval: 30_000 });

  const counts = useMemo(() => {
    const result: Record<AppState, number> = { running: 0, limited: 0, disabled: 0 };
    for (const app of apps.data ?? []) result[appState(app)] += 1;
    return result;
  }, [apps.data]);
  const total = apps.data?.length ?? 0;
  const pie = STATES.map((state) => ({ key: state.key, name: state.label, value: counts[state.key] })).filter((item) => item.value > 0);

  const live = [
    { icon: Users, label: "在线用户", value: online.data?.onlineUsers, loading: online.isLoading },
    { icon: Cable, label: "实时连接", value: online.data?.onlineConnections, loading: online.isLoading },
    { icon: MonitorSmartphone, label: "在线应用", value: online.data?.onlineApps, loading: online.isLoading },
    { icon: ShieldCheck, label: "管理员会话", value: dashboard.data?.stats.activeSessions, loading: dashboard.isLoading }
  ];

  return (
    <OverviewCard
      icon={Gauge}
      tone="emerald"
      title="平台状态"
      className={className}
      action={<CardLink href="/apps">应用</CardLink>}
    >
      <div className="flex items-center gap-4 sm:gap-5">
        <div className="relative size-[120px] shrink-0">
          {apps.isLoading ? (
            <Skeleton className="size-full rounded-full" />
          ) : total === 0 ? (
            <div className="grid size-full place-items-center rounded-full border-[10px] border-muted">
              <span className="text-xs text-muted-foreground">暂无应用</span>
            </div>
          ) : (
            <ChartContainer config={CHART_CONFIG} className="aspect-square size-full">
              <PieChart>
                <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel nameKey="key" />} />
                <Pie data={pie} dataKey="value" nameKey="key" innerRadius={42} outerRadius={58} paddingAngle={pie.length > 1 ? 3 : 0} cornerRadius={4} strokeWidth={0}>
                  {pie.map((item) => (
                    <Cell key={item.key} fill={`var(--color-${item.key})`} />
                  ))}
                  <Label
                    content={({ viewBox }) => {
                      if (!viewBox || !("cx" in viewBox)) return null;
                      return (
                        <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) - 6} className="fill-foreground font-data text-xl font-semibold">
                            {total}
                          </tspan>
                          <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 13} className="fill-muted-foreground text-[10px]">
                            个应用
                          </tspan>
                        </text>
                      );
                    }}
                  />
                </Pie>
              </PieChart>
            </ChartContainer>
          )}
        </div>

        <ul className="min-w-0 flex-1 space-y-2">
          {STATES.map((state) => (
            <li key={state.key}>
              <Link
                href="/apps"
                className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-xs transition-colors hover:bg-muted/60"
              >
                <span aria-hidden className={cn("size-2 shrink-0 rounded-full", state.dot)} />
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{state.label}</span>
                {apps.isLoading ? (
                  <Skeleton className="h-4 w-6" />
                ) : (
                  <span className="font-data text-sm font-semibold tabular-nums">{counts[state.key]}</span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      </div>

      <Separator className="my-4" />

      <div className="grid grid-cols-2 gap-2">
        {live.map((item) => (
          <div key={item.label} className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/50 px-3 py-2.5">
            <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
              <item.icon className="size-3.5 shrink-0" />
              <span className="truncate">{item.label}</span>
            </span>
            {item.loading ? (
              <Skeleton className="h-6 w-12" />
            ) : (
              <span className="flex items-center gap-1.5">
                <span className="font-data text-lg leading-6 font-semibold tabular-nums">{typeof item.value === "number" ? formatCount(item.value) : "—"}</span>
                {item.label === "在线用户" && (item.value ?? 0) > 0 ? (
                  <span aria-hidden className="relative flex size-1.5">
                    <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-500 opacity-60 motion-reduce:animate-none" />
                    <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
                  </span>
                ) : null}
              </span>
            )}
          </div>
        ))}
      </div>
    </OverviewCard>
  );
}
