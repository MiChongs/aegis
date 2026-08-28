"use client";

import { PendingTasks, RecentActivity, SystemAlerts } from "@/components/dashboard/admin-workspace";
import { GreetingHero } from "@/components/dashboard/greeting-hero";
import { QuickActions } from "@/components/dashboard/quick-actions";
import { SystemPulse } from "@/components/dashboard/system-pulse";
import { AvailabilityDashboard } from "@/components/monitor/availability-dashboard";
import { CrashLogPanel } from "@/components/monitor/crash-log-panel";
import { MemoryPanel } from "@/components/monitor/memory-panel";
import { DatabasePanel } from "@/components/monitor/database-panel";
import { SystemInfoPanel } from "@/components/monitor/system-info-panel";
import { PlatformBannerCarousel } from "@/components/overview/platform-banner-carousel";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { useActivePlatformBannersQuery, useAdminAppsQuery } from "@/lib/admin-hooks";
import { useIsSuperAdmin } from "@/lib/permissions";
import { cn } from "@/lib/utils";

export default function OverviewPage() {
  const appsQuery = useAdminAppsQuery();
  const bannersQuery = useActivePlatformBannersQuery();
  const isSuperAdmin = useIsSuperAdmin();

  const hasBanners = (bannersQuery.data?.length ?? 0) > 0;

  return (
    <div className="space-y-6">
      {/* ── 首屏主视觉：全宽着色器问候区 + 平台指标 ── */}
      <GreetingHero />

      {/* ── 平台横幅：独立成行，无数据时整行不渲染 ── */}
      {hasBanners ? (
        <WidgetBoundary title="平台横幅加载失败">
          <div className="lg:h-[240px]">
            <PlatformBannerCarousel />
          </div>
        </WidgetBoundary>
      ) : null}

      {/* ── 快捷入口 ── */}
      <QuickActions />

      {/* ── 工作区：健康度 / 待办 / 操作流水 等高并排（流水占两栏容纳长路径）── */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {/* 监控接口要求超管，非超管挂载只会拿到 401 */}
        {isSuperAdmin ? (
          <WidgetBoundary title="系统脉搏加载失败">
            <SystemPulse />
          </WidgetBoundary>
        ) : null}
        <PendingTasks />
        {/* 没有系统脉搏时（非超管）让流水吃掉空出来的那一栏，避免整行留空 */}
        <div className={cn("md:col-span-2", !isSuperAdmin && "lg:col-span-3")}>
          <RecentActivity />
        </div>
      </div>

      <SystemAlerts />

      {isSuperAdmin && <AvailabilityDashboard mode="console" apps={appsQuery.data || []} />}
      {isSuperAdmin && <SystemInfoPanel />}
      {isSuperAdmin && <DatabasePanel />}
      {isSuperAdmin && <MemoryPanel />}
      {isSuperAdmin && <CrashLogPanel />}
    </div>
  );
}
