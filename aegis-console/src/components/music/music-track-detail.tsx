"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Disc3, Layers, Pencil, Sparkles, Tags, TrendingUp, Waypoints } from "lucide-react";
import { CopyField } from "@/components/apps/detail/app-detail-kit";
import { shortDay } from "@/components/overview/overview-kit";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { MusicTrackDetail } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useMusicTrackQuery } from "@/lib/music-hooks";
import { clockText, compact, minutesText, num, pct, sourceLabel } from "./music-format";
import { EmptyLine, MusicError, PanelSkeleton, TrackCell, TrackCover, musicHref, musicTrackHref } from "./music-kit";
import { MusicAccessGate, MusicPageHeader, useMusicAccess } from "./music-shell";
import { TagEditorDialog, TagsDetail } from "./music-tag-parts";

const DAILY_CONFIG = {
  plays: { label: "播放次数", color: "var(--color-violet-500, #8b5cf6)" },
  listeners: { label: "听众", color: "var(--color-sky-500, #0ea5e9)" }
} satisfies ChartConfig;

/**
 * 作品详情（/apps/{appKey}/music/tracks/{workKey}）。一个作品是同一首歌在各音源的全部版本，
 * 统计按作品合并；相似作品分两路列出：「常被一起听」来自 ItemCF 邻居，「口味相近」来自 ALS 向量近邻。
 * 第三期加内容标签（大模型生成或人工修改），有写权限时可以在这里直接修改。
 */
export function MusicTrackDetailPage({ appKey, workKey }: { appKey: string; workKey: string }) {
  const query = useMusicTrackQuery(appKey, workKey);
  const data = query.data;
  return (
    <div className="page-stack min-w-0 gap-4">
      <MusicPageHeader
        appKey={appKey}
        title={data?.track.title || "作品详情"}
        subtitle={data ? [data.track.artist, data.track.album].filter(Boolean).join("  ") || undefined : undefined}
        icon={data ? <TrackCover track={data.track} size={40} /> : undefined}
        backHref={musicHref(appKey, "tracks")}
        backLabel="返回作品排行"
      />
      <MusicAccessGate appKey={appKey}>
        {!data ? (
          query.isError ? (
            <Panel icon={Disc3} title="作品详情">
              <MusicError error={query.error} onRetry={() => query.refetch()} />
            </Panel>
          ) : (
            <TabStack>
              <PanelSkeleton height={120} />
              <PanelSkeleton height={240} />
            </TabStack>
          )
        ) : (
          <TrackBody appKey={appKey} data={data} />
        )}
      </MusicAccessGate>
    </div>
  );
}

function TrackBody({ appKey, data }: { appKey: string; data: MusicTrackDetail }) {
  const s = data.stats;
  const figures: Array<[string, string]> = [
    ["播放", `${num(s.plays)} 次`],
    ["听众", `${num(s.listeners)} 人`],
    ["听歌时长", minutesText(s.minutes)],
    ["完播率", pct(s.completionRate)],
    ["跳过率", pct(s.skipRate)],
    ["收藏", `${num(s.likes)} 次`],
    ["不感兴趣", `${num(s.dislikes)} 次`],
    ["歌曲时长", clockText(data.track.durationMs)]
  ];
  const itemcf = data.similar.filter((x) => x.via === "itemcf");
  const als = data.similar.filter((x) => x.via === "als");
  return (
    <TabStack>
      <section className="grid min-w-0 gap-4 rounded-2xl border border-border bg-card p-4 sm:p-5 @4xl:grid-cols-[minmax(0,1fr)_18rem]">
        <dl className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4">
          {figures.map(([label, value]) => (
            <div key={label} className="min-w-0 rounded-xl border border-border p-2.5">
              <dt className="truncate text-[11px] text-muted-foreground">{label}</dt>
              <dd className="mt-0.5 truncate text-base font-semibold tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
        <CopyField label="作品键" value={data.workKey} />
      </section>

      <TagsPanel appKey={appKey} data={data} />

      <DailyPanel data={data} />

      <Panel icon={Layers} tone="sky" title="各音源版本" aside={<span className="text-xs text-muted-foreground tabular-nums">{data.variants.length}</span>}>
        {!data.variants.length ? (
          <EmptyLine>暂无</EmptyLine>
        ) : (
          <div className="min-w-0 overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>音源</TableHead>
                  <TableHead>歌曲 ID</TableHead>
                  <TableHead className="text-right">播放</TableHead>
                  <TableHead className="text-right">最近播放</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {[...data.variants]
                  .sort((a, b) => b.plays - a.plays)
                  .map((v) => (
                    <TableRow key={`${v.source}:${v.songId}`}>
                      <TableCell className="text-xs">{sourceLabel(v.source)}</TableCell>
                      <TableCell className="font-mono text-xs">{v.songId}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{num(v.plays)}</TableCell>
                      <TableCell className="text-right text-xs text-muted-foreground" title={formatDateTimeMinute(v.lastSeenAt)}>
                        {formatRelative(v.lastSeenAt)}
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Panel>

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
        <SimilarPanel appKey={appKey} icon={Waypoints} title="常被一起听" items={itemcf} barClassName="bg-violet-500" />
        <SimilarPanel appKey={appKey} icon={Sparkles} title="口味相近" items={als} barClassName="bg-sky-500" />
      </div>
    </TabStack>
  );
}

function TagsPanel({ appKey, data }: { appKey: string; data: MusicTrackDetail }) {
  const { canManage } = useMusicAccess(appKey);
  const [editing, setEditing] = useState(false);
  // 服务端升级前没有这个字段，按「未打标签」处理
  const tags = data.tags ?? null;
  return (
    <Panel
      icon={Tags}
      tone="sky"
      title="内容标签"
      aside={
        canManage ? (
          <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
            <Pencil />
            修改标签
          </Button>
        ) : null
      }
    >
      <TagsDetail tags={tags} />
      <TagEditorDialog
        appKey={appKey}
        target={editing ? { workKey: data.workKey, track: data.track, tags } : null}
        onOpenChange={(open) => !open && setEditing(false)}
      />
    </Panel>
  );
}

function DailyPanel({ data }: { data: MusicTrackDetail }) {
  const [view, setView] = useState<"plays" | "listeners">("plays");
  const rows = useMemo(() => data.daily.map((d) => ({ label: shortDay(d.day), plays: d.plays, listeners: d.listeners })), [data.daily]);
  return (
    <Panel
      icon={TrendingUp}
      tone="violet"
      title="每日播放"
      aside={
        <Segmented
          value={view}
          options={[
            { value: "plays", label: "播放次数" },
            { value: "listeners", label: "听众" }
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

function SimilarPanel({
  appKey,
  icon,
  title,
  items,
  barClassName
}: {
  appKey: string;
  icon: typeof Sparkles;
  title: string;
  items: MusicTrackDetail["similar"];
  barClassName: string;
}) {
  const max = Math.max(0.0001, ...items.map((i) => i.score));
  return (
    <Panel icon={icon} tone="muted" title={title} aside={<span className="text-xs text-muted-foreground tabular-nums">{items.length}</span>}>
      {!items.length ? (
        <EmptyLine>暂无，模型训练后生成</EmptyLine>
      ) : (
        <ol className="flex min-w-0 flex-col divide-y divide-border/60">
          {items.map((item) => (
            <li key={item.workKey} className="grid min-w-0 grid-cols-[minmax(0,1fr)_6rem_3rem] items-center gap-3 py-2 first:pt-0 last:pb-0">
              <TrackCell track={item.track} fallback={`作品 ${item.workKey}`} href={musicTrackHref(appKey, item.workKey)} />
              <span className="h-1.5 overflow-hidden rounded-full bg-muted">
                <span className={`block h-full rounded-full ${barClassName}`} style={{ width: `${(item.score / max) * 100}%` }} />
              </span>
              <span className="text-right text-xs text-muted-foreground tabular-nums">{item.score.toFixed(3)}</span>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}
