"use client";

import { useEffect, useRef } from "react";
import * as THREE from "three";
import { EffectComposer } from "three/examples/jsm/postprocessing/EffectComposer.js";
import { RenderPass } from "three/examples/jsm/postprocessing/RenderPass.js";
import { ShaderPass } from "three/examples/jsm/postprocessing/ShaderPass.js";
import { Timer } from "three/src/core/Timer.js";

// ─────────────────────────────────────────────────────────────────────
//  Dither — 基于 three.js 的自托管渲染实现。
//
//  放弃 @react-three/fiber + @react-three/postprocessing 组合，因为：
//    1) R3F 内部 `new THREE.Clock()` 持续触发 r183+ 的 deprecation 警告；
//    2) R3F frameloop / Suspense / AnimatePresence 的嵌套会偶发让 R3F 退化到
//       demand 模式，uniform 停止更新 → 画面冻住；
//    3) @react-three/postprocessing 的 EffectComposer 抽象层在 r183+ 三方
//       组合下行为不稳定。
//
//  改为在一个 useEffect 里手动初始化 WebGLRenderer + EffectComposer，用
//  原生 requestAnimationFrame + THREE.Timer 驱动。无 React 抽象层、无
//  R3F 生命周期，帧循环完全在我们掌控之下。
// ─────────────────────────────────────────────────────────────────────

/* ------------------------------------------------------------------ */
/*  Shaders                                                            */
/* ------------------------------------------------------------------ */

const waveVertexShader = /* glsl */ `
precision highp float;
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

const waveFragmentShader = /* glsl */ `
precision highp float;
uniform vec2 resolution;
uniform float time;
uniform float waveSpeed;
uniform float waveFrequency;
uniform float waveAmplitude;
uniform vec3 waveColor;
uniform vec2 mousePos;
uniform int enableMouseInteraction;
uniform float mouseRadius;

vec4 mod289(vec4 x) { return x - floor(x * (1.0/289.0)) * 289.0; }
vec4 permute(vec4 x) { return mod289(((x * 34.0) + 1.0) * x); }
vec4 taylorInvSqrt(vec4 r) { return 1.79284291400159 - 0.85373472095314 * r; }
vec2 fade(vec2 t) { return t*t*t*(t*(t*6.0-15.0)+10.0); }

float cnoise(vec2 P) {
  vec4 Pi = floor(P.xyxy) + vec4(0.0,0.0,1.0,1.0);
  vec4 Pf = fract(P.xyxy) - vec4(0.0,0.0,1.0,1.0);
  Pi = mod289(Pi);
  vec4 ix = Pi.xzxz;
  vec4 iy = Pi.yyww;
  vec4 fx = Pf.xzxz;
  vec4 fy = Pf.yyww;
  vec4 i = permute(permute(ix) + iy);
  vec4 gx = fract(i * (1.0/41.0)) * 2.0 - 1.0;
  vec4 gy = abs(gx) - 0.5;
  vec4 tx = floor(gx + 0.5);
  gx = gx - tx;
  vec2 g00 = vec2(gx.x, gy.x);
  vec2 g10 = vec2(gx.y, gy.y);
  vec2 g01 = vec2(gx.z, gy.z);
  vec2 g11 = vec2(gx.w, gy.w);
  vec4 norm = taylorInvSqrt(vec4(dot(g00,g00), dot(g01,g01), dot(g10,g10), dot(g11,g11)));
  g00 *= norm.x; g01 *= norm.y; g10 *= norm.z; g11 *= norm.w;
  float n00 = dot(g00, vec2(fx.x, fy.x));
  float n10 = dot(g10, vec2(fx.y, fy.y));
  float n01 = dot(g01, vec2(fx.z, fy.z));
  float n11 = dot(g11, vec2(fx.w, fy.w));
  vec2 fade_xy = fade(Pf.xy);
  vec2 n_x = mix(vec2(n00, n01), vec2(n10, n11), fade_xy.x);
  return 2.3 * mix(n_x.x, n_x.y, fade_xy.y);
}

const int OCTAVES = 4;
float fbm(vec2 p) {
  float value = 0.0;
  float amp = 1.0;
  float freq = waveFrequency;
  for (int i = 0; i < OCTAVES; i++) {
    value += amp * abs(cnoise(p));
    p *= freq;
    amp *= waveAmplitude;
  }
  return value;
}

float pattern(vec2 p) {
  vec2 p2 = p - time * waveSpeed;
  return fbm(p + fbm(p2));
}

void main() {
  vec2 uv = gl_FragCoord.xy / resolution.xy;
  uv -= 0.5;
  uv.x *= resolution.x / resolution.y;
  float f = pattern(uv);
  if (enableMouseInteraction == 1) {
    vec2 mouseNDC = (mousePos / resolution - 0.5) * vec2(1.0, -1.0);
    mouseNDC.x *= resolution.x / resolution.y;
    float dist = length(uv - mouseNDC);
    float effect = 1.0 - smoothstep(0.0, mouseRadius, dist);
    f -= 0.5 * effect;
  }
  vec3 col = mix(vec3(0.0), waveColor, f);
  gl_FragColor = vec4(col, 1.0);
}
`;

// ShaderPass 要求 fragment shader 暴露 uniform `tDiffuse`
const ditherFragmentShader = /* glsl */ `
precision highp float;
uniform sampler2D tDiffuse;
uniform vec2 resolution;
uniform float colorNum;
uniform float pixelSize;
varying vec2 vUv;

const float bayerMatrix8x8[64] = float[64](
  0.0/64.0, 48.0/64.0, 12.0/64.0, 60.0/64.0,  3.0/64.0, 51.0/64.0, 15.0/64.0, 63.0/64.0,
  32.0/64.0,16.0/64.0, 44.0/64.0, 28.0/64.0, 35.0/64.0,19.0/64.0, 47.0/64.0, 31.0/64.0,
  8.0/64.0, 56.0/64.0,  4.0/64.0, 52.0/64.0, 11.0/64.0,59.0/64.0,  7.0/64.0, 55.0/64.0,
  40.0/64.0,24.0/64.0, 36.0/64.0, 20.0/64.0, 43.0/64.0,27.0/64.0, 39.0/64.0, 23.0/64.0,
  2.0/64.0, 50.0/64.0, 14.0/64.0, 62.0/64.0,  1.0/64.0,49.0/64.0, 13.0/64.0, 61.0/64.0,
  34.0/64.0,18.0/64.0, 46.0/64.0, 30.0/64.0, 33.0/64.0,17.0/64.0, 45.0/64.0, 29.0/64.0,
  10.0/64.0,58.0/64.0,  6.0/64.0, 54.0/64.0,  9.0/64.0,57.0/64.0,  5.0/64.0, 53.0/64.0,
  42.0/64.0,26.0/64.0, 38.0/64.0, 22.0/64.0, 41.0/64.0,25.0/64.0, 37.0/64.0, 21.0/64.0
);

vec3 dither(vec2 uv, vec3 color) {
  vec2 scaledCoord = floor(uv * resolution / pixelSize);
  int x = int(mod(scaledCoord.x, 8.0));
  int y = int(mod(scaledCoord.y, 8.0));
  float threshold = bayerMatrix8x8[y * 8 + x] - 0.25;
  float step = 1.0 / (colorNum - 1.0);
  color += threshold * step;
  float bias = 0.2;
  color = clamp(color - bias, 0.0, 1.0);
  return floor(color * (colorNum - 1.0) + 0.5) / (colorNum - 1.0);
}

void main() {
  vec2 normalizedPixelSize = pixelSize / resolution;
  vec2 uvPixel = normalizedPixelSize * floor(vUv / normalizedPixelSize);
  vec4 color = texture2D(tDiffuse, uvPixel);
  color.rgb = dither(vUv, color.rgb);
  gl_FragColor = color;
}
`;

/* ------------------------------------------------------------------ */
/*  React 包装                                                         */
/* ------------------------------------------------------------------ */

export type DitherProps = {
  waveSpeed?: number;
  waveFrequency?: number;
  waveAmplitude?: number;
  waveColor?: [number, number, number];
  colorNum?: number;
  pixelSize?: number;
  disableAnimation?: boolean;
  enableMouseInteraction?: boolean;
  mouseRadius?: number;
};

export default function Dither({
  waveSpeed = 0.05,
  waveFrequency = 3,
  waveAmplitude = 0.3,
  waveColor = [0.5, 0.5, 0.5],
  colorNum = 4,
  pixelSize = 2,
  disableAnimation = false,
  enableMouseInteraction = true,
  mouseRadius = 1,
}: DitherProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  // 把最新 props 存入 ref，动画循环按引用读取，避免每次都重建渲染器
  const propsRef = useRef({
    waveSpeed,
    waveFrequency,
    waveAmplitude,
    waveColor,
    colorNum,
    pixelSize,
    disableAnimation,
    enableMouseInteraction,
    mouseRadius,
  });
  useEffect(() => {
    propsRef.current = {
      waveSpeed,
      waveFrequency,
      waveAmplitude,
      waveColor,
      colorNum,
      pixelSize,
      disableAnimation,
      enableMouseInteraction,
      mouseRadius,
    };
  }, [
    waveSpeed,
    waveFrequency,
    waveAmplitude,
    waveColor,
    colorNum,
    pixelSize,
    disableAnimation,
    enableMouseInteraction,
    mouseRadius,
  ]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // ── 基础 three.js 初始化 ─────────────────────────────────────
    const renderer = new THREE.WebGLRenderer({
      antialias: true,
      alpha: false,
      powerPreference: "high-performance",
    });
    renderer.setPixelRatio(1); // 保持低 DPR 让 pixelation 更明显且省 GPU
    const w0 = Math.max(1, container.clientWidth);
    const h0 = Math.max(1, container.clientHeight);
    renderer.setSize(w0, h0, false);
    renderer.domElement.style.position = "absolute";
    renderer.domElement.style.inset = "0";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.display = "block";
    container.appendChild(renderer.domElement);

    const scene = new THREE.Scene();
    const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);

    // ── 全屏波形着色器 ────────────────────────────────────────────
    const waveUniforms = {
      time: { value: 0 },
      resolution: { value: new THREE.Vector2(w0, h0) },
      waveSpeed: { value: propsRef.current.waveSpeed },
      waveFrequency: { value: propsRef.current.waveFrequency },
      waveAmplitude: { value: propsRef.current.waveAmplitude },
      waveColor: { value: new THREE.Color(...propsRef.current.waveColor) },
      mousePos: { value: new THREE.Vector2(0, 0) },
      enableMouseInteraction: { value: propsRef.current.enableMouseInteraction ? 1 : 0 },
      mouseRadius: { value: propsRef.current.mouseRadius },
    };
    const waveMaterial = new THREE.ShaderMaterial({
      vertexShader: waveVertexShader,
      fragmentShader: waveFragmentShader,
      uniforms: waveUniforms,
    });
    const fullscreenQuad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), waveMaterial);
    scene.add(fullscreenQuad);

    // ── EffectComposer 后期 Dither ───────────────────────────────
    const composer = new EffectComposer(renderer);
    composer.setSize(w0, h0);
    composer.addPass(new RenderPass(scene, camera));

    const ditherPass = new ShaderPass({
      uniforms: {
        tDiffuse: { value: null },
        resolution: { value: new THREE.Vector2(w0, h0) },
        colorNum: { value: propsRef.current.colorNum },
        pixelSize: { value: propsRef.current.pixelSize },
      },
      vertexShader: /* glsl */ `
        varying vec2 vUv;
        void main() {
          vUv = uv;
          gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
        }
      `,
      fragmentShader: ditherFragmentShader,
    });
    composer.addPass(ditherPass);

    // ── 鼠标交互 ──────────────────────────────────────────────────
    const onPointerMove = (e: PointerEvent) => {
      if (!propsRef.current.enableMouseInteraction) return;
      const rect = renderer.domElement.getBoundingClientRect();
      waveUniforms.mousePos.value.set(
        (e.clientX - rect.left) * renderer.getPixelRatio(),
        (e.clientY - rect.top) * renderer.getPixelRatio(),
      );
    };
    renderer.domElement.style.pointerEvents = "auto";
    renderer.domElement.addEventListener("pointermove", onPointerMove);

    // ── 自适应尺寸 ────────────────────────────────────────────────
    const onResize = () => {
      const w = Math.max(1, container.clientWidth);
      const h = Math.max(1, container.clientHeight);
      renderer.setSize(w, h, false);
      composer.setSize(w, h);
      waveUniforms.resolution.value.set(w, h);
      (ditherPass.uniforms.resolution.value as THREE.Vector2).set(w, h);
    };
    const ro = new ResizeObserver(onResize);
    ro.observe(container);

    // ── 渲染循环（THREE.Timer，避免 deprecated Clock） ────────────
    const timer = new Timer();
    if (typeof document !== "undefined") timer.connect(document);

    let rafId = 0;
    let running = true;
    const tick = () => {
      if (!running) return;
      timer.update();
      const p = propsRef.current;
      if (!p.disableAnimation) {
        waveUniforms.time.value = timer.getElapsed();
      }
      waveUniforms.waveSpeed.value = p.waveSpeed;
      waveUniforms.waveFrequency.value = p.waveFrequency;
      waveUniforms.waveAmplitude.value = p.waveAmplitude;
      (waveUniforms.waveColor.value as THREE.Color).setRGB(
        p.waveColor[0],
        p.waveColor[1],
        p.waveColor[2],
      );
      waveUniforms.enableMouseInteraction.value = p.enableMouseInteraction ? 1 : 0;
      waveUniforms.mouseRadius.value = p.mouseRadius;
      ditherPass.uniforms.colorNum.value = p.colorNum;
      ditherPass.uniforms.pixelSize.value = p.pixelSize;

      composer.render();
      rafId = requestAnimationFrame(tick);
    };
    rafId = requestAnimationFrame(tick);

    // ── 卸载清理 ──────────────────────────────────────────────────
    return () => {
      running = false;
      cancelAnimationFrame(rafId);
      ro.disconnect();
      renderer.domElement.removeEventListener("pointermove", onPointerMove);
      timer.disconnect();
      timer.dispose();
      composer.dispose();
      waveMaterial.dispose();
      fullscreenQuad.geometry.dispose();
      renderer.dispose();
      if (renderer.domElement.parentNode === container) {
        container.removeChild(renderer.domElement);
      }
    };
  }, []);

  return <div ref={containerRef} className="absolute inset-0 size-full overflow-hidden" />;
}
