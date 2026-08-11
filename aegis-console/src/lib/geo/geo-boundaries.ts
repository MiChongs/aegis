"use client";

export type GeoLevel = "world" | "country" | "province" | "city";

export type GeoView = {
  level: GeoLevel;
  regionName: string;
  mapKey: string;
  countryCode?: string;
  adcode?: string;
  parentName?: string;
};

type GeoJSONFeatureCollection = {
  type: "FeatureCollection";
  features: Array<{
    type: "Feature";
    properties?: Record<string, unknown>;
    geometry?: unknown;
  }>;
};

const boundaryCache = new Map<string, Promise<GeoJSONFeatureCollection>>();
const LOCAL_WORLD_BOUNDARY_URL = "/geo/world-countries.geo.json";
const LOCAL_CHINA_BOUNDARY_BASE_URL = "/geo/china";

function resolveBoundaryURL(view: GeoView) {
  if (view.level === "world") {
    return LOCAL_WORLD_BOUNDARY_URL;
  }
  if (view.level === "country") {
    return `${LOCAL_CHINA_BOUNDARY_BASE_URL}/100000.json`;
  }
  if (view.adcode) {
    return `${LOCAL_CHINA_BOUNDARY_BASE_URL}/${view.adcode}.json`;
  }
  throw new Error(`missing geo adcode for level ${view.level}`);
}

export function normalizeAdminName(value: string) {
  return value
    .trim()
    .replace(/特别行政区/g, "")
    .replace(/维吾尔自治区|壮族自治区|回族自治区|自治区/g, "")
    .replace(/自治州/g, "")
    .replace(/地区/g, "")
    .replace(/盟/g, "")
    .replace(/林区/g, "")
    .replace(/省|市|区|县/g, "");
}

export function loadGeoBoundary(view: GeoView) {
  const cacheKey = `${view.level}:${view.adcode || view.countryCode || "world"}`;
  const cached = boundaryCache.get(cacheKey);
  if (cached) {
    return cached;
  }

  const promise = fetch(resolveBoundaryURL(view), {
    method: "GET",
    cache: "force-cache"
  }).then(async (response) => {
    if (!response.ok) {
      throw new Error(`load geo boundary failed: ${response.status}`);
    }
    return (await response.json()) as GeoJSONFeatureCollection;
  });

  boundaryCache.set(cacheKey, promise);
  return promise;
}

export function getFeatureName(properties?: Record<string, unknown>) {
  const value =
    properties?.name ||
    properties?.NAME ||
    properties?.fullname ||
    properties?.fullName ||
    properties?.中文名 ||
    properties?.name_zh;
  return typeof value === "string" ? value : "";
}

export function getFeatureAdcode(properties?: Record<string, unknown>) {
  const value = properties?.adcode || properties?.adcode99 || properties?.code || properties?.id;
  if (typeof value === "number") {
    return String(value);
  }
  return typeof value === "string" ? value : "";
}

export type { GeoJSONFeatureCollection };
