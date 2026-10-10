"use client";

import Link from "next/link";
import { ArrowRight, BarChart3, PlugZap, ScrollText, ShieldAlert, Users2 } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { appSectionHref } from "@/lib/app-sections";
import { cn } from "@/lib/utils";
import type { AppSummary } from "@/lib/api/types";
import type { PlatformAppOverviewItem } from "@/lib/api/platform-governance";
import { AppRowActions } from "@/components/apps/app-row-actions";
import {
  AppAccessIndicators,
  AppHealthPill,
  AppKeyText,
  AppTile,
  appHealth,
  formatAppDate,
  formatCount
} from "@/components/apps/app-shared";

/**
 * 列表页的两种视图。
 *
 * 指标（用户数 / 今日新增 / 今日登录）来自治理总览接口，它一次返回全部应用的聚合值；
 * 没有 `platform:app:read` 的管理员拿不到，此时整块指标区不渲染而不是显示一排 0 ——
 * 「查不到」和「真的是 0」必须能区分开。
 *
 * 表格只在 md 以上出现；窄屏一律是卡片，七列表格塞进手机只剩横向滚动。
 */

export type AppMetrics = Pick<
  PlatformAppOverviewItem,
  "totalUsers" | "newUsersToday" | "loginSuccessToday" | "loginFailureToday" | "state" | "reason" | "adminCount"
>;

export type AppListRow = {
  app: AppSummary;
  metrics?: AppMetrics;
};

const QUICK_LINKS = [
  { section: "stats", label: "统计", icon: BarChart3 },
  { section: "audit", label: "审计", icon: ScrollText },
  { section: "auth-protocol", label: "接入", icon: PlugZap }
] as const;

/* ── 卡片视图 ── */

export function AppCardGrid({
  rows,
  onDelete,
  hasMetrics,
  className
}: {
  rows: AppListRow[];
  onDelete: (app: AppSummary) => void;
  hasMetrics: boolean;
  className?: string;
}) {
  return (
    <div className={cn("grid gap-3 sm:grid-cols-2 xl:grid-cols-3", className)}>
      {rows.map((row) => (
        <AppCard key={row.app.appKey} row={row} onDelete={onDelete} hasMetrics={hasMetrics} />
      ))}
    </div>
  );
}

function AppCard({ row: { app, metrics }, onDelete, hasMetrics }: { row: AppListRow; onDelete: (app: AppSummary) => void; hasMetrics: boolean }) {
  const health = appHealth(app, metrics?.state);
  const governed = health.tone === "governed";
  const attempts = (metrics?.loginSuccessToday ?? 0) + (metrics?.loginFailureToday ?? 0);
  const successRate = attempts > 0 ? ((metrics?.loginSuccessToday ?? 0) / attempts) * 100 : null;

  return (
    <article
      className={cn(
        "group relative flex flex-col overflow-hidden rounded-2xl border bg-card transition-[border-color,box-shadow]",
        "hover:border-foreground/20 hover:shadow-md",
        governed && "border-red-300/70 dark:border-red-900/70"
      )}
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      {governed ? (
        <div className="flex items-start gap-2 border-b border-red-200/70 bg-red-50/70 px-4 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
          <ShieldAlert className="mt-px size-3.5 shrink-0" />
          <span className="line-clamp-2">
            {health.label}
            {metrics?.reason ? `：${metrics.reason}` : ""}
          </span>
        </div>
      ) : null}

      <div className="flex flex-1 flex-col gap-4 p-4">
        {/* 身份 */}
        <div className="flex items-start gap-3">
          <AppTile name={app.name} seed={app.appKey} size="lg" className={cn(!app.status && "opacity-50 grayscale")} />
          <div className="min-w-0 flex-1 pt-0.5">
            {/* 覆盖整卡的链接：空白处都能点进去，按钮与复制图标靠 z-10 浮在其上 */}
            <Link href={appSectionHref(app.appKey)} className="outline-none before:absolute before:inset-0 before:rounded-2xl focus-visible:before:ring-2 focus-visible:before:ring-ring">
              <h3 className="truncate text-[15px] font-semibold tracking-tight">{app.name}</h3>
            </Link>
            <div className="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
              <span className="font-mono">#{app.id}</span>
              <span>创建于 {formatAppDate(app.createdAt, false)}</span>
            </div>
          </div>
          <div className="relative z-10 flex items-center gap-1">
            <AppHealthPill health={health} />
            <AppRowActions app={app} onDelete={onDelete} />
          </div>
        </div>

        {/* 指标 */}
        {hasMetrics ? (
          <div className="rounded-xl border bg-muted/30">
            <div className="grid grid-cols-3 divide-x">
              <Metric label="用户" value={formatCount(metrics?.totalUsers)} />
              <Metric
                label="今日新增"
                value={metrics?.newUsersToday ? `+${formatCount(metrics.newUsersToday)}` : "0"}
                positive={Boolean(metrics?.newUsersToday)}
              />
              <Metric label="今日登录" value={formatCount(metrics?.loginSuccessToday)} />
            </div>
            <div className="flex items-center gap-2 border-t px-3 py-2">
              <span className="shrink-0 text-[11px] text-muted-foreground">登录成功率</span>
              <div className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                <div
                  className={cn(
                    "h-full rounded-full",
                    successRate == null ? "bg-transparent" : successRate >= 90 ? "bg-emerald-500" : successRate >= 70 ? "bg-amber-500" : "bg-red-500"
                  )}
                  style={{ width: `${successRate ?? 0}%` }}
                />
              </div>
              <span className="w-12 shrink-0 text-right text-[11px] font-medium tabular-nums">
                {successRate == null ? "暂无" : `${successRate.toFixed(successRate === 100 ? 0 : 1)}%`}
              </span>
            </div>
          </div>
        ) : null}

        {/* 开关与 AppKey */}
        <div className="mt-auto space-y-2.5">
          <AppAccessIndicators app={app} />
          <div className="relative z-10 flex w-fit max-w-full items-center gap-2 rounded-lg bg-muted/50 px-2 py-1">
            <span className="shrink-0 text-[11px] text-muted-foreground">AppKey</span>
            <AppKeyText appKey={app.appKey} className="min-w-0" />
          </div>
        </div>
      </div>

      {/* 快捷入口 */}
      <div className="relative z-10 flex items-center gap-0.5 border-t px-2 py-1.5">
        {QUICK_LINKS.map(({ section, label, icon: Icon }) => (
          <Link
            key={section}
            href={appSectionHref(app.appKey, section)}
            className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <Icon className="size-3.5" />
            {label}
          </Link>
        ))}
        <Link
          href={`/app-users?app=${encodeURIComponent(app.appKey)}`}
          className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Users2 className="size-3.5" />
          用户
        </Link>
        <Link
          href={appSectionHref(app.appKey)}
          className="ml-auto inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors hover:bg-accent"
        >
          配置
          <ArrowRight className="size-3 transition-transform group-hover:translate-x-0.5" />
        </Link>
      </div>
    </article>
  );
}

function Metric({ label, value, positive }: { label: string; value: string; positive?: boolean }) {
  return (
    <div className="min-w-0 px-3 py-2.5">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5 truncate text-base font-semibold tabular-nums", positive && "text-emerald-600 dark:text-emerald-400")}>
        {value}
      </div>
    </div>
  );
}

/* ── 表格视图 ── */

export function AppTable({
  rows,
  onDelete,
  hasMetrics,
  className
}: {
  rows: AppListRow[];
  onDelete: (app: AppSummary) => void;
  hasMetrics: boolean;
  className?: string;
}) {
  return (
    <div className={cn("overflow-hidden rounded-2xl border bg-card", className)} style={{ boxShadow: "var(--shadow-soft)" }}>
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/30 hover:bg-muted/30">
              <TableHead className="h-10 min-w-64 pl-4 text-xs">应用</TableHead>
              <TableHead className="h-10 text-xs">状态</TableHead>
              <TableHead className="h-10 text-xs">服务 / 注册 / 登录</TableHead>
              {hasMetrics && <TableHead className="h-10 text-right text-xs">用户</TableHead>}
              {hasMetrics && <TableHead className="h-10 text-right text-xs">今日新增</TableHead>}
              {hasMetrics && <TableHead className="h-10 text-right text-xs">今日登录</TableHead>}
              <TableHead className="hidden h-10 text-xs xl:table-cell">创建时间</TableHead>
              <TableHead className="h-10 w-12 pr-3" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(({ app, metrics }) => {
              const health = appHealth(app, metrics?.state);
              return (
                <TableRow key={app.appKey} className="group">
                  <TableCell className="py-3 pl-4">
                    <div className="flex items-center gap-3">
                      <AppTile name={app.name} seed={app.appKey} className={cn(!app.status && "opacity-50 grayscale")} />
                      <div className="min-w-0">
                        <Link href={appSectionHref(app.appKey)} className="block truncate text-sm font-medium hover:underline">
                          {app.name}
                        </Link>
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-[11px] text-muted-foreground">#{app.id}</span>
                          <AppKeyText appKey={app.appKey} className="max-w-[220px]" />
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="py-3">
                    <AppHealthPill health={health} />
                    {health.tone === "governed" && metrics?.reason ? (
                      <div className="mt-1 max-w-48 truncate text-[11px] text-muted-foreground" title={metrics.reason}>
                        {metrics.reason}
                      </div>
                    ) : null}
                  </TableCell>
                  <TableCell className="py-3">
                    <AppAccessIndicators app={app} compact />
                  </TableCell>
                  {hasMetrics && (
                    <TableCell className="py-3 text-right text-sm font-medium tabular-nums">{formatCount(metrics?.totalUsers)}</TableCell>
                  )}
                  {hasMetrics && (
                    <TableCell
                      className={cn(
                        "py-3 text-right text-sm tabular-nums",
                        metrics?.newUsersToday ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground"
                      )}
                    >
                      {metrics?.newUsersToday ? `+${formatCount(metrics.newUsersToday)}` : "0"}
                    </TableCell>
                  )}
                  {hasMetrics && (
                    <TableCell className="py-3 text-right">
                      <div className="text-sm tabular-nums">{formatCount(metrics?.loginSuccessToday)}</div>
                      {metrics?.loginFailureToday ? (
                        <div className="text-[11px] text-red-600 tabular-nums dark:text-red-400">失败 {formatCount(metrics.loginFailureToday)}</div>
                      ) : null}
                    </TableCell>
                  )}
                  <TableCell className="hidden py-3 text-xs text-muted-foreground tabular-nums xl:table-cell">
                    {formatAppDate(app.createdAt)}
                  </TableCell>
                  <TableCell className="py-3 pr-3">
                    <AppRowActions app={app} onDelete={onDelete} />
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

/* ── 骨架屏 ── */

function CardSkeleton({ hasMetrics }: { hasMetrics: boolean }) {
  return (
    <div className="overflow-hidden rounded-2xl border bg-card">
      <div className="space-y-4 p-4">
        <div className="flex items-start gap-3">
          <Skeleton className="size-12 rounded-2xl" />
          <div className="flex-1 space-y-2 pt-1">
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-3 w-36" />
          </div>
          <Skeleton className="h-5 w-14 rounded-full" />
        </div>
        {hasMetrics ? <Skeleton className="h-[92px] w-full rounded-xl" /> : null}
        <div className="flex gap-1.5">
          <Skeleton className="h-6 w-16 rounded-md" />
          <Skeleton className="h-6 w-16 rounded-md" />
          <Skeleton className="h-6 w-16 rounded-md" />
        </div>
        <Skeleton className="h-7 w-48 rounded-lg" />
      </div>
      <div className="flex gap-2 border-t px-3 py-2.5">
        <Skeleton className="h-5 w-12" />
        <Skeleton className="h-5 w-12" />
        <Skeleton className="h-5 w-12" />
        <Skeleton className="ml-auto h-5 w-12" />
      </div>
    </div>
  );
}

export function AppListSkeleton({ view, hasMetrics = true }: { view: "grid" | "table"; hasMetrics?: boolean }) {
  const grid = (
    <div className={cn("grid gap-3 sm:grid-cols-2 xl:grid-cols-3", view === "table" && "md:hidden")}>
      {Array.from({ length: 6 }).map((_, index) => (
        <CardSkeleton key={index} hasMetrics={hasMetrics} />
      ))}
    </div>
  );
  if (view === "grid") return grid;
  return (
    <>
      {grid}
      <div className="hidden overflow-hidden rounded-2xl border bg-card md:block">
        <div className="h-10 border-b bg-muted/30" />
        {Array.from({ length: 6 }).map((_, index) => (
          <div key={index} className="flex items-center gap-6 border-b px-4 py-3 last:border-b-0">
            <div className="flex w-64 items-center gap-3">
              <Skeleton className="size-10 rounded-xl" />
              <div className="space-y-1.5">
                <Skeleton className="h-3.5 w-28" />
                <Skeleton className="h-3 w-40" />
              </div>
            </div>
            <Skeleton className="h-5 w-16 rounded-full" />
            <Skeleton className="h-6 w-24" />
            <Skeleton className="ml-auto h-4 w-16" />
            <Skeleton className="h-4 w-12" />
            <Skeleton className="size-8 rounded-md" />
          </div>
        ))}
      </div>
    </>
  );
}
