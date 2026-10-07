"use client";

import { useMemo, type ReactNode } from "react";
import { Activity, ShieldAlert, Users, XCircle, type LucideIcon } from "lucide-react";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import type { AuditOverview as Overview, AuditStatItem } from "@/lib/api/types";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const TREND_SERIES = [
  { key: "operations", label: "操作", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "failed", label: "失败", color: { light: "#f43f5e", dark: "#fb7185" } },
  { key: "highRisk", label: "高风险", color: { light: "#f59e0b", dark: "#fbbf24" } }
];

/**
 * 顶部总览。「操作」不含查看类请求：把每次翻页都算进去，今日操作就失去了意义。
 * 失败与高风险两张卡可点击，直接把列表筛到对应结果。
 */
export function AuditOverview({
  overview,
  loading,
  activeTile,
  onFailed,
  onHighRisk,
  onModule,
  onAdmin
}: {
  overview?: Overview;
  loading: boolean;
  activeTile?: "failed" | "highRisk" | null;
  onFailed: () => void;
  onHighRisk: () => void;
  onModule: (key: string) => void;
  onAdmin: (name: string) => void;
}) {
  const chartConfig = useMemo(() => buildChartConfig(TREND_SERIES), []);
  const rows = useMemo(
    () =>
      (overview?.trend ?? []).map((day) => ({
        ...day,
        label: `${Number(day.day.slice(5, 7))}/${Number(day.day.slice(8, 10))}`
      })),
    [overview?.trend]
  );

  if (loading || !overview) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-[84px] rounded-xl" />
          ))}
        </div>
        <div className="grid gap-3 lg:grid-cols-[1.6fr_1fr]">
          <Skeleton className="h-[248px] rounded-xl" />
          <Skeleton className="h-[248px] rounded-xl" />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Tile icon={Activity} label="今日操作" value={overview.todayOperations} hint={`查看 ${overview.todayReads} 次，近 7 天操作 ${overview.weekOperations}`} />
        <Tile
          icon={XCircle}
          label="今日失败"
          value={overview.todayFailed}
          tone={overview.todayFailed ? "danger" : "muted"}
          hint={overview.todayFailed ? "点击查看失败记录" : "无失败操作"}
          active={activeTile === "failed"}
          onClick={onFailed}
        />
        <Tile
          icon={ShieldAlert}
          label="今日高风险"
          value={overview.todayHighRisk}
          tone={overview.todayHighRisk ? "warning" : "muted"}
          hint={overview.todayHighRisk ? "点击查看高风险操作" : "无高风险操作"}
          active={activeTile === "highRisk"}
          onClick={onHighRisk}
        />
        <Tile icon={Users} label="活跃管理员" value={overview.activeAdmins} hint="近 7 天有过操作" />
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.6fr_1fr]">
        <ChartCard
          title="近 14 天操作趋势"
          height={180}
          empty={rows.every((row) => row.operations === 0)}
          emptyText="近 14 天没有操作记录"
        >
          <ChartContainer config={chartConfig} className="h-[180px] w-full">
            <LineChart data={rows} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={32} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              {TREND_SERIES.map((series) => (
                <Line key={series.key} dataKey={series.key} type="monotone" stroke={`var(--color-${series.key})`} strokeWidth={2} dot={false} />
              ))}
              <ChartLegend content={<ChartLegendContent />} />
            </LineChart>
          </ChartContainer>
        </ChartCard>

        <div className="grid gap-3 rounded-xl border bg-card p-4 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
          <Ranking title="近 7 天模块分布" items={overview.topModules} onPick={(item) => onModule(item.key)} />
          <Ranking title="近 7 天最活跃" items={overview.topAdmins} onPick={(item) => onAdmin(item.label)} />
        </div>
      </div>
    </div>
  );
}

function Tile({
  icon: Icon,
  label,
  value,
  hint,
  tone = "default",
  active,
  onClick
}: {
  icon: LucideIcon;
  label: string;
  value: ReactNode;
  hint?: string;
  tone?: "default" | "danger" | "warning" | "muted";
  active?: boolean;
  onClick?: () => void;
}) {
  const body = (
    <>
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div
        className={cn(
          "mt-1 text-2xl font-semibold tabular-nums tracking-tight",
          tone === "danger" && "text-red-600 dark:text-red-400",
          tone === "warning" && "text-amber-600 dark:text-amber-400",
          tone === "muted" && "text-muted-foreground"
        )}
      >
        {value}
      </div>
      {hint ? <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{hint}</div> : null}
    </>
  );
  const base = "rounded-xl border bg-card px-4 py-3 text-left";
  if (!onClick) return <div className={base}>{body}</div>;
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(base, "transition-colors hover:bg-muted/50", active && "border-primary ring-1 ring-primary/30")}
    >
      {body}
    </button>
  );
}

function Ranking({ title, items, onPick }: { title: string; items: AuditStatItem[]; onPick: (item: AuditStatItem) => void }) {
  const max = Math.max(1, ...items.map((item) => item.count));
  return (
    <div className="min-w-0 space-y-2">
      <div className="text-xs font-medium text-muted-foreground">{title}</div>
      {items.length === 0 ? (
        <p className="py-4 text-center text-xs text-muted-foreground">暂无操作</p>
      ) : (
        <div className="space-y-1.5">
          {items.slice(0, 5).map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => onPick(item)}
              className="group block w-full text-left"
              title={`筛选：${item.label}`}
            >
              <div className="flex items-center justify-between gap-2 text-xs">
                <span className="truncate group-hover:text-foreground">{item.label}</span>
                <span className="shrink-0 tabular-nums text-muted-foreground">{item.count}</span>
              </div>
              <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-primary/70" style={{ width: `${(item.count / max) * 100}%` }} />
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
