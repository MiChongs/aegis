"use client";

import { useMemo, useState, type ReactNode } from "react";
import {
  AlertTriangle,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock,
  ClipboardCopy,
  Copy,
  Download,
  Eye,
  Infinity as InfinityIcon,
  ListFilter,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Timer,
  Unlock,
  X
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { CountryFlag } from "@/components/ui/country-flag";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CardEmpty } from "@/components/overview/overview-kit";
import { Segmented } from "@/components/traffic/traffic-kit";
import { useAdminToken } from "@/lib/admin-hooks";
import { firewallBansApi, type BanFilter } from "@/lib/api/firewall-bans";
import type { IPBanEntry } from "@/lib/api/types";
import { formatDateTime, formatRelative } from "@/lib/datetime";
import { banErrorMessage, useBanListQuery, useBanStatsQuery } from "@/lib/firewall-ban-hooks";
import { cn } from "@/lib/utils";
import { countryName } from "../overview/overview-parts";
import { MAX_BAN_BATCH, type EditRequest, type UnbanRequest } from "./ban-dialogs";
import {
  banPlace,
  banTimeState,
  downloadBansCSV,
  isActiveBan,
  SOURCE_KEYS,
  SOURCE_META,
  SourceBadge,
  STATUS_META,
  TIME_TONE,
  useBanModes,
  useNow,
  type BanModes
} from "./ban-format";
import { activeFilterCount, PAGE_SIZES, type BanListFilter, type BanListState, type BanSort, type BanStatusFilter } from "./ban-list-state";

/**
 * IP 封禁列表：筛选、多选、批量操作、分页。
 *
 * 选择分两种：勾选（ids，跨页保留，批量修改只在这种模式下可用）与「按筛选全选」（filter，
 * 解封时把筛选与确认时看到的条数一起交给后端，条数变了后端拒绝，见 ban-dialogs）。
 * 容器 ≥ 64rem 时是一行一格的密表，窄屏折成卡片。
 */

const ALL = "__all__";
const EXPORT_PAGE = 500;
const EXPORT_MAX = 50_000;

const STATUS_OPTIONS: ReadonlyArray<{ value: BanStatusFilter; label: string }> = [
  { value: "active", label: "生效中" },
  { value: "expired", label: "已过期" },
  { value: "revoked", label: "已解除" },
  { value: "all", label: "全部" }
];

const SORT_OPTIONS: ReadonlyArray<{ value: BanSort; label: string }> = [
  { value: "created_desc", label: "最新" },
  { value: "created_asc", label: "最早" },
  { value: "expires_asc", label: "即将到期" },
  { value: "ip", label: "IP" }
];

/** 宽屏一行一格；窄屏是卡片 */
const ROW_GRID =
  "@5xl:grid @5xl:grid-cols-[1rem_minmax(0,10rem)_minmax(0,1fr)_6.5rem_7rem_6.5rem_minmax(0,1.15fr)_4.5rem_1.75rem] @5xl:items-center @5xl:gap-3";

export function BanList({
  state,
  canBan,
  onInspect,
  onUnban,
  onEdit
}: {
  state: BanListState;
  canBan: boolean;
  onInspect: (ip: string) => void;
  onUnban: (target: UnbanRequest) => void;
  onEdit: (target: EditRequest) => void;
}) {
  const { filter, update, reset, page, setPage, pageSize, setPageSize, selection, setSelection, clearSelection, query, pageQuery } = state;
  const now = useNow();
  const modes = useBanModes();
  const listQuery = useBanListQuery(pageQuery);
  const data = listQuery.data;
  const items = useMemo(() => data?.items ?? [], [data]);
  const total = data?.total ?? 0;
  const [showFilters, setShowFilters] = useState(false);

  const filterCount = activeFilterCount(filter);
  const anyFilter = filterCount > 0;

  /* ── 选择 ── */
  const filterMode = selection.kind === "filter";
  const isSelected = (id: number) => (selection.kind === "filter" ? true : selection.rows.has(id));
  const pageSelected = items.filter((ban) => isSelected(ban.id)).length;
  const pageAll = items.length > 0 && pageSelected === items.length;
  const selectedCount = selection.kind === "filter" ? total : selection.rows.size;
  const selectedRows = useMemo(() => (selection.kind === "ids" ? [...selection.rows.values()] : []), [selection]);

  const toggleRow = (ban: IPBanEntry) => {
    if (selection.kind === "filter") {
      // 从「全部匹配」里去掉一条：退回勾选本页其余各条
      setSelection({ kind: "ids", rows: new Map(items.filter((row) => row.id !== ban.id).map((row) => [row.id, row] as const)) });
      return;
    }
    const rows = new Map(selection.rows);
    if (rows.has(ban.id)) rows.delete(ban.id);
    else rows.set(ban.id, ban);
    setSelection({ kind: "ids", rows });
  };

  const togglePage = () => {
    if (selection.kind === "filter") {
      clearSelection();
      return;
    }
    const rows = new Map(selection.rows);
    if (pageAll) items.forEach((ban) => rows.delete(ban.id));
    else items.forEach((ban) => rows.set(ban.id, ban));
    setSelection({ kind: "ids", rows });
  };

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <Card className="min-w-0 gap-0 overflow-hidden py-0">
        {/* 第一行：关键字 + 状态 + 排序 + 刷新 */}
        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-0 flex-1 basis-56">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={filter.keyword}
              onChange={(event) => update({ keyword: event.target.value })}
              placeholder="IP 前缀 / 原因 / 规则 / 运营商"
              className="h-9 pr-8 pl-8"
              aria-label="关键字"
              spellCheck={false}
            />
            {filter.keyword ? (
              <button
                type="button"
                aria-label="清除关键字"
                onClick={() => update({ keyword: "" })}
                className="absolute top-1/2 right-2 grid size-5 -translate-y-1/2 place-items-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <X className="size-3.5" />
              </button>
            ) : null}
          </div>
          <Segmented value={filter.status} options={STATUS_OPTIONS} onChange={(status) => update({ status })} ariaLabel="状态" />
          <Select value={filter.sort} onValueChange={(sort) => update({ sort: sort as BanSort })}>
            <SelectTrigger className="w-28" aria-label="排序">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SORT_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" className="@4xl:hidden" aria-expanded={showFilters} onClick={() => setShowFilters((prev) => !prev)}>
            <SlidersHorizontal />
            筛选
            {filterCount > 0 ? (
              <Badge variant="secondary" size="sm" className="tabular-nums">
                {filterCount}
              </Badge>
            ) : null}
          </Button>
          <Button size="icon" variant="ghost" className="ml-auto" aria-label="刷新" onClick={() => void listQuery.refetch()}>
            <RefreshCw className={cn(listQuery.isFetching && "animate-spin")} />
          </Button>
        </div>

        {/* 第二行：结构化筛选与快捷条件；窄屏收进「筛选」 */}
        <div className={cn("flex-wrap items-center gap-2 border-b p-3", showFilters ? "flex" : "hidden @4xl:flex")}>
          <div className="grid min-w-0 flex-1 grid-cols-1 gap-2 @md:grid-cols-3 @4xl:max-w-2xl">
            <FilterSelect
              label="来源"
              value={filter.source}
              onChange={(source) => update({ source })}
              options={SOURCE_KEYS.map((key) => ({ value: key, label: SOURCE_META[key]!.label }))}
            />
            <FilterSelect
              label="模式"
              value={filter.mode}
              onChange={(mode) => update({ mode })}
              options={[{ value: "default", label: "默认" }, ...modes.modes.map((mode) => ({ value: mode.value as string, label: mode.label }))]}
            />
            <CountrySelect value={filter.country} onChange={(country) => update({ country })} />
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            <QuickChip
              icon={Clock}
              active={filter.expiring}
              onClick={() => update(filter.expiring ? { expiring: false } : { expiring: true, permanent: "", sort: "expires_asc" })}
            >
              24 小时内到期
            </QuickChip>
            <QuickChip icon={InfinityIcon} active={filter.permanent === "true"} onClick={() => update({ permanent: filter.permanent === "true" ? "" : "true", expiring: false })}>
              仅永久
            </QuickChip>
            <QuickChip icon={Timer} active={filter.permanent === "false"} onClick={() => update({ permanent: filter.permanent === "false" ? "" : "false" })}>
              仅限期
            </QuickChip>
          </div>
        </div>

        {/* 生效中的条件 */}
        {anyFilter ? <ActiveFilters filter={filter} modes={modes} update={update} reset={reset} /> : null}

        {/* 全选提示 */}
        {pageAll && total > items.length ? (
          <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 border-b bg-primary/5 px-4 py-2 text-xs">
            {filterMode ? (
              <>
                <span>
                  已选择全部 <span className="font-medium tabular-nums">{total.toLocaleString("zh-CN")}</span> 条匹配的封禁。
                </span>
                <Button size="xs" variant="ghost" className="text-primary" onClick={clearSelection}>
                  取消选择
                </Button>
              </>
            ) : (
              <>
                <span>
                  已选择本页 <span className="font-medium tabular-nums">{items.length}</span> 条。
                </span>
                <Button size="xs" variant="ghost" className="text-primary" onClick={() => setSelection({ kind: "filter" })}>
                  选择全部 {total.toLocaleString("zh-CN")} 条匹配的封禁
                </Button>
              </>
            )}
          </div>
        ) : null}

        {/* 表头（宽屏）/ 全选本页（窄屏） */}
        {items.length > 0 ? (
          <div className={cn("flex items-center gap-3 border-b bg-muted/30 px-4 py-2 text-[11px] font-medium text-muted-foreground", ROW_GRID)}>
            <Checkbox
              checked={pageAll ? true : pageSelected > 0 ? "indeterminate" : false}
              onCheckedChange={togglePage}
              aria-label="选择本页"
              className="bg-background"
            />
            <span className="@5xl:hidden">{pageSelected > 0 ? `本页已选 ${pageSelected} 条` : "选择本页"}</span>
            <span className="hidden @5xl:block">IP</span>
            <span className="hidden @5xl:block">归属地 / 运营商</span>
            <span className="hidden @5xl:block">来源</span>
            <span className="hidden @5xl:block">状态</span>
            <span className="hidden @5xl:block">响应模式</span>
            <span className="hidden @5xl:block">原因 / 触发规则</span>
            <span className="hidden @5xl:block">封禁于</span>
            <span className="hidden @5xl:block" />
          </div>
        ) : null}

        {listQuery.isError && !data ? (
          <div className="p-4">
            <CardEmpty icon={AlertTriangle} title={banErrorMessage(listQuery.error, "封禁列表读取失败")} />
          </div>
        ) : !data ? (
          <div className="flex flex-col">
            {Array.from({ length: 8 }).map((_, index) => (
              <div key={index} className="border-b px-4 py-3 last:border-b-0">
                <Skeleton className="h-9 w-full rounded-md" />
              </div>
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 p-6">
            <CardEmpty
              icon={ShieldCheck}
              title={anyFilter ? "没有符合条件的封禁" : filter.status === "active" ? "当前没有生效中的封禁" : "暂无记录"}
              description={anyFilter ? "换个条件试试，或清空筛选。" : undefined}
            />
            {anyFilter ? (
              <Button size="sm" variant="outline" onClick={reset}>
                清空筛选
              </Button>
            ) : null}
          </div>
        ) : (
          <div className={cn("flex flex-col transition-opacity", listQuery.isPlaceholderData && "opacity-60")}>
            {items.map((ban) => (
              <BanRow
                key={ban.id}
                ban={ban}
                now={now}
                modes={modes}
                selected={isSelected(ban.id)}
                canBan={canBan}
                onToggle={() => toggleRow(ban)}
                onInspect={() => onInspect(ban.ip)}
                onEdit={() => onEdit({ kind: "single", ban })}
                onUnban={() => onUnban({ kind: "ids", rows: [ban] })}
              />
            ))}
          </div>
        )}

        {data && total > 0 ? (
          <Pagination page={data.page || page} totalPages={Math.max(1, data.totalPages)} total={total} pageSize={pageSize} onPage={setPage} onPageSize={setPageSize} />
        ) : null}
      </Card>

      {selectedCount > 0 ? (
        <BulkBar
          count={selectedCount}
          filterMode={filterMode}
          rows={selectedRows}
          query={query}
          total={total}
          now={now}
          modes={modes}
          canBan={canBan}
          onClear={clearSelection}
          onUnban={() => onUnban(filterMode ? { kind: "filter", filter: query } : { kind: "ids", rows: selectedRows })}
          onEdit={(kind) => onEdit({ kind, rows: selectedRows })}
        />
      ) : null}
    </div>
  );
}

/* ── 行 ── */

function BanRow({
  ban,
  now,
  modes,
  selected,
  canBan,
  onToggle,
  onInspect,
  onEdit,
  onUnban
}: {
  ban: IPBanEntry;
  now: number;
  modes: BanModes;
  selected: boolean;
  canBan: boolean;
  onToggle: () => void;
  onInspect: () => void;
  onEdit: () => void;
  onUnban: () => void;
}) {
  const active = isActiveBan(ban, now);
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard.writeText(ban.ip).then(
      () => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1500);
      },
      () => toast.error("复制失败")
    );
  };

  const place = banPlace(ban);
  const status = <StatusCell ban={ban} now={now} />;
  const mode = <ModeCell ban={ban} modes={modes} />;
  const created = (
    <span className="text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(ban.createdAt)}>
      {formatRelative(ban.createdAt, "—", now)}
    </span>
  );

  return (
    <div
      className={cn(
        "group relative flex min-w-0 items-start gap-3 border-b px-4 py-3 transition-colors last:border-b-0 hover:bg-muted/40",
        selected && "bg-primary/5 hover:bg-primary/8",
        ROW_GRID,
        "@5xl:py-2"
      )}
    >
      <span className="flex h-5 items-center @5xl:h-auto">
        <Checkbox checked={selected} onCheckedChange={onToggle} aria-label={`选择 ${ban.ip}`} />
      </span>

      <div className="flex min-w-0 flex-1 flex-col gap-1.5 @5xl:contents">
        {/* IP（窄屏同一行带状态） */}
        <div className="flex min-w-0 items-center gap-2 @5xl:contents">
          <span className="flex min-w-0 items-center gap-0.5">
            <button
              type="button"
              onClick={onInspect}
              title={ban.ip}
              className="min-w-0 truncate rounded-sm text-left font-mono text-[13px] font-medium hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              {ban.ip}
            </button>
            <Button
              size="icon-xs"
              variant="ghost"
              aria-label="复制 IP"
              onClick={copy}
              className="shrink-0 text-muted-foreground @5xl:opacity-0 @5xl:group-hover:opacity-100 @5xl:focus-visible:opacity-100"
            >
              {copied ? <Check /> : <Copy />}
            </Button>
          </span>
          <span className="ml-auto shrink-0 @5xl:hidden">{status}</span>
        </div>

        {/* 归属地 / 运营商 */}
        <span className="flex min-w-0 items-center gap-1.5 text-xs">
          {ban.countryCode ? <CountryFlag code={ban.countryCode} size={14} /> : null}
          <span className="min-w-0 truncate" title={[place, ban.isp].filter(Boolean).join(" · ")}>
            {place || <span className="text-muted-foreground">未知</span>}
            {ban.isp ? <span className="text-muted-foreground"> · {ban.isp}</span> : null}
          </span>
        </span>

        <span className="hidden min-w-0 @5xl:flex">
          <SourceBadge source={ban.source} />
        </span>
        <span className="hidden min-w-0 @5xl:block">{status}</span>
        <span className="hidden min-w-0 @5xl:flex">{mode}</span>

        {/* 窄屏：来源、模式、时间一行 */}
        <span className="flex min-w-0 flex-wrap items-center gap-1.5 @5xl:hidden">
          <SourceBadge source={ban.source} />
          {mode}
          <span className="ml-auto">{created}</span>
        </span>

        <ReasonCell ban={ban} />
        <span className="hidden @5xl:block">{created}</span>
      </div>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="icon-xs" variant="ghost" aria-label="更多操作" className="-mr-1 shrink-0 text-muted-foreground">
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuItem onSelect={onInspect}>
            <Eye />
            查看
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={copy}>
            <Copy />
            复制 IP
          </DropdownMenuItem>
          {canBan && active ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil />
                修改
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onUnban}>
                <Unlock />
                解封
              </DropdownMenuItem>
            </>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function StatusCell({ ban, now }: { ban: IPBanEntry; now: number }) {
  const state = banTimeState(ban, now);
  const effective = isActiveBan(ban, now) ? "active" : ban.status === "revoked" ? "revoked" : "expired";
  const content = (
    <span className={cn("inline-flex max-w-full items-center gap-1.5 text-xs whitespace-nowrap tabular-nums", TIME_TONE[state.tone])}>
      <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", STATUS_META[effective]?.dot, state.tone === "warning" && "bg-amber-500")} />
      <span className="truncate">{state.text}</span>
    </span>
  );
  if (!state.title) return content;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex max-w-full cursor-default">{content}</span>
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-xs text-xs">
        {state.title}
      </TooltipContent>
    </Tooltip>
  );
}

function ModeCell({ ban, modes }: { ban: IPBanEntry; modes: BanModes }) {
  const label = modes.short(ban.mode);
  return (
    <span
      title={modes.label(ban.mode)}
      className={cn(
        "inline-flex h-5 max-w-full items-center truncate rounded-md border px-1.5 text-[11px] whitespace-nowrap",
        ban.mode ? "border-border text-foreground/85" : "border-dashed text-muted-foreground"
      )}
    >
      {label}
      {ban.mode === "tarpit" && ban.delayMs ? <span className="ml-1 text-muted-foreground tabular-nums">{(ban.delayMs / 1000).toFixed(ban.delayMs % 1000 ? 1 : 0)}s</span> : null}
    </span>
  );
}

function ReasonCell({ ban }: { ban: IPBanEntry }) {
  if (!ban.reason && !ban.triggerRule) return <span className="hidden text-xs text-muted-foreground @5xl:block">—</span>;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex min-w-0 cursor-default flex-col">
          <span className="truncate text-xs">{ban.reason || <span className="text-muted-foreground">无原因</span>}</span>
          {ban.triggerRule ? <span className="truncate font-mono text-[11px] text-muted-foreground">{ban.triggerRule}</span> : null}
        </span>
      </TooltipTrigger>
      <TooltipContent side="top" align="start" className="max-w-sm text-xs">
        <p className="break-words">{ban.reason || "无原因"}</p>
        {ban.triggerRule ? (
          <p className="mt-1 font-mono break-all opacity-80">
            {ban.triggerRule}
            {ban.triggerCount ? ` · 触发 ${ban.triggerCount} 次` : ""}
          </p>
        ) : null}
      </TooltipContent>
    </Tooltip>
  );
}

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
  options: Array<{ value: string; label: ReactNode }>;
}) {
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger size="sm" aria-label={label} className={cn("w-full min-w-0", value && "border-foreground/25 bg-muted/40")}>
        <span className="shrink-0 text-muted-foreground">{label}</span>
        <span className="min-w-0 flex-1 truncate text-left">
          <SelectValue />
        </span>
      </SelectTrigger>
      <SelectContent className="max-h-80">
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

/** 国家：取生效中封禁最多的那些国家；已选的不在其中时也列出来 */
function CountrySelect({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const stats = useBanStatsQuery();
  const codes = useMemo(() => {
    const list = (stats.data?.byCountry ?? []).map((item) => item.key).filter((code) => code && code !== "??");
    if (value && !list.includes(value)) list.unshift(value);
    return list;
  }, [stats.data?.byCountry, value]);
  return (
    <FilterSelect
      label="国家"
      value={value}
      onChange={onChange}
      options={codes.map((code) => ({
        value: code,
        label: (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <CountryFlag code={code} size={14} />
            {countryName(code)}
          </span>
        )
      }))}
    />
  );
}

function QuickChip({ icon: Icon, active, onClick, children }: { icon: typeof Clock; active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        active ? "border-primary/40 bg-primary/10 text-primary" : "text-muted-foreground hover:bg-accent hover:text-foreground"
      )}
    >
      <Icon className="size-3.5" />
      {children}
    </button>
  );
}

function ActiveFilters({
  filter,
  modes,
  update,
  reset
}: {
  filter: BanListFilter;
  modes: BanModes;
  update: (patch: Partial<BanListFilter>) => void;
  reset: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5 border-b bg-muted/20 px-3 py-2">
      <ListFilter className="size-3.5 text-muted-foreground" />
      {filter.source ? <FilterChip onRemove={() => update({ source: "" })}>来源：{SOURCE_META[filter.source]?.label ?? filter.source}</FilterChip> : null}
      {filter.mode ? <FilterChip onRemove={() => update({ mode: "" })}>模式：{modes.short(filter.mode)}</FilterChip> : null}
      {filter.country ? (
        <FilterChip onRemove={() => update({ country: "" })}>
          <CountryFlag code={filter.country} size={12} />
          {countryName(filter.country)}
        </FilterChip>
      ) : null}
      {filter.keyword.trim() ? (
        <FilterChip onRemove={() => update({ keyword: "" })}>
          关键字 <span className="max-w-40 truncate font-mono">{filter.keyword.trim()}</span>
        </FilterChip>
      ) : null}
      {filter.expiring ? <FilterChip onRemove={() => update({ expiring: false })}>24 小时内到期</FilterChip> : null}
      {filter.permanent ? <FilterChip onRemove={() => update({ permanent: "" })}>{filter.permanent === "true" ? "仅永久" : "仅限期"}</FilterChip> : null}
      <Button size="xs" variant="ghost" className="ml-auto text-muted-foreground" onClick={reset}>
        清空
      </Button>
    </div>
  );
}

function FilterChip({ children, onRemove }: { children: ReactNode; onRemove: () => void }) {
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

/* ── 分页 ── */

function pageWindow(current: number, total: number): Array<number | "…"> {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const pages = [...new Set([1, total, current - 1, current, current + 1])].filter((p) => p >= 1 && p <= total).sort((a, b) => a - b);
  const out: Array<number | "…"> = [];
  pages.forEach((p, i) => {
    if (i > 0 && p - (pages[i - 1] as number) > 1) out.push("…");
    out.push(p);
  });
  return out;
}

function Pagination({
  page,
  totalPages,
  total,
  pageSize,
  onPage,
  onPageSize
}: {
  page: number;
  totalPages: number;
  total: number;
  pageSize: number;
  onPage: (page: number) => void;
  onPageSize: (size: number) => void;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t bg-muted/20 px-4 py-2.5">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="tabular-nums">共 {total.toLocaleString("zh-CN")} 条</span>
        <Select value={String(pageSize)} onValueChange={(value) => onPageSize(Number(value))}>
          <SelectTrigger size="sm" className="h-7 w-[92px] text-xs" aria-label="每页条数">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((size) => (
              <SelectItem key={size} value={String(size)}>
                {size} 条/页
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex items-center gap-1">
        <Button size="icon" variant="ghost" className="size-8" aria-label="上一页" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          <ChevronLeft className="size-4" />
        </Button>
        <span className="px-1 text-xs text-muted-foreground tabular-nums sm:hidden">
          {page} / {totalPages}
        </span>
        <div className="hidden items-center gap-1 sm:flex">
          {pageWindow(page, totalPages).map((p, i) =>
            p === "…" ? (
              <span key={`gap-${i}`} className="px-1 text-xs text-muted-foreground">
                …
              </span>
            ) : (
              <Button key={p} size="sm" variant={p === page ? "secondary" : "ghost"} className="h-8 min-w-8 px-2 tabular-nums" onClick={() => onPage(p)}>
                {p}
              </Button>
            )
          )}
        </div>
        <Button size="icon" variant="ghost" className="size-8" aria-label="下一页" disabled={page >= totalPages} onClick={() => onPage(page + 1)}>
          <ChevronRight className="size-4" />
        </Button>
      </div>
    </div>
  );
}

/* ── 批量操作条 ── */

function BulkBar({
  count,
  filterMode,
  rows,
  query,
  total,
  now,
  modes,
  canBan,
  onClear,
  onUnban,
  onEdit
}: {
  count: number;
  filterMode: boolean;
  rows: IPBanEntry[];
  query: BanFilter;
  total: number;
  now: number;
  modes: BanModes;
  canBan: boolean;
  onClear: () => void;
  onUnban: () => void;
  onEdit: (kind: "mode" | "duration") => void;
}) {
  const token = useAdminToken();
  const [busy, setBusy] = useState(false);
  const activeCount = filterMode ? null : rows.filter((ban) => isActiveBan(ban, now)).length;
  const editBlocked = filterMode
    ? "按筛选全选时不能批量修改，请改为勾选具体记录"
    : activeCount === 0
      ? "选中的封禁都已失效"
      : (activeCount ?? 0) > MAX_BAN_BATCH
        ? `一次最多修改 ${MAX_BAN_BATCH} 条`
        : null;

  /** 按筛选全选时逐页拉齐（每页 500，最多 5 万条） */
  const collect = async (): Promise<IPBanEntry[] | null> => {
    if (!filterMode) return rows;
    if (!token) return null;
    const limit = Math.min(total, EXPORT_MAX);
    const toastId = toast.loading(`正在读取 0 / ${limit.toLocaleString("zh-CN")} 条…`);
    setBusy(true);
    try {
      const out: IPBanEntry[] = [];
      for (let page = 1; out.length < limit; page += 1) {
        const res = await firewallBansApi.list(token, { ...query, page, pageSize: EXPORT_PAGE });
        out.push(...res.items);
        toast.loading(`正在读取 ${Math.min(out.length, limit).toLocaleString("zh-CN")} / ${limit.toLocaleString("zh-CN")} 条…`, { id: toastId });
        if (res.items.length < EXPORT_PAGE || page >= res.totalPages) break;
      }
      toast.dismiss(toastId);
      if (total > EXPORT_MAX) toast.info(`超过 ${EXPORT_MAX.toLocaleString("zh-CN")} 条，只取了前 ${EXPORT_MAX.toLocaleString("zh-CN")} 条`);
      return out.slice(0, limit);
    } catch (error) {
      toast.error(banErrorMessage(error, "读取失败"), { id: toastId });
      return null;
    } finally {
      setBusy(false);
    }
  };

  const copyIPs = async () => {
    const list = await collect();
    if (!list) return;
    try {
      await navigator.clipboard.writeText(list.map((ban) => ban.ip).join("\n"));
      toast.success(`已复制 ${list.length.toLocaleString("zh-CN")} 个 IP`);
    } catch {
      toast.error("复制失败");
    }
  };

  const exportCSV = async () => {
    const list = await collect();
    if (!list) return;
    downloadBansCSV(list, modes);
    toast.success(`已导出 ${list.length.toLocaleString("zh-CN")} 条`);
  };

  const editButton = (kind: "mode" | "duration", icon: ReactNode, label: string) => {
    const button = (
      <Button size="sm" variant="outline" disabled={Boolean(editBlocked) || busy} onClick={() => onEdit(kind)}>
        {icon}
        {label}
      </Button>
    );
    if (!editBlocked) return button;
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={0} className="inline-flex rounded-md focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none">
            {button}
          </span>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-xs text-xs">
          {editBlocked}
        </TooltipContent>
      </Tooltip>
    );
  };

  return (
    <div
      role="toolbar"
      aria-label="批量操作"
      className="sticky bottom-4 z-20 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 rounded-2xl border bg-card/95 px-3 py-2.5 shadow-lg backdrop-blur supports-[backdrop-filter]:bg-card/85 sm:px-4"
    >
      <div className="flex min-w-0 items-center gap-1.5">
        <Button size="icon-xs" variant="ghost" aria-label="取消选择" onClick={onClear}>
          <X />
        </Button>
        <span className="text-sm font-medium tabular-nums">
          {filterMode ? "全部匹配 " : "已选 "}
          {count.toLocaleString("zh-CN")} 条
        </span>
        {activeCount !== null && activeCount < count ? <span className="text-xs text-muted-foreground tabular-nums">（生效中 {activeCount}）</span> : null}
      </div>
      <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-1.5">
        {canBan ? (
          <>
            <Button size="sm" disabled={busy || activeCount === 0} onClick={onUnban}>
              <Unlock />
              解封
            </Button>
            {editButton("mode", <SlidersHorizontal />, "修改响应模式")}
            {editButton("duration", <Timer />, "修改时长")}
          </>
        ) : null}
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => void copyIPs()}>
          <ClipboardCopy />
          复制 IP
        </Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => void exportCSV()}>
          <Download />
          导出 CSV
        </Button>
      </div>
    </div>
  );
}
