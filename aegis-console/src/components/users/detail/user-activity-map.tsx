"use client";

// 用户活动地图 —— deck.gl ⋈ MapLibre（GPU 渲染）
//
// 回答一个问题：这个账号在**哪些地方**活动过，其中有没有说不通的。
//
// 三类数据画在同一张图上，因为它们本来就是同一件事的三个切面：
//   活跃会话   现在还有效的令牌在哪
//   登录记录   成功 / 失败 / 被拦截各发生在哪
//   位移轨迹   按时间连起来的相邻两次活动；超过民航速度的那一段标红
//
// 内网 / 回环 / 链路本地地址**不伪造地理位置**：GeoIP 对它们没有任何结论，
// 从前那版按 IP 求和取模散布在中国上空，看起来像真的，其实是噪音。
// 现在它们统一收敛到一个「服务器地址」节点（位置与攻击飞线图共用同一份配置）。
//
// 渲染栈与 components/security/attack-flight-map 同源：底图 MapLibre，
// 数据层 deck.gl MapboxOverlay（叠加模式，瓦片被墙时图层仍可见）。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Map as MapLibreMap, IControl } from "maplibre-gl";
import { MapboxOverlay } from "@deck.gl/mapbox";
import { ArcLayer, ScatterplotLayer } from "@deck.gl/layers";
import { HeatmapLayer } from "@deck.gl/aggregation-layers";
import type { Layer, PickingInfo } from "@deck.gl/core";
import { useTheme } from "next-themes";
import { Gauge, Loader2, MapPinned, Route, Server, Settings2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { MapLibreMap as BaseMap } from "@/components/maps/maplibre-map";
import { ServerLocationDialog } from "@/components/maps/server-location-dialog";
import { useServerLocation } from "@/lib/geo/server-location";
import { classifyIp, isServerSideAddress } from "@/lib/geo/private-network";
import { coordsBounds, flagImgHtml, fmtCount, haversineKm } from "@/lib/geo/geo-map-shared";
import { useAdminUserSessionsQuery } from "@/lib/admin-hooks";
import { useAdminUserLoginAuditsQuery } from "@/lib/app-user-hooks";
import type { SessionDetailView } from "@/lib/api/apps";
import type { UserLoginAuditRecord } from "@/lib/api/types";

// ── 常量 ──────────────────────────

/** 画轨迹用的登录样本量（后端单页上限 100） */
const LOGIN_SAMPLE = 100;
/** 位置聚合精度：保留 1 位小数 ≈ 11km。GeoIP 的城市级坐标本就有这个量级的抖动 */
const GRID_DECIMALS = 1;
/** 最多画多少段轨迹（只留最近的） */
const MAX_SEGMENTS = 60;
/** 超过这个速度且跨度足够远，才算「不可能位移」——民航巡航约 900km/h */
const IMPOSSIBLE_KMH = 900;
const IMPOSSIBLE_MIN_KM = 500;

type EventStatus = "active" | "success" | "failed" | "blocked";

const STATUS_META: Record<EventStatus, { label: string; rgb: [number, number, number]; css: string }> = {
  active: { label: "活跃会话", rgb: [16, 185, 129], css: "#10b981" },
  success: { label: "登录成功", rgb: [59, 130, 246], css: "#3b82f6" },
  failed: { label: "登录失败", rgb: [245, 158, 11], css: "#f59e0b" },
  blocked: { label: "被拦截", rgb: [239, 68, 68], css: "#ef4444" }
};

const SERVER_RGB: [number, number, number] = [139, 92, 246]; // violet-500
const SERVER_CSS = "#8b5cf6";

/** 节点主色的优先级：风险信号盖过常态 */
const DOMINANCE: EventStatus[] = ["blocked", "failed", "active", "success"];

// ── 数据形状 ──────────────────────────

type ActivityEvent = {
  status: EventStatus;
  at: number;
  ip: string;
  lat: number | null;
  lng: number | null;
  /** 内网 / 回环 —— 落到服务器节点 */
  server: boolean;
  city?: string;
  region?: string;
  country?: string;
  countryCode?: string;
  isp?: string;
};

type ActivityNode = {
  key: string;
  lng: number;
  lat: number;
  server: boolean;
  label: string;
  sub: string;
  countryCode: string;
  counts: Record<EventStatus, number>;
  total: number;
  ips: string[];
  lastAt: number;
};

type TrailSegment = {
  from: [number, number];
  to: [number, number];
  fromLabel: string;
  toLabel: string;
  km: number;
  hours: number;
  kmh: number;
  impossible: boolean;
  at: number;
};

// ── 事件归一 ──────────────────────────

function loginStatus(raw?: string): EventStatus {
  if (raw === "success") return "success";
  if (raw === "blocked") return "blocked";
  return "failed";
}

function toTs(iso?: string | null): number {
  if (!iso) return 0;
  const t = new Date(iso).getTime();
  return Number.isFinite(t) ? t : 0;
}

function hasCoords(lat?: number | null, lng?: number | null): boolean {
  return (
    typeof lat === "number" &&
    typeof lng === "number" &&
    Number.isFinite(lat) &&
    Number.isFinite(lng) &&
    !(lat === 0 && lng === 0) // (0,0) 是几内亚湾，实际是「没解析出来」
  );
}

function sessionEvent(s: SessionDetailView): ActivityEvent {
  return {
    status: "active",
    at: toTs(s.issuedAt),
    ip: s.ip || "",
    lat: hasCoords(s.latitude, s.longitude) ? (s.latitude as number) : null,
    lng: hasCoords(s.latitude, s.longitude) ? (s.longitude as number) : null,
    server: isServerSideAddress(s.ip, s.isPrivate),
    city: s.city,
    region: s.region,
    country: s.country,
    countryCode: s.countryCode,
    isp: s.isp
  };
}

function loginEvent(l: UserLoginAuditRecord): ActivityEvent {
  return {
    status: loginStatus(l.status),
    at: toTs(l.createdAt),
    ip: l.loginIp || "",
    lat: hasCoords(l.latitude, l.longitude) ? (l.latitude as number) : null,
    lng: hasCoords(l.latitude, l.longitude) ? (l.longitude as number) : null,
    server: isServerSideAddress(l.loginIp, l.isPrivate),
    city: l.city,
    region: l.region,
    country: l.country,
    countryCode: l.countryCode,
    isp: l.isp
  };
}

function emptyCounts(): Record<EventStatus, number> {
  return { active: 0, success: 0, failed: 0, blocked: 0 };
}

/** 网格量化：聚合与轨迹必须用同一份实现，否则轨迹会找不到自己的节点 */
function snap(v: number): number {
  const f = 10 ** GRID_DECIMALS;
  return Math.round(v * f) / f;
}

function gridKey(lat: number, lng: number): string {
  return `${snap(lat).toFixed(GRID_DECIMALS)}|${snap(lng).toFixed(GRID_DECIMALS)}`;
}

/** 按网格聚合成节点；内网来源全部并入服务器节点，无坐标的公网来源单独计数 */
function buildNodes(
  events: ActivityEvent[],
  server: { name: string; lat: number; lng: number }
): { nodes: ActivityNode[]; unlocated: number; serverHits: number } {
  const byKey = new Map<string, ActivityNode>();
  let unlocated = 0;
  let serverHits = 0;

  for (const e of events) {
    let key: string;
    let lng: number;
    let lat: number;

    if (e.server) {
      key = "server";
      lng = server.lng;
      lat = server.lat;
      serverHits++;
    } else if (e.lat != null && e.lng != null) {
      lat = snap(e.lat);
      lng = snap(e.lng);
      key = gridKey(e.lat, e.lng);
    } else {
      unlocated++;
      continue;
    }

    let node = byKey.get(key);
    if (!node) {
      node = {
        key,
        lng,
        lat,
        server: e.server,
        label: e.server ? server.name : e.city || e.region || e.country || e.ip || "未知位置",
        sub: e.server ? classifyIp(e.ip).detail : [e.country, e.isp].filter(Boolean).join(" · "),
        countryCode: e.server ? "" : e.countryCode || "",
        counts: emptyCounts(),
        total: 0,
        ips: [],
        lastAt: 0
      };
      byKey.set(key, node);
    }
    node.counts[e.status]++;
    node.total++;
    // 节点落点用第一次见到的坐标，但展示名优先取更近一次里更具体的城市名
    if (!e.server && e.city && node.label !== e.city && e.at >= node.lastAt) node.label = e.city;
    if (e.at > node.lastAt) node.lastAt = e.at;
    if (e.ip && !node.ips.includes(e.ip)) node.ips.push(e.ip);
  }

  return { nodes: [...byKey.values()], unlocated, serverHits };
}

function dominantStatus(node: ActivityNode): EventStatus {
  for (const s of DOMINANCE) if (node.counts[s] > 0) return s;
  return "success";
}

/**
 * 位移轨迹：按时间升序，把相邻两次**有真实坐标**的活动连起来。
 *
 * 服务器节点不参与 —— 内网地址的"位置"是我们自己安排的，
 * 拿它算速度只会凭空造出一堆不可能位移。
 */
function buildTrail(events: ActivityEvent[], nodeOf: (e: ActivityEvent) => ActivityNode | null): TrailSegment[] {
  const seq = events
    .filter((e) => !e.server && e.lat != null && e.lng != null && e.at > 0)
    .sort((a, b) => a.at - b.at);

  const segments: TrailSegment[] = [];
  for (let i = 1; i < seq.length; i++) {
    const prev = seq[i - 1];
    const cur = seq[i];
    const a = nodeOf(prev);
    const b = nodeOf(cur);
    if (!a || !b || a.key === b.key) continue;

    const km = haversineKm(a.lat, a.lng, b.lat, b.lng);
    const hours = Math.max(0, (cur.at - prev.at) / 3_600_000);
    const kmh = hours > 0 ? km / hours : Infinity;
    segments.push({
      from: [a.lng, a.lat],
      to: [b.lng, b.lat],
      fromLabel: a.label,
      toLabel: b.label,
      km,
      hours,
      kmh,
      impossible: km >= IMPOSSIBLE_MIN_KM && kmh > IMPOSSIBLE_KMH,
      at: cur.at
    });
  }
  return segments.slice(-MAX_SEGMENTS);
}

// ── 展示小工具 ──────────────────────────

function esc(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function fmtTime(ts: number) {
  if (!ts) return "—";
  return new Date(ts).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function fmtSpeed(seg: TrailSegment) {
  if (!Number.isFinite(seg.kmh)) return "同一时刻";
  if (seg.kmh >= 10) return `${Math.round(seg.kmh).toLocaleString("zh-CN")} km/h`;
  return `${seg.kmh.toFixed(1)} km/h`;
}

function fmtGap(hours: number) {
  if (hours <= 0) return "0 分钟";
  if (hours < 1) return `${Math.round(hours * 60)} 分钟`;
  if (hours < 48) return `${hours.toFixed(1)} 小时`;
  return `${(hours / 24).toFixed(1)} 天`;
}

const TOOLTIP_STYLE = {
  background: "var(--popover)",
  color: "var(--popover-foreground)",
  border: "1px solid var(--border)",
  borderRadius: "10px",
  padding: "0",
  boxShadow: "0 8px 24px color-mix(in srgb, var(--foreground) 10%, transparent)"
};

function nodeTooltip(d: ActivityNode) {
  const rows = DOMINANCE.filter((s) => d.counts[s] > 0)
    .map(
      (s) =>
        `<span style="display:inline-flex;align-items:center;gap:4px;margin-right:8px">
           <span style="width:6px;height:6px;border-radius:9999px;background:${STATUS_META[s].css}"></span>
           ${STATUS_META[s].label} <b>${d.counts[s]}</b>
         </span>`
    )
    .join("");
  const ips = d.ips.slice(0, 3).map(esc).join("、") + (d.ips.length > 3 ? ` 等 ${d.ips.length} 个` : "");
  return `<div style="padding:8px 10px;font-size:12px;line-height:1.7;max-width:280px">
      <div style="font-weight:600">${d.server ? "🖥 " : flagImgHtml(d.countryCode)}${esc(d.label)}</div>
      ${d.sub ? `<div style="opacity:.7">${esc(d.sub)}</div>` : ""}
      <div style="margin-top:2px">${rows}</div>
      <div style="opacity:.7">${ips}</div>
      <div style="opacity:.7">最近 ${fmtTime(d.lastAt)}</div>
    </div>`;
}

function segmentTooltip(d: TrailSegment) {
  return `<div style="padding:8px 10px;font-size:12px;line-height:1.7;max-width:280px">
      <div style="font-weight:600">${esc(d.fromLabel)} → ${esc(d.toLabel)}</div>
      <div style="opacity:.75">${Math.round(d.km).toLocaleString("zh-CN")} km · 间隔 ${fmtGap(d.hours)}</div>
      <div style="opacity:.75">折合 ${fmtSpeed(d)}</div>
      ${
        d.impossible
          ? `<div style="color:${STATUS_META.blocked.css};font-weight:600">超过民航速度，位移说不通</div>`
          : ""
      }
      <div style="opacity:.6">${fmtTime(d.at)}</div>
    </div>`;
}

// ── 组件 ──────────────────────────

type LayerStyle = "points" | "heat";

const STYLE_KEY = "aegis:activity-map:layer-style";
const TRAIL_KEY = "aegis:activity-map:trail";

function loadPref<T extends string>(key: string, allowed: T[], fallback: T): T {
  if (typeof window === "undefined") return fallback;
  try {
    const v = localStorage.getItem(key) as T | null;
    return v && allowed.includes(v) ? v : fallback;
  } catch {
    return fallback;
  }
}

function persist(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // 隐私模式忽略
  }
}

export function UserActivityMap({ appKey, userId }: { appKey: string; userId: number }) {
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";
  const server = useServerLocation();

  const [layerStyle, setLayerStyle] = useState<LayerStyle>(() =>
    loadPref(STYLE_KEY, ["points", "heat"], "points")
  );
  const [showTrail, setShowTrail] = useState(() => loadPref(TRAIL_KEY, ["on", "off"], "on") === "on");
  const [showSettings, setShowSettings] = useState(false);

  // 会话查询与「活跃会话」面板同一个 key，命中同一份缓存，不会多打一次请求
  const sessionsQuery = useAdminUserSessionsQuery(appKey, userId);
  // 登录记录另取一份不带状态筛选的大样本：地图要画的是全貌，
  // 页面上那份会跟着「仅失败」这类筛选变，拿它画图会得出「这个人只在国外失败过」的错觉
  const loginsQuery = useAdminUserLoginAuditsQuery(appKey, userId, { limit: LOGIN_SAMPLE });

  const loading = sessionsQuery.isLoading || loginsQuery.isLoading;

  const events = useMemo(() => {
    const list: ActivityEvent[] = [];
    for (const s of sessionsQuery.data?.items ?? []) list.push(sessionEvent(s));
    for (const l of loginsQuery.data?.items ?? []) list.push(loginEvent(l));
    return list;
  }, [sessionsQuery.data, loginsQuery.data]);

  const { nodes, unlocated, serverHits } = useMemo(() => buildNodes(events, server), [events, server]);

  const trail = useMemo(() => {
    if (nodes.length === 0) return [];
    const index = new Map(nodes.map((n) => [n.key, n]));
    const locate = (e: ActivityEvent): ActivityNode | null => {
      if (e.lat == null || e.lng == null) return null;
      return index.get(gridKey(e.lat, e.lng)) ?? null;
    };
    return buildTrail(events, locate);
  }, [events, nodes]);

  const impossibleCount = useMemo(() => trail.filter((s) => s.impossible).length, [trail]);
  const maxTotal = useMemo(() => nodes.reduce((m, n) => Math.max(m, n.total), 1), [nodes]);
  const activeNodes = useMemo(() => nodes.filter((n) => n.counts.active > 0), [nodes]);

  // ── deck.gl overlay ──
  const [map, setMap] = useState<MapLibreMap | null>(null);
  const overlayRef = useRef<MapboxOverlay | null>(null);

  const handleMapReady = useCallback((m: MapLibreMap) => {
    const overlay = new MapboxOverlay({
      interleaved: false,
      layers: [],
      getTooltip: (info: PickingInfo) => {
        const obj = info.object as ActivityNode | TrailSegment | undefined;
        if (!obj) return null;
        if ("counts" in obj) return { html: nodeTooltip(obj), style: TOOLTIP_STYLE };
        if ("kmh" in obj) return { html: segmentTooltip(obj), style: TOOLTIP_STYLE };
        return null;
      }
    });
    m.addControl(overlay as unknown as IControl);
    overlayRef.current = overlay;
    setMap(m);
  }, []);

  useEffect(() => {
    return () => {
      overlayRef.current?.finalize();
      overlayRef.current = null;
    };
  }, []);

  const prefersReducedMotion = useMemo(
    () => typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches,
    []
  );
  const animate = layerStyle === "points" && activeNodes.length > 0 && !prefersReducedMotion;

  // ── 静态图层 ──
  const staticLayers = useMemo<Layer[]>(() => {
    if (nodes.length === 0) return [];
    const out: Layer[] = [];

    if (layerStyle === "heat") {
      out.push(
        new HeatmapLayer<ActivityNode>({
          id: "activity-heat",
          data: nodes,
          getPosition: (d) => [d.lng, d.lat],
          getWeight: (d) => d.total,
          radiusPixels: 48,
          intensity: 1.1,
          aggregation: "SUM",
          colorRange: [
            [96, 165, 250, 40],
            [96, 165, 250, 120],
            [59, 130, 246, 170],
            [99, 102, 241, 205],
            [139, 92, 246, 230],
            [217, 70, 239, 255]
          ]
        })
      );
    } else {
      if (showTrail && trail.length > 0) {
        out.push(
          new ArcLayer<TrailSegment>({
            id: "activity-trail",
            data: trail,
            greatCircle: true,
            getSourcePosition: (d) => d.from,
            getTargetPosition: (d) => d.to,
            getSourceColor: (d) =>
              (d.impossible ? [...STATUS_META.blocked.rgb, 210] : [99, 102, 241, 120]) as [
                number,
                number,
                number,
                number
              ],
            getTargetColor: (d) =>
              (d.impossible ? [...STATUS_META.blocked.rgb, 230] : [139, 92, 246, 150]) as [
                number,
                number,
                number,
                number
              ],
            getWidth: (d) => (d.impossible ? 2.6 : 1.4),
            widthUnits: "pixels",
            getHeight: 0.25,
            pickable: true,
            autoHighlight: true,
            highlightColor: [99, 102, 241, 255]
          })
        );
      }

      out.push(
        new ScatterplotLayer<ActivityNode>({
          id: "activity-nodes",
          data: nodes,
          getPosition: (d) => [d.lng, d.lat],
          getRadius: (d) => (d.server ? 7 : 3) + 9 * Math.sqrt(d.total / maxTotal),
          radiusUnits: "pixels",
          getFillColor: (d) =>
            (d.server
              ? [...SERVER_RGB, 225]
              : [...STATUS_META[dominantStatus(d)].rgb, 205]) as [number, number, number, number],
          stroked: true,
          getLineColor: dark ? [9, 9, 11, 230] : [255, 255, 255, 235],
          getLineWidth: 1.4,
          lineWidthUnits: "pixels",
          pickable: true,
          autoHighlight: true,
          highlightColor: [99, 102, 241, 255]
        })
      );

      // 活跃会话所在位置额外套一圈：颜色已经被风险信号占用，
      // 「此刻有人拿着令牌」这条事实必须有独立的视觉通道
      if (activeNodes.length > 0) {
        out.push(
          new ScatterplotLayer<ActivityNode>({
            id: "activity-active-ring",
            data: activeNodes,
            getPosition: (d) => [d.lng, d.lat],
            getRadius: (d) => (d.server ? 12 : 8) + 9 * Math.sqrt(d.total / maxTotal),
            radiusUnits: "pixels",
            filled: false,
            stroked: true,
            getLineColor: [...STATUS_META.active.rgb, 200] as [number, number, number, number],
            getLineWidth: 1.4,
            lineWidthUnits: "pixels",
            pickable: false
          })
        );
      }
    }

    return out;
  }, [nodes, trail, showTrail, layerStyle, maxTotal, dark, activeNodes]);

  // ── 活跃节点的呼吸光圈（rAF）──
  const staticLayersRef = useRef<Layer[]>(staticLayers);
  const activeNodesRef = useRef<ActivityNode[]>(activeNodes);
  const maxTotalRef = useRef(maxTotal);
  useEffect(() => {
    staticLayersRef.current = staticLayers;
  }, [staticLayers]);
  useEffect(() => {
    activeNodesRef.current = activeNodes;
  }, [activeNodes]);
  useEffect(() => {
    maxTotalRef.current = maxTotal;
  }, [maxTotal]);

  useEffect(() => {
    const ov = overlayRef.current;
    if (!ov || !map) return;

    if (!animate) {
      ov.setProps({ layers: staticLayers });
      return;
    }

    const PERIOD = 2.6; // 秒
    let raf = 0;
    const start = performance.now();

    const frame = (now: number) => {
      const t = (((now - start) / 1000) % PERIOD) / PERIOD;
      const eased = 1 - (1 - t) * (1 - t);
      ov.setProps({
        layers: [
          ...staticLayersRef.current,
          new ScatterplotLayer<ActivityNode>({
            id: "activity-pulse",
            data: activeNodesRef.current,
            getPosition: (d) => [d.lng, d.lat],
            getRadius: (d) =>
              ((d.server ? 12 : 8) + 9 * Math.sqrt(d.total / maxTotalRef.current)) * (1 + 1.8 * eased),
            radiusUnits: "pixels",
            filled: false,
            stroked: true,
            getLineColor: [
              ...STATUS_META.active.rgb,
              Math.round(150 * (1 - t) * (1 - t))
            ] as [number, number, number, number],
            getLineWidth: 1.2,
            lineWidthUnits: "pixels",
            pickable: false,
            updateTriggers: { getRadius: eased, getLineColor: t }
          })
        ]
      });
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, [animate, staticLayers, map]);

  // ── 自动取景 ──
  // 节点集合变了才重新取景：每次轮询刷新都拉一次视野会把正在看细节的人甩走
  const fitSignatureRef = useRef("");
  useEffect(() => {
    if (!map || nodes.length === 0) return;
    const signature = nodes.map((n) => n.key).sort().join("|");
    if (signature === fitSignatureRef.current) return;
    fitSignatureRef.current = signature;
    const bounds = coordsBounds(nodes.map((n) => [n.lng, n.lat] as [number, number]));
    if (bounds) map.fitBounds(bounds, { padding: 80, maxZoom: 6, duration: 700 });
  }, [map, nodes]);

  const switchStyle = useCallback((v: LayerStyle) => {
    setLayerStyle(v);
    persist(STYLE_KEY, v);
  }, []);

  const toggleTrail = useCallback(() => {
    setShowTrail((prev) => {
      persist(TRAIL_KEY, prev ? "off" : "on");
      return !prev;
    });
  }, []);

  const isEmpty = !loading && nodes.length === 0;
  const totalEvents = events.length;

  if (loading) {
    return <Skeleton className="h-[380px] w-full rounded-xl" />;
  }

  return (
    <div className="space-y-3">
      {/* 控制行 */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-1 rounded-lg border bg-muted/50 p-0.5">
            {(["points", "heat"] as LayerStyle[]).map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => switchStyle(v)}
                className={cn(
                  "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors",
                  layerStyle === v
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                {v === "points" ? <MapPinned className="size-3.5" /> : <Gauge className="size-3.5" />}
                {v === "points" ? "活动点" : "密度热力"}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={toggleTrail}
            disabled={layerStyle !== "points"}
            className={cn(
              "flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors disabled:opacity-40",
              showTrail && layerStyle === "points"
                ? "border-indigo-500/40 bg-indigo-500/10 text-indigo-600 dark:text-indigo-400"
                : "bg-muted/50 text-muted-foreground hover:text-foreground"
            )}
          >
            <Route className="size-3.5" />
            位移轨迹
            {trail.length > 0 && <span className="tabular-nums opacity-70">{trail.length}</span>}
          </button>
          {impossibleCount > 0 && (
            <span className="flex items-center gap-1 rounded-lg border border-destructive/40 bg-destructive/10 px-2.5 py-1.5 text-xs font-medium text-destructive">
              {impossibleCount} 段位移说不通
            </span>
          )}
        </div>
        <Button size="sm" variant="outline" className="h-7 gap-1.5 text-xs" onClick={() => setShowSettings(true)}>
          <Settings2 className="size-3.5" />
          端点位置
        </Button>
      </div>

      {/* 地图 */}
      <BaseMap className="h-[380px] xl:h-[440px]" zoom={1.5} onMapReady={handleMapReady}>
        {isEmpty && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
            <span className="rounded-full border border-border bg-card/95 px-4 py-1.5 text-xs text-muted-foreground backdrop-blur-sm">
              {totalEvents > 0 ? "这些活动都没有可用坐标" : "没有可展示的活动"}
            </span>
          </div>
        )}
        {loginsQuery.isFetching && !loading && (
          <div className="absolute inset-x-0 top-3 z-10 flex justify-center">
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-card/95 px-3 py-1 text-[11px] text-muted-foreground backdrop-blur-sm">
              <Loader2 className="size-3 animate-spin" />
              正在刷新
            </span>
          </div>
        )}
        {/* 图例 */}
        <div className="absolute top-3 left-3 z-10 flex flex-wrap items-center gap-x-2.5 gap-y-1 rounded-lg border border-border bg-card/95 px-2.5 py-1.5 backdrop-blur-sm">
          {DOMINANCE.map((s) => (
            <span key={s} className="flex items-center gap-1 text-[10.5px] text-muted-foreground">
              <span className="size-2 rounded-full" style={{ backgroundColor: STATUS_META[s].css }} />
              {STATUS_META[s].label}
            </span>
          ))}
          <span className="flex items-center gap-1 border-l border-border pl-2 text-[10.5px] text-muted-foreground">
            <span className="size-2 rounded-full" style={{ backgroundColor: SERVER_CSS }} />
            服务器地址
          </span>
        </div>
      </BaseMap>

      {/* 汇总：地图上看不见的那部分事实（内网收敛、无法定位）必须写出来 */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
        <span>
          {fmtCount(nodes.length)} 个位置 · {fmtCount(totalEvents)} 次活动
        </span>
        {serverHits > 0 && (
          <span className="flex items-center gap-1">
            <Server className="size-3" style={{ color: SERVER_CSS }} />
            {fmtCount(serverHits)} 次来自内网 / 回环，已归到「{server.name}」
          </span>
        )}
        {unlocated > 0 && <span>{fmtCount(unlocated)} 次的 IP 无法定位，未画在图上</span>}
        {trail.length > 0 && <span>轨迹取最近 {fmtCount(trail.length)} 段位移</span>}
      </div>

      <ServerLocationDialog
        open={showSettings}
        onOpenChange={setShowSettings}
        value={server}
        description="内网 / 回环来源没有地理位置，统一画在这个点上；该配置与攻击飞线图共用。"
        onSaved={() => {
          fitSignatureRef.current = "";
        }}
      />
    </div>
  );
}
