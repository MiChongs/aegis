"use client";

// MapLibre GL 主题底图 —— 多供应商底座
//
// 设计要点：
//   - **本地矢量简图恒在最底层**：/geo/world-countries.geo.json 的国界轮廓，
//     零外部依赖。任何一家在线供应商被墙、超时、密钥失效时，透出来的是一张
//     配色正确的世界地图，而不是纯黑画布。这也是「离线」这一档的全部内容。
//   - **在线瓦片由目录驱动**：供应商是什么坐标系、有没有深色版、注记什么语言、
//     要不要密钥，全部写在 lib/geo/map-providers.ts 一处（见那里的说明）。
//   - **默认供应商跟随浏览器语言**，使用者可随时锁定某一家，选择对全站生效。
//   - **GCJ-02 供应商会纠偏**：偏的是瓦片，业务数据一个坐标都不动
//     （见 lib/geo/gcj02-tiles.ts）。
//
// 三条实现约束：
//   1. 换供应商 / 换主题只增删 `aegis-tiles-*` 图层，**永远不调 setStyle**：
//      业务面板在 onMapReady 里挂的 source / layer 一旦被 setStyle 清掉，
//      热力、围栏、轨迹会整层消失而地图看起来完全正常。
//   2. 新瓦片层一律插在**第一个业务图层之前**，否则底图会盖住数据。
//   3. 就绪信号取 styledata 而非 load —— load 要等瓦片下载完，
//      瓦片被墙时它可能永远不来，业务图层就一条都挂不上。

import { ReactNode, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
// maplibre-gl v6 起不再提供 default export，改用命名空间导入
import * as maplibregl from "maplibre-gl";
import type {
  Map as MapLibreInstance,
  RasterLayerSpecification,
  RasterSourceSpecification,
  StyleSpecification
} from "maplibre-gl";
import { useTheme } from "next-themes";
import { ShieldCheck, TriangleAlert } from "lucide-react";

import { cn } from "@/lib/utils";
import { MapProviderPicker } from "@/components/maps/map-provider-picker";
import { useMapProvider } from "@/lib/geo/map-provider-store";
import { OFFLINE_PROVIDER, providerKey, type MapTileLayer } from "@/lib/geo/map-providers";
import {
  GCJ02_MIN_ZOOM,
  GCJ02_RELEASE_ZOOM,
  isGcj02CorrectionUnsupported,
  onGcj02CorrectionUnsupported,
  registerGcj02Protocol,
  wrapGcj02Tiles
} from "@/lib/geo/gcj02-tiles";

/** 与 globals.css 的 zinc 令牌严格对齐（layer paint 需要具体色值） */
const PALETTE = {
  light: { ocean: "#f4f4f5", land: "#ffffff", boundary: "#e4e4e7" },
  dark: { ocean: "#09090b", land: "#18181b", boundary: "#27272a" }
} as const;

const BASE_LAYER_IDS = ["aegis-base-bg", "aegis-base-land", "aegis-base-boundary"];
const TILE_PREFIX = "aegis-tiles-";
/** 连续这么多次瓦片错误就判定这家不可达，收起在线图层只留本地简图 */
const TILE_FAILURE_LIMIT = 8;

function buildBaseStyle(dark: boolean): StyleSpecification {
  const palette = dark ? PALETTE.dark : PALETTE.light;
  return {
    version: 8,
    sources: {
      "aegis-world": { type: "geojson", data: "/geo/world-countries.geo.json" }
    },
    layers: [
      { id: "aegis-base-bg", type: "background", paint: { "background-color": palette.ocean } },
      { id: "aegis-base-land", type: "fill", source: "aegis-world", paint: { "fill-color": palette.land } },
      {
        id: "aegis-base-boundary",
        type: "line",
        source: "aegis-world",
        paint: { "line-color": palette.boundary, "line-width": 0.8 }
      }
    ]
  };
}

/** 主题变化时原地更新矢量底衬配色，不触碰任何其它图层 */
function applyBaseTheme(map: MapLibreInstance, dark: boolean) {
  const palette = dark ? PALETTE.dark : PALETTE.light;
  map.setPaintProperty("aegis-base-bg", "background-color", palette.ocean);
  map.setPaintProperty("aegis-base-land", "fill-color", palette.land);
  map.setPaintProperty("aegis-base-boundary", "line-color", palette.boundary);
}

/**
 * 深色近似：供应商没有原生深色版时，用栅格 paint 把亮底图压成暗调。
 *
 * 这只是近似 —— 栅格没有反相能力，压暗后的街道图是「黄昏」而不是真正的暗色图。
 * 但在深色控制台里放一张纯白地图会直接晃眼，两害相权取其轻，
 * 并且选择器里那些标了原生深色的供应商随时可以换过去。
 */
function tilePaint(dim: boolean): RasterLayerSpecification["paint"] {
  if (!dim) return { "raster-fade-duration": 150 };
  return {
    "raster-fade-duration": 150,
    "raster-brightness-max": 0.52,
    "raster-saturation": -0.22,
    "raster-contrast": 0.06
  };
}

type TileOptions = { corrected: boolean; dim: boolean };

/** 重挂在线瓦片层：先清空既有的，再插到第一个业务图层之前 */
function applyTiles(map: MapLibreInstance, layers: MapTileLayer[], { corrected, dim }: TileOptions) {
  const current = map.getStyle();
  for (const layer of current.layers) {
    if (layer.id.startsWith(TILE_PREFIX) && map.getLayer(layer.id)) map.removeLayer(layer.id);
  }
  for (const sourceId of Object.keys(current.sources)) {
    if (sourceId.startsWith(TILE_PREFIX) && map.getSource(sourceId)) map.removeSource(sourceId);
  }
  if (layers.length === 0) return;

  // 清空之后，第一个不属于底衬的图层就是业务面板挂上来的那一层
  const beforeId = map.getStyle().layers.find((layer) => !BASE_LAYER_IDS.includes(layer.id))?.id;

  layers.forEach((layer, index) => {
    const id = `${TILE_PREFIX}${index}-${layer.key}`;
    const source: RasterSourceSpecification = {
      type: "raster",
      tiles: corrected ? [wrapGcj02Tiles(layer.tiles, layer.scheme)] : layer.tiles,
      tileSize: layer.tileSize,
      maxzoom: layer.maxZoom
    };
    // 纠偏模式下 y 轴翻转由协议内部处理：交给 maplibre 的话，
    // 送到纠偏逻辑手上的已经是翻转过的 y，算出来的地理位置会整体错掉
    if (layer.scheme && !corrected) source.scheme = layer.scheme;
    map.addSource(id, source);
    map.addLayer({ id, type: "raster", source: id, paint: tilePaint(dim) }, beforeId);
  });
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

  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";
  const { provider, lang } = useMapProvider();

  // 纠偏能力是全局结论（某一家没开 CORS，五个面板都一样），订阅模块级状态
  const correctionUnsupported = useSyncExternalStore(
    onGcj02CorrectionUnsupported,
    isGcj02CorrectionUnsupported,
    () => false
  );

  // 缩放跨过阈值才纠偏：低层级偏移不足半个像素，白白付出画布合成的代价。
  // 初值由初始视野决定，之后只在 zoomend 里改 —— 不在 effect 里直接置状态。
  const [zoomHigh, setZoomHigh] = useState(() => zoom >= GCJ02_MIN_ZOOM);
  const corrected = provider.datum === "gcj02" && zoomHigh && !correctionUnsupported;

  const layers = useMemo(
    () => provider.build({ theme: dark ? "dark" : "light", lang, key: providerKey(provider) }),
    [provider, dark, lang]
  );

  // 失败结论绑定在「这套配置」上：换供应商 / 换主题即自动作废，
  // 不需要再写一个把状态清空的 effect（那既多一次渲染，也过不了 set-state-in-effect）
  const signature = `${provider.id}|${dark ? "dark" : "light"}|${lang}|${corrected ? "fix" : "raw"}`;
  const [failedSignature, setFailedSignature] = useState<string | null>(null);
  const unreachable = failedSignature === signature;

  // 初始值固定进 ref，保证地图只创建一次
  const initialRef = useRef({ center, zoom, minZoom, maxZoom, dark });
  const onMapReadyRef = useRef(onMapReady);
  useEffect(() => {
    onMapReadyRef.current = onMapReady;
  }, [onMapReady]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    registerGcj02Protocol();

    const init = initialRef.current;
    const map = new maplibregl.Map({
      container,
      style: buildBaseStyle(init.dark),
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

    // 就绪 = style 解析完（可以加 source / layer / overlay），**不是** load。
    //
    // load 要等到"所有必要资源下载完且首帧渲染完成"，其中包含瓦片：
    // 在线瓦片若被墙或超时，load 可能永远不来，业务面板的图层就一条都挂不上去
    //（地图看着正常，数据层整体消失）。styledata 只表示样式本身就绪，
    // 正是这里需要的语义。
    //
    // 三个入口都收敛到同一个只发一次的通知：监听挂上时可能已经就绪
    //（HMR / 严格模式重挂载），load 作为最后兜底。
    let notified = false;
    const notifyReady = () => {
      if (notified) return;
      notified = true;
      setReady(true);
      onMapReadyRef.current?.(map);
    };
    if (map.isStyleLoaded()) notifyReady();
    else {
      map.once("styledata", notifyReady);
      map.once("load", notifyReady);
    }

    const ro = new ResizeObserver(() => map.resize());
    ro.observe(container);

    return () => {
      ro.disconnect();
      setReady(false);
      mapRef.current = null;
      map.remove();
    };
  }, []);

  // 主题切换：原地更新矢量底衬配色
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !ready) return;
    applyBaseTheme(map, dark);
  }, [dark, ready]);

  // 供应商 / 主题 / 语言 / 纠偏状态任一变化都重挂在线瓦片层
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !ready) return;
    applyTiles(map, unreachable ? [] : layers, { corrected, dim: dark && !provider.nativeDark });
  }, [ready, layers, corrected, dark, provider, unreachable]);

  // 缩放阈值（带滞回，避免在阈值上反复横跳导致瓦片抖动）
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !ready) return;
    const onZoomEnd = () => {
      const level = map.getZoom();
      setZoomHigh((prev) => (prev ? level >= GCJ02_RELEASE_ZOOM : level >= GCJ02_MIN_ZOOM));
    };
    map.on("zoomend", onZoomEnd);
    return () => {
      map.off("zoomend", onZoomEnd);
    };
  }, [ready]);

  // 瓦片错误计数：只统计在线底图自己的错误，业务 source 的错误与这里无关
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !ready) return;
    let failures = 0;
    const onError = (event: unknown) => {
      const sourceId = (event as { sourceId?: string }).sourceId;
      if (!sourceId?.startsWith(TILE_PREFIX)) return;
      // 纠偏刚被判定不可用时会成片报错，但结论已经有了：
      // 等这一轮重挂成原始瓦片即可，不该顺带把整家供应商判死
      if (isGcj02CorrectionUnsupported() && corrected) return;
      failures += 1;
      if (failures >= TILE_FAILURE_LIMIT) setFailedSignature(signature);
    };
    map.on("error", onError);
    return () => {
      map.off("error", onError);
    };
  }, [ready, signature, corrected]);

  const notice = useMemo(() => {
    if (unreachable) {
      return {
        tone: "danger" as const,
        text: `${provider.short}瓦片不可达`,
        title: "在线瓦片连续加载失败，已退回本地矢量简图；可在左侧切换其它供应商"
      };
    }
    if (provider.datum === "gcj02" && zoomHigh && correctionUnsupported) {
      return {
        tone: "warn" as const,
        text: "未纠偏 · 约 500m",
        title: "该瓦片服务未开放跨域读取，无法纠偏；底图与数据点存在 GCJ-02 固有偏移"
      };
    }
    if (corrected) {
      return {
        tone: "ok" as const,
        text: "GCJ-02 已纠偏",
        title: "底图瓦片已按 GCJ-02 偏移量重新对齐到 WGS-84，与数据点位置一致"
      };
    }
    return null;
  }, [unreachable, provider, zoomHigh, correctionUnsupported, corrected]);

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

      {/* 供应商切换 + 当前底图状态 */}
      <div className="absolute bottom-3 left-3 z-10 flex items-center gap-1.5">
        <MapProviderPicker />
        {notice && (
          <button
            type="button"
            // 不可达往往是一次网络抖动。没有这个入口的话，同一家供应商
            // 在本次会话里就再也回不来了 —— 重选一遍等于没选（偏好没变化）
            disabled={notice.tone !== "danger"}
            onClick={() => setFailedSignature(null)}
            title={notice.tone === "danger" ? `${notice.title}；点此重试` : notice.title}
            className={cn(
              "flex items-center gap-1 rounded-lg border px-1.5 py-1 text-[10px] font-medium backdrop-blur-sm",
              notice.tone === "danger" &&
                "cursor-pointer border-destructive/40 bg-destructive/10 text-destructive hover:bg-destructive/20",
              notice.tone === "warn" &&
                "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400",
              notice.tone === "ok" &&
                "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
            )}
          >
            {notice.tone === "ok" ? <ShieldCheck className="size-3" /> : <TriangleAlert className="size-3" />}
            {notice.text}
          </button>
        )}
      </div>

      {/* 版权署名：用了谁的瓦片就署谁的名，这是各家服务条款的硬性要求。
          回退到本地简图时署的也必须跟着换，否则会署上一家根本没被渲染的名字。 */}
      <div className="pointer-events-none absolute right-1.5 bottom-1 z-10 max-w-[60%] truncate rounded px-1 text-right text-[9.5px] leading-4 text-muted-foreground/80">
        {unreachable || layers.length === 0 ? OFFLINE_PROVIDER.attribution : provider.attribution}
      </div>

      {children}
    </div>
  );
}
