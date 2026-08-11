"use client";

// 用户登录轨迹回放
//
// 数据：GET /api/admin/system/geo/trail（userId + appId 必填）。
// 回放：rAF 驱动的分段线性插值 —— 精确进度存 ref，React 只在
//       「当前点序号变化」时重渲染，列表与地图标记保持 60fps 平滑。
// 风控辅助：相邻两点位移速度 > 1000 km/h 且距离 > 80km 标记为
//           「不可能旅行」，与后端 geo_impossible_travel 规则同口径。

import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
// maplibre-gl v6 起不再提供 default export，改用命名空间导入
import * as maplibregl from "maplibre-gl";
import type { Map as MapLibreMap, GeoJSONSource, ExpressionSpecification } from "maplibre-gl";
import { useTheme } from "next-themes";
import {
  AlertTriangle,
  Loader2,
  Pause,
  Play,
  RefreshCw,
  Route,
  Search,
  SkipBack
} from "lucide-react";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { MapLibreMap as BaseMap } from "@/components/maps/maplibre-map";
import { useAdminAppsQuery, useAdminToken } from "@/lib/admin-hooks";
import { getGeoTrail, type GeoTrailPoint } from "@/lib/api/geo";
import { CountryFlag } from "@/components/ui/country-flag";
import { coordsBounds, flagImgHtml, fmtCount, fmtTimeShort, haversineKm } from "@/lib/geo/geo-map-shared";

type LocatedPoint = GeoTrailPoint & { lat: number; lng: number };

type TrailParams = { userId: number; appId: number; limit: number };

const TRAIL_COLOR = "#6366f1";
const TRAIL_FADED = "#a1a1aa";
const SEGMENT_SECONDS = 1.2; // 1x 速度下每段耗时

function esc(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function pointColorExpr(maxIdx: number): ExpressionSpecification {
  return [
    "case",
    ["==", ["get", "flagged"], 1],
    "#ef4444",
    ["interpolate", ["linear"], ["get", "idx"], 0, TRAIL_FADED, Math.max(1, maxIdx), TRAIL_COLOR]
  ];
}

export function GeoTrailPanel() {
  const token = useAdminToken();
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";

  const appsQuery = useAdminAppsQuery();
  const apps = appsQuery.data ?? [];

  // ── 查询表单 ──
  const [appSel, setAppSel] = useState("");
  const [userIdInput, setUserIdInput] = useState("");
  const [limitSel, setLimitSel] = useState("100");
  const [params, setParams] = useState<TrailParams | null>(null);

  const trailQuery = useQuery({
    queryKey: ["geo-trail", token, params],
    queryFn: () => getGeoTrail(token as string, params as TrailParams),
    enabled: Boolean(token && params),
    staleTime: 15_000
  });

  const rawTotal = trailQuery.data?.length ?? 0;
  const points = useMemo<LocatedPoint[]>(() => {
    return (trailQuery.data ?? [])
      .filter((p): p is LocatedPoint => p.lat != null && p.lng != null)
      .sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime());
  }, [trailQuery.data]);
  const unlocated = rawTotal - points.length;

  /** 不可能旅行点（与上一点的速度 > 1000 km/h 且距离 > 80km） */
  const flagged = useMemo(() => {
    const set = new Set<number>();
    for (let i = 1; i < points.length; i++) {
      const a = points[i - 1];
      const b = points[i];
      const km = haversineKm(a.lat, a.lng, b.lat, b.lng);
      const hours = Math.max((new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()) / 3_600_000, 1 / 3600);
      if (km > 80 && km / hours > 1000) set.add(i);
    }
    return set;
  }, [points]);

  const countryCount = useMemo(() => {
    const s = new Set(points.map((p) => p.countryCode).filter(Boolean));
    return s.size;
  }, [points]);

  // ── 回放状态 ──
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState("1");
  const [follow, setFollow] = useState(true);
  const [activeIdx, setActiveIdx] = useState(0);
  const progressRef = useRef(0);
  const followRef = useRef(follow);
  useEffect(() => {
    followRef.current = follow;
  }, [follow]);

  const [map, setMap] = useState<MapLibreMap | null>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const markerRef = useRef<maplibregl.Marker | null>(null);
  const activeRowRef = useRef<HTMLLIElement | null>(null);
  const pointsRef = useRef<LocatedPoint[]>([]);
  useEffect(() => {
    pointsRef.current = points;
  }, [points]);

  // ── 地图初始化 ──
  const handleMapReady = useCallback((m: MapLibreMap) => {
    mapRef.current = m;
    const emptyFC = { type: "FeatureCollection" as const, features: [] };
    m.addSource("trail-line", { type: "geojson", data: emptyFC, lineMetrics: true });
    m.addSource("trail-points", { type: "geojson", data: emptyFC });

    m.addLayer({
      id: "trail-line",
      type: "line",
      source: "trail-line",
      layout: { "line-cap": "round", "line-join": "round" },
      paint: {
        "line-width": 2.2,
        "line-gradient": [
          "interpolate",
          ["linear"],
          ["line-progress"],
          0, TRAIL_FADED,
          1, TRAIL_COLOR
        ]
      }
    });

    m.addLayer({
      id: "trail-active-ring",
      type: "circle",
      source: "trail-points",
      filter: ["==", ["get", "idx"], -1],
      paint: {
        "circle-radius": 9,
        "circle-color": "rgba(99,102,241,0.22)",
        "circle-stroke-color": TRAIL_COLOR,
        "circle-stroke-width": 1.6
      }
    });

    m.addLayer({
      id: "trail-points",
      type: "circle",
      source: "trail-points",
      paint: {
        "circle-radius": ["case", ["==", ["get", "flagged"], 1], 5, 3.8],
        "circle-color": pointColorExpr(1),
        "circle-stroke-color": "#ffffff",
        "circle-stroke-width": 1.2
      }
    });

    const popup = new maplibregl.Popup({ closeButton: false, offset: 12, maxWidth: "280px" });
    m.on("click", "trail-points", (e) => {
      const f = e.features?.[0];
      if (!f) return;
      const idx = Number(f.properties?.idx);
      const p = pointsRef.current[idx];
      if (!p) return;
      setPlaying(false);
      progressRef.current = idx;
      setActiveIdx(idx);
      markerRef.current?.setLngLat([p.lng, p.lat]);
      popup
        .setLngLat([p.lng, p.lat])
        .setHTML(
          `<div style="padding:10px 12px;font-size:12px;line-height:1.65">
             <div style="font-weight:600">${flagImgHtml(p.countryCode)}${esc(p.city || p.region || p.country || "未知位置")}</div>
             <div style="opacity:.72">${esc(new Date(p.createdAt).toLocaleString("zh-CN"))}</div>
             <div style="opacity:.72;font-family:var(--font-mono)">${esc(p.ip)}</div>
             <div style="opacity:.72">${esc(p.loginType || "login")}${p.deviceId ? ` · ${esc(p.deviceId.slice(0, 18))}` : ""}</div>
           </div>`
        )
        .addTo(m);
    });
    m.on("mouseenter", "trail-points", () => {
      m.getCanvas().style.cursor = "pointer";
    });
    m.on("mouseleave", "trail-points", () => {
      m.getCanvas().style.cursor = "";
    });

    setMap(m);
  }, []);

  // ── 轨迹数据写入地图 ──
  useEffect(() => {
    if (!map) return;
    const lineSource = map.getSource("trail-line") as GeoJSONSource | undefined;
    const pointSource = map.getSource("trail-points") as GeoJSONSource | undefined;
    if (!lineSource || !pointSource) return;

    lineSource.setData(
      points.length >= 2
        ? {
            type: "FeatureCollection",
            features: [
              {
                type: "Feature",
                geometry: { type: "LineString", coordinates: points.map((p) => [p.lng, p.lat]) },
                properties: {}
              }
            ]
          }
        : { type: "FeatureCollection", features: [] }
    );

    const maxIdx = Math.max(1, points.length - 1);
    pointSource.setData({
      type: "FeatureCollection",
      features: points.map((p, i) => ({
        type: "Feature",
        geometry: { type: "Point", coordinates: [p.lng, p.lat] },
        properties: {
          idx: i,
          flagged: flagged.has(i) ? 1 : 0,
          role: i === 0 ? "start" : i === points.length - 1 ? "end" : "mid"
        }
      }))
    });
    map.setPaintProperty("trail-points", "circle-color", pointColorExpr(maxIdx));

    // 重置回放并取景
    progressRef.current = 0;
    setActiveIdx(0);
    setPlaying(false);
    if (points.length > 0) {
      const bounds = coordsBounds(points.map((p) => [p.lng, p.lat] as [number, number]));
      if (bounds) map.fitBounds(bounds, { padding: 72, duration: 600, maxZoom: 9 });
    }
  }, [map, points, flagged]);

  // ── 当前点高亮环 ──
  useEffect(() => {
    if (!map) return;
    map.setFilter("trail-active-ring", ["==", ["get", "idx"], activeIdx]);
  }, [map, activeIdx]);

  // ── 主题：点描边贴合陆地色 ──
  useEffect(() => {
    if (!map) return;
    map.setPaintProperty("trail-points", "circle-stroke-color", dark ? "#18181b" : "#ffffff");
  }, [map, dark]);

  // ── 回放标记 ──
  useEffect(() => {
    if (!map || points.length === 0) return;
    const el = document.createElement("div");
    el.className = "aegis-trail-marker";
    const marker = new maplibregl.Marker({ element: el })
      .setLngLat([points[0].lng, points[0].lat])
      .addTo(map);
    markerRef.current = marker;
    return () => {
      marker.remove();
      markerRef.current = null;
    };
  }, [map, points]);

  // ── rAF 回放循环 ──
  useEffect(() => {
    if (!playing || !map || points.length < 2) return;
    let raf = 0;
    let last = performance.now();
    const maxIdx = points.length - 1;
    const rate = Number(speed) / SEGMENT_SECONDS;

    const tick = (now: number) => {
      const dt = (now - last) / 1000;
      last = now;
      let p = progressRef.current + dt * rate;
      if (p >= maxIdx) {
        p = maxIdx;
        setPlaying(false);
      }
      progressRef.current = p;

      const i = Math.min(Math.floor(p), maxIdx - 1);
      const frac = Math.min(1, p - i);
      const a = points[i];
      const b = points[i + 1];
      const lng = a.lng + (b.lng - a.lng) * frac;
      const lat = a.lat + (b.lat - a.lat) * frac;
      markerRef.current?.setLngLat([lng, lat]);
      if (followRef.current) map.setCenter([lng, lat]);

      const nextActive = Math.round(p);
      setActiveIdx((prev) => (prev === nextActive ? prev : nextActive));
      if (p < maxIdx) raf = requestAnimationFrame(tick);
    };

    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [playing, speed, map, points]);

  // ── 列表自动滚动到当前点 ──
  useEffect(() => {
    activeRowRef.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [activeIdx]);

  const jumpTo = useCallback(
    (idx: number, ease = true) => {
      const p = points[idx];
      if (!p) return;
      progressRef.current = idx;
      setActiveIdx(idx);
      markerRef.current?.setLngLat([p.lng, p.lat]);
      if (ease) map?.easeTo({ center: [p.lng, p.lat], duration: 420 });
    },
    [points, map]
  );

  const handleSubmit = useCallback(
    (e?: FormEvent) => {
      e?.preventDefault();
      const userId = Number(userIdInput.trim());
      const appId = Number(appSel);
      if (!appId) return;
      if (!Number.isInteger(userId) || userId <= 0) return;
      setParams({ userId, appId, limit: Number(limitSel) });
    },
    [userIdInput, appSel, limitSel]
  );

  const canSubmit = Boolean(appSel) && /^\d+$/.test(userIdInput.trim()) && Number(userIdInput) > 0;
  const activePoint = points[activeIdx];
  const isInitial = !params;
  const isEmpty = Boolean(params) && trailQuery.isSuccess && points.length === 0;

  return (
    <div className="space-y-4">
      {/* 顶部：标题 + 查询表单 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-indigo-500/10 text-indigo-600 dark:text-indigo-400">
            <Route className="size-4" />
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-semibold leading-tight">登录轨迹回放</span>
            <span className="text-[11px] leading-tight text-muted-foreground">
              按时间顺序回放用户登录位置，自动标注「不可能旅行」异常段
            </span>
          </div>
        </div>

        <form className="flex flex-wrap items-center gap-2" onSubmit={handleSubmit}>
          <Select value={appSel} onValueChange={setAppSel}>
            <SelectTrigger className="h-8 w-[150px] text-xs">
              <SelectValue placeholder="选择应用" />
            </SelectTrigger>
            <SelectContent className="max-h-64">
              {apps.map((a) => (
                <SelectItem key={a.id} value={String(a.id)} className="text-xs">
                  {a.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            className="h-8 w-[128px] font-mono text-xs"
            inputMode="numeric"
            placeholder="用户 ID"
            value={userIdInput}
            onChange={(e) => setUserIdInput(e.target.value)}
          />
          <Select value={limitSel} onValueChange={setLimitSel}>
            <SelectTrigger className="h-8 w-[110px] text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {["50", "100", "200", "500"].map((n) => (
                <SelectItem key={n} value={n} className="text-xs">
                  最近 {n} 条
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="submit" size="sm" className="h-8 gap-1.5" disabled={!canSubmit || trailQuery.isFetching}>
            {trailQuery.isFetching ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Search className="size-3.5" />
            )}
            查询
          </Button>
          {params && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-8 gap-1.5"
              onClick={() => trailQuery.refetch()}
              disabled={trailQuery.isFetching}
            >
              <RefreshCw className={cn("size-3.5", trailQuery.isFetching && "animate-spin")} />
              刷新
            </Button>
          )}
        </form>
      </div>

      {/* 统计行 */}
      {params && points.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <Badge variant="outline" size="sm">
            轨迹点 {fmtCount(points.length)}
            {unlocated > 0 && <span className="ml-1 text-muted-foreground">（{unlocated} 条无坐标）</span>}
          </Badge>
          <Badge variant="outline" size="sm">
            跨越 {countryCount} 个国家 / 地区
          </Badge>
          <Badge variant="outline" size="sm">
            {fmtTimeShort(points[0]?.createdAt)} → {fmtTimeShort(points[points.length - 1]?.createdAt)}
          </Badge>
          {flagged.size > 0 && (
            <Badge variant="danger" size="sm">
              <AlertTriangle className="mr-0.5 size-3" />
              {flagged.size} 段不可能旅行
            </Badge>
          )}
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_300px]">
        {/* 地图 */}
        <div className="space-y-3">
          <BaseMap className="h-[440px] xl:h-[500px]" onMapReady={handleMapReady}>
            {isInitial && (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <div className="rounded-xl border border-border bg-card/95 px-5 py-4 text-center backdrop-blur-sm">
                  <Route className="mx-auto size-5 text-muted-foreground" />
                  <p className="mt-2 text-xs font-medium text-foreground">选择应用并输入用户 ID</p>
                  <p className="mt-0.5 text-[11px] text-muted-foreground">查询后即可回放该用户的登录轨迹</p>
                </div>
              </div>
            )}
            {trailQuery.isLoading && (
              <div className="absolute inset-x-0 top-3 z-10 flex justify-center">
                <span className="flex items-center gap-1.5 rounded-full border border-border bg-card/95 px-3 py-1 text-[11px] text-muted-foreground backdrop-blur-sm">
                  <Loader2 className="size-3 animate-spin" />
                  正在加载轨迹...
                </span>
              </div>
            )}
            {isEmpty && (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <span className="rounded-full border border-border bg-card/95 px-4 py-1.5 text-xs text-muted-foreground backdrop-blur-sm">
                  {rawTotal > 0 ? "该用户的登录事件均无坐标信息" : "未找到该用户的登录轨迹"}
                </span>
              </div>
            )}
          </BaseMap>

          {/* 回放控制条 */}
          <div className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-card px-3 py-2.5">
            <div className="flex items-center gap-1">
              <Button
                size="icon"
                variant="ghost"
                className="size-8"
                title="回到起点"
                disabled={points.length === 0}
                onClick={() => jumpTo(0)}
              >
                <SkipBack className="size-4" />
              </Button>
              <Button
                size="icon"
                className="size-8"
                title={playing ? "暂停" : "播放"}
                disabled={points.length < 2}
                onClick={() => {
                  if (!playing && progressRef.current >= points.length - 1) {
                    jumpTo(0, false);
                  }
                  setPlaying((v) => !v);
                }}
              >
                {playing ? <Pause className="size-4" /> : <Play className="size-4" />}
              </Button>
            </div>

            <Slider
              className="min-w-[120px] flex-1"
              min={0}
              max={Math.max(0, points.length - 1)}
              step={1}
              value={[Math.min(activeIdx, Math.max(0, points.length - 1))]}
              disabled={points.length < 2}
              onValueChange={([v]) => jumpTo(v, false)}
            />

            <Select value={speed} onValueChange={setSpeed}>
              <SelectTrigger className="h-7 w-[72px] text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {["1", "2", "4", "8"].map((s) => (
                  <SelectItem key={s} value={s} className="text-xs">
                    {s}x
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <label className="flex cursor-pointer items-center gap-1.5 text-[11px] text-muted-foreground">
              跟随
              <Switch checked={follow} onCheckedChange={setFollow} className="scale-90" />
            </label>

            <span className="min-w-0 truncate text-[11px] text-muted-foreground">
              {activePoint ? (
                <>
                  <span className="font-medium text-foreground">
                    {activeIdx + 1}/{points.length}
                  </span>{" "}
                  · {fmtTimeShort(activePoint.createdAt)} ·{" "}
                  <CountryFlag code={activePoint.countryCode} size={13} className="mx-0.5" />
                  {activePoint.city || activePoint.country || "未知"}
                </>
              ) : (
                "—"
              )}
            </span>
          </div>
        </div>

        {/* 时间轴列表 */}
        <div className="flex max-h-[560px] min-h-[320px] flex-col rounded-xl border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <span className="text-xs font-semibold">登录时间轴</span>
            <Badge variant="outline" size="sm">
              {points.length} 点
            </Badge>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {points.length === 0 ? (
              <p className="px-2 py-10 text-center text-xs text-muted-foreground">
                {isInitial ? "等待查询" : "暂无可回放的轨迹点"}
              </p>
            ) : (
              <ul className="space-y-0.5">
                {points.map((p, i) => {
                  const active = i === activeIdx;
                  const isFlagged = flagged.has(i);
                  return (
                    <li key={`${p.createdAt}-${i}`} ref={active ? activeRowRef : undefined}>
                      <button
                        type="button"
                        onClick={() => {
                          setPlaying(false);
                          jumpTo(i);
                        }}
                        className={cn(
                          "w-full rounded-lg px-2.5 py-2 text-left transition-colors",
                          active ? "bg-primary/10 ring-1 ring-primary/30" : "hover:bg-muted/60"
                        )}
                      >
                        <span className="flex items-center justify-between gap-2">
                          <span className="flex min-w-0 items-center gap-1.5">
                            <span
                              className={cn(
                                "w-6 shrink-0 text-right font-mono text-[10px]",
                                active ? "text-primary" : "text-muted-foreground/70"
                              )}
                            >
                              {i + 1}
                            </span>
                            <CountryFlag code={p.countryCode} size={14} className="self-center" />
                            <span className="truncate text-xs font-medium">
                              {p.city || p.region || p.country || "未知位置"}
                            </span>
                            {isFlagged && <AlertTriangle className="size-3 shrink-0 text-red-500" />}
                          </span>
                          <span className="shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground">
                            {fmtTimeShort(p.createdAt)}
                          </span>
                        </span>
                        <span className="mt-0.5 flex items-center gap-1.5 pl-[30px]">
                          <span className="truncate font-mono text-[10.5px] text-muted-foreground">{p.ip}</span>
                          {p.loginType && (
                            <Badge variant="secondary" size="sm" className="px-1 py-0 text-[9.5px]">
                              {p.loginType}
                            </Badge>
                          )}
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
