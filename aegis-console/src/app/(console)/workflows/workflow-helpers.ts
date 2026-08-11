import type {
  WorkflowDefinition,
  WorkflowDetail,
  WorkflowNode,
  WorkflowNodeType
} from "@/lib/api-client";

export type WorkflowDraft = {
  name: string;
  description: string;
  category: string;
  status: string;
  definition: WorkflowDefinition;
  triggerConfigText: string;
  uiConfigText: string;
  permissionsText: string;
};

export const DEFAULT_NODE_TYPES: WorkflowNodeType[] = [
  { type: "start", label: "开始节点" },
  { type: "condition", label: "条件节点" },
  { type: "task", label: "人工任务" },
  { type: "webhook", label: "Webhook 调用" },
  { type: "end", label: "结束节点" }
];

export const WORKFLOW_STATUS_OPTIONS = [
  { value: "draft", label: "草稿" },
  { value: "active", label: "启用" },
  { value: "disabled", label: "停用" }
];

export const INSTANCE_STATUS_FILTERS = [
  { value: "all", label: "全部实例" },
  { value: "running", label: "运行中" },
  { value: "paused", label: "已暂停" },
  { value: "completed", label: "已完成" },
  { value: "failed", label: "失败" },
  { value: "cancelled", label: "已取消" }
];

export const TASK_STATUS_FILTERS = [
  { value: "all", label: "全部任务" },
  { value: "pending", label: "待处理" },
  { value: "assigned", label: "已分配" },
  { value: "completed", label: "已完成" }
];

export function formatDate(value?: string | null) {
  if (!value) {
    return "未记录";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "未记录";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

export function toText(value: unknown, fallback = "未记录") {
  return typeof value === "string" && value.trim() !== "" ? value : fallback;
}

export function stringifyJson(value: unknown) {
  try {
    return JSON.stringify(value ?? {}, null, 2);
  } catch {
    return "{}";
  }
}

export function parseJsonText(value: string, fieldName: string) {
  const source = value.trim();
  if (!source) {
    return {};
  }
  try {
    const parsed = JSON.parse(source);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error(`${fieldName}必须为 JSON 对象`);
    }
    return parsed as Record<string, unknown>;
  } catch (error) {
    throw new Error(error instanceof Error ? error.message : `${fieldName}格式不正确`);
  }
}

export function normalizeDefinition(value: WorkflowDetail["definition"] | undefined): WorkflowDefinition {
  const base = value && typeof value === "object" ? value : {};
  const nodes = Array.isArray((base as WorkflowDefinition).nodes) ? (base as WorkflowDefinition).nodes : [];
  const edges = Array.isArray((base as WorkflowDefinition).edges) ? (base as WorkflowDefinition).edges : [];

  return {
    nodes: nodes.map((node) => ({
      id: toText(node?.id, ""),
      type: toText(node?.type, "task"),
      name: toText(node?.name, "未命名节点"),
      config: node?.config && typeof node.config === "object" && !Array.isArray(node.config) ? node.config : {}
    })),
    edges: edges.map((edge) => ({
      id: edge?.id,
      source: toText(edge?.source, ""),
      target: toText(edge?.target, ""),
      condition: typeof edge?.condition === "string" ? edge.condition : ""
    }))
  };
}

export function createEmptyDefinition(): WorkflowDefinition {
  return {
    nodes: [
      { id: "start_1", type: "start", name: "开始", config: {} },
      { id: "end_1", type: "end", name: "结束", config: {} }
    ],
    edges: [{ id: "edge_1", source: "start_1", target: "end_1", condition: "" }]
  };
}

export function createEmptyDraft(): WorkflowDraft {
  return {
    name: "",
    description: "",
    category: "",
    status: "draft",
    definition: createEmptyDefinition(),
    triggerConfigText: "{}",
    uiConfigText: "{}",
    permissionsText: "{}"
  };
}

export function buildDraft(detail: WorkflowDetail): WorkflowDraft {
  return {
    name: detail.name || "",
    description: toText(detail.description, ""),
    category: toText(detail.category, ""),
    status: detail.status || "draft",
    definition: normalizeDefinition(detail.definition),
    triggerConfigText: stringifyJson(detail.triggerConfig),
    uiConfigText: stringifyJson(detail.uiConfig),
    permissionsText: stringifyJson(detail.permissions)
  };
}

export function buildNodeId(type: string, nodes: WorkflowNode[]) {
  const prefix = type.replace(/[^a-z0-9]+/gi, "_").toLowerCase();
  let index = nodes.length + 1;
  while (nodes.some((node) => node.id === `${prefix}_${index}`)) {
    index += 1;
  }
  return `${prefix}_${index}`;
}

export function nodeTone(type: string) {
  switch (type) {
    case "start":
      return "border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-500/25 dark:bg-emerald-500/12 dark:text-emerald-100";
    case "condition":
      return "border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-500/25 dark:bg-amber-500/12 dark:text-amber-100";
    case "webhook":
      return "border-blue-300 bg-blue-50 text-blue-900 dark:border-sky-500/25 dark:bg-sky-500/12 dark:text-sky-100";
    case "end":
      return "border-slate-300 bg-slate-100 text-slate-900 dark:border-slate-500/25 dark:bg-slate-500/12 dark:text-slate-100";
    default:
      return "border-violet-300 bg-violet-50 text-violet-900 dark:border-violet-500/25 dark:bg-violet-500/12 dark:text-violet-100";
  }
}

export function statusBadgeVariant(
  status?: string
): "default" | "secondary" | "outline" | "success" | "warning" | "danger" {
  switch (status) {
    case "active":
    case "completed":
      return "success";
    case "running":
      return "secondary";
    case "failed":
    case "disabled":
    case "cancelled":
      return "danger";
    case "paused":
      return "warning";
    default:
      return "outline";
  }
}

export function summarizeConfig(config: Record<string, unknown> | undefined) {
  if (!config) {
    return "无配置";
  }
  const entries = Object.entries(config).filter(([, value]) => value !== null && typeof value !== "undefined" && value !== "");
  if (!entries.length) {
    return "无配置";
  }
  return entries
    .slice(0, 3)
    .map(([key, value]) => `${key}: ${typeof value === "object" ? "[对象]" : String(value)}`)
    .join(" | ");
}

export function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请求失败";
}

export function readBoolean(item: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const value = item[key];
    if (typeof value === "boolean") {
      return value;
    }
  }
  return false;
}

export function readString(item: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const value = item[key];
    if (typeof value === "string" && value.trim() !== "") {
      return value;
    }
  }
  return "";
}
