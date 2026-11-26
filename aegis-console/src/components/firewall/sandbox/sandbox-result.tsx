"use client";

import { Ban, CircleCheck, Eye, FileText, Info, Layers, Timer } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { ArcGauge } from "@/components/traffic/traffic-kit";
import { RULE_ACTION_META, RuleIdPill, RuleSeverityBadge, STAGE_META, ToneBadge, groupName, reasonLabel } from "@/components/firewall/firewall-kit";
import { ExprCode } from "@/components/firewall/rules/rule-expression";
import type { FirewallMatchedRule, FirewallRule, FirewallSandboxResult } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";
import { SANDBOX_REASON } from "./sandbox-presets";

/**
 * 测试台结果：结论 → 计分 → 谁做的决定 → 命中明细。
 * 「结论」与「计分」并排放在最上面，因为管理员来这里问的第一件事就是「拦不拦」。
 */

const VERDICT_META = {
  pass: { label: "通过", icon: CircleCheck, className: "border-emerald-500/30 bg-emerald-500/8 text-emerald-700 dark:text-emerald-300" },
  block: { label: "拦截", icon: Ban, className: "border-rose-500/30 bg-rose-500/8 text-rose-700 dark:text-rose-300" },
  observe: { label: "观察", icon: Eye, className: "border-amber-500/30 bg-amber-500/10 text-amber-800 dark:text-amber-300" }
} as const;

function baseType(value: string) {
  return value.split(";")[0]!.trim().toLowerCase();
}

function typeName(value: string) {
  const base = baseType(value);
  if (base === "application/json" || base.endsWith("+json")) return "JSON";
  if (base === "application/x-www-form-urlencoded") return "表单";
  if (base === "multipart/form-data") return "multipart";
  if (base === "text/plain") return "纯文本";
  if (base === "application/octet-stream") return "二进制";
  return base || "未声明";
}

function formatMicros(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10_000 ? 0 : 1)} ms`;
  return `${value} µs`;
}

export function SandboxResultView({ result, declaredType }: { result: FirewallSandboxResult; declaredType: string }) {
  const verdict = VERDICT_META[result.verdict] ?? VERDICT_META.pass;
  const VerdictIcon = verdict.icon;
  const stage = result.stage ? STAGE_META[result.stage] : null;
  const threshold = Math.max(1, result.threshold);
  const max = Math.max(threshold * 2, result.inboundScore, 10);
  const effective = result.effectiveContentType ?? "";
  const retyped = Boolean(effective) && baseType(effective) !== baseType(declaredType);
  const matches = [...(result.matches ?? [])].sort((a, b) => a.phase - b.phase);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="grid min-w-0 gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
        <div className={cn("flex min-w-0 flex-col justify-between gap-3 rounded-xl border p-4", verdict.className)}>
          <div className="flex min-w-0 items-center gap-3">
            <VerdictIcon className="size-9 shrink-0" strokeWidth={1.75} />
            <div className="min-w-0">
              <div className="text-2xl leading-8 font-semibold tracking-tight">{verdict.label}</div>
              <div className="text-xs opacity-80">{SANDBOX_REASON[result.reason] ?? reasonLabel(result.reason)}</div>
            </div>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[11px]">
            {stage ? (
              <span className="inline-flex items-center gap-1 rounded-md bg-background/70 px-1.5 leading-5 text-foreground" title={stage.hint}>
                <Layers className="size-3" />
                {stage.label}段决定
              </span>
            ) : null}
            {result.status ? (
              <span className="rounded-md bg-background/70 px-1.5 font-mono leading-5 text-foreground">HTTP {result.status}</span>
            ) : null}
            {result.ruleId ? (
              <span className="inline-flex items-center gap-1 rounded-md bg-background/70 px-1 leading-5 text-foreground">
                决定性规则 <RuleIdPill id={result.ruleId} className="h-4" />
              </span>
            ) : null}
            {result.detectionOnly ? <span className="rounded-md bg-background/70 px-1.5 leading-5 text-foreground">观察模式</span> : null}
            {result.usedDraft ? <span className="rounded-md bg-background/70 px-1.5 leading-5 text-foreground">已带草稿例外</span> : null}
            <span className="inline-flex items-center gap-1 rounded-md bg-background/70 px-1.5 leading-5 text-foreground tabular-nums">
              <Timer className="size-3" />
              {formatMicros(result.elapsedMicros)}
            </span>
          </div>
        </div>
        <div className="flex min-w-0 justify-center rounded-xl border border-border px-4 pt-4 pb-3">
          <ArcGauge
            value={result.inboundScore}
            max={max}
            threshold={threshold}
            label="入站异常分"
            display={
              <span className="tabular-nums">
                {result.inboundScore}
                <span className="text-xs font-normal text-muted-foreground"> / {result.threshold}</span>
              </span>
            }
            sub={result.inboundScore >= result.threshold ? "已越过阈值" : `距阈值 ${result.threshold - result.inboundScore} 分`}
          />
        </div>
      </div>

      {retyped ? (
        <div className="flex min-w-0 items-start gap-2 rounded-lg border border-sky-500/30 bg-sky-500/8 px-3 py-2 text-xs text-sky-800 dark:text-sky-300">
          <Info className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0">
            声明为 <code className="font-mono">{typeName(declaredType)}</code>，按内容改判为 <code className="font-mono">{typeName(effective)}</code>
            ：处理器解析请求体时不看 Content-Type，WAF 也按实际内容检查。
          </span>
        </div>
      ) : null}

      {result.customRule ? <CustomRuleRow title="决定的自定义规则" rule={result.customRule} /> : null}
      {result.loggedRules?.length ? (
        <div className="flex min-w-0 flex-col gap-2">
          <h4 className="text-xs font-medium text-muted-foreground">记录类规则（{result.loggedRules.length}）</h4>
          {result.loggedRules.map((rule) => (
            <CustomRuleRow key={rule.id} rule={rule} compact />
          ))}
        </div>
      ) : null}

      <div className="flex min-w-0 flex-col gap-2">
        <h4 className="text-xs font-medium text-muted-foreground">WAF 命中（{matches.length}）</h4>
        {matches.length === 0 ? (
          <div className="grid min-h-14 place-items-center rounded-xl border border-dashed border-border text-xs text-muted-foreground">暂无命中</div>
        ) : (
          <ol className="relative flex min-w-0 flex-col">
            {matches.map((match, index) => (
              <MatchRow key={`${match.id}-${index}`} match={match} last={index === matches.length - 1} />
            ))}
          </ol>
        )}
      </div>
    </div>
  );
}

function CustomRuleRow({ rule, title, compact }: { rule: FirewallRule; title?: string; compact?: boolean }) {
  const meta = RULE_ACTION_META[rule.action] ?? RULE_ACTION_META.block;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 rounded-xl border border-border px-3 py-2.5">
      {title ? <span className="text-[11px] text-muted-foreground">{title}</span> : null}
      <div className="flex min-w-0 items-center gap-1.5">
        <FileText className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{rule.name}</span>
        <ToneBadge tone={meta.tone}>{meta.label}</ToneBadge>
      </div>
      {!compact ? <ExprCode source={rule.expression} /> : null}
    </div>
  );
}

function MatchRow({ match, last }: { match: FirewallMatchedRule; last: boolean }) {
  return (
    <li className="relative flex min-w-0 gap-3 pb-3">
      {!last ? <span aria-hidden className="absolute top-6 bottom-0 left-[13px] w-px bg-border" /> : null}
      <span className="z-[1] grid size-7 shrink-0 place-items-center rounded-full border border-border bg-card font-mono text-[10px] text-muted-foreground" title={`阶段 ${match.phase}`}>
        P{match.phase}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1 rounded-xl border border-border px-3 py-2">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <RuleIdPill id={match.id} />
          <RuleSeverityBadge severity={match.severity} />
          <Badge variant="outline" size="sm">
            {groupName(match.group)}
          </Badge>
        </div>
        <p className="text-xs leading-5">{match.message}</p>
        {match.targets?.length ? (
          <div className="flex min-w-0 flex-wrap gap-1">
            {match.targets.map((target) => (
              <code key={target} className="rounded-md bg-muted px-1.5 font-mono text-[11px] leading-5 break-all">
                {target}
              </code>
            ))}
          </div>
        ) : null}
        {match.data ? (
          <code className="line-clamp-3 block rounded-md bg-muted/50 px-2 py-1 font-mono text-[11px] leading-4 break-all whitespace-pre-wrap text-muted-foreground">
            {match.data}
          </code>
        ) : null}
      </div>
    </li>
  );
}
