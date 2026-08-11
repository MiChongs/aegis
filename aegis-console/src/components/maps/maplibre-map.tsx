"use client";

// MapLibre GL 主题底图
//
// 设计要点：
//   - 默认「简图」模式：本地 /geo/world-countries.geo.json 矢量轮廓，
//     零外部依赖（离线可用），配色与控制台 zinc 主题完全一致
//   - 可切换「街道」模式：Carto 栅格瓦片（需外网），深浅主题分别用 light_all / dark_all
//   - 主题 / 底图切换只改 paint 与 visibility，不调用 setStyle，
//     保证业务面板加挂的 source / layer 永不丢失

import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
// maplibre-gl v6 起不再提供 default export，改用命名空间导入
import * as maplibregl from "maplibre-gl";
import type { Map as MapLibreInstance, StyleSpecification } from "maplibre-gl";
import { useTheme } from "next-themes";
import { Layers, Map as MapIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export type TilesMode = "vector" | "raster";

const TILES_STORAGE_KEY = "aegis:geo-map:tiles";

function loadTilesMode(): TilesMode {
  if (typeof window === "undefined") return "vector";
  try {
    return localStorage.getItem(TILES_STORAGE_KEY) === "raster" ? "raster" : "vector";
  } catch {
    return "vector";
  }
}

/** 与 globals.css 的 zinc 令牌严格对齐（layer paint 需要具体色值） */
const PALETTE = {
  light: { ocean: "#f4f4f5", land: "#ffffff", boundary: "#e4e4e7" },
  dark: { ocean: "#09090b", land: "#18181b", boundary: "#27272a" }
} as const;

function cartoTiles(flavor: "light_all" | "dark_all") {
  return ["a", "b", "c", "d"].map((s) => `https://${s}.basemaps.cartocdn.com/${flavor}/{z}/{x}/{y}@2x.png`);
}

function buildBaseStyle(dark: boolean, tiles: TilesMode): StyleSpecification {
  const p = dark ? PALETTE.dark : PALETTE.light;
  return {
    version: 8,
    sources: {
      "aegis-world": { type: "geojson", data: "/geo/world-countries.geo.json" },
      "aegis-carto-light": {
        type: "raster",
        tiles: cartoTiles("light_all"),
        tileSize: 512,
        maxzoom: 19
      },
      "aegis-carto-dark": {
        type: "raster",
        tiles: cartoTiles("dark_all"),
        tileSize: 512,
        maxzoom: 19
      }
    },
    // 图层顺序：背景 → 矢量底衬（恒可见）→ 在线栅格（覆盖于矢量之上）。
    // 栅格瓦片被墙 / 加载失败时是透明的，会透出下方矢量轮廓而非纯黑——
    // 这是「街道」模式的优雅降级，对国内网络尤其重要。
    layers: [
      {
        id: "aegis-base-bg",
        type: "background",
        paint: { "background-color": p.ocean }
      },
      {
        id: "aegis-base-land",
        type: "fill",
        source: "aegis-world",
        paint: { "fill-color": p.land }
      },
      {
        id: "aegis-base-boundary",
        type: "line",
        source: "aegis-world",
        paint: { "line-color": p.boundary, "line-width": 0.8 }
      },
      {
        id: "aegis-base-raster-light",
        type: "raster",
        source: "aegis-carto-light",
        layout: { visibility: tiles === "raster" && !dark ? "visible" : "none" },
        paint: { "raster-fade-duration": 150 }
      },
      {
        id: "aegis-base-raster-dark",
        type: "raster",
        source: "aegis-carto-dark",
        layout: { visibility: tiles === "raster" && dark ? "visible" : "none" },
        paint: { "raster-fade-duration": 150 }
      }
    ]
  };
}

/** 主题 / 底图模式变化时原地更新基础图层，不触碰业务图层 */
function applyBaseTheme(map: MapLibreInstance, dark: boolean, tiles: TilesMode) {
  const p = dark ? PALETTE.dark : PALETTE.light;
  // 矢量底衬恒可见（作为栅格的降级底图），仅更新配色
  map.setPaintProperty("aegis-base-bg", "background-color", p.ocean);
  map.setPaintProperty("aegis-base-land", "fill-color", p.land);
  map.setPaintProperty("aegis-base-boundary", "line-color", p.boundary);
  map.setLayoutProperty(
    "aegis-base-raster-light",
    "visibility",
    tiles === "raster" && !dark ? "visible" : "none"
  );
  map.setLayoutProperty(
    "aegis-base-raster-dark",
    "visibility",
    tiles === "raster" && dark ? "visible" : "none"
  );
}

export type MapLibreMapProps = {
  className?: string;
  /** 初始视野（仅首挂载生效） */
  center?: [number, number];
  zoom?: number;
  minZoom?: number;
  maxZoom?: number;
  /** style 加载完毕后调用一次，业务面板在此挂自己的 source / layer */
  onMapReady?: (map: MapLibreInstance) => void;
  /** true 时画布显示十字光标（绘制模式） */
  drawCursor?: boolean;
  /** true 时画布显示抓取光标（顶点拖拽中） */
  grabCursor?: boolean;
  /** 叠加在地图上的覆盖层（工具栏 / 图例等，自行绝对定位） */
  children?: ReactNode;
};

export function MapLibreMap({
  className,
  center = [15, 23],
  zoom = 1.4,
  minZoom = 0.8,
  maxZoom = 18,
  onMapReady,
  drawCursor,
  grabCursor,
  children
}: MapLibreMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<MapLibreInstance | null>(null);
  const [ready, setReady] = useState(false);
  const [tiles, setTiles] = useState<TilesMode>(loadTilesMode);

  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";

  // 初始值固定进 ref，保证地图只创建一次
  const initialRef = useRef({ center, zoom, minZoom, maxZoom, dark, tiles });
  const onMapReadyRef = useRef(onMapReady);
  useEffect(() => {
    onMapReadyRef.current = onMapReady;
  }, [onMapReady]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const init = initialRef.current;
    const map = new maplibregl.Map({
      container,
      style: buildBaseStyle(init.dark, init.tiles),
      center: init.center,
      zoom: init.zoom,
      minZoom: init.minZoom,
      maxZoom: init.maxZoom,
      attributionControl: false,
      dragRotate: false,
      pitchWithRotate: false,
      touchPitch: false
    });
    map.touchZoomRotate.disableRotation();
    map.addControl(new maplibregl.NavigationControl({ showCompass: false }), "top-right");
    mapRef.current = map;

    map.on("load", () => {
      setReady(true);
      onMapReadyRef.current?.(map);
    });

    const ro = new ResizeObserver(() => map.resize());
    ro.observe(container);

    return () => {
      ro.disconnect();
      setReady(false);
      mapRef.current = null;
      map.remove();
    };
  }, []);

  // 主题 / 底图模式切换：原地更新基础图层
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !ready) return;
    applyBaseTheme(map, dark, tiles);
  }, [dark, tiles, ready]);

  const switchTiles = useCallback((next: TilesMode) => {
    setTiles(next);
    try {
      localStorage.setItem(TILES_STORAGE_KEY, next);
    } catch {
      // 隐私模式下 localStorage 不可写，忽略
    }
  }, []);

  return (
    <div
      className={cn("aegis-map relative overflow-hidden rounded-xl border border-border", className)}
      data-draw-cursor={drawCursor ? "true" : undefined}
      data-grab-cursor={grabCursor ? "true" : undefined}
    >
      {/* 用 h-full/w-full 而非 absolute inset-0：maplibre 会给容器强制
          position:relative（其 CSS @import 在 Tailwind 之后，覆盖 .absolute），
          导致 inset-0 无法撑开高度而塌成 0。显式 100% 宽高不依赖 position。 */}
      <div ref={containerRef} className="h-full w-full" />

      {/* 底图模式切换 */}
      <div className="absolute bottom-3 left-3 z-10 flex items-center gap-0.5 rounded-lg border border-border bg-card/95 p-0.5 backdrop-blur-sm">
        <button
          type="button"
          onClick={() => switchTiles("vector")}
          className={cn(
            "flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium transition-colors",
            tiles === "vector" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"
          )}
          title="本地矢量简图（离线可用）"
        >
          <MapIcon className="size-3" />
          简图
        </button>
        <button
          type="button"
          onClick={() => switchTiles("raster")}
          className={cn(
            "flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium transition-colors",
            tiles === "raster" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"
          )}
          title="Carto 街道瓦片（需外网）"
        >
          <Layers className="size-3" />
          街道
        </button>
      </div>

      {/* 栅格瓦片版权（仅街道模式展示） */}
      {tiles === "raster" && (
        <div className="pointer-events-none absolute right-1.5 bottom-1 z-10 rounded px-1 text-[9.5px] leading-4 text-muted-foreground/80">
          © OpenStreetMap contributors © CARTO
        </div>
      )}

      {children}
    </div>
  );
}
