"use client";

import { useMemo, useState } from "react";
import { Bar, CartesianGrid, ComposedChart, Line, ReferenceLine, XAxis, YAxis } from "recharts";
import { ArrowDownLeft, ArrowUpRight, ChartColumnBig, Coins, Undo2, type LucideIcon } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { CardEmpty, OverviewCard } from "@/components/overview/overview-kit";
import type { CommerceTrend } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { buildChartConfig, MONEY_COLORS } from "../commerce-charts";
import { centsToMoney, compactMoney, formatMoney, toCents } from "../commerce-format";
import { bucketLabels, ChartColorScope, moneyTooltipRow, RefreshSpinner, Swatch } from "./overview-shared";

type SeriesKey = "paid" | "refunded" | "walletIn" | "walletOut";
type Group = "revenue" | "wallet";

const SERIES: Array<{ key: SeriesKey; group: Group; label: string; icon: LucideIcon; wallet?: boolean }> = [
  { key: "paid", group: "revenue", label: "已支付", icon: Coins },
  { key: "refunded", group: "revenue", label: "已退款", icon: Undo2 },
  { key: "walletIn", group: "wallet", label: "钱包入账", icon: ArrowDownLeft, wallet: true },
  { key: "walletOut", group: "wallet", label: "钱包出账", icon: ArrowUpRight, wallet: true }
];

/** 每组的「进 / 出 / 净」三个键：进朝上、出朝下，净额是算出来的，画虚线 */
const GROUP_KEYS: Record<Group, { up: SeriesKey; down: SeriesKey; net: "net" | "walletNet"; netLabel: string }> = {
  revenue: { up: "paid", down: "refunded", net: "net", netLabel: "实收净额" },
  wallet: { up: "walletIn", down: "walletOut", net: "walletNet", netLabel: "钱包净流入" }
};

const CONFIG = buildChartConfig([
  { key: "paid", label: "已支付", color: MONEY_COLORS.paid },
  { key: "refunded", label: "已退款", color: MONEY_COLORS.refunded },
  { key: "net", label: "实收净额", color: MONEY_COLORS.net },
  { key: "walletIn", label: "钱包入账", color: MONEY_COLORS.walletIn },
  { key: "walletOut", label: "钱包出账", color: MONEY_COLORS.walletOut },
  { key: "walletNet", label: "钱包净流入", color: MONEY_COLORS.walletNet }
]);

const abs = (cents: bigint) => (cents < BigInt(0) ? -cents : cents);

/**
 * 交易趋势。
 *
 * 上方四个汇总格同时是图表的切换器：点「已支付 / 已退款」看收入组，点钱包两格看钱包组。
 * 每组只画三样东西 —— 进项朝上、出项朝下、净额一条虚线：两组六条线挤在一张图上看起来很全，
 * 但没有人能从里面读出结论；朝上朝下则一眼分得出钱往哪边走。
 */
export function TrendCard({
  trend,
  walletCurrency,
  refreshing,
  className
}: {
  trend?: CommerceTrend;
  walletCurrency?: string;
  refreshing?: boolean;
  className?: string;
}) {
  const [active, setActive] = useState<SeriesKey>("paid");
  const group = SERIES.find((item) => item.key === active)!.group;
  const keys = GROUP_KEYS[group];
  const points = useMemo(() => trend?.points ?? [], [trend]);

  const totals = useMemo(() => {
    const sum = (pick: (point: (typeof points)[number]) => string) =>
      centsToMoney(points.reduce((acc, point) => acc + abs(toCents(pick(point))), BigInt(0)));
    return {
      paid: sum((point) => point.paidAmount),
      refunded: sum((point) => point.refundedAmount),
      walletIn: sum((point) => point.walletIn),
      walletOut: sum((point) => point.walletOut)
    } satisfies Record<SeriesKey, string>;
  }, [points]);

  const rows = useMemo(
    () =>
      points.map((point) => {
        const walletIn = abs(toCents(point.walletIn));
        const walletOut = abs(toCents(point.walletOut));
        const refunded = abs(toCents(point.refundedAmount));
        return {
          label: point.label,
          paid: Number(point.paidAmount),
          paidRaw: point.paidAmount,
          refunded: -Number(centsToMoney(refunded)),
          refundedRaw: centsToMoney(refunded),
          net: Number(point.netAmount),
          netRaw: point.netAmount,
          walletIn: Number(centsToMoney(walletIn)),
          walletInRaw: centsToMoney(walletIn),
          walletOut: -Number(centsToMoney(walletOut)),
          walletOutRaw: centsToMoney(walletOut),
          walletNet: Number(centsToMoney(walletIn - walletOut)),
          walletNetRaw: centsToMoney(walletIn - walletOut)
        };
      }),
    [points]
  );

  const formatter = useMemo(() => moneyTooltipRow(CONFIG, group === "wallet" ? walletCurrency : undefined), [group, walletCurrency]);
  const allZero = rows.every((row) => !row[keys.up] && !row[keys.down]);

  return (
    <OverviewCard
      icon={ChartColumnBig}
      tone="violet"
      title="交易趋势"
      description={bucketLabels[trend?.bucket ?? "day"] ?? "按天"}
      className={className}
      action={<RefreshSpinner active={refreshing} />}
    >
      <ChartColorScope config={CONFIG} className="flex min-w-0 flex-1 flex-col">
        <div role="tablist" aria-label="趋势指标" className="grid grid-cols-2 gap-2 lg:grid-cols-4">
          {SERIES.map((series) => {
            const selected = active === series.key;
            const inGroup = series.group === group;
            return (
              <button
                key={series.key}
                type="button"
                role="tab"
                aria-selected={selected}
                onClick={() => setActive(series.key)}
                className={cn(
                  "relative flex min-w-0 flex-col gap-1.5 overflow-hidden rounded-lg border px-3 py-2.5 text-left transition-colors",
                  "focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
                  selected ? "border-foreground/15 bg-muted/60" : inGroup ? "bg-muted/25 hover:bg-muted/40" : "hover:bg-muted/40"
                )}
              >
                <span
                  aria-hidden
                  className={cn("absolute inset-x-0 top-0 h-0.5 bg-(--color-key) transition-opacity", inGroup ? "opacity-100" : "opacity-0")}
                  style={{ "--color-key": `var(--color-${series.key})` } as React.CSSProperties}
                />
                <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
                  <series.icon className="size-3.5 shrink-0" />
                  <span className="truncate">{series.label}</span>
                  {series.wallet && walletCurrency ? <span className="ml-auto shrink-0 font-data text-[10px]">{walletCurrency}</span> : null}
                </span>
                <span className="truncate font-data text-lg leading-6 font-semibold tabular-nums sm:text-xl" title={formatMoney(totals[series.key])}>
                  {formatMoney(totals[series.key])}
                </span>
              </button>
            );
          })}
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <Swatch colorKey={keys.up} />
            {CONFIG[keys.up]?.label}（向上）
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Swatch colorKey={keys.down} />
            {CONFIG[keys.down]?.label}（向下）
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Swatch colorKey={keys.net} shape="dash" />
            {keys.netLabel}
          </span>
        </div>

        <div className="mt-2 min-h-[260px] flex-1">
          {rows.length === 0 || allZero ? (
            <CardEmpty icon={ChartColumnBig} title={rows.length === 0 ? "暂无资金记录" : "金额均为 0"} description="换一个时间窗或应用再看" />
          ) : (
            <ChartContainer config={CONFIG} className="aspect-auto h-[260px] w-full">
              <ComposedChart data={rows} stackOffset="sign" margin={{ left: 0, right: 8, top: 8, bottom: 0 }} barCategoryGap="18%">
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={20} fontSize={11} />
                <YAxis tickLine={false} axisLine={false} width={48} fontSize={11} tickFormatter={compactMoney} />
                <ReferenceLine y={0} className="stroke-border" />
                <ChartTooltip cursor={{ fillOpacity: 0.35 }} content={<ChartTooltipContent formatter={formatter} />} />
                <Bar
                  dataKey={keys.up}
                  name={keys.up}
                  stackId="flow"
                  fill={`var(--color-${keys.up})`}
                  fillOpacity={active === keys.up ? 1 : 0.45}
                  radius={2}
                  maxBarSize={28}
                />
                <Bar
                  dataKey={keys.down}
                  name={keys.down}
                  stackId="flow"
                  fill={`var(--color-${keys.down})`}
                  fillOpacity={active === keys.down ? 1 : 0.45}
                  radius={2}
                  maxBarSize={28}
                />
                <Line
                  dataKey={keys.net}
                  name={keys.net}
                  type="monotone"
                  stroke={`var(--color-${keys.net})`}
                  strokeWidth={2}
                  strokeDasharray="4 3"
                  dot={false}
                  activeDot={{ r: 3 }}
                />
              </ComposedChart>
            </ChartContainer>
          )}
        </div>
      </ChartColorScope>
    </OverviewCard>
  );
}
