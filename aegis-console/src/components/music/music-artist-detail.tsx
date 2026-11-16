"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Disc3, MicVocal, Sparkles, TrendingUp } from "lucide-react";
import { shortDay } from "@/components/overview/overview-kit";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import type { MusicArtistDetail } from "@/lib/api/music";
import { useMusicArtistQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { compact, minutesText, num, pct } from "./music-format";
import { ArtistCell, ArtistCover, DAYS_LONG, DaysSwitch, EmptyLine, MusicError, PanelSkeleton, TrackCell, musicArtistHref, musicHref, musicTrackHref } from "./music-kit";
import { MusicAccessGate, MusicPageHeader } from "./music-shell";

const DAILY_CONFIG = {
  plays: { label: "播放次数", color: "var(--color-amber-500, #f59e0b)" },
  listeners: { label: "听众", color: "var(--color-sky-500, #0ea5e9)" }
} satisfies ChartConfig;

/**
 * 歌手详情（/apps/{appKey}/music/artists/{name}）：统计、每日播放与听众、代表作、模型里的相似歌手。
 * 相似歌手按共同听众的余弦相似度算（训练时生成），点击可继续跳到那位歌手。
 */
export function MusicArtistDetailPage({ appKey, name }: { appKey: string; name: string }) {
  const [days, setDays] = useState(30);
  const query = useMusicArtistQuery(appKey, name, days);
  // 从相似歌手跳过来时，占位数据是上一位歌手的，不拿来显示
  const data = query.data && !(query.isPlaceholderData && query.data.name !== name) ? query.data : undefined;
  return (
    <div className="page-stack min-w-0 gap-4">
      <MusicPageHeader
        appKey={appKey}
        title={name || "歌手详情"}
        subtitle={data ? `${num(data.tracks)} 首作品` : undefined}
        icon={<ArtistCover cover={data?.cover} size={40} />}
        backHref={musicHref(appKey, "artists")}
        backLabel="返回歌手排行"
        actions={<DaysSwitch value={days} options={DAYS_LONG} onChange={setDays} />}
      />
      <MusicAccessGate appKey={appKey}>
        {!data ? (
          query.isError ? (
            <Panel icon={MicVocal} title="歌手详情">
              <MusicError error={query.error} onRetry={() => query.refetch()} />
            </Panel>
          ) : (
            <TabStack>
              <PanelSkeleton height={120} />
              <PanelSkeleton height={240} />
            </TabStack>
          )
        ) : (
          <ArtistBody appKey={appKey} data={data} days={days} fetching={query.isFetching && query.isPlaceholderData} />
        )}
      </MusicAccessGate>
    </div>
  );
}

function ArtistBody({ appKey, data, days, fetching }: { appKey: string; data: MusicArtistDetail; days: number; fetching: boolean }) {
  const figures: Array<[string, string]> = [
    ["播放", `${num(data.plays)} 次`],
    ["听众", `${num(data.listeners)} 人`],
    ["听歌时长", minutesText(data.minutes)],
    ["作品", `${num(data.tracks)} 首`],
    ["完播率", pct(data.completionRate)],
    ["跳过率", pct(data.skipRate)],
    ["收藏", `${num(data.likes)} 次`],
    ["不喜欢这位歌手", `${num(data.dislikes)} 人`]
  ];
  const maxSim = Math.max(0.0001, ...data.similar.map((s) => s.score));
  return (
    <TabStack className={cn(fetching && "opacity-70 transition-opacity")}>
      <section className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-card p-4 sm:p-5">
        <p className="text-sm leading-6 text-muted-foreground">
          {data.plays
            ? `近 ${days} 天 ${num(data.listeners)} 人听了 ${data.name} 共 ${compact(data.plays)} 次，人均 ${data.listeners ? (data.plays / data.listeners).toFixed(1) : "0"} 次。`
            : `近 ${days} 天没有人听 ${data.name}。`}
        </p>
        <dl className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4">
          {figures.map(([label, value]) => (
            <div key={label} className="min-w-0 rounded-xl border border-border p-2.5">
              <dt className="truncate text-[11px] text-muted-foreground">{label}</dt>
              <dd className="mt-0.5 truncate text-base font-semibold tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
      </section>

      <DailyPanel data={data} />

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Panel icon={Disc3} tone="violet" title="代表作" aside={<span className="text-xs text-muted-foreground tabular-nums">{data.topTracks.length}</span>}>
          {!data.topTracks.length ? (
            <EmptyLine>暂无播放</EmptyLine>
          ) : (
            <ol className="flex min-w-0 flex-col divide-y divide-border/60">
              {data.topTracks.map((t, i) => (
                <li key={t.workKey} className="grid min-w-0 grid-cols-[1.25rem_minmax(0,1fr)_auto] items-center gap-3 py-2 first:pt-0 last:pb-0">
                  <span className={cn("text-xs font-semibold tabular-nums", i < 3 ? "text-foreground" : "text-muted-foreground")}>{i + 1}</span>
                  <TrackCell track={t.track} href={musicTrackHref(appKey, t.workKey)} />
                  <span className="flex flex-col items-end text-[11px] leading-4 text-muted-foreground tabular-nums">
                    <span className="text-xs font-medium text-foreground">{compact(t.plays)} 次</span>
                    {num(t.listeners)} 人
                  </span>
                </li>
              ))}
            </ol>
          )}
        </Panel>
        <Panel icon={Sparkles} tone="amber" title="相似歌手" aside={<span className="text-xs text-muted-foreground tabular-nums">{data.similar.length}</span>}>
          {!data.similar.length ? (
            <EmptyLine>暂无，模型训练后生成</EmptyLine>
          ) : (
            <ol className="flex min-w-0 flex-col divide-y divide-border/60">
              {data.similar.map((s) => (
                <li key={s.name} className="grid min-w-0 grid-cols-[minmax(0,1fr)_5rem_3rem] items-center gap-3 py-2 first:pt-0 last:pb-0">
                  <ArtistCell name={s.name} cover={s.cover} href={musicArtistHref(appKey, s.name)} />
                  <span className="h-1.5 overflow-hidden rounded-full bg-muted">
                    <span className="block h-full rounded-full bg-amber-500" style={{ width: `${(s.score / maxSim) * 100}%` }} />
                  </span>
                  <span className="text-right text-xs text-muted-foreground tabular-nums">{s.score.toFixed(3)}</span>
                </li>
              ))}
            </ol>
          )}
        </Panel>
      </div>
    </TabStack>
  );
}

function DailyPanel({ data }: { data: MusicArtistDetail }) {
  const [view, setView] = useState<"plays" | "listeners">("listeners");
  const rows = useMemo(() => data.daily.map((d) => ({ label: shortDay(d.day), plays: d.plays, listeners: d.listeners })), [data.daily]);
  return (
    <Panel
      icon={TrendingUp}
      tone="amber"
      title="每日走势"
      aside={
        <Segmented
          value={view}
          options={[
            { value: "listeners", label: "听众" },
            { value: "plays", label: "播放次数" }
          ]}
          onChange={setView}
          ariaLabel="指标"
        />
      }
    >
      {!rows.length ? (
        <div className="grid h-[220px] place-items-center rounded-xl bg-muted/40 text-sm text-muted-foreground">暂无数据</div>
      ) : (
        <ChartContainer config={DAILY_CONFIG} className="aspect-auto h-[220px] w-full">
          <AreaChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} />
            <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <Area dataKey={view} type="monotone" stroke={`var(--color-${view})`} fill={`var(--color-${view})`} fillOpacity={0.15} strokeWidth={2} />
          </AreaChart>
        </ChartContainer>
      )}
    </Panel>
  );
}
