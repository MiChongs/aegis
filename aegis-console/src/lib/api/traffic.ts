import { apiRequest, buildQuery } from "./client";

/**
 * 流量管理（内置插件 traffic）管理端 API。
 *
 * 类型逐字段对齐后端 `pkg/traffic`（spec.go / views.go）与
 * `internal/service/traffic_service.go` 的返回形状。全部接口仅超级管理员可用。
 */

// ── 配置 ──

export type TrafficMode = "enforce" | "observe" | "bypass";
export type TrafficClass = "critical" | "high" | "normal" | "low";
export type LimitKey = "global" | "ip" | "subnet" | "app" | "token" | "header" | "query";
export type LimitScope = "local" | "cluster";
export type HeaderOp = "exists" | "absent" | "equals" | "prefix" | "contains";

export interface ResponseSpec {
  status: number; // 429 | 503 | 403
  code: number;
  message: string;
  retryAfterSeconds: number;
}

export interface OverloadSpec {
  enabled: boolean;
  cpuThreshold: number;
  maxInflight: number;
  cooldownMs: number;
}

export interface TrafficSettings {
  enabled: boolean;
  exemptAdmin: boolean;
  exemptPaths: string[];
  overload: OverloadSpec;
  response: ResponseSpec;
  cluster: { enabled: boolean };
  maxTrackedKeys: number;
}

export interface HeaderMatch {
  name: string;
  op: HeaderOp;
  value: string;
}

export interface MatchSpec {
  methods: string[];
  paths: string[];
  excludePaths: string[];
  hosts: string[];
  appKeys: string[];
  cidrs: string[];
  excludeCidrs: string[];
  headers: HeaderMatch[];
}

export interface LimitSpec {
  key: LimitKey;
  name: string;
  rate: number;
  periodMs: number;
  burst: number;
  scope: LimitScope;
  fallback: "ip" | "skip";
}

export interface ConcurrencySpec {
  max: number;
  adaptive: boolean;
  minLimit: number;
  maxLimit: number;
  latencyToleranceX10: number;
}

export interface QueueSpec {
  size: number;
  timeoutMs: number;
}

export interface WarmupSpec {
  baselineRps: number;
  maxRps: number;
  stepPct: number;
  intervalMs: number;
  cooldownSeconds: number;
}

export interface TrafficRule {
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  mode: TrafficMode;
  class: TrafficClass;
  terminal: boolean;
  match: MatchSpec;
  limits: LimitSpec[];
  concurrency?: ConcurrencySpec | null;
  queue?: QueueSpec | null;
  warmup?: WarmupSpec | null;
  response?: ResponseSpec | null;
  updatedAt?: string;
  updatedBy?: number | null;
}

export interface TrafficConfig {
  revision: number;
  settings: TrafficSettings;
  rules: TrafficRule[];
  updatedAt?: string | null;
  updatedBy?: number | null;
}

export interface ValidationError {
  field: string;
  message: string;
}

export interface RuleTemplate {
  id: string;
  name: string;
  description: string;
  category: string;
  rule: TrafficRule;
}

// ── 运行态 ──

export interface Counts {
  arrivals: number;
  admitted: number;
  queued: number;
  bypassed: number;
  observed: number;
  rejectedRate: number;
  rejectedConcurrency: number;
  rejectedQueue: number;
  rejectedTimeout: number;
  rejectedOverload: number;
}

export interface SeriesPoint {
  ts: number; // unix 秒
  arrivals: number;
  admitted: number;
  queued: number;
  rejected: number;
  observed: number;
  p50: number; // 毫秒
  p99: number;
  inflight: number;
  limit: number;
}

export interface LatencyView {
  samples: number;
  avg: number;
  p50: number;
  p90: number;
  p99: number;
  max: number;
}

export type WarmupState = "stable" | "ramping" | "saturated" | "cooldown";

export interface WarmupView {
  state: WarmupState;
  ceiling: number;
  baseline: number;
  max: number;
  rampEvents: number;
  lastBurstAt?: string | null;
}

export interface HotKey {
  kind: LimitKey;
  key: string;
  count: number;
}

export interface LimitView {
  key: LimitKey;
  name?: string;
  rate: number;
  periodMs: number;
  burst: number;
  scope: LimitScope;
  effectiveRate: number;
  trackedKeys: number;
  overflowed: number;
  rejected: number;
  leaseMode?: "lease" | "fallback" | "";
}

export interface RuleStats {
  id: string;
  name: string;
  mode: TrafficMode;
  class: TrafficClass;
  enabled: boolean;
  totals: Counts;
  lastSecond: Counts;
  inflight: number;
  concurrencyLimit: number;
  queueDepth: number;
  queueCapacity: number;
  latency: LatencyView;
  warmup?: WarmupView | null;
  limits: LimitView[];
  hotKeys: HotKey[];
  series?: SeriesPoint[];
}

export interface OverloadView {
  enabled: boolean;
  active: boolean;
  cpu: number;
  threshold: number;
  inflight: number;
  capacity: number;
  maxPass: number;
  minRttMs: number;
  dropped: number;
  lastDropAt?: string | null;
}

export interface TrafficSnapshot {
  instance: string;
  enabled: boolean;
  revision: number;
  statsSince: string;
  now: string;
  totals: Counts;
  lastSecond: Counts;
  exempt: number;
  inflight: number;
  peakRps: number;
  latency: LatencyView;
  overload: OverloadView;
  rules: RuleStats[];
  series: SeriesPoint[];
}

export interface NodeSummary {
  instance: string;
  host: string;
  startedAt: string;
  updatedAt: string;
  revision: number;
  enabled: boolean;
  lastSecond: Counts;
  totals: Counts;
  inflight: number;
  cpu: number;
  overloaded: boolean;
  p99Ms: number;
  goroutines: number;
  heapMb: number;
  cores: number;
}

export type NativePluginState = "running" | "stopped" | "starting" | "stopping" | "failed";

export interface NativePlugin {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  category: string;
  capabilities: string[];
  builtin: boolean;
  enabled: boolean;
  state: NativePluginState;
  since?: string | null;
  error?: string;
  health?: Record<string, unknown>;
  updatedAt?: string | null;
  updatedBy?: number | null;
}

export interface TrafficOverview {
  plugin: NativePlugin | null;
  revision: number;
  local: TrafficSnapshot;
  nodes: NodeSummary[];
  cluster: {
    instances: number;
    lastSecond: Counts;
    inflight: number;
    coordinator: "redis" | "none";
  };
}

export interface SimulateInput {
  method: string;
  path: string;
  host?: string;
  ip?: string;
  query?: string;
  headers?: Record<string, string>;
}

export interface SimKey {
  kind: LimitKey;
  value: string;
  remaining: number;
  capacity: number;
  wouldPass: boolean;
}

export interface SimulateStep {
  ruleId: string;
  ruleName: string;
  matched: boolean;
  miss?: string;
  mode: TrafficMode;
  keys?: SimKey[];
  outcome: "pass" | "reject" | "observe" | "bypass" | "skip";
  detail?: string;
  terminal: boolean;
}

export interface SimulateResult {
  exempt: boolean;
  exemptReason?: string;
  class: TrafficClass;
  verdict: "allow" | "reject" | "observe";
  steps: SimulateStep[];
}

export interface RevisionSummary {
  revision: number;
  action: string; // settings | rule.create | rule.update | rule.delete | rule.reorder | rule.toggle | rollback | import | seed
  note: string;
  ruleCount: number;
  enabled: boolean;
  createdBy?: number | null;
  createdAt: string;
}

export interface RevisionDetail extends RevisionSummary {
  config: TrafficConfig;
}

export type HistoryRange = "1h" | "6h" | "24h" | "7d";

export interface HistoryPoint {
  ts: number; // unix 秒（桶起点）
  arrivals: number;
  admitted: number;
  queued: number;
  rejected: number;
  observed: number;
  p50: number;
  p99: number;
  peakRps: number;
  maxInflight: number;
}

export interface TrafficHistory {
  range: HistoryRange;
  stepSeconds: number;
  ruleId?: string;
  points: HistoryPoint[];
  totals: Counts;
}

export interface TrafficEvent {
  id: number;
  at: string;
  instance: string;
  kind: string;
  ruleId?: string;
  ruleName?: string;
  detail?: Record<string, unknown>;
}

// ── 请求 ──

const BASE = "/api/admin/system/traffic";
const PLUGINS = "/api/admin/system/native-plugins";

type Mutation = { expectedRevision?: number; note?: string };

export const trafficApi = {
  overview: (token: string, seconds: number) =>
    apiRequest<TrafficOverview>(`${BASE}/overview${buildQuery({ seconds })}`, { token }),
  config: (token: string) => apiRequest<TrafficConfig>(`${BASE}/config`, { token }),
  templates: (token: string) => apiRequest<RuleTemplate[]>(`${BASE}/templates`, { token }),

  saveSettings: (token: string, settings: TrafficSettings, m: Mutation = {}) =>
    apiRequest<TrafficConfig>(`${BASE}/settings`, { token, method: "PUT", body: JSON.stringify({ settings, ...m }) }),
  createRule: (token: string, rule: TrafficRule, m: Mutation & { position?: "top" | "bottom" } = {}) =>
    apiRequest<TrafficConfig>(`${BASE}/rules`, { token, method: "POST", body: JSON.stringify({ rule, ...m }) }),
  updateRule: (token: string, ruleId: string, rule: TrafficRule, m: Mutation = {}) =>
    apiRequest<TrafficConfig>(`${BASE}/rules/${encodeURIComponent(ruleId)}`, {
      token, method: "PUT", body: JSON.stringify({ rule, ...m }),
    }),
  deleteRule: (token: string, ruleId: string, m: Mutation = {}) =>
    apiRequest<TrafficConfig>(`${BASE}/rules/${encodeURIComponent(ruleId)}${buildQuery({ expectedRevision: m.expectedRevision })}`, {
      token, method: "DELETE",
    }),
  toggleRule: (token: string, ruleId: string, enabled: boolean, m: Mutation = {}) =>
    apiRequest<TrafficConfig>(`${BASE}/rules/${encodeURIComponent(ruleId)}/toggle`, {
      token, method: "POST", body: JSON.stringify({ enabled, ...m }),
    }),
  reorderRules: (token: string, ids: string[], m: Mutation = {}) =>
    apiRequest<TrafficConfig>(`${BASE}/rules/reorder`, { token, method: "POST", body: JSON.stringify({ ids, ...m }) }),
  ruleStats: (token: string, ruleId: string, seconds: number) =>
    apiRequest<RuleStats>(`${BASE}/rules/${encodeURIComponent(ruleId)}/stats${buildQuery({ seconds })}`, { token }),
  validateRule: (token: string, rule: TrafficRule) =>
    apiRequest<{ errors: ValidationError[]; rule: TrafficRule }>(`${BASE}/validate`, {
      token, method: "POST", body: JSON.stringify({ rule }),
    }),
  simulate: (token: string, input: SimulateInput, draft?: TrafficRule) =>
    apiRequest<SimulateResult>(`${BASE}/simulate`, {
      token, method: "POST", body: JSON.stringify({ ...input, draft }),
    }),

  revisions: (token: string, limit = 50) =>
    apiRequest<RevisionSummary[]>(`${BASE}/revisions${buildQuery({ limit })}`, { token }),
  revision: (token: string, revision: number) =>
    apiRequest<RevisionDetail>(`${BASE}/revisions/${revision}`, { token }),
  rollback: (token: string, revision: number, note?: string) =>
    apiRequest<TrafficConfig>(`${BASE}/rollback`, { token, method: "POST", body: JSON.stringify({ revision, note }) }),

  history: (token: string, range: HistoryRange, ruleId?: string) =>
    apiRequest<TrafficHistory>(`${BASE}/history${buildQuery({ range, ruleId })}`, { token }),
  events: (token: string, limit = 100, kind?: string) =>
    apiRequest<TrafficEvent[]>(`${BASE}/events${buildQuery({ limit, kind })}`, { token }),
  resetStats: (token: string) =>
    apiRequest<{ ok: boolean }>(`${BASE}/reset-stats`, { token, method: "POST" }),
};

export const nativePluginApi = {
  list: (token: string) => apiRequest<NativePlugin[]>(PLUGINS, { token }),
  enable: (token: string, id: string) =>
    apiRequest<NativePlugin>(`${PLUGINS}/${encodeURIComponent(id)}/enable`, { token, method: "POST" }),
  disable: (token: string, id: string) =>
    apiRequest<NativePlugin>(`${PLUGINS}/${encodeURIComponent(id)}/disable`, { token, method: "POST" }),
  restart: (token: string, id: string) =>
    apiRequest<NativePlugin>(`${PLUGINS}/${encodeURIComponent(id)}/restart`, { token, method: "POST" }),
};
