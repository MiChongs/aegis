"use client";

import type { ReactNode } from "react";
import Link from "next/link";
import { AppWindow, ArrowUpRight, CircleCheck, LogIn, ShieldAlert, UserPlus, Users2 } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { formatCount } from "@/components/apps/app-shared";
import type { PlatformOverviewSummary } from "@/lib/api/platform-governance";
import { cn } from "@/lib/utils";

/**
 * 应用列表顶部的总览带。
 *
 * 左边是规模（应用数与启停占比、用户、今日新增、今日登录），右边是「需要关注」：
 * 只列不为零的事项，每一项都能点 —— 能就地筛选的直接筛，治理类跳到平台治理台。
 * 一切正常时右栏只显示一句「一切正常」，不摆一排 0 去稀释注意力。
 *
 * 用户与登录数据来自治理总览接口，无 platform:app:read 权限时这三格不渲染。
 */

export type AppListCounts = {
  total: number;
  enabled: number;
  disabled: number;
  registerOff: number;
  loginOff: number;
  governed: number;
  /** 启用且注册、登录都开放 */
  fullyOpen: number;
};

export type AttentionFilter = "disabled" | "register-off" | "login-off" | "governed";

export function AppListOverview({
  counts,
  summary,
  loginFailuresToday,
  loading,
  metricsLoading,
  hasMetrics,
  onFilter
}: {
  counts: AppListCounts;
  summary?: PlatformOverviewSummary;
  loginFailuresToday: number;
  loading: boolean;
  metricsLoading: boolean;
  hasMetrics: boolean;
  onFilter: (filter: AttentionFilter) => void;
}) {
  const enabledShare = counts.total > 0 ? (counts.enabled / counts.total) * 100 : 0;
  const loginsToday = summary?.loginsToday ?? 0;
  const attempts = loginsToday + loginFailuresToday;
  const successRate = attempts > 0 ? (loginsToday / attempts) * 100 : null;

  const attention: Array<{ key: string; label: string; value: number; tone: "danger" | "warning"; icon: ReactNode; filter?: AttentionFilter; href?: string }> = [
    { key: "governed", label: "被平台治理", value: counts.governed, tone: "danger" as const, icon: <ShieldAlert className="size-3" />, filter: "governed" as const },
    { key: "appeals", label: "待处理申诉", value: summary?.pendingAppeals ?? 0, tone: "warning" as const, icon: <ShieldAlert className="size-3" />, href: "/platform?tab=appeals" },
    { key: "expiring", label: "处置即将到期", value: summary?.expiringSoon ?? 0, tone: "warning" as const, icon: <ShieldAlert className="size-3" />, href: "/platform?tab=apps" },
    { key: "disabled", label: "应用已停用", value: counts.disabled, tone: "danger" as const, icon: <AppWindow className="size-3" />, filter: "disabled" as const },
    { key: "register", label: "注册已关闭", value: counts.registerOff, tone: "warning" as const, icon: <UserPlus className="size-3" />, filter: "register-off" as const },
    { key: "login", label: "登录已关闭", value: counts.loginOff, tone: "warning" as const, icon: <LogIn className="size-3" />, filter: "login-off" as const }
  ].filter((item) => item.value > 0);

  return (
    <section className="overflow-hidden rounded-2xl border bg-card text-card-foreground">
      <div className={cn("grid grid-cols-2", hasMetrics ? "md:grid-cols-4" : "md:grid-cols-2")}>
        {/* 应用规模 */}
        <Tile label="应用" icon={<AppWindow className="size-3.5" />} className="col-span-2 md:col-span-1">
          {loading ? (
            <Skeleton className="mt-1.5 h-8 w-12" />
          ) : (
            <div className="mt-1 text-[28px] leading-9 font-semibold tracking-tight tabular-nums">{counts.total}</div>
          )}
          <div className="mt-2 flex h-1.5 overflow-hidden rounded-full bg-muted" role="img" aria-label={`已启用 ${counts.enabled} 个`}>
            <div className="h-full bg-emerald-500 transition-[width]" style={{ width: `${enabledShare}%` }} />
          </div>
          <div className="mt-1.5 flex items-center gap-3 text-[11px] text-muted-foreground tabular-nums">
            <span className="flex items-center gap-1"><span className="size-1.5 rounded-full bg-emerald-500" />启用 {counts.enabled}</span>
            <span className="flex items-center gap-1"><span className="size-1.5 rounded-full bg-zinc-300 dark:bg-zinc-600" />停用 {counts.disabled}</span>
          </div>
        </Tile>

        {hasMetrics ? (
          <>
            <Tile label="用户总数" icon={<Users2 className="size-3.5" />} className="border-t md:border-t-0 md:border-l">
              <BigNumber loading={metricsLoading} value={formatCount(summary?.totalUsers)} />
              <Hint loading={metricsLoading}>全部应用合计</Hint>
            </Tile>
            <Tile label="今日新增" icon={<UserPlus className="size-3.5" />} className="border-t border-l md:border-t-0">
              <BigNumber
                loading={metricsLoading}
                value={summary?.newUsersToday ? `+${formatCount(summary.newUsersToday)}` : "0"}
                tone={summary?.newUsersToday ? "positive" : undefined}
              />
              <Hint loading={metricsLoading}>自零点起注册</Hint>
            </Tile>
            <Tile label="今日登录" icon={<LogIn className="size-3.5" />} className="col-span-2 border-t md:col-span-1 md:border-t-0 md:border-l">
              <BigNumber loading={metricsLoading} value={formatCount(summary?.loginsToday)} />
              <Hint loading={metricsLoading}>
                {successRate == null ? "暂无登录尝试" : `成功率 ${successRate.toFixed(1)}%，失败 ${formatCount(loginFailuresToday)} 次`}
              </Hint>
            </Tile>
          </>
        ) : (
          <Tile label="完全开放" icon={<LogIn className="size-3.5" />} className="border-t md:border-t-0 md:border-l">
            {loading ? (
              <Skeleton className="mt-1.5 h-8 w-20" />
            ) : (
              <div className="mt-1 text-[28px] leading-9 font-semibold tracking-tight tabular-nums">
                {counts.fullyOpen}
                <span className="ml-1 text-sm font-normal text-muted-foreground">/ {counts.total}</span>
              </div>
            )}
            <Hint loading={loading}>服务、注册、登录全部开放</Hint>
          </Tile>
        )}
      </div>

      {/* 需要关注 */}
      <div className="flex flex-col gap-2 border-t bg-muted/30 px-4 py-3 sm:flex-row sm:items-center">
        <span className="shrink-0 text-xs font-medium text-muted-foreground">需要关注</span>
        {loading || metricsLoading ? (
          <div className="flex gap-2">
            <Skeleton className="h-7 w-28 rounded-full" />
            <Skeleton className="h-7 w-28 rounded-full" />
          </div>
        ) : attention.length === 0 ? (
          <span className="flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-300">
            <CircleCheck className="size-3.5" />
            全部应用运行正常
          </span>
        ) : (
          <ul className="flex flex-wrap gap-1.5">
            {attention.map((item) => {
              const body = (
                <>
                  <span className={cn("grid size-5 shrink-0 place-items-center rounded-full", item.tone === "danger" ? "bg-red-500/10 text-red-600 dark:text-red-400" : "bg-amber-500/10 text-amber-600 dark:text-amber-400")}>
                    {item.icon}
                  </span>
                  {item.label}
                  <span className="font-semibold tabular-nums">{item.value}</span>
                  {item.href ? <ArrowUpRight className="size-3 text-muted-foreground" /> : null}
                </>
              );
              const cls = "inline-flex h-7 items-center gap-1.5 rounded-full bg-background py-1 pr-2.5 pl-1 text-xs ring-1 ring-border transition-colors hover:bg-accent";
              return (
                <li key={item.key}>
                  {item.href ? (
                    <Link href={item.href} className={cls}>{body}</Link>
                  ) : (
                    <button type="button" className={cls} onClick={() => item.filter && onFilter(item.filter)}>{body}</button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </section>
  );
}

function Tile({ label, icon, className, children }: { label: string; icon: ReactNode; className?: string; children: ReactNode }) {
  return (
    <div className={cn("min-w-0 p-4", className)}>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {icon}
        {label}
      </div>
      {children}
    </div>
  );
}

function BigNumber({ value, loading, tone }: { value: string; loading: boolean; tone?: "positive" }) {
  if (loading) return <Skeleton className="mt-1.5 h-8 w-20" />;
  return (
    <div
      className={cn(
        "mt-1 truncate text-[28px] leading-9 font-semibold tracking-tight tabular-nums",
        tone === "positive" && "text-emerald-600 dark:text-emerald-400"
      )}
    >
      {value}
    </div>
  );
}

function Hint({ loading, children }: { loading: boolean; children: ReactNode }) {
  return <div className="mt-2 truncate text-[11px] text-muted-foreground">{loading ? " " : children}</div>;
}
