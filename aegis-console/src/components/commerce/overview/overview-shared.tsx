"use client";

import { useId } from "react";
import { ChartStyle, type ChartConfig, type ChartTooltipContent } from "@/components/ui/chart";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";
import { formatMoney } from "../commerce-format";

/**
 * 交易概览各卡片共用的小件。
 *
 * 图表里的数值是为了画图才转成 number 的；提示框要显示的是**原始字符串**，
 * 所以每行数据都带一份 `<key>Raw`，提示框按它格式化，不拿画图用的浮点数说话。
 */

export const bucketLabels: Record<string, string> = { day: "按天", week: "按周", month: "按月" };

export const orderStatusLabels: Record<string, string> = {
  paid: "已支付",
  pending: "待支付",
  expired: "已过期",
  failed: "失败",
  cancelled: "已取消",
  closed: "已关闭"
};

/** 换时间窗后台刷新时挂在卡片右上角；首屏加载走骨架，不走它 */
export function RefreshSpinner({ active }: { active?: boolean }) {
  if (!active) return null;
  return <Spinner className="size-3.5 text-muted-foreground" />;
}

type TooltipFormatter = NonNullable<React.ComponentProps<typeof ChartTooltipContent>["formatter"]>;

/**
 * 金额提示框的一行：色块 + 名称 + 金额。
 * 作为 `ChartTooltipContent` 的 formatter 用，金额取数据行上的 `<key>Raw`；
 * 色块与 shadcn 的取法一致：数据行自带 fill（按格着色的柱）优先，否则用系列色。
 */
export function moneyTooltipRow(config: ChartConfig, currency?: string): TooltipFormatter {
  return function MoneyTooltipRow(_value, name, item) {
    const key = String(name);
    const row = (item?.payload ?? {}) as Record<string, unknown>;
    const raw = row[`${key}Raw`];
    const color = (typeof row.fill === "string" ? row.fill : undefined) ?? item?.color;
    return (
      <div className="flex w-full items-center gap-2">
        <span aria-hidden className="size-2.5 shrink-0 rounded-[2px]" style={{ background: color }} />
        <span className="flex-1 text-muted-foreground">{config[key]?.label ?? String(row.name ?? key)}</span>
        <span className="font-data font-medium text-foreground tabular-nums">
          {formatMoney(typeof raw === "string" ? raw : "0", currency)}
        </span>
      </div>
    );
  };
}

/**
 * 让图表外的元素（列表色点、汇总格顶边）也能用 `var(--color-<key>)`。
 * 复用 shadcn 的 ChartStyle 注入同一份 ChartConfig，深浅色与图里的柱子一定同色。
 */
export function ChartColorScope({
  config,
  className,
  children
}: {
  config: ChartConfig;
  className?: string;
  children: React.ReactNode;
}) {
  const id = `scope-${useId().replace(/[^a-zA-Z0-9-]/g, "")}`;
  return (
    <div data-chart={id} className={className}>
      <ChartStyle id={id} config={config} />
      {children}
    </div>
  );
}

/** 一枚色块：放在 ChartColorScope 里用，`colorKey` 即 ChartConfig 的键 */
export function Swatch({ colorKey, shape = "square", className }: { colorKey: string; shape?: "square" | "dot" | "dash"; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "shrink-0",
        shape === "square" && "size-2.5 rounded-[2px] bg-(--swatch)",
        shape === "dot" && "size-2.5 rounded-full bg-(--swatch)",
        shape === "dash" && "h-0 w-3 border-t-2 border-dashed border-(--swatch)",
        className
      )}
      style={{ "--swatch": `var(--color-${colorKey})` } as React.CSSProperties}
    />
  );
}
