"use client";

import { useMemo, useState, type ReactNode } from "react";
import NumberFlow, { type Format } from "@number-flow/react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Line, LineChart, Pie, PieChart, XAxis, YAxis } from "recharts";
import { Activity, ArrowRight, Brain, Clock3, Disc3, FlaskConical, Gauge, GitCompareArrows, Layers, MicVocal, Network, ScanText, Smartphone, ThumbsDown, TrendingUp } from "lucide-react";
import { RingGauge } from "@/components/apps/detail/app-detail-kit";
import { shortDay, longDay } from "@/components/overview/overview-kit";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { MusicOverviewContent, MusicOverview as Overview } from "@/lib/api/music";
import { formatDateTimeMinute, formatRelative } from "@/lib/datetime";
import { useMusicOverviewQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import {
  changeOf,
  changeWord,
  compact,
  dislikeReasonLabel,
  eventLabel,
  lagText,
  minutesText,
  num,
  pct,
  peakHour,
  platformLabel,
  sceneLabel,
  sourceLabel
} from "./music-format";
import { DaysSwitch, Delta, EmptyLine, MusicErrorPanel, PanelSkeleton, RankBars, TrackCell, musicTrackHref, scenesText } from "./music-kit";
import { DeepRankerBadge } from "./music-deep-parts";
import { ModelMetricsCompare } from "./music-model-parts";

/**
 * 音乐推荐总览：先用一句话说清这段时间的听歌情况，再按「趋势 → 行为构成 → 推荐效果（含按模型版本、运行中的实验）
 * → 内容与反馈 → 模型与管道 → 深度模型与内容理解（第三期）」展开。全部数据来自一个接口（/overview），
 * 同一时间窗下各块数字互相对得上。
 */

const TREND_VIEWS = [
  { value: "listeners", label: "听歌人数" },
  { value: "plays", label: "播放次数" },
  { value: "minutes", label: "听歌时长" },
  { value: "rates", label: "完播与跳过" }
] as const;
type TrendView = (typeof TREND_VIEWS)[number]["value"];

const TREND_CONFIG = {
  listeners: { label: "听歌人数", color: "var(--color-sky-500, #0ea5e9)" },
  plays: { label: "播放次数", color: "var(--color-violet-500, #8b5cf6)" },
  minutes: { label: "听歌分钟", color: "var(--color-emerald-500, #10b981)" },
  completion: { label: "完播率", color: "var(--color-emerald-500, #10b981)" },
  skip: { label: "跳过率", color: "var(--color-rose-500, #f43f5e)" }
} satisfies ChartConfig;

const HOURS_CONFIG = { plays: { label: "播放次数", color: "var(--color-sky-500, #0ea5e9)" } } satisfies ChartConfig;

/** 分类色轮：音源、平台这类无序类别 */
const WHEEL = [
  "var(--color-sky-500, #0ea5e9)",
  "var(--color-violet-500, #8b5cf6)",
  "var(--color-amber-500, #f59e0b)",
  "var(--color-emerald-500, #10b981)",
  "var(--color-rose-500, #f43f5e)",
  "var(--color-teal-500, #14b8a6)",
  "var(--color-zinc-400, #a1a1aa)"
];

export function MusicOverview({ appKey, onNavigate }: { appKey: string; onNavigate: (tab: string) => void }) {
  const [days, setDays] = useState(7);
  const query = useMusicOverviewQuery(appKey, days);
  const data = query.data;
  const daysSwitch = <DaysSwitch value={days} onChange={setDays} />;

  if (!data) {
    if (query.isError) return <MusicErrorPanel icon={Activity} title="总览" error={query.error} onRetry={() => query.refetch()} />;
    return (
      <TabStack>
        <PanelSkeleton height={180} />
        <PanelSkeleton height={260} />
        <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
          <PanelSkeleton height={220} />
          <PanelSkeleton height={220} />
        </div>
      </TabStack>
    );
  }

  return (
    <TabStack className={cn(query.isFetching && query.isPlaceholderData && "opacity-70 transition-opacity")}>
      <Hero data={data} aside={daysSwitch} />
      <TrendPanel data={data} />
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
        <EventTypesPanel data={data} onNavigate={onNavigate} />
        <HoursPanel hours={data.hours} />
      </div>
      <ScenesPanel data={data} />
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <ModelVersionsPanel data={data} />
        <ExperimentsPanel data={data} onNavigate={onNavigate} />
      </div>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <TopTracksPanel appKey={appKey} data={data} onNavigate={onNavigate} />
        <TopArtistsPanel data={data} />
      </div>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2 @6xl:grid-cols-3">
        <SourcesPanel data={data} />
        <PlatformsPanel data={data} />
        <FeedbackReasonsPanel data={data} />
      </div>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <ModelPanel data={data} onNavigate={onNavigate} />
        <PipelinePanel data={data} />
      </div>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <DeepModelPanel data={data} onNavigate={onNavigate} />
        <ContentCoveragePanel content={data.content} onNavigate={onNavigate} />
      </div>
    </TabStack>
  );
}

/* ── 一句话摘要 + 关键指标 ── */

function Hero({ data, aside }: { data: Overview; aside: ReactNode }) {
  const t = data.totals;
  const p = data.previous;
  const head = `近 ${data.days} 天`;
  const title = t.listeners
    ? `${head}有 ${num(t.listeners)} 人听歌，共播放 ${compact(t.plays)} 次，累计 ${minutesText(t.listeningMinutes)}`
    : `${head}还没有人听歌`;
  const sub = (() => {
    if (!t.listeners) return "客户端接入行为上报后，这里会汇总听歌人数、播放与推荐效果。";
    const parts: string[] = [];
    const c = changeOf(t.listeners, p.listeners);
    if (c !== null) parts.push(`听歌人数较前 ${data.days} 天${changeWord(c)}`);
    parts.push(`完播率 ${pct(t.completionRate)}，跳过率 ${pct(t.skipRate)}`);
    if (t.likes) parts.push(`新增收藏 ${num(t.likes)} 次`);
    if (t.searches) parts.push(`搜索 ${num(t.searches)} 次`);
    return `${parts.join("；")}。`;
  })();

  return (
    <section className="flex min-w-0 flex-col gap-5 rounded-2xl border border-border bg-card p-4 sm:p-5">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <h3 className="text-xl leading-8 font-semibold tracking-tight text-balance">{title}</h3>
          <p className="text-sm leading-6 text-muted-foreground">{sub}</p>
        </div>
        <div className="shrink-0">{aside}</div>
      </div>
      <div className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-3 @5xl:grid-cols-5">
        <Kpi label="听歌人数" value={t.listeners} delta={changeOf(t.listeners, p.listeners)} note={`前一周期 ${num(p.listeners)} 人`} />
        <Kpi label="播放次数" value={t.plays} delta={changeOf(t.plays, p.plays)} note={`前一周期 ${compact(p.plays)} 次`} />
        <Kpi
          label="听歌分钟"
          value={t.listeningMinutes}
          delta={changeOf(t.listeningMinutes, p.listeningMinutes)}
          note={`人均 ${t.listeners ? minutesText(t.listeningMinutes / t.listeners) : "—"}`}
        />
        <Kpi
          label="完播率"
          value={t.completionRate}
          format={{ style: "percent", maximumFractionDigits: 1 }}
          note="听到 80% 以上或自然播完的占比"
        />
        <Kpi
          label="跳过率"
          value={t.skipRate}
          format={{ style: "percent", maximumFractionDigits: 1 }}
          note="开头 30 秒内切走的占比"
          warn={t.skipRate >= 0.4}
        />
      </div>
    </section>
  );
}

function Kpi({
  label,
  value,
  format,
  delta,
  badWhenUp,
  note,
  warn
}: {
  label: string;
  value: number;
  format?: Format;
  delta?: number | null;
  badWhenUp?: boolean;
  note?: ReactNode;
  warn?: boolean;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-xl border border-border p-3">
      <span className="truncate text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 items-baseline gap-1.5">
        <span className={cn("text-2xl leading-8 font-semibold tracking-tight tabular-nums", warn && "text-amber-600 dark:text-amber-400")}>
          <NumberFlow value={Number.isFinite(value) ? value : 0} locales="zh-CN" format={format ?? { notation: "compact", maximumFractionDigits: 1 }} />
        </span>
        <Delta ratio={delta} badWhenUp={badWhenUp} />
      </div>
      <div className="min-h-4 truncate text-[11px] leading-4 text-muted-foreground">{note}</div>
    </div>
  );
}

/* ── 每日趋势 ── */

function TrendPanel({ data }: { data: Overview }) {
  const [view, setView] = useState<TrendView>("listeners");
  const rows = useMemo(
    () =>
      data.daily.map((d) => ({
        day: d.day,
        label: shortDay(d.day),
        listeners: d.listeners,
        plays: d.plays,
        minutes: Math.round(d.listeningMinutes),
        completion: d.completionRate,
        skip: d.skipRate
      })),
    [data.daily]
  );
  const key = view === "minutes" ? "minutes" : view === "plays" ? "plays" : "listeners";
  const peak = rows.reduce<(typeof rows)[number] | null>((best, r) => (r[key] > (best?.[key] ?? 0) ? r : best), null);
  const note = !rows.length
    ? "这段时间没有数据。"
    : view === "rates"
      ? `完播率在 ${pct(Math.min(...rows.map((r) => r.completion)))} 到 ${pct(Math.max(...rows.map((r) => r.completion)))} 之间。`
      : peak
        ? `${longDay(peak.day)}最多，${TREND_CONFIG[key].label} ${num(peak[key])}。`
        : "这段时间没有数据。";

  return (
    <Panel
      icon={TrendingUp}
      tone="sky"
      title="每日趋势"
      aside={<Segmented value={view} options={TREND_VIEWS} onChange={setView} ariaLabel="趋势指标" />}
    >
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-sm leading-5 text-muted-foreground">{note}</p>
        {!rows.length ? (
          <div className="grid h-[240px] place-items-center rounded-xl bg-muted/40 text-sm text-muted-foreground">暂无数据</div>
        ) : view === "rates" ? (
          <ChartContainer config={TREND_CONFIG} className="aspect-auto h-[240px] w-full">
            <LineChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} />
              <YAxis tickLine={false} axisLine={false} width={44} domain={[0, 1]} tickFormatter={(v: number) => `${Math.round(v * 100)}%`} />
              <ChartTooltip content={<ChartTooltipContent indicator="dot" formatter={rateFormatter} />} />
              <Line dataKey="completion" type="monotone" stroke="var(--color-completion)" strokeWidth={2} dot={false} />
              <Line dataKey="skip" type="monotone" stroke="var(--color-skip)" strokeWidth={2} dot={false} />
            </LineChart>
          </ChartContainer>
        ) : (
          <ChartContainer config={TREND_CONFIG} className="aspect-auto h-[240px] w-full">
            <AreaChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} />
              <YAxis tickLine={false} axisLine={false} width={44} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
              <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
              <Area dataKey={key} type="monotone" stroke={`var(--color-${key})`} fill={`var(--color-${key})`} fillOpacity={0.15} strokeWidth={2} />
            </AreaChart>
          </ChartContainer>
        )}
      </div>
    </Panel>
  );
}

function rateFormatter(value: unknown, name: unknown) {
  const label = TREND_CONFIG[name as keyof typeof TREND_CONFIG]?.label ?? String(name);
  return (
    <span className="flex w-full items-center justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono font-medium tabular-nums">{pct(Number(value))}</span>
    </span>
  );
}

/* ── 行为构成与时段 ── */

function EventTypesPanel({ data, onNavigate }: { data: Overview; onNavigate: (tab: string) => void }) {
  const items = [...data.eventTypes].sort((a, b) => b.count - a.count);
  const total = items.reduce((s, i) => s + i.count, 0);
  return (
    <Panel
      icon={Layers}
      tone="violet"
      title="行为构成"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("events")}>
          事件检索
          <ArrowRight />
        </Button>
      }
    >
      <RankBars
        barClassName="bg-violet-500"
        empty="暂无事件"
        items={items.map((i) => ({
          key: i.type,
          label: eventLabel(i.type),
          value: i.count,
          display: `${compact(i.count)}  ${pct(total ? i.count / total : 0)}`
        }))}
      />
    </Panel>
  );
}

function HoursPanel({ hours }: { hours: number[] }) {
  const rows = Array.from({ length: 24 }, (_, h) => ({ label: `${h}点`, plays: hours[h] ?? 0 }));
  const total = rows.reduce((s, r) => s + r.plays, 0);
  const peak = peakHour(hours);
  const night = rows.slice(22).concat(rows.slice(0, 2)).reduce((s, r) => s + r.plays, 0);
  return (
    <Panel icon={Clock3} tone="amber" title="24 小时分布">
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-sm leading-5 text-muted-foreground">
          {total ? `${peak} 点播放最多；22 点到次日 2 点占 ${pct(night / total)}。` : "暂无播放。"}
        </p>
        <ChartContainer config={HOURS_CONFIG} className="aspect-auto h-[200px] w-full">
          <BarChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} interval={2} />
            <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
            <ChartTooltip cursor={false} content={<ChartTooltipContent hideIndicator />} />
            <Bar dataKey="plays" fill="var(--color-plays)" radius={[3, 3, 0, 0]} />
          </BarChart>
        </ChartContainer>
      </div>
    </Panel>
  );
}

/* ── 推荐场景漏斗 ── */

const FUNNEL = [
  { key: "requests", label: "请求", bar: "bg-sky-500/70" },
  { key: "impressions", label: "曝光", bar: "bg-sky-500" },
  { key: "clicks", label: "点击", bar: "bg-violet-500" },
  { key: "plays", label: "播放", bar: "bg-emerald-500" }
] as const;

function ScenesPanel({ data }: { data: Overview }) {
  const scenes = data.scenes;
  const best = [...scenes].filter((s) => s.impressions > 0).sort((a, b) => b.playRate - a.playRate)[0];
  return (
    <Panel icon={Gauge} tone="emerald" title="推荐效果">
      <div className="flex min-w-0 flex-col gap-4">
        <p className="text-sm leading-5 text-muted-foreground">
          {best ? `${sceneLabel(best.scene)}的播放率最高，曝光后 ${pct(best.playRate)} 被播放。` : "推荐结果还没有被展示过。"}
        </p>
        {!scenes.length ? (
          <EmptyLine>暂无推荐请求</EmptyLine>
        ) : (
          <div className="grid min-w-0 gap-3 @3xl:grid-cols-2">
            {scenes.map((s) => {
              const logMax = Math.log10(Math.max(s.requests, s.impressions) + 1) || 1;
              return (
                <div key={s.scene} className="flex min-w-0 flex-col gap-3 rounded-xl border border-border p-3">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <span className="truncate text-sm font-medium">{sceneLabel(s.scene)}</span>
                    <span className="flex shrink-0 gap-1.5">
                      <Badge variant="info" size="sm">
                        点击率 {pct(s.ctr)}
                      </Badge>
                      <Badge variant="success" size="sm">
                        播放率 {pct(s.playRate)}
                      </Badge>
                    </span>
                  </div>
                  <ol className="flex min-w-0 flex-col gap-1.5">
                    {FUNNEL.map((step) => {
                      const value = s[step.key];
                      const width = value > 0 ? 8 + 92 * (Math.log10(value + 1) / logMax) : 0;
                      return (
                        <li key={step.key} className="grid min-w-0 grid-cols-[2.5rem_minmax(0,1fr)_4.5rem] items-center gap-2">
                          <span className="text-[11px] text-muted-foreground">{step.label}</span>
                          <span className="h-5 min-w-0 overflow-hidden rounded-md bg-muted/60">
                            <span className={cn("block h-full rounded-md transition-[width] duration-700", step.bar)} style={{ width: `${width}%` }} />
                          </span>
                          <span className="text-right text-xs font-medium tabular-nums">{compact(value)}</span>
                        </li>
                      );
                    })}
                  </ol>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Panel>
  );
}

/* ── 按模型版本与实验 ── */

function ModelVersionsPanel({ data }: { data: Overview }) {
  // 服务端升级前没有这个字段，按空处理
  const rows = [...(data.models ?? [])].sort((a, b) => b.version - a.version);
  const maxCtr = Math.max(0.0001, ...rows.map((r) => r.ctr));
  const current = data.model?.id;
  const best = rows.filter((r) => r.impressions > 0).sort((a, b) => b.ctr - a.ctr)[0];
  return (
    <Panel icon={GitCompareArrows} tone="violet" title="按模型版本的效果">
      {!rows.length ? (
        <EmptyLine>暂无推荐请求</EmptyLine>
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          <p className="text-sm leading-5 text-muted-foreground">
            {best
              ? `${best.version ? `第 ${best.version} 版` : "未使用模型的请求"}点击率最高，为 ${pct(best.ctr)}${rows.length > 1 ? `；这段时间共有 ${rows.length} 个版本在线上出过推荐` : ""}。`
              : "推荐结果还没有被展示过。"}
          </p>
          <ul className="flex min-w-0 flex-col gap-2">
            {rows.map((r) => (
              <li key={r.version} className="grid min-w-0 grid-cols-[6.5rem_minmax(0,1fr)_auto] items-center gap-3">
                <span className="flex min-w-0 items-center gap-1.5 text-xs">
                  <span className="truncate font-medium">{r.version ? `第 ${r.version} 版` : "未使用模型"}</span>
                  {r.version && r.version === current ? (
                    <Badge variant="success" size="sm">
                      当前
                    </Badge>
                  ) : null}
                </span>
                <span className="h-2 min-w-0 overflow-hidden rounded-full bg-muted" title={`点击率 ${pct(r.ctr)}`}>
                  <span className="block h-full rounded-full bg-violet-500 transition-[width] duration-500" style={{ width: `${(r.ctr / maxCtr) * 100}%` }} />
                </span>
                <span className="flex flex-col items-end text-[11px] leading-4 text-muted-foreground tabular-nums">
                  <span className="text-xs font-medium text-foreground">点击率 {pct(r.ctr)}</span>
                  <span>
                    播放率 {pct(r.playRate)}，曝光 {compact(r.impressions)}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Panel>
  );
}

function ExperimentsPanel({ data, onNavigate }: { data: Overview; onNavigate: (tab: string) => void }) {
  const items = data.experiments ?? [];
  return (
    <Panel
      icon={FlaskConical}
      tone="emerald"
      title="运行中的实验"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("experiments")}>
          全部实验
          <ArrowRight />
        </Button>
      }
    >
      {!items.length ? (
        <EmptyLine>没有运行中的实验</EmptyLine>
      ) : (
        <ul className="flex min-w-0 flex-col divide-y divide-border/60">
          {items.map((e) => (
            <li key={e.id} className="flex min-w-0 flex-col gap-0.5 py-2 first:pt-0 last:pb-0">
              <div className="flex min-w-0 items-center justify-between gap-2">
                <span className="truncate text-sm font-medium">{e.name}</span>
                <Badge variant="secondary" size="sm">
                  {e.variants} 个变体
                </Badge>
              </div>
              <span className="truncate text-[11px] text-muted-foreground" title={formatDateTimeMinute(e.startedAt)}>
                {scenesText(e.scenes)}，{formatRelative(e.startedAt)}开始
              </span>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

/* ── 热门内容 ── */

function TopTracksPanel({ appKey, data, onNavigate }: { appKey: string; data: Overview; onNavigate: (tab: string) => void }) {
  const items = data.topTracks.slice(0, 10);
  return (
    <Panel
      icon={Disc3}
      tone="violet"
      title="热门作品"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("tracks")}>
          作品排行
          <ArrowRight />
        </Button>
      }
    >
      {!items.length ? (
        <EmptyLine>暂无播放</EmptyLine>
      ) : (
        <ol className="flex min-w-0 flex-col divide-y divide-border/60">
          {items.map((item, index) => (
            <li key={item.workKey} className="grid min-w-0 grid-cols-[1.25rem_minmax(0,1fr)_auto] items-center gap-3 py-2 first:pt-0 last:pb-0">
              <span className={cn("text-xs font-semibold tabular-nums", index < 3 ? "text-foreground" : "text-muted-foreground")}>{index + 1}</span>
              <TrackCell track={item.track} href={musicTrackHref(appKey, item.workKey)} />
              <span className="flex flex-col items-end text-[11px] leading-4 text-muted-foreground tabular-nums">
                <span className="text-xs font-medium text-foreground">{compact(item.plays)} 次</span>
                <span>
                  {compact(item.listeners)} 人，完播 {pct(item.completionRate, 0)}
                </span>
              </span>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}

function TopArtistsPanel({ data }: { data: Overview }) {
  return (
    <Panel icon={MicVocal} tone="amber" title="热门歌手">
      <RankBars
        barClassName="bg-amber-500"
        empty="暂无播放"
        items={data.topArtists.slice(0, 10).map((a) => ({
          key: a.name,
          label: a.name,
          value: a.plays,
          display: `${compact(a.plays)} 次`,
          title: `${a.name}：${num(a.plays)} 次播放，${num(a.listeners)} 位听众`
        }))}
      />
    </Panel>
  );
}

/* ── 音源与平台 ── */

function SourcesPanel({ data }: { data: Overview }) {
  const rows = [...data.sources].sort((a, b) => b.plays - a.plays);
  const total = rows.reduce((s, r) => s + r.plays, 0);
  const config = Object.fromEntries(rows.map((r, i) => [r.source, { label: sourceLabel(r.source), color: WHEEL[i % WHEEL.length] }])) satisfies ChartConfig;
  return (
    <Panel icon={Layers} tone="sky" title="音源分布">
      {!total ? (
        <EmptyLine>暂无播放</EmptyLine>
      ) : (
        <div className="grid min-w-0 items-center gap-4 @lg:grid-cols-[180px_minmax(0,1fr)]">
          <ChartContainer config={config} className="mx-auto aspect-square h-[180px]">
            <PieChart>
              <ChartTooltip content={<ChartTooltipContent hideLabel nameKey="source" />} />
              <Pie data={rows} dataKey="plays" nameKey="source" innerRadius={52} outerRadius={80} strokeWidth={2}>
                {rows.map((r, i) => (
                  <Cell key={r.source} fill={WHEEL[i % WHEEL.length]} />
                ))}
              </Pie>
            </PieChart>
          </ChartContainer>
          <ul className="flex min-w-0 flex-col gap-2">
            {rows.map((r, i) => (
              <li key={r.source} className="flex min-w-0 items-center gap-2 text-xs">
                <span aria-hidden className="size-2.5 shrink-0 rounded-[3px]" style={{ background: WHEEL[i % WHEEL.length] }} />
                <span className="min-w-0 flex-1 truncate">{sourceLabel(r.source)}</span>
                <span className="text-muted-foreground tabular-nums">{compact(r.plays)} 次</span>
                <span className="w-12 text-right font-medium tabular-nums">{pct(r.plays / total, 0)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Panel>
  );
}

function PlatformsPanel({ data }: { data: Overview }) {
  const rows = [...data.platforms].sort((a, b) => b.listeners - a.listeners);
  return (
    <Panel icon={Smartphone} tone="emerald" title="平台分布">
      <RankBars
        barClassName="bg-emerald-500"
        empty="暂无听众"
        items={rows.map((r) => ({ key: r.platform || "unknown", label: platformLabel(r.platform), value: r.listeners, display: `${num(r.listeners)} 人` }))}
      />
    </Panel>
  );
}

function FeedbackReasonsPanel({ data }: { data: Overview }) {
  const rows = [...(data.feedbackReasons ?? [])].sort((a, b) => b.count - a.count);
  const total = rows.reduce((s, r) => s + r.count, 0);
  return (
    <Panel icon={ThumbsDown} tone="rose" title="不感兴趣的原因">
      <RankBars
        barClassName="bg-rose-500"
        empty="暂无不感兴趣"
        items={rows.map((r) => ({
          key: r.reason || "not_interested",
          label: dislikeReasonLabel(r.reason),
          value: r.count,
          display: `${compact(r.count)}  ${pct(total ? r.count / total : 0, 0)}`
        }))}
      />
    </Panel>
  );
}

/* ── 模型与管道 ── */

function ModelPanel({ data, onNavigate }: { data: Overview; onNavigate: (tab: string) => void }) {
  const model = data.model;
  return (
    <Panel
      icon={Brain}
      tone="violet"
      title="当前模型"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("models")}>
          训练记录
          <ArrowRight />
        </Button>
      }
    >
      {!model ? (
        <EmptyLine>尚未发布模型，推荐暂时只给近期热门</EmptyLine>
      ) : (
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-1">
            <span className="text-2xl font-semibold tracking-tight tabular-nums">第 {model.id} 版</span>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="text-xs text-muted-foreground">发布于 {formatRelative(model.publishedAt)}</span>
              </TooltipTrigger>
              <TooltipContent>{formatDateTimeMinute(model.publishedAt)}</TooltipContent>
            </Tooltip>
            <span className="text-xs text-muted-foreground tabular-nums">
              {num(model.users)} 位用户，{num(model.items)} 首作品，{compact(model.interactions)} 条交互
            </span>
          </div>
          {model.metrics ? <ModelMetricsCompare metrics={model.metrics} /> : <EmptyLine>这个版本没有评估结果</EmptyLine>}
        </div>
      )}
    </Panel>
  );
}

function PipelinePanel({ data }: { data: Overview }) {
  const { lagSeconds, pending, lastEventAt } = data.pipeline;
  const state = lagSeconds >= 600 || pending >= 50_000 ? "danger" : lagSeconds >= 60 || pending >= 5_000 ? "warning" : "success";
  const word = state === "success" ? "处理及时" : state === "warning" ? "略有积压" : "积压严重";
  return (
    <Panel icon={Activity} tone="emerald" title="数据处理" aside={<Badge variant={state} size="sm">{word}</Badge>}>
      <dl className="grid min-w-0 grid-cols-2 gap-3">
        <div className="flex min-w-0 flex-col gap-1 rounded-xl border border-border p-3">
          <dt className="text-xs text-muted-foreground">待处理事件</dt>
          <dd className="text-2xl leading-8 font-semibold tabular-nums">{compact(pending)}</dd>
        </div>
        <div className="flex min-w-0 flex-col gap-1 rounded-xl border border-border p-3">
          <dt className="text-xs text-muted-foreground">处理延迟</dt>
          <dd className="text-2xl leading-8 font-semibold tabular-nums">{lagText(lagSeconds)}</dd>
        </div>
        <div className="col-span-2 flex min-w-0 items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>最近一条事件</span>
          <span className="tabular-nums" title={formatDateTimeMinute(lastEventAt)}>
            {lastEventAt ? formatRelative(lastEventAt) : "暂无"}
          </span>
        </div>
      </dl>
    </Panel>
  );
}

/* ── 第三期：深度模型与内容理解 ── */

const DEEP_METRIC_KEYS = ["ndcg20", "recall20", "hitRate20"] as const;

function DeepModelPanel({ data, onNavigate }: { data: Overview; onNavigate: (tab: string) => void }) {
  // 服务端升级前没有这个字段，按「未发布」处理
  const deep = data.deep ?? null;
  return (
    <Panel
      icon={Network}
      tone="violet"
      title="当前深度模型"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("deep")}>
          训练记录
          <ArrowRight />
        </Button>
      }
    >
      {!deep ? (
        <EmptyLine>尚未发布深度模型，序列模型通道与 LightGBM 精排暂不参与推荐</EmptyLine>
      ) : (
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-1">
            <span className="text-2xl font-semibold tracking-tight tabular-nums">第 {deep.id} 版</span>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="text-xs text-muted-foreground">发布于 {formatRelative(deep.publishedAt)}</span>
              </TooltipTrigger>
              <TooltipContent>{formatDateTimeMinute(deep.publishedAt)}</TooltipContent>
            </Tooltip>
            <span className="text-xs text-muted-foreground tabular-nums">
              {num(deep.users)} 位用户，{num(deep.items)} 首作品，{compact(deep.sequences ?? 0)} 条序列
            </span>
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              精排
              <DeepRankerBadge ranker={deep.ranker ?? null} />
            </span>
          </div>
          {deep.metrics ? (
            <ModelMetricsCompare metrics={deep.metrics} keys={DEEP_METRIC_KEYS} note="以每人最后一个正向行为为目标、在全部作品上打分" />
          ) : (
            <EmptyLine>这个版本没有评估结果</EmptyLine>
          )}
        </div>
      )}
    </Panel>
  );
}

/** 契约只写了「已打标签占比」：兼容比例数字与带计数的对象两种形状 */
function contentCoverage(content: MusicOverviewContent) {
  if (typeof content === "number") return Number.isFinite(content) ? { ratio: content } : null;
  if (!content || typeof content !== "object") return null;
  const { tracks, tagged, embedded } = content;
  const ratio = typeof content.ratio === "number" ? content.ratio : tracks ? (tagged ?? 0) / tracks : null;
  if (ratio === null || !Number.isFinite(ratio)) return null;
  return { ratio, tracks, tagged, embeddedRatio: tracks && typeof embedded === "number" ? embedded / tracks : undefined };
}

function ContentCoveragePanel({ content, onNavigate }: { content: MusicOverviewContent; onNavigate: (tab: string) => void }) {
  const cov = contentCoverage(content);
  return (
    <Panel
      icon={ScanText}
      tone="sky"
      title="内容理解覆盖率"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("content")}>
          内容理解
          <ArrowRight />
        </Button>
      }
    >
      {!cov ? (
        <EmptyLine>暂无内容理解数据</EmptyLine>
      ) : (
        <div className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-4">
          <div className="flex items-center gap-3">
            <RingGauge value={cov.ratio * 100} tone="sky" size={88} stroke={9}>
              <span className="text-sm font-semibold tabular-nums">{pct(cov.ratio, 0)}</span>
            </RingGauge>
            <span className="flex flex-col">
              <span className="text-sm font-medium">已打标签</span>
              {typeof cov.tracks === "number" ? (
                <span className="text-[11px] text-muted-foreground tabular-nums">
                  {num(cov.tagged ?? 0)} / {num(cov.tracks)} 首
                </span>
              ) : null}
            </span>
          </div>
          {typeof cov.embeddedRatio === "number" ? (
            <div className="flex items-center gap-3">
              <RingGauge value={cov.embeddedRatio * 100} tone="violet" size={88} stroke={9}>
                <span className="text-sm font-semibold tabular-nums">{pct(cov.embeddedRatio, 0)}</span>
              </RingGauge>
              <span className="text-sm font-medium">有语义向量</span>
            </div>
          ) : null}
          <p className="w-full text-[11px] text-muted-foreground">没有标签的作品在序列模型里只靠行为与歌手、语种表示，冷门与新歌更难被推荐。</p>
        </div>
      )}
    </Panel>
  );
}
