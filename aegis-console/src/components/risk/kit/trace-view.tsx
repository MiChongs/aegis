"use client";

import { useMemo, useState } from "react";
import { AlertTriangle, Check, ChevronDown, CircleDashed, Eye, Gavel, Timer } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { Trace, TraceRule, TraceStage } from "@/lib/api/risk";
import { useRiskFeatures, useRiskModels } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { latencyText } from "./humanize";
import { ACTION_LABEL, DECIDED_BY_LABEL, DIM_LABEL, SOURCE_LABEL, STAGE_LABEL } from "./labels";

/**
 * 决策轨迹：一次评估依次经过名单与特征、派生特征、模型、规则、决策流、策略六个环节。
 * 决策详情与场景模拟器共用。
 *
 * 结构：一句话摘要 → 耗时占比条 → 环节流程（每个环节一句人话说明做了什么、结果如何）→ 规则清单。
 * 出错标红、降级标琥珀，排查「为什么慢」「为什么没取到特征」时先看这里。
 */
export function TraceView({ trace }: { trace: Trace }) {
  const names = useTraceNames();
  const stages = useMemo(() => trace.stages ?? [], [trace.stages]);
  const total = stages.reduce((sum, s) => sum + Math.max(0, s.micros), 0);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <TraceSummary trace={trace} total={total} names={names} />
      {stages.length === 0 ? (
        <p className="rounded-xl bg-muted/50 px-4 py-6 text-center text-sm text-muted-foreground">这次评估没有记录环节耗时。</p>
      ) : (
        <>
          <TimeShare stages={stages} total={total} />
          <StageFlow stages={stages} />
        </>
      )}
      <RuleList rules={trace.rules ?? []} />
    </div>
  );
}

/* ── 名称与说法 ── */

type Names = { feature: (key: string) => string; model: (key: string) => string };

function useTraceNames(): Names {
  const features = useRiskFeatures();
  const models = useRiskModels();
  return useMemo(() => {
    const f = new Map((features.data ?? []).map((x) => [x.key, x.name]));
    const m = new Map((models.data ?? []).map((x) => [x.key, x.name]));
    return { feature: (k: string) => f.get(k) || k, model: (k: string) => m.get(k) || k };
  }, [features.data, models.data]);
}

/** 由谁作出结论的一句话 */
export function decidedByText(by?: string): string {
  switch (by) {
    case "list":
    case "rule":
    case "flow":
    case "policy":
      return `由${DECIDED_BY_LABEL[by]}作出结论`;
    case "no_release":
      return "场景尚未发布，按默认处置";
    case "scope":
      return "请求不在场景的应用范围内，按默认处置";
    case "default":
    case "":
    case undefined:
      return "未触发任何判定，按默认处置";
    default:
      return `由${DECIDED_BY_LABEL[by] ?? "场景配置"}作出结论`;
  }
}

/** 降级项的名称：「关系图」「模型「设备风险」」 */
export function degradedName(item: string, names?: Partial<Names>): string {
  const [kind, key] = item.includes(":") ? [item.slice(0, item.indexOf(":")), item.slice(item.indexOf(":") + 1)] : [item, ""];
  if (kind === "derived") return `派生特征「${names?.feature?.(key) ?? key}」`;
  if (kind === "model") return `模型「${names?.model?.(key) ?? key}」`;
  if (kind === "flow") return "决策流";
  if (kind === "policy") return "策略";
  return SOURCE_LABEL[kind] ?? STAGE_LABEL[kind] ?? kind;
}

const actionWord = (a: string) => ACTION_LABEL[a as keyof typeof ACTION_LABEL] ?? a;

/** 把引擎写的环节说明改成人话 */
function stageNote(stage: TraceStage): string {
  const n = (stage.note ?? "").trim();
  let m: RegExpMatchArray | null;
  if ((m = n.match(/^名单 (\d+) 项、特征 (\d+) 个$/))) return `查询名单 ${m[1]} 项，读取特征 ${m[2]} 个`;
  if ((m = n.match(/^(\d+) 个派生特征$/))) return m[1] === "0" ? "无派生特征" : `计算派生特征 ${m[1]} 个`;
  if ((m = n.match(/^(\d+) 个模型$/))) return m[1] === "0" ? "未启用模型" : `运行模型 ${m[1]} 个`;
  if ((m = n.match(/^(\d+) 条参评，(\d+) 条命中$/))) return m[2] === "0" ? `评估规则 ${m[1]} 条，均未命中` : `评估规则 ${m[1]} 条，命中 ${m[2]} 条`;
  if ((m = n.match(/^决策流 (\w+)$/))) return `决策流给出${actionWord(m[1]!)}`;
  if ((m = n.match(/^分数段 (\w+)$/))) return `按分数段给出${actionWord(m[1]!)}`;
  if ((m = n.match(/^裁决 (\w+)$/))) return `最终裁决为${actionWord(m[1]!)}`;
  return n.replace(/\b(pass|review|captcha|block|ban)\b/g, (a) => actionWord(a));
}

/* ── 摘要 ── */

function TraceSummary({ trace, total, names }: { trace: Trace; total: number; names: Names }) {
  const stages = trace.stages ?? [];
  const degraded = [...new Set(trace.degraded ?? [])];
  const failed = stages.filter((s) => s.error).length;
  const parts = [
    stages.length ? `用时 ${latencyText(total)}，经过 ${stages.length} 个环节` : "",
    decidedByText(trace.decidedBy),
  ].filter(Boolean);

  return (
    <div
      className={cn(
        "flex min-w-0 items-start gap-3 rounded-xl border px-4 py-3",
        failed ? "border-red-500/30 bg-red-500/5" : degraded.length ? "border-amber-500/30 bg-amber-500/5" : "border-border bg-muted/40"
      )}
    >
      <span
        className={cn(
          "mt-0.5 grid size-6 shrink-0 place-items-center rounded-full",
          failed ? "bg-red-500/15 text-red-600 dark:text-red-400" : degraded.length ? "bg-amber-500/15 text-amber-600 dark:text-amber-400" : "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
        )}
      >
        {failed || degraded.length ? <AlertTriangle className="size-3.5" /> : <Check className="size-3.5" />}
      </span>
      <div className="flex min-w-0 flex-col gap-1">
        <p className="text-sm leading-6">{parts.join("，")}。</p>
        {degraded.length ? (
          <p className="text-xs leading-5 text-muted-foreground">
            {degraded.map((d) => degradedName(d, names)).join("、")}未能正常取得，已按默认值继续评估。
          </p>
        ) : null}
        {failed ? <p className="text-xs leading-5 text-red-600 dark:text-red-400">{failed} 个环节执行出错，详情见下方。</p> : null}
      </div>
    </div>
  );
}

/* ── 耗时占比 ── */

const STAGE_COLOR: Record<string, string> = {
  fetch: "bg-sky-500",
  derived: "bg-teal-500",
  models: "bg-violet-500",
  rules: "bg-amber-500",
  flow: "bg-indigo-500",
  policy: "bg-slate-500",
};

const stageColor = (s: TraceStage) => (s.error ? "bg-red-500" : STAGE_COLOR[s.stage] ?? "bg-zinc-400");

function TimeShare({ stages, total }: { stages: TraceStage[]; total: number }) {
  const sum = total || 1;
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <header className="flex items-center gap-2">
        <Timer className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-medium">耗时分布</h4>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">合计 {latencyText(total)}</span>
      </header>
      <div className="flex h-3 w-full min-w-0 gap-0.5 overflow-hidden rounded-full bg-muted">
        {stages.map((s, i) => {
          const share = Math.max(0, s.micros) / sum;
          if (share <= 0) return null;
          return (
            <Tooltip key={`${s.stage}-${i}`}>
              <TooltipTrigger asChild>
                <span className={cn("h-full min-w-1 first:rounded-l-full last:rounded-r-full", stageColor(s))} style={{ flexGrow: share, flexBasis: 0 }} />
              </TooltipTrigger>
              <TooltipContent>
                {STAGE_LABEL[s.stage] ?? s.stage} {latencyText(s.micros)}，占 {Math.round(share * 100)}%
              </TooltipContent>
            </Tooltip>
          );
        })}
      </div>
      <div className="flex min-w-0 flex-wrap gap-x-4 gap-y-1">
        {stages.map((s, i) => (
          <span key={`${s.stage}-${i}`} className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span aria-hidden className={cn("size-2 rounded-[3px]", stageColor(s))} />
            {STAGE_LABEL[s.stage] ?? s.stage}
            <span className="tabular-nums">{Math.round((Math.max(0, s.micros) / sum) * 100)}%</span>
          </span>
        ))}
      </div>
    </section>
  );
}

/* ── 环节流程 ── */

function StageFlow({ stages }: { stages: TraceStage[] }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <header className="flex items-center gap-2">
        <Gavel className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-medium">评估环节</h4>
      </header>
      <ol className="flex min-w-0 flex-col">
        {stages.map((stage, i) => (
          <StageStep key={`${stage.stage}-${i}`} stage={stage} index={i} last={i === stages.length - 1} />
        ))}
      </ol>
    </section>
  );
}

function StageStep({ stage, index, last }: { stage: TraceStage; index: number; last: boolean }) {
  const bad = !!stage.error;
  const warn = !bad && !!stage.degraded;
  const sources = stage.sources ?? [];
  const sourceMax = Math.max(1, ...sources.map((s) => s.micros));
  const note = stageNote(stage);

  return (
    <li className="relative flex min-w-0 gap-3">
      <div className="flex shrink-0 flex-col items-center">
        <span
          className={cn(
            "z-10 grid size-6 place-items-center rounded-full border text-[11px] font-medium tabular-nums",
            bad
              ? "border-red-500/50 bg-red-500/10 text-red-600 dark:text-red-400"
              : warn
                ? "border-amber-500/50 bg-amber-500/10 text-amber-600 dark:text-amber-400"
                : "border-border bg-background text-muted-foreground"
          )}
        >
          {index + 1}
        </span>
        {!last ? <span aria-hidden className="w-px flex-1 bg-border" /> : null}
      </div>
      <div className={cn("flex min-w-0 flex-1 flex-col gap-1.5", last ? "pb-0" : "pb-4")}>
        <div className="flex min-h-6 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className={cn("size-2 shrink-0 rounded-[3px]", stageColor(stage))} aria-hidden />
          <span className="text-sm font-medium">{STAGE_LABEL[stage.stage] ?? stage.stage}</span>
          {bad ? <Badge variant="danger" size="sm">出错</Badge> : warn ? <Badge variant="warning" size="sm">部分降级</Badge> : null}
          <span className="ml-auto text-xs text-muted-foreground tabular-nums">{latencyText(stage.micros)}</span>
        </div>
        {note ? <p className="text-xs leading-5 text-muted-foreground">{note}</p> : null}
        {stage.error ? <ErrorDetail text={stage.error} /> : null}
        {sources.length > 0 ? (
          <ul className="mt-0.5 flex min-w-0 flex-col gap-1.5 rounded-lg border border-border bg-muted/30 px-3 py-2.5">
            {sources.map((src) => (
              <li key={src.source} className="flex min-w-0 flex-col gap-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className={cn("w-16 shrink-0 truncate text-xs", src.error ? "text-red-600 dark:text-red-400" : "text-foreground")}>
                    {SOURCE_LABEL[src.source] ?? src.source}
                  </span>
                  <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                    <div
                      className={cn("h-full rounded-full", src.error ? "bg-red-500" : "bg-sky-500")}
                      style={{ width: `${Math.max(3, (src.micros / sourceMax) * 100)}%` }}
                    />
                  </div>
                  <span className="w-16 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{latencyText(src.micros)}</span>
                </div>
                {src.error ? <ErrorDetail text={src.error} /> : null}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </li>
  );
}

function ErrorDetail({ text }: { text: string }) {
  return <p className="rounded-md bg-red-500/8 px-2 py-1 font-mono text-[11px] leading-4 break-all text-red-700 dark:text-red-300">{text}</p>;
}

/* ── 规则 ── */

function RuleList({ rules }: { rules: TraceRule[] }) {
  const [open, setOpen] = useState(false);
  const hits = rules.filter((r) => r.hit).sort((a, b) => Number(a.mode === "observe") - Number(b.mode === "observe") || b.score - a.score);
  const misses = rules.filter((r) => !r.hit).sort((a, b) => Number(!!b.error) - Number(!!a.error));
  const missErrors = misses.filter((r) => r.error).length;
  const enforced = hits.filter((r) => r.mode !== "observe");
  const points = enforced.reduce((s, r) => s + r.score, 0);

  return (
    <section className="flex min-w-0 flex-col gap-2">
      <header className="flex items-center gap-2">
        <Check className="size-3.5 text-muted-foreground" />
        <h4 className="text-xs font-medium">规则</h4>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">
          {rules.length ? `评估 ${rules.length} 条，命中 ${hits.length} 条${points ? `，计 ${points} 分` : ""}` : ""}
        </span>
      </header>
      {rules.length === 0 ? (
        <p className="rounded-xl bg-muted/50 px-4 py-6 text-center text-sm text-muted-foreground">这个场景没有参与评估的规则。</p>
      ) : (
        <>
          {hits.length > 0 ? (
            <ul className="flex min-w-0 flex-col gap-1.5">
              {hits.map((rule) => (
                <RuleRow key={rule.key} rule={rule} />
              ))}
            </ul>
          ) : (
            <p className="rounded-xl bg-muted/50 px-4 py-4 text-center text-sm text-muted-foreground">没有命中任何规则。</p>
          )}
          {misses.length > 0 ? (
            <Collapsible open={open} onOpenChange={setOpen}>
              <CollapsibleTrigger className="inline-flex items-center gap-1.5 rounded-md px-1 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground">
                <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
                {open ? "收起" : "查看"}未命中的 {misses.length} 条
                {missErrors > 0 ? <Badge variant="danger" size="sm">{missErrors} 条出错</Badge> : null}
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
  const Icon = rule.error ? AlertTriangle : rule.hit ? (observe ? Eye : Check) : CircleDashed;
  return (
    <li
      className={cn(
        "flex min-w-0 gap-2.5 rounded-lg border px-3 py-2",
        rule.error
          ? "border-red-500/30 bg-red-500/5"
          : rule.hit
            ? observe
              ? "border-dashed border-sky-500/40 bg-sky-500/5"
              : "border-amber-500/30 bg-amber-500/5"
            : "border-border"
      )}
    >
      <Icon
        className={cn(
          "mt-0.5 size-4 shrink-0",
          rule.error ? "text-red-500" : rule.hit ? (observe ? "text-sky-500" : "text-amber-500") : "text-muted-foreground"
        )}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className={cn("min-w-0 text-sm", !rule.hit && !rule.error && "text-muted-foreground")}>{rule.name || "未命名规则"}</span>
          {rule.dimension ? <Badge variant="outline" size="sm">{DIM_LABEL[rule.dimension] ?? rule.dimension}</Badge> : null}
          {observe ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="info" size="sm">观察中</Badge>
              </TooltipTrigger>
              <TooltipContent>只记录命中，不计分，不影响结论</TooltipContent>
            </Tooltip>
          ) : null}
          <span
            className={cn(
              "ml-auto shrink-0 text-xs font-medium tabular-nums",
              rule.hit && !observe && rule.score > 0 ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground"
            )}
          >
            {rule.score > 0 ? `+${rule.score}` : rule.score} 分
          </span>
        </div>
        {rule.hit && rule.reason ? <p className="text-xs leading-5 text-muted-foreground">{rule.reason}</p> : null}
        {rule.hit && rule.decision && !observe ? (
          <p className="text-xs leading-5 text-red-600 dark:text-red-400">直接判定为{actionWord(rule.decision)}</p>
        ) : null}
        {rule.error ? <ErrorDetail text={rule.error} /> : null}
      </div>
    </li>
  );
}
