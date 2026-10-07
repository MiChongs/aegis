"use client";

import { keepPreviousData, useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";
import { useAdminToken } from "@/lib/admin-hooks";
import {
  exportAuditLogs,
  getAuditFacets,
  getAuditLog,
  getAuditOverview,
  listAuditLogs,
  type AuditListParams
} from "@/lib/api/audit";

/** 审计日志的 React Query hooks。列表按页累加，供按日期分组的连续列表使用。 */

const PAGE_SIZE = 50;

export function useAuditLogsInfiniteQuery(params: Omit<AuditListParams, "page" | "limit">) {
  const token = useAdminToken();
  return useInfiniteQuery({
    queryKey: ["audit-logs", token, params],
    initialPageParam: 1,
    enabled: Boolean(token),
    placeholderData: keepPreviousData,
    queryFn: ({ pageParam }) => listAuditLogs(token as string, { ...params, page: pageParam as number, limit: PAGE_SIZE }),
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((sum, page) => sum + page.items.length, 0);
      return loaded >= lastPage.total ? undefined : (lastPage.page ?? allPages.length) + 1;
    }
  });
}

export function useAuditLogQuery(id?: number | null) {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["audit-log", token, id],
    queryFn: () => getAuditLog(token as string, id as number),
    enabled: Boolean(token && id)
  });
}

export function useAuditOverviewQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["audit-overview", token],
    queryFn: () => getAuditOverview(token as string),
    enabled: Boolean(token),
    refetchInterval: 60_000
  });
}

export function useAuditFacetsQuery() {
  const token = useAdminToken();
  return useQuery({
    queryKey: ["audit-facets", token],
    queryFn: () => getAuditFacets(token as string),
    enabled: Boolean(token),
    staleTime: 5 * 60_000
  });
}

export function useExportAuditLogsMutation() {
  const token = useAdminToken();
  return useMutation({
    mutationFn: (params: Omit<AuditListParams, "page" | "limit">) => exportAuditLogs(token as string, params)
  });
}
