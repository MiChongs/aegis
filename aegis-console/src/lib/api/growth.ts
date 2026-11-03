import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 成长调控 API（管理端，docs/growth-control.md）。
 *
 * 一条规则 = 作用对象（某个用户 / 全应用）× 积分或经验 × 分数倍率 × 取整方式 × 每日上限 × 来源 × 生效时间。
 * 倍率用分数：×2 = 2/1，÷3 = 1/3，×1.5 = 3/2，冻结 = 0/1。同一类型下所有生效规则叠乘，每日上限取最严的。
 * 只作用于自动发放的收入；管理员调整、充值、会员赠送、折算、退回永远不受影响。
 */

export type GrowthKind = "integral" | "experience";
export type GrowthRounding = "floor" | "round" | "ceil" | "random";
export type GrowthRuleStatus = "active" | "scheduled" | "expired" | "paused" | "revoked";
export type GrowthEffect = "boost" | "penalty" | "freeze" | "neutral";

export type GrowthFactor = { num: number; den: number };

export type GrowthSourceSpec = {
  key: string;
  label: string;
  description: string;
  default: boolean;
  kinds: GrowthKind[];
};

export type GrowthRoundingSpec = { key: GrowthRounding; label: string; description: string };

export type GrowthPreset = { key: string; label: string; numerator: number; denominator: number; effect: GrowthEffect };

export type GrowthCatalog = {
  sources: GrowthSourceSpec[];
  roundings: GrowthRoundingSpec[];
  presets: GrowthPreset[];
  maxLevel: number;
  limits: { maxFactorTerm: number; maxDailyCap: number; maxActiveRules: number; maxReason: number; maxLabel: number };
};

export type GrowthRule = {
  id: number;
  appid: number;
  /** 0 = 全应用 */
  userId: number;
  account?: string;
  nickname?: string;
  kind: GrowthKind;
  numerator: number;
  denominator: number;
  rounding: GrowthRounding;
  dailyCap: number;
  sources: string[];
  startsAt?: string | null;
  endsAt?: string | null;
  enabled: boolean;
  label: string;
  reason: string;
  createdBy?: string;
  updatedBy?: string;
  revokedAt?: string | null;
  revokedBy?: string;
  createdAt: string;
  updatedAt: string;
  status: GrowthRuleStatus;
  effect: GrowthEffect;
};

export type GrowthRulePayload = {
  userId?: number;
  kind?: GrowthKind;
  numerator: number;
  denominator: number;
  rounding: GrowthRounding;
  dailyCap: number;
  sources: string[];
  startsAt?: string | null;
  endsAt?: string | null;
  enabled: boolean;
  label?: string;
  reason?: string;
};

export type GrowthRuleParams = {
  userId?: number;
  scope?: "all" | "app" | "user";
  kind?: GrowthKind | "";
  status?: GrowthRuleStatus | "live" | "";
  effect?: GrowthEffect | "";
  keyword?: string;
  page?: number;
  limit?: number;
};

export type GrowthPage<T> = { items: T[]; total: number; page: number; limit: number };

export type GrowthEvent = {
  id: number;
  appid: number;
  userId: number;
  ruleId?: number;
  action: "rule_create" | "rule_update" | "rule_revoke" | "rule_delete" | "level_cap" | "level_uncap" | string;
  summary: string;
  detail: Record<string, unknown>;
  operator?: string;
  note?: string;
  createdAt: string;
};

export type GrowthUsage = {
  day: string;
  kind: GrowthKind;
  base: number;
  granted: number;
  suppressed: number;
  events: number;
};

export type GrowthSourceStat = { source: string; base: number; granted: number; count: number; touched: number };

export type GrowthSourceEffect = {
  source: string;
  label: string;
  factor: GrowthFactor;
  display: string;
  rounding: GrowthRounding;
  dailyCap: number;
  ruleIds: number[];
};

export type GrowthKindView = {
  kind: GrowthKind;
  factor: GrowthFactor;
  label: string;
  rounding: GrowthRounding;
  dailyCap: number;
  usedToday: number;
  bySource: GrowthSourceEffect[];
  stats: GrowthSourceStat[];
  liveRules: number;
  restricted: boolean;
  boosted: boolean;
};

export type GrowthLevelState = {
  level: number;
  locked: number;
  capped: number;
  highest: number;
  maxLevel: number;
  experience: number;
  /** 只按经验（不看封顶）应到的等级 */
  eligible: number;
};

export type GrowthProfile = {
  userId: number;
  integral: GrowthKindView;
  experience: GrowthKindView;
  rules: GrowthRule[];
  appRules: GrowthRule[];
  level: GrowthLevelState;
  usage: GrowthUsage[];
  events: GrowthEvent[];
  statsDays: number;
  computedAt: string;
};

export type GrowthOutcome = {
  base: number;
  scaled: number;
  granted: number;
  num: number;
  den: number;
  rounding: GrowthRounding;
  dailyCap?: number;
  usedToday?: number;
  capped?: boolean;
  ruleIds: number[];
  source: string;
};

export type GrowthPreview = {
  source: string;
  base: number;
  before: GrowthOutcome;
  after: GrowthOutcome;
  expectedAfter: number;
  usedToday: number;
};

export type GrowthPreviewPayload = {
  userId?: number;
  kind: GrowthKind;
  source: string;
  base: number;
  draft?: GrowthRulePayload;
  excludeRuleId?: number;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/growth`;
const userBase = (appKey: string, userId: number) => `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/growth`;

export function getGrowthCatalog(token: string, appKey: string) {
  return apiRequest<GrowthCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getUserGrowth(token: string, appKey: string, userId: number) {
  return apiRequest<GrowthProfile>(userBase(appKey, userId), { token });
}

export function listGrowthRules(token: string, appKey: string, params: GrowthRuleParams = {}) {
  return apiRequest<GrowthPage<GrowthRule>>(`${base(appKey)}/rules${buildQuery(params)}`, { token });
}

export function createGrowthRule(token: string, appKey: string, payload: GrowthRulePayload) {
  return apiRequest<GrowthRule>(`${base(appKey)}/rules`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateGrowthRule(token: string, appKey: string, ruleId: number, payload: GrowthRulePayload) {
  return apiRequest<GrowthRule>(`${base(appKey)}/rules/${ruleId}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function revokeGrowthRule(token: string, appKey: string, ruleId: number, note?: string) {
  return apiRequest<GrowthRule>(`${base(appKey)}/rules/${ruleId}/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify({ note: note || undefined })
  });
}

export function deleteGrowthRule(token: string, appKey: string, ruleId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/rules/${ruleId}`, { method: "DELETE", token });
}

export function previewGrowth(token: string, appKey: string, payload: GrowthPreviewPayload) {
  return apiRequest<GrowthPreview>(`${base(appKey)}/preview`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function listGrowthEvents(token: string, appKey: string, params: { userId?: number; page?: number; limit?: number } = {}) {
  return apiRequest<GrowthPage<GrowthEvent>>(`${base(appKey)}/events${buildQuery(params)}`, { token });
}

export function setUserLevelCap(token: string, appKey: string, userId: number, payload: { level: number; demote: boolean; note?: string }) {
  return apiRequest<GrowthLevelState>(`${userBase(appKey, userId)}/level-cap`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function clearUserLevelCap(token: string, appKey: string, userId: number, note?: string) {
  return apiRequest<GrowthLevelState>(`${userBase(appKey, userId)}/level-cap${buildQuery({ note })}`, { method: "DELETE", token });
}

// ── 展示 ──

export function factorLabel(num: number, den: number) {
  if (den <= 0) return "×1";
  if (num === 0) return "冻结";
  if (num % den === 0) return `×${num / den}`;
  if (num === 1) return `÷${den}`;
  const value = num / den;
  // ×1.5、×0.75 这种有限小数直接写小数，其余写分数
  const text = Number(value.toFixed(4));
  return Math.abs(text - value) < 1e-9 ? `×${text}` : `×${num}/${den}`;
}

export function factorValue(num: number, den: number) {
  return den > 0 ? num / den : 1;
}
