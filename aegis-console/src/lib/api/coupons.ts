import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 优惠系统 API（管理端，docs/coupons.md）。
 *
 * 类型与后端 internal/domain/coupon/types.go、pricing.go 与 internal/service/coupon_service.go 逐字段对应。
 * 金额（decimal.Decimal）在 JSON 里是十进制字符串，这里一律按字符串收发，不转 number —— 浮点会吃掉分。
 *
 * 权限：读 `app:coupon:read`（含试算），改 `app:coupon:write`，删 `app:coupon:delete`，发券与收回 `app:coupon:issue`。
 */

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/coupons`;

export const COUPON_READ = "app:coupon:read";
export const COUPON_WRITE = "app:coupon:write";
export const COUPON_DELETE = "app:coupon:delete";
export const COUPON_ISSUE = "app:coupon:issue";

// ── 枚举 ──

export type CouponKind = "amount_off" | "percent_off";
export type CouponStatus = "draft" | "active" | "paused" | "ended";
export type CouponValidityMode = "fixed" | "relative";
export type CouponScope = "vip_purchase" | "integral_purchase" | "goods" | "wallet_recharge";
export type CouponTheme = "red" | "orange" | "gold" | "green" | "blue" | "purple";
export type AudienceKind = "all" | "new_user" | "first_order" | "non_vip" | "vip";
export type UserCouponStatus = "available" | "locked" | "used" | "expired" | "revoked" | "not_started";
export type CouponSource =
  | "claim"
  | "admin"
  | "rebate"
  | "card_key"
  | "points_store"
  | "lottery"
  | "item_grant"
  | "invite"
  | "leaderboard"
  | "refund";
export type PromotionKind = "discount" | "threshold" | "rebate";
export type DiscountMode = "percent" | "amount_off" | "fixed_price";
export type OrderPromotionStatus = "reserved" | "settled" | "released" | "refunded";
export type RebateStatus = "none" | "pending" | "granted" | "skipped" | "revoked" | "failed";

export type CouponAudience = {
  kind: AudienceKind;
  /** 新用户的天数（kind=new_user，1–365） */
  newUserDays?: number;
};

// ── 券（模板） ──

export type CouponTemplate = {
  id: number;
  appid: number;
  name: string;
  description?: string;
  theme: CouponTheme;
  kind: CouponKind;
  /** 立减金额 */
  amount: string;
  /** 折扣：付款比例 %（85 即 8.5 折） */
  rate?: number;
  /** 折扣最多减（0 不封顶） */
  maxDiscount: string;
  /** 门槛（0 无门槛） */
  minAmount: string;
  scopes: CouponScope[];
  /** 只能用于这些会员套餐（空为全部） */
  planIds: number[];
  stackWithPromotion: boolean;
  validityMode: CouponValidityMode;
  validFrom?: string | null;
  validUntil?: string | null;
  validDays?: number;
  validDelayDays?: number;
  /** 发放总量（0 不限），全部渠道共用 */
  totalQuantity: number;
  /** 每人限领（只限领券中心，0 不限） */
  perUserLimit: number;
  claimable: boolean;
  claimStartsAt?: string | null;
  claimEndsAt?: string | null;
  audience: CouponAudience;
  refundReturn: boolean;
  status: CouponStatus;
  issuedCount: number;
  usedCount: number;
  discountTotal: string;
  createdBy?: string;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
};

/** 新建 / 修改券的载荷（金额按字符串） */
export type CouponTemplatePayload = {
  name: string;
  description?: string;
  theme: CouponTheme;
  kind: CouponKind;
  amount: string;
  rate: number;
  maxDiscount: string;
  minAmount: string;
  scopes: CouponScope[];
  planIds: number[];
  stackWithPromotion: boolean;
  validityMode: CouponValidityMode;
  validFrom?: string | null;
  validUntil?: string | null;
  validDays: number;
  validDelayDays: number;
  totalQuantity: number;
  perUserLimit: number;
  claimable: boolean;
  claimStartsAt?: string | null;
  claimEndsAt?: string | null;
  audience: CouponAudience;
  refundReturn: boolean;
};

export type UserCoupon = {
  id: number;
  appid: number;
  userId: number;
  templateId: number;
  code: string;
  /** 管理端列表里已按时间推导成展示状态（含 not_started / expired） */
  status: UserCouponStatus;
  source: CouponSource | string;
  sourceRef?: string;
  validFrom: string;
  validUntil: string;
  orderNo?: string;
  usedAt?: string | null;
  discount: string;
  revokedReason?: string;
  createdAt: string;
  updatedAt: string;
  template?: CouponTemplate;
  userAccount?: string;
  userNickname?: string;
};

// ── 活动 ──

export type ThresholdTier = { minAmount: string; off: string };
export type RebateCoupon = { templateId: number; count: number; templateName?: string };

export type PromotionRule = {
  discountMode?: DiscountMode;
  rate?: number;
  amount: string;
  maxDiscount: string;
  tiers?: ThresholdTier[];
  minAmount: string;
  rebateCoupons?: RebateCoupon[];
  rebateIntegral?: number;
};

export type Promotion = {
  id: number;
  appid: number;
  name: string;
  description?: string;
  /** 角标（服务端已按规则补上默认值） */
  tag: string;
  theme: CouponTheme;
  kind: PromotionKind;
  rule: PromotionRule;
  priority: number;
  scopes: CouponScope[];
  planIds: number[];
  packageIds: number[];
  audience: CouponAudience;
  stackWithCoupon: boolean;
  startsAt?: string | null;
  endsAt?: string | null;
  perUserLimit: number;
  totalLimit: number;
  budget: string;
  status: CouponStatus;
  usedCount: number;
  discountTotal: string;
  createdBy?: string;
  updatedBy?: string;
  createdAt: string;
  updatedAt: string;
};

export type PromotionPayload = {
  name: string;
  description?: string;
  tag?: string;
  theme: CouponTheme;
  kind: PromotionKind;
  rule: PromotionRule;
  priority: number;
  scopes: CouponScope[];
  planIds: number[];
  packageIds: number[];
  audience: CouponAudience;
  stackWithCoupon: boolean;
  startsAt?: string | null;
  endsAt?: string | null;
  perUserLimit: number;
  totalLimit: number;
  budget: string;
};

// ── 订单上的优惠 ──

export type OrderPromotion = {
  id: number;
  appid: number;
  userId: number;
  orderId: number;
  orderNo: string;
  purpose: string;
  subject?: string;
  originalAmount: string;
  promotionDiscount: string;
  couponDiscount: string;
  payableAmount: string;
  promotionId?: number | null;
  promotionName?: string;
  userCouponId?: number | null;
  couponName?: string;
  templateId?: number | null;
  rebatePromotionId?: number | null;
  rebatePromotionName?: string;
  rebateStatus: RebateStatus;
  rebateNote?: string;
  status: OrderPromotionStatus;
  note?: string;
  createdAt: string;
  settledAt?: string | null;
  releasedAt?: string | null;
  userAccount?: string;
  userNickname?: string;
  orderStatus?: string;
};

// ── 概览 ──

export type CouponTrendPoint = { date: string; issued: number; used: number; orders: number; paid: string; discount: string };
export type CouponRankItem = { id: number; name: string; count: number; discount: string; paid: string };
export type CouponSourceCount = { source: string; count: number };

export type CouponOverview = {
  templates: number;
  activeTemplates: number;
  promotions: number;
  activePromotions: number;
  issued: number;
  used: number;
  available: number;
  expired: number;
  orders: number;
  paidAmount: string;
  originalAmount: string;
  couponDiscount: string;
  promotionDiscount: string;
  rebateIntegral: number;
  rebateCoupons: number;
  trend: CouponTrendPoint[];
  topTemplates: CouponRankItem[];
  topPromotions: CouponRankItem[];
  sources: CouponSourceCount[];
};

// ── 试算 ──

export type AppliedPromotion = {
  id: number;
  name: string;
  tag: string;
  kind: PromotionKind;
  theme: CouponTheme;
  discount: string;
  endsAt?: string | null;
};

export type AppliedCoupon = { id: number; name: string; face: string; discount: string };

export type CouponOption = {
  coupon: UserCoupon;
  face: string;
  usable: boolean;
  reasonCode?: string;
  reason?: string;
  discount: string;
  totalDiscount: string;
  payable: string;
  best: boolean;
  dropsPromotion?: boolean;
};

export type RebatePreview = {
  id: number;
  name: string;
  tag: string;
  eligible: boolean;
  minAmount: string;
  gap: string;
  coupons?: RebateCoupon[];
  integral?: number;
};

export type CouponQuote = {
  scope: CouponScope;
  originalAmount: string;
  promotionDiscount: string;
  couponDiscount: string;
  totalDiscount: string;
  payableAmount: string;
  promotion?: AppliedPromotion | null;
  coupon?: AppliedCoupon | null;
  rebate?: RebatePreview | null;
  coupons: CouponOption[];
  bestCouponId: number;
  notes: string[];
};

export type CouponQuotePayload = {
  userId: number;
  purpose?: string;
  planId?: number;
  packageId?: number;
  amount?: string;
  couponId?: number;
  /** auto 自动选最优、none 或空不用券 */
  couponMode?: string;
};

// ── 发券 ──

export type IssueOutcome = { userId: number; ok: boolean; message?: string; couponIds?: number[] };
export type IssueResult = { items: IssueOutcome[]; succeeded: number };

// ── 分页与查询 ──

export type CouponPage<T> = { items: T[]; total: number; page: number; limit: number; totalPages: number };

export type CouponTemplateQuery = { status?: string; keyword?: string; page?: number; limit?: number };
export type PromotionQuery = { kind?: string; status?: string; keyword?: string; page?: number; limit?: number };
export type UserCouponQuery = {
  templateId?: number;
  userId?: number;
  status?: string;
  source?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};
export type OrderPromotionQuery = {
  promotionId?: number;
  templateId?: number;
  userId?: number;
  status?: string;
  keyword?: string;
  page?: number;
  limit?: number;
};

/** 控制台表单的目录（coupon.Catalog()） */
export type CouponCatalog = {
  couponKinds: CouponKind[];
  priceScopes: CouponScope[];
  rebateScopes: CouponScope[];
  promotionKinds: PromotionKind[];
  discountModes: DiscountMode[];
  audiences: AudienceKind[];
  themes: CouponTheme[];
  statuses: CouponStatus[];
  userCouponStatus: UserCouponStatus[];
  sources: string[];
  orderStatuses: OrderPromotionStatus[];
  rebateStatuses: RebateStatus[];
  minPayable: string;
  maxIssueUsers: number;
  maxIssueCount: number;
  maxTiers: number;
  maxRebateCoupons: number;
  maxNameLength: number;
  maxDescLength: number;
  maxTagLength: number;
  maxValidDays: number;
  maxGrantCount: number;
  maxPlanIds: number;
  maxRebateIntegral: number;
};

// ── 请求 ──

export function getCouponCatalog(token: string, appKey: string) {
  return apiRequest<CouponCatalog>(`${base(appKey)}/catalog`, { token });
}

/** tz 为浏览器时区（按它分日），days 默认 14 */
export function getCouponOverview(token: string, appKey: string, tz: string, days = 14) {
  return apiRequest<CouponOverview>(`${base(appKey)}/overview${buildQuery({ tz, days })}`, { token });
}

export function listCouponTemplates(token: string, appKey: string, query: CouponTemplateQuery) {
  return apiRequest<CouponPage<CouponTemplate>>(`${base(appKey)}/templates${buildQuery(query)}`, { token });
}

export function getCouponTemplate(token: string, appKey: string, id: number) {
  return apiRequest<CouponTemplate>(`${base(appKey)}/templates/${id}`, { token });
}

export function createCouponTemplate(token: string, appKey: string, payload: CouponTemplatePayload) {
  return apiRequest<CouponTemplate>(`${base(appKey)}/templates`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateCouponTemplate(token: string, appKey: string, id: number, payload: CouponTemplatePayload) {
  return apiRequest<CouponTemplate>(`${base(appKey)}/templates/${id}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function setCouponTemplateStatus(token: string, appKey: string, id: number, status: CouponStatus) {
  return apiRequest<CouponTemplate>(`${base(appKey)}/templates/${id}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

export function deleteCouponTemplate(token: string, appKey: string, id: number) {
  return apiRequest<{ id: number }>(`${base(appKey)}/templates/${id}`, { method: "DELETE", token });
}

/** 给一批用户发券：userIds 为管理端用户 ID（≤ 500），count 每人张数（1–20），notify 是否发站内信 */
export function issueCoupons(token: string, appKey: string, id: number, payload: { userIds: number[]; count: number; notify: boolean }) {
  return apiRequest<IssueResult>(`${base(appKey)}/templates/${id}/issue`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function listUserCoupons(token: string, appKey: string, query: UserCouponQuery) {
  return apiRequest<CouponPage<UserCoupon>>(`${base(appKey)}/user-coupons${buildQuery(query)}`, { token });
}

/** 收回一张券：只能收回可用（含已过期）的，被订单占用或已使用的不行（42325） */
export function revokeUserCoupon(token: string, appKey: string, id: number, reason?: string) {
  return apiRequest<UserCoupon>(`${base(appKey)}/user-coupons/${id}/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason: reason ?? "" })
  });
}

export function listPromotions(token: string, appKey: string, query: PromotionQuery) {
  return apiRequest<CouponPage<Promotion>>(`${base(appKey)}/promotions${buildQuery(query)}`, { token });
}

export function getPromotion(token: string, appKey: string, id: number) {
  return apiRequest<Promotion>(`${base(appKey)}/promotions/${id}`, { token });
}

export function createPromotion(token: string, appKey: string, payload: PromotionPayload) {
  return apiRequest<Promotion>(`${base(appKey)}/promotions`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updatePromotion(token: string, appKey: string, id: number, payload: PromotionPayload) {
  return apiRequest<Promotion>(`${base(appKey)}/promotions/${id}`, { method: "PUT", token, body: JSON.stringify(payload) });
}

export function setPromotionStatus(token: string, appKey: string, id: number, status: CouponStatus) {
  return apiRequest<Promotion>(`${base(appKey)}/promotions/${id}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

export function deletePromotion(token: string, appKey: string, id: number) {
  return apiRequest<{ id: number }>(`${base(appKey)}/promotions/${id}`, { method: "DELETE", token });
}

export function listOrderPromotions(token: string, appKey: string, query: OrderPromotionQuery) {
  return apiRequest<CouponPage<OrderPromotion>>(`${base(appKey)}/orders${buildQuery(query)}`, { token });
}

/** 替某个用户试算（只算不写）；指定的券用不了时 42317 */
export function quoteCoupon(token: string, appKey: string, payload: CouponQuotePayload) {
  return apiRequest<CouponQuote>(`${base(appKey)}/quote`, { method: "POST", token, body: JSON.stringify(payload) });
}
