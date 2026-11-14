"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Activity,
  BarChart3,
  Database,
  FileText,
  FolderLock,
  Gauge,
  Globe,
  Layers,
  RefreshCw,
  Shield,
  Truck
} from "lucide-react";
import { SectionHeading } from "@/components/ui/section-heading";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FileManagerPanel, type StorageConfigOption } from "@/components/storage/file-manager";
import { StorageCDNPanel } from "@/components/storage/storage-cdn-panel";
import { StorageEventsPanel } from "@/components/storage/storage-events-panel";
import { StorageMigrationsPanel } from "@/components/storage/storage-migrations-panel";
import { StorageOverviewPanel } from "@/components/storage/storage-overview-panel";
import { StoragePoolsPanel } from "@/components/storage/storage-pools-panel";
import { StorageRulesPanel } from "@/components/storage/storage-rules-panel";
import { providerName } from "@/components/storage/storage-shared";
import { StorageSourcesPanel } from "@/components/storage/storage-sources-panel";
import { StorageUsagePanel } from "@/components/storage/storage-usage-panel";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { usePermissionChecker } from "@/lib/permissions";
import { useStorageOverviewQuery, useStorageProvidersQuery, useStorageSourcesQuery } from "@/lib/storage-center-hooks";
import { cn } from "@/lib/utils";

/**
 * 存储中心。
 *
 * `?tab=` 的键沿用旧页面（global / app / files / rules / cdn / usage），已分享的深链不失效；
 * 新增 overview（默认）/ pools / migrations / events。
 */
const TABS = [
  { value: "overview", label: "概览", icon: Gauge },
  { value: "global", label: "存储源", icon: Database },
  { value: "pools", label: "存储池", icon: Layers },
  { value: "migrations", label: "迁移", icon: Truck },
  { value: "events", label: "运行事件", icon: Activity },
  { value: "app", label: "应用存储", icon: FolderLock },
  { value: "files", label: "文件管理", icon: FileText },
  { value: "rules", label: "上传规则", icon: Shield },
  { value: "cdn", label: "CDN 与安全", icon: Globe },
  { value: "usage", label: "用量", icon: BarChart3 }
] as const;

function StoragePageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const requested = searchParams.get("tab");
  const tab = TABS.some((item) => item.value === requested) ? (requested as string) : "overview";
  const navigate = (next: string) => router.replace(`/storage?tab=${next}`, { scroll: false });

  const providers = useStorageProvidersQuery().data;
  const overview = useStorageOverviewQuery({
    live: tab === "overview" || tab === "pools"
  });
  const globalSources = useStorageSourcesQuery("global");

  const apps = useAdminAppsQuery().data ?? [];
  const [pickedAppId, setPickedAppId] = useState<number | null>(null);
  // `?app=<appKey>` 来自应用详情页的「存储」入口：落在同一个应用上。
  const scopedAppKey = searchParams.get("app");
  const scopedApp = scopedAppKey ? apps.find((item) => item.appKey === scopedAppKey) : undefined;
  const selectedAppId = pickedAppId ?? scopedApp?.id ?? apps[0]?.id ?? null;
  const appSources = useStorageSourcesQuery("app", selectedAppId);

  const sources = useMemo(() => globalSources.data ?? [], [globalSources.data]);
  const allSources = useMemo(() => [...sources, ...(appSources.data ?? [])], [sources, appSources.data]);
  const fileOptions = useMemo<StorageConfigOption[]>(
    () =>
      allSources.map((source) => ({
        id: source.id,
        label: `${source.config_name}（${providerName(providers, source.provider)}）`,
        provider: source.provider
      })),
    [allSources, providers]
  );
  // 永久删除与回收站清理对应 system:storage:purge，前端据此隐藏按钮。
  const can = usePermissionChecker();
  const canPurge = can("system:storage:purge");
  const unhealthy = overview.data?.totals.unhealthy ?? 0;

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="控制台"
        title="存储中心"
        action={
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              void overview.refetch();
              void globalSources.refetch();
            }}
            disabled={overview.isFetching}
          >
            <RefreshCw className={cn("size-3.5", overview.isFetching && "animate-spin")} />
            刷新
          </Button>
        }
      />

      <Tabs value={tab} onValueChange={navigate} className="space-y-5">
        <TabsList className="h-auto flex-wrap justify-start">
          {TABS.map((item) => {
            const Icon = item.icon;
            return (
              <TabsTrigger key={item.value} value={item.value} className="gap-1.5">
                <Icon className="size-3.5" />
                {item.label}
                {item.value === "overview" && unhealthy > 0 ? (
                  <span className="grid min-w-4 place-items-center rounded-full bg-red-500 px-1 text-[10px] font-semibold leading-4 text-white">
                    {unhealthy}
                  </span>
                ) : null}
              </TabsTrigger>
            );
          })}
        </TabsList>

        <TabsContent value="overview">
          <StorageOverviewPanel overview={overview.data} loading={overview.isLoading} onNavigate={navigate} />
        </TabsContent>

        <TabsContent value="global">
          <StorageSourcesPanel
            scope="global"
            sources={sources}
            overviews={overview.data?.configs}
            pools={overview.data?.pools}
            loading={globalSources.isLoading}
          />
        </TabsContent>

        <TabsContent value="pools">
          <StoragePoolsPanel pools={overview.data?.pools ?? []} sources={sources} loading={overview.isLoading} />
        </TabsContent>

        <TabsContent value="migrations">
          <StorageMigrationsPanel
            sources={sources}
            overviews={overview.data?.configs ?? []}
            pools={overview.data?.pools ?? []}
            onAddSource={() => navigate("global")}
          />
        </TabsContent>

        <TabsContent value="events">
          <StorageEventsPanel pools={overview.data?.pools ?? []} sources={sources} />
        </TabsContent>

        <TabsContent value="app">
          <div className="space-y-4">
            <Select value={selectedAppId ? String(selectedAppId) : ""} onValueChange={(value) => setPickedAppId(Number(value))}>
              <SelectTrigger className="w-60">
                <SelectValue placeholder="选择应用" />
              </SelectTrigger>
              <SelectContent>
                {apps.map((app) => (
                  <SelectItem key={app.id} value={String(app.id)}>
                    {app.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {selectedAppId ? (
              <StorageSourcesPanel
                scope="app"
                appid={selectedAppId}
                sources={appSources.data ?? []}
                loading={appSources.isLoading}
              />
            ) : (
              <div className="rounded-2xl border border-dashed py-12 text-center text-xs text-muted-foreground">暂无应用</div>
            )}
          </div>
        </TabsContent>

        <TabsContent value="files">
          <FileManagerPanel configs={fileOptions} canPurge={canPurge} />
        </TabsContent>

        <TabsContent value="rules">
          <StorageRulesPanel sources={allSources} />
        </TabsContent>

        <TabsContent value="cdn">
          <StorageCDNPanel sources={allSources} />
        </TabsContent>

        <TabsContent value="usage">
          <StorageUsagePanel sources={allSources} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default function StoragePage() {
  return (
    <Suspense>
      <StoragePageInner />
    </Suspense>
  );
}
