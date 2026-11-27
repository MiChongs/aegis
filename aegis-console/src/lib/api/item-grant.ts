import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 物品发放域 API（管理端，docs/item-grant.md）。
 *
 * 规则是一张画布（与 React Flow 同形：nodes / edges / viewport），原样保存原样取回。
 * 物品、节点、触发方式、事件、条件字段与运算符全部由后端目录（`/item-grants/catalog`）下发，前端不另抄一份。
 * 判定只在服务端：预演（`/preview`）与正式执行走同一个判定函数，画布上看到的路径就是执行时会走的路径。
 */

// ── 目录 ──

/** `number_ticket`：靓号券，`amount` 张数 + `refId` 等级 1–4（docs/user-number.md「发放渠道」） */
export type ItemGrantValue = "" | "amount" | "money" | "ref" | "feature" | "feature_minutes" | "text" | "number_ticket" | "cosmetic" | "coupon";

export type ItemGrantItemSpec = {
  type: string;
  label: string;
  hint: string;
  group: string;
  value: ItemGrantValue;
  unit?: string;
  min?: number;
  max?: number;
  /** 数量可以写成按人计算的表达式 */
  scalable?: boolean;
  needsLoginCard?: boolean;
  /** 不自动到账，记录进入待发放 */
  pending?: boolean;
};

export type ItemGrantOption = { key: string; label: string };

export type ItemGrantHandleSpec = { id: string; label: string; tone: "positive" | "negative" | "neutral" };

export type ItemGrantNodeType =
  | "trigger"
  | "audience"
  | "condition"
  | "window"
  | "split"
  | "limit"
  | "grant"
  | "random"
  | "notify"
  | "end";

export type ItemGrantNodeSpec = {
  type: ItemGrantNodeType;
  label: string;
  hint: string;
  category: string;
  outputs: ItemGrantHandleSpec[];
  dynamicOutputs?: boolean;
  singleton?: boolean;
  noInput?: boolean;
};

export type ItemGrantTriggerKind = "manual" | "schedule" | "event";

export type ItemGrantFieldSpec = {
  key: string;
  label: string;
  group: string;
  kind: "number" | "bool";
  unit?: string;
};

export type ItemGrantEventSpec = {
  key: string;
  label: string;
  hint: string;
  fields: ItemGrantFieldSpec[];
};

export type ItemGrantOperatorSpec = {
  op: string;
  label: string;
  kinds: Array<"number" | "bool">;
  /** 0 不需要取值、1 一个、2 区间、-1 列表 */
  arity: number;
};

export type ItemGrantPlanRef = {
  id: number;
  name: string;
  durationDays: number;
  lifetime: boolean;
  trial: boolean;
  active: boolean;
};

export type ItemGrantFeatureRef = { tag: string; name: string; isActive: boolean };

export type ItemGrantTemplate = {
  key: string;
  name: string;
  description: string;
  graph: ItemGrantGraph;
};

export type ItemGrantCatalog = {
  items: ItemGrantItemSpec[];
  itemGroups: ItemGrantOption[];
  nodes: ItemGrantNodeSpec[];
  categories: ItemGrantOption[];
  triggers: Array<{ kind: ItemGrantTriggerKind; label: string; hint: string }>;
  events: ItemGrantEventSpec[];
  fields: ItemGrantFieldSpec[];
  operators: ItemGrantOperatorSpec[];
  scopes: ItemGrantOption[];
  periods: ItemGrantOption[];
  routes: ItemGrantOption[];
  plans: ItemGrantPlanRef[];
  features: ItemGrantFeatureRef[];
  limits: Record<string, number>;
  timezone: string;
  statuses: ItemGrantStatus[];
  templates: ItemGrantTemplate[];
};

// ── 画布 ──

export type ItemGrantItem = {
  type: string;
  amount?: number;
  /** 金额按字符串：JSON number 是双精度浮点 */
  money?: string;
  refId?: number;
  tag?: string;
  text?: string;
  amountExpr?: string;
  /** 个性装扮：发放后顺手戴上 */
  equip?: boolean;
};

export type ItemGrantNodeMeta = { label?: string; note?: string };

export type ItemGrantTriggerData = ItemGrantNodeMeta & { kind: ItemGrantTriggerKind; cron?: string; event?: string };
export type ItemGrantAudienceData = ItemGrantNodeMeta & {
  scope: string;
  registeredWithinDays?: number;
  registeredBeforeDays?: number;
  activeWithinDays?: number;
  minLevel?: number;
  maxLevel?: number;
  userIds?: number[];
  excludeUserIds?: number[];
};
export type ItemGrantConditionRule = { field: string; op: string; value?: number[] };
export type ItemGrantConditionData = ItemGrantNodeMeta & {
  mode: "all" | "any" | "expr";
  rules?: ItemGrantConditionRule[];
  expr?: string;
};
export type ItemGrantWindowData = ItemGrantNodeMeta & {
  weekdays?: number[];
  startMinute: number;
  endMinute: number;
  dateFrom?: string;
  dateTo?: string;
};
export type ItemGrantSplitBranch = { id: string; label: string; weight: number };
export type ItemGrantSplitData = ItemGrantNodeMeta & { branches: ItemGrantSplitBranch[]; sticky: boolean };
export type ItemGrantLimitData = ItemGrantNodeMeta & { period: string; perUser?: number; global?: number };
export type ItemGrantGrantData = ItemGrantNodeMeta & { items: ItemGrantItem[] };
export type ItemGrantRandomOption = { id: string; label: string; weight: number; items: ItemGrantItem[] };
export type ItemGrantRandomData = ItemGrantNodeMeta & { picks: number; options: ItemGrantRandomOption[] };
export type ItemGrantNotifyData = ItemGrantNodeMeta & { title: string; content: string; route?: string };

export type ItemGrantNodeData =
  | ItemGrantTriggerData
  | ItemGrantAudienceData
  | ItemGrantConditionData
  | ItemGrantWindowData
  | ItemGrantSplitData
  | ItemGrantLimitData
  | ItemGrantGrantData
  | ItemGrantRandomData
  | ItemGrantNotifyData
  | ItemGrantNodeMeta;

export type ItemGrantGraphNode = {
  id: string;
  type: ItemGrantNodeType;
  position: { x: number; y: number };
  data: Record<string, unknown>;
};

export type ItemGrantGraphEdge = {
  id: string;
  source: string;
  sourceHandle?: string;
  target: string;
};

export type ItemGrantGraph = {
  nodes: ItemGrantGraphNode[];
  edges: ItemGrantGraphEdge[];
  viewport?: { x: number; y: number; zoom: number };
};

export type ItemGrantIssue = {
  level: "error" | "warning";
  nodeId?: string;
  edgeId?: string;
  message: string;
};

// ── 规则、执行、记录 ──

export type ItemGrantStatus = "draft" | "active" | "paused" | "archived";
export type ItemGrantRunStatus = "running" | "succeeded" | "failed" | "cancelled";
export type ItemGrantRecordStatus = "granted" | "pending" | "fulfilled" | "failed";

export type ItemGrantRuleStats = {
  records: number;
  users: number;
  today: number;
  pending: number;
  failed: number;
  lastGrantAt?: string;
};

export type ItemGrantRun = {
  id: number;
  appid: number;
  ruleId: number;
  ruleName?: string;
  ruleVersion: number;
  trigger: ItemGrantTriggerKind;
  status: ItemGrantRunStatus;
  total: number;
  processed: number;
  granted: number;
  skipped: number;
  failed: number;
  cursor: number;
  error?: string;
  operator?: string;
  startedAt: string;
  heartbeatAt: string;
  finishedAt?: string;
  graph?: ItemGrantGraph;
};

export type ItemGrantRule = {
  id: number;
  appid: number;
  name: string;
  description: string;
  status: ItemGrantStatus;
  trigger: ItemGrantTriggerKind;
  event?: string;
  cron?: string;
  startsAt?: string;
  endsAt?: string;
  graph: ItemGrantGraph;
  version: number;
  nextRunAt?: string;
  lastRunAt?: string;
  createdBy?: string;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
  stats?: ItemGrantRuleStats;
  running?: ItemGrantRun;
};

export type ItemGrantStep = { nodeId: string; type: ItemGrantNodeType; handle?: string; detail: string };
export type ItemGrantPlannedItem = { nodeId: string; optionId?: string; item: ItemGrantItem };
export type ItemGrantPlannedNotify = { nodeId: string; title: string; content: string; route?: string };

export type ItemGrantOutcome = {
  steps: ItemGrantStep[];
  items: ItemGrantPlannedItem[];
  notifies: ItemGrantPlannedNotify[];
  limits: string[];
  blocked?: string;
};

export type ItemGrantItemResult = {
  nodeId: string;
  optionId?: string;
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
  expireAt?: string;
  converted?: boolean;
  pending?: boolean;
};

export type ItemGrantRecord = {
  id: number;
  recordNo: string;
  appid: number;
  ruleId: number;
  ruleName?: string;
  ruleVersion: number;
  runId?: number;
  userId: number;
  account?: string;
  nickname?: string;
  trigger: ItemGrantTriggerKind;
  event?: string;
  status: ItemGrantRecordStatus;
  steps: ItemGrantStep[];
  items: ItemGrantPlannedItem[];
  results: ItemGrantItemResult[];
  error?: string;
  eventData?: Record<string, unknown>;
  note?: string;
  handledBy?: string;
  handledAt?: string;
  createdAt: string;
};

export type ItemGrantPage<T> = { items: T[]; total: number; page: number; limit: number; totalPages: number };

export type ItemGrantBasics = {
  userId: number;
  account: string;
  nickname: string;
  registeredAt: string;
  member: boolean;
  lifetime: boolean;
  level: number;
  lastActiveAt?: string;
  eligible: boolean;
};

export type ItemGrantEstimateItem = {
  type: string;
  label: string;
  detail: string;
  count: number;
  amount: number;
  projected: number;
};

export type ItemGrantEstimate = {
  candidates: number;
  sampled: number;
  granting: number;
  nodeHits: Record<string, number>;
  handleHits: Record<string, number>;
  items: ItemGrantEstimateItem[];
  blocked: number;
};

export type ItemGrantPreview = {
  issues: ItemGrantIssue[];
  trace?: { user: ItemGrantBasics; outcome: ItemGrantOutcome; items: string[] };
  estimate?: ItemGrantEstimate;
};

export type ItemGrantExecuteResult = {
  status: "granted" | "pending" | "failed" | "skipped" | "duplicate";
  outcome: ItemGrantOutcome;
  record?: ItemGrantRecord;
  error?: string;
};

export type ItemGrantOverview = {
  rules: number;
  activeRules: number;
  records: number;
  users: number;
  today: number;
  pending: number;
  failed: number;
  running: number;
  trend: Array<{ date: string; records: number; users: number }>;
  byItem: Array<{ type: string; label: string; count: number; amount: number }>;
};

export type ItemGrantRulePayload = {
  name: string;
  description?: string;
  startsAt?: string | null;
  endsAt?: string | null;
  graph: ItemGrantGraph;
};

export type ItemGrantPreviewPayload = {
  ruleId?: number;
  graph: ItemGrantGraph;
  user?: string;
  eventData?: Record<string, unknown>;
  samples?: number;
};

export type ItemGrantRecordParams = {
  ruleId?: number;
  runId?: number;
  userId?: number;
  status?: ItemGrantRecordStatus;
  keyword?: string;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type ItemGrantRunParams = { ruleId?: number; page?: number; limit?: number };

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/item-grants`;

export function getItemGrantCatalog(token: string, appKey: string) {
  return apiRequest<ItemGrantCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getItemGrantOverview(token: string, appKey: string, days: number) {
  return apiRequest<ItemGrantOverview>(`${base(appKey)}/overview${buildQuery({ days })}`, { token });
}

export function listItemGrantRules(token: string, appKey: string) {
  return apiRequest<ItemGrantRule[]>(`${base(appKey)}/rules`, { token });
}

export function getItemGrantRule(token: string, appKey: string, ruleId: number) {
  return apiRequest<ItemGrantRule>(`${base(appKey)}/rules/${ruleId}`, { token });
}

export function createItemGrantRule(token: string, appKey: string, payload: ItemGrantRulePayload) {
  return apiRequest<ItemGrantRule>(`${base(appKey)}/rules`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateItemGrantRule(token: string, appKey: string, ruleId: number, payload: ItemGrantRulePayload) {
  return apiRequest<ItemGrantRule>(`${base(appKey)}/rules/${ruleId}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function deleteItemGrantRule(token: string, appKey: string, ruleId: number) {
  return apiRequest<null>(`${base(appKey)}/rules/${ruleId}`, { method: "DELETE", token });
}

export function setItemGrantRuleStatus(token: string, appKey: string, ruleId: number, status: ItemGrantStatus) {
  return apiRequest<ItemGrantRule>(`${base(appKey)}/rules/${ruleId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

export function runItemGrantRule(token: string, appKey: string, ruleId: number) {
  return apiRequest<ItemGrantRun>(`${base(appKey)}/rules/${ruleId}/run`, { method: "POST", token });
}

export function validateItemGrant(token: string, appKey: string, graph: ItemGrantGraph) {
  return apiRequest<{ issues: ItemGrantIssue[]; valid: boolean }>(`${base(appKey)}/validate`, {
    method: "POST",
    token,
    body: JSON.stringify({ graph })
  });
}

export function previewItemGrant(token: string, appKey: string, payload: ItemGrantPreviewPayload) {
  return apiRequest<ItemGrantPreview>(`${base(appKey)}/preview`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function listItemGrantRuns(token: string, appKey: string, params?: ItemGrantRunParams) {
  return apiRequest<ItemGrantPage<ItemGrantRun>>(`${base(appKey)}/runs${buildQuery(params ?? {})}`, { token });
}

export function getItemGrantRun(token: string, appKey: string, runId: number) {
  return apiRequest<ItemGrantRun>(`${base(appKey)}/runs/${runId}`, { token });
}

export function cancelItemGrantRun(token: string, appKey: string, runId: number) {
  return apiRequest<ItemGrantRun>(`${base(appKey)}/runs/${runId}/cancel`, { method: "POST", token });
}

export function listItemGrantRecords(token: string, appKey: string, params?: ItemGrantRecordParams) {
  return apiRequest<ItemGrantPage<ItemGrantRecord>>(`${base(appKey)}/records${buildQuery(params ?? {})}`, { token });
}

export function getItemGrantRecord(token: string, appKey: string, recordId: number) {
  return apiRequest<ItemGrantRecord>(`${base(appKey)}/records/${recordId}`, { token });
}

export function fulfillItemGrantRecord(token: string, appKey: string, recordId: number, note: string) {
  return apiRequest<ItemGrantRecord>(`${base(appKey)}/records/${recordId}/fulfill`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

export function retryItemGrantRecord(token: string, appKey: string, recordId: number) {
  return apiRequest<ItemGrantExecuteResult>(`${base(appKey)}/records/${recordId}/retry`, { method: "POST", token });
}
