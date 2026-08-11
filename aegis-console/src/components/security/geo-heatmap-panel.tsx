"use client";

// 地理热力分析 —— 防火墙拦截 / 登录事件的空间分布
//
// 数据源：geo_stats_hourly 预聚合表（geohash-5 网格，约 5km）。
// 渲染：低缩放用 heatmap 层表现密度，放大后过渡为按事件量定径的圆点层；
//       可叠加近期攻击源 DBSCAN 聚类（仅「拦截」类别）。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
// maplibre-gl v6 起不再提供 default export，改用命名空间导入
import * as maplibregl from "maplibre-gl";
import type { Map as MapLibreMap, GeoJSONSource, ExpressionSpecification } from "maplibre-gl";
import { useTheme } from "next-themes";
import { Crosshair, Flame, Loader2, LogIn, RefreshCw, X } from "lucide-react";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { MapLibreMap as BaseMap } from "@/components/maps/maplibre-map";
import { useAdminToken } from "@/lib/admin-hooks";
import { getGeoClusters, getGeoHeatmap, type GeoHeatmapCell, type GeoStatsKind } from "@/lib/api/geo";
import { CountryFlag } from "@/components/ui/country-flag";
import { coordsBounds, flagImgHtml, fmtCount } from "@/lib/geo/geo-map-shared";

// ──────────────────────────────────────
// 常量与小工具
// ──────────────────────────────────────

const RANGE_OPTIONS = [
  { hours: 24, label: "近 24 小时" },
  { hours: 72, label: "近 3 天" },
  { hours: 168, label: "近 7 天" },
  { hours: 720, label: "近 30 天" }
] as const;

const CELL_LIMIT = 2000;

const KIND_META: Record<GeoStatsKind, { label: string; icon: typeof Flame; pointColor: string }> = {
  block: { label: "防火墙拦截", icon: Flame, pointColor: "#ef4444" },
  login: { label: "登录事件", icon: LogIn, pointColor: "#6366f1" }
};

function heatRamp(kind: GeoStatsKind): ExpressionSpecification {
  if (kind === "block") {
    return [
      "interpolate",
      ["linear"],
      ["heatmap-density"],
      0, "rgba(0,0,0,0)",
      0.2, "rgba(251,191,36,0.4)",
      0.45, "#f59e0b",
      0.7, "#f97316",
      1, "#dc2626"
    ];
  }
  return [
    "interpolate",
    ["linear"],
    ["heatmap-density"],
    0, "rgba(0,0,0,0)",
    0.2, "rgba(56,189,248,0.35)",
    0.45, "#38bdf8",
    0.7, "#6366f1",
    1, "#8b5cf6"
  ];
}

function heatWeight(maxCount: number): ExpressionSpecification {
  const max = Math.max(2, maxCount);
  return ["interpolate", ["linear"], ["get", "count"], 1, 0.15, max, 1];
}

function esc(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

type PointFeature = {
  type: "Feature";
  geometry: { type: "Point"; coordinates: [number, number] };
  properties: Record<string, string | number>;
};

function featureCollection(features: PointFeature[]) {
  return { type: "FeatureCollection" as const, features };
}

function cellFeatures(cells: GeoHeatmapCell[]): PointFeature[] {
  return cells
    .filter((c) => Number.isFinite(c.lat) && Number.isFinite(c.lng))
    .map((c) => ({
      type: "Feature" as const,
      geometry: { type: "Point" as const, coordinates: [c.lng, c.lat] as [number, number] },
      properties: {
        count: c.count,
        city: c.city || "",
        countryCode: c.countryCode || ""
      }
    }));
}

// ──────────────────────────────────────
// 面板
// ──────────────────────────────────────

export function GeoHeatmapPanel() {
  const token = useAdminToken();
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";

  const [kind, setKind] = useState<GeoStatsKind>("block");
  const [hours, setHours] = useState<number>(168);
  const [country, setCountry] = useState("");
  const [showClusters, setShowClusters] = useState(false);

  const start = useMemo(() => new Date(Date.now() - hours * 3_600_000).toISOString(), [hours]);

  // 国家筛选为空时，两份查询 key 相同 → React Query 自动去重为一次请求
  const rankQuery = useQuery({
    queryKey: ["geo-heatmap", token, kind, hours, ""],
    queryFn: () => getGeoHeatmap(token as string, { kind, start, limit: CELL_LIMIT }),
    enabled: Boolean(token),
    staleTime: 30_000
  });
  const cellQuery = useQuery({
    queryKey: ["geo-heatmap", token, kind, hours, country],
    queryFn: () =>
      getGeoHeatmap(token as string, { kind, start, country: country || undefined, limit: CELL_LIMIT }),
    enabled: Boolean(token),
    staleTime: 30_000
  });

  const clusterHours = Math.min(hours, 168);
  const clusterQuery = useQuery({
    queryKey: ["geo-clusters", token, clusterHours],
    queryFn: () => getGeoClusters(token as string, { hours: clusterHours, eps: 0.5, minPoints: 10, limit: 20 }),
    enabled: Boolean(token) && showClusters && kind === "block",
    staleTime: 30_000
  });

  const cells = useMemo(() => cellQuery.data?.cells ?? [], [cellQuery.data]);
  const countries = useMemo(() => rankQuery.data?.countries ?? [], [rankQuery.data]);
  const total = cellQuery.data?.total ?? 0;
  const maxCount = useMemo(() => cells.reduce((m, c) => Math.max(m, c.count), 0), [cells]);
  const maxCountryCount = countries.length > 0 ? countries[0].count : 0;
  const clusters = kind === "block" && showClusters ? (clusterQuery.data ?? []) : [];

  const regionNames = useMemo(() => {
    try {
      return new Intl.DisplayNames(["zh-CN"], { type: "region" });
    } catch {
      return null;
    }
  }, []);
  const countryName = useCallback(
    (code: string) => {
      if (!code) return "未知地区";
      try {
        return regionNames?.of(code) ?? code;
      } catch {
        return code;
      }
    },
    [regionNames]
  );

  // ── 地图图层 ──
  const [map, setMap] = useState<MapLibreMap | null>(null);
  const fitPendingRef = useRef(true);

  const handleMapReady = useCallback((m: MapLibreMap) => {
    m.addSource("heat-cells", { type: "geojson", data: featureCollection([]) });
    m.addSource("heat-clusters", { type: "geojson", data: featureCollection([]) });

    m.addLayer({
      id: "heat-layer",
      type: "heatmap",
      source: "heat-cells",
      paint: {
        "heatmap-weight": heatWeight(10),
        "heatmap-color": heatRamp("block"),
        "heatmap-intensity": ["interpolate", ["linear"], ["zoom"], 0, 0.8, 9, 2.2],
        "heatmap-radius": ["interpolate", ["linear"], ["zoom"], 0, 14, 5, 28, 9, 44],
        "heatmap-opacity": ["interpolate", ["linear"], ["zoom"], 5.5, 0.85, 7.5, 0.4]
      }
    });

    m.addLayer({
      id: "heat-points",
      type: "circle",
      source: "heat-cells",
      minzoom: 4.5,
      paint: {
        "circle-color": KIND_META.block.pointColor,
        "circle-opacity": ["interpolate", ["linear"], ["zoom"], 4.5, 0, 6, 0.78],
        "circle-stroke-width": 1,
        "circle-stroke-color": "#ffffff",
        "circle-stroke-opacity": ["interpolate", ["linear"], ["zoom"], 4.5, 0, 6, 0.6],
        "circle-radius": ["interpolate", ["linear"], ["sqrt", ["get", "count"]], 1, 3, 100, 22]
      }
    });

    m.addLayer({
      id: "cluster-circles",
      type: "circle",
      source: "heat-clusters",
      paint: {
        "circle-color": "rgba(239,68,68,0.16)",
        "circle-stroke-width": 1.6,
        "circle-stroke-color": "#ef4444",
        "circle-radius": ["interpolate", ["linear"], ["sqrt", ["get", "hits"]], 1, 8, 100, 38]
      }
    });

    const popup = new maplibregl.Popup({ closeButton: false, offset: 10, maxWidth: "260px" });

    m.on("click", "heat-points", (e) => {
      const f = e.features?.[0];
      if (!f) return;
      const p = f.properties as { count?: number; city?: string; countryCode?: string };
      const code = String(p.countryCode ?? "");
      const city = String(p.city ?? "");
      popup
        .setLngLat(e.lngLat)
        .setHTML(
          `<div style="padding:10px 12px;font-size:12px;line-height:1.6">
             <div style="font-weight:600">${flagImgHtml(code)}${esc(city || code || "未知位置")}</div>
             <div style="opacity:.72">事件数 <b>${Number(p.count ?? 0).toLocaleString()}</b> · 网格约 5km</div>
           </div>`
        )
        .addTo(m);
    });

    m.on("click", "cluster-circles", (e) => {
      const f = e.features?.[0];
      if (!f) return;
      const p = f.properties as { hits?: number; uniqueIps?: number; topReason?: string };
      popup
        .setLngLat(e.lngLat)
        .setHTML(
          `<div style="padding:10px 12px;font-size:12px;line-height:1.6">
             <div style="font-weight:600">攻击聚类</div>
             <div style="opacity:.72">命中 <b>${Number(p.hits ?? 0).toLocaleString()}</b> · 独立 IP <b>${Number(
               p.uniqueIps ?? 0
             ).toLocaleString()}</b></div>
             ${p.topReason ? `<div style="opacity:.72">主因：${esc(String(p.topReason))}</div>` : ""}
           </div>`
        )
        .addTo(m);
    });

    for (const layer of ["heat-points", "cluster-circles"]) {
      m.on("mouseenter", layer, () => {
        m.getCanvas().style.cursor = "pointer";
      });
      m.on("mouseleave", layer, () => {
        m.getCanvas().style.cursor = "";
      });
    }

    setMap(m);
  }, []);

  // 参数变化 → 下一次数据到达后自动取景
  useEffect(() => {
    fitPendingRef.current = true;
  }, [kind, hours, country]);

  // 热力数据写入
  useEffect(() => {
    if (!map) return;
    const source = map.getSource("heat-cells") as GeoJSONSource | undefined;
    if (!source) return;
    source.setData(featureCollection(cellFeatures(cells)));
    map.setPaintProperty("heat-layer", "heatmap-weight", heatWeight(maxCount));

    if (fitPendingRef.current && cells.length > 0) {
      const bounds = coordsBounds(cells.map((c) => [c.lng, c.lat] as [number, number]));
      if (bounds) {
        map.fitBounds(bounds, { padding: 56, maxZoom: 6, duration: 650 });
      }
      fitPendingRef.current = false;
    }
  }, [map, cells, maxCount]);

  // 聚类数据写入
  useEffect(() => {
    if (!map) return;
    const source = map.getSource("heat-clusters") as GeoJSONSource | undefined;
    if (!source) return;
    source.setData(
      featureCollection(
        clusters.map((c) => ({
          type: "Feature" as const,
          geometry: { type: "Point" as const, coordinates: [c.centerLng, c.centerLat] as [number, number] },
          properties: { hits: c.hits, uniqueIps: c.uniqueIps, topReason: c.topReason || "" }
        }))
      )
    );
  }, [map, clusters]);

  // 类别切换 → 颜色体系
  useEffect(() => {
    if (!map) return;
    map.setPaintProperty("heat-layer", "heatmap-color", heatRamp(kind));
    map.setPaintProperty("heat-points", "circle-color", KIND_META[kind].pointColor);
  }, [map, kind]);

  // 主题切换 → 圆点描边贴合陆地色
  useEffect(() => {
    if (!map) return;
    map.setPaintProperty("heat-points", "circle-stroke-color", dark ? "#18181b" : "#ffffff");
  }, [map, dark]);

  const isLoading = cellQuery.isLoading || rankQuery.isLoading;
  const isEmpty = !isLoading && cellQuery.isSuccess && cells.length === 0;

  return (
    <div className="space-y-4">
      {/* 顶部：标题 + 控制 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-orange-500/10 text-orange-600 dark:text-orange-400">
            <Flame className="size-4" />
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-semibold leading-tight">地理热力分析</span>
            <span className="text-[11px] leading-tight text-muted-foreground">
              基于小时级预聚合（geohash-5 网格），窗口内共{" "}
              <span className="font-medium text-foreground">{fmtCount(total)}</span> 起事件
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* 事件类别 */}
          <div className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {(Object.keys(KIND_META) as GeoStatsKind[]).map((k) => {
              const Icon = KIND_META[k].icon;
              return (
                <button
                  key={k}
                  type="button"
                  onClick={() => setKind(k)}
                  className={cn(
                    "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                    kind === k
                      ? "bg-background text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <Icon className="size-3.5" />
                  {KIND_META[k].label}
                </button>
              );
            })}
          </div>

          {/* 时间窗口 */}
          <Select value={String(hours)} onValueChange={(v) => setHours(Number(v))}>
            <SelectTrigger className="h-8 w-[124px] text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANGE_OPTIONS.map((r) => (
                <SelectItem key={r.hours} value={String(r.hours)} className="text-xs">
                  {r.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Button
            size="sm"
            variant="outline"
            className="h-8 gap-1.5"
            onClick={() => {
              cellQuery.refetch();
              rankQuery.refetch();
              if (showClusters) clusterQuery.refetch();
            }}
            disabled={cellQuery.isFetching}
          >
            <RefreshCw className={cn("size-3.5", cellQuery.isFetching && "animate-spin")} />
            刷新
          </Button>
        </div>
      </div>

      {/* 地图 + 国家排行 */}
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_288px]">
        <BaseMap className="h-[460px] xl:h-[540px]" onMapReady={handleMapReady}>
          {/* 聚类叠加开关（仅拦截类别） */}
          {kind === "block" && (
            <label className="absolute top-3 left-3 z-10 flex cursor-pointer items-center gap-2 rounded-lg border border-border bg-card/95 px-2.5 py-1.5 backdrop-blur-sm">
              <Crosshair className="size-3.5 text-red-500" />
              <span className="text-[11px] font-medium">攻击聚类</span>
              <Switch checked={showClusters} onCheckedChange={setShowClusters} className="scale-90" />
              {clusterQuery.isFetching && <Loader2 className="size-3 animate-spin text-muted-foreground" />}
            </label>
          )}

          {/* 国家筛选提示 */}
          {country && (
            <button
              type="button"
              onClick={() => setCountry("")}
              className="absolute top-3 right-12 z-10 flex items-center gap-1.5 rounded-lg border border-primary/40 bg-primary/10 px-2.5 py-1.5 text-[11px] font-medium text-primary backdrop-blur-sm transition-colors hover:bg-primary/15"
            >
              <CountryFlag code={country} size={14} /> {countryName(country)}
              <X className="size-3" />
            </button>
          )}

          {/* 加载 / 空态浮层 */}
          {isLoading && (
            <div className="absolute inset-x-0 top-3 z-10 flex justify-center">
              <span className="flex items-center gap-1.5 rounded-full border border-border bg-card/95 px-3 py-1 text-[11px] text-muted-foreground backdrop-blur-sm">
                <Loader2 className="size-3 animate-spin" />
                正在加载热力数据...
              </span>
            </div>
          )}
          {isEmpty && (
            <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
              <span className="rounded-full border border-border bg-card/95 px-4 py-1.5 text-xs text-muted-foreground backdrop-blur-sm">
                当前窗口内没有{KIND_META[kind].label}数据
              </span>
            </div>
          )}
        </BaseMap>

        {/* 国家排行 */}
        <div className="flex min-h-[300px] flex-col rounded-xl border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <span className="text-xs font-semibold">国家 / 地区排行</span>
            <Badge variant="outline" size="sm">
              {countries.length} 个
            </Badge>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {countries.length === 0 ? (
              <p className="px-2 py-8 text-center text-xs text-muted-foreground">暂无数据</p>
            ) : (
              <ul className="space-y-0.5">
                {countries.map((c, idx) => {
                  const active = country === c.countryCode;
                  const share = maxCountryCount > 0 ? Math.max(2, (c.count / maxCountryCount) * 100) : 0;
                  return (
                    <li key={c.countryCode || `unknown-${idx}`}>
                      <button
                        type="button"
                        onClick={() => setCountry(active ? "" : c.countryCode)}
                        className={cn(
                          "w-full rounded-lg px-2.5 py-2 text-left transition-colors",
                          active ? "bg-primary/10 ring-1 ring-primary/30" : "hover:bg-muted/60"
                        )}
                      >
                        <span className="flex items-baseline justify-between gap-2">
                          <span className="flex min-w-0 items-baseline gap-1.5 text-xs">
                            <span className="w-5 shrink-0 text-right font-mono text-[10px] text-muted-foreground/70">
                              {idx + 1}
                            </span>
                            <CountryFlag code={c.countryCode} size={15} className="self-center" />
                            <span className="truncate font-medium">{countryName(c.countryCode)}</span>
                          </span>
                          <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
                            {fmtCount(c.count)}
                          </span>
                        </span>
                        <span className="mt-1.5 block h-1 overflow-hidden rounded-full bg-muted">
                          <span
                            className={cn(
                              "block h-full rounded-full",
                              kind === "block" ? "bg-red-500/70" : "bg-indigo-500/70"
                            )}
                            style={{ width: `${share}%` }}
                          />
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
