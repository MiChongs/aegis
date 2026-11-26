import { apiRequest, buildQuery } from "./client";
import type { IPBanMode } from "./types";

/**
 * 防火墙工作台（规则集 v2，docs/firewall.md）管理端 API。
 *
 * 类型逐字段对齐后端：
 * - 事件 / 统计：internal/domain/firewall/types.go（FirewallLog、FirewallStats、MatchedRule）
 * - 设置：internal/domain/system/settings.go（FirewallSettingsView / FirewallSettingsPatch）
 * - 规则目录：internal/firewall/catalog.go（Catalog、RuleGroup、RuleInfo）
 * - 运行态 / 测试台：internal/service/firewall_policy_service.go
 * 全部接口仅超级管理员可用，前缀 /api/admin/system/firewall。
 */

const BASE = "/api/admin/system/firewall";

type QueryParams = Parameters<typeof buildQuery>[0];

// ── 事件 ──────────────────────────────────────────────

export type FirewallAction = "block" | "observe" | "log";
export type FirewallStage = "edge" | "payload" | "response";
export type FirewallTransport = "plain" | "sealed" | "legacy";
export type FirewallSeverity = "critical" | "high" | "medium" | "low";

/** WAF 命中的一条规则（截断、脱敏后入库） */
export interface FirewallMatchedRule {
  id: number;
  message: string;
  data?: string;
  /** critical / error / warning / notice（CRS 规则自身的严重性） */
  severity: string;
  phase: number;
  /** 规则组：CRS 文件编号（"942"）、"aegis"、"baseline" */
  group: string;
  tags?: string[];
  /** 命中的变量，如 ARGS:json.nickname、REQUEST_HEADERS:User-Agent */
  targets?: string[];
}

export interface FirewallEvent {
  id: number;
  requestId: string;
  ip: string;
  method: string;
  path: string;
  queryString: string;
  userAgent: string;
  headers?: Record<string, string>;
  /** 拦截原因：waf_blocked / waf_observed / waf_outbound / custom_rule / custom_log / rate_limited / blocked_signature / banned_* … */
  reason: string;
  httpStatus: number;
  responseCode: number;
  wafRuleId?: number | null;
  wafAction?: string;
  wafData?: string;
  country: string;
  countryCode: string;
  region: string;
  city: string;
  isp: string;
  asn: string;
  timezone: string;
  latitude?: number | null;
  longitude?: number | null;
  severity: FirewallSeverity | string;
  blockedAt: string;
  // 规则集 v2
  action: FirewallAction;
  stage: FirewallStage;
  transport: FirewallTransport;
  appKey?: string;
  anomalyScore?: number;
  matches?: FirewallMatchedRule[];
  /** 自定义规则 ID */
  ruleRef?: number;
  ruleName?: string;
}

export interface FirewallEventPage {
  items: FirewallEvent[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface FirewallEventQuery {
  page?: number;
  pageSize?: number;
  startTime?: string;
  endTime?: string;
  ip?: string;
  country?: string;
  reason?: string;
  wafRuleId?: number;
  pathPattern?: string;
  severity?: string;
  action?: FirewallAction | "";
  stage?: FirewallStage | "";
  transport?: FirewallTransport | "";
  appKey?: string;
  ruleRef?: number;
  /** 请求 ID / 路径 / UA / 规则名 */
  keyword?: string;
  sortBy?: "blocked_at" | "ip" | "score";
  sortOrder?: "asc" | "desc";
}

export interface RankedItem {
  key: string;
  count: number;
}

export interface FirewallTimePoint {
  time: string;
  count: number;
  critical: number;
  high: number;
  medium: number;
  low: number;
}

/** 统计：排行与时间序列只数拦截（action = block） */
export interface FirewallStatsV2 {
  totalBlocked: number;
  totalObserved: number;
  totalLogged: number;
  uniqueIPs: number;
  /** 上一个等长时间窗的拦截数（环比） */
  previousBlocked: number;
  topIPs: RankedItem[];
  topCountries: RankedItem[];
  topRules: RankedItem[];
  topPaths: RankedItem[];
  topReasons: RankedItem[];
  severityCounts: RankedItem[];
  actionCounts: RankedItem[];
  stageCounts: RankedItem[];
  transportCounts: RankedItem[];
  /** 规则组（从命中明细展开，key 为 "942" / "aegis"…） */
  topGroups: RankedItem[];
  /** 自定义规则（key 为规则名） */
  topCustomRules: RankedItem[];
  topApps: RankedItem[];
  timeSeries: FirewallTimePoint[];
}

export interface FirewallStatsQuery {
  startTime?: string;
  endTime?: string;
  /** minute 只在 6 小时以内生效，超过自动退回 hour */
  granularity?: "minute" | "hour" | "day";
}

// ── 运行态 ────────────────────────────────────────────

export interface FirewallCounters {
  inspected: number;
  edgeBlocked: number;
  wafBlocked: number;
  observed: number;
  /** 解密后检测的请求数（sealed + legacy） */
  decrypted: number;
  wafErrors: number;
  avgWafMicros: number;
  startedAt: string;
}

/** 本副本运行态（多副本时各副本计数独立） */
export interface FirewallRuntime {
  enabled: boolean;
  wafActive: boolean;
  detectionOnly: boolean;
  threshold: number;
  paranoia: number;
  customRules: number;
  exclusions: number;
  ruleErrors: string[];
  reloadVersion: number;
  reloadedAt: string;
  crsVersion: string;
  activeRules: number;
  totalRules: number;
  rulesetVersion: number;
  counters: FirewallCounters;
}

// ── 设置 ──────────────────────────────────────────────

export interface FirewallSettings {
  enabled: boolean;
  globalRate: string;
  authRate: string;
  adminRate: string;
  corazaEnabled: boolean;
  corazaParanoia: number;
  requestBodyLimit: number;
  requestBodyMemory: number;
  allowedCIDRs: string[];
  blockedCIDRs: string[];
  blockedUserAgents: string[];
  blockedPathPrefix: string[];
  maxPathLength: number;
  maxQueryLength: number;
  defaultBanMode: IPBanMode | string;
  tarpitDelayMs: number;
  banRedirectUrl: string;
  rulesetVersion: number;
  corazaDetectionOnly: boolean;
  corazaDetectionParanoia: number;
  corazaAnomalyThreshold: number;
  corazaOutboundThreshold: number;
  corazaResponseInspection: boolean;
  corazaResponseBodyLimit: number;
  corazaRemovedRules: string[];
  corazaDisabledGroups: string[];
  corazaInspectAdminBody: boolean;
  corazaBodyExemptPrefixes: string[];
  corazaSensitiveArgs: string[];
  corazaRichTextArgs: string[];
  corazaArgumentsLimit: number;
  corazaExtraContentTypes: string[];
  trustedCIDRs: string[];
  blockedSignatures: string[];
  /** environment：还没在控制台保存过；database：以数据库为准 */
  source: "environment" | "database" | "unavailable" | string;
  reloadVersion: number;
  reloadedAt: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
}

/** 部分更新：只传要改的字段；列表传 [] 表示显式清空（不是恢复默认） */
export type FirewallSettingsPatch = Partial<
  Omit<FirewallSettings, "source" | "reloadVersion" | "reloadedAt" | "updatedBy" | "updatedAt" | "rulesetVersion">
>;

// ── 自定义规则 ────────────────────────────────────────

export type FirewallRuleAction = "allow" | "block" | "log" | "skip_waf";

export interface FirewallRule {
  id: number;
  name: string;
  description: string;
  enabled: boolean;
  /** 越小越先求值 */
  priority: number;
  /** expr 表达式，变量见 FIREWALL_RULE_VARIABLES */
  expression: string;
  action: FirewallRuleAction;
  /** 拦截时的响应方式（封禁模式之一；空 = 403 JSON） */
  blockMode: IPBanMode | "";
  /** 命中后封禁来源 IP 的秒数（0 = 不封，-1 = 永久；只有 block 可用） */
  banSeconds: number;
  hits: number;
  lastHitAt?: string | null;
  createdBy?: number | null;
  updatedBy?: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface FirewallRuleInput {
  name: string;
  description?: string;
  enabled?: boolean;
  priority?: number;
  expression: string;
  action: FirewallRuleAction;
  blockMode?: IPBanMode | "";
  banSeconds?: number;
}

/** 表达式里可用的变量（与后端 firewall.RuleEnv 一致） */
export const FIREWALL_RULE_VARIABLES: { name: string; type: string; label: string; example: string }[] = [
  { name: "ip", type: "string", label: "客户端 IP", example: `cidr(ip, "10.0.0.0/8")` },
  { name: "method", type: "string", label: "请求方法", example: `method == "POST"` },
  { name: "path", type: "string", label: "路径", example: `path startsWith "/api/admin/"` },
  { name: "query", type: "string", label: "原始查询串", example: `query contains "debug=1"` },
  { name: "host", type: "string", label: "Host", example: `host endsWith ".example.com"` },
  { name: "ua", type: "string", label: "User-Agent", example: `ua matches "(?i)python-requests"` },
  { name: "referer", type: "string", label: "Referer", example: `referer == ""` },
  { name: "contentType", type: "string", label: "Content-Type", example: `contentType startsWith "text/xml"` },
  { name: "headers", type: "map", label: "请求头（键小写）", example: `headers["x-forwarded-proto"] != "https"` },
  { name: "args", type: "map", label: "查询参数", example: `args["page"] == "999"` },
  { name: "app", type: "string", label: "网关 appKey", example: `app == "voyage_main"` },
  { name: "country", type: "string", label: "国家代码", example: `country in ["KP", "IR"]` },
  { name: "region", type: "string", label: "省 / 州", example: `region == "广东"` },
  { name: "city", type: "string", label: "城市", example: `city == "深圳"` },
  { name: "isp", type: "string", label: "运营商", example: `isp contains "Amazon"` },
  { name: "asn", type: "string", label: "ASN", example: `asn == "AS16509"` },
  { name: "hour", type: "int", label: "北京时间小时", example: `hour >= 2 && hour < 6` },
  { name: "weekday", type: "int", label: "星期（0 = 周日）", example: `weekday in [0, 6]` },
  { name: "encrypted", type: "bool", label: "声明了加密载荷", example: `!encrypted && path startsWith "/api/v1/apps/"` }
];

// ── WAF 例外 ──────────────────────────────────────────

export type ExclusionPathMatch = "prefix" | "exact" | "regex";

export interface FirewallExclusion {
  id: number;
  name: string;
  note: string;
  enabled: boolean;
  position: number;
  pathMatch: ExclusionPathMatch;
  path: string;
  methods: string[];
  ruleIds: number[];
  tags: string[];
  /** 为空 = 整条移除规则；非空 = 只把这些变量从检查目标里拿掉 */
  targets: string[];
  createdBy?: number | null;
  updatedBy?: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface FirewallExclusionInput {
  name: string;
  note?: string;
  enabled?: boolean;
  pathMatch: ExclusionPathMatch;
  path: string;
  methods?: string[];
  ruleIds?: number[];
  tags?: string[];
  targets?: string[];
}

// ── 规则目录 ──────────────────────────────────────────

export interface FirewallRuleGroup {
  code: string;
  file: string;
  name: string;
  description: string;
  kind: "request" | "response" | "aegis" | "core";
  ruleCount: number;
  enabled: boolean;
  /** 不能停用（初始化、阻断评估、关联统计、Aegis 专属） */
  locked: boolean;
}

export interface FirewallRuleInfo {
  id: number;
  group: string;
  message: string;
  severity: string;
  paranoia: number;
  phase: number;
  tags: string[] | null;
  removed: boolean;
  active: boolean;
}

export interface FirewallCatalog {
  version: string;
  groups: FirewallRuleGroup[];
  rules: FirewallRuleInfo[];
  totalRules: number;
  activeRules: number;
  paranoia: number;
  detectionParanoia: number;
}

export interface FirewallCatalogRuleDetail extends FirewallRuleInfo {
  groupName: string;
  recent: FirewallEvent[];
  recentTotal: number;
}

// ── 测试台 ────────────────────────────────────────────

export interface FirewallSandboxInput {
  method: string;
  url: string;
  headers?: Record<string, string>;
  body?: string;
  ip?: string;
  transport?: FirewallTransport;
  /** 带上一条还没保存的例外，看它能不能消掉误报 */
  draftExclusion?: FirewallExclusionInput;
}

export interface FirewallSandboxResult {
  verdict: "pass" | "block" | "observe";
  stage: FirewallStage | "";
  /** custom_rule / custom_allow / custom_skip_waf / waf_blocked / waf_observed / waf_disabled / clean */
  reason: string;
  customRule?: FirewallRule;
  loggedRules: FirewallRule[];
  inboundScore: number;
  threshold: number;
  detectionOnly: boolean;
  status: number;
  ruleId: number;
  matches: FirewallMatchedRule[];
  effectiveContentType: string;
  elapsedMicros: number;
  usedDraft: boolean;
}

// ── 请求 ──────────────────────────────────────────────

export const firewallApi = {
  runtime: (token: string) => apiRequest<FirewallRuntime>(`${BASE}/runtime`, { token }),

  settings: (token: string) => apiRequest<FirewallSettings>(`${BASE}/settings`, { token }),
  updateSettings: (token: string, patch: FirewallSettingsPatch) =>
    apiRequest<FirewallSettings>(`${BASE}/settings`, { method: "PUT", token, body: JSON.stringify(patch) }),

  events: (token: string, query?: FirewallEventQuery) =>
    apiRequest<FirewallEventPage>(`${BASE}/logs${buildQuery({ ...(query ?? {}) } as QueryParams)}`, { token }),
  event: (token: string, id: number) => apiRequest<FirewallEvent>(`${BASE}/logs/${id}`, { token }),
  stats: (token: string, query?: FirewallStatsQuery) =>
    apiRequest<FirewallStatsV2>(`${BASE}/stats${buildQuery({ ...(query ?? {}) } as QueryParams)}`, { token }),
  cleanup: (token: string, before: string) =>
    apiRequest<{ deleted: number; before: string }>(`${BASE}/logs`, { method: "DELETE", token, body: JSON.stringify({ before }) }),

  rules: (token: string) => apiRequest<FirewallRule[]>(`${BASE}/rules`, { token }),
  createRule: (token: string, input: FirewallRuleInput) =>
    apiRequest<FirewallRule>(`${BASE}/rules`, { method: "POST", token, body: JSON.stringify(input) }),
  updateRule: (token: string, id: number, input: FirewallRuleInput) =>
    apiRequest<FirewallRule>(`${BASE}/rules/${id}`, { method: "PUT", token, body: JSON.stringify(input) }),
  toggleRule: (token: string, id: number, enabled: boolean) =>
    apiRequest<{ id: number; enabled: boolean }>(`${BASE}/rules/${id}`, { method: "PATCH", token, body: JSON.stringify({ enabled }) }),
  deleteRule: (token: string, id: number) => apiRequest<{ id: number }>(`${BASE}/rules/${id}`, { method: "DELETE", token }),
  reorderRules: (token: string, ids: number[]) =>
    apiRequest<{ ids: number[] }>(`${BASE}/rules/reorder`, { method: "POST", token, body: JSON.stringify({ ids }) }),
  validateRule: (token: string, expression: string, action?: FirewallRuleAction) =>
    apiRequest<{ valid: boolean; error?: string }>(`${BASE}/rules/validate`, {
      method: "POST",
      token,
      body: JSON.stringify({ expression, action })
    }),

  exclusions: (token: string) => apiRequest<FirewallExclusion[]>(`${BASE}/exclusions`, { token }),
  createExclusion: (token: string, input: FirewallExclusionInput) =>
    apiRequest<FirewallExclusion>(`${BASE}/exclusions`, { method: "POST", token, body: JSON.stringify(input) }),
  updateExclusion: (token: string, id: number, input: FirewallExclusionInput) =>
    apiRequest<FirewallExclusion>(`${BASE}/exclusions/${id}`, { method: "PUT", token, body: JSON.stringify(input) }),
  toggleExclusion: (token: string, id: number, enabled: boolean) =>
    apiRequest<{ id: number; enabled: boolean }>(`${BASE}/exclusions/${id}`, { method: "PATCH", token, body: JSON.stringify({ enabled }) }),
  deleteExclusion: (token: string, id: number) => apiRequest<{ id: number }>(`${BASE}/exclusions/${id}`, { method: "DELETE", token }),

  catalog: (token: string) => apiRequest<FirewallCatalog>(`${BASE}/catalog`, { token }),
  catalogRule: (token: string, id: number) => apiRequest<FirewallCatalogRuleDetail>(`${BASE}/catalog/rules/${id}`, { token }),
  directives: (token: string) => apiRequest<{ directives: string }>(`${BASE}/directives`, { token }),

  sandbox: (token: string, input: FirewallSandboxInput) =>
    apiRequest<FirewallSandboxResult>(`${BASE}/sandbox`, { method: "POST", token, body: JSON.stringify(input) })
};
