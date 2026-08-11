import { apiRequest, buildQuery } from "./client";

// 平台治理 API（/api/admin/platform/*）
//
// 与 /api/admin/apps/{appKey}/* 的分工：那一组是**应用作用域**（应用管理员改自己的配置），
// 这一组是**全局作用域**（平台管理员改治理结论）。后端对 /api/admin/platform/ 前缀
// 恒按全局作用域鉴权，因此应用级管理员即使被授了 platform:app:govern 也进不来 ——
// 不存在「被冻结的应用自己给自己解封」。

export type GovernanceState =
  | "active"
  | "restricted"
  | "frozen"
  | "suspended"
  | "banned"
  | "archived";

export type GovernanceAction =
  | "restrict"
  | "freeze"
  | "suspend"
  | "ban"
  | "archive"
  | "restore"
  | "update"
  | "expire"
  | "appeal_approved"
  | "revoke_sessions";

export type GovernanceAppealStatus = "none" | "pending" | "approved" | "rejected" | "withdrawn";

export type GovernanceRestrictions = {
  blockLogin: boolean;
  blockRegister: boolean;
  blockApi: boolean;
  blockPayment: boolean;
  blockStorage: boolean;
  blockNotification: boolean;
  blockAdminWrite: boolean;
};

export type Governance = {
  appid: number;
  appName?: string;
  appKey?: string;
  state: GovernanceState;
  reason?: string;
  restrictions: GovernanceRestrictions;
  evidence?: Record<string, unknown>;
  startAt?: string;
  endAt?: string;
  operatorAdminId?: number;
  operatorName?: string;
  lastAction?: string;
  appealStatus: GovernanceAppealStatus;
  createdAt?: string;
  updatedAt?: string;
};

export type GovernanceActionRecord = {
  id: number;
  appid: number;
  appName?: string;
  appKey?: string;
  action: GovernanceAction;
  fromState: GovernanceState;
  toState: GovernanceState;
  reason?: string;
  restrictions: GovernanceRestrictions;
  evidence?: Record<string, unknown>;
  endAt?: string;
  operatorAdminId?: number;
  operatorName?: string;
  operatorIp?: string;
  revokedSessions: number;
  createdAt: string;
};

export type GovernanceAppeal = {
  id: number;
  appid: number;
  appName?: string;
  appKey?: string;
  actionId?: number;
  stateSnapshot?: string;
  content: string;
  attachments?: string[];
  submittedByAdminId?: number;
  submittedByName?: string;
  status: GovernanceAppealStatus;
  reviewAdminId?: number;
  reviewAdminName?: string;
  reviewNote?: string;
  reviewedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type PlatformAppOverviewItem = {
  appid: number;
  name: string;
  appKey?: string;
  status: boolean;
  loginStatus: boolean;
  registerStatus: boolean;
  state: GovernanceState;
  reason?: string;
  restrictions: GovernanceRestrictions;
  startAt?: string;
  endAt?: string;
  operatorName?: string;
  lastAction?: string;
  appealStatus: GovernanceAppealStatus;
  totalUsers: number;
  disabledUsers: number;
  bannedUsers: number;
  newUsersToday: number;
  loginSuccessToday: number;
  loginFailureToday: number;
  adminCount: number;
  createdAt: string;
  updatedAt: string;
};

export type PlatformOverviewSummary = {
  totalApps: number;
  activeApps: number;
  governedApps: number;
  stateCounts: Record<string, number>;
  totalUsers: number;
  newUsersToday: number;
  loginsToday: number;
  pendingAppeals: number;
  expiringSoon: number;
};

export type PlatformOverviewResult = {
  items: PlatformAppOverviewItem[];
  summary: PlatformOverviewSummary;
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type GovernanceStateMeta = {
  key: GovernanceState;
  name: string;
  description: string;
  action: GovernanceAction;
  severity: number;
  permanent: boolean;
  requiresDanger: boolean;
  restrictions: GovernanceRestrictions;
  customizable: boolean;
};

export type GovernanceCapabilityMeta = {
  key: string;
  field: keyof GovernanceRestrictions;
  name: string;
  description: string;
  /** 后端执行点，写进目录是为了让「这个开关到底管不管用」可被直接核对 */
  enforcement: string;
};

export type GovernanceCatalog = {
  states: GovernanceStateMeta[];
  capabilities: GovernanceCapabilityMeta[];
  durations: { label: string; seconds: number }[];
};

export type GovernanceDetail = {
  governance: Governance;
  recentActions: GovernanceActionRecord[];
  pendingAppeal?: GovernanceAppeal;
  /** 后端算好的按钮可用性，避免「点了才 403」 */
  canGovern: boolean;
  canDanger: boolean;
};

export type GovernanceActionPayload = {
  action: GovernanceAction;
  reason?: string;
  restrictions?: GovernanceRestrictions;
  evidence?: Record<string, unknown>;
  endAt?: string;
  durationSeconds?: number;
  revokeSessions?: boolean;
  notifyAdmins?: boolean;
};

export type GovernanceBatchResult = {
  requested: number;
  succeeded: number;
  failed: number;
  items: { appid: number; appName?: string; ok: boolean; state?: string; error?: string }[];
};

export type PaginatedResult<T> = {
  items: T[];
  page: number;
  limit: number;
  total: number;
  totalPages: number;
};

export type PlatformOverviewParams = {
  keyword?: string;
  state?: string;
  governed?: boolean;
  sortBy?: string;
  sortOrder?: string;
  page?: number;
  limit?: number;
};

export function getGovernanceCatalog(token: string) {
  return apiRequest<GovernanceCatalog>("/api/admin/platform/catalog", { token });
}

export function listPlatformApps(token: string, params: PlatformOverviewParams = {}) {
  const qs = buildQuery({
    keyword: params.keyword,
    state: params.state,
    governed: params.governed ? "true" : undefined,
    sortBy: params.sortBy,
    sortOrder: params.sortOrder,
    page: params.page,
    limit: params.limit
  });
  return apiRequest<PlatformOverviewResult>(`/api/admin/platform/overview${qs}`, { token });
}

export function getPlatformAppGovernance(token: string, appKey: string | number) {
  return apiRequest<GovernanceDetail>(`/api/admin/platform/apps/${appKey}/governance`, { token });
}

export function applyPlatformAppGovernance(
  token: string,
  appKey: string | number,
  payload: GovernanceActionPayload
) {
  return apiRequest<{ governance: Governance; action: GovernanceActionRecord }>(
    `/api/admin/platform/apps/${appKey}/governance`,
    { method: "POST", token, body: JSON.stringify(payload) }
  );
}

export function batchApplyGovernance(
  token: string,
  payload: GovernanceActionPayload & { appids: number[] }
) {
  return apiRequest<GovernanceBatchResult>("/api/admin/platform/apps/batch-governance", {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function revokePlatformAppSessions(token: string, appKey: string | number, reason: string) {
  return apiRequest<GovernanceActionRecord>(`/api/admin/platform/apps/${appKey}/revoke-sessions`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export type GovernanceActionParams = {
  appid?: number;
  action?: string;
  state?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

export function listGovernanceActions(token: string, params: GovernanceActionParams = {}) {
  const qs = buildQuery({
    appid: params.appid,
    action: params.action,
    state: params.state,
    keyword: params.keyword,
    page: params.page,
    limit: params.limit
  });
  return apiRequest<PaginatedResult<GovernanceActionRecord>>(
    `/api/admin/platform/governance/actions${qs}`,
    { token }
  );
}

export type GovernanceAppealParams = {
  appid?: number;
  status?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

export function listGovernanceAppeals(token: string, params: GovernanceAppealParams = {}) {
  const qs = buildQuery({
    appid: params.appid,
    status: params.status,
    keyword: params.keyword,
    page: params.page,
    limit: params.limit
  });
  return apiRequest<PaginatedResult<GovernanceAppeal>>(
    `/api/admin/platform/governance/appeals${qs}`,
    { token }
  );
}

export function reviewGovernanceAppeal(
  token: string,
  appealId: number,
  payload: { decision: "approved" | "rejected"; note?: string; restore?: boolean }
) {
  return apiRequest<GovernanceAppeal>(
    `/api/admin/platform/governance/appeals/${appealId}/review`,
    { method: "POST", token, body: JSON.stringify(payload) }
  );
}

// ── 应用侧（应用管理员看自己的处境 + 申诉）──

export function getAppGovernance(token: string, appKey: string | number) {
  return apiRequest<GovernanceDetail>(`/api/admin/apps/${appKey}/governance`, { token });
}

export function listAppGovernanceHistory(
  token: string,
  appKey: string | number,
  params: { page?: number; limit?: number } = {}
) {
  const qs = buildQuery({ page: params.page, limit: params.limit });
  return apiRequest<PaginatedResult<GovernanceActionRecord>>(
    `/api/admin/apps/${appKey}/governance/history${qs}`,
    { token }
  );
}

export function submitAppGovernanceAppeal(
  token: string,
  appKey: string | number,
  payload: { content: string; attachments?: string[] }
) {
  return apiRequest<GovernanceAppeal>(`/api/admin/apps/${appKey}/governance/appeals`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function withdrawAppGovernanceAppeal(
  token: string,
  appKey: string | number,
  appealId: number
) {
  return apiRequest<GovernanceAppeal>(
    `/api/admin/apps/${appKey}/governance/appeals/${appealId}/withdraw`,
    { method: "POST", token }
  );
}
