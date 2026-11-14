"use client";

import { useMemo } from "react";
import NumberFlow from "@number-flow/react";
import { Bar, BarChart, XAxis, YAxis } from "recharts";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { buildChartConfig } from "@/components/commerce/commerce-charts";
import { Skeleton } from "@/components/ui/skeleton";
import type { TicketStats } from "@/lib/api/tickets";
import { cn } from "@/lib/utils";
import { Kbd, formatDuration } from "@/components/tickets/ticket-shared";

// 反馈台的数字：页头一行摘要，以及没选中任何反馈时右侧的概览。
//
// 旧版在页面顶部常驻一条六格指标卡，占掉一整行高度，而处理反馈时没人看它。
// 现在常驻的只剩页头里一行小字，完整的数字放进本来空着的详情区。

const CATEGORY_CONFIG = buildChartConfig([{ key: "count", label: "数量", color: { light: "#0ea5e9", dark: "#38bdf8" } }]);

function rating(stats: TicketStats) {
  return stats.ratingCount ? stats.avgRating.toFixed(1) : "—";
}

/** 页头摘要：只留三件随时想知道的事 */
export function FeedbackStatsLine({ stats }: { stats?: TicketStats }) {
  if (!stats) return <Skeleton className="h-4 w-56" />;
  const overdue = stats.overdueFirstResponse + stats.overdueResolve;
  return (
    <p className="flex flex-wrap items-center gap-x-4 gap-y-0.5 text-xs text-muted-foreground">
      <span>
        今日新增 <NumberFlow value={stats.createdToday} className="font-medium tabular-nums text-foreground" />
      </span>
      <span>
        今日解决 <NumberFlow value={stats.resolvedToday} className="font-medium tabular-nums text-foreground" />
      </span>
      <span>
        平均首响 <span className="font-medium text-foreground">{formatDuration(stats.avgFirstResponseMs)}</span>
      </span>
      {overdue > 0 ? (
        <span>
          已超时 <NumberFlow value={overdue} className="font-medium tabular-nums text-destructive" />
        </span>
      ) : null}
    </p>
  );
}

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["J"], label: "下一条" },
  { keys: ["K"], label: "上一条" },
  { keys: ["/"], label: "搜索" },
  { keys: ["R"], label: "回复" },
  { keys: ["N"], label: "写备注" },
  { keys: ["E"], label: "解决" },
  { keys: ["A"], label: "指派给我" },
  { keys: ["I"], label: "资料面板" },
  { keys: ["."], label: "操作菜单" },
  { keys: ["Ctrl", "Enter"], label: "发送" }
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

/** 没选中反馈时的详情区：还有多少要处理、处理得怎么样、都在反馈什么 */
export function FeedbackOverview({ stats }: { stats?: TicketStats }) {
  const categories = useMemo(
    () => [...(stats?.byCategory ?? [])].filter((item) => item.count > 0).sort((a, b) => b.count - a.count).slice(0, 6),
    [stats?.byCategory]
  );

  if (!stats) {
    return (
      <div className="mx-auto flex h-full max-w-xl flex-col justify-center gap-6 p-8">
        <Skeleton className="h-16 w-40" />
        <Skeleton className="h-24" />
        <Skeleton className="h-40" />
      </div>
    );
  }

  const overdue = stats.overdueFirstResponse + stats.overdueResolve;
  const tiles = [
    { label: "处理中", value: String(stats.processing) },
    { label: "待补充", value: String(stats.pendingUser) },
    { label: "未指派", value: String(stats.unassigned) },
    { label: "平均首响", value: formatDuration(stats.avgFirstResponseMs) },
    { label: "平均解决", value: formatDuration(stats.avgResolveMs) },
    { label: "满意度", value: stats.ratingCount ? `${rating(stats)}（${stats.ratingCount} 评）` : "—" }
  ];

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto flex min-h-full max-w-xl flex-col justify-center gap-8 p-8">
        <div className="flex items-end gap-6">
          <div>
            <p className="text-xs text-muted-foreground">待受理</p>
            <NumberFlow value={stats.open} className="text-5xl font-semibold tabular-nums tracking-tight text-foreground" />
          </div>
          {overdue > 0 ? (
            <div className="pb-1.5">
              <p className="text-xs text-muted-foreground">已超时</p>
              <NumberFlow value={overdue} className="text-2xl font-semibold tabular-nums text-destructive" />
            </div>
          ) : null}
        </div>

        <dl className="grid grid-cols-3 gap-x-6 gap-y-4 border-y py-5">
          {tiles.map((tile) => (
            <div key={tile.label} className="min-w-0">
              <dt className="text-xs text-muted-foreground">{tile.label}</dt>
              <dd className="mt-0.5 truncate text-sm font-medium tabular-nums text-foreground">{tile.value}</dd>
            </div>
          ))}
        </dl>

        {categories.length > 0 ? (
          <div className="space-y-2">
            <p className="text-xs text-muted-foreground">分类分布</p>
            <ChartContainer config={CATEGORY_CONFIG} className="aspect-auto w-full" style={{ height: categories.length * 34 + 8 }}>
              <BarChart data={categories} layout="vertical" margin={{ top: 0, right: 16, bottom: 0, left: 0 }}>
                <XAxis type="number" hide />
                <YAxis type="category" dataKey="categoryName" width={76} tickLine={false} axisLine={false} />
                <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel />} />
                <Bar dataKey="count" fill="var(--color-count)" radius={4} barSize={14} />
              </BarChart>
            </ChartContainer>
          </div>
        ) : null}

        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">快捷键</p>
          <ShortcutList />
        </div>
      </div>
    </div>
  );
}
