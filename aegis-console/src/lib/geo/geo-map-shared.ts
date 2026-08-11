// 地理风控三个面板（热力图 / 围栏 / 轨迹）共用的纯函数与常量。

import type { GeoFenceMode } from "@/lib/api/geo";

// ──────────────────────────────────────
// 围栏模式语义
// ──────────────────────────────────────

export const FENCE_MODE_META: Record<
  GeoFenceMode,
  { label: string; description: string; color: string; badgeVariant: "danger" | "success" | "warning" }
> = {
  deny: {
    label: "拦截",
    description: "落在区域内的登录 / 请求将被拦截",
    color: "#ef4444",
    badgeVariant: "danger"
  },
  allow: {
    label: "白名单",
    description: "存在任一白名单围栏时，区域之外全部拦截",
    color: "#10b981",
    badgeVariant: "success"
  },
  review: {
    label: "观察",
    description: "仅记录命中，不拦截（用于灰度评估）",
    color: "#f59e0b",
    badgeVariant: "warning"
  }
};

export const ALL_FENCE_MODES: GeoFenceMode[] = ["deny", "allow", "review"];

// ──────────────────────────────────────
// 时间 / 文案格式化
// ──────────────────────────────────────

export function fmtDateTime(iso?: string | null) {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString("zh-CN", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit"
    });
  } catch {
    return iso;
  }
}

export function fmtTimeShort(iso?: string | null) {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit"
    });
  } catch {
    return iso;
  }
}

/** Twemoji 国旗 SVG 资源前缀（jdecked 维护版；国内 jsDelivr 可访问） */
const TWEMOJI_SVG_BASE = "https://cdn.jsdelivr.net/gh/jdecked/twemoji@15.1.0/assets/svg";

/** ISO 3166-1 alpha-2 → Twemoji 国旗 SVG 地址；无效代码返回 null */
export function flagTwemojiUrl(countryCode?: string | null): string | null {
  const code = (countryCode || "").trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(code)) return null;
  // 两个区域指示符的 codepoint（如 CN → 1f1e8-1f1f3）
  const cp = [...code].map((c) => (0x1f1e6 + c.charCodeAt(0) - 65).toString(16)).join("-");
  return `${TWEMOJI_SVG_BASE}/${cp}.svg`;
}

/** 未知国家的兜底图标：内联 lucide `globe` 线框 SVG（tooltip 是 HTML 串，用不了 React 组件） */
function globeSvg(sizeEm: number): string {
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"` +
    ` stroke-width="2" stroke-linecap="round" stroke-linejoin="round"` +
    ` style="display:inline-block;width:${sizeEm}em;height:${sizeEm}em;vertical-align:-0.15em;margin-right:3px;opacity:.6">` +
    `<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/></svg>`
  );
}

/** 生成国旗的 <img> HTML 串，供 deck.gl / maplibre 的 HTML tooltip 使用。
 *  加载失败时（CDN 抖动）自动隐藏，不影响后续文本。 */
export function flagImgHtml(countryCode?: string | null, sizeEm = 1.05): string {
  const url = flagTwemojiUrl(countryCode);
  if (!url) return globeSvg(sizeEm);
  return `<img src="${url}" alt="${(countryCode || "").trim().toUpperCase()}" decoding="async" style="width:${sizeEm}em;height:${sizeEm}em;vertical-align:-0.15em;margin-right:3px" onerror="this.style.display='none'" />`;
}

export function fmtCount(value?: number | null) {
  if (value === null || typeof value === "undefined") return "—";
  return value.toLocaleString("zh-CN");
}

/** 半径展示：< 1000m 用米，否则保留 1 位小数的千米 */
export function fmtRadius(radiusM: number) {
  if (radiusM < 1000) return `${Math.round(radiusM)} m`;
  return `${(radiusM / 1000).toFixed(1)} km`;
}

// ──────────────────────────────────────
// 几何工具
// ──────────────────────────────────────

const EARTH_RADIUS_KM = 6371;

export function haversineKm(lat1: number, lng1: number, lat2: number, lng2: number) {
  const toRad = (d: number) => (d * Math.PI) / 180;
  const dLat = toRad(lat2 - lat1);
  const dLng = toRad(lng2 - lng1);
  const a =
    Math.sin(dLat / 2) ** 2 + Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLng / 2) ** 2;
  return 2 * EARTH_RADIUS_KM * Math.asin(Math.min(1, Math.sqrt(a)));
}

/**
 * 圆形围栏 → 多边形外环（仅用于地图展示；后端持久化仍是 center+radius）。
 * 返回 GeoJSON 坐标环（[lng, lat]，首尾闭合）。
 */
export function circleToRing(centerLng: number, centerLat: number, radiusM: number, steps = 72): number[][] {
  const ring: number[][] = [];
  const latRad = (centerLat * Math.PI) / 180;
  const dLat = radiusM / 111_320; // 1° 纬度 ≈ 111.32km
  const dLng = radiusM / (111_320 * Math.max(0.01, Math.cos(latRad)));
  for (let i = 0; i <= steps; i++) {
    const theta = (i / steps) * Math.PI * 2;
    ring.push([centerLng + dLng * Math.cos(theta), centerLat + dLat * Math.sin(theta)]);
  }
  return ring;
}

/** 计算一组坐标的包围盒，返回 [[west, south], [east, north]]；空集合返回 null */
export function coordsBounds(coords: Array<[number, number]>): [[number, number], [number, number]] | null {
  if (coords.length === 0) return null;
  let west = Infinity;
  let south = Infinity;
  let east = -Infinity;
  let north = -Infinity;
  for (const [lng, lat] of coords) {
    if (!Number.isFinite(lng) || !Number.isFinite(lat)) continue;
    west = Math.min(west, lng);
    east = Math.max(east, lng);
    south = Math.min(south, lat);
    north = Math.max(north, lat);
  }
  if (!Number.isFinite(west) || !Number.isFinite(south)) return null;
  return [
    [west, south],
    [east, north]
  ];
}
