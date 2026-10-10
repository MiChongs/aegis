"use client";

import { Suspense, useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { AppWindow, LayoutGrid, RotateCw, Rows3, Search, SearchX, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { SectionHeading } from "@/components/ui/section-heading";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { LoadingState } from "@/components/ui/data-state";
import { AppCreateDialog } from "@/components/apps/app-create-dialog";
import { AppDeleteDialog } from "@/components/apps/app-delete-dialog";
import { AppCardGrid, AppListSkeleton, AppTable, type AppListRow } from "@/components/apps/app-list-views";
import { AppListOverview, type AppListCounts } from "@/components/apps/app-list-overview";
import { useAdminAppsQuery, useDeleteAdminAppMutation } from "@/lib/admin-hooks";
import { usePlatformAppsQuery } from "@/lib/platform-governance-hooks";
import { usePermissionChecker } from "@/lib/permissions";
import { useAppScopeStore, type AppListView } from "@/lib/app-scope-store";
import { resolveAppSection } from "@/lib/app-sections";
import { cn } from "@/lib/utils";
import type { AppSummary } from "@/lib/api/types";

/**
 * 应用列表 —— 应用管理的入口页。
 *
 * 这一页只回答「有哪些应用、它们现在什么状态」，配置全部下沉到二级页
 * `/apps/{appKey}`。此前两者被压在同一屏：应用是右上角一个下拉框，13 个配置 Tab
 * 平铺在下面。那种形状下，你既数不出自己有几个应用、也看不出哪个被停用了，
 * 而所谓「切换应用」不过是换掉当前 Tab 的数据源，其余上下文全部丢失。
 *
 * 侧边栏那 13 个三级子项链接的是不带 appKey 的 `/apps?tab=xxx`，
 * 由本页转交给「最近打开的那个应用」（`app-scope-store`），
 * 因此点侧边栏的「第三方登录」落回的是你正在配的应用，不是永远的第一个。
 */

type StatusFilter = "all" | "enabled" | "disabled" | "register-off" | "login-off" | "governed";
type SortKey = "created" | "name" | "id" | "users" | "new" | "logins";

const SORT_LABEL: Record<SortKey, string> = {
  created: "最近创建",
  name: "按名称",
  id: "按应用 ID",
  users: "按用户数",
  new: "按今日新增",
  logins: "按今日登录"
};
const METRIC_SORTS: SortKey[] = ["users", "new", "logins"];

function AppsPageInner() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const can = usePermissionChecker();

  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);

  // 指标是增强层：没有治理读权限就整条不发请求，避免每次进列表都撞一次 403
  const canReadOverview = can("platform:app:read");
  const overviewQuery = usePlatformAppsQuery({ limit: 200 }, { enabled: canReadOverview });
  const metricsByAppKey = useMemo(() => {
    const map = new Map<string, AppListRow["metrics"]>();
    for (const item of overviewQuery.data?.items ?? []) {
      if (item.appKey) map.set(item.appKey, item);
    }
    return map;
  }, [overviewQuery.data]);
  // 有权限就按「有指标」排版，加载期间显示骨架而不是先缺一块再撑开
  const hasMetrics = canReadOverview && !overviewQuery.isError;
  const loginFailuresToday = useMemo(
    () => (overviewQuery.data?.items ?? []).reduce((sum, item) => sum + (item.loginFailureToday ?? 0), 0),
    [overviewQuery.data]
  );

  const listView = useAppScopeStore((s) => s.listView);
  const setListView = useAppScopeStore((s) => s.setListView);
  const lastAppKey = useAppScopeStore((s) => s.lastAppKey);

  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [sortChoice, setSort] = useState<SortKey>("created");
  const sort: SortKey = !hasMetrics && METRIC_SORTS.includes(sortChoice) ? "created" : sortChoice;
  const [deleteTarget, setDeleteTarget] = useState<AppSummary | null>(null);
  const deleteMutation = useDeleteAdminAppMutation();

  /* ── 侧边栏深链转交：`/apps?tab=oauth` → `/apps/{最近应用}?tab=oauth` ── */
  const tabParam = searchParams.get("tab");
  const redirecting = Boolean(tabParam) && (appsQuery.isLoading || apps.length > 0);
  useEffect(() => {
    if (!tabParam || apps.length === 0) return;
    const target = apps.find((app) => app.appKey === lastAppKey) ?? apps[0];
    router.replace(`/apps/${encodeURIComponent(target.appKey)}?tab=${resolveAppSection(tabParam)}`);
  }, [tabParam, apps, lastAppKey, router]);

  const rows = useMemo<AppListRow[]>(() => {
    const kw = keyword.trim().toLowerCase();
    const filtered = apps.filter((app) => {
      const metrics = metricsByAppKey.get(app.appKey);
      if (kw) {
        const haystack = `${app.name} ${app.id} ${app.appKey}`.toLowerCase();
        if (!haystack.includes(kw)) return false;
      }
      switch (status) {
        case "enabled":
          return app.status;
        case "disabled":
          return !app.status;
        case "register-off":
          return !app.registerStatus;
        case "login-off":
          return !app.loginStatus;
        case "governed":
          return Boolean(metrics && metrics.state !== "active");
        default:
          return true;
      }
    });

    const metric = (app: AppSummary, key: "totalUsers" | "newUsersToday" | "loginSuccessToday") =>
      metricsByAppKey.get(app.appKey)?.[key] ?? 0;
    const sorted = [...filtered].sort((a, b) => {
      switch (sort) {
        case "name":
          return a.name.localeCompare(b.name, "zh-CN");
        case "id":
          return a.id - b.id;
        case "users":
          return metric(b, "totalUsers") - metric(a, "totalUsers");
        case "new":
          return metric(b, "newUsersToday") - metric(a, "newUsersToday");
        case "logins":
          return metric(b, "loginSuccessToday") - metric(a, "loginSuccessToday");
        default:
          return new Date(b.createdAt ?? 0).getTime() - new Date(a.createdAt ?? 0).getTime();
      }
    });

    return sorted.map((app) => ({ app, metrics: metricsByAppKey.get(app.appKey) }));
  }, [apps, keyword, metricsByAppKey, sort, status]);

  const counts = useMemo<AppListCounts>(() => {
    const result: AppListCounts = { total: apps.length, enabled: 0, disabled: 0, registerOff: 0, loginOff: 0, governed: 0, fullyOpen: 0 };
    for (const app of apps) {
      if (app.status) result.enabled += 1;
      else result.disabled += 1;
      if (!app.registerStatus) result.registerOff += 1;
      if (!app.loginStatus) result.loginOff += 1;
      if (app.status && app.registerStatus && app.loginStatus) result.fullyOpen += 1;
      const metrics = metricsByAppKey.get(app.appKey);
      if (metrics && metrics.state !== "active") result.governed += 1;
    }
    return result;
  }, [apps, metricsByAppKey]);

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return;
    try {
      await deleteMutation.mutateAsync(deleteTarget.appKey);
      toast.success(`应用 ${deleteTarget.name} 已删除`);
      setDeleteTarget(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [deleteTarget, deleteMutation]);

  if (redirecting) return <LoadingState title="打开应用配置" description="正在跳转..." />;

  const segments: Array<{ key: StatusFilter; label: string; value: number; dot?: string }> = [
    { key: "all", label: "全部", value: counts.total },
    { key: "enabled", label: "已启用", value: counts.enabled, dot: "bg-emerald-500" },
    { key: "disabled", label: "已停用", value: counts.disabled, dot: "bg-zinc-400" },
    { key: "register-off", label: "注册关闭", value: counts.registerOff, dot: "bg-amber-500" },
    { key: "login-off", label: "登录关闭", value: counts.loginOff, dot: "bg-amber-500" }
  ];
  if (hasMetrics) segments.push({ key: "governed", label: "被治理", value: counts.governed, dot: "bg-red-500" });

  const filtered = keyword.trim() !== "" || status !== "all";
  const createDialog = <AppCreateDialog onCreated={(app) => router.push(`/apps/${encodeURIComponent(app.appKey)}`)} />;

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="控制台"
        title="应用"
        action={
          <div className="flex items-center gap-2">
            <Button
              size="icon"
              variant="ghost"
              className="size-8"
              title="刷新"
              disabled={appsQuery.isFetching}
              onClick={() => {
                void appsQuery.refetch();
                if (canReadOverview) void overviewQuery.refetch();
              }}
            >
              <RotateCw className={cn("size-3.5", (appsQuery.isFetching || overviewQuery.isFetching) && "animate-spin")} />
            </Button>
            {createDialog}
          </div>
        }
      />

      <AppListOverview
        counts={counts}
        summary={overviewQuery.data?.summary}
        loginFailuresToday={loginFailuresToday}
        loading={appsQuery.isLoading}
        metricsLoading={canReadOverview && overviewQuery.isLoading}
        hasMetrics={hasMetrics}
        onFilter={(next) => setStatus(next)}
      />

      <div className="space-y-3">
        {/* 状态分段：窄屏横向滚动，不折行 */}
        <div className="-mx-1 overflow-x-auto px-1 pb-0.5 [scrollbar-width:none]">
          <div role="tablist" aria-label="按状态筛选" className="inline-flex min-w-max items-center gap-1 rounded-xl border bg-muted/40 p-1">
            {segments.map((segment) => {
              const active = status === segment.key;
              return (
                <button
                  key={segment.key}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  onClick={() => setStatus(segment.key)}
                  className={cn(
                    "inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-xs font-medium whitespace-nowrap transition-colors",
                    active ? "bg-background text-foreground shadow-sm ring-1 ring-border" : "text-muted-foreground hover:text-foreground",
                    !active && segment.value === 0 && segment.key !== "all" && "opacity-60"
                  )}
                >
                  {segment.dot ? <span className={cn("size-1.5 rounded-full", segment.dot)} /> : null}
                  {segment.label}
                  <span className={cn("rounded-md px-1 tabular-nums", active ? "bg-muted" : "")}>{segment.value}</span>
                </button>
              );
            })}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-0 flex-1 basis-56 sm:max-w-80">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              placeholder="搜索应用名称、ID 或 AppKey"
              className="h-9 pr-8 pl-8 text-sm"
            />
            {keyword && (
              <button
                type="button"
                aria-label="清除搜索"
                onClick={() => setKeyword("")}
                className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3.5" />
              </button>
            )}
          </div>
          <Select value={sort} onValueChange={(value) => setSort(value as SortKey)}>
            <SelectTrigger className="h-9 w-36 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(SORT_LABEL) as SortKey[])
                .filter((key) => hasMetrics || !METRIC_SORTS.includes(key))
                .map((key) => (
                  <SelectItem key={key} value={key}>{SORT_LABEL[key]}</SelectItem>
                ))}
            </SelectContent>
          </Select>
          <span className="hidden text-xs text-muted-foreground tabular-nums sm:inline">
            {filtered ? `筛选出 ${rows.length} / ${apps.length}` : `共 ${apps.length} 个应用`}
          </span>
          <div className="ml-auto hidden md:block">
            <ViewToggle value={listView} onChange={setListView} />
          </div>
        </div>
      </div>

      {appsQuery.isLoading ? (
        <AppListSkeleton view={listView} hasMetrics={canReadOverview} />
      ) : appsQuery.isError ? (
        <EmptyBlock
          icon={<AppWindow className="size-5" />}
          title="应用列表加载失败"
          description="请检查网络或稍后重试。"
          action={<Button size="sm" variant="outline" className="h-8" onClick={() => void appsQuery.refetch()}>重新加载</Button>}
        />
      ) : apps.length === 0 ? (
        <EmptyBlock
          icon={<AppWindow className="size-5" />}
          title="还没有应用"
          description="创建第一个应用后，即可配置登录方式、接入密钥与各项服务。"
          action={createDialog}
        />
      ) : rows.length === 0 ? (
        <EmptyBlock
          icon={<SearchX className="size-5" />}
          title="没有匹配的应用"
          description="调整搜索词或状态筛选后重试。"
          action={
            <Button size="sm" variant="outline" className="h-8" onClick={() => { setKeyword(""); setStatus("all"); }}>
              清除筛选
            </Button>
          }
        />
      ) : listView === "grid" ? (
        <AppCardGrid rows={rows} onDelete={setDeleteTarget} hasMetrics={hasMetrics} />
      ) : (
        <>
          <AppCardGrid rows={rows} onDelete={setDeleteTarget} hasMetrics={hasMetrics} className="md:hidden" />
          <AppTable rows={rows} onDelete={setDeleteTarget} hasMetrics={hasMetrics} className="hidden md:block" />
        </>
      )}

      {deleteTarget && (
        <AppDeleteDialog
          open
          onOpenChange={(open) => !open && setDeleteTarget(null)}
          appName={deleteTarget.name}
          onConfirm={handleDelete}
          isPending={deleteMutation.isPending}
        />
      )}
    </div>
  );
}

function ViewToggle({ value, onChange }: { value: AppListView; onChange: (view: AppListView) => void }) {
  return (
    <div className="flex items-center gap-0.5 rounded-lg border bg-muted/50 p-0.5">
      {([
        { key: "grid", label: "卡片", icon: LayoutGrid },
        { key: "table", label: "表格", icon: Rows3 }
      ] as const).map(({ key, label, icon: Icon }) => (
        <button
          key={key}
          type="button"
          title={label}
          aria-pressed={value === key}
          onClick={() => onChange(key)}
          className={cn(
            "inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs transition-colors",
            value === key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
          )}
        >
          <Icon className="size-3.5" />
          {label}
        </button>
      ))}
    </div>
  );
}

function EmptyBlock({
  icon,
  title,
  description,
  action
}: {
  icon: ReactNode;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex min-h-64 flex-col items-center justify-center gap-3 rounded-2xl border border-dashed bg-card/50 px-6 py-10 text-center">
      <span className="grid size-11 place-items-center rounded-2xl bg-muted text-muted-foreground">{icon}</span>
      <div className="space-y-1">
        <p className="text-sm font-semibold">{title}</p>
        <p className="max-w-sm text-xs leading-5 text-muted-foreground">{description}</p>
      </div>
      {action}
    </div>
  );
}

export default function AppsPage() {
  return (
    <Suspense>
      <AppsPageInner />
    </Suspense>
  );
}
