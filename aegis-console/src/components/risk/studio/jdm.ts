import type { DecisionTableColumn, DecisionTableContent, JdmGraph, RiskAction, RiskLevel, ScoreBand } from "@/lib/api/risk";

/**
 * 决策表 ⇄ zen JDM 决策图。
 *
 * 编辑器只生成一种形状：inputNode → decisionTableNode → outputNode，hitPolicy 固定 first，
 * 输出列固定为 action / level / reason（engine/evaluate.go 的 runFlow 只读这三个）。
 * 形状与 services/risk/internal/engine/engine_test.go 的 testFlow 一致：
 * 输入格是一元表达式（`>= 80`，空 = 任意），输出格是表达式，所以字符串要带引号（`"block"`）。
 */

export interface TableInput { id: string; name: string; field: string }
export interface TableRow { id: string; when: Record<string, string>; action: RiskAction; level: RiskLevel | ""; reason: string }
export interface TableModel { inputs: TableInput[]; rows: TableRow[] }

const OUT_ACTION = "o_action";
const OUT_LEVEL = "o_level";
const OUT_REASON = "o_reason";

export const rid = () => Math.random().toString(36).slice(2, 9);

/** 输入列字段的常用取值（决策流的输入见 engine/evaluate.go flowInput） */
export const BASE_INPUT_FIELDS: Array<{ field: string; label: string }> = [
  { field: "score", label: "总分" },
  { field: "forced", label: "规则强制处置" },
  { field: "lists.block", label: "命中黑名单" },
  { field: "lists.allow", label: "命中白名单" },
  { field: "lists.watch", label: "命中观察名单" },
];

export function graphFromTable(model: TableModel): JdmGraph {
  const outputs: DecisionTableColumn[] = [
    { id: OUT_ACTION, name: "Action", field: "action", type: "expression" },
    { id: OUT_LEVEL, name: "Level", field: "level", type: "expression" },
    { id: OUT_REASON, name: "Reason", field: "reason", type: "expression" },
  ];
  const content: DecisionTableContent = {
    hitPolicy: "first",
    inputs: model.inputs.map((i) => ({ id: i.id, name: i.name || i.field, field: i.field, type: "expression" })),
    outputs,
    rules: model.rows.map((r) => {
      const row: Record<string, string> & { _id: string } = { _id: r.id };
      for (const i of model.inputs) row[i.id] = (r.when[i.id] ?? "").trim();
      row[OUT_ACTION] = JSON.stringify(r.action);
      row[OUT_LEVEL] = JSON.stringify(r.level);
      row[OUT_REASON] = JSON.stringify(r.reason);
      return row;
    }),
  };
  return {
    nodes: [
      { id: "in", type: "inputNode", name: "Request", position: { x: 0, y: 0 } },
      { id: "table", type: "decisionTableNode", name: "Decide", position: { x: 260, y: 0 }, content: content as unknown as Record<string, unknown> },
      { id: "out", type: "outputNode", name: "Response", position: { x: 560, y: 0 } },
    ],
    edges: [
      { id: "e1", sourceId: "in", targetId: "table", type: "edge" },
      { id: "e2", sourceId: "table", targetId: "out", type: "edge" },
    ],
  };
}

/** 输出格表达式 → 字面值：`"block"` → block；不是字符串字面量就原样返回 */
function unquote(cell: string | undefined): string {
  const s = (cell ?? "").trim();
  if (/^"(?:[^"\\]|\\.)*"$/.test(s)) {
    try {
      return JSON.parse(s) as string;
    } catch {
      return s;
    }
  }
  if (/^'[^']*'$/.test(s)) return s.slice(1, -1);
  return s;
}

/** 决策图是不是编辑器能处理的单表形状；不是就返回 null（交给 JSON 编辑） */
export function tableFromGraph(graph: JdmGraph | null | undefined): TableModel | null {
  if (!graph || !Array.isArray(graph.nodes) || !Array.isArray(graph.edges)) return null;
  const tables = graph.nodes.filter((n) => n.type === "decisionTableNode");
  const others = graph.nodes.filter((n) => n.type !== "decisionTableNode" && n.type !== "inputNode" && n.type !== "outputNode");
  if (tables.length !== 1 || others.length > 0) return null;
  const content = tables[0].content as unknown as DecisionTableContent | undefined;
  if (!content || !Array.isArray(content.inputs) || !Array.isArray(content.outputs) || !Array.isArray(content.rules)) return null;
  if (content.hitPolicy && content.hitPolicy !== "first") return null;
  const outBy = (field: string) => content.outputs.find((o) => o.field === field)?.id;
  const actionId = outBy("action");
  if (!actionId) return null;
  if (content.outputs.some((o) => !["action", "level", "reason"].includes(o.field))) return null;
  const levelId = outBy("level");
  const reasonId = outBy("reason");
  const inputs = content.inputs.map((i) => ({ id: i.id, name: i.name, field: i.field }));
  const rows: TableRow[] = content.rules.map((r, idx) => ({
    id: r._id || `r${idx + 1}`,
    when: Object.fromEntries(inputs.map((i) => [i.id, String(r[i.id] ?? "")])),
    action: (unquote(r[actionId]) || "pass") as RiskAction,
    level: (levelId ? unquote(r[levelId]) : "") as RiskLevel | "",
    reason: reasonId ? unquote(r[reasonId]) : "",
  }));
  return { inputs, rows };
}

/** 从分数段起一张表：高分在前，最后一行兜底 */
export function tableFromBands(bands: ScoreBand[]): TableModel {
  const input: TableInput = { id: "i_score", name: "Score", field: "score" };
  const sorted = [...bands].sort((a, b) => b.min - a.min);
  const rows: TableRow[] = sorted.map((b, idx) => ({
    id: rid(),
    when: { [input.id]: idx === sorted.length - 1 ? "" : `>= ${b.min}` },
    action: b.action,
    level: b.level,
    reason: "",
  }));
  if (!rows.length) rows.push({ id: rid(), when: { [input.id]: "" }, action: "pass", level: "", reason: "" });
  return { inputs: [input], rows };
}

export function tableRowCount(graph: JdmGraph | null | undefined): number | null {
  return tableFromGraph(graph)?.rows.length ?? null;
}
