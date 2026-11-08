"use client";

import { Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Segmented } from "@/components/traffic/traffic-kit";
import type { Feature, RiskModel, RiskVariable, SceneField } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

import type { CompletionEntry } from "./code-editor";

/**
 * 规则条件的可视化构建器：若干组「变量 运算 值」，组内与组间各自选「且 / 或」，生成 expr 表达式。
 *
 * 只覆盖构建器自己能生成的那几种写法；反解失败（手写的复杂表达式）时调用方退回表达式编辑。
 */

export type VarKind = "bool" | "number" | "string" | "list";

export interface VarOption {
  name: string;
  label: string;
  kind: VarKind;
  group: string;
}

export type Join = "&&" | "||";
export interface CondRow { variable: string; op: string; value: string }
export interface CondGroup { join: Join; rows: CondRow[] }
export interface CondTree { join: Join; groups: CondGroup[] }

const KIND_OF_TYPE: Record<string, VarKind> = { bool: "bool", int: "number", float: "number", string: "string", "[]string": "list" };

/** 变量目录：请求变量、场景入参、特征、模型分 */
export function buildVarOptions(variables: RiskVariable[], features: Feature[], models: Pick<RiskModel, "key" | "name">[], fields: SceneField[]): VarOption[] {
  const out: VarOption[] = [];
  for (const v of variables) {
    const kind = KIND_OF_TYPE[v.type];
    if (!kind) continue; // extra / F / M 这三个 map 由下面展开
    const group = v.name.includes(".") ? v.name.split(".")[0] : "请求";
    out.push({ name: v.name, label: v.label, kind, group: GROUP_LABEL[group] ?? group });
  }
  for (const f of fields) {
    out.push({ name: `extra.${f.key}`, label: f.label || f.key, kind: f.type === "number" ? "number" : f.type === "bool" ? "bool" : "string", group: "场景入参" });
  }
  for (const f of features) out.push({ name: `F.${f.key}`, label: f.name, kind: "number", group: "特征" });
  for (const m of models) out.push({ name: `M.${m.key}`, label: m.name, kind: "number", group: "模型分" });
  return out;
}

const GROUP_LABEL: Record<string, string> = { 请求: "请求", device: "设备", client: "客户端", net: "网络", list: "名单" };

/** 同一份目录喂给 Monaco 补全 */
export function completionsOf(options: VarOption[]): CompletionEntry[] {
  return options.map((o) => ({
    label: o.name,
    detail: o.label,
    kind: o.name.startsWith("F.") ? "feature" : o.name.startsWith("M.") ? "model" : "variable",
  }));
}

const OPS: Record<VarKind, Array<{ value: string; label: string }>> = {
  bool: [{ value: "true", label: "为真" }, { value: "false", label: "为假" }],
  number: [
    { value: ">=", label: "≥" }, { value: ">", label: ">" }, { value: "<=", label: "≤" }, { value: "<", label: "<" },
    { value: "==", label: "=" }, { value: "!=", label: "≠" },
  ],
  string: [
    { value: "==", label: "等于" }, { value: "!=", label: "不等于" }, { value: "in", label: "属于" }, { value: "not in", label: "不属于" },
    { value: "contains", label: "包含" }, { value: "startsWith", label: "开头是" }, { value: "endsWith", label: "结尾是" }, { value: "matches", label: "匹配正则" },
  ],
  list: [{ value: "has", label: "包含" }, { value: "not_has", label: "不包含" }],
};

const quote = (s: string) => JSON.stringify(s);
const splitValues = (s: string) => s.split(/[,，]/).map((x) => x.trim()).filter(Boolean);

function rowExpr(row: CondRow, kind: VarKind): string {
  const v = row.variable;
  switch (kind) {
    case "bool":
      return row.op === "false" ? `!${v}` : v;
    case "number": {
      const n = row.value.trim() || "0";
      return `${v} ${row.op} ${n}`;
    }
    case "list":
      return row.op === "not_has" ? `!(${quote(row.value.trim())} in ${v})` : `${quote(row.value.trim())} in ${v}`;
    default:
      if (row.op === "in" || row.op === "not in") return `${v} ${row.op} [${splitValues(row.value).map(quote).join(", ")}]`;
      return `${v} ${row.op} ${quote(row.value)}`;
  }
}

export function kindOf(options: VarOption[], name: string): VarKind {
  const found = options.find((o) => o.name === name);
  if (found) return found.kind;
  if (name.startsWith("F.") || name.startsWith("M.")) return "number";
  return "string";
}

export function treeToExpr(tree: CondTree, options: VarOption[]): string {
  const groups = tree.groups
    .map((g) => {
      const rows = g.rows.filter((r) => r.variable).map((r) => rowExpr(r, kindOf(options, r.variable)));
      if (!rows.length) return "";
      const joined = rows.join(` ${g.join} `);
      return rows.length > 1 && tree.groups.length > 1 ? `(${joined})` : joined;
    })
    .filter(Boolean);
  return groups.join(` ${tree.join} `);
}

/* ── 反解 ── */

/** 在括号 / 方括号 / 引号之外按运算符切分 */
function splitTop(src: string, op: Join): string[] {
  const parts: string[] = [];
  let depth = 0;
  let quoteCh = "";
  let start = 0;
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    if (quoteCh) {
      if (c === "\\") i++;
      else if (c === quoteCh) quoteCh = "";
      continue;
    }
    if (c === '"' || c === "'") quoteCh = c;
    else if (c === "(" || c === "[") depth++;
    else if (c === ")" || c === "]") depth--;
    else if (depth === 0 && src.startsWith(op, i)) {
      parts.push(src.slice(start, i).trim());
      start = i + op.length;
      i += op.length - 1;
    }
  }
  parts.push(src.slice(start).trim());
  return parts;
}

function stripParens(s: string): string {
  let t = s.trim();
  while (t.startsWith("(") && t.endsWith(")")) {
    let depth = 0;
    let closesAtEnd = true;
    for (let i = 0; i < t.length; i++) {
      if (t[i] === "(") depth++;
      else if (t[i] === ")") depth--;
      if (depth === 0 && i < t.length - 1) {
        closesAtEnd = false;
        break;
      }
    }
    if (!closesAtEnd) break;
    t = t.slice(1, -1).trim();
  }
  return t;
}

function parseStr(s: string): string | null {
  const t = s.trim();
  if (/^"(?:[^"\\]|\\.)*"$/.test(t)) {
    try {
      return JSON.parse(t) as string;
    } catch {
      return null;
    }
  }
  if (/^'[^']*'$/.test(t)) return t.slice(1, -1);
  return null;
}

const VAR = String.raw`[A-Za-z_][\w.]*`;
const STR = String.raw`"(?:[^"\\]|\\.)*"|'[^']*'`;

function parseAtom(src: string): CondRow | null {
  const s = stripParens(src);
  let m: RegExpMatchArray | null;
  if ((m = s.match(new RegExp(`^!\\s*\\(\\s*(${STR})\\s+in\\s+(${VAR})\\s*\\)$`)))) return { variable: m[2], op: "not_has", value: parseStr(m[1]) ?? "" };
  if ((m = s.match(new RegExp(`^(${STR})\\s+in\\s+(${VAR})$`)))) return { variable: m[2], op: "has", value: parseStr(m[1]) ?? "" };
  if ((m = s.match(new RegExp(`^!\\s*(${VAR})$`)))) return { variable: m[1], op: "false", value: "" };
  if ((m = s.match(new RegExp(`^(${VAR})\\s+(not in|in)\\s+\\[(.*)\\]$`)))) {
    const items = m[3].trim() ? m[3].split(",").map((x) => parseStr(x)) : [];
    if (items.some((x) => x === null)) return null;
    return { variable: m[1], op: m[2], value: (items as string[]).join(", ") };
  }
  if ((m = s.match(new RegExp(`^(${VAR})\\s+(contains|startsWith|endsWith|matches)\\s+(${STR})$`)))) {
    return { variable: m[1], op: m[2], value: parseStr(m[3]) ?? "" };
  }
  if ((m = s.match(new RegExp(`^(${VAR})\\s*(==|!=|>=|<=|>|<)\\s*(.+)$`)))) {
    const str = parseStr(m[3]);
    if (str !== null) return { variable: m[1], op: m[2], value: str };
    if (/^-?\d+(\.\d+)?$/.test(m[3].trim())) return { variable: m[1], op: m[2], value: m[3].trim() };
    return null;
  }
  if ((m = s.match(new RegExp(`^(${VAR})$`))) && !["true", "false", "nil"].includes(m[1])) return { variable: m[1], op: "true", value: "" };
  return null;
}

function parseGroup(src: string, join: Join): CondGroup | null {
  const rows = splitTop(stripParens(src), join).map(parseAtom);
  if (rows.some((r) => r === null)) return null;
  return { join, rows: rows as CondRow[] };
}

/** expr → 构建器状态；超出构建器能表达的范围时返回 null */
export function exprToTree(expr: string): CondTree | null {
  const s = expr.trim();
  if (!s) return { join: "&&", groups: [{ join: "&&", rows: [] }] };
  const ors = splitTop(s, "||");
  if (ors.length > 1) {
    const groups = ors.map((p) => {
      const inner = stripParens(p);
      if (splitTop(inner, "||").length > 1) return null;
      return parseGroup(inner, "&&");
    });
    if (groups.some((g) => g === null)) return null;
    return { join: "||", groups: groups as CondGroup[] };
  }
  const ands = splitTop(s, "&&");
  const groups = ands.map((p) => {
    const inner = stripParens(p);
    if (splitTop(inner, "&&").length > 1 && ands.length > 1) return null;
    return splitTop(inner, "||").length > 1 ? parseGroup(inner, "||") : parseGroup(inner, "&&");
  });
  if (groups.some((g) => g === null)) return null;
  const gs = groups as CondGroup[];
  if (gs.length === 1) return { join: "&&", groups: gs };
  // a && b && c：一组即可；(a || b) && c：多组
  if (gs.every((g) => g.rows.length === 1)) return { join: "&&", groups: [{ join: "&&", rows: gs.flatMap((g) => g.rows) }] };
  return { join: "&&", groups: gs };
}

/* ── 界面 ── */

const JOIN_OPTIONS = [{ value: "&&" as Join, label: "且" }, { value: "||" as Join, label: "或" }];

function VariablePicker({ options, value, onChange }: { options: VarOption[]; value: string; onChange: (v: string) => void }) {
  const current = options.find((o) => o.name === value);
  const groups = new Map<string, VarOption[]>();
  for (const o of options) groups.set(o.group, [...(groups.get(o.group) ?? []), o]);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="sm" className="h-8 w-full min-w-0 justify-start font-normal">
          {value ? (
            <span className="flex min-w-0 items-baseline gap-1.5">
              <span className="truncate font-mono text-xs">{value}</span>
              {current ? <span className="truncate text-[11px] text-muted-foreground">{current.label}</span> : null}
            </span>
          ) : (
            <span className="text-xs text-muted-foreground">选择变量</span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 p-0">
        <Command>
          <CommandInput placeholder="搜索变量、特征、模型" />
          <CommandList className="max-h-80">
            <CommandEmpty>无匹配变量</CommandEmpty>
            {[...groups.entries()].map(([g, items]) => (
              <CommandGroup key={g} heading={g}>
                {items.map((o) => (
                  <CommandItem key={o.name} value={`${o.name} ${o.label}`} onSelect={() => onChange(o.name)}>
                    <span className="truncate font-mono text-xs">{o.name}</span>
                    <span className="ml-auto truncate pl-2 text-[11px] text-muted-foreground">{o.label}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function ConditionBuilder({ tree, onChange, options, readOnly }: {
  tree: CondTree;
  onChange: (next: CondTree) => void;
  options: VarOption[];
  readOnly?: boolean;
}) {
  const setGroup = (gi: number, g: CondGroup) => onChange({ ...tree, groups: tree.groups.map((x, i) => (i === gi ? g : x)) });
  const setRow = (gi: number, ri: number, patch: Partial<CondRow>) => {
    const g = tree.groups[gi];
    setGroup(gi, { ...g, rows: g.rows.map((r, i) => (i === ri ? { ...r, ...patch } : r)) });
  };

  return (
    <div className={cn("flex min-w-0 flex-col gap-2", readOnly && "pointer-events-none opacity-70")}>
      {tree.groups.map((g, gi) => (
        <div key={gi} className="flex min-w-0 flex-col gap-2">
          {gi > 0 ? (
            <div className="flex items-center gap-2">
              <Segmented value={tree.join} options={JOIN_OPTIONS} onChange={(join) => onChange({ ...tree, join })} />
              <span className="h-px flex-1 bg-border" />
            </div>
          ) : null}
          <div className="flex min-w-0 flex-col gap-2 rounded-lg border bg-muted/20 p-2.5">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">组内</span>
              <Segmented value={g.join} options={JOIN_OPTIONS} onChange={(join) => setGroup(gi, { ...g, join })} />
              <span className="flex-1" />
              {tree.groups.length > 1 ? (
                <Button type="button" size="icon-xs" variant="ghost" aria-label="删除这一组" onClick={() => onChange({ ...tree, groups: tree.groups.filter((_, i) => i !== gi) })}>
                  <Trash2 />
                </Button>
              ) : null}
            </div>
            {g.rows.map((r, ri) => {
              const kind = kindOf(options, r.variable);
              const ops = OPS[kind];
              const needsValue = kind !== "bool";
              return (
                <div key={ri} className="grid min-w-0 grid-cols-[minmax(0,1.4fr)_7rem_minmax(0,1fr)_auto] items-center gap-2">
                  <VariablePicker
                    options={options}
                    value={r.variable}
                    onChange={(v) => {
                      const k = kindOf(options, v);
                      const keep = OPS[k].some((o) => o.value === r.op);
                      setRow(gi, ri, { variable: v, op: keep ? r.op : OPS[k][0].value });
                    }}
                  />
                  <Select value={ops.some((o) => o.value === r.op) ? r.op : ops[0].value} onValueChange={(op) => setRow(gi, ri, { op })}>
                    <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {ops.map((o) => <SelectItem key={o.value} value={o.value} className="text-xs">{o.label}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  {needsValue ? (
                    <Input
                      className="h-8 font-mono text-xs"
                      inputMode={kind === "number" ? "decimal" : undefined}
                      value={r.value}
                      placeholder={kind === "number" ? "数值" : r.op === "in" || r.op === "not in" ? "多个值用逗号分隔" : "值"}
                      onChange={(e) => setRow(gi, ri, { value: e.target.value })}
                    />
                  ) : (
                    <span />
                  )}
                  <Button type="button" size="icon-sm" variant="ghost" aria-label="删除条件" onClick={() => setGroup(gi, { ...g, rows: g.rows.filter((_, i) => i !== ri) })}>
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              );
            })}
            <Button type="button" size="xs" variant="ghost" className="w-fit text-muted-foreground" onClick={() => setGroup(gi, { ...g, rows: [...g.rows, { variable: "", op: "true", value: "" }] })}>
              <Plus />
              添加条件
            </Button>
          </div>
        </div>
      ))}
      <Button type="button" size="xs" variant="outline" className="w-fit" onClick={() => onChange({ ...tree, groups: [...tree.groups, { join: "&&", rows: [{ variable: "", op: "true", value: "" }] }] })}>
        <Plus />
        添加一组
      </Button>
    </div>
  );
}
