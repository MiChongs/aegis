import type {
  AssetRef,
  CosmeticAsset,
  CosmeticCatalog,
  CosmeticSlot,
  Design,
  Easing,
  Keyframe,
  Layer,
  LayerType,
  NameStyle,
  Plate,
  ShapeKind,
  Track,
  TrackProperty
} from "@/lib/api/cosmetics";
import { isDesignColor } from "../renderer/color";
import { layerStateAt, sampleTrack, trackOf } from "../renderer/timeline";

/**
 * 设计稿的纯函数操作：建图层、打关键帧、动效预设、签名、本地校验。全部返回新对象，不改入参
 * （撤销栈存的是快照，改了旧快照撤销就回不去了）。
 */

export const AVATAR_ID = "__avatar";
/** 铭牌的文字区域在设计器里也是一个可选中的对象（与头像同理，不是图层） */
export const PLATE_ID = "__plate";

/** 铭牌 template 里的占位：{value} 号码 / 有效期，{tier} 会员标签名（只有会员铭牌） */
export const PLATE_VALUE = "{value}";
export const PLATE_TIER = "{tier}";

const countOf = (text: string, token: string) => text.split(token).length - 1;

/**
 * template 的问题；没有问题返回 null（与 design.go 的校验同口径）：
 * 号码铭牌恰好一个 {value}、不能有 {tier}；会员铭牌 {value}、{tier} 各最多一次且至少一个。
 */
export function plateTemplateIssue(template: string, slot: CosmeticSlot, maxRunes = 32): string | null {
  const runes = Array.from(template).length;
  if (runes < 1 || runes > maxRunes) return `文字模板需要 1–${maxRunes} 字`;
  const values = countOf(template, PLATE_VALUE);
  const tiers = countOf(template, PLATE_TIER);
  if (slot === "vip_plate") {
    if (values > 1 || tiers > 1 || values + tiers === 0) return "会员铭牌的文字模板里 {value}、{tier} 各最多一次，且至少有一个";
    return null;
  }
  if (tiers > 0) return "只有会员铭牌可以使用 {tier}";
  if (values !== 1) return "文字模板需要恰好包含一个 {value}";
  return null;
}

/** 铭牌的空白文字区域：横向留边、纵向居中，字号取区域高度的 2/3 */
export function defaultPlate(canvas: { width: number; height: number }, template: string): Plate {
  const w = canvas.width || 360;
  const h = canvas.height || 96;
  const height = Math.round(h * 0.625);
  return {
    x: Math.round(w / 15),
    y: Math.round((h - height) / 2),
    width: Math.round(w - (w / 15) * 2),
    height,
    template,
    align: "center",
    fontSize: Math.round(height * 2 / 3),
    fontWeight: 700,
    letterSpacing: 0.04,
    font: "sans"
  };
}

/** 把伸缩段收回画布内并保持 x0 < x1（改画布宽、拖参考线后） */
export function clampStretch(stretch: { x0: number; x1: number }, canvasWidth: number) {
  const w = Math.max(1, canvasWidth);
  const x1 = Math.max(1, Math.min(w, stretch.x1));
  const x0 = Math.max(0, Math.min(x1 - 1, stretch.x0));
  return { x0, x1 };
}

/** 伸缩段的问题；没有问题返回 null（与 design.go 同口径：0 ≤ x0 < x1 ≤ canvas.width） */
export function plateStretchIssue(stretch: { x0: number; x1: number } | undefined, canvasWidth: number): string | null {
  if (!stretch) return null;
  const { x0, x1 } = stretch;
  if (!Number.isFinite(x0) || !Number.isFinite(x1) || x0 < 0 || x1 > canvasWidth + 0.01 || !(x0 < x1)) {
    return `伸缩段需要满足 0 ≤ 起点 < 终点 ≤ ${canvasWidth}`;
  }
  return null;
}

/** 把文字区域夹回画布内（拖动、改画布尺寸后）；伸缩段一并收回 */
export function clampPlate(plate: Plate, canvas: { width: number; height: number }): Plate {
  const width = Math.max(1, Math.min(plate.width, canvas.width));
  const height = Math.max(1, Math.min(plate.height, canvas.height));
  return {
    ...plate,
    width,
    height,
    x: Math.max(0, Math.min(canvas.width - width, plate.x)),
    y: Math.max(0, Math.min(canvas.height - height, plate.y)),
    stretch: plate.stretch ? clampStretch(plate.stretch, canvas.width) : plate.stretch
  };
}

export const PROPERTY_LABEL: Record<TrackProperty, string> = {
  x: "X",
  y: "Y",
  scale: "缩放",
  scaleX: "横向缩放",
  scaleY: "纵向缩放",
  rotation: "旋转",
  opacity: "不透明度",
  hue: "色相",
  sweep: "扫过角度",
  dashOffset: "虚线偏移"
};

/** 可动画属性的取值范围（与 design.go 的 propertyRanges 同值） */
export const PROPERTY_RANGE: Record<TrackProperty, [number, number]> = {
  x: [-4096, 8192],
  y: [-4096, 8192],
  scale: [0, 20],
  scaleX: [0, 20],
  scaleY: [0, 20],
  rotation: [-36000, 36000],
  opacity: [0, 1],
  hue: [-3600, 3600],
  sweep: [0, 360],
  dashOffset: [-100000, 100000]
};

export const PROPERTY_ORDER: TrackProperty[] = ["x", "y", "scale", "scaleX", "scaleY", "rotation", "opacity", "hue", "sweep", "dashOffset"];

export const LAYER_TYPE_LABEL: Record<LayerType, string> = {
  image: "图片",
  sprite: "序列帧",
  text: "文字",
  shape: "形状",
  particles: "粒子"
};

export const SHAPE_LABEL: Record<ShapeKind, string> = {
  rect: "矩形",
  circle: "圆形",
  ring: "圆环",
  star: "星形",
  polygon: "多边形"
};

export const EASING_LABEL: Record<Easing, string> = {
  linear: "线性",
  easeIn: "缓入",
  easeOut: "缓出",
  easeInOut: "缓入缓出",
  step: "阶跃",
  backOut: "回弹",
  elastic: "弹性"
};

export function cloneDesign(design: Design): Design {
  return JSON.parse(JSON.stringify(design)) as Design;
}

const signatures = new WeakMap<Design, string>();

/** 语义签名：素材只比编号（地址每次下发都可能不同）。按对象缓存，选择器每帧调用也不重复序列化 */
export function designSignature(design: Design | null | undefined) {
  if (!design) return "";
  const hit = signatures.get(design);
  if (hit !== undefined) return hit;
  const value = computeSignature(design);
  signatures.set(design, value);
  return value;
}

function computeSignature(design: Design) {
  const assets = design.assets
    ? Object.fromEntries(
        Object.entries(design.assets)
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([key, ref]) => [key, ref.id])
      )
    : undefined;
  return JSON.stringify({ ...design, assets });
}

/** 图层编号：字母数字下划线短横线，最长 32 位，在本稿内唯一 */
export function newLayerId(design: Design, prefix: string) {
  const taken = new Set(design.layers.map((layer) => layer.id));
  const base = prefix.replace(/[^A-Za-z0-9_-]/g, "").slice(0, 20) || "layer";
  for (let i = 1; i < 10_000; i++) {
    const id = `${base}_${i}`;
    if (!taken.has(id)) return id;
  }
  return `${base}_${Date.now().toString(36)}`;
}

function uniqueName(design: Design, base: string) {
  const taken = new Set(design.layers.map((layer) => layer.name));
  if (!taken.has(base)) return base;
  for (let i = 2; i < 1000; i++) {
    const name = `${base} ${i}`;
    if (!taken.has(name)) return name;
  }
  return base;
}

function baseLayer(design: Design, type: LayerType, prefix: string, name: string, w: number, h: number): Layer {
  const cw = design.canvas.width || 300;
  const ch = design.canvas.height || 300;
  return {
    id: newLayerId(design, prefix),
    name: uniqueName(design, name),
    type,
    // 铭牌槽位的图层缺省画在文字之下
    placement: design.plate ? "below" : "above",
    x: Math.round((cw - w) / 2),
    y: Math.round((ch - h) / 2),
    width: Math.round(w),
    height: Math.round(h),
    anchorX: 0.5,
    anchorY: 0.5,
    rotation: 0,
    scale: 1,
    opacity: 1,
    blend: "normal"
  };
}

export type NewLayerKind = { type: "text" } | { type: "particles" } | { type: "shape"; shape: ShapeKind };

/** 新建一个图层，放在画布中央 */
export function createLayer(design: Design, kind: NewLayerKind): Layer {
  const cw = design.canvas.width || 300;
  const ch = design.canvas.height || 300;
  const side = Math.min(cw, ch);
  if (kind.type === "text") {
    const fontSize = Math.max(12, Math.round(side * 0.14));
    return {
      ...baseLayer(design, "text", "text", "文字", side * 0.7, fontSize * 1.6),
      text: "文字",
      fontSize,
      fontWeight: 700,
      color: "#FFFFFFFF",
      align: "center",
      letterSpacing: 0
    };
  }
  if (kind.type === "particles") {
    return {
      ...baseLayer(design, "particles", "particles", "粒子", cw, ch),
      x: 0,
      y: 0,
      particles: {
        emitter: design.avatar ? "ring" : "rect",
        radius: design.avatar ? Math.round(design.avatar.size / 2 + 6) : undefined,
        outward: Boolean(design.avatar),
        shape: "circle",
        colors: ["#FFFFFF", "#FDE68A"],
        count: 30,
        lifetime: 2000,
        speed: 24,
        direction: 0,
        spread: 40,
        sizeMin: Math.max(1, Math.round(side / 100)),
        sizeMax: Math.max(2, Math.round(side / 40)),
        fade: true,
        shrink: false,
        seed: Math.floor(Math.random() * 1_000_000)
      }
    };
  }
  const shape = kind.shape;
  if (shape === "ring") {
    const size = design.avatar ? design.avatar.size + 24 : side * 0.8;
    const layer = baseLayer(design, "shape", "ring", SHAPE_LABEL.ring, size, size);
    if (design.avatar) {
      layer.x = design.avatar.x + design.avatar.size / 2 - size / 2;
      layer.y = design.avatar.y + design.avatar.size / 2 - size / 2;
    }
    return { ...layer, shape, stroke: "#FFD86B", strokeWidth: Math.max(2, Math.round(side / 50)), sweep: 360, startAngle: 0 };
  }
  const size = side * (shape === "rect" ? 0.5 : 0.4);
  const layer = baseLayer(design, "shape", shape, SHAPE_LABEL[shape], size, size);
  return {
    ...layer,
    shape,
    fill: shape === "star" ? "#FFE08A" : "#3B82F6",
    cornerRadius: shape === "rect" ? Math.round(size * 0.12) : undefined,
    sides: shape === "star" ? 5 : shape === "polygon" ? 6 : undefined,
    innerRatio: shape === "star" ? 0.45 : undefined
  };
}

/** 素材在设计稿 assets 表里的键：同一素材复用同一个键 */
export function assetKeyFor(design: Design, asset: Pick<CosmeticAsset, "id">) {
  const found = Object.entries(design.assets ?? {}).find(([, ref]) => ref.id === asset.id);
  if (found) return found[0];
  const taken = new Set(Object.keys(design.assets ?? {}));
  let key = `a${asset.id}`;
  let i = 2;
  while (taken.has(key)) key = `a${asset.id}_${i++}`;
  return key;
}

export function withAsset(design: Design, asset: CosmeticAsset): { design: Design; key: string } {
  const key = assetKeyFor(design, asset);
  const ref: AssetRef = {
    id: asset.id,
    url: asset.url,
    width: asset.width,
    height: asset.height,
    contentType: asset.contentType,
    sha256: asset.sha256
  };
  return { design: { ...design, assets: { ...(design.assets ?? {}), [key]: ref } }, key };
}

/** 用素材建一个图片 / 序列帧图层：按素材比例放进画布 60% 的框里 */
export function createAssetLayer(design: Design, asset: CosmeticAsset, key: string, kind: "image" | "sprite"): Layer {
  const cw = design.canvas.width || 300;
  const ch = design.canvas.height || 300;
  let columns = 1;
  let rows = 1;
  let aw = asset.width || 100;
  let ah = asset.height || 100;
  if (kind === "sprite") {
    // 猜网格：横条按正方形帧切，否则 4×4
    const ratio = aw / Math.max(1, ah);
    if (ratio >= 2 && Number.isInteger(Math.round(ratio))) {
      columns = Math.min(16, Math.round(ratio));
    } else if (ratio <= 0.5) {
      rows = Math.min(16, Math.round(1 / ratio));
    } else {
      columns = 4;
      rows = 4;
    }
    aw /= columns;
    ah /= rows;
  }
  const fit = Math.min((cw * 0.6) / aw, (ch * 0.6) / ah, 1e6);
  const w = Math.max(1, aw * fit);
  const h = Math.max(1, ah * fit);
  const layer = baseLayer(design, kind, kind === "sprite" ? "sprite" : "image", asset.name || LAYER_TYPE_LABEL[kind], w, h);
  if (kind === "sprite") {
    return { ...layer, asset: key, fit: "contain", columns, rows, frameCount: columns * rows, fps: 12 };
  }
  return { ...layer, asset: key, fit: "contain" };
}

export function patchLayer(design: Design, id: string, patch: Partial<Layer> | ((layer: Layer) => Layer)): Design {
  return {
    ...design,
    layers: design.layers.map((layer) => (layer.id === id ? (typeof patch === "function" ? patch(layer) : { ...layer, ...patch }) : layer))
  };
}

export function duplicateLayer(design: Design, layer: Layer): Layer {
  const copy = JSON.parse(JSON.stringify(layer)) as Layer;
  copy.id = newLayerId(design, layer.id.replace(/_\d+$/, ""));
  copy.name = uniqueName(design, `${layer.name || LAYER_TYPE_LABEL[layer.type]} 副本`);
  copy.x += 12;
  copy.y += 12;
  if (copy.tracks) {
    copy.tracks = copy.tracks.map((track) =>
      track.property === "x" || track.property === "y"
        ? { ...track, keyframes: track.keyframes.map((kf) => ({ ...kf, v: kf.v + 12 })) }
        : track
    );
  }
  return copy;
}

/** 插入到某个图层之上（数组里紧跟其后），没有参照时放到最上面 */
export function insertLayer(design: Design, layer: Layer, afterId?: string | null): Design {
  const layers = [...design.layers];
  const index = afterId ? layers.findIndex((item) => item.id === afterId) : -1;
  if (index >= 0) {
    layers.splice(index + 1, 0, { ...layer, placement: layers[index].placement ?? layer.placement });
  } else {
    layers.push(layer);
  }
  return { ...design, layers };
}

export function removeLayer(design: Design, id: string): Design {
  return { ...design, layers: design.layers.filter((layer) => layer.id !== id) };
}

/** 这个图层能做哪些属性的动画（sweep 只给圆环，dashOffset 要先有虚线） */
export function animatableProperties(layer: Layer): TrackProperty[] {
  return PROPERTY_ORDER.filter((property) => {
    if (property === "sweep") return layer.type === "shape" && layer.shape === "ring";
    if (property === "dashOffset") return layer.type === "shape" && Boolean(layer.dash?.length);
    return true;
  });
}

/** 属性在没有轨道时的静态值 */
export function staticValue(layer: Layer, property: TrackProperty): number {
  switch (property) {
    case "x":
      return layer.x;
    case "y":
      return layer.y;
    case "scale":
      return layer.scale ?? 1;
    case "rotation":
      return layer.rotation ?? 0;
    case "opacity":
      return layer.opacity ?? 1;
    case "sweep":
      return layer.sweep ?? 360;
    case "dashOffset":
      return layer.dashOffset ?? 0;
    case "hue":
      return 0;
    default:
      return 1;
  }
}

/** 属性在时刻 t 的值（有轨道取轨道，否则静态值） */
export function valueAt(layer: Layer, property: TrackProperty, t: number): number {
  const track = trackOf(layer, property);
  const sampled = track ? sampleTrack(track, t) : undefined;
  return sampled ?? staticValue(layer, property);
}

function setStatic(layer: Layer, property: TrackProperty, value: number): Layer {
  switch (property) {
    case "x":
    case "y":
    case "scale":
    case "rotation":
    case "opacity":
    case "sweep":
    case "dashOffset":
      return { ...layer, [property]: value };
    default:
      return layer;
  }
}

function clampValue(property: TrackProperty, value: number) {
  const [min, max] = PROPERTY_RANGE[property];
  return Math.max(min, Math.min(max, value));
}

export function sortFrames(frames: Keyframe[]) {
  return [...frames].sort((a, b) => a.t - b.t);
}

/** 在时刻 t 设一个关键帧（同一时刻已有则改值），没有轨道就新建一条 */
export function setKeyframe(layer: Layer, property: TrackProperty, t: number, v: number, easing?: Easing): Layer {
  const time = Math.max(0, Math.round(t));
  const value = clampValue(property, v);
  const tracks = [...(layer.tracks ?? [])];
  const index = tracks.findIndex((track) => track.property === property);
  if (index < 0) {
    tracks.push({ property, keyframes: [{ t: time, v: value, easing: easing ?? "linear" }] });
  } else {
    const frames = [...tracks[index].keyframes];
    const at = frames.findIndex((kf) => Math.abs(kf.t - time) < 1);
    if (at >= 0) frames[at] = { ...frames[at], v: value, easing: easing ?? frames[at].easing };
    else frames.push({ t: time, v: value, easing: easing ?? "easeInOut" });
    tracks[index] = { ...tracks[index], keyframes: sortFrames(frames) };
  }
  return { ...layer, tracks };
}

/**
 * 编辑一个可动画属性：有轨道时改（或新建）当前时刻的关键帧，没有轨道时改静态值；
 * 横向 / 纵向缩放与色相没有静态值，第一次改就建轨道。
 */
export function editAnimated(layer: Layer, property: TrackProperty, value: number, t: number, animate: boolean): Layer {
  const track = trackOf(layer, property);
  if (track && animate) return setKeyframe(layer, property, t, value);
  if (property === "scaleX" || property === "scaleY" || property === "hue") {
    return animate ? setKeyframe(layer, property, t, value) : layer;
  }
  return setStatic(layer, property, clampValue(property, value));
}

/** 整条轨道平移（拖动有 x 轨道的图层时，整段路径跟着走） */
export function shiftAnimated(layer: Layer, property: "x" | "y" | "rotation", delta: number): Layer {
  const next: Layer = { ...layer, [property]: (layer[property] ?? 0) + delta };
  if (!layer.tracks?.some((track) => track.property === property)) return next;
  next.tracks = layer.tracks.map((track) =>
    track.property === property
      ? { ...track, keyframes: track.keyframes.map((kf) => ({ ...kf, v: clampValue(property, kf.v + delta) })) }
      : track
  );
  return next;
}

export function removeTrack(layer: Layer, property: TrackProperty): Layer {
  const tracks = (layer.tracks ?? []).filter((track) => track.property !== property);
  return { ...layer, tracks: tracks.length ? tracks : undefined };
}

export function updateKeyframe(layer: Layer, property: TrackProperty, index: number, patch: Partial<Keyframe>): Layer {
  return {
    ...layer,
    tracks: (layer.tracks ?? []).map((track) => {
      if (track.property !== property) return track;
      const frames = track.keyframes.map((kf, i) => (i === index ? { ...kf, ...patch } : kf));
      return { ...track, keyframes: frames };
    })
  };
}

export function removeKeyframe(layer: Layer, property: TrackProperty, index: number): Layer {
  const tracks = (layer.tracks ?? [])
    .map((track) => (track.property === property ? { ...track, keyframes: track.keyframes.filter((_, i) => i !== index) } : track))
    .filter((track) => track.keyframes.length > 0);
  return { ...layer, tracks: tracks.length ? tracks : undefined };
}

/** 改时长时把落在外面的关键帧收进来 */
export function clampDuration(design: Design, duration: number): Design {
  return {
    ...design,
    duration,
    layers: design.layers.map((layer) =>
      layer.tracks
        ? {
            ...layer,
            tracks: layer.tracks.map((track) => ({
              ...track,
              keyframes: sortFrames(track.keyframes.map((kf) => ({ ...kf, t: Math.min(kf.t, duration) })))
            }))
          }
        : layer
    )
  };
}

// ── 动效预设 ──

export type PresetKey = "spin" | "breathe" | "pulse" | "float" | "hue" | "dashRun" | "sweepIn";

export const PRESETS: Array<{ key: PresetKey; label: string }> = [
  { key: "spin", label: "旋转一圈" },
  { key: "breathe", label: "呼吸" },
  { key: "pulse", label: "脉冲缩放" },
  { key: "float", label: "浮动" },
  { key: "hue", label: "色相流转" },
  { key: "dashRun", label: "流光跑边" },
  { key: "sweepIn", label: "圆环扫入" }
];

export function presetAvailable(layer: Layer, key: PresetKey) {
  if (key === "dashRun") return layer.type === "shape" && layer.shape !== undefined;
  if (key === "sweepIn") return layer.type === "shape" && layer.shape === "ring";
  return true;
}

function withTrack(layer: Layer, track: Track): Layer {
  const tracks = (layer.tracks ?? []).filter((item) => item.property !== track.property);
  return { ...layer, tracks: [...tracks, track] };
}

/** 套一个动效预设，返回新图层；duration 为 0 的设计稿由调用方先设一个时长 */
export function applyPreset(layer: Layer, key: PresetKey, duration: number): Layer {
  const d = Math.max(1, duration);
  const half = Math.round(d / 2);
  switch (key) {
    case "spin": {
      const r = layer.rotation ?? 0;
      return withTrack(layer, { property: "rotation", keyframes: [{ t: 0, v: r, easing: "linear" }, { t: d, v: r + 360, easing: "linear" }] });
    }
    case "breathe": {
      const o = layer.opacity ?? 1;
      return withTrack(layer, {
        property: "opacity",
        keyframes: [
          { t: 0, v: Math.max(0, o * 0.55), easing: "easeInOut" },
          { t: half, v: o, easing: "easeInOut" },
          { t: d, v: Math.max(0, o * 0.55), easing: "easeInOut" }
        ]
      });
    }
    case "pulse": {
      const s = layer.scale ?? 1;
      return withTrack(layer, {
        property: "scale",
        keyframes: [
          { t: 0, v: s, easing: "easeInOut" },
          { t: half, v: Math.min(20, s * 1.12), easing: "easeInOut" },
          { t: d, v: s, easing: "easeInOut" }
        ]
      });
    }
    case "float": {
      const amplitude = Math.max(2, Math.round(layer.height * 0.06));
      return withTrack(layer, {
        property: "y",
        keyframes: [
          { t: 0, v: layer.y, easing: "easeInOut" },
          { t: half, v: layer.y - amplitude, easing: "easeInOut" },
          { t: d, v: layer.y, easing: "easeInOut" }
        ]
      });
    }
    case "hue":
      return withTrack(layer, { property: "hue", keyframes: [{ t: 0, v: 0, easing: "linear" }, { t: d, v: 360, easing: "linear" }] });
    case "dashRun": {
      const base = layer.dash?.length ? layer.dash : [Math.max(6, Math.round(layer.width / 12)), Math.max(4, Math.round(layer.width / 20))];
      const cycle = base.reduce((sum, v) => sum + v, 0) * 4;
      const stroke = layer.stroke || "#FFFFFF";
      return withTrack(
        { ...layer, dash: base, stroke, strokeWidth: layer.strokeWidth && layer.strokeWidth > 0 ? layer.strokeWidth : 3 },
        { property: "dashOffset", keyframes: [{ t: 0, v: 0, easing: "linear" }, { t: d, v: -cycle, easing: "linear" }] }
      );
    }
    case "sweepIn":
      return withTrack(layer, {
        property: "sweep",
        keyframes: [
          { t: 0, v: 0, easing: "easeOut" },
          { t: Math.round(d * 0.4), v: 360, easing: "easeOut" },
          { t: d, v: 360, easing: "linear" }
        ]
      });
  }
}

// ── 本地校验（提示用，保存以后端为准） ──

export type DesignIssue = { layerId?: string; message: string };

export function localIssues(design: Design, slot: CosmeticSlot, catalog?: CosmeticCatalog | null): DesignIssue[] {
  const issues: DesignIssue[] = [];
  const limits = catalog?.vocabulary?.limits;
  if (slot === "name_style") {
    const style = design.nameStyle;
    if (!style) return [{ message: "缺少昵称样式" }];
    return nameStyleIssues(style, "昵称");
  }
  const spec = catalog?.slots.find((item) => item.key === slot);
  const plateSlot = slot === "id_plate" || slot === "vip_plate";
  if (spec) {
    const { width, height } = design.canvas;
    if (spec.plate || plateSlot) {
      // 铭牌：任意比例，最长边 minCanvas–maxCanvas
      const longest = Math.max(width, height);
      if (longest < spec.minCanvas || longest > spec.maxCanvas || Math.min(width, height) < 1) {
        issues.push({ message: `画布最长边需要在 ${spec.minCanvas}–${spec.maxCanvas} 之间` });
      }
    } else if (width < spec.minCanvas || width > spec.maxCanvas || height < spec.minCanvas || height > spec.maxCanvas) {
      issues.push({ message: `画布边长需要在 ${spec.minCanvas}–${spec.maxCanvas} 之间` });
    }
    if (spec.square && width !== height) issues.push({ message: "画布必须是正方形" });
  }
  if (plateSlot) {
    const plate = design.plate;
    if (!plate) {
      issues.push({ layerId: PLATE_ID, message: "缺少铭牌文字区域" });
    } else {
      const push = (message: string) => issues.push({ layerId: PLATE_ID, message: `铭牌文字：${message}` });
      const templateIssue = plateTemplateIssue(plate.template ?? "", slot, limits?.maxPlateTemplateRunes ?? 32);
      if (templateIssue) push(templateIssue);
      if (!(plate.width > 0) || !(plate.height > 0) || plate.x < 0 || plate.y < 0 ||
        plate.x + plate.width > design.canvas.width + 0.01 || plate.y + plate.height > design.canvas.height + 0.01) {
        push("文字区域需要完整落在画布内");
      }
      if (!(plate.fontSize > 0)) push("字号需要大于 0");
      if (plate.fontWeight < 100 || plate.fontWeight > 900) push("字重需要在 100–900 之间");
      if (plate.letterSpacing < -0.2 || plate.letterSpacing > 1) push("字间距需要在 -0.2–1 em 之间");
      const stretchIssue = plateStretchIssue(plate.stretch, design.canvas.width);
      if (stretchIssue) push(stretchIssue);
    }
    if (!design.nameStyle) issues.push({ layerId: PLATE_ID, message: "缺少文字样式" });
    else issues.push(...nameStyleIssues(design.nameStyle, "文字").map((issue) => ({ ...issue, layerId: PLATE_ID })));
  }
  const maxDuration = limits?.maxDurationMs ?? 20000;
  if (design.duration < 0 || design.duration > maxDuration) issues.push({ message: `动画时长需要在 0–${maxDuration} 毫秒之间` });
  if (design.avatar) {
    const a = design.avatar;
    if (a.size < 8 || a.x < 0 || a.y < 0 || a.x + a.size > design.canvas.width + 0.01 || a.y + a.size > design.canvas.height + 0.01) {
      issues.push({ layerId: AVATAR_ID, message: "头像区域需要完整落在画布内，且边长不小于 8" });
    }
  }
  if (design.layers.length > (limits?.maxLayers ?? 40)) issues.push({ message: `最多 ${limits?.maxLayers ?? 40} 个图层` });
  const seen = new Set<string>();
  for (const layer of design.layers) {
    const push = (message: string) => issues.push({ layerId: layer.id, message: `${layer.name || layer.id}：${message}` });
    if (!/^[A-Za-z0-9_-]{1,32}$/.test(layer.id)) push("图层编号只能用字母、数字、下划线与短横线");
    if (seen.has(layer.id)) push("图层编号重复");
    seen.add(layer.id);
    if (!(layer.width > 0) || !(layer.height > 0)) push("宽高需要大于 0");
    if ((layer.type === "image" || layer.type === "sprite") && (!layer.asset || !design.assets?.[layer.asset])) push("没有选择素材");
    if (layer.type === "text" && !(layer.text ?? "").trim()) push("文字不能为空");
    if (layer.type === "text" && Array.from(layer.text ?? "").length > (limits?.maxTextRunes ?? 32)) push(`文字不能超过 ${limits?.maxTextRunes ?? 32} 字`);
    if (layer.type === "shape") {
      if (layer.shape === "ring" && (!layer.stroke || !(layer.strokeWidth && layer.strokeWidth > 0))) push("圆环需要描边颜色与宽度");
      if (layer.shape !== "ring" && !layer.fill && (!layer.stroke || !(layer.strokeWidth && layer.strokeWidth > 0))) push("需要填充色或描边");
    }
    if ((layer.glow ?? 0) > 0 && !layer.glowColor) push("开了发光需要选择发光颜色");
    if (layer.type === "particles") {
      const p = layer.particles;
      if (!p) push("缺少发射器设置");
      else {
        if (p.shape !== "image" && !(p.colors?.length)) push("粒子至少要选一种颜色");
        if (p.shape === "image" && (!p.asset || !design.assets?.[p.asset])) push("图片粒子没有选择素材");
        if (p.sizeMin <= 0 || p.sizeMax < p.sizeMin) push("粒子大小需要大于 0，且最大值不小于最小值");
      }
    }
    const tracks = layer.tracks ?? [];
    if (tracks.length > (limits?.maxTracks ?? 8)) push(`最多 ${limits?.maxTracks ?? 8} 条动画轨道`);
    if (tracks.length && design.duration === 0) push("有动画轨道时时长不能为 0");
    for (const track of tracks) {
      if (track.keyframes.length < 2) push(`${PROPERTY_LABEL[track.property]}至少需要两个关键帧`);
      if (track.keyframes.some((kf) => kf.t > design.duration)) push(`${PROPERTY_LABEL[track.property]}有关键帧超出时长`);
    }
  }
  return issues;
}

function nameStyleIssues(style: NameStyle, subject: string): DesignIssue[] {
  const issues: DesignIssue[] = [];
  if (!isDesignColor(style.color)) issues.push({ message: `${subject}颜色不合法` });
  if (style.effect === "cycle" && ((style.colors?.length ?? 0) < 2 || (style.colors?.length ?? 0) > 6)) {
    issues.push({ message: "循环变色需要 2–6 种颜色" });
  }
  if (style.effect === "glow" && !isDesignColor(style.glowColor)) issues.push({ message: "外发光需要选择发光颜色" });
  const speed = style.speed ?? 2400;
  if (speed < 300 || speed > 20000) issues.push({ message: "效果周期需要在 300–20000 毫秒之间" });
  return issues;
}

/** 当前时刻图层的状态（供变换控件与吸附用） */
export function stateOf(layer: Layer, design: Design, t: number) {
  return layerStateAt(layer, t, design.duration > 0);
}
