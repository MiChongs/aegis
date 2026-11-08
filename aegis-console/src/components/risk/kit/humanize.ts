/**
 * 风控控制台的统一文案层：把接口里的字段拼成「谁、在哪、结果、为什么」这样的句子。
 *
 * 规则：文案正式简洁，不用「·」分隔（分隔靠排版）；不出现内部字段名、规则键、接口名；
 * 数值带单位与语义（「与 6 个账号共用设备」而不是「g_device_accounts=6」）。各页面只从这里取文案。
 */
import type { DecisionRow, ListEntry, ListKind, ListType, RiskAction, RiskCase, RiskLevel } from "@/lib/api/risk";
import { formatDateTime, formatRelative } from "@/lib/datetime";

import { EVENT_LABEL, KIND_LABEL, LIST_TYPE_LABEL } from "./labels";

/** 处置结果的说法：「已拦截」「转人工复核」 */
export const ACTION_RESULT: Record<RiskAction, string> = {
  pass: "已通过",
  review: "转人工复核",
  captcha: "要求验证码",
  block: "已拦截",
  ban: "已拦截并封禁",
};

/** 处置的一句解释（用在说明、空状态、图例的悬停里） */
export const ACTION_EXPLAIN: Record<RiskAction, string> = {
  pass: "请求正常放行",
  review: "请求放行，同时生成复核单等待人工判断",
  captcha: "完成图形验证码后才能继续",
  block: "本次请求被拒绝",
  ban: "本次请求被拒绝，账号或设备同时被处置",
};

/** 风险分的说法：0–29 低、30–59 中、60–79 高、80 以上极高 */
export function scoreWord(score: number): { label: string; tone: "success" | "info" | "warning" | "danger" } {
  if (score >= 80) return { label: "极高风险", tone: "danger" };
  if (score >= 60) return { label: "高风险", tone: "warning" };
  if (score >= 30) return { label: "中等风险", tone: "info" };
  return { label: "低风险", tone: "success" };
}

export const LEVEL_WORD: Record<RiskLevel, string> = { none: "无风险", low: "低风险", medium: "中等风险", high: "高风险", critical: "极高风险" };

/** 请求的主体：账号名优先，其次用户编号，再次 IP */
export function subjectOf(row: Pick<DecisionRow, "account" | "userId" | "ip">): string {
  if (row.account) return row.account;
  if (row.userId) return `用户 #${row.userId}`;
  if (row.ip) return row.ip;
  return "匿名访客";
}

/** 一条决策的标题：「chuanyi 的抽奖请求已拦截」 */
export function decisionHeadline(row: DecisionRow, sceneName: (k: string) => string): string {
  return `${subjectOf(row)} 的${sceneName(row.scene)}请求${ACTION_RESULT[row.action] ?? row.action}`;
}

/** 一条决策的原因：命中规则给出的理由，没有时说明为何是这个结论 */
export function decisionReason(row: Pick<DecisionRow, "reasons" | "action" | "hits">, max = 2): string {
  const reasons = (row.reasons ?? []).filter(Boolean);
  if (reasons.length) {
    const head = reasons.slice(0, max).join("；");
    return reasons.length > max ? `${head}，另有 ${reasons.length - max} 条` : head;
  }
  if (row.action === "pass") return (row.hits ?? []).length ? "命中的规则均在观察中，未影响结果" : "未命中风险规则";
  return "由场景策略判定";
}

/** 请求来自哪里：「中国 广州」「HK」 */
export function placeOf(row: Pick<DecisionRow, "country" | "city">): string {
  return [row.country, row.city].filter(Boolean).join(" ");
}

/** 「3 分钟前」，悬停给完整时间 */
export function when(iso: string | null | undefined): { relative: string; absolute: string } {
  if (!iso) return { relative: "", absolute: "" };
  return { relative: formatRelative(iso), absolute: formatDateTime(iso) };
}

/** 名单有效期：「长期有效」「还剩 18 小时」「已过期」 */
export function remaining(expiresAt?: string | null): { text: string; expired: boolean } {
  if (!expiresAt) return { text: "长期有效", expired: false };
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (!Number.isFinite(ms)) return { text: "长期有效", expired: false };
  if (ms <= 0) return { text: "已过期", expired: true };
  const m = Math.ceil(ms / 60000);
  if (m < 60) return { text: `还剩 ${m} 分钟`, expired: false };
  const h = Math.round(m / 60);
  if (h < 48) return { text: `还剩 ${h} 小时`, expired: false };
  return { text: `还剩 ${Math.round(h / 24)} 天`, expired: false };
}

/** 名单条目的一句话：「这台设备在黑名单中」 */
export function listEntrySentence(e: Pick<ListEntry, "type" | "kind">): string {
  const what = LIST_TYPE_SUBJECT[e.type] ?? LIST_TYPE_LABEL[e.type] ?? e.type;
  return `${what}在${KIND_LABEL[e.kind as ListKind] ?? e.kind}中`;
}

const LIST_TYPE_SUBJECT: Partial<Record<ListType, string>> = {
  ip: "这个 IP", cidr: "这个网段", device: "这台设备", account: "这个登录名", user: "这位用户", email_domain: "这个邮箱域名",
  phone_prefix: "这个号段", asn: "这个运营商网络", country: "这个国家或地区",
};

/** 名单作用范围：「全部应用」「应用 #10000」 */
export function scopeText(appId?: number | null, appName?: (id: number) => string | undefined): string {
  if (!appId) return "全部应用";
  return appName?.(appId) ?? `应用 #${appId}`;
}

/** 复核单的状态 */
export function caseStatus(c: Pick<RiskCase, "verdict">): { label: string; tone: "warning" | "success" | "danger" | "secondary" } {
  switch (c.verdict) {
    // 与后端一致：approve = 放行（没问题），reject = 确认有风险
    case "approve":
      return { label: "已判定为正常", tone: "success" };
    case "reject":
      return { label: "已确认有风险", tone: "danger" };
    default:
      return { label: "待复核", tone: "warning" };
  }
}

/** 事件的名称 */
export function eventName(type: string): string {
  return EVENT_LABEL[type] ?? type;
}

/** 数量带单位：「1.2 万次」 */
export function countText(n: number, unit = "次"): string {
  if (n >= 1e8) return `${(n / 1e8).toFixed(1)} 亿${unit}`;
  if (n >= 1e4) return `${(n / 1e4).toFixed(n >= 1e5 ? 0 : 1)} 万${unit}`;
  return `${n.toLocaleString("zh-CN")} ${unit}`;
}

/** 占比：「3.2%」，极小时保留两位 */
export function percent(part: number, total: number): string {
  if (!total) return "0%";
  const v = (part / total) * 100;
  return `${v >= 10 ? v.toFixed(0) : v >= 1 ? v.toFixed(1) : v.toFixed(2)}%`;
}

/** 耗时：微秒 → 「1.8 毫秒」「230 微秒」 */
export function latencyText(us: number): string {
  if (us >= 1000) return `${(us / 1000).toFixed(us >= 10000 ? 0 : 1)} 毫秒`;
  return `${Math.round(us)} 微秒`;
}
