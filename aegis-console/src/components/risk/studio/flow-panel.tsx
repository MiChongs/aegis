"use client";

import { useId, useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow as TR } from "@/components/ui/table";
import { JsonEditor } from "@/components/functions/json-editor";
import { NumberField, Segmented } from "@/components/traffic/traffic-kit";
import { ACTION_LABEL, LEVEL_LABEL } from "@/components/risk/kit/labels";
import type { CompileIssue, JdmGraph, RiskAction, RiskLevel, RiskMeta, Scene, ScoreBand } from "@/lib/api/risk";

import { IssueList, StudioSection } from "./kit";
import { BASE_INPUT_FIELDS, graphFromTable, rid, tableFromBands, tableFromGraph, type TableModel } from "./jdm";

type View = "bands" | "table" | "json";

function BandsEditor({ bands, onChange, meta }: { bands: ScoreBand[]; onChange: (b: ScoreBand[]) => void; meta: RiskMeta }) {
  const set = (i: number, patch: Partial<ScoreBand>) => onChange(bands.map((b, j) => (j === i ? { ...b, ...patch } : b)));
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="min-w-0 overflow-x-auto">
        <Table>
          <TableHeader>
            <TR>
              <TableHead className="w-28">最低分</TableHead>
              <TableHead className="w-28">最高分</TableHead>
              <TableHead>处置</TableHead>
              <TableHead>等级</TableHead>
              <TableHead className="w-10" />
            </TR>
          </TableHeader>
          <TableBody>
            {bands.map((b, i) => (
              <TR key={i}>
                <TableCell><NumberField value={b.min} onChange={(min) => set(i, { min })} /></TableCell>
                <TableCell><NumberField value={b.max} onChange={(max) => set(i, { max })} suffix={b.max <= 0 ? "不限" : undefined} /></TableCell>
                <TableCell>
                  <Select value={b.action} onValueChange={(v) => set(i, { action: v as RiskAction })}>
                    <SelectTrigger size="sm" className="h-8 w-32"><SelectValue /></SelectTrigger>
                    <SelectContent>{meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}</SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <Select value={b.level} onValueChange={(v) => set(i, { level: v as RiskLevel })}>
                    <SelectTrigger size="sm" className="h-8 w-28"><SelectValue /></SelectTrigger>
                    <SelectContent>{meta.levels.map((l) => <SelectItem key={l} value={l}>{LEVEL_LABEL[l]}</SelectItem>)}</SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <Button size="icon-sm" variant="ghost" aria-label="删除分数段" onClick={() => onChange(bands.filter((_, j) => j !== i))}>
                    <Trash2 className="size-3.5" />
                  </Button>
                </TableCell>
              </TR>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button size="xs" variant="outline" onClick={() => {
          const last = bands[bands.length - 1];
          onChange([...bands, { min: last ? Math.max(last.max, last.min) + 1 : 0, max: 0, action: "pass", level: "none" }]);
        }}>
          <Plus />
          添加分数段
        </Button>
        <Button size="xs" variant="ghost" onClick={() => onChange(meta.defaultBands)}>恢复默认</Button>
      </div>
    </div>
  );
}

function TableEditor({ model, onChange, fieldOptions, meta }: {
  model: TableModel;
  onChange: (m: TableModel) => void;
  fieldOptions: Array<{ field: string; label: string }>;
  meta: RiskMeta;
}) {
  const listId = useId();
  const setRow = (i: number, patch: Partial<TableModel["rows"][number]>) => onChange({ ...model, rows: model.rows.map((r, j) => (j === i ? { ...r, ...patch } : r)) });
  const move = (i: number, d: -1 | 1) => {
    const rows = [...model.rows];
    const j = i + d;
    if (j < 0 || j >= rows.length) return;
    [rows[i], rows[j]] = [rows[j], rows[i]];
    onChange({ ...model, rows });
  };
  const hasCatchAll = model.rows.some((r) => model.inputs.every((i) => !(r.when[i.id] ?? "").trim()));

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <datalist id={listId}>
        {fieldOptions.map((o) => <option key={o.field} value={o.field}>{o.label}</option>)}
      </datalist>
      <div className="min-w-0 overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TR>
              <TableHead className="w-10">#</TableHead>
              {model.inputs.map((input, ci) => (
                <TableHead key={input.id} className="min-w-40 bg-sky-500/5 align-top">
                  <div className="flex items-center gap-1 py-1.5">
                    <Input
                      list={listId}
                      className="h-7 font-mono text-xs"
                      value={input.field}
                      onChange={(e) => onChange({ ...model, inputs: model.inputs.map((x, j) => (j === ci ? { ...x, field: e.target.value.trim(), name: e.target.value.trim() } : x)) })}
                    />
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label="删除输入列"
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
                </TableHead>
              ))}
              <TableHead className="min-w-32 bg-emerald-500/5">处置</TableHead>
              <TableHead className="min-w-28 bg-emerald-500/5">等级</TableHead>
              <TableHead className="min-w-40 bg-emerald-500/5">原因</TableHead>
              <TableHead className="w-24" />
            </TR>
          </TableHeader>
          <TableBody>
            {model.rows.map((row, ri) => (
              <TR key={row.id}>
                <TableCell className="text-xs text-muted-foreground tabular-nums">{ri + 1}</TableCell>
                {model.inputs.map((input) => (
                  <TableCell key={input.id}>
                    <Input
                      className="h-7 font-mono text-xs"
                      placeholder="任意"
                      value={row.when[input.id] ?? ""}
                      onChange={(e) => setRow(ri, { when: { ...row.when, [input.id]: e.target.value } })}
                    />
                  </TableCell>
                ))}
                <TableCell>
                  <Select value={row.action} onValueChange={(v) => setRow(ri, { action: v as RiskAction })}>
                    <SelectTrigger size="sm" className="h-7 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>{meta.actions.map((a) => <SelectItem key={a} value={a}>{ACTION_LABEL[a]}</SelectItem>)}</SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <Select value={row.level || "auto"} onValueChange={(v) => setRow(ri, { level: v === "auto" ? "" : (v as RiskLevel) })}>
                    <SelectTrigger size="sm" className="h-7 w-full text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="auto">按处置</SelectItem>
                      {meta.levels.map((l) => <SelectItem key={l} value={l}>{LEVEL_LABEL[l]}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </TableCell>
                <TableCell>
                  <Input className="h-7 text-xs" value={row.reason} onChange={(e) => setRow(ri, { reason: e.target.value })} />
                </TableCell>
                <TableCell>
                  <div className="flex justify-end gap-0.5">
                    <Button size="icon-xs" variant="ghost" aria-label="上移" disabled={ri === 0} onClick={() => move(ri, -1)}><ArrowUp /></Button>
                    <Button size="icon-xs" variant="ghost" aria-label="下移" disabled={ri === model.rows.length - 1} onClick={() => move(ri, 1)}><ArrowDown /></Button>
                    <Button size="icon-xs" variant="ghost" aria-label="删除行" disabled={model.rows.length <= 1} onClick={() => onChange({ ...model, rows: model.rows.filter((_, j) => j !== ri) })}><Trash2 /></Button>
                  </div>
                </TableCell>
              </TR>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button size="xs" variant="outline" onClick={() => onChange({ ...model, rows: [...model.rows, { id: rid(), when: {}, action: "pass", level: "", reason: "" }] })}>
          <Plus />
          添加行
        </Button>
        <Button size="xs" variant="outline" onClick={() => onChange({ ...model, inputs: [...model.inputs, { id: `i_${rid()}`, name: "forced", field: "forced" }] })}>
          <Plus />
          添加输入列
        </Button>
        <span className="text-xs text-muted-foreground">按顺序命中第一行；输入格写一元条件，如 {">= 80"}、{'"ban"'}、true</span>
        {!hasCatchAll ? <span className="text-xs text-amber-600 dark:text-amber-400">没有兜底行</span> : null}
      </div>
    </div>
  );
}

export function FlowPanel({ scene, patch, meta, featureKeys, modelKeys, issues }: {
  scene: Scene;
  patch: (p: Partial<Scene>) => void;
  meta: RiskMeta;
  featureKeys: string[];
  modelKeys: string[];
  issues: CompileIssue[];
}) {
  const table = useMemo(() => tableFromGraph(scene.flow), [scene.flow]);
  const [view, setView] = useState<View | null>(null);
  const [jsonText, setJsonText] = useState<string | null>(null);
  const [jsonError, setJsonError] = useState<string | null>(null);
  const effective: View = view ?? (!scene.flow ? "bands" : table ? "table" : "json");

  const fieldOptions = useMemo(() => [
    ...BASE_INPUT_FIELDS,
    ...featureKeys.map((k) => ({ field: `features.${k}`, label: `特征 ${k}` })),
    ...modelKeys.map((k) => ({ field: `models.${k}`, label: `模型 ${k}` })),
    ...meta.dimensions.map((d) => ({ field: `dimensions.${d}`, label: `维度分 ${d}` })),
  ], [featureKeys, modelKeys, meta.dimensions]);

  const choose = (next: View) => {
    setJsonText(null);
    setJsonError(null);
    if (next === "bands") patch({ flow: null });
    if (next === "table" && !table) patch({ flow: graphFromTable(tableFromBands(scene.bands ?? meta.defaultBands)) });
    setView(next);
  };

  const viewOptions: Array<{ value: View; label: string }> = [
    { value: "bands", label: "分数段" },
    { value: "table", label: "决策表" },
    { value: "json", label: "JSON" },
  ];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <StudioSection
        title="决策流"
        aside={
          <Segmented
            value={effective}
            options={viewOptions.filter((o) => o.value !== "table" || table || !scene.flow || effective === "table")}
            onChange={choose}
          />
        }
      >
        {issues.length ? <IssueList issues={issues} /> : null}
        {effective === "bands" ? (
          <BandsEditor bands={scene.bands ?? []} onChange={(bands) => patch({ bands })} meta={meta} />
        ) : effective === "table" && table ? (
          <TableEditor model={table} onChange={(m) => patch({ flow: graphFromTable(m) })} fieldOptions={fieldOptions} meta={meta} />
        ) : (
          <div className="flex min-w-0 flex-col gap-2">
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
                    setJsonError("需要 nodes 与 edges 两个数组");
                    return;
                  }
                  setJsonError(null);
                  patch({ flow: parsed });
                } catch (e) {
                  setJsonError(e instanceof Error ? e.message : "JSON 格式错误");
                }
              }}
            />
            {jsonError ? <p className="text-xs text-red-600 dark:text-red-400">{jsonError}</p> : null}
          </div>
        )}
      </StudioSection>
      {effective !== "bands" ? (
        <StudioSection title="分数段（决策流出错时使用）">
          <BandsEditor bands={scene.bands ?? []} onChange={(bands) => patch({ bands })} meta={meta} />
        </StudioSection>
      ) : null}
    </div>
  );
}
