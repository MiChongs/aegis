"use client";

import { useMemo, useState, type ReactNode } from "react";
import NumberFlow, { type Format } from "@number-flow/react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Label, Pie, PieChart, XAxis, YAxis } from "recharts";
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Boxes,
  ClipboardCheck,
  Gauge,
  Globe2,
  LayoutGrid,
  ListChecks,
  ShieldAlert,
  ShieldBan,
  Timer,
  Waypoints,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Bar as ShareBar, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { CountryFlag } from "@/components/ui/country-flag";
import { Skeleton } from "@/components/ui/skeleton";
import type { DashboardData, KV, RiskAction } from "@/lib/api/risk";
import { formatClock, formatShortDateTime, formatWith } from "@/lib/datetime";
import { useRiskDashboard, useRiskPatterns } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { RANGE_PRESETS, errorText, useRangeFrom, useSceneName, type RangeValue } from "./decisions";
import { ACTION_COLOR, ACTION_LABEL, ACTION_ORDER, formatMicros } from "./kit/labels";

const ACTION_CONFIG = Object.fromEntries(ACTION_ORDER.map((a) => [a, { label: ACTION_LABEL[a], color: ACTION_COLOR[a] }])) satisfies ChartConfig;

const fmt = (n: number) => n.toLocaleString("zh-CN");
const pct = (n: number) => `${(n * 100).toFixed(n >= 0.1 ? 1 : 2)}%`;

export function RiskOverview({ app, onNavigate }: { app?: number; onNavigate: (tab: string) => void }) {
  const [range, setRange] = useState<RangeValue>("24h");
  const from = useRangeFrom(range);
  const query = useRiskDashboard({ app, from });
  const data = query.data;

  const rangeSwitch = <Segmented value={range} options={RANGE_PRESETS} onChange={setRange} ariaLabel="时间范围" />;

  if (!data) {
    return (
      <TabStack>
        <div className="flex justify-end">{rangeSwitch}</div>
        {query.isError ? (
          <Panel icon={AlertTriangle} tone="rose" title="读取失败">
            <p className="text-sm text-muted-foreground">{errorText(query.error)}</p>
          </Panel>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3 @3xl:grid-cols-5">
              {Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} className="h-[112px] rounded-xl" />
              ))}
            </div>
            <div className="grid gap-4 @4xl:grid-cols-[minmax(0,1fr)_320px]">
              <Skeleton className="h-[340px] rounded-2xl" />
              <Skeleton className="h-[340px] rounded-2xl" />
            </div>
            <div className="grid gap-4 @4xl:grid-cols-2">
              <Skeleton className="h-[320px] rounded-2xl" />
              <Skeleton className="h-[320px] rounded-2xl" />
            </div>
          </>
        )}
      </TabStack>
    );
  }

  const counts = actionCounts(data);
  const total = ACTION_ORDER.reduce((s, a) => s + counts[a], 0);
  const empty = total === 0 && !(data.alerts?.length);

  return (
    <TabStack>
      <KpiRow data={data} counts={counts} total={total} onNavigate={onNavigate} />
      {empty ? (
        <Panel icon={Activity} tone="sky" title="决策趋势" aside={rangeSwitch}>
          <CardEmpty icon={ShieldAlert} title="暂无决策" />
        </Panel>
      ) : (
        <>
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-[minmax(0,1fr)_320px]">
            <TrendPanel data={data} range={range} aside={rangeSwitch} onNavigate={onNavigate} />
            <MixPanel counts={counts} total={total} />
          </div>
          <div className="grid min-w-0 gap-4 @4xl:grid-cols-2">
            <ScenePanel data={data} />
            <RulesPanel rules={data.topRules ?? []} />
          </div>
          <div className="grid min-w-0 gap-4 @3xl:grid-cols-2 @6xl:grid-cols-3">
            <IpPanel ips={data.topIPs ?? []} />
            <CountryPanel countries={data.topCountries ?? []} />
            <AlertPanel alerts={data.alerts ?? []} onNavigate={onNavigate} />
          </div>
        </>
      )}
    </TabStack>
  );
}

function actionCounts(data: DashboardData): Record<RiskAction, number> {
  const out = { pass: 0, review: 0, captcha: 0, block: 0, ban: 0 } as Record<RiskAction, number>;
  for (const kv of data.byAction ?? []) {
    if (kv.key in out) out[kv.key as RiskAction] += kv.count;
  }
  return out;
}

/* ── 指标 ── */

const ACCENT = {
  sky: "bg-sky-500",
  rose: "bg-rose-500",
  amber: "bg-amber-500",
  violet: "bg-violet-500",
  emerald: "bg-emerald-500",
} as const;

function Kpi({
  icon: Icon,
  label,
  value,
  format,
  display,
  sub,
  accent,
  alert,
  onClick,
}: {
  icon: typeof Activity;
  label: string;
  value?: number;
  format?: Format;
  display?: ReactNode;
  sub?: ReactNode;
  accent: keyof typeof ACCENT;
  alert?: boolean;
  onClick?: () => void;
}) {
  const body = (
    <>
      <span aria-hidden className={cn("absolute inset-y-4 left-0 w-1 rounded-r-full", ACCENT[accent])} />
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className="size-3.5" />
        <span className="truncate">{label}</span>
        {onClick ? <ArrowRight className="ml-auto size-3.5 opacity-0 transition-opacity group-hover:opacity-100" /> : null}
      </div>
      <div
        className={cn(
          "text-3xl leading-9 font-semibold tracking-tight tabular-nums",
          alert && "text-rose-600 dark:text-rose-400"
        )}
      >
        {display ?? <NumberFlow value={value ?? 0} locales="zh-CN" format={format ?? { notation: "compact", maximumFractionDigits: 1 }} />}
      </div>
      <div className="min-h-4 truncate text-[11px] leading-4 text-muted-foreground tabular-nums">{sub}</div>
    </>
  );
  const cls = "group relative flex min-h-[112px] min-w-0 flex-col justify-between gap-1 overflow-hidden rounded-xl border border-border bg-card p-4 pl-5 text-left";
  return onClick ? (
    <button type="button" onClick={onClick} className={cn(cls, "transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none")}>
      {body}
    </button>
  ) : (
    <div className={cls}>{body}</div>
  );
}

function KpiRow({
  data,
  counts,
  total,
  onNavigate,
}: {
  data: DashboardData;
  counts: Record<RiskAction, number>;
  total: number;
  onNavigate: (tab: string) => void;
}) {
  const blocked = counts.block + counts.ban;
  const rate = total > 0 ? blocked / total : 0;
  const challenged = counts.review + counts.captcha;
  const pending = data.pendingCases ?? 0;
  const lat = data.latency;
  return (
    <div className="grid min-w-0 grid-cols-2 gap-3 @3xl:grid-cols-5">
      <Kpi icon={Activity} label="决策总数" value={total} accent="sky" sub={`放行 ${fmt(counts.pass)}`} />
      <Kpi
        icon={ShieldBan}
        label="拦截率"
        value={rate}
        format={{ style: "percent", maximumFractionDigits: rate >= 0.1 ? 1 : 2 }}
        accent="rose"
        sub={`拦截 ${fmt(counts.block)} 封禁 ${fmt(counts.ban)}`}
      />
      <Kpi
        icon={ShieldAlert}
        label="复核与验证码"
        value={challenged}
        accent="amber"
        sub={`复核 ${fmt(counts.review)} 验证码 ${fmt(counts.captcha)}`}
      />
      <Kpi
        icon={ClipboardCheck}
        label="待复核"
        value={pending}
        accent="violet"
        alert={pending > 0}
        sub={pending > 0 ? "前往处理" : "已清空"}
        onClick={() => onNavigate("cases")}
      />
      <Kpi
        icon={Timer}
        label="P99 耗时"
        display={lat ? formatMicros(lat.p99) : "—"}
        accent="emerald"
        sub={lat ? `P50 ${formatMicros(lat.p50)} 均值 ${formatMicros(lat.avg)}` : undefined}
      />
    </div>
  );
}

/* ── 趋势 ── */

const RANGE_MS: Record<RangeValue, number> = { "1h": 3600_000, "24h": 86400_000, "7d": 7 * 86400_000, "30d": 30 * 86400_000 };

function bucketLabel(t: string, range: RangeValue) {
  if (range === "1h" || range === "24h") return formatClock(t);
  if (range === "7d") return formatShortDateTime(t);
  return formatWith(t, { month: "2-digit", day: "2-digit" });
}

function TrendPanel({ data, range, aside, onNavigate }: { data: DashboardData; range: RangeValue; aside: ReactNode; onNavigate: (tab: string) => void }) {
  const rows = useMemo(() => {
    // 后端只返回有数据的桶（UTC 对齐：≤6 小时 5 分钟一桶，≤7 天 1 小时，更长 1 天）；这里把空桶补齐成 0，
    // 否则只有一两个桶有数据时趋势图画不出来
    const span = RANGE_MS[range];
    const step = span <= 6 * 3600_000 ? 300_000 : span <= 7 * 86400_000 ? 3600_000 : 86400_000;
    // 区间以后端回显的 from / to 为准（没有时只画有数据的桶）
    const to = data.to ? Date.parse(data.to) : 0;
    const from = data.from ? Date.parse(data.from) : to;
    const map = new Map<number, Record<string, number | string>>();
    const blank = (ms: number) => {
      const t = new Date(ms).toISOString();
      return { t, label: bucketLabel(t, range), pass: 0, review: 0, captcha: 0, block: 0, ban: 0 } as Record<string, number | string>;
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
    }
    return [...map.entries()].sort(([a], [b]) => a - b).map(([, r]) => r);
  }, [data.series, data.from, data.to, range]);
  const present = ACTION_ORDER.filter((a) => rows.some((r) => (r[a] as number) > 0));

  return (
    <Panel
      icon={Activity}
      tone="sky"
      title="决策趋势"
      aside={
        <>
          {aside}
          <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("decisions")}>
            流水
            <ArrowRight />
          </Button>
        </>
      }
    >
      {!(data.series ?? []).length ? (
        <div className="grid h-[268px] place-items-center rounded-xl bg-muted/40 text-xs text-muted-foreground">暂无数据</div>
      ) : (
        <ChartContainer config={ACTION_CONFIG} className="aspect-auto h-[268px] w-full">
          <AreaChart data={rows} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="label" tickLine={false} axisLine={false} tickMargin={8} minTickGap={40} />
            <YAxis tickLine={false} axisLine={false} width={44} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {present.map((a) => (
              <Area
                key={a}
                dataKey={a}
                type="monotone"
                stackId="actions"
                stroke={`var(--color-${a})`}
                fill={`var(--color-${a})`}
                fillOpacity={0.32}
                strokeWidth={1.5}
                isAnimationActive={false}
              />
            ))}
          </AreaChart>
        </ChartContainer>
      )}
    </Panel>
  );
}

function compact(v: number) {
  if (v >= 100_000_000) return `${(v / 100_000_000).toFixed(1)}亿`;
  if (v >= 10_000) return `${(v / 10_000).toFixed(v >= 100_000 ? 0 : 1)}万`;
  return String(v);
}

/* ── 处置构成 ── */

function MixPanel({ counts, total }: { counts: Record<RiskAction, number>; total: number }) {
  const rows = ACTION_ORDER.map((a) => ({ key: a, value: counts[a] })).filter((r) => r.value > 0);
  return (
    <Panel icon={Gauge} tone="violet" title="处置构成">
      <div className="flex min-w-0 flex-col items-center gap-4">
        <ChartContainer config={ACTION_CONFIG} className="aspect-square h-[180px]">
          <PieChart>
            <ChartTooltip content={<ChartTooltipContent nameKey="key" hideLabel />} />
            <Pie data={rows.length ? rows : [{ key: "pass", value: 1 }]} dataKey="value" nameKey="key" innerRadius={58} outerRadius={84} strokeWidth={2} paddingAngle={rows.length > 1 ? 2 : 0}>
              {(rows.length ? rows : [{ key: "pass", value: 1 }]).map((r) => (
                <Cell key={r.key} fill={rows.length ? `var(--color-${r.key})` : "var(--muted)"} />
              ))}
              <Label
                content={({ viewBox }) => {
                  if (!viewBox || !("cx" in viewBox)) return null;
                  return (
                    <text x={viewBox.cx} y={viewBox.cy} textAnchor="middle" dominantBaseline="middle">
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) - 6} className="fill-foreground text-xl font-semibold">
                        {compact(total)}
                      </tspan>
                      <tspan x={viewBox.cx} y={(viewBox.cy ?? 0) + 14} className="fill-muted-foreground text-[11px]">
                        次决策
                      </tspan>
                    </text>
                  );
                }}
              />
            </Pie>
          </PieChart>
        </ChartContainer>
        <ul className="flex w-full flex-col gap-2">
          {ACTION_ORDER.map((a) => {
            const share = total ? counts[a] / total : 0;
            return (
              <li key={a} className="flex min-w-0 items-center gap-2 text-xs">
                <span aria-hidden className="size-2.5 shrink-0 rounded-[3px]" style={{ background: ACTION_COLOR[a] }} />
                <span className="min-w-0 flex-1 truncate">{ACTION_LABEL[a]}</span>
                <span className="font-medium tabular-nums">{fmt(counts[a])}</span>
                <span className="w-14 text-right text-muted-foreground tabular-nums">{pct(share)}</span>
              </li>
            );
          })}
        </ul>
      </div>
    </Panel>
  );
}

/* ── 场景 × 处置 ── */

function ScenePanel({ data }: { data: DashboardData }) {
  const sceneName = useSceneName();
  const rows = useMemo(() => {
    const map = new Map<string, Record<string, number | string>>();
    for (const r of data.byScene ?? []) {
      let row = map.get(r.scene);
      if (!row) {
        row = { scene: r.scene, name: sceneName(r.scene), total: 0, pass: 0, review: 0, captcha: 0, block: 0, ban: 0 };
        map.set(r.scene, row);
      }
      row[r.action] = ((row[r.action] as number) ?? 0) + r.count;
      row.total = (row.total as number) + r.count;
    }
    return [...map.values()].sort((a, b) => (b.total as number) - (a.total as number)).slice(0, 10);
  }, [data.byScene, sceneName]);
  const present = ACTION_ORDER.filter((a) => rows.some((r) => (r[a] as number) > 0));

  return (
    <Panel icon={LayoutGrid} tone="primary" title="场景处置">
      {rows.length === 0 ? (
        <CardEmpty icon={LayoutGrid} title="暂无数据" />
      ) : (
        <ChartContainer config={ACTION_CONFIG} className="aspect-auto w-full" style={{ height: Math.max(160, rows.length * 34 + 48) }}>
          <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 8, bottom: 0, left: 0 }} barCategoryGap={8}>
            <CartesianGrid horizontal={false} strokeDasharray="3 3" />
            <XAxis type="number" tickLine={false} axisLine={false} allowDecimals={false} tickFormatter={(v: number) => compact(v)} />
            <YAxis type="category" dataKey="name" tickLine={false} axisLine={false} width={96} tickFormatter={(v: string) => trim(v, 8)} />
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {present.map((a) => (
              <Bar key={a} dataKey={a} stackId="actions" fill={`var(--color-${a})`} isAnimationActive={false} />
            ))}
          </BarChart>
        </ChartContainer>
      )}
    </Panel>
  );
}

function trim(v: string, n: number) {
  return v.length > n ? `${v.slice(0, n - 1)}…` : v;
}

/* ── 规则 ── */

const RULE_CONFIG = { count: { label: "命中", color: "var(--chart-4)" } } satisfies ChartConfig;

function RulesPanel({ rules }: { rules: KV[] }) {
  const rows = rules.slice(0, 12);
  return (
    <Panel icon={ListChecks} tone="amber" title="命中最多的规则">
      {rows.length === 0 ? (
        <CardEmpty icon={ListChecks} title="暂无命中" />
      ) : (
        <ChartContainer config={RULE_CONFIG} className="aspect-auto w-full" style={{ height: Math.max(160, rows.length * 28 + 16) }}>
          <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 40, bottom: 0, left: 0 }} barCategoryGap={6}>
            <XAxis type="number" hide />
            <YAxis
              type="category"
              dataKey="key"
              tickLine={false}
              axisLine={false}
              width={150}
              tick={{ fontSize: 11, fontFamily: "var(--font-mono, ui-monospace)" }}
              tickFormatter={(v: string) => trim(v, 20)}
            />
            <ChartTooltip cursor={false} content={<ChartTooltipContent hideIndicator />} />
            <Bar
              dataKey="count"
              fill="var(--color-count)"
              radius={[0, 4, 4, 0]}
              isAnimationActive={false}
              label={{ position: "right", className: "fill-muted-foreground text-[11px] tabular-nums", formatter: (v: unknown) => compact(Number(v)) }}
            />
          </BarChart>
        </ChartContainer>
      )}
    </Panel>
  );
}

/* ── 榜单 ── */

function Ranked({ rows, render, barClass }: { rows: KV[]; render: (kv: KV) => ReactNode; barClass: string }) {
  const max = rows[0]?.count ?? 1;
  return (
    <ol className="flex flex-col gap-2.5">
      {rows.map((kv, i) => (
        <li key={kv.key || i} className="flex min-w-0 flex-col gap-1">
          <div className="flex min-w-0 items-center gap-2">
            <span
              className={cn(
                "grid size-5 shrink-0 place-items-center rounded-md text-[11px] tabular-nums",
                i < 3 ? "bg-foreground text-background" : "bg-muted text-muted-foreground"
              )}
            >
              {i + 1}
            </span>
            <div className="min-w-0 flex-1 truncate">{render(kv)}</div>
            <span className="shrink-0 text-xs font-medium tabular-nums">{fmt(kv.count)}</span>
          </div>
          <div className="pl-7">
            <ShareBar ratio={kv.count / max} className={barClass} trackClassName="h-1" />
          </div>
        </li>
      ))}
    </ol>
  );
}

function IpPanel({ ips }: { ips: KV[] }) {
  return (
    <Panel icon={Waypoints} tone="rose" title="被拦截最多的 IP">
      {ips.length === 0 ? (
        <CardEmpty icon={Waypoints} title="暂无拦截" />
      ) : (
        <Ranked rows={ips} barClass="bg-rose-500/70" render={(kv) => <span className="font-mono text-xs">{kv.key}</span>} />
      )}
    </Panel>
  );
}

function CountryPanel({ countries }: { countries: KV[] }) {
  const regionName = useMemo(() => {
    try {
      return new Intl.DisplayNames(["zh-CN"], { type: "region" });
    } catch {
      return null;
    }
  }, []);
  return (
    <Panel icon={Globe2} tone="sky" title="风险来源地区">
      {countries.length === 0 ? (
        <CardEmpty icon={Globe2} title="暂无数据" />
      ) : (
        <Ranked
          rows={countries}
          barClass="bg-sky-500/70"
          render={(kv) =>
            kv.key ? (
              <span className="inline-flex min-w-0 items-center gap-1.5 text-sm">
                <CountryFlag code={kv.key} size={14} />
                <span className="truncate">{safeRegion(regionName, kv.key)}</span>
              </span>
            ) : (
              <span className="text-sm text-muted-foreground">未知</span>
            )
          }
        />
      )}
    </Panel>
  );
}

function safeRegion(names: Intl.DisplayNames | null, code: string) {
  try {
    return names?.of(code.toUpperCase()) ?? code;
  } catch {
    return code;
  }
}

function AlertPanel({ alerts, onNavigate }: { alerts: KV[]; onNavigate: (tab: string) => void }) {
  const patterns = useRiskPatterns();
  const names = useMemo(() => new Map((patterns.data ?? []).map((p) => [p.key, p.name])), [patterns.data]);
  return (
    <Panel
      icon={Boxes}
      tone="amber"
      title="行为模式告警"
      aside={
        <Button size="sm" variant="ghost" className="h-8 text-xs" onClick={() => onNavigate("patterns")}>
          模式
          <ArrowRight />
        </Button>
      }
    >
      {alerts.length === 0 ? (
        <CardEmpty icon={Boxes} title="暂无告警" />
      ) : (
        <Ranked
          rows={alerts}
          barClass="bg-amber-500/70"
          render={(kv) => (
            <span className="inline-flex min-w-0 items-baseline gap-1.5">
              <span className="truncate text-sm">{names.get(kv.key) ?? kv.key}</span>
              {names.has(kv.key) ? <span className="truncate font-mono text-[11px] text-muted-foreground">{kv.key}</span> : null}
            </span>
          )}
        />
      )}
    </Panel>
  );
}
