"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { GalleryHorizontalEnd, Megaphone, MousePointerClick, Pin, Radio, Server } from "lucide-react";
import { AnnouncementsPanel } from "./announcements-panel";
import { BannerPanel } from "@/components/content/banner-panel";
import { NoticePanel } from "@/components/content/notice-panel";
import { StatTile, clickRate, formatCount, formatDateTime } from "@/components/content/content-shared";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAdminAppsQuery } from "@/lib/admin-hooks";
import { useAppScopeStore } from "@/lib/app-scope-store";
import { useContentOverviewQuery } from "@/lib/content-hooks";

/**
 * 内容中心。
 *
 * 三个页签的作用域不同，这是页面上唯一需要先讲清楚的事：
 *   - Banner / 公告 属于**当前选中的应用**，改的是那个应用的客户端会看到什么
 *   - 系统公告是**平台级**广播，发给控制台的管理员，与应用选择器无关
 *
 * 与 `/platform-banners` 的分工也是作用域：那边是画在控制台总览页的平台横幅，
 * 只有超管能改；这边是画在应用客户端里的素材，由应用管理员维护。
 */
const TABS = ["banners", "notices", "announcements"] as const;
type ContentTab = (typeof TABS)[number];

function resolveTab(value?: string | null): ContentTab {
  return TABS.includes(value as ContentTab) ? (value as ContentTab) : "banners";
}

function ContentPageInner() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const tab = resolveTab(searchParams.get("tab"));

  const appsQuery = useAdminAppsQuery();
  const apps = useMemo(() => appsQuery.data ?? [], [appsQuery.data]);
  const { lastAppKey, setLastAppKey } = useAppScopeStore();

  // `?app=<appKey>` 来自应用详情页的「内容」入口：从那里跳过来必须落在同一个应用上，
  // 否则跨页之后作用域悄悄换成了别的应用，改错对象都不会有任何提示。
  const scopedAppKey = searchParams.get("app");
  const [picked, setPicked] = useState<string | null>(scopedAppKey ?? lastAppKey);

  // 当前应用是**派生**的，不用 effect 回填：选中的还在列表里就用它，否则落到第一个。
  // 用 effect 同步会在应用列表到位的那一帧多渲染一次，而这一帧里所有面板
  // 都会拿着空 appId 发一轮请求。
  const appKey = useMemo(() => {
    if (picked && apps.some((item) => item.appKey === picked)) return picked;
    return apps[0]?.appKey ?? "";
  }, [picked, apps]);
  const currentApp = useMemo(() => apps.find((item) => item.appKey === appKey), [apps, appKey]);
  const appId = currentApp?.id ?? null;

  const overviewQuery = useContentOverviewQuery(appId);

  function switchApp(value: string) {
    setPicked(value);
    setLastAppKey(value);
  }

  if (appsQuery.isLoading) {
    return <LoadingState title="内容中心" description="正在读取应用列表" />;
  }

  if (!apps.length) {
    return (
      <div className="page-stack">
        <SectionHeading eyebrow="控制台" title="内容中心" />
        <EmptyState title="还没有应用" description="先创建一个应用，才能给它投放 Banner 与公告。" />
      </div>
    );
  }

  return (
    <div className="page-stack">
      <SectionHeading
        eyebrow="控制台"
        title="内容中心"
        action={
          <Select value={appKey} onValueChange={switchApp}>
            <SelectTrigger className="h-8 w-48 text-xs">
              <SelectValue placeholder="选择应用" />
            </SelectTrigger>
            <SelectContent>
              {apps.map((item) => (
                <SelectItem key={item.appKey ?? item.id} value={item.appKey || String(item.id)}>
                  {item.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
      />

      <ContentOverviewStrip
        loading={overviewQuery.isLoading}
        data={overviewQuery.data}
      />

      <Tabs value={tab} onValueChange={(value) => router.replace(`/content?tab=${value}`, { scroll: false })}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="banners">
            <GalleryHorizontalEnd className="size-3.5" />
            Banner
          </TabsTrigger>
          <TabsTrigger value="notices">
            <Megaphone className="size-3.5" />
            应用公告
          </TabsTrigger>
          <TabsTrigger value="announcements">
            <Server className="size-3.5" />
            系统公告
          </TabsTrigger>
        </TabsList>

        <TabsContent value="banners" className="mt-4">
          <BannerPanel appId={appId} />
        </TabsContent>

        <TabsContent value="notices" className="mt-4">
          <NoticePanel appId={appId} />
        </TabsContent>

        <TabsContent value="announcements" className="mt-4 space-y-3">
          <p className="text-xs text-muted-foreground">
            平台级广播，发给控制台的全体管理员，不随上方应用切换
          </p>
          <AnnouncementsPanel />
        </TabsContent>
      </Tabs>
    </div>
  );
}

/**
 * 总览条。
 *
 * 六格里有四格是**结论**而不是原始计数：「投放中」不等于「已启用」——
 * 一条启用但结束时间已过的 Banner，开关是开的，客户端上什么都没有。
 * 不把这个差额直接说出来，管理员就要自己拿三个字段和当前时间做心算。
 */
function ContentOverviewStrip({
  loading,
  data
}: {
  loading: boolean;
  data?: import("@/lib/api/types").ContentOverview;
}) {
  if (loading) {
    return (
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {[0, 1, 2, 3].map((key) => (
          <Skeleton key={key} className="h-[86px] rounded-xl" />
        ))}
      </div>
    );
  }
  if (!data) return null;

  const idle = data.bannerScheduled + data.bannerExpired + data.bannerDisabled;
  const rate = clickRate(data.bannerViews, data.bannerClicks);

  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <StatTile
        label="投放中的 Banner"
        icon={Radio}
        value={data.bannerLive}
        tone={data.bannerLive > 0 ? "positive" : "muted"}
        hint={idle > 0 ? `另有 ${idle} 条未在投放` : `共 ${data.bannerTotal} 条`}
      />
      <StatTile
        label="曝光与点击"
        icon={MousePointerClick}
        value={formatCount(data.bannerViews)}
        hint={rate ? `点击 ${formatCount(data.bannerClicks)} · 点击率 ${rate}` : "还没有曝光"}
      />
      <StatTile
        label="已发布公告"
        icon={Megaphone}
        value={data.noticePublished}
        hint={
          data.noticeDraft > 0
            ? `${data.noticeDraft} 条草稿待发布`
            : data.lastPublishedAt
              ? `最近发布 ${formatDateTime(data.lastPublishedAt)}`
              : "暂无发布记录"
        }
      />
      <StatTile
        label="公告阅读"
        icon={Pin}
        value={formatCount(data.noticeViews)}
        hint={data.noticePinned > 0 ? `${data.noticePinned} 条置顶中` : `归档 ${data.noticeArchived} 条`}
      />
    </div>
  );
}

// `useSearchParams()` 必须处在 Suspense 边界内，否则 next build 报错、整页退化为客户端渲染
export default function ContentPage() {
  return (
    <Suspense fallback={<LoadingState title="内容中心" description="正在读取应用列表" />}>
      <ContentPageInner />
    </Suspense>
  );
}
