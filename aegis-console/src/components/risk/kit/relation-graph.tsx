"use client";

import { useEffect, useMemo, useRef } from "react";
import { useTheme } from "next-themes";
import * as echarts from "echarts/core";
import { GraphChart } from "echarts/charts";
import { LegendComponent, TooltipComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";

import type { GraphNode, GraphNodeKind, Subgraph } from "@/lib/api/risk";
import { formatDateTime } from "@/lib/datetime";

import { EDGE_LABEL, NODE_KIND_LABEL } from "./labels";

echarts.use([GraphChart, TooltipComponent, LegendComponent, CanvasRenderer]);

/** 每种点的配色与大小（固定色板，明暗主题都清楚） */
const KIND_STYLE: Record<GraphNodeKind, { color: string; size: number; symbol: string }> = {
  user: { color: "#6366f1", size: 26, symbol: "circle" },
  account: { color: "#8b5cf6", size: 16, symbol: "circle" },
  device: { color: "#0ea5e9", size: 22, symbol: "roundRect" },
  ip: { color: "#10b981", size: 18, symbol: "diamond" },
  subnet: { color: "#14b8a6", size: 14, symbol: "diamond" },
  phone: { color: "#f59e0b", size: 16, symbol: "triangle" },
  email: { color: "#ec4899", size: 16, symbol: "triangle" },
  identity: { color: "#2563eb", size: 18, symbol: "pin" },
  payer: { color: "#d97706", size: 16, symbol: "rect" },
  passkey: { color: "#059669", size: 14, symbol: "rect" },
  client: { color: "#7c3aed", size: 16, symbol: "roundRect" },
};

const KINDS = Object.keys(KIND_STYLE) as GraphNodeKind[];

function cssVar(name: string, fallback: string) {
  if (typeof window === "undefined") return fallback;
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

export function isBanned(n: GraphNode) {
  return n.props?.banned === true || n.props?.status === "banned";
}

function propLines(n: GraphNode): string[] {
  const out: string[] = [];
  const p = n.props ?? {};
  const pick = (k: string, label: string) => {
    const v = p[k];
    if (v !== undefined && v !== null && v !== "" && v !== 0 && v !== false) out.push(`${label}：${String(v)}`);
  };
  pick("uid", "用户 ID");
  pick("name", "名称");
  pick("brand", "品牌");
  pick("model", "型号");
  pick("platform", "平台");
  pick("country", "国家");
  pick("province", "省份");
  pick("city", "城市");
  pick("isp", "运营商");
  pick("asn", "ASN");
  pick("last_action", "最近处置");
  if (p.emulator === true) out.push("模拟器");
  if (p.rooted === true) out.push("已 Root");
  if (isBanned(n)) out.push("已封禁");
  if (typeof p.first_seen === "number" && p.first_seen > 0) out.push(`首次出现：${formatDateTime(new Date((p.first_seen as number) * 1000).toISOString())}`);
  if (typeof p.last_seen === "number" && p.last_seen > 0) out.push(`最近出现：${formatDateTime(new Date((p.last_seen as number) * 1000).toISOString())}`);
  return out;
}

export interface RelationGraphProps {
  graph: Subgraph;
  height?: number;
  onNodeClick?: (node: GraphNode) => void;
  /** 高亮的点（如当前选中） */
  focusId?: string | null;
}

/**
 * 关系图：力导向布局。中心点最大，被封禁的点红色描边，图例按点类型开关。
 * 点击一个点交给调用方（通常是以它为中心再展开一层，或打开详情）。
 */
export function RelationGraph({ graph, height = 520, onNodeClick, focusId }: RelationGraphProps) {
  const ref = useRef<HTMLDivElement>(null);
  const chartRef = useRef<echarts.ECharts | null>(null);
  const { resolvedTheme } = useTheme();
  const clickRef = useRef(onNodeClick);
  const byId = useMemo(() => new Map(graph.nodes.map((n) => [n.id, n])), [graph]);
  const byIdRef = useRef(byId);
  // 事件回调里读最新的点表与点击处理：图表只初始化一次，不能每次渲染重新绑定
  useEffect(() => {
    clickRef.current = onNodeClick;
    byIdRef.current = byId;
  }, [onNodeClick, byId]);

  const option = useMemo(() => {
    const fg = cssVar("--foreground", resolvedTheme === "dark" ? "#e5e7eb" : "#111827");
    const muted = cssVar("--muted-foreground", "#6b7280");
    const border = cssVar("--border", "#e5e7eb");
    const destructive = "#ef4444";
    const degree = new Map<string, number>();
    for (const l of graph.links) {
      degree.set(l.source, (degree.get(l.source) ?? 0) + 1);
      degree.set(l.target, (degree.get(l.target) ?? 0) + 1);
    }
    const nodes = graph.nodes.map((n) => {
      const st = KIND_STYLE[n.kind] ?? KIND_STYLE.account;
      const center = n.id === graph.center;
      const banned = isBanned(n);
      const size = (center ? st.size * 1.6 : st.size) + Math.min(10, (degree.get(n.id) ?? 0) * 0.6);
      return {
        id: n.id,
        name: n.label || n.id,
        category: KINDS.indexOf(n.kind),
        symbol: st.symbol,
        symbolSize: size,
        value: n.depth,
        itemStyle: {
          color: st.color,
          borderColor: banned ? destructive : n.id === focusId ? fg : "rgba(255,255,255,0.85)",
          borderWidth: banned ? 3 : n.id === focusId || center ? 2.5 : 1,
          shadowBlur: center ? 14 : 0,
          shadowColor: st.color,
        },
        label: {
          show: center || n.depth <= 1 || graph.nodes.length <= 60,
          position: "right" as const,
          color: banned ? destructive : fg,
          fontSize: center ? 13 : 11,
          fontWeight: center ? ("bold" as const) : ("normal" as const),
          formatter: (p: { name: string }) => (p.name.length > 18 ? `${p.name.slice(0, 17)}…` : p.name),
        },
      };
    });
    const links = graph.links.map((l) => ({
      source: l.source,
      target: l.target,
      value: l.count,
      kind: l.kind,
      lineStyle: {
        color: l.kind === "invited" ? "#f59e0b" : border,
        width: Math.min(4, 1 + Math.log10(Math.max(1, l.count))),
        opacity: 0.85,
        curveness: l.kind === "invited" ? 0.2 : 0.05,
        type: (l.kind === "in_subnet" || l.kind === "account_of" ? "dashed" : "solid") as "dashed" | "solid",
      },
    }));
    return {
      backgroundColor: "transparent",
      tooltip: {
        confine: true,
        backgroundColor: cssVar("--popover", "#fff"),
        borderColor: border,
        textStyle: { color: fg, fontSize: 12 },
        formatter: (p: { dataType: string; data: { id?: string; source?: string; target?: string; kind?: string; value?: number } }) => {
          if (p.dataType === "edge") {
            const s = byId.get(p.data.source ?? "");
            const t = byId.get(p.data.target ?? "");
            return `${s?.label ?? p.data.source} → ${t?.label ?? p.data.target}<br/>${EDGE_LABEL[p.data.kind ?? ""] ?? p.data.kind}，${p.data.value ?? 0} 次`;
          }
          const n = byId.get(p.data.id ?? "");
          if (!n) return "";
          const head = `<b>${NODE_KIND_LABEL[n.kind] ?? n.kind}</b>　${n.label}`;
          const lines = propLines(n);
          return [head, ...lines].join("<br/>");
        },
      },
      legend: {
        top: 0,
        left: 0,
        textStyle: { color: muted, fontSize: 12 },
        itemWidth: 12,
        itemHeight: 12,
        data: KINDS.filter((k) => graph.nodes.some((n) => n.kind === k)).map((k) => ({ name: NODE_KIND_LABEL[k], icon: KIND_STYLE[k].symbol })),
      },
      animationDurationUpdate: 600,
      series: [
        {
          type: "graph",
          layout: "force",
          roam: true,
          draggable: true,
          top: 36,
          data: nodes,
          links,
          categories: KINDS.map((k) => ({ name: NODE_KIND_LABEL[k], itemStyle: { color: KIND_STYLE[k].color } })),
          force: { repulsion: graph.nodes.length > 120 ? 120 : 220, edgeLength: [50, 140], gravity: 0.08, friction: 0.15, layoutAnimation: true },
          emphasis: { focus: "adjacency" as const, lineStyle: { width: 3 } },
          edgeSymbol: ["none", "arrow"],
          edgeSymbolSize: 6,
          labelLayout: { hideOverlap: true },
          scaleLimit: { min: 0.3, max: 4 },
        },
      ],
    };
  }, [graph, byId, focusId, resolvedTheme]);

  useEffect(() => {
    if (!ref.current) return;
    const chart = echarts.init(ref.current, undefined, { renderer: "canvas" });
    chartRef.current = chart;
    chart.on("click", (p) => {
      if (p.dataType !== "node") return;
      const id = (p.data as { id?: string })?.id;
      const n = id ? byIdRef.current.get(id) : undefined;
      if (n) clickRef.current?.(n);
    });
    const ro = new ResizeObserver(() => chart.resize());
    ro.observe(ref.current);
    return () => {
      ro.disconnect();
      chart.dispose();
      chartRef.current = null;
    };
  }, []);

  useEffect(() => {
    chartRef.current?.setOption(option as echarts.EChartsCoreOption, { notMerge: true });
  }, [option]);

  return <div ref={ref} style={{ height }} className="w-full" />;
}
