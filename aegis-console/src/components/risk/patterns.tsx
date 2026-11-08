"use client";

import { Fragment, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AlertTriangle,
  ArrowRight,
  Boxes,
  ChevronLeft,
  ChevronRight,
  ClipboardCheck,
  Ban,
  ListPlus,
  Pencil,
  Plus,
  RotateCw,
  Timer,
  Trash2,
} from "lucide-react";

import { CardEmpty } from "@/components/overview/overview-kit";
import { Field, NumberField, Panel, Segmented, TabStack } from "@/components/traffic/traffic-kit";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import type { CepAction, CepPattern, CepStep, CondOp, RiskEntity, RiskMeta } from "@/lib/api/risk";
import { useDeletePattern, useRiskMeta, useRiskPatterns, useRiskScenes, useSavePattern } from "@/lib/risk-hooks";
import { cn } from "@/lib/utils";

import { column, sp, tidy } from "./features";
import { useAppName } from "./kit/app-scope";
import { when } from "./kit/humanize";
import { ACTION_LABEL, DURATION_PRESETS, ENTITY_LABEL, EVENT_LABEL, KIND_LABEL, SEVERITY_LABEL, WINDOW_PRESETS, formatWindow } from "./kit/labels";
import { AppMultiSelect, ConditionRows, errorText, formatCondValue, keyError, MultiPick } from "./studio/kit";

/**
 * 行为模式：同一个人（账号、设备、IP）在一段时间内按顺序做了几件事，就发出告警。
 *
 * 每个模式画成一条时间线：先做什么、再做什么（或之后没有做什么），全部在多长时间之内，
 * 命中后建复核单或加名单。保存约 30 秒后生效，不需要发布。
 */

type KeyBy = CepPattern["keyBy"];
type Severity = CepPattern["severity"];

const KEY_BY: KeyBy[] = ["account", "user", "device", "ip"];
const SEVERITIES: Severity[] = ["low", "medium", "high", "critical"];
const SEVERITY_TONE: Record<Severity, "info" | "warning" | "danger"> = { low: "info", medium: "warning", high: "danger", critical: "danger" };
const SEVERITY_BAR: Record<Severity, string> = { low: "bg-sky-500", medium: "bg-amber-500", high: "bg-orange-500", critical: "bg-red-600" };
const TARGETS: Array<NonNullable<CepAction["target"]>> = ["device", "ip", "account", "user"];
const MAX_WITHIN = 30 * 86400;
const OP_WORD: Record<CondOp, string> = { eq: "为", ne: "不为", gt: "大于", gte: "不小于", lt: "小于", lte: "不大于", in: "属于", not_in: "不属于" };

const eventsText = (events: string[]) => (events.length ? events.map((e) => EVENT_LABEL[e] ?? e).join("或") : "任意事件");

/** 一步做了什么：「登录失败至少 3 次，每次 IP 各不相同」 */
function stepBody(s: CepStep, sceneName?: (k: string) => string): string {
  let body = eventsText(s.events);
  const extra: string[] = [];
  if (s.events.includes("decision")) {
    if (s.scenes?.length) extra.push(`${s.scenes.map((k) => sceneName?.(k) ?? k).join("、")}场景`);
    if (s.actions?.length) extra.push(`结果为${s.actions.map((a) => ACTION_LABEL[a as keyof typeof ACTION_LABEL] ?? a).join("或")}`);
  }
  if (extra.length) body += `（${extra.join("，")}）`;
  if (s.times > 1) body += `至少 ${s.times} 次`;
  if (s.distinct && s.times > 1) body += `，每次${sp(column(s.distinct))}各不相同`;
  if (s.conditions?.length) body += `，且${s.conditions.map((c) => `${sp(column(c.field))}${OP_WORD[c.op] ?? c.op} ${formatCondValue(c.value)} `).join("且")}`;
  return body;
}

function connector(s: CepStep): string {
  if (s.absent) return "之后没有";
  if (s.contiguous) return "紧接着";
  return "再";
}

/** 整个模式的一句话：「同一账号先登录失败至少 3 次，再登录成功，全部在 10 分钟之内」 */
export function patternSentence(p: Pick<CepPattern, "keyBy" | "withinSeconds" | "steps">, sceneName?: (k: string) => string): string {
  const who = ENTITY_LABEL[p.keyBy as RiskEntity] ?? p.keyBy;
  const parts = p.steps.map((s, i) => (i === 0 ? `先${stepBody(s, sceneName)}` : `${connector(s)}${stepBody(s, sceneName)}`));
  if (p.steps.length === 1) return tidy(`同一${sp(who)}在 ${formatWindow(p.withinSeconds)}之内${stepBody(p.steps[0]!, sceneName)}`);
  return tidy(`同一${sp(who)}${parts.join("，")}，全部在 ${formatWindow(p.withinSeconds)}之内`);
}

function actionText(a: CepAction): string {
  if (a.kind === "case") return "生成复核单，等待人工判断";
  const target = ENTITY_LABEL[a.target as RiskEntity] ?? a.target;
  return tidy(`把${sp(target ?? "")}加入${KIND_LABEL[a.listKind ?? "block"]}，${a.durationSeconds ? `${formatWindow(a.durationSeconds)}后自动移除` : "长期有效"}`);
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

/* ════════════════════════ 页面 ════════════════════════ */

export function RiskPatterns() {
  const meta = useRiskMeta();
  const patterns = useRiskPatterns();
  const scenes = useRiskScenes();
  const save = useSavePattern();
  const del = useDeletePattern();
  const [editing, setEditing] = useState<{ pattern: CepPattern; isNew: boolean } | null>(null);
  const [deleting, setDeleting] = useState<CepPattern | null>(null);
  const [picked, setPicked] = useState<string | null>(null);
  const [toggling, setToggling] = useState<string | null>(null);
  const list = useMemo(() => patterns.data ?? [], [patterns.data]);
  const sceneName = useMemo(() => {
    const m = new Map((scenes.data ?? []).map((s) => [s.key, s.name]));
    return (k: string) => m.get(k) ?? k;
  }, [scenes.data]);
  const current = list.find((p) => p.key === picked) ?? list[0] ?? null;
  const enabled = list.filter((p) => p.enabled).length;

  const toggle = (p: CepPattern) => {
    setToggling(p.key);
    save.mutate(
      { ...p, enabled: !p.enabled },
      {
        onSuccess: () => toast.success(p.enabled ? `已停用「${p.name}」` : `已启用「${p.name}」，约 30 秒后生效`),
        onError: (e) => toast.error(errorText(e)),
        onSettled: () => setToggling(null),
      }
    );
  };

  return (
    <TabStack>
      <Panel
        icon={Boxes}
        tone="violet"
        title="行为模式"
        aside={
          <>
            {patterns.data ? (
              <span className="text-xs text-muted-foreground">
                <span className="font-medium text-foreground tabular-nums">{enabled}</span> / {list.length} 个启用
              </span>
            ) : null}
            <Button size="sm" disabled={!meta.data} onClick={() => setEditing({ pattern: blank(), isNew: true })}>
              <Plus />
              新建模式
            </Button>
          </>
        }
        bodyClassName="px-0 pb-0 sm:px-0 sm:pb-0"
      >
        {patterns.isError && !patterns.data ? (
          <div className="flex flex-col items-center gap-3 border-t px-5 py-10 text-center">
            <AlertTriangle className="size-5 text-red-500" />
            <p className="text-sm text-muted-foreground">{errorText(patterns.error, "行为模式读取失败")}</p>
            <Button size="sm" variant="outline" onClick={() => patterns.refetch()}>
              <RotateCw />
              重试
            </Button>
          </div>
        ) : patterns.isLoading ? (
          <PatternsSkeleton />
        ) : list.length === 0 ? (
          <div className="flex flex-col items-center gap-1 border-t pb-6">
            <CardEmpty icon={Boxes} title="还没有行为模式" description="例如同一账号连续登录失败后又登录成功，可自动生成复核单" />
            <Button size="sm" disabled={!meta.data} onClick={() => setEditing({ pattern: blank(), isNew: true })}>
              <Plus />
              新建模式
            </Button>
          </div>
        ) : (
          <div className="grid min-w-0 border-t @4xl:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]">
            <ul className="flex min-w-0 flex-col border-b @4xl:border-r @4xl:border-b-0">
              {list.map((p) => {
                const on = current?.key === p.key;
                return (
                  <li key={p.key} className="border-b last:border-b-0">
                    <div className={cn("relative flex min-w-0 items-start gap-3 px-4 py-3 transition-colors sm:px-5", on ? "bg-muted/60" : "hover:bg-muted/30")}>
                      <span aria-hidden className={cn("absolute inset-y-3 left-0 w-[3px] rounded-r-full", SEVERITY_BAR[p.severity], !p.enabled && "opacity-30")} />
                      <button type="button" onClick={() => setPicked(p.key)} className="min-w-0 flex-1 text-left focus-visible:outline-none" aria-pressed={on}>
                        <span className={cn("block truncate text-sm font-medium", !p.enabled && "text-muted-foreground")}>{p.name}</span>
                        <span className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{patternSentence(p, sceneName)}</span>
                      </button>
                      <Switch
                        checked={p.enabled}
                        disabled={toggling === p.key}
                        onCheckedChange={() => toggle(p)}
                        aria-label={p.enabled ? `停用 ${p.name}` : `启用 ${p.name}`}
                        className="mt-0.5"
                      />
                    </div>
                  </li>
                );
              })}
            </ul>
            {current ? (
              <PatternDetail
                key={current.key}
                pattern={current}
                sceneName={sceneName}
                canEdit={!!meta.data}
                onEdit={() => setEditing({ pattern: current, isNew: false })}
                onDelete={() => setDeleting(current)}
              />
            ) : null}
          </div>
        )}
      </Panel>

      {editing && meta.data ? (
        <PatternDialog
          initial={editing.pattern}
          isNew={editing.isNew}
          meta={meta.data}
          existing={new Set(list.map((p) => p.key))}
          sceneName={sceneName}
          onSaved={(key) => setPicked(key)}
          onClose={() => setEditing(null)}
        />
      ) : null}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{deleting?.name}」</AlertDialogTitle>
            <AlertDialogDescription>约 30 秒后不再识别这个行为模式，已生成的告警与复核单保留。如只想暂停，可改为停用。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() =>
                deleting &&
                del.mutate(deleting.key, {
                  onSuccess: () => {
                    toast.success("行为模式已删除");
                    setPicked(null);
                  },
                  onError: (e) => toast.error(errorText(e, "删除失败")),
                })
              }
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TabStack>
  );
}

/* ── 详情：时间线 ── */

function PatternDetail({
  pattern: p,
  sceneName,
  canEdit,
  onEdit,
  onDelete,
}: {
  pattern: CepPattern;
  sceneName: (k: string) => string;
  canEdit: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const appName = useAppName();
  const updated = when(p.updatedAt);
  return (
    <div className="flex min-w-0 flex-col gap-5 p-4 sm:p-5">
      <div className="flex min-w-0 flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h4 className="text-base font-semibold tracking-tight">{p.name}</h4>
            <Badge variant={SEVERITY_TONE[p.severity]} size="sm">
              {SEVERITY_LABEL[p.severity]}风险
            </Badge>
            {!p.enabled ? (
              <Badge variant="secondary" size="sm">
                已停用
              </Badge>
            ) : null}
          </div>
          <p className="mt-1 text-sm leading-6 text-foreground/85">{patternSentence(p, sceneName)}</p>
          {p.description ? <p className="mt-0.5 text-xs text-muted-foreground">{p.description}</p> : null}
        </div>
        <div className="flex shrink-0 gap-1">
          <Button size="sm" variant="outline" disabled={!canEdit} onClick={onEdit}>
            <Pencil />
            编辑
          </Button>
          <Button size="icon-sm" variant="ghost" className="hover:text-destructive" aria-label="删除" onClick={onDelete}>
            <Trash2 />
          </Button>
        </div>
      </div>

      <StepTimeline pattern={p} sceneName={sceneName} />

      <div className="grid min-w-0 gap-4 @2xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <section className="flex min-w-0 flex-col gap-2">
          <h5 className="text-xs font-medium text-muted-foreground">命中后</h5>
          {p.actions.length ? (
            <ul className="flex min-w-0 flex-col gap-1.5">
              {p.actions.map((a, i) => {
                const Icon = a.kind === "case" ? ClipboardCheck : a.listKind === "block" ? Ban : ListPlus;
                return (
                  <li key={i} className="flex min-w-0 items-center gap-2.5 rounded-lg border px-3 py-2">
                    <span
                      className={cn(
                        "grid size-7 shrink-0 place-items-center rounded-lg",
                        a.kind === "case"
                          ? "bg-sky-500/12 text-sky-600 dark:text-sky-400"
                          : a.listKind === "block"
                            ? "bg-red-500/12 text-red-600 dark:text-red-400"
                            : "bg-amber-500/12 text-amber-600 dark:text-amber-400"
                      )}
                    >
                      <Icon className="size-3.5" />
                    </span>
                    <span className="min-w-0 text-sm">{actionText(a)}</span>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">只记录告警，不做处置</p>
          )}
        </section>
        <section className="flex min-w-0 flex-col gap-2">
          <h5 className="text-xs font-medium text-muted-foreground">范围</h5>
          <dl className="grid min-w-0 gap-2 text-sm">
            <div className="flex min-w-0 justify-between gap-3">
              <dt className="text-muted-foreground">按谁识别</dt>
              <dd className="truncate">{tidy(`同一${sp(ENTITY_LABEL[p.keyBy as RiskEntity] ?? p.keyBy)}`)}</dd>
            </div>
            <div className="flex min-w-0 justify-between gap-3">
              <dt className="text-muted-foreground">作用应用</dt>
              <dd className="truncate text-right">{p.appIds?.length ? p.appIds.map((id) => appName(id)).join("、") : "全部应用"}</dd>
            </div>
            <div className="flex min-w-0 justify-between gap-3">
              <dt className="text-muted-foreground">最近修改</dt>
              <dd className="truncate" title={updated.absolute}>
                {updated.relative ? `${updated.relative}${p.updatedBy ? `，${p.updatedBy}` : ""}` : "—"}
              </dd>
            </div>
          </dl>
        </section>
      </div>
    </div>
  );
}

/** 步骤时间线：节点 + 连线 + 底部的时间窗括线 */
function StepTimeline({ pattern: p, sceneName, compact }: { pattern: Pick<CepPattern, "steps" | "withinSeconds" | "keyBy">; sceneName?: (k: string) => string; compact?: boolean }) {
  return (
    <div className="min-w-0 overflow-x-auto">
      <div className={cn("flex min-w-max flex-col gap-3 rounded-xl border bg-muted/30", compact ? "p-3" : "p-4")}>
        <div className="flex items-stretch">
          {p.steps.map((s, i) => (
            <Fragment key={i}>
              {i > 0 ? (
                <div className="flex w-24 shrink-0 flex-col items-center pt-3">
                  <div className="flex w-full items-center">
                    <span className={cn("h-px flex-1", s.absent ? "border-t border-dashed border-amber-500" : "bg-border")} />
                    <ArrowRight className={cn("size-3.5 shrink-0", s.absent ? "text-amber-500" : "text-muted-foreground")} />
                  </div>
                  <span className={cn("mt-1 rounded-full px-2 py-0.5 text-[11px] font-medium", s.absent ? "bg-amber-500/12 text-amber-700 dark:text-amber-300" : "bg-background text-muted-foreground")}>
                    {s.absent ? "之后没有" : s.contiguous ? "紧接着" : "然后"}
                  </span>
                </div>
              ) : null}
              <div className={cn("flex w-44 shrink-0 flex-col items-center gap-2 text-center", compact && "w-40")}>
                <span
                  className={cn(
                    "grid size-7 place-items-center rounded-full text-xs font-semibold",
                    s.absent ? "border-2 border-dashed border-amber-500 text-amber-600 dark:text-amber-400" : "bg-primary text-primary-foreground"
                  )}
                >
                  {s.absent ? <Ban className="size-3.5" /> : i + 1}
                </span>
                <div className={cn("flex w-full min-w-0 flex-col gap-0.5 rounded-lg border bg-card px-2.5 py-2", s.absent && "border-dashed border-amber-500/60")}>
                  <span className={cn("text-sm leading-5 font-medium break-words", s.absent && "line-through decoration-amber-500/70")}>{eventsText(s.events)}</span>
                  <span className="text-[11px] leading-4 text-muted-foreground">{s.times > 1 ? `至少 ${s.times} 次` : "1 次"}</span>
                  {stepDetails(s, sceneName).map((d) => (
                    <span key={d} className="text-[11px] leading-4 break-words text-muted-foreground">
                      {d}
                    </span>
                  ))}
                </div>
              </div>
            </Fragment>
          ))}
        </div>
        <div className="flex items-center gap-2 px-3">
          <span className="h-2.5 w-px bg-violet-500" />
          <span className="h-px flex-1 bg-violet-500/60" />
          <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-violet-500/12 px-2.5 py-0.5 text-[11px] font-medium text-violet-700 dark:text-violet-300">
            <Timer className="size-3" />
            {tidy(`同一${sp(ENTITY_LABEL[p.keyBy as RiskEntity] ?? p.keyBy)}，全部在 ${formatWindow(p.withinSeconds)}之内`)}
          </span>
          <span className="h-px flex-1 bg-violet-500/60" />
          <span className="h-2.5 w-px bg-violet-500" />
        </div>
      </div>
    </div>
  );
}

function stepDetails(s: CepStep, sceneName?: (k: string) => string): string[] {
  const out: string[] = [];
  if (s.events.includes("decision")) {
    if (s.scenes?.length) out.push(`${s.scenes.map((k) => sceneName?.(k) ?? k).join("、")}场景`);
    if (s.actions?.length) out.push(`结果为${s.actions.map((a) => ACTION_LABEL[a as keyof typeof ACTION_LABEL] ?? a).join("或")}`);
  }
  if (s.distinct && s.times > 1) out.push(tidy(`每次${sp(column(s.distinct))}各不相同`));
  for (const c of s.conditions ?? []) out.push(tidy(`${sp(column(c.field))}${OP_WORD[c.op] ?? c.op} ${formatCondValue(c.value)}`));
  return out;
}

function PatternsSkeleton() {
  return (
    <div className="grid min-w-0 border-t @4xl:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]">
      <div className="flex flex-col border-b @4xl:border-r @4xl:border-b-0">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="flex items-start gap-3 border-b px-4 py-3 last:border-b-0 sm:px-5">
            <div className="flex flex-1 flex-col gap-1.5">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-3 w-full" />
            </div>
            <Skeleton className="h-5 w-9 rounded-full" />
          </div>
        ))}
      </div>
      <div className="flex flex-col gap-5 p-4 sm:p-5">
        <Skeleton className="h-5 w-40" />
        <Skeleton className="h-4 w-full max-w-lg" />
        <div className="flex items-center gap-4 rounded-xl border p-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex flex-col items-center gap-2">
              <Skeleton className="size-7 rounded-full" />
              <Skeleton className="h-14 w-40 rounded-lg" />
            </div>
          ))}
        </div>
        <Skeleton className="h-11 rounded-lg" />
      </div>
    </div>
  );
}

/* ════════════════════════ 编辑 ════════════════════════ */

function StepCard({
  step,
  index,
  count,
  meta,
  sceneOptions,
  onChange,
  onMove,
  onRemove,
}: {
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
    <div className={cn("flex w-80 shrink-0 flex-col gap-3 rounded-xl border bg-card p-3", step.absent && "border-dashed border-amber-500")}>
      <div className="flex items-center gap-1.5">
        <span className="grid size-6 shrink-0 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">{index + 1}</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium">第 {index + 1} 步</span>
        <Button size="icon-xs" variant="ghost" aria-label="前移" disabled={index === 0} onClick={() => onMove(-1)}>
          <ChevronLeft />
        </Button>
        <Button size="icon-xs" variant="ghost" aria-label="后移" disabled={isLast} onClick={() => onMove(1)}>
          <ChevronRight />
        </Button>
        <Button size="icon-xs" variant="ghost" aria-label="删除这一步" disabled={count <= 1} onClick={onRemove}>
          <Trash2 />
        </Button>
      </div>
      {index > 0 ? (
        <div className="flex flex-col gap-1.5 rounded-lg bg-muted/40 px-2.5 py-2">
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={!!step.contiguous} disabled={!!step.absent} onCheckedChange={(contiguous) => set({ contiguous })} />
            紧接着上一步，中间不夹其他事件
          </label>
          <label className={cn("flex items-center gap-2 text-xs", !isLast && "opacity-50")}>
            <Switch checked={!!step.absent} disabled={!isLast} onCheckedChange={(absent) => set({ absent, contiguous: absent ? false : step.contiguous })} />
            改为「之后没有发生」（仅最后一步）
          </label>
        </div>
      ) : null}
      <Field label="发生了什么（任一即可）">
        <MultiPick
          options={meta.eventTypes.map((e) => ({ value: e.type, label: EVENT_LABEL[e.type] ?? e.label, hint: e.type }))}
          value={step.events}
          onChange={(events) => set({ events })}
          emptyLabel="选择事件"
        />
      </Field>
      {hasDecision ? (
        <>
          <Field label="决策所在场景">
            <MultiPick options={sceneOptions} value={step.scenes ?? []} onChange={(scenes) => set({ scenes })} emptyLabel="全部场景" />
          </Field>
          <Field label="决策结果">
            <MultiPick options={meta.actions.map((a) => ({ value: a, label: ACTION_LABEL[a] }))} value={step.actions ?? []} onChange={(actions) => set({ actions })} emptyLabel="任意结果" />
          </Field>
        </>
      ) : null}
      <div className="grid grid-cols-2 gap-2">
        <Field label="至少几次">
          <NumberField value={step.times} min={1} max={1000} onChange={(times) => set({ times: Math.max(1, times), distinct: times > 1 ? step.distinct : undefined })} />
        </Field>
        <Field label="每次哪项不同">
          <Select value={step.distinct || "none"} disabled={step.times <= 1} onValueChange={(v) => set({ distinct: v === "none" ? undefined : v })}>
            <SelectTrigger size="sm" className="h-8 w-full text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">不要求</SelectItem>
              {meta.streamColumns.map((c) => (
                <SelectItem key={c} value={c} className="text-xs">
                  {column(c)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Field label="附加条件">
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
          <Select
            value={a.kind}
            onValueChange={(v) =>
              set(
                i,
                v === "case"
                  ? { kind: "case", target: undefined, listKind: undefined, durationSeconds: undefined }
                  : { kind: "list_add", target: a.target ?? "device", listKind: a.listKind ?? "watch", durationSeconds: a.durationSeconds ?? 86400 }
              )
            }
          >
            <SelectTrigger size="sm" className="h-8 w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="case">生成复核单</SelectItem>
              <SelectItem value="list_add">加入名单</SelectItem>
            </SelectContent>
          </Select>
          {a.kind === "list_add" ? (
            <>
              <Select value={a.target ?? "device"} onValueChange={(v) => set(i, { target: v as CepAction["target"] })}>
                <SelectTrigger size="sm" className="h-8 w-24">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TARGETS.map((t) => (
                    <SelectItem key={t} value={t}>
                      {ENTITY_LABEL[t]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Segmented
                value={a.listKind ?? "watch"}
                options={[
                  { value: "watch" as const, label: KIND_LABEL.watch },
                  { value: "block" as const, label: KIND_LABEL.block },
                ]}
                onChange={(listKind) => set(i, { listKind })}
              />
              <Select value={String(a.durationSeconds ?? 0)} onValueChange={(v) => set(i, { durationSeconds: Number(v) })}>
                <SelectTrigger size="sm" className="h-8 w-24">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DURATION_PRESETS.map((d) => (
                    <SelectItem key={d.seconds} value={String(d.seconds)}>
                      {d.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </>
          ) : null}
          <span className="flex-1" />
          <Button size="icon-sm" variant="ghost" aria-label="删除处置" onClick={() => onChange(actions.filter((_, j) => j !== i))}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      ))}
      <Button size="xs" variant="outline" className="w-fit" onClick={() => onChange([...actions, { kind: "case" }])}>
        <Plus />
        添加处置
      </Button>
    </div>
  );
}

function PatternDialog({
  initial,
  isNew,
  meta,
  existing,
  sceneName,
  onSaved,
  onClose,
}: {
  initial: CepPattern;
  isNew: boolean;
  meta: RiskMeta;
  existing: Set<string>;
  sceneName: (k: string) => string;
  onSaved: (key: string) => void;
  onClose: () => void;
}) {
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
    [steps[i], steps[j]] = [steps[j]!, steps[i]!];
    set({ steps: normalize(steps) });
  };

  const submit = () => {
    setTouched(true);
    if (kErr || nErr || stepErr >= 0) return;
    const steps = normalize(p.steps).map((s, i) => ({ ...s, name: s.name || `s${i + 1}` }));
    save.mutate(
      { ...p, name: p.name.trim(), steps },
      {
        onSuccess: () => {
          toast.success("已保存，约 30 秒后生效");
          onSaved(p.key);
          onClose();
        },
        onError: (e) => toast.error(errorText(e, "保存失败")),
      }
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[94vh] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{isNew ? "新建行为模式" : `编辑「${initial.name}」`}</DialogTitle>
          <DialogDescription>保存约 30 秒后生效，不需要发布。</DialogDescription>
        </DialogHeader>
        <div className="flex min-w-0 flex-col gap-5">
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
            <Field label="名称" error={touched ? nErr : null}>
              <Input value={p.name} placeholder="撞库后登录成功" onChange={(e) => set({ name: e.target.value })} />
            </Field>
            <Field label="模式键" error={touched ? kErr : null}>
              <Input className="font-mono" disabled={!isNew} value={p.key} placeholder="login_bruteforce" onChange={(e) => set({ key: e.target.value.trim() })} />
            </Field>
            <Field label="启用">
              <div className="flex h-9 items-center">
                <Switch checked={p.enabled} onCheckedChange={(enabled) => set({ enabled })} />
              </div>
            </Field>
          </div>
          <div className="grid gap-3 lg:grid-cols-3">
            <Field label="按谁识别">
              <Segmented value={p.keyBy} options={KEY_BY.map((k) => ({ value: k, label: tidy(`同一${sp(ENTITY_LABEL[k])}`) }))} onChange={(keyBy) => set({ keyBy })} />
            </Field>
            <Field label="严重程度">
              <Segmented value={p.severity} options={SEVERITIES.map((s) => ({ value: s, label: SEVERITY_LABEL[s] }))} onChange={(severity) => set({ severity })} />
            </Field>
            <Field label="作用应用">
              <AppMultiSelect value={p.appIds ?? []} onChange={(appIds) => set({ appIds })} />
            </Field>
          </div>
          <Field label="全部步骤需在多长时间之内完成">
            <Segmented value={String(p.withinSeconds)} options={withinOptions.map((w) => ({ value: String(w.seconds), label: w.label }))} onChange={(v) => set({ withinSeconds: Number(v) })} />
          </Field>

          <div className="flex min-w-0 flex-col gap-2">
            <span className="text-xs text-muted-foreground">预览</span>
            <p className="text-sm leading-6">{patternSentence(p, sceneName)}</p>
            <StepTimeline pattern={p} sceneName={sceneName} compact />
          </div>

          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">步骤 {p.steps.length} / 8</span>
              {touched && stepErr >= 0 ? <span className="text-xs text-red-600 dark:text-red-400">第 {stepErr + 1} 步还没有选择事件</span> : null}
            </div>
            <div className="flex min-w-0 items-start gap-2 overflow-x-auto pb-2">
              {p.steps.map((s, i) => (
                <div key={i} className="flex shrink-0 items-start gap-2">
                  {i > 0 ? (
                    <div className="flex h-24 flex-col items-center justify-center gap-1 text-[11px] text-muted-foreground">
                      <ChevronRight className="size-4" />
                      {s.absent ? "之后没有" : s.contiguous ? "紧接着" : "然后"}
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
            <Textarea rows={2} value={p.description ?? ""} placeholder="选填" onChange={(e) => set({ description: e.target.value })} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={save.isPending}>
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
