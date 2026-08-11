"use client";

import { useMemo, useState } from "react";
import {
  Activity, AlertTriangle, ChevronRight, Clock, Cpu, Gauge,
  ShieldAlert, Timer, TrendingUp, Users,
} from "lucide-react";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Label, Line, LineChart,
  Pie, PieChart, XAxis, YAxis,
} from "recharts";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { useRiskDashboardQuery } from "@/lib/risk-hooks";
import type { RiskDashboard, RiskSeriesPoint } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import {
  ACTION_COLORS, CATEGORICAL_COLORS, ChartSkeleton, DeltaHint, InlineEmpty, LEVEL_COLORS,
  actionChartConfig, ellipsisMiddle, fmtBucket, fmtNumber, fmtPercent, fmtRelative,
  levelChartConfig, useRiskCatalog,
} from "./risk-shared";

/**
 * 风控大盘。
 *
 * 三段：引擎状态（现在在跑什么）、概要指标（带环比）、图表（趋势 / 分布 / 榜单）。
 * 引擎状态放最上面是因为「大盘全是 0」有两种截然不同的原因：真没风险，
 * 或者根本没规则在跑。不先把这件事说清楚，后面的图全是误导。
 */

const RANGES = [
  { value: "24h", label: "近 24 小时", hours: 24 },
  { value: "7d", label: "近 7 天", hours: 24 * 7 },
  { value: "30d", label: "近 30 天", hours: 24 * 30 },
  { value: "90d", label: "近 90 天", hours: 24 * 90 },
] as const;

export function RiskDashboardPanel({ onInspectRule, onInspectIP, onInspectDevice }: {
  onInspectRule?: (ruleId: number) => void;
  onInspectIP?: (ip: string) => void;
  onInspectDevice?: (deviceId: string) => void;
}) {
  const [rangeKey, setRangeKey] = useState<string>("7d");

  // 区间随 rangeKey 派生。用 state + useEffect 同步会让每次渲染都拿到新的
  // ISO 字符串，query key 每次都变，于是永远在重新请求。
  const { start, end } = useMemo(() => {
    const hours = RANGES.find((r) => r.value === rangeKey)?.hours ?? 24 * 7;
    // 对齐到分钟，否则每次渲染都产生一个新的毫秒级 key。
    const alignedEnd = Math.floor(Date.now() / 60000) * 60000;
    return {
      start: new Date(alignedEnd - hours * 3600 * 1000).toISOString(),
      end: new Date(alignedEnd).toISOString(),
    };
  }, [rangeKey]);

  const query = useRiskDashboardQuery(start, end);
  const data = query.data;

  return (
    <div className="space-y-4">
      <EngineStatusCard data={data} rangeKey={rangeKey} onRangeChange={setRangeKey} />

      <SummaryCards data={data} loading={query.isLoading} />

      <TrendCard data={data} loading={query.isLoading} />

      <div className="grid gap-4 lg:grid-cols-3">
        <LevelDonutCard data={data} loading={query.isLoading} />
        <ActionBarCard data={data} loading={query.isLoading} />
        <ScoreHistogramCard data={data} loading={query.isLoading} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <SceneCard data={data} loading={query.isLoading} />
        <TopRulesCard data={data} loading={query.isLoading} onInspect={onInspectRule} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <TopIPsCard data={data} loading={query.isLoading} onInspect={onInspectIP} />
        <TopDevicesCard data={data} loading={query.isLoading} onInspect={onInspectDevice} />
      </div>
    </div>
  );
}

/* ── 引擎状态 ── */

function EngineStatusCard({ data, rangeKey, onRangeChange }: {
  data?: RiskDashboard; rangeKey: string; onRangeChange: (value: string) => void;
}) {
  const { sceneLabel } = useRiskCatalog();
  const engine = data?.engine;
  const uncovered = engine?.scenesUncovered ?? [];
  const noRules = engine ? engine.activeRules === 0 : false;
  const noActions = engine ? engine.activeActions === 0 : false;

  return (
    <Card>
      <CardContent className="flex flex-wrap items-center gap-x-5 gap-y-2 p-3">
        <span className="flex items-center gap-1.5 text-xs font-medium">
          <Cpu className="size-3.5 text-muted-foreground" />引擎状态
        </span>

        {engine ? (
          <>
            <Metric label="启用规则" value={`${engine.activeRules} / ${engine.totalRules}`} alert={noRules} />
            <Metric label="启用策略" value={`${engine.activeActions} / ${engine.totalActions}`} alert={noActions} />
            <Metric label="IP 情报源" value={engine.ipProviderReady ? engine.ipProvider : "未配置"} />
            <Metric label="频率限流" value={engine.rateLimitOn ? "已启用" : "未启用"} />

            {uncovered.length > 0 && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="warning" size="sm" className="cursor-help gap-1">
                    <AlertTriangle className="size-3" />
                    {uncovered.length} 个场景无规则
                  </Badge>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  <p className="text-xs">这些场景没有启用的规则：{uncovered.map(sceneLabel).join("、")}</p>
                </TooltipContent>
              </Tooltip>
            )}

            {(noRules || noActions) && (
              <span className="flex items-center gap-1 text-[11px] text-rose-600 dark:text-rose-400">
                <AlertTriangle className="size-3" />
                {noRules ? "无启用规则，所有请求得分为 0" : "无启用策略，命中后不执行任何动作"}
              </span>
            )}
          </>
        ) : (
          <span className="text-xs text-muted-foreground">读取中</span>
        )}

        <Select value={rangeKey} onValueChange={onRangeChange}>
          <SelectTrigger className="ml-auto h-7 w-28 shrink-0 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            {RANGES.map((r) => <SelectItem key={r.value} value={r.value}>{r.label}</SelectItem>)}
          </SelectContent>
        </Select>
      </CardContent>
    </Card>
  );
}

function Metric({ label, value, alert }: { label: string; value: string; alert?: boolean }) {
  return (
    <span className="flex items-baseline gap-1.5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className={cn("font-mono font-medium tabular-nums",
        alert && "text-rose-600 dark:text-rose-400")}>{value}</span>
    </span>
  );
}

/* ── 概要指标 ── */

function SummaryCards({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const s = data?.summary;
  if (loading && !s) {
    return (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {Array.from({ length: 8 }, (_, i) => <ChartSkeleton key={i} height={80} />)}
      </div>
    );
  }
  if (!s) return null;

  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <StatCard label="总评估" icon={Activity} value={fmtNumber(s.totalAssessments)}
        footer={<DeltaHint current={s.totalAssessments} previous={s.prevTotalAssessments} />} />
      <StatCard label="已拦截" icon={ShieldAlert} tone="danger" value={fmtNumber(s.totalBlocked)}
        footer={<DeltaHint current={s.totalBlocked} previous={s.prevTotalBlocked} invert />} />
      <StatCard label="拦截率" icon={Gauge} value={fmtPercent(s.blockRate)}
        footer={<DeltaHint current={s.blockRate} previous={s.prevBlockRate} invert />} />
      <StatCard label="待复核" icon={Clock} tone={s.pendingReviews > 0 ? "warning" : undefined}
        value={fmtNumber(s.pendingReviews)}
        footer={<Sub>累计复核 {fmtNumber(s.totalReviews)}</Sub>} />
      <StatCard label="平均分" icon={TrendingUp} value={s.avgScore.toFixed(1)}
        footer={<DeltaHint current={s.avgScore} previous={s.prevAvgScore} invert />} />
      <StatCard label="高危及以上" icon={AlertTriangle} tone="danger" value={fmtNumber(s.highRiskCount)}
        footer={<Sub>最高 {s.maxScore} 分</Sub>} />
      <StatCard label="独立 IP" icon={Users} value={fmtNumber(s.distinctIps)}
        footer={<Sub>设备 {fmtNumber(s.distinctDevices)}，账号 {fmtNumber(s.distinctAccounts)}</Sub>} />
      <StatCard label="判定耗时" icon={Timer} value={`${s.avgLatencyMs.toFixed(1)} ms`}
        footer={<Sub>登录链路平均开销</Sub>} />
    </div>
  );
}

function Sub({ children }: { children: React.ReactNode }) {
  return <span className="text-[10px] text-muted-foreground">{children}</span>;
}

function StatCard({ label, value, icon: Icon, tone, footer }: {
  label: string; value: string; icon: typeof Activity;
  tone?: "danger" | "warning"; footer?: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent className="p-3.5">
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <Icon className="size-3.5" />
          <span className="text-[10px] font-medium uppercase tracking-widest">{label}</span>
        </div>
        <div className={cn("mt-1 font-mono text-2xl font-bold tabular-nums",
          tone === "danger" && "text-rose-600 dark:text-rose-400",
          tone === "warning" && "text-amber-600 dark:text-amber-400")}>
          {value}
        </div>
        <div className="mt-0.5 min-h-4">{footer}</div>
      </CardContent>
    </Card>
  );
}

/* ── 趋势 ── */

type TrendView = "level" | "action" | "rate";

const TREND_VIEWS = [
  { key: "level", label: "风险等级" },
  { key: "action", label: "处置动作" },
  { key: "rate", label: "拦截率与均分" },
] as const;

function TrendCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { levels, actions } = useRiskCatalog();
  const [view, setView] = useState<TrendView>("level");

  const series = data?.series ?? [];
  const bucket = data?.range.bucket ?? "hour";

  const chartData = useMemo(
    () => series.map((point: RiskSeriesPoint) => ({
      ...point,
      label: fmtBucket(point.time, bucket),
      blockRatePct: Number((point.blockRate * 100).toFixed(2)),
    })),
    [series, bucket],
  );

  const config = useMemo<ChartConfig>(() => {
    if (view === "action") return actionChartConfig(actions);
    if (view === "rate") {
      return {
        blockRatePct: { label: "拦截率 %", theme: { light: "#f43f5e", dark: "#fb7185" } },
        avgScore: { label: "平均分", theme: { light: "#6366f1", dark: "#818cf8" } },
      };
    }
    return levelChartConfig(levels);
  }, [view, levels, actions]);

  const stackKeys = view === "action" ? actions.map((a) => a.value) : levels.map((l) => l.value);
  const hasData = chartData.some((point) => point.total > 0);

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold">评估趋势</h3>
            <Badge variant="outline" size="sm">{bucket === "day" ? "按天" : "按小时"}</Badge>
          </div>
          <SegmentedControl value={view} options={TREND_VIEWS} onChange={setView} />
        </div>

        {loading && chartData.length === 0 ? <ChartSkeleton height={260} />
          : !hasData ? <InlineEmpty text="该区间内没有评估记录" height={260} />
          : (
            <ChartContainer config={config} className="h-[260px] w-full">
              {view === "rate" ? (
                <LineChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                  <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} fontSize={10} />
                  <YAxis tickLine={false} axisLine={false} width={40} fontSize={10} />
                  <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
                  <ChartLegend content={<ChartLegendContent />} />
                  <Line dataKey="blockRatePct" type="monotone" stroke="var(--color-blockRatePct)" strokeWidth={2} dot={false} />
                  <Line dataKey="avgScore" type="monotone" stroke="var(--color-avgScore)" strokeWidth={2} dot={false} />
                </LineChart>
              ) : (
                <AreaChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                  <defs>
                    {stackKeys.map((key) => (
                      <linearGradient key={key} id={`risk-fill-${key}`} x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor={`var(--color-${key})`} stopOpacity={0.7} />
                        <stop offset="95%" stopColor={`var(--color-${key})`} stopOpacity={0.08} />
                      </linearGradient>
                    ))}
                  </defs>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
                  <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} fontSize={10} />
                  <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} fontSize={10} />
                  <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
                  <ChartLegend content={<ChartLegendContent />} />
                  {stackKeys.map((key) => (
                    <Area key={key} dataKey={key} type="monotone" stackId="risk"
                      stroke={`var(--color-${key})`} fill={`url(#risk-fill-${key})`} strokeWidth={1.5} />
                  ))}
                </AreaChart>
              )}
            </ChartContainer>
          )}
      </CardContent>
    </Card>
  );
}

function SegmentedControl<T extends string>({ value, options, onChange }: {
  value: T;
  options: ReadonlyArray<{ key: T; label: string }>;
  onChange: (next: T) => void;
}) {
  return (
    <div className="flex w-fit items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5">
      {options.map((option) => (
        <button key={option.key} type="button" onClick={() => onChange(option.key)}
          className={cn("rounded-md px-2.5 py-1 text-[11px] transition-colors",
            value === option.key ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground")}>
          {option.label}
        </button>
      ))}
    </div>
  );
}

/* ── 分布 ── */

function CardShell({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        {children}
      </CardContent>
    </Card>
  );
}

function LevelDonutCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { levels } = useRiskCatalog();
  const config = useMemo(() => levelChartConfig(levels), [levels]);
  const items = (data?.levelDistribution ?? []).filter((l) => l.count > 0);
  const total = items.reduce((sum, l) => sum + l.count, 0);
  const highShare = total > 0
    ? items.filter((i) => i.level === "high" || i.level === "critical").reduce((s, i) => s + i.count, 0) / total
    : 0;

  return (
    <CardShell title="风险等级分布">
      {loading && items.length === 0 ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="该区间内没有评估记录" height={220} />
        : (
          <>
            <ChartContainer config={config} className="mx-auto h-[220px] w-full">
              <PieChart>
                <ChartTooltip content={<ChartTooltipContent nameKey="level" hideLabel />} />
                <Pie data={items} dataKey="count" nameKey="level" innerRadius={55} outerRadius={85}
                  strokeWidth={2} paddingAngle={2}>
                  {items.map((item) => <Cell key={item.level} fill={`var(--color-${item.level})`} />)}
                  <Label content={({ viewBox }) => {
                    if (!viewBox || !("cx" in viewBox)) return null;
                    return (
                      <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                        <tspan x={viewBox.cx} y={viewBox.cy} className="fill-foreground text-xl font-bold">
                          {fmtNumber(total)}
                        </tspan>
                        <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 18} className="fill-muted-foreground text-[10px]">
                          次评估
                        </tspan>
                      </text>
                    );
                  }} />
                </Pie>
                <ChartLegend content={<ChartLegendContent nameKey="level" className="flex-wrap gap-x-3 gap-y-1" />} />
              </PieChart>
            </ChartContainer>
            <p className="text-[10px] text-muted-foreground">高危及以上占 {fmtPercent(highShare)}</p>
          </>
        )}
    </CardShell>
  );
}

function ActionBarCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { actions, actionLabel } = useRiskCatalog();
  const config = useMemo(() => actionChartConfig(actions), [actions]);
  const items = useMemo(
    () => (data?.actionDistribution ?? []).map((a) => ({ ...a, label: actionLabel(a.action) })),
    [data?.actionDistribution, actionLabel],
  );
  const total = items.reduce((sum, a) => sum + a.count, 0);

  return (
    <CardShell title="处置动作分布">
      {loading && items.length === 0 ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="该区间内没有评估记录" height={220} />
        : (
          <ChartContainer config={config} className="h-[220px] w-full">
            <BarChart data={items} layout="vertical" margin={{ top: 4, right: 24, bottom: 4, left: 4 }}>
              <CartesianGrid horizontal={false} strokeDasharray="3 3" className="stroke-border/40" />
              <XAxis type="number" hide />
              <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} width={56} fontSize={10} />
              <ChartTooltip content={<ChartTooltipContent nameKey="action" hideLabel />} />
              <Bar dataKey="count" radius={[0, 4, 4, 0]} barSize={18}>
                {items.map((item) => <Cell key={item.action} fill={`var(--color-${item.action})`} />)}
              </Bar>
            </BarChart>
          </ChartContainer>
        )}
    </CardShell>
  );
}

function ScoreHistogramCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { levels } = useRiskCatalog();
  const config = useMemo(() => levelChartConfig(levels), [levels]);
  const items = useMemo(
    // 桶边界与等级分档一致。用独立的分桶宽度会让直方图的峰和饼图的占比对不上。
    () => (data?.scoreHistogram ?? []).map((bucket) => ({
      ...bucket,
      label: bucket.max > 1000 ? `${bucket.min}+` : `${bucket.min}-${bucket.max}`,
    })),
    [data?.scoreHistogram],
  );
  const total = items.reduce((sum, b) => sum + b.count, 0);

  return (
    <CardShell title="分数分布">
      {loading && items.length === 0 ? <ChartSkeleton height={220} />
        : total === 0 ? <InlineEmpty text="该区间内没有评估记录" height={220} />
        : (
          <ChartContainer config={config} className="h-[220px] w-full">
            <BarChart data={items} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" className="stroke-border/40" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} fontSize={10} />
              <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} fontSize={10} />
              <ChartTooltip content={<ChartTooltipContent nameKey="level" hideLabel />} />
              <Bar dataKey="count" radius={[4, 4, 0, 0]}>
                {items.map((item) => <Cell key={item.level} fill={`var(--color-${item.level})`} />)}
              </Bar>
            </BarChart>
          </ChartContainer>
        )}
    </CardShell>
  );
}

function SceneCard({ data, loading }: { data?: RiskDashboard; loading: boolean }) {
  const { sceneLabel } = useRiskCatalog();
  const items = useMemo(
    () => (data?.sceneDistribution ?? []).map((s, index) => ({
      ...s,
      label: sceneLabel(s.scene),
      color: CATEGORICAL_COLORS[index % CATEGORICAL_COLORS.length],
    })),
    [data?.sceneDistribution, sceneLabel],
  );

  const config = useMemo<ChartConfig>(() => {
    const out: ChartConfig = { count: { label: "评估次数" } };
    items.forEach((item) => { out[item.scene] = { label: item.label, theme: item.color }; });
    return out;
  }, [items]);

  return (
    <CardShell title="场景分布">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="该区间内没有评估记录" height={200} />
        : (
          <>
            <ChartContainer config={config} className="h-[200px] w-full">
              <BarChart data={items} layout="vertical" margin={{ top: 4, right: 24, bottom: 4, left: 4 }}>
                <CartesianGrid horizontal={false} strokeDasharray="3 3" className="stroke-border/40" />
                <XAxis type="number" hide />
                <YAxis type="category" dataKey="label" tickLine={false} axisLine={false} width={56} fontSize={10} />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Bar dataKey="count" radius={[0, 4, 4, 0]} barSize={16}>
                  {items.map((item) => <Cell key={item.scene} fill={`var(--color-${item.scene})`} />)}
                </Bar>
              </BarChart>
            </ChartContainer>
            <Separator />
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted-foreground">
                <span className="w-12 shrink-0">场景</span>
                <span className="flex-1">拦截占比</span>
                <span className="w-24 shrink-0 text-right">拦截 / 均分</span>
              </div>
              {items.map((item) => (
                <div key={item.scene} className="flex items-center gap-2 text-[11px]">
                  <span className="w-12 shrink-0 text-muted-foreground">{item.label}</span>
                  <Progress value={item.count > 0 ? (item.blocked / item.count) * 100 : 0} className="h-1.5 flex-1" />
                  <span className="w-24 shrink-0 text-right tabular-nums text-muted-foreground">
                    {fmtNumber(item.blocked)} / {item.avgScore.toFixed(1)}
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
    </CardShell>
  );
}

/* ── 榜单 ── */

function RankRow({ onClick, children }: { onClick?: () => void; children: React.ReactNode }) {
  return (
    <button type="button" onClick={onClick} disabled={!onClick}
      className={cn("flex w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left text-xs transition-colors",
        onClick && "hover:border-primary/40 hover:bg-accent/40")}>
      {children}
      {onClick && <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
    </button>
  );
}

function TopRulesCard({ data, loading, onInspect }: {
  data?: RiskDashboard; loading: boolean; onInspect?: (ruleId: number) => void;
}) {
  const { conditionLabel } = useRiskCatalog();
  const items = data?.topRules ?? [];
  const max = items[0]?.hits || 1;

  return (
    <CardShell title="命中最多的规则">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="没有规则被命中，可能是阈值偏高" height={200} />
        : (
          <div className="space-y-1.5">
            {items.map((rule) => (
              <RankRow key={rule.ruleId} onClick={onInspect ? () => onInspect(rule.ruleId) : undefined}>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate font-medium">{rule.ruleName || `规则 ${rule.ruleId}`}</span>
                    <Badge variant="outline" size="sm" className="shrink-0">{conditionLabel(rule.conditionType)}</Badge>
                    {!rule.isActive && <Badge variant="warning" size="sm" className="shrink-0">已停用</Badge>}
                  </div>
                  <div className="mt-1 flex items-center gap-2">
                    <Progress value={(rule.hits / max) * 100} className="h-1.5 flex-1" />
                    <span className="w-36 shrink-0 text-right tabular-nums text-[10px] text-muted-foreground">
                      命中 {fmtNumber(rule.hits)}，拦截 {fmtNumber(rule.blocked)}，{fmtRelative(rule.lastHitAt)}
                    </span>
                  </div>
                </div>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}

function TopIPsCard({ data, loading, onInspect }: {
  data?: RiskDashboard; loading: boolean; onInspect?: (ip: string) => void;
}) {
  const items = data?.topIps ?? [];
  return (
    <CardShell title="高频 IP">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="该区间内没有评估记录" height={200} />
        : (
          <div className="space-y-1">
            {items.map((item) => (
              <RankRow key={item.ip} onClick={onInspect ? () => onInspect(item.ip) : undefined}>
                <span className="w-36 shrink-0 truncate font-mono">{item.ip}</span>
                <span className="w-10 shrink-0 text-muted-foreground">{item.country || "未知"}</span>
                {item.riskTag !== "normal" && <Badge variant="danger" size="sm">{item.riskTag}</Badge>}
                <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                  {fmtNumber(item.count)} 次，拦截 {fmtNumber(item.blocked)}，最高 {item.maxScore} 分
                </span>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}

function TopDevicesCard({ data, loading, onInspect }: {
  data?: RiskDashboard; loading: boolean; onInspect?: (deviceId: string) => void;
}) {
  const items = data?.topDevices ?? [];
  return (
    <CardShell title="高频设备">
      {loading && items.length === 0 ? <ChartSkeleton height={200} />
        : items.length === 0 ? <InlineEmpty text="客户端未上报设备标识时不会有记录" height={200} />
        : (
          <div className="space-y-1">
            {items.map((item) => (
              <RankRow key={item.deviceId} onClick={onInspect ? () => onInspect(item.deviceId) : undefined}>
                <span className="w-40 shrink-0 truncate font-mono" title={item.deviceId}>
                  {ellipsisMiddle(item.deviceId, 14, 8)}
                </span>
                {item.riskTag !== "normal" && <Badge variant="danger" size="sm">{item.riskTag}</Badge>}
                <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                  {fmtNumber(item.count)} 次，{fmtNumber(item.accounts)} 个账号，最高 {item.maxScore} 分
                </span>
              </RankRow>
            ))}
          </div>
        )}
    </CardShell>
  );
}
