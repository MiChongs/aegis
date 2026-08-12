"use client";

// 攻击飞线图 —— deck.gl ⋈ MapLibre（GPU 渲染）
//
// 架构：
//   - 底图复用 components/maps/maplibre-map（本地矢量 / Carto 瓦片、zinc 主题联动）
//   - deck.gl MapboxOverlay 以 interleaved 模式叠加：ArcLayer 大圆弧线 +
//     ScatterplotLayer 攻击源 + HeatmapLayer GPU 密度图（二选一图层风格）
//   - 「立体」视角 = 地图 pitch 倾斜 + 弧线抬升（替代旧 echarts-gl globe，
//     渲染开销约为其 1/5，且与 2D 共享全部图层与交互）
//
// 地理数据处理（@turf/turf）：
//   原始拦截日志（≤500 条）按 IP 去重 → DBSCAN（120km）空间聚类合并近邻攻击源
//   → 按拦截量截取 Top 150 绘制弧线，避免万级 IP 时的视觉与性能崩坏。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Map as MapLibreMap, IControl } from "maplibre-gl";
import { MapboxOverlay } from "@deck.gl/mapbox";
import { ArcLayer, ScatterplotLayer } from "@deck.gl/layers";
import { HeatmapLayer } from "@deck.gl/aggregation-layers";
import type { Layer, PickingInfo } from "@deck.gl/core";
import { clustersDbscan, featureCollection, point } from "@turf/turf";
import type { Feature, Point as GeoJSONPoint } from "geojson";
import { useTheme } from "next-themes";
import { Loader2, Settings2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { MapLibreMap as BaseMap } from "@/components/maps/maplibre-map";
import { ServerLocationDialog } from "@/components/maps/server-location-dialog";
import { useServerLocation, type ServerLocation } from "@/lib/geo/server-location";
import { useFirewallLogsQuery } from "@/lib/admin-hooks";
import type { FirewallLogEntry } from "@/lib/api/types";
import { coordsBounds, flagImgHtml, fmtCount, haversineKm } from "@/lib/geo/geo-map-shared";

// ── 常量与持久化 ──────────────────────────

const VIEW_KEY = "aegis:attack-map:view-mode";
const STYLE_KEY = "aegis:attack-map:layer-style";
const MAP_PAGE_SIZE = 500;
const MAX_ARCS = 150;
const CLUSTER_DISTANCE_KM = 120;

type ViewMode = "flat" | "tilt";
type LayerStyle = "arcs" | "heat";

function loadEnum<T extends string>(key: string, allowed: T[], fallback: T): T {
  if (typeof window === "undefined") return fallback;
  try {
    const v = localStorage.getItem(key) as T | null;
    return v && allowed.includes(v) ? v : fallback;
  } catch {
    return fallback;
  }
}

function persist(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // 隐私模式忽略
  }
}

// ── 攻击数据处理 ──────────────────────────

type AttackPoint = {
  lat: number;
  lng: number;
  count: number; // 拦截次数合计
  ips: number; // 合并的独立 IP 数
  label: string; // 展示名（城市 / IP / 聚类描述）
  country: string;
  countryCode: string;
  severity: string;
};

function sevWeight(s: string) {
  return s === "critical" ? 4 : s === "high" ? 3 : s === "medium" ? 2 : 1;
}

const SEV_LABEL: Record<string, string> = { critical: "严重", high: "高危", medium: "中等", low: "低危" };

function sevRGB(s: string): [number, number, number] {
  switch (s) {
    case "critical":
      return [239, 68, 68]; // red-500
    case "high":
      return [249, 115, 22]; // orange-500
    case "medium":
      return [234, 179, 8]; // yellow-500
    default:
      return [161, 161, 170]; // zinc-400
  }
}

function hasCoords(lat?: number | null, lng?: number | null) {
  return lat != null && lng != null && !(lat === 0 && lng === 0);
}

/** 同源本地/无坐标 IP 围绕服务器位置做稳定散布，避免全部叠在一个点上 */
function stableJitter(s: string, salt: number): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = ((h << 5) - h + s.charCodeAt(i) + salt * 7) | 0;
  return ((h & 0xffff) / 0xffff - 0.5) * 6;
}

/** 第一步：按 IP 去重聚合 */
function dedupeByIP(logs: FirewallLogEntry[], server: ServerLocation): AttackPoint[] {
  const byIP = new Map<string, AttackPoint>();
  for (const l of logs) {
    const existing = byIP.get(l.ip);
    if (existing) {
      existing.count++;
      if (sevWeight(l.severity) > sevWeight(existing.severity)) existing.severity = l.severity;
      continue;
    }
    const local = !hasCoords(l.latitude, l.longitude);
    byIP.set(l.ip, {
      lat: local ? server.lat + stableJitter(l.ip, 0) : (l.latitude as number),
      lng: local ? server.lng + stableJitter(l.ip, 1) : (l.longitude as number),
      count: 1,
      ips: 1,
      label: l.city || (local ? `${l.ip}（本地网络）` : l.ip),
      country: l.country || (local ? "本地网络" : ""),
      countryCode: l.countryCode || "",
      severity: l.severity || "medium"
    });
  }
  return [...byIP.values()];
}

/** 第二步：turf DBSCAN 把近邻攻击源合并为聚类（密集 IP 段一根弧线） */
function clusterPoints(points: AttackPoint[]): AttackPoint[] {
  if (points.length < 8) return points;
  const fc = featureCollection(points.map((p, i) => point([p.lng, p.lat], { idx: i })));
  const clustered = clustersDbscan(fc, CLUSTER_DISTANCE_KM, { minPoints: 2, units: "kilometers" });

  const groups = new Map<number, AttackPoint[]>();
  const standalone: AttackPoint[] = [];
  for (const f of clustered.features as Array<Feature<GeoJSONPoint, { idx: number; cluster?: number }>>) {
    const src = points[f.properties.idx];
    if (typeof f.properties.cluster === "number") {
      const list = groups.get(f.properties.cluster) ?? [];
      list.push(src);
      groups.set(f.properties.cluster, list);
    } else {
      standalone.push(src);
    }
  }

  const merged: AttackPoint[] = [...standalone];
  for (const members of groups.values()) {
    const total = members.reduce((s, m) => s + m.count, 0);
    const top = members.reduce((a, b) => (b.count > a.count ? b : a));
    merged.push({
      // 按拦截量加权的质心，让聚合点落在攻击最密的位置附近
      lat: members.reduce((s, m) => s + m.lat * m.count, 0) / total,
      lng: members.reduce((s, m) => s + m.lng * m.count, 0) / total,
      count: total,
      ips: members.reduce((s, m) => s + m.ips, 0),
      label: members.length > 1 ? `${top.label} 等 ${members.length} 个源` : top.label,
      country: top.country,
      countryCode: top.countryCode,
      severity: members.reduce((a, b) => (sevWeight(b.severity) > sevWeight(a.severity) ? b : a)).severity
    });
  }
  return merged;
}

function esc(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

// ── 大圆飞线动画几何 ──────────────────────────

/** 球面大圆插值：返回 n+1 个 [lng,lat] 采样点（与 ArcLayer greatCircle 走同一条线） */
function greatCirclePath(lng1: number, lat1: number, lng2: number, lat2: number, n = 64): [number, number][] {
  const toRad = Math.PI / 180;
  const toDeg = 180 / Math.PI;
  const φ1 = lat1 * toRad;
  const λ1 = lng1 * toRad;
  const φ2 = lat2 * toRad;
  const λ2 = lng2 * toRad;
  const d =
    2 *
    Math.asin(
      Math.min(
        1,
        Math.sqrt(Math.sin((φ2 - φ1) / 2) ** 2 + Math.cos(φ1) * Math.cos(φ2) * Math.sin((λ2 - λ1) / 2) ** 2)
      )
    );
  const out: [number, number][] = [];
  if (d < 1e-9) {
    for (let i = 0; i <= n; i++) out.push([lng1, lat1]);
    return out;
  }
  const sinD = Math.sin(d);
  for (let i = 0; i <= n; i++) {
    const f = i / n;
    const A = Math.sin((1 - f) * d) / sinD;
    const B = Math.sin(f * d) / sinD;
    const x = A * Math.cos(φ1) * Math.cos(λ1) + B * Math.cos(φ2) * Math.cos(λ2);
    const y = A * Math.cos(φ1) * Math.sin(λ1) + B * Math.cos(φ2) * Math.sin(λ2);
    const z = A * Math.sin(φ1) + B * Math.sin(φ2);
    out.push([Math.atan2(y, x) * toDeg, Math.atan2(z, Math.sqrt(x * x + y * y)) * toDeg]);
  }
  return out;
}

/** 沿采样路径按归一化参数 t∈[0,1] 线性取点 */
function sampleAlong(path: [number, number][], t: number): [number, number] {
  const n = path.length - 1;
  if (n <= 0) return path[0];
  const x = Math.max(0, Math.min(1, t)) * n;
  const i = Math.floor(x);
  if (i >= n) return path[n];
  const f = x - i;
  const a = path[i];
  const b = path[i + 1];
  return [a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f];
}

type ArcPath = {
  samples: [number, number][];
  rgb: [number, number, number];
  distKm: number;
  phase: number; // 错相，避免所有脉冲同步
  travelSec: number; // 越远越慢
};

type PulseDot = { pos: [number, number, number]; color: [number, number, number, number]; radius: number };

// ── 组件 ──────────────────────────

type AttackFlightMapProps = {
  timeRange?: { startTime?: string; endTime?: string };
};

export function AttackFlightMap({ timeRange }: AttackFlightMapProps) {
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";

  // 端点位置与「用户活动地图」共用一份配置（同一台机器不该在两张图上各在一处）
  const server = useServerLocation();
  const [viewMode, setViewMode] = useState<ViewMode>(() => loadEnum(VIEW_KEY, ["flat", "tilt"], "flat"));
  const [layerStyle, setLayerStyle] = useState<LayerStyle>(() => loadEnum(STYLE_KEY, ["arcs", "heat"], "arcs"));
  const [showSettings, setShowSettings] = useState(false);

  const mapQuery = useFirewallLogsQuery({
    pageSize: MAP_PAGE_SIZE,
    page: 1,
    startTime: timeRange?.startTime,
    endTime: timeRange?.endTime,
    sortBy: "blocked_at",
    sortOrder: "desc"
  });
  const logs = useMemo(() => mapQuery.data?.items ?? [], [mapQuery.data]);

  const rawPoints = useMemo(() => dedupeByIP(logs, server), [logs, server]);
  const points = useMemo(() => {
    const merged = clusterPoints(rawPoints);
    merged.sort((a, b) => b.count - a.count);
    return merged.slice(0, MAX_ARCS);
  }, [rawPoints]);
  const totalBlocks = useMemo(() => rawPoints.reduce((s, p) => s + p.count, 0), [rawPoints]);
  const maxCount = points.length > 0 ? points[0].count : 1;

  // ── deck.gl overlay ──
  const [map, setMap] = useState<MapLibreMap | null>(null);
  const overlayRef = useRef<MapboxOverlay | null>(null);
  const fitPendingRef = useRef(true);

  const handleMapReady = useCallback((m: MapLibreMap) => {
    const overlay = new MapboxOverlay({
      // 叠加模式（独立画布渲染在底图之上）：比 interleaved 更稳健，
      // 弧线渲染不依赖底图 GL 状态，底图瓦片被墙/加载失败时弧线仍可见
      interleaved: false,
      layers: [],
      getTooltip: (info: PickingInfo) => {
        const d = info.object as AttackPoint | undefined;
        if (!d || typeof d.count !== "number") return null;
        return {
          html: `<div style="padding:8px 10px;font-size:12px;line-height:1.6">
              <div style="font-weight:600">${flagImgHtml(d.countryCode)}${esc(d.label)}</div>
              <div style="opacity:.75">${esc(d.country)} · ${SEV_LABEL[d.severity] ?? d.severity}</div>
              <div style="opacity:.75">拦截 <b>${d.count.toLocaleString()}</b> 次 · ${d.ips} 个 IP</div>
            </div>`,
          style: {
            background: "var(--popover)",
            color: "var(--popover-foreground)",
            border: "1px solid var(--border)",
            borderRadius: "10px",
            padding: "0",
            boxShadow: "0 8px 24px color-mix(in srgb, var(--foreground) 10%, transparent)"
          }
        };
      }
    });
    // MapboxOverlay 实现 IControl 协议，maplibre 与 mapbox 类型声明在此兼容
    m.addControl(overlay as unknown as IControl);
    overlayRef.current = overlay;
    setMap(m);
  }, []);

  // 卸载时显式释放 deck.gl 资源（BaseMap 的 map.remove 会触发 onRemove，
  // 这里再 finalize 一次确保 WebGL 上下文与图层缓冲及时回收）
  useEffect(() => {
    return () => {
      overlayRef.current?.finalize();
      overlayRef.current = null;
    };
  }, []);

  // 是否启用动画（尊重系统「减弱动态效果」偏好）
  const prefersReducedMotion = useMemo(
    () => typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches,
    []
  );
  const animateArcs = layerStyle === "arcs" && points.length > 0 && !prefersReducedMotion;

  // ── 静态图层（弧线轨迹 / 攻击源 / 服务器端点，或热力图）──
  const staticLayers = useMemo<Layer[]>(() => {
    if (points.length === 0) return [];

    const serverPos: [number, number] = [server.lng, server.lat];
    const result: Layer[] = [];

    if (layerStyle === "heat") {
      result.push(
        new HeatmapLayer<AttackPoint>({
          id: "attack-heat",
          data: points,
          getPosition: (d) => [d.lng, d.lat],
          getWeight: (d) => d.count,
          radiusPixels: 46,
          intensity: 1.1,
          aggregation: "SUM",
          colorRange: [
            [251, 191, 36, 40],
            [251, 191, 36, 120],
            [245, 158, 11, 170],
            [249, 115, 22, 200],
            [239, 68, 68, 230],
            [185, 28, 28, 255]
          ]
        })
      );
    } else {
      result.push(
        new ArcLayer<AttackPoint>({
          id: "attack-arcs",
          data: points,
          greatCircle: true,
          getSourcePosition: (d) => [d.lng, d.lat],
          getTargetPosition: () => serverPos,
          // 动画开启时弧线作为暗色轨迹底衬，让飞行的脉冲更突出
          getSourceColor: (d) => [...sevRGB(d.severity), animateArcs ? 90 : 200] as [number, number, number, number],
          getTargetColor: [99, 102, 241, animateArcs ? 110 : 230],
          getWidth: (d) => 1.2 + 4.5 * Math.sqrt(d.count / maxCount),
          widthUnits: "pixels",
          getHeight: viewMode === "tilt" ? 0.45 : 0.12,
          pickable: true,
          autoHighlight: true,
          highlightColor: [99, 102, 241, 255]
        })
      );
      result.push(
        new ScatterplotLayer<AttackPoint>({
          id: "attack-points",
          data: points,
          getPosition: (d) => [d.lng, d.lat],
          getRadius: (d) => 2.5 + 9 * Math.sqrt(d.count / maxCount),
          radiusUnits: "pixels",
          getFillColor: (d) => [...sevRGB(d.severity), 190] as [number, number, number, number],
          stroked: true,
          getLineColor: dark ? [9, 9, 11, 220] : [255, 255, 255, 230],
          getLineWidth: 1,
          lineWidthUnits: "pixels",
          pickable: true,
          autoHighlight: true,
          highlightColor: [99, 102, 241, 255]
        })
      );
    }

    // 服务器端点（常驻）
    result.push(
      new ScatterplotLayer<ServerLocation>({
        id: "server-point",
        data: [server],
        getPosition: () => serverPos,
        getRadius: 7,
        radiusUnits: "pixels",
        getFillColor: [16, 185, 129, 235], // emerald
        stroked: true,
        getLineColor: dark ? [9, 9, 11, 255] : [255, 255, 255, 255],
        getLineWidth: 2,
        lineWidthUnits: "pixels"
      })
    );
    return result;
  }, [points, server, dark, viewMode, layerStyle, maxCount, animateArcs]);

  // ── 预计算每条弧线的大圆采样路径（points/server 变化时重算）──
  const arcPaths = useMemo<ArcPath[]>(() => {
    if (points.length === 0) return [];
    return points.map((p, i) => ({
      samples: greatCirclePath(p.lng, p.lat, server.lng, server.lat, 64),
      rgb: sevRGB(p.severity),
      distKm: haversineKm(p.lat, p.lng, server.lat, server.lng),
      phase: (i * 0.6180339887) % 1, // 黄金比错相
      travelSec: 2.0 + haversineKm(p.lat, p.lng, server.lat, server.lng) / 9000
    }));
  }, [points, server]);

  // refs 供 rAF 闭包读取最新值，避免每帧重建动画循环
  const staticLayersRef = useRef<Layer[]>(staticLayers);
  const arcPathsRef = useRef<ArcPath[]>(arcPaths);
  const viewModeRef = useRef(viewMode);
  useEffect(() => {
    staticLayersRef.current = staticLayers;
  }, [staticLayers]);
  useEffect(() => {
    arcPathsRef.current = arcPaths;
  }, [arcPaths]);
  useEffect(() => {
    viewModeRef.current = viewMode;
  }, [viewMode]);

  // ── 渲染驱动：动画时用 rAF 每帧推送飞行脉冲；否则只推静态图层 ──
  useEffect(() => {
    const ov = overlayRef.current;
    if (!ov || !map) return;

    if (!animateArcs) {
      ov.setProps({ layers: staticLayers });
      return;
    }

    const TRAIL = 7; // 彗尾点数
    const STEP = 0.018; // 相邻彗尾点的参数间隔
    let raf = 0;
    let last = performance.now();
    let clock = 0;

    const frame = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      clock += dt;
      const tilt = viewModeRef.current === "tilt";
      const dots: PulseDot[] = [];
      for (const a of arcPathsRef.current) {
        const headT = ((clock / a.travelSec) + a.phase) % 1;
        const peak = tilt ? a.distKm * 1000 * 0.16 : 0; // 立体模式抬升以贴合弧线
        for (let k = 0; k < TRAIL; k++) {
          const tt = headT - k * STEP;
          if (tt <= 0) continue;
          const [lng, lat] = sampleAlong(a.samples, tt);
          const z = peak * 4 * tt * (1 - tt); // 抛物线高度（近似 ArcLayer 弧形）
          const fade = 1 - k / TRAIL;
          dots.push({
            pos: [lng, lat, z],
            color: [a.rgb[0], a.rgb[1], a.rgb[2], Math.round(235 * fade * fade)],
            radius: (k === 0 ? 4.6 : 3.0) * fade + 0.5
          });
        }
      }
      ov.setProps({
        layers: [
          ...staticLayersRef.current,
          new ScatterplotLayer<PulseDot>({
            id: "attack-pulse",
            data: dots,
            getPosition: (d) => d.pos,
            getFillColor: (d) => d.color,
            getRadius: (d) => d.radius,
            radiusUnits: "pixels",
            billboard: true,
            stroked: false,
            pickable: false
          })
        ]
      });
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [animateArcs, staticLayers, map]);

  // ── 视角切换 ──
  useEffect(() => {
    if (!map) return;
    map.easeTo(
      viewMode === "tilt" ? { pitch: 52, bearing: -10, duration: 700 } : { pitch: 0, bearing: 0, duration: 600 }
    );
  }, [map, viewMode]);

  // ── 数据到达后自动取景（含服务器位置）──
  useEffect(() => {
    if (!map || points.length === 0 || !fitPendingRef.current) return;
    const bounds = coordsBounds([
      ...points.map((p) => [p.lng, p.lat] as [number, number]),
      [server.lng, server.lat]
    ]);
    if (bounds) map.fitBounds(bounds, { padding: 70, maxZoom: 5, duration: 700 });
    fitPendingRef.current = false;
  }, [map, points, server]);

  useEffect(() => {
    fitPendingRef.current = true;
  }, [timeRange?.startTime, timeRange?.endTime]);

  const switchView = useCallback((v: ViewMode) => {
    setViewMode(v);
    persist(VIEW_KEY, v);
  }, []);
  const switchStyle = useCallback((v: LayerStyle) => {
    setLayerStyle(v);
    persist(STYLE_KEY, v);
  }, []);

  const isEmpty = !mapQuery.isLoading && mapQuery.isSuccess && points.length === 0;

  return (
    <div className="space-y-3">
      {/* 控制行 */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {(["flat", "tilt"] as ViewMode[]).map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => switchView(v)}
                className={cn(
                  "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                  viewMode === v
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                {v === "flat" ? "平面" : "立体"}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {(["arcs", "heat"] as LayerStyle[]).map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => switchStyle(v)}
                className={cn(
                  "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                  layerStyle === v
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                {v === "arcs" ? "攻击弧线" : "密度热力"}
              </button>
            ))}
          </div>
          <span className="text-[11px] text-muted-foreground">
            {fmtCount(totalBlocks)} 次拦截 · {fmtCount(rawPoints.length)} 个 IP
            {points.length < rawPoints.length && <>（聚合为 {points.length} 个攻击源）</>}
          </span>
        </div>
        <Button size="sm" variant="outline" className="h-7 gap-1.5 text-xs" onClick={() => setShowSettings(true)}>
          <Settings2 className="size-3.5" />
          端点位置
        </Button>
      </div>

      {/* 地图 */}
      <BaseMap className="h-[420px] xl:h-[480px]" zoom={1.6} onMapReady={handleMapReady}>
        {mapQuery.isLoading && (
          <div className="absolute inset-x-0 top-3 z-10 flex justify-center">
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-card/95 px-3 py-1 text-[11px] text-muted-foreground backdrop-blur-sm">
              <Loader2 className="size-3 animate-spin" />
              正在加载攻击数据...
            </span>
          </div>
        )}
        {isEmpty && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
            <span className="rounded-full border border-border bg-card/95 px-4 py-1.5 text-xs text-muted-foreground backdrop-blur-sm">
              当前时间窗内没有拦截记录
            </span>
          </div>
        )}
        {/* 图例 */}
        <div className="absolute top-3 left-3 z-10 flex items-center gap-2.5 rounded-lg border border-border bg-card/95 px-2.5 py-1.5 backdrop-blur-sm">
          {(["critical", "high", "medium", "low"] as const).map((s) => (
            <span key={s} className="flex items-center gap-1 text-[10.5px] text-muted-foreground">
              <span
                className="size-2 rounded-full"
                style={{ backgroundColor: `rgb(${sevRGB(s).join(",")})` }}
              />
              {SEV_LABEL[s]}
            </span>
          ))}
          <span className="flex items-center gap-1 border-l border-border pl-2 text-[10.5px] text-muted-foreground">
            <span className="size-2 rounded-full bg-emerald-500" />
            服务器
          </span>
        </div>
      </BaseMap>

      {/* 端点设置 */}
      <ServerLocationDialog
        open={showSettings}
        onOpenChange={setShowSettings}
        value={server}
        description="攻击弧线的汇聚目标；无坐标的本地流量也会散布在该位置附近"
        onSaved={() => {
          fitPendingRef.current = true;
        }}
      />
    </div>
  );
}
