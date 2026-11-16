"use client";

import { useMemo, useState } from "react";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import { shortDay } from "@/components/overview/overview-kit";
import { Segmented } from "@/components/traffic/traffic-kit";
import { Badge } from "@/components/ui/badge";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import type { MusicExperiment, MusicExperimentResults as Results } from "@/lib/api/music";
import { useMusicExperimentResultsQuery } from "@/lib/music-hooks";
import { cn } from "@/lib/utils";
import { EXPERIMENT_STATUS, SIGNIFICANCE, compact, minutesText, num, pValueText, pct, signedPct } from "./music-format";
import { VARIANT_COLOR, overridesText } from "./music-experiment-parts";
import { DaysSwitch, EmptyLine, MusicError, scenesText } from "./music-kit";

/**
 * 实验效果（GET /experiments/:id/results?days=）：各变体的规模与转化、相对对照（A）的提升与两比例 z 检验的 p 值，
 * 以及按天的点击率 / 播放率走势。p 值低于 0.05 记为显著，提升为正标绿、为负标红；样本不足时只给数字不下结论。
 */

type Metric = "ctr" | "playRate";
const METRICS: Array<{ value: Metric; label: string }> = [
  { value: "ctr", label: "点击率" },
  { value: "playRate", label: "播放率" }
];

export function MusicExperimentResults({
  appKey,
  experiment,
  onOpenChange
}: {
  appKey: string;
  experiment: MusicExperiment | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [days, setDays] = useState(14);
  const query = useMusicExperimentResultsQuery(appKey, experiment?.id ?? null, days);
  // 换了实验时，占位数据是上一个实验的，不拿来显示
  const data = query.data && !(query.isPlaceholderData && query.data.experimentId !== experiment?.id) ? query.data : undefined;
  const status = experiment ? EXPERIMENT_STATUS[experiment.status] : null;

  return (
    <Sheet open={experiment !== null} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-3xl">
        <SheetHeader className="border-b">
          <SheetTitle className="flex items-center gap-2">
            {experiment?.name ?? "实验效果"}
            {status ? (
              <Badge variant={status.tone} size="sm">
                {status.label}
              </Badge>
            ) : null}
          </SheetTitle>
          <SheetDescription>{experiment ? scenesText(experiment.scenes) : ""}</SheetDescription>
        </SheetHeader>
        <div className="@container flex min-w-0 flex-col gap-6 p-4">
          <div className="flex items-center justify-between gap-3">
            <span className="text-xs text-muted-foreground">统计近 {days} 天</span>
            <DaysSwitch value={days} onChange={setDays} />
          </div>
          {query.isError && !data ? (
            <MusicError error={query.error} onRetry={() => query.refetch()} />
          ) : !data ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-48 w-full" />
              <Skeleton className="h-60 w-full" />
            </div>
          ) : (
            <div className={cn("flex min-w-0 flex-col gap-6", query.isFetching && "opacity-70 transition-opacity")}>
              <Verdict data={data} />
              <VariantTable data={data} experiment={experiment} />
              <DailyChart data={data} />
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function isSignificant(p: number | undefined | null) {
  return typeof p === "number" && Number.isFinite(p) && p < SIGNIFICANCE;
}

/** 一句话结论：哪个变体在哪个指标上显著优于 / 劣于对照 */
function Verdict({ data }: { data: Results }) {
  const findings: Array<{ text: string; good: boolean }> = [];
  for (const v of data.variants) {
    if (!v.lift || !v.pValue) continue;
    for (const m of METRICS) {
      if (isSignificant(v.pValue[m.value])) {
        const lift = v.lift[m.value];
        findings.push({ text: `${v.key}（${v.name}）的${m.label}比对照${lift >= 0 ? "高" : "低"} ${signedPct(lift).replace(/^[+−]/, "")}`, good: lift >= 0 });
      }
    }
  }
  const totalUsers = data.variants.reduce((s, v) => s + v.users, 0);
  if (!totalUsers) return <EmptyLine>这段时间还没有用户进入实验</EmptyLine>;
  if (!findings.length) {
    return (
      <p className="rounded-xl border border-border px-4 py-3 text-sm leading-6 text-muted-foreground">
        共 {num(totalUsers)} 位用户进入实验，各变体与对照之间暂无显著差异（p ≥ {SIGNIFICANCE}），建议继续积累数据。
      </p>
    );
  }
  return (
    <ul className="flex min-w-0 flex-col gap-2">
      {findings.map((f) => (
        <li
          key={f.text}
          className={cn(
            "rounded-xl border px-4 py-3 text-sm leading-6 font-medium",
            f.good
              ? "border-emerald-500/30 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300"
              : "border-rose-500/30 bg-rose-500/5 text-rose-700 dark:text-rose-300"
          )}
        >
          显著：{f.text}
        </li>
      ))}
    </ul>
  );
}

function LiftCell({ value, lift, p }: { value: number; lift: number | undefined; p: number | undefined }) {
  const sig = isSignificant(p);
  return (
    <div className="flex flex-col items-end gap-0.5">
      <span className="text-xs font-medium tabular-nums">{pct(value)}</span>
      {lift === undefined ? (
        <span className="text-[11px] text-muted-foreground">对照</span>
      ) : (
        <span className="flex items-center gap-1 text-[11px] tabular-nums">
          <span
            className={cn(
              sig ? (lift >= 0 ? "font-medium text-emerald-600 dark:text-emerald-400" : "font-medium text-rose-600 dark:text-rose-400") : "text-muted-foreground"
            )}
          >
            {signedPct(lift)}
          </span>
          <span className="text-muted-foreground">p {pValueText(p)}</span>
          {sig ? (
            <Badge variant={lift >= 0 ? "success" : "danger"} size="sm">
              显著
            </Badge>
          ) : null}
        </span>
      )}
    </div>
  );
}

function VariantTable({ data, experiment }: { data: Results; experiment: MusicExperiment | null }) {
  const traffic = new Map((experiment?.variants ?? []).map((v) => [v.key, v]));
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h4 className="text-sm font-semibold">各变体效果</h4>
      <div className="min-w-0 overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>变体</TableHead>
              <TableHead className="text-right">用户</TableHead>
              <TableHead className="text-right">曝光</TableHead>
              <TableHead className="text-right">点击率</TableHead>
              <TableHead className="text-right">播放率</TableHead>
              <TableHead className="text-right">完播率</TableHead>
              <TableHead className="text-right">人均时长</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.variants.map((v, i) => {
              const def = traffic.get(v.key);
              return (
                <TableRow key={v.key}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span aria-hidden className="size-2.5 shrink-0 rounded-[3px]" style={{ background: VARIANT_COLOR[i % VARIANT_COLOR.length] }} />
                      <span className="text-xs font-medium">{v.key}</span>
                      <span className="max-w-32 truncate text-xs">{v.name}</span>
                    </div>
                    {def ? (
                      <p className="mt-0.5 max-w-56 truncate text-[11px] text-muted-foreground" title={overridesText(def)}>
                        {def.traffic}% 流量{overridesText(def) ? `，${overridesText(def)}` : ""}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{num(v.users)}</TableCell>
                  <TableCell className="text-right text-xs tabular-nums">
                    {compact(v.impressions)}
                    <span className="block text-[11px] text-muted-foreground">
                      点击 {compact(v.clicks)}，播放 {compact(v.plays)}
                    </span>
                  </TableCell>
                  <TableCell className="text-right">
                    <LiftCell value={v.ctr} lift={v.lift?.ctr} p={v.pValue?.ctr} />
                  </TableCell>
                  <TableCell className="text-right">
                    <LiftCell value={v.playRate} lift={v.lift?.playRate} p={v.pValue?.playRate} />
                  </TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{pct(v.completionRate)}</TableCell>
                  <TableCell className="text-right text-xs tabular-nums">{minutesText(v.minutesPerUser)}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
      <p className="text-[11px] text-muted-foreground">提升相对 A（对照）计算；p 值为两比例 z 检验，低于 {SIGNIFICANCE} 记为显著。</p>
    </section>
  );
}

function DailyChart({ data }: { data: Results }) {
  const [metric, setMetric] = useState<Metric>("ctr");
  const keys = data.variants.map((v) => v.key);
  const config = useMemo(
    () =>
      Object.fromEntries(
        data.variants.map((v, i) => [`v_${v.key}`, { label: `${v.key} ${v.name}`, color: VARIANT_COLOR[i % VARIANT_COLOR.length] }])
      ) satisfies ChartConfig,
    [data.variants]
  );
  const rows = useMemo(() => {
    const byDay = new Map<string, Record<string, number | string>>();
    for (const d of data.daily) {
      const row = byDay.get(d.day) ?? { day: d.day, label: shortDay(d.day) };
      row[`v_${d.variant}`] = d[metric];
      byDay.set(d.day, row);
    }
    return [...byDay.values()].sort((a, b) => String(a.day).localeCompare(String(b.day)));
  }, [data.daily, metric]);

  return (
    <section className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <h4 className="text-sm font-semibold">按天走势</h4>
        <Segmented value={metric} options={METRICS} onChange={setMetric} ariaLabel="走势指标" />
      </div>
      {!rows.length ? (
        <div className="grid h-[220px] place-items-center rounded-xl bg-muted/40 text-sm text-muted-foreground">暂无数据</div>
      ) : (
        <ChartContainer config={config} className="aspect-auto h-[240px] w-full">
          <LineChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={24} />
            <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(v: number) => pct(v, 0)} />
            <ChartTooltip content={<ChartTooltipContent indicator="dot" formatter={(value, name) => <RateRow name={config[String(name)]?.label ?? String(name)} value={Number(value)} />} />} />
            {keys.map((k) => (
              <Line key={k} dataKey={`v_${k}`} type="monotone" stroke={`var(--color-v_${k})`} strokeWidth={2} dot={false} connectNulls />
            ))}
          </LineChart>
        </ChartContainer>
      )}
    </section>
  );
}

function RateRow({ name, value }: { name: string; value: number }) {
  return (
    <span className="flex w-full items-center justify-between gap-3">
      <span className="text-muted-foreground">{name}</span>
      <span className="font-mono font-medium tabular-nums">{pct(value)}</span>
    </span>
  );
}
