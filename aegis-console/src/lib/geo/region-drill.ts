"use client";

import type { RegionStatItem } from "@/lib/api/types";
import { buildCountryHotspots } from "@/lib/geo/country-hotspots";
import { normalizeAdminName } from "@/lib/geo/geo-boundaries";

export type DrillDatum = {
  name: string;
  label: string;
  value: number;
  share: number;
  code?: string;
  parent?: string;
  parentPath?: string;
};

function toRegionCount(items: RegionStatItem[]) {
  return items
    .map((item, index) => ({
      region: item.region || item.name || item.label || `地区 ${index + 1}`,
      code: item.code,
      count: Number(item.count || item.value || 0),
      parent: item.parent || "",
      parentPath: item.parentPath || ""
    }))
    .filter((item) => item.count > 0);
}

function buildAdministrativeDrillData(items: Array<{ region: string; count: number; code?: string; parent?: string; parentPath?: string }>) {
  const total = items.reduce((sum, item) => sum + item.count, 0);
  const aggregated = new Map<string, DrillDatum>();

  for (const item of items) {
    const normalizedName = normalizeAdminName(item.region);
    const key = `${normalizedName}::${normalizeAdminName(item.parent || "")}::${(item.parentPath || "").split("/").map(normalizeAdminName).join("/")}`;
    const current = aggregated.get(key);
    const nextValue = (current?.value || 0) + item.count;
    aggregated.set(key, {
      name: normalizedName || item.region,
      label: current?.label || item.region,
      value: nextValue,
      share: 0,
      code: item.code,
      parent: item.parent,
      parentPath: item.parentPath
    });
  }

  return Array.from(aggregated.values())
    .sort((left, right) => right.value - left.value || left.label.localeCompare(right.label, "zh-CN"))
    .map((item) => ({
      ...item,
      share: total > 0 ? Number(((item.value / total) * 100).toFixed(1)) : 0
    }));
}

export function buildWorldDrillData(items: RegionStatItem[]) {
  const result = buildCountryHotspots(
    toRegionCount(items).map((item) => ({
      region: item.region,
      code: item.code,
      count: item.count
    }))
  );

  return {
    items: result.items.map((item) => ({
      name: item.englishName,
      label: item.region,
      value: item.count,
      share: item.share,
      code: item.code
    })),
    unknownItems: result.unknownItems
  };
}

export function buildProvinceDrillData(items: RegionStatItem[]) {
  return buildAdministrativeDrillData(toRegionCount(items));
}

export function buildCityDrillData(items: RegionStatItem[], provinceName: string) {
  const normalizedProvince = normalizeAdminName(provinceName);
  return buildAdministrativeDrillData(
    toRegionCount(items).filter((item) => normalizeAdminName(item.parent || "") === normalizedProvince)
  );
}

export function buildDistrictDrillData(items: RegionStatItem[], provinceName: string, cityName: string) {
  const province = normalizeAdminName(provinceName);
  const city = normalizeAdminName(cityName);
  return buildAdministrativeDrillData(
    toRegionCount(items).filter((item) => {
      const segments = (item.parentPath || "").split("/").map(normalizeAdminName);
      return segments[0] === province && segments[1] === city;
    })
  );
}
