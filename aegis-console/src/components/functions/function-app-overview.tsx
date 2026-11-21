"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, Label, Pie, PieChart, XAxis, YAxis } from "recharts";
import {
  Activity,
  AlertOctagon,
  Boxes,
  CheckCircle2,
  Code2,
  FileCode2,
  Gauge,
  Layers,
  ListChecks,
  Plus,
  Radar,
  ScrollText,
  ShieldAlert,
  Timer,
  Zap,
  type LucideIcon
} from "lucide-react";
import type {
  AppFunctionOverview,
  AppFunctionOverviewItem,
  FunctionCatalog
} from "@/lib/api/app-functions";
import { useFunctionAppOverviewQuery } from "@/lib/function-hooks";
import { CardEmpty, OverviewCard, Sparkline, formatCount } from "@/components/overview/overview-kit";
import { buildChartConfig, type ThemedColor } from "@/components/commerce/commerce-charts";
import { ChartColorScope, RefreshSpinner, Swatch } from "@/components/commerce/overview/overview-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig
} from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  SOURCE_LABELS,
  SourceBadge,
  capabilityGroupIcon,
  formatDuration,
  formatRate,
  formatRelative,
  formatTime,
  resolveCapabilityGroups
} from "./function-shared";

/** 打开某个函数的某个面板；invocationId 存在时直接展开那条调用的详情 */
export type OpenFunction = (name: string, panel?: string, invocationId?: number) => void;

const WINDOWS = [
  { hours: 1, label: "1 小时" },
  { hours: 24, label: "24 小时" },
  { hours: 168, label: "7 天" }
];

/** 成功与失败分色系：「跑了很多次」和「失败了很多次」必须一眼分得开 */
const TREND_CONFIG = {
  success: { label: "成功", theme: { light: "#10b981", dark: "#34d399" } },
  failed: { label: "失败", theme: { light: "#f43f5e", dark: "#fb7185" } }
} satisfies ChartConfig;

/** 来源固定顺序与配色：按数量排序会让同一来源每次换位置、换颜色 */
const SOURCE_ORDER = ["api", "event", "schedule", "console", "function"] as const;
const SOURCE_COLORS: Record<string, ThemedColor> = {
  api: { light: "#0ea5e9", dark: "#38bdf8" },
  event: { light: "#8b5cf6", dark: "#a78bfa" },
  schedule: { light: "#f59e0b", dark: "#fbbf24" },
  console: { light: "#71717a", dark: "#a1a1aa" },
  function: { light: "#14b8a6", dark: "#2dd4bf" }
};

type Health = { key: string; label: string; variant: "success" | "warning" | "danger" | "outline"; rank: number; stroke: string };

/**
 * 健康度只有五档，排序即处理优先级：先看在出错的，再看调不通的，最后才是好的与闲着的。
 * 「异常」的门槛与单函数概览一致（成功率低于 90%）。
 */
function healthOf(item: AppFunctionOverviewItem): Health {
  if (item.total > 0 && item.successRate < 0.9)
    return { key: "bad", label: "异常", variant: "danger", rank: 0, stroke: "text-rose-500" };
  if (item.status !== "active" || !item.activeVersion)
    return { key: "off", label: "不可调用", variant: "warning", rank: 1, stroke: "text-amber-500" };
  if (item.failed > 0)
    return { key: "warn", label: "有失败", variant: "warning", rank: 2, stroke: "text-amber-500" };
  if (!item.total) return { key: "idle", label: "闲置", variant: "outline", rank: 4, stroke: "text-muted-foreground" };
  return { key: "ok", label: "健康", variant: "success", rank: 3, stroke: "text-emerald-500" };
}

/**
 * 远程函数的应用全景：进 /functions 的第一屏。
 *
 * 回答的是「这个应用的函数整体跑得怎么样、碰了哪些系统、哪里在出错」——
 * 单函数的概览只能一个一个点进去看，函数一多就没人会去看了。
 * 一个接口取齐全部数据（/function-overview），几块卡片不会出现「趋势已刷新、
 * 错误列表还是上一个时间窗」的自相矛盾。
 */
export function FunctionAppOverview({
  appKey,
  catalog,
  onOpen,
  onCreate
}: {
  appKey: string;
  catalog?: FunctionCatalog;
  onOpen: OpenFunction;
  onCreate: () => void;
}) {
  const [hours, setHours] = useState(24);
  const query = useFunctionAppOverviewQuery(appKey, hours);
  const data = query.data;
  const refreshing = query.isFetching && !query.isLoading;

  return (
    <div className="flex flex-col gap-3 pb-3">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-medium">运行全景</h2>
        <RefreshSpinner active={refreshing} />
        <div className="ml-auto flex items-center gap-2">
          <div className="flex items-center gap-0.5 rounded-lg border bg-card p-0.5">
            {WINDOWS.map((item) => (
              <button
                key={item.hours}
                type="button"
                onClick={() => setHours(item.hours)}
                className={cn(
                  "rounded-md px-2.5 py-1 text-xs transition-colors",
                  hours === item.hours ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
                )}
              >
                {item.label}
              </button>
            ))}
          </div>
          <Button size="sm" className="h-8 text-xs" onClick={onCreate}>
            <Plus className="size-3.5" />
            创建函数
          </Button>
        </div>
      </div>

      {!data ? (
        <OverviewSkeleton />
      ) : data.functions === 0 ? (
        <Card className="py-10">
          <CardEmpty icon={Code2} title="暂无函数" />
          <div className="flex justify-center">
            <Button size="sm" onClick={onCreate}>
              <Plus className="size-4" />
              创建函数
            </Button>
          </div>
        </Card>
      ) : (
        <>
          <KpiStrip data={data} />
          <div className="grid gap-3 lg:grid-cols-3">
            <TrendCard data={data} className="lg:col-span-2" />
            <SourceCard data={data} />
          </div>
          <div className="grid gap-3 lg:grid-cols-3">
            <CoverageCard data={data} catalog={catalog} className="lg:col-span-2" />
            <RecentErrorsCard data={data} onOpen={onOpen} />
          </div>
          <FunctionsCard data={data} onOpen={onOpen} />
        </>
      )}
    </div>
  );
}

function OverviewSkeleton() {
  return (
    <div className="flex flex-col gap-3">
      <Skeleton className="h-24 w-full rounded-xl" />
      <div className="grid gap-3 lg:grid-cols-3">
        <Skeleton className="h-72 rounded-xl lg:col-span-2" />
        <Skeleton className="h-72 rounded-xl" />
      </div>
      <Skeleton className="h-64 w-full rounded-xl" />
    </div>
  );
}

// ── 指标条 ──

function KpiStrip({ data }: { data: AppFunctionOverview }) {
  const rateTone =
    data.success + data.failed === 0 ? "default" : data.successRate < 0.9 ? "danger" : data.successRate < 0.99 ? "warning" : "success";
  return (
    <Card className="grid grid-cols-2 gap-0 overflow-hidden py-0 sm:grid-cols-3 xl:grid-cols-6">
      <Kpi icon={Boxes} label="可被调用" value={`${data.active}`} suffix={`/ ${data.functions}`} sub="个函数" />
      <Kpi icon={Activity} label="调用量" value={formatCount(data.total)} sub={`失败 ${formatCount(data.failed)}`} subTone={data.failed ? "danger" : undefined} />
      <Kpi
        icon={CheckCircle2}
        label="成功率"
        value={formatRate(data.successRate, data.success + data.failed)}
        tone={rateTone}
        sub={`成功 ${formatCount(data.success)}`}
      />
      <Kpi icon={Timer} label="P95 耗时" value={data.total ? formatDuration(data.p95Ms) : "—"} sub="已结束的调用" />
      <Kpi icon={Zap} label="触发器" value={`${data.triggersEnabled}`} suffix={`/ ${data.triggers}`} sub="启用中" />
      <Kpi
        icon={ListChecks}
        label="触发队列"
        value={formatCount(data.pendingJobs)}
        sub={`失败 ${formatCount(data.failedJobs)}`}
        subTone={data.failedJobs ? "danger" : undefined}
      />
    </Card>
  );
}

function Kpi({
  icon: Icon,
  label,
  value,
  suffix,
  sub,
  tone = "default",
  subTone
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  suffix?: string;
  sub?: string;
  tone?: "default" | "success" | "warning" | "danger";
  subTone?: "danger";
}) {
  return (
    <div className="min-w-0 border-b border-r px-4 py-3 last:border-r-0">
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </p>
      <p
        className={cn(
          "mt-1.5 truncate font-data text-2xl font-semibold tabular-nums",
          tone === "success" && "text-emerald-600 dark:text-emerald-400",
          tone === "warning" && "text-amber-600 dark:text-amber-400",
          tone === "danger" && "text-destructive"
        )}
      >
        {value}
        {suffix ? <span className="ml-1 text-sm font-normal text-muted-foreground">{suffix}</span> : null}
      </p>
      {sub ? (
        <p className={cn("mt-0.5 truncate text-[11px] text-muted-foreground", subTone === "danger" && "text-destructive")}>{sub}</p>
      ) : null}
    </div>
  );
}

// ── 调用趋势 ──

function TrendCard({ data, className }: { data: AppFunctionOverview; className?: string }) {
  const long = data.windowHours > 24;
  const rows = useMemo(
    () =>
      data.buckets.map((bucket) => {
        const at = new Date(bucket.at);
        return {
          at: long
            ? `${at.getMonth() + 1}/${at.getDate()} ${String(at.getHours()).padStart(2, "0")}时`
            : `${String(at.getHours()).padStart(2, "0")}:00`,
          success: bucket.success,
          failed: bucket.failed
        };
      }),
    [data.buckets, long]
  );
  const empty = data.total === 0;
  return (
    <OverviewCard
      icon={Activity}
      tone="emerald"
      title="调用趋势"
      description={`按小时，共 ${data.total.toLocaleString("zh-CN")} 次`}
      className={className}
    >
      {empty ? (
        <CardEmpty icon={Activity} title="暂无调用" />
      ) : (
        <ChartContainer config={TREND_CONFIG} className="h-60 w-full">
          <BarChart data={rows} barCategoryGap={long ? 1 : "18%"}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="at" tickLine={false} axisLine={false} fontSize={11} minTickGap={28} />
            <YAxis tickLine={false} axisLine={false} fontSize={11} width={36} allowDecimals={false} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <ChartLegend content={<ChartLegendContent />} />
            <Bar dataKey="success" stackId="calls" fill="var(--color-success)" radius={[0, 0, 0, 0]} />
            <Bar dataKey="failed" stackId="calls" fill="var(--color-failed)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      )}
    </OverviewCard>
  );
}

// ── 来源构成 ──

function SourceCard({ data }: { data: AppFunctionOverview }) {
  const rows = useMemo(() => {
    const counts = new Map(data.sources.map((item) => [item.source, item.count]));
    const known = SOURCE_ORDER.map((key) => ({ key: key as string, count: counts.get(key) ?? 0 }));
    // 后端新增的来源排在后面，不丢
    for (const item of data.sources) {
      if (!SOURCE_ORDER.includes(item.source as (typeof SOURCE_ORDER)[number])) known.push({ key: item.source, count: item.count });
    }
    return known.map((row) => ({ ...row, label: SOURCE_LABELS[row.key] ?? row.key, fill: `var(--color-${row.key})` }));
  }, [data.sources]);
  const config = useMemo(
    () => buildChartConfig(rows.map((row) => ({ key: row.key, label: row.label, color: SOURCE_COLORS[row.key] }))),
    [rows]
  );
  const total = rows.reduce((sum, row) => sum + row.count, 0);
  const pieRows = rows.filter((row) => row.count > 0);

  return (
    <OverviewCard icon={Radar} tone="sky" title="调用来源" description={`近 ${data.windowHours} 小时`}>
      <ChartColorScope config={config} className="flex min-w-0 flex-col gap-3">
        <ChartContainer config={config} className="mx-auto aspect-square h-[150px]">
          <PieChart>
            {total ? <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} /> : null}
            <Pie
              data={total ? pieRows : [{ key: "none", count: 1, fill: "var(--muted)" }]}
              dataKey="count"
              nameKey="key"
              innerRadius={48}
              outerRadius={68}
              strokeWidth={3}
              paddingAngle={total ? 2 : 0}
              cornerRadius={4}
            >
              {(total ? pieRows : [{ key: "none", fill: "var(--muted)" }]).map((row) => (
                <Cell key={row.key} fill={row.fill} />
              ))}
              <Label
                content={({ viewBox }) => {
                  if (!viewBox || !("cx" in viewBox)) return null;
                  const cy = viewBox.cy ?? 0;
                  return (
                    <text x={viewBox.cx} y={cy} textAnchor="middle" dominantBaseline="middle">
                      <tspan x={viewBox.cx} y={cy - 6} className="fill-foreground font-data text-xl font-semibold">
                        {formatCount(total)}
                      </tspan>
                      <tspan x={viewBox.cx} y={cy + 14} className="fill-muted-foreground text-[11px]">
                        次调用
                      </tspan>
                    </text>
                  );
                }}
              />
            </Pie>
          </PieChart>
        </ChartContainer>
        <div className="space-y-1">
          {rows.map((row) => (
            <div key={row.key} className={cn("flex items-center gap-2 text-xs", !row.count && "text-muted-foreground")}>
              <Swatch colorKey={row.key} shape="dot" />
              <span className="flex-1">{row.label}</span>
              <span className="font-data tabular-nums">{row.count.toLocaleString("zh-CN")}</span>
              <span className="w-12 text-right font-data text-muted-foreground tabular-nums">
                {total ? `${((row.count / total) * 100).toFixed(1)}%` : "—"}
              </span>
            </div>
          ))}
        </div>
      </ChartColorScope>
    </OverviewCard>
  );
}

// ── 系统覆盖 ──

/**
 * 「这个应用的函数碰了哪些系统」：每个能力分组一格，底色深浅是用到它的函数占比，
 * 格内每个小方块是组里的一项能力（实心 = 至少一个函数声明了它，红色 = 声明了的高风险能力）。
 * 分组的标题、说明与顺序全部来自后端目录。
 */
function CoverageCard({
  data,
  catalog,
  className
}: {
  data: AppFunctionOverview;
  catalog?: FunctionCatalog;
  className?: string;
}) {
  const declared = useMemo(() => new Map(data.capabilities.map((item) => [item.key, item.count])), [data.capabilities]);
  const tiles = useMemo(() => {
    const capabilities = catalog?.capabilities ?? [];
    return resolveCapabilityGroups(capabilities, catalog?.groups)
      .map((group) => {
        const items = capabilities.filter(
          (item) => item.group === group.key && (!item.deprecated || declared.has(item.key))
        );
        const keys = new Set(items.map((item) => item.key));
        const functions = data.items.filter((fn) => fn.capabilities.some((key) => keys.has(key))).length;
        return { group, items, functions, used: items.filter((item) => declared.has(item.key)).length };
      })
      .filter((tile) => tile.items.length > 0 && (tile.group.key !== "legacy" || tile.used > 0));
  }, [catalog, data.items, declared]);

  const touched = tiles.filter((tile) => tile.functions > 0).length;
  const highRisk = (catalog?.capabilities ?? []).filter((item) => item.risk === "high" && declared.has(item.key));

  return (
    <OverviewCard
      icon={Layers}
      tone="violet"
      title="系统覆盖"
      description={catalog ? `触及 ${touched} / ${tiles.length} 个系统` : "加载能力目录"}
      className={className}
      action={
        highRisk.length ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="danger" size="sm" className="gap-1">
                <ShieldAlert className="size-3" />
                高风险 {highRisk.length}
              </Badge>
            </TooltipTrigger>
            <TooltipContent className="max-w-72">
              {highRisk.map((item) => `${item.label}（${declared.get(item.key)} 个函数）`).join("、")}
            </TooltipContent>
          </Tooltip>
        ) : null
      }
    >
      {!catalog ? (
        <Skeleton className="h-48 w-full rounded-xl" />
      ) : (
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {tiles.map(({ group, items, functions, used }) => {
            const Icon = capabilityGroupIcon(group.key);
            const share = data.functions ? functions / data.functions : 0;
            return (
              <div
                key={group.key}
                className={cn(
                  "space-y-2 rounded-lg border p-2.5 transition-colors",
                  functions ? "border-primary/25" : "border-dashed"
                )}
                style={
                  functions
                    ? { backgroundColor: `color-mix(in oklab, var(--primary) ${Math.round(4 + share * 14)}%, transparent)` }
                    : undefined
                }
              >
                <div className="flex items-start gap-2">
                  <Icon className={cn("mt-0.5 size-3.5 shrink-0", functions ? "text-primary" : "text-muted-foreground")} />
                  <div className="min-w-0 flex-1">
                    <p className={cn("truncate text-xs font-medium", !functions && "text-muted-foreground")}>{group.label}</p>
                    <p className="truncate text-[11px] text-muted-foreground" title={group.hint}>
                      {functions ? `${functions} 个函数，${used} / ${items.length} 项能力` : "未使用"}
                    </p>
                  </div>
                </div>
                <div className="flex flex-wrap gap-1">
                  {items.map((item) => {
                    const count = declared.get(item.key) ?? 0;
                    return (
                      <Tooltip key={item.key}>
                        <TooltipTrigger asChild>
                          <span
                            className={cn(
                              "size-2.5 rounded-[3px]",
                              count
                                ? item.risk === "high"
                                  ? "bg-rose-500"
                                  : "bg-primary"
                                : "bg-muted-foreground/15"
                            )}
                          />
                        </TooltipTrigger>
                        <TooltipContent>
                          {item.label}
                          {count ? `：${count} 个函数` : "：未声明"}
                        </TooltipContent>
                      </Tooltip>
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </OverviewCard>
  );
}

// ── 最近错误 ──

function RecentErrorsCard({ data, onOpen }: { data: AppFunctionOverview; onOpen: OpenFunction }) {
  return (
    <OverviewCard
      icon={AlertOctagon}
      tone="rose"
      title="最近错误"
      description={data.recentErrors.length ? `最近 ${data.recentErrors.length} 次失败` : undefined}
      contentClassName="px-2 sm:px-3"
    >
      {!data.recentErrors.length ? (
        <CardEmpty icon={CheckCircle2} title="暂无失败调用" />
      ) : (
        <div className="max-h-[22rem] space-y-0.5 overflow-y-auto">
          {data.recentErrors.map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => onOpen(item.function, "invocations", item.id)}
              className="w-full space-y-1 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted/60"
            >
              <span className="flex items-center gap-1.5">
                <span className="min-w-0 truncate font-mono text-xs font-medium">{item.function}</span>
                <SourceBadge source={item.source} />
                <span className="ml-auto shrink-0 text-[11px] text-muted-foreground" title={formatTime(item.createdAt)}>
                  {formatRelative(item.createdAt)}
                </span>
              </span>
              <span className="line-clamp-2 break-all text-[11px] text-destructive">{item.message || "执行失败"}</span>
            </button>
          ))}
        </div>
      )}
    </OverviewCard>
  );
}

// ── 函数一览 ──

function FunctionsCard({ data, onOpen }: { data: AppFunctionOverview; onOpen: OpenFunction }) {
  const rows = useMemo(
    () =>
      data.items
        .map((item) => ({ item, health: healthOf(item) }))
        .sort((a, b) => a.health.rank - b.health.rank || b.item.total - a.item.total),
    [data.items]
  );
  const counts = rows.reduce<Record<string, number>>((acc, row) => {
    acc[row.health.label] = (acc[row.health.label] ?? 0) + 1;
    return acc;
  }, {});

  return (
    <OverviewCard
      icon={Gauge}
      tone="primary"
      title="函数"
      description={Object.entries(counts)
        .map(([label, count]) => `${label} ${count}`)
        .join("，")}
      contentClassName="px-0 sm:px-0"
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-4 sm:pl-5">函数</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>版本</TableHead>
            <TableHead className="text-right">触发器</TableHead>
            <TableHead className="text-right">调用</TableHead>
            <TableHead className="text-right">成功率</TableHead>
            <TableHead className="text-right">P95</TableHead>
            <TableHead>趋势</TableHead>
            <TableHead>最近调用</TableHead>
            <TableHead className="pr-4 text-right sm:pr-5">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(({ item, health }) => (
            <TableRow key={item.name} className="cursor-pointer" onClick={() => onOpen(item.name, "script")}>
              <TableCell className="max-w-64 pl-4 sm:pl-5">
                <p className="truncate font-mono text-xs font-medium">{item.name}</p>
                {item.description ? (
                  <p className="truncate text-[11px] text-muted-foreground">{item.description}</p>
                ) : null}
              </TableCell>
              <TableCell>
                <Badge variant={health.variant} size="sm">
                  {health.label}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-[11px] text-muted-foreground">
                {item.activeVersion || "—"}
                {item.runtime !== "script" ? <span className="ml-1 rounded border px-1">{item.runtime}</span> : null}
              </TableCell>
              <TableCell className="text-right font-data text-xs tabular-nums">{item.triggers || "—"}</TableCell>
              <TableCell className="text-right font-data text-xs tabular-nums">
                {formatCount(item.total)}
                {item.failed ? <span className="ml-1 text-destructive">-{formatCount(item.failed)}</span> : null}
              </TableCell>
              <TableCell
                className={cn(
                  "text-right font-data text-xs tabular-nums",
                  item.total && item.successRate < 0.9 && "text-destructive"
                )}
              >
                {formatRate(item.successRate, item.total)}
              </TableCell>
              <TableCell className="text-right font-data text-xs tabular-nums">
                {item.total ? formatDuration(item.p95Ms) : "—"}
              </TableCell>
              <TableCell>
                <Sparkline values={item.spark} className={cn("h-6 w-20", health.stroke)} />
              </TableCell>
              <TableCell className="text-[11px] text-muted-foreground" title={formatTime(item.lastInvokedAt)}>
                {formatRelative(item.lastInvokedAt)}
              </TableCell>
              <TableCell className="pr-4 sm:pr-5" onClick={(event) => event.stopPropagation()}>
                <div className="flex justify-end gap-0.5">
                  <RowAction icon={FileCode2} label="脚本" onClick={() => onOpen(item.name, "script")} />
                  <RowAction icon={ScrollText} label="调用" onClick={() => onOpen(item.name, "invocations")} />
                  <RowAction icon={Zap} label="触发器" onClick={() => onOpen(item.name, "triggers")} />
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OverviewCard>
  );
}

function RowAction({ icon: Icon, label, onClick }: { icon: LucideIcon; label: string; onClick: () => void }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon-sm" variant="ghost" className="size-7" onClick={onClick} aria-label={label}>
          <Icon className="size-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
