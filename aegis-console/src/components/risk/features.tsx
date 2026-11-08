"use client";

import { useEffect, useState } from "react";
import { CheckCircle2, Pencil, Plus, RefreshCw, Trash2, XCircle } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Field, NumberField, Segmented } from "@/components/traffic/traffic-kit";
import { ENTITY_LABEL, EVENT_LABEL, FEATURE_KIND_HINT, FEATURE_KIND_LABEL, GRAPH_TEMPLATE_LABEL, WINDOW_PRESETS, formatWindow } from "@/components/risk/kit/labels";
import type { Agg, Feature, FeatureKind, RiskEntity, RiskMeta, StreamSyncResult } from "@/lib/api/risk";
import { formatShortDateTime } from "@/lib/datetime";
import { useDeleteFeature, useRiskFeatureSQL, useRiskFeatures, useRiskMeta, useRiskScenes, useSaveFeature, useSyncStreams, useValidateExpr } from "@/lib/risk-hooks";

import { CodeEditor } from "./studio/code-editor";
import { buildVarOptions, completionsOf } from "./studio/condition-builder";
import { ConditionRows, errorText, keyError, MultiPick } from "./studio/kit";

const KINDS: FeatureKind[] = ["velocity", "stream", "graph", "vector", "derived"];
const AGG_LABEL: Record<Agg, string> = { count: "计数", distinct: "去重计数", sum: "求和", max: "最大值", avg: "平均值" };
const VECTOR_OUTPUT_LABEL = { similar_count: "相似设备数", similar_banned: "相似且已封禁数", max_score: "最高相似度" } as const;
const MAX_VELOCITY_WINDOW = 30 * 86400;

function describe(f: Feature): string {
  switch (f.kind) {
    case "velocity": {
      const v = f.velocity;
      if (!v) return "";
      const agg = v.agg === "distinct" ? `不同${ENTITY_LABEL[v.distinctOf as RiskEntity] ?? v.distinctOf ?? ""}数` : "次数";
      return `${formatWindow(v.windowSeconds)}内${agg}${v.allScenes ? "（全部场景）" : ""}`;
    }
    case "stream": {
      const s = f.stream;
      if (!s) return "";
      const ev = s.events.map((e) => EVENT_LABEL[e] ?? e).join(" / ");
      return `${ev} ${formatWindow(s.windowSeconds)} ${AGG_LABEL[s.agg]}${s.field ? ` ${s.field}` : ""}${s.conditions?.length ? ` 条件 ${s.conditions.length}` : ""}`;
    }
    case "graph":
      return f.graph ? `${GRAPH_TEMPLATE_LABEL[f.graph.template] ?? f.graph.template}${f.graph.withinDays ? ` 近 ${f.graph.withinDays} 天` : ""}` : "";
    case "vector":
      return f.vector ? `${VECTOR_OUTPUT_LABEL[f.vector.output]} Top${f.vector.topK} ≥ ${f.vector.minScore}` : "";
    case "derived":
      return f.expr ?? "";
  }
}

function blank(kind: FeatureKind): Feature {
  const base: Feature = { key: "", name: "", kind, entity: "", default: 0, global: false };
  switch (kind) {
    case "velocity":
      return { ...base, entity: "ip", velocity: { windowSeconds: 600, agg: "count", allScenes: false } };
    case "stream":
      return { ...base, entity: "account", stream: { events: ["login.fail"], agg: "count", windowSeconds: 3600, conditions: [] } };
    case "graph":
      return { ...base, entity: "user", graph: { template: "device_accounts", withinDays: 30, cacheTtl: 120 } };
    case "vector":
      return { ...base, vector: { collection: "device", topK: 20, minScore: 0.95, output: "similar_count" } };
    case "derived":
      return { ...base, expr: "" };
  }
}

/** 保存时只带当前种类的定义块，切换种类留下的其它块不下发 */
function clean(f: Feature): Feature {
  const out: Feature = { key: f.key, name: f.name.trim(), kind: f.kind, entity: f.entity, global: f.global, default: f.default, description: f.description };
  if (f.kind === "velocity") out.velocity = f.velocity;
  if (f.kind === "stream") out.stream = { ...f.stream!, field: f.stream?.agg === "count" ? "" : f.stream?.field };
  if (f.kind === "graph") out.graph = f.graph;
  if (f.kind === "vector") out.vector = f.vector;
  if (f.kind === "derived") {
    out.expr = f.expr;
    out.entity = "";
  }
  return out;
}

function WindowPicker({ value, onChange, max }: { value: number; onChange: (v: number) => void; max?: number }) {
  const presets = WINDOW_PRESETS.filter((p) => !max || p.seconds <= max);
  const options = presets.some((p) => p.seconds === value) ? presets : [...presets, { label: formatWindow(value), seconds: value }];
  return (
    <Segmented value={String(value)} options={options.map((p) => ({ value: String(p.seconds), label: p.label }))} onChange={(v) => onChange(Number(v))} />
  );
}

function EntitySelect({ value, onChange, meta, className }: { value: string; onChange: (v: RiskEntity) => void; meta: RiskMeta; className?: string }) {
  return (
    <Select value={value || undefined} onValueChange={(v) => onChange(v as RiskEntity)}>
      <SelectTrigger className={className ?? "w-full"}><SelectValue placeholder="选择主体" /></SelectTrigger>
      <SelectContent>{meta.entities.map((e) => <SelectItem key={e} value={e}>{ENTITY_LABEL[e] ?? e}</SelectItem>)}</SelectContent>
    </Select>
  );
}

/** 派生表达式即时校验。校验接口按布尔表达式编译，数值表达式报类型不符时视为通过（保存时以数值再编一次） */
function useDerivedCheck(expr: string) {
  const { mutate } = useValidateExpr();
  const [result, setResult] = useState<{ expr: string; ok: boolean; error?: string } | null>(null);
  useEffect(() => {
    if (!expr.trim()) return;
    const timer = setTimeout(() => {
      mutate(expr, {
        onSuccess: (r) => setResult({ expr, ok: r.ok || /expected bool/i.test(r.error ?? ""), error: r.error }),
        onError: (e) => setResult({ expr, ok: false, error: errorText(e) }),
      });
    }, 400);
    return () => clearTimeout(timer);
  }, [expr, mutate]);
  return result && result.expr === expr ? result : null;
}

function FeatureDialog({ initial, isNew, meta, features, onClose }: { initial: Feature; isNew: boolean; meta: RiskMeta; features: Feature[]; onClose: () => void }) {
  const save = useSaveFeature();
  const scenes = useRiskScenes();
  const [f, setF] = useState<Feature>(initial);
  const [touched, setTouched] = useState(false);
  const sql = useRiskFeatureSQL(!isNew && initial.kind === "stream" ? initial.key : null);
  const check = useDerivedCheck(f.kind === "derived" ? f.expr ?? "" : "");
  const kErr = keyError(f.key) ?? (isNew && features.some((x) => x.key === f.key) ? "特征键已存在" : null);
  const nErr = f.name.trim() ? null : "必填";
  const set = (p: Partial<Feature>) => setF({ ...f, ...p });
  const options = buildVarOptions(meta.variables, features.filter((x) => x.key !== f.key), [], []);

  const submit = () => {
    setTouched(true);
    if (kErr || nErr) return;
    save.mutate(clean(f), {
      onSuccess: () => {
        toast.success("特征已保存");
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };

  const form = (
    <div className="flex min-w-0 flex-col gap-4">
      {isNew ? (
        <Field label="种类">
          <Segmented
            value={f.kind}
            options={KINDS.map((k) => ({ value: k, label: FEATURE_KIND_LABEL[k] }))}
            onChange={(kind) => setF({ ...blank(kind), key: f.key, name: f.name, default: f.default, description: f.description })}
          />
        </Field>
      ) : null}
      <p className="text-xs text-muted-foreground">{FEATURE_KIND_HINT[f.kind]}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="特征键（F.键）" error={touched ? kErr : null}>
          <Input className="font-mono" disabled={!isNew} value={f.key} onChange={(e) => set({ key: e.target.value.trim() })} />
        </Field>
        <Field label="名称" error={touched ? nErr : null}>
          <Input value={f.name} onChange={(e) => set({ name: e.target.value })} />
        </Field>
      </div>

      {f.kind === "velocity" && f.velocity ? (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="主体"><EntitySelect value={f.entity ?? ""} onChange={(entity) => set({ entity })} meta={meta} /></Field>
            <Field label="统计">
              <Segmented
                value={f.velocity.agg}
                options={[{ value: "count" as const, label: "次数" }, { value: "distinct" as const, label: "去重计数" }]}
                onChange={(agg) => set({ velocity: { ...f.velocity!, agg, distinctOf: agg === "distinct" ? f.velocity!.distinctOf || "account" : undefined } })}
              />
            </Field>
            {f.velocity.agg === "distinct" ? (
              <Field label="数哪个主体">
                <EntitySelect value={f.velocity.distinctOf ?? ""} onChange={(distinctOf) => set({ velocity: { ...f.velocity!, distinctOf } })} meta={meta} />
              </Field>
            ) : null}
          </div>
          <Field label="窗口"><WindowPicker max={MAX_VELOCITY_WINDOW} value={f.velocity.windowSeconds} onChange={(windowSeconds) => set({ velocity: { ...f.velocity!, windowSeconds } })} /></Field>
          <div className="flex flex-wrap gap-6">
            <label className="flex items-center gap-2 text-sm"><Switch checked={!!f.velocity.allScenes} onCheckedChange={(allScenes) => set({ velocity: { ...f.velocity!, allScenes } })} />全部场景一起数</label>
            <label className="flex items-center gap-2 text-sm"><Switch checked={!!f.global} onCheckedChange={(global) => set({ global })} />跨应用统计</label>
          </div>
        </>
      ) : null}

      {f.kind === "stream" && f.stream ? (
        <>
          <Field label="事件">
            <MultiPick
              options={meta.eventTypes.map((e) => ({ value: e.type, label: EVENT_LABEL[e.type] ?? e.label, hint: e.type }))}
              value={f.stream.events}
              onChange={(events) => set({ stream: { ...f.stream!, events } })}
              emptyLabel="选择事件"
            />
          </Field>
          <Field label="限定场景">
            <MultiPick
              options={(scenes.data ?? []).map((s) => ({ value: s.key, label: s.name, hint: s.key }))}
              value={f.stream.scenes ?? []}
              onChange={(sc) => set({ stream: { ...f.stream!, scenes: sc } })}
              emptyLabel="全部场景"
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="主体"><EntitySelect value={f.entity ?? ""} onChange={(entity) => set({ entity })} meta={meta} /></Field>
            <Field label="聚合">
              <Select value={f.stream.agg} onValueChange={(v) => set({ stream: { ...f.stream!, agg: v as Agg } })}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>{(Object.keys(AGG_LABEL) as Agg[]).map((a) => <SelectItem key={a} value={a}>{AGG_LABEL[a]}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
            {f.stream.agg !== "count" ? (
              <Field label="字段">
                <Select value={f.stream.field || undefined} onValueChange={(field) => set({ stream: { ...f.stream!, field } })}>
                  <SelectTrigger className="w-full font-mono"><SelectValue placeholder="选择字段" /></SelectTrigger>
                  <SelectContent>{meta.streamColumns.map((c) => <SelectItem key={c} value={c} className="font-mono">{c}</SelectItem>)}</SelectContent>
                </Select>
              </Field>
            ) : null}
          </div>
          <Field label="窗口"><WindowPicker value={f.stream.windowSeconds} onChange={(windowSeconds) => set({ stream: { ...f.stream!, windowSeconds } })} /></Field>
          <Field label="过滤条件">
            <ConditionRows value={f.stream.conditions ?? []} onChange={(conditions) => set({ stream: { ...f.stream!, conditions } })} columns={meta.streamColumns} />
          </Field>
          <label className="flex items-center gap-2 text-sm"><Switch checked={!!f.global} onCheckedChange={(global) => set({ global })} />跨应用统计</label>
        </>
      ) : null}

      {f.kind === "graph" && f.graph ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="查询模板">
            <Select value={f.graph.template} onValueChange={(template) => set({ graph: { ...f.graph!, template } })}>
              <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>{meta.graphTemplates.map((t) => <SelectItem key={t} value={t}>{GRAPH_TEMPLATE_LABEL[t] ?? t}</SelectItem>)}</SelectContent>
            </Select>
          </Field>
          <Field label="主体"><EntitySelect value={f.entity ?? ""} onChange={(entity) => set({ entity })} meta={meta} /></Field>
          <Field label="只看近几天（0 = 不限）">
            <NumberField value={f.graph.withinDays ?? 0} min={0} suffix="天" onChange={(withinDays) => set({ graph: { ...f.graph!, withinDays } })} />
          </Field>
          <Field label="缓存">
            <NumberField value={f.graph.cacheTtl ?? 60} min={0} suffix="秒" onChange={(cacheTtl) => set({ graph: { ...f.graph!, cacheTtl } })} />
          </Field>
        </div>
      ) : null}

      {f.kind === "vector" && f.vector ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="向量集合">
            <Select value={f.vector.collection} onValueChange={(collection) => set({ vector: { ...f.vector!, collection } })}>
              <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="device">设备指纹</SelectItem>
                <SelectItem value="behavior">行为序列</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="输出">
            <Select value={f.vector.output} onValueChange={(v) => set({ vector: { ...f.vector!, output: v as keyof typeof VECTOR_OUTPUT_LABEL } })}>
              <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                {(Object.keys(VECTOR_OUTPUT_LABEL) as Array<keyof typeof VECTOR_OUTPUT_LABEL>).map((o) => <SelectItem key={o} value={o}>{VECTOR_OUTPUT_LABEL[o]}</SelectItem>)}
              </SelectContent>
            </Select>
          </Field>
          <Field label="TopK（1-100）">
            <NumberField value={f.vector.topK} min={1} max={100} onChange={(topK) => set({ vector: { ...f.vector!, topK } })} />
          </Field>
          <Field label="最低相似度（0-1）">
            <NumberField value={f.vector.minScore} min={0} max={1} step={0.01} onChange={(minScore) => set({ vector: { ...f.vector!, minScore } })} />
          </Field>
        </div>
      ) : null}

      {f.kind === "derived" ? (
        <Field label="表达式">
          <CodeEditor language="risk-expr" value={f.expr ?? ""} onChange={(expr) => set({ expr })} completions={completionsOf(options)} height={110} placeholder="net.proxy || net.vpn || net.tor" />
          {f.expr?.trim() ? (
            check ? (
              check.ok ? (
                <span className="inline-flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400"><CheckCircle2 className="size-3.5" />编译通过</span>
              ) : (
                <pre className="rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 font-mono text-[11px] break-all whitespace-pre-wrap text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
                  <XCircle className="mr-1 inline size-3.5" />
                  {check.error}
                </pre>
              )
            ) : (
              <span className="text-xs text-muted-foreground">校验中</span>
            )
          ) : null}
        </Field>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="取不到时的默认值">
          <NumberField value={f.default} onChange={(v) => set({ default: v })} />
        </Field>
      </div>
      <Field label="说明">
        <Textarea rows={2} value={f.description ?? ""} onChange={(e) => set({ description: e.target.value })} />
      </Field>
    </div>
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{isNew ? "新建特征" : `编辑特征 ${initial.name}`}</DialogTitle>
        </DialogHeader>
        {!isNew && initial.kind === "stream" ? (
          <Tabs defaultValue="form">
            <TabsList>
              <TabsTrigger value="form">定义</TabsTrigger>
              <TabsTrigger value="sql">SQL</TabsTrigger>
            </TabsList>
            <TabsContent value="form" className="mt-3">{form}</TabsContent>
            <TabsContent value="sql" className="mt-3">
              {sql.isLoading ? (
                <Skeleton className="h-72 rounded-lg" />
              ) : sql.data ? (
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap gap-2 font-mono text-[11px] text-muted-foreground">
                    <span>{sql.data.object.base}</span>
                  </div>
                  <CodeEditor language="sql" readOnly lineNumbers value={sql.data.sql} height={360} />
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{errorText(sql.error, "暂无 SQL")}</p>
              )}
            </TabsContent>
          </Tabs>
        ) : form}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={submit} disabled={save.isPending}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SyncResultDialog({ result, onClose }: { result: StreamSyncResult; onClose: () => void }) {
  const failed = Object.entries(result.failed ?? {});
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>同步结果</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 text-sm">
          <div>
            <div className="mb-1 text-xs text-muted-foreground">新建 {result.created?.length ?? 0}</div>
            <div className="flex flex-wrap gap-1">{(result.created ?? []).map((k) => <Badge key={k} variant="success" size="sm">{k}</Badge>)}</div>
          </div>
          <div>
            <div className="mb-1 text-xs text-muted-foreground">清理 {result.dropped?.length ?? 0}</div>
            <div className="flex flex-wrap gap-1">{(result.dropped ?? []).map((k) => <Badge key={k} variant="secondary" size="sm">{k}</Badge>)}</div>
          </div>
          <div>
            <div className="mb-1 text-xs text-muted-foreground">失败 {failed.length}</div>
            <ul className="flex flex-col gap-1">
              {failed.map(([k, msg]) => (
                <li key={k} className="rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 text-xs text-red-700 dark:border-red-800/70 dark:bg-red-950/60 dark:text-red-300">
                  <span className="font-mono">{k}</span> {msg}
                </li>
              ))}
            </ul>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function RiskFeatures() {
  const meta = useRiskMeta();
  const features = useRiskFeatures();
  const del = useDeleteFeature();
  const sync = useSyncStreams();
  const [editing, setEditing] = useState<{ feature: Feature; isNew: boolean } | null>(null);
  const [deleting, setDeleting] = useState<Feature | null>(null);
  const [syncResult, setSyncResult] = useState<StreamSyncResult | null>(null);
  const [kind, setKind] = useState<FeatureKind | "all">("all");
  const list = features.data ?? [];
  const shown = KINDS.filter((k) => kind === "all" || k === kind);

  const runSync = () => sync.mutate(undefined, {
    onSuccess: (r) => {
      setSyncResult(r);
      const failed = Object.keys(r.failed ?? {}).length;
      if (failed) toast.error(`${failed} 个流式特征同步失败`);
      else toast.success("流式特征已同步");
    },
    onError: (e) => toast.error(errorText(e, "同步失败")),
  });

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Segmented
          value={kind}
          options={[{ value: "all" as const, label: `全部 ${list.length}` }, ...KINDS.map((k) => ({ value: k, label: `${FEATURE_KIND_LABEL[k]} ${list.filter((f) => f.kind === k).length}` }))]}
          onChange={setKind}
        />
        <span className="flex-1" />
        <Button size="sm" variant="outline" onClick={runSync} disabled={sync.isPending}>
          <RefreshCw className={sync.isPending ? "animate-spin" : undefined} />
          同步流式特征
        </Button>
        <Button size="sm" disabled={!meta.data} onClick={() => setEditing({ feature: blank("velocity"), isNew: true })}>
          <Plus />
          新建特征
        </Button>
      </div>

      {features.isLoading ? (
        <div className="flex flex-col gap-2">{Array.from({ length: 8 }, (_, i) => <Skeleton key={i} className="h-11 rounded-md" />)}</div>
      ) : features.isError ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">{errorText(features.error, "加载失败")}</div>
      ) : list.length === 0 ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">暂无特征</div>
      ) : (
        shown.map((k) => {
          const items = list.filter((f) => f.kind === k);
          if (!items.length) return null;
          return (
            <section key={k} className="flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-4">
              <header className="flex min-w-0 flex-wrap items-baseline gap-2">
                <h3 className="text-sm font-semibold">{FEATURE_KIND_LABEL[k]}</h3>
                <Badge variant="secondary" size="sm">{items.length}</Badge>
                <span className="min-w-0 truncate text-xs text-muted-foreground">{FEATURE_KIND_HINT[k]}</span>
              </header>
              <div className="min-w-0 overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>特征</TableHead>
                      <TableHead>主体</TableHead>
                      <TableHead className="min-w-64">定义</TableHead>
                      <TableHead className="text-right">默认值</TableHead>
                      <TableHead>更新</TableHead>
                      <TableHead className="w-20" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map((f) => (
                      <TableRow key={f.key}>
                        <TableCell>
                          <div className="flex items-center gap-1.5">
                            <span className="text-sm">{f.name}</span>
                            {f.global ? <Badge variant="info" size="sm">跨应用</Badge> : null}
                          </div>
                          <div className="font-mono text-[11px] text-muted-foreground">F.{f.key}</div>
                        </TableCell>
                        <TableCell className="text-sm">{f.entity ? ENTITY_LABEL[f.entity as RiskEntity] ?? f.entity : "无"}</TableCell>
                        <TableCell className={k === "derived" ? "font-mono text-[11px]" : "text-sm"}>{describe(f)}</TableCell>
                        <TableCell className="text-right tabular-nums">{f.default}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">{formatShortDateTime(f.updatedAt)}</TableCell>
                        <TableCell>
                          <div className="flex justify-end gap-0.5">
                            <Button size="icon-sm" variant="ghost" aria-label="编辑" disabled={!meta.data} onClick={() => setEditing({ feature: { ...blank(f.kind), ...f }, isNew: false })}>
                              <Pencil className="size-3.5" />
                            </Button>
                            <Button size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setDeleting(f)}>
                              <Trash2 className="size-3.5" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </section>
          );
        })
      )}

      {editing && meta.data ? (
        <FeatureDialog initial={editing.feature} isNew={editing.isNew} meta={meta.data} features={list} onClose={() => setEditing(null)} />
      ) : null}
      {syncResult ? <SyncResultDialog result={syncResult} onClose={() => setSyncResult(null)} /> : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除特征</AlertDialogTitle>
            <AlertDialogDescription>删除 F.{deleting?.key}，引用它的规则下次发布时会编译失败。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => deleting && del.mutate(deleting.key, {
                onSuccess: () => toast.success("特征已删除"),
                onError: (e) => toast.error(errorText(e, "删除失败")),
              })}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
