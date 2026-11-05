"use client";

import { useId, useState } from "react";
import Link from "next/link";
import { Check, Copy, type LucideIcon } from "lucide-react";
import { toast } from "sonner";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/**
 * 应用详情页自用的展示原语。
 *
 * 头部名片、区块导航与「基本信息」面板共用一套外壳与色调：同一页里卡片的圆角、
 * 标题栏高度、图标底色如果各写各的，几块拼在一起就像三个页面。
 * 只放展示件，不碰数据；面板里的业务逻辑留在各自文件。
 */

/* ── 色调 ── */

export type KitTone = "primary" | "emerald" | "amber" | "sky" | "violet" | "rose" | "muted";

const TONE_PLATE: Record<KitTone, string> = {
  primary: "bg-primary/10 text-primary",
  emerald: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400",
  amber: "bg-amber-500/12 text-amber-600 dark:text-amber-400",
  sky: "bg-sky-500/12 text-sky-600 dark:text-sky-400",
  violet: "bg-violet-500/12 text-violet-600 dark:text-violet-400",
  rose: "bg-rose-500/12 text-rose-600 dark:text-rose-400",
  muted: "bg-muted text-muted-foreground"
};

const TONE_STROKE: Record<KitTone, string> = {
  primary: "text-primary",
  emerald: "text-emerald-500",
  amber: "text-amber-500",
  sky: "text-sky-500",
  violet: "text-violet-500",
  rose: "text-rose-500",
  muted: "text-muted-foreground"
};

export function TonalIcon({
  icon: Icon,
  tone = "muted",
  size = "default",
  className
}: {
  icon: LucideIcon;
  tone?: KitTone;
  size?: "sm" | "default" | "lg";
  className?: string;
}) {
  return (
    <span
      aria-hidden
      className={cn(
        "grid shrink-0 place-items-center",
        size === "sm" && "size-7 rounded-lg",
        size === "default" && "size-8 rounded-lg",
        size === "lg" && "size-10 rounded-xl",
        TONE_PLATE[tone],
        className
      )}
    >
      <Icon className={size === "lg" ? "size-5" : size === "sm" ? "size-3.5" : "size-4"} />
    </span>
  );
}

/* ── 面板外壳 ── */

export function DetailPanel({
  icon,
  tone = "muted",
  title,
  description,
  aside,
  footer,
  children,
  className,
  bodyClassName,
  id
}: {
  icon: LucideIcon;
  tone?: KitTone;
  title: string;
  description?: React.ReactNode;
  aside?: React.ReactNode;
  footer?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bodyClassName?: string;
  id?: string;
}) {
  return (
    <section
      id={id}
      className={cn("flex min-w-0 flex-col overflow-hidden rounded-2xl border border-border bg-card", className)}
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <header className="flex min-w-0 items-center gap-3 px-4 pt-4 pb-3 sm:px-5">
        <TonalIcon icon={icon} tone={tone} />
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-sm leading-5 font-semibold tracking-tight">{title}</h3>
          {description && <p className="truncate text-[11px] leading-4 text-muted-foreground">{description}</p>}
        </div>
        {aside && <div className="flex shrink-0 items-center gap-1.5">{aside}</div>}
      </header>
      <div className={cn("min-w-0 flex-1 px-4 pb-4 sm:px-5 sm:pb-5", bodyClassName)}>{children}</div>
      {footer}
    </section>
  );
}

/** 面板右上角的小链接（「查看全部 →」之类） */
export function PanelLink({ children, ...props }: React.ComponentProps<typeof Link>) {
  return (
    <Link
      scroll={false}
      {...props}
      className={cn(
        "inline-flex h-7 items-center gap-1 rounded-lg px-2 text-[11px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
        props.className
      )}
    >
      {children}
    </Link>
  );
}

/* ── 数值 ── */

export function Metric({
  label,
  value,
  hint,
  icon,
  tone = "muted",
  loading,
  emphasis,
  className,
  children
}: {
  label: string;
  value: React.ReactNode;
  hint?: React.ReactNode;
  icon?: LucideIcon;
  tone?: KitTone;
  loading?: boolean;
  /** 数值着色：异常值（失败数）用 */
  emphasis?: "warn" | "danger";
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-2 rounded-xl border border-border/80 bg-background/60 p-3", className)}>
      <div className="flex min-w-0 items-center justify-between gap-2">
        <span className="truncate text-[11px] text-muted-foreground">{label}</span>
        {icon && <TonalIcon icon={icon} tone={tone} size="sm" />}
      </div>
      {loading ? (
        <Skeleton className="h-7 w-20 rounded" />
      ) : (
        <div
          className={cn(
            "truncate text-2xl leading-7 font-semibold tracking-tight tabular-nums",
            emphasis === "warn" && "text-amber-600 dark:text-amber-400",
            emphasis === "danger" && "text-red-600 dark:text-red-400"
          )}
        >
          {value}
        </div>
      )}
      {children}
      {hint && <div className="truncate text-[11px] text-muted-foreground">{hint}</div>}
    </div>
  );
}

/** 横向分段条：启用 / 停用、密码 / 第三方这类构成比 */
export function Meter({
  segments,
  className
}: {
  segments: Array<{ value: number; className: string; label: string }>;
  className?: string;
}) {
  const total = segments.reduce((sum, item) => sum + Math.max(0, item.value), 0);
  return (
    <div className={cn("flex h-1.5 w-full gap-0.5 overflow-hidden rounded-full bg-muted", className)}>
      {total > 0 &&
        segments.map((item) =>
          item.value > 0 ? (
            <span
              key={item.label}
              title={`${item.label} ${Math.round((item.value / total) * 1000) / 10}%`}
              className={cn("h-full rounded-full transition-[width] duration-500", item.className)}
              style={{ width: `${(item.value / total) * 100}%` }}
            />
          ) : null
        )}
    </div>
  );
}

/** 环形百分比。value 为 null 时只画底环（没有数据不等于 0%） */
export function RingGauge({
  value,
  tone = "emerald",
  size = 44,
  stroke = 5,
  children
}: {
  value: number | null;
  tone?: KitTone;
  size?: number;
  stroke?: number;
  children?: React.ReactNode;
}) {
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const pct = value === null ? 0 : Math.min(100, Math.max(0, value));
  return (
    <span className="relative inline-grid shrink-0 place-items-center" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90" aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={stroke} className="stroke-muted" />
        {value !== null && (
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeDasharray={`${(pct / 100) * c} ${c}`}
            className={cn("stroke-current transition-[stroke-dasharray] duration-700", TONE_STROKE[tone])}
          />
        )}
      </svg>
      {children && <span className="absolute inset-0 grid place-items-center">{children}</span>}
    </span>
  );
}

/**
 * 迷你柱状趋势。不用图表库：这里只要形状，不要坐标轴与提示框；
 * 悬停某一天在下方说明里显示日期与数值，不弹浮层。
 */
export function TrendBars({
  data,
  tone = "primary",
  loading,
  className
}: {
  data: Array<{ label: string; value: number }>;
  tone?: KitTone;
  loading?: boolean;
  className?: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(1, ...data.map((item) => item.value));
  const focus = hover === null ? data.length - 1 : hover;
  const current = data[focus];
  const titleId = useId();

  if (loading) return <Skeleton className={cn("h-24 w-full rounded-xl", className)} />;
  if (data.length === 0) {
    return (
      <div className={cn("grid h-24 place-items-center rounded-xl bg-muted/40 text-[11px] text-muted-foreground", className)}>
        暂无趋势数据
      </div>
    );
  }

  return (
    <figure className={cn("min-w-0", className)} aria-labelledby={titleId}>
      <div className="flex h-20 items-end gap-[3px]" onMouseLeave={() => setHover(null)}>
        {data.map((item, index) => (
          <button
            key={item.label}
            type="button"
            tabIndex={-1}
            aria-label={`${item.label} ${item.value}`}
            onMouseEnter={() => setHover(index)}
            onFocus={() => setHover(index)}
            className="group flex h-full min-w-0 flex-1 items-end"
          >
            <span
              className={cn(
                "w-full rounded-t-[3px] rounded-b-[1px] transition-[height,opacity] duration-300",
                TONE_STROKE[tone],
                "bg-current",
                index === focus ? "opacity-100" : "opacity-30 group-hover:opacity-60"
              )}
              style={{ height: `${Math.max(4, (item.value / max) * 100)}%` }}
            />
          </button>
        ))}
      </div>
      <figcaption id={titleId} className="mt-2 flex items-center justify-between text-[11px] text-muted-foreground">
        <span className="tabular-nums">{current?.label}</span>
        <span className="font-medium text-foreground tabular-nums">{current ? `${current.value.toLocaleString("zh-CN")} 人` : "—"}</span>
      </figcaption>
    </figure>
  );
}

/* ── 标识 ── */

export function useCopy() {
  const [copied, setCopied] = useState<string | null>(null);
  const copy = (value: string, label: string) => {
    void navigator.clipboard.writeText(value).then(
      () => {
        setCopied(value);
        toast.success(`${label}已复制`);
        window.setTimeout(() => setCopied((prev) => (prev === value ? null : prev)), 1600);
      },
      () => toast.error("复制失败，请手动选择")
    );
  };
  return { copied, copy };
}

/** 一行可复制的值。长值完整显示并允许任意处换行：截断的密钥复制给别人也没用 */
export function CopyField({
  label,
  value,
  mono = true,
  placeholder = "—",
  className
}: {
  label: string;
  value?: string | null;
  mono?: boolean;
  placeholder?: string;
  className?: string;
}) {
  const { copied, copy } = useCopy();
  const done = Boolean(value) && copied === value;
  return (
    <div className={cn("min-w-0", className)}>
      <div className="mb-1 text-[11px] text-muted-foreground">{label}</div>
      <div className="flex min-w-0 items-stretch gap-1.5">
        <div
          className={cn(
            "min-w-0 flex-1 rounded-lg border border-border/70 bg-muted/40 px-2.5 py-1.5 text-xs leading-relaxed break-all",
            mono && "font-mono",
            !value && "text-muted-foreground"
          )}
        >
          {value || placeholder}
        </div>
        <button
          type="button"
          aria-label={`复制${label}`}
          disabled={!value}
          onClick={() => value && copy(value, label)}
          className={cn(
            "grid w-8 shrink-0 place-items-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-40",
            done && "border-emerald-500/40 text-emerald-600 dark:text-emerald-400"
          )}
        >
          {done ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
        </button>
      </div>
    </div>
  );
}

export function Fact({
  label,
  value,
  mono,
  hint,
  className
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
  hint?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0", className)}>
      <dt className="truncate text-[11px] text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 truncate text-xs font-medium", mono && "font-mono")}>{value}</dd>
      {hint && <dd className="truncate text-[10px] text-muted-foreground">{hint}</dd>}
    </div>
  );
}

/** 小号状态点 + 文字 */
export function StatusDot({ on, onLabel = "开启", offLabel = "关闭" }: { on: boolean; onLabel?: string; offLabel?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 text-[11px] font-medium",
        on ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400"
      )}
    >
      <span className={cn("size-1.5 rounded-full", on ? "bg-emerald-500" : "bg-amber-500")} />
      {on ? onLabel : offLabel}
    </span>
  );
}
