"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, Globe2, MapPinned } from "lucide-react";
import { EmptyState, LoadingState } from "@/components/ui/data-state";
import { Badge } from "@/components/ui/badge";
import { GeoDrillChart } from "@/components/maps/geo-drill-chart";
import { useAdminToken } from "@/lib/admin-hooks";
import { getAdminAppRegions } from "@/lib/api/apps";
import type { RegionStatItem } from "@/lib/api/types";
import { getCountryCodeFromEnglishName } from "@/lib/geo/country-hotspots";
import { loadGeoBoundary, normalizeAdminName, type GeoView } from "@/lib/geo/geo-boundaries";
import {
  buildCityDrillData,
  buildDistrictDrillData,
  buildProvinceDrillData,
  buildWorldDrillData,
  type DrillDatum
} from "@/lib/geo/region-drill";

type GeoHotspotPanelProps = {
  appKey: string;
  formatCount: (value?: number | null) => string;
};

function rankingCaption(view: GeoView) {
  switch (view.level) {
    case "world":
      return "国家热点";
    case "country":
      return "省级热点";
    case "province":
      return "城市热点";
    case "city":
      return "区县热点";
    default:
      return "热点";
  }
}

export function GeoHotspotPanel({ appKey, formatCount }: GeoHotspotPanelProps) {
  const token = useAdminToken();
  const [viewStack, setViewStack] = useState<GeoView[]>([
    { level: "world", regionName: "全球地区分布", mapKey: "world" }
  ]);

  const currentView = viewStack[viewStack.length - 1];

  useEffect(() => {
    setViewStack([{ level: "world", regionName: "全球地区分布", mapKey: "world" }]);
  }, [appKey]);

  const worldQuery = useQuery({
    queryKey: ["admin-app-regions-country", token, appKey],
    queryFn: () => getAdminAppRegions(token as string, appKey, { type: "country", limit: 240 }),
    enabled: Boolean(token && appKey),
    staleTime: 60_000
  });

  const provinceQuery = useQuery({
    queryKey: ["admin-app-regions-province", token, appKey],
    queryFn: () => getAdminAppRegions(token as string, appKey, { type: "province", limit: 100 }),
    enabled: Boolean(token && appKey && currentView.level !== "world"),
    staleTime: 60_000
  });

  const cityQuery = useQuery({
    queryKey: ["admin-app-regions-city", token, appKey],
    queryFn: () => getAdminAppRegions(token as string, appKey, { type: "city", limit: 1200 }),
    enabled: Boolean(token && appKey && (currentView.level === "province" || currentView.level === "city")),
    staleTime: 60_000
  });

  const districtQuery = useQuery({
    queryKey: ["admin-app-regions-district", token, appKey],
    queryFn: () => getAdminAppRegions(token as string, appKey, { type: "district", limit: 4000 }),
    enabled: Boolean(token && appKey && currentView.level === "city"),
    staleTime: 60_000
  });

  const boundaryQuery = useQuery({
    queryKey: ["geo-boundary", currentView.mapKey],
    queryFn: () => loadGeoBoundary(currentView),
    staleTime: Infinity,
    gcTime: Infinity
  });

  const worldItems = worldQuery.data?.items || [];
  const provinceItems = provinceQuery.data?.items || [];
  const cityItems = cityQuery.data?.items || [];
  const districtItems = districtQuery.data?.items || [];

  const { drillData, unknownItems } = useMemo(() => {
    if (currentView.level === "world") {
      const result = buildWorldDrillData(worldItems as RegionStatItem[]);
      return {
        drillData: result.items,
        unknownItems: result.unknownItems
      };
    }
    if (currentView.level === "country") {
      return { drillData: buildProvinceDrillData(provinceItems as RegionStatItem[]), unknownItems: [] };
    }
    if (currentView.level === "province") {
      return {
        drillData: buildCityDrillData(cityItems as RegionStatItem[], currentView.regionName),
        unknownItems: []
      };
    }
    return {
      drillData: buildDistrictDrillData(
        districtItems as RegionStatItem[],
        currentView.parentName || "",
        currentView.regionName
      ),
      unknownItems: []
    };
  }, [cityItems, currentView.level, currentView.parentName, currentView.regionName, districtItems, provinceItems, worldItems]);

  const rankingItems: DrillDatum[] = Array.isArray(drillData) ? drillData.slice(0, 8) : [];
  const topItem = rankingItems[0];

  function handleSelect(payload: { name: string; adcode?: string; code?: string }) {
    if (currentView.level === "world") {
      const code = payload.code || getCountryCodeFromEnglishName(payload.name);
      if (code === "CN") {
        setViewStack((previous) => [
          ...previous,
          {
            level: "country",
            regionName: "中国",
            countryCode: "CN",
            adcode: "100000",
            mapKey: "china"
          }
        ]);
      }
      return;
    }

    if (currentView.level === "country" && payload.adcode) {
      setViewStack((previous) => [
        ...previous,
        {
          level: "province",
          regionName: payload.name,
          adcode: payload.adcode,
          parentName: "中国",
          mapKey: `province-${payload.adcode}`
        }
      ]);
      return;
    }

    if (currentView.level === "province" && payload.adcode) {
      setViewStack((previous) => [
        ...previous,
        {
          level: "city",
          regionName: payload.name,
          adcode: payload.adcode,
          parentName: currentView.regionName,
          mapKey: `city-${payload.adcode}`
        }
      ]);
    }
  }

  if (worldQuery.isLoading || boundaryQuery.isLoading) {
    return <LoadingState title="正在加载地区分布" description="正在读取地理边界与统计数据。" />;
  }

  if (worldQuery.isError || boundaryQuery.isError || !boundaryQuery.data) {
    return <EmptyState title="地区地图不可用" description="当前未能加载地理边界数据。" />;
  }

  return (
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1.55fr)_360px]">
      <section className="space-y-4">
        <div className="flex flex-col gap-4 rounded-[28px] border border-border bg-card p-5">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                {viewStack.length > 1 ? (
                  <button
                    type="button"
                    onClick={() => setViewStack((previous) => previous.slice(0, -1))}
                    className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition hover:bg-muted"
                  >
                    <ChevronLeft className="size-3.5" />
                    返回上级
                  </button>
                ) : null}
                {viewStack.map((item, index) => (
                  <div
                    key={`${item.mapKey}-${index}`}
                    className="inline-flex items-center gap-1.5 rounded-full border border-border bg-muted px-3 py-1.5 text-xs text-muted-foreground"
                  >
                    {index === 0 ? <Globe2 className="size-3.5" /> : <MapPinned className="size-3.5" />}
                    <span>{item.regionName}</span>
                  </div>
                ))}
              </div>
              <div className="space-y-1">
                <h3 className="text-xl font-semibold tracking-tight text-foreground">{rankingCaption(currentView)}</h3>
                <p className="text-sm text-muted-foreground">点击地图可继续查看下一级分布。</p>
              </div>
            </div>
            <div className="grid min-w-[220px] gap-3 sm:grid-cols-2 xl:grid-cols-1">
              <div className="rounded-[22px] border border-border bg-muted px-4 py-3">
                <div className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">当前热点</div>
                <div className="mt-3 flex items-end justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-base font-semibold text-foreground">{topItem?.label || "暂无数据"}</div>
                    <div className="mt-1 text-xs text-muted-foreground">{topItem ? `占比 ${topItem.share}%` : "等待统计"}</div>
                  </div>
                  <div className="shrink-0 text-right font-data text-2xl font-semibold text-foreground">
                    {formatCount(topItem?.value)}
                  </div>
                </div>
              </div>
              <div className="rounded-[22px] border border-border bg-muted px-4 py-3">
                <div className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">覆盖层级</div>
                <div className="mt-3 flex items-end justify-between gap-3">
                  <div className="text-base font-semibold text-foreground">{currentView.regionName}</div>
                  <div className="text-right text-xs text-muted-foreground">{rankingItems.length} 项</div>
                </div>
              </div>
            </div>
          </div>

          <GeoDrillChart
            view={currentView}
            geoJson={boundaryQuery.data}
            data={Array.isArray(drillData) ? drillData : []}
            formatCount={formatCount}
            onSelect={handleSelect}
          />
        </div>
      </section>

      <aside className="space-y-4">
        <div className="rounded-[28px] border border-border bg-card p-5">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">排行</div>
              <div className="mt-1 text-lg font-semibold text-foreground">{rankingCaption(currentView)}</div>
            </div>
            <Badge variant="outline">{rankingItems.length}</Badge>
          </div>
        </div>

        {rankingItems.length === 0 ? (
          <div className="rounded-[28px] border border-dashed border-border bg-muted px-5 py-6 text-sm text-muted-foreground">
            当前层级暂无热点数据。
          </div>
        ) : (
          rankingItems.map((item, index) => (
            <div
              key={`${item.name}-${index}`}
              className="rounded-[24px] border border-border bg-card px-4 py-4"
            >
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="inline-flex size-7 items-center justify-center rounded-full bg-primary/8 font-data text-xs font-semibold text-primary">
                      {index + 1}
                    </span>
                    <div className="truncate text-sm font-semibold text-foreground">{item.label}</div>
                  </div>
                  <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted/70">
                    <div
                      className="h-full rounded-full bg-[linear-gradient(90deg,#71717a,#a1a1aa)] dark:bg-[linear-gradient(90deg,#52525b,#a1a1aa)]"
                      style={{ width: `${Math.max(item.share, 4)}%` }}
                    />
                  </div>
                </div>
                <div className="shrink-0 text-right">
                  <div className="font-data text-lg font-semibold text-foreground">{formatCount(item.value)}</div>
                  <div className="mt-1 text-xs text-muted-foreground">{item.share}%</div>
                </div>
              </div>
            </div>
          ))
        )}

        {unknownItems.length > 0
          ? unknownItems.slice(0, 2).map((item) => (
              <div
                key={item.region}
                className="rounded-[24px] border border-dashed border-border bg-muted px-4 py-4"
              >
                <div className="flex items-center justify-between gap-4">
                  <div className="min-w-0 text-sm text-muted-foreground">{item.region}</div>
                  <div className="text-right">
                    <div className="font-data text-sm font-semibold text-foreground">{formatCount(item.count)}</div>
                    <div className="text-xs text-muted-foreground">{item.share}%</div>
                  </div>
                </div>
              </div>
            ))
          : null}
      </aside>
    </div>
  );
}
