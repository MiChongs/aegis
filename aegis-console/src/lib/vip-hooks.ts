"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  claimAdminVipTrial,
  deleteAdminVipFeature,
  deleteAdminVipPlan,
  getAdminVipEntitlement,
  grantAdminVip,
  listAdminVipFeatures,
  listAdminVipPlans,
  listAdminVipTransactions,
  listAdminVipTrialClaims,
  resetAdminVipTrial,
  saveAdminVipFeature,
  saveAdminVipPlan,
  type VipFeaturePayload,
  type VipPlanPayload
} from "@/lib/api/vip";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 会员域 hooks。
 *
 * **失效关系集中在这里**，不散在组件里：一次套餐保存会同时影响套餐列表、
 * 某个用户的权益判定（他的套餐名变了）与开通记录三张表；删一个功能标识
 * 还会牵动套餐（那几个套餐从此少一项权益）。散着写必然漏掉其中几张，
 * 表现为「保存成功了但列表没变」，而那看起来像是保存失败。
 */

const KEY = {
  plans: "admin-vip-plans",
  features: "admin-vip-features",
  entitlement: "admin-vip-entitlement",
  claims: "admin-vip-trial-claims",
  transactions: "admin-vip-transactions"
} as const;

/** 会员域的全部缓存键，任一写操作后成组失效 */
const VIP_SCOPE_KEYS = Object.values(KEY);

function useInvalidateVipScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(VIP_SCOPE_KEYS.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

// ── 套餐 ──

export function useAdminVipPlansQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.plans, token, appKey],
    queryFn: () => listAdminVipPlans(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdminVipPlanMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: VipPlanPayload) => saveAdminVipPlan(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAdminVipPlanMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (planId: number) => deleteAdminVipPlan(token as string, appKey as string, planId),
    onSuccess: invalidate
  });
}

// ── 功能标识目录 ──

export function useAdminVipFeaturesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.features, token, appKey],
    queryFn: () => listAdminVipFeatures(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveAdminVipFeatureMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: VipFeaturePayload) => saveAdminVipFeature(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useDeleteAdminVipFeatureMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (tag: string) => deleteAdminVipFeature(token as string, appKey as string, tag),
    onSuccess: invalidate
  });
}

// ── 会员权益 ──

export function useAdminVipEntitlementQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.entitlement, token, appKey, userId],
    queryFn: () => getAdminVipEntitlement(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId && userId > 0)
  });
}

export function useGrantAdminVipMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: { userId: number; days: number; reason?: string; bonusIntegral?: number }) =>
      grantAdminVip(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useAdminVipTransactionsQuery(
  appKey?: string | null,
  params?: { userId?: number; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.transactions, token, appKey, params?.userId ?? 0, params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listAdminVipTransactions(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

// ── 试用 ──

export function useAdminVipTrialClaimsQuery(
  appKey?: string | null,
  params?: { page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.claims, token, appKey, params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listAdminVipTrialClaims(token as string, appKey as string, params),
    enabled: Boolean(token && appKey)
  });
}

export function useClaimAdminVipTrialMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (userId: number) => claimAdminVipTrial(token as string, appKey as string, userId),
    onSuccess: invalidate
  });
}

export function useResetAdminVipTrialMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (userId: number) => resetAdminVipTrial(token as string, appKey as string, userId),
    onSuccess: invalidate
  });
}
