"use client";

import { useEffect, useMemo, useRef } from "react";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { MapChart, EffectScatterChart, ScatterChart } from "echarts/charts";
import { TooltipComponent, GeoComponent } from "echarts/components";
import type { EChartsType } from "echarts/core";
import { useTheme } from "next-themes";
import { useQuery } from "@tanstack/react-query";
import type { SessionDetailView } from "@/lib/api/apps";
import { Skeleton } from "@/components/ui/skeleton";

echarts.use([CanvasRenderer, MapChart, EffectScatterChart, ScatterChart, TooltipComponent, GeoComponent]);

const GEO_URL = "/geo/world-countries.geo.json";
const MAP_NAME = "aegis:session-locations";

// 主要城市坐标缓存（用于未知城市的国家级定位）
const COUNTRY_COORDS: Record<string, [number, number]> = {
  中国: [104.0, 35.5], 美国: [-95.7, 37.1], 日本: [138.3, 36.2], 韩国: [127.8, 35.9],
  英国: [-1.2, 52.9], 德国: [10.5, 51.2], 法国: [2.2, 46.6], 俄罗斯: [105.3, 61.5],
  加拿大: [-106.3, 56.1], 澳大利亚: [133.8, -25.3], 巴西: [-51.9, -14.2], 印度: [78.9, 20.6],
  新加坡: [103.8, 1.35], 香港: [114.2, 22.3], 台湾: [121.0, 23.7],
};

// 中国主要城市坐标
const CITY_COORDS: Record<string, [number, number]> = {
  北京: [116.4, 39.9], 上海: [121.5, 31.2], 广州: [113.3, 23.1], 深圳: [114.1, 22.5],
  杭州: [120.2, 30.3], 成都: [104.1, 30.6], 武汉: [114.3, 30.6], 南京: [118.8, 32.1],
  重庆: [106.6, 29.6], 西安: [108.9, 34.3], 天津: [117.2, 39.1], 苏州: [120.6, 31.3],
  郑州: [113.6, 34.8], 长沙: [113.0, 28.2], 沈阳: [123.4, 41.8], 青岛: [120.4, 36.1],
  大连: [121.6, 38.9], 厦门: [118.1, 24.5], 济南: [117.0, 36.7], 福州: [119.3, 26.1],
  哈尔滨: [126.6, 45.8], 昆明: [102.8, 25.0], 合肥: [117.3, 31.8], 石家庄: [114.5, 38.0],
  太原: [112.5, 37.9], 南昌: [115.9, 28.7], 贵阳: [106.7, 26.6], 兰州: [103.8, 36.1],
  海口: [110.3, 20.0], 南宁: [108.3, 22.8], 拉萨: [91.1, 29.6], 乌鲁木齐: [87.6, 43.8],
  呼和浩特: [111.7, 40.8], 银川: [106.3, 38.5], 西宁: [101.8, 36.6],
};

function resolveCoords(s: SessionDetailView): [number, number] | null {
  // 尝试城市名匹配
  if (s.city) {
    const key = s.city.replace(/市$/, "");
    if (CITY_COORDS[key]) return CITY_COORDS[key];
  }
  // 尝试国家名匹配
  if (s.country && COUNTRY_COORDS[s.country]) return COUNTRY_COORDS[s.country];
  // 根据 IP 简单 hash 给一个偏移避免完全重叠
  if (s.country === "中国" || s.countryCode === "CN") {
    const hash = s.ip ? s.ip.split(".").reduce((a, b) => a + Number(b), 0) : 0;
    return [100 + (hash % 30), 25 + (hash % 20)];
  }
  return null;
}

type Props = {
  sessions: SessionDetailView[];
  className?: string;
};

export function SessionLocationMap({ sessions, className }: Props) {
  const chartRef = useRef<HTMLDivElement | null>(null);
  const instanceRef = useRef<EChartsType | null>(null);
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";

  const geoQuery = useQuery({
    queryKey: ["geo-world-sessions"],
    queryFn: async () => {
      const res = await fetch(GEO_URL);
      return res.json();
    },
    staleTime: Infinity,
    gcTime: Infinity
  });

  const scatterData = useMemo(() => {
    const points: Array<{ value: [number, number, number]; session: SessionDetailView }> = [];
    for (const s of sessions) {
      const coords = resolveCoords(s);
      if (coords) {
        // 添加小随机偏移避免完全重叠
        const jitterLng = (Math.random() - 0.5) * 2;
        const jitterLat = (Math.random() - 0.5) * 2;
        points.push({
          value: [coords[0] + jitterLng, coords[1] + jitterLat, 1],
          session: s
        });
      }
    }
    return points;
  }, [sessions]);

  useEffect(() => {
    if (!chartRef.current || !geoQuery.data) return;

    echarts.registerMap(MAP_NAME, geoQuery.data);
    const chart = echarts.init(chartRef.current, undefined, { renderer: "canvas" });
    instanceRef.current = chart;

    const borderColor = isDark ? "rgba(255,255,255,0.12)" : "rgba(0,0,0,0.08)";
    const areaColor = isDark ? "#1c1c1f" : "#f0f0f2";
    const emphasisColor = isDark ? "#2a2a2e" : "#e8e8ec";
    const scatterColor = isDark ? "#60a5fa" : "#3b82f6";

    chart.setOption({
      backgroundColor: "transparent",
      tooltip: {
        trigger: "item",
        formatter: (params: any) => {
          const d = params.data?.session as SessionDetailView | undefined;
          if (!d) return "";
          const loc = [d.city, d.region, d.country].filter(Boolean).join(", ") || "未知";
          const time = d.issuedAt ? new Date(d.issuedAt).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : "";
          return `<div style="font-size:12px;line-height:1.6">
            <div><b>${d.ip}</b></div>
            <div>${loc}</div>
            ${d.userAgent ? `<div style="color:#999;max-width:240px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${d.userAgent}</div>` : ""}
            ${time ? `<div style="color:#999">${time}</div>` : ""}
          </div>`;
        }
      },
      geo: {
        map: MAP_NAME,
        roam: true,
        silent: false,
        zoom: 1.2,
        itemStyle: {
          areaColor,
          borderColor,
          borderWidth: 0.6
        },
        emphasis: {
          itemStyle: { areaColor: emphasisColor },
          label: { show: false }
        },
        label: { show: false }
      },
      series: [
        {
          type: "effectScatter",
          coordinateSystem: "geo",
          data: scatterData,
          symbolSize: 8,
          showEffectOn: "render",
          rippleEffect: { brushType: "stroke", scale: 3, period: 4 },
          itemStyle: { color: scatterColor, shadowBlur: 6, shadowColor: scatterColor },
          zlevel: 1
        }
      ]
    });

    const onResize = () => chart.resize();
    window.addEventListener("resize", onResize);
    const resizeObserver = new ResizeObserver(() => chart.resize());
    resizeObserver.observe(chartRef.current);

    return () => {
      window.removeEventListener("resize", onResize);
      resizeObserver.disconnect();
      chart.dispose();
      instanceRef.current = null;
    };
  }, [geoQuery.data, scatterData, isDark]);

  if (geoQuery.isLoading) {
    return <Skeleton className={`h-80 w-full rounded-xl ${className || ""}`} />;
  }

  if (!geoQuery.data) {
    return null;
  }

  return (
    <div ref={chartRef} className={`h-80 w-full rounded-xl border bg-card ${className || ""}`} />
  );
}
