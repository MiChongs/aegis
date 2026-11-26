"use client";

import { useState } from "react";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { DURATION_PRESETS, type BanModes } from "./ban-format";

/** 封禁表单的三个字段：响应模式、时长、tarpit 延迟。批量封禁、单条修改、批量修改共用。 */

const DEFAULT_VALUE = "__default__";
const KEEP = "__keep__";
const CUSTOM = "__custom__";

export function ModeSelect({
  value,
  onChange,
  modes,
  id,
  className
}: {
  /** "" = 沿用全局默认 */
  value: string;
  onChange: (value: string) => void;
  modes: BanModes;
  id?: string;
  className?: string;
}) {
  const description = modes.describe(value);
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <Select value={value || DEFAULT_VALUE} onValueChange={(next) => onChange(next === DEFAULT_VALUE ? "" : next)}>
        <SelectTrigger id={id} className="w-full min-w-0" aria-label="响应模式">
          <SelectValue placeholder={modes.loading ? "读取中…" : "选择响应模式"} />
        </SelectTrigger>
        <SelectContent className="max-h-80">
          <SelectItem value={DEFAULT_VALUE}>{modes.label("")}</SelectItem>
          {modes.modes.map((mode) => (
            <SelectItem key={mode.value} value={mode.value}>
              {mode.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {description ? <p className="text-[11px] leading-4 text-muted-foreground">{description}</p> : null}
    </div>
  );
}

const UNITS = [
  { value: "60", label: "分钟" },
  { value: "3600", label: "小时" },
  { value: "86400", label: "天" }
] as const;

function splitSeconds(seconds: number): { amount: string; unit: string } {
  if (seconds > 0 && seconds % 86_400 === 0) return { amount: String(seconds / 86_400), unit: "86400" };
  if (seconds > 0 && seconds % 3600 === 0) return { amount: String(seconds / 3600), unit: "3600" };
  if (seconds > 0) return { amount: String(Math.max(1, Math.round(seconds / 60))), unit: "60" };
  return { amount: "", unit: "3600" };
}

/**
 * 时长：预设一键 + 自定义（数值 + 单位）。`value` 为秒，0 = 永久；
 * 给了 `keepLabel` 时多一个「保持不变」，对应 `value = null`。
 */
export function DurationPicker({
  value,
  onChange,
  keepLabel
}: {
  value: number | null;
  onChange: (value: number | null) => void;
  keepLabel?: string;
}) {
  const isPreset = value === null || DURATION_PRESETS.some((item) => item.value === value);
  const [custom, setCustom] = useState(!isPreset);
  const [draft, setDraft] = useState(() => splitSeconds(value ?? 0));

  const selected = custom ? CUSTOM : value === null ? KEEP : String(value);

  const applyDraft = (next: { amount: string; unit: string }) => {
    setDraft(next);
    const amount = Number(next.amount);
    if (Number.isFinite(amount) && amount > 0) onChange(Math.round(amount * Number(next.unit)));
  };

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        value={selected}
        aria-label="时长"
        className="max-w-full flex-wrap"
        onValueChange={(next) => {
          if (!next) return;
          if (next === CUSTOM) {
            setCustom(true);
            applyDraft(draft.amount ? draft : { amount: "12", unit: "3600" });
            return;
          }
          setCustom(false);
          onChange(next === KEEP ? null : Number(next));
        }}
      >
        {keepLabel ? (
          <ToggleGroupItem value={KEEP} className="px-2.5 text-xs">
            {keepLabel}
          </ToggleGroupItem>
        ) : null}
        {DURATION_PRESETS.map((item) => (
          <ToggleGroupItem key={item.value} value={String(item.value)} className="px-2.5 text-xs">
            {item.label}
          </ToggleGroupItem>
        ))}
        <ToggleGroupItem value={CUSTOM} className="px-2.5 text-xs">
          自定义
        </ToggleGroupItem>
      </ToggleGroup>
      {custom ? (
        <div className="flex items-center gap-2">
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            value={draft.amount}
            onChange={(event) => applyDraft({ ...draft, amount: event.target.value })}
            className="h-8 w-28 tabular-nums"
            aria-label="时长数值"
          />
          <Select value={draft.unit} onValueChange={(unit) => applyDraft({ ...draft, unit })}>
            <SelectTrigger size="sm" className="w-24" aria-label="时长单位">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {UNITS.map((unit) => (
                <SelectItem key={unit.value} value={unit.value}>
                  {unit.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-xs text-muted-foreground">从现在起</span>
        </div>
      ) : null}
    </div>
  );
}

/** tarpit 延迟（毫秒，0 = 沿用防火墙配置，上限 30 秒） */
export function TarpitField({ value, onChange, id }: { value: number; onChange: (value: number) => void; id?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <Input
          id={id}
          type="number"
          inputMode="numeric"
          min={0}
          max={30_000}
          step={500}
          value={value || ""}
          placeholder="沿用配置"
          onChange={(event) => onChange(Math.max(0, Math.min(30_000, Math.round(Number(event.target.value) || 0))))}
          className="h-8 w-32 tabular-nums"
          aria-label="拖延毫秒"
        />
        <span className="text-xs text-muted-foreground">毫秒{value > 0 ? `（${(value / 1000).toFixed(value % 1000 ? 1 : 0)} 秒）` : ""}</span>
      </div>
      <p className="text-[11px] leading-4 text-muted-foreground">每个请求先等这么久再返回 403，最长 30 秒；留空沿用防火墙配置。</p>
    </div>
  );
}

/** 表单里的一组：标签 + 控件 */
export function FormBlock({ label, hint, htmlFor, children, className }: { label: string; hint?: string; htmlFor?: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <div className="flex min-w-0 items-baseline justify-between gap-2">
        <label htmlFor={htmlFor} className="text-xs font-medium">
          {label}
        </label>
        {hint ? <span className="truncate text-[11px] text-muted-foreground">{hint}</span> : null}
      </div>
      {children}
    </div>
  );
}
