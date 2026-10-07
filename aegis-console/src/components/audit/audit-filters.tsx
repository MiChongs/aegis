"use client";

import { useEffect, useState } from "react";
import { CalendarRange, Download, Loader2, RotateCcw, Search, X } from "lucide-react";
import type { AuditFacets, AuditStatItem } from "@/lib/api/types";
import type { AuditListParams } from "@/lib/api/audit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { AUDIT_RANGES, AUDIT_SEVERITIES, AUDIT_STATUSES, AUDIT_VIEWS, type AuditView } from "./audit-shared";

/** 筛选状态。字段名即 URL 查询参数名，便于分享当前视图。 */
export type AuditFilterState = {
  view: AuditView;
  module: string;
  admin: string;
  app: string;
  status: string;
  severity: string;
  q: string;
  session: string;
  range: string; // today / 7d / 30d / custom / ""（不限）
  from: string; // datetime-local
  to: string;
};

export const EMPTY_FILTERS: AuditFilterState = {
  view: "operation",
  module: "",
  admin: "",
  app: "",
  status: "",
  severity: "",
  q: "",
  session: "",
  range: "",
  from: "",
  to: ""
};

export function filtersFromSearch(search: URLSearchParams): AuditFilterState {
  const view = search.get("view");
  return {
    ...EMPTY_FILTERS,
    view: view === "read" || view === "all" ? view : "operation",
    module: search.get("module") ?? "",
    admin: search.get("admin") ?? "",
    app: search.get("app") ?? "",
    status: search.get("status") ?? "",
    severity: search.get("severity") ?? "",
    q: search.get("q") ?? "",
    session: search.get("session") ?? "",
    range: search.get("range") ?? "",
    from: search.get("from") ?? "",
    to: search.get("to") ?? ""
  };
}

export function filtersToSearch(filters: AuditFilterState) {
  const search = new URLSearchParams();
  (Object.keys(filters) as Array<keyof AuditFilterState>).forEach((key) => {
    const value = filters[key];
    if (!value || (key === "view" && value === "operation")) return;
    if ((key === "from" || key === "to") && filters.range !== "custom") return;
    search.set(key, value);
  });
  return search;
}

function startOfToday() {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d;
}

function localToISO(value: string) {
  if (!value) return undefined;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

/** 筛选状态 → 接口参数。时间快捷档在这里换算成具体起点。 */
export function filtersToParams(filters: AuditFilterState): Omit<AuditListParams, "page" | "limit"> {
  let startTime: string | undefined;
  let endTime: string | undefined;
  if (filters.range === "today") startTime = startOfToday().toISOString();
  if (filters.range === "7d") startTime = new Date(startOfToday().getTime() - 6 * 86_400_000).toISOString();
  if (filters.range === "30d") startTime = new Date(startOfToday().getTime() - 29 * 86_400_000).toISOString();
  if (filters.range === "custom") {
    startTime = localToISO(filters.from);
    endTime = localToISO(filters.to);
  }
  return {
    kind: filters.view === "all" ? undefined : filters.view,
    category: filters.module || undefined,
    adminId: filters.admin || undefined,
    appId: filters.app || undefined,
    status: filters.status || undefined,
    severity: filters.severity || undefined,
    keyword: filters.q.trim() || undefined,
    sessionId: filters.session || undefined,
    startTime,
    endTime
  };
}

export function hasActiveFilters(filters: AuditFilterState) {
  return (Object.keys(EMPTY_FILTERS) as Array<keyof AuditFilterState>).some(
    (key) => key !== "view" && key !== "from" && key !== "to" && filters[key] !== EMPTY_FILTERS[key]
  );
}

export function AuditFilters({
  filters,
  facets,
  exporting,
  onChange,
  onExport
}: {
  filters: AuditFilterState;
  facets?: AuditFacets;
  exporting: boolean;
  onChange: (patch: Partial<AuditFilterState>) => void;
  onExport: () => void;
}) {
  // 搜索框本地输入，停顿 350ms 再提交，避免每敲一个字就查一次
  // 外部清除搜索（点筛选标签、清除全部）时同步回输入框：渲染期比较上一次的值，不用 effect
  const [keyword, setKeyword] = useState(filters.q);
  const [syncedQ, setSyncedQ] = useState(filters.q);
  if (filters.q !== syncedQ) {
    setSyncedQ(filters.q);
    setKeyword(filters.q);
  }
  useEffect(() => {
    if (keyword === filters.q) return;
    const timer = setTimeout(() => onChange({ q: keyword }), 350);
    return () => clearTimeout(timer);
  }, [keyword, filters.q, onChange]);

  const chips = activeChips(filters, facets);

  return (
    <div className="space-y-2.5 rounded-xl border bg-card p-3">
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          type="single"
          value={filters.view}
          onValueChange={(value) => value && onChange({ view: value as AuditView })}
          variant="outline"
          size="sm"
          className="shrink-0"
        >
          {AUDIT_VIEWS.map((view) => (
            <ToggleGroupItem key={view.value} value={view.value} className="h-8 px-3 text-xs" title={view.hint}>
              {view.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>

        <div className="relative min-w-[220px] flex-1">
          <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索操作者、对象、应用、IP 或请求 ID"
            className="h-8 pl-8 text-xs"
          />
        </div>

        <Button variant="outline" size="sm" className="h-8" onClick={onExport} disabled={exporting}>
          {exporting ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
          导出 CSV
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <FacetSelect
          placeholder="全部模块"
          value={filters.module}
          items={facets?.modules ?? []}
          onChange={(module) => onChange({ module })}
          className="w-36"
        />
        <FacetSelect
          placeholder="全部操作者"
          value={filters.admin}
          items={facets?.admins ?? []}
          onChange={(admin) => onChange({ admin })}
          className="w-36"
        />
        <FacetSelect
          placeholder="全部应用"
          value={filters.app}
          items={facets?.apps ?? []}
          onChange={(app) => onChange({ app })}
          className="w-36"
        />
        <PlainSelect
          placeholder="全部结果"
          value={filters.status}
          options={AUDIT_STATUSES}
          onChange={(status) => onChange({ status })}
          className="w-28"
        />
        <PlainSelect
          placeholder="全部风险"
          value={filters.severity}
          options={AUDIT_SEVERITIES.map((item) => ({ value: item.value, label: `${item.label}风险` }))}
          onChange={(severity) => onChange({ severity })}
          className="w-28"
        />

        <div className="mx-1 hidden h-5 w-px bg-border sm:block" />

        <ToggleGroup
          type="single"
          value={filters.range === "custom" ? "" : filters.range}
          onValueChange={(range) => onChange({ range: range ?? "" })}
          variant="outline"
          size="sm"
        >
          {AUDIT_RANGES.map((range) => (
            <ToggleGroupItem key={range.value} value={range.value} className="h-8 px-2.5 text-xs">
              {range.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <CustomRange filters={filters} onChange={onChange} />
      </div>

      {chips.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5 border-t pt-2.5">
          <span className="text-[11px] text-muted-foreground">已筛选</span>
          {chips.map((chip) => (
            <button
              key={chip.key}
              type="button"
              onClick={() => onChange(chip.clear)}
              className="inline-flex items-center gap-1 rounded-full border bg-muted/50 px-2 py-0.5 text-[11px] transition-colors hover:bg-muted"
            >
              {chip.label}
              <X className="size-3 text-muted-foreground" />
            </button>
          ))}
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-[11px]"
            onClick={() => onChange({ ...EMPTY_FILTERS, view: filters.view })}
          >
            <RotateCcw className="size-3" />
            清除全部
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function labelOf(items: AuditStatItem[] | undefined, key: string) {
  return items?.find((item) => item.key === key)?.label ?? key;
}

function activeChips(filters: AuditFilterState, facets?: AuditFacets) {
  const chips: Array<{ key: string; label: string; clear: Partial<AuditFilterState> }> = [];
  if (filters.q.trim()) chips.push({ key: "q", label: `搜索：${filters.q.trim()}`, clear: { q: "" } });
  if (filters.module) chips.push({ key: "module", label: `模块：${labelOf(facets?.modules, filters.module)}`, clear: { module: "" } });
  if (filters.admin) chips.push({ key: "admin", label: `操作者：${labelOf(facets?.admins, filters.admin)}`, clear: { admin: "" } });
  if (filters.app) chips.push({ key: "app", label: `应用：${labelOf(facets?.apps, filters.app)}`, clear: { app: "" } });
  if (filters.status) {
    chips.push({ key: "status", label: `结果：${AUDIT_STATUSES.find((s) => s.value === filters.status)?.label ?? filters.status}`, clear: { status: "" } });
  }
  if (filters.severity) {
    chips.push({ key: "severity", label: `风险：${AUDIT_SEVERITIES.find((s) => s.value === filters.severity)?.label ?? filters.severity}`, clear: { severity: "" } });
  }
  if (filters.session) chips.push({ key: "session", label: "同一会话", clear: { session: "" } });
  if (filters.range && filters.range !== "custom") {
    chips.push({ key: "range", label: `时间：${AUDIT_RANGES.find((r) => r.value === filters.range)?.label}`, clear: { range: "" } });
  }
  if (filters.range === "custom" && (filters.from || filters.to)) {
    const text = `${filters.from ? filters.from.replace("T", " ") : "不限"} 至 ${filters.to ? filters.to.replace("T", " ") : "现在"}`;
    chips.push({ key: "custom", label: `时间：${text}`, clear: { range: "", from: "", to: "" } });
  }
  return chips;
}

const ALL = "__all__";

function FacetSelect({
  placeholder,
  value,
  items,
  onChange,
  className
}: {
  placeholder: string;
  value: string;
  items: AuditStatItem[];
  onChange: (value: string) => void;
  className?: string;
}) {
  const selected = items.find((item) => item.key === value);
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger size="sm" className={cn("h-8 text-xs", className)}>
        <span className={cn("truncate", !selected && !value && "text-muted-foreground")}>{selected?.label ?? (value || placeholder)}</span>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{placeholder}</SelectItem>
        {items.map((item) => (
          <SelectItem key={item.key} value={item.key}>
            <span className="flex w-full min-w-40 items-center justify-between gap-3">
              <span className="truncate">{item.label}</span>
              <span className="tabular-nums text-muted-foreground">{item.count}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function PlainSelect({
  placeholder,
  value,
  options,
  onChange,
  className
}: {
  placeholder: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
  className?: string;
}) {
  const selected = options.find((option) => option.value === value);
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger size="sm" className={cn("h-8 text-xs", className)}>
        <span className={cn("truncate", !selected && "text-muted-foreground")}>{selected?.label ?? placeholder}</span>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{placeholder}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function CustomRange({ filters, onChange }: { filters: AuditFilterState; onChange: (patch: Partial<AuditFilterState>) => void }) {
  const [open, setOpen] = useState(false);
  const [from, setFrom] = useState(filters.from);
  const [to, setTo] = useState(filters.to);
  const active = filters.range === "custom";
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) {
          setFrom(filters.from);
          setTo(filters.to);
        }
      }}
    >
      <PopoverTrigger asChild>
        <Button variant={active ? "secondary" : "outline"} size="sm" className="h-8 text-xs">
          <CalendarRange className="size-3.5" />
          自定义
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 space-y-3">
        <div className="space-y-1.5">
          <Label className="text-xs">开始时间</Label>
          <Input type="datetime-local" value={from} onChange={(event) => setFrom(event.target.value)} className="h-8 text-xs" />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs">结束时间</Label>
          <Input type="datetime-local" value={to} onChange={(event) => setTo(event.target.value)} className="h-8 text-xs" />
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>
            取消
          </Button>
          <Button
            size="sm"
            disabled={!from && !to}
            onClick={() => {
              onChange({ range: "custom", from, to });
              setOpen(false);
            }}
          >
            应用
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
