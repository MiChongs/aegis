"use client";

import { CountryFlag } from "@/components/ui/country-flag";
import { ActionBadge, reasonLabel, RuleIdPill, SeverityDot, TransportBadge } from "@/components/firewall/firewall-kit";
import type { FirewallEvent } from "@/lib/api/firewall";
import { formatCompact, formatDateTime } from "@/lib/datetime";
import { cn } from "@/lib/utils";
import { isEvaluationRule } from "./event-utils";

/** 行上展示的「主规则」：优先第一条计分规则，v1 旧事件只有 wafRuleId */
export function primaryRuleId(event: FirewallEvent) {
  const scoring = (event.matches ?? []).find((match) => !isEvaluationRule(match.id));
  if (scoring) return scoring.id;
  return event.wafRuleId ?? null;
}

/** 宽屏一行一格对齐；窄屏（容器 < 56rem）折成三行：判定、请求行、来源 */
export const EVENT_GRID =
  "@4xl:grid @4xl:grid-cols-[5.5rem_4.5rem_minmax(0,8.5rem)_minmax(0,1fr)_minmax(0,12rem)_6.5rem_3rem_4.5rem] @4xl:items-center @4xl:gap-3";

export function EventListHeader() {
  return (
    <div className={cn("hidden border-b bg-muted/30 px-4 py-2 text-[11px] font-medium text-muted-foreground", EVENT_GRID)}>
      <span>时间</span>
      <span>动作</span>
      <span>原因</span>
      <span>请求</span>
      <span>来源</span>
      <span>载荷</span>
      <span className="text-right">异常分</span>
      <span>规则</span>
    </div>
  );
}

export function EventRow({ event, active, onOpen }: { event: FirewallEvent; active?: boolean; onOpen: () => void }) {
  const ruleId = primaryRuleId(event);
  const place = [event.city || event.region, event.isp].filter(Boolean).join(" · ");
  const score = event.anomalyScore ?? 0;

  const timeCell = (
    <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(event.blockedAt)}>
      {formatCompact(event.blockedAt)}
    </span>
  );
  const scoreCell =
    score > 0 ? (
      <span className="font-data text-xs font-medium tabular-nums">{score}</span>
    ) : (
      <span className="text-xs text-muted-foreground">—</span>
    );
  const ruleCell = ruleId ? (
    <RuleIdPill id={ruleId} />
  ) : event.ruleRef ? (
    <span className="truncate text-[11px] text-muted-foreground" title={event.ruleName}>
      {event.ruleName || `自定义 #${event.ruleRef}`}
    </span>
  ) : (
    <span className="text-xs text-muted-foreground">—</span>
  );

  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "flex w-full min-w-0 flex-col gap-1.5 border-b px-4 py-2.5 text-left transition-colors last:border-b-0",
        "hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none",
        active && "bg-muted/50",
        EVENT_GRID,
        "@4xl:py-2"
      )}
    >
      {/* 宽屏：时间 */}
      <span className="hidden @4xl:block">{timeCell}</span>

      {/* 窄屏第一行：严重性 + 动作 + 原因 + 时间；宽屏拆成两格 */}
      <span className="flex min-w-0 items-center gap-2 @4xl:contents">
        <span className="flex shrink-0 items-center gap-1.5">
          <SeverityDot severity={event.severity} />
          <ActionBadge action={event.action} />
        </span>
        <span className="min-w-0 flex-1 truncate text-xs font-medium @4xl:flex-none">{reasonLabel(event.reason)}</span>
        <span className="@4xl:hidden">{timeCell}</span>
      </span>

      {/* 请求行 */}
      <span className="flex min-w-0 items-center gap-1.5 font-mono text-xs">
        <span className="shrink-0 text-[11px] font-semibold text-muted-foreground">{event.method}</span>
        <span className="min-w-0 truncate" title={event.path + (event.queryString ? `?${event.queryString}` : "")}>
          {event.path}
          {event.queryString ? <span className="text-muted-foreground">?{event.queryString}</span> : null}
        </span>
      </span>

      {/* 来源 */}
      <span className="flex min-w-0 items-center gap-1.5 text-xs">
        <CountryFlag code={event.countryCode} size={14} />
        <span className="shrink-0 font-mono">{event.ip}</span>
        {place ? <span className="min-w-0 truncate text-[11px] text-muted-foreground">{place}</span> : null}
        {/* 窄屏把载荷、异常分、规则挤到来源这一行的末尾 */}
        <span className="ml-auto flex shrink-0 items-center gap-2 @4xl:hidden">
          <TransportBadge transport={event.transport} />
          {score > 0 ? scoreCell : null}
          {ruleId ? <RuleIdPill id={ruleId} /> : null}
        </span>
      </span>

      <span className="hidden @4xl:block">
        <TransportBadge transport={event.transport} />
      </span>
      <span className="hidden text-right @4xl:block">{scoreCell}</span>
      <span className="hidden min-w-0 @4xl:flex">{ruleCell}</span>
    </button>
  );
}
