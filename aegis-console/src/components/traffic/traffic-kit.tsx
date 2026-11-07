"use client";

import { useCallback, useState, type KeyboardEvent, type ReactNode } from "react";
import NumberFlow, { type Format } from "@number-flow/react";
import { X, type LucideIcon } from "lucide-react";
import { TonalIcon, type KitTone } from "@/components/apps/detail/app-detail-kit";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { NativePluginState, TrafficClass, TrafficMode } from "@/lib/api/traffic";
import { cn } from "@/lib/utils";
import { CLASS_LABEL, MODE_LABEL, PLUGIN_STATE_LABEL, STREAM, type StreamKey } from "./traffic-format";

/**
 * 流量管理自用的展示件。
 *
 * 面板外壳、数值块、徽标、标签输入都在这里，四个页签与规则编辑页共用一套边距：
 * 面板内容区左右 16 / 20（手机 / 桌面），面板之间 16。各写各的就会出现
 * 「总览的卡片和设置页的卡片差两像素」这种拼起来像两个产品的画面。
 */

/* ── 面板 ── */

export function Panel({
  icon,
  tone = "muted",
  title,
  aside,
  children,
  className,
  bodyClassName,
  id
}: {
  icon?: LucideIcon;
  tone?: KitTone;
  title: ReactNode;
  aside?: ReactNode;
  /** 为空时只有标题栏（可开关的区块关闭时） */
  children?: ReactNode;
  className?: string;
  bodyClassName?: string;
  id?: string;
}) {
  const hasBody = children !== null && children !== undefined && children !== false;
  return (
    <section id={id} className={cn("flex min-w-0 flex-col rounded-2xl border border-border bg-card", className)}>
      <header className={cn("flex min-h-14 min-w-0 flex-wrap items-center gap-x-3 gap-y-2 px-4 pt-4 sm:px-5", hasBody ? "pb-3" : "pb-4")}>
        {icon ? <TonalIcon icon={icon} tone={tone} size="sm" /> : null}
        <h3 className="min-w-0 flex-1 truncate text-sm leading-5 font-semibold tracking-tight">{title}</h3>
        {aside ? <div className="flex shrink-0 flex-wrap items-center gap-1.5">{aside}</div> : null}
      </header>
      {hasBody ? <div className={cn("min-w-0 flex-1 px-4 pb-4 sm:px-5 sm:pb-5", bodyClassName)}>{children}</div> : null}
    </section>
  );
}

/** 页签内容的统一纵向节奏 */
export function TabStack({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("@container flex min-w-0 flex-col gap-4", className)}>{children}</div>;
}

/* ── 数值块 ── */

export function StatTile({
  label,
  value,
  unit,
  stream,
  tone,
  format,
  className,
  children
}: {
  label: string;
  value: number | null | undefined;
  unit?: string;
  stream?: StreamKey;
  tone?: "default" | "warn" | "danger";
  format?: Format;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div className={cn("relative flex min-h-[92px] min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-xl border border-border bg-card p-3 pl-4", className)}>
      <span aria-hidden className={cn("absolute inset-y-3 left-0 w-[3px] rounded-r-full", stream ? STREAM[stream].bg : "bg-foreground/20")} />
      <span className="truncate text-xs text-muted-foreground">{label}</span>
      <div
        className={cn(
          "flex min-w-0 items-baseline gap-1 text-2xl leading-7 font-semibold tracking-tight tabular-nums",
          tone === "warn" && "text-amber-600 dark:text-amber-400",
          tone === "danger" && "text-rose-600 dark:text-rose-400"
        )}
      >
        {typeof value === "number" && Number.isFinite(value) ? (
          <NumberFlow value={value} locales="zh-CN" format={format ?? { notation: "compact", maximumFractionDigits: 1 }} />
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
        {unit ? <span className="text-xs font-normal text-muted-foreground">{unit}</span> : null}
      </div>
      {children}
    </div>
  );
}

/** 小号键值：标签在上、数值在下 */
export function Figure({ label, value, className, valueClassName }: { label: string; value: ReactNode; className?: string; valueClassName?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      <div className="truncate text-[11px] leading-4 text-muted-foreground">{label}</div>
      <div className={cn("truncate text-sm leading-5 font-medium tabular-nums", valueClassName)}>{value}</div>
    </div>
  );
}

/* ── 徽标 ── */

const MODE_BADGE: Record<TrafficMode, "success" | "info" | "outline"> = {
  enforce: "success",
  observe: "info",
  bypass: "outline"
};

export function ModeBadge({ mode, className }: { mode: TrafficMode; className?: string }) {
  return (
    <Badge variant={MODE_BADGE[mode]} size="sm" className={className}>
      {MODE_LABEL[mode]}
    </Badge>
  );
}

const CLASS_TONE: Record<TrafficClass, string> = {
  critical: "border-rose-500/30 text-rose-700 dark:text-rose-300",
  high: "border-amber-500/30 text-amber-700 dark:text-amber-300",
  normal: "border-border text-muted-foreground",
  low: "border-dashed border-border text-muted-foreground"
};

export function ClassBadge({ value, className }: { value: TrafficClass; className?: string }) {
  return (
    <Badge variant="outline" size="sm" className={cn(CLASS_TONE[value], className)}>
      {CLASS_LABEL[value]}
    </Badge>
  );
}

const PLUGIN_BADGE: Record<NativePluginState, "success" | "warning" | "danger" | "outline" | "info"> = {
  running: "success",
  stopped: "outline",
  starting: "info",
  stopping: "warning",
  failed: "danger"
};

export function PluginStateBadge({ state }: { state: NativePluginState }) {
  return (
    <Badge variant={PLUGIN_BADGE[state]} size="sm">
      <span
        aria-hidden
        className={cn(
          "size-1.5 rounded-full",
          state === "running" && "bg-emerald-500",
          state === "failed" && "bg-rose-500",
          (state === "starting" || state === "stopping") && "animate-pulse bg-current",
          state === "stopped" && "bg-muted-foreground"
        )}
      />
      {PLUGIN_STATE_LABEL[state]}
    </Badge>
  );
}

/* ── 切换 ── */

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  className,
  size = "sm",
  ariaLabel
}: {
  value: T;
  options: ReadonlyArray<{ value: T; label: ReactNode }>;
  onChange: (value: T) => void;
  className?: string;
  size?: "sm" | "default";
  ariaLabel?: string;
}) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size={size}
      value={value}
      aria-label={ariaLabel}
      onValueChange={(next) => {
        if (next) onChange(next as T);
      }}
      className={cn("max-w-full flex-wrap", className)}
    >
      {options.map((option) => (
        <ToggleGroupItem key={option.value} value={option.value} className="text-xs">
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

/* ── 设置行：标题 + 当前值 / 控件 ── */

export function SettingRow({
  title,
  value,
  children,
  error,
  stacked,
  className
}: {
  title: string;
  value?: ReactNode;
  children?: ReactNode;
  error?: string;
  /** 控件另起一行（滑块、标签输入） */
  stacked?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex min-h-12 min-w-0 flex-col justify-center gap-2 py-2.5", className)}>
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
        <span className="min-w-0 flex-1 text-sm">{title}</span>
        {value !== undefined ? <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{value}</span> : null}
        {!stacked && children ? <div className="flex shrink-0 items-center gap-2">{children}</div> : null}
      </div>
      {stacked && children ? <div className="min-w-0">{children}</div> : null}
      {error ? <FieldError message={error} /> : null}
    </div>
  );
}

export function FieldError({ message, className }: { message?: string | null; className?: string }) {
  if (!message) return null;
  return <p className={cn("text-xs text-rose-600 dark:text-rose-400", className)}>{message}</p>;
}

/** 表单字段：标签 + 控件 + 错误 */
export function Field({
  label,
  error,
  children,
  className
}: {
  label: string;
  error?: string | null;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <span className="text-xs text-muted-foreground">{label}</span>
      {children}
      <FieldError message={error} />
    </div>
  );
}

/* ── 标签输入 ── */

export function ChipInput({
  value,
  onChange,
  placeholder,
  mono = true,
  invalid,
  normalize,
  ariaLabel,
  className
}: {
  value: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  mono?: boolean;
  /** 校验失败的条目下标 */
  invalid?: ReadonlySet<number>;
  normalize?: (raw: string) => string;
  ariaLabel?: string;
  className?: string;
}) {
  const [text, setText] = useState("");

  const commit = useCallback(
    (raw: string) => {
      const items = raw
        .split(/[\s,，]+/)
        .map((item) => (normalize ? normalize(item.trim()) : item.trim()))
        .filter(Boolean);
      if (!items.length) return;
      const next = [...value];
      for (const item of items) if (!next.includes(item)) next.push(item);
      onChange(next);
      setText("");
    },
    [normalize, onChange, value]
  );

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter" || event.key === "," || event.key === "Tab") {
      if (!text.trim()) return;
      event.preventDefault();
      commit(text);
    } else if (event.key === "Backspace" && !text && value.length) {
      onChange(value.slice(0, -1));
    }
  };

  return (
    <div
      className={cn(
        "flex min-h-9 w-full min-w-0 flex-wrap items-center gap-1.5 rounded-md border border-input bg-transparent px-2 py-1.5 transition-colors focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50 dark:bg-input/30",
        invalid?.size ? "border-rose-500/60" : null,
        className
      )}
    >
      {value.map((item, index) => (
        <span
          key={`${item}-${index}`}
          className={cn(
            "inline-flex max-w-full min-w-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs",
            mono && "font-mono",
            invalid?.has(index) && "bg-rose-500/12 text-rose-700 dark:text-rose-300"
          )}
        >
          <span className="min-w-0 break-all">{item}</span>
          <button
            type="button"
            aria-label={`移除 ${item}`}
            onClick={() => onChange(value.filter((_, i) => i !== index))}
            className="grid size-4 shrink-0 place-items-center rounded text-muted-foreground hover:bg-foreground/10 hover:text-foreground"
          >
            <X className="size-3" />
          </button>
        </span>
      ))}
      <input
        value={text}
        aria-label={ariaLabel}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => commit(text)}
        onPaste={(event) => {
          const pasted = event.clipboardData.getData("text");
          if (/[\s,，]/.test(pasted)) {
            event.preventDefault();
            commit(`${text} ${pasted}`);
          }
        }}
        placeholder={value.length ? undefined : placeholder}
        className={cn("h-6 min-w-24 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground", mono && "font-mono text-xs")}
      />
    </div>
  );
}

/* ── 半环仪表 ── */

export function ArcGauge({
  value,
  max,
  threshold,
  label,
  display,
  sub,
  size = 148
}: {
  value: number;
  max: number;
  /** 告警线（与 value 同单位） */
  threshold?: number;
  label: string;
  display: ReactNode;
  sub?: ReactNode;
  size?: number;
}) {
  const stroke = 12;
  const r = (size - stroke) / 2;
  const cx = size / 2;
  const cy = size / 2;
  const height = size / 2 + stroke;
  const ratio = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
  const limitRatio = threshold && max > 0 ? Math.min(1, threshold / max) : null;
  const point = (t: number) => {
    const angle = Math.PI * (1 - t);
    return [cx + r * Math.cos(angle), cy - r * Math.sin(angle)] as const;
  };
  const [sx, sy] = point(0);
  const [ex, ey] = point(1);
  const [vx, vy] = point(ratio);
  const level = limitRatio === null ? (ratio > 0.9 ? 2 : ratio > 0.7 ? 1 : 0) : ratio >= limitRatio ? 2 : ratio >= limitRatio * 0.8 ? 1 : 0;
  const tick = limitRatio !== null ? [point(limitRatio), limitRatio] as const : null;

  return (
    <figure className="flex min-w-0 flex-col items-center gap-1">
      <div className="relative" style={{ width: size, height }}>
        <svg width={size} height={height} viewBox={`0 0 ${size} ${height}`} aria-hidden className="overflow-visible">
          <path d={`M${sx},${sy} A${r},${r} 0 0 1 ${ex},${ey}`} fill="none" strokeWidth={stroke} strokeLinecap="round" className="stroke-muted" />
          {ratio > 0.001 ? (
            <path
              d={`M${sx},${sy} A${r},${r} 0 0 1 ${vx},${vy}`}
              fill="none"
              strokeWidth={stroke}
              strokeLinecap="round"
              className={cn(
                "transition-[d] duration-700",
                level === 0 && "stroke-emerald-500",
                level === 1 && "stroke-amber-500",
                level === 2 && "stroke-rose-500"
              )}
            />
          ) : null}
          {tick ? (
            <g>
              <line
                x1={cx + (r - stroke) * Math.cos(Math.PI * (1 - tick[1]))}
                y1={cy - (r - stroke) * Math.sin(Math.PI * (1 - tick[1]))}
                x2={cx + (r + stroke) * Math.cos(Math.PI * (1 - tick[1]))}
                y2={cy - (r + stroke) * Math.sin(Math.PI * (1 - tick[1]))}
                strokeWidth={2.5}
                strokeLinecap="round"
                className="stroke-foreground"
              />
            </g>
          ) : null}
        </svg>
        <div className="absolute inset-x-0 bottom-0 flex flex-col items-center">
          <div className="text-xl leading-6 font-semibold tracking-tight tabular-nums">{display}</div>
        </div>
      </div>
      <figcaption className="flex min-w-0 flex-col items-center">
        <span className="text-xs font-medium">{label}</span>
        {sub ? <span className="truncate text-[11px] text-muted-foreground tabular-nums">{sub}</span> : null}
      </figcaption>
    </figure>
  );
}

/* ── 横向占比条 ── */

export function Bar({ ratio, className, trackClassName }: { ratio: number; className?: string; trackClassName?: string }) {
  const pct = Math.min(100, Math.max(0, ratio * 100));
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-muted", trackClassName)}>
      <div className={cn("h-full rounded-full transition-[width] duration-500", className)} style={{ width: `${pct}%` }} />
    </div>
  );
}

/* ── 迷你折线 ── */

export function Spark({ values, className, height = 28, width = 96 }: { values: number[]; className?: string; height?: number; width?: number }) {
  if (values.length < 2) {
    return <svg aria-hidden width={width} height={height} className={cn("shrink-0", className)}><line x1={0} x2={width} y1={height - 1.5} y2={height - 1.5} strokeWidth={1} strokeDasharray="2 3" className="stroke-current opacity-30" /></svg>;
  }
  const max = Math.max(...values, 1);
  const step = width / (values.length - 1);
  const pts = values.map((v, i) => [i * step, height - 2 - (v / max) * (height - 4)] as const);
  const line = pts.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const last = pts[pts.length - 1]!;
  return (
    <svg aria-hidden width={width} height={height} viewBox={`0 0 ${width} ${height}`} className={cn("shrink-0 overflow-visible", className)}>
      <path d={`${line} L${width},${height} L0,${height} Z`} className="fill-current opacity-10" />
      <path d={line} className="fill-none stroke-current" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={last[0]} cy={last[1]} r={2.25} className="fill-current" />
    </svg>
  );
}

/* ── 图例点 ── */

export function Swatch({ stream, label, className }: { stream: StreamKey; label?: string; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs text-muted-foreground", className)}>
      <span aria-hidden className={cn("size-2 rounded-[3px]", STREAM[stream].bg)} />
      {label ?? STREAM[stream].label}
    </span>
  );
}

/* ── 数字输入 ── */

export function NumberField({
  value,
  onChange,
  min,
  max,
  step,
  suffix,
  invalid,
  className,
  ariaLabel
}: {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  suffix?: string;
  invalid?: boolean;
  className?: string;
  ariaLabel?: string;
}) {
  return (
    <div className={cn("relative flex min-w-0 items-center", className)}>
      <Input
        type="number"
        inputMode="numeric"
        aria-label={ariaLabel}
        aria-invalid={invalid || undefined}
        value={Number.isFinite(value) ? value : ""}
        min={min}
        max={max}
        step={step}
        onChange={(event) => {
          const raw = event.target.value;
          const next = raw === "" ? 0 : Number(raw);
          if (Number.isFinite(next)) onChange(next);
        }}
        className={cn("h-8 text-right tabular-nums", suffix && "pr-10")}
      />
      {suffix ? <span className="pointer-events-none absolute right-2.5 text-xs text-muted-foreground">{suffix}</span> : null}
    </div>
  );
}
