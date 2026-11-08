import { apiRequest, buildQuery } from "./client";

/**
 * 风控平台管理端 API（docs/risk-platform.md）。
 *
 * 主服务把 /api/admin/system/risk/* 原样转发到独立服务 risk-admin 的 /v1/*。
 * 类型逐字段对齐 services/risk 里的 Go 结构（bundle / store / admin 包的 JSON 标签）。
 * 读接口对能进系统设置的管理员开放，写接口限超级管理员。
 */

const P = "/api/admin/system/risk";

type Q = Record<string, string | number | boolean | undefined | null>;

const get = <T,>(token: string, path: string, query?: Q) => apiRequest<T>(`${P}${path}${query ? buildQuery(query) : ""}`, { token });
const send = <T,>(token: string, method: string, path: string, body?: unknown) =>
  apiRequest<T>(`${P}${path}`, { method, token, body: body === undefined ? undefined : JSON.stringify(body) });

export interface Paged<T> { list: T[]; total: number; page: number; pageSize: number }

// ── 枚举 ──

export type RiskAction = "pass" | "review" | "captcha" | "block" | "ban";
export type RiskLevel = "none" | "low" | "medium" | "high" | "critical";
export type RiskEntity = "user" | "account" | "device" | "ip" | "ip_subnet" | "phone" | "email_domain" | "app";
export type ListType = "ip" | "cidr" | "device" | "account" | "user" | "email_domain" | "phone_prefix" | "asn" | "country" | "ua_keyword";
export type ListKind = "block" | "allow" | "watch";
export type FeatureKind = "velocity" | "stream" | "graph" | "vector" | "derived";
export type Agg = "count" | "distinct" | "sum" | "max" | "avg";
export type CondOp = "eq" | "ne" | "gt" | "gte" | "lt" | "lte" | "in" | "not_in";
export type RuleMode = "enforce" | "observe";
export type FieldType = "string" | "number" | "bool" | "list" | "object";
export type EnforceTarget = "device" | "ip" | "account" | "firewall";

// ── 元数据 ──

export interface RiskVariable { name: string; type: string; label: string }
export interface RiskEventType { type: string; label: string }

export interface RiskMeta {
  variables: RiskVariable[];
  entities: RiskEntity[];
  dimensions: string[];
  listTypes: ListType[];
  eventTypes: RiskEventType[];
  graphTemplates: string[];
  streamColumns: string[];
  actions: RiskAction[];
  levels: RiskLevel[];
  guardPolicy: string;
  defaultBands: ScoreBand[];
  graphEdges: string[];
}

// ── 场景 ──

export interface SceneField { key: string; label: string; type: FieldType; required?: boolean }
export interface Enforcement { targets: EnforceTarget[]; durationSeconds: number; autoBanAllowed: boolean }
export interface ScoreBand { min: number; max: number; action: RiskAction; level: RiskLevel }

export interface Scene {
  key: string;
  name: string;
  description?: string;
  appIds?: number[];
  fields?: SceneField[];
  timeoutMs: number;
  failMode: "open" | "closed";
  captchaSupported: boolean;
  defaultAction: RiskAction | "";
  enforcement: Enforcement;
  listTypes: ListType[];
  features: string[];
  models: string[];
  bands: ScoreBand[];
  flow?: JdmGraph | null;
  policy: string;
  builtin: boolean;
  enabled: boolean;
  updatedAt: string;
  updatedBy: string;
}

export interface ReleaseState {
  scene: string;
  primaryId: number | null;
  candidateId: number | null;
  canaryPercent: number;
  shadow: boolean;
  updatedAt: string;
  updatedBy: string;
}

export interface SceneView extends Scene { state?: ReleaseState; ruleCount?: number }

/** zen JDM 决策图（只建模本控制台编辑器用到的部分，其余字段原样保留） */
export interface JdmNode {
  id: string;
  type: "inputNode" | "outputNode" | "decisionTableNode" | "expressionNode" | "functionNode" | "switchNode" | string;
  name: string;
  position: { x: number; y: number };
  content?: Record<string, unknown>;
}
export interface JdmEdge { id: string; sourceId: string; targetId: string; type: "edge"; sourceHandle?: string }
export interface JdmGraph { nodes: JdmNode[]; edges: JdmEdge[] }

export interface DecisionTableColumn { id: string; name: string; field: string; type: "expression" }
export interface DecisionTableContent {
  hitPolicy: "first" | "collect";
  inputs: DecisionTableColumn[];
  outputs: DecisionTableColumn[];
  rules: Array<Record<string, string> & { _id: string; _description?: string }>;
}

// ── 特征 ──

export interface Condition { field: string; op: CondOp; value: unknown }
export interface VelocityDef { windowSeconds: number; agg: "count" | "distinct"; distinctOf?: RiskEntity; allScenes?: boolean }
export interface StreamDef { events: string[]; scenes?: string[]; agg: Agg; field?: string; windowSeconds: number; conditions?: Condition[] }
export interface GraphDef { template: string; withinDays?: number; cacheTtl?: number }
export interface VectorDef { collection: string; topK: number; minScore: number; output: "similar_count" | "similar_banned" | "max_score" }

export interface Feature {
  key: string;
  name: string;
  kind: FeatureKind;
  entity?: RiskEntity | "";
  global?: boolean;
  default: number;
  velocity?: VelocityDef;
  stream?: StreamDef;
  graph?: GraphDef;
  vector?: VectorDef;
  expr?: string;
  description?: string;
  updatedAt?: string;
  updatedBy?: string;
}

// ── 规则 ──

export interface Rule {
  id: number;
  scene: string; // * = 全部场景
  key: string;
  name: string;
  description?: string;
  dimension: string;
  expr: string;
  score: number;
  mode: RuleMode;
  decision?: RiskAction | "";
  priority: number;
  reason?: string;
  tags?: string[];
  enabled: boolean;
  templateKey?: string;
  updatedAt?: string;
  updatedBy?: string;
}

export interface RuleTemplate { key: string; name: string; description: string; scenes: string[]; rules: Rule[] }

// ── 模型 ──

export interface RiskModel {
  key: string;
  name: string;
  format: "lightgbm" | "xgboost";
  objectKey: string;
  checksum: string;
  inputs: string[];
  transform: boolean;
  description?: string;
  nFeatures: number;
  sizeBytes: number;
  updatedAt: string;
  updatedBy: string;
}

// ── 名单 ──

export interface ListEntry {
  id: string;
  type: ListType;
  value: string;
  kind: ListKind;
  app?: number;
  scenes?: string[];
  reason?: string;
  source?: string;
  by?: string;
  decision?: string;
  exp?: string | null;
  at: string;
}

export interface ListHit { listType: ListType; kind: ListKind; value: string; listKey?: string; reason?: string }

// ── 行为模式（Flink CEP）──

export interface CepStep {
  name: string;
  events: string[];
  scenes?: string[];
  actions?: string[];
  conditions?: Condition[];
  times: number;
  contiguous?: boolean;
  distinct?: string;
  absent?: boolean;
}
export interface CepAction { kind: "list_add" | "case"; target?: "device" | "ip" | "account" | "user"; listKind?: "block" | "watch"; durationSeconds?: number }
export interface CepPattern {
  key: string;
  name: string;
  description?: string;
  enabled: boolean;
  keyBy: "user" | "device" | "ip" | "account";
  appIds?: number[];
  steps: CepStep[];
  withinSeconds: number;
  severity: "low" | "medium" | "high" | "critical";
  actions: CepAction[];
  updatedAt?: string;
  updatedBy?: string;
}

// ── 发布 ──

export interface Release {
  id: number;
  scene: string;
  version: string;
  bundle?: Record<string, unknown>;
  checksum: string;
  note: string;
  publishedBy: string;
  publishedAt: string;
}
export interface CompileIssue { stage: string; key?: string; message: string }
export interface ValidateResult { ok: boolean; issues: CompileIssue[]; features: number; rules: number; models: number }
export interface StreamSyncResult { created: string[] | null; dropped: string[] | null; failed: Record<string, string> }
export interface PublishResult { release: { id: number; version: string; checksum: string }; state: ReleaseState; streams: StreamSyncResult }

// ── 模拟与轨迹 ──

export interface SimRequest {
  appId?: number;
  userId?: number;
  account?: string;
  ip?: string;
  userAgent?: string;
  phone?: string;
  email?: string;
  amount?: number;
  targetUserId?: number;
  captchaPassed?: boolean;
  captchaUnavailable?: boolean;
  device?: { id?: string; platform?: string; brand?: string; model?: string; osVersion?: string; emulator?: boolean; rooted?: boolean; screen?: string };
  geo?: { country?: string; province?: string; city?: string; isp?: string; asn?: number; proxy?: boolean; vpn?: boolean; tor?: boolean; datacenter?: boolean; ipScore?: number };
  extra?: Record<string, unknown>;
}

export interface RpcRuleHit { key: string; name: string; dimension: string; score: number; mode: RuleMode; decision?: string; reason?: string }
export interface EvalResponse {
  decision_id: string;
  action: RiskAction;
  risk_level: RiskLevel;
  score: number;
  reasons?: string[];
  hits?: RpcRuleHit[];
  dimension_scores?: Record<string, number>;
  model_scores?: Record<string, number>;
  list_hits?: Array<{ list_type: string; kind: string; value: string; reason?: string }>;
  release: string;
  latency_us: number;
  decided_by: string;
  commands?: Array<{ id: string; kind: string; target: string; value: string; duration_seconds?: number; reason?: string }>;
  degraded?: string[];
}

export interface TraceStage {
  stage: string;
  micros: number;
  note?: string;
  degraded?: boolean;
  error?: string;
  result?: unknown;
  sources?: Array<{ source: string; micros: number; error?: string }>;
}
export interface TraceRule { key: string; name: string; dimension: string; score: number; mode: RuleMode; decision?: string; reason?: string; hit: boolean; error?: string }
export interface Trace {
  stages: TraceStage[];
  env: Record<string, unknown>;
  features: Record<string, number>;
  rules: TraceRule[];
  flow?: unknown;
  policy?: Record<string, unknown>;
  listHits?: ListHit[];
  commands?: unknown[];
  decidedBy?: string;
  degraded?: string[];
}

export interface SimulateInput { request: SimRequest; draft: boolean; mode: "fixed" | "live"; features?: Record<string, number>; lists?: ListHit[] }
export interface SimulateResult { result: EvalResponse; trace: Trace | null; features?: Record<string, number>; listHits?: ListHit[]; commands?: unknown[] }

// ── 流水与分析 ──

export interface DecisionRow {
  id: string;
  ts: string;
  appId: number;
  scene: string;
  userId: number;
  account: string;
  ip: string;
  deviceId: string;
  country: string;
  city: string;
  action: RiskAction;
  level: RiskLevel;
  score: number;
  release: string;
  hits: string[];
  reasons: string[];
  latencyUs: number;
  shadow: number;
}

export interface DecisionDetail {
  decision: DecisionRow;
  ua: string;
  province: string;
  isp: string;
  asn: number;
  features: Record<string, number>;
  models: Record<string, number>;
  extra: Record<string, unknown> | null;
  trace: Trace | null;
  device: Record<string, string> | null;
}

export interface KV { key: string; count: number }
export interface DashboardData {
  from: string;
  to: string;
  byAction?: KV[];
  byScene?: Array<{ scene: string; action: RiskAction; count: number }>;
  series?: Array<{ t: string; action: RiskAction; count: number }>;
  latency?: { avg: number; p50: number; p99: number };
  topRules?: KV[];
  topIPs?: KV[];
  topCountries?: KV[];
  alerts?: KV[];
  pendingCases?: number;
}

export interface EventRow {
  id: string; ts: string; type: string; scene: string; appId: number; userId: number; account: string; ip: string;
  deviceId: string; country: string; city: string; amount: number; targetUserId: number; props: string;
}

export interface AlertRow {
  id: string; ts: string; appId: number; pattern: string; patternName: string; entity: string; value: string;
  userId: number; deviceId: string; ip: string; severity: string; eventIds: string[]; detail: string;
}

export interface RiskCase {
  id: number; decisionId: string; appId: number; scene: string; userId: number; account: string; ip: string; deviceId: string;
  action: RiskAction; score: number; reasons: string[]; source: string; status: "pending" | "closed"; verdict: "" | "approve" | "reject";
  reviewer: string; note: string; createdAt: string; reviewedAt: string | null;
}

export interface DisposeInput {
  appId?: number; userId?: number; account?: string; deviceId?: string; ip?: string;
  kind: ListKind; targets: Array<"device" | "ip" | "account" | "user" | "ban_account" | "firewall">; durationSeconds: number; reason: string;
}

// ── 关系图与画像 ──

export type GraphNodeKind = "user" | "account" | "device" | "ip" | "subnet" | "phone" | "email" | "identity" | "payer" | "passkey" | "client";
export interface GraphNode { id: string; kind: GraphNodeKind; label: string; props: Record<string, unknown>; depth: number }
export interface GraphLink { source: string; target: string; kind: string; firstSeen: number; lastSeen: number; count: number }
export interface Subgraph { center: string; nodes: GraphNode[]; links: GraphLink[]; truncated: boolean }

export interface EntityUse {
  value: string; lastSeen: string; firstSeen: string; count: number; country: string; city: string;
  lists: ListEntry[] | null; fingerprint?: Record<string, string>; similar?: Array<{ deviceId: string; userId: number; banned: boolean; score: number }>;
}

export interface UserRiskProfile {
  appId: number;
  userId: number;
  days: number;
  graph?: Subgraph;
  timeline?: Array<{ day: string; action: RiskAction; count: number; maxScore: number; avgScore: number }>;
  summary?: {
    total: number; blocked: number; reviewed: number; captcha: number; maxScore: number; avgScore: number;
    scenes: number; devices: number; ips: number; firstSeen: string; lastSeen: string;
  };
  scenes?: KV[];
  rules?: KV[];
  decisions?: DecisionRow[];
  events?: EventRow[];
  alerts?: AlertRow[];
  features?: Array<{ scene: string; ts: string; features: Record<string, number>; models: Record<string, number> }>;
  cases?: RiskCase[];
  devices: EntityUse[];
  ips: EntityUse[];
  lists: ListEntry[];
  errors: Record<string, string>;
}

export interface RiskStatus {
  components: Record<string, string>;
  decision: { instance: string; scenes?: Array<{ scene: string; version: string; checksum: string; loaded_at: number; candidate?: string; canary_percent?: number; shadow?: boolean; error?: string }>; dependencies?: Record<string, string> } | null;
  spoolPending: number;
}

export interface AuditEntry { id: number; operator: string; action: string; target: string; detail: unknown; at: string }

// ════════════════════════ 接口 ════════════════════════

export const getRiskMeta = (t: string) => get<RiskMeta>(t, "/meta");
export const validateExpr = (t: string, expr: string) => send<{ ok: boolean; error?: string }>(t, "POST", "/expr/validate", { expr });

export const listScenes = (t: string) => get<SceneView[]>(t, "/scenes").then((r) => r ?? []);
export const getScene = (t: string, key: string) => get<SceneView>(t, `/scenes/${encodeURIComponent(key)}`);
export const saveScene = (t: string, key: string, scene: Partial<Scene>) => send<Scene>(t, "PUT", `/scenes/${encodeURIComponent(key)}`, scene);
export const deleteScene = (t: string, key: string) => send<void>(t, "DELETE", `/scenes/${encodeURIComponent(key)}`);
export const getDraftBundle = (t: string, key: string) => get<Record<string, unknown>>(t, `/scenes/${encodeURIComponent(key)}/bundle`);
export const validateScene = (t: string, key: string) => send<ValidateResult>(t, "POST", `/scenes/${encodeURIComponent(key)}/validate`);
/**
 * 决策服务的响应是 protobuf 结构直接转的 JSON：值为 0 / 空的字段会被省掉（omitempty），
 * 这里补回默认值，组件里不必处处判空。
 */
export function normalizeEval(r: Partial<EvalResponse> | undefined): EvalResponse {
  return {
    decision_id: r?.decision_id ?? "",
    action: r?.action ?? "pass",
    risk_level: r?.risk_level ?? "none",
    score: r?.score ?? 0,
    reasons: r?.reasons ?? [],
    hits: (r?.hits ?? []).map((h) => ({ ...h, score: h.score ?? 0, mode: h.mode ?? "enforce" })),
    dimension_scores: r?.dimension_scores ?? {},
    model_scores: r?.model_scores ?? {},
    list_hits: r?.list_hits ?? [],
    release: r?.release ?? "",
    latency_us: r?.latency_us ?? 0,
    decided_by: r?.decided_by ?? "default",
    commands: r?.commands ?? [],
    degraded: r?.degraded ?? [],
  };
}

export const simulateScene = (t: string, key: string, input: SimulateInput) =>
  send<SimulateResult>(t, "POST", `/scenes/${encodeURIComponent(key)}/simulate`, input).then((res) => ({ ...res, result: normalizeEval(res?.result) }));
export const publishScene = (t: string, key: string, input: { note: string; mode: "full" | "canary" | "shadow"; canaryPercent?: number }) =>
  send<PublishResult>(t, "POST", `/scenes/${encodeURIComponent(key)}/publish`, input);
export const promoteScene = (t: string, key: string) => send<ReleaseState>(t, "POST", `/scenes/${encodeURIComponent(key)}/promote`);
export const rollbackScene = (t: string, key: string, releaseId: number) => send<ReleaseState>(t, "POST", `/scenes/${encodeURIComponent(key)}/rollback`, { releaseId });
export const withdrawScene = (t: string, key: string) => send<ReleaseState>(t, "POST", `/scenes/${encodeURIComponent(key)}/withdraw`);
export const setSceneTraffic = (t: string, key: string, input: { canaryPercent?: number; shadow?: boolean; drop?: boolean }) =>
  send<ReleaseState>(t, "POST", `/scenes/${encodeURIComponent(key)}/traffic`, input);
export const listReleases = (t: string, key: string) => get<{ releases: Release[]; state?: ReleaseState }>(t, `/scenes/${encodeURIComponent(key)}/releases`);
export const getRelease = (t: string, id: number) => get<Release>(t, `/releases/${id}`);

export const listFeatures = (t: string) => get<Feature[]>(t, "/features").then((r) => r ?? []);
export const saveFeature = (t: string, key: string, f: Partial<Feature>) => send<Feature>(t, "PUT", `/features/${encodeURIComponent(key)}`, f);
export const deleteFeature = (t: string, key: string) => send<void>(t, "DELETE", `/features/${encodeURIComponent(key)}`);
export const getFeatureSQL = (t: string, key: string) => get<{ sql: string; object: { feature: string; base: string } }>(t, `/features/${encodeURIComponent(key)}/sql`);
export const syncStreams = (t: string) => send<StreamSyncResult>(t, "POST", "/features/sync");

export const listRules = (t: string, scene?: string) => get<Rule[]>(t, "/rules", { scene }).then((r) => r ?? []);
export const saveRule = (t: string, rule: Partial<Rule>) =>
  rule.id ? send<Rule>(t, "PUT", `/rules/${rule.id}`, rule) : send<Rule>(t, "POST", "/rules", rule);
export const deleteRule = (t: string, id: number) => send<void>(t, "DELETE", `/rules/${id}`);
export const listTemplates = (t: string) => get<RuleTemplate[]>(t, "/templates").then((r) => r ?? []);
export const applyTemplate = (t: string, input: { template: string; scene: string; observe: boolean }) =>
  send<{ installed: number; skipped: number }>(t, "POST", "/templates/apply", input);

export const listModels = (t: string) => get<RiskModel[]>(t, "/models").then((r) => r ?? []);
export const uploadModel = (t: string, form: FormData) => apiRequest<RiskModel>(`${P}/models`, { method: "POST", token: t, body: form });
export const updateModel = (t: string, key: string, input: { name?: string; description?: string; inputs?: string[]; transform?: boolean }) =>
  send<RiskModel>(t, "PUT", `/models/${encodeURIComponent(key)}`, input);
export const deleteModel = (t: string, key: string) => send<void>(t, "DELETE", `/models/${encodeURIComponent(key)}`);
export const predictModel = (t: string, key: string, values: Record<string, number>) =>
  send<{ score: number; inputs: string[]; values: number[] }>(t, "POST", `/models/${encodeURIComponent(key)}/predict`, { values });

export const listEntries = (t: string, q: { type?: string; kind?: string; value?: string; app?: number; source?: string; active?: boolean; page?: number; limit?: number }) =>
  get<Paged<ListEntry>>(t, "/lists", q);
export const addEntries = (t: string, input: { listType: ListType; kind: ListKind; values: string[]; appId?: number; scenes?: string[]; reason?: string; durationSeconds?: number }) =>
  send<{ added: number; invalid: string[] | null }>(t, "POST", "/lists", input);
export const removeEntries = (t: string, ids: string[]) => send<{ removed: number }>(t, "POST", "/lists/remove", { ids });
export const checkLists = (t: string, input: { appId?: number; scene?: string; values: Partial<Record<ListType, string>> }) =>
  send<ListHit[] | null>(t, "POST", "/lists/check", input).then((r) => r ?? []);
export const rebuildLists = (t: string) => send<{ entries: number }>(t, "POST", "/lists/rebuild");

export const listPatterns = (t: string) => get<CepPattern[]>(t, "/cep/patterns").then((r) => r ?? []);
export const savePattern = (t: string, key: string, p: Partial<CepPattern>) => send<CepPattern>(t, "PUT", `/cep/patterns/${encodeURIComponent(key)}`, p);
export const deletePattern = (t: string, key: string) => send<void>(t, "DELETE", `/cep/patterns/${encodeURIComponent(key)}`);

export const getDashboard = (t: string, q: { app?: number; from?: string; to?: string }) => get<DashboardData>(t, "/dashboard", q);
export const listDecisions = (t: string, q: { app?: number; scene?: string; action?: string; user?: number; device?: string; ip?: string; account?: string; rule?: string; release?: string; shadow?: boolean; from?: string; to?: string; page?: number; limit?: number }) =>
  get<Paged<DecisionRow>>(t, "/decisions", q);
export const getDecision = (t: string, id: string) => get<DecisionDetail>(t, `/decisions/${encodeURIComponent(id)}`);
export const listEvents = (t: string, q: { app?: number; user?: number; type?: string; device?: string; ip?: string; from?: string; to?: string; limit?: number }) =>
  get<EventRow[]>(t, "/events", q).then((r) => r ?? []);
export const listAlerts = (t: string, q: { app?: number; pattern?: string; user?: number; from?: string; to?: string; limit?: number }) =>
  get<AlertRow[]>(t, "/alerts", q).then((r) => r ?? []);

export const listCases = (t: string, q: { status?: string; app?: number; user?: number; scene?: string; page?: number; limit?: number }) =>
  get<Paged<RiskCase>>(t, "/cases", q);
export const reviewCase = (t: string, id: number, input: { verdict: "approve" | "reject"; note: string; allow?: boolean; dispose?: Omit<DisposeInput, "appId" | "userId" | "account" | "deviceId" | "ip"> }) =>
  send<{ case: number; dispose: Record<string, string> | null }>(t, "POST", `/cases/${id}/review`, input);
export const dispose = (t: string, input: DisposeInput) => send<Record<string, string>>(t, "POST", "/dispose", input);

/** 图数据兜底：没有边或点时服务端可能给 null，界面统一按空数组处理 */
export function normalizeGraph(g: Partial<Subgraph> | null | undefined): Subgraph {
  return { center: g?.center ?? "", nodes: g?.nodes ?? [], links: g?.links ?? [], truncated: g?.truncated ?? false };
}

export const getGraph = (t: string, q: { type: "user" | "device" | "ip" | "account" | "vid"; app?: number; user?: number; value?: string; depth?: number; fanout?: number; max?: number; days?: number; subnets?: 0 | 1 }) =>
  get<Subgraph>(t, "/graph", q).then(normalizeGraph);
export const getUserProfile = (t: string, app: number, user: number, days = 30) =>
  get<UserRiskProfile>(t, "/profile", { app, user, days }).then((p) => ({
    ...p,
    graph: p?.graph ? normalizeGraph(p.graph) : undefined,
    timeline: p?.timeline ?? [],
  }));
export const getRiskStatus = (t: string) => get<RiskStatus>(t, "/status");
export const listAudit = (t: string, limit = 100) => get<AuditEntry[]>(t, "/audit", { limit }).then((r) => r ?? []);
