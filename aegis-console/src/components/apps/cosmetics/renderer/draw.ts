import type { AvatarSlot, Design, Layer, NameStyle } from "@/lib/api/cosmetics";
import { mixColor, parseDesignColor, rgbaCss, scaleBrightness, type RGBA } from "./color";
import { getImage, resolveAssetUrl, tintedImage } from "./images";
import { sampleParticles } from "./particles";
import { layerMatrix, layerStateAt, loopTime, type LayerState } from "./timeline";

/**
 * 设计稿的 Canvas2D 渲染器（规格 v1，docs/cosmetics.md「设计稿」）。
 *
 * 调用方负责把 ctx 的变换设成「设计稿像素 → 设备像素」（通常是 setTransform(scale, 0, 0, scale, 0, 0)），
 * 并把这个比例作为 pixelRatio 传进来：canvas 的阴影模糊不随变换缩放，发光半径要自己乘上去。
 *
 * 绘制顺序：底色 → placement=below 的图层 → 头像（只有头像框）→ 其余图层；数组顺序即绘制顺序。
 */

export const DESIGN_FONT =
  '"PingFang SC", "HarmonyOS Sans SC", "MiSans", "Microsoft YaHei", "Noto Sans SC", system-ui, -apple-system, sans-serif';

const BLEND: Record<string, GlobalCompositeOperation> = {
  normal: "source-over",
  screen: "screen",
  multiply: "multiply",
  add: "lighter",
  overlay: "overlay"
};

const DEG = Math.PI / 180;

export type AvatarPainter = (ctx: CanvasRenderingContext2D, size: number) => void;

export type RenderOptions = {
  /** 自己的时钟（毫秒），序列帧与粒子按它走 */
  elapsed: number;
  /** 设计稿时刻；不给时取 elapsed mod duration（设计器拖动时间轴时显式给出） */
  time?: number;
  /** 一个设计稿像素对应多少设备像素 */
  pixelRatio: number;
  /** 画不画底色（资料卡） */
  background?: boolean;
  /** 头像框里的头像：图片地址或自定义绘制；null 不画头像 */
  avatar?: { url?: string | null; painter?: AvatarPainter } | null;
  /** 设计器里额外隐藏的图层（不改设计稿） */
  hiddenIds?: ReadonlySet<string>;
  /** 素材缺失时画占位框（设计器） */
  placeholders?: boolean;
  /** 素材地址兜底：设计稿里的引用没有 url 时按编号查（版本历史里的旧版本） */
  assetUrl?: (id: number) => string | undefined;
};

/** 画一整份设计稿（不含昵称样式，昵称样式见 drawNameStyle） */
export function renderDesign(ctx: CanvasRenderingContext2D, design: Design, options: RenderOptions) {
  const width = design.canvas?.width ?? 0;
  const height = design.canvas?.height ?? 0;
  if (!width || !height) return;
  const animate = (design.duration ?? 0) > 0;
  const t = options.time ?? loopTime(design, options.elapsed);

  if (options.background !== false && design.background) {
    const bg = parseDesignColor(design.background);
    if (bg) {
      ctx.fillStyle = rgbaCss(bg);
      ctx.fillRect(0, 0, width, height);
    }
  }

  const layers = design.layers ?? [];
  if (design.avatar) {
    for (const layer of layers) {
      if (layer.placement === "below") drawLayer(ctx, design, layer, t, animate, options);
    }
    if (options.avatar !== null) drawAvatar(ctx, design.avatar, options.avatar ?? {});
    for (const layer of layers) {
      if (layer.placement !== "below") drawLayer(ctx, design, layer, t, animate, options);
    }
  } else {
    for (const layer of layers) drawLayer(ctx, design, layer, t, animate, options);
  }
}

/** 头像：按 avatar.shape 裁剪（circle / rounded（圆角 = radius × size）/ square） */
export function drawAvatar(
  ctx: CanvasRenderingContext2D,
  avatar: AvatarSlot,
  fill: { url?: string | null; painter?: AvatarPainter }
) {
  const { x, y, size } = avatar;
  if (!(size > 0)) return;
  ctx.save();
  ctx.beginPath();
  avatarPath(ctx, avatar);
  ctx.clip();
  ctx.translate(x, y);
  const image = fill.url ? getImage(fill.url) : null;
  if (image && image.naturalWidth > 0) {
    const scale = Math.max(size / image.naturalWidth, size / image.naturalHeight);
    const w = image.naturalWidth * scale;
    const h = image.naturalHeight * scale;
    ctx.drawImage(image, (size - w) / 2, (size - h) / 2, w, h);
  } else {
    (fill.painter ?? paintSampleAvatar)(ctx, size);
  }
  ctx.restore();
}

export function avatarPath(ctx: CanvasRenderingContext2D, avatar: AvatarSlot) {
  const { x, y, size } = avatar;
  if (avatar.shape === "square") {
    ctx.rect(x, y, size, size);
  } else if (avatar.shape === "rounded") {
    ctx.roundRect(x, y, size, size, Math.max(0, Math.min(0.5, avatar.radius ?? 0)) * size);
  } else {
    ctx.arc(x + size / 2, y + size / 2, size / 2, 0, Math.PI * 2);
  }
}

/** 示例头像：纯色底 + 头肩剪影 */
export function paintSampleAvatar(ctx: CanvasRenderingContext2D, size: number) {
  ctx.fillStyle = "#94A3B8";
  ctx.fillRect(0, 0, size, size);
  ctx.fillStyle = "#E2E8F0";
  ctx.beginPath();
  ctx.arc(size / 2, size * 0.4, size * 0.19, 0, Math.PI * 2);
  ctx.fill();
  ctx.beginPath();
  ctx.ellipse(size / 2, size * 0.98, size * 0.36, size * 0.32, 0, Math.PI, 0);
  ctx.fill();
}

/** 一个图层。t 为设计稿时刻，elapsed 为自己的时钟 */
export function drawLayer(
  ctx: CanvasRenderingContext2D,
  design: Design,
  layer: Layer,
  t: number,
  animate: boolean,
  options: RenderOptions
) {
  if (layer.hidden || options.hiddenIds?.has(layer.id)) return;
  const state = layerStateAt(layer, t, animate);
  if (state.opacity <= 0 || state.sx === 0 || state.sy === 0 || !(state.width > 0) || !(state.height > 0)) return;
  ctx.save();
  const m = layerMatrix(state);
  ctx.transform(m[0], m[1], m[2], m[3], m[4], m[5]);
  ctx.globalAlpha *= state.opacity;
  ctx.globalCompositeOperation = BLEND[layer.blend ?? "normal"] ?? "source-over";
  if (state.hue) ctx.filter = `hue-rotate(${state.hue}deg)`;
  // 发光半径是画布像素：乘上显示比例与图层自身的缩放
  const blurScale = options.pixelRatio * Math.sqrt(Math.abs(state.sx * state.sy));
  switch (layer.type) {
    case "image":
    case "sprite":
      drawImageLayer(ctx, design, layer, state, options);
      break;
    case "text":
      drawTextLayer(ctx, layer, state, blurScale);
      break;
    case "shape":
      drawShapeLayer(ctx, layer, state, blurScale);
      break;
    case "particles":
      drawParticleLayer(ctx, design, layer, state, options);
      break;
  }
  ctx.restore();
}

function assetUrlOf(design: Design, key: string | undefined, options: RenderOptions) {
  if (!key) return "";
  const ref = design.assets?.[key];
  if (!ref) return "";
  return ref.url || (ref.id ? (options.assetUrl?.(ref.id) ?? "") : "");
}

function fitRect(fit: string | undefined, sw: number, sh: number, w: number, h: number) {
  if (fit === "fill" || !(sw > 0) || !(sh > 0)) return { x: 0, y: 0, w, h };
  const scale = fit === "cover" ? Math.max(w / sw, h / sh) : Math.min(w / sw, h / sh);
  const dw = sw * scale;
  const dh = sh * scale;
  return { x: (w - dw) / 2, y: (h - dh) / 2, w: dw, h: dh };
}

function drawPlaceholder(ctx: CanvasRenderingContext2D, w: number, h: number) {
  ctx.save();
  ctx.globalCompositeOperation = "source-over";
  ctx.filter = "none";
  ctx.strokeStyle = "rgba(148, 163, 184, 0.9)";
  ctx.fillStyle = "rgba(148, 163, 184, 0.12)";
  const lw = Math.max(1, Math.min(w, h) / 80);
  ctx.lineWidth = lw;
  ctx.setLineDash([lw * 4, lw * 3]);
  ctx.fillRect(0, 0, w, h);
  ctx.strokeRect(0, 0, w, h);
  ctx.setLineDash([]);
  ctx.beginPath();
  ctx.moveTo(0, 0);
  ctx.lineTo(w, h);
  ctx.moveTo(w, 0);
  ctx.lineTo(0, h);
  ctx.stroke();
  ctx.restore();
}

function drawImageLayer(ctx: CanvasRenderingContext2D, design: Design, layer: Layer, state: LayerState, options: RenderOptions) {
  const url = assetUrlOf(design, layer.asset, options);
  const image = url ? getImage(url) : null;
  const w = state.width;
  const h = state.height;
  if (!image || !image.naturalWidth) {
    if (options.placeholders) drawPlaceholder(ctx, w, h);
    return;
  }
  const source = layer.tint ? tintedImage(image, resolveAssetUrl(url), layer.tint) : image;
  let sx = 0;
  let sy = 0;
  let sw = image.naturalWidth;
  let sh = image.naturalHeight;
  if (layer.type === "sprite") {
    const columns = Math.max(1, Math.floor(layer.columns ?? 1));
    const rows = Math.max(1, Math.floor(layer.rows ?? 1));
    const frames = Math.max(1, Math.min(columns * rows, Math.floor(layer.frameCount || columns * rows)));
    const fps = layer.fps && layer.fps > 0 ? layer.fps : 12;
    // 序列帧不受 duration 影响，按自己的时钟一直播
    const frame = Math.floor((Math.max(0, options.elapsed) / 1000) * fps) % frames;
    sw = image.naturalWidth / columns;
    sh = image.naturalHeight / rows;
    sx = (frame % columns) * sw;
    sy = Math.floor(frame / columns) * sh;
  }
  const dest = fitRect(layer.fit, sw, sh, w, h);
  if (layer.fit === "cover") {
    ctx.beginPath();
    ctx.rect(0, 0, w, h);
    ctx.clip();
  }
  ctx.drawImage(source, sx, sy, sw, sh, dest.x, dest.y, dest.w, dest.h);
}

function setGlow(ctx: CanvasRenderingContext2D, layer: Pick<Layer, "glow" | "glowColor">, blurScale: number) {
  const glow = layer.glow ?? 0;
  const color = parseDesignColor(layer.glowColor);
  if (glow > 0 && color) {
    ctx.shadowColor = rgbaCss(color);
    ctx.shadowBlur = glow * blurScale;
    ctx.shadowOffsetX = 0;
    ctx.shadowOffsetY = 0;
  }
}

function clearGlow(ctx: CanvasRenderingContext2D) {
  ctx.shadowBlur = 0;
  ctx.shadowColor = "transparent";
}

const widthCache = new Map<string, number[]>();

function charWidths(ctx: CanvasRenderingContext2D, font: string, chars: string[]) {
  const key = `${font}|${chars.join("")}`;
  const hit = widthCache.get(key);
  if (hit) return hit;
  const widths = chars.map((char) => ctx.measureText(char).width);
  if (widthCache.size > 500) widthCache.clear();
  widthCache.set(key, widths);
  return widths;
}

function drawTextLayer(ctx: CanvasRenderingContext2D, layer: Layer, state: LayerState, blurScale: number) {
  const text = layer.text ?? "";
  if (!text) return;
  const size = layer.fontSize && layer.fontSize > 0 ? layer.fontSize : 24;
  const weight = layer.fontWeight || 600;
  const font = `${weight} ${size}px ${DESIGN_FONT}`;
  ctx.font = font;
  ctx.textBaseline = "middle";
  ctx.textAlign = "left";
  const chars = Array.from(text);
  const widths = charWidths(ctx, font, chars);
  const spacing = layer.letterSpacing ?? 0;
  const total = widths.reduce((sum, w) => sum + w, 0) + spacing * Math.max(0, chars.length - 1);
  const w = state.width;
  let x = layer.align === "left" ? 0 : layer.align === "right" ? w - total : (w - total) / 2;
  const y = state.height / 2;
  const positions: number[] = [];
  for (const cw of widths) {
    positions.push(x);
    x += cw + spacing;
  }
  const fill = parseDesignColor(layer.color);
  const stroke = parseDesignColor(layer.stroke);
  const strokeWidth = layer.strokeWidth ?? 0;
  let glowPending = true;
  if (stroke && strokeWidth > 0) {
    setGlow(ctx, layer, blurScale);
    glowPending = false;
    ctx.lineJoin = "round";
    ctx.lineWidth = strokeWidth;
    ctx.strokeStyle = rgbaCss(stroke);
    chars.forEach((char, i) => ctx.strokeText(char, positions[i], y));
    clearGlow(ctx);
  }
  if (fill) {
    if (glowPending) setGlow(ctx, layer, blurScale);
    ctx.fillStyle = rgbaCss(fill);
    chars.forEach((char, i) => ctx.fillText(char, positions[i], y));
    clearGlow(ctx);
  }
}

/** 星形：sides 个角、内径 = 外径 × innerRatio、第一个角朝上；外径按图层矩形取椭圆（与 circle 取内切椭圆同理） */
function starPath(ctx: CanvasRenderingContext2D, cx: number, cy: number, rx: number, ry: number, sides: number, inner: number, rotation = 0) {
  const n = Math.max(3, Math.floor(sides));
  for (let k = 0; k < n * 2; k++) {
    const angle = -Math.PI / 2 + rotation + (k * Math.PI) / n;
    const r = k % 2 === 0 ? 1 : inner;
    const px = cx + Math.cos(angle) * rx * r;
    const py = cy + Math.sin(angle) * ry * r;
    if (k === 0) ctx.moveTo(px, py);
    else ctx.lineTo(px, py);
  }
  ctx.closePath();
}

function polygonPath(ctx: CanvasRenderingContext2D, cx: number, cy: number, rx: number, ry: number, sides: number) {
  const n = Math.max(3, Math.floor(sides));
  for (let k = 0; k < n; k++) {
    const angle = -Math.PI / 2 + (k * 2 * Math.PI) / n;
    const px = cx + Math.cos(angle) * rx;
    const py = cy + Math.sin(angle) * ry;
    if (k === 0) ctx.moveTo(px, py);
    else ctx.lineTo(px, py);
  }
  ctx.closePath();
}

function applyDash(ctx: CanvasRenderingContext2D, layer: Layer, state: LayerState) {
  const dash = (layer.dash ?? []).filter((v) => v > 0);
  if (dash.length) {
    ctx.setLineDash(dash);
    ctx.lineDashOffset = state.dashOffset;
  } else {
    ctx.setLineDash([]);
  }
}

function drawShapeLayer(ctx: CanvasRenderingContext2D, layer: Layer, state: LayerState, blurScale: number) {
  const w = state.width;
  const h = state.height;
  const fill = parseDesignColor(layer.fill);
  const stroke = parseDesignColor(layer.stroke);
  const strokeWidth = layer.strokeWidth ?? 0;
  setGlow(ctx, layer, blurScale);

  if (layer.shape === "ring") {
    // 描边圆弧：直径取 min(w, h) − strokeWidth、居中；从 12 点方向起、偏 startAngle、顺时针扫 sweep；端点圆头
    if (!stroke || strokeWidth <= 0) return;
    const radius = (Math.min(w, h) - strokeWidth) / 2;
    if (radius <= 0 || state.sweep <= 0) return;
    const start = ((layer.startAngle ?? 0) - 90) * DEG;
    ctx.beginPath();
    ctx.arc(w / 2, h / 2, radius, start, start + state.sweep * DEG, false);
    ctx.lineCap = "round";
    ctx.lineWidth = strokeWidth;
    ctx.strokeStyle = rgbaCss(stroke);
    applyDash(ctx, layer, state);
    ctx.stroke();
    return;
  }

  ctx.beginPath();
  switch (layer.shape) {
    case "circle":
      ctx.ellipse(w / 2, h / 2, w / 2, h / 2, 0, 0, Math.PI * 2);
      break;
    case "star":
      starPath(ctx, w / 2, h / 2, w / 2, h / 2, layer.sides ?? 5, layer.innerRatio && layer.innerRatio > 0 ? layer.innerRatio : 0.5);
      break;
    case "polygon":
      polygonPath(ctx, w / 2, h / 2, w / 2, h / 2, layer.sides ?? 6);
      break;
    default: {
      const radius = Math.max(0, Math.min(layer.cornerRadius ?? 0, w / 2, h / 2));
      if (radius > 0) ctx.roundRect(0, 0, w, h, radius);
      else ctx.rect(0, 0, w, h);
    }
  }
  // 先填充后描边
  if (fill) {
    ctx.fillStyle = rgbaCss(fill);
    ctx.fill();
  }
  if (stroke && strokeWidth > 0) {
    ctx.lineJoin = "round";
    ctx.lineCap = "butt";
    ctx.lineWidth = strokeWidth;
    ctx.strokeStyle = rgbaCss(stroke);
    applyDash(ctx, layer, state);
    ctx.stroke();
  }
}

function drawParticleLayer(ctx: CanvasRenderingContext2D, design: Design, layer: Layer, state: LayerState, options: RenderOptions) {
  const p = layer.particles;
  if (!p) return;
  const samples = sampleParticles(p, state.width, state.height, Math.max(0, options.elapsed));
  const baseAlpha = ctx.globalAlpha;
  const image =
    p.shape === "image" ? getImage(assetUrlOf(design, p.asset, options)) : null;
  const colorCache = new Map<string, string>();
  const css = (value?: string) => {
    if (!value) return "#FFFFFF";
    let hit = colorCache.get(value);
    if (!hit) {
      const parsed = parseDesignColor(value);
      hit = parsed ? rgbaCss(parsed) : "#FFFFFF";
      colorCache.set(value, hit);
    }
    return hit;
  };
  for (const s of samples) {
    if (s.size <= 0 || s.alpha <= 0) continue;
    ctx.globalAlpha = baseAlpha * s.alpha;
    const half = s.size / 2;
    switch (p.shape) {
      case "star":
        ctx.fillStyle = css(s.color);
        ctx.beginPath();
        starPath(ctx, s.x, s.y, half, half, 5, 0.45, s.spin * DEG);
        ctx.fill();
        break;
      case "spark": {
        // 沿运动方向拉长的菱形：长 = 3 × 大小
        const len = s.size * 1.5;
        const cos = Math.cos(s.heading);
        const sin = Math.sin(s.heading);
        ctx.fillStyle = css(s.color);
        ctx.beginPath();
        ctx.moveTo(s.x + cos * len, s.y + sin * len);
        ctx.lineTo(s.x - sin * half, s.y + cos * half);
        ctx.lineTo(s.x - cos * len, s.y - sin * len);
        ctx.lineTo(s.x + sin * half, s.y - cos * half);
        ctx.closePath();
        ctx.fill();
        break;
      }
      case "image":
        if (image && image.naturalWidth) {
          const scale = Math.min(s.size / image.naturalWidth, s.size / image.naturalHeight);
          const iw = image.naturalWidth * scale;
          const ih = image.naturalHeight * scale;
          if (s.spin) {
            ctx.save();
            ctx.translate(s.x, s.y);
            ctx.rotate(s.spin * DEG);
            ctx.drawImage(image, -iw / 2, -ih / 2, iw, ih);
            ctx.restore();
          } else {
            ctx.drawImage(image, s.x - iw / 2, s.y - ih / 2, iw, ih);
          }
        }
        break;
      default:
        ctx.fillStyle = css(s.color);
        ctx.beginPath();
        ctx.arc(s.x, s.y, half, 0, Math.PI * 2);
        ctx.fill();
    }
  }
  ctx.globalAlpha = baseAlpha;
}

// ── 昵称样式 ──

export type NameDrawOptions = {
  x: number;
  /** 文字垂直中线 */
  y: number;
  fontSize: number;
  elapsed: number;
  pixelRatio: number;
  align?: "left" | "center";
};

const HIGHLIGHT_DEFAULT: RGBA = { r: 255, g: 255, b: 255, a: 0.7 };

function nameFont(style: NameStyle, fontSize: number) {
  return `${style.fontWeight || 600} ${fontSize}px ${DESIGN_FONT}`;
}

let measureCtx: CanvasRenderingContext2D | null = null;

/** 昵称的显示宽度（不含发光） */
export function measureName(text: string, style: NameStyle | undefined | null, fontSize: number) {
  if (typeof document === "undefined") return text.length * fontSize;
  if (!measureCtx) measureCtx = document.createElement("canvas").getContext("2d");
  if (!measureCtx) return text.length * fontSize;
  const font = nameFont(style ?? { color: "#000000", effect: "none" }, fontSize);
  measureCtx.font = font;
  return charWidths(measureCtx, font, Array.from(text)).reduce((sum, w) => sum + w, 0);
}

/**
 * 昵称的六种效果：none 纯色 / shimmer 高光扫过 / pulse 亮度 75%–100% 呼吸 / cycle 颜色循环 /
 * glow 外发光 / wave 逐字起伏（振幅 0.12em，相邻字相位差 0.15 周期）。周期都是 speed 毫秒。
 */
export function drawNameStyle(ctx: CanvasRenderingContext2D, text: string, style: NameStyle, options: NameDrawOptions) {
  if (!text) return;
  const fontSize = options.fontSize;
  const font = nameFont(style, fontSize);
  ctx.save();
  ctx.font = font;
  ctx.textBaseline = "middle";
  ctx.textAlign = "left";
  const chars = Array.from(text);
  const widths = charWidths(ctx, font, chars);
  const total = widths.reduce((sum, w) => sum + w, 0);
  const startX = options.align === "center" ? options.x - total / 2 : options.x;
  const speed = Math.max(300, style.speed || 2400);
  const phase = (((options.elapsed % speed) + speed) % speed) / speed;
  const base = parseDesignColor(style.color) ?? { r: 59, g: 130, b: 246, a: 1 };

  let fill: string | CanvasGradient = rgbaCss(base);
  switch (style.effect) {
    case "pulse": {
      const factor = 0.75 + 0.25 * (0.5 + 0.5 * Math.cos(phase * Math.PI * 2));
      fill = rgbaCss(scaleBrightness(base, factor));
      break;
    }
    case "cycle": {
      const colors = (style.colors ?? []).map((c) => parseDesignColor(c)).filter((c): c is RGBA => Boolean(c));
      if (colors.length >= 2) {
        const pos = phase * colors.length;
        const i = Math.floor(pos) % colors.length;
        const frac = pos - Math.floor(pos);
        const smooth = frac * frac * (3 - 2 * frac);
        fill = rgbaCss(mixColor(colors[i], colors[(i + 1) % colors.length], smooth));
      }
      break;
    }
    case "shimmer": {
      const highlight = parseDesignColor(style.highlightColor) ?? HIGHLIGHT_DEFAULT;
      const peak = mixColor(base, { ...highlight, a: base.a }, highlight.a);
      const band = 0.18;
      // 高光中心从左侧带外扫到右侧带外
      const center = -band + phase * (1 + band * 2);
      const gradient = ctx.createLinearGradient(startX, 0, startX + Math.max(1, total), 0);
      const stop = (offset: number, color: RGBA) => {
        if (offset >= 0 && offset <= 1) gradient.addColorStop(offset, rgbaCss(color));
      };
      gradient.addColorStop(0, rgbaCss(Math.abs(center) < band ? mixColor(peak, base, Math.abs(center) / band) : base));
      stop(center - band, base);
      stop(center, peak);
      stop(center + band, base);
      gradient.addColorStop(1, rgbaCss(Math.abs(1 - center) < band ? mixColor(peak, base, Math.abs(1 - center) / band) : base));
      fill = gradient;
      break;
    }
    case "glow": {
      const glow = parseDesignColor(style.glowColor);
      if (glow && (style.glowRadius ?? 0) > 0) {
        ctx.shadowColor = rgbaCss(glow);
        ctx.shadowBlur = (style.glowRadius ?? 0) * options.pixelRatio;
      }
      break;
    }
  }
  ctx.fillStyle = fill;
  let x = startX;
  chars.forEach((char, index) => {
    let y = options.y;
    if (style.effect === "wave") {
      y -= 0.12 * fontSize * Math.sin(Math.PI * 2 * (phase - index * 0.15));
    }
    ctx.fillText(char, x, y);
    x += widths[index];
  });
  ctx.restore();
}
