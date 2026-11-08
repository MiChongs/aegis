"use client";

import { useMemo, useState, type ReactNode } from "react";
import NumberFlow, { type Format } from "@number-flow/react";
import { Area, AreaChart, CartesianGrid, Cell, Label, Pie, PieChart, XAxis, YAxis } from "recharts";
import { Activity, ArrowDownRight, ArrowRight, ArrowUpRight, Boxes, Globe2, LayoutGrid, ListChecks, PieChart as PieIcon, ShieldAlert, Workflow } from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Bar as ShareBar, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { CountryFlag } from "@/components/ui/country-flag";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { DashboardData, KV, RiskAction } from "@/lib/api/risk";
import { formatClock, formatRelative, formatShortDateTime, formatWith } from "@/lib/datetime";
import { useRiskCaseStats, useRiskDashboard, useRiskPatterns, useRiskRules } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { ACTION_UI, ErrorState, RANGE_PRESETS, regionName, useRangeFrom, useSceneName, type RangeValue } from "./decisions";
import { ACTION_EXPLAIN, ACTION_RESULT, countText, latencyText, percent } from "./kit/humanize";
import { ACTION_LABEL, ACTION_ORDER } from "./kit/labels";

/**
 * 风控总览：先用一句话说清这段时间发生了什么，再用处置漏斗、趋势、场景热力格、
 * 规则榜与风险来源逐层展开。上一时段用同一接口按前一个等长窗口再取一次，只用于环比。
 */

type Counts = Record<RiskAction, number>;

const ACTION_CONFIG = Object.fromEntries(ACTION_ORDER.map((a) => [a, { label: ACTION_LABEL[a], color: ACTION_UI[a].color }])) satisfies ChartConfig;
const TOTAL_CONFIG = { total: { label: "评估", color: "var(--color-sky-500, #0ea5e9)" } } satisfies ChartConfig;

const fmt = (n: number) => n.toLocaleString("zh-CN");

function actionCounts(data?: DashboardData): Counts {
  const out: Counts = { pass: 0, review: 0, captcha: 0, block: 0, ban: 0 };
  for (const kv of data?.byAction ?? []) {
    if (kv.key in out) out[kv.key as RiskAction] += kv.count;
  }
  return out;
}

const sumOf = (c: Counts) => ACTION_ORDER.reduce((s, a) => s + c[a], 0);

export function RiskOverview({ app, onNavigate }: { app?: number; onNavigate: (tab: string) => void }) {
  const [range, setRange] = useState<RangeValue>("24h");
  const preset = RANGE_PRESETS.find((r) => r.value === range) ?? RANGE_PRESETS[1];
  const from = useRangeFrom(range);
  const prevFrom = String(Number(from) - preset.seconds * 1000);
  const query = useRiskDashboard({ app, from });
  const prev = useRiskDashboard({ app, from: prevFrom, to: from });
  const data = query.data;

  const rangeSwitch = <Segmented value={range} options={RANGE_PRESETS} onChange={setRange} ariaLabel="时间范围" />;

  if (!data) {
    return query.isError ? (
      <TabStack>
        <Panel icon={ShieldAlert} tone="rose" title="风控总览" aside={rangeSwitch}>
          <ErrorState error={query.error} onRetry={() => query.refetch()} />
        </Panel>
      </TabStack>
    ) : (
      <OverviewSkeleton aside={rangeSwitch} />
    );
  }

  const counts = actionCounts(data);
  const total = sumOf(counts);
  const prevCounts = prev.data ? actionCounts(prev.data) : null;
  const empty = total === 0 && !(data.alerts ?? []).length;

  return (
    <TabStack>
      <Hero
        app={app}
        data={data}
        counts={counts}
        total={total}
        prev={prevCounts}
        rangeLabel={preset.label}
        aside={rangeSwitch}
        onNavigate={onNavigate}
      />
      {empty ? (
        <Panel icon={Activity} tone="sky" title="评估趋势">
          <div className="flex flex-col items-center pb-2">
            <CardEmpty icon={ShieldAlert} title="这段时间没有评估记录" description="场景发布并接入业务请求后，这里会显示趋势、场景处置与风险来源" />
            <Button size="sm" variant="outline" onClick={() => onNavigate("scenes")}>
              <Workflow />
              查看场景
            </Button>
          </div>
        </Panel>
      ) : (
        <>
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_320px]">
            <TrendPanel data={data} range={range} onNavigate={onNavigate} />
            <MixPanel counts={counts} total={total} />
          </div>
          <div className="grid min-w-0 gap-4 @5xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
            <SceneHeatPanel data={data} />
            <RulesPanel rules={data.topRules ?? []} />
          </div>
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
            <SourcePanel ips={data.topIPs ?? []} countries={data.topCountries ?? []} />
            <AlertPanel alerts={data.alerts ?? []} onNavigate={onNavigate} />
          </div>
        </>
      )}
    </TabStack>
  );
}

/* ── 骨架 ── */

function OverviewSkeleton({ aside }: { aside: ReactNode }) {
  return (
    <TabStack>
      <section className="grid min-w-0 gap-6 rounded-2xl border border-border bg-card p-4 sm:p-5 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex items-start justify-between gap-3">
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <Skeleton className="h-7 w-4/5" />
              <Skeleton className="h-4 w-3/5" />
            </div>
            <div className="@4xl:hidden">{aside}</div>
          </div>
          <div className="grid grid-cols-2 gap-3 @2xl:grid-cols-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex flex-col gap-2 rounded-xl border p-3">
                <Skeleton className="h-3.5 w-16" />
                <Skeleton className="h-7 w-20" />
                <Skeleton className="h-3 w-24" />
              </div>
            ))}
          </div>
        </div>
        <div className="flex min-w-0 flex-col gap-3">
          <div className="hidden justify-end @4xl:flex">{aside}</div>
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="flex items-center gap-3">
              <Skeleton className="h-4 w-16" />
              <Skeleton className="h-7 flex-1 rounded-lg" />
              <Skeleton className="h-4 w-20" />
            </div>
          ))}
        </div>
      </section>
      <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_320px]">
        <PanelSkeleton bodyHeight={300} />
        <PanelSkeleton bodyHeight={300} />
      </div>
      <div className="grid min-w-0 gap-4 @5xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <PanelSkeleton bodyHeight={280} />
        <PanelSkeleton bodyHeight={280} />
      </div>
    </TabStack>
  );
}

function PanelSkeleton({ bodyHeight }: { bodyHeight: number }) {
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-2xl border border-border bg-card p-4 sm:p-5">
      <div className="flex items-center gap-3">
        <Skeleton className="size-7 rounded-lg" />
        <Skeleton className="h-4 w-28" />
      </div>
      <Skeleton className="w-full rounded-xl" style={{ height: bodyHeight }} />
    </div>
  );
}

/* ── 一句话摘要 + 处置漏斗 ── */

function topSceneBy(data: DashboardData, actions: RiskAction[]) {
  const map = new Map<string, number>();
  for (const r of data.byScene ?? []) if (actions.includes(r.action)) map.set(r.scene, (map.get(r.scene) ?? 0) + r.count);
  let best: { scene: string; count: number } | null = null;
  let sum = 0;
  for (const [scene, count] of map) {
    sum += count;
    if (!best || count > best.count) best = { scene, count };
  }
  return best ? { ...best, share: sum ? best.count / sum : 0 } : null;
}

function change(cur: number, prev: number) {
  if (prev <= 0) return null;
  return (cur - prev) / prev;
}

function changeWord(ratio: number) {
  const pct = Math.abs(ratio) * 100;
  if (pct < 1) return "基本持平";
  return `${ratio > 0 ? "增加" : "减少"} ${pct >= 100 ? Math.round(pct) : pct.toFixed(pct >= 10 ? 0 : 1)}%`;
}

function Hero({
  app,
  data,
  counts,
  total,
  prev,
  rangeLabel,
  aside,
  onNavigate,
}: {
  app?: number;
  data: DashboardData;
  counts: Counts;
  total: number;
  prev: Counts | null;
  rangeLabel: string;
  aside: ReactNode;
  onNavigate: (tab: string) => void;
}) {
  const sceneName = useSceneName();
  const blocked = counts.block + counts.ban;
  const challenged = counts.review + counts.captcha;
  const prevTotal = prev ? sumOf(prev) : 0;
  const prevBlocked = prev ? prev.block + prev.ban : 0;
  const caseStats = useRiskCaseStats(app);
  const pending = caseStats.data?.pending ?? data.pendingCases ?? 0;
  const urgent = caseStats.data ? (caseStats.data.pendingByLevel.critical ?? 0) + (caseStats.data.pendingByLevel.high ?? 0) : 0;
  const oldestAt = caseStats.data?.oldestPendingAt;
  const alerts = (data.alerts ?? []).reduce((s, kv) => s + kv.count, 0);

  const title = (() => {
    const head = `过去 ${rangeLabel}`;
    if (!total) return `${head}没有评估记录`;
    let s = `${head}评估 ${countText(total)}，`;
    if (blocked) {
      const top = topSceneBy(data, ["block", "ban"]);
      s += `拦截 ${countText(blocked)}`;
      if (top) s += top.share >= 0.5 ? `，主要来自${sceneName(top.scene)}场景` : `，${sceneName(top.scene)}场景最多`;
    } else if (challenged) {
      s += `没有拦截，${countText(challenged)}要求验证或复核`;
    } else {
      s += "全部放行";
    }
    return s;
  })();

  const sub = (() => {
    const parts: string[] = [];
    if (total) {
      const c = change(total, prevTotal);
      if (c !== null) parts.push(`评估量较前 ${rangeLabel}${changeWord(c)}`);
      parts.push(`拦截率 ${percent(blocked, total)}${prevTotal ? `，前一时段为 ${percent(prevBlocked, prevTotal)}` : ""}`);
    }
    if (alerts) parts.push(`行为模式告警 ${countText(alerts)}`);
    if (pending) parts.push(`${pending.toLocaleString("zh-CN")} 张复核单等待处理${urgent ? `，其中 ${urgent} 张高风险` : ""}`);
    return parts.length ? `${parts.join("；")}。` : "场景发布并接入业务后，这里会实时汇总评估与处置情况。";
  })();

  const rate = total ? blocked / total : 0;
  const prevRate = prevTotal ? prevBlocked / prevTotal : null;
  const lat = data.latency;

  return (
    <section className="grid min-w-0 gap-6 rounded-2xl border border-border bg-card p-4 sm:p-5 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <div className="flex min-w-0 flex-col justify-between gap-5">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-2">
            <h3 className="text-xl leading-8 font-semibold tracking-tight text-balance">{title}</h3>
            <p className="text-sm leading-6 text-muted-foreground">{sub}</p>
          </div>
          <div className="shrink-0 @4xl:hidden">{aside}</div>
        </div>
        <div className="grid min-w-0 grid-cols-2 gap-3 @2xl:grid-cols-4">
          <Figure
            label="评估次数"
            value={total}
            delta={change(total, prevTotal)}
            note={prevTotal ? `前一时段 ${countText(prevTotal)}` : "暂无上一时段数据"}
          />
          <Figure
            label="拦截率"
            value={rate}
            format={{ style: "percent", maximumFractionDigits: rate >= 0.1 ? 1 : 2 }}
            delta={prevRate !== null && prevRate > 0 ? (rate - prevRate) / prevRate : null}
            badWhenUp
            note={`拦截 ${fmt(counts.block)}，封禁 ${fmt(counts.ban)}`}
            alert={rate >= 0.05}
          />
          <Figure
            label="待复核"
            value={pending}
            note={pending ? (urgent ? `其中 ${urgent} 张高风险` : oldestAt ? `最早一张创建于 ${formatRelative(oldestAt)}` : "前往处理") : "已全部处理"}
            alert={pending > 0}
            onClick={() => onNavigate("cases")}
          />
          <Figure
            label="响应速度"
            display={lat && total ? latencyText(lat.p99) : "暂无"}
            note={lat && total ? `99% 的评估在此时间内完成，中位 ${latencyText(lat.p50)}` : "暂无评估"}
          />
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-3">
        <div className="hidden items-center justify-between gap-3 @4xl:flex">
          <span className="text-xs font-medium text-muted-foreground">处置漏斗</span>
          {aside}
        </div>
        <span className="text-xs font-medium text-muted-foreground @4xl:hidden">处置漏斗</span>
        <Funnel counts={counts} total={total} />
      </div>
    </section>
  );
}

function Figure({
  label,
  value,
  display,
  format,
  delta,
  badWhenUp,
  note,
  alert,
  onClick,
}: {
  label: string;
  value?: number;
  display?: ReactNode;
  format?: Format;
  delta?: number | null;
  badWhenUp?: boolean;
  note?: ReactNode;
  alert?: boolean;
  onClick?: () => void;
}) {
  const showDelta = typeof delta === "number" && Math.abs(delta) >= 0.01;
  const up = (delta ?? 0) > 0;
  const body = (
    <>
      <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        <span className="truncate">{label}</span>
        {onClick ? <ArrowRight className="ml-auto size-3.5 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" /> : null}
      </div>
      <div className="flex min-w-0 items-baseline gap-1.5">
        <span className={cn("text-2xl leading-8 font-semibold tracking-tight tabular-nums", alert && "text-red-600 dark:text-red-400")}>
          {display ?? <NumberFlow value={value ?? 0} locales="zh-CN" format={format ?? { notation: "compact", maximumFractionDigits: 1 }} />}
        </span>
        {showDelta ? (
          <span
            className={cn(
              "inline-flex items-center text-xs font-medium tabular-nums",
              badWhenUp ? (up ? "text-red-600 dark:text-red-400" : "text-emerald-600 dark:text-emerald-400") : "text-muted-foreground"
            )}
          >
            {up ? <ArrowUpRight className="size-3.5" /> : <ArrowDownRight className="size-3.5" />}
            {Math.abs(delta! * 100) >= 100 ? Math.round(Math.abs(delta! * 100)) : Math.abs(delta! * 100).toFixed(1)}%
          </span>
        ) : null}
      </div>
      <div className="min-h-4 truncate text-[11px] leading-4 text-muted-foreground">{note}</div>
    </>
  );
  const cls = "group flex min-w-0 flex-col gap-1 rounded-xl border border-border p-3 text-left";
  return onClick ? (
    <button type="button" onClick={onClick} className={cn(cls, "transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none")}>
      {body}
    </button>
  ) : (
    <div className={cls}>{body}</div>
  );
}

const FUNNEL_STEPS: Array<{ key: string; label: string; hint: string; bar: string; of: (c: Counts) => number }> = [
  { key: "all", label: "全部评估", hint: "进入风控的全部请求", bar: "bg-sky-500", of: (c) => sumOf(c) },
  { key: "flagged", label: "需要关注", hint: "未直接放行：复核、验证码、拦截与封禁", bar: "bg-amber-500", of: (c) => c.review + c.captcha + c.block + c.ban },
  { key: "rejected", label: "拒绝请求", hint: "被拦截或封禁", bar: "bg-red-500", of: (c) => c.block + c.ban },
  { key: "banned", label: "同时封禁", hint: ACTION_EXPLAIN.ban, bar: "bg-red-700", of: (c) => c.ban },
];

/** 处置漏斗：从全部评估逐级收窄到封禁。条宽按数量级缩放，小数量也看得见，具体数字标在右侧 */
function Funnel({ counts, total }: { counts: Counts; total: number }) {
  const logMax = Math.log10(total + 1) || 1;
  const values = FUNNEL_STEPS.map((step) => step.of(counts));
  return (
    <ol className="flex min-w-0 flex-col gap-2">
      {FUNNEL_STEPS.map((step, i) => {
        const value = values[i]!;
        const prevValue = i === 0 ? total : values[i - 1]!;
        const width = value > 0 ? 14 + 86 * (Math.log10(value + 1) / logMax) : 0;
        const ofPrev = i === 0 ? null : prevValue ? percent(value, prevValue) : "0%";
        return (
          <li key={step.key} className="grid min-w-0 grid-cols-[5rem_minmax(0,1fr)_6.5rem] items-center gap-3">
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="truncate text-xs text-muted-foreground">{step.label}</span>
              </TooltipTrigger>
              <TooltipContent>{step.hint}</TooltipContent>
            </Tooltip>
            <div className="flex h-8 min-w-0 items-center justify-center rounded-lg bg-muted/50">
              {value > 0 ? (
                <span className={cn("h-full rounded-lg transition-[width] duration-700", step.bar)} style={{ width: `${width}%` }} />
              ) : (
                <span className="text-[11px] text-muted-foreground">无</span>
              )}
            </div>
            <div className="flex min-w-0 flex-col items-end leading-tight">
              <span className="text-sm font-semibold tabular-nums">{fmt(value)}</span>
              <span className="text-[11px] text-muted-foreground tabular-nums">{ofPrev === null ? "100%" : `占上一级 ${ofPrev}`}</span>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

/* ── 趋势 ── */

const RANGE_MS: Record<RangeValue, number> = { "1h": 3600_000, "24h": 86400_000, "7d": 7 * 86400_000, "30d": 30 * 86400_000 };

function bucketLabel(t: string, range: RangeValue) {
  if (range === "1h" || range === "24h") return formatClock(t);
  if (range === "7d") return formatShortDateTime(t);
  return formatWith(t, { month: "2-digit", day: "2-digit" });
}

const RISK_ACTIONS: RiskAction[] = ["review", "captcha", "block", "ban"];

function TrendPanel({ data, range, onNavigate }: { data: DashboardData; range: RangeValue; onNavigate: (tab: string) => void }) {
  const [view, setView] = useState<"risk" | "all">("risk");
  const rows = useMemo(() => {
    // 后端只返回有数据的桶（≤6 小时 5 分钟一桶，≤7 天 1 小时，更长 1 天）；空桶补 0，否则只有零星几个点时画不出走势
    const span = RANGE_MS[range];
    const step = span <= 6 * 3600_000 ? 300_000 : span <= 7 * 86400_000 ? 3600_000 : 86400_000;
    const to = data.to ? Date.parse(data.to) : 0;
    const from = data.from ? Date.parse(data.from) : to;
    const map = new Map<number, Record<string, number | string>>();
    const blank = (ms: number) => {
      const t = new Date(ms).toISOString();
      return { t, label: bucketLabel(t, range), pass: 0, review: 0, captcha: 0, block: 0, ban: 0, total: 0, risk: 0 } as Record<string, number | string>;
    };
    for (let ms = Math.floor(from / step) * step; ms < to; ms += step) map.set(ms, blank(ms));
    for (const p of data.series ?? []) {
      const ms = Date.parse(p.t);
      let row = map.get(ms);
      if (!row) {
        row = blank(ms);
        map.set(ms, row);
      }
      row[p.action] = ((row[p.action] as number) ?? 0) + p.count;
      row.total = (row.total as number) + p.count;
      if (p.action !== "pass") row.risk = (row.risk as number) + p.count;
    }
    return [...map.entries()].sort(([a], [b]) => a - b).map(([, r]) => r);
  }, [data.series, data.from, data.to, range]);

  const key = view === "risk" ? "risk" : "total";
  const peak = rows.reduce<Record<string, number | string> | null>((best, r) => ((r[key] as number) > ((best?.[key] as number) ?? 0) ? r : best), null);
  const present = RISK_ACTIONS.filter((a) => rows.some((r) => (r[a] as number) > 0));
  const hasData = (data.series ?? []).length > 0;

  const note = !hasData
    ? "这段时间没有评估。"
    : !peak
      ? view === "risk"
        ? "这段时间没有需要关注的请求，全部放行。"
        : "这段时间没有评估。"
      : `${view === "risk" ? "风险处置" : "评估量"}在 ${peak.label} 最多，共 ${countText(peak[key] as number)}。`;

  return (
    <Panel
      icon={Activity}
      tone="sky"
      title="评估趋势"
      aside={
        <>
          <Segmented
            value={view}
            options={[
              { value: "risk", label: "风险处置" },
              { value: "all", label: "全部评估" },
            ]}
            onChange={setView}
            ariaLabel="趋势视图"
          />
          <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("decisions")}>
            决策流水
            <ArrowRight />
          </Button>
        </>
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-sm leading-5 text-muted-foreground">{note}</p>
        {view === "risk" ? (
          <div className="flex min-h-5 flex-wrap gap-x-4 gap-y-1">
            {RISK_ACTIONS.map((a) => (
              <span key={a} className={cn("inline-flex items-center gap-1.5 text-xs", present.includes(a) ? "text-muted-foreground" : "text-muted-foreground/50")}>
                <span aria-hidden className={cn("size-2 rounded-[3px]", ACTION_UI[a].dot)} />
                {ACTION_LABEL[a]}
              </span>
            ))}
          </div>
        ) : (
          <div className="min-h-5" />
        )}
        {!hasData ? (
          <div className="grid h-[240px] place-items-center rounded-xl bg-muted/40 text-sm text-muted-foreground">暂无数据</div>
        ) : view === "risk" ? (
          <ChartContainer config={ACTION_CONFIG} className="aspect-auto h-[240px] w-full">
            <AreaChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} />
              <YAxis tickLine={false} axisLine={false} width={44} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
              <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
              {RISK_ACTIONS.map((a) => (
                <Area
                  key={a}
                  dataKey={a}
                  type="monotone"
                  stackId="risk"
                  stroke={`var(--color-${a})`}
                  fill={`var(--color-${a})`}
                  fillOpacity={0.28}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                />
              ))}
            </AreaChart>
          </ChartContainer>
        ) : (
          <ChartContainer config={TOTAL_CONFIG} className="aspect-auto h-[240px] w-full">
            <AreaChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} />
              <YAxis tickLine={false} axisLine={false} width={44} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
              <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
              <Area dataKey="total" type="monotone" stroke="var(--color-total)" fill="var(--color-total)" fillOpacity={0.2} strokeWidth={1.75} isAnimationActive={false} />
            </AreaChart>
          </ChartContainer>
        )}
      </div>
    </Panel>
  );
}

function compact(v: number) {
  if (v >= 100_000_000) return `${(v / 100_000_000).toFixed(1)}亿`;
  if (v >= 10_000) return `${(v / 10_000).toFixed(v >= 100_000 ? 0 : 1)}万`;
  return String(v);
}

/* ── 处置构成 ── */

function MixPanel({ counts, total }: { counts: Counts; total: number }) {
  const rows = ACTION_ORDER.map((a) => ({ key: a, value: counts[a] })).filter((r) => r.value > 0);
  const data = rows.length ? rows : [{ key: "pass" as RiskAction, value: 1 }];
  const top = [...rows].filter((r) => r.key !== "pass").sort((a, b) => b.value - a.value)[0];
  return (
    <Panel icon={PieIcon} tone="violet" title="处置构成">
      <div className="flex min-w-0 flex-col items-center gap-4">
        <ChartContainer config={ACTION_CONFIG} className="aspect-square h-[176px]">
          <PieChart>
            <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
            <Pie data={data} dataKey="value" nameKey="key" innerRadius={58} outerRadius={82} strokeWidth={2} paddingAngle={rows.length > 1 ? 2 : 0}>
              {data.map((r) => (
                <Cell key={r.key} fill={rows.length ? `var(--color-${r.key})` : "var(--muted)"} />
              ))}
              <Label
                content={({ viewBox }) => {
                  if (!viewBox || !("cx" in viewBox)) return null;
                  return (
                    <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) - 6} className="fill-foreground text-xl font-semibold">
                        {total ? percent(total - counts.pass, total) : "0%"}
                      </tspan>
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 14} className="fill-muted-foreground text-[11px]">
                        未直接放行
                      </tspan>
                    </text>
                  );
                }}
              />
            </Pie>
          </PieChart>
        </ChartContainer>
        <ul className="flex w-full flex-col gap-1">
          {ACTION_ORDER.map((a) => (
            <li key={a}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className={cn("flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-xs", counts[a] === 0 && "opacity-50")}>
                    <span aria-hidden className={cn("size-2.5 shrink-0 rounded-[3px]", ACTION_UI[a].dot)} />
                    <span className="min-w-0 flex-1 truncate">{ACTION_RESULT[a]}</span>
                    <span className="font-medium tabular-nums">{fmt(counts[a])}</span>
                    <span className="w-12 text-right text-muted-foreground tabular-nums">{percent(counts[a], total)}</span>
                  </div>
                </TooltipTrigger>
                <TooltipContent side="left">{ACTION_EXPLAIN[a]}</TooltipContent>
              </Tooltip>
            </li>
          ))}
        </ul>
        <p className="w-full border-t pt-3 text-xs leading-5 text-muted-foreground">
          {top ? `除放行外，${ACTION_RESULT[top.key]}最多，共 ${countText(top.value)}。` : "这段时间的请求全部放行。"}
        </p>
      </div>
    </Panel>
  );
}

/* ── 场景热力格 ── */

const HEAT: Record<Exclude<RiskAction, "pass">, string[]> = {
  review: ["", "bg-sky-500/10", "bg-sky-500/20", "bg-sky-500/35", "bg-sky-500/55"],
  captcha: ["", "bg-amber-500/10", "bg-amber-500/20", "bg-amber-500/35", "bg-amber-500/55"],
  block: ["", "bg-red-500/10", "bg-red-500/20", "bg-red-500/35", "bg-red-500/55"],
  ban: ["", "bg-red-700/12", "bg-red-700/25", "bg-red-700/40", "bg-red-700/60"],
};

function heatLevel(share: number) {
  if (share <= 0) return 0;
  if (share < 0.01) return 1;
  if (share < 0.05) return 2;
  if (share < 0.2) return 3;
  return 4;
}

function SceneHeatPanel({ data }: { data: DashboardData }) {
  const sceneName = useSceneName();
  const [all, setAll] = useState(false);
  const rows = useMemo(() => {
    const map = new Map<string, Counts & { scene: string; total: number }>();
    for (const r of data.byScene ?? []) {
      let row = map.get(r.scene);
      if (!row) {
        row = { scene: r.scene, total: 0, pass: 0, review: 0, captcha: 0, block: 0, ban: 0 };
        map.set(r.scene, row);
      }
      if (r.action in row) row[r.action] += r.count;
      row.total += r.count;
    }
    return [...map.values()].sort((a, b) => b.total - b.pass - (a.total - a.pass) || b.total - a.total);
  }, [data.byScene]);
  const maxTotal = Math.max(1, ...rows.map((r) => r.total));
  const shown = all ? rows : rows.slice(0, 8);
  const riskiest = rows.find((r) => r.block + r.ban > 0);

  return (
    <Panel
      icon={LayoutGrid}
      tone="primary"
      title="场景处置"
      aside={rows.length ? <span className="text-xs text-muted-foreground">颜色越深，占比越高</span> : null}
    >
      {rows.length === 0 ? (
        <CardEmpty icon={LayoutGrid} title="暂无场景数据" />
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          <p className="text-sm leading-5 text-muted-foreground">
            {riskiest
              ? `${sceneName(riskiest.scene)}场景拦截率最高，为 ${percent(riskiest.block + riskiest.ban, riskiest.total)}。`
              : `${rows.length} 个场景均没有拦截。`}
          </p>
          <div className="-mx-1 overflow-x-auto px-1">
            <table className="w-full min-w-[560px] border-separate border-spacing-1 text-xs">
              <thead>
                <tr className="text-muted-foreground">
                  <th className="pb-1 text-left font-normal">场景</th>
                  <th className="w-28 pb-1 text-left font-normal">评估量</th>
                  {RISK_ACTIONS.map((a) => (
                    <th key={a} className="w-16 pb-1 text-center font-normal">
                      {ACTION_LABEL[a]}
                    </th>
                  ))}
                  <th className="w-16 pb-1 text-right font-normal">拦截率</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((r) => {
                  const blockRate = r.total ? (r.block + r.ban) / r.total : 0;
                  return (
                    <tr key={r.scene}>
                      <td className="max-w-40 truncate py-1 pr-2 text-sm">{sceneName(r.scene)}</td>
                      <td className="py-1 pr-2">
                        <div className="flex flex-col gap-1">
                          <span className="tabular-nums">{fmt(r.total)}</span>
                          <ShareBar ratio={r.total / maxTotal} className="bg-foreground/40" trackClassName="h-1" />
                        </div>
                      </td>
                      {RISK_ACTIONS.map((a) => {
                        const level = heatLevel(r.total ? r[a] / r.total : 0);
                        return (
                          <td key={a} className="p-0">
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <div
                                  className={cn(
                                    "grid h-9 place-items-center rounded-md tabular-nums",
                                    level ? HEAT[a as Exclude<RiskAction, "pass">][level] : "bg-muted/40 text-muted-foreground/60",
                                    level >= 3 && "font-medium"
                                  )}
                                >
                                  {r[a] ? compact(r[a]) : "0"}
                                </div>
                              </TooltipTrigger>
                              <TooltipContent>
                                {sceneName(r.scene)}场景{ACTION_RESULT[a]} {fmt(r[a])} 次，占 {percent(r[a], r.total)}
                              </TooltipContent>
                            </Tooltip>
                          </td>
                        );
                      })}
                      <td className={cn("py-1 text-right font-medium tabular-nums", blockRate >= 0.05 ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
                        {percent(r.block + r.ban, r.total)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {rows.length > 8 ? (
            <Button size="sm" variant="ghost" className="h-8 self-start text-xs" onClick={() => setAll((v) => !v)}>
              {all ? "收起" : `查看全部 ${rows.length} 个场景`}
            </Button>
          ) : null}
        </div>
      )}
    </Panel>
  );
}

/* ── 规则榜 ── */

function RulesPanel({ rules }: { rules: KV[] }) {
  const list = useRiskRules();
  const sceneName = useSceneName();
  const byKey = useMemo(() => {
    const map = new Map<string, { name: string; scene: string }>();
    for (const r of list.data ?? []) if (!map.has(r.key)) map.set(r.key, { name: r.name, scene: r.scene });
    return map;
  }, [list.data]);
  const rows = rules.slice(0, 8);
  const sum = rules.reduce((s, r) => s + r.count, 0);
  const max = rows[0]?.count ?? 1;
  const nameOf = (k: string) => byKey.get(k)?.name || "已删除的规则";

  return (
    <Panel icon={ListChecks} tone="amber" title="命中最多的规则">
      {rows.length === 0 ? (
        <CardEmpty icon={ListChecks} title="这段时间没有规则命中" />
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          <p className="text-sm leading-5 text-muted-foreground">
            「{nameOf(rows[0]!.key)}」命中最多，占全部命中的 {percent(rows[0]!.count, sum)}。
          </p>
          <ol className="flex min-w-0 flex-col gap-2.5">
            {rows.map((kv, i) => {
              const meta = byKey.get(kv.key);
              return (
                <li key={kv.key} className="flex min-w-0 flex-col gap-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span
                      className={cn(
                        "grid size-5 shrink-0 place-items-center rounded-md text-[11px] tabular-nums",
                        i < 3 ? "bg-amber-500/15 text-amber-700 dark:text-amber-300" : "bg-muted text-muted-foreground"
                      )}
                    >
                      {i + 1}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-sm">{nameOf(kv.key)}</span>
                    {meta?.scene && meta.scene !== "*" ? <span className="hidden shrink-0 text-[11px] text-muted-foreground sm:inline">{sceneName(meta.scene)}</span> : null}
                    <span className="w-14 shrink-0 text-right text-xs font-medium tabular-nums">{compact(kv.count)}</span>
                  </div>
                  <div className="pl-7">
                    <ShareBar ratio={kv.count / max} className="bg-amber-500" trackClassName="h-1" />
                  </div>
                </li>
              );
            })}
          </ol>
        </div>
      )}
    </Panel>
  );
}

/* ── 风险来源 ── */

function SourcePanel({ ips, countries }: { ips: KV[]; countries: KV[] }) {
  const [view, setView] = useState<"ip" | "country">("ip");
  const rows = view === "ip" ? ips : countries;
  const max = rows[0]?.count ?? 1;
  const top = rows[0];
  const note = !top
    ? ""
    : view === "ip"
      ? `${top.key} 被拦截 ${countText(top.count)}，是拦截最多的 IP。`
      : `${top.key ? regionName(top.key) : "未知地区"}的风险请求最多，共 ${countText(top.count)}。`;

  return (
    <Panel
      icon={Globe2}
      tone="rose"
      title="风险来源"
      aside={
        <Segmented
          value={view}
          options={[
            { value: "ip", label: "IP" },
            { value: "country", label: "地区" },
          ]}
          onChange={setView}
          ariaLabel="来源维度"
        />
      }
    >
      {rows.length === 0 ? (
        <CardEmpty icon={Globe2} title={view === "ip" ? "这段时间没有被拦截的 IP" : "这段时间没有风险请求"} />
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          <p className="text-sm leading-5 text-muted-foreground">{note}</p>
          <ol className="flex min-w-0 flex-col gap-2.5">
            {rows.map((kv, i) => (
              <li key={kv.key || i} className="flex min-w-0 flex-col gap-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="w-5 shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{i + 1}</span>
                  {view === "ip" ? (
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{kv.key}</span>
                  ) : (
                    <span className="inline-flex min-w-0 flex-1 items-center gap-1.5 text-sm">
                      {kv.key ? <CountryFlag code={kv.key} size={14} /> : null}
                      <span className="truncate">{kv.key ? regionName(kv.key) : "未知地区"}</span>
                    </span>
                  )}
                  <span className="shrink-0 text-xs font-medium tabular-nums">{fmt(kv.count)} 次</span>
                </div>
                <div className="pl-7">
                  <ShareBar ratio={kv.count / max} className={view === "ip" ? "bg-red-500" : "bg-rose-400"} trackClassName="h-1" />
                </div>
              </li>
            ))}
          </ol>
        </div>
      )}
    </Panel>
  );
}

/* ── 行为模式告警 ── */

function AlertPanel({ alerts, onNavigate }: { alerts: KV[]; onNavigate: (tab: string) => void }) {
  const patterns = useRiskPatterns();
  const byKey = useMemo(() => new Map((patterns.data ?? []).map((p) => [p.key, p])), [patterns.data]);
  const sum = alerts.reduce((s, kv) => s + kv.count, 0);
  const max = alerts[0]?.count ?? 1;
  const nameOf = (k: string) => byKey.get(k)?.name || "已删除的模式";
  const SEVERITY: Record<string, { label: string; cls: string }> = {
    critical: { label: "严重", cls: "bg-red-500/12 text-red-700 dark:text-red-300" },
    high: { label: "高", cls: "bg-amber-500/12 text-amber-700 dark:text-amber-300" },
    medium: { label: "中", cls: "bg-sky-500/12 text-sky-700 dark:text-sky-300" },
    low: { label: "低", cls: "bg-muted text-muted-foreground" },
  };

  return (
    <Panel
      icon={Boxes}
      tone="amber"
      title="行为模式告警"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("patterns")}>
          行为模式
          <ArrowRight />
        </Button>
      }
    >
      {alerts.length === 0 ? (
        <CardEmpty icon={Boxes} title="这段时间没有告警" />
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          <p className="text-sm leading-5 text-muted-foreground">
            共告警 {countText(sum)}，「{nameOf(alerts[0]!.key)}」最多。
          </p>
          <ol className="flex min-w-0 flex-col gap-2.5">
            {alerts.map((kv) => {
              const p = byKey.get(kv.key);
              const sev = p ? SEVERITY[p.severity] : undefined;
              return (
                <li key={kv.key} className="flex min-w-0 flex-col gap-1">
                  <div className="flex min-w-0 items-center gap-2">
                    {sev ? <span className={cn("shrink-0 rounded px-1.5 text-[11px] leading-[18px]", sev.cls)}>{sev.label}</span> : null}
                    <span className="min-w-0 flex-1 truncate text-sm">{nameOf(kv.key)}</span>
                    <span className="shrink-0 text-xs font-medium tabular-nums">{fmt(kv.count)} 次</span>
                  </div>
                  <ShareBar ratio={kv.count / max} className="bg-amber-500" trackClassName="h-1" />
                </li>
              );
            })}
          </ol>
        </div>
      )}
    </Panel>
  );
}

