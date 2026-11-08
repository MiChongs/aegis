"use client";

/**
 * 风控关系图画布（AntV G6 5）。
 *
 * 布局：辐射（默认，中心是主体，一跳、两跳各占一圈）/ 力导向 / 同心圆；
 * 交互：拖动平移、滚轮缩放、悬停高亮直接相连的点并淡化其余、单击查看说明、双击以它为中心展开、
 * 点「N 个 IP」展开 IP 历史；工具栏（适应窗口、缩放、布局、全屏、导出图片）与图例筛选是 React 叠层。
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ComponentType } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import { useTheme } from "next-themes";
import type { Graph, GraphData, IElementEvent, NodeData, EdgeData } from "@antv/g6";
import {
  AppWindow, AtSign, BadgeCheck, Download, Globe, KeyRound, Layers, Mail, Maximize2, Minimize2, Network, Phone,
  ScanSearch, Smartphone, User, Wallet, ZoomIn, ZoomOut, type LucideProps,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import type { GraphNode, Subgraph } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

import {
  KIND_COLOR, KIND_LABEL, KIND_ORDER, WEAK_COLOR, buildView, describe, describeEdge, peopleDegree, providerName,
  type ViewEdge, type ViewKind, type ViewNode,
} from "./graph-model";

export type LayoutKind = "radial" | "force" | "concentric";

const LAYOUTS: ReadonlyArray<{ value: LayoutKind; label: string }> = [
  { value: "radial", label: "辐射" },
  { value: "force", label: "力导向" },
  { value: "concentric", label: "同心圆" },
];

const ICONS: Record<ViewKind, ComponentType<LucideProps>> = {
  user: User, account: AtSign, device: Smartphone, ip: Globe, subnet: Network, phone: Phone, email: Mail,
  identity: BadgeCheck, payer: Wallet, passkey: KeyRound, client: AppWindow, ip_group: Layers,
};

const DANGER = "#ef4444";
const EDGE_COLOR: Record<string, string> = {
  invited: "#f59e0b", uses_device: KIND_COLOR.device, registered_on: KIND_COLOR.device, uses_ip: KIND_COLOR.ip,
  registered_from: KIND_COLOR.ip, bound_identity: KIND_COLOR.identity, has_phone: KIND_COLOR.phone, has_email: KIND_COLOR.email,
  paid_with: KIND_COLOR.payer, account_of: KIND_COLOR.account, has_passkey: KIND_COLOR.passkey, authorized: KIND_COLOR.client,
};

/** lucide 图标转成白色 SVG 图片（G6 的节点图标用图片）；只在浏览器里生成一次 */
let iconCache: Record<ViewKind, string> | null = null;
function lucideIcons(): Record<ViewKind, string> {
  if (iconCache) return iconCache;
  const host = document.createElement("div");
  const root = createRoot(host);
  flushSync(() => {
    root.render(
      <div>
        {KIND_ORDER.map((k) => {
          const Icon = ICONS[k];
          return <Icon key={k} data-kind={k} color="#ffffff" size={48} strokeWidth={2.2} />;
        })}
      </div>,
    );
  });
  const out = {} as Record<ViewKind, string>;
  host.querySelectorAll("svg").forEach((svg) => {
    const k = svg.getAttribute("data-kind") as ViewKind;
    svg.setAttribute("xmlns", "http://www.w3.org/2000/svg");
    out[k] = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg.outerHTML)}`;
  });
  root.unmount();
  iconCache = out;
  return out;
}

interface NodeMeta extends Record<string, unknown> {
  view: ViewNode;
}
interface EdgeMeta extends Record<string, unknown> {
  view: ViewEdge;
}

function sizeOf(v: ViewNode) {
  if (v.center) return 62;
  if (v.weak) return 22;
  switch (v.kind) {
    case "user":
      return 40;
    case "device":
      return 34;
    case "ip_group":
      return 38;
    case "ip":
      return 26;
    default:
      return 28;
  }
}

function layoutOf(kind: LayoutKind, center: string, count: number) {
  switch (kind) {
    case "force":
      return {
        type: "d3-force",
        link: { distance: (e: EdgeData) => ((e.data as EdgeMeta | undefined)?.view.weak ? 170 : 120), strength: 0.5 },
        manyBody: { strength: count > 150 ? -260 : -420 },
        collide: { radius: 40, strength: 0.9 },
      };
    case "concentric":
      return { type: "concentric", sortBy: "rank", preventOverlap: true, nodeSize: 56, nodeSpacing: 14, maxLevelDiff: 1 };
    default:
      return {
        type: "radial", focusNode: center, unitRadius: count > 120 ? 190 : 150, linkDistance: 170,
        preventOverlap: true, nodeSize: 58, nodeSpacing: 12, strictRadial: false, maxPreventOverlapIteration: 300,
      };
  }
}

export interface RiskGraphProps {
  graph: Subgraph;
  height?: number;
  selectedId?: string | null;
  /** 单击：选中节点（空白处单击传 null） */
  onSelect?: (node: GraphNode | null) => void;
  /** 双击：以它为中心展开 */
  onExpand?: (node: GraphNode) => void;
  defaultLayout?: LayoutKind;
  className?: string;
}

interface Hover {
  x: number;
  y: number;
  /** 画布宽度（事件发生时量的，提示框靠右时往左收） */
  w: number;
  title: string;
  subtitle?: string;
  lines: string[];
}

export function RiskGraph({ graph, height = 560, selectedId, onSelect, onExpand, defaultLayout = "radial", className }: RiskGraphProps) {
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";
  const wrapRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
  const g6 = useRef<Graph | null>(null);
  const [ready, setReady] = useState(0);
  const [layout, setLayout] = useState<LayoutKind>(defaultLayout);
  const [hidden, setHidden] = useState<Set<ViewKind>>(() => new Set());
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [hover, setHover] = useState<Hover | null>(null);
  const [full, setFull] = useState(false);

  const view = useMemo(() => buildView(graph, hidden, expanded), [graph, hidden, expanded]);
  const degree = useMemo(() => peopleDegree(graph), [graph]);
  const counts = useMemo(() => {
    const m = new Map<ViewKind, number>();
    for (const n of graph.nodes) m.set(n.kind, (m.get(n.kind) ?? 0) + 1);
    return m;
  }, [graph]);

  // 回调与查找表放进 ref：G6 实例只建一次，事件里读最新值
  const live = useRef({ view, onSelect, onExpand, degree, nodes: new Map<string, ViewNode>(), edges: new Map<string, ViewEdge>() });
  useEffect(() => {
    live.current = {
      view, onSelect, onExpand, degree,
      nodes: new Map(view.nodes.map((n) => [n.id, n])),
      edges: new Map(view.edges.map((e) => [e.id, e])),
    };
  }, [view, onSelect, onExpand, degree]);

  const palette = useMemo(
    () => ({ fg: dark ? "#e5e7eb" : "#111827", muted: dark ? "#94a3b8" : "#6b7280", surface: dark ? "#0b0c10" : "#ffffff" }),
    [dark],
  );

  // 建图（主题变化时重建）
  useEffect(() => {
    let disposed = false;
    let instance: Graph | null = null;
    (async () => {
      const { Graph } = await import("@antv/g6");
      const host = canvasRef.current;
      if (disposed || !host) return;
      const icons = lucideIcons();
      const { fg, muted, surface } = palette;
      instance = new Graph({
        container: host,
        autoResize: true,
        background: "transparent",
        theme: dark ? "dark" : "light",
        zoomRange: [0.15, 4],
        animation: { duration: 400 },
        node: {
          type: "circle",
          style: (d: NodeData) => {
            const v = (d.data as NodeMeta).view;
            const color = v.weak ? WEAK_COLOR : KIND_COLOR[v.kind];
            const size = sizeOf(v);
            return {
              size,
              fill: color,
              fillOpacity: v.weak ? 0.55 : 1,
              stroke: v.banned ? DANGER : v.weak ? WEAK_COLOR : surface,
              lineWidth: v.banned ? 3 : v.center ? 3 : 1.5,
              lineDash: v.weak ? [3, 2] : 0,
              iconSrc: icons[v.kind],
              iconWidth: size * 0.5,
              iconHeight: size * 0.5,
              labelText: v.label,
              labelFill: v.banned ? DANGER : v.weak ? muted : fg,
              labelFontSize: v.center ? 13 : 11,
              labelFontWeight: v.center ? 600 : 400,
              labelPlacement: "bottom",
              labelMaxWidth: 150,
              labelWordWrap: false,
              labelBackground: true,
              labelBackgroundFill: surface,
              labelBackgroundOpacity: 0.82,
              labelBackgroundRadius: 4,
              labelPadding: [1, 4],
              halo: v.center,
              haloStroke: color,
              haloLineWidth: 16,
              haloStrokeOpacity: 0.2,
              badges: v.badges.map((text, i) => ({
                text,
                placement: i === 0 ? ("right-top" as const) : ("left-top" as const),
                fill: "#ffffff",
                backgroundFill: text.includes("封禁") ? DANGER : text.startsWith("共享") || text === "机房" ? "#64748b" : "#f59e0b",
              })),
              badgeFontSize: 9,
              badgePadding: [1, 4],
            };
          },
          state: {
            active: { halo: true, haloLineWidth: 10, haloStrokeOpacity: 0.28 },
            inactive: { opacity: 0.16 },
            selected: { halo: true, haloLineWidth: 18, haloStrokeOpacity: 0.38, stroke: fg, lineWidth: 3 },
          },
        },
        edge: {
          type: (d: EdgeData) => ((d.data as EdgeMeta).view.kind === "invited" ? "quadratic" : "line"),
          style: (d: EdgeData) => {
            const e = (d.data as EdgeMeta).view;
            const color = e.weak ? WEAK_COLOR : (EDGE_COLOR[e.kind] ?? (dark ? "#475569" : "#cbd5e1"));
            const arrow = e.kind === "invited" || e.kind === "paid_with";
            return {
              stroke: color,
              lineWidth: e.weak ? 1 : Math.min(3.5, 1 + Math.log10(Math.max(1, e.count)) * 0.9),
              strokeOpacity: e.weak ? 0.45 : 0.6,
              lineDash: e.weak || e.kind === "account_of" ? [4, 3] : 0,
              endArrow: arrow,
              endArrowSize: 8,
              labelText: e.kind === "invited" ? "邀请" : undefined,
              labelFill: "#d97706",
              labelFontSize: 10,
              labelBackground: true,
              labelBackgroundFill: surface,
              labelBackgroundOpacity: 0.9,
              labelPadding: [0, 3],
            };
          },
          state: {
            active: { strokeOpacity: 1, lineWidth: 2.5 },
            inactive: { strokeOpacity: 0.06 },
          },
        },
        behaviors: [
          "drag-canvas",
          "zoom-canvas",
          "drag-element",
          { type: "hover-activate", degree: 1, state: "active", inactiveState: "inactive" },
        ],
        plugins: [
          {
            type: "minimap",
            key: "minimap",
            size: [150, 96],
            position: "right-bottom",
            padding: 8,
            containerStyle: {
              border: `1px solid ${dark ? "#27272a" : "#e5e7eb"}`,
              borderRadius: "10px",
              background: surface,
              margin: "10px",
              overflow: "hidden",
            },
            maskStyle: { border: `1.5px solid ${KIND_COLOR.user}`, background: "rgba(99,102,241,0.08)", borderRadius: "4px" },
          },
        ],
      });

      const pointOf = (e: IElementEvent) => {
        const c = (e as unknown as { client?: { x: number; y: number } }).client;
        const rect = host.getBoundingClientRect();
        return { x: (c?.x ?? 0) - rect.left, y: (c?.y ?? 0) - rect.top, w: rect.width };
      };
      instance.on("node:pointerenter", (e: IElementEvent) => {
        const v = live.current.nodes.get(e.target.id);
        if (!v) return;
        const { x, y, w } = pointOf(e);
        if (v.kind === "ip_group") {
          const ms = v.members ?? [];
          const isp = new Map<string, number>();
          for (const m of ms) {
            const k = (m.props?.isp as string) || "未知运营商";
            isp.set(k, (isp.get(k) ?? 0) + 1);
          }
          setHover({
            x, y, w, title: v.label, subtitle: "只有这一位用户用过的 IP，点击展开",
            lines: [[...isp].sort((a, b) => b[1] - a[1]).slice(0, 3).map(([k, n]) => `${k} ${n} 个`).join("，")],
          });
          return;
        }
        if (!v.node) return;
        const d = describe(v.node, live.current.degree.get(v.id) ?? 0);
        setHover({ x, y, w, title: d.title, subtitle: d.subtitle, lines: d.facts.slice(0, 4).map((f) => `${f.label}：${f.value}`) });
      });
      instance.on("edge:pointerenter", (e: IElementEvent) => {
        const ed = live.current.edges.get(e.target.id);
        if (!ed) return;
        const { x, y, w } = pointOf(e);
        const name = (id: string) => live.current.nodes.get(id)?.label ?? id;
        setHover({ x, y, w, title: describeEdge(ed, name(ed.source), name(ed.target)), lines: [] });
      });
      const leave = () => setHover(null);
      instance.on("node:pointerleave", leave);
      instance.on("edge:pointerleave", leave);
      instance.on("canvas:pointerleave", leave);
      instance.on("node:click", (e: IElementEvent) => {
        const v = live.current.nodes.get(e.target.id);
        if (!v) return;
        if (v.kind === "ip_group") {
          const owner = v.id.slice(4);
          setExpanded((prev) => new Set(prev).add(owner));
          return;
        }
        live.current.onSelect?.(v.node ?? null);
      });
      instance.on("node:dblclick", (e: IElementEvent) => {
        const v = live.current.nodes.get(e.target.id);
        if (v?.node && !v.center) live.current.onExpand?.(v.node);
      });
      instance.on("canvas:click", () => live.current.onSelect?.(null));

      if (disposed) {
        instance.destroy();
        return;
      }
      g6.current = instance;
      setReady((n) => n + 1);
    })();
    return () => {
      disposed = true;
      g6.current = null;
      instance?.destroy();
    };
  }, [dark, palette]);

  // 数据或布局变化：换数据重新布局
  useEffect(() => {
    const instance = g6.current;
    if (!instance || !ready) return;
    const rank = (v: ViewNode) => (v.center ? 100 : 100 - v.depth * 20 - (v.kind === "user" ? 0 : 5));
    const data: GraphData = {
      nodes: view.nodes.map((v) => ({ id: v.id, data: { view: v, rank: rank(v) } satisfies NodeMeta & { rank: number } })),
      edges: view.edges.map((e) => ({ id: e.id, source: e.source, target: e.target, data: { view: e } satisfies EdgeMeta })),
    };
    instance.setLayout(layoutOf(layout, graph.center, view.nodes.length) as Parameters<Graph["setLayout"]>[0]);
    instance.setData(data);
    let cancelled = false;
    instance.render().then(() => {
      if (!cancelled) void instance.fitView({ when: "always", direction: "both" }, false);
    });
    return () => {
      cancelled = true;
    };
  }, [view, layout, ready, graph.center]);

  // 选中态
  useEffect(() => {
    const instance = g6.current;
    if (!instance || !ready) return;
    const states: Record<string, string[]> = {};
    for (const n of view.nodes) states[n.id] = n.id === selectedId ? ["selected"] : [];
    try {
      void instance.setElementState(states, false);
    } catch {
      // 渲染中途切换数据时元素可能还不在：下一次选中再设
    }
  }, [selectedId, view, ready]);

  useEffect(() => {
    const onChange = () => setFull(document.fullscreenElement === wrapRef.current);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);

  const toggleKind = useCallback((k: ViewKind) => {
    setHidden((prev) => {
      const next = new Set(prev);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      return next;
    });
  }, []);

  const exportPng = async () => {
    const instance = g6.current;
    if (!instance) return;
    const url = await instance.toDataURL({ mode: "overall", type: "image/png" });
    const a = document.createElement("a");
    a.href = url;
    a.download = "关系图.png";
    a.click();
  };

  const legend = KIND_ORDER.filter((k) => k === "ip_group" ? view.groups > 0 : counts.has(k));
  const groupedOwners = view.nodes.filter((n) => n.kind === "ip_group").length;

  return (
    <div ref={wrapRef} className={cn("relative overflow-hidden rounded-xl bg-background", full && "rounded-none", className)} style={{ height: full ? "100vh" : height }}>
      <div ref={canvasRef} className="absolute inset-0" />

      {/* 图例：点一下隐藏 / 显示这一类 */}
      <div className="pointer-events-none absolute top-3 left-3 flex max-w-[70%] flex-wrap gap-1.5">
        {legend.map((k) => {
          const off = hidden.has(k);
          const n = k === "ip_group" ? groupedOwners : counts.get(k) ?? 0;
          return (
            <button
              key={k}
              type="button"
              onClick={() => toggleKind(k)}
              className={cn(
                "pointer-events-auto inline-flex items-center gap-1.5 rounded-full border bg-background/85 px-2.5 py-1 text-xs backdrop-blur transition-colors hover:bg-muted",
                off && "text-muted-foreground line-through opacity-60",
              )}
            >
              <span className="size-2.5 rounded-full" style={{ background: KIND_COLOR[k] }} />
              {KIND_LABEL[k]}
              <span className="text-muted-foreground tabular-nums">{n}</span>
            </button>
          );
        })}
      </div>

      {/* 工具栏 */}
      <TooltipProvider delayDuration={300}>
      <div className="absolute top-3 right-3 flex items-center gap-1 rounded-lg border bg-background/85 p-1 backdrop-blur">
        <Select value={layout} onValueChange={(v) => setLayout(v as LayoutKind)}>
          <SelectTrigger size="sm" className="h-7 w-[92px] border-0 shadow-none"><SelectValue /></SelectTrigger>
          <SelectContent>
            {LAYOUTS.map((l) => <SelectItem key={l.value} value={l.value}>{l.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <ToolButton label="适应窗口" onClick={() => void g6.current?.fitView()}><ScanSearch className="size-4" /></ToolButton>
        <ToolButton label="放大" onClick={() => void g6.current?.zoomBy(1.25)}><ZoomIn className="size-4" /></ToolButton>
        <ToolButton label="缩小" onClick={() => void g6.current?.zoomBy(0.8)}><ZoomOut className="size-4" /></ToolButton>
        <ToolButton label="导出图片" onClick={() => void exportPng()}><Download className="size-4" /></ToolButton>
        <ToolButton
          label={full ? "退出全屏" : "全屏"}
          onClick={() => (full ? void document.exitFullscreen() : void wrapRef.current?.requestFullscreen())}
        >
          {full ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
        </ToolButton>
      </div>
      </TooltipProvider>

      {expanded.size > 0 ? (
        <div className="absolute bottom-3 left-3">
          <Button size="sm" variant="outline" className="h-7 bg-background/85 backdrop-blur" onClick={() => setExpanded(new Set())}>
            <Layers className="size-3.5" />
            收起 IP 历史
          </Button>
        </div>
      ) : (
        <p className="pointer-events-none absolute bottom-3 left-3 max-w-[60%] text-xs text-muted-foreground">
          拖动平移，滚轮缩放，悬停看关系，单击看说明，双击以它为中心展开
        </p>
      )}

      {hover ? (
        <div
          className="pointer-events-none absolute z-10 max-w-72 rounded-lg border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md"
          style={{ left: Math.max(8, Math.min(hover.x + 14, hover.w - 300)), top: hover.y + 14 }}
        >
          <div className="font-medium break-all">{hover.title}</div>
          {hover.subtitle ? <div className="mt-0.5 text-muted-foreground">{hover.subtitle}</div> : null}
          {hover.lines.filter(Boolean).length ? (
            <ul className="mt-1.5 space-y-0.5">
              {hover.lines.filter(Boolean).map((l) => <li key={l} className="text-muted-foreground">{l}</li>)}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function ToolButton({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="ghost" className="size-7" onClick={onClick} aria-label={label}>{children}</Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** 第三方账号的显示名（摘要卡片用） */
export function identityLabel(n: GraphNode) {
  return `${providerName(String(n.props?.provider ?? ""))} ${String(n.props?.nickname ?? "")}`.trim();
}
