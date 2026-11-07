"use client";

import { Fragment, type ReactNode } from "react";
import { ChevronRight, Filter, Gauge, Hourglass, Layers, ShieldBan, TrendingUp, type LucideIcon } from "lucide-react";
import type { ResponseSpec, TrafficRule } from "@/lib/api/traffic";
import { cn } from "@/lib/utils";
import {
  HEADER_OP_LABEL,
  concurrencyPhrase,
  fmtDuration,
  fmtNum,
  limitPhrase,
  warmupPhrase
} from "./traffic-format";

/**
 * 规则流水线：匹配 → 限速 → 并发 → 排队 → 预热 → 处置。
 *
 * 规则卡片、编辑页的实时预览、模板库都画这一条，所以「一条规则长什么样」只有一种画法。
 * 横向用于卡片（窄了自动折行），纵向用于编辑页右栏。
 */

type StageTone = "sky" | "primary" | "violet" | "amber" | "emerald" | "rose" | "muted";

const STAGE_TONE: Record<StageTone, { plate: string; icon: string }> = {
  sky: { plate: "border-sky-500/25 bg-sky-500/8", icon: "text-sky-600 dark:text-sky-400" },
  primary: { plate: "border-border bg-muted/60", icon: "text-foreground" },
  violet: { plate: "border-violet-500/25 bg-violet-500/8", icon: "text-violet-600 dark:text-violet-400" },
  amber: { plate: "border-amber-500/25 bg-amber-500/8", icon: "text-amber-600 dark:text-amber-400" },
  emerald: { plate: "border-emerald-500/25 bg-emerald-500/8", icon: "text-emerald-600 dark:text-emerald-400" },
  rose: { plate: "border-rose-500/25 bg-rose-500/8", icon: "text-rose-600 dark:text-rose-400" },
  muted: { plate: "border-dashed border-border bg-transparent", icon: "text-muted-foreground" }
};

type Stage = { key: string; caption: string; icon: LucideIcon; tone: StageTone; items: ReactNode[] };

function summarizeList(values: string[], max: number) {
  if (values.length <= max) return values;
  return [...values.slice(0, max), `+${values.length - max}`];
}

export function buildStages(rule: TrafficRule, defaultResponse?: ResponseSpec | null): Stage[] {
  const m = rule.match;
  const matchItems: ReactNode[] = [];
  if (m.methods.length) matchItems.push(m.methods.join(" "));
  summarizeList(m.paths, 3).forEach((path) => matchItems.push(path));
  if (m.hosts.length) summarizeList(m.hosts, 2).forEach((host) => matchItems.push(host));
  if (m.appKeys.length) matchItems.push(`应用 ${m.appKeys.length}`);
  if (m.cidrs.length) matchItems.push(m.cidrs.length === 1 ? m.cidrs[0] : `网段 ${m.cidrs.length}`);
  m.headers.slice(0, 2).forEach((header) =>
    matchItems.push(
      header.op === "exists" || header.op === "absent"
        ? `${header.name} ${HEADER_OP_LABEL[header.op]}`
        : `${header.name} ${HEADER_OP_LABEL[header.op]} ${header.value}`
    )
  );
  if (m.headers.length > 2) matchItems.push(`请求头 +${m.headers.length - 2}`);
  const excluded = m.excludePaths.length + m.excludeCidrs.length;
  if (excluded) matchItems.push(`排除 ${excluded}`);
  if (!matchItems.length) matchItems.push("全部请求");

  const stages: Stage[] = [{ key: "match", caption: "匹配", icon: Filter, tone: "sky", items: matchItems }];

  if (rule.limits.length) {
    stages.push({
      key: "limits",
      caption: "限速",
      icon: Gauge,
      tone: "primary",
      items: rule.limits.map((limit, index) => (
        <span key={index} className="inline-flex items-center gap-1">
          {limitPhrase(limit)}
          {limit.scope === "cluster" ? <span className="rounded bg-foreground/10 px-1 text-[10px]">集群</span> : null}
        </span>
      ))
    });
  }
  if (rule.concurrency) {
    stages.push({ key: "concurrency", caption: "并发", icon: Layers, tone: "violet", items: [concurrencyPhrase(rule.concurrency)] });
  }
  if (rule.queue) {
    stages.push({
      key: "queue",
      caption: "排队",
      icon: Hourglass,
      tone: "amber",
      items: [`${fmtNum(rule.queue.size)} 个`, `等 ${fmtDuration(rule.queue.timeoutMs)}`]
    });
  }
  if (rule.warmup) {
    stages.push({ key: "warmup", caption: "预热", icon: TrendingUp, tone: "emerald", items: [warmupPhrase(rule.warmup)] });
  }

  const response = rule.response ?? defaultResponse ?? null;
  const actionItems: ReactNode[] =
    rule.mode === "enforce"
      ? [response ? `拒绝 ${response.status}` : "拒绝"]
      : rule.mode === "observe"
        ? ["只记录"]
        : ["直接放行"];
  if (rule.terminal) actionItems.push("命中即止");
  stages.push({
    key: "action",
    caption: "处置",
    icon: ShieldBan,
    tone: rule.mode === "enforce" ? "rose" : rule.mode === "observe" ? "violet" : "muted",
    items: actionItems
  });
  return stages;
}

export function RulePipeline({
  rule,
  defaultResponse,
  orientation = "horizontal",
  className
}: {
  rule: TrafficRule;
  defaultResponse?: ResponseSpec | null;
  orientation?: "horizontal" | "vertical";
  className?: string;
}) {
  const stages = buildStages(rule, defaultResponse);

  if (orientation === "vertical") {
    return (
      <ol className={cn("relative flex min-w-0 flex-col gap-2", className)}>
        <span aria-hidden className="absolute top-4 bottom-4 left-[15px] w-px bg-border" />
        {stages.map((stage) => {
          const tone = STAGE_TONE[stage.tone];
          const Icon = stage.icon;
          return (
            <li key={stage.key} className="relative flex min-w-0 items-start gap-3">
              <span className={cn("relative z-10 grid size-8 shrink-0 place-items-center rounded-lg border bg-card", tone.plate)}>
                <Icon className={cn("size-3.5", tone.icon)} />
              </span>
              <div className="min-w-0 flex-1 pt-0.5">
                <div className="text-[11px] leading-4 text-muted-foreground">{stage.caption}</div>
                <div className="mt-0.5 flex min-w-0 flex-wrap gap-1">
                  {stage.items.map((item, index) => (
                    <span key={index} className="min-w-0 rounded-md bg-muted px-1.5 py-0.5 font-mono text-[11px] leading-4 break-all">
                      {item}
                    </span>
                  ))}
                </div>
              </div>
            </li>
          );
        })}
      </ol>
    );
  }

  return (
    <ol className={cn("flex min-w-0 flex-wrap items-stretch gap-y-2", className)}>
      {stages.map((stage, index) => {
        const tone = STAGE_TONE[stage.tone];
        const Icon = stage.icon;
        return (
          <Fragment key={stage.key}>
            {index > 0 ? (
              <li aria-hidden className="flex items-center px-0.5 text-muted-foreground/60">
                <ChevronRight className="size-3.5" />
              </li>
            ) : null}
            <li className={cn("flex min-w-0 max-w-full flex-col gap-1 rounded-lg border px-2 py-1.5", tone.plate)}>
              <span className={cn("inline-flex items-center gap-1 text-[10px] leading-3 font-medium", tone.icon)}>
                <Icon className="size-3" />
                {stage.caption}
              </span>
              <span className="flex min-w-0 flex-wrap gap-x-2 gap-y-0.5 font-mono text-[11px] leading-4">
                {stage.items.map((item, i) => (
                  <span key={i} className="min-w-0 break-all">
                    {item}
                  </span>
                ))}
              </span>
            </li>
          </Fragment>
        );
      })}
    </ol>
  );
}
