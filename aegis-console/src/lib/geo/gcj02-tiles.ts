"use client";

// GCJ-02 瓦片纠偏 —— 偏的是底图，不是数据
//
// 中国大陆的地图服务只提供 GCJ-02 偏移后的瓦片，而平台里所有坐标
// （GeoIP、围栏、轨迹）都是 WGS-84，直接叠加会有 300–700 米的系统性错位。
//
// 有两种修法，这里选了后者：
//
//   A. 把数据转成 GCJ-02 再画。要在五个面板、约四十处取坐标的地方各转一次，
//      围栏面板还得在鼠标点击时反向转回去。漏掉任何一处都不会报错，
//      只是那一层悄悄错位 —— 典型的会随时间腐烂的实现。
//   B. 把瓦片搬回 WGS-84 位置。纠偏只发生在瓦片管线这一个地方，
//      业务层完全不知道底图换过坐标系，「库里的坐标只有 WGS-84 一种」
//      这条不变式得以保持。
//
// 做法：注册一个 maplibre 自定义协议。请求某张 WGS-84 瓦片时，算出它的中心点
// 在 GCJ-02 下落到源瓦片网格的哪个位置，取来覆盖该范围的 1–4 张源瓦片，
// 按偏移量画进画布再交回去。偏移在一张瓦片内的变化量远小于一个像素，
// 因此整片平移的近似是准确的。
//
// 两条边界条件让开销可以忽略：
//   - 境外（outOfChina）本来就没有偏移，原样透传
//   - z < GCJ02_MIN_ZOOM 时偏移不足半个像素，肉眼与鼠标都分辨不出来
// 控制台的默认视野是全球，也就是说绝大多数时候这段代码根本不会被执行。

import * as maplibregl from "maplibre-gl";

import { outOfChina, wgs84ToGcj02 } from "@/lib/geo/datum";

export const GCJ02_PROTOCOL = "gcj02";

/** 低于该层级时偏移不足半个像素，纠偏没有意义（也不必承担跨域读像素的风险） */
export const GCJ02_MIN_ZOOM = 8;
/** 退回原始瓦片的层级，比启用阈值低半级：避免在阈值上反复横跳导致瓦片抖动 */
export const GCJ02_RELEASE_ZOOM = 7.5;

type TileConfig = { t: string[]; tms?: 1 };

/**
 * 把一组原始瓦片模板包装成纠偏地址。
 *
 * 子域名不交给 maplibre 的 `tiles` 数组轮转，而是整组塞进配置里由本模块按
 * **源瓦片坐标**取模选择：相邻的输出瓦片会请求到重叠的源瓦片，只有让同一张
 * 源瓦片始终落在同一个子域名上，浏览器 HTTP 缓存才命中得了。
 *
 * TMS（腾讯那种纵轴自下而上的服务）也在这里处理，不设置 source 的 `scheme` ——
 * 否则 maplibre 会先把 y 翻转再交给我们，而纠偏计算需要的是 XYZ 的 y。
 */
export function wrapGcj02Tiles(tiles: string[], scheme?: "tms"): string {
  const cfg: TileConfig = scheme === "tms" ? { t: tiles, tms: 1 } : { t: tiles };
  // 配置整体 URL 编码：其中的 `{z}` 变成 `%7Bz%7D`，不会被 maplibre 误当成占位符替换
  return `${GCJ02_PROTOCOL}://tile/{z}/{x}/{y}?cfg=${encodeURIComponent(JSON.stringify(cfg))}`;
}

// ──────────────────────────────────────
// 能力探测：跨域读像素失败时全局降级
// ──────────────────────────────────────

let unsupported = false;
const unsupportedListeners = new Set<() => void>();

/** 纠偏是否已被判定为不可用（瓦片服务没开 CORS，读不到像素） */
export function isGcj02CorrectionUnsupported(): boolean {
  return unsupported;
}

export function onGcj02CorrectionUnsupported(listener: () => void): () => void {
  unsupportedListeners.add(listener);
  return () => {
    unsupportedListeners.delete(listener);
  };
}

function markUnsupported() {
  if (unsupported) return;
  unsupported = true;
  for (const listener of unsupportedListeners) listener();
}

// ──────────────────────────────────────
// 墨卡托瓦片网格
// ──────────────────────────────────────

function lngToTileX(lng: number, scale: number): number {
  return ((lng + 180) / 360) * scale;
}

function latToTileY(lat: number, scale: number): number {
  const s = Math.min(0.9999, Math.max(-0.9999, Math.sin((lat * Math.PI) / 180)));
  return (0.5 - Math.log((1 + s) / (1 - s)) / (4 * Math.PI)) * scale;
}

function tileXToLng(x: number, scale: number): number {
  return (x / scale) * 360 - 180;
}

function tileYToLat(y: number, scale: number): number {
  const n = Math.PI - (2 * Math.PI * y) / scale;
  return (180 / Math.PI) * Math.atan(Math.sinh(n));
}

function sourceTileUrl(cfg: TileConfig, z: number, x: number, y: number): string {
  const scale = 2 ** z;
  const wrappedX = ((x % scale) + scale) % scale;
  const outY = cfg.tms ? scale - 1 - y : y;
  const template = cfg.t[(wrappedX + y) % cfg.t.length];
  return template
    .replace(/\{z\}/g, String(z))
    .replace(/\{x\}/g, String(wrappedX))
    .replace(/\{y\}/g, String(outY));
}

// ──────────────────────────────────────
// 协议实现
// ──────────────────────────────────────

const URL_PATTERN = /^gcj02:\/\/tile\/(\d+)\/(-?\d+)\/(-?\d+)\?cfg=(.*)$/;

/**
 * 取一张源瓦片的字节。
 *
 * 跨域读像素被拒（服务端没开 CORS）表现为 `TypeError`，这是一次就能定性的事实：
 * 立刻让全站退回未纠偏显示，不必每张瓦片都白跑一遍。
 * 中断（AbortError）与 4xx/5xx 都不算 —— 它们只说明这一张没取到。
 */
async function fetchTile(url: string, signal: AbortSignal): Promise<ArrayBuffer> {
  try {
    const response = await fetch(url, { signal, mode: "cors", credentials: "omit" });
    if (!response.ok) throw new Error(`瓦片请求失败：HTTP ${response.status}`);
    return await response.arrayBuffer();
  } catch (error) {
    if ((error as Error)?.name === "TypeError") markUnsupported();
    throw error;
  }
}

/**
 * 画布交回位图。
 *
 * maplibre 的自定义协议除了原始字节，也直接收 ImageBitmap
 *（见其 ImageRequest.doImageRequest 对 isImageBitmap 的分支），
 * 因此这里不必编码成 PNG 再让它解回来 —— 每张瓦片省一轮编解码。
 * OffscreenCanvas 上的 transferToImageBitmap 更是零拷贝。
 */
async function canvasToBitmap(canvas: OffscreenCanvas | HTMLCanvasElement): Promise<ImageBitmap> {
  if (typeof OffscreenCanvas !== "undefined" && canvas instanceof OffscreenCanvas) {
    return canvas.transferToImageBitmap();
  }
  return createImageBitmap(canvas as HTMLCanvasElement);
}

type Composited = { canvas: OffscreenCanvas | HTMLCanvasElement; ctx: CanvasRenderingContext2D };

function createCanvas(size: number): Composited {
  if (typeof OffscreenCanvas !== "undefined") {
    const canvas = new OffscreenCanvas(size, size);
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("画布上下文不可用");
    // OffscreenCanvas 与 HTMLCanvasElement 的 2D 上下文在本模块用到的
    // drawImage 一支上签名完全一致，统一按后者处理省掉一层联合类型分支
    return { canvas, ctx: ctx as unknown as CanvasRenderingContext2D };
  }
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("画布上下文不可用");
  return { canvas, ctx };
}

async function loadCorrectedTile(
  cfg: TileConfig,
  z: number,
  x: number,
  y: number,
  signal: AbortSignal
): Promise<ImageBitmap> {
  const scale = 2 ** z;

  // 该输出瓦片中心（WGS-84）在源网格（GCJ-02）里的位置，两者之差即整片平移量
  const centerLng = tileXToLng(x + 0.5, scale);
  const centerLat = tileYToLat(y + 0.5, scale);
  const [shiftedLng, shiftedLat] = wgs84ToGcj02(centerLng, centerLat);
  const originX = x + (lngToTileX(shiftedLng, scale) - (x + 0.5));
  const originY = y + (latToTileY(shiftedLat, scale) - (y + 0.5));

  // 覆盖 [originX, originX+1] × [originY, originY+1] 的源瓦片，最多 2×2 张
  const requests: Array<{ i: number; j: number }> = [];
  for (let i = Math.floor(originX); i <= Math.ceil(originX + 1) - 1; i++) {
    for (
      let j = Math.max(0, Math.floor(originY));
      j <= Math.min(scale - 1, Math.ceil(originY + 1) - 1);
      j++
    ) {
      requests.push({ i, j });
    }
  }

  const settled = await Promise.allSettled(
    requests.map(async ({ i, j }) => ({
      i,
      j,
      bitmap: await createImageBitmap(new Blob([await fetchTile(sourceTileUrl(cfg, z, i, j), signal)]))
    }))
  );
  const pieces = settled.flatMap((item) => (item.status === "fulfilled" ? [item.value] : []));

  if (pieces.length === 0) {
    const rejected = settled.find((item) => item.status === "rejected") as PromiseRejectedResult | undefined;
    throw (rejected?.reason as Error | undefined) ?? new Error("瓦片不可用");
  }

  const size = pieces[0].bitmap.width;
  const { canvas, ctx } = createCanvas(size);
  for (const { i, j, bitmap } of pieces) {
    ctx.drawImage(bitmap, (i - originX) * size, (j - originY) * size, size, size);
    bitmap.close();
  }

  return canvasToBitmap(canvas);
}

let registered = false;

/** 幂等注册；地图组件在创建实例前调用 */
export function registerGcj02Protocol() {
  if (registered) return;
  registered = true;

  maplibregl.addProtocol(GCJ02_PROTOCOL, async (params, abortController) => {
    const matched = URL_PATTERN.exec(params.url);
    if (!matched) throw new Error(`无法解析的纠偏瓦片地址：${params.url}`);

    const z = Number(matched[1]);
    const x = Number(matched[2]);
    const y = Number(matched[3]);
    const cfg = JSON.parse(decodeURIComponent(matched[4])) as TileConfig;
    const signal = abortController.signal;

    const scale = 2 ** z;
    const centerLng = tileXToLng(x + 0.5, scale);
    const centerLat = tileYToLat(y + 0.5, scale);

    // 境外无偏移、纠偏已被判定不可用：两种情况都退化为原样取回
    if (unsupported || outOfChina(centerLng, centerLat)) {
      return { data: await fetchTile(sourceTileUrl(cfg, z, x, y), signal) };
    }

    return { data: await loadCorrectedTile(cfg, z, x, y, signal) };
  });
}
