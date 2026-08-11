import { apiRequest, buildQuery } from "./client";

// ── 类型 ──────────────────────────────────────────

export const DEVICE_PLATFORMS = ["ios", "android"] as const;
export type DevicePlatform = (typeof DEVICE_PLATFORMS)[number];

export const DEVICE_SOURCES = ["seed", "manual", "import"] as const;
export type DeviceSource = (typeof DEVICE_SOURCES)[number];

export type DeviceMarketingName = {
  id: number;
  platform: DevicePlatform;
  identifier: string;
  marketingName: string;
  manufacturer: string;
  manufacturerIconUrl: string;
  deviceImageUrl: string;
  source: DeviceSource;
  notes: string;
  createdAt: string;
  updatedAt: string;
};

export type DeviceMarketingPage = {
  items: DeviceMarketingName[];
  total: number;
  page: number;
  limit: number;
};

export type DeviceMarketingQuery = {
  platform?: DevicePlatform | "";
  keyword?: string;
  source?: DeviceSource | "";
  manufacturer?: string;
  page?: number;
  limit?: number;
};

export type DeviceMarketingCreateInput = {
  platform: DevicePlatform;
  identifier: string;
  marketingName: string;
  manufacturer?: string;
  manufacturerIconUrl?: string;
  deviceImageUrl?: string;
  notes?: string;
};

export type DeviceMarketingUpdateInput = Partial<{
  platform: DevicePlatform;
  identifier: string;
  marketingName: string;
  manufacturer: string;
  manufacturerIconUrl: string;
  deviceImageUrl: string;
  notes: string;
}>;

export type DeviceManufacturerList = {
  items: string[];
};

export type DeviceMarketingSeedStats = {
  parsed: number;
  inserted: number;
  duplicate: number;
  elapsedMs: number;
};

// ── API ──────────────────────────────────────────

const BASE = "/api/admin/system/device-marketing-names";

export function listDeviceMarketingNames(token: string, params: DeviceMarketingQuery) {
  // 注意：buildQuery 返回形如 "?platform=ios&..."（已带 '?'），直接拼接即可，不要再叠加 '?'
  const qs = buildQuery({
    platform: params.platform ?? "",
    keyword: params.keyword ?? "",
    source: params.source ?? "",
    manufacturer: params.manufacturer ?? "",
    page: params.page,
    limit: params.limit,
  });
  return apiRequest<DeviceMarketingPage>(`${BASE}${qs}`, { token });
}

export function listDeviceManufacturers(token: string, platform?: DevicePlatform | "") {
  const qs = buildQuery({ platform: platform ?? "" });
  return apiRequest<DeviceManufacturerList>(`${BASE}/manufacturers${qs}`, { token });
}


export function getDeviceMarketingName(token: string, id: number) {
  return apiRequest<DeviceMarketingName>(`${BASE}/${id}`, { token });
}

export function lookupDeviceMarketingName(
  token: string,
  platform: DevicePlatform,
  identifier: string,
) {
  const qs = buildQuery({ platform, identifier });
  return apiRequest<DeviceMarketingName>(`${BASE}/lookup${qs}`, { token });
}

export function createDeviceMarketingName(
  token: string,
  payload: DeviceMarketingCreateInput,
) {
  return apiRequest<DeviceMarketingName>(BASE, {
    method: "POST",
    token,
    body: JSON.stringify(payload),
  });
}

export function updateDeviceMarketingName(
  token: string,
  id: number,
  payload: DeviceMarketingUpdateInput,
) {
  return apiRequest<DeviceMarketingName>(`${BASE}/${id}`, {
    method: "PUT",
    token,
    body: JSON.stringify(payload),
  });
}

export function deleteDeviceMarketingName(token: string, id: number) {
  return apiRequest<void>(`${BASE}/${id}`, { method: "DELETE", token });
}

export function reseedDeviceMarketingNames(token: string) {
  return apiRequest<DeviceMarketingSeedStats>(`${BASE}/seed`, {
    method: "POST",
    token,
  });
}
