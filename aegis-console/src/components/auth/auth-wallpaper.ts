/**
 * 登录页壁纸的取图与下载（浏览器端）。
 *
 * 必应的 `th?id=…_UHD.jpg` 支持 `w / h / rs=1 / c=4 / qlt` 现场缩放，同一张图按屏幕像素取三档：
 *
 *   1920×1080 ≈ 0.55MB    2560×1440 ≈ 0.95MB    3840×2160 ≈ 2.3MB（qlt=90，2026-11 实测）
 *
 * 档位按「铺满视口要多少像素」算：16:9 的图按 object-cover 铺满 W×H，需要的宽度是
 * max(W, H·16/9)·dpr —— 竖屏手机因此也会落到高档，不然裁切后会糊。dpr 封顶 2，开了省流量就只取 1920。
 *
 * 另取一张 480×270 的小图（约 22KB）先垫在底下做模糊占位，高清图到了再盖上去。
 */

export type WallpaperSources = { preview: string; full: string; width: number };

const TIERS = [1920, 2560, 3840] as const;

export function wallpaperSources(base: string): WallpaperSources {
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  const { width: screenW, height: screenH } = window.screen;
  const need = Math.max(screenW, (screenH * 16) / 9) * dpr;
  const saveData = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData;
  const width = saveData ? TIERS[0] : (TIERS.find((tier) => tier >= need) ?? TIERS[TIERS.length - 1]);
  const height = Math.round((width * 9) / 16);
  return {
    preview: `${base}&w=480&h=270&rs=1&c=4`,
    full: `${base}&w=${width}&h=${height}&rs=1&c=4&qlt=90`,
    width
  };
}

/** 本次会话里已经下好的图：地址 → blob URL。登录与注册来回切时不再重新下载、不再播加载动画。 */
const loaded = new Map<string, string>();
let introPlayed = false;

export const wallpaperCache = {
  get: (url: string) => loaded.get(url),
  get introPlayed() {
    return introPlayed;
  },
  markIntroPlayed() {
    introPlayed = true;
  }
};

/**
 * 下载并解码一张图，边下边报进度（0–1；拿不到总长时报 null，界面转为不定进度）。
 *
 * 用 fetch 读流而不是 <img>：<img> 没有进度事件。读完转成 blob URL 再 `decode()`，
 * 保证交给界面时已经解码完毕 —— 否则淡入的第一帧会卡一下，4K 图尤其明显。
 * fetch 失败（跨域策略变了、被拦）时退回普通 <img> 加载，只是没有进度。
 */
export async function loadWallpaper(
  url: string,
  onProgress: (value: number | null) => void,
  signal: AbortSignal
): Promise<string> {
  const cached = loaded.get(url);
  if (cached) {
    // 先让出一拍：调用方在 effect 里同步调用，缓存命中时也不在 effect 体内同步改状态
    await Promise.resolve();
    onProgress(1);
    return cached;
  }

  let src: string;
  try {
    const response = await fetch(url, { signal, mode: "cors", credentials: "omit" });
    if (!response.ok || !response.body) throw new Error(`wallpaper ${response.status}`);
    const total = Number(response.headers.get("content-length")) || 0;
    const reader = response.body.getReader();
    const chunks: BlobPart[] = [];
    let received = 0;
    onProgress(total ? 0 : null);
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push(value);
      received += value.byteLength;
      if (total) onProgress(Math.min(received / total, 0.99));
    }
    src = URL.createObjectURL(new Blob(chunks, { type: response.headers.get("content-type") || "image/jpeg" }));
  } catch (error) {
    if (signal.aborted) throw error;
    onProgress(null);
    src = url;
  }

  const image = new Image();
  image.decoding = "async";
  image.src = src;
  await image.decode();
  if (signal.aborted) throw new DOMException("aborted", "AbortError");
  loaded.set(url, src);
  onProgress(1);
  return src;
}

/** 预取小图占位：失败就算了，不影响高清图 */
export function loadPreview(url: string): Promise<string | null> {
  return new Promise((resolve) => {
    const image = new Image();
    image.onload = () => resolve(url);
    image.onerror = () => resolve(null);
    image.src = url;
  });
}
