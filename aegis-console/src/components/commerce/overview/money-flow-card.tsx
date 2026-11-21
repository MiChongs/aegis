"use client";

import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, Cell, LabelList, XAxis, YAxis } from "recharts";
import { BadgeCheck, Coins, ReceiptText, Undo2, UserRound, Waves } from "lucide-react";
import { Metric } from "@/components/apps/detail/app-detail-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { OverviewCard } from "@/components/overview/overview-kit";
import type { OrderStats } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { buildChartConfig, MONEY_COLORS } from "../commerce-charts";
import { compactMoney, divideMoney, formatMoney, formatPercent, moneyRatio, toCents } from "../commerce-format";
import { moneyTooltipRow, RefreshSpinner } from "./overview-shared";

const CONFIG = buildChartConfig([
  { key: "paid", label: "已支付", color: MONEY_COLORS.paid },
  { key: "refunded", label: "已退款", color: MONEY_COLORS.refunded },
  { key: "net", label: "实收净额", color: MONEY_COLORS.net },
  { key: "pending", label: "待支付", color: MONEY_COLORS.pending }
]);

/**
 * 资金流向：已支付 → 扣掉退款 → 实收净额，画成瀑布。
 *
 * 三个数在 KPI 格里并排时，人要自己做减法才知道退款吃掉了多少；
 * 瀑布把这一步画出来 —— 退款那根柱子悬在半空，长度就是被退回去的钱。
 * 待支付另起一根灰柱：它还没到账，不参与这条减法。
 */
export function MoneyFlowCard({
  orders,
  refreshing,
  className
}: {
  orders: OrderStats;
  refreshing?: boolean;
  className?: string;
}) {
  const rows = useMemo(() => {
    const paid = Number(orders.paidAmount);
    const refunded = Number(orders.refundedAmount);
    const net = Number(orders.netAmount);
    // 退款按退款成功时间计、实收按到账时间计，时间窗里退款可能比实收还多，净额为负；
    // 此时退款柱从 0 画到已支付，长度不再等于退款额，但提示框里的数字仍是真值
    const refundBase = Math.max(0, Math.min(net, paid));
    return [
      { key: "paid", name: "已支付", base: 0, value: paid, valueRaw: orders.paidAmount, fill: "var(--color-paid)" },
      {
        key: "refunded",
        name: "已退款",
        base: refundBase,
        value: Math.max(0, paid - refundBase),
        valueRaw: orders.refundedAmount,
        fill: "var(--color-refunded)"
      },
      { key: "net", name: "实收净额", base: 0, value: Math.max(0, net), valueRaw: orders.netAmount, fill: "var(--color-net)" },
      { key: "pending", name: "待支付", base: 0, value: Number(orders.pendingAmount), valueRaw: orders.pendingAmount, fill: "var(--color-pending)" }
    ];
  }, [orders]);

  const formatter = useMemo(() => moneyTooltipRow(CONFIG), []);
  const refundRate = moneyRatio(orders.refundedAmount, orders.paidAmount);
  const avgOrder = divideMoney(orders.paidAmount, orders.paidOrders);
  const perPayer = divideMoney(orders.netAmount, orders.payerCount);
  const negativeNet = toCents(orders.netAmount) < BigInt(0);
  const empty = orders.totalOrders === 0 && toCents(orders.refundedAmount) === BigInt(0);

  return (
    <OverviewCard
      icon={Waves}
      tone="emerald"
      title="资金流向"
      description="走支付渠道的订单资金"
      className={className}
      action={<RefreshSpinner active={refreshing} />}
    >
      <div className="@container/flow min-w-0">
        <div className="grid min-w-0 gap-5 @2xl/flow:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
          <div className="flex min-w-0 flex-col gap-4">
            <div className="min-w-0 space-y-1">
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <BadgeCheck className="size-3.5" />
                实收净额
              </span>
              <div
                className={cn(
                  "truncate font-data text-3xl leading-10 font-semibold tracking-tight tabular-nums sm:text-4xl",
                  negativeNet ? "text-rose-600 dark:text-rose-400" : "text-foreground"
                )}
                title={formatMoney(orders.netAmount)}
              >
                {formatMoney(orders.netAmount)}
              </div>
              <p className="truncate text-[11px] text-muted-foreground tabular-nums">
                已支付 {formatMoney(orders.paidAmount)}，退款 {formatMoney(orders.refundedAmount)}
              </p>
            </div>

            <div className="grid grid-cols-2 gap-2">
              <Metric
                label="付费用户"
                icon={UserRound}
                tone="sky"
                value={<span className="font-data">{orders.payerCount.toLocaleString("zh-CN")}</span>}
                hint={perPayer ? `人均实收 ${formatMoney(perPayer)}` : "暂无付费"}
              />
              <Metric
                label="客单价"
                icon={ReceiptText}
                tone="violet"
                value={<span className="font-data">{avgOrder ? formatMoney(avgOrder) : "—"}</span>}
                hint={`${orders.paidOrders.toLocaleString("zh-CN")} 笔已支付`}
              />
              <Metric
                label="退款率"
                icon={Undo2}
                tone="rose"
                emphasis={refundRate !== null && refundRate >= 0.1 ? "danger" : undefined}
                value={<span className="font-data">{formatPercent(refundRate)}</span>}
                hint={`${orders.refundCount.toLocaleString("zh-CN")} 笔退款`}
              />
              <Metric
                label="待支付"
                icon={Coins}
                tone="amber"
                value={<span className="font-data">{formatMoney(orders.pendingAmount)}</span>}
                hint={`${orders.pendingOrders.toLocaleString("zh-CN")} 笔未到账`}
              />
            </div>
          </div>

          <div className="min-h-[240px] min-w-0">
            {empty ? (
              <div className="flex h-full min-h-[240px] items-center justify-center rounded-xl border border-dashed text-xs text-muted-foreground">
                时间窗内没有订单
              </div>
            ) : (
              <ChartContainer config={CONFIG} className="aspect-auto h-[260px] w-full">
                <BarChart data={rows} margin={{ left: 0, right: 4, top: 24, bottom: 0 }} barCategoryGap="22%">
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis dataKey="name" tickLine={false} axisLine={false} tickMargin={8} fontSize={11} />
                  <YAxis tickLine={false} axisLine={false} width={44} fontSize={11} tickFormatter={compactMoney} />
                  <ChartTooltip cursor={{ fillOpacity: 0.4 }} content={<ChartTooltipContent hideLabel formatter={formatter} />} />
                  {/* 透明底座把退款柱托到净额的高度，瀑布就是靠它悬空的 */}
                  <Bar dataKey="base" stackId="flow" fill="transparent" tooltipType="none" isAnimationActive={false} />
                  <Bar dataKey="value" stackId="flow" radius={[6, 6, 2, 2]}>
                    {rows.map((row) => (
                      <Cell
                        key={row.key}
                        fill={row.fill}
                        fillOpacity={row.key === "pending" ? 0.55 : 1}
                        strokeDasharray={row.key === "net" ? "4 3" : undefined}
                      />
                    ))}
                    <LabelList
                      dataKey="valueRaw"
                      position="top"
                      offset={6}
                      className="fill-muted-foreground font-data"
                      fontSize={11}
                      formatter={(value) => compactMoney(Number(value))}
                    />
                  </Bar>
                </BarChart>
              </ChartContainer>
            )}
          </div>
        </div>
      </div>
    </OverviewCard>
  );
}
