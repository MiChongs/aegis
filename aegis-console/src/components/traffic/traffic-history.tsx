"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, Bar as RBar, CartesianGrid, ComposedChart, Line, LineChart, XAxis, YAxis } from "recharts";
import { Activity, BarChart3, Gauge, Timer } from "lucide-react";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Meter } from "@/components/apps/detail/app-detail-kit";
import type { HistoryRange } from "@/lib/api/traffic";
import { formatClock, formatWith } from "@/lib/datetime";
import { useTrafficConfigQuery, useTrafficHistoryQuery } from "@/lib/traffic-hooks";
import { Panel, Segmented, StatTile, Swatch, TabStack } from "./traffic-kit";
import { REJECT_PARTS, STREAM, fmtMs, fmtNum, fmtPct, rejectedOf } from "./traffic-format";

const RANGES: ReadonlyArray<{ value: HistoryRange; label: string }> = [
  { value: "1h", label: "1 小时" },
  { value: "6h", label: "6 小时" },
  { value: "24h", label: "24 小时" },
  { value: "7d", label: "7 天" }
];

const ALL = "__all";

const trafficConfig = {
  admitted: { label: "放行", theme: STREAM.admitted.chart },
  queued: { label: "排队", theme: STREAM.queued.chart },
  observed: { label: "观察", theme: STREAM.observed.chart },
  rejected: { label: "拒绝", theme: STREAM.rejected.chart }
} satisfies ChartConfig;

const latencyConfig = {
  p50: { label: "P50", theme: { light: "#0284c7", dark: "#38bdf8" } },
  p99: { label: "P99", theme: { light: "#f59e0b", dark: "#fbbf24" } }
} satisfies ChartConfig;

const peakConfig = {
  peakRps: { label: "峰值", theme: { light: "#52525b", dark: "#a1a1aa" } },
  maxInflight: { label: "最大在途", theme: { light: "#8b5cf6", dark: "#a78bfa" } }
} satisfies ChartConfig;

const REJECT_TONES = ["bg-rose-500", "bg-violet-500", "bg-amber-500", "bg-orange-500", "bg-red-700"];

export function TrafficHistoryTab() {
  const [range, setRange] = useState<HistoryRange>("1h");
  const [ruleId, setRuleId] = useState<string>(ALL);
  const configQuery = useTrafficConfigQuery();
  const historyQuery = useTrafficHistoryQuery(range, ruleId === ALL ? undefined : ruleId);
  const history = historyQuery.data;

  const tick = (ts: number) =>
    range === "7d" ? formatWith(ts * 1000, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : formatClock(ts * 1000);

  const data = useMemo(
    () =>
      (history?.points ?? []).map((point) => ({
        ts: point.ts,
        admitted: point.admitted,
        queued: point.queued,
        observed: point.observed,
        rejected: point.rejected,
        p50: point.p50,
        p99: point.p99,
        peakRps: point.peakRps,
        maxInflight: point.maxInflight
      })),
    [history?.points]
  );

  const totals = history?.totals;
  const rejected = rejectedOf(totals);
  const arrivals = totals?.arrivals ?? 0;

  const label = (value: unknown) => (typeof value === "number" ? formatWith(value * 1000, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : "");

  return (
    <TabStack>
      <div className="flex min-w-0 flex-wrap items-center gap-3 rounded-2xl border border-border bg-card px-4 py-3 sm:px-5">
        <Segmented value={range} options={RANGES} onChange={setRange} ariaLabel="时间范围" />
        <Select value={ruleId} onValueChange={setRuleId}>
          <SelectTrigger size="sm" className="w-48 max-w-full text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>全部流量</SelectItem>
            {(configQuery.data?.rules ?? []).map((rule) => (
              <SelectItem key={rule.id} value={rule.id}>
                {rule.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="grid min-w-0 grid-cols-2 gap-3 @xl:grid-cols-3 @4xl:grid-cols-5">
        <StatTile label="到达" value={totals?.arrivals} stream="arrivals" />
        <StatTile label="放行" value={totals?.admitted} stream="admitted" />
        <StatTile label="排队" value={totals?.queued} stream="queued" />
        <StatTile label="观察" value={totals?.observed} stream="observed" />
        <StatTile label="拒绝" value={rejected} stream="rejected" tone={rejected > 0 ? "danger" : "default"} className="col-span-2 @xl:col-span-1">
          <span className="text-[11px] text-muted-foreground tabular-nums">{arrivals > 0 ? fmtPct(rejected / arrivals) : "—"}</span>
        </StatTile>
      </div>

      {rejected > 0 && totals ? (
        <Panel icon={BarChart3} tone="rose" title="拒绝构成">
          <Meter
            className="h-2.5"
            segments={REJECT_PARTS.map((part, i) => ({ value: Number(totals[part.key] ?? 0), className: REJECT_TONES[i]!, label: part.label }))}
          />
          <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2">
            {REJECT_PARTS.map((part, i) => (
              <span key={part.key} className="inline-flex items-center gap-1.5 text-xs">
                <span aria-hidden className={`size-2 rounded-[3px] ${REJECT_TONES[i]}`} />
                <span className="text-muted-foreground">{part.label}</span>
                <span className="font-medium tabular-nums">{fmtNum(Number(totals[part.key] ?? 0))}</span>
              </span>
            ))}
          </div>
        </Panel>
      ) : null}

      <Panel
        icon={Activity}
        tone="primary"
        title="流量"
        aside={
          <div className="flex flex-wrap gap-x-3 gap-y-1">
            <Swatch stream="admitted" />
            <Swatch stream="queued" />
            <Swatch stream="observed" />
            <Swatch stream="rejected" />
          </div>
        }
      >
        <ChartFrame loading={historyQuery.isLoading} empty={data.length < 2} height={280}>
          <ChartContainer config={trafficConfig} className="aspect-auto h-[280px] w-full">
            <AreaChart data={data} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="ts" tickFormatter={tick} tickLine={false} axisLine={false} tickMargin={8} minTickGap={56} />
              <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(value: number) => fmtNum(value)} />
              <ChartTooltip content={<ChartTooltipContent indicator="dot" labelFormatter={(_, payload) => label(payload?.[0]?.payload?.ts)} />} />
              <Area dataKey="admitted" type="monotone" stackId="h" stroke="var(--color-admitted)" fill="var(--color-admitted)" fillOpacity={0.28} strokeWidth={1.5} />
              <Area dataKey="queued" type="monotone" stackId="h" stroke="var(--color-queued)" fill="var(--color-queued)" fillOpacity={0.3} strokeWidth={1.5} />
              <Area dataKey="observed" type="monotone" stackId="h" stroke="var(--color-observed)" fill="var(--color-observed)" fillOpacity={0.3} strokeWidth={1.5} />
              <Area dataKey="rejected" type="monotone" stackId="h" stroke="var(--color-rejected)" fill="var(--color-rejected)" fillOpacity={0.3} strokeWidth={1.5} />
            </AreaChart>
          </ChartContainer>
        </ChartFrame>
      </Panel>

      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
        <Panel
          icon={Timer}
          tone="sky"
          title="响应时间"
          aside={
            <div className="flex gap-3 text-xs text-muted-foreground">
              <LegendLine color="bg-sky-500" label="P50" />
              <LegendLine color="bg-amber-500" label="P99" />
            </div>
          }
        >
          <ChartFrame loading={historyQuery.isLoading} empty={data.length < 2} height={220}>
            <ChartContainer config={latencyConfig} className="aspect-auto h-[220px] w-full">
              <LineChart data={data} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="ts" tickFormatter={tick} tickLine={false} axisLine={false} tickMargin={8} minTickGap={56} />
                <YAxis tickLine={false} axisLine={false} width={52} tickFormatter={(value: number) => fmtMs(value)} />
                <ChartTooltip content={<ChartTooltipContent indicator="line" labelFormatter={(_, payload) => label(payload?.[0]?.payload?.ts)} />} />
                <Line dataKey="p50" type="monotone" stroke="var(--color-p50)" strokeWidth={1.75} dot={false} />
                <Line dataKey="p99" type="monotone" stroke="var(--color-p99)" strokeWidth={1.75} dot={false} />
              </LineChart>
            </ChartContainer>
          </ChartFrame>
        </Panel>

        <Panel
          icon={Gauge}
          tone="violet"
          title="峰值"
          aside={
            <div className="flex gap-3 text-xs text-muted-foreground">
              <LegendLine color="bg-zinc-500" label="峰值" />
              <LegendLine color="bg-violet-500" label="最大在途" />
            </div>
          }
        >
          <ChartFrame loading={historyQuery.isLoading} empty={data.length < 2} height={220}>
            <ChartContainer config={peakConfig} className="aspect-auto h-[220px] w-full">
              <ComposedChart data={data} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="ts" tickFormatter={tick} tickLine={false} axisLine={false} tickMargin={8} minTickGap={56} />
                <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(value: number) => fmtNum(value)} />
                <ChartTooltip content={<ChartTooltipContent indicator="dot" labelFormatter={(_, payload) => label(payload?.[0]?.payload?.ts)} />} />
                <RBar dataKey="peakRps" fill="var(--color-peakRps)" fillOpacity={0.5} radius={[2, 2, 0, 0]} />
                <Line dataKey="maxInflight" type="monotone" stroke="var(--color-maxInflight)" strokeWidth={1.75} dot={false} />
              </ComposedChart>
            </ChartContainer>
          </ChartFrame>
        </Panel>
      </div>
    </TabStack>
  );
}

function LegendLine({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden className={`h-0.5 w-3 rounded-full ${color}`} />
      {label}
    </span>
  );
}

function ChartFrame({
  loading,
  empty,
  height,
  children
}: {
  loading: boolean;
  empty: boolean;
  height: number;
  children: React.ReactNode;
}) {
  if (loading) return <Skeleton className="w-full rounded-xl" style={{ height }} />;
  if (empty) {
    return (
      <div className="grid place-items-center rounded-xl bg-muted/40 text-xs text-muted-foreground" style={{ minHeight: height }}>
        暂无数据
      </div>
    );
  }
  return <>{children}</>;
}
