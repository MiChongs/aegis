"use client";

import "@flowgram.ai/free-layout-editor/index.css";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  EditorRenderer,
  FreeLayoutEditorProvider,
  LineType,
  WorkflowDragService,
  WorkflowLineEntity,
  WorkflowNodeEntity,
  WorkflowSelectService,
  delay,
  getNodeForm,
  useClientContext,
  usePlaygroundTools,
  useService,
  type FreeLayoutPluginContext,
  type FreeLayoutProps,
  type ShortcutsHandler,
  type WorkflowJSON,
  type WorkflowPortEntity
} from "@flowgram.ai/free-layout-editor";
import { createFreeSnapPlugin } from "@flowgram.ai/free-snap-plugin";
import { FlowMinimapService, MinimapRender, createMinimapPlugin } from "@flowgram.ai/minimap-plugin";
import { useTheme } from "next-themes";
import { useHotkeys } from "react-hotkeys-hook";
import {
  AlertTriangle,
  ArrowLeft,
  CircleAlert,
  CircleCheck,
  Crosshair,
  FileCode2,
  FileDown,
  FileUp,
  ListChecks,
  Loader2,
  Map as MapIcon,
  Maximize,
  Minus,
  PanelLeft,
  PanelRight,
  Plus,
  Redo2,
  Rocket,
  Rows3,
  Save,
  Search,
  Settings2,
  Spline,
  Undo2,
  Waypoints,
  X
} from "lucide-react";
import { toast } from "sonner";
import { Field } from "@/components/apps/item-grant/item-grant-fields";
import { NODE_WIDTH, edgeId, fromCanvas, graphSignature, newId, toCanvas, type CanvasJSON } from "@/components/apps/item-grant/item-grant-graph";
import { EgressNodeCard, EgressOverlayContext, type EgressCanvasOverlay } from "@/components/configuration/egress/egress-canvas-nodes";
import { EgressImportDialog } from "@/components/configuration/egress/egress-import-dialog";
import { EgressInspector } from "@/components/configuration/egress/egress-inspector";
import { readNodeValues } from "@/components/configuration/egress/egress-inspector-shared";
import { ACTION_LABEL, CORE_STATUS_LABEL, NODE_VISUAL, PORT_COLOR, defaultNodeData, nodeTitle } from "@/components/configuration/egress/egress-meta";
import { ToneIcon } from "@/components/users/detail/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ItemGrantGraph } from "@/lib/api/item-grant";
import type {
  EgressCompileView,
  EgressExplanation,
  EgressGraph,
  EgressGraphCatalog,
  EgressGraphView,
  EgressIssue,
  EgressNodeType,
  EgressValidateResult
} from "@/lib/api/egress";
import { ApiError } from "@/lib/api/client";
import { formatDateTime } from "@/lib/datetime";
import {
  useApplyEgressGraphMutation,
  useEgressExplainMutation,
  useExportEgressGraphMutation,
  useMihomoDelayMutation,
  useMihomoProxiesQuery,
  useMihomoStatusQuery,
  useSaveEgressGraphMutation,
  useSelectMihomoMutation,
  useValidateEgressGraphMutation
} from "@/lib/egress-hooks";
import { cn } from "@/lib/utils";

/**
 * 出海网关画布工作区（Flowgram 自由布局编辑器，只在浏览器里加载）。
 *
 * | 区域 | 作用 |
 * |---|---|
 * | 顶栏 | 已应用版本、草稿是否有改动、问题数、导入 mihomo 配置、导出 YAML、保存草稿（⌘S）、应用 |
 * | 节点库（左侧浮层） | 网关设置、路由规则、网关线路；mihomo 的策略组、节点、订阅、分流、核心。拖到画布上或点一下放到中央 |
 * | 画布 | 规则链竖排（自上而下依次匹配）；「命中 / 未命中」接线路、策略组、节点或 mihomo 分流；线路「经由」上一跳；
 *          策略组「成员」接节点、策略组、订阅；节点与订阅「前置代理」接另一个节点或策略组。只能连合法的线、不成环 |
 * | 底部工具条 | 撤销 / 重做、缩放、适应画布、整理排版（按路由去向分列）、曲线 / 折线、小地图 |
 * | 右侧 | 配置（选中节点的表单 / 未选中时是概况）、问题（实时校验）、追踪（输入域名看它怎么出去，路径在画布上流动）、预览（编译出的路由表与核心 YAML） |
 *
 * 运行态叠在卡片上：线路健康与延迟、规则命中数、策略组当前节点（可就地切换）、节点测速、订阅用量、核心速率与内存。
 */

const ACCENT = "#6366f1";

type CanvasBridge = {
  catalog: EgressGraphCatalog;
  edgeIssues: Set<string>;
  traced: Set<string>;
  onContentChange: () => void;
  onLoaded: (signature: string) => void;
  onDropLine: (port: WorkflowPortEntity, canvas: { x: number; y: number }, client: { x: number; y: number }) => void;
};

function lineKey(line: WorkflowLineEntity) {
  const port = line.fromPort?.portID;
  return edgeId(line.from?.id ?? "", port === undefined ? "" : String(port), line.to?.id ?? "");
}

function toCanvasJSON(graph: EgressGraph) {
  return toCanvas(graph as unknown as ItemGrantGraph) as unknown as WorkflowJSON;
}

function fromCanvasJSON(json: unknown): EgressGraph {
  return fromCanvas(json as CanvasJSON) as unknown as EgressGraph;
}

function portSpec(catalog: EgressGraphCatalog, type: string, port: string) {
  return catalog.nodes.find((spec) => spec.type === type)?.outputs?.find((output) => output.id === port);
}

/** 这几类连线连起来不能成环：规则链、经由、成员与前置代理。 */
const CYCLE_PORTS = new Set(["rules", "next", "via", "members", "dialer"]);

function buildEditorProps(graph: EgressGraph, catalog: EgressGraphCatalog, bridge: { current: CanvasBridge }): FreeLayoutProps {
  const selectAll: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "egress.select-all",
    shortcuts: ["meta a", "ctrl a"],
    execute: () => {
      ctx.selection.selection = ctx.document.getAllNodes();
    }
  });
  const remove: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "egress.delete",
    shortcuts: ["backspace", "delete"],
    execute: () => {
      if (ctx.playground.config.readonly) return;
      ctx.history.startTransaction();
      for (const entity of ctx.selection.selection) {
        if (entity instanceof WorkflowNodeEntity) {
          if (ctx.document.canRemove(entity)) entity.dispose();
        } else if (entity instanceof WorkflowLineEntity) {
          if (ctx.document.linesManager.canRemove(entity)) entity.dispose();
        }
      }
      ctx.selection.selection = ctx.selection.selection.filter((entity) => !entity.disposed);
      ctx.history.endTransaction();
    }
  });
  const duplicate: (ctx: FreeLayoutPluginContext) => ShortcutsHandler = (ctx) => ({
    commandId: "egress.duplicate",
    shortcuts: ["meta d", "ctrl d"],
    execute: () => {
      const nodes = ctx.selection.selection.filter((entity): entity is WorkflowNodeEntity => entity instanceof WorkflowNodeEntity);
      void duplicateNodes(ctx, nodes, bridge.current.catalog);
    }
  });

  return {
    background: { gridSize: 20, dotSize: 1, dotColor: "var(--eg-dot)", dotOpacity: 1 },
    readonly: false,
    twoWayConnection: false,
    initialData: toCanvasJSON(graph),
    nodeRegistries: catalog.nodes.map((spec) => ({
      type: spec.type,
      meta: {
        useDynamicPort: true,
        defaultPorts: spec.input ? [{ type: "input" as const, location: spec.input }] : [],
        defaultExpanded: true
      },
      formMeta: { render: () => <></> }
    })),
    nodeEngine: { enable: true },
    history: { enable: true, enableChangeNode: true },
    materials: { renderDefaultNode: EgressNodeCard },
    lineColor: {
      hidden: "transparent",
      default: "var(--eg-line, #94a3b8)",
      drawing: ACCENT,
      hovered: ACCENT,
      selected: ACCENT,
      error: "#ef4444",
      flowing: "#10b981"
    },
    setLineClassName: (_ctx, line) => {
      const port = String(line.fromPort?.portID ?? "");
      const traced = bridge.current.traced;
      return cn(`eg-line-${port}`, bridge.current.edgeIssues.has(lineKey(line)) && "eg-line-error", traced.size > 0 && !traced.has(lineKey(line)) && "eg-line-dim");
    },
    isFlowingLine: (_ctx, line) => bridge.current.traced.has(lineKey(line)),
    canAddLine: (ctx, fromPort, toPort) => {
      if (fromPort.portType !== "output" || toPort.portType !== "input") return false;
      const source = fromPort.node;
      const target = toPort.node;
      if (source.id === target.id) return false;
      const port = String(fromPort.portID ?? "");
      const spec = portSpec(bridge.current.catalog, String(source.flowNodeType), port);
      if (!spec || !(spec.accepts ?? []).includes(target.flowNodeType as EgressNodeType)) return false;
      const lines = ctx.document.linesManager.getAllLines();
      if (lines.some((line) => line.from?.id === source.id && line.to?.id === target.id && String(line.fromPort?.portID) === port)) return false;
      if (!spec.multi && lines.some((line) => line.from?.id === source.id && String(line.fromPort?.portID) === port)) return false;
      // 规则链是一条直线：每条规则只能有一个上游
      if ((port === "rules" || port === "next") && lines.some((line) => line.to?.id === target.id && ["rules", "next"].includes(String(line.fromPort?.portID)))) return false;
      if (!CYCLE_PORTS.has(port)) return true;
      const next = new Map<string, string[]>();
      for (const line of lines) {
        if (!line.from || !line.to || !CYCLE_PORTS.has(String(line.fromPort?.portID))) continue;
        next.set(line.from.id, [...(next.get(line.from.id) ?? []), line.to.id]);
      }
      const stack = [target.id];
      const seen = new Set<string>();
      while (stack.length) {
        const id = stack.pop() as string;
        if (id === source.id) return false;
        if (seen.has(id)) continue;
        seen.add(id);
        stack.push(...(next.get(id) ?? []));
      }
      return true;
    },
    onDragLineEnd: async (_ctx, params) => {
      const { fromPort, toPort, mousePos, line, originLine, event } = params;
      if (originLine || !line || toPort || !fromPort) return;
      const native = event as unknown as { clientX?: number; clientY?: number; event?: { clientX: number; clientY: number } };
      const client = native.event ?? { clientX: native.clientX ?? 0, clientY: native.clientY ?? 0 };
      bridge.current.onDropLine(fromPort, mousePos, { x: client.clientX, y: client.clientY });
    },
    onContentChange: () => bridge.current.onContentChange(),
    onAllLayersRendered: (ctx) => {
      void ctx.tools.fitView(false);
      bridge.current.onLoaded(graphSignature(fromCanvas(ctx.document.toJSON() as unknown as CanvasJSON)));
    },
    shortcuts: (registry, ctx) => registry.addHandlers(selectAll(ctx), remove(ctx), duplicate(ctx)),
    plugins: () => [
      createMinimapPlugin({ disableLayer: true, canvasStyle: { canvasWidth: 200, canvasHeight: 124, canvasPadding: 40 } }),
      createFreeSnapPlugin({ edgeColor: ACCENT, alignColor: ACCENT, edgeLineWidth: 1, alignLineWidth: 1, alignCrossWidth: 8 })
    ]
  };
}

function createEditor(graph: EgressGraph, catalog: EgressGraphCatalog, handlers: Pick<CanvasBridge, "onContentChange" | "onLoaded" | "onDropLine">) {
  const bridge = { current: { catalog, edgeIssues: new Set<string>(), traced: new Set<string>(), ...handlers } as CanvasBridge };
  return {
    props: buildEditorProps(graph, catalog, bridge),
    update: (patch: Partial<Pick<CanvasBridge, "edgeIssues" | "traced">>) => {
      Object.assign(bridge.current, patch);
    }
  };
}

/** 复制出来的节点改个名字：网关线路名、规则名、核心里的名字都必须唯一。 */
function renameCopy(type: EgressNodeType, data: Record<string, unknown>) {
  const suffix = (value: unknown) => `${String(value ?? "")} 副本`;
  switch (type) {
    case "rule":
      return { ...data, name: suffix(data.name) };
    case "endpoint":
      return { ...data, name: `${String(data.name ?? "line")}-copy` };
    case "m_provider":
      return { ...data, name: suffix(data.name) };
    case "m_proxy":
      return { ...data, proxy: { ...(data.proxy as object), name: suffix((data.proxy as Record<string, unknown>)?.name) } };
    case "m_group":
      return { ...data, group: { ...(data.group as object), name: suffix((data.group as Record<string, unknown>)?.name) } };
    default:
      return data;
  }
}

async function duplicateNodes(ctx: FreeLayoutPluginContext, nodes: WorkflowNodeEntity[], catalog: EgressGraphCatalog) {
  const single = new Set(catalog.nodes.filter((spec) => spec.single).map((spec) => spec.type as string));
  const source = nodes.filter((node) => !single.has(String(node.flowNodeType)));
  if (!source.length) return [];
  const mapping = new Map<string, WorkflowNodeEntity>();
  ctx.history.startTransaction();
  for (const node of source) {
    const position = node.transform.position;
    const data = renameCopy(node.flowNodeType as EgressNodeType, JSON.parse(JSON.stringify(readNodeValues(node))));
    const copy = ctx.document.createWorkflowNodeByType(node.flowNodeType, { x: position.x + 40, y: position.y + 40 }, { id: newId(String(node.flowNodeType)), data });
    mapping.set(node.id, copy);
  }
  await delay(30);
  for (const line of ctx.document.linesManager.getAllLines()) {
    const from = line.from && mapping.get(line.from.id);
    const to = line.to && mapping.get(line.to.id);
    if (from && to) ctx.document.linesManager.createLine({ from: from.id, fromPort: line.fromPort?.portID, to: to.id });
  }
  ctx.history.endTransaction();
  ctx.selection.selection = [...mapping.values()];
  return [...mapping.values()];
}

function cssVar(element: Element, name: string, fallback: string) {
  return getComputedStyle(element).getPropertyValue(name).trim() || fallback;
}

function applyMinimapTheme(minimap: FlowMinimapService, dark: boolean) {
  const root = document.querySelector(".eg-canvas") ?? document.documentElement;
  const service = minimap as unknown as { options: { canvasStyle: Record<string, unknown> }; initStyle?: () => void; render?: () => void };
  service.options.canvasStyle = {
    ...service.options.canvasStyle,
    canvasBackground: cssVar(root, "--card", "#ffffff"),
    canvasBorderRadius: 12,
    viewportBackground: dark ? "rgba(255,255,255,0.06)" : "rgba(15,23,42,0.05)",
    viewportBorderRadius: 6,
    viewportBorderColor: ACCENT,
    viewportBorderWidth: 1,
    viewportBorderDashLength: undefined,
    nodeColor: dark ? "rgba(255,255,255,0.22)" : "rgba(15,23,42,0.16)",
    nodeBorderRadius: 3,
    nodeBorderWidth: 0,
    overlayColor: "rgba(0,0,0,0)"
  };
  service.initStyle?.();
  service.render?.();
}

/** 画布往返一次再算签名，与载入后的画布同一口径。 */
export function egressSignature(graph: EgressGraph) {
  return graphSignature(fromCanvas(toCanvas(graph as unknown as ItemGrantGraph)));
}

type DropMenuState = { port: WorkflowPortEntity; canvas: { x: number; y: number }; client: { x: number; y: number } };

export default function EgressWorkspace({
  view,
  graph,
  seededFrom,
  onClose,
  onDirtyChange,
  onReplace
}: {
  view: EgressGraphView;
  graph: EgressGraph;
  seededFrom?: string;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onReplace: (graph: EgressGraph, from: string) => void;
}) {
  const [revision, setRevision] = useState(0);
  const [baseline, setBaseline] = useState<string | null>(null);
  const [dropMenu, setDropMenu] = useState<DropMenuState | null>(null);
  const [editor] = useState(() =>
    createEditor(graph, view.catalog, {
      onContentChange: () => setRevision((value) => value + 1),
      onLoaded: (signature) => setBaseline(signature),
      onDropLine: (port, canvas, client) => setDropMenu({ port, canvas, client })
    })
  );
  const [initialSaved] = useState(() => (seededFrom ? egressSignature(view.graph) : null));

  return (
    <FreeLayoutEditorProvider {...editor.props}>
      <Workspace
        view={view}
        seededFrom={seededFrom}
        initialSaved={initialSaved}
        onClose={onClose}
        onDirtyChange={onDirtyChange}
        onReplace={onReplace}
        updateBridge={editor.update}
        revision={revision}
        baseline={baseline}
        dropMenu={dropMenu}
        setDropMenu={setDropMenu}
      />
    </FreeLayoutEditorProvider>
  );
}

function Workspace({
  view,
  seededFrom,
  initialSaved,
  onClose,
  onDirtyChange,
  onReplace,
  updateBridge,
  revision,
  baseline,
  dropMenu,
  setDropMenu
}: {
  view: EgressGraphView;
  seededFrom?: string;
  initialSaved: string | null;
  onClose: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onReplace: (graph: EgressGraph, from: string) => void;
  updateBridge: (patch: Partial<Pick<CanvasBridge, "edgeIssues" | "traced">>) => void;
  revision: number;
  baseline: string | null;
  dropMenu: DropMenuState | null;
  setDropMenu: (menu: DropMenuState | null) => void;
}) {
  const ctx = useClientContext();
  const selectService = useService(WorkflowSelectService);
  const canvasRef = useRef<HTMLDivElement>(null);
  const catalog = view.catalog;
  const loaded = baseline !== null;
  const [savedSignature, setSavedSignature] = useState<string | null>(initialSaved);
  const [draftRevision, setDraftRevision] = useState(view.draftRevision);
  const [tab, setTab] = useState("config");
  const [panelOpen, setPanelOpen] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(true);
  const [compile, setCompile] = useState<EgressCompileView>(view.compile);
  const [preview, setPreview] = useState<EgressValidateResult | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [applyOpen, setApplyOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [trace, setTrace] = useState<{ explanation: EgressExplanation; nodes: Set<string>; edges: Set<string> } | null>(null);
  const [testing, setTesting] = useState<Set<string>>(new Set());
  const save = useSaveEgressGraphMutation();
  const validate = useValidateEgressGraphMutation();
  const exporter = useExportEgressGraphMutation();
  const runValidate = validate.mutate;
  const coreActive = compile.summary.mihomoEnabled || view.core.status !== "disabled";
  const status = useMihomoStatusQuery(coreActive, 3_000);
  const proxies = useMihomoProxiesQuery(coreActive && status.data?.status === "running");
  const delayMutation = useMihomoDelayMutation();
  const select = useSelectMihomoMutation();

  const toGraph = useCallback((): EgressGraph => fromCanvasJSON(ctx.document.toJSON()), [ctx]);
  const graph = useMemo(() => (loaded ? toGraph() : null), [loaded, revision, toGraph]); // eslint-disable-line react-hooks/exhaustive-deps
  const signature = graph ? graphSignature(graph as unknown as ItemGrantGraph) : null;
  const reference = savedSignature ?? baseline;
  const dirty = signature !== null && reference !== null && signature !== reference;
  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange]);

  useEffect(() => {
    const update = () => {
      const nodes = selectService.selectedNodes.filter((node) => !node.disposed);
      setSelectedId(nodes.length === 1 ? nodes[0].id : null);
    };
    update();
    const disposable = selectService.onSelectionChanged(update);
    return () => disposable.dispose();
  }, [selectService]);
  const selectedNode = selectedId ? ctx.document.getNode(selectedId) : undefined;

  // 实时校验：停手 600ms 后让后端编译一遍（与应用时同一套判据），顺带拿到路由表与核心 YAML 预览。
  useEffect(() => {
    if (!loaded) return;
    const timer = setTimeout(
      () =>
        runValidate(toGraph(), {
          onSuccess: (data) => {
            setCompile(data.compile);
            setPreview(data);
          }
        }),
      600
    );
    return () => clearTimeout(timer);
  }, [signature, loaded, runValidate, toGraph]);

  // 新连出「命中 / 未命中」时，动作若还是直连或拒绝就改成走代理：否则这条线连了也不会用到。
  const routeLines = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (!loaded) return;
    const current = new Set<string>();
    const added: WorkflowLineEntity[] = [];
    for (const line of ctx.document.linesManager.getAllLines()) {
      const port = String(line.fromPort?.portID ?? "");
      if (port !== "hit" && port !== "fallback") continue;
      const key = lineKey(line);
      current.add(key);
      if (routeLines.current && !routeLines.current.has(key)) added.push(line);
    }
    routeLines.current = current;
    for (const line of added) {
      if (!line.from) continue;
      const form = getNodeForm(line.from);
      if (!form) continue;
      const field = String(line.fromPort?.portID) === "fallback" ? "defaultAction" : "action";
      const value = (form.values as Record<string, unknown>)[field];
      if (value === "direct" || value === "reject") {
        form.setValueIn(field, "proxy");
        toast.message(field === "defaultAction" ? "未命中时已改为走代理" : "这条规则已改为命中后走代理");
      }
    }
  }, [revision, loaded, ctx]);

  // 画布一变，追踪结果就不再对应当前配置
  const lastSignature = useRef<string | null>(null);
  useEffect(() => {
    if (signature === null) return;
    if (lastSignature.current !== null && lastSignature.current !== signature) setTrace(null);
    lastSignature.current = signature;
  }, [signature]);

  const issues = compile.issues;
  const issueMap = useMemo(() => {
    const map = new Map<string, EgressIssue[]>();
    for (const issue of issues) if (issue.nodeId) map.set(issue.nodeId, [...(map.get(issue.nodeId) ?? []), issue]);
    return map;
  }, [issues]);
  const errorCount = issues.filter((issue) => issue.level === "error").length;
  const warningCount = issues.length - errorCount;

  useEffect(() => {
    updateBridge({
      edgeIssues: new Set(issues.filter((issue) => issue.edgeId).map((issue) => issue.edgeId as string)),
      traced: trace?.edges ?? new Set()
    });
    ctx.document.linesManager.forceUpdate();
  }, [issues, trace, ctx, updateBridge]);

  const mihomoTargets = useMemo(() => {
    const names: string[] = [];
    for (const node of graph?.nodes ?? []) {
      if (node.type === "m_group") names.push(String((node.data.group as Record<string, unknown>)?.name ?? ""));
    }
    for (const node of graph?.nodes ?? []) {
      if (node.type === "m_proxy") names.push(String((node.data.proxy as Record<string, unknown>)?.name ?? ""));
    }
    return names.filter(Boolean);
  }, [graph]);

  const endpointStats = useMemo(() => new Map(view.runtime.endpoints.map((item) => [item.name, item])), [view.runtime.endpoints]);
  const ruleStats = useMemo(() => new Map(view.runtime.rules.map((item) => [item.name, item])), [view.runtime.rules]);
  const chainIndex = useMemo(() => new Map((compile.chain ?? []).map((id, index) => [id, index])), [compile.chain]);

  const runDelay = useCallback(
    (name: string, group: boolean) => {
      setTesting((current) => new Set(current).add(name));
      delayMutation.mutate(
        { name, group, url: catalog.mihomo.defaultTestUrl, timeoutMs: 5000 },
        {
          onSuccess: (data) => {
            const values = Object.values(data.delays);
            if (!group) toast.success(values[0] ? `${name} 延迟 ${values[0]} ms` : `${name} 测试超时`);
            else toast.success(`${name}：${values.filter(Boolean).length} 个节点可用`);
          },
          onError: (error) => toast.error(error instanceof ApiError ? error.message : "测试失败"),
          onSettled: () => {
            setTesting((current) => {
              const next = new Set(current);
              next.delete(name);
              return next;
            });
            void proxies.refetch();
          }
        }
      );
    },
    [delayMutation, catalog.mihomo.defaultTestUrl, proxies]
  );

  const runSelect = useCallback(
    (group: string, member: string) =>
      select.mutate(
        { group, name: member },
        {
          onSuccess: () => {
            toast.success(member ? `${group} 已切换到 ${member}` : `${group} 已恢复自动选择`);
            void proxies.refetch();
          },
          onError: (error) => toast.error(error instanceof ApiError ? error.message : "切换失败")
        }
      ),
    [select, proxies]
  );

  const removeNode = useCallback(
    (node: WorkflowNodeEntity) => {
      if (ctx.document.canRemove(node)) node.dispose();
    },
    [ctx]
  );

  const focusNode = useCallback(
    (id: string) => {
      const node = ctx.document.getNode(id);
      if (!node) return;
      selectService.selectNode(node);
      setTab("config");
      setPanelOpen(true);
      void ctx.playground.scrollToView({ bounds: node.transform.bounds, scrollToCenter: true, zoom: Math.max(ctx.playground.config.zoom, 1) });
    },
    [ctx, selectService]
  );

  const addNodeAt = useCallback(
    (type: EgressNodeType, position: { x: number; y: number }) => {
      const spec = catalog.nodes.find((item) => item.type === type);
      if (spec?.single && ctx.document.getAllNodes().some((node) => node.flowNodeType === type)) {
        toast.error(`画布上只能有一个「${spec.label}」`);
        return undefined;
      }
      const index = ctx.document.getAllNodes().filter((node) => node.flowNodeType === type).length + 1;
      const node = ctx.document.createWorkflowNodeByType(type, position, { id: newId(type), data: defaultNodeData(type, catalog, index) });
      selectService.selectNode(node);
      setTab("config");
      setPanelOpen(true);
      return node;
    },
    [ctx, selectService, catalog]
  );

  const canvasCenter = () => {
    const rect = canvasRef.current?.getBoundingClientRect();
    const point = ctx.playground.config.getPosFromMouseEvent({
      clientX: (rect ? rect.left + rect.width / 2 : window.innerWidth / 2) + (Math.random() - 0.5) * 40,
      clientY: (rect ? rect.top + rect.height / 2 : window.innerHeight / 2) + (Math.random() - 0.5) * 40
    });
    return { x: point.x, y: point.y - 60 };
  };

  const openPortMenu = useCallback(
    (port: WorkflowPortEntity, event: React.MouseEvent) => {
      const canvas = ctx.playground.config.getPosFromMouseEvent(event.nativeEvent);
      const below = ["rules", "next"].includes(String(port.portID));
      setDropMenu({
        port,
        canvas: below ? { x: canvas.x, y: canvas.y + 60 } : { x: canvas.x + 120 + NODE_WIDTH / 2, y: canvas.y - 30 },
        client: { x: event.clientX + 16, y: event.clientY }
      });
    },
    [ctx, setDropMenu]
  );

  const connectNew = async (menu: DropMenuState, type: EgressNodeType) => {
    setDropMenu(null);
    ctx.history.startTransaction();
    const node = addNodeAt(type, { x: menu.canvas.x, y: menu.canvas.y });
    if (node) {
      await delay(30);
      ctx.document.linesManager.createLine({ from: menu.port.node.id, fromPort: menu.port.portID, to: node.id });
    }
    ctx.history.endTransaction();
  };

  /** 整理排版：规则链在最左一列竖排，路由去向按距离分列排在右侧，核心设置与没接上的节点放在左侧。 */
  const tidy = () => {
    const nodes = ctx.document.getAllNodes();
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const lines = ctx.document.linesManager.getAllLines();
    const height = (node: WorkflowNodeEntity) => Math.max(node.transform.bounds.height || 0, 120);
    const placed = new Set<string>();
    const column = (index: number) => index * (NODE_WIDTH + 140);
    ctx.history.startTransaction();
    const put = (node: WorkflowNodeEntity | undefined, x: number, y: number) => {
      if (!node || placed.has(node.id)) return;
      ctx.operation.updateNodePosition(node, { x, y });
      placed.add(node.id);
    };

    let y = 0;
    const gateway = nodes.find((node) => node.flowNodeType === "gateway");
    if (gateway) {
      put(gateway, column(0), y);
      y += height(gateway) + 70;
    }
    for (const id of compile.chain ?? []) {
      const node = byId.get(id);
      put(node, column(0), y);
      if (node) y += height(node) + 70;
    }

    // 路由去向：从规则与网关出发，沿命中 / 经由 / 成员 / 前置代理按层分列。
    const depth = new Map<string, number>();
    const queue: Array<[string, number]> = [];
    for (const line of lines) {
      const port = String(line.fromPort?.portID ?? "");
      if ((port === "hit" || port === "fallback") && line.to) queue.push([line.to.id, 1]);
    }
    while (queue.length) {
      const [id, level] = queue.shift() as [string, number];
      if ((depth.get(id) ?? 0) >= level) continue;
      if (level > 12) continue;
      depth.set(id, level);
      for (const line of lines) {
        const port = String(line.fromPort?.portID ?? "");
        if (line.from?.id === id && line.to && ["via", "members", "dialer"].includes(port)) queue.push([line.to.id, level + 1]);
      }
    }
    const columns = new Map<number, WorkflowNodeEntity[]>();
    for (const node of nodes) {
      const level = depth.get(node.id);
      if (level === undefined || placed.has(node.id)) continue;
      columns.set(level, [...(columns.get(level) ?? []), node]);
    }
    for (const [level, items] of [...columns.entries()].sort((a, b) => a[0] - b[0])) {
      let top = 0;
      for (const node of items) {
        put(node, column(level), top);
        top += height(node) + 40;
      }
    }
    let looseY = 0;
    for (const node of nodes) {
      if (placed.has(node.id)) continue;
      put(node, -column(1), looseY);
      looseY += height(node) + 40;
    }
    ctx.history.endTransaction();
    void delay(60).then(() => ctx.tools.fitView());
  };

  const overlay = useMemo<EgressCanvasOverlay>(
    () => ({
      catalog,
      issues: issueMap,
      chainIndex,
      bindings: compile.bindings ?? {},
      mihomoNames: compile.mihomoNames ?? {},
      endpoints: endpointStats,
      rules: ruleStats,
      proxies: proxies.data?.proxies,
      providers: proxies.data?.providers,
      core: status.data,
      traced: trace?.nodes ?? new Set(),
      testing,
      onPortClick: openPortMenu,
      onDuplicate: (node) => void duplicateNodes(ctx, [node], catalog),
      onDelete: removeNode,
      onDelay: runDelay,
      onSelect: runSelect
    }),
    [catalog, issueMap, chainIndex, compile, endpointStats, ruleStats, proxies.data, status.data, trace, testing, openPortMenu, ctx, removeNode, runDelay, runSelect]
  );

  const saveDraft = () => {
    const current = toGraph();
    const sent = graphSignature(current as unknown as ItemGrantGraph);
    save.mutate(
      { graph: current, revision: draftRevision },
      {
        onSuccess: (data) => {
          setSavedSignature(sent);
          setDraftRevision(data.draftRevision);
          toast.success("草稿已保存");
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "保存失败")
      }
    );
  };

  useHotkeys(
    "mod+s",
    (event) => {
      event.preventDefault();
      if (!save.isPending) saveDraft();
    },
    { enableOnFormTags: true, enableOnContentEditable: true }
  );

  const exportYaml = () => {
    exporter.mutate(
      { graph: toGraph(), includeSecrets: false },
      {
        onSuccess: (data) => {
          const blob = new Blob([data.yaml], { type: "text/yaml" });
          const link = document.createElement("a");
          link.href = URL.createObjectURL(blob);
          link.download = "aegis-mihomo.yaml";
          link.click();
          URL.revokeObjectURL(link.href);
          toast.success("已导出核心配置（密钥已打码）");
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "导出失败")
      }
    );
  };

  const empty = loaded && (graph?.nodes.length ?? 0) === 0;
  const summary = compile.summary;
  const coreStatus = status.data ?? view.core;
  const coreLabel = CORE_STATUS_LABEL[coreStatus.status] ?? CORE_STATUS_LABEL.disabled;

  return (
    <EgressOverlayContext.Provider value={overlay}>
      <style>{CANVAS_STYLE}</style>
      <div className="eg-canvas flex h-full flex-col">
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background px-3">
          <Button size="icon" variant="ghost" onClick={onClose} aria-label="返回">
            <ArrowLeft />
          </Button>
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <span className="truncate text-sm font-semibold">出海网关</span>
            {view.appliedVersion ? (
              <Badge variant={view.active ? "success" : "secondary"} size="sm">
                {view.active ? `已应用第 ${view.appliedVersion} 版` : "画布未生效"}
              </Badge>
            ) : (
              <Badge variant="secondary" size="sm">
                尚未应用
              </Badge>
            )}
            {summary.mihomoEnabled ? (
              <Badge variant={coreLabel.tone === "success" ? "success" : coreLabel.tone === "danger" ? "danger" : "secondary"} size="sm">
                mihomo {coreLabel.label}
              </Badge>
            ) : null}
            <span className="hidden text-xs text-muted-foreground lg:inline">
              {summary.activeRules} 条规则，{summary.endpoints} 条线路
              {summary.mihomoEnabled ? `，${summary.groups} 个策略组，${summary.proxies} 个节点${summary.providers ? `，${summary.providers} 个订阅` : ""}` : ""}
            </span>
            {seededFrom ? <span className="hidden text-xs text-muted-foreground xl:inline">载入自{seededFrom}</span> : null}
            {dirty ? (
              <span className="hidden items-center gap-1 text-xs text-amber-600 sm:flex dark:text-amber-400">
                <span className="size-1.5 rounded-full bg-amber-500" />
                未保存
              </span>
            ) : view.dirty ? (
              <span className="hidden text-xs text-muted-foreground sm:inline">草稿与已应用不同</span>
            ) : null}
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              className={cn(
                "gap-1.5",
                errorCount ? "text-red-600 dark:text-red-400" : warningCount ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400"
              )}
              onClick={() => {
                setTab("issues");
                setPanelOpen(true);
              }}
            >
              {validate.isPending ? <Loader2 className="animate-spin" /> : errorCount ? <CircleAlert /> : warningCount ? <AlertTriangle /> : <CircleCheck />}
              <span className="hidden sm:inline">{errorCount ? `${errorCount} 个错误` : warningCount ? `${warningCount} 个提醒` : "校验通过"}</span>
            </Button>
            <ToolbarButton label="导入 mihomo / Clash 配置或分享链接" onClick={() => setImportOpen(true)}>
              <FileUp />
            </ToolbarButton>
            <ToolbarButton label="导出核心配置 YAML" disabled={!summary.mihomoEnabled || exporter.isPending} onClick={exportYaml}>
              {exporter.isPending ? <Loader2 className="animate-spin" /> : <FileDown />}
            </ToolbarButton>
            <ToolbarButton label={panelOpen ? "收起右侧面板" : "展开右侧面板"} onClick={() => setPanelOpen(!panelOpen)}>
              <PanelRight />
            </ToolbarButton>
            <span className="mx-1 h-5 w-px bg-border" />
            <Button size="sm" variant="outline" onClick={saveDraft} disabled={save.isPending || !dirty}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              保存草稿
            </Button>
            <Button size="sm" onClick={() => setApplyOpen(true)} disabled={errorCount > 0 || !loaded}>
              <Rocket />
              应用
            </Button>
          </div>
        </header>

        <div className="relative flex min-h-0 flex-1">
          <div ref={canvasRef} className="relative min-h-0 min-w-0 flex-1 overflow-hidden" onMouseDown={() => dropMenu && setDropMenu(null)}>
            <EditorRenderer className="absolute inset-0" />

            <div className="pointer-events-none absolute top-3 left-3 z-20 flex max-h-[calc(100%-5.5rem)] flex-col">
              {paletteOpen ? (
                <NodePalette catalog={catalog} graph={graph} onCollapse={() => setPaletteOpen(false)} onAdd={(type) => addNodeAt(type, canvasCenter())} onImport={() => setImportOpen(true)} />
              ) : (
                <Button variant="outline" size="sm" className="pointer-events-auto gap-1.5 rounded-xl bg-background/95 shadow-sm" onClick={() => setPaletteOpen(true)}>
                  <PanelLeft /> 节点库
                </Button>
              )}
            </div>

            {trace ? (
              <div className="pointer-events-none absolute top-3 left-1/2 z-20 -translate-x-1/2">
                <div className="pointer-events-auto flex items-center gap-2 rounded-xl border bg-background/95 px-3 py-1.5 text-xs shadow-sm backdrop-blur">
                  <Crosshair className="size-3.5 text-emerald-500" />
                  {`${trace.explanation.host} ${ACTION_LABEL[trace.explanation.action]}`}
                  {trace.explanation.endpoint ? `，经 ${trace.explanation.chain?.join(" ← ") ?? trace.explanation.endpoint}` : ""}
                  <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setTrace(null)}>
                    清除
                  </button>
                </div>
              </div>
            ) : null}

            {empty ? (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
                <div className="rounded-2xl border border-dashed bg-background/90 px-6 py-5 text-center">
                  <div className="text-sm font-medium">从节点库拖一个「网关设置」到画布上开始，或导入一份 mihomo 配置</div>
                  <p className="mt-1 text-xs text-muted-foreground">从「规则链」端口往下拖线到空白处，就能就地接出规则</p>
                </div>
              </div>
            ) : null}

            <CanvasToolbar onTidy={tidy} />

            {dropMenu ? <QuickAddMenu catalog={catalog} menu={dropMenu} onPick={(type) => void connectNew(dropMenu, type)} onClose={() => setDropMenu(null)} /> : null}
          </div>

          <aside
            className={cn(
              "min-h-0 w-[min(420px,100%)] flex-col border-l bg-background",
              "absolute inset-y-0 right-0 z-30 shadow-xl xl:static xl:z-auto xl:shadow-none",
              panelOpen ? "flex" : "hidden"
            )}
          >
            <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
              <div className="flex items-center gap-2 border-b px-3 py-2">
                <TabsList className="flex-1">
                  <TabsTrigger value="config" className="flex-1 text-xs">
                    <Settings2 /> 配置
                  </TabsTrigger>
                  <TabsTrigger value="issues" className="flex-1 text-xs">
                    <ListChecks /> 问题
                    {issues.length ? (
                      <span className={cn("ml-0.5 rounded-full px-1.5 text-[10px] tabular-nums text-white", errorCount ? "bg-red-500" : "bg-amber-500")}>{issues.length}</span>
                    ) : null}
                  </TabsTrigger>
                  <TabsTrigger value="trace" className="flex-1 text-xs">
                    <Crosshair /> 追踪
                  </TabsTrigger>
                  <TabsTrigger value="preview" className="flex-1 text-xs">
                    <FileCode2 /> 预览
                  </TabsTrigger>
                </TabsList>
                <Button size="icon" variant="ghost" className="size-7 xl:hidden" onClick={() => setPanelOpen(false)} aria-label="收起">
                  <X />
                </Button>
              </div>
              <TabsContent value="config" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                {selectedNode ? (
                  <EgressInspector
                    key={selectedNode.id}
                    node={selectedNode}
                    issues={issueMap.get(selectedNode.id) ?? []}
                    context={{
                      catalog,
                      mihomoTargets,
                      revision,
                      endpointStat: compile.bindings?.[selectedNode.id] ? endpointStats.get(compile.bindings[selectedNode.id]) : undefined,
                      ruleStat: selectedNode.flowNodeType === "rule" ? ruleStats.get(String(readNodeValues(selectedNode).name ?? "")) : undefined,
                      onDelay: coreStatus.status === "running" ? runDelay : undefined
                    }}
                    onDuplicate={() => void duplicateNodes(ctx, [selectedNode], catalog)}
                    onDelete={() => removeNode(selectedNode)}
                  />
                ) : (
                  <Overview view={view} compile={compile} graph={graph} onFocus={focusNode} />
                )}
              </TabsContent>
              <TabsContent value="issues" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                <IssueList issues={issues} graph={graph} onFocus={focusNode} />
              </TabsContent>
              <TabsContent value="trace" className="min-h-0 flex-1 overflow-y-auto px-4 py-4" forceMount hidden={tab !== "trace"}>
                <TracePanel
                  compile={compile}
                  dirty={dirty || view.dirty || !view.active}
                  proxies={proxies.data?.proxies}
                  graph={graph}
                  onTrace={setTrace}
                  onFocus={focusNode}
                />
              </TabsContent>
              <TabsContent value="preview" className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
                <PreviewPanel preview={preview} />
              </TabsContent>
            </Tabs>
          </aside>
        </div>
      </div>

      <ApplyDialog
        open={applyOpen}
        onOpenChange={setApplyOpen}
        graph={toGraph}
        revision={draftRevision}
        warnings={warningCount}
        mihomo={summary.mihomoEnabled}
        coreMissing={summary.mihomoEnabled && !coreStatus.binaryPresent}
        onApplied={(data) => {
          setSavedSignature(signature);
          setDraftRevision(data.draftRevision);
          setCompile(data.compile);
        }}
      />
      <EgressImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        graph={toGraph}
        onImported={(next, from) => onReplace(next, from)}
      />
    </EgressOverlayContext.Provider>
  );
}

const CANVAS_STYLE = `
.eg-canvas {
  --g-editor-background: color-mix(in oklab, var(--muted) 60%, var(--background));
  --g-selection-background: ${ACCENT};
  --g-playground-select: ${ACCENT};
  --g-playground-hover: ${ACCENT};
  --g-playground-line: ${ACCENT};
  --g-playground-selectBox-outline: ${ACCENT};
  --g-playground-selectBox-background: rgba(99, 102, 241, 0.08);
  --g-playground-select-hover-background: rgba(99, 102, 241, 0.08);
  --g-workflow-port-color-background: var(--card);
  --eg-dot: color-mix(in oklab, var(--foreground) 18%, transparent);
}
${Object.entries(PORT_COLOR)
  .map(([port, color]) => `.eg-canvas .eg-line-${port} { --eg-line: ${color}; }`)
  .join("\n")}
.eg-canvas .eg-line-error { --eg-line: #ef4444; }
.eg-canvas .eg-line-dim { opacity: 0.25; }
`;

function ToolbarButton({ label, disabled, active, onClick, children }: { label: string; disabled?: boolean; active?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="ghost" className={cn("size-8 rounded-lg", active && "bg-accent text-foreground")} disabled={disabled} onClick={onClick} aria-label={label}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function CanvasToolbar({ onTidy }: { onTidy: () => void }) {
  const ctx = useClientContext();
  const tools = usePlaygroundTools();
  const minimap = useService(FlowMinimapService);
  const { resolvedTheme } = useTheme();
  const [canUndo, setCanUndo] = useState(false);
  const [canRedo, setCanRedo] = useState(false);
  const [showMinimap, setShowMinimap] = useState(true);

  useEffect(() => {
    const disposable = ctx.history.undoRedoService.onChange(() => {
      setCanUndo(ctx.history.canUndo());
      setCanRedo(ctx.history.canRedo());
    });
    return () => disposable.dispose();
  }, [ctx]);

  useEffect(() => applyMinimapTheme(minimap, resolvedTheme === "dark"), [minimap, resolvedTheme]);

  const curved = tools.lineType === LineType.BEZIER;

  return (
    <>
      <div className="absolute bottom-4 left-1/2 z-20 flex -translate-x-1/2 items-center gap-0.5 rounded-2xl border bg-background/95 p-1 shadow-lg backdrop-blur">
        <ToolbarButton label="撤销（⌘Z）" disabled={!canUndo} onClick={() => ctx.history.undo()}>
          <Undo2 />
        </ToolbarButton>
        <ToolbarButton label="重做（⌘⇧Z）" disabled={!canRedo} onClick={() => ctx.history.redo()}>
          <Redo2 />
        </ToolbarButton>
        <span className="mx-1 h-5 w-px bg-border" />
        <ToolbarButton label="缩小" onClick={() => tools.zoomout()}>
          <Minus />
        </ToolbarButton>
        <button type="button" className="h-8 w-12 rounded-lg text-xs tabular-nums text-muted-foreground hover:bg-accent hover:text-foreground" onClick={() => tools.fitView()} title="适应画布">
          {Math.round(tools.zoom * 100)}%
        </button>
        <ToolbarButton label="放大" onClick={() => tools.zoomin()}>
          <Plus />
        </ToolbarButton>
        <ToolbarButton label="适应画布" onClick={() => tools.fitView()}>
          <Maximize />
        </ToolbarButton>
        <span className="mx-1 h-5 w-px bg-border" />
        <ToolbarButton label="整理排版（规则链在左，路由去向按层分列）" onClick={onTidy}>
          <Rows3 />
        </ToolbarButton>
        <ToolbarButton label={curved ? "改为折线" : "改为曲线"} onClick={() => tools.switchLineType(curved ? LineType.LINE_CHART : LineType.BEZIER)}>
          {curved ? <Spline /> : <Waypoints />}
        </ToolbarButton>
        <ToolbarButton label={showMinimap ? "隐藏小地图" : "显示小地图"} active={showMinimap} onClick={() => setShowMinimap(!showMinimap)}>
          <MapIcon />
        </ToolbarButton>
      </div>
      <div className={cn("absolute right-4 bottom-4 z-20 overflow-hidden rounded-2xl border bg-card shadow-lg", !showMinimap && "hidden")}>
        <MinimapRender
          containerStyles={{ pointerEvents: "auto", position: "relative", top: "unset", right: "unset", bottom: "unset", left: "unset" }}
          inactiveStyle={{ opacity: 1, scale: 1, translateX: 0, translateY: 0 }}
        />
      </div>
    </>
  );
}

function NodePalette({
  catalog,
  graph,
  onAdd,
  onCollapse,
  onImport
}: {
  catalog: EgressGraphCatalog;
  graph: EgressGraph | null;
  onAdd: (type: EgressNodeType) => void;
  onCollapse: () => void;
  onImport: () => void;
}) {
  const dragService = useService(WorkflowDragService);
  const pressed = useRef<{ type: EgressNodeType; x: number; y: number; dragging: boolean } | null>(null);
  const present = new Set((graph?.nodes ?? []).map((node) => node.type));

  return (
    <div className="pointer-events-auto flex min-h-0 w-60 flex-col overflow-hidden rounded-2xl border bg-background/95 shadow-lg backdrop-blur">
      <div className="flex items-center gap-2 px-3 pt-3 pb-2">
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold">节点库</div>
          <p className="text-[11px] leading-4 text-muted-foreground">拖到画布上，或点击添加</p>
        </div>
        <Button size="icon" variant="ghost" className="size-7" onClick={onCollapse} aria-label="收起节点库">
          <PanelLeft />
        </Button>
      </div>
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-3 pb-3">
        {catalog.categories.map((category) => {
          const specs = catalog.nodes.filter((spec) => spec.category === category.key);
          if (!specs.length) return null;
          return (
            <div key={category.key} className="space-y-1">
              <div className="px-1 text-[11px] font-medium text-muted-foreground">{category.label}</div>
              {specs.map((spec) => {
                const visual = NODE_VISUAL[spec.type];
                const disabled = Boolean(spec.single && present.has(spec.type));
                return (
                  <Tooltip key={spec.type}>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        disabled={disabled}
                        onMouseDown={(event) => {
                          if (disabled || event.button !== 0) return;
                          pressed.current = { type: spec.type, x: event.clientX, y: event.clientY, dragging: false };
                        }}
                        onMouseMove={(event) => {
                          const state = pressed.current;
                          if (!state || state.dragging || state.type !== spec.type) return;
                          if (Math.abs(event.clientX - state.x) + Math.abs(event.clientY - state.y) < 5) return;
                          state.dragging = true;
                          const index = (graph?.nodes ?? []).filter((node) => node.type === spec.type).length + 1;
                          void dragService.startDragCard(spec.type, event, { id: newId(spec.type), data: defaultNodeData(spec.type, catalog, index) });
                        }}
                        onMouseLeave={() => {
                          if (pressed.current && !pressed.current.dragging) pressed.current = null;
                        }}
                        onClick={() => {
                          const state = pressed.current;
                          pressed.current = null;
                          if (state?.dragging) return;
                          onAdd(spec.type);
                        }}
                        className={cn(
                          "group flex w-full cursor-grab items-center gap-2.5 rounded-xl border border-transparent px-2 py-1.5 text-left transition-colors hover:border-border hover:bg-accent/50 active:cursor-grabbing",
                          disabled && "cursor-not-allowed opacity-45 hover:border-transparent hover:bg-transparent"
                        )}
                      >
                        <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
                        <div className="min-w-0">
                          <div className="truncate text-xs font-medium">{spec.label}</div>
                          <div className="truncate text-[11px] text-muted-foreground">
                            {disabled ? "画布上已有" : spec.outputs?.length ? spec.outputs.map((output) => output.label).join(" / ") : "设置节点"}
                          </div>
                        </div>
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="right" className="max-w-64">
                      {spec.hint}
                    </TooltipContent>
                  </Tooltip>
                );
              })}
            </div>
          );
        })}
        <button
          type="button"
          onClick={onImport}
          className="flex w-full items-center gap-2.5 rounded-xl border border-dashed px-2 py-2 text-left transition-colors hover:bg-accent/50"
        >
          <ToneIcon icon={FileUp} tone="primary" size="sm" />
          <div className="min-w-0">
            <div className="truncate text-xs font-medium">导入配置</div>
            <div className="truncate text-[11px] text-muted-foreground">Clash / mihomo YAML、分享链接、订阅</div>
          </div>
        </button>
        <div className="rounded-xl border border-dashed p-2.5 text-[11px] leading-5 text-muted-foreground">
          <div className="font-medium text-foreground">怎么搭</div>
          <ol className="mt-1 list-decimal space-y-0.5 pl-4">
            <li>网关设置的「规则链」往下连第一条规则，规则之间用「下一条」串起来</li>
            <li>规则的「命中」连到线路、策略组、节点或 mihomo 分流</li>
            <li>策略组的「成员」连到节点、订阅或其它策略组</li>
          </ol>
          <div className="mt-2 font-medium text-foreground">快捷键</div>
          <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-2">
            {[
              ["⌘S", "保存草稿"],
              ["⌘Z", "撤销"],
              ["⌘D", "复制"],
              ["Delete", "删除"],
              ["Shift 拖动", "框选"]
            ].map(([key, label]) => (
              <div key={key} className="contents">
                <dt className="font-mono">{key}</dt>
                <dd>{label}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </div>
  );
}

function QuickAddMenu({ catalog, menu, onPick, onClose }: { catalog: EgressGraphCatalog; menu: DropMenuState; onPick: (type: EgressNodeType) => void; onClose: () => void }) {
  useHotkeys("escape", onClose, { enableOnFormTags: true });
  const width = 260;
  const left = Math.max(8, Math.min(menu.client.x + 8, window.innerWidth - width - 8));
  const top = Math.max(64, Math.min(menu.client.y - 24, window.innerHeight - 320));
  const port = portSpec(catalog, String(menu.port.node.flowNodeType), String(menu.port.portID ?? ""));
  const specs = catalog.nodes.filter((spec) => (port?.accepts ?? []).includes(spec.type));
  return (
    <div className="fixed z-40 max-h-[360px] overflow-y-auto rounded-2xl border bg-popover p-1.5 text-popover-foreground shadow-xl" style={{ left, top, width }} onMouseDown={(event) => event.stopPropagation()}>
      <div className="px-2 pt-1 pb-1.5 text-[11px] text-muted-foreground">从「{port?.label}」接一个新节点</div>
      {specs.map((spec) => {
        const visual = NODE_VISUAL[spec.type];
        return (
          <button key={spec.type} type="button" onClick={() => onPick(spec.type)} className="flex w-full items-center gap-2.5 rounded-xl px-2 py-1.5 text-left transition-colors hover:bg-accent">
            <ToneIcon icon={visual.icon} tone={visual.tone} size="sm" />
            <div className="min-w-0">
              <div className="truncate text-xs font-medium">{spec.label}</div>
              <div className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{spec.hint}</div>
            </div>
          </button>
        );
      })}
    </div>
  );
}

function Overview({ view, compile, graph, onFocus }: { view: EgressGraphView; compile: EgressCompileView; graph: EgressGraph | null; onFocus: (id: string) => void }) {
  const summary = compile.summary;
  const runtime = view.runtime;
  const gateway = graph?.nodes.find((node) => node.type === "gateway");
  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3">
        <ToneIcon icon={NODE_VISUAL.gateway.icon} tone="primary" size="lg" />
        <div>
          <div className="text-sm font-semibold">出海网关</div>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">平台所有出网调用按这张图决定走直连、代理还是拒绝。选中节点可以编辑它的配置。</p>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-2">
        {[
          ["规则", `${summary.activeRules} / ${summary.rules}`],
          ["网关线路", String(summary.endpoints)],
          ["策略组", String(summary.groups)],
          ["mihomo 节点", String(summary.proxies)],
          ["订阅", String(summary.providers)],
          ["分流规则", String(summary.mihomoRules)]
        ].map(([label, value]) => (
          <div key={label} className="rounded-xl border px-3 py-2">
            <div className="text-[11px] text-muted-foreground">{label}</div>
            <div className="text-base font-semibold tabular-nums">{value}</div>
          </div>
        ))}
      </div>
      <section className="space-y-2">
        <h4 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">本进程运行态</h4>
        <div className="grid grid-cols-4 gap-2 text-center">
          {[
            ["判定", runtime.routed],
            ["走代理", runtime.proxied],
            ["直连", runtime.direct],
            ["拒绝", runtime.rejected]
          ].map(([label, value]) => (
            <div key={label as string} className="rounded-xl bg-muted/60 px-1 py-2">
              <div className="text-[11px] text-muted-foreground">{label}</div>
              <div className="text-sm font-semibold tabular-nums">{Number(value).toLocaleString()}</div>
            </div>
          ))}
        </div>
        {runtime.failed ? <p className="text-[11px] text-red-600 dark:text-red-400">另有 {runtime.failed.toLocaleString()} 次选不出可用线路</p> : null}
      </section>
      <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
        <dt className="text-muted-foreground">已应用</dt>
        <dd>{view.appliedVersion ? `第 ${view.appliedVersion} 版${view.appliedAt ? `，${formatDateTime(view.appliedAt)}` : ""}` : "尚未应用"}</dd>
        <dt className="text-muted-foreground">草稿</dt>
        <dd>{view.draftUpdatedAt ? formatDateTime(view.draftUpdatedAt) : view.origin === "config" ? "由当前配置生成" : "—"}</dd>
        <dt className="text-muted-foreground">配置来源</dt>
        <dd>{runtime.source === "graph" ? "画布" : runtime.source === "database" ? "旧版控制台" : runtime.source || "环境变量"}</dd>
      </dl>
      {gateway ? (
        <Button size="sm" variant="outline" className="w-full" onClick={() => onFocus(gateway.id)}>
          打开网关设置
        </Button>
      ) : null}
      <p className="rounded-lg bg-muted/60 px-3 py-2 text-xs leading-5 text-muted-foreground">
        保存草稿不影响线上；应用时先让 mihomo 核心接受新配置，再热替换网关路由表，任何一步失败都停在旧配置上。应用记录可在出海代理页的「版本」里回滚。
      </p>
    </div>
  );
}

function IssueList({ issues, graph, onFocus }: { issues: EgressIssue[]; graph: EgressGraph | null; onFocus: (id: string) => void }) {
  if (!issues.length) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-center text-sm text-muted-foreground">
        <CircleCheck className="size-8 text-emerald-500" />
        没有发现问题
      </div>
    );
  }
  const byId = new Map((graph?.nodes ?? []).map((node) => [node.id, node]));
  return (
    <div className="space-y-1.5">
      {issues.map((issue, index) => {
        const node = issue.nodeId ? byId.get(issue.nodeId) : undefined;
        return (
          <button
            key={index}
            type="button"
            disabled={!node}
            onClick={() => node && onFocus(node.id)}
            className={cn(
              "flex w-full items-start gap-2 rounded-xl border px-3 py-2 text-left text-xs leading-5 transition-colors",
              node && "hover:bg-accent/50",
              issue.level === "error" ? "border-red-500/30" : "border-amber-500/30"
            )}
          >
            {issue.level === "error" ? <CircleAlert className="mt-0.5 size-3.5 shrink-0 text-red-500" /> : <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />}
            <span className="min-w-0 flex-1">
              {node ? <span className="font-medium">{nodeTitle(node.type, node.data)}：</span> : null}
              {issue.message}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/** 追踪：问网关「这个目标会怎么出去」，把规则 → 线路 → 策略组当前成员这条路径在画布上点亮。 */
function TracePanel({
  compile,
  dirty,
  proxies,
  graph,
  onTrace,
  onFocus
}: {
  compile: EgressCompileView;
  dirty: boolean;
  proxies?: Record<string, { now?: string }>;
  graph: EgressGraph | null;
  onTrace: (trace: { explanation: EgressExplanation; nodes: Set<string>; edges: Set<string> } | null) => void;
  onFocus: (id: string) => void;
}) {
  const explain = useEgressExplainMutation();
  const [host, setHost] = useState("api.stripe.com");
  const [port, setPort] = useState("443");
  const [profile, setProfile] = useState("");
  const [network, setNetwork] = useState<"tcp" | "udp">("tcp");
  const [result, setResult] = useState<EgressExplanation | null>(null);

  const run = () => {
    explain.mutate(
      { host: host.trim(), port: Number(port) || 443, profile: profile.trim() || undefined, network },
      {
        onSuccess: (data) => {
          setResult(data);
          const nodes = new Set<string>();
          const edges = new Set<string>();
          const endpointNode = new Map(Object.entries(compile.bindings ?? {}).map(([node, endpoint]) => [endpoint, node]));
          const nameNode = new Map(Object.entries(compile.mihomoNames ?? {}).map(([node, name]) => [name, node]));
          const gateway = graph?.nodes.find((node) => node.type === "gateway")?.id;
          const ruleNode = data.rule === "default" ? undefined : compile.ruleNodes?.[data.rule];
          if (gateway) nodes.add(gateway);
          // 规则链上，命中那条之前的规则都判定过一遍；未命中则整条链都走完
          let previous = gateway;
          let previousPort = "rules";
          for (const id of compile.chain ?? []) {
            nodes.add(id);
            if (previous) edges.add(edgeId(previous, previousPort, id));
            previous = id;
            previousPort = "next";
            if (id === ruleNode) break;
          }
          const hitPort = data.rule === "default" ? "fallback" : "hit";
          const chain = data.chain ?? (data.endpoint ? [data.endpoint] : []);
          let from = data.rule === "default" ? gateway : ruleNode;
          let fromPort = hitPort;
          for (const endpoint of chain) {
            const id = endpointNode.get(endpoint);
            if (!id) continue;
            nodes.add(id);
            if (from) edges.add(edgeId(from, fromPort, id));
            from = id;
            fromPort = "via";
          }
          // 进了 mihomo：沿策略组当前选择往下走
          let current = data.endpoint ? endpointNode.get(data.endpoint) : undefined;
          for (let depth = 0; current && depth < 8; depth++) {
            const name = compile.mihomoNames?.[current];
            const now = name ? proxies?.[name]?.now : undefined;
            const next = now ? nameNode.get(now) : undefined;
            if (!next) break;
            nodes.add(next);
            edges.add(edgeId(current, "members", next));
            current = next;
          }
          onTrace({ explanation: data, nodes, edges });
        },
        onError: (error) => toast.error(error instanceof ApiError ? error.message : "追踪失败")
      }
    );
  };

  return (
    <div className="space-y-4">
      <p className="text-xs leading-5 text-muted-foreground">输入一个目标，看网关会用哪条规则、走哪条线路，路径会在画布上点亮。</p>
      {dirty ? <p className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs leading-5 text-amber-700 dark:text-amber-300">追踪按线上生效的配置判定；画布还没应用的改动不参与。</p> : null}
      <Field label="目标域名或 IP">
        <Input className="h-8 font-mono text-xs" value={host} onChange={(event) => setHost(event.target.value)} onKeyDown={(event) => event.key === "Enter" && run()} />
      </Field>
      <div className="grid w-40 grid-cols-2 gap-1 rounded-lg bg-muted/60 p-0.5">
        {(["tcp", "udp"] as const).map((item) => (
          <button
            key={item}
            type="button"
            onClick={() => setNetwork(item)}
            className={cn("rounded-md py-1 font-mono text-[11px] transition-colors", network === item ? "bg-background font-semibold shadow-sm" : "text-muted-foreground hover:text-foreground")}
          >
            {item.toUpperCase()}
          </button>
        ))}
      </div>
      <div className="grid grid-cols-[96px_1fr] gap-3">
        <Field label="端口">
          <Input className="h-8 font-mono text-xs" value={port} onChange={(event) => setPort(event.target.value)} />
        </Field>
        <Field label="调用方（可选）">
          <Input className="h-8 font-mono text-xs" placeholder="payment.stripe" value={profile} onChange={(event) => setProfile(event.target.value)} />
        </Field>
      </div>
      <div className="flex gap-2">
        <Button size="sm" className="flex-1" onClick={run} disabled={!host.trim() || explain.isPending}>
          {explain.isPending ? <Loader2 className="animate-spin" /> : <Search />}
          追踪
        </Button>
        {result ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setResult(null);
              onTrace(null);
            }}
          >
            清除
          </Button>
        ) : null}
      </div>
      {result ? (
        <div className="space-y-2 rounded-xl border p-3 text-xs">
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">结果</span>
            <Badge variant={result.action === "proxy" ? "success" : result.action === "reject" ? "danger" : "info"} size="sm">
              {ACTION_LABEL[result.action]}
            </Badge>
          </div>
          <div className="flex justify-between gap-2">
            <span className="text-muted-foreground">规则</span>
            <button type="button" className="truncate font-medium hover:underline" onClick={() => compile.ruleNodes?.[result.rule] && onFocus(compile.ruleNodes[result.rule])}>
              {result.rule === "default" ? "未命中（兜底）" : result.rule}
            </button>
          </div>
          {result.reason ? (
            <div className="flex justify-between gap-2">
              <span className="text-muted-foreground">原因</span>
              <span className="truncate font-mono">{result.reason}</span>
            </div>
          ) : null}
          {result.chain?.length ? (
            <div className="flex justify-between gap-2">
              <span className="text-muted-foreground">线路</span>
              <span className="truncate font-mono">{result.chain.join(" ← ")}</span>
            </div>
          ) : null}
          {result.error ? <div className="text-red-600 dark:text-red-400">{result.error}</div> : null}
          <div className="border-t pt-2">
            <div className="mb-1 text-muted-foreground">逐条判定</div>
            <div className="space-y-0.5">
              {result.evaluated.map((item) => (
                <div key={item.rule} className="flex items-center justify-between gap-2">
                  <span className={cn("truncate", item.matched ? "font-medium text-emerald-600 dark:text-emerald-400" : "text-muted-foreground")}>{item.rule}</span>
                  <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{item.matched ? item.reason || "命中" : "未命中"}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function PreviewPanel({ preview }: { preview: EgressValidateResult | null }) {
  if (!preview) return <div className="py-10 text-center text-sm text-muted-foreground">编译中…</div>;
  const routes = preview.routes;
  return (
    <Tabs defaultValue="routes" className="gap-3">
      <TabsList className="w-full">
        <TabsTrigger value="routes" className="flex-1 text-xs">
          网关路由表
        </TabsTrigger>
        <TabsTrigger value="yaml" className="flex-1 text-xs" disabled={!preview.yaml}>
          核心配置
        </TabsTrigger>
      </TabsList>
      <TabsContent value="routes" className="space-y-3">
        <div className="space-y-1">
          {(routes.rules ?? []).map((rule, index) => (
            <div key={rule.name} className="rounded-lg border px-2.5 py-1.5 text-xs">
              <div className="flex items-center justify-between gap-2">
                <span className="truncate font-medium">
                  <span className="mr-1 tabular-nums text-muted-foreground">#{index + 1}</span>
                  {rule.name}
                  {rule.enabled === false ? <span className="ml-1 text-muted-foreground">（停用）</span> : null}
                </span>
                <span className="shrink-0 text-muted-foreground">{ACTION_LABEL[rule.action ?? "proxy"]}</span>
              </div>
              {rule.endpoints?.length ? <div className="truncate font-mono text-[11px] text-muted-foreground">{rule.endpoints.join("  ")}</div> : null}
            </div>
          ))}
          <div className="rounded-lg border border-dashed px-2.5 py-1.5 text-xs">
            <span className="font-medium">未命中</span>
            <span className="ml-2 text-muted-foreground">{ACTION_LABEL[routes.defaultAction]}</span>
            {routes.defaultEndpoints?.length ? <div className="truncate font-mono text-[11px] text-muted-foreground">{routes.defaultEndpoints.join("  ")}</div> : null}
          </div>
        </div>
        <div className="space-y-1">
          <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">线路</div>
          {(routes.endpoints ?? []).map((endpoint) => (
            <div key={endpoint.name} className="flex items-center justify-between gap-2 text-xs">
              <span className="truncate font-mono">{endpoint.name}</span>
              <span className="shrink-0 truncate text-muted-foreground">
                {endpoint.protocol === "mihomo" ? `mihomo → ${endpoint.target || "分流"}` : `${endpoint.protocol} ${endpoint.address ?? ""}`}
                {endpoint.via ? ` 经 ${endpoint.via}` : ""}
              </span>
            </div>
          ))}
        </div>
      </TabsContent>
      <TabsContent value="yaml">
        <pre className="max-h-[calc(100dvh-12rem)] overflow-auto rounded-xl border bg-muted/40 p-3 font-mono text-[11px] leading-5">{preview.yaml}</pre>
        <p className="mt-1.5 text-[11px] leading-4 text-muted-foreground">密钥已打码；控制器、入站与 IN-USER 分发规则由网关在运行时注入。</p>
      </TabsContent>
    </Tabs>
  );
}

function ApplyDialog({
  open,
  onOpenChange,
  graph,
  revision,
  warnings,
  mihomo,
  coreMissing,
  onApplied
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  graph: () => EgressGraph;
  revision: number;
  warnings: number;
  mihomo: boolean;
  coreMissing: boolean;
  onApplied: (view: EgressGraphView) => void;
}) {
  const apply = useApplyEgressGraphMutation();
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState(false);
  return (
    <Dialog open={open} onOpenChange={(next) => !apply.isPending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>应用到线上</DialogTitle>
          <DialogDescription>平台所有出站立刻按这张图路由；会同时保存为草稿并记一个版本，可随时回滚。</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          {warnings ? <p className="rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">还有 {warnings} 个提醒，不影响应用。</p> : null}
          {mihomo ? (
            <p className="rounded-lg bg-muted/60 px-3 py-2 text-xs leading-5 text-muted-foreground">
              {coreMissing ? "本机还没有 mihomo 核心，应用时会先从 GitHub 发布页下载并校验，可能要几十秒。" : "mihomo 核心会先校验并热重载这份配置，核心拒绝就不会改动网关。"}
            </p>
          ) : null}
          <div className="space-y-1.5">
            <Label className="text-xs">版本备注</Label>
            <Textarea rows={2} className="min-h-0 text-xs" placeholder="改了什么（可选）" value={note} onChange={(event) => setNote(event.target.value)} />
          </div>
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={confirm} onCheckedChange={setConfirm} />
            我已确认路由变化，会影响支付、登录、存储等全部出站
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={apply.isPending}>
            取消
          </Button>
          <Button
            disabled={!confirm || apply.isPending}
            onClick={() =>
              apply.mutate(
                { graph: graph(), revision, note: note.trim() || undefined },
                {
                  onSuccess: (data) => {
                    toast.success(`已应用第 ${data.appliedVersion} 版`);
                    onApplied(data);
                    setNote("");
                    setConfirm(false);
                    onOpenChange(false);
                  },
                  onError: (error) => toast.error(error instanceof ApiError ? error.message : "应用失败", { duration: 10_000 })
                }
              )
            }
          >
            {apply.isPending ? <Loader2 className="animate-spin" /> : <Rocket />}
            {apply.isPending ? (coreMissing ? "正在安装核心并应用" : "正在应用") : "应用"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
