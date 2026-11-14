"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import * as ops from "@/lib/api/risk-ops";
import { useAdminToken } from "@/lib/admin-hooks";
import { usePermissionChecker } from "@/lib/permissions";
import { RISK_KEYS } from "@/lib/risk-hooks";

export const RISK_OPS_KEYS = {
  banned: ["risk-ops", "banned"] as const,
  candidates: ["risk-ops", "candidates"] as const,
  inspect: ["risk-ops", "inspect"] as const,
};

/** 解除风控与删除账号：system:risk_ops:manage */
export function useCanOperateRisk() {
  return usePermissionChecker()("system:risk_ops:manage");
}

/** 正被风控平台封禁的账号 */
export function useRiskBanned(q: { app?: number; q?: string; page?: number; limit?: number }) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...RISK_OPS_KEYS.banned, q],
    queryFn: () => ops.listRiskBanned(t, q),
    enabled: !!t,
    placeholderData: keepPreviousData,
  });
}

/** 按风控信号发现风险账号 */
export function useRiskCandidates(q: { app?: number; source: ops.CandidateSource; days?: number; min?: number; q?: string; page?: number; limit?: number }) {
  const t = useAdminToken() as string;
  return useQuery({
    queryKey: [...RISK_OPS_KEYS.candidates, q],
    queryFn: () => ops.findCandidates(t, q),
    enabled: !!t,
    placeholderData: keepPreviousData,
  });
}

/** 一组账号的处置清单（related：连带找关联账号） */
export function useRiskInspection(appId: number | undefined, userIds: number[], related: boolean, enabled = true) {
  const t = useAdminToken() as string;
  const ids = [...userIds].sort((a, b) => a - b);
  return useQuery({
    queryKey: [...RISK_OPS_KEYS.inspect, appId, ids, related],
    queryFn: () => ops.inspectAccounts(t, appId as number, ids, related),
    enabled: !!t && !!appId && ids.length > 0 && enabled,
    staleTime: 15_000,
  });
}

function useInvalidateAll() {
  const qc = useQueryClient();
  return () => {
    for (const key of [RISK_OPS_KEYS.banned, RISK_OPS_KEYS.candidates, RISK_OPS_KEYS.inspect, RISK_KEYS.lists, RISK_KEYS.cases, RISK_KEYS.profile, RISK_KEYS.graph, RISK_KEYS.audit]) {
      qc.invalidateQueries({ queryKey: key });
    }
    // 用户详情、用户列表、封禁记录
    qc.invalidateQueries({ predicate: (q) => typeof q.queryKey[0] === "string" && /user|ban/i.test(q.queryKey[0] as string) });
  };
}

export function useReleaseAccounts() {
  const t = useAdminToken() as string;
  const inv = useInvalidateAll();
  return useMutation({ mutationFn: (input: ops.OpsReleaseInput) => ops.releaseAccounts(t, input), onSettled: inv });
}

export function usePurgeAccounts() {
  const t = useAdminToken() as string;
  const inv = useInvalidateAll();
  return useMutation({ mutationFn: (input: ops.OpsPurgeInput) => ops.purgeAccounts(t, input), onSettled: inv });
}
