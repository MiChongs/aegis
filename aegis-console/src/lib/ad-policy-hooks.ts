"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getAdPolicy,
  getAdPolicyStats,
  getUserAdProfile,
  listAdConsentLogs,
  listAdConsents,
  listSplashAdEvents,
  saveAdPolicy,
  type AdConsentLogParams,
  type AdConsentParams,
  type SaveAdPolicyPayload,
  type SplashAdEventParams
} from "@/lib/api/ad-policy";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 广告服务策略域 hooks。
 *
 * 保存策略会改当前条款版本，进而改变「待重新选择」的人数、用户列表里的过期标记
 * 与用户详情里的服务模式 —— 保存后整组失效。
 */

const KEY = {
  config: "admin-ad-policy-config",
  consents: "admin-ad-policy-consents",
  logs: "admin-ad-policy-consent-logs",
  splash: "admin-ad-policy-splash-events",
  stats: "admin-ad-policy-stats",
  user: "admin-ad-policy-user"
} as const;

const AD_POLICY_SCOPE = Object.values(KEY);

function useInvalidateAdPolicyScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(AD_POLICY_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useAdPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.config, token, appKey],
    queryFn: () => getAdPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateAdPolicyScope();
  return useMutation({
    mutationFn: (payload: SaveAdPolicyPayload) => saveAdPolicy(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useAdConsentsQuery(appKey?: string | null, params?: AdConsentParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.consents,
      token,
      appKey,
      params?.accepted ?? "",
      params?.outdated ?? "",
      params?.source ?? "",
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listAdConsents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdConsentLogsQuery(appKey?: string | null, params?: AdConsentLogParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.logs, token, appKey, params?.userId ?? 0, params?.keyword ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listAdConsentLogs(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useSplashAdEventsQuery(appKey?: string | null, params?: SplashAdEventParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.splash,
      token,
      appKey,
      params?.userId ?? 0,
      params?.status ?? "",
      params?.keyword ?? "",
      params?.start ?? "",
      params?.end ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listSplashAdEvents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useAdPolicyStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getAdPolicyStats(token as string, appKey as string, days),
    enabled: Boolean(token && appKey)
  });
}

export function useUserAdProfileQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.user, token, appKey, userId],
    queryFn: () => getUserAdProfile(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}
