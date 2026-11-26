"use client";

import { useMemo } from "react";
import NumberFlow from "@number-flow/react";
import { motion } from "motion/react";
import { Bar, BarChart, XAxis, YAxis } from "recharts";
import { AlarmClock, ArrowRight, Crown, LayoutGrid, UserCheck, UserX } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { buildChartConfig } from "@/components/commerce/commerce-charts";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import type { TicketAppSummary, TicketPriority, TicketStats } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { Kbd, PRIORITY_LABEL, formatDuration, formatRelativeTime } from "@/components/tickets/ticket-shared";
import { AppMark, useDeskApps } from "./desk-app-rail";
import { useDesk } from "./desk-context";

// 没选中任何一条时的详情区：这一屏的「现在怎么样」。
//
// 看全部应用时，最显眼的是「按应用」一组卡片：我创建的应用排在前面，
// 每张卡回答「这个应用有多少没处理、有没有超时、多久没人管」，点一下就切过去。
// 选了某个应用时换成这个应用自己的数字、分类分布与优先级构成。

const CATEGORY_CONFIG = buildChartConfig([{ key: "count", label: "数量", color: { light: "#0ea5e9", dark: "#38bdf8" } }]);

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["J"], label: "下一条" },
  { keys: ["K"], label: "上一条" },
  { keys: ["/"], label: "搜索" },
  { keys: ["R"], label: "回复" },
  { keys: ["N"], label: "写备注" },
  { keys: ["E"], label: "解决" },
  { keys: ["A"], label: "指派给我" },
  { keys: ["X"], label: "勾选当前" },
  { keys: ["I"], label: "资料面板" },
  { keys: ["."], label: "操作菜单" },
  { keys: ["Ctrl", "Enter"], label: "发送" },
  { keys: ["Shift", "点击"], label: "多选" }
];

export function ShortcutList({ className }: { className?: string }) {
  return (
    <ul className={cn("grid grid-cols-2 gap-x-6 gap-y-2", className)}>
      {SHORTCUTS.map((item) => (
        <li key={item.label} className="flex items-center justify-between gap-3 text-xs">
          <span className="text-muted-foreground">{item.label}</span>
          <span className="flex gap-1">
            {item.keys.map((key) => (
              <Kbd key={key}>{key}</Kbd>
            ))}
          </span>
        </li>
      ))}
    </ul>
  );
}

const PRIORITY_ORDER: TicketPriority[] = ["urgent", "high", "normal", "low"];
const PRIORITY_BAR: Record<TicketPriority, string> = {
  urgent: "bg-red-500",
  high: "bg-amber-500",
  normal: "bg-sky-500",
  low: "bg-muted-foreground/40"
};

export function DeskOverview({
  stats,
  appid,
  own,
  onSelectApp
}: {
  stats?: TicketStats;
  appid: number | null;
  own: boolean;
  onSelectApp: (appid: number | null, own?: boolean) => void;
}) {
  const { noun } = useDesk();
  const { apps, owned, query: appsQuery } = useDeskApps();
  const current = appid === null ? null : apps.find((app) => app.appid === appid);
  const categories = useMemo(
    () => [...(stats?.byCategory ?? [])].filter((item) => item.count > 0).sort((a, b) => b.count - a.count).slice(0, 6),
    [stats?.byCategory]
  );
  // 卡片：我创建的全部列出，其余按未结多少取前几个
  const cards = useMemo(() => {
    const pool = own ? owned : apps;
    const mine = pool.filter((app) => app.owned);
    const rest = pool.filter((app) => !app.owned).slice(0, Math.max(0, 9 - mine.length));
    return [...mine, ...rest];
  }, [apps, owned, own]);

  const title = current ? current.appName : own ? "我创建的应用" : "全部应用";

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto flex max-w-3xl flex-col gap-7 p-6 lg:p-8">
        {/* 标题 */}
        <div className="flex items-center gap-3">
          {current ? (
            <AppMark appid={current.appid} name={current.appName} className="size-10 rounded-xl text-base" />
          ) : (
            <span className="flex size-10 items-center justify-center rounded-xl bg-muted text-muted-foreground">
              {own ? <Crown className="size-5" /> : <LayoutGrid className="size-5" />}
            </span>
          )}
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="truncate text-lg font-semibold tracking-tight">{title}</h2>
              {current?.owned ? (
                <Badge variant="warning" size="sm">
                  <Crown />
                  我创建的
                </Badge>
              ) : null}
            </div>
            <p className="text-xs text-muted-foreground">
              {current
                ? current.lastAt
                  ? `最近一条${noun}更新于 ${formatRelativeTime(current.lastAt)}`
                  : `还没有${noun}`
                : `左侧选一条${noun}开始处理，或在下方挑一个应用`}
            </p>
          </div>
          {appid !== null ? (
            <button
              type="button"
              onClick={() => onSelectApp(null, false)}
              className="ml-auto shrink-0 text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
            >
              查看全部应用
            </button>
          ) : null}
        </div>

        {!stats ? (
          <div className="space-y-4">
            <Skeleton className="h-24 rounded-2xl" />
            <Skeleton className="h-36 rounded-2xl" />
          </div>
        ) : (
          <>
            <Headline stats={stats} />
            <Tiles stats={stats} />
          </>
        )}

        {/* 按应用 */}
        {appid === null ? (
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium">按应用</h3>
              <span className="text-xs text-muted-foreground">我创建的应用排在前面</span>
            </div>
            {appsQuery.isLoading ? (
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {Array.from({ length: 3 }, (_, index) => (
                  <Skeleton key={index} className="h-28 rounded-xl" />
                ))}
              </div>
            ) : cards.length === 0 ? (
              <p className="rounded-xl border border-dashed px-4 py-6 text-center text-xs text-muted-foreground">没有可见的应用</p>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {cards.map((app, index) => (
                  <AppCard key={app.appid} app={app} index={index} noun={noun} onClick={() => onSelectApp(app.appid)} />
                ))}
              </div>
            )}
          </section>
        ) : null}

        {stats ? (
          <div className="grid gap-6 md:grid-cols-2">
            <section className="space-y-3">
              <h3 className="text-sm font-medium">优先级构成</h3>
              <PriorityBars stats={stats} />
            </section>
            {categories.length > 0 ? (
              <section className="space-y-2">
                <h3 className="text-sm font-medium">分类分布</h3>
                <ChartContainer config={CATEGORY_CONFIG} className="aspect-auto w-full" style={{ height: categories.length * 30 + 8 }}>
                  <BarChart data={categories} layout="vertical" margin={{ top: 0, right: 16, bottom: 0, left: 0 }}>
                    <XAxis type="number" hide />
                    <YAxis type="category" dataKey="categoryName" width={76} tickLine={false} axisLine={false} fontSize={12} />
                    <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel />} />
                    <Bar dataKey="count" fill="var(--color-count)" radius={4} barSize={12} />
                  </BarChart>
                </ChartContainer>
              </section>
            ) : null}
          </div>
        ) : null}

        <section className="space-y-3 rounded-xl border bg-muted/20 p-4">
          <h3 className="text-xs font-medium text-muted-foreground">快捷键</h3>
          <ShortcutList />
        </section>
      </div>
    </div>
  );
}

function Headline({ stats }: { stats: TicketStats }) {
  const overdue = stats.overdueFirstResponse + stats.overdueResolve;
  const items = [
    { label: "已超时", value: overdue, icon: AlarmClock, tone: overdue > 0 ? "text-destructive" : "text-foreground" },
    { label: "未指派", value: stats.unassigned, icon: UserX, tone: "text-foreground" },
    { label: "我受理", value: stats.mineAssigned, icon: UserCheck, tone: "text-foreground" }
  ];
  return (
    <div className="flex flex-wrap items-end gap-x-10 gap-y-4 rounded-2xl border bg-card px-6 py-5">
      <div>
        <p className="text-xs text-muted-foreground">待受理</p>
        <NumberFlow value={stats.open} className="text-5xl font-semibold tabular-nums tracking-tight text-foreground" />
      </div>
      {items.map((item) => (
        <div key={item.label} className="pb-1.5">
          <p className="flex items-center gap-1 text-xs text-muted-foreground">
            <item.icon className="size-3.5" />
            {item.label}
          </p>
          <NumberFlow value={item.value} className={cn("text-2xl font-semibold tabular-nums", item.tone)} />
        </div>
      ))}
    </div>
  );
}

function Tiles({ stats }: { stats: TicketStats }) {
  const tiles = [
    { label: "处理中", value: String(stats.processing) },
    { label: "等待中", value: String(stats.pending) },
    { label: "今日新增", value: String(stats.createdToday) },
    { label: "今日解决", value: String(stats.resolvedToday) },
    { label: "平均首响", value: formatDuration(stats.avgFirstResponseMs) },
    { label: "平均解决", value: formatDuration(stats.avgResolveMs) },
    { label: "满意度", value: stats.ratingCount ? `${stats.avgRating.toFixed(1)} 分` : "—", hint: stats.ratingCount ? `${stats.ratingCount} 条评价` : "暂无评价" },
    { label: "累计", value: String(stats.total) }
  ];
  return (
    <dl className="grid grid-cols-2 gap-px overflow-hidden rounded-2xl border bg-border sm:grid-cols-4">
      {tiles.map((tile) => (
        <div key={tile.label} className="min-w-0 bg-card px-4 py-3">
          <dt className="text-xs text-muted-foreground">{tile.label}</dt>
          <dd className="mt-0.5 truncate text-sm font-semibold tabular-nums text-foreground">{tile.value}</dd>
          {tile.hint ? <dd className="truncate text-[11px] text-muted-foreground">{tile.hint}</dd> : null}
        </div>
      ))}
    </dl>
  );
}

function PriorityBars({ stats }: { stats: TicketStats }) {
  const total = PRIORITY_ORDER.reduce((acc, key) => acc + (stats.byPriority?.[key] ?? 0), 0);
  if (!total) return <p className="text-xs text-muted-foreground">暂无数据</p>;
  return (
    <div className="space-y-3">
      <div className="flex h-2.5 overflow-hidden rounded-full bg-muted">
        {PRIORITY_ORDER.map((key) => {
          const value = stats.byPriority?.[key] ?? 0;
          return value ? (
            <motion.span
              key={key}
              initial={{ width: 0 }}
              animate={{ width: `${(value / total) * 100}%` }}
              transition={{ duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
              className={cn("h-full", PRIORITY_BAR[key])}
            />
          ) : null;
        })}
      </div>
      <ul className="grid grid-cols-2 gap-x-6 gap-y-1.5">
        {PRIORITY_ORDER.map((key) => {
          const value = stats.byPriority?.[key] ?? 0;
          return (
            <li key={key} className="flex items-center gap-2 text-xs">
              <span className={cn("size-2 rounded-full", PRIORITY_BAR[key])} />
              <span className="text-muted-foreground">{PRIORITY_LABEL[key]}</span>
              <span className="ml-auto tabular-nums text-foreground">{value}</span>
              <span className="w-9 text-right tabular-nums text-muted-foreground">{Math.round((value / total) * 100)}%</span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function AppCard({ app, index, noun, onClick }: { app: TicketAppSummary; index: number; noun: string; onClick: () => void }) {
  return (
    <motion.button
      type="button"
      onClick={onClick}
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, delay: Math.min(index, 8) * 0.03 }}
      className="group/card flex flex-col gap-3 rounded-xl border bg-card p-4 text-left outline-none transition-[border-color,box-shadow] hover:border-primary/40 hover:shadow-sm focus-visible:ring-2 focus-visible:ring-ring/40"
    >
      <div className="flex items-center gap-2">
        <AppMark appid={app.appid} name={app.appName} className="size-7 text-xs" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{app.appName}</span>
        {app.owned ? <Crown className="size-3.5 shrink-0 text-amber-500" aria-label="我创建的" /> : null}
        <ArrowRight className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/card:opacity-100" />
      </div>
      <div className="flex items-end justify-between gap-2">
        <div>
          <p className="text-2xl font-semibold leading-none tabular-nums">{app.active}</p>
          <p className="mt-1 text-[11px] text-muted-foreground">未结{noun}</p>
        </div>
        <div className="text-right text-[11px] leading-5 text-muted-foreground">
          {app.overdue ? <p className="font-medium text-destructive">超时 {app.overdue}</p> : null}
          {app.unaccepted ? <p>待受理 {app.unaccepted}</p> : null}
          {app.unassigned ? <p>未指派 {app.unassigned}</p> : null}
          {!app.overdue && !app.unaccepted && !app.unassigned ? <p>{app.lastAt ? formatRelativeTime(app.lastAt) : `还没有${noun}`}</p> : null}
        </div>
      </div>
    </motion.button>
  );
}
