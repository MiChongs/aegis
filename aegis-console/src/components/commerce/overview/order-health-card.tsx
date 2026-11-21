"use client";

import { useMemo } from "react";
import { Cell, Label, Pie, PieChart } from "recharts";
import { HeartPulse, Receipt } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Item, ItemActions, ItemContent, ItemGroup, ItemMedia, ItemTitle } from "@/components/ui/item";
import { CardEmpty, OverviewCard } from "@/components/overview/overview-kit";
import type { OrderStats } from "@/lib/api/types";
import { buildChartConfig, ORDER_STATUS_COLORS } from "../commerce-charts";
import { formatMoney } from "../commerce-format";
import { ChartColorScope, orderStatusLabels, RefreshSpinner, Swatch } from "./overview-shared";

/** 状态的固定顺序：先成功、再进行中、最后各种没成的。按数量排序会让同一状态每次换位置 */
const STATUS_ORDER = ["paid", "pending", "expired", "failed", "cancelled", "closed"];

/**
 * 订单健康：环图中心是支付成功率，下面逐个状态列出笔数与金额。
 * 成功率的分母是全部订单（含待支付）：待支付堆积本身就是要看的信号。
 */
export function OrderHealthCard({
  orders,
  refreshing,
  className
}: {
  orders: OrderStats;
  refreshing?: boolean;
  className?: string;
}) {
  const rows = useMemo(() => {
    const rank = (key: string) => {
      const index = STATUS_ORDER.indexOf(key);
      return index === -1 ? STATUS_ORDER.length : index;
    };
    return orders.byStatus
      .filter((item) => item.count > 0)
      .sort((a, b) => rank(a.key) - rank(b.key))
      .map((item) => ({ ...item, name: orderStatusLabels[item.key] ?? item.key, fill: `var(--color-${item.key})` }));
  }, [orders.byStatus]);

  const config = useMemo(
    () => buildChartConfig(rows.map((row) => ({ key: row.key, label: row.name, color: ORDER_STATUS_COLORS[row.key] }))),
    [rows]
  );

  const total = rows.reduce((sum, row) => sum + row.count, 0);
  const paid = rows.find((row) => row.key === "paid")?.count ?? 0;
  const successRate = total > 0 ? paid / total : null;

  return (
    <OverviewCard
      icon={HeartPulse}
      tone="sky"
      title="订单健康"
      description={`共 ${orders.totalOrders.toLocaleString("zh-CN")} 笔订单`}
      className={className}
      action={<RefreshSpinner active={refreshing} />}
    >
      {total === 0 ? (
        <CardEmpty icon={Receipt} title="暂无订单" description="时间窗内没有创建订单" />
      ) : (
        <ChartColorScope config={config} className="flex min-w-0 flex-col gap-3">
          <ChartContainer config={config} className="mx-auto aspect-square h-[180px]">
            <PieChart>
              <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
              <Pie data={rows} dataKey="count" nameKey="key" innerRadius={58} outerRadius={82} strokeWidth={3} paddingAngle={2} cornerRadius={4}>
                {rows.map((row) => (
                  <Cell key={row.key} fill={row.fill} />
                ))}
                <Label
                  content={({ viewBox }) => {
                    if (!viewBox || !("cx" in viewBox)) return null;
                    const cy = viewBox.cy ?? 0;
                    return (
                      <text x={viewBox.cx} y={cy} textAnchor="middle" dominantBaseline="middle">
                        <tspan x={viewBox.cx} y={cy - 6} className="fill-foreground font-data text-2xl font-semibold">
                          {successRate === null ? "—" : `${(successRate * 100).toFixed(1)}%`}
                        </tspan>
                        <tspan x={viewBox.cx} y={cy + 16} className="fill-muted-foreground text-[11px]">
                          支付成功率
                        </tspan>
                      </text>
                    );
                  }}
                />
              </Pie>
            </PieChart>
          </ChartContainer>

          <ItemGroup className="-mx-2">
            {rows.map((row) => (
              <Item key={row.key} size="sm" className="gap-2.5 px-2 py-1.5">
                <ItemMedia>
                  <Swatch colorKey={row.key} shape="dot" />
                </ItemMedia>
                <ItemContent className="min-w-0">
                  <ItemTitle className="text-xs font-normal">{row.name}</ItemTitle>
                </ItemContent>
                <ItemActions className="gap-3 font-data text-xs tabular-nums">
                  <span className="text-muted-foreground">{row.count.toLocaleString("zh-CN")} 笔</span>
                  <span className="w-24 truncate text-right">{formatMoney(row.amount)}</span>
                </ItemActions>
              </Item>
            ))}
          </ItemGroup>
        </ChartColorScope>
      )}
    </OverviewCard>
  );
}
