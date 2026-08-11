// 地理风控 API —— 围栏管理 / 热力图 / 攻击聚类 / 登录轨迹
// 对应后端 internal/transport/http/geo_handlers.go（仅超级管理员）。

import { apiRequest, buildQuery } from "./client";

// ──────────────────────────────────────
// 类型
// ──────────────────────────────────────

export type GeoFenceMode = "deny" | "allow" | "review";

/** GeoJSON 围栏几何（后端仅接受 Polygon / MultiPolygon） */
export type GeoFenceGeometry =
  | { type: "Polygon"; coordinates: number[][][] }
  | { type: "MultiPolygon"; coordinates: number[][][][] };

export type GeoFenceEntry = {
  id: number;
  /** 空 = 平台级 */
  appId?: number | null;
  name: string;
  mode: GeoFenceMode;
  /** 多边形围栏 GeoJSON；与圆形字段二选一 */
  fence?: GeoFenceGeometry | null;
  centerLat?: number | null;
  centerLng?: number | null;
  radiusM?: number | null;
  /** 命中后的响应模式；空 = 平台默认 */
  banMode: string;
  reason: string;
  enabled: boolean;
  expiresAt?: string | null;
  matchCount: number;
  lastMatchAt?: string | null;
  createdBy?: number | null;
  createdAt: string;
  updatedAt: string;
};

export type GeoFencePayload = {
  appId?: number | null;
  name: string;
  mode: GeoFenceMode;
  fence?: GeoFenceGeometry | null;
  centerLat?: number | null;
  centerLng?: number | null;
  radiusM?: number | null;
  banMode?: string;
  reason?: string;
  enabled?: boolean;
  /** RFC3339；空 = 永久 */
  expiresAt?: string;
};

export type GeoFencePreviewResult = {
  windowDays: number;
  loginMatches: number;
  blockMatches: number;
  uniqueUsers: number;
};

export type GeoStatsKind = "block" | "login";

export type GeoHeatmapCell = {
  geohash: string;
  lat: number;
  lng: number;
  countryCode: string;
  city: string;
  count: number;
};

export type GeoCountryStat = {
  countryCode: string;
  count: number;
};

export type GeoHeatmapResult = {
  cells: GeoHeatmapCell[];
  countries: GeoCountryStat[];
  total: number;
};

export type GeoCluster = {
  clusterId: number;
  hits: number;
  uniqueIps: number;
  centerLat: number;
  centerLng: number;
  topReason: string;
};

export type GeoTrailPoint = {
  ip: string;
  countryCode: string;
  country: string;
  region: string;
  city: string;
  lat?: number | null;
  lng?: number | null;
  loginType: string;
  deviceId: string;
  createdAt: string;
};

// ──────────────────────────────────────
// 围栏 CRUD
// ──────────────────────────────────────

export function getGeoFences(token: string) {
  return apiRequest<GeoFenceEntry[]>("/api/admin/system/firewall/geo-fences", { token });
}

export function createGeoFence(token: string, payload: GeoFencePayload) {
  return apiRequest<GeoFenceEntry>("/api/admin/system/firewall/geo-fences", {
    method: "POST",
    body: JSON.stringify(payload),
    token
  });
}

export function updateGeoFence(token: string, id: number, payload: GeoFencePayload) {
  return apiRequest<GeoFenceEntry>(`/api/admin/system/firewall/geo-fences/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
    token
  });
}

export function toggleGeoFence(token: string, id: number, enabled: boolean) {
  return apiRequest<{ id: number; enabled: boolean }>(`/api/admin/system/firewall/geo-fences/${id}`, {
    method: "PATCH",
    body: JSON.stringify({ enabled }),
    token
  });
}

export function deleteGeoFence(token: string, id: number) {
  return apiRequest<{ id: number }>(`/api/admin/system/firewall/geo-fences/${id}`, {
    method: "DELETE",
    token
  });
}

/** 创建前回测：过去 windowDays 天内会命中多少登录 / 拦截 / 用户 */
export function previewGeoFence(token: string, payload: GeoFencePayload & { windowDays?: number }) {
  return apiRequest<GeoFencePreviewResult>("/api/admin/system/firewall/geo-fences/preview", {
    method: "POST",
    body: JSON.stringify(payload),
    token
  });
}

// ──────────────────────────────────────
// 地理分析
// ──────────────────────────────────────

export function getGeoHeatmap(
  token: string,
  params: { kind: GeoStatsKind; start?: string; end?: string; country?: string; limit?: number }
) {
  return apiRequest<GeoHeatmapResult>(`/api/admin/system/geo/heatmap${buildQuery(params)}`, { token });
}

export function getGeoClusters(
  token: string,
  params: { hours?: number; eps?: number; minPoints?: number; limit?: number } = {}
) {
  return apiRequest<GeoCluster[]>(`/api/admin/system/geo/clusters${buildQuery(params)}`, { token });
}

export function getGeoTrail(token: string, params: { userId: number; appId: number; limit?: number }) {
  return apiRequest<GeoTrailPoint[]>(`/api/admin/system/geo/trail${buildQuery(params)}`, { token });
}
