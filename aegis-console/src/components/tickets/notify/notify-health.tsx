"use client";

import { useMemo } from "react";
import { Activity, AlertTriangle, CircleDashed, Gauge, Send } from "lucide-react";
import { Metric, RingGauge } from "@/components/apps/detail/app-detail-kit";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useNotifyCatalogQuery, useNotifyDeliveryStatsQuery } from "@/lib/ticket-hooks";
import { cn } from "@/lib/utils";
import { ChannelMark } from "./notify-kit";

// 顶部投递健康：近 7 天成功率、失败、跳过、平均耗时，以及各渠道类型的发送量。
// 成功率按「成功 /（成功 + 失败）」看更直观，但后端 successPct 已定口径，这里直接用。

export function NotifyHealthStrip() {
  const statsQuery = useNotifyDeliveryStatsQuery(7);
  const catalogQuery = useNotifyCatalogQuery();
  const stats = statsQuery.data;
  const loading = statsQuery.isPending;

  const kindNames = useMemo(
    () => Object.fromEntries((catalogQuery.data?.kinds ?? []).map((item) => [item.kind, item.name])),
    [catalogQuery.data]
  );

  const byKind = useMemo(
    () =>
      Object.entries(stats?.byKind ?? {})
        .filter(([, count]) => count > 0)
        .sort((a, b) => b[1] - a[1])
        .slice(0, 5),
    [stats]
  );
  const kindMax = byKind[0]?.[1] ?? 0;

  const pct = stats && stats.total > 0 ? stats.successPct : null;
  const tone = pct === null ? "muted" : pct >= 95 ? "emerald" : pct >= 80 ? "amber" : "rose";

  return (
    <Card className="grid gap-4 p-4 sm:p-5 lg:grid-cols-[auto_minmax(0,1fr)_minmax(0,18rem)] lg:items-center">
      <div className="flex items-center gap-4">
        {loading ? (
          <Skeleton className="size-20 rounded-full" />
        ) : (
          <RingGauge value={pct} tone={tone} size={80} stroke={7}>
            <span className="text-center leading-none">
              <span className="block text-lg font-semibold tabular-nums tracking-tight">
                {pct === null ? "—" : `${pct.toFixed(pct >= 99.95 ? 0 : 1)}%`}
              </span>
              <span className="mt-1 block text-[10px] text-muted-foreground">成功率</span>
            </span>
          </RingGauge>
        )}
        <div className="space-y-1">
          <p className="text-sm font-semibold text-foreground">近 7 天投递健康</p>
          <p className="max-w-56 text-xs leading-5 text-muted-foreground">
            {loading
              ? "正在统计…"
              : !stats || stats.total === 0
                ? "这段时间没有投递，订阅的事件发生后会在这里统计。"
                : stats.failed > 0
                  ? `有 ${stats.failed} 次投递失败，可在投递记录里按「失败」筛选查看原因。`
                  : "全部投递成功。"}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <Metric label="投递总数" icon={Send} tone="sky" loading={loading} value={stats?.total ?? 0} />
        <Metric
          label="失败"
          icon={AlertTriangle}
          tone="rose"
          loading={loading}
          value={stats?.failed ?? 0}
          emphasis={stats && stats.failed > 0 ? "danger" : undefined}
        />
        <Metric label="已跳过" icon={CircleDashed} tone="muted" loading={loading} value={stats?.skipped ?? 0} hint="静默窗口或优先级未达标" />
        <Metric
          label="平均耗时"
          icon={Gauge}
          tone="violet"
          loading={loading}
          value={
            <>
              {stats?.avgLatencyMs ?? 0}
              <span className="ml-0.5 text-xs font-normal text-muted-foreground">ms</span>
            </>
          }
        />
      </div>

      <div className="min-w-0 space-y-2 rounded-xl border border-border/80 bg-background/60 p-3">
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Activity className="size-3" />
          各渠道发送量
        </div>
        {loading ? (
          <div className="space-y-2">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-4/5" />
            <Skeleton className="h-4 w-3/5" />
          </div>
        ) : byKind.length === 0 ? (
          <p className="py-2 text-xs text-muted-foreground">暂无发送</p>
        ) : (
          <ul className="space-y-1.5">
            {byKind.map(([kind, count]) => (
              <li key={kind} className="flex items-center gap-2 text-xs">
                <ChannelMark kind={kind} size="sm" />
                <span className="w-20 shrink-0 truncate text-foreground">{kindNames[kind] ?? kind}</span>
                <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                  <span
                    className={cn("block h-full rounded-full bg-primary/70 transition-[width] duration-500")}
                    style={{ width: `${kindMax ? (count / kindMax) * 100 : 0}%` }}
                  />
                </span>
                <span className="w-10 shrink-0 text-right tabular-nums text-muted-foreground">{count}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  );
}
