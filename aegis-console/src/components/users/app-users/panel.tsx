"use client";

import { useCallback, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import type { RowSelectionState } from "@tanstack/react-table";
import { AppWindow, ChevronLeft, ChevronRight, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api-client";
import { useAdminAppUsersQuery, useAdminAppsQuery } from "@/lib/admin-hooks";
import { useExportAppUsersMutation } from "@/lib/app-user-hooks";
import type { AdminAppUserItem } from "@/lib/api/types";
import { BulkActionBar } from "./bulk-actions";
import { AppUsersFilters } from "./filters";
import { AppUsersMetrics } from "./metrics";
import { AppUsersTable } from "./table";
import {
  PAGE_SIZES,
  parseQuery,
  serializeQuery,
  toListParams,
  type UserQueryState
} from "./shared";

/**
 * 应用用户列表（/app-users 的页面主体）。
 *
 * 整页只有**一个**查询状态对象（`UserQueryState`），URL 是它的唯一持久化形式。
 * 旧版把 keyword / status / page / limit 拆成四个 useState 再逐个同步进 URL，
 * 于是后端支持的十个过滤条件里有八个从来没被接上来 —— 加一个条件要动四处。
 *
 * 选中态**不进 URL**：它是"我现在要对谁动手"的临时意图，不该被分享或前进后退还原。
 * 换应用、换筛选、翻页时一律清空 —— 选中的行已经不在屏幕上了，留着它只会让
 * 批量操作打到看不见的账号上。
 */
export function AppUsersPanel() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);

  const urlQuery = useMemo(() => parseQuery(searchParams), [searchParams]);
  const appKey = urlQuery.appKey || apps[0]?.appKey || null;
  const query: UserQueryState = useMemo(() => ({ ...urlQuery, appKey }), [urlQuery, appKey]);

  const [selection, setSelection] = useState<RowSelectionState>({});
  const exportMutation = useExportAppUsersMutation(appKey);

  const usersQuery = useAdminAppUsersQuery(appKey, toListParams(query));
  const items = usersQuery.data?.items ?? [];
  const total = usersQuery.data?.total ?? 0;
  const totalPages = usersQuery.data?.totalPages ?? 0;

  const applyQuery = useCallback(
    (next: UserQueryState, keepSelection = false) => {
      if (!keepSelection) setSelection({});
      router.replace(serializeQuery(next), { scroll: false });
    },
    [router]
  );

  const backHref = useMemo(() => serializeQuery(query), [query]);

  const openDetail = useCallback(
    (user: AdminAppUserItem) => {
      if (!appKey) return;
      const params = new URLSearchParams({ from: backHref });
      router.push(`/app-users/${appKey}/${user.id}?${params.toString()}`);
    },
    [appKey, backHref, router]
  );

  const selectedIds = useMemo(
    () =>
      Object.entries(selection)
        .filter(([, selected]) => selected)
        .map(([id]) => Number(id))
        .filter((id) => Number.isFinite(id)),
    [selection]
  );

  async function handleExport() {
    try {
      const { page: _page, limit: _limit, ...rest } = toListParams(query);
      await exportMutation.mutateAsync(rest);
      toast.success("导出已开始下载");
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : "导出失败");
    }
  }

  if (appsQuery.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64 rounded-md" />
        <Skeleton className="h-24 w-full rounded-2xl" />
        <Skeleton className="h-8 w-full rounded-md" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  }

  if (!apps.length) {
    return (
      <div className="space-y-6">
        <SectionHeading eyebrow="控制台" title="应用用户" />
        <EmptyState
          title="还没有应用"
          description="应用用户按应用隔离，先在「应用」里创建一个应用，这里才会有用户可管理。"
        />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <SectionHeading
        eyebrow="控制台"
        title="应用用户"
        action={
          <div className="flex items-center gap-2">
            {usersQuery.isFetching ? (
              <Loader2 className="size-3.5 animate-spin text-muted-foreground" />
            ) : null}
            <span className="hidden text-[11px] text-muted-foreground sm:inline">
              用户按应用隔离，换应用即换一套用户库
            </span>
            <Select
              value={appKey ?? ""}
              onValueChange={(value) => applyQuery({ ...query, appKey: value, page: 1 })}
            >
              <SelectTrigger size="sm" className="h-8 w-48 text-xs">
                <AppWindow className="size-3.5 text-muted-foreground" />
                <SelectValue placeholder="选择应用" />
              </SelectTrigger>
              <SelectContent>
                {apps.map((app) => (
                  <SelectItem key={app.id} value={app.appKey} className="text-xs">
                    {app.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        }
      />

      <AppUsersMetrics
        appKey={appKey}
        status={query.status}
        onStatusChange={(next) => applyQuery({ ...query, status: next, page: 1 })}
      />

      <AppUsersFilters
        state={query}
        onChange={(next) => applyQuery(next)}
        onExport={handleExport}
        exporting={exportMutation.isPending}
        total={total}
        onOpenUser={openDetail}
      />

      <AppUsersTable
        data={items}
        loading={usersQuery.isLoading}
        query={query}
        onQueryChange={(next) => applyQuery(next)}
        selection={selection}
        onSelectionChange={setSelection}
        onRowClick={openDetail}
        emptyText={
          total === 0 && items.length === 0
            ? "没有命中任何用户。检查一下上面的筛选条件，或清空全部重来。"
            : "暂无用户"
        }
      />

      <Pagination
        page={query.page}
        limit={query.limit}
        total={total}
        totalPages={totalPages}
        onPageChange={(page) => applyQuery({ ...query, page })}
        onLimitChange={(limit) => applyQuery({ ...query, limit, page: 1 })}
      />

      <BulkActionBar appKey={appKey} selectedIds={selectedIds} onClear={() => setSelection({})} />
    </div>
  );
}

function Pagination({
  page,
  limit,
  total,
  totalPages,
  onPageChange,
  onLimitChange
}: {
  page: number;
  limit: number;
  total: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  onLimitChange: (limit: number) => void;
}) {
  const pages = Math.max(totalPages, 1);
  const from = total === 0 ? 0 : (page - 1) * limit + 1;
  const to = Math.min(page * limit, total);

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-muted-foreground">
      <span className="tabular-nums">
        {total === 0 ? "共 0 条" : `第 ${from}–${to} 条 · 共 ${total.toLocaleString("zh-CN")} 条`}
      </span>
      <div className="flex items-center gap-2">
        <span>每页</span>
        <Select value={String(limit)} onValueChange={(value) => onLimitChange(Number(value))}>
          <SelectTrigger size="sm" className="h-7 w-[76px] text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((size) => (
              <SelectItem key={size} value={String(size)} className="text-xs">
                {size}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="tabular-nums">
          {page} / {pages}
        </span>
        <Button
          size="icon"
          variant="outline"
          className="size-7"
          aria-label="上一页"
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
        >
          <ChevronLeft className="size-3.5" />
        </Button>
        <Button
          size="icon"
          variant="outline"
          className="size-7"
          aria-label="下一页"
          disabled={page >= pages}
          onClick={() => onPageChange(page + 1)}
        >
          <ChevronRight className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}
