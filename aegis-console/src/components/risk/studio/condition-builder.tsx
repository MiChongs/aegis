"use client";

import { Fragment, type ReactNode } from "react";
import { ChevronsUpDown, Plus, Trash2 } from "lucide-react";

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
 * 同一份结构也能读成中文：「设备 1 天出现的账号数不少于 3，且请求来自代理为是」。
 *
 * 只覆盖构建器自己能生成的那几种写法；反解失败（手写的复杂表达式）时调用方退回表达式编辑，
 * 读法退回逐词翻译（describeExpr 的 loose 结果）。
 */

/** flag：取值只有 0 / 1 的派生特征，按「为是 / 为否」读写（写成 F.x == 1） */
export type VarKind = "bool" | "number" | "string" | "list" | "flag";

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
const GROUP_LABEL: Record<string, string> = { 请求: "请求", device: "设备", client: "客户端", net: "网络", list: "名单" };

/** 派生特征的表达式是不是一个判断（结果只有 0 / 1） */
function isFlagExpr(expr?: string): boolean {
  const s = (expr ?? "").trim();
  if (!s || /[?+*/%]|(^|[^\w])-/.test(s)) return false;
  return /\|\||&&|==|!=|>=|<=|[<>]|^!|\bin\b|\bmatches\b|\bcontains\b/.test(s) || /^[A-Za-z_][\w.]*$/.test(s);
}

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
    out.push({ name: `extra.${f.key}`, label: f.label || f.key, kind: f.type === "number" ? "number" : f.type === "bool" ? "bool" : f.type === "list" ? "list" : "string", group: "业务入参" });
  }
  for (const f of features) {
    const flag = f.kind === "derived" && isFlagExpr(f.expr);
    out.push({ name: `F.${f.key}`, label: f.name || f.key, kind: flag ? "flag" : "number", group: f.kind === "derived" ? "派生特征" : "特征" });
  }
  for (const m of models) out.push({ name: `M.${m.key}`, label: m.name || m.key, kind: "number", group: "模型分" });
  return out;
}

/** 同一份目录喂给 Monaco 补全 */
export function completionsOf(options: VarOption[]): CompletionEntry[] {
  return options.map((o) => ({
    label: o.name,
    detail: o.label,
    kind: o.name.startsWith("F.") ? "feature" : o.name.startsWith("M.") ? "model" : "variable",
  }));
}

const OPS: Record<VarKind, Array<{ value: string; label: string }>> = {
  bool: [{ value: "true", label: "为是" }, { value: "false", label: "为否" }],
  flag: [{ value: "true", label: "为是" }, { value: "false", label: "为否" }],
  number: [
    { value: ">=", label: "不少于" }, { value: ">", label: "超过" }, { value: "<=", label: "不超过" }, { value: "<", label: "少于" },
    { value: "==", label: "等于" }, { value: "!=", label: "不等于" },
  ],
  string: [
    { value: "==", label: "是" }, { value: "!=", label: "不是" }, { value: "in", label: "属于其中之一" }, { value: "not in", label: "不属于其中任何一个" },
    { value: "contains", label: "包含" }, { value: "startsWith", label: "开头是" }, { value: "endsWith", label: "结尾是" }, { value: "matches", label: "符合正则" },
  ],
  list: [{ value: "has", label: "包含" }, { value: "not_has", label: "不包含" }],
};

const quote = (s: string) => JSON.stringify(s);
const splitValues = (s: string) => s.split(/[,，、]/).map((x) => x.trim()).filter(Boolean);

function rowExpr(row: CondRow, kind: VarKind): string {
  const v = row.variable;
  switch (kind) {
    case "bool":
      return row.op === "false" ? `!${v}` : v;
    case "flag":
      return row.op === "false" ? `${v} == 0` : `${v} == 1`;
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

/** 0 / 1 特征的比较改写成「为是 / 为否」；写法对不上就交还表达式编辑 */
function normalizeFlags(tree: CondTree, options: VarOption[]): CondTree | null {
  let ok = true;
  const groups = tree.groups.map((g) => ({
    ...g,
    rows: g.rows.map((r) => {
      if (kindOf(options, r.variable) !== "flag") return r;
      const v = r.value.trim();
      if ((r.op === "==" && v === "1") || (r.op === ">=" && v === "1") || (r.op === ">" && v === "0") || (r.op === "!=" && v === "0")) return { ...r, op: "true", value: "" };
      if ((r.op === "==" && v === "0") || (r.op === "<" && v === "1") || (r.op === "!=" && v === "1")) return { ...r, op: "false", value: "" };
      if (r.op === "true" || r.op === "false") return r;
      ok = false;
      return r;
    }),
  }));
  return ok ? { ...tree, groups } : null;
}

/** expr → 构建器状态；超出构建器能表达的范围时返回 null。给 options 时把 0 / 1 特征读成「为是 / 为否」 */
export function exprToTree(expr: string, options?: VarOption[]): CondTree | null {
  const tree = parseTree(expr);
  if (!tree || !options) return tree;
  return normalizeFlags(tree, options);
}

function parseTree(expr: string): CondTree | null {
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

/* ── 读成中文 ── */

/** 一句话的片段：变量（名称）、运算（说法）、取值、连接词 */
export type Token = { t: "var" | "op" | "value" | "join" | "text"; s: string };

/** 变量的中文名；目录里找不到时给一个不带内部键的说法 */
export function varLabel(options: VarOption[], name: string): string {
  const found = options.find((o) => o.name === name);
  if (found) return found.label;
  if (name.startsWith("F.")) return "已删除的特征";
  if (name.startsWith("M.")) return "已删除的模型";
  if (name.startsWith("extra.")) return `业务入参 ${name.slice(6)}`;
  return name;
}

function rowTokens(row: CondRow, options: VarOption[]): Token[] {
  const kind = kindOf(options, row.variable);
  const v: Token = { t: "var", s: varLabel(options, row.variable) };
  const val = row.value.trim();
  switch (kind) {
    case "bool":
    case "flag":
      return [v, { t: "op", s: row.op === "false" ? "为否" : "为是" }];
    case "number":
      return [v, { t: "op", s: OPS.number.find((o) => o.value === row.op)?.label ?? row.op }, { t: "value", s: val || "0" }];
    case "list":
      return [v, { t: "op", s: row.op === "not_has" ? "不包含" : "包含" }, { t: "value", s: val || "空" }];
    default: {
      if (row.op === "in" || row.op === "not in") {
        return [v, { t: "op", s: row.op === "in" ? "属于" : "不属于" }, { t: "value", s: splitValues(row.value).join("、") || "空" }, { t: "op", s: "其中之一" }];
      }
      if (row.op === "startsWith") return [v, { t: "op", s: "以" }, { t: "value", s: val || "空" }, { t: "op", s: "开头" }];
      if (row.op === "endsWith") return [v, { t: "op", s: "以" }, { t: "value", s: val || "空" }, { t: "op", s: "结尾" }];
      const word = row.op === "==" ? "是" : row.op === "!=" ? "不是" : row.op === "contains" ? "包含" : row.op === "matches" ? "符合正则" : row.op;
      return [v, { t: "op", s: word }, { t: "value", s: row.value === "" ? "空" : row.value }];
    }
  }
}

/** 构建器状态 → 一句话的片段（不含「当……时」） */
export function describeTree(tree: CondTree, options: VarOption[]): Token[] {
  const groups = tree.groups.map((g) => g.rows.filter((r) => r.variable)).map((rows, gi) => ({ rows, join: tree.groups[gi].join })).filter((g) => g.rows.length);
  const out: Token[] = [];
  groups.forEach((g, gi) => {
    if (gi > 0) out.push({ t: "join", s: tree.join === "||" ? "，或者" : "，并且" });
    const wrap = groups.length > 1 && g.rows.length > 1;
    if (wrap) out.push({ t: "text", s: "（" });
    g.rows.forEach((r, ri) => {
      if (ri > 0) out.push({ t: "join", s: g.join === "||" ? "或" : "且" });
      out.push(...rowTokens(r, options));
    });
    if (wrap) out.push({ t: "text", s: "）" });
  });
  return out;
}

const LOOSE_OPS: Record<string, string> = {
  "&&": "且", "||": "或", "!": "非", "==": "等于", "!=": "不等于", ">=": "不少于", "<=": "不超过", ">": "超过", "<": "少于",
  and: "且", or: "或", not: "非", in: "属于", matches: "符合正则", contains: "包含", startsWith: "开头是", endsWith: "结尾是",
  "+": "加", "-": "减", "*": "乘", "/": "除以", "%": "取余", "?": "则", ":": "否则",
  len: "长度", lower: "小写", upper: "大写", abs: "绝对值", max: "最大值", min: "最小值", count: "计数", any: "任一", all: "全部", none: "均不",
};

/** 逐词翻译：变量换成中文名，运算换成说法。手写的复杂表达式用它兜底 */
function looseTokens(expr: string, options: VarOption[]): Token[] {
  const re = /("(?:[^"\\]|\\.)*"|'[^']*')|(\d+(?:\.\d+)?)|(&&|\|\||==|!=|>=|<=|[<>!+\-*/%?:])|([A-Za-z_][\w.]*)|([()[\],])|(\s+)/g;
  const out: Token[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(expr))) {
    if (m[1]) out.push({ t: "value", s: parseStr(m[1]) ?? m[1] });
    else if (m[2]) out.push({ t: "value", s: m[2] });
    else if (m[3]) out.push({ t: m[3] === "&&" || m[3] === "||" ? "join" : "op", s: LOOSE_OPS[m[3]] ?? m[3] });
    else if (m[4]) {
      const w = m[4];
      if (w === "true") out.push({ t: "value", s: "是" });
      else if (w === "false") out.push({ t: "value", s: "否" });
      else if (w === "nil") out.push({ t: "value", s: "空" });
      else if (LOOSE_OPS[w] && !w.includes(".")) out.push({ t: w === "and" || w === "or" ? "join" : "op", s: LOOSE_OPS[w] });
      else out.push({ t: "var", s: varLabel(options, w) });
    } else if (m[5]) out.push({ t: "text", s: m[5] === "(" ? "（" : m[5] === ")" ? "）" : m[5] === "," ? "、" : m[5] });
  }
  return out;
}

/** 表达式 → 一句话的片段；loose 为真表示是逐词翻译 */
export function describeExpr(expr: string, options: VarOption[]): { tokens: Token[]; loose: boolean } {
  const tree = exprToTree(expr, options);
  if (tree) return { tokens: describeTree(tree, options), loose: false };
  return { tokens: looseTokens(expr, options), loose: true };
}

const ascii = (s: string) => /[A-Za-z0-9]$/.test(s);
const asciiStart = (s: string) => /^[A-Za-z0-9]/.test(s);

/** 片段 → 纯文本：变量加书名号，中英文之间留空格 */
export function tokensText(tokens: Token[]): string {
  let out = "";
  for (const tk of tokens) {
    const s = tk.t === "var" ? `「${tk.s}」` : tk.s;
    if (out && tk.t !== "text" && !out.endsWith("（") && !s.startsWith("，") && (ascii(out) || asciiStart(s))) out += " ";
    out += s;
  }
  return out;
}

/** 一句话的排版：变量是浅色标签，取值加粗 */
export function SentenceView({ tokens, prefix, suffix, className, empty = "尚未设置条件" }: {
  tokens: Token[];
  prefix?: string;
  suffix?: ReactNode;
  className?: string;
  empty?: string;
}) {
  if (!tokens.length) return <span className={cn("text-muted-foreground", className)}>{empty}</span>;
  return (
    <span className={cn("leading-7 [overflow-wrap:anywhere]", className)}>
      {prefix ? <span>{prefix}</span> : null}
      {tokens.map((tk, i) => {
        const space = i > 0 || prefix ? " " : "";
        return (
          <Fragment key={i}>
            {tk.t === "text" || tk.s.startsWith("，") ? null : space}
            {tk.t === "var" ? (
              <span className="rounded-md bg-sky-500/10 px-1.5 py-0.5 font-medium text-sky-800 dark:bg-sky-400/15 dark:text-sky-200">{tk.s}</span>
            ) : tk.t === "value" ? (
              <span className="font-semibold tabular-nums">{tk.s}</span>
            ) : tk.t === "join" ? (
              <span className="text-muted-foreground">{tk.s}</span>
            ) : (
              <span>{tk.s}</span>
            )}
          </Fragment>
        );
      })}
      {suffix}
    </span>
  );
}

/* ── 界面 ── */

const ROW_JOIN = [{ value: "&&" as Join, label: "全部" }, { value: "||" as Join, label: "任一" }];
const GROUP_JOIN = [{ value: "&&" as Join, label: "并且" }, { value: "||" as Join, label: "或者" }];

function VariablePicker({ options, value, onChange, disabled }: { options: VarOption[]; value: string; onChange: (v: string) => void; disabled?: boolean }) {
  const current = options.find((o) => o.name === value);
  const groups = new Map<string, VarOption[]>();
  for (const o of options) groups.set(o.group, [...(groups.get(o.group) ?? []), o]);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="sm" disabled={disabled} className="h-9 w-full min-w-0 justify-between gap-2 font-normal">
          {value ? (
            <span className="flex min-w-0 items-baseline gap-2">
              <span className="truncate text-sm">{current?.label ?? varLabel(options, value)}</span>
              {current ? <span className="shrink-0 text-[11px] text-muted-foreground">{current.group}</span> : null}
            </span>
          ) : (
            <span className="text-sm text-muted-foreground">选择判断对象</span>
          )}
          <ChevronsUpDown className="size-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 p-0">
        <Command>
          <CommandInput placeholder="搜索请求信息、特征、模型" />
          <CommandList className="max-h-80">
            <CommandEmpty>没有匹配的项目</CommandEmpty>
            {[...groups.entries()].map(([g, items]) => (
              <CommandGroup key={g} heading={g}>
                {items.map((o) => (
                  <CommandItem key={o.name} value={`${o.label} ${o.name}`} onSelect={() => onChange(o.name)}>
                    <span className="min-w-0 flex-1 truncate">{o.label}</span>
                    <span className="shrink-0 text-[11px] text-muted-foreground">
                      {o.kind === "bool" || o.kind === "flag" ? "是否" : o.kind === "number" ? "数值" : o.kind === "list" ? "列表" : "文本"}
                    </span>
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
  const single = tree.groups.length === 1;

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {tree.groups.map((g, gi) => (
        <div key={gi} className="flex min-w-0 flex-col gap-3">
          {gi > 0 ? (
            <div className="flex items-center gap-3">
              <span className="h-px flex-1 bg-border" />
              {readOnly ? (
                <span className="text-xs text-muted-foreground">{tree.join === "||" ? "或者" : "并且"}</span>
              ) : (
                <Segmented value={tree.join} options={GROUP_JOIN} onChange={(join) => onChange({ ...tree, join })} ariaLabel="组之间的关系" />
              )}
              <span className="h-px flex-1 bg-border" />
            </div>
          ) : null}
          <div className="flex min-w-0 flex-col gap-2.5 rounded-lg border bg-muted/30 p-3">
            <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span>{single ? "满足以下" : `第 ${gi + 1} 组：满足以下`}</span>
              {readOnly ? (
                <span className="font-medium text-foreground">{g.join === "||" ? "任一" : "全部"}</span>
              ) : (
                <Segmented value={g.join} options={ROW_JOIN} onChange={(join) => setGroup(gi, { ...g, join })} ariaLabel="组内条件的关系" />
              )}
              <span>条件</span>
              <span className="flex-1" />
              {!readOnly && tree.groups.length > 1 ? (
                <Button type="button" size="xs" variant="ghost" onClick={() => onChange({ ...tree, groups: tree.groups.filter((_, i) => i !== gi) })}>
                  <Trash2 />
                  删除这一组
                </Button>
              ) : null}
            </div>
            {g.rows.length === 0 ? <p className="py-1 text-xs text-muted-foreground">还没有条件</p> : null}
            {g.rows.map((r, ri) => {
              const kind = kindOf(options, r.variable);
              const ops = OPS[kind];
              const needsValue = kind !== "bool" && kind !== "flag";
              return (
                <div key={ri} className="grid min-w-0 grid-cols-1 items-center gap-2 sm:grid-cols-[minmax(0,1.5fr)_9rem_minmax(0,1fr)_auto]">
                  <VariablePicker
                    options={options}
                    value={r.variable}
                    disabled={readOnly}
                    onChange={(v) => {
                      const k = kindOf(options, v);
                      const keep = OPS[k].some((o) => o.value === r.op);
                      setRow(gi, ri, { variable: v, op: keep ? r.op : OPS[k][0].value });
                    }}
                  />
                  <Select disabled={readOnly} value={ops.some((o) => o.value === r.op) ? r.op : ops[0].value} onValueChange={(op) => setRow(gi, ri, { op })}>
                    <SelectTrigger className="h-9 w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {ops.map((o) => <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  {needsValue ? (
                    <Input
                      disabled={readOnly}
                      className={cn("h-9", kind === "number" && "tabular-nums")}
                      inputMode={kind === "number" ? "decimal" : undefined}
                      value={r.value}
                      aria-invalid={kind === "number" && r.value.trim() !== "" && !Number.isFinite(Number(r.value)) ? true : undefined}
                      placeholder={kind === "number" ? "数值" : r.op === "in" || r.op === "not in" ? "多个值用顿号或逗号分隔" : "取值"}
                      onChange={(e) => setRow(gi, ri, { value: e.target.value })}
                    />
                  ) : (
                    <span className="hidden sm:block" />
                  )}
                  {!readOnly ? (
                    <Button type="button" size="icon-sm" variant="ghost" aria-label="删除条件" onClick={() => setGroup(gi, { ...g, rows: g.rows.filter((_, i) => i !== ri) })}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  ) : <span />}
                </div>
              );
            })}
            {!readOnly ? (
              <Button type="button" size="xs" variant="ghost" className="w-fit text-muted-foreground" onClick={() => setGroup(gi, { ...g, rows: [...g.rows, { variable: "", op: "true", value: "" }] })}>
                <Plus />
                添加条件
              </Button>
            ) : null}
          </div>
        </div>
      ))}
      {!readOnly ? (
        <Button type="button" size="xs" variant="outline" className="w-fit" onClick={() => onChange({ ...tree, groups: [...tree.groups, { join: "&&", rows: [{ variable: "", op: "true", value: "" }] }] })}>
          <Plus />
          添加一组条件
        </Button>
      ) : null}
    </div>
  );
}
