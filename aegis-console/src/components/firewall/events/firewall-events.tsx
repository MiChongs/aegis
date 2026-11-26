"use client";

import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, ListFilter, RefreshCw, ScrollText, Search, SlidersHorizontal, Trash2, X } from "lucide-react";
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
import { Card } from "@/components/ui/card";
import { CountryFlag } from "@/components/ui/country-flag";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { CardEmpty } from "@/components/overview/overview-kit";
import { ListPagination } from "@/components/payment/payment-list-parts";
import { ACTION_META, REASON_LABEL, SEVERITY_META, STAGE_META, TRANSPORT_META } from "@/components/firewall/firewall-kit";
import type { FirewallAction, FirewallEvent, FirewallEventQuery, FirewallStage, FirewallTransport } from "@/lib/api/firewall";
import { firewallErrorMessage, useCleanupFirewallEventsMutation, useFirewallEventsQuery } from "@/lib/firewall-hooks";
import { useFirewallIntents } from "@/lib/firewall-store";
import { usePermissionChecker } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { countryName } from "../overview/overview-parts";
import { FIREWALL_RANGES, nearestRange, rangeStart, type FirewallRangeKey } from "../overview/firewall-range";
import { EventDetailDialog } from "./event-detail";
import { EventListHeader, EventRow } from "./event-row";

type FirewallTab = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

const PAGE_SIZE = 30;
const ALL = "all";

type RangeValue = FirewallRangeKey | typeof ALL;
type SortValue = "latest" | "score";

type Filters = {
  range: RangeValue;
  /** 时间窗的锚点：选定时间窗那一刻；不随实时刷新前移（翻页时窗口不能变） */
  anchor: number;
  action: FirewallAction | "";
  stage: FirewallStage | "";
  transport: FirewallTransport | "";
  severity: string;
  reason: string;
  ip: string;
  pathPattern: string;
  wafRuleId: string;
  keyword: string;
  // 只从总览带过来，没有单独的输入框，以「筛选条件」胶囊显示、可移除
  country: string;
  appKey: string;
  ruleRef: number | null;
  sort: SortValue;
};

const TEXT_KEYS = ["ip", "pathPattern", "wafRuleId", "keyword"] as const;
type TextKey = (typeof TEXT_KEYS)[number];

function initialFilters(intent: FirewallEventQuery | null, now: number): Filters {
  return {
    range: nearestRange(intent?.startTime, now) ?? "24h",
    anchor: now,
    action: intent?.action ?? "",
    stage: intent?.stage ?? "",
    transport: intent?.transport ?? "",
    severity: intent?.severity ?? "",
    reason: intent?.reason ?? "",
    ip: intent?.ip ?? "",
    pathPattern: intent?.pathPattern ?? "",
    wafRuleId: intent?.wafRuleId ? String(intent.wafRuleId) : "",
    keyword: intent?.keyword ?? "",
    country: intent?.country ?? "",
    appKey: intent?.appKey ?? "",
    ruleRef: intent?.ruleRef ?? null,
    sort: intent?.sortBy === "score" ? "score" : "latest"
  };
}

/** 输入框的值 300ms 后才进查询：节流是机器的活 */
function useDebounced<T>(value: T, delay = 300) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

/**
 * 防火墙事件浏览。
 *
 * - 总览或别的页签点进来时带着一份筛选（`useFirewallIntents().eventsFilter`）：挂载时先「看」一眼作为初值，
 *   再在 effect 里取走清空。不在初值函数里直接 take —— 严格模式下初值函数会跑两次，第二次就拿不到了。
 * - 任何筛选变化都回到第 1 页。
 * - 实时模式每 15 秒刷新当前页；翻到后面的页时新事件会把行往后挤，所以默认只在第 1 页有意义。
 */
export function FirewallEventsTab({ onNavigate }: { onNavigate: (tab: FirewallTab) => void }) {
  const [filters, setFilters] = useState<Filters>(() => initialFilters(useFirewallIntents.getState().eventsFilter, Date.now()));
  useEffect(() => {
    useFirewallIntents.getState().takeEventsFilter();
  }, []);

  const [page, setPage] = useState(1);
  const [live, setLive] = useState(true);
  const [showFilters, setShowFilters] = useState(false);
  const [selected, setSelected] = useState<FirewallEvent | null>(null);

  const update = (patch: Partial<Filters>) => {
    setFilters((prev) => ({ ...prev, ...patch }));
    setPage(1);
  };

  const text = useDebounced(
    useMemo(
      () => ({ ip: filters.ip, pathPattern: filters.pathPattern, wafRuleId: filters.wafRuleId, keyword: filters.keyword }),
      [filters.ip, filters.pathPattern, filters.wafRuleId, filters.keyword]
    )
  );

  const query = useMemo<FirewallEventQuery>(() => {
    const ruleId = text.wafRuleId.trim().replace(/^#/, "");
    return {
      page,
      pageSize: PAGE_SIZE,
      startTime: filters.range === ALL ? undefined : rangeStart(filters.range, filters.anchor),
      action: filters.action || undefined,
      stage: filters.stage || undefined,
      transport: filters.transport || undefined,
      severity: filters.severity || undefined,
      reason: filters.reason || undefined,
      ip: text.ip.trim() || undefined,
      pathPattern: text.pathPattern.trim() || undefined,
      wafRuleId: /^\d+$/.test(ruleId) ? Number(ruleId) : undefined,
      keyword: text.keyword.trim() || undefined,
      country: filters.country || undefined,
      appKey: filters.appKey || undefined,
      ruleRef: filters.ruleRef ?? undefined,
      sortBy: filters.sort === "score" ? "score" : "blocked_at",
      sortOrder: "desc"
    };
  }, [filters, text, page]);

  const eventsQuery = useFirewallEventsQuery(query, { live });
  const data = eventsQuery.data;
  const items = data?.items ?? [];

  const advancedCount =
    [filters.action, filters.stage, filters.transport, filters.severity, filters.reason, filters.ip, filters.pathPattern, filters.wafRuleId].filter(Boolean)
      .length;
  const anyFilter = advancedCount > 0 || Boolean(filters.keyword || filters.country || filters.appKey || filters.ruleRef);

  const reset = () =>
    update({
      action: "",
      stage: "",
      transport: "",
      severity: "",
      reason: "",
      ip: "",
      pathPattern: "",
      wafRuleId: "",
      keyword: "",
      country: "",
      appKey: "",
      ruleRef: null
    });

  const applyPatch = (patch: FirewallEventQuery) => {
    const next: Partial<Filters> = {};
    if (patch.ip !== undefined) next.ip = patch.ip;
    if (patch.country !== undefined) next.country = patch.country;
    if (patch.wafRuleId !== undefined) next.wafRuleId = String(patch.wafRuleId);
    if (patch.pathPattern !== undefined) next.pathPattern = patch.pathPattern;
    update(next);
  };

  return (
    <div className="@container flex min-w-0 flex-col gap-3">
      <Card className="min-w-0 gap-0 overflow-hidden py-0">
        {/* 第一行：关键字 + 时间窗 + 排序 + 实时 + 操作 */}
        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-0 flex-1 basis-56">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={filters.keyword}
              onChange={(e) => update({ keyword: e.target.value })}
              placeholder="请求 ID / 路径 / UA / 规则名"
              className="h-9 pl-8"
              aria-label="关键字"
            />
          </div>
          <Select value={filters.range} onValueChange={(value) => update({ range: value as RangeValue, anchor: Date.now() })}>
            <SelectTrigger className="w-28" aria-label="时间范围">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {FIREWALL_RANGES.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  近 {item.label}
                </SelectItem>
              ))}
              <SelectItem value={ALL}>全部时间</SelectItem>
            </SelectContent>
          </Select>
          <Select value={filters.sort} onValueChange={(value) => update({ sort: value as SortValue })}>
            <SelectTrigger className="w-32" aria-label="排序">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="latest">最新</SelectItem>
              <SelectItem value="score">异常分最高</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            className="@4xl:hidden"
            aria-expanded={showFilters}
            onClick={() => setShowFilters((prev) => !prev)}
          >
            <SlidersHorizontal />
            筛选
            {advancedCount > 0 ? (
              <Badge variant="secondary" size="sm" className="tabular-nums">
                {advancedCount}
              </Badge>
            ) : null}
          </Button>
          <div className="ml-auto flex items-center gap-2">
            <Label className="flex h-9 items-center gap-2 rounded-md border px-2.5 text-xs font-normal">
              <Switch checked={live} onCheckedChange={setLive} aria-label="实时刷新" />
              实时
            </Label>
            <Button size="icon" variant="ghost" aria-label="刷新" onClick={() => void eventsQuery.refetch()}>
              <RefreshCw className={cn(eventsQuery.isFetching && "animate-spin")} />
            </Button>
            <CleanupButton />
          </div>
        </div>

        {/* 第二行：结构化筛选；窄屏收进「筛选」 */}
        <div className={cn("grid-cols-2 gap-2 border-b p-3 @xl:grid-cols-4 @4xl:grid-cols-8", showFilters ? "grid" : "hidden @4xl:grid")}>
          <FilterSelect
            label="动作"
            value={filters.action}
            onChange={(value) => update({ action: value as FirewallAction | "" })}
            options={Object.entries(ACTION_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <FilterSelect
            label="阶段"
            value={filters.stage}
            onChange={(value) => update({ stage: value as FirewallStage | "" })}
            options={Object.entries(STAGE_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <FilterSelect
            label="载荷来源"
            value={filters.transport}
            onChange={(value) => update({ transport: value as FirewallTransport | "" })}
            options={Object.entries(TRANSPORT_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <FilterSelect
            label="严重性"
            value={filters.severity}
            onChange={(value) => update({ severity: value })}
            options={Object.entries(SEVERITY_META).map(([value, meta]) => ({ value, label: meta.label }))}
          />
          <FilterSelect
            label="原因"
            value={filters.reason}
            onChange={(value) => update({ reason: value })}
            options={Object.entries(REASON_LABEL).map(([value, label]) => ({ value, label }))}
          />
          <TextFilter label="IP" value={filters.ip} onChange={(value) => update({ ip: value })} mono />
          <TextFilter label="路径" value={filters.pathPattern} onChange={(value) => update({ pathPattern: value })} mono />
          <TextFilter
            label="规则 ID"
            value={filters.wafRuleId}
            onChange={(value) => update({ wafRuleId: value.replace(/[^\d#]/g, "") })}
            mono
            inputMode="numeric"
          />
        </div>

        {/* 从别处带来的条件 + 清空 */}
        {anyFilter ? (
          <div className="flex flex-wrap items-center gap-1.5 border-b bg-muted/20 px-3 py-2">
            <ListFilter className="size-3.5 text-muted-foreground" />
            {filters.country ? (
              <FilterChip onRemove={() => update({ country: "" })}>
                <CountryFlag code={filters.country} size={12} />
                {countryName(filters.country)}
              </FilterChip>
            ) : null}
            {filters.appKey ? (
              <FilterChip onRemove={() => update({ appKey: "" })}>
                应用 <span className="font-mono">{filters.appKey}</span>
              </FilterChip>
            ) : null}
            {filters.ruleRef ? <FilterChip onRemove={() => update({ ruleRef: null })}>自定义规则 #{filters.ruleRef}</FilterChip> : null}
            {TEXT_KEYS.filter((key) => filters[key]).map((key) => (
              <FilterChip key={key} onRemove={() => update({ [key]: "" } as Pick<Filters, TextKey>)}>
                {TEXT_LABEL[key]} <span className="max-w-40 truncate font-mono">{filters[key]}</span>
              </FilterChip>
            ))}
            <Button size="xs" variant="ghost" className="ml-auto text-muted-foreground" onClick={reset}>
              清空筛选
            </Button>
          </div>
        ) : null}

        <EventListHeader />
        {eventsQuery.isError && !data ? (
          <div className="p-4">
            <CardEmpty icon={AlertTriangle} title={firewallErrorMessage(eventsQuery.error, "事件读取失败")} />
          </div>
        ) : !data ? (
          <div className="flex flex-col">
            {Array.from({ length: 8 }).map((_, index) => (
              <div key={index} className="border-b px-4 py-3 last:border-b-0">
                <Skeleton className="h-8 w-full rounded-md" />
              </div>
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="p-4">
            <CardEmpty icon={ScrollText} title={anyFilter ? "没有符合条件的事件" : "暂无事件"} />
          </div>
        ) : (
          <div className={cn("flex flex-col transition-opacity", eventsQuery.isPlaceholderData && "opacity-60")}>
            {items.map((event) => (
              <EventRow key={event.id} event={event} active={selected?.id === event.id} onOpen={() => setSelected(event)} />
            ))}
          </div>
        )}
        {data && data.total > 0 ? (
          <ListPagination page={data.page || page} totalPages={Math.max(1, data.totalPages)} total={data.total} unit="条" onPage={setPage} />
        ) : null}
      </Card>

      <EventDetailDialog
        eventId={selected?.id ?? null}
        preview={selected}
        onClose={() => setSelected(null)}
        onNavigate={onNavigate}
        onFilter={applyPatch}
      />
    </div>
  );
}

const TEXT_LABEL: Record<TextKey, string> = {
  ip: "IP",
  pathPattern: "路径",
  wafRuleId: "规则",
  keyword: "关键字"
};

/* ── 筛选控件 ── */

function FilterSelect({
  label,
  value,
  onChange,
  options
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger size="sm" aria-label={label} className={cn("w-full min-w-0", value && "border-foreground/25 bg-muted/40")}>
        <span className="shrink-0 text-muted-foreground">{label}</span>
        <span className="min-w-0 flex-1 truncate text-left">
          <SelectValue />
        </span>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>全部</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function TextFilter({
  label,
  value,
  onChange,
  mono,
  inputMode
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  mono?: boolean;
  inputMode?: React.HTMLAttributes<HTMLInputElement>["inputMode"];
}) {
  return (
    <div className="relative min-w-0">
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={label}
        aria-label={label}
        inputMode={inputMode}
        className={cn("h-8 pr-7 text-xs", mono && "font-mono", value && "border-foreground/25 bg-muted/40")}
      />
      {value ? (
        <button
          type="button"
          aria-label={`清除${label}`}
          onClick={() => onChange("")}
          className="absolute top-1/2 right-1.5 grid size-5 -translate-y-1/2 place-items-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      ) : null}
    </div>
  );
}

function FilterChip({ children, onRemove }: { children: React.ReactNode; onRemove: () => void }) {
  return (
    <span className="inline-flex h-6 max-w-full items-center gap-1 rounded-md border bg-background pr-0.5 pl-2 text-[11px]">
      {children}
      <button
        type="button"
        aria-label="移除条件"
        onClick={onRemove}
        className="grid size-5 place-items-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <X className="size-3" />
      </button>
    </span>
  );
}

/* ── 清理 ── */

const CLEANUP_OPTIONS = [
  { value: "7", label: "7 天前" },
  { value: "30", label: "30 天前" },
  { value: "90", label: "90 天前" }
] as const;

function CleanupButton() {
  const allowed = usePermissionChecker()("system:firewall:log_clean");
  const [open, setOpen] = useState(false);
  const [days, setDays] = useState<string>("30");
  const cleanup = useCleanupFirewallEventsMutation();
  if (!allowed) return null;

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>
        <Button size="icon" variant="ghost" aria-label="清理旧事件" title="清理旧事件">
          <Trash2 />
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>清理旧事件</AlertDialogTitle>
          <AlertDialogDescription>所选时间之前的事件将被永久删除，无法恢复。</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">删除</span>
          <Select value={days} onValueChange={setDays}>
            <SelectTrigger size="sm" className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CLEANUP_OPTIONS.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-sm text-muted-foreground">的事件</span>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={cleanup.isPending}
            className="bg-destructive text-white hover:bg-destructive/90"
            onClick={(e) => {
              e.preventDefault();
              const before = new Date(Date.now() - Number(days) * 86_400_000).toISOString();
              cleanup.mutate(before, { onSuccess: () => setOpen(false) });
            }}
          >
            清理
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
