"use client";

import { useState } from "react";
import { Check, FlaskConical, Loader2, Play, Plus, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { SimulateResult, SimulateStep, TrafficRule } from "@/lib/api/traffic";
import { useTrafficSimulateMutation } from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { ClassBadge, Panel } from "./traffic-kit";
import { KEY_LABEL, METHODS, OUTCOME_LABEL, VERDICT_LABEL, fmtNum } from "./traffic-format";

/**
 * 模拟请求：把当前草稿连同已发布的其它规则一起过一遍，逐条给出命中与判定。
 * 模拟不消耗任何配额。
 */

function samplePath(rule: TrafficRule) {
  const path = rule.match.paths[0];
  if (!path) return "/api/";
  return path.replace(/\*/g, "demo");
}

type HeaderRow = { name: string; value: string };

export function RuleSimulator({ rule }: { rule: TrafficRule }) {
  const simulate = useTrafficSimulateMutation();
  const [method, setMethod] = useState(() => rule.match.methods[0] ?? "GET");
  const [path, setPath] = useState(() => samplePath(rule));
  const [ip, setIp] = useState("203.0.113.10");
  const [headers, setHeaders] = useState<HeaderRow[]>(() =>
    rule.match.headers.filter((h) => h.op !== "absent").slice(0, 2).map((h) => ({ name: h.name, value: h.value }))
  );

  const run = () =>
    simulate.mutate({
      input: {
        method,
        path: path.trim() || "/",
        ip: ip.trim() || undefined,
        headers: Object.fromEntries(headers.filter((h) => h.name.trim()).map((h) => [h.name.trim(), h.value]))
      },
      draft: rule
    });

  return (
    <Panel
      icon={FlaskConical}
      tone="violet"
      title="模拟请求"
      aside={
        <Button size="sm" onClick={run} disabled={simulate.isPending}>
          {simulate.isPending ? <Loader2 className="animate-spin" /> : <Play />}
          模拟
        </Button>
      }
    >
      <div className="flex flex-col gap-2">
        <div className="flex min-w-0 gap-2">
          <Select value={method} onValueChange={setMethod}>
            <SelectTrigger size="sm" className="w-24 shrink-0 font-mono text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {METHODS.map((item) => (
                <SelectItem key={item} value={item} className="font-mono text-xs">
                  {item}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input value={path} onChange={(event) => setPath(event.target.value)} aria-label="路径" className="h-8 min-w-0 font-mono text-xs" />
        </div>
        <Input value={ip} onChange={(event) => setIp(event.target.value)} aria-label="来源 IP" placeholder="来源 IP" className="h-8 font-mono text-xs" />
        {headers.map((header, index) => (
          <div key={index} className="flex min-w-0 gap-2">
            <Input
              value={header.name}
              placeholder="请求头"
              aria-label="请求头名"
              onChange={(event) => setHeaders(headers.map((h, i) => (i === index ? { ...h, name: event.target.value } : h)))}
              className="h-8 w-[42%] min-w-0 font-mono text-xs"
            />
            <Input
              value={header.value}
              placeholder="值"
              aria-label="请求头值"
              onChange={(event) => setHeaders(headers.map((h, i) => (i === index ? { ...h, value: event.target.value } : h)))}
              className="h-8 min-w-0 flex-1 font-mono text-xs"
            />
            <Button size="icon-sm" variant="ghost" aria-label="移除请求头" onClick={() => setHeaders(headers.filter((_, i) => i !== index))}>
              <X />
            </Button>
          </div>
        ))}
        <Button size="xs" variant="ghost" className="self-start text-muted-foreground" onClick={() => setHeaders([...headers, { name: "", value: "" }])}>
          <Plus />
          请求头
        </Button>
      </div>

      {simulate.data ? <SimulationResult result={simulate.data} draftId={rule.id} /> : null}
    </Panel>
  );
}

const VERDICT_TONE: Record<SimulateResult["verdict"], string> = {
  allow: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
  reject: "bg-rose-500/12 text-rose-700 dark:text-rose-300",
  observe: "bg-violet-500/12 text-violet-700 dark:text-violet-300"
};

const OUTCOME_BADGE: Record<SimulateStep["outcome"], "success" | "danger" | "info" | "outline"> = {
  pass: "success",
  reject: "danger",
  observe: "info",
  bypass: "outline",
  skip: "outline"
};

function SimulationResult({ result, draftId }: { result: SimulateResult; draftId: string }) {
  return (
    <div className="mt-4 flex flex-col gap-3 border-t border-border pt-4">
      <div className={cn("flex min-w-0 flex-wrap items-center gap-2 rounded-xl px-3 py-2.5", VERDICT_TONE[result.verdict])}>
        <span className="text-lg leading-6 font-semibold">{VERDICT_LABEL[result.verdict]}</span>
        <ClassBadge value={result.class} />
        {result.exempt ? (
          <Badge variant="outline" size="sm">
            免检{result.exemptReason ? ` ${result.exemptReason}` : ""}
          </Badge>
        ) : null}
      </div>
      {result.steps.length ? (
        <ol className="relative flex flex-col gap-2">
          <span aria-hidden className="absolute top-3 bottom-3 left-[7px] w-px bg-border" />
          {result.steps.map((step, index) => {
            const isDraft = step.ruleId === draftId || (!draftId && !step.ruleId);
            return (
              <li key={`${step.ruleId}-${index}`} className={cn("relative flex min-w-0 gap-3", !step.matched && "opacity-60")}>
                <span
                  aria-hidden
                  className={cn(
                    "relative z-10 mt-1 size-[15px] shrink-0 rounded-full border-2 border-card",
                    !step.matched
                      ? "bg-muted-foreground/40"
                      : step.outcome === "reject"
                        ? "bg-rose-500"
                        : step.outcome === "observe"
                          ? "bg-violet-500"
                          : "bg-emerald-500"
                  )}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                    <span className="min-w-0 truncate text-sm font-medium">{step.ruleName || "本规则"}</span>
                    {isDraft ? (
                      <Badge variant="info" size="sm">
                        草稿
                      </Badge>
                    ) : null}
                    {step.matched ? (
                      <Badge variant={OUTCOME_BADGE[step.outcome]} size="sm">
                        {OUTCOME_LABEL[step.outcome]}
                      </Badge>
                    ) : (
                      <Badge variant="outline" size="sm">
                        未命中
                      </Badge>
                    )}
                    {step.terminal && step.matched ? (
                      <Badge variant="outline" size="sm">
                        命中即止
                      </Badge>
                    ) : null}
                  </div>
                  {!step.matched && step.miss ? <div className="mt-0.5 text-xs text-muted-foreground">{step.miss}</div> : null}
                  {step.detail ? <div className="mt-0.5 text-xs text-muted-foreground">{step.detail}</div> : null}
                  {step.keys?.length ? (
                    <div className="mt-1.5 flex flex-col gap-1">
                      {step.keys.map((key, i) => (
                        <div key={i} className="flex min-w-0 items-center gap-2 rounded-md bg-muted/60 px-2 py-1 text-[11px]">
                          {key.wouldPass ? (
                            <Check className="size-3 shrink-0 text-emerald-600 dark:text-emerald-400" />
                          ) : (
                            <X className="size-3 shrink-0 text-rose-600 dark:text-rose-400" />
                          )}
                          <span className="shrink-0 text-muted-foreground">{KEY_LABEL[key.kind] ?? key.kind}</span>
                          <span className="min-w-0 flex-1 truncate font-mono">{key.value}</span>
                          <span className="shrink-0 tabular-nums">
                            {fmtNum(Math.floor(key.remaining))}
                            <span className="text-muted-foreground">/{fmtNum(key.capacity)}</span>
                          </span>
                        </div>
                      ))}
                    </div>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ol>
      ) : null}
    </div>
  );
}
