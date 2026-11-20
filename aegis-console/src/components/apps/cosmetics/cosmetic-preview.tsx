"use client";

import { useEffect, useRef, useState } from "react";
import { ImageOff } from "lucide-react";
import type { CosmeticItem, CosmeticSlot, Design } from "@/lib/api/cosmetics";
import { useCosmeticItemQuery } from "@/lib/cosmetic-hooks";
import { cn } from "@/lib/utils";
import { DesignCanvas, NameStyleCanvas } from "./cosmetic-canvas";
import { resolveAssetUrl } from "./renderer";

/**
 * 一件装扮的缩略预览：进入视口才去取单件（列表接口不带设计稿），取到后用渲染器实时播放
 * 已发布的版本（没发布过就画草稿）；取到之前先显示静态预览图。
 */
export function CosmeticItemPreview({
  appKey,
  item,
  width,
  height,
  prefer = "published",
  nameText,
  className
}: {
  appKey: string;
  item: Pick<CosmeticItem, "id" | "slot" | "name" | "preview">;
  width: number;
  height: number;
  /** 画已发布的版本还是草稿 */
  prefer?: "published" | "draft";
  nameText?: string;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el || seen) return;
    if (typeof IntersectionObserver === "undefined") {
      const timer = window.setTimeout(() => setSeen(true), 0);
      return () => window.clearTimeout(timer);
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setSeen(true);
      },
      { rootMargin: "120px" }
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [seen]);

  const detail = useCosmeticItemQuery(appKey, item.id, seen);
  const design =
    prefer === "draft"
      ? (detail.data?.draft ?? detail.data?.design ?? null)
      : (detail.data?.design ?? detail.data?.draft ?? null);

  return (
    <div ref={ref} className={cn("flex items-center justify-center overflow-hidden", className)} style={{ width, height }}>
      {design ? (
        <SlotDesign slot={item.slot} design={design} width={width} height={height} nameText={nameText ?? item.name} />
      ) : item.preview ? (
        // eslint-disable-next-line @next/next/no-img-element -- 存储代理地址，不经 next/image 优化
        <img src={resolveAssetUrl(item.preview)} alt={item.name} className="max-h-full max-w-full object-contain" />
      ) : detail.isError ? (
        <ImageOff className="size-5 text-muted-foreground" />
      ) : (
        <span className="size-1/2 animate-pulse rounded-full bg-muted" />
      )}
    </div>
  );
}

/** 按槽位画一份设计稿：昵称样式画一行字，其余画整张画布 */
export function SlotDesign({
  slot,
  design,
  width,
  height,
  nameText = "昵称样式",
  fontSize,
  time,
  playing,
  avatarUrl,
  assetUrl
}: {
  slot: CosmeticSlot;
  design: Design;
  width: number;
  height: number;
  nameText?: string;
  fontSize?: number;
  time?: number;
  playing?: boolean;
  avatarUrl?: string | null;
  assetUrl?: (id: number) => string | undefined;
}) {
  if (slot === "name_style") {
    const size = fontSize ?? Math.max(12, Math.min(28, Math.round(height / 3.2)));
    return <NameStyleCanvas style={design.nameStyle} text={nameText} fontSize={size} time={time} playing={playing} />;
  }
  return (
    <DesignCanvas
      design={design}
      width={width}
      height={height}
      time={time}
      playing={playing}
      avatarUrl={avatarUrl}
      assetUrl={assetUrl}
    />
  );
}
