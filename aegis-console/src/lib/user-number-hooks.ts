"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  adjustNumberRight,
  assignNumber,
  getNumberCatalog,
  getNumberOverview,
  getNumberSettings,
  getNumberUser,
  inspectNumber,
  listNumberDraws,
  listNumberHistory,
  listNumberHolds,
  listNumberLedger,
  previewNumberOdds,
  releaseNumberHold,
  reserveNumbers,
  rollbackNumber,
  saveNumberSettings,
  type NumberListParams,
  type NumberRightKind,
  type NumberSettings
} from "@/lib/api/user-number";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 用户号码 hooks（docs/user-number.md）。
 *
 * 任何写操作之后号码相关的查询一起失效：换号会同时改变总览的分布、记录列表、占用（旧号冻结）
 * 与那个用户的详情，逐个挑着失效迟早漏一个。用户列表与详情里的号码字段一并刷新。
 */

const KEY = {
  catalog: "admin-number-catalog",
  overview: "admin-number-overview",
  settings: "admin-number-settings",
  odds: "admin-number-odds",
  inspect: "admin-number-inspect",
  user: "admin-number-user",
  history: "admin-number-history",
  ledger: "admin-number-ledger",
  draws: "admin-number-draws",
  holds: "admin-number-holds"
} as const;

const ALL_KEYS = Object.values(KEY);

export function useNumberCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getNumberCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 60_000
  });
}

export function useNumberOverviewQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.overview, token, appKey],
    queryFn: () => getNumberOverview(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useNumberSettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getNumberSettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 试算：设置本身进查询键，调用方先防抖再传进来 */
export function useNumberOddsQuery(appKey: string | null | undefined, settings: NumberSettings | null, luck: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.odds, token, appKey, luck, JSON.stringify(settings)],
    queryFn: () => previewNumberOdds(token as string, appKey as string, settings as NumberSettings, luck),
    enabled: Boolean(token && appKey && settings),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
    retry: false
  });
}

export function useNumberInspectQuery(appKey: string | null | undefined, number: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.inspect, token, appKey, number],
    queryFn: () => inspectNumber(token as string, appKey as string, number),
    enabled: Boolean(token && appKey && /^[1-9]\d{4,11}$/.test(number)),
    retry: false
  });
}

export function useNumberUserQuery(appKey: string | null | undefined, userId: number | null | undefined) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.user, token, appKey, userId],
    queryFn: () => getNumberUser(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId),
    retry: false
  });
}

export function useNumberHistoryQuery(appKey: string | null | undefined, params: NumberListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.history, token, appKey, params.userId ?? 0, params.page ?? 1, params.limit ?? 20],
    queryFn: () => listNumberHistory(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useNumberLedgerQuery(appKey: string | null | undefined, params: NumberListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.ledger, token, appKey, params.userId ?? 0, params.kind ?? "", params.page ?? 1, params.limit ?? 20],
    queryFn: () => listNumberLedger(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useNumberDrawsQuery(appKey: string | null | undefined, params: NumberListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.draws, token, appKey, params.userId ?? 0, params.page ?? 1, params.limit ?? 20],
    queryFn: () => listNumberDraws(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useNumberHoldsQuery(appKey: string | null | undefined, params: NumberListParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.holds, token, appKey, params.kind ?? "", params.page ?? 1, params.limit ?? 20],
    queryFn: () => listNumberHolds(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

function useInvalidateNumbers() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(
      [...ALL_KEYS.filter((key) => key !== KEY.catalog), "admin-app-users", "admin-app-user"].map((key) =>
        queryClient.invalidateQueries({ queryKey: [key] })
      )
    );
  };
}

export function useSaveNumberSettingsMutation(appKey: string) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (settings: NumberSettings) => saveNumberSettings(token as string, appKey, settings),
    onSuccess: async () => {
      // 规律等级改了，目录里的 tier 与查号结果都跟着变
      await Promise.all(
        [KEY.settings, KEY.catalog, KEY.inspect, KEY.user].map((key) => queryClient.invalidateQueries({ queryKey: [key] }))
      );
    }
  });
}

export function useAssignNumberMutation(appKey: string, userId: number) {
  const token = useAdminToken();
  const invalidate = useInvalidateNumbers();
  return useMutation({
    mutationFn: (payload: { number: string; force: boolean; note?: string }) => assignNumber(token as string, appKey, userId, payload),
    onSuccess: invalidate
  });
}

export function useRollbackNumberMutation(appKey: string, userId: number) {
  const token = useAdminToken();
  const invalidate = useInvalidateNumbers();
  return useMutation({
    mutationFn: (historyId: number) => rollbackNumber(token as string, appKey, userId, historyId),
    onSuccess: invalidate
  });
}

export function useAdjustNumberRightMutation(appKey: string, userId: number) {
  const token = useAdminToken();
  const invalidate = useInvalidateNumbers();
  return useMutation({
    mutationFn: (payload: { kind: NumberRightKind; level: number; delta: number; note?: string }) =>
      adjustNumberRight(token as string, appKey, userId, payload),
    onSuccess: invalidate
  });
}

export function useReserveNumbersMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateNumbers();
  return useMutation({
    mutationFn: (payload: { numbers: string[]; note?: string; expiresAt?: string | null }) => reserveNumbers(token as string, appKey, payload),
    onSuccess: invalidate
  });
}

export function useReleaseNumberHoldMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateNumbers();
  return useMutation({
    mutationFn: (number: number | string) => releaseNumberHold(token as string, appKey, number),
    onSuccess: invalidate
  });
}
