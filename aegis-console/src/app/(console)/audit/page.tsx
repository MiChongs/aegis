"use client";

import { Suspense, useCallback, useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import type { AuditLog } from "@/lib/api/types";
import { ApiError } from "@/lib/api/client";
import {
  useAuditFacetsQuery,
  useAuditLogsInfiniteQuery,
  useAuditOverviewQuery,
  useExportAuditLogsMutation
} from "@/lib/audit-hooks";
import { SectionHeading } from "@/components/ui/section-heading";
import { AuditDetail } from "@/components/audit/audit-detail";
import { HIGH_RISK_SEVERITIES } from "@/components/audit/audit-shared";
import {
  AuditFilters,
  EMPTY_FILTERS,
  filtersFromSearch,
  filtersToParams,
  filtersToSearch,
  hasActiveFilters,
  type AuditFilterState
} from "@/components/audit/audit-filters";
import { AuditList } from "@/components/audit/audit-list";
import { AuditOverview } from "@/components/audit/audit-overview";

/**
 * 审计日志（仅超级管理员）。
 *
 * 操作名、模块、对象由后端的操作目录给出，这里只负责筛选与排版。
 * 默认只看「操作」（不含查看类请求）；筛选状态写在 URL 上，复制地址即可分享当前视图。
 */
export default function AuditPage() {
  // 筛选读写 URL 查询参数，useSearchParams 必须包在 Suspense 边界内
  return (
    <Suspense>
      <AuditPageContent />
    </Suspense>
  );
}

function AuditPageContent() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const filters = useMemo(() => filtersFromSearch(new URLSearchParams(searchParams.toString())), [searchParams]);
  const params = useMemo(() => filtersToParams(filters), [filters]);

  const setFilters = useCallback(
    (next: AuditFilterState) => {
      const query = filtersToSearch(next).toString();
      router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
    },
    [pathname, router]
  );
  const patchFilters = useCallback(
    (patch: Partial<AuditFilterState>) => setFilters({ ...filters, ...patch }),
    [filters, setFilters]
  );

  const overviewQuery = useAuditOverviewQuery();
  const facetsQuery = useAuditFacetsQuery();
  const logsQuery = useAuditLogsInfiniteQuery(params);
  const exportMutation = useExportAuditLogsMutation();

  const logs = useMemo<AuditLog[]>(() => logsQuery.data?.pages.flatMap((page) => page.items) ?? [], [logsQuery.data]);
  const total = logsQuery.data?.pages[0]?.total ?? 0;
  const [selected, setSelected] = useState<AuditLog | null>(null);

  const facets = facetsQuery.data;
  const { fetchNextPage } = logsQuery;
  const loadMore = useCallback(() => void fetchNextPage(), [fetchNextPage]);

  const activeTile = filters.range === "today" && filters.view !== "read"
    ? filters.status === "failed"
      ? "failed"
      : filters.severity === HIGH_RISK_SEVERITIES
        ? "highRisk"
        : null
    : null;

  const exportCsv = () => {
    exportMutation.mutate(params, {
      onSuccess: () => toast.success("已导出"),
      onError: (error) => toast.error(error instanceof ApiError ? error.message : "导出失败")
    });
  };

  return (
    <div className="page-stack">
      <SectionHeading eyebrow="控制台" title="审计日志" />

      <AuditOverview
        overview={overviewQuery.data}
        loading={overviewQuery.isLoading}
        activeTile={activeTile}
        onFailed={() => setFilters({ ...EMPTY_FILTERS, view: "operation", range: "today", status: "failed" })}
        onHighRisk={() => setFilters({ ...EMPTY_FILTERS, view: "operation", range: "today", severity: HIGH_RISK_SEVERITIES })}
        onModule={(module) => setFilters({ ...EMPTY_FILTERS, view: "operation", range: "7d", module })}
        onAdmin={(name) => {
          const admin = facets?.admins.find((item) => item.label === name);
          setFilters(admin ? { ...EMPTY_FILTERS, view: "operation", range: "7d", admin: admin.key } : { ...EMPTY_FILTERS, q: name });
        }}
      />

      <AuditFilters
        filters={filters}
        facets={facets}
        exporting={exportMutation.isPending}
        onChange={patchFilters}
        onExport={exportCsv}
      />

      <AuditList
        logs={logs}
        total={total}
        loading={logsQuery.isLoading}
        error={logsQuery.isError ? (logsQuery.error instanceof ApiError ? logsQuery.error.message : "请稍后重试") : null}
        hasMore={Boolean(logsQuery.hasNextPage)}
        loadingMore={logsQuery.isFetchingNextPage}
        hasFilters={hasActiveFilters(filters)}
        selectedId={selected?.id}
        onLoadMore={loadMore}
        onOpen={setSelected}
        onRetry={() => void logsQuery.refetch()}
        onClearFilters={() => setFilters({ ...EMPTY_FILTERS, view: filters.view })}
      />

      <AuditDetail
        log={selected}
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        onSameSession={(sessionId) => {
          setSelected(null);
          setFilters({ ...EMPTY_FILTERS, view: "all", session: sessionId });
        }}
        onSameAdmin={(adminId) => {
          setSelected(null);
          setFilters({ ...EMPTY_FILTERS, view: filters.view, admin: String(adminId) });
        }}
      />
    </div>
  );
}
