import type { TrafficConfig, TrafficRule, TrafficSettings } from "@/lib/api/traffic";
import {
  CLASS_LABEL,
  HEADER_OP_LABEL,
  MODE_LABEL,
  concurrencyPhrase,
  fmtDuration,
  fmtNum,
  limitPhrase,
  normalizeRuleShape,
  queuePhrase,
  warmupPhrase
} from "./traffic-format";

/**
 * 两份配置的结构化对比。方向是「当前 → 目标版本」，即回滚后会发生什么：
 * 目标版本独有的规则会被恢复，当前独有的会被移除，两边都有但内容不同的逐字段列出。
 */

export type FieldChange = { label: string; from: string; to: string };
export type RuleChange = { kind: "restore" | "remove" | "change"; id: string; name: string; changes: FieldChange[] };

const list = (values: string[] | null | undefined, empty = "无") => (values && values.length ? values.join("、") : empty);
const onOff = (value: boolean) => (value ? "开启" : "关闭");

const SETTINGS_FIELDS: Array<[string, (s: TrafficSettings) => string]> = [
  ["总开关", (s) => onOff(s.enabled)],
  ["管理端免检", (s) => onOff(s.exemptAdmin)],
  ["免检路径", (s) => list(s.exemptPaths)],
  ["集群协同", (s) => onOff(Boolean(s.cluster?.enabled))],
  ["跟踪键上限", (s) => fmtNum(s.maxTrackedKeys)],
  ["过载保护", (s) => onOff(s.overload.enabled)],
  ["CPU 阈值", (s) => `${s.overload.cpuThreshold}%`],
  ["在途上限", (s) => (s.overload.maxInflight > 0 ? fmtNum(s.overload.maxInflight) : "不限")],
  ["冷却时间", (s) => fmtDuration(s.overload.cooldownMs)],
  ["状态码", (s) => String(s.response.status)],
  ["业务码", (s) => String(s.response.code)],
  ["提示文案", (s) => s.response.message],
  ["重试间隔", (s) => `${s.response.retryAfterSeconds} 秒`]
];

const RULE_FIELDS: Array<[string, (r: TrafficRule) => string]> = [
  ["名称", (r) => r.name],
  ["说明", (r) => r.description || "无"],
  ["启用", (r) => onOff(r.enabled)],
  ["模式", (r) => MODE_LABEL[r.mode]],
  ["优先级", (r) => CLASS_LABEL[r.class]],
  ["命中即止", (r) => (r.terminal ? "是" : "否")],
  ["方法", (r) => list(r.match.methods, "全部")],
  ["路径", (r) => list(r.match.paths, "全部")],
  ["排除路径", (r) => list(r.match.excludePaths)],
  ["主机", (r) => list(r.match.hosts, "全部")],
  ["应用", (r) => list(r.match.appKeys, "全部")],
  ["网段", (r) => list(r.match.cidrs, "全部")],
  ["排除网段", (r) => list(r.match.excludeCidrs)],
  [
    "请求头",
    (r) =>
      list(
        r.match.headers.map((h) =>
          h.op === "exists" || h.op === "absent" ? `${h.name} ${HEADER_OP_LABEL[h.op]}` : `${h.name} ${HEADER_OP_LABEL[h.op]} ${h.value}`
        )
      )
  ],
  [
    "限速",
    (r) =>
      list(
        r.limits.map(
          (l) => `${limitPhrase(l)} 突发 ${fmtNum(l.burst)}${l.scope === "cluster" ? " 集群" : ""}${l.fallback === "skip" ? " 缺键不限" : ""}`
        )
      )
  ],
  ["并发", (r) => (r.concurrency ? concurrencyPhrase(r.concurrency) : "无")],
  ["排队", (r) => (r.queue ? queuePhrase(r.queue) : "无")],
  [
    "预热",
    (r) =>
      r.warmup
        ? `${warmupPhrase(r.warmup)} 每 ${fmtDuration(r.warmup.intervalMs)} 升 ${r.warmup.stepPct}% 冷却 ${r.warmup.cooldownSeconds} 秒`
        : "无"
  ],
  ["拒绝响应", (r) => (r.response ? `${r.response.status} ${r.response.code} ${r.response.message}` : "默认")]
];

function compareFields<T>(fields: Array<[string, (value: T) => string]>, from: T, to: T): FieldChange[] {
  const changes: FieldChange[] = [];
  for (const [label, read] of fields) {
    const a = read(from);
    const b = read(to);
    if (a !== b) changes.push({ label, from: a, to: b });
  }
  return changes;
}

export function diffConfigs(current: TrafficConfig, target: TrafficConfig) {
  const settings = compareFields(SETTINGS_FIELDS, current.settings, target.settings);

  const currentRules = (current.rules ?? []).map(normalizeRuleShape);
  const targetRules = (target.rules ?? []).map(normalizeRuleShape);
  const currentById = new Map(currentRules.map((rule) => [rule.id, rule]));
  const targetById = new Map(targetRules.map((rule) => [rule.id, rule]));

  const rules: RuleChange[] = [];
  for (const rule of targetRules) {
    const now = currentById.get(rule.id);
    if (!now) {
      rules.push({ kind: "restore", id: rule.id, name: rule.name, changes: [] });
      continue;
    }
    const changes = compareFields(RULE_FIELDS, now, rule);
    if (changes.length) rules.push({ kind: "change", id: rule.id, name: rule.name, changes });
  }
  for (const rule of currentRules) {
    if (!targetById.has(rule.id)) rules.push({ kind: "remove", id: rule.id, name: rule.name, changes: [] });
  }

  const shared = (ids: string[], other: Map<string, TrafficRule>) => ids.filter((id) => other.has(id));
  const currentOrder = shared(currentRules.map((rule) => rule.id), targetById);
  const targetOrder = shared(targetRules.map((rule) => rule.id), currentById);
  const orderChanged = currentOrder.join("\n") !== targetOrder.join("\n");
  const order: FieldChange | null = orderChanged
    ? {
        label: "顺序",
        from: currentOrder.map((id) => currentById.get(id)?.name ?? id).join("、"),
        to: targetOrder.map((id) => targetById.get(id)?.name ?? id).join("、")
      }
    : null;

  return { settings, rules, order, empty: settings.length === 0 && rules.length === 0 && !order };
}
