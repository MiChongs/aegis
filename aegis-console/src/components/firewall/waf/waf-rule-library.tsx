"use client";

import { useDeferredValue, useMemo, useState } from "react";
import { ArrowRight, Library, Loader2, Search, Undo2, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { VirtualList } from "@/components/ui/virtual-list";
import { Panel } from "@/components/traffic/traffic-kit";
import type { FirewallCatalog, FirewallRuleInfo } from "@/lib/api/firewall";
import { formatDateTime } from "@/lib/datetime";
import { useFirewallCatalogRuleQuery } from "@/lib/firewall-hooks";
import { cn } from "@/lib/utils";
import { ActionBadge, RULE_SEVERITY_META, RuleIdPill, RuleSeverityBadge, groupName } from "../firewall-kit";
import type { SettingsForm } from "./settings-draft";
import { PHASE_LABEL, RULE_STATUS_META, RuleStatusBadge, makeRuleStatus, type FirewallTab, type RuleStatus } from "./waf-shared";

const ROW_HEIGHT = 60;
const ALL = "all";

type Row = FirewallRuleInfo & { status: RuleStatus };

export function RuleLibraryPanel({
  form,
  catalog,
  dirty,
  onOpenRule
}: {
  form: SettingsForm;
  catalog?: FirewallCatalog;
  /** 有未保存的修改时，状态按草稿预览 */
  dirty: boolean;
  onOpenRule: (id: number) => void;
}) {
  const [query, setQuery] = useState("");
  const [group, setGroup] = useState(ALL);
  const [severity, setSeverity] = useState(ALL);
  const [status, setStatus] = useState(ALL);
  const [paranoia, setParanoia] = useState(ALL);
  const deferredQuery = useDeferredValue(query);

  const rows = useMemo<Row[]>(() => {
    if (!catalog) return [];
    const statusOf = makeRuleStatus(form, catalog.groups);
    return catalog.rules.map((rule) => ({ ...rule, status: statusOf(rule) }));
  }, [catalog, form]);

  const statusCounts = useMemo(() => {
    const out: Partial<Record<RuleStatus, number>> = {};
    for (const row of rows) out[row.status] = (out[row.status] ?? 0) + 1;
    return out;
  }, [rows]);

  const filtered = useMemo(() => {
    const q = deferredQuery.trim().toLowerCase().replace(/^#/, "");
    return rows.filter((row) => {
      if (group !== ALL && row.group !== group) return false;
      if (severity !== ALL && row.severity !== severity) return false;
      if (status !== ALL && row.status !== status) return false;
      if (paranoia !== ALL && row.paranoia !== Number(paranoia)) return false;
      if (!q) return true;
      return (
        String(row.id).startsWith(q) ||
        row.message.toLowerCase().includes(q) ||
        (row.tags ?? []).some((tag) => tag.toLowerCase().includes(q))
      );
    });
  }, [deferredQuery, group, paranoia, rows, severity, status]);

  const filtering = Boolean(query) || group !== ALL || severity !== ALL || status !== ALL || paranoia !== ALL;
  const reset = () => {
    setQuery("");
    setGroup(ALL);
    setSeverity(ALL);
    setStatus(ALL);
    setParanoia(ALL);
  };

  return (
    <Panel
      icon={Library}
      tone="muted"
      title="规则库"
      aside={
        catalog ? (
          <span className="text-xs text-muted-foreground tabular-nums">
            {filtering ? `${filtered.length} / ` : ""}
            {catalog.totalRules} 条 · 生效 {statusCounts.active ?? 0}
          </span>
        ) : null
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        <div className="grid min-w-0 grid-cols-2 gap-2 @3xl:grid-cols-[minmax(0,1fr)_repeat(4,auto)]">
          <div className="relative col-span-2 min-w-0 @3xl:col-span-1">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="规则编号、说明或标签" className="h-8 pl-8" aria-label="搜索规则" />
          </div>
          <FilterSelect
            value={group}
            onChange={setGroup}
            label="规则组"
            options={(catalog?.groups ?? []).map((item) => ({ value: item.code, label: `${item.code} ${item.name || groupName(item.code)}` }))}
          />
          <FilterSelect
            value={severity}
            onChange={setSeverity}
            label="严重性"
            options={Object.entries(RULE_SEVERITY_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <FilterSelect
            value={status}
            onChange={setStatus}
            label="状态"
            options={(Object.keys(RULE_STATUS_META) as RuleStatus[]).map((value) => ({
              value,
              label: `${RULE_STATUS_META[value].label} ${statusCounts[value] ?? 0}`
            }))}
          />
          <FilterSelect value={paranoia} onChange={setParanoia} label="偏执等级" options={[1, 2, 3, 4].map((level) => ({ value: String(level), label: `PL${level}` }))} />
        </div>

        <div className="flex min-h-5 flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {dirty ? <span className="rounded bg-amber-500/12 px-1.5 leading-5 text-amber-700 dark:text-amber-300">状态按未保存的设置预览</span> : null}
          {!form.corazaEnabled ? <span className="rounded bg-muted px-1.5 leading-5">WAF 已关闭，以下规则都不检查</span> : null}
          {filtering ? (
            <Button type="button" size="xs" variant="ghost" onClick={reset}>
              <X />
              清除筛选
            </Button>
          ) : null}
        </div>

        <div className="min-w-0 overflow-hidden rounded-xl border border-border">
          <div className="hidden h-9 items-center gap-3 border-b border-border bg-muted/40 px-3 text-[11px] font-medium text-muted-foreground @3xl:grid @3xl:grid-cols-[76px_132px_minmax(0,1fr)_52px_40px_56px_92px]">
            <span>编号</span>
            <span>规则组</span>
            <span>说明</span>
            <span>严重性</span>
            <span>等级</span>
            <span>阶段</span>
            <span>状态</span>
          </div>
          {!catalog ? (
            <div className="flex flex-col gap-1 p-2">
              {Array.from({ length: 8 }, (_, index) => (
                <Skeleton key={index} className="h-12 rounded-md" />
              ))}
            </div>
          ) : (
            <VirtualList
              items={filtered}
              estimateSize={ROW_HEIGHT}
              height={Math.min(520, Math.max(160, filtered.length * ROW_HEIGHT))}
              getItemKey={(index) => filtered[index]?.id ?? index}
              ariaLabel="规则库"
              className="rounded-none"
              empty="没有匹配的规则"
              renderItem={(row) => <RuleRow row={row} onOpen={onOpenRule} />}
            />
          )}
        </div>
      </div>
    </Panel>
  );
}

function FilterSelect({
  value,
  onChange,
  label,
  options
}: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  options: { value: string; label: string }[];
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger size="sm" className={cn("w-full min-w-0 @3xl:w-auto @3xl:min-w-28", value !== ALL && "border-primary/50")} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="max-h-80">
        <SelectItem value={ALL}>全部{label}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function RuleRow({ row, onOpen }: { row: Row; onOpen: (id: number) => void }) {
  const muted = row.status !== "active";
  return (
    <button
      type="button"
      onClick={() => onOpen(row.id)}
      className="flex h-full w-full min-w-0 items-center border-b border-border px-3 text-left transition-colors hover:bg-accent/50"
    >
      {/* 窄屏：两行 */}
      <div className="flex w-full min-w-0 flex-col gap-1 @3xl:hidden">
        <div className="flex min-w-0 items-center gap-1.5">
          <RuleIdPill id={row.id} />
          <RuleSeverityBadge severity={row.severity} />
          <span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
            {groupName(row.group)} · PL{row.paranoia}
          </span>
          <RuleStatusBadge status={row.status} />
        </div>
        <span className={cn("truncate text-xs", muted && "text-muted-foreground")}>{row.message}</span>
      </div>
      {/* 宽屏：一行 */}
      <div className="hidden w-full min-w-0 items-center gap-3 @3xl:grid @3xl:grid-cols-[76px_132px_minmax(0,1fr)_52px_40px_56px_92px]">
        <RuleIdPill id={row.id} className="w-fit" />
        <span className="truncate text-xs text-muted-foreground">
          <span className="font-mono">{row.group}</span> {groupName(row.group)}
        </span>
        <span className="flex min-w-0 flex-col">
          <span className={cn("truncate text-xs", muted && "text-muted-foreground")} title={row.message}>
            {row.message}
          </span>
          {row.tags?.length ? <span className="truncate font-mono text-[10px] text-muted-foreground">{row.tags.join(" · ")}</span> : null}
        </span>
        <span>
          <RuleSeverityBadge severity={row.severity} />
        </span>
        <span className="font-mono text-xs tabular-nums">PL{row.paranoia}</span>
        <span className="text-xs text-muted-foreground">{PHASE_LABEL[row.phase] ?? (row.phase || "—")}</span>
        <span>
          <RuleStatusBadge status={row.status} />
        </span>
      </div>
    </button>
  );
}

/* ── 规则详情 ── */

export function RuleDetailDialog({
  ruleId,
  onClose,
  form,
  catalog,
  canWrite,
  onToggleRemoved,
  onNavigate
}: {
  ruleId: number | null;
  onClose: () => void;
  form: SettingsForm;
  catalog?: FirewallCatalog;
  canWrite: boolean;
  onToggleRemoved: (id: number, removed: boolean) => void;
  onNavigate: (tab: FirewallTab) => void;
}) {
  const detail = useFirewallCatalogRuleQuery(ruleId);
  const data = detail.data && detail.data.id === ruleId ? detail.data : undefined;
  const fallback = catalog?.rules.find((rule) => rule.id === ruleId);
  const rule = data ?? fallback;
  const status = rule && catalog ? makeRuleStatus(form, catalog.groups)(rule) : null;
  const removedInDraft = ruleId !== null && form.corazaRemovedRules.includes(String(ruleId));

  return (
    <Dialog open={ruleId !== null} onOpenChange={(open) => (!open ? onClose() : undefined)}>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-center gap-2">
            规则 <span className="font-mono tabular-nums">#{ruleId}</span>
            {rule ? <RuleSeverityBadge severity={rule.severity} /> : null}
            {status ? <RuleStatusBadge status={status} /> : null}
          </DialogTitle>
          <DialogDescription className="break-words">{rule?.message ?? (detail.isError ? "目录中没有这条规则" : "加载中")}</DialogDescription>
        </DialogHeader>

        {rule ? (
          <div className="grid min-w-0 grid-cols-2 gap-3 rounded-xl border border-border p-3 sm:grid-cols-4">
            <Meta label="规则组" value={`${rule.group} ${data?.groupName || groupName(rule.group)}`} />
            <Meta label="偏执等级" value={`PL${rule.paranoia}`} />
            <Meta label="阶段" value={PHASE_LABEL[rule.phase] ?? String(rule.phase || "—")} />
            <Meta label="最近命中" value={data ? `${data.recentTotal} 次` : "—"} />
            {rule.tags?.length ? (
              <div className="col-span-2 flex min-w-0 flex-wrap gap-1 sm:col-span-4">
                {rule.tags.map((tag) => (
                  <Badge key={tag} variant="outline" size="sm" className="font-mono">
                    {tag}
                  </Badge>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}

        <div className="flex min-w-0 flex-col gap-2">
          <div className="text-xs font-medium text-muted-foreground">最近命中</div>
          {detail.isLoading ? (
            <div className="flex items-center gap-2 py-4 text-xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" />
              加载中
            </div>
          ) : data?.recent?.length ? (
            <div className="flex min-w-0 flex-col divide-y divide-border rounded-xl border border-border">
              {data.recent.map((event) => (
                <div key={event.id} className="flex min-w-0 flex-col gap-1 px-3 py-2 sm:flex-row sm:items-center sm:gap-3">
                  <div className="flex shrink-0 items-center gap-2">
                    <ActionBadge action={event.action} />
                    <span className="text-[11px] text-muted-foreground tabular-nums">{formatDateTime(event.blockedAt)}</span>
                  </div>
                  <span className="shrink-0 font-mono text-xs tabular-nums">{event.ip}</span>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground" title={`${event.method} ${event.path}`}>
                    {event.method} {event.path}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <div className="rounded-xl border border-dashed border-border py-5 text-center text-xs text-muted-foreground">暂无命中</div>
          )}
        </div>

        <DialogFooter className="gap-2 sm:justify-between">
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              onClose();
              onNavigate("exclusions");
            }}
          >
            按路径或字段排除
            <ArrowRight />
          </Button>
          {canWrite && ruleId !== null ? (
            <Button type="button" variant={removedInDraft ? "secondary" : "destructive"} onClick={() => onToggleRemoved(ruleId, !removedInDraft)}>
              {removedInDraft ? <Undo2 /> : <X />}
              {removedInDraft ? "取消全局移除" : "全局移除"}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="truncate text-sm font-medium tabular-nums" title={value}>
        {value}
      </div>
    </div>
  );
}
