"use client";

import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { AdminScope } from "./admin-shared";

/**
 * 概览带，同时是筛选器：点「停用 2」就是只看这 2 个。
 *
 * 「在线」「认证异常」两格依赖超管专属接口，非超管不渲染，
 * 而不是显示一个永远为 0 的数字。
 */

export type AdminOverviewCounts = Record<AdminScope, number>;

type Segment = { key: AdminScope; label: string; dot?: string; tone?: "danger" };

export function AdminOverview({
  counts,
  scope,
  onScopeChange,
  loading,
  showSessionMetrics
}: {
  counts: AdminOverviewCounts;
  scope: AdminScope;
  onScopeChange: (next: AdminScope) => void;
  loading: boolean;
  showSessionMetrics: boolean;
}) {
  const segments: Segment[] = [
    { key: "all", label: "全部管理员" },
    { key: "active", label: "正常", dot: "bg-emerald-500" },
    { key: "disabled", label: "已停用", dot: "bg-zinc-400" },
    { key: "super", label: "超级管理员", dot: "bg-sky-500" }
  ];
  if (showSessionMetrics) {
    segments.push(
      { key: "online", label: "当前在线", dot: "bg-emerald-500" },
      { key: "broken", label: "认证异常", dot: "bg-red-500", tone: "danger" }
    );
  }

  const activeShare = counts.all > 0 ? (counts.active / counts.all) * 100 : 0;

  return (
    <div className="overflow-hidden rounded-2xl border bg-card text-card-foreground">
      <div
        className={cn(
          "grid grid-cols-2 gap-1 p-1.5 sm:grid-cols-3",
          showSessionMetrics ? "lg:grid-cols-6" : "lg:grid-cols-4"
        )}
      >
        {segments.map((segment) => {
          const active = scope === segment.key;
          const value = counts[segment.key];
          const alert = segment.tone === "danger" && value > 0;
          return (
            <button
              key={segment.key}
              type="button"
              aria-pressed={active}
              onClick={() => onScopeChange(active && segment.key !== "all" ? "all" : segment.key)}
              className={cn(
                "rounded-xl px-3 py-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                active ? "bg-muted" : "hover:bg-muted/50"
              )}
            >
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {segment.dot ? <span className={cn("size-1.5 shrink-0 rounded-full", segment.dot)} /> : null}
                <span className="truncate">{segment.label}</span>
              </span>
              {loading ? (
                <Skeleton className="mt-1.5 h-7 w-12" />
              ) : (
                <span
                  className={cn(
                    "mt-1 block text-2xl leading-8 font-semibold tracking-tight tabular-nums",
                    alert && "text-red-600 dark:text-red-400"
                  )}
                >
                  {value.toLocaleString("zh-CN")}
                </span>
              )}
            </button>
          );
        })}
      </div>
      <div className="border-t px-4 py-2.5">
        {loading ? (
          <Skeleton className="h-1.5 w-full rounded-full" />
        ) : (
          <div
            className="flex h-1.5 overflow-hidden rounded-full bg-muted"
            role="img"
            aria-label={`正常账号占 ${activeShare.toFixed(0)}%`}
          >
            <div className="h-full bg-emerald-500" style={{ width: `${activeShare}%` }} />
            <div className="h-full bg-zinc-400" style={{ width: `${counts.all > 0 ? 100 - activeShare : 0}%` }} />
          </div>
        )}
        <div className="mt-1.5 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-[11px] text-muted-foreground tabular-nums">
          <span>{loading ? " " : counts.all > 0 ? `正常账号占比 ${activeShare.toFixed(0)}%` : "暂无管理员"}</span>
          {showSessionMetrics && !loading ? <span>在线状态每 15 秒刷新</span> : null}
        </div>
      </div>
    </div>
  );
}
