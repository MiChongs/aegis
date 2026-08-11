"use client";

// 地理围栏绘制编辑器
//
// 交互模型（自研，无第三方绘制库）：
//   - 多边形：单击逐点绘制，Enter / 双击完成，Backspace 撤销上一点，Esc 取消
//   - 圆形：第一次单击定圆心，移动预览半径，第二次单击定半径
//   - 顶点编辑：拖拽顶点调整 / 拖拽中点插入新顶点 / 右键删除顶点
//
// 性能约定：绘制期间几何细节保存在 ref 中并直接写 GeoJSON source（不触发 React
// 重渲染），只有「加点 / 结束 / 模式切换」这类低频事件才同步到 state 驱动 UI。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Map as MapLibreMap, GeoJSONSource, MapMouseEvent, ExpressionSpecification } from "maplibre-gl";
import {
  CircleDashed,
  Hexagon,
  LandPlot,
  Loader2,
  LocateFixed,
  Pencil,
  Plus,
  RefreshCw,
  Spline,
  Trash2
} from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { MapLibreMap as BaseMap } from "@/components/maps/maplibre-map";
import { useAdminAppsQuery, useAdminToken, useIPBanModesQuery } from "@/lib/admin-hooks";
import {
  createGeoFence,
  deleteGeoFence,
  getGeoFences,
  previewGeoFence,
  toggleGeoFence,
  updateGeoFence,
  type GeoFenceEntry,
  type GeoFenceMode,
  type GeoFencePayload,
  type GeoFencePreviewResult
} from "@/lib/api/geo";
import {
  ALL_FENCE_MODES,
  FENCE_MODE_META,
  circleToRing,
  coordsBounds,
  fmtCount,
  fmtDateTime,
  fmtRadius,
  haversineKm
} from "@/lib/geo/geo-map-shared";

// ──────────────────────────────────────
// 几何工具
// ──────────────────────────────────────

type LngLat = [number, number];

type PendingGeometry =
  | { kind: "polygon"; ring: LngLat[] }
  | { kind: "circle"; center: LngLat; radiusM: number };

type DrawMode = "idle" | "polygon" | "circle" | "edit";

/** 绘制期间的全部几何细节（只存 ref，不进 React state） */
type DrawDetail = {
  mode: DrawMode;
  vertices: LngLat[];
  circleCenter: LngLat | null;
  circleRadiusM: number | null;
  editFenceId: number | null;
  editRing: LngLat[];
  dragIndex: number | null;
};

const emptyDraw = (): DrawDetail => ({
  mode: "idle",
  vertices: [],
  circleCenter: null,
  circleRadiusM: null,
  editFenceId: null,
  editRing: [],
  dragIndex: null
});

function closeRing(ring: LngLat[]): number[][] {
  if (ring.length === 0) return [];
  const [fx, fy] = ring[0];
  const [lx, ly] = ring[ring.length - 1];
  const closed = fx === lx && fy === ly ? [...ring] : [...ring, ring[0]];
  return closed.map(([lng, lat]) => [lng, lat]);
}

/** 提取可编辑外环：单环 Polygon 或单面单环 MultiPolygon；其余返回 null */
function editableRing(fence: GeoFenceEntry): LngLat[] | null {
  const g = fence.fence;
  if (!g) return null;
  let ring: number[][] | undefined;
  if (g.type === "Polygon" && g.coordinates.length === 1) {
    ring = g.coordinates[0];
  } else if (g.type === "MultiPolygon" && g.coordinates.length === 1 && g.coordinates[0].length === 1) {
    ring = g.coordinates[0][0];
  }
  if (!ring || ring.length < 4) return null;
  const open = ring.slice(0, -1); // 去掉闭合重复点
  return open.map((c) => [c[0], c[1]] as LngLat);
}

function fenceCoords(fence: GeoFenceEntry): LngLat[] {
  if (fence.fence) {
    const g = fence.fence;
    const out: LngLat[] = [];
    const polys = g.type === "Polygon" ? [g.coordinates] : g.coordinates;
    for (const poly of polys) {
      for (const ring of poly) {
        for (const c of ring) out.push([c[0], c[1]]);
      }
    }
    return out;
  }
  if (fence.centerLat != null && fence.centerLng != null && fence.radiusM != null) {
    return circleToRing(fence.centerLng, fence.centerLat, fence.radiusM).map((c) => [c[0], c[1]] as LngLat);
  }
  return [];
}

function fenceGeometrySummary(fence: GeoFenceEntry) {
  if (fence.fence) {
    const g = fence.fence;
    if (g.type === "Polygon") {
      return `多边形 · ${Math.max(0, (g.coordinates[0]?.length ?? 1) - 1)} 顶点`;
    }
    return `多面体 · ${g.coordinates.length} 个面`;
  }
  if (fence.radiusM != null) return `圆形 · 半径 ${fmtRadius(fence.radiusM)}`;
  return "几何缺失";
}

type Feature = {
  type: "Feature";
  geometry:
    | { type: "Point"; coordinates: number[] }
    | { type: "LineString"; coordinates: number[][] }
    | { type: "Polygon"; coordinates: number[][][] }
    | { type: "MultiPolygon"; coordinates: number[][][][] };
  properties: Record<string, string | number>;
};

function fc(features: Feature[]) {
  return { type: "FeatureCollection" as const, features };
}

function setSourceData(map: MapLibreMap, id: string, features: Feature[]) {
  const source = map.getSource(id) as GeoJSONSource | undefined;
  if (source) source.setData(fc(features));
}

function modeColorExpr(): ExpressionSpecification {
  return [
    "match",
    ["get", "mode"],
    "deny", FENCE_MODE_META.deny.color,
    "allow", FENCE_MODE_META.allow.color,
    "review", FENCE_MODE_META.review.color,
    "#71717a"
  ];
}

const DRAFT_COLOR = "#6366f1";

function toLocalInputValue(iso?: string | null) {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function FormField({
  label,
  required,
  hint,
  children
}: {
  label: string;
  required?: boolean;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <Label className="text-[12px] font-medium">
          {label}
          {required && <span className="ml-1 text-destructive">*</span>}
        </Label>
        {hint && <span className="truncate text-[10.5px] text-muted-foreground/80">{hint}</span>}
      </div>
      {children}
    </div>
  );
}

// ──────────────────────────────────────
// 面板
// ──────────────────────────────────────

export function GeoFencePanel() {
  const token = useAdminToken();
  const queryClient = useQueryClient();

  const fencesQuery = useQuery({
    queryKey: ["geo-fences", token],
    queryFn: () => getGeoFences(token as string),
    enabled: Boolean(token),
    staleTime: 15_000
  });
  const fences = useMemo(() => fencesQuery.data ?? [], [fencesQuery.data]);

  const appsQuery = useAdminAppsQuery();
  const apps = appsQuery.data ?? [];
  const appName = useCallback(
    (appId?: number | null) => {
      if (appId == null) return "平台级";
      return apps.find((a) => a.id === appId)?.name ?? `App #${appId}`;
    },
    [apps]
  );

  const modesQuery = useIPBanModesQuery();
  const banModes = modesQuery.data?.modes ?? [];
  const defaultBanMode = modesQuery.data?.default ?? "forbidden";

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: ["geo-fences"] }),
    [queryClient]
  );

  // ── 选中 / 绘制状态 ──
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [drawMode, setDrawMode] = useState<DrawMode>("idle");
  const [vertexCount, setVertexCount] = useState(0);
  const [radiusPreview, setRadiusPreview] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);

  const drawRef = useRef<DrawDetail>(emptyDraw());
  const mapInstanceRef = useRef<MapLibreMap | null>(null);
  const [map, setMap] = useState<MapLibreMap | null>(null);

  // ── 对话框状态 ──
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingFence, setEditingFence] = useState<GeoFenceEntry | null>(null);
  const [pendingGeometry, setPendingGeometry] = useState<PendingGeometry | null>(null);
  const [fName, setFName] = useState("");
  const [fMode, setFMode] = useState<GeoFenceMode>("deny");
  const [fApp, setFApp] = useState("platform");
  const [fBanMode, setFBanMode] = useState("__default__");
  const [fReason, setFReason] = useState("");
  const [fEnabled, setFEnabled] = useState(true);
  const [fExpires, setFExpires] = useState("");
  const [fCircleLat, setFCircleLat] = useState("");
  const [fCircleLng, setFCircleLng] = useState("");
  const [fCircleRadius, setFCircleRadius] = useState("");
  const [previewDays, setPreviewDays] = useState("7");
  const [previewResult, setPreviewResult] = useState<GeoFencePreviewResult | null>(null);

  // ──────────────────────────────────────
  // 绘制状态同步（ref → source，按需 → state）
  // ──────────────────────────────────────

  const syncDraft = useCallback(() => {
    const m = mapInstanceRef.current;
    if (!m) return;
    const d = drawRef.current;
    const features: Feature[] = [];

    if (d.mode === "polygon") {
      const pts = d.vertices;
      if (pts.length >= 2) {
        features.push({
          type: "Feature",
          geometry: { type: "LineString", coordinates: pts.map((p) => [p[0], p[1]]) },
          properties: { role: "line" }
        });
      }
      if (pts.length >= 3) {
        features.push({
          type: "Feature",
          geometry: { type: "Polygon", coordinates: [closeRing(pts)] },
          properties: { role: "fill" }
        });
      }
      pts.forEach(([lng, lat], i) =>
        features.push({
          type: "Feature",
          geometry: { type: "Point", coordinates: [lng, lat] },
          properties: { role: "vertex", idx: i }
        })
      );
    }

    if (d.mode === "circle" && d.circleCenter) {
      features.push({
        type: "Feature",
        geometry: { type: "Point", coordinates: [d.circleCenter[0], d.circleCenter[1]] },
        properties: { role: "vertex", idx: 0 }
      });
      if (d.circleRadiusM && d.circleRadiusM > 0) {
        features.push({
          type: "Feature",
          geometry: {
            type: "Polygon",
            coordinates: [circleToRing(d.circleCenter[0], d.circleCenter[1], d.circleRadiusM)]
          },
          properties: { role: "fill" }
        });
      }
    }

    setSourceData(m, "fence-draft", features);
  }, []);

  const syncEdit = useCallback(() => {
    const m = mapInstanceRef.current;
    if (!m) return;
    const d = drawRef.current;
    const features: Feature[] = [];
    if (d.mode === "edit" && d.editRing.length >= 3) {
      features.push({
        type: "Feature",
        geometry: { type: "Polygon", coordinates: [closeRing(d.editRing)] },
        properties: { role: "fill" }
      });
      d.editRing.forEach(([lng, lat], i) =>
        features.push({
          type: "Feature",
          geometry: { type: "Point", coordinates: [lng, lat] },
          properties: { role: "vertex", idx: i }
        })
      );
      for (let i = 0; i < d.editRing.length; i++) {
        const a = d.editRing[i];
        const b = d.editRing[(i + 1) % d.editRing.length];
        features.push({
          type: "Feature",
          geometry: { type: "Point", coordinates: [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2] },
          properties: { role: "midpoint", idx: i }
        });
      }
    }
    setSourceData(m, "fence-edit", features);
  }, []);

  const resetDraw = useCallback(() => {
    drawRef.current = emptyDraw();
    setDrawMode("idle");
    setVertexCount(0);
    setRadiusPreview(null);
    setDragging(false);
    const m = mapInstanceRef.current;
    if (m) {
      m.doubleClickZoom.enable();
      m.dragPan.enable();
      setSourceData(m, "fence-draft", []);
      setSourceData(m, "fence-edit", []);
    }
  }, []);

  // ──────────────────────────────────────
  // 对话框
  // ──────────────────────────────────────

  const openCreateDialog = useCallback((geometry: PendingGeometry) => {
    setEditingFence(null);
    setPendingGeometry(geometry);
    setFName("");
    setFMode("deny");
    setFApp("platform");
    setFBanMode("__default__");
    setFReason("");
    setFEnabled(true);
    setFExpires("");
    setPreviewResult(null);
    setDialogOpen(true);
  }, []);

  const openEditDialog = useCallback((fence: GeoFenceEntry) => {
    setEditingFence(fence);
    setPendingGeometry(null);
    setFName(fence.name);
    setFMode(fence.mode);
    setFApp(fence.appId == null ? "platform" : String(fence.appId));
    setFBanMode(fence.banMode || "__default__");
    setFReason(fence.reason || "");
    setFEnabled(fence.enabled);
    setFExpires(toLocalInputValue(fence.expiresAt));
    setFCircleLat(fence.centerLat != null ? String(fence.centerLat) : "");
    setFCircleLng(fence.centerLng != null ? String(fence.centerLng) : "");
    setFCircleRadius(fence.radiusM != null ? String(fence.radiusM) : "");
    setPreviewResult(null);
    setDialogOpen(true);
  }, []);

  /** 组装提交载荷；geometry 错误时返回 null 并 toast */
  const buildPayload = useCallback((): GeoFencePayload | null => {
    if (!fName.trim()) {
      toast.error("请输入围栏名称");
      return null;
    }
    const payload: GeoFencePayload = {
      appId: fApp === "platform" ? null : Number(fApp),
      name: fName.trim(),
      mode: fMode,
      banMode: fBanMode === "__default__" ? "" : fBanMode,
      reason: fReason.trim(),
      enabled: fEnabled,
      expiresAt: ""
    };
    if (fExpires) {
      const d = new Date(fExpires);
      if (Number.isNaN(d.getTime())) {
        toast.error("过期时间格式无效");
        return null;
      }
      payload.expiresAt = d.toISOString();
    }

    if (pendingGeometry) {
      if (pendingGeometry.kind === "polygon") {
        payload.fence = { type: "Polygon", coordinates: [closeRing(pendingGeometry.ring)] };
      } else {
        payload.centerLng = pendingGeometry.center[0];
        payload.centerLat = pendingGeometry.center[1];
        payload.radiusM = pendingGeometry.radiusM;
      }
      return payload;
    }

    if (editingFence) {
      if (editingFence.fence) {
        payload.fence = editingFence.fence;
        return payload;
      }
      const lat = Number(fCircleLat);
      const lng = Number(fCircleLng);
      const radius = Number(fCircleRadius);
      if (!Number.isFinite(lat) || lat < -90 || lat > 90) {
        toast.error("圆心纬度无效（-90 ~ 90）");
        return null;
      }
      if (!Number.isFinite(lng) || lng < -180 || lng > 180) {
        toast.error("圆心经度无效（-180 ~ 180）");
        return null;
      }
      if (!Number.isFinite(radius) || radius <= 0) {
        toast.error("半径必须大于 0（单位：米）");
        return null;
      }
      payload.centerLat = lat;
      payload.centerLng = lng;
      payload.radiusM = radius;
      return payload;
    }

    toast.error("缺少围栏几何，请先在地图上绘制");
    return null;
  }, [fName, fApp, fMode, fBanMode, fReason, fEnabled, fExpires, pendingGeometry, editingFence, fCircleLat, fCircleLng, fCircleRadius]);

  const saveMutation = useMutation({
    mutationFn: async (payload: GeoFencePayload) => {
      if (editingFence) return updateGeoFence(token as string, editingFence.id, payload);
      return createGeoFence(token as string, payload);
    },
    onSuccess: () => {
      toast.success(editingFence ? "围栏已更新" : "围栏已创建");
      setDialogOpen(false);
      resetDraw();
      invalidate();
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "保存失败")
  });

  const previewMutation = useMutation({
    mutationFn: (payload: GeoFencePayload & { windowDays: number }) =>
      previewGeoFence(token as string, payload),
    onSuccess: (data) => setPreviewResult(data),
    onError: (err) => toast.error(err instanceof Error ? err.message : "回测失败")
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      toggleGeoFence(token as string, id, enabled),
    onSuccess: invalidate,
    onError: (err) => toast.error(err instanceof Error ? err.message : "切换失败")
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteGeoFence(token as string, id),
    onSuccess: () => {
      toast.success("围栏已删除");
      invalidate();
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "删除失败")
  });

  const geometrySaveMutation = useMutation({
    mutationFn: ({ fence, ring }: { fence: GeoFenceEntry; ring: LngLat[] }) =>
      updateGeoFence(token as string, fence.id, {
        appId: fence.appId ?? null,
        name: fence.name,
        mode: fence.mode,
        banMode: fence.banMode || "",
        reason: fence.reason || "",
        enabled: fence.enabled,
        expiresAt: fence.expiresAt || "",
        fence: { type: "Polygon", coordinates: [closeRing(ring)] }
      }),
    onSuccess: () => {
      toast.success("几何已更新");
      resetDraw();
      invalidate();
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "几何保存失败")
  });

  // ──────────────────────────────────────
  // 绘制动作（供地图事件 / 工具栏调用）
  // ──────────────────────────────────────

  const finishPolygon = useCallback(() => {
    const d = drawRef.current;
    if (d.mode !== "polygon") return;
    // 去掉相邻重复点（双击会先触发两次 click）
    const pts = d.vertices.filter((p, i, arr) => {
      if (i === 0) return true;
      const prev = arr[i - 1];
      return Math.abs(p[0] - prev[0]) > 1e-9 || Math.abs(p[1] - prev[1]) > 1e-9;
    });
    if (pts.length < 3) {
      toast.error("多边形至少需要 3 个顶点");
      return;
    }
    openCreateDialog({ kind: "polygon", ring: pts });
  }, [openCreateDialog]);

  const actionsRef = useRef({ finishPolygon, openCreateDialog, syncDraft, syncEdit, resetDraw });
  actionsRef.current = { finishPolygon, openCreateDialog, syncDraft, syncEdit, resetDraw };

  const startPolygon = useCallback(() => {
    resetDraw();
    drawRef.current = { ...emptyDraw(), mode: "polygon" };
    setDrawMode("polygon");
    setSelectedId(null);
    mapInstanceRef.current?.doubleClickZoom.disable();
  }, [resetDraw]);

  const startCircle = useCallback(() => {
    resetDraw();
    drawRef.current = { ...emptyDraw(), mode: "circle" };
    setDrawMode("circle");
    setSelectedId(null);
    mapInstanceRef.current?.doubleClickZoom.disable();
  }, [resetDraw]);

  const startEditGeometry = useCallback(
    (fence: GeoFenceEntry) => {
      const ring = editableRing(fence);
      if (!ring) {
        toast.error("该围栏为圆形或复杂多面几何，请通过「编辑」对话框调整参数");
        return;
      }
      resetDraw();
      drawRef.current = { ...emptyDraw(), mode: "edit", editFenceId: fence.id, editRing: ring };
      setDrawMode("edit");
      setSelectedId(fence.id);
      setVertexCount(ring.length);
      syncEdit();
      const bounds = coordsBounds(ring);
      if (bounds) mapInstanceRef.current?.fitBounds(bounds, { padding: 80, duration: 500, maxZoom: 12 });
    },
    [resetDraw, syncEdit]
  );

  const saveEditGeometry = useCallback(() => {
    const d = drawRef.current;
    if (d.mode !== "edit" || d.editFenceId == null) return;
    const fence = fences.find((f) => f.id === d.editFenceId);
    if (!fence) return;
    if (d.editRing.length < 3) {
      toast.error("多边形至少需要 3 个顶点");
      return;
    }
    geometrySaveMutation.mutate({ fence, ring: d.editRing });
  }, [fences, geometrySaveMutation]);

  // ──────────────────────────────────────
  // 地图初始化与事件
  // ──────────────────────────────────────

  const handleMapReady = useCallback((m: MapLibreMap) => {
    mapInstanceRef.current = m;

    m.addSource("fences", { type: "geojson", data: fc([]) });
    m.addSource("fence-draft", { type: "geojson", data: fc([]) });
    m.addSource("fence-edit", { type: "geojson", data: fc([]) });

    // 既有围栏
    m.addLayer({
      id: "fences-fill",
      type: "fill",
      source: "fences",
      paint: {
        "fill-color": modeColorExpr(),
        "fill-opacity": [
          "case",
          ["==", ["get", "selected"], 1], 0.24,
          ["==", ["get", "enabled"], 1], 0.12,
          0.05
        ]
      }
    });
    m.addLayer({
      id: "fences-line",
      type: "line",
      source: "fences",
      filter: ["==", ["get", "enabled"], 1],
      paint: {
        "line-color": modeColorExpr(),
        "line-width": ["case", ["==", ["get", "selected"], 1], 2.4, 1.4]
      }
    });
    m.addLayer({
      id: "fences-line-disabled",
      type: "line",
      source: "fences",
      filter: ["==", ["get", "enabled"], 0],
      paint: {
        "line-color": modeColorExpr(),
        "line-width": 1.2,
        "line-dasharray": [2.2, 2.2],
        "line-opacity": 0.7
      }
    });

    // 绘制草稿
    m.addLayer({
      id: "draft-fill",
      type: "fill",
      source: "fence-draft",
      filter: ["==", ["get", "role"], "fill"],
      paint: { "fill-color": DRAFT_COLOR, "fill-opacity": 0.1 }
    });
    m.addLayer({
      id: "draft-line",
      type: "line",
      source: "fence-draft",
      filter: ["!=", ["get", "role"], "vertex"],
      paint: { "line-color": DRAFT_COLOR, "line-width": 1.8, "line-dasharray": [1.6, 1.4] }
    });
    m.addLayer({
      id: "draft-vertices",
      type: "circle",
      source: "fence-draft",
      filter: ["==", ["get", "role"], "vertex"],
      paint: {
        "circle-radius": 4.2,
        "circle-color": "#ffffff",
        "circle-stroke-color": DRAFT_COLOR,
        "circle-stroke-width": 2
      }
    });

    // 顶点编辑
    m.addLayer({
      id: "edit-fill",
      type: "fill",
      source: "fence-edit",
      filter: ["==", ["get", "role"], "fill"],
      paint: { "fill-color": DRAFT_COLOR, "fill-opacity": 0.08 }
    });
    m.addLayer({
      id: "edit-line",
      type: "line",
      source: "fence-edit",
      filter: ["==", ["get", "role"], "fill"],
      paint: { "line-color": DRAFT_COLOR, "line-width": 1.8 }
    });
    m.addLayer({
      id: "edit-midpoints",
      type: "circle",
      source: "fence-edit",
      filter: ["==", ["get", "role"], "midpoint"],
      paint: {
        "circle-radius": 3.4,
        "circle-color": DRAFT_COLOR,
        "circle-opacity": 0.55,
        "circle-stroke-color": "#ffffff",
        "circle-stroke-width": 1
      }
    });
    m.addLayer({
      id: "edit-vertices",
      type: "circle",
      source: "fence-edit",
      filter: ["==", ["get", "role"], "vertex"],
      paint: {
        "circle-radius": 5,
        "circle-color": "#ffffff",
        "circle-stroke-color": DRAFT_COLOR,
        "circle-stroke-width": 2.2
      }
    });

    // ── 单击 ──
    m.on("click", (e: MapMouseEvent) => {
      const d = drawRef.current;
      const a = actionsRef.current;
      if (d.mode === "polygon") {
        d.vertices = [...d.vertices, [e.lngLat.lng, e.lngLat.lat]];
        setVertexCount(d.vertices.length);
        a.syncDraft();
        return;
      }
      if (d.mode === "circle") {
        if (!d.circleCenter) {
          d.circleCenter = [e.lngLat.lng, e.lngLat.lat];
          a.syncDraft();
          return;
        }
        const radiusM =
          haversineKm(d.circleCenter[1], d.circleCenter[0], e.lngLat.lat, e.lngLat.lng) * 1000;
        if (radiusM < 50) {
          toast.error("半径过小（至少 50 米），请把光标移远后再点击");
          return;
        }
        d.circleRadiusM = radiusM;
        a.syncDraft();
        a.openCreateDialog({ kind: "circle", center: d.circleCenter, radiusM: Math.round(radiusM) });
        return;
      }
      if (d.mode === "idle") {
        const hits = m.queryRenderedFeatures(e.point, { layers: ["fences-fill"] });
        const id = hits[0]?.properties?.id;
        setSelectedId(typeof id === "number" ? id : id != null ? Number(id) : null);
      }
    });

    // ── 双击完成多边形 ──
    m.on("dblclick", (e: MapMouseEvent) => {
      if (drawRef.current.mode === "polygon") {
        e.preventDefault();
        actionsRef.current.finishPolygon();
      }
    });

    // ── 移动预览（节流到帧级别由 maplibre 事件本身保证） ──
    m.on("mousemove", (e: MapMouseEvent) => {
      const d = drawRef.current;
      const a = actionsRef.current;
      if (d.mode === "circle" && d.circleCenter && d.circleRadiusM == null) {
        const liveRadius =
          haversineKm(d.circleCenter[1], d.circleCenter[0], e.lngLat.lat, e.lngLat.lng) * 1000;
        setRadiusPreview(liveRadius);
        const mm = mapInstanceRef.current;
        if (mm) {
          const features: Feature[] = [
            {
              type: "Feature",
              geometry: { type: "Point", coordinates: [d.circleCenter[0], d.circleCenter[1]] },
              properties: { role: "vertex", idx: 0 }
            }
          ];
          if (liveRadius > 0) {
            features.push({
              type: "Feature",
              geometry: {
                type: "Polygon",
                coordinates: [circleToRing(d.circleCenter[0], d.circleCenter[1], liveRadius)]
              },
              properties: { role: "fill" }
            });
          }
          setSourceData(mm, "fence-draft", features);
        }
        return;
      }
      if (d.mode === "edit" && d.dragIndex != null) {
        d.editRing = d.editRing.map((p, i) =>
          i === d.dragIndex ? ([e.lngLat.lng, e.lngLat.lat] as LngLat) : p
        );
        a.syncEdit();
      }
    });

    // ── 顶点 / 中点拖拽 ──
    const beginDrag = (idx: number) => {
      drawRef.current.dragIndex = idx;
      setDragging(true);
      m.dragPan.disable();
    };
    m.on("mousedown", "edit-vertices", (e) => {
      if (drawRef.current.mode !== "edit") return;
      e.preventDefault();
      const idx = Number(e.features?.[0]?.properties?.idx);
      if (Number.isFinite(idx)) beginDrag(idx);
    });
    m.on("mousedown", "edit-midpoints", (e) => {
      const d = drawRef.current;
      if (d.mode !== "edit") return;
      e.preventDefault();
      const idx = Number(e.features?.[0]?.properties?.idx);
      if (!Number.isFinite(idx)) return;
      const a = d.editRing[idx];
      const b = d.editRing[(idx + 1) % d.editRing.length];
      const mid: LngLat = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
      d.editRing = [...d.editRing.slice(0, idx + 1), mid, ...d.editRing.slice(idx + 1)];
      setVertexCount(d.editRing.length);
      actionsRef.current.syncEdit();
      beginDrag(idx + 1);
    });
    m.on("mouseup", () => {
      if (drawRef.current.dragIndex != null) {
        drawRef.current.dragIndex = null;
        setDragging(false);
        m.dragPan.enable();
      }
    });

    // ── 右键删除顶点 ──
    m.on("contextmenu", "edit-vertices", (e) => {
      const d = drawRef.current;
      if (d.mode !== "edit") return;
      e.preventDefault();
      const idx = Number(e.features?.[0]?.properties?.idx);
      if (!Number.isFinite(idx)) return;
      if (d.editRing.length <= 3) {
        toast.error("多边形至少保留 3 个顶点");
        return;
      }
      d.editRing = d.editRing.filter((_, i) => i !== idx);
      setVertexCount(d.editRing.length);
      actionsRef.current.syncEdit();
    });

    // ── hover 指针 ──
    for (const layer of ["fences-fill", "edit-vertices", "edit-midpoints"]) {
      m.on("mouseenter", layer, () => {
        if (drawRef.current.mode === "idle" || drawRef.current.mode === "edit") {
          m.getCanvas().style.cursor = "pointer";
        }
      });
      m.on("mouseleave", layer, () => {
        m.getCanvas().style.cursor = "";
      });
    }

    setMap(m);
  }, []);

  // ── 键盘 ──
  useEffect(() => {
    if (drawMode === "idle") return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        resetDraw();
        return;
      }
      if (drawMode === "polygon") {
        if (e.key === "Enter") {
          e.preventDefault();
          finishPolygon();
        } else if (e.key === "Backspace") {
          e.preventDefault();
          const d = drawRef.current;
          d.vertices = d.vertices.slice(0, -1);
          setVertexCount(d.vertices.length);
          syncDraft();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drawMode, finishPolygon, resetDraw, syncDraft]);

  // ── 画布外松开鼠标也要结束顶点拖拽 ──
  useEffect(() => {
    if (!dragging) return;
    const onUp = () => {
      if (drawRef.current.dragIndex != null) {
        drawRef.current.dragIndex = null;
        setDragging(false);
        mapInstanceRef.current?.dragPan.enable();
      }
    };
    window.addEventListener("mouseup", onUp);
    return () => window.removeEventListener("mouseup", onUp);
  }, [dragging]);

  // ── 既有围栏写入地图 ──
  useEffect(() => {
    if (!map) return;
    const editingId = drawMode === "edit" ? drawRef.current.editFenceId : null;
    const features: Feature[] = [];
    for (const f of fences) {
      if (f.id === editingId) continue;
      const props = {
        id: f.id,
        mode: f.mode,
        enabled: f.enabled ? 1 : 0,
        selected: f.id === selectedId ? 1 : 0
      };
      if (f.fence) {
        features.push({ type: "Feature", geometry: f.fence, properties: props });
      } else if (f.centerLat != null && f.centerLng != null && f.radiusM != null) {
        features.push({
          type: "Feature",
          geometry: { type: "Polygon", coordinates: [circleToRing(f.centerLng, f.centerLat, f.radiusM)] },
          properties: props
        });
      }
    }
    setSourceData(map, "fences", features);
  }, [map, fences, selectedId, drawMode]);

  const locateFence = useCallback(
    (fence: GeoFenceEntry) => {
      setSelectedId(fence.id);
      const bounds = coordsBounds(fenceCoords(fence));
      if (bounds && map) map.fitBounds(bounds, { padding: 80, duration: 550, maxZoom: 11 });
    },
    [map]
  );

  const handleDelete = useCallback(
    (fence: GeoFenceEntry) => {
      if (!confirm(`确认删除围栏「${fence.name}」？删除后立即停止匹配。`)) return;
      deleteMutation.mutate(fence.id);
    },
    [deleteMutation]
  );

  const handleSave = useCallback(() => {
    const payload = buildPayload();
    if (!payload) return;
    saveMutation.mutate(payload);
  }, [buildPayload, saveMutation]);

  const handlePreview = useCallback(() => {
    const payload = buildPayload();
    if (!payload) return;
    previewMutation.mutate({ ...payload, windowDays: Number(previewDays) });
  }, [buildPayload, previewMutation, previewDays]);

  const activeCount = useMemo(() => fences.filter((f) => f.enabled).length, [fences]);
  const banModeLabel = useCallback(
    (value: string) => banModes.find((mode) => mode.value === value)?.label ?? value,
    [banModes]
  );

  const drawHint = (() => {
    switch (drawMode) {
      case "polygon":
        return `已 ${vertexCount} 点 · 单击加点 · Enter / 双击完成 · Backspace 撤销 · Esc 取消`;
      case "circle":
        return radiusPreview != null
          ? `半径 ${fmtRadius(radiusPreview)} · 再次单击确定 · Esc 取消`
          : "单击地图设置圆心 · Esc 取消";
      case "edit":
        return `${vertexCount} 顶点 · 拖拽调整 · 拖中点插入 · 右键删除 · Esc 放弃`;
      default:
        return null;
    }
  })();

  const editingGeoFence = drawMode === "edit" ? fences.find((f) => f.id === drawRef.current.editFenceId) : null;

  return (
    <div className="space-y-4">
      {/* 顶部 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
            <LandPlot className="size-4" />
          </div>
          <div className="flex flex-col">
            <span className="text-sm font-semibold leading-tight">地理围栏</span>
            <span className="text-[11px] leading-tight text-muted-foreground">
              共 <span className="font-medium text-foreground">{fences.length}</span> 条规则，
              {activeCount} 条生效中 · 拦截 / 白名单 / 观察三种模式
            </span>
          </div>
        </div>
        <Button
          size="sm"
          variant="outline"
          className="h-8 gap-1.5"
          onClick={() => fencesQuery.refetch()}
          disabled={fencesQuery.isFetching}
        >
          <RefreshCw className={cn("size-3.5", fencesQuery.isFetching && "animate-spin")} />
          刷新
        </Button>
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        {/* 地图 + 绘制工具栏 */}
        <BaseMap
          className="h-[480px] xl:h-[560px]"
          onMapReady={handleMapReady}
          drawCursor={drawMode === "polygon" || drawMode === "circle"}
          grabCursor={dragging}
        >
          <div className="absolute top-3 left-3 z-10 flex flex-wrap items-center gap-1.5">
            {drawMode === "idle" ? (
              <div className="flex items-center gap-0.5 rounded-lg border border-border bg-card/95 p-0.5 backdrop-blur-sm">
                <button
                  type="button"
                  onClick={startPolygon}
                  className="flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                >
                  <Hexagon className="size-3.5" />
                  绘制多边形
                </button>
                <button
                  type="button"
                  onClick={startCircle}
                  className="flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                >
                  <CircleDashed className="size-3.5" />
                  绘制圆形
                </button>
              </div>
            ) : (
              <div className="flex items-center gap-1.5 rounded-lg border border-indigo-500/40 bg-card/95 px-2.5 py-1.5 backdrop-blur-sm">
                <Spline className="size-3.5 text-indigo-500" />
                <span className="text-[11px] font-medium text-foreground">{drawHint}</span>
                {drawMode === "polygon" && (
                  <Button size="sm" className="h-6 px-2 text-[11px]" onClick={finishPolygon} disabled={vertexCount < 3}>
                    完成
                  </Button>
                )}
                {drawMode === "edit" && (
                  <Button
                    size="sm"
                    className="h-6 px-2 text-[11px]"
                    onClick={saveEditGeometry}
                    disabled={geometrySaveMutation.isPending}
                  >
                    {geometrySaveMutation.isPending ? (
                      <Loader2 className="size-3 animate-spin" />
                    ) : (
                      "保存几何"
                    )}
                  </Button>
                )}
                <Button size="sm" variant="ghost" className="h-6 px-2 text-[11px]" onClick={resetDraw}>
                  取消
                </Button>
              </div>
            )}
          </div>

          {/* 图例 */}
          <div className="absolute bottom-3 right-3 z-10 flex items-center gap-2.5 rounded-lg border border-border bg-card/95 px-2.5 py-1.5 backdrop-blur-sm">
            {ALL_FENCE_MODES.map((mode) => (
              <span key={mode} className="flex items-center gap-1 text-[10.5px] text-muted-foreground">
                <span className="size-2 rounded-sm" style={{ backgroundColor: FENCE_MODE_META[mode].color }} />
                {FENCE_MODE_META[mode].label}
              </span>
            ))}
          </div>

          {editingGeoFence && (
            <div className="absolute inset-x-0 top-14 z-10 flex justify-center">
              <span className="rounded-full border border-indigo-500/40 bg-indigo-500/10 px-3 py-1 text-[11px] font-medium text-indigo-600 dark:text-indigo-400">
                正在编辑「{editingGeoFence.name}」的几何
              </span>
            </div>
          )}
        </BaseMap>

        {/* 围栏列表 */}
        <div className="flex max-h-[560px] min-h-[320px] flex-col rounded-xl border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <span className="text-xs font-semibold">围栏规则</span>
            <span className="text-[10.5px] text-muted-foreground">在地图上绘制以新建</span>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {fencesQuery.isLoading ? (
              <p className="flex items-center justify-center gap-1.5 px-2 py-10 text-xs text-muted-foreground">
                <Loader2 className="size-3.5 animate-spin" />
                加载中...
              </p>
            ) : fences.length === 0 ? (
              <div className="px-3 py-8 text-center">
                <p className="text-xs font-medium text-foreground">暂无地理围栏</p>
                <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                  点击地图左上角「绘制多边形 / 圆形」框选区域，
                  即可创建拦截、白名单或观察规则
                </p>
                <Button size="sm" className="mt-3 h-7 gap-1 text-[11px]" onClick={startPolygon}>
                  <Plus className="size-3" />
                  开始绘制
                </Button>
              </div>
            ) : (
              <ul className="space-y-1.5">
                {fences.map((f) => {
                  const meta = FENCE_MODE_META[f.mode] ?? FENCE_MODE_META.review;
                  const selected = f.id === selectedId;
                  return (
                    <li
                      key={f.id}
                      className={cn(
                        "rounded-lg border p-2.5 transition-colors",
                        selected
                          ? "border-primary/40 bg-primary/5"
                          : "border-border/70 hover:border-border hover:bg-muted/40"
                      )}
                    >
                      <button type="button" className="block w-full text-left" onClick={() => locateFence(f)}>
                        <span className="flex items-center justify-between gap-2">
                          <span className="flex min-w-0 items-center gap-1.5">
                            <span className="size-2 shrink-0 rounded-sm" style={{ backgroundColor: meta.color }} />
                            <span className="truncate text-xs font-semibold">{f.name}</span>
                            <Badge variant={meta.badgeVariant} size="sm">
                              {meta.label}
                            </Badge>
                          </span>
                        </span>
                        <span className="mt-1 block truncate text-[10.5px] text-muted-foreground">
                          {appName(f.appId)} · {fenceGeometrySummary(f)} · 命中 {fmtCount(f.matchCount)}
                          {f.banMode ? ` · ${banModeLabel(f.banMode)}` : ""}
                        </span>
                        <span className="mt-0.5 block text-[10.5px] text-muted-foreground/80">
                          {f.expiresAt ? `失效 ${fmtDateTime(f.expiresAt)}` : "永久有效"}
                        </span>
                      </button>
                      <div className="mt-2 flex items-center justify-between gap-1">
                        <div className="flex items-center gap-0.5">
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6 text-muted-foreground hover:text-foreground"
                            title="定位"
                            onClick={() => locateFence(f)}
                          >
                            <LocateFixed className="size-3.5" />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6 text-muted-foreground hover:text-foreground"
                            title="编辑信息"
                            onClick={() => openEditDialog(f)}
                          >
                            <Pencil className="size-3.5" />
                          </Button>
                          {f.fence && (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="size-6 text-muted-foreground hover:text-foreground"
                              title="编辑顶点"
                              onClick={() => startEditGeometry(f)}
                            >
                              <Spline className="size-3.5" />
                            </Button>
                          )}
                          <Button
                            size="icon"
                            variant="ghost"
                            className="size-6 text-muted-foreground hover:text-destructive"
                            title="删除"
                            onClick={() => handleDelete(f)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        </div>
                        <Switch
                          checked={f.enabled}
                          onCheckedChange={(v) => toggleMutation.mutate({ id: f.id, enabled: v })}
                          disabled={toggleMutation.isPending}
                          className="scale-90"
                          aria-label={f.enabled ? "禁用围栏" : "启用围栏"}
                        />
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      </div>

      {/* 创建 / 编辑对话框 */}
      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          setDialogOpen(open);
          if (!open && !editingFence) resetDraw();
        }}
      >
        <DialogContent className="max-w-lg gap-0 overflow-hidden p-0">
          <DialogHeader className="flex flex-row items-start gap-3 space-y-0 border-b border-border bg-muted/30 px-5 py-4">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
              <LandPlot className="size-4" />
            </div>
            <div className="flex-1 space-y-0.5">
              <DialogTitle className="text-sm font-semibold">
                {editingFence ? `编辑围栏「${editingFence.name}」` : "新建地理围栏"}
              </DialogTitle>
              <DialogDescription className="text-xs leading-relaxed">
                {editingFence
                  ? fenceGeometrySummary(editingFence)
                  : pendingGeometry?.kind === "circle"
                    ? `圆形围栏 · 半径 ${fmtRadius(pendingGeometry.radiusM)}`
                    : `多边形围栏 · ${pendingGeometry?.ring.length ?? 0} 顶点`}
                ，保存后实时生效
              </DialogDescription>
            </div>
          </DialogHeader>

          <div className="max-h-[calc(100vh-240px)] overflow-y-auto px-5 py-4">
            <div className="space-y-3.5">
              <div className="grid grid-cols-2 gap-3">
                <FormField label="围栏名称" required>
                  <Input
                    className="h-9 text-sm"
                    value={fName}
                    onChange={(e) => setFName(e.target.value)}
                    placeholder="如：高危地区拦截"
                  />
                </FormField>
                <FormField label="适用范围">
                  <Select value={fApp} onValueChange={setFApp}>
                    <SelectTrigger className="h-9 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="max-h-64">
                      <SelectItem value="platform" className="text-sm">
                        平台级（所有应用）
                      </SelectItem>
                      {apps.map((a) => (
                        <SelectItem key={a.id} value={String(a.id)} className="text-sm">
                          {a.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </FormField>
              </div>

              <FormField label="围栏模式" required>
                <div className="grid grid-cols-3 gap-1.5">
                  {ALL_FENCE_MODES.map((mode) => {
                    const meta = FENCE_MODE_META[mode];
                    const active = fMode === mode;
                    return (
                      <button
                        key={mode}
                        type="button"
                        onClick={() => setFMode(mode)}
                        className={cn(
                          "rounded-lg border px-2 py-1.5 text-left transition-colors",
                          active ? "border-primary/50 bg-primary/5" : "border-border hover:bg-muted/50"
                        )}
                      >
                        <span className="flex items-center gap-1.5 text-[11.5px] font-medium">
                          <span className="size-2 rounded-sm" style={{ backgroundColor: meta.color }} />
                          {meta.label}
                        </span>
                      </button>
                    );
                  })}
                </div>
                <p className="rounded-md border border-border/60 bg-muted/40 px-2.5 py-1.5 text-[11px] leading-relaxed text-muted-foreground">
                  {FENCE_MODE_META[fMode].description}
                </p>
              </FormField>

              {editingFence && !editingFence.fence && (
                <div className="grid grid-cols-3 gap-3">
                  <FormField label="圆心纬度" required>
                    <Input
                      className="h-9 font-mono text-sm"
                      value={fCircleLat}
                      onChange={(e) => setFCircleLat(e.target.value)}
                      placeholder="-90 ~ 90"
                    />
                  </FormField>
                  <FormField label="圆心经度" required>
                    <Input
                      className="h-9 font-mono text-sm"
                      value={fCircleLng}
                      onChange={(e) => setFCircleLng(e.target.value)}
                      placeholder="-180 ~ 180"
                    />
                  </FormField>
                  <FormField label="半径（米）" required>
                    <Input
                      className="h-9 font-mono text-sm"
                      value={fCircleRadius}
                      onChange={(e) => setFCircleRadius(e.target.value)}
                      placeholder="如 50000"
                    />
                  </FormField>
                </div>
              )}

              <div className="grid grid-cols-2 gap-3">
                <FormField label="响应模式" hint="命中后的处置方式">
                  <Select value={fBanMode} onValueChange={setFBanMode}>
                    <SelectTrigger className="h-9 text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="max-h-72">
                      <SelectItem value="__default__" className="text-sm">
                        平台默认 · {banModeLabel(defaultBanMode)}
                      </SelectItem>
                      {banModes.map((mode) => (
                        <SelectItem key={mode.value} value={mode.value} className="text-sm">
                          {mode.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </FormField>
                <FormField label="过期时间" hint="留空 = 永久">
                  <Input
                    type="datetime-local"
                    className="h-9 text-sm"
                    value={fExpires}
                    onChange={(e) => setFExpires(e.target.value)}
                  />
                </FormField>
              </div>

              <FormField label="备注原因">
                <Input
                  className="h-9 text-sm"
                  value={fReason}
                  onChange={(e) => setFReason(e.target.value)}
                  placeholder="可选，将写入审计日志并用于拦截提示"
                />
              </FormField>

              <label className="flex cursor-pointer items-center justify-between rounded-lg border border-border bg-muted/40 px-3 py-2.5">
                <div className="flex flex-col gap-0.5">
                  <span className="text-[12px] font-medium">立即启用</span>
                  <span className="text-[10.5px] text-muted-foreground">关闭后保留规则但不参与匹配</span>
                </div>
                <Switch checked={fEnabled} onCheckedChange={setFEnabled} />
              </label>

              {/* 影响面回测 */}
              <div className="space-y-2 rounded-lg border border-border bg-muted/30 p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-[12px] font-medium">影响面回测</span>
                  <div className="flex items-center gap-1.5">
                    <Select value={previewDays} onValueChange={setPreviewDays}>
                      <SelectTrigger className="h-7 w-[88px] text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {["7", "14", "30"].map((d) => (
                          <SelectItem key={d} value={d} className="text-xs">
                            近 {d} 天
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 px-2.5 text-xs"
                      onClick={handlePreview}
                      disabled={previewMutation.isPending}
                    >
                      {previewMutation.isPending ? <Loader2 className="size-3 animate-spin" /> : "回测"}
                    </Button>
                  </div>
                </div>
                {previewResult ? (
                  <div className="grid grid-cols-3 gap-2">
                    {[
                      { label: "命中登录", value: previewResult.loginMatches },
                      { label: "命中拦截", value: previewResult.blockMatches },
                      { label: "受影响用户", value: previewResult.uniqueUsers }
                    ].map((item) => (
                      <div key={item.label} className="rounded-md border border-border bg-background px-2 py-1.5">
                        <p className="text-[10px] text-muted-foreground">{item.label}</p>
                        <p className="font-mono text-sm font-semibold tabular-nums">{fmtCount(item.value)}</p>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-[10.5px] leading-relaxed text-muted-foreground">
                    用过去窗口内的真实登录 / 拦截数据评估该围栏的影响范围，建议上线 deny 围栏前先回测
                  </p>
                )}
              </div>
            </div>
          </div>

          <DialogFooter className="border-t border-border bg-muted/30 px-5 py-3">
            <Button variant="ghost" size="sm" onClick={() => setDialogOpen(false)}>
              取消
            </Button>
            <Button size="sm" onClick={handleSave} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? (
                <>
                  <Loader2 className="mr-1 size-3 animate-spin" />
                  保存中...
                </>
              ) : editingFence ? (
                "保存更新"
              ) : (
                "创建围栏"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
