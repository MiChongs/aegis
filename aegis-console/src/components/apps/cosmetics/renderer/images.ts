import { joinApiUrl } from "@/lib/api/client";
import { parseDesignColor, rgbaCss } from "./color";

/**
 * 素材图片的加载与缓存（进程内共享：卡片、选择器、设计器、预览用的是同一批 Image）。
 *
 * 素材地址可能是对象存储直链，也可能是 `/api/storage/proxy/…` 这样的相对路径（按 API 基址补全）。
 * 先按 `crossOrigin=anonymous` 加载：这样画上去的 canvas 不被污染，发布时才能导出预览图；
 * 存储没开 CORS 时退回普通加载，照样能画，只是导出预览图会失败（发布流程里如实提示）。
 *
 * 图片到达时通知订阅者重绘，静态设计稿也能等到素材加载完再画最终的样子。
 */

type Entry = {
  image: HTMLImageElement;
  status: "loading" | "ready" | "error";
  /** 是否带 CORS 加载成功（导出预览图要求） */
  clean: boolean;
};

const entries = new Map<string, Entry>();
const listeners = new Set<() => void>();
const tintCache = new Map<string, HTMLCanvasElement>();

function notify() {
  for (const listener of listeners) listener();
}

export function subscribeImages(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function resolveAssetUrl(url?: string | null) {
  if (!url) return "";
  if (/^(https?:|data:|blob:)/i.test(url)) return url;
  return joinApiUrl(url);
}

function load(url: string, cors: boolean): Entry {
  const image = new Image();
  const entry: Entry = { image, status: "loading", clean: cors };
  if (cors) image.crossOrigin = "anonymous";
  image.decoding = "async";
  image.onload = () => {
    entry.status = "ready";
    notify();
  };
  image.onerror = () => {
    if (cors) {
      // 没有 CORS 头时带 crossOrigin 会直接失败：退回普通加载
      entries.set(url, load(url, false));
      return;
    }
    entry.status = "error";
    notify();
  };
  image.src = url;
  return entry;
}

/** 取一张已经加载好的图片；还在加载或失败时返回 null（并触发加载） */
export function getImage(rawUrl?: string | null): HTMLImageElement | null {
  if (typeof window === "undefined") return null;
  const url = resolveAssetUrl(rawUrl);
  if (!url) return null;
  let entry = entries.get(url);
  if (!entry) {
    entry = load(url, true);
    entries.set(url, entry);
  }
  return entry.status === "ready" ? entry.image : null;
}

/** 这张图画上去之后 canvas 还能不能导出 */
export function imageIsClean(rawUrl?: string | null) {
  const entry = entries.get(resolveAssetUrl(rawUrl));
  return !entry || entry.clean;
}

/** 图片是否都已就绪（导出预览图前等待用） */
export function imagesSettled(urls: string[]) {
  return urls.every((raw) => {
    const entry = entries.get(resolveAssetUrl(raw));
    return entry ? entry.status !== "loading" : false;
  });
}

/** 预先发起加载 */
export function preloadImages(urls: string[]) {
  for (const url of urls) getImage(url);
}

/**
 * 染色（SrcIn）：保留图片的透明度，颜色整体换成 tint。按「地址 + 颜色」缓存一张离屏图。
 */
export function tintedImage(image: HTMLImageElement, url: string, tint: string): CanvasImageSource {
  const color = parseDesignColor(tint);
  if (!color) return image;
  const key = `${url}|${tint}`;
  const hit = tintCache.get(key);
  if (hit) return hit;
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, image.naturalWidth);
  canvas.height = Math.max(1, image.naturalHeight);
  const ctx = canvas.getContext("2d");
  if (!ctx) return image;
  ctx.drawImage(image, 0, 0);
  ctx.globalCompositeOperation = "source-in";
  ctx.fillStyle = rgbaCss(color);
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  if (tintCache.size > 200) tintCache.clear();
  tintCache.set(key, canvas);
  return canvas;
}
