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
  /** 省略表示协议支持就承载 UDP；false 让线路只走 TCP */
  udp?: boolean;
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
  /** 传输层：tcp / udp，留空两者都匹配 */
  networks?: string[];
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
  /** 对能承载 UDP 的线路另做一次 UDP 探测（经线路发 DNS 查询） */
  udpProbe?: boolean;
  udpProbeServer?: string;
  udpProbeName?: string;
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
  udp: boolean;
  udpTransport?: "udp" | "stream";
  udpHealthy: boolean;
  udpLatencyMs: number;
  udpLastError?: string;
  udpLastCheckedAt?: string;
  udpSessions: number;
  packetsIn: number;
  packetsOut: number;
}

export interface EgressRuleStat {
  name: string;
  enabled: boolean;
  priority: number;
  action: EgressAction;
  strategy?: EgressStrategy;
  endpoints?: string[];
  suffixCount: number;
  networks?: string[];
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
  routedUdp: number;
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
  network: "tcp" | "udp";
  /** UDP 自测（DNS 查询）的解析结果 */
  answers?: string[];
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
  network: "tcp" | "udp";
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
  udpChecked: boolean;
  udpOk: boolean;
  udpLatencyMs?: number;
  udpError?: string;
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
  payload: { url?: string; endpoint?: string; profile?: string; timeoutMs?: number; network?: "tcp" | "udp"; name?: string }
) {
  return apiRequest<EgressTestResult>(`${BASE}/test`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function explainEgress(
  token: string,
  payload: { host: string; port?: number; scheme?: string; profile?: string; network?: "tcp" | "udp" }
) {
  return apiRequest<EgressExplanation>(`${BASE}/explain`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function probeEgress(token: string) {
  return apiRequest<{ results: EgressProbeResult[] }>(`${BASE}/probe`, { token, method: "POST" });
}

// ── 画布与 mihomo 核心 ──
//
// 画布与物品发放、等级体系同形（nodes / edges，节点位置是左上角），后端把它编译成
// 网关路由表与 mihomo 核心配置。密钥读回时是占位 `catalog.secretKept`，原样提交即保持原值。

export type EgressNodeType = "gateway" | "rule" | "endpoint" | "m_proxy" | "m_group" | "m_provider" | "m_rules" | "m_core";

export type EgressGraphNode = {
  id: string;
  type: EgressNodeType;
  position: { x: number; y: number };
  data: Record<string, unknown>;
};

export type EgressGraphEdge = { id: string; source: string; sourceHandle?: string; target: string };

export type EgressGraph = {
  nodes: EgressGraphNode[];
  edges: EgressGraphEdge[];
  viewport?: { x: number; y: number; zoom: number };
};

export type EgressIssue = { level: "error" | "warning"; nodeId?: string; edgeId?: string; message: string };

export type EgressPortSpec = { id: string; label: string; accepts: EgressNodeType[]; multi: boolean; side: "bottom" | "right" };

export type EgressNodeSpec = {
  type: EgressNodeType;
  label: string;
  category: string;
  hint: string;
  single?: boolean;
  input?: "top" | "left";
  outputs: EgressPortSpec[] | null;
};

export type MihomoFieldKind = "text" | "number" | "bool" | "select" | "secret" | "list" | "textarea" | "map";

export type MihomoFieldSpec = {
  key: string;
  label: string;
  kind: MihomoFieldKind;
  section?: string;
  options?: string[];
  required?: boolean;
  default?: unknown;
  placeholder?: string;
  hint?: string;
  /** 只在另一个字段取这些值时显示；值为 "*" 表示任意非空 */
  showWhen?: Record<string, string[]>;
};

export type MihomoTypeSpec = { type: string; label: string; hint?: string; server: boolean; fields: MihomoFieldSpec[] | null };

export type MihomoRuleTypeSpec = {
  type: string;
  label: string;
  hint?: string;
  example?: string;
  noPayload?: boolean;
  params?: string[];
  logic?: boolean;
};

export type MihomoCatalog = {
  proxyTypes: MihomoTypeSpec[];
  groupTypes: MihomoTypeSpec[];
  providerFields: MihomoFieldSpec[];
  ruleProviderFields: MihomoFieldSpec[];
  ruleTypes: MihomoRuleTypeSpec[];
  dnsFields: MihomoFieldSpec[];
  builtins: string[];
  secretKeys: string[];
  reservedKeys: string[];
  defaultTestUrl: string;
};

export type MihomoSettings = {
  mode?: string;
  logLevel?: string;
  ipv6: boolean;
  unifiedDelay: boolean;
  tcpConcurrent: boolean;
  globalClientFingerprint?: string;
  keepAliveIdle?: number;
  keepAliveInterval?: number;
  geodataMode: boolean;
  geodataLoader?: string;
  geoAutoUpdate: boolean;
  geoUpdateInterval?: number;
  geoxUrl?: Record<string, string>;
  globalUa?: string;
};

export type EgressGatewayData = {
  enabled: boolean;
  defaultAction?: EgressAction;
  defaultStrategy?: EgressStrategy;
  dialTimeoutMs?: number;
  tlsHandshakeTimeoutMs?: number;
  responseHeaderTimeoutMs?: number;
  idleConnTimeoutMs?: number;
  maxIdleConnsPerHost?: number;
  health: EgressHealth;
  order?: string[];
  note?: string;
};

export type EgressRuleData = {
  name: string;
  enabled?: boolean;
  action?: EgressAction;
  strategy?: EgressStrategy;
  match: EgressMatch;
  order?: string[];
  note?: string;
};

export type MihomoProxyData = { proxy: Record<string, unknown>; note?: string };

export type MihomoGroupData = {
  group: Record<string, unknown>;
  builtins?: string[];
  builtinsFirst?: boolean;
  order?: string[];
  note?: string;
};

export type MihomoProviderData = { name: string; provider: Record<string, unknown>; note?: string };

export type MihomoRuleRow = {
  id: string;
  type: string;
  payload?: string;
  target: string;
  params?: string[];
  enabled?: boolean;
  note?: string;
};

export type MihomoRulesData = {
  rules: MihomoRuleRow[];
  ruleProviders?: Record<string, Record<string, unknown>>;
  note?: string;
};

export type MihomoCoreData = {
  settings: MihomoSettings;
  dns?: Record<string, unknown>;
  dnsEnabled: boolean;
  extra?: Record<string, unknown>;
  note?: string;
};

export type EgressGraphCatalog = {
  nodes: EgressNodeSpec[];
  categories: Array<{ key: string; label: string }>;
  protocols: string[];
  actions: EgressAction[];
  strategies: EgressStrategy[];
  shadowsocksMethods: string[];
  defaultProbeUrl: string;
  secretKept: string;
  defaultGateway: EgressGatewayData;
  defaultCore: MihomoCoreData;
  mihomo: MihomoCatalog;
  /** 平台里已知的调用方标识（"x.*" 是前缀通配） */
  profiles: Array<{ name: string; label: string }>;
  /** 能承载 UDP 的网关协议：udp 要上一跳也能承载 UDP，stream 封装在 TCP 里 */
  udpProtocols: Record<string, "udp" | "stream">;
};

export type EgressGraphSummary = {
  enabled: boolean;
  defaultAction: EgressAction;
  rules: number;
  activeRules: number;
  endpoints: number;
  proxies: number;
  groups: number;
  providers: number;
  mihomoRules: number;
  ruleProviders: number;
  mihomoEnabled: boolean;
  targets: number;
};

export type EgressCompileView = {
  issues: EgressIssue[];
  summary: EgressGraphSummary;
  /** 节点 → 网关端点名（运行态按它贴回画布） */
  bindings: Record<string, string>;
  /** 规则名 → 节点 */
  ruleNodes: Record<string, string>;
  /** 节点 → 核心里的名字 */
  mihomoNames: Record<string, string>;
  chain: string[] | null;
  targets: string[] | null;
};

export type MihomoStatus = {
  status: "disabled" | "missing" | "installing" | "starting" | "running" | "crashed" | "stopped" | "failed";
  version?: string;
  binary: string;
  binaryPresent: boolean;
  home: string;
  pid?: number;
  startedAt?: string;
  appliedAt?: string;
  configHash?: string;
  restarts: number;
  lastError?: string;
  controllerAddr?: string;
  inboundAddr?: string;
  targets: number;
  proxies: number;
  groups: number;
  providers: number;
  rules: number;
  autoInstall: boolean;
  installing: boolean;
  platform: string;
  memoryInUse: number;
  uploadSpeed: number;
  downloadSpeed: number;
};

export type MihomoTrafficSample = { time: string; up: number; down: number; memory: number };

export type MihomoCoreStatusView = MihomoStatus & { traffic: MihomoTrafficSample[] | null };

export type EgressGraphView = {
  graph: EgressGraph;
  origin: "draft" | "applied" | "config";
  dirty: boolean;
  draftRevision: number;
  draftUpdatedAt?: string;
  draftUpdatedBy?: number;
  appliedVersion: number;
  appliedAt?: string;
  appliedBy?: number;
  active: boolean;
  compile: EgressCompileView;
  catalog: EgressGraphCatalog;
  core: MihomoStatus;
  runtime: EgressRuntime;
};

export type EgressRoutePreview = {
  enabled: boolean;
  defaultAction: EgressAction;
  defaultEndpoints: string[] | null;
  rules: EgressRule[] | null;
  endpoints: Array<{ name: string; protocol: string; address?: string; via?: string; target?: string }> | null;
};

export type EgressValidateResult = { compile: EgressCompileView; yaml?: string; routes: EgressRoutePreview };

export type EgressImportOptions = { mode: "merge" | "replace"; routeFallback: boolean };

export type EgressImportResult = {
  graph: EgressGraph;
  format: "yaml" | "uri";
  notes: string[] | null;
  added: EgressGraphSummary;
  compile: EgressCompileView;
};

export type EgressHistoryEntry = {
  version: number;
  appliedAt: string;
  appliedBy?: number;
  note?: string;
  summary: EgressGraphSummary;
};

export type MihomoDelayHistory = { time: string; delay: number };

export type MihomoProxyInfo = {
  name: string;
  type: string;
  udp: boolean;
  alive: boolean;
  history: MihomoDelayHistory[] | null;
  now?: string;
  all?: string[];
  fixed?: string;
  hidden?: boolean;
  testUrl?: string;
  icon?: string;
  "provider-name"?: string;
};

export type MihomoProviderInfo = {
  name: string;
  type: string;
  vehicleType: string;
  updatedAt?: string;
  proxies: MihomoProxyInfo[] | null;
  subscriptionInfo?: { Upload: number; Download: number; Total: number; Expire: number };
};

export type MihomoRuleProviderInfo = {
  name: string;
  type: string;
  vehicleType: string;
  behavior: string;
  format: string;
  ruleCount: number;
  updatedAt?: string;
};

export type MihomoProxiesView = {
  proxies: Record<string, MihomoProxyInfo>;
  providers: Record<string, MihomoProviderInfo> | null;
  ruleProviders: Record<string, MihomoRuleProviderInfo> | null;
};

export type MihomoConnection = {
  id: string;
  metadata: Record<string, string | number | boolean | null>;
  upload: number;
  download: number;
  start: string;
  chains: string[] | null;
  rule: string;
  rulePayload: string;
};

export type MihomoConnections = {
  downloadTotal: number;
  uploadTotal: number;
  memory: number;
  connections: MihomoConnection[] | null;
};

export type MihomoLogLine = { seq: number; time: string; source: "core" | "aegis"; level: string; message: string };

export type MihomoRuleInfo = {
  index: number;
  type: string;
  payload: string;
  proxy: string;
  size: number;
  extra?: { disabled: boolean; hitCount: number; hitAt: string; missCount: number };
};

export type MihomoInstallResult = { version: string; asset: string; path: string; sha256: string; size: number; source: string };

const GRAPH = `${BASE}/graph`;
const CORE = `${BASE}/mihomo`;

function post<T>(token: string, path: string, payload?: unknown) {
  return apiRequest<T>(path, { token, method: "POST", body: payload === undefined ? undefined : JSON.stringify(payload) });
}

export function getEgressGraph(token: string) {
  return apiRequest<EgressGraphView>(GRAPH, { token });
}

export function saveEgressGraph(token: string, payload: { graph: EgressGraph; revision: number }) {
  return apiRequest<EgressGraphView>(GRAPH, { token, method: "PUT", body: JSON.stringify(payload) });
}

export function validateEgressGraph(token: string, graph: EgressGraph) {
  return post<EgressValidateResult>(token, `${GRAPH}/validate`, { graph });
}

export function applyEgressGraph(token: string, payload: { graph: EgressGraph; revision: number; note?: string }) {
  return post<EgressGraphView>(token, `${GRAPH}/apply`, payload);
}

export function discardEgressGraph(token: string) {
  return post<EgressGraphView>(token, `${GRAPH}/discard`);
}

export function importEgressGraph(token: string, payload: { graph: EgressGraph; content?: string; url?: string; options: EgressImportOptions }) {
  return post<EgressImportResult>(token, `${GRAPH}/import`, payload);
}

export function exportEgressGraph(token: string, payload: { graph: EgressGraph; includeSecrets: boolean }) {
  return post<{ yaml: string }>(token, `${GRAPH}/export`, payload);
}

export function getEgressGraphHistory(token: string) {
  return apiRequest<{ items: EgressHistoryEntry[] | null }>(`${GRAPH}/history`, { token });
}

export function getEgressGraphVersion(token: string, version: number) {
  return apiRequest<{ version: number; graph: EgressGraph }>(`${GRAPH}/history/${version}`, { token });
}

export function rollbackEgressGraph(token: string, version: number) {
  return post<EgressGraphView>(token, `${GRAPH}/rollback`, { version });
}

export function getMihomoStatus(token: string) {
  return apiRequest<MihomoCoreStatusView>(`${CORE}/status`, { token });
}

export function installMihomo(token: string, version?: string) {
  return post<MihomoInstallResult>(token, `${CORE}/install`, { version: version ?? "" });
}

export function restartMihomo(token: string) {
  return post<null>(token, `${CORE}/restart`);
}

export function stopMihomo(token: string) {
  return post<null>(token, `${CORE}/stop`);
}

export function getMihomoProxies(token: string) {
  return apiRequest<MihomoProxiesView>(`${CORE}/proxies`, { token });
}

export function testMihomoDelay(token: string, payload: { name: string; group?: boolean; url?: string; timeoutMs?: number }) {
  return post<{ delays: Record<string, number> }>(token, `${CORE}/delay`, payload);
}

export function selectMihomoProxy(token: string, payload: { group: string; name: string }) {
  return post<null>(token, `${CORE}/select`, payload);
}

export function getMihomoConnections(token: string) {
  return apiRequest<MihomoConnections>(`${CORE}/connections`, { token });
}

export function closeMihomoConnection(token: string, id?: string) {
  return post<null>(token, `${CORE}/connections/close`, { id: id ?? "" });
}

export function mihomoProviderAction(token: string, payload: { kind: "proxy" | "rule"; name: string; action: "update" | "healthcheck" }) {
  return post<null>(token, `${CORE}/providers`, payload);
}

export function getMihomoRules(token: string) {
  return apiRequest<{ items: MihomoRuleInfo[] | null }>(`${CORE}/rules`, { token });
}

export function getMihomoLogs(token: string, after: number, limit = 500) {
  return apiRequest<{ items: MihomoLogLine[] | null }>(`${CORE}/logs?after=${after}&limit=${limit}`, { token });
}

export function queryMihomoDNS(token: string, payload: { name: string; type?: string }) {
  return post<Record<string, unknown>>(token, `${CORE}/dns`, payload);
}

export function mihomoMaintenance(token: string, action: "geo" | "flush-dns") {
  return post<null>(token, `${CORE}/maintenance`, { action });
}
