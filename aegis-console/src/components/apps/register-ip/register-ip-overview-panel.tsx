"use client";

import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, XAxis, YAxis } from "recharts";
import {
  Activity,
  Ban,
  BadgeCheck,
  Eye,
  Flame,
  Globe2,
  LockKeyhole,
  Network,
  ShieldQuestion,
  SlidersHorizontal,
  Smartphone,
  UserPlus,
  Wifi
} from "lucide-react";
import { StatusDot } from "@/components/apps/app-config-primitives";
import {
  ACTION_META,
  Block,
  DeviceText,
  durationLabel,
  formatCount,
  IPText,
  KIND_META,
  LIST_KINDS,
  LIST_META,
  methodLabel,
  REASON_META,
  REGISTERED_COLOR,
  ruleSummary,
  SCOPE_META,
  SurgeSparkline,
  windowLabel
} from "@/components/apps/register-ip/register-ip-shared";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { RegIPEventKind, RegIPPolicy, RegIPReason, RegIPTopItem } from "@/lib/api/register-ip";
import { formatRelative } from "@/lib/datetime";
import { useRegIPOverviewQuery } from "@/lib/register-ip-hooks";
import { cn } from "@/lib/utils";

const RANGES = [7, 14, 30] as const;
const TREND_KINDS: RegIPEventKind[] = ["rejected", "challenged", "challenge_passed", "observed"];

const TREND_CONFIG = buildChartConfig([
  { key: "registered", label: "注册", color: REGISTERED_COLOR },
  ...TREND_KINDS.map((kind) => ({ key: kind, label: KIND_META[kind].label, color: KIND_META[kind].color }))
]);
const DISTRIBUTION_CONFIG = buildChartConfig([{ key: "count", label: "地址数", color: { light: "#0ea5e9", dark: "#38bdf8" } }]);
const REASON_CONFIG = buildChartConfig([{ key: "count", label: "次数", color: { light: "#f43f5e", dark: "#fb7185" } }]);
const DEVICE_DISTRIBUTION_CONFIG = buildChartConfig([{ key: "count", label: "设备数", color: { light: "#d946ef", dark: "#e879f9" } }]);

/**
 * 概览。注册数取台账、拦截取记录，按所选天数与浏览器时区分日；惩罚期与突增是「此刻」的，30 秒刷新一次。
 * 没开突增检测时也按推荐参数算一份突增状态，开之前先看看平时的量。
 */
export function RegIPOverviewPanel({
  appKey,
  onOpenPolicy,
  onOpenIP,
  onOpenDevice
}: {
  appKey: string;
  onOpenPolicy: () => void;
  onOpenIP: (ip: string) => void;
  onOpenDevice: (device: string) => void;
}) {
  const [days, setDays] = useState<number>(7);
  const query = useRegIPOverviewQuery(appKey, days);
  const data = query.data;
  const loading = query.isLoading;

  const trendRows = useMemo(
    () =>
      (data?.trend ?? []).map((point) => {
        const row: Record<string, number | string> = { label: point.date.slice(5), registered: point.registered ?? 0 };
        for (const kind of TREND_KINDS) row[kind] = point[kind] ?? 0;
        return row;
      }),
    [data]
  );
  const trendEmpty = trendRows.every((row) => !row.registered && TREND_KINDS.every((kind) => !row[kind]));

  const reasonRows = useMemo(
    () =>
      Object.entries(data?.reasons ?? {})
        .filter(([, count]) => (count ?? 0) > 0)
        .map(([reason, count]) => ({ label: REASON_META[reason as Exclude<RegIPReason, "">]?.label ?? reason, count: count ?? 0 }))
        .sort((a, b) => b.count - a.count),
    [data]
  );
  const distribution = data?.distribution ?? [];
  const distributionEmpty = distribution.every((row) => !row.count);

  const total = (key: string) => (data ? formatCount((data.totals as Record<string, number>)[key]) : "—");
  const blocked = data ? (data.totals.rejected ?? 0) + (data.totals.challenged ?? 0) : 0;
  const registrations = data?.totals.registrations ?? 0;

  const rangePicker = (
    <ToggleGroup type="single" size="sm" value={String(days)} onValueChange={(next) => next && setDays(Number(next))} className="shrink-0">
      {RANGES.map((range) => (
        <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
          {range} 天
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );

  return (
    <div className="space-y-4">
      <PolicySummary policy={data?.policy} configured={data?.configured ?? false} loading={loading} onOpenPolicy={onOpenPolicy} aside={rangePicker} />

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="新注册" icon={UserPlus} value={total("registrations")} hint={data ? `来自 ${formatCount(data.totals.distinctIps)} 个地址` : undefined} />
        <StatTile
          label="拒绝"
          icon={Ban}
          value={total("rejected")}
          hint={data && registrations + blocked > 0 ? `拦截率 ${((blocked / (registrations + blocked)) * 100).toFixed(1)}%` : undefined}
        />
        <StatTile
          label="要求加验"
          icon={ShieldQuestion}
          value={total("challenged")}
          hint={data?.totals.challenge_passed ? `${formatCount(data.totals.challenge_passed)} 次过验后放行` : undefined}
        />
        <StatTile label="观察到" icon={Eye} value={total("observed")} tone={data?.totals.observed ? "default" : "muted"} />
        <StatTile label="注册网段" icon={Network} value={total("distinctSubnets")} />
        <StatTile
          label="人均地址"
          icon={Wifi}
          value={data && data.totals.distinctIps ? (registrations / data.totals.distinctIps).toFixed(2) : "—"}
          hint="每个地址平均注册的账号数"
        />
        <StatTile label="惩罚期中" icon={LockKeyhole} value={data ? formatCount(data.penaltyCount) : "—"} hint={data?.totals.penalized ? `区间内新增 ${formatCount(data.totals.penalized)} 次` : undefined} />
        <StatTile
          label="名单"
          icon={SlidersHorizontal}
          value={
            data
              ? formatCount(LIST_KINDS.reduce((sum, kind) => sum + (data.lists[kind] ?? 0) + (data.devices.lists[kind] ?? 0), 0))
              : "—"
          }
          hint={
            data
              ? `地址 ${LIST_KINDS.reduce((sum, kind) => sum + (data.lists[kind] ?? 0), 0)}，设备 ${LIST_KINDS.reduce((sum, kind) => sum + (data.devices.lists[kind] ?? 0), 0)}`
              : undefined
          }
        />
      </div>

      <DeviceStrip loading={loading} data={data} />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <ChartCard title="注册与拦截" loading={loading} empty={trendEmpty} height={230}>
          <ChartContainer config={TREND_CONFIG} className="h-[230px] w-full">
            <BarChart data={trendRows} margin={{ left: 0, right: 8, top: 8 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Bar dataKey="registered" fill="var(--color-registered)" radius={[3, 3, 0, 0]} />
              {TREND_KINDS.map((kind, index) => (
                <Bar
                  key={kind}
                  dataKey={kind}
                  stackId="events"
                  fill={`var(--color-${kind})`}
                  radius={index === TREND_KINDS.length - 1 ? [3, 3, 0, 0] : 0}
                />
              ))}
              <ChartLegend content={<ChartLegendContent />} />
            </BarChart>
          </ChartContainer>
        </ChartCard>

        <SurgeCard loading={loading} data={data} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title="每个地址注册的账号数" loading={loading} empty={distributionEmpty} emptyText="暂无注册" height={190}>
          <ChartContainer config={DISTRIBUTION_CONFIG} className="h-[190px] w-full">
            <BarChart data={distribution} margin={{ left: 0, right: 8, top: 12 }}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="bucket" tickLine={false} axisLine={false} fontSize={11} tickFormatter={(v) => `${v} 个`} />
              <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
              <ChartTooltip content={<ChartTooltipContent labelFormatter={(label) => `注册 ${label} 个账号的地址`} />} />
              <Bar dataKey="count" radius={[3, 3, 0, 0]}>
                {distribution.map((row, index) => (
                  // 一个地址注册越多越可疑：颜色随档位加深
                  <Cell key={row.bucket} fill="var(--color-count)" fillOpacity={0.35 + index * 0.16} />
                ))}
              </Bar>
            </BarChart>
          </ChartContainer>
        </ChartCard>

        <ChartCard title="拦截原因" loading={loading} empty={reasonRows.length === 0} emptyText="区间内没有拦截" height={190}>
          <ChartContainer config={REASON_CONFIG} className="h-[190px] w-full">
            <BarChart data={reasonRows} layout="vertical" margin={{ left: 8, right: 24, top: 4, bottom: 4 }}>
              <XAxis type="number" hide allowDecimals={false} />
              <YAxis dataKey="label" type="category" tickLine={false} axisLine={false} fontSize={11} width={76} />
              <ChartTooltip content={<ChartTooltipContent hideLabel />} />
              <Bar dataKey="count" fill="var(--color-count)" radius={[0, 4, 4, 0]} barSize={16} />
            </BarChart>
          </ChartContainer>
        </ChartCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <TopList title="注册最多的地址" items={data?.topIps} loading={loading} onOpen={onOpenIP} unit="个账号" />
        <TopList
          title="注册最多的网段"
          items={data?.topSubnets}
          loading={loading}
          onOpen={(cidr) => onOpenIP(cidr.split("/")[0])}
          unit="个账号"
        />
        <TopList title="被拦最多的地址" items={data?.topBlocked} loading={loading} onOpen={onOpenIP} unit="次" tone="danger" />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <TopList title="注册最多的设备" items={data?.devices.topDevices} loading={loading} onOpen={onOpenDevice} unit="个账号" kind="device" />
        <TopList
          title="设备最多的地址"
          items={data?.devices.topIpDevices}
          loading={loading}
          onOpen={onOpenIP}
          unit="台设备"
          tone="danger"
          hint="一个出口后面冒出很多设备：群控、模拟器"
        />
        <TopList
          title="换地址最多的设备"
          items={data?.devices.topDeviceIps}
          loading={loading}
          onOpen={onOpenDevice}
          unit="个地址"
          kind="device"
          tone="danger"
          hint="一台设备不停换出口：代理池轮换"
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Block title="正在惩罚期" aside={data ? <Badge variant="outline" size="sm">{formatCount(data.penaltyCount)}</Badge> : null} bodyClassName="p-0">
          {loading ? (
            <ListSkeleton />
          ) : !data?.penalties.length ? (
            <p className="px-4 py-10 text-center text-xs text-muted-foreground">没有地址在惩罚期</p>
          ) : (
            <ul className="divide-y divide-border">
              {data.penalties.map((item) => (
                <li key={item.subject} className="flex items-center gap-3 px-4 py-2.5">
                  <LockKeyhole className="size-4 shrink-0 text-violet-500" />
                  {item.subject.startsWith("device:") ? (
                    <DeviceText value={item.subject.slice(7)} className="min-w-0 flex-1" onClick={() => onOpenDevice(item.subject.slice(7))} />
                  ) : (
                    <IPText value={item.subject} className="min-w-0 flex-1" onClick={() => onOpenIP(item.subject.split("/")[0])} />
                  )}
                  <Badge variant="outline" size="sm">第 {item.level} 次</Badge>
                  <span className="w-20 text-right text-[11px] tabular-nums text-muted-foreground">剩 {durationLabel(item.remaining)}</span>
                </li>
              ))}
            </ul>
          )}
        </Block>

        <Block title="规则命中" bodyClassName="p-0">
          {loading ? (
            <ListSkeleton />
          ) : !data?.rules.length ? (
            <p className="px-4 py-10 text-center text-xs text-muted-foreground">区间内没有规则被触发</p>
          ) : (
            <ul className="divide-y divide-border">
              {data.rules.slice(0, 8).map((item) => {
                const meta = KIND_META[item.kind];
                return (
                  <li key={`${item.ruleId}-${item.kind}`} className="flex items-center gap-3 px-4 py-2.5">
                    <meta.icon className={cn("size-3.5 shrink-0", meta.variant === "danger" ? "text-rose-500" : "text-muted-foreground")} />
                    <span className="min-w-0 flex-1 truncate text-xs">{item.ruleName || item.ruleId}</span>
                    <span className="text-[11px] text-muted-foreground">{meta.label}</span>
                    <span className="w-12 text-right text-xs font-medium tabular-nums">{formatCount(item.count)}</span>
                  </li>
                );
              })}
            </ul>
          )}
        </Block>
      </div>

      {data ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <Activity className="size-3" /> 写入队列
          </span>
          {(["ledger", "events"] as const).map((key) => {
            const stats = data.writers[key];
            return (
              <span key={key} className="tabular-nums">
                {key === "ledger" ? "台账" : "记录"}：已写 {formatCount(stats.written)}，排队 {stats.pending}
                {stats.dropped ? <span className="text-rose-600 dark:text-rose-400">，丢弃 {stats.dropped}</span> : null}
                {stats.failed ? <span className="text-rose-600 dark:text-rose-400">，失败 {stats.failed}</span> : null}
              </span>
            );
          })}
          <span>
            注册方式：
            {Object.entries(data.methods)
              .map(([method, count]) => `${methodLabel(method)} ${formatCount(count)}`)
              .join("，") || "—"}
          </span>
        </div>
      ) : null}
    </div>
  );
}

function SurgeCard({ loading, data }: { loading: boolean; data?: ReturnType<typeof useRegIPOverviewQuery>["data"] }) {
  const surge = data?.surge;
  const cfg = data?.policy.surge;
  const enabled = Boolean(cfg?.enabled && data?.policy.enabled);
  return (
    <Block
      title={
        <span className="flex items-center gap-1.5">
          <Flame className={cn("size-4", surge?.active ? "text-rose-500" : "text-muted-foreground")} />
          注册突增
        </span>
      }
      aside={
        surge?.active ? (
          <Badge variant="danger" size="sm">突增中</Badge>
        ) : (
          <Badge variant={enabled ? "success" : "outline"} size="sm">{enabled ? "检测中" : "未开启（按推荐参数试算）"}</Badge>
        )
      }
    >
      {loading || !surge ? (
        <Skeleton className="h-[170px] w-full" />
      ) : (
        <div className="space-y-3">
          <SurgeSparkline series={surge.series ?? []} windowMinutes={cfg?.windowMinutes ?? 5} threshold={surge.threshold} active={surge.active} height={72} />
          <div className="grid grid-cols-3 gap-2 text-center">
            <Metric label={`最近 ${cfg?.windowMinutes ?? 5} 分钟`} value={formatCount(surge.current)} strong={surge.active} />
            <Metric label="平常期望" value={surge.expected.toFixed(1)} />
            <Metric label="触发线" value={surge.threshold.toFixed(1)} />
          </div>
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            偏离 {surge.score.toFixed(1)} 个标准差（灵敏度 {cfg?.sensitivity ?? 4}）。
            {enabled && cfg
              ? `触发后${ACTION_META[cfg.action].label}，配额收紧为 ${Math.round(cfg.tightenFactor * 100)}%。`
              : "开启后按策略处置并收紧配额。"}
          </p>
        </div>
      )}
    </Block>
  );
}

/** 设备一侧的一行：带设备的注册占比、不同设备数、每台设备账号数分布 */
function DeviceStrip({ loading, data }: { loading: boolean; data?: ReturnType<typeof useRegIPOverviewQuery>["data"] }) {
  const total = (data?.totals.withDevice ?? 0) + (data?.totals.withoutDevice ?? 0);
  const coverage = total ? ((data?.totals.withDevice ?? 0) / total) * 100 : 0;
  const distribution = data?.devices.distribution ?? [];
  const empty = distribution.every((row) => !row.count);
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
      <Block
        title={
          <span className="flex items-center gap-1.5">
            <Smartphone className="size-4 text-fuchsia-500" />
            设备一侧
          </span>
        }
      >
        {loading || !data ? (
          <Skeleton className="h-[120px] w-full" />
        ) : (
          <div className="space-y-4">
            <div className="grid grid-cols-3 gap-2 text-center">
              <Metric label="不同设备" value={formatCount(data.totals.distinctDevices)} />
              <Metric label="人均设备" value={data.totals.distinctDevices ? (data.totals.withDevice / data.totals.distinctDevices).toFixed(2) : "—"} />
              <Metric label="未上报设备" value={formatCount(data.totals.withoutDevice)} strong={coverage < 80 && total > 0} />
            </div>
            <div className="space-y-1.5">
              <div className="flex items-baseline justify-between text-[11px]">
                <span className="text-muted-foreground">带设备标识的注册</span>
                <span className="font-medium tabular-nums">{total ? `${coverage.toFixed(1)}%` : "—"}</span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-fuchsia-500 transition-[width] duration-200" style={{ width: `${coverage}%` }} />
              </div>
              <p className="text-[10px] leading-relaxed text-muted-foreground">
                不带设备标识的注册只受地址一侧约束；占比偏高时，检查客户端是否上报 X-Device-Id，或在策略里要求设备标识。
              </p>
            </div>
          </div>
        )}
      </Block>
      <ChartCard title="每台设备注册的账号数" loading={loading} empty={empty} emptyText="暂无带设备的注册" height={160}>
        <ChartContainer config={DEVICE_DISTRIBUTION_CONFIG} className="h-[160px] w-full">
          <BarChart data={distribution} margin={{ left: 0, right: 8, top: 12 }}>
            <CartesianGrid vertical={false} strokeDasharray="3 3" />
            <XAxis dataKey="bucket" tickLine={false} axisLine={false} fontSize={11} tickFormatter={(v) => `${v} 个`} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} fontSize={11} width={36} />
            <ChartTooltip content={<ChartTooltipContent labelFormatter={(label) => `注册 ${label} 个账号的设备`} />} />
            <Bar dataKey="count" radius={[3, 3, 0, 0]}>
              {distribution.map((row, index) => (
                <Cell key={row.bucket} fill="var(--color-count)" fillOpacity={0.35 + index * 0.16} />
              ))}
            </Bar>
          </BarChart>
        </ChartContainer>
      </ChartCard>
    </div>
  );
}

function Metric({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className="rounded-lg bg-muted px-2 py-2">
      <div className={cn("text-base font-semibold tabular-nums", strong && "text-rose-600 dark:text-rose-400")}>{value}</div>
      <div className="text-[10px] text-muted-foreground">{label}</div>
    </div>
  );
}

function TopList({
  title,
  items,
  loading,
  onOpen,
  unit,
  tone,
  kind = "ip",
  hint
}: {
  title: string;
  items?: RegIPTopItem[];
  loading: boolean;
  onOpen: (key: string) => void;
  unit: string;
  tone?: "danger";
  kind?: "ip" | "device";
  hint?: string;
}) {
  const peak = Math.max(1, ...(items ?? []).map((item) => item.count));
  return (
    <Block title={title} aside={hint ? <span className="truncate text-[10px] text-muted-foreground">{hint}</span> : null} bodyClassName="p-0">
      {loading ? (
        <ListSkeleton />
      ) : !items?.length ? (
        <p className="px-4 py-10 text-center text-xs text-muted-foreground">暂无</p>
      ) : (
        <ul className="divide-y divide-border">
          {items.map((item, index) => (
            <li key={item.key} className="relative px-4 py-2">
              {/* 底部比例条：排行之间的差距比数字更直观 */}
              <span
                className={cn("absolute inset-y-0 left-0 opacity-[0.07]", tone === "danger" ? "bg-rose-500" : "bg-indigo-500")}
                style={{ width: `${(item.count / peak) * 100}%` }}
              />
              <div className="relative flex items-center gap-2.5">
                <span className="w-4 shrink-0 text-[11px] tabular-nums text-muted-foreground">{index + 1}</span>
                <div className="min-w-0 flex-1">
                  {kind === "device" ? (
                    <DeviceText value={item.key} onClick={() => onOpen(item.key)} />
                  ) : (
                    <IPText value={item.key} onClick={() => onOpen(item.key)} />
                  )}
                  <div className="truncate text-[10px] text-muted-foreground">
                    {item.location || "属地未知"} · {formatRelative(item.lastAt)}
                    {item.accounts && unit !== "个账号" ? ` · ${formatCount(item.accounts)} 个账号` : ""}
                    {item.accounts && unit === "个账号" ? ` · ${formatCount(item.accounts)} 个地址` : ""}
                  </div>
                </div>
                <span className={cn("shrink-0 text-xs font-medium tabular-nums", tone === "danger" && "text-rose-600 dark:text-rose-400")}>
                  {formatCount(item.count)}
                  <span className="ml-0.5 text-[10px] font-normal text-muted-foreground">{unit}</span>
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Block>
  );
}

function ListSkeleton() {
  return (
    <div className="space-y-2 p-4">
      {[0, 1, 2, 3].map((index) => (
        <Skeleton key={index} className="h-8 w-full" />
      ))}
    </div>
  );
}

/** 策略摘要条：开没开、什么模式、几条规则、惩罚期、突增、附加限制 */
function PolicySummary({
  policy,
  configured,
  loading,
  onOpenPolicy,
  aside
}: {
  policy?: RegIPPolicy;
  configured: boolean;
  loading: boolean;
  onOpenPolicy: () => void;
  aside: React.ReactNode;
}) {
  if (loading || !policy) return <Skeleton className="h-[120px] w-full rounded-2xl" />;
  const rules = policy.rules.filter((rule) => rule.enabled);
  return (
    <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3">
        <div className="flex flex-wrap items-center gap-3">
          <StatusDot active={policy.enabled} labelActive="注册准入已开启" labelInactive="注册准入未开启" />
          {policy.enabled && policy.mode === "observe" ? (
            <Badge variant="info" size="sm" className="gap-1">
              <Eye className="size-3" /> 观察模式：只记录不拦截
            </Badge>
          ) : null}
          {!configured && policy.enabled ? <Badge variant="outline" size="sm">沿用旧开关「注册 IP 唯一」</Badge> : null}
        </div>
        <div className="flex items-center gap-2">
          {aside}
          <Button size="sm" variant="outline" className="h-7" onClick={onOpenPolicy}>
            <SlidersHorizontal className="size-3.5" /> 编辑策略
          </Button>
        </div>
      </div>
      <div className={cn("grid gap-px bg-border sm:grid-cols-2 xl:grid-cols-4", !policy.enabled && "opacity-60")}>
        <SummaryCell label={`配额规则 ${rules.length} 条`}>
          {rules.length ? (
            <ul className="space-y-1">
              {rules.slice(0, 3).map((rule) => {
                const Icon = SCOPE_META[rule.scope].icon;
                return (
                  <li key={rule.id} className="flex items-center gap-1.5 text-xs">
                    <Icon className="size-3 shrink-0 text-muted-foreground" />
                    <span className="truncate">{ruleSummary(rule)}</span>
                    <span className="shrink-0 text-[10px] text-muted-foreground">{ACTION_META[rule.action].label}</span>
                  </li>
                );
              })}
              {rules.length > 3 ? <li className="text-[11px] text-muted-foreground">另有 {rules.length - 3} 条</li> : null}
            </ul>
          ) : (
            <span className="text-sm text-muted-foreground">没有配额规则</span>
          )}
        </SummaryCell>
        <SummaryCell label="惩罚期">
          {policy.penalty.enabled ? (
            <div className="text-xs">
              <div className="font-medium">
                {durationLabel(policy.penalty.strikeWindowSeconds)}内被拒 {policy.penalty.strikes} 次
              </div>
              <div className="mt-0.5 text-[11px] text-muted-foreground">
                首次 {durationLabel(policy.penalty.baseSeconds)}，每次 ×{policy.penalty.multiplier}，最长 {durationLabel(policy.penalty.maxSeconds)}
              </div>
            </div>
          ) : (
            <span className="text-sm text-muted-foreground">未开启</span>
          )}
        </SummaryCell>
        <SummaryCell label="突增检测">
          {policy.surge.enabled ? (
            <div className="text-xs">
              <div className="font-medium">
                {policy.surge.windowMinutes} 分钟对比 {policy.surge.baselineMinutes} 分钟基线
              </div>
              <div className="mt-0.5 text-[11px] text-muted-foreground">
                {policy.surge.sensitivity}σ 触发，{ACTION_META[policy.surge.action].label}，配额收紧到 {Math.round(policy.surge.tightenFactor * 100)}%
              </div>
            </div>
          ) : (
            <span className="text-sm text-muted-foreground">未开启</span>
          )}
        </SummaryCell>
        <SummaryCell label="附加限制">
          <div className="flex flex-wrap gap-1.5">
            {policy.isp.enabled ? (
              <Badge variant="outline" size="sm" className="font-normal">运营商 {policy.isp.keywords.length} 个关键词</Badge>
            ) : null}
            {policy.geo.mode !== "off" ? (
              <Badge variant="outline" size="sm" className="gap-1 font-normal">
                <Globe2 className="size-3" />
                {policy.geo.mode === "allow" ? "只允许" : "拒绝"} {policy.geo.regions.slice(0, 2).join("、")}
                {policy.geo.regions.length > 2 ? ` 等 ${policy.geo.regions.length} 处` : ""}
              </Badge>
            ) : null}
            {policy.missingIp === "reject" ? <Badge variant="outline" size="sm" className="font-normal">缺 IP 拒绝</Badge> : null}
            {policy.device.missing !== "allow" ? (
              <Badge variant="outline" size="sm" className="gap-1 font-normal">
                <Smartphone className="size-3" /> 缺设备{policy.device.missing === "reject" ? "拒绝" : "加验"}
              </Badge>
            ) : null}
            {policy.device.invalid !== "allow" ? (
              <Badge variant="outline" size="sm" className="font-normal">无效设备{policy.device.invalid === "reject" ? "拒绝" : "加验"}</Badge>
            ) : null}
            {policy.failMode === "closed" ? <Badge variant="warning" size="sm" className="font-normal">故障时拒绝</Badge> : null}
            {!policy.isp.enabled &&
            policy.geo.mode === "off" &&
            policy.missingIp !== "reject" &&
            policy.failMode !== "closed" &&
            policy.device.missing === "allow" &&
            policy.device.invalid === "allow" ? (
              <span className="flex items-center gap-1 text-sm text-muted-foreground">
                <BadgeCheck className="size-3.5" /> 无
              </span>
            ) : null}
          </div>
        </SummaryCell>
      </div>
    </section>
  );
}

function SummaryCell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="bg-card px-5 py-3.5">
      <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{label}</div>
      {children}
    </div>
  );
}
