import { apiRequest, buildQuery } from "./client";

/**
 * 会员域 API（应用级）。
 *
 * 覆盖后端 `/api/admin/apps/{appKey}/vip/*` 的全部管理能力：
 * 套餐（含试用）、功能标识目录、试用领取记录、会员权益判定、开通记录、授予与收回。
 *
 * 金额一律按**字符串**处理（后端 shopspring/decimal），前端不做任何算术 ——
 * 转 number 会丢分，与交易中心同一条约束。
 */

// ── 类型 ──

/** 套餐种类：付费 / 试用。试用恒 0 元且只能领取，不能购买。 */
export type VipPlanKind = "paid" | "trial";

export type VipPlan = {
  id: number;
  appid: number;
  name: string;
  kind: VipPlanKind;
  /** 永久套餐：durationDays 恒为 0，只能是付费套餐，创建后不能在永久与限时之间切换 */
  lifetime: boolean;
  /** 仅试用套餐有意义：同一设备只能领一次 */
  trialDeviceLimited: boolean;
  /** 这个套餐解锁哪些功能标识（引用功能目录的 tag） */
  features: string[];
  durationDays: number;
  price: string;
  originalPrice?: string;
  bonusIntegral: number;
  description?: string;
  isActive: boolean;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
};

/** 套餐保存载荷。字段不传即不变更（后端按指针语义处理）。 */
export type VipPlanPayload = {
  id?: number;
  name?: string;
  kind?: VipPlanKind;
  /** 只在创建时生效；对已有套餐传入不同的值，后端以 40014 拒绝 */
  lifetime?: boolean;
  trialDeviceLimited?: boolean;
  features?: string[];
  /** 永久套餐忽略 */
  durationDays?: number;
  price?: string;
  originalPrice?: string;
  bonusIntegral?: number;
  description?: string;
  isActive?: boolean;
  sortOrder?: number;
};

export type VipFeature = {
  id: number;
  appid: number;
  /** 机器标识，接入方校验时传的就是它；创建后不可改 */
  tag: string;
  name: string;
  description?: string;
  isActive: boolean;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
};

export type VipFeaturePayload = {
  tag: string;
  name?: string;
  description?: string;
  isActive?: boolean;
  sortOrder?: number;
};

/**
 * 会员来源：凭什么是会员。开通记录的渠道（payChannel）也用这组值，
 * 另多两档只出现在记录里、不会成为会员来源的：`admin_revoke`（扣减天数 / 取消永久会员）
 * 与 `legacy_import`（老系统迁移进来的永久会员，判定时归为 `unknown`）。
 */
export type VipSource =
  | "none"
  | "trial"
  | "wallet"
  | "payment_order"
  | "admin_grant"
  | "card_key"
  | "ad_reward"
  | "admin_revoke"
  | "legacy_import"
  | "unknown";

export type VipTrialState = {
  active: boolean;
  claimedAt: string;
  endsAt: string;
  durationDays: number;
  planId?: number;
  planName: string;
  remainingSeconds: number;
};

/** 试用资格判据。客户端与控制台按 reason 分支，不匹配 message。 */
export type VipTrialReason =
  | "eligible"
  | "not_configured"
  | "already_claimed"
  | "member_active"
  | "device_claimed"
  | "device_required";

export type VipTrialOffer = {
  available: boolean;
  reason: VipTrialReason;
  message: string;
  planId?: number;
  planName?: string;
  durationDays?: number;
  bonusIntegral?: number;
  deviceLimited?: boolean;
  description?: string;
};

export type VipEntitlement = {
  isVip: boolean;
  /** 永久会员：此时没有 expireAt，remainingSeconds / remainingDays 为 0 */
  isLifetime: boolean;
  /** 成为永久会员的时间 */
  lifetimeSince?: string;
  /** 永久会员另有一段仍在期内的限时会员时，那段的到期时间 */
  timedExpireAt?: string;
  isTrial: boolean;
  /** 永久会员取最近一笔永久开通；老系统迁移的为 unknown */
  source: VipSource;
  planName?: string;
  expireAt?: string;
  remainingSeconds: number;
  remainingDays: number;
  features: string[];
  trial?: VipTrialState;
  trialOffer: VipTrialOffer;
};

export type VipTrialClaim = {
  id: number;
  appid: number;
  userId: number;
  account?: string;
  planId?: number;
  planName: string;
  durationDays: number;
  trialEndsAt: string;
  transactionNo: string;
  deviceId?: string;
  deviceLocked: boolean;
  clientIp?: string;
  operator?: string;
  createdAt: string;
  /** 领取之后是否发生过付费开通 —— 开试用的理由就是这一列 */
  converted?: boolean;
};

export type VipTrialSummary = {
  total: number;
  active: number;
  converted: number;
};

export type VipTrialClaimPage = {
  items: VipTrialClaim[];
  total: number;
  page: number;
  limit: number;
  summary: VipTrialSummary;
};

export type VipTransaction = {
  id: number;
  transactionNo: string;
  userId: number;
  appid: number;
  planId?: number;
  planName: string;
  features: string[];
  /** 扣减天数时为负数；永久开通与取消永久会员时为 0 */
  durationDays: number;
  /** 永久开通，或 admin_revoke 渠道下的「取消永久会员」留痕 */
  lifetime: boolean;
  payChannel: VipSource;
  payAmount: string;
  relatedOrderNo?: string;
  bonusIntegral: number;
  /** 限时那条线开通前后的到期时间；永久开通不动它，没有 expireAfter */
  expireBefore?: string;
  expireAfter?: string;
  operator?: string;
  metadata?: Record<string, unknown>;
  /** 这笔开通已作废（退款冲正 / 取消永久会员），不再贡献任何权益 */
  revokedAt?: string;
  /** refund / admin_revoke */
  revokeReason?: string;
  createdAt: string;
};

export type VipTransactionPage = {
  items: VipTransaction[];
  total: number;
  page: number;
  limit: number;
};

export type VipTrialClaimResult = {
  claim: VipTrialClaim;
  transaction?: VipTransaction;
  entitlement: VipEntitlement;
  replayed?: boolean;
};

// ── 套餐 ──

export function listAdminVipPlans(token: string, appKey: string) {
  return apiRequest<VipPlan[]>(`/api/admin/apps/${appKey}/vip/plans`, { token });
}

export function saveAdminVipPlan(token: string, appKey: string, payload: VipPlanPayload) {
  return apiRequest<VipPlan>(`/api/admin/apps/${appKey}/vip/plans`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAdminVipPlan(token: string, appKey: string, planId: number) {
  return apiRequest<{ deleted: boolean }>(`/api/admin/apps/${appKey}/vip/plans/${planId}`, {
    method: "DELETE",
    token
  });
}

// ── 功能标识目录 ──

export function listAdminVipFeatures(token: string, appKey: string) {
  return apiRequest<VipFeature[]>(`/api/admin/apps/${appKey}/vip/features`, { token });
}

export function saveAdminVipFeature(token: string, appKey: string, payload: VipFeaturePayload) {
  return apiRequest<VipFeature>(`/api/admin/apps/${appKey}/vip/features`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 删除功能标识。返回还有几个套餐挂着它 —— 那几个套餐从此不再发放这项权益。 */
export function deleteAdminVipFeature(token: string, appKey: string, tag: string) {
  return apiRequest<{ deleted: boolean; affectedPlans: number }>(
    `/api/admin/apps/${appKey}/vip/features/${encodeURIComponent(tag)}`,
    { method: "DELETE", token }
  );
}

// ── 会员权益 ──

export function getAdminVipEntitlement(token: string, appKey: string, userId: number) {
  return apiRequest<VipEntitlement>(
    `/api/admin/apps/${appKey}/vip/entitlement${buildQuery({ userId })}`,
    { token }
  );
}

/**
 * 发放会员载荷。planId > 0 按套餐发放（时长与权益取自套餐 × quantity，永久套餐只能 1 份）；
 * 否则自定义发放：lifetime 为 true 即发永久会员（days 忽略），否则按 days 发。
 */
export type VipGrantPayload = {
  userId: number;
  planId?: number;
  quantity?: number;
  days?: number;
  lifetime?: boolean;
  features?: string[];
  reason?: string;
  bonusIntegral?: number;
};

export function grantAdminVip(token: string, appKey: string, payload: VipGrantPayload) {
  return apiRequest<VipTransaction>(`/api/admin/apps/${appKey}/vip/grant`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/**
 * 收回会员载荷。lifetime 为 true 即取消永久会员（作废全部仍生效的永久开通，限时会员不受影响）；
 * 否则从限时会员的到期时间往回扣 days 天。两者都只收权益不退钱。
 */
export type VipRevokePayload = {
  userId: number;
  lifetime?: boolean;
  days?: number;
  reason?: string;
};

/** 收回会员，返回那条留痕记录（payChannel 为 admin_revoke）。 */
export function revokeAdminVip(token: string, appKey: string, payload: VipRevokePayload) {
  return apiRequest<VipTransaction>(`/api/admin/apps/${appKey}/vip/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function listAdminVipTransactions(
  token: string,
  appKey: string,
  params?: { userId?: number; page?: number; limit?: number }
) {
  return apiRequest<VipTransactionPage>(
    `/api/admin/apps/${appKey}/vip/transactions${buildQuery(params ?? {})}`,
    { token }
  );
}

// ── 试用 ──

export function listAdminVipTrialClaims(
  token: string,
  appKey: string,
  params?: { page?: number; limit?: number }
) {
  return apiRequest<VipTrialClaimPage>(
    `/api/admin/apps/${appKey}/vip/trial/claims${buildQuery(params ?? {})}`,
    { token }
  );
}

/** 管理员代某用户领取试用（走与用户自助完全相同的资格判定）。 */
export function claimAdminVipTrial(token: string, appKey: string, userId: number) {
  return apiRequest<VipTrialClaimResult>(`/api/admin/apps/${appKey}/vip/trial/claims`, {
    method: "POST",
    token,
    body: JSON.stringify({ userId })
  });
}

/** 恢复某用户的试用资格。**只删资格，不收回已发放的会员时长**。 */
export function resetAdminVipTrial(token: string, appKey: string, userId: number) {
  return apiRequest<{ reset: boolean }>(`/api/admin/apps/${appKey}/vip/trial/claims/${userId}`, {
    method: "DELETE",
    token
  });
}
