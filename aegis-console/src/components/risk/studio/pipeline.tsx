"use client";

import { Fragment } from "react";
import { ChevronRight, type LucideIcon } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * 评估管线示意：一次请求依次经过的环节，分「准备数据 → 做出判断 → 执行处置」三段。
 *
 * 每个节点写着这一步的实时摘要与问题数，点击切换下方的编辑面板。
 * 用普通排版而不是画布：文字多少都能撑开节点，窄屏时整条横向滑动。
 */

export type PipelineStage = {
  key: string;
  title: string;
  icon: LucideIcon;
  summary: string[];
  issues: number;
  /** 这一步没有任何配置（虚线框） */
  idle?: boolean;
  /** 所属阶段：准备 / 判断 / 执行 */
  phase?: "prepare" | "decide" | "act";
};

const PHASES: Array<{ key: NonNullable<PipelineStage["phase"]>; title: string }> = [
  { key: "prepare", title: "准备数据" },
  { key: "decide", title: "做出判断" },
  { key: "act", title: "执行处置" },
];

function StageNode({ stage, selected, onSelect }: { stage: PipelineStage; selected: boolean; onSelect: () => void }) {
  const Icon = stage.icon;
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={selected ? "step" : undefined}
      className={cn(
        "group relative flex w-36 shrink-0 flex-col gap-1.5 rounded-xl border bg-card px-3 py-2.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/40 sm:w-40",
        selected ? "border-foreground/60 ring-1 ring-foreground/20" : "hover:border-foreground/30 hover:bg-accent/30",
        stage.idle && !selected && "border-dashed bg-transparent",
        stage.issues > 0 && !selected && "border-red-300 dark:border-red-900",
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        <span className={cn("grid size-6 shrink-0 place-items-center rounded-md", selected ? "bg-foreground text-background" : "bg-muted text-muted-foreground")}>
          <Icon className="size-3.5" />
        </span>
        <span className="min-w-0 flex-1 truncate text-xs font-semibold">{stage.title}</span>
        {stage.issues > 0 ? (
          <span className="grid h-4 min-w-4 place-items-center rounded-full bg-destructive px-1 text-[10px] leading-none font-semibold text-white tabular-nums">{stage.issues}</span>
        ) : null}
      </span>
      <span className="flex min-w-0 flex-col gap-0.5">
        {stage.summary.slice(0, 2).map((line, i) => (
          <span key={i} className={cn("text-[11px] leading-4 [overflow-wrap:anywhere]", i === 0 ? "text-foreground" : "text-muted-foreground")}>{line}</span>
        ))}
      </span>
    </button>
  );
}

export function ScenePipeline({ stages, selected, onSelect, className }: {
  stages: PipelineStage[];
  selected: string;
  onSelect: (key: string) => void;
  className?: string;
}) {
  const grouped = PHASES.map((p) => ({ ...p, items: stages.filter((s) => (s.phase ?? "prepare") === p.key) })).filter((p) => p.items.length);

  return (
    <nav aria-label="评估管线" className={cn("min-w-0 overflow-x-auto", className)}>
      <ol className="flex w-max min-w-full items-stretch gap-2 pb-1">
        {grouped.map((phase, pi) => (
          <Fragment key={phase.key}>
            {pi > 0 ? (
              <li aria-hidden className="flex items-center pt-6">
                <ChevronRight className="size-4 text-muted-foreground" />
              </li>
            ) : null}
            <li className="flex flex-col gap-2">
              <div className="flex items-center gap-2 px-1">
                <span className="text-[11px] font-medium text-muted-foreground">{phase.title}</span>
                <span className="h-px flex-1 bg-border" />
              </div>
              <ol className="flex flex-1 items-stretch gap-1.5">
                {phase.items.map((s, i) => (
                  <Fragment key={s.key}>
                    {i > 0 ? <li aria-hidden className="flex items-center"><span className="h-px w-2.5 bg-border" /></li> : null}
                    <li className="flex">
                      <StageNode stage={s} selected={s.key === selected} onSelect={() => onSelect(s.key)} />
                    </li>
                  </Fragment>
                ))}
              </ol>
            </li>
          </Fragment>
        ))}
      </ol>
    </nav>
  );
}
