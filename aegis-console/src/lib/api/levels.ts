import { apiRequest, buildQuery } from "@/lib/api/client";
import type {
  ItemGrantConditionRule,
  ItemGrantFeatureRef,
  ItemGrantFieldSpec,
  ItemGrantGraph,
  ItemGrantIssue,
  ItemGrantItem,
  ItemGrantItemResult,
  ItemGrantItemSpec,
  ItemGrantOperatorSpec,
  ItemGrantOption,
  ItemGrantPlanRef
} from "@/lib/api/item-grant";

/**
 * 等级体系 API（管理端，docs/levels.md）。
 *
 * 每个应用一套等级体系，是一张画布（与物品发放同形：nodes / edges / viewport）：体系设置 → 等级 → 晋级条件 → 等级 …，
 * 等级上挂达成奖励与等级权益。草稿与已发布分开存，发布留版本。节点、权益、图标、物品、晋级条件字段全部由后端目录下发。
 * 等级号不存，按主干顺序编排；校验结果里的 `levels` 是编译后的逐级展开（画布上的 Lv.N 从这里来）。
 */

// ── 目录 ──

export type LevelNodeType = "scheme" | "level" | "gate" | "reward" | "privilege";

export type LevelHandleSpec = { id: string; label: string; tone: "positive" | "negative" | "neutral" };

export type LevelNodeSpec = {
  type: LevelNodeType;
  label: string;
  hint: string;
  category: string;
  outputs: LevelHandleSpec[];
  inputs: boolean;
  single?: boolean;
};

export type LevelPrivilegeKind = "exp_boost" | "integral_boost" | "display" | "custom";

export type LevelPrivilegeSpec = {
  kind: LevelPrivilegeKind;
  label: string;
  hint: string;
  numeric: boolean;
  min?: number;
  max?: number;
  unit?: string;
  needsKey?: boolean;
};

export type LevelIconSpec = { key: string; label: string };

export type LevelTemplate = { key: string; name: string; description: string; graph: ItemGrantGraph };

export type LevelCatalog = {
  nodes: LevelNodeSpec[];
  categories: ItemGrantOption[];
  privileges: LevelPrivilegeSpec[];
  icons: LevelIconSpec[];
  items: ItemGrantItemSpec[];
  itemGroups: ItemGrantOption[];
  fields: ItemGrantFieldSpec[];
  operators: ItemGrantOperatorSpec[];
  routes: ItemGrantOption[];
  plans: ItemGrantPlanRef[];
  features: ItemGrantFeatureRef[];
  templates: LevelTemplate[];
  placeholders: ItemGrantOption[];
  limits: Record<string, number>;
  timezone: string;
};

// ── 节点配置 ──

export type LevelNodeMeta = { label?: string; note?: string };

export type LevelSchemeData = LevelNodeMeta & {
  notify?: boolean;
  notifyTitle?: string;
  notifyContent?: string;
  notifyRoute?: string;
  hideLocked?: boolean;
};

export type LevelLevelData = LevelNodeMeta & {
  threshold: number;
  icon?: string;
  color?: string;
  description?: string;
};

export type LevelGateData = LevelNodeMeta & {
  mode: "all" | "any" | "expr";
  rules?: ItemGrantConditionRule[];
  expr?: string;
  hint?: string;
};

export type LevelClaim = "auto" | "manual";

export type LevelRewardData = LevelNodeMeta & { items: ItemGrantItem[]; claim: LevelClaim };

export type LevelScope = "inherit" | "only";

export type LevelPrivilegeData = LevelNodeMeta & {
  kind: LevelPrivilegeKind;
  value?: number;
  key?: string;
  text?: string;
  icon?: string;
  scope?: LevelScope;
};

// ── 编译结果 ──

export type LevelReward = { nodeId: string; label: string; items: ItemGrantItem[]; claim: LevelClaim };

export type LevelPrivilege = {
  nodeId: string;
  kind: LevelPrivilegeKind;
  label: string;
  key?: string;
  value?: number;
  text?: string;
  icon?: string;
  scope: LevelScope;
  level: number;
};

export type LevelGateRow = { nodeId: string; label?: string; hint?: string; expr?: string };

export type LevelRow = {
  level: number;
  nodeId: string;
  name: string;
  threshold: number;
  icon?: string;
  color?: string;
  description?: string;
  gate?: LevelGateRow;
  rewards: LevelReward[];
  privileges: LevelPrivilege[];
  effective: LevelPrivilege[];
};

export type LevelSummary = {
  levels: number;
  maxThreshold: number;
  rewards: number;
  privileges: number;
  gates: number;
  name?: string;
  firstLevel?: string;
  topLevel?: string;
  manualRewards: number;
};

export type LevelSchemeView = {
  appid: number;
  draft: ItemGrantGraph;
  draftRevision: number;
  draftUpdatedBy?: string;
  draftUpdatedAt?: string;
  publishedVersion: number;
  publishedBy?: string;
  publishedAt?: string;
  dirty: boolean;
  isDefault: boolean;
  issues: ItemGrantIssue[];
  levels: LevelRow[];
  summary: LevelSummary;
};

export type LevelValidation = { issues: ItemGrantIssue[]; levels: LevelRow[]; summary: LevelSummary; valid: boolean };

export type LevelJobStatus = "running" | "succeeded" | "failed" | "cancelled";

export type LevelJob = {
  id: number;
  appid: number;
  kind: "recalc";
  params: { downgrade?: boolean };
  status: LevelJobStatus;
  schemeVersion: number;
  total: number;
  processed: number;
  changed: number;
  cursor: number;
  message?: string;
  createdBy?: string;
  heartbeatAt: string;
  createdAt: string;
  finishedAt?: string;
};

export type LevelPublishResult = { version: number; issues: ItemGrantIssue[]; job?: LevelJob };

export type LevelVersion = {
  version: number;
  summary: LevelSummary;
  note?: string;
  publishedBy?: string;
  publishedAt: string;
  graph?: ItemGrantGraph;
};

export type LevelGateTrace = { level: number; nodeId: string; label?: string; passed: boolean; error?: string };

export type LevelSimulation = {
  experience: number;
  level: number;
  name: string;
  progress: number;
  toNext: number;
  blocked?: LevelGateRow;
  effective: LevelPrivilege[];
  rewards: LevelReward[];
  trace?: LevelGateTrace[];
};

// ── 用户与记录 ──

export type LevelUser = {
  userId: number;
  account: string;
  nickname?: string;
  avatar?: string;
  level: number;
  levelName?: string;
  experience: number;
  progress: number;
  highestLevel: number;
  levelUpCount: number;
  lastLevelUpAt?: string;
  updatedAt?: string;
  /** 管理员锁定的保底等级（「重算并允许降级」也不降到它以下），0 = 没有锁定 */
  lockedLevel: number;
  claimable: number;
};

export type LevelUserParams = {
  level?: number;
  minLevel?: number;
  maxLevel?: number;
  keyword?: string;
  sort?: "level" | "experience" | "recent";
  page?: number;
  limit?: number;
};

export type LevelEventReason = "experience" | "sync" | "recalc" | "admin" | "penalty";

export type LevelNotifyStatus = "none" | "pending" | "sending" | "sent" | "failed";

export type LevelEvent = {
  id: number;
  appid: number;
  userId: number;
  account?: string;
  nickname?: string;
  fromLevel: number;
  toLevel: number;
  fromName?: string;
  toName?: string;
  experience: number;
  reason: LevelEventReason;
  schemeVersion: number;
  operator?: string;
  note?: string;
  notifyStatus: LevelNotifyStatus;
  createdAt: string;
};

export type LevelEventParams = {
  userId?: number;
  reason?: string;
  level?: number;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type LevelRewardStatus = "queued" | "claimable" | "granted" | "pending" | "failed";

export type LevelRewardRecord = {
  id: number;
  appid: number;
  userId: number;
  account?: string;
  nickname?: string;
  level: number;
  nodeId: string;
  label: string;
  items: ItemGrantItem[];
  results: ItemGrantItemResult[];
  itemsText: string[];
  claim: LevelClaim;
  status: LevelRewardStatus;
  error?: string;
  attempts: number;
  schemeVersion: number;
  origin: "level_up" | "backfill";
  grantedAt?: string;
  handledBy?: string;
  handledNote?: string;
  createdAt: string;
  updatedAt: string;
};

export type LevelRewardParams = {
  userId?: number;
  status?: string;
  level?: number;
  nodeId?: string;
  page?: number;
  limit?: number;
};

export type LevelPage<T> = { items: T[]; total: number; page: number; limit: number };

export type LevelInfo = {
  currentLevel: number;
  currentLevelName: string;
  expMultiplier: number;
  totalExperience: number;
  expInCurrentLevel: number;
  expRangeForLevel: number;
  expToNextLevel: number;
  levelProgress: number;
  nextLevel?: number;
  nextLevelName?: string;
  highestLevel: number;
  levelUpCount: number;
  lastLevelUpAt?: string;
  isMaxLevel: boolean;
  icon?: string;
  color?: string;
  maxLevel: number;
  privileges: Array<Omit<LevelPrivilege, "nodeId" | "scope">>;
  claimableRewards: number;
  blockedBy?: { label?: string; hint?: string };
  schemeName?: string;
  schemeVersion: number;
};

export type LevelUserDetail = {
  user: LevelUser;
  info: LevelInfo;
  events: LevelEvent[];
  rewards: LevelRewardRecord[];
};

export type LevelAdjustResult = { user: LevelUser; from: number; to: number; addExperience: number };

export type LevelOverview = {
  version: number;
  summary: LevelSummary;
  totalUsers: number;
  rankedUsers: number;
  distribution: Array<{ level: number; name: string; icon?: string; color?: string; threshold: number; users: number }>;
  levelUpsToday: number;
  levelUps7d: number;
  trend: Array<{ date: string; levelUps: number; rewards: number }>;
  rewards: Record<LevelRewardStatus, number>;
  runningJob?: LevelJob;
  averageLevel: number;
};

// ── 请求 ──

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/levels`;

export function getLevelCatalog(token: string, appKey: string) {
  return apiRequest<LevelCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getLevelOverview(token: string, appKey: string, days: number) {
  return apiRequest<LevelOverview>(`${base(appKey)}/overview${buildQuery({ days })}`, { token });
}

export function getLevelScheme(token: string, appKey: string) {
  return apiRequest<LevelSchemeView>(`${base(appKey)}/scheme`, { token });
}

export function saveLevelScheme(token: string, appKey: string, graph: ItemGrantGraph, revision: number) {
  return apiRequest<LevelSchemeView>(`${base(appKey)}/scheme`, { token, method: "PUT", body: JSON.stringify({ graph, revision }) });
}

export function validateLevelScheme(token: string, appKey: string, graph: ItemGrantGraph) {
  return apiRequest<LevelValidation>(`${base(appKey)}/validate`, { token, method: "POST", body: JSON.stringify({ graph }) });
}

export type LevelPublishPayload = { graph: ItemGrantGraph; note?: string; recalc?: boolean; downgrade?: boolean };

export function publishLevelScheme(token: string, appKey: string, payload: LevelPublishPayload) {
  return apiRequest<LevelPublishResult>(`${base(appKey)}/publish`, { token, method: "POST", body: JSON.stringify(payload) });
}

export type LevelSimulatePayload = { graph?: ItemGrantGraph; experience?: number; user?: string };

export function simulateLevel(token: string, appKey: string, payload: LevelSimulatePayload) {
  return apiRequest<LevelSimulation>(`${base(appKey)}/simulate`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function listLevelVersions(token: string, appKey: string) {
  return apiRequest<LevelVersion[]>(`${base(appKey)}/versions`, { token });
}

export function getLevelVersion(token: string, appKey: string, version: number) {
  return apiRequest<LevelVersion>(`${base(appKey)}/versions/${version}`, { token });
}

export function restoreLevelVersion(token: string, appKey: string, version: number) {
  return apiRequest<LevelSchemeView>(`${base(appKey)}/versions/${version}/restore`, { token, method: "POST" });
}

export function listLevelUsers(token: string, appKey: string, params: LevelUserParams) {
  return apiRequest<LevelPage<LevelUser>>(`${base(appKey)}/users${buildQuery(params)}`, { token });
}

export function getLevelUser(token: string, appKey: string, userId: number) {
  return apiRequest<LevelUserDetail>(`${base(appKey)}/users/${userId}`, { token });
}

/**
 * 强制升级：level 越过经验门槛与晋级条件直接调到至少这一级（0 = 只按当前体系补判）；
 * topUp 把经验补到门槛；lock 把结果锁定为保底、unlock 解除锁定；skipRewards 不发经过的达成奖励；silent 不发升级通知。
 */
export type LevelAdjustPayload = {
  level: number;
  topUp?: boolean;
  lock?: boolean;
  unlock?: boolean;
  skipRewards?: boolean;
  silent?: boolean;
  note?: string;
};

export type LevelBatchAdjustPayload = LevelAdjustPayload & { userIds: number[] };

export type LevelBatchAdjustResult = {
  total: number;
  succeeded: number;
  changed: number;
  failed: number;
  items: Array<{ userId: number; account?: string; from: number; to: number; addExperience: number; error?: string }>;
};

/** 一次批量强制升级最多的人数（与服务端一致） */
export const LEVEL_BATCH_ADJUST_MAX = 500;

export function adjustLevelUser(token: string, appKey: string, userId: number, payload: LevelAdjustPayload) {
  return apiRequest<LevelAdjustResult>(`${base(appKey)}/users/${userId}/adjust`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function adjustLevelUsers(token: string, appKey: string, payload: LevelBatchAdjustPayload) {
  return apiRequest<LevelBatchAdjustResult>(`${base(appKey)}/users/adjust`, { token, method: "POST", body: JSON.stringify(payload) });
}

export function listLevelEvents(token: string, appKey: string, params: LevelEventParams) {
  return apiRequest<LevelPage<LevelEvent>>(`${base(appKey)}/events${buildQuery(params)}`, { token });
}

export function listLevelRewards(token: string, appKey: string, params: LevelRewardParams) {
  return apiRequest<LevelPage<LevelRewardRecord>>(`${base(appKey)}/rewards${buildQuery(params)}`, { token });
}

export function retryLevelReward(token: string, appKey: string, rewardId: number) {
  return apiRequest<LevelRewardRecord>(`${base(appKey)}/rewards/${rewardId}/retry`, { token, method: "POST" });
}

export function fulfillLevelReward(token: string, appKey: string, rewardId: number, note: string) {
  return apiRequest<LevelRewardRecord>(`${base(appKey)}/rewards/${rewardId}/fulfill`, { token, method: "POST", body: JSON.stringify({ note }) });
}

export function backfillLevelReward(token: string, appKey: string, nodeId: string) {
  return apiRequest<{ created: number }>(`${base(appKey)}/rewards/backfill`, { token, method: "POST", body: JSON.stringify({ nodeId }) });
}

export function listLevelJobs(token: string, appKey: string) {
  return apiRequest<LevelJob[]>(`${base(appKey)}/jobs`, { token });
}

export function startLevelJob(token: string, appKey: string, downgrade: boolean) {
  return apiRequest<LevelJob>(`${base(appKey)}/jobs`, { token, method: "POST", body: JSON.stringify({ downgrade }) });
}

export function cancelLevelJob(token: string, appKey: string, jobId: number) {
  return apiRequest<LevelJob>(`${base(appKey)}/jobs/${jobId}/cancel`, { token, method: "POST" });
}
