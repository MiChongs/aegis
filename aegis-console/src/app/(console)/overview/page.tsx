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

  // 没有横幅时 carousel 内部返回 null，若仍保留两列栅格，右侧 60% 会空掉；
  // 这里按数据决定栅格，让问候区在无横幅时铺满整行
  const hasBanners = (bannersQuery.data?.length ?? 0) > 0;

  return (
    <div className="space-y-6">
      {/* ── 首屏主视觉：横幅（有则占左 40%）+ 问候与平台指标 ── */}
      {hasBanners ? (
        <div className="grid gap-6 lg:grid-cols-[2fr_3fr] lg:items-stretch">
          <WidgetBoundary title="平台横幅加载失败">
            <PlatformBannerCarousel />
          </WidgetBoundary>
          <GreetingHero />
        </div>
      ) : (
        <GreetingHero />
      )}

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
