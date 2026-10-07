"use client";

import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Inbox, Loader2 } from "lucide-react";
import type { AuditLog } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  AdminAvatar,
  DATA_FONT,
  ModuleBadge,
  RiskBadge,
  StatusBadge,
  TruncatedText,
  dayKey,
  dayLabel,
  formatAbsolute,
  formatLatency,
  formatRelative,
  isFailure,
  roleLabel,
  targetText
} from "./audit-shared";

// 时间、操作者、操作、结果、风险、来源、耗时
const GRID = "grid grid-cols-[96px_minmax(140px,1fr)_minmax(220px,2.2fr)_minmax(150px,1.4fr)_72px_minmax(150px,1.2fr)_64px] items-center gap-3";

/**
 * 审计列表。每行读作一句话：谁、做了什么、对什么、结果如何、从哪儿。
 * 方法、路径、状态码这类排查字段放进详情，不占列表。按日期分组，滚到底自动加载下一页。
 */
export function AuditList({
  logs,
  total,
  loading,
  error,
  hasMore,
  loadingMore,
  hasFilters,
  selectedId,
  onLoadMore,
  onOpen,
  onRetry,
  onClearFilters
}: {
  logs: AuditLog[];
  total: number;
  loading: boolean;
  error?: string | null;
  hasMore: boolean;
  loadingMore: boolean;
  hasFilters: boolean;
  selectedId?: number | null;
  onLoadMore: () => void;
  onOpen: (log: AuditLog) => void;
  onRetry: () => void;
  onClearFilters: () => void;
}) {
  // 相对时间每分钟刷新一次
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(timer);
  }, []);

  const groups = useMemo(() => {
    const out: Array<{ key: string; items: AuditLog[] }> = [];
    for (const log of logs) {
      const key = dayKey(log.createdAt);
      const last = out[out.length - 1];
      if (last && last.key === key) last.items.push(log);
      else out.push({ key, items: [log] });
    }
    return out;
  }, [logs]);

  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = sentinel.current;
    if (!node || !hasMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && !loadingMore) onLoadMore();
      },
      { rootMargin: "320px" }
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [hasMore, loadingMore, onLoadMore]);

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="overflow-x-auto">
        <div className="min-w-[1040px]">
          <div className={cn(GRID, "border-b bg-muted/30 px-4 py-2 text-[11px] font-medium text-muted-foreground")}>
            <span>时间</span>
            <span>操作者</span>
            <span>操作</span>
            <span>结果</span>
            <span>风险</span>
            <span>来源</span>
            <span className="text-right">耗时</span>
          </div>

          {loading ? (
            <ListSkeleton />
          ) : error ? (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <AlertTriangle className="size-6 text-amber-500" />
              <div className="space-y-1">
                <p className="text-sm font-medium">审计日志加载失败</p>
                <p className="text-xs text-muted-foreground">{error}</p>
              </div>
              <Button variant="outline" size="sm" onClick={onRetry}>
                重试
              </Button>
            </div>
          ) : logs.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <Inbox className="size-6 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">{hasFilters ? "没有符合筛选条件的记录" : "暂无审计记录"}</p>
              {hasFilters ? (
                <Button variant="outline" size="sm" onClick={onClearFilters}>
                  清除筛选
                </Button>
              ) : null}
            </div>
          ) : (
            <>
              {groups.map((group) => (
                <Fragment key={group.key}>
                  <div className="sticky top-0 z-[1] flex items-center justify-between border-b bg-card px-4 py-1.5 text-xs font-medium">
                    <span>{dayLabel(group.key, now)}</span>
                    <span className="text-[11px] font-normal text-muted-foreground">{group.items.length} 条</span>
                  </div>
                  {group.items.map((log) => (
                    <AuditRow key={log.id} log={log} now={now} selected={selectedId === log.id} onOpen={() => onOpen(log)} />
                  ))}
                </Fragment>
              ))}
              <div ref={sentinel} className="flex items-center justify-center gap-2 px-4 py-3 text-xs text-muted-foreground">
                {loadingMore ? (
                  <>
                    <Loader2 className="size-3.5 animate-spin" />
                    正在加载
                  </>
                ) : hasMore ? (
                  <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={onLoadMore}>
                    加载更多
                  </Button>
                ) : (
                  <span>已显示全部 {total} 条</span>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function AuditRow({ log, now, selected, onOpen }: { log: AuditLog; now: number; selected: boolean; onOpen: () => void }) {
  const failed = isFailure(log);
  const target = targetText(log);
  const source = [log.browser, log.os].filter(Boolean).join(" / ");
  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        GRID,
        "w-full border-b px-4 py-2.5 text-left text-sm transition-colors last:border-b-0 hover:bg-muted/40",
        selected && "bg-muted/60",
        failed && "bg-red-50/40 dark:bg-red-950/10"
      )}
    >
      <Tooltip>
        <TooltipTrigger asChild>
          <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{formatRelative(log.createdAt, now)}</span>
        </TooltipTrigger>
        <TooltipContent>{formatAbsolute(log.createdAt)}</TooltipContent>
      </Tooltip>

      <span className="flex min-w-0 items-center gap-2">
        <AdminAvatar name={log.adminName} />
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{log.adminName || "未知管理员"}</span>
          {log.adminRole ? <span className="block truncate text-[11px] text-muted-foreground">{roleLabel(log.adminRole)}</span> : null}
        </span>
      </span>

      <span className="min-w-0">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate font-medium">{log.operationName}</span>
          <ModuleBadge label={log.moduleLabel} />
        </span>
        {target || log.appName ? (
          <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
            {target ? <span className="truncate">{target}</span> : null}
            {log.appName ? <span className="shrink-0">应用「{log.appName}」</span> : null}
          </span>
        ) : null}
      </span>

      <span className="min-w-0">
        <StatusBadge status={log.status} />
        {failed && log.errorMessage ? (
          <TruncatedText text={log.errorMessage} className="mt-0.5 text-xs text-red-600 dark:text-red-400" />
        ) : null}
      </span>

      <span>
        <RiskBadge severity={log.severity} />
      </span>

      <span className="min-w-0">
        <span className="block truncate text-xs">
          <span className={DATA_FONT}>{log.ip || "—"}</span>
          {log.location ? <span className="ml-1.5 text-muted-foreground">{log.location}</span> : null}
        </span>
        {source ? <span className="block truncate text-[11px] text-muted-foreground">{source}</span> : null}
      </span>

      <span className={cn("text-right text-xs text-muted-foreground", DATA_FONT, (log.latencyMs ?? 0) >= 3000 && "text-amber-600 dark:text-amber-400")}>
        {formatLatency(log.latencyMs)}
      </span>
    </button>
  );
}

function ListSkeleton() {
  return (
    <div className="divide-y">
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className={cn(GRID, "px-4 py-3")}>
          <Skeleton className="h-3.5 w-14" />
          <div className="flex items-center gap-2">
            <Skeleton className="size-7 rounded-full" />
            <div className="space-y-1.5">
              <Skeleton className="h-3.5 w-20" />
              <Skeleton className="h-3 w-14" />
            </div>
          </div>
          <div className="space-y-1.5">
            <Skeleton className="h-3.5 w-36" />
            <Skeleton className="h-3 w-48" />
          </div>
          <Skeleton className="h-4 w-12 rounded-full" />
          <span />
          <div className="space-y-1.5">
            <Skeleton className="h-3.5 w-28" />
            <Skeleton className="h-3 w-20" />
          </div>
          <Skeleton className="ml-auto h-3.5 w-10" />
        </div>
      ))}
    </div>
  );
}
