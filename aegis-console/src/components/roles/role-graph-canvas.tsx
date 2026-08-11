"use client";

import { useCallback, useEffect, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  useNodesState,
  useEdgesState,
  type Node,
  type Edge,
  MarkerType,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useRoleGraphQuery } from "@/lib/admin-hooks";
import { Badge } from "@/components/ui/badge";
import { LoadingState, EmptyState } from "@/components/ui/data-state";

const COLORS: Record<string, string> = {
  super_admin: "#ef4444",
  platform_admin: "#3b82f6",
  app_admin: "#6366f1",
  app_operator: "#eab308",
  app_auditor: "#f97316",
  app_viewer: "#6b7280",
};

function RoleNode({ data }: { data: { name: string; level: number; scope: string; isCustom: boolean; permCount: number; color: string } }) {
  return (
    <div className="rounded-lg border-2 bg-card px-4 py-3 shadow-md min-w-[140px] text-center" style={{ borderColor: data.color }}>
      <div className="text-sm font-semibold">{data.name}</div>
      <div className="flex items-center justify-center gap-1.5 mt-1">
        <Badge variant={data.isCustom ? "outline" : "secondary"} className="text-[9px]">
          {data.isCustom ? "自定义" : data.scope === "global" ? "全局" : "应用"}
        </Badge>
        <span className="text-[10px] text-muted-foreground">Lv.{data.level}</span>
      </div>
      <div className="text-[10px] text-muted-foreground mt-0.5">{data.permCount} 项权限</div>
    </div>
  );
}

const nodeTypes = { role: RoleNode };

export function RoleGraphCanvas() {
  const { data: graph, isLoading } = useRoleGraphQuery();

  const { initialNodes, initialEdges } = useMemo(() => {
    if (!graph) return { initialNodes: [], initialEdges: [] };

    // 简单的按 level 分层布局
    const sorted = [...graph.nodes].sort((a, b) => b.level - a.level);
    const levels = new Map<number, typeof sorted>();
    for (const n of sorted) {
      const arr = levels.get(n.level) || [];
      arr.push(n);
      levels.set(n.level, arr);
    }

    const nodes: Node[] = [];
    let y = 0;
    for (const [, group] of [...levels.entries()].sort((a, b) => b[0] - a[0])) {
      const totalWidth = group.length * 200;
      const startX = -totalWidth / 2 + 100;
      group.forEach((n, i) => {
        nodes.push({
          id: n.key,
          type: "role",
          position: { x: startX + i * 200, y },
          data: {
            name: n.name, level: n.level, scope: n.scope,
            isCustom: n.isCustom, permCount: n.permCount,
            color: n.isCustom ? "#8b5cf6" : (COLORS[n.key] || "#6b7280"),
          },
        });
      });
      y += 120;
    }

    const edges: Edge[] = graph.edges.map((e, i) => ({
      id: `e-${i}`,
      source: e.source,
      target: e.target,
      animated: e.relation === "inherits",
      style: { stroke: e.relation === "inherits" ? "#8b5cf6" : "#94a3b8", strokeDasharray: e.relation === "inherits" ? "5 5" : undefined },
      markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
      label: e.relation === "inherits" ? "继承" : "包含",
    }));

    return { initialNodes: nodes, initialEdges: edges };
  }, [graph]);

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  useEffect(() => {
    setNodes(initialNodes);
    setEdges(initialEdges);
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  if (isLoading) return <LoadingState title="加载关系图" description="" />;
  if (!graph) return <EmptyState title="无数据" description="" />;

  return (
    <div className="h-[600px] rounded-xl border bg-card overflow-hidden">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={nodeTypes}
        fitView
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={20} />
        <Controls />
      </ReactFlow>
    </div>
  );
}
