"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteCloudStorageUserItem,
  getCloudStorageConfig,
  getCloudStorageStats,
  getCloudStorageUser,
  getCloudStorageUserItem,
  linkCloudStorageUserItem,
  listCloudStorageUserItemRevisions,
  listCloudStorageUserItems,
  listCloudStorageUsers,
  purgeCloudStorageUser,
  purgeExpiredCloudTrash,
  restoreCloudStorageUserItem,
  rollbackCloudStorageUserItem,
  saveCloudStorageConfig,
  saveCloudStorageUser,
  type CloudItemParams,
  type CloudUserParams,
  type SaveCloudStorageConfigPayload,
  type SaveCloudUserPayload
} from "@/lib/api/cloud-storage";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 用户云存储域 hooks。
 *
 * 失效关系集中在这里：任何一次写（改配置、改某人的配额、回滚、删除）都会同时改动
 * 概览的总量、用户列表的用量排序和那个人的账目 —— 所以整组一起失效，
 * 而不是逐个判断「这次写会影响哪几块」。
 */

const KEY = {
  config: "admin-cloud-storage-config",
  stats: "admin-cloud-storage-stats",
  users: "admin-cloud-storage-users",
  user: "admin-cloud-storage-user",
  items: "admin-cloud-storage-items",
  item: "admin-cloud-storage-item",
  revisions: "admin-cloud-storage-revisions"
} as const;

const CLOUD_SCOPE = Object.values(KEY);

function useInvalidateCloudScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(CLOUD_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 应用级 ──

export function useCloudStorageConfigQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.config, token, appKey],
    queryFn: () => getCloudStorageConfig(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveCloudStorageConfigMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCloudScope();
  return useMutation({
    mutationFn: (payload: SaveCloudStorageConfigPayload) =>
      saveCloudStorageConfig(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useCloudStorageStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getCloudStorageStats(token as string, appKey as string, days),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useCloudStorageUsersQuery(appKey?: string | null, params?: CloudUserParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.users,
      token,
      appKey,
      params?.keyword ?? "",
      params?.sort ?? "usage",
      params?.frozen ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCloudStorageUsers(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function usePurgeExpiredCloudTrashMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCloudScope();
  return useMutation({
    mutationFn: () => purgeExpiredCloudTrash(token as string, appKey as string),
    onSuccess: invalidate
  });
}

// ── 单个用户 ──

export function useCloudStorageUserQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.user, token, appKey, userId],
    queryFn: () => getCloudStorageUser(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useSaveCloudStorageUserMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCloudScope();
  return useMutation({
    mutationFn: (payload: SaveCloudUserPayload) =>
      saveCloudStorageUser(token as string, appKey as string, userId as number, payload),
    onSuccess: invalidate
  });
}

export function usePurgeCloudStorageUserMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCloudScope();
  return useMutation({
    mutationFn: () => purgeCloudStorageUser(token as string, appKey as string, userId as number),
    onSuccess: invalidate
  });
}

export function useCloudStorageUserItemsQuery(
  appKey?: string | null,
  userId?: number | null,
  params?: CloudItemParams
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.items,
      token,
      appKey,
      userId,
      params?.namespace ?? "",
      params?.keyword ?? "",
      params?.status ?? "active",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCloudStorageUserItems(token as string, appKey as string, userId as number, params),
    enabled: Boolean(token && appKey && userId),
    placeholderData: (previous) => previous
  });
}

export function useCloudStorageUserItemQuery(
  appKey?: string | null,
  userId?: number | null,
  itemId?: number | null,
  revision?: number
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.item, token, appKey, userId, itemId, revision ?? 0],
    queryFn: () =>
      getCloudStorageUserItem(token as string, appKey as string, userId as number, itemId as number, revision),
    enabled: Boolean(token && appKey && userId && itemId)
  });
}

export function useCloudStorageUserItemRevisionsQuery(
  appKey?: string | null,
  userId?: number | null,
  itemId?: number | null
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.revisions, token, appKey, userId, itemId],
    queryFn: () =>
      listCloudStorageUserItemRevisions(token as string, appKey as string, userId as number, itemId as number),
    enabled: Boolean(token && appKey && userId && itemId)
  });
}

export function useCloudStorageItemActions(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCloudScope();
  const rollback = useMutation({
    mutationFn: ({ itemId, revision }: { itemId: number; revision: number }) =>
      rollbackCloudStorageUserItem(token as string, appKey as string, userId as number, itemId, revision),
    onSuccess: invalidate
  });
  const restore = useMutation({
    mutationFn: (itemId: number) =>
      restoreCloudStorageUserItem(token as string, appKey as string, userId as number, itemId),
    onSuccess: invalidate
  });
  const remove = useMutation({
    mutationFn: ({ itemId, permanent }: { itemId: number; permanent: boolean }) =>
      deleteCloudStorageUserItem(token as string, appKey as string, userId as number, itemId, permanent),
    onSuccess: invalidate
  });
  const link = useMutation({
    mutationFn: ({ itemId, revision }: { itemId: number; revision?: number }) =>
      linkCloudStorageUserItem(token as string, appKey as string, userId as number, itemId, revision)
  });
  return { rollback, restore, remove, link };
}
