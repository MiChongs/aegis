import { apiRequest, buildQuery, joinApiUrl } from "@/lib/api/client";

/**
 * AI 中转网关域 API（docs/ai-gateway.md）。
 *
 * 应用的终端用户凭自己的 sk-ag- 密钥（或登录令牌）调 `/api/ai/v1`，按模型价格 × 倍率
 * 从免费额度、会员额度、额度包、永久额度、余额里依次扣费。这里是它的管理端：
 * 设置、计费方案（时段价格、会员优惠，docs/ai-billing.md）、模型与价格、调用记录与退款、用户密钥、统计、
 * 额度商店（额度包、订单、批次、生意统计）与用户的 AI 额度。
 *
 * AI 额度是独立资产，与积分不互通：积分来源 `integral` 已从目录移除，类型里保留只为显示旧记录。
 *
 * 金额（点、元、倍率、单价）一律是十进制字符串（后端 shopspring/decimal），只在展示时格式化。
 */

/** `integral` 只出现在 2026-11-26 之前的旧记录里，设置里不再接受。 */
export type AIGatewaySourceKey = "free_daily" | "vip_daily" | "vip_monthly" | "pack" | "credit" | "integral" | "wallet";

export type AIGatewaySourceMeta = {
  key: AIGatewaySourceKey;
  label: string;
  hint: string;
  periodic: boolean;
  period?: "day" | "month";
  vipOnly?: boolean;
};

export type AIGatewayPriceKey =
  | "inputPrice"
  | "outputPrice"
  | "requestPrice"
  | "imagePrice"
  | "charPrice"
  | "minutePrice";

export type AIGatewayKindMeta = {
  key: string;
  label: string;
  endpoints: string[];
  prices: AIGatewayPriceKey[];
};

export type AIGatewayLevelMultiplier = { level: number; multiplier: string };

// ── 计费方案（docs/ai-billing.md）──

/** 时段色调：只影响展示。 */
export type AIGatewayTimeTone = "off_peak" | "peak" | "promo" | "normal";

/** 一个价格时段：`[start, end)`，`end ≤ start` 跨零点，`start = end` 全天；列表里靠前的优先。 */
export type AIGatewayTimeWindow = {
  id: string;
  name: string;
  tone: AIGatewayTimeTone;
  /** 1 = 周一 … 7 = 周日，空 = 每天；跨零点的时段按开始那一天判星期。 */
  weekdays: number[];
  start: string;
  end: string;
  multiplier: string;
  /** 对外模型名，空 = 全部。 */
  models?: string[];
  /** 模型种类，空 = 全部。 */
  kinds?: string[];
  /** 活动日期 `YYYY-MM-DD`（含当天），可空。 */
  from?: string | null;
  to?: string | null;
  enabled: boolean;
};

export type AIGatewayTimeStacking = "multiply" | "best";

export type AIGatewayTimePricing = {
  enabled: boolean;
  stacking: AIGatewayTimeStacking;
  windows: AIGatewayTimeWindow[];
};

/** 会员档位优惠：按优先级排列，用户持有的档位里最靠前的生效。 */
export type AIGatewayMemberTier = {
  tierId: number;
  label: string;
  multiplier: string;
  daily: string;
  monthly: string;
  packDiscount: string;
  packBonusRate: string;
  /** 0 沿用应用设置。 */
  rpm: number;
  maxConcurrent: number;
};

export type AIGatewayWelcome = { enabled: boolean; amount: string; validityDays: number };

export type AIGatewayStoreSettings = { enabled: boolean; notice: string };

/** 一个用户此刻生效的会员优惠。source：tier 档位配置 / vip 通用会员 / 其余为非会员。 */
export type AIGatewayMemberBenefit = {
  source: string;
  tierId?: number;
  name?: string;
  multiplier: string;
  daily: string;
  monthly: string;
  packDiscount: string;
  packBonusRate: string;
  rpm: number;
  maxConcurrent: number;
};

/** 命中的时段（快照）。 */
export type AIGatewayWindowHit = {
  id: string;
  name: string;
  tone: AIGatewayTimeTone;
  multiplier: string;
  endsAt?: string;
};

/** 倍率分解：total = base × member × level × time（stacking = best 时会员与闲时取更优惠的一个）。 */
export type AIGatewayFactorBreakdown = {
  base: string;
  member: string;
  level: string;
  time: string;
  total: string;
  stacking: AIGatewayTimeStacking;
  window?: AIGatewayWindowHit | null;
};

/** 时间轴上的一段（相邻同价段已合并）。windowId 为空表示标准价。 */
export type AIGatewayTimelineSegment = {
  start: string;
  end: string;
  windowId: string;
  name: string;
  tone: AIGatewayTimeTone;
  multiplier: string;
};

export type AIGatewayPricingPreviewPayload = {
  /** 草稿设置；不传用已保存的。 */
  settings?: AIGatewaySettings;
  model?: string;
  kind?: string;
  tierId?: number;
  isVip?: boolean;
  level?: number;
  /** RFC3339，不传为现在。 */
  at?: string;
};

export type AIGatewayPricingPreview = {
  breakdown: AIGatewayFactorBreakdown;
  member: AIGatewayMemberBenefit;
  timeline: AIGatewayTimelineSegment[];
  nextChangeAt?: string | null;
  /** 传了 model 时：该模型乘上倍率后的单价。 */
  price?: AIGatewayPrice | null;
};

export type AIGatewaySettings = {
  enabled: boolean;
  allowSessionToken: boolean;
  audience: "all" | "vip";
  sources: AIGatewaySourceKey[];
  priceMultiplier: string;
  vipMultiplier: string;
  levelMultipliers: AIGatewayLevelMultiplier[];
  freeDaily: string;
  vipDaily: string;
  vipMonthly: string;
  walletRate: string;
  dailySpendLimit: string;
  rpm: number;
  maxConcurrent: number;
  maxKeysPerUser: number;
  defaultMaxTokens: number;
  timezone: string;
  redactContent: boolean;
  /** 每天从公开模型目录更新对话模型的能力声明（不改价格）。 */
  catalogAutoSync?: boolean;
  /** 会员档位优惠（按优先级）。 */
  memberTiers?: AIGatewayMemberTier[];
  timePricing?: AIGatewayTimePricing;
  welcome?: AIGatewayWelcome;
  store?: AIGatewayStoreSettings;
  /** 通用会员买额度包的折扣（0.85 = 85 折）与多送比例（0.1 = 多送 10%）。 */
  vipPackDiscount?: string;
  vipPackBonusRate?: string;
  updatedBy?: string;
  updatedAt?: string;
};

export type AIGatewayCatalog = {
  sources: AIGatewaySourceMeta[];
  kinds: AIGatewayKindMeta[];
  defaults: AIGatewaySettings;
  baseUrl: string;
  keyPrefix: string;
  /** 应用的会员档位，会员优惠里选档位用。 */
  vipTiers?: Array<{ id: number; name: string }>;
  /** 时段色调键：normal / off_peak / peak / promo。 */
  tones?: AIGatewayTimeTone[];
  /** 额度包配色键：blue / violet / amber / emerald / rose / slate。 */
  packageTones?: AIGatewayPackageTone[];
};

export type AIGatewayPrice = {
  inputPrice: string;
  outputPrice: string;
  requestPrice: string;
  imagePrice: string;
  imagePrices?: Record<string, string>;
  charPrice: string;
  minutePrice: string;
  free: boolean;
};

/** 思考强度，与 OpenAI 的 reasoning_effort 同一组取值，由低到高。 */
export const AI_REASONING_EFFORTS = ["none", "minimal", "low", "medium", "high", "xhigh", "max"] as const;
export type AIReasoningEffort = (typeof AI_REASONING_EFFORTS)[number];

/** 对话模型的能力声明：客户端据此决定发不发图片、带不带工具、给不给思考强度的选项。 */
export type AIGatewayCapabilities = {
  vision: boolean;
  tools: boolean;
  reasoning: boolean;
  /** 空表示 low / medium / high；含 none 表示可以关闭思考。 */
  reasoningEfforts: AIReasoningEffort[];
  /** 上下文长度（token），0 表示未知。 */
  contextLength: number;
  /** 模型本身的最大输出（只是声明，不是网关的输出上限）。 */
  maxOutput?: number;
  /** 最近一次从模型目录同步时对上的条目。 */
  catalogKey?: string;
};

// ── 公开模型目录（models.dev / OpenRouter / LiteLLM）──

export type ModelCatalogSource = { source: string; url: string; count: number; error?: string; fetchedAt: string };

export type ModelCatalogStatus = {
  fetchedAt: string | null;
  count: number;
  sources: ModelCatalogSource[];
  providers: { provider: string; count: number }[];
  refreshed: boolean;
};

export type ModelCatalogEntry = {
  key: string;
  provider: string;
  model: string;
  name?: string;
  family?: string;
  description?: string;
  kind: string;
  vision: boolean;
  tools: boolean;
  reasoning: boolean;
  reasoningEfforts?: AIReasoningEffort[];
  reasoningBudget?: { min?: number; max?: number };
  inputModalities?: string[];
  outputModalities?: string[];
  contextLength?: number;
  inputLimit?: number;
  maxOutput?: number;
  cost?: { input?: number; output?: number; cacheRead?: number; cacheWrite?: number; image?: number; request?: number };
  knowledge?: string;
  releaseDate?: string;
  status?: string;
  sources: string[];
};

export type ModelCatalogMatch = "exact" | "normalized" | "fuzzy";
export type ModelCatalogCandidate = { entry: ModelCatalogEntry; match: ModelCatalogMatch; score: number };

export type ModelCatalogField = "capabilities" | "info" | "prices";

export type ModelCatalogSyncPayload = {
  modelIds?: number[];
  fields: ModelCatalogField[];
  /** 1 美元折合多少点。 */
  priceRate?: string;
  overwrite?: boolean;
  preview?: boolean;
  allowFuzzy?: boolean;
  keys?: Record<number, string>;
};

export type ModelCatalogSyncResult = {
  modelId: number;
  model: string;
  upstream: string;
  match?: ModelCatalogCandidate;
  others?: ModelCatalogCandidate[];
  changes: { field: string; from: unknown; to: unknown }[];
  applied: boolean;
  skipped?: string;
};

export type AIGatewayModel = AIGatewayPrice & {
  id: number;
  appid: number;
  model: string;
  upstreamModel: string;
  kind: string;
  displayName: string;
  description: string;
  configId: number;
  enabled: boolean;
  vipOnly: boolean;
  maxOutputTokens: number;
  capabilities?: AIGatewayCapabilities;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
};

export type AIGatewayModelPayload = {
  model: string;
  upstreamModel: string;
  kind: string;
  displayName: string;
  description: string;
  configId: number;
  enabled: boolean;
  vipOnly: boolean;
  free: boolean;
  inputPrice: string;
  outputPrice: string;
  requestPrice: string;
  imagePrice: string;
  imagePrices: Record<string, string>;
  charPrice: string;
  minutePrice: string;
  maxOutputTokens: number;
  capabilities?: AIGatewayCapabilities;
  sortOrder: number;
};

export type AIGatewayRequestStatus = "reserved" | "succeeded" | "aborted" | "failed" | "expired" | "refunded";

/** 额度包来源在各批次上的拆分（微点）。 */
export type AIGatewayLotShare = { lotId: number; micro: number };

export type AIGatewayHold = {
  source: AIGatewaySourceKey;
  micro: number;
  units?: number;
  period?: string;
  /** source = pack 时各批次各扣了多少。 */
  lots?: AIGatewayLotShare[];
};

export type AIGatewayProfile = {
  isVip: boolean;
  level: number;
  tierIds?: number[];
  tierId?: number;
  tierName?: string;
};

/** 暂扣那一刻的计费口径；pricing 是倍率分解与命中的时段。 */
export type AIGatewayBillingSnapshot = {
  sources?: AIGatewaySourceKey[];
  walletRate?: string;
  timezone?: string;
  profile?: AIGatewayProfile;
  pricing?: AIGatewayFactorBreakdown | null;
};
export type AIGatewayCharge = AIGatewayHold & { txnNo?: string };

export type AIGatewayRequest = {
  id: number;
  requestNo: string;
  appid: number;
  userId: number;
  account?: string;
  keyId?: number;
  keyHint?: string;
  authKind: "key" | "session";
  endpoint: string;
  model: string;
  upstreamModel?: string;
  configId?: number;
  provider?: string;
  stream: boolean;
  status: AIGatewayRequestStatus;
  price: AIGatewayPrice;
  factor: string;
  estimate: string;
  reserved: string;
  holds: AIGatewayHold[];
  billing?: AIGatewayBillingSnapshot;
  clampMaxTokens?: number;
  inputTokens: number;
  outputTokens: number;
  images: number;
  imageSize?: string;
  imageQuality?: string;
  characters: number;
  audioMs: number;
  usageEstimated: boolean;
  cost: string;
  charged: string;
  shortfall: string;
  charges: AIGatewayCharge[];
  integralTxnNo?: string;
  walletTxnNo?: string;
  errorCode?: string;
  errorMessage?: string;
  clientIp?: string;
  userAgent?: string;
  latencyMs: number;
  /** 上游尝试次数；大于 1 表示发生过链路回退。 */
  attempts: number;
  firstTokenMs?: number;
  finishReason?: string;
  upstreamRequestId?: string;
  requestBytes?: number;
  responseBytes?: number;
  configName?: string;
  /** 只有单条查询带。 */
  trace?: AIGatewayTrace | null;
  /** 只在实时推送里有。 */
  live?: AIGatewayLiveProgress;
  refundReason?: string;
  refundedBy?: string;
  refundedAt?: string;
  createdAt: string;
  expiresAt: string;
  settledAt?: string;
};

export type AIGatewayAttemptStatus = "running" | "succeeded" | "failed" | "canceled";

/** 一次上游尝试（链路里的一条通道被调用了一次）。 */
export type AIGatewayAttempt = {
  seq: number;
  configId?: number;
  configName?: string;
  provider?: string;
  protocol?: string;
  inherited?: boolean;
  model?: string;
  url?: string;
  method?: string;
  status: AIGatewayAttemptStatus;
  httpStatus?: number;
  upstreamRequestId?: string;
  error?: string;
  retryable?: boolean;
  startedAt: string;
  headersMs?: number;
  firstTokenMs?: number;
  latencyMs: number;
  requestBytes?: number;
  responseBytes?: number;
  rateLimit?: Record<string, string>;
};

export type AIGatewayTrace = {
  attempts: AIGatewayAttempt[];
  request: Record<string, unknown>;
  response: Record<string, unknown>;
};

export type AIGatewayLiveProgress = {
  elapsedMs: number;
  firstTokenMs?: number;
  outputChars?: number;
  outputTokens?: number;
  attempt?: number;
};

export type AIGatewayUpstreamSummary = {
  configId: number;
  configName: string;
  provider: string;
  attempts: number;
  failed: number;
  avgLatencyMs: number;
  lastError?: string;
};

export type AIGatewayLiveSummary = {
  minutes: number;
  inFlight: number;
  total: number;
  succeeded: number;
  failed: number;
  retried: number;
  users: number;
  inputTokens: number;
  outputTokens: number;
  charged: string;
  avgLatencyMs: number;
  p95LatencyMs: number;
  avgFirstTokenMs: number;
  upstreams: AIGatewayUpstreamSummary[];
};

export type AIGatewayLiveEventType = "created" | "attempt" | "progress" | "finished" | "resync";

/** 调用记录实时事件（SSE，见 docs/ai-gateway.md「实时」）。 */
export type AIGatewayLiveEvent = {
  type: AIGatewayLiveEventType;
  appid: number;
  requestNo?: string;
  request?: AIGatewayRequest;
  attempts?: AIGatewayAttempt[];
  progress?: AIGatewayLiveProgress;
  at: string;
};

export type AIGatewayPage<T> = { items: T[]; total: number; page: number; limit: number };

export type AIGatewayRequestParams = {
  userId?: number;
  keyId?: number;
  model?: string;
  status?: string;
  endpoint?: string;
  /** 调用编号或上游请求编号（前缀）。 */
  keyword?: string;
  configId?: number;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type AIGatewayKey = {
  id: number;
  appid: number;
  userId: number;
  account?: string;
  name: string;
  hint: string;
  status: "active" | "disabled" | "revoked";
  models: string[];
  spendLimit: string;
  spent: string;
  expiresAt?: string;
  lastUsedAt?: string;
  lastUsedIp?: string;
  createdAt: string;
  updatedAt: string;
};

export type AIGatewayKeyParams = { userId?: number; status?: string; page?: number; limit?: number };

export type AIGatewayStats = {
  days: number;
  requests: number;
  succeeded: number;
  failed: number;
  users: number;
  inputTokens: number;
  outputTokens: number;
  images: number;
  charged: string;
  shortfall: string;
  refunded: string;
  bySource: Array<{ source: AIGatewaySourceKey; amount: string; units: number }>;
  byModel: Array<{ model: string; requests: number; inputTokens: number; outputTokens: number; charged: string }>;
  daily: Array<{ date: string; requests: number; failed: number; charged: string }>;
  reservedNow: number;
  creditBalance: string;
};

export type AIGatewayQuotaView = {
  source: AIGatewaySourceKey;
  label: string;
  period: string;
  limit: string;
  used: string;
  reserved: string;
  remaining: string;
  eligible: boolean;
};

export type AIGatewayOverview = {
  enabled: boolean;
  allowSessionToken: boolean;
  audience: "all" | "vip";
  eligible: boolean;
  unit?: string;
  sources: AIGatewaySourceMeta[];
  profile: AIGatewayProfile;
  member?: AIGatewayMemberBenefit;
  factor: string;
  pricing?: AIGatewayFactorBreakdown;
  nextChangeAt?: string | null;
  quotas: AIGatewayQuotaView[];
  /** 额度包余额（不含暂扣中）。 */
  pack?: string;
  packFrozen?: string;
  /** 永久额度。 */
  credit: string;
  creditFrozen: string;
  /** pack + credit（不含暂扣中）。 */
  balance?: string;
  /** 7 天内到期的批次合计与最早的到期时间。 */
  expiring?: { amount: string; at: string } | null;
  wallet: string;
  walletRate: string;
  available: string;
  dailySpendLimit: string;
  todaySpent: string;
  totalSpent: string;
  totalRequests: number;
  rpm: number;
  maxConcurrent: number;
  maxKeysPerUser: number;
  welcome?: { enabled: boolean; amount: string; validityDays: number; claimable: boolean; claimedAt?: string | null };
  store?: { enabled: boolean };
  baseUrl: string;
};

export type AIGatewayCreditKind =
  | "grant"
  | "consume"
  | "refund"
  | "adjust"
  | "purchase"
  | "welcome"
  | "expire"
  | "revoke"
  | "order_refund";

export type AIGatewayCreditEntry = {
  id: number;
  kind: AIGatewayCreditKind;
  /** credit 永久额度 / pack 额度包。 */
  bucket?: "credit" | "pack";
  lotId?: number;
  amount: string;
  /** 这一桶变动后的余额。 */
  balanceAfter: string;
  /** 变动后的 AI 额度合计（永久 + 额度包，不含暂扣中）。 */
  totalAfter?: string;
  refType?: string;
  refId?: string;
  remark?: string;
  operator?: string;
  createdAt: string;
};

export type AIGatewayCreditAdjustPayload = { amount: string; reason: string; requestId: string };

// ── 额度商店 ──

export type AIGatewayPackageTone = "blue" | "violet" | "amber" | "emerald" | "rose" | "slate";

export type AIGatewayPackagePayload = {
  name: string;
  subtitle: string;
  description: string;
  badge: string;
  tone: AIGatewayPackageTone;
  featured: boolean;
  points: string;
  bonusPoints: string;
  price: string;
  originalPrice: string;
  /** 0 = 永久。 */
  validityDays: number;
  audience: "all" | "vip";
  tierIds: number[];
  memberDiscount: boolean;
  perUserLimit: number;
  monthlyLimit: number;
  /** 总库存，0 不限。 */
  stock: number;
  startsAt: string | null;
  endsAt: string | null;
  /** 上架。 */
  enabled: boolean;
  sortOrder: number;
};

export type AIGatewayPackage = AIGatewayPackagePayload & {
  id: number;
  appid?: number;
  sold: number;
  /** 管理端列表另带的累计数据。 */
  orders?: number;
  buyers?: number;
  revenue?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type AIGatewayPackageOrder = {
  orderNo: string;
  packageId: number;
  packageName: string;
  listPrice: string;
  price: string;
  discount: string;
  discountLabel?: string;
  points: string;
  /** 包自带赠送 + 会员多送之和。 */
  bonusPoints: string;
  totalPoints: string;
  validityDays: number;
  expiresAt?: string | null;
  lotId: number;
  walletTxnNo?: string;
  /** paid / refunded。 */
  status: string;
  refundAmount: string;
  refundPoints: string;
  refundReason?: string;
  refundedAt?: string | null;
  createdAt: string;
  userId: number;
  account?: string;
  refundedBy?: string;
  /**
   * 以下四项只在管理端、且订单未退款并有批次时给：批次总量 / 剩余（批次已过期或作废视为 0）/ 暂扣中，
   * 与预计退款 floor₂(price × lotRemaining / lotAmount)。lotFrozen > 0 时退款会被拒绝（40238）。
   */
  lotAmount?: string;
  lotRemaining?: string;
  lotFrozen?: string;
  refundEstimate?: string;
};

export type AIGatewayPackageOrderParams = {
  userId?: number;
  packageId?: number;
  status?: string;
  /** 订单号前缀。 */
  keyword?: string;
  from?: string;
  to?: string;
  page?: number;
  limit?: number;
};

/** purchase 购买 / welcome 新人礼 / admin 管理员发放 / grant 发放规则与活动。 */
export type AIGatewayLotOrigin = string;
export type AIGatewayLotStatus = "active" | "expired" | "revoked" | "exhausted";

export type AIGatewayCreditLot = {
  id: number;
  origin: AIGatewayLotOrigin;
  title: string;
  amount: string;
  remaining: string;
  frozen: string;
  used: string;
  orderNo?: string;
  packageId?: number;
  expiresAt?: string | null;
  status: AIGatewayLotStatus;
  createdAt: string;
  userId: number;
  account?: string;
};

export type AIGatewayLotParams = {
  userId?: number;
  status?: AIGatewayLotStatus;
  origin?: string;
  page?: number;
  limit?: number;
};

export type AIGatewayLotGrantPayload = { amount: string; validityDays: number; title: string; requestId: string };

export type AIGatewayCommerce = {
  days: number;
  revenue: string;
  refunded: string;
  orders: number;
  buyers: number;
  pointsSold: string;
  /** 未消耗负债：额度包 / 永久额度。 */
  outstanding: string;
  outstandingPermanent: string;
  expiringSoon: string;
  byPackage: Array<{ packageId: number; name: string; orders: number; revenue: string; points: string }>;
  daily: Array<{ date: string; orders: number; revenue: string; refunded: string }>;
  /** bucket：7d / 30d / 90d / later / never。 */
  expiry: Array<{ bucket: string; amount: string }>;
  /** saved = charged / time − charged，加价时段为负。 */
  windows: Array<{ windowId: string; name: string; requests: number; charged: string; saved: string }>;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/ai-gateway`;

export function getAIGatewayCatalog(token: string, appKey: string) {
  return apiRequest<AIGatewayCatalog>(`${base(appKey)}/catalog`, { token });
}

export function getModelCatalog(token: string, appKey: string) {
  return apiRequest<ModelCatalogStatus>(`${base(appKey)}/model-catalog`, { token });
}

export function refreshModelCatalog(token: string, appKey: string) {
  return apiRequest<ModelCatalogStatus>(`${base(appKey)}/model-catalog/refresh`, { method: "POST", token });
}

export function searchModelCatalog(token: string, appKey: string, params: { q?: string; provider?: string; kind?: string; limit?: number }) {
  return apiRequest<{ items: ModelCatalogEntry[] }>(`${base(appKey)}/model-catalog/search${buildQuery(params)}`, { token });
}

export function matchModelCatalog(token: string, appKey: string, params: { model: string; configId?: number; limit?: number }) {
  return apiRequest<{ items: ModelCatalogCandidate[] }>(`${base(appKey)}/model-catalog/match${buildQuery(params)}`, { token });
}

export function syncAIGatewayModels(token: string, appKey: string, payload: ModelCatalogSyncPayload) {
  return apiRequest<{ items: ModelCatalogSyncResult[] }>(`${base(appKey)}/models/sync`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

/** 价格模拟：settings 传草稿时按草稿算，不写库。 */
export function previewAIGatewayPricing(token: string, appKey: string, payload: AIGatewayPricingPreviewPayload) {
  return apiRequest<AIGatewayPricingPreview>(`${base(appKey)}/pricing/preview`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function getAIGatewaySettings(token: string, appKey: string) {
  return apiRequest<AIGatewaySettings>(`${base(appKey)}/settings`, { token });
}

export function saveAIGatewaySettings(token: string, appKey: string, payload: AIGatewaySettings) {
  return apiRequest<AIGatewaySettings>(`${base(appKey)}/settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function listAIGatewayModels(token: string, appKey: string) {
  return apiRequest<{ items: AIGatewayModel[] }>(`${base(appKey)}/models`, { token });
}

export function createAIGatewayModel(token: string, appKey: string, payload: AIGatewayModelPayload) {
  return apiRequest<AIGatewayModel>(`${base(appKey)}/models`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAIGatewayModel(token: string, appKey: string, modelId: number, payload: AIGatewayModelPayload) {
  return apiRequest<AIGatewayModel>(`${base(appKey)}/models/${modelId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function deleteAIGatewayModel(token: string, appKey: string, modelId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/models/${modelId}`, { method: "DELETE", token });
}

export function listAIGatewayRequests(token: string, appKey: string, params: AIGatewayRequestParams) {
  return apiRequest<AIGatewayPage<AIGatewayRequest>>(`${base(appKey)}/requests${buildQuery(params)}`, { token });
}

export function getAIGatewayRequest(token: string, appKey: string, requestNo: string) {
  return apiRequest<AIGatewayRequest>(`${base(appKey)}/requests/${encodeURIComponent(requestNo)}`, { token });
}

export function getAIGatewayRequestSummary(token: string, appKey: string, minutes: number) {
  return apiRequest<AIGatewayLiveSummary>(`${base(appKey)}/requests/summary${buildQuery({ minutes })}`, { token });
}

/** SSE 连接的结局：服务端到点换连接（bye）、断开、或凭据失效（不再重连）。 */
export type AIGatewayLiveEnd = "bye" | "closed" | "unauthorized";

/**
 * 读调用记录的实时事件流。浏览器的 EventSource 带不了 Authorization 头，所以用 fetch 读流自己按 SSE 切分。
 * 连上后先 onOpen；流结束时兑现结局，网络错误抛出（由调用方退避重连）。
 */
export async function streamAIGatewayLive(
  token: string,
  appKey: string,
  handlers: { onOpen: () => void; onEvent: (event: AIGatewayLiveEvent) => void },
  signal: AbortSignal
): Promise<AIGatewayLiveEnd> {
  const response = await fetch(joinApiUrl(`${base(appKey)}/requests/live`), {
    headers: { Accept: "text/event-stream", Authorization: `Bearer ${token}`, "X-Admin-Token": token },
    cache: "no-store",
    signal
  });
  if (response.status === 401 || response.status === 403) return "unauthorized";
  if (!response.ok || !response.body) throw new Error(`实时连接失败（${response.status}）`);
  handlers.onOpen();
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return "closed";
    buffer += value;
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      boundary = buffer.indexOf("\n\n");
      let name = "message";
      const data: string[] = [];
      for (const line of block.split("\n")) {
        if (line.startsWith("event:")) name = line.slice(6).trim();
        else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
      }
      if (name === "bye") {
        await reader.cancel().catch(() => undefined);
        return "bye";
      }
      if (name === "ready" || data.length === 0) continue;
      try {
        handlers.onEvent(JSON.parse(data.join("\n")) as AIGatewayLiveEvent);
      } catch {
        // 单条坏数据不影响后续事件。
      }
    }
  }
}

export function refundAIGatewayRequest(token: string, appKey: string, requestNo: string, reason: string) {
  return apiRequest<AIGatewayRequest>(`${base(appKey)}/requests/${encodeURIComponent(requestNo)}/refund`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export function listAIGatewayKeys(token: string, appKey: string, params: AIGatewayKeyParams) {
  return apiRequest<AIGatewayPage<AIGatewayKey>>(`${base(appKey)}/keys${buildQuery(params)}`, { token });
}

export function setAIGatewayKeyStatus(token: string, appKey: string, keyId: number, status: "active" | "disabled") {
  return apiRequest<AIGatewayKey>(`${base(appKey)}/keys/${keyId}/status`, {
    method: "PUT",
    token,
    body: JSON.stringify({ status })
  });
}

export function getAIGatewayStats(token: string, appKey: string, days: number) {
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  return apiRequest<AIGatewayStats>(`${base(appKey)}/stats${buildQuery({ days, tz })}`, { token });
}

export function getAIGatewayUserAccount(token: string, appKey: string, userId: number) {
  return apiRequest<AIGatewayOverview>(`${base(appKey)}/users/${userId}/account`, { token });
}

export function listAIGatewayUserCredits(token: string, appKey: string, userId: number, page: number, limit: number) {
  return apiRequest<AIGatewayPage<AIGatewayCreditEntry>>(
    `${base(appKey)}/users/${userId}/credits${buildQuery({ page, limit })}`,
    { token }
  );
}

export function adjustAIGatewayUserCredit(
  token: string,
  appKey: string,
  userId: number,
  payload: AIGatewayCreditAdjustPayload
) {
  return apiRequest<{ balance: string; replayed: boolean }>(`${base(appKey)}/users/${userId}/credits`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

// ── 额度商店 ──

export function listAIGatewayPackages(token: string, appKey: string) {
  return apiRequest<{ items: AIGatewayPackage[] }>(`${base(appKey)}/packages`, { token });
}

export function createAIGatewayPackage(token: string, appKey: string, payload: AIGatewayPackagePayload) {
  return apiRequest<AIGatewayPackage>(`${base(appKey)}/packages`, { method: "POST", token, body: JSON.stringify(payload) });
}

export function updateAIGatewayPackage(token: string, appKey: string, packageId: number, payload: AIGatewayPackagePayload) {
  return apiRequest<AIGatewayPackage>(`${base(appKey)}/packages/${packageId}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

/** 有订单的包删不掉（40234），只能下架。 */
export function deleteAIGatewayPackage(token: string, appKey: string, packageId: number) {
  return apiRequest<{ deleted: boolean }>(`${base(appKey)}/packages/${packageId}`, { method: "DELETE", token });
}

export function listAIGatewayPackageOrders(token: string, appKey: string, params: AIGatewayPackageOrderParams) {
  return apiRequest<AIGatewayPage<AIGatewayPackageOrder>>(`${base(appKey)}/package-orders${buildQuery(params)}`, { token });
}

/** 按批次剩余比例退款：refundAmount = floor₂(price × remaining / amount)。 */
export function refundAIGatewayPackageOrder(token: string, appKey: string, orderNo: string, reason: string) {
  return apiRequest<AIGatewayPackageOrder>(`${base(appKey)}/package-orders/${encodeURIComponent(orderNo)}/refund`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export function listAIGatewayLots(token: string, appKey: string, params: AIGatewayLotParams) {
  return apiRequest<AIGatewayPage<AIGatewayCreditLot>>(`${base(appKey)}/lots${buildQuery(params)}`, { token });
}

/** 返回新建的批次；同一 requestId 重放返回原批次。 */
export function grantAIGatewayLot(token: string, appKey: string, userId: number, payload: AIGatewayLotGrantPayload) {
  return apiRequest<AIGatewayCreditLot>(`${base(appKey)}/users/${userId}/lots`, {
    method: "POST",
    token,
    body: JSON.stringify(payload)
  });
}

export function revokeAIGatewayLot(token: string, appKey: string, lotId: number, reason: string) {
  return apiRequest<AIGatewayCreditLot>(`${base(appKey)}/lots/${lotId}/revoke`, {
    method: "POST",
    token,
    body: JSON.stringify({ reason })
  });
}

export function getAIGatewayCommerce(token: string, appKey: string, days: number) {
  return apiRequest<AIGatewayCommerce>(`${base(appKey)}/commerce${buildQuery({ days })}`, { token });
}
