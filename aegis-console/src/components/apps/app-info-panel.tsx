"use client";

import { Info } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { InfoAccess } from "@/components/apps/info/info-access";
import { InfoAuthSources, InfoRecentLogins } from "@/components/apps/info/info-insights";
import { InfoOverview } from "@/components/apps/info/info-overview";
import { InfoAssets, InfoDangerZone, InfoIdentity, InfoShortcuts } from "@/components/apps/info/info-side";
import { useAdminAppQuery } from "@/lib/admin-hooks";

/**
 * 应用「基本信息」：进入一个应用后的首页。
 *
 * 从上到下回答四件事，分区各自成卡：
 *
 * 1. 运行概况 —— 多大、今天活不活、最近在不在涨（通栏）；
 * 2. 名称与访问 —— 唯一能在这里改的东西：名称与三项开关，带客户端提示预览；
 * 3. 登录方式 / 最近登录 —— 用户靠什么进来、刚才谁进来了（排查登录问题的第一站）；
 * 4. 右栏 —— 接入标识、内容与绑定、常用配置、危险操作。
 *
 * 分栏跟随面板自身宽度（@container）而不是视口：控制台侧栏展开与收起时，
 * 同一视口下面板宽度能差三百多像素，按视口断点排会在窄的那种情况下挤坏。
 */
export function AppInfoPanel({ appKey, onDelete }: { appKey?: string | null; onDelete?: () => void }) {
  const query = useAdminAppQuery(appKey);
  const app = query.data;

  if (!appKey) return <NoAppSelected icon={<Info className="size-8" />} />;
  if (query.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-56 w-full rounded-2xl" />
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
          <Skeleton className="h-80 w-full rounded-2xl" />
          <Skeleton className="h-80 w-full rounded-2xl" />
        </div>
      </div>
    );
  }
  if (!app) return <div className="py-12 text-center text-sm text-muted-foreground">应用不存在</div>;

  return (
    <div className="@container/info min-w-0 space-y-4">
      <InfoOverview appKey={app.appKey} />

      <div className="grid min-w-0 items-start gap-4 @5xl/info:grid-cols-[minmax(0,1fr)_20rem] @7xl/info:grid-cols-[minmax(0,1fr)_23rem]">
        <div className="min-w-0 space-y-4">
          <InfoAccess app={app} />
          <div className="grid min-w-0 items-start gap-4 @3xl/info:grid-cols-2">
            <InfoAuthSources appKey={app.appKey} />
            <InfoRecentLogins appKey={app.appKey} />
          </div>
        </div>

        <aside className="grid min-w-0 content-start gap-4 @3xl/info:grid-cols-2 @5xl/info:grid-cols-1">
          <InfoIdentity app={app} />
          <div className="grid min-w-0 content-start gap-4">
            <InfoAssets appKey={app.appKey} />
            <InfoShortcuts appKey={app.appKey} />
          </div>
          {onDelete && (
            <div className="@3xl/info:col-span-2 @5xl/info:col-span-1">
              <InfoDangerZone appName={app.name} onDelete={onDelete} />
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
