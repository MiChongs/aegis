"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowRight, type LucideIcon } from "lucide-react";
import { TonalIcon, type KitTone } from "@/components/apps/detail/app-detail-kit";
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

/**
 * 概览页的展示原语。
 *
 * 每块都是 shadcn 的 Card：标题栏 = 色调图标 + 标题 + 右侧操作，内容区与页脚的左右边距一致
 * （手机 16、桌面 20），几块并排时边线、圆角和标题高度对齐。色调图标沿用应用详情页的 TonalIcon。
 */

export function OverviewCard({
  icon,
  tone = "muted",
  title,
  description,
  action,
  footer,
  children,
  className,
  contentClassName
}: {
  icon: LucideIcon;
  tone?: KitTone;
  title: string;
  description?: React.ReactNode;
  action?: React.ReactNode;
  footer?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  return (
    <Card className={cn("min-w-0 gap-4 py-4 sm:py-5", className)}>
      <CardHeader className="px-4 sm:px-5">
        <div className="flex min-w-0 items-center gap-3">
          <TonalIcon icon={icon} tone={tone} size="sm" />
          <div className="min-w-0">
            <CardTitle className="truncate text-sm leading-5">{title}</CardTitle>
            {description ? <CardDescription className="truncate text-xs leading-4">{description}</CardDescription> : null}
          </div>
        </div>
        {action ? <CardAction className="flex items-center gap-1.5 self-center">{action}</CardAction> : null}
      </CardHeader>
      <CardContent className={cn("flex min-w-0 flex-1 flex-col px-4 sm:px-5", contentClassName)}>{children}</CardContent>
      {footer ? <CardFooter className="border-t px-4 pt-3 sm:px-5 [.border-t]:pt-3">{footer}</CardFooter> : null}
    </Card>
  );
}

/** 卡片右上角或页脚的「查看全部」链接 */
export function CardLink({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) {
  return (
    <Link
      href={href}
      className={cn(
        "inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
        className
      )}
    >
      {children}
      <ArrowRight className="size-3" />
    </Link>
  );
}

export function CardEmpty({ icon: Icon, title, description }: { icon: LucideIcon; title: string; description?: string }) {
  return (
    <Empty className="gap-3 p-4 md:p-6">
      <EmptyHeader>
        <EmptyMedia variant="icon" className="mb-1 size-9 text-muted-foreground">
          <Icon className="size-4.5" />
        </EmptyMedia>
        <EmptyTitle className="text-sm">{title}</EmptyTitle>
        {description ? <EmptyDescription className="text-xs">{description}</EmptyDescription> : null}
      </EmptyHeader>
    </Empty>
  );
}

export function formatCount(value: number) {
  if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  return value.toLocaleString("zh-CN");
}

/** 数值变化时从旧值缓动到新值；减少动态效果时直接跳到目标。 */
export function useCountUp(target: number | undefined, disabled: boolean) {
  const [value, setValue] = useState(0);
  const fromRef = useRef(0);

  useEffect(() => {
    if (typeof target !== "number") return;
    const from = fromRef.current;
    if (disabled || from === target) {
      fromRef.current = target;
      const id = requestAnimationFrame(() => setValue(target));
      return () => cancelAnimationFrame(id);
    }
    const started = performance.now();
    let frame = requestAnimationFrame(function step(now) {
      const progress = Math.min(1, (now - started) / 900);
      const eased = 1 - Math.pow(1 - progress, 3);
      setValue(Math.round(from + (target - from) * eased));
      if (progress < 1) frame = requestAnimationFrame(step);
      else fromRef.current = target;
    });
    return () => {
      cancelAnimationFrame(frame);
      fromRef.current = target;
    };
  }, [target, disabled]);

  return typeof target === "number" ? value : undefined;
}

/** 环比：本期相对上期的变化比例；上期为 0 时无法比较 */
export function changeRatio(current: number, previous: number) {
  if (previous <= 0) return current > 0 ? null : 0;
  return (current - previous) / previous;
}

export function formatRatio(ratio: number) {
  const pct = Math.abs(ratio) * 100;
  return `${pct >= 100 ? Math.round(pct) : pct.toFixed(1)}%`;
}

/** YYYY-MM-DD 按日历日解析，不受时区换算影响 */
export function parseDay(date: string) {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1);
}

export function shortDay(date: string) {
  const day = parseDay(date);
  return `${day.getMonth() + 1}/${day.getDate()}`;
}

const WEEKDAYS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

export function longDay(date: string) {
  const day = parseDay(date);
  return `${day.getMonth() + 1}月${day.getDate()}日 ${WEEKDAYS[day.getDay()]}`;
}

/** 迷你折线：只表达走势，不带坐标轴；全为 0 时画一条底线 */
export function Sparkline({ values, className }: { values: number[]; className?: string }) {
  if (values.length < 2) return null;
  const width = 96;
  const height = 28;
  const max = Math.max(...values, 1);
  const step = width / (values.length - 1);
  const points = values.map((value, index) => [index * step, height - 2 - (value / max) * (height - 4)] as const);
  const line = points.map(([x, y], index) => `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const last = points[points.length - 1]!;
  return (
    <svg aria-hidden viewBox={`0 0 ${width} ${height}`} className={cn("h-7 w-24 overflow-visible", className)} preserveAspectRatio="none">
      <path d={`${line} L${width},${height} L0,${height} Z`} className="fill-current opacity-10" />
      <path d={line} className="fill-none stroke-current" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
      <circle cx={last[0]} cy={last[1]} r={2.25} className="fill-current" />
    </svg>
  );
}
