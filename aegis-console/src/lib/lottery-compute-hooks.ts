"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  analyzeLottery,
  cancelLotteryComputeJob,
  createLotteryComputeJob,
  deleteLotteryComputeJob,
  getLotteryComputeJob,
  listLotteryComputeJobs,
  solveLottery,
  type LotteryAnalyzePayload,
  type LotteryComputeJob,
  type LotteryComputePayload,
  type LotteryPersonaPayload,
  type LotterySolvePayload
} from "@/lib/api/lottery-compute";
import { useAdminToken } from "@/lib/admin-hooks";

/**
 * 抽奖计算引擎 hooks。
 *
 * 精确分析与求解不改任何数据，不失效缓存。推演任务：发起、取消、删除后任务列表与任务本身失效；
 * 排队与执行中的任务按固定间隔轮询（进度由执行它的实例每两秒写一次库），结束即停。
 */

const KEY = {
  analysis: "admin-lottery-analysis",
  jobs: "admin-lottery-compute-jobs",
  job: "admin-lottery-compute-job"
} as const;

const POLL_MS = 1000;

const active = (job?: LotteryComputeJob | null) => job?.status === "queued" || job?.status === "running";

/** 已保存活动的精确分析（人群变了就是另一份） */
export function useLotteryAnalysisQuery(
  appKey: string | null | undefined,
  activityId: number | null | undefined,
  personas: LotteryPersonaPayload[]
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.analysis, token, appKey, activityId, JSON.stringify(personas)],
    queryFn: () => analyzeLottery(token as string, appKey as string, { activityId: activityId as number, personas }),
    enabled: Boolean(token && appKey && activityId),
    placeholderData: keepPreviousData,
    staleTime: 30_000
  });
}

/**
 * 编辑器里对未保存内容的精确分析。载荷本身进查询键：弹窗打开时取一次编辑内容，
 * 换人群就是另一份；不留缓存（下次打开编辑内容多半已经变了）。
 */
export function useLotteryDraftAnalysisQuery(appKey: string, payload: LotteryAnalyzePayload) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.analysis, "draft", token, appKey, JSON.stringify(payload)],
    queryFn: () => analyzeLottery(token as string, appKey, payload),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    gcTime: 0,
    retry: false
  });
}

export function useSolveLotteryMutation(appKey: string) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: LotterySolvePayload) => solveLottery(token as string, appKey, payload)
  });
}

// ── 推演任务 ──

export function useLotteryComputeJobsQuery(appKey?: string | null, activityId?: number | null, page = 1, limit = 10) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.jobs, token, appKey, activityId ?? 0, page, limit],
    queryFn: () =>
      listLotteryComputeJobs(token as string, appKey as string, { activityId: activityId ?? undefined, page, limit }),
    enabled: Boolean(token && appKey),
    placeholderData: keepPreviousData,
    // 列表里还有没结束的任务时接着轮询
    refetchInterval: (query) => (query.state.data?.items.some(active) ? POLL_MS : false)
  });
}

export function useLotteryComputeJobQuery(appKey?: string | null, jobId?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KEY.job, token, appKey, jobId],
    queryFn: () => getLotteryComputeJob(token as string, appKey as string, jobId as number),
    enabled: Boolean(token && appKey && jobId),
    refetchInterval: (query) => (!query.state.data || active(query.state.data) ? POLL_MS : false)
  });
}

function useInvalidateCompute() {
  const queryClient = useQueryClient();
  return async () => {
    await Promise.all([KEY.jobs, KEY.job].map((key) => queryClient.invalidateQueries({ queryKey: [key] })));
  };
}

export function useCreateLotteryComputeJobMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCompute();
  return useMutation({
    mutationFn: (payload: LotteryComputePayload) => createLotteryComputeJob(token as string, appKey, payload),
    onSuccess: invalidate
  });
}

export function useCancelLotteryComputeJobMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCompute();
  return useMutation({
    mutationFn: (jobId: number) => cancelLotteryComputeJob(token as string, appKey, jobId),
    onSuccess: invalidate
  });
}

export function useDeleteLotteryComputeJobMutation(appKey: string) {
  const token = useAdminToken();
  const invalidate = useInvalidateCompute();
  return useMutation({
    mutationFn: (jobId: number) => deleteLotteryComputeJob(token as string, appKey, jobId),
    onSuccess: invalidate
  });
}
