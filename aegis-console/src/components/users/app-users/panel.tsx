"use client";

import { useCallback, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import type { RowSelectionState } from "@tanstack/react-table";
import { ChevronLeft, ChevronRight, Loader2 } from "lucide-react";
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
import { cn } from "@/lib/utils";
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
        <EmptyState title="暂无应用" />
      </div>
    );
  }

  const currentApp = apps.find((app) => app.appKey === appKey);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">用户与权限</p>
          <h1 className="mt-1 text-xl font-semibold tracking-tight">应用用户</h1>
        </div>
        <div className="flex items-center gap-2">
          {usersQuery.isFetching && !usersQuery.isLoading ? (
            <Loader2 className="size-3.5 animate-spin text-muted-foreground" aria-label="正在刷新" />
          ) : null}
          <Select
            value={appKey ?? ""}
            onValueChange={(value) => applyQuery({ ...query, appKey: value, page: 1 })}
          >
            <SelectTrigger className="h-10 w-64 gap-2.5 rounded-xl pl-2 text-left" aria-label="选择应用">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-xs font-semibold text-primary">
                {(currentApp?.name || "应").slice(0, 1)}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{currentApp?.name || "选择应用"}</span>
                <span className="block truncate text-[11px] text-muted-foreground">
                  {currentApp ? (currentApp.status ? "运行中" : "已停用") : ""}
                </span>
              </span>
            </SelectTrigger>
            {/* 触发器里放的是自定义内容而不是 <SelectValue>，默认的 item-aligned 定位要靠
                SelectValue 节点对齐，缺了它下拉列表定位失败、根本显示不出来；popper 定位只锚定触发器 */}
            <SelectContent position="popper" align="end" sideOffset={6} className="w-(--radix-select-trigger-width)">
              {apps.map((app) => (
                <SelectItem key={app.id} value={app.appKey}>
                  <span className="flex items-center gap-2">
                    <span
                      className={cn("size-1.5 shrink-0 rounded-full", app.status ? "bg-emerald-500" : "bg-muted-foreground/40")}
                      aria-hidden
                    />
                    {app.name}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

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
        emptyText={query.status !== "all" || query.keyword ? "没有符合条件的用户" : "该应用还没有用户"}
        footer={
          <Pagination
            page={query.page}
            limit={query.limit}
            total={total}
            totalPages={totalPages}
            onPageChange={(page) => applyQuery({ ...query, page })}
            onLimitChange={(limit) => applyQuery({ ...query, limit, page: 1 })}
          />
        }
      />

      <BulkActionBar appKey={appKey} selectedIds={selectedIds} onClear={() => setSelection({})} />
    </div>
  );
}

/** 页码窗口：首尾页常驻，当前页前后各一页，其余折叠成省略号 */
function pageWindow(page: number, pages: number): Array<number | "gap"> {
  if (pages <= 7) return Array.from({ length: pages }, (_, index) => index + 1);
  const keep = new Set([1, pages, page - 1, page, page + 1].filter((value) => value >= 1 && value <= pages));
  const sorted = [...keep].sort((a, b) => a - b);
  const result: Array<number | "gap"> = [];
  sorted.forEach((value, index) => {
    if (index > 0 && value - sorted[index - 1] > 1) result.push("gap");
    result.push(value);
  });
  return result;
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
        {total === 0 ? "共 0 位用户" : `第 ${from}–${to} 位，共 ${total.toLocaleString("zh-CN")} 位`}
      </span>
      <div className="flex flex-wrap items-center gap-3">
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
        </div>
        <nav className="flex items-center gap-1" aria-label="分页">
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            aria-label="上一页"
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
          >
            <ChevronLeft className="size-3.5" />
          </Button>
          {pageWindow(page, pages).map((value, index) =>
            value === "gap" ? (
              <span key={`gap-${index}`} className="w-5 text-center text-muted-foreground/60">
                …
              </span>
            ) : (
              <button
                key={value}
                type="button"
                aria-current={value === page ? "page" : undefined}
                onClick={() => onPageChange(value)}
                className={cn(
                  "h-7 min-w-7 rounded-md px-1.5 tabular-nums transition-colors",
                  value === page
                    ? "bg-foreground font-medium text-background"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                {value}
              </button>
            )
          )}
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            aria-label="下一页"
            disabled={page >= pages}
            onClick={() => onPageChange(page + 1)}
          >
            <ChevronRight className="size-3.5" />
          </Button>
        </nav>
      </div>
    </div>
  );
}
