"use client";

import type { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import type { ChartConfig } from "@/components/ui/chart";

/**
 * 交易中心图表的共用外壳与配色。
 *
 * 配色与风控中心同一套约定：`ChartConfig` 的 `theme: { light, dark }`，
 * 由 shadcn 的 `ChartContainer` 注入成 `var(--color-<key>)`。
 * 不要在组件里写死十六进制色 —— 那样深色模式下要么刺眼要么看不见。
 */

export type ThemedColor = { light: string; dark: string };

/** 资金语义色：进项与出项必须分色系，否则「收了 10 万」和「退了 10 万」在图上一样。 */
export const MONEY_COLORS = {
  paid: { light: "#10b981", dark: "#34d399" },
  refunded: { light: "#f43f5e", dark: "#fb7185" },
  net: { light: "#6366f1", dark: "#818cf8" },
  walletIn: { light: "#0ea5e9", dark: "#38bdf8" },
  walletOut: { light: "#f59e0b", dark: "#fbbf24" }
} satisfies Record<string, ThemedColor>;

/** 订单状态色：与订单表格里的徽标同色，两处对不上会让人以为是两组数据。 */
export const ORDER_STATUS_COLORS: Record<string, ThemedColor> = {
  paid: { light: "#10b981", dark: "#34d399" },
  pending: { light: "#f59e0b", dark: "#fbbf24" },
  expired: { light: "#a1a1aa", dark: "#71717a" },
  failed: { light: "#f43f5e", dark: "#fb7185" },
  cancelled: { light: "#78716c", dark: "#a8a29e" },
  closed: { light: "#78716c", dark: "#a8a29e" }
};

/** 钱包流水类型色：入账走冷色、出账走暖色，一眼能分出资金方向。 */
export const WALLET_TYPE_COLORS: Record<string, ThemedColor> = {
  recharge: { light: "#10b981", dark: "#34d399" },
  refund: { light: "#0ea5e9", dark: "#38bdf8" },
  admin_adjust: { light: "#f59e0b", dark: "#fbbf24" },
  vip_purchase: { light: "#8b5cf6", dark: "#a78bfa" },
  order_pay: { light: "#6366f1", dark: "#818cf8" },
  consume: { light: "#f43f5e", dark: "#fb7185" }
};

/** 分类色轮：给渠道这类没有固有语义的维度用。 */
export const CATEGORICAL_COLORS: ThemedColor[] = [
  { light: "#6366f1", dark: "#818cf8" },
  { light: "#0ea5e9", dark: "#38bdf8" },
  { light: "#14b8a6", dark: "#2dd4bf" },
  { light: "#f59e0b", dark: "#fbbf24" },
  { light: "#ec4899", dark: "#f472b6" },
  { light: "#8b5cf6", dark: "#a78bfa" },
  { light: "#10b981", dark: "#34d399" },
  { light: "#f43f5e", dark: "#fb7185" }
];

export function categoricalColor(index: number) {
  return CATEGORICAL_COLORS[index % CATEGORICAL_COLORS.length];
}

/** 由「键 → 标签 → 配色」构建 ChartConfig。 */
export function buildChartConfig(
  items: Array<{ key: string; label: string; color?: ThemedColor }>,
  fallback: (index: number) => ThemedColor = categoricalColor
): ChartConfig {
  const config: ChartConfig = {};
  items.forEach((item, index) => {
    config[item.key] = { label: item.label, theme: item.color ?? fallback(index) };
  });
  return config;
}

/** 图表卡外壳：标题、右上角操作、加载骨架与空态一处收口。 */
export function ChartCard({
  title,
  description,
  action,
  loading,
  empty,
  emptyText = "该时间窗内没有记录",
  height = 240,
  children
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  loading?: boolean;
  empty?: boolean;
  emptyText?: string;
  height?: number;
  children: ReactNode;
}) {
  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 space-y-0.5">
            <h3 className="text-sm font-medium">{title}</h3>
            {description ? <p className="text-[11px] leading-snug text-muted-foreground">{description}</p> : null}
          </div>
          {action}
        </div>
        {loading ? (
          <Skeleton className="w-full rounded-xl" style={{ height }} />
        ) : empty ? (
          <div
            className="flex items-center justify-center rounded-xl border border-dashed text-xs text-muted-foreground"
            style={{ height }}
          >
            {emptyText}
          </div>
        ) : (
          children
        )}
      </CardContent>
    </Card>
  );
}
