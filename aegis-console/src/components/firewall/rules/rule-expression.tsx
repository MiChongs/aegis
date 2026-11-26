"use client";

import { Fragment, useMemo } from "react";
import { FIREWALL_RULE_VARIABLES, type FirewallRuleAction, type FirewallRuleInput } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";

/**
 * 自定义规则表达式（expr-lang）的轻量着色与参考资料。
 *
 * 着色只做词法：变量 / 函数 / 字符串 / 数字 / 运算符 / 字面量。不引 highlight.js 的语言包——
 * expr 不在它的内置语言里，而列表上每张卡片都要渲染一次，手写分词器更轻。
 */

export type ExprTokenKind = "space" | "string" | "number" | "variable" | "function" | "keyword" | "word-op" | "op" | "punct" | "ident";

export interface ExprToken {
  kind: ExprTokenKind;
  text: string;
}

const VARIABLE_NAMES = new Set(FIREWALL_RULE_VARIABLES.map((item) => item.name));
const WORD_OPERATORS = new Set(["startsWith", "endsWith", "contains", "matches", "in", "not", "and", "or"]);
const LITERALS = new Set(["true", "false", "nil"]);
const SYMBOL_OPERATORS = ["&&", "||", "==", "!=", ">=", "<=", "??", "?.", "..", "!", "<", ">", "+", "-", "*", "/", "%", "?", ":"];

export function tokenizeExpression(source: string): ExprToken[] {
  const tokens: ExprToken[] = [];
  let i = 0;
  const push = (kind: ExprTokenKind, text: string) => tokens.push({ kind, text });
  while (i < source.length) {
    const ch = source[i]!;
    if (/\s/.test(ch)) {
      let j = i + 1;
      while (j < source.length && /\s/.test(source[j]!)) j += 1;
      push("space", source.slice(i, j));
      i = j;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      let j = i + 1;
      while (j < source.length && source[j] !== ch) {
        if (source[j] === "\\" && ch !== "`") j += 1;
        j += 1;
      }
      push("string", source.slice(i, Math.min(j + 1, source.length)));
      i = j + 1;
      continue;
    }
    if (/[0-9]/.test(ch)) {
      let j = i + 1;
      while (j < source.length && /[0-9_.]/.test(source[j]!) && !(source[j] === "." && source[j + 1] === ".")) j += 1;
      push("number", source.slice(i, j));
      i = j;
      continue;
    }
    if (/[A-Za-z_$]/.test(ch)) {
      let j = i + 1;
      while (j < source.length && /[A-Za-z0-9_$]/.test(source[j]!)) j += 1;
      const word = source.slice(i, j);
      let k = j;
      while (k < source.length && source[k] === " ") k += 1;
      const prev = tokens.filter((token) => token.kind !== "space").at(-1);
      const isMember = prev?.text === "." || prev?.text === "?.";
      if (WORD_OPERATORS.has(word)) push("word-op", word);
      else if (LITERALS.has(word)) push("keyword", word);
      else if (source[k] === "(") push("function", word);
      else if (!isMember && VARIABLE_NAMES.has(word)) push("variable", word);
      else push("ident", word);
      i = j;
      continue;
    }
    const symbol = SYMBOL_OPERATORS.find((candidate) => source.startsWith(candidate, i));
    if (symbol) {
      push("op", symbol);
      i += symbol.length;
      continue;
    }
    push("punct", ch);
    i += 1;
  }
  return tokens;
}

const TOKEN_CLASS: Record<ExprTokenKind, string> = {
  space: "",
  string: "text-emerald-700 dark:text-emerald-400",
  number: "text-amber-700 dark:text-amber-400",
  variable: "text-sky-700 dark:text-sky-400 font-medium",
  function: "text-violet-700 dark:text-violet-400",
  keyword: "text-amber-700 dark:text-amber-400",
  "word-op": "text-rose-700 dark:text-rose-400",
  op: "text-rose-700 dark:text-rose-400",
  punct: "text-muted-foreground",
  ident: "text-foreground"
};

/** 着色后的表达式片段（不带外框）；编辑器底层与卡片共用 */
export function ExprTokens({ source }: { source: string }) {
  const tokens = useMemo(() => tokenizeExpression(source), [source]);
  return (
    <>
      {tokens.map((token, index) =>
        token.kind === "space" ? (
          <Fragment key={index}>{token.text}</Fragment>
        ) : (
          <span key={index} className={TOKEN_CLASS[token.kind]}>
            {token.text}
          </span>
        )
      )}
    </>
  );
}

/** 只读的表达式块 */
export function ExprCode({ source, className, clamp }: { source: string; className?: string; clamp?: boolean }) {
  return (
    <code
      className={cn(
        "block min-w-0 rounded-lg border border-border bg-muted/40 px-2.5 py-1.5 font-mono text-xs leading-5 break-all whitespace-pre-wrap",
        clamp && "line-clamp-3",
        className
      )}
    >
      {source ? <ExprTokens source={source} /> : <span className="text-muted-foreground">（空）</span>}
    </code>
  );
}

/* ── 参考资料 ── */

export const EXPR_FUNCTIONS: { snippet: string; label: string }[] = [
  { snippet: `cidr(ip, "10.0.0.0/8", "1.2.3.4")`, label: "IP 属于任一网段或地址" },
  { snippet: `lower(ua) contains "bot"`, label: "转小写后比较" },
  { snippet: `len(query) > 2048`, label: "长度" }
];

export const EXPR_OPERATORS: { snippet: string; label: string }[] = [
  { snippet: `path startsWith "/api/admin/"`, label: "前缀" },
  { snippet: `path endsWith ".php"`, label: "后缀" },
  { snippet: `ua contains "curl"`, label: "包含" },
  { snippet: `ua matches "(?i)sqlmap|nikto"`, label: "正则（RE2）" },
  { snippet: `method in ["PUT", "DELETE"]`, label: "属于列表" },
  { snippet: `&&`, label: "并且" },
  { snippet: `||`, label: "或者" },
  { snippet: `!`, label: "取反" },
  { snippet: `==`, label: "等于" },
  { snippet: `!=`, label: "不等于" }
];

export interface RuleTemplate {
  key: string;
  name: string;
  description: string;
  input: FirewallRuleInput;
}

function template(key: string, name: string, description: string, action: FirewallRuleAction, expression: string, extra?: Partial<FirewallRuleInput>): RuleTemplate {
  return { key, name, description, input: { name, description, expression, action, enabled: true, blockMode: "", banSeconds: 0, ...extra } };
}

export const RULE_TEMPLATES: RuleTemplate[] = [
  template(
    "scanner-ua",
    "拦截扫描器 UA",
    "常见漏洞扫描器的 User-Agent，命中后封禁 1 小时",
    "block",
    `ua matches "(?i)(sqlmap|nikto|nuclei|masscan|zgrab|dirbuster|gobuster|wpscan|acunetix)"`,
    { banSeconds: 3600 }
  ),
  template(
    "admin-night-write",
    "夜间禁止管理端写操作",
    "北京时间 0-6 点拒绝管理端的非 GET 请求",
    "block",
    `path startsWith "/api/admin/" && method != "GET" && hour < 6`
  ),
  template(
    "admin-country",
    "只允许指定国家访问管理端",
    "管理端只对国内来源开放",
    "block",
    `path startsWith "/api/admin/" && !(country in ["CN"])`
  ),
  template(
    "probe-allow",
    "放行监控探针",
    "内网探针访问健康检查时跳过其余检查",
    "allow",
    `cidr(ip, "10.0.0.0/8", "172.16.0.0/12") && path == "/healthz"`
  ),
  template(
    "gateway-plain",
    "拦截未加密的网关请求",
    "接入网关的 POST 必须走加密载荷",
    "block",
    `path startsWith "/api/v1/apps/" && !encrypted && method == "POST"`
  ),
  template("log-referer", "记录无 Referer 的登录请求", "只记事件，不影响请求", "log", `path endsWith "/login" && method == "POST" && referer == ""`)
];
