"use client";

import { useMemo } from "react";
import { Background, Handle, MarkerType, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { LucideIcon } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * 场景决策管线：装配 → 名单 → 特征 → 派生 → 模型 → 规则 → 决策流 → 策略 → 处置。
 *
 * 只读布局（不能拖、不能连线），节点上是每一步的实时摘要与校验问题数，点节点切换下方的编辑面板。
 * 用 xyflow 而不是一排 div：窄屏时整条管线按比例缩放、可横向拖动查看，连线与箭头也不用手画。
 */

export type PipelineStage = {
  key: string;
  title: string;
  icon: LucideIcon;
  summary: string[];
  issues: number;
  /** 这一步没有任何配置（虚线框） */
  idle?: boolean;
};

type StageData = PipelineStage & { selected: boolean; first: boolean; last: boolean };

const NODE_W = 156;
const GAP = 44;

function StageNode({ data }: NodeProps<Node<StageData>>) {
  const Icon = data.icon;
  return (
    <div
      className={cn(
        "flex h-[104px] cursor-pointer flex-col gap-1.5 rounded-xl border bg-card px-3 py-2.5 text-left transition-colors",
        data.selected ? "border-primary ring-2 ring-primary/25" : "hover:border-foreground/30",
        data.idle && !data.selected && "border-dashed",
        data.issues > 0 && !data.selected && "border-red-300 dark:border-red-800",
      )}
      style={{ width: NODE_W }}
    >
      {!data.first ? <Handle type="target" position={Position.Left} className="size-1.5! border-0! bg-border!" isConnectable={false} /> : null}
      <div className="flex min-w-0 items-center gap-1.5">
        <Icon className={cn("size-3.5 shrink-0", data.selected ? "text-primary" : "text-muted-foreground")} />
        <span className="min-w-0 flex-1 truncate text-xs font-semibold">{data.title}</span>
        {data.issues > 0 ? (
          <span className="grid h-4 min-w-4 place-items-center rounded-full bg-destructive px-1 text-[10px] leading-none font-semibold text-white">{data.issues}</span>
        ) : null}
      </div>
      <div className="flex min-w-0 flex-col gap-0.5">
        {data.summary.slice(0, 3).map((line, i) => (
          <span key={i} className={cn("truncate text-[11px] leading-4", i === 0 ? "text-foreground" : "text-muted-foreground")}>{line}</span>
        ))}
      </div>
      {!data.last ? <Handle type="source" position={Position.Right} className="size-1.5! border-0! bg-border!" isConnectable={false} /> : null}
    </div>
  );
}

const nodeTypes = { stage: StageNode };

export function ScenePipeline({ stages, selected, onSelect, className }: {
  stages: PipelineStage[];
  selected: string;
  onSelect: (key: string) => void;
  className?: string;
}) {
  const { nodes, edges } = useMemo(() => {
    const nodes: Node<StageData>[] = stages.map((s, i) => ({
      id: s.key,
      type: "stage",
      position: { x: i * (NODE_W + GAP), y: 0 },
      data: { ...s, selected: s.key === selected, first: i === 0, last: i === stages.length - 1 },
      draggable: false,
      connectable: false,
      selectable: false,
    }));
    const edges: Edge[] = stages.slice(1).map((s, i) => ({
      id: `${stages[i].key}-${s.key}`,
      source: stages[i].key,
      target: s.key,
      type: "straight",
      style: { stroke: "var(--border)", strokeWidth: 1.5 },
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: "var(--muted-foreground)" },
    }));
    return { nodes, edges };
  }, [stages, selected]);

  return (
    <div className={cn("h-[150px] min-w-0 overflow-hidden rounded-xl border bg-muted/20", className)}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodeClick={(_, node) => onSelect(node.id)}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        zoomOnPinch={false}
        zoomOnDoubleClick={false}
        panOnScroll={false}
        preventScrolling={false}
        fitView
        fitViewOptions={{ padding: 0.08, minZoom: 0.45, maxZoom: 1 }}
        minZoom={0.45}
        maxZoom={1}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={18} size={1} />
      </ReactFlow>
    </div>
  );
}
