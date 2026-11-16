"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { AlertTriangle, BarChart3, Palette, ScanText, Smile, Tags } from "lucide-react";
import { RingGauge } from "@/components/apps/detail/app-detail-kit";
import { longDay, shortDay } from "@/components/overview/overview-kit";
import { Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import type { MusicContentOverview } from "@/lib/api/music";
import { useMusicConfigQuery, useMusicContentQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { compact, num, pct } from "./music-format";
import { MusicContentTracks } from "./music-content-tracks";
import { MusicErrorPanel, PanelSkeleton, RankBars } from "./music-kit";
import { genreLabel, moodLabel } from "./music-tags";

const USAGE_VIEWS = [
  { value: "calls", label: "调用次数" },
  { value: "tokens", label: "token" },
  { value: "items", label: "处理首数" }
] as const;
type UsageView = (typeof USAGE_VIEWS)[number]["value"];

const USAGE_CONFIG = {
  calls: { label: "调用次数", color: "var(--color-sky-500, #0ea5e9)" },
  tokens: { label: "token", color: "var(--color-violet-500, #8b5cf6)" },
  items: { label: "处理首数", color: "var(--color-emerald-500, #10b981)" }
} satisfies ChartConfig;

/**
 * 内容理解（第三期，docs/music-personalization-3.md「二、内容理解」）：大模型给曲库打标签、算语义向量的进度与用量，
 * 曲风与情绪分布，以及作品标签列表（人工修改、重新打标签）。
 *
 * 没配置大模型时服务端不打标签、不做曲库扩展，这里在最上方明确说明，而不是让数字一直停在 0。
 */
export function MusicContent({ appKey, canManage }: { appKey: string; canManage: boolean }) {
  const query = useMusicContentQuery(appKey, 14);
  const config = useMusicConfigQuery(appKey);
  const data = query.data;
  const disabled = config.data?.content?.enabled === false;

  return (
    <TabStack>
      {!data ? (
        query.isError ? (
          <MusicErrorPanel icon={ScanText} title="内容理解" error={query.error} onRetry={() => query.refetch()} />
        ) : (
          <>
            <PanelSkeleton height={160} />
            <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
              <PanelSkeleton height={200} />
              <PanelSkeleton height={200} />
            </div>
          </>
        )
      ) : (
        <div className={cn("flex min-w-0 flex-col gap-4", query.isFetching && query.isPlaceholderData && "opacity-70 transition-opacity")}>
          {!data.llmConfigured ? (
            <Notice title="未配置大模型">
              music-trainer 与 music-rec 缺少 MUSIC_LLM_BASE_URL、MUSIC_LLM_API_KEY，新歌不会打标签、不会生成语义向量，大模型曲库扩展也不生效。
              深度模型照常训练，只是内容特征为零。配置后重启这两个服务即可开始处理。
            </Notice>
          ) : disabled ? (
            <Notice title="内容理解已关闭">应用配置里关闭了内容理解，新歌不会排队打标签；已有标签照常参与推荐。可在「配置」页签开启。</Notice>
          ) : null}
          <CoveragePanel data={data} />
          <UsagePanel data={data} />
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
            <DistributionPanel icon={Palette} title="曲风分布" rows={data.genres} label={genreLabel} barClassName="bg-sky-500" tagged={data.tagged} />
            <DistributionPanel icon={Smile} title="情绪分布" rows={data.moods} label={moodLabel} barClassName="bg-amber-500" tagged={data.tagged} />
          </div>
        </div>
      )}
      <MusicContentTracks appKey={appKey} canManage={canManage} />
    </TabStack>
  );
}

function Notice({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 items-start gap-3 rounded-2xl border border-amber-500/40 bg-amber-500/5 px-4 py-3 sm:px-5">
      <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
      <div className="flex min-w-0 flex-col gap-1">
        <span className="text-sm font-medium text-amber-800 dark:text-amber-200">{title}</span>
        <p className="text-xs leading-5 text-amber-800/90 dark:text-amber-200/90">{children}</p>
      </div>
    </div>
  );
}

/* ── 覆盖率 ── */

function CoveragePanel({ data }: { data: MusicContentOverview }) {
  const taggedRatio = data.tracks ? data.tagged / data.tracks : 0;
  const embeddedRatio = data.tracks ? data.embedded / data.tracks : 0;
  const head = data.tracks
    ? `曲库 ${num(data.tracks)} 首作品，已打标签 ${num(data.tagged)} 首，占 ${pct(taggedRatio)}`
    : "曲库里还没有作品";
  const sub = data.tracks
    ? [
        `有语义向量 ${num(data.embedded)} 首`,
        data.pending ? `待打标签 ${num(data.pending)} 首` : "没有待打标签的作品",
        data.failed ? `失败 ${num(data.failed)} 首（1 小时后重试，连续 3 次失败当天不再试）` : null
      ]
        .filter(Boolean)
        .join("；")
    : "客户端上报播放后作品会进入曲库，随后排队打标签。";
  return (
    <section className="flex min-w-0 flex-col gap-5 rounded-2xl border border-border bg-card p-4 sm:p-5">
      <div className="flex min-w-0 flex-col gap-2">
        <h3 className="text-xl leading-8 font-semibold tracking-tight text-balance">{head}</h3>
        <p className="text-sm leading-6 text-muted-foreground">{sub}。</p>
      </div>
      <div className="grid min-w-0 gap-4 @3xl:grid-cols-[auto_auto_minmax(0,1fr)] @3xl:items-center">
        <Ring label="已打标签" value={taggedRatio} count={`${num(data.tagged)} / ${num(data.tracks)}`} tone="sky" />
        <Ring label="有语义向量" value={embeddedRatio} count={`${num(data.embedded)} / ${num(data.tracks)}`} tone="violet" />
        <dl className="grid min-w-0 grid-cols-2 gap-3 @xl:grid-cols-4">
          <Figure label="待打标签" value={num(data.pending)} warn={data.pending > 0 && !data.llmConfigured} />
          <Figure label="失败" value={num(data.failed)} danger={data.failed > 0} />
          <Figure label="今日调用" value={compact(data.today.calls)} />
          <Figure label="今日 token" value={compact(data.today.tokens)} note={`处理 ${num(data.today.items)} 首`} />
        </dl>
      </div>
    </section>
  );
}

function Ring({ label, value, count, tone }: { label: string; value: number; count: string; tone: "sky" | "violet" }) {
  return (
    <div className="flex items-center gap-3">
      <RingGauge value={Number.isFinite(value) ? value * 100 : null} tone={tone} size={88} stroke={9}>
        <span className="text-sm font-semibold tabular-nums">{pct(value, 0)}</span>
      </RingGauge>
      <span className="flex flex-col">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-[11px] text-muted-foreground tabular-nums">{count}</span>
      </span>
    </div>
  );
}

function Figure({ label, value, note, warn, danger }: { label: string; value: string; note?: string; warn?: boolean; danger?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-xl border border-border p-3">
      <dt className="truncate text-xs text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          "truncate text-2xl leading-8 font-semibold tracking-tight tabular-nums",
          warn && "text-amber-600 dark:text-amber-400",
          danger && "text-rose-600 dark:text-rose-400"
        )}
      >
        {value}
      </dd>
      <span className="min-h-4 truncate text-[11px] leading-4 text-muted-foreground">{note}</span>
    </div>
  );
}

/* ── 用量 ── */

function UsagePanel({ data }: { data: MusicContentOverview }) {
  const [view, setView] = useState<UsageView>("calls");
  const rows = useMemo(() => data.daily.map((d) => ({ day: d.day, label: shortDay(d.day), calls: d.calls, tokens: d.tokens, items: d.items })), [data.daily]);
  const totals = rows.reduce((s, r) => ({ calls: s.calls + r.calls, tokens: s.tokens + r.tokens, items: s.items + r.items }), { calls: 0, tokens: 0, items: 0 });
  const peak = rows.reduce<(typeof rows)[number] | null>((best, r) => (r[view] > (best?.[view] ?? 0) ? r : best), null);
  return (
    <Panel
      icon={BarChart3}
      tone="sky"
      title={`近 ${rows.length || 14} 天大模型用量`}
      aside={<Segmented value={view} options={USAGE_VIEWS} onChange={setView} ariaLabel="用量指标" />}
    >
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-sm leading-5 text-muted-foreground">
          {totals.calls
            ? `共调用 ${num(totals.calls)} 次，消耗 ${compact(totals.tokens)} token，处理 ${num(totals.items)} 首${peak ? `；${longDay(peak.day)}${USAGE_CONFIG[view].label}最多` : ""}。`
            : "这段时间没有调用大模型。"}
        </p>
        {!rows.length ? (
          <div className="grid h-[220px] place-items-center rounded-xl bg-muted/40 text-sm text-muted-foreground">暂无数据</div>
        ) : (
          <ChartContainer config={USAGE_CONFIG} className="aspect-auto h-[220px] w-full">
            <BarChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={16} />
              <YAxis tickLine={false} axisLine={false} width={44} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
              <ChartTooltip cursor={false} content={<ChartTooltipContent hideIndicator />} />
              <Bar dataKey={view} fill={`var(--color-${view})`} radius={[3, 3, 0, 0]} />
            </BarChart>
          </ChartContainer>
        )}
      </div>
    </Panel>
  );
}

/* ── 分布 ── */

function DistributionPanel({
  icon,
  title,
  rows,
  label,
  barClassName,
  tagged
}: {
  icon: typeof Tags;
  title: string;
  rows: Array<{ key: string; count: number }>;
  label: (key: string) => string;
  barClassName: string;
  tagged: number;
}) {
  const sorted = [...rows].sort((a, b) => b.count - a.count).slice(0, 12);
  return (
    <Panel icon={icon} tone="muted" title={title} aside={rows.length > 12 ? <span className="text-xs text-muted-foreground">前 12 项</span> : null}>
      <RankBars
        barClassName={barClassName}
        empty="还没有打过标签"
        items={sorted.map((r) => ({
          key: r.key,
          label: label(r.key),
          value: r.count,
          // 一首歌最多 3 个，占比按已打标签的作品数算，各项相加会超过 100%
          display: `${num(r.count)} 首  ${pct(tagged ? r.count / tagged : 0, 0)}`,
          title: r.key
        }))}
      />
    </Panel>
  );
}
