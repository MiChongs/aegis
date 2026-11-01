import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 注册 IP 控制 API。
 *
 * 同一网络能注册多少账号、注册多快：按地址 / 网段 / 属地 / 整个应用分别配滑动窗口或累计配额，
 * 加上名单、惩罚期、突增检测、运营商与属地限制。判定全部在后端（`regip.Decide`），
 * 控制台只配置、查看、查询与模拟。规则见 docs/register-ip.md。
 */

export type RegIPMode = "enforce" | "observe";
export type RegIPAction = "reject" | "challenge" | "observe";
export type RegIPScope = "ip" | "subnet" | "city" | "app";
export type RegIPMethod = "password" | "sms" | "oauth" | "cardkey";
export type RegIPGeoMode = "off" | "allow" | "deny";
export type RegIPAllowReject = "allow" | "reject";
export type RegIPFailMode = "open" | "closed";
export type RegIPListKind = "allow" | "deny" | "elevated";
export type RegIPOutcome = "allow" | "challenge" | "reject";
export type RegIPReason =
  | ""
  | "disabled"
  | "missing_ip"
  | "list_allow"
  | "list_deny"
  | "penalty"
  | "geo"
  | "isp"
  | "surge"
  | "rule"
  | "failure";
export type RegIPEventKind = "rejected" | "challenged" | "challenge_passed" | "observed" | "released" | "penalized";

export type RegIPRule = {
  id: string;
  name: string;
  enabled: boolean;
  scope: RegIPScope;
  /** 0 表示累计（不限时间） */
  windowSeconds: number;
  limit: number;
  action: RegIPAction;
  /** 为空表示全部注册方式 */
  methods?: RegIPMethod[] | null;
};

export type RegIPPolicy = {
  enabled: boolean;
  mode: RegIPMode;
  network: { ipv4Subnet: number; ipv6Host: number; ipv6Subnet: number };
  rules: RegIPRule[];
  penalty: {
    enabled: boolean;
    strikes: number;
    strikeWindowSeconds: number;
    baseSeconds: number;
    multiplier: number;
    maxSeconds: number;
    scope: "ip" | "subnet";
  };
  surge: {
    enabled: boolean;
    windowMinutes: number;
    baselineMinutes: number;
    sensitivity: number;
    minRegistrations: number;
    action: RegIPAction;
    tightenFactor: number;
  };
  isp: { enabled: boolean; keywords: string[]; action: RegIPAction };
  geo: { mode: RegIPGeoMode; regions: string[]; action: RegIPAction; unknown: RegIPAllowReject };
  missingIp: RegIPAllowReject;
  failMode: RegIPFailMode;
  rejectMessage: string;
};

export type RegIPPolicyView = {
  policy: RegIPPolicy;
  /** false：还没用新策略保存过，当前值由旧开关「注册 IP 唯一」推导 */
  configured: boolean;
  defaults: RegIPPolicy;
};

export type RegIPLocation = {
  country?: string;
  province?: string;
  city?: string;
  isp?: string;
  resolved: boolean;
};

export type RegIPListMatch = {
  entryId: number;
  kind: RegIPListKind;
  cidr: string;
  factor?: number;
  note?: string;
};

export type RegIPSurgeState = {
  active: boolean;
  current: number;
  expected: number;
  threshold: number;
  score: number;
  series?: number[];
};

export type RegIPRuleHit = {
  ruleId: string;
  name: string;
  scope: RegIPScope;
  subject: string;
  windowSeconds: number;
  action: RegIPAction;
  count: number;
  limit: number;
  configured: number;
  exceeded: boolean;
  skipped?: boolean;
  skipReason?: string;
};

export type RegIPDecision = {
  outcome: RegIPOutcome;
  reason: RegIPReason;
  observed?: RegIPOutcome;
  ruleId?: string;
  ruleName?: string;
  detail?: string;
  message?: string;
  retryAfter?: number;
  rules: RegIPRuleHit[];
  factor?: number;
  tighten?: number;
  challengePassed?: boolean;
};

export type RegIPAddress = { ip: string; host: string; subnet: string; family: 4 | 6; private: boolean };

export type RegIPTopItem = { key: string; count: number; location?: string; lastAt: string };

export type RegIPPenalty = { subject: string; level: number; expiresAt: string; remaining: number };

export type RegIPWriterStats = { pending: number; written: number; dropped: number; failed: number };

export type RegIPTrendPoint = { date: string; registered: number } & Partial<Record<RegIPEventKind, number>>;

export type RegIPOverview = {
  policy: RegIPPolicy;
  configured: boolean;
  days: number;
  tz: string;
  totals: { registrations: number; distinctIps: number; distinctSubnets: number } & Partial<Record<RegIPEventKind, number>>;
  trend: RegIPTrendPoint[];
  reasons: Partial<Record<RegIPReason, number>>;
  methods: Record<string, number>;
  rules: Array<{ ruleId: string; ruleName: string; kind: RegIPEventKind; count: number }>;
  distribution: Array<{ bucket: string; count: number }>;
  topIps: RegIPTopItem[];
  topSubnets: RegIPTopItem[];
  topBlocked: RegIPTopItem[];
  topCities: RegIPTopItem[];
  lists: Partial<Record<RegIPListKind, number>>;
  penalties: RegIPPenalty[];
  penaltyCount: number;
  surge?: RegIPSurgeState | null;
  writers: Record<"events" | "ledger", RegIPWriterStats>;
};

export type RegIPEvent = {
  id: number;
  kind: RegIPEventKind;
  reason: RegIPReason;
  ruleId: string;
  ruleName: string;
  method: string;
  ip: string;
  subnet: string;
  account: string;
  deviceId: string;
  country: string;
  province: string;
  city: string;
  isp: string;
  count: number;
  limit: number;
  detail: string;
  extra?: Record<string, unknown>;
  operator?: string;
  createdAt: string;
};

export type RegIPRegistration = {
  id: number;
  userId: number | null;
  account: string;
  ip: string;
  method: string;
  country: string;
  province: string;
  city: string;
  isp: string;
  deviceId: string;
  releasedAt: string | null;
  createdAt: string;
};

export type RegIPListEntry = {
  id: number;
  cidr: string;
  kind: RegIPListKind;
  factor: number;
  note: string;
  expiresAt: string | null;
  hits: number;
  lastHitAt: string | null;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
};

export type RegIPPage<T> = { items: T[]; total: number; page: number; limit: number };

export type RegIPRuleUsage = RegIPRuleHit & { remaining: number; resetAt?: string | null };

export type RegIPInspection = {
  address: RegIPAddress;
  location: RegIPLocation;
  list: RegIPListMatch | null;
  lists: RegIPListEntry[];
  penalty: { subject: string; remaining: number; level: number; expiresAt: string | null };
  decision: RegIPDecision;
  usage: RegIPRuleUsage[];
  hostRegistrations: number;
  subnetRegistrations: number;
  registrations: RegIPRegistration[];
  subnetRecent: RegIPRegistration[];
  events: RegIPEvent[];
};

export type RegIPSimulateInput = {
  policy?: RegIPPolicy | null;
  ip: string;
  method?: RegIPMethod;
  captchaPassed?: boolean;
  counts?: Record<string, number>;
  location?: RegIPLocation | null;
  surgeActive?: boolean | null;
  penaltySeconds?: number | null;
};

export type RegIPSimulation = {
  decision: RegIPDecision;
  address?: RegIPAddress;
  location: RegIPLocation;
  list: RegIPListMatch | null;
  penaltySeconds: number;
  surge: RegIPSurgeState;
  draft: boolean;
  storeFailed: boolean;
};

export type RegIPReleaseInput = {
  ip: string;
  scope: "ip" | "subnet";
  counters?: boolean;
  penalty?: boolean;
  resetLevel?: boolean;
  ledger?: boolean;
  note?: string;
};

export type RegIPReleaseResult = {
  subjects: string[];
  countersCleared: number;
  penaltyCleared: boolean;
  ledgerReleased: number;
};

export type RegIPListInput = {
  values: string[];
  kind: RegIPListKind;
  factor?: number;
  note?: string;
  expiresAt?: string | null;
};

export type RegIPEventParams = {
  kind?: string;
  reason?: string;
  ruleId?: string;
  method?: string;
  ip?: string;
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type RegIPRegistrationParams = {
  ip?: string;
  method?: string;
  keyword?: string;
  released?: boolean;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type RegIPListParams = { kind?: string; keyword?: string; expired?: boolean; page?: number; limit?: number };

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/register-ip`;

export function getRegIPOverview(token: string, appKey: string, days: number, tz: string) {
  return apiRequest<RegIPOverview>(`${base(appKey)}${buildQuery({ days, tz })}`, { token });
}

export function getRegIPPolicy(token: string, appKey: string) {
  return apiRequest<RegIPPolicyView>(`${base(appKey)}/policy`, { token });
}

export function saveRegIPPolicy(token: string, appKey: string, policy: RegIPPolicy) {
  return apiRequest<RegIPPolicyView>(`${base(appKey)}/policy`, { method: "PUT", token, body: JSON.stringify(policy) });
}

export function simulateRegIP(token: string, appKey: string, input: RegIPSimulateInput) {
  return apiRequest<RegIPSimulation>(`${base(appKey)}/simulate`, { method: "POST", token, body: JSON.stringify(input) });
}

export function inspectRegIP(token: string, appKey: string, ip: string) {
  return apiRequest<RegIPInspection>(`${base(appKey)}/inspect${buildQuery({ ip })}`, { token });
}

export function releaseRegIP(token: string, appKey: string, input: RegIPReleaseInput) {
  return apiRequest<RegIPReleaseResult>(`${base(appKey)}/release`, { method: "POST", token, body: JSON.stringify(input) });
}

export function listRegIPPenalties(token: string, appKey: string, limit = 100) {
  return apiRequest<{ items: RegIPPenalty[]; total: number }>(`${base(appKey)}/penalties${buildQuery({ limit })}`, { token });
}

export function listRegIPEvents(token: string, appKey: string, params?: RegIPEventParams) {
  return apiRequest<RegIPPage<RegIPEvent>>(`${base(appKey)}/events${buildQuery(params ?? {})}`, { token });
}

export function listRegIPRegistrations(token: string, appKey: string, params?: RegIPRegistrationParams) {
  return apiRequest<RegIPPage<RegIPRegistration>>(`${base(appKey)}/registrations${buildQuery(params ?? {})}`, { token });
}

export function listRegIPLists(token: string, appKey: string, params?: RegIPListParams) {
  return apiRequest<RegIPPage<RegIPListEntry>>(`${base(appKey)}/lists${buildQuery(params ?? {})}`, { token });
}

export function addRegIPLists(token: string, appKey: string, input: RegIPListInput) {
  return apiRequest<{ items: RegIPListEntry[] }>(`${base(appKey)}/lists`, { method: "POST", token, body: JSON.stringify(input) });
}

export function updateRegIPList(
  token: string,
  appKey: string,
  id: number,
  input: { kind: RegIPListKind; factor?: number; note?: string; expiresAt?: string | null }
) {
  return apiRequest<RegIPListEntry>(`${base(appKey)}/lists/${id}`, { method: "PUT", token, body: JSON.stringify(input) });
}

export function deleteRegIPLists(token: string, appKey: string, ids: number[]) {
  return apiRequest<{ deleted: number }>(`${base(appKey)}/lists/delete`, { method: "POST", token, body: JSON.stringify({ ids }) });
}
