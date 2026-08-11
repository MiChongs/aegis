"use client";

import { useEffect, useMemo, useRef } from "react";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { MapChart } from "echarts/charts";
import { TooltipComponent, VisualMapComponent } from "echarts/components";
import type { EChartsType } from "echarts/core";
import { useTheme } from "next-themes";
import { getCountryCodeFromEnglishName } from "@/lib/geo/country-hotspots";
import { getFeatureAdcode, getFeatureName, normalizeAdminName, type GeoJSONFeatureCollection, type GeoView } from "@/lib/geo/geo-boundaries";
import type { DrillDatum } from "@/lib/geo/region-drill";
import { cn } from "@/lib/utils";

echarts.use([CanvasRenderer, MapChart, TooltipComponent, VisualMapComponent]);

type GeoDrillChartProps = {
  view: GeoView;
  geoJson: GeoJSONFeatureCollection;
  data: DrillDatum[];
  formatCount: (value?: number | null) => string;
  className?: string;
  onSelect?: (payload: { name: string; adcode?: string; code?: string }) => void;
};

type FeatureMeta = {
  name: string;
  adcode?: string;
  code?: string;
};

function buildFeatureLookup(view: GeoView, geoJson: GeoJSONFeatureCollection) {
  const byName = new Map<string, FeatureMeta>();
  const byCode = new Map<string, FeatureMeta>();
  for (const feature of geoJson.features) {
    const name = getFeatureName(feature.properties);
    if (!name) {
      continue;
    }
    const item = {
      name,
      adcode: getFeatureAdcode(feature.properties)
    };
    byName.set(normalizeAdminName(name) || name, item);
    if (view.level === "world") {
      const code = getCountryCodeFromEnglishName(name);
      if (code) {
        byCode.set(code, {
          ...item,
          code
        });
      }
    }
  }
  return { byName, byCode };
}

export function GeoDrillChart({ view, geoJson, data, formatCount, className, onSelect }: GeoDrillChartProps) {
  const chartRef = useRef<HTMLDivElement | null>(null);
  const instanceRef = useRef<EChartsType | null>(null);
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";

  const featureLookup = useMemo(() => buildFeatureLookup(view, geoJson), [geoJson, view]);
  const maxValue = useMemo(() => data.reduce((max, item) => Math.max(max, item.value), 0), [data]);

  useEffect(() => {
    if (!chartRef.current) {
      return;
    }

    const mapName = `aegis:${view.mapKey}`;
    echarts.registerMap(mapName, geoJson as never);
    const chart = echarts.init(chartRef.current, undefined, {
      renderer: "canvas"
    });
    instanceRef.current = chart;

    const normalizedData = data
      .map((item) => {
        const feature =
          (item.code ? featureLookup.byCode.get(item.code.toUpperCase()) : undefined) ||
          featureLookup.byName.get(normalizeAdminName(item.name) || item.name);
        return {
          name: feature?.name || item.name,
          value: item.value,
          label: item.label,
          share: item.share,
          code: feature?.code || item.code,
          adcode: feature?.adcode
        };
      })
      .filter((item) => Boolean(item.name));

    chart.setOption(
      {
        backgroundColor: "transparent",
        animationDurationUpdate: 420,
        animationEasingUpdate: "cubicOut",
        tooltip: {
          trigger: "item",
          borderWidth: 0,
          backgroundColor: isDark ? "rgba(24,24,27,0.96)" : "rgba(255,255,255,0.97)",
          textStyle: {
            color: isDark ? "#f4f4f5" : "#18181b"
          },
          formatter: (params: { data?: { label?: string; share?: number } | null; name: string; value?: number | number[] }) => {
            const label = params.data?.label || params.name;
            const value = Array.isArray(params.value) ? Number(params.value[0] || 0) : Number(params.value || 0);
            const share = typeof params.data?.share === "number" ? `${params.data.share}%` : "";
            return [
              `<div style="font-weight:600;margin-bottom:4px">${label}</div>`,
              `<div style="display:flex;gap:16px;align-items:center;justify-content:space-between">`,
              `<span style="opacity:.72">数量</span>`,
              `<span>${formatCount(value)}${share ? ` · ${share}` : ""}</span>`,
              `</div>`
            ].join("");
          }
        },
        visualMap: {
          show: false,
          min: 0,
          max: maxValue > 0 ? maxValue : 1,
          inRange: {
            color: isDark
              ? ["#18181b", "#2c2c30", "#52525b", "#a1a1aa"]
              : ["#fafafa", "#e4e4e7", "#a1a1aa", "#52525b"]
          }
        },
        series: [
          {
            type: "map",
            map: mapName,
            roam: true,
            zoom: view.level === "world" ? 1.06 : 1.12,
            scaleLimit: { min: 1, max: 12 },
            data: normalizedData,
            emphasis: {
              label: {
              show: false
              },
              itemStyle: {
                areaColor: isDark ? "#71717a" : "#3f3f46",
                borderColor: isDark ? "#d4d4d8" : "#ffffff",
                borderWidth: 1.2,
                shadowBlur: 10,
                shadowColor: isDark ? "rgba(161,161,170,.2)" : "rgba(63,63,70,.15)"
              }
            },
            select: {
              disabled: true
            },
            itemStyle: {
              areaColor: isDark ? "#1c1c1f" : "#f4f4f5",
              borderColor: isDark ? "#27272a" : "#d4d4d8",
              borderWidth: 0.8
            },
            label: {
              show: false
            }
          }
        ]
      },
      true
    );

    const resizeObserver = new ResizeObserver(() => chart.resize());
    resizeObserver.observe(chartRef.current);

    const clickHandler = (params: unknown) => {
      if (!onSelect) {
        return;
      }
      const payload = params as {
        name?: string;
        data?: Record<string, unknown> | null;
      };
      if (!payload.name) {
        return;
      }
      onSelect({
        name: payload.name,
        adcode: typeof payload.data?.adcode === "string" ? payload.data.adcode : undefined,
        code: typeof payload.data?.code === "string" ? payload.data.code : undefined
      });
    };

    chart.on("click", clickHandler);

    return () => {
      chart.off("click", clickHandler);
      resizeObserver.disconnect();
      chart.dispose();
      instanceRef.current = null;
    };
  }, [data, featureLookup, formatCount, geoJson, isDark, maxValue, onSelect, view.level, view.mapKey]);

  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-[32px] border border-border bg-card dark:bg-zinc-900",
        className
      )}
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 z-[1] h-28" />
      <div ref={chartRef} className="h-[540px] w-full md:h-[600px]" />
      <div className="pointer-events-none absolute inset-x-6 bottom-5 z-[2] flex items-center justify-between rounded-2xl border border-border bg-background px-4 py-2.5">
        <div className="text-[11px] font-medium text-muted-foreground">{view.regionName}</div>
        <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
          <span>低</span>
          <div className="h-2 w-28 rounded-full bg-[linear-gradient(90deg,#fafafa,#a1a1aa,#52525b)] dark:bg-[linear-gradient(90deg,#18181b,#52525b,#a1a1aa)]" />
          <span>高</span>
        </div>
      </div>
    </div>
  );
}
