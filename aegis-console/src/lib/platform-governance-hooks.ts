"use client";

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  applyPlatformAppGovernance,
  batchApplyGovernance,
  getAppGovernance,
  getGovernanceCatalog,
  getPlatformAppGovernance,
  listGovernanceActions,
  listGovernanceAppeals,
  listPlatformApps,
  reviewGovernanceAppeal,
  revokePlatformAppSessions,
  submitAppGovernanceAppeal,
  withdrawAppGovernanceAppeal,
  type GovernanceActionParams,
  type GovernanceActionPayload,
  type GovernanceAppealParams,
  type PlatformOverviewParams
} from "@/lib/api/platform-governance";

/**
 * 治理动作会同时改变：总览列表、单应用详情、流水、申诉队列，
 * 以及应用页顶部的治理横幅。任何一处漏失效都会让操作者看到"点了没反应"，
 * 因此统一在这里一次性失效整棵 platform-governance 子树。
 */
function invalidateGovernance(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ["platform-governance"] });
  // 应用列表里的 status/loginStatus 与治理状态并列展示，一并刷新
  void qc.invalidateQueries({ queryKey: ["apps"] });
}

export function useGovernanceCatalogQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-governance", "catalog", token],
    queryFn: () => getGovernanceCatalog(token as string),
    enabled: Boolean(token),
    // 编译进后端二进制的静态表，不会在运行期变化
    staleTime: 30 * 60_000,
    gcTime: 60 * 60_000
  });
}

/**
 * `options.enabled` 供**非治理页面**按权限自行关闭。
 * `/apps` 列表把这份总览当作用户数与今日新增的来源，但那是锦上添花的增强层：
 * 没有 `platform:app:read` 的管理员必须整条不发请求，否则每次进列表都撞一次 403。
 */
export function usePlatformAppsQuery(params: PlatformOverviewParams = {}, options?: { enabled?: boolean }) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-governance", "apps", token, params],
    queryFn: () => listPlatformApps(token as string, params),
    enabled: Boolean(token) && options?.enabled !== false
  });
}

export function usePlatformAppGovernanceQuery(appKey?: string | number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-governance", "detail", token, appKey],
    queryFn: () => getPlatformAppGovernance(token as string, appKey as string | number),
    enabled: Boolean(token) && appKey !== null && typeof appKey !== "undefined" && appKey !== ""
  });
}

export function useGovernanceActionsQuery(params: GovernanceActionParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-governance", "actions", token, params],
    queryFn: () => listGovernanceActions(token as string, params),
    enabled: Boolean(token)
  });
}

export function useGovernanceAppealsQuery(params: GovernanceAppealParams = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-governance", "appeals", token, params],
    queryFn: () => listGovernanceAppeals(token as string, params),
    enabled: Boolean(token)
  });
}

export function useApplyGovernanceMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { appKey: string | number; payload: GovernanceActionPayload }) =>
      applyPlatformAppGovernance(token as string, args.appKey, args.payload),
    onSuccess: () => invalidateGovernance(qc)
  });
}

export function useBatchGovernanceMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (payload: GovernanceActionPayload & { appids: number[] }) =>
      batchApplyGovernance(token as string, payload),
    onSuccess: () => invalidateGovernance(qc)
  });
}

export function useRevokeAppSessionsMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { appKey: string | number; reason: string }) =>
      revokePlatformAppSessions(token as string, args.appKey, args.reason),
    onSuccess: () => invalidateGovernance(qc)
  });
}

export function useReviewGovernanceAppealMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: {
      appealId: number;
      decision: "approved" | "rejected";
      note?: string;
      restore?: boolean;
    }) =>
      reviewGovernanceAppeal(token as string, args.appealId, {
        decision: args.decision,
        note: args.note,
        restore: args.restore
      }),
    onSuccess: () => invalidateGovernance(qc)
  });
}

// ── 应用侧 ──

export function useAppGovernanceQuery(appKey?: string | number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["platform-governance", "app-detail", token, appKey],
    queryFn: () => getAppGovernance(token as string, appKey as string | number),
    enabled: Boolean(token) && appKey !== null && typeof appKey !== "undefined" && appKey !== ""
  });
}

export function useSubmitGovernanceAppealMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { appKey: string | number; content: string; attachments?: string[] }) =>
      submitAppGovernanceAppeal(token as string, args.appKey, {
        content: args.content,
        attachments: args.attachments
      }),
    onSuccess: () => invalidateGovernance(qc)
  });
}

export function useWithdrawGovernanceAppealMutation() {
  const token = useAdminToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { appKey: string | number; appealId: number }) =>
      withdrawAppGovernanceAppeal(token as string, args.appKey, args.appealId),
    onSuccess: () => invalidateGovernance(qc)
  });
}
