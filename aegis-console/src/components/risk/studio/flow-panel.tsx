"use client";

import { useMemo, useState } from "react";
import { Background, Handle, MarkerType, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { ArrowDown, ArrowUp, Braces, GitBranch, Plus, Sigma, Table2, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select";
import { JsonEditor } from "@/components/functions/json-editor";
import { LEVEL_WORD } from "@/components/risk/kit/humanize";
import { ACTION_LABEL, DIM_LABEL } from "@/components/risk/kit/labels";
import type { CompileIssue, JdmGraph, RiskAction, RiskLevel, RiskMeta, Scene } from "@/lib/api/risk";
import { cn } from "@/lib/utils";

import { varLabel, type VarOption } from "./condition-builder";
import { BandsEditor } from "./config-panels";
import {
  BASE_INPUT_FIELDS, describeCell, fieldLabel, graphFromTable, NODE_TYPE_LABEL, rid, tableFromBands, tableFromGraph, type TableModel,
} from "./jdm";
import { ACTION_SWATCH, Callout, ConfirmDialog, IssueList, OptionCard, StudioSection } from "./kit";

/**
 * 决策流：总分与命中情况 → 处置。三种写法：
 * 分数段（不建决策图，按总分落段），决策表（一张按行命中的表），自定义决策图（任意 JDM，用 JSON 编辑）。
 * 决策表与自定义决策图都在上方画出节点图，便于看清走向。
 */

type View = "bands" | "table" | "json";

/* ── 决策图示意 ── */

type DiagramData = { label: string; title: string; detail?: string; kind: string };

function DiagramNode({ data }: NodeProps<Node<DiagramData>>) {
  const io = data.kind === "inputNode" || data.kind === "outputNode";
  return (
    <div className={cn("flex min-w-36 flex-col gap-0.5 rounded-lg border bg-card px-3 py-2 text-left", io ? "border-dashed" : "border-foreground/25")}>
      {data.kind !== "inputNode" ? <Handle type="target" position={Position.Left} className="size-1.5! border-0! bg-muted-foreground!" isConnectable={false} /> : null}
      <span className="text-[11px] leading-4 text-muted-foreground">{data.label}</span>
      <span className="text-xs leading-4 font-medium">{data.title}</span>
      {data.detail ? <span className="text-[11px] leading-4 text-muted-foreground">{data.detail}</span> : null}
      {data.kind !== "outputNode" ? <Handle type="source" position={Position.Right} className="size-1.5! border-0! bg-muted-foreground!" isConnectable={false} /> : null}
    </div>
  );
}

const nodeTypes = { jdm: DiagramNode };

function FlowDiagram({ graph }: { graph: JdmGraph }) {
  const { nodes, edges } = useMemo(() => {
    const nodes: Node<DiagramData>[] = graph.nodes.map((n, i) => {
      const rules = (n.content as { rules?: unknown[] } | undefined)?.rules;
      return {
        id: n.id,
        type: "jdm",
        position: n.position && Number.isFinite(n.position.x) ? n.position : { x: i * 240, y: 0 },
        data: {
          kind: n.type,
          label: NODE_TYPE_LABEL[n.type] ?? "节点",
          title: n.type === "inputNode" ? "总分与命中情况" : n.type === "outputNode" ? "处置与风险等级" : n.name || "未命名",
          detail: n.type === "decisionTableNode" && Array.isArray(rules) ? `${rules.length} 行` : undefined,
        },
        draggable: false,
        connectable: false,
        selectable: false,
      };
    });
    const edges: Edge[] = graph.edges.map((e) => ({
      id: e.id,
      source: e.sourceId,
      target: e.targetId,
      type: "smoothstep",
      style: { stroke: "var(--edge-path-color)", strokeWidth: 1.5 },
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: "var(--edge-path-color)" },
    }));
    return { nodes, edges };
  }, [graph]);

  return (
    <div className="h-56 min-w-0 overflow-hidden rounded-lg border bg-muted/20">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        zoomOnDoubleClick={false}
        panOnScroll={false}
        preventScrolling={false}
        fitView
        fitViewOptions={{ padding: 0.2, maxZoom: 1 }}
        minZoom={0.3}
        maxZoom={1.25}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={18} size={1} color="var(--canvas-grid-color)" />
      </ReactFlow>
    </div>
  );
}

/* ── 决策表 ── */

function TableEditor({ model, onChange, fields, labelOf, meta }: {
  model: TableModel;
  onChange: (m: TableModel) => void;
  fields: Array<{ group: string; items: Array<{ field: string; label: string }> }>;
  labelOf: (field: string) => string;
  meta: RiskMeta;
}) {
  const setRow = (i: number, patch: Partial<TableModel["rows"][number]>) => onChange({ ...model, rows: model.rows.map((r, j) => (j === i ? { ...r, ...patch } : r)) });
  const move = (i: number, d: -1 | 1) => {
    const rows = [...model.rows];
    const j = i + d;
    if (j < 0 || j >= rows.length) return;
    [rows[i], rows[j]] = [rows[j], rows[i]];
    onChange({ ...model, rows });
  };
  const isCatchAll = (r: TableModel["rows"][number]) => model.inputs.every((i) => !(r.when[i.id] ?? "").trim());
  const hasCatchAll = model.rows.some(isCatchAll);
  const known = new Set(fields.flatMap((g) => g.items.map((i) => i.field)));

  const rowSentence = (r: TableModel["rows"][number]) => {
    const conds = model.inputs.filter((i) => (r.when[i.id] ?? "").trim()).map((i) => `${labelOf(i.field)}${describeCell(r.when[i.id])}`);
    const level = r.level ? `，记为${LEVEL_WORD[r.level as RiskLevel] ?? r.level}` : "";
    return `${conds.length ? `当${conds.join("，且")}时` : "其他情况"}，${ACTION_LABEL[r.action] ?? r.action}${level}`;
  };

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="min-w-0 overflow-x-auto rounded-lg border">
        <table className="w-full min-w-[720px] text-sm">
          <thead>
            <tr className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
              <th className="w-10 px-3 py-2 font-medium">顺序</th>
              {model.inputs.map((input, ci) => (
                <th key={input.id} className="min-w-44 px-2 py-2 align-top font-medium">
                  <div className="flex items-center gap-1">
                    <Select
                      value={input.field}
                      onValueChange={(v) => onChange({ ...model, inputs: model.inputs.map((x, j) => (j === ci ? { ...x, field: v, name: v } : x)) })}
                    >
                      <SelectTrigger size="sm" className="h-8 w-full bg-background text-xs"><SelectValue placeholder="选择判断对象">{labelOf(input.field)}</SelectValue></SelectTrigger>
                      <SelectContent>
                        {!known.has(input.field) && input.field ? <SelectItem value={input.field}>{labelOf(input.field)}</SelectItem> : null}
                        {fields.map((g) => (
                          <SelectGroup key={g.group}>
                            <SelectLabel>{g.group}</SelectLabel>
                            {g.items.map((it) => <SelectItem key={it.field} value={it.field}>{it.label}</SelectItem>)}
                          </SelectGroup>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label="删除这一列"
                      disabled={model.inputs.length <= 1}
                      onClick={() => onChange({
                        inputs: model.inputs.filter((_, j) => j !== ci),
                        rows: model.rows.map((r) => {
                          const when = { ...r.when };
                          delete when[input.id];
                          return { ...r, when };
                        }),
                      })}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </th>
              ))}
              <th className="min-w-32 px-2 py-2 font-medium">处置</th>
              <th className="min-w-28 px-2 py-2 font-medium">风险等级</th>
              <th className="min-w-40 px-2 py-2 font-medium">原因</th>
              <th className="w-24 px-2 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y">
            {model.rows.map((row, ri) => {
              const sw = ACTION_SWATCH[row.action] ?? ACTION_SWATCH.pass;
              return (
                <tr key={row.id} className="align-top">
                  <td className="px-3 py-2">
                    <span className="grid size-6 place-items-center rounded-full bg-muted text-[11px] font-medium tabular-nums">{ri + 1}</span>
                  </td>
                  {model.inputs.map((input) => (
                    <td key={input.id} className="px-2 py-2">
                      <Input
                        className="h-8 font-mono text-xs"
                        placeholder="任意"
                        value={row.when[input.id] ?? ""}
                        onChange={(e) => setRow(ri, { when: { ...row.when, [input.id]: e.target.value } })}
                      />
                    </td>
                  ))}
                  <td className="px-2 py-2">
                    <Select value={row.action} onValueChange={(v) => setRow(ri, { action: v as RiskAction })}>
                      <SelectTrigger size="sm" className="h-8 w-full text-xs">
                        <span className="flex items-center gap-1.5"><span aria-hidden className={cn("size-2 rounded-full", sw.solid)} /><SelectValue /></span>
                      </SelectTrigger>
                      <SelectContent>{meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}</SelectContent>
                    </Select>
                  </td>
                  <td className="px-2 py-2">
                    <Select value={row.level || "auto"} onValueChange={(v) => setRow(ri, { level: v === "auto" ? "" : (v as RiskLevel) })}>
                      <SelectTrigger size="sm" className="h-8 w-full text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="auto">随处置</SelectItem>
                        {meta.levels.map((l) => <SelectItem key={l} value={l}>{LEVEL_WORD[l]}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  </td>
                  <td className="px-2 py-2">
                    <Input className="h-8 text-xs" value={row.reason} placeholder="可留空" onChange={(e) => setRow(ri, { reason: e.target.value })} />
                  </td>
                  <td className="px-2 py-2">
                    <div className="flex justify-end gap-0.5">
                      <Button size="icon-xs" variant="ghost" aria-label="上移" disabled={ri === 0} onClick={() => move(ri, -1)}><ArrowUp /></Button>
                      <Button size="icon-xs" variant="ghost" aria-label="下移" disabled={ri === model.rows.length - 1} onClick={() => move(ri, 1)}><ArrowDown /></Button>
                      <Button size="icon-xs" variant="ghost" aria-label="删除这一行" disabled={model.rows.length <= 1} onClick={() => onChange({ ...model, rows: model.rows.filter((_, j) => j !== ri) })}><Trash2 /></Button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button size="xs" variant="outline" onClick={() => onChange({ ...model, rows: [...model.rows, { id: rid(), when: {}, action: "pass", level: "", reason: "" }] })}>
          <Plus />
          添加一行
        </Button>
        <Button size="xs" variant="outline" onClick={() => onChange({ ...model, inputs: [...model.inputs, { id: `i_${rid()}`, name: "forced", field: "forced" }] })}>
          <Plus />
          添加判断列
        </Button>
        <span className="text-xs text-muted-foreground">从上到下逐行匹配，命中第一行即停。格子里写 {">= 80"}、{"[60..79]"}、{'"ban"'}、true，留空表示任意。</span>
      </div>
      {!hasCatchAll ? <Callout tone="warning">没有兜底行。建议在最后加一行全部留空，处理其余情况。</Callout> : null}
      <div className="flex min-w-0 flex-col gap-1.5 rounded-lg bg-muted/40 p-3">
        <span className="text-xs font-medium">读作</span>
        <ol className="flex flex-col gap-1">
          {model.rows.map((r, i) => (
            <li key={r.id} className="flex min-w-0 items-start gap-2 text-xs leading-5">
              <span className="w-5 shrink-0 text-muted-foreground tabular-nums">{i + 1}</span>
              <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", (ACTION_SWATCH[r.action] ?? ACTION_SWATCH.pass).solid)} />
              <span className="min-w-0 break-words">{rowSentence(r)}</span>
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}

/* ── 面板 ── */

export function FlowPanel({ scene, patch, meta, options, featureKeys, modelKeys, issues, nameOf }: {
  scene: Scene;
  patch: (p: Partial<Scene>) => void;
  meta: RiskMeta;
  options: VarOption[];
  featureKeys: string[];
  modelKeys: string[];
  issues: CompileIssue[];
  nameOf?: (issue: CompileIssue) => string | undefined;
}) {
  const table = useMemo(() => tableFromGraph(scene.flow), [scene.flow]);
  const [view, setView] = useState<View | null>(null);
  const [jsonText, setJsonText] = useState<string | null>(null);
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [confirmBands, setConfirmBands] = useState(false);
  const [jsonOpen, setJsonOpen] = useState(true);
  const derived: View = !scene.flow ? "bands" : table ? "table" : "json";
  const effective: View = view === "table" && !table ? derived : view ?? derived;

  const labelOf = (field: string) =>
    fieldLabel(field, (kind, key) => {
      const name = varLabel(options, `${kind === "feature" ? "F" : "M"}.${key}`);
      return name.startsWith("已删除") ? undefined : name;
    }, (d) => DIM_LABEL[d] ?? d);

  const fields = useMemo(() => [
    { group: "评估结果", items: BASE_INPUT_FIELDS },
    { group: "特征", items: featureKeys.map((k) => ({ field: `features.${k}`, label: varLabel(options, `F.${k}`) })) },
    { group: "模型分", items: modelKeys.map((k) => ({ field: `models.${k}`, label: varLabel(options, `M.${k}`) })) },
    { group: "归类得分", items: meta.dimensions.map((d) => ({ field: `dimensions.${d}`, label: `${DIM_LABEL[d] ?? d}得分` })) },
  ].filter((g) => g.items.length), [featureKeys, modelKeys, meta.dimensions, options]);

  const choose = (next: View) => {
    if (next === effective) return;
    if (next === "bands" && scene.flow) {
      setConfirmBands(true);
      return;
    }
    setJsonText(null);
    setJsonError(null);
    if (next === "table" && !table) patch({ flow: graphFromTable(tableFromBands(scene.bands?.length ? scene.bands : meta.defaultBands)) });
    setView(next);
  };

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <StudioSection icon={GitBranch} title="怎样得出处置" description="规则累加出总分后，由这里决定最终处置与风险等级。">
        {issues.length ? <IssueList issues={issues} nameOf={nameOf} /> : null}
        <div role="radiogroup" aria-label="决策方式" className="grid gap-2 md:grid-cols-3">
          <OptionCard selected={effective === "bands"} onSelect={() => choose("bands")} icon={Sigma} title="按分数段" description="总分落在哪一段就按哪一段处置，最常用" />
          <OptionCard
            selected={effective === "table"}
            onSelect={() => choose("table")}
            icon={Table2}
            title="决策表"
            description={scene.flow && !table ? "当前决策图较复杂，不能转为决策表" : "逐行匹配总分、名单、规则结论等多个条件"}
            disabled={!!scene.flow && !table}
          />
          <OptionCard selected={effective === "json"} onSelect={() => choose("json")} icon={Braces} title="自定义决策图" description="直接编辑完整的决策图数据，适合复杂分支" />
        </div>
      </StudioSection>

      {effective === "bands" ? (
        <StudioSection title="分数段" description="0 到 100 分按段对应处置，100 分以上归入最后一段。">
          <BandsEditor bands={scene.bands ?? []} onChange={(bands) => patch({ bands })} meta={meta} />
        </StudioSection>
      ) : null}

      {effective === "table" && table ? (
        <StudioSection title="决策表" description="每一行是一种情况，左侧是判断条件，右侧是对应的处置。">
          {scene.flow ? <FlowDiagram graph={scene.flow} /> : null}
          <TableEditor model={table} onChange={(m) => patch({ flow: graphFromTable(m) })} fields={fields} labelOf={labelOf} meta={meta} />
        </StudioSection>
      ) : null}

      {effective === "json" ? (
        <StudioSection title="自定义决策图" description={scene.flow ? `共 ${scene.flow.nodes?.length ?? 0} 个节点、${scene.flow.edges?.length ?? 0} 条连线。` : "粘贴或编写决策图数据。"}>
          {scene.flow && Array.isArray(scene.flow.nodes) && scene.flow.nodes.length ? <FlowDiagram graph={scene.flow} /> : null}
          <Collapsible open={jsonOpen} onOpenChange={setJsonOpen}>
            <CollapsibleTrigger asChild>
              <Button size="xs" variant="ghost" className="w-fit text-muted-foreground">{jsonOpen ? "收起数据" : "编辑数据"}</Button>
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-2 flex flex-col gap-2">
              <JsonEditor
                height={420}
                value={jsonText ?? (scene.flow ? JSON.stringify(scene.flow, null, 2) : "")}
                onChange={(text) => {
                  setJsonText(text);
                  if (!text.trim()) {
                    setJsonError(null);
                    patch({ flow: null });
                    return;
                  }
                  try {
                    const parsed = JSON.parse(text) as JdmGraph;
                    if (!parsed || !Array.isArray(parsed.nodes) || !Array.isArray(parsed.edges)) {
                      setJsonError("需要包含 nodes 与 edges 两个数组");
                      return;
                    }
                    setJsonError(null);
                    patch({ flow: parsed });
                  } catch (e) {
                    setJsonError(e instanceof Error ? `格式有误：${e.message}` : "格式有误");
                  }
                }}
              />
              {jsonError ? <Callout tone="danger">{jsonError}</Callout> : null}
            </CollapsibleContent>
          </Collapsible>
        </StudioSection>
      ) : null}

      {effective !== "bands" ? (
        <StudioSection title="备用分数段" description="决策流运行出错时，改按这些分数段处置。">
          <BandsEditor bands={scene.bands ?? []} onChange={(bands) => patch({ bands })} meta={meta} compact />
        </StudioSection>
      ) : null}

      <ConfirmDialog
        open={confirmBands}
        onOpenChange={setConfirmBands}
        title="改为按分数段处置"
        description="当前的决策表或决策图会被清除，只按分数段得出处置。保存前可以放弃修改恢复。"
        confirmLabel="改为分数段"
        danger
        onConfirm={() => {
          patch({ flow: null });
          setJsonText(null);
          setJsonError(null);
          setView("bands");
          setConfirmBands(false);
        }}
      />
    </div>
  );
}
