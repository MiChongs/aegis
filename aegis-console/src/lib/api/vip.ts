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

/**
 * 权益标签来源：`ai` 是后端按应用当前的 AI 计费设置自动生成的（改价后自动跟着变，只读），
 * `custom` 是管理员在档位上手填的补充说明。
 */
export type VipPerkSource = "ai" | "custom";

/**
 * 权益标签（docs/vip-tiers.md「权益标签」）：会员页上「这个档位给你什么」的一句话。
 *
 * **只用于展示**，与功能标识（features）无关 —— 不参与「已拥有」判断、会员折算，也不解锁任何功能。
 * key 自动标签为 `ai.rate` / `ai.daily` / `ai.monthly` / `ai.pack` / `ai.limits`，手填的为 `custom.N`（保存时后端重编）。
 */
export type VipPerk = {
  key: string;
  label: string;
  detail?: string;
  source: VipPerkSource;
};

/** 手填标签的上限，与后端 `vip.MaxCustomPerks` 及文字长度校验一致（超出时后端 400 + 中文原因） */
export const VIP_PERK_LIMITS = { count: 12, label: 16, detail: 60 } as const;

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
  /** 套餐卖的会员档位。权益属于档位，套餐只是售卖规格（价格 × 时长 / 永久） */
  tierId: number;
  tierName?: string;
  /** 所属档位当前的功能（只读，改功能请改档位） */
  features: string[];
  /** 所属档位的权益标签（自动的在前、手填的在后）。后端只在用户侧在售列表里填，管理端套餐列表通常缺省 */
  perks?: VipPerk[];
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

/**
 * 会员档位（docs/vip-tiers.md）：权益的唯一来源。
 *
 * 每段会员期记的是档位，权益按档位**当前**的功能算 —— 删套餐、重新上架、调价都不影响已开通的会员；
 * 档位不能被直接删掉，还有套餐或会员时必须指定迁往的档位。
 */
export type VipTier = {
  id: number;
  appid: number;
  name: string;
  description?: string;
  features: string[];
  /** 管理员手填的补充权益标签（可编辑）；从未填过时可能为 null */
  perks?: VipPerk[] | null;
  /** 按 AI 计费设置自动生成的标签（只读预览，改价后自动更新） */
  autoPerks?: VipPerk[] | null;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
  plans: Array<{ id: number; name: string; kind: VipPlanKind; lifetime: boolean; isActive: boolean }>;
  /** 此刻有仍生效会员期落在这个档位的人数 */
  members: number;
  lifetimeMembers: number;
  /** 不挂套餐的永久会员（自定义发放、老系统迁入）默认归入的档位 */
  defaultLifetime: boolean;
};

export type VipTierPayload = {
  id?: number;
  name?: string;
  description?: string;
  features?: string[];
  /** 手填的补充权益标签：不传即不变更，传空数组即清空；文字相同的后端去重，key 由后端重编 */
  perks?: Array<{ label: string; detail?: string }>;
  sortOrder?: number;
};

// ── 会员权益折算（docs/vip-conversion.md） ──

/** 已拥有的发放：convert 折算为积分 / grant 照常发放 */
export type VipOwnedMode = "convert" | "grant";
/** 开通永久时的存量：convert 折算并结束 / keep 保留 */
export type VipUpgradeMode = "convert" | "keep";

export type VipConversionPolicy = {
  appid: number;
  owned: VipOwnedMode;
  upgrade: VipUpgradeMode;
  perVipDay: number;
  perFeatureDay: number;
  perFeatureHour: number;
  perLifetime: number;
  restoreOnRevoke: boolean;
  /** false 表示从未保存过，当前是默认策略 */
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string | null;
};

export type VipConversionModeSpec = { key: string; label: string; hint: string };

export type VipConversionPolicyBundle = {
  policy: VipConversionPolicy;
  ownedModes: VipConversionModeSpec[];
  upgradeModes: VipConversionModeSpec[];
  defaults: VipConversionPolicy;
};

/** vip_time / vip_plan / vip_lifetime / feature_unlock / feature_permanent / feature_time / ad_free */
export type VipConversionItem = {
  kind: string;
  label: string;
  seconds?: number;
  feature?: string;
  features?: string[];
  ref?: string;
  points: number;
};

export type VipConversionSummary = {
  conversionNo: string;
  /** owned（发放时已拥有）/ upgrade（开通永久时折算存量） */
  kind: "owned" | "upgrade";
  reason: string;
  points: number;
  label: string;
  message: string;
};

export type VipConversion = {
  id: number;
  conversionNo: string;
  appid: number;
  userId: number;
  kind: "owned" | "upgrade";
  source: string;
  reason: string;
  items: VipConversionItem[];
  points: number;
  integralTransactionNo?: string;
  triggerTransactionNo?: string;
  vipTransactionNo?: string;
  message: string;
  /** applied / restored（永久会员被取消后已还回） */
  status: "applied" | "restored";
  restoredAt?: string | null;
  restore?: { restored?: string[]; pointsClawedBack?: number; pointsShortfall?: number; operator?: string } | null;
  operator?: string;
  createdAt: string;
};

export type VipConversionPage = { items: VipConversion[]; total: number; page: number; limit: number };

export type VipUpgradePreview = {
  mode: VipUpgradeMode;
  items: VipConversionItem[];
  points: number;
  message: string;
};

export type VipGrantPreview = {
  holdings: { lifetime: boolean; lifetimeFeatures: string[]; permanentUnlocks: string[] };
  policy: VipConversionPolicy;
  owned?: { covered: boolean; converted: boolean; item: VipConversionItem; message: string } | null;
  upgrade?: VipUpgradePreview | null;
};

/** 套餐保存载荷。字段不传即不变更（后端按指针语义处理）。 */
export type VipPlanPayload = {
  id?: number;
  name?: string;
  kind?: VipPlanKind;
  /** 只在创建时生效；对已有套餐传入不同的值，后端以 40014 拒绝 */
  lifetime?: boolean;
  trialDeviceLimited?: boolean;
  /** 套餐卖的档位。新建时不传：功能完全相同的档位就并进去，否则按套餐名新建一个 */
  tierId?: number;
  /** 改的是所属档位的功能（同档位的套餐一起变） */
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
  | "points_store"
  | "invite"
  | "leaderboard"
  | "lottery"
  | "item_grant"
  | "admin_revoke"
  | "legacy_import"
  | "lifetime_convert"
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
  /** 单独解锁的功能（积分兑换、管理员授予），与会员身份无关；features 已经包含它们 */
  unlocks?: Array<{ tag: string; permanent: boolean; expireAt?: string }>;
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
  /** 这一行对应的折算：已拥有的发放被折成积分（原渠道、0 天），或开通永久折掉了存量（lifetime_convert、负天数） */
  conversion?: VipConversionSummary | null;
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

// ── 会员档位 ──

export function listAdminVipTiers(token: string, appKey: string) {
  return apiRequest<VipTier[]>(`/api/admin/apps/${appKey}/vip/tiers`, { token });
}

export function saveAdminVipTier(token: string, appKey: string, payload: VipTierPayload) {
  return apiRequest<VipTier>(`/api/admin/apps/${appKey}/vip/tiers`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 档位下还有套餐或在期会员时必须给 moveTo，否则 40996 */
export function deleteAdminVipTier(token: string, appKey: string, tierId: number, moveTo?: number) {
  return apiRequest<{ deleted: boolean; movedTo: number }>(
    `/api/admin/apps/${appKey}/vip/tiers/${tierId}${buildQuery({ moveTo: moveTo || undefined })}`,
    { method: "DELETE", token }
  );
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
  /** 自定义发放归入的档位（features 是档位之外另给的）。不传：永久会员归入应用的永久档位，限时的只带 features */
  tierId?: number;
  features?: string[];
  reason?: string;
  bonusIntegral?: number;
  /** 这笔对他已是空的（永久会员已包含）时：不传按应用折算策略，grant 为这一次照常发放 */
  ownedPolicy?: "" | VipOwnedMode;
  /** 发永久会员时被覆盖的存量：不传按应用折算策略，convert / keep 为这一次单独指定 */
  upgradePolicy?: "" | VipUpgradeMode;
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

/** 发放前预览（不落账）：这一笔会不会被折成积分、发永久会员时存量会怎样。 */
export function previewAdminVipGrant(token: string, appKey: string, payload: VipGrantPayload) {
  return apiRequest<VipGrantPreview>(`/api/admin/apps/${appKey}/vip/grant/preview`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 折算策略与记录 ──

export function getAdminVipConversionPolicy(token: string, appKey: string) {
  return apiRequest<VipConversionPolicyBundle>(`/api/admin/apps/${appKey}/vip/conversion-policy`, { token });
}

export type VipConversionPolicyPayload = Omit<VipConversionPolicy, "appid" | "configured" | "updatedBy" | "updatedAt">;

export function saveAdminVipConversionPolicy(token: string, appKey: string, payload: VipConversionPolicyPayload) {
  return apiRequest<VipConversionPolicy>(`/api/admin/apps/${appKey}/vip/conversion-policy`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function listAdminVipConversions(
  token: string,
  appKey: string,
  params?: { userId?: number; page?: number; limit?: number }
) {
  return apiRequest<VipConversionPage>(
    `/api/admin/apps/${appKey}/vip/conversions${buildQuery(params ?? {})}`,
    { token }
  );
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
