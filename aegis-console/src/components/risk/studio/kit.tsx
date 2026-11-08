"use client";

import { useState, type ReactNode } from "react";
import { AlertTriangle, Check, CircleAlert, CircleCheck, Info, Plus, RotateCw, Trash2, X, type LucideIcon } from "lucide-react";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ACTION_LABEL } from "@/components/risk/kit/labels";
import { LEVEL_WORD } from "@/components/risk/kit/humanize";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import type { CompileIssue, CondOp, Condition, RiskAction, Rule, ScoreBand } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

/**
 * 场景工作台的公用件与工作台特有的说法。
 *
 * 前半部分（errorText、keyError、MultiPick、AppMultiSelect、ConditionRows）被特征、模型、行为模式、场景列表等页面共用，签名不动。
 * 后半部分是工作台的版式件：分节卡片、提示条、选项卡片、空 / 错 / 载入块、分数段区间条、比例环、二次确认。
 */

export function errorText(error: unknown, fallback = "操作失败") {
  return error instanceof Error && error.message ? error.message : fallback;
}

export const KEY_PATTERN = /^[a-z][a-z0-9_]*$/;

/** 键名（场景、特征、规则、模型、模式）的统一校验文案 */
export function keyError(key: string): string | null {
  if (!key) return "必填";
  if (!KEY_PATTERN.test(key)) return "只能用小写字母、数字、下划线，且以字母开头";
  return null;
}

/* ── 多选标签 ── */

export interface PickOption {
  value: string;
  label: string;
  hint?: string;
  group?: string;
}

export function MultiPick({
  options,
  value,
  onChange,
  emptyLabel = "全部",
  addLabel = "添加",
  className,
  disabled,
}: {
  options: PickOption[];
  value: string[];
  onChange: (next: string[]) => void;
  /** 未选任何项时按钮上的字 */
  emptyLabel?: string;
  addLabel?: string;
  className?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const byValue = new Map(options.map((o) => [o.value, o]));
  const toggle = (v: string) => onChange(value.includes(v) ? value.filter((x) => x !== v) : [...value, v]);
  const groups = new Map<string, PickOption[]>();
  for (const o of options) {
    const g = o.group ?? "";
    groups.set(g, [...(groups.get(g) ?? []), o]);
  }

  return (
    <div className={cn("flex min-h-9 min-w-0 flex-wrap items-center gap-1.5 rounded-md border border-input px-2 py-1.5 dark:bg-input/30", className)}>
      {value.map((v) => {
        const o = byValue.get(v);
        return (
          <span key={v} className="inline-flex max-w-full min-w-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs">
            <span className="min-w-0 truncate">{o?.label ?? v}</span>
            {o?.hint ? <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{o.hint}</span> : null}
            {!disabled ? (
              <button
                type="button"
                aria-label={`移除 ${o?.label ?? v}`}
                onClick={() => toggle(v)}
                className="grid size-4 shrink-0 place-items-center rounded text-muted-foreground hover:bg-foreground/10 hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            ) : null}
          </span>
        );
      })}
      {!disabled ? (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button type="button" size="xs" variant="ghost" className="text-muted-foreground">
              <Plus />
              {value.length ? addLabel : emptyLabel}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 p-0">
            <Command>
              <CommandInput placeholder="搜索" />
              <CommandList>
                <CommandEmpty>无匹配项</CommandEmpty>
                {[...groups.entries()].map(([g, items]) => (
                  <CommandGroup key={g || "_"} heading={g || undefined}>
                    {items.map((o) => {
                      const selected = value.includes(o.value);
                      return (
                        <CommandItem key={o.value} value={`${o.label} ${o.value} ${o.hint ?? ""}`} onSelect={() => toggle(o.value)}>
                          <Check className={cn("size-3.5", selected ? "opacity-100" : "opacity-0")} />
                          <span className="min-w-0 flex-1 truncate">{o.label}</span>
                          {o.hint ? <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{o.hint}</span> : null}
                        </CommandItem>
                      );
                    })}
                  </CommandGroup>
                ))}
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      ) : value.length === 0 ? (
        <span className="text-xs text-muted-foreground">{emptyLabel}</span>
      ) : null}
    </div>
  );
}

/** 应用多选：空 = 全部应用 */
export function AppMultiSelect({ value, onChange, className }: { value: number[]; onChange: (next: number[]) => void; className?: string }) {
  const apps = useAdminAppsQuery();
  const options = (apps.data ?? []).map((a) => ({ value: String(a.id), label: a.name, hint: `#${a.id}` }));
  return (
    <MultiPick
      className={className}
      options={options}
      value={value.map(String)}
      onChange={(next) => onChange(next.map(Number).filter((n) => Number.isFinite(n) && n > 0))}
      emptyLabel="全部应用"
    />
  );
}

/* ── 结构化条件（流式特征、行为模式共用）── */

export const COND_OP_LABEL: Record<CondOp, string> = {
  eq: "等于", ne: "不等于", gt: "大于", gte: "大于等于", lt: "小于", lte: "小于等于", in: "属于", not_in: "不属于",
};

const COND_OPS = Object.keys(COND_OP_LABEL) as CondOp[];

/** 输入框里的文本 → 条件值：in / not_in 按逗号拆成数组，纯数字转数字 */
export function parseCondValue(op: CondOp, raw: string): unknown {
  const conv = (s: string) => (/^-?\d+(\.\d+)?$/.test(s) ? Number(s) : s === "true" ? true : s === "false" ? false : s);
  if (op === "in" || op === "not_in") return raw.split(/[,，]/).map((s) => s.trim()).filter(Boolean).map(conv);
  return conv(raw.trim());
}

export function formatCondValue(value: unknown): string {
  if (Array.isArray(value)) return value.map(String).join(", ");
  if (value === undefined || value === null) return "";
  return String(value);
}

export function ConditionRows({
  value,
  onChange,
  columns,
  className,
}: {
  value: Condition[];
  onChange: (next: Condition[]) => void;
  columns: string[];
  className?: string;
}) {
  // 文本按行缓存：「1,」这种半截输入转成数组再转回来会丢掉逗号
  const [texts, setTexts] = useState<Record<number, string>>({});
  const set = (i: number, patch: Partial<Condition>) => onChange(value.map((c, j) => (j === i ? { ...c, ...patch } : c)));
  return (
    <div className={cn("flex min-w-0 flex-col gap-2", className)}>
      {value.map((c, i) => (
        <div key={i} className="grid min-w-0 grid-cols-[minmax(0,1fr)_7rem_minmax(0,1fr)_auto] items-center gap-2">
          <Select value={c.field} onValueChange={(v) => set(i, { field: v })}>
            <SelectTrigger size="sm" className="h-8 w-full font-mono text-xs"><SelectValue placeholder="字段" /></SelectTrigger>
            <SelectContent>
              {columns.map((col) => <SelectItem key={col} value={col} className="font-mono text-xs">{col}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={c.op} onValueChange={(v) => {
            const op = v as CondOp;
            set(i, { op, value: parseCondValue(op, texts[i] ?? formatCondValue(c.value)) });
          }}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              {COND_OPS.map((op) => <SelectItem key={op} value={op} className="text-xs">{COND_OP_LABEL[op]}</SelectItem>)}
            </SelectContent>
          </Select>
          <Input
            className="h-8 font-mono text-xs"
            value={texts[i] ?? formatCondValue(c.value)}
            placeholder={c.op === "in" || c.op === "not_in" ? "多个值用逗号分隔" : "值"}
            onChange={(e) => {
              setTexts({ ...texts, [i]: e.target.value });
              set(i, { value: parseCondValue(c.op, e.target.value) });
            }}
          />
          <Button type="button" size="icon-sm" variant="ghost" aria-label="删除条件" onClick={() => {
            setTexts({});
            onChange(value.filter((_, j) => j !== i));
          }}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      ))}
      <Button type="button" size="xs" variant="outline" className="w-fit" onClick={() => onChange([...value, { field: columns[0] ?? "type", op: "eq", value: "" }])}>
        <Plus />
        添加条件
      </Button>
    </div>
  );
}

/* ════════════════════════ 工作台的说法 ════════════════════════ */

/** 工作台各面板的名称与一句话用途 */
export const PANEL_TITLE: Record<string, { title: string; hint: string }> = {
  basic: { title: "基本设置", hint: "场景的名称、作用范围与运行方式" },
  lists: { title: "名单", hint: "评估时先查哪些名单" },
  features: { title: "特征", hint: "评估时取用的统计数据" },
  derived: { title: "派生特征", hint: "由请求信息组合出的判断" },
  models: { title: "模型", hint: "参与打分的机器学习模型" },
  rules: { title: "规则", hint: "命中条件后加分或直接处置" },
  flow: { title: "决策流", hint: "由总分与命中情况得出处置" },
  policy: { title: "场景策略", hint: "对结论做最后的业务修正" },
  enforcement: { title: "处置方式", hint: "结论为封禁时如何执行" },
  simulate: { title: "模拟评估", hint: "用一次假想请求检验草稿" },
  release: { title: "发布与版本", hint: "上线、灰度、影子验证与回滚" },
};

/** 校验问题所在的环节 */
export const ISSUE_STAGE_LABEL: Record<string, string> = {
  scene: "基本设置", features: "特征", models: "模型", rules: "规则", bands: "分数段", flow: "决策流", policy: "场景策略",
};

/** 规则命中后的处置说法：「直接拦截」 */
export const ACTION_VERB: Record<RiskAction, string> = {
  pass: "直接放行",
  review: "转人工复核",
  captcha: "要求图形验证码",
  block: "直接拦截",
  ban: "拦截并封禁",
};

/** 模拟结论的说法：「这次请求会被拦截」 */
export const ACTION_VERDICT: Record<RiskAction, string> = {
  pass: "这次请求会被放行",
  review: "这次请求会放行，并转人工复核",
  captcha: "这次请求需要先完成图形验证码",
  block: "这次请求会被拦截",
  ban: "这次请求会被拦截，并执行封禁",
};

/** 处置配色：从轻到重。实色用于区间条与图形，浅色用于底衬 */
export const ACTION_SWATCH: Record<RiskAction, { solid: string; onSolid: string; soft: string; text: string; fill: string; stroke: string; ring: string }> = {
  pass: {
    solid: "bg-emerald-500 dark:bg-emerald-600", onSolid: "text-white", soft: "bg-emerald-500/10", text: "text-emerald-700 dark:text-emerald-300",
    fill: "fill-emerald-500", stroke: "stroke-emerald-500", ring: "border-emerald-500/40",
  },
  review: {
    solid: "bg-sky-500 dark:bg-sky-600", onSolid: "text-white", soft: "bg-sky-500/10", text: "text-sky-700 dark:text-sky-300",
    fill: "fill-sky-500", stroke: "stroke-sky-500", ring: "border-sky-500/40",
  },
  captcha: {
    solid: "bg-amber-400 dark:bg-amber-500", onSolid: "text-amber-950", soft: "bg-amber-500/10", text: "text-amber-700 dark:text-amber-300",
    fill: "fill-amber-400", stroke: "stroke-amber-400", ring: "border-amber-500/40",
  },
  block: {
    solid: "bg-orange-500 dark:bg-orange-600", onSolid: "text-white", soft: "bg-orange-500/10", text: "text-orange-700 dark:text-orange-300",
    fill: "fill-orange-500", stroke: "stroke-orange-500", ring: "border-orange-500/40",
  },
  ban: {
    solid: "bg-red-600 dark:bg-red-600", onSolid: "text-white", soft: "bg-red-500/10", text: "text-red-700 dark:text-red-300",
    fill: "fill-red-600", stroke: "stroke-red-600", ring: "border-red-500/40",
  },
};

/** 规则命中后的结果：「加 50 分」「直接拦截，并计 40 分」 */
export function ruleOutcome(rule: Pick<Rule, "score" | "decision" | "mode">): string {
  const score = rule.score ?? 0;
  const pts = score > 0 ? `加 ${score} 分` : score < 0 ? `减 ${-score} 分` : "";
  const decision = rule.decision ? ACTION_VERB[rule.decision as RiskAction] ?? "" : "";
  if (decision && pts) return `${decision}，并${pts}`;
  if (decision) return decision;
  if (pts) return pts;
  return "只记录命中";
}

/** 分数段的文字：「30 至 59 分」「100 分以上」 */
export function bandRange(b: Pick<ScoreBand, "min" | "max">): string {
  if (b.max <= 0) return `${b.min} 分及以上`;
  if (b.min === b.max) return `${b.min} 分`;
  return `${b.min} 至 ${b.max} 分`;
}

/** 分数段的问题：缺口、重叠、倒置 */
export function bandProblems(bands: ScoreBand[]): string[] {
  if (!bands.length) return ["没有分数段，任何分数都无法得出处置"];
  const out: string[] = [];
  const sorted = [...bands].sort((a, b) => a.min - b.min);
  for (const b of sorted) if (b.max > 0 && b.min > b.max) out.push(`${b.min} 至 ${b.max} 分的起点大于终点`);
  if (sorted[0].min > 0) out.push(`0 至 ${sorted[0].min - 1} 分没有对应处置`);
  for (let i = 1; i < sorted.length; i++) {
    const a = sorted[i - 1];
    const b = sorted[i];
    if (a.max <= 0) {
      out.push(`${bandRange(a)}与${bandRange(b)}重叠`);
      continue;
    }
    if (b.min > a.max + 1) out.push(`${a.max + 1} 至 ${b.min - 1} 分没有对应处置`);
    else if (b.min <= a.max) out.push(`${bandRange(a)}与${bandRange(b)}重叠`);
  }
  const last = sorted[sorted.length - 1];
  if (last.max > 0) out.push(`${last.max + 1} 分以上没有对应处置`);
  return out;
}

/** 分数落在哪一段 */
export function bandOf(bands: ScoreBand[], score: number): ScoreBand | undefined {
  return [...bands].sort((a, b) => b.min - a.min).find((b) => score >= b.min && (b.max <= 0 || score <= b.max));
}

/* ════════════════════════ 版式件 ════════════════════════ */

/** 分节卡片：标题、一句话说明、右侧操作 */
export function StudioSection({ title, description, icon: Icon, aside, children, className, bodyClassName }: {
  title: ReactNode;
  description?: ReactNode;
  icon?: LucideIcon;
  aside?: ReactNode;
  children?: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={cn("flex min-w-0 flex-col gap-4 rounded-xl border bg-card p-4 sm:p-5", className)}>
      <header className="flex min-w-0 flex-wrap items-start gap-x-3 gap-y-2">
        {Icon ? (
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
            <Icon className="size-4" />
          </span>
        ) : null}
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h3 className="text-sm leading-5 font-semibold">{title}</h3>
          {description ? <p className="text-xs leading-5 text-muted-foreground">{description}</p> : null}
        </div>
        {aside ? <div className="flex shrink-0 flex-wrap items-center gap-2">{aside}</div> : null}
      </header>
      {children !== undefined && children !== null ? <div className={cn("flex min-w-0 flex-col gap-4", bodyClassName)}>{children}</div> : null}
    </section>
  );
}

const CALLOUT_TONE = {
  info: { box: "border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-900/70 dark:bg-sky-950/40 dark:text-sky-100", icon: Info, iconClass: "text-sky-600 dark:text-sky-400" },
  success: { box: "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900/70 dark:bg-emerald-950/40 dark:text-emerald-100", icon: CircleCheck, iconClass: "text-emerald-600 dark:text-emerald-400" },
  warning: { box: "border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/40 dark:text-amber-100", icon: AlertTriangle, iconClass: "text-amber-600 dark:text-amber-400" },
  danger: { box: "border-red-200 bg-red-50 text-red-900 dark:border-red-900/70 dark:bg-red-950/40 dark:text-red-100", icon: CircleAlert, iconClass: "text-red-600 dark:text-red-400" },
} as const;

/** 提示条 */
export function Callout({ tone = "info", title, children, action, icon, className }: {
  tone?: keyof typeof CALLOUT_TONE;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  icon?: LucideIcon;
  className?: string;
}) {
  const t = CALLOUT_TONE[tone];
  const Icon = icon ?? t.icon;
  return (
    <div className={cn("flex min-w-0 items-start gap-2.5 rounded-lg border px-3 py-2.5 text-xs leading-5", t.box, className)}>
      <Icon className={cn("mt-0.5 size-4 shrink-0", t.iconClass)} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        {title ? <div className="text-sm leading-5 font-medium">{title}</div> : null}
        {children ? <div className="min-w-0 break-words opacity-90">{children}</div> : null}
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}

/** 空状态 */
export function EmptyBlock({ icon: Icon, title, description, action, className }: {
  icon?: LucideIcon;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col items-center gap-3 rounded-xl border border-dashed px-6 py-10 text-center", className)}>
      {Icon ? (
        <span className="grid size-10 place-items-center rounded-xl bg-muted text-muted-foreground">
          <Icon className="size-5" />
        </span>
      ) : null}
      <div className="flex max-w-sm flex-col gap-1">
        <p className="text-sm font-medium">{title}</p>
        {description ? <p className="text-xs leading-5 text-muted-foreground">{description}</p> : null}
      </div>
      {action ? <div className="flex flex-wrap justify-center gap-2">{action}</div> : null}
    </div>
  );
}

/** 读取失败 */
export function ErrorBlock({ title = "读取失败", error, onRetry, className }: { title?: string; error?: unknown; onRetry?: () => void; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col items-center gap-3 rounded-xl border border-red-200 bg-red-50/60 px-6 py-8 text-center dark:border-red-900/70 dark:bg-red-950/30", className)}>
      <span className="grid size-10 place-items-center rounded-xl bg-red-100 text-red-600 dark:bg-red-950 dark:text-red-400">
        <CircleAlert className="size-5" />
      </span>
      <div className="flex max-w-md flex-col gap-1">
        <p className="text-sm font-medium">{title}</p>
        {error ? <p className="text-xs leading-5 break-words text-muted-foreground">{errorText(error, "请稍后重试")}</p> : null}
      </div>
      {onRetry ? (
        <Button size="sm" variant="outline" onClick={onRetry}>
          <RotateCw />
          重试
        </Button>
      ) : null}
    </div>
  );
}

/** 列表骨架 */
export function ListSkeleton({ rows = 4, className, itemClassName }: { rows?: number; className?: string; itemClassName?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-2", className)}>
      {Array.from({ length: rows }, (_, i) => <Skeleton key={i} className={cn("h-16 rounded-lg", itemClassName)} />)}
    </div>
  );
}

/** 单选卡片：标题 + 一句话说明 */
export function OptionCard({ selected, onSelect, title, description, icon: Icon, disabled, className, accent }: {
  selected: boolean;
  onSelect: () => void;
  title: ReactNode;
  description?: ReactNode;
  icon?: LucideIcon;
  disabled?: boolean;
  className?: string;
  /** 选中时的强调色（处置色） */
  accent?: RiskAction;
}) {
  const sw = accent ? ACTION_SWATCH[accent] : null;
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      onClick={onSelect}
      className={cn(
        "flex min-w-0 items-start gap-2.5 rounded-lg border px-3 py-2.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/40",
        selected ? cn("border-foreground/40 bg-accent/60", sw?.ring, sw?.soft) : "hover:bg-accent/40",
        disabled && "cursor-not-allowed opacity-50 hover:bg-transparent",
        className,
      )}
    >
      {Icon ? <Icon className={cn("mt-0.5 size-4 shrink-0", selected ? sw?.text ?? "text-foreground" : "text-muted-foreground")} /> : null}
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-sm leading-5 font-medium">{title}</span>
        {description ? <span className="text-xs leading-5 text-muted-foreground">{description}</span> : null}
      </span>
      <span
        aria-hidden
        className={cn(
          "mt-1 grid size-3.5 shrink-0 place-items-center rounded-full border",
          selected ? "border-foreground bg-foreground" : "border-muted-foreground/40",
        )}
      >
        {selected ? <span className="size-1.5 rounded-full bg-background" /> : null}
      </span>
    </button>
  );
}

/** 二次确认 */
export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel, danger, onConfirm, pending }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel: string;
  danger?: boolean;
  onConfirm: () => void;
  pending?: boolean;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description ? <AlertDialogDescription asChild><div className="text-sm leading-6 text-muted-foreground">{description}</div></AlertDialogDescription> : null}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            disabled={pending}
            className={danger ? "bg-destructive text-white hover:bg-destructive/90" : undefined}
            onClick={onConfirm}
          >
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/* ── 编译问题 ── */

/** 校验问题列表。nameOf 把问题里的键换成名称，换不到时只说环节 */
export function IssueList({ issues, empty = "校验通过", className, nameOf, onJump }: {
  issues: CompileIssue[];
  empty?: string;
  className?: string;
  nameOf?: (issue: CompileIssue) => string | undefined;
  onJump?: (issue: CompileIssue) => void;
}) {
  if (!issues.length) {
    return (
      <p className={cn("inline-flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-400", className)}>
        <CircleCheck className="size-3.5" />
        {empty}
      </p>
    );
  }
  return (
    <ul className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      {issues.map((issue, i) => {
        const name = nameOf?.(issue);
        const stage = ISSUE_STAGE_LABEL[issue.stage];
        return (
          <li key={i} className="flex min-w-0 items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs leading-5 text-red-800 dark:border-red-900/70 dark:bg-red-950/40 dark:text-red-200">
            <CircleAlert className="mt-0.5 size-3.5 shrink-0 text-red-600 dark:text-red-400" />
            <span className="min-w-0 flex-1 break-words">
              {stage || name ? <span className="font-medium">{[stage, name ? `「${name}」` : ""].filter(Boolean).join("")}：</span> : null}
              {issue.message}
            </span>
            {onJump && stage ? (
              <button type="button" className="shrink-0 font-medium underline-offset-2 hover:underline" onClick={() => onJump(issue)}>
                去处理
              </button>
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}

export function UnsavedBadge({ dirty }: { dirty: boolean }) {
  return dirty ? <Badge variant="warning" size="sm">未保存</Badge> : null;
}

/** 排序后序列化：差异视图两边键顺序一致才有意义 */
export function stableJson(value: unknown): string {
  const sort = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(sort);
    if (v && typeof v === "object") {
      return Object.fromEntries(Object.keys(v as Record<string, unknown>).sort().map((k) => [k, sort((v as Record<string, unknown>)[k])]));
    }
    return v;
  };
  return JSON.stringify(sort(value), null, 2);
}

/* ════════════════════════ 信息图形 ════════════════════════ */

/** 区间条把 0–100 画满主轨，100 以上压进右端的一小段 */
const OVER = 14;
const DOMAIN = 100 + OVER;
const pos = (v: number) => (Math.max(0, Math.min(v, DOMAIN)) / DOMAIN) * 100;

/**
 * 分数段区间条：0–100 分按段着色，每段写上处置；marker 标出一次评估的分数。
 * 100 分以上的开放段画在右端的窄格里。
 */
export function ScoreBandBar({ bands, marker, compact, className }: {
  bands: ScoreBand[];
  marker?: number;
  compact?: boolean;
  className?: string;
}) {
  const sorted = [...bands].sort((a, b) => a.min - b.min);
  const segs = sorted.map((b) => {
    const start = pos(b.min);
    const end = b.max <= 0 ? 100 : pos(b.max + 1);
    return { band: b, start, width: Math.max(0, end - start) };
  });
  const ticks = [...new Set([0, ...sorted.map((b) => b.min).filter((m) => m > 0 && m <= 100), 100])];
  const markerAt = marker !== undefined ? pos(marker) : null;

  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      {markerAt !== null ? (
        <div className="relative h-6">
          <div className="absolute bottom-0 flex -translate-x-1/2 flex-col items-center" style={{ left: `${markerAt}%` }}>
            <span className="rounded-md bg-foreground px-1.5 py-0.5 text-[11px] leading-4 font-semibold whitespace-nowrap text-background tabular-nums">{marker} 分</span>
            <span className="size-0 border-x-4 border-t-4 border-x-transparent border-t-foreground" />
          </div>
        </div>
      ) : null}
      <div className={cn("relative w-full overflow-hidden rounded-lg bg-muted", compact ? "h-3" : "h-9")} role="img" aria-label="分数段区间">
        <div className="absolute inset-y-0 border-l border-dashed border-muted-foreground/40" style={{ left: `${pos(100)}%` }} />
        {segs.map(({ band, start, width }, i) => {
          const sw = ACTION_SWATCH[band.action] ?? ACTION_SWATCH.pass;
          return (
            <div
              key={i}
              title={`${bandRange(band)}：${ACTION_LABEL[band.action] ?? band.action}，${LEVEL_WORD[band.level] ?? band.level}`}
              className={cn("absolute inset-y-0 flex min-w-0 items-center justify-center overflow-hidden border-r-2 border-card", sw.solid, sw.onSolid)}
              style={{ left: `${start}%`, width: `${width}%` }}
            >
              {!compact && width >= 9 ? (
                <span className="truncate px-1 text-[11px] leading-4 font-medium">{ACTION_LABEL[band.action] ?? band.action}</span>
              ) : null}
            </div>
          );
        })}
        {markerAt !== null ? <div className="absolute inset-y-0 w-0.5 -translate-x-1/2 bg-foreground" style={{ left: `${markerAt}%` }} /> : null}
      </div>
      {!compact ? (
        <div className="relative h-4 text-[10px] leading-4 text-muted-foreground tabular-nums">
          {ticks.map((t) => (
            <span key={t} className={cn("absolute", t === 0 ? "" : "-translate-x-1/2")} style={{ left: `${pos(t)}%` }}>{t}</span>
          ))}
          <span className="absolute right-0">以上</span>
        </div>
      ) : null}
    </div>
  );
}

/** 分数段图例：每段一行「30 至 59 分 放行 低风险」 */
export function BandLegend({ bands, className }: { bands: ScoreBand[]; className?: string }) {
  const sorted = [...bands].sort((a, b) => a.min - b.min);
  return (
    <ul className={cn("grid min-w-0 gap-x-4 gap-y-1.5 sm:grid-cols-2 xl:grid-cols-3", className)}>
      {sorted.map((b, i) => (
        <li key={i} className="flex min-w-0 items-center gap-2 text-xs">
          <span aria-hidden className={cn("size-2.5 shrink-0 rounded-[3px]", (ACTION_SWATCH[b.action] ?? ACTION_SWATCH.pass).solid)} />
          <span className="shrink-0 tabular-nums">{bandRange(b)}</span>
          <span className="min-w-0 truncate text-muted-foreground">{ACTION_LABEL[b.action] ?? b.action}，{LEVEL_WORD[b.level] ?? b.level}</span>
        </li>
      ))}
    </ul>
  );
}

/** 比例环：多段首尾相接；dashed 段画成虚线（影子流量） */
export function SplitRing({ parts, size = 132, thickness = 12, children, className }: {
  parts: Array<{ value: number; className: string; dashed?: boolean }>;
  size?: number;
  thickness?: number;
  children?: ReactNode;
  className?: string;
}) {
  const r = (size - thickness) / 2;
  const c = 2 * Math.PI * r;
  const total = parts.filter((p) => !p.dashed).reduce((s, p) => s + Math.max(0, p.value), 0);
  const solid = parts.filter((p) => !p.dashed).length;
  const segs = parts.reduce<Array<{ p: (typeof parts)[number]; len: number; offset: number }>>((out, p) => {
    const prev = out[out.length - 1];
    const offset = prev ? prev.offset + prev.len : 0;
    out.push({ p, len: p.dashed || !total ? 0 : (Math.max(0, p.value) / total) * c, offset });
    return out;
  }, []);
  return (
    <div className={cn("relative shrink-0", className)} style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="-rotate-90" aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" strokeWidth={thickness} className="stroke-muted" />
        {total > 0 || parts.some((p) => p.dashed)
          ? segs.map(({ p, len, offset }, i) => (
              <circle
                key={i}
                cx={size / 2}
                cy={size / 2}
                r={p.dashed ? r - thickness : r}
                fill="none"
                strokeWidth={p.dashed ? thickness / 2 : thickness}
                strokeDasharray={p.dashed ? "3 4" : `${Math.max(0, len - (solid > 1 && len > 0 ? 2 : 0))} ${c}`}
                strokeDashoffset={p.dashed ? 0 : -offset}
                className={cn("transition-[stroke-dasharray,stroke-dashoffset] duration-500", p.className)}
              />
            ))
          : null}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center text-center">{children}</div>
    </div>
  );
}
