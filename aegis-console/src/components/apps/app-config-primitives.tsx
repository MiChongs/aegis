"use client";

import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroupItem } from "@/components/ui/radio-group";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";

/**
 * 应用配置面板的共享字段原语。
 *
 * 这些组件原先私有在 app-policy-panel.tsx 里，配置页拆分后被多个面板复用。
 * 抽出来的目的不只是省代码：配置项的视觉语言（开关行、滑块、分组标题、
 * 生效状态点）必须在所有配置面板里完全一致，否则同一种控件在两个 Tab 里
 * 长得不一样，管理员会以为它们的语义也不同。
 */

/** 一张配置卡：图标 + 标题 + 说明 + 右上角状态 */
export function SectionCard({
  icon,
  title,
  description,
  aside,
  footer,
  children
}: {
  icon: React.ReactNode;
  title: string;
  description?: string;
  aside?: React.ReactNode;
  footer?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section
      className="overflow-hidden rounded-2xl border border-border bg-card"
      style={{ boxShadow: "var(--shadow-soft)" }}
    >
      <header className="flex flex-col gap-2 border-b border-border px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-start gap-3">
          <div className="grid size-9 shrink-0 place-items-center rounded-xl bg-emerald-500/10 text-emerald-600 ring-1 ring-inset ring-emerald-500/20 dark:text-emerald-400">
            {icon}
          </div>
          <div className="space-y-0.5">
            <h3 className="text-sm font-semibold tracking-tight">{title}</h3>
            {description && <p className="text-xs text-muted-foreground">{description}</p>}
          </div>
        </div>
        {aside && <div className="shrink-0">{aside}</div>}
      </header>
      <div className="p-5">{children}</div>
      {footer && <footer className="border-t border-border bg-muted/30 px-5 py-3">{footer}</footer>}
    </section>
  );
}

/** 卡内字段分组：小标题 + 右侧提示 */
export function FieldGroup({
  label,
  hint,
  children
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</Label>
        {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
      </div>
      {children}
    </div>
  );
}

/** 开关行。整行可点击，开启态用主色描边强调「这条已生效」 */
export function SwitchRow({
  label,
  hint,
  icon,
  checked,
  onChange,
  disabled
}: {
  label: string;
  hint?: string;
  icon?: React.ReactNode;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5 transition-colors duration-150",
        "hover:bg-muted-foreground/10",
        checked && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30 hover:bg-emerald-500/15",
        disabled && "pointer-events-none opacity-50"
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        {icon && (
          <span className={cn("text-muted-foreground", checked && "text-emerald-600 dark:text-emerald-400")}>
            {icon}
          </span>
        )}
        <div className="min-w-0 space-y-0.5">
          <div className="truncate text-xs font-medium">{label}</div>
          {hint && <div className="text-[10px] leading-snug text-muted-foreground">{hint}</div>}
        </div>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} />
    </label>
  );
}

/**
 * 滑块字段。
 *
 * `valueLabel` 用于把数值替换成语义文案（例如 0 显示「永不过期」而不是「0 天」）——
 * 数字 0 在这类配置里几乎总有特殊含义，直接显示 "0 天" 会被读成「立刻过期」。
 */
export function SliderField({
  label,
  value,
  min,
  max,
  step = 1,
  unit,
  disabled,
  valueLabel,
  hint,
  onChange
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  unit?: string;
  disabled?: boolean;
  valueLabel?: string;
  hint?: string;
  onChange: (v: number) => void;
}) {
  return (
    <div className={cn("space-y-2 rounded-xl bg-muted px-3 py-2.5", disabled && "opacity-50")}>
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        <span className="font-mono text-xs tabular-nums text-foreground">
          {valueLabel ?? `${value}${unit ? ` ${unit}` : ""}`}
        </span>
      </div>
      <Slider
        value={[value]}
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        onValueChange={(v) => onChange(v[0] ?? min)}
      />
      <div className="flex justify-between text-[10px] text-muted-foreground">
        <span>{min}</span>
        {hint ? <span className="truncate px-2 text-center">{hint}</span> : null}
        <span>{max}</span>
      </div>
    </div>
  );
}

/**
 * 数值输入字段。用于取值范围大、滑块拖不准的配置（例如换绑冷却秒数、积分兑换率）。
 * 受控值保持字符串，避免输入过程中的空串被 Number() 变成 0 又写回输入框。
 */
export function NumberField({
  label,
  value,
  onChange,
  min,
  max,
  unit,
  hint,
  placeholder,
  disabled
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  min?: number;
  max?: number;
  unit?: string;
  hint?: string;
  placeholder?: string;
  disabled?: boolean;
}) {
  return (
    <div className={cn("space-y-1.5", disabled && "opacity-50")}>
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[11px] font-medium">{label}</Label>
        {unit && <span className="text-[10px] text-muted-foreground">{unit}</span>}
      </div>
      <Input
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className="font-mono text-xs tabular-nums"
      />
      {hint && <p className="text-[10px] leading-snug text-muted-foreground">{hint}</p>}
    </div>
  );
}

/** 单选卡（配合 RadioGroup 使用） */
export function ModeCard({
  value,
  active,
  title,
  description,
  icon,
  disabled
}: {
  value: string;
  active: boolean;
  title: string;
  description: string;
  icon: React.ReactNode;
  disabled?: boolean;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-xl bg-muted p-3 transition-colors duration-150",
        "hover:bg-muted-foreground/10",
        active && "bg-emerald-500/10 ring-1 ring-inset ring-emerald-500/30 hover:bg-emerald-500/15",
        disabled && "pointer-events-none opacity-50"
      )}
    >
      <RadioGroupItem
        value={value}
        disabled={disabled}
        className={cn("mt-0.5", active && "border-emerald-500 text-emerald-500 data-[state=checked]:border-emerald-500")}
      />
      <div className="flex-1 space-y-0.5">
        <div className="flex items-center gap-1.5 text-sm font-medium">
          <span className={cn("text-muted-foreground", active && "text-emerald-600 dark:text-emerald-400")}>{icon}</span>
          {title}
        </div>
        <p className="text-[11px] text-muted-foreground">{description}</p>
      </div>
    </label>
  );
}

/** 生效状态点（带脉冲动画表示「正在生效」） */
export function StatusDot({
  active,
  labelActive,
  labelInactive
}: {
  active: boolean;
  labelActive: string;
  labelInactive: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 text-xs font-medium leading-none",
        active ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground"
      )}
    >
      <span className="relative flex size-2 items-center justify-center">
        {active && <span className="absolute inset-0 animate-ping rounded-full bg-emerald-500/40" />}
        <span className={cn("relative size-2 rounded-full", active ? "bg-emerald-500" : "bg-muted-foreground/50")} />
      </span>
      <span>{active ? labelActive : labelInactive}</span>
    </span>
  );
}

/** 面板级空态：未选择应用时的统一提示 */
export function NoAppSelected({ icon, hint = "请先选择应用" }: { icon: React.ReactNode; hint?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <span className="text-muted-foreground/50">{icon}</span>
      <div className="text-sm text-muted-foreground">{hint}</div>
    </div>
  );
}
