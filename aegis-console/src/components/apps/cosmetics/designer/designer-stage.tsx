"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Maximize, Minus, Plus, Scan } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import type { AvatarSlot, Design, Layer } from "@/lib/api/cosmetics";
import { cn } from "@/lib/utils";
import {
  applyMatrix,
  invertMatrix,
  layerCorners,
  layerMatrix,
  renderDesign,
  type LayerState,
  type Matrix
} from "../renderer";
import { AVATAR_ID, patchLayer, shiftAnimated, stateOf } from "./design-ops";
import { designTime, useDesigner, useDesignerApi } from "./designer-store";

/**
 * 设计器画布：按画布尺寸缩放显示，可缩放（Ctrl / ⌘ + 滚轮、按钮）与平移（拖空白处、空格 + 拖、中键、滚轮），
 * 棋盘格透明底；头像框显示可调的头像区域并用示例头像填充（placement=below 的层画在头像下面）。
 *
 * 选中图层后叠一层变换控件：拖动移动（吸附画布中心与边缘、头像区域）、拖角 / 边缩放宽高（Shift 等比）、
 * 顶部手柄旋转（Shift 每 15°）、拖动锚点（位置自动补偿，图层看起来不动）。有 x / y / rotation 轨道时，
 * 拖动会把整条轨道一起平移，动画路径跟着走。
 *
 * 画面在 rAF 里直接读 store 画（不经过 React），播放时由这里推进时钟。
 */

type View = { zoom: number; panX: number; panY: number };
type Backdrop = "checker" | "light" | "dark";

type Gesture =
  | { kind: "pan"; startX: number; startY: number; view: View }
  | { kind: "move"; id: string; start: { x: number; y: number }; layer: Layer; state: LayerState }
  | { kind: "resize"; id: string; hx: number; hy: number; layer: Layer; state: LayerState; matrix: Matrix; inverse: Matrix }
  | { kind: "rotate"; id: string; layer: Layer; state: LayerState; pivot: { x: number; y: number }; angle0: number }
  | { kind: "anchor"; id: string; layer: Layer; state: LayerState; inverse: Matrix; matrix: Matrix }
  | { kind: "avatar-move"; start: { x: number; y: number }; avatar: AvatarSlot }
  | { kind: "avatar-resize"; hx: number; hy: number; avatar: AvatarSlot };

const HANDLES: Array<{ key: string; hx: number; hy: number; cursor: string }> = [
  { key: "nw", hx: 0, hy: 0, cursor: "nwse-resize" },
  { key: "n", hx: 0.5, hy: 0, cursor: "ns-resize" },
  { key: "ne", hx: 1, hy: 0, cursor: "nesw-resize" },
  { key: "e", hx: 1, hy: 0.5, cursor: "ew-resize" },
  { key: "se", hx: 1, hy: 1, cursor: "nwse-resize" },
  { key: "s", hx: 0.5, hy: 1, cursor: "ns-resize" },
  { key: "sw", hx: 0, hy: 1, cursor: "nesw-resize" },
  { key: "w", hx: 0, hy: 0.5, cursor: "ew-resize" }
];

const r1 = (value: number) => Math.round(value * 10) / 10;

function fitView(design: Design, width: number, height: number): View {
  const cw = design.canvas.width || 1;
  const ch = design.canvas.height || 1;
  const zoom = Math.max(0.05, Math.min(8, Math.min((width - 96) / cw, (height - 96) / ch)));
  return { zoom, panX: (width - cw * zoom) / 2, panY: (height - ch * zoom) / 2 };
}

let checkerTile: HTMLCanvasElement | null = null;
function checkerPattern(ctx: CanvasRenderingContext2D) {
  if (!checkerTile) {
    checkerTile = document.createElement("canvas");
    checkerTile.width = 16;
    checkerTile.height = 16;
    const tile = checkerTile.getContext("2d");
    if (tile) {
      tile.fillStyle = "#ffffff";
      tile.fillRect(0, 0, 16, 16);
      tile.fillStyle = "#e4e4e7";
      tile.fillRect(0, 0, 8, 8);
      tile.fillRect(8, 8, 8, 8);
    }
  }
  return ctx.createPattern(checkerTile, "repeat");
}

/** 一个点落在哪个图层上：从最上面往下找，粒子层最后才算（它通常铺满画布） */
function hitTest(design: Design, t: number, locked: Record<string, boolean>, point: { x: number; y: number }) {
  const ordered: Array<Layer | "avatar"> = [];
  const layers = design.layers;
  if (design.avatar) {
    ordered.push(...layers.filter((layer) => layer.placement !== "below").reverse(), "avatar", ...layers.filter((layer) => layer.placement === "below").reverse());
  } else {
    ordered.push(...[...layers].reverse());
  }
  const inside = (layer: Layer) => {
    const inverse = invertMatrix(layerMatrix(stateOf(layer, design, t)));
    if (!inverse) return false;
    const q = applyMatrix(inverse, point.x, point.y);
    return q.x >= 0 && q.y >= 0 && q.x <= layer.width && q.y <= layer.height;
  };
  for (const entry of ordered) {
    if (entry === "avatar") {
      const a = design.avatar!;
      if (point.x >= a.x && point.y >= a.y && point.x <= a.x + a.size && point.y <= a.y + a.size) return AVATAR_ID;
      continue;
    }
    if (entry.hidden || locked[entry.id] || entry.type === "particles") continue;
    if (inside(entry)) return entry.id;
  }
  for (const entry of ordered) {
    if (entry === "avatar" || entry.hidden || locked[entry.id] || entry.type !== "particles") continue;
    if (inside(entry)) return entry.id;
  }
  return null;
}

export function DesignerStage({ readOnly, assetUrl }: { readOnly: boolean; assetUrl?: (id: number) => string | undefined }) {
  const api = useDesignerApi();
  const design = useDesigner((s) => s.design);
  const selected = useDesigner((s) => s.selected);
  const locked = useDesigner((s) => s.locked);
  const t = useDesigner((s) => designTime(s));
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [customView, setCustomView] = useState<View | null>(null);
  const [backdrop, setBackdrop] = useState<Backdrop>("checker");
  const [showAvatar, setShowAvatar] = useState(true);
  const [guides, setGuides] = useState<{ x?: number; y?: number }>({});
  const [spaceDown, setSpaceDown] = useState(false);
  const [panning, setPanning] = useState(false);
  const gesture = useRef<Gesture | null>(null);

  const view = customView ?? fitView(design, size.width, size.height);
  const live = useRef({ view, backdrop, showAvatar, size, assetUrl });
  useEffect(() => {
    live.current = { view, backdrop, showAvatar, size, assetUrl };
  });

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => {
      setSize({ width: entry.contentRect.width, height: entry.contentRect.height });
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // 绘制循环：读 store 直接画；播放中推进时钟
  useEffect(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;
    let raf = 0;
    let last = performance.now();
    const frame = (now: number) => {
      const state = api.getState();
      if (state.playing) state.setClock(state.clock + Math.min(100, now - last));
      last = now;
      const { view: v, size: s, backdrop: bg, showAvatar: avatarOn } = live.current;
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      const w = Math.max(1, Math.round(s.width * dpr));
      const h = Math.max(1, Math.round(s.height * dpr));
      if (canvas.width !== w) canvas.width = w;
      if (canvas.height !== h) canvas.height = h;
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.clearRect(0, 0, w, h);
      const d = api.getState().design;
      const cw = d.canvas.width || 1;
      const ch = d.canvas.height || 1;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      if (bg === "checker") {
        const pattern = checkerPattern(ctx);
        if (pattern) ctx.fillStyle = pattern;
      } else {
        ctx.fillStyle = bg === "light" ? "#ffffff" : "#18181b";
      }
      ctx.fillRect(v.panX, v.panY, cw * v.zoom, ch * v.zoom);
      ctx.save();
      ctx.beginPath();
      ctx.rect(v.panX, v.panY, cw * v.zoom, ch * v.zoom);
      ctx.clip();
      const ratio = dpr * v.zoom;
      ctx.setTransform(ratio, 0, 0, ratio, dpr * v.panX, dpr * v.panY);
      const latest = api.getState();
      renderDesign(ctx, d, {
        elapsed: latest.clock,
        time: designTime(latest),
        pixelRatio: ratio,
        placeholders: true,
        avatar: avatarOn ? {} : null,
        assetUrl: live.current.assetUrl
      });
      ctx.restore();
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.strokeStyle = "rgba(113, 113, 122, 0.45)";
      ctx.lineWidth = 1;
      ctx.strokeRect(v.panX - 0.5, v.panY - 0.5, cw * v.zoom + 1, ch * v.zoom + 1);
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [api]);

  // 空格按住即平移
  useEffect(() => {
    const typing = (target: EventTarget | null) =>
      target instanceof HTMLElement && (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName));
    const down = (event: KeyboardEvent) => {
      if (event.code === "Space" && !typing(event.target)) setSpaceDown(true);
    };
    const up = (event: KeyboardEvent) => {
      if (event.code === "Space") setSpaceDown(false);
    };
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
    };
  }, []);

  // 滚轮：Ctrl / ⌘ 缩放（以指针为中心），否则平移；要 preventDefault，所以用非被动监听
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = el.getBoundingClientRect();
      const current = live.current.view;
      if (event.ctrlKey || event.metaKey) {
        const px = event.clientX - rect.left;
        const py = event.clientY - rect.top;
        const zoom = Math.max(0.05, Math.min(16, current.zoom * Math.exp(-event.deltaY * 0.0025)));
        setCustomView({
          zoom,
          panX: px - ((px - current.panX) * zoom) / current.zoom,
          panY: py - ((py - current.panY) * zoom) / current.zoom
        });
      } else {
        setCustomView({ ...current, panX: current.panX - event.deltaX, panY: current.panY - event.deltaY });
      }
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const toDesign = (event: { clientX: number; clientY: number }) => {
    const rect = containerRef.current!.getBoundingClientRect();
    return { x: (event.clientX - rect.left - view.panX) / view.zoom, y: (event.clientY - rect.top - view.panY) / view.zoom };
  };
  const toScreen = (p: { x: number; y: number }) => ({ x: view.panX + p.x * view.zoom, y: view.panY + p.y * view.zoom });

  const selectedLayer = selected && selected !== AVATAR_ID ? design.layers.find((layer) => layer.id === selected) : undefined;
  const selectedState = selectedLayer && !selectedLayer.hidden ? stateOf(selectedLayer, design, t) : null;
  const editable = !readOnly;

  const snapTargets = useMemo(() => {
    const xs = [0, design.canvas.width / 2, design.canvas.width];
    const ys = [0, design.canvas.height / 2, design.canvas.height];
    if (design.avatar) {
      const a = design.avatar;
      xs.push(a.x, a.x + a.size / 2, a.x + a.size);
      ys.push(a.y, a.y + a.size / 2, a.y + a.size);
    }
    return { xs, ys };
  }, [design.canvas.width, design.canvas.height, design.avatar]);

  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button === 2) return;
    const target = event.target as Element;
    event.currentTarget.setPointerCapture(event.pointerId);
    if (event.button === 1 || spaceDown) {
      gesture.current = { kind: "pan", startX: event.clientX, startY: event.clientY, view };
      setPanning(true);
      return;
    }
    const p = toDesign(event);
    const state = api.getState();
    const handle = target.closest("[data-handle]")?.getAttribute("data-handle");
    if (handle && editable) {
      if (selected === AVATAR_ID && design.avatar) {
        const spec = HANDLES.find((item) => item.key === handle);
        if (spec) {
          state.begin();
          gesture.current = { kind: "avatar-resize", hx: spec.hx, hy: spec.hy, avatar: { ...design.avatar } };
        }
        return;
      }
      if (selectedLayer && selectedState && !locked[selectedLayer.id]) {
        const matrix = layerMatrix(selectedState);
        const inverse = invertMatrix(matrix);
        if (!inverse) return;
        state.begin();
        if (handle === "rotate") {
          const pivot = applyMatrix(matrix, selectedState.anchorX * selectedState.width, selectedState.anchorY * selectedState.height);
          gesture.current = {
            kind: "rotate",
            id: selectedLayer.id,
            layer: selectedLayer,
            state: selectedState,
            pivot,
            angle0: Math.atan2(p.y - pivot.y, p.x - pivot.x)
          };
        } else if (handle === "anchor") {
          gesture.current = { kind: "anchor", id: selectedLayer.id, layer: selectedLayer, state: selectedState, inverse, matrix };
        } else {
          const spec = HANDLES.find((item) => item.key === handle);
          if (spec) {
            gesture.current = { kind: "resize", id: selectedLayer.id, hx: spec.hx, hy: spec.hy, layer: selectedLayer, state: selectedState, matrix, inverse };
          }
        }
        return;
      }
    }
    const hit = hitTest(design, t, locked, p);
    state.select(hit);
    if (!hit) {
      gesture.current = { kind: "pan", startX: event.clientX, startY: event.clientY, view };
      setPanning(true);
      return;
    }
    if (!editable) return;
    if (hit === AVATAR_ID && design.avatar) {
      state.begin();
      gesture.current = { kind: "avatar-move", start: p, avatar: { ...design.avatar } };
      return;
    }
    const layer = design.layers.find((item) => item.id === hit);
    if (layer) {
      state.begin();
      gesture.current = { kind: "move", id: layer.id, start: p, layer, state: stateOf(layer, design, t) };
    }
  };

  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const g = gesture.current;
    if (!g) return;
    const state = api.getState();
    if (g.kind === "pan") {
      setCustomView({ ...g.view, panX: g.view.panX + event.clientX - g.startX, panY: g.view.panY + event.clientY - g.startY });
      return;
    }
    const p = toDesign(event);
    const threshold = 6 / view.zoom;
    if (g.kind === "move") {
      let dx = p.x - g.start.x;
      let dy = p.y - g.start.y;
      if (event.shiftKey) {
        if (Math.abs(dx) > Math.abs(dy)) dy = 0;
        else dx = 0;
      }
      const corners = layerCorners(g.state);
      const xs = corners.map((c) => c.x + dx);
      const ys = corners.map((c) => c.y + dy);
      const box = { minX: Math.min(...xs), maxX: Math.max(...xs), minY: Math.min(...ys), maxY: Math.max(...ys) };
      const snap = (values: number[], targets: number[]) => {
        let best: { diff: number; at: number } | null = null;
        for (const value of values) {
          for (const target of targets) {
            const diff = target - value;
            if (Math.abs(diff) <= threshold && (!best || Math.abs(diff) < Math.abs(best.diff))) best = { diff, at: target };
          }
        }
        return best;
      };
      const sx = event.altKey ? null : snap([box.minX, (box.minX + box.maxX) / 2, box.maxX], snapTargets.xs);
      const sy = event.altKey ? null : snap([box.minY, (box.minY + box.maxY) / 2, box.maxY], snapTargets.ys);
      if (sx) dx += sx.diff;
      if (sy) dy += sy.diff;
      setGuides({ x: sx?.at, y: sy?.at });
      const moved = shiftAnimated(shiftAnimated(g.layer, "x", r1(dx)), "y", r1(dy));
      state.live((d) => patchLayer(d, g.id, () => moved));
      return;
    }
    if (g.kind === "resize") {
      const s0 = g.state;
      const q = applyMatrix(g.inverse, p.x, p.y);
      const fixed = { x: g.hx === 0.5 ? s0.width / 2 : (1 - g.hx) * s0.width, y: g.hy === 0.5 ? s0.height / 2 : (1 - g.hy) * s0.height };
      let w = g.hx === 1 ? q.x - fixed.x : g.hx === 0 ? fixed.x - q.x : s0.width;
      let h = g.hy === 1 ? q.y - fixed.y : g.hy === 0 ? fixed.y - q.y : s0.height;
      w = Math.max(1, w);
      h = Math.max(1, h);
      if (event.shiftKey && g.hx !== 0.5 && g.hy !== 0.5) {
        const k = Math.max(w / s0.width, h / s0.height);
        w = s0.width * k;
        h = s0.height * k;
      }
      w = r1(w);
      h = r1(h);
      const world = applyMatrix(g.matrix, fixed.x, fixed.y);
      const nf = { x: g.hx === 0.5 ? w / 2 : (1 - g.hx) * w, y: g.hy === 0.5 ? h / 2 : (1 - g.hy) * h };
      const ax = s0.anchorX * w;
      const ay = s0.anchorY * h;
      const [a, b, c, d] = g.matrix;
      const vx = nf.x - ax;
      const vy = nf.y - ay;
      const nx = world.x - ax - (a * vx + c * vy);
      const ny = world.y - ay - (b * vx + d * vy);
      const resized = { ...shiftAnimated(shiftAnimated(g.layer, "x", r1(nx - s0.x)), "y", r1(ny - s0.y)), width: w, height: h };
      state.live((design) => patchLayer(design, g.id, () => resized));
      return;
    }
    if (g.kind === "rotate") {
      const angle = Math.atan2(p.y - g.pivot.y, p.x - g.pivot.x);
      let delta = ((angle - g.angle0) * 180) / Math.PI;
      if (event.shiftKey) delta = Math.round((g.state.rotation + delta) / 15) * 15 - g.state.rotation;
      const rotated = shiftAnimated(g.layer, "rotation", r1(delta));
      state.live((design) => patchLayer(design, g.id, () => rotated));
      return;
    }
    if (g.kind === "anchor") {
      const s0 = g.state;
      const q = applyMatrix(g.inverse, p.x, p.y);
      const snap01 = (v: number) => {
        const clamped = Math.max(0, Math.min(1, v));
        for (const target of [0, 0.5, 1]) if (Math.abs(clamped - target) < 0.04) return target;
        return Math.round(clamped * 1000) / 1000;
      };
      const nax = snap01(q.x / s0.width);
      const nay = snap01(q.y / s0.height);
      const ax = s0.anchorX * s0.width;
      const ay = s0.anchorY * s0.height;
      const ux = ax - nax * s0.width;
      const uy = ay - nay * s0.height;
      const [a, b, c, d] = g.matrix;
      const dx = ux - (a * ux + c * uy);
      const dy = uy - (b * ux + d * uy);
      const next = { ...shiftAnimated(shiftAnimated(g.layer, "x", r1(dx)), "y", r1(dy)), anchorX: nax, anchorY: nay };
      state.live((design) => patchLayer(design, g.id, () => next));
      return;
    }
    if (g.kind === "avatar-move") {
      const a = g.avatar;
      const x = Math.max(0, Math.min(design.canvas.width - a.size, a.x + p.x - g.start.x));
      const y = Math.max(0, Math.min(design.canvas.height - a.size, a.y + p.y - g.start.y));
      state.live((d) => ({ ...d, avatar: { ...a, x: r1(x), y: r1(y) } }));
      return;
    }
    if (g.kind === "avatar-resize") {
      const a = g.avatar;
      const fx = a.x + (1 - g.hx) * a.size;
      const fy = a.y + (1 - g.hy) * a.size;
      const wx = g.hx === 0.5 ? a.size : g.hx === 1 ? p.x - fx : fx - p.x;
      const wy = g.hy === 0.5 ? a.size : g.hy === 1 ? p.y - fy : fy - p.y;
      let next = Math.max(8, g.hx === 0.5 ? wy : g.hy === 0.5 ? wx : Math.max(wx, wy));
      const maxX = g.hx === 0 ? fx : design.canvas.width - (g.hx === 0.5 ? a.x : fx);
      const maxY = g.hy === 0 ? fy : design.canvas.height - (g.hy === 0.5 ? a.y : fy);
      next = r1(Math.min(next, maxX, maxY));
      const x = g.hx === 0 ? fx - next : g.hx === 0.5 ? a.x + (a.size - next) / 2 : fx;
      const y = g.hy === 0 ? fy - next : g.hy === 0.5 ? a.y + (a.size - next) / 2 : fy;
      state.live((d) => ({ ...d, avatar: { ...a, size: next, x: r1(Math.max(0, x)), y: r1(Math.max(0, y)) } }));
    }
  };

  const onPointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    const g = gesture.current;
    gesture.current = null;
    setGuides({});
    setPanning(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    if (g && g.kind !== "pan") api.getState().end();
  };

  const zoomBy = (factor: number) => {
    const cx = size.width / 2;
    const cy = size.height / 2;
    const zoom = Math.max(0.05, Math.min(16, view.zoom * factor));
    setCustomView({ zoom, panX: cx - ((cx - view.panX) * zoom) / view.zoom, panY: cy - ((cy - view.panY) * zoom) / view.zoom });
  };
  const actualSize = () => {
    const cw = design.canvas.width;
    const ch = design.canvas.height;
    setCustomView({ zoom: 1, panX: (size.width - cw) / 2, panY: (size.height - ch) / 2 });
  };

  // ── 叠加层几何 ──
  let selection: React.ReactNode = null;
  if (selectedState && selectedLayer) {
    const corners = layerCorners(selectedState).map(toScreen);
    const matrix = layerMatrix(selectedState);
    const isLocked = Boolean(locked[selectedLayer.id]);
    const handlePoint = (hx: number, hy: number) => toScreen(applyMatrix(matrix, hx * selectedState.width, hy * selectedState.height));
    const anchor = handlePoint(selectedState.anchorX, selectedState.anchorY);
    const top = handlePoint(0.5, 0);
    const center = handlePoint(0.5, 0.5);
    const ux = top.x - center.x;
    const uy = top.y - center.y;
    const len = Math.hypot(ux, uy) || 1;
    const rotateHandle = { x: top.x + (ux / len) * 26, y: top.y + (uy / len) * 26 };
    selection = (
      <g>
        <polygon
          points={corners.map((c) => `${c.x},${c.y}`).join(" ")}
          fill="none"
          stroke={isLocked ? "#a1a1aa" : "#6366f1"}
          strokeWidth={1.5}
          strokeDasharray={isLocked ? "4 3" : undefined}
        />
        {editable && !isLocked ? (
          <>
            <line x1={top.x} y1={top.y} x2={rotateHandle.x} y2={rotateHandle.y} stroke="#6366f1" strokeWidth={1.5} />
            <circle
              data-handle="rotate"
              cx={rotateHandle.x}
              cy={rotateHandle.y}
              r={6}
              fill="#ffffff"
              stroke="#6366f1"
              strokeWidth={1.5}
              style={{ cursor: "grab" }}
            />
            {HANDLES.map((h) => {
              const pt = handlePoint(h.hx, h.hy);
              return (
                <rect
                  key={h.key}
                  data-handle={h.key}
                  x={pt.x - 4.5}
                  y={pt.y - 4.5}
                  width={9}
                  height={9}
                  rx={2}
                  fill="#ffffff"
                  stroke="#6366f1"
                  strokeWidth={1.5}
                  style={{ cursor: h.cursor }}
                />
              );
            })}
            <g data-handle="anchor" style={{ cursor: "move" }}>
              <circle cx={anchor.x} cy={anchor.y} r={7} fill="rgba(255,255,255,0.01)" stroke="#f59e0b" strokeWidth={1.5} />
              <line x1={anchor.x - 10} y1={anchor.y} x2={anchor.x + 10} y2={anchor.y} stroke="#f59e0b" strokeWidth={1.5} />
              <line x1={anchor.x} y1={anchor.y - 10} x2={anchor.x} y2={anchor.y + 10} stroke="#f59e0b" strokeWidth={1.5} />
            </g>
          </>
        ) : null}
      </g>
    );
  }

  let avatarOverlay: React.ReactNode = null;
  if (design.avatar) {
    const a = design.avatar;
    const tl = toScreen({ x: a.x, y: a.y });
    const sizePx = a.size * view.zoom;
    const active = selected === AVATAR_ID;
    const radius = a.shape === "circle" ? sizePx / 2 : a.shape === "rounded" ? (a.radius ?? 0) * sizePx : 0;
    avatarOverlay = (
      <g>
        <rect
          x={tl.x}
          y={tl.y}
          width={sizePx}
          height={sizePx}
          rx={radius}
          fill="none"
          stroke={active ? "#10b981" : "rgba(16,185,129,0.55)"}
          strokeWidth={active ? 1.5 : 1}
          strokeDasharray="5 4"
        />
        {active && editable
          ? HANDLES.filter((h) => h.hx !== 0.5 && h.hy !== 0.5).map((h) => (
              <rect
                key={h.key}
                data-handle={h.key}
                x={tl.x + h.hx * sizePx - 4.5}
                y={tl.y + h.hy * sizePx - 4.5}
                width={9}
                height={9}
                rx={2}
                fill="#ffffff"
                stroke="#10b981"
                strokeWidth={1.5}
                style={{ cursor: h.cursor }}
              />
            ))
          : null}
      </g>
    );
  }

  const guideLines = (
    <g>
      {guides.x !== undefined ? (
        <line
          x1={toScreen({ x: guides.x, y: 0 }).x}
          x2={toScreen({ x: guides.x, y: 0 }).x}
          y1={0}
          y2={size.height}
          stroke="#ec4899"
          strokeWidth={1}
        />
      ) : null}
      {guides.y !== undefined ? (
        <line
          y1={toScreen({ x: 0, y: guides.y }).y}
          y2={toScreen({ x: 0, y: guides.y }).y}
          x1={0}
          x2={size.width}
          stroke="#ec4899"
          strokeWidth={1}
        />
      ) : null}
    </g>
  );

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden bg-muted/60">
      <div
        ref={containerRef}
        className={cn("absolute inset-0 touch-none select-none", spaceDown || panning ? "cursor-grab" : "cursor-default", panning && "cursor-grabbing")}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
      >
        <canvas ref={canvasRef} className="absolute inset-0" style={{ width: size.width, height: size.height }} />
        <svg className="absolute inset-0" width={size.width} height={size.height}>
          {avatarOverlay}
          {selection}
          {guideLines}
        </svg>
      </div>

      <div className="absolute bottom-3 left-3 flex items-center gap-1 rounded-lg border bg-card p-1 shadow-sm">
        <Button size="icon-xs" variant="ghost" onClick={() => zoomBy(1 / 1.25)} aria-label="缩小">
          <Minus />
        </Button>
        <button type="button" className="w-12 text-center text-xs tabular-nums hover:text-foreground" onClick={actualSize} title="实际大小">
          {Math.round(view.zoom * 100)}%
        </button>
        <Button size="icon-xs" variant="ghost" onClick={() => zoomBy(1.25)} aria-label="放大">
          <Plus />
        </Button>
        <Button size="icon-xs" variant="ghost" onClick={() => setCustomView(null)} aria-label="适应画布" title="适应画布">
          <Maximize />
        </Button>
        <span className="mx-1 h-4 w-px bg-border" />
        <ToggleGroup type="single" size="sm" value={backdrop} onValueChange={(value) => value && setBackdrop(value as Backdrop)}>
          <ToggleGroupItem value="checker" className="h-6 px-2 text-[11px]">
            透明
          </ToggleGroupItem>
          <ToggleGroupItem value="light" className="h-6 px-2 text-[11px]">
            浅色
          </ToggleGroupItem>
          <ToggleGroupItem value="dark" className="h-6 px-2 text-[11px]">
            深色
          </ToggleGroupItem>
        </ToggleGroup>
        {design.avatar ? (
          <>
            <span className="mx-1 h-4 w-px bg-border" />
            <Button
              size="xs"
              variant={showAvatar ? "secondary" : "ghost"}
              onClick={() => setShowAvatar((value) => !value)}
              title="示例头像"
            >
              <Scan />
              头像
            </Button>
          </>
        ) : null}
      </div>
      <div className="pointer-events-none absolute right-3 bottom-3 rounded-md border bg-card px-2 py-1 text-[11px] tabular-nums text-muted-foreground">
        {design.canvas.width} × {design.canvas.height}
      </div>
    </div>
  );
}
