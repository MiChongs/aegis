"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteCardKeyBatch,
  disableCardKeys,
  downloadCardKeyBatchCsv,
  generateCardKeys,
  getCardKeyCatalog,
  listCardKeyBatches,
  listCardKeyDevices,
  listCardKeyRedemptions,
  listCardKeys,
  restoreCardKeys,
  setCardKeyBatchStatus,
  unbindCardKeyDevice,
  type CardKeyListParams,
  type GenerateCardKeysPayload
} from "@/lib/api/card-key";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 卡密域 hooks。
 *
 * **失效关系集中在这里**，不散在组件里：生成一批卡会同时影响批次列表（多一批）、
 * 单卡列表（多一堆卡）；批量作废会同时影响单卡列表与批次上的核销进度。
 * 散着写必然漏掉其中几张，表现是「操作成功了但列表没变」，而那看起来像操作失败。
 */

const KEY = {
  catalog: "admin-card-key-catalog",
  batches: "admin-card-key-batches",
  codes: "admin-card-keys",
  devices: "admin-card-key-devices",
  redemptions: "admin-card-key-redemptions"
} as const;

/** 卡密域的全部缓存键，任一写操作后成组失效 */
const CARD_KEY_SCOPE = Object.values(KEY);

function useInvalidateCardKeyScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(CARD_KEY_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 目录 ──

/**
 * 权益目录。
 *
 * `staleTime` 拉长到一小时：它只随后端发版变化，而权益编辑抽屉每次打开都要读它。
 */
export function useCardKeyCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getCardKeyCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 60 * 60 * 1000
  });
}

// ── 批次 ──

export function useCardKeyBatchesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.batches, token, appKey],
    queryFn: () => listCardKeyBatches(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useGenerateCardKeysMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (payload: GenerateCardKeysPayload) =>
      generateCardKeys(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useSetCardKeyBatchStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (input: { batchId: number; enabled: boolean }) =>
      setCardKeyBatchStatus(token as string, appKey as string, input.batchId, input.enabled),
    onSuccess: invalidate
  });
}

export function useDeleteCardKeyBatchMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (batchId: number) => deleteCardKeyBatch(token as string, appKey as string, batchId),
    onSuccess: invalidate
  });
}

/** 导出不失效任何缓存 —— 它只是把已有数据取一份下来。 */
export function useExportCardKeyBatchMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (batchId: number) => downloadCardKeyBatchCsv(token as string, appKey as string, batchId)
  });
}

// ── 单卡 ──

export function useCardKeysQuery(appKey?: string | null, params?: CardKeyListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.codes,
      token,
      appKey,
      params?.batchId ?? 0,
      params?.status ?? "",
      params?.kind ?? "",
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCardKeys(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useDisableCardKeysMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (input: { ids: number[]; reason?: string }) =>
      disableCardKeys(token as string, appKey as string, input.ids, input.reason),
    onSuccess: invalidate
  });
}

export function useRestoreCardKeysMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (ids: number[]) => restoreCardKeys(token as string, appKey as string, ids),
    onSuccess: invalidate
  });
}

export function useCardKeyDevicesQuery(appKey?: string | null, cardId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.devices, token, appKey, cardId ?? 0],
    queryFn: () => listCardKeyDevices(token as string, appKey as string, cardId as number),
    enabled: Boolean(token && appKey && cardId)
  });
}

export function useUnbindCardKeyDeviceMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateCardKeyScope();
  return useMutation({
    mutationFn: (input: { cardId: number; deviceId: string }) =>
      unbindCardKeyDevice(token as string, appKey as string, input.cardId, input.deviceId),
    onSuccess: invalidate
  });
}

// ── 核销记录 ──

export function useCardKeyRedemptionsQuery(
  appKey?: string | null,
  params?: { batchId?: number; keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.redemptions,
      token,
      appKey,
      params?.batchId ?? 0,
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listCardKeyRedemptions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}
