"use client";

import { useState } from "react";
import { BarChart3, ListChecks, MonitorPlay, Settings2 } from "lucide-react";
import { NoAppSelected } from "@/components/apps/app-config-primitives";
import { RewardedAdConfigPanel } from "@/components/apps/rewarded-ad/rewarded-ad-config-panel";
import { RewardedAdOverviewPanel } from "@/components/apps/rewarded-ad/rewarded-ad-overview-panel";
import { RewardedAdViewsPanel } from "@/components/apps/rewarded-ad/rewarded-ad-views-panel";
import { cn } from "@/lib/utils";

/**
 * 激励广告区块。
 *
 * | 视图 | 回答什么 |
 * |---|---|
 * | 概览 | 今天看了多少、发了多少、多少人在看，近两周的走势 |
 * | 记录 | 某一次观看两方各说了什么、最终发没发、为什么没发 |
 * | 配置 | 接哪个平台、怎么验、每个场景发什么、限多少次 |
 *
 * 视图状态不进 URL，理由同卡密区块：`?tab=` 已经被应用区块占用。
 */
const VIEWS = [
  { key: "overview", label: "概览", icon: BarChart3 },
  { key: "views", label: "记录", icon: ListChecks },
  { key: "config", label: "配置", icon: Settings2 }
] as const;

type ViewKey = (typeof VIEWS)[number]["key"];

export function AppRewardedAdPanel({ appKey }: { appKey?: string | null }) {
  const [view, setView] = useState<ViewKey>("overview");

  if (!appKey) {
    return <NoAppSelected icon={<MonitorPlay className="size-5" />} />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-muted/40 p-1">
        {VIEWS.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setView(item.key)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
              view === item.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="size-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {view === "overview" ? <RewardedAdOverviewPanel appKey={appKey} onOpenConfig={() => setView("config")} /> : null}
      {view === "views" ? <RewardedAdViewsPanel appKey={appKey} /> : null}
      {view === "config" ? <RewardedAdConfigPanel appKey={appKey} /> : null}
    </div>
  );
}
