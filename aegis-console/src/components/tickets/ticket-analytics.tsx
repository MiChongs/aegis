"use client";

import { useCallback, useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  AlarmClock,
  AppWindow,
  BarChart3,
  CheckCircle2,
  Clock,
  Crown,
  Gauge,
  Inbox,
  Layers,
  ListChecks,
  Plus,
  RefreshCw,
  Star,
  Tags,
  Timer,
  TriangleAlert,
  UserX,
  Users,
  type LucideIcon
} from "lucide-react";
import { TonalIcon, Meter, type KitTone } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty, OverviewCard, formatCount, longDay, shortDay } from "@/components/overview/overview-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  useTicketAgentStatsQuery,
  useTicketAppsQuery,
  useTicketStatsQuery,
  useTicketTrendQuery
} from "@/lib/ticket-hooks";
import type { TicketAppSummary, TicketKind, TicketStats } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { PersonAvatar, formatDuration } from "./ticket-shared";

// 工单 / 反馈的统计分析。
//
// 应用范围放在地址栏 `?app=<appid>`（其余参数原样保留，replace 不占历史），
// 选择器里「我创建的应用」一组在前。范围为「全部应用」时多一张按应用排行，点一行即切到该应用。

const KIND_NOUN: Record<TicketKind, string> = { ticket: "工单", feedback: "反馈" };

const TREND_CONFIG = {
  created: { label: "新建", theme: { light: "#4f46e5", dark: "#818cf8" } },
  resolved: { label: "解决", theme: { light: "#059669", dark: "#34d399" } },
  closed: { label: "关闭", theme: { light: "#a1a1aa", dark: "#71717a" } }
} satisfies ChartConfig;

const CATEGORY_CONFIG = {
  count: { label: "数量", theme: { light: "#0284c7", dark: "#38bdf8" } }
} satisfies ChartConfig;

const RANGES = [7, 30, 90] as const;

const STATUS_ROWS: Array<{ key: keyof TicketStats; label: string; bar: string }> = [
  { key: "open", label: "待受理", bar: "bg-sky-500" },
  { key: "processing", label: "处理中", bar: "bg-indigo-500" },
  { key: "pendingUser", label: "等待补充", bar: "bg-amber-500" },
  { key: "resolved", label: "已解决", bar: "bg-emerald-500" },
  { key: "closed", label: "已关闭", bar: "bg-zinc-400" }
];

const PRIORITY_ROWS = [
  { key: "urgent", label: "紧急", bar: "bg-red-500" },
  { key: "high", label: "高", bar: "bg-amber-500" },
  { key: "normal", label: "中", bar: "bg-sky-500" },
  { key: "low", label: "低", bar: "bg-slate-400" }
];

function percent(value: number, total: number) {
  if (total <= 0) return "0%";
  const pct = (value / total) * 100;
  return `${pct >= 10 || pct === 0 ? Math.round(pct) : pct.toFixed(1)}%`;
}

// ─────────────── 范围（地址栏） ───────────────

function useAppScope() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const raw = searchParams.get("app");
  const parsed = raw === null || raw === "" ? NaN : Number(raw);
  const appId = Number.isInteger(parsed) && parsed >= 0 ? parsed : undefined;

  const setAppId = useCallback(
    (next: number | undefined) => {
      const params = new URLSearchParams(searchParams.toString());
      if (next === undefined) params.delete("app");
      else params.set("app", String(next));
      const query = params.toString();
      router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
    },
    [pathname, router, searchParams]
  );

  return [appId, setAppId] as const;
}

// ─────────────── 入口 ───────────────

export function TicketAnalytics({ kind }: { kind: TicketKind }) {
  const [appId, setAppId] = useAppScope();
  const [days, setDays] = useState<(typeof RANGES)[number]>(30);

  const statsQuery = useTicketStatsQuery(appId, kind);
  const trendQuery = useTicketTrendQuery(days, appId, kind);
  const agentsQuery = useTicketAgentStatsQuery(20, appId, kind);
  const appsQuery = useTicketAppsQuery(kind);

  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const current = appId === undefined ? undefined : apps.find((app) => app.appid === appId);
  const noun = KIND_NOUN[kind];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <AppScopePicker apps={apps} loading={appsQuery.isLoading} value={appId} onChange={setAppId} />
        <p className="min-w-0 text-xs text-muted-foreground">
          {appId === undefined
            ? `统计你可见范围内全部应用的${noun}`
            : `只统计「${current?.appName ?? (appId === 0 ? "平台内部" : `应用 #${appId}`)}」的${noun}`}
        </p>
      </div>

      <KpiRow stats={statsQuery.data} loading={statsQuery.isLoading} error={statsQuery.isError} onRetry={() => void statsQuery.refetch()} />

      <TrendCard
        days={days}
        onDaysChange={setDays}
        data={trendQuery.data}
        loading={trendQuery.isLoading}
        error={trendQuery.isError}
        onRetry={() => void trendQuery.refetch()}
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <StatusCard stats={statsQuery.data} loading={statsQuery.isLoading} noun={noun} />
        <PriorityCard stats={statsQuery.data} loading={statsQuery.isLoading} />
        <SlaCard stats={statsQuery.data} loading={statsQuery.isLoading} noun={noun} />
      </div>

      <div className={cn("grid gap-4", appId === undefined && "xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]")}>
        {appId === undefined ? (
          <AppRankCard
            apps={apps}
            loading={appsQuery.isLoading}
            error={appsQuery.isError}
            onRetry={() => void appsQuery.refetch()}
            onPick={setAppId}
            noun={noun}
          />
        ) : null}
        <CategoryCard stats={statsQuery.data} loading={statsQuery.isLoading} />
      </div>

      <AgentCard
        agents={agentsQuery.data}
        loading={agentsQuery.isLoading}
        error={agentsQuery.isError}
        onRetry={() => void agentsQuery.refetch()}
      />
    </div>
  );
}

// ─────────────── 应用范围选择 ───────────────

function AppScopePicker({
  apps,
  loading,
  value,
  onChange
}: {
  apps: TicketAppSummary[];
  loading: boolean;
  value: number | undefined;
  onChange: (next: number | undefined) => void;
}) {
  const owned = apps.filter((app) => app.owned);
  const others = apps.filter((app) => !app.owned);
  const known = value === undefined || apps.some((app) => app.appid === value);

  const renderItem = (app: TicketAppSummary) => (
    <SelectItem key={app.appid} value={String(app.appid)}>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate">{app.appName || `应用 #${app.appid}`}</span>
        {app.active > 0 ? (
          <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">{app.active} 未结</span>
        ) : null}
      </span>
    </SelectItem>
  );

  return (
    <Select value={value === undefined ? "all" : String(value)} onValueChange={(next) => onChange(next === "all" ? undefined : Number(next))}>
      <SelectTrigger className="h-9 w-64" aria-label="统计范围">
        <AppWindow className="size-4 text-muted-foreground" />
        <SelectValue placeholder={loading ? "加载应用…" : "全部应用"} />
      </SelectTrigger>
      <SelectContent className="max-h-80">
        <SelectItem value="all">全部应用</SelectItem>
        {!known && value !== undefined ? <SelectItem value={String(value)}>{value === 0 ? "平台内部" : `应用 #${value}`}</SelectItem> : null}
        {owned.length > 0 ? (
          <>
            <SelectSeparator />
            <SelectGroup>
              <SelectLabel>我创建的应用</SelectLabel>
              {owned.map(renderItem)}
            </SelectGroup>
          </>
        ) : null}
        {others.length > 0 ? (
          <>
            <SelectSeparator />
            <SelectGroup>
              <SelectLabel>{owned.length > 0 ? "其他应用" : "应用"}</SelectLabel>
              {others.map(renderItem)}
            </SelectGroup>
          </>
        ) : null}
      </SelectContent>
    </Select>
  );
}

// ─────────────── 通用小件 ───────────────

function ErrorBlock({ onRetry, className }: { onRetry: () => void; className?: string }) {
  return (
    <div className={cn("flex flex-col items-center justify-center gap-3 py-8 text-center", className)}>
      <TonalIcon icon={TriangleAlert} tone="rose" />
      <p className="text-sm text-muted-foreground">数据加载失败</p>
      <Button size="sm" variant="outline" onClick={onRetry}>
        <RefreshCw />
        重试
      </Button>
    </div>
  );
}

function DistributionList({
  rows,
  total
}: {
  rows: Array<{ key: string; label: string; bar: string; value: number }>;
  total: number;
}) {
  return (
    <div className="space-y-3">
      <Meter segments={rows.map((row) => ({ value: row.value, className: row.bar, label: row.label }))} className="h-2" />
      <ul className="space-y-2">
        {rows.map((row) => (
          <li key={row.key} className="flex items-center gap-2 text-xs">
            <span className={cn("size-2 shrink-0 rounded-full", row.bar)} />
            <span className="text-muted-foreground">{row.label}</span>
            <span className="ml-auto font-medium tabular-nums text-foreground">{formatCount(row.value)}</span>
            <span className="w-11 text-right text-muted-foreground tabular-nums">{percent(row.value, total)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ListSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-3">
      <Skeleton className="h-2 w-full rounded-full" />
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-4 w-full" />
      ))}
    </div>
  );
}

// ─────────────── 指标行 ───────────────

function KpiRow({
  stats,
  loading,
  error,
  onRetry
}: {
  stats?: TicketStats;
  loading: boolean;
  error: boolean;
  onRetry: () => void;
}) {
  if (error && !stats) {
    return (
      <div className="rounded-xl border bg-card">
        <ErrorBlock onRetry={onRetry} />
      </div>
    );
  }

  const unresolved = stats ? stats.open + stats.processing + stats.pendingUser : 0;
  const items: Array<{ key: string; label: string; icon: LucideIcon; tone: KitTone; value: string; hint?: string }> = [
    {
      key: "open",
      label: "未结",
      icon: Inbox,
      tone: "primary",
      value: formatCount(unresolved),
      hint: stats ? `待受理 ${stats.open} · 未指派 ${stats.unassigned}` : undefined
    },
    { key: "today", label: "今日新增", icon: Plus, tone: "sky", value: formatCount(stats?.createdToday ?? 0) },
    { key: "solved", label: "今日解决", icon: CheckCircle2, tone: "emerald", value: formatCount(stats?.resolvedToday ?? 0) },
    { key: "first", label: "平均首响", icon: Timer, tone: "violet", value: formatDuration(stats?.avgFirstResponseMs ?? 0) },
    { key: "resolve", label: "平均解决", icon: Clock, tone: "amber", value: formatDuration(stats?.avgResolveMs ?? 0) },
    {
      key: "rating",
      label: "满意度",
      icon: Star,
      tone: "amber",
      value: stats && stats.ratingCount > 0 ? `${stats.avgRating.toFixed(1)} / 5` : "暂无评价",
      hint: stats && stats.ratingCount > 0 ? `${stats.ratingCount} 条评价` : undefined
    }
  ];

  return (
    <div className="grid grid-cols-2 gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-3 xl:grid-cols-6">
      {items.map((item) => (
        <div key={item.key} className="flex min-w-0 items-center gap-3 bg-card px-4 py-3.5">
          <TonalIcon icon={item.icon} tone={item.tone} />
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">{item.label}</p>
            {loading || !stats ? (
              <Skeleton className="mt-1 h-5 w-14" />
            ) : (
              <>
                <p className="truncate text-base font-semibold tabular-nums text-foreground">{item.value}</p>
                {item.hint ? <p className="truncate text-[11px] text-muted-foreground">{item.hint}</p> : null}
              </>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

// ─────────────── 趋势 ───────────────

function TrendCard({
  days,
  onDaysChange,
  data,
  loading,
  error,
  onRetry
}: {
  days: (typeof RANGES)[number];
  onDaysChange: (days: (typeof RANGES)[number]) => void;
  data?: Array<{ date: string; created: number; resolved: number; closed: number }>;
  loading: boolean;
  error: boolean;
  onRetry: () => void;
}) {
  const rows = useMemo(() => (data ?? []).map((point) => ({ ...point, label: shortDay(point.date) })), [data]);
  const totals = useMemo(
    () =>
      rows.reduce(
        (sum, point) => ({ created: sum.created + point.created, resolved: sum.resolved + point.resolved, closed: sum.closed + point.closed }),
        { created: 0, resolved: 0, closed: 0 }
      ),
    [rows]
  );
  const empty = rows.every((point) => !point.created && !point.resolved && !point.closed);

  return (
    <OverviewCard
      icon={BarChart3}
      tone="primary"
      title="新建与解决趋势"
      description={`近 ${days} 天`}
      action={
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={String(days)}
          onValueChange={(value) => value && onDaysChange(Number(value) as (typeof RANGES)[number])}
          aria-label="时间范围"
        >
          {RANGES.map((value) => (
            <ToggleGroupItem key={value} value={String(value)} className="px-2.5 text-xs">
              {value} 天
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      }
    >
      <div className="mb-3 flex flex-wrap gap-x-6 gap-y-1">
        {(Object.keys(TREND_CONFIG) as Array<keyof typeof TREND_CONFIG>).map((key) => (
          <div key={key} className="flex items-baseline gap-1.5">
            <span className="text-xs text-muted-foreground">{TREND_CONFIG[key].label}</span>
            {loading ? (
              <Skeleton className="h-5 w-10" />
            ) : (
              <span className="text-lg font-semibold tabular-nums text-foreground">{formatCount(totals[key])}</span>
            )}
          </div>
        ))}
      </div>
      {error && !data ? (
        <ErrorBlock onRetry={onRetry} className="h-[240px]" />
      ) : loading ? (
        <Skeleton className="h-[240px] w-full rounded-lg" />
      ) : empty ? (
        <div className="grid h-[240px] place-items-center">
          <CardEmpty icon={BarChart3} title="这段时间没有新的记录" />
        </div>
      ) : (
        <ChartContainer config={TREND_CONFIG} className="h-[240px] w-full">
          <AreaChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} minTickGap={18} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(_, payload) => {
                    const date = (payload?.[0]?.payload as { date?: string } | undefined)?.date;
                    return date ? longDay(date) : "";
                  }}
                />
              }
            />
            <Area dataKey="created" type="monotone" stroke="var(--color-created)" fill="var(--color-created)" fillOpacity={0.12} strokeWidth={2} dot={false} />
            <Area dataKey="resolved" type="monotone" stroke="var(--color-resolved)" fill="var(--color-resolved)" fillOpacity={0.12} strokeWidth={2} dot={false} />
            <Area dataKey="closed" type="monotone" stroke="var(--color-closed)" fill="transparent" strokeWidth={1.5} strokeDasharray="4 3" dot={false} />
            <ChartLegend content={<ChartLegendContent />} />
          </AreaChart>
        </ChartContainer>
      )}
    </OverviewCard>
  );
}

// ─────────────── 分布 ───────────────

function StatusCard({ stats, loading, noun }: { stats?: TicketStats; loading: boolean; noun: string }) {
  const rows = STATUS_ROWS.map((row) => ({ ...row, key: String(row.key), value: Number(stats?.[row.key] ?? 0) }));
  const total = rows.reduce((sum, row) => sum + row.value, 0);
  return (
    <OverviewCard icon={ListChecks} tone="sky" title="状态分布" description={stats ? `共 ${formatCount(stats.total)} 条${noun}` : undefined}>
      {loading || !stats ? <ListSkeleton rows={5} /> : total === 0 ? <CardEmpty icon={ListChecks} title={`还没有${noun}`} /> : <DistributionList rows={rows} total={total} />}
    </OverviewCard>
  );
}

function PriorityCard({ stats, loading }: { stats?: TicketStats; loading: boolean }) {
  const rows = PRIORITY_ROWS.map((row) => ({ ...row, value: stats?.byPriority?.[row.key] ?? 0 }));
  const total = rows.reduce((sum, row) => sum + row.value, 0);
  return (
    <OverviewCard icon={Layers} tone="violet" title="优先级构成">
      {loading || !stats ? <ListSkeleton rows={4} /> : total === 0 ? <CardEmpty icon={Layers} title="暂无数据" /> : <DistributionList rows={rows} total={total} />}
    </OverviewCard>
  );
}

function SlaCard({ stats, loading, noun }: { stats?: TicketStats; loading: boolean; noun: string }) {
  const items: Array<{ key: string; label: string; hint: string; value: number; icon: LucideIcon }> = stats
    ? [
        { key: "first", label: "首响超时", hint: "超过首次响应时限仍未回复", value: stats.overdueFirstResponse, icon: AlarmClock },
        { key: "resolve", label: "解决超时", hint: "超过解决时限仍未解决", value: stats.overdueResolve, icon: Gauge },
        { key: "unassigned", label: "未指派", hint: `尚无受理人的未结${noun}`, value: stats.unassigned, icon: UserX }
      ]
    : [];
  const overdue = stats ? stats.overdueFirstResponse + stats.overdueResolve : 0;

  return (
    <OverviewCard
      icon={AlarmClock}
      tone={overdue > 0 ? "rose" : "emerald"}
      title="时效"
      description={stats ? (overdue > 0 ? `${overdue} 条已超时` : "没有超时") : undefined}
    >
      {loading || !stats ? (
        <div className="space-y-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : (
        <ul className="space-y-2">
          {items.map((item) => {
            const alert = item.value > 0 && item.key !== "unassigned";
            return (
              <li
                key={item.key}
                className={cn("flex items-center gap-3 rounded-lg border px-3 py-2", alert && "border-destructive/30 bg-destructive/5")}
              >
                <item.icon className={cn("size-4 shrink-0", alert ? "text-destructive" : "text-muted-foreground")} />
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-foreground">{item.label}</p>
                  <p className="truncate text-[11px] text-muted-foreground">{item.hint}</p>
                </div>
                <span className={cn("text-lg font-semibold tabular-nums", alert ? "text-destructive" : "text-foreground")}>{item.value}</span>
              </li>
            );
          })}
        </ul>
      )}
    </OverviewCard>
  );
}

// ─────────────── 按应用 ───────────────

function AppRankCard({
  apps,
  loading,
  error,
  onRetry,
  onPick,
  noun
}: {
  apps: TicketAppSummary[];
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  onPick: (appId: number) => void;
  noun: string;
}) {
  const rows = apps.filter((app) => app.total > 0 || app.owned);
  return (
    <OverviewCard icon={AppWindow} tone="emerald" title="按应用" description="我创建的应用排在前面，点击一行查看该应用" contentClassName="px-0 sm:px-0">
      {error && apps.length === 0 ? (
        <ErrorBlock onRetry={onRetry} />
      ) : loading ? (
        <div className="space-y-2 px-4 sm:px-5">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-9 w-full" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <CardEmpty icon={AppWindow} title={`还没有任何应用收到${noun}`} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4 sm:pl-5">应用</TableHead>
              <TableHead className="text-right">未结</TableHead>
              <TableHead className="text-right">待受理</TableHead>
              <TableHead className="text-right">未指派</TableHead>
              <TableHead className="text-right">超时</TableHead>
              <TableHead className="text-right">今日</TableHead>
              <TableHead className="pr-4 text-right sm:pr-5">累计</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((app) => (
              <TableRow
                key={app.appid}
                tabIndex={0}
                onClick={() => onPick(app.appid)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onPick(app.appid);
                  }
                }}
                className="cursor-pointer focus-visible:bg-muted/50 focus-visible:outline-none"
              >
                <TableCell className="max-w-56 pl-4 sm:pl-5">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate font-medium">{app.appName || `应用 #${app.appid}`}</span>
                    {app.owned ? (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="inline-flex shrink-0">
                            <Badge variant="info" size="sm">
                              <Crown className="size-3" />
                              我创建的
                            </Badge>
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>这个应用由你创建</TooltipContent>
                      </Tooltip>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="text-right font-medium tabular-nums">{app.active}</TableCell>
                <TableCell className="text-right tabular-nums">{app.unaccepted}</TableCell>
                <TableCell className="text-right tabular-nums">{app.unassigned}</TableCell>
                <TableCell className="text-right">
                  {app.overdue > 0 ? (
                    <Badge variant="danger" size="sm">
                      {app.overdue}
                    </Badge>
                  ) : (
                    <span className="text-muted-foreground tabular-nums">0</span>
                  )}
                </TableCell>
                <TableCell className="text-right tabular-nums">{app.createdToday}</TableCell>
                <TableCell className="pr-4 text-right text-muted-foreground tabular-nums sm:pr-5">{formatCount(app.total)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </OverviewCard>
  );
}

// ─────────────── 分类 ───────────────

function CategoryCard({ stats, loading }: { stats?: TicketStats; loading: boolean }) {
  const categories = useMemo(
    () =>
      [...(stats?.byCategory ?? [])]
        .sort((a, b) => b.count - a.count)
        .slice(0, 8)
        .map((item) => ({ ...item, categoryName: item.categoryName || "未分类" })),
    [stats?.byCategory]
  );
  const height = Math.max(180, categories.length * 34);

  return (
    <OverviewCard icon={Tags} tone="amber" title="分类 Top 8">
      {loading || !stats ? (
        <Skeleton className="h-[180px] w-full rounded-lg" />
      ) : categories.length === 0 ? (
        <CardEmpty icon={Tags} title="暂无分类数据" />
      ) : (
        <ChartContainer config={CATEGORY_CONFIG} className="w-full" style={{ height }}>
          <BarChart data={categories} layout="vertical" margin={{ left: 0, right: 16, top: 4 }}>
            <CartesianGrid horizontal={false} strokeDasharray="3 3" />
            <XAxis type="number" allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} />
            <YAxis type="category" dataKey="categoryName" tickLine={false} axisLine={false} fontSize={11} width={88} />
            <ChartTooltip cursor={{ fill: "var(--muted)", opacity: 0.4 }} content={<ChartTooltipContent hideLabel />} />
            <Bar dataKey="count" fill="var(--color-count)" radius={[0, 4, 4, 0]} maxBarSize={22} />
          </BarChart>
        </ChartContainer>
      )}
    </OverviewCard>
  );
}

// ─────────────── 处理人 ───────────────

type AgentRow = NonNullable<ReturnType<typeof useTicketAgentStatsQuery>["data"]>[number];

function AgentCard({
  agents,
  loading,
  error,
  onRetry
}: {
  agents?: AgentRow[];
  loading: boolean;
  error: boolean;
  onRetry: () => void;
}) {
  const rows = agents ?? [];
  const maxResolved = Math.max(1, ...rows.map((agent) => agent.resolved));

  return (
    <OverviewCard icon={Users} tone="primary" title="处理人绩效" description="按受理的记录统计" contentClassName="px-0 sm:px-0">
      {error && !agents ? (
        <ErrorBlock onRetry={onRetry} />
      ) : loading ? (
        <div className="space-y-2 px-4 sm:px-5">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <CardEmpty icon={Users} title="还没有处理记录" />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4 sm:pl-5">处理人</TableHead>
              <TableHead className="text-right">已分配</TableHead>
              <TableHead className="w-40">已解决</TableHead>
              <TableHead className="text-right">未结</TableHead>
              <TableHead className="text-right">平均首响</TableHead>
              <TableHead className="text-right">平均解决</TableHead>
              <TableHead className="text-right">超时</TableHead>
              <TableHead className="pr-4 text-right sm:pr-5">评分</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((agent) => {
              const name = agent.displayName || agent.account;
              return (
                <TableRow key={agent.adminId}>
                  <TableCell className="pl-4 sm:pl-5">
                    <div className="flex min-w-0 items-center gap-2">
                      <PersonAvatar name={name} size="sm" />
                      <div className="min-w-0">
                        <p className="truncate font-medium">{name}</p>
                        {agent.displayName && agent.account ? <p className="truncate text-[11px] text-muted-foreground">{agent.account}</p> : null}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{agent.assigned}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-emerald-500" style={{ width: `${(agent.resolved / maxResolved) * 100}%` }} />
                      </div>
                      <span className="w-8 text-right tabular-nums">{agent.resolved}</span>
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{agent.open}</TableCell>
                  <TableCell className="text-right whitespace-nowrap">{formatDuration(agent.avgFirstResponseMs)}</TableCell>
                  <TableCell className="text-right whitespace-nowrap">{formatDuration(agent.avgResolveMs)}</TableCell>
                  <TableCell className="text-right">
                    {agent.breached > 0 ? (
                      <Badge variant="danger" size="sm">
                        {agent.breached}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground tabular-nums">0</span>
                    )}
                  </TableCell>
                  <TableCell className="pr-4 text-right sm:pr-5">
                    {agent.avgRating > 0 ? (
                      <span className="inline-flex items-center gap-1 tabular-nums">
                        <Star className="size-3 fill-amber-400 text-amber-400" />
                        {agent.avgRating.toFixed(1)}
                      </span>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </OverviewCard>
  );
}
