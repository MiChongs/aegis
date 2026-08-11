"use client";

import Link from "next/link";
import { ArrowRight, BarChart3, PlugZap, ScrollText, Users2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { appSectionHref } from "@/lib/app-sections";
import { cn } from "@/lib/utils";
import type { AppSummary } from "@/lib/api/types";
import type { PlatformAppOverviewItem } from "@/lib/api/platform-governance";
import { AppRowActions } from "@/components/apps/app-row-actions";
import {
  AppKeyText,
  AppStatusBadges,
  AppTile,
  formatAppDate,
  formatCount
} from "@/components/apps/app-shared";

/**
 * 列表页的两种视图。
 *
 * 指标（用户数 / 今日新增 / 今日登录）来自治理总览接口，它一次返回全部应用的聚合值；
 * 没有 `platform:app:read` 的管理员拿不到，此时整块指标区不渲染而不是显示一排 0 ——
 * 「查不到」和「真的是 0」必须能区分开。
 */

export type AppMetrics = Pick<
  PlatformAppOverviewItem,
  "totalUsers" | "newUsersToday" | "loginSuccessToday" | "state"
>;

export type AppListRow = {
  app: AppSummary;
  metrics?: AppMetrics;
};

/* ── 卡片视图 ── */

export function AppCardGrid({
  rows,
  onDelete,
  hasMetrics
}: {
  rows: AppListRow[];
  onDelete: (app: AppSummary) => void;
  hasMetrics: boolean;
}) {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {rows.map(({ app, metrics }) => (
        <article
          key={app.appKey}
          className={cn(
            "group relative flex flex-col gap-3 rounded-2xl border border-border bg-card p-4 transition-colors",
            "hover:border-foreground/20",
            !app.status && "border-dashed"
          )}
          style={{ boxShadow: "var(--shadow-soft)" }}
        >
          <div className="flex items-start gap-3">
            <AppTile name={app.name} seed={app.appKey} />
            <div className="min-w-0 flex-1 space-y-1">
              {/* 覆盖整卡的链接：卡片任意空白处都能点进去，按钮与复制图标靠 z-10 浮在其上 */}
              <Link href={appSectionHref(app.appKey)} className="before:absolute before:inset-0 before:rounded-2xl">
                <h3 className="truncate text-sm font-semibold tracking-tight">{app.name}</h3>
              </Link>
              <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <span className="font-mono">#{app.id}</span>
                <span className="text-border">·</span>
                <span>{formatAppDate(app.createdAt, false)} 创建</span>
              </div>
            </div>
            <div className="relative z-10">
              <AppRowActions app={app} onDelete={onDelete} />
            </div>
          </div>

          <AppStatusBadges app={app} governance={metrics?.state} />

          <div className="relative z-10 w-fit max-w-full">
            <AppKeyText appKey={app.appKey} />
          </div>

          {hasMetrics && (
            <div className="grid grid-cols-3 gap-2 rounded-xl bg-muted/60 p-2">
              <CardMetric label="用户" value={metrics?.totalUsers} />
              <CardMetric label="今日新增" value={metrics?.newUsersToday} />
              <CardMetric label="今日登录" value={metrics?.loginSuccessToday} />
            </div>
          )}

          <div className="relative z-10 mt-auto flex items-center justify-between gap-2 border-t border-border/70 pt-3">
            <div className="flex items-center gap-0.5">
              <QuickLink app={app} section="stats" label="统计" icon={BarChart3} />
              <QuickLink app={app} section="audit" label="审计" icon={ScrollText} />
              <QuickLink app={app} section="auth-protocol" label="接入" icon={PlugZap} />
              <Tooltip>
                <TooltipTrigger asChild>
                  <Link
                    href={`/users?tab=app-users&app=${encodeURIComponent(app.appKey)}`}
                    className="grid size-7 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    <Users2 className="size-3.5" />
                  </Link>
                </TooltipTrigger>
                <TooltipContent>用户</TooltipContent>
              </Tooltip>
            </div>
            <Button asChild size="sm" variant="ghost" className="h-7 gap-1 text-xs">
              <Link href={appSectionHref(app.appKey)}>
                配置
                <ArrowRight className="size-3" />
              </Link>
            </Button>
          </div>
        </article>
      ))}
    </div>
  );
}

function CardMetric({ label, value }: { label: string; value?: number }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="truncate text-sm font-semibold tabular-nums">{formatCount(value)}</div>
    </div>
  );
}

function QuickLink({
  app,
  section,
  label,
  icon: Icon
}: {
  app: AppSummary;
  section: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Link
          href={appSectionHref(app.appKey, section)}
          className="grid size-7 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Icon className="size-3.5" />
        </Link>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/* ── 表格视图 ── */

export function AppTable({
  rows,
  onDelete,
  hasMetrics
}: {
  rows: AppListRow[];
  onDelete: (app: AppSummary) => void;
  hasMetrics: boolean;
}) {
  return (
    <div className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="min-w-56">应用</TableHead>
              <TableHead className="min-w-40">状态</TableHead>
              {hasMetrics && <TableHead className="text-right">用户</TableHead>}
              {hasMetrics && <TableHead className="text-right">今日新增</TableHead>}
              {hasMetrics && <TableHead className="text-right">今日登录</TableHead>}
              <TableHead className="min-w-32">创建时间</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(({ app, metrics }) => (
              <TableRow key={app.appKey} className="group">
                <TableCell>
                  <div className="flex items-center gap-2.5">
                    <AppTile name={app.name} seed={app.appKey} size="sm" />
                    <div className="min-w-0">
                      <Link
                        href={appSectionHref(app.appKey)}
                        className="block truncate text-sm font-medium hover:underline"
                      >
                        {app.name}
                      </Link>
                      <div className="flex items-center gap-1.5">
                        <span className="font-mono text-[11px] text-muted-foreground">#{app.id}</span>
                        <AppKeyText appKey={app.appKey} className="max-w-[220px]" />
                      </div>
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <AppStatusBadges app={app} governance={metrics?.state} />
                </TableCell>
                {hasMetrics && (
                  <TableCell className="text-right text-sm tabular-nums">{formatCount(metrics?.totalUsers)}</TableCell>
                )}
                {hasMetrics && (
                  <TableCell className="text-right text-sm tabular-nums">
                    {formatCount(metrics?.newUsersToday)}
                  </TableCell>
                )}
                {hasMetrics && (
                  <TableCell className="text-right text-sm tabular-nums">
                    {formatCount(metrics?.loginSuccessToday)}
                  </TableCell>
                )}
                <TableCell className="text-xs text-muted-foreground">{formatAppDate(app.createdAt)}</TableCell>
                <TableCell>
                  <AppRowActions app={app} onDelete={onDelete} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

/* ── 骨架屏 ── */

export function AppListSkeleton({ view }: { view: "grid" | "table" }) {
  if (view === "table") {
    return (
      <div className="space-y-2 rounded-2xl border border-border bg-card p-4">
        {Array.from({ length: 5 }).map((_, index) => (
          <Skeleton key={index} className="h-12 w-full rounded-lg" />
        ))}
      </div>
    );
  }
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {Array.from({ length: 6 }).map((_, index) => (
        <Skeleton key={index} className="h-52 w-full rounded-2xl" />
      ))}
    </div>
  );
}
