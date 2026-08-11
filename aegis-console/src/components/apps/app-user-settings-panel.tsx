"use client";

import { useState } from "react";
import {
  BrushCleaning,
  ClipboardCheck,
  Loader2,
  PlayCircle,
  Search,
  SlidersHorizontal,
  TriangleAlert,
  UserCog,
  Wrench
} from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api-client";
import { cn } from "@/lib/utils";
import type { SettingsCleanupResult, SettingsIntegrityResult } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import {
  useAdminUserSettingsQuery,
  useAdminUserSettingsStatsQuery,
  useBatchInitializeUserSettingsMutation,
  useCheckUserSettingsIntegrityMutation,
  useCleanupUserSettingsMutation,
  useInitializeUserSettingsMutation
} from "@/lib/admin-hooks";
import { NoAppSelected, SectionCard } from "@/components/apps/app-config-primitives";

/**
 * 用户默认设置的覆盖率与运维动作。
 *
 * 旧实现把 stats、单用户设置、完整性检查结果统统 `JSON.stringify` 打在 `<pre>` 里，
 * 于是「覆盖率 0%」与「这个应用还没有用户」长得一模一样，
 * 完整性检查点完只能靠 toast 猜结果。这里按数据的实际结构渲染。
 */
export function AppUserSettingsPanel({ appId }: { appId?: number | null }) {
  const statsQuery = useAdminUserSettingsStatsQuery(appId);
  const batchInitMutation = useBatchInitializeUserSettingsMutation();
  const integrityMutation = useCheckUserSettingsIntegrityMutation();
  const cleanupMutation = useCleanupUserSettingsMutation();

  const [integrityResult, setIntegrityResult] = useState<SettingsIntegrityResult | null>(null);
  const [cleanupResult, setCleanupResult] = useState<SettingsCleanupResult | null>(null);

  const stats = statsQuery.data;
  const categories = Object.entries(stats?.settingsStats ?? {});

  async function handleBatchInit() {
    if (!appId) return;
    try {
      const result = await batchInitMutation.mutateAsync({ appid: appId });
      toast.success(
        `批量初始化完成：处理 ${result.processedUsers} 个用户，新建 ${result.initializedCategories} 项，跳过已存在 ${result.skippedExisting} 项`
      );
      await statsQuery.refetch();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "初始化失败");
    }
  }

  async function handleIntegrity(autoRepair: boolean) {
    if (!appId) return;
    try {
      const result = await integrityMutation.mutateAsync({ appid: appId, autoRepair });
      setIntegrityResult(result);
      if (result.totalIssues === 0) {
        toast.success("完整性检查通过，没有缺失的默认键");
      } else if (autoRepair) {
        toast.success(`已修复 ${result.repairs.length} 条记录`);
      } else {
        toast.warning(`发现 ${result.totalIssues} 条记录缺少默认键`);
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "检查失败");
    }
  }

  async function handleCleanup(dryRun: boolean) {
    if (!appId) return;
    try {
      const result = await cleanupMutation.mutateAsync({ appid: appId, dryRun });
      setCleanupResult(result);
      if (result.foundInvalid === 0) {
        toast.success("没有发现失效记录");
      } else if (dryRun) {
        toast.warning(`预检发现 ${result.foundInvalid} 条失效记录，执行清理才会真正删除`);
      } else {
        toast.success(`已清理 ${result.cleaned} 条失效记录`);
      }
      await statsQuery.refetch();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "清理失败");
    }
  }

  if (!appId) return <NoAppSelected icon={<SlidersHorizontal className="size-8" />} />;

  return (
    <div className="grid gap-5 xl:grid-cols-[1.1fr_0.9fr] xl:items-start">
      <div className="space-y-5">
        <SectionCard
          icon={<SlidersHorizontal className="size-4" />}
          title="设置覆盖率"
          description="每个设置分类有多少用户已经落库。缺失的分类会在用户首次读取时按默认值补齐。"
          aside={
            stats ? (
              <Badge variant="outline" className="text-[10px]">
                {stats.summary?.totalCategories ?? categories.length} 个分类 · 平均 {Math.round(stats.summary?.avgCoverage ?? 0)}%
              </Badge>
            ) : null
          }
        >
          {statsQuery.isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-10 w-full rounded-xl" />
              <Skeleton className="h-10 w-full rounded-xl" />
              <Skeleton className="h-10 w-full rounded-xl" />
            </div>
          ) : !stats || stats.totalUsers === 0 ? (
            <div className="rounded-xl bg-muted p-4 text-center text-xs text-muted-foreground">
              该应用还没有用户，暂无设置数据。
            </div>
          ) : (
            <div className="space-y-4">
              <div className="rounded-xl bg-muted px-3 py-2.5">
                <div className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">总用户</div>
                <div className="mt-0.5 font-mono text-lg tabular-nums leading-none">{stats.totalUsers}</div>
              </div>
              <div className="space-y-3">
                {categories.length === 0 ? (
                  <p className="text-[11px] text-muted-foreground">尚未定义任何设置分类。</p>
                ) : (
                  categories.map(([category, stat]) => (
                    <div key={category} className="space-y-1.5">
                      <div className="flex items-baseline justify-between gap-2 text-xs">
                        <span className="truncate font-medium">{category}</span>
                        <span className="shrink-0 font-mono tabular-nums text-muted-foreground">
                          {stat.usersWithSettings} / {stat.usersWithSettings + stat.usersWithoutSettings}
                          <span
                            className={cn(
                              "ml-1.5",
                              stat.coverage >= 90
                                ? "text-emerald-600 dark:text-emerald-400"
                                : stat.coverage >= 50
                                  ? "text-amber-600 dark:text-amber-400"
                                  : "text-destructive"
                            )}
                          >
                            {Math.round(stat.coverage)}%
                          </span>
                        </span>
                      </div>
                      <Progress value={stat.coverage} className="h-1.5" />
                    </div>
                  ))
                )}
              </div>
            </div>
          )}
        </SectionCard>

        <SectionCard
          icon={<Wrench className="size-4" />}
          title="运维动作"
          description="批量补齐、完整性校验与失效记录清理。"
        >
          <div className="space-y-4">
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" disabled={batchInitMutation.isPending} onClick={handleBatchInit}>
                {batchInitMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <PlayCircle className="size-3.5" />}
                批量初始化
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={integrityMutation.isPending}
                onClick={() => handleIntegrity(false)}
              >
                {integrityMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <ClipboardCheck className="size-3.5" />}
                检查完整性
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={cleanupMutation.isPending}
                onClick={() => handleCleanup(true)}
              >
                {cleanupMutation.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Search className="size-3.5" />}
                清理预检
              </Button>
            </div>

            {integrityResult ? (
              <div className="space-y-2 rounded-xl border border-border bg-muted/30 p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium">
                    完整性检查：{integrityResult.totalIssues === 0 ? "全部通过" : `${integrityResult.totalIssues} 条缺键`}
                  </span>
                  {integrityResult.totalIssues > 0 && !integrityResult.autoRepair ? (
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 text-[11px]"
                      disabled={integrityMutation.isPending}
                      onClick={() => handleIntegrity(true)}
                    >
                      <Wrench className="size-3" />
                      自动修复
                    </Button>
                  ) : null}
                </div>
                {integrityResult.issues.length > 0 && (
                  <ScrollArea className="max-h-40">
                    <ul className="space-y-1.5 pr-3">
                      {integrityResult.issues.map((issue) => (
                        <li key={`${issue.userId}-${issue.category}`} className="text-[11px]">
                          <span className="font-mono">#{issue.userId}</span>
                          <span className="mx-1.5 text-muted-foreground">·</span>
                          <span>{issue.category}</span>
                          <span className="ml-1.5 text-muted-foreground">缺 {issue.missingKeys.join("、")}</span>
                        </li>
                      ))}
                    </ul>
                  </ScrollArea>
                )}
                {integrityResult.repairs.length > 0 && (
                  <p className="text-[10px] text-emerald-600 dark:text-emerald-400">
                    已修复 {integrityResult.repairs.length} 条记录
                  </p>
                )}
              </div>
            ) : null}

            {cleanupResult ? (
              <div className="space-y-2 rounded-xl border border-border bg-muted/30 p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium">
                    {cleanupResult.dryRun ? "清理预检" : "清理结果"}：发现 {cleanupResult.foundInvalid} 条
                    {cleanupResult.dryRun ? "" : `，已删除 ${cleanupResult.cleaned} 条`}
                  </span>
                  {cleanupResult.dryRun && cleanupResult.foundInvalid > 0 ? (
                    <AlertDialog>
                      <AlertDialogTrigger asChild>
                        <Button size="sm" variant="outline" className="h-7 text-[11px]">
                          <BrushCleaning className="size-3" />
                          执行清理
                        </Button>
                      </AlertDialogTrigger>
                      <AlertDialogContent>
                        <AlertDialogHeader>
                          <AlertDialogTitle>删除 {cleanupResult.foundInvalid} 条失效设置记录？</AlertDialogTitle>
                          <AlertDialogDescription>
                            这些记录已被标记失效，删除后不可恢复。用户下次读取设置时会按默认值重新落库。
                          </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel>取消</AlertDialogCancel>
                          <AlertDialogAction
                            className="bg-destructive text-white hover:bg-destructive/90"
                            onClick={() => handleCleanup(false)}
                          >
                            确认删除
                          </AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                  ) : null}
                </div>
                {cleanupResult.dryRun && cleanupResult.foundInvalid > 0 ? (
                  <p className="flex items-center gap-1.5 text-[10px] text-amber-600 dark:text-amber-400">
                    <TriangleAlert className="size-3" />
                    预检不会删除任何数据
                  </p>
                ) : null}
              </div>
            ) : null}
          </div>
        </SectionCard>
      </div>

      <SingleUserCard appId={appId} />
    </div>
  );
}

/**
 * 分类设置的键值渲染。
 *
 * 刻意不用 JsonViewer —— 它基于 Monaco，会把整个编辑器拖进应用配置页
 * （见 aegis-console/CLAUDE.md 对 Monaco 预热的约束）。设置项都是扁平的
 * 标量与短数组，键值表比 JSON 更好读，也不需要编辑能力。
 */
function SettingsKeyValues({ values }: { values: Record<string, unknown> }) {
  const entries = Object.entries(values ?? {});
  if (entries.length === 0) {
    return <p className="rounded-lg bg-muted px-2.5 py-2 text-[10px] text-muted-foreground">（空）</p>;
  }
  return (
    <dl className="divide-y divide-border overflow-hidden rounded-lg bg-muted">
      {entries.map(([key, value]) => (
        <div key={key} className="flex items-baseline justify-between gap-3 px-2.5 py-1.5">
          <dt className="shrink-0 font-mono text-[10px] text-muted-foreground">{key}</dt>
          <dd className="min-w-0 truncate text-right font-mono text-[10px]">{formatSettingValue(value)}</dd>
        </div>
      ))}
    </dl>
  );
}

function formatSettingValue(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "boolean") return value ? "开" : "关";
  if (typeof value === "number" || typeof value === "string") return String(value);
  if (Array.isArray(value)) return value.length === 0 ? "[]" : value.map((v) => String(v)).join(", ");
  return JSON.stringify(value);
}

function SingleUserCard({ appId }: { appId: number }) {
  const [input, setInput] = useState("");
  const [userId, setUserId] = useState<number | null>(null);
  const settingsQuery = useAdminUserSettingsQuery(appId, userId);
  const initMutation = useInitializeUserSettingsMutation();

  const parsed = Number(input.trim());
  const canQuery = Boolean(input.trim()) && Number.isFinite(parsed) && parsed > 0;
  const view = settingsQuery.data;

  async function handleInit() {
    if (!userId) return;
    try {
      const result = await initMutation.mutateAsync({ appid: appId, userId });
      toast.success(`已为用户 ${userId} 初始化 ${result.initializedCategories} 项设置`);
      await settingsQuery.refetch();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "初始化失败");
    }
  }

  return (
    <SectionCard
      icon={<UserCog className="size-4" />}
      title="单用户设置"
      description="按用户 ID 查看各分类的实际取值与版本。"
    >
      <div className="space-y-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="输入用户 ID"
            className="sm:max-w-52"
            onKeyDown={(e) => {
              if (e.key === "Enter" && canQuery) setUserId(parsed);
            }}
          />
          <Button size="sm" variant="outline" disabled={!canQuery} onClick={() => setUserId(parsed)}>
            <Search className="size-3.5" />
            查询
          </Button>
        </div>

        {userId === null ? (
          <div className="rounded-xl bg-muted p-4 text-center text-xs text-muted-foreground">
            输入用户 ID 后可查看该用户的分类设置。
          </div>
        ) : settingsQuery.isLoading ? (
          <Skeleton className="h-64 w-full rounded-xl" />
        ) : !view ? (
          <div className="rounded-xl bg-muted p-4 text-center text-xs text-muted-foreground">未找到该用户。</div>
        ) : (
          <div className="space-y-4">
            <div className="flex items-center justify-between gap-3 rounded-xl bg-muted px-3 py-2.5">
              <div className="min-w-0">
                <div className="truncate text-xs font-medium">{view.user?.nickname || view.user?.account}</div>
                <div className="truncate font-mono text-[10px] text-muted-foreground">
                  #{view.user?.id} {view.user?.email ? `· ${view.user.email}` : ""}
                </div>
              </div>
              {view.missingCategories?.length ? (
                <Button size="sm" variant="outline" className="h-7 shrink-0 text-[11px]" disabled={initMutation.isPending} onClick={handleInit}>
                  {initMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : <PlayCircle className="size-3" />}
                  补齐 {view.missingCategories.length} 项
                </Button>
              ) : (
                <Badge variant="success" className="shrink-0 text-[9px]">分类完整</Badge>
              )}
            </div>

            {view.missingCategories?.length ? (
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-[10px] text-muted-foreground">缺失分类：</span>
                {view.missingCategories.map((c) => (
                  <Badge key={c} variant="outline" className="text-[9px]">{c}</Badge>
                ))}
              </div>
            ) : null}

            <ScrollArea className="h-96 rounded-xl border border-border">
              <div className="space-y-3 p-3">
                {Object.entries(view.settings ?? {}).map(([category, record]) => (
                  <div key={category} className="space-y-1.5">
                    <div className="flex items-baseline justify-between gap-2">
                      <span className="text-xs font-medium">{category}</span>
                      <span className="font-mono text-[10px] text-muted-foreground">v{record.version}</span>
                    </div>
                    <SettingsKeyValues values={record.settings} />
                  </div>
                ))}
                {Object.keys(view.settings ?? {}).length === 0 ? (
                  <p className="py-6 text-center text-[11px] text-muted-foreground">该用户还没有任何设置记录。</p>
                ) : null}
              </div>
            </ScrollArea>
          </div>
        )}
      </div>
    </SectionCard>
  );
}
