"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Activity, Brain, Disc3, ScrollText, Settings2, Users } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { appSectionHref } from "@/lib/app-sections";
import { MusicConfigPanel } from "./music-config";
import { MusicEvents } from "./music-events";
import { MusicListeners } from "./music-listeners";
import { MusicModels } from "./music-models";
import { MusicOverview } from "./music-overview";
import { MusicAccessGate, MusicPageHeader, useMusicAccess } from "./music-shell";
import { MusicTracks } from "./music-tracks";

/**
 * 音乐推荐工作台（/apps/{appKey}/music）。页签同步到 `?tab=`，只挂载当前页签；
 * 应用详情的「音乐推荐」区块、用户画像页与作品详情页的返回链接都落在这里。
 */

export const MUSIC_TABS = [
  { value: "overview", label: "总览", icon: Activity },
  { value: "listeners", label: "听众", icon: Users },
  { value: "tracks", label: "作品", icon: Disc3 },
  { value: "events", label: "事件", icon: ScrollText },
  { value: "models", label: "模型", icon: Brain },
  { value: "config", label: "配置", icon: Settings2 }
] as const;

type TabValue = (typeof MUSIC_TABS)[number]["value"];
const TAB_SET = new Set<string>(MUSIC_TABS.map((t) => t.value));

export function MusicWorkspace({ appKey }: { appKey: string }) {
  return (
    <div className="page-stack min-w-0 gap-4">
      <MusicPageHeader appKey={appKey} title="音乐推荐" backHref={appSectionHref(appKey, "music")} backLabel="返回应用" />
      <MusicAccessGate appKey={appKey}>
        <WorkspaceTabs appKey={appKey} />
      </MusicAccessGate>
    </div>
  );
}

function WorkspaceTabs({ appKey }: { appKey: string }) {
  const params = useSearchParams();
  const router = useRouter();
  const { canManage } = useMusicAccess(appKey);
  const raw = params.get("tab");
  const tab: TabValue = raw && TAB_SET.has(raw) ? (raw as TabValue) : "overview";

  const go = (next: string) => {
    const q = new URLSearchParams(params.toString());
    if (next === "overview") q.delete("tab");
    else q.set("tab", next);
    const s = q.toString();
    router.replace(`/apps/${encodeURIComponent(appKey)}/music${s ? `?${s}` : ""}`, { scroll: false });
  };

  return (
    <Tabs value={tab} onValueChange={go} className="gap-4">
      <TabsList className="w-full justify-start overflow-x-auto sm:w-fit">
        {MUSIC_TABS.map((t) => (
          <TabsTrigger key={t.value} value={t.value} className="gap-1.5">
            <t.icon className="size-4" />
            {t.label}
          </TabsTrigger>
        ))}
      </TabsList>
      <TabsContent value="overview">{tab === "overview" && <MusicOverview appKey={appKey} onNavigate={go} />}</TabsContent>
      <TabsContent value="listeners">{tab === "listeners" && <MusicListeners appKey={appKey} />}</TabsContent>
      <TabsContent value="tracks">{tab === "tracks" && <MusicTracks appKey={appKey} />}</TabsContent>
      <TabsContent value="events">{tab === "events" && <MusicEvents appKey={appKey} initialUserId={Number(params.get("userId")) || undefined} />}</TabsContent>
      <TabsContent value="models">{tab === "models" && <MusicModels appKey={appKey} canManage={canManage} />}</TabsContent>
      <TabsContent value="config">{tab === "config" && <MusicConfigPanel appKey={appKey} canManage={canManage} />}</TabsContent>
    </Tabs>
  );
}
