"use client";

import { useRef, useState } from "react";
import { Diamond, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { composeDesignColor, designColorToCss, isDesignColor, parseDesignColor, rgbHexOf } from "../renderer/color";

/**
 * 属性检查器的输入部件。数值框带范围（超出时夹回）、拖动标签即可连续调值；颜色框是设计稿口径
 * （#RRGGBB / #AARRGGBB，alpha 在前），带透明度滑杆。
 */

/** 透明底的棋盘格：内联 SVG 平铺（不用 CSS 渐变） */
export const CHECKER_BACKGROUND = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12'%3E%3Crect width='12' height='12' fill='%23ffffff'/%3E%3Crect width='6' height='6' fill='%23d4d4d8'/%3E%3Crect x='6' y='6' width='6' height='6' fill='%23d4d4d8'/%3E%3C/svg%3E")`;

export function FieldRow({ label, children, className }: { label: React.ReactNode; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("grid grid-cols-[64px_minmax(0,1fr)] items-center gap-2", className)}>
      <span className="truncate text-xs text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function FieldGroup({ title, children, actions }: { title: string; children: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2.5 border-b px-3 py-3 last:border-b-0">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-semibold tracking-wide text-muted-foreground">{title}</span>
        {actions}
      </div>
      {children}
    </div>
  );
}

function roundTo(value: number, step: number) {
  const digits = step >= 1 ? 0 : Math.min(4, Math.ceil(-Math.log10(step)));
  return Number(value.toFixed(digits));
}

/**
 * 数值框。输入中保留原文（允许打出「-」「0.」这类中间态），失焦或回车时按范围夹回；
 * 按住标签左右拖动连续调值，按住 Shift 十倍步长。
 */
export function NumberField({
  label,
  value,
  onChange,
  min,
  max,
  step = 1,
  suffix,
  disabled,
  keyframe,
  className
}: {
  label?: React.ReactNode;
  value: number | undefined;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  suffix?: string;
  disabled?: boolean;
  /** 可动画属性旁的关键帧按钮 */
  keyframe?: { state: "none" | "track" | "here"; onClick: () => void };
  className?: string;
}) {
  const [text, setText] = useState<string | null>(null);
  const drag = useRef<{ x: number; start: number } | null>(null);
  const clamp = (v: number) => Math.max(min ?? -Infinity, Math.min(max ?? Infinity, v));
  const shown = text ?? (value === undefined || Number.isNaN(value) ? "" : String(roundTo(value, step)));

  const commit = (raw: string) => {
    const parsed = Number(raw);
    if (raw.trim() !== "" && Number.isFinite(parsed)) onChange(roundTo(clamp(parsed), step));
    setText(null);
  };

  return (
    <div className={cn("flex min-w-0 items-center gap-1", className)}>
      <div
        className={cn(
          "flex h-7 min-w-0 flex-1 items-center rounded-md border bg-background text-xs focus-within:ring-2 focus-within:ring-ring/40",
          disabled && "opacity-50"
        )}
      >
        {label !== undefined ? (
          <span
            className="flex h-full shrink-0 cursor-ew-resize items-center pr-1 pl-2 text-[11px] text-muted-foreground select-none"
            onPointerDown={(event) => {
              if (disabled) return;
              event.currentTarget.setPointerCapture(event.pointerId);
              drag.current = { x: event.clientX, start: value ?? 0 };
            }}
            onPointerMove={(event) => {
              if (!drag.current) return;
              const delta = (event.clientX - drag.current.x) * step * (event.shiftKey ? 10 : 1);
              onChange(roundTo(clamp(drag.current.start + delta), step));
            }}
            onPointerUp={() => {
              drag.current = null;
            }}
          >
            {label}
          </span>
        ) : null}
        <input
          value={shown}
          disabled={disabled}
          inputMode="decimal"
          onChange={(event) => setText(event.target.value)}
          onBlur={(event) => commit(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit((event.target as HTMLInputElement).value);
            if (event.key === "ArrowUp" || event.key === "ArrowDown") {
              event.preventDefault();
              const delta = (event.key === "ArrowUp" ? 1 : -1) * step * (event.shiftKey ? 10 : 1);
              onChange(roundTo(clamp((value ?? 0) + delta), step));
              setText(null);
            }
            if (event.key === "Escape") setText(null);
          }}
          className="h-full w-full min-w-0 bg-transparent px-1.5 tabular-nums outline-none"
        />
        {suffix ? <span className="shrink-0 pr-2 text-[11px] text-muted-foreground">{suffix}</span> : null}
      </div>
      {keyframe ? (
        <button
          type="button"
          onClick={keyframe.onClick}
          disabled={disabled}
          title={keyframe.state === "here" ? "更新此刻的关键帧" : "在此刻打关键帧"}
          className={cn(
            "inline-flex size-6 shrink-0 items-center justify-center rounded transition-colors hover:bg-accent",
            keyframe.state === "none" ? "text-muted-foreground/60" : "text-amber-500"
          )}
        >
          <Diamond className={cn("size-3", keyframe.state === "here" && "fill-current")} />
        </button>
      ) : null}
    </div>
  );
}

const SWATCHES = [
  "#FFFFFF",
  "#000000",
  "#FFD86B",
  "#F59E0B",
  "#EF4444",
  "#EC4899",
  "#8B5CF6",
  "#3B82F6",
  "#22D3EE",
  "#10B981",
  "#A3E635",
  "#94A3B8"
];

/** 颜色框：色块 + 值；弹层里是原生取色器、透明度、十六进制与常用色 */
export function ColorField({
  value,
  onChange,
  allowEmpty,
  disabled,
  placeholder = "无"
}: {
  value: string | undefined;
  onChange: (value: string | undefined) => void;
  allowEmpty?: boolean;
  disabled?: boolean;
  placeholder?: string;
}) {
  const parsed = parseDesignColor(value);
  const alpha = parsed?.a ?? 1;
  const [hex, setHex] = useState<string | null>(null);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          className="flex h-7 w-full min-w-0 items-center gap-2 rounded-md border bg-background px-1.5 text-left text-xs disabled:opacity-50"
        >
          <span className="size-4 shrink-0 overflow-hidden rounded-sm border" style={{ backgroundImage: CHECKER_BACKGROUND }}>
            <span className="block size-full" style={{ backgroundColor: designColorToCss(value) }} />
          </span>
          <span className={cn("truncate font-mono", !value && "text-muted-foreground")}>{value || placeholder}</span>
          {parsed && alpha < 1 ? <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">{Math.round(alpha * 100)}%</span> : null}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-3" align="start">
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <input
              type="color"
              value={rgbHexOf(value ?? "#FFFFFF")}
              onChange={(event) => onChange(composeDesignColor(event.target.value, alpha))}
              className="h-9 w-12 shrink-0 cursor-pointer rounded border bg-transparent p-0.5"
              aria-label="取色"
            />
            <Input
              value={hex ?? value ?? ""}
              placeholder="#AARRGGBB"
              className="h-9 font-mono text-xs"
              onChange={(event) => {
                const next = event.target.value.trim().toUpperCase();
                setHex(next);
                if (isDesignColor(next)) onChange(next);
              }}
              onBlur={() => setHex(null)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>不透明度</span>
              <span className="tabular-nums">{Math.round(alpha * 100)}%</span>
            </div>
            <Slider
              value={[Math.round(alpha * 100)]}
              min={0}
              max={100}
              step={1}
              onValueChange={([next]) => onChange(composeDesignColor(rgbHexOf(value ?? "#FFFFFF"), next / 100))}
            />
          </div>
          <div className="grid grid-cols-6 gap-1.5">
            {SWATCHES.map((swatch) => (
              <button
                key={swatch}
                type="button"
                onClick={() => onChange(composeDesignColor(swatch, alpha))}
                className="h-6 rounded border transition-transform hover:scale-110"
                style={{ backgroundColor: swatch }}
                aria-label={swatch}
              />
            ))}
          </div>
          {allowEmpty ? (
            <Button size="sm" variant="outline" onClick={() => onChange(undefined)}>
              <X />
              清除
            </Button>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function SelectField<T extends string>({
  value,
  onChange,
  options,
  disabled
}: {
  value: T | undefined;
  onChange: (value: T) => void;
  options: Array<{ value: T; label: React.ReactNode }>;
  disabled?: boolean;
}) {
  return (
    <Select value={value} onValueChange={(next) => onChange(next as T)} disabled={disabled}>
      <SelectTrigger size="sm" className="h-7 w-full text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value} className="text-xs">
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function SwitchField({ checked, onChange, disabled }: { checked: boolean; onChange: (value: boolean) => void; disabled?: boolean }) {
  return <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} className="scale-90" />;
}

export function TextField({
  value,
  onChange,
  maxLength,
  placeholder,
  disabled,
  mono
}: {
  value: string;
  onChange: (value: string) => void;
  maxLength?: number;
  placeholder?: string;
  disabled?: boolean;
  mono?: boolean;
}) {
  return (
    <Input
      value={value}
      maxLength={maxLength}
      placeholder={placeholder}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
      className={cn("h-7 text-xs", mono && "font-mono")}
    />
  );
}

/** 一组颜色（粒子颜色、循环变色），可增删 */
export function ColorListField({
  values,
  onChange,
  max,
  disabled
}: {
  values: string[];
  onChange: (values: string[]) => void;
  max: number;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      {values.map((color, index) => (
        <div key={index} className="flex items-center gap-1">
          <div className="min-w-0 flex-1">
            <ColorField
              value={color}
              disabled={disabled}
              onChange={(next) => onChange(values.map((item, i) => (i === index ? (next ?? item) : item)))}
            />
          </div>
          <Button
            size="icon-xs"
            variant="ghost"
            disabled={disabled}
            onClick={() => onChange(values.filter((_, i) => i !== index))}
            aria-label="移除颜色"
          >
            <X />
          </Button>
        </div>
      ))}
      {values.length < max ? (
        <Button
          size="xs"
          variant="outline"
          className="border-dashed"
          disabled={disabled}
          onClick={() => onChange([...values, values[values.length - 1] ?? "#FFFFFF"])}
        >
          添加颜色
        </Button>
      ) : null}
    </div>
  );
}

/** 九宫格锚点选择 */
export function AnchorPicker({
  x,
  y,
  onChange,
  disabled
}: {
  x: number;
  y: number;
  onChange: (x: number, y: number) => void;
  disabled?: boolean;
}) {
  const points = [0, 0.5, 1];
  return (
    <div className="grid size-[52px] shrink-0 grid-cols-3 gap-0.5 rounded-md border p-1">
      {points.map((py) =>
        points.map((px) => {
          const active = Math.abs(px - x) < 0.01 && Math.abs(py - y) < 0.01;
          return (
            <button
              key={`${px}-${py}`}
              type="button"
              disabled={disabled}
              onClick={() => onChange(px, py)}
              className={cn("rounded-sm transition-colors", active ? "bg-primary" : "bg-muted hover:bg-muted-foreground/30")}
              aria-label={`锚点 ${px} ${py}`}
            />
          );
        })
      )}
    </div>
  );
}
