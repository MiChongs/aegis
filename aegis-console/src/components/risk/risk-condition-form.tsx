"use client";

import { useMemo } from "react";
import { AlertTriangle, Check, Loader2, X } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { RiskConditionCatalog, RiskFieldSchema } from "@/lib/api/types";
import { cn } from "@/lib/utils";
import { useRiskCatalog } from "./risk-shared";

/**
 * 条件参数表单 —— 完全由后端 `/risk/metadata` 的 schema 驱动。
 *
 * 重构前每种条件的参数字段是硬编码在页面里的 if 链，于是后端加一种条件类型，
 * 前端要么跟着改，要么管理员配出一条没有参数、永不命中的规则。
 * 现在新增条件类型时这里零改动。
 */

export type ConditionData = Record<string, unknown>;

/** 用 schema 的默认值造一份初始参数。 */
export function defaultConditionData(catalog?: RiskConditionCatalog): ConditionData {
  const data: ConditionData = {};
  for (const field of catalog?.fields ?? []) {
    if (field.default !== undefined && field.default !== null) {
      data[field.key] = field.default;
    } else if (field.type === "bool") {
      data[field.key] = false;
    } else if (field.type === "list") {
      data[field.key] = [];
    } else {
      data[field.key] = "";
    }
  }
  return data;
}

/** list 类型在表单里是逗号 / 换行分隔的文本，提交前拆成数组。 */
export function normalizeConditionData(catalog: RiskConditionCatalog | undefined, data: ConditionData): ConditionData {
  const out: ConditionData = {};
  for (const field of catalog?.fields ?? []) {
    const raw = data[field.key];
    switch (field.type) {
      case "number":
        out[field.key] = typeof raw === "number" ? raw : Number(String(raw ?? "").trim() || 0);
        break;
      case "bool":
        out[field.key] = Boolean(raw);
        break;
      case "list":
        out[field.key] = Array.isArray(raw)
          ? raw
          : String(raw ?? "").split(/[,\n;、\s]+/).map((s) => s.trim()).filter(Boolean);
        break;
      default:
        out[field.key] = String(raw ?? "").trim();
    }
  }
  return out;
}

/** 把落库的参数还原成表单值（数组 → 逗号分隔文本）。 */
export function conditionDataToForm(catalog: RiskConditionCatalog | undefined, data: ConditionData): ConditionData {
  const out: ConditionData = { ...data };
  for (const field of catalog?.fields ?? []) {
    if (field.type === "list" && Array.isArray(data[field.key])) {
      out[field.key] = (data[field.key] as unknown[]).join(", ");
    }
  }
  return out;
}

/** 前端侧的必填校验，与后端 ValidateRuleInput 同一份 schema，因此不会漂移。 */
export function validateConditionData(catalog: RiskConditionCatalog | undefined, data: ConditionData): string | null {
  for (const field of catalog?.fields ?? []) {
    if (!field.required) continue;
    const raw = data[field.key];
    if (field.type === "list") {
      const list = Array.isArray(raw) ? raw : String(raw ?? "").split(/[,\n;、\s]+/).filter(Boolean);
      if (list.length === 0) return `请填写${field.label}`;
      continue;
    }
    if (field.type === "number") {
      const num = typeof raw === "number" ? raw : Number(String(raw ?? "").trim());
      if (!Number.isFinite(num)) return `${field.label}必须是数字`;
      if (field.min != null && num < field.min) return `${field.label}不能小于 ${field.min}`;
      if (field.max != null && num > field.max) return `${field.label}不能大于 ${field.max}`;
      continue;
    }
    if (String(raw ?? "").trim() === "") return `请填写${field.label}`;
  }
  return null;
}

export function ConditionTypeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { conditions } = useRiskCatalog();
  // 按目录声明的分组归类，一条平铺的十七项下拉是选不动的。
  const grouped = useMemo(() => {
    const map = new Map<string, RiskConditionCatalog[]>();
    for (const item of conditions) {
      const list = map.get(item.group) ?? [];
      list.push(item);
      map.set(item.group, list);
    }
    return [...map.entries()];
  }, [conditions]);

  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-8 text-xs"><SelectValue placeholder="选择条件类型" /></SelectTrigger>
      <SelectContent>
        {grouped.map(([group, items]) => (
          <div key={group}>
            <div className="px-2 py-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{group}</div>
            {items.map((item) => (
              <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
            ))}
          </div>
        ))}
      </SelectContent>
    </Select>
  );
}

/**
 * 条件的说明与前置依赖提示。
 *
 * requiresProvider / requiresRedis 是最值得显式提示的两件事：
 * 没配情报源时「代理 / VPN」规则永远不命中，而界面上它显示"已启用" ——
 * 不说出来的话，管理员会以为已经防住了。
 */
export function ConditionHint({ catalog, providerReady, redisReady }: {
  catalog?: RiskConditionCatalog; providerReady?: boolean; redisReady?: boolean;
}) {
  if (!catalog) return null;
  const missingProvider = catalog.requiresProvider && providerReady === false;
  const missingRedis = catalog.requiresRedis && redisReady === false;
  return (
    <div className="space-y-1.5 rounded-lg border bg-muted/30 px-3 py-2">
      <p className="text-xs leading-relaxed text-muted-foreground">{catalog.description}</p>
      {missingProvider && (
        <p className="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-400">
          <AlertTriangle className="mt-0.5 size-3 shrink-0" />
          未配置 IP 情报源，此条件不会命中
        </p>
      )}
      {missingRedis && (
        <p className="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-400">
          <AlertTriangle className="mt-0.5 size-3 shrink-0" />
          未启用频率限流，相关计数恒为 0
        </p>
      )}
    </div>
  );
}

export function ConditionFields({ catalog, value, onChange }: {
  catalog?: RiskConditionCatalog;
  value: ConditionData;
  onChange: (next: ConditionData) => void;
}) {
  const fields = catalog?.fields ?? [];
  if (fields.length === 0) {
    return <p className="rounded-lg border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">无需额外参数</p>;
  }
  const patch = (key: string, next: unknown) => onChange({ ...value, [key]: next });

  return (
    <div className="grid gap-3 sm:grid-cols-2">
      {fields.map((field) => (
        <ConditionField key={field.key} field={field} value={value[field.key]} onChange={(v) => patch(field.key, v)} />
      ))}
    </div>
  );
}

function ConditionField({ field, value, onChange }: {
  field: RiskFieldSchema; value: unknown; onChange: (v: unknown) => void;
}) {
  const wide = field.type === "textarea" || field.type === "list";
  return (
    <div className={cn("space-y-1", wide && "sm:col-span-2")}>
      <Label className="flex items-center gap-1 text-xs">
        {field.label}
        {field.required && <span className="text-rose-500">*</span>}
      </Label>
      {renderControl(field, value, onChange)}
      {field.help && <p className="text-[10px] text-muted-foreground">{field.help}</p>}
    </div>
  );
}

function renderControl(field: RiskFieldSchema, value: unknown, onChange: (v: unknown) => void) {
  switch (field.type) {
    case "bool":
      return (
        <div className="flex h-8 items-center">
          <Switch checked={Boolean(value)} onCheckedChange={onChange} />
        </div>
      );
    case "select":
      return (
        <Select value={String(value ?? "")} onValueChange={onChange}>
          <SelectTrigger className="h-8 text-xs"><SelectValue placeholder={field.placeholder} /></SelectTrigger>
          <SelectContent>
            {(field.options ?? []).map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    case "number":
      return (
        <Input type="number" className="h-8 font-mono text-xs"
          min={field.min ?? undefined} max={field.max ?? undefined}
          value={value === undefined || value === null ? "" : String(value)}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value === "" ? "" : Number(e.target.value))} />
      );
    case "textarea":
      return (
        <Textarea rows={3} className="font-mono text-xs"
          value={String(value ?? "")} placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)} />
      );
    case "list":
      return (
        <Textarea rows={2} className="font-mono text-xs"
          value={Array.isArray(value) ? value.join(", ") : String(value ?? "")}
          placeholder={field.placeholder ?? "多项用逗号或换行分隔"}
          onChange={(e) => onChange(e.target.value)} />
      );
    case "time":
      return (
        <Input type="time" className="h-8 w-32 font-mono text-xs"
          value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />
      );
    default:
      return (
        <Input className="h-8 text-xs" value={String(value ?? "")} placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)} />
      );
  }
}

/** 表达式校验的实时反馈条。 */
export function ExpressionVerdict({ state }: {
  state: { status: "idle" | "checking" | "valid" | "invalid"; message?: string };
}) {
  if (state.status === "idle") return null;
  if (state.status === "checking") {
    return (
      <p className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin" />校验中…
      </p>
    );
  }
  if (state.status === "valid") {
    return (
      <p className="flex items-center gap-1.5 text-[11px] text-emerald-600 dark:text-emerald-400">
        <Check className="size-3" />语法正确
      </p>
    );
  }
  return (
    <div className="flex items-start gap-1.5 rounded-md border border-rose-200 bg-rose-50 px-2 py-1.5 dark:border-rose-900/60 dark:bg-rose-950/40">
      <X className="mt-0.5 size-3 shrink-0 text-rose-600 dark:text-rose-400" />
      <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-[10px] leading-relaxed text-rose-700 dark:text-rose-300">
        {state.message}
      </pre>
    </div>
  );
}

/** 变量与函数速查。写表达式时最需要的就是「我能用什么」。 */
export function ExpressionReference({ onInsert }: { onInsert: (snippet: string) => void }) {
  const { metadata } = useRiskCatalog();
  const grouped = useMemo(() => {
    const map = new Map<string, typeof metadata extends undefined ? never : NonNullable<typeof metadata>["variables"]>();
    for (const variable of metadata?.variables ?? []) {
      const list = map.get(variable.group) ?? [];
      list.push(variable);
      map.set(variable.group, list);
    }
    return [...map.entries()];
  }, [metadata]);

  if (!metadata) return null;

  return (
    <div className="space-y-3 rounded-lg border bg-muted/20 p-3">
      <div className="space-y-2">
        <h5 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">可用变量</h5>
        {grouped.map(([group, variables]) => (
          <div key={group} className="space-y-1">
            <span className="text-[10px] text-muted-foreground">{group}</span>
            <div className="flex flex-wrap gap-1">
              {variables.map((variable) => (
                <button key={variable.name} type="button" onClick={() => onInsert(variable.name)}
                  title={`${variable.description}（${variable.type}）`}
                  className="rounded border bg-background px-1.5 py-0.5 font-mono text-[10px] transition-colors hover:border-primary/40 hover:bg-accent">
                  {variable.name}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>

      <div className="space-y-1">
        <h5 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">内置函数</h5>
        {metadata.functions.map((fn) => (
          <button key={fn.name} type="button" onClick={() => onInsert(fn.example ?? fn.name)}
            className="flex w-full items-start gap-2 rounded border bg-background px-2 py-1 text-left transition-colors hover:border-primary/40 hover:bg-accent">
            <code className="shrink-0 font-mono text-[10px]">{fn.name}</code>
            <span className="min-w-0 flex-1 text-[10px] text-muted-foreground">{fn.description}</span>
          </button>
        ))}
      </div>

      <div className="space-y-1">
        <h5 className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">示例</h5>
        {metadata.samples.map((sample) => (
          <button key={sample.title} type="button" onClick={() => onInsert(sample.expression)}
            className="flex w-full flex-col gap-0.5 rounded border bg-background px-2 py-1 text-left transition-colors hover:border-primary/40 hover:bg-accent">
            <span className="flex items-center gap-1.5">
              <Badge variant="outline" size="sm">{sample.title}</Badge>
            </span>
            <code className="break-all font-mono text-[10px] text-muted-foreground">{sample.expression}</code>
          </button>
        ))}
      </div>
    </div>
  );
}
