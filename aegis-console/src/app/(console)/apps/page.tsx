"use client";

import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { LayoutGrid, RotateCw, Rows3, Search, X } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { SectionHeading } from "@/components/ui/section-heading";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { AppCreateDialog } from "@/components/apps/app-create-dialog";
import { AppDeleteDialog } from "@/components/apps/app-delete-dialog";
import { AppCardGrid, AppListSkeleton, AppTable, type AppListRow } from "@/components/apps/app-list-views";
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
type SortKey = "created" | "name" | "id" | "users";

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
  const hasMetrics = canReadOverview && metricsByAppKey.size > 0;

  const listView = useAppScopeStore((s) => s.listView);
  const setListView = useAppScopeStore((s) => s.setListView);
  const lastAppKey = useAppScopeStore((s) => s.lastAppKey);

  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [sort, setSort] = useState<SortKey>("created");
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

    const sorted = [...filtered].sort((a, b) => {
      switch (sort) {
        case "name":
          return a.name.localeCompare(b.name, "zh-CN");
        case "id":
          return a.id - b.id;
        case "users":
          return (
            (metricsByAppKey.get(b.appKey)?.totalUsers ?? 0) - (metricsByAppKey.get(a.appKey)?.totalUsers ?? 0)
          );
        default:
          return new Date(b.createdAt ?? 0).getTime() - new Date(a.createdAt ?? 0).getTime();
      }
    });

    return sorted.map((app) => ({ app, metrics: metricsByAppKey.get(app.appKey) }));
  }, [apps, keyword, metricsByAppKey, sort, status]);

  const counts = useMemo(() => {
    let enabled = 0;
    let disabled = 0;
    let registerOff = 0;
    let loginOff = 0;
    let governed = 0;
    for (const app of apps) {
      if (app.status) enabled += 1;
      else disabled += 1;
      if (!app.registerStatus) registerOff += 1;
      if (!app.loginStatus) loginOff += 1;
      const metrics = metricsByAppKey.get(app.appKey);
      if (metrics && metrics.state !== "active") governed += 1;
    }
    return { total: apps.length, enabled, disabled, registerOff, loginOff, governed };
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

  if (redirecting) return <LoadingState title="打开应用配置" description="正在跳转到最近使用的应用..." />;

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
              onClick={() => void appsQuery.refetch()}
            >
              <RotateCw className={cn("size-3.5", appsQuery.isFetching && "animate-spin")} />
            </Button>
            <ViewToggle value={listView} onChange={setListView} />
            <AppCreateDialog onCreated={(app) => router.push(`/apps/${encodeURIComponent(app.appKey)}`)} />
          </div>
        }
      />

      {/* 汇总即筛选：数字本身可点，看到「2 个已停用」就能直接点进去看是哪两个 */}
      <div className="flex flex-wrap items-center gap-2">
        <FilterChip label="全部应用" value={counts.total} active={status === "all"} onClick={() => setStatus("all")} />
        <FilterChip label="已启用" value={counts.enabled} active={status === "enabled"} onClick={() => setStatus("enabled")} tone="success" />
        <FilterChip label="已停用" value={counts.disabled} active={status === "disabled"} onClick={() => setStatus("disabled")} tone="danger" />
        <FilterChip label="注册关闭" value={counts.registerOff} active={status === "register-off"} onClick={() => setStatus("register-off")} tone="warning" />
        <FilterChip label="登录关闭" value={counts.loginOff} active={status === "login-off"} onClick={() => setStatus("login-off")} tone="warning" />
        {hasMetrics && (
          <FilterChip label="被治理" value={counts.governed} active={status === "governed"} onClick={() => setStatus("governed")} tone="danger" />
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-56 flex-1 sm:max-w-80">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索应用名称、ID 或 AppKey"
            className="h-8 pl-8 text-sm"
          />
          {keyword && (
            <button
              type="button"
              onClick={() => setKeyword("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          )}
        </div>
        <Select value={sort} onValueChange={(value) => setSort(value as SortKey)}>
          <SelectTrigger className="h-8 w-36 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="created">最近创建</SelectItem>
            <SelectItem value="name">按名称</SelectItem>
            <SelectItem value="id">按应用 ID</SelectItem>
            {hasMetrics && <SelectItem value="users">按用户数</SelectItem>}
          </SelectContent>
        </Select>
        <span className="text-xs text-muted-foreground">
          {rows.length === apps.length ? `共 ${apps.length} 个应用` : `筛选出 ${rows.length} / ${apps.length}`}
        </span>
      </div>

      {appsQuery.isLoading ? (
        <AppListSkeleton view={listView} />
      ) : appsQuery.isError ? (
        <EmptyState title="应用列表加载失败" description="请检查后端服务状态后重试。" />
      ) : apps.length === 0 ? (
        <EmptyState title="还没有应用" description="创建第一个应用后，它的认证、支付、运营配置都会在这里。" />
      ) : rows.length === 0 ? (
        <EmptyState title="没有匹配的应用" description="调整搜索关键词或筛选条件后再试。" />
      ) : listView === "grid" ? (
        <AppCardGrid rows={rows} onDelete={setDeleteTarget} hasMetrics={hasMetrics} />
      ) : (
        <AppTable rows={rows} onDelete={setDeleteTarget} hasMetrics={hasMetrics} />
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
          onClick={() => onChange(key)}
          className={cn(
            "grid size-7 place-items-center rounded-md transition-colors",
            value === key ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
          )}
        >
          <Icon className="size-3.5" />
        </button>
      ))}
    </div>
  );
}

function FilterChip({
  label,
  value,
  active,
  tone,
  onClick
}: {
  label: string;
  value: number;
  active: boolean;
  tone?: "success" | "warning" | "danger";
  onClick: () => void;
}) {
  const muted = value === 0 && !active;
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-2 rounded-xl border px-3 py-1.5 text-left transition-colors",
        active ? "border-foreground/30 bg-accent" : "border-border bg-card hover:bg-accent/50",
        muted && "opacity-60"
      )}
    >
      <span
        className={cn(
          "text-sm font-semibold tabular-nums",
          value > 0 && tone === "success" && "text-emerald-600 dark:text-emerald-400",
          value > 0 && tone === "warning" && "text-amber-600 dark:text-amber-400",
          value > 0 && tone === "danger" && "text-red-600 dark:text-red-400"
        )}
      >
        {value}
      </span>
      <span className="text-xs text-muted-foreground">{label}</span>
    </button>
  );
}

export default function AppsPage() {
  return (
    <Suspense>
      <AppsPageInner />
    </Suspense>
  );
}
