"use client";

import { ActivityTrend } from "@/components/overview/activity-trend";
import { OverviewHero } from "@/components/overview/overview-hero";
import { AttentionPanel, RecentActivity } from "@/components/overview/overview-workspace";
import { PlatformBannerCarousel } from "@/components/overview/platform-banner-carousel";
import { PlatformPulse } from "@/components/overview/platform-pulse";
import { QuickActions } from "@/components/overview/quick-actions";
import { RuntimeSection } from "@/components/overview/runtime-section";
import { TopApps } from "@/components/overview/top-apps";
import { WidgetBoundary } from "@/components/ui/error-boundary";
import { useActivePlatformBannersQuery } from "@/lib/admin-hooks";
import { useIsSuperAdmin } from "@/lib/permissions";

/**
 * 概览。
 *
 * 手机上单列，顺序按「先看要处理的，再看数据」：待处理 → 趋势 → 平台状态 → 排行 → 最近操作。
 * 桌面三列：左两列放趋势与最近操作，右列依次是待处理、平台状态、排行；
 * 网格位置写死在 lg 断点上，DOM 顺序因此可以只照顾手机。
 * 趋势、平台状态、排行与系统运行的数据只有超管拿得到，其他角色只有待处理与最近操作两块并排。
 */
const SLOT = {
  trend: "lg:col-span-2 lg:col-start-1 lg:row-start-1",
  attention: "lg:col-start-3 lg:row-start-1",
  recent: "lg:col-span-2 lg:col-start-1 lg:row-span-2 lg:row-start-2",
  pulse: "lg:col-start-3 lg:row-start-2",
  topApps: "lg:col-start-3 lg:row-start-3"
} as const;

export default function OverviewPage() {
  const bannersQuery = useActivePlatformBannersQuery();
  const isSuperAdmin = useIsSuperAdmin();
  const hasBanners = (bannersQuery.data?.length ?? 0) > 0;

  return (
    <div className="mx-auto flex w-full max-w-[1600px] min-w-0 flex-col gap-4 lg:gap-5">
      <OverviewHero />

      {/* 平台横幅独立成行，无数据时整行不渲染 */}
      {hasBanners ? (
        <WidgetBoundary title="平台横幅加载失败">
          <div className="lg:h-[240px]">
            <PlatformBannerCarousel />
          </div>
        </WidgetBoundary>
      ) : null}

      {isSuperAdmin ? (
        <div className="grid min-w-0 gap-4 lg:grid-cols-3 lg:gap-5">
          {/* 出错时的占位也要占住同一格，否则后面几块会挤进空位 */}
          <WidgetBoundary title="待处理加载失败" className={SLOT.attention}>
            <AttentionPanel className={SLOT.attention} />
          </WidgetBoundary>
          <WidgetBoundary title="趋势加载失败" className={SLOT.trend}>
            <ActivityTrend className={SLOT.trend} />
          </WidgetBoundary>
          <WidgetBoundary title="平台状态加载失败" className={SLOT.pulse}>
            <PlatformPulse className={SLOT.pulse} />
          </WidgetBoundary>
          <WidgetBoundary title="应用排行加载失败" className={SLOT.topApps}>
            <TopApps className={SLOT.topApps} />
          </WidgetBoundary>
          <WidgetBoundary title="最近操作加载失败" className={SLOT.recent}>
            <RecentActivity className={SLOT.recent} />
          </WidgetBoundary>
        </div>
      ) : (
        <div className="grid min-w-0 gap-4 lg:grid-cols-3 lg:gap-5">
          <AttentionPanel />
          <RecentActivity className="lg:col-span-2" />
        </div>
      )}

      <QuickActions />

      {/* 运行信息接口要求超管，非超管挂载只会拿到 401 */}
      {isSuperAdmin ? <RuntimeSection /> : null}
    </div>
  );
}
