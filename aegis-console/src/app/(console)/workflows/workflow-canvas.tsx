"use client";

import { useEffect, useMemo, useState } from "react";
import dagre from "dagre";
import {
  addEdge,
  Background,
  ConnectionLineType,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  type Connection,
  type Edge,
  type Node,
  type OnNodeDrag,
  type NodeProps,
  type ReactFlowInstance,
  type XYPosition
} from "@xyflow/react";
import type { WorkflowEdge, WorkflowNode } from "@/lib/api-client";
import { Button } from "@/components/ui/button";
import { summarizeConfig } from "./workflow-helpers";

type WorkflowCanvasProps = {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  selectedNodeId: string | null;
  uiConfigText: string;
  isFullscreen: boolean;
  onSelectNode: (id: string | null) => void;
  onNodesChange: (nodes: WorkflowNode[]) => void;
  onEdgesChange: (edges: WorkflowEdge[]) => void;
  onUiConfigTextChange: (text: string) => void;
  onCreateNode: (type: string, label: string, position?: XYPosition) => void;
};

type DesignerPositions = Record<string, XYPosition>;

type WorkflowCanvasNodeData = {
  label: string;
  type: string;
  summary: string;
};

type WorkflowCanvasNode = Node<WorkflowCanvasNodeData>;

const NODE_WIDTH = 240;
const NODE_HEIGHT = 116;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function getToneClass(type: string, selected: boolean) {
  if (selected) {
    return "border-slate-950 bg-slate-950 text-white shadow-[0_18px_48px_-28px_rgba(15,23,42,0.72)] dark:border-sky-400/20 dark:bg-slate-950 dark:text-slate-50 dark:shadow-[0_24px_64px_-30px_rgba(2,6,23,0.82)]";
  }
  switch (type) {
    case "start":
      return "border-emerald-300 bg-emerald-50 text-emerald-950 dark:border-emerald-500/24 dark:bg-emerald-500/10 dark:text-emerald-100";
    case "condition":
      return "border-amber-300 bg-amber-50 text-amber-950 dark:border-amber-500/24 dark:bg-amber-500/10 dark:text-amber-100";
    case "webhook":
      return "border-sky-300 bg-sky-50 text-sky-950 dark:border-sky-500/24 dark:bg-sky-500/10 dark:text-sky-100";
    case "end":
      return "border-slate-300 bg-slate-100 text-slate-950 dark:border-slate-500/24 dark:bg-slate-500/10 dark:text-slate-100";
    default:
      return "border-violet-300 bg-violet-50 text-violet-950 dark:border-violet-500/24 dark:bg-violet-500/10 dark:text-violet-100";
  }
}

function WorkflowCanvasNodeView({ data, selected }: NodeProps<WorkflowCanvasNode>) {
  const toneClass = getToneClass(data.type, selected);
  const showTarget = data.type !== "start";
  const showSource = data.type !== "end";

  return (
    <div className={`w-[240px] rounded-[22px] border px-4 py-3 text-left transition ${toneClass}`}>
      {showTarget ? (
        <Handle
          type="target"
          position={Position.Left}
          className="!h-3 !w-3 !border-2 !border-white/90 !bg-slate-700 dark:!border-slate-950 dark:!bg-slate-200"
        />
      ) : null}
      {showSource ? (
        <Handle
          type="source"
          position={Position.Right}
          className="!h-3 !w-3 !border-2 !border-white/90 !bg-slate-700 dark:!border-slate-950 dark:!bg-slate-200"
        />
      ) : null}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{data.label}</div>
          <div className={`mt-1 text-[11px] uppercase tracking-[0.18em] ${selected ? "text-slate-300" : "opacity-70"}`}>
            {data.type}
          </div>
        </div>
      </div>
      <div
        className={`mt-3 rounded-2xl border px-3 py-2 text-[11px] leading-5 ${
          selected
            ? "border-white/10 bg-white/8 text-slate-300 dark:border-white/8 dark:bg-white/5 dark:text-slate-300"
            : "border-black/5 bg-white/70 text-slate-600 dark:border-white/8 dark:bg-slate-950/36 dark:text-slate-300"
        }`}
      >
        {data.summary}
      </div>
    </div>
  );
}

const flowNodeTypes = {
  workflowNode: WorkflowCanvasNodeView
};

function computeLayoutPositions(nodes: WorkflowNode[], edges: WorkflowEdge[]) {
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({ rankdir: "LR", ranksep: 90, nodesep: 40, marginx: 24, marginy: 24 });

  for (const node of nodes) {
    graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  }

  for (const edge of edges) {
    if (edge.source && edge.target) {
      graph.setEdge(edge.source, edge.target);
    }
  }

  dagre.layout(graph);

  const positions: DesignerPositions = {};
  for (const node of nodes) {
    const point = graph.node(node.id);
    positions[node.id] = point
      ? { x: point.x - NODE_WIDTH / 2, y: point.y - NODE_HEIGHT / 2 }
      : { x: 0, y: 0 };
  }

  return positions;
}

function readDesignerPositions(uiConfigText: string) {
  try {
    const parsed = JSON.parse(uiConfigText);
    if (!isRecord(parsed)) {
      return {};
    }
    const designer = isRecord(parsed.designer) ? parsed.designer : null;
    const positions = designer && isRecord(designer.positions) ? designer.positions : null;
    if (!positions) {
      return {};
    }

    return Object.entries(positions).reduce<DesignerPositions>((acc, [nodeId, value]) => {
      if (isRecord(value) && typeof value.x === "number" && typeof value.y === "number") {
        acc[nodeId] = { x: value.x, y: value.y };
      }
      return acc;
    }, {});
  } catch {
    return {};
  }
}

function buildUiConfigText(uiConfigText: string, positions: DesignerPositions) {
  let base: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(uiConfigText);
    if (isRecord(parsed)) {
      base = parsed;
    }
  } catch {
    base = {};
  }

  const designer = isRecord(base.designer) ? base.designer : {};
  return JSON.stringify(
    {
      ...base,
      designer: {
        ...designer,
        positions
      }
    },
    null,
    2
  );
}

function buildFlowNodes(nodes: WorkflowNode[], edges: WorkflowEdge[], uiConfigText: string, selectedNodeId: string | null) {
  const storedPositions = readDesignerPositions(uiConfigText);
  const layoutPositions = computeLayoutPositions(nodes, edges);

  const positions = nodes.reduce<DesignerPositions>((acc, node) => {
    acc[node.id] = storedPositions[node.id] || layoutPositions[node.id] || { x: 0, y: 0 };
    return acc;
  }, {});

  const flowNodes: WorkflowCanvasNode[] = nodes.map((node) => ({
    id: node.id,
    type: "workflowNode",
    position: positions[node.id],
    selected: node.id === selectedNodeId,
    data: {
      label: node.name,
      type: node.type,
      summary: summarizeConfig(node.config)
    }
  }));

  return { flowNodes, positions };
}

function buildFlowEdges(edges: WorkflowEdge[]) {
  return edges.map<Edge>((edge, index) => ({
    id: edge.id || `edge_${edge.source}_${edge.target}_${index}`,
    source: edge.source,
    target: edge.target,
    label: edge.condition || undefined,
    markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18 },
    type: "smoothstep",
    animated: false
  }));
}

export function WorkflowCanvas({
  nodes,
  edges,
  selectedNodeId,
  uiConfigText,
  isFullscreen,
  onSelectNode,
  onNodesChange,
  onEdgesChange,
  onUiConfigTextChange,
  onCreateNode
}: WorkflowCanvasProps) {
  const [reactFlow, setReactFlow] = useState<ReactFlowInstance<WorkflowCanvasNode, Edge> | null>(null);

  const { flowNodes, positions } = useMemo(
    () => buildFlowNodes(nodes, edges, uiConfigText, selectedNodeId),
    [edges, nodes, selectedNodeId, uiConfigText]
  );

  const flowEdges = useMemo(() => buildFlowEdges(edges), [edges]);

  useEffect(() => {
    if (!reactFlow) {
      return;
    }
    const frame = window.requestAnimationFrame(() => {
      reactFlow.fitView({ padding: 0.18, duration: 240 });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [reactFlow, nodes.length, edges.length, isFullscreen]);

  function syncPositions(nextPositions: DesignerPositions) {
    onUiConfigTextChange(buildUiConfigText(uiConfigText, nextPositions));
  }

  function handleConnect(connection: Connection) {
    if (!connection.source || !connection.target) {
      return;
    }

    const nextEdges = addEdge(
      {
        ...connection,
        id: `edge_${Date.now()}`,
        type: "smoothstep"
      },
      flowEdges
    ).map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      condition: typeof edge.label === "string" ? edge.label : ""
    }));

    onEdgesChange(nextEdges);
  }

  const handleNodeDragStop: OnNodeDrag<WorkflowCanvasNode> = (_event, node) => {
    syncPositions({
      ...positions,
      [node.id]: node.position
    });
  };

  function handleNodesDelete(deletedNodes: WorkflowCanvasNode[]) {
    const deletedIds = new Set(deletedNodes.map((node) => node.id));
    const nextNodes = nodes.filter((node) => !deletedIds.has(node.id));
    const nextEdges = edges.filter((edge) => !deletedIds.has(edge.source) && !deletedIds.has(edge.target));
    const nextPositions = Object.fromEntries(
      Object.entries(positions).filter(([nodeId]) => !deletedIds.has(nodeId))
    );

    onNodesChange(nextNodes);
    onEdgesChange(nextEdges);
    syncPositions(nextPositions);
    onSelectNode(null);
  }

  function handleEdgesDelete(deletedEdges: Edge[]) {
    const deletedIds = new Set(deletedEdges.map((edge) => edge.id));
    onEdgesChange(edges.filter((edge, index) => !deletedIds.has(edge.id || `edge_${edge.source}_${edge.target}_${index}`)));
  }

  function handleDrop(event: React.DragEvent<HTMLDivElement>) {
    event.preventDefault();

    if (!reactFlow) {
      return;
    }

    const type = event.dataTransfer.getData("application/aegis-workflow-node-type");
    const label = event.dataTransfer.getData("application/aegis-workflow-node-label");

    if (!type || !label) {
      return;
    }

    const position = reactFlow.screenToFlowPosition({
      x: event.clientX,
      y: event.clientY
    });

    onCreateNode(type, label, position);
  }

  function handleAutoLayout() {
    const nextPositions = computeLayoutPositions(nodes, edges);
    syncPositions(nextPositions);
    window.requestAnimationFrame(() => {
      reactFlow?.fitView({ padding: 0.18, duration: 260 });
    });
  }

  return (
    <div className={`workflow-canvas-shell ${isFullscreen ? "workflow-canvas-shell--fullscreen" : ""}`}>
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={flowNodeTypes}
        onInit={setReactFlow}
        onConnect={handleConnect}
        onNodeDragStop={handleNodeDragStop}
        onNodesDelete={handleNodesDelete}
        onEdgesDelete={handleEdgesDelete}
        onNodeClick={(_, node) => onSelectNode(node.id)}
        onPaneClick={() => onSelectNode(null)}
        onDrop={handleDrop}
        onDragOver={(event) => {
          event.preventDefault();
          event.dataTransfer.dropEffect = "copy";
        }}
        fitView
        minZoom={0.35}
        maxZoom={1.8}
        deleteKeyCode={["Backspace", "Delete"]}
        proOptions={{ hideAttribution: true }}
        connectionLineType={ConnectionLineType.SmoothStep}
        defaultEdgeOptions={{
          type: "smoothstep",
          markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18 }
        }}
      >
        <Background gap={20} size={1} color="var(--canvas-grid-color)" />
        <MiniMap
          pannable
          zoomable
          className="!rounded-2xl !border !border-border/80 !bg-background/90 dark:!bg-background/85"
          maskColor="var(--canvas-mask-color)"
        />
        <Controls className="!overflow-hidden !rounded-2xl !border !border-border/80 !bg-background/92 !shadow-none dark:!bg-background/82" />
        <Panel position="top-right" className="!m-3 flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={handleAutoLayout}>
            自动整理
          </Button>
        </Panel>
        <Panel className="!m-3 rounded-2xl border border-border/80 bg-background/90 px-3 py-2 text-xs text-muted-foreground shadow-sm dark:bg-background/80" position="bottom-left">
          拖动节点排版，按住节点连接锚点即可快速连线。
        </Panel>
      </ReactFlow>
    </div>
  );
}
