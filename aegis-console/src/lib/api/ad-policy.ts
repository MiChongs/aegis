import { apiRequest, buildQuery } from "@/lib/api/client";

/**
 * 广告服务策略域 API。
 *
 * 是否同意广告服务记在账号上：应用开启策略后，拒绝或尚未选择的用户只能使用基础服务
 * （客户端的功能标识清单），有效会员可免除。开屏广告逐次记录，登录用户挂在账号上。
 * 服务模式的判定只在后端（adpolicy.Evaluate），前端只展示结论。
 */

export type AdPolicyMode = "full" | "basic";
export type AdConsentSource = "app" | "web" | "guest_sync";
export type SplashAdStatus = "shown" | "clicked" | "failed" | "timeout";

export type AdSplashConfig = {
  enabled: boolean;
  /** 留空时客户端使用包里内置的广告位 */
  placementId: string;
  /** 两次开屏的最小间隔（秒），0 表示不限 */
  minIntervalSeconds: number;
  /** 每人每天最多展示几次，0 表示不限 */
  dailyLimit: number;
};

export type AdPolicy = {
  appid: number;
  enabled: boolean;
  consentVersion: number;
  policyUrl: string;
  vipExempt: boolean;
  basicTools: string[];
  splash: AdSplashConfig;
  /** 没保存过时为 false，客户端按自己内置的默认策略处理 */
  configured: boolean;
  updatedBy?: string;
  updatedAt?: string;
};

export type SaveAdPolicyPayload = {
  enabled: boolean;
  consentVersion: number;
  policyUrl: string;
  vipExempt: boolean;
  basicTools: string[];
  splash: AdSplashConfig;
};

export type AdConsent = {
  userId?: number;
  account?: string;
  accepted: boolean;
  version: number;
  source: AdConsentSource;
  deviceId?: string;
  clientIp?: string;
  decidedAt: string;
  /** 选择所依据的条款版本已不是当前版本 */
  outdated?: boolean;
};

export type AdConsentLog = {
  id: number;
  userId: number;
  account?: string;
  accepted: boolean;
  version: number;
  source: AdConsentSource;
  deviceId?: string;
  clientIp?: string;
  createdAt: string;
};

export type SplashAdEvent = {
  id: number;
  userId?: number;
  account?: string;
  eventId: string;
  deviceId?: string;
  placementId: string;
  status: SplashAdStatus;
  errorCode?: string;
  errorMessage?: string;
  loadMs: number;
  shownMs: number;
  clientIp?: string;
  occurredAt: string;
  createdAt: string;
};

export type AdPolicyPage<T> = {
  items: T[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type AdConsentParams = {
  accepted?: boolean;
  outdated?: boolean;
  source?: AdConsentSource;
  keyword?: string;
  page?: number;
  limit?: number;
};

export type AdConsentLogParams = {
  userId?: number;
  keyword?: string;
  page?: number;
  limit?: number;
};

export type SplashAdEventParams = {
  userId?: number;
  status?: SplashAdStatus;
  keyword?: string;
  start?: string;
  end?: string;
  page?: number;
  limit?: number;
};

export type AdPolicyStatsDay = {
  date: string;
  shown: number;
  clicked: number;
  failed: number;
  splashUsers: number;
  accepted: number;
  declined: number;
};

export type AdPolicyStats = {
  days: number;
  summary: {
    accepted: number;
    declined: number;
    outdated: number;
    todayShown: number;
    todayClicked: number;
    todayFailed: number;
    todayUsers: number;
  };
  trend: AdPolicyStatsDay[];
};

export type UserAdProfile = {
  configured: boolean;
  enabled: boolean;
  consentVersion: number;
  consent?: AdConsent | null;
  vip: boolean;
  exempt: boolean;
  decisionRequired: boolean;
  mode: AdPolicyMode;
  logs: AdConsentLog[];
  splash: {
    shown: number;
    clicked: number;
    failed: number;
    todayShown: number;
    lastShownAt?: string;
  };
  recentSplash: SplashAdEvent[];
};

const base = (appKey: string) => `/api/admin/apps/${encodeURIComponent(appKey)}/ad-policy`;

export function getAdPolicy(token: string, appKey: string) {
  return apiRequest<AdPolicy>(`${base(appKey)}/config`, { token });
}

export function saveAdPolicy(token: string, appKey: string, payload: SaveAdPolicyPayload) {
  return apiRequest<AdPolicy>(`${base(appKey)}/config`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload)
  });
}

export function listAdConsents(token: string, appKey: string, params?: AdConsentParams) {
  return apiRequest<AdPolicyPage<AdConsent>>(`${base(appKey)}/consents${buildQuery(params ?? {})}`, { token });
}

export function listAdConsentLogs(token: string, appKey: string, params?: AdConsentLogParams) {
  return apiRequest<AdPolicyPage<AdConsentLog>>(`${base(appKey)}/consent-logs${buildQuery(params ?? {})}`, {
    token
  });
}

export function listSplashAdEvents(token: string, appKey: string, params?: SplashAdEventParams) {
  return apiRequest<AdPolicyPage<SplashAdEvent>>(`${base(appKey)}/splash-events${buildQuery(params ?? {})}`, {
    token
  });
}

export function getAdPolicyStats(token: string, appKey: string, days: number) {
  return apiRequest<AdPolicyStats>(`${base(appKey)}/stats${buildQuery({ days })}`, { token });
}

export function getUserAdProfile(token: string, appKey: string, userId: number) {
  return apiRequest<UserAdProfile>(
    `/api/admin/apps/${encodeURIComponent(appKey)}/users/${userId}/ad-policy`,
    { token }
  );
}
