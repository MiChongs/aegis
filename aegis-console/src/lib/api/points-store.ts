import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 积分商城域 API。
 *
 * 用积分兑换会员、功能解锁、经验、余额、抽奖次数与人工发放的权益。权益表单由后端目录驱动
 * （`/points-store/catalog`），可兑换判定与兑换语义全部在后端（`pointstore.Evaluate`），
 * 控制台只配置与展示。规则见 docs/points-store.md。
 */

/** `number_ticket`：靓号券，`amount` 张数 + `refId` 等级 1–4（docs/user-number.md） */
export type PointsStoreRewardValue = "amount" | "money" | "ref" | "feature" | "text" | "number_ticket" | "cosmetic";
export type PointsStoreLimitPeriod = "total" | "day" | "week" | "month";
/** 补货周期：每到新的自然日 / ISO 周 / 自然月，剩余库存回到补货数量 */
export type PointsStoreRestockPeriod = "none" | "day" | "week" | "month";
export type PointsStoreAudience = "all" | "vip" | "non_vip" | "lifetime";
export type PointsStoreItemStatus = "active" | "inactive";
export type PointsStoreRedemptionStatus = "completed" | "pending" | "cancelled";

/** 一档权益的自述，由后端目录下发，前端不另抄一份 */
export type PointsStoreRewardSpec = {
  type: string;
  label: string;
  hint: string;
  value: PointsStoreRewardValue;
  unit?: string;
  min?: number;
  max?: number;
  /** 同一件商品里可以出现多次（功能解锁） */
  repeatable?: boolean;
  /** 只能单独成为一件商品（人工发放） */
  exclusive?: boolean;
};

export type PointsStoreCatalog = {
  rewards: PointsStoreRewardSpec[];
  limitPeriods: PointsStoreLimitPeriod[];
  restockPeriods: PointsStoreRestockPeriod[];
  /** 周期边界与每日开放时段所用的时区（应用默认时区，IANA 名） */
  timezone: string;
  audiences: PointsStoreAudience[];
  maxRewards: number;
  /** 应用里已经用过的分类，供分类输入补全 */
  categories: string[];
};

export type PointsStoreReward = {
  type: string;
  /** 整数量；功能解锁时是天数，0 为永久 */
  amount?: number;
  /** 金额按字符串传输：JSON number 是双精度浮点 */
  money?: string;
  refId?: number;
  tag?: string;
  text?: string;
};

export type PointsStoreRewardResult = {
  type: string;
  label: string;
  detail: string;
  transactionNo?: string;
  expireAt?: string;
  pending?: boolean;
};

export type PointsStoreSettings = {
  appid: number;
  enabled: boolean;
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string;
};

export type PointsStoreItem = {
  id: number;
  appid: number;
  title: string;
  subtitle?: string;
  description?: string;
  category?: string;
  badge?: string;
  cost: number;
  originalCost?: number;
  rewards: PointsStoreReward[];
  /** 剩余库存，缺省为不限。开了补货周期时是库里那个周期的值，此刻的库存看管理端的 currentStock */
  stock?: number;
  soldCount: number;
  restockPeriod: PointsStoreRestockPeriod;
  /** 每个补货周期补到多少 */
  restockQuantity?: number;
  stockWindowStart?: string;
  /** 全站名额：所有人合计每个周期最多兑多少份，0 为不限 */
  globalLimitPeriod: PointsStoreLimitPeriod;
  globalLimitCount: number;
  limitPeriod: PointsStoreLimitPeriod;
  limitCount: number;
  /** 每人累计上限，与按周期限兑叠加，0 为不限 */
  userTotalLimit: number;
  /** 每日开放时段（应用默认时区，自零点起的分钟数），都缺省为全天 */
  dailyOpenMinute?: number;
  dailyCloseMinute?: number;
  audience: PointsStoreAudience;
  minLevel: number;
  startsAt?: string;
  endsAt?: string;
  status: PointsStoreItemStatus;
  sortOrder: number;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
};

/** 管理端商品行：商品 + 兑换汇总（不含已取消） */
export type PointsStoreAdminItem = PointsStoreItem & {
  redemptions: number;
  redeemers: number;
  pointsSpent: number;
  pending: number;
  /** 此刻的剩余库存（已按补货周期换算），不限时缺省 */
  currentStock?: number;
  nextRestockAt?: string;
  /** 本名额周期已用份数与下次清零时刻 */
  globalUsed: number;
  globalResetsAt?: string;
  /** 此刻是否在每日开放时段内 */
  openNow: boolean;
};

export type PointsStoreItemPayload = {
  title: string;
  subtitle: string;
  description: string;
  category: string;
  badge: string;
  cost: number;
  originalCost: number | null;
  rewards: PointsStoreReward[];
  stock: number | null;
  restockPeriod: PointsStoreRestockPeriod;
  restockQuantity: number | null;
  globalLimitPeriod: PointsStoreLimitPeriod;
  globalLimitCount: number;
  limitPeriod: PointsStoreLimitPeriod;
  limitCount: number;
  userTotalLimit: number;
  dailyOpenMinute: number | null;
  dailyCloseMinute: number | null;
  audience: PointsStoreAudience;
  minLevel: number;
  startsAt: string | null;
  endsAt: string | null;
  status: PointsStoreItemStatus;
  sortOrder: number;
};

export type PointsStoreRedemption = {
  id: number;
  redemptionNo: string;
  appid: number;
  userId: number;
  account?: string;
  nickname?: string;
  /** 商品被删除后为空，标题与权益看快照 */
  itemId?: number;
  itemTitle: string;
  cost: number;
  rewards: PointsStoreReward[];
  results: PointsStoreRewardResult[];
  status: PointsStoreRedemptionStatus;
  integralTransactionNo?: string;
  refundTransactionNo?: string;
  requestId?: string;
  note?: string;
  handledBy?: string;
  handledAt?: string;
  deviceId?: string;
  clientIp?: string;
  createdAt: string;
  updatedAt: string;
};

export type PointsStorePage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type PointsStoreRedemptionParams = {
  userId?: number;
  itemId?: number;
  status?: PointsStoreRedemptionStatus;
  keyword?: string;
  /** 下单时间区间（ISO 瞬时值），左闭右开 */
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

export type PointsStoreStats = {
  summary: {
    redemptions: number;
    redeemers: number;
    pointsSpent: number;
    pending: number;
    activeItems: number;
    today: number;
    /** 窗口内被取消（已退积分）的笔数，不计入上面的汇总 */
    cancelled: number;
  };
  trend: Array<{ date: string; redemptions: number; pointsSpent: number; redeemers: number }>;
  topItems: Array<{ itemId?: number; title: string; redemptions: number; pointsSpent: number }>;
};

/** 一条功能解锁记录（账本行） */
export type FeatureUnlock = {
  id: number;
  tag: string;
  source: "points_store" | "admin_grant" | "invite" | "leaderboard" | "lottery" | string;
  sourceRef?: string;
  /** 0 为永久 */
  durationDays: number;
  startsAt: string;
  endsAt?: string;
  revokedAt?: string;
  revokedBy?: string;
  revokeReason?: string;
  operator?: string;
  createdAt: string;
};

/** 某个功能当前的解锁状态（同一功能的多条记录合成一条） */
export type FeatureUnlockView = {
  tag: string;
  permanent: boolean;
  expireAt?: string;
};

export type UserFeatureUnlocks = {
  items: FeatureUnlock[];
  active: FeatureUnlockView[];
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/points-store`;
const userBase = (appKey: string, userId: number) =>
  `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/feature-unlocks`;

// ── 目录与开关 ──

export function getPointsStoreCatalog(token: string, appKey: string) {
  return apiRequest<PointsStoreCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getPointsStoreSettings(token: string, appKey: string) {
  return apiRequest<PointsStoreSettings>(`${base(appKey)}/settings`, { token });
}

export function savePointsStoreSettings(token: string, appKey: string, enabled: boolean) {
  return apiRequest<PointsStoreSettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify({ enabled })
  });
}

// ── 商品 ──

export function listPointsStoreItems(token: string, appKey: string) {
  return apiRequest<PointsStoreAdminItem[]>(`${base(appKey)}/items`, { token });
}

export function createPointsStoreItem(token: string, appKey: string, payload: PointsStoreItemPayload) {
  return apiRequest<PointsStoreItem>(`${base(appKey)}/items`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function updatePointsStoreItem(token: string, appKey: string, itemId: number, payload: PointsStoreItemPayload) {
  return apiRequest<PointsStoreItem>(`${base(appKey)}/items/${itemId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function setPointsStoreItemStatus(token: string, appKey: string, itemId: number, active: boolean) {
  return apiRequest<PointsStoreItem>(`${base(appKey)}/items/${itemId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ active })
  });
}

/** 按数组顺序重排商品（排序值写成 10、20、30……），只动排序，不碰库存 */
export function reorderPointsStoreItems(token: string, appKey: string, itemIds: number[]) {
  return apiRequest<{ updated: number }>(`${base(appKey)}/item-order`, {
    method: "PUT",
    token,
    body: JSON.stringify({ itemIds })
  });
}

export function deletePointsStoreItem(token: string, appKey: string, itemId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/items/${itemId}`, { method: "DELETE", token });
}

// ── 兑换记录 ──

export function listPointsStoreRedemptions(token: string, appKey: string, params?: PointsStoreRedemptionParams) {
  return apiRequest<PointsStorePage<PointsStoreRedemption>>(
    `${base(appKey)}/redemptions${buildQuery(params ?? {})}`,
    { token }
  );
}

/** 处理一笔待发放的兑换：发放（写给用户的话）或取消（退回积分）。都会站内信通知用户。 */
export function handlePointsStoreRedemption(
  token: string,
  appKey: string,
  redemptionId: number,
  action: "fulfill" | "cancel",
  note: string
) {
  return apiRequest<PointsStoreRedemption>(`${base(appKey)}/redemptions/${redemptionId}/${action}`, {
    method: "POST",
    token,
    body: JSON.stringify({ note })
  });
}

/** tz 为浏览器时区：「今天」与按天分桶都按它切日 */
export function getPointsStoreStats(token: string, appKey: string, days: number, tz?: string) {
  return apiRequest<PointsStoreStats>(`${base(appKey)}/stats${buildQuery({ days, tz })}`, { token });
}

// ── 用户的功能解锁 ──

export function getUserFeatureUnlocks(token: string, appKey: string, userId: number) {
  return apiRequest<UserFeatureUnlocks>(userBase(appKey, userId), { token });
}

/** days 为 0 即永久 */
export function grantUserFeatureUnlock(token: string, appKey: string, userId: number, tag: string, days: number) {
  return apiRequest<FeatureUnlock>(userBase(appKey, userId), {
    method: "POST",
    token,
    body: JSON.stringify({ tag, days })
  });
}

export function revokeUserFeatureUnlock(token: string, appKey: string, userId: number, unlockId: number, reason: string) {
  return apiRequest<FeatureUnlock>(`${userBase(appKey, userId)}/${unlockId}/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}
