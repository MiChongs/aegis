"use client";

import { useMemo, useState, type ReactNode } from "react";
import { motion } from "motion/react";
import { Boxes, ChevronsUpDown, Crown, LayoutGrid, PanelLeftClose, PanelLeftOpen, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useTicketAppsQuery } from "@/lib/ticket-hooks";
import type { TicketAppSummary } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { formatRelativeTime } from "@/components/tickets/ticket-shared";
import { useDesk } from "./desk-context";

// 应用栏：服务台最左一列，按应用区分工单 / 反馈。
//
// 「我创建的应用」固定排在最上面（后端已排好：我创建的在前，其次未结多的、最近有动静的），
// 一条都还没有的自建应用也列出来，否则刚建好的应用在这里找不到。
// 每行只回答「这个应用现在有多少要处理、有没有超时」：数字是未结数，红点是有超时。

const SEARCH_THRESHOLD = 8;

/** 应用标记：首字 + 按应用 ID 取的色相，同一个应用在各处颜色一致 */
export function AppMark({ appid, name, className }: { appid: number; name: string; className?: string }) {
  const hue = appid === 0 ? 220 : (appid * 47) % 360;
  const letter = appid === 0 ? "台" : Array.from(name.trim())[0]?.toUpperCase() || "?";
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-md text-[11px] font-semibold leading-none",
        className
      )}
      style={{
        backgroundColor: `oklch(0.93 0.05 ${hue})`,
        color: `oklch(0.42 0.12 ${hue})`
      }}
    >
      {letter}
    </span>
  );
}

type Totals = { active: number; overdue: number; unaccepted: number };

function sum(items: TicketAppSummary[]): Totals {
  return items.reduce(
    (acc, item) => ({
      active: acc.active + item.active,
      overdue: acc.overdue + item.overdue,
      unaccepted: acc.unaccepted + item.unaccepted
    }),
    { active: 0, overdue: 0, unaccepted: 0 }
  );
}

export function useDeskApps() {
  const { kind } = useDesk();
  const query = useTicketAppsQuery(kind);
  const apps = useMemo(() => query.data ?? [], [query.data]);
  const owned = useMemo(() => apps.filter((app) => app.owned), [apps]);
  const others = useMemo(() => apps.filter((app) => !app.owned), [apps]);
  return { query, apps, owned, others, totals: sum(apps), ownedTotals: sum(owned) };
}

type RailProps = {
  value: number | null;
  own: boolean;
  onSelect: (appid: number | null, own?: boolean) => void;
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
};

/** 宽屏：常驻的应用栏，可收成只剩标记的窄条 */
export function DeskAppRail({ value, own, onSelect, collapsed, onCollapsedChange }: RailProps) {
  const { noun } = useDesk();
  if (collapsed) {
    return <CollapsedRail value={value} own={own} onSelect={onSelect} onExpand={() => onCollapsedChange(false)} />;
  }
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <Boxes className="size-4 text-muted-foreground" />
        <span className="text-sm font-medium">应用</span>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="icon-xs" variant="ghost" className="ml-auto" onClick={() => onCollapsedChange(true)} aria-label="收起应用栏">
              <PanelLeftClose />
            </Button>
          </TooltipTrigger>
          <TooltipContent>收起应用栏</TooltipContent>
        </Tooltip>
      </div>
      <AppList value={value} own={own} onSelect={onSelect} noun={noun} layoutId="desk-app-active" />
    </div>
  );
}

/** 窄屏：收件箱顶部的应用切换器，内容与应用栏同一份 */
export function DeskAppPicker({ value, own, onSelect }: Omit<RailProps, "collapsed" | "onCollapsedChange">) {
  const { noun } = useDesk();
  const { apps } = useDeskApps();
  const [open, setOpen] = useState(false);
  const current = value === null ? null : apps.find((app) => app.appid === value);
  const label = current ? current.appName : own ? "我创建的应用" : "全部应用";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex h-8 min-w-0 max-w-full items-center gap-2 rounded-lg px-2 text-sm font-medium outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/40"
        >
          {current ? <AppMark appid={current.appid} name={current.appName} className="size-5 text-[10px]" /> : <LayoutGrid className="size-4 text-muted-foreground" />}
          <span className="truncate">{label}</span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="flex h-[min(28rem,70vh)] w-72 flex-col p-0">
        <AppList
          value={value}
          own={own}
          noun={noun}
          layoutId="desk-app-picker-active"
          onSelect={(appid, nextOwn) => {
            onSelect(appid, nextOwn);
            setOpen(false);
          }}
        />
      </PopoverContent>
    </Popover>
  );
}

function AppList({
  value,
  own,
  onSelect,
  noun,
  layoutId
}: {
  value: number | null;
  own: boolean;
  onSelect: (appid: number | null, own?: boolean) => void;
  noun: string;
  layoutId: string;
}) {
  const { query, apps, owned, others, totals, ownedTotals } = useDeskApps();
  const [keyword, setKeyword] = useState("");
  const searchable = apps.length > SEARCH_THRESHOLD;
  const match = (app: TicketAppSummary) => {
    const k = keyword.trim().toLowerCase();
    return !k || app.appName.toLowerCase().includes(k) || (app.appKey ?? "").toLowerCase().includes(k) || String(app.appid) === k;
  };
  const shownOwned = owned.filter(match);
  const shownOthers = others.filter(match);

  return (
    <>
      {searchable ? (
        <div className="relative shrink-0 border-b px-2 py-2">
          <Search className="pointer-events-none absolute left-4 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="查找应用"
            aria-label="查找应用"
            className="h-7 w-full rounded-md bg-muted/60 pl-7 pr-7 text-xs outline-none placeholder:text-muted-foreground focus-visible:bg-background focus-visible:ring-2 focus-visible:ring-ring/40"
          />
          {keyword ? (
            <button
              type="button"
              onClick={() => setKeyword("")}
              aria-label="清空"
              className="absolute right-4 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          ) : null}
        </div>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        <RailRow
          active={value === null && !own}
          onClick={() => onSelect(null, false)}
          icon={<LayoutGrid className="size-4" />}
          label="全部应用"
          hint={`我创建的应用排在前面`}
          count={totals.active}
          overdue={totals.overdue}
          layoutId={layoutId}
        />
        {owned.length > 0 ? (
          <RailRow
            active={value === null && own}
            onClick={() => onSelect(null, true)}
            icon={<Crown className="size-4" />}
            label="我创建的应用"
            hint={`只看 ${owned.length} 个自建应用`}
            count={ownedTotals.active}
            overdue={ownedTotals.overdue}
            layoutId={layoutId}
          />
        ) : null}

        {query.isLoading ? (
          <div className="mt-3 space-y-1.5 px-1">
            {Array.from({ length: 5 }, (_, index) => (
              <div key={index} className="flex items-center gap-2 py-1.5">
                <Skeleton className="size-6 rounded-md" />
                <Skeleton className="h-3.5 flex-1" />
                <Skeleton className="h-4 w-6 rounded-full" />
              </div>
            ))}
          </div>
        ) : query.isError ? (
          <div className="mt-6 flex flex-col items-center gap-2 px-3 text-center">
            <p className="text-xs text-muted-foreground">应用列表加载失败</p>
            <Button size="xs" variant="outline" onClick={() => void query.refetch()}>
              重试
            </Button>
          </div>
        ) : (
          <>
            <RailSection title="我创建的" count={owned.length}>
              {owned.length === 0 ? (
                <p className="px-2 py-1.5 text-xs leading-relaxed text-muted-foreground">还没有自己创建的应用。</p>
              ) : shownOwned.length === 0 ? (
                <p className="px-2 py-1.5 text-xs text-muted-foreground">没有匹配的应用</p>
              ) : (
                shownOwned.map((app) => (
                  <AppRow key={app.appid} app={app} active={value === app.appid} onClick={() => onSelect(app.appid)} noun={noun} layoutId={layoutId} />
                ))
              )}
            </RailSection>
            {others.length > 0 ? (
              <RailSection title="其他应用" count={others.length}>
                {shownOthers.length === 0 ? (
                  <p className="px-2 py-1.5 text-xs text-muted-foreground">没有匹配的应用</p>
                ) : (
                  shownOthers.map((app) => (
                    <AppRow key={app.appid} app={app} active={value === app.appid} onClick={() => onSelect(app.appid)} noun={noun} layoutId={layoutId} />
                  ))
                )}
              </RailSection>
            ) : null}
          </>
        )}
      </div>
    </>
  );
}

function RailSection({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  return (
    <section className="mt-3">
      <h3 className="flex items-center gap-1.5 px-2 pb-1 text-[11px] font-medium text-muted-foreground">
        {title}
        <span className="tabular-nums opacity-70">{count}</span>
      </h3>
      <div className="space-y-px">{children}</div>
    </section>
  );
}

function RailRow({
  active,
  onClick,
  icon,
  label,
  hint,
  count,
  overdue,
  layoutId
}: {
  active: boolean;
  onClick: () => void;
  icon: ReactNode;
  label: string;
  hint: string;
  count: number;
  overdue: number;
  layoutId: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={hint}
      aria-current={active ? "true" : undefined}
      className={cn(
        "relative flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/40",
        active ? "font-medium text-foreground" : "text-foreground/80 hover:bg-muted/60"
      )}
    >
      {active ? (
        <motion.span layoutId={layoutId} className="absolute inset-0 rounded-lg bg-accent" transition={{ type: "spring", stiffness: 520, damping: 42 }} />
      ) : null}
      <span className="relative flex size-6 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">{icon}</span>
      <span className="relative min-w-0 flex-1 truncate">{label}</span>
      <Count value={count} overdue={overdue} />
    </button>
  );
}

function AppRow({
  app,
  active,
  onClick,
  noun,
  layoutId
}: {
  app: TicketAppSummary;
  active: boolean;
  onClick: () => void;
  noun: string;
  layoutId: string;
}) {
  const detail = [
    `未结 ${app.active}`,
    app.unaccepted ? `待受理 ${app.unaccepted}` : null,
    app.unassigned ? `未指派 ${app.unassigned}` : null,
    app.overdue ? `超时 ${app.overdue}` : null,
    app.mine ? `我受理 ${app.mine}` : null,
    `今日新增 ${app.createdToday}`,
    `累计 ${app.total} 条${noun}`,
    app.lastAt ? `最近 ${formatRelativeTime(app.lastAt)}` : `还没有${noun}`
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <Tooltip delayDuration={500}>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          aria-current={active ? "true" : undefined}
          className={cn(
            "relative flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/40",
            active ? "font-medium text-foreground" : "text-foreground/80 hover:bg-muted/60",
            app.total === 0 && !active && "text-muted-foreground"
          )}
        >
          {active ? (
            <motion.span layoutId={layoutId} className="absolute inset-0 rounded-lg bg-accent" transition={{ type: "spring", stiffness: 520, damping: 42 }} />
          ) : null}
          <AppMark appid={app.appid} name={app.appName} className="relative" />
          <span className="relative min-w-0 flex-1 truncate">{app.appName || `应用 #${app.appid}`}</span>
          <Count value={app.active} overdue={app.overdue} strong={app.unaccepted > 0} />
        </button>
      </TooltipTrigger>
      <TooltipContent side="right" className="max-w-64">
        {detail}
      </TooltipContent>
    </Tooltip>
  );
}

function Count({ value, overdue, strong }: { value: number; overdue: number; strong?: boolean }) {
  if (!value && !overdue) return null;
  return (
    <span className="relative flex shrink-0 items-center gap-1">
      {overdue > 0 ? <span className="size-1.5 rounded-full bg-destructive" aria-label={`${overdue} 条超时`} /> : null}
      {value > 0 ? (
        <span
          className={cn(
            "min-w-5 rounded-full px-1.5 text-center text-[11px] font-medium leading-5 tabular-nums",
            strong ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
          )}
        >
          {value > 999 ? "999+" : value}
        </span>
      ) : null}
    </span>
  );
}

function CollapsedRail({
  value,
  own,
  onSelect,
  onExpand
}: {
  value: number | null;
  own: boolean;
  onSelect: (appid: number | null, own?: boolean) => void;
  onExpand: () => void;
}) {
  const { owned, others, totals } = useDeskApps();
  const item = (key: string, active: boolean, label: string, node: ReactNode, onClick: () => void, dot?: boolean) => (
    <Tooltip key={key}>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          aria-label={label}
          aria-current={active ? "true" : undefined}
          className={cn(
            "relative flex size-9 items-center justify-center rounded-lg outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring/40",
            active ? "bg-accent ring-1 ring-border" : "hover:bg-muted/60"
          )}
        >
          {node}
          {dot ? <span className="absolute right-1 top-1 size-1.5 rounded-full bg-destructive ring-2 ring-card" /> : null}
        </button>
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
  return (
    <div className="flex h-full min-h-0 flex-col items-center gap-1 py-2">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button size="icon-sm" variant="ghost" onClick={onExpand} aria-label="展开应用栏">
            <PanelLeftOpen />
          </Button>
        </TooltipTrigger>
        <TooltipContent side="right">展开应用栏</TooltipContent>
      </Tooltip>
      <div className="my-1 h-px w-6 bg-border" />
      <div className="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-y-auto [scrollbar-width:none]">
        {item("all", value === null && !own, `全部应用 · 未结 ${totals.active}`, <LayoutGrid className="size-4 text-muted-foreground" />, () => onSelect(null, false), totals.overdue > 0)}
        {owned.length > 0
          ? item("own", value === null && own, "我创建的应用", <Crown className="size-4 text-muted-foreground" />, () => onSelect(null, true))
          : null}
        {[...owned, ...others].map((app) =>
          item(
            String(app.appid),
            value === app.appid,
            `${app.appName}${app.owned ? "（我创建的）" : ""} · 未结 ${app.active}`,
            <AppMark appid={app.appid} name={app.appName} />,
            () => onSelect(app.appid),
            app.overdue > 0
          )
        )}
      </div>
    </div>
  );
}
