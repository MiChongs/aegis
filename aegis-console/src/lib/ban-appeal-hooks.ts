"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as api from "@/lib/api/ban-appeals";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 封禁申诉与全站封禁的查询与操作（docs/account-suspension.md）。
 *
 * 申诉按 appKey 走应用级接口；不指定应用时走跨应用接口（超级管理员）。
 * 两者共用同一组缓存前缀，通过、驳回、改设置之后一起失效。
 */
export const BAN_APPEAL_KEYS = {
  all: ["ban-appeals"] as const,
  list: ["ban-appeals", "list"] as const,
  stats: ["ban-appeals", "stats"] as const,
  detail: ["ban-appeals", "detail"] as const,
  settings: ["ban-appeals", "settings"] as const,
  platform: ["platform-bans"] as const,
};

/** appKey 为空 = 全部应用 */
export function useBanAppeals(appKey: string | undefined, q: api.BanAppealQuery, enabled = true) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...BAN_APPEAL_KEYS.list, appKey ?? "*", q],
    queryFn: () => (appKey ? api.listBanAppeals(t, appKey, q) : api.listAllBanAppeals(t, q)),
    enabled: !!t && enabled,
    placeholderData: keepPreviousData,
  });
}

export function useBanAppealStats(appKey: string | undefined, enabled = true) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...BAN_APPEAL_KEYS.stats, appKey ?? "*"],
    queryFn: () => (appKey ? api.getBanAppealStats(t, appKey) : api.getAllBanAppealStats(t)),
    enabled: !!t && enabled,
    staleTime: 15_000,
  });
}

export function useBanAppeal(appKey: string | undefined, id: number | undefined) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...BAN_APPEAL_KEYS.detail, appKey, id],
    queryFn: () => api.getBanAppeal(t, appKey as string, id as number),
    enabled: !!t && !!appKey && !!id,
  });
}

function useInvalidateAppeals() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: BAN_APPEAL_KEYS.all });
    qc.invalidateQueries({ queryKey: BAN_APPEAL_KEYS.platform });
    // 通过会解封：用户详情、封禁记录、风控处置清单都要刷新
    qc.invalidateQueries({ predicate: (q) => typeof q.queryKey[0] === "string" && /user|ban|risk-ops/i.test(q.queryKey[0] as string) });
  };
}

export function useDecideBanAppeal(appKey: string | undefined) {
  const t = useAdminToken() as string;
  const inv = useInvalidateAppeals();
  return useMutation({
    mutationFn: ({ id, decision, ...input }: { id: number; decision: "approve" | "reject" } & api.AppealDecisionInput) =>
      decision === "approve" ? api.approveBanAppeal(t, appKey as string, id, input) : api.rejectBanAppeal(t, appKey as string, id, input),
    onSettled: inv,
  });
}

export function useRerunBanAppealReview(appKey: string | undefined) {
  const t = useAdminToken() as string;
  const inv = useInvalidateAppeals();
  return useMutation({
    mutationFn: (id: number) => api.rerunBanAppealReview(t, appKey as string, id),
    onSettled: inv,
  });
}

export function useBanAppealSettings(appKey: string | undefined) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...BAN_APPEAL_KEYS.settings, appKey],
    queryFn: () => api.getBanAppealSettings(t, appKey as string),
    enabled: !!t && !!appKey,
  });
}

export function useSaveBanAppealSettings(appKey: string | undefined) {
  const t = useAdminToken() as string;
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (settings: api.BanAppealSettings) => api.updateBanAppealSettings(t, appKey as string, settings),
    onSuccess: (data) => qc.setQueryData([...BAN_APPEAL_KEYS.settings, appKey], data),
    onSettled: () => qc.invalidateQueries({ queryKey: BAN_APPEAL_KEYS.settings }),
  });
}

// ── 全站封禁 ──

export function usePlatformBans(q: { status?: string; q?: string; page?: number; pageSize?: number }, enabled = true) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...BAN_APPEAL_KEYS.platform, "list", q],
    queryFn: () => api.listPlatformBans(t, q),
    enabled: !!t && enabled,
    placeholderData: keepPreviousData,
  });
}

export function usePlatformBan(id: number | undefined) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...BAN_APPEAL_KEYS.platform, "detail", id],
    queryFn: () => api.getPlatformBan(t, id as number),
    enabled: !!t && !!id,
  });
}

export function useRevokePlatformBan() {
  const t = useAdminToken() as string;
  const inv = useInvalidateAppeals();
  return useMutation({
    mutationFn: ({ id, reason }: { id: number; reason: string }) => api.revokePlatformBan(t, id, reason),
    onSettled: inv,
  });
}
