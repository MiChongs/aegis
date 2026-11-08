"use client";

import { useState, type ReactNode } from "react";
import { Check, Plus, Trash2, X } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import type { CompileIssue, CondOp, Condition } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

/** 风控工作台与配置页共用的表单件 */

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

/* ── 编译问题 ── */

export function IssueList({ issues, empty = "校验通过", className }: { issues: CompileIssue[]; empty?: string; className?: string }) {
  if (!issues.length) return <p className={cn("text-xs text-emerald-600 dark:text-emerald-400", className)}>{empty}</p>;
  return (
    <ul className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      {issues.map((issue, i) => (
        <li key={i} className="flex min-w-0 items-start gap-2 rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 text-xs text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
          {issue.key ? <span className="shrink-0 font-mono">{issue.key}</span> : null}
          <span className="min-w-0 break-words">{issue.message}</span>
        </li>
      ))}
    </ul>
  );
}

/* ── 面板 ── */

export function StudioSection({ title, aside, children, className }: { title: ReactNode; aside?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn("flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4 sm:p-5", className)}>
      <header className="flex min-w-0 flex-wrap items-center gap-2">
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</h3>
        {aside ? <div className="flex shrink-0 flex-wrap items-center gap-1.5">{aside}</div> : null}
      </header>
      {children}
    </section>
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
