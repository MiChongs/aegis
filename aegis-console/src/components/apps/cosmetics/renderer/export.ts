import type { CosmeticSlot, Design } from "@/lib/api/cosmetics";
import { drawNameStyle, measureName, renderDesign } from "./draw";
import { imagesSettled, preloadImages } from "./images";

/**
 * 用离屏 canvas 把设计稿的某一帧渲染成 PNG（发布时上传的静态预览图）。
 * 先等素材加载（最多 4 秒）；存储没开 CORS 时 canvas 被污染，toBlob 抛 SecurityError，由调用方提示。
 */

function assetUrls(design: Design) {
  return Object.values(design.assets ?? {})
    .map((ref) => ref.url ?? "")
    .filter(Boolean);
}

async function waitForImages(urls: string[], timeout = 4000) {
  preloadImages(urls);
  const start = Date.now();
  while (!imagesSettled(urls) && Date.now() - start < timeout) {
    await new Promise((resolve) => setTimeout(resolve, 80));
  }
}

function toBlob(canvas: HTMLCanvasElement) {
  return new Promise<Blob>((resolve, reject) => {
    try {
      canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error("导出预览图失败"))), "image/png");
    } catch (error) {
      reject(error);
    }
  });
}

export async function renderPreviewPng(
  design: Design,
  slot: CosmeticSlot,
  options: { time: number; elapsed: number; nameText: string; maxBytes: number }
): Promise<Blob> {
  await waitForImages(assetUrls(design));
  let target = 512;
  for (let attempt = 0; attempt < 4; attempt++) {
    const canvas = document.createElement("canvas");
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("浏览器不支持画布");
    if (slot === "name_style") {
      const fontSize = Math.round(target / 8);
      const width = Math.ceil(measureName(options.nameText, design.nameStyle, fontSize) + fontSize * 1.2);
      canvas.width = width;
      canvas.height = Math.ceil(fontSize * 2);
      if (design.nameStyle) {
        drawNameStyle(ctx, options.nameText, design.nameStyle, {
          x: fontSize * 0.6,
          y: canvas.height / 2,
          fontSize,
          elapsed: options.elapsed,
          pixelRatio: 1
        });
      }
    } else {
      const cw = design.canvas.width || 1;
      const ch = design.canvas.height || 1;
      const scale = Math.min(2, target / Math.max(cw, ch));
      canvas.width = Math.max(1, Math.round(cw * scale));
      canvas.height = Math.max(1, Math.round(ch * scale));
      ctx.setTransform(scale, 0, 0, scale, 0, 0);
      renderDesign(ctx, design, { elapsed: options.elapsed, time: options.time, pixelRatio: scale, avatar: null });
    }
    const blob = await toBlob(canvas);
    if (blob.size <= options.maxBytes) return blob;
    target = Math.round(target * 0.7);
  }
  throw new Error("预览图超过大小上限");
}
