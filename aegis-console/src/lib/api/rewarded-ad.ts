import { apiRequest, buildQuery } from "@/lib/api/client";
import type { CardKeyReward, CardKeyRewardResult, CardKeyRewardSpec } from "@/lib/api/card-key";

/**
 * 激励广告域 API。
 *
 * 一次观看由两方确认：广告平台的服务端回调（验签）与客户端上报（带用户令牌），
 * 两方靠平台的 transId 对上。权益沿用卡密的权益目录与数据形态，但只开放其中几档
 * （目录由后端随配置一起下发，前端不另抄一份）。
 */

export type RewardedAdVerifyMode = "dual" | "server" | "client";
export type RewardedAdViewStatus = "pending" | "granted" | "rejected";

export type RewardedAdScene = {
  key: string;
  name: string;
  placementId: string;
  enabled: boolean;
  rewards: CardKeyReward[];
  /** 每人每天在该场景最多领几次，0 表示只受总限额约束 */
  dailyLimit: number;
  /** 两次领取的最小间隔（秒），0 表示不限 */
  cooldownSeconds: number;
  /**
   * 送出的会员带什么权益，跟随当前配置（改了对已领到的会员同样生效）。
   * features 只对「会员天数」生效（「会员套餐」跟随套餐）；adFree 不填按免广告。
   */
  membership?: RewardedAdMembership;
  /**
   * 永久会员（或已永久拥有某功能的人）看这个场景时，已拥有的权益怎么办。
   * 不传按「按价值折算」：会员每天 10、功能时长每小时 10，折成积分。
   */
  lifetime?: RewardedAdLifetime;
};

export type RewardedAdLifetimePolicy = "convert" | "replace" | "hide" | "grant";

export type RewardedAdLifetime = {
  policy: RewardedAdLifetimePolicy;
  /** convert / hide：折成哪一档（integral / ai_credit / experience / lottery_draws） */
  target?: string;
  /** 会员每 1 天折多少 */
  perVipDay?: number;
  /** 功能时长每 60 分钟折多少，不足一小时按比例向上取整 */
  perFeatureHour?: number;
  /** replace：改发的权益 */
  rewards?: CardKeyReward[];
};

export type RewardedAdLifetimePolicySpec = {
  key: RewardedAdLifetimePolicy;
  label: string;
  hint: string;
};

/** 一次观看里「场景配的是 A，发的是 B」 */
export type RewardedAdConversion = {
  policy: RewardedAdLifetimePolicy;
  reason: "lifetime" | "feature";
  owned: CardKeyReward[];
  ownedLabel: string;
  rewards: CardKeyReward[];
  label: string;
  message: string;
};

export type RewardedAdMembership = {
  features: string[];
  adFree?: boolean;
};

/** 广告位的回调验签实际用哪一把 Key：自己的 / 应用默认的 / 都没有（回调会验签失败） */
export type RewardedAdKeySource = "own" | "default" | "missing";

/**
 * 平台上的一个激励广告位。Apex 的 Security Key 按广告位签发，各广告位的回调用各自的 Key 验签；
 * 没单独配 Key 的广告位用应用的默认 Key。Key 只回「配没配」与末 4 位提示。
 */
export type RewardedAdPlacement = {
  id: string;
  name: string;
  hasSecurityKey: boolean;
  securityKeyHint?: string;
  keyUpdatedAt?: string;
  keySource: RewardedAdKeySource;
  /** 用这个广告位的场景标识 */
  scenes: string[];
  /** 统计窗口（activityDays）内最近一次验签通过的回调 */
  lastCallbackAt?: string;
  /** 统计窗口内验签通过的回调次数 */
  callbacks: number;
};

export type RewardedAdConfig = {
  enabled: boolean;
  provider: string;
  providerAppId: string;
  /** 应用的默认 Key（给没单独配 Key 的广告位兜底） */
  hasSecurityKey: boolean;
  securityKeyHint?: string;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: number;
  placements?: RewardedAdPlacement[];
  /** placements 里回调统计的窗口（天） */
  activityDays?: number;
  scenes: RewardedAdScene[];
  /** 填进广告平台「配置回调URL」的地址；未配 API_BASE_URL 时只有路径 */
  callbackUrl: string;
  callbackAbsolute: boolean;
  updatedBy?: string;
  updatedAt?: string;
  catalog: CardKeyRewardSpec[];
  verifyModes: RewardedAdVerifyMode[];
  providers: string[];
  lifetimePolicies?: RewardedAdLifetimePolicySpec[];
  convertTargets?: string[];
};

/** 保存时的一个广告位。securityKey 留空沿用；改了 ID 时 previousId 填原 ID，已存的 Key 随之带过去 */
export type SaveRewardedAdPlacement = {
  id: string;
  previousId?: string;
  name: string;
  securityKey?: string;
  clearSecurityKey?: boolean;
};

export type SaveRewardedAdConfigPayload = {
  enabled: boolean;
  provider: string;
  providerAppId: string;
  /** 默认 Key，留空表示不修改 */
  securityKey?: string;
  clearSecurityKey?: boolean;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: number;
  /** 不传表示不改广告位列表；传了整体替换 */
  placements?: SaveRewardedAdPlacement[];
  scenes: RewardedAdScene[];
};

export type RewardedAdView = {
  id: number;
  appid: number;
  userId: number;
  account?: string;
  transId: string;
  scene: string;
  sceneName?: string;
  placementId: string;
  status: RewardedAdViewStatus;
  reason?: string;
  serverVerifiedAt?: string;
  rewardName?: string;
  rewardAmount?: number;
  networkId?: string;
  extra?: string;
  clientReportedAt?: string;
  clientVerified?: boolean;
  clientError?: string;
  deviceId?: string;
  clientIp?: string;
  results: CardKeyRewardResult[];
  /** 已拥有的权益折算或改发了什么，照常发放时为空 */
  conversion?: RewardedAdConversion;
  grantedAt?: string;
  operator?: string;
  createdAt: string;
  updatedAt: string;
};

export type RewardedAdViewPage = {
  items: RewardedAdView[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type RewardedAdViewParams = {
  status?: RewardedAdViewStatus;
  scene?: string;
  keyword?: string;
  userId?: number;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type RewardedAdStatsDay = {
  date: string;
  views: number;
  granted: number;
  rejected: number;
  users: number;
  converted?: number;
};

export type RewardedAdStats = {
  days: number;
  summary: {
    todayViews: number;
    todayGranted: number;
    todayRejected: number;
    todayUsers: number;
    pending: number;
    totalGranted: number;
    totalConverted?: number;
    convertedIntegral?: number;
  };
  trend: RewardedAdStatsDay[];
  scenes: Array<{ scene: string; name?: string; views: number; granted: number; converted?: number }>;
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/rewarded-ads`;

export function getRewardedAdConfig(token: string, appKey: string) {
  return apiRequest<RewardedAdConfig>(`${base(appKey)}/config`, { token });
}

export function saveRewardedAdConfig(token: string, appKey: string, payload: SaveRewardedAdConfigPayload) {
  return apiRequest<RewardedAdConfig>(`${base(appKey)}/config`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function listRewardedAdViews(token: string, appKey: string, params?: RewardedAdViewParams) {
  return apiRequest<RewardedAdViewPage>(`${base(appKey)}/views${buildQuery(params ?? {})}`, { token });
}

export function getRewardedAdStats(token: string, appKey: string, days: number) {
  return apiRequest<RewardedAdStats>(`${base(appKey)}/stats${buildQuery({ days })}`, { token });
}

export function grantRewardedAdView(token: string, appKey: string, viewId: number, scene?: string) {
  return apiRequest<RewardedAdView>(`${base(appKey)}/views/${viewId}/grant`, {
    method: "POST",
    token,
    body: JSON.stringify({ scene: scene ?? "" })
  });
}
