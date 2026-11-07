"use client";

import NumberFlow from "@number-flow/react";
import { Radio } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import type { LatencyView, RuleStats } from "@/lib/api/traffic";
import { formatRelative } from "@/lib/datetime";
import { useTrafficRuleStatsQuery, trafficErrorMessage } from "@/lib/traffic-hooks";
import { cn } from "@/lib/utils";
import { Bar, Figure, Panel, Spark } from "./traffic-kit";
import { KEY_LABEL, SCOPE_LABEL, WARMUP_STATE_LABEL, fmtMs, fmtNum, limitPhrase, rejectedOf } from "./traffic-format";

/** 已发布规则的实时运行态（编辑页右栏） */
export function RuleLiveStats({ ruleId }: { ruleId: string }) {
  const statsQuery = useTrafficRuleStatsQuery(ruleId, 60);
  const stats = statsQuery.data;

  return (
    <Panel
      icon={Radio}
      tone="emerald"
      title="运行态"
      aside={stats ? <Spark values={(stats.series ?? []).map((p) => p.arrivals)} width={72} height={22} className="text-sky-500" /> : null}
    >
      {!stats ? (
        statsQuery.isError ? (
          <p className="text-sm text-muted-foreground">{trafficErrorMessage(statsQuery.error, "读取失败")}</p>
        ) : (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-14 rounded-xl" />
            <Skeleton className="h-20 rounded-xl" />
          </div>
        )
      ) : (
        <StatsBody stats={stats} />
      )}
    </Panel>
  );
}

function StatsBody({ stats }: { stats: RuleStats }) {
  const last = stats.lastSecond;
  const rejected = rejectedOf(last);
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3">
        <div className="min-w-0">
          <div className="text-[11px] text-muted-foreground">到达</div>
          <div className="flex items-baseline gap-1">
            <span className="text-xl leading-7 font-semibold tabular-nums">
              <NumberFlow value={last.arrivals} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
            </span>
            <span className="text-[11px] text-muted-foreground">/秒</span>
          </div>
        </div>
        <div className="min-w-0">
          <div className="text-[11px] text-muted-foreground">拒绝</div>
          <div className="flex items-baseline gap-1">
            <span className={cn("text-xl leading-7 font-semibold tabular-nums", rejected > 0 && "text-rose-600 dark:text-rose-400")}>
              <NumberFlow value={rejected} locales="zh-CN" format={{ notation: "compact", maximumFractionDigits: 1 }} />
            </span>
            <span className="text-[11px] text-muted-foreground">/秒</span>
          </div>
        </div>
        <Figure
          label="在途"
          value={stats.concurrencyLimit > 0 ? `${fmtNum(stats.inflight)} / ${fmtNum(stats.concurrencyLimit)}` : fmtNum(stats.inflight)}
        />
        <Figure
          label="队列"
          value={stats.queueCapacity > 0 ? `${fmtNum(stats.queueDepth)} / ${fmtNum(stats.queueCapacity)}` : "—"}
        />
      </div>

      <LatencyRuler latency={stats.latency} />

      {stats.warmup ? (
        <div className="flex flex-col gap-2 rounded-xl border border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-medium">预热</span>
            <Badge variant={stats.warmup.state === "saturated" ? "warning" : stats.warmup.state === "ramping" ? "info" : "outline"} size="sm">
              {WARMUP_STATE_LABEL[stats.warmup.state]}
            </Badge>
          </div>
          <Bar
            ratio={stats.warmup.max > stats.warmup.baseline ? (stats.warmup.ceiling - stats.warmup.baseline) / (stats.warmup.max - stats.warmup.baseline) : 1}
            className="bg-emerald-500"
          />
          <div className="grid grid-cols-3 gap-2">
            <Figure label="基线" value={fmtNum(stats.warmup.baseline)} />
            <Figure label="当前上限" value={fmtNum(Math.round(stats.warmup.ceiling))} />
            <Figure label="最高" value={fmtNum(stats.warmup.max)} />
          </div>
          {stats.warmup.lastBurstAt ? (
            <div className="text-[11px] text-muted-foreground">最近洪峰 {formatRelative(stats.warmup.lastBurstAt)}</div>
          ) : null}
        </div>
      ) : null}

      {stats.limits?.length ? (
        <div className="flex flex-col gap-2">
          <div className="text-xs font-medium">限速</div>
          {stats.limits.map((limit, index) => (
            <div key={index} className="flex min-w-0 flex-col gap-1.5 rounded-xl border border-border p-3">
              <div className="flex min-w-0 items-center gap-1.5">
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{limitPhrase({ key: limit.key, name: limit.name ?? "", rate: limit.rate, periodMs: limit.periodMs })}</span>
                <Badge variant="outline" size="sm">
                  {SCOPE_LABEL[limit.scope]}
                </Badge>
                {limit.leaseMode === "fallback" ? (
                  <Badge variant="warning" size="sm">
                    降级
                  </Badge>
                ) : null}
              </div>
              <div className="grid grid-cols-3 gap-2">
                <Figure label="本机速率" value={`${fmtNum(Math.round(limit.effectiveRate * 10) / 10)}/秒`} />
                <Figure label="跟踪键" value={fmtNum(limit.trackedKeys)} />
                <Figure
                  label="拒绝"
                  value={fmtNum(limit.rejected)}
                  valueClassName={limit.rejected > 0 ? "text-rose-600 dark:text-rose-400" : undefined}
                />
              </div>
              {limit.overflowed > 0 ? <div className="text-[11px] text-amber-600 dark:text-amber-400">溢出 {fmtNum(limit.overflowed)}</div> : null}
            </div>
          ))}
        </div>
      ) : null}

      {stats.hotKeys?.length ? (
        <div className="flex flex-col gap-1.5">
          <div className="text-xs font-medium">热点键</div>
          {stats.hotKeys.slice(0, 6).map((hot) => (
            <div key={`${hot.kind}:${hot.key}`} className="flex min-w-0 items-center gap-2 text-xs">
              <Badge variant="outline" size="sm" className="shrink-0">
                {KEY_LABEL[hot.kind] ?? hot.kind}
              </Badge>
              <span className="min-w-0 flex-1 truncate font-mono" title={hot.key}>
                {hot.key}
              </span>
              <span className="shrink-0 font-medium text-rose-600 tabular-nums dark:text-rose-400">{fmtNum(hot.count)}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

const MARKS: Array<{ key: keyof LatencyView; label: string; dot: string }> = [
  { key: "p50", label: "P50", dot: "bg-sky-500" },
  { key: "p90", label: "P90", dot: "bg-emerald-500" },
  { key: "p99", label: "P99", dot: "bg-amber-500" },
  { key: "max", label: "最大", dot: "bg-rose-500" }
];

/** 分位标尺：四个分位落在 0 到最大值的同一条刻度上，拖尾一眼可见 */
function LatencyRuler({ latency }: { latency: LatencyView }) {
  const max = Math.max(latency.max, latency.p99, 1);
  return (
    <div className="flex flex-col gap-2 rounded-xl border border-border p-3">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium">响应时间</span>
        <span className="text-[11px] text-muted-foreground tabular-nums">{fmtNum(latency.samples)} 次</span>
      </div>
      <div className="relative h-5">
        <div className="absolute inset-x-0 top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-muted" />
        <div
          className="absolute top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-foreground/15"
          style={{ left: `${(latency.p50 / max) * 100}%`, width: `${Math.max(0, ((latency.p99 - latency.p50) / max) * 100)}%` }}
        />
        {MARKS.map((mark) => (
          <span
            key={mark.key}
            aria-hidden
            className={cn("absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-card transition-[left] duration-500", mark.dot)}
            style={{ left: `${Math.min(100, (Number(latency[mark.key]) / max) * 100)}%` }}
          />
        ))}
      </div>
      <div className="grid grid-cols-4 gap-2">
        {MARKS.map((mark) => (
          <div key={mark.key} className="min-w-0">
            <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
              <span aria-hidden className={cn("size-1.5 rounded-full", mark.dot)} />
              {mark.label}
            </div>
            <div className="truncate text-xs font-medium tabular-nums">{fmtMs(Number(latency[mark.key]))}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
