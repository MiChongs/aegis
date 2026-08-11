"use client";

import { useEffect, useRef } from "react";

// ─────────────────────────────────────────────────────────────────────
//  PixelIntro — 轻量版进入动画（纯 Canvas2D，性能友好）。
//
//  - 移植自 React Bits 的 PixelCard 原理，但做了以下性能改造：
//    1) 全屏场景下 gap 默认 14px，像素数量控制在 ~1 万以内（vs 原版 ~5 万+）。
//    2) fillStyle 按颜色分组，每帧一次 beginPath/fillRect，减少 style 切换。
//    3) 只在 appear / disappear 两个阶段运行；全部 idle 后立即停 raf。
//    4) DPR 固定 1，不为 Retina 放大——像素艺术不需要。
//  - 深色 / 浅色 palette 自适应（基于 matchMedia prefers-color-scheme）。
//  - 叠加水平扫描线 + 中心辉光 + Aegis 字样，强化未来感。
// ─────────────────────────────────────────────────────────────────────

const LIGHT_COLORS = ["#c96442", "#d97757", "#b0aea5", "#5e5d59"];
const DARK_COLORS = ["#d97757", "#f5f4ed", "#6aa3d2", "#9c7bff"]; // terracotta + ivory + cyan + lavender

type Stage = "appear" | "hold" | "disappear" | "done";

type PixelIntroProps = {
  /** 动画整体时长（毫秒） */
  totalMs?: number;
  /** 完成回调：overlay 可以在此卸载自身 */
  onDone?: () => void;
  /** 像素间距（像素越多性能越差） */
  gap?: number;
};

class Pixel {
  x: number;
  y: number;
  color: string;
  speed: number;
  size: number;
  sizeStep: number;
  minSize: number;
  maxSize: number;
  maxSizeInteger: number;
  delay: number;
  counter: number;
  counterStep: number;
  isIdle: boolean;
  isReverse: boolean;
  isShimmer: boolean;

  constructor(x: number, y: number, color: string, speed: number, delay: number, dim: number) {
    this.x = x;
    this.y = y;
    this.color = color;
    this.speed = (Math.random() * 0.8 + 0.2) * speed;
    this.size = 0;
    this.sizeStep = Math.random() * 0.5 + 0.15;
    this.minSize = 0.5;
    this.maxSizeInteger = 2.5;
    this.maxSize = Math.random() * (this.maxSizeInteger - this.minSize) + this.minSize;
    this.delay = delay;
    this.counter = 0;
    this.counterStep = Math.random() * 4 + dim * 0.01;
    this.isIdle = false;
    this.isReverse = false;
    this.isShimmer = false;
  }

  advance(mode: "appear" | "disappear") {
    if (mode === "appear") {
      this.isIdle = false;
      if (this.counter <= this.delay) {
        this.counter += this.counterStep;
        return;
      }
      if (this.size >= this.maxSize) this.isShimmer = true;
      if (this.isShimmer) {
        if (this.size >= this.maxSize) this.isReverse = true;
        else if (this.size <= this.minSize) this.isReverse = false;
        this.size += this.isReverse ? -this.speed : this.speed;
      } else {
        this.size += this.sizeStep;
      }
    } else {
      this.isShimmer = false;
      this.counter = 0;
      if (this.size <= 0) {
        this.isIdle = true;
        return;
      }
      this.size -= 0.18;
    }
  }
}

function prefersDark(): boolean {
  if (typeof window === "undefined") return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function prefersReducedMotion(): boolean {
  if (typeof window === "undefined") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function PixelIntro({ totalMs = 2600, onDone, gap = 14 }: PixelIntroProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const pixelsRef = useRef<Pixel[]>([]);
  const groupedRef = useRef<Map<string, Pixel[]>>(new Map());
  const stageRef = useRef<Stage>("appear");
  const rafRef = useRef<number | null>(null);
  const doneRef = useRef(false);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    if (prefersReducedMotion()) {
      onDone?.();
      return;
    }

    const ctx = canvas.getContext("2d", { alpha: true });
    if (!ctx) {
      onDone?.();
      return;
    }

    const w = Math.floor(window.innerWidth);
    const h = Math.floor(window.innerHeight);
    canvas.width = w;
    canvas.height = h;
    canvas.style.width = `${w}px`;
    canvas.style.height = `${h}px`;

    const palette = prefersDark() ? DARK_COLORS : LIGHT_COLORS;
    const speed = 0.06;
    const dim = Math.hypot(w, h);

    // 构建像素 + 按颜色分组（减少 fillStyle 切换）
    const pixels: Pixel[] = [];
    const grouped = new Map<string, Pixel[]>();
    for (const c of palette) grouped.set(c, []);
    const cx = w / 2;
    const cy = h / 2;
    for (let x = 0; x < w; x += gap) {
      for (let y = 0; y < h; y += gap) {
        const color = palette[Math.floor(Math.random() * palette.length)]!;
        const dx = x - cx;
        const dy = y - cy;
        // 从中心向外扩散：delay 随半径递增
        const delay = Math.sqrt(dx * dx + dy * dy) * 0.6;
        const p = new Pixel(x, y, color, speed, delay, dim);
        pixels.push(p);
        grouped.get(color)!.push(p);
      }
    }
    pixelsRef.current = pixels;
    groupedRef.current = grouped;

    const startedAt = performance.now();
    const holdUntil = startedAt + totalMs * 0.55;
    const disappearUntil = startedAt + totalMs;
    stageRef.current = "appear";

    let lastFrame = performance.now();
    const frameInterval = 1000 / 60;

    const tick = () => {
      rafRef.current = requestAnimationFrame(tick);
      const now = performance.now();
      if (now - lastFrame < frameInterval) return;
      lastFrame = now;

      // 阶段推进
      if (stageRef.current === "appear" && now >= holdUntil) stageRef.current = "disappear";
      if (stageRef.current === "disappear" && now >= disappearUntil) {
        // 强制进入收尾：剩余像素直接衰减
      }

      const mode = stageRef.current === "disappear" ? "disappear" : "appear";
      let allIdle = mode === "disappear";
      for (const p of pixels) {
        p.advance(mode);
        if (!p.isIdle) allIdle = false;
      }

      // 批量绘制：一种颜色一个 fillStyle，O(color) 次切换 + O(n) 次 fillRect
      ctx.clearRect(0, 0, w, h);
      for (const [color, group] of grouped) {
        ctx.fillStyle = color;
        for (const p of group) {
          if (p.size <= 0) continue;
          const offset = p.maxSizeInteger * 0.5 - p.size * 0.5;
          ctx.fillRect(p.x + offset, p.y + offset, p.size, p.size);
        }
      }

      if (mode === "disappear" && allIdle && !doneRef.current) {
        doneRef.current = true;
        stageRef.current = "done";
        if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
        onDone?.();
      }
    };
    rafRef.current = requestAnimationFrame(tick);

    return () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [totalMs, gap, onDone]);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      style={{
        position: "fixed",
        inset: 0,
        width: "100vw",
        height: "100vh",
        display: "block",
        pointerEvents: "none",
      }}
    />
  );
}
