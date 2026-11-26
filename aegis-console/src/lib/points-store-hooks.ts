"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createPointsStoreItem,
  deletePointsStoreItem,
  getPointsStoreCatalog,
  getPointsStoreSettings,
  getPointsStoreStats,
  getPointsStoreGift,
  getPointsStoreGiftPolicy,
  handlePointsStoreGift,
  listPointsStoreGifts,
  savePointsStoreGiftPolicy,
  type PointsStoreGiftParams,
  type PointsStoreGiftRule,
  getUserFeatureUnlocks,
  grantUserFeatureUnlock,
  handlePointsStoreRedemption,
  listPointsStoreItems,
  listPointsStoreRedemptions,
  reorderPointsStoreItems,
  revokeUserFeatureUnlock,
  savePointsStoreSettings,
  setPointsStoreItemStatus,
  updatePointsStoreItem,
  type PointsStoreItemPayload,
  type PointsStoreRedemptionParams
} from "@/lib/api/points-store";
import { useAdminToken } from "@/lib/admin-hooks";
import { browserTimeZone } from "@/lib/datetime";

/**
 * 积分商城域 hooks。
 *
 * 失效关系集中在这里：改商品影响商品列表、目录里的分类与概览的上架数；处理一笔兑换
 * 影响兑换记录、商品上的待发放数与库存、概览。整组失效，不在组件里逐个点名。
 * 用户的功能解锁另外牵动会员权益（`features` 里多了或少了一项），一并失效会员缓存。
 */

const KEY = {
  catalog: "admin-points-store-catalog",
  settings: "admin-points-store-settings",
  items: "admin-points-store-items",
  redemptions: "admin-points-store-redemptions",
  stats: "admin-points-store-stats",
  unlocks: "admin-points-store-unlocks",
  gifts: "admin-points-store-gifts",
  giftPolicy: "admin-points-store-gift-policy"
} as const;

const POINTS_STORE_SCOPE = Object.values(KEY);

function useInvalidatePointsStoreScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(POINTS_STORE_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 目录与开关 ──

export function usePointsStoreCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getPointsStoreCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function usePointsStoreSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getPointsStoreSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSavePointsStoreSettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePointsStoreScope();
  return useMutation({
    mutationFn: (enabled: boolean) => savePointsStoreSettings(token as string, appKey as string, enabled),
    onSuccess: invalidate
  });
}

// ── 商品 ──

export function usePointsStoreItemsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.items, token, appKey],
    queryFn: () => listPointsStoreItems(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSavePointsStoreItemMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePointsStoreScope();
  return useMutation({
    mutationFn: (input: { itemId?: number; payload: PointsStoreItemPayload }) =>
      input.itemId
        ? updatePointsStoreItem(token as string, appKey as string, input.itemId, input.payload)
        : createPointsStoreItem(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useSetPointsStoreItemStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePointsStoreScope();
  return useMutation({
    mutationFn: (input: { itemId: number; active: boolean }) =>
      setPointsStoreItemStatus(token as string, appKey as string, input.itemId, input.active),
    onSuccess: invalidate
  });
}

export function useReorderPointsStoreItemsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePointsStoreScope();
  return useMutation({
    mutationFn: (itemIds: number[]) => reorderPointsStoreItems(token as string, appKey as string, itemIds),
    onSuccess: invalidate
  });
}

export function useDeletePointsStoreItemMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePointsStoreScope();
  return useMutation({
    mutationFn: (itemId: number) => deletePointsStoreItem(token as string, appKey as string, itemId),
    onSuccess: invalidate
  });
}

// ── 兑换记录 ──

export function usePointsStoreRedemptionsQuery(appKey?: string | null, params?: PointsStoreRedemptionParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.redemptions,
      token,
      appKey,
      params?.userId ?? 0,
      params?.itemId ?? 0,
      params?.status ?? "",
      params?.keyword ?? "",
      params?.from ?? "",
      params?.to ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listPointsStoreRedemptions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

/** 当前筛选条件下各状态的笔数（每个状态取一条，只读 total），供状态页签上的计数 */
export function usePointsStoreRedemptionCountsQuery(
  appKey?: string | null,
  filters?: Omit<PointsStoreRedemptionParams, "status" | "page" | "limit">
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.redemptions,
      "counts",
      token,
      appKey,
      filters?.userId ?? 0,
      filters?.itemId ?? 0,
      filters?.keyword ?? "",
      filters?.from ?? "",
      filters?.to ?? ""
    ],
    enabled: Boolean(token && appKey),
    queryFn: async () => {
      const count = (status?: PointsStoreRedemptionParams["status"]) =>
        listPointsStoreRedemptions(token as string, appKey as string, { ...filters, status, page: 1, limit: 1 }).then(
          (page) => page.total
        );
      const [all, completed, pending, cancelled, revoked] = await Promise.all([
        count(),
        count("completed"),
        count("pending"),
        count("cancelled"),
        count("revoked")
      ]);
      return { all, completed, pending, cancelled, revoked };
    }
  });
}

export function useHandlePointsStoreRedemptionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidatePointsStoreScope();
  return useMutation({
    mutationFn: (input: { redemptionId: number; action: "fulfill" | "cancel"; note: string }) =>
      handlePointsStoreRedemption(token as string, appKey as string, input.redemptionId, input.action, input.note),
    onSuccess: invalidate
  });
}

export function usePointsStoreStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getPointsStoreStats(token as string, appKey as string, days, browserTimeZone()),
    enabled: Boolean(token && appKey)
  });
}

// ── 用户的功能解锁 ──

export function useUserFeatureUnlocksQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.unlocks, token, appKey, userId],
    queryFn: () => getUserFeatureUnlocks(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId && userId > 0)
  });
}

/** 授予 / 收回都会改变这个人的 features：会员权益的缓存一并失效（键名以 admin-vip 开头的那一组）。 */
function useInvalidateUnlockScope() {
  const queryClient = useQueryClient();
  return async () => {
    await queryClient.invalidateQueries({ queryKey: [KEY.unlocks] });
    await queryClient.invalidateQueries({
      predicate: (query) => String(query.queryKey[0] ?? "").includes("vip")
    });
  };
}

export function useGrantUserFeatureUnlockMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateUnlockScope();
  return useMutation({
    mutationFn: (input: { tag: string; days: number }) =>
      grantUserFeatureUnlock(token as string, appKey as string, userId as number, input.tag, input.days),
    onSuccess: invalidate
  });
}

export function useRevokeUserFeatureUnlockMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateUnlockScope();
  return useMutation({
    mutationFn: (input: { unlockId: number; reason: string }) =>
      revokeUserFeatureUnlock(token as string, appKey as string, userId as number, input.unlockId, input.reason),
    onSuccess: invalidate
  });
}

// ── 赠好友 ──
// 审核、撤销会动积分、库存、兑换记录与接收人的会员：积分商城整组失效，会员缓存一并失效。

function useInvalidateGiftScope() {
  const invalidateStore = useInvalidatePointsStoreScope();
  const queryClient = useQueryClient();
  return async () => {
    await invalidateStore();
    await queryClient.invalidateQueries({
      predicate: (query) => String(query.queryKey[0] ?? "").includes("vip")
    });
  };
}

export function usePointsStoreGiftsQuery(appKey?: string | null, params?: PointsStoreGiftParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.gifts,
      token,
      appKey,
      params?.status ?? "",
      params?.userId ?? 0,
      params?.itemId ?? 0,
      params?.keyword ?? "",
      params?.from ?? "",
      params?.to ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listPointsStoreGifts(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function usePointsStoreGiftQuery(appKey?: string | null, giftId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.gifts, "detail", token, appKey, giftId],
    queryFn: () => getPointsStoreGift(token as string, appKey as string, giftId as number),
    enabled: Boolean(token && appKey && giftId)
  });
}

export function useHandlePointsStoreGiftMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateGiftScope();
  return useMutation({
    mutationFn: (input: { giftId: number; action: "approve" | "reject" | "revoke"; note: string; refundSender?: boolean }) =>
      handlePointsStoreGift(token as string, appKey as string, input.giftId, input.action, input.note, input.refundSender),
    onSuccess: invalidate
  });
}

export function usePointsStoreGiftPolicyQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.giftPolicy, token, appKey],
    queryFn: () => getPointsStoreGiftPolicy(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSavePointsStoreGiftPolicyMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (policy: { enabled: boolean; rules: PointsStoreGiftRule[] }) =>
      savePointsStoreGiftPolicy(token as string, appKey as string, policy),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: [KEY.giftPolicy] });
    }
  });
}
