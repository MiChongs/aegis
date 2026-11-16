"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { toast } from "sonner";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  Brain,
  CalendarDays,
  Clock3,
  Heart,
  History,
  Loader2,
  MicVocal,
  ScrollText,
  Sparkles,
  ThumbsDown,
  Trash2,
  UserRound,
  Wand2
} from "lucide-react";
import { RingGauge } from "@/components/apps/detail/app-detail-kit";
import { ChipInput, Panel, TabStack } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ApiError } from "@/lib/api/client";
import type { MusicProfile, MusicRecItem, MusicRecPreviewParams, MusicScene, MusicUserDetail } from "@/lib/api/music";
import { formatDateTime, formatDateTimeMinute, formatRelative, formatShortDateTime } from "@/lib/datetime";
import {
  useForgetMusicUserMutation,
  useMusicUserMixesQuery,
  useMusicUserNames,
  useMusicUserQuery,
  useMusicUserRecommendationsQuery,
  useMusicUserTimelineQuery
} from "@/lib/music-hooks";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { cn } from "@/lib/utils";
import {
  ADJUST_CHANNELS,
  CHANNEL_COLOR,
  CHANNEL_ORDER,
  EVENT_TYPES,
  SCENES,
  WEEKDAY_LABEL,
  channelLabel,
  compact,
  elapsedText,
  eventLabel,
  eventSummary,
  minutesText,
  num,
  pct,
  peakHour,
  sceneLabel,
  sourceLabel
} from "./music-format";
import { EmptyLine, MusicError, PanelSkeleton, RankBars, TrackCell, TrackList, appUserHref, musicHref, musicTrackHref, userDisplayName } from "./music-kit";
import { FatiguePanel, FeedbackPanel, MixesPanel, PreferencesPanel, ReportPanel } from "./music-user-extras";
import { MusicAccessGate, MusicPageHeader, useMusicAccess } from "./music-shell";

const HOURS_CONFIG = { minutes: { label: "听歌分钟", color: "var(--color-violet-500, #8b5cf6)" } } satisfies ChartConfig;
const WEEK_CONFIG = { minutes: { label: "听歌分钟", color: "var(--color-sky-500, #0ea5e9)" } } satisfies ChartConfig;

/**
 * 用户听歌画像（/apps/{appKey}/music/users/{userId}）。
 *
 * 一页看完一个人：画像（与他在客户端看到的同一份）、听歌报告、实时状态（推荐即时生效的那部分）、
 * 偏好设置与不喜欢列表、曝光疲劳、模型里的口味与所在实验、风格歌单、推荐预览（带每个通道的分数拆解，不发曝光）
 * 与事件时间线。「清除听歌数据」与用户自己在客户端清除等效。
 */
export function MusicUserDetailPage({ appKey, userId }: { appKey: string; userId: number }) {
  const ids = useMemo(() => [userId], [userId]);
  const names = useMusicUserNames(appKey, ids);
  const name = userDisplayName(userId, names);
  const { canManage } = useMusicAccess(appKey);
  return (
    <div className="page-stack min-w-0 gap-4">
      <MusicPageHeader
        appKey={appKey}
        title={name}
        subtitle={names.get(userId) ? `#${userId}` : undefined}
        backHref={musicHref(appKey, "listeners")}
        backLabel="返回听众排行"
        actions={
          <>
            <Button asChild size="sm" variant="outline">
              <Link href={appUserHref(appKey, userId)}>
                <UserRound />
                用户资料
              </Link>
            </Button>
            <Button asChild size="sm" variant="outline">
              <Link href={`${musicHref(appKey, "events")}&userId=${userId}`}>
                <ScrollText />
                事件检索
              </Link>
            </Button>
            {canManage ? <ForgetButton appKey={appKey} userId={userId} name={name} /> : null}
          </>
        }
      />
      <MusicAccessGate appKey={appKey}>
        <UserBody appKey={appKey} userId={userId} />
      </MusicAccessGate>
    </div>
  );
}

function ForgetButton({ appKey, userId, name }: { appKey: string; userId: number; name: string }) {
  const [open, setOpen] = useState(false);
  const mutation = useForgetMusicUserMutation(appKey);
  const confirm = async () => {
    try {
      await mutation.mutateAsync(userId);
      toast.success("听歌数据已清除");
      setOpen(false);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "清除失败");
    }
  };
  return (
    <>
      <Button size="sm" variant="outline" className="text-rose-600 hover:text-rose-700 dark:text-rose-400" onClick={() => setOpen(true)}>
        <Trash2 />
        清除听歌数据
      </Button>
      <AlertDialog open={open} onOpenChange={(v) => !mutation.isPending && setOpen(v)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清除 {name} 的听歌数据？</AlertDialogTitle>
            <AlertDialogDescription>删除全部听歌事件、实时状态与模型里的口味，不可恢复。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={mutation.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={(e) => {
                e.preventDefault();
                void confirm();
              }}
            >
              {mutation.isPending ? <Loader2 className="animate-spin" /> : null}
              清除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function UserBody({ appKey, userId }: { appKey: string; userId: number }) {
  const query = useMusicUserQuery(appKey, userId);
  const data = query.data;
  if (!data) {
    if (query.isError) {
      return (
        <Panel icon={UserRound} title="听歌画像">
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        </Panel>
      );
    }
    return (
      <TabStack>
        <PanelSkeleton height={180} />
        <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
          <PanelSkeleton height={220} />
          <PanelSkeleton height={220} />
        </div>
      </TabStack>
    );
  }
  const p = data.profile;
  return (
    <TabStack>
      <PersonaHero profile={p} />
      {p.plays > 0 ? (
        <>
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
            <Panel icon={MicVocal} tone="amber" title="最常听的歌手">
              <RankBars
                barClassName="bg-amber-500"
                items={p.topArtists.slice(0, 10).map((a) => ({
                  key: a.name,
                  label: a.name,
                  value: a.plays,
                  display: `${num(a.plays)} 次  ${pct(a.share, 0)}`,
                  title: `${a.name}：${num(a.plays)} 次，${minutesText(a.minutes)}`
                }))}
              />
            </Panel>
            <Panel icon={Sparkles} tone="violet" title="最常听的歌">
              <TrackList
                appKey={appKey}
                items={p.topTracks.slice(0, 10).map((t, i) => ({
                  key: `${t.track.source}:${t.track.songId}:${i}`,
                  track: t.track,
                  right: `${num(t.plays)} 次`,
                  sub: minutesText(t.minutes)
                }))}
              />
            </Panel>
          </div>
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
            <HoursPanel hours={p.hours} />
            <WeekdaysPanel weekdays={p.weekdays} />
          </div>
        </>
      ) : null}
      <ReportPanel appKey={appKey} userId={userId} />
      <RealtimePanel appKey={appKey} data={data} />
      <div className="grid min-w-0 items-start gap-4 @4xl:grid-cols-2">
        <PreferencesPanel appKey={appKey} data={data} />
        <FatiguePanel appKey={appKey} data={data} />
      </div>
      <FeedbackPanel appKey={appKey} data={data} />
      <ModelTastePanel data={data} />
      <MixesPanel appKey={appKey} userId={userId} />
      <RecPreviewPanel appKey={appKey} userId={userId} data={data} />
      <TimelinePanel appKey={appKey} userId={userId} />
    </TabStack>
  );
}

/* ── 画像 ── */

function PersonaHero({ profile: p }: { profile: MusicProfile }) {
  const figures: Array<[string, string]> = [
    ["听歌时长", minutesText(p.listeningMinutes)],
    ["播放", `${num(p.plays)} 次`],
    ["听过的歌", `${num(p.tracks)} 首`],
    ["听过的歌手", `${num(p.artists)} 位`],
    ["活跃天数", `${num(p.activeDays)} 天`],
    ["最长连续", `${num(p.longestStreak)} 天`],
    ["收藏", `${num(p.likes)} 首`],
    ["新歌占比", pct(p.newTrackShare, 0)]
  ];
  const sources = [...p.sources].sort((a, b) => b.plays - a.plays);
  const sourceTotal = sources.reduce((s, x) => s + x.plays, 0);
  return (
    <section className="grid min-w-0 gap-5 rounded-2xl border border-border bg-card p-4 sm:p-5 @4xl:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
      <div className="flex min-w-0 flex-col gap-4">
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-xs text-muted-foreground">
            近 {p.days} 天的听歌画像{p.updatedAt ? `，${formatRelative(p.updatedAt)}更新` : ""}
          </span>
          <h3 className="text-2xl leading-8 font-semibold tracking-tight">{p.plays > 0 ? p.persona?.title || "随性型" : "还没有听歌记录"}</h3>
          {p.plays > 0 && p.persona?.text ? <p className="text-sm leading-6 text-muted-foreground">{p.persona.text}</p> : null}
        </div>
        <dl className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4">
          {figures.map(([label, value]) => (
            <div key={label} className="min-w-0 rounded-xl border border-border p-2.5">
              <dt className="truncate text-[11px] text-muted-foreground">{label}</dt>
              <dd className="mt-0.5 truncate text-base font-semibold tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
      </div>
      <div className="flex min-w-0 flex-col gap-4">
        <div className="flex items-center gap-6">
          <Ring label="完播率" value={p.completionRate} tone="emerald" />
          <Ring label="跳过率" value={p.skipRate} tone="rose" />
        </div>
        {p.traits.length ? (
          <ul className="flex min-w-0 flex-col gap-2">
            {p.traits.map((t) => (
              <li key={t.key} className="grid min-w-0 grid-cols-[4rem_minmax(0,1fr)_3rem] items-center gap-3 text-xs">
                <span className="truncate">{t.title}</span>
                <span className="h-2 overflow-hidden rounded-full bg-muted">
                  <span className="block h-full rounded-full bg-violet-500" style={{ width: `${Math.min(100, Math.max(0, t.value * 100))}%` }} />
                </span>
                <span className="text-right text-muted-foreground tabular-nums">{pct(t.value, 0)}</span>
              </li>
            ))}
          </ul>
        ) : null}
        {sourceTotal ? (
          <p className="text-xs text-muted-foreground">
            常用音源：
            {sources
              .slice(0, 3)
              .map((s) => `${sourceLabel(s.source)} ${pct(s.plays / sourceTotal, 0)}`)
              .join("，")}
          </p>
        ) : null}
      </div>
    </section>
  );
}

function Ring({ label, value, tone }: { label: string; value: number; tone: "emerald" | "rose" }) {
  return (
    <div className="flex items-center gap-3">
      <RingGauge value={Number.isFinite(value) ? value * 100 : null} tone={tone} size={64} stroke={7}>
        <span className="text-xs font-semibold tabular-nums">{pct(value, 0)}</span>
      </RingGauge>
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
  );
}

function HoursPanel({ hours }: { hours: number[] }) {
  const rows = Array.from({ length: 24 }, (_, h) => ({ label: `${h}点`, minutes: Math.round(hours[h] ?? 0) }));
  const peak = peakHour(hours);
  return (
    <Panel icon={Clock3} tone="violet" title="一天中的听歌时间">
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-sm leading-5 text-muted-foreground">{peak !== null ? `最常在 ${peak} 点听歌。` : "暂无数据。"}</p>
        <ChartContainer config={HOURS_CONFIG} className="aspect-auto h-[180px] w-full">
          <BarChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval={2} />
            <YAxis tickLine={false} axisLine={false} width={36} allowDecimals={false} />
            <ChartTooltip cursor={false} content={<ChartTooltipContent hideIndicator />} />
            <Bar dataKey="minutes" fill="var(--color-minutes)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </div>
    </Panel>
  );
}

function WeekdaysPanel({ weekdays }: { weekdays: number[] }) {
  const rows = WEEKDAY_LABEL.map((label, i) => ({ label, minutes: Math.round(weekdays[i] ?? 0) }));
  const best = rows.reduce<(typeof rows)[number] | null>((b, r) => (r.minutes > (b?.minutes ?? 0) ? r : b), null);
  return (
    <Panel icon={CalendarDays} tone="sky" title="一周中的听歌时间">
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-sm leading-5 text-muted-foreground">{best ? `${best.label}听得最多。` : "暂无数据。"}</p>
        <ChartContainer config={WEEK_CONFIG} className="aspect-auto h-[180px] w-full">
          <BarChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis tickLine={false} axisLine={false} width={36} allowDecimals={false} />
            <ChartTooltip cursor={false} content={<ChartTooltipContent hideIndicator />} />
            <Bar dataKey="minutes" fill="var(--color-minutes)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </div>
    </Panel>
  );
}

/* ── 实时状态 ── */

function RealtimePanel({ appKey, data }: { appKey: string; data: MusicUserDetail }) {
  const rt = data.realtime;
  return (
    <div className="grid min-w-0 gap-4 @4xl:grid-cols-2 @6xl:grid-cols-4">
      <Panel icon={History} tone="sky" title="最近播放" aside={<span className="text-xs text-muted-foreground tabular-nums">{rt.recent.length}</span>}>
        <div className="max-h-96 overflow-y-auto">
          <TrackList
            appKey={appKey}
            items={rt.recent.map((r, i) => ({ key: `${r.workKey}:${r.at}:${i}`, track: r.track, workKey: r.workKey, sub: formatRelative(r.at) }))}
          />
        </div>
      </Panel>
      <Panel icon={Heart} tone="emerald" title="正反馈" aside={<span className="text-xs text-muted-foreground tabular-nums">{rt.positive.length}</span>}>
        <div className="max-h-96 overflow-y-auto">
          <TrackList
            appKey={appKey}
            items={rt.positive.map((r) => ({ key: r.workKey, track: r.track, workKey: r.workKey, right: r.weight.toFixed(1) }))}
          />
        </div>
      </Panel>
      <Panel icon={ThumbsDown} tone="rose" title="负反馈" aside={<span className="text-xs text-muted-foreground tabular-nums">{rt.negative.length}</span>}>
        <div className="max-h-96 overflow-y-auto">
          <TrackList
            appKey={appKey}
            items={rt.negative.map((r) => ({ key: r.workKey, track: r.track, workKey: r.workKey, sub: formatRelative(r.at) }))}
          />
        </div>
      </Panel>
      <Panel icon={MicVocal} tone="amber" title="不喜欢的歌手">
        {rt.dislikedArtists.length ? (
          <ul className="flex min-w-0 flex-col gap-2">
            {rt.dislikedArtists.map((a) => (
              <li key={a.name} className="flex min-w-0 items-center justify-between gap-2 text-xs">
                <span className="truncate">{a.name}</span>
                <span className="shrink-0 text-[11px] text-muted-foreground" title={formatDateTimeMinute(a.at)}>
                  {formatRelative(a.at)}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyLine>暂无</EmptyLine>
        )}
      </Panel>
    </div>
  );
}

/* ── 模型口味 ── */

function ModelTastePanel({ data }: { data: MusicUserDetail }) {
  const m = data.model;
  // 服务端升级前没有这两个字段，按「没用折入、不在实验里」处理
  const experiments = m.experiments ?? [];
  return (
    <Panel
      icon={Brain}
      tone="violet"
      title="模型里的口味"
      aside={
        <>
          {m.foldIn ? (
            <Badge variant="info" size="sm">
              使用实时折入
            </Badge>
          ) : null}
          {m.inModel ? (
            <Badge variant="success" size="sm">
              已进入第 {m.version} 版模型
            </Badge>
          ) : (
            <Badge variant="secondary" size="sm">
              未进入模型
            </Badge>
          )}
        </>
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        {experiments.length ? (
          <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">所在实验：</span>
            {experiments.map((e) => (
              <Badge key={e.id} variant="outline" size="sm">
                {e.name}，{e.variant} 组
              </Badge>
            ))}
          </div>
        ) : null}
        {!m.inModel ? (
          <EmptyLine>
            {m.foldIn ? "还没进入训练好的模型，推荐按最近的正反馈实时折入口味后给出" : "行为数据还不够或尚未训练，推荐按实时反馈与热门给出"}
          </EmptyLine>
        ) : (
          <>
            <p className="text-xs text-muted-foreground">
              训练时计入 {num(m.interactions)} 条交互{m.foldIn ? "；训练之后的新行为已实时折入当下口味" : ""}。
            </p>
            <RankBars
              barClassName="bg-violet-500"
              empty="暂无歌手偏好"
              items={m.topArtists.slice(0, 12).map((a) => ({ key: a.name, label: a.name, value: a.weight, display: a.weight.toFixed(2) }))}
            />
          </>
        )}
      </div>
    </Panel>
  );
}

/* ── 推荐预览 ── */

/** autoplay 种子：`音源:歌曲 ID` */
const SEED_RE = /^[a-z0-9_]+:\S+$/i;

function RecPreviewPanel({ appKey, userId, data }: { appKey: string; userId: number; data: MusicUserDetail }) {
  const [scene, setScene] = useState<MusicScene>("home");
  // 相似歌曲需要一首种子：取最近播放的第一首，没有就取最常听的第一首
  const seed = data.realtime.recent.find((r) => r.track)?.track ?? data.profile.topTracks[0]?.track ?? null;
  // 智能续播的种子：默认取最近播放的 5 首（最近放的在后），可以改
  const defaultSeeds = useMemo(
    () =>
      data.realtime.recent
        .filter((r) => r.track?.source && r.track.songId)
        .slice(0, 5)
        .map((r) => `${r.track?.source}:${r.track?.songId}`)
        .reverse(),
    [data.realtime.recent]
  );
  const [seedDraft, setSeedDraft] = useState<string[] | null>(null);
  const seeds = (seedDraft ?? defaultSeeds).slice(-20);
  const invalidSeeds = new Set(seeds.flatMap((s, i) => (SEED_RE.test(s) ? [] : [i])));
  // 歌手热门：默认最常听的歌手
  const [artistDraft, setArtistDraft] = useState<string | null>(null);
  const artistText = artistDraft ?? data.profile.topArtists[0]?.name ?? "";
  const artist = useDebouncedValue(artistText.trim(), 400);
  // 风格歌单：默认第一个
  const mixes = useMusicUserMixesQuery(scene === "mix" ? appKey : null, userId);
  const [mixDraft, setMixDraft] = useState<string | null>(null);
  const mixId = mixDraft ?? mixes.data?.items[0]?.id ?? "";

  const params: MusicRecPreviewParams = { scene, limit: 20 };
  let blocked: string | null = null;
  if (scene === "similar") {
    if (seed) Object.assign(params, { source: seed.source, songId: seed.songId });
    else blocked = "没有可作种子的歌曲";
  } else if (scene === "autoplay") {
    if (seeds.length && !invalidSeeds.size) params.seeds = seeds.join(",");
    else blocked = seeds.length ? "种子格式为「音源:歌曲 ID」" : "至少填一首种子歌曲";
  } else if (scene === "mix") {
    if (mixId) params.mixId = mixId;
    else blocked = mixes.data ? "这个人还没有风格歌单" : "加载风格歌单";
  } else if (scene === "artist") {
    if (artist) params.artist = artist;
    else blocked = "填写歌手名";
  }
  const query = useMusicUserRecommendationsQuery(appKey, userId, params, !blocked);
  const preview = query.data;
  const maxTotal = Math.max(
    0.0001,
    ...(preview?.items ?? []).map((item) => CHANNEL_ORDER.reduce((s, k) => s + Math.max(0, item.channels?.[k] ?? 0), 0))
  );
  const experiment = preview?.experimentId ? (data.model.experiments ?? []).find((e) => e.id === preview.experimentId) : undefined;

  return (
    <Panel
      icon={Wand2}
      tone="emerald"
      title="推荐预览"
      aside={
        <Select value={scene} onValueChange={(v) => setScene(v as MusicScene)}>
          <SelectTrigger size="sm" className="h-8 w-32 text-xs" aria-label="推荐场景">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SCENES.map((s) => (
              <SelectItem key={s} value={s}>
                {sceneLabel(s)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        {scene === "autoplay" ? (
          <div className="flex min-w-0 flex-col gap-1.5">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">种子歌曲（音源:歌曲 ID，最近放的在后，最多 20 首）</span>
              {seedDraft ? (
                <Button size="xs" variant="ghost" onClick={() => setSeedDraft(null)}>
                  用最近播放
                </Button>
              ) : null}
            </div>
            <ChipInput value={seeds} onChange={setSeedDraft} invalid={invalidSeeds} placeholder="如 wy:186016，回车添加" ariaLabel="种子歌曲" />
          </div>
        ) : scene === "mix" ? (
          <div className="flex min-w-0 items-center gap-2">
            <span className="shrink-0 text-xs text-muted-foreground">风格歌单</span>
            <Select value={mixId} onValueChange={setMixDraft} disabled={!mixes.data?.items.length}>
              <SelectTrigger size="sm" className="h-8 w-72 max-w-full text-xs" aria-label="风格歌单">
                <SelectValue placeholder={mixes.data ? "没有风格歌单" : "加载中"} />
              </SelectTrigger>
              <SelectContent>
                {(mixes.data?.items ?? []).map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : scene === "artist" ? (
          <div className="flex min-w-0 items-center gap-2">
            <span className="shrink-0 text-xs text-muted-foreground">歌手</span>
            <Input value={artistText} onChange={(e) => setArtistDraft(e.target.value)} placeholder="歌手名" className="h-8 w-56 text-xs" aria-label="歌手名" />
          </div>
        ) : null}

        {blocked ? (
          <EmptyLine>{blocked}</EmptyLine>
        ) : query.isError && !preview ? (
          <MusicError error={query.error} onRetry={() => query.refetch()} />
        ) : !preview ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="h-12 animate-pulse rounded-lg bg-muted/60" />
            ))}
          </div>
        ) : (
          <div className={cn("flex min-w-0 flex-col gap-3", query.isFetching && "opacity-60 transition-opacity")}>
            <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
              {scene === "similar" && seed ? <span>种子：《{seed.title}》</span> : null}
              <span>{preview.modelVersion ? `第 ${preview.modelVersion} 版模型` : "未使用模型"}</span>
              {preview.coldStart ? (
                <Badge variant="warning" size="sm">
                  冷启动
                </Badge>
              ) : null}
              {!preview.personalized ? (
                <Badge variant="secondary" size="sm">
                  非个性化
                </Badge>
              ) : null}
              {preview.experimentId ? (
                <Badge variant="info" size="sm">
                  {experiment ? experiment.name : `实验 #${preview.experimentId}`}，{preview.variant} 组
                </Badge>
              ) : null}
              <span>耗时 {elapsedText(preview.tookMs)}</span>
              <span className="ml-auto flex flex-wrap gap-x-3 gap-y-1">
                {CHANNEL_ORDER.map((k) => (
                  <span key={k} className="inline-flex items-center gap-1.5">
                    <span aria-hidden className={cn("size-2 rounded-[3px]", CHANNEL_COLOR[k])} />
                    {channelLabel(k)}
                  </span>
                ))}
              </span>
            </div>
            {!preview.items.length ? (
              <EmptyLine>没有可推荐的歌曲</EmptyLine>
            ) : (
              <ol className="flex min-w-0 flex-col divide-y divide-border/60">
                {preview.items.map((item, index) => (
                  <RecRow key={`${item.workKey}:${index}`} appKey={appKey} item={item} index={index} maxTotal={maxTotal} />
                ))}
              </ol>
            )}
          </div>
        )}
      </div>
    </Panel>
  );
}

/**
 * 融合之后的调整项：精排分只要有就显示；运营干预与曝光疲劳是乘数，等于 1（或缺省为 0）时没起作用，不显示。
 * 契约只给了通道名，没写取值含义，这里按「精排为分数、另两项为乘数」展示原始值。
 */
function adjustTags(channels: Record<string, number>) {
  return ADJUST_CHANNELS.flatMap((k) => {
    const v = channels[k];
    if (typeof v !== "number" || !Number.isFinite(v)) return [];
    if (k !== "ranker" && (v === 0 || v === 1)) return [];
    return [{ key: k, text: k === "ranker" ? `${channelLabel(k)} ${v.toFixed(3)}` : `${channelLabel(k)} ×${Number(v.toFixed(2))}`, bad: k === "fatigue" || (k === "rule" && v < 1) }];
  });
}

function RecRow({ appKey, item, index, maxTotal }: { appKey: string; item: MusicRecItem; index: number; maxTotal: number }) {
  const channels = item.channels ?? {};
  const penalty = channels.penalty ?? 0;
  return (
    <li className="grid min-w-0 grid-cols-[1.5rem_minmax(0,1fr)] items-center gap-3 py-2 first:pt-0 last:pb-0 @3xl:grid-cols-[1.5rem_minmax(0,16rem)_minmax(0,1fr)_minmax(0,14rem)_5.5rem]">
      <span className="text-xs text-muted-foreground tabular-nums">{index + 1}</span>
      <TrackCell track={item.track} href={musicTrackHref(appKey, item.workKey)} />
      <span className="col-start-2 truncate text-[11px] text-muted-foreground @3xl:col-start-auto">{item.reason?.text || "—"}</span>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="col-start-2 flex h-2.5 min-w-0 overflow-hidden rounded-full bg-muted @3xl:col-start-auto">
            {CHANNEL_ORDER.map((k) => {
              const v = Math.max(0, channels[k] ?? 0);
              return v > 0 ? <span key={k} className={cn("h-full", CHANNEL_COLOR[k])} style={{ width: `${(v / maxTotal) * 100}%` }} /> : null;
            })}
          </span>
        </TooltipTrigger>
        <TooltipContent>
          <div className="flex flex-col gap-0.5 text-xs">
            {[...CHANNEL_ORDER, "penalty", ...ADJUST_CHANNELS].map((k) =>
              channels[k] ? (
                <span key={k} className="flex justify-between gap-4">
                  <span>{channelLabel(k)}</span>
                  <span className="tabular-nums">{channels[k].toFixed(3)}</span>
                </span>
              ) : null
            )}
          </div>
        </TooltipContent>
      </Tooltip>
      <span className="col-start-2 text-xs font-medium tabular-nums @3xl:col-start-auto @3xl:text-right">
        {item.score.toFixed(3)}
        {penalty < 0 ? <span className="block text-[11px] font-normal text-rose-600 dark:text-rose-400">降权 {penalty.toFixed(2)}</span> : null}
        {adjustTags(channels).map((t) => (
          <span key={t.key} className={cn("block text-[11px] font-normal whitespace-nowrap", t.bad ? "text-rose-600 dark:text-rose-400" : "text-sky-600 dark:text-sky-400")}>
            {t.text}
          </span>
        ))}
      </span>
    </li>
  );
}

/* ── 事件时间线 ── */

const ALL = "all";

function TimelinePanel({ appKey, userId }: { appKey: string; userId: number }) {
  const [type, setType] = useState(ALL);
  const query = useMusicUserTimelineQuery(appKey, userId, type === ALL ? "" : type);
  const events = useMemo(() => query.data?.pages.flatMap((p) => p.items) ?? [], [query.data]);
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query;

  // 滚到底部自动取下一页（游标 nextBefore）；按钮保留，键盘与不支持观察器的环境也能翻
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasNextPage) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting) && !isFetchingNextPage) void fetchNextPage();
      },
      { rootMargin: "200px" }
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  return (
    <Panel
      icon={ScrollText}
      tone="sky"
      title="事件时间线"
      aside={
        <Select value={type} onValueChange={setType}>
          <SelectTrigger size="sm" className="h-8 w-36 text-xs" aria-label="事件类型">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部类型</SelectItem>
            {EVENT_TYPES.map((t) => (
              <SelectItem key={t} value={t}>
                {eventLabel(t)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
    >
      {query.isError && !query.data ? (
        <MusicError error={query.error} onRetry={() => query.refetch()} />
      ) : query.isLoading ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="h-10 animate-pulse rounded-lg bg-muted/60" />
          ))}
        </div>
      ) : !events.length ? (
        <EmptyLine>暂无事件</EmptyLine>
      ) : (
        <div className="flex min-w-0 flex-col">
          <ol className="relative flex min-w-0 flex-col border-l border-border pl-4">
            {events.map((event) => (
              <li key={event.id} className="relative flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 py-2">
                <span aria-hidden className="absolute top-1/2 -left-[21px] size-2 -translate-y-1/2 rounded-full border-2 border-background bg-sky-500" />
                <span className="w-24 shrink-0 text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(event.ts)}>
                  {formatShortDateTime(event.ts)}
                </span>
                <Badge variant="outline" size="sm">
                  {eventLabel(event.type)}
                </Badge>
                {event.track ? (
                  <TrackCell track={event.track} cover={false} href={event.workKey ? musicTrackHref(appKey, event.workKey) : undefined} className="max-w-64" />
                ) : null}
                <span className="min-w-0 truncate text-xs text-muted-foreground">{eventSummary(event)}</span>
              </li>
            ))}
          </ol>
          <div ref={sentinel} className="flex justify-center pt-3">
            {hasNextPage ? (
              <Button size="sm" variant="ghost" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                {isFetchingNextPage ? <Loader2 className="animate-spin" /> : null}
                加载更多
              </Button>
            ) : (
              <span className="text-[11px] text-muted-foreground">已到最早一条，共 {compact(events.length)} 条</span>
            )}
          </div>
        </div>
      )}
    </Panel>
  );
}

