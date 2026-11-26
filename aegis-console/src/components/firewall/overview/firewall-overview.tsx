"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  AppWindow,
  ArrowDownRight,
  ArrowUpRight,
  Ban,
  Eye,
  FileText,
  Fingerprint,
  Globe2,
  ListFilter,
  Map as MapIcon,
  Minus,
  Route,
  ShieldHalf,
  Siren
} from "lucide-react";
import { AttackFlightMap } from "@/components/security/attack-flight-map";
import { changeRatio, formatRatio, OverviewCard, Sparkline } from "@/components/overview/overview-kit";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { CountryFlag } from "@/components/ui/country-flag";
import { groupName, groupOfRule, reasonLabel, RuleIdPill } from "@/components/firewall/firewall-kit";
import type { FirewallEventQuery, FirewallStatsV2 } from "@/lib/api/firewall";
import { firewallErrorMessage, useFirewallStatsQuery } from "@/lib/firewall-hooks";
import { useFirewallIntents } from "@/lib/firewall-store";
import { cn } from "@/lib/utils";
import { AttackSurfaceCard } from "./attack-surface-card";
import { FIREWALL_RANGES, granularityOf, rangeStart, type FirewallRangeKey } from "./firewall-range";
import { countryName, formatShare, KpiTile, RankedList, Segmented } from "./overview-parts";
import { SeverityTrendCard } from "./severity-trend-card";

type FirewallTab = "overview" | "events" | "rules" | "waf" | "exclusions" | "access" | "bans" | "sandbox";

/** 阻断评估 / 关联统计这类「结论规则」：几乎每条拦截都有它们，排进命中规则没有信息量 */
const EVALUATION_RULES = new Set([949110, 959100, 980170]);

/**
 * 防火墙总览：一个时间窗下的拦截全貌。
 *
 * 所有排行都只数拦截（action = block，服务端口径）；观察与记录类事件只出现在指标格里。
 * 列表里的每一项都能点：带上筛选（含当前时间窗）跳到事件页，总览只负责「看出哪里不对」，
 * 「具体是哪几条请求」交给事件页。
 */
export function FirewallOverviewTab({ onNavigate }: { onNavigate: (tab: FirewallTab) => void }) {
  const [range, setRange] = useState<FirewallRangeKey>("24h");
  // 窗口锚点每分钟前移一次：实时刷新只管窗口里的新事件，起点也要跟着走，否则开着看一小时就变成两小时
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

  const granularity = granularityOf(range);
  const startTime = rangeStart(range, now);
  const statsQuery = useFirewallStatsQuery({ startTime, granularity }, { live: true });
  const stats = statsQuery.data;

  // 飞线图重绘代价大：起点按 10 分钟取整，不随每分钟的锚点重算
  const mapStart = rangeStart(range, Math.floor(now / 600_000) * 600_000);
  const mapRange = useMemo(() => ({ startTime: mapStart }), [mapStart]);

  const setEventsFilter = useFirewallIntents((state) => state.setEventsFilter);
  const pick = (filter: FirewallEventQuery) => {
    setEventsFilter({ action: "block", startTime, ...filter });
    onNavigate("events");
  };

  const header = (
    <div className="flex flex-wrap items-center justify-end gap-2">
      {statsQuery.isFetching && stats ? <Spinner className="size-3.5 text-muted-foreground" /> : null}
      <Segmented value={range} options={FIREWALL_RANGES} onChange={setRange} ariaLabel="时间范围" />
    </div>
  );

  if (!stats) {
    return (
      <div className="@container flex min-w-0 flex-col gap-4">
        {header}
        {statsQuery.isError ? (
          <OverviewCard icon={AlertTriangle} tone="rose" title="统计读取失败">
            <p className="text-sm text-muted-foreground">{firewallErrorMessage(statsQuery.error, "读取失败")}</p>
          </OverviewCard>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3 @4xl:grid-cols-4">
              {Array.from({ length: 4 }).map((_, index) => (
                <Skeleton key={index} className="h-[118px] rounded-xl" />
              ))}
            </div>
            <div className="grid gap-4 @5xl:grid-cols-[minmax(0,1fr)_360px]">
              <Skeleton className="h-[340px] rounded-xl" />
              <Skeleton className="h-[340px] rounded-xl" />
            </div>
            <Skeleton className="h-[420px] rounded-xl" />
          </>
        )}
      </div>
    );
  }

  return (
    <div className="@container flex min-w-0 flex-col gap-4">
      {header}
      <KpiRow stats={stats} onPick={pick} />
      <div className="grid min-w-0 gap-4 @5xl:grid-cols-[minmax(0,1fr)_360px]">
        <SeverityTrendCard
          stats={stats}
          startMs={Date.parse(startTime)}
          endMs={now}
          granularity={granularity}
          range={range}
          refreshing={statsQuery.isFetching}
        />
        <AttackSurfaceCard stats={stats} onPick={pick} />
      </div>
      <OverviewCard icon={MapIcon} tone="sky" title="攻击来源" description="只画拦截">
        <AttackFlightMap timeRange={mapRange} />
      </OverviewCard>
      <RankedGrid stats={stats} onPick={pick} />
    </div>
  );
}

/* ── 指标 ── */

function KpiRow({ stats, onPick }: { stats: FirewallStatsV2; onPick: (filter: FirewallEventQuery) => void }) {
  const ratio = changeRatio(stats.totalBlocked, stats.previousBlocked);
  const all = stats.totalBlocked + stats.totalObserved + stats.totalLogged;
  const spark = useMemo(() => (stats.timeSeries ?? []).map((point) => point.count), [stats.timeSeries]);

  const trend =
    ratio === null ? (
      <span>上期无拦截</span>
    ) : ratio === 0 ? (
      <span className="inline-flex items-center gap-1">
        <Minus className="size-3" />
        与上期持平
      </span>
    ) : (
      <span className={cn("inline-flex items-center gap-0.5 font-medium", ratio > 0 ? "text-rose-600 dark:text-rose-400" : "text-emerald-600 dark:text-emerald-400")}>
        {ratio > 0 ? <ArrowUpRight className="size-3" /> : <ArrowDownRight className="size-3" />}
        {formatRatio(ratio)}
        <span className="font-normal text-muted-foreground">较上期 {stats.previousBlocked.toLocaleString("zh-CN")}</span>
      </span>
    );

  return (
    <div className="grid min-w-0 grid-cols-2 gap-3 @4xl:grid-cols-4">
      <KpiTile
        icon={Ban}
        tone="rose"
        label="拦截次数"
        value={stats.totalBlocked}
        footer={trend}
        aside={spark.length > 1 ? <Sparkline values={spark} className="hidden text-rose-500 @md:block" /> : null}
        onClick={() => onPick({ action: "block" })}
      />
      <KpiTile
        icon={Eye}
        tone="amber"
        label="观察命中"
        value={stats.totalObserved}
        footer={<span>占全部事件 {formatShare(stats.totalObserved, all)}</span>}
        onClick={() => onPick({ action: "observe" })}
      />
      <KpiTile
        icon={Fingerprint}
        tone="violet"
        label="独立来源 IP"
        value={stats.uniqueIPs}
        footer={
          stats.uniqueIPs > 0 ? <span>平均每个来源 {(stats.totalBlocked / stats.uniqueIPs).toFixed(1)} 次</span> : <span>暂无来源</span>
        }
        onClick={() => onPick({ action: "block" })}
      />
      <KpiTile
        icon={FileText}
        tone="sky"
        label="自定义规则记录"
        value={stats.totalLogged}
        footer={<span>占全部事件 {formatShare(stats.totalLogged, all)}</span>}
        onClick={() => onPick({ action: "log" })}
      />
    </div>
  );
}

/* ── 排行 ── */

function RankedGrid({ stats, onPick }: { stats: FirewallStatsV2; onPick: (filter: FirewallEventQuery) => void }) {
  const total = stats.totalBlocked;
  const [ruleView, setRuleView] = useState<"custom" | "apps">("custom");
  const topRules = useMemo(
    () => (stats.topRules ?? []).map((item) => ({ ...item, id: Number(item.key) })).filter((item) => item.id > 0 && !EVALUATION_RULES.has(item.id)),
    [stats.topRules]
  );

  return (
    <div className="grid min-w-0 gap-4 @2xl:grid-cols-2 @6xl:grid-cols-3">
      <OverviewCard icon={ShieldHalf} tone="violet" title="规则组" description="一条拦截可命中多个组">
        <RankedList
          items={stats.topGroups ?? []}
          total={total}
          barClassName="bg-violet-500/10 dark:bg-violet-500/15"
          emptyIcon={ShieldHalf}
          emptyTitle="暂无 WAF 命中"
          renderLabel={(item) => (
            <>
              <span className="truncate">{groupName(item.key)}</span>
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{item.key}</span>
            </>
          )}
        />
        {topRules.length > 0 ? (
          <div className="mt-3 flex flex-wrap items-center gap-1.5 border-t pt-3">
            <span className="mr-1 text-[11px] text-muted-foreground">主要规则</span>
            {topRules.slice(0, 8).map((rule) => (
              <span key={rule.key} title={`${groupName(groupOfRule(rule.id))} · ${rule.count} 次`} className="inline-flex">
                <RuleIdPill id={rule.id} onClick={() => onPick({ wafRuleId: rule.id })} />
              </span>
            ))}
          </div>
        ) : null}
      </OverviewCard>

      <OverviewCard icon={Fingerprint} tone="rose" title="来源 IP">
        <RankedList
          items={stats.topIPs ?? []}
          total={total}
          emptyIcon={Fingerprint}
          emptyTitle="暂无来源"
          onPick={(item) => onPick({ ip: item.key })}
          renderLabel={(item) => <span className="truncate font-mono">{item.key}</span>}
        />
      </OverviewCard>

      <OverviewCard icon={Globe2} tone="sky" title="国家 / 地区">
        <RankedList
          items={stats.topCountries ?? []}
          total={total}
          barClassName="bg-sky-500/10 dark:bg-sky-500/15"
          emptyIcon={Globe2}
          emptyTitle="暂无归属地"
          onPick={(item) => onPick({ country: item.key })}
          renderLabel={(item) => (
            <>
              <CountryFlag code={item.key} size={14} />
              <span className="truncate">{countryName(item.key)}</span>
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{item.key}</span>
            </>
          )}
        />
      </OverviewCard>

      <OverviewCard icon={Route} tone="amber" title="路径">
        <RankedList
          items={stats.topPaths ?? []}
          total={total}
          barClassName="bg-amber-500/10 dark:bg-amber-500/15"
          emptyIcon={Route}
          emptyTitle="暂无路径"
          onPick={(item) => onPick({ pathPattern: item.key })}
          renderLabel={(item) => (
            <span className="truncate font-mono" title={item.key}>
              {item.key}
            </span>
          )}
        />
      </OverviewCard>

      <OverviewCard icon={Siren} tone="rose" title="拦截原因">
        <RankedList
          items={stats.topReasons ?? []}
          total={total}
          emptyIcon={Siren}
          emptyTitle="暂无拦截"
          onPick={(item) => onPick({ reason: item.key })}
          renderLabel={(item) => (
            <>
              <span className="truncate">{reasonLabel(item.key)}</span>
              <span className="hidden shrink-0 font-mono text-[10px] text-muted-foreground @md:inline">{item.key}</span>
            </>
          )}
        />
      </OverviewCard>

      <OverviewCard
        icon={ruleView === "custom" ? ListFilter : AppWindow}
        tone="emerald"
        title={ruleView === "custom" ? "自定义规则" : "应用"}
        action={
          <Segmented
            value={ruleView}
            onChange={setRuleView}
            ariaLabel="排行维度"
            options={[
              { value: "custom", label: "规则" },
              { value: "apps", label: "应用" }
            ]}
          />
        }
      >
        {ruleView === "custom" ? (
          <RankedList
            items={stats.topCustomRules ?? []}
            total={total}
            barClassName="bg-emerald-500/10 dark:bg-emerald-500/15"
            emptyIcon={ListFilter}
            emptyTitle="暂无自定义规则拦截"
            onPick={(item) => onPick({ keyword: item.key })}
            renderLabel={(item) => <span className="truncate">{item.key}</span>}
          />
        ) : (
          <RankedList
            items={stats.topApps ?? []}
            total={total}
            barClassName="bg-emerald-500/10 dark:bg-emerald-500/15"
            emptyIcon={AppWindow}
            emptyTitle="暂无应用"
            onPick={(item) => onPick({ appKey: item.key })}
            renderLabel={(item) => <span className="truncate font-mono">{item.key}</span>}
          />
        )}
      </OverviewCard>
    </div>
  );
}
