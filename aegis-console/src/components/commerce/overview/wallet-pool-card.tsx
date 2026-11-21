"use client";

import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, Cell, ReferenceLine, XAxis, YAxis } from "recharts";
import { ArrowDownLeft, ArrowUpRight, Landmark, Scale, Users, Wallet } from "lucide-react";
import { Metric } from "@/components/apps/detail/app-detail-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { CardEmpty, CardLink, OverviewCard } from "@/components/overview/overview-kit";
import type { WalletStats } from "@/lib/api/types";
import { buildChartConfig, WALLET_TYPE_COLORS } from "../commerce-charts";
import { centsToMoney, compactMoney, formatMoney, toCents, walletTypeLabel } from "../commerce-format";
import { moneyTooltipRow, RefreshSpinner } from "./overview-shared";

const absMoney = (value: string) => {
  const cents = toCents(value);
  return centsToMoney(cents < BigInt(0) ? -cents : cents);
};

/**
 * 钱包资金池。
 *
 * 余额合计是**时点值**：平台此刻欠用户多少钱，不随时间窗变化，所以单独做成大数字；
 * 时间窗内的流水按类型画成左右分开的条形 —— 右边进池、左边出池，
 * 一眼能看出钱是从哪个口子进来、又从哪个口子流走的。
 */
export function WalletPoolCard({
  wallet,
  currency,
  refreshing,
  className
}: {
  wallet: WalletStats;
  currency?: string;
  refreshing?: boolean;
  className?: string;
}) {
  const rows = useMemo(
    () =>
      wallet.byType
        .filter((item) => item.count > 0 && toCents(item.amount) !== BigInt(0))
        .map((item) => ({
          key: item.type,
          name: walletTypeLabel(item.type),
          amount: Number(item.amount),
          amountRaw: absMoney(item.amount),
          count: item.count,
          fill: `var(--color-${item.type})`
        }))
        // 进项在上、出项在下，各自按金额从大到小
        .sort((a, b) => (a.amount >= 0) === (b.amount >= 0) ? Math.abs(b.amount) - Math.abs(a.amount) : a.amount >= 0 ? -1 : 1),
    [wallet.byType]
  );

  const config = useMemo(
    () => buildChartConfig(rows.map((row) => ({ key: row.key, label: row.name, color: WALLET_TYPE_COLORS[row.key] }))),
    [rows]
  );
  const formatter = useMemo(() => moneyTooltipRow(config, currency), [config, currency]);
  const netNegative = toCents(wallet.net) < BigInt(0);
  const chartHeight = Math.max(200, rows.length * 34 + 24);

  return (
    <OverviewCard
      icon={Wallet}
      tone="amber"
      title="钱包资金池"
      description={currency ? `记账币种 ${currency}` : undefined}
      className={className}
      action={
        <>
          <RefreshSpinner active={refreshing} />
          <CardLink href="/commerce?tab=wallet">流水</CardLink>
        </>
      }
    >
      <div className="@container/pool min-w-0">
        <div className="grid min-w-0 gap-5 @2xl/pool:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
          <div className="flex min-w-0 flex-col gap-4">
            <div className="min-w-0 space-y-1">
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Landmark className="size-3.5" />
                待兑付余额
              </span>
              <div className="truncate font-data text-3xl leading-10 font-semibold tracking-tight tabular-nums sm:text-4xl" title={formatMoney(wallet.balance, currency)}>
                {formatMoney(wallet.balance)}
              </div>
              <p className="text-[11px] text-muted-foreground">全部用户此刻的余额合计，不随时间窗变化</p>
            </div>

            <div className="grid grid-cols-2 gap-2">
              <Metric
                label="入账合计"
                icon={ArrowDownLeft}
                tone="emerald"
                value={<span className="font-data">{formatMoney(absMoney(wallet.totalIn))}</span>}
              />
              <Metric
                label="出账合计"
                icon={ArrowUpRight}
                tone="rose"
                value={<span className="font-data">{formatMoney(absMoney(wallet.totalOut))}</span>}
              />
              <Metric
                label="净流入"
                icon={Scale}
                tone={netNegative ? "rose" : "sky"}
                emphasis={netNegative ? "warn" : undefined}
                value={<span className="font-data">{formatMoney(wallet.net)}</span>}
              />
              <Metric
                label="流水笔数"
                icon={Users}
                tone="violet"
                value={<span className="font-data">{wallet.count.toLocaleString("zh-CN")}</span>}
                hint={`${wallet.userCount.toLocaleString("zh-CN")} 位用户`}
              />
            </div>
          </div>

          <div className="min-w-0">
            {rows.length === 0 ? (
              <CardEmpty icon={Wallet} title="时间窗内没有钱包流水" description="充值、消费与调账都会记在这里" />
            ) : (
              <>
                <div className="mb-1 flex justify-between px-1 text-[10px] text-muted-foreground">
                  <span>← 流出</span>
                  <span>流入 →</span>
                </div>
                <ChartContainer config={config} className="aspect-auto w-full" style={{ height: chartHeight }}>
                  <BarChart data={rows} layout="vertical" margin={{ left: 0, right: 12, top: 0, bottom: 0 }} barCategoryGap="24%">
                    <CartesianGrid horizontal={false} strokeDasharray="3 3" />
                    <XAxis type="number" tickLine={false} axisLine={false} fontSize={11} tickFormatter={compactMoney} />
                    <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={76} fontSize={11} />
                    <ReferenceLine x={0} className="stroke-border" />
                    <ChartTooltip cursor={{ fillOpacity: 0.35 }} content={<ChartTooltipContent hideLabel formatter={formatter} />} />
                    <Bar dataKey="amount" radius={3} maxBarSize={18}>
                      {rows.map((row) => (
                        <Cell key={row.key} fill={row.fill} />
                      ))}
                    </Bar>
                  </BarChart>
                </ChartContainer>
              </>
            )}
          </div>
        </div>
      </div>
    </OverviewCard>
  );
}
