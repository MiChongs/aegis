"use client";

import { useEffect, useState } from "react";
import { ImageIcon } from "lucide-react";
import type { CosmeticAsset } from "@/lib/api/cosmetics";
import { cn } from "@/lib/utils";
import { resolveAssetUrl } from "./renderer";

/**
 * 工坊零件的两种可视化：序列帧按切分逐帧播放（列表缩略图与编辑弹窗共用）、整张网格图上叠切分线
 * （编辑弹窗里核对行列与帧数）。帧的取法与渲染器一致：第 i 帧在第 i % columns 列、第 ⌊i / columns⌋ 行。
 */

type Cut = { columns: number; rows: number; frameCount: number; fps: number };

/** 把 width × height 的东西等比放进 boxW × boxH */
function fit(width: number, height: number, boxW: number, boxH: number) {
  if (width <= 0 || height <= 0) return { w: boxW, h: boxH };
  const scale = Math.min(boxW / width, boxH / height);
  return { w: Math.max(1, Math.round(width * scale)), h: Math.max(1, Math.round(height * scale)) };
}

export function SpriteFrames({
  asset,
  cut,
  size,
  playing = true,
  className
}: {
  asset: Pick<CosmeticAsset, "url" | "width" | "height" | "name">;
  cut: Cut;
  size: number;
  playing?: boolean;
  className?: string;
}) {
  const columns = Math.max(1, cut.columns);
  const rows = Math.max(1, cut.rows);
  const frames = Math.max(1, Math.min(cut.frameCount || columns * rows, columns * rows));
  const fps = cut.fps > 0 ? Math.min(60, cut.fps) : 12;
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    if (!playing || frames <= 1) return;
    const timer = window.setInterval(() => setFrame((value) => (value + 1) % frames), 1000 / fps);
    return () => window.clearInterval(timer);
  }, [playing, frames, fps]);

  const shown = playing ? frame % frames : 0;
  const col = shown % columns;
  const row = Math.floor(shown / columns);
  const box = fit(asset.width / columns, asset.height / rows, size, size);

  return (
    <div className={cn("flex items-center justify-center", className)} style={{ width: size, height: size }}>
      {asset.url ? (
        <div
          role="img"
          aria-label={asset.name}
          style={{
            width: box.w,
            height: box.h,
            backgroundImage: `url("${resolveAssetUrl(asset.url)}")`,
            backgroundSize: `${columns * 100}% ${rows * 100}%`,
            backgroundPosition: `${columns > 1 ? (col / (columns - 1)) * 100 : 0}% ${rows > 1 ? (row / (rows - 1)) * 100 : 0}%`,
            backgroundRepeat: "no-repeat"
          }}
        />
      ) : (
        <ImageIcon className="size-5 text-muted-foreground" />
      )}
    </div>
  );
}

/** 整张网格图 + 切分线；超出帧数的格子压暗 */
export function SpriteGrid({
  asset,
  cut,
  maxWidth,
  maxHeight,
  className
}: {
  asset: Pick<CosmeticAsset, "url" | "width" | "height" | "name">;
  cut: Cut;
  maxWidth: number;
  maxHeight: number;
  className?: string;
}) {
  const columns = Math.max(1, Math.min(cut.columns || 1, 64));
  const rows = Math.max(1, Math.min(cut.rows || 1, 64));
  const frames = cut.frameCount > 0 ? cut.frameCount : columns * rows;
  const box = fit(asset.width, asset.height, maxWidth, maxHeight);
  return (
    <div className={cn("relative shrink-0 overflow-hidden rounded-md border bg-muted/40", className)} style={{ width: box.w, height: box.h }}>
      {asset.url ? (
        // eslint-disable-next-line @next/next/no-img-element -- 存储代理地址，不经 next/image 优化
        <img src={resolveAssetUrl(asset.url)} alt={asset.name} className="absolute inset-0 size-full object-fill" />
      ) : null}
      <div
        className="absolute inset-0 grid"
        style={{ gridTemplateColumns: `repeat(${columns}, 1fr)`, gridTemplateRows: `repeat(${rows}, 1fr)` }}
        aria-hidden
      >
        {Array.from({ length: columns * rows }, (_, index) => (
          <span
            key={index}
            className={cn("border-[0.5px] border-primary/60", index >= frames && "bg-background/70")}
          />
        ))}
      </div>
    </div>
  );
}
