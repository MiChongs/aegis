"use client";

import Link from "next/link";
import { ArrowRight, Brain, Disc3, FlaskConical, ListMusic, MicVocal, ScrollText, Search, Settings2, ShieldBan, Users } from "lucide-react";
import { DetailPanel, Metric } from "@/components/apps/detail/app-detail-kit";
import { Button } from "@/components/ui/button";
import { formatRelative } from "@/lib/datetime";
import { useMusicOverviewQuery } from "@/lib/music-hooks";
import { compact, minutesText, num, pct } from "./music-format";
import { MusicError, musicHref } from "./music-kit";
import { useMusicAccess } from "./music-shell";

const LINKS = [
  { tab: "listeners", label: "听众", icon: Users },
  { tab: "tracks", label: "作品", icon: Disc3 },
  { tab: "artists", label: "歌手", icon: MicVocal },
  { tab: "search", label: "搜索", icon: Search },
  { tab: "rules", label: "运营干预", icon: ShieldBan },
  { tab: "experiments", label: "实验", icon: FlaskConical },
  { tab: "events", label: "事件", icon: ScrollText },
  { tab: "models", label: "模型", icon: Brain },
  { tab: "config", label: "配置", icon: Settings2 }
] as const;

/**
 * 应用详情的「音乐推荐」区块（/apps/{appKey}?tab=music）：近 7 天要点 + 进整页工作台的入口。
 * 完整功能在 /apps/{appKey}/music，区块只放摘要，免得在左侧导航与整页之间维护两份同样的页签。
 */
export function AppMusicSectionPanel({ appKey }: { appKey: string }) {
  const { loading, canRead } = useMusicAccess(appKey);
  const query = useMusicOverviewQuery(canRead ? appKey : null, 7);
  const data = query.data;
  const t = data?.totals;

  return (
    <DetailPanel
      icon={ListMusic}
      tone="violet"
      title="音乐推荐"
      aside={
        canRead ? (
          <Button asChild size="sm">
            <Link href={musicHref(appKey)}>
              打开工作台
              <ArrowRight />
            </Link>
          </Button>
        ) : null
      }
    >
      {loading ? null : !canRead ? (
        <p className="text-xs text-muted-foreground">缺少「音乐推荐：查看」权限</p>
      ) : query.isError && !data ? (
        <MusicError error={query.error} onRetry={() => query.refetch()} />
      ) : (
        <div className="flex min-w-0 flex-col gap-4">
          <div className="grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-4">
            <Metric label="近 7 天听歌人数" value={t ? num(t.listeners) : "—"} loading={!data} icon={Users} tone="sky" />
            <Metric label="播放次数" value={t ? compact(t.plays) : "—"} loading={!data} />
            <Metric label="听歌时长" value={t ? minutesText(t.listeningMinutes) : "—"} loading={!data} />
            <Metric label="完播率" value={t ? pct(t.completionRate) : "—"} loading={!data} hint={t ? `跳过率 ${pct(t.skipRate)}` : undefined} />
          </div>
          <p className="text-xs text-muted-foreground">
            {data
              ? data.model
                ? `当前使用第 ${data.model.id} 版模型，${formatRelative(data.model.publishedAt)}发布。`
                : "尚未发布模型，推荐暂时只给近期热门。"
              : " "}
          </p>
          <div className="flex flex-wrap gap-2">
            {LINKS.map((link) => (
              <Button key={link.tab} asChild size="sm" variant="outline">
                <Link href={musicHref(appKey, link.tab)}>
                  <link.icon />
                  {link.label}
                </Link>
              </Button>
            ))}
          </div>
        </div>
      )}
    </DetailPanel>
  );
}
