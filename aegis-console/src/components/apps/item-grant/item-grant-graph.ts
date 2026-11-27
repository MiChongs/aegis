import type { ItemGrantCatalog, ItemGrantGraph, ItemGrantItem, ItemGrantNodeType } from "@/lib/api/item-grant";

/**
 * 画布模型：后端的画布（nodes / edges，节点位置是左上角）与 Flowgram 文档（节点位置是顶边中点、
 * 连线是 sourceNodeID / sourcePortID / targetNodeID）之间的往返，新节点的默认配置，连线合法性（不成环）。
 *
 * Flowgram 节点的 `data` 就是后端节点的配置本身；运行期的叠加信息（问题、预演命中、追踪路径）
 * 不放进 data，而是走编辑器上下文，否则撤销历史里会混进这些与配置无关的变化。
 */

export type FlowNodeData = Record<string, unknown>;

/** Flowgram 文档的节点与连线（只取这里用到的字段）。 */
export type CanvasNodeJSON = {
  id: string;
  type: string | number;
  meta?: { position?: { x: number; y: number } };
  data?: FlowNodeData;
};

export type CanvasEdgeJSON = {
  sourceNodeID: string;
  targetNodeID: string;
  sourcePortID?: string | number;
  targetPortID?: string | number;
};

export type CanvasJSON = { nodes: CanvasNodeJSON[]; edges: CanvasEdgeJSON[] };

/** 节点卡片宽度：位置换算（左上角 ↔ 顶边中点）与排版都按它。 */
export const NODE_WIDTH = 280;

/** 没写端口的旧连线按目录里的第一个输出端口处理（与后端 defaultHandle 一致）。 */
function defaultPort(type: string, catalog?: ItemGrantCatalog) {
  return catalog?.nodes.find((spec) => spec.type === type)?.outputs[0]?.id ?? "next";
}

export function toCanvas(graph: ItemGrantGraph | null | undefined, catalog?: ItemGrantCatalog): CanvasJSON {
  const nodes = graph?.nodes ?? [];
  const typeOf = new Map(nodes.map((node) => [node.id, node.type]));
  return {
    nodes: nodes.map((node) => ({
      id: node.id,
      type: node.type,
      meta: { position: { x: (node.position?.x ?? 0) + NODE_WIDTH / 2, y: node.position?.y ?? 0 } },
      data: { ...(node.data ?? {}) }
    })),
    edges: (graph?.edges ?? [])
      .filter((edge) => typeOf.has(edge.source) && typeOf.has(edge.target))
      .map((edge) => ({
        sourceNodeID: edge.source,
        sourcePortID: edge.sourceHandle || defaultPort(typeOf.get(edge.source) as string, catalog),
        targetNodeID: edge.target
      }))
  };
}

/** 连线标识由端点与端口拼成：同一端口到同一节点只会有一条线，校验问题按它定位回画布上的线。 */
export function edgeId(source: string, port: string, target: string) {
  return `${source}:${port}->${target}`;
}

export function fromCanvas(json: CanvasJSON): ItemGrantGraph {
  return {
    nodes: json.nodes.map((node) => ({
      id: node.id,
      type: node.type as ItemGrantNodeType,
      position: {
        x: Math.round((node.meta?.position?.x ?? 0) - NODE_WIDTH / 2),
        y: Math.round(node.meta?.position?.y ?? 0)
      },
      data: { ...(node.data ?? {}) }
    })),
    edges: json.edges
      .filter((edge) => edge.sourceNodeID && edge.targetNodeID)
      .map((edge) => {
        const port = edge.sourcePortID === undefined || edge.sourcePortID === "" ? "" : String(edge.sourcePortID);
        return {
          id: edgeId(edge.sourceNodeID, port, edge.targetNodeID),
          source: edge.sourceNodeID,
          sourceHandle: port || undefined,
          target: edge.targetNodeID
        };
      })
  };
}

/** 只比较配置与结构（与顺序无关）：「未保存」提示与预演失效都用它。 */
export function graphSignature(graph: ItemGrantGraph) {
  const nodes = graph.nodes
    .map((node) => [node.id, node.type, node.position.x, node.position.y, node.data] as const)
    .sort((a, b) => a[0].localeCompare(b[0]));
  const edges = graph.edges.map((edge) => edge.id).sort();
  return JSON.stringify([nodes, edges]);
}

let counter = 0;

/** 新节点 / 分支 / 选项的标识：类型前缀 + 时间 + 计数，同一画布内足够唯一。 */
export function newId(prefix: string) {
  counter = (counter + 1) % 1_000_000;
  return `${prefix}-${Date.now().toString(36)}${counter.toString(36)}`;
}

export function shortId() {
  counter = (counter + 1) % 1_000_000;
  return `${Math.random().toString(36).slice(2, 6)}${counter.toString(36)}`;
}

export function defaultItem(type: string, catalog?: ItemGrantCatalog): ItemGrantItem {
  const spec = catalog?.items.find((item) => item.type === type);
  switch (spec?.value) {
    case "money":
      return { type, money: "1.00" };
    case "ref":
      return { type, refId: catalog?.plans.find((plan) => plan.active)?.id ?? catalog?.plans[0]?.id ?? 0 };
    case "feature":
      return { type, tag: catalog?.features.find((feature) => feature.isActive)?.tag ?? "", amount: 7 };
    case "feature_minutes":
      return { type, tag: catalog?.features.find((feature) => feature.isActive)?.tag ?? "", amount: 60 };
    case "text":
      return { type, text: "" };
    case "number_ticket":
      return { type, amount: 1, refId: 1 };
    case "cosmetic":
      return { type, refId: 0, amount: 0 };
    case "coupon":
      return { type, refId: 0, amount: 1 };
    case "":
      return { type };
    default: {
      const presets: Record<string, number> = { points: 100, experience: 100, vip_days: 7, ad_free: 3, lottery_draws: 3, ai_credit: 10, number_change: 1, number_draw: 1 };
      return { type, amount: presets[type] ?? spec?.min ?? 1 };
    }
  }
}

export function defaultNodeData(type: ItemGrantNodeType, catalog?: ItemGrantCatalog): FlowNodeData {
  switch (type) {
    case "trigger":
      return { kind: "manual" };
    case "audience":
      return { scope: "all" };
    case "condition":
      return { mode: "all", rules: [{ field: "user.level", op: "gte", value: [3] }] };
    case "window":
      return { weekdays: [], startMinute: 0, endMinute: 0 };
    case "split":
      return {
        sticky: true,
        branches: [
          { id: shortId(), label: "A 组", weight: 50 },
          { id: shortId(), label: "B 组", weight: 50 }
        ]
      };
    case "limit":
      return { period: "day", perUser: 1 };
    case "grant":
      return { items: [defaultItem("points", catalog)] };
    case "random":
      return {
        picks: 1,
        options: [
          { id: shortId(), label: "积分", weight: 70, items: [defaultItem("points", catalog)] },
          { id: shortId(), label: "抽奖次数", weight: 30, items: [defaultItem("lottery_draws", catalog)] }
        ]
      };
    case "notify":
      return { title: "奖励已到账", content: "{items}" };
    case "end":
      return {};
  }
}

/** 从 target 出发沿连线能不能走回 source（连上这条线会不会成环）。 */
export function wouldCycle(edges: Array<{ source: string; target: string }>, source: string, target: string) {
  if (source === target) return true;
  const next = new Map<string, string[]>();
  for (const edge of edges) next.set(edge.source, [...(next.get(edge.source) ?? []), edge.target]);
  const stack = [target];
  const seen = new Set<string>();
  while (stack.length) {
    const id = stack.pop() as string;
    if (id === source) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    stack.push(...(next.get(id) ?? []));
  }
  return false;
}

export function cloneGraph(graph: ItemGrantGraph): ItemGrantGraph {
  return JSON.parse(JSON.stringify(graph)) as ItemGrantGraph;
}
