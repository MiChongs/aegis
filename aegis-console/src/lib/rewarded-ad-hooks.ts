"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getRewardedAdConfig,
  getRewardedAdStats,
  grantRewardedAdView,
  listRewardedAdViews,
  saveRewardedAdConfig,
  type RewardedAdViewParams,
  type SaveRewardedAdConfigPayload
} from "@/lib/api/rewarded-ad";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 激励广告域 hooks。
 *
 * 失效关系集中在这里：保存配置会改场景名（记录表与概览按场景名展示），
 * 补发一条记录会同时改记录表与概览里的发放数 —— 任一写操作后整组失效。
 */

const KEY = {
  config: "admin-rewarded-ad-config",
  views: "admin-rewarded-ad-views",
  stats: "admin-rewarded-ad-stats"
} as const;

const REWARDED_AD_SCOPE = Object.values(KEY);

function useInvalidateRewardedAdScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(REWARDED_AD_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useRewardedAdConfigQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.config, token, appKey],
    queryFn: () => getRewardedAdConfig(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveRewardedAdConfigMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRewardedAdScope();
  return useMutation({
    mutationFn: (payload: SaveRewardedAdConfigPayload) =>
      saveRewardedAdConfig(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useRewardedAdViewsQuery(appKey?: string | null, params?: RewardedAdViewParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.views,
      token,
      appKey,
      params?.status ?? "",
      params?.scene ?? "",
      params?.keyword ?? "",
      params?.start ?? "",
      params?.end ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listRewardedAdViews(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useRewardedAdStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getRewardedAdStats(token as string, appKey as string, days),
    enabled: Boolean(token && appKey)
  });
}

export function useGrantRewardedAdViewMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateRewardedAdScope();
  return useMutation({
    mutationFn: ({ viewId, scene }: { viewId: number; scene?: string }) =>
      grantRewardedAdView(token as string, appKey as string, viewId, scene),
    onSuccess: invalidate
  });
}
