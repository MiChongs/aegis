"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import {
  Activity,
  ArrowUpRight,
  Ban,
  ChevronDown,
  Gauge,
  Loader2,
  Network,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  Smartphone,
  Users
} from "lucide-react";
import { SectionCard } from "@/components/apps/app-config-primitives";
import { Segmented } from "@/components/apps/lottery/editor/editor-fields";
import {
  RiskActionChip,
  riskActionDetail,
  riskActions,
  riskActionVisual,
  riskConditionText,
  riskSignalOf,
  riskValueText
} from "@/components/apps/lottery/lottery-risk-shared";
import { formatCount } from "@/components/apps/lottery/lottery-shared";
import { formatStoreDate } from "@/components/apps/points-store/points-store-shared";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import type { LotteryCatalog, LotteryRiskCluster, LotteryRiskEvent, LotteryRiskOverview } from "@/lib/api/lottery";
import { useLotteryActivitiesQuery, useLotteryCatalogQuery, useLotteryRiskQuery } from "@/lib/lottery-hooks";
import { cn } from "@/lib/utils";

/** 跳到抽奖记录时带的筛选 */
export type RiskDrawJump = { activityId: number; risk?: string; keyword?: string };

const RANGES = [
  { value: "1", label: "今天" },
  { value: "7", label: "7 天" },
  { value: "30", label: "30 天" }
] as const;

/**
 * 风控总览：一个活动最近 N 天风控拦了谁、按哪条规则、怎么处置的。
 *
 * 规则逐条列出命中次数（点一下进抽奖记录看那些记录）；设备、IP、网段按账号数排出聚集；被「拒绝抽奖」挡下的请求
 * 没有抽奖记录，单独在最后列出，展开能看到当时每条规则的取值。后端八组聚合并行查，右上角是这一次的用时。
 */
export function LotteryRiskPanel({
  appKey,
  activityId,
  onActivityChange,
  onOpenDraws
}: {
  appKey: string;
  activityId?: number;
  onActivityChange: (activityId: number) => void;
  onOpenDraws: (jump: RiskDrawJump) => void;
}) {
  const [days, setDays] = useState<(typeof RANGES)[number]["value"]>("7");
  const activitiesQuery = useLotteryActivitiesQuery(appKey);
  const catalogQuery = useLotteryCatalogQuery(appKey);
  const activities = useMemo(() => activitiesQuery.data ?? [], [activitiesQuery.data]);
  const currentId = activityId && activities.some((item) => item.id === activityId) ? activityId : activities[0]?.id;
  const query = useLotteryRiskQuery(appKey, currentId, Number(days));
  const overview = query.data;
  const catalog = catalogQuery.data;

  if (activitiesQuery.isLoading) return <Skeleton className="h-64 w-full rounded-2xl" />;
  if (!currentId) {
    return (
      <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">暂无活动</p>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={String(currentId)} onValueChange={(next) => onActivityChange(Number(next))}>
          <SelectTrigger className="h-8 w-60">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {activities.map((activity) => (
              <SelectItem key={activity.id} value={String(activity.id)}>
                {activity.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Segmented size="sm" value={days} onChange={setDays} options={RANGES.map((item) => ({ ...item }))} />
        <div className="ml-auto flex items-center gap-2">
          {overview ? <span className="font-mono text-[11px] text-muted-foreground">并行查询 {overview.tookMs} ms</span> : null}
          <Button variant="outline" size="sm" onClick={() => query.refetch()} disabled={query.isFetching}>
            {query.isFetching ? <Loader2 className="size-3.5 animate-spin" /> : <RotateCcw className="size-3.5" />}
            刷新
          </Button>
        </div>
      </div>

      {query.isLoading || !overview ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full rounded-2xl" />
          <Skeleton className="h-72 w-full rounded-2xl" />
        </div>
      ) : (
        <div className={cn("space-y-4 transition-opacity", query.isFetching && "opacity-70")}>
          <KpiStrip overview={overview} />
          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
            <RulesCard overview={overview} catalog={catalog} onOpen={(risk) => onOpenDraws({ activityId: currentId, risk })} />
            <div className="space-y-4">
              <ActionsCard overview={overview} catalog={catalog} onOpen={() => onOpenDraws({ activityId: currentId, risk: "any" })} />
              <TrendCard overview={overview} />
            </div>
          </div>
          <ClustersCard overview={overview} onOpen={(keyword) => onOpenDraws({ activityId: currentId, keyword })} />
          <EventsCard overview={overview} catalog={catalog} appKey={appKey} />
        </div>
      )}
    </div>
  );
}

// ── 指标 ──

function KpiStrip({ overview }: { overview: LotteryRiskOverview }) {
  const share = overview.draws ? overview.flagged / overview.draws : 0;
  const tiles = [
    { icon: Activity, label: "抽奖", value: formatCount(overview.draws), sub: `${formatCount(overview.users)} 人`, tone: "text-foreground" },
    {
      icon: ShieldAlert,
      label: "命中风控",
      value: formatCount(overview.flagged),
      sub: `占 ${(share * 100).toFixed(share < 0.01 && share > 0 ? 2 : 1)}%，${formatCount(overview.flaggedUsers)} 人`,
      tone: "text-orange-600 dark:text-orange-400"
    },
    { icon: Ban, label: "拒绝抽奖", value: formatCount(overview.denied), sub: "未扣次数，见下方", tone: "text-rose-600 dark:text-rose-400" },
    {
      icon: ShieldCheck,
      label: "生效规则",
      value: String(overview.rules.filter((rule) => !rule.disabled).length),
      sub: overview.rules.length ? "按设置顺序评估" : "未开启风控",
      tone: "text-emerald-600 dark:text-emerald-400"
    }
  ];
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {tiles.map((tile) => (
        <div key={tile.label} className="rounded-2xl border border-border bg-card p-4" style={{ boxShadow: "var(--shadow-soft)" }}>
          <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <tile.icon className="size-3.5" />
            {tile.label}
          </div>
          <p className={cn("mt-1.5 font-mono text-2xl font-semibold tabular-nums", tile.tone)}>{tile.value}</p>
          <p className="mt-0.5 text-[11px] text-muted-foreground">{tile.sub}</p>
        </div>
      ))}
    </div>
  );
}

// ── 按规则 ──

function RulesCard({
  overview,
  catalog,
  onOpen
}: {
  overview: LotteryRiskOverview;
  catalog?: LotteryCatalog | null;
  onOpen: (signal: string) => void;
}) {
  const counts = new Map(overview.signals.map((item) => [item.key, item]));
  const ruleSignals = new Set(overview.rules.map((rule) => rule.signal));
  // 不在当前规则里、却出现在记录里的信号（规则后来删了，或迁移前的记录）
  const orphans = overview.signals.filter((item) => !ruleSignals.has(item.key));
  const max = Math.max(1, ...overview.signals.map((item) => item.draws + item.denied));

  return (
    <SectionCard icon={<Gauge className="size-4" />} title="按规则" description="每条规则命中了多少次，点一下查看那些记录">
      {overview.rules.length === 0 && orphans.length === 0 ? (
        <p className="py-6 text-center text-xs text-muted-foreground">这个活动没有开启风控规则，在活动编辑的出奖策略里开启</p>
      ) : (
        <div className="space-y-1">
          {overview.rules.map((rule, index) => {
            const count = counts.get(rule.signal);
            const total = (count?.draws ?? 0) + (count?.denied ?? 0);
            const visual = riskActionVisual(rule.action);
            const detail = riskActionDetail(rule);
            return (
              <button
                key={`${rule.signal}-${index}`}
                type="button"
                onClick={() => onOpen(rule.signal)}
                disabled={!count?.draws}
                className={cn(
                  "group grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1.5 rounded-lg px-2.5 py-2 text-left transition-colors",
                  count?.draws ? "hover:bg-muted/60" : "cursor-default",
                  rule.disabled && "opacity-50"
                )}
              >
                <div className="min-w-0">
                  <p className="truncate text-xs font-medium">{rule.name || riskSignalOf(rule.signal, catalog).label}</p>
                  <p className="truncate text-[10px] text-muted-foreground">{riskConditionText(rule, catalog)}</p>
                </div>
                <RiskActionChip action={rule.action} catalog={catalog} suffix={detail ? ` ${detail}` : null} />
                <div className="col-span-2 flex items-center gap-2">
                  <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                    <span className={cn("block h-full rounded-full", visual.bar)} style={{ width: `${(total / max) * 100}%` }} />
                  </span>
                  <span className="w-28 shrink-0 text-right font-mono text-[11px] tabular-nums">
                    {formatCount(count?.draws)}
                    {count?.denied ? <span className="text-rose-600 dark:text-rose-400"> +{formatCount(count.denied)} 拒</span> : null}
                  </span>
                  <ArrowUpRight
                    className={cn("size-3.5 shrink-0 text-muted-foreground transition-opacity", count?.draws ? "opacity-0 group-hover:opacity-100" : "opacity-0")}
                  />
                </div>
              </button>
            );
          })}
          {orphans.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => onOpen(item.key)}
              disabled={!item.draws}
              className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-lg px-2.5 py-2 text-left hover:bg-muted/60"
            >
              <div className="min-w-0">
                <p className="truncate text-xs text-muted-foreground">{riskSignalOf(item.key, catalog).label}</p>
                <p className="text-[10px] text-muted-foreground">不在当前规则里（规则已删除或旧记录）</p>
              </div>
              <span className="font-mono text-[11px] tabular-nums">{formatCount(item.draws + item.denied)}</span>
            </button>
          ))}
        </div>
      )}
    </SectionCard>
  );
}

// ── 按处置 ──

function ActionsCard({
  overview,
  catalog,
  onOpen
}: {
  overview: LotteryRiskOverview;
  catalog?: LotteryCatalog | null;
  onOpen: () => void;
}) {
  const counts = new Map(overview.actions.map((item) => [item.key, item.draws + item.denied]));
  const total = [...counts.values()].reduce((sum, value) => sum + value, 0);
  const specs = riskActions(catalog);
  return (
    <SectionCard
      icon={<ShieldAlert className="size-4" />}
      title="按处置"
      description="一次抽奖命中多条时按最重的一条归类"
      aside={
        overview.flagged ? (
          <Button size="sm" variant="ghost" className="h-7" onClick={onOpen}>
            全部记录 <ArrowUpRight className="size-3.5" />
          </Button>
        ) : null
      }
    >
      {total === 0 ? (
        <p className="py-3 text-center text-xs text-muted-foreground">这段时间没有命中</p>
      ) : (
        <div className="space-y-3">
          <div className="flex h-2.5 overflow-hidden rounded-full bg-muted">
            {specs.map((spec) => {
              const value = counts.get(spec.key) ?? 0;
              return value ? (
                <span key={spec.key} className={riskActionVisual(spec.key).solid} style={{ width: `${(value / total) * 100}%` }} title={`${spec.label} ${value}`} />
              ) : null;
            })}
          </div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 sm:grid-cols-3">
            {specs.map((spec) => (
              <div key={spec.key} className="flex items-center gap-1.5 text-[11px]">
                <span className={cn("size-2 rounded-sm", riskActionVisual(spec.key).solid)} />
                <span className="text-muted-foreground">{spec.label}</span>
                <span className="ml-auto font-mono tabular-nums">{formatCount(counts.get(spec.key) ?? 0)}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </SectionCard>
  );
}

// ── 趋势 ──

function TrendCard({ overview }: { overview: LotteryRiskOverview }) {
  const max = Math.max(1, ...overview.trend.map((day) => day.draws + day.denied));
  return (
    <SectionCard icon={<Activity className="size-4" />} title="按天" description="灰色为正常抽奖，橙色为命中风控，红色为拒绝">
      {overview.trend.length === 0 ? (
        <p className="py-3 text-center text-xs text-muted-foreground">这段时间没有抽奖</p>
      ) : (
        <div className="flex h-32 items-end gap-1">
          {overview.trend.map((day) => {
            const clean = Math.max(0, day.draws - day.flagged);
            return (
              <div
                key={day.date}
                className="group flex h-full min-w-0 flex-1 flex-col justify-end"
                title={`${day.date}：抽奖 ${day.draws}，命中 ${day.flagged}，拒绝 ${day.denied}`}
              >
                <div className="flex flex-col-reverse overflow-hidden rounded-t-md">
                  <span className="bg-muted-foreground/25" style={{ height: `${(clean / max) * 112}px` }} />
                  <span className="bg-orange-400" style={{ height: `${(day.flagged / max) * 112}px` }} />
                  <span className="bg-rose-500" style={{ height: `${(day.denied / max) * 112}px` }} />
                </div>
                <span className="mt-1 truncate text-center font-mono text-[9px] text-muted-foreground">{day.date.slice(5)}</span>
              </div>
            );
          })}
        </div>
      )}
    </SectionCard>
  );
}

// ── 聚集 ──

const CLUSTER_TABS = [
  { value: "devices", label: "设备", icon: Smartphone },
  { value: "ips", label: "IP", icon: Network },
  { value: "subnets", label: "网段", icon: Users }
] as const;

function ClustersCard({ overview, onOpen }: { overview: LotteryRiskOverview; onOpen: (keyword: string) => void }) {
  const [tab, setTab] = useState<(typeof CLUSTER_TABS)[number]["value"]>("devices");
  const items: LotteryRiskCluster[] = overview[tab];
  const keywordOf = (key: string) => (tab === "devices" ? `device:${key}` : tab === "ips" ? `ip:${key}` : undefined);
  return (
    <SectionCard
      icon={<Users className="size-4" />}
      title="多账号聚集"
      description="同一设备、IP 或网段上抽过奖的账号数（2 个以上），按账号数排序"
      aside={
        <Segmented
          size="sm"
          value={tab}
          onChange={setTab}
          options={CLUSTER_TABS.map((item) => ({
            value: item.value,
            label: `${item.label} ${overview[item.value].length}`,
            icon: <item.icon />
          }))}
        />
      }
    >
      {items.length === 0 ? (
        <p className="py-4 text-center text-xs text-muted-foreground">没有多账号聚集</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border text-[10px] text-muted-foreground">
                <th className="py-1.5 text-left font-medium">{CLUSTER_TABS.find((item) => item.value === tab)?.label}</th>
                <th className="py-1.5 text-right font-medium">账号</th>
                <th className="py-1.5 text-right font-medium">抽奖</th>
                <th className="py-1.5 text-right font-medium">命中风控</th>
                <th className="py-1.5 text-right font-medium">最近</th>
                <th className="w-0" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((item) => {
                const keyword = keywordOf(item.key);
                const heat = Math.min(1, item.accounts / Math.max(2, items[0]?.accounts ?? 2));
                return (
                  <tr key={item.key}>
                    <td className="max-w-72 py-2">
                      <span className="block truncate font-mono text-[11px]" title={item.key}>
                        {item.key}
                      </span>
                    </td>
                    <td className="py-2 text-right">
                      <span
                        className="inline-flex min-w-8 justify-center rounded-md px-1.5 py-0.5 font-mono text-[11px] font-semibold tabular-nums text-orange-700 dark:text-orange-300"
                        style={{ backgroundColor: `color-mix(in oklab, rgb(249 115 22) ${Math.round(8 + heat * 22)}%, transparent)` }}
                      >
                        {item.accounts}
                      </span>
                    </td>
                    <td className="py-2 text-right font-mono tabular-nums">{formatCount(item.draws)}</td>
                    <td className="py-2 text-right font-mono tabular-nums">
                      {item.flagged ? <span className="text-orange-600 dark:text-orange-400">{formatCount(item.flagged)}</span> : "0"}
                    </td>
                    <td className="whitespace-nowrap py-2 text-right text-[11px] text-muted-foreground">{formatStoreDate(item.lastAt)}</td>
                    <td className="py-2 pl-2">
                      {keyword ? (
                        <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px]" onClick={() => onOpen(keyword)}>
                          记录 <ArrowUpRight className="size-3" />
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </SectionCard>
  );
}

// ── 拒绝事件 ──

function EventsCard({ overview, catalog, appKey }: { overview: LotteryRiskOverview; catalog?: LotteryCatalog | null; appKey: string }) {
  return (
    <SectionCard
      icon={<Ban className="size-4" />}
      title="被拒绝的请求"
      description="拒绝抽奖时事务已回滚、没有抽奖记录，只在这里留痕（最近 40 条）"
    >
      {overview.events.length === 0 ? (
        <p className="py-4 text-center text-xs text-muted-foreground">这段时间没有被拒绝的请求</p>
      ) : (
        <div className="divide-y divide-border">
          {overview.events.map((event) => (
            <EventRow key={event.id} event={event} catalog={catalog} appKey={appKey} />
          ))}
        </div>
      )}
    </SectionCard>
  );
}

function EventRow({ event, catalog, appKey }: { event: LotteryRiskEvent; catalog?: LotteryCatalog | null; appKey: string }) {
  const [open, setOpen] = useState(false);
  const hits = event.report?.checks.filter((check) => check.hit) ?? [];
  return (
    <div className="py-2">
      <button type="button" onClick={() => setOpen((value) => !value)} className="flex w-full items-center gap-3 text-left">
        <RiskActionChip action={event.action} catalog={catalog} />
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs">
            <Link
              href={`/app-users/${encodeURIComponent(appKey)}/${event.userId}?tab=assets`}
              onClick={(e) => e.stopPropagation()}
              className="font-medium hover:underline"
            >
              {event.account || `#${event.userId}`}
            </Link>
            {event.nickname ? <span className="ml-1.5 text-muted-foreground">{event.nickname}</span> : null}
          </p>
          <p className="truncate text-[10px] text-muted-foreground">
            {event.rules.map((key) => riskSignalOf(key, catalog).label).join("、") || "—"}
          </p>
        </div>
        <div className="hidden shrink-0 text-right font-mono text-[10px] text-muted-foreground sm:block">
          <p>{event.clientIp || "无 IP"}</p>
          <p className="max-w-40 truncate">{event.deviceId || "无设备号"}</p>
        </div>
        <span className="shrink-0 whitespace-nowrap text-[11px] text-muted-foreground">{formatStoreDate(event.createdAt)}</span>
        <ChevronDown className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")} />
      </button>
      {open && event.report ? (
        <div className="mt-2 space-y-1 rounded-lg bg-muted/40 p-2.5">
          {event.report.checks.map((check, index) => {
            const spec = riskSignalOf(check.signal, catalog);
            return (
              <div key={`${check.signal}-${index}`} className="flex items-center gap-2 text-[11px]">
                <span className={cn("size-1.5 shrink-0 rounded-full", check.hit ? riskActionVisual(check.action).solid : "bg-emerald-500")} />
                <span className={cn("min-w-0 flex-1 truncate", !check.hit && "text-muted-foreground")}>
                  {check.name || spec.label}：{riskConditionText(check, catalog)}
                </span>
                <span className="font-mono tabular-nums">{check.missing ? "取不到" : riskValueText(check.value, spec)}</span>
              </div>
            );
          })}
          {hits.length === 0 ? <p className="text-[11px] text-muted-foreground">没有记录到命中的规则</p> : null}
        </div>
      ) : null}
    </div>
  );
}
