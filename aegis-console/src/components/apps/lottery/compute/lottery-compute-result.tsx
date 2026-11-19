"use client";

import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, ComposedChart, Line, LineChart, XAxis, YAxis } from "recharts";
import { AlertTriangle, ShieldCheck } from "lucide-react";
import {
  Metric,
  estimateSpread,
  formatDraws,
  formatValue,
  rateInterval
} from "@/components/apps/lottery/compute/lottery-compute-shared";
import { percent } from "@/components/apps/lottery/lottery-plan-view";
import { formatCount } from "@/components/apps/lottery/lottery-shared";
import { buildChartConfig, categoricalColor, ChartCard, type ThemedColor } from "@/components/commerce/commerce-charts";
import { Badge } from "@/components/ui/badge";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent
} from "@/components/ui/chart";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type {
  LotteryComputeDistribution,
  LotteryComputeResult,
  LotteryEstimate
} from "@/lib/api/lottery-compute";

/**
 * 一次推演的结果。
 *
 * 每个指标是各平行世界的均值，副文是 95% 置信区间：区间窄说明这个数可信，区间宽就该多演几遍。
 * 稀有奖品的出现比例看 Wilson 区间（全部世界合在一起算），十万分之一的大奖也给得出不含负数的范围。
 */

const DAILY_SERIES: Array<{ key: "points" | "value" | "realRate"; label: string; color: ThemedColor }> = [
  { key: "points", label: "发出积分", color: { light: "#f59e0b", dark: "#fbbf24" } },
  { key: "value", label: "发出价值", color: { light: "#6366f1", dark: "#818cf8" } },
  { key: "realRate", label: "真实奖品率", color: { light: "#10b981", dark: "#34d399" } }
];

const COUNT_COLOR: ThemedColor = { light: "#6366f1", dark: "#818cf8" };

const interval = (estimate: LotteryEstimate, format: (value: number) => string) =>
  estimate.n < 2 ? "只演了一遍，没有区间" : `${format(estimate.low)} – ${format(estimate.high)}`;

export function ComputeResultView({ result }: { result: LotteryComputeResult }) {
  const hasNone = result.prizes.some((prize) => prize.kind === "none");
  const arbitrage = result.arbitrage;
  const precision = result.precision;

  const dailyConfig = useMemo(() => buildChartConfig(DAILY_SERIES), []);
  const dailyRows = useMemo(
    () => result.daily.map((day) => ({ ...day, label: `第 ${day.day} 天`, realRate: Number((day.realRate * 100).toFixed(2)) })),
    [result.daily]
  );
  const stockConfig = useMemo(
    () => buildChartConfig(result.stock.map((series, index) => ({ key: `p${index}`, label: series.name || `位置 ${series.position + 1}` }))),
    [result.stock]
  );
  const stockRows = useMemo(
    () =>
      Array.from({ length: result.days }, (_, day) => {
        const row: Record<string, number | string> = { label: `第 ${day + 1} 天` };
        result.stock.forEach((series, index) => {
          row[`p${index}`] = series.used[day] ?? 0;
        });
        return row;
      }),
    [result.days, result.stock]
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
        <span>
          {formatCount(result.users)} 人 × {result.days} 天 × {result.replications} 遍，共 {formatDraws(result.draws)} 次
        </span>
        <span className="font-mono">种子 {result.seed}</span>
        {result.replications >= 2 ? (
          <Badge variant={precision.enough ? "success" : "warning"} size="sm">
            {precision.enough
              ? `精度 ±${percent(Math.max(precision.valueError, precision.rateError), 1)}`
              : `区间偏宽 ±${percent(Math.max(precision.valueError, precision.rateError), 1)}`}
          </Badge>
        ) : null}
      </div>

      {arbitrage ? (
        <div
          className={
            arbitrage.exceeds
              ? "flex items-start gap-2 rounded-xl border border-rose-500/30 bg-rose-500/[0.06] px-3 py-2 text-[11px] text-rose-800 dark:text-rose-300"
              : "flex items-start gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/[0.06] px-3 py-2 text-[11px] text-emerald-800 dark:text-emerald-300"
          }
        >
          {arbitrage.exceeds ? <AlertTriangle className="mt-px size-3.5 shrink-0" /> : <ShieldCheck className="mt-px size-3.5 shrink-0" />}
          <span>
            每抽积分回报 {formatValue(arbitrage.return.mean)}（{interval(arbitrage.return, formatValue)}），买一次 {arbitrage.price} 积分
            {arbitrage.exceeds ? "：用积分买次数稳赚" : ""}
            {arbitrage.probability > 0 && !arbitrage.exceeds ? `；${percent(arbitrage.probability, 0)} 的世界里回报超过价格` : ""}
          </span>
        </div>
      ) : null}

      <div className="grid gap-2 sm:grid-cols-3 lg:grid-cols-6">
        {hasNone ? (
          <Metric label="中奖率" value={percent(result.winRate.mean)} sub={interval(result.winRate, (v) => percent(v))} />
        ) : null}
        <Metric label="真实奖品率" value={percent(result.realRate.mean)} sub={interval(result.realRate, (v) => percent(v))} />
        <Metric label="每抽价值" value={formatValue(result.valuePerDraw.mean)} sub={interval(result.valuePerDraw, formatValue)} />
        <Metric label="每抽积分" value={formatValue(result.pointsPerDraw.mean)} sub={interval(result.pointsPerDraw, formatValue)} />
        <Metric
          label="全程发出积分"
          value={formatDraws(Math.round(result.pointsPerWorld.mean))}
          sub={interval(result.pointsPerWorld, (v) => formatDraws(Math.round(v)))}
        />
        <Metric label="从未抽到真实奖品" value={percent(result.neverWon.mean)} sub="占全部用户" />
        {result.pityShare.mean > 0 ? <Metric label="保底占比" value={percent(result.pityShare.mean)} /> : null}
        {result.fallbackShare.mean > 0 ? (
          <Metric label="无奖可出改发兜底" value={percent(result.fallbackShare.mean)} tone="down" />
        ) : null}
        {result.budget ? (
          <Metric
            label="积分预算"
            value={`${percent(result.budget.used, 0)} 用量`}
            sub={
              result.budget.share > 0
                ? `${percent(result.budget.share, 0)} 的日子用完，约 ${dayClock(result.budget.at)}`
                : `每天 ${formatCount(result.budget.dailyPoints)}，没有用完过`
            }
            tone={result.budget.share > 0 ? "down" : undefined}
          />
        ) : null}
      </div>

      {result.personas.length > 1 ? (
        <div className="overflow-x-auto rounded-xl border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>人群</TableHead>
                <TableHead className="text-right">人数</TableHead>
                <TableHead className="text-right">抽奖次数</TableHead>
                <TableHead className="text-right">真实奖品率</TableHead>
                <TableHead className="text-right">每抽价值</TableHead>
                <TableHead className="text-right">每抽积分回报</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {result.personas.map((persona) => (
                <TableRow key={persona.key}>
                  <TableCell className="text-xs">
                    {persona.name}
                    <span className="ml-1.5 text-muted-foreground">{percent(persona.share, 0)}</span>
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{formatCount(persona.users)}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">{formatDraws(persona.draws)}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {percent(persona.realRate.mean)}
                    <Spread estimate={persona.realRate} />
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {formatValue(persona.valuePerDraw.mean)}
                    <Spread estimate={persona.valuePerDraw} />
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {formatValue(persona.return.mean)}
                    <Spread estimate={persona.return} />
                    {persona.arbitrage ? (
                      <Badge variant="danger" size="sm" className="ml-1.5">
                        稳赚
                      </Badge>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}

      <div className="overflow-x-auto rounded-xl border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8">#</TableHead>
              <TableHead>奖品</TableHead>
              <TableHead className="text-right">公示</TableHead>
              <TableHead className="text-right">推演</TableHead>
              <TableHead className="text-right">95% 区间</TableHead>
              <TableHead className="text-right">出现</TableHead>
              <TableHead className="text-right">出不了</TableHead>
              <TableHead className="text-right">库存</TableHead>
              <TableHead className="text-right">每日限量</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {result.prizes.map((prize) => (
              <TableRow key={`${prize.prizeId}-${prize.position}`}>
                <TableCell className="font-mono text-[11px] text-muted-foreground">{prize.position + 1}</TableCell>
                <TableCell className="max-w-40 truncate text-xs">{prize.name || `位置 ${prize.position + 1}`}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">{percent(prize.baseRate)}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{percent(prize.rate.mean)}</TableCell>
                <TableCell className="text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                  {rateInterval(prize.low, prize.high)}
                </TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums">{formatCount(prize.wins)}</TableCell>
                <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                  {prize.blocked ? formatDraws(prize.blocked) : "—"}
                </TableCell>
                <TableCell className="text-right text-[11px] tabular-nums">
                  {prize.stockOut
                    ? prize.stockOut.probability > 0
                      ? `${percent(prize.stockOut.probability, 0)} 抽空，第 ${prize.stockOut.day.mean.toFixed(1)} 天`
                      : "没有抽空"
                    : "—"}
                </TableCell>
                <TableCell className="text-right text-[11px] tabular-nums">
                  {prize.dailyCap
                    ? prize.dailyCap.share > 0
                      ? `${percent(prize.dailyCap.share, 0)} 的日子触顶，约 ${dayClock(prize.dailyCap.at)}`
                      : "没有触顶"
                    : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {result.days > 1 ? (
          <ChartCard title="按天" height={220}>
            <ChartContainer config={dailyConfig} className="h-[220px] w-full">
              <ComposedChart data={dailyRows} margin={{ left: 0, right: 0, top: 8 }} barGap={2}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
                <YAxis yAxisId="amount" tickLine={false} axisLine={false} fontSize={11} width={44} tickFormatter={compact} />
                <YAxis yAxisId="rate" orientation="right" tickLine={false} axisLine={false} fontSize={11} width={36} unit="%" />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Bar yAxisId="amount" dataKey="points" fill="var(--color-points)" radius={[3, 3, 0, 0]} />
                <Bar yAxisId="amount" dataKey="value" fill="var(--color-value)" radius={[3, 3, 0, 0]} />
                <Line yAxisId="rate" dataKey="realRate" type="monotone" stroke="var(--color-realRate)" strokeWidth={2} dot={false} />
                <ChartLegend content={<ChartLegendContent />} />
              </ComposedChart>
            </ChartContainer>
          </ChartCard>
        ) : null}

        {result.stock.length ? (
          <ChartCard title="限量奖品累计已出" height={220}>
            <ChartContainer config={stockConfig} className="h-[220px] w-full">
              <LineChart data={stockRows} margin={{ left: 0, right: 8, top: 8 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
                <YAxis tickLine={false} axisLine={false} fontSize={11} width={44} tickFormatter={compact} />
                <ChartTooltip content={<ChartTooltipContent />} />
                {result.stock.map((series, index) => (
                  <Line
                    key={series.prizeId}
                    dataKey={`p${index}`}
                    type="monotone"
                    stroke={`var(--color-p${index})`}
                    strokeWidth={2}
                    dot={result.days <= 14}
                  />
                ))}
                <ChartLegend content={<ChartLegendContent />} />
              </LineChart>
            </ChartContainer>
          </ChartCard>
        ) : null}

        <DistributionChart title="每人抽到真实奖品的次数" unit="次" distribution={result.winsPerUser} color={categoricalColor(0)} />
        <DistributionChart title="每人最长连续没抽到真实奖品" unit="次" distribution={result.missStreak} color={categoricalColor(3)} />
        <DistributionChart title="第几抽第一次抽到真实奖品" unit="抽" distribution={result.firstWin} color={categoricalColor(2)} />

        <div className="space-y-3 rounded-xl border border-border bg-card p-4">
          <h3 className="text-sm font-medium">每人拿到的</h3>
          <QuantileRow label="价值（积分当量）" quantiles={result.valuePerUser} />
          <QuantileRow label="积分" quantiles={result.pointsPerUser} />
          {result.fortune ? <QuantileRow label="运气系数" quantiles={result.fortune} digits={2} /> : null}
        </div>
      </div>
    </div>
  );
}

/** 区间半宽，跟在数值后面：「±1.2%」 */
function Spread({ estimate }: { estimate: LotteryEstimate }) {
  const spread = estimateSpread(estimate);
  if (spread === null) return null;
  return <span className="ml-1 text-[10px] text-muted-foreground">±{percent(spread, 1)}</span>;
}

/** 一天里的时刻（0–1 → 「14:30」） */
function dayClock(progress: number) {
  const minutes = Math.round(Math.min(Math.max(progress, 0), 1) * 24 * 60);
  return `${String(Math.floor(minutes / 60) % 24).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
}

function compact(value: number) {
  if (Math.abs(value) >= 10_000) return `${(value / 10_000).toLocaleString("zh-CN", { maximumFractionDigits: 1 })}万`;
  return value.toLocaleString("zh-CN");
}

function DistributionChart({
  title,
  unit,
  distribution,
  color
}: {
  title: string;
  unit: string;
  distribution: LotteryComputeDistribution;
  color?: ThemedColor;
}) {
  const config = useMemo(() => buildChartConfig([{ key: "count", label: "人数", color: color ?? COUNT_COLOR }]), [color]);
  const rows = useMemo(
    () =>
      distribution.buckets.map((bucket) => ({
        label: bucket.from === bucket.to ? String(bucket.from) : `${bucket.from}–${bucket.to}`,
        count: bucket.count
      })),
    [distribution.buckets]
  );
  return (
    <ChartCard
      title={title}
      description={`均值 ${formatValue(distribution.mean)}，中位 ${distribution.p50}，P90 ${distribution.p90}，P99 ${distribution.p99}，最多 ${distribution.max} ${unit}`}
      empty={rows.length === 0}
      height={180}
    >
      <ChartContainer config={config} className="h-[180px] w-full">
        <BarChart data={rows} margin={{ left: 0, right: 8, top: 4 }}>
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={10} interval="preserveStartEnd" />
          <YAxis tickLine={false} axisLine={false} fontSize={11} width={44} tickFormatter={compact} />
          <ChartTooltip content={<ChartTooltipContent />} />
          <Bar dataKey="count" fill="var(--color-count)" radius={[2, 2, 0, 0]} />
        </BarChart>
      </ChartContainer>
    </ChartCard>
  );
}

function QuantileRow({
  label,
  quantiles,
  digits = 1
}: {
  label: string;
  quantiles: { mean: number; p10: number; p50: number; p90: number; p99: number; max: number };
  digits?: number;
}) {
  const cell = (value: number) => value.toLocaleString("zh-CN", { maximumFractionDigits: digits });
  const items: Array<[string, number]> = [
    ["均值", quantiles.mean],
    ["P10", quantiles.p10],
    ["中位", quantiles.p50],
    ["P90", quantiles.p90],
    ["P99", quantiles.p99],
    ["最多", quantiles.max]
  ];
  return (
    <div className="space-y-1">
      <span className="text-[11px] text-muted-foreground">{label}</span>
      <div className="grid grid-cols-6 gap-1">
        {items.map(([name, value]) => (
          <div key={name} className="rounded-md bg-muted px-1.5 py-1 text-center">
            <div className="text-[10px] text-muted-foreground">{name}</div>
            <div className="truncate font-mono text-[11px] tabular-nums">{cell(value)}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
