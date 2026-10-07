"use client";

import { CalendarClock, Download, FileEdit, Gauge, PauseCircle, Rocket, Send, Smartphone } from "lucide-react";
import type { Release, ReleaseOverview as Overview } from "@/lib/api/types";
import { StatTile } from "@/components/content/content-shared";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { ChannelDot, DATA_FONT, UpdateTypeBadge, percent, platformLabel } from "./release-shared";

export function ReleaseOverview({
  overview,
  loading,
  onOpen
}: {
  overview?: Overview;
  loading: boolean;
  onOpen: (release: Release) => void;
}) {
  if (loading || !overview) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-[84px] rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-28 rounded-xl" />
      </div>
    );
  }
  const funnel = overview.last14d;
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile label="下发中" icon={Rocket} tone="positive" value={overview.live} hint={`灰度中 ${overview.rollingOut}`} />
        <StatTile label="待发布" icon={CalendarClock} value={overview.scheduled} hint={`草稿 ${overview.draft}`} />
        <StatTile label="已暂停" icon={PauseCircle} tone={overview.paused ? "default" : "muted"} value={overview.paused} hint={`已撤回 ${overview.revoked}`} />
        <StatTile label="版本总数" icon={FileEdit} value={overview.total} />
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.4fr_1fr]">
        <div className="rounded-xl border bg-card">
          <div className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">各渠道当前版本</div>
          {overview.latest.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">暂无下发中的版本</p>
          ) : (
            <div className="divide-y">
              {overview.latest.map((release) => (
                <button
                  key={release.id}
                  type="button"
                  onClick={() => onOpen(release)}
                  className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors hover:bg-muted/50"
                >
                  <span className="flex w-28 shrink-0 items-center gap-1.5 truncate">
                    <ChannelDot color={release.channel?.color} />
                    {release.channel?.name ?? "不限渠道"}
                  </span>
                  <span className="flex w-20 shrink-0 items-center gap-1 text-xs text-muted-foreground">
                    <Smartphone className="size-3" />
                    {platformLabel(release.platform)}
                  </span>
                  <span className={cn("min-w-0 flex-1 truncate font-medium", DATA_FONT)}>{release.version}</span>
                  {release.rolloutPct < 100 ? (
                    <span className={cn("text-xs text-muted-foreground", DATA_FONT)}>{release.rolloutPct}%</span>
                  ) : null}
                  <UpdateTypeBadge value={release.updateType} />
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="rounded-xl border bg-card">
          <div className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">近 14 天更新漏斗</div>
          <div className="space-y-2.5 px-4 py-3">
            <FunnelBar icon={Send} label="下发" value={funnel.offered} max={funnel.offered} />
            <FunnelBar icon={Download} label="下载" value={funnel.downloaded} max={funnel.offered} rate={percent(funnel.downloaded, funnel.offered)} />
            <FunnelBar icon={Gauge} label="安装" value={funnel.installed} max={funnel.offered} rate={percent(funnel.installed, funnel.offered)} />
            <div className="flex justify-between pt-1 text-xs text-muted-foreground">
              <span>失败 {funnel.failed}</span>
              <span>跳过 {funnel.dismissed}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function FunnelBar({
  icon: Icon,
  label,
  value,
  max,
  rate
}: {
  icon: typeof Send;
  label: string;
  value: number;
  max: number;
  rate?: string;
}) {
  const width = max > 0 ? Math.max(2, Math.round((value / max) * 100)) : 0;
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Icon className="size-3" />
          {label}
        </span>
        <span className={DATA_FONT}>
          {value}
          {rate ? <span className="ml-1.5 text-muted-foreground">{rate}</span> : null}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary" style={{ width: `${width}%` }} />
      </div>
    </div>
  );
}
