"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  clearUserLevelCap,
  createGrowthRule,
  deleteGrowthRule,
  getGrowthCatalog,
  getUserGrowth,
  listGrowthEvents,
  listGrowthRules,
  previewGrowth,
  revokeGrowthRule,
  setUserLevelCap,
  updateGrowthRule,
  type GrowthPreviewPayload,
  type GrowthRuleParams,
  type GrowthRulePayload
} from "@/lib/api/growth";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 成长调控 hooks（docs/growth-control.md）。
 *
 * 用户总览的查询键以 `admin-app-user` 开头：资产页调整积分 / 经验后已有的失效会顺带刷新「奖惩」页。
 * 规则或封顶变化后同时失效等级相关的查询（等级页的用户列表与详情会显示封顶）。
 */

const KEY = {
  catalog: "admin-growth-catalog",
  rules: "admin-growth-rules",
  events: "admin-growth-events",
  profile: "admin-app-user"
} as const;

function useInvalidateGrowth() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: [KEY.rules] }),
      queryClient.invalidateQueries({ queryKey: [KEY.events] }),
      queryClient.invalidateQueries({ queryKey: [KEY.profile] }),
      queryClient.invalidateQueries({ queryKey: ["admin-level-user"] }),
      queryClient.invalidateQueries({ queryKey: ["admin-level-users"] })
    ]);
}

export function useGrowthCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getGrowthCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 5 * 60_000
  });
}

export function useUserGrowthQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.profile, "growth", token, appKey, userId],
    queryFn: () => getUserGrowth(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId),
    placeholderData: keepPreviousData
  });
}

export function useGrowthRulesQuery(appKey?: string | null, params: GrowthRuleParams = {}, enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.rules, token, appKey, params],
    queryFn: () => listGrowthRules(token as string, appKey as string, params),
    enabled: Boolean(token && appKey && enabled),
    placeholderData: keepPreviousData
  });
}

export function useGrowthEventsQuery(appKey?: string | null, params: { userId?: number; page?: number; limit?: number } = {}) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.events, token, appKey, params],
    queryFn: () => listGrowthEvents(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData
  });
}

export function useSaveGrowthRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateGrowth();
  return useMutation({
    mutationFn: (input: { ruleId?: number; payload: GrowthRulePayload }) =>
      input.ruleId
        ? updateGrowthRule(token as string, appKey as string, input.ruleId, input.payload)
        : createGrowthRule(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useRevokeGrowthRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateGrowth();
  return useMutation({
    mutationFn: (input: { ruleId: number; note?: string }) => revokeGrowthRule(token as string, appKey as string, input.ruleId, input.note),
    onSuccess: invalidate
  });
}

export function useDeleteGrowthRuleMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateGrowth();
  return useMutation({
    mutationFn: (ruleId: number) => deleteGrowthRule(token as string, appKey as string, ruleId),
    onSuccess: invalidate
  });
}

export function useGrowthPreviewMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: GrowthPreviewPayload) => previewGrowth(token as string, appKey as string, payload)
  });
}

export function useUserLevelCapMutation(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateGrowth();
  return useMutation({
    mutationFn: (input: { level: number; demote: boolean; note?: string } | { clear: true; note?: string }) =>
      "clear" in input
        ? clearUserLevelCap(token as string, appKey as string, userId as number, input.note)
        : setUserLevelCap(token as string, appKey as string, userId as number, input),
    onSuccess: invalidate
  });
}
