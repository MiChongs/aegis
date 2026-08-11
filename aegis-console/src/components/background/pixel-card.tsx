"use client";

import { forwardRef, useEffect, useImperativeHandle, useRef, type CSSProperties, type ReactNode } from "react";
import "./pixel-card.css";

// ─────────────────────────────────────────────────────────────────────
//  PixelCard — 像素阵列动画组件（2D Canvas，基于 React Bits 源码移植）。
//
//  与上游组件差异：
//   1) 增加 fullscreen 模式：覆盖整个视口，用作全站进入动画层。
//   2) 暴露命令式句柄 appear / disappear，便于父组件（IntroOverlay）控制
//      动画时序，而不是仅依赖 hover / focus 触发。
//   3) 默认渲染为 card 模式（与上游一致），fullscreen 模式需显式开启。
// ─────────────────────────────────────────────────────────────────────

type PixelVariant = "default" | "blue" | "yellow" | "pink" | "brand";

type VariantConfig = {
  activeColor: string | null;
  gap: number;
  speed: number;
  colors: string;
  noFocus: boolean;
};

const VARIANTS: Record<PixelVariant, VariantConfig> = {
  default: {
    activeColor: null,
    gap: 5,
    speed: 35,
    colors: "#f8fafc,#f1f5f9,#cbd5e1",
    noFocus: false,
  },
  blue: {
    activeColor: "#e0f2fe",
    gap: 10,
    speed: 25,
    colors: "#e0f2fe,#7dd3fc,#0ea5e9",
    noFocus: false,
  },
  yellow: {
    activeColor: "#fef08a",
    gap: 3,
    speed: 20,
    colors: "#fef08a,#fde047,#eab308",
    noFocus: false,
  },
  pink: {
    activeColor: "#fecdd3",
    gap: 6,
    speed: 80,
    colors: "#fecdd3,#fda4af,#e11d48",
    noFocus: true,
  },
  // Aegis 品牌色：Terracotta + Sand，用于全屏进入动画
  brand: {
    activeColor: "#d97757",
    gap: 6,
    speed: 50,
    colors: "#d97757,#c96442,#f5f4ed,#b0aea5",
    noFocus: true,
  },
};

class Pixel {
  ctx: CanvasRenderingContext2D;
  x: number;
  y: number;
  color: string;
  speed: number;
  size: number;
  sizeStep: number;
  minSize: number;
  maxSizeInteger: number;
  maxSize: number;
  delay: number;
  counter: number;
  counterStep: number;
  isIdle: boolean;
  isReverse: boolean;
  isShimmer: boolean;

  constructor(
    canvas: HTMLCanvasElement,
    context: CanvasRenderingContext2D,
    x: number,
    y: number,
    color: string,
    speed: number,
    delay: number,
  ) {
    this.ctx = context;
    this.x = x;
    this.y = y;
    this.color = color;
    this.speed = this.getRandomValue(0.1, 0.9) * speed;
    this.size = 0;
    this.sizeStep = Math.random() * 0.4;
    this.minSize = 0.5;
    this.maxSizeInteger = 2;
    this.maxSize = this.getRandomValue(this.minSize, this.maxSizeInteger);
    this.delay = delay;
    this.counter = 0;
    this.counterStep = Math.random() * 4 + (canvas.width + canvas.height) * 0.01;
    this.isIdle = false;
    this.isReverse = false;
    this.isShimmer = false;
  }

  private getRandomValue(min: number, max: number) {
    return Math.random() * (max - min) + min;
  }

  draw() {
    const centerOffset = this.maxSizeInteger * 0.5 - this.size * 0.5;
    this.ctx.fillStyle = this.color;
    this.ctx.fillRect(this.x + centerOffset, this.y + centerOffset, this.size, this.size);
  }

  appear() {
    this.isIdle = false;
    if (this.counter <= this.delay) {
      this.counter += this.counterStep;
      return;
    }
    if (this.size >= this.maxSize) {
      this.isShimmer = true;
    }
    if (this.isShimmer) {
      this.shimmer();
    } else {
      this.size += this.sizeStep;
    }
    this.draw();
  }

  disappear() {
    this.isShimmer = false;
    this.counter = 0;
    if (this.size <= 0) {
      this.isIdle = true;
      return;
    }
    this.size -= 0.1;
    this.draw();
  }

  private shimmer() {
    if (this.size >= this.maxSize) {
      this.isReverse = true;
    } else if (this.size <= this.minSize) {
      this.isReverse = false;
    }
    if (this.isReverse) {
      this.size -= this.speed;
    } else {
      this.size += this.speed;
    }
  }
}

function getEffectiveSpeed(value: number, reducedMotion: boolean) {
  const min = 0;
  const max = 100;
  const throttle = 0.001;
  if (value <= min || reducedMotion) return min;
  if (value >= max) return max * throttle;
  return value * throttle;
}

export type PixelCardHandle = {
  appear: () => void;
  disappear: () => void;
  isAllIdle: () => boolean;
};

export type PixelCardProps = {
  variant?: PixelVariant;
  gap?: number;
  speed?: number;
  colors?: string;
  noFocus?: boolean;
  className?: string;
  style?: CSSProperties;
  /** fullscreen 模式：作为全屏覆盖层（用于全站进入动画） */
  fullscreen?: boolean;
  children?: ReactNode;
};

const PixelCard = forwardRef<PixelCardHandle, PixelCardProps>(function PixelCard(
  { variant = "default", gap, speed, colors, noFocus, className = "", style, fullscreen = false, children },
  ref,
) {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const pixelsRef = useRef<Pixel[]>([]);
  const animationRef = useRef<number | null>(null);
  const timePreviousRef = useRef<number>(0);
  const reducedMotionRef = useRef<boolean>(false);

  const variantCfg = VARIANTS[variant] ?? VARIANTS.default;
  const finalGap = gap ?? variantCfg.gap;
  const finalSpeed = speed ?? variantCfg.speed;
  const finalColors = colors ?? variantCfg.colors;
  const finalNoFocus = noFocus ?? variantCfg.noFocus;

  const initPixels = () => {
    const container = containerRef.current;
    const canvas = canvasRef.current;
    if (!container || !canvas) return;

    const rect = container.getBoundingClientRect();
    const width = Math.floor(rect.width);
    const height = Math.floor(rect.height);
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    canvas.width = width;
    canvas.height = height;
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const colorsArray = finalColors.split(",");
    const pxs: Pixel[] = [];
    const gapI = Math.max(1, Math.floor(finalGap));
    for (let x = 0; x < width; x += gapI) {
      for (let y = 0; y < height; y += gapI) {
        const color = colorsArray[Math.floor(Math.random() * colorsArray.length)] ?? "#ffffff";
        const dx = x - width / 2;
        const dy = y - height / 2;
        const distance = Math.sqrt(dx * dx + dy * dy);
        const delay = reducedMotionRef.current ? 0 : distance;
        pxs.push(new Pixel(canvas, ctx, x, y, color, getEffectiveSpeed(finalSpeed, reducedMotionRef.current), delay));
      }
    }
    pixelsRef.current = pxs;
  };

  const doAnimate = (fnName: "appear" | "disappear") => {
    animationRef.current = requestAnimationFrame(() => doAnimate(fnName));
    const timeNow = performance.now();
    const timePassed = timeNow - timePreviousRef.current;
    const timeInterval = 1000 / 60;
    if (timePassed < timeInterval) return;
    timePreviousRef.current = timeNow - (timePassed % timeInterval);

    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!ctx || !canvas) return;
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    let allIdle = true;
    for (const pixel of pixelsRef.current) {
      pixel[fnName]();
      if (!pixel.isIdle) allIdle = false;
    }
    if (allIdle && animationRef.current !== null) {
      cancelAnimationFrame(animationRef.current);
      animationRef.current = null;
    }
  };

  const handleAnimation = (name: "appear" | "disappear") => {
    if (animationRef.current !== null) cancelAnimationFrame(animationRef.current);
    animationRef.current = requestAnimationFrame(() => doAnimate(name));
  };

  useImperativeHandle(ref, () => ({
    appear: () => handleAnimation("appear"),
    disappear: () => handleAnimation("disappear"),
    isAllIdle: () => pixelsRef.current.every((p) => p.isIdle),
  }));

  useEffect(() => {
    if (typeof window !== "undefined") {
      reducedMotionRef.current = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      timePreviousRef.current = performance.now();
    }

    initPixels();
    const container = containerRef.current;
    let observer: ResizeObserver | null = null;
    if (container) {
      observer = new ResizeObserver(() => initPixels());
      observer.observe(container);
    }
    return () => {
      observer?.disconnect();
      if (animationRef.current !== null) {
        cancelAnimationFrame(animationRef.current);
        animationRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [finalGap, finalSpeed, finalColors, finalNoFocus]);

  const onMouseEnter = fullscreen ? undefined : () => handleAnimation("appear");
  const onMouseLeave = fullscreen ? undefined : () => handleAnimation("disappear");
  const onFocus = (e: React.FocusEvent<HTMLDivElement>) => {
    if (e.currentTarget.contains(e.relatedTarget)) return;
    handleAnimation("appear");
  };
  const onBlur = (e: React.FocusEvent<HTMLDivElement>) => {
    if (e.currentTarget.contains(e.relatedTarget)) return;
    handleAnimation("disappear");
  };

  const modeClass = fullscreen ? "pixel-card--fullscreen" : "pixel-card--card";

  return (
    <div
      ref={containerRef}
      className={`pixel-card ${modeClass} ${className}`.trim()}
      style={style}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      onFocus={fullscreen || finalNoFocus ? undefined : onFocus}
      onBlur={fullscreen || finalNoFocus ? undefined : onBlur}
      tabIndex={fullscreen || finalNoFocus ? -1 : 0}
    >
      <canvas className="pixel-canvas" ref={canvasRef} />
      {children}
    </div>
  );
});

export default PixelCard;
