/**
 * 设计稿颜色：`#RRGGBB` 或 `#AARRGGBB`（alpha 在前，与 Android 的 ARGB 一致）。
 * 浏览器的 `#RRGGBBAA` 是 alpha 在后，直接塞给 canvas 会把颜色读错，一律经这里换成 `rgba()`。
 */

export type RGBA = { r: number; g: number; b: number; a: number };

const COLOR_PATTERN = /^#([0-9a-f]{6}|[0-9a-f]{8})$/i;
const cache = new Map<string, RGBA | null>();

export function isDesignColor(value?: string | null): value is string {
  return Boolean(value && COLOR_PATTERN.test(value));
}

export function parseDesignColor(value?: string | null): RGBA | null {
  if (!value) return null;
  const hit = cache.get(value);
  if (hit !== undefined) return hit;
  let parsed: RGBA | null = null;
  if (COLOR_PATTERN.test(value)) {
    const hex = value.slice(1);
    if (hex.length === 6) {
      parsed = {
        r: parseInt(hex.slice(0, 2), 16),
        g: parseInt(hex.slice(2, 4), 16),
        b: parseInt(hex.slice(4, 6), 16),
        a: 1
      };
    } else {
      parsed = {
        a: parseInt(hex.slice(0, 2), 16) / 255,
        r: parseInt(hex.slice(2, 4), 16),
        g: parseInt(hex.slice(4, 6), 16),
        b: parseInt(hex.slice(6, 8), 16)
      };
    }
  }
  if (cache.size > 2000) cache.clear();
  cache.set(value, parsed);
  return parsed;
}

export function rgbaCss(color: RGBA, alphaMul = 1) {
  const a = Math.max(0, Math.min(1, color.a * alphaMul));
  return `rgba(${Math.round(color.r)}, ${Math.round(color.g)}, ${Math.round(color.b)}, ${Number(a.toFixed(4))})`;
}

/** 设计稿颜色 → CSS 颜色；不合法时返回 fallback */
export function designColorToCss(value?: string | null, fallback = "transparent", alphaMul = 1) {
  const parsed = parseDesignColor(value);
  return parsed ? rgbaCss(parsed, alphaMul) : fallback;
}

/** 组成设计稿颜色：alpha 为 1 时写成 `#RRGGBB`，否则 `#AARRGGBB` */
export function formatDesignColor(color: RGBA) {
  const hex = (n: number) => Math.round(Math.max(0, Math.min(255, n))).toString(16).padStart(2, "0").toUpperCase();
  const alpha = Math.round(Math.max(0, Math.min(1, color.a)) * 255);
  return alpha >= 255 ? `#${hex(color.r)}${hex(color.g)}${hex(color.b)}` : `#${hex(alpha)}${hex(color.r)}${hex(color.g)}${hex(color.b)}`;
}

/** `#RRGGBB`（浏览器原生取色器的值）+ alpha → 设计稿颜色 */
export function composeDesignColor(rgbHex: string, alpha: number) {
  const hex = rgbHex.replace("#", "");
  return formatDesignColor({
    r: parseInt(hex.slice(0, 2), 16) || 0,
    g: parseInt(hex.slice(2, 4), 16) || 0,
    b: parseInt(hex.slice(4, 6), 16) || 0,
    a: alpha
  });
}

/** 设计稿颜色的 RGB 部分（给原生取色器） */
export function rgbHexOf(value?: string | null) {
  const parsed = parseDesignColor(value);
  if (!parsed) return "#000000";
  const hex = (n: number) => Math.round(n).toString(16).padStart(2, "0");
  return `#${hex(parsed.r)}${hex(parsed.g)}${hex(parsed.b)}`;
}

export function mixColor(a: RGBA, b: RGBA, t: number): RGBA {
  return {
    r: a.r + (b.r - a.r) * t,
    g: a.g + (b.g - a.g) * t,
    b: a.b + (b.b - a.b) * t,
    a: a.a + (b.a - a.a) * t
  };
}

export function scaleBrightness(c: RGBA, factor: number): RGBA {
  return { r: c.r * factor, g: c.g * factor, b: c.b * factor, a: c.a };
}
