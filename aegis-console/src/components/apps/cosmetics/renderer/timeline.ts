import type { Design, Easing, Layer, Track, TrackProperty } from "@/lib/api/cosmetics";

/**
 * 关键帧轨道（docs/cosmetics.md「动画轨道」）。
 *
 * - 整份设计稿以 `elapsed mod duration` 取当前时刻；duration 为 0 时轨道不生效。
 * - 两帧之间用**后一帧**的 easing；早于第一帧取第一帧的值，晚于最后一帧取最后一帧的值。
 * - 轨道存在时覆盖静态值；scaleX / scaleY 乘在 scale 上。
 */

const C1 = 1.70158;
const C3 = C1 + 1;

export function applyEasing(easing: Easing | string | undefined, t: number): number {
  const p = Math.max(0, Math.min(1, t));
  switch (easing) {
    case "easeIn":
      return p * p * p;
    case "easeOut":
      return 1 - Math.pow(1 - p, 3);
    case "easeInOut":
      return p < 0.5 ? 4 * p * p * p : 1 - Math.pow(-2 * p + 2, 3) / 2;
    case "step":
      // 未到下一帧前保持上一帧
      return p >= 1 ? 1 : 0;
    case "backOut":
      return 1 + C3 * Math.pow(p - 1, 3) + C1 * Math.pow(p - 1, 2);
    case "elastic":
      if (p === 0) return 0;
      if (p === 1) return 1;
      return Math.pow(2, -10 * p) * Math.sin(((10 * p - 0.75) * 2 * Math.PI) / 3) + 1;
    default:
      return p;
  }
}

/** 一条轨道在时刻 t 的值；关键帧为空时 undefined。关键帧不要求已排序。 */
export function sampleTrack(track: Track, t: number): number | undefined {
  const frames = track.keyframes;
  if (!frames || frames.length === 0) return undefined;
  let sorted = frames;
  for (let i = 1; i < frames.length; i++) {
    if (frames[i].t < frames[i - 1].t) {
      sorted = [...frames].sort((a, b) => a.t - b.t);
      break;
    }
  }
  const first = sorted[0];
  const last = sorted[sorted.length - 1];
  if (t <= first.t) return first.v;
  if (t >= last.t) return last.v;
  for (let i = 0; i < sorted.length - 1; i++) {
    const prev = sorted[i];
    const next = sorted[i + 1];
    if (t >= prev.t && t < next.t) {
      const span = next.t - prev.t;
      const p = span > 0 ? (t - prev.t) / span : 1;
      return prev.v + (next.v - prev.v) * applyEasing(next.easing, p);
    }
  }
  return last.v;
}

/** 设计稿当前时刻：elapsed 对 duration 取模；duration 为 0 时恒为 0 */
export function loopTime(design: Pick<Design, "duration">, elapsed: number) {
  const duration = design.duration || 0;
  if (duration <= 0) return 0;
  const m = elapsed % duration;
  return m < 0 ? m + duration : m;
}

export function trackOf(layer: Layer, property: TrackProperty): Track | undefined {
  return layer.tracks?.find((track) => track.property === property);
}

/** 图层在某一时刻生效的变换与动画量 */
export type LayerState = {
  x: number;
  y: number;
  width: number;
  height: number;
  anchorX: number;
  anchorY: number;
  /** 最终的 x / y 缩放（scale × scaleX / scaleY） */
  sx: number;
  sy: number;
  rotation: number;
  opacity: number;
  hue: number;
  sweep: number;
  dashOffset: number;
};

/**
 * 图层在设计稿时刻 t 的状态。`animate=false`（duration 为 0）时只用静态值。
 */
export function layerStateAt(layer: Layer, t: number, animate: boolean): LayerState {
  const value = (property: TrackProperty, fallback: number) => {
    if (!animate) return fallback;
    const track = trackOf(layer, property);
    if (!track) return fallback;
    const v = sampleTrack(track, t);
    return v === undefined || !Number.isFinite(v) ? fallback : v;
  };
  const scale = value("scale", layer.scale ?? 1);
  const scaleX = value("scaleX", 1);
  const scaleY = value("scaleY", 1);
  return {
    x: value("x", layer.x ?? 0),
    y: value("y", layer.y ?? 0),
    width: layer.width ?? 0,
    height: layer.height ?? 0,
    anchorX: layer.anchorX ?? 0.5,
    anchorY: layer.anchorY ?? 0.5,
    sx: Math.max(0, scale * scaleX),
    sy: Math.max(0, scale * scaleY),
    rotation: value("rotation", layer.rotation ?? 0),
    opacity: Math.max(0, Math.min(1, value("opacity", layer.opacity ?? 1))),
    hue: value("hue", 0),
    sweep: Math.max(0, Math.min(360, value("sweep", layer.sweep ?? (layer.shape === "ring" ? 360 : 0)))),
    dashOffset: value("dashOffset", layer.dashOffset ?? 0)
  };
}

/** 2D 仿射矩阵 [a, b, c, d, e, f]，与 canvas 的 setTransform 同序 */
export type Matrix = [number, number, number, number, number, number];

/**
 * 图层局部坐标（以图层矩形左上角为原点、未缩放的像素）→ 画布坐标。
 * 以锚点为中心先缩放、再旋转（顺时针为正），即 p ↦ P + R·S·(p − A)，P = (x, y) + A，A = 锚点在图层内的偏移。
 */
export function layerMatrix(state: LayerState): Matrix {
  const ax = state.anchorX * state.width;
  const ay = state.anchorY * state.height;
  const rad = (state.rotation * Math.PI) / 180;
  const cos = Math.cos(rad);
  const sin = Math.sin(rad);
  const a = cos * state.sx;
  const b = sin * state.sx;
  const c = -sin * state.sy;
  const d = cos * state.sy;
  const px = state.x + ax;
  const py = state.y + ay;
  return [a, b, c, d, px - (a * ax + c * ay), py - (b * ax + d * ay)];
}

export function applyMatrix(m: Matrix, x: number, y: number): { x: number; y: number } {
  return { x: m[0] * x + m[2] * y + m[4], y: m[1] * x + m[3] * y + m[5] };
}

export function invertMatrix(m: Matrix): Matrix | null {
  const det = m[0] * m[3] - m[1] * m[2];
  if (!det || !Number.isFinite(det)) return null;
  const inv = 1 / det;
  return [
    m[3] * inv,
    -m[1] * inv,
    -m[2] * inv,
    m[0] * inv,
    (m[2] * m[5] - m[3] * m[4]) * inv,
    (m[1] * m[4] - m[0] * m[5]) * inv
  ];
}

/** 图层四个角在画布上的位置：左上、右上、右下、左下 */
export function layerCorners(state: LayerState) {
  const m = layerMatrix(state);
  return [
    applyMatrix(m, 0, 0),
    applyMatrix(m, state.width, 0),
    applyMatrix(m, state.width, state.height),
    applyMatrix(m, 0, state.height)
  ];
}

/** 设计稿里是否有任何会随时间变化的东西（决定要不要逐帧重绘） */
export function designAnimates(design: Design | null | undefined) {
  if (!design) return false;
  if (design.nameStyle) return design.nameStyle.effect !== "none" && design.nameStyle.effect !== "glow";
  for (const layer of design.layers ?? []) {
    if (layer.hidden) continue;
    if (layer.type === "sprite" || layer.type === "particles") return true;
    if (design.duration > 0 && layer.tracks && layer.tracks.length > 0) return true;
  }
  return false;
}
