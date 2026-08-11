import { apiRequest } from "./client";

/**
 * 出海代理网关管理端 API。
 *
 * 类型直接对齐后端 `systemdomain.EgressSettingsView` / `EgressSettingsUpdate`：
 * 配置本体是一整张路由表（端点 + 规则），读写都按整份处理，
 * 不做逐字段 patch —— 列表型配置的部分更新既难表达也难审计。
 */

export type EgressAction = "proxy" | "direct" | "reject";
export type EgressStrategy = "failover" | "round_robin" | "random" | "weighted" | "latency";

export interface EgressTLSConfig {
  enabled: boolean;
  serverName?: string;
  insecureSkipVerify?: boolean;
  caPem?: string;
  clientCertPem?: string;
  clientKeyPem?: string;
  alpn?: string[];
  minVersion?: string;
}

export interface EgressSSHConfig {
  user?: string;
  password?: string;
  privateKeyPem?: string;
  passphrase?: string;
  hostKeyFingerprint?: string;
  keepAliveSeconds?: number;
}

export interface EgressShadowsocksConfig {
  method?: string;
  password?: string;
}

export interface EgressEndpoint {
  name: string;
  enabled?: boolean;
  protocol: string;
  address?: string;
  username?: string;
  password?: string;
  via?: string;
  weight?: number;
  dialTimeoutMs?: number;
  region?: string;
  remark?: string;
  probeUrl?: string;
  httpForwardMode?: boolean;
  headers?: Record<string, string>;
  tls: EgressTLSConfig;
  ssh: EgressSSHConfig;
  shadowsocks: EgressShadowsocksConfig;
  options?: Record<string, string>;
  /** 只读：后端只回传「是否已配置」，密钥本身永不出网 */
  passwordSet?: boolean;
  privateKeySet?: boolean;
  /** 只在提交时使用：显式清空该端点的全部密钥 */
  clearSecrets?: boolean;
}

export interface EgressMatch {
  domainSuffixes?: string[];
  excludeDomainSuffixes?: string[];
  domains?: string[];
  domainKeywords?: string[];
  domainRegexps?: string[];
  cidrs?: string[];
  ports?: number[];
  schemes?: string[];
  profiles?: string[];
  matchAll?: boolean;
}

export interface EgressRule {
  name: string;
  enabled?: boolean;
  priority?: number;
  action?: EgressAction;
  endpoints?: string[];
  strategy?: EgressStrategy;
  match: EgressMatch;
  remark?: string;
}

export interface EgressHealth {
  enabled: boolean;
  intervalSeconds?: number;
  timeoutSeconds?: number;
  failureThreshold?: number;
  successThreshold?: number;
  probeUrl?: string;
  passiveEnabled: boolean;
  cooldownSeconds?: number;
  allowUnhealthy?: boolean;
}

export interface EgressEndpointStat {
  name: string;
  protocol: string;
  address?: string;
  region?: string;
  remark?: string;
  via?: string;
  chain?: string[];
  weight: number;
  enabled: boolean;
  healthy: boolean;
  dials: number;
  successes: number;
  failures: number;
  bytesIn: number;
  bytesOut: number;
  latencyMs: number;
  consecutiveFailures: number;
  lastError?: string;
  lastCheckedAt?: string;
  cooldownUntil?: string;
}

export interface EgressRuleStat {
  name: string;
  enabled: boolean;
  priority: number;
  action: EgressAction;
  strategy?: EgressStrategy;
  endpoints?: string[];
  suffixCount: number;
  matched: number;
  remark?: string;
}

export interface EgressRuntime {
  enabled: boolean;
  source?: string;
  version: number;
  loadedAt: string;
  defaultAction: EgressAction;
  routed: number;
  proxied: number;
  direct: number;
  rejected: number;
  failed: number;
  endpoints: EgressEndpointStat[];
  rules: EgressRuleStat[];
}

export interface EgressCatalog {
  protocols: string[];
  actions: EgressAction[];
  strategies: EgressStrategy[];
  shadowsocksMethods: string[];
  defaultProbeUrl: string;
  secretPlaceholder: string;
}

export interface EgressSettings {
  enabled: boolean;
  defaultAction: EgressAction;
  defaultEndpoints: string[] | null;
  defaultStrategy: EgressStrategy;
  dialTimeoutMs: number;
  tlsHandshakeTimeoutMs: number;
  responseHeaderTimeoutMs: number;
  idleConnTimeoutMs: number;
  maxIdleConnsPerHost: number;
  health: EgressHealth;
  endpoints: EgressEndpoint[];
  rules: EgressRule[];
  source: string;
  reloadVersion: number;
  reloadedAt: string;
  updatedBy?: number | null;
  updatedAt?: string | null;
  runtime: EgressRuntime;
  catalog: EgressCatalog;
}

export type EgressSettingsUpdate = Omit<
  EgressSettings,
  "source" | "reloadVersion" | "reloadedAt" | "updatedBy" | "updatedAt" | "runtime" | "catalog"
>;

export interface EgressTestResult {
  ok: boolean;
  url: string;
  action: EgressAction;
  rule?: string;
  reason?: string;
  endpoint?: string;
  chain?: string[];
  statusCode?: number;
  latencyMs: number;
  body?: string;
  error?: string;
}

export interface EgressRuleEvaluation {
  rule: string;
  matched: boolean;
  reason?: string;
}

export interface EgressExplanation {
  host: string;
  port: number;
  scheme: string;
  profile?: string;
  action: EgressAction;
  rule: string;
  reason?: string;
  endpoint?: string;
  chain?: string[];
  error?: string;
  evaluated: EgressRuleEvaluation[];
}

export interface EgressProbeResult {
  endpoint: string;
  ok: boolean;
  latencyMs: number;
  error?: string;
  probeUrl?: string;
  chain?: string[];
}

const BASE = "/api/admin/system/egress";

export function getEgressSettings(token: string) {
  return apiRequest<EgressSettings>(BASE, { token });
}

export function updateEgressSettings(token: string, payload: EgressSettingsUpdate) {
  return apiRequest<EgressSettings>(BASE, { token, method: "PUT", body: JSON.stringify(payload) });
}

export function resetEgressSettings(token: string) {
  return apiRequest<EgressSettings>(`${BASE}/reset`, { token, method: "POST" });
}

export function testEgress(
  token: string,
  payload: { url?: string; endpoint?: string; profile?: string; timeoutMs?: number }
) {
  return apiRequest<EgressTestResult>(`${BASE}/test`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function explainEgress(
  token: string,
  payload: { host: string; port?: number; scheme?: string; profile?: string }
) {
  return apiRequest<EgressExplanation>(`${BASE}/explain`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function probeEgress(token: string) {
  return apiRequest<{ results: EgressProbeResult[] }>(`${BASE}/probe`, { token, method: "POST" });
}
