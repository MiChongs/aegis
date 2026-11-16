"use client";

import { useMemo, useState } from "react";
import { Bar, CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts";
import { CircleSlash, Search, TrendingUp } from "lucide-react";
import { shortDay } from "@/components/overview/overview-kit";
import { Panel, TabStack } from "@/components/traffic/traffic-kit";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useMusicSearchStatsQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { compact, num, pct } from "./music-format";
import { DaysSwitch, EmptyLine, MusicErrorPanel, PanelSkeleton } from "./music-kit";

/**
 * 搜索分析（GET /search?days=&limit=50）：搜了多少、点了多少，热门词，以及搜了却没有点任何结果的词
 * （多半是曲库缺歌或搜索结果不准，运营补歌的线索），点击率按天走势。
 */

const DAILY_CONFIG = {
  searches: { label: "搜索次数", color: "var(--color-sky-500, #0ea5e9)" },
  clickRate: { label: "点击率", color: "var(--color-violet-500, #8b5cf6)" }
} satisfies ChartConfig;

export function MusicSearch({ appKey }: { appKey: string }) {
  const [days, setDays] = useState(7);
  const query = useMusicSearchStatsQuery(appKey, days);
  const data = query.data;
  const rows = useMemo(() => (data?.daily ?? []).map((d) => ({ label: shortDay(d.day), searches: d.searches, clickRate: d.clickRate })), [data?.daily]);

  if (!data) {
    if (query.isError) return <MusicErrorPanel icon={Search} title="搜索" error={query.error} onRetry={() => query.refetch()} />;
    return (
      <TabStack>
        <PanelSkeleton height={120} />
        <PanelSkeleton height={240} />
      </TabStack>
    );
  }

  const noClickShare = data.searches ? data.noClick.reduce((s, q) => s + q.searches, 0) / data.searches : 0;

  return (
    <TabStack className={cn(query.isFetching && query.isPlaceholderData && "opacity-70 transition-opacity")}>
      <section className="flex min-w-0 flex-col gap-5 rounded-2xl border border-border bg-card p-4 sm:p-5">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <h3 className="text-xl leading-8 font-semibold tracking-tight text-balance">
              {data.searches ? `近 ${data.days} 天 ${num(data.searchers)} 人搜索了 ${compact(data.searches)} 次，${pct(data.clickRate)} 点了结果` : `近 ${data.days} 天没有搜索`}
            </h3>
            <p className="text-sm leading-6 text-muted-foreground">
              {data.noClick.length
                ? `${data.noClick.length} 个词搜了却没有点任何结果，合计占搜索的 ${pct(noClickShare)}，可能是曲库缺歌或结果不准。`
                : "没有搜了不点的词。"}
            </p>
          </div>
          <DaysSwitch value={days} onChange={setDays} />
        </div>
        <dl className="grid min-w-0 grid-cols-3 gap-3">
          <Figure label="搜索次数" value={compact(data.searches)} />
          <Figure label="搜索人数" value={num(data.searchers)} />
          <Figure label="点击率" value={pct(data.clickRate)} warn={data.searches > 0 && data.clickRate < 0.3} />
        </dl>
      </section>

      <Panel icon={TrendingUp} tone="sky" title="每日搜索与点击率">
        {!rows.length ? (
          <div className="grid h-[220px] place-items-center rounded-xl bg-muted/40 text-sm text-muted-foreground">暂无数据</div>
        ) : (
          <ChartContainer config={DAILY_CONFIG} className="aspect-auto h-[240px] w-full">
            <ComposedChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} />
              <YAxis yAxisId="count" tickLine={false} axisLine={false} width={40} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
              <YAxis yAxisId="rate" orientation="right" tickLine={false} axisLine={false} width={40} domain={[0, 1]} tickFormatter={(v: number) => `${Math.round(v * 100)}%`} />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    indicator="dot"
                    formatter={(value, name) => (
                      <span className="flex w-full items-center justify-between gap-3">
                        <span className="text-muted-foreground">{DAILY_CONFIG[name as keyof typeof DAILY_CONFIG]?.label ?? String(name)}</span>
                        <span className="font-mono font-medium tabular-nums">{name === "clickRate" ? pct(Number(value)) : num(Number(value))}</span>
                      </span>
                    )}
                  />
                }
              />
              <Bar yAxisId="count" dataKey="searches" fill="var(--color-searches)" fillOpacity={0.7} radius={[3, 3, 0, 0]} />
              <Line yAxisId="rate" dataKey="clickRate" type="monotone" stroke="var(--color-clickRate)" strokeWidth={2} dot={false} />
            </ComposedChart>
          </ChartContainer>
        )}
      </Panel>

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Panel icon={Search} tone="violet" title="热门搜索词" aside={<span className="text-xs text-muted-foreground tabular-nums">{data.top.length}</span>}>
          {!data.top.length ? (
            <EmptyLine>暂无搜索</EmptyLine>
          ) : (
            <div className="min-w-0 overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-10">排名</TableHead>
                    <TableHead>搜索词</TableHead>
                    <TableHead className="text-right">次数</TableHead>
                    <TableHead className="text-right">人数</TableHead>
                    <TableHead className="text-right">点击</TableHead>
                    <TableHead className="text-right">点击率</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.top.map((q, i) => (
                    <TableRow key={q.query}>
                      <TableCell className="text-xs text-muted-foreground tabular-nums">{i + 1}</TableCell>
                      <TableCell className="max-w-56 truncate text-xs font-medium" title={q.query}>
                        {q.query}
                      </TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{num(q.searches)}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{num(q.users)}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{num(q.clicks)}</TableCell>
                      <TableCell className={cn("text-right text-xs tabular-nums", q.clickRate < 0.2 && "text-amber-600 dark:text-amber-400")}>
                        {pct(q.clickRate)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </Panel>
        <Panel icon={CircleSlash} tone="amber" title="搜了没点的词" aside={<span className="text-xs text-muted-foreground tabular-nums">{data.noClick.length}</span>}>
          {!data.noClick.length ? (
            <EmptyLine>没有</EmptyLine>
          ) : (
            <ul className="flex min-w-0 flex-col divide-y divide-border/60">
              {data.noClick.map((q) => (
                <li key={q.query} className="flex min-w-0 items-center justify-between gap-3 py-2 first:pt-0 last:pb-0">
                  <span className="min-w-0 truncate text-xs font-medium" title={q.query}>
                    {q.query}
                  </span>
                  <span className="shrink-0 text-[11px] text-muted-foreground tabular-nums">
                    {num(q.searches)} 次，{num(q.users)} 人
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Panel>
      </div>
    </TabStack>
  );
}

function Figure({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-xl border border-border p-3">
      <dt className="truncate text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("text-2xl leading-8 font-semibold tracking-tight tabular-nums", warn && "text-amber-600 dark:text-amber-400")}>{value}</dd>
    </div>
  );
}
