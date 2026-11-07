"use client";

import { useMemo, useState } from "react";
import NumberFlow from "@number-flow/react";
import { Area, CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts";
import {
  Activity,
  AlertTriangle,
  Cpu,
  Flame,
  History,
  Loader2,
  RotateCcw,
  Server,
  Waves
} from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { CardEmpty } from "@/components/overview/overview-kit";
import type { HotKey, NodeSummary, TrafficEvent, TrafficOverview } from "@/lib/api/traffic";
import { formatClockSeconds, formatCompact, formatRelative } from "@/lib/datetime";
import {
  useNativePluginsQuery,
  useResetTrafficStatsMutation,
  useTrafficConfigQuery,
  useTrafficEventsQuery,
  useTrafficOverviewQuery,
  trafficErrorMessage
} from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { FlowGate, type FlowGateRule } from "./flow-gate";
import { TrafficStateBanner } from "./plugin-banner";
import { ArcGauge, Bar, Figure, Panel, Segmented, StatTile, Swatch, TabStack } from "./traffic-kit";
import { EVENT_LABEL, KEY_LABEL, STREAM, fmtMs, fmtNum, rejectedOf } from "./traffic-format";

const RANGES = [
  { value: "60", label: "1 分钟" },
  { value: "300", label: "5 分钟" },
  { value: "900", label: "15 分钟" }
] as const;

type RangeValue = (typeof RANGES)[number]["value"];

export function TrafficOverviewTab() {
  const [range, setRange] = useState<RangeValue>("60");
  const overviewQuery = useTrafficOverviewQuery(Number(range));
  const configQuery = useTrafficConfigQuery();
  const pluginsQuery = useNativePluginsQuery();
  const overview = overviewQuery.data;
  const settings = configQuery.data?.settings;
  const plugin = overview?.plugin ?? pluginsQuery.data?.find((item) => item.id === "traffic") ?? null;

  if (!overview) {
    return (
      <TabStack>
        {overviewQuery.isError ? <TrafficStateBanner plugin={plugin} settings={settings} /> : null}
        {overviewQuery.isError ? (
          <Panel icon={AlertTriangle} tone="rose" title="运行态读取失败">
            <p className="text-sm text-muted-foreground">{trafficErrorMessage(overviewQuery.error, "读取失败")}</p>
          </Panel>
        ) : (
          <>
            <Skeleton className="h-[372px] w-full rounded-2xl" />
            <div className="grid grid-cols-2 gap-3 @xl:grid-cols-4 @5xl:grid-cols-7">
              {Array.from({ length: 7 }).map((_, i) => (
                <Skeleton key={i} className="h-[92px] rounded-xl" />
              ))}
            </div>
            <Skeleton className="h-[340px] w-full rounded-2xl" />
          </>
        )}
      </TabStack>
    );
  }

  return (
    <TabStack>
      <TrafficStateBanner plugin={plugin} settings={settings} />
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_288px]">
        <FlowPanel overview={overview} paused={settings ? !settings.enabled : !overview.local.enabled} />
        <LoadPanel overview={overview} />
      </div>
      <KpiRow overview={overview} />
      <LivePanel overview={overview} range={range} onRange={setRange} />
      <NodesPanel overview={overview} />
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
        <HotKeysPanel overview={overview} />
        <EventsPanel />
      </div>
    </TabStack>
  );
}

/* ── 闸门 ── */

function FlowPanel({ overview, paused }: { overview: TrafficOverview; paused: boolean }) {
  const local = overview.local;
  const last = local.lastSecond;
  const reset = useResetTrafficStatsMutation();
  const gates: FlowGateRule[] = (local.rules ?? [])
    .filter((rule) => rule.enabled)
    .map((rule) => ({
      id: rule.id,
      name: rule.name,
      mode: rule.mode,
      arrivals: rule.lastSecond?.arrivals ?? 0,
      rejected: rejectedOf(rule.lastSecond),
      observed: rule.lastSecond?.observed ?? 0
    }));

  return (
    <Panel
      icon={Waves}
      tone="sky"
      title="闸门"
      aside={
        <>
          <Badge variant="outline" size="sm">
            {overview.cluster.coordinator === "redis" ? `集群 ${overview.cluster.instances} 实例` : "单机"}
          </Badge>
          <Badge variant="outline" size="sm" className="font-mono">
            r{overview.revision}
          </Badge>
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button size="icon-sm" variant="ghost" aria-label="重置统计" disabled={reset.isPending}>
                {reset.isPending ? <Loader2 className="animate-spin" /> : <RotateCcw />}
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>重置统计</AlertDialogTitle>
                <AlertDialogDescription>本实例的累计计数与分位将清零。</AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>取消</AlertDialogCancel>
                <AlertDialogAction onClick={() => reset.mutate()}>重置</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </>
      }
    >
      <FlowGate
        arrivals={last.arrivals}
        admitted={last.admitted}
        queued={last.queued}
        observed={last.observed}
        rejected={rejectedOf(last)}
        gates={gates}
        paused={paused}
        overload={local.overload.active}
      />
    </Panel>
  );
}

/* ── 负载 ── */

function LoadPanel({ overview }: { overview: TrafficOverview }) {
  const o = overview.local.overload;
  const capacity = Math.max(0, o.capacity);
  const inflight = overview.local.inflight;
  const inflightMax = Math.max(capacity * 1.25, inflight, 1);
  const state = !o.enabled ? "未开启" : o.active ? "保护中" : "正常";

  return (
    <Panel
      icon={Cpu}
      tone={o.active ? "rose" : "emerald"}
      title="负载"
      aside={
        <Badge variant={!o.enabled ? "outline" : o.active ? "danger" : "success"} size="sm">
          {state}
        </Badge>
      }
    >
      <div className="grid grid-cols-2 gap-2 @4xl:grid-cols-1">
        <div className="flex justify-center">
          <ArcGauge
            value={o.cpu}
            max={100}
            threshold={o.threshold}
            label="CPU"
            display={<NumberFlow value={Math.round(o.cpu)} suffix="%" />}
            sub={`阈值 ${o.threshold}%`}
            size={136}
          />
        </div>
        <div className="flex justify-center">
          <ArcGauge
            value={inflight}
            max={inflightMax}
            threshold={capacity > 0 ? capacity : undefined}
            label="在途"
            display={<NumberFlow value={inflight} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />}
            sub={capacity > 0 ? `承载 ${fmtNum(Math.round(capacity))}` : "承载未知"}
            size={136}
          />
        </div>
      </div>
      <dl className="mt-4 grid grid-cols-2 gap-x-3 gap-y-3 border-t border-border pt-4">
        <Figure label="已丢弃" value={fmtNum(o.dropped)} valueClassName={o.dropped > 0 ? "text-rose-600 dark:text-rose-400" : undefined} />
        <Figure label="最大吞吐" value={`${fmtNum(o.maxPass)}/秒`} />
        <Figure label="最小响应" value={fmtMs(o.minRttMs)} />
        <Figure label="最近丢弃" value={o.lastDropAt ? formatRelative(o.lastDropAt) : "—"} />
      </dl>
    </Panel>
  );
}

/* ── 指标 ── */

function KpiRow({ overview }: { overview: TrafficOverview }) {
  const local = overview.local;
  const last = local.lastSecond;
  const rejected = rejectedOf(last);
  return (
    <div className="grid min-w-0 grid-cols-2 gap-3 @xl:grid-cols-4 @5xl:grid-cols-7">
      <StatTile label="到达" value={last.arrivals} unit="/秒" stream="arrivals" />
      <StatTile label="放行" value={last.admitted} unit="/秒" stream="admitted" />
      <StatTile label="排队" value={last.queued} unit="/秒" stream="queued" />
      <StatTile label="拒绝" value={rejected} unit="/秒" stream="rejected" tone={rejected > 0 ? "danger" : "default"} />
      <StatTile label="在途" value={local.inflight} />
      <StatTile label="峰值" value={local.peakRps} unit="/秒" />
      <StatTile label="P99" value={local.latency.p99} unit="毫秒" format={{ maximumFractionDigits: 1 }} tone={local.latency.p99 > 1000 ? "warn" : "default"} />
    </div>
  );
}

/* ── 实时曲线 ── */

const liveConfig = {
  admitted: { label: "放行", theme: STREAM.admitted.chart },
  queued: { label: "排队", theme: STREAM.queued.chart },
  rejected: { label: "拒绝", theme: STREAM.rejected.chart },
  arrivals: { label: "到达", theme: STREAM.arrivals.chart },
  limit: { label: "预热上限", theme: { light: "#52525b", dark: "#a1a1aa" } }
} satisfies ChartConfig;

function LivePanel({
  overview,
  range,
  onRange
}: {
  overview: TrafficOverview;
  range: RangeValue;
  onRange: (value: RangeValue) => void;
}) {
  const data = useMemo(
    () =>
      (overview.local.series ?? []).map((point) => ({
        label: formatClockSeconds(point.ts * 1000),
        admitted: point.admitted,
        queued: point.queued,
        rejected: point.rejected,
        arrivals: point.arrivals,
        limit: point.limit > 0 ? point.limit : null
      })),
    [overview.local.series]
  );
  const hasLimit = data.some((point) => point.limit !== null);

  return (
    <Panel
      icon={Activity}
      tone="primary"
      title="实时流量"
      aside={<Segmented value={range} options={RANGES} onChange={onRange} ariaLabel="时间范围" />}
    >
      <div className="mb-3 flex flex-wrap gap-x-4 gap-y-1.5">
        <Swatch stream="admitted" />
        <Swatch stream="queued" />
        <Swatch stream="rejected" />
        <Swatch stream="arrivals" />
        {hasLimit ? (
          <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
            <span aria-hidden className="h-0 w-3 border-t-2 border-dashed border-muted-foreground" />
            预热上限
          </span>
        ) : null}
      </div>
      {data.length < 2 ? (
        <div className="grid min-h-[260px] place-items-center rounded-xl bg-muted/40 text-xs text-muted-foreground">暂无数据</div>
      ) : (
        <ChartContainer config={liveConfig} className="aspect-auto h-[260px] w-full">
          <ComposedChart data={data} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={48} />
            <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(value: number) => fmtNum(value)} allowDecimals={false} />
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <Area dataKey="admitted" type="monotone" stackId="flow" stroke="var(--color-admitted)" fill="var(--color-admitted)" fillOpacity={0.28} strokeWidth={1.5} isAnimationActive={false} />
            <Area dataKey="queued" type="monotone" stackId="flow" stroke="var(--color-queued)" fill="var(--color-queued)" fillOpacity={0.3} strokeWidth={1.5} isAnimationActive={false} />
            <Area dataKey="rejected" type="monotone" stackId="flow" stroke="var(--color-rejected)" fill="var(--color-rejected)" fillOpacity={0.3} strokeWidth={1.5} isAnimationActive={false} />
            <Line dataKey="arrivals" type="monotone" stroke="var(--color-arrivals)" strokeWidth={1.75} dot={false} isAnimationActive={false} />
            {hasLimit ? (
              <Line dataKey="limit" type="stepAfter" stroke="var(--color-limit)" strokeDasharray="5 4" strokeWidth={1.5} dot={false} connectNulls isAnimationActive={false} />
            ) : null}
          </ComposedChart>
        </ChartContainer>
      )}
    </Panel>
  );
}

/* ── 集群节点 ── */

function NodesPanel({ overview }: { overview: TrafficOverview }) {
  const nodes = overview.nodes ?? [];
  return (
    <Panel
      icon={Server}
      tone="violet"
      title="节点"
      aside={
        <span className="text-xs text-muted-foreground tabular-nums">
          {fmtNum(overview.cluster.lastSecond.arrivals)}/秒
        </span>
      }
    >
      {nodes.length === 0 ? (
        <CardEmpty icon={Server} title="暂无节点" />
      ) : (
        <div className="grid min-w-0 gap-3 @lg:grid-cols-2 @4xl:grid-cols-3">
          {nodes.map((node) => (
            <NodeTile key={node.instance} node={node} revision={overview.revision} self={node.instance === overview.local.instance} />
          ))}
        </div>
      )}
    </Panel>
  );
}

function NodeTile({ node, revision, self }: { node: NodeSummary; revision: number; self: boolean }) {
  const mismatch = node.revision !== revision;
  const cpu = Math.max(0, Math.min(100, node.cpu));
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col gap-3 rounded-xl border p-3",
        node.overloaded ? "border-rose-500/40 bg-rose-500/6" : "border-border bg-background/60"
      )}
    >
      <div className="flex min-w-0 items-start gap-2">
        <span
          aria-hidden
          className={cn("mt-1.5 size-2 shrink-0 rounded-full", node.overloaded ? "bg-rose-500" : node.enabled ? "bg-emerald-500" : "bg-muted-foreground")}
        />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{node.host || node.instance}</div>
          <div className="truncate font-mono text-[11px] text-muted-foreground">{node.instance}</div>
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-1">
          {self ? <Badge variant="outline" size="sm">本机</Badge> : null}
          {node.overloaded ? <Badge variant="danger" size="sm">过载</Badge> : null}
          {mismatch ? (
            <Badge variant="warning" size="sm" className="font-mono" title={`当前 r${revision}`}>
              r{node.revision}
            </Badge>
          ) : null}
          {!node.enabled ? <Badge variant="outline" size="sm">已关闭</Badge> : null}
        </div>
      </div>
      <div className="flex min-w-0 items-baseline gap-1">
        <span className="text-2xl leading-7 font-semibold tracking-tight tabular-nums">
          <NumberFlow value={node.lastSecond.arrivals} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
        </span>
        <span className="text-xs text-muted-foreground">/秒</span>
        {rejectedOf(node.lastSecond) > 0 ? (
          <span className="ml-auto text-xs text-rose-600 tabular-nums dark:text-rose-400">拒 {fmtNum(rejectedOf(node.lastSecond))}</span>
        ) : null}
      </div>
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between text-[11px] text-muted-foreground">
          <span>CPU</span>
          <span className="tabular-nums">{Math.round(cpu)}%</span>
        </div>
        <Bar ratio={cpu / 100} className={cpu >= 85 ? "bg-rose-500" : cpu >= 65 ? "bg-amber-500" : "bg-emerald-500"} />
      </div>
      <dl className="grid grid-cols-3 gap-2">
        <Figure label="在途" value={fmtNum(node.inflight)} />
        <Figure label="P99" value={fmtMs(node.p99Ms)} />
        <Figure label="心跳" value={formatRelative(node.updatedAt)} />
      </dl>
    </div>
  );
}

/* ── 热点键 ── */

function HotKeysPanel({ overview }: { overview: TrafficOverview }) {
  const keys = useMemo(() => {
    const merged = new Map<string, HotKey & { rules: Set<string> }>();
    for (const rule of overview.local.rules ?? []) {
      for (const hot of rule.hotKeys ?? []) {
        const id = `${hot.kind}:${hot.key}`;
        const entry = merged.get(id);
        if (entry) {
          entry.count += hot.count;
          entry.rules.add(rule.name);
        } else {
          merged.set(id, { ...hot, rules: new Set([rule.name]) });
        }
      }
    }
    return [...merged.values()].sort((a, b) => b.count - a.count).slice(0, 10);
  }, [overview.local.rules]);
  const max = keys[0]?.count ?? 1;

  return (
    <Panel icon={Flame} tone="rose" title="热点键">
      {keys.length === 0 ? (
        <CardEmpty icon={Flame} title="暂无热点" />
      ) : (
        <ol className="flex flex-col gap-2.5">
          {keys.map((hot, index) => (
            <li key={`${hot.kind}:${hot.key}`} className="flex min-w-0 flex-col gap-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="w-4 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{index + 1}</span>
                <Badge variant="outline" size="sm" className="shrink-0">
                  {KEY_LABEL[hot.kind] ?? hot.kind}
                </Badge>
                <span className="min-w-0 flex-1 truncate font-mono text-xs" title={hot.key}>
                  {hot.key}
                </span>
                <span className="shrink-0 text-xs font-medium text-rose-600 tabular-nums dark:text-rose-400">{fmtNum(hot.count)}</span>
              </div>
              <div className="pl-6">
                <Bar ratio={hot.count / max} className="bg-rose-500/70" trackClassName="h-1" />
              </div>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}

/* ── 事件 ── */

const EVENT_DOT: Record<string, string> = {
  "warmup.start": "bg-emerald-500",
  "warmup.saturated": "bg-amber-500",
  "warmup.settled": "bg-sky-500",
  "overload.start": "bg-rose-500",
  "overload.end": "bg-emerald-500",
  "reject.burst": "bg-rose-500",
  "concurrency.shrink": "bg-violet-500"
};

const DETAIL_LABEL: Record<string, string> = {
  ceiling: "上限",
  demand: "需求",
  limit: "并发上限",
  arrivals: "到达",
  rejected: "拒绝",
  cpu: "CPU",
  inflight: "在途",
  capacity: "承载量",
  dropped: "丢弃"
};

function eventFacts(event: TrafficEvent) {
  const facts: Array<{ label: string; value: string }> = [];
  for (const [key, raw] of Object.entries(event.detail ?? {})) {
    const label = DETAIL_LABEL[key];
    if (!label) continue;
    const value = typeof raw === "number" ? fmtNum(Math.round(raw * 10) / 10) : typeof raw === "string" ? raw : null;
    if (value) facts.push({ label, value });
  }
  return facts.slice(0, 4);
}

function EventsPanel() {
  const eventsQuery = useTrafficEventsQuery(30);
  const events = eventsQuery.data ?? [];
  return (
    <Panel icon={History} tone="amber" title="事件">
      {eventsQuery.isLoading ? (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-10 rounded-lg" />
          ))}
        </div>
      ) : events.length === 0 ? (
        <CardEmpty icon={History} title="暂无事件" />
      ) : (
        <ol className="relative flex max-h-[420px] flex-col gap-3 overflow-y-auto pr-1">
          {events.map((event) => (
            <li key={event.id} className="relative flex min-w-0 gap-3">
              <span className="flex w-12 shrink-0 justify-end pt-0.5 text-[11px] text-muted-foreground tabular-nums">
                {formatCompact(event.at)}
              </span>
              <span aria-hidden className={cn("mt-1.5 size-2 shrink-0 rounded-full", EVENT_DOT[event.kind] ?? "bg-muted-foreground")} />
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                  <span className="text-sm font-medium">{EVENT_LABEL[event.kind] ?? event.kind}</span>
                  {event.ruleName ? <span className="truncate text-xs text-muted-foreground">{event.ruleName}</span> : null}
                </div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {eventFacts(event).map((fact) => (
                    <span key={fact.label} className="rounded-md bg-muted px-1.5 py-0.5 text-[11px] tabular-nums">
                      <span className="text-muted-foreground">{fact.label}</span> {fact.value}
                    </span>
                  ))}
                  {event.instance ? (
                    <span className="rounded-md border border-border px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">{event.instance}</span>
                  ) : null}
                </div>
              </div>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  );
}
