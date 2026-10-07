"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  addUsersToVersionChannel,
  createAdminRelease,
  createAdminVersionChannel,
  deleteAdminRelease,
  deleteAdminVersionChannel,
  getAdminRelease,
  getAdminReleaseOverview,
  getAdminReleaseStats,
  getAdminReleases,
  getAdminVersionChannelUsers,
  getAdminVersionChannels,
  publishAdminRelease,
  removeUsersFromVersionChannel,
  setAdminReleaseRollout,
  simulateAdminRelease,
  transitionAdminRelease,
  updateAdminRelease,
  updateAdminVersionChannel,
  uploadAdminReleaseAsset,
  type ReleaseListParams,
  type ReleaseTransition
} from "@/lib/api/releases";
import type { ReleaseSavePayload, ReleaseSimulateInput } from "@/lib/api/types";

/**
 * 发布中心的 React Query hooks。
 *
 * 一次版本写操作会同时影响「版本列表」「总览」「该版本详情」三处，失效关系集中在这里，
 * 散在组件里必然漏掉总览那张 —— 表现为发布之后统计卡还停在旧数字。
 */

const RELEASES_KEY = "releases";
const OVERVIEW_KEY = "release-overview";
const DETAIL_KEY = "release-detail";
const STATS_KEY = "release-stats";
const CHANNELS_KEY = "release-channels";
const CHANNEL_USERS_KEY = "release-channel-users";

function useReleaseInvalidator() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(
      [RELEASES_KEY, OVERVIEW_KEY, DETAIL_KEY].map((key) => queryClient.invalidateQueries({ queryKey: [key] }))
    );
  };
}

export function useReleasesQuery(appKey?: string | null, params: ReleaseListParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [RELEASES_KEY, token, appKey, params],
    queryFn: () => getAdminReleases(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useReleaseOverviewQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [OVERVIEW_KEY, token, appKey],
    queryFn: () => getAdminReleaseOverview(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useReleaseDetailQuery(appKey?: string | null, releaseId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [DETAIL_KEY, token, appKey, releaseId],
    queryFn: () => getAdminRelease(token as string, appKey as string, releaseId as number),
    enabled: Boolean(token && appKey && releaseId)
  });
}

export function useReleaseStatsQuery(appKey?: string | null, releaseId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [STATS_KEY, token, appKey, releaseId],
    queryFn: () => getAdminReleaseStats(token as string, appKey as string, releaseId as number),
    enabled: Boolean(token && appKey && releaseId)
  });
}

export function useSaveReleaseMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useReleaseInvalidator();
  return useMutation({
    mutationFn: (input: { releaseId?: number | null; payload: ReleaseSavePayload }) =>
      input.releaseId
        ? updateAdminRelease(token as string, appKey as string, input.releaseId, input.payload)
        : createAdminRelease(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function usePublishReleaseMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useReleaseInvalidator();
  return useMutation({
    mutationFn: (input: { releaseId: number; publishAt?: string | null }) =>
      publishAdminRelease(token as string, appKey as string, input.releaseId, input.publishAt),
    onSuccess: invalidate
  });
}

export function useTransitionReleaseMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useReleaseInvalidator();
  return useMutation({
    mutationFn: (input: { releaseId: number; action: ReleaseTransition }) =>
      transitionAdminRelease(token as string, appKey as string, input.releaseId, input.action),
    onSuccess: invalidate
  });
}

export function useReleaseRolloutMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useReleaseInvalidator();
  return useMutation({
    mutationFn: (input: { releaseId: number; rolloutPct: number }) =>
      setAdminReleaseRollout(token as string, appKey as string, input.releaseId, input.rolloutPct),
    onSuccess: invalidate
  });
}

export function useDeleteReleaseMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useReleaseInvalidator();
  return useMutation({
    mutationFn: (releaseId: number) => deleteAdminRelease(token as string, appKey as string, releaseId),
    onSuccess: invalidate
  });
}

export function useUploadReleaseAssetMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (file: File) => uploadAdminReleaseAsset(token as string, appKey as string, file)
  });
}

export function useSimulateReleaseMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: ReleaseSimulateInput) => simulateAdminRelease(token as string, appKey as string, input)
  });
}

/* ───────────────────────── 渠道 ───────────────────────── */

export function useReleaseChannelsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [CHANNELS_KEY, token, appKey],
    queryFn: () => getAdminVersionChannels(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveReleaseChannelMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { channelId?: number | null; payload: Record<string, unknown> }) =>
      input.channelId
        ? updateAdminVersionChannel(token as string, appKey as string, input.channelId, input.payload)
        : createAdminVersionChannel(token as string, appKey as string, input.payload),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [CHANNELS_KEY] }),
        queryClient.invalidateQueries({ queryKey: [RELEASES_KEY] })
      ]);
    }
  });
}

export function useDeleteReleaseChannelMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (channelId: number) => deleteAdminVersionChannel(token as string, appKey as string, channelId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [CHANNELS_KEY] }),
        queryClient.invalidateQueries({ queryKey: [RELEASES_KEY] }),
        queryClient.invalidateQueries({ queryKey: [OVERVIEW_KEY] })
      ]);
    }
  });
}

export function useReleaseChannelUsersQuery(appKey?: string | null, channelId?: number | null, page = 1) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [CHANNEL_USERS_KEY, token, appKey, channelId, page],
    queryFn: () => getAdminVersionChannelUsers(token as string, appKey as string, channelId as number, { page, limit: 20 }),
    enabled: Boolean(token && appKey && channelId)
  });
}

export function useReleaseChannelMembersMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { channelId: number; userIds: number[]; remove?: boolean }): Promise<unknown> =>
      input.remove
        ? removeUsersFromVersionChannel(token as string, appKey as string, input.channelId, input.userIds)
        : addUsersToVersionChannel(token as string, appKey as string, input.channelId, input.userIds),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [CHANNEL_USERS_KEY] }),
        queryClient.invalidateQueries({ queryKey: [CHANNELS_KEY] })
      ]);
    }
  });
}
