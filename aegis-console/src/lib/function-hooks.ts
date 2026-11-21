"use client";

import { useCallback } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  activateAppFunctionVersion,
  analyzeAppFunction,
  createAppFunctionTrigger,
  deleteAppFunctionTrigger,
  fireAppFunctionTrigger,
  getAppFunctionInvocation,
  getAppFunctionOverview,
  invokeAppFunction,
  listAppFunctionTriggerJobs,
  listAppFunctionTriggers,
  replayAppFunctionInvocation,
  retryAppFunctionTriggerJob,
  updateAppFunctionTrigger,
  type AppFunctionInvocationQuery,
  type AppFunctionTriggerInput,
  type AppFunctionTriggerJobQuery,
  createAppFunction,
  createAppFunctionVersion,
  deleteAppFunction,
  deleteAppFunctionKv,
  deleteAppFunctionVersion,
  getAppFunctionCatalog,
  getAppFunctionStats,
  getAppFunctionVersion,
  listAppFunctionInvocations,
  listAppFunctionKv,
  listAppFunctions,
  listAppFunctionVersions,
  testAppFunction,
  updateAppFunction,
  type AppFunctionUpdate
} from "@/lib/api/app-functions";

/**
 * 远程函数域的 React Query hooks。
 *
 * 失效关系集中在这里：一次「激活版本」会同时改变函数列表（activeVersion 与
 * status）、版本列表与统计三处，散在各组件里必然漏掉其中一两张 ——
 * 表现是激活成功了但列表上那一行还写着旧版本，而刷新一下又是对的。
 */

const CATALOG_KEY = "app-function-catalog";
const LIST_KEY = "app-functions";
const VERSIONS_KEY = "app-function-versions";
const VERSION_KEY = "app-function-version";
const INVOCATIONS_KEY = "app-function-invocations";
const STATS_KEY = "app-function-stats";
const KV_KEY = "app-function-kv";
const ANALYSIS_KEY = "app-function-analysis";
const OVERVIEW_KEY = "app-function-overview";
const INVOCATION_KEY = "app-function-invocation";
const TRIGGERS_KEY = "app-function-triggers";
const TRIGGER_JOBS_KEY = "app-function-trigger-jobs";

/**
 * 能力目录。它是静态的（只随后端版本变化），因此缓存放到很久 ——
 * 每切一次函数就重拉一遍目录纯属浪费。
 */
export function useFunctionCatalogQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [CATALOG_KEY, token, appKey],
    queryFn: () => getAppFunctionCatalog(token as string, appKey as string),
    enabled: Boolean(token && appKey),
    staleTime: 30 * 60 * 1000,
    gcTime: 60 * 60 * 1000
  });
}

export function useAppFunctionsQuery(appKey?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [LIST_KEY, token, appKey],
    queryFn: () => listAppFunctions(token as string, appKey as string),
    enabled: Boolean(token && appKey)
  });
}

export function useFunctionVersionsQuery(appKey?: string | null, name?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [VERSIONS_KEY, token, appKey, name],
    queryFn: () => listAppFunctionVersions(token as string, appKey as string, name as string),
    enabled: Boolean(token && appKey && name)
  });
}

/** 单个版本的正文。按 (函数, 版本) 缓存 —— 版本不可变，取回来的正文永远不会变。 */
export function useFunctionVersionSourceQuery(
  appKey?: string | null,
  name?: string | null,
  version?: string | null
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [VERSION_KEY, token, appKey, name, version],
    queryFn: () =>
      getAppFunctionVersion(token as string, appKey as string, name as string, version as string),
    enabled: Boolean(token && appKey && name && version),
    staleTime: Infinity
  });
}

export function useFunctionInvocationsQuery(
  appKey: string | null | undefined,
  name: string | null | undefined,
  query: AppFunctionInvocationQuery
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [INVOCATIONS_KEY, token, appKey, name, query],
    queryFn: () => listAppFunctionInvocations(token as string, appKey as string, name as string, query),
    enabled: Boolean(token && appKey && name)
  });
}

export function useFunctionStatsQuery(
  appKey?: string | null,
  name?: string | null,
  hours = 24
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [STATS_KEY, token, appKey, name, hours],
    queryFn: () => getAppFunctionStats(token as string, appKey as string, name as string, hours),
    enabled: Boolean(token && appKey && name)
  });
}

export function useFunctionKvQuery(
  appKey: string | null | undefined,
  query: { scope?: string; scopeId?: number; prefix?: string; page?: number; limit?: number }
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [KV_KEY, token, appKey, query],
    queryFn: () => listAppFunctionKv(token as string, appKey as string, query),
    enabled: Boolean(token && appKey)
  });
}

/**
 * 应用全景。queryKey 第四位留空（null）：成组失效按「第三位应用、第四位函数名」匹配，
 * 全景不属于任何一个函数，任何函数的变化都该让它重拉。
 * 换时间窗时沿用上一份数据，卡片右上角转圈，而不是整页闪回骨架。
 */
export function useFunctionAppOverviewQuery(appKey?: string | null, windowHours = 24) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [OVERVIEW_KEY, token, appKey, null, windowHours],
    queryFn: () => getAppFunctionOverview(token as string, appKey as string, windowHours),
    enabled: Boolean(token && appKey),
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[2] === appKey ? previous : undefined,
    refetchInterval: 60 * 1000
  });
}

/** 单条调用的完整记录。调用记录落库后不再变化（执行中的除外），缓存一分钟足够 */
export function useFunctionInvocationQuery(
  appKey?: string | null,
  name?: string | null,
  invocationId?: number | null
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [INVOCATION_KEY, token, appKey, name, invocationId],
    queryFn: () =>
      getAppFunctionInvocation(token as string, appKey as string, name as string, invocationId as number),
    enabled: Boolean(token && appKey && name && invocationId),
    staleTime: 60 * 1000
  });
}

export function useFunctionTriggersQuery(appKey?: string | null, name?: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [TRIGGERS_KEY, token, appKey, name],
    queryFn: () => listAppFunctionTriggers(token as string, appKey as string, name as string),
    enabled: Boolean(token && appKey && name),
    select: (data) => data.items ?? []
  });
}

/** 触发任务队列。排队中的任务会自己变化，开着页面时每 15 秒刷一次 */
export function useFunctionTriggerJobsQuery(
  appKey: string | null | undefined,
  name: string | null | undefined,
  query: AppFunctionTriggerJobQuery
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [TRIGGER_JOBS_KEY, token, appKey, name, query],
    queryFn: () => listAppFunctionTriggerJobs(token as string, appKey as string, name as string, query),
    enabled: Boolean(token && appKey && name),
    placeholderData: (previous) => previous,
    refetchInterval: 15 * 1000
  });
}

/** 成组失效：改一个函数会牵动列表 / 版本 / 审计 / 统计 / 全景 / 触发器多张表。 */
export function useFunctionInvalidator(appKey?: string | null) {
  const client = useQueryClient();
  return useCallback(
    async (name?: string) => {
      await Promise.all(
        [
          LIST_KEY,
          VERSIONS_KEY,
          VERSION_KEY,
          INVOCATIONS_KEY,
          INVOCATION_KEY,
          STATS_KEY,
          KV_KEY,
          OVERVIEW_KEY,
          TRIGGERS_KEY,
          TRIGGER_JOBS_KEY
        ].map((key) =>
          client.invalidateQueries({
            predicate: (query) => {
              const [head, , queryAppKey, queryName] = query.queryKey as unknown[];
              if (head !== key) return false;
              if (appKey && queryAppKey !== appKey) return false;
              // 未指定函数名时失效该应用下的全部
              return !name || !queryName || queryName === name;
            }
          })
        )
      );
    },
    [client, appKey]
  );
}

export function useCreateFunctionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (payload: Parameters<typeof createAppFunction>[2]) =>
      createAppFunction(token as string, appKey as string, payload),
    onSuccess: () => invalidate()
  });
}

export function useUpdateFunctionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; payload: AppFunctionUpdate }) =>
      updateAppFunction(token as string, appKey as string, variables.name, variables.payload),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

export function useDeleteFunctionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (name: string) => deleteAppFunction(token as string, appKey as string, name),
    onSuccess: () => invalidate()
  });
}

export function useCreateVersionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: {
      name: string;
      payload: Parameters<typeof createAppFunctionVersion>[3];
    }) => createAppFunctionVersion(token as string, appKey as string, variables.name, variables.payload),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

export function useActivateVersionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; version: string }) =>
      activateAppFunctionVersion(token as string, appKey as string, variables.name, variables.version),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

export function useDeleteVersionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; version: string }) =>
      deleteAppFunctionVersion(token as string, appKey as string, variables.name, variables.version),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

/**
 * 试跑。**刻意不失效任何缓存** —— 试跑不产生真实副作用，
 * 让它刷新一遍列表会给人「刚刚发生了什么」的错觉。
 */
export function useTestFunctionMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (variables: { name: string; payload: Parameters<typeof testAppFunction>[3] }) =>
      testAppFunction(token as string, appKey as string, variables.name, variables.payload)
  });
}

/**
 * 静态检查。用 query 而不是 mutation：它没有副作用，且要跟着正文变化自动重跑
 * —— 让作者手动点一次「检查」，等于回到了「发布被拦才知道有问题」。
 *
 * 正文进 queryKey 意味着改一个字符就是一次新查询，因此调用方**必须**传防抖后的
 * 正文；缓存在这里的价值是「撤销回上一版内容时不用再问一次」。
 */
export function useFunctionAnalysisQuery(
  appKey?: string | null,
  name?: string | null,
  source?: string,
  enabled = true
) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [ANALYSIS_KEY, token, appKey, name, source],
    queryFn: () => analyzeAppFunction(token as string, appKey as string, name as string, source as string),
    enabled: Boolean(enabled && token && appKey && name && source?.trim()),
    staleTime: 5 * 60 * 1000,
    // 检查失败不该在编辑器上留一片红：静默保留上一次结果，
    // 让作者继续写下去 —— 网络抖动不是他的代码有问题。
    retry: false,
    placeholderData: (previous) => previous
  });
}

export function useDeleteFunctionKvMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (entry: { scope: string; scopeId: number; key: string }) =>
      deleteAppFunctionKv(token as string, appKey as string, entry),
    onSuccess: () => invalidate()
  });
}

export function useSaveTriggerMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; triggerId?: number; payload: AppFunctionTriggerInput }) =>
      variables.triggerId
        ? updateAppFunctionTrigger(
            token as string,
            appKey as string,
            variables.name,
            variables.triggerId,
            variables.payload
          )
        : createAppFunctionTrigger(token as string, appKey as string, variables.name, variables.payload),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

export function useDeleteTriggerMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; triggerId: number }) =>
      deleteAppFunctionTrigger(token as string, appKey as string, variables.name, variables.triggerId),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

/** 立即触发是真实执行：调用审计、触发器计数、全景都会变 */
export function useFireTriggerMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: {
      name: string;
      triggerId: number;
      payload: Parameters<typeof fireAppFunctionTrigger>[4];
    }) =>
      fireAppFunctionTrigger(
        token as string,
        appKey as string,
        variables.name,
        variables.triggerId,
        variables.payload
      ),
    onSettled: (_data, _error, variables) => invalidate(variables.name)
  });
}

export function useRetryTriggerJobMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; jobId: number }) =>
      retryAppFunctionTriggerJob(token as string, appKey as string, variables.name, variables.jobId),
    onSuccess: (_data, variables) => invalidate(variables.name)
  });
}

/** 真实调用（管理员发起，可代某个用户） */
export function useInvokeFunctionMutation(appKey?: string | null) {
  const token = useAdminToken();
  const invalidate = useFunctionInvalidator(appKey);
  return useMutation({
    mutationFn: (variables: { name: string; payload: Parameters<typeof invokeAppFunction>[3] }) =>
      invokeAppFunction(token as string, appKey as string, variables.name, variables.payload),
    onSettled: (_data, _error, variables) => invalidate(variables.name)
  });
}

/** 重放是试跑：不产生真实副作用，因此与试跑一样不失效任何缓存 */
export function useReplayInvocationMutation(appKey?: string | null) {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (variables: { name: string; invocationId: number }) =>
      replayAppFunctionInvocation(token as string, appKey as string, variables.name, variables.invocationId)
  });
}
