"use client";

// 地理风控聚合面板：热力图 / 围栏管理 / 轨迹回放
//
// MapLibre GL 体积较大且强依赖 window，三个子面板全部走
// next/dynamic + ssr:false 懒加载，仅在用户切到本 Tab 时才拉取。

import { useState } from "react";
import dynamic from "next/dynamic";
import { Skeleton } from "@/components/ui/skeleton";

function MapPanelSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-9 w-48 rounded-lg" />
        <Skeleton className="h-8 w-64 rounded-lg" />
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_300px]">
        <Skeleton className="h-[480px] rounded-xl" />
        <Skeleton className="h-[480px] rounded-xl" />
      </div>
    </div>
  );
}

const GeoHeatmapPanel = dynamic(
  () => import("./geo-heatmap-panel").then((m) => m.GeoHeatmapPanel),
  { ssr: false, loading: () => <MapPanelSkeleton /> }
);
const GeoFencePanel = dynamic(
  () => import("./geo-fence-panel").then((m) => m.GeoFencePanel),
  { ssr: false, loading: () => <MapPanelSkeleton /> }
);
const GeoTrailPanel = dynamic(
  () => import("./geo-trail-panel").then((m) => m.GeoTrailPanel),
  { ssr: false, loading: () => <MapPanelSkeleton /> }
);

type GeoView = "heatmap" | "fences" | "trail";

const VIEWS: Array<{ key: GeoView; label: string }> = [
  { key: "heatmap", label: "热力分析" },
  { key: "fences", label: "围栏管理" },
  { key: "trail", label: "轨迹回放" }
];

export function GeoIntelPanel() {
  const [view, setView] = useState<GeoView>("heatmap");

  return (
    <div className="space-y-5">
      {/* 子视图切换（与防火墙 Tab 的子视图风格一致） */}
      <div className="flex w-fit items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
        {VIEWS.map((v) => (
          <button
            key={v.key}
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
              view === v.key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
            onClick={() => setView(v.key)}
          >
            {v.label}
          </button>
        ))}
      </div>

      {view === "heatmap" ? <GeoHeatmapPanel /> : view === "fences" ? <GeoFencePanel /> : <GeoTrailPanel />}
    </div>
  );
}
