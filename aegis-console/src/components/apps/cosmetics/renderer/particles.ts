import type { Particles } from "@/lib/api/cosmetics";

/**
 * 确定性粒子（docs/cosmetics.md「粒子」）：三端用同一套随机数与公式，看起来一致。
 *
 * 第 i 个粒子的随机数来自 `mulberry32(seed + i·0x9E3779B9)`，依次取 r0…r5；
 * 出生偏移 phase = i / count · lifetime，时刻 t 的年龄 age = (t + phase) mod lifetime。
 * 加法按 32 位整数回绕，与 Kotlin 的 `(seed + i * 0x9E3779B9).toInt()` 同值。
 */

export function mulberry32(seed: number) {
  let a = seed | 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const GOLDEN = 0x9e3779b9;

/** 32 位回绕的 seed + i·0x9E3779B9 */
export function particleSeed(seed: number, index: number) {
  // seed 可能大于 2^31：先按 2^32 取模再参与运算，保证与 64 位整数截断到 32 位同值
  const base = Number(BigInt.asIntN(32, BigInt(Math.trunc(seed || 0))));
  return (base + Math.imul(index, GOLDEN)) | 0;
}

export type ParticleSample = {
  x: number;
  y: number;
  size: number;
  alpha: number;
  color: string | undefined;
  /** 自转角度（度） */
  spin: number;
  /** 当前运动方向（弧度，canvas 坐标系，0 指向 +x） */
  heading: number;
};

const DEG = Math.PI / 180;

/**
 * 粒子在时刻 t（毫秒，自己的时钟，不受 duration 影响）的状态。坐标在图层局部空间（原点为图层左上角）。
 */
export function sampleParticles(p: Particles, width: number, height: number, t: number): ParticleSample[] {
  const count = Math.max(0, Math.min(240, Math.floor(p.count || 0)));
  const lifetime = Math.max(1, p.lifetime || 1);
  const colors = p.colors ?? [];
  const cx = width / 2;
  const cy = height / 2;
  const out: ParticleSample[] = [];
  for (let i = 0; i < count; i++) {
    const rand = mulberry32(particleSeed(p.seed, i));
    const r0 = rand();
    const r1 = rand();
    const r2 = rand();
    const r3 = rand();
    const r4 = rand();
    const r5 = rand();
    const phase = (i / count) * lifetime;
    let age = (t + phase) % lifetime;
    if (age < 0) age += lifetime;
    const progress = age / lifetime;

    const theta = r0 * 360;
    let ox = cx;
    let oy = cy;
    if (p.emitter === "ring") {
      const radius = p.radius ?? 0;
      ox = cx + radius * Math.sin(theta * DEG);
      oy = cy - radius * Math.cos(theta * DEG);
    } else if (p.emitter === "rect") {
      ox = r0 * width;
      oy = r1 * height;
    }

    const spread = p.spread ?? 0;
    const angle = p.outward && p.emitter === "ring" ? theta : (p.direction ?? 0) - spread / 2 + r2 * spread;
    // 0 向上、顺时针
    const dx = Math.sin(angle * DEG);
    const dy = -Math.cos(angle * DEG);
    const speed = (p.speed ?? 0) * (0.6 + 0.4 * r3);
    const seconds = age / 1000;
    const gravity = p.gravity ?? 0;
    const x = ox + speed * dx * seconds;
    const y = oy + speed * dy * seconds + 0.5 * gravity * seconds * seconds;

    let size = p.sizeMin + (p.sizeMax - p.sizeMin) * r4;
    if (p.shrink) size *= 1 - progress;
    let alpha = 1;
    if (p.fade) alpha = (1 - progress) * Math.min(1, progress / 0.1);

    const vx = speed * dx;
    const vy = speed * dy + gravity * seconds;
    out.push({
      x,
      y,
      size: Math.max(0, size),
      alpha: Math.max(0, Math.min(1, alpha)),
      color: colors.length ? colors[Math.min(colors.length - 1, Math.floor(r5 * colors.length))] : undefined,
      spin: (p.spin ?? 0) * seconds,
      heading: vx === 0 && vy === 0 ? -Math.PI / 2 : Math.atan2(vy, vx)
    });
  }
  return out;
}
