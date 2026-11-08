"use client";

import { useState } from "react";
import { ChevronLeft, ChevronRight, Pencil, Plus, Trash2 } from "lucide-react";
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
import { Textarea } from "@/components/ui/textarea";
import { Field, NumberField, Segmented } from "@/components/traffic/traffic-kit";
import { useAppName } from "@/components/risk/kit/app-scope";
import { ACTION_LABEL, DURATION_PRESETS, ENTITY_LABEL, EVENT_LABEL, KIND_LABEL, SEVERITY_LABEL, WINDOW_PRESETS, formatWindow } from "@/components/risk/kit/labels";
import type { CepAction, CepPattern, CepStep, RiskEntity, RiskMeta } from "@/lib/api/risk";
import { formatShortDateTime } from "@/lib/datetime";
import { useDeletePattern, useRiskMeta, useRiskPatterns, useRiskScenes, useSavePattern } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { AppMultiSelect, ConditionRows, errorText, keyError, MultiPick } from "./studio/kit";

/**
 * 行为模式（Flink CEP）：若干步按时间先后发生（或最后一步「没有发生」），命中后建复核单或加名单。
 * 保存即生效：risk-cep 作业每 30 秒左右从 risk-admin 拉一次模式并热加载，无需发布。
 */

type KeyBy = CepPattern["keyBy"];
type Severity = CepPattern["severity"];

const KEY_BY: KeyBy[] = ["account", "user", "device", "ip"];
const SEVERITIES: Severity[] = ["low", "medium", "high", "critical"];
const SEVERITY_TONE: Record<Severity, "secondary" | "info" | "warning" | "danger"> = { low: "secondary", medium: "info", high: "warning", critical: "danger" };
const TARGETS: Array<NonNullable<CepAction["target"]>> = ["device", "ip", "account", "user"];
const MAX_WITHIN = 30 * 86400;

const eventsText = (events: string[]) => (events.length ? events.map((e) => EVENT_LABEL[e] ?? e).join("或") : "任意事件");

function stepPhrase(s: CepStep, i: number): string {
  let body = eventsText(s.events);
  if (s.actions?.length) body += `（${s.actions.map((a) => ACTION_LABEL[a as keyof typeof ACTION_LABEL] ?? a).join("、")}）`;
  if (s.times > 1) body += ` ≥${s.times} 次`;
  if (s.distinct && s.times > 1) body += `，${s.distinct} 各不相同`;
  if (s.conditions?.length) body += `，满足 ${s.conditions.length} 个条件`;
  if (i === 0) return body;
  if (s.absent) return `之后没有发生 ${body}`;
  if (s.contiguous) return `紧接着 ${body}`;
  return `随后 ${body}`;
}

export function patternSentence(p: Pick<CepPattern, "keyBy" | "withinSeconds" | "steps">): string {
  return `同一${ENTITY_LABEL[p.keyBy as RiskEntity] ?? p.keyBy}在 ${formatWindow(p.withinSeconds)}内：${p.steps.map(stepPhrase).join("，")}`;
}

function actionText(a: CepAction): string {
  if (a.kind === "case") return "建复核单";
  return `${ENTITY_LABEL[a.target as RiskEntity] ?? a.target}加入${KIND_LABEL[a.listKind ?? "block"]} ${a.durationSeconds ? formatWindow(a.durationSeconds) : "永久"}`;
}

function blank(): CepPattern {
  return {
    key: "", name: "", description: "", enabled: true, keyBy: "account", appIds: [], withinSeconds: 600, severity: "medium",
    steps: [
      { name: "fail", events: ["login.fail"], times: 3 },
      { name: "success", events: ["login.success"], times: 1 },
    ],
    actions: [{ kind: "case" }],
  };
}

function StepCard({ step, index, count, meta, sceneOptions, onChange, onMove, onRemove }: {
  step: CepStep;
  index: number;
  count: number;
  meta: RiskMeta;
  sceneOptions: Array<{ value: string; label: string; hint?: string }>;
  onChange: (s: CepStep) => void;
  onMove: (d: -1 | 1) => void;
  onRemove: () => void;
}) {
  const isLast = index === count - 1;
  const set = (p: Partial<CepStep>) => onChange({ ...step, ...p });
  const hasDecision = step.events.includes("decision");
  return (
    <div className={cn("flex w-80 shrink-0 flex-col gap-3 rounded-xl border bg-card p-3", step.absent && "border-dashed border-amber-400 dark:border-amber-700")}>
      <div className="flex items-center gap-1.5">
        <span className="grid size-6 shrink-0 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">{index + 1}</span>
        <Input className="h-7 font-mono text-xs" value={step.name} placeholder={`s${index + 1}`} onChange={(e) => set({ name: e.target.value.trim() })} />
        <Button size="icon-xs" variant="ghost" aria-label="左移" disabled={index === 0} onClick={() => onMove(-1)}><ChevronLeft /></Button>
        <Button size="icon-xs" variant="ghost" aria-label="右移" disabled={isLast} onClick={() => onMove(1)}><ChevronRight /></Button>
        <Button size="icon-xs" variant="ghost" aria-label="删除这一步" disabled={count <= 1} onClick={onRemove}><Trash2 /></Button>
      </div>
      {index > 0 ? (
        <div className="flex flex-col gap-1.5">
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={!!step.contiguous} disabled={!!step.absent} onCheckedChange={(contiguous) => set({ contiguous })} />
            紧接着（中间不夹别的事件）
          </label>
          <label className={cn("flex items-center gap-2 text-xs", !isLast && "opacity-50")}>
            <Switch checked={!!step.absent} disabled={!isLast} onCheckedChange={(absent) => set({ absent, contiguous: absent ? false : step.contiguous })} />
            之后没有发生
          </label>
        </div>
      ) : null}
      <Field label="事件（任一）">
        <MultiPick
          options={meta.eventTypes.map((e) => ({ value: e.type, label: EVENT_LABEL[e.type] ?? e.label, hint: e.type }))}
          value={step.events}
          onChange={(events) => set({ events })}
          emptyLabel="选择事件"
        />
      </Field>
      {hasDecision ? (
        <>
          <Field label="决策场景">
            <MultiPick options={sceneOptions} value={step.scenes ?? []} onChange={(scenes) => set({ scenes })} emptyLabel="全部场景" />
          </Field>
          <Field label="决策处置">
            <MultiPick options={meta.actions.map((a) => ({ value: a, label: ACTION_LABEL[a] }))} value={step.actions ?? []} onChange={(actions) => set({ actions })} emptyLabel="任意处置" />
          </Field>
        </>
      ) : null}
      <div className="grid grid-cols-2 gap-2">
        <Field label="至少几次">
          <NumberField value={step.times} min={1} max={1000} onChange={(times) => set({ times: Math.max(1, times), distinct: times > 1 ? step.distinct : undefined })} />
        </Field>
        <Field label="各不相同">
          <Select value={step.distinct || "none"} disabled={step.times <= 1} onValueChange={(v) => set({ distinct: v === "none" ? undefined : v })}>
            <SelectTrigger size="sm" className="h-8 w-full font-mono text-xs"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="none">不要求</SelectItem>
              {meta.streamColumns.map((c) => <SelectItem key={c} value={c} className="font-mono text-xs">{c}</SelectItem>)}
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Field label="字段条件">
        <ConditionRows value={step.conditions ?? []} onChange={(conditions) => set({ conditions })} columns={meta.streamColumns} />
      </Field>
    </div>
  );
}

function ActionsEditor({ actions, onChange }: { actions: CepAction[]; onChange: (a: CepAction[]) => void }) {
  const set = (i: number, p: Partial<CepAction>) => onChange(actions.map((a, j) => (j === i ? { ...a, ...p } : a)));
  return (
    <div className="flex min-w-0 flex-col gap-2">
      {actions.map((a, i) => (
        <div key={i} className="flex min-w-0 flex-wrap items-center gap-2 rounded-lg border px-3 py-2">
          <Select value={a.kind} onValueChange={(v) => set(i, v === "case" ? { kind: "case", target: undefined, listKind: undefined, durationSeconds: undefined } : { kind: "list_add", target: a.target ?? "device", listKind: a.listKind ?? "watch", durationSeconds: a.durationSeconds ?? 86400 })}>
            <SelectTrigger size="sm" className="h-8 w-32"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="case">建复核单</SelectItem>
              <SelectItem value="list_add">加入名单</SelectItem>
            </SelectContent>
          </Select>
          {a.kind === "list_add" ? (
            <>
              <Select value={a.target ?? "device"} onValueChange={(v) => set(i, { target: v as CepAction["target"] })}>
                <SelectTrigger size="sm" className="h-8 w-24"><SelectValue /></SelectTrigger>
                <SelectContent>{TARGETS.map((t) => <SelectItem key={t} value={t}>{ENTITY_LABEL[t]}</SelectItem>)}</SelectContent>
              </Select>
              <Segmented
                value={a.listKind ?? "watch"}
                options={[{ value: "watch" as const, label: KIND_LABEL.watch }, { value: "block" as const, label: KIND_LABEL.block }]}
                onChange={(listKind) => set(i, { listKind })}
              />
              <Select value={String(a.durationSeconds ?? 0)} onValueChange={(v) => set(i, { durationSeconds: Number(v) })}>
                <SelectTrigger size="sm" className="h-8 w-24"><SelectValue /></SelectTrigger>
                <SelectContent>{DURATION_PRESETS.map((d) => <SelectItem key={d.seconds} value={String(d.seconds)}>{d.label}</SelectItem>)}</SelectContent>
              </Select>
            </>
          ) : null}
          <span className="flex-1" />
          <Button size="icon-sm" variant="ghost" aria-label="删除处置" onClick={() => onChange(actions.filter((_, j) => j !== i))}><Trash2 className="size-3.5" /></Button>
        </div>
      ))}
      <Button size="xs" variant="outline" className="w-fit" onClick={() => onChange([...actions, { kind: "case" }])}>
        <Plus />
        添加处置
      </Button>
    </div>
  );
}

function PatternDialog({ initial, isNew, meta, existing, onClose }: { initial: CepPattern; isNew: boolean; meta: RiskMeta; existing: Set<string>; onClose: () => void }) {
  const save = useSavePattern();
  const scenes = useRiskScenes();
  const [p, setP] = useState<CepPattern>(initial);
  const [touched, setTouched] = useState(false);
  const set = (patch: Partial<CepPattern>) => setP({ ...p, ...patch });
  const kErr = keyError(p.key) ?? (isNew && existing.has(p.key) ? "模式键已存在" : null);
  const nErr = p.name.trim() ? null : "必填";
  const stepErr = p.steps.findIndex((s) => s.events.length === 0);
  const sceneOptions = (scenes.data ?? []).map((s) => ({ value: s.key, label: s.name, hint: s.key }));
  const withinPresets = WINDOW_PRESETS.filter((w) => w.seconds <= MAX_WITHIN);
  const withinOptions = withinPresets.some((w) => w.seconds === p.withinSeconds) ? withinPresets : [...withinPresets, { label: formatWindow(p.withinSeconds), seconds: p.withinSeconds }];

  const setStep = (i: number, s: CepStep) => set({ steps: p.steps.map((x, j) => (j === i ? s : x)) });
  const normalize = (steps: CepStep[]) =>
    steps.map((s, i) => ({ ...s, absent: i > 0 && i === steps.length - 1 ? s.absent : false, contiguous: i > 0 ? s.contiguous : false }));
  const move = (i: number, d: -1 | 1) => {
    const steps = [...p.steps];
    const j = i + d;
    [steps[i], steps[j]] = [steps[j], steps[i]];
    set({ steps: normalize(steps) });
  };

  const submit = () => {
    setTouched(true);
    if (kErr || nErr || stepErr >= 0) return;
    const steps = normalize(p.steps).map((s, i) => ({ ...s, name: s.name || `s${i + 1}` }));
    save.mutate({ ...p, name: p.name.trim(), steps }, {
      onSuccess: () => {
        toast.success("模式已保存，约 30 秒内生效");
        onClose();
      },
      onError: (e) => toast.error(errorText(e, "保存失败")),
    });
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[94vh] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{isNew ? "新建行为模式" : `编辑 ${initial.name}`}</DialogTitle>
        </DialogHeader>
        <div className="flex min-w-0 flex-col gap-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="模式键" error={touched ? kErr : null}>
              <Input className="font-mono" disabled={!isNew} value={p.key} onChange={(e) => set({ key: e.target.value.trim() })} />
            </Field>
            <Field label="名称" error={touched ? nErr : null}>
              <Input value={p.name} onChange={(e) => set({ name: e.target.value })} />
            </Field>
            <Field label="启用">
              <div className="flex h-9 items-center"><Switch checked={p.enabled} onCheckedChange={(enabled) => set({ enabled })} /></div>
            </Field>
          </div>
          <div className="grid gap-3 lg:grid-cols-3">
            <Field label="按谁分组">
              <Segmented value={p.keyBy} options={KEY_BY.map((k) => ({ value: k, label: ENTITY_LABEL[k] }))} onChange={(keyBy) => set({ keyBy })} />
            </Field>
            <Field label="严重程度">
              <Segmented value={p.severity} options={SEVERITIES.map((s) => ({ value: s, label: SEVERITY_LABEL[s] }))} onChange={(severity) => set({ severity })} />
            </Field>
            <Field label="作用应用">
              <AppMultiSelect value={p.appIds ?? []} onChange={(appIds) => set({ appIds })} />
            </Field>
          </div>
          <Field label="时间窗">
            <Segmented value={String(p.withinSeconds)} options={withinOptions.map((w) => ({ value: String(w.seconds), label: w.label }))} onChange={(v) => set({ withinSeconds: Number(v) })} />
          </Field>

          <div className="rounded-lg bg-muted px-3 py-2 text-sm">{patternSentence(p)}</div>

          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">步骤 {p.steps.length}/8</span>
              {touched && stepErr >= 0 ? <span className="text-xs text-red-600 dark:text-red-400">第 {stepErr + 1} 步没有选事件</span> : null}
            </div>
            <div className="flex min-w-0 items-start gap-2 overflow-x-auto pb-2">
              {p.steps.map((s, i) => (
                <div key={i} className="flex shrink-0 items-start gap-2">
                  {i > 0 ? (
                    <div className="flex h-24 flex-col items-center justify-center gap-1 text-[11px] text-muted-foreground">
                      <ChevronRight className="size-4" />
                      {s.absent ? "没有" : s.contiguous ? "紧接" : "随后"}
                    </div>
                  ) : null}
                  <StepCard
                    step={s}
                    index={i}
                    count={p.steps.length}
                    meta={meta}
                    sceneOptions={sceneOptions}
                    onChange={(next) => setStep(i, next)}
                    onMove={(d) => move(i, d)}
                    onRemove={() => set({ steps: normalize(p.steps.filter((_, j) => j !== i)) })}
                  />
                </div>
              ))}
              {p.steps.length < 8 ? (
                <Button
                  variant="outline"
                  className="h-24 w-28 shrink-0 border-dashed"
                  onClick={() => set({ steps: normalize([...p.steps.map((s) => ({ ...s, absent: false })), { name: "", events: [], times: 1 }]) })}
                >
                  <Plus />
                  添加一步
                </Button>
              ) : null}
            </div>
          </div>

          <Field label="命中后">
            <ActionsEditor actions={p.actions} onChange={(actions) => set({ actions })} />
          </Field>
          <Field label="说明">
            <Textarea rows={2} value={p.description ?? ""} onChange={(e) => set({ description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={submit} disabled={save.isPending}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function RiskPatterns() {
  const meta = useRiskMeta();
  const patterns = useRiskPatterns();
  const save = useSavePattern();
  const del = useDeletePattern();
  const appName = useAppName();
  const [editing, setEditing] = useState<{ pattern: CepPattern; isNew: boolean } | null>(null);
  const [deleting, setDeleting] = useState<CepPattern | null>(null);
  const list = patterns.data ?? [];

  const toggle = (p: CepPattern) => save.mutate({ ...p, enabled: !p.enabled }, {
    onSuccess: () => toast.success(p.enabled ? "已停用" : "已启用"),
    onError: (e) => toast.error(errorText(e)),
  });

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="min-w-0 flex-1 text-xs text-muted-foreground">保存后 Flink 作业约 30 秒内热加载</span>
        <Button size="sm" disabled={!meta.data} onClick={() => setEditing({ pattern: blank(), isNew: true })}>
          <Plus />
          新建模式
        </Button>
      </div>

      {patterns.isLoading ? (
        <div className="grid gap-3 lg:grid-cols-2">{[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-36 rounded-xl" />)}</div>
      ) : patterns.isError ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">{errorText(patterns.error, "加载失败")}</div>
      ) : list.length === 0 ? (
        <div className="rounded-xl border p-6 text-sm text-muted-foreground">暂无行为模式</div>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {list.map((p) => (
            <div key={p.key} className={cn("flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4", !p.enabled && "opacity-70")}>
              <div className="flex min-w-0 items-start gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                    <span className="truncate text-sm font-semibold">{p.name}</span>
                    <Badge variant={SEVERITY_TONE[p.severity]} size="sm">{SEVERITY_LABEL[p.severity]}</Badge>
                    {p.appIds?.length ? <Badge variant="outline" size="sm">{p.appIds.map((id) => appName(id)).join("、")}</Badge> : null}
                  </div>
                  <div className="font-mono text-[11px] text-muted-foreground">{p.key}</div>
                </div>
                <Switch checked={p.enabled} disabled={save.isPending} onCheckedChange={() => toggle(p)} aria-label="启用" />
              </div>
              <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
                {p.steps.map((s, i) => (
                  <div key={i} className="flex shrink-0 items-center gap-1">
                    {i > 0 ? <span className="px-0.5 text-[11px] text-muted-foreground">{s.absent ? "没有" : s.contiguous ? "紧接" : "随后"}</span> : null}
                    <span className={cn("rounded-md border px-2 py-1 text-xs", s.absent && "border-dashed border-amber-400 dark:border-amber-700")}>
                      {eventsText(s.events)}
                      {s.times > 1 ? <span className="ml-1 text-muted-foreground">×{s.times}</span> : null}
                    </span>
                  </div>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">{patternSentence(p)}</p>
              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                {p.actions.map((a, i) => <Badge key={i} variant={a.kind === "case" ? "info" : a.listKind === "block" ? "danger" : "warning"} size="sm">{actionText(a)}</Badge>)}
                <span className="flex-1" />
                <span className="text-xs text-muted-foreground">{formatShortDateTime(p.updatedAt)}</span>
                <Button size="icon-sm" variant="ghost" aria-label="编辑" disabled={!meta.data} onClick={() => setEditing({ pattern: p, isNew: false })}><Pencil className="size-3.5" /></Button>
                <Button size="icon-sm" variant="ghost" aria-label="删除" onClick={() => setDeleting(p)}><Trash2 className="size-3.5" /></Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing && meta.data ? (
        <PatternDialog
          initial={editing.pattern}
          isNew={editing.isNew}
          meta={meta.data}
          existing={new Set(list.map((p) => p.key))}
          onClose={() => setEditing(null)}
        />
      ) : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除行为模式</AlertDialogTitle>
            <AlertDialogDescription>删除「{deleting?.name}」，Flink 作业热加载后不再匹配。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => deleting && del.mutate(deleting.key, {
                onSuccess: () => toast.success("模式已删除"),
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
