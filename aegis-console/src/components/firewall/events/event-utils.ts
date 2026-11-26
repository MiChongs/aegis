import type { FirewallEvent, FirewallExclusionInput, FirewallMatchedRule, FirewallSandboxInput } from "@/lib/api/firewall";

/** 阻断评估（入站 949110 / 出站 959100）与关联统计（980170）：只是「越线了」的结论，不是误报本身 */
export const EVALUATION_RULE_IDS = new Set([949110, 959100, 980170]);

export function isEvaluationRule(id: number) {
  return EVALUATION_RULE_IDS.has(id);
}

/** 真正计分的那几条（建例外时要排除的就是它们） */
export function scoringMatches(event: Pick<FirewallEvent, "matches">): FirewallMatchedRule[] {
  return (event.matches ?? []).filter((match) => !isEvaluationRule(match.id));
}

/**
 * 只有这三类变量能做到「只把这个字段拿出检查目标」；ARGS_GET / ARGS_POST 归并到 ARGS，
 * 例外拼出的是 `ctl:ruleRemoveTargetById=<id>;ARGS:json.x`，CRS 规则检查的正是 ARGS。
 */
const TARGET_PREFIXES: Array<[string, string]> = [
  ["ARGS:", "ARGS:"],
  ["ARGS_GET:", "ARGS:"],
  ["ARGS_POST:", "ARGS:"],
  ["REQUEST_HEADERS:", "REQUEST_HEADERS:"],
  ["REQUEST_COOKIES:", "REQUEST_COOKIES:"]
];

function normalizeTarget(target: string) {
  for (const [prefix, replacement] of TARGET_PREFIXES) {
    if (target.startsWith(prefix) && target.length > prefix.length) {
      return replacement + target.slice(prefix.length);
    }
  }
  return null;
}

/**
 * 「为这个误报建例外」的草稿：只针对这条路径（精确匹配）、只针对真正计分的规则；
 * 命中的字段能单独排除的只排除字段（规则对其余字段照常生效），一个都没有时整条移除规则。
 * 草稿进例外编辑器后还要管理员确认，并可在测试台带着它重放。
 */
export function buildExclusionDraft(event: FirewallEvent): FirewallExclusionInput | null {
  const matches = scoringMatches(event);
  if (matches.length === 0) return null;
  const ruleIds = [...new Set(matches.map((match) => match.id))];
  const targets = [
    ...new Set(
      matches
        .flatMap((match) => match.targets ?? [])
        .map(normalizeTarget)
        .filter((item): item is string => item !== null)
    )
  ];
  return {
    name: `#${ruleIds[0]} · ${event.path}`.slice(0, 120),
    note: `来自事件 ${event.requestId || `#${event.id}`}`,
    enabled: true,
    pathMatch: "exact",
    path: event.path,
    ruleIds,
    targets
  };
}

/** 「在测试台复现」：请求体出于隐私从不入库，复现时只有请求行与请求头 */
export function buildSandboxDraft(event: FirewallEvent): FirewallSandboxInput {
  return {
    method: event.method,
    url: event.path + (event.queryString ? `?${event.queryString}` : ""),
    headers: event.headers ?? {},
    ip: event.ip,
    transport: event.transport
  };
}

function safeDecode(value: string) {
  try {
    return decodeURIComponent(value.replace(/\+/g, " "));
  } catch {
    return value;
  }
}

/** 查询串拆成键值对并解码（解不开的原样保留） */
export function decodeQuery(query: string): Array<{ key: string; value: string }> {
  if (!query) return [];
  return query
    .split("&")
    .filter(Boolean)
    .map((pair) => {
      const index = pair.indexOf("=");
      if (index < 0) return { key: safeDecode(pair), value: "" };
      return { key: safeDecode(pair.slice(0, index)), value: safeDecode(pair.slice(index + 1)) };
    });
}
