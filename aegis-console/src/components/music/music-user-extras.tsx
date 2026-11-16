"use client";

import { useState } from "react";
import Link from "next/link";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { BookHeart, ChevronLeft, ChevronRight, Disc3, EyeOff, ListMusic, MicVocal, Moon, Sparkles, SlidersHorizontal, Sunrise, ThumbsDown } from "lucide-react";
import { longDay } from "@/components/overview/overview-kit";
import { Panel, Segmented } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import {
  MUSIC_REPORT_MAX_OFFSET,
  type MusicFeedbackItem,
  type MusicFeedbackKind,
  type MusicMix,
  type MusicReport,
  type MusicReportPeriod,
  type MusicUserDetail
} from "@/lib/api/music";
import { formatDate, formatDateTimeMinute, formatRelative, formatShortDateTime } from "@/lib/datetime";
import { useMusicUserMixesQuery, useMusicUserReportQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import {
  FEEDBACK_KIND_LABEL,
  REPORT_PERIOD_LABEL,
  WEEKDAY_LABEL,
  changeOf,
  compact,
  dislikeReasonLabel,
  langLabel,
  minutesText,
  num,
  sourceLabel
} from "./music-format";
import { ArtistCell, Delta, EmptyLine, MusicError, TrackCover, TrackList, musicArtistHref } from "./music-kit";

/**
 * 用户画像页的第二期区块：偏好设置、不喜欢列表、曝光疲劳、风格歌单、听歌报告。
 * 全部只读：用户自己在客户端改偏好、撤销不喜欢；管理端只看，清除数据仍走页头的「清除听歌数据」。
 */

/* ── 偏好设置 ── */

export function PreferencesPanel({ appKey, data }: { appKey: string; data: MusicUserDetail }) {
  // 服务端升级前没有这个字段，按「没设置过」处理
  const p = data.preferences ?? null;
  return (
    <Panel
      icon={SlidersHorizontal}
      tone="sky"
      title="偏好设置"
      aside={p?.updatedAt ? <span className="text-[11px] text-muted-foreground">{formatRelative(p.updatedAt)}更新</span> : null}
    >
      {!p ? (
        <EmptyLine>没有设置过偏好，按默认开启个性化推荐与智能续播</EmptyLine>
      ) : (
        <dl className="flex min-w-0 flex-col divide-y divide-border/60">
          <PrefRow label="个性化推荐">
            {p.personalized ? (
              <Badge variant="success" size="sm">
                开启
              </Badge>
            ) : (
              <Badge variant="warning" size="sm">
                已关闭，推荐不读个人数据
              </Badge>
            )}
          </PrefRow>
          <PrefRow label="智能续播">
            <Badge variant={p.autoplay ? "success" : "secondary"} size="sm">
              {p.autoplay ? "开启" : "关闭"}
            </Badge>
          </PrefRow>
          <PrefRow label="新用户偏好选择">
            <span className="text-xs">{p.onboarded ? "已完成或已跳过" : "未完成"}</span>
          </PrefRow>
          <PrefRow label="语种" stacked>
            {p.languages.length ? (
              <div className="flex flex-wrap gap-1.5">
                {p.languages.map((l) => (
                  <Badge key={l} variant="outline" size="sm">
                    {langLabel(l)}
                  </Badge>
                ))}
              </div>
            ) : (
              <span className="text-xs text-muted-foreground">未选择</span>
            )}
          </PrefRow>
          <PrefRow label={`喜欢的歌手${p.artists.length ? `（${p.artists.length}）` : ""}`} stacked>
            {p.artists.length ? (
              <div className="flex flex-wrap gap-1.5">
                {p.artists.map((a) => (
                  <Badge key={a} asChild variant="outline" size="sm">
                    <Link href={musicArtistHref(appKey, a)}>{a}</Link>
                  </Badge>
                ))}
              </div>
            ) : (
              <span className="text-xs text-muted-foreground">未选择</span>
            )}
          </PrefRow>
        </dl>
      )}
    </Panel>
  );
}

function PrefRow({ label, children, stacked }: { label: string; children: React.ReactNode; stacked?: boolean }) {
  return (
    <div className={cn("flex min-w-0 gap-3 py-2 first:pt-0 last:pb-0", stacked ? "flex-col gap-1.5" : "items-center justify-between")}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

/* ── 不喜欢列表 ── */

const FEEDBACK_KINDS: MusicFeedbackKind[] = ["track", "artist", "suppressed", "version"];

export function FeedbackPanel({ appKey, data }: { appKey: string; data: MusicUserDetail }) {
  const feedback = data.feedback ?? [];
  const counts = Object.fromEntries(FEEDBACK_KINDS.map((k) => [k, feedback.filter((f) => f.kind === k).length])) as Record<MusicFeedbackKind, number>;
  const [kind, setKind] = useState<MusicFeedbackKind>("track");
  const items = feedback.filter((f) => f.kind === kind).sort((a, b) => b.at - a.at);
  return (
    <Panel
      icon={ThumbsDown}
      tone="rose"
      title="不喜欢列表"
      aside={
        <Segmented
          value={kind}
          options={FEEDBACK_KINDS.map((k) => ({ value: k, label: `${FEEDBACK_KIND_LABEL[k]} ${counts[k]}` }))}
          onChange={setKind}
          ariaLabel="分组"
        />
      }
    >
      <div className="max-h-96 overflow-y-auto">
        {kind === "artist" ? (
          items.length ? (
            <ul className="flex min-w-0 flex-col divide-y divide-border/60">
              {items.map((f) => (
                <li key={`${f.artist}:${f.at}`} className="flex min-w-0 items-center justify-between gap-3 py-2 first:pt-0 last:pb-0">
                  <ArtistCell name={f.artist} href={f.artist ? musicArtistHref(appKey, f.artist) : undefined} />
                  <span className="shrink-0 text-[11px] text-muted-foreground" title={formatDateTimeMinute(f.at)}>
                    {formatRelative(f.at)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyLine>暂无</EmptyLine>
          )
        ) : (
          <TrackList
            appKey={appKey}
            empty="暂无"
            items={items.map((f, i) => ({
              key: `${f.workKey}:${f.track?.source ?? ""}:${f.track?.songId ?? ""}:${i}`,
              track: f.track,
              workKey: f.workKey || undefined,
              right: feedbackRight(f),
              sub: feedbackSub(f)
            }))}
          />
        )}
      </div>
      <p className="mt-3 text-[11px] text-muted-foreground">{FEEDBACK_NOTE[kind]}</p>
    </Panel>
  );
}

const FEEDBACK_NOTE: Record<MusicFeedbackKind, string> = {
  track: "这些歌不会再推荐给这个人，用户可在客户端撤销。",
  artist: "这些歌手的歌不会再推荐给这个人。",
  suppressed: "选了「听腻了」的歌，到期前不推荐，不算作不喜欢。",
  version: "选了「音质差」或「歌曲不对」，只屏蔽这个音源版本，作品本身照常推荐。"
};

function feedbackRight(f: MusicFeedbackItem) {
  if (f.kind === "version" && f.track?.source) return `${sourceLabel(f.track.source)}版本`;
  return dislikeReasonLabel(f.reason);
}

function feedbackSub(f: MusicFeedbackItem) {
  if (f.kind === "suppressed" && f.until) return `${formatDate(f.until)} 前不推荐`;
  return formatRelative(f.at);
}

/* ── 曝光疲劳 ── */

export function FatiguePanel({ appKey, data }: { appKey: string; data: MusicUserDetail }) {
  const rows = [...(data.realtime.fatigue ?? [])].sort((a, b) => b.count - a.count);
  return (
    <Panel icon={EyeOff} tone="amber" title="曝光疲劳" aside={<span className="text-xs text-muted-foreground tabular-nums">{rows.length}</span>}>
      <div className="max-h-96 overflow-y-auto">
        <TrackList
          appKey={appKey}
          empty="近 7 天没有反复展示却没点的歌"
          items={rows.map((r) => ({ key: r.workKey, track: r.track, workKey: r.workKey, right: `${num(r.count)} 次`, sub: "展示未点击" }))}
        />
      </div>
      <p className="mt-3 text-[11px] text-muted-foreground">近 7 天展示达到阈值仍未点击的歌会被降权，点击或播放后清零。</p>
    </Panel>
  );
}

/* ── 风格歌单 ── */

export function MixesPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const query = useMusicUserMixesQuery(appKey, userId);
  const data = query.data;
  return (
    <Panel
      icon={ListMusic}
      tone="violet"
      title="风格歌单"
      aside={data?.expiresAt ? <span className="text-[11px] text-muted-foreground">有效至 {formatShortDateTime(data.expiresAt)}</span> : null}
    >
      {query.isError && !data ? (
        <MusicError error={query.error} onRetry={() => query.refetch()} />
      ) : !data ? (
        <div className="grid min-w-0 gap-3 @3xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="h-20 animate-pulse rounded-xl bg-muted/60" />
          ))}
        </div>
      ) : !data.items.length ? (
        <EmptyLine>常听的歌还不够（少于 12 首），暂时生成不了风格歌单</EmptyLine>
      ) : (
        <ul className="grid min-w-0 gap-3 @3xl:grid-cols-2 @6xl:grid-cols-3">
          {data.items.map((mix) => (
            <MixCard key={mix.id} mix={mix} />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function MixCard({ mix }: { mix: MusicMix }) {
  const covers = mix.covers.slice(0, 4);
  return (
    <li className="flex min-w-0 items-center gap-3 rounded-xl border border-border p-2.5">
      <span className="grid size-16 shrink-0 grid-cols-2 grid-rows-2 overflow-hidden rounded-lg bg-muted">
        {Array.from({ length: 4 }).map((_, i) => (
          <TrackCover key={i} track={covers[i] ? { source: "", songId: "", title: "", cover: covers[i] } : null} size={32} className="rounded-none" />
        ))}
      </span>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-sm font-medium" title={mix.title}>
          {mix.title}
        </span>
        <span className="truncate text-[11px] text-muted-foreground" title={mix.subtitle}>
          {mix.subtitle}
        </span>
        <span className="text-[11px] text-muted-foreground tabular-nums">
          {mix.size} 首{mix.lang ? `，${langLabel(mix.lang)}为主` : ""}
        </span>
      </span>
    </li>
  );
}

/* ── 听歌报告 ── */

const PERIODS: Array<{ value: MusicReportPeriod; label: string }> = (["week", "month", "year"] as const).map((p) => ({ value: p, label: REPORT_PERIOD_LABEL[p] }));
const MINUTES_CONFIG = { minutes: { label: "听歌分钟", color: "var(--color-violet-500, #8b5cf6)" } } satisfies ChartConfig;

export function ReportPanel({ appKey, userId }: { appKey: string; userId: number }) {
  const [period, setPeriod] = useState<MusicReportPeriod>("week");
  const [offset, setOffset] = useState(0);
  const query = useMusicUserReportQuery(appKey, userId, period, offset);
  const data = query.data;
  const max = MUSIC_REPORT_MAX_OFFSET[period];
  const word = period === "week" ? "周" : period === "month" ? "月" : "年";

  return (
    <Panel
      icon={BookHeart}
      tone="violet"
      title="听歌报告"
      aside={
        <>
          <Segmented
            value={period}
            options={PERIODS}
            onChange={(v) => {
              setPeriod(v);
              setOffset(0);
            }}
            ariaLabel="报告周期"
          />
          <Button size="icon-sm" variant="outline" aria-label={`上一${word}`} title={`上一${word}`} disabled={offset >= max} onClick={() => setOffset((o) => Math.min(max, o + 1))}>
            <ChevronLeft />
          </Button>
          <Button size="icon-sm" variant="outline" aria-label={`下一${word}`} title={`下一${word}`} disabled={offset <= 0} onClick={() => setOffset((o) => Math.max(0, o - 1))}>
            <ChevronRight />
          </Button>
        </>
      }
    >
      {query.isError && !data ? (
        <MusicError error={query.error} onRetry={() => query.refetch()} />
      ) : !data ? (
        <div className="h-56 animate-pulse rounded-xl bg-muted/60" />
      ) : (
        <div className={cn("flex min-w-0 flex-col gap-5", query.isFetching && query.isPlaceholderData && "opacity-60 transition-opacity")}>
          <ReportBody data={data} appKey={appKey} />
        </div>
      )}
    </Panel>
  );
}

function ReportBody({ data, appKey }: { data: MusicReport; appKey: string }) {
  if (data.empty) {
    return (
      <>
        <p className="text-sm font-medium">{data.label}</p>
        <EmptyLine>这一期没有听歌记录</EmptyLine>
      </>
    );
  }
  const figures: Array<{ label: string; value: string; delta?: number | null }> = [
    { label: "听歌时长", value: minutesText(data.listeningMinutes), delta: changeOf(data.listeningMinutes, data.previous.listeningMinutes) },
    { label: "播放", value: `${num(data.plays)} 次`, delta: changeOf(data.plays, data.previous.plays) },
    { label: "听过的歌", value: `${num(data.tracks)} 首` },
    { label: "听过的歌手", value: `${num(data.artists)} 位` },
    { label: "新听的歌", value: `${num(data.newTracks)} 首` },
    { label: "新认识的歌手", value: `${num(data.newArtists)} 位` },
    { label: "活跃天数", value: `${num(data.activeDays)} 天` }
  ];
  const isYear = data.period === "year";
  const hours = Array.from({ length: 24 }, (_, h) => ({ label: `${h}点`, minutes: Math.round(data.hours[h] ?? 0) }));
  const second = isYear
    ? Array.from({ length: 12 }, (_, m) => ({ label: `${m + 1}月`, minutes: Math.round(data.months[m] ?? 0) }))
    : WEEKDAY_LABEL.map((label, i) => ({ label, minutes: Math.round(data.weekdays[i] ?? 0) }));

  return (
    <>
      <div className="flex min-w-0 flex-col gap-1.5">
        <span className="text-xs text-muted-foreground">{data.label}</span>
        <h4 className="text-xl leading-7 font-semibold tracking-tight">{data.persona?.title || REPORT_PERIOD_LABEL[data.period]}</h4>
        {data.persona?.text ? <p className="text-sm leading-6 text-muted-foreground">{data.persona.text}</p> : null}
        {data.highlights.length ? (
          <ul className="mt-1 flex min-w-0 flex-col gap-1">
            {data.highlights.map((h) => (
              <li key={h} className="flex items-start gap-2 text-sm leading-6">
                <Sparkles className="mt-1 size-3.5 shrink-0 text-violet-500" />
                {h}
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      <dl className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4 @5xl:grid-cols-7">
        {figures.map((f) => (
          <div key={f.label} className="min-w-0 rounded-xl border border-border p-2.5">
            <dt className="truncate text-[11px] text-muted-foreground">{f.label}</dt>
            <dd className="mt-0.5 flex min-w-0 items-baseline gap-1.5">
              <span className="truncate text-base font-semibold tabular-nums">{f.value}</span>
              {f.delta !== undefined ? <Delta ratio={f.delta} /> : null}
            </dd>
          </div>
        ))}
      </dl>

      <Moments data={data} />

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-3">
        <ReportSection icon={Disc3} title="最常听的歌">
          <TrackList
            appKey={appKey}
            items={data.topTracks.slice(0, 10).map((t, i) => ({
              key: `${t.track.source}:${t.track.songId}:${i}`,
              track: t.track,
              right: `${num(t.plays)} 次`,
              sub: minutesText(t.minutes)
            }))}
          />
        </ReportSection>
        <ReportSection icon={MicVocal} title="最常听的歌手">
          {data.topArtists.length ? (
            <ul className="flex min-w-0 flex-col divide-y divide-border/60">
              {data.topArtists.slice(0, 10).map((a) => (
                <li key={a.name} className="flex min-w-0 items-center justify-between gap-3 py-2 first:pt-0 last:pb-0">
                  <ArtistCell name={a.name} cover={a.cover} href={musicArtistHref(appKey, a.name)} />
                  <span className="flex shrink-0 flex-col items-end text-[11px] leading-4 text-muted-foreground tabular-nums">
                    <span className="text-xs text-foreground">{num(a.plays)} 次</span>
                    {minutesText(a.minutes)}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyLine>暂无</EmptyLine>
          )}
        </ReportSection>
        <ReportSection icon={Sparkles} title="新认识的歌手">
          {data.discoveries.length ? (
            <ul className="flex min-w-0 flex-col divide-y divide-border/60">
              {data.discoveries.slice(0, 10).map((a) => (
                <li key={a.name} className="flex min-w-0 items-center justify-between gap-3 py-2 first:pt-0 last:pb-0">
                  <ArtistCell name={a.name} cover={a.cover} href={musicArtistHref(appKey, a.name)} sub={`${formatShortDateTime(a.firstAt)} 第一次听`} />
                  <span className="shrink-0 text-xs tabular-nums">{num(a.plays)} 次</span>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyLine>这一期没有新认识的歌手</EmptyLine>
          )}
        </ReportSection>
      </div>

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
        <MiniBars title="一天中的听歌时间" rows={hours} interval={2} />
        <MiniBars title={isYear ? "每月听歌时间" : "一周中的听歌时间"} rows={second} />
      </div>
    </>
  );
}

function ReportSection({ icon: Icon, title, children }: { icon: typeof Disc3; title: string; children: React.ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h5 className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Icon className="size-3.5" />
        {title}
      </h5>
      {children}
    </section>
  );
}

/** 这一期的几个时刻：第一首、最晚的深夜、单日循环最多、听得最多的一天 */
function Moments({ data }: { data: MusicReport }) {
  const cards: Array<{ key: string; icon: typeof Disc3; title: string; main: string; sub: string }> = [];
  if (data.firstTrack) {
    cards.push({
      key: "first",
      icon: Sunrise,
      title: "这一期的第一首",
      main: trackLine(data.firstTrack.track),
      sub: formatShortDateTime(data.firstTrack.at)
    });
  }
  if (data.latestNight) {
    cards.push({
      key: "night",
      icon: Moon,
      title: "最晚的一次深夜",
      main: trackLine(data.latestNight.track),
      sub: formatShortDateTime(data.latestNight.at)
    });
  }
  if (data.mostRepeated) {
    cards.push({
      key: "repeat",
      icon: Disc3,
      title: "单日循环最多",
      main: trackLine(data.mostRepeated.track),
      sub: `${longDay(data.mostRepeated.day)}听了 ${num(data.mostRepeated.plays)} 遍`
    });
  }
  if (data.peakDay) {
    cards.push({ key: "peak", icon: Sparkles, title: "听得最多的一天", main: longDay(data.peakDay.day), sub: minutesText(data.peakDay.minutes) });
  }
  if (!cards.length) return null;
  return (
    <ul className="grid min-w-0 gap-3 @2xl:grid-cols-2 @5xl:grid-cols-4">
      {cards.map((c) => (
        <li key={c.key} className="flex min-w-0 flex-col gap-1 rounded-xl bg-muted/40 px-3 py-2.5">
          <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <c.icon className="size-3.5" />
            {c.title}
          </span>
          <span className="truncate text-sm font-medium" title={c.main}>
            {c.main}
          </span>
          <span className="text-[11px] text-muted-foreground tabular-nums">{c.sub}</span>
        </li>
      ))}
    </ul>
  );
}

function trackLine(track: { title: string; artist?: string }) {
  return track.artist ? `${track.title}（${track.artist}）` : track.title || "未知歌曲";
}

function MiniBars({ title, rows, interval }: { title: string; rows: Array<{ label: string; minutes: number }>; interval?: number }) {
  const total = rows.reduce((s, r) => s + r.minutes, 0);
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <h5 className="text-xs font-medium text-muted-foreground">
        {title}
        {total ? <span className="ml-2 tabular-nums">共 {compact(total)} 分钟</span> : null}
      </h5>
      <ChartContainer config={MINUTES_CONFIG} className="aspect-auto h-[160px] w-full">
        <BarChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval={interval ?? 0} />
          <YAxis tickLine={false} axisLine={false} width={36} allowDecimals={false} />
          <ChartTooltip cursor={false} content={<ChartTooltipContent hideIndicator />} />
          <Bar dataKey="minutes" fill="var(--color-minutes)" radius={[3, 3, 0, 0]} />
        </BarChart>
      </ChartContainer>
    </section>
  );
}
