"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelNotificationCampaign,
  createNotificationCampaign,
  getNotificationCampaign,
  getNotificationCampaignCatalog,
  listNotificationCampaigns,
  previewNotificationAudience,
  recallNotificationCampaign,
  type NotificationAudience,
  type NotificationCampaign,
  type NotificationCampaignListParams,
  type NotificationCampaignPayload
} from "@/lib/api/notification-campaigns";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 站内信群发任务 hooks（docs/notification-center.md「群发任务」）。
 *
 * 定时、排队、发送中、撤回中的任务按固定间隔轮询（执行器每批提交一次进度），全部结束即停。
 * 发起、取消、撤回后任务列表、任务详情与管理端消息列表一起失效。
 */

const KEY = {
  catalog: "admin-notification-campaign-catalog",
  preview: "admin-notification-campaign-preview",
  list: "admin-notification-campaigns",
  detail: "admin-notification-campaign"
} as const;

const POLL_MS = 2000;

const ACTIVE_STATUSES = new Set(["scheduled", "queued", "running", "recalling"]);

export const isCampaignActive = (campaign?: NotificationCampaign | null) =>
  Boolean(campaign && ACTIVE_STATUSES.has(campaign.status));

export function useNotificationCampaignCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getNotificationCampaignCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 10 * 60_000
  });
}

/** 人群预览：人群本身进查询键，调用方先防抖再传进来；`audience` 为 null 时不请求（表单不合法） */
export function useNotificationAudiencePreviewQuery(appKey: string | null | undefined, audience: NotificationAudience | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.preview, token, appKey, JSON.stringify(audience)],
    queryFn: () => previewNotificationAudience(token as string, appKey as string, audience as NotificationAudience),
    enabled: Boolean(token && appKey && audience),
    placeholderData: keepPreviousData,
    staleTime: 15_000,
    retry: false
  });
}

export function useNotificationCampaignsQuery(appKey: string | null | undefined, params: NotificationCampaignListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.list, token, appKey, params.status ?? "", params.page ?? 1, params.limit ?? 20],
    queryFn: () => listNotificationCampaigns(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    refetchInterval: (query) => ((query.state.data?.items ?? []).some(isCampaignActive) ? POLL_MS : false)
  });
}

export function useNotificationCampaignQuery(appKey: string | null | undefined, campaignId: number | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.detail, token, appKey, campaignId],
    queryFn: () => getNotificationCampaign(token as string, appKey as string, campaignId as number),
    enabled: Boolean(token && appKey && campaignId),
    refetchInterval: (query) => (isCampaignActive(query.state.data) ? POLL_MS : false)
  });
}

function useInvalidateCampaigns() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(
      [KEY.list, KEY.detail, "admin-app-notifications"].map((key) => queryClient.invalidateQueries({ queryKey: [key] }))
    );
  };
}

export function useCreateNotificationCampaignMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCampaigns();
  return useMutation({
    mutationFn: (payload: NotificationCampaignPayload) => createNotificationCampaign(token as string, appKey, payload),
    onSuccess: invalidate
  });
}

export function useCancelNotificationCampaignMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCampaigns();
  return useMutation({
    mutationFn: (campaignId: number) => cancelNotificationCampaign(token as string, appKey, campaignId),
    onSuccess: invalidate
  });
}

export function useRecallNotificationCampaignMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCampaigns();
  return useMutation({
    mutationFn: (campaignId: number) => recallNotificationCampaign(token as string, appKey, campaignId),
    onSuccess: invalidate
  });
}
