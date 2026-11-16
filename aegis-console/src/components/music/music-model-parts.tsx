"use client";

import { Info } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { MusicModelMetrics, MusicModelRun } from "@/lib/api/music";
import { cn } from "@/lib/utils";
import { METRIC_META, MODEL_STATUS, liftText, pct } from "./music-format";

/**
 * 模型指标对比与状态徽标：总览的「当前模型」、训练记录表与训练详情共用一份，
 * 同一个版本在三处的数字和叫法必须一致。
 */

export function ModelStatusBadge({ run }: { run: Pick<MusicModelRun, "status"> }) {
  const meta = MODEL_STATUS[run.status] ?? { label: run.status, tone: "secondary" as const };
  return (
    <Badge variant={meta.tone} size="sm">
      {meta.label}
    </Badge>
  );
}

/** 指标 vs 热度基线：每项一行，模型值、基线值、提升幅度与对比条 */
export function ModelMetricsCompare({
  metrics,
  className
}: {
  metrics: MusicModelMetrics & { baseline: MusicModelMetrics; testUsers: number };
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-3", className)}>
      <ul className="flex min-w-0 flex-col gap-3">
        {METRIC_META.map((meta) => {
          const value = metrics[meta.key] ?? 0;
          const base = metrics.baseline?.[meta.key] ?? 0;
          const max = Math.max(value, base, 0.0001);
          const better = value >= base;
          return (
            <li key={meta.key} className="flex min-w-0 flex-col gap-1.5">
              <div className="flex min-w-0 items-center gap-2 text-xs">
                <span className="font-medium">{meta.label}</span>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="size-3 text-muted-foreground" aria-label={meta.note} />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-64">{meta.note}</TooltipContent>
                </Tooltip>
                <span className="ml-auto tabular-nums">{pct(value)}</span>
                <span className="text-muted-foreground tabular-nums">基线 {pct(base)}</span>
                <span
                  className={cn(
                    "w-16 text-right font-medium tabular-nums",
                    better ? "text-emerald-600 dark:text-emerald-400" : "text-rose-600 dark:text-rose-400"
                  )}
                >
                  {liftText(value, base)}
                </span>
              </div>
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="h-1.5 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-violet-500" style={{ width: `${(value / max) * 100}%` }} />
                </span>
                <span className="h-1 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-zinc-400" style={{ width: `${(base / max) * 100}%` }} />
                </span>
              </div>
            </li>
          );
        })}
      </ul>
      <p className="text-[11px] text-muted-foreground">
        按每人最后 20% 的正反馈留出测试，评估 {metrics.testUsers.toLocaleString("zh-CN")} 位用户；基线为「只推近期热门」。
      </p>
    </div>
  );
}
