"use client";

import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Ban, Copy, FlaskConical, Globe2, ListChecks, ListFilter, Loader2, Network, ScrollText, ShieldAlert } from "lucide-react";
import { Fact, useCopy } from "@/components/apps/detail/app-detail-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CountryFlag } from "@/components/ui/country-flag";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  ActionBadge,
  groupName,
  reasonLabel,
  RuleSeverityBadge,
  SEVERITY_META,
  SeverityDot,
  STAGE_META,
  TransportBadge
} from "@/components/firewall/firewall-kit";
import { useCreateIPBanMutation } from "@/lib/admin-hooks";
import type { FirewallEvent, FirewallEventQuery, FirewallMatchedRule } from "@/lib/api/firewall";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { firewallErrorMessage, useFirewallEventQuery, useFirewallRuntimeQuery } from "@/lib/firewall-hooks";
import { useFirewallIntents } from "@/lib/firewall-store";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { buildExclusionDraft, buildSandboxDraft, decodeQuery, isEvaluationRule } from "./event-utils";
import { RulePopover } from "./rule-popover";

type FirewallTab = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/**
 * 事件详情。列表行先垫着显示，详情接口回来后替换：服务端会给 v1 旧事件补上规则说明与所在组，
 * 列表接口不做这一步。
 */
export function EventDetailDialog({
  eventId,
  preview,
  onClose,
  onNavigate,
  onFilter
}: {
  eventId: number | null;
  preview: FirewallEvent | null;
  onClose: () => void;
  onNavigate: (tab: FirewallTab) => void;
  onFilter: (patch: FirewallEventQuery) => void;
}) {
  const detail = useFirewallEventQuery(eventId);
  const event = detail.data && detail.data.id === eventId ? detail.data : preview;

  return (
    <Dialog open={eventId !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[92dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        {event ? (
          <EventBody
            event={event}
            enriched={Boolean(detail.data && detail.data.id === eventId)}
            loadError={detail.isError ? firewallErrorMessage(detail.error, "详情读取失败") : null}
            onNavigate={onNavigate}
            onFilter={(patch) => {
              onClose();
              onFilter(patch);
            }}
          />
        ) : (
          <div className="flex flex-col gap-3 p-6">
            <DialogTitle className="sr-only">事件详情</DialogTitle>
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function EventBody({
  event,
  enriched,
  loadError,
  onNavigate,
  onFilter
}: {
  event: FirewallEvent;
  enriched: boolean;
  loadError: string | null;
  onNavigate: (tab: FirewallTab) => void;
  onFilter: (patch: FirewallEventQuery) => void;
}) {
  const can = usePermissionChecker();
  const canPolicy = can("system:firewall:policy");
  const canBan = can("system:firewall:ban");
  const { setExclusionDraft, setSandboxDraft } = useFirewallIntents();
  const { copy } = useCopy();
  const exclusionDraft = buildExclusionDraft(event);
  const severity = SEVERITY_META[event.severity] ?? SEVERITY_META.medium;

  return (
    <>
      <DialogHeader className="gap-2 border-b px-5 pt-5 pb-4 text-left sm:px-6">
        <div className="flex flex-wrap items-center gap-1.5 pr-8">
          <ActionBadge action={event.action} />
          <Badge variant="outline" size="sm" className="gap-1.5">
            <SeverityDot severity={event.severity} />
            {severity?.label}
          </Badge>
          <TransportBadge transport={event.transport} />
          {loadError ? (
            <span className="text-[11px] text-amber-600 dark:text-amber-400">{loadError}</span>
          ) : !enriched ? (
            <Loader2 className="size-3.5 animate-spin text-muted-foreground" />
          ) : null}
        </div>
        <DialogTitle className="text-base leading-6">{reasonLabel(event.reason)}</DialogTitle>
        <DialogDescription className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
          <span className="tabular-nums">{formatDateTime(event.blockedAt)}</span>
          <span>{formatRelative(event.blockedAt)}</span>
          {event.requestId ? (
            <button
              type="button"
              onClick={() => copy(event.requestId, "请求 ID")}
              className="inline-flex items-center gap-1 font-mono text-[11px] hover:text-foreground"
            >
              {event.requestId}
              <Copy className="size-3" />
            </button>
          ) : null}
        </DialogDescription>
      </DialogHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4 sm:px-6">
        <div className="flex flex-col gap-5">
          <RequestSection event={event} />
          <div className="grid gap-5 md:grid-cols-2">
            <SourceSection event={event} onFilter={onFilter} />
            <VerdictSection event={event} />
          </div>
          {event.ruleRef ? (
            <Section icon={ListFilter} title="自定义规则">
              <div className="flex min-w-0 items-center gap-2 rounded-lg border px-3 py-2">
                <span className="font-mono text-[11px] text-muted-foreground">#{event.ruleRef}</span>
                <span className="min-w-0 flex-1 truncate text-sm">{event.ruleName || "—"}</span>
                <Button size="xs" variant="ghost" onClick={() => onNavigate("rules")}>
                  查看规则
                </Button>
              </div>
            </Section>
          ) : null}
          <MatchesSection matches={event.matches ?? []} onFilterRule={(id) => onFilter({ wafRuleId: id })} />
        </div>
      </div>

      <div className="flex flex-col gap-2 border-t bg-muted/20 px-5 py-3 sm:px-6">
        <div className="flex flex-wrap items-center gap-2">
          {canPolicy && exclusionDraft ? (
            <Button
              size="sm"
              onClick={() => {
                setExclusionDraft(exclusionDraft);
                onNavigate("exclusions");
              }}
            >
              <ListChecks />
              为这个误报建例外
            </Button>
          ) : null}
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setSandboxDraft(buildSandboxDraft(event));
              onNavigate("sandbox");
            }}
          >
            <FlaskConical />
            在测试台复现
          </Button>
          {canBan && event.ip ? <BanIPButton ip={event.ip} reason={reasonLabel(event.reason)} /> : null}
          <Button size="sm" variant="ghost" onClick={() => onFilter({ ip: event.ip })}>
            <ListFilter />
            只看此 IP
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground">请求体不入库，测试台复现只带请求行与请求头。</p>
      </div>
    </>
  );
}

/* ── 分区 ── */

function Section({ icon: Icon, title, aside, children }: { icon: typeof Globe2; title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Icon className="size-3.5" />
        {title}
        {aside ? <span className="ml-auto">{aside}</span> : null}
      </div>
      {children}
    </section>
  );
}

function RequestSection({ event }: { event: FirewallEvent }) {
  const params = decodeQuery(event.queryString);
  const headers = Object.entries(event.headers ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <Section icon={ScrollText} title="请求">
      <div className="min-w-0 rounded-lg border bg-muted/30 px-3 py-2 font-mono text-xs leading-5 break-all">
        <span className="mr-2 font-semibold">{event.method}</span>
        {event.path}
      </div>
      {params.length > 0 ? (
        <KeyValueTable rows={params.map((item) => [item.key, item.value])} label="查询参数" />
      ) : null}
      {event.userAgent ? (
        <div className="min-w-0">
          <div className="mb-1 text-[11px] text-muted-foreground">User-Agent</div>
          <div className="rounded-lg border px-3 py-2 font-mono text-[11px] leading-4 break-all">{event.userAgent}</div>
        </div>
      ) : null}
      {headers.length > 0 ? <KeyValueTable rows={headers} label="请求头" /> : null}
    </Section>
  );
}

function KeyValueTable({ rows, label }: { rows: Array<[string, string]>; label: string }) {
  return (
    <div className="min-w-0">
      <div className="mb-1 text-[11px] text-muted-foreground">
        {label} <span className="tabular-nums">{rows.length}</span>
      </div>
      <dl className="divide-y overflow-hidden rounded-lg border">
        {rows.map(([key, value], index) => (
          <div key={`${key}-${index}`} className="grid min-w-0 grid-cols-1 gap-x-3 px-3 py-1.5 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)]">
            <dt className="truncate font-mono text-[11px] text-muted-foreground" title={key}>
              {key}
            </dt>
            <dd className="min-w-0 font-mono text-[11px] break-all">{value || <span className="text-muted-foreground">（空）</span>}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function SourceSection({ event, onFilter }: { event: FirewallEvent; onFilter: (patch: FirewallEventQuery) => void }) {
  const { copy } = useCopy();
  const place = [event.country, event.region, event.city].filter(Boolean).join(" · ");
  return (
    <Section icon={Network} title="来源">
      <div className="flex flex-col gap-3 rounded-lg border p-3">
        <div className="flex min-w-0 items-center gap-2">
          <CountryFlag code={event.countryCode} size={18} />
          <span className="min-w-0 flex-1 truncate font-mono text-sm font-medium">{event.ip || "—"}</span>
          {event.ip ? (
            <Button size="icon-xs" variant="ghost" aria-label="复制 IP" onClick={() => copy(event.ip, "IP")}>
              <Copy />
            </Button>
          ) : null}
        </div>
        <dl className="grid grid-cols-2 gap-x-3 gap-y-2">
          <Fact
            label="归属地"
            value={
              event.countryCode ? (
                <button type="button" className="truncate hover:underline" onClick={() => onFilter({ country: event.countryCode })}>
                  {place || event.countryCode}
                </button>
              ) : (
                place || "—"
              )
            }
            className="col-span-2"
          />
          <Fact label="运营商" value={event.isp || "—"} />
          <Fact label="ASN" value={event.asn || "—"} mono />
          <Fact label="时区" value={event.timezone || "—"} />
          <Fact label="应用" value={event.appKey || "—"} mono />
        </dl>
      </div>
    </Section>
  );
}

function VerdictSection({ event }: { event: FirewallEvent }) {
  const runtime = useFirewallRuntimeQuery();
  const threshold = runtime.data?.threshold ?? 0;
  const score = event.anomalyScore ?? 0;
  const stage = STAGE_META[event.stage];
  const over = threshold > 0 && score >= threshold;
  const scale = Math.max(score, threshold * 2, 1);

  return (
    <Section icon={ShieldAlert} title="判定">
      <div className="flex flex-col gap-3 rounded-lg border p-3">
        <dl className="grid grid-cols-2 gap-x-3 gap-y-2">
          <Fact label="动作" value={<ActionBadge action={event.action} />} />
          <Fact label="阶段" value={stage?.label ?? event.stage} hint={stage?.hint} />
          <Fact label="载荷来源" value={<TransportBadge transport={event.transport} />} />
          <Fact label="响应" value={`HTTP ${event.httpStatus || "—"}${event.responseCode ? ` · ${event.responseCode}` : ""}`} mono />
        </dl>
        {event.anomalyScore !== undefined && event.anomalyScore !== null ? (
          <div className="flex flex-col gap-1.5 border-t pt-3">
            <div className="flex items-baseline justify-between text-[11px] text-muted-foreground">
              <span>入站异常分</span>
              <span className="tabular-nums">
                <span className={cn("font-data text-base font-semibold", over ? "text-rose-600 dark:text-rose-400" : "text-foreground")}>{score}</span>
                {threshold > 0 ? <span> / 阈值 {threshold}</span> : null}
              </span>
            </div>
            <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <span
                className={cn("absolute inset-y-0 left-0 rounded-full", over ? "bg-rose-500" : "bg-amber-500")}
                style={{ width: `${Math.min(100, (score / scale) * 100)}%` }}
              />
              {threshold > 0 ? (
                <span aria-hidden className="absolute inset-y-0 w-0.5 bg-foreground/60" style={{ left: `${(threshold / scale) * 100}%` }} />
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </Section>
  );
}

function MatchesSection({ matches, onFilterRule }: { matches: FirewallMatchedRule[]; onFilterRule: (id: number) => void }) {
  // 计分规则在前，阻断评估 / 关联统计这类结论规则沉底
  const ordered = [...matches].sort((a, b) => Number(isEvaluationRule(a.id)) - Number(isEvaluationRule(b.id)));
  return (
    <Section icon={ShieldAlert} title="命中规则" aside={<span className="tabular-nums">{matches.length}</span>}>
      {ordered.length === 0 ? (
        <div className="rounded-lg border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">无 WAF 命中</div>
      ) : (
        <ol className="flex flex-col gap-2">
          {ordered.map((match, index) => {
            const evaluation = isEvaluationRule(match.id);
            return (
              <li key={`${match.id}-${index}`} className={cn("flex min-w-0 flex-col gap-2 rounded-lg border p-3", evaluation && "bg-muted/30")}>
                <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                  <RulePopover id={match.id} onFilter={onFilterRule} />
                  <RuleSeverityBadge severity={match.severity} />
                  <span className="text-[11px] text-muted-foreground">{groupName(match.group)}</span>
                  {evaluation ? (
                    <Badge variant="outline" size="sm">
                      评估
                    </Badge>
                  ) : null}
                </div>
                <p className={cn("text-sm leading-5", evaluation && "text-muted-foreground")}>{match.message || "—"}</p>
                {match.targets && match.targets.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {match.targets.map((target) => (
                      <span key={target} className="rounded-md border bg-background px-1.5 py-0.5 font-mono text-[11px] break-all">
                        {target}
                      </span>
                    ))}
                  </div>
                ) : null}
                {match.tags && match.tags.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {match.tags.map((tag) => (
                      <span key={tag} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                        {tag}
                      </span>
                    ))}
                  </div>
                ) : null}
                {match.data ? (
                  <pre className="max-h-40 overflow-auto rounded-md bg-muted/60 px-2.5 py-2 font-mono text-[11px] leading-4 break-all whitespace-pre-wrap">
                    <code>{match.data}</code>
                  </pre>
                ) : null}
              </li>
            );
          })}
        </ol>
      )}
    </Section>
  );
}

/* ── 封禁 ── */

const BAN_DURATIONS = [
  { value: "3600", label: "1 小时" },
  { value: "86400", label: "24 小时" },
  { value: "604800", label: "7 天" },
  { value: "2592000", label: "30 天" },
  { value: "0", label: "永久" }
] as const;

function BanIPButton({ ip, reason }: { ip: string; reason: string }) {
  const [open, setOpen] = useState(false);
  const [duration, setDuration] = useState<string>("86400");
  const create = useCreateIPBanMutation();

  const submit = () => {
    create.mutate(
      { ip, reason: `防火墙事件：${reason}`, duration: Number(duration) },
      {
        onSuccess: () => {
          toast.success(`已封禁 ${ip}`);
          setOpen(false);
        },
        onError: (error) => toast.error(firewallErrorMessage(error, "封禁失败"))
      }
    );
  };

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>
        <Button size="sm" variant="outline" className="text-rose-600 hover:text-rose-700 dark:text-rose-400">
          <Ban />
          封禁此 IP
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>封禁 {ip}</AlertDialogTitle>
          <AlertDialogDescription>该 IP 的所有请求将在入口被拒绝。</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">时长</span>
          <Select value={duration} onValueChange={setDuration}>
            <SelectTrigger className="w-36" size="sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {BAN_DURATIONS.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={create.isPending}
            onClick={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            {create.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
            封禁
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
