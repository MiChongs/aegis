"use client";

import { useCallback, useEffect, useRef } from "react";
import type { Design, NameStyle } from "@/lib/api/cosmetics";
import { cn } from "@/lib/utils";
import {
  designAnimates,
  drawNameStyle,
  measureName,
  renderDesign,
  subscribeImages,
  type AvatarPainter
} from "./renderer";

/**
 * 设计稿的实时渲染组件。卡片、选择器、预览场景、版本历史、用户详情都用它（设计器画布另有一份
 * 带变换控件的舞台，但调的是同一个 renderDesign）。
 *
 * - 时钟取 `performance.now()`：同一页上的所有装扮动画同步，改设计稿不会让动画从头来。
 * - 离开视口（IntersectionObserver）即停帧；静态设计稿只在素材到达时重绘一次。
 * - 给了 `time` 就只画那一帧（设计器拖动时间轴、导出预览图）。
 */

type Common = {
  className?: string;
  /** 固定画某一时刻（毫秒）；给出时不跑动画 */
  time?: number;
  playing?: boolean;
  title?: string;
};

function useRenderLoop(
  canvasRef: React.RefObject<HTMLCanvasElement | null>,
  animated: boolean,
  fixedTime: number | undefined,
  paint: (ctx: CanvasRenderingContext2D, elapsed: number, dpr: number) => void
) {
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    let raf = 0;
    let visible = true;
    const loop = animated && fixedTime === undefined;
    const frame = () => {
      raf = 0;
      const dpr = Math.min(3, window.devicePixelRatio || 1);
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      paint(ctx, fixedTime ?? performance.now(), dpr);
      if (loop && visible) raf = requestAnimationFrame(frame);
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(frame);
    };
    const observer =
      typeof IntersectionObserver !== "undefined"
        ? new IntersectionObserver((entries) => {
            visible = entries.some((entry) => entry.isIntersecting);
            if (visible) schedule();
          })
        : null;
    observer?.observe(canvas);
    const unsubscribe = subscribeImages(schedule);
    schedule();
    return () => {
      if (raf) cancelAnimationFrame(raf);
      observer?.disconnect();
      unsubscribe();
    };
  }, [canvasRef, animated, fixedTime, paint]);
}

/** 按 CSS 尺寸 × 设备像素比设定画布像素尺寸 */
function fitCanvas(canvas: HTMLCanvasElement, cssWidth: number, cssHeight: number, dpr: number) {
  const w = Math.max(1, Math.round(cssWidth * dpr));
  const h = Math.max(1, Math.round(cssHeight * dpr));
  if (canvas.width !== w) canvas.width = w;
  if (canvas.height !== h) canvas.height = h;
}

/**
 * 整张设计稿等比画进 width × height 的盒子（不给 height 时按画布比例）。
 */
export function DesignCanvas({
  design,
  width,
  height,
  avatarUrl,
  avatarPainter,
  hideAvatar,
  background = true,
  placeholders,
  assetUrl,
  className,
  style,
  time,
  playing = true,
  title
}: Common & {
  style?: React.CSSProperties;
  design: Design | null | undefined;
  width: number;
  height?: number;
  avatarUrl?: string | null;
  avatarPainter?: AvatarPainter;
  hideAvatar?: boolean;
  background?: boolean;
  placeholders?: boolean;
  assetUrl?: (id: number) => string | undefined;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const cw = design?.canvas?.width || 1;
  const ch = design?.canvas?.height || 1;
  const boxHeight = height ?? (width * ch) / cw;
  const scale = Math.min(width / cw, boxHeight / ch);
  const drawW = cw * scale;
  const drawH = ch * scale;
  const animated = playing && designAnimates(design);

  const paintRef = useRef<(ctx: CanvasRenderingContext2D, elapsed: number, dpr: number) => void>(() => {});
  useEffect(() => {
    paintRef.current = (ctx, elapsed, dpr) => {
      const canvas = ctx.canvas;
      fitCanvas(canvas, drawW, drawH, dpr);
      if (!design || !design.canvas?.width) return;
      const ratio = scale * dpr;
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      renderDesign(ctx, design, {
        elapsed,
        pixelRatio: ratio,
        background,
        avatar: hideAvatar ? null : { url: avatarUrl, painter: avatarPainter },
        placeholders,
        assetUrl
      });
    };
  });
  const paint = useStablePaint(paintRef);
  useRenderLoop(canvasRef, animated, time, paint);
  // 尺寸或设计稿变化时补画一帧（不在循环里的静态设计稿）
  useEffect(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx || (animated && time === undefined)) return;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    paintRef.current(ctx, time ?? performance.now(), Math.min(3, window.devicePixelRatio || 1));
  });

  return (
    <div className={cn("relative flex items-center justify-center", className)} style={{ ...style, width, height: boxHeight }} title={title}>
      <canvas ref={canvasRef} style={{ width: drawW, height: drawH }} className="block" />
    </div>
  );
}

/** 让渲染循环拿到最新的 paint 而不必每次重建循环 */
function useStablePaint(ref: React.RefObject<(ctx: CanvasRenderingContext2D, elapsed: number, dpr: number) => void>) {
  return useCallback((ctx: CanvasRenderingContext2D, elapsed: number, dpr: number) => ref.current(ctx, elapsed, dpr), [ref]);
}

/**
 * 戴着头像框的头像：头像本身是 size × size，外框按「头像尺寸 × canvas.width / avatar.size」放大并对齐头像区域，
 * 超出头像的部分溢出到盒子外（与客户端一致）。
 */
export function AvatarFrameCanvas({
  design,
  size,
  avatarUrl,
  className,
  time,
  playing
}: Common & { design: Design | null | undefined; size: number; avatarUrl?: string | null }) {
  const avatar = design?.avatar;
  if (!design || !avatar || !(avatar.size > 0)) {
    return <div className={cn("shrink-0 rounded-full bg-muted", className)} style={{ width: size, height: size }} />;
  }
  const k = size / avatar.size;
  const outer = design.canvas.width * k;
  return (
    <div className={cn("relative shrink-0", className)} style={{ width: size, height: size }}>
      <DesignCanvas
        design={design}
        width={outer}
        avatarUrl={avatarUrl}
        time={time}
        playing={playing}
        className="pointer-events-none absolute"
        style={{ left: -avatar.x * k, top: -avatar.y * k }}
      />
    </div>
  );
}

/** 昵称样式：画一行昵称，宽度按文字量出来 */
export function NameStyleCanvas({
  style,
  text,
  fontSize = 16,
  className,
  time,
  playing = true
}: Common & { style: NameStyle | null | undefined; text: string; fontSize?: number }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const pad = Math.ceil(Math.max(fontSize * 0.3, (style?.effect === "glow" ? style.glowRadius ?? 0 : 0) * 1.5));
  const textWidth = measureName(text, style, fontSize);
  const cssW = Math.ceil(textWidth + pad * 2);
  const cssH = Math.ceil(fontSize * 1.5 + pad * 2);
  const animated = playing && Boolean(style) && style?.effect !== "none" && style?.effect !== "glow";

  const paintRef = useRef<(ctx: CanvasRenderingContext2D, elapsed: number, dpr: number) => void>(() => {});
  useEffect(() => {
    paintRef.current = (ctx, elapsed, dpr) => {
      fitCanvas(ctx.canvas, cssW, cssH, dpr);
      if (!style) return;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      drawNameStyle(ctx, text, style, { x: pad, y: cssH / 2, fontSize, elapsed, pixelRatio: dpr });
    };
  });
  const paint = useStablePaint(paintRef);
  useRenderLoop(canvasRef, animated, time, paint);
  useEffect(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx || (animated && time === undefined)) return;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    paintRef.current(ctx, time ?? performance.now(), Math.min(3, window.devicePixelRatio || 1));
  });

  return (
    <canvas
      ref={canvasRef}
      className={cn("block shrink-0", className)}
      style={{ width: cssW, height: cssH, margin: `${-pad}px` }}
      aria-label={text}
    />
  );
}
