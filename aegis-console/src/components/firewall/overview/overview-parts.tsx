"use client";

import type { ReactNode } from "react";
import NumberFlow from "@number-flow/react";
import { ArrowUpRight, type LucideIcon } from "lucide-react";
import { TonalIcon, type KitTone } from "@/components/apps/detail/app-detail-kit";
import { CardEmpty } from "@/components/overview/overview-kit";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { RankedItem } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";

/** 分段切换（时间窗、卡片内的维度切换） */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  ariaLabel,
  className
}: {
  value: T;
  options: ReadonlyArray<{ value: T; label: ReactNode }>;
  onChange: (value: T) => void;
  ariaLabel: string;
  className?: string;
}) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      value={value}
      aria-label={ariaLabel}
      onValueChange={(next) => {
        if (next) onChange(next as T);
      }}
      className={cn("max-w-full", className)}
    >
      {options.map((option) => (
        <ToggleGroupItem key={option.value} value={option.value} className="px-2.5 text-xs">
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

/** 指标格：整格可点，点了跳到事件页并带上对应筛选 */
export function KpiTile({
  icon,
  tone,
  label,
  value,
  footer,
  aside,
  onClick
}: {
  icon: LucideIcon;
  tone: KitTone;
  label: string;
  value: number;
  footer?: ReactNode;
  aside?: ReactNode;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-3.5 text-left transition-colors sm:p-4",
        "hover:border-foreground/15 hover:bg-muted/30 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      )}
    >
      <span className="flex min-w-0 items-center gap-2">
        <TonalIcon icon={icon} tone={tone} size="sm" />
        <span className="truncate text-xs text-muted-foreground">{label}</span>
        <ArrowUpRight aria-hidden className="ml-auto size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
      </span>
      <span className="flex min-w-0 items-end justify-between gap-2">
        <span className="truncate font-data text-2xl leading-8 font-semibold tracking-tight tabular-nums sm:text-[28px]">
          <NumberFlow
            value={value}
            locales="zh-CN"
            format={value >= 100_000 ? { notation: "compact", maximumFractionDigits: 1 } : undefined}
          />
        </span>
        {aside}
      </span>
      <span className="flex min-h-4 min-w-0 items-center gap-1 truncate text-[11px] text-muted-foreground">{footer}</span>
    </button>
  );
}

const regionNames = typeof Intl !== "undefined" && "DisplayNames" in Intl ? new Intl.DisplayNames(["zh-CN"], { type: "region" }) : null;

/** 两字母国家代码 → 中文名（查不到就原样返回） */
export function countryName(code: string) {
  const cc = code.trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(cc)) return code || "未知";
  try {
    return regionNames?.of(cc) ?? cc;
  } catch {
    return cc;
  }
}

export function formatShare(count: number, total: number) {
  if (total <= 0) return "—";
  const pct = (count / total) * 100;
  if (pct > 0 && pct < 0.1) return "<0.1%";
  return `${pct >= 10 ? Math.round(pct) : pct.toFixed(1)}%`;
}

/**
 * 排行列表。行身后的浅色条是相对第一名的长度，不是图表：
 * 每一行都要能点（点了带筛选进事件页），条只是帮眼睛比长短，数值与占比已经写在行尾。
 */
export function RankedList({
  items,
  total,
  renderLabel,
  onPick,
  barClassName = "bg-rose-500/10 dark:bg-rose-500/15",
  emptyIcon,
  emptyTitle,
  limit = 8
}: {
  items: RankedItem[];
  total: number;
  renderLabel: (item: RankedItem) => ReactNode;
  onPick?: (item: RankedItem) => void;
  barClassName?: string;
  emptyIcon: LucideIcon;
  emptyTitle: string;
  limit?: number;
}) {
  const rows = items.filter((item) => item.count > 0).slice(0, limit);
  if (rows.length === 0) return <CardEmpty icon={emptyIcon} title={emptyTitle} />;
  const max = rows[0]?.count ?? 1;
  return (
    <ol className="-mx-2 flex flex-col gap-0.5">
      {rows.map((item, index) => {
        const content = (
          <>
            <span
              aria-hidden
              className={cn("absolute inset-y-0.5 left-0 rounded-md transition-[width] duration-500", barClassName)}
              style={{ width: `${Math.max(3, (item.count / max) * 100)}%` }}
            />
            <span className="relative w-4 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{index + 1}</span>
            <span className="relative flex min-w-0 flex-1 items-center gap-1.5 text-xs">{renderLabel(item)}</span>
            <span className="relative shrink-0 font-data text-xs font-medium tabular-nums">{item.count.toLocaleString("zh-CN")}</span>
            <span className="relative w-11 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{formatShare(item.count, total)}</span>
          </>
        );
        const rowClass = "relative flex h-8 w-full min-w-0 items-center gap-2 overflow-hidden rounded-md px-2 text-left";
        return (
          <li key={`${item.key}-${index}`}>
            {onPick ? (
              <button
                type="button"
                onClick={() => onPick(item)}
                className={cn(
                  rowClass,
                  "transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                )}
              >
                {content}
              </button>
            ) : (
              <div className={rowClass}>{content}</div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
