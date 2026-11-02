"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createPromoCampaign,
  deletePromoCampaign,
  getPromoCampaign,
  getPromoCatalog,
  getPromoLeaderboard,
  getPromoStats,
  listPromoCampaigns,
  listPromoSubmissions,
  reviewPromoSubmissions,
  setPromoCampaignStatus,
  updatePromoCampaign,
  type PromoCampaignParams,
  type PromoCampaignPayload,
  type PromoReviewPayload,
  type PromoStatus,
  type PromoSubmissionParams
} from "@/lib/api/promo";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 推广活动域 hooks。
 *
 * 失效关系集中在这里：改活动（保存、上下线、删除）影响活动列表、详情与统计；审核一批提交影响
 * 提交列表、活动列表上的待审与通过数、统计。整组失效，不在组件里逐个点名。目录不随操作变化，不失效。
 */

const KEY = {
  catalog: "admin-promo-catalog",
  campaigns: "admin-promo-campaigns",
  campaign: "admin-promo-campaign",
  stats: "admin-promo-stats",
  leaderboard: "admin-promo-leaderboard",
  submissions: "admin-promo-submissions"
} as const;

const PROMO_SCOPE = [KEY.campaigns, KEY.campaign, KEY.stats, KEY.leaderboard, KEY.submissions];

function useInvalidatePromoScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(PROMO_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 目录 ──

export function usePromoCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getPromoCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 5 * 60_000
  });
}

// ── 活动 ──

export function usePromoCampaignsQuery(appKey?: string | null, params?: PromoCampaignParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.campaigns,
      token,
      appKey,
      params?.status ?? "",
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listPromoCampaigns(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function usePromoCampaignQuery(appKey?: string | null, id?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.campaign, token, appKey, id ?? 0],
    queryFn: () => getPromoCampaign(token as string, appKey as string, id as number),
    enabled: Boolean(token && appKey && id)
  });
}

export function useSavePromoCampaignMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePromoScope();
  return useMutation({
    mutationFn: (input: { id?: number; payload: PromoCampaignPayload }) =>
      input.id
        ? updatePromoCampaign(token as string, appKey as string, input.id, input.payload)
        : createPromoCampaign(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useSetPromoCampaignStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePromoScope();
  return useMutation({
    mutationFn: (input: { id: number; status: PromoStatus }) =>
      setPromoCampaignStatus(token as string, appKey as string, input.id, input.status),
    onSuccess: invalidate
  });
}

export function useDeletePromoCampaignMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePromoScope();
  return useMutation({
    mutationFn: (id: number) => deletePromoCampaign(token as string, appKey as string, id),
    onSuccess: invalidate
  });
}

export function usePromoStatsQuery(appKey?: string | null, id?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, id ?? 0],
    queryFn: () => getPromoStats(token as string, appKey as string, id as number),
    enabled: Boolean(token && appKey && id)
  });
}

export function usePromoLeaderboardQuery(appKey?: string | null, id?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.leaderboard, token, appKey, id ?? 0],
    queryFn: () => getPromoLeaderboard(token as string, appKey as string, id as number),
    enabled: Boolean(token && appKey && id)
  });
}

// ── 分享提交 ──

export function usePromoSubmissionsQuery(appKey?: string | null, params?: PromoSubmissionParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.submissions,
      token,
      appKey,
      params?.campaignId ?? 0,
      params?.status ?? "",
      params?.platform ?? "",
      params?.userId ?? 0,
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listPromoSubmissions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

/** 一批审核逐条处理、各自成败：返回每条的结论，失败的由调用方展示 */
export function useReviewPromoSubmissionsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePromoScope();
  return useMutation({
    mutationFn: (payload: PromoReviewPayload) => reviewPromoSubmissions(token as string, appKey as string, payload),
    onSettled: invalidate
  });
}
