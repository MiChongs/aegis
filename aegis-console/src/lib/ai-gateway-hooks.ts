"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  adjustAIGatewayUserCredit,
  createAIGatewayModel,
  deleteAIGatewayModel,
  getAIGatewayCatalog,
  getAIGatewayRequest,
  getAIGatewayRequestSummary,
  getAIGatewaySettings,
  getAIGatewayStats,
  getAIGatewayUserAccount,
  getModelCatalog,
  matchModelCatalog,
  refreshModelCatalog,
  searchModelCatalog,
  syncAIGatewayModels,
  type ModelCatalogSyncPayload,
  listAIGatewayKeys,
  listAIGatewayModels,
  listAIGatewayRequests,
  listAIGatewayUserCredits,
  refundAIGatewayRequest,
  saveAIGatewaySettings,
  setAIGatewayKeyStatus,
  streamAIGatewayLive,
  updateAIGatewayModel,
  createAIGatewayPackage,
  deleteAIGatewayPackage,
  getAIGatewayCommerce,
  grantAIGatewayLot,
  listAIGatewayLots,
  listAIGatewayPackageOrders,
  listAIGatewayPackages,
  previewAIGatewayPricing,
  refundAIGatewayPackageOrder,
  revokeAIGatewayLot,
  updateAIGatewayPackage,
  type AIGatewayLotGrantPayload,
  type AIGatewayLotParams,
  type AIGatewayPackageOrderParams,
  type AIGatewayPackagePayload,
  type AIGatewayPricingPreviewPayload,
  type AIGatewayLiveEvent,
  type AIGatewayPage,
  type AIGatewayRequest,
  type AIGatewayCreditAdjustPayload,
  type AIGatewayKeyParams,
  type AIGatewayModelPayload,
  type AIGatewayRequestParams,
  type AIGatewaySettings
} from "@/lib/api/ai-gateway";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * AI 中转网关域 hooks。
 *
 * 失效关系：改设置会改动用户的额度与倍率（账户视图），改模型会改动统计里的模型名，
 * 退款与额度调整会改动统计、记录与账户 —— 整组一起失效，与云存储区块同一个取舍。
 */

const KEY = {
  catalog: "admin-ai-gateway-catalog",
  settings: "admin-ai-gateway-settings",
  models: "admin-ai-gateway-models",
  requests: "admin-ai-gateway-requests",
  request: "admin-ai-gateway-request",
  summary: "admin-ai-gateway-summary",
  keys: "admin-ai-gateway-keys",
  stats: "admin-ai-gateway-stats",
  account: "admin-ai-gateway-account",
  credits: "admin-ai-gateway-credits",
  modelCatalog: "admin-ai-model-catalog",
  catalogSearch: "admin-ai-model-catalog-search",
  pricingPreview: "admin-ai-gateway-pricing-preview",
  packages: "admin-ai-gateway-packages",
  packageOrders: "admin-ai-gateway-package-orders",
  lots: "admin-ai-gateway-lots",
  commerce: "admin-ai-gateway-commerce"
} as const;

function useInvalidateScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(
      Object.values(KEY)
        .filter((key) => key !== KEY.catalog && key !== KEY.modelCatalog && key !== KEY.catalogSearch)
        .map((key) => queryClient.invalidateQueries({ queryKey: [key] }))
    );
  };
}

export function useAIGatewayCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getAIGatewayCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: Infinity
  });
}

/** 公开模型目录的概况（全平台一份，任一应用的入口都能看）。 */
export function useModelCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.modelCatalog, token, appKey],
    queryFn: () => getModelCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useRefreshModelCatalogMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => refreshModelCatalog(token as string, appKey as string),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: [KEY.modelCatalog] });
      await queryClient.invalidateQueries({ queryKey: [KEY.catalogSearch] });
    }
  });
}

export function useModelCatalogSearchQuery(appKey: string | null | undefined, query: string, kind?: string) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalogSearch, token, appKey, query, kind],
    queryFn: () => searchModelCatalog(token as string, appKey as string, { q: query, kind, limit: 30 }),
    enabled: Boolean(token && appKey && query.trim().length >= 2),
    staleTime: 60_000
  });
}

/** 按模型名匹配目录条目：侧栏里点「从目录填充」时调一次。 */
export function useMatchModelCatalogMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (params: { model: string; configId?: number }) => matchModelCatalog(token as string, appKey as string, params)
  });
}

/** 从目录同步；preview 时不写库，也就不失效列表。 */
export function useSyncAIGatewayModelsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: (payload: ModelCatalogSyncPayload) => syncAIGatewayModels(token as string, appKey as string, payload),
    onSuccess: async (_, payload) => {
      if (!payload.preview) await invalidate();
    }
  });
}

export function useAIGatewaySettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getAIGatewaySettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAIGatewaySettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: (payload: AIGatewaySettings) => saveAIGatewaySettings(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useAIGatewayModelsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.models, token, appKey],
    queryFn: () => listAIGatewayModels(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAIGatewayModelMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ id, payload }: { id?: number; payload: AIGatewayModelPayload }) =>
      id
        ? updateAIGatewayModel(token as string, appKey as string, id, payload)
        : createAIGatewayModel(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAIGatewayModelMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: (modelId: number) => deleteAIGatewayModel(token as string, appKey as string, modelId),
    onSuccess: invalidate
  });
}

/** refetchInterval：实时连接断开时调用记录页退回定时刷新。 */
export function useAIGatewayRequestsQuery(
  appKey: string | null | undefined,
  params: AIGatewayRequestParams,
  refetchInterval: number | false = false
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.requests, token, appKey, params],
    queryFn: () => listAIGatewayRequests(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous,
    refetchInterval
  });
}

export function useAIGatewayRequestQuery(appKey?: string | null, requestNo?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.request, token, appKey, requestNo],
    queryFn: () => getAIGatewayRequest(token as string, appKey as string, requestNo as string),
    enabled: Boolean(token && appKey && requestNo)
  });
}

export function useAIGatewayRequestSummaryQuery(appKey: string | null | undefined, minutes: number, refetchInterval: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.summary, token, appKey, minutes],
    queryFn: () => getAIGatewayRequestSummary(token as string, appKey as string, minutes),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous,
    refetchInterval
  });
}

// ── 实时 ──

export type AIGatewayLiveStatus = "connecting" | "live" | "offline" | "paused" | "unauthorized";

/** 新进来的调用在列表上高亮多久。 */
const FRESH_MS = 2500;
const SUMMARY_DEBOUNCE_MS = 1500;

/** 一条刚暂扣的调用是否该出现在这一页列表里（只插第一页，且要满足筛选）。 */
function belongsTo(params: AIGatewayRequestParams, record: AIGatewayRequest) {
  if ((params.page ?? 1) !== 1 || params.end) return false;
  if (params.userId && record.userId !== params.userId) return false;
  if (params.keyId && record.keyId !== params.keyId) return false;
  if (params.model && record.model !== params.model) return false;
  if (params.endpoint && record.endpoint !== params.endpoint) return false;
  if (params.status && params.status !== record.status) return false;
  if (params.configId && record.configId !== params.configId) return false;
  if (params.keyword && !record.requestNo.startsWith(params.keyword)) return false;
  return true;
}

type RequestsKey = [string, string | null, string, AIGatewayRequestParams];

function patchLists(
  queryClient: QueryClient,
  token: string,
  appKey: string,
  update: (page: AIGatewayPage<AIGatewayRequest>, params: AIGatewayRequestParams) => AIGatewayPage<AIGatewayRequest>
) {
  for (const [key, page] of queryClient.getQueriesData<AIGatewayPage<AIGatewayRequest>>({
    queryKey: [KEY.requests, token, appKey]
  })) {
    if (!page) continue;
    const next = update(page, (key as RequestsKey)[3] ?? {});
    if (next !== page) queryClient.setQueryData(key, next);
  }
}

function patchRow(
  page: AIGatewayPage<AIGatewayRequest>,
  requestNo: string,
  update: (record: AIGatewayRequest) => AIGatewayRequest
) {
  const index = page.items.findIndex((item) => item.requestNo === requestNo);
  if (index < 0) return page;
  const items = page.items.slice();
  items[index] = update(items[index]);
  return { ...page, items };
}

/**
 * 调用记录的实时事件：连上 SSE，把事件直接写进 react-query 的缓存 ——
 * 新调用插到第一页顶上，进行中的调用随上游尝试与流式进度更新，结束后换成终态并重取详情。
 * 断线按 1s、2s、4s… 退避重连（最长 30s），重连成功后整体刷新一次补上断线期间的变化。
 */
export function useAIGatewayLiveFeed(appKey: string | null | undefined, paused: boolean) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<AIGatewayLiveStatus>("connecting");
  const [fresh, setFresh] = useState<ReadonlySet<string>>(() => new Set());
  const freshTimers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const summaryTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!token || !appKey || paused) return;
    const controller = new AbortController();
    const timers = freshTimers.current;

    const refreshSummary = () => {
      if (summaryTimer.current) return;
      summaryTimer.current = setTimeout(() => {
        summaryTimer.current = null;
        void queryClient.invalidateQueries({ queryKey: [KEY.summary, token, appKey] });
      }, SUMMARY_DEBOUNCE_MS);
    };
    const markFresh = (requestNo: string) => {
      setFresh((previous) => new Set(previous).add(requestNo));
      clearTimeout(timers.get(requestNo));
      timers.set(
        requestNo,
        setTimeout(() => {
          timers.delete(requestNo);
          setFresh((previous) => {
            const next = new Set(previous);
            next.delete(requestNo);
            return next;
          });
        }, FRESH_MS)
      );
    };

    const apply = (event: AIGatewayLiveEvent) => {
      const requestNo = event.requestNo ?? event.request?.requestNo;
      switch (event.type) {
        case "resync":
          void queryClient.invalidateQueries({ queryKey: [KEY.requests, token, appKey] });
          refreshSummary();
          return;
        case "created": {
          const record = event.request;
          if (!record) return;
          patchLists(queryClient, token, appKey, (page, params) => {
            if (!belongsTo(params, record) || page.items.some((item) => item.requestNo === record.requestNo)) return page;
            const limit = params.limit ?? page.limit ?? 20;
            return { ...page, items: [record, ...page.items].slice(0, limit), total: page.total + 1 };
          });
          markFresh(record.requestNo);
          refreshSummary();
          return;
        }
        case "attempt":
        case "progress": {
          if (!requestNo) return;
          const attempts = event.attempts;
          const last = attempts?.[attempts.length - 1];
          patchLists(queryClient, token, appKey, (page) =>
            patchRow(page, requestNo, (record) =>
              record.status !== "reserved"
                ? record
                : {
                    ...record,
                    live: event.progress ?? record.live,
                    attempts: attempts?.length ?? record.attempts,
                    configName: last?.configName ?? record.configName,
                    provider: last?.provider ?? record.provider
                  }
            )
          );
          queryClient.setQueryData<AIGatewayRequest>([KEY.request, token, appKey, requestNo], (record) =>
            record && record.status === "reserved"
              ? {
                  ...record,
                  live: event.progress ?? record.live,
                  trace: attempts
                    ? { attempts, request: record.trace?.request ?? {}, response: record.trace?.response ?? {} }
                    : record.trace
                }
              : record
          );
          return;
        }
        case "finished": {
          const record = event.request;
          if (!record || !requestNo) return;
          patchLists(queryClient, token, appKey, (page) =>
            patchRow(page, requestNo, (previous) => ({ ...previous, ...record, account: record.account || previous.account, live: undefined }))
          );
          void queryClient.invalidateQueries({ queryKey: [KEY.request, token, appKey, requestNo] });
          refreshSummary();
        }
      }
    };

    void (async () => {
      let attempt = 0;
      while (!controller.signal.aborted) {
        setStatus(attempt === 0 ? "connecting" : "offline");
        try {
          const end = await streamAIGatewayLive(
            token,
            appKey,
            {
              onOpen: () => {
                if (attempt > 0) {
                  void queryClient.invalidateQueries({ queryKey: [KEY.requests, token, appKey] });
                  refreshSummary();
                }
                attempt = 0;
                setStatus("live");
              },
              onEvent: apply
            },
            controller.signal
          );
          if (end === "unauthorized") {
            setStatus("unauthorized");
            return;
          }
          if (end === "bye") continue;
        } catch {
          if (controller.signal.aborted) return;
        }
        attempt += 1;
        setStatus("offline");
        await new Promise((resolve) => setTimeout(resolve, Math.min(30_000, 1000 * 2 ** (attempt - 1))));
      }
    })();

    return () => {
      controller.abort();
      if (summaryTimer.current) clearTimeout(summaryTimer.current);
      summaryTimer.current = null;
      timers.forEach((timer) => clearTimeout(timer));
      timers.clear();
    };
  }, [token, appKey, paused, queryClient]);

  return { status: (paused ? "paused" : token && appKey ? status : "connecting") as AIGatewayLiveStatus, fresh };
}

export function useRefundAIGatewayRequestMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ requestNo, reason }: { requestNo: string; reason: string }) =>
      refundAIGatewayRequest(token as string, appKey as string, requestNo, reason),
    onSuccess: invalidate
  });
}

export function useAIGatewayKeysQuery(appKey: string | null | undefined, params: AIGatewayKeyParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.keys, token, appKey, params],
    queryFn: () => listAIGatewayKeys(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useSetAIGatewayKeyStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ keyId, status }: { keyId: number; status: "active" | "disabled" }) =>
      setAIGatewayKeyStatus(token as string, appKey as string, keyId, status),
    onSuccess: invalidate
  });
}

export function useAIGatewayStatsQuery(appKey?: string | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, days],
    queryFn: () => getAIGatewayStats(token as string, appKey as string, days),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useAIGatewayUserAccountQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.account, token, appKey, userId],
    queryFn: () => getAIGatewayUserAccount(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useAIGatewayUserCreditsQuery(appKey: string | null | undefined, userId: number | null, page: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.credits, token, appKey, userId, page],
    queryFn: () => listAIGatewayUserCredits(token as string, appKey as string, userId as number, page, 20),
    enabled: Boolean(token && appKey && userId),
    placeholderData: (previous) => previous
  });
}

export function useAdjustAIGatewayCreditMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ userId, payload }: { userId: number; payload: AIGatewayCreditAdjustPayload }) =>
      adjustAIGatewayUserCredit(token as string, appKey as string, userId, payload),
    onSuccess: invalidate
  });
}

// ── 计费方案 ──

/**
 * 价格模拟。payload 里带草稿设置，键用整份 payload —— 草稿一变就是一次新的模拟；
 * 上一次的结果先留着（placeholderData），输入时不闪。
 */
export function useAIGatewayPricingPreviewQuery(
  appKey: string | null | undefined,
  payload: AIGatewayPricingPreviewPayload | null
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.pricingPreview, token, appKey, payload],
    queryFn: () => previewAIGatewayPricing(token as string, appKey as string, payload as AIGatewayPricingPreviewPayload),
    enabled: Boolean(token && appKey && payload),
    placeholderData: (previous) => previous,
    staleTime: 30_000,
    retry: false
  });
}

// ── 额度商店 ──

export function useAIGatewayPackagesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.packages, token, appKey],
    queryFn: () => listAIGatewayPackages(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAIGatewayPackageMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ id, payload }: { id?: number; payload: AIGatewayPackagePayload }) =>
      id
        ? updateAIGatewayPackage(token as string, appKey as string, id, payload)
        : createAIGatewayPackage(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAIGatewayPackageMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: (packageId: number) => deleteAIGatewayPackage(token as string, appKey as string, packageId),
    onSuccess: invalidate
  });
}

export function useAIGatewayPackageOrdersQuery(appKey: string | null | undefined, params: AIGatewayPackageOrderParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.packageOrders, token, appKey, params],
    queryFn: () => listAIGatewayPackageOrders(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useRefundAIGatewayPackageOrderMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ orderNo, reason }: { orderNo: string; reason: string }) =>
      refundAIGatewayPackageOrder(token as string, appKey as string, orderNo, reason),
    onSuccess: invalidate
  });
}

/** enabled = false 时不查（例如订单详情还没打开）。 */
export function useAIGatewayLotsQuery(appKey: string | null | undefined, params: AIGatewayLotParams, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.lots, token, appKey, params],
    queryFn: () => listAIGatewayLots(token as string, appKey as string, params),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: (previous) => previous
  });
}

export function useGrantAIGatewayLotMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ userId, payload }: { userId: number; payload: AIGatewayLotGrantPayload }) =>
      grantAIGatewayLot(token as string, appKey as string, userId, payload),
    onSuccess: invalidate
  });
}

export function useRevokeAIGatewayLotMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateScope();
  return useMutation({
    mutationFn: ({ lotId, reason }: { lotId: number; reason: string }) =>
      revokeAIGatewayLot(token as string, appKey as string, lotId, reason),
    onSuccess: invalidate
  });
}

export function useAIGatewayCommerceQuery(appKey?: string | null, days = 30) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.commerce, token, appKey, days],
    queryFn: () => getAIGatewayCommerce(token as string, appKey as string, days),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}
