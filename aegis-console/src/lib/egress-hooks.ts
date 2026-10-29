"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  applyEgressGraph,
  closeMihomoConnection,
  discardEgressGraph,
  exportEgressGraph,
  getEgressGraph,
  getEgressGraphHistory,
  getEgressGraphVersion,
  getMihomoConnections,
  getMihomoLogs,
  getMihomoProxies,
  getMihomoRules,
  getMihomoStatus,
  importEgressGraph,
  installMihomo,
  mihomoMaintenance,
  mihomoProviderAction,
  queryMihomoDNS,
  restartMihomo,
  rollbackEgressGraph,
  saveEgressGraph,
  selectMihomoProxy,
  stopMihomo,
  testMihomoDelay,
  validateEgressGraph,
  type EgressGraph,
  explainEgress,
  getEgressSettings,
  probeEgress,
  resetEgressSettings,
  testEgress,
  updateEgressSettings,
  type EgressSettingsUpdate,
} from "@/lib/api/egress";
import { useAdminToken } from "@/lib/admin-hooks";

const KEY = ["admin-egress-settings"] as const;

export function useEgressSettingsQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...KEY, token],
    queryFn: () => getEgressSettings(token as string),
    enabled: Boolean(token && enabled),
    // 端点健康与流量统计随时间变化，页面停留时定期刷新一次即可。
    refetchInterval: 30_000,
  });
}

export function useUpdateEgressSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: EgressSettingsUpdate) => updateEgressSettings(token as string, payload),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: KEY });
    },
  });
}

export function useResetEgressSettingsMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => resetEgressSettings(token as string),
    // 恢复为环境变量会撤掉画布、停掉核心
    onSuccess: async () => refreshEgress(queryClient),
  });
}

export function useEgressTestMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof testEgress>[1]) => testEgress(token as string, payload),
  });
}

export function useEgressExplainMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (payload: Parameters<typeof explainEgress>[1]) => explainEgress(token as string, payload),
  });
}

export function useEgressProbeMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => probeEgress(token as string),
    onSuccess: async () => {
      // 探测会改写端点健康状态，拉一次最新运行态。
      await queryClient.invalidateQueries({ queryKey: KEY });
    },
  });
}

// ── 画布与 mihomo 核心 ──

const GRAPH_KEY = ["admin-egress-graph"] as const;
const CORE_KEY = ["admin-egress-mihomo"] as const;

/** 画布变化会改写网关运行态，几处视图一起刷新。 */
async function refreshEgress(queryClient: ReturnType<typeof useQueryClient>) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: GRAPH_KEY }),
    queryClient.invalidateQueries({ queryKey: KEY }),
    queryClient.invalidateQueries({ queryKey: CORE_KEY })
  ]);
}

export function useEgressGraphQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...GRAPH_KEY, token],
    queryFn: () => getEgressGraph(token as string),
    enabled: Boolean(token && enabled),
    refetchInterval: 30_000
  });
}

export function useSaveEgressGraphMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { graph: EgressGraph; revision: number }) => saveEgressGraph(token as string, payload),
    onSuccess: (data) => queryClient.setQueryData([...GRAPH_KEY, token], data)
  });
}

export function useValidateEgressGraphMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (graph: EgressGraph) => validateEgressGraph(token as string, graph) });
}

export function useApplyEgressGraphMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (payload: { graph: EgressGraph; revision: number; note?: string }) => applyEgressGraph(token as string, payload),
    onSuccess: async (data) => {
      queryClient.setQueryData([...GRAPH_KEY, token], data);
      await refreshEgress(queryClient);
    }
  });
}

export function useDiscardEgressGraphMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => discardEgressGraph(token as string),
    onSuccess: async () => refreshEgress(queryClient)
  });
}

export function useImportEgressGraphMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (payload: Parameters<typeof importEgressGraph>[1]) => importEgressGraph(token as string, payload) });
}

export function useExportEgressGraphMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (payload: Parameters<typeof exportEgressGraph>[1]) => exportEgressGraph(token as string, payload) });
}

export function useEgressGraphHistoryQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...GRAPH_KEY, "history", token],
    queryFn: () => getEgressGraphHistory(token as string),
    enabled: Boolean(token && enabled)
  });
}

export function useEgressGraphVersionMutation() {
  const token = useAdminToken();
  return useMutation({ mutationFn: (version: number) => getEgressGraphVersion(token as string, version) });
}

export function useRollbackEgressGraphMutation() {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (version: number) => rollbackEgressGraph(token as string, version),
    onSuccess: async () => refreshEgress(queryClient)
  });
}

/** 核心状态与流量采样：页面停留时每 2 秒一次，曲线才是连续的。 */
export function useMihomoStatusQuery(enabled = true, interval = 2_000) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...CORE_KEY, "status", token],
    queryFn: () => getMihomoStatus(token as string),
    enabled: Boolean(token && enabled),
    refetchInterval: interval
  });
}

export function useMihomoProxiesQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...CORE_KEY, "proxies", token],
    queryFn: () => getMihomoProxies(token as string),
    enabled: Boolean(token && enabled),
    refetchInterval: 10_000,
    retry: false
  });
}

export function useMihomoConnectionsQuery(enabled = true, paused = false) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...CORE_KEY, "connections", token],
    queryFn: () => getMihomoConnections(token as string),
    enabled: Boolean(token && enabled),
    refetchInterval: paused ? false : 2_000,
    retry: false
  });
}

export function useMihomoRulesQuery(enabled = true) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...CORE_KEY, "rules", token],
    queryFn: () => getMihomoRules(token as string),
    enabled: Boolean(token && enabled),
    refetchInterval: 10_000,
    retry: false
  });
}

export function useMihomoLogsQuery(after: number, enabled = true, paused = false) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...CORE_KEY, "logs", token, after],
    queryFn: () => getMihomoLogs(token as string, after),
    enabled: Boolean(token && enabled),
    refetchInterval: paused ? false : 2_000
  });
}

function useCoreMutation<TArgs, TResult>(fn: (token: string, args: TArgs) => Promise<TResult>) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (args: TArgs) => fn(token as string, args),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: CORE_KEY });
    }
  });
}

export const useInstallMihomoMutation = () => useCoreMutation((token, version: string | undefined) => installMihomo(token, version));
export const useRestartMihomoMutation = () => useCoreMutation((token, _: void) => restartMihomo(token));
export const useStopMihomoMutation = () => useCoreMutation((token, _: void) => stopMihomo(token));
export const useMihomoDelayMutation = () => useCoreMutation((token, payload: Parameters<typeof testMihomoDelay>[1]) => testMihomoDelay(token, payload));
export const useSelectMihomoMutation = () => useCoreMutation((token, payload: Parameters<typeof selectMihomoProxy>[1]) => selectMihomoProxy(token, payload));
export const useCloseMihomoConnectionMutation = () => useCoreMutation((token, id: string | undefined) => closeMihomoConnection(token, id));
export const useMihomoProviderMutation = () =>
  useCoreMutation((token, payload: Parameters<typeof mihomoProviderAction>[1]) => mihomoProviderAction(token, payload));
export const useMihomoMaintenanceMutation = () => useCoreMutation((token, action: "geo" | "flush-dns") => mihomoMaintenance(token, action));
export const useMihomoDNSMutation = () => useCoreMutation((token, payload: Parameters<typeof queryMihomoDNS>[1]) => queryMihomoDNS(token, payload));
