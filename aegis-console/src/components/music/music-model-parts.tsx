"use client";

import { Info } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { MusicModelMetrics, MusicModelRun, MusicRanker } from "@/lib/api/music";
import { cn } from "@/lib/utils";
import { METRIC_META, MODEL_STATUS, liftText, pct, rankFeatureLabel } from "./music-format";
import { EmptyLine } from "./music-kit";

/**
 * 模型指标对比、状态徽标与精排摘要：总览的「当前模型」、训练记录表与训练详情共用一份，
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

/**
 * 精排（第二期）：是否启用、没启用的原因、留出集 AUC 对比「融合分」基线、标准化后的特征权重。
 * 权重条以中线为零点，正向（推高点击概率）向右标绿，负向向左标红，按绝对值从大到小排。
 */
export function RankerSummary({ ranker }: { ranker: MusicRanker }) {
  if (!ranker) {
    return <EmptyLine>这个版本没有训练精排（旧版本快照，或精排开关关闭）</EmptyLine>;
  }
  const better = ranker.auc > ranker.baselineAuc;
  const features = [...ranker.features].sort((a, b) => Math.abs(b.weight) - Math.abs(a.weight));
  const maxAbs = Math.max(0.0001, ...features.map((f) => Math.abs(f.weight)));
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Badge variant={ranker.enabled ? "success" : "secondary"} size="sm">
          {ranker.enabled ? "已启用" : "未启用"}
        </Badge>
        {!ranker.enabled && ranker.reason ? <span className="text-xs text-muted-foreground">{ranker.reason}</span> : null}
      </div>
      <dl className="grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-4">
        <RankerFigure label="样本" value={ranker.samples.toLocaleString("zh-CN")} />
        <RankerFigure label="正样本占比" value={pct(ranker.samples ? ranker.positives / ranker.samples : 0)} />
        <RankerFigure label="AUC" value={ranker.auc.toFixed(3)} tone={better ? "good" : "bad"} />
        <RankerFigure label="基线 AUC（融合分）" value={ranker.baselineAuc.toFixed(3)} />
      </dl>
      <p className="text-[11px] text-muted-foreground">
        按时间留出最后 20% 的曝光算 AUC；{better ? `比基线高 ${(ranker.auc - ranker.baselineAuc).toFixed(3)}` : "不高于基线时不启用"}。
      </p>
      {features.length ? (
        <ul className="flex min-w-0 flex-col gap-1.5">
          {features.map((f) => {
            const width = (Math.abs(f.weight) / maxAbs) * 50;
            return (
              <li key={f.name} className="grid min-w-0 grid-cols-[7.5rem_minmax(0,1fr)_3.5rem] items-center gap-3 text-xs">
                <span className="truncate" title={f.name}>
                  {rankFeatureLabel(f.name)}
                </span>
                <span className="relative h-2.5 min-w-0 rounded-full bg-muted">
                  <span aria-hidden className="absolute inset-y-0 left-1/2 w-px bg-border" />
                  <span
                    className={cn("absolute inset-y-0 rounded-full", f.weight >= 0 ? "bg-emerald-500" : "bg-rose-500")}
                    style={f.weight >= 0 ? { left: "50%", width: `${width}%` } : { right: "50%", width: `${width}%` }}
                  />
                </span>
                <span className={cn("text-right tabular-nums", f.weight >= 0 ? "text-emerald-600 dark:text-emerald-400" : "text-rose-600 dark:text-rose-400")}>
                  {f.weight >= 0 ? "+" : "−"}
                  {Math.abs(f.weight).toFixed(3)}
                </span>
              </li>
            );
          })}
        </ul>
      ) : null}
      <p className="text-[11px] text-muted-foreground">展示位置只在训练时用来消除位置偏差，线上打分时置 0。</p>
    </div>
  );
}

function RankerFigure({ label, value, tone }: { label: string; value: string; tone?: "good" | "bad" }) {
  return (
    <div className="min-w-0 rounded-xl border border-border p-2.5">
      <dt className="truncate text-[11px] text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          "mt-0.5 truncate text-base font-semibold tabular-nums",
          tone === "good" && "text-emerald-600 dark:text-emerald-400",
          tone === "bad" && "text-rose-600 dark:text-rose-400"
        )}
      >
        {value}
      </dd>
    </div>
  );
}
