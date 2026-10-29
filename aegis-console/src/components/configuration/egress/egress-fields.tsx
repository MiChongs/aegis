"use client";

import { useMemo, useState } from "react";
import { Eye, EyeOff, KeyRound, Plus, Trash2, X } from "lucide-react";
import { parse as parseYaml, stringify as stringifyYaml } from "yaml";
import { Field } from "@/components/apps/item-grant/item-grant-fields";
import { fieldVisible, getPath, setPath } from "@/components/configuration/egress/egress-meta";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { MihomoFieldSpec } from "@/lib/api/egress";
import { cn } from "@/lib/utils";

/**
 * mihomo 表单：按后端目录（FieldSpec）渲染节点、策略组、订阅、规则集、DNS 的字段，
 * 并提供列表、键值、密钥、YAML 这几种通用输入。值都存成 mihomo 原生键值，嵌套键用点号路径写入。
 */

const SECTION_LABEL: Record<string, string> = {
  basic: "基础",
  transport: "传输",
  tls: "TLS 与伪装",
  advanced: "高级",
  members: "成员",
  health: "健康检查",
  filter: "筛选",
  override: "覆盖节点参数",
  servers: "DNS 服务器"
};

const NONE = "__none__";

export function SectionTitle({ children, aside }: { children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2 pt-1">
      <h4 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{children}</h4>
      {aside}
    </div>
  );
}

/** 可选的数字：清空即删除这个键（不像物品发放那样回写 0）。 */
export function OptionalNumber({
  value,
  onChange,
  placeholder,
  suffix,
  className
}: {
  value: unknown;
  onChange: (value: number | undefined) => void;
  placeholder?: string;
  suffix?: string;
  className?: string;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const text = draft ?? (value === undefined || value === null || value === "" ? "" : String(value));
  return (
    <div className={cn("flex items-center gap-1.5", className)}>
      <Input
        type="number"
        inputMode="numeric"
        className="h-8 min-w-0 flex-1 tabular-nums"
        value={text}
        placeholder={placeholder}
        onChange={(event) => {
          setDraft(event.target.value);
          if (event.target.value === "") onChange(undefined);
          else {
            const parsed = Number(event.target.value);
            if (Number.isFinite(parsed)) onChange(parsed);
          }
        }}
        onBlur={() => setDraft(null)}
      />
      {suffix ? <span className="shrink-0 text-xs text-muted-foreground">{suffix}</span> : null}
    </div>
  );
}

/** 密钥输入：读回的是占位，显示「已保存」；修改后才是明文，清除即提交空串。 */
export function SecretInput({
  value,
  onChange,
  secretKept,
  placeholder,
  multiline
}: {
  value: unknown;
  onChange: (value: string) => void;
  secretKept: string;
  placeholder?: string;
  multiline?: boolean;
}) {
  const [visible, setVisible] = useState(false);
  const text = typeof value === "string" ? value : "";
  if (text === secretKept) {
    return (
      <div className="flex h-8 items-center gap-2 rounded-md border border-dashed px-2.5 text-xs text-muted-foreground">
        <KeyRound className="size-3.5 shrink-0 text-emerald-500" />
        <span className="flex-1">已保存，不回显</span>
        <button type="button" className="text-foreground hover:underline" onClick={() => onChange("")}>
          重新填写
        </button>
      </div>
    );
  }
  if (multiline) {
    return <Textarea className="min-h-20 font-mono text-xs" value={text} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} spellCheck={false} />;
  }
  return (
    <div className="relative">
      <Input
        type={visible ? "text" : "password"}
        className="h-8 pr-8 font-mono text-xs"
        value={text}
        placeholder={placeholder}
        autoComplete="new-password"
        onChange={(event) => onChange(event.target.value)}
      />
      <button
        type="button"
        className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
        onClick={() => setVisible(!visible)}
        aria-label={visible ? "隐藏" : "显示"}
      >
        {visible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
      </button>
    </div>
  );
}

/** 一行一项的字符串列表。 */
export function ListInput({
  value,
  onChange,
  placeholder,
  rows = 3,
  mono = true
}: {
  value: unknown;
  onChange: (value: string[]) => void;
  placeholder?: string;
  rows?: number;
  mono?: boolean;
}) {
  const items = Array.isArray(value) ? value.map(String) : typeof value === "string" && value ? [value] : [];
  const [draft, setDraft] = useState<string | null>(null);
  return (
    <Textarea
      rows={rows}
      className={cn("min-h-0 text-xs", mono && "font-mono")}
      value={draft ?? items.join("\n")}
      placeholder={placeholder}
      spellCheck={false}
      onChange={(event) => {
        setDraft(event.target.value);
        onChange(
          event.target.value
            .split(/\n/)
            .map((item) => item.trim())
            .filter(Boolean)
        );
      }}
      onBlur={() => setDraft(null)}
    />
  );
}

/** 键值对编辑（请求头、按域名指定 DNS）。 */
export function KeyValueInput({
  value,
  onChange,
  placeholder
}: {
  value: unknown;
  onChange: (value: Record<string, unknown> | undefined) => void;
  placeholder?: string;
}) {
  const entries = value && typeof value === "object" && !Array.isArray(value) ? Object.entries(value as Record<string, unknown>) : [];
  const [rows, setRows] = useState<Array<[string, string]>>(() =>
    entries.map(([key, item]) => [key, Array.isArray(item) ? item.join(", ") : String(item ?? "")])
  );
  const [placeholderKey, placeholderValue] = (placeholder ?? "键: 值").split(/:\s?/, 2);
  const commit = (next: Array<[string, string]>) => {
    setRows(next);
    const out: Record<string, unknown> = {};
    for (const [key, item] of next) if (key.trim()) out[key.trim()] = item;
    onChange(Object.keys(out).length ? out : undefined);
  };
  return (
    <div className="space-y-1.5">
      {rows.map(([key, item], index) => (
        <div key={index} className="flex items-center gap-1.5">
          <Input className="h-8 w-[40%] font-mono text-xs" value={key} placeholder={placeholderKey} onChange={(event) => commit(rows.map((row, i) => (i === index ? [event.target.value, row[1]] : row)))} />
          <Input className="h-8 flex-1 font-mono text-xs" value={item} placeholder={placeholderValue} onChange={(event) => commit(rows.map((row, i) => (i === index ? [row[0], event.target.value] : row)))} />
          <Button size="icon" variant="ghost" className="size-8 shrink-0" onClick={() => commit(rows.filter((_, i) => i !== index))} aria-label="删除">
            <X />
          </Button>
        </div>
      ))}
      <Button size="sm" variant="outline" className="h-7 gap-1 text-xs" onClick={() => setRows([...rows, ["", ""]])}>
        <Plus /> 添加一项
      </Button>
    </div>
  );
}

/** 带「不设置」的下拉框（Radix Select 不允许空值，用哨兵值代替）。 */
export function OptionSelect({
  value,
  options,
  onChange,
  labels,
  noneLabel = "不设置",
  allowNone = true
}: {
  value: unknown;
  options: string[];
  onChange: (value: string | undefined) => void;
  labels?: Record<string, string>;
  noneLabel?: string;
  allowNone?: boolean;
}) {
  const text = value === undefined || value === null || value === "" ? NONE : String(value);
  const choices = options.filter((option) => option !== "");
  const custom = text !== NONE && !choices.includes(text);
  return (
    <Select value={text} onValueChange={(next) => onChange(next === NONE ? undefined : next)}>
      <SelectTrigger size="sm" className="h-8 w-full text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {allowNone || options.includes("") ? (
          <SelectItem value={NONE} className="text-xs text-muted-foreground">
            {noneLabel}
          </SelectItem>
        ) : null}
        {custom ? (
          <SelectItem value={text} className="text-xs">
            {text}
          </SelectItem>
        ) : null}
        {choices.map((option) => (
          <SelectItem key={option} value={option} className="text-xs">
            {labels?.[option] ?? option}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function ToggleRow({ label, hint, checked, onChange }: { label: string; hint?: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-start justify-between gap-3 rounded-lg px-0.5 py-0.5">
      <span className="min-w-0">
        <span className="block text-xs font-medium">{label}</span>
        {hint ? <span className="block text-[11px] leading-4 text-muted-foreground">{hint}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} className="mt-0.5" />
    </label>
  );
}

/** 渲染一个字段。 */
export function SpecField({
  spec,
  values,
  onChange,
  secretKept
}: {
  spec: MihomoFieldSpec;
  values: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  secretKept: string;
}) {
  const value = getPath(values, spec.key);
  const write = (next: unknown) => onChange(setPath(values, spec.key, next));
  const label = (
    <>
      {spec.label}
      {spec.required ? <span className="ml-0.5 text-red-500">*</span> : null}
    </>
  );
  switch (spec.kind) {
    case "bool":
      return <ToggleRow label={spec.label} hint={spec.hint} checked={value === undefined ? spec.default === true : value === true} onChange={(checked) => write(checked)} />;
    case "number":
      return (
        <Field label={label} hint={spec.hint}>
          <OptionalNumber value={value} onChange={write} placeholder={spec.placeholder ?? (spec.default !== undefined ? `默认 ${String(spec.default)}` : undefined)} />
        </Field>
      );
    case "select":
      return (
        <Field label={label} hint={spec.hint}>
          <OptionSelect value={value} options={spec.options ?? []} onChange={write} allowNone={!spec.required} noneLabel={spec.default !== undefined ? `默认（${String(spec.default)}）` : "不设置"} />
        </Field>
      );
    case "secret":
      return (
        <Field label={label} hint={spec.hint}>
          <SecretInput value={value} onChange={write} secretKept={secretKept} placeholder={spec.placeholder} multiline={spec.key.endsWith("private-key")} />
        </Field>
      );
    case "list":
      return (
        <Field label={label} hint={spec.hint ?? "一行一项"}>
          <ListInput value={value} onChange={write} placeholder={spec.placeholder} />
        </Field>
      );
    case "textarea":
      return (
        <Field label={label} hint={spec.hint}>
          <Textarea className="min-h-20 font-mono text-xs" value={typeof value === "string" ? value : ""} placeholder={spec.placeholder} onChange={(event) => write(event.target.value)} spellCheck={false} />
        </Field>
      );
    case "map":
      return (
        <Field label={label} hint={spec.hint}>
          <KeyValueInput value={value} onChange={write} placeholder={spec.placeholder} />
        </Field>
      );
    default:
      return (
        <Field label={label} hint={spec.hint}>
          <Input
            className="h-8 text-xs"
            value={value === undefined || value === null ? "" : String(value)}
            placeholder={spec.placeholder ?? (spec.default !== undefined ? String(spec.default) : undefined)}
            onChange={(event) => write(event.target.value)}
          />
        </Field>
      );
  }
}

/** 按 section 分组渲染全部字段；skip 里的键由外层自己渲染（如名称、服务器）。 */
export function SpecForm({
  fields,
  values,
  onChange,
  secretKept,
  skip = []
}: {
  fields: MihomoFieldSpec[] | null | undefined;
  values: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  secretKept: string;
  skip?: string[];
}) {
  const sections = useMemo(() => {
    const out = new Map<string, MihomoFieldSpec[]>();
    for (const spec of fields ?? []) {
      if (skip.includes(spec.key)) continue;
      const key = spec.section ?? "basic";
      out.set(key, [...(out.get(key) ?? []), spec]);
    }
    return [...out.entries()];
  }, [fields, skip]);
  return (
    <div className="space-y-5">
      {sections.map(([section, specs]) => {
        const visible = specs.filter((spec) => fieldVisible(spec, values));
        if (!visible.length) return null;
        const toggles = visible.filter((spec) => spec.kind === "bool");
        const others = visible.filter((spec) => spec.kind !== "bool");
        return (
          <section key={section} className="space-y-3">
            <SectionTitle>{SECTION_LABEL[section] ?? section}</SectionTitle>
            {others.length ? (
              <div className="grid grid-cols-2 gap-x-3 gap-y-3">
                {others.map((spec) => (
                  <div key={spec.key} className={cn(["list", "map", "textarea", "secret"].includes(spec.kind) || spec.hint ? "col-span-2" : "col-span-1")}>
                    <SpecField spec={spec} values={values} onChange={onChange} secretKept={secretKept} />
                  </div>
                ))}
              </div>
            ) : null}
            {toggles.length ? (
              <div className="space-y-1.5 rounded-xl border p-2.5">
                {toggles.map((spec) => (
                  <SpecField key={spec.key} spec={spec} values={values} onChange={onChange} secretKept={secretKept} />
                ))}
              </div>
            ) : null}
          </section>
        );
      })}
    </div>
  );
}

/** YAML 编辑：失焦或按「应用」时解析，解析失败只提示、不写回。 */
export function YamlEditor({
  value,
  onCommit,
  hint,
  rows = 14,
  omit = []
}: {
  value: Record<string, unknown> | undefined;
  onCommit: (value: Record<string, unknown>) => void;
  hint?: React.ReactNode;
  rows?: number;
  omit?: string[];
}) {
  const source = useMemo(() => {
    const copy = { ...(value ?? {}) };
    for (const key of omit) delete copy[key];
    return Object.keys(copy).length ? stringifyYaml(copy, { lineWidth: 0 }) : "";
  }, [value, omit]);
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const text = draft ?? source;
  const commit = () => {
    if (draft === null) return;
    try {
      const parsed = draft.trim() ? parseYaml(draft) : {};
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("顶层必须是键值对");
      const kept: Record<string, unknown> = {};
      for (const key of omit) if (value && key in value) kept[key] = value[key];
      onCommit({ ...(parsed as Record<string, unknown>), ...kept });
      setDraft(null);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "YAML 无法解析");
    }
  };
  return (
    <div className="space-y-1.5">
      <Textarea
        rows={rows}
        className={cn("min-h-0 font-mono text-xs leading-5", error && "border-red-400 focus-visible:ring-red-400/30")}
        value={text}
        spellCheck={false}
        onChange={(event) => {
          setDraft(event.target.value);
          setError(null);
        }}
        onBlur={commit}
      />
      {error ? <p className="text-[11px] leading-4 text-red-600 dark:text-red-400">{error}</p> : hint ? <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p> : null}
      {draft !== null && !error ? (
        <div className="flex justify-end gap-1.5">
          <Button size="sm" variant="ghost" className="h-7 text-xs" onClick={() => setDraft(null)}>
            撤回
          </Button>
          <Button size="sm" className="h-7 text-xs" onClick={commit}>
            应用修改
          </Button>
        </div>
      ) : null}
    </div>
  );
}

export function RemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button size="icon" variant="ghost" className="size-7 shrink-0 text-muted-foreground hover:text-destructive" onClick={onClick} aria-label={label}>
      <Trash2 className="size-3.5" />
    </Button>
  );
}
