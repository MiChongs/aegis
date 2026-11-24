"use client";

import { createRef, useCallback, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { PhotoSlider } from "react-photo-view";
import type { DataType, OverlayRenderProps } from "react-photo-view/dist/types";
import screenfull from "screenfull";
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  Download,
  Expand,
  ExternalLink,
  ImageOff,
  Info,
  Maximize,
  Minimize,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  X,
  ZoomIn,
  ZoomOut
} from "lucide-react";
import { notify } from "@/lib/notify";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// 全站图片查看器，底座是 react-photo-view：从缩略图原位放大展开、手势缩放 / 拖动 / 下拉关闭、旋转。
//
// 库只负责「看」，其余全部在 overlayRender 里自己画：顶栏（序号、文件名、尺寸与大小）、
// 工具条（缩放、旋转、轮播、复制、下载、新窗口、全屏）、左右切换、底部缩略图条。
// 键盘也自己接（window 捕获阶段）：查看器常从 Radix 抽屉 / 弹窗里打开，
// 不抢在前面的话 Esc 会连同底下的抽屉一起关掉。
//
// 三种用法：
//   <LightboxOverlay open slides index onClose />  受控浮层（头像、存储文件、任意按钮触发）
//   <ImageGrid images />                           缩略图网格 + 浮层（工单附件）
//   <ImageLightbox src alt />                      带预览框的单图卡片（品牌设置）

export type LightboxSlide = {
  src: string;
  alt?: string;
  /** 底部说明文字 */
  description?: string;
  /** 顶栏标题与下载文件名，缺省取 alt */
  fileName?: string;
  /** 字节数，顶栏显示 */
  size?: number;
  /** 下载地址，缺省用 src */
  downloadUrl?: string;
  /** 打开时从这个元素原位放大，关闭时缩回去 */
  origin?: RefObject<HTMLElement | null>;
};

type LightboxOverlayProps = {
  open: boolean;
  onClose: () => void;
  /** 至少一张 */
  slides: LightboxSlide[];
  enableDownload?: boolean;
  /** 打开时定位到第几张 */
  index?: number;
};

const MIN_SCALE = 1;
const MAX_SCALE = 8;
const SLIDESHOW_MS = 3500;

export function LightboxOverlay({ open, onClose, slides, enableDownload = true, index = 0 }: LightboxOverlayProps) {
  const [current, setCurrent] = useState(index);
  // 每次打开都回到调用方指定的那一张（渲染期调整，不走 effect）
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setCurrent(Math.min(Math.max(index, 0), Math.max(slides.length - 1, 0)));
  }

  const images = useMemo<DataType[]>(
    () =>
      slides.map((slide, i) => ({
        key: `${i}:${slide.src}`,
        src: slide.src,
        originRef: slide.origin as DataType["originRef"]
      })),
    [slides]
  );

  if (slides.length === 0) return null;

  return (
    <PhotoSlider
      images={images}
      visible={open}
      index={current}
      onIndexChange={setCurrent}
      onClose={() => onClose()}
      className="aegis-photo"
      bannerVisible={false}
      maskOpacity={1}
      loop={slides.length > 2}
      speed={() => 320}
      easing={(type) => (type === 2 ? "cubic-bezier(0.36, 0, 0.66, -0.56)" : "cubic-bezier(0.22, 1, 0.36, 1)")}
      loadingElement={<PhotoLoading />}
      brokenElement={(params: { src: string }) => <PhotoBroken src={params.src} />}
      overlayRender={(props) => <PhotoChrome {...props} slides={slides} enableDownload={enableDownload} />}
    />
  );
}

// ─────────────── 浮层控件 ───────────────

type ChromeProps = OverlayRenderProps & { slides: LightboxSlide[]; enableDownload: boolean };

function PhotoChrome({
  index,
  onIndexChange,
  onClose,
  visible,
  rotate,
  onRotate,
  scale,
  onScale,
  slides,
  enableDownload
}: ChromeProps) {
  const total = slides.length;
  const slide = slides[index] ?? slides[0];
  const title = slide.fileName || slide.alt || "图片";
  const [playing, setPlaying] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const dims = useNaturalSize(slide.src);
  const rootRef = useRef<HTMLDivElement>(null);

  const go = useCallback(
    (delta: number) => {
      if (total < 2) return;
      const next = index + delta;
      // 两张以内不循环（库的 loop 阈值是 3），到头就停
      if (total <= 2) {
        if (next >= 0 && next < total) onIndexChange(next);
        return;
      }
      onIndexChange((next + total) % total);
    },
    [index, onIndexChange, total]
  );
  const zoom = useCallback(
    (factor: number) => onScale(Math.min(MAX_SCALE, Math.max(MIN_SCALE, Number((scale * factor).toFixed(2))))),
    [onScale, scale]
  );
  const turn = useCallback((deg: number) => onRotate(rotate + deg), [onRotate, rotate]);
  const reset = useCallback(() => {
    onScale(1);
    onRotate(0);
  }, [onRotate, onScale]);

  const toggleFullscreen = useCallback(() => {
    if (!screenfull.isEnabled) return;
    const portal = rootRef.current?.closest(".PhotoView-Portal");
    void screenfull.toggle((portal as HTMLElement | null) ?? document.documentElement);
  }, []);

  useEffect(() => {
    if (!screenfull.isEnabled) return;
    const sync = () => setFullscreen(screenfull.isFullscreen);
    screenfull.on("change", sync);
    return () => screenfull.off("change", sync);
  }, []);

  // 关掉查看器时退出全屏、停轮播
  useEffect(() => {
    if (visible) return;
    if (screenfull.isEnabled && screenfull.isFullscreen) void screenfull.exit();
  }, [visible]);
  const slideshow = playing && visible && total > 1;

  useEffect(() => {
    if (!slideshow) return;
    const timer = window.setInterval(() => onIndexChange((index + 1) % total), SLIDESHOW_MS);
    return () => window.clearInterval(timer);
  }, [index, onIndexChange, slideshow, total]);

  // 键盘：捕获阶段先拿到，处理过的键不再往下传（底下的 Radix 抽屉与库自带的监听都收不到）
  const live = useRef({ go, zoom, turn, reset, toggleFullscreen, onClose, total });
  useEffect(() => {
    live.current = { go, zoom, turn, reset, toggleFullscreen, onClose, total };
  });
  useEffect(() => {
    if (!visible) return;
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
      const k = live.current;
      let handled = true;
      switch (event.key) {
        case "Escape":
          k.onClose();
          break;
        case "ArrowLeft":
          k.go(-1);
          break;
        case "ArrowRight":
          k.go(1);
          break;
        case "+":
        case "=":
          k.zoom(1.5);
          break;
        case "-":
        case "_":
          k.zoom(1 / 1.5);
          break;
        case "0":
          k.reset();
          break;
        case "r":
        case "R":
          k.turn(event.shiftKey ? -90 : 90);
          break;
        case "f":
        case "F":
          k.toggleFullscreen();
          break;
        case " ":
          if (k.total > 1) setPlaying((value) => !value);
          break;
        case "i":
        case "I":
          setInfoOpen((value) => !value);
          break;
        default:
          handled = false;
      }
      if (handled) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [visible]);

  const download = async () => {
    const url = slide.downloadUrl || slide.src;
    try {
      const response = await fetch(url);
      if (!response.ok) throw new Error(String(response.status));
      const blob = await response.blob();
      const href = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = href;
      link.download = downloadName(title, blob.type);
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(href), 1000);
    } catch {
      // 跨域或限时链接失效时退回新窗口，让浏览器自己处理
      window.open(url, "_blank", "noopener,noreferrer");
    }
  };

  const copy = async () => {
    try {
      await copyImageToClipboard(slide.src);
      notify.success("已复制图片");
    } catch {
      try {
        await navigator.clipboard.writeText(slide.src);
        notify.info("浏览器不允许复制图片，已复制图片地址");
      } catch {
        notify.error("复制失败");
      }
    }
  };

  const meta = [dims ? `${dims.w} × ${dims.h}` : null, slide.size ? formatSize(slide.size) : null].filter(Boolean);
  const transformed = scale !== 1 || rotate % 360 !== 0;

  return (
    <div ref={rootRef} className="aegis-photo__chrome pointer-events-none absolute inset-0 z-20 text-white">
      {/* 顶栏：左边是这张图是什么，右边只留关闭 */}
      <div className="aegis-photo__top pointer-events-auto absolute inset-x-0 top-0 flex items-center gap-3 px-4 pb-12 pt-4 sm:px-6">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          {total > 1 ? (
            <span className="shrink-0 rounded-full bg-white/12 px-2.5 py-1 font-mono text-[11px] font-medium tabular-nums text-white ring-1 ring-white/10">
              {index + 1}
              <span className="text-white/45"> / {total}</span>
            </span>
          ) : null}
          <div className="min-w-0">
            <p className="truncate text-sm font-medium leading-5">{title}</p>
            {meta.length ? <p className="truncate font-mono text-[11px] leading-4 text-white/55">{meta.join("  ")}</p> : null}
          </div>
        </div>
        <button
          type="button"
          onClick={() => onClose()}
          title="关闭（Esc）"
          aria-label="关闭"
          className="inline-flex size-10 shrink-0 items-center justify-center rounded-full bg-white/10 text-white/85 ring-1 ring-white/12 backdrop-blur-xl transition-[background-color,color,transform] hover:bg-white/20 hover:text-white active:scale-90"
        >
          <X className="size-[18px]" />
        </button>
      </div>

      {/* 图片信息 */}
      {infoOpen ? (
        <div className="pointer-events-auto absolute right-4 top-20 w-72 overflow-hidden rounded-2xl bg-zinc-900/80 text-xs shadow-2xl ring-1 ring-white/10 backdrop-blur-xl sm:right-6">
          <div className="flex items-center gap-2 border-b border-white/8 px-4 py-3">
            <Info className="size-3.5 text-white/60" />
            <span className="text-[13px] font-medium">图片信息</span>
          </div>
          <dl className="grid grid-cols-[4rem_1fr] gap-x-3 gap-y-2 px-4 py-3">
            <dt className="text-white/50">名称</dt>
            <dd className="break-all">{title}</dd>
            <dt className="text-white/50">尺寸</dt>
            <dd className="font-mono">{dims ? `${dims.w} × ${dims.h}` : "读取中"}</dd>
            {slide.size ? (
              <>
                <dt className="text-white/50">大小</dt>
                <dd className="font-mono">{formatSize(slide.size)}</dd>
              </>
            ) : null}
            <dt className="text-white/50">缩放</dt>
            <dd className="font-mono">{Math.round(scale * 100)}%</dd>
            <dt className="text-white/50">旋转</dt>
            <dd className="font-mono">{((rotate % 360) + 360) % 360}°</dd>
          </dl>
          <div className="grid grid-cols-2 gap-x-3 gap-y-1.5 border-t border-white/8 bg-white/[0.03] px-4 py-3 text-[11px] text-white/55">
            {SHORTCUTS.map(([key, label]) => (
              <span key={key} className="flex items-center gap-1.5">
                <kbd className="min-w-5 rounded bg-white/10 px-1 py-px text-center font-mono text-[10px] text-white/80 ring-1 ring-white/10">{key}</kbd>
                {label}
              </span>
            ))}
          </div>
          <p className="border-t border-white/8 px-4 py-2.5 text-[11px] leading-relaxed text-white/45">滚轮或双指缩放，拖动平移，双击放大。</p>
        </div>
      ) : null}

      {/* 左右切换 */}
      {total > 1 ? (
        <>
          <NavButton side="left" onClick={() => go(-1)} disabled={total <= 2 && index === 0} />
          <NavButton side="right" onClick={() => go(1)} disabled={total <= 2 && index === total - 1} />
        </>
      ) : null}

      {/* 底部：说明、胶片条、工具坞 */}
      <div className="aegis-photo__bottom pointer-events-none absolute inset-x-0 bottom-0 flex flex-col items-center gap-3 px-4 pb-5 pt-16">
        {slide.description && slide.description !== title ? (
          <p className="pointer-events-auto max-w-2xl truncate rounded-full bg-black/35 px-3 py-1 text-center text-xs text-white/80 backdrop-blur-md">
            {slide.description}
          </p>
        ) : null}
        {total > 1 ? <ThumbStrip slides={slides} index={index} onSelect={onIndexChange} /> : null}

        <div className="pointer-events-auto relative flex max-w-full items-center gap-0.5 overflow-hidden rounded-2xl bg-zinc-900/70 p-1.5 shadow-[0_16px_48px_-12px_rgb(0_0_0/0.7)] ring-1 ring-white/10 backdrop-blur-xl">
          <ChromeButton label="缩小（-）" onClick={() => zoom(1 / 1.5)} disabled={scale <= MIN_SCALE}>
            <ZoomOut />
          </ChromeButton>
          <button
            type="button"
            onClick={reset}
            title="还原（0）"
            className={cn(
              "h-9 min-w-14 shrink-0 rounded-xl px-2 font-mono text-xs tabular-nums transition-colors hover:bg-white/12",
              transformed ? "text-white" : "text-white/55"
            )}
          >
            {Math.round(scale * 100)}%
          </button>
          <ChromeButton label="放大（+）" onClick={() => zoom(1.5)} disabled={scale >= MAX_SCALE}>
            <ZoomIn />
          </ChromeButton>
          <ChromeDivider />
          <ChromeButton label="向左旋转（Shift+R）" onClick={() => turn(-90)} className="hidden sm:inline-flex">
            <RotateCcw />
          </ChromeButton>
          <ChromeButton label="向右旋转（R）" onClick={() => turn(90)}>
            <RotateCw />
          </ChromeButton>
          {total > 1 ? (
            <ChromeButton label={playing ? "暂停轮播（空格）" : "轮播（空格）"} onClick={() => setPlaying((value) => !value)} active={playing}>
              {playing ? <Pause /> : <Play />}
            </ChromeButton>
          ) : null}
          <ChromeDivider />
          <ChromeButton label="复制图片" onClick={() => void copy()} className="hidden sm:inline-flex">
            <Copy />
          </ChromeButton>
          {enableDownload ? (
            <ChromeButton label="下载" onClick={() => void download()}>
              <Download />
            </ChromeButton>
          ) : null}
          <ChromeButton
            label="新窗口打开"
            onClick={() => window.open(slide.src, "_blank", "noopener,noreferrer")}
            className="hidden sm:inline-flex"
          >
            <ExternalLink />
          </ChromeButton>
          <ChromeDivider className="hidden sm:block" />
          <ChromeButton label="图片信息（I）" onClick={() => setInfoOpen((value) => !value)} active={infoOpen} className="hidden sm:inline-flex">
            <Info />
          </ChromeButton>
          {screenfull.isEnabled ? (
            <ChromeButton label={fullscreen ? "退出全屏（F）" : "全屏（F）"} onClick={toggleFullscreen} className="hidden sm:inline-flex">
              {fullscreen ? <Minimize /> : <Maximize />}
            </ChromeButton>
          ) : null}

          {/* 轮播进度贴在工具坞底边 */}
          {slideshow ? (
            <span
              key={index}
              aria-hidden
              className="absolute inset-x-3 bottom-0 h-0.5 origin-left rounded-full bg-white/80"
              style={{ animation: `aegis-photo-progress ${SLIDESHOW_MS}ms linear forwards` }}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

const SHORTCUTS: [string, string][] = [
  ["← →", "切换"],
  ["+ −", "缩放"],
  ["0", "还原"],
  ["R", "旋转"],
  ["F", "全屏"],
  ["Esc", "关闭"]
];

function ChromeButton({
  label,
  onClick,
  disabled,
  active,
  className,
  children
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  active?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "inline-flex size-9 shrink-0 items-center justify-center rounded-xl text-white/75 transition-[background-color,color,transform] hover:bg-white/12 hover:text-white active:scale-90 disabled:pointer-events-none disabled:opacity-30 [&_svg]:size-[18px]",
        active && "bg-white/90 text-zinc-900 hover:bg-white hover:text-zinc-900",
        className
      )}
    >
      {children}
    </button>
  );
}

function ChromeDivider({ className }: { className?: string }) {
  return <span aria-hidden className={cn("mx-1 h-5 w-px shrink-0 bg-white/12", className)} />;
}

function NavButton({ side, onClick, disabled }: { side: "left" | "right"; onClick: () => void; disabled?: boolean }) {
  const Icon = side === "left" ? ChevronLeft : ChevronRight;
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={side === "left" ? "上一张" : "下一张"}
      className={cn(
        "pointer-events-auto absolute top-1/2 hidden size-12 -translate-y-1/2 items-center justify-center rounded-full bg-zinc-900/55 text-white/85 shadow-xl ring-1 ring-white/10 backdrop-blur-xl transition-[background-color,color,opacity] hover:bg-zinc-800/80 hover:text-white disabled:pointer-events-none disabled:opacity-0 sm:inline-flex",
        side === "left" ? "left-4 sm:left-6" : "right-4 sm:right-6"
      )}
    >
      <Icon className="size-6" />
    </button>
  );
}

function ThumbStrip({ slides, index, onSelect }: { slides: LightboxSlide[]; index: number; onSelect: (index: number) => void }) {
  const stripRef = useRef<HTMLDivElement>(null);
  // 只滚胶片条自己。不能用 scrollIntoView：它会连带滚动所有可滚祖先，
  // 包括查看器 overflow: hidden 的根节点，整个查看器被推向一侧、右边露出底下的页面。
  useEffect(() => {
    const strip = stripRef.current;
    const el = strip?.children[index] as HTMLElement | undefined;
    if (!strip || !el) return;
    strip.scrollTo({ left: el.offsetLeft - (strip.clientWidth - el.offsetWidth) / 2, behavior: "smooth" });
  }, [index]);
  return (
    <div
      ref={stripRef}
      className="pointer-events-auto relative flex max-w-[min(100%,44rem)] gap-2 overflow-x-auto overscroll-contain p-2 [scrollbar-width:none]"
    >
      {slides.map((slide, i) => (
        <button
          key={`${i}:${slide.src}`}
          type="button"
          onClick={() => onSelect(i)}
          aria-label={`第 ${i + 1} 张`}
          aria-current={i === index}
          className={cn(
            "relative h-14 w-11 shrink-0 overflow-hidden rounded-lg bg-white/5 transition-[opacity,transform,box-shadow] duration-200 sm:h-16 sm:w-12",
            i === index
              ? "-translate-y-0.5 opacity-100 shadow-[0_0_0_2px_rgb(255_255_255),0_8px_20px_-6px_rgb(0_0_0/0.8)]"
              : "opacity-40 ring-1 ring-white/10 hover:opacity-80"
          )}
        >
          {/* eslint-disable-next-line @next/next/no-img-element -- 同源代理与外链地址，不走图像优化器 */}
          <img src={slide.src} alt="" loading="lazy" decoding="async" draggable={false} className="size-full object-cover" />
        </button>
      ))}
    </div>
  );
}

function PhotoLoading() {
  return (
    <div className="flex flex-col items-center gap-3 text-white/70">
      <span className="relative block size-11">
        <span className="absolute inset-0 rounded-full border-2 border-white/12" />
        <span className="absolute inset-0 animate-spin rounded-full border-2 border-transparent border-t-white/90 [animation-duration:0.8s]" />
        <span className="absolute inset-2 animate-spin rounded-full border-2 border-transparent border-b-white/40 [animation-direction:reverse] [animation-duration:1.2s]" />
      </span>
      <span className="text-xs tracking-wide">正在加载</span>
    </div>
  );
}

function PhotoBroken({ src }: { src: string }) {
  return (
    <div className="flex w-72 flex-col items-center gap-3 rounded-2xl bg-white/5 px-6 py-8 text-center text-white ring-1 ring-white/10 backdrop-blur-md">
      <span className="flex size-12 items-center justify-center rounded-full bg-white/10">
        <ImageOff className="size-5 text-white/80" />
      </span>
      <div className="space-y-1">
        <p className="text-sm font-medium">图片加载失败</p>
        <p className="text-xs text-white/55">链接可能已过期，或文件已被删除</p>
      </div>
      <a
        href={src}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1.5 rounded-full bg-white/12 px-3 py-1.5 text-xs transition-colors hover:bg-white/20"
      >
        <ExternalLink className="size-3.5" />
        新窗口打开
      </a>
    </div>
  );
}

// ─────────────── 缩略图 ───────────────

type ImageThumbProps = {
  src: string;
  alt: string;
  onOpen: () => void;
  /** 供查看器原位展开 */
  thumbRef?: RefObject<HTMLButtonElement | null>;
  className?: string;
  /** 右下角角标，如「+3」 */
  badge?: ReactNode;
  title?: string;
};

/** 可点开的缩略图：加载时扫光，加载完淡入，失败时给出占位。 */
export function ImageThumb({ src, alt, onOpen, thumbRef, className, badge, title }: ImageThumbProps) {
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  return (
    <button
      ref={thumbRef}
      type="button"
      onClick={onOpen}
      title={title ?? alt}
      className={cn(
        "group/thumb relative overflow-hidden rounded-lg border bg-muted outline-none transition-[border-color,box-shadow] hover:border-primary/40 focus-visible:ring-2 focus-visible:ring-ring",
        state === "loading" && "aegis-photo-shimmer",
        className
      )}
    >
      {state === "error" ? (
        <span className="absolute inset-0 flex flex-col items-center justify-center gap-1 text-muted-foreground">
          <ImageOff className="size-4" />
          <span className="text-[10px]">加载失败</span>
        </span>
      ) : null}
      {/* eslint-disable-next-line @next/next/no-img-element -- 同源代理与外链地址，不走图像优化器 */}
      <img
        src={src}
        alt={alt}
        loading="lazy"
        decoding="async"
        draggable={false}
        onLoad={() => setState("ready")}
        onError={() => setState("error")}
        className={cn(
          "size-full object-cover transition-[opacity,transform] duration-300 group-hover/thumb:scale-[1.04]",
          state === "ready" ? "opacity-100" : "opacity-0"
        )}
      />
      {state === "ready" ? (
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/0 text-white opacity-0 transition-[background-color,opacity] group-hover/thumb:bg-black/25 group-hover/thumb:opacity-100">
          <Expand className="size-4 drop-shadow" />
        </span>
      ) : null}
      {badge ? (
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/45 text-sm font-semibold text-white">
          {badge}
        </span>
      ) : null}
    </button>
  );
}

/** 缩略图网格 + 查看器。max 之外的收进最后一格的「+N」。 */
export function ImageGrid({
  images,
  thumbClassName = "size-20",
  max,
  className,
  enableDownload = true
}: {
  images: LightboxSlide[];
  thumbClassName?: string;
  max?: number;
  className?: string;
  enableDownload?: boolean;
}) {
  const [openAt, setOpenAt] = useState<number | null>(null);
  const visible = max && images.length > max ? images.slice(0, max) : images;
  const hidden = images.length - visible.length;

  // 每格一个原位展开的锚点；收进「+N」的那几张从最后一格展开
  const origins = useMemo(
    () => visible.map(() => createRef<HTMLButtonElement>()),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 只随格数变化
    [visible.length]
  );
  const slides = useMemo(
    () => images.map((image, i) => ({ ...image, origin: origins[Math.min(i, origins.length - 1)] })),
    [images, origins]
  );

  if (images.length === 0) return null;
  return (
    <div className={cn("flex flex-wrap gap-2", className)}>
      {visible.map((image, i) => (
        <ImageThumb
          key={`${i}:${image.src}`}
          src={image.src}
          alt={image.alt ?? image.fileName ?? ""}
          title={image.fileName ? `${image.fileName}${image.size ? `（${formatSize(image.size)}）` : ""}` : undefined}
          thumbRef={origins[i]}
          onOpen={() => setOpenAt(i)}
          className={thumbClassName}
          badge={hidden > 0 && i === visible.length - 1 ? `+${hidden + 1}` : undefined}
        />
      ))}
      <LightboxOverlay
        open={openAt !== null}
        index={openAt ?? 0}
        onClose={() => setOpenAt(null)}
        slides={slides}
        enableDownload={enableDownload}
      />
    </div>
  );
}

// ─────────────── 单图预览卡片 ───────────────

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

/** 带预览框的单图卡片：棋盘格底（透明图看得出边界）、悬停提示、尺寸角标、大图 / 新窗口 / 下载。 */
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
  enableDownload = true
}: ImageLightboxProps) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [loadedSrc, setLoadedSrc] = useState(src);
  if (loadedSrc !== src) {
    setLoadedSrc(src);
    setState("loading");
  }
  const usable = isUsableSrc(src);
  const imageRef = useRef<HTMLImageElement>(null);
  const dims = useNaturalSize(usable && state === "ready" ? src : undefined);
  const slides = useMemo(
    () => (usable ? [{ src, alt, description: caption, fileName: caption || alt, origin: imageRef }] : []),
    [alt, caption, src, usable]
  );

  return (
    <div className={cn("overflow-hidden rounded-xl border bg-card", className, previewClassName)}>
      <button
        type="button"
        onClick={() => usable && state !== "error" && setOpen(true)}
        disabled={!usable}
        aria-label={usable ? `查看大图：${alt}` : emptyLabel}
        className="group/frame block w-full text-left outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:cursor-not-allowed"
      >
        <div
          className={cn(
            "aegis-checkerboard relative flex min-h-40 items-center justify-center overflow-hidden border-b px-4 py-5",
            usable && state === "loading" && "aegis-photo-shimmer",
            frameClassName
          )}
        >
          {usable ? (
            <>
              {state === "error" ? (
                <div className="flex flex-col items-center gap-2 text-muted-foreground">
                  <ImageOff className="size-7 opacity-70" />
                  <span className="text-xs">图片加载失败</span>
                </div>
              ) : null}
              {/* eslint-disable-next-line @next/next/no-img-element -- 同源代理与外链地址，不走图像优化器 */}
              <img
                ref={imageRef}
                src={src}
                alt={alt}
                onLoad={() => setState("ready")}
                onError={() => setState("error")}
                className={cn(
                  "relative z-10 max-h-56 w-auto max-w-full rounded-md object-contain drop-shadow-sm transition-[opacity,transform] duration-300 group-hover/frame:scale-[1.015]",
                  state === "ready" ? "opacity-100" : "opacity-0",
                  state === "error" && "hidden",
                  imageClassName
                )}
              />
              {state === "ready" ? (
                <>
                  <span className="pointer-events-none absolute right-3 top-3 z-20 inline-flex items-center gap-1 rounded-full bg-background/90 px-2.5 py-1 text-[10px] font-medium text-muted-foreground opacity-0 shadow-sm ring-1 ring-border backdrop-blur transition-opacity group-hover/frame:opacity-100">
                    <Expand className="size-3" />
                    查看大图
                  </span>
                  {dims ? (
                    <span className="pointer-events-none absolute bottom-2 left-2 z-20 rounded-md bg-background/85 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground ring-1 ring-border backdrop-blur">
                      {dims.w} × {dims.h}
                    </span>
                  ) : null}
                </>
              ) : null}
            </>
          ) : (
            <div className="flex flex-col items-center gap-2 text-muted-foreground">
              <ImageOff className="size-8 opacity-70" />
              <span className="text-xs">{emptyLabel}</span>
            </div>
          )}
        </div>
      </button>

      {caption || hint ? (
        <div className="space-y-1 border-b px-4 py-3">
          {caption ? <p className="text-sm font-medium text-foreground">{caption}</p> : null}
          {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-1.5 bg-muted/20 px-3 py-2.5">
        <Button type="button" variant="outline" size="sm" disabled={!usable || state === "error"} onClick={() => setOpen(true)}>
          <Expand className="size-3.5" />
          查看大图
        </Button>
        {usable && showOpenInNew ? (
          <Button asChild variant="ghost" size="sm">
            <a href={src} target="_blank" rel="noreferrer">
              <ExternalLink className="size-3.5" />
              新窗口
            </a>
          </Button>
        ) : null}
      </div>

      {usable ? (
        <LightboxOverlay open={open} onClose={() => setOpen(false)} slides={slides} enableDownload={enableDownload} />
      ) : null}
    </div>
  );
}

// ─────────────── 工具 ───────────────

const sizeCache = new Map<string, { w: number; h: number }>();

/** 图片原始尺寸。浏览器已缓存的图会立刻返回，不会多下一遍。 */
function useNaturalSize(src?: string) {
  const [dims, setDims] = useState<{ src: string; w: number; h: number } | null>(null);
  useEffect(() => {
    if (!src || sizeCache.has(src)) return;
    let cancelled = false;
    const img = new Image();
    img.onload = () => {
      const value = { w: img.naturalWidth, h: img.naturalHeight };
      sizeCache.set(src, value);
      if (!cancelled) setDims({ src, ...value });
    };
    img.src = src;
    return () => {
      cancelled = true;
    };
  }, [src]);
  if (!src) return null;
  const cached = sizeCache.get(src);
  if (cached) return cached;
  return dims && dims.src === src ? { w: dims.w, h: dims.h } : null;
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

function downloadName(name: string, mime: string) {
  if (/\.[a-z0-9]{2,5}$/i.test(name)) return name;
  const ext = mime.split("/")[1]?.replace("jpeg", "jpg").replace("svg+xml", "svg");
  return ext ? `${name}.${ext}` : name;
}

/** 剪贴板只稳定支持 PNG：其余格式先画到画布上再转 */
async function copyImageToClipboard(src: string) {
  if (typeof ClipboardItem === "undefined" || !navigator.clipboard?.write) throw new Error("unsupported");
  const response = await fetch(src);
  if (!response.ok) throw new Error(String(response.status));
  let blob = await response.blob();
  if (blob.type !== "image/png") {
    const bitmap = await createImageBitmap(blob);
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    canvas.getContext("2d")?.drawImage(bitmap, 0, 0);
    blob = await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob((value) => (value ? resolve(value) : reject(new Error("encode"))), "image/png")
    );
  }
  await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
}
