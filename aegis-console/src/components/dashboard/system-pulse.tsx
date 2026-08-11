"use client";

import Link from "next/link";
import { ArrowRight, ServerCog } from "lucide-react";
import { useSystemMonitorQuery } from "@/lib/admin-hooks";
import type { MonitorStatus } from "@/lib/api/types";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  状态映射（与 availability-dashboard 保持同一套配色）                   */
/* ------------------------------------------------------------------ */

const statusLabel: Record<MonitorStatus, string> = {
  available: "运行正常",
  degraded: "部分降级",
  unavailable: "存在故障",
};

const statusTone: Record<MonitorStatus, { text: string; ring: string; dot: string }> = {
  available:   { text: "text-emerald-600 dark:text-emerald-400", ring: "stroke-emerald-500", dot: "bg-emerald-500" },
  degraded:    { text: "text-amber-600 dark:text-amber-400",     ring: "stroke-amber-500",   dot: "bg-amber-500" },
  unavailable: { text: "text-red-600 dark:text-red-400",         ring: "stroke-red-500",     dot: "bg-red-500" },
};

function fmtCheckedAt(iso?: string): string {
  if (!iso) return "--";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "--";
  return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

function fmtUptime(seconds?: number): string {
  if (!seconds || seconds < 0) return "--";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d} 天 ${h} 小时`;
  if (h > 0) return `${h} 小时 ${m} 分`;
  return `${m} 分钟`;
}

/* ------------------------------------------------------------------ */
/*  组件                                                                */
/* ------------------------------------------------------------------ */

/**
 * 系统脉搏 —— 首屏的健康度速览。
 *
 * 数据来自 `/api/system/monitor`（后端要求超管），因此调用方需自行判定权限后再挂载。
 * 详细的组件级下钻仍由下方 `AvailabilityDashboard` 承担，这里只回答「现在有没有事」。
 */
export function SystemPulse() {
  const query = useSystemMonitorQuery();
  const data = query.data;

  const status: MonitorStatus = data?.status || "available";
  const tone = statusTone[status];
  const score = Math.max(0, Math.min(100, Math.round(data?.score ?? 0)));

  // 半径 34 的圆环周长，用 dasharray 表达完成度
  const circumference = 2 * Math.PI * 34;

  return (
    <Card className="h-full py-0">
      <CardContent className="flex h-full flex-col gap-4 p-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <ServerCog className="size-4 text-muted-foreground" />
            系统脉搏
          </h3>
          <Link
            href="/status"
            className="inline-flex items-center gap-1 text-[10px] text-muted-foreground transition-colors hover:text-foreground"
          >
            状态页 <ArrowRight className="size-3" />
          </Link>
        </div>

        {query.isLoading ? (
          <div className="flex items-center gap-4">
            <Skeleton className="size-20 rounded-full" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-3 w-32" />
            </div>
          </div>
        ) : !data ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-1.5 py-4 text-center">
            <ServerCog className="size-7 text-muted-foreground/30" />
            <p className="text-xs text-muted-foreground">监测数据不可用</p>
            <p className="text-[10px] text-muted-foreground/70">接口未返回状态数据</p>
          </div>
        ) : (
          <>
            <div className="flex items-center gap-4">
              {/* 健康分环 */}
              <div className="relative shrink-0">
                <svg viewBox="0 0 80 80" className="size-20 -rotate-90">
                  <circle cx="40" cy="40" r="34" fill="none" strokeWidth="6" className="stroke-muted" />
                  <circle
                    cx="40"
                    cy="40"
                    r="34"
                    fill="none"
                    strokeWidth="6"
                    strokeLinecap="round"
                    className={cn("transition-[stroke-dasharray] duration-500", tone.ring)}
                    strokeDasharray={`${(score / 100) * circumference} ${circumference}`}
                  />
                </svg>
                <div className="absolute inset-0 flex flex-col items-center justify-center">
                  <span className="font-data text-lg leading-none font-semibold tabular-nums text-foreground">{score}</span>
                  <span className="text-[9px] text-muted-foreground">健康分</span>
                </div>
              </div>

              <div className="min-w-0 flex-1 space-y-1.5">
                <p className={cn("flex items-center gap-1.5 text-sm font-medium", tone.text)}>
                  <span className={cn("size-1.5 shrink-0 rounded-full", tone.dot)} />
                  {statusLabel[status]}
                </p>
                <p className="text-xs text-muted-foreground">
                  可用率 <span className="font-data tabular-nums text-foreground">{(data.availabilityRate ?? 0).toFixed(2)}%</span>
                </p>
                {/* 组件计数下面那一排已经写了，这里改报检测时间，避免重复同一句话 */}
                <p className="text-[11px] text-muted-foreground/80" title={data.summary}>
                  检测于 {fmtCheckedAt(data.checkedAt)}
                </p>
              </div>
            </div>

            {/* 组件计数 */}
            <div className="grid grid-cols-3 gap-px overflow-hidden rounded-lg border bg-border">
              <CountCell label="正常" value={data.counts?.available} dot="bg-emerald-500" />
              <CountCell label="降级" value={data.counts?.degraded} dot="bg-amber-500" />
              <CountCell label="故障" value={data.counts?.unavailable} dot="bg-red-500" />
            </div>

            <div className="mt-auto flex items-center justify-between border-t pt-2.5 text-[11px] text-muted-foreground">
              <span>已运行 {fmtUptime(data.runtime?.uptimeSeconds)}</span>
              <span className="font-data">{data.runtime?.environment || "--"}</span>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function CountCell({ label, value, dot }: { label: string; value?: number; dot: string }) {
  return (
    <div className="flex flex-col items-center gap-0.5 bg-card px-2 py-2">
      <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
        <span className={cn("size-1.5 rounded-full", dot)} />
        {label}
      </span>
      <span className="font-data text-sm font-semibold tabular-nums text-foreground">{value ?? 0}</span>
    </div>
  );
}
