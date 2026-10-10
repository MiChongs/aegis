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
};

export type RewardedAdMembership = {
  features: string[];
  adFree?: boolean;
};

export type RewardedAdConfig = {
  enabled: boolean;
  provider: string;
  providerAppId: string;
  hasSecurityKey: boolean;
  securityKeyHint?: string;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: number;
  scenes: RewardedAdScene[];
  /** 填进广告平台「配置回调URL」的地址；未配 API_BASE_URL 时只有路径 */
  callbackUrl: string;
  callbackAbsolute: boolean;
  updatedBy?: string;
  updatedAt?: string;
  catalog: CardKeyRewardSpec[];
  verifyModes: RewardedAdVerifyMode[];
  providers: string[];
};

export type SaveRewardedAdConfigPayload = {
  enabled: boolean;
  provider: string;
  providerAppId: string;
  /** 留空表示不修改 */
  securityKey?: string;
  clearSecurityKey?: boolean;
  verifyMode: RewardedAdVerifyMode;
  dailyLimit: number;
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
  };
  trend: RewardedAdStatsDay[];
  scenes: Array<{ scene: string; name?: string; views: number; granted: number }>;
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
