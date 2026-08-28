"use client";

/**
 * 用户详情页的 React Query hooks。
 *
 * 之所以不塞进 3400 行的 admin-hooks.ts：详情页的失效关系是**成组**的 ——
 * 一次封禁同时影响封禁列表、生效封禁、用户详情、用户列表、活跃会话五处，
 * 散在通用文件里必然漏掉其中几张。这里用 `invalidateUserScope` 统一收口。
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  adjustAdminUserWallet,
  batchCreateAdminAppUserBan,
  batchUpdateAdminAppUserStatus,
  bulkNotifyAdminAppUsers,
  createAdminUserBan,
  downloadAdminAppUsersCsv,
  getAdminUserActiveBan,
  getAdminUserBans,
  getAdminUserLoginAudits,
  getAdminUserSessionAudits,
  getAdminUserWallet,
  getAdminUserWalletTransactions,
  revokeAdminUserBan
} from "@/lib/api/app-users";
import {
  claimAdminVipTrial,
  getAdminVipEntitlement,
  getAdminVipFeatures,
  getAdminVipPlans,
  getAdminVipTransactions,
  grantAdminVip,
  resetAdminVipTrial
} from "@/lib/api/configuration";
import type { AdminAppUserListParams } from "@/lib/api/apps";
import type { AdminUserBanCreateInput } from "@/lib/api/types";

/** 处置动作（封禁 / 撤销）之后要一起失效的所有查询。 */
const USER_SCOPE_KEYS = [
  ["admin-user-bans"],
  ["admin-user-active-ban"],
  ["admin-app-user"],
  ["admin-app-users"],
  ["admin-user-sessions"],
  ["admin-user-login-audits"],
  ["admin-user-session-audits"]
] as const;

function useInvalidateUserScope() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all(USER_SCOPE_KEYS.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

type Key = string | null | undefined;
type Id = number | string | null | undefined;

const ready = (token: Key, appKey: Key, userId: Id) => Boolean(token && appKey && userId);

// ── 封禁 ──────────────────────────

export function useAdminUserBansQuery(
  appKey?: Key,
  userId?: Id,
  params?: { status?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-bans", token, appKey, userId, params?.status, params?.page, params?.limit],
    queryFn: () => getAdminUserBans(token as string, appKey as string, userId as number, params),
    enabled: ready(token, appKey, userId)
  });
}

export function useAdminUserActiveBanQuery(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-active-ban", token, appKey, userId],
    queryFn: () => getAdminUserActiveBan(token as string, appKey as string, userId as number),
    enabled: ready(token, appKey, userId)
  });
}

export function useCreateAdminUserBanMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const invalidate = useInvalidateUserScope();
  return useMutation({
    mutationFn: (payload: AdminUserBanCreateInput) =>
      createAdminUserBan(token as string, appKey as string, userId as number, payload),
    onSuccess: invalidate
  });
}

export function useRevokeAdminUserBanMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const invalidate = useInvalidateUserScope();
  return useMutation({
    mutationFn: (args: { banId: number; reason?: string }) =>
      revokeAdminUserBan(token as string, appKey as string, userId as number, args.banId, {
        reason: args.reason
      }),
    onSuccess: invalidate
  });
}

// ── 钱包 ──────────────────────────

export function useAdminUserWalletQuery(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-wallet", token, appKey, userId],
    queryFn: () => getAdminUserWallet(token as string, appKey as string, userId as number),
    enabled: ready(token, appKey, userId),
    // 钱包可能尚未开户（首次充值时才建行），后端返回错误时不必反复重试
    retry: false
  });
}

export function useAdminUserWalletTransactionsQuery(
  appKey?: Key,
  userId?: Id,
  params?: { type?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      "admin-user-wallet-txns",
      token,
      appKey,
      userId,
      params?.type,
      params?.page,
      params?.limit
    ],
    queryFn: () =>
      getAdminUserWalletTransactions(token as string, appKey as string, userId as number, params),
    enabled: ready(token, appKey, userId),
    retry: false
  });
}

export function useAdjustAdminUserWalletMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { amount: string; reason?: string }) =>
      adjustAdminUserWallet(token as string, appKey as string, {
        userId: Number(userId),
        amount: payload.amount,
        reason: payload.reason
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["admin-user-wallet"] }),
        queryClient.invalidateQueries({ queryKey: ["admin-user-wallet-txns"] })
      ]);
    }
  });
}

// ── 会员 ──────────────────────────

export function useAdminUserVipTransactionsQuery(
  appKey?: Key,
  userId?: Id,
  params?: { page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-vip-txns", token, appKey, userId, params?.page, params?.limit],
    queryFn: () =>
      getAdminVipTransactions(token as string, appKey as string, {
        userId: Number(userId),
        page: params?.page,
        limit: params?.limit
      }),
    enabled: ready(token, appKey, userId)
  });
}

/** 会员权益判定（与用户端 /vip/status 同一判定入口）。 */
export function useAdminUserVipEntitlementQuery(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-user-vip-entitlement", token, appKey, userId],
    queryFn: () => getAdminVipEntitlement(token as string, appKey as string, Number(userId)),
    enabled: ready(token, appKey, userId)
  });
}

export function useAdminVipPlansQuery(appKey?: Key) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-vip-plans", token, appKey],
    queryFn: () => getAdminVipPlans(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useAdminVipFeaturesQuery(appKey?: Key) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["admin-vip-features", token, appKey],
    queryFn: () => getAdminVipFeatures(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

/** 发放会员后需要一起刷新的查询：记录、权益判定、详情、列表。 */
const VIP_SCOPE_KEYS = [
  ["admin-user-vip-txns"],
  ["admin-user-vip-entitlement"],
  ["admin-app-user"],
  ["admin-app-users"]
] as const;

function useInvalidateVipScope() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all(VIP_SCOPE_KEYS.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

export function useGrantAdminUserVipMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: (payload: {
      planId?: number;
      quantity?: number;
      days?: number;
      features?: string[];
      reason?: string;
      bonusIntegral?: number;
    }) =>
      grantAdminVip(token as string, appKey as string, {
        userId: Number(userId),
        ...payload
      }),
    onSuccess: invalidate
  });
}

export function useClaimAdminVipTrialMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: () => claimAdminVipTrial(token as string, appKey as string, Number(userId)),
    onSuccess: invalidate
  });
}

export function useResetAdminVipTrialMutation(appKey?: Key, userId?: Id) {
  const token = useAdminToken();
  const invalidate = useInvalidateVipScope();
  return useMutation({
    mutationFn: () => resetAdminVipTrial(token as string, appKey as string, Number(userId)),
    onSuccess: invalidate
  });
}

// ── 单用户审计 ──────────────────────────

export function useAdminUserLoginAuditsQuery(
  appKey?: Key,
  userId?: Id,
  params?: { status?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      "admin-user-login-audits",
      token,
      appKey,
      userId,
      params?.status,
      params?.page,
      params?.limit
    ],
    queryFn: () => getAdminUserLoginAudits(token as string, appKey as string, userId as number, params),
    enabled: ready(token, appKey, userId)
  });
}

export function useAdminUserSessionAuditsQuery(
  appKey?: Key,
  userId?: Id,
  params?: { eventType?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      "admin-user-session-audits",
      token,
      appKey,
      userId,
      params?.eventType,
      params?.page,
      params?.limit
    ],
    queryFn: () =>
      getAdminUserSessionAudits(token as string, appKey as string, userId as number, params),
    enabled: ready(token, appKey, userId)
  });
}

// ── 列表页批量操作 ──────────────────────────
//
// 一次批量会同时影响用户列表、应用统计（启用/受限计数）与被操作用户的详情缓存，
// 三张一起失效。只失效列表会让指标带上的「受限 0」在批量限制之后仍然显示 0。

const LIST_SCOPE_KEYS = [
  ["admin-app-users"],
  ["admin-app-user"],
  ["admin-app-stats"],
  ["admin-app-trend"],
  ["admin-user-bans"],
  ["admin-user-active-ban"]
] as const;

function useInvalidateListScope() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all(LIST_SCOPE_KEYS.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

export function useBatchUpdateAppUserStatusMutation(appKey?: Key) {
  const token = useAdminToken();
  const invalidate = useInvalidateListScope();
  return useMutation({
    mutationFn: (payload: {
      userIds: number[];
      enabled: boolean;
      disabledReason?: string;
      clearDisabledEndTime?: boolean;
    }) => batchUpdateAdminAppUserStatus(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useBatchBanAppUsersMutation(appKey?: Key) {
  const token = useAdminToken();
  const invalidate = useInvalidateListScope();
  return useMutation({
    mutationFn: (payload: { userIds: number[] } & AdminUserBanCreateInput) =>
      batchCreateAdminAppUserBan(token as string, appKey as string, payload),
    onSuccess: invalidate
  });
}

export function useBulkNotifyAppUsersMutation(appKey?: Key) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: {
      userIds: number[];
      type: string;
      title: string;
      content: string;
      level?: string;
    }) => bulkNotifyAdminAppUsers(token as string, appKey as string, payload)
  });
}

/**
 * 导出当前筛选结果。
 *
 * 导出的是**筛选条件**而不是选中项 —— 后端 export 端点按查询条件取数，
 * 上限 20000 条，与列表分页无关。想导出选中的少数几个用户，用 userId 精确筛选。
 */
export function useExportAppUsersMutation(appKey?: Key) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (params: Omit<AdminAppUserListParams, "page" | "limit"> & { limit?: number }) =>
      downloadAdminAppUsersCsv(token as string, appKey as string, params)
  });
}
