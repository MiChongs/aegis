"use client";

import { useState } from "react";
import { AlertTriangle, ChevronDown, CircleCheck, CircleDashed, Timer } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import type { Trace, TraceRule, TraceStage } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

import { ActionBadge } from "./badges";
import { DECIDED_BY_LABEL, DIM_LABEL, SOURCE_LABEL, STAGE_LABEL, formatMicros } from "./labels";

/**
 * 决策轨迹：阶段瀑布 + 规则命中。决策详情与场景模拟器共用。
 *
 * 阶段按执行顺序首尾相接画成瀑布，条宽 = 本阶段耗时占总耗时的比例，
 * 起点 = 之前各阶段耗时之和；名单与特征阶段内的各数据源是并行的，按各自耗时另画一行细条。
 * 降级与出错的阶段 / 数据源标色，这是排查「为什么慢」「为什么没取到特征」的主要入口。
 */
export function TraceView({ trace }: { trace: Trace }) {
  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TraceSummary trace={trace} />
      <StageTimeline stages={trace.stages ?? []} />
      <RuleHits rules={trace.rules ?? []} />
    </div>
  );
}

function TraceSummary({ trace }: { trace: Trace }) {
  const degraded = trace.degraded ?? [];
  if (!trace.decidedBy && degraded.length === 0) return null;
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
      {trace.decidedBy ? (
        <span className="inline-flex items-center gap-1.5 text-muted-foreground">
          决定方
          <Badge variant="outline" size="sm">{DECIDED_BY_LABEL[trace.decidedBy] ?? trace.decidedBy}</Badge>
        </span>
      ) : null}
      {degraded.length > 0 ? (
        <span className="inline-flex flex-wrap items-center gap-1.5 text-muted-foreground">
          降级
          {degraded.map((d) => (
            <Badge key={d} variant="warning" size="sm">{SOURCE_LABEL[d] ?? STAGE_LABEL[d] ?? d}</Badge>
          ))}
        </span>
      ) : null}
    </div>
  );
}

/* ── 阶段瀑布 ── */

function StageTimeline({ stages }: { stages: TraceStage[] }) {
  if (stages.length === 0) {
    return <p className="rounded-lg bg-muted/40 px-3 py-6 text-center text-xs text-muted-foreground">暂无阶段记录</p>;
  }
  const total = stages.reduce((sum, s) => sum + Math.max(0, s.micros), 0) || 1;
  const rows = stages.reduce<Array<{ stage: TraceStage; start: number }>>((acc, stage) => {
    const prev = acc[acc.length - 1];
    acc.push({ stage, start: prev ? prev.start + Math.max(0, prev.stage.micros) : 0 });
    return acc;
  }, []);

  return (
    <section className="flex min-w-0 flex-col gap-2">
      <header className="flex items-center gap-2">
        <Timer className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-medium">阶段耗时</h4>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">合计 {formatMicros(total)}</span>
      </header>
      <ol className="flex min-w-0 flex-col gap-1.5">
        {rows.map(({ stage, start }, index) => (
          <StageRow key={`${stage.stage}-${index}`} stage={stage} start={start} total={total} />
        ))}
      </ol>
    </section>
  );
}

function StageRow({ stage, start, total }: { stage: TraceStage; start: number; total: number }) {
  const bad = !!stage.error;
  const warn = !bad && !!stage.degraded;
  const left = (start / total) * 100;
  const width = Math.max(0.8, (Math.max(0, stage.micros) / total) * 100);
  const sources = stage.sources ?? [];
  const sourceMax = Math.max(1, ...sources.map((s) => s.micros));

  return (
    <li
      className={cn(
        "flex min-w-0 flex-col gap-1.5 rounded-lg border px-3 py-2",
        bad ? "border-rose-500/40 bg-rose-500/5" : warn ? "border-amber-500/40 bg-amber-500/5" : "border-border bg-background/60"
      )}
    >
      <div className="flex min-w-0 items-center gap-3">
        <span className="w-20 shrink-0 truncate text-xs font-medium">{STAGE_LABEL[stage.stage] ?? stage.stage}</span>
        <div className="relative h-2.5 min-w-0 flex-1 rounded-full bg-muted">
          <span
            className={cn("absolute inset-y-0 rounded-full", bad ? "bg-rose-500" : warn ? "bg-amber-500" : "bg-primary/70")}
            style={{ left: `${Math.min(left, 99.2)}%`, width: `${Math.min(width, 100 - Math.min(left, 99.2))}%` }}
          />
        </div>
        <span className="w-16 shrink-0 text-right text-xs tabular-nums text-muted-foreground">{formatMicros(stage.micros)}</span>
        {bad ? <Badge variant="danger" size="sm">出错</Badge> : warn ? <Badge variant="warning" size="sm">降级</Badge> : null}
      </div>
      {stage.note ? <p className="pl-23 text-[11px] text-muted-foreground">{stage.note}</p> : null}
      {stage.error ? <p className="pl-23 font-mono text-[11px] break-all text-rose-600 dark:text-rose-400">{stage.error}</p> : null}
      {sources.length > 0 ? (
        <ul className="flex min-w-0 flex-col gap-1 pl-23">
          {sources.map((src) => (
            <li key={src.source} className="flex min-w-0 flex-col gap-0.5">
              <div className="flex min-w-0 items-center gap-2">
                <span className={cn("w-16 shrink-0 truncate text-[11px]", src.error ? "text-rose-600 dark:text-rose-400" : "text-muted-foreground")}>
                  {SOURCE_LABEL[src.source] ?? src.source}
                </span>
                <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className={cn("h-full rounded-full", src.error ? "bg-rose-500" : "bg-sky-500/70")}
                    style={{ width: `${Math.max(2, (src.micros / sourceMax) * 100)}%` }}
                  />
                </div>
                <span className="w-16 shrink-0 text-right text-[11px] tabular-nums text-muted-foreground">{formatMicros(src.micros)}</span>
              </div>
              {src.error ? <p className="pl-18 font-mono text-[11px] break-all text-rose-600 dark:text-rose-400">{src.error}</p> : null}
            </li>
          ))}
        </ul>
      ) : null}
    </li>
  );
}

/* ── 规则命中 ── */

function RuleHits({ rules }: { rules: TraceRule[] }) {
  const [open, setOpen] = useState(false);
  const hits = rules.filter((r) => r.hit).sort((a, b) => b.score - a.score);
  const misses = rules.filter((r) => !r.hit);
  const missErrors = misses.filter((r) => r.error).length;

  return (
    <section className="flex min-w-0 flex-col gap-2">
      <header className="flex items-center gap-2">
        <CircleCheck className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-medium">规则</h4>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          命中 {hits.length} / {rules.length}
        </span>
      </header>
      {rules.length === 0 ? (
        <p className="rounded-lg bg-muted/40 px-3 py-6 text-center text-xs text-muted-foreground">暂无规则</p>
      ) : (
        <>
          {hits.length > 0 ? (
            <ul className="flex min-w-0 flex-col gap-1.5">
              {hits.map((rule) => (
                <RuleRow key={rule.key} rule={rule} />
              ))}
            </ul>
          ) : (
            <p className="rounded-lg bg-muted/40 px-3 py-3 text-center text-xs text-muted-foreground">无命中规则</p>
          )}
          {misses.length > 0 ? (
            <Collapsible open={open} onOpenChange={setOpen}>
              <CollapsibleTrigger className="inline-flex items-center gap-1.5 rounded-md px-1 py-1 text-xs text-muted-foreground hover:text-foreground">
                <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
                未命中 {misses.length} 条
                {missErrors > 0 ? <Badge variant="danger" size="sm">出错 {missErrors}</Badge> : null}
              </CollapsibleTrigger>
              <CollapsibleContent>
                <ul className="mt-1.5 flex min-w-0 flex-col gap-1">
                  {misses.map((rule) => (
                    <RuleRow key={rule.key} rule={rule} />
                  ))}
                </ul>
              </CollapsibleContent>
            </Collapsible>
          ) : null}
        </>
      )}
    </section>
  );
}

function RuleRow({ rule }: { rule: TraceRule }) {
  const observe = rule.mode === "observe";
  return (
    <li
      className={cn(
        "flex min-w-0 flex-col gap-1 rounded-lg border px-3 py-2",
        rule.error
          ? "border-rose-500/40 bg-rose-500/5"
          : rule.hit
            ? observe
              ? "border-dashed border-sky-500/40 bg-sky-500/5"
              : "border-amber-500/40 bg-amber-500/5"
            : "border-border bg-background/40"
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        {rule.hit ? (
          <CircleCheck className={cn("size-3.5 shrink-0", observe ? "text-sky-500" : "text-amber-500")} />
        ) : rule.error ? (
          <AlertTriangle className="size-3.5 shrink-0 text-rose-500" />
        ) : (
          <CircleDashed className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className={cn("min-w-0 truncate text-sm", !rule.hit && "text-muted-foreground")}>{rule.name || rule.key}</span>
        <span className="hidden min-w-0 truncate font-mono text-[11px] text-muted-foreground sm:inline">{rule.key}</span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          {rule.dimension ? <Badge variant="outline" size="sm">{DIM_LABEL[rule.dimension] ?? rule.dimension}</Badge> : null}
          {observe ? <Badge variant="info" size="sm">观察</Badge> : null}
          {rule.hit && rule.decision ? <ActionBadge action={rule.decision} /> : null}
          <span
            className={cn(
              "w-10 text-right text-xs font-medium tabular-nums",
              rule.hit && rule.score > 0 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"
            )}
          >
            {rule.score > 0 ? `+${rule.score}` : rule.score}
          </span>
        </span>
      </div>
      {rule.hit && rule.reason ? <p className="pl-5.5 text-xs text-muted-foreground">{rule.reason}</p> : null}
      {rule.error ? <p className="pl-5.5 font-mono text-[11px] break-all text-rose-600 dark:text-rose-400">{rule.error}</p> : null}
    </li>
  );
}
