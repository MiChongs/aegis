"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createCosmeticItem,
  createWorkshopPart,
  deleteCosmeticAsset,
  deleteCosmeticItem,
  deleteWorkshopPart,
  duplicateCosmeticItem,
  getCosmeticCatalog,
  getCosmeticItem,
  getCosmeticSettings,
  getCosmeticStats,
  getUserCosmetics,
  grantUserCosmetic,
  listCosmeticAssets,
  listCosmeticItems,
  listCosmeticOwnerships,
  listCosmeticRevisions,
  listWorkshopItems,
  listWorkshopParts,
  publishCosmeticItem,
  reorderCosmeticItems,
  restoreCosmeticRevision,
  restoreWorkshopItem,
  revokeUserCosmetic,
  saveCosmeticDraft,
  saveCosmeticSettings,
  setCosmeticStatus,
  takedownWorkshopItem,
  unequipUserCosmetic,
  updateCosmeticItem,
  updateWorkshopPart,
  uploadCosmeticAsset,
  uploadCosmeticPreview,
  type CosmeticAssetFilter,
  type CosmeticItem,
  type CosmeticItemFilter,
  type CosmeticItemPayload,
  type CosmeticOwnershipFilter,
  type CosmeticSettingsPayload,
  type CosmeticSlot,
  type CosmeticStatus,
  type Design,
  type WorkshopItemFilter,
  type WorkshopPartInput
} from "@/lib/api/cosmetics";
import { useAdminToken } from "@/lib/admin-hooks";
import { rememberCosmeticNames } from "@/lib/cosmetic-names";

/**
 * 个性装扮域 hooks。
 *
 * 失效关系集中在这里：装扮的任何写操作（含草稿、发布、上下架）都影响列表、单件、统计与目录里的设置；
 * 素材的上传与删除影响素材库与统计（素材占用）；给用户发放 / 收回 / 摘下影响该用户的装扮页、持有记录、
 * 列表上的持有与佩戴人数与统计。整组失效，不在组件里逐个点名。
 * 工坊：零件的写操作影响零件库与素材库（被零件占用的素材不能删）；作品下架 / 恢复影响作品列表与用户的装扮页。
 */

const KEY = {
  catalog: "admin-cosmetic-catalog",
  settings: "admin-cosmetic-settings",
  items: "admin-cosmetic-items",
  item: "admin-cosmetic-item",
  revisions: "admin-cosmetic-revisions",
  assets: "admin-cosmetic-assets",
  ownerships: "admin-cosmetic-ownerships",
  stats: "admin-cosmetic-stats",
  user: "admin-cosmetic-user",
  workshopParts: "admin-cosmetic-workshop-parts",
  workshopItems: "admin-cosmetic-workshop-items"
} as const;

const SCOPE = Object.values(KEY);

function useInvalidateCosmetics() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 目录与设置 ──

export function useCosmeticCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getCosmeticCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 60_000
  });
}

export function useCosmeticSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getCosmeticSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveCosmeticSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (settings: CosmeticSettingsPayload) =>
      saveCosmeticSettings(token as string, appKey as string, settings),
    onSuccess: invalidate
  });
}

// ── 装扮 ──

export function useCosmeticItemsQuery(appKey?: string | null, filter: CosmeticItemFilter = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.items, token, appKey, filter.slot ?? "", filter.status ?? "", filter.rarity ?? "", filter.keyword ?? ""],
    queryFn: async () => {
      const items = await listCosmeticItems(token as string, appKey as string, filter);
      rememberCosmeticNames(items);
      return items;
    },
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

/** 单件（含草稿与已发布的设计稿）。卡片与选择器的实时预览也读它，所以缓存放得久一些。 */
export function useCosmeticItemQuery(appKey?: string | null, itemId?: number | null, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.item, token, appKey, itemId],
    queryFn: () => getCosmeticItem(token as string, appKey as string, itemId as number),
    enabled: Boolean(token && appKey && itemId && enabled),
    staleTime: 30_000
  });
}

export function useSaveCosmeticItemMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { itemId?: number; payload: CosmeticItemPayload }) =>
      input.itemId
        ? updateCosmeticItem(token as string, appKey as string, input.itemId, input.payload)
        : createCosmeticItem(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useDeleteCosmeticItemMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (itemId: number) => deleteCosmeticItem(token as string, appKey as string, itemId),
    onSuccess: invalidate
  });
}

export function useDuplicateCosmeticItemMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (itemId: number) => duplicateCosmeticItem(token as string, appKey as string, itemId),
    onSuccess: invalidate
  });
}

export function useSetCosmeticStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { itemId: number; status: CosmeticStatus }) =>
      setCosmeticStatus(token as string, appKey as string, input.itemId, input.status),
    onSuccess: invalidate
  });
}

export function useReorderCosmeticItemsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (itemIds: number[]) => reorderCosmeticItems(token as string, appKey as string, itemIds),
    onSettled: invalidate
  });
}

/**
 * 保存草稿。成功后只把结果写回单件缓存、不整组失效：设计器自动保存时每隔几秒一次，
 * 整组失效会把列表、统计一并重拉。列表上的「未发布修改」标记在关闭设计器时再刷新。
 */
export function useSaveCosmeticDraftMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { itemId: number; design: Design }) =>
      saveCosmeticDraft(token as string, appKey as string, input.itemId, input.design),
    onSuccess: (item: CosmeticItem) => {
      queryClient.setQueryData([KEY.item, token, appKey, item.id], (old: CosmeticItem | undefined) =>
        old ? { ...old, ...item, design: item.design ?? old.design, draft: item.draft ?? old.draft } : item
      );
    }
  });
}

export function usePublishCosmeticMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { itemId: number; note: string }) =>
      publishCosmeticItem(token as string, appKey as string, input.itemId, input.note),
    onSuccess: invalidate
  });
}

export function useUploadCosmeticPreviewMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (input: { itemId: number; file: Blob }) =>
      uploadCosmeticPreview(token as string, appKey as string, input.itemId, input.file)
  });
}

export function useCosmeticRevisionsQuery(appKey?: string | null, itemId?: number | null, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.revisions, token, appKey, itemId],
    queryFn: () => listCosmeticRevisions(token as string, appKey as string, itemId as number),
    enabled: Boolean(token && appKey && itemId && enabled)
  });
}

export function useRestoreCosmeticRevisionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { itemId: number; revision: number }) =>
      restoreCosmeticRevision(token as string, appKey as string, input.itemId, input.revision),
    onSuccess: invalidate
  });
}

/** 关闭设计器时调用：让列表上的「未发布修改」与预览图跟上 */
export function useRefreshCosmeticLists() {
  return useInvalidateCosmetics();
}

// ── 素材 ──

export function useCosmeticAssetsQuery(appKey?: string | null, filter: CosmeticAssetFilter = {}, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.assets, token, appKey, filter.kind ?? "", filter.keyword ?? "", filter.page ?? 1, filter.limit ?? 60],
    queryFn: () => listCosmeticAssets(token as string, appKey as string, filter),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: keepPreviousData
  });
}

export function useUploadCosmeticAssetMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { file: File; name?: string; kind: "image" | "sprite" }) =>
      uploadCosmeticAsset(token as string, appKey as string, input),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [KEY.assets] }),
        queryClient.invalidateQueries({ queryKey: [KEY.stats] })
      ]);
    }
  });
}

export function useDeleteCosmeticAssetMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (assetId: number) => deleteCosmeticAsset(token as string, appKey as string, assetId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [KEY.assets] }),
        queryClient.invalidateQueries({ queryKey: [KEY.stats] })
      ]);
    }
  });
}

// ── 持有记录与统计 ──

export function useCosmeticOwnershipsQuery(appKey?: string | null, filter: CosmeticOwnershipFilter = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.ownerships,
      token,
      appKey,
      filter.userId ?? 0,
      filter.cosmeticId ?? 0,
      filter.source ?? "",
      filter.state ?? "",
      filter.page ?? 1,
      filter.limit ?? 50
    ],
    queryFn: () => listCosmeticOwnerships(token as string, appKey as string, filter),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useCosmeticStatsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey],
    queryFn: () => getCosmeticStats(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

// ── 单个用户 ──

export function useUserCosmeticsQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.user, token, appKey, userId],
    queryFn: () => getUserCosmetics(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useGrantUserCosmeticMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { cosmeticId: number; days: number; equip?: boolean }) =>
      grantUserCosmetic(token as string, appKey as string, userId as number, input.cosmeticId, input.days, input.equip ?? false),
    onSuccess: invalidate
  });
}

export function useRevokeUserCosmeticMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { ownershipId: number; reason: string }) =>
      revokeUserCosmetic(token as string, appKey as string, userId as number, input.ownershipId, input.reason),
    onSuccess: invalidate
  });
}

export function useUnequipUserCosmeticMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCosmetics();
  return useMutation({
    mutationFn: (input: { slot: CosmeticSlot; position: number }) =>
      unequipUserCosmetic(token as string, appKey as string, userId as number, input.slot, input.position),
    onSuccess: invalidate
  });
}

// ── 自定义工坊 ──

export function useWorkshopPartsQuery(appKey?: string | null, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.workshopParts, token, appKey],
    queryFn: async () => (await listWorkshopParts(token as string, appKey as string)).items ?? [],
    enabled: Boolean(token && appKey && enabled)
  });
}

function useInvalidateWorkshopParts() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: [KEY.workshopParts] }),
      queryClient.invalidateQueries({ queryKey: [KEY.assets] })
    ]);
  };
}

export function useSaveWorkshopPartMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateWorkshopParts();
  return useMutation({
    mutationFn: (input: { partId?: number; payload: WorkshopPartInput }) =>
      input.partId
        ? updateWorkshopPart(token as string, appKey as string, input.partId, input.payload)
        : createWorkshopPart(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useDeleteWorkshopPartMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateWorkshopParts();
  return useMutation({
    mutationFn: (partId: number) => deleteWorkshopPart(token as string, appKey as string, partId),
    onSuccess: invalidate
  });
}

export function useWorkshopItemsQuery(appKey?: string | null, filter: WorkshopItemFilter = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.workshopItems,
      token,
      appKey,
      filter.userId ?? 0,
      filter.slot ?? "",
      filter.status ?? "",
      filter.keyword ?? "",
      filter.page ?? 1,
      filter.limit ?? 20
    ],
    queryFn: () => listWorkshopItems(token as string, appKey as string, filter),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

/** 下架（带原因）或恢复一件作品 */
export function useSetWorkshopItemStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { itemId: number; takedown: boolean; reason?: string }) =>
      input.takedown
        ? takedownWorkshopItem(token as string, appKey as string, input.itemId, input.reason ?? "")
        : restoreWorkshopItem(token as string, appKey as string, input.itemId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [KEY.workshopItems] }),
        queryClient.invalidateQueries({ queryKey: [KEY.user] })
      ]);
    }
  });
}
