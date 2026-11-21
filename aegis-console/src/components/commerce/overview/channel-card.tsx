"use client";

import { useMemo } from "react";
import { Bar, BarChart, Cell, LabelList, XAxis, YAxis } from "recharts";
import { CreditCard } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { CardEmpty, CardLink, OverviewCard } from "@/components/overview/overview-kit";
import { useAdminPaymentMethodsQuery } from "@/lib/admin-hooks";
import type { OrderGroupStat } from "@/lib/api/types";
import { buildChartConfig } from "../commerce-charts";
import { compactMoney, formatPercent, moneyRatio, sumMoney } from "../commerce-format";
import { moneyTooltipRow, RefreshSpinner } from "./overview-shared";

/**
 * 支付渠道排行：按金额从高到低，金额与占比写在柱尾，不用再对坐标轴。
 * 渠道没有固有语义，用分类色轮；配置键用序号而不是渠道标识 —— 标识里可能有点号，
 * 拼进 CSS 变量名就失效了。
 */
export function ChannelCard({
  items,
  appKey,
  refreshing,
  className
}: {
  items: OrderGroupStat[];
  appKey?: string;
  refreshing?: boolean;
  className?: string;
}) {
  const methodsQuery = useAdminPaymentMethodsQuery();
  const nameOf = useMemo(() => {
    const map = new Map((methodsQuery.data ?? []).map((item) => [item.method, item.name] as const));
    return (key: string) => map.get(key) ?? key;
  }, [methodsQuery.data]);

  const rows = useMemo(() => {
    const live = items.filter((item) => item.count > 0);
    const total = sumMoney(live.map((item) => item.amount));
    return [...live]
      .sort((a, b) => Number(b.amount) - Number(a.amount))
      .map((item, index) => ({
        key: `m${index}`,
        name: nameOf(item.key),
        amount: Number(item.amount),
        amountRaw: item.amount,
        count: item.count,
        tail: `${compactMoney(Number(item.amount))}  ${formatPercent(moneyRatio(item.amount, total), 0)}`,
        fill: `var(--color-m${index})`
      }));
  }, [items, nameOf]);

  const config = useMemo(() => buildChartConfig(rows.map((row) => ({ key: row.key, label: row.name }))), [rows]);
  const formatter = useMemo(() => moneyTooltipRow(config), [config]);

  return (
    <OverviewCard
      icon={CreditCard}
      tone="primary"
      title="支付渠道"
      description="按已支付金额"
      className={className}
      action={
        <>
          <RefreshSpinner active={refreshing} />
          {appKey ? <CardLink href={`/apps/${encodeURIComponent(appKey)}?tab=payment`}>配置</CardLink> : null}
        </>
      }
    >
      {rows.length === 0 ? (
        <CardEmpty icon={CreditCard} title="暂无渠道订单" />
      ) : (
        <ChartContainer config={config} className="aspect-auto w-full" style={{ height: Math.max(120, rows.length * 40) }}>
          <BarChart data={rows} layout="vertical" margin={{ left: 0, right: 84, top: 0, bottom: 0 }} barCategoryGap="20%">
            <XAxis type="number" hide />
            <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={84} fontSize={11} />
            <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel formatter={formatter} />} />
            <Bar dataKey="amount" radius={4} maxBarSize={20} minPointSize={4}>
              {rows.map((row) => (
                <Cell key={row.key} fill={row.fill} />
              ))}
              <LabelList dataKey="tail" position="right" offset={8} className="fill-muted-foreground font-data" fontSize={11} />
            </Bar>
          </BarChart>
        </ChartContainer>
      )}
    </OverviewCard>
  );
}
