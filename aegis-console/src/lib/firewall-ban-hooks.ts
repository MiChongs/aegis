"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAdminToken } from "@/lib/admin-hooks";
import { ApiError } from "@/lib/api/client";
import { firewallBansApi, type BanBatchCreateInput, type BanFilter, type BanUpdateInput } from "@/lib/api/firewall-bans";

/**
 * IP 封禁批量管理的数据层。缓存键沿用旧面板的 "ip-bans" / "geo-bans" 前缀，
 * 写操作后一并失效防火墙工作台（运行态、事件）与旧 hooks 的缓存。
 */

const BANS_KEY = ["ip-bans"] as const;

async function invalidateBans(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: BANS_KEY }),
    queryClient.invalidateQueries({ queryKey: ["geo-bans"] }),
    queryClient.invalidateQueries({ queryKey: ["admin-firewall"] })
  ]);
}

export function banErrorMessage(error: unknown, fallback = "操作失败") {
  return error instanceof ApiError ? error.message || fallback : fallback;
}

export function useBanListQuery(filter: BanFilter) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...BANS_KEY, "list-v2", token, filter],
    queryFn: () => firewallBansApi.list(token as string, filter),
    enabled: Boolean(token),
    placeholderData: keepPreviousData
  });
}

export function useBanCountQuery(filter: BanFilter, enabled: boolean) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...BANS_KEY, "count", token, filter],
    queryFn: () => firewallBansApi.count(token as string, filter),
    enabled: Boolean(token && enabled)
  });
}

export function useBanStatsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...BANS_KEY, "stats", token],
    queryFn: () => firewallBansApi.stats(token as string),
    enabled: Boolean(token),
    refetchInterval: 30_000,
    placeholderData: keepPreviousData
  });
}

export function useInspectIPQuery(ip: string | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: [...BANS_KEY, "inspect", token, ip],
    queryFn: () => firewallBansApi.inspect(token as string, ip as string),
    enabled: Boolean(token && ip),
    retry: false
  });
}

function useBanMutation<V, R>(run: (token: string, vars: V) => Promise<R>, success?: (result: R, vars: V) => string) {
  const token = useAdminToken();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => run(token as string, vars),
    onSuccess: async (result, vars) => {
      await invalidateBans(queryClient);
      if (success) toast.success(success(result, vars));
    },
    onError: (error) => toast.error(banErrorMessage(error))
  });
}

export function useBatchBanMutation() {
  return useBanMutation(
    (token, input: BanBatchCreateInput) => firewallBansApi.batchBan(token, input),
    (r) => `新增封禁 ${r.succeeded} 个${r.skipped ? `，${r.skipped} 个已在封禁中` : ""}${r.failed ? `，${r.failed} 个无效` : ""}`
  );
}

export function useUnbanIdsMutation() {
  return useBanMutation(
    (token, vars: { ids: number[]; note?: string }) => firewallBansApi.unbanIds(token, vars.ids, vars.note),
    (r) => `已解封 ${r.succeeded} 条${r.skipped ? `，${r.skipped} 条已失效跳过` : ""}`
  );
}

export function useUnbanFilterMutation() {
  return useBanMutation(
    (token, vars: { filter: BanFilter; expected: number; note?: string }) => firewallBansApi.unbanFilter(token, vars.filter, vars.expected, vars.note),
    (r) => (r.truncated ? `已解封 ${r.succeeded} 条（单次上限），还有剩余，请再执行一次` : `已解封 ${r.succeeded} 条`)
  );
}

export function useUpdateBanMutation() {
  return useBanMutation((token, vars: { id: number; input: BanUpdateInput }) => firewallBansApi.update(token, vars.id, vars.input), (ban) => `已修改 ${ban.ip}`);
}

export function useBatchUpdateBansMutation() {
  return useBanMutation(
    (token, vars: { ids: number[]; input: BanUpdateInput }) => firewallBansApi.batchUpdate(token, vars.ids, vars.input),
    (r) => `已修改 ${r.succeeded} 条${r.failed ? `，${r.failed} 条失败` : ""}`
  );
}

export function useGeoBanBatchMutation() {
  return useBanMutation(
    (token, vars: { ids: number[]; action: "enable" | "disable" | "delete" }) => firewallBansApi.geoBatch(token, vars.ids, vars.action),
    (r, vars) => `已${{ enable: "启用", disable: "停用", delete: "删除" }[vars.action]} ${r.processed} 条规则`
  );
}
