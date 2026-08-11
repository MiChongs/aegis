"use client";

import { useMemo, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Download as DownloadIcon,
  Expand,
  ExternalLink,
  ImageOff,
  Loader2,
  Maximize,
  Minimize,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import Lightbox from "yet-another-react-lightbox";
import Download from "yet-another-react-lightbox/plugins/download";
import Fullscreen from "yet-another-react-lightbox/plugins/fullscreen";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ */
/*  共享 Lightbox 浮层 — Avatar / ImageLightbox 等统一使用              */
/* ------------------------------------------------------------------ */

type LightboxOverlayProps = {
  open: boolean;
  onClose: () => void;
  /** 至少一张幻灯片 */
  slides: Array<{ src: string; alt?: string; description?: string }>;
  enableDownload?: boolean;
};

const zoomConfig = {
  maxZoomPixelRatio: 4,
  zoomInMultiplier: 2,
  doubleTapDelay: 250,
  keyboardMoveDistance: 40,
  wheelZoomDistanceFactor: 180,
} as const;

const animationConfig = { fade: 200, swipe: 250 } as const;

/* Lucide 图标统一 size — 匹配 --yarl__icon_size: 1rem */
const ico = "size-4";

/** 纯 Lightbox 浮层，无额外 UI，可在任意组件中复用 */
export function LightboxOverlay({ open, onClose, slides, enableDownload = true }: LightboxOverlayProps) {
  const plugins = useMemo(
    () => (enableDownload ? [Fullscreen, Zoom, Download] : [Fullscreen, Zoom]),
    [enableDownload],
  );
  const single = slides.length <= 1;

  const render = useMemo(
    () => ({
      /* Lucide 图标替换 YARL 默认 SVG — 全站图标一致 */
      iconClose: () => <X className={ico} />,
      iconZoomIn: () => <ZoomIn className={ico} />,
      iconZoomOut: () => <ZoomOut className={ico} />,
      iconEnterFullscreen: () => <Maximize className={ico} />,
      iconExitFullscreen: () => <Minimize className={ico} />,
      iconDownload: () => <DownloadIcon className={ico} />,
      iconPrev: () => <ArrowLeft className={ico} />,
      iconNext: () => <ArrowRight className={ico} />,
      iconLoading: () => <Loader2 className="size-5 animate-spin text-muted-foreground" />,
      iconError: () => <ImageOff className="size-5 text-muted-foreground" />,
      buttonPrev: single ? () => null : undefined,
      buttonNext: single ? () => null : undefined,
    }),
    [single],
  );

  return (
    <Lightbox
      open={open}
      close={onClose}
      slides={slides}
      plugins={plugins}
      carousel={{ finite: true }}
      animation={animationConfig}
      controller={{ closeOnBackdropClick: true }}
      render={render}
      zoom={zoomConfig}
    />
  );
}

/* ------------------------------------------------------------------ */
/*  ImageLightbox — 带预览卡片的图片灯箱                                 */
/* ------------------------------------------------------------------ */

type ImageLightboxProps = {
  src?: string | null;
  alt: string;
  caption?: string;
  hint?: string;
  className?: string;
  previewClassName?: string;
  frameClassName?: string;
  imageClassName?: string;
  emptyLabel?: string;
  showOpenInNew?: boolean;
  enableDownload?: boolean;
};

function isUsableSrc(src?: string | null): src is string {
  return typeof src === "string" && src.trim().length > 0;
}

export function ImageLightbox({
  src,
  alt,
  caption,
  hint,
  className,
  previewClassName,
  frameClassName,
  imageClassName,
  emptyLabel = "暂无图片",
  showOpenInNew = true,
  enableDownload = true,
}: ImageLightboxProps) {
  const [open, setOpen] = useState(false);
  const usable = isUsableSrc(src);
  const slides = useMemo(() => (usable ? [{ src, alt, description: caption }] : []), [alt, caption, src, usable]);

  return (
    <div className={cn("overflow-hidden rounded-2xl border bg-card", className, previewClassName)}>
      <button
        type="button"
        onClick={() => usable && setOpen(true)}
        disabled={!usable}
        className="group block w-full text-left transition-colors hover:bg-accent/20 disabled:cursor-not-allowed disabled:opacity-75"
      >
        <div
          className={cn(
            "relative flex min-h-40 items-center justify-center border-b bg-muted/30 px-4 py-5",
            frameClassName,
          )}
        >
          {usable ? (
            <>
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={src}
                alt={alt}
                className={cn(
                  "relative z-10 max-h-56 w-auto max-w-full rounded-xl object-contain transition-transform duration-200 group-hover:scale-[1.01]",
                  imageClassName,
                )}
              />
              <div className="pointer-events-none absolute right-3 top-3 inline-flex items-center gap-1 rounded-full border bg-background/90 px-2.5 py-1 text-[10px] font-medium text-muted-foreground backdrop-blur">
                <ZoomIn className="size-3" />
                预览
              </div>
            </>
          ) : (
            <div className="flex flex-col items-center gap-2 text-muted-foreground">
              <ImageOff className="size-8 opacity-70" />
              <span className="text-xs">{emptyLabel}</span>
            </div>
          )}
        </div>
      </button>

      {(caption || hint) && (
        <div className="space-y-1 border-b px-4 py-3">
          {caption ? <p className="text-sm font-medium text-foreground">{caption}</p> : null}
          {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 bg-background/60 px-4 py-3">
        <Button type="button" variant="outline" size="sm" disabled={!usable} onClick={() => setOpen(true)}>
          <Expand className="size-3.5" />
          大图预览
        </Button>
        {usable && showOpenInNew ? (
          <Button asChild variant="ghost" size="sm">
            <a href={src} target="_blank" rel="noreferrer">
              <ExternalLink className="size-3.5" />
              新开窗口
            </a>
          </Button>
        ) : null}
      </div>

      {usable ? (
        <LightboxOverlay
          open={open}
          onClose={() => setOpen(false)}
          slides={slides}
          enableDownload={enableDownload}
        />
      ) : null}
    </div>
  );
}
