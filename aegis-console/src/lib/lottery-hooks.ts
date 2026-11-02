"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  adjustLotteryTickets,
  getLotteryTicketAccount,
  getLotteryTicketOverview,
  listLotteryTicketAccounts,
  listLotteryTicketLedger,
  type LotteryTicketAdjustPayload,
  type LotteryTicketLedgerParams,
  createLotteryActivity,
  createLotteryFortuneControls,
  getLotteryFortune,
  getLotteryFortuneUser,
  listLotteryFortuneControls,
  previewLotteryFortuneControl,
  revokeLotteryFortuneControls,
  setLotteryFortuneSpread,
  getLotterySettings,
  saveLotterySettings,
  type LotterySupreme,
  type LotteryFortuneControlParams,
  type LotteryFortuneControlPayload,
  deleteLotteryActivity,
  fulfillLotteryDraw,
  getLotteryAudit,
  getLotteryRisk,
  getLotteryCatalog,
  getLotteryStats,
  listLotteryActivities,
  listLotteryDraws,
  setLotteryActivityStatus,
  simulateLottery,
  updateLotteryActivity,
  type LotteryActivityPayload,
  type LotteryDrawParams,
  type LotterySimulatePayload,
  type LotteryStatus
} from "@/lib/api/lottery";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 抽奖域 hooks。
 *
 * 失效关系集中在这里：改活动影响活动列表与统计里的奖品；标记发放影响记录、
 * 活动上的待发放数与统计。整组失效，不在组件里逐个点名。
 * 目录里的激励广告场景归激励广告区块管，改了场景要等目录自然过期或重新进入区块。
 */

const KEY = {
  catalog: "admin-lottery-catalog",
  activities: "admin-lottery-activities",
  draws: "admin-lottery-draws",
  stats: "admin-lottery-stats",
  audit: "admin-lottery-audit",
  risk: "admin-lottery-risk",
  fortune: "admin-lottery-fortune",
  fortuneUser: "admin-lottery-fortune-user",
  fortuneControls: "admin-lottery-fortune-controls",
  tickets: "admin-lottery-tickets",
  ticketAccounts: "admin-lottery-ticket-accounts",
  ticketAccount: "admin-lottery-ticket-account",
  ticketLedger: "admin-lottery-ticket-ledger",
  settings: "admin-lottery-settings"
} as const;

const LOTTERY_SCOPE = [KEY.activities, KEY.draws, KEY.stats, KEY.settings];

function useInvalidateLotteryScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(LOTTERY_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useLotteryCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.catalog, token, appKey],
    queryFn: () => getLotteryCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

// ── 活动 ──

export function useLotteryActivitiesQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.activities, token, appKey],
    queryFn: () => listLotteryActivities(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (input: { activityId?: number; payload: LotteryActivityPayload }) =>
      input.activityId
        ? updateLotteryActivity(token as string, appKey as string, input.activityId, input.payload)
        : createLotteryActivity(token as string, appKey as string, input.payload),
    onSuccess: invalidate
  });
}

export function useSetLotteryActivityStatusMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (input: { activityId: number; status: LotteryStatus }) =>
      setLotteryActivityStatus(token as string, appKey as string, input.activityId, input.status),
    onSuccess: invalidate
  });
}

export function useDeleteLotteryActivityMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (activityId: number) => deleteLotteryActivity(token as string, appKey as string, activityId),
    onSuccess: invalidate
  });
}

export function useLotteryStatsQuery(appKey?: string | null, activityId?: number | null, days = 14) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.stats, token, appKey, activityId, days],
    queryFn: () => getLotteryStats(token as string, appKey as string, activityId as number, days),
    enabled: Boolean(token && appKey && activityId)
  });
}

// ── 抽奖记录 ──

export function useLotteryDrawsQuery(appKey?: string | null, params?: LotteryDrawParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.draws,
      token,
      appKey,
      params?.activityId ?? 0,
      params?.userId ?? 0,
      params?.status ?? "",
      params?.source ?? "",
      params?.winning ?? false,
      params?.keyword ?? "",
      params?.risk ?? "",
      params?.riskAction ?? "",
      params?.outcome ?? "",
      params?.prizeId ?? 0,
      params?.start ?? "",
      params?.end ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listLotteryDraws(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    // 翻页、改筛选时保留上一页，表格不闪成骨架
    placeholderData: keepPreviousData
  });
}

export function useFulfillLotteryDrawMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (input: { drawId: number; note: string }) =>
      fulfillLotteryDraw(token as string, appKey as string, input.drawId, input.note),
    onSuccess: invalidate
  });
}

// ── 策略模拟 ──

/** 模拟不改任何数据，不失效缓存 */
export function useSimulateLotteryMutation(appKey: string) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: LotterySimulatePayload) => simulateLottery(token as string, appKey, payload)
  });
}

/** 风控总览 */
export function useLotteryRiskQuery(appKey: string | null | undefined, activityId: number | null | undefined, days: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.risk, token, appKey, activityId, days],
    queryFn: () => getLotteryRisk(token as string, appKey as string, activityId as number, days),
    enabled: Boolean(token && appKey && activityId),
    placeholderData: keepPreviousData,
    staleTime: 30_000
  });
}

/** 出奖审计（读全部带方案的记录，按需手动刷新） */
export function useLotteryAuditQuery(appKey: string | null | undefined, activityId: number | null | undefined, days: number) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.audit, token, appKey, activityId, days],
    queryFn: () => getLotteryAudit(token as string, appKey as string, activityId as number, days),
    enabled: Boolean(token && appKey && activityId),
    staleTime: 60_000
  });
}

// ── 运气控制 ──
//
// 下干预、撤销、调差异都会改变今日分布、单人读数与记录：三者整组失效。调差异还改了活动策略，活动列表一并失效。

const FORTUNE_SCOPE = [KEY.fortune, KEY.fortuneUser, KEY.fortuneControls];

function useInvalidateFortuneScope() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all(FORTUNE_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useLotteryFortuneQuery(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.fortune, token, appKey, activityId],
    queryFn: () => getLotteryFortune(token as string, appKey as string, activityId as number),
    enabled: Boolean(token && appKey && activityId)
  });
}

export function useLotteryFortuneUserQuery(appKey?: string | null, activityId?: number | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.fortuneUser, token, appKey, activityId, userId],
    queryFn: () => getLotteryFortuneUser(token as string, appKey as string, activityId as number, userId as number),
    enabled: Boolean(token && appKey && activityId && userId)
  });
}

export function useLotteryFortuneControlsQuery(
  appKey?: string | null,
  activityId?: number | null,
  params?: LotteryFortuneControlParams
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.fortuneControls,
      token,
      appKey,
      activityId,
      params?.userId ?? 0,
      params?.kind ?? "",
      params?.status ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listLotteryFortuneControls(token as string, appKey as string, activityId as number, params),
    enabled: Boolean(token && appKey && activityId),
    placeholderData: (previous) => previous
  });
}

export function useCreateLotteryFortuneControlsMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateFortuneScope();
  return useMutation({
    mutationFn: (payload: LotteryFortuneControlPayload) =>
      createLotteryFortuneControls(token as string, appKey as string, activityId as number, payload),
    onSuccess: invalidate
  });
}

/** 预演不改任何数据，不失效缓存 */
export function usePreviewLotteryFortuneControlMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: LotteryFortuneControlPayload) =>
      previewLotteryFortuneControl(token as string, appKey as string, activityId as number, payload)
  });
}

export function useRevokeLotteryFortuneControlsMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const invalidate = useInvalidateFortuneScope();
  return useMutation({
    mutationFn: (input: { ids?: number[]; all?: boolean }) =>
      revokeLotteryFortuneControls(token as string, appKey as string, activityId as number, input),
    onSuccess: invalidate
  });
}

export function useSetLotteryFortuneSpreadMutation(appKey?: string | null, activityId?: number | null) {
  const token = useAdminToken();
  const invalidateFortune = useInvalidateFortuneScope();
  const invalidateLottery = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (spread: number) => setLotteryFortuneSpread(token as string, appKey as string, activityId as number, spread),
    onSuccess: async () => {
      await Promise.all([invalidateFortune(), invalidateLottery()]);
    }
  });
}

// ── 至尊加成（应用设置）──
//
// 保存会改变跟随应用设置的活动的运气：运气总览与单人读数、活动列表（积分账）一并失效。

export function useLotterySettingsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.settings, token, appKey],
    queryFn: () => getLotterySettings(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useSaveLotterySettingsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  const invalidateFortune = useInvalidateFortuneScope();
  const invalidateLottery = useInvalidateLotteryScope();
  return useMutation({
    mutationFn: (supreme: LotterySupreme) => saveLotterySettings(token as string, appKey as string, supreme),
    onSuccess: async (data) => {
      queryClient.setQueryData([KEY.settings, token, appKey], data);
      await Promise.all([invalidateFortune(), invalidateLottery()]);
    }
  });
}

// ── 抽奖次数 ──

const TICKET_SCOPE = [KEY.tickets, KEY.ticketAccounts, KEY.ticketAccount, KEY.ticketLedger];

export function useLotteryTicketOverviewQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.tickets, token, appKey],
    queryFn: () => getLotteryTicketOverview(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useLotteryTicketAccountsQuery(
  appKey?: string | null,
  params?: { keyword?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.ticketAccounts, token, appKey, params?.keyword ?? "", params?.page ?? 1, params?.limit ?? 20],
    queryFn: () => listLotteryTicketAccounts(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useLotteryTicketAccountQuery(appKey?: string | null, userId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.ticketAccount, token, appKey, userId],
    queryFn: () => getLotteryTicketAccount(token as string, appKey as string, userId as number),
    enabled: Boolean(token && appKey && userId)
  });
}

export function useLotteryTicketLedgerQuery(appKey?: string | null, params?: LotteryTicketLedgerParams) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [
      KEY.ticketLedger,
      token,
      appKey,
      params?.userId ?? 0,
      params?.kind ?? "",
      params?.origin ?? "",
      params?.keyword ?? "",
      params?.page ?? 1,
      params?.limit ?? 20
    ],
    queryFn: () => listLotteryTicketLedger(token as string, appKey as string, params),
    enabled: Boolean(token && appKey),
    placeholderData: (previous) => previous
  });
}

export function useAdjustLotteryTicketsMutation(appKey?: string | null) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, payload }: { userId: number; payload: LotteryTicketAdjustPayload }) =>
      adjustLotteryTickets(token as string, appKey as string, userId, payload),
    onSuccess: async () => {
      await Promise.all(TICKET_SCOPE.map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
    }
  });
}

